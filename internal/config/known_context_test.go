package config

import "testing"

func TestKnownContextWindowPresetIDs(t *testing.T) {
	cases := []struct {
		id   string
		want int
	}{
		{"gpt-5.6", 1_050_000},
		{"gpt-5.6-sol", 1_050_000},
		{"gpt-5.6-luna", 1_050_000},
		{"openai/gpt-5.6", 1_050_000},
		{"gpt-5.5", 1_050_000},
		{"openai/gpt-5.5", 1_050_000},
		{"glm-5.3", 1_000_000},
		{"zhipu/glm-5.3-flash", 1_000_000},
		{"kimi-k3", 1_048_576},
		{"kimi/kimi-k3", 1_048_576},
		{"k3", 1_048_576},
		{"grok-4.6", 500_000},
		{"grok/grok-4.5", 500_000},
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

func TestKnownMaxTokensPresetIDs(t *testing.T) {
	cases := []struct {
		id   string
		want int
	}{
		{"gpt-5.6", 128_000},
		{"gpt-5.6-sol", 128_000},
		{"openai/gpt-5.6", 128_000},
		{"gpt-5.5", 128_000},
		{"glm-5.3", 128_000},
		{"zhipu/glm-5.3-flash", 128_000},
		{"kimi-k3", 131_072},
		{"kimi/k3", 131_072},
		{"grok-4.6", 0},
		{"claude-opus-4-7", 0},
		{"totally-unknown-model", 0},
		{"", 0},
	}
	for _, tc := range cases {
		if got := KnownMaxTokens(tc.id); got != tc.want {
			t.Errorf("KnownMaxTokens(%q) = %d, want %d", tc.id, got, tc.want)
		}
	}
}

func TestMaxTokensOrDefaultPrefersSaved(t *testing.T) {
	if got := MaxTokensOrDefault("glm-5.3", 8192); got != 8192 {
		t.Fatalf("saved override = %d, want 8192", got)
	}
	if got := MaxTokensOrDefault("glm-5.3", 0); got != 128_000 {
		t.Fatalf("empty saved = %d, want official 128000", got)
	}
	if got := MaxTokensOrDefault("unknown", 0); got != 0 {
		t.Fatalf("unknown = %d, want 0", got)
	}
}

func TestContextWindowOrDefaultPrefersSaved(t *testing.T) {
	if got := ContextWindowOrDefault("glm-5.3", 256_000); got != 256_000 {
		t.Fatalf("saved override = %d, want 256000", got)
	}
	if got := ContextWindowOrDefault("glm-5.3", 0); got != 1_000_000 {
		t.Fatalf("empty saved = %d, want official 1000000", got)
	}
	if got := ContextWindowOrDefault("unknown", 0); got != 0 {
		t.Fatalf("unknown = %d, want 0", got)
	}
}
