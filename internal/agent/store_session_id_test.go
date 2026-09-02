package agent

import (
	"testing"

	coderuntime "github.com/fastclaw-ai/fastclaw/internal/runtime"
)

func TestStoreSessionID(t *testing.T) {
	plain := &Agent{}
	if got := plain.storeSessionID("proj", "chat"); got != "chat" {
		t.Fatalf("plain agent: got %q, want chat", got)
	}
	if got := plain.storeSessionID("", "chat"); got != "chat" {
		t.Fatalf("loose chat: got %q, want chat", got)
	}

	coding := &Agent{projectRuntime: &coderuntime.Manager{}}
	if got := coding.storeSessionID("proj", "chat"); got != "" {
		t.Fatalf("coding in project: got %q, want empty (project root)", got)
	}
	if got := coding.storeSessionID("", "chat"); got != "chat" {
		t.Fatalf("coding loose chat: got %q, want chat", got)
	}
}
