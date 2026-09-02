package session

import (
	"context"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/provider"
)

type noopSessionStore struct{}

func (noopSessionStore) GetSession(context.Context, string, string) ([]provider.Message, error) {
	return nil, nil
}
func (noopSessionStore) SaveSession(context.Context, string, string, string, string, string, string, []provider.Message) error {
	return nil
}
func (noopSessionStore) AppendMessage(context.Context, string, string, provider.Message) error {
	return nil
}
func (noopSessionStore) ListMessages(context.Context, string, string) ([]provider.Message, error) {
	return nil, nil
}
func (noopSessionStore) ListWebSessions(context.Context, string) ([]WebSession, error) {
	return nil, nil
}
func (noopSessionStore) DeleteSession(context.Context, string, string) error {
	return nil
}
func (noopSessionStore) RenameSession(context.Context, string, string, string) error {
	return nil
}
func (noopSessionStore) MoveSession(context.Context, string, string, string) error {
	return nil
}
func (noopSessionStore) ResolveActiveSessionKey(context.Context, string, string, string, string) (string, error) {
	return "", nil
}
func (noopSessionStore) LookupSessionTriple(context.Context, string, string) (string, string, string, error) {
	return "", "", "", nil
}
func (noopSessionStore) LookupSessionProject(context.Context, string, string) (string, error) {
	return "", nil
}

func TestTurnActiveFollowsBeginAndEnd(t *testing.T) {
	mgr := NewManagerWithStoreForUser(t.TempDir(), noopSessionStore{}, "u1", "agt-1")
	s := mgr.Get("web", "", "chat-1", "")
	if s.TurnActive() {
		t.Fatal("new session should not have an active turn")
	}
	s.BeginTurn()
	if !s.TurnActive() {
		t.Fatal("BeginTurn should mark the session active")
	}
	s.BeginTurn()
	if leftover := s.EndTurn(); leftover != nil {
		t.Fatalf("nested EndTurn leftover = %v, want nil", leftover)
	}
	if !s.TurnActive() {
		t.Fatal("turn should stay active while depth > 0")
	}
	if leftover := s.EndTurn(); leftover != nil {
		t.Fatalf("final EndTurn leftover = %v, want nil", leftover)
	}
	if s.TurnActive() {
		t.Fatal("EndTurn should clear the active flag")
	}
}

func TestNewManagerWithStoreForUserEmptyUserIDDoesNotPanic(t *testing.T) {
	mgr := NewManagerWithStoreForUser(t.TempDir(), noopSessionStore{}, "", "agent-1")
	if mgr == nil {
		t.Fatal("expected manager")
	}
	s := mgr.Get("web", "", "chat-1", "")
	if s == nil {
		t.Fatal("expected session")
	}
	s.Append(provider.Message{Role: "user", Content: "hello"})
	if got := len(s.GetMessages()); got != 1 {
		t.Fatalf("message count = %d, want 1", got)
	}
}

// /v1/chat/completions calls Get() to echo session_id, then HandleMessage
// calls Get() again. Without a store hit (save is on first Append), the
// second Get must reuse the in-memory key instead of minting another s-...
func TestGetReusesInMemoryKeyBeforeStorePersist(t *testing.T) {
	mgr := NewManagerWithStoreForUser(t.TempDir(), noopSessionStore{}, "u1", "agt-1")
	first := mgr.Get("api", "", "app:user:conv", "")
	second := mgr.Get("api", "", "app:user:conv", "")
	if first == nil || second == nil {
		t.Fatal("expected sessions")
	}
	if first.SessionKey() == "" || first.SessionKey() != second.SessionKey() {
		t.Fatalf("keys %q then %q, want the same minted s-...", first.SessionKey(), second.SessionKey())
	}
	if len(first.SessionKey()) < 3 || first.SessionKey()[:2] != "s-" {
		t.Fatalf("expected native session_id, got %q", first.SessionKey())
	}
}
