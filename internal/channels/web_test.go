package channels

import (
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/bus"
)

func TestWebChannelSubscribeIsUserScoped(t *testing.T) {
	w := NewWebChannel()
	alice, unsubAlice := w.Subscribe("u_alice", "agt_1", "s-shared")
	defer unsubAlice()
	bob, unsubBob := w.Subscribe("u_bob", "agt_1", "s-shared")
	defer unsubBob()

	if err := w.SendMessage(bus.OutboundMessage{
		UserID:  "u_alice",
		AgentID: "agt_1",
		ChatID:  "s-shared",
		Text:    "for alice",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}

	select {
	case msg := <-alice:
		if msg.Text != "for alice" {
			t.Fatalf("alice got %q", msg.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("alice did not receive her message")
	}
	select {
	case msg := <-bob:
		t.Fatalf("bob received alice's message: %q", msg.Text)
	case <-time.After(50 * time.Millisecond):
	}

	if err := w.SendMessage(bus.OutboundMessage{
		AgentID: "agt_1",
		ChatID:  "s-shared",
		Text:    "no user",
	}); err != nil {
		t.Fatalf("send empty user: %v", err)
	}
	select {
	case msg := <-alice:
		t.Fatalf("alice received unscoped message: %q", msg.Text)
	case msg := <-bob:
		t.Fatalf("bob received unscoped message: %q", msg.Text)
	case <-time.After(50 * time.Millisecond):
	}
}
