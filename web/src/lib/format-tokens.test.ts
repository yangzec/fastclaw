import assert from "node:assert/strict";
import { test } from "node:test";
import { estimateDraftTokens, formatTokenCount } from "./format-tokens.ts";

test("formatTokenCount compact units", () => {
  assert.equal(formatTokenCount(0), "0");
  assert.equal(formatTokenCount(999), "999");
  assert.equal(formatTokenCount(1200), "1.2k");
  assert.equal(formatTokenCount(12_400), "12k");
  assert.equal(formatTokenCount(1_000_000), "1M");
  assert.equal(formatTokenCount(1_050_000), "1.05M");
});

test("estimateDraftTokens is chars/4", () => {
  assert.equal(estimateDraftTokens(""), 0);
  assert.equal(estimateDraftTokens("abcd"), 1);
  assert.equal(estimateDraftTokens("abcde"), 2);
});
