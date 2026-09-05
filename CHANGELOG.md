# Changelog

Notable changes to FastClaw. Items marked **BREAKING** require operator
action on upgrade — read those notes before deploying.

## [Unreleased]

### Added

- **SSH hosts test on add.** Creating or changing a host (address,
  user, auth, cwd, credentials) probes with `echo ok` first. Failure
  returns 400 and leaves the address book unchanged. Rows store
  `last_test_status` / error / tested-at; the Settings list shows
  live pool Connected plus last-probe Failed / Not tested. The
  dialog primary action is **Test and save**.
- **Feishu 待办 / 文档 read + confirm-to-edit.** Chat can list and
  get official tasks (`feishu_list_tasks`, `feishu_get_task`) and
  append text or retitle a 云文档 (`feishu_append_doc`). Completing
  or updating a task and modifying a doc require a two-step
  `confirm_token`: first call only previews, the second applies
  after the user agrees.   QR login now also requests `task:task:read` plus the v1
  `task:task` / `task:task:readonly` scopes so the bot can list
  todos it created. Existing bots still need a rescan for new
  scopes.
- **Knowledge architecture notes.** [`docs/knowledge-architecture.md`](docs/knowledge-architecture.md)
  records what the per-agent Knowledge page actually does (text files,
  256KB, keyword / CJK-bigram search) and when *not* to bolt on
  SiliconFlow embeddings, Dify, Volcengine knowledge / OpenViking /
  Mem0. Chat models can still use SiliconFlow via a Custom provider.
- **This-turn token line in the composer.** After a reply finishes, the
  footer shows `12.4k → 890` next to the context-window meter (hover
  for in/out/cache). Distinct from the session context bar.
- **Insert cancels the in-flight provider stream.** Queue still waits
  for the turn; Insert now aborts the current completion, keeps the
  partial assistant text, and turns around in the same turn. Phones
  show both Queue and Insert next to Stop. Last tokens on the cut
  bubble are not dropped when Insert races the LLM cancel.

### Changed

- **Upstream website usage/quota is the API-key owner.** `GET /v1/usage`
  always returns the key owner's ledger (`user_id` is ignored, which
  also closes the previous IDOR). `/v1/quota` is a site-wide cap on
  that same owner; `user_id` is optional and must match. **BREAKING**
  for clients that set quota or expected usage on an app_user id.
- **First run no longer trains the operator.** Rule: don't ask what
  already has a default; after the last required field, enter the
  product; if the machine already has a key, don't ask a human.
  `fastclaw` auto-inits from `OPENAI_API_KEY` / sibling env vars, or
  opens the browser and waits. The wizard is one screen (account +
  key) and **Start chatting** lands in the first agent. Login, `/`,
  and signup do the same. Empty chat offers three click-to-send
  starters. The first agent is named `Assistant`. Login and Overview
  pick the **oldest** agent (API lists newest first). Overview shows
  that agent's model and links Model / Tools / Runtime to the pages
  that change them. Scheduler empty state says to ask in chat.
  Customize tabs hint what to write. Chat follow-up controls stay in
  English. The composer model picker no longer crashes (Base UI #31).
  A second agent inherits the default / oldest sibling model so it
  talks without another Models visit. Existing agents without a
  model do the same at chat time. Settings groups into Talk / Teach
  / Connect / Run. Empty chat has Adjust this agent. Skills empty
  state offers three featured installs. `/cron/` and `/channels/`
  open the current agent. Feishu / WeCom cards stay in English.

### Fixed

- **Feishu `feishu_list_tasks` no longer returns an empty 待办 list.**
  Official task v2 list (`type=my_tasks`) requires a user token.
  FastClaw only has the bot's tenant token, so "我负责的" was always
  empty. Listing now uses task v1 (app-created tasks) and, on Feishu
  chat, filters to the current sender's assignments.
- **Knowledge uploads accept multiple files and keep CJK names.** The
  picker is `multiple`; the server no longer turns `产品说明.md` into
  `----.md`. Path separators and control characters are still stripped.
- **Closed-door config refusals name the real door and forbid retry.**
  `configure_agent` / `agents config` errors for mcpServers, plugins,
  tools, and provider.* now point at the dashboard (or the matching
  CLI) and say not to retry, `--help`, or read source. The tool
  description lists allowed keys; one standing prompt rule covers the
  whole class. Docs alone never reach the model on the failure path.

- **Chat replies no longer leak literal ` ``` `.** Models often open
  a fence and never close it, wrap the whole answer in ` ```markdown `,
  or glue ` ``` ` onto the next sentence. The previous repair broke
  those markers with a zero-width space, so Streamdown stopped
  swallowing the bubble — but the backticks still rendered. The
  repair now unwraps a whole-message prose fence and *strips*
  stray openers / mid-line fences when the body looks like chat
  prose. Real language fences (` ```js `) and plain log dumps stay.
  WeCom's classic markdown does not render fences or GFM tables, so
  outbound WeCom text now drops fence markers and flattens tables
  the same way other IM channels already do.

