package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"github.com/abbayosua/skynet/internal/agent"
	"github.com/abbayosua/skynet/internal/agent/notify"
	"github.com/abbayosua/skynet/internal/event"
	"github.com/abbayosua/skynet/internal/session"
	"github.com/abbayosua/skynet/internal/workspace"
	"github.com/spf13/cobra"
)

var autoLoopCmd = &cobra.Command{
	Use:   "autoloop <mission>",
	Short: "Run a fully autonomous mission-driven loop",
	Long: `Run AutoLoop — a mission-driven autonomous developer pair.

A builder agent picks and implements the most valuable task each
iteration, and a reviewer agent inspects the diff before committing.
Progress is persisted in .skynet/autoloop/*.md and commits land on a
dedicated branch so you can cherry-pick the good ones.`,
	Example: `
# Free-running, no budget, push when done
skynet autoloop "make this app production-ready - test all features and translation" --budget 0 --push

# Bounded run on a named branch
skynet autoloop "fix all failing tests" --branch fix/tests --max-iterations 20

# Continue a previous session
skynet autoloop --continue "keep improving test coverage"
	`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var mission string
		if len(args) > 0 {
			mission = strings.TrimSpace(args[0])
		}

		var (
			sessionID, _   = cmd.Flags().GetString("session")
			useLast, _     = cmd.Flags().GetBool("continue")
			branch, _      = cmd.Flags().GetString("branch")
			push, _        = cmd.Flags().GetBool("push")
			noReview, _    = cmd.Flags().GetBool("no-review")
			maxIters, _    = cmd.Flags().GetInt("max-iterations")
			budget, _      = cmd.Flags().GetDuration("budget")
			mode, _        = cmd.Flags().GetString("mode")
			autoCompact, _ = cmd.Flags().GetInt64("auto-compact")
		)
		if mode != "normal" && mode != "hunt" {
			return fmt.Errorf("invalid --mode %q: must be \"normal\" or \"hunt\"", mode)
		}

		if mission == "" && !useLast && sessionID == "" {
			return fmt.Errorf("mission is required (usage: skynet autoloop <mission>)")
		}

		ws, cleanup, err := setupLocalWorkspace(cmd)
		if err != nil {
			return err
		}
		defer cleanup()

		event.AppInitialized()

		if !ws.Config().IsConfigured() {
			return fmt.Errorf("no providers configured - please run 'skynet' to set up a provider interactively")
		}

		appWs := ws.(*workspace.AppWorkspace)
		a := appWs.App()

		ctx := context.Background()

		var sess session.Session
		switch {
		case sessionID != "":
			sess, err = a.Sessions.Get(ctx, sessionID)
			if err != nil {
				return fmt.Errorf("session %q not found: %w", sessionID, err)
			}

		case useLast:
			sess, err = a.Sessions.GetLast(ctx)
			if err != nil {
				slog.Info("No previous sessions found, creating a fresh session")
				sess, err = a.Sessions.Create(ctx, "AutoLoop")
				if err != nil {
					return fmt.Errorf("failed to create session: %w", err)
				}
			}

		default:
			sess, err = a.Sessions.Create(ctx, "AutoLoop")
			if err != nil {
				return fmt.Errorf("failed to create session: %w", err)
			}
		}

		if mission == "" {
			return fmt.Errorf("mission is required")
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
		defer cancel()

		a.Permissions.AutoApproveSession(sess.ID)

		slog.Info("Starting AutoLoop",
			"session_id", sess.ID,
			"mission", mission,
		)
		fmt.Printf("Mission: %s\nSession: %s\n\n", mission, sess.ID)

		if autoCompact > 0 {
			a.AgentCoordinator.SetAutoCompactTokens(sess.ID, autoCompact)
		}

		// Stream agent activity (tool calls, thinking, errors) to stdout
		// so the run is visible instead of silent between iterations.
		events := a.AgentNotifications().Subscribe(ctx)
		go func() {
			for ev := range events {
				n := ev.Payload
				if n.SessionID != sess.ID {
					continue
				}
				switch n.Type {
				case notify.TypeActivityUpdate, notify.TypeToolError, notify.TypeAgentError:
					fmt.Printf("  · %s\n", n.Activity)
				case notify.TypeToolOutput:
					if n.Activity != "" {
						fmt.Printf("  · tool: %s\n", truncate(n.Activity, 200))
					}
				}
			}
		}()

		return a.AgentCoordinator.RunAutoLoop(ctx, sess.ID, mission, agent.AutoLoopOptions{
			Branch:        branch,
			Push:          push,
			NoReview:      noReview,
			MaxIterations: maxIters,
			Budget:        budget,
			Mode:          mode,
		}, os.Stdout)
	},
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func init() {
	autoLoopCmd.Flags().StringP("session", "s", "", "Existing session ID to continue in")
	autoLoopCmd.Flags().BoolP("continue", "C", false, "Continue in the most recent session")
	autoLoopCmd.Flags().String("branch", "", "Git branch to commit to (default autoloop/<timestamp>)")
	autoLoopCmd.Flags().Bool("push", false, "Push the branch to origin when done")
	autoLoopCmd.Flags().Bool("no-review", false, "Skip the reviewer turn")
	autoLoopCmd.Flags().IntP("max-iterations", "m", 0, "Maximum iterations (0 = unlimited)")
	autoLoopCmd.Flags().Duration("budget", 0, "Wall-clock budget, e.g. 2h (0 = unlimited)")
	autoLoopCmd.Flags().String("mode", "normal", "Run mode: normal or hunt (hunt never stops on reviewer DONE)")
	autoLoopCmd.Flags().Int64("auto-compact", 0, "Auto-compact threshold in tokens (0 = default)")
	autoLoopCmd.MarkFlagsMutuallyExclusive("session", "continue")
}
