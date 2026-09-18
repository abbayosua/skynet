# Skynet Session Recap — 2026-08-26

> Ringkasan sesi panjang (serena + codebase-memory + sleep tool + network bug + omni PostToolUse + autopilot + cache hit + opencode payload + job bug) supaya AI agent lain bisa lanjut tanpa baca ulang chat.

---

## 1. Project & Infra

- **Repo**: `github.com/abbayosua/skynet`, `module github.com/abbayosua/skynet`, Go `1.26.3`, `CGO_ENABLED=0`, `GOEXPERIMENT=greenteagc` **removed for local binary** (macOS SIGKILL issue)
- **Branch**: `main`, HEAD `06b98d66` (+ dirty: job fix uncommitted), tags `v0.1.12` latest pushed, `v0.1.11`, `v0.1.9`
- **Binary lokal**:
  - `/Users/user/go/bin/skynet` (PATH) `101M 2026-08-26` built `CGO_ENABLED=0 go build -o /tmp/skynet_* .` + `rm` old + `cp` + `codesign -s - -f` (need rm first or macOS AMFI kills new exec with `Killed: 9`)
  - `/Users/user/Documents/skynet/skynet` same
  - **DO NOT build with `GOEXPERIMENT=greenteagc`** — causes `Killed: 9` on `--yolo` TUI (verified: green=kill, no-green=exit 143 graceful)
- **Config locations**:
  - `~/.config/skynet/skynet.json` (GlobalConfig) — providers, models, `hooks.PreToolUse/PostToolUse`
  - `~/.local/share/skynet/skynet.json` (GlobalConfigData) — also read, has `providers.opencode-go` with 14 models incl. added ones, `hooks` duplicated (should move to GlobalConfig)
  - `crush.json` project-level minimal LSP only
- **Release CI**: `.github/workflows/release.yml` on `push tags v*` — builds `linux/darwin/windows` + `.deb`, no GOEXPERIMENT. `v0.1.12` published 2026-08-26.

---

## 2. Serena + Codebase-Memory Activation

- **Serena**:
  - `.serena/project.yml` `project_name: skynet`, `languages: [go]`, `ignore_all_files_in_gitignore: true`
  - `gopls` was missing → `go install golang.org/x/tools/gopls@latest` → `~/go/bin/gopls 39M v0.23.0`
  - `serena project health-check /Users/user/Documents/skynet` → `29 tools`, `LSP init 0.235s`, `412 files` PASS
  - MCP wired: `~/.config/opencode/opencode.json` + `~/.config/opencode/opencode.jsonc` merged to contain `codebase-memory-mcp` + `browsermcp` + `serena` (`serena start-mcp-server --context ide-assistant --project-from-cwd`) + `plugin caveman`
- **Codebase-Memory**:
  - Project `Users-user-Documents-skynet` `6546 nodes 31457 edges` (moderate), `Go 415 files`, hotspot `internal/event.Errorf`
  - Re-index `moderate` done, `get_architecture overview` verified

---

## 3. Sleep Tool (non-blocking alternative to `bash sleep 30`)

- **Files**: `internal/agent/tools/sleep.go` (`SleepToolName="sleep"`, `SleepParams{Duration string, Seconds float64}`, `SleepResponseMetadata`, `parseSleepDuration`, max 300s, `ReportActivity` + `time.After` + `ctx.Done()`), `sleep.md` doc, `sleep_test.go` 5 suites
- **Register**: `internal/config/config.go:745 allToolNames() + "sleep"`, `internal/agent/coordinator.go:758 NewSleepTool()`
- **Test**: `go test ./internal/agent/tools -run TestSleepTool -v` PASS 1.7s, fix `load_test.go` expected tool list (added `sleep`, `planner`, `skynet_info` etc.)
- **Usage**: `sleep {"duration":"30s"}` or `{"seconds":30}` instead of `bash {"command":"sleep 30"}`

---

## 4. Network Disconnect Bug (context 0% + need multiple retries)

