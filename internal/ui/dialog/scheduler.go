package dialog

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/abbayosua/skynet/internal/scheduler"
	"github.com/abbayosua/skynet/internal/ui/common"
	"github.com/abbayosua/skynet/internal/ui/list"
	"github.com/abbayosua/skynet/internal/ui/util"
	uv "github.com/charmbracelet/ultraviolet"
)

const SchedulerID = "scheduler"

type schedulerMode int

const (
	schedulerModeList schedulerMode = iota
	schedulerModeAddName
	schedulerModeAddInterval
	schedulerModeAddPrompt
	schedulerModeDelete
)

type SchedulerDialog struct {
	com  *common.Common
	help help.Model
	list *list.FilterableList
	mode schedulerMode

	jobs    []*scheduler.Job
	store   *scheduler.Store
	input       textinput.Model
	addName     string
	addInterval string

	keyMap struct {
		Select  key.Binding
		Next    key.Binding
		Prev    key.Binding
		New     key.Binding
		Delete  key.Binding
		Confirm key.Binding
		Cancel  key.Binding
		Close   key.Binding
	}
}

var _ Dialog = (*SchedulerDialog)(nil)

func NewSchedulerDialog(com *common.Common) (*SchedulerDialog, error) {
	d := &SchedulerDialog{
		com:  com,
		mode: schedulerModeList,
	}

	var err error
	d.store, err = scheduler.NewStore(scheduler.DefaultDataDir())
	if err != nil {
		return nil, err
	}
	d.jobs = d.store.List()

	d.list = list.NewFilterableList()
	d.list.Focus()
	d.list.SetSelected(0)
	d.setItems()

	d.help = help.New()
	d.help.Styles = com.Styles.DialogHelpStyles()

	d.input = textinput.New()
	d.input.SetVirtualCursor(false)
	d.input.SetStyles(com.Styles.TextInput)

	d.keyMap.Select = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "run job"))
	d.keyMap.Next = key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "next"))
	d.keyMap.Prev = key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "prev"))
	d.keyMap.New = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new job"))
	d.keyMap.Delete = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete"))
	d.keyMap.Confirm = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm"))
	d.keyMap.Cancel = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))
	d.keyMap.Close = CloseKey

	return d, nil
}

func (d *SchedulerDialog) ID() string { return SchedulerID }

func (d *SchedulerDialog) InitialCmd() tea.Cmd { return nil }

func (d *SchedulerDialog) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch d.mode {
		case schedulerModeList:
			return d.handleListKey(msg)
		case schedulerModeAddName, schedulerModeAddInterval, schedulerModeAddPrompt:
			return d.handleAddKey(msg)
		case schedulerModeDelete:
			return d.handleDeleteKey(msg)
		}
	}
	return nil
}

func (d *SchedulerDialog) handleListKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, d.keyMap.Close):
		return ActionClose{}
	case key.Matches(msg, d.keyMap.Next):
		if d.list.IsSelectedLast() {
			d.list.SelectFirst()
		} else {
			d.list.SelectNext()
		}
		d.list.ScrollToSelected()
	case key.Matches(msg, d.keyMap.Prev):
		if d.list.IsSelectedFirst() {
			d.list.SelectLast()
		} else {
			d.list.SelectPrev()
		}
		d.list.ScrollToSelected()
	case key.Matches(msg, d.keyMap.Select):
		if sel := d.list.SelectedItem(); sel != nil {
			if item, ok := sel.(*SchedulerItem); ok && item.job != nil {
				return ActionRunCustomCommand{Content: item.job.Prompt}
			}
		}
	case key.Matches(msg, d.keyMap.New):
		d.mode = schedulerModeAddName
		d.input.SetValue("")
		d.input.Placeholder = "Job name (e.g. auto-tester)"
		d.input.Focus()
	case key.Matches(msg, d.keyMap.Delete):
		if sel := d.list.SelectedItem(); sel != nil {
			if _, ok := sel.(*SchedulerItem); ok {
				d.mode = schedulerModeDelete
			}
		}
	}
	return nil
}

