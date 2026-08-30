package config

import "testing"

func TestKnownContextWindowPresetIDs(t *testing.T) {
	cases := []struct {
		id   string
		want int
	}{
		{"gpt-5.5", 1_050_000},
		{"openai/gpt-5.5", 1_050_000},
		{"claude-opus-4-7", 1_000_000},
		{"anthropic/claude-sonnet-4-7", 1_000_000},
		{"claude-haiku-4-5", 200_000},
		{"deepseek-v4-flash", 1_000_000},
		{"google/gemini-3-flash-preview", 1_048_576},
		{"gemini-3-flash-preview", 1_048_576},
		{"qwen3.5:35b-a3b-int4", 262_144},
		{"totally-unknown-model", 0},
		{"", 0},
	}
	for _, tc := range cases {
		if got := KnownContextWindow(tc.id); got != tc.want {
			t.Errorf("KnownContextWindow(%q) = %d, want %d", tc.id, got, tc.want)
		}
	}
}
