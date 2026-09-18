package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cyakimov/treepi/internal/git"
	"github.com/cyakimov/treepi/internal/state"
)

// gitExecutor adapts *git.Client to state.Executor so undo can replay an op's
// inverse steps. Ref ops run at the repo root; worktree steps use the step's
// Path.
type gitExecutor struct {
	g    *git.Client
	root string
}

func (s *Service) executor() state.Executor { return gitExecutor{g: s.git, root: s.repo.Root} }

func (e gitExecutor) Apply(ctx context.Context, st state.Step) error {
	switch st.Kind {
	case state.StepSetRef:
		// Restore Ref to From, only if it still points at To (CAS).
		return e.g.UpdateRefCAS(ctx, e.root, st.Ref, st.From, st.To)
	case state.StepRewindTrunk:
		// If trunk is checked out, reset that worktree so HEAD+index+files move
		// together (a bare update-ref would corrupt it); else CAS the ref.
		wts, err := e.g.WorktreeList(ctx, e.root)
		if err != nil {
			return err
		}
		branch := strings.TrimPrefix(st.Ref, "refs/heads/")
		if wt, ok := git.FindWorktreeOnBranch(wts, branch); ok {
			return e.g.ResetHard(ctx, wt.Path, st.From)
		}
		return e.g.UpdateRefCAS(ctx, e.root, st.Ref, st.From, st.To)
	case state.StepCreateRef:
		return e.g.SetRef(ctx, e.root, st.Ref, st.From)
	case state.StepDeleteRef:
		_ = e.g.DeleteRef(ctx, e.root, st.Ref) // idempotent: ignore a missing ref
		return nil
	case state.StepRemoveWorktree:
		_ = e.g.WorktreeRemove(ctx, e.root, st.Path, true)
		return e.g.WorktreePrune(ctx, e.root)
	case state.StepAddWorktree:
		return e.addWorktree(ctx, st)
	case state.StepRestoreTree:
		return e.g.RestoreWorktreeFrom(ctx, st.Path, st.Snapshot)
	case state.StepResetHard:
		return e.g.ResetHard(ctx, st.Path, st.From)
	default:
		return nil
	}
}

func (e gitExecutor) addWorktree(ctx context.Context, st state.Step) error {
	wts, err := e.g.WorktreeList(ctx, e.root)
	if err != nil {
		return err
	}
	for _, wt := range wts {
		if filepath.Clean(wt.Path) == filepath.Clean(st.Path) {
			if wt.Branch == "refs/heads/"+st.Branch && wt.Head == st.From {
				if _, err := os.Stat(st.Path); err == nil {
					return nil
				}
			}
			return fmt.Errorf("cannot restore %s: target worktree differs from the recorded branch or commit", st.Path)
		}
	}
	if _, err := os.Stat(st.Path); err == nil {
		return fmt.Errorf("cannot restore %s: target path already exists", st.Path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	oid, err := e.g.ResolveRef(ctx, e.root, "refs/heads/"+st.Branch)
	switch {
	case err == nil && oid == st.From:
		return e.g.WorktreeAddExisting(ctx, e.root, st.Path, st.Branch)
	case err == nil:
		return fmt.Errorf("cannot restore %s: branch %s moved", st.Path, st.Branch)
	case errors.Is(err, git.ErrRefNotFound):
		return e.g.WorktreeAdd(ctx, e.root, st.Path, st.Branch, st.From)
	default:
		return err
	}
}

// CanRewindTrunk reports whether ref may be rewound from `to` back to `from` -
// i.e. no branch other than ref itself descends from `to`. Trunk is append-only
// once another branch has built on the merged tip.
func (e gitExecutor) CanRewindTrunk(ctx context.Context, ref, _, to string) (bool, error) {
	heads, err := e.g.ForEachHead(ctx, e.root)
	if err != nil {
		return false, err
	}
	for _, h := range heads {
		if h.Name == ref {
			continue
		}
		descends, err := e.g.IsAncestor(ctx, e.root, to, h.OID)
		if err != nil {
			return false, err
		}
		if descends {
			return false, nil
		}
	}
	return true, nil
}
