package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/abbayosua/skynet/internal/ui/common"
	"github.com/abbayosua/skynet/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

func newTestSaveCommandDialog(t *testing.T) *SaveCommand {
	t.Helper()
	st := styles.CharmtonePantera()
	dia, _ := NewSaveCommand(&common.Common{Styles: &st})
	return dia
}

func enterKey() tea.Msg {
	return tea.KeyPressMsg{Code: tea.KeyEnter}
}

func escapeKey() tea.Msg {
	return tea.KeyPressMsg{Code: tea.KeyEscape}
}

func typeRunes(s string) tea.Msg {
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func TestSaveCommand_EmptyNameDoesNotSubmit(t *testing.T) {
	t.Parallel()

	dia := newTestSaveCommandDialog(t)
	action := dia.HandleMsg(enterKey())
	require.Nil(t, action, "empty name should not submit")
}

func TestSaveCommand_SubmitReturnsAction(t *testing.T) {
	t.Parallel()

	dia := newTestSaveCommandDialog(t)
	for _, r := range "review-commit" {
		dia.HandleMsg(typeRunes(string(r)))
	}

	action := dia.HandleMsg(enterKey())
	save, ok := action.(ActionSaveCommand)
	require.True(t, ok, "expected ActionSaveCommand, got %T", action)
	require.Equal(t, "review-commit", save.Name)
}

func TestSaveCommand_EscapeCloses(t *testing.T) {
	t.Parallel()

	dia := newTestSaveCommandDialog(t)
	action := dia.HandleMsg(escapeKey())
	_, ok := action.(ActionClose)
	require.True(t, ok, "expected ActionClose, got %T", action)
}

func TestSaveCommand_TrimsName(t *testing.T) {
	t.Parallel()

	dia := newTestSaveCommandDialog(t)
	for _, r := range "check" {
		dia.HandleMsg(typeRunes(string(r)))
	}

	action := dia.HandleMsg(enterKey())
	save, ok := action.(ActionSaveCommand)
	require.True(t, ok)
	require.Equal(t, "check", save.Name)
}
