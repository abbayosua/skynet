package dialog

import (
	"strings"

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
		Submit key.Binding
		Close  key.Binding
	}
	area textarea.Model
	help help.Model
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
		switch {
		case key.Matches(msg, e.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, e.keyMap.Submit):
			return ActionEditAnswerShortPrompt{Prompt: strings.TrimSpace(e.area.Value())}
		default:
			var cmd tea.Cmd
			e.area, cmd = e.area.Update(msg)
			return ActionCmd{Cmd: cmd}
		}
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
	rc.AddPart(e.com.Styles.Dialog.ListItem.InfoBlurred.Render(
		"Appended to every prompt when answer_short is enabled. Leave empty to reset to default."))
	rc.Help = e.help.View(e)

	view := rc.Render()
	DrawCenterCursor(scr, area, view, e.Cursor())
	return e.Cursor()
}

// ShortHelp implements [help.KeyMap].
func (e *EditAnswerShortPrompt) ShortHelp() []key.Binding {
	return []key.Binding{
		e.keyMap.Submit,
		e.keyMap.Close,
	}
}

// FullHelp implements [help.KeyMap].
func (e *EditAnswerShortPrompt) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{e.keyMap.Submit},
		{e.keyMap.Close},
	}
}
