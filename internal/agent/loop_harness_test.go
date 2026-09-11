package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/bus"
	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/provider"
)

type scriptedProvider struct {
	chatN, streamN atomic.Int32
	onChat         func(n int, tools []provider.Tool) (*provider.Response, error)
	onStream       func(n int, tools []provider.Tool) (*provider.Response, error)
}

func (p *scriptedProvider) Chat(ctx context.Context, messages []provider.Message, tools []provider.Tool, model string, maxTokens int, temperature float64) (*provider.Response, error) {
	n := int(p.chatN.Add(1))
	if p.onChat != nil {
		return p.onChat(n, tools)
	}
	return p.ChatFromStream(ctx, messages, tools, model, maxTokens, temperature)
}

func (p *scriptedProvider) ChatFromStream(ctx context.Context, messages []provider.Message, tools []provider.Tool, model string, maxTokens int, temperature float64) (*provider.Response, error) {
	sr, err := p.ChatStream(ctx, messages, tools, model, maxTokens, temperature)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	var tcs []provider.ToolCall
	var thinking string
	var raw json.RawMessage
	for {
		chunk, ok := sr.Next()
		if !ok {
			break
		}
		b.WriteString(chunk.Content)
		if chunk.Done {
			tcs = chunk.ToolCalls
			thinking = chunk.Thinking
			raw = chunk.RawAssistant
		}
	}
	return &provider.Response{Content: b.String(), ToolCalls: tcs, Thinking: thinking, RawAssistant: raw}, sr.Err()
}

func (p *scriptedProvider) ChatStream(ctx context.Context, messages []provider.Message, tools []provider.Tool, model string, maxTokens int, temperature float64) (*provider.StreamReader, error) {
	n := int(p.streamN.Add(1))
	var resp *provider.Response
	var err error
	if p.onStream != nil {
		resp, err = p.onStream(n, tools)
	} else if p.onChat != nil {
		resp, err = p.onChat(n, tools)
	}
	if err != nil {
		return nil, err
	}
	if resp == nil {
		resp = &provider.Response{Content: "ok"}
	}
	ch := make(chan provider.StreamChunk, 2)
	go func() {
		defer close(ch)
		if resp.Content != "" {
			ch <- provider.StreamChunk{Content: resp.Content}
		}
		ch <- provider.StreamChunk{
			Done:         true,
			ToolCalls:    resp.ToolCalls,
			Thinking:     resp.Thinking,
			RawAssistant: resp.RawAssistant,
		}
	}()
	return provider.NewStreamReader(ch), nil
}

func testAgent(t *testing.T, p provider.Provider) *Agent {
	t.Helper()
	home := t.TempDir()
	return NewAgent(config.ResolvedAgent{
		ID:                "agt_harness",
		DisplayName:       "Harness",
		Home:              home + "/agent",
		Workspace:         home + "/ws",
		Model:             "test/tiny",
		MaxTokens:         128,
		MaxToolIterations: 6,
	}, p, bus.New(), home)
}

func TestHandleMessageStreamDeliversChatContentWithoutSecondStream(t *testing.T) {
	p := &scriptedProvider{
		onChat: func(n int, tools []provider.Tool) (*provider.Response, error) {
			if n != 1 {
				t.Errorf("unexpected Chat call %d", n)
			}
			return &provider.Response{Content: "hello from chat"}, nil
		},
	}
	ag := testAgent(t, p)
	sr := ag.HandleMessageStream(context.Background(), bus.InboundMessage{Channel: "web", ChatID: "c1", UserID: "u1", Text: "hi"})
	var b strings.Builder
	for {
		chunk, ok := sr.Next()
		if !ok {
			break
		}
		b.WriteString(chunk.Content)
	}
	if sr.Err() != nil {
		t.Fatal(sr.Err())
	}
	if got := b.String(); got != "hello from chat" {
		t.Fatalf("stream=%q", got)
	}
	if p.streamN.Load() != 0 {
		t.Fatalf("ChatStream called %d times; want 0", p.streamN.Load())
	}
}

func TestHandleMessageEmptyContentRetriesOnceWithToolsDisabled(t *testing.T) {
	p := &scriptedProvider{
		onStream: func(n int, tools []provider.Tool) (*provider.Response, error) {
			if n == 1 {
				return &provider.Response{Content: "   "}, nil
			}
			if tools != nil {
				t.Errorf("retry %d still had %d tools", n, len(tools))
			}
			return &provider.Response{Content: "recovered text"}, nil
		},
	}
	ag := testAgent(t, p)
	got := ag.HandleMessage(context.Background(), bus.InboundMessage{Channel: "web", ChatID: "c1", UserID: "u1", Text: "hi"})
	if !strings.Contains(got, "recovered text") {
		t.Fatalf("got %q", got)
	}
	if p.streamN.Load() != 2 {
		t.Fatalf("stream calls=%d want 2", p.streamN.Load())
	}
}

func TestHandleMessageXMLDoesNotExecute(t *testing.T) {
	var ran atomic.Int32
	p := &scriptedProvider{
		onStream: func(n int, tools []provider.Tool) (*provider.Response, error) {
			if n == 1 {
				return &provider.Response{Content: `<invoke name="exec"><parameter name="command" string="true">echo pwned</parameter></invoke>`}, nil
			}
			return &provider.Response{Content: "answered in text"}, nil
		},
	}
	ag := testAgent(t, p)
	ag.registry.Register("exec", "exec", map[string]any{"type": "object"}, func(context.Context, json.RawMessage) (string, error) {
		ran.Add(1)
		return "pwned", nil
	})
	got := ag.HandleMessage(context.Background(), bus.InboundMessage{Channel: "web", ChatID: "c1", UserID: "u1", Text: "hi"})
	if ran.Load() != 0 {
		t.Fatal("XML invoke executed exec")
	}
	if !strings.Contains(got, "answered in text") {
		t.Fatalf("got %q", got)
	}
}

func TestHandleMessageLoopDetectedDoesNotCap(t *testing.T) {
	p := &scriptedProvider{
		onStream: func(n int, tls []provider.Tool) (*provider.Response, error) {
			if n <= 3 {
				return &provider.Response{ToolCalls: []provider.ToolCall{{
					ID:       "c1",
					Type:     "function",
					Function: provider.FunctionCall{Name: "echo", Arguments: `{"x":1}`},
				}}}, nil
			}
			if tls != nil {
				t.Errorf("wrap-up call %d still had tools", n)
			}
			return &provider.Response{Content: "stopped looping"}, nil
		},
	}
	ag := testAgent(t, p)
	ag.registry.Register("echo", "echo", map[string]any{"type": "object"}, func(context.Context, json.RawMessage) (string, error) {
		return "same", nil
	})
	got := ag.HandleMessage(context.Background(), bus.InboundMessage{Channel: "web", ChatID: "c1", UserID: "u1", Text: "hi"})
	if !strings.Contains(got, "stopped looping") {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "maximum number of tool iterations") {
		t.Fatalf("loop stall used cap copy: %q", got)
	}
}
