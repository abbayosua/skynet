package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/abbayosua/skynet/internal/ui/common"
	"github.com/abbayosua/skynet/internal/ui/styles"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pasteMsg(content string) tea.Msg {
	return tea.PasteMsg{Content: content}
}

func newTestTelegramDialog(t *testing.T) *TelegramConnect {
	t.Helper()
	st := styles.CharmtonePantera()
	dia, _ := NewTelegramConnect(&common.Common{Styles: &st}, "test-session")
	return dia
}

// Telegram bot tokens are copied from BotFather, so paste is the normal way to
// enter one. Regression: the dialog accepted KeyPressMsg but had no PasteMsg
// case, so pasting silently did nothing.
func TestTelegramConnect_PasteFillsInput(t *testing.T) {
	t.Parallel()

	dia := newTestTelegramDialog(t)

	dia.HandleMsg(pasteMsg("123456789:AAF-abcdefghijklmnopqrstuvwxyz012345"))
	assert.Equal(t, "123456789:AAF-abcdefghijklmnopqrstuvwxyz012345", dia.input.Value())
}

func TestTelegramConnect_PasteThenSubmit(t *testing.T) {
	t.Parallel()

	dia := newTestTelegramDialog(t)
	dia.HandleMsg(pasteMsg("  123456789:AAF-token  "))

	action := dia.HandleMsg(enterKey())
	_, ok := action.(ActionCmd)
	require.True(t, ok, "submitting a pasted token should start verification, got %T", action)
	assert.Equal(t, TelegramStateVerifying, dia.state)
}

func TestTelegramConnect_PasteIgnoredAfterSubmit(t *testing.T) {
	t.Parallel()

	dia := newTestTelegramDialog(t)
	dia.HandleMsg(pasteMsg("123456789:AAF-first"))
	dia.HandleMsg(enterKey())
	require.Equal(t, TelegramStateVerifying, dia.state)

	dia.HandleMsg(pasteMsg("123456789:AAF-second"))
	assert.Equal(t, "123456789:AAF-first", dia.input.Value(),
		"paste must not mutate the input while the token is being verified")
}

func recallKey() tea.Msg {
	return tea.KeyPressMsg{Code: tea.KeyUp}
}

func TestTelegramConnect_RecallLastToken(t *testing.T) {
	t.Parallel()

	dia := newTestTelegramDialog(t)
	dia.lastToken = "123456789:AAF-saved"
	dia.HandleMsg(recallKey())
	assert.Equal(t, "123456789:AAF-saved", dia.input.Value())
}

func TestTelegramConnect_RecallKeepsTypedInput(t *testing.T) {
	t.Parallel()

	dia := newTestTelegramDialog(t)
	dia.lastToken = "123456789:AAF-saved"
	dia.HandleMsg(pasteMsg("123:typed"))
	dia.HandleMsg(recallKey())
	assert.Equal(t, "123:typed", dia.input.Value(),
		"recall must not overwrite text the user already typed")
}

func TestTelegramConnect_RecallWithoutSaved(t *testing.T) {
	t.Parallel()

	dia := newTestTelegramDialog(t)
	require.Empty(t, dia.lastToken)
	dia.HandleMsg(recallKey())
	assert.Empty(t, dia.input.Value())
}

// Every other dialog with a text input handles PasteMsg too; this asserts the
// ones a user typically pastes into stay wired up.
func TestDialogsAcceptPaste(t *testing.T) {
	t.Parallel()

	st := styles.CharmtonePantera()
	com := &common.Common{Styles: &st}
	msg := pasteMsg("hello")

	t.Run("save command", func(t *testing.T) {
		t.Parallel()
		dia, _ := NewSaveCommand(com)
		dia.HandleMsg(msg)
		assert.Equal(t, "hello", dia.input.Value())
	})

	t.Run("auto compact", func(t *testing.T) {
		t.Parallel()
		dia, _ := NewAutoCompact(com, 0)
		dia.HandleMsg(msg)
		assert.Equal(t, "hello", dia.input.Value())
	})

	t.Run("autopilot goal", func(t *testing.T) {
		t.Parallel()
		dia, _ := NewAutopilot(com)
		dia.HandleMsg(msg)
		assert.Equal(t, "hello", dia.goalInput.Value())
	})

	t.Run("edit answer short prompt", func(t *testing.T) {
		t.Parallel()
		dia, _ := NewEditAnswerShortPrompt(com, "current")
		dia.HandleMsg(msg)
		assert.Contains(t, dia.area.Value(), "hello")
	})
}
