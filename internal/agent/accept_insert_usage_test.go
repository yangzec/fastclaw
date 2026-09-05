package agent

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/bus"
	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/provider"
)

// interruptThenTurnProvider streams a partial reply, waits for Insert
// to cancel the bound LLM ctx, then answers the redirected turn.
type interruptThenTurnProvider struct {
	firstChunk chan struct{}
	calls      atomic.Int32
}

func (p *interruptThenTurnProvider) Chat(ctx context.Context, messages []provider.Message, tools []provider.Tool, model string, maxTokens int, temperature float64) (*provider.Response, error) {
	sr, err := p.ChatStream(ctx, messages, tools, model, maxTokens, temperature)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	var usage provider.Usage
	for {
		chunk, ok := sr.Next()
		if !ok {
			break
		}
		b.WriteString(chunk.Content)
		if chunk.Done {
			usage = chunk.Usage
		}
	}
	return &provider.Response{Content: b.String(), Usage: usage}, sr.Err()
}

func (p *interruptThenTurnProvider) ChatStream(ctx context.Context, messages []provider.Message, tools []provider.Tool, model string, maxTokens int, temperature float64) (*provider.StreamReader, error) {
	n := p.calls.Add(1)
	ch := make(chan provider.StreamChunk, 4)
	go func() {
		defer close(ch)
		if n == 1 {
			ch <- provider.StreamChunk{Content: "I was listing fruits"}
			close(p.firstChunk)
			<-ctx.Done()
			return
		}
		ch <- provider.StreamChunk{Content: "TURNED_AROUND"}
		ch <- provider.StreamChunk{
			Done:  true,
			Usage: provider.Usage{InputTokens: 80, OutputTokens: 6},
		}
	}()
	return provider.NewStreamReader(ch), nil
}

func TestAcceptInsertCancelsStreamAndReportsTurnUsage(t *testing.T) {
	prov := &interruptThenTurnProvider{firstChunk: make(chan struct{})}
	home := t.TempDir()
	ag := NewAgent(config.ResolvedAgent{
		ID:                "agt_accept",
		DisplayName:       "Accept",
		Home:              home + "/agent",
		Workspace:         home + "/workspace",
		Model:             "test/tiny",
		MaxTokens:         128,
		MaxToolIterations: 4,
	}, prov, bus.New(), home)

	events := make(chan ChatEvent, 64)
	replyCh := make(chan string, 1)
	go func() {
		replyCh <- ag.HandleWebChatStream(context.Background(), "s-accept", "", "user-1", "list some fruits", nil, nil, events)
		close(events)
	}()

	select {
	case <-prov.firstChunk:
	case <-time.After(5 * time.Second):
		t.Fatal("first stream chunk never arrived")
	}
	if !ag.SteerWeb("s-accept", "", "stop, say TURNED_AROUND") {
		t.Fatal("Insert must land on the in-flight turn")
	}

	var reply string
	select {
	case reply = <-replyCh:
	case <-time.After(8 * time.Second):
		t.Fatal("turn did not finish after Insert")
	}

	var types []string
	var sawSteer, sawPartial, sawTurn, sawUsage bool
	var usage map[string]any
	for ev := range events {
		types = append(types, ev.Type)
		switch ev.Type {
		case "content_delta":
			if d, _ := ev.Data["delta"].(string); strings.Contains(d, "fruits") {
				sawPartial = true
			}
			if d, _ := ev.Data["delta"].(string); strings.Contains(d, "TURNED_AROUND") {
				sawTurn = true
			}
		case "steer":
			if ev.Data["content"] == "stop, say TURNED_AROUND" {
				sawSteer = true
			}
		case "content":
			if c, _ := ev.Data["content"].(string); strings.Contains(c, "TURNED_AROUND") {
				sawTurn = true
			}
		case "done":
			if raw, ok := ev.Data["usage"].(map[string]any); ok {
				usage = raw
				sawUsage = true
			}
		}
	}

	if !sawPartial {
		t.Fatalf("expected partial fruit text before Insert, events=%v reply=%q", types, reply)
	}
	if !sawSteer {
		t.Fatalf("expected steer echo, events=%v", types)
	}
	if !sawTurn && !strings.Contains(reply, "TURNED_AROUND") {
		t.Fatalf("expected redirected reply, events=%v reply=%q", types, reply)
	}
	if !sawUsage {
		t.Fatalf("done should carry this-turn usage, events=%v", types)
	}
	if usage["inputTokens"] != 80 || usage["outputTokens"] != 6 {
		t.Fatalf("usage=%v", usage)
	}
	if prov.calls.Load() < 2 {
		t.Fatalf("Insert should start a second completion, calls=%d", prov.calls.Load())
	}
}
