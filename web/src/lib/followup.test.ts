import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import {
  followupComposerHint,
  loadFollowupBehavior,
  loadSessionQueue,
  resolveFollowupAction,
  saveFollowupBehavior,
  saveSessionQueue,
} from "./followup.ts";

function memoryStorage() {
  const data = new Map<string, string>();
  return {
    getItem(key: string) {
      return data.has(key) ? data.get(key)! : null;
    },
    setItem(key: string, value: string) {
      data.set(key, value);
    },
    removeItem(key: string) {
      data.delete(key);
    },
    get size() {
      return data.size;
    },
    has(key: string) {
      return data.has(key);
    },
  };
}

const originalWindow = (globalThis as { window?: unknown }).window;

function installWindow() {
  const localStorage = memoryStorage();
  const sessionStorage = memoryStorage();
  (globalThis as { window: unknown }).window = { localStorage, sessionStorage };
  return { localStorage, sessionStorage };
}

afterEach(() => {
  if (originalWindow === undefined) {
    delete (globalThis as { window?: unknown }).window;
  } else {
    (globalThis as { window: unknown }).window = originalWindow;
  }
});

test("default queue mode sends to the tray, modifier steers", () => {
  assert.equal(resolveFollowupAction("queue", false), "queue");
  assert.equal(resolveFollowupAction("queue", true), "steer");
});

test("steer mode inserts, modifier queues", () => {
  assert.equal(resolveFollowupAction("steer", false), "steer");
  assert.equal(resolveFollowupAction("steer", true), "queue");
});

test("mobile composer hint names the on-screen buttons, not keyboard shortcuts", () => {
  assert.equal(followupComposerHint("queue", true), "Type, then tap Queue or Insert");
  assert.equal(followupComposerHint("steer", true), "Type, then tap Queue or Insert");
  assert.match(followupComposerHint("queue", false), /Enter to queue/);
  assert.match(followupComposerHint("steer", false), /Enter to insert/);
});

test("follow-up behavior defaults to queue unless localStorage is exactly steer", () => {
  const { localStorage } = installWindow();
  assert.equal(loadFollowupBehavior(), "queue");
  localStorage.setItem("fastclaw.followupBehavior", "queued");
  assert.equal(loadFollowupBehavior(), "queue");
  saveFollowupBehavior("steer");
  assert.equal(loadFollowupBehavior(), "steer");
  saveFollowupBehavior("queue");
  assert.equal(loadFollowupBehavior(), "queue");
});

test("session queues are isolated by agent and session", () => {
  installWindow();
  saveSessionQueue("agt_a", "s1", [{ id: "q1", text: "one" }]);
  saveSessionQueue("agt_a", "s2", [{ id: "q2", text: "two" }]);
  saveSessionQueue("agt_b", "s1", [{ id: "q3", text: "three" }]);
  assert.deepEqual(loadSessionQueue("agt_a", "s1"), [{ id: "q1", text: "one" }]);
  assert.deepEqual(loadSessionQueue("agt_a", "s2"), [{ id: "q2", text: "two" }]);
  assert.deepEqual(loadSessionQueue("agt_b", "s1"), [{ id: "q3", text: "three" }]);
});

test("empty queue removes the sessionStorage key", () => {
  const { sessionStorage } = installWindow();
  saveSessionQueue("agt_a", "s1", [{ id: "q1", text: "one" }]);
  assert.equal(sessionStorage.size, 1);
  saveSessionQueue("agt_a", "s1", []);
  assert.equal(sessionStorage.size, 0);
  assert.deepEqual(loadSessionQueue("agt_a", "s1"), []);
});

test("malformed or partial queue payloads load as empty", () => {
  const { sessionStorage } = installWindow();
  sessionStorage.setItem("fastclaw.followupQueue.agt_a\ts1", "{not-json");
  assert.deepEqual(loadSessionQueue("agt_a", "s1"), []);
  sessionStorage.setItem("fastclaw.followupQueue.agt_a\ts1", JSON.stringify({ id: "q1" }));
  assert.deepEqual(loadSessionQueue("agt_a", "s1"), []);
  sessionStorage.setItem(
    "fastclaw.followupQueue.agt_a\ts1",
    JSON.stringify([{ id: "keep", text: "ok" }, { id: 1, text: "nope" }, null]),
  );
  assert.deepEqual(loadSessionQueue("agt_a", "s1"), [{ id: "keep", text: "ok" }]);
  assert.deepEqual(loadSessionQueue("", "s1"), []);
  assert.deepEqual(loadSessionQueue("agt_a", ""), []);
});
