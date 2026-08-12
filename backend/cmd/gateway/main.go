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
	"github.com/fix-gateway/backend/internal/orders"
	"github.com/fix-gateway/backend/internal/outbox"
	"github.com/fix-gateway/backend/internal/platform/db"
	"github.com/fix-gateway/backend/internal/platform/logx"
	"github.com/fix-gateway/backend/internal/platform/migrate"
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
	manager.SetStatusListener(func(adapterID string, status domain.AdapterStatus) {
		hub.Broadcast("adapter.status_changed", map[string]any{
			"adapter_id": adapterID, "status": status,
		})
	})

	server = api.NewServer(cfg, log, pool, brokerStore, orderSvc, orderStore, adapterStore, manager, outboxStore, auditStore, users, hub)

	if err := manager.StartAll(ctx); err != nil {
		log.Error("adapter manager start failed", "error", err)
	}

	go syncInstrumentsAfterLogon(ctx, log, manager, refStore)

	worker := outbox.NewWorker(outboxStore, orderStore, manager, log, cfg.WorkerID, cfg.OutboxPollInterval, cfg.OutboxBatchSize)
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
