package app

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/abbayosua/skynet/internal/session"
	"github.com/abbayosua/skynet/internal/telegram"
	"github.com/stretchr/testify/require"
)

func TestRetargetedWithin(t *testing.T) {
	s := &telegramSession{}
	require.False(t, s.retargetedWithin(time.Second))
	s.setTarget("a")
	require.True(t, s.retargetedWithin(time.Minute))
	// A retarget clears the streaming status message.
	s.streamMsgID = 42
	s.setTarget("b")
	require.Zero(t, s.streamMsgID)
}

func TestShortSessionID(t *testing.T) {
	require.Equal(t, "abc", shortSessionID("abc"))
	require.Equal(t, "12345678", shortSessionID("1234567890"))
}

func TestTelegramToggles(t *testing.T) {
	s := &telegramSession{sessionID: "s1", bot: telegram.NewBot("tok")}
	app := &App{telegramBot: s}

	require.True(t, app.TelegramSetVerbose("s1", true))
	require.True(t, s.verbose.Load())
	require.True(t, app.TelegramSetThinking("s1", true))
	require.True(t, s.thinking.Load())
	require.True(t, app.TelegramSetStream("s1", true))
	require.True(t, s.stream.Load())
	require.True(t, app.TelegramSetSubagents("s1", true))
	require.True(t, s.subagents.Load())

	// Wrong session: not applied.
	require.False(t, app.TelegramSetVerbose("other", true))
}

func TestFormatTodos(t *testing.T) {
	got := formatTodos([]session.Todo{
		{Content: "a", Status: session.TodoStatusCompleted},
		{Content: "b", Status: session.TodoStatusInProgress, ActiveForm: "doing b"},
		{Content: "c", Status: session.TodoStatusPending},
	})
	require.Contains(t, got, "✅ a")
	require.Contains(t, got, "▶ doing b")
	require.Contains(t, got, "⬜ c")
}

// TestTelegramSessionConcurrentRetargetAndSend exercises the locking around
// target switching, send serialisation and streaming updates. Run with -race.
func TestTelegramSessionConcurrentRetargetAndSend(t *testing.T) {
	s := &telegramSession{bot: telegram.NewBot("tok")}
	s.stream.Store(true)

	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(3)
		go func(i int) {
			defer wg.Done()
			s.setTarget("sess-" + strconv.Itoa(i))
		}(i)
		go func() {
			defer wg.Done()
			_ = s.send("hello")
		}()
		go func() {
			defer wg.Done()
			s.streamUpdate("partial text")
		}()
	}
	wg.Wait()
}

func TestStreamUpdateDisabledIsNoop(t *testing.T) {
	s := &telegramSession{bot: telegram.NewBot("tok")}
	// stream disabled by default
	require.NotPanics(t, func() { s.streamUpdate("x") })
	require.Zero(t, s.streamMsgID)
}
