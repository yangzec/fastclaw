import assert from "node:assert/strict";
import { test } from "node:test";
import { collectChatModels, shortModelLabel } from "./chat-models.ts";

test("shortModelLabel drops the provider prefix", () => {
  assert.equal(shortModelLabel("zhipu/glm-5.3"), "glm-5.3");
  assert.equal(shortModelLabel("glm-5.3"), "glm-5.3");
  assert.equal(shortModelLabel(""), "Model");
});

test("collectChatModels prefers agent rows and dedupes", () => {
  const got = collectChatModels(
    [{ name: "zhipu", models: [{ id: "glm-5.3", name: "GLM 5.3", contextWindow: 256_000 }] }],
    [
      { name: "zhipu", models: [{ id: "glm-5.3", name: "GLM 5.3 user", contextWindow: 1_000_000 }] },
      { name: "kimi", models: [{ id: "kimi-k3", name: "Kimi K3", contextWindow: 1_048_576 }] },
    ],
    [],
  );
  assert.equal(got.length, 2);
  assert.equal(got[0].value, "zhipu/glm-5.3");
  assert.equal(got[0].contextWindow, 256_000);
  assert.equal(got[1].value, "kimi/kimi-k3");
});
