package config

import "os"

// DetectedProvider is a provider we can wire from an environment variable
// already sitting on the machine. Name/Env/Model are safe to show in the UI;
// the secret itself never leaves the process.
type DetectedProvider struct {
	Name  string `json:"name"`
	Env   string `json:"env"`
	Model string `json:"model,omitempty"`
}

// envProviderCandidates is first-match-wins. Keep models aligned with
// web/src/lib/provider-presets.ts so auto-onboard lands on a chatable id.
var envProviderCandidates = []DetectedProvider{
	{Name: "openai", Env: "OPENAI_API_KEY", Model: "gpt-5.6"},
	{Name: "anthropic", Env: "ANTHROPIC_API_KEY", Model: "claude-sonnet-4-7"},
	{Name: "openrouter", Env: "OPENROUTER_API_KEY", Model: "google/gemini-3-flash-preview"},
	{Name: "zhipu", Env: "ZHIPUAI_API_KEY", Model: "glm-5.3"},
	{Name: "kimi", Env: "MOONSHOT_API_KEY", Model: "kimi-k3"},
	{Name: "grok", Env: "XAI_API_KEY", Model: "grok-4.6"},
	{Name: "deepseek", Env: "DEEPSEEK_API_KEY", Model: "deepseek-v4-pro"},
}

// DetectProviderFromEnv returns the first known provider whose API key is
// already exported. Empty env values do not count.
func DetectProviderFromEnv() (DetectedProvider, bool) {
	for _, candidate := range envProviderCandidates {
		if os.Getenv(candidate.Env) != "" {
			return candidate, true
		}
	}
	return DetectedProvider{}, false
}

// EnvAPIKeyFor returns the process env value for a provider name, or "".
func EnvAPIKeyFor(provider string) string {
	for _, candidate := range envProviderCandidates {
		if candidate.Name == provider {
			return os.Getenv(candidate.Env)
		}
	}
	return ""
}
