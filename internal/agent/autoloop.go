package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// AutoLoopOptions configures a RunAutoLoop invocation.
type AutoLoopOptions struct {
	// Branch is the git branch autoloop commits to. Empty = "autoloop/<ts>".
	Branch string
	// Push pushes the branch to origin when the run finishes.
	Push bool
	// NoReview disables the reviewer turn (builder self-approves).
	NoReview bool
	// MaxIterations bounds the loop; 0 = unlimited.
	MaxIterations int
	// Budget bounds the wall-clock time; 0 = unlimited.
	Budget time.Duration
	// Mode is "normal" or "hunt". In hunt mode the reviewer's DONE
	// signal is ignored so the loop keeps hunting for new issues until
	// the budget, iteration cap, or Ctrl+C stops it. Runs that produce
	// no file changes for several iterations in a row still stop.
	Mode string
	// AutoApprove reports whether permissions are auto-approved. The CLI
	// sets permissions on the session before calling RunAutoLoop.
	AutoApprove bool
}

// AutoLoop memory directory, relative to the working directory.
const autoLoopDir = ".skynet/autoloop"

// RunAutoLoop runs a mission-driven autonomous loop: each iteration the
// builder picks one valuable task, implements and verifies it, then the
// reviewer approves or rejects before a commit lands on a dedicated
// branch. Progress is persisted in .skynet/autoloop/*.md so the loop
// survives restarts and the user can inspect it.
func (c *coordinator) RunAutoLoop(ctx context.Context, sessionID, mission string, opts AutoLoopOptions, output io.Writer) error {
	if strings.TrimSpace(mission) == "" {
		return fmt.Errorf("autoloop: mission is required")
	}
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("autoloop: session is required")
	}

	writeLine := func(format string, args ...any) {
		if output == nil {
			return
		}
		fmt.Fprintf(output, format+"\n", args...)
	}

	if err := c.initAutoLoopDir(mission); err != nil {
		return fmt.Errorf("autoloop: init memory dir: %w", err)
	}

	branch := opts.Branch
	if branch == "" {
		branch = "autoloop/" + time.Now().Format("20060102-150405")
	}
	if err := autoLoopGit("checkout", "-b", branch); err != nil {
		// Branch may already exist; switch to it instead of creating.
		if err2 := autoLoopGit("checkout", branch); err2 != nil {
			return fmt.Errorf("autoloop: cannot checkout branch %q: %w", branch, err2)
		}
	}

	deadline := time.Time{}
	if opts.Budget > 0 {
		deadline = time.Now().Add(opts.Budget)
	}

	writeLine("── AutoLoop ──────────────────────────────────")
	writeLine("🎯 Mission: %s", mission)
	writeLine("🌿 Branch: %s", branch)
	if opts.Budget > 0 {
		writeLine("⏱  Budget: %s", opts.Budget)
	} else {
		writeLine("⏱  Budget: unlimited")
	}
	if opts.MaxIterations > 0 {
		writeLine("🔁 Max iterations: %d", opts.MaxIterations)
	}

	iter := 0
	idleStreak := 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if opts.MaxIterations > 0 && iter >= opts.MaxIterations {
			writeLine("⏱  Reached max iterations (%d).", opts.MaxIterations)
			break
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			writeLine("⏱  Budget exhausted after %d iteration(s).", iter)
			break
		}
		iter++
		writeLine("\n[iter %d] 🔨 Builder working...", iter)

		builderPrompt := buildAutoLoopBuilderPrompt(mission, iter, branch)
		builderResult, err := c.Run(ctx, sessionID, builderPrompt)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			slog.Warn("AutoLoop builder failed", "iteration", iter, "error", err)
			writeLine("⚠️  Builder error: %v", err)
			c.appendAutoLoopLog(iter, "builder error: "+err.Error())
			continue
		}
		writeLine("\n── Builder result ──")
		writeLine("%s", indentPreview(responseText(builderResult), 2000))
		if status, err := exec.Command("git", "status", "--short").Output(); err == nil {
			if len(status) == 0 {
				idleStreak++
				writeLine("ℹ️  No file changes this iteration (idle streak %d).", idleStreak)
				if idleStreak >= 3 {
					writeLine("🛑 3 consecutive iterations with no changes; stopping.")
					c.autoLoopPush(branch, opts, writeLine)
					return nil
				}
			} else {
				idleStreak = 0
				writeLine("\n── Changed files ──\n%s", strings.TrimSpace(string(status)))
			}
		}

		if opts.NoReview {
			writeLine("[iter %d] ⏭  Review skipped (--no-review)", iter)
			continue
		}

		writeLine("[iter %d] 🔍 Reviewer checking...", iter)
		result, err := c.Run(ctx, sessionID, buildAutoLoopReviewerPrompt(mission, branch, opts.Mode))
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			slog.Warn("AutoLoop reviewer failed", "iteration", iter, "error", err)
			writeLine("⚠️  Reviewer error: %v", err)
			c.appendAutoLoopLog(iter, "reviewer error: "+err.Error())
			continue
		}

		text := responseText(result)
		writeLine("\n── Reviewer result ──")
		writeLine("%s", indentPreview(text, 1000))
		switch {
		case strings.Contains(text, "AUTOLOOP_DONE"):
			if opts.Mode == "hunt" {
				writeLine("ℹ️  Reviewer signaled DONE, but hunt mode ignores it — looking for more issues.")
				c.appendAutoLoopLog(iter, "DONE ignored (hunt mode)")
				break
			}
			writeLine("🏁 Reviewer signaled completion.")
			c.appendAutoLoopLog(iter, "DONE")
			c.autoLoopPush(branch, opts, writeLine)
			return nil
		case strings.Contains(text, "AUTOLOOP_REJECT"):
			writeLine("❌ Reviewer rejected changes; builder will revise next iteration.")
			c.appendAutoLoopLog(iter, "REJECT")
		default:
			c.appendAutoLoopLog(iter, "APPROVE")
		}
	}

	c.autoLoopPush(branch, opts, writeLine)
	writeLine("🏁 AutoLoop finished after %d iteration(s).", iter)
	return nil
}

