import assert from "node:assert/strict";
import { test } from "node:test";
import {
  applyToolCallEvent,
  applyToolResultEvent,
  hasRunningTools,
  type ResumeChatMessage,
} from "./resume-tool-events.ts";

function group(calls: ResumeChatMessage["toolCalls"]): ResumeChatMessage {
  return {
    id: "tg-1",
    role: "tool-group",
    content: "",
    timestamp: 1,
    toolCalls: calls,
  };
}

test("applyToolCallEvent creates a tool-group when none exists", () => {
  const next = applyToolCallEvent([], { id: "c1", name: "exec", arguments: "{}" });
  assert.equal(next.length, 1);
  assert.equal(next[0].role, "tool-group");
  assert.deepEqual(next[0].toolCalls, [{ id: "c1", name: "exec", arguments: "{}" }]);
});

test("applyToolCallEvent appends to a still-running group", () => {
  const prev = [group([{ id: "c1", name: "exec", arguments: "{}" }])];
  const next = applyToolCallEvent(prev, { id: "c2", name: "read_file", arguments: "{}" });
  assert.equal(next.length, 1);
  assert.deepEqual(next[0].toolCalls?.map((c) => c.id), ["c1", "c2"]);
});

test("applyToolCallEvent is a no-op when the call id already exists", () => {
  const prev = [group([{ id: "c1", name: "exec", arguments: "{}" }])];
  const next = applyToolCallEvent(prev, { id: "c1", name: "exec", arguments: "{}" });
  assert.equal(next, prev);
});

test("applyToolResultEvent fills the matching running call", () => {
  const prev = [group([{ id: "c1", name: "exec", arguments: "{}" }])];
  const next = applyToolResultEvent(prev, { id: "c1", result: "ok" });
  assert.equal(next[0].toolCalls?.[0].result, "ok");
  assert.equal(hasRunningTools(next), false);
});

test("applyToolResultEvent creates a group when the call was not in history yet", () => {
  const next = applyToolResultEvent([], { id: "c1", name: "exec", result: "ok" });
  assert.equal(next[0].toolCalls?.[0].id, "c1");
  assert.equal(next[0].toolCalls?.[0].name, "exec");
  assert.equal(next[0].toolCalls?.[0].result, "ok");
});
