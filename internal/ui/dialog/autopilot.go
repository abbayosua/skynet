package dialog

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/abbayosua/skynet/internal/ui/common"
	uv "github.com/charmbracelet/ultraviolet"
)

// AutopilotID is the identifier for the autopilot dialog.
const AutopilotID = "autopilot"

// Autopilot is a dialog for entering a goal for the autonomous autopilot loop.
type Autopilot struct {
	com   *common.Common
	width int

	keyMap struct {
		Submit key.Binding
		Close  key.Binding
	}

	input textinput.Model
	help  help.Model
}

var _ Dialog = (*Autopilot)(nil)

// NewAutopilot creates a new autopilot goal dialog.
func NewAutopilot(com *common.Common) (*Autopilot, tea.Cmd) {
	m := &Autopilot{
		com:   com,
		width: 60,
	}

	m.input = textinput.New()
	m.input.SetVirtualCursor(false)
	m.input.Placeholder = "Enter a goal for autonomous autopilot..."
	m.input.SetStyles(com.Styles.TextInput)
	m.input.Focus()

	m.keyMap.Submit = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "start autopilot"),
	)
	closeKey := CloseKey
	closeKey.SetHelp("esc", "cancel")
	m.keyMap.Close = closeKey

	help := help.New()
	help.Styles = com.Styles.DialogHelpStyles()
	m.help = help

	return m, nil
}

// ID implements Dialog.
func (a *Autopilot) ID() string {
	return AutopilotID
}

// HandleMsg implements Dialog.
func (a *Autopilot) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, a.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, a.keyMap.Submit):
			goal := strings.TrimSpace(a.input.Value())
			if goal == "" {
				break
			}
			return ActionAutopilot{Goal: goal}
		default:
			var cmd tea.Cmd
			a.input, cmd = a.input.Update(msg)
			return ActionCmd{Cmd: cmd}
		}
	}
	return nil
}

// InitialCmd implements Dialog.
func (a *Autopilot) InitialCmd() tea.Cmd {
	return nil
}

// Cursor implements Dialog.
func (a *Autopilot) Cursor() *tea.Cursor {
	return InputCursor(a.com.Styles, a.input.Cursor())
}

// Draw implements Dialog.
func (a *Autopilot) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	rc := NewRenderContext(a.com.Styles, a.width)
	rc.Title = "Autopilot"
	inputView := a.com.Styles.Dialog.InputPrompt.Render(a.input.View())
	rc.AddPart(inputView)
	rc.AddPart(a.com.Styles.Dialog.ListItem.InfoBlurred.Render("Runs the goal-driven autonomous autopilot loop in the current session."))
	rc.Help = a.help.View(a)

	view := rc.Render()
	DrawCenterCursor(scr, area, view, a.Cursor())
	return a.Cursor()
}

// ShortHelp implements help.KeyMap.
func (a *Autopilot) ShortHelp() []key.Binding {
	return []key.Binding{
		a.keyMap.Submit,
		a.keyMap.Close,
	}
}

// FullHelp implements help.KeyMap.
func (a *Autopilot) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{a.keyMap.Submit},
		{a.keyMap.Close},
	}
}
