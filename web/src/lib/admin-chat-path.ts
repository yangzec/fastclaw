/**
 * Same-tab admin chat deep link. Keeps `actAs` so the session stays read-only.
 *
 * Do not open this href with target=_blank / window.open: on the static SPA
 * export that path can leave a new tab stuck on about:blank even though the
 * same URL loads in the current tab (confirmed on the yangzec gateway).
 */
export function adminChatHref(s: {
  agentId: string;
  id: string;
  userId: string;
}): string {
  return `/agents/${encodeURIComponent(s.agentId)}/chat/${encodeURIComponent(s.id)}/?actAs=${encodeURIComponent(s.userId)}`;
}
