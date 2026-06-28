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

// createGraceWindow bounds how long an in-flight create is left untouched before
// Reconcile treats a still-"creating" task with no live worktree as orphaned.
// The slow worktree add + post_create hook run unlocked, so the window only has
// to exceed a normal create; a crashed create is recovered once it lapses (and
// is undo-/rm-able regardless, since orphaning never destroys anything).
const createGraceWindow = 15 * time.Minute

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
	return Reconcile(m, views, s.clock.Now()), nil
}

// Reconcile is the pure structural pass: it marks tasks whose worktree has
// vanished (or whose create died) as orphaned, leaving a recent in-flight create
// untouched (within createGraceWindow, since the worktree add runs unlocked). It
// never deletes branches or worktrees - that is a caller/recovery concern.
// Returns whether it changed the manifest.
func Reconcile(m *Manifest, views []WorktreeView, now time.Time) bool {
	byPath := make(map[string]bool, len(views))
	for _, v := range views {
		byPath[filepath.Clean(v.Path)] = true
	}
	changed := false
	for _, t := range m.Tasks {
		hasTree := byPath[filepath.Clean(t.Path)]

		// A recent in-flight create is hands-off; the slow worktree add + hook run
		// unlocked, so CreatedAt bounds how long we wait before orphaning it.
		if t.Status == StatusCreating && now.Sub(t.CreatedAt) < createGraceWindow {
			continue
		}
		switch {
		case t.Status == StatusCreating: // create died (or stalled past the window)
			t.Status = StatusOrphaned
			changed = true
		case !hasTree && t.Status != StatusOrphaned:
			t.Status = StatusOrphaned
			changed = true
		}
	}
	return changed
}
