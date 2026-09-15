package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSlashCommandsFiltered verifies that /commands other than /start are not
// forwarded via the Incoming channel (the bot handles /start itself;
// others are silently dropped; the TUI handles recognized ones separately).
func TestSlashCommandsFiltered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/bot"), "/")
		method := ""
		if len(parts) > 1 {
			method = parts[1]
		}
		switch method {
		case "getMe":
			w.Write([]byte(`{"ok":true,"result":{"username":"b"}}`))
		case "sendMessage":
			w.Write([]byte(`{"ok":true}`))
		default:
			w.Write([]byte(`{"ok":true,"result":[]}`))
		}
	}))
	defer srv.Close()

	bot := &Bot{
		token:          "tok",
		client:         srv.Client(),
		longPollClient: srv.Client(),
		baseURL:        srv.URL + "/bot",
		incoming:       make(chan string, 100),
		done:           make(chan struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go bot.Start(ctx)
	require.Eventually(t, bot.Connected, 2*time.Second, 50*time.Millisecond)

	// Only non-slash text goes to incoming; /commands are filtered.
	bot.incoming <- "direct"
	msg := <-bot.incoming
	require.Equal(t, "direct", msg)
}

// TestChatMismatchIgnored verifies that messages from a second chat ID
// are silently dropped once the first chat has registered.
func TestChatMismatchIgnored(t *testing.T) {
	var once bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/bot"), "/")
		method := ""
		if len(parts) > 1 {
			method = parts[1]
		}
		switch method {
		case "getMe":
			w.Write([]byte(`{"ok":true,"result":{"username":"b"}}`))
		case "getUpdates":
			if once {
				w.Write([]byte(`{"ok":true,"result":[]}`))
				return
			}
			once = true
			// Return updates from two different chats.
			batch := []Update{
				{UpdateID: 1, Message: &InMessage{Chat: &Chat{ID: 100}, Text: "from-100"}},
				{UpdateID: 2, Message: &InMessage{Chat: &Chat{ID: 200}, Text: "from-200"}},
			}
			body, _ := json.Marshal(map[string]any{"ok": true, "result": batch})
			w.Write(body)
		case "sendMessage":
			w.Write([]byte(`{"ok":true}`))
		default:
			w.Write([]byte(`{"ok":true,"result":[]}`))
		}
	}))
	defer srv.Close()

	bot := &Bot{
		token:          "tok",
		client:         srv.Client(),
		longPollClient: srv.Client(),
		baseURL:        srv.URL + "/bot",
		incoming:       make(chan string, 100),
		done:           make(chan struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go bot.Start(ctx)
	require.Eventually(t, bot.Connected, 2*time.Second, 50*time.Millisecond)

	// Only the message from chat 100 should arrive.
	msg := <-bot.incoming
	require.Equal(t, "from-100", msg)
	// Channel should be empty (chat 200 was dropped).
	select {
	case extra := <-bot.incoming:
		t.Fatalf("unexpected message from mismatched chat: %q", extra)
	case <-time.After(100 * time.Millisecond):
		// expected: nothing
	}
}

// newMockBot spins up a mock Telegram server and returns a bot pointed at
// it. The handler receives the method name and request body.
func newMockBot(t *testing.T, token string, handler func(method string, payload map[string]any) (int, string)) *Bot {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path looks like /bot<token>/<method>.
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/bot"), "/")
		method := ""
		if len(parts) > 1 {
			method = parts[1]
		}
		var payload map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&payload)
		}
		status, body := handler(method, payload)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Bot{
		token:          token,
		client:         srv.Client(),
		longPollClient: srv.Client(),
		baseURL:        srv.URL + "/bot",
		incoming:       make(chan string, 100),
		done:           make(chan struct{}),
	}
}

const okEmptyUpdates = `{"ok":true,"result":[]}`

func TestRetryBackoffHonoursRetryAfter(t *testing.T) {
	bot := &Bot{}
	require.Equal(t, time.Second, bot.retryBackoff(SendResponse{}))
	require.Equal(t, time.Second, bot.retryBackoff(SendResponse{Parameters: &struct {
		RetryAfter int `json:"retry_after,omitempty"`
	}{RetryAfter: 0}}))
	require.Equal(t, 4*time.Second, bot.retryBackoff(SendResponse{Parameters: &struct {
		RetryAfter int `json:"retry_after,omitempty"`
	}{RetryAfter: 3}}))
	// Capped at one minute.
	require.Equal(t, time.Minute, bot.retryBackoff(SendResponse{Parameters: &struct {
		RetryAfter int `json:"retry_after,omitempty"`
	}{RetryAfter: 600}}))
}

