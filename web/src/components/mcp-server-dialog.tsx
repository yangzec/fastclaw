"use client";

import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { MCPServerConfig } from "@/lib/api";
import { inheritsToAgents } from "@/lib/api";
import {
  formatMCPServersJSON,
  MCP_JSON_PLACEHOLDER,
  parseMCPServersJSON,
} from "@/lib/mcp-json";

export type MCPEntry = { name: string } & MCPServerConfig;

function entryBody(entry: MCPEntry): MCPServerConfig {
  const { name: _name, ...cfg } = entry;
  return cfg;
}

function entriesFromForm(opts: {
  name: string;
  type: "http" | "stdio";
  url: string;
  command: string;
  args: string;
  envText: string;
  headersText: string;
  inheritAll: boolean;
  showInherit?: boolean;
  disabled?: boolean;
}): MCPEntry | string {
  const trimName = opts.name.trim();
  if (!trimName) return "Name is required";
  const entry: MCPEntry = { name: trimName, type: opts.type, disabled: opts.disabled };
  if (opts.showInherit) {
    entry.inherit = opts.inheritAll ? "all" : "none";
  }
  if (opts.type === "http") {
    if (!opts.url.trim()) return "URL is required for HTTP type";
    entry.url = opts.url.trim();
    const h = textToKV(opts.headersText);
    if (Object.keys(h).length > 0) entry.headers = h;
  } else {
    if (!opts.command.trim()) return "Command is required for stdio type";
    entry.command = opts.command.trim();
    const a = opts.args.trim();
    if (a) entry.args = a.split(/\s+/);
    const e = textToKV(opts.envText);
    if (Object.keys(e).length > 0) entry.env = e;
  }
  return entry;
}

