package agent

import (
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

func toolResultMessages(n int, size int) []fantasy.Message {
	msgs := make([]fantasy.Message, 0, n)
	for i := 0; i < n; i++ {
		msgs = append(msgs, fantasy.Message{
			Role: fantasy.MessageRoleTool,
			Content: []fantasy.MessagePart{
				fantasy.ToolResultPart{
					ToolCallID: "call_" + strings.Repeat("x", 3),
					Output: fantasy.ToolResultOutputContentText{
						Text: strings.Repeat("a", size),
					},
				},
			},
		})
	}
	return msgs
}

func renderedToolTexts(history []fantasy.Message) []string {
	out := make([]string, 0, len(history))
	for _, msg := range history {
		for _, part := range msg.Content {
			tr, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part)
			if !ok {
				continue
			}
			text, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](tr.Output)
			if !ok {
				out = append(out, "<media>")
				continue
			}
			out = append(out, text.Text)
		}
	}
	return out
}

func TestCompactToolResultsForReplay_CapsByAbsolutePosition(t *testing.T) {
	t.Parallel()

	require.Equal(t, maxReplayOldToolResultBytes, toolResultByteCap(0))
	require.Equal(t, maxReplayOldToolResultBytes, toolResultByteCap(midToolResultPosition-1))
	require.Equal(t, midReplayToolResultBytes, toolResultByteCap(midToolResultPosition))
	require.Equal(t, midReplayToolResultBytes, toolResultByteCap(fullToolResultPosition-1))
	require.Equal(t, maxReplayToolResultBytes, toolResultByteCap(fullToolResultPosition))
}

func TestCompactToolResultsForReplay_StableWhenAppending(t *testing.T) {
	t.Parallel()

	history := toolResultMessages(30, 4000)
	compacted := compactToolResultsForReplay(history)
	before := renderedToolTexts(compacted)
	require.Len(t, before, 30)

	// Every rendered result must respect the cap for its absolute position and
	// stay byte-identical when the history grows.
	for i, text := range before {
		require.LessOrEqual(t, len(text), maxReplayOldToolResultBytes+len(truncatedMarker),
			"result %d exceeds the cap", i)
	}

	grown := append(compacted, fantasy.NewUserMessage("next turn"))
	again := compactToolResultsForReplay(grown)
	after := renderedToolTexts(again)

	require.Equal(t, before, after[:len(before)],
		"existing history must render byte-identically after appending")
}

func TestCompactToolResultsForReplay_Idempotent(t *testing.T) {
	t.Parallel()

	history := toolResultMessages(5, 8000)
	first := renderedToolTexts(compactToolResultsForReplay(history))
	second := renderedToolTexts(compactToolResultsForReplay(history))
	require.Equal(t, first, second, "compaction must be deterministic")
}

func TestCompactToolResultsForReplay_KeepsRecentMedia(t *testing.T) {
	t.Parallel()

	media := fantasy.ToolResultOutputContentMedia{Data: strings.Repeat("B", 2048), MediaType: "image/png"}
	history := make([]fantasy.Message, 0, 4)
	for i := 0; i < 4; i++ {
		history = append(history, fantasy.Message{
			Role: fantasy.MessageRoleTool,
			Content: []fantasy.MessagePart{
				fantasy.ToolResultPart{ToolCallID: "call_media", Output: media},
			},
		})
	}

	compacted := compactToolResultsForReplay(history)
	kept := 0
	for _, msg := range compacted {
		for _, part := range msg.Content {
			tr, _ := fantasy.AsMessagePart[fantasy.ToolResultPart](part)
			if _, isMedia := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](tr.Output); isMedia {
				kept++
			}
		}
	}
	require.Equal(t, keepRecentMediaParts, kept, "only the most recent media should survive")
}

func TestEstimateMessagesTokens_GrowsWithPayload(t *testing.T) {
	t.Parallel()

	small := []fantasy.Message{fantasy.NewUserMessage("hello")}
	large := []fantasy.Message{fantasy.NewUserMessage(strings.Repeat("x", 30000))}

	require.Positive(t, estimateMessagesTokens(small))
	require.Greater(t, estimateMessagesTokens(large), estimateMessagesTokens(small))
}

func TestEnforceHistoryBudget_ShrinksOversizedHistory(t *testing.T) {
	t.Parallel()

	history := toolResultMessages(5, 30000)
	budget := int64(2000)
	out := enforceHistoryBudget(history, budget)

	require.LessOrEqual(t, estimateMessagesTokens(out), budget)
	require.Len(t, out, len(history), "messages must never be dropped, only shortened")
}
