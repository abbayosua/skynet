package dialog

import (
	"strconv"
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

// Autopilot is a dialog for entering a goal and optional max steps.
type Autopilot struct {
	com   *common.Common
	width int

	keyMap struct {
		Submit key.Binding
		Close  key.Binding
	}

	goalInput    textinput.Model
	stepsFocused bool
	stepsInput   textinput.Model
	help         help.Model
}

var _ Dialog = (*Autopilot)(nil)

// NewAutopilot creates a new autopilot dialog.
func NewAutopilot(com *common.Common) (*Autopilot, tea.Cmd) {
	m := &Autopilot{
		com:   com,
		width: 60,
	}

	m.goalInput = textinput.New()
	m.goalInput.SetVirtualCursor(false)
	m.goalInput.Placeholder = "Enter a goal for autonomous autopilot..."
	m.goalInput.SetStyles(com.Styles.TextInput)
	m.goalInput.Focus()

	m.stepsInput = textinput.New()
	m.stepsInput.SetVirtualCursor(false)
	m.stepsInput.Placeholder = "10"
	m.stepsInput.SetStyles(com.Styles.TextInput)
	m.stepsInput.SetValue("10")

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
			goal := strings.TrimSpace(a.goalInput.Value())
			if goal == "" {
				break
			}
			maxSteps := 10
			if v := strings.TrimSpace(a.stepsInput.Value()); v != "" {
				if n, err := strconv.Atoi(v); err == nil && n > 0 {
					maxSteps = n
				}
			}
			return ActionAutopilot{Goal: goal, MaxSteps: maxSteps}
		case key.Matches(msg, key.NewBinding(key.WithKeys("tab"))):
			a.stepsFocused = !a.stepsFocused
			if a.stepsFocused {
				a.goalInput.Blur()
				a.stepsInput.Focus()
			} else {
				a.stepsInput.Blur()
				a.goalInput.Focus()
			}
		default:
			var cmd tea.Cmd
			if a.stepsFocused {
				a.stepsInput, cmd = a.stepsInput.Update(msg)
			} else {
				a.goalInput, cmd = a.goalInput.Update(msg)
			}
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
	if a.stepsFocused {
		return InputCursor(a.com.Styles, a.stepsInput.Cursor())
	}
	return InputCursor(a.com.Styles, a.goalInput.Cursor())
}

// Draw implements Dialog.
func (a *Autopilot) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	rc := NewRenderContext(a.com.Styles, a.width)
	rc.Title = "Autopilot"

	goalView := a.com.Styles.Dialog.InputPrompt.Render("Goal: " + a.goalInput.View())
	stepsLabel := "Max Steps: "
	stepsView := a.com.Styles.Dialog.InputPrompt.Render(stepsLabel + a.stepsInput.View())

	rc.AddPart(goalView)
	rc.AddPart(stepsView)
	rc.AddPart(a.com.Styles.Dialog.ListItem.InfoBlurred.Render("Tab to switch fields. Autopilot runs the goal-driven autonomous loop."))
	rc.Help = a.help.View(a)

	view := rc.Render()
	DrawCenterCursor(scr, area, view, a.Cursor())
	return a.Cursor()
}

// ShortHelp implements help.KeyMap.
func (a *Autopilot) ShortHelp() []key.Binding {
	return []key.Binding{
		a.keyMap.Submit,
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch field")),
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
