// Package telegram provides a Telegram bot client for mirroring agent
// conversations bidirectionally.
package telegram

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// IncomingMessage is a message received from Telegram. It is sent as a
// [tea.Msg] to the Bubble Tea program so the UI can process it. SessionID
// binds the message to the session that owns the bot.
type IncomingMessage struct {
	SessionID string
	Text      string
}

const (
	apiBase      = "https://api.telegram.org/bot"
	pollTimeout  = 60
	sendTimeout  = 10 * time.Second
	maxSendRetry = 3
	maxMsgLen    = 3500

	// incomingBuffer caps queued inbound messages before drop-on-full.
	incomingBuffer = 1000
	// maxPendingOutbound caps messages buffered before the first chat.
	maxPendingOutbound = 20
)

// Update is a Telegram API update object.
type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *InMessage     `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

// CallbackQuery represents an inline button press from Telegram.
type CallbackQuery struct {
	ID      string     `json:"id"`
	From    *Chat      `json:"from"` // user who pressed
	Message *InMessage `json:"message,omitempty"`
	Data    string     `json:"data"` // callback_data from the button
}

// IncomingCallback is a callback query received from Telegram via an inline
// button press. Sent as a [tea.Msg] to the Bubble Tea program.
type IncomingCallback struct {
	SessionID    string
	CallbackID   string
	CallbackData string
}

// InMessage is a Telegram message object for incoming messages.
type InMessage struct {
	MessageID int64  `json:"message_id"`
	Chat      *Chat  `json:"chat"`
	Text      string `json:"text"`
}

// Chat is a Telegram chat object.
type Chat struct {
	ID int64 `json:"id"`
}

// SendResponse is the response from the Telegram sendMessage API.
type SendResponse struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description,omitempty"`
	Parameters  *struct {
		RetryAfter int `json:"retry_after,omitempty"`
	} `json:"parameters,omitempty"`
	Result *struct {
		MessageID int64 `json:"message_id"`
	} `json:"result,omitempty"`
}

// Bot manages a connection to the Telegram Bot API.
type Bot struct {
	token          string
	username       string
	chatID         int64
	sessionID      string // set by caller; forwarded with callbacks
	client         *http.Client
	longPollClient *http.Client
	baseURL        string
	incoming       chan string
	callbacks      chan IncomingCallback
	done           chan struct{}
	mu             sync.Mutex
	lastOffset     int64
	running        bool
	shutdown       bool
	connected      bool

	// pendingOutbound buffers messages produced before a chat has
	// contacted the bot (bots cannot DM first). Flushed on first chat.
	pendingOutbound []string

	// pairingCode is derived from the token; when requirePairing is set the
	// first incoming message must equal it before the chat is authorized.
	pairingCode    string
	requirePairing bool

	// offsetPath, when set, persists the long-poll offset across restarts.
	offsetPath string

	// lastIncomingMsgID tracks the last Telegram message ID so that
	// replies can reference it with reply_to_message_id.
	lastIncomingMsgID int64
	lastIncomingMu    sync.Mutex
}

