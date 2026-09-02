package sandbox

import (
	"context"
	"strings"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

func TestHydrateWorkspaceProjectChatLandsInSessionSubdir(t *testing.T) {
	ws := workspace.NewLocalFS(t.TempDir())
	ctx := context.Background()
	if err := ws.Put(ctx, "agt", "proj", "chat", "notes.md", strings.NewReader("hi"), 2, ""); err != nil {
		t.Fatal(err)
	}
	if err := ws.Put(ctx, "agt", "proj", "other", "sibling.md", strings.NewReader("no"), 2, ""); err != nil {
		t.Fatal(err)
	}
	ex := &fakeExecutor{}
	hydrateWorkspace(ctx, ws, ex, "agt", "proj", "chat", "/workspace")
	ex.mu.Lock()
	defer ex.mu.Unlock()
	if got := ex.writes["/workspace/notes.md"]; got != "hi" {
		t.Fatalf("chat file dest = %#v", ex.writes)
	}
	if _, ok := ex.writes["/workspace/chat/notes.md"]; ok {
		t.Fatal("store-prefix mount lands files at /workspace/<rel>, not /workspace/<sid>/")
	}
	if _, ok := ex.writes["/workspace/other/sibling.md"]; ok {
		t.Fatal("must not hydrate a sibling chat into this sandbox scope")
	}
}

func TestHydrateWorkspaceLooseChatStaysAtWorkspaceRoot(t *testing.T) {
	ws := workspace.NewLocalFS(t.TempDir())
	ctx := context.Background()
	if err := ws.Put(ctx, "agt", "", "chat", "notes.md", strings.NewReader("hi"), 2, ""); err != nil {
		t.Fatal(err)
	}
	ex := &fakeExecutor{}
	hydrateWorkspace(ctx, ws, ex, "agt", "", "chat", "/workspace")
	ex.mu.Lock()
	defer ex.mu.Unlock()
	if got := ex.writes["/workspace/notes.md"]; got != "hi" {
		t.Fatalf("loose dest = %#v", ex.writes)
	}
}

func TestHydrateWorkspaceCodingUsesProjectRoot(t *testing.T) {
	ws := workspace.NewLocalFS(t.TempDir())
	ctx := context.Background()
	if err := ws.Put(ctx, "agt", "proj", "", "app/src.ts", strings.NewReader("x"), 1, ""); err != nil {
		t.Fatal(err)
	}
	ex := &fakeExecutor{}
	hydrateWorkspace(ctx, ws, ex, "agt", "proj", "", "/workspace")
	ex.mu.Lock()
	defer ex.mu.Unlock()
	if got := ex.writes["/workspace/app/src.ts"]; got != "x" {
		t.Fatalf("coding dest = %#v", ex.writes)
	}
}
