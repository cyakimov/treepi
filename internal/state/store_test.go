package state

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/gofrs/flock"

	"github.com/cyakimov/treepi/internal/exit"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir(), fixedClock{t: time.Unix(1000, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStoreDoPersistsOnDirty(t *testing.T) {
	s := newTestStore(t)
	err := s.Do(context.Background(), nil, func(tx *Txn) error {
		tx.Manifest().Tasks["x"] = &Task{Name: "x", Type: "feat", Branch: "feat/x", Status: StatusReady}
		tx.MarkDirty()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.View()
	if err != nil {
		t.Fatal(err)
	}
	if m.Tasks["x"] == nil {
		t.Fatal("task not persisted")
	}
	if m.Generation != 1 {
		t.Fatalf("generation = %d, want 1", m.Generation)
	}
}

func TestStoreNoWriteWhenClean(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_ = s.Do(ctx, nil, func(tx *Txn) error { tx.MarkDirty(); return nil }) // gen -> 1
	_ = s.Do(ctx, nil, func(tx *Txn) error { return nil })                 // no change
	m, _ := s.View()
	if m.Generation != 1 {
		t.Fatalf("generation bumped without a change: %d", m.Generation)
	}
}

func TestStoreGenerationCAS(t *testing.T) {
	s := newTestStore(t)
	err := s.Do(context.Background(), nil, func(tx *Txn) error {
		// A non-flock writer bumps the on-disk generation mid-transaction.
		m := newManifest()
		m.Generation = 7
		b, _ := json.MarshalIndent(m, "", "  ")
		if werr := os.WriteFile(s.manifestPath(), b, 0o644); werr != nil {
			t.Fatal(werr)
		}
		tx.MarkDirty()
		return nil
	})
	if !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("got %v, want ErrGenerationConflict", err)
	}
}

func TestStoreDoLockBusy(t *testing.T) {
	s := newTestStore(t)
	other := flock.New(s.lockPath())
	ok, err := other.TryLock()
	if err != nil || !ok {
		t.Fatalf("pre-lock failed: ok=%v err=%v", ok, err)
	}
	defer func() { _ = other.Unlock() }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err = s.Do(ctx, nil, func(tx *Txn) error { return nil })
	if exit.CodeOf(err) != exit.LockBusy {
		t.Fatalf("got code %d (%v), want LockBusy", exit.CodeOf(err), err)
	}
}
