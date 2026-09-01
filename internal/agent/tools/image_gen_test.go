package tools

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/toolproviders/imagegen"
	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

type imageArchiveStore struct {
	puts   []string
	dels   []string
	failAt int
}

func (s *imageArchiveStore) Put(ctx context.Context, agentID, projectID, sessionID, path string, r io.Reader, size int64, contentType string) error {
	if s.failAt > 0 && len(s.puts)+1 == s.failAt {
		return io.ErrClosedPipe
	}
	_, _ = io.Copy(io.Discard, r)
	s.puts = append(s.puts, agentID+":"+sessionID+":"+path+":"+contentType)
	return nil
}
func (s *imageArchiveStore) Get(context.Context, string, string, string, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}
func (s *imageArchiveStore) Stat(context.Context, string, string, string, string) (*workspace.ObjectInfo, error) {
	return nil, workspace.ErrNotFound
}
func (s *imageArchiveStore) List(context.Context, string, string, string) ([]workspace.ObjectInfo, error) {
	return nil, nil
}
func (s *imageArchiveStore) Delete(ctx context.Context, agentID, projectID, sessionID, path string) error {
	s.dels = append(s.dels, path)
	return nil
}
func (s *imageArchiveStore) Move(context.Context, string, string, string, string, string) error {
	return nil
}
func (s *imageArchiveStore) SignedURL(context.Context, string, string, string, string, time.Duration) (string, error) {
	return "", workspace.ErrSignedURLUnsupported
}

const onePxPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII="

func TestImageGenArchiveBase64WritesWorkspacePath(t *testing.T) {
	st := &imageArchiveStore{}
	r := NewRegistry(t.TempDir(), t.TempDir())
	r.SetWorkspaceStore(st, "agent-a")
	r.SetSessionID("sess-a")
	text, err := r.archiveImageGenOutput(context.Background(), imagegen.Output{Base64: []string{onePxPNG}})
	if err != nil {
		t.Fatal(err)
	}
	if len(st.puts) != 1 {
		t.Fatalf("want one put, got %#v", st.puts)
	}
	if !strings.Contains(st.puts[0], ":sess-a:generated-images/") {
		t.Fatalf("session-scoped generated-images put missing: %#v", st.puts)
	}
	if !strings.Contains(text, "](/workspace/generated-images/") {
		t.Fatalf("workspace display path missing: %s", text)
	}
}

func TestImageGenArchiveFetchesURLIntoSessionStore(t *testing.T) {
	png, err := decodeImageBase64(onePxPNG)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
	t.Cleanup(srv.Close)
	orig := archiveHTTPClient
	archiveHTTPClient = srv.Client()
	t.Cleanup(func() { archiveHTTPClient = orig })

	st := &imageArchiveStore{}
	r := NewRegistry(t.TempDir(), t.TempDir())
	r.SetWorkspaceStore(st, "agent-a")
	r.SetSessionID("chat-1")
	text, err := r.archiveImageGenOutput(context.Background(), imagegen.Output{URLs: []string{srv.URL + "/img.png"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(st.puts) != 1 || !strings.Contains(st.puts[0], ":chat-1:generated-images/") {
		t.Fatalf("url archive put missing: %#v", st.puts)
	}
	if !strings.Contains(text, "/workspace/generated-images/") {
		t.Fatalf("workspace path missing: %s", text)
	}
}

func TestImageGenArchiveRejectsBadInputsAndRollsBack(t *testing.T) {
	r := NewRegistry(t.TempDir(), t.TempDir())
	if _, err := r.archiveImageGenOutput(context.Background(), imagegen.Output{Base64: []string{onePxPNG}}); err == nil {
		t.Fatalf("missing workspace store should error")
	}
	st := &imageArchiveStore{}
	r.SetWorkspaceStore(st, "agent-a")
	r.SetSessionID("sess-a")
	if _, err := r.archiveImageGenOutput(context.Background(), imagegen.Output{Base64: []string{"not base64"}}); err == nil {
		t.Fatalf("bad base64 should error")
	}
	if _, err := r.archiveImageGenOutput(context.Background(), imagegen.Output{Base64: []string{"aGVsbG8="}}); err == nil {
		t.Fatalf("non-image should error")
	}

	st = &imageArchiveStore{failAt: 2}
	r.SetWorkspaceStore(st, "agent-a")
	if _, err := r.archiveImageGenOutput(context.Background(), imagegen.Output{Base64: []string{onePxPNG, onePxPNG}}); err == nil {
		t.Fatalf("put failure should error")
	}
	if len(st.dels) != 1 {
		t.Fatalf("rollback should delete first image, got %#v", st.dels)
	}
}