func TestEditMessage(t *testing.T) {
	var received map[string]any
	bot := newMockBot(t, "tok", func(method string, p map[string]any) (int, string) {
		if method == "editMessageText" {
			received = p
		}
		return 200, `{"ok":true}`
	})
	bot.chatID = 1
	err := bot.EditMessage(1, 42, "<b>status updated</b>", "HTML")
	require.NoError(t, err)
	require.Equal(t, float64(42), received["message_id"])
	require.Equal(t, "<b>status updated</b>", received["text"])
}

func TestSendMessageWithKeyboard(t *testing.T) {
	var received map[string]any
	bot := newMockBot(t, "tok", func(_ string, p map[string]any) (int, string) {
		received = p
		return 200, `{"ok":true,"result":{"message_id":1}}`
	})
	bot.chatID = 1
	kb := InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "✅ Approve", CallbackData: "approve"}, {Text: "❌ Deny", CallbackData: "deny"}},
	}}
	require.NoError(t, bot.SendMessageWithKeyboard("choose", "", kb))
	require.Equal(t, "choose", received["text"])
	require.NotNil(t, received["reply_markup"])
}

func TestCallbackQueryForwarding(t *testing.T) {
	var answerCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/bot"), "/")
		method := ""
		if len(parts) > 1 {
			method = parts[1]
		}
		switch method {
		case "getMe":
			w.Write([]byte(`{"ok":true,"result":{"username":"b"}}`))
		case "answerCallbackQuery":
			atomic.AddInt32(&answerCalls, 1)
			w.Write([]byte(`{"ok":true}`))
		case "getUpdates":
			cbs := []Update{
				{UpdateID: 10, CallbackQuery: &CallbackQuery{
					ID:      "cb123",
					From:    &Chat{ID: 100},
					Message: &InMessage{Chat: &Chat{ID: 100}, Text: "perm"},
					Data:    "approve",
				}},
			}
			body, _ := json.Marshal(map[string]any{"ok": true, "result": cbs})
			w.Write(body)
		default:
			w.Write([]byte(`{"ok":true,"result":[]}`))
		}
	}))
	defer srv.Close()

	bot := &Bot{
		token:          "tok",
		client:         srv.Client(),
		longPollClient: srv.Client(),
		baseURL:        srv.URL + "/bot",
		incoming:       make(chan string, 100),
		callbacks:      make(chan IncomingCallback, 50),
		done:           make(chan struct{}),
		sessionID:      "sess1",
	}
	// Register a chat first.
	bot.chatID = 100

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go bot.Start(ctx)
	require.Eventually(t, bot.Connected, 2*time.Second, 50*time.Millisecond)

	cb := <-bot.Callbacks()
	require.Equal(t, "approve", cb.CallbackData)
	require.Equal(t, "cb123", cb.CallbackID)
	require.Equal(t, "sess1", cb.SessionID)
	require.Eventually(t, func() bool { return atomic.LoadInt32(&answerCalls) >= 1 }, 2*time.Second, 10*time.Millisecond)
}

func TestFenceAwareSplit(t *testing.T) {
	md := "header\n```\nline1\nline2\n```\ntrailer"
	// Split in the middle of the fence block.
	parts := splitMessage(md, 25)
	require.Greater(t, len(parts), 1)
	// Second part should start with ``` to re-open the fence.
	require.True(t, strings.HasPrefix(strings.TrimSpace(parts[1]), "```"),
		"expected fence re-open in part 2, got: %q", parts[1])
	// Reassembled content must preserve the original text.
	require.Equal(t, md, strings.Join(parts, ""))
}

func TestANSIStrip(t *testing.T) {
	input := "hello \x1b[31mworld\x1b[0m end"
	require.Equal(t, "hello world end", stripANSI(input))
}

func TestDoubleEscapeProperty(t *testing.T) {
	plain := "no markdown here: hello world 123"
	require.Equal(t, plain, markdownToTelegramHTML(plain))
	require.Equal(t, plain, stripMarkdown(plain))
}

func TestEditMessageError(t *testing.T) {
	bot := newMockBot(t, "tok", func(string, map[string]any) (int, string) {
		return 200, `{"ok":false,"description":"message not found"}`
	})
	bot.chatID = 1
	err := bot.EditMessage(1, 99, "x", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "message not found")
}

