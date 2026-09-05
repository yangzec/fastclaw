# FastClaw Upstream App Integration API

This is the integration contract for upstream applications that want to use
FastClaw as their Agent runtime.

Use this document when building a SaaS, bot, marketplace, or workflow product
that owns its own users but delegates agent execution, tools, session history,
usage metering, quota checks, and optional channels to FastClaw.

Website-shaped products (one FastClaw account, template agents, session keys,
chatter ids) should follow the **default path** below. Optional Basic Memory
HTTP: `docs/upstream-basic-memory.md` (FastClaw does not call BM). Decision
record: `docs/upstream-website-model.md`.

## Which Interface To Use

| Need | Interface | Notes |
|---|---|---|
| End-user chat with an agent | `POST /v1/chat/completions` | OpenAI-shaped. Only the last `user` message is used. |
| Which conversation | `X-Fastclaw-Session-Key` | `<app>:<your-user-id>:<conversation-id>` |
| Which person for USER.md / MEMORY.md | `X-Fastclaw-Chatter` and/or `params.user_id` | Not body `user`. See [Chatter](#chatter-memory-identity). |
| Who the model should treat as the speaker | `params` this turn | Not persisted. |
| List callable agents | `GET /v1/agents` | Respects API-key scope. |
| Token totals | `GET /v1/usage` | On the default path, omit `user_id` → API-key owner. |
| Site-wide cost cap | `/v1/quota` | Same authenticated owner unless you opted into app_users. |
| List/download files from a chat | `GET /api/agents/{id}/files` | Always pass `?sessionId=` = session key or `session_id`. |
| Provision a FastClaw app_user | `POST /v1/users` | **Optional. Not the default website path.** |
| Admin / dashboard automation | `/api/*` or `fastclaw` CLI | Broader surface, not the minimal chat contract. |
| Coding-agent live preview | `docs/coding-agent-runtime.md` | Separate. |

For a normal website, start with `/v1/*` plus the file list URL. Do not build
against dashboard `/api/chat/*` unless you are embedding FastClaw's own UI.

## Authentication

```http
Authorization: Bearer fcak_...
```

| Key type | Intended use |
|---|---|
| `admin` | Platform automation. Server-side only. |
| `user` | App backend as one FastClaw owner account. |
| `agent` | App backend scoped to explicit agent IDs. Prefer this for one product capability. |

Do not put FastClaw keys in browsers or mobile apps. The website backend
calls FastClaw.

## Identity Model (default website path)

One website = one FastClaw account + one server-side API key.
Agents are **product capabilities** (support, writing), not one agent per
registered user.

| Concern | How |
|---|---|
| Which product | `agent_id` |
| Which registered user / thread | `X-Fastclaw-Session-Key: <app>:<user-id>:<conversation-id>` |
| FastClaw USER.md / MEMORY.md / Auto-remember | `X-Fastclaw-Chatter: app:<user-id>` and/or `params.user_id` (same string) |
| Display name, locale, short facts | `params` every turn |
| Per-registered-user billing | Your DB, keyed by the session-key prefix |
| Site-wide cap / agent token rollup | `/v1/quota` and `GET /v1/usage` on the **API-key owner** |

**Do not send** body `user` or `X-Fastclaw-End-User` on this path. Those
rebind the request to a FastClaw `app_user` (new UserSpace). Chatter is a
different field; End-User does **not** set it.

`POST /v1/users` remains for callers that truly need an `app_user` row. It
is not required for chat, files, or owner-level usage on the default path.

Leave **Auto-remember chatter** off on public template agents until you
send distinct chatter ids. Console + personal IM for operators is a
different setup: turn on **Shared identity across channels** on that
agent (existing toggle; not this API).

## Chat API

### `POST /v1/chat/completions`

OpenAI-compatible **shape**. Semantics are IM: FastClaw takes the **last**
`role=user` message only. Earlier items in `messages` are discarded.
Session history, system prompt, tools, compaction, and quota are assembled
inside the agent. Do not replay a full OpenAI transcript.

Required:

- `messages`: at least one `user` message (only the last one is used)

Headers:

| Header | Purpose |
|---|---|
| `Authorization` | Bearer API key |
| `X-Fastclaw-Agent-ID` | Agent if not in the body |
| `X-Fastclaw-Session-Key` | Conversation address. Omitted → FastClaw mints `api-…` for that turn |
| `X-Fastclaw-Chatter` | Chatter id for USER.md / MEMORY.md. See below |
| `X-Fastclaw-Channel` | Optional. Override reply channel (e.g. cron → IM) |
| `X-Fastclaw-End-User` | **Do not send** on the default website path |

Request:

```http
POST /v1/chat/completions
Authorization: Bearer fcak_...
Content-Type: application/json
X-Fastclaw-Session-Key: mysite:u_123:conv-8
X-Fastclaw-Chatter: app:u_123

{
  "agent_id": "agt_...",
  "model": "ignored-by-fastclaw-agent-config",
  "stream": true,
  "messages": [
    { "role": "user", "content": "帮我总结今天的订单异常" }
  ],
  "params": {
    "user_id": "app:u_123",
    "display_name": "Alice",
    "locale": "zh-CN",
    "known_facts": ["付费用户", "偏好短回复"]
  }
}
```

Body extensions:

| Field | Type | Purpose |
|---|---|---|
| `agent_id` | string | Agent to call. Body wins over header. Always send when the key can see more than one agent. |
| `user` | string | Rebinds the request to an `app_user`. **Not chatter. Omit on the default path.** |
| `params` | object | Per-turn context for the model. Not persisted. Optional `params.user_id` (string) is chatter when the header is omitted. |
| `images` | string[] | Image URLs / data URLs for vision; also landed in the session workspace. |
| `imageUrls` | string[] | Alias for `images`. |
| `attachments` | array | Non-vision files into the workspace (`url`, optional `name`). |

Attachment shape:

```json
{
  "attachments": [
    { "url": "https://example.com/report.pdf", "name": "report.pdf" }
  ]
}
```

Streaming is OpenAI SSE. The agent may run tools for a long time before
the first content token. FastClaw writes the role chunk immediately, then
an SSE comment every 10s:

```text
: heartbeat
```

Keep the connection open. Ignore lines that start with `:`.

Non-streaming responses add `session_key` and `session_id`. The `usage`
object on the completion is currently **zeros**. Do not bill from it.
Use `GET /v1/usage`.

Responses also set:

- `X-Fastclaw-Session-Key`: what you sent, or the auto-minted key
- `X-Fastclaw-Session-Id`: native row id (`s-...`)

File APIs accept either as `?sessionId=`.

### Chatter (memory identity)

`USER.md`, `MEMORY.md`, and Auto-remember key on
`(agent_id, chatter_user_id)`.

| Request | Chatter |
|---|---|
| Neither `X-Fastclaw-Chatter` nor `params.user_id` | `api-user` (all API visitors share one memory) |
| Only one set | That trimmed string |
| Both set and equal | That string |
| Both set and different | **400** |
| `params.user_id` present but not a string | **400** |
| Only body `user` / `X-Fastclaw-End-User` | Still `api-user` |

Send a stable prefixed id from your **backend**, for example
`app:u_123`. Do not let the browser call FastClaw with a client-chosen
chatter (it can impersonate another person's USER.md).

This does not switch UserSpace or billing. Cross-session product memory
beyond FastClaw's two markdown files: your store or Basic Memory HTTP,
then a short `params.known_facts` list. See `docs/upstream-basic-memory.md`.

### Session Key Guidance

```text
<app-name>:<upstream-user-id>:<conversation-id>
```

Reuse the key to continue a thread (that is session memory, including
compaction). Mint a new conversation-id for a new chat. Do not reuse one
key across users. Session keys also scope workspace files and how you
group usage on your side.

### Session files

```http
GET /api/agents/agt_.../files?sessionId=mysite:u_123:conv-8
Authorization: Bearer fcak_...
```

Download one file:

```http
GET /api/agents/agt_.../files/report.md?sessionId=mysite:u_123:conv-8
Authorization: Bearer fcak_...
```

`/workspace/report.md` in the model reply is `report.md`.
Always pass `sessionId`. The owner API key can list every file on the
agent if it is omitted.

If you did use End-User on chat (non-default), send the same
`X-Fastclaw-End-User` on file requests so the row lookup matches.

## Agents

### `GET /v1/agents`

```http
GET /v1/agents
Authorization: Bearer fcak_...
```

```json
{
  "agents": [
    { "id": "agt_...", "name": "agt_...", "model": "openai/gpt-4.1-mini" }
  ]
}
```

Create agents, clone templates, install skills, and configure providers
with the dashboard `/api/*` or the CLI — not `/v1`.

## Usage And Quotas

On the **default path** (no End-User), call these as the API-key owner.
Omit `user_id` unless you provisioned app_users and know you need that
row. Per registered-user spend belongs in your database (session-key
prefix). Per-agent rollup is `daily[].agentId`.

### `GET /v1/usage`

| Param | Required | Notes |
|---|---|---|
| `days` | no | Default `30`, max `90`. |
| `user_id` | no | FastClaw user id. Default path: omit (owner). |

```http
GET /v1/usage?days=30
Authorization: Bearer fcak_...
```

### `PUT /v1/quota`

```http
PUT /v1/quota
Authorization: Bearer fcak_...
Content-Type: application/json

{
  "user_id": "<owner-or-app-user-id>",
  "monthly_token_limit": 5000000,
  "monthly_request_limit": 10000,
  "reset_day": 1
}
```

`0` means no limit on that dimension. `reset_day` is `1..28`.

### `GET /v1/quota` / `DELETE /v1/quota`

```http
GET /v1/quota?user_id=<same-as-put>
DELETE /v1/quota?user_id=<same-as-put>
```

On this tree, quota GET/DELETE still require `user_id`. Use the owner
account id for a site-wide cap.

## Optional: app_user (not the default)

`POST /v1/users` or chat `user` / `X-Fastclaw-End-User` mints a FastClaw
`app_user` and **switches UserSpace**. Sessions and files then live under
that user. Chatter is still `api-user` unless you also send
`X-Fastclaw-Chatter`. Do not mix this with the default website path.

## Recommended Upstream Flow

1. One FastClaw account for the website. A few template agents. Keep
   **Auto-remember** off until chatter ids are unique per product user.
2. Create an `agent` or `user` API key. Store it server-side.
3. For each product conversation:
   - `POST /v1/chat/completions` with `agent_id`
   - `X-Fastclaw-Session-Key: <app>:<your-user-id>:<conversation-id>`
   - `X-Fastclaw-Chatter: app:<your-user-id>` (same string in `params.user_id`)
   - `messages` = this turn only
   - `params` = identity and optional short `known_facts`
   - do not send `user` / `X-Fastclaw-End-User`
   - persist `session_id` if you want dashboard-style ids
   - list files with `?sessionId=` = that key or `session_id`
4. Optional cross-session store: Basic Memory HTTP from **your** backend,
   then `params.known_facts`. See `docs/upstream-basic-memory.md`.
5. Site-wide cap: `PUT /v1/quota` for the owner. Agent totals:
   `GET /v1/usage` and sum `daily[].agentId`. Per registered user: your DB.

## Error Shape

```json
{
  "error": {
    "message": "agent not found",
    "type": "not_found_error"
  }
}
```

| Status | Meaning |
|---|---|
| `400` | Bad body, no user message, chatter mismatch, `params.user_id` not a string, etc. |
| `401` | Missing/invalid API key. |
| `404` | Agent not found or not on this API key. |
| `429` | Rate limited or quota exceeded. |
| `503` | Usage/quota subsystem not configured. |

## What To Give An Agent

1. This document.
2. `docs/upstream-basic-memory.md` if the product uses BM.
3. FastClaw base URL, API key type/scope, agent id.
4. Your user-id field (session-key middle segment + chatter).
5. Your conversation-id field (session-key suffix).
6. Whether owner usage/quota must be wired.

Skill:

```text
skills/fastclaw-api-integration/SKILL.md
```
