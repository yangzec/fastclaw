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
import type { MCPServerConfig } from "@/lib/api";
import { inheritsToAgents } from "@/lib/api";

export type MCPEntry = { name: string } & MCPServerConfig;

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
  onSave: (entry: MCPEntry) => Promise<void>;
}) {
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
    } else {
      setName("");
      setType("stdio");
      setUrl("");
      setCommand("");
      setArgs("");
      setEnvText("");
      setHeadersText("");
      setInheritAll(false);
    }
    setError("");
  }, [open, initial]);

  const handleSubmit = async () => {
    const trimName = name.trim();
    if (!trimName) {
      setError("Name is required");
      return;
    }
    if (!initial && existingNames.includes(trimName)) {
      setError("A server with this name already exists");
      return;
    }
    if (initial && initial.name !== trimName && existingNames.includes(trimName)) {
      setError("A server with this name already exists");
      return;
    }

    const entry: MCPEntry = { name: trimName, type, disabled: initial?.disabled };
    if (showInherit) {
      entry.inherit = inheritAll ? "all" : "none";
    }
    if (type === "http") {
      if (!url.trim()) {
        setError("URL is required for HTTP type");
        return;
      }
      entry.url = url.trim();
      const h = textToKV(headersText);
      if (Object.keys(h).length > 0) entry.headers = h;
    } else {
      if (!command.trim()) {
        setError("Command is required for stdio type");
        return;
      }
      entry.command = command.trim();
      const a = args.trim();
      if (a) entry.args = a.split(/\s+/);
      const e = textToKV(envText);
      if (Object.keys(e).length > 0) entry.env = e;
    }

    setSaving(true);
    try {
      await onSave(entry);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Save failed");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{initial ? "Edit MCP Server" : "Add MCP Server"}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label>Name</Label>
            <Input
              placeholder="e.g. postgres, filesystem"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>

          <div className="space-y-1.5">
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
              <div className="space-y-1.5">
                <Label>URL</Label>
                <Input
                  placeholder="https://example.com/mcp"
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                />
              </div>
              <div className="space-y-1.5">
                <Label>
                  Headers{" "}
                  <span className="text-muted-foreground font-normal">
                    (optional, KEY=VALUE per line)
                  </span>
                </Label>
                <textarea
                  className="flex w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 min-h-[60px] resize-y"
                  placeholder={"Authorization=Bearer $TOKEN\nX-Custom=value"}
                  value={headersText}
                  onChange={(e) => setHeadersText(e.target.value)}
                  rows={3}
                />
              </div>
            </>
          ) : (
            <>
              <div className="space-y-1.5">
                <Label>Command</Label>
                <Input
                  placeholder="e.g. npx, python, node"
                  value={command}
                  onChange={(e) => setCommand(e.target.value)}
                />
              </div>
              <div className="space-y-1.5">
                <Label>
                  Arguments{" "}
                  <span className="text-muted-foreground font-normal">(space-separated)</span>
                </Label>
                <Input
                  placeholder="e.g. -y @anthropic/mcp-server-postgres postgresql://..."
                  value={args}
                  onChange={(e) => setArgs(e.target.value)}
                />
              </div>
              <div className="space-y-1.5">
                <Label>
                  Environment{" "}
                  <span className="text-muted-foreground font-normal">
                    (optional, KEY=VALUE per line)
                  </span>
                </Label>
                <textarea
                  className="flex w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 min-h-[60px] resize-y"
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
                <p className="text-xs text-muted-foreground mt-0.5">
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

          {error && <p className="text-sm text-destructive">{error}</p>}

          <div className="flex justify-end gap-2 pt-2">
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button onClick={handleSubmit} disabled={saving}>
              {saving ? "Saving..." : initial ? "Save" : "Add"}
            </Button>
          </div>
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
