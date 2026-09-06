import assert from "node:assert/strict";
import { test } from "node:test";
import { adminChatHref } from "./admin-chat-path.ts";

test("adminChatHref keeps actAs and encodes path segments", () => {
  assert.equal(
    adminChatHref({ agentId: "a/1", id: "s 2", userId: "u&3" }),
    "/agents/a%2F1/chat/s%202/?actAs=u%263",
  );
});
