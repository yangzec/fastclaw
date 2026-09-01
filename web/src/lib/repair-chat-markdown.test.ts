import assert from "node:assert/strict";
import { test } from "node:test";
import { looksLikeChatProse, repairChatMarkdown } from "./repair-chat-markdown.ts";

test("leaves balanced fences alone", () => {
  const src = "before\n```js\nconst x = 1;\n```\nafter **bold**";
  assert.equal(repairChatMarkdown(src), src);
});

test("leaves a real unclosed code dump alone", () => {
  const src = "here is the log:\n```\nAPI error 400: connection reset\nstack at foo.ts:12\n";
  assert.equal(repairChatMarkdown(src), src);
});

test("unclosed fence that swallowed prose is neutralized", () => {
  const src = [
    "真正的报错是：",
    "```",
    'API error 400: {"error":"bad"}```请求打模型',
    "",
    "- `agt_abc`: 当前 agent",
    "2. **23:04 / 23:07**",
    "   `Upstream error: 400`",
  ].join("\n");
  const out = repairChatMarkdown(src);
  assert.match(out, /\u200b/);
  assert.doesNotMatch(out.split("\n")[1], /^```/);
  assert.match(out, /\*\*23:04 \/ 23:07\*\*/);
});

test("mid-line triple backticks do not stay as a fence starter", () => {
  const src = 'API error 400: {"error":"x"}```请求打模型';
  const out = repairChatMarkdown(src);
  assert.match(out, /\u200b/);
  assert.doesNotMatch(out, /^\s*```/);
});

test("looksLikeChatProse detects the screenshot shape", () => {
  const body = [
    "- `agt_a953ba334564fae62f97`: 当前 agent",
    "2. **23:04 / 23:07**",
    "`tokens=403022`",
  ].join("\n");
  assert.equal(looksLikeChatProse(body), true);
  assert.equal(looksLikeChatProse("API error 400: connection reset by peer"), false);
});
