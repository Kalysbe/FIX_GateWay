package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	pb "github.com/fix-gateway/backend/gen/realtime/v1"
	"github.com/fix-gateway/backend/internal/adapters"
	"github.com/fix-gateway/backend/internal/admin"
	"github.com/fix-gateway/backend/internal/api"
	"github.com/fix-gateway/backend/internal/audit"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/config"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/executioninbox"
	"github.com/fix-gateway/backend/internal/grpcstream"
	"github.com/fix-gateway/backend/internal/marketdata"
	"github.com/fix-gateway/backend/internal/metrics"
	"github.com/fix-gateway/backend/internal/observability"
	"github.com/fix-gateway/backend/internal/orders"
	"github.com/fix-gateway/backend/internal/outbox"
	"github.com/fix-gateway/backend/internal/platform/db"
	"github.com/fix-gateway/backend/internal/platform/logx"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/fix-gateway/backend/internal/positions"
	"github.com/fix-gateway/backend/internal/quikexport"
	"github.com/fix-gateway/backend/internal/ratelimit"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/fix-gateway/backend/internal/refdata"
	"github.com/fix-gateway/backend/internal/routing"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

func main() {
	cfg := config.Load()
	log := logx.New()
	if err := cfg.Validate(); err != nil {
		log.Error("invalid production config", "error", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := migrate.Up(cfg.DatabaseURL, cfg.MigrationsPath); err != nil {
		log.Error("migration failed", "error", err)
		os.Exit(1)
	}

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("db connect failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	var quikExportStore *quikexport.Store
	if cfg.QuikExportDatabaseURL != "" {
		quikPool, qerr := db.ConnectReadOnly(ctx, cfg.QuikExportDatabaseURL)
		if qerr != nil {
			log.Warn("quik export db connect failed", "error", qerr)
		} else {
			defer quikPool.Close()
			quikExportStore = quikexport.NewStore(quikPool)
			log.Info("quik export database connected")
		}
	}

	users := admin.NewUserStore(pool)
	if err := users.EnsureAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword); err != nil {
		log.Error("ensure admin failed", "error", err)
		os.Exit(1)
	}
	if cfg.IsProduction() {
		if err := users.RefuseDefaultPassword(ctx, cfg.AdminEmail); err != nil {
			log.Error("unsafe administrator credential", "error", err)
			os.Exit(1)
		}
	}

	brokerStore := brokers.NewStore(pool, cfg.APITokenPepper)
	orderStore := orders.NewStore(pool)
	auditStore := audit.NewStore(pool)
	orderSvc := orders.NewService(orderStore, brokerStore, auditStore)
	adapterStore := adapters.NewStore(pool)
	outboxStore := outbox.NewStore(pool)
	refStore := refdata.NewStore(pool)
	hub := realtime.NewHub()
	obsStore := observability.NewStore(pool)
	obsSettings := observability.DefaultSettings()
	obsSettings.FastMS = cfg.ObsFastMS
	obsSettings.NormalMS = cfg.ObsNormalMS
	obsSettings.SlowMS = cfg.ObsSlowMS
	obsSettings.ChannelSize = cfg.ObsChannelSize
	obsSettings.FixRawLogs = cfg.ObsFixRawLogs
	obsSettings.ExportDir = cfg.ExportDir
	obsSettings.AlertWindow = cfg.ObsAlertWindow
	obsSettings.ErrorWarnPct = cfg.ObsErrorWarnPct
	obsSettings.ErrorErrorPct = cfg.ObsErrorErrorPct
	obsSettings.ErrorCritPct = cfg.ObsErrorCritPct
	obsSettings.LatencyWarnMS = cfg.ObsLatencyWarnMS
	obsSettings.LatencyErrorMS = cfg.ObsLatencyErrorMS
	obsSettings.Retention = observability.Retention{
		APIDays: cfg.ObsAPIRetentionDays, BusinessDays: cfg.ObsBusinessRetentionDays,
		AuthDays: cfg.ObsAuthRetentionDays, AuditDays: cfg.ObsAuditRetentionDays,
		DebugDays: cfg.ObsDebugRetentionDays, ErrorDays: cfg.ObsErrorRetentionDays,
		SystemDays: cfg.ObsSystemRetentionDays,
	}
	obs := observability.NewPipeline(obsStore, log, hub, obsSettings)
	obs.Emit(observability.Event{Type: observability.TypeSystem, Subtype: "DATABASE_CONNECTED", Message: "postgres connected"})
	obs.Emit(observability.Event{Type: observability.TypeSystem, Subtype: "QUEUE_CONNECTED", Message: "outbox queue ready"})

	orderSvc.SetObserver(obs)
	inbox, err := executioninbox.Open(cfg.ExecutionInboxDir, cfg.ExecutionInboxMaxBytes, cfg.ExecutionInboxMaxEntries, log)
	if err != nil {
		log.Error("persistent execution inbox unavailable; refusing FIX logon", "error", err)
		os.Exit(1)
	}
	defer inbox.Close()
	if err := obs.InitExports(ctx); err != nil {
		log.Error("export storage initialization failed", "error", err)
		os.Exit(1)
	}
	go obs.Run(ctx)

	var server *api.Server
	manager := adapters.NewManager(
		adapterStore, log, routing.New(cfg.LBStrategy),
		func(report adapters.ExecutionReport) {
			if err := inbox.Append(report); err != nil {
				metrics.ExecutionInboxPersistenceFailures.Inc()
				log.Error("cannot durably accept FIX report; terminating before target sequence advances", "error", err, "adapter_id", report.AdapterID)
				// A normal FIX MessageReject advances the target sequence too. A
				// fail-stop here preserves that sequence for upstream replay; the
				// process must never return success for an unjournalled report.
				os.Exit(1)
			}
		},
		cfg.FIXSessionDir,
		cfg.HealthInterval,
	)
	manager.SetHostOverride(cfg.AdapterHostOverride)
	positionStore := positions.NewStore(pool)
	positionWriter := &automaticPositionSnapshots{store: positionStore, hub: hub}
	manager.SetPositionListener(func(adapterID, reqID string, items []domain.FIXPosition) error {
		persist, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return positionWriter.saveStream(persist, adapterID, reqID, items)
	})
	hub.MarketDataSnapshot = func() any {
		_, quotes, ok := manager.LiveQuotes()
		if !ok {
			quotes = []domain.MarketQuote{}
		}
		for i := range quotes {
			quotes[i].RawFIX = ""
		}
		return map[string]any{"quotes": quotes, "subscription": manager.MarketDataSubscription(), "replace": true}
	}
	manager.SetMarketDataListener(func(quotes []domain.MarketQuote) {
		slim := make([]domain.MarketQuote, len(quotes))
		for i, q := range quotes {
			q.RawFIX = ""
			slim[i] = q
		}
		hub.Broadcast("marketdata.tick", map[string]any{"quotes": slim})
	})
	manager.SetStatusListener(func(adapterID string, status domain.AdapterStatus) {
		hub.Broadcast("adapter.status_changed", map[string]any{
			"adapter_id": adapterID, "status": status,
		})
		if status == domain.AdapterOffline {
			obs.Emit(observability.Event{
				Type: observability.TypeAdapter, Subtype: "ADAPTER_DISCONNECTED",
				AdapterID: adapterID, Message: string(status), Severity: observability.SeverityWarning,
			})
			if manager.AvailableCount() == 0 {
				obs.Emit(observability.Event{
					Type: observability.TypeError, Subtype: observability.CritAllAdaptersOffline,
					Severity: observability.SeverityCritical, AdapterID: adapterID,
					ErrorCode: observability.CritAllAdaptersOffline, Message: "all adapters offline",
					FailureStage: "adapter_manager",
				})
			}
		}
	})
	manager.SetObserver(func(eventType, subtype, adapterID, message string) {
		sev := observability.SeverityInfo
		if subtype == "ADAPTER_DISCONNECTED" || subtype == "ADAPTER_ERROR" {
			sev = observability.SeverityWarning
		}
		obs.Emit(observability.Event{Type: eventType, Subtype: subtype, AdapterID: adapterID, Message: message, Severity: sev})
	})
	manager.SetFIXHandler(func(kind, adapterID, sessionID, msgType, direction, clOrdID, reason string, seq int) {
		e := observability.Event{
			Type: observability.TypeFIX, Subtype: kind, AdapterID: adapterID, SessionID: sessionID,
			FIXMsgType: msgType, FIXDirection: direction, ClientOrderID: clOrdID, Message: reason,
		}
		if seq > 0 {
			e.FIXSeqNum = &seq
		}
		if kind == "FIX_SESSION_DISCONNECTED" {
			e.Severity = observability.SeverityWarning
			obs.Emit(observability.Event{
				Type: observability.TypeError, Subtype: observability.CritFIXSessionDown,
				AdapterID: adapterID, SessionID: sessionID, Severity: observability.SeverityCritical,
				ErrorCode: observability.CritFIXSessionDown, Message: "FIX session down", FailureStage: "fix_session",
			})
		}
		obs.Emit(e)
	})

	server = api.NewServer(cfg, log, pool, brokerStore, orderSvc, orderStore, adapterStore, manager, outboxStore, auditStore, users, hub, obs)
	server.SetExecutionInbox(inbox)
	nonces := auth.NewPersistentNonceStore(pool, 10*time.Minute)
	server.SetNonceStore(nonces)
	go nonces.RunCleanup(ctx)
	go inbox.Run(ctx, func(work context.Context, entry executioninbox.Entry) error {
		return server.OnExecution(orders.WithExecutionReceipt(work, entry.ID), entry.Report)
	}, func(state executioninbox.Status, err error) {
		metrics.ExecutionInboxPending.Set(float64(state.Pending))
		metrics.ExecutionInboxBytes.Set(float64(state.Bytes))
		metrics.ExecutionInboxOldest.Set(state.OldestAge.Seconds())
		if state.Healthy() {
			metrics.ExecutionInboxUnhealthy.Set(0)
		} else {
			metrics.ExecutionInboxUnhealthy.Set(1)
		}
		if err != nil {
			metrics.ExecutionInboxApplyFailures.Inc()
		}
	})
	go server.RunDashboardRefresh(ctx)
	if os.Getenv("TRADE_RECONCILIATION_ENABLED") == "true" { go server.RunTradeReconciliation(ctx) }
	server.SetQuikExport(quikExportStore)
	admission := ratelimit.NewAdmissionWithCapacity(int64(cfg.RateLimitMaxStates))
	rateStore := &ratelimit.Store{Pool: pool, Admission: admission, DefaultRead: cfg.DefaultRateLimitRPS, DefaultTrade: cfg.DefaultOrderRateLimit}
	if err := rateStore.Refresh(ctx); err != nil {
		log.Error("rate limit configuration unavailable", "error", err)
		os.Exit(1)
	}
	server.SetAdmission(rateStore)
	metrics.RegisterAdmission(admission)
	go rateStore.Run(ctx, func(err error) { log.Warn("rate limit refresh failed; retaining previous policies", "error", err) })

	grpcCreds, err := grpcstream.ServerCredentials(cfg.GRPCCertFile, cfg.GRPCKeyFile, cfg.GRPCCAFile, cfg.GRPCRequireClientCert)
	if err != nil {
		log.Error("gRPC TLS configuration failed; provision certificates", "error", err)
		os.Exit(1)
	}
	clientCreds, err := grpcstream.ClientCredentials(cfg.GRPCClientCertFile, cfg.GRPCClientKeyFile, cfg.GRPCCAFile, cfg.GRPCServerName)
	if err != nil {
		log.Error("gRPC bridge TLS configuration failed", "error", err)
		os.Exit(1)
	}
	grpcListener, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		log.Error("gRPC listen failed", "error", err)
		os.Exit(1)
	}
	grpcServer := grpc.NewServer(grpc.Creds(grpcCreds), grpc.MaxRecvMsgSize(16<<10), grpc.MaxSendMsgSize(8<<20), grpc.MaxConcurrentStreams(128), grpc.KeepaliveParams(keepalive.ServerParameters{MaxConnectionIdle: 2 * time.Minute, Time: time.Minute, Timeout: 10 * time.Second}))
	pb.RegisterRealtimeServiceServer(grpcServer, &grpcstream.Service{Hub: hub, Users: users, Brokers: brokerStore, HMACRequired: cfg.HMACRequired, Nonces: nonces, JWTSecret: cfg.JWTSecret, Admission: admission, Log: log, PositionsSnapshot: positions.NewStore(pool).RealtimeSnapshot})
	go func() {
		log.Info("gRPC realtime listening", "addr", cfg.GRPCAddr, "mtls", cfg.GRPCRequireClientCert)
		if err := grpcServer.Serve(grpcListener); err != nil {
			log.Error("gRPC serve failed", "error", err)
			cancel()
		}
	}()
	defer grpcServer.Stop()
	grpcConn, err := grpc.NewClient(cfg.GRPCTarget, grpc.WithTransportCredentials(clientCreds), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(8<<20)))
	if err != nil {
		log.Error("gRPC bridge connection failed", "error", err)
		os.Exit(1)
	}
	defer grpcConn.Close()
	server.SetRealtimeClient(pb.NewRealtimeServiceClient(grpcConn))

	if err := manager.StartAll(ctx); err != nil {
		log.Error("adapter manager start failed", "error", err)
	}

	go syncInstrumentsAfterLogon(ctx, log, manager, refStore)
	if cfg.PositionStreamEnabled {
		go positionSubscriptionLoop(ctx, log, manager, hub, 5*time.Second)
	}
	go positionSnapshotLoop(ctx, log, manager, positionWriter, cfg.PositionSyncInterval)
	go syncMarketDataLoop(ctx, log, manager, marketdata.NewStore(pool), refStore, hub, cfg.MarketDataSyncInterval, cfg.MarketDataBoards)

	worker := outbox.NewWorker(outboxStore, orderStore, manager, log, cfg.WorkerID, cfg.OutboxPollInterval, cfg.OutboxBatchSize)
	worker.SetObserver(obs)
	worker.SetDispatchGuard(func() bool { return inbox.Status().Healthy() })
	orderSvc.SetQueueNotifier(worker.Notify)
	go worker.Run(ctx)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
	go func() {
		log.Info("gateway listening", "addr", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http server failed", "error", err)
			cancel()
		}
	}()
	opsServer := &http.Server{Addr: cfg.OperationsAddr, Handler: server.OperationsHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 1 << 14}
	go func() {
		log.Info("private operations listener", "addr", cfg.OperationsAddr)
		if err := opsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("operations listener failed", "error", err)
			cancel()
		}
	}()

	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
	_ = opsServer.Shutdown(shutdownCtx)
	grpcServer.Stop()
	manager.StopAll(shutdownCtx)
	log.Info("gateway stopped")
}
