package tools

import (
	"context"
	"io"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

// Unsandboxed exec must chdir into the session workspace so a relative
// write lands next to write_file — not in the gateway process cwd
// (often the git checkout).
func TestHostExecRelativeWriteLandsInSessionWorkspace(t *testing.T) {
	root := t.TempDir()
	ws := workspace.NewLocalFS(root)
	r := NewRegistry(t.TempDir(), t.TempDir())
	r.SetWorkspaceStore(ws, "agt_exec_cwd")
	r.SetSessionID("chat-exec")
	r.SetCallerIsAdmin(true)
	registerExecFull(r, nil, nil, nil)

	out, err := r.Execute(context.Background(), "exec", `{"command":"printf 'from-exec\\n' > report.md && pwd"}`)
	if err != nil {
		t.Fatalf("exec: %v (%s)", err, out)
	}

	rc, err := ws.Get(context.Background(), "agt_exec_cwd", "", "chat-exec", "report.md")
	if err != nil {
		t.Fatalf("store get report.md: %v\nexec output: %s", err, out)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if string(body) != "from-exec\n" {
		t.Fatalf("store body = %q, want from-exec\\n (exec out %q)", body, out)
	}
}

func TestHostExecRelativeWriteLandsInProjectChatWorkspace(t *testing.T) {
	ws := workspace.NewLocalFS(t.TempDir())
	r := NewRegistry(t.TempDir(), t.TempDir())
	r.SetWorkspaceStore(ws, "agt_exec_proj")
	r.SetProjectID("proj_a")
	r.SetSessionID("chat-a")
	r.SetCallerIsAdmin(true)
	registerExecFull(r, nil, nil, nil)

	if _, err := r.Execute(context.Background(), "exec", `{"command":"printf 'proj\\n' > note.txt"}`); err != nil {
		t.Fatalf("exec: %v", err)
	}
	rc, err := ws.Get(context.Background(), "agt_exec_proj", "proj_a", "chat-a", "note.txt")
	if err != nil {
		t.Fatalf("store get: %v", err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if string(body) != "proj\n" {
		t.Fatalf("body = %q", body)
	}
}
