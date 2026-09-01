package setup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/auth"
	"github.com/fastclaw-ai/fastclaw/internal/store"
	"github.com/fastclaw-ai/fastclaw/internal/users"
)

func TestChatSubscribeRejectsForeignSession(t *testing.T) {
	ctx := context.Background()
	s, resolver, owner, viewer := newAuthTestServer(t, ctx)

	const (
		agentID    = "agt_sub_iso"
		sessionKey = "s-owner-live"
	)
	now := time.Now().UTC()
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{
		ID: agentID, UserID: owner.ID, Name: "sub iso",
		IsPublic: true, Config: map[string]any{}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("save agent: %v", err)
	}
	if err := s.dataStore.SaveSession(ctx, owner.ID, agentID, sessionKey, &store.SessionRecord{
		Channel: "web", ChatID: sessionKey,
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	sub := func(userID string) *httptest.ResponseRecorder {
		t.Helper()
		req := authTestRequest(t, ctx, resolver, http.MethodGet,
			"/api/chat/subscribe?agentId="+agentID+"&sessionId="+sessionKey, userID)
		rr := httptest.NewRecorder()
		s.authMiddleware(s.handleChatSubscribe)(rr, req)
		return rr
	}

	if rr := sub(viewer.ID); rr.Code != http.StatusNotFound {
		t.Fatalf("viewer subscribe = %d, want 404: %s", rr.Code, rr.Body.String())
	}

	newReq := httptest.NewRequest(http.MethodGet, "/", nil)
	newReq = newReq.WithContext(auth.WithIdentity(newReq.Context(), auth.Identity{UserID: viewer.ID, Role: users.RoleUser}))
	if !s.canSubscribeSession(newReq, agentID, "s-brand-new") {
		t.Fatal("a session id with no row yet must still be subscribable")
	}
	ownReq := httptest.NewRequest(http.MethodGet, "/", nil)
	ownReq = ownReq.WithContext(auth.WithIdentity(ownReq.Context(), auth.Identity{UserID: owner.ID, Role: users.RoleSuperAdmin}))
	if !s.canSubscribeSession(ownReq, agentID, sessionKey) {
		t.Fatal("owner must be able to subscribe to their own session")
	}
}