export function MCPEditDialog({
  open,
  onOpenChange,
  initial,
  existingNames,
  showInherit,
  onSave,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  initial: MCPEntry | null;
  existingNames: string[];
  showInherit?: boolean;
  onSave: (entries: MCPEntry[]) => Promise<void>;
}) {
  const [mode, setMode] = useState<"json" | "form">("json");
  const [jsonText, setJsonText] = useState("");
  const [name, setName] = useState("");
  const [type, setType] = useState<"http" | "stdio">("stdio");
  const [url, setUrl] = useState("");
  const [command, setCommand] = useState("");
  const [args, setArgs] = useState("");
  const [envText, setEnvText] = useState("");
  const [headersText, setHeadersText] = useState("");
  const [inheritAll, setInheritAll] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) return;
    if (initial) {
      setName(initial.name);
      setType(initial.type || "stdio");
      setUrl(initial.url ?? "");
      setCommand(initial.command ?? "");
      setArgs((initial.args ?? []).join(" "));
      setEnvText(kvToText(initial.env));
      setHeadersText(kvToText(initial.headers));
      setInheritAll(inheritsToAgents(initial.inherit));
      setJsonText(formatMCPServersJSON({ [initial.name]: entryBody(initial) }));
    } else {
      setName("");
      setType("stdio");
      setUrl("");
      setCommand("");
      setArgs("");
      setEnvText("");
      setHeadersText("");
      setInheritAll(false);
      setJsonText("");
    }
    setMode("json");
    setError("");
  }, [open, initial]);

  const applyFormToJson = () => {
    const built = entriesFromForm({
      name,
      type,
      url,
      command,
      args,
      envText,
      headersText,
      inheritAll,
      showInherit,
      disabled: initial?.disabled,
    });
    if (typeof built === "string") return;
    setJsonText(formatMCPServersJSON({ [built.name]: entryBody(built) }));
  };

  const applyJsonToForm = () => {
    const parsed = parseMCPServersJSON(jsonText, { defaultName: name || initial?.name });
    if (!parsed.ok) return;
    const names = Object.keys(parsed.servers);
    if (names.length !== 1) return;
    const n = names[0];
    const cfg = parsed.servers[n];
    setName(n);
    setType(cfg.type || "stdio");
    setUrl(cfg.url ?? "");
    setCommand(cfg.command ?? "");
    setArgs((cfg.args ?? []).join(" "));
    setEnvText(kvToText(cfg.env));
    setHeadersText(kvToText(cfg.headers));
    if (showInherit) setInheritAll(inheritsToAgents(cfg.inherit));
  };

  const handleModeChange = (next: string) => {
    const tab = next === "form" ? "form" : "json";
    if (tab === "json" && mode === "form") applyFormToJson();
    if (tab === "form" && mode === "json") applyJsonToForm();
    setMode(tab);
    setError("");
  };

  const collectEntries = (): MCPEntry[] | string => {
    if (mode === "form") {
      const built = entriesFromForm({
        name,
        type,
        url,
        command,
        args,
        envText,
        headersText,
        inheritAll,
        showInherit,
        disabled: initial?.disabled,
      });
      if (typeof built === "string") return built;
      return [built];
    }

    const parsed = parseMCPServersJSON(jsonText, {
      defaultName: name || initial?.name,
    });
    if (!parsed.ok) return parsed.error;
    const entries: MCPEntry[] = [];
    for (const [n, cfg] of Object.entries(parsed.servers)) {
      const entry: MCPEntry = { name: n, ...cfg };
      if (showInherit) {
        if (entry.inherit !== "all" && entry.inherit !== "none") {
          entry.inherit = inheritAll ? "all" : "none";
        }
      } else {
        delete entry.inherit;
      }
      if (
        initial?.disabled &&
        Object.keys(parsed.servers).length === 1
      ) {
        entry.disabled = initial.disabled;
      }
      entries.push(entry);
    }
    if (entries.length === 0) return "No servers in JSON";
    return entries;
  };

  const handleSubmit = async () => {
    const collected = collectEntries();
    if (typeof collected === "string") {
      setError(collected);
      return;
    }
    if (
      initial &&
      collected.length === 1 &&
      initial.name !== collected[0].name &&
      existingNames.includes(collected[0].name)
    ) {
      setError(`A server named "${collected[0].name}" already exists`);
      return;
    }

    setSaving(true);
    try {
      await onSave(collected);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Save failed");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{initial ? "Edit MCP Server" : "Add MCP Server"}</DialogTitle>
        </DialogHeader>
        <Tabs value={mode} onValueChange={handleModeChange}>
          <TabsList>
            <TabsTrigger value="json">JSON</TabsTrigger>
            <TabsTrigger value="form">Form</TabsTrigger>
          </TabsList>
          <TabsContent value="json" className="flex flex-col gap-4 pt-3">
            <p className="text-xs text-muted-foreground">
              Paste a Cursor / Claude Desktop{" "}
              <code className="text-[11px]">mcp.json</code> snippet, a
              name→config map, or a single server object.
            </p>
            <textarea
              className="min-h-[220px] w-full resize-y rounded-md border border-input bg-background px-3 py-2 font-mono text-xs leading-5 ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
              placeholder={MCP_JSON_PLACEHOLDER}
              value={jsonText}
              onChange={(e) => setJsonText(e.target.value)}
              spellCheck={false}
            />
            {showInherit && (
              <div className="flex items-center justify-between gap-3 rounded-md border p-3">
                <div className="min-w-0">
                  <Label>Share with agents</Label>
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    Applied when JSON omits <code className="text-[11px]">inherit</code>.
                    Off keeps servers catalog-only.
                  </p>
                </div>
                <Switch
                  checked={inheritAll}
                  onCheckedChange={setInheritAll}
                  aria-label="Share MCP server with agents"
                />
              </div>
            )}
          </TabsContent>
          <TabsContent value="form" className="flex flex-col gap-4 pt-3">
            <div className="flex flex-col gap-1.5">
              <Label>Name</Label>
              <Input
                placeholder="e.g. postgres, filesystem"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>

            <div className="flex flex-col gap-1.5">
              <Label>Type</Label>
              <Select value={type} onValueChange={(v) => setType(v as "http" | "stdio")}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="stdio">stdio</SelectItem>
                  <SelectItem value="http">http</SelectItem>
                </SelectContent>
              </Select>
            </div>

            {type === "http" ? (
              <>
                <div className="flex flex-col gap-1.5">
                  <Label>URL</Label>
                  <Input
                    placeholder="https://example.com/mcp"
                    value={url}
                    onChange={(e) => setUrl(e.target.value)}
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <Label>
                    Headers{" "}
                    <span className="font-normal text-muted-foreground">
                      (optional, KEY=VALUE per line)
                    </span>
                  </Label>
                  <textarea
                    className="min-h-[60px] w-full resize-y rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                    placeholder={"Authorization=Bearer $TOKEN\nX-Custom=value"}
                    value={headersText}
                    onChange={(e) => setHeadersText(e.target.value)}
                    rows={3}
                  />
                </div>
              </>
            ) : (
              <>
                <div className="flex flex-col gap-1.5">
                  <Label>Command</Label>
                  <Input
                    placeholder="e.g. npx, python, node"
                    value={command}
                    onChange={(e) => setCommand(e.target.value)}
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <Label>
                    Arguments{" "}
                    <span className="font-normal text-muted-foreground">(space-separated)</span>
                  </Label>
                  <Input
                    placeholder="e.g. -y @anthropic/mcp-server-postgres postgresql://..."
                    value={args}
                    onChange={(e) => setArgs(e.target.value)}
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <Label>
                    Environment{" "}
                    <span className="font-normal text-muted-foreground">
                      (optional, KEY=VALUE per line)
                    </span>
                  </Label>
                  <textarea
                    className="min-h-[60px] w-full resize-y rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                    placeholder={"DATABASE_URL=postgresql://...\nAPI_KEY=$SECRET"}
                    value={envText}
                    onChange={(e) => setEnvText(e.target.value)}
                    rows={3}
                  />
                </div>
              </>
            )}

            {showInherit && (
              <div className="flex items-center justify-between gap-3 rounded-md border p-3">
                <div className="min-w-0">
                  <Label>Share with agents</Label>
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    Off keeps this server in the catalog. On attaches it
                    automatically to agents that can see this row.
                  </p>
                </div>
                <Switch
                  checked={inheritAll}
                  onCheckedChange={setInheritAll}
                  aria-label="Share MCP server with agents"
                />
              </div>
            )}
          </TabsContent>
        </Tabs>

        {error && <p className="text-sm text-destructive">{error}</p>}

        <div className="flex justify-end gap-2 pt-2">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={saving}>
            {saving ? "Saving..." : initial ? "Save" : "Add"}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

export function kvToText(kv?: Record<string, string>): string {
  if (!kv) return "";
  return Object.entries(kv)
    .map(([k, v]) => `${k}=${v}`)
    .join("\n");
}

export function textToKV(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of text.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const idx = trimmed.indexOf("=");
    if (idx <= 0) continue;
    out[trimmed.slice(0, idx).trim()] = trimmed.slice(idx + 1);
  }
  return out;
}

export function mcpEndpoint(cfg: MCPServerConfig): string {
  return cfg.type === "http"
    ? cfg.url || "(no URL)"
    : [cfg.command, ...(cfg.args ?? [])].join(" ");
}