func (c *coordinator) autoLoopPush(branch string, opts AutoLoopOptions, writeLine func(string, ...any)) {
	if !opts.Push {
		return
	}
	if err := autoLoopGit("push", "-u", "origin", branch); err != nil {
		writeLine("⚠️  Push failed: %v", err)
		return
	}
	writeLine("🚀 Pushed %s to origin.", branch)
}

// initAutoLoopDir creates the memory directory and seeds the mission file.
func (c *coordinator) initAutoLoopDir(mission string) error {
	if err := os.MkdirAll(autoLoopDir, 0o755); err != nil {
		return err
	}
	files := map[string]string{
		"MISSION.md":  "# Mission\n\n" + mission + "\n",
		"PROGRESS.md": "# Progress\n\n",
		"BACKLOG.md":  "# Backlog\n\n",
		"BLOCKERS.md": "# Blockers\n\n",
	}
	for name, content := range files {
		p := filepath.Join(autoLoopDir, name)
		if _, err := os.Stat(p); err == nil {
			continue
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (c *coordinator) appendAutoLoopLog(iter int, status string) {
	f, err := os.OpenFile(filepath.Join(autoLoopDir, "PROGRESS.md"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "- iter %d: %s\n", iter, status)
}

func autoLoopGit(args ...string) error {
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// indentPreview trims and indents a long model response for terminal output.
func indentPreview(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		s = s[:maxLen] + "\n... (truncated)"
	}
	return s
}

func buildAutoLoopBuilderPrompt(mission string, iter int, branch string) string {
	return fmt.Sprintf(`You are the BUILDER half of an autonomous pair working on mission:

%q

Iteration %d on branch %q.

Memory files persist state between turns — READ them first:
- %s/MISSION.md
- %s/PROGRESS.md
- %s/BACKLOG.md
- %s/BLOCKERS.md

Do this in ONE turn:
1. Read the memory files.
2. Pick the single most valuable, concrete task toward the mission.
3. Implement it fully and verify it with real commands (tests, build, lint).
4. Append a one-line entry to %s/PROGRESS.md.
5. If you discover new work, append it to %s/BACKLOG.md.

Do NOT commit. The reviewer will inspect the diff and commit if it passes.
If the mission is fully complete, say so explicitly.`, mission, iter, branch,
		autoLoopDir, autoLoopDir, autoLoopDir, autoLoopDir, autoLoopDir, autoLoopDir)
}

func buildAutoLoopReviewerPrompt(mission string, branch, mode string) string {
	doneRule := "- If the mission looks fully complete and everything verifies, reply exactly: AUTOLOOP_DONE"
	if mode == "hunt" {
		doneRule = "- Do NOT signal completion. There is always more to hunt for; only approve or reject changes."
	}
	return fmt.Sprintf(`You are the REVIEWER half of an autonomous pair on mission:

%q

Branch: %q.

Review the current uncommitted diff (run `+"`git diff`"+` and `+"`git status`"+`).

Rules:
- REJECT if the change does not move the mission forward, breaks build/tests,
  or is scope creep. Reply starting with AUTOLOOP_REJECT: <reason>.
%s
- Otherwise APPROVE: stage and commit with a semantic message (feat:, fix:,
  test:, docs:, refactor:, chore:), then reply with AUTOLOOP_APPROVE.

Append the outcome to %s/PROGRESS.md.`, mission, branch, doneRule, autoLoopDir)
}
