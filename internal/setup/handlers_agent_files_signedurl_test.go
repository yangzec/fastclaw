package setup

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

type signedURLWorkspaceStore struct {
	signedURL string
	publicURL string
	getCalls  int
}

func (s *signedURLWorkspaceStore) Put(context.Context, string, string, string, string, io.Reader, int64, string) error {
	return nil
}
func (s *signedURLWorkspaceStore) Get(context.Context, string, string, string, string) (io.ReadCloser, error) {
	s.getCalls++
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

func TestScopedWorkspaceFilePathUsesResolvedSessionScope(t *testing.T) {
	got := scopedWorkspaceFilePath("preview.html", "", "chat-123")
	if got != "sessions/chat-123/preview.html" {
		t.Fatalf("path = %q, want sessions/chat-123/preview.html", got)
	}
}

func TestScopedWorkspaceFilePathUsesResolvedProjectAndSessionScope(t *testing.T) {
	got := scopedWorkspaceFilePath("src/index.html", "proj-1", "chat-123")
	if got != "projects/proj-1/chat-123/src/index.html" {
		t.Fatalf("path = %q, want projects/proj-1/chat-123/src/index.html", got)
	}
}

func TestServeFileFromWorkspaceStoreRedirectsNonHTMLToSignedURL(t *testing.T) {
	st := &signedURLWorkspaceStore{signedURL: "https://r2.example.test/bucket/image.png?X-Amz-Signature=abc"}
	s := NewServer(0)
	s.SetWorkspaceStore(st)

	req := httptest.NewRequest(http.MethodGet, "/api/agents/agent-a/files/sessions/s1/image.png", nil)
	rr := httptest.NewRecorder()
	s.serveFileFromWorkspaceStore(rr, req, "agent-a", "sessions/s1/image.png")

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
	st := &signedURLWorkspaceStore{publicURL: "https://cdn.example.test/fastclaw/agent-a/sessions/s1/image.png"}
	s := NewServer(0)
	s.SetWorkspaceStore(st)

	req := httptest.NewRequest(http.MethodGet, "/api/agents/agent-a/files/sessions/s1/image.png", nil)
	rr := httptest.NewRecorder()
	s.serveFileFromWorkspaceStore(rr, req, "agent-a", "sessions/s1/image.png")

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
	st := &signedURLWorkspaceStore{publicURL: "https://cdn.example.test/fastclaw/agent-a/sessions/s1/page.html", signedURL: "https://r2.example.test/bucket/page.html?X-Amz-Signature=abc"}
	s := NewServer(0)
	s.SetWorkspaceStore(st)

	req := httptest.NewRequest(http.MethodGet, "/api/agents/agent-a/files/sessions/s1/page.html", nil)
	rr := httptest.NewRecorder()
	s.serveFileFromWorkspaceStore(rr, req, "agent-a", "sessions/s1/page.html")

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
