package dialog

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/abbayosua/skynet/internal/ui/common"
	uv "github.com/charmbracelet/ultraviolet"
)

// EditAnswerShortPromptID is the identifier for the answer short prompt
// editor dialog.
const EditAnswerShortPromptID = "edit_answer_short_prompt"

// EditAnswerShortPrompt is a dialog for editing the directive appended to
// prompts when answer_short is enabled.
type EditAnswerShortPrompt struct {
	com   *common.Common
	width int

	keyMap struct {
		Submit  key.Binding
		Clear   key.Binding
		Close   key.Binding
		Confirm key.Binding
		Cancel  key.Binding
	}
	area textarea.Model
	help help.Model
	// confirmClear is true while the dialog waits for the second Ctrl+D that
	// confirms clearing the prompt.
	confirmClear bool
	// shiftStamps records press times of a bare Shift key so triple presses
	// within shiftWindow can trigger the same clear confirmation.
	shiftStamps []time.Time
}

// tripleShiftWindow is the maximum span between the first and third bare
// Shift press that triggers the clear confirmation.
const tripleShiftWindow = 800 * time.Millisecond

// shiftKeyPress reports whether msg is a bare left or right Shift press.
func shiftKeyPress(msg tea.KeyPressMsg) bool {
	return msg.String() == "leftshift" || msg.String() == "rightshift"
}

var _ Dialog = (*EditAnswerShortPrompt)(nil)

// NewEditAnswerShortPrompt creates a new answer short prompt editor dialog,
// pre-filled with the given text.
func NewEditAnswerShortPrompt(com *common.Common, current string) (*EditAnswerShortPrompt, tea.Cmd) {
	m := &EditAnswerShortPrompt{
		com:   com,
		width: 70,
	}

	m.area = textarea.New()
	m.area.SetVirtualCursor(false)
	m.area.Placeholder = "Enter the directive appended to every prompt..."
	m.area.SetWidth(60)
	m.area.SetHeight(3)
	m.area.SetStyles(com.Styles.Editor.Textarea)
	m.area.SetValue(current)
	m.area.Focus()
	m.area.CursorEnd()

	m.keyMap.Submit = key.NewBinding(
		key.WithKeys("ctrl+s", "alt+enter"),
		key.WithHelp("ctrl+s", "save"),
	)
	m.keyMap.Clear = key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("ctrl+d", "clear"),
	)
	m.keyMap.Confirm = key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("ctrl+d", "confirm clear"),
	)
	m.keyMap.Cancel = key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel clear"),
	)
	closeKey := CloseKey
	closeKey.SetHelp("esc", "cancel")
	m.keyMap.Close = closeKey

	help := help.New()
	help.Styles = com.Styles.DialogHelpStyles()
	m.help = help

	return m, nil
}

func (e *EditAnswerShortPrompt) ID() string {
	return EditAnswerShortPromptID
}

func (e *EditAnswerShortPrompt) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// While waiting for a clear confirmation, only the confirm and
		// cancel keys are honoured so stray keystrokes cannot trigger it.
		if e.confirmClear {
			switch {
			case key.Matches(msg, e.keyMap.Confirm):
				e.area.SetValue("")
				e.confirmClear = false
				e.shiftStamps = nil
				return ActionEditAnswerShortPrompt{Prompt: ""}
			case key.Matches(msg, e.keyMap.Cancel):
				e.confirmClear = false
				e.shiftStamps = nil
				return nil
			default:
				return nil
			}
		}
		switch {
		case shiftKeyPress(msg):
			now := time.Now()
			e.shiftStamps = append(e.shiftStamps, now)
			if len(e.shiftStamps) > 3 {
				e.shiftStamps = e.shiftStamps[len(e.shiftStamps)-3:]
			}
			if len(e.shiftStamps) == 3 && now.Sub(e.shiftStamps[0]) <= tripleShiftWindow {
				e.shiftStamps = nil
				if strings.TrimSpace(e.area.Value()) == "" {
					return nil
				}
				e.confirmClear = true
			}
			return nil
		case key.Matches(msg, e.keyMap.Clear):
			if strings.TrimSpace(e.area.Value()) == "" {
				return nil
			}
			e.confirmClear = true
			e.shiftStamps = nil
			return nil
		case key.Matches(msg, e.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, e.keyMap.Submit):
			e.confirmClear = false
			e.shiftStamps = nil
			return ActionEditAnswerShortPrompt{Prompt: strings.TrimSpace(e.area.Value())}
		default:
			e.confirmClear = false
			e.shiftStamps = nil
			var cmd tea.Cmd
			e.area, cmd = e.area.Update(msg)
			return ActionCmd{Cmd: cmd}
		}
	case tea.PasteMsg:
		// This field holds a long prose prompt, so paste is the normal way in.
		var cmd tea.Cmd
		e.area, cmd = e.area.Update(msg)
		return ActionCmd{Cmd: cmd}
	}
	return nil
}

func (e *EditAnswerShortPrompt) InitialCmd() tea.Cmd {
	return nil
}

func (e *EditAnswerShortPrompt) Cursor() *tea.Cursor {
	return InputCursor(e.com.Styles, e.area.Cursor())
}

func (e *EditAnswerShortPrompt) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	rc := NewRenderContext(e.com.Styles, e.width)
	rc.Title = "Edit Answer Short Prompt"
	inputView := e.com.Styles.Dialog.InputPrompt.Render(e.area.View())
	rc.AddPart(inputView)
	if e.confirmClear {
		rc.AddPart(e.com.Styles.Dialog.ListItem.InfoBlurred.Render(
			"Press Ctrl+D again to clear the prompt, or Esc to keep editing."))
	} else {
		rc.AddPart(e.com.Styles.Dialog.ListItem.InfoBlurred.Render(
			"Appended to every prompt when answer_short is enabled. Leave empty to reset to default."))
	}
	rc.Help = e.help.View(e)

	view := rc.Render()
	DrawCenterCursor(scr, area, view, e.Cursor())
	return e.Cursor()
}

// ShortHelp implements [help.KeyMap].
func (e *EditAnswerShortPrompt) ShortHelp() []key.Binding {
	if e.confirmClear {
		return []key.Binding{
			e.keyMap.Confirm,
			e.keyMap.Cancel,
		}
	}
	return []key.Binding{
		e.keyMap.Submit,
		e.keyMap.Clear,
		e.keyMap.Close,
	}
}

// FullHelp implements [help.KeyMap].
func (e *EditAnswerShortPrompt) FullHelp() [][]key.Binding {
	if e.confirmClear {
		return [][]key.Binding{
			{e.keyMap.Confirm},
			{e.keyMap.Cancel},
		}
	}
	return [][]key.Binding{
		{e.keyMap.Submit},
		{e.keyMap.Clear},
		{e.keyMap.Close},
	}
}