### Changed

- **API chatter is no longer hardcoded to `api-user`.**
  `POST /v1/chat/completions` reads `X-Fastclaw-Chatter` and/or
  `params.user_id` (string) for USER.md / MEMORY.md / Auto-remember
  routing. One value or matching values win; both set and different
  is `400`. Neither set still falls back to `api-user`. This does
  **not** switch UserSpace — `user` / `X-Fastclaw-End-User` stay on
  the app_user path. Website backends should send a stable prefixed
  id such as `app:<their-user-id>`. Cross-session Basic Memory stays
  on the website backend; recipe in `docs/upstream-basic-memory.md`.
  `docs/upstream-api.md` and the integration skill now describe the
  default website path (session key + chatter, no End-User).

- **Website integration decision record.**
  `docs/upstream-website-model.md` captures the agreed model (template
  agents, session keys, params, billing owner, no End-User default).

- **Guests cannot inventory an agent's config.** Non-owner chatters
  can no longer `exec` `cat` persona files or `SKILL.md`, see those
  names in `list_dir`, or receive `create_agent` / `configure_agent` /
  `install_skill` / `host_exec` in the tool catalog. The
  confidentiality prompt also forbids dumping tool schemas.

- **Host shell is super_admin-only.** On a shared/self-hosted gateway,
  owning an agent no longer grants host `exec`, `host_exec`, or
  absolute host-path file tools. Those require a resolved FastClaw
  account with role `super_admin`. Agent owners can still edit persona
  files, provision sibling agents, and use write-mode slash commands.
  Cron / subagent / shared-identity group turns never get a host
  shell. `/whoami` now prints `Agent admin` and `Host access`
  separately. **BREAKING** for multi-tenant installs where a regular
  `user` previously ran host commands from web chat.

### Added

- **Composer context meter and model switcher.** The chat input shows
  working-set tokens vs the model's context window (same estimate
  compaction uses), and a model button next to Send lists configured
  provider models. Switching writes the agent's model; viewers see
  the name only.
- **Per-model context and max output on the Models page.** Each
  model row applies a recommended pair automatically (banner:
  「推荐已套用」). GPT-5.6 = 1.05M / 64k, GPT-5.5 = 1.05M / 32k,
  GLM = 1M / 64k, Kimi = 1.05M / 131k, Grok = 500k / 32k. Chips
  stay collapsed until 「调整」; 「套用推荐」 restores the pair.
  Old 200k / 8k rows pick up the new defaults when you open the
  dialog. `gpt-5.6-luna` context default is 1.05M. Claude / DeepSeek /
  Gemini / Qwen get a matching tip instead of “没认到 200k”.
- **Codex-style follow-up queue.** Sending while a turn is running now
  queues by default (composer tray + TUI list) and runs after `done`,
  instead of always inserting into the current turn. Switch the
  composer to **插入** (or press ⌘/Ctrl+Enter / TUI Ctrl+S) to steer
  the in-flight turn. Per-item **插入** promotes a queued line; Stop
  still aborts and leaves the queue intact.
- **MCP JSON configuration.** Add Server opens a dialog (JSON by
  default, form as the other tab). Paste Cursor or Claude Desktop
  `mcp.json` (or a name→config map) and save — servers become
  cards on the page. `type` is optional — `url` means http,
  `command` means stdio. Agent-page cards also list tools that
  attached after save; no FastClaw process restart. On phones,
  Settings lists MCP in a wrapping grid so it is not hidden
  behind a sideways swipe.
