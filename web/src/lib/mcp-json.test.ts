import assert from "node:assert/strict";
import { test } from "node:test";
import {
  formatMCPServersJSON,
  looksLikeServerConfig,
  mcpToolPrefix,
  normalizeMCPServer,
  normalizeMCPServerType,
  parseMCPServersJSON,
} from "./mcp-json.ts";

test("normalizeMCPServerType maps Cursor / SSE aliases to http", () => {
  assert.equal(normalizeMCPServerType("http"), "http");
  assert.equal(normalizeMCPServerType("SSE"), "http");
  assert.equal(normalizeMCPServerType("streamable-http"), "http");
  assert.equal(normalizeMCPServerType("stdio"), "stdio");
  assert.equal(normalizeMCPServerType("command"), "stdio");
  assert.equal(normalizeMCPServerType(""), "");
});

test("normalizeMCPServer infers type from url or command", () => {
  const http = normalizeMCPServer({ url: "https://example.com/mcp" });
  assert.deepEqual(http, { type: "http", url: "https://example.com/mcp" });

  const stdio = normalizeMCPServer({
    command: "npx",
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
  });
  assert.deepEqual(stdio, {
    type: "stdio",
    command: "npx",
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
  });
});

test("parseMCPServersJSON accepts Cursor mcp.json", () => {
  const parsed = parseMCPServersJSON(`{
    "mcpServers": {
      "filesystem": {
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
      },
      "remote": {
        "url": "https://api.example.com/mcp",
        "headers": { "Authorization": "Bearer tok" }
      }
    }
  }`);
  assert.equal(parsed.ok, true);
  if (!parsed.ok) return;
  assert.deepEqual(parsed.servers.filesystem, {
    type: "stdio",
    command: "npx",
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
  });
  assert.deepEqual(parsed.servers.remote, {
    type: "http",
    url: "https://api.example.com/mcp",
    headers: { Authorization: "Bearer tok" },
  });
});

test("parseMCPServersJSON accepts a name→config map and a named object", () => {
  const map = parseMCPServersJSON(`{
    "postgres": { "type": "stdio", "command": "npx", "args": ["-y", "pg"] }
  }`);
  assert.equal(map.ok, true);
  if (map.ok) assert.equal(map.servers.postgres.command, "npx");

  const named = parseMCPServersJSON(`{
    "name": "github",
    "type": "http",
    "url": "https://mcp.example/github"
  }`);
  assert.equal(named.ok, true);
  if (named.ok) assert.equal(named.servers.github.url, "https://mcp.example/github");
});

test("parseMCPServersJSON uses defaultName for a bare server object", () => {
  const missing = parseMCPServersJSON(`{ "command": "uvx", "args": ["mcp"] }`);
  assert.equal(missing.ok, false);

  const named = parseMCPServersJSON(`{ "command": "uvx", "args": ["mcp"] }`, {
    defaultName: "tools",
  });
  assert.equal(named.ok, true);
  if (named.ok) {
    assert.equal(named.servers.tools.type, "stdio");
    assert.equal(named.servers.tools.command, "uvx");
  }
});

test("parseMCPServersJSON keeps inherit and disabled", () => {
  const parsed = parseMCPServersJSON(`{
    "mcpServers": {
      "shared": { "command": "npx", "inherit": "all" },
      "off": { "url": "https://x", "disabled": true }
    }
  }`);
  assert.equal(parsed.ok, true);
  if (!parsed.ok) return;
  assert.equal(parsed.servers.shared.inherit, "all");
  assert.equal(parsed.servers.off.disabled, true);
  assert.equal(parsed.servers.off.type, "http");
});

test("parseMCPServersJSON rejects invalid JSON and empty maps", () => {
  assert.equal(parseMCPServersJSON("not-json").ok, false);
  assert.equal(parseMCPServersJSON("[]").ok, false);
  assert.equal(parseMCPServersJSON("{ \"mcpServers\": {} }").ok, false);
  assert.equal(looksLikeServerConfig({ foo: 1 }), false);
});

test("parseMCPServersJSON allowEmpty accepts an empty catalog", () => {
  const parsed = parseMCPServersJSON('{ "mcpServers": {} }', { allowEmpty: true });
  assert.equal(parsed.ok, true);
  if (parsed.ok) assert.deepEqual(parsed.servers, {});
});

test("formatMCPServersJSON wraps the Cursor-style envelope", () => {
  const text = formatMCPServersJSON({
    demo: { type: "stdio", command: "npx" },
  });
  assert.deepEqual(JSON.parse(text), {
    mcpServers: { demo: { type: "stdio", command: "npx" } },
  });
});

test("mcpToolPrefix matches the runtime mcp_<server>_ tool names", () => {
  assert.equal(mcpToolPrefix("serper"), "mcp_serper_");
  assert.equal(mcpToolPrefix("my-server"), "mcp_my_server_");
});
