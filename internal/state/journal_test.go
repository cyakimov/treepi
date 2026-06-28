package state

import (
	"context"
	"os"
	"testing"
)

func seedOp(t *testing.T, s *Store, op *Op, commit bool) {
	t.Helper()
	err := s.Do(context.Background(), nil, func(tx *Txn) error {
		tx.AppendBegin(op)
		if commit {
			tx.AppendCommit(op.ID, "done")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestJournalCommittedAndIncomplete(t *testing.T) {
	s := newTestStore(t)

	seedOp(t, s, &Op{ID: "op1", Kind: "new", Steps: []Step{{Kind: StepDeleteRef, Ref: "refs/heads/feat/x"}}}, true)
	seedOp(t, s, &Op{ID: "op2", Kind: "merge"}, false) // began, never committed

	last, err := s.LastCommittedOp()
	if err != nil {
		t.Fatal(err)
	}
	if last == nil || last.ID != "op1" {
		t.Fatalf("LastCommittedOp = %+v, want op1", last)
	}

	inc, err := s.IncompleteOp()
	if err != nil {
		t.Fatal(err)
	}
	if inc == nil || inc.ID != "op2" {
		t.Fatalf("IncompleteOp = %+v, want op2", inc)
	}
}

func TestJournalToleratesTornLine(t *testing.T) {
	s := newTestStore(t)
	seedOp(t, s, &Op{ID: "ok", Kind: "new"}, true)

	// Simulate a crash mid-append: a partial JSON line at the end.
	f, err := os.OpenFile(s.journalPath(), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"rec":"begin","op":{"id":"torn"`); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	last, err := s.LastCommittedOp()
	if err != nil {
		t.Fatalf("torn line should be tolerated, got %v", err)
	}
	if last == nil || last.ID != "ok" {
		t.Fatalf("LastCommittedOp = %+v, want ok", last)
	}
}

func TestSnapshotRefNaming(t *testing.T) {
	if got := SnapshotRef("login-fix", "premerge", "abc"); got != "refs/treepi/snapshots/login-fix/premerge-abc" {
		t.Errorf("SnapshotRef = %q", got)
	}
	if got := ConflictRef("login-fix", "abc"); got != "refs/treepi/conflicts/login-fix/abc" {
		t.Errorf("ConflictRef = %q", got)
	}
}
