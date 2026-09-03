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
  "gpt-5.6-luna": 1_050_000,
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

export type ModelLimitFamily = "gpt-5.6" | "gpt-5.5" | "glm" | "kimi" | "grok" | "other";

export function bareModelId(modelId: string): string {
  const id = modelId.trim().toLowerCase();
  const slash = id.lastIndexOf("/");
  return slash >= 0 ? id.slice(slash + 1) : id;
}

export function modelLimitFamily(modelId: string): ModelLimitFamily {
  const id = bareModelId(modelId);
  if (id === "gpt-5.6" || id.startsWith("gpt-5.6-")) return "gpt-5.6";
  if (id === "gpt-5.5" || id.startsWith("gpt-5.5-")) return "gpt-5.5";
  if (id.startsWith("glm-")) return "glm";
  if (id === "k3" || id.startsWith("kimi")) return "kimi";
  if (id.startsWith("grok-")) return "grok";
  return "other";
}

// suggestedMaxTokens is the daily request budget we prefill / highlight.
// Official caps stay in knownMaxTokens (often 128k) — sending that every
// turn is usually the wrong default, especially on GPT-5.5.
export function suggestedMaxTokens(modelId: string): number {
  switch (modelLimitFamily(modelId)) {
    case "gpt-5.6":
      return 65_536;
    case "gpt-5.5":
      return 32_768;
    case "glm":
      return 65_536;
    case "kimi":
      return 131_072;
    case "grok":
      return 32_768;
    default:
      return knownMaxTokens(modelId) || DEFAULT_MAX_TOKENS;
  }
}

export function presetMaxTokens(modelId: string): number {
  return suggestedMaxTokens(modelId);
}

export const MAX_TOKEN_OPTIONS: { value: number; label: string }[] = [
  { value: 8_192, label: "8k" },
  { value: 16_384, label: "16k" },
  { value: 32_768, label: "32k" },
  { value: 65_536, label: "64k" },
  { value: 128_000, label: "128k" },
];

export type MaxTokenOption = {
  value: number;
  label: string;
  tag?: "suggested" | "official";
};

function optionLabel(n: number): string {
  const hit = MAX_TOKEN_OPTIONS.find((o) => o.value === n);
  if (hit) return hit.label;
  if (n === 131_072) return "131k";
  if (n >= 1000 && n % 1024 === 0) return n / 1024 + "k";
  if (n >= 1000) return Math.round(n / 1000) + "k";
  return String(n);
}

export function maxTokenOptionsFor(modelId: string): MaxTokenOption[] {
  const suggested = suggestedMaxTokens(modelId);
  const official = knownMaxTokens(modelId);
  const values = new Set<number>(MAX_TOKEN_OPTIONS.map((o) => o.value));
  if (official > 0) values.add(official);
  if (suggested > 0) values.add(suggested);
  return [...values]
    .sort((a, b) => a - b)
    .map((value) => {
      let tag: MaxTokenOption["tag"];
      if (value === suggested) tag = "suggested";
      else if (value === official) tag = "official";
      return { value, label: optionLabel(value), tag };
    });
}

export type MaxOutputTip = { headline: string; body: string };

export function maxOutputTip(modelId: string): MaxOutputTip {
  switch (modelLimitFamily(modelId)) {
    case "gpt-5.6":
      return {
        headline: "GPT-5.6 · 建议 64k（和 5.5 不一样）",
        body: "API 窗口和上限跟 5.5 一样是 1.05M / 128k。差别在推理：5.6 多了 max 档，思考 token 也算在这笔输出预算里。档位开高时 8k/16k 经常只剩思考、正文被截断。Sol / 高推理选 64k 或 128k；Luna 日常 16k–32k 即可。",
      };
    case "gpt-5.5":
      return {
        headline: "GPT-5.5 · 建议 32k",
        body: "API 上限也是 128k，但推理只到 xhigh，没有 5.6 的 max。日常 agent 选 16k 或 32k 就够，不必跟 5.6 一样拉满。只有超长单轮才需要 64k+。",
      };
    case "glm":
      return {
        headline: "GLM-5.3 · 建议 64k",
        body: "官方上限 128k，智谱默认也是 64k。编码和长推理选 64k；短对话 16k–32k。Flash 同口径。",
      };
    case "kimi":
      return {
        headline: "Kimi K3 · 建议 131k",
        body: "一直开思考，接口默认 131072，上限可以到整窗 1M，但不要拉到 1M——会占 TPM。日常 64k 或 131k。",
      };
    case "grok":
      return {
        headline: "Grok · 建议 32k",
        body: "官方没写输出上限，上下文 500k。16k–32k 够用；超长写代码再上 64k。",
      };
    default:
      return {
        headline: "建议 8k，按截断再加",
        body: "没认到官方上限就先 8k 或 16k。被截断再升到 32k / 64k。数字是每次请求的 max_tokens，压缩也会按它留空。",
      };
  }
}

// nextMaxTokensOnIdChange updates the budget only while it still looks
// like a default (unset, 8192, or the previous model's suggested /
// official size). A number the user picked is kept.
export function nextMaxTokensOnIdChange(
  nextId: string,
  prevId: string,
  current: number,
): number {
  const next = suggestedMaxTokens(nextId);
  if (next <= 0) return current;
  if (
    current <= 0 ||
    current === DEFAULT_MAX_TOKENS ||
    current === suggestedMaxTokens(prevId) ||
    current === knownMaxTokens(prevId)
  ) {
    return next;
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
