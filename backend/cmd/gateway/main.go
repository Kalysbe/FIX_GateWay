package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
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
	"github.com/fix-gateway/backend/internal/metrics"
	"github.com/fix-gateway/backend/internal/observability"
	"github.com/fix-gateway/backend/internal/orders"
	"github.com/fix-gateway/backend/internal/outbox"
	"github.com/fix-gateway/backend/internal/platform/db"
	"github.com/fix-gateway/backend/internal/platform/logx"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/fix-gateway/backend/internal/quikexport"
	"github.com/fix-gateway/backend/internal/ratelimit"
	"github.com/fix-gateway/backend/internal/realtime"
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
		// The pool reconnects on demand: an outage at startup only delays Orders.
		quikPool, qerr := db.OpenReadOnly(ctx, cfg.QuikExportDatabaseURL)
		if quikPool == nil {
			log.Error("quik export database url invalid; Orders disabled", "error", qerr)
		} else {
			defer quikPool.Close()
			quikExportStore = quikexport.NewStore(quikPool)
			if qerr != nil {
				log.Warn("quik export database unavailable at startup; will retry on each read", "error", qerr)
			} else {
				log.Info("quik export database connected")
			}
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

	// Logs and the audit trail go to daily files, not the database.
	logFiles, err := observability.OpenFileLog(cfg.LogDir)
	if err != nil {
		log.Error("log directory unavailable", "dir", cfg.LogDir, "error", err)
		os.Exit(1)
	}
	defer logFiles.Close()
	brokerStore := brokers.NewStore(pool, cfg.APITokenPepper)
	orderStore := orders.NewStore(pool)
	auditStore := audit.NewStore(logFiles)
	orderSvc := orders.NewService(orderStore, brokerStore, auditStore)
	if quikExportStore != nil {
		// Accounts a firm may trade come only from QUIK Trdaccs, instruments from Securities.
		orderSvc.SetTradingAccounts(quikExportStore)
		orderSvc.SetInstruments(quikExportStore)
	}
	adapterStore := adapters.NewStore(pool)
	outboxStore := outbox.NewStore(pool)
	hub := realtime.NewHub()
	obsStore := observability.NewStore(pool, logFiles)
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
	fixHours, err := adapters.ParseSessionHours(cfg.FIXSessionHours, quikexport.KSETime)
	if err != nil {
		log.Error("invalid FIX_SESSION_HOURS", "error", err)
		os.Exit(1)
	}
	manager.SetSessionHours(fixHours)
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
	sessionDown := newSessionDownAlarm(fixSessionDownGrace, func(adapterID, sessionID string) {
		// A stopped adapter (scheduled pause, disable, shutdown) is not an outage.
		if _, running := manager.Get(adapterID); !running {
			return
		}
		obs.Emit(observability.Event{
			Type: observability.TypeError, Subtype: observability.CritFIXSessionDown,
			AdapterID: adapterID, SessionID: sessionID, Severity: observability.SeverityCritical,
			ErrorCode: observability.CritFIXSessionDown, Message: "FIX session down", FailureStage: "fix_session",
		})
	})
	manager.SetFIXHandler(func(kind, adapterID, sessionID, msgType, direction, clOrdID, reason string, seq int) {
		e := observability.Event{
			Type: observability.TypeFIX, Subtype: kind, AdapterID: adapterID, SessionID: sessionID,
			FIXMsgType: msgType, FIXDirection: direction, ClientOrderID: clOrdID, Message: reason,
		}
		if seq > 0 {
			e.FIXSeqNum = &seq
		}
		switch kind {
		case "FIX_SESSION_DISCONNECTED":
			e.Severity = observability.SeverityWarning
			sessionDown.down(adapterID, sessionID)
		case "FIX_SESSION_CONNECTED":
			sessionDown.up(adapterID)
		}
		obs.Emit(e)
	})

	server = api.NewServer(cfg, log, pool, brokerStore, orderSvc, orderStore, adapterStore, manager, outboxStore, auditStore, users, hub, obs)
	inboxCases := executioninbox.NewCaseStore(pool)
	inbox.SetQuarantineCheck(inboxCases.Exists)
	server.SetExecutionInbox(inbox, inboxCases)
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
	marketClasses := quikexport.NewClassSettings(pool)
	server.SetMarketDataClasses(marketClasses)
	orderSvc.SetServedClasses(marketClasses.Classes)
	if quikExportStore != nil {
		quikExportStore.SetServedClasses(marketClasses.Classes)
	}
	server.SetQuikExport(quikExportStore)
	go server.RunFIXSafetyMonitor(ctx)
	go orderStore.RunPurge(ctx, log, cfg.FinishedOrderGrace)
	if quikExportStore != nil {
		go quikExportStore.RunOrders(ctx, func(change quikexport.OrderChange) {
			hub.BroadcastOrderForFirm(change.Order.Raw, strings.TrimSpace(change.Order.Text("FirmId")), change.Removed)
		}, func(err error) {
			log.Warn("QUIK Orders change read failed; retaining previous selection", "error", err)
		})
		go quikExportStore.RunTrades(ctx, func(trade quikexport.Order) {
			hub.BroadcastTradeForFirm(trade.Raw, strings.TrimSpace(trade.Text("FirmId")))
		}, func(err error) {
			log.Warn("QUIK Trades change read failed; retaining previous selection", "error", err)
		})
		go quikExportStore.RunPositions(ctx, func(table string) {
			// Only a trigger: each broker stream reads its own firm's rows of the table.
			hub.Broadcast(table+".updated", map[string]any{"source": "quik"})
		}, func(err error) {
			log.Warn("QUIK positions change read failed; retaining previous selection", "error", err)
		})
		go quikExportStore.RunParams(ctx, func(change quikexport.ParamChange) {
			hub.BroadcastMarketData(change.Param.Raw, change.Removed)
		}, func(err error) {
			log.Warn("QUIK Params change read failed; retaining previous selection", "error", err)
		})
	}
	admission := ratelimit.NewAdmissionWithCapacity(int64(cfg.RateLimitMaxStates))
	rateStore := &ratelimit.Store{Pool: pool, Audit: auditStore.RateLimitUpdated, Admission: admission, DefaultRead: cfg.DefaultRateLimitRPS, DefaultTrade: cfg.DefaultOrderRateLimit}
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
	if notAfter, err := grpcstream.CertificateExpiry(cfg.GRPCCertFile); err == nil {
		server.SetGRPCCertificateExpiry(notAfter)
		if !time.Now().Before(notAfter) {
			log.Error("gRPC server certificate has expired; TLS clients and the admin event stream cannot connect", "not_after", notAfter)
		} else if time.Until(notAfter) <= api.GRPCCertWarnBefore {
			log.Warn("gRPC server certificate expires soon", "not_after", notAfter)
		}
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
	pb.RegisterRealtimeServiceServer(grpcServer, &grpcstream.Service{Hub: hub, Users: users, Brokers: brokerStore, HMACRequired: cfg.HMACRequired, Nonces: nonces, JWTSecret: cfg.JWTSecret, Admission: admission, Log: log, PositionsSnapshot: func(work context.Context, table string, firms []string) (any, error) {
		return quikExportStore.ListPositionTable(work, table, firms)
	}})
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

	worker := outbox.NewWorker(outboxStore, orderStore, manager, log, cfg.WorkerID, cfg.OutboxPollInterval, cfg.OutboxBatchSize)
	worker.SetObserver(obs)
	worker.SetDispatchGuard(func() bool { return inbox.Status().Healthy() })
	orderSvc.SetQueueNotifier(worker.Notify)
	go worker.Run(ctx)

	// Streams and export downloads set their own write deadlines.
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      cfg.HTTPWriteTimeout,
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
	// Long-lived streams would hold the HTTP drain for the whole budget, and
	// the container is killed before the FIX sessions send Logout; QUIK then
	// sees a dropped socket. The drain is capped so Logout always goes out.
	drainCtx, drainCancel := context.WithTimeout(shutdownCtx, 5*time.Second)
	_ = httpServer.Shutdown(drainCtx)
	_ = opsServer.Shutdown(drainCtx)
	drainCancel()
	grpcServer.Stop()
	manager.StopAll(shutdownCtx)
	log.Info("gateway stopped")
}
