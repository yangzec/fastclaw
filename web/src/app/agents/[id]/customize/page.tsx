"use client";

import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Save, Check, Loader2, RotateCcw } from "lucide-react";
import { apiFetch } from "@/lib/api";

import { useAgentIdFromURL } from "@/hooks/use-agent-id";
import { useAgentName } from "@/hooks/use-agent-name";

const CUSTOMIZE_FILES = [
  { name: "SOUL.md", label: "Soul", hint: "Who this agent is, what it cares about, and how it should talk." },
  { name: "IDENTITY.md", label: "Identity", hint: "Name, role, and the few facts that should stay true every time." },
  { name: "USER.md", label: "User", hint: "What this agent should know about you." },
  { name: "TOOLS.md", label: "Tools", hint: "How this agent should use tools." },
  { name: "BOOTSTRAP.md", label: "Bootstrap", hint: "What to do when a new conversation starts." },
  { name: "HEARTBEAT.md", label: "Heartbeat", hint: "What to check on a regular pulse." },
  { name: "MEMORY.md", label: "Memory", hint: "What this agent should remember across chats." },
  { name: "AGENTS.md", label: "Agents", hint: "How this agent should work with other agents." },
];

// FileState mirrors the backend's GET response: `content` is what's
// effectively loaded, `source` says where it came from, and `baseContent`
// (only set when source==="db" with a different owner row to revert to)
// is what the user would fall back to on Revert.
//
//   - "db":      the caller's own per-user override row (USER.md /
//                MEMORY.md only) — distinct from the owner's content.
//   - "owner":   the agent owner's row, the canonical "shared template"
//                — what identity files (SOUL/IDENTITY/BOOTSTRAP/...)
//                always render as, and what per-user files fall back to.
//   - "fs":      legacy filesystem default. Kept for back-compat.
//   - "default": neither caller nor owner row exists; tab is empty.
type FileSource = "db" | "owner" | "fs" | "default";
type FileState = {
  content: string;
  savedContent: string;
  source: FileSource;
  baseContent?: string;
};

function isDirty(f?: FileState): boolean {
  return !!f && f.content !== f.savedContent;
}

function fileStateFromResponse(data: {
  content?: string;
  source?: string;
  baseContent?: string;
}): FileState {
  const content = data.content || "";
  return {
    content,
    savedContent: content,
    source: (data.source || "default") as FileSource,
    baseContent: data.baseContent,
  };
}

