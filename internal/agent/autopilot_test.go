package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParsePlanSteps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response string
		expected []string
	}{
		{
			name: "markdown checklist",
			response: "Here is the plan:\n" +
				"- [ ] Add timeout field to job_output tool\n" +
				"- [ ] Wrap WaitContext with context.WithTimeout\n" +
				"- [x] Already done step\n",
			expected: []string{
				"Add timeout field to job_output tool",
				"Wrap WaitContext with context.WithTimeout",
				"Already done step",
			},
		},
		{
			name:     "numbered list",
			response: "1. Fix the bug\n2. Write tests\n3. Run build",
			expected: []string{"Fix the bug", "Write tests", "Run build"},
		},
		{
			name:     "no plan found",
			response: "I cannot create a plan for this.",
			expected: nil,
		},
		{
			name:     "empty response",
			response: "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, parsePlanSteps(tt.response, 10))
		})
	}
}

func TestParsePlanSteps_CapsAtMaxSteps(t *testing.T) {
	t.Parallel()

	response := ""
	for i := 0; i < 15; i++ {
		response += "- [ ] Step something\n"
		_ = i
	}
	steps := parsePlanSteps(response, 10)
	require.Len(t, steps, 10)
}

func TestExtractBlockedReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response string
		expected string
	}{
		{
			name:     "with reason on same line",
			response: "Cannot proceed. <autopilot>BLOCKED</autopilot>: missing API key",
			expected: "missing API key",
		},
		{
			name:     "reason without colon",
			response: "<autopilot>BLOCKED tests fail and I cannot fix them</autopilot>",
			expected: "tests fail and I cannot fix them</autopilot>",
		},
		{
			name:     "no reason given",
			response: "stuff <autopilot>BLOCKED</autopilot>",
			expected: "unknown reason",
		},
		{
			name:     "no tag at all",
			response: "just text",
			expected: "unknown reason",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, extractBlockedReason(tt.response))
		})
	}
}
