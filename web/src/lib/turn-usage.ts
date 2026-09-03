const LAST_USAGE_PREFIX = "fastclaw.lastTurnUsage.";

export type TurnUsage = {
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens?: number;
  cacheCreationTokens?: number;
  requestCount?: number;
};

export function parseTurnUsage(data: unknown): TurnUsage | null {
  if (!data || typeof data !== "object") return null;
  const raw = data as Record<string, unknown>;
  const usage = (raw.usage && typeof raw.usage === "object" ? raw.usage : raw) as Record<string, unknown>;
  const input = Number(usage.inputTokens);
  const output = Number(usage.outputTokens);
  if (!Number.isFinite(input) || !Number.isFinite(output)) return null;
  if (input <= 0 && output <= 0) return null;
  return {
    inputTokens: input,
    outputTokens: output,
    cacheReadTokens: Number(usage.cacheReadTokens) || 0,
    cacheCreationTokens: Number(usage.cacheCreationTokens) || 0,
    requestCount: Number(usage.requestCount) || 0,
  };
}

export function formatTokenCount(n: number): string {
  if (!Number.isFinite(n)) return "—";
  const abs = Math.abs(n);
  if (abs < 1000) return String(Math.round(n));
  if (abs < 1_000_000) return (n / 1_000).toFixed(abs < 10_000 ? 1 : 1).replace(/\.0$/, "") + "k";
  return (n / 1_000_000).toFixed(2).replace(/\.00$/, "") + "M";
}

export function formatTurnUsageLine(u: TurnUsage): string {
  return `${formatTokenCount(u.inputTokens)} → ${formatTokenCount(u.outputTokens)}`;
}

function lastUsageKey(agentId: string, sessionId: string): string {
  return `${LAST_USAGE_PREFIX}${agentId}\t${sessionId}`;
}

export function loadLastTurnUsage(agentId: string, sessionId: string): TurnUsage | null {
  if (typeof window === "undefined" || !agentId || !sessionId) return null;
  try {
    return parseTurnUsage(JSON.parse(window.sessionStorage.getItem(lastUsageKey(agentId, sessionId)) || "null"));
  } catch {
    return null;
  }
}

export function saveLastTurnUsage(agentId: string, sessionId: string, usage: TurnUsage): void {
  if (typeof window === "undefined" || !agentId || !sessionId) return;
  window.sessionStorage.setItem(lastUsageKey(agentId, sessionId), JSON.stringify(usage));
}

export function formatTurnUsageHint(u: TurnUsage): string {
  const parts = [
    `入 ${u.inputTokens.toLocaleString()}`,
    `出 ${u.outputTokens.toLocaleString()}`,
  ];
  if (u.cacheReadTokens) parts.push(`缓存读 ${u.cacheReadTokens.toLocaleString()}`);
  if (u.cacheCreationTokens) parts.push(`缓存写 ${u.cacheCreationTokens.toLocaleString()}`);
  if (u.requestCount && u.requestCount > 1) parts.push(`${u.requestCount} 次调用`);
  return parts.join(" · ");
}
