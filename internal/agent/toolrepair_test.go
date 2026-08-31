package agent

import (
	"context"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

func TestRepairToolCallArgs_UnwrapArguments(t *testing.T) {
	t.Parallel()

	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "bash",
			Input:    `{"arguments": {"command": "echo hi", "description": "test"}}`,
		},
	}

	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, result.Input, `"command"`)
	require.Contains(t, result.Input, `"echo hi"`)
	require.NotContains(t, result.Input, `"arguments"`)
}

func TestRepairToolCallArgs_NoWrapping(t *testing.T) {
	t.Parallel()

	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "bash",
			Input:    `{"command": "echo hi"}`,
		},
	}

	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.Nil(t, result, "should not repair already-correct input")
}

func TestRepairToolCallArgs_EmptyInput(t *testing.T) {
	t.Parallel()

	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "bash",
			Input:    "",
		},
	}

	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.Nil(t, result)
}

func TestRepairToolCallArgs_MultipleKeys(t *testing.T) {
	t.Parallel()

	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "bash",
			Input:    `{"command": "echo hi", "description": "test"}`,
		},
	}

	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.Nil(t, result, "should not repair when multiple top-level keys exist")
}

func TestRepairToolCallArgs_ArgumentsNotObject(t *testing.T) {
	t.Parallel()

	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "bash",
			Input:    `{"arguments": "not an object"}`,
		},
	}

	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.Nil(t, result, "should not repair when arguments is not an object")
}