### Diagnosis (logic only, `agent.go`/`coordinator.go`/`session`)
- **0% bug**: `session.PromptTokens+CompletionTokens / ContextWindow` → if `updateSessionUsage` overwrites with zero usage on provider error → sum 0 → 0%. Also `Summarize` resets to small. + UI stale cache.
- **No response after reconnect**: `shouldAutoRetry` only handled `endpoint is unavailable` + `net.Error`, missed `no such host`, `dial tcp`, `broken pipe`, `i/o timeout` etc. → no auto-retry. + duplicate `createUserMessage` per retry → history polluted.

### Fix (`06b98d66` ancestor: `8218e8d9`, `9866cecc`)
- `internal/agent/agent.go:1295-1306` guard `if usage zero && overrideCost nil/0 {return}` + `911-917` guard summarize same
- `internal/agent/agent.go:295-333` deduplicate + cleanup failed turn (delete last `User+Assistant(Error)` + orphan duplicate) before re-creating user message
- `internal/agent/coordinator.go:286-303` add `cleanupFailedTurn` before retry + expand `shouldAutoRetry` + `isTransientNetworkMessage` 23 substrings (`no such host`, `dial tcp`, `connection reset`, `broken pipe`, `i/o timeout`, `timeout`, `tls handshake timeout`, `eof`, etc.)
- `internal/agent/coordinator.go:321-401` add helper, handle `ProviderError.Message` too
- **Test**: `internal/agent/retry_test.go` 26 cases `TestShouldAutoRetry_NetworkErrors` + `TestIsTransientNetworkMessage` + `TestUpdateSessionUsage_GuardZero` PASS, `go test ./internal/config -run TestConfig_setupAgents` PASS, `go build ./...` ok
- **Release**: included in `v0.1.12` via later commits.

---

## 5. Hooks — PostToolUse for Omni (Full support)

### Before
- Only `EventPreToolUse` (`hooks.go:14`), `hooked_tool.go` only PreRunner. Omni needs Pre+Post to distill output (97.2% on second read, `where-omni-sits.svg`). Skynet was `MCP-only` tier.

### After (`87046934`)
- `internal/hooks/hooks.go`: `EventPostToolUse`, `HookResult.UpdatedOutput`, `AggregateResult.UpdatedOutput`, `HookMetadata.OutputRewrite`, `aggregate` last-writer-wins `UpdatedOutput`
- `internal/hooks/input.go`: `PostPayload` (aliases `hookEventName`/`sessionId` + snake), `BuildPostPayload` (tool_response with `content/stdout/file.content`, supports `view`→Read), `parseStdout` + `parseClaudeCodeOutput` handle `hookSpecificOutput.updatedToolOutput` omni shape (`stdout`/`content`/`file.content`/`result`/`mcp` + `additionalContext`), `extractUpdatedOutput` helper
- `internal/hooks/runner.go:142` `RunPost(sessionID, toolName, toolInputJSON, toolOutput, isError)` with `BuildPostPayload`, env `SKYNET_TOOL_OUTPUT`, `OutputRewrite` in `HookInfo`, `hooksEventPostToolUseAlias` (later removed for constant)
- `internal/agent/hooked_tool.go:16` `preRunner/postRunner` dual, `wrapToolsWithHooks(pre,post)`, `Run` order: Pre deny/halt → `inner.Run` → Post rewrite `resp.Content` + `Context` + merged `HookMetadata`
- `internal/agent/coordinator.go:785` `postHookRunner` from `Hooks[PostToolUse]`, `895` wrap both
- **Tests**: `hooked_tool_test.go` updated for `newHookedTool(inner, pre, post)` + `wrapToolsWithHooks` nil checks + `TestHookedTool_PostRewritesOutput` + `TestHookedTool_PostOmniShape` PASS; `hooks_test.go` `TestBuildEnv` fix `AGENT=skynet` PASS; `go vet ./internal/hooks ./internal/agent` ok
- **Config**: `hooks: {PreToolUse: [{matcher:"^bash$", command:"/opt/homebrew/bin/omni --pre-hook"}], PostToolUse: [{matcher:"^(bash|view|grep)$", command:"/opt/homebrew/bin/omni --post-hook"}]}` in `~/.local/share/skynet/skynet.json:24-38` (also need in `~/.config/skynet/skynet.json` for standard)
- **Omni**: `/opt/homebrew/bin/omni -> ../Cellar/omni/0.7.7` `omni doctor` shows `MCP-only` for skynet (not in allowlist `hermes/openclaw` in `normalize.rs:282`) but distillation works via `ClaudeCode` shape; `omni stats` counts.
- **Status**: binary `2026-08-26` already has it; to get `Full`, skynet's normalize allowlist would need `skynet`, but not blocking.

