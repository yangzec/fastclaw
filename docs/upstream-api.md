# FastClaw Upstream App Integration API

This is the integration contract for upstream applications that want to use
FastClaw as their Agent runtime.

Use this document when building a SaaS, bot, marketplace, or workflow product
that owns its own users but delegates agent execution, tools, memory, usage
metering, quota checks, and optional channels to FastClaw.

## Which Interface To Use

| Need | Interface | Notes |
|---|---|---|
| End-user chat with an agent | `/v1/chat/completions` | OpenAI-compatible, plus FastClaw extensions. This is the primary upstream app API. |
| List callable agents for an API key | `GET /v1/agents` | Respects API-key scope. |
| Provision one upstream end-user | `POST /v1/users` | Optional. Not used for billing. Prefer session keys for isolation. |
| Query usage / token spend | `GET /v1/usage` | Owner-account totals. Roll up by `daily[].agentId`. |
| Set a site-wide cost cap | `/v1/quota` | Monthly token/request ceiling on the API-key owner. |
| List/download files from a chat | `GET /api/agents/{id}/files` | Same API key as `/v1`. Pass `?sessionId=` = `X-Fastclaw-Session-Key` or the returned `session_id`. |
| Admin/dashboard automation | `/api/*` or `fastclaw ...` CLI | Cookie/admin oriented, broader surface, not the minimal upstream app contract. |
| Coding-agent live preview runtime | `docs/coding-agent-runtime.md` | Project/runtime endpoints are documented separately. |

For a normal upstream product, start with `/v1/*`. Do not build against
dashboard internals unless the product is also administering FastClaw.

## Authentication

Use an API key:

```http
Authorization: Bearer fcak_...
```

API key types:

| Type | Intended use |
|---|---|
| `admin` | Platform automation. Can operate broadly. Keep server-side only. |
| `user` | App backend acting as one FastClaw owner account. Can use that owner's agents. |
| `agent` | App backend scoped to explicit agent IDs. Recommended for single-agent integrations. |

Do not expose FastClaw API keys in browsers or mobile apps. The upstream
backend calls FastClaw. Map your product user to a deterministic
`X-Fastclaw-Session-Key`, not to a FastClaw `user` / app_user.

## Recommended model (one website, one FastClaw account)

Use **one FastClaw user** (the website account) and **one server-side API
key**. Create **a few template agents** (support, writing, …) — one agent
per *product capability*, not one agent per registered user.

| Concern | How to do it |
|---|---|
| Which product | `agent_id` on each chat |
| Which registered user / conversation | `X-Fastclaw-Session-Key: <app>:<user-id>:<conversation-id>` |
| Who the model should treat as the speaker | `params` this turn (name, plan, locale). Not persisted. |
| Token totals per agent | `GET /v1/usage` → sum `daily[]` by `agentId` |
| Token totals per registered user | Your DB, keyed by the session-key prefix. FastClaw does not bill per app_user. |
| Site-wide cost cap | `PUT /v1/quota` on the API-key owner |
| Cross-session FastClaw MEMORY.md / USER.md | Leave **Auto-remember chatter** off (default). API chats share chatter `api-user`; those files are not per website user. |

Do **not** send `user` or `X-Fastclaw-End-User` on the default path. That
rebinds the request to an app_user: session rows move, `/v1/usage` used
to look empty, and owner quota did not apply. Usage and quota APIs now
always use the API-key owner, but chat isolation still depends on the
session key.

`POST /v1/users` remains for callers that need an app_user row. It is
not required for chat, files, usage, or quota.

Keep public template agents on the default **Auto-remember chatter =
off**. Turn it on only for a private / seat agent that should distill
into USER.md / MEMORY.md.

Do not create one FastClaw agent per registered user. A UserSpace loads
every agent owned by that account into memory.

## Chat API

### `POST /v1/chat/completions`

OpenAI-compatible endpoint with FastClaw extensions.

Required:

- `messages`: array with at least one `user` message

Agent selection:

- Preferred: body field `agent_id`
- Alternative: `X-Fastclaw-Agent-ID` header
- If omitted, FastClaw resolves the default accessible agent for the caller

Session selection:

