package state

import (
	"context"
	"testing"

	"github.com/cyakimov/treepi/internal/exit"
)

type fakeExecutor struct {
	applied   []Step
	canRewind bool
}

func (f *fakeExecutor) Apply(_ context.Context, st Step) error {
	f.applied = append(f.applied, st)
	return nil
}

func (f *fakeExecutor) CanRewindTrunk(_ context.Context, _, _, _ string) (bool, error) {
	return f.canRewind, nil
}

func TestUndoNoOp(t *testing.T) {
	s := newTestStore(t)
	op, err := s.Undo(context.Background(), &fakeExecutor{})
	if err != nil || op != nil {
		t.Fatalf("empty journal: got op=%+v err=%v, want nil,nil", op, err)
	}
}

func TestUndoAppliesStepsThenBlocksRepeat(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	steps := []Step{
		{Kind: StepRemoveWorktree, Path: "/p/x"},
		{Kind: StepDeleteRef, Ref: "refs/heads/feat/x"},
	}
	seedOp(t, s, &Op{ID: "op1", Kind: "new", Steps: steps}, true)

	exec := &fakeExecutor{}
	op, err := s.Undo(ctx, exec)
	if err != nil || op == nil || op.ID != "op1" {
		t.Fatalf("undo: op=%+v err=%v", op, err)
	}
	if len(exec.applied) != 2 || exec.applied[0].Kind != StepRemoveWorktree || exec.applied[1].Kind != StepDeleteRef {
		t.Fatalf("steps not applied in order: %+v", exec.applied)
	}

	// A second undo is a no-op: the marker shadows op1.
	exec2 := &fakeExecutor{}
	op2, err := s.Undo(ctx, exec2)
	if err != nil || op2 != nil || len(exec2.applied) != 0 {
		t.Fatalf("double undo should no-op: op=%+v err=%v applied=%+v", op2, err, exec2.applied)
	}
}

func TestUndoRefusesPublishedTrunkRewind(t *testing.T) {
	s := newTestStore(t)
	seedOp(t, s, &Op{ID: "merge1", Kind: "merge", Steps: []Step{
		{Kind: StepRewindTrunk, Ref: "refs/heads/main", From: "old", To: "new"},
	}}, true)

	exec := &fakeExecutor{canRewind: false}
	_, err := s.Undo(context.Background(), exec)
	if exit.CodeOf(err) != exit.Refused {
		t.Fatalf("got code %d (%v), want Refused", exit.CodeOf(err), err)
	}
	if len(exec.applied) != 0 {
		t.Fatalf("no steps should be applied when the guard refuses: %+v", exec.applied)
	}
}