// NewBot creates a new Telegram bot with the given token.
func NewBot(token string) *Bot {
	return &Bot{
		token:          token,
		client:         &http.Client{Timeout: 15 * time.Second},
		longPollClient: &http.Client{Timeout: 90 * time.Second},
		baseURL:        apiBase,
		incoming:       make(chan string, incomingBuffer),
		callbacks:      make(chan IncomingCallback, 50),
		done:           make(chan struct{}),
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
	b.pairingCode = derivePairingCode(b.token)
	if b.offsetPath != "" {
		if off, err := readOffsetFile(b.offsetPath); err == nil && off > 0 {
			b.lastOffset = off
		}
	}
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
			if upd.CallbackQuery != nil && upd.CallbackQuery.Data != "" {
				cb := upd.CallbackQuery
				sessionID := ""
				b.mu.Lock()
				if b.chatID != 0 && cb.Message != nil && cb.Message.Chat.ID == b.chatID {
					sessionID = b.sessionID
				}
				b.mu.Unlock()
				if sessionID != "" {
					select {
					case b.callbacks <- IncomingCallback{
						SessionID:    sessionID,
						CallbackID:   cb.ID,
						CallbackData: cb.Data,
					}:
					default:
						slog.Warn("Telegram bot: callback channel full, dropping")
					}
				}
				b.AnswerCallbackQuery(cb.ID, "")
			}
			if upd.Message != nil && upd.Message.Text != "" {
				text := strings.TrimSpace(upd.Message.Text)
				chatID := upd.Message.Chat.ID

				justPaired := false
				b.mu.Lock()
				// A new chat must be authorized. When pairing is enabled
				// the first message must carry the pairing code; otherwise
				// the first sender wins (convenience default).
				if b.chatID == 0 {
					if b.requirePairing && text != b.pairingCode {
						b.mu.Unlock()
						slog.Info("Telegram bot: ignoring unauthorized chat", "chat_id", chatID)
						b.lastOffset = upd.UpdateID + 1
						continue
					}
					b.chatID = chatID
					justPaired = b.requirePairing
					slog.Info("Telegram bot: registered chat", "chat_id", chatID)
				}
				currentChat := b.chatID
				b.mu.Unlock()

				if chatID != currentChat {
					b.lastOffset = upd.UpdateID + 1
					continue
				}

				b.lastIncomingMu.Lock()
				b.lastIncomingMsgID = upd.Message.MessageID
				b.lastIncomingMu.Unlock()

				// Deliver anything buffered before the first chat arrived.
				b.flushPending()

				switch {
				case justPaired:
					b.SendMessage("✅ Pairing successful. Send a message to begin.")
				case text != "" && !strings.HasPrefix(text, "/"):
					select {
					case b.incoming <- text:
					default:
						slog.Warn("Telegram bot: incoming channel full, dropping message")
					}
				case text == "/start":
					b.SendMessage("✅ Connected! Send me any message and it will be mirrored to the AI agent.")
				}
				// Other /commands are silently dropped by the bot;
				// the TUI handles recognized ones via IncomingMessage.
			}
			b.lastOffset = upd.UpdateID + 1
			b.saveOffset()
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
	if strings.TrimSpace(text) == "" {
		return nil
	}

	b.mu.Lock()
	chatID := b.chatID
	shuttingDown := b.shutdown
	// Before the first chat contacts the bot we cannot send (Telegram
	// forbids bots DMing first). Buffer instead of dropping the answer.
	if chatID == 0 && !shuttingDown {
		if len(b.pendingOutbound) >= maxPendingOutbound {
			b.pendingOutbound = b.pendingOutbound[1:]
		}
		b.pendingOutbound = append(b.pendingOutbound, text)
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	if shuttingDown {
		return nil
	}
	if chatID == 0 {
		return fmt.Errorf("no chat registered")
	}

	// Split the source (markdown) text, then render each chunk to
	// Telegram-safe HTML independently so tags stay balanced per message.
	parts := splitMessage(text, maxMsgLen)
	for i, part := range parts {
		html := markdownToTelegramHTML(part)
		if _, err := b.sendMessagePart(chatID, html, "HTML"); err != nil {
			// Telegram rejects malformed entities: fall back to plain text.
			if _, fbErr := b.sendMessagePart(chatID, stripMarkdown(part), ""); fbErr != nil {
				return fmt.Errorf("send part %d/%d: %w", i+1, len(parts), fbErr)
			}
		}
		if i < len(parts)-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}
	return nil
}

// SendMessageReturningID sends a single text message and returns its
// Telegram message ID. Used to open a status message that is later edited
// in place for streaming updates. The text is rendered to Telegram HTML with
// a plain-text fallback; it is not chunked (callers should keep it short).
func (b *Bot) SendMessageReturningID(text string) (int64, error) {
	b.mu.Lock()
	chatID := b.chatID
	b.mu.Unlock()
	if chatID == 0 {
		return 0, fmt.Errorf("no chat registered")
	}
	if strings.TrimSpace(text) == "" {
		return 0, nil
	}
	id, err := b.sendMessagePart(chatID, markdownToTelegramHTML(text), "HTML")
	if err != nil {
		return b.sendMessagePart(chatID, stripMarkdown(text), "")
	}
	return id, nil
}

// flushPending delivers any messages buffered before the first chat
// contacted the bot. It is a no-op once the buffer is empty.
func (b *Bot) flushPending() {
	b.mu.Lock()
	if b.chatID == 0 || len(b.pendingOutbound) == 0 {
		b.mu.Unlock()
		return
	}
	pending := b.pendingOutbound
	b.pendingOutbound = nil
	b.mu.Unlock()

	for _, text := range pending {
		if err := b.SendMessage(text); err != nil {
			slog.Warn("Telegram bot: failed to flush buffered message", "error", err)
		}
	}
}

// Shutdown stops polling and attempts to flush buffered messages within
// the given timeout. Safe to call multiple times.
func (b *Bot) Shutdown(timeout time.Duration) {
	b.Stop()
	select {
	case <-b.done:
	case <-time.After(timeout):
	}
	b.flushPending()
}

// EditMessage edits a previously sent message. Returns an error if the
// edit fails (e.g. message too old or content unchanged). Useful for
// updating a single "status" message in place instead of sending many.
func (b *Bot) EditMessage(chatID int64, messageID int64, text, parseMode string) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
		"parse_mode": parseMode,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.apiURL("editMessageText"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return b.scrub(err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var sr SendResponse
	if err := json.Unmarshal(respBody, &sr); err != nil {
		return err
	}
	if !sr.OK {
		return fmt.Errorf("editMessageText error: %s", sr.Description)
	}
	return nil
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

// SendMessageWithKeyboard sends a text message with an inline keyboard.
func (b *Bot) SendMessageWithKeyboard(text, parseMode string, keyboard InlineKeyboardMarkup) error {
	b.mu.Lock()
	chatID := b.chatID
	b.mu.Unlock()
	if chatID == 0 {
		return fmt.Errorf("no chat registered")
	}
	payload := map[string]any{
		"chat_id":      chatID,
		"text":         text,
		"parse_mode":   parseMode,
		"reply_markup": keyboard,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	var lastErr error
	for i := 0; i < maxSendRetry; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.apiURL("sendMessage"), bytes.NewReader(body))
		if err != nil {
			cancel()
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := b.client.Do(req)
		cancel()
		if err != nil {
			lastErr = b.scrub(err)
			time.Sleep(b.retryBackoff(SendResponse{}))
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
			time.Sleep(b.retryBackoff(sr))
			continue
		}
		return nil
	}
	return b.scrub(fmt.Errorf("sendMessageWithKeyboard failed after %d retries: %w", maxSendRetry, lastErr))
}

// AnswerCallbackQuery answers a callback query (required after inline button
// press to dismiss the loading state).
func (b *Bot) AnswerCallbackQuery(callbackID, text string) error {
	payload := map[string]any{
		"callback_query_id": callbackID,
		"text":              text,
		"show_alert":        false,
	}
	body, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.apiURL("answerCallbackQuery"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return b.scrub(err)
	}
	defer resp.Body.Close()
	return nil
}

// InlineKeyboardMarkup represents an inline keyboard attached to a message.
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// InlineKeyboardButton represents one button on an inline keyboard.
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

// SetSessionID stores the session ID that this bot is bound to. It is
// forwarded with IncomingCallback so the TUI can route the event correctly.
func (b *Bot) SetSessionID(sessionID string) {
	b.mu.Lock()
	b.sessionID = sessionID
	b.mu.Unlock()
}

// SetTarget updates the session the bot is mirroring, so that callback
// queries carry the correct session ID.
func (b *Bot) SetTarget(sessionID string) {
	b.mu.Lock()
	b.sessionID = sessionID
	b.mu.Unlock()
}

// Callbacks returns a read-only channel of inline button presses.
func (b *Bot) Callbacks() <-chan IncomingCallback {
	return b.callbacks
}

// ChatID returns the registered chat ID (0 if no chat has contacted the bot yet).
func (b *Bot) ChatID() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.chatID
}

// LastIncomingMsgID returns the message ID of the last message received
// from Telegram, so that replies can reference it via reply_to_message_id.
func (b *Bot) LastIncomingMsgID() int64 {
	b.lastIncomingMu.Lock()
	defer b.lastIncomingMu.Unlock()
	return b.lastIncomingMsgID
}

// SetRequirePairing enables or disables pairing-code authorization. When
// enabled, the first message must equal PairingCode() before the chat is
// accepted.
func (b *Bot) SetRequirePairing(v bool) {
	b.mu.Lock()
	b.requirePairing = v
	b.mu.Unlock()
}

// PairingCode returns the pairing code derived from the token. Empty until
// Start has verified the token.
func (b *Bot) PairingCode() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pairingCode
}

// SetDataDir sets the directory used to persist the long-poll offset so
// restarts do not reprocess old updates.
func (b *Bot) SetDataDir(dir string) {
	if dir == "" {
		return
	}
	b.mu.Lock()
	b.offsetPath = filepath.Join(dir, "tg_offset_"+derivePairingCode(b.token))
	b.mu.Unlock()
}

// derivePairingCode returns a short, stable, non-reversible code derived
// from the token. Used both as pairing code and offset-file discriminator.
func derivePairingCode(token string) string {
	sum := sha256.Sum256([]byte(token))
	return strings.ToUpper(hex.EncodeToString(sum[:3]))
}

func readOffsetFile(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
}

// saveOffset persists the current long-poll offset, if a path is configured.
func (b *Bot) saveOffset() {
	b.mu.Lock()
	path := b.offsetPath
	offset := b.lastOffset
	b.mu.Unlock()
	if path == "" || offset <= 0 {
		return
	}
	if err := os.WriteFile(path, []byte(strconv.FormatInt(offset, 10)), 0o600); err != nil {
		slog.Debug("Telegram bot: failed to persist offset", "error", err)
	}
}

// SendPhoto uploads an image to the registered chat. filename is used by
// Telegram for the upload. Empty data is a no-op. Only image payloads
// should be passed by callers.
func (b *Bot) SendPhoto(data []byte, filename string) error {
	if len(data) == 0 {
		return nil
	}
	b.mu.Lock()
	chatID := b.chatID
	b.mu.Unlock()
	if chatID == 0 {
		return fmt.Errorf("no chat registered")
	}
	if filename == "" {
		filename = "image.png"
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("chat_id", strconv.FormatInt(chatID, 10))
	fw, err := mw.CreateFormFile("photo", filename)
	if err != nil {
		return err
	}
	if _, err := fw.Write(data); err != nil {
		return err
	}
	mw.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.apiURL("sendPhoto"), &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := b.client.Do(req)
	if err != nil {
		return b.scrub(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var sr SendResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		return err
	}
	if !sr.OK {
		return fmt.Errorf("sendPhoto error: %s", sr.Description)
	}
	return nil
}

// SendChatAction sends a "typing" chat action so Telegram shows the bot is busy.
func (b *Bot) SendChatAction(chatID int64, action string) error {
	payload := map[string]string{"chat_id": strconv.FormatInt(chatID, 10), "action": action}
	body, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.apiURL("sendChatAction"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return b.scrub(err)
	}
	defer resp.Body.Close()
	return nil
}

// Incoming returns a read-only channel of messages received from Telegram.
func (b *Bot) Incoming() <-chan string {
	return b.incoming
}

// --- internal ---

func (b *Bot) apiURL(method string) string {
	return b.baseURL + b.token + "/" + method
}

// scrub removes the bot token from an error so it is safe to log or surface
// (HTTP client errors embed the request URL, which contains the token).
func (b *Bot) scrub(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(strings.ReplaceAll(err.Error(), b.token, "***"))
}

// retryBackoff returns how long to wait before retrying after a non-OK
// response, honouring Telegram's "retry_after" hint (used for 429s).
func (b *Bot) retryBackoff(sr SendResponse) time.Duration {
	if sr.Parameters != nil && sr.Parameters.RetryAfter > 0 {
		d := time.Duration(sr.Parameters.RetryAfter)*time.Second + time.Second
		if d > time.Minute {
			d = time.Minute
		}
		return d
	}
	return time.Second
}

func (b *Bot) getMe() error {
	resp, err := b.client.Get(b.apiURL("getMe"))
	if err != nil {
		return b.scrub(fmt.Errorf("getMe request failed: %w", err))
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
		return nil, b.scrub(fmt.Errorf("getUpdates: %w", err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("getUpdates read: %w", err)
	}

	var raw struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("getUpdates decode: %w", err)
	}
	if !raw.OK {
		return nil, fmt.Errorf("getUpdates returned not OK")
	}
	var updates []Update
	if err := json.Unmarshal(raw.Result, &updates); err != nil {
		// If the entire batch fails, try per-item parsing.
		var items []json.RawMessage
		if err2 := json.Unmarshal(raw.Result, &items); err2 != nil {
			return nil, fmt.Errorf("getUpdates decode result: %w", err)
		}
		for _, item := range items {
			var u Update
			if err := json.Unmarshal(item, &u); err != nil {
				slog.Warn("Telegram bot: skipping malformed update", "error", err)
				continue
			}
			updates = append(updates, u)
		}
	}
	return updates, nil
}

func (b *Bot) sendMessagePart(chatID int64, text, parseMode string) (int64, error) {
	b.lastIncomingMu.Lock()
	replyTo := b.lastIncomingMsgID
	b.lastIncomingMu.Unlock()

	payload := map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": parseMode,
	}
	if replyTo > 0 {
		payload["reply_parameters"] = map[string]any{"message_id": replyTo}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal: %w", err)
	}

	var lastErr error
	for i := 0; i < maxSendRetry; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			b.apiURL("sendMessage"), bytes.NewReader(body))
		if err != nil {
			cancel()
			return 0, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := b.client.Do(req)
		cancel()
		if err != nil {
			lastErr = b.scrub(err)
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
			time.Sleep(b.retryBackoff(sr))
			continue
		}
		var id int64
		if sr.Result != nil {
			id = sr.Result.MessageID
		}
		return id, nil
	}
	return 0, b.scrub(fmt.Errorf("sendMessage failed after %d retries: %w", maxSendRetry, lastErr))
}

func splitMessage(text string, maxLen int) []string {
	if text == "" {
		return nil
	}
	var parts []string
	var cur []string
	curLen := 0
	for g := uniseg.NewGraphemes(text); g.Next(); {
		cluster := g.Str()
		n := utf8.RuneCountInString(cluster)
		if curLen+n > maxLen && len(cur) > 0 {
			// Prefer to break at the last newline inside the window so
			// lines are not cut mid-way; falls back to a hard break.
			cut := len(cur)
			for i := len(cur) - 1; i >= 0; i-- {
				if cur[i] == "\n" {
					cut = i + 1
					break
				}
			}
			parts = append(parts, strings.Join(cur[:cut], ""))
			rest := cur[cut:]
			cur = append([]string(nil), rest...)
			curLen = 0
			for _, c := range cur {
				curLen += utf8.RuneCountInString(c)
			}
		}
		cur = append(cur, cluster)
		curLen += n
	}
	if len(cur) > 0 {
		parts = append(parts, strings.Join(cur, ""))
	}
	return parts
}

// RenderHTML converts markdown into Telegram-safe HTML using the same
// rules as SendMessage. Exposed for callers that edit messages in place.
func RenderHTML(md string) string {
	return markdownToTelegramHTML(md)
}

// markdownToTelegramHTML converts a chunk of (GitHub-flavoured) Markdown
// into HTML that Telegram accepts with parse_mode=HTML. Only a safe subset
// of formatting is translated; all text is HTML-escaped first so user
// content can never be interpreted as markup.
func markdownToTelegramHTML(md string) string {
	lines := strings.Split(md, "\n")
	out := make([]string, 0, len(lines))
	inFence := false

	for i, raw := range lines {
		if reFence.MatchString(raw) {
			if inFence {
				out = append(out, "</pre>")
				inFence = false
			} else {
				out = append(out, "<pre>")
				inFence = true
			}
			continue
		}

		// Code block content is preserved verbatim (escaped only).
		if inFence {
			out = append(out, escapeHTML(raw))
			continue
		}

		trimmed := strings.TrimSpace(raw)

		// Pipe-table separator rows carry no content.
		if isTableSeparator(trimmed) {
			continue
		}
		// Wide tables (>=4 columns) render as monospace code blocks.
		if isTableRow(trimmed) {
			if reWideTable.MatchString(trimmed) {
				tableLines := []string{raw}
				for j := i + 1; j < len(lines); j++ {
					tl := strings.TrimSpace(lines[j])
					if isTableSeparator(tl) || isTableRow(tl) {
						tableLines = append(tableLines, lines[j])
					} else {
						break
					}
				}
				out = append(out, "<pre>"+escapeHTML(strings.Join(tableLines, "\n"))+"</pre>")
				i += len(tableLines) - 1
				continue
			}
			raw = tableRowToText(trimmed)
			trimmed = strings.TrimSpace(raw)
		}

		switch {
		case isHorizontalRule(trimmed):
			out = append(out, "──────────")
		case reHeading.MatchString(raw):
			heading := reHeading.FindStringSubmatch(raw)[1]
			out = append(out, "<b>"+convertInline(escapeHTML(heading))+"</b>")
		case reBullet.MatchString(raw):
			m := reBullet.FindStringSubmatch(raw)
			out = append(out, m[1]+"• "+convertInline(escapeHTML(m[2])))
		case reOrdered.MatchString(raw):
			m := reOrdered.FindStringSubmatch(raw)
			out = append(out, m[1]+m[2]+". "+convertInline(escapeHTML(m[3])))
		default:
			out = append(out, convertInline(escapeHTML(raw)))
		}
	}

	if inFence {
		out = append(out, "</pre>")
	}
	return strings.Join(out, "\n")
}

// convertInline applies inline Markdown formatting. The input must already
// be HTML-escaped.
func convertInline(s string) string {
	// Protect inline code spans so their contents are not re-parsed.
	var codes []string
	s = reInlineCode.ReplaceAllStringFunc(s, func(m string) string {
		inner := m[1 : len(m)-1]
		codes = append(codes, "<code>"+inner+"</code>")
		return fmt.Sprintf("\x00%d\x00", len(codes)-1)
	})

	s = reLink.ReplaceAllStringFunc(s, func(m string) string {
		sub := reLink.FindStringSubmatch(m)
		label, link := sub[1], sub[2]
		// Telegram only accepts absolute hrefs; relative paths (e.g.
		// internal/file.go) are rendered as plain text instead.
		if isAbsoluteURL(link) {
			return `<a href="` + link + `">` + label + `</a>`
		}
		return label + " (" + link + ")"
	})
	s = reStrike.ReplaceAllString(s, "<s>$1</s>")
	s = reBold.ReplaceAllString(s, "<b>$1</b>")
	s = reUnderline.ReplaceAllString(s, "<u>$1</u>")
	s = reItalic.ReplaceAllString(s, "<i>$1</i>")

	for i, c := range codes {
		s = strings.Replace(s, fmt.Sprintf("\x00%d\x00", i), c, 1)
	}
	return s
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// isAbsoluteURL reports whether a link target can be used as an <a href>.
func isAbsoluteURL(link string) bool {
	for _, prefix := range []string{"http://", "https://", "tg://", "mailto:"} {
		if strings.HasPrefix(link, prefix) {
			return true
		}
	}
	return false
}

func tableRowToText(trimmed string) string {
	inner := strings.TrimSuffix(strings.TrimPrefix(trimmed, "|"), "|")
	parts := strings.Split(inner, "|")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return strings.Join(parts, " — ")
}

func isHorizontalRule(line string) bool {
	if len(line) < 3 {
		return false
	}
	c := line[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	for i := 0; i < len(line); i++ {
		if line[i] != c {
			return false
		}
	}
	return true
}

var (
	reFence      = regexp.MustCompile("^\\s*```")
	reHeading    = regexp.MustCompile(`^#{1,6}\s+(.*)$`)
	reBullet     = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	reOrdered    = regexp.MustCompile(`^(\s*)(\d+)\.\s+(.*)$`)
	reInlineCode = regexp.MustCompile("`([^`\n]+)`")
	reLink       = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	reBold       = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reUnderline  = regexp.MustCompile(`__(.+?)__`)
	reItalic     = regexp.MustCompile(`\*([^*\n]+?)\*`)
	reStrike     = regexp.MustCompile(`~~(.+?)~~`)
	reWideTable  = regexp.MustCompile(`^\|[^|]+\|[^|]+\|[^|]+\|`)
)

// stripANSI removes ANSI escape sequences (SGR codes, cursor movement,
// etc.) from text so they do not corrupt Telegram output.
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
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
