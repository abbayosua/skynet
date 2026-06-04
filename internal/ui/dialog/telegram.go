package dialog

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/abbayosua/skynet/internal/telegram"
	"github.com/abbayosua/skynet/internal/ui/common"
	uv "github.com/charmbracelet/ultraviolet"
)

// TelegramID is the identifier for the Telegram connect dialog.
const TelegramID = "telegram_connect"

type TelegramConnectState int

const (
	TelegramStateInput TelegramConnectState = iota
	TelegramStateVerifying
	TelegramStateSuccess
	TelegramStateError
)

// TelegramConnect is a dialog for entering a Telegram bot token.
type TelegramConnect struct {
	com    *common.Common
	state  TelegramConnectState
	width  int
	height int

	keyMap struct {
		Submit key.Binding
		Close  key.Binding
	}
	input   textinput.Model
	spinner spinner.Model
	help    help.Model
	err     string
}

var _ Dialog = (*TelegramConnect)(nil)

// NewTelegramConnect creates a new Telegram connect dialog.
func NewTelegramConnect(com *common.Common) (*TelegramConnect, tea.Cmd) {
	t := com.Styles

	m := &TelegramConnect{
		com:   com,
		width: 60,
	}

	m.input = textinput.New()
	m.input.SetVirtualCursor(false)
	m.input.Placeholder = "Enter your Telegram bot token..."
	m.input.SetStyles(com.Styles.TextInput)
	m.input.Focus()

	m.keyMap.Submit = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "connect"),
	)
	closeKey := CloseKey
	closeKey.SetHelp("esc", "cancel")
	m.keyMap.Close = closeKey

	help := help.New()
	help.Styles = com.Styles.DialogHelpStyles()
	m.help = help

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = t.Dialog.Spinner
	m.spinner = s

	return m, nil
}

func (t *TelegramConnect) ID() string {
	return TelegramID
}

func (t *TelegramConnect) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if t.state == TelegramStateVerifying {
			var cmd tea.Cmd
			t.spinner, cmd = t.spinner.Update(msg)
			return ActionCmd{Cmd: cmd}
		}
	case telegramVerifiedMsg:
		if msg.err != "" {
			t.state = TelegramStateError
			t.err = msg.err
			t.input.SetValue("")
			t.input.Focus()
		} else {
			t.state = TelegramStateSuccess
			return ActionConnectTelegram{Token: msg.token}
		}
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, t.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, t.keyMap.Submit):
			if t.state != TelegramStateInput {
				break
			}
			token := strings.TrimSpace(t.input.Value())
			if token == "" {
				break
			}
			t.state = TelegramStateVerifying
			return ActionCmd{Cmd: func() tea.Msg {
				bot := telegram.NewBot(token)
				if bot.TestToken() {
					return telegramVerifiedMsg{token: token}
				}
				return telegramVerifiedMsg{err: "Invalid token or network error"}
			}}
		default:
			if t.state == TelegramStateInput {
				var cmd tea.Cmd
				t.input, cmd = t.input.Update(msg)
				return ActionCmd{Cmd: cmd}
			}
		}
	}
	return nil
}

func (t *TelegramConnect) InitialCmd() tea.Cmd {
	return nil
}

func (t *TelegramConnect) Cursor() *tea.Cursor {
	if t.state == TelegramStateInput {
		return InputCursor(t.com.Styles, t.input.Cursor())
	}
	return nil
}

func (t *TelegramConnect) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	rc := NewRenderContext(t.com.Styles, t.width)

	switch t.state {
	case TelegramStateInput:
		rc.Title = "Connect Telegram"
		inputView := t.com.Styles.Dialog.InputPrompt.Render(t.input.View())
		rc.AddPart(inputView)
		rc.Help = t.help.View(t)
	case TelegramStateVerifying:
		rc.Title = "Verifying..."
		rc.AddPart(t.com.Styles.Dialog.Spinner.Render(t.spinner.View() + " Checking token..."))
	case TelegramStateSuccess:
		rc.Title = "Connected"
		rc.AddPart(t.com.Styles.Dialog.NormalItem.Render("Telegram bot connected successfully!"))
		rc.AddPart(t.com.Styles.Dialog.ListItem.InfoBlurred.Render("Send /start to your bot on Telegram to start mirroring."))
	case TelegramStateError:
		rc.Title = "Connection Failed"
		rc.AddPart(t.com.Styles.Dialog.NormalItem.Render(t.err))
		rc.Help = fmt.Sprintf("Press %s to try again", t.keyMap.Close.Help().Key)
	}

	view := rc.Render()
	DrawCenterCursor(scr, area, view, t.Cursor())
	return t.Cursor()
}

// ShortHelp implements [help.KeyMap].
func (t *TelegramConnect) ShortHelp() []key.Binding {
	return []key.Binding{
		t.keyMap.Submit,
		t.keyMap.Close,
	}
}

// FullHelp implements [help.KeyMap].
func (t *TelegramConnect) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{t.keyMap.Submit},
		{t.keyMap.Close},
	}
}

// telegramVerifiedMsg is sent when token verification completes.
type telegramVerifiedMsg struct {
	token string
	err   string
}
