"use client";

import { useEffect, useState } from "react";
import { getChatContext, type ChatContextUsage } from "@/lib/api";
import { estimateDraftTokens, formatTokenCount } from "@/lib/format-tokens";

type Props = {
  agentId: string;
  sessionId?: string;
  draftText?: string;
  refreshKey?: string | number;
};

export function ComposerContextMeter({
  agentId,
  sessionId,
  draftText = "",
  refreshKey,
}: Props) {
  const [usage, setUsage] = useState<ChatContextUsage | null>(null);

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
    usage.threshold > 0 ? `compact around ${usage.threshold.toLocaleString()}` : "",
    usage.model ? `model ${usage.model}` : "",
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <div className="flex min-w-0 items-center gap-2" title={title}>
      <div className="h-1 w-16 shrink-0 overflow-hidden rounded-full bg-muted">
        <div className={`h-full ${bar}`} style={{ width: `${Math.max(pct, pct > 0 ? 2 : 0)}%` }} />
      </div>
      <span className={`truncate tabular-nums ${danger ? "text-destructive" : warn ? "text-amber-600 dark:text-amber-400" : ""}`}>
        {formatTokenCount(used)} / {formatTokenCount(usage.contextWindow)}
      </span>
    </div>
  );
}
