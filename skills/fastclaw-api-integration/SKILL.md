# FastClaw API Integration

Use this skill when integrating an upstream application with FastClaw as an
agent runtime.

Canonical HTTP contract: `docs/upstream-api.md`.
Basic Memory (optional, app-side): `docs/upstream-basic-memory.md`.
Follow those over guesses from dashboard code.

## Default website path

One website = one FastClaw account + one server-side API key.
Agents are product capabilities, not one agent per registered user.

```http
POST /v1/chat/completions
Authorization: Bearer <FASTCLAW_API_KEY>
Content-Type: application/json
X-Fastclaw-Session-Key: <app>:<your-user-id>:<conversation-id>
X-Fastclaw-Chatter: app:<your-user-id>
```

```json
{
  "agent_id": "agt_...",
  "stream": true,
  "messages": [
    { "role": "user", "content": "..." }
  ],
  "params": {
    "user_id": "app:<your-user-id>",
    "display_name": "Ada",
    "known_facts": []
  }
}
```

- `messages` = **this turn only**. FastClaw keeps session history. Replaying
  an OpenAI transcript is ignored except the last user line.
- Chatter (`X-Fastclaw-Chatter` and/or `params.user_id`) keys USER.md /
  MEMORY.md. Both set and different → 400. Neither → `api-user`.
- Do **not** send `user` or `X-Fastclaw-End-User` (those switch UserSpace
  and still do not set chatter).
- Keys stay on the server. Browsers must not call FastClaw or choose chatter.

## Other `/v1` calls

- `GET /v1/agents` — agents this API key can call.
- `GET /v1/usage` — owner-account token ledger (`user_id` ignored).
  Sum `daily[].agentId` for per-agent. Per registered user: your DB.
- `/v1/quota` — site-wide cap on that same owner (`user_id` optional,
  must match the owner when set).
- Files: `GET /api/agents/{id}/files?sessionId=<session key or session_id>`.
  Always pass `sessionId`.
- `POST /v1/users` — optional app_user mint. Not the default website path.

Dashboard `/api/chat/*` is not the upstream chat API.

## Session and memory

- Reuse the session key to continue a thread. New conversation-id = new thread.
- FastClaw compaction handles long threads. You do not need a second compressor.
- Cross-session product facts: your backend or Basic Memory HTTP, then
  `params.known_facts`. FastClaw does not call BM. Do not mount BM MCP on
  a template agent. Recipe: `docs/upstream-basic-memory.md`.
- Template agents: keep Auto-remember off until chatter ids are unique.
- Operator console + personal IM: FastClaw **Shared identity** toggle on
  that agent. No BM required for that pair.

## Streaming

First event is the role chunk. `: heartbeat` every 10s while tools run.
Ignore lines starting with `:`. Do not treat silence as a hang.
Completion `usage` is zeros — bill from `GET /v1/usage`.

## Checklist

1. Server-side base URL, API key, agent id.
2. Deterministic session key and chatter (`app:<user-id>`).
3. SSE client that survives heartbeats.
4. File list with `?sessionId=`.
5. Owner usage/quota only if you need a site cap. Do not look up
   usage by app_user.
6. Errors: `400` (including chatter mismatch), `401`, `403` (quota
   `user_id` is not the owner), `404`, `429`, `503`.

## Do not

- Put FastClaw or BM keys in the frontend.
- Send End-User / body `user` on the default path.
- Reuse one session key across users.
- Replay full chat history in `messages`.
- Create one FastClaw agent per registered user.
- Configure providers/skills through `/v1`.
