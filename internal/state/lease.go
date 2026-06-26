package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrLeaseHeld is returned when a lease file already exists for a task.
var ErrLeaseHeld = errors.New("state: lease already held")

func (s *Store) leasePath(task string) string {
	return filepath.Join(s.dir, leasesDir, task+".lease")
}

// AcquireLease atomically creates the lease file for task (O_EXCL). Two racing
// processes cannot both succeed; the loser gets ErrLeaseHeld. The file is the
// authoritative record of the claim; the manifest mirror is derived.
func (s *Store) AcquireLease(task string, l *Lease) error {
	f, err := os.OpenFile(s.leasePath(task), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return ErrLeaseHeld
	}
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		return err
	}
	return f.Sync()
}

// WriteLease atomically creates-or-overwrites the lease file for task via a temp
// file + fsync + rename, so a concurrent reader never sees a torn lease and an
// expired-but-not-removed file is cleanly replaced. Unlike AcquireLease it does
// NOT guard with O_EXCL: serialization is the caller's flock plus the manifest
// generation-CAS, with the manifest as the logical source of truth. Used by
// claim (durable lease) and renew (heartbeat).
func (s *Store) WriteLease(task string, l *Lease) error {
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	path := s.leasePath(task)
	tmp := path + ".tmp"
	if err := writeFileSync(tmp, b); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("state: commit lease: %w", err)
	}
	return nil
}

// ReleaseLease removes the lease file (idempotent).
func (s *Store) ReleaseLease(task string) error {
	err := os.Remove(s.leasePath(task))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// ReadLease returns the lease for task, or (nil, nil) if none exists.
func (s *Store) ReadLease(task string) (*Lease, error) {
	b, err := os.ReadFile(s.leasePath(task))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var l Lease
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, err
	}
	return &l, nil
}
