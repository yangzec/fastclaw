"use client";

import { useCallback, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { Server, Plus, Trash2, Pencil, AlertTriangle } from "lucide-react";
import { Switch } from "@/components/ui/switch";
import {
  getConfig,
  getMe,
  inheritsToAgents,
  updateConfig,
  type MCPServerConfig,
} from "@/lib/api";
import {
  MCPEditDialog,
  mcpEndpoint,
  type MCPEntry,
} from "@/components/mcp-server-dialog";

export default function GlobalMCPPage() {
  const [servers, setServers] = useState<Record<string, MCPServerConfig>>({});
  const [loading, setLoading] = useState(true);
  const [editOpen, setEditOpen] = useState(false);
  const [editEntry, setEditEntry] = useState<MCPEntry | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
  const [isHosted, setIsHosted] = useState(false);
  const [isAdmin, setIsAdmin] = useState(false);
  const [inheritSaving, setInheritSaving] = useState<Record<string, boolean>>({});

  const fetchConfig = useCallback(async () => {
    setLoading(true);
    try {
      const [cfg, me] = await Promise.all([
        getConfig(),
        getMe().catch(() => null),
      ]);
      setServers(cfg.mcpServers ?? {});
      if (me?.deployMode === "hosted") setIsHosted(true);
      setIsAdmin(me?.user?.role === "super_admin");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchConfig();
  }, [fetchConfig]);

  const saveServers = async (next: Record<string, MCPServerConfig>) => {
    const res = await updateConfig({ mcpServers: next });
    if (res && typeof res === "object" && "ok" in res && res.ok === false) {
      throw new Error((res as { error?: string }).error || "Save failed");
    }
    setServers(next);
  };

  const handleSaveEntries = async (entries: MCPEntry[]) => {
    const next = { ...servers };
    if (editEntry && entries.length === 1 && editEntry.name !== entries[0].name) {
      delete next[editEntry.name];
    }
    for (const entry of entries) {
      const { name, ...cfg } = entry;
      next[name] = cfg;
    }
    await saveServers(next);
    setEditOpen(false);
    setEditEntry(null);
  };

  const handleDelete = async (name: string) => {
    const next = { ...servers };
    delete next[name];
    await saveServers(next);
    setDeleteTarget(null);
  };

  const handleInherit = async (name: string, next: boolean) => {
    const prev = servers[name];
    if (!prev) return;
    const updated = { ...prev, inherit: next ? "all" : "none" };
    setServers((m) => ({ ...m, [name]: updated }));
    setInheritSaving((m) => ({ ...m, [name]: true }));
    try {
      await saveServers({ ...servers, [name]: updated });
    } catch {
      setServers((m) => ({ ...m, [name]: prev }));
    } finally {
      setInheritSaving((m) => {
        const copy = { ...m };
        delete copy[name];
        return copy;
      });
    }
  };

  if (loading) {
    return (
      <div className="p-6 space-y-6 max-w-5xl mx-auto">
        <Skeleton className="h-10 w-48" />
        <Skeleton className="h-48 w-full" />
      </div>
    );
  }

  const entries = Object.entries(servers);
  const hasStdio = entries.some(([, cfg]) => cfg.type === "stdio");

  return (
    <div className="p-6 space-y-6 max-w-5xl mx-auto">
      {isHosted && hasStdio && (
        <div className="flex items-start gap-2 rounded-lg border border-yellow-500/30 bg-yellow-500/5 p-3 text-sm">
          <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0 text-yellow-500" />
          <div>
            <span className="font-medium">stdio servers may not work in cloud deployments.</span>{" "}
            stdio MCP servers run as local subprocesses and are not shared across instances.
            Use <strong>http</strong> type for distributed environments.
          </div>
        </div>
      )}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">MCP Servers</h2>
          <p className="text-sm text-muted-foreground mt-1">
            {isAdmin
              ? "Platform catalog. Paste a Cursor mcp.json or fill the form. Share with agents attaches a server to every tenant. Off keeps it catalog-only — other tenants never see the definition or its secrets."
              : "Your catalog. Paste a Cursor mcp.json or fill the form. Share with agents attaches a server to your agents only. Off keeps it here until an agent adds it itself."}
          </p>
        </div>
        <Button
          size="sm"
          onClick={() => {
            setEditEntry(null);
            setEditOpen(true);
          }}
        >
          <Plus className="w-4 h-4 mr-1" /> Add Server
        </Button>
      </div>

      {entries.length === 0 ? (
        <div className="rounded-lg border border-dashed p-12 text-center text-muted-foreground">
          <Server className="w-10 h-10 mx-auto mb-3 opacity-40" />
          <p>No global MCP servers configured.</p>
          <p className="text-xs mt-1">
            Paste a Cursor-style mcp.json, or add a server. New ones stay
            catalog-only until you turn on Share with agents.
          </p>
        </div>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {entries.map(([name, cfg]) => (
            <div key={name} className="rounded-lg border bg-card p-4 space-y-2">
              <div className="flex items-start justify-between gap-2">
                <div className="flex items-center gap-2 min-w-0">
                  <Server className="w-4 h-4 shrink-0 text-muted-foreground" />
                  <span className="font-medium truncate">{name}</span>
                </div>
                <Badge variant="secondary" className="shrink-0 text-xs">
                  {cfg.type}
                </Badge>
              </div>
              <div className="text-xs text-muted-foreground truncate">
                {mcpEndpoint(cfg)}
              </div>
              {cfg.env && Object.keys(cfg.env).length > 0 && (
                <div className="text-xs text-muted-foreground">
                  env: {Object.keys(cfg.env).join(", ")}
                </div>
              )}
              {cfg.headers && Object.keys(cfg.headers).length > 0 && (
                <div className="text-xs text-muted-foreground">
                  headers: {Object.keys(cfg.headers).join(", ")}
                </div>
              )}
              <div className="flex items-center justify-between gap-2 pt-1">
                <div className="flex items-center gap-2">
                  <Switch
                    checked={inheritsToAgents(cfg.inherit)}
                    onCheckedChange={(v) => handleInherit(name, v)}
                    disabled={inheritSaving[name] === true}
                    aria-label={`Share ${name} with agents`}
                  />
                  <span className="text-xs text-muted-foreground">
                    {inheritsToAgents(cfg.inherit) ? "Shared" : "Catalog only"}
                  </span>
                </div>
              </div>
              <div className="flex gap-1 pt-1">
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7"
                  onClick={() => {
                    setEditEntry({ name, ...cfg });
                    setEditOpen(true);
                  }}
                >
                  <Pencil className="w-3.5 h-3.5" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7 text-destructive"
                  onClick={() => setDeleteTarget(name)}
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}

      <MCPEditDialog
        open={editOpen}
        onOpenChange={(o) => {
          if (!o) setEditEntry(null);
          setEditOpen(o);
        }}
        initial={editEntry}
        existingNames={Object.keys(servers)}
        showInherit
        onSave={handleSaveEntries}
      />

      <AlertDialog
        open={!!deleteTarget}
        onOpenChange={(o) => !o && setDeleteTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Remove MCP server</AlertDialogTitle>
            <AlertDialogDescription>
              Remove <strong>{deleteTarget}</strong> from this catalog?
              Agents that inherited it will lose its tools unless they
              have their own overlay.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground"
              onClick={() => deleteTarget && handleDelete(deleteTarget)}
            >
              Remove
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
