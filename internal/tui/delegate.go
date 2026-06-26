package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cyakimov/treepi/internal/core"
)

// taskItem adapts a core.TaskInfo to a bubbles/list item, filtered by task name.
type taskItem struct{ ti core.TaskInfo }

func (i taskItem) FilterValue() string { return i.ti.Task }

var (
	selectedStyle = lipgloss.NewStyle().Bold(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
	statusStyles  = map[string]lipgloss.Style{
		"ready":            lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
		"creating":         lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
		"merging":          lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
		"needs-resolution": lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
		"orphaned":         lipgloss.NewStyle().Faint(true),
	}
)

// itemDelegate renders one task as a fixed-width row.
type itemDelegate struct{}

func (itemDelegate) Height() int                         { return 1 }
func (itemDelegate) Spacing() int                        { return 0 }
func (itemDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (itemDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(taskItem)
	if !ok {
		return
	}
	ti := it.ti

	cursor := "  "
	if index == m.Index() {
		cursor = "> "
	}
	status := ti.Status
	if st, ok := statusStyles[status]; ok {
		status = st.Render(pad(ti.Status, 16))
	} else {
		status = pad(ti.Status, 16)
	}
	lease := ""
	if ti.Lease != nil {
		lease = ti.Lease.Owner
	}
	row := fmt.Sprintf("%s%s %s %3d  %s %s",
		cursor,
		pad(ti.Task, 18),
		status,
		ti.Slot,
		pad(fmt.Sprintf("%d/%d", ti.Ahead, ti.Behind), 7),
		dimStyle.Render(lease),
	)
	if index == m.Index() {
		row = selectedStyle.Render(row)
	}
	_, _ = fmt.Fprint(w, row)
}

// pad truncates or right-pads s to exactly n cells (ASCII-width approximation).
func pad(s string, n int) string {
	if len(s) > n {
		if n <= 1 {
			return s[:n]
		}
		return s[:n-1] + "…"
	}
	return s + strings.Repeat(" ", n-len(s))
}