---

## 6. AutoPilot

- `internal/cmd/autopilot.go:17` `skynet autopilot [session-id] [--session/-s] [--continue/-C]` mutually exclusive
- `coordinator.go:94 RunAutoPilot(ctx, output, mainSessionID)` — creates `AutoPilot` session + separate agent (`isSubAgent=true`, `buildAgent(..., true)`), reads 10 last msgs for context preview, logs `.skynet/autopilot/logs/<id>.log`, loop until `<autopilot>DONE</autopilot>`/`BLOCKED`, rotate after length, watchdog, `Ctrl+C` stop
- **Hooks**: NOT fired in inner loop (`isSubAgent=true` → `wrapToolsWithHooks` skip), only outer `agent`/`spawn_agent` call is hooked. MCP: YES (opencode `AllowedMCP=nil` for coder).
- **TODO**: if want hooks in autopilot inner loop, need `include_sub_agents` per-hook (FUTURE.md).

---

## 7. Cache Hit DeepSeek (7.14M miss / 357M hit)

### Problem
- DeepSeek prefix cache = exact match from token 0, block ≥1024. Skynet rebuilt `systemPrompt` every `Run` with `Date` (daily change, `prompt.go:210`) + `GitStatus` (branch+status+commits, `prompt.go:233`) inside `<env>` of `coder.md.tpl` → prefix broke almost every turn, especially since agent edits files → git status changes.

### Fix (`9866cecc`)
- `internal/agent/agent.go:61 isDeepSeekCacheModel(m Model)` checks `deepseek|mimo` in provider/model, `557 StopWhen` threshold for cache models `0.1` not `0.2` (115k not 102k on 128k window, keeps 90% prefix, 50x cheaper)
- `internal/agent/prompt/prompt.go:202` for cache models: `Date="1/1/2006"` fixed + `GitStatus` only `branch` not status/commits
- `isDeepSeekCachePrompt(provider, model)` helper mirrored
- **Proof**: live 5-turn test `deepseek-v4-flash` via `opencode-go` with stable 8K-char system (~1831 prompt tokens): turn1 `cached 0`, turn2-10 `cached 5376` stable each turn, `overall_hit 96.8%` (55K prompt, 53K cached). With mutated system (date/branch/status per turn) same test: **all turns `cached 0, hit 0.0%`** — validates.
- **Direct DeepSeek** `api.deepseek.com` key `sk-ad19...` invalid (truncated); opencode path works, direct not tested.

### Usage
- Both `deepseek-ecosystem: deepseek-...` and `opencode-go:deepseek/mimo` hit due to string check.
- Needs same session (`skynet run --continue` or TUI long session), don't change skills/MCP mid-session, idle TTL hours before evict.
- **Old sessions continued** on new binary: first turn miss once (system prompt changed), then hit.

---

## 8. Binary Update Issue

- `zsh: killed` after `cp` greenteagc binary. Root cause: `GOEXPERIMENT=greenteagc` (Taskfile `env: GOEXPERIMENT: greenteagc`) binary SIGKILL on macOS TUI.
- Fix: rebuild `CGO_ENABLED=0 go build -o /tmp/skynet_* .` **without** GOEXPERIMENT, then `rm` old paths + `cp` fresh + `codesign -s - -f`.

---

## 9. Opencode Payload Investigation

- Cloned `github.com/anomalyco/opencode` to `/tmp/opencode`, fresh 2026-08-26.
- Findings vs new official endpoint table:

| Model | Endpoint | AI SDK | Skynet handling |
|---|---|---|---|
| grok-4.6, gpt-5.6-luna, muse-spark-1.2-contributor | `/zen/go/v1/responses` `@ai-sdk/openai` | Was chat→500 for muse family, now fixed | `opencodeNeedsResponsesAPI` added (contains `muse` + `grok-4.`|`gpt-5.6` prefix) |
| glm-5.x, kimi-k2.6/k3, longcat-2.0, deepseek-v4*, mimo, hy3, ox-alpha-free | `/chat/completions` `@ai-sdk/openai-compatible` | OK | unchanged |
| minimax m3/m2.7/m2.5, qwen3.8/3.7/3.6 | `/messages` `@ai-sdk/anthropic` | Skynet would have used chat, but test shows `chat` also returns 200 with Bearer (output has raw `<think>`). Official says `/messages`. Skynet no anthropic routing yet — works via chat, minor. |

- Opencode special handling in `transform.ts:556 topP 0.95` for `deepseek-v4-flash`, `1317 promptCacheKey = sessionID` for *all* `opencode*`, `1317 include encrypted reasoning`, `1197 chat_template_args enable_thinking` for kimi, `system.ts:28` muse name. Skynet mimics promptCacheKey via `getProviderOptions` injection.
- **Empirical matrix** (keys from `~/.local/share/skynet/skynet.json`):
  - `zen/x-preview-f-free` plain+reasoning+tools+stream = all 200
  - `go/muse-spark-1.2-contributor` via chat = 500, via responses = 200 (key finding)
  - `go/mimo-v2.5`, `go/deepseek-v4-flash` = 200 both; `zen/deepseek-v4-flash` 401 insufficient balance (credits)
  - `prompt_cache_key` snake works on responses, camel fails 400 → use snake.
  - Headers: no TTL header from gateway (only `CF-RAY`), detect via `usage.prompt_tokens_details.cached_tokens`.
- **Fix implemented** (`9866cecc`):
  - `coordinator.go:1086-1106` opencode branch `WithUseResponsesAPI` + `WithResponsesAPIFunc(opencodeNeedsResponsesAPI)`, helper added `1129`, `buildAzureProvider` fix (missing `func` line)
  - `getProviderOptions/mergeCallOptions` now take `sessionID`, inject `extra_body.prompt_cache_key = sessionID` for opencode*
  - Added tests `TestGetProviderOptionsOpencodeCacheKey` + `TestOpencodeNeedsResponsesAPI` PASS, `coordinator_test.go:416` updated
  - Live e2e `skynet run -q -m opencode-go/muse-spark-1.2-contributor "MUSE OK"` → `MUSE OK`, ox-alpha/mimo/dsv4 also OK
  - Grok 4.6 missing from catwalk → added 14 models to `~/.local/share/skynet/skynet.json` `providers.opencode-go.models`
  - Updated `opencodeNeedsResponsesAPI` again `06b98d66` to include `grok-4.` + `gpt-5.6` after table, live grok → `GROK OK`

---

## 10. Job Bug — Stuck on `job_output` / `job_kill`

### Summary from prior session
- Sesi lain: "stuck bukan job_kill — provider 500 retry 27x orphaned tool call, SSE OpenAI path no try/catch"

### Analysis (code `background.go`/`job_output.go`/`coordinator.go`)
- **Root 1** `job_output.go:48 WaitContext(ctx)` unbounded → dev server never exits = infinite block (exit never)
- **Root 2** `background.go:153 Kill <-done` unbounded → child holding pipe FD = infinite hang
- **Root 3** retry storm 30× backoff diam → TUI spinner looks stuck
- `Cleanup()` only removes completed >8h — not culprit.

