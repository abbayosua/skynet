package telegram

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSplitMessage(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		maxLen  int
		wantMin int // minimum number of parts expected
	}{
		{
			name:   "short text no split",
			text:   "Hello, world!",
			maxLen: 4000,
			wantMin: 1,
		},
		{
			name:   "long text splits",
			text:   string(runes(5000)),
			maxLen: 4000,
			wantMin: 2,
		},
		{
			name:   "split at newline boundary",
			text:   string(runes(2000)) + "\n" + string(runes(2000)) + "\n" + string(runes(1000)),
			maxLen: 2500,
			wantMin: 2,
		},
		{
			name:   "empty text",
			text:   "",
			maxLen: 4000,
			wantMin: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := splitMessage(tt.text, tt.maxLen)
			require.GreaterOrEqual(t, len(parts), tt.wantMin)

			// Verify each part is within maxLen
			for i, part := range parts {
				require.LessOrEqual(t, len([]rune(part)), tt.maxLen,
					"part %d exceeds max length: %d > %d", i, len([]rune(part)), tt.maxLen)
			}

			// Verify concatenation produces original (for non-empty input)
			if tt.text != "" && len(parts) > 0 {
				concatenated := ""
				for _, p := range parts {
					concatenated += p
				}
				require.Equal(t, tt.text, concatenated)
			}
		})
	}
}

func runes(n int) string {
	r := make([]rune, n)
	for i := range r {
		r[i] = 'a' + rune(i%26)
	}
	return string(r)
}

func TestTestToken(t *testing.T) {
	// Mock server that returns OK for getMe
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/botTestToken123/getMe" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]bool{"ok": true})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	bot := &Bot{
		token:   "TestToken123",
		client:  server.Client(),
		baseURL: server.URL + "/bot",
	}

	require.True(t, bot.TestToken())
}

func TestTestTokenInvalid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":          false,
			"description": "Not Found",
		})
	}))
	defer server.Close()

	bot := &Bot{
		token:   "BadToken",
		client:  server.Client(),
		baseURL: server.URL + "/bot",
	}

	require.False(t, bot.TestToken())
}

func TestSendMessage(t *testing.T) {
	var receivedPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"result": map[string]interface{}{
				"message_id": 123,
			},
		})
	}))
	defer server.Close()

	bot := &Bot{
		token:   "TestToken",
		client:  server.Client(),
		baseURL: server.URL + "/bot",
		chatID:  456,
	}

	err := bot.SendMessage("Hello from test")
	require.NoError(t, err)
	require.Equal(t, float64(456), receivedPayload["chat_id"])
	require.Equal(t, "Hello from test", receivedPayload["text"])
}

func TestSendMessageNoChat(t *testing.T) {
	bot := &Bot{
		token:  "TestToken",
		client: http.DefaultClient,
		chatID: 0,
	}

	err := bot.SendMessage("Hello")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no chat registered")
}

func TestIncomingChannel(t *testing.T) {
	bot := NewBot("test")
	incoming := bot.Incoming()

	// Send to channel
	go func() {
		bot.incoming <- "test message"
	}()

	msg := <-incoming
	require.Equal(t, "test message", msg)
}
