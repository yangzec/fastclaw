"use client";

import { useEffect, useState } from "react";
import { Check, ChevronDown } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { getChatContext, updateAgent, type ChatContextUsage } from "@/lib/api";
import { estimateDraftTokens, formatTokenCount } from "@/lib/format-tokens";

const OUTPUT_PRESETS = [
  { value: 2048, label: "2k" },
  { value: 4096, label: "4k" },
  { value: 8192, label: "8k" },
  { value: 16384, label: "16k" },
  { value: 32768, label: "32k" },
  { value: 65536, label: "64k" },
] as const;

function outputShort(n: number): string {
  if (n <= 0) return "—";
  const hit = OUTPUT_PRESETS.find((p) => p.value === n);
  return hit ? hit.label : formatTokenCount(n);
}

type Props = {
  agentId: string;
  sessionId?: string;
  draftText?: string;
  refreshKey?: string | number;
  canEdit?: boolean;
  onChanged?: () => void;
};

export function ComposerContextMeter({
  agentId,
  sessionId,
  draftText = "",
  refreshKey,
  canEdit = false,
  onChanged,
}: Props) {
  const [usage, setUsage] = useState<ChatContextUsage | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!agentId) {
      setUsage(null);
      return;
    }
    let aborted = false;
    getChatContext(agentId, sessionId)
      .then((next) => {
        if (!aborted) setUsage(next);
      })
      .catch(() => {
        if (!aborted) setUsage(null);
      });
    return () => {
      aborted = true;
    };
  }, [agentId, sessionId, refreshKey]);

  if (!agentId || !usage || usage.contextWindow <= 0) return null;

  const draft = estimateDraftTokens(draftText);
  const used = usage.tokens + draft;
  const pct = Math.min(100, (used / usage.contextWindow) * 100);
  const warn = usage.threshold > 0 && used >= usage.threshold * 0.85;
  const danger = used >= usage.contextWindow * 0.9 || (usage.threshold > 0 && used >= usage.threshold);
  const bar = danger ? "bg-destructive" : warn ? "bg-amber-500" : "bg-foreground/50";

  const title = [
    `Working set ${used.toLocaleString()} tokens`,
    `window ${usage.contextWindow.toLocaleString()}`,
    usage.maxTokens > 0 ? `max output ${usage.maxTokens.toLocaleString()}` : "",
    usage.threshold > 0 ? `compact around ${usage.threshold.toLocaleString()}` : "",
    usage.model ? `model ${usage.model}` : "",
  ]
    .filter(Boolean)
    .join(" · ");

  const pickOutput = async (value: number) => {
    if (!canEdit || saving || value === usage.maxTokens) return;
    const prev = usage.maxTokens;
    setUsage({ ...usage, maxTokens: value });
    setSaving(true);
    try {
      const res = await updateAgent(agentId, { maxTokens: value });
      if (res?.error) {
        setUsage({ ...usage, maxTokens: prev });
        return;
      }
      onChanged?.();
    } catch {
      setUsage({ ...usage, maxTokens: prev });
    } finally {
      setSaving(false);
    }
  };

  const output = (
    <span className="shrink-0 tabular-nums" title={usage.maxTokens > 0 ? `Max output ${usage.maxTokens.toLocaleString()} tokens` : "Max output"}>
      out {outputShort(usage.maxTokens)}
    </span>
  );

  return (
    <div className="flex min-w-0 items-center gap-2" title={title}>
      <div className="h-1 w-16 shrink-0 overflow-hidden rounded-full bg-muted">
        <div className={`h-full ${bar}`} style={{ width: `${Math.max(pct, pct > 0 ? 2 : 0)}%` }} />
      </div>
      <span className={`truncate tabular-nums ${danger ? "text-destructive" : warn ? "text-amber-600 dark:text-amber-400" : ""}`}>
        {formatTokenCount(used)} / {formatTokenCount(usage.contextWindow)}
      </span>
      <span className="text-muted-foreground/50">·</span>
      {canEdit ? (
        <DropdownMenu>
          <DropdownMenuTrigger
            disabled={saving}
            className="-mx-1 flex items-center gap-0.5 rounded px-1 hover:bg-muted hover:text-foreground disabled:opacity-50"
            aria-label="Max output tokens"
            title={`Max output ${usage.maxTokens.toLocaleString()} tokens`}
          >
            {output}
            <ChevronDown className="h-3 w-3 shrink-0 opacity-60" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="min-w-40" side="top">
            <DropdownMenuLabel>This agent&apos;s max output</DropdownMenuLabel>
            {OUTPUT_PRESETS.map((opt) => (
              <DropdownMenuItem
                key={opt.value}
                onClick={() => void pickOutput(opt.value)}
                className="justify-between gap-3"
              >
                <span className="flex items-center gap-1.5">
                  {opt.value === usage.maxTokens ? <Check className="h-3.5 w-3.5" /> : <span className="w-3.5" />}
                  {opt.label}
                </span>
                <span className="tabular-nums text-[11px] text-muted-foreground">{opt.value.toLocaleString()}</span>
              </DropdownMenuItem>
            ))}
            {!OUTPUT_PRESETS.some((p) => p.value === usage.maxTokens) && usage.maxTokens > 0 ? (
              <DropdownMenuItem disabled className="justify-between gap-3">
                <span className="flex items-center gap-1.5">
                  <Check className="h-3.5 w-3.5" />
                  {outputShort(usage.maxTokens)}
                </span>
                <span className="tabular-nums text-[11px] text-muted-foreground">{usage.maxTokens.toLocaleString()}</span>
              </DropdownMenuItem>
            ) : null}
          </DropdownMenuContent>
        </DropdownMenu>
      ) : (
        output
      )}
    </div>
  );
}
