# Plan: telegram-edge-cases

> Created: 2026-09-15 16:41:07
> **Status**: Ready
> **Token**: `8772313627:AAF8pRIakVlDDqa4aLvJZI7gWw8Ez1ifAHI` (@picoclaw90909_bot)

## Objective

Implement all 50 return-path edge cases from EDGECASETELEGRAM.md §4.2 (plus
the 6 ⚠️ items from §4.1), with unit + live Telegram testing, grouped into
8 phases. Each phase ends with `go build`, unit tests, and at least one
live Telegram round-trip.

## Acceptance Criteria

All 56 edge cases implemented, unit-tested, live-tested (where network
required), build passes, and EDGECASETELEGRAM.md updated to ✅.

---

## Phase A — Inbound gaps (§4.1 ⚠️ items)

- [ ] **A1.** `#24 /start greeting`: Add `/start` case to `ui.go` telegram handler; reply with session title + "Connected!"; unit test `TestInboundStartGreeting`
- [ ] **A2.** `#25 Non-/ commands filtered`: Verify `strings.HasPrefix(text, "/")` filter in `bot.go` Start() actually drops unknown `/foo`; unit test `TestSlashFilter`
- [ ] **A3.** `#26 Chat mismatch ignored`: Add `TestChatMismatchIgnored` in `edge_test.go` using mock that sends two different chat IDs; verify second is dropped
- [ ] **A4.** Live test A1-A3: run `TestLiveTelegram` window, send `/start` from Telegram, verify greeting reply; send `/unknown_cmd`, verify no reply; build check `go build ./...`

## Phase B — Tool info & status display (#1–4, #10)

- [ ] **B1.** `#1 Tool call detail`: In `mirrorActivityToTelegram`, enrich "Running: X" with tool name + first 80 chars of args; update `notify.Notification.Activity`; unit test `TestToolActivityEnriched`
- [ ] **B2.** `#3 Tool error sent`: Handle `notify.TypeToolError` (new type in `notify.go`); publish from agent when tool result `IsError`; mirror as `❌ <tool>: <err>`; unit test
- [ ] **B3.** `#10 Single status message via editMessageText`: Add `EditMessage` to `bot.go`; replace activity spam with one status message that gets edited; unit test `TestEditMessage`
- [ ] **B4.** Live test B1-B3: trigger a bash tool from Telegram prompt, verify enriched "Running:" and edit behavior; build `go build ./...`

## Phase C — De-dup fix (#7–8) & summary filter (#18)

- [ ] **C1.** `#7-8 De-dup by message ID`: Change `setLast` to track message ID instead of content text; add `msgID string` param; unit test `TestDedupByID`
- [ ] **C2.** `#18 Filter IsSummaryMessage`: Add `if msg.IsSummaryMessage { continue }` in `mirrorMessagesToTelegram`; unit test
- [ ] **C3.** Live test C1-C2: trigger two turns with same answer, verify both sent; trigger compact, verify summary not sent; build `go build ./...`

## Phase D — Permission transparency (#11–12, #25)

- [ ] **D1.** `#11-12 Permission detail + /approve`: Mirror permission request to Telegram with tool name + action + path + first 120 chars of params; inline keyboard with Approve/Deny buttons; unit test `TestPermissionMirror`
- [ ] **D2.** `#25 Permission before TG connect`: When TG connects, check `m.pendingPermission`; if set, send it; unit test `TestPendingPermissionFlush`
- [ ] **D3.** Add Telegram callback query handler to `bot.go` for inline button presses; unit test `TestCallbackQuery`
- [ ] **D4.** Live test D1-D3: send a prompt that triggers permission (e.g. non-yolo `rm`), verify permission message + buttons in Telegram; tap Approve; verify tool proceeds; build

## Phase E — Error & re-auth polish (#13–14, #20)

- [ ] **E1.** `#13 Provider error → friendly`: Map known error titles in `mirrorActivityToTelegram` to human-readable messages with emoji; unit test
- [ ] **E2.** `#14 Re-auth link`: Enrich `TypeReAuthenticate` handler to include `skynet auth <provider>`; unit test
- [ ] **E3.** `#20 Cancel mark`: When `TypeAgentResponded` arrives with `FinishReasonCanceled`, append "(dibatalkan)"; unit test
- [ ] **E4.** Live test E1-E3: trigger auth error (bad key), verify friendly re-auth message; send prompt, cancel quickly, verify "(dibatalkan)"; build

## Phase F — Outbound reliability (#5, #15–17, #27, #37–38)

