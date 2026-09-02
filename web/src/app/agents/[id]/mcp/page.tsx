"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
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
import { Server, Plus, Trash2, Pencil, AlertTriangle, Undo2 } from "lucide-react";
import {
  getAgentConfig,
  getConfig,
  getMe,
  updateAgent,
  type MCPServerConfig,
} from "@/lib/api";
import { useAgentIdFromURL } from "@/hooks/use-agent-id";
import { useAgentName } from "@/hooks/use-agent-name";
import {
  MCPEditDialog,
  mcpEndpoint,
  type MCPEntry,
} from "@/components/mcp-server-dialog";

type Row = MCPEntry & {
  source: "inherited" | "agent";
  shadowed?: boolean;
};

export default function AgentMCPPage() {
  const agentId = useAgentIdFromURL();
  const agentName = useAgentName(agentId);
  const [globalServers, setGlobalServers] = useState<Record<string, MCPServerConfig>>({});
  const [localServers, setLocalServers] = useState<Record<string, MCPServerConfig>>({});
  const [loading, setLoading] = useState(true);
  const [editOpen, setEditOpen] = useState(false);
  const [editEntry, setEditEntry] = useState<MCPEntry | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
  const [isHosted, setIsHosted] = useState(false);

  const fetchConfig = useCallback(async () => {
    if (!agentId) return;
    setLoading(true);
    try {
      const [agentCfg, globalCfg, me] = await Promise.all([
        getAgentConfig(agentId),
        getConfig().catch(() => null),
        getMe().catch(() => null),
      ]);
      setLocalServers(agentCfg.mcpServers ?? {});
      setGlobalServers(globalCfg?.mcpServers ?? {});
      if (me?.deployMode === "hosted") setIsHosted(true);
    } finally {
      setLoading(false);
    }
  }, [agentId]);

  useEffect(() => {
    fetchConfig();
  }, [fetchConfig]);

  const rows = useMemo<Row[]>(() => {
    const names = new Set([
      ...Object.keys(globalServers),
      ...Object.keys(localServers),
    ]);
    const out: Row[] = [];
    for (const name of names) {
      const local = localServers[name];
      const inherited = globalServers[name];
      if (local) {
        out.push({
          name,
          ...local,
          source: "agent",
          shadowed: !!inherited,
        });
      } else if (inherited) {
        out.push({ name, ...inherited, source: "inherited" });
      }
    }
    return out.sort((a, b) => a.name.localeCompare(b.name));
  }, [globalServers, localServers]);

  const saveLocal = async (next: Record<string, MCPServerConfig>) => {
    await updateAgent(agentId, { mcpServers: next });
    setLocalServers(next);
  };

  const handleSaveEntry = async (entry: MCPEntry) => {
    const { name, ...cfg } = entry;
    const next = { ...localServers };
    if (editEntry && editEntry.name !== name) {
      delete next[editEntry.name];
    }
    next[name] = cfg;
    await saveLocal(next);
    setEditOpen(false);
    setEditEntry(null);
  };

  const handleDeleteLocal = async (name: string) => {
    const next = { ...localServers };
    delete next[name];
    await saveLocal(next);
    setDeleteTarget(null);
  };

  const handleDisableInherited = async (row: Row) => {
    await saveLocal({
      ...localServers,
      [row.name]: { ...stripMeta(row), disabled: true },
    });
  };

  const handleResetInherit = async (name: string) => {
    const next = { ...localServers };
    delete next[name];
    await saveLocal(next);
  };

  if (loading) {
    return (
      <div className="p-6 space-y-6 max-w-5xl mx-auto">
        <Skeleton className="h-10 w-48" />
        <Skeleton className="h-48 w-full" />
      </div>
    );
  }

  const hasStdio = rows.some((r) => r.type === "stdio" && !r.disabled);

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
          <h2 className="text-xl font-semibold">MCP Servers</h2>
          <p className="text-sm text-muted-foreground mt-1">
            Servers for <strong>{agentName || "this agent"}</strong> — inherited
            global servers plus this agent&apos;s overlays.
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

      {rows.length === 0 ? (
        <div className="rounded-lg border border-dashed p-12 text-center text-muted-foreground">
          <Server className="w-10 h-10 mx-auto mb-3 opacity-40" />
          <p>No MCP servers configured.</p>
          <p className="text-xs mt-1">
            Add a server for this agent, or add one under global MCP —
            those show up here as inherited.
          </p>
        </div>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {rows.map((row) => (
            <div
              key={row.name}
              className="rounded-lg border bg-card p-4 space-y-2"
            >
              <div className="flex items-start justify-between gap-2">
                <div className="flex items-center gap-2 min-w-0">
                  <Server className="w-4 h-4 shrink-0 text-muted-foreground" />
                  <span className="font-medium truncate">{row.name}</span>
                </div>
                <div className="flex shrink-0 flex-wrap items-center justify-end gap-1">
                  <Badge variant="secondary" className="text-xs">
                    {row.type}
                  </Badge>
                  {row.source === "inherited" ? (
                    <Badge variant="secondary" className="text-[10px]">
                      Inherited
                    </Badge>
                  ) : row.shadowed ? (
                    <Badge variant="outline" className="text-[10px]">
                      Override
                    </Badge>
                  ) : (
                    <Badge variant="outline" className="text-[10px]">
                      This agent
                    </Badge>
                  )}
                  {row.disabled && (
                    <Badge variant="outline" className="text-[10px]">
                      Off
                    </Badge>
                  )}
                </div>
              </div>
              <div className="text-xs text-muted-foreground truncate">
                {mcpEndpoint(row)}
              </div>
              {row.env && Object.keys(row.env).length > 0 && (
                <div className="text-xs text-muted-foreground">
                  env: {Object.keys(row.env).join(", ")}
                </div>
              )}
              {row.headers && Object.keys(row.headers).length > 0 && (
                <div className="text-xs text-muted-foreground">
                  headers: {Object.keys(row.headers).join(", ")}
                </div>
              )}
              <div className="flex gap-1 pt-1">
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7"
                  title={row.source === "inherited" ? "Override for this agent" : "Edit"}
                  onClick={() => {
                    setEditEntry(row);
                    setEditOpen(true);
                  }}
                >
                  <Pencil className="w-3.5 h-3.5" />
                </Button>
                {row.source === "inherited" ? (
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-7 w-7 text-destructive"
                    title="Disable for this agent"
                    onClick={() => handleDisableInherited(row)}
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </Button>
                ) : (
                  <>
                    {row.shadowed && (
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7"
                        title="Reset to inherited"
                        onClick={() => handleResetInherit(row.name)}
                      >
                        <Undo2 className="w-3.5 h-3.5" />
                      </Button>
                    )}
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-7 w-7 text-destructive"
                      onClick={() => setDeleteTarget(row.name)}
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </Button>
                  </>
                )}
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
        existingNames={rows.map((r) => r.name)}
        onSave={handleSaveEntry}
      />

      <AlertDialog
        open={!!deleteTarget}
        onOpenChange={(o) => !o && setDeleteTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Remove MCP overlay</AlertDialogTitle>
            <AlertDialogDescription>
              Remove the agent overlay for <strong>{deleteTarget}</strong>?
              {deleteTarget && globalServers[deleteTarget]
                ? " The inherited global server will come back."
                : " The server's tools will no longer be available."}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground"
              onClick={() => deleteTarget && handleDeleteLocal(deleteTarget)}
            >
              Remove
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function stripMeta(row: Row): MCPServerConfig {
  return {
    type: row.type,
    url: row.url,
    headers: row.headers,
    command: row.command,
    args: row.args,
    env: row.env,
    disabled: row.disabled,
  };
}
