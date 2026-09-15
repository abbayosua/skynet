package app

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTelegramSessionSetLastWindow(t *testing.T) {
	s := &telegramSession{}

	require.True(t, s.setLast("msg1", "same"))
	require.False(t, s.setLast("msg1", "same")) // same message ID
	require.False(t, s.setLast("", "same"))     // duplicate text within window
	require.True(t, s.setLast("", "other"))

	// An identical answer produced later (outside the window) is delivered.
	s.lastMu.Lock()
	s.last = "same"
	s.lastAt = time.Now().Add(-10 * time.Second)
	s.lastMu.Unlock()
	require.True(t, s.setLast("msg2", "same"))
}

func TestTelegramSessionRegistry(t *testing.T) {
	app := &App{}

	require.Error(t, app.StartTelegramBot("", "token"))
	require.False(t, app.TelegramBotActive("s1"))
	require.Error(t, app.SendTelegramMessage(context.Background(), "s1", "hi"))
	app.RetargetTelegramBot("s1") // no-op, must not panic
	app.StopTelegramBot("s1")     // no-op, must not panic
}

func TestFullTelegramText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "normal text",
			text: "Hello, this is a response.",
			want: "🤖 Hello, this is a response.",
		},
		{
			name: "long text no truncation",
			text: "This is a very long response that should be sent in full to Telegram without any truncation or summarization whatsoever. The user specifically requested full responses.",
			want: "🤖 This is a very long response that should be sent in full to Telegram without any truncation or summarization whatsoever. The user specifically requested full responses.",
		},
		{
			name: "mulitiline text",
			text: "Line one\nLine two\nLine three",
			want: "🤖 Line one\nLine two\nLine three",
		},
		{
			name: "empty text",
			text: "",
			want: "(no text output)",
		},
		{
			name: "whitespace only",
			text: "   \n  \t  ",
			want: "(no text output)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fullTelegramText(tt.text)
			require.Equal(t, tt.want, got)
		})
	}
}
