package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/agent/tools"
	"github.com/fastclaw-ai/fastclaw/internal/bus"
	"github.com/fastclaw-ai/fastclaw/internal/provider"
	"github.com/fastclaw-ai/fastclaw/internal/session"
)

type finalToolCallProvider struct {
	streamCalls atomic.Int32
}

func (p *finalToolCallProvider) Chat(ctx context.Context, messages []provider.Message, tools []provider.Tool, model string, maxTokens int, temperature float64) (*provider.Response, error) {
	return &provider.Response{Content: "initial answer without tools"}, nil
}

func (p *finalToolCallProvider) ChatStream(ctx context.Context, messages []provider.Message, tools []provider.Tool, model string, maxTokens int, temperature float64) (*provider.StreamReader, error) {
	ch := make(chan provider.StreamChunk, 4)
	switch p.streamCalls.Add(1) {
	case 1:
		ch <- provider.StreamChunk{Done: true, ToolCalls: []provider.ToolCall{{
			ID:   "call-final-tool",
			Type: "function",
			Function: provider.FunctionCall{
				Name:      "record_tool",
				Arguments: `{"value":"from-final-synthesis"}`,
			},
		}}}
	default:
		ch <- provider.StreamChunk{Content: "final answer after tool"}
		ch <- provider.StreamChunk{Done: true}
	}
	close(ch)
	return provider.NewStreamReader(ch), nil
}

func TestHandleMessageStreamExecutesToolCallsFromFinalSynthesis(t *testing.T) {
	var toolRan atomic.Bool
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	reg.Register("record_tool", "record that the tool ran", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"value": map[string]any{"type": "string"},
		},
	}, func(ctx context.Context, args json.RawMessage) (string, error) {
		toolRan.Store(true)
		if !strings.Contains(string(args), "from-final-synthesis") {
			t.Fatalf("unexpected tool args: %s", args)
		}
		return "tool result ok", nil
	})

	home := t.TempDir()
	workspace := t.TempDir()
	a := &Agent{
		name:              "agent-test",
		provider:          &finalToolCallProvider{},
		registry:          reg,
		sessions:          session.NewManager(t.TempDir()),
		memory:            NewMemory(home),
		ctxBuilder:        NewContextBuilder(home, NewMemory(home), ""),
		hooks:             NewHookRegistry(),
		model:             "kimi/k3[1m]",
		maxTokens:         128,
		temperature:       0.7,
		maxToolIterations: 3,
		messageBus:        bus.New(),
		engine:            newSDKEngine("test-session"),
		workspacePath:     workspace,
		ownerUserID:       "owner-1",
	}
	a.ctxBuilder.SetWorkspace(workspace)

	msg := bus.InboundMessage{Channel: "web", ChatID: "chat-1", UserID: "user-1", Text: "please do work"}
	reader := a.HandleMessageStream(context.Background(), msg)
	var got strings.Builder
	for {
		chunk, ok := reader.Next()
		if !ok {
			break
		}
		got.WriteString(chunk.Content)
	}
	if err := reader.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if !toolRan.Load() {
		t.Fatal("expected final-synthesis tool call to execute")
	}
	if got.String() != "final answer after tool" {
		t.Fatalf("unexpected streamed content %q", got.String())
	}

	sess := a.sessions.Get(sessionTriple(msg, msg.ProjectID))
	messages := sess.GetMessages()
	var sawToolResult bool
	for _, m := range messages {
		if m.Role == "tool" && m.ToolCallID == "call-final-tool" && strings.Contains(m.Content, "tool result ok") {
			sawToolResult = true
		}
	}
	if !sawToolResult {
		t.Fatalf("expected persisted tool result after final-synthesis tool call; messages=%+v", messages)
	}
}
