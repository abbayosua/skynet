package agent

import (
	"context"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

func TestPatchToolSchemas_OpencodeStripsRequired(t *testing.T) {
	t.Parallel()

	tool := fantasy.NewAgentTool(
		"bash",
		"Run a shell command",
		func(ctx context.Context, params struct {
			Description string `json:"description"`
			Command     string `json:"command"`
		}, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse("ok"), nil
		},
	)

	patched := patchToolSchemas([]fantasy.AgentTool{tool}, "opencode-go", "muse-spark-1.2-contributor")
	require.Len(t, patched, 1)
	info := patched[0].Info()
	require.Empty(t, info.Required, "required should be empty for opencode providers")
}

func TestPatchToolSchemas_NonOpencodePreservesRequired(t *testing.T) {
	t.Parallel()

	tool := fantasy.NewAgentTool(
		"bash",
		"Run a shell command",
		func(ctx context.Context, params struct {
			Description string `json:"description"`
			Command     string `json:"command"`
		}, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse("ok"), nil
		},
	)

	patched := patchToolSchemas([]fantasy.AgentTool{tool}, "anthropic", "claude-4-sonnet")
	require.Len(t, patched, 1)
	require.Equal(t, tool, patched[0], "non-opencode providers should be unchanged")
}

func TestRepairOpencodeInput_BashMissingCommand(t *testing.T) {
	t.Parallel()

	info := fantasy.ToolInfo{
		Name:     "bash",
		Required: []string{},
	}

	input := `{"description": "run echo"}`
	result := repairOpencodeInput(input, info)
	require.NotEmpty(t, result, "should repair bash input missing command")
	require.Contains(t, result, `"command"`)
	require.Contains(t, result, `"run echo"`)
}

func TestRepairOpencodeInput_BashMissingDescription(t *testing.T) {
	t.Parallel()

	info := fantasy.ToolInfo{
		Name:     "bash",
		Required: []string{},
	}

	input := `{"command": "echo hi"}`
	result := repairOpencodeInput(input, info)
	require.NotEmpty(t, result, "should repair bash input missing description")
	require.Contains(t, result, `"command"`)
	require.Contains(t, result, `"echo hi"`)
	require.Contains(t, result, `"description"`)
}

func TestRepairOpencodeInput_BashEmptyCommand(t *testing.T) {
	t.Parallel()

	info := fantasy.ToolInfo{
		Name:     "bash",
		Required: []string{},
	}

	input := `{"command": "", "description": "run something"}`
	result := repairOpencodeInput(input, info)
	require.NotEmpty(t, result, "should repair bash input with empty command")
	require.Contains(t, result, `"command"`)
	require.Contains(t, result, `"run something"`)
}
