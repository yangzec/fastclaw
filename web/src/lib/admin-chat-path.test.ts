import assert from "node:assert/strict";
import { test } from "node:test";
import { adminChatHref } from "./admin-chat-path.ts";

test("adminChatHref keeps actAs and encodes path segments", () => {
  assert.equal(
    adminChatHref({ agentId: "a/1", id: "s 2", userId: "u&3" }),
    "/agents/a%2F1/chat/s%202/?actAs=u%263",
  );
});

test("adminChatHref matches the live yangzec Open deep link", () => {
  assert.equal(
    adminChatHref({
      agentId: "agt_f1574ba7ca5784e006",
      id: "s-1788677812465-rrt4kf",
      userId: "u_4f0c64c5c6507e73a680",
    }),
    "/agents/agt_f1574ba7ca5784e006/chat/s-1788677812465-rrt4kf/?actAs=u_4f0c64c5c6507e73a680",
  );
});
