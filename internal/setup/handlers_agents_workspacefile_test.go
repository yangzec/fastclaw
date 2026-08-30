package setup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/store"
	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

// write_file("/workspace/chart.svg") and write_file("chart.svg") store the
// same session object. Chat markdown used to rewrite /workspace/X as
// sessions/<url-session-key>/X, which 404s when the store key is
// sessions/<chat_id>/X. The file GET must resolve a bare name (and a
// leftover workspace/ prefix) via ?sessionId=.
func TestAgentFileGetResolvesWorkspacePathViaSession(t *testing.T) {
	ctx := context.Background()
	s, resolver, owner, _ := newAuthTestServer(t, ctx)

	const (
		agentID    = "agt_wsfile"
		sessionKey = "s-url-token"
		chatID     = "web-chat-1"
	)
	now := time.Now().UTC()
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{
		ID: agentID, UserID: owner.ID, Name: "wsfile agent",
		Config: map[string]any{}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("save agent: %v", err)
	}
	if err := s.dataStore.SaveSession(ctx, owner.ID, agentID, sessionKey, &store.SessionRecord{
		Channel: "web", ChatID: chatID,
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	ws := workspace.NewLocalFS(t.TempDir())
	s.SetWorkspaceStore(ws)
	const body = "<svg xmlns='http://www.w3.org/2000/svg'></svg>"
	if err := ws.Put(ctx, agentID, "", chatID, "chart.svg", strings.NewReader(body), int64(len(body)), "image/svg+xml"); err != nil {
		t.Fatalf("put: %v", err)
	}

	get := func(path, query string) *httptest.ResponseRecorder {
		t.Helper()
		req := authTestRequest(t, ctx, resolver, http.MethodGet, "/api/agents/"+agentID+"/files/"+path+query, owner.ID)
		req.SetPathValue("id", agentID)
		req.SetPathValue("path", path)
		rr := httptest.NewRecorder()
		s.authMiddleware(s.handleAgentFile)(rr, req)
		return rr
	}

	for _, tc := range []struct {
		name  string
		path  string
		query string
	}{
		{"bare name + sessionId", "chart.svg", "?sessionId=" + sessionKey},
		{"workspace prefix + sessionId", "workspace/chart.svg", "?sessionId=" + sessionKey},
		{"full store key", "sessions/" + chatID + "/chart.svg", ""},
	} {
		rr := get(tc.path, tc.query)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: GET = %d: %s", tc.name, rr.Code, rr.Body.String())
		}
		if got := rr.Body.String(); got != body {
			t.Fatalf("%s: body = %q, want %q", tc.name, got, body)
		}
	}

	// A sessionId that does not resolve for this caller must not fall
	// back to treating the token as a chat_id (that was a cross-user leak).
	if rr := get("chart.svg", "?sessionId=someone-elses-chat"); rr.Code != http.StatusNotFound {
		t.Fatalf("foreign sessionId = %d, want 404", rr.Code)
	}
}
