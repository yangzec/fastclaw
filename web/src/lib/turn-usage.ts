import { formatTokenCount } from "./format-tokens.ts";

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
    `in ${u.inputTokens.toLocaleString()}`,
    `out ${u.outputTokens.toLocaleString()}`,
  ];
  if (u.cacheReadTokens) parts.push(`cache read ${u.cacheReadTokens.toLocaleString()}`);
  if (u.cacheCreationTokens) parts.push(`cache write ${u.cacheCreationTokens.toLocaleString()}`);
  if (u.requestCount && u.requestCount > 1) parts.push(`${u.requestCount} calls`);
  return parts.join(" · ");
}
