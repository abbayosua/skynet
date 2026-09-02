package agent

import (
	"context"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

// fakeBashParams mirrors the bash tool's required fields so schema.Generate
// marks command+description as required.
type fakeBashParams struct {
	Command     string `json:"command" description:"REQUIRED: The command to execute (required, do not omit)"`
	Description string `json:"description" description:"REQUIRED: A brief description of what the command does (required, do not omit)"`
}

func fakeBashTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		"bash",
		"Run a shell command",
		func(_ context.Context, _ fakeBashParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse("ok"), nil
		},
	)
}

func TestRepairToolCallArgs_FillDescriptionFromCommand(t *testing.T) {
	t.Parallel()

	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "bash",
			Input:    `{"command": "echo 'repair_works'"}`,
		},
		AvailableTools: []fantasy.AgentTool{fakeBashTool()},
	}

	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, result, "should repair input missing description")
	require.Contains(t, result.Input, `"command"`)
	require.Contains(t, result.Input, `"echo 'repair_works'"`)
	require.Contains(t, result.Input, `"description"`)
}

func TestShouldUseArgsRepair_BAI(t *testing.T) {
	t.Parallel()

	require.True(t, shouldUseArgsRepair("b-ai"), "b-ai provider should use args repair")
	require.True(t, shouldUseArgsRepair("b-ai-motivasihiduptt"), "b-ai-motivasihiduptt provider should use args repair")
	require.True(t, shouldUseArgsRepair("b-ai-nvbsei"), "b-ai-nvbsei provider should use args repair")
	require.True(t, shouldUseArgsRepair("b-ai-bangdjarot"), "b-ai-bangdjarot provider should use args repair")
	require.True(t, shouldUseArgsRepair("b-ai-any-new-provider"), "any b-ai-* provider should use args repair")
	require.True(t, shouldUseArgsRepair("opencode-go"), "opencode-go provider should use args repair")
	require.False(t, shouldUseArgsRepair("anthropic"), "anthropic should not use args repair")
	require.False(t, shouldUseArgsRepair("gemini"), "gemini should not use args repair")
}
