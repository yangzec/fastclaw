package channels

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/bus"
)

func TestHydrateMediaPathsReadsHostFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "clip.mp3")
	if err := os.WriteFile(p, []byte("audio-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := bus.OutboundMessage{MediaPaths: []string{p, "/no/such/file.mp3"}}
	hydrateMediaPaths(&msg)
	if len(msg.MediaPaths) != 0 {
		t.Fatalf("MediaPaths should be cleared, got %#v", msg.MediaPaths)
	}
	if len(msg.MediaItems) != 1 || msg.MediaItems[0].Filename != "clip.mp3" || string(msg.MediaItems[0].Bytes) != "audio-bytes" {
		t.Fatalf("MediaItems = %#v", msg.MediaItems)
	}
}
