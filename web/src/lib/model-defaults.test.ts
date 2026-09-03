import assert from "node:assert/strict";
import { test } from "node:test";
import {
  knownContextWindow,
  knownMaxTokens,
  contextWindowOptionsFor,
  contextWindowTip,
  formContextWindow,
  formMaxTokens,
  maxOutputTip,
  modelLimitsTip,
  maxTokenOptionsFor,
  modelLimitFamily,
  presetContextWindow,
  presetMaxTokens,
  suggestedMaxTokens,
  nextContextWindowOnIdChange,
  nextMaxTokensOnIdChange,
  DEFAULT_CONTEXT_WINDOW,
  DEFAULT_MAX_TOKENS,
} from "./model-defaults.ts";

test("knownContextWindow covers latest GPT / Zhipu / Kimi / Grok ids", () => {
  assert.equal(knownContextWindow("gpt-5.6"), 1_050_000);
  assert.equal(knownContextWindow("openai/gpt-5.6-sol"), 1_050_000);
  assert.equal(knownContextWindow("glm-5.3"), 1_000_000);
  assert.equal(knownContextWindow("zhipu/glm-5.3-flash"), 1_000_000);
  assert.equal(knownContextWindow("kimi-k3"), 1_048_576);
  assert.equal(knownContextWindow("kimi/k3"), 1_048_576);
  assert.equal(knownContextWindow("grok-4.6"), 500_000);
  assert.equal(knownContextWindow("grok/grok-4.5"), 500_000);
  assert.equal(knownContextWindow("gpt-5.6-luna"), 1_050_000);
  assert.equal(knownContextWindow("unknown-model"), 0);
});

test("presetContextWindow falls back to the Models UI default", () => {
  assert.equal(presetContextWindow("totally-unknown"), DEFAULT_CONTEXT_WINDOW);
  assert.equal(presetContextWindow("kimi-k3"), 1_048_576);
});

test("contextWindowOptionsFor marks suggested and 5.5 legacy 400k", () => {
  const gpt56 = contextWindowOptionsFor("gpt-5.6");
  assert.equal(gpt56.find((o) => o.value === 1_050_000)?.tag, "suggested");
  const gpt55 = contextWindowOptionsFor("gpt-5.5");
  assert.equal(gpt55.find((o) => o.value === 1_050_000)?.tag, "suggested");
  assert.equal(gpt55.find((o) => o.value === 400_000)?.tag, "legacy");
  const kimi = contextWindowOptionsFor("kimi-k3");
  assert.ok(kimi.some((o) => o.value === 1_048_576 && o.tag === "suggested"));
  assert.equal(kimi.some((o) => o.value === 1_050_000), false);
});

test("contextWindowTip calls out 5.5 vs 5.6", () => {
  assert.match(contextWindowTip("gpt-5.6").body, /不在窗口/);
  assert.match(contextWindowTip("gpt-5.5").body, /400k/);
  assert.match(contextWindowTip("grok-4.6").headline, /500k/);
});

test("nextContextWindowOnIdChange keeps a manual override", () => {
  assert.equal(nextContextWindowOnIdChange("glm-5.3", "gpt-5.6", 256_000), 256_000);
  assert.equal(nextContextWindowOnIdChange("grok-4.6", "gpt-5.6", 1_050_000), 500_000);
  assert.equal(nextContextWindowOnIdChange("kimi-k3", "", 0), 1_048_576);
  assert.equal(nextContextWindowOnIdChange("kimi-k3", "unknown", DEFAULT_CONTEXT_WINDOW), 1_048_576);
});

test("knownMaxTokens covers GPT / Zhipu / Kimi ids", () => {
  assert.equal(knownMaxTokens("gpt-5.6"), 128_000);
  assert.equal(knownMaxTokens("openai/gpt-5.6-sol"), 128_000);
  assert.equal(knownMaxTokens("glm-5.3"), 128_000);
  assert.equal(knownMaxTokens("zhipu/glm-5.3-flash"), 128_000);
  assert.equal(knownMaxTokens("kimi-k3"), 131_072);
  assert.equal(knownMaxTokens("kimi/k3"), 131_072);
  assert.equal(knownMaxTokens("grok-4.6"), 0);
  assert.equal(knownMaxTokens("unknown-model"), 0);
});

