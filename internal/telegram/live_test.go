package telegram

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func liveToken(t *testing.T) string {
	t.Helper()
	token := os.Getenv("SKYNET_TELEGRAM_TEST_TOKEN")
	if token == "" {
		t.Skip("SKYNET_TELEGRAM_TEST_TOKEN not set")
	}
	return token
}

// TestLiveInvalidToken ensures a bogus token is rejected without panicking.
func TestLiveInvalidToken(t *testing.T) {
	bot := NewBot("123456:AAF-invalid-invalid-invalid-invalid")
	require.False(t, bot.TestToken())
}

// TestLiveSendBadChat ensures an API error is surfaced and the token is
// never embedded in the error message.
func TestLiveSendBadChat(t *testing.T) {
	token := liveToken(t)
	bot := NewBot(token)
	bot.chatID = 999999999999 // almost certainly invalid
	err := bot.SendMessage("skynet edge test")
	if err == nil {
		t.Skip("unexpectedly accepted the fake chat id; skipping")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("token leaked in error: %v", err)
	}
	t.Logf("got (sanitized) error: %v", err)
}

// TestLiveTelegram exercises the real Telegram Bot API against a live bot.
// It is skipped unless SKYNET_TELEGRAM_TEST_TOKEN is set, so it never runs
// in CI or leaks a token into the repo.
func TestLiveTelegram(t *testing.T) {
	token := os.Getenv("SKYNET_TELEGRAM_TEST_TOKEN")
	if token == "" {
		t.Skip("SKYNET_TELEGRAM_TEST_TOKEN not set")
	}

	bot := NewBot(token)
	if !bot.TestToken() {
		t.Fatal("TestToken failed for provided token")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	go bot.Start(ctx)

	t.Log("waiting for an incoming message...")
	deadline := time.After(70 * time.Second)
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-deadline:
			t.Logf("no incoming message within window (connected=%v)", bot.Connected())
			return
		case msg := <-bot.Incoming():
			t.Logf("received from chat: %q", msg)
			if err := bot.SendMessage("pong: " + msg); err != nil {
				t.Fatalf("SendMessage failed: %v", err)
			}
			t.Log("reply sent successfully")
			return
		case <-tick.C:
			t.Logf("connected=%v username=%q", bot.Connected(), bot.Username())
		}
	}
}
