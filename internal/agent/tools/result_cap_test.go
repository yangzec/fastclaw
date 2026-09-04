package tools

import (
	"strings"
	"testing"
)

func TestClipToolResultLeavesSmallOutput(t *testing.T) {
	in := "ok\n"
	if got := clipToolResult(in); got != in {
		t.Fatalf("small output was rewritten: %q", got)
	}
}

func TestClipToolResultCapsHugeExecDump(t *testing.T) {
	in := strings.Repeat("A", maxToolResultRunes+50_000)
	got := clipToolResult(in)
	if strings.Contains(got, strings.Repeat("A", maxToolResultRunes+1)) {
		t.Fatal("clipped output still contains the full dump")
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("missing truncation notice: %s", got[len(got)-120:])
	}
	if n := len([]rune(got)); n > maxToolResultRunes+200 {
		t.Fatalf("clipped length %d still too large", n)
	}
}
