# Website + Basic Memory (HTTP, not FastClaw MCP)

Product-user **cross-session** memory is **your backend**, not FastClaw.
FastClaw does not call Basic Memory. Do not mount the BM MCP on a
shared template agent.

Canonical product decisions: `docs/upstream-website-model.md` (§8.3)
when that page is on your branch; this file is the operational recipe.

## What FastClaw does vs what you do

| Layer | Owner |
|---|---|
| This conversation | FastClaw session key |
| Who is speaking this turn | `X-Fastclaw-Chatter` / `params.user_id` + display fields |
| Long-term facts across conversations | **Your** BM project, injected as `params.known_facts` |
| FastClaw `USER.md` / Auto-remember | Optional extra once chatter is set; not a substitute for BM |

## One project per product user

BM has no SaaS `user_id`. You name the project:

```text
name = "app-" + <your stable user id>
```

Your backend (API key stays server-side):

1. `POST https://api.basicmemory.com/v2/projects/resolve`  
   `{ "identifier": "app-u_123" }`
2. If missing: `POST /v2/projects/` with that `name` (and a path, local
   or Cloud workspace as you configured).
3. Keep the returned project UUID. Cache it against your user row.

Local BM is `http://localhost:8000` instead of the Cloud host.
See [Projects API](https://basicmachines-co-basic-memory.mintlify.app/api/rest/projects)
and [REST overview](https://basicmachines-co-basic-memory.mintlify.app/api/rest/overview).

## Each chat turn

```text
user message
  → your DB: persist the bubble (client_msg_id)
  → BM: POST /v2/projects/{project_uuid}/search/
        { "text": "<this turn or a short profile query>", "page_size": 5 }
  → trim to a few short sentences (hundreds of tokens, not the dump)
  → POST FastClaw /v1/chat/completions
       X-Fastclaw-Session-Key: app:<user>:<conversation>
       X-Fastclaw-Chatter: app:<user>
       params.user_id = same string
       params.known_facts = ["…", "…"]
       messages = this turn only
  → SSE → your transcript
  → optional: write new facts back to the same BM project (dedupe)
```

Search: [Search API](https://basicmachines-co-basic-memory.mintlify.app/api/rest/search).  
If BM times out, call FastClaw with empty `known_facts`. Do not block chat.

On the template agent SOUL.md, say that `params.known_facts` are verified
facts (FastClaw still labels `params` as client parameters).

## Do not

- Register BM MCP on the public/template agent.
- Let the browser hold the BM key or talk to FastClaw.
- Inject the full BM note dump every turn.
- Use body `user` / `X-Fastclaw-End-User` to “select” a BM user.
- Expect FastClaw `#48` chatter plumbing to create BM projects.

## Write-back

After the turn, if you extract 1–3 durable facts, create or update a
note **in that same project** via BM’s knowledge/entities API. Use your
own fact ids so retries do not duplicate. FastClaw Auto-remember is a
different store (`USER.md`); leave it off on template agents unless you
intentionally want both.
