import assert from "node:assert/strict";
import { test } from "node:test";
import { FASTCLAW_PLUGINS_DIR, pluginsDirCopyText } from "./plugins-path.ts";

test("canonical plugins dir is ~/.fastclaw/plugins", () => {
  assert.equal(FASTCLAW_PLUGINS_DIR, "~/.fastclaw/plugins");
  assert.equal(pluginsDirCopyText(), "~/.fastclaw/plugins");
});

test("pluginsDirCopyText strips a trailing slash", () => {
  assert.equal(pluginsDirCopyText("~/.fastclaw/plugins/"), "~/.fastclaw/plugins");
});