func (d *SchedulerDialog) handleAddKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, d.keyMap.Cancel):
		d.mode = schedulerModeList
		d.input.Blur()
		return nil
	case key.Matches(msg, d.keyMap.Confirm):
		val := strings.TrimSpace(d.input.Value())
		if val == "" {
			return nil
		}
		switch d.mode {
		case schedulerModeAddName:
			d.addName = val
			d.mode = schedulerModeAddInterval
			d.input.SetValue("")
			d.input.Placeholder = "Interval (e.g. 15m, hourly, daily)"
		case schedulerModeAddInterval:
			d.addInterval = val
			d.mode = schedulerModeAddPrompt
			d.input.SetValue("")
			d.input.Placeholder = "Prompt to execute"
		case schedulerModeAddPrompt:
			job := &scheduler.Job{
				Name:      d.addName,
				Interval:  d.addInterval,
				Prompt:    val,
				Enabled:   true,
				CreatedAt: time.Now(),
			}
			job.ID = scheduler.JobID(job.Name)
			if _, err := scheduler.ParseInterval(job.Interval); err != nil {
				return ActionCmd{Cmd: func() tea.Msg {
					return util.InfoMsg{Type: util.InfoTypeError, Msg: fmt.Sprintf("Invalid interval %q: %s", job.Interval, err)}
				}}
			}
			if err := d.store.Save(job); err != nil {
				return ActionCmd{Cmd: func() tea.Msg {
					return util.InfoMsg{Type: util.InfoTypeError, Msg: fmt.Sprintf("Failed: %s", err)}
				}}
			}
			d.jobs = d.store.List()
			d.setItems()
			d.mode = schedulerModeList
			d.input.Blur()
			return ActionCmd{Cmd: func() tea.Msg {
				return util.InfoMsg{Type: util.InfoTypeSuccess, Msg: fmt.Sprintf("Job %s added", job.Name)}
			}}
		}
	default:
		var cmd tea.Cmd
		d.input, cmd = d.input.Update(msg)
		return ActionCmd{Cmd: cmd}
	}
	return nil
}

func (d *SchedulerDialog) inputValue() string {
	return d.input.Value()
}

func (d *SchedulerDialog) handleDeleteKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, d.keyMap.Confirm):
		if sel := d.list.SelectedItem(); sel != nil {
			if item, ok := sel.(*SchedulerItem); ok && item.job != nil {
				d.store.Delete(item.job.ID)
				d.jobs = d.store.List()
				d.setItems()
			}
		}
		d.mode = schedulerModeList
	case key.Matches(msg, d.keyMap.Cancel), key.Matches(msg, d.keyMap.Close):
		d.mode = schedulerModeList
	}
	return nil
}

func (d *SchedulerDialog) setItems() {
	items := make([]list.FilterableItem, len(d.jobs))
	for i, j := range d.jobs {
		items[i] = &SchedulerItem{job: j, Versioned: list.NewVersioned()}
	}
	d.list.SetItems(items...)
}

func (d *SchedulerDialog) Cursor() *tea.Cursor {
	if d.mode != schedulerModeList {
		return InputCursor(d.com.Styles, d.input.Cursor())
	}
	return nil
}

func (d *SchedulerDialog) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))

	innerWidth := max(1, width-t.Dialog.View.GetHorizontalFrameSize())
	heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize()

	listHeight := max(1, height-heightOffset)
	d.list.SetSize(innerWidth, listHeight)
	d.help.SetWidth(innerWidth)

	rc := NewRenderContext(t, width)
	rc.Title = "Scheduled Jobs"

	if d.mode == schedulerModeList {
		listView := t.Dialog.List.Height(d.list.Height()).Render(d.list.Render())
		rc.AddPart(listView)
		rc.Help = d.help.View(d)
	} else {
		prompt := ""
		switch d.mode {
		case schedulerModeAddName:
			prompt = "Job Name:"
		case schedulerModeAddInterval:
			prompt = "Interval (15m, hourly, daily):"
		case schedulerModeAddPrompt:
			prompt = "Prompt:"
		case schedulerModeDelete:
			if sel := d.list.SelectedItem(); sel != nil {
				if item, ok := sel.(*SchedulerItem); ok && item.job != nil {
					prompt = fmt.Sprintf("Delete job %q? Press enter to confirm, esc to cancel.", item.job.Name)
				}
			}
		}

		if prompt != "" {
			rc.AddPart(t.Dialog.InputPrompt.Render(prompt))
		}
		if d.mode <= schedulerModeAddPrompt {
			d.input.SetWidth(max(0, innerWidth-t.Dialog.InputPrompt.GetHorizontalFrameSize()-1))
			rc.AddPart(t.Dialog.InputPrompt.Render(d.input.View()))
		}
		rc.Help = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("enter: confirm | esc: cancel")
	}

	view := rc.Render()
	cur := d.Cursor()
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

func (d *SchedulerDialog) ShortHelp() []key.Binding {
	return []key.Binding{d.keyMap.New, d.keyMap.Select, d.keyMap.Delete, d.keyMap.Close}
}

func (d *SchedulerDialog) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{d.keyMap.Select, d.keyMap.Next, d.keyMap.Prev, d.keyMap.New, d.keyMap.Delete},
		{d.keyMap.Close},
	}
}

type SchedulerItem struct {
	*list.Versioned
	job *scheduler.Job
}

func (i *SchedulerItem) Finished() bool { return true }

func (i *SchedulerItem) Filter() string { return i.job.Name }

func (i *SchedulerItem) Render(width int) string {
	status := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("● active")
	if !i.job.Enabled {
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("○ disabled")
	}
	mode := ""
	if i.job.Continue {
		mode = " [continue]"
	}
	first := fmt.Sprintf("%s  %s  %s%s", i.job.Name, i.job.Interval, status, mode)
	second := i.job.Prompt
	if len(second) > 50 {
		second = second[:50] + "..."
	}
	if !i.job.LastRunAt.IsZero() {
		second += fmt.Sprintf(" — last: %s", i.job.LastResult)
	}
	return first + "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(second)
}
