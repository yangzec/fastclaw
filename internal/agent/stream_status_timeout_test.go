package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/provider"
)

type idleStreamProvider struct{}

func (idleStreamProvider) Chat(context.Context, []provider.Message, []provider.Tool, string, int, float64) (*provider.Response, error) {
	return nil, errors.New("unexpected Chat call")
}

func (idleStreamProvider) ChatStream(context.Context, []provider.Message, []provider.Tool, string, int, float64) (*provider.StreamReader, error) {
	return provider.NewStreamReader(make(chan provider.StreamChunk)), nil
}

func TestStreamChatToResponseEmitsThinkingStatusBeforeWaitingForFirstChunk(t *testing.T) {
	events := make(chan ChatEvent, 4)
	ctx := ContextWithChatEvents(context.Background(), events)
	a := &Agent{provider: idleStreamProvider{}, model: "test-model", maxTokens: 16, temperature: 0}
	oldTimeout := modelFirstChunkIdleTimeout
	modelFirstChunkIdleTimeout = 20 * time.Millisecond
	defer func() { modelFirstChunkIdleTimeout = oldTimeout }()

	done := make(chan error, 1)
	go func() {
		_, err := a.streamChatToResponseWithOptions(ctx, []provider.Message{{Role: "user", Content: "hi"}}, nil, true)
		done <- err
	}()

	select {
	case evt := <-events:
		if evt.Type != "status" {
			t.Fatalf("first event type = %q, want status", evt.Type)
		}
		if phase, _ := evt.Data["phase"].(string); phase != "thinking" {
			t.Fatalf("status phase = %v, want thinking", evt.Data["phase"])
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for status event before first model chunk")
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("streamChatToResponseWithOptions err = nil, want idle timeout")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("streamChatToResponseWithOptions did not return on idle stream")
	}
}
