package agent

import (
	"context"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/provider"
)

func TestEmitDoneIncludesAccumulatedUsage(t *testing.T) {
	ch := make(chan ChatEvent, 1)
	ctx := withTurnUsage(ContextWithChatEvents(context.Background(), ch))
	addTurnUsage(ctx, provider.Usage{InputTokens: 100, OutputTokens: 20})
	addTurnUsage(ctx, provider.Usage{InputTokens: 50, OutputTokens: 10, CacheReadTokens: 8})
	emitDone(ctx)

	ev := <-ch
	if ev.Type != "done" {
		t.Fatalf("type=%q", ev.Type)
	}
	raw, ok := ev.Data["usage"].(map[string]any)
	if !ok {
		t.Fatalf("missing usage: %#v", ev.Data)
	}
	if raw["inputTokens"] != 150 || raw["outputTokens"] != 30 {
		t.Fatalf("tokens=%v", raw)
	}
	if raw["cacheReadTokens"] != 8 || raw["requestCount"] != 2 {
		t.Fatalf("cache/calls=%v", raw)
	}
}

func TestEmitDoneOmitsZeroUsage(t *testing.T) {
	ch := make(chan ChatEvent, 1)
	ctx := withTurnUsage(ContextWithChatEvents(context.Background(), ch))
	emitDone(ctx)
	ev := <-ch
	if ev.Type != "done" {
		t.Fatalf("type=%q", ev.Type)
	}
	if ev.Data != nil {
		t.Fatalf("expected empty done, got %#v", ev.Data)
	}
}

func TestMeterTokensAccumulatesWithoutMeter(t *testing.T) {
	ch := make(chan ChatEvent, 1)
	ctx := withTurnUsage(ContextWithChatEvents(context.Background(), ch))
	a := &Agent{name: "t"}
	a.meterTokens(ctx, "sess", provider.Usage{InputTokens: 12, OutputTokens: 3}, 0)
	emitDone(ctx)
	ev := <-ch
	raw, _ := ev.Data["usage"].(map[string]any)
	if raw["inputTokens"] != 12 || raw["outputTokens"] != 3 || raw["requestCount"] != 1 {
		t.Fatalf("usage=%v", raw)
	}
}
