package session

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/provider"
)

func um(s string) provider.Message {
	return provider.Message{Role: "user", Content: s}
}

func TestPushSteerRequiresActiveTurn(t *testing.T) {
	s := &Session{}

	if s.PushSteerIfActive(um("a")) {
		t.Fatal("push before BeginTurn should return false")
	}
	if got := s.DrainSteer(); got != nil {
		t.Fatalf("expected empty drain, got %v", got)
	}

	s.BeginTurn()
	if !s.PushSteerIfActive(um("b")) {
		t.Fatal("push during active turn should return true")
	}
	drained := s.DrainSteer()
	if len(drained) != 1 || drained[0].Content != "b" {
		t.Fatalf("unexpected drain: %v", drained)
	}
	if got := s.DrainSteer(); got != nil {
		t.Fatalf("second drain should be empty, got %v", got)
	}
}

func TestEndTurnReturnsLeftoverAndClosesWindow(t *testing.T) {
	s := &Session{}
	s.BeginTurn()
	s.PushSteerIfActive(um("x")) // never drained — end-of-turn race

	leftover := s.EndTurn()
	if len(leftover) != 1 || leftover[0].Content != "x" {
		t.Fatalf("EndTurn should return the undrained message, got %v", leftover)
	}
	if s.PushSteerIfActive(um("y")) {
		t.Fatal("push after the last EndTurn should return false")
	}
	if got := s.EndTurn(); got != nil {
		t.Fatalf("EndTurn with no active turn should return nil, got %v", got)
	}
}

func TestNestedTurnsDoNotStrandActiveFlag(t *testing.T) {
	s := &Session{}
	s.BeginTurn()
	s.BeginTurn() // re-entrant (redispatch / sub-flow)

	if l := s.EndTurn(); l != nil {
		t.Fatalf("inner EndTurn must not flush while outer turn active, got %v", l)
	}
	if !s.PushSteerIfActive(um("z")) {
		t.Fatal("turn still active after inner EndTurn — push should succeed")
	}
	if l := s.EndTurn(); len(l) != 1 || l[0].Content != "z" {
		t.Fatalf("outer EndTurn should flush leftover, got %v", l)
	}
}

// Concurrent producers vs. a drain loop: every accepted message is
// delivered exactly once (no loss, no duplication) under -race.
func TestConcurrentPushDrainNoLossNoDup(t *testing.T) {
	s := &Session{}
	s.BeginTurn()

	const producers = 8
	const perProducer = 200

	var got int64
	var wg sync.WaitGroup
	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perProducer; i++ {
				s.PushSteerIfActive(provider.Message{Role: "user", Content: "m"})
			}
		}()
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			atomic.AddInt64(&got, int64(len(s.DrainSteer())))
			select {
			case <-stop:
				// Final sweep after producers are guaranteed done.
				atomic.AddInt64(&got, int64(len(s.DrainSteer())))
				return
			default:
			}
		}
	}()

	wg.Wait()
	close(stop)
	<-done
	got += int64(len(s.EndTurn())) // sweep the post-last-drain window

	if got != producers*perProducer {
		t.Fatalf("message count mismatch: got %d want %d", got, producers*perProducer)
	}
}

func TestPushSteerInterruptsBoundLLM(t *testing.T) {
	s := &Session{}
	s.BeginTurn()
	ctx, cancel := context.WithCancel(context.Background())
	s.BindLLMCancel(cancel)

	if !s.PushSteerIfActive(um("turn around")) {
		t.Fatal("push during active turn should return true")
	}
	if ctx.Err() == nil {
		t.Fatal("steer should cancel the bound LLM context")
	}
	drained := s.DrainSteer()
	if len(drained) != 1 || drained[0].Content != "turn around" {
		t.Fatalf("steer should stay buffered after interrupt, got %v", drained)
	}

	s.BindLLMCancel(nil)
	s.EndTurn()
}

func TestPushSteerCancelDoesNotDeadlockUnbind(t *testing.T) {
	s := &Session{}
	s.BeginTurn()
	_, cancel := context.WithCancel(context.Background())
	s.BindLLMCancel(func() {
		cancel()
		s.BindLLMCancel(nil)
	})

	done := make(chan struct{})
	go func() {
		s.PushSteerIfActive(um("x"))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("PushSteerIfActive deadlocked against BindLLMCancel")
	}
	s.EndTurn()
}
