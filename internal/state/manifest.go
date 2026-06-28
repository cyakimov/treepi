// Package state owns everything under <common-dir>/treepi/: the JSON manifest
// and the append-only journal. It is the only writer of treepi state. Mutations
// go through Store.Do, which holds a flock for the fast critical section only
// (never across slow git or hook work) and guards every write with a
// generation-CAS.
package state

import "time"

// Status is a task's lifecycle state in the manifest.
type Status string

const (
	StatusCreating        Status = "creating"         // worktree provisioning (slow work in flight)
	StatusReady           Status = "ready"            // provisioned and idle
	StatusMerging         Status = "merging"          // a merge is mid-flight / interrupted
	StatusNeedsResolution Status = "needs-resolution" // a sync/merge hit conflicts
	StatusOrphaned        Status = "orphaned"         // manifest entry with no live worktree
)

// Task is one worktree treepi manages.
type Task struct {
	Name      string    `json:"task"`
	Type      string    `json:"type"`
	Branch    string    `json:"branch"`
	Path      string    `json:"path"`
	BaseSha   string    `json:"baseSha,omitempty"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
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
