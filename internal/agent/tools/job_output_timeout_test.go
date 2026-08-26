package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/abbayosua/skynet/internal/shell"
	"github.com/stretchr/testify/require"
)

func TestJobOutput_WaitTimeout_ReturnsRunning(t *testing.T) {
	t.Parallel()

	workingDir := t.TempDir()
	bgManager := shell.GetBackgroundShellManager()
	bgShell, err := bgManager.Start(context.Background(), workingDir, nil, "sleep 5", "")
	require.NoError(t, err)
	defer bgManager.Kill(bgShell.ID)

	// Call job_output with wait=true but timeout 1s -> should return within ~1s with running
	tool := NewJobOutputTool()
	params := JobOutputParams{ShellID: bgShell.ID, Wait: true, Timeout: 1}
	input, _ := json.Marshal(params)
	call := fantasy.ToolCall{ID: "call-timeout", Name: JobOutputToolName, Input: string(input)}

	start := time.Now()
	resp, err := tool.Run(context.Background(), call)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.False(t, resp.IsError)

	// Must not have waited full 5s
	require.Less(t, elapsed, 2*time.Second, "wait with timeout should return early")
	require.GreaterOrEqual(t, elapsed, 900*time.Millisecond)

	// Status must be running, not completed
	require.Contains(t, resp.Content, "Status: running")
	require.Contains(t, resp.Content, "running")

	var meta JobOutputResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &meta))
	require.False(t, meta.Done)

	// Job should still be running
	require.False(t, bgShell.IsDone())
}

func TestJobOutput_WaitWithoutTimeout_CompletesWhenDone(t *testing.T) {
	t.Parallel()

	workingDir := t.TempDir()
	bgManager := shell.GetBackgroundShellManager()
	bgShell, err := bgManager.Start(context.Background(), workingDir, nil, "echo done", "")
	require.NoError(t, err)
	defer bgManager.Kill(bgShell.ID)

	// Wait for echo to finish, but tool waits with default 30s -> should complete quickly
	tool := NewJobOutputTool()
	params := JobOutputParams{ShellID: bgShell.ID, Wait: true, Timeout: 2}
	input, _ := json.Marshal(params)
	call := fantasy.ToolCall{ID: "call-done", Name: JobOutputToolName, Input: string(input)}

	resp, err := tool.Run(context.Background(), call)
	require.NoError(t, err)
	require.Contains(t, resp.Content, "Status: completed")
	require.Contains(t, resp.Content, "done")

	var meta JobOutputResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &meta))
	require.True(t, meta.Done)
}

func TestJobOutput_WaitTimeout_Clamped(t *testing.T) {
	t.Parallel()

	workingDir := t.TempDir()
	bgManager := shell.GetBackgroundShellManager()
	// Very long sleep, but timeout 300 clamped - we use 1 for test, check clamp logic doesn't panic for big value
	bgShell, err := bgManager.Start(context.Background(), workingDir, nil, "sleep 10", "")
	require.NoError(t, err)
	defer bgManager.Kill(bgShell.ID)

	tool := NewJobOutputTool()
	params := JobOutputParams{ShellID: bgShell.ID, Wait: true, Timeout: 999}
	input, _ := json.Marshal(params)
	call := fantasy.ToolCall{ID: "call-clamp", Name: JobOutputToolName, Input: string(input)}

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	start := time.Now()
	resp, _ := tool.Run(ctx, call)
	elapsed := time.Since(start)

	// ctx should cancel before 300s, so we get ctx error path -> resp may be error or running
	// Just ensure we didn't block for 10s
	require.Less(t, elapsed, 2*time.Second)
	_ = resp
}
