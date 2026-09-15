package message

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllTextConcatenatesTextParts(t *testing.T) {
	m := Message{
		Role: Assistant,
		Parts: []ContentPart{
			ReasoningContent{Thinking: "hmm"},
			TextContent{Text: "first "},
			ToolCall{Name: "bash"},
			TextContent{Text: "second"},
		},
	}
	require.Equal(t, "first \n\nsecond", m.AllText())
	// Content() intentionally returns only the first text part.
	require.Equal(t, "first ", m.Content().String())
}

func TestAllTextEmpty(t *testing.T) {
	m := Message{Role: Assistant, Parts: []ContentPart{ToolCall{Name: "bash"}}}
	require.Equal(t, "", m.AllText())
}
