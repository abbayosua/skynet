package agent

import (
	"charm.land/fantasy"
)

const (
	// maxReplayToolResultBytes caps a single recent tool result when history
	// is replayed. Large scrape dumps otherwise dominate the request payload
	// and push sessions past the model's context window.
	maxReplayToolResultBytes = 16 * 1024

	// maxReplayOldToolResultBytes is the stricter cap applied to tool results
	// that are further back in the history. Long sessions accumulate thousands
	// of results; keeping all of them at full size is what pushed requests to
	// multiple times the context window.
	maxReplayOldToolResultBytes = 1024

	// recentFullToolResults is how many trailing tool results keep the larger
	// cap. Everything older is trimmed to maxReplayOldToolResultBytes.
	recentFullToolResults = 20

	// keepRecentMediaParts is how many trailing media tool results keep their
	// binary payload. Older ones are replaced with a short text placeholder so
	// base64 screenshots stop inflating every request.
	keepRecentMediaParts = 2

	// bytesPerToken is a deliberately conservative character-per-token
	// estimate. Base64 media and code tokenize closer to 2-3 characters per
	// token, and underestimating means the provider still rejects the request
	// even after trimming.
	bytesPerToken = 3
)

// compactToolResultsForReplay shrinks tool results in the replayed history.
// Tool results are the single largest part of long sessions (scraped pages,
// base64 screenshots), so trimming them keeps requests inside the context
// window without touching stored messages.
func compactToolResultsForReplay(history []fantasy.Message) []fantasy.Message {
	mediaKept := 0
	toolResultsSeen := 0
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != fantasy.MessageRoleTool {
			continue
		}
		for j := len(history[i].Content) - 1; j >= 0; j-- {
			tr, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](history[i].Content[j])
			if !ok {
				continue
			}
			toolResultsSeen++
			limit := maxReplayOldToolResultBytes
			if toolResultsSeen <= recentFullToolResults {
				limit = maxReplayToolResultBytes
			}
			if media, isMedia := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](tr.Output); isMedia {
				mediaKept++
				if mediaKept > keepRecentMediaParts {
					tr.Output = fantasy.ToolResultOutputContentText{
						Text: "[image omitted from history replay: " + media.MediaType + "]",
					}
					history[i].Content[j] = tr
				}
				continue
			}
			text, isText := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](tr.Output)
			if !isText {
				continue
			}
			if len(text.Text) > limit {
				tr.Output = fantasy.ToolResultOutputContentText{
					Text: text.Text[:limit] +
						"\n\n[truncated: original tool output exceeded the history replay limit]",
				}
				history[i].Content[j] = tr
			}
		}
	}
	return history
}

// enforceHistoryBudget is the last line of defence before a request is sent.
// When the compacted history still exceeds the model's context window, older
// content is progressively blanked out: first tool results, then assistant
// text. Messages themselves are never removed, so tool calls always keep
// their matching results and providers do not reject the request.
func enforceHistoryBudget(history []fantasy.Message, budgetTokens int64) []fantasy.Message {
	if budgetTokens <= 0 {
		return history
	}
	if estimateMessagesTokens(history) <= budgetTokens {
		return history
	}

	const omitted = "[omitted: history trimmed to fit the model context window]"

	for i := range history {
		if estimateMessagesTokens(history) <= budgetTokens {
			return history
		}
		if history[i].Role != fantasy.MessageRoleTool {
			continue
		}
		for j, part := range history[i].Content {
			tr, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part)
			if !ok {
				continue
			}
			if _, isText := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](tr.Output); isText {
				tr.Output = fantasy.ToolResultOutputContentText{Text: omitted}
				history[i].Content[j] = tr
			}
		}
	}

	for i := range history {
		if estimateMessagesTokens(history) <= budgetTokens {
			return history
		}
		if history[i].Role != fantasy.MessageRoleAssistant {
			continue
		}
		for j, part := range history[i].Content {
			text, ok := fantasy.AsMessagePart[fantasy.TextPart](part)
			if !ok || len(text.Text) <= 512 {
				continue
			}
			text.Text = text.Text[:512] + "…" + omitted
			history[i].Content[j] = text
		}
	}

	return history
}

// estimateMessagesTokens estimates the payload size of prepared messages.
// Provider-reported usage only reflects the last successful request, so a
// session that grew large through tool results never trips the compaction
// threshold. Estimating the payload itself closes that gap.
func estimateMessagesTokens(history []fantasy.Message) int64 {
	var bytes int64
	for _, msg := range history {
		for _, part := range msg.Content {
			switch p := part.(type) {
			case fantasy.TextPart:
				bytes += int64(len(p.Text))
			case fantasy.ReasoningPart:
				bytes += int64(len(p.Text))
			case fantasy.FilePart:
				bytes += int64(len(p.Data))
			case fantasy.ToolCallPart:
				bytes += int64(len(p.Input))
			case fantasy.ToolResultPart:
				if text, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](p.Output); ok {
					bytes += int64(len(text.Text))
				}
				if media, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](p.Output); ok {
					bytes += int64(len(media.Data))
				}
			}
		}
	}
	return bytes / bytesPerToken
}
