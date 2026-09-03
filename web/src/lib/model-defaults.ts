// Official (or vendor-documented) input context and max-output sizes
// for the model IDs FastClaw ships as provider presets. Keep in sync
// with internal/config/known_context.go.
//
// Sources checked 2026-09-02 (context) / 2026-09-03 (max output):
//   - OpenAI GPT-5.6 Sol: 1,050,000 in / 128,000 out
//     https://developers.openai.com/api/docs/models/gpt-5.6-sol
//   - Zhipu GLM-5.3 / GLM-5.3-Flash: 1M in / 128K out
//     https://docs.bigmodel.cn/cn/guide/models/text/glm-5.3
//   - Moonshot Kimi K3: 1,048,576 in / 131,072 out (API default)
//     https://platform.kimi.ai/docs/guide/kimi-k3-quickstart
//   - xAI Grok 4.6 / 4.5: 500,000 in, no published output cap
//     https://docs.x.ai/developers/models/grok-4.6

export const DEFAULT_CONTEXT_WINDOW = 200000;
export const DEFAULT_MAX_TOKENS = 8192;

const KNOWN_CONTEXT_WINDOWS: Record<string, number> = {
  "gpt-5.6": 1_050_000,
  "gpt-5.6-sol": 1_050_000,
  "gpt-5.6-terra": 1_050_000,
  "gpt-5.6-luna": 400_000,
  "gpt-5.5": 1_050_000,
  "gpt-5.5-pro": 1_050_000,
  "glm-5.3": 1_000_000,
  "glm-5.3-flash": 1_000_000,
  "kimi-k3": 1_048_576,
  k3: 1_048_576,
  "grok-4.6": 500_000,
  "grok-4.5": 500_000,
  "grok-4.5-latest": 500_000,
  "claude-opus-4-7": 1_000_000,
  "claude-sonnet-4-7": 1_000_000,
  "claude-haiku-4-5": 200_000,
  "claude-haiku-4-5-20251001": 200_000,
  "deepseek-v4-pro": 1_000_000,
  "deepseek-v4-flash": 1_000_000,
  "gemini-3-flash-preview": 1_048_576,
  "google/gemini-3-flash-preview": 1_048_576,
  "qwen3.5:35b-a3b-int4": 262_144,
  "qwen3.5:35b-a3b": 262_144,
};

export function knownContextWindow(modelId: string): number {
  const id = modelId.trim();
  if (!id) return 0;
  if (KNOWN_CONTEXT_WINDOWS[id]) return KNOWN_CONTEXT_WINDOWS[id];
  const slash = id.indexOf("/");
  if (slash >= 0) {
    const rest = id.slice(slash + 1);
    if (KNOWN_CONTEXT_WINDOWS[rest]) return KNOWN_CONTEXT_WINDOWS[rest];
  }
  return 0;
}

export function presetContextWindow(modelId: string): number {
  return knownContextWindow(modelId) || DEFAULT_CONTEXT_WINDOW;
}

const KNOWN_MAX_TOKENS: Record<string, number> = {
  "gpt-5.6": 128_000,
  "gpt-5.6-sol": 128_000,
  "gpt-5.6-terra": 128_000,
  "gpt-5.6-luna": 128_000,
  "gpt-5.5": 128_000,
  "gpt-5.5-pro": 128_000,
  "glm-5.3": 128_000,
  "glm-5.3-flash": 128_000,
  "kimi-k3": 131_072,
  k3: 131_072,
};

export function knownMaxTokens(modelId: string): number {
  const id = modelId.trim();
  if (!id) return 0;
  if (KNOWN_MAX_TOKENS[id]) return KNOWN_MAX_TOKENS[id];
  const slash = id.indexOf("/");
  if (slash >= 0) {
    const rest = id.slice(slash + 1);
    if (KNOWN_MAX_TOKENS[rest]) return KNOWN_MAX_TOKENS[rest];
  }
  return 0;
}

export function presetMaxTokens(modelId: string): number {
  return knownMaxTokens(modelId) || DEFAULT_MAX_TOKENS;
}

// nextMaxTokensOnIdChange updates the completion budget only while it
// still looks like a default (unset, the generic UI fallback, or the
// previous model's official size). A number the user typed is kept.
export function nextMaxTokensOnIdChange(
  nextId: string,
  prevId: string,
  current: number,
): number {
  const known = knownMaxTokens(nextId);
  if (known <= 0) return current;
  const prevKnown = knownMaxTokens(prevId);
  if (current <= 0 || current === DEFAULT_MAX_TOKENS || current === prevKnown) {
    return known;
  }
  return current;
}

// nextContextWindowOnIdChange updates the window only while it still
// looks like a default (unset, the generic UI fallback, or the previous
// model's official size). A number the user typed is kept.
export function nextContextWindowOnIdChange(
  nextId: string,
  prevId: string,
  currentWindow: number,
): number {
  const known = knownContextWindow(nextId);
  if (known <= 0) return currentWindow;
  const prevKnown = knownContextWindow(prevId);
  if (
    currentWindow <= 0 ||
    currentWindow === DEFAULT_CONTEXT_WINDOW ||
    currentWindow === prevKnown
  ) {
    return known;
  }
  return currentWindow;
}
