export type FirstChatAgent = {
  id?: string;
  name?: string;
  model?: string;
  createdAt?: string | number | Date;
};

function createdAtMs(value: FirstChatAgent["createdAt"]): number {
  if (value == null || value === "") return Number.NaN;
  if (typeof value === "number") return Number.isFinite(value) ? value : Number.NaN;
  const ms = new Date(value).getTime();
  return Number.isFinite(ms) ? ms : Number.NaN;
}

/** Oldest created agent with an id. GET /api/agents is newest-first. */
export function firstAgent(
  agents: Array<FirstChatAgent | null | undefined> | null | undefined,
): FirstChatAgent | null {
  const list = (agents ?? []).filter((agent): agent is FirstChatAgent => Boolean(agent?.id));
  if (list.length === 0) return null;
  return [...list].sort((a, b) => {
    const aMs = createdAtMs(a.createdAt);
    const bMs = createdAtMs(b.createdAt);
    const aOk = Number.isFinite(aMs);
    const bOk = Number.isFinite(bMs);
    if (aOk && bOk) return aMs - bMs;
    if (aOk) return -1;
    if (bOk) return 1;
    return 0;
  })[0];
}

export function firstAgentChatPath(
  agents: Array<FirstChatAgent | null | undefined> | null | undefined,
): string | null {
  const id = firstAgent(agents)?.id;
  return id ? `/agents/${encodeURIComponent(id)}/chat/` : null;
}
