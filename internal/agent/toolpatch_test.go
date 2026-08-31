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

	patched := patchToolSchemas([]fantasy.AgentTool{tool}, "opencode-go")
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

	patched := patchToolSchemas([]fantasy.AgentTool{tool}, "anthropic")
	require.Len(t, patched, 1)
	require.Equal(t, tool, patched[0], "non-opencode providers should be unchanged")
}
