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

export const CONTEXT_WINDOW_OPTIONS: { value: number; label: string }[] = [
  { value: 128_000, label: "128k" },
  { value: 200_000, label: "200k" },
  { value: 256_000, label: "256k" },
  { value: 400_000, label: "400k" },
  { value: 500_000, label: "500k" },
  { value: 1_000_000, label: "1M" },
  { value: 1_050_000, label: "1.05M" },
];

export type LimitOptionTag = "suggested" | "official" | "legacy";

export type LimitOption = {
  value: number;
  label: string;
  tag?: LimitOptionTag;
};

export function compactLimitLabel(n: number): string {
  const named = [...CONTEXT_WINDOW_OPTIONS, ...MAX_TOKEN_OPTIONS].find((o) => o.value === n);
  if (named) return named.label;
  if (n === 1_048_576) return "1.05M";
  if (n === 131_072) return "131k";
  if (n === 262_144) return "256k";
  if (n >= 1_000_000) {
    const m = n / 1_000_000;
    return (Number.isInteger(m) ? String(m) : m.toFixed(2).replace(/\.?0+$/, "")) + "M";
  }
  if (n >= 10_000) return Math.round(n / 1000) + "k";
  if (n >= 1000) {
    const k = n / 1000;
    return (Number.isInteger(k) ? String(k) : k.toFixed(1).replace(/\.0$/, "")) + "k";
  }
  return String(n);
}

export function contextWindowOptionsFor(modelId: string): LimitOption[] {
  const official = presetContextWindow(modelId);
  const values = new Set<number>(CONTEXT_WINDOW_OPTIONS.map((o) => o.value));
  if (official > 0) values.add(official);
  // Kimi's 1,048,576 and GPT's 1,050,000 both read as 1.05M — keep one chip.
  if (values.has(1_048_576)) values.delete(1_050_000);
  const family = modelLimitFamily(modelId);
  return [...values]
    .sort((a, b) => a - b)
    .map((value) => {
      let tag: LimitOptionTag | undefined;
      if (value === official) tag = "suggested";
      else if (family === "gpt-5.5" && value === 400_000) tag = "legacy";
      return { value, label: compactLimitLabel(value), tag };
    });
}

export type ModelLimitTip = { headline: string; body: string };

export function contextWindowTip(modelId: string): ModelLimitTip {
  switch (modelLimitFamily(modelId)) {
    case "gpt-5.6":
      return {
        headline: "GPT-5.6 · 建议 1.05M",
        body: "官方 API 窗口是 1.05M，Sol / Terra / Luna 一样。和 5.5 的 API 窗口相同——5.6 的差别在输出/推理，不在窗口。超过 272k 输入会加价。压缩按这个数留空。",
      };
    case "gpt-5.5":
      return {
        headline: "GPT-5.5 · 建议 1.05M（和 5.6 的差别不在这里）",
        body: "官方 API 已是 1.05M，和 5.6 一样。你记得的 400k 多半是 ChatGPT 产品档（约 272k 输入 + 128k 输出）。走官方 API 选 1.05M；网关/套餐还是 400k 就选 400k。超过 272k 输入两边都加价。",
      };
    case "glm":
      return {
        headline: "GLM-5.3 · 建议 1M",
        body: "官方 1M 上下文。Flash 同口径。压缩按保存的数字触发，不是官方默认。",
      };
    case "kimi":
      return {
        headline: "Kimi K3 · 建议 1.05M",
        body: "官方 1,048,576，输入和输出共用这一窗。窗口选满即可；真正要收的是下面的最大输出，别把输出也拉到 1M。",
      };
    case "grok":
      return {
        headline: "Grok · 建议 500k",
        body: "4.6 / 4.5 官方都是 500k，不是 1M。选 500k；更大的数压缩会算错、请求也容易 400。",
      };
    default:
      return {
        headline: "没认到就先 200k",
        body: "通用默认 200k。知道官方窗口再改。压缩用保存的数字，不认官方默认。",
      };
  }
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

export function maxTokenOptionsFor(modelId: string): LimitOption[] {
  const suggested = suggestedMaxTokens(modelId);
  const official = knownMaxTokens(modelId);
  const values = new Set<number>(MAX_TOKEN_OPTIONS.map((o) => o.value));
  if (official > 0) values.add(official);
  if (suggested > 0) values.add(suggested);
  return [...values]
    .sort((a, b) => a - b)
    .map((value) => {
      let tag: LimitOptionTag | undefined;
      if (value === suggested) tag = "suggested";
      else if (value === official) tag = "official";
      return { value, label: compactLimitLabel(value), tag };
    });
}

export function maxOutputTip(modelId: string): ModelLimitTip {
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
