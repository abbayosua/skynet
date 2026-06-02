package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
