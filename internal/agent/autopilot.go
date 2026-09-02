package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"

	"charm.land/fantasy"
	"github.com/abbayosua/skynet/internal/agent/notify"
	"github.com/abbayosua/skynet/internal/pubsub"
)

// Autopilot stop markers the model can emit in its response.
const (
	autopilotTagDone       = "<autopilot>DONE</autopilot>"
	autopilotTagBlockedPre = "<autopilot>BLOCKED"
)

// autopilotMaxSteps bounds the plan execution and fallback loop.
var autopilotDefaultMaxSteps = 10

var (
	errAutoPilotGoalRequired   = errors.New("autopilot: goal is required")
	errAutoPilotSessionMissing = errors.New("autopilot: session is required")
)

// RunAutoPilotGoal runs a goal-driven autopilot inside an existing
// session. Unlike a free-running loop it follows three explicit phases:
//
//  1. Plan    — the model proposes a numbered, verifiable plan.
//  2. Execute — each step runs in its own turn; progress is visible in
//     the chat as normal messages (hooks and MCP included, because every
//     turn is a regular top-level run).
//  3. Report  — a final turn summarizes what was done.
//
// Progress lines are written to output when non-nil, and activity
// updates are published so the TUI status line reflects the current
// phase even while another session is open.
func (c *coordinator) RunAutoPilotGoal(ctx context.Context, sessionID, goal string, maxSteps int, output io.Writer) error {
	if strings.TrimSpace(goal) == "" {
		return errAutoPilotGoalRequired
	}
	if strings.TrimSpace(sessionID) == "" {
		return errAutoPilotSessionMissing
	}
	if maxSteps <= 0 {
		maxSteps = autopilotDefaultMaxSteps
	}

	writeLine := func(format string, args ...any) {
		if output == nil {
			return
		}
		fmt.Fprintf(output, format+"\n", args...)
	}
	setActivity := func(activity string) {
		c.notify.Publish(pubsub.UpdatedEvent, notify.Notification{
			SessionID: sessionID,
			Type:      notify.TypeActivityUpdate,
			Activity:  activity,
		})
	}

	setActivity("Autopilot: planning")
	writeLine("── AutoPilot ──────────────────────────────────")
	writeLine("  🎯 Goal: %s", goal)
	writeLine("  📝 Phase 1/3: Planning...")

	result, err := c.Run(ctx, sessionID, buildPlanPrompt(goal))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("autopilot: plan phase failed: %w", err)
	}

	steps := parsePlanSteps(responseText(result), maxSteps)
	writeLine("  📋 Plan: %d step(s)", len(steps))

	if len(steps) == 0 {
		// The model did not produce a parseable plan. Fall back to a
		// bounded working loop instead of looping forever.
		return c.runAutoPilotFallback(ctx, sessionID, goal, maxSteps, writeLine, setActivity)
	}

	for i, step := range steps {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		setActivity(fmt.Sprintf("Autopilot: step %d/%d", i+1, len(steps)))
		writeLine("  ⚙️  Phase 2/3: Step %d/%d — %s", i+1, len(steps), step)

		stepResult, err := c.Run(ctx, sessionID, buildStepPrompt(i+1, len(steps), step, goal))
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			slog.Warn("Autopilot step failed", "step", i+1, "error", err)
			writeLine("  ⚠️  Step %d failed: %v", i+1, err)
			continue
		}

		response := responseText(stepResult)
		if strings.Contains(response, autopilotTagBlockedPre) {
			reason := extractBlockedReason(response)
			writeLine("  🛑 Blocked at step %d: %s", i+1, reason)
			break
		}
		writeLine("  ✅ Step %d/%d done", i+1, len(steps))
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	setActivity("Autopilot: reporting")
	writeLine("  📊 Phase 3/3: Reporting...")
	if _, err := c.Run(ctx, sessionID, buildReportPrompt(goal)); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		slog.Warn("Autopilot report phase failed", "error", err)
	}

	writeLine("  🏁 AutoPilot finished.")
	return nil
}