- [ ] **F1.** `#5 Multi text-part join`: Change `AllText()` join from concat to `\n\n`; update existing test
- [ ] **F2.** `#15-16 Outbound queue`: Replace direct `bot.SendMessage` with `bot.Enqueue`; add `sendQueue` buffered channel in `Bot`; worker goroutine retries with backoff, marks failed; unit test `TestEnqueueRetry`
- [ ] **F3.** `#17 Buffer before chat`: In `Bot.SendMessage`, if `chatID==0`, queue message; deliver when first chat arrives; unit test `TestBufferBeforeChat`
- [ ] **F4.** `#38 Split by final HTML length`: Change `splitMessage` to operate on rendered HTML output (not source); unit test `TestSplitByHTMLLength`
- [ ] **F5.** `#27 Code-fence-aware split`: Track fence open/close state across split boundaries; reopen ``` in next part if left open; unit test `TestFenceAwareSplit`
- [ ] **F6.** `#37 Token-bucket rate limiter`: Add simple token bucket to outbound send path; cap at 20 msg/sec per bot; unit test
- [ ] **F7.** Live test F1-F6: send a very long answer (>3500 chars with code fence) from skynet, verify correctly split; verify rate limiting works; build

## Phase G — Formatting improvements (#28–30, #41–45)

- [ ] **G1.** `#28 Wide table → code block`: Detect wide pipe tables (≥4 columns) and render in `<pre>` instead of flat; unit test
- [ ] **G2.** `#29 Grapheme-aware length`: Use `golang.org/x/text/unicode/norm` or rune counting for split boundaries; unit test `TestGraphemeLength`
- [ ] **G3.** `#41 Strip ANSI`: Add `stripANSI` helper, apply before `markdownToTelegramHTML`; unit test `TestStripANSI`
- [ ] **G4.** `#42 Bold __x__`: Add `__` regex to `convertInline`; unit test
- [ ] **G5.** `#43 Preserve list indent`: Detect `^\s+[-*+]` prefix, preserve whitespace in `convertInline`; unit test
- [ ] **G6.** `#45 Double-escape property test`: Add fuzz/property test: `markdownToTelegramHTML(plain)` == `plain` when no markdown; unit test
- [ ] **G7.** Live test G1-G6: send a prompt that returns a wide table + ANSI + bold, verify formatted correctly in Telegram; build

## Phase H — Inbound commands & UX (#31–36, #46–50)

- [ ] **H1.** `#31 sendChatAction typing`: Add `SendChatAction` to `bot.go`; call in activity mirror goroutine; unit test `TestSendChatAction`
- [ ] **H2.** `#33 /cancel from TG`: Add `/cancel` handler in `ui.go` → `AgentCancel(sessionID)`; unit test
- [ ] **H3.** `#34 /sessions + /switch`: Add `/sessions` handler (list last 10 sessions, reply as numbered list) and `/switch <n>` (load session); unit test
- [ ] **H4.** `#46 Per-item update parse`: In `getUpdates`, parse each update individually; skip malformed ones with warning; unit test `TestGetUpdatesPerItem`
- [ ] **H5.** `#47 Offset persist`: Save `lastOffset` to `<data-dir>/tg_offset_<token-hash>`; load on start; unit test `TestOffsetPersist`
- [ ] **H6.** `#48 Larger incoming buffer`: Increase `incoming` channel from 100 to 1000; add overflow log; unit test
- [ ] **H7.** `#49 409 conflict detection`: Detect 409 from `getUpdates`/`sendMessage`; log clear message and back off 30s; unit test `Test409Conflict`
- [ ] **H8.** `#50 Graceful drain`: Add `Bot.Shutdown(timeout)` that drains pending sends; unit test `TestGracefulDrain`
- [ ] **H9.** `#35 Pairing code`: When bot starts, generate a 6-char code from token; require first inbound message to contain that code; unit test `TestPairingCode`
- [ ] **H10.** `#36 Token from env`: If `--telegram` empty, check `SKYNET_TELEGRAM_TOKEN` env; if empty, check stdin; unit test
- [ ] **H11.** Live test H1-H10: connect bot, send messages from two different accounts, verify only paired one accepted; try /cancel, /sessions, /switch; verify typing indicator; build

## Phase I — Remaining low-priority (#6, #9, #21–24, #32, #39–40)

- [ ] **I1.** `#6 Real-time streaming throttle`: Add `editMessageText` support; during OnTextDelta, batch edits every 2s; unit test `TestStreamingThrottle`
- [ ] **I2.** `#9 Activity coalesce queue`: Replace rate-limit with coalesce queue; flush last known state every 3s; unit test
- [ ] **I3.** `#30 reply_to_message_id`: Track last user message ID from Telegram; set `reply_to_message_id` on bot replies; unit test
- [ ] **I4.** `#48 (overflow log)`: Add structured log when incoming channel drops messages; unit test (already covered by H6)
- [ ] **I5.** Live test I1-I3: send prompt, verify streaming edits appear in Telegram; verify reply context; build

## Phase J — Final

- [ ] **J1.** Run full suite: `go build ./... && go test ./internal/telegram/... ./internal/app/... ./internal/message/... ./internal/ui/model/... -race`
- [ ] **J2.** Run live test window (90s) with `SKYNET_TELEGRAM_TEST_TOKEN`, send varied messages, verify all features
- [ ] **J3.** Update `EDGECASETELEGRAM.md`: mark all 56 items ✅, update §5 residual risks, add final test evidence
- [ ] **J4.** Regenerate schema: `go run main.go schema > schema.json`
- [ ] **J5.** `gofumpt -w` on all changed files
