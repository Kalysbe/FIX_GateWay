package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fix-gateway/backend/internal/adapters"
	"github.com/fix-gateway/backend/internal/admin"
	"github.com/fix-gateway/backend/internal/api"
	"github.com/fix-gateway/backend/internal/audit"
	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/config"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/marketdata"
	"github.com/fix-gateway/backend/internal/observability"
	"github.com/fix-gateway/backend/internal/orders"
	"github.com/fix-gateway/backend/internal/outbox"
	"github.com/fix-gateway/backend/internal/platform/db"
	"github.com/fix-gateway/backend/internal/platform/logx"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/fix-gateway/backend/internal/positions"
	"github.com/fix-gateway/backend/internal/quikexport"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/fix-gateway/backend/internal/refdata"
	"github.com/fix-gateway/backend/internal/routing"
)

func main() {
	cfg := config.Load()
	log := logx.New()
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
	go obs.Run(ctx)
	obs.Emit(observability.Event{Type: observability.TypeSystem, Subtype: "DATABASE_CONNECTED", Message: "postgres connected"})
	obs.Emit(observability.Event{Type: observability.TypeSystem, Subtype: "QUEUE_CONNECTED", Message: "outbox queue ready"})

	orderSvc.SetObserver(obs)

	var server *api.Server
	manager := adapters.NewManager(
		adapterStore, log, routing.New(cfg.LBStrategy),
		func(report adapters.ExecutionReport) {
			if server != nil {
				server.OnExecution(report)
			}
		},
		filepath.Join(os.TempDir(), "fix-gateway-sessions"),
		cfg.HealthInterval,
	)
	manager.SetHostOverride(cfg.AdapterHostOverride)
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
	server.SetQuikExport(quikExportStore)

	if err := manager.StartAll(ctx); err != nil {
		log.Error("adapter manager start failed", "error", err)
	}

	go syncInstrumentsAfterLogon(ctx, log, manager, refStore)
	go syncPositionsLoop(ctx, log, manager, positions.NewStore(pool), hub, cfg.PositionSyncInterval)
	go syncMarketDataLoop(ctx, log, manager, marketdata.NewStore(pool), refStore, hub, cfg.MarketDataSyncInterval)

	worker := outbox.NewWorker(outboxStore, orderStore, manager, log, cfg.WorkerID, cfg.OutboxPollInterval, cfg.OutboxBatchSize)
	worker.SetObserver(obs)
	go worker.Run(ctx)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Info("gateway listening", "addr", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http server failed", "error", err)
			cancel()
		}
	}()

	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
	manager.StopAll(shutdownCtx)
	log.Info("gateway stopped")
}

func syncInstrumentsAfterLogon(ctx context.Context, log *slog.Logger, manager *adapters.Manager, refStore *refdata.Store) {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if manager.AvailableCount() > 0 {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
	if manager.AvailableCount() == 0 {
		log.Warn("skip FIX instrument sync: no online adapters yet")
		return
	}
	syncCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	adapterID, items, err := manager.FetchSecurities(syncCtx)
	if err != nil {
		log.Warn("FIX instrument sync failed", "error", err)
		return
	}
	n, err := refStore.SyncFromFIX(syncCtx, items)
	if err != nil {
		log.Warn("FIX instrument upsert failed", "error", err)
		return
	}
	log.Info("instruments synced via FIX Security Definition",
		"adapter_id", adapterID,
		"upserted", n.InstrumentsUpserted,
		"removed", n.InstrumentsRemoved,
		"inactive", n.InstrumentsInactive,
	)
}

func syncPositionsLoop(ctx context.Context, log *slog.Logger, manager *adapters.Manager, store *positions.Store, hub *realtime.Hub, interval time.Duration) {
	if interval <= 0 {
		log.Info("background FIX position sync disabled")
		return
	}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if manager.AvailableCount() > 0 {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}

	run := func() {
		syncCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		adapterID, reqID, count, _, err := store.SyncFromFIX(syncCtx, manager, domain.PositionQuery{})
		if err != nil {
			log.Warn("background FIX position sync failed", "error", err)
			return
		}
		log.Info("positions synced via FIX", "adapter_id", adapterID, "pos_req_id", reqID, "received", count)
		hub.Broadcast("positions.updated", map[string]any{
			"adapter_id": adapterID, "pos_req_id": reqID, "received": count,
		})
	}

	run()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}

func syncMarketDataLoop(ctx context.Context, log *slog.Logger, manager *adapters.Manager, store *marketdata.Store, refStore *refdata.Store, hub *realtime.Hub, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if manager.AvailableCount() > 0 {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}

	subscribe := func() bool {
		list, err := refStore.ListActiveInstruments(ctx)
		if err != nil || len(list) == 0 {
			log.Info("market data subscribe skipped: no instruments yet")
			return false
		}
		syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		adapterID, reqID, items, err := manager.SubscribeMarketData(syncCtx, domain.MarketDataQuery{Instruments: list})
		if err != nil {
			log.Warn("FIX market data subscribe failed", "error", err)
			return false
		}
		log.Info("market data subscribed via FIX", "adapter_id", adapterID, "md_req_id", reqID, "quotes", len(items))
		if len(items) > 0 {
			hub.Broadcast("marketdata.tick", map[string]any{"quotes": items})
			snapID, err := store.BeginSnapshot(syncCtx, adapterID, reqID)
			if err == nil {
				_ = store.InsertQuotes(syncCtx, snapID, items)
				_ = store.CompleteSnapshot(syncCtx, snapID, "OK", "", len(items))
			}
		}
		return true
	}

	for {
		if subscribe() {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, items, ok := manager.LiveQuotes(); ok && len(items) > 0 {
				continue
			}
			_ = subscribe()
		}
	}
}
