"use client";

import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import type { MCPServerConfig } from "@/lib/api";
import {
  formatMCPServersJSON,
  MCP_JSON_PLACEHOLDER,
  parseMCPServersJSON,
} from "@/lib/mcp-json";

export function MCPJsonPanel({
  servers,
  hint,
  stripInherit,
  onSave,
}: {
  servers: Record<string, MCPServerConfig>;
  hint?: string;
  stripInherit?: boolean;
  onSave: (next: Record<string, MCPServerConfig>) => Promise<void>;
}) {
  const [text, setText] = useState("");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setText(
      Object.keys(servers).length > 0
        ? formatMCPServersJSON(servers)
        : "",
    );
    setError("");
  }, [servers]);

  const handleSave = async () => {
    const parsed = parseMCPServersJSON(text, { allowEmpty: true });
    if (!parsed.ok) {
      setError(parsed.error);
      return;
    }
    const next: Record<string, MCPServerConfig> = {};
    for (const [name, cfg] of Object.entries(parsed.servers)) {
      const copy = { ...cfg };
      if (stripInherit) delete copy.inherit;
      next[name] = copy;
    }
    setSaving(true);
    setError("");
    try {
      await onSave(next);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Save failed");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex flex-col gap-3 rounded-lg border bg-card p-4">
      <div className="flex flex-col gap-1">
        <Label>mcp.json</Label>
        <p className="text-xs text-muted-foreground">
          {hint ??
            "Paste a Cursor / Claude Desktop mcp.json here and save. type is optional."}
        </p>
      </div>
      <textarea
        className="min-h-[280px] w-full resize-y rounded-md border border-input bg-background px-3 py-2 font-mono text-xs leading-5 ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
        placeholder={MCP_JSON_PLACEHOLDER}
        value={text}
        onChange={(e) => setText(e.target.value)}
        spellCheck={false}
      />
      {error && <p className="text-sm text-destructive">{error}</p>}
      <div className="flex justify-end">
        <Button onClick={handleSave} disabled={saving}>
          {saving ? "Saving..." : "Save JSON"}
        </Button>
      </div>
    </div>
  );
}
