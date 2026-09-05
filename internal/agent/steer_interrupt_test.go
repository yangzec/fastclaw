package agent

import (
	"context"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/provider"
	"github.com/fastclaw-ai/fastclaw/internal/session"
)

func testSession(t *testing.T) *session.Session {
	t.Helper()
	s := &session.Session{}
	s.BeginTurn()
	return s
}

func TestFoldSteerInterruptPersistsPartialAndRedirects(t *testing.T) {
	a := &Agent{name: "t"}
	sess := testSession(t)
	if !sess.PushSteerIfActive(provider.Message{Role: "user", Content: "stop, count down instead"}) {
		t.Fatal("expected active turn")
	}
	resp := &provider.Response{Content: "I was listing fruits..."}
	ok, msgs := a.foldSteerInterrupt(context.Background(), sess, nil, resp)
	if !ok {
		t.Fatal("expected redirect")
	}
	if len(msgs) != 2 {
		t.Fatalf("want assistant + steer, got %#v", msgs)
	}
	if msgs[0].Role != "assistant" || msgs[0].Content != resp.Content {
		t.Fatalf("partial assistant: %#v", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content != "stop, count down instead" {
		t.Fatalf("steer: %#v", msgs[1])
	}
	if got := sess.DrainSteer(); got != nil {
		t.Fatalf("queue should be empty, got %v", got)
	}
}

func TestFoldSteerInterruptDropsIncompleteToolCalls(t *testing.T) {
	a := &Agent{name: "t"}
	sess := testSession(t)
	sess.PushSteerIfActive(provider.Message{Role: "user", Content: "never mind"})
	resp := &provider.Response{
		Content:      "calling search",
		ToolCalls:    []provider.ToolCall{{ID: "c1", Function: provider.FunctionCall{Name: "web_search"}}},
		RawAssistant: []byte(`{"incomplete":true}`),
	}
	ok, msgs := a.foldSteerInterrupt(context.Background(), sess, nil, resp)
	if !ok {
		t.Fatal("expected redirect")
	}
	if msgs[0].RawAssistant != nil {
		t.Fatalf("incomplete RawAssistant should be dropped, got %s", msgs[0].RawAssistant)
	}
	if len(msgs[0].ToolCalls) != 0 {
		t.Fatalf("tool calls should not be replayed, got %#v", msgs[0].ToolCalls)
	}
}

func TestFoldSteerInterruptSkipsWhenTurnStopped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := &Agent{name: "t"}
	sess := testSession(t)
	sess.PushSteerIfActive(provider.Message{Role: "user", Content: "redirect"})
	ok, _ := a.foldSteerInterrupt(ctx, sess, nil, &provider.Response{Content: "partial"})
	if ok {
		t.Fatal("Stop must not fold steer into a new completion")
	}
	if got := sess.DrainSteer(); len(got) != 1 {
		t.Fatalf("steer should remain for EndTurn parking, got %v", got)
	}
}

func TestFoldSteerInterruptNoOpWithoutQueue(t *testing.T) {
	a := &Agent{name: "t"}
	sess := testSession(t)
	ok, msgs := a.foldSteerInterrupt(context.Background(), sess, nil, &provider.Response{Content: "done"})
	if ok || msgs != nil {
		t.Fatalf("empty queue should not redirect, ok=%v msgs=%#v", ok, msgs)
	}
}
