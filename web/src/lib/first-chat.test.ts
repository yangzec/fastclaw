import assert from "node:assert/strict";
import { test } from "node:test";
import { firstAgent, firstAgentChatPath } from "./first-chat.ts";

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

test("firstAgentChatPath prefers the oldest createdAt when the list is newest-first", () => {
  assert.equal(
    firstAgentChatPath([
      { id: "agt_new", createdAt: "2026-09-05T09:00:00Z" },
      { id: "agt_old", createdAt: "2026-09-04T09:00:00Z" },
    ]),
    "/agents/agt_old/chat/",
  );
});

test("firstAgent returns name and model for the oldest agent", () => {
  const agent = firstAgent([
    { id: "agt_new", name: "Scout", model: "openai/gpt-4o", createdAt: "2026-09-05T09:00:00Z" },
    { id: "agt_old", name: "Assistant", model: "openai/gpt-5.5", createdAt: "2026-09-04T09:00:00Z" },
  ]);
  assert.equal(agent?.id, "agt_old");
  assert.equal(agent?.name, "Assistant");
  assert.equal(agent?.model, "openai/gpt-5.5");
});
