package state

import (
	"context"
	"path/filepath"
	"time"
)

// WorktreeView is the structural slice of a git worktree that Reconcile needs.
type WorktreeView struct {
	Path     string
	Branch   string // short name; "" if detached
	Detached bool
	Locked   bool
	Prunable bool
}

// Reconciler supplies the live worktree set and prunes deleted ones. core wires
// this to *git.Client, so state never shells out to git directly.
type Reconciler interface {
	ListWorktrees(ctx context.Context) ([]WorktreeView, error)
	Prune(ctx context.Context) error
}

// LivenessFunc reports whether a lease's owner is still alive. Same-host leases
// are probed by pid; cross-host leases return true (only TTL frees them).
type LivenessFunc func(l *Lease, now time.Time) bool

// reconcile runs the structural pass under the flock: it prunes git metadata
// for deleted worktrees, then diffs the live worktree set against the manifest.
func (s *Store) reconcile(ctx context.Context, m *Manifest, rec Reconciler) (bool, error) {
	views, err := rec.ListWorktrees(ctx)
	if err != nil {
		return false, err
	}
	prunable := false
	for _, v := range views {
		if v.Prunable {
			prunable = true
			break
		}
	}
	if prunable {
		if err := rec.Prune(ctx); err != nil {
			return false, err
		}
		if views, err = rec.ListWorktrees(ctx); err != nil {
			return false, err
		}
	}
	return Reconcile(m, views, s.clock.Now(), s.live), nil
}

// Reconcile is the pure structural pass: it clears stale leases, marks tasks
// whose worktree has vanished (or whose create died) as orphaned, and leaves an
// in-flight create with a live owner untouched (M7). It never deletes branches
// or worktrees - that is a caller/recovery concern. Returns whether it changed
// the manifest.
func Reconcile(m *Manifest, views []WorktreeView, now time.Time, live LivenessFunc) bool {
	byPath := make(map[string]bool, len(views))
	for _, v := range views {
		byPath[filepath.Clean(v.Path)] = true
	}
	changed := false
	for _, t := range m.Tasks {
		hasTree := byPath[filepath.Clean(t.Path)]
		leaseLive := t.Lease != nil && !t.Lease.Expired(now) && (live == nil || live(t.Lease, now))

		// An in-flight create with a live owner is hands-off.
		if t.Status == StatusCreating && leaseLive {
			continue
		}
		if t.Lease != nil && !leaseLive {
			t.Lease = nil
			changed = true
		}
		switch {
		case t.Status == StatusCreating: // create died before flipping to ready
			t.Status = StatusOrphaned
			changed = true
		case !hasTree && t.Status != StatusOrphaned:
			t.Status = StatusOrphaned
			changed = true
		}
	}
	return changed
}
