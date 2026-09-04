# FastClaw API Integration

Use this skill when integrating an upstream application with FastClaw as an
agent runtime.

The canonical reference is `docs/upstream-api.md` in the FastClaw repository.
Follow that document over guesses from existing dashboard code.

## Default model

One website = one FastClaw account + one server-side API key.

Agents are **product capabilities** (support, writing), not registered users.
End users are isolated with `X-Fastclaw-Session-Key`:

```text
<app>:<your-user-id>:<conversation-id>
```

Reuse the key to continue a chat. New conversation-id for a new thread.
Never reuse a key across users.

Do **not** send `user` or `X-Fastclaw-End-User` on this path. Put who the
speaker is in `params` this turn. Keep **Auto-remember chatter** off on
public template agents (that is the default). FastClaw USER.md / MEMORY.md
are not per website user on `/v1/chat/completions`.

Do not create one FastClaw agent per registered user — the website
UserSpace loads every owned agent into memory.

## Integration Boundary

Use `/v1/*` for upstream applications:

- `POST /v1/chat/completions` for end-user chat.
- `GET /v1/agents` to list agents accessible to the API key.
- `GET /v1/usage` for the owner-account token ledger (sum `daily[]` by `agentId`).
- `PUT /v1/quota`, `GET /v1/quota`, `DELETE /v1/quota` for a site-wide cap
  on that same owner account.
- After a chat turn, list/download workspace files with the same API key:
  `GET /api/agents/{agent_id}/files?sessionId=<session_key or session_id>`.

`POST /v1/users` is optional and not part of billing.

Use `/api/*` or the `fastclaw` CLI only for operator/admin workflows such as
creating agents, configuring providers, installing skills, managing channels,
and runtime/project administration.

## Required Inputs

Before writing integration code, identify:

- FastClaw base URL.
- API key, stored server-side only.
- Agent ID (one per product, not per end user).
- Upstream stable user ID field (session-key middle segment + optional `params`).
- Upstream conversation/session ID field.
- Whether owner-level usage/quota must be wired.
- Whether attachments/images must be supported.

Do not expose FastClaw API keys to browsers or mobile clients. Route calls
through the upstream backend.

## Chat Contract

Call:

```http
POST /v1/chat/completions
Authorization: Bearer <FASTCLAW_API_KEY>
Content-Type: application/json
X-Fastclaw-Session-Key: <deterministic-session-key>
```

Body:

```json
{
  "agent_id": "agt_...",
  "stream": true,
  "messages": [
    { "role": "user", "content": "..." }
  ],
  "params": {
    "user_id": "upstream-user-id",
    "display_name": "Alice"
  }
}
```

Rules:

- `agent_id` selects the FastClaw agent. Body wins over
  `X-Fastclaw-Agent-ID`. Always send it when the account has more than
  one agent.
- Do not send `user` or `X-Fastclaw-End-User`.
- `messages` is **this turn only**. FastClaw already has the session
  history. Do not replay the full transcript.
- `X-Fastclaw-Session-Key` is YOUR conversation id. Use
  `<app>:<user-id>:<conversation-id>`. FastClaw stores a separate native
  `session_id` (`s-...`) for that row.
- Every completion echoes both ids:
  - headers `X-Fastclaw-Session-Key` (what you sent) and
    `X-Fastclaw-Session-Id` (native `s-...`)
  - non-stream JSON also has `session_key` and `session_id`
- Use either id as `?sessionId=` when listing or downloading files.
- `params` is per-turn structured context. It is shown to the agent but not
  persisted.
- Use `images` or `imageUrls` for image URLs/data URLs intended for vision
  models.
- Use `attachments` for general files, with optional `name`.
- Completion `usage` is currently zeros. Bill from `GET /v1/usage`.

## Session Files

Agent-produced files (markdown, charts, uploads) live in the session
workspace. After a completion:

```http
GET /api/agents/<agent_id>/files?sessionId=<session_key or session_id>
Authorization: Bearer <FASTCLAW_API_KEY>
```

Download one file (relative name from the list, or `/workspace/<name>`):

```http
GET /api/agents/<agent_id>/files/<name>?sessionId=<session_key or session_id>
Authorization: Bearer <FASTCLAW_API_KEY>
```

`sessionId` accepts the header you sent on chat (`session_key`) or the
native `session_id` from the response. Always pass it. Omitting it lets
the owner key list every file on the agent.

Do not guess `sessions/<id>/...` paths. Do not put `?token=` file URLs
on a public page.

## Usage And Quota

Query the owner bucket:

```http
GET /v1/usage?days=30
Authorization: Bearer <FASTCLAW_API_KEY>
```

`user_id` is ignored. Sum `daily[].agentId` for per-agent totals. Per
registered user: add up your own session keys.

Site-wide cap (owner account):

```http
PUT /v1/quota
Authorization: Bearer <FASTCLAW_API_KEY>
Content-Type: application/json

{
  "monthly_token_limit": 5000000,
  "monthly_request_limit": 10000,
  "reset_day": 1
}
```

`user_id` is optional and must be the API-key owner when set.

## Implementation Checklist

1. Add server-side FastClaw client configuration:
   - base URL
   - API key
   - agent ID
2. Derive `X-Fastclaw-Session-Key` as `<app>:<user-id>:<conversation-id>`.
3. Send only this turn's user message. Put identity in `params`.
4. Implement streaming SSE parsing for `/v1/chat/completions`.
   Keep the socket open through tool execution: the first event is the
   role chunk, then `: heartbeat` comments every 10s until tokens
   arrive. Ignore lines that start with `:`. Do not time out on silence.
5. After each completion, store `session_id` if you need dashboard-style
   ids; file APIs accept either token. Always pass `sessionId`.
6. Add attachment support only if the product UI needs it.
7. Add `/v1/usage` and `/v1/quota` only if owner-level billing/limits
   are required. Do not look up usage by app_user.
8. Leave Auto-remember chatter off on public template agents.
9. Handle OpenAI-style error objects:
   - `400` invalid request
   - `401` auth failure
   - `403` quota user_id is not the owner
   - `404` inaccessible agent
   - `429` rate/quota limit
   - `503` subsystem disabled

## Do Not

- Do not call dashboard `/api/chat/stream` for upstream app chat unless you are
  embedding FastClaw's own dashboard semantics.
- Do not put FastClaw API keys in frontend code.
- Do not send `user` / `X-Fastclaw-End-User` on the default website path.
- Do not replay the full chat transcript in `messages`.
- Do not reuse one session key across users or unrelated conversations.
- Do not create one FastClaw agent per registered user.
- Do not configure agents/providers/skills through `/v1`; use dashboard,
  `/api/*`, or CLI for admin workflows.
