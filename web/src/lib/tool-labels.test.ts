import assert from "node:assert/strict";
import { test } from "node:test";
import {
  categoryDisplayName,
  chainRefFor,
  emptyChainMessage,
  humanizeToolName,
  isProviderConfigured,
} from "./tool-labels.ts";

test("categoryDisplayName prefers Label over raw name", () => {
  assert.equal(
    categoryDisplayName({ name: "web_search", label: "Web Search" }),
    "Web Search",
  );
  assert.equal(categoryDisplayName({ name: "web_search", label: "" }), "Web Search");
  assert.equal(categoryDisplayName({ name: "image_gen" }), "Image Gen");
});

test("humanizeToolName never concatenates name+tool", () => {
  assert.equal(humanizeToolName("web_search"), "Web Search");
  assert.notEqual(humanizeToolName("web_search") + "tool", "web_searchtool");
});

test("isProviderConfigured treats keys/urls and built-ins, not none", () => {
  assert.equal(
    isProviderConfigured({ name: "exa", needsKey: true }, { apiKey: "sk-test" }),
    true,
  );
  assert.equal(
    isProviderConfigured({ name: "exa", needsKey: true }, { apiKey: "  " }),
    false,
  );
  assert.equal(
    isProviderConfigured(
      { name: "searxng", needsUrl: true },
      { endpoint: "https://searx.example" },
    ),
    true,
  );
  assert.equal(isProviderConfigured({ name: "direct" }, {}), true);
  assert.equal(isProviderConfigured({ name: "none" }, {}), false);
});

test("chainRefFor uses typed model or the first catalog model", () => {
  assert.equal(
    chainRefFor({ name: "exa", models: ["auto", "neural"] }, { options: { model: "neural" } }),
    "exa/neural",
  );
  assert.equal(chainRefFor({ name: "direct", models: ["default"] }, {}), "direct/default");
  assert.equal(chainRefFor({ name: "exa", models: ["auto", "neural"] }, {}), "exa/auto");
  assert.equal(chainRefFor({ name: "custom" }, {}), null);
});

test("emptyChainMessage distinguishes configured vs not in chain", () => {
  assert.match(emptyChainMessage(true), /aren't in the fallback chain/i);
  assert.match(emptyChainMessage(false), /Add a provider key/i);
});
