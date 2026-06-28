package core

import (
	"context"
	"errors"

	"github.com/cyakimov/treepi/internal/config"
	"github.com/cyakimov/treepi/internal/exit"
	"github.com/cyakimov/treepi/internal/git"
	"github.com/cyakimov/treepi/internal/state"
)

// Sync rebases a task's worktree onto the latest trunk. It snapshots dirty work
// first (nothing tracked is lost), and on conflict captures the conflicted tree,
// aborts to a clean state, and marks the task needs-resolution
// (conflict-as-data) - leaving sibling worktrees untouched.
func (s *Service) Sync(ctx context.Context, task string) (*TaskInfo, error) {
	m, err := s.store.View()
	if err != nil {
		return nil, err
	}
	t := m.Tasks[task]
	if t == nil {
		return nil, exit.New(exit.NotFound, "not_found", "unknown task: "+task)
	}
	dir := t.Path

	branch, err := s.git.SymbolicHEAD(ctx, dir)
	if err != nil {
		return nil, err
	}
	if branch == s.repo.Trunk {
		return nil, exit.New(exit.Refused, "on_trunk", "refusing to sync the trunk worktree")
	}

	ident := git.Identity{Name: defaultOwner()}

	// Snapshot dirty work first.
	snapRef := ""
	if clean, _ := s.git.IsClean(ctx, dir); !clean {
		ref := state.SnapshotRef(task, "sync", s.clock.NewID())
		if _, created, serr := s.git.Snapshot(ctx, dir, ref, ident, s.cfg.IncludeIgnored); serr == nil && created {
			snapRef = ref
		}
	}

	preOID, err := s.git.ResolveRef(ctx, dir, "HEAD")
	if err != nil {
		return nil, err
	}

	base, err := s.resolveBase(ctx)
	if err != nil {
		return nil, err
	}

	if rerr := s.git.Rebase(ctx, dir, base); rerr != nil {
		if errors.Is(rerr, git.ErrConflict) {
			return nil, s.markConflict(ctx, task, dir, ident)
		}
		return nil, exit.Wrap(exit.Internal, "rebase_failed", "rebase failed", rerr)
	}

	opID := s.clock.NewID()

	// post_sync hook. The rebase is already durable, so any failure is a warning
	// (a configured abort/rollback degrades to warn - there is nothing safe to
	// undo here).
	newHead, _ := s.git.ResolveRef(ctx, dir, "HEAD")
	hc := s.baseHookContext(ctx, config.EventPostSync, t.Name, t.Type, branch, dir)
	hc.Op, hc.OldHead, hc.NewHead = opID, preOID, newHead
	if _, herr := s.fireHook(ctx, hc); herr != nil {
		s.warnf("post_sync hook failed (rebase kept, not rolled back): %v", herr)
	}

	var info *TaskInfo
	if err := s.store.Do(ctx, nil, func(tx *state.Txn) error {
		t := tx.Manifest().Tasks[task]
		if t == nil {
			return exit.New(exit.Internal, "task_vanished", "task vanished during sync")
		}
		before := *t
		steps := []state.Step{{Kind: state.StepResetHard, Path: dir, From: preOID}}
		if snapRef != "" {
			steps = append(steps, state.Step{Kind: state.StepRestoreTree, Path: dir, Snapshot: snapRef})
		}
		t.Status = state.StatusReady
		t.BaseSha = base
		tx.AppendBegin(&state.Op{
			ID: opID, Kind: "sync", StartedAt: s.clock.Now(), Phase: "rebased",
			Steps: steps, TasksBefore: map[string]*state.Task{task: &before},
		})
		tx.AppendCommit(opID, "synced")
		tx.MarkDirty()
		info = taskInfo(t)
		return nil
	}); err != nil {
		return nil, err
	}
	return info, nil
}

// markConflict captures the conflicted tree, aborts the rebase to a clean state,
// and marks the task needs-resolution (conflict-as-data). Shared by sync and
// merge. Returns the conflict error.
func (s *Service) markConflict(ctx context.Context, task, dir string, ident git.Identity) error {
	_, _, _ = s.git.Snapshot(ctx, dir, state.ConflictRef(task, s.clock.NewID()), ident, s.cfg.IncludeIgnored)
	_ = s.git.RebaseAbort(ctx, dir)
	_ = s.store.Do(ctx, nil, func(tx *state.Txn) error {
		if t := tx.Manifest().Tasks[task]; t != nil {
			t.Status = state.StatusNeedsResolution
			tx.MarkDirty()
		}
		return nil
	})
	return exit.New(exit.Conflict, "conflict",
		"rebase hit conflicts; tree left clean, task marked needs-resolution")
}