### Fix (current dirty, not yet pushed)
- `internal/agent/tools/job_output.go:20-23` `Timeout int` param (default 30s, max 300s min 1s, clamp)
- `job_output.go:49-74` `context.WithTimeout` wrap, `ReportActivity("Waiting for job %s (max %s)...")`, on timeout return current output `Status: running` so agent re-polls, on `ctx.Err()` propagate cancel
- `job_output.md` updated
- `internal/shell/background.go:146-162` `Kill` now `select {case <-done: return; case <-After(2s): abandon}`
- `internal/agent/coordinator.go:286-303` add `c.notify.Publish(TypeActivityUpdate, "Provider error, retrying %d/%d in %s...")` per retry for visibility
- **Tests** (`internal/shell/kill_grace_test.go` 2 tests, `internal/agent/tools/job_output_timeout_test.go` 3 tests):
  - `TestJobOutput_WaitTimeout_ReturnsRunning: 1.00s PASS` (sleep 5, timeout 1 → running, still alive)
  - `TestJobOutput_WaitWithoutTimeout_CompletesWhenDone PASS`, `TestJobOutput_WaitTimeout_Clamped PASS`
  - `TestBackgroundShellManager_Kill_GraceTimeout PASS`, `Kill_AbandonsHungJob 2.00s PASS` (manual hung `done` never closes → abandon after 2s)
  - `internal/shell -count=1 ok 3.5s`, `internal/agent/tools` (filtered) ok, `go vet` ok, `go build ./...` ok, binary `101M v0.1.12+dirty`
  - Note (fixed in v0.1.33): `TestReadBuiltinFile` failed because the tests still used the pre-fork `crush://skills/` prefix while `skills.BuiltinPrefix` is `skynet://skills/`, and asserted the old "Crush Configuration" title.
  - `/tmp/test_kill_grace.go` removed

---

## 11. Current Git Status (2026-08-26)

```
On branch main, ahead of origin/main by 1? Actually pushed to 06b98d66 already.
Now dirty:
 M internal/agent/coordinator.go              (job retry visibility)
 M internal/agent/tools/job_output.go
 M internal/agent/tools/job_output.md
 M internal/shell/background.go
?? .commandcode/  .serena/  RAW_SKYNET_GUIDE.md  etc. untracked
?? internal/agent/tools/job_output_timeout_test.go
?? internal/shell/kill_grace_test.go
```

Recent commits:
- `06b98d66 fix(agent): route grok-4.6 and gpt-5.6-luna` (pushed, tagged v0.1.12)
- `9866cecc feat(agent): deepseek prefix-cache...` (v0.1.11)
- `53a3f2ea fix(build): add missing commandcode transport`
- `87046934 feat(hooks): add PostToolUse`
- `8218e8d9 fix(agent): guard zero usage...`

Releases:
- `v0.1.12` https://github.com/abbayosua/skynet/releases/tag/v0.1.12 — darwin amd64/arm64, linux am64 .deb/tar.gz, windows zip, checksums, install.sh/ps1 — all assets present, workflow success.
- Next pending: job fix (4 files) → needs `06b98d66..HEAD` commit, push, tag `v0.1.13`. Also `skynet` binary at `~/go/bin/skynet` already `v0.1.12+dirty` with job fix.

---

## 12. TODOs for Next Agent

- **Commit & release job fix**: `job_output.Timeout`, `background.Kill` grace, `coordinator` retry activity. Run `go fmt`, `go vet ./internal/agent ./internal/shell`, `go test ./internal/shell -count=1 -v`, `go test ./internal/agent/tools -run TestJobOutput -count=1`, then `CGO_ENABLED=0 go build -o skynet`, `cp` + `codesign -s - -f` to `~/go/bin/skynet`, `git commit -m "fix(job): cap wait timeout and kill grace, show retry activity"`, push, tag `v0.1.13`.
- **Config hygiene**: move `hooks` from `~/.local/share/skynet/skynet.json` to `~/.config/skynet/skynet.json` (standard), keep `matcher` `^(bash|view|grep)$` or widen to `.*` if want omni on all tools.
- **Install skynet update on other machines**: `curl -fsSL https://github.com/abbayosua/skynet/releases/latest/download/install.sh | bash` or `gh release download v0.1.12`.
- **Omni `Full` tier**: to make `omni doctor` report Full for skynet, PR to `fajarhide/omni` `normalize.rs` add `skynet` to `is_plugin_host` or handle via `agent: skynet` detection — currently works via ClaudeCode shape anyway.
- **Portability of added models**: catwalk models added only to local `~/.local/share/skynet/skynet.json`; other machines won't have them until catwalk updates or you commit them to `~/.config/skynet/skynet.json` provider override and push.
- **Potential follow-up**: `/messages` routing for minimax/qwen (anthropic) if `chat` ever breaks — currently chat works (tested 200), but official table says `/messages`.

---
