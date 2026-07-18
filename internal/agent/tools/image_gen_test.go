package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/toolproviders/imagegen"
	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

type imageArchiveStore struct {
	puts      []string
	dels      []string
	failAt    int
	publicURL string
}

func (s *imageArchiveStore) Put(ctx context.Context, agentID, projectID, sessionID, path string, r io.Reader, size int64, contentType string) error {
	if s.failAt > 0 && len(s.puts)+1 == s.failAt {
		return fmt.Errorf("put failed")
	}
	s.puts = append(s.puts, agentID+":"+sessionID+":"+path+":"+contentType)
	return nil
}
func (s *imageArchiveStore) Get(context.Context, string, string, string, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}
func (s *imageArchiveStore) Stat(context.Context, string, string, string, string) (*workspace.ObjectInfo, error) {
	return nil, nil
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
	return "", nil
}
func (s *imageArchiveStore) PublicURL(ctx context.Context, agentID, projectID, sessionID, path string) (string, error) {
	if s.publicURL == "" {
		return "", workspace.ErrSignedURLUnsupported
	}
	return s.publicURL + "/" + path, nil
}

const onePxPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII="

func TestImageGenArchiveBase64WritesStableURL(t *testing.T) {
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
	if !bytes.Contains([]byte(text), []byte("/api/agents/agent-a/files/sessions/sess-a/generated-images/")) {
		t.Fatalf("stable URL missing: %s", text)
	}
}

func TestImageGenArchiveUsesPublicBaseURLWhenConfigured(t *testing.T) {
	st := &imageArchiveStore{publicURL: "https://cdn.example.test/fastclaw/agent-a/sessions/sess-a"}
	r := NewRegistry(t.TempDir(), t.TempDir())
	r.SetWorkspaceStore(st, "agent-a")
	r.SetSessionID("sess-a")
	text, err := r.archiveImageGenOutput(context.Background(), imagegen.Output{Base64: []string{onePxPNG}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(text), []byte("https://cdn.example.test/fastclaw/agent-a/sessions/sess-a/generated-images/")) {
		t.Fatalf("public URL missing: %s", text)
	}
	if bytes.Contains([]byte(text), []byte("/api/agents/agent-a/files/sessions/sess-a/")) {
		t.Fatalf("public URL configured but internal proxy URL was returned: %s", text)
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
