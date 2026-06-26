// Package tui is the interactive Charmbracelet dashboard (bare `tp` / `tp dash`).
// It drives the same core.Service the cli does: List for the live view, and
// Sync/Merge/Remove for inline actions. Long-running core calls run as tea.Cmd
// goroutines (List is lock-free; the mutating ops take the flock only briefly),
// so the event loop never blocks; a single in-flight guard prevents a second
// mutation, and actions target a task by name so a refresh can't misdirect them.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cyakimov/treepi/internal/core"
)

type viewState int

const (
	browsing viewState = iota
	confirming
	working
)

const footerLines = 3

type pendingAction struct {
	op   string // "sync" | "merge" | "rm"
	task string
}

type (
	tickMsg       time.Time
	tasksMsg      struct {
		tasks []core.TaskInfo
		err   error
	}
	actionDoneMsg struct {
		op   string
		task string
		err  error
	}
)

// Model is the dashboard state.
type Model struct {
	svc      *core.Service
	ctx      context.Context
	list     list.Model
	spinner  spinner.Model
	state    viewState
	pending  pendingAction
	lastErr  error
	chosenCD string // worktree path the user chose to cd into (printed on exit)
	w, h     int
}

var (
	helpStyle  = lipgloss.NewStyle().Faint(true)
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	titleStyle = lipgloss.NewStyle().Bold(true)
	modalStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 3)
)

func newModel(ctx context.Context, svc *core.Service) Model {
	l := list.New(nil, itemDelegate{}, 0, 0)
	l.Title = "treepi worktrees"
	l.SetShowStatusBar(false)
	l.SetShowHelp(false) // we render our own footer
	l.Styles.Title = titleStyle

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	return Model{svc: svc, ctx: ctx, list: l, spinner: sp, state: browsing}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.refreshCmd(), tickCmd(), m.spinner.Tick)
}

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) refreshCmd() tea.Cmd {
	svc, ctx := m.svc, m.ctx
	return func() tea.Msg {
		tasks, err := svc.List(ctx, true)
		return tasksMsg{tasks: tasks, err: err}
	}
}

func (m Model) actionCmd(p pendingAction) tea.Cmd {
	svc, ctx := m.svc, m.ctx
	return func() tea.Msg {
		var err error
		switch p.op {
		case "sync":
			_, err = svc.Sync(ctx, p.task)
		case "merge":
			_, err = svc.Merge(ctx, p.task)
		case "rm":
			_, err = svc.Remove(ctx, p.task, false)
		}
		return actionDoneMsg{op: p.op, task: p.task, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.list.SetSize(msg.Width, msg.Height-footerLines)
		return m, nil

	case tickMsg:
		cmds := []tea.Cmd{tickCmd()}
		// Don't refresh under a modal, mid-action, or mid-typing: it would
		// reorder rows and move the selection out from under the user.
		if m.state == browsing && m.list.FilterState() != list.Filtering {
			cmds = append(cmds, m.refreshCmd())
		}
		return m, tea.Batch(cmds...)

	case tasksMsg:
		if msg.err != nil {
			m.lastErr = msg.err
			return m, nil
		}
		m.lastErr = nil
		m.setItems(msg.tasks)
		return m, nil

	case actionDoneMsg:
		m.state = browsing
		m.lastErr = msg.err
		return m, m.refreshCmd()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While filtering, every key belongs to the list (typing the filter).
	if m.list.FilterState() == list.Filtering {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}

	switch m.state {
	case confirming:
		switch msg.String() {
		case "y", "Y":
			m.state = working
			return m, tea.Batch(m.actionCmd(m.pending), m.spinner.Tick)
		default: // n / esc / anything else cancels
			m.state = browsing
			return m, nil
		}
	case working:
		return m, nil // input ignored while an action runs
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "r":
		return m, m.refreshCmd()
	case "enter":
		if it, ok := m.list.SelectedItem().(taskItem); ok {
			m.chosenCD = it.ti.Path
			return m, tea.Quit
		}
		return m, nil
	case "s":
		return m.act("sync")
	case "m":
		return m.act("merge")
	case "x", "d":
		return m.act("rm")
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// act guards the selected task's status, then either fires sync directly or opens
// a confirmation modal for the destructive/integrative ops.
func (m Model) act(op string) (tea.Model, tea.Cmd) {
	it, ok := m.list.SelectedItem().(taskItem)
	if !ok {
		return m, nil
	}
	st := it.ti.Status
	if op == "rm" {
		if st != "ready" && st != "orphaned" {
			m.lastErr = fmt.Errorf("cannot rm a %s task", st)
			return m, nil
		}
	} else if st != "ready" {
		m.lastErr = fmt.Errorf("cannot %s a %s task", op, st)
		return m, nil
	}

	m.pending = pendingAction{op: op, task: it.ti.Task}
	if op == "sync" {
		m.state = working
		return m, tea.Batch(m.actionCmd(m.pending), m.spinner.Tick)
	}
	m.state = confirming
	return m, nil
}

// setItems rebuilds the list, preserving the selection by task name.
func (m *Model) setItems(tasks []core.TaskInfo) {
	selected := ""
	if it, ok := m.list.SelectedItem().(taskItem); ok {
		selected = it.ti.Task
	}
	items := make([]list.Item, len(tasks))
	keep := -1
	for i, t := range tasks {
		items[i] = taskItem{ti: t}
		if t.Task == selected {
			keep = i
		}
	}
	m.list.SetItems(items)
	if keep >= 0 {
		m.list.Select(keep)
	}
}

func (m Model) View() string {
	if m.w == 0 {
		return "loading…"
	}
	if m.state == confirming {
		return m.confirmView()
	}
	var b strings.Builder
	b.WriteString(m.list.View())
	b.WriteString("\n")
	b.WriteString(m.footer())
	return b.String()
}

func (m Model) footer() string {
	help := "enter: cd · s: sync · m: merge · x: rm · r: refresh · /: filter · q: quit"
	if m.state == working {
		help = m.spinner.View() + " " + m.pending.op + " " + m.pending.task + "…"
	}
	out := helpStyle.Render(help)
	if m.lastErr != nil {
		out += "\n" + errStyle.Render("error: "+m.lastErr.Error())
	}
	return out
}

func (m Model) confirmView() string {
	var msg string
	switch m.pending.op {
	case "rm":
		msg = "Remove " + m.pending.task + "?\ndeletes the worktree + branch (snapshotted first)\n[y/N]"
	case "merge":
		msg = "Merge " + m.pending.task + " into trunk?\n[y/N]"
	default:
		msg = m.pending.op + " " + m.pending.task + "?\n[y/N]"
	}
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, modalStyle.Render(msg))
}
