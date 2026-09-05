import assert from "node:assert/strict";
import { test } from "node:test";
import { formatTokenCount } from "./format-tokens.ts";
import {
  formatTurnUsageHint,
  formatTurnUsageLine,
  parseTurnUsage,
} from "./turn-usage.ts";

test("parseTurnUsage reads done.data.usage", () => {
  const u = parseTurnUsage({
    usage: { inputTokens: 12400, outputTokens: 890, cacheReadTokens: 32, requestCount: 2 },
  });
  assert.deepEqual(u, {
    inputTokens: 12400,
    outputTokens: 890,
    cacheReadTokens: 32,
    cacheCreationTokens: 0,
    requestCount: 2,
  });
});

test("parseTurnUsage ignores empty totals", () => {
  assert.equal(parseTurnUsage({ usage: { inputTokens: 0, outputTokens: 0 } }), null);
  assert.equal(parseTurnUsage(undefined), null);
});

test("formatTurnUsageLine is quiet in→out", () => {
  assert.equal(formatTurnUsageLine({ inputTokens: 12400, outputTokens: 890 }), `${formatTokenCount(12400)} → ${formatTokenCount(890)}`);
  assert.equal(formatTokenCount(999), "999");
  assert.equal(formatTokenCount(1000), "1k");
});

test("formatTurnUsageHint lists cache and extra calls", () => {
  assert.equal(
    formatTurnUsageHint({
      inputTokens: 100,
      outputTokens: 20,
      cacheReadTokens: 8,
      requestCount: 2,
    }),
    "in 100 · out 20 · cache read 8 · 2 calls",
  );
});
