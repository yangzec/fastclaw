package setup

import (
	"context"
	"testing"
	"time"
)

func TestDetachedAgentCtxSurvivesRequestCancel(t *testing.T) {
	reqCtx, reqCancel := context.WithCancel(context.Background())
	ctx, _, release, detach := newDetachedAgentCtx(reqCtx)
	defer release()

	reqCancel()
	// WithoutCancel + detach must keep in-flight tools alive after
	// the browser refresh cancels the HTTP request.
	select {
	case <-ctx.Done():
		t.Fatal("request cancel must not cancel the detached agent context")
	case <-time.After(20 * time.Millisecond):
	}

	detach()
	release()
	if err := ctx.Err(); err != nil {
		t.Fatalf("detach+release canceled agent ctx: %v", err)
	}
}

func TestDetachedAgentCtxStopStillWorksAfterDetach(t *testing.T) {
	ctx, cancel, release, detach := newDetachedAgentCtx(context.Background())
	defer release()
	detach()
	release()
	if ctx.Err() != nil {
		t.Fatal("detach must not cancel the agent context")
	}
	cancel()
	if ctx.Err() == nil {
		t.Fatal("explicit Stop cancel must still kill a detached turn")
	}
}

func TestReleaseCancelsWhenHandlerStillOwnsTurn(t *testing.T) {
	ctx, _, release, _ := newDetachedAgentCtx(context.Background())
	release()
	if ctx.Err() == nil {
		t.Fatal("release should cancel when the handler still owns the turn")
	}
}
