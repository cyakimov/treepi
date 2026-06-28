package state

import (
	"context"

	"github.com/cyakimov/treepi/internal/exit"
)

// Executor applies inverse steps to git. core wires this to *git.Client so the
// undo logic and journal stay in state while git side effects stay in git.
type Executor interface {
	Apply(ctx context.Context, step Step) error
	// CanRewindTrunk reports whether ref can be safely rewound from `to` back to
	// `from` - i.e. no other branch descends from `to`. Trunk is append-only
	// once published.
	CanRewindTrunk(ctx context.Context, ref, from, to string) (bool, error)
}

// Undo reverses the last committed op by replaying its inverse steps, then
// journals an undo marker so the same op is never reversed twice. It refuses
// (exit 3, undo_unsafe) to rewind a published trunk. It returns the reversed
// op, or (nil, nil) when there is nothing to undo.
//
// Following the two-tier model, the git side effects run outside the state lock;
// only the marker append is locked.
func (s *Store) Undo(ctx context.Context, exec Executor) (*Op, error) {
	op, err := s.LastCommittedOp()
	if err != nil {
		return nil, err
	}
	if op == nil || op.Kind == "undo" {
		return nil, nil // nothing to undo (or the last op was itself an undo)
	}

	// Guard published-trunk rewinds before mutating anything.
	for _, st := range op.Steps {
		if st.Kind != StepRewindTrunk {
			continue
		}
		ok, err := exec.CanRewindTrunk(ctx, st.Ref, st.From, st.To)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, exit.New(exit.Refused, "undo_unsafe",
				"refusing to rewind "+st.Ref+": another branch has built on it")
		}
	}

	for _, st := range op.Steps {
		if err := exec.Apply(ctx, st); err != nil {
			return nil, err
		}
	}

	marker := &Op{ID: op.ID + ".undo", Kind: "undo", StartedAt: s.clock.Now()}
	if err := s.Do(ctx, nil, func(tx *Txn) error {
		m := tx.Manifest()
		for name, before := range op.TasksBefore {
			if before == nil {
				delete(m.Tasks, name)
			} else {
				m.Tasks[name] = before
			}
		}
		tx.AppendBegin(marker)
		tx.AppendCommit(marker.ID, "done")
		return nil
	}); err != nil {
		return nil, err
	}
	return op, nil
}
