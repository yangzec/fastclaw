export function firstAgentChatPath(
  agents: Array<{ id?: string } | null | undefined> | null | undefined,
): string | null {
  const id = agents?.find((agent) => agent?.id)?.id;
  return id ? `/agents/${encodeURIComponent(id)}/chat/` : null;
}