- **Composer autofocus on new and switched chats.** Opening a new
  conversation or switching sessions (including project chats) puts
  the caret in the message box. A second click on New chat does the
  same even when the URL is already the empty-chat route.
- **Configurable inherit scope for MCP, plugins, and skills.**
  Catalog items are no longer attached to every agent by default.
  MCP, plugins, and skills all use `inherit: "all" | "none"`
  (empty = none): Share with agents is an explicit opt-in in the
  admin catalog. **BREAKING:** global / bundled skills no longer
  attach until an operator turns that switch on. Agent-local,
  personal, and team copies still load without the flag. System
  `inherit=all` is the only platform-wide share; a user-scope
  `inherit=all` stays inside that tenant.
- **Multi-tenant isolation for global catalogs.** GET `/api/config`
  no longer merges system MCP / plugin entries / skill entries into
  another tenant's editor view, so platform secrets are not leaked
  or written back into a user row. Runtime attach still applies
  system `inherit=all` items. Plugin binaries and the
  `~/.fastclaw/skills` tree remain install-wide.
- **Global MCP servers.** The sidebar has an MCP page (platform
  admin edits the system catalog; other users edit their own) that
  writes `mcpServers` through `/api/config`. Same-name agent
  overlays still win. An agent can disable an inherited server
  (`disabled: true`) without deleting the shared definition.
  Secrets in headers/env are masked on GET and restored on a
  no-op save. The agent MCP page lists `inheritedMcpServers`
  so Inherited badges match runtime attach.
- **Plugin process vs inherit.** Enabling a plugin starts the
  process. Share with agents attaches its hooks. An agent overlay
  can still turn one off or on; Reset drops the overlay. Missing
  both a shared catalog entry and an overlay stays off.
- **Zhipu / Kimi / Grok / latest GPT context defaults.** Models and
  onboard presets now include 智谱 (`glm-5.3`, `glm-5.3-flash`, 1M),
  Kimi (`kimi-k3`, 1,048,576), Grok (`grok-4.6` / `grok-4.5`, 500k —
  the current xAI flagship is not 1M), and GPT-5.6 (`gpt-5.6` /
  `gpt-5.6-sol`, 1.05M). These numbers only prefill the Context window
  field — you can type any size; the saved value is what compaction
  uses. "Reset to default" restores the official size. CLI
  `--provider zhipu|kimi|grok` fills the official OpenAI-compatible
  base URL.
- **Chat composer drag-and-drop attachments.** Dropping files onto
  the message input attaches them the same way as the paperclip
  picker and image paste (chips, then upload on send).
- **Inherited global skills on the agent Skills page.** The agent
  Skills tab lists global `~/.fastclaw/skills` entries as
  **Inherited** (same merge the runtime uses). An agent-local copy
  with the same name still wins and can be removed without deleting
  the global skill.
- **Feishu official calendar, tasks, and docs from chat.** The
  connected Feishu / Lark bot can create 日程, 待办, and 云文档
  via OpenAPI (`feishu_create_event`, `feishu_create_task`,
  `feishu_complete_task`, `feishu_create_doc`, `feishu_read_doc`)
  using the same tenant token as IM. QR login now requests
  calendar / task / docs-create / share scopes. Bots created
  before this update must disconnect and scan again.
- **WeCom (企业微信) scan-to-create + long-connection.** New
  channel type `wecom` (does not reuse personal WeChat iLink).
  The dashboard opens Tencent's official `@wecom/wecom-aibot-sdk`
  scan popup; after the phone confirms, FastClaw stores BotID +
  long-conn Secret and subscribes on
  `wss://openws.work.weixin.qq.com` (`aibot_subscribe`). Inbound
  `aibot_msg_callback` is replied with `aibot_respond_msg`;
  enter-chat gets the official welcome command; typing uses a
  stream bubble. Paste BotID/Secret remains as fallback. One bot
  keeps one live socket (new subscribe kicks the old). See
  https://developer.work.weixin.qq.com/document/path/101463
