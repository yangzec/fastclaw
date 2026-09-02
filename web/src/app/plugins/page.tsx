"use client";

import { useCallback, useEffect, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Plug } from "lucide-react";
import {
  getConfig,
  getPlugins,
  updateConfig,
  updatePlugin,
  type PluginInfo,
} from "@/lib/api";

export default function GlobalPluginsPage() {
  const [plugins, setPlugins] = useState<PluginInfo[]>([]);
  const [systemEnabled, setSystemEnabled] = useState(false);
  const [loading, setLoading] = useState(true);
  const [masterSaving, setMasterSaving] = useState(false);
  const [saving, setSaving] = useState<Record<string, boolean>>({});

  const fetchAll = useCallback(async () => {
    setLoading(true);
    try {
      const [list, cfg] = await Promise.all([
        getPlugins().catch(() => [] as PluginInfo[]),
        getConfig().catch(() => null),
      ]);
      setPlugins(Array.isArray(list) ? list : []);
      setSystemEnabled(cfg?.plugins?.enabled === true);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchAll();
  }, [fetchAll]);

  const handleMaster = async (next: boolean) => {
    const prev = systemEnabled;
    setSystemEnabled(next);
    setMasterSaving(true);
    try {
      const res = await updateConfig({ plugins: { enabled: next } });
      if (res && typeof res === "object" && "ok" in res && res.ok === false) {
        throw new Error((res as { error?: string }).error || "Save failed");
      }
    } catch {
      setSystemEnabled(prev);
    } finally {
      setMasterSaving(false);
    }
  };

  const handleToggle = async (id: string, next: boolean) => {
    const prev = plugins.find((p) => p.id === id)?.enabled === true;
    setPlugins((list) =>
      list.map((p) => (p.id === id ? { ...p, enabled: next, status: next ? "running" : "stopped" } : p)),
    );
    setSaving((m) => ({ ...m, [id]: true }));
    try {
      await updatePlugin(id, { enabled: next });
    } catch {
      setPlugins((list) =>
        list.map((p) => (p.id === id ? { ...p, enabled: prev, status: prev ? "running" : "stopped" } : p)),
      );
    } finally {
      setSaving((m) => {
        const copy = { ...m };
        delete copy[id];
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
          Install-wide hook and tool plugins. Enabled plugins are inherited
          by every agent; an agent can still turn one off for itself.
        </p>
      </div>

      <div className="flex items-center justify-between rounded-lg border bg-card p-4">
        <div className="min-w-0 pr-4">
          <p className="text-sm font-medium">Plugin runtime</p>
          <p className="text-xs text-muted-foreground mt-0.5">
            Master switch. Off means discovered plugins stay unloaded.
            On, each plugin below can start and be inherited.
          </p>
        </div>
        <Switch
          checked={systemEnabled}
          onCheckedChange={handleMaster}
          disabled={masterSaving}
          aria-label="Enable plugin runtime"
        />
      </div>

      {plugins.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border bg-card/30 p-12">
          <div className="flex flex-col items-center justify-center">
            <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-primary/10 mb-4">
              <Plug className="h-7 w-7 text-primary" />
            </div>
            <p className="text-sm text-muted-foreground mb-1">
              No plugins installed
            </p>
            <p className="text-xs text-muted-foreground/60 max-w-sm text-center">
              Drop a plugin directory into{" "}
              <code className="text-[10px]">~/.fastclaw/plugins/</code>{" "}
              with a <code className="text-[10px]">plugin.json</code>, then
              restart the daemon.
            </p>
          </div>
        </div>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {plugins.map((p) => {
            const enabled = p.enabled === true;
            const busy = saving[p.id] === true;
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
                        {p.type && (
                          <Badge variant="outline" className="text-[10px]">
                            {p.type}
                          </Badge>
                        )}
                        {p.version && (
                          <Badge variant="outline" className="text-[10px]">
                            v{p.version}
                          </Badge>
                        )}
                        <Badge variant="secondary" className="text-[10px]">
                          {enabled ? "Enabled" : "Off"}
                        </Badge>
                      </div>
                    </div>
                  </div>
                  <Switch
                    checked={enabled}
                    onCheckedChange={(v) => handleToggle(p.id, v)}
                    disabled={busy || !systemEnabled}
                    aria-label={`Enable plugin ${p.id}`}
                  />
                </div>
                {p.description && (
                  <p className="text-xs text-muted-foreground line-clamp-3">
                    {p.description}
                  </p>
                )}
                <code className="text-[10px] text-muted-foreground/70 mt-3 block truncate">
                  {p.id}
                </code>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