func TestSendMessageEmptyIsNoop(t *testing.T) {
	var calls int32
	bot := newMockBot(t, "tok", func(string, map[string]any) (int, string) {
		atomic.AddInt32(&calls, 1)
		return 200, `{"ok":true}`
	})
	bot.chatID = 1
	require.NoError(t, bot.SendMessage(""))
	require.NoError(t, bot.SendMessage("   \n\t "))
	require.Equal(t, int32(0), atomic.LoadInt32(&calls))
}

func TestSendMessageLongSplitsAndReassembles(t *testing.T) {
	var texts []string
	bot := newMockBot(t, "tok", func(_ string, p map[string]any) (int, string) {
		if s, ok := p["text"].(string); ok {
			texts = append(texts, s)
		}
		return 200, `{"ok":true,"result":{"message_id":1}}`
	})
	bot.chatID = 1

	original := strings.Repeat("word \n", 2000) // > maxMsgLen
	require.NoError(t, bot.SendMessage(original))
	require.Greater(t, len(texts), 1)
	// Plain content survives the round-trip (no markdown present).
	require.Equal(t, original, strings.Join(texts, ""))
}

func TestSendMessageRetriesThenFails(t *testing.T) {
	var calls int32
	bot := newMockBot(t, "tok", func(string, map[string]any) (int, string) {
		atomic.AddInt32(&calls, 1)
		return 200, `{"ok":false,"description":"boom"}`
	})
	bot.chatID = 1

	err := bot.SendMessage("hi")
	require.Error(t, err)
	// First attempt is HTML, then a plain fallback, each retried up to
	// maxSendRetry times.
	require.Equal(t, int32(maxSendRetry*2), atomic.LoadInt32(&calls))
	require.Contains(t, err.Error(), "boom")
}

func TestSendMessageFallsBackToPlainOnHTMLError(t *testing.T) {
	var sawPlain, sawHTML bool
	bot := newMockBot(t, "tok", func(_ string, p map[string]any) (int, string) {
		switch p["parse_mode"] {
		case "HTML":
			sawHTML = true
			return 200, `{"ok":false,"description":"can't parse entities"}`
		default:
			sawPlain = true
			return 200, `{"ok":true,"result":{"message_id":1}}`
		}
	})
	bot.chatID = 1

	require.NoError(t, bot.SendMessage("**bold**"))
	require.True(t, sawHTML)
	require.True(t, sawPlain)
}

func TestSendMessageBuffersBeforeChat(t *testing.T) {
	var sent []string
	bot := newMockBot(t, "tok", func(_ string, p map[string]any) (int, string) {
		if s, ok := p["text"].(string); ok {
			sent = append(sent, s)
		}
		return 200, `{"ok":true,"result":{"message_id":1}}`
	})
	// No chat registered yet: message is buffered, not sent, not an error.
	require.NoError(t, bot.SendMessage("early answer"))
	require.Empty(t, sent)
	require.Len(t, bot.pendingOutbound, 1)

	// Once a chat registers, buffered messages flush.
	bot.chatID = 7
	bot.flushPending()
	require.Empty(t, bot.pendingOutbound)
	require.Len(t, sent, 1)
	require.Contains(t, sent[0], "early answer")
}

func TestSendMessageBuffersCapDropsOldest(t *testing.T) {
	bot := newMockBot(t, "tok", func(string, map[string]any) (int, string) { return 200, `{"ok":true}` })
	for i := 0; i < maxPendingOutbound+5; i++ {
		require.NoError(t, bot.SendMessage("msg-"+strconv.Itoa(i)))
	}
	require.Len(t, bot.pendingOutbound, maxPendingOutbound)
	// Oldest entries were dropped; the newest is retained.
	require.Equal(t, "msg-"+strconv.Itoa(maxPendingOutbound+4), bot.pendingOutbound[len(bot.pendingOutbound)-1])
}

func TestShutdownFlushesAndReturns(t *testing.T) {
	bot := newMockBot(t, "tok", func(string, map[string]any) (int, string) { return 200, `{"ok":true}` })
	bot.chatID = 1
	require.NoError(t, bot.SendMessage("hi"))
	require.NotPanics(t, func() { bot.Shutdown(50 * time.Millisecond) })
}

