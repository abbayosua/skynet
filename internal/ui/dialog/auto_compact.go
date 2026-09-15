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

// AutoCompactID is the identifier for the auto-compact dialog.
const AutoCompactID = "auto_compact"

// AutoCompact is a dialog for setting a custom auto-compact threshold in
// tokens. An empty or zero value restores context-window based compaction.
type AutoCompact struct {
	com   *common.Common
	width int

	keyMap struct {
		Submit key.Binding
		Close  key.Binding
	}

	input textinput.Model
	help  help.Model
}

var _ Dialog = (*AutoCompact)(nil)

// NewAutoCompact creates a new auto-compact threshold dialog, pre-filled
// with the given token value.
func NewAutoCompact(com *common.Common, current int64) (*AutoCompact, tea.Cmd) {
	m := &AutoCompact{
		com:   com,
		width: 60,
	}

	m.input = textinput.New()
	m.input.SetVirtualCursor(false)
	m.input.Placeholder = "e.g. 15000 (0 or empty = default)"
	m.input.SetStyles(com.Styles.TextInput)
	if current > 0 {
		m.input.SetValue(strconv.FormatInt(current, 10))
	}
	m.input.Focus()

	m.keyMap.Submit = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "apply"),
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
func (a *AutoCompact) ID() string {
	return AutoCompactID
}

// HandleMsg implements Dialog.
func (a *AutoCompact) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, a.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, a.keyMap.Submit):
			raw := strings.TrimSpace(a.input.Value())
			var tokens int64
			if raw != "" {
				n, err := strconv.ParseInt(raw, 10, 64)
				if err != nil || n < 0 {
					break
				}
				tokens = n
			}
			return ActionSetAutoCompact{Tokens: tokens}
		default:
			var cmd tea.Cmd
			a.input, cmd = a.input.Update(msg)
			return ActionCmd{Cmd: cmd}
		}
	}
	return nil
}

// InitialCmd implements Dialog.
func (a *AutoCompact) InitialCmd() tea.Cmd {
	return nil
}

// Cursor implements Dialog.
func (a *AutoCompact) Cursor() *tea.Cursor {
	return InputCursor(a.com.Styles, a.input.Cursor())
}

// Draw implements Dialog.
func (a *AutoCompact) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	rc := NewRenderContext(a.com.Styles, a.width)
	rc.Title = "Auto-Compact Threshold"

	inputView := a.com.Styles.Dialog.InputPrompt.Render("Tokens: " + a.input.View())
	rc.AddPart(inputView)
	rc.AddPart(a.com.Styles.Dialog.ListItem.InfoBlurred.Render(
		"Compacts the session at an exact token count instead of the context-window threshold. 0 restores the default."))
	rc.Help = a.help.View(a)

	view := rc.Render()
	DrawCenterCursor(scr, area, view, a.Cursor())
	return a.Cursor()
}

// ShortHelp implements help.KeyMap.
func (a *AutoCompact) ShortHelp() []key.Binding {
	return []key.Binding{
		a.keyMap.Submit,
		a.keyMap.Close,
	}
}

// FullHelp implements help.KeyMap.
func (a *AutoCompact) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{a.keyMap.Submit},
		{a.keyMap.Close},
	}
}