- Header: `X-Fastclaw-Session-Key`
- If omitted, FastClaw creates an API session key for that turn

Request:

```http
POST /v1/chat/completions
Authorization: Bearer fcak_...
Content-Type: application/json
X-Fastclaw-Session-Key: chat-upstream-user-123-default

{
  "agent_id": "agt_...",
  "model": "ignored-by-fastclaw-agent-config",
  "stream": true,
  "messages": [
    { "role": "user", "content": "帮我总结今天的订单异常" }
  ],
  "params": {
    "user_id": "upstream-user-123",
    "display_name": "Alice",
    "tenant_id": "tenant_1",
    "locale": "zh-CN"
  }
}
```

FastClaw extensions:

| Field | Type | Purpose |
|---|---|---|
| `agent_id` | string | Agent to call. Body wins over header. Always send this when the account has more than one agent. |
| `user` | string | Optional. Rebinds the request to an app_user. **Do not send** on the default website path. Isolation is the session key. |
| `params` | object | Per-turn structured context shown to the agent (identity, plan, locale). Not persisted. |
| `images` | string[] | Image URLs/data URLs shown to vision models and materialized into workspace. |
| `imageUrls` | string[] | Alias for `images`. |
| `attachments` | array | General files materialized into workspace. Use for PDFs, docs, zips, etc. |

Attachment shape:

```json
{
  "attachments": [
    {
      "url": "https://example.com/report.pdf",
      "name": "report.pdf"
    }
  ]
}
```

Streaming responses follow OpenAI SSE shape:

```text
data: {"id":"chatcmpl-...","object":"chat.completion.chunk","choices":[{"delta":{"content":"..."},"index":0}]}

data: [DONE]
```

The agent may run tools for a long time before the first content token.
During that silence FastClaw still writes the role chunk immediately,
then an SSE comment every 10s:

```text
: heartbeat
```

Keep the HTTP connection open. Ignore lines that start with `:`. Do not
treat a quiet period as a hang and close the socket.

Non-streaming responses follow OpenAI response shape, plus two FastClaw
session fields:

```json
{
  "id": "chatcmpl-...",
  "object": "chat.completion",
  "model": "agent",
  "session_key": "chat-upstream-user-123-default",
  "session_id": "s-...",
  "choices": [
    {
      "index": 0,
      "message": { "role": "assistant", "content": "..." },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 0,
    "completion_tokens": 0,
    "total_tokens": 0
  }
}
```

Streaming and non-streaming responses also set:

- `X-Fastclaw-Session-Key`: the key you sent (or the auto-minted `api-...` key)
- `X-Fastclaw-Session-Id`: the native row id (`s-...`) used by dashboard URLs

`session_key` is your conversation address (ChatID). `session_id` is the
stored session row. File APIs accept either as `?sessionId=`.

The `usage` object on the completion is currently zeros. Do not bill from
it. Poll `GET /v1/usage` for owner totals.

Send **only this turn's user text** in `messages`. FastClaw already stores
the session transcript. Replaying the full OpenAI history duplicates
context.

### Session Key Guidance

Choose a deterministic session key from your product model:

```text
<app-name>:<upstream-user-id>:<conversation-id>
```

Reuse the same key to continue a conversation (that is the session
memory). Mint a new conversation-id for a new thread. Do not reuse one
key across users. Session keys control chat history, workspace files,
and how you group usage on your side.

### Session files

```http
GET /api/agents/agt_.../files?sessionId=chat-upstream-user-123-default
Authorization: Bearer fcak_...
```

or the same URL with `sessionId=s-...`. Download a single file:

```http
GET /api/agents/agt_.../files/report.md?sessionId=chat-upstream-user-123-default
Authorization: Bearer fcak_...
```

`/workspace/report.md` in the model reply is the same file as `report.md`.

Always pass that user's `sessionId`. The API key can list every file on
the agent if `sessionId` is omitted — isolation is your backend's job.
Do not put `?token=` file URLs on a public page.

## Agents

### `GET /v1/agents`

Lists only agents accessible to the API key.

```http
GET /v1/agents
Authorization: Bearer fcak_...
```

Response:

```json
{
  "agents": [
    {
      "id": "agt_...",
      "name": "agt_...",
      "model": "openai/gpt-4.1-mini"
    }
  ]
}
```

