package state

import (
	"testing"
	"time"
)

var now = time.Unix(1000, 0).UTC()

func TestReconcileOrphansVanishedTree(t *testing.T) {
	m := newManifest()
	m.Tasks["x"] = &Task{Name: "x", Path: "/p/x", Status: StatusReady}
	if changed := Reconcile(m, nil, now); !changed {
		t.Fatal("expected change")
	}
	if m.Tasks["x"].Status != StatusOrphaned {
		t.Fatalf("status = %s, want orphaned", m.Tasks["x"].Status)
	}
}

func TestReconcileKeepsLiveTree(t *testing.T) {
	m := newManifest()
	m.Tasks["x"] = &Task{Name: "x", Path: "/p/x", Status: StatusReady}
	views := []WorktreeView{{Path: "/p/x", Branch: "feat/x"}}
	if changed := Reconcile(m, views, now); changed {
		t.Fatal("a present tree should not change")
	}
	if m.Tasks["x"].Status != StatusReady {
		t.Fatalf("status = %s, want ready", m.Tasks["x"].Status)
	}
}

func TestReconcileLeavesRecentCreating(t *testing.T) {
	m := newManifest()
	// No view yet (worktree still being added), but the create is recent.
	m.Tasks["x"] = &Task{Name: "x", Path: "/p/x", Status: StatusCreating, CreatedAt: now}
	if changed := Reconcile(m, nil, now); changed {
		t.Fatal("a recent in-flight create must be hands-off")
	}
	if m.Tasks["x"].Status != StatusCreating {
		t.Fatalf("status = %s, want creating", m.Tasks["x"].Status)
	}
}

func TestReconcileOrphansStaleCreating(t *testing.T) {
	m := newManifest()
	// A create older than the grace window with no worktree: the creator is gone.
	m.Tasks["x"] = &Task{Name: "x", Path: "/p/x", Status: StatusCreating,
		CreatedAt: now.Add(-createGraceWindow - time.Minute)}
	if changed := Reconcile(m, nil, now); !changed {
		t.Fatal("expected change")
	}
	if m.Tasks["x"].Status != StatusOrphaned {
		t.Fatalf("stale create must orphan: %+v", m.Tasks["x"])
	}
}