test("form helpers upgrade generic 200k/8k to model defaults", () => {
  assert.equal(formContextWindow("gpt-5.6", 0), 1_050_000);
  assert.equal(formContextWindow("gpt-5.6", DEFAULT_CONTEXT_WINDOW), 1_050_000);
  assert.equal(formContextWindow("gpt-5.6", 400_000), 400_000);
  assert.equal(formMaxTokens("gpt-5.6", 0), 65_536);
  assert.equal(formMaxTokens("gpt-5.6", DEFAULT_MAX_TOKENS), 65_536);
  assert.equal(formMaxTokens("gpt-5.5", DEFAULT_MAX_TOKENS), 32_768);
  assert.equal(formMaxTokens("gpt-5.6", 128_000), 128_000);
});

test("modelLimitsTip names both defaults", () => {
  assert.match(modelLimitsTip("gpt-5.6").headline, /1\.05M/);
  assert.match(modelLimitsTip("gpt-5.6").headline, /64k/);
  assert.match(modelLimitsTip("gpt-5.5").headline, /32k/);
  assert.match(modelLimitsTip("claude-sonnet-4-7").headline, /1M/);
  assert.match(modelLimitsTip("claude-sonnet-4-7").body, /1M/);
  assert.doesNotMatch(modelLimitsTip("claude-sonnet-4-7").body, /没认到/);
  assert.match(modelLimitsTip("claude-haiku-4-5").body, /200k/);
  assert.match(modelLimitsTip("kimi-k3").headline, /131k/);
});

test("suggestedMaxTokens differs for GPT-5.5 vs 5.6", () => {
  assert.equal(modelLimitFamily("openai/gpt-5.6-sol"), "gpt-5.6");
  assert.equal(modelLimitFamily("gpt-5.5"), "gpt-5.5");
  assert.equal(suggestedMaxTokens("gpt-5.6"), 65_536);
  assert.equal(suggestedMaxTokens("gpt-5.5"), 32_768);
  assert.equal(presetMaxTokens("kimi-k3"), 131_072);
  assert.equal(presetMaxTokens("totally-unknown"), DEFAULT_MAX_TOKENS);
});

test("maxTokenOptionsFor marks suggested and official", () => {
  const gpt56 = maxTokenOptionsFor("gpt-5.6");
  assert.equal(gpt56.find((o) => o.value === 65_536)?.tag, "suggested");
  assert.equal(gpt56.find((o) => o.value === 128_000)?.tag, "official");
  const kimi = maxTokenOptionsFor("kimi-k3");
  assert.ok(kimi.some((o) => o.value === 131_072 && o.tag === "suggested"));
});

test("maxOutputTip calls out 5.5 vs 5.6", () => {
  assert.match(maxOutputTip("gpt-5.6-sol").headline, /5\.6/);
  assert.match(maxOutputTip("gpt-5.6").body, /max/);
  assert.match(maxOutputTip("gpt-5.5").body, /xhigh/);
  assert.notEqual(maxOutputTip("gpt-5.5").headline, maxOutputTip("gpt-5.6").headline);
});

test("nextMaxTokensOnIdChange keeps a manual override", () => {
  assert.equal(nextMaxTokensOnIdChange("glm-5.3", "gpt-5.6", 4096), 4096);
  assert.equal(nextMaxTokensOnIdChange("kimi-k3", "gpt-5.6", 128_000), 131_072);
  assert.equal(nextMaxTokensOnIdChange("gpt-5.5", "gpt-5.6", 65_536), 32_768);
  assert.equal(nextMaxTokensOnIdChange("kimi-k3", "", 0), 131_072);
  assert.equal(nextMaxTokensOnIdChange("kimi-k3", "unknown", DEFAULT_MAX_TOKENS), 131_072);
});
