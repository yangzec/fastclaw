package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDockerSnapshotWalksMountRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "mine.md"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "nested.md"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &DockerExecutor{sb: &DockerSandbox{workspace: root}}
	files, err := d.SnapshotWorkspace(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["mine.md"]; !ok {
		t.Fatalf("expected mine.md, got %#v", files)
	}
	if _, ok := files["docs/nested.md"]; !ok {
		t.Fatalf("expected docs/nested.md, got %#v", files)
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
