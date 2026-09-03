package config

import "strings"

// Official (or vendor-documented) input context sizes for the model IDs
// FastClaw ships as provider presets. These are defaults only: a saved
// ModelEntry.ContextWindow always wins. Used when that field is 0 —
// onboard used to persist only id/name — and as the prefill when the
// Models dialog picks a preset.
//
// Keep in sync with web/src/lib/model-defaults.ts.
//
// Sources checked 2026-09-02:
//   - OpenAI GPT-5.6 Sol: 1,050,000
//     https://developers.openai.com/api/docs/models/gpt-5.6-sol
//   - Zhipu GLM-5.3 / GLM-5.3-Flash: 1M
//     https://docs.bigmodel.cn/cn/guide/models/text/glm-5.3
//   - Moonshot Kimi K3: 1,048,576
//     https://platform.moonshot.cn/docs/guide/start-using-kimi-api
//   - xAI Grok 4.6 / 4.5: 500,000 (latest flagship is not 1M)
//     https://docs.x.ai/developers/models/grok-4.6
//     https://docs.x.ai/developers/models
var knownContextWindows = map[string]int{
	"gpt-5.6":                       1_050_000,
	"gpt-5.6-sol":                   1_050_000,
	"gpt-5.6-terra":                 1_050_000,
	"gpt-5.6-luna":                  400_000,
	"gpt-5.5":                       1_050_000,
	"gpt-5.5-pro":                   1_050_000,
	"glm-5.3":                       1_000_000,
	"glm-5.3-flash":                 1_000_000,
	"kimi-k3":                       1_048_576,
	"k3":                            1_048_576,
	"grok-4.6":                      500_000,
	"grok-4.5":                      500_000,
	"grok-4.5-latest":               500_000,
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

// ContextWindowOrDefault returns a saved operator override when it is
// set, otherwise the official preset for model. 0 means we still don't
// know.
func ContextWindowOrDefault(model string, saved int) int {
	if saved > 0 {
		return saved
	}
	return KnownContextWindow(model)
}

// Official (or vendor-documented) max output sizes for the same
// preset IDs. These are the request ceiling we prefill as maxTokens —
// not "how long we want a typical reply". Keep in sync with
// web/src/lib/model-defaults.ts.
//
// Sources checked 2026-09-03:
//   - OpenAI GPT-5.6 Sol / Terra / Luna / GPT-5.5: 128,000
//     https://developers.openai.com/api/docs/models/gpt-5.6-sol
//   - Zhipu GLM-5.3 / GLM-5.3-Flash: 128K
//     https://docs.bigmodel.cn/cn/guide/models/text/glm-5.3
//   - Moonshot Kimi K3: max_completion_tokens default 131,072
//     (raisable to the 1,048,576 shared window)
//     https://platform.kimi.ai/docs/guide/kimi-k3-quickstart
//   - xAI Grok 4.6: no published text output limit — omit (8192 UI default)
//     https://docs.x.ai/developers/release-notes
var knownMaxTokens = map[string]int{
	"gpt-5.6":           128_000,
	"gpt-5.6-sol":       128_000,
	"gpt-5.6-terra":     128_000,
	"gpt-5.6-luna":      128_000,
	"gpt-5.5":           128_000,
	"gpt-5.5-pro":       128_000,
	"glm-5.3":           128_000,
	"glm-5.3-flash":     128_000,
	"kimi-k3":           131_072,
	"k3":                131_072,
}

// KnownMaxTokens returns the preset completion budget for a model id
// (with or without a "provider/" prefix). 0 means we don't know.
func KnownMaxTokens(model string) int {
	id := strings.TrimSpace(model)
	if id == "" {
		return 0
	}
	if w := knownMaxTokens[id]; w > 0 {
		return w
	}
	if i := strings.Index(id, "/"); i >= 0 {
		rest := id[i+1:]
		if w := knownMaxTokens[rest]; w > 0 {
			return w
		}
	}
	return 0
}

// MaxTokensOrDefault returns a saved operator override when it is set,
// otherwise the official preset for model. 0 means we still don't know.
func MaxTokensOrDefault(model string, saved int) int {
	if saved > 0 {
		return saved
	}
	return KnownMaxTokens(model)
}
