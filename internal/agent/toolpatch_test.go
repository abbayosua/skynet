package agent

import (
	"context"
	"encoding/json"
	"os"
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
	require.Equal(t, tool, patchToolSchemas([]fantasy.AgentTool{tool}, "muse", "muse-spark-1.3-contributor")[0], "direct chat-completions muse must stay untouched")
}

func TestPatchToolSchemas_MuseStripsRequired(t *testing.T) {
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

	patched := patchToolSchemas([]fantasy.AgentTool{tool}, "MuseV1Responses", "muse-spark-1.3-contributor")
	require.Len(t, patched, 1)
	require.Empty(t, patched[0].Info().Required, "required should be empty for MuseV1Responses")
}

func TestRepairOpencodeInput_MuseNestedArguments(t *testing.T) {
	t.Parallel()

	info := fantasy.ToolInfo{
		Name:     "bash",
		Required: []string{},
	}

	input := `{"arguments":"{\"command\":\"echo hi\",\"description\":\"run echo\"}"}`
	result := repairOpencodeInput(input, info)
	require.NotEmpty(t, result, "should unwrap nested muse arguments wrapper")
	require.Contains(t, result, `"command"`)
	require.Contains(t, result, `"echo hi"`)
}

func TestSalvageRequiredFields_TruncatedNestedWrapper(t *testing.T) {
	t.Parallel()

	// Real-world Muse failure: 3-layer arguments wrapper, truncated,
	// file_path buried at the tail. Must salvage instead of erroring.
	input := `{"arguments":"{\"file_path\": \"/tmp/x.txt\"}"}`
	info := fantasy.ToolInfo{Name: "view", Required: []string{}}
	result := repairOpencodeInput(input, info)
	require.NotEmpty(t, result, "should salvage file_path from truncated nesting")
	require.Contains(t, result, `"file_path"`)
	require.Contains(t, result, `/tmp/x.txt`)
}

func TestSalvageRequiredFields_HealthyUntouched(t *testing.T) {
	t.Parallel()

	// Healthy inputs return "" (no rewrite needed) so the original bytes
	// go to the provider untouched (prefix cache stays byte-identical).
	input := `{"file_path":"/tmp/x.txt","limit":10}`
	info := fantasy.ToolInfo{Name: "view", Required: []string{}}
	result := repairOpencodeInput(input, info)
	require.Empty(t, result, "healthy input must not be rewritten")
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

func TestRepairOpencodeInput_BashWrappedNoCommand(t *testing.T) {
	t.Parallel()

	// Muse nested wrapper that unwraps to description-only (no command
	// anywhere): the bash fallback must fill command from description
	// instead of letting the tool fail with "missing command".
	info := fantasy.ToolInfo{
		Name:     "bash",
		Required: []string{},
	}

	input := `{"arguments":"{\"description\": \"run echo\"}"}`
	result := repairOpencodeInput(input, info)
	require.NotEmpty(t, result, "should fill command from description in wrapped input")
	require.Contains(t, result, `"command"`)
	require.Contains(t, result, `run echo`)
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

func TestSalvageRequiredFields_RealZellyInput(t *testing.T) {
	b, err := os.ReadFile("testdata/muse_nested_truncated.json")
	require.NoError(t, err)
	info := fantasy.ToolInfo{Name: "edit", Required: []string{}}
	result := repairOpencodeInput(string(b), info)
	require.NotEmpty(t, result, "should salvage file_path from real truncated nesting")
	require.Contains(t, result, `file_path`)
}

func TestRepairOpencodeInput_RealTourandtravelCases(t *testing.T) {

	cases := []struct {
		name    string
		file    string
		tool    string
		wantKey string
	}{
		{
			name:    "bash nested missing command",
			file:    "testdata/muse_bash_nested_missing_command.json",
			tool:    "bash",
			wantKey: "command",
		},
		{
			name:    "view nested missing file_path",
			file:    "testdata/muse_view_nested_missing_file_path.json",
			tool:    "view",
			wantKey: "file_path",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {

			raw, err := os.ReadFile(tc.file)
			require.NoError(t, err)

			info := fantasy.ToolInfo{Name: tc.tool, Required: []string{}}
			out := repairOpencodeInput(string(raw), info)
			require.NotEmpty(t, out, "repair must produce usable input")

			var got map[string]any
			require.NoError(t, json.Unmarshal([]byte(out), &got))
			val, ok := got[tc.wantKey].(string)
			require.True(t, ok, tc.wantKey+" must be present as a string")
			require.NotEmpty(t, val)
		})
	}
}