For provisioning agents, cloning templates, installing skills, or configuring
providers, use dashboard `/api/*` endpoints or the `fastclaw` CLI. Those are
operator/admin workflows, not the minimal end-user chat API.

## Usage And Quotas

Usage and quota are the **API-key owner** bucket (the website FastClaw
account). `user_id` and `X-Fastclaw-End-User` do not select another
person's ledger. Per registered-user billing belongs in the upstream app
(session-key prefix). Per-agent rollup is `daily[].agentId`.

### `GET /v1/usage`

Query params:

| Param | Required | Notes |
|---|---|---|
| `days` | no | Default `30`, max `90`. |
| `user_id` | no | Ignored. Kept so old clients do not break. |

```http
GET /v1/usage?days=30
Authorization: Bearer fcak_...
```

Response:

```json
{
  "userId": "u_...",
  "days": 30,
  "daily": [
    {
      "day": "2026-06-26",
      "agentId": "agt_...",
      "model": "gpt-4.1-mini",
      "inputTokens": 100,
      "outputTokens": 50,
      "cacheReadTokens": 0,
      "cacheCreationTokens": 0,
      "requestCount": 1
    }
  ],
  "totals": {
    "inputTokens": 100,
    "outputTokens": 50,
    "cacheReadTokens": 0,
    "cacheCreationTokens": 0,
    "requestCount": 1
  }
}
```

### `PUT /v1/quota`

Sets the monthly ceiling for the API-key owner (site-wide kill switch).
`user_id` is optional; when present it must be the owner.

```http
PUT /v1/quota
Authorization: Bearer fcak_...
Content-Type: application/json

{
  "monthly_token_limit": 5000000,
  "monthly_request_limit": 10000,
  "reset_day": 1
}
```

Limits of `0` mean no limit for that dimension. `reset_day` must be `1..28`;
invalid values are normalized to `1`.

### `GET /v1/quota`

```http
GET /v1/quota
Authorization: Bearer fcak_...
```

Returns the configured quota and, when usage metering is enabled, current
status.

### `DELETE /v1/quota`

```http
DELETE /v1/quota
Authorization: Bearer fcak_...
```

Removes explicit quota and reverts the owner to unlimited FastClaw-side quota.

## Recommended Upstream Flow

1. Operator creates one FastClaw account for the website and a few
   template agents (keep **Auto-remember chatter** off).
2. Operator creates an `agent` or `user` API key. Store it server-side.
3. For each product conversation:
   - `POST /v1/chat/completions` with `agent_id`
   - `X-Fastclaw-Session-Key: <app>:<your-user-id>:<conversation-id>`
   - `messages` = this turn only
   - `params` = who the speaker is (optional)
   - do not send `user` / `X-Fastclaw-End-User`
   - persist `session_id` if you want dashboard-style ids
   - list/download files with `?sessionId=` = that key or `session_id`
4. Site-wide cap: `PUT /v1/quota` (owner bucket).
5. Agent totals: `GET /v1/usage` and sum by `agentId`. Per registered
   user: sum your own session keys.

## Error Shape

FastClaw returns JSON errors compatible with OpenAI-style clients:

```json
{
  "error": {
    "message": "agent not found",
    "type": "not_found_error"
  }
}
```

Common statuses:

| Status | Meaning |
|---|---|
| `400` | Bad request body, missing messages, etc. |
| `403` | Quota `user_id` is not the API-key owner. |
| `401` | Missing/invalid API key. |
| `404` | Agent not found or not accessible by this API key. |
| `429` | Rate limited or quota exceeded. |
| `503` | Usage/quota subsystem not configured for that endpoint. |

## What To Give An Agent

When asking an AI coding agent to integrate an upstream app with FastClaw,
give it:

1. This document.
2. The FastClaw base URL.
3. API key type and scope.
4. Agent ID.
5. Your product user-id field (goes in the session key and optional `params`).
6. Your conversation/session ID field (session-key suffix).
7. Whether you need owner-level usage/quota.

If the agent supports Skills, install or load:

```text
skills/fastclaw-api-integration/SKILL.md
```

That skill is the short operational version of this API contract.
