import assert from "node:assert/strict";
import { test } from "node:test";
import {
  knownContextWindow,
  knownMaxTokens,
  presetContextWindow,
  presetMaxTokens,
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
  assert.equal(knownContextWindow("unknown-model"), 0);
});

test("presetContextWindow falls back to the Models UI default", () => {
  assert.equal(presetContextWindow("totally-unknown"), DEFAULT_CONTEXT_WINDOW);
  assert.equal(presetContextWindow("kimi-k3"), 1_048_576);
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

test("presetMaxTokens falls back to 8192", () => {
  assert.equal(presetMaxTokens("totally-unknown"), DEFAULT_MAX_TOKENS);
  assert.equal(presetMaxTokens("kimi-k3"), 131_072);
});

test("nextMaxTokensOnIdChange keeps a manual override", () => {
  assert.equal(nextMaxTokensOnIdChange("glm-5.3", "gpt-5.6", 4096), 4096);
  assert.equal(nextMaxTokensOnIdChange("kimi-k3", "gpt-5.6", 128_000), 131_072);
  assert.equal(nextMaxTokensOnIdChange("kimi-k3", "", 0), 131_072);
  assert.equal(nextMaxTokensOnIdChange("kimi-k3", "unknown", DEFAULT_MAX_TOKENS), 131_072);
});
