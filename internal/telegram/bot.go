// Package telegram provides a Telegram bot client for mirroring agent
// conversations bidirectionally.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// IncomingMessage is a message received from Telegram. It is sent as a
// [tea.Msg] to the Bubble Tea program so the UI can process it.
type IncomingMessage struct {
	Text string
}

const (
	apiBase      = "https://api.telegram.org/bot"
	pollTimeout  = 60
	sendTimeout  = 10 * time.Second
	maxSendRetry = 3
	maxMsgLen    = 4000
)

// Update is a Telegram API update object.
type Update struct {
	UpdateID int64       `json:"update_id"`
	Message  *InMessage  `json:"message,omitempty"`
}

// InMessage is a Telegram message object for incoming messages.
type InMessage struct {
	MessageID int64  `json:"message_id"`
	Chat     *Chat  `json:"chat"`
	Text     string `json:"text"`
}

// Chat is a Telegram chat object.
type Chat struct {
	ID int64 `json:"id"`
}

// SendResponse is the response from the Telegram sendMessage API.
type SendResponse struct {
	OK     bool   `json:"ok"`
	Result *struct {
		MessageID int64 `json:"message_id"`
	} `json:"result,omitempty"`
	Description string `json:"description,omitempty"`
}

// Bot manages a connection to the Telegram Bot API.
type Bot struct {
	token      string
	username   string
	chatID     int64
	client     *http.Client
	longPollClient *http.Client
	baseURL    string
	incoming   chan string
	done       chan struct{}
	mu         sync.Mutex
	lastOffset int64
	running    bool
	shutdown   bool
	connected  bool
}

// NewBot creates a new Telegram bot with the given token.
func NewBot(token string) *Bot {
	return &Bot{
		token:    token,
		client:   &http.Client{Timeout: 15 * time.Second},
		longPollClient: &http.Client{Timeout: pollTimeout + 30*time.Second},
		baseURL:  apiBase,
		incoming: make(chan string, 100),
		done:     make(chan struct{}),
	}
}

// Start begins long-polling for updates. Blocks until the context is
// cancelled. Call in a goroutine.
func (b *Bot) Start(ctx context.Context) {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return
	}
	b.running = true
	b.shutdown = false
	b.connected = false
	b.mu.Unlock()

	slog.Info("Telegram bot: starting long polling")

	// Verify token by calling getMe.
	if err := b.getMe(); err != nil {
		slog.Warn("Telegram bot: token verification failed", "error", err)
		b.mu.Lock()
		b.running = false
		b.mu.Unlock()
		return
	}

	b.mu.Lock()
	b.connected = true
	b.mu.Unlock()
	slog.Info("Telegram bot: connected")

	for {
		select {
		case <-ctx.Done():
			slog.Info("Telegram bot: shutting down")
			b.mu.Lock()
			b.running = false
			b.connected = false
			b.shutdown = true
			b.mu.Unlock()
			return
		default:
		}

		updates, err := b.getUpdates(ctx)
		if err != nil {
			slog.Warn("Telegram bot: getUpdates error", "error", err)
			select {
			case <-ctx.Done():
				continue
			case <-time.After(3 * time.Second):
			}
			continue
		}

		for _, upd := range updates {
			if upd.Message != nil && upd.Message.Text != "" {
				chatID := upd.Message.Chat.ID
				b.mu.Lock()
				// Accept any chat — per requirement "siapa saja boleh ngirim"
				if b.chatID == 0 {
					b.chatID = chatID
					slog.Info("Telegram bot: registered chat", "chat_id", chatID)
				}
				currentChat := b.chatID
				b.mu.Unlock()

				// Only accept from the same chat that first contacted us
				// (still "siapa saja" since the first sender becomes the authorized chat)
				if chatID == currentChat {
					text := strings.TrimSpace(upd.Message.Text)
					if text != "" && !strings.HasPrefix(text, "/") {
						select {
						case b.incoming <- text:
						default:
							slog.Warn("Telegram bot: incoming channel full, dropping message")
						}
					} else if text == "/start" {
						b.SendMessage("Connected! Send me any message and it will be mirrored to the AI agent.")
					}
				}
			}
			b.lastOffset = upd.UpdateID + 1
		}
	}
}

// Stop signals the polling loop to shut down.
func (b *Bot) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.shutdown = true
	b.running = false
	b.connected = false
	select {
	case <-b.done:
	default:
		close(b.done)
	}
}

// SendMessage sends a text message to the registered chat. Returns an error
// if no chat is registered or sending fails.
func (b *Bot) SendMessage(text string) error {
	b.mu.Lock()
	chatID := b.chatID
	shuttingDown := b.shutdown
	b.mu.Unlock()

	if shuttingDown {
		return nil
	}
	if chatID == 0 {
		return fmt.Errorf("no chat registered")
	}

	// Split long messages.
	if len(text) > maxMsgLen {
		parts := splitMessage(text, maxMsgLen)
		for i, part := range parts {
			if err := b.sendMessagePart(chatID, part); err != nil {
				return fmt.Errorf("send part %d/%d: %w", i+1, len(parts), err)
			}
			time.Sleep(100 * time.Millisecond)
		}
		return nil
	}
	return b.sendMessagePart(chatID, text)
}

// Connected returns true if the bot has verified its token and is polling.
func (b *Bot) Connected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connected
}

