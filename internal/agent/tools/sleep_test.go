package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

func TestSleepTool_Seconds(t *testing.T) {
	t.Parallel()
	tool := NewSleepTool()
	ctx := context.Background()

	start := time.Now()
	resp := runSleepTool(t, tool, ctx, SleepParams{Seconds: 0.1})
	elapsed := time.Since(start)

	require.False(t, resp.IsError, "response should not be error: %s", resp.Content)
	require.Contains(t, resp.Content, "Slept for")
	require.GreaterOrEqual(t, elapsed.Milliseconds(), int64(90))
	require.Less(t, elapsed.Milliseconds(), int64(500))

	var meta SleepResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &meta))
	require.InDelta(t, 0.1, meta.Seconds, 0.02)
	require.Equal(t, "100ms", meta.Duration)
}

func TestSleepTool_DurationString(t *testing.T) {
	t.Parallel()
	tool := NewSleepTool()

	tests := []struct {
		name     string
		params   SleepParams
		expected time.Duration
	}{
		{"duration 200ms", SleepParams{Duration: "200ms"}, 200 * time.Millisecond},
		{"duration 0.2s", SleepParams{Duration: "0.2s"}, 200 * time.Millisecond},
		{"numeric string 0.05", SleepParams{Duration: "0.05"}, 50 * time.Millisecond},
		{"seconds field", SleepParams{Seconds: 0.15}, 150 * time.Millisecond},
		{"duration with seconds unit", SleepParams{Duration: "1s"}, 1 * time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			start := time.Now()
			resp := runSleepTool(t, tool, ctx, tc.params)
			elapsed := time.Since(start)
			require.False(t, resp.IsError)
			require.GreaterOrEqual(t, elapsed, tc.expected-time.Millisecond*20)
			require.Less(t, elapsed, tc.expected+500*time.Millisecond)
		})
	}
}

func TestSleepTool_Invalid(t *testing.T) {
	t.Parallel()
	tool := NewSleepTool()
	ctx := context.Background()

	// Missing duration
	resp := runSleepTool(t, tool, ctx, SleepParams{})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "missing duration")

	// Zero
	resp = runSleepTool(t, tool, ctx, SleepParams{Seconds: 0, Duration: "0s"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "duration must be > 0")

	// Exceeds max
	resp = runSleepTool(t, tool, ctx, SleepParams{Duration: "301s"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "exceeds maximum")

	// Invalid string
	resp = runSleepTool(t, tool, ctx, SleepParams{Duration: "not-a-duration"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "invalid duration")
}

func TestSleepTool_ContextCancel(t *testing.T) {
	t.Parallel()
	tool := NewSleepTool()
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after 50ms, sleep 5s should abort
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	input, _ := json.Marshal(SleepParams{Duration: "5s"})
	call := fantasy.ToolCall{ID: "test-cancel", Name: SleepToolName, Input: string(input)}
	_, err := tool.Run(ctx, call)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

func TestSleepTool_ParseDuration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		params   SleepParams
		expected time.Duration
		hasErr   bool
	}{
		{SleepParams{Duration: "30s"}, 30 * time.Second, false},
		{SleepParams{Duration: "30"}, 30 * time.Second, false},
		{SleepParams{Duration: "1.5"}, 1500 * time.Millisecond, false},
		{SleepParams{Duration: "  2m  "}, 2 * time.Minute, false},
		{SleepParams{Seconds: 2}, 2 * time.Second, false},
		{SleepParams{Duration: "500ms"}, 500 * time.Millisecond, false},
		{SleepParams{Duration: "1m30s"}, 90 * time.Second, false},
		{SleepParams{}, 0, true},
	}

	for _, tc := range tests {
		d, err := parseSleepDuration(tc.params)
		if tc.hasErr {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, tc.expected, d)
		}
	}
}

func runSleepTool(t *testing.T, tool fantasy.AgentTool, ctx context.Context, params SleepParams) fantasy.ToolResponse {
	t.Helper()
	input, err := json.Marshal(params)
	require.NoError(t, err)
	call := fantasy.ToolCall{
		ID:    "test-call",
		Name:  SleepToolName,
		Input: string(input),
	}
	resp, err := tool.Run(ctx, call)
	require.NoError(t, err)
	return resp
}
