export function decodePathSegment(segment: string): string {
  try {
    return decodeURIComponent(segment);
  } catch {
    // Keep the raw segment for malformed URLs instead of crashing the chat UI.
    return segment;
  }
}

// Parse the per-route ids out of the pathname. ChatScreen is mounted once at
// the agent layout level and stays alive across these routes:
//
//   /agents/<aid>/                         — fresh loose chat
//   /agents/<aid>/chat/                    — fresh loose chat
//   /agents/<aid>/chat/<session>           — open existing chat by id
//   /agents/<aid>/project/<pid>            — fresh chat in a project
//
// The dynamic path segment may be percent-encoded (e.g. ':' -> '%3A') because
// sidebar links use encodeURIComponent(s.id). Return the decoded id so API
// calls do not double-encode it and miss the persisted session row.
export function parseAgentRoute(pathname: string): {
  sessionId: string;
  projectId: string;
} {
  const sessMatch = pathname.match(/^\/agents\/[^/]+\/chat\/([^/]+)/);
  if (sessMatch) {
    const sid = decodePathSegment(sessMatch[1]);
    // "_" is the build-time placeholder Next emits under output:'export'
    // for the dynamic [session] segment. Treat it as "no session".
    return { sessionId: sid === "_" ? "" : sid, projectId: "" };
  }
  const projMatch = pathname.match(/^\/agents\/[^/]+\/project\/([^/]+)/);
  if (projMatch) {
    const pid = decodePathSegment(projMatch[1]);
    return { sessionId: "", projectId: pid === "_" ? "" : pid };
  }
  return { sessionId: "", projectId: "" };
}
