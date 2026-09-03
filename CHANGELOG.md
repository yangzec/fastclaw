# Changelog

Notable changes to FastClaw. Items marked **BREAKING** require operator
action on upgrade — read those notes before deploying.

## [Unreleased]

### Fixed

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
- **Per-model context and max output on the Models page.** Both
  fields are chips plus a tip for that model. Context: 128k / 200k /
  256k / 400k / 500k / 1M / 1.05M — GPT-5.5 and 5.6 both suggest
  1.05M on the API (the 400k you remember is the old ChatGPT 5.5
  product tier); Grok 500k; GLM 1M; Kimi 1.05M. Max output: 8k /
  16k / 32k / 64k / 128k (plus 131k for Kimi). GPT-5.5 suggests
  32k; GPT-5.6 suggests 64k because `max` reasoning shares the
  output budget. Saving writes the catalog row; unset maxTokens
  stays 8192. `gpt-5.6-luna` context default is 1.05M.
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
