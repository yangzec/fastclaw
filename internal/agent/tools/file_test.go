package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestWorkspaceRelUnifiesSandboxAndRelativePaths(t *testing.T) {
	cases := []struct {
		path    string
		wantRel string
		wantOK  bool
	}{
		{"report.md", "report.md", true},
		{"charts/plot.svg", "charts/plot.svg", true},
		{"/workspace/report.md", "report.md", true},
		{"/workspace/charts/plot.svg", "charts/plot.svg", true},
		{"/workspace", ".", true},
		{"/workspace/", ".", true},
		{"/tmp/draw.py", "", false},
		{"SOUL.md", "", false},
		{"/workspace/SOUL.md", "", false},
		{"skills/foo/SKILL.md", "", false},
		{"", ".", true},
		{".", ".", true},
	}
	for _, tc := range cases {
		got, ok := workspaceRel(tc.path)
		if ok != tc.wantOK || got != tc.wantRel {
			t.Errorf("workspaceRel(%q) = (%q, %v), want (%q, %v)", tc.path, got, ok, tc.wantRel, tc.wantOK)
		}
		if r := (&Registry{}).isWorkspacePath(tc.path); r != tc.wantOK {
			t.Errorf("isWorkspacePath(%q) = %v, want %v", tc.path, r, tc.wantOK)
		}
	}
	if got := workspaceListPrefix("/workspace"); got != "" {
		t.Errorf("workspaceListPrefix(/workspace) = %q, want empty", got)
	}
	if got := workspaceListPrefix("/workspace/docs"); got != "docs" {
		t.Errorf("workspaceListPrefix(/workspace/docs) = %q, want docs", got)
	}
}

// TestApplyEdit pins the contract that edit_file's three backends share:
// a single match replaces in place, the empty / equal / not-found / multi-
// match cases each error with a fragment the LLM can act on, and
// replace_all swaps every occurrence. Pure-function tests only — backend
// routing is exercised through the running agent.
func TestApplyEdit(t *testing.T) {
	const (
		path  = "MEMORY.md"
		oldS  = "alpha"
		newS  = "beta"
		multi = "alpha and alpha"
	)

	cases := []struct {
		name       string
		content    string
		oldStr     string
		newStr     string
		replaceAll bool

		wantContent string
		wantCount   int
		wantErrSub  string // substring; empty == expect no error
	}{
		{
			name:        "single match replaces in place",
			content:     "x alpha y",
			oldStr:      oldS,
			newStr:      newS,
			wantContent: "x beta y",
			wantCount:   1,
		},
		{
			name:        "replace_all swaps every occurrence",
			content:     multi,
			oldStr:      oldS,
			newStr:      newS,
			replaceAll:  true,
			wantContent: "beta and beta",
			wantCount:   2,
		},
		{
			name:       "multi match without replace_all errors with count and hint",
			content:    multi,
			oldStr:     oldS,
			newStr:     newS,
			wantErrSub: "matches 2 locations",
		},
		{
			name:       "not found errors with path so the LLM knows which file to re-read",
			content:    "nothing here",
			oldStr:     oldS,
			newStr:     newS,
			wantErrSub: "not found in " + path,
		},
		{
			name:       "empty old_string rejected (use write_file instead)",
			content:    "anything",
			oldStr:     "",
			newStr:     newS,
			wantErrSub: "old_string is empty",
		},
		{
			name:       "no-op edit (old == new) rejected",
			content:    "x alpha y",
			oldStr:     oldS,
			newStr:     oldS,
			wantErrSub: "must differ",
		},
		{
			name:        "replace_all with single match still works",
			content:     "x alpha y",
			oldStr:      oldS,
			newStr:      newS,
			replaceAll:  true,
			wantContent: "x beta y",
			wantCount:   1,
		},
		{
			name:        "whitespace-sensitive match (indentation matters)",
			content:     "  alpha\n",
			oldStr:      "  alpha",
			newStr:      "  beta",
			wantContent: "  beta\n",
			wantCount:   1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, count, err := applyEdit(path, tc.content, tc.oldStr, tc.newStr, tc.replaceAll)

			if tc.wantErrSub != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (content=%q)", tc.wantErrSub, got)
				}
				if !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrSub)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantContent {
				t.Errorf("content mismatch:\n  got:  %q\n  want: %q", got, tc.wantContent)
			}
			if count != tc.wantCount {
				t.Errorf("count mismatch: got %d, want %d", count, tc.wantCount)
			}
		})
	}
}

func TestWriteFileWorkspaceStoreReturnsStablePaths(t *testing.T) {
	st := &imageArchiveStore{}
	r := NewRegistry(t.TempDir(), t.TempDir())
	r.SetWorkspaceStore(st, "agent-a")
	r.SetSessionID("sess-a")
	r.SetGoalSessionKey("s-key")

	args, err := json.Marshal(writeFileArgs{Path: "preview.html", Content: "<h1>Hello</h1>"})
	if err != nil {
		t.Fatal(err)
	}
	text, err := makeWriteFile(r)(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Written 14 bytes to preview.html") {
		t.Fatalf("write summary missing: %s", text)
	}
	if !strings.Contains(text, "Workspace path: /workspace/preview.html") {
		t.Fatalf("workspace path missing: %s", text)
	}
	if !strings.Contains(text, "URL: /api/agents/agent-a/files/preview.html?sessionId=s-key") {
		t.Fatalf("stable gateway URL missing: %s", text)
	}
	if strings.Contains(text, "sessions/s-key/") || strings.Contains(text, "sessions/sess-a/") {
		t.Fatalf("must not guess sessions/<id>/ in the display URL: %s", text)
	}
}

func TestWriteFileWorkspaceStoreUsesPublicURLWhenConfigured(t *testing.T) {
	st := &imageArchiveStore{publicURL: "https://cdn.example.test/fastclaw/agent-a/sessions/sess-a/preview.html"}
	r := NewRegistry(t.TempDir(), t.TempDir())
	r.SetWorkspaceStore(st, "agent-a")
	r.SetSessionID("sess-a")
	r.SetGoalSessionKey("s-key")

	args, err := json.Marshal(writeFileArgs{Path: "preview.html", Content: "<h1>Hello</h1>"})
	if err != nil {
		t.Fatal(err)
	}
	text, err := makeWriteFile(r)(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "URL: https://cdn.example.test/fastclaw/agent-a/sessions/sess-a/preview.html") {
		t.Fatalf("public URL missing: %s", text)
	}
	if strings.Contains(text, "URL: /api/agents/agent-a/files/") {
		t.Fatalf("public URL configured but gateway URL was returned: %s", text)
	}
}

func TestArchiveGatewayURLOmitsSessionIdForCodingRoot(t *testing.T) {
	r := NewRegistry(t.TempDir(), t.TempDir())
	r.SetWorkspaceStore(&imageArchiveStore{}, "agent-a")
	r.SetProjectID("proj-1")
	r.SetGoalSessionKey("s-key")
	r.SetCodingRootScope(true)
	got := r.archiveGatewayURL("app/src.ts")
	want := "/api/agents/agent-a/files/app/src.ts?projectId=proj-1"
	if got != want {
		t.Fatalf("coding URL = %q, want %q", got, want)
	}
}
