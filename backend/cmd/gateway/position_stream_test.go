package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
)

type positionLoopFixture struct {
	calls  int
	cancel context.CancelFunc
}

func (f *positionLoopFixture) EnsurePositionSubscription(context.Context) error {
	f.calls++
	if f.calls == 4 {
		f.cancel()
	}
	if f.calls < 3 {
		return errors.New("rejected")
	}
	return nil
}
func (f *positionLoopFixture) PositionSubscription() domain.PositionSubscription {
	if f.calls < 3 {
		return domain.PositionSubscription{Status: "RETRYING"}
	}
	return domain.PositionSubscription{Status: "ACTIVE", Positions: 0}
}

func TestPositionSupervisorContinuesAfterRejectAndConfirmedEmptySnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	f := &positionLoopFixture{cancel: cancel}
	hub := &testMDHub{}
	positionSubscriptionLoop(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), f, hub, time.Millisecond)
	if f.calls != 4 || hub.events != 2 {
		t.Fatal("supervisor stopped or did not publish subscription state", f.calls, hub.events)
	}
}
