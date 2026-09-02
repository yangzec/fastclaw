package gateway

import (
	"bytes"
	"context"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/workspace"
)

func TestAppendNewWorkspaceMediaSkipsHistoricalFiles(t *testing.T) {
	ctx := context.Background()
	ws := workspace.NewLocalFS(t.TempDir())
	put := func(name, body string) {
		t.Helper()
		if err := ws.Put(ctx, "agent", "", "chat", name, bytes.NewBufferString(body), int64(len(body)), "application/octet-stream"); err != nil {
			t.Fatal(err)
		}
	}

	put("old.pdf", "old")
	before, ok := snapshotWorkspacePaths(ctx, ws, "agent", "", "chat")
	if !ok {
		t.Fatal("snapshot failed")
	}

	// Rewriting an old artifact simulates sandbox sync refreshing its mtime.
	put("old.pdf", "old")
	put("current.pdf", "current")

	got := appendNewWorkspaceMedia(ctx, ws, "agent", "", "chat", before, ok, nil)
	if len(got) != 1 || got[0].Filename != "current.pdf" || string(got[0].Bytes) != "current" {
		t.Fatalf("got %#v, want only current.pdf", got)
	}
}

func TestAppendNewWorkspaceMediaRequiresSuccessfulSnapshot(t *testing.T) {
	ctx := context.Background()
	ws := workspace.NewLocalFS(t.TempDir())
	if err := ws.Put(ctx, "agent", "", "chat", "old.pdf", bytes.NewBufferString("old"), 3, "application/pdf"); err != nil {
		t.Fatal(err)
	}

	got := appendNewWorkspaceMedia(ctx, ws, "agent", "", "chat", nil, false, nil)
	if len(got) != 0 {
		t.Fatalf("got %d attachments after failed snapshot, want 0", len(got))
	}
}

func TestSplitFilesFromReplyUsesExplicitFinalDocument(t *testing.T) {
	ctx := context.Background()
	ws := workspace.NewLocalFS(t.TempDir())
	for name, body := range map[string]string{"draft.pdf": "draft", "final.pdf": "final"} {
		if err := ws.Put(ctx, "agent", "", "chat", name, bytes.NewBufferString(body), int64(len(body)), "application/pdf"); err != nil {
			t.Fatal(err)
		}
	}

	text, items := splitFilesFromReply(ctx, ws, "agent", "", "chat", "已完成：[最终报告](/workspace/final.pdf)")
	if text != "已完成：" {
		t.Fatalf("text = %q", text)
	}
	if len(items) != 1 || items[0].Filename != "final.pdf" || string(items[0].Bytes) != "final" {
		t.Fatalf("items = %#v, want only final.pdf", items)
	}
}

func TestSplitFilesFromReplyUsesCodingProjectRoot(t *testing.T) {
	ctx := context.Background()
	ws := workspace.NewLocalFS(t.TempDir())
	if err := ws.Put(ctx, "agent", "proj", "", "final.pdf", bytes.NewBufferString("root"), 4, "application/pdf"); err != nil {
		t.Fatal(err)
	}
	if err := ws.Put(ctx, "agent", "proj", "chat", "final.pdf", bytes.NewBufferString("chat"), 4, "application/pdf"); err != nil {
		t.Fatal(err)
	}

	_, items := splitFilesFromReply(ctx, ws, "agent", "proj", "", "见 [报告](/workspace/final.pdf)")
	if len(items) != 1 || string(items[0].Bytes) != "root" {
		t.Fatalf("coding store session must read project root, got %#v", items)
	}
}

type publicURLFS struct {
	*workspace.LocalFS
	base string
}

func (s publicURLFS) PublicURL(_ context.Context, _, _, _, path string) (string, error) {
	return s.base + "/" + path, nil
}

func TestSplitFilesFromReplyResolvesR2PublicURL(t *testing.T) {
	ctx := context.Background()
	inner := workspace.NewLocalFS(t.TempDir())
	ws := publicURLFS{LocalFS: inner, base: "https://cdn.example.test/fastclaw"}
	if err := ws.Put(ctx, "agent", "", "chat", "final.pdf", bytes.NewBufferString("pdf-bytes"), 9, "application/pdf"); err != nil {
		t.Fatal(err)
	}

	text, items := splitFilesFromReply(ctx, ws, "agent", "", "chat", "完成：[报告](https://cdn.example.test/fastclaw/final.pdf)")
	if text != "完成：" {
		t.Fatalf("text = %q", text)
	}
	if len(items) != 1 || items[0].Filename != "final.pdf" || string(items[0].Bytes) != "pdf-bytes" {
		t.Fatalf("items = %#v", items)
	}
	if items[0].URL != "https://cdn.example.test/fastclaw/final.pdf" {
		t.Fatalf("public URL = %q", items[0].URL)
	}
}

func TestSplitMediaFromReplyResolvesR2PublicURL(t *testing.T) {
	ctx := context.Background()
	inner := workspace.NewLocalFS(t.TempDir())
	ws := publicURLFS{LocalFS: inner, base: "https://cdn.example.test/fastclaw"}
	if err := ws.Put(ctx, "agent", "", "chat", "shot.png", bytes.NewBufferString("png-bytes"), 9, "image/png"); err != nil {
		t.Fatal(err)
	}

	text, items := splitMediaFromReply(ctx, ws, "agent", "", "chat", "见图：![shot](https://cdn.example.test/fastclaw/shot.png)")
	if text != "见图：" {
		t.Fatalf("text = %q", text)
	}
	if len(items) != 1 || items[0].Filename != "shot.png" || string(items[0].Bytes) != "png-bytes" {
		t.Fatalf("items = %#v", items)
	}
}

func TestSplitFilesFromReplyLeavesForeignHTTPLinks(t *testing.T) {
	ctx := context.Background()
	ws := workspace.NewLocalFS(t.TempDir())
	text, items := splitFilesFromReply(ctx, ws, "agent", "", "chat", "见 [外链](https://example.com/a.pdf)")
	if text != "见 [外链](https://example.com/a.pdf)" || len(items) != 0 {
		t.Fatalf("text=%q items=%#v", text, items)
	}
}
