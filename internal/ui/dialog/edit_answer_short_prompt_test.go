package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/abbayosua/skynet/internal/ui/common"
	"github.com/abbayosua/skynet/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

func shiftPressMsg() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyLeftShift, Mod: tea.ModShift}
}

func ctrlDPressMsg() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
}

func escPressMsg() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyEscape}
}

func TestEditAnswerShortPrompt_ClearFlows(t *testing.T) {
	t.Parallel()

	newDialog := func(t *testing.T, value string) *EditAnswerShortPrompt {
		t.Helper()
		st := styles.CharmtonePantera()
		com := &common.Common{Styles: &st}
		dia, _ := NewEditAnswerShortPrompt(com, value)
		return dia
	}

	t.Run("ctrl+d asks then confirms clearing", func(t *testing.T) {
		t.Parallel()
		dia := newDialog(t, "keep me")

		out := dia.HandleMsg(ctrlDPressMsg())
		require.Nil(t, out)
		require.Equal(t, "keep me", dia.area.Value(), "first ctrl+d must not clear")

		out = dia.HandleMsg(ctrlDPressMsg())
		act, ok := out.(ActionEditAnswerShortPrompt)
		require.True(t, ok, "second ctrl+d confirms clearing, got %T", out)
		require.Equal(t, "", act.Prompt)
		require.Equal(t, "", dia.area.Value())
	})

	t.Run("esc cancels clearing", func(t *testing.T) {
		t.Parallel()
		dia := newDialog(t, "keep me")

		require.Nil(t, dia.HandleMsg(ctrlDPressMsg()))
		out := dia.HandleMsg(escPressMsg())
		require.Nil(t, out)
		require.Equal(t, "keep me", dia.area.Value(), "esc must keep the text")
	})

	t.Run("triple shift triggers confirm", func(t *testing.T) {
		t.Parallel()
		dia := newDialog(t, "keep me")

		require.Nil(t, dia.HandleMsg(shiftPressMsg()))
		require.Nil(t, dia.HandleMsg(shiftPressMsg()))
		require.Nil(t, dia.HandleMsg(shiftPressMsg()))
		require.True(t, dia.confirmClear, "three bare shifts must arm the confirm state")
		require.Equal(t, "keep me", dia.area.Value(), "arming must not clear")
	})

	t.Run("stale shifts do not trigger", func(t *testing.T) {
		t.Parallel()
		dia := newDialog(t, "keep me")
		first := shiftPressMsg()

		require.Nil(t, dia.HandleMsg(first))
		dia.shiftStamps[0] = dia.shiftStamps[0].Add(-tripleShiftWindow * 2)
		require.Nil(t, dia.HandleMsg(shiftPressMsg()))
		require.Nil(t, dia.HandleMsg(shiftPressMsg()))
		require.False(t, dia.confirmClear, "shifts spread over time must not arm")
		require.Equal(t, "keep me", dia.area.Value())
	})
}
