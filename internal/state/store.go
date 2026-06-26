package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"

	"github.com/cyakimov/treepi/internal/exit"
)

const (
	manifestName = "manifest.json"
	lockName     = "manifest.json.lock"
	leasesDir    = "leases"
	lockRetry    = 25 * time.Millisecond
)

// ErrGenerationConflict means the on-disk manifest changed under us between
// load and save - a non-flock writer raced. The CAS is the correctness
// guarantee behind the advisory flock.
var ErrGenerationConflict = errors.New("state: manifest generation conflict")

// Clock yields the current time; injected so tests are deterministic.
type Clock interface{ Now() time.Time }

// Store is the single writer of <stateDir>. It is safe to construct once per
// process and reuse.
type Store struct {
	dir   string
	clock Clock
	live  LivenessFunc
}

// Open ensures the state directory (and leases subdir) exist and returns a Store.
func Open(stateDir string, clock Clock) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(stateDir, leasesDir), 0o755); err != nil {
		return nil, fmt.Errorf("state: create %s: %w", stateDir, err)
	}
	return &Store{dir: stateDir, clock: clock, live: DefaultLiveness(hostName())}, nil
}

// Dir returns the state directory.
func (s *Store) Dir() string { return s.dir }

func (s *Store) manifestPath() string { return filepath.Join(s.dir, manifestName) }
func (s *Store) lockPath() string     { return filepath.Join(s.dir, lockName) }

// Txn is the mutable handle passed to Store.Do. The function mutates
// tx.Manifest() and calls tx.MarkDirty() to request a persisted write, and may
// append journal records (begin/commit) that are flushed atomically with the
// manifest under the same lock.
type Txn struct {
	m       *Manifest
	now     time.Time
	dirty   bool
	records []opRecord
}

// Manifest returns the live manifest to mutate.
func (tx *Txn) Manifest() *Manifest { return tx.m }

// Now returns the transaction's timestamp (from the injected clock).
func (tx *Txn) Now() time.Time { return tx.now }

// MarkDirty requests that the manifest be persisted at the end of the txn.
func (tx *Txn) MarkDirty() { tx.dirty = true }

// AppendBegin journals the start of op, capturing its inverse before any git
// side effect runs (those run outside this lock, in the two-tier model).
func (tx *Txn) AppendBegin(op *Op) {
	tx.records = append(tx.records, opRecord{Rec: recBegin, Op: op})
	tx.m.LastOpID = op.ID
	tx.dirty = true
}

// AppendCommit journals the successful completion of op at finalPhase.
func (tx *Txn) AppendCommit(opID, finalPhase string) {
	tx.records = append(tx.records, opRecord{Rec: recCommit, ID: opID, Phase: finalPhase})
	tx.dirty = true
}

// Do runs fn inside the fast critical section: it acquires the flock (failing
// fast with exit.LockBusy rather than hanging), loads the manifest, runs the
// structural Reconcile (if rec is non-nil), invokes fn, and persists the
// manifest with a generation-CAS iff Reconcile or fn changed it. It must NOT be
// used to wrap slow git or hook work - that runs between Do calls, guarded by a
// lease.
func (s *Store) Do(ctx context.Context, rec Reconciler, fn func(tx *Txn) error) error {
	fl := flock.New(s.lockPath())
	locked, err := fl.TryLockContext(ctx, lockRetry)
	if err != nil {
		return exit.Wrap(exit.LockBusy, "lock_busy", "could not acquire state lock", err)
	}
	if !locked {
		return exit.New(exit.LockBusy, "lock_busy", "treepi state is locked by another process")
	}
	defer func() { _ = fl.Unlock() }()

	m, err := s.load()
	if err != nil {
		return err
	}
	gen := m.Generation

	changed := false
	if rec != nil {
		rc, err := s.reconcile(ctx, m, rec)
		if err != nil {
			return err
		}
		changed = rc
	}

	tx := &Txn{m: m, now: s.clock.Now()}
	if fn != nil {
		if err := fn(tx); err != nil {
			return err
		}
	}

	if changed || tx.dirty {
		m.UpdatedAt = s.clock.Now()
		if err := s.save(m, gen); err != nil {
			return err
		}
	}
	if len(tx.records) > 0 {
		if err := s.appendJournal(tx.records); err != nil {
			return err
		}
	}
	return nil
}

// View returns a read-only copy of the manifest without taking the write lock
// (atomic rename makes a concurrent write either fully visible or not at all).
func (s *Store) View() (*Manifest, error) { return s.load() }

func (s *Store) load() (*Manifest, error) {
	b, err := os.ReadFile(s.manifestPath())
	if errors.Is(err, fs.ErrNotExist) {
		return newManifest(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("state: read manifest: %w", err)
	}
	m := newManifest()
	if err := json.Unmarshal(b, m); err != nil {
		return nil, fmt.Errorf("state: parse manifest: %w", err)
	}
	if m.Tasks == nil {
		m.Tasks = map[string]*Task{}
	}
	return m, nil
}

func (s *Store) diskGeneration() (uint64, error) {
	b, err := os.ReadFile(s.manifestPath())
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var head struct {
		Generation uint64 `json:"generation"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return 0, fmt.Errorf("state: parse manifest generation: %w", err)
	}
	return head.Generation, nil
}

// save writes the manifest with a generation-CAS: it refuses if the on-disk
// generation no longer matches expectedGen, then bumps and atomically renames.
func (s *Store) save(m *Manifest, expectedGen uint64) error {
	cur, err := s.diskGeneration()
	if err != nil {
		return err
	}
	if cur != expectedGen {
		return ErrGenerationConflict
	}
	m.Generation = expectedGen + 1
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("state: marshal manifest: %w", err)
	}
	tmp := s.manifestPath() + ".tmp"
	if err := writeFileSync(tmp, b); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.manifestPath()); err != nil {
		return fmt.Errorf("state: commit manifest: %w", err)
	}
	return nil
}

// writeFileSync writes b to path and fsyncs before close, so the atomic rename
// that follows can never publish a torn file.
func writeFileSync(path string, b []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("state: open %s: %w", path, err)
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return fmt.Errorf("state: write %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("state: fsync %s: %w", path, err)
	}
	return f.Close()
}
