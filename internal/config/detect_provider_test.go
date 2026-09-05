package config

import "testing"

func TestDetectProviderFromEnvFirstMatch(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant")
	t.Setenv("OPENROUTER_API_KEY", "or-key")
	got, ok := DetectProviderFromEnv()
	if !ok {
		t.Fatal("expected a detected provider")
	}
	if got.Name != "anthropic" || got.Env != "ANTHROPIC_API_KEY" || got.Model == "" {
		t.Fatalf("got %+v", got)
	}
}

func TestDetectProviderFromEnvNone(t *testing.T) {
	for _, candidate := range envProviderCandidates {
		t.Setenv(candidate.Env, "")
	}
	if _, ok := DetectProviderFromEnv(); ok {
		t.Fatal("empty env should not detect a provider")
	}
}

func TestEnvAPIKeyFor(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	if got := EnvAPIKeyFor("openai"); got != "sk-test" {
		t.Fatalf("got %q", got)
	}
	if got := EnvAPIKeyFor("missing"); got != "" {
		t.Fatalf("unknown provider = %q", got)
	}
}
