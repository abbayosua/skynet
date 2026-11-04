package agent

import (
	"charm.land/fantasy"
)

const (
	// maxReplayToolResultBytes caps a single recent tool result when history
	// is replayed. Large scrape dumps otherwise dominate the request payload
	// and push sessions past the model's context window.
	maxReplayToolResultBytes = 16 * 1024

	// midReplayToolResultBytes is the cap for tool results in the middle of the
	// history.
	midReplayToolResultBytes = 4 * 1024

	// maxReplayOldToolResultBytes is the strictest cap, applied to the oldest
	// tool results. Long sessions accumulate thousands of results; keeping all
	// of them at full size is what pushed requests to multiple times the
	// context window.
	maxReplayOldToolResultBytes = 1024

	// Caps are chosen by ABSOLUTE position (counted from the start of the
	// history), never by distance from the end. A message's position never
	// changes, so its rendering stays byte-identical for the life of the
	// session and the provider prefix cache keeps hitting.
	midToolResultPosition  = 500
	fullToolResultPosition = 2000

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
// truncatedMarker is appended to a tool result that had to be shortened for
// history replay. Keeping it a constant guarantees the rendered bytes are
// stable across requests.
const truncatedMarker = " [truncated: original tool output exceeded the history replay limit]"

// toolResultByteCap returns the replay cap for a tool result at the given
// absolute position in the history (0-based, counted from the start).
func toolResultByteCap(absPos int) int {
	switch {
	case absPos < midToolResultPosition:
		return maxReplayOldToolResultBytes
	case absPos < fullToolResultPosition:
		return midReplayToolResultBytes
	default:
		return maxReplayToolResultBytes
	}
}

// compactToolResultsForReplay shrinks tool results in the replayed history.
// Tool results are the single largest part of long sessions (scraped pages,
// base64 screenshots), so trimming them keeps requests inside the context
// window without touching stored messages.
//
// Decisions depend on absolute position, so a given message always renders the
// same way and only appended messages change the payload. That keeps the
// provider prefix cache intact across turns.
func compactToolResultsForReplay(history []fantasy.Message) []fantasy.Message {
	_, totalMedia := countToolResults(history)

	position := 0
	mediaIndex := 0
	for i := range history {
		if history[i].Role != fantasy.MessageRoleTool {
			continue
		}
		for j := range history[i].Content {
			tr, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](history[i].Content[j])
			if !ok {
				continue
			}
			if media, isMedia := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](tr.Output); isMedia {
				mediaIndex++
				if mediaIndex <= totalMedia-keepRecentMediaParts {
					tr.Output = fantasy.ToolResultOutputContentText{
						Text: "[image omitted from history replay: " + media.MediaType + "]",
					}
					history[i].Content[j] = tr
				}
				position++
				continue
			}
			capBytes := toolResultByteCap(position)
			position++
			text, isText := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](tr.Output)
			if !isText || len(text.Text) <= capBytes {
				continue
			}
			tr.Output = fantasy.ToolResultOutputContentText{
				Text: text.Text[:capBytes] + truncatedMarker,
			}
			history[i].Content[j] = tr
		}
	}
	return history
}

// countToolResults counts tool result parts and media tool results in the
// history.
func countToolResults(history []fantasy.Message) (results, media int) {
	for _, msg := range history {
		if msg.Role != fantasy.MessageRoleTool {
			continue
		}
		for _, part := range msg.Content {
			tr, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part)
			if !ok {
				continue
			}
			results++
			if _, isMedia := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](tr.Output); isMedia {
				media++
			}
		}
	}
	return results, media
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
