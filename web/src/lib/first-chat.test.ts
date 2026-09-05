import assert from "node:assert/strict";
import { test } from "node:test";
import { firstAgentChatPath } from "./first-chat.ts";

test("firstAgentChatPath uses the first agent with an id", () => {
  assert.equal(firstAgentChatPath([{ id: "agt_1" }, { id: "agt_2" }]), "/agents/agt_1/chat/");
});

test("firstAgentChatPath skips empty ids", () => {
  assert.equal(firstAgentChatPath([{ id: "" }, { id: "agt_ok" }]), "/agents/agt_ok/chat/");
});

test("firstAgentChatPath encodes the agent id", () => {
  assert.equal(firstAgentChatPath([{ id: "agt/weird" }]), "/agents/agt%2Fweird/chat/");
});

test("firstAgentChatPath is null when there is no agent", () => {
  assert.equal(firstAgentChatPath([]), null);
  assert.equal(firstAgentChatPath(null), null);
  assert.equal(firstAgentChatPath(undefined), null);
});
