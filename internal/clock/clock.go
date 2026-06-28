// Package clock provides the ambient time and id-generation effects treepi
// injects into its services, so tests can substitute deterministic fakes.
// Consumers (core, state) define their own narrow interfaces; this package
// supplies the concrete Real implementation and a Fake for tests.
package clock

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// Real is the production clock and id generator.
type Real struct{}

// Now returns the current time in UTC.
func (Real) Now() time.Time { return time.Now().UTC() }

// NewID returns a lexicographically sortable id: hex(unix-nanos) + a random
// suffix. Sortable-by-time is enough for op ids and snapshot ref names without
// pulling in a ULID dependency.
func (Real) NewID() string {
	ts := uint64(time.Now().UTC().UnixNano())
	var b [6]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%016x-%s", ts, hex.EncodeToString(b[:]))
}

// Fake is a deterministic clock and id generator for tests. Now returns T;
// NewID returns op0001, op0002, ... Advance bumps T.
type Fake struct {
	T   time.Time
	seq int
}

func (f *Fake) Now() time.Time { return f.T }

func (f *Fake) NewID() string {
	f.seq++
	return fmt.Sprintf("op%04d", f.seq)
}

// Advance moves the fake clock forward by d.
func (f *Fake) Advance(d time.Duration) { f.T = f.T.Add(d) }
