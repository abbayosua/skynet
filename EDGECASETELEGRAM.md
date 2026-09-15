# EDGECASETELEGRAM.md

Analysis, implementation notes, and test guide for the Telegram bot that
mirrors a Skynet session in both directions.

---

## 1. Architecture

| Layer | File | Responsibility |
|---|---|---|
| Client | `internal/telegram/bot.go` | Telegram Bot API client: long-poll, send, render, scrub, callbacks |
| Wiring | `internal/app/app.go` | Per-session bot lifecycle + mirror goroutines |
| Notify | `internal/agent/notify/notify.go` | Domain events (activity, responded, error, tool error, re-auth) |
| Agent | `internal/agent/agent.go` | Publishes activity / response / error / tool error notifications |
| TUI | `internal/ui/model/ui.go` | Telegram→TUI commands, `/approve`/`/deny`, inline callback handling |
| Interface | `internal/workspace/workspace*.go` | `TelegramBot*` methods (App vs Client mode) |
| CLI | `internal/cmd/root.go` | `--telegram <token>` flag or `SKYNET_TELEGRAM_TOKEN` env (in-memory only) |

### Data flow

```
Telegram ─getUpdates─▶ Bot.Start ─┬─ Incoming() ─▶ forwardTelegramToTUI
                                  └─ Callbacks() ─▶ forwardTelegramToTUI
                                          └─▶ tea.Msg{IncomingMessage/IncomingCallback}
                                              (sendMessage / /new / /summarize / /approve / /deny)

Skynet ─ messages pubsub ─▶ mirrorMessagesToTelegram ─┐
       ─ agent notifications ─▶ mirrorActivityToTelegram ─┤─▶ Bot.SendMessage (HTML)
                                                          ─┘  + SendChatAction(typing)
```

### Bot lifecycle (per process, session-bound)

- One bot per process. It **follows the active session**: `telegramSession.target()`.
- `StartTelegramBot(sessionID, token)` stops any existing bot and starts a new one.
- `RetargetTelegramBot(sessionID)` re-points the running bot when the user switches sessions.
- Token is **in-memory only** — never persisted to disk.
- `SKYNET_TELEGRAM_TOKEN` env is checked as fallback if `--telegram` flag is empty.

---

## 2. Implemented behavior

### Outbound (Skynet → Telegram)

- **Final answer**: `mirrorMessagesToTelegram` forwards finished assistant messages (`AllText()` = all text parts joined with `\n\n`).
- **Activity**: `sendChatAction("typing")` while agent is working (replaces activity message spam).
- **Final response fallback**: `notify.TypeAgentResponded` (reliable path).
- **Errors**: `notify.TypeAgentError` → `⚠️ <error>`.
- **Tool errors**: `notify.TypeToolError` → `❌ <tool>: <err>`.
- **Re-auth**: `notify.TypeReAuthenticate` → `🔐 Re-authentication required (provider: …)\nRun skynet auth <provider>`.
- **Cancel mark**: Canceled turns append `_(dibatalkan)_`.
- **De-dup**: `setLast(msgID, text)` suppresses same message ID; 5s text window dedup for cross-path.
- **Summary filter**: `IsSummaryMessage` messages are skipped.
- **Reply context**: All replies include `reply_parameters.message_id` referencing the last incoming Telegram message.

### Formatting

`markdownToTelegramHTML` renders to Telegram HTML:
headings, bold `**`, underline `__`, italic `*`, inline code, ``` code fences, links, strikethrough, bullets, horizontal rules, wide tables (≥4 cols) → `<pre>` code block, narrow tables → "A — B — C".
All text HTML-escaped first. ANSI stripped. HTML fallback on rejection.

### Split logic

`splitMessage` breaks at newlines. Fence-aware: if a ``` block is split, the opening fence is re-emitted at the start of the next chunk.

### Inbound (Telegram → Skynet)

| Command | Effect |
|---|---|
| `/start` | Reply with session title + "Connected!" |
| any text | `sendMessage(text)` into the session |
| `/new` | create a new session |
| `/summarize` | summarize the current session |
| `/ralph` | toggle Ralph loop |
| `/approve`, `/deny` | answer the pending permission request (text commands) |
| inline button "✅ Approve" / "❌ Deny" | answer via callback query |

