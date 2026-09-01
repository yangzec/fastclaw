package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTTSArchiveCopiesTempFileIntoSessionStore(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "fastclaw-tts-test.mp3")
	if err := os.WriteFile(tmp, []byte("ID3fake-audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := &imageArchiveStore{}
	r := NewRegistry(dir, dir)
	r.SetWorkspaceStore(st, "agent-tts")
	r.SetSessionID("sess-tts")

	text, err := r.archiveTTSOutput(context.Background(), "Generated audio: clip.mp3\nMEDIA:"+tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.puts) != 1 {
		t.Fatalf("want one put, got %#v", st.puts)
	}
	if !strings.Contains(st.puts[0], "agent-tts:sess-tts:generated-audio/") {
		t.Fatalf("session-scoped generated-audio put missing: %#v", st.puts)
	}
	if !strings.Contains(st.puts[0], ":audio/mpeg") {
		t.Fatalf("audio content type missing: %#v", st.puts)
	}
	if !strings.Contains(text, "MEDIA:"+tmp) {
		t.Fatalf("original MEDIA line should stay for IM attach: %s", text)
	}
	if !strings.Contains(text, "Workspace path: /workspace/generated-audio/") {
		t.Fatalf("workspace path missing: %s", text)
	}
}

func TestTTSArchiveNoStoreLeavesTextUnchanged(t *testing.T) {
	r := NewRegistry(t.TempDir(), t.TempDir())
	in := "Generated audio: clip.mp3\nMEDIA:/tmp/missing.mp3"
	got, err := r.archiveTTSOutput(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if got != in {
		t.Fatalf("got %q want %q", got, in)
	}
}
