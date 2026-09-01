# Changelog

Notable changes to FastClaw. Items marked **BREAKING** require operator
action on upgrade — read those notes before deploying.

## [Unreleased]

### Added

<<<<<<< HEAD
- **Chat composer drag-and-drop attachments.** Dropping files onto
  the message input attaches them the same way as the paperclip
  picker and image paste (chips, then upload on send).
=======
- **Inherited global skills on the agent Skills page.** The agent
  Skills tab lists global `~/.fastclaw/skills` entries as
  **Inherited** (same merge the runtime uses). An agent-local copy
  with the same name still wins and can be removed without deleting
  the global skill.
>>>>>>> origin/cursor/agent-inherited-skills-16c5
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
