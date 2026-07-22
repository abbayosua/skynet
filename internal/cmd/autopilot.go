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
	Use:   "autopilot [session-id]",
	Short: "Start autonomous coding mode",
	Long: `Start the AutoPilot - a fully autonomous AI developer that continuously 
improves the codebase. It has its own context and works independently.

Optionally pass a session ID to give the AutoPilot read-only access to
that session's conversation history for context.`,
	Example: `
# Start autopilot with access to the most recent session
skynet autopilot

# Start autopilot with access to a specific session
skynet autopilot <session-id>

# Start autopilot with the latest session
skynet autopilot --continue
	`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var (
			sessionID, _  = cmd.Flags().GetString("session")
			useLast, _    = cmd.Flags().GetBool("continue")
		)

		if len(args) > 0 && sessionID == "" {
			sessionID = args[0]
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

		// Resolve main session ID for context access.
		var mainSessionID string
		ctx := context.Background()
		switch {
		case sessionID != "":
			sess, err := a.Sessions.Get(ctx, sessionID)
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
			mainSessionID = sess.ID

		case useLast:
			sess, err := a.Sessions.GetLast(ctx)
			if err != nil {
				slog.Info("No previous sessions found, autopilot running without context")
			} else {
				mainSessionID = sess.ID
			}
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
		defer cancel()

		slog.Info("Starting AutoPilot",
			"session_id", mainSessionID,
		)

		return a.AgentCoordinator.RunAutoPilot(ctx, os.Stdout, mainSessionID)
	},
}

func init() {
	autopilotCmd.Flags().StringP("session", "s", "", "Session ID to read for context")
	autopilotCmd.Flags().BoolP("continue", "C", false, "Use the most recent session for context")
	autopilotCmd.MarkFlagsMutuallyExclusive("session", "continue")
}
