# Plan: rewrite-autopilot-as-tui-integrated-goal

> Created: 2026-08-26 14:26:16
> **Status**: Draft

## Objective

Rewrite AutoPilot as TUI-integrated goal-driven service: plan→execute→report loop in internal/agent/autopilot.go, /autopilot command in TUI chat, hooks enabled (top-level agent), CLI headless fallback reuses same service

## Scope

**In Scope:**
-

**Out of Scope:**
-

## Context

Skynet Go TUI (bubbletea v2). Existing headless RunAutoPilot at internal/agent/coordinator.go:1587 uses buildAgent(isSubAgent=true) so hooks don't fire; free loop until DONE tag. TUI chat page renders sessions via pubsub messages. Coordinator has notify pubsub with TypeActivityUpdate.

## Approach

-

## Tasks

| # | Task | Files | Status |
|---|------|-------|--------|
| 1 | - | - | pending |

## Risks & Mitigations

-

## Verification

- [ ] All tasks completed
- [ ] Tests pass
- [ ] Edge cases handled
