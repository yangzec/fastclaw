import assert from "node:assert/strict";
import { test } from "node:test";
import {
  knownContextWindow,
  presetContextWindow,
  nextContextWindowOnIdChange,
  DEFAULT_CONTEXT_WINDOW,
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
