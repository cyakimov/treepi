package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/cyakimov/treepi/internal/config"
	"github.com/cyakimov/treepi/internal/exit"
	"github.com/cyakimov/treepi/internal/git"
	"github.com/cyakimov/treepi/internal/state"
)

// MergeResult reports a completed merge.
type MergeResult struct {
	Task   string `json:"task"`
	Branch string `json:"branch"`
	Trunk  string `json:"trunk"`
	Merged string `json:"merged"` // the new trunk OID
}

// Merge integrates a task's branch into trunk: snapshot -> rebase onto the
// resolved base -> verify on the rebased tip -> fast-forward trunk (conditional
// on whether trunk is checked out) -> remove the worktree + branch. The rebase
// and verify run lock-free; trunk advances via a CAS update-ref when trunk is
// not checked out, or `merge --ff-only` from the trunk worktree when it is.
// On any failure before trunk advances, the work is preserved (recover-to-main);
// the completed merge is journaled with a guarded trunk-rewind inverse for undo.
func (s *Service) Merge(ctx context.Context, task string) (*MergeResult, error) {
	m, err := s.store.View()
	if err != nil {
		return nil, err
	}
	t := m.Tasks[task]
	if t == nil {
		return nil, exit.New(exit.NotFound, "not_found", "unknown task: "+task)
	}
	dir, branch, trunk, root := t.Path, t.Branch, s.repo.Trunk, s.repo.Root
	before := *t
	before.Status = state.StatusReady
	before.Lease = nil

	// Guards (refuse before touching anything).
	if branch == trunk {
		return nil, exit.New(exit.Refused, "on_trunk", "refusing to merge the trunk")
	}
	if clean, _ := s.git.IsClean(ctx, dir); !clean {
		return nil, exit.New(exit.Refused, "uncommitted", "feature worktree has uncommitted changes")
	}
	wts, err := s.git.WorktreeList(ctx, root)
	if err != nil {
		return nil, err
	}
	trunkWt, trunkCheckedOut := git.FindWorktreeOnBranch(wts, trunk)
	if trunkCheckedOut {
		if clean, _ := s.git.IsClean(ctx, trunkWt.Path); !clean {
			return nil, exit.New(exit.Refused, "dirty_trunk", "trunk worktree has uncommitted changes")
		}
	}

	trunkOID, err := s.git.ResolveRef(ctx, root, "refs/heads/"+trunk)
	if err != nil {
		return nil, exit.New(exit.NoTrunk, "no_trunk", "could not resolve trunk "+trunk)
	}
	ident := git.Identity{Name: defaultOwner()}

	// Mark merging for crash visibility (reconcile surfaces it, never auto-resumes).
	_ = s.setStatus(ctx, task, state.StatusMerging)

	// Snapshot the pre-merge tip.
	_, _, _ = s.git.Snapshot(ctx, dir, state.SnapshotRef(task, "premerge", s.clock.NewID()), ident, s.cfg.IncludeIgnored)

	// Resolve base and rebase (lock-free).
	base, offline, err := s.resolveBase(ctx)
	if err != nil {
		s.unmerge(ctx, task)
		return nil, err
	}
	if offline {
		fmt.Fprintf(s.warn, "treepi: rebasing onto local %s (offline / no remote)\n", trunk)
	}
	if rerr := s.git.Rebase(ctx, dir, base); rerr != nil {
		if errors.Is(rerr, git.ErrConflict) {
			return nil, s.markConflict(ctx, task, dir, ident)
		}
		s.unmerge(ctx, task)
		return nil, exit.Wrap(exit.Internal, "rebase_failed", "rebase failed", rerr)
	}
	rebasedOID, err := s.git.ResolveRef(ctx, dir, "HEAD")
	if err != nil {
		return nil, err
	}

	// Verify on the rebased tip (lock-free). Failure leaves the branch rebased.
	// This is the simple argv gate (exit 8); the pre_merge hook below is the
	// full-env lifecycle gate (exit 9). Verify runs first.
	if len(s.cfg.Verify) > 0 {
		if verr := s.runVerify(ctx, dir, s.cfg.Verify); verr != nil {
			s.unmerge(ctx, task)
			return nil, exit.Wrap(exit.VerifyFailed, "verify_failed",
				"verify failed; merge aborted, branch left rebased", verr)
		}
	}

	opID := s.clock.NewID()

	// pre_merge hook on the rebased tip (default abort). Failure leaves the branch
	// rebased and trunk untouched - nothing tracked is lost.
	preCtx := s.baseHookContext(ctx, config.EventPreMerge, task, t.Type, branch, dir, t.Slot)
	preCtx.Op, preCtx.OldHead, preCtx.NewHead = opID, trunkOID, rebasedOID
	if _, spec, herr := s.fireHook(ctx, preCtx); herr != nil {
		if spec.OnFailure != config.OnFailureWarn {
			s.unmerge(ctx, task)
			return nil, exit.Wrap(exit.HookAbort, "pre_merge_aborted",
				"pre_merge hook failed; merge aborted, branch left rebased", herr)
		}
		s.warnf("pre_merge hook failed (proceeding, on_failure=warn): %v", herr)
	}

	// Fast-forward trunk, conditional on its checkout state.
	if trunkCheckedOut {
		if merr := s.git.MergeFFOnly(ctx, trunkWt.Path, branch); merr != nil {
			s.unmerge(ctx, task)
			return nil, exit.New(exit.Conflict, "non_ff",
				"trunk advanced during merge; run `tp sync "+task+"` and retry")
		}
	} else {
		cur, _ := s.git.ResolveRef(ctx, root, "refs/heads/"+trunk)
		if ancestor, _ := s.git.IsAncestor(ctx, root, cur, rebasedOID); !ancestor {
			s.unmerge(ctx, task)
			return nil, exit.New(exit.Conflict, "non_ff",
				"trunk advanced during merge; run `tp sync "+task+"` and retry")
		}
		if uerr := s.git.UpdateRefCAS(ctx, root, "refs/heads/"+trunk, rebasedOID, cur); uerr != nil {
			s.unmerge(ctx, task)
			return nil, exit.New(exit.Conflict, "non_ff", "trunk moved during merge; retry")
		}
	}
	// --- trunk has advanced past here: do not roll back (recover forward) ---

	// Cleanup: remove the tree first (un-checks-out the branch), then delete the
	// branch only after proving it is merged into trunk.
	_ = s.git.WorktreeRemove(ctx, root, dir, false)
	_ = s.git.WorktreePrune(ctx, root)
	if merged, _ := s.git.IsAncestor(ctx, root, "refs/heads/"+branch, "refs/heads/"+trunk); merged {
		_ = s.git.DeleteRef(ctx, root, "refs/heads/"+branch)
	} else {
		_ = s.git.BranchDelete(ctx, root, branch, true)
	}

	// post_merge hook (warn-only; trunk has advanced, nothing to roll back). The
	// feature tree is gone, so the hook runs in the trunk worktree (or repo root).
	postCtx := s.baseHookContext(ctx, config.EventPostMerge, task, t.Type, branch, dir, t.Slot)
	postCtx.Op, postCtx.OldHead, postCtx.NewHead = opID, trunkOID, rebasedOID
	postCtx.WorktreePath = postCtx.MainPath
	if postCtx.WorktreePath == "" {
		postCtx.WorktreePath = root
	}
	if _, _, herr := s.fireHook(ctx, postCtx); herr != nil {
		s.warnf("post_merge hook failed (merge already committed): %v", herr)
	}

	// Finalize: drop the task and journal the merge with its guarded inverse.
	if err := s.store.Do(ctx, nil, func(tx *state.Txn) error {
		delete(tx.Manifest().Tasks, task)
		tx.AppendBegin(&state.Op{
			ID: opID, Kind: "merge", StartedAt: s.clock.Now(), Phase: "trunk-merged",
			Steps: []state.Step{
				{Kind: state.StepRewindTrunk, Ref: "refs/heads/" + trunk, From: trunkOID, To: rebasedOID},
				// AddWorktree -b recreates the branch and the tree together.
				{Kind: state.StepAddWorktree, Path: dir, Branch: branch, From: rebasedOID},
			},
			TasksBefore: map[string]*state.Task{task: &before},
		})
		tx.AppendCommit(opID, "merged")
		tx.MarkDirty()
		return nil
	}); err != nil {
		return nil, err
	}

	return &MergeResult{Task: task, Branch: branch, Trunk: trunk, Merged: rebasedOID}, nil
}

// setStatus updates a task's status under the lock.
func (s *Service) setStatus(ctx context.Context, task string, st state.Status) error {
	return s.store.Do(ctx, nil, func(tx *state.Txn) error {
		if t := tx.Manifest().Tasks[task]; t != nil {
			t.Status = st
			tx.MarkDirty()
		}
		return nil
	})
}

// unmerge resets a task from "merging" back to "ready" after an aborted merge
// (used only before trunk has advanced).
func (s *Service) unmerge(ctx context.Context, task string) {
	_ = s.store.Do(ctx, nil, func(tx *state.Txn) error {
		if t := tx.Manifest().Tasks[task]; t != nil && t.Status == state.StatusMerging {
			t.Status = state.StatusReady
			tx.MarkDirty()
		}
		return nil
	})
}

// runVerify executes the configured verify command in dir, streaming its output
// to stderr so stdout stays clean for --json. A non-zero exit is the error.
func (s *Service) runVerify(ctx context.Context, dir string, argv []string) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	cmd.Stdout = s.warn
	cmd.Stderr = s.warn
	return cmd.Run()
}
