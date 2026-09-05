package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/fastclaw-ai/fastclaw/internal/provider"
)

// ChatEvent represents a real-time event emitted during the agent ReAct loop.
type ChatEvent struct {
	Type string         `json:"type"` // "content", "content_delta", "tool_call", "tool_result", "tool_progress", "status", "steer", "error", "done", "turn_pending", "subagent_progress"
	Data map[string]any `json:"data,omitempty"`
}

type chatEventsKey struct{}

// ChatEventsFromContext retrieves the events channel from context, if present.
//
// Deprecated: prefer ContextWithStream which carries the persistence
// sink + hub alongside the legacy channel. Kept for callers that only
// need the channel (tests, simple non-persistent flows).
func ChatEventsFromContext(ctx context.Context) chan<- ChatEvent {
	ch, _ := ctx.Value(chatEventsKey{}).(chan<- ChatEvent)
	return ch
}

// ContextWithChatEvents returns a new context with the events channel attached.
//
// Deprecated: prefer ContextWithStream so events also persist + publish
// to the hub for resume-on-reconnect.
func ContextWithChatEvents(ctx context.Context, ch chan<- ChatEvent) context.Context {
	return context.WithValue(ctx, chatEventsKey{}, ch)
}

// emitEvent fans one event out to every consumer registered on ctx:
//   - the persistent sink (session_events table) — assigns a seq used by
//     reconnecting clients to dedup replayed events
//   - the in-process hub (live subscribers across tabs / handlers)
//   - the legacy channel (the synchronous SSE handler that's still
//     holding the request open)
//
// Persistence is best-effort and logged on failure — a DB hiccup
// shouldn't kill the turn. Hub publishes never block (full-buffer
// subscribers are skipped). The legacy channel send respects
// ctx.Done() so the agent goroutine doesn't leak when the channel
// consumer is gone but the agent ctx is cancelled.
// turnUsage accumulates provider.Usage across every LLM call in one
// HandleMessage turn (including extra completions after Insert). The
// trailing `done` event carries the totals so the chat UI can show
// this-turn 入→出 without another round-trip.
type turnUsage struct {
	mu    sync.Mutex
	u     provider.Usage
	calls int
}

type turnUsageKey struct{}

func withTurnUsage(ctx context.Context) context.Context {
	if _, ok := ctx.Value(turnUsageKey{}).(*turnUsage); ok {
		return ctx
	}
	return context.WithValue(ctx, turnUsageKey{}, &turnUsage{})
}

func addTurnUsage(ctx context.Context, u provider.Usage) {
	acc, ok := ctx.Value(turnUsageKey{}).(*turnUsage)
	if !ok || acc == nil {
		return
	}
	acc.mu.Lock()
	defer acc.mu.Unlock()
	acc.u.InputTokens += u.InputTokens
	acc.u.OutputTokens += u.OutputTokens
	acc.u.CacheReadTokens += u.CacheReadTokens
	acc.u.CacheCreationTokens += u.CacheCreationTokens
	acc.calls++
}

func turnUsageData(ctx context.Context) map[string]any {
	acc, ok := ctx.Value(turnUsageKey{}).(*turnUsage)
	if !ok || acc == nil {
		return nil
	}
	acc.mu.Lock()
	defer acc.mu.Unlock()
	if acc.calls == 0 {
		return nil
	}
	if acc.u.InputTokens == 0 && acc.u.OutputTokens == 0 &&
		acc.u.CacheReadTokens == 0 && acc.u.CacheCreationTokens == 0 {
		return nil
	}
	return map[string]any{
		"inputTokens":         acc.u.InputTokens,
		"outputTokens":        acc.u.OutputTokens,
		"cacheReadTokens":     acc.u.CacheReadTokens,
		"cacheCreationTokens": acc.u.CacheCreationTokens,
		"requestCount":        acc.calls,
	}
}

// emitDone closes the turn SSE. When the turn burned any reported
// tokens, they ride on data.usage so the composer can show 入→出.
func emitDone(ctx context.Context) {
	if usage := turnUsageData(ctx); usage != nil {
		emitEvent(ctx, ChatEvent{Type: "done", Data: map[string]any{"usage": usage}})
		return
	}
	emitEvent(ctx, ChatEvent{Type: "done"})
}

func emitEvent(ctx context.Context, evt ChatEvent) {
	stream := streamFromContext(ctx)

	var seq int64 = -1
	// Skip persistence for high-volume live-only events. content_delta
	// streams ~one chunk per generated token (100+ rows per turn for a
	// modest answer), which would dwarf the rest of session_events for
	// no replay value: the trailing `content` event carries the full
	// final text, so a refresh in the middle of a turn just rejoins
	// the live hub and gets the final on completion.
	persist := evt.Type != "content_delta"

	if persist && stream != nil && stream.sink != nil && stream.userID != "" && stream.sessionKey != "" {
		blob, _ := json.Marshal(evt.Data)
		s, err := stream.sink.AppendSessionEvent(ctx, stream.userID, stream.agentID, stream.sessionKey, evt.Type, blob)
		if err != nil {
			slog.Warn("persist chat event failed",
				"agent", stream.agentID, "session", stream.sessionKey,
				"type", evt.Type, "error", err)
		} else {
			seq = s
		}
	}

	if stream != nil && stream.hub != nil && stream.userID != "" && stream.sessionKey != "" {
		stream.hub.Publish(stream.userID, stream.agentID, stream.sessionKey, EventEnvelope{Seq: seq, Event: evt})
	}

	// Legacy channel path: prefer the channel held on streamCtx (set by
	// the new SSE handler); fall back to the deprecated chatEventsKey
	// channel for callers that haven't migrated.
	var ch chan<- ChatEvent
	if stream != nil {
		ch = stream.channel
	}
	if ch == nil {
		ch = ChatEventsFromContext(ctx)
	}
	if ch == nil {
		return
	}
	select {
	case ch <- evt:
	case <-ctx.Done():
		// Insert cancels only the LLM ctx. A turn reader is still
		// there — don't drop the last tokens on that race.
		select {
		case ch <- evt:
		default:
		}
	}
}