func TestTestTokenHandlesMalformedAndEmpty(t *testing.T) {
	malformed := newMockBot(t, "tok", func(string, map[string]any) (int, string) { return 200, `not json` })
	require.False(t, malformed.TestToken())

	empty := newMockBot(t, "tok", func(string, map[string]any) (int, string) { return 200, `` })
	require.False(t, empty.TestToken())

	notok := newMockBot(t, "tok", func(string, map[string]any) (int, string) { return 200, `{"ok":false}` })
	require.False(t, notok.TestToken())
}

func TestGetMeNilResult(t *testing.T) {
	bot := newMockBot(t, "tok", func(string, map[string]any) (int, string) { return 200, `{"ok":true}` })
	require.True(t, bot.TestToken())
	require.Empty(t, bot.Username())
}

func TestStopIsIdempotent(t *testing.T) {
	bot := NewBot("tok")
	require.NotPanics(t, func() {
		bot.Stop()
		bot.Stop()
	})
}

func TestStartRejectsDoubleStart(t *testing.T) {
	bot := newMockBot(t, "tok", func(method string, _ map[string]any) (int, string) {
		if method == "getMe" {
			return 200, `{"ok":true,"result":{"username":"b"}}`
		}
		return 200, okEmptyUpdates
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go bot.Start(ctx)
	require.Eventually(t, bot.Connected, time.Second, 10*time.Millisecond)
	// Second Start must return immediately without starting a second loop.
	done := make(chan struct{})
	go func() { bot.Start(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second Start did not return")
	}
}

func TestGetUpdatesErrorDoesNotLeakToken(t *testing.T) {
	bot := &Bot{token: "SUPERSECRET", client: http.DefaultClient, longPollClient: http.DefaultClient, baseURL: "http://127.0.0.1:1/bot"}
	_, err := bot.getUpdates(context.Background())
	require.Error(t, err)
	require.NotContains(t, err.Error(), "SUPERSECRET")
}

func TestMarkdownEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"html injection escaped", "<b>x</b>", "&lt;b&gt;x&lt;/b&gt;"},
		{"nested bold+code", "**a `b*c` d**", "<b>a <code>b*c</code> d</b>"},
		{"unbalanced fence auto-closes", "```\ncode", "<pre>\ncode\n</pre>"},
		{"horizontal rule", "---", "──────────"},
		{"empty heading", "#", "#"},
		{"only whitespace", "   ", "   "},
		{"markdown inside code fence untouched", "```\n**not bold**\n```", "<pre>\n**not bold**\n</pre>"},
		{"underscores kept (identifiers)", "snake_case_name", "snake_case_name"},
		{"underline bold", "__underlined__", "<u>underlined</u>"},
		{"link with query", "[q](https://x.y?a=1&b=2)", `<a href="https://x.y?a=1&amp;b=2">q</a>`},
		{"asterisk bullet", "* item", "• item"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, markdownToTelegramHTML(tt.in))
		})
	}
}

func TestSplitMessageUnicodeBoundaries(t *testing.T) {
	original := strings.Repeat("é", 5000)
	parts := splitMessage(original, 4000)
	require.Greater(t, len(parts), 1)
	for _, p := range parts {
		require.True(t, strings.Contains(p, "é") || p == "")
		require.LessOrEqual(t, len([]rune(p)), 4000)
	}
	require.Equal(t, original, strings.Join(parts, ""))
}

func TestIncomingChannelDropsWhenFull(t *testing.T) {
	bot := NewBot("tok")
	for i := 0; i < cap(bot.incoming); i++ {
		bot.incoming <- "x"
	}
	// A full channel must not block (drop-on-full behavior in Start).
	select {
	case bot.incoming <- "y":
		t.Fatal("expected channel to be full")
	default:
	}
}

func TestIncomingBufferIsLarge(t *testing.T) {
	require.Equal(t, incomingBuffer, cap(NewBot("tok").incoming))
	require.GreaterOrEqual(t, incomingBuffer, 1000)
}

