package setup

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/auth"
	"github.com/fastclaw-ai/fastclaw/internal/store"
	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

type signedURLWorkspaceStore struct {
	signedURL string
	publicURL string
	getCalls  int
	lastGet   [4]string
}

func (s *signedURLWorkspaceStore) Put(context.Context, string, string, string, string, io.Reader, int64, string) error {
	return nil
}
func (s *signedURLWorkspaceStore) Get(_ context.Context, agentID, projectID, sessionID, path string) (io.ReadCloser, error) {
	s.getCalls++
	s.lastGet = [4]string{agentID, projectID, sessionID, path}
	return io.NopCloser(strings.NewReader("<html>ok</html>")), nil
}
func (s *signedURLWorkspaceStore) Stat(context.Context, string, string, string, string) (*workspace.ObjectInfo, error) {
	return nil, nil
}
func (s *signedURLWorkspaceStore) List(context.Context, string, string, string) ([]workspace.ObjectInfo, error) {
	return nil, nil
}
func (s *signedURLWorkspaceStore) Delete(context.Context, string, string, string, string) error {
	return nil
}
func (s *signedURLWorkspaceStore) Move(context.Context, string, string, string, string, string) error {
	return nil
}
func (s *signedURLWorkspaceStore) SignedURL(context.Context, string, string, string, string, time.Duration) (string, error) {
	return s.signedURL, nil
}
func (s *signedURLWorkspaceStore) PublicURL(context.Context, string, string, string, string) (string, error) {
	if s.publicURL != "" {
		return s.publicURL, nil
	}
	return "", workspace.ErrSignedURLUnsupported
}

func getAgentFile(t *testing.T, ctx context.Context, s *Server, resolver *auth.Resolver, userID, agentID, path, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := authTestRequest(t, ctx, resolver, http.MethodGet, "/api/agents/"+agentID+"/files/"+path+query, userID)
	req.SetPathValue("id", agentID)
	req.SetPathValue("path", path)
	rr := httptest.NewRecorder()
	s.authMiddleware(s.handleAgentFile)(rr, req)
	return rr
}

func TestServeFileFromWorkspaceStoreRedirectsNonHTMLToSignedURL(t *testing.T) {
	ctx := context.Background()
	s, resolver, owner, _ := newAuthTestServer(t, ctx)
	const agentID = "agt_signed"
	now := time.Now().UTC()
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{
		ID: agentID, UserID: owner.ID, Name: "signed",
		Config: map[string]any{}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	st := &signedURLWorkspaceStore{signedURL: "https://r2.example.test/bucket/image.png?X-Amz-Signature=abc"}
	s.SetWorkspaceStore(st)

	rr := getAgentFile(t, ctx, s, resolver, owner.ID, agentID, "sessions/s1/image.png", "")
	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%q", rr.Code, http.StatusFound, rr.Body.String())
	}
	if got := rr.Header().Get("Location"); got != st.signedURL {
		t.Fatalf("Location = %q, want %q", got, st.signedURL)
	}
	if st.getCalls != 0 {
		t.Fatalf("Get called %d times; signed-url redirect should avoid proxy read", st.getCalls)
	}
}

func TestServeFileFromWorkspaceStoreRedirectsNonHTMLToPublicURLWhenConfigured(t *testing.T) {
	ctx := context.Background()
	s, resolver, owner, _ := newAuthTestServer(t, ctx)
	const agentID = "agt_public"
	now := time.Now().UTC()
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{
		ID: agentID, UserID: owner.ID, Name: "public",
		Config: map[string]any{}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	st := &signedURLWorkspaceStore{publicURL: "https://cdn.example.test/fastclaw/agt_public/sessions/s1/image.png"}
	s.SetWorkspaceStore(st)

	rr := getAgentFile(t, ctx, s, resolver, owner.ID, agentID, "sessions/s1/image.png", "")
	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%q", rr.Code, http.StatusFound, rr.Body.String())
	}
	if got := rr.Header().Get("Location"); got != st.publicURL {
		t.Fatalf("Location = %q, want %q", got, st.publicURL)
	}
	if st.getCalls != 0 {
		t.Fatalf("Get called %d times; public-url redirect should avoid proxy read", st.getCalls)
	}
}

func TestServeFileFromWorkspaceStoreDoesNotRedirectHTMLToPublicURL(t *testing.T) {
	ctx := context.Background()
	s, resolver, owner, _ := newAuthTestServer(t, ctx)
	const agentID = "agt_html"
	now := time.Now().UTC()
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{
		ID: agentID, UserID: owner.ID, Name: "html",
		Config: map[string]any{}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	st := &signedURLWorkspaceStore{
		publicURL: "https://cdn.example.test/fastclaw/agt_html/sessions/s1/page.html",
		signedURL: "https://r2.example.test/bucket/page.html?X-Amz-Signature=abc",
	}
	s.SetWorkspaceStore(st)

	rr := getAgentFile(t, ctx, s, resolver, owner.ID, agentID, "sessions/s1/page.html", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", rr.Code, http.StatusOK, rr.Body.String())
	}
	if got := rr.Header().Get("Location"); got != "" {
		t.Fatalf("HTML should not redirect; Location = %q", got)
	}
	if got := rr.Header().Get("Content-Security-Policy"); got != "sandbox allow-scripts" {
		t.Fatalf("CSP = %q, want sandbox allow-scripts", got)
	}
	if st.getCalls != 1 {
		t.Fatalf("Get called %d times, want 1", st.getCalls)
	}
}

func TestServeFileRedirectUsesResolvedSessionNotURLPathGuess(t *testing.T) {
	ctx := context.Background()
	s, resolver, owner, _ := newAuthTestServer(t, ctx)
	const (
		agentID    = "agt_resolve"
		sessionKey = "s-url-token"
		chatID     = "web-chat-1"
	)
	now := time.Now().UTC()
	if err := s.dataStore.SaveAgent(ctx, &store.AgentRecord{
		ID: agentID, UserID: owner.ID, Name: "resolve",
		Config: map[string]any{}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.dataStore.SaveSession(ctx, owner.ID, agentID, sessionKey, &store.SessionRecord{
		Channel: "web", ChatID: chatID,
	}); err != nil {
		t.Fatal(err)
	}

	st := &signedURLWorkspaceStore{publicURL: "https://cdn.example.test/ok.png"}
	s.SetWorkspaceStore(st)

	rr := getAgentFile(t, ctx, s, resolver, owner.ID, agentID, "chart.png", "?sessionId="+sessionKey)
	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%q", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Location"); got != st.publicURL {
		t.Fatalf("Location = %q", got)
	}
}
