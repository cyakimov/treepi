// Package state owns everything under <common-dir>/treepi/: the JSON manifest,
// the append-only journal, and the per-task lease files. It is the only writer
// of treepi state. Mutations go through Store.Do, which holds a flock for the
// fast critical section only (never across slow git or hook work) and guards
// every write with a generation-CAS.
package state

import "time"

// Status is a task's lifecycle state in the manifest.
type Status string

const (
	StatusCreating        Status = "creating"        // worktree provisioning (slow work in flight)
	StatusReady           Status = "ready"           // provisioned and idle
	StatusMerging         Status = "merging"         // a merge is mid-flight / interrupted
	StatusNeedsResolution Status = "needs-resolution" // a sync/merge hit conflicts
	StatusOrphaned        Status = "orphaned"        // manifest entry with no live worktree
)

// Lease is a durable claim on a task. The lease file on disk is authoritative
// for existence; this struct mirrors it into the manifest for display.
type Lease struct {
	Owner      string    `json:"owner"`
	PID        int       `json:"pid"`
	Host       string    `json:"host"`
	StartTime  string    `json:"startTime,omitempty"` // process start fingerprint (defends pid reuse)
	Nonce      string    `json:"nonce"`
	AcquiredAt time.Time `json:"acquiredAt"`
	Expires    time.Time `json:"expires"`
}

// Expired reports whether the lease's TTL has elapsed. TTL is the authoritative
// reclaimer; a pid probe can only shorten the wait, never steal.
func (l *Lease) Expired(now time.Time) bool { return now.After(l.Expires) }

// Task is one worktree treepi manages.
type Task struct {
	Name      string         `json:"task"`
	Type      string         `json:"type"`
	Branch    string         `json:"branch"`
	Path      string         `json:"path"`
	Slot      int            `json:"slot"`
	BaseSha   string         `json:"baseSha,omitempty"`
	Status    Status         `json:"status"`
	CreatedAt time.Time      `json:"createdAt"`
	Lease     *Lease         `json:"lease,omitempty"`
	Extras    map[string]any `json:"extras,omitempty"`
}

// Manifest is the reconciled intent for a repo's worktrees.
type Manifest struct {
	Version       int              `json:"version"`
	Generation    uint64           `json:"generation"`
	RepoRoot      string           `json:"repoRoot"`
	WorktreesBase string           `json:"worktreesBase"`
	Trunk         string           `json:"trunk"`
	UpdatedAt     time.Time        `json:"updatedAt"`
	LastOpID      string           `json:"lastOpId,omitempty"`
	Tasks         map[string]*Task `json:"tasks"`
}

func newManifest() *Manifest {
	return &Manifest{Version: 1, Tasks: map[string]*Task{}}
}

// UsedSlots returns the set of slots currently held by tasks.
func (m *Manifest) UsedSlots() map[int]bool {
	used := make(map[int]bool, len(m.Tasks))
	for _, t := range m.Tasks {
		used[t.Slot] = true
	}
	return used
}

// LowestFreeSlot returns the lowest slot in [lo, hi] not currently used, or
// false if the range is exhausted.
func (m *Manifest) LowestFreeSlot(lo, hi int) (int, bool) {
	used := m.UsedSlots()
	for s := lo; s <= hi; s++ {
		if !used[s] {
			return s, true
		}
	}
	return 0, false
}
