package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/provider"
)

func TestMentionsProtectedConfig(t *testing.T) {
	yes := []string{
		"cat SOUL.md",
		"cat /var/lib/fastclaw/agents/x/SOUL.md",
		"head -n 20 IDENTITY.md",
		"cat /skills/support/SKILL.md",
		"cat /ski*/support/SKI*.md",
		"python -c 'print(open(\"agent.json\").read())'",
		"cat notes/SOUL.md",
	}
	for _, cmd := range yes {
		if !mentionsProtectedConfig(cmd) {
			t.Errorf("mentionsProtectedConfig(%q) = false, want true", cmd)
		}
	}
	no := []string{
		"python /skills/support/main.py",
		"ls /skills",
		"echo hello",
		"cat report-about-soul.md",
	}
	for _, cmd := range no {
		if mentionsProtectedConfig(cmd) {
			t.Errorf("mentionsProtectedConfig(%q) = true, want false", cmd)
		}
	}
}

func TestGuestExecRefusesConfigRead(t *testing.T) {
	r := NewRegistry(t.TempDir(), t.TempDir())
	registerExecFull(r, nil, nil, nil)

	_, err := r.Execute(context.Background(), "exec", `{"command":"cat /skills/x/SKILL.md"}`)
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("guest cat SKILL.md: err = %v", err)
	}

	r.SetCallerIsAdmin(true)
	r.SetCallerCanHost(true)
	out, err := r.Execute(context.Background(), "exec", `{"command":"echo owner-ok"}`)
	if err != nil {
		t.Fatalf("owner exec: %v", err)
	}
	if !strings.Contains(out, "owner-ok") {
		t.Fatalf("owner exec output = %q", out)
	}
}

func TestGuestListDirHidesIdentityFiles(t *testing.T) {
	r := NewRegistry(t.TempDir(), t.TempDir())

	listing := r.filterGuestDirListing("f SOUL.md (10 bytes)\nf report.md (2 bytes)\nf SKILL.md (3 bytes)\nd skills/\n")
	if strings.Contains(listing, "SOUL.md") || strings.Contains(listing, "SKILL.md") {
		t.Fatalf("guest listing leaked config: %q", listing)
	}
	if !strings.Contains(listing, "report.md") || !strings.Contains(listing, "skills/") {
		t.Fatalf("guest listing dropped workspace entries: %q", listing)
	}

	r.SetCallerIsAdmin(true)
	full := r.filterGuestDirListing("f SOUL.md (10 bytes)\n")
	if !strings.Contains(full, "SOUL.md") {
		t.Fatalf("owner listing should keep SOUL.md: %q", full)
	}
}

func TestDefinitionsHideOwnerAndHostToolsFromGuest(t *testing.T) {
	r := NewRegistry(t.TempDir(), t.TempDir())
	noop := func(context.Context, json.RawMessage) (string, error) { return "", nil }
	r.Register("exec", "run", map[string]any{}, noop)
	r.Register("create_agent", "provision", map[string]any{}, noop)
	r.Register(HostExecToolName, "host", map[string]any{}, noop)
	r.Register("web_search", "search", map[string]any{}, noop)

	names := toolDefNames(r.DefinitionsForMode(nil))
	if names["create_agent"] || names[HostExecToolName] {
		t.Fatalf("guest catalog leaked management/host tools: %v", names)
	}
	if !names["exec"] || !names["web_search"] {
		t.Fatalf("guest catalog missing chat tools: %v", names)
	}

	r.SetCallerIsAdmin(true)
	names = toolDefNames(r.DefinitionsForMode(nil))
	if !names["create_agent"] {
		t.Fatal("owner should see create_agent")
	}
	if names[HostExecToolName] {
		t.Fatal("owner without host must not see host_exec")
	}

	r.SetCallerCanHost(true)
	names = toolDefNames(r.DefinitionsForMode(nil))
	if !names[HostExecToolName] {
		t.Fatal("super_admin should see host_exec")
	}
}

func TestExecuteHidesOwnerToolsFromGuest(t *testing.T) {
	r := NewRegistry(t.TempDir(), t.TempDir())
	r.Register("create_agent", "provision", map[string]any{}, func(context.Context, json.RawMessage) (string, error) {
		return "created", nil
	})
	_, err := r.Execute(context.Background(), "create_agent", `{"name":"x"}`)
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("guest create_agent: %v", err)
	}
	r.SetCallerIsAdmin(true)
	out, err := r.Execute(context.Background(), "create_agent", `{"name":"x"}`)
	if err != nil || out != "created" {
		t.Fatalf("owner create_agent: out=%q err=%v", out, err)
	}
}

func toolDefNames(defs []provider.Tool) map[string]bool {
	out := make(map[string]bool, len(defs))
	for _, d := range defs {
		out[d.Function.Name] = true
	}
	return out
}
