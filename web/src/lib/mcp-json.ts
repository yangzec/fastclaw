import type { MCPServerConfig } from "@/lib/api";

export type ParseMCPJsonResult =
  | { ok: true; servers: Record<string, MCPServerConfig> }
  | { ok: false; error: string };

const HTTP_TYPES = new Set(["http", "sse", "streamable-http", "streamable_http"]);
const STDIO_TYPES = new Set(["stdio", "command"]);

function isRecord(v: unknown): v is Record<string, unknown> {
  return !!v && typeof v === "object" && !Array.isArray(v);
}

function stringMap(v: unknown): Record<string, string> | undefined {
  if (!isRecord(v)) return undefined;
  const out: Record<string, string> = {};
  for (const [k, val] of Object.entries(v)) {
    if (val == null) continue;
    out[k] = String(val);
  }
  return Object.keys(out).length ? out : undefined;
}

export function looksLikeServerConfig(v: unknown): v is Record<string, unknown> {
  if (!isRecord(v)) return false;
  return (
    typeof v.type === "string" ||
    typeof v.transport === "string" ||
    typeof v.command === "string" ||
    typeof v.url === "string"
  );
}

export function normalizeMCPServerType(raw: unknown): "http" | "stdio" | "" {
  const t = String(raw ?? "").trim().toLowerCase();
  if (HTTP_TYPES.has(t)) return "http";
  if (STDIO_TYPES.has(t)) return "stdio";
  return "";
}

export function normalizeMCPServer(raw: unknown): MCPServerConfig | string {
  if (!looksLikeServerConfig(raw)) return "Not an MCP server object";
  const o = raw;
  let type = normalizeMCPServerType(o.type ?? o.transport);
  if (!type) {
    if (typeof o.url === "string" && o.url.trim()) type = "http";
    else if (typeof o.command === "string" && o.command.trim()) type = "stdio";
    else return "Set type to http or stdio, or provide url / command";
  }

  const cfg: MCPServerConfig = { type };
  if (type === "http") {
    if (typeof o.url !== "string" || !o.url.trim()) {
      return "HTTP server needs url";
    }
    cfg.url = o.url.trim();
    const headers = stringMap(o.headers);
    if (headers) cfg.headers = headers;
  } else {
    if (typeof o.command !== "string" || !o.command.trim()) {
      return "stdio server needs command";
    }
    cfg.command = o.command.trim();
    if (Array.isArray(o.args)) {
      cfg.args = o.args.map((a) => String(a));
    } else if (typeof o.args === "string" && o.args.trim()) {
      cfg.args = o.args.trim().split(/\s+/);
    }
    const env = stringMap(o.env);
    if (env) cfg.env = env;
  }
  if (o.disabled === true) cfg.disabled = true;
  if (o.inherit === "all" || o.inherit === "none") cfg.inherit = o.inherit;
  return cfg;
}

function parseServerMap(
  map: Record<string, unknown>,
  allowEmpty = false,
): ParseMCPJsonResult {
  const keys = Object.keys(map);
  if (keys.length === 0) {
    if (allowEmpty) return { ok: true, servers: {} };
    return { ok: false, error: "No servers in JSON" };
  }
  const servers: Record<string, MCPServerConfig> = {};
  for (const name of keys) {
    const trimmed = name.trim();
    if (!trimmed) return { ok: false, error: "Server name is required" };
    const cfg = normalizeMCPServer(map[name]);
    if (typeof cfg === "string") return { ok: false, error: `${trimmed}: ${cfg}` };
    servers[trimmed] = cfg;
  }
  return { ok: true, servers };
}

// Accepts Cursor / Claude Desktop mcp.json, a name→config map, a single
// server object (with optional name), or { name, ...server }.
export function parseMCPServersJSON(
  text: string,
  opts?: { defaultName?: string; allowEmpty?: boolean },
): ParseMCPJsonResult {
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    return { ok: false, error: "Invalid JSON" };
  }
  if (!isRecord(parsed)) return { ok: false, error: "JSON must be an object" };

  if (isRecord(parsed.mcpServers)) {
    return parseServerMap(parsed.mcpServers, opts?.allowEmpty);
  }

  if (typeof parsed.name === "string" && looksLikeServerConfig(parsed)) {
    const cfg = normalizeMCPServer(parsed);
    if (typeof cfg === "string") return { ok: false, error: cfg };
    const name = parsed.name.trim();
    if (!name) return { ok: false, error: "Server name is required" };
    return { ok: true, servers: { [name]: cfg } };
  }

  if (looksLikeServerConfig(parsed)) {
    const name = opts?.defaultName?.trim();
    if (!name) {
      return {
        ok: false,
        error: 'Add a name, or wrap as { "my-server": { ... } }',
      };
    }
    const cfg = normalizeMCPServer(parsed);
    if (typeof cfg === "string") return { ok: false, error: cfg };
    return { ok: true, servers: { [name]: cfg } };
  }

  return parseServerMap(parsed, opts?.allowEmpty);
}

export function formatMCPServersJSON(
  servers: Record<string, MCPServerConfig>,
): string {
  return JSON.stringify({ mcpServers: servers }, null, 2);
}

// Matches internal/mcp prefixToolName: the live registry names tools
// mcp_<sanitized-server>_<original>. Used by the agent MCP cards to
// show which tools actually attached after save (no process restart).
export function mcpToolPrefix(serverName: string): string {
  const safe = serverName.replace(/[^a-zA-Z0-9_]/g, "_");
  return `mcp_${safe}_`;
}

export const MCP_JSON_PLACEHOLDER = `{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
    }
  }
}`;
