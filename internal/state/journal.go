package state

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"time"
)

// journalName is the append-only op-log under the state dir.
const journalName = "journal.jsonl"

// Record discriminators.
const (
	recBegin  = "begin"
	recCommit = "commit"
)

// StepKind names a reversible primitive. Steps are stored as data so undo can
// replay an operation's inverse without re-deriving anything.
type StepKind string

const (
	StepSetRef         StepKind = "set_ref"         // restore Ref to From (CAS: only if currently To)
	StepDeleteRef      StepKind = "delete_ref"      // delete Ref (undo a created branch/ref)
	StepCreateRef      StepKind = "create_ref"      // create Ref at From (undo a deleted branch)
	StepRemoveWorktree StepKind = "remove_worktree" // remove the worktree at Path
	StepAddWorktree    StepKind = "add_worktree"    // recreate the worktree at Path on Branch at From
	StepRestoreTree    StepKind = "restore_tree"    // overlay Path's content from the Snapshot ref
	StepResetHard      StepKind = "reset_hard"      // reset the worktree at Path (HEAD+index+files) to From
	StepRewindTrunk    StepKind = "rewind_trunk"    // guarded: rewind Ref From<-To only if no branch built on To
)

// Step is one ordered inverse primitive.
type Step struct {
	Kind     StepKind `json:"kind"`
	Ref      string   `json:"ref,omitempty"`
	Branch   string   `json:"branch,omitempty"`
	Path     string   `json:"path,omitempty"`
	From     string   `json:"from,omitempty"`     // OID the inverse restores to
	To       string   `json:"to,omitempty"`       // OID the forward op left it at (CAS)
	Snapshot string   `json:"snapshot,omitempty"` // snapshot ref for a tree restore
	Task     string   `json:"task,omitempty"`
}

// Op is one journaled, reversible operation.
type Op struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"` // new|sync|merge|rm|undo
	StartedAt time.Time `json:"startedAt"`
	Phase     string    `json:"phase,omitempty"`
	Steps     []Step    `json:"steps,omitempty"` // ordered inverse git primitives
	// TasksBefore captures the manifest entries the op touched, as they were
	// before it ran. On undo each is restored; a present key with a nil value
	// means the task did not exist before (so undo deletes it). This is how undo
	// spans the manifest in addition to refs/worktrees.
	TasksBefore map[string]*Task `json:"tasksBefore,omitempty"`
}

// opRecord is one JSONL line: a begin (carrying the full op) or a commit
// (referencing the op id and its final phase).
type opRecord struct {
	Rec   string `json:"rec"`
	Op    *Op    `json:"op,omitempty"`
	ID    string `json:"id,omitempty"`
	Phase string `json:"phase,omitempty"`
}

// SnapshotRef builds the ref for a snapshot of task, e.g.
// refs/treepi/snapshots/login-fix/premerge-<id>.
func SnapshotRef(task, kind, id string) string {
	return path.Join("refs/treepi/snapshots", task, kind+"-"+id)
}

// ConflictRef builds the ref for a captured conflict tree.
func ConflictRef(task, id string) string {
	return path.Join("refs/treepi/conflicts", task, id)
}

func (s *Store) journalPath() string { return path.Join(s.dir, journalName) }

// appendJournal appends records as JSONL lines, fsynced. Called under the flock
// (from Store.Do).
func (s *Store) appendJournal(records []opRecord) error {
	f, err := os.OpenFile(s.journalPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("state: open journal: %w", err)
	}
	defer func() { _ = f.Close() }()
	for _, rec := range records {
		b, err := json.Marshal(rec)
		if err != nil {
			return fmt.Errorf("state: marshal journal record: %w", err)
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			return fmt.Errorf("state: write journal: %w", err)
		}
	}
	return f.Sync()
}

// readJournal parses every complete JSONL record. A torn trailing line (from a
// crash mid-append) is tolerated and skipped - the op it belonged to is treated
// as uncommitted.
func (s *Store) readJournal() ([]opRecord, error) {
	f, err := os.Open(s.journalPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var recs []opRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec opRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue // torn/partial line: skip
		}
		recs = append(recs, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return recs, nil
}

// LastCommittedOp returns the most recent op that has a matching commit record,
// or (nil, nil) if there is none. This is what undo reverses.
func (s *Store) LastCommittedOp() (*Op, error) {
	recs, err := s.readJournal()
	if err != nil {
		return nil, err
	}
	committed := map[string]bool{}
	ops := map[string]*Op{}
	var order []string
	for _, r := range recs {
		switch r.Rec {
		case recBegin:
			if r.Op != nil {
				ops[r.Op.ID] = r.Op
				order = append(order, r.Op.ID)
			}
		case recCommit:
			committed[r.ID] = true
			if op := ops[r.ID]; op != nil && r.Phase != "" {
				op.Phase = r.Phase
			}
		}
	}
	for i := len(order) - 1; i >= 0; i-- {
		if committed[order[i]] {
			return ops[order[i]], nil
		}
	}
	return nil, nil
}

// IncompleteOp returns the most recent op that began but never committed (a
// crash mid-operation), or (nil, nil). Used for phase-aware recovery.
func (s *Store) IncompleteOp() (*Op, error) {
	recs, err := s.readJournal()
	if err != nil {
		return nil, err
	}
	committed := map[string]bool{}
	for _, r := range recs {
		if r.Rec == recCommit {
			committed[r.ID] = true
		}
	}
	for i := len(recs) - 1; i >= 0; i-- {
		if recs[i].Rec == recBegin && recs[i].Op != nil && !committed[recs[i].Op.ID] {
			return recs[i].Op, nil
		}
	}
	return nil, nil
}
