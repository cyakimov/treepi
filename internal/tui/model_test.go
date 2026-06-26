package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cyakimov/treepi/internal/core"
)

func key(s string) tea.KeyMsg {
	if s == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func update(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

// seeded returns a sized model populated with tasks (svc is nil: the reducer
// tests never execute the returned commands, so no service is touched).
func seeded(tasks ...core.TaskInfo) Model {
	m := newModel(context.Background(), nil)
	m, _ = update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = update(m, tasksMsg{tasks: tasks})
	return m
}

func TestUpdateTasksMsgPopulates(t *testing.T) {
	m := seeded(
		core.TaskInfo{Task: "a", Status: "ready"},
		core.TaskInfo{Task: "b", Status: "ready"},
	)
	if len(m.list.Items()) != 2 {
		t.Fatalf("items = %d, want 2", len(m.list.Items()))
	}
	if it, ok := m.list.SelectedItem().(taskItem); !ok || it.ti.Task != "a" {
		t.Errorf("selected = %+v", m.list.SelectedItem())
	}
}

func TestUpdateMergeConfirmsThenRuns(t *testing.T) {
	m := seeded(core.TaskInfo{Task: "a", Status: "ready", Path: "/p/a"})
	m, _ = update(m, key("m"))
	if m.state != confirming {
		t.Fatalf("state after m = %v, want confirming", m.state)
	}
	m, cmd := update(m, key("y"))
	if m.state != working {
		t.Errorf("state after y = %v, want working", m.state)
	}
	if cmd == nil {
		t.Error("expected an action command after confirming")
	}
}

func TestUpdateConfirmCancel(t *testing.T) {
	m := seeded(core.TaskInfo{Task: "a", Status: "ready", Path: "/p/a"})
	m, _ = update(m, key("x")) // rm -> confirm
	if m.state != confirming {
		t.Fatalf("state = %v, want confirming", m.state)
	}
	m, _ = update(m, key("n"))
	if m.state != browsing {
		t.Errorf("state after n = %v, want browsing", m.state)
	}
}

func TestUpdateSyncFiresWithoutConfirm(t *testing.T) {
	m := seeded(core.TaskInfo{Task: "a", Status: "ready", Path: "/p/a"})
	m, cmd := update(m, key("s"))
	if m.state != working {
		t.Errorf("sync should go straight to working, got %v", m.state)
	}
	if cmd == nil {
		t.Error("expected a sync command")
	}
}

func TestUpdateGuardsNonReady(t *testing.T) {
	m := seeded(core.TaskInfo{Task: "a", Status: "creating", Path: "/p/a"})
	m, _ = update(m, key("m"))
	if m.state != browsing {
		t.Errorf("must not act on a non-ready task; state = %v", m.state)
	}
	if m.lastErr == nil {
		t.Error("expected a guard error")
	}
}

func TestUpdateRmAllowedOnOrphaned(t *testing.T) {
	m := seeded(core.TaskInfo{Task: "a", Status: "orphaned", Path: "/p/a"})
	m, _ = update(m, key("x"))
	if m.state != confirming {
		t.Errorf("rm should be allowed on an orphaned task; state = %v", m.state)
	}
}

func TestUpdateWorkingIgnoresInput(t *testing.T) {
	m := seeded(core.TaskInfo{Task: "a", Status: "ready", Path: "/p/a"})
	m, _ = update(m, key("s")) // -> working
	before := m.state
	m, _ = update(m, key("m")) // a second mutation must be ignored
	if m.state != before {
		t.Errorf("input while working changed state to %v", m.state)
	}
}

func TestUpdateEnterChoosesPathAndQuits(t *testing.T) {
	m := seeded(core.TaskInfo{Task: "a", Status: "ready", Path: "/p/a"})
	m, cmd := update(m, key("enter"))
	if m.chosenCD != "/p/a" {
		t.Errorf("chosenCD = %q, want /p/a", m.chosenCD)
	}
	if cmd == nil {
		t.Fatal("enter should return a quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("enter should quit the program")
	}
}

func TestUpdateActionErrorSurvivesRefresh(t *testing.T) {
	m := seeded(core.TaskInfo{Task: "a", Status: "ready", Path: "/p/a"})
	m, _ = update(m, actionDoneMsg{op: "merge", task: "a", err: errors.New("boom")})
	if m.lastErr == nil {
		t.Fatal("action error should be set")
	}
	// A successful background refresh must NOT clear the action error.
	m, _ = update(m, tasksMsg{tasks: []core.TaskInfo{{Task: "a", Status: "ready"}}})
	if m.lastErr == nil {
		t.Error("a background refresh wrongly cleared the action error")
	}
}

func TestUpdateRefreshSkippedWhileFiltering(t *testing.T) {
	m := seeded(
		core.TaskInfo{Task: "alpha", Status: "ready"},
		core.TaskInfo{Task: "beta", Status: "ready"},
	)
	m, _ = update(m, key("/")) // engage the list filter
	if m.list.FilterState() != list.Filtering {
		t.Skip("filter did not engage in this bubbles version")
	}
	m, _ = update(m, tasksMsg{tasks: []core.TaskInfo{{Task: "gamma", Status: "ready"}}})
	for _, it := range m.list.Items() {
		if ti, ok := it.(taskItem); ok && ti.ti.Task == "gamma" {
			t.Error("refresh applied new items while the user was filtering")
		}
	}
}

func TestUpdateActionDoneRefreshes(t *testing.T) {
	m := seeded(core.TaskInfo{Task: "a", Status: "ready", Path: "/p/a"})
	m, _ = update(m, key("s"))
	m, cmd := update(m, actionDoneMsg{op: "sync", task: "a"})
	if m.state != browsing {
		t.Errorf("state after actionDone = %v, want browsing", m.state)
	}
	if cmd == nil {
		t.Error("actionDone should trigger a refresh")
	}
}
