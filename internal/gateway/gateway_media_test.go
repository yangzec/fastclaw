package gateway

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestSplitMediaFromReplyAttachesAbsoluteLocalImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "generated.png")

	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatalf("encode image: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close image: %v", err)
	}

	text, items := splitMediaFromReply(context.Background(), nil, "agt", "", "chat", "生成好了：\n\n![图]("+path+")\n")
	if strings.Contains(text, path) || strings.Contains(text, "![图]") {
		t.Fatalf("image markdown was not stripped from text: %q", text)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 media item, got %d", len(items))
	}
	if items[0].Filename != "generated.png" {
		t.Fatalf("filename = %q", items[0].Filename)
	}
	if items[0].ContentType != "image/png" {
		t.Fatalf("content type = %q", items[0].ContentType)
	}
	if len(items[0].Bytes) == 0 {
		t.Fatal("media bytes empty")
	}
}

func TestSplitMediaFromReplyDoesNotAttachAbsoluteNonImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(path, []byte("not an image"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, items := splitMediaFromReply(context.Background(), nil, "agt", "", "chat", "![bad]("+path+")")
	if len(items) != 0 {
		t.Fatalf("expected no media items for non-image file, got %d", len(items))
	}
}

func TestSplitMediaFromReplyAttachesAllowlistedRemoteImage(t *testing.T) {
	var imageBytes strings.Builder
	imageBytes.WriteString("\x89PNG\r\n\x1a\nPNGDATA")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(imageBytes.String()))
	}))
	defer srv.Close()
	t.Setenv(remoteImageMediaHostsEnv, "127.0.0.1")

	text, items := splitMediaFromReply(context.Background(), nil, "agt", "", "chat", "生成好了：\n\n![图]("+srv.URL+"/img.png)\n")
	if strings.Contains(text, srv.URL) || strings.Contains(text, "![图]") {
		t.Fatalf("remote image markdown was not stripped from text: %q", text)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 media item, got %d", len(items))
	}
	if items[0].Filename != "img.png" {
		t.Fatalf("filename = %q", items[0].Filename)
	}
	if items[0].ContentType != "image/png" {
		t.Fatalf("content type = %q", items[0].ContentType)
	}
	if string(items[0].Bytes) != imageBytes.String() {
		t.Fatalf("media bytes = %q", string(items[0].Bytes))
	}
}

func TestSplitMediaFromReplyKeepsUnallowlistedRemoteImageLink(t *testing.T) {
	reply := "生成好了：\n\n![图](https://example.com/img.png)\n"
	text, items := splitMediaFromReply(context.Background(), nil, "agt", "", "chat", reply)
	if text != strings.TrimSpace(reply) {
		t.Fatalf("text = %q, want original markdown fallback", text)
	}
	if len(items) != 0 {
		t.Fatalf("expected no media items, got %d", len(items))
	}
}
