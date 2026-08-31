package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/abbayosua/skynet/internal/shell"
)

const (
	JobOutputToolName = "job_output"
)

//go:embed job_output.md
var jobOutputDescription string

type JobOutputParams struct {
	ShellID string `json:"shell_id" description:"REQUIRED: The ID of the background shell to retrieve output from (required, do not omit)"`
	Wait    bool   `json:"wait" description:"If true, block until the background shell completes before returning output"`
	Timeout int    `json:"timeout,omitempty" description:"When wait is true, max seconds to wait (default 30, max 300). Returns current output with running status if exceeded, so the agent can re-poll. Prevents infinite hang on never-ending jobs (e.g. dev servers)"`
}

type JobOutputResponseMetadata struct {
	ShellID          string `json:"shell_id"`
	Command          string `json:"command"`
	Description      string `json:"description"`
	Done             bool   `json:"done"`
	WorkingDirectory string `json:"working_directory"`
}

func NewJobOutputTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		JobOutputToolName,
		jobOutputDescription,
		func(ctx context.Context, params JobOutputParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.ShellID == "" {
				return fantasy.NewTextErrorResponse("missing shell_id"), nil
			}

			bgManager := shell.GetBackgroundShellManager()
			bgShell, ok := bgManager.Get(params.ShellID)
			if !ok {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("background shell not found: %s", params.ShellID)), nil
			}

			if params.Wait {
				waitDur := 30 * time.Second
				if params.Timeout > 0 {
					waitDur = time.Duration(params.Timeout) * time.Second
					if waitDur > 300*time.Second {
						waitDur = 300 * time.Second
					}
					if waitDur < time.Second {
						waitDur = time.Second
					}
				}
				// Log visibility while we block - helps diagnose stuck-looking states.
				ReportActivity(ctx, fmt.Sprintf("Waiting for job %s (max %s)...", params.ShellID, waitDur))
				waitCtx, cancel := context.WithTimeout(ctx, waitDur)
				completed := bgShell.WaitContext(waitCtx)
				cancel()
				if !completed {
					// Check if parent context was cancelled vs timeout.
					if ctx.Err() != nil {
						return fantasy.ToolResponse{}, ctx.Err()
					}
					// Timeout hit while job still running - we will return current output below with
					// status running so the agent re-polls instead of hanging forever.
					// Add a hint to output so the model knows to retry with wait if needed.
				}
			}

			stdout, stderr, done, err := bgShell.GetOutput()

			var outputParts []string
			if stdout != "" {
				outputParts = append(outputParts, stdout)
			}
			if stderr != "" {
				outputParts = append(outputParts, stderr)
			}

			status := "running"
			if done {
				status = "completed"
				if err != nil {
					exitCode := shell.ExitCode(err)
					if exitCode != 0 {
						outputParts = append(outputParts, fmt.Sprintf("Exit code %d", exitCode))
					}
				}
			}

			output := strings.Join(outputParts, "\n")
			output = TruncateOutput(output)

			metadata := JobOutputResponseMetadata{
				ShellID:          params.ShellID,
				Command:          bgShell.Command,
				Description:      bgShell.Description,
				Done:             done,
				WorkingDirectory: bgShell.WorkingDir,
			}

			if output == "" {
				output = BashNoOutput
			}

			result := fmt.Sprintf("Status: %s\n\n%s", status, output)
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(result), metadata), nil
		})
}