// Username returns the bot's username as returned by getMe.
// Empty string if not yet connected.
func (b *Bot) Username() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.username
}

// TestToken verifies the bot token by calling getMe. Returns true if valid.
func (b *Bot) TestToken() bool {
	return b.getMe() == nil
}

// Incoming returns a read-only channel of messages received from Telegram.
func (b *Bot) Incoming() <-chan string {
	return b.incoming
}

// --- internal ---

func (b *Bot) apiURL(method string) string {
	return b.baseURL + b.token + "/" + method
}

func (b *Bot) getMe() error {
	resp, err := b.client.Get(b.apiURL("getMe"))
	if err != nil {
		return fmt.Errorf("getMe request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result struct {
		OK     bool `json:"ok"`
		Result *struct {
			ID       int64  `json:"id"`
			IsBot    bool   `json:"is_bot"`
			Username string `json:"username"`
		} `json:"result,omitempty"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("getMe decode failed: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("getMe returned not OK")
	}
	b.mu.Lock()
	if result.Result != nil {
		b.username = result.Result.Username
	}
	b.mu.Unlock()
	return nil
}

func (b *Bot) getUpdates(ctx context.Context) ([]Update, error) {
	params := url.Values{}
	params.Set("timeout", strconv.Itoa(pollTimeout))

	b.mu.Lock()
	offset := b.lastOffset
	b.mu.Unlock()

	if offset > 0 {
		params.Set("offset", strconv.FormatInt(offset, 10))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		b.apiURL("getUpdates")+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := b.longPollClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getUpdates: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("getUpdates read: %w", err)
	}

	var result struct {
		OK     bool     `json:"ok"`
		Result []Update `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("getUpdates decode: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("getUpdates returned not OK")
	}
	return result.Result, nil
}

func (b *Bot) sendMessagePart(chatID int64, text string) error {
	// Strip common markdown syntax and pipe tables so the message
	// is clean readable text when sent with parse_mode disabled.
	safe := stripMarkdown(text)
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       safe,
		"parse_mode": "",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	var lastErr error
	for i := 0; i < maxSendRetry; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			b.apiURL("sendMessage"), bytes.NewReader(body))
		if err != nil {
			cancel()
			return err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := b.client.Do(req)
		cancel()
		if err != nil {
			lastErr = err
			time.Sleep(time.Second)
			continue
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var sr SendResponse
		if err := json.Unmarshal(respBody, &sr); err != nil {
			lastErr = err
			continue
		}
		if !sr.OK {
			lastErr = fmt.Errorf("telegram API error: %s", sr.Description)
			time.Sleep(time.Second)
			continue
		}
		return nil
	}
	return fmt.Errorf("sendMessage failed after %d retries: %w", maxSendRetry, lastErr)
}

func splitMessage(text string, maxLen int) []string {
	var parts []string
	runes := []rune(text)
	for len(runes) > 0 {
		if len(runes) <= maxLen {
			parts = append(parts, string(runes))
			break
		}
		// Try to split at a newline near the boundary.
		splitAt := maxLen
		if idx := strings.LastIndex(string(runes[:maxLen]), "\n"); idx > 0 {
			splitAt = idx + 1
		}
		parts = append(parts, string(runes[:splitAt]))
		runes = runes[splitAt:]
	}
	return parts
}

// stripMarkdown removes common markdown formatting and pipe tables
// from text, leaving clean plain text suitable for Telegram with
// parse_mode disabled.
func stripMarkdown(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	inCodeBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track code blocks — keep content but remove the markers.
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			if !inCodeBlock {
				// End of code block — skip the ``` line.
				continue
			}
			// Skip the opening ``` line.
			continue
		}

		// Inside code block — keep content as-is.
		if inCodeBlock {
			out = append(out, line)
			continue
		}

		// Skip pipe-table separator lines: |---|---|, |:---|---| etc.
		if isTableSeparator(trimmed) {
			continue
		}

		// Convert pipe-table data rows: | A | B | → A — B
		if isTableRow(trimmed) {
			inner := trimmed[1 : len(trimmed)-1]
			parts := strings.Split(inner, "|")
			for i, p := range parts {
				parts[i] = strings.TrimSpace(p)
			}
			line = strings.Join(parts, " — ")
		}

		// Clean up common markdown formatting.
		line = strings.ReplaceAll(line, "**", "")
		line = strings.ReplaceAll(line, "__", "")
		line = strings.ReplaceAll(line, "```", "")

		out = append(out, line)
	}

	return strings.TrimSpace(strings.Join(out, "\n"))
}

// isTableSeparator returns true if the line is a pipe-table separator
// like |---|---| or |:---|---|.
func isTableSeparator(line string) bool {
	if !strings.HasPrefix(line, "|") {
		return false
	}
	noPipe := strings.ReplaceAll(line, "|", "")
	noDash := strings.ReplaceAll(noPipe, "-", "")
	noColon := strings.ReplaceAll(noDash, ":", "")
	noSpace := strings.ReplaceAll(noColon, " ", "")
	return noSpace == ""
}

// isTableRow returns true if the line starts and ends with | (a table row).
func isTableRow(line string) bool {
	return strings.HasPrefix(line, "|") && strings.HasSuffix(line, "|")
}