- **Feishu / Lark scan-to-create.** Connecting a Feishu channel no
  longer requires creating a custom app in the developer console.
  The dashboard now uses the official OAuth 2.0 Device Authorization
  Grant flow (`registration.RegisterApp`): scan a QR code in the
  Feishu or Lark phone app and the platform creates a PersonalAgent
  bot with the right scopes and `im.message.receive_v1`, then
  FastClaw stores the issued App ID / Secret and starts long-connection
  inbound. Manual App ID + Secret paste remains as a fallback.
  See https://open.feishu.cn/document/mcp_open_tools/scan-to-create-an-app-in-one-click
  The QR flow now requests the official agent-app checklist
  (receive/send, cards, chat membership, reactions, docs comments,
  `offline_access`, `im.message.receive_v1` plus bot-added / reaction
  / doc-comment events and `card.action.trigger`). Credentials persist
  as soon as the phone confirms (so closing the dialog cannot drop the
  binding), and FastClaw DMs the scanning user a welcome so the 1:1
  chat is opened.
- **Feishu typing / reply-to status.** While the agent is working, the
  bot adds a `Typing` reaction on the user's message (same as Hermes)
  and replies in-thread so the turn is quoted. The reaction is cleared
  when the reply is delivered, or replaced with `CrossMark` if send
  fails.

### Fixed

- **Gateway no longer panics on a content-only tool failure.**
  `isFailedToolResult` can be true from an HTTP 4xx/5xx body (or the
  analyze-error marker) while `r.err` is nil. The non-streaming loop
  called `r.err.Error()` and crashed the process. Same nil-guard the
  streaming path already had.
- **Compaction now clips a giant tool result in the keep-recent tail.**
  One 500KB `exec` dump used to be "recent" and survive summarize
  almost intact (`202996 → 202907` tokens), then overflow-retry 502'd
  again because the configured window was larger than the upstream
  limit. Tail tool payloads are capped at 8k runes; ingest clips any
  tool return over 64KiB. Logging no longer prints `elapsed=2562047h`
  when AfterToolCall has no start time.
- **Overflow retry no longer trusts a 1M Models window.** A Zhipu /
  Kimi / GPT default of 1,000,000 sets the compact threshold near
  900k, so a 203k request that already 502'd never hard-trimmed.
  Force-compact after overflow now aims at half the rejected size.
- **Settings → Customize Save is clickable.** The dialog close
  control sat on top of the page Save button, so a click closed
  the sheet instead of writing SOUL.md / IDENTITY.md. The panel
  now clears the close button, and a failed PUT shows an error
  instead of a false Saved state.
- **Customize Save writes every edited tab.** One Save used to
  PUT only the visible file, then reload wiped in-memory edits
  on Soul / Bootstrap / the rest. Dirty tabs get a dot; the
  button says how many files will be written. Switching to
  Profile / MCP no longer unmounts those edits, Close asks
  before discarding them, and a failed GET after PUT no longer
  blanks a tab that just saved.
- **`make build` works with pnpm 11.** Overrides live in
  `web/pnpm-workspace.yaml` so `pnpm install --frozen-lockfile` no
  longer fails with `ERR_PNPM_LOCKFILE_CONFIG_MISMATCH`. The same
  pins stay in `package.json` for pnpm 9/10. Native install scripts
  for `sharp` / `msw` / `unrs-resolver` are allowed so pnpm 11 does
  not abort on `ERR_PNPM_IGNORED_BUILDS`.
- **Follow-up Stop is per conversation.** Stopping chat B no longer
  prevents chat A's queued follow-ups from sending when A's turn
  finishes. Coming back to an idle chat whose turn ended while you
  were elsewhere now sends the next queued item.
- **Compaction follows Pi's working-set shrink.** Auto-compact and
  `/compact [focus…]` now keep a ~20k-token verbatim hot tail (not
  the last 20 messages), ask the model for a structured Goal /
  Constraints / Progress / Decisions / Next Steps / Critical Context
  handoff, and attach `<read-files>` / `<modified-files>` so the next
  turn can rehydrate. The loop also compact-checks after each tool
  round and, on a context-overflow 400, force-compacts once and
  retries the turn. Successful summarize no longer tells the user to
  `/new`; that hint stays on hard-trim only.
