package sandbox

import "testing"

func TestChatSandboxDir(t *testing.T) {
	cases := []struct {
		pid, sid, want string
	}{
		{"", "chat", "/workspace"},
		{"proj", "", "/workspace"},
		{"proj", "chat", "/workspace"},
		{"", "", "/workspace"},
	}
	for _, tc := range cases {
		if got := ChatSandboxDir(tc.pid, tc.sid); got != tc.want {
			t.Fatalf("ChatSandboxDir(%q,%q)=%q want %q", tc.pid, tc.sid, got, tc.want)
		}
	}
}

func TestChatSandboxFile(t *testing.T) {
	if got := ChatSandboxFile("proj", "chat", "notes.md"); got != "/workspace/notes.md" {
		t.Fatalf("got %q", got)
	}
	if got := ChatSandboxFile("", "chat", "notes.md"); got != "/workspace/notes.md" {
		t.Fatalf("got %q", got)
	}
	if got := ChatSandboxFile("proj", "", "app/src.ts"); got != "/workspace/app/src.ts" {
		t.Fatalf("got %q", got)
	}
}

func TestRelFromSandboxPath(t *testing.T) {
	got, ok := RelFromSandboxPath("proj", "chat", "/workspace/notes.md")
	if !ok || got != "notes.md" {
		t.Fatalf("/workspace/notes.md: got %q ok=%v", got, ok)
	}
	got, ok = RelFromSandboxPath("proj", "chat", "/workspace/chat/notes.md")
	if !ok || got != "notes.md" {
		t.Fatalf("legacy /workspace/<sid>/ prefix: got %q ok=%v", got, ok)
	}
	got, ok = RelFromSandboxPath("", "chat", "/workspace/notes.md")
	if !ok || got != "notes.md" {
		t.Fatalf("loose chat: got %q ok=%v", got, ok)
	}
	got, ok = RelFromSandboxPath("proj", "", "/workspace/app/src.ts")
	if !ok || got != "app/src.ts" {
		t.Fatalf("coding root: got %q ok=%v", got, ok)
	}
}

func TestSkipSnapshotRel(t *testing.T) {
	if !skipSnapshotRel("app/node_modules/pkg/index.js") {
		t.Fatal("node_modules should be skipped")
	}
	if skipSnapshotRel("app/src/index.ts") {
		t.Fatal("source should be kept")
	}
}
