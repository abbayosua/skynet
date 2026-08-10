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

// SaveCommandID is the identifier for the save command dialog.
const SaveCommandID = "save_command"

// SaveCommand is a dialog for saving the current input as a custom command.
type SaveCommand struct {
	com   *common.Common
	width int

	keyMap struct {
		Submit key.Binding
		Close  key.Binding
	}
	input textinput.Model
	help  help.Model
}

var _ Dialog = (*SaveCommand)(nil)

// NewSaveCommand creates a new save command dialog.
func NewSaveCommand(com *common.Common) (*SaveCommand, tea.Cmd) {
	m := &SaveCommand{
		com:   com,
		width: 60,
	}

	m.input = textinput.New()
	m.input.SetVirtualCursor(false)
	m.input.Placeholder = "Enter a name for this command..."
	m.input.SetStyles(com.Styles.TextInput)
	m.input.Focus()

	m.keyMap.Submit = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "save"),
	)
	closeKey := CloseKey
	closeKey.SetHelp("esc", "cancel")
	m.keyMap.Close = closeKey

	help := help.New()
	help.Styles = com.Styles.DialogHelpStyles()
	m.help = help

	return m, nil
}

func (s *SaveCommand) ID() string {
	return SaveCommandID
}

func (s *SaveCommand) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, s.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, s.keyMap.Submit):
			name := strings.TrimSpace(s.input.Value())
			if name == "" {
				break
			}
			return ActionSaveCommand{Name: name}
		default:
			var cmd tea.Cmd
			s.input, cmd = s.input.Update(msg)
			return ActionCmd{Cmd: cmd}
		}
	}
	return nil
}

func (s *SaveCommand) InitialCmd() tea.Cmd {
	return nil
}

func (s *SaveCommand) Cursor() *tea.Cursor {
	return InputCursor(s.com.Styles, s.input.Cursor())
}

func (s *SaveCommand) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	rc := NewRenderContext(s.com.Styles, s.width)
	rc.Title = "Save Command"
	inputView := s.com.Styles.Dialog.InputPrompt.Render(s.input.View())
	rc.AddPart(inputView)
	rc.AddPart(s.com.Styles.Dialog.ListItem.InfoBlurred.Render("Saves the current input as a user command."))
	rc.Help = s.help.View(s)

	view := rc.Render()
	DrawCenterCursor(scr, area, view, s.Cursor())
	return s.Cursor()
}

// ShortHelp implements [help.KeyMap].
func (s *SaveCommand) ShortHelp() []key.Binding {
	return []key.Binding{
		s.keyMap.Submit,
		s.keyMap.Close,
	}
}

// FullHelp implements [help.KeyMap].
func (s *SaveCommand) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{s.keyMap.Submit},
		{s.keyMap.Close},
	}
}