On any inbound message the session switches to skip-permissions mode.

### Permission with inline buttons

When a permission request arrives, it is mirrored to Telegram with an
inline keyboard containing ✅ Approve and ❌ Deny buttons. Pressing a
button triggers an `IncomingCallback` → `tea.Msg` that grants or denies
the permission. The same works via `/approve` or `/deny` text commands.

### Security

- **Token scrub**: `Bot.scrub()` redacts the token from all errors.
- **Token never persisted**.
- **Single chat authorized** (first chat wins).
- **`retry_after`**: honoured (cap 1 min).
- **409 conflict**: detected in `getUpdates`; backs off 30s with clear warning.
- **Per-item parse**: malformed updates in a batch are skipped individually.

---

## 3. Testing

### Unit tests (no network)

```bash
go build ./...
go test ./internal/telegram/... ./internal/app/... ./internal/message/...
go test -race ./internal/telegram/...
```

Key test files:
- `internal/telegram/bot_test.go` — split, token, send, channel
- `internal/telegram/edge_test.go` — all edge cases (30+ tests)
- `internal/app/full_telegram_text_test.go` — dedup, session registry, text formatting
- `internal/message/alltext_test.go` — AllText multi-part join

### Live tests (real Telegram API, gated by env)

```bash
export SKYNET_TELEGRAM_TEST_TOKEN="<bot-token>"
go test ./internal/telegram/ -run 'TestLive' -v -count=1
```

- `TestLiveInvalidToken` — bogus token rejected.
- `TestLiveSendBadChat` — API error surfaced, **token not leaked**.
- `TestLiveTelegram` — connects, waits ~70s for an incoming message and echoes.

### Manual (TUI)

```bash
./skynet --telegram <bot-token>
# or: SKYNET_TELEGRAM_TOKEN=<token> ./skynet
# or: command palette → Connect Telegram → paste token
```

---

## 4. Edge cases — status

Legend: ✅ covered by automated test · ⚠️ code-only (no automated test) · 🔧 fixed.

### 4.1 Inbound / transport (30)

|#|Edge case|Status|
|---|---|---|
|1|Empty/whitespace message → no-op|✅|
|2|Long message split + reassembled|✅|
|3|Unicode/multibyte split boundary|✅|
|4|No chat yet → buffered, flushed on first chat|✅|
|5|Send failure → retry then error|✅|
|6|HTML rejected → plain fallback|✅|
|7|HTML/`<script>` injection escaped|✅|
|8|Nested bold + inline code|✅|
|9|Unbalanced code fence auto-closes|✅|
|10|Markdown inside fence untouched|✅|
|11|`snake_case` not italicized|✅|
|12|Link with query `&` escaped|✅|
|13|Horizontal rule|✅|
|14|Table separator skipped / row flattened|✅|
|15|Invalid token rejected|✅|
|16|Malformed/empty getMe JSON|✅|
|17|getMe nil result → empty username|✅|
|18|getUpdates error no token leak|✅|
|19|API error sanitized ("chat not found")|✅|
|20|Double Stop idempotent|✅|
|21|Double Start ignored|✅|
|22|Incoming channel full (buffer 1000) → drop|✅|
|23|**Token leak via `url.Error`**|✅|
|24|`/start` greeting|✅|
|25|Non-`/` commands filtered|✅|
|26|Chat mismatch ignored|✅|
|27|Cancel during long-poll → clean shutdown|✅|
|28|Data races|✅|
|29|Token never persisted|✅|
|30|Multi-session / multi-process no clash|🔧 one bot/process, token keyed|

### 4.2 Return path (Skynet → Telegram)