export default function AgentCustomizePage({
  onDirtyChange,
}: {
  onDirtyChange?: (dirty: boolean) => void;
} = {}) {
  const agentId = useAgentIdFromURL();
  const agentName = useAgentName(agentId);
  const [activeTab, setActiveTab] = useState("SOUL.md");
  const [files, setFiles] = useState<Record<string, FileState>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadAll = async () => {
    const entries = await Promise.all(
      CUSTOMIZE_FILES.map(async (f) => {
        try {
          const res = await apiFetch(`/api/agents/${agentId}/system-files/${f.name}`);
          if (res.ok) {
            const data = await res.json();
            return [f.name, fileStateFromResponse(data)] as [string, FileState];
          }
        } catch {}
        return [f.name, fileStateFromResponse({})] as [string, FileState];
      })
    );
    setFiles(Object.fromEntries(entries));
  };

  useEffect(() => {
    setLoading(true);
    loadAll().then(() => setLoading(false));
  }, [agentId]);

  const active = files[activeTab];
  const filesRef = useRef(files);
  filesRef.current = files;
  const dirtyNames = CUSTOMIZE_FILES.map((f) => f.name).filter((n) => isDirty(files[n]));

  useEffect(() => {
    onDirtyChange?.(dirtyNames.length > 0);
  }, [dirtyNames.length, onDirtyChange]);
  useEffect(() => () => onDirtyChange?.(false), [onDirtyChange]);

  const handleSave = async () => {
    const snapshot = filesRef.current;
    const dirty = CUSTOMIZE_FILES.map((f) => f.name).filter((n) => isDirty(snapshot[n]));
    const toWrite = dirty.length > 0 ? dirty : [activeTab];
    const payloads: Record<string, string> = {};
    for (const name of toWrite) payloads[name] = snapshot[name]?.content ?? "";
    setSaving(true);
    setError(null);
    const succeeded: string[] = [];
    const failures: string[] = [];
    await Promise.all(
      toWrite.map(async (name) => {
        const label = CUSTOMIZE_FILES.find((f) => f.name === name)?.label || name;
        try {
          const res = await apiFetch(`/api/agents/${agentId}/system-files/${name}`, {
            method: "PUT",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ content: payloads[name] }),
          });
          const data = await res.json().catch(() => ({}));
          if (!res.ok || data?.ok === false) {
            failures.push(`${label}: ${data?.error || `save failed (${res.status})`}`);
            return;
          }
          succeeded.push(name);
        } catch (e) {
          failures.push(`${label}: ${e instanceof Error ? e.message : "save failed"}`);
        }
      }),
    );
    // Mark written tabs clean locally. A follow-up GET can 404/fail
    // after invalidateUser and would blank Bootstrap on screen even
    // though the PUT landed — that looked like "some files vanished".
    if (succeeded.length > 0) {
      setFiles((prev) => {
        const next = { ...prev };
        for (const name of succeeded) {
          if (!next[name]) continue;
          next[name] = { ...next[name], savedContent: payloads[name] };
        }
        return next;
      });
    }
    if (failures.length > 0) {
      setError(failures.join(" · "));
    } else {
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    }
    setSaving(false);
  };

  // Revert deletes the DB override so the runtime falls back to the FS base
  // shipped with the agent definition. Only meaningful when source==="db"
  // AND a baseContent exists (otherwise the tab just becomes empty).
  const handleRevert = async () => {
    if (!active || active.source !== "db") return;
    if (!confirm(`Revert ${activeTab} to the repo base? Your edits will be discarded.`)) return;
    setSaving(true);
    try {
      await apiFetch(`/api/agents/${agentId}/system-files/${activeTab}`, {
        method: "DELETE",
      });
      await loadAll();
    } catch {}
    setSaving(false);
  };

  if (loading) {
    return (
      <div className="p-6 space-y-4">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-96 w-full" />
      </div>
    );
  }

  const sourceBadge = (source: FileSource | undefined) => {
    if (source === "db") {
      return (
        <span className="text-xs px-2 py-0.5 rounded-md border border-amber-500/30 text-amber-600">
          Edited
        </span>
      );
    }
    if (source === "fs") {
      return (
        <span className="text-xs px-2 py-0.5 rounded-md border border-emerald-500/30 text-emerald-600">
          From repo
        </span>
      );
    }
    return null;
  };

  return (
    <div className="p-6 max-w-4xl mx-auto">
      <div className="flex items-center justify-between mb-4">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Customize</h2>
          <p className="text-sm text-muted-foreground mt-1">
            Personality, memory, and behavior files for <strong>{agentName}</strong>.
            Save writes every tab you edited, not only the one on screen.
          </p>
        </div>
        <div className="flex gap-2">
          {active?.source === "db" && (
            <Button
              onClick={handleRevert}
              disabled={saving}
              variant="outline"
              title={
                active.baseContent
                  ? "Discard your edits and revert to the file shipped in the repo"
                  : "Discard your edits (no repo base for this file — tab will become empty)"
              }
            >
              <RotateCcw className="h-4 w-4 mr-2" /> Revert
            </Button>
          )}
          <Button
            onClick={handleSave}
            disabled={saving}
            variant={saved ? "outline" : "default"}
            className={saved ? "border-emerald-500/30 text-emerald-600" : ""}
          >
            {saved ? (
              <><Check className="h-4 w-4 mr-2" /> Saved</>
            ) : saving ? (
              <><Loader2 className="h-4 w-4 mr-2 animate-spin" /> Saving...</>
            ) : dirtyNames.length > 1 ? (
              <><Save className="h-4 w-4 mr-2" /> Save {dirtyNames.length} files</>
            ) : (
              <><Save className="h-4 w-4 mr-2" /> Save</>
            )}
          </Button>
        </div>
      </div>

      {/* Tabs */}
      <div className="flex gap-1 border-b border-border mb-4 overflow-x-auto">
        {CUSTOMIZE_FILES.map((f) => (
          <button
            key={f.name}
            onClick={() => setActiveTab(f.name)}
            className={`px-3 py-2 text-sm font-medium whitespace-nowrap border-b-2 transition-colors flex items-center gap-2 ${
              activeTab === f.name
                ? "border-primary text-primary"
                : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
          >
            {f.label}
            {isDirty(files[f.name]) ? (
              <span className="size-1.5 rounded-full bg-orange-500" title="Unsaved" />
            ) : files[f.name]?.source === "db" ? (
              <span className="size-1.5 rounded-full bg-amber-500" />
            ) : null}
          </button>
        ))}
      </div>

      {/* Active-tab status line — only shows when there's something
          actionable to say (override active / loaded from repo). The
          "default" case (empty + no repo base) is silent. */}
      {error && (
        <p className="mb-2 text-sm text-destructive">{error}</p>
      )}

      {(active?.source === "db" || active?.source === "fs") && (
        <div className="flex items-center gap-2 mb-2 text-xs text-muted-foreground">
          {sourceBadge(active?.source)}
          {active?.source === "db" && active.baseContent && (
            <span>Override active — repo base is {active.baseContent.length} chars.</span>
          )}
          {active?.source === "fs" && (
            <span>Loaded from <code>{`<agent home>/${activeTab}`}</code>. Editing creates a per-agent override.</span>
          )}
        </div>
      )}

      {/* Editor */}
      <textarea
        value={active?.content || ""}
        readOnly={saving}
        onChange={(e) =>
          setFiles((prev) => ({
            ...prev,
            [activeTab]: {
              ...(prev[activeTab] || { source: "default", savedContent: "" }),
              content: e.target.value,
            },
          }))
        }
        spellCheck={false}
        className="w-full rounded-lg border border-border bg-card px-4 py-3 font-mono text-sm leading-relaxed outline-none focus:ring-1 focus:ring-primary/30 resize-none"
        // Bounded so the editor stays a reasonable size inside the
        // Settings dialog (85vh modal) — the previous
        // `calc(100vh - 240px)` made the textarea swallow nearly the
        // whole dialog. The clamp keeps the standalone /customize/
        // page usable too: still grows on tall screens, but stops
        // short of "fills the viewport".
        style={{ height: "min(55vh, 480px)", minHeight: 280 }}
        placeholder={CUSTOMIZE_FILES.find((f) => f.name === activeTab)?.hint || "Write what this file should say."}
      />
    </div>
  );
}
