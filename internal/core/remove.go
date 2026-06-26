package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cyakimov/treepi/internal/exit"
	"github.com/cyakimov/treepi/internal/git"
	"github.com/cyakimov/treepi/internal/state"
)

// RemoveResult reports a removed task.
type RemoveResult struct {
	Task   string `json:"task"`
	Branch string `json:"branch"`
}

// Remove discards a task's worktree and branch. It refuses to remove the
// worktree the caller is standing in, respects another owner's lease (unless
// forced), and refuses a dirty tree without --force. It always snapshots first,
// so even a forced discard is recoverable via undo; on a forced discard it warns
// which gitignored paths will not be recoverable.
func (s *Service) Remove(ctx context.Context, task string, force bool) (*RemoveResult, error) {
	m, err := s.store.View()
	if err != nil {
		return nil, err
	}
	t := m.Tasks[task]
	if t == nil {
		return nil, exit.New(exit.NotFound, "not_found", "unknown task: "+task)
	}
	dir, branch, root := t.Path, t.Branch, s.repo.Root
	before := *t

	if cwd, werr := os.Getwd(); werr == nil {
		c, d := filepath.Clean(cwd), filepath.Clean(dir)
		if c == d || strings.HasPrefix(c, d+string(filepath.Separator)) {
			return nil, exit.New(exit.Refused, "cwd_in_target",
				"refusing to remove the worktree you are inside; cd elsewhere first")
		}
	}
	if t.Lease != nil && t.Lease.Owner != defaultOwner() && !force {
		return nil, exit.New(exit.LeaseHeld, "lease_held",
			"task is leased by "+t.Lease.Owner+" (use --force)")
	}

	clean, _ := s.git.IsClean(ctx, dir)
	if !clean && !force {
		return nil, exit.New(exit.Refused, "dirty",
			"worktree has uncommitted changes (use --force; work is snapshotted regardless)")
	}

	ident := git.Identity{Name: defaultOwner()}
	branchOID, _ := s.git.ResolveRef(ctx, dir, "HEAD")

	// Snapshot first so undo can recover even a forced discard.
	snapRef := state.SnapshotRef(task, "prerm", s.clock.NewID())
	_, snapCreated, _ := s.git.Snapshot(ctx, dir, snapRef, ident, s.cfg.IncludeIgnored)

	if force && !clean {
		if ig, _ := s.git.IgnoredPaths(ctx, dir); len(ig) > 0 {
			fmt.Fprintf(s.warn, "treepi: %d gitignored path(s) will NOT be recoverable by undo (e.g. %s)\n", len(ig), ig[0])
		}
	}

	// (pre_remove hook runs here once hooks land at task 7.)
	_ = s.git.WorktreeRemove(ctx, root, dir, force)
	_ = s.git.WorktreePrune(ctx, root)
	_ = s.git.BranchDelete(ctx, root, branch, true) // recoverable via undo (branchOID)

	steps := []state.Step{{Kind: state.StepAddWorktree, Path: dir, Branch: branch, From: branchOID}}
	if snapCreated {
		steps = append(steps, state.Step{Kind: state.StepRestoreTree, Path: dir, Snapshot: snapRef})
	}
	opID := s.clock.NewID()
	if err := s.store.Do(ctx, nil, func(tx *state.Txn) error {
		delete(tx.Manifest().Tasks, task)
		tx.AppendBegin(&state.Op{
			ID: opID, Kind: "rm", StartedAt: s.clock.Now(), Phase: "removed",
			Steps: steps, TasksBefore: map[string]*state.Task{task: &before},
		})
		tx.AppendCommit(opID, "removed")
		tx.MarkDirty()
		return nil
	}); err != nil {
		return nil, err
	}
	return &RemoveResult{Task: task, Branch: branch}, nil
}
