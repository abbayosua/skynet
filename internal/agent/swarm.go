package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/abbayosua/skynet/internal/config"
)

// SwarmFastOptions configures a RunAutoLoopSwarm invocation.
type SwarmFastOptions struct {
	// Branch is the git branch swarm commits to. Empty = "swarm/<ts>".
	Branch string
	// Push pushes the branch to origin when the run finishes.
	Push bool
	// Workers is the max parallel worker agents. Default 3.
	Workers int
	// Reviewers is informational; review runs as one parent turn.
	Reviewers int
	// MaxIterations bounds a second pipeline pass; 0 = single pass.
	MaxIterations int
	// Budget bounds wall-clock time; 0 = unlimited.
	Budget time.Duration
}

const swarmDir = ".skynet/swarm"

// RunAutoLoopSwarm runs a fast autonomous pipeline:
//
//  1. Orchestrator decomposes the mission into independent tasks.
//  2. Workers execute tasks in parallel (background agents).
//  3. A reviewer pass inspects the combined diff and commits.
//
// Workers share the working directory; the orchestrator is instructed to
// split tasks across disjoint files/directories to avoid edit conflicts.
func (c *coordinator) RunAutoLoopSwarm(ctx context.Context, sessionID, mission string, opts SwarmFastOptions, output io.Writer) error {
	if strings.TrimSpace(mission) == "" {
		return fmt.Errorf("swarm: mission is required")
	}
	if opts.Workers <= 0 {
		opts.Workers = 3
	}
	if opts.Reviewers <= 0 {
		opts.Reviewers = 2
	}

	writeLine := func(format string, args ...any) {
		if output == nil {
			return
		}
		fmt.Fprintf(output, format+"\n", args...)
	}

	if err := os.MkdirAll(filepath.Join(swarmDir, "queue"), 0o755); err != nil {
		return fmt.Errorf("swarm: init dir: %w", err)
	}

	branch := opts.Branch
	if branch == "" {
		branch = "swarm/" + time.Now().Format("20060102-150405")
	}
	if err := autoLoopGit("checkout", "-b", branch); err != nil {
		if err2 := autoLoopGit("checkout", branch); err2 != nil {
			return fmt.Errorf("swarm: cannot checkout %q: %w", branch, err2)
		}
	}

	writeLine("── SwarmFast ──────────────────────────────")
	writeLine("🎯 Mission: %s", mission)
	writeLine("🌿 Branch: %s", branch)
	writeLine("👷 Workers: %d | Reviewers: %d", opts.Workers, opts.Reviewers)

	// Phase 1: planning.
	writeLine("\n[1/3] 🧠 Orchestrator planning...")
	planPrompt := fmt.Sprintf(`Break this mission into independent, parallelizable tasks that each touch DIFFERENT files or directories (so they can run in parallel without conflicts):

Mission: %q

Output ONLY a markdown checklist to %s/PLAN.md, one task per line, each ending with a verification command. Do not execute anything.`, mission, swarmDir)
	if _, err := c.Run(ctx, sessionID, planPrompt); err != nil {
		return fmt.Errorf("swarm: planning failed: %w", err)
	}

	planBytes, err := os.ReadFile(filepath.Join(swarmDir, "PLAN.md"))
	if err != nil {
		return fmt.Errorf("swarm: plan not written: %w", err)
	}
	tasks := parsePlanSteps(string(planBytes), 0)
	if len(tasks) == 0 {
		return fmt.Errorf("swarm: orchestrator produced no parseable tasks")
	}
	writeLine("📋 %d task(s) planned", len(tasks))

	// Phase 2: parallel workers.
	writeLine("\n[2/3] 👷 Workers executing in parallel...")
	sem := make(chan struct{}, opts.Workers)
	var wg sync.WaitGroup
	for i, task := range tasks {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, task string) {
			defer wg.Done()
			defer func() { <-sem }()
			writeLine("  ▶ worker %d: %s", i+1, task)
			workerPrompt := fmt.Sprintf(`You are worker agent %d in a parallel swarm. Mission: %q.

Your task ONLY: %s

Rules:
- Touch ONLY files related to your task. Do not edit files owned by other tasks.
- Verify with a real command (tests/build/lint) before finishing.
- Do NOT git commit. Do NOT install new dependencies.`, i+1, mission, task)
			result, err := c.runBackgroundTaskWithAgent(ctx, config.AgentCoder, workerPrompt)
			if err != nil {
				slog.Warn("Swarm worker failed", "task", i+1, "error", err)
				writeLine("  ⚠️ worker %d failed: %v", i+1, err)
				return
			}
			writeLine("  ✅ worker %d done: %s", i+1, firstLine(result))
		}(i, task)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Phase 3: review + commit.
	writeLine("\n[3/3] 🔍 Review & commit...")
	reviewPrompt := fmt.Sprintf(`Review ALL uncommitted changes in this repo (run git status/git diff).

Mission: %q

Rules:
- If everything verifies and moves the mission forward: stage and commit with a semantic message (first line), then reply AUTOLOOP_APPROVE.
- If the changes are broken or unrelated: reply AUTOLOOP_REJECT: <reason> and DO NOT commit.`, mission)
	result, err := c.Run(ctx, sessionID, reviewPrompt)
	if err != nil {
		return fmt.Errorf("swarm: review failed: %w", err)
	}
	text := responseText(result)
	if strings.Contains(text, "AUTOLOOP_REJECT") {
		writeLine("❌ Reviewer rejected the changes; nothing committed.")
		if opts.Push {
			c.autoLoopPush(branch, AutoLoopOptions{Push: opts.Push}, writeLine)
		}
		return nil
	}

	if opts.Push {
		if err := autoLoopGit("push", "-u", "origin", branch); err != nil {
			writeLine("⚠️  Push failed: %v", err)
		} else {
			writeLine("🚀 Pushed %s to origin.", branch)
		}
	}
	writeLine("🏁 SwarmFast finished.")
	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[:i]
	}
	return truncateAutoPilotMsg(s, 120)
}