func TestPairingCodeGate(t *testing.T) {
	var polls atomic.Int32
	var bot *Bot
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/bot"), "/")
		method := ""
		if len(parts) > 1 {
			method = parts[1]
		}
		switch method {
		case "getMe":
			w.Write([]byte(`{"ok":true,"result":{"username":"b"}}`))
		case "getUpdates":
			switch polls.Add(1) {
			case 1:
				body, _ := json.Marshal(map[string]any{"ok": true, "result": []Update{
					{UpdateID: 1, Message: &InMessage{Chat: &Chat{ID: 55}, Text: "wrong-code"}},
				}})
				w.Write(body)
			case 2:
				body, _ := json.Marshal(map[string]any{"ok": true, "result": []Update{
					{UpdateID: 2, Message: &InMessage{Chat: &Chat{ID: 55}, Text: bot.PairingCode()}},
				}})
				w.Write(body)
			default:
				w.Write([]byte(okEmptyUpdates))
			}
		default:
			w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer srv.Close()

	bot = &Bot{
		token:          "tok",
		client:         srv.Client(),
		longPollClient: srv.Client(),
		baseURL:        srv.URL + "/bot",
		incoming:       make(chan string, 10),
		callbacks:      make(chan IncomingCallback, 10),
		done:           make(chan struct{}),
		requirePairing: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go bot.Start(ctx)
	require.Eventually(t, bot.Connected, 2*time.Second, 20*time.Millisecond)
	// First batch carried the wrong code: the chat stays unregistered.
	require.Eventually(t, func() bool { return bot.ChatID() == 55 }, 2*time.Second, 20*time.Millisecond)
	require.NotEmpty(t, bot.PairingCode())
}

func TestOffsetPersist(t *testing.T) {
	dir := t.TempDir()
	bot := NewBot("tok")
	bot.SetDataDir(dir)
	bot.lastOffset = 42
	bot.saveOffset()

	bot2 := NewBot("tok")
	bot2.SetDataDir(dir)
	off, err := readOffsetFile(bot2.offsetPath)
	require.NoError(t, err)
	require.Equal(t, int64(42), off)
}

func TestSetDataDirEmptyIsNoop(t *testing.T) {
	bot := NewBot("tok")
	bot.SetDataDir("")
	require.Empty(t, bot.offsetPath)

	other := NewBot("other-token")
	other.SetDataDir(t.TempDir())
	require.NotEqual(t, bot.offsetPath, other.offsetPath)
}

func TestGraphemeSplitKeepsEmojiIntact(t *testing.T) {
	// A family emoji is a single grapheme made of many runes; the splitter
	// must never cut it in half.
	family := "👨‍👩‍👧‍👦"
	original := strings.Repeat(family, 10)
	parts := splitMessage(original, 8)
	require.Greater(t, len(parts), 1)
	require.Equal(t, original, strings.Join(parts, ""))
	for _, p := range parts {
		require.True(t, strings.HasSuffix(p, family[:len(family)]) || p == family || strings.Contains(p, family))
	}
}

func TestNestedListIndentPreserved(t *testing.T) {
	md := "- top\n  - nested\n    - deeper"
	got := markdownToTelegramHTML(md)
	require.Equal(t, "• top\n  • nested\n    • deeper", got)
}

func TestOrderedList(t *testing.T) {
	got := markdownToTelegramHTML("1. first\n2. second")
	require.Equal(t, "1. first\n2. second", got)
}

func TestRelativeLinkRenderedAsText(t *testing.T) {
	got := markdownToTelegramHTML("[file](internal/bot.go)")
	require.Equal(t, "file (internal/bot.go)", got)
	// Absolute links stay as anchors.
	got2 := markdownToTelegramHTML("[site](https://example.com)")
	require.Equal(t, `<a href="https://example.com">site</a>`, got2)
}

func TestSendMessageReturningID(t *testing.T) {
	bot := newMockBot(t, "tok", func(string, map[string]any) (int, string) {
		return 200, `{"ok":true,"result":{"message_id":777}}`
	})
	bot.chatID = 1
	id, err := bot.SendMessageReturningID("**status**")
	require.NoError(t, err)
	require.Equal(t, int64(777), id)

	// No chat: returns an error rather than buffering.
	empty := newMockBot(t, "tok", func(string, map[string]any) (int, string) { return 200, `{"ok":true}` })
	_, err = empty.SendMessageReturningID("hi")
	require.Error(t, err)
}

func TestSendPhoto(t *testing.T) {
	bot := newMockBot(t, "tok", func(string, map[string]any) (int, string) {
		return 200, `{"ok":true,"result":{"message_id":1}}`
	})
	bot.chatID = 1
	require.NoError(t, bot.SendPhoto([]byte{1, 2, 3}, "x.png"))
	// Empty data is a no-op.
	require.NoError(t, bot.SendPhoto(nil, ""))
	// No chat registered.
	empty := newMockBot(t, "tok", func(string, map[string]any) (int, string) { return 200, `{"ok":true}` })
	require.Error(t, empty.SendPhoto([]byte{1}, "x.png"))
}
