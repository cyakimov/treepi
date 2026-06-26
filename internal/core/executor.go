package core

import (
	"context"
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
		return e.g.WorktreeAdd(ctx, e.root, st.Path, st.Branch, st.From)
	case state.StepRestoreTree:
		return e.g.RestoreWorktreeFrom(ctx, st.Path, st.Snapshot)
	case state.StepResetHard:
		return e.g.ResetHard(ctx, st.Path, st.From)
	default:
		return nil
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
