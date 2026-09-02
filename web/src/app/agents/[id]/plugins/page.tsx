"use client";

import { useCallback, useEffect, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Plug, Undo2 } from "lucide-react";
import {
  getAgent,
  listHookPlugins,
  updateAgent,
  type HookPlugin,
} from "@/lib/api";
import { useAgentIdFromURL } from "@/hooks/use-agent-id";
import { useAgentName } from "@/hooks/use-agent-name";

// Per-agent plugin enable tab. System-wide enable (global Plugins page)
// is inherited when this agent has no overlay key. A toggle writes an
// override; Reset drops it so the inherited value comes back.
export default function AgentPluginsPage() {
  const agentId = useAgentIdFromURL();
  const agentName = useAgentName(agentId);
  const [hookPlugins, setHookPlugins] = useState<HookPlugin[]>([]);
  const [pluginEnabled, setPluginEnabled] = useState<Record<string, boolean>>({});
  const [pluginSaving, setPluginSaving] = useState<Record<string, boolean>>({});
  const [loading, setLoading] = useState(true);

  const fetchAll = useCallback(async () => {
    if (!agentId) return;
    setLoading(true);
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
      setLoading(false);
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
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Plugins</h2>
        <p className="text-sm text-muted-foreground mt-1">
          Hook plugins for <strong>{agentName}</strong> — inherited from
          the global Plugins page unless you override them here.
        </p>
      </div>

      {hookPlugins.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border bg-card/30 p-12">
          <div className="flex flex-col items-center justify-center">
            <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-primary/10 mb-4">
              <Plug className="h-7 w-7 text-primary" />
            </div>
            <p className="text-sm text-muted-foreground mb-1">
              No hook plugins installed
            </p>
            <p className="text-xs text-muted-foreground/60 max-w-sm text-center">
              Drop a plugin directory into{" "}
              <code className="text-[10px]">~/.fastclaw/plugins/</code>{" "}
              with <code className="text-[10px]">type: &quot;hook&quot;</code> in
              its <code className="text-[10px]">plugin.json</code>, then
              restart the daemon.
            </p>
          </div>
        </div>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {hookPlugins.map((p) => {
            const hasOverlay = Object.prototype.hasOwnProperty.call(pluginEnabled, p.id);
            const systemOn = p.enabled === true;
            const enabled = hasOverlay ? pluginEnabled[p.id] === true : systemOn;
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
                        ) : (
                          <Badge variant="secondary" className="text-[10px]">
                            Inherited
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
    </div>
  );
}
