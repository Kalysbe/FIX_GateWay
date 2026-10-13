package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
)

type positionSubscriptionManager interface {
	EnsurePositionSubscription(context.Context) error
	PositionSubscription() domain.PositionSubscription
}

func positionSubscriptionLoop(ctx context.Context, log *slog.Logger, manager positionSubscriptionManager, hub marketDataPublisher, interval time.Duration) {
	if interval <= 0 || interval > 5*time.Second {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastError := ""
	lastState := ""
	for {
		check, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := manager.EnsurePositionSubscription(check)
		cancel()
		if err != nil {
			if err.Error() != lastError {
				log.Warn("position subscription unavailable", "error", err)
				lastError = err.Error()
			}
		} else {
			lastError = ""
		}
		state := manager.PositionSubscription()
		if err != nil && state.Error == "" && state.Status != "ACTIVE" {
			state.Error = err.Error()
		}
		key, _ := json.Marshal(state)
		if string(key) != lastState {
			hub.Broadcast("positions.updated", map[string]any{"subscription": state, "source": "fix-stream"})
			lastState = string(key)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
