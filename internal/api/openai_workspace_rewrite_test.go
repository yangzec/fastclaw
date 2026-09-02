package api

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

type apiPublicURLStore struct{}

func (s apiPublicURLStore) Put(context.Context, string, string, string, string, io.Reader, int64, string) error {
	return nil
}
func (s apiPublicURLStore) Get(context.Context, string, string, string, string) (io.ReadCloser, error) {
	return nil, workspace.ErrNotFound
}
func (s apiPublicURLStore) Stat(context.Context, string, string, string, string) (*workspace.ObjectInfo, error) {
	return nil, workspace.ErrNotFound
}
func (s apiPublicURLStore) List(context.Context, string, string, string) ([]workspace.ObjectInfo, error) {
	return nil, nil
}
func (s apiPublicURLStore) Delete(context.Context, string, string, string, string) error { return nil }
func (s apiPublicURLStore) Move(context.Context, string, string, string, string, string) error {
	return nil
}
func (s apiPublicURLStore) SignedURL(context.Context, string, string, string, string, time.Duration) (string, error) {
	return "", workspace.ErrSignedURLUnsupported
}
func (s apiPublicURLStore) PublicURL(ctx context.Context, agentID, projectID, sessionID, path string) (string, error) {
	return "https://cdn.example.test/fastclaw/" + agentID + "/sessions/" + sessionID + "/" + path, nil
}

func TestRewriteWorkspaceURLsToPublicRewritesRelativeAndAbsolutizedWorkspaceLinks(t *testing.T) {
	input := "已完成：[打开](/workspace/report.html)\n也可能是 https://www-agents.com/workspace/report.html"
	got := rewriteWorkspaceURLsToPublic(context.Background(), apiPublicURLStore{}, "agt_123", "", "chat-456", input)
	want := "https://cdn.example.test/fastclaw/agt_123/sessions/chat-456/report.html"
	if strings.Count(got, want) != 2 {
		t.Fatalf("rewrite result = %q, want two public URLs %q", got, want)
	}
	if strings.Contains(got, "www-agents.com/workspace") || strings.Contains(got, "](/workspace/") {
		t.Fatalf("workspace URL leaked after rewrite: %q", got)
	}
}

func TestWorkspaceURLStreamRewriterRewritesSplitWorkspaceURL(t *testing.T) {
	rewriter := newWorkspaceURLStreamRewriter(context.Background(), apiPublicURLStore{}, "agt_123", "", "chat-456")

	if got := rewriter.Write("已完成：[打开](/work"); got != "已完成：[打开](" {
		t.Fatalf("first streamed content = %q", got)
	}

	const publicURL = "https://cdn.example.test/fastclaw/agt_123/sessions/chat-456/report.html"
	if got := rewriter.Write("space/report.html)\n"); got != publicURL+")\n" {
		t.Fatalf("rewritten streamed content = %q, want %q", got, publicURL+")\n")
	}
	if got := rewriter.Flush(); got != "" {
		t.Fatalf("flush = %q, want empty", got)
	}
}