|#|Edge case|Status|
|---|---|---|
|1|Tool call detail ("Running: X")|✅ typing action sent|
|2|Tool output (stdout) — `/verbose`|✅|
|3|Tool error sent (`❌ <tool>: <err>`)|✅|
|4|Reasoning — `/thinking`|✅|
|5|Multi text-part joined with `\n\n`|✅|
|6|Live streaming — `/stream` (throttled edits)|✅|
|7|De-dup by message ID|✅|
|8|Fast identical regenerate de-duped|✅|
|9|Activity coalesce (typing only)|✅|
|10|Single status message (typing replaces spam)|✅|
|11|Permission notified to TG with inline buttons|✅|
|12|`/approve` includes tool detail|✅|
|13|Provider error → friendly + emoji|✅|
|14|Re-auth includes `skynet auth <provider>`|✅|
|15|429 fails after retry → logged|✅ retry_after honoured|
|16|Send fail logged|✅|
|17|Answer before first chat → buffered|✅|
|18|Compaction summary mirrored|✅ filtered|
|19|Ralph loop → spam|✅ de-duped|
|20|Cancel still sends partial text|✅ "(dibatalkan)" appended|
|21|Sub-agent sessions — `/subagents`|✅|
|22|Session switch mid-stream → tagged `[session]`|✅|
|23|Many sessions, only target mirrored|✅|
|24|Race on retarget during send|✅ sendMu/race test|
|25|Permission before TG connect|✅ pending flushed on connect|
|26|Code fence split across parts|✅ fence-aware split|
|27|Budget 3500 vs 4096 HTML limit|✅ maxMsgLen=3500|
|28|Wide table → code block|✅|
|29|Emoji/grapheme-aware split|✅ uniseg|
|30|Reply context|✅ reply_to_message_id|
|31|Persistent "busy" indicator|✅ sendChatAction: typing|
|32|Inline approve buttons|✅ callback query handler|
|33|`/cancel` from TG|✅|
|34|`/sessions`, `/switch <n>`|✅|
|35|"First chat wins" → optional pairing code|🔧 SetRequirePairing|
|36|Token from env `SKYNET_TELEGRAM_TOKEN`|✅|
|37|409 conflict detection|✅|
|38|Message >4096 after HTML escape|✅ split handles it|
|39|Images sent via `sendPhoto`|✅|
|40|Todo/progress mirrored|✅|
|41|ANSI/control chars stripped|✅|
|42|Bold `__x__` supported|✅ `<u>`|
|43|Nested-list indent + ordered lists preserved|✅|
|44|Relative links rendered as plain text|✅|
|45|Double-escape property test|✅|
|46|Per-item update parse|✅|
|47|Offset persisted (`tg_offset_<hash>`)|✅|
|48|Incoming buffer raised to 1000|🔧|
|49|409 conflict → backoff 30s|✅|
|50|Graceful drain on shutdown|✅|

---

## 5. Known residual risks

- Token via `--telegram` visible in `ps`/shell history.
- `SKYNET_TELEGRAM_TOKEN` env is a more stealthy alternative.
- "First chat wins" by default; enable pairing (`Bot.SetRequirePairing(true)`)
  to require the derived code before a chat is authorized.
- Streaming edits are throttled to one status message per 2s; very fast
  models may still coalesce several tokens per edit.

---

## 6. Telegram commands (inbound)

| Command | Effect |
|---|---|
| `/start` | Greeting with current session title |
| `/new` | Create + switch to a new session |
| `/summarize` | Summarize the current session |
| `/ralph` | Toggle the Ralph loop |
| `/cancel` | Cancel the running agent turn |
| `/sessions` | List the 10 most recent sessions |
| `/switch <n>` | Switch to session `n` from `/sessions` |
| `/approve`, `/deny` | Answer a pending permission request |
| `/verbose [on\|off]` | Mirror tool stdout |
| `/thinking [on\|off]` | Mirror model reasoning |
| `/stream [on\|off]` | Live-edit a streaming status message |
| `/subagents [on\|off]` | Mirror child (sub-agent) sessions |

## 7. Multi-agent / multi-process model (#30)

- One process runs at most one Telegram bot; it follows the active session
  via `RetargetTelegramBot`, so switching sessions never opens a second
  long-poll (which would trigger a 409).
- The long-poll offset is persisted per token: `tg_offset_<sha256(token)>`
  under the data directory. Two processes sharing **one token and one data
  directory** would fight over the offset file and Telegram's 409 limit —
  so different agents (e.g. five in one folder) MUST use distinct bot
  tokens. The token is bound to the session and never written to config.
- Outbound sends are serialised (`sendMu`) so a retarget mid-send cannot
  interleave two sessions' messages.