- **Compaction no longer blanks old tool results before summarizing.**
  The first pass used to replace every tool output older than 20
  messages with a placeholder, then (if still over budget) ask the
  model to summarize a history that no longer had the search hits /
  file reads / exec output. Summarize now runs first and keeps capped
  tool findings in the summarizer prompt. Local prune is only the
  fallback when summarize is unavailable or fails.
- **Refreshing the chat page killed in-flight tools.** The stream
  handler detached the agent from the HTTP request context
  (`context.WithoutCancel`) so a refresh would not cancel the turn,
  then immediately `defer cancel()`'d that same context when the
  browser disconnected. Exec / fetch / subagent calls saw
  `context.Canceled` and stopped. The handler now leaves the detached
  timeout running after a client drop. Reload also keeps unfinished
  tools spinning and resumes `tool_call` / `tool_result` over
  `/api/chat/subscribe` instead of painting them `(stopped)`.
  **Stop** now calls `POST /api/chat/stop` so it still cancels the
  server turn after a refresh (aborting the browser SSE is no longer
  enough).
- **Chat bubbles swallowed the rest of a reply into one numbered
  code card.** A stray or unclosed ` ``` ` (common when the model
  quotes an `API error 400` dump) is a CommonMark fence that runs
  to EOF, so later `**bold**`, lists, and `` `inline code` ``
  rendered as literal text. Completed chat markdown now breaks
  that fence when the body looks like prose, and mid-line ` ``` `
  is no longer treated as a fence opener.
- **Oversized sessions kept getting HTTP 400 after compaction
  failed.** Compression called the summarizer with a nil
  `context.Context`, which surfaced as `net/http: nil Context`. The
  fallback then kept the still-huge pruned history (~400k tokens) and
  `llmRetry` retried the resulting `invalid_request_error` three
  times. Compaction now passes a real context, caps summarizer input,
  and hard-trims oldest turns until the history fits. After
  auto-compaction the user is told to send `/new` if they want a
  clean session (the chat UI still shows the archived thread, but
  the model only sees the compacted working set). Non-transient
  4xx LLM errors are no longer retried.
- **Cron jobs fired ~hours late on Postgres when the server ran in a
  non-UTC timezone.** The `cron_jobs` time columns were declared
  `TIMESTAMP WITHOUT TIME ZONE`. A Go `time.Time` carrying a non-UTC
  offset (e.g. `Asia/Shanghai`) was written with its offset silently
  dropped, so a Beijing "09:00" job was stored as `09:00` and read back
  as `09:00 UTC` = `17:00 Beijing` — firing 8h late (or, in the opposite
  direction, never matching `next_run <= now()` at all). SQLite was
  unaffected. The columns are now `TIMESTAMPTZ`, which preserves the
  instant across write/read regardless of offset or session `TimeZone`.

### BREAKING — cron schedule state is reset on upgrade (Postgres only)

When a Postgres deployment runs the schema migration for the first time,
**every existing row in `cron_jobs` is deleted.** This is deliberate:
the stored `next_run` wall-clocks already carry the wrong timezone, so
converting them would freeze the bug into the new column type. A clean
reschedule is the correct recovery.

- **What is lost:** pending scheduled jobs (recurring `cron`, `interval`,
  and not-yet-fired `once` reminders).
- **What is NOT lost:** chat history (`sessions`, `session_messages`),
  agent identity files, provider/channel config — none of these are
  touched.
- **Operator action required:** after upgrading, any recurring schedule
  a user relies on (e.g. "every day at 9am") must be recreated by asking
  the agent again, or via the dashboard's Scheduler tab. The original
  `create_cron_job` tool calls are still visible in chat history and can
  serve as a reference for what to rebuild.
- **Visibility:** the gateway logs a single
  `level=WARN msg="resetting cron schedule state for timestamptz migration …"`
  line with the row count before wiping, so operators can tell from the
  upgrade log whether any jobs were affected.
- **SQLite deployments:** unaffected — the migration is skipped entirely.
- **Idempotent:** re-running the migration (e.g. on every daemon boot)
  is a no-op once the columns are already `timestamptz`.
