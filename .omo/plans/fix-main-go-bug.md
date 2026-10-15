# Plan: fix-main-go-bug

> Created: 2026-08-27 11:28:18
> **Status**: Draft

## Objective

Fix bug in `main.go` (entrypoint CLI Skynet).

## Scope

**In Scope:**
- `main.go` - pprof conditional, `cmd.Execute()` wiring
- Build/test verification

**Out of Scope:**
- Refactor besar `internal/cmd`, `internal/app`

## Context

`main.go:24-34` - `func main()` conditional `SKYNET_PROFILE` → goroutine `http.ListenAndServe("localhost:6060")` + `cmd.Execute()`. No LSP diagnostics. Bug belum dispesifik → plan investigasi dulu. Repo Go, `CGO_ENABLED=0`.

## Acceptance Criteria

- Bug teridentifikasi & fix di `main.go`
- `go build .` & `go test ./...` lolos
- Tidak regresi pprof/cmd

## Approach

1. Triage via Serena (`get_symbols_overview`, `find_symbol` body, `get_diagnostics_for_file`, `search_for_pattern`) + `codebase-memory` jika ter-index.
2. Reproduksi minimal (env `SKYNET_PROFILE=1` cek port 6060, `cmd.Execute` error path).
3. Fix targeted di `main.go` (hardcode addr, goroutine leak, error handling).
4. Verify build/test + `gofumpt`.

## Tasks

| # | Task | Files | Status |
|---|------|-------|--------|
| 1 | Triage bug: diagnosa + `search_for_pattern` pprof/Execute, cek `internal/cmd` Execute | main.go, internal/cmd/*.go | pending |
| 2 | Reproduksi: run `SKYNET_PROFILE=1 go run . --help` & cek pprof listen | main.go | pending |
| 3 | Fix `main.go` (configurable addr, graceful log, handle `cmd.Execute` error) | main.go | pending |
| 4 | Verify: `go build .`, `go test ./...`, `task lint:fix`/`gofumpt -w .` | main.go | pending |

## Risks & Mitigations

- Bug tidak spesifik → mitigasi: buat 2 hipotesis utama (pprof hardcode 6060 conflict & missing error return dari `cmd.Execute`).
- Port conflict → jadikan env `SKYNET_PPROF_ADDR`.
- Silent fail `ListenAndServe` → sudah log Error, pertahankan.

## Verification

- [ ] `go vet` / diagnostics clean
- [ ] `go build .` sukses
- [ ] `SKYNET_PROFILE=1` pprof reachable & tidak block `cmd.Execute`
- [ ] Tests pass
