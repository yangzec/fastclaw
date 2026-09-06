"use client";

import { useCallback, useEffect, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Download, Plug, Undo2, Upload } from "lucide-react";
import { PluginsEmptyState } from "@/components/plugins-empty-state";
import { InstallPluginDialog, UploadPluginDialog } from "@/components/plugins-install-dialogs";
import {
  getAgent,
  inheritsToAgents,
  listHookPlugins,
  updateAgent,
  type HookPlugin,
} from "@/lib/api";
import { useAgentIdFromURL } from "@/hooks/use-agent-id";
import { useAgentName } from "@/hooks/use-agent-name";

// Per-agent plugin enable tab. A catalog item is inherited only when
// it is enabled AND inherit=all. Otherwise it stays Available until
// this agent opts in. Reset drops the overlay.
export default function AgentPluginsPage() {
  const agentId = useAgentIdFromURL();
  const agentName = useAgentName(agentId);
  const [hookPlugins, setHookPlugins] = useState<HookPlugin[]>([]);
  const [pluginEnabled, setPluginEnabled] = useState<Record<string, boolean>>({});
  const [pluginSaving, setPluginSaving] = useState<Record<string, boolean>>({});
  const [loading, setLoading] = useState(true);
  const [installOpen, setInstallOpen] = useState(false);
  const [uploadOpen, setUploadOpen] = useState(false);
  const [restartHint, setRestartHint] = useState(false);

  const fetchAll = useCallback(async (opts?: { silent?: boolean }) => {
    if (!agentId) return;
    if (!opts?.silent) setLoading(true);
    try {
      const [agentRec, hooks] = await Promise.all([
        getAgent(agentId).catch(() => null),
        listHookPlugins(),
      ]);
      setPluginEnabled(
        agentRec?.plugins && typeof agentRec.plugins === "object"
          ? (agentRec.plugins as Record<string, boolean>)
          : {}
      );
      setHookPlugins(hooks);
    } finally {
      if (!opts?.silent) setLoading(false);
    }
  }, [agentId]);

  useEffect(() => {
    fetchAll();
  }, [fetchAll]);

  const handleToggle = async (pluginID: string, next: boolean) => {
    const prevOverlay = pluginEnabled[pluginID];
    const hadOverlay = Object.prototype.hasOwnProperty.call(pluginEnabled, pluginID);
    setPluginEnabled((m) => ({ ...m, [pluginID]: next }));
    setPluginSaving((m) => ({ ...m, [pluginID]: true }));
    try {
      await updateAgent(agentId, { plugins: { [pluginID]: next } });
    } catch {
      setPluginEnabled((m) => {
        const copy = { ...m };
        if (hadOverlay) copy[pluginID] = prevOverlay;
        else delete copy[pluginID];
        return copy;
      });
    } finally {
      setPluginSaving((m) => {
        const copy = { ...m };
        delete copy[pluginID];
        return copy;
      });
    }
  };

  const handleReset = async (pluginID: string) => {
    const prev = pluginEnabled[pluginID];
    setPluginEnabled((m) => {
      const copy = { ...m };
      delete copy[pluginID];
      return copy;
    });
    setPluginSaving((m) => ({ ...m, [pluginID]: true }));
    try {
      // Clearing a single key: rewrite the overlay without it.
      const next: Record<string, boolean> = {};
      for (const [k, v] of Object.entries(pluginEnabled)) {
        if (k !== pluginID) next[k] = v;
      }
      if (Object.keys(next).length === 0) {
        await updateAgent(agentId, { pluginsReset: true });
      } else {
        // Whole-map replace isn't available — patch only adds keys.
        // Reset then re-apply remaining overrides.
        await updateAgent(agentId, { pluginsReset: true });
        await updateAgent(agentId, { plugins: next });
      }
    } catch {
      setPluginEnabled((m) => ({ ...m, [pluginID]: prev }));
    } finally {
      setPluginSaving((m) => {
        const copy = { ...m };
        delete copy[pluginID];
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

  return (
    <div className="p-6 space-y-6 max-w-5xl mx-auto">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Plugins</h2>
          <p className="text-sm text-muted-foreground mt-1">
            Hook plugins for <strong>{agentName}</strong>. Inherited only
            when the catalog item is shared with agents; otherwise opt in
            here.
          </p>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <Button variant="outline" onClick={() => setUploadOpen(true)}>
            <Upload className="h-4 w-4 mr-2" />
            Upload
          </Button>
          <Button onClick={() => setInstallOpen(true)}>
            <Download className="h-4 w-4 mr-2" />
            Install
          </Button>
        </div>
      </div>
      {restartHint && (
        <p className="text-xs text-muted-foreground rounded-md border bg-muted/30 px-3 py-2">
          Plugin files are on disk. Restart FastClaw if a newly installed plugin does not show as running yet.
        </p>
      )}

      {hookPlugins.length === 0 ? (
        <PluginsEmptyState title="No plugins yet" onInstall={() => setInstallOpen(true)} onUpload={() => setUploadOpen(true)} />
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {hookPlugins.map((p) => {
            const hasOverlay = Object.prototype.hasOwnProperty.call(pluginEnabled, p.id);
            const inherited = p.enabled === true && inheritsToAgents(p.inherit);
            const enabled = hasOverlay ? pluginEnabled[p.id] === true : inherited;
            const saving = pluginSaving[p.id] === true;
            return (
              <div
                key={p.id}
                className="group rounded-lg border border-border bg-card p-5 transition-colors hover:bg-muted/50"
              >
                <div className="flex items-start justify-between mb-3 gap-3">
                  <div className="flex items-center gap-2.5 min-w-0">
                    <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary/10 shrink-0">
                      <Plug className="h-4 w-4 text-primary" />
                    </div>
                    <div className="min-w-0">
                      <p className="text-sm font-medium truncate">
                        {p.name || p.id}
                      </p>
                      <div className="mt-1 flex flex-wrap items-center gap-1">
                        {p.version && (
                          <Badge variant="outline" className="text-[10px]">
                            v{p.version}
                          </Badge>
                        )}
                        {hasOverlay ? (
                          <Badge variant="outline" className="text-[10px]">
                            Override
                          </Badge>
                        ) : inherited ? (
                          <Badge variant="secondary" className="text-[10px]">
                            Inherited
                          </Badge>
                        ) : (
                          <Badge variant="outline" className="text-[10px]">
                            Available
                          </Badge>
                        )}
                      </div>
                    </div>
                  </div>
                  <Switch
                    checked={enabled}
                    onCheckedChange={(v) => handleToggle(p.id, v)}
                    disabled={saving}
                    aria-label={`Enable plugin ${p.id}`}
                  />
                </div>
                {p.description && (
                  <p className="text-xs text-muted-foreground line-clamp-3">
                    {p.description}
                  </p>
                )}
                <div className="mt-3 flex items-center justify-between gap-2">
                  <code className="text-[10px] text-muted-foreground/70 truncate">
                    {p.id}
                  </code>
                  {hasOverlay && (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 px-2 text-xs"
                      disabled={saving}
                      onClick={() => handleReset(p.id)}
                    >
                      <Undo2 className="h-3 w-3 mr-1" />
                      Inherit
                    </Button>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
      <InstallPluginDialog
        open={installOpen}
        onOpenChange={setInstallOpen}
        onInstalled={(info) => {
          if (info?.needsRestart) setRestartHint(true);
          fetchAll({ silent: true });
        }}
      />
      <UploadPluginDialog
        open={uploadOpen}
        onOpenChange={setUploadOpen}
        onInstalled={(info) => {
          if (info?.needsRestart) setRestartHint(true);
          fetchAll({ silent: true });
        }}
      />
    </div>
  );
}
