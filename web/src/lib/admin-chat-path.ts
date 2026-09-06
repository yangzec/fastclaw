/** Same-tab admin chat deep link. Keeps `actAs` so the session stays read-only. */
export function adminChatHref(s: {
  agentId: string;
  id: string;
  userId: string;
}): string {
  return `/agents/${encodeURIComponent(s.agentId)}/chat/${encodeURIComponent(s.id)}/?actAs=${encodeURIComponent(s.userId)}`;
}
