package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"github.com/abbayosua/skynet/internal/event"
	"github.com/abbayosua/skynet/internal/session"
	"github.com/abbayosua/skynet/internal/workspace"
	"github.com/spf13/cobra"
)

var autopilotCmd = &cobra.Command{
	Use:   "autopilot <goal>",
	Short: "Run the AutoPilot on a concrete goal",
	Long: `Run the AutoPilot - a goal-driven autonomous AI developer.

It plans the work, executes each step in its own turn (with tests/build
verification), and finishes with a summary report. All progress is
stored as a regular session, so you can review it later in the TUI.

Optionally pass --session or --continue to seed the run with context
from an existing session.`,
	Example: `
# Run autopilot on a new session with a concrete goal
skynet autopilot "fix the race condition in internal/shell/background.go"

# Continue working in an existing session
skynet autopilot --continue "add tests for the job output timeout"
	`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		goal := strings.TrimSpace(args[0])

		var (
			sessionID, _ = cmd.Flags().GetString("session")
			useLast, _   = cmd.Flags().GetBool("continue")
			maxSteps, _  = cmd.Flags().GetInt("max-steps")
		)

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

		// Resolve an existing session for context, otherwise create one.
		var sess session.Session
		switch {
		case sessionID != "":
			sess, err = a.Sessions.Get(ctx, sessionID)
			if err != nil {
				sessions, listErr := a.Sessions.List(ctx)
				if listErr != nil {
					return fmt.Errorf("session %q not found and cannot list sessions: %w", sessionID, err)
				}
				var found bool
				for _, s := range sessions {
					hash := session.HashID(s.ID)
					if hash == sessionID || strings.HasPrefix(hash, sessionID) {
						sess = s
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("session %q not found", sessionID)
				}
			}

		case useLast:
			sess, err = a.Sessions.GetLast(ctx)
			if err != nil {
				slog.Info("No previous sessions found, creating a fresh session")
				sess, err = a.Sessions.Create(ctx, "AutoPilot")
				if err != nil {
					return fmt.Errorf("failed to create session: %w", err)
				}
			}

		default:
			sess, err = a.Sessions.Create(ctx, "AutoPilot")
			if err != nil {
				return fmt.Errorf("failed to create session: %w", err)
			}
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
		defer cancel()

		// Headless mode: approve permissions automatically for this run.
		a.Permissions.AutoApproveSession(sess.ID)

		slog.Info("Starting AutoPilot",
			"session_id", sess.ID,
			"goal", goal,
		)
		fmt.Printf("Goal: %s\nSession: %s\n\n", goal, sess.ID)

		return a.AgentCoordinator.RunAutoPilotGoal(ctx, sess.ID, goal, maxSteps, os.Stdout)
	},
}

func init() {
	autopilotCmd.Flags().StringP("session", "s", "", "Existing session ID to continue in")
	autopilotCmd.Flags().BoolP("continue", "C", false, "Continue in the most recent session")
	autopilotCmd.Flags().IntP("max-steps", "m", 10, "Maximum number of steps/iterations")
	autopilotCmd.MarkFlagsMutuallyExclusive("session", "continue")
}
