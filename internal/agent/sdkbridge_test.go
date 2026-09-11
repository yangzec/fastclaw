package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fastclaw-ai/fastclaw/internal/agent/tools"
	"github.com/fastclaw-ai/fastclaw/internal/provider"
)

func testRegistryWith(t *testing.T, name string, fn tools.ToolFunc) *tools.Registry {
	t.Helper()
	r := tools.NewRegistry(t.TempDir(), t.TempDir())
	r.Register(name, "test", map[string]any{"type": "object"}, fn)
	return r
}

func TestExecuteToolsConcurrentlyClipsHugeSuccess(t *testing.T) {
	r := testRegistryWith(t, "dump", func(context.Context, json.RawMessage) (string, error) {
		return strings.Repeat("A", 64*1024+8000), nil
	})
	eng := newSDKEngine("test")
	got := eng.executeToolsConcurrently(context.Background(), r, []provider.ToolCall{
		{ID: "1", Function: provider.FunctionCall{Name: "dump", Arguments: "{}"}},
	}, t.TempDir(), map[string]struct{}{"dump": {}})
	if len(got) != 1 || got[0].err != nil {
		t.Fatalf("got %+v", got)
	}
	if strings.Contains(got[0].result, strings.Repeat("A", 64*1024+1)) {
		t.Fatal("unclipped dump reached the model path")
	}
	if !strings.Contains(got[0].result, "truncated") {
		t.Fatalf("missing truncation notice: %s", tail(got[0].result))
	}
}

func TestExecuteToolsConcurrentlyClipsHugeErrorAndHintsOnce(t *testing.T) {
	r := testRegistryWith(t, "boom", func(context.Context, json.RawMessage) (string, error) {
		return strings.Repeat("E", 64*1024+4000), fmt.Errorf("executor exploded")
	})
	eng := newSDKEngine("test")
	got := eng.executeToolsConcurrently(context.Background(), r, []provider.ToolCall{
		{ID: "1", Function: provider.FunctionCall{Name: "boom", Arguments: "{}"}},
	}, t.TempDir(), map[string]struct{}{"boom": {}})
	if len(got) != 1 || got[0].err == nil {
		t.Fatalf("want error result, got %+v", got)
	}
	if strings.Contains(got[0].result, strings.Repeat("E", 64*1024+1)) {
		t.Fatal("unclipped error body reached the model path")
	}
	if n := strings.Count(got[0].result, tools.ErrorAnalyzeHint); n != 1 {
		t.Fatalf("Analyze hint count = %d, want 1\n%s", n, tail(got[0].result))
	}
}

func TestExecuteToolsConcurrentlyRefusesToolsNotInAllowlist(t *testing.T) {
	var ran atomic.Int32
	r := testRegistryWith(t, "echo", func(context.Context, json.RawMessage) (string, error) {
		ran.Add(1)
		return "ran", nil
	})
	eng := newSDKEngine("test")
	got := eng.executeToolsConcurrently(context.Background(), r, []provider.ToolCall{
		{ID: "1", Function: provider.FunctionCall{Name: "echo", Arguments: "{}"}},
	}, t.TempDir(), map[string]struct{}{})
	if ran.Load() != 0 {
		t.Fatal("disallowed tool still executed")
	}
	if len(got) != 1 || got[0].err == nil || !strings.Contains(got[0].result, "Not executed") {
		t.Fatalf("got %+v", got)
	}
}

func TestExecuteToolsConcurrentlyRejectsInvalidJSONArgs(t *testing.T) {
	var ran atomic.Int32
	r := testRegistryWith(t, "echo", func(context.Context, json.RawMessage) (string, error) {
		ran.Add(1)
		return "ran", nil
	})
	eng := newSDKEngine("test")
	got := eng.executeToolsConcurrently(context.Background(), r, []provider.ToolCall{
		{ID: "1", Function: provider.FunctionCall{Name: "echo", Arguments: "{not-json"}},
	}, t.TempDir(), map[string]struct{}{"echo": {}})
	if ran.Load() != 0 {
		t.Fatal("invalid JSON still executed via _raw")
	}
	if len(got) != 1 || got[0].err == nil || !strings.Contains(got[0].result, "valid JSON") {
		t.Fatalf("got %+v", got)
	}
}

func tail(s string) string {
	if len(s) < 160 {
		return s
	}
	return s[len(s)-160:]
}
