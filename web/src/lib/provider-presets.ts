// Shared provider dropdown presets for onboard + Models pages.
// Keep labels/bases in sync with internal/agentcli.providerPreset.

export type ProviderPreset = {
  apiBase: string;
  apiType: string;
  authType: string;
  models: string[];
};

export const PROVIDER_PRESETS: Record<string, ProviderPreset> = {
  openai: {
    apiBase: "https://api.openai.com/v1",
    apiType: "openai-chat",
    authType: "bearer-token",
    models: ["gpt-5.6", "gpt-5.5"],
  },
  zhipu: {
    apiBase: "https://open.bigmodel.cn/api/paas/v4",
    apiType: "openai-chat",
    authType: "bearer-token",
    models: ["glm-5.3", "glm-5.3-flash"],
  },
  kimi: {
    apiBase: "https://api.moonshot.cn/v1",
    apiType: "openai-chat",
    authType: "bearer-token",
    models: ["kimi-k3"],
  },
  grok: {
    apiBase: "https://api.x.ai/v1",
    apiType: "openai-chat",
    authType: "bearer-token",
    models: ["grok-4.6", "grok-4.5"],
  },
  openrouter: {
    apiBase: "https://openrouter.ai/api/v1",
    apiType: "openai-chat",
    authType: "bearer-token",
    models: ["google/gemini-3-flash-preview"],
  },
  anthropic: {
    apiBase: "https://api.anthropic.com",
    apiType: "anthropic-messages",
    authType: "api-key",
    models: ["claude-opus-4-7", "claude-sonnet-4-7", "claude-haiku-4-5"],
  },
  deepseek: {
    apiBase: "https://api.deepseek.com",
    apiType: "openai-chat",
    authType: "bearer-token",
    models: ["deepseek-v4-pro", "deepseek-v4-flash"],
  },
  ollama: {
    apiBase: "http://localhost:11434/v1",
    apiType: "openai-chat",
    authType: "bearer-token",
    models: ["qwen3.5:35b-a3b-int4"],
  },
  custom: { apiBase: "", apiType: "openai-chat", authType: "bearer-token", models: [] },
};

export const PROVIDER_LABELS: Record<string, string> = {
  openai: "OpenAI (GPT)",
  zhipu: "Zhipu (智谱)",
  kimi: "Kimi (Moonshot)",
  grok: "Grok (xAI)",
  openrouter: "OpenRouter",
  anthropic: "Anthropic",
  deepseek: "DeepSeek",
  ollama: "Ollama",
  custom: "Custom",
};
