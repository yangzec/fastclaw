import assert from "node:assert/strict";
import { test } from "node:test";
import { resolveFollowupAction } from "./followup.ts";

test("default queue mode sends to the tray, modifier steers", () => {
  assert.equal(resolveFollowupAction("queue", false), "queue");
  assert.equal(resolveFollowupAction("queue", true), "steer");
});

test("steer mode inserts, modifier queues", () => {
  assert.equal(resolveFollowupAction("steer", false), "steer");
  assert.equal(resolveFollowupAction("steer", true), "queue");
});