// runAutoPilotFallback executes the goal without a structured plan,
// bounded by autopilotMaxSteps iterations with explicit stop markers.
func (c *coordinator) runAutoPilotFallback(
	ctx context.Context,
	sessionID, goal string,
	maxSteps int,
	writeLine func(string, ...any),
	setActivity func(string),
) error {
	writeLine("  ℹ️  No structured plan detected, using direct mode.")

	prompt := fmt.Sprintf(
		"Work toward this goal: %s\n\n"+
			"Rules:\n"+
			"- Make concrete changes, verify them (tests/build), then continue.\n"+
			"- When the goal is fully achieved, end your reply with %s.\n"+
			"- If you cannot make progress, end your reply with %s: reason.\n",
		goal, autopilotTagDone, autopilotTagBlockedPre)

	for i := 0; i < maxSteps; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		setActivity(fmt.Sprintf("Autopilot: iteration %d/%d", i+1, maxSteps))
		result, err := c.Run(ctx, sessionID, prompt)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("autopilot: iteration %d failed: %w", i+1, err)
		}

		response := responseText(result)
		switch {
		case strings.Contains(response, autopilotTagDone):
			writeLine("  ✅ Goal achieved after %d iteration(s).", i+1)
			return nil
		case strings.Contains(response, autopilotTagBlockedPre):
			writeLine("  🛑 Blocked: %s", extractBlockedReason(response))
			return nil
		}

		prompt = "Continue working toward the goal. If it is fully achieved, end your reply with <autopilot>DONE</autopilot>. If blocked, end with <autopilot>BLOCKED</autopilot>: reason."
	}

	writeLine("  ⏱ Stopped after %d iterations without an explicit completion.", maxSteps)
	return nil
}

// buildPlanPrompt asks for a strictly formatted checklist so steps can
// be parsed deterministically.
func buildPlanPrompt(goal string) string {
	return fmt.Sprintf(
		"Create an execution plan for this goal: %s\n\n"+
			"Output ONLY a markdown checklist, one item per concrete step, like:\n"+
			"- [ ] Step description\n\n"+
			"Each step must be independently executable and verifiable "+
			"(e.g. ends with a test or build check). Do not start executing.",
		goal)
}

func buildStepPrompt(number, total int, step, goal string) string {
	return fmt.Sprintf(
		"Execute ONLY step %d of %d toward the goal (%q):\n%s\n\n"+
			"Do exactly this step, verify it (run relevant tests or build), "+
			"and do not move on to other steps. If you cannot complete it, "+
			"explain why and end your reply with %s: reason.",
		number, total, goal, step, autopilotTagBlockedPre)
}

func buildReportPrompt(goal string) string {
	return fmt.Sprintf(
		"The autopilot run for the goal (%q) has finished. Summarize: "+
			"what was changed, what was verified, and any remaining follow-ups.",
		goal)
}

var planStepRe = regexp.MustCompile(`^\s*(?:[-*]\s*\[[ xX]\]\s*|\d+[.)]\s+)(.+)$`)

// parsePlanSteps extracts ordered steps from a markdown checklist or
// numbered list in the model's response.
func parsePlanSteps(response string, maxSteps int) []string {
	var steps []string
	for _, line := range strings.Split(response, "\n") {
		m := planStepRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		step := strings.TrimSpace(m[1])
		if step != "" && !strings.HasPrefix(step, "<autopilot>") {
			steps = append(steps, step)
		}
	}
	if maxSteps > 0 && len(steps) > maxSteps {
		steps = steps[:maxSteps]
	}
	return steps
}

// extractBlockedReason pulls the human-readable reason out of a
// BLOCKED-tagged response.
func extractBlockedReason(response string) string {
	idx := strings.Index(response, autopilotTagBlockedPre)
	if idx < 0 {
		return "unknown reason"
	}
	reason := strings.TrimSpace(strings.TrimPrefix(response[idx:], autopilotTagBlockedPre))
	reason = strings.TrimSpace(strings.TrimPrefix(reason, "</autopilot>"))
	reason = strings.TrimSpace(strings.TrimPrefix(reason, ":"))
	if reason == "" {
		return "unknown reason"
	}
	return truncateAutoPilotMsg(reason, 300)
}

func truncateAutoPilotMsg(msg string, maxLen int) string {
	msg = strings.TrimSpace(msg)
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen] + "..."
}

func responseText(result *fantasy.AgentResult) string {
	if result == nil {
		return ""
	}
	return result.Response.Content.Text()
}
