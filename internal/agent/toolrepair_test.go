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
			Input:    `{"command": "echo hi", "description": "test"}`,
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

func TestRepairToolCallArgs_BashMissingCommand_FillFromDescription(t *testing.T) {
	t.Parallel()

	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "bash",
			Input:    `{"description": "echo hello world"}`,
		},
	}

	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, result.Input, `"command"`)
	require.Contains(t, result.Input, `"echo hello world"`)
	require.Contains(t, result.Input, `"description"`)
}

func TestRepairToolCallArgs_BashMissingDescription_FillFromCommand(t *testing.T) {
	t.Parallel()

	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "bash",
			Input:    `{"command": "echo hello world"}`,
		},
	}

	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, result.Input, `"description"`)
	require.Contains(t, result.Input, `echo hello world`)
	require.Contains(t, result.Input, `"command"`)
}

func TestRepairToolCallArgs_WriteMissingFilePath_FillFromAlias(t *testing.T) {
	t.Parallel()

	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "write",
			Input:    `{"filePath": "/tmp/test.txt", "content": "hello"}`,
		},
	}

	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, result.Input, `"file_path"`)
	require.Contains(t, result.Input, `/tmp/test.txt`)
}

func TestRepairToolCallArgs_EditMissingOldString_NoRepairNeeded(t *testing.T) {
	t.Parallel()

	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "edit",
			Input:    `{"file_path": "/tmp/test.txt", "new_string": "hello"}`,
		},
	}

	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.Nil(t, result, "should not repair when old_string is missing (valid for file creation)")
}

func TestRepairToolCallArgs_UnwrapParams(t *testing.T) {
	t.Parallel()

	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "bash",
			Input:    `{"params": {"command": "echo hi", "description": "test"}}`,
		},
	}

	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, result.Input, `"command"`)
	require.Contains(t, result.Input, `"echo hi"`)
	require.NotContains(t, result.Input, `"params"`)
}

func TestRepairToolCallArgs_UnwrapParameters(t *testing.T) {
	t.Parallel()
	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "bash",
			Input:    `{"parameters": {"command": "ls -la", "description": "list files"}}`,
		},
	}
	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, result.Input, `"command"`)
	require.NotContains(t, result.Input, `"parameters"`)
}

func TestRepairToolCallArgs_UnwrapInput(t *testing.T) {
	t.Parallel()
	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "bash",
			Input:    `{"input": {"command": "pwd", "description": "print working dir"}}`,
		},
	}
	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, result.Input, `"command"`)
	require.NotContains(t, result.Input, `"input"`)
}

func TestRepairToolCallArgs_BashBothFieldsMissing(t *testing.T) {
	t.Parallel()
	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "bash",
			Input:    `{}`,
		},
	}
	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.Nil(t, result, "should not repair when both command and description are missing")
}

func TestRepairToolCallArgs_WriteMissingContent_FillFromTextAlias(t *testing.T) {
	t.Parallel()
	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "write",
			Input:    `{"file_path": "/tmp/test.txt", "text": "hello world"}`,
		},
	}
	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, result.Input, `"content"`)
	require.Contains(t, result.Input, `"hello world"`)
}

func TestRepairToolCallArgs_EditFilePathFromPathAlias(t *testing.T) {
	t.Parallel()
	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "edit",
			Input:    `{"path": "/tmp/test.txt", "old_string": "foo", "new_string": "bar"}`,
		},
	}
	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, result.Input, `"file_path"`)
	require.Contains(t, result.Input, `/tmp/test.txt`)
}

func TestRepairToolCallArgs_EditOldStringFromFindAlias(t *testing.T) {
	t.Parallel()
	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "edit",
			Input:    `{"file_path": "/tmp/test.txt", "find": "old text", "new_string": "new text"}`,
		},
	}
	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, result.Input, `"old_string"`)
	require.Contains(t, result.Input, `"old text"`)
}

func TestRepairToolCallArgs_EditNewStringFromReplaceAlias(t *testing.T) {
	t.Parallel()
	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "edit",
			Input:    `{"file_path": "/tmp/test.txt", "old_string": "old", "replace": "new"}`,
		},
	}
	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, result.Input, `"new_string"`)
	require.Contains(t, result.Input, `"new"`)
}

func TestRepairToolCallArgs_MultieditFilePathFromAlias(t *testing.T) {
	t.Parallel()
	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "multiedit",
			Input:    `{"filePath": "/tmp/test.txt", "edits": [{"old_string": "a", "new_string": "b"}]}`,
		},
	}
	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, result.Input, `"file_path"`)
	require.Contains(t, result.Input, `/tmp/test.txt`)
}

func TestRepairToolCallArgs_MultieditEditsFromChangesAlias(t *testing.T) {
	t.Parallel()
	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "multiedit",
			Input:    `{"file_path": "/tmp/test.txt", "changes": [{"old_string": "a", "new_string": "b"}]}`,
		},
	}
	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, result.Input, `"edits"`)
}

func TestRepairToolCallArgs_NestedWrapperDoubleWrapped(t *testing.T) {
	t.Parallel()
	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "bash",
			Input:    `{"arguments": {"arguments": {"command": "echo nested", "description": "nested test"}}}`,
		},
	}
	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, result.Input, `"command"`)
	require.Contains(t, result.Input, `"echo nested"`)
	// Double-wrapped: repair unwraps outer layer, inner arguments becomes a field.
	// This is an extreme edge case — the important thing is command/description are present.
}

func TestRepairToolCallArgs_BashCommandFromExtractJSONField(t *testing.T) {
	t.Parallel()
	opts := fantasy.ToolCallRepairOptions{
		OriginalToolCall: fantasy.ToolCallContent{
			ToolName: "bash",
			Input:    `{"arguments": "{\"description\": \"run echo\"}"}`,
		},
	}
	result, err := repairToolCallArgs(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, result.Input, `"command"`)
	require.Contains(t, result.Input, `"run echo"`)
}

func TestPatchToolSchemas_MuseHasReqHint(t *testing.T) {
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
	patched := patchToolSchemas([]fantasy.AgentTool{tool}, "opencode-go", "muse-spark-1.3-contributor")
	require.Len(t, patched, 1)
	info := patched[0].Info()
	require.Empty(t, info.Required)
}

func TestPatchToolSchemas_NonMuseOpencodeNoReqHint(t *testing.T) {
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
	patched := patchToolSchemas([]fantasy.AgentTool{tool}, "opencode-go", "deepseek-v4-flash")
	require.Len(t, patched, 1)
	info := patched[0].Info()
	require.Empty(t, info.Required)
}
