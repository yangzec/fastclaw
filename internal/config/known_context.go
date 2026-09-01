package config

import "strings"

// Official (or vendor-documented) input context sizes for the model IDs
// FastClaw ships as provider presets. Used when a saved ModelEntry has
// contextWindow 0 — onboard used to persist only id/name — and as the
// prefill when the Models dialog picks a preset.
//
// Keep in sync with web/src/lib/model-defaults.ts.
var knownContextWindows = map[string]int{
	"gpt-5.5":                       1_050_000,
	"gpt-5.5-pro":                   1_050_000,
	"claude-opus-4-7":               1_000_000,
	"claude-sonnet-4-7":             1_000_000,
	"claude-haiku-4-5":              200_000,
	"claude-haiku-4-5-20251001":     200_000,
	"deepseek-v4-pro":               1_000_000,
	"deepseek-v4-flash":             1_000_000,
	"gemini-3-flash-preview":        1_048_576,
	"google/gemini-3-flash-preview": 1_048_576,
	"qwen3.5:35b-a3b-int4":          262_144,
	"qwen3.5:35b-a3b":               262_144,
}

// KnownContextWindow returns the preset context size for a model id
// (with or without a "provider/" prefix). 0 means we don't know.
func KnownContextWindow(model string) int {
	id := strings.TrimSpace(model)
	if id == "" {
		return 0
	}
	if w := knownContextWindows[id]; w > 0 {
		return w
	}
	if i := strings.Index(id, "/"); i >= 0 {
		rest := id[i+1:]
		if w := knownContextWindows[rest]; w > 0 {
			return w
		}
	}
	return 0
}
