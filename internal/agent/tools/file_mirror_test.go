package tools

import (
	"context"
	"testing"
	"time"

	"github.com/fastclaw-ai/fastclaw/internal/sandbox"
)

type recExec struct {
	writes []string
}

func (e *recExec) Exec(context.Context, string, time.Duration) (string, error) { return "", nil }
func (e *recExec) ReadFile(context.Context, string) (string, error)            { return "", nil }
func (e *recExec) WriteFile(_ context.Context, path, _ string) (string, error) {
	e.writes = append(e.writes, path)
	return "", nil
}
func (e *recExec) ListDir(context.Context, string) (string, error) { return "", nil }
func (e *recExec) Backend() string                                 { return "fake" }
func (e *recExec) Close() error                                    { return nil }

var _ sandbox.Executor = (*recExec)(nil)

func TestMirrorWorkspaceFileToSandboxProjectChat(t *testing.T) {
	rec := &recExec{}
	r := NewRegistry(t.TempDir(), t.TempDir())
	r.SetProjectID("proj")
	r.SetSessionID("chat")
	r.executor = rec
	r.mirrorWorkspaceFileToSandbox(context.Background(), "notes.md", "hi")
	if len(rec.writes) != 1 || rec.writes[0] != "/workspace/chat/notes.md" {
		t.Fatalf("got %#v, want /workspace/chat/notes.md", rec.writes)
	}
}

func TestMirrorWorkspaceFileToSandboxLooseAndCoding(t *testing.T) {
	rec := &recExec{}
	r := NewRegistry(t.TempDir(), t.TempDir())
	r.SetSessionID("chat")
	r.executor = rec
	r.mirrorWorkspaceFileToSandbox(context.Background(), "/workspace/notes.md", "hi")
	if len(rec.writes) != 1 || rec.writes[0] != "/workspace/notes.md" {
		t.Fatalf("loose: %#v", rec.writes)
	}

	rec.writes = nil
	r.SetProjectID("proj")
	r.SetCodingRootScope(true)
	r.mirrorWorkspaceFileToSandbox(context.Background(), "app/src.ts", "x")
	if len(rec.writes) != 1 || rec.writes[0] != "/workspace/app/src.ts" {
		t.Fatalf("coding: %#v", rec.writes)
	}
}
