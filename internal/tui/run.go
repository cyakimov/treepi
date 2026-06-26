package tui

import (
	"context"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cyakimov/treepi/internal/core"
)

// Run launches the dashboard and returns the path of the worktree the user chose
// to cd into ("" if none). It renders to the controlling terminal (/dev/tty) so
// the chosen path on stdout stays clean for the `tp` shell wrapper to capture;
// where /dev/tty is unavailable (Windows) it falls back to the default I/O and
// cd-capture is simply unavailable.
func Run(ctx context.Context, svc *core.Service) (string, error) {
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		defer func() { _ = tty.Close() }()
		opts = append(opts, tea.WithInput(tty), tea.WithOutput(tty))
	}
	final, err := tea.NewProgram(newModel(ctx, svc), opts...).Run()
	if err != nil {
		return "", err
	}
	if fm, ok := final.(Model); ok {
		return fm.chosenCD, nil
	}
	return "", nil
}
