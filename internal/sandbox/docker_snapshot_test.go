package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDockerSnapshotWalksOnlyChatSubdir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "chat-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "chat-b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "chat-a", "mine.md"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "chat-b", "other.md"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "root.md"), []byte("r"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := &DockerExecutor{sb: &DockerSandbox{workspace: root, workdir: "/workspace/chat-a"}}
	files, err := d.SnapshotWorkspace(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["mine.md"]; !ok {
		t.Fatalf("expected mine.md, got %#v", files)
	}
	if _, ok := files["other.md"]; ok {
		t.Fatal("sibling chat file leaked into this chat's snapshot")
	}
	if _, ok := files["root.md"]; ok {
		t.Fatal("project-root file leaked into this chat's snapshot")
	}
}

func TestDockerSnapshotCodingWalksProjectRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app", "src.ts"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &DockerExecutor{sb: &DockerSandbox{workspace: root, workdir: "/workspace"}}
	files, err := d.SnapshotWorkspace(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["app/src.ts"]; !ok {
		t.Fatalf("expected app/src.ts, got %#v", files)
	}
}
