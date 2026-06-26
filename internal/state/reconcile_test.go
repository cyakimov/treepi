package state

import (
	"testing"
	"time"
)

func alwaysAlive(*Lease, time.Time) bool { return true }
func neverAlive(*Lease, time.Time) bool  { return false }

var now = time.Unix(1000, 0).UTC()

func TestReconcileOrphansVanishedTree(t *testing.T) {
	m := newManifest()
	m.Tasks["x"] = &Task{Name: "x", Path: "/p/x", Status: StatusReady}
	if changed := Reconcile(m, nil, now, alwaysAlive); !changed {
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
	if changed := Reconcile(m, views, now, alwaysAlive); changed {
		t.Fatal("a present tree should not change")
	}
	if m.Tasks["x"].Status != StatusReady {
		t.Fatalf("status = %s, want ready", m.Tasks["x"].Status)
	}
}

func TestReconcileLeavesLiveCreating(t *testing.T) {
	m := newManifest()
	// no view yet (worktree still being added), but the create's owner is live
	m.Tasks["x"] = &Task{Name: "x", Path: "/p/x", Status: StatusCreating,
		Lease: &Lease{Expires: now.Add(time.Hour)}}
	if changed := Reconcile(m, nil, now, alwaysAlive); changed {
		t.Fatal("a live in-flight create must be hands-off")
	}
	if m.Tasks["x"].Status != StatusCreating || m.Tasks["x"].Lease == nil {
		t.Fatalf("got %+v", m.Tasks["x"])
	}
}

func TestReconcileOrphansDeadCreating(t *testing.T) {
	m := newManifest()
	m.Tasks["x"] = &Task{Name: "x", Path: "/p/x", Status: StatusCreating,
		Lease: &Lease{Expires: now.Add(time.Hour)}}
	if changed := Reconcile(m, nil, now, neverAlive); !changed {
		t.Fatal("expected change")
	}
	if m.Tasks["x"].Status != StatusOrphaned || m.Tasks["x"].Lease != nil {
		t.Fatalf("dead create must orphan + clear lease: %+v", m.Tasks["x"])
	}
}

func TestReconcileClearsExpiredLease(t *testing.T) {
	m := newManifest()
	m.Tasks["x"] = &Task{Name: "x", Path: "/p/x", Status: StatusReady,
		Lease: &Lease{Expires: now.Add(-time.Hour)}}
	views := []WorktreeView{{Path: "/p/x"}}
	if changed := Reconcile(m, views, now, alwaysAlive); !changed {
		t.Fatal("expected lease cleared")
	}
	if m.Tasks["x"].Lease != nil {
		t.Fatal("expired lease must be cleared")
	}
}

func TestReconcileKeepsDurableLeaseThenClearsExpired(t *testing.T) {
	live := DefaultLiveness("this-host")
	m := newManifest()
	// A PID=0 (durable / claim heartbeat) lease: the claiming process has exited,
	// so it must NOT be probed - only TTL frees it.
	m.Tasks["x"] = &Task{Name: "x", Path: "/p/x", Status: StatusReady,
		Lease: &Lease{Owner: "agent", PID: 0, Host: "this-host", Expires: now.Add(time.Hour)}}
	views := []WorktreeView{{Path: "/p/x"}}
	if changed := Reconcile(m, views, now, live); changed {
		t.Fatal("a durable unexpired lease must survive reconcile")
	}
	if m.Tasks["x"].Lease == nil {
		t.Fatal("durable lease was wrongly cleared")
	}
	// Once it expires, TTL frees it.
	m.Tasks["x"].Lease.Expires = now.Add(-time.Minute)
	if changed := Reconcile(m, views, now, live); !changed {
		t.Fatal("expected the expired durable lease to be cleared")
	}
	if m.Tasks["x"].Lease != nil {
		t.Fatal("expired durable lease must be cleared")
	}
}

func TestReconcileKeepsCrossHostLease(t *testing.T) {
	m := newManifest()
	m.Tasks["x"] = &Task{Name: "x", Path: "/p/x", Status: StatusReady,
		Lease: &Lease{Host: "other-host", Expires: now.Add(time.Hour)}}
	views := []WorktreeView{{Path: "/p/x"}}
	live := DefaultLiveness("this-host") // host mismatch -> treated alive
	if changed := Reconcile(m, views, now, live); changed {
		t.Fatal("an unexpired cross-host lease must be kept")
	}
	if m.Tasks["x"].Lease == nil {
		t.Fatal("cross-host lease was wrongly cleared")
	}
}
