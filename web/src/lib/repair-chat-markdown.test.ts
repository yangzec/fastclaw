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

test("unclosed fence that swallowed prose is stripped, not shown", () => {
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
  assert.doesNotMatch(out, /```/);
  assert.doesNotMatch(out, /\u200b/);
  assert.match(out, /^真正的报错是：/m);
  assert.match(out, /请求打模型/);
  assert.match(out, /\*\*23:04 \/ 23:07\*\*/);
});

test("mid-line triple backticks become a line break", () => {
  const src = 'API error 400: {"error":"x"}```请求打模型';
  const out = repairChatMarkdown(src);
  assert.doesNotMatch(out, /```/);
  assert.doesNotMatch(out, /\u200b/);
  assert.equal(out, 'API error 400: {"error":"x"}\n请求打模型');
});

test("mid-line fences inside a real code block stay put", () => {
  const src = "log:\n```\nerror: foo```bar\n```\ndone";
  assert.equal(repairChatMarkdown(src), src);
});

test("unwraps a whole-message markdown fence", () => {
  const src = "```markdown\n# 结论\n\n这是 **重点**\n\n- `agt_abc`: 当前 agent\n```";
  const out = repairChatMarkdown(src);
  assert.equal(out, "# 结论\n\n这是 **重点**\n\n- `agt_abc`: 当前 agent");
});

test("unwraps a whole-message empty fence around prose", () => {
  const src = "```\n**结论**\n\n- `s-1`: 会话\n2. **下一步**\n```";
  const out = repairChatMarkdown(src);
  assert.doesNotMatch(out, /```/);
  assert.match(out, /\*\*结论\*\*/);
});

test("does not unwrap a whole-message language fence", () => {
  const src = "```python\nprint(1)\nprint(2)\n```";
  assert.equal(repairChatMarkdown(src), src);
});

test("does not unwrap a fence that is only part of the reply", () => {
  const src = "先看这段：\n```markdown\n# Title\n- item\n```\n以上。";
  assert.equal(repairChatMarkdown(src), src);
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
