package core

import (
	"context"
	"errors"
	"strings"

	"github.com/cyakimov/treepi/internal/exit"
	"github.com/cyakimov/treepi/internal/state"
)

// ClaimRequest is the input to Claim.
type ClaimRequest struct {
	Task     string // explicit task name; "" picks any free tree or creates one
	Owner    string // lease owner; "" -> defaultOwner()
	Type     string // branch type for the create path; "" -> cfg.DefaultType
	NoCreate bool   // fail with exit 5 instead of creating when none is free
}

// ReleaseOptions controls Release.
type ReleaseOptions struct {
	Force bool // release a lease owned by someone else
	Rm    bool // also remove the worktree + branch
}

// ReleaseResult reports a release.
type ReleaseResult struct {
	Task         string `json:"task"`
	Branch       string `json:"branch"`
	LeaseCleared bool   `json:"leaseCleared"`
	Removed      bool   `json:"removed,omitempty"`
}

// errClaimRace signals that a claim candidate was taken between the lock-free
// screen and the locked grab; Claim retries the next candidate.
var errClaimRace = errors.New("core: claim candidate taken; retry")

const maxClaimAttempts = 8

// Claim returns a free, clean, ready worktree with a durable lease - the agent
// entrypoint. It screens candidates lock-free (cleanliness must not run under the
// flock), grabs one under the lock, and retries on a lost race; when none is free
// it creates one unless NoCreate is set (exit 5).
func (s *Service) Claim(ctx context.Context, req ClaimRequest) (*TaskInfo, error) {
	owner := s.ownerOf(req.Owner)

	if req.Task != "" {
		m, err := s.store.View()
		if err != nil {
			return nil, err
		}
		if _, ok := m.Tasks[req.Task]; ok {
			info, cerr := s.claimSpecific(ctx, req.Task, owner)
			if errors.Is(cerr, errClaimRace) {
				return nil, exit.New(exit.LeaseHeld, "lease_held",
					"task is held by another owner or not clean: "+req.Task)
			}
			return info, cerr
		}
		if req.NoCreate {
			return nil, exit.New(exit.NoneAvailable, "none_available", "no such task and --no-create set: "+req.Task)
		}
		return s.claimCreate(ctx, req.Task, owner, req.Type)
	}

	for attempt := 0; attempt < maxClaimAttempts; attempt++ {
		pick, err := s.pickFreeCleanTask(ctx)
		if err != nil {
			return nil, err
		}
		if pick == "" {
			break
		}
		info, cerr := s.claimSpecific(ctx, pick, owner)
		switch {
		case cerr == nil:
			return info, nil
		case errors.Is(cerr, errClaimRace):
			continue
		default:
			return nil, cerr
		}
	}
	if req.NoCreate {
		return nil, exit.New(exit.NoneAvailable, "none_available", "no free worktree available")
	}
	return s.claimCreate(ctx, "", owner, req.Type)
}

// pickFreeCleanTask returns the name of a ready task with no live lease whose
// worktree is clean, or "" if none. The git status check runs lock-free.
func (s *Service) pickFreeCleanTask(ctx context.Context) (string, error) {
	m, err := s.store.View()
	if err != nil {
		return "", err
	}
	now := s.clock.Now()
	for _, t := range sortedTasks(m) {
		if t.Status != state.StatusReady {
			continue
		}
		if t.Lease != nil && !t.Lease.Expired(now) {
			continue
		}
		if clean, err := s.git.IsClean(ctx, t.Path); err == nil && clean {
			return t.Name, nil
		}
	}
	return "", nil
}

// claimSpecific grabs task under the lock: after Reconcile (so a non-nil lease is
// genuinely live), it requires the task ready and unleased, writes a durable
// lease file, and mirrors it into the manifest. errClaimRace means it was taken
// or changed first.
func (s *Service) claimSpecific(ctx context.Context, task, owner string) (*TaskInfo, error) {
	lease := s.newClaimLease(owner)
	var info *TaskInfo
	err := s.store.Do(ctx, s.reconciler(), func(tx *state.Txn) error {
		t := tx.Manifest().Tasks[task]
		if t == nil || t.Status != state.StatusReady {
			return errClaimRace
		}
		if t.Lease != nil {
			return errClaimRace
		}
		if werr := s.store.WriteLease(task, lease); werr != nil {
			return werr
		}
		t.Lease = lease
		tx.MarkDirty()
		info = taskInfo(t)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return info, nil
}

// claimCreate creates a new worktree and retains a durable lease on it. With an
// auto-generated name it tolerates the (astronomically rare) race where a
// concurrent claimer grabs the brand-new tree, making another.
func (s *Service) claimCreate(ctx context.Context, task, owner, typ string) (*TaskInfo, error) {
	if typ == "" {
		typ = s.cfg.DefaultType
	}
	for attempt := 0; attempt < 4; attempt++ {
		name := task
		if name == "" {
			name = "claim-" + shortID(s.clock.NewID())
		}
		if _, err := s.New(ctx, typ, name); err != nil {
			if task == "" && isReason(err, "task_exists") {
				continue // random-name collision; try another
			}
			return nil, err // propagates exit 6 (slots), 11 (lock), etc.
		}
		info, cerr := s.claimSpecific(ctx, name, owner)
		switch {
		case cerr == nil:
			return info, nil
		case task == "" && errors.Is(cerr, errClaimRace):
			continue // our fresh tree was claimed first; make another
		default:
			return nil, cerr
		}
	}
	return nil, exit.New(exit.NoneAvailable, "none_available", "could not claim a freshly created worktree after retries")
}

// Release clears a task's lease (file + manifest mirror), refusing a lease owned
// by another unless Force, and optionally removing the worktree.
func (s *Service) Release(ctx context.Context, task string, opts ReleaseOptions) (*ReleaseResult, error) {
	m, err := s.store.View()
	if err != nil {
		return nil, err
	}
	t := m.Tasks[task]
	if t == nil {
		return nil, exit.New(exit.NotFound, "not_found", "unknown task: "+task)
	}
	if t.Lease != nil && t.Lease.Owner != defaultOwner() && !opts.Force {
		return nil, exit.New(exit.LeaseHeld, "lease_held", "task is leased by "+t.Lease.Owner+" (use --force)")
	}

	res := &ReleaseResult{Task: task, Branch: t.Branch}
	_ = s.store.ReleaseLease(task)
	_ = s.store.Do(ctx, nil, func(tx *state.Txn) error {
		if tt := tx.Manifest().Tasks[task]; tt != nil && tt.Lease != nil {
			tt.Lease = nil
			tx.MarkDirty()
		}
		return nil
	})
	res.LeaseCleared = true

	if opts.Rm {
		// The lease is already cleared, so Remove's owner-guard is moot; its dirty
		// guard still applies (pass --force to discard - work is snapshotted).
		if _, rerr := s.Remove(ctx, task, opts.Force); rerr != nil {
			return res, rerr
		}
		res.Removed = true
	}
	return res, nil
}

// Renew extends a claimed task's lease (the agent heartbeat). It refuses if the
// lease has lapsed (re-claim) or is held by another owner.
func (s *Service) Renew(ctx context.Context, task, owner string) (*TaskInfo, error) {
	owner = s.ownerOf(owner)
	var info *TaskInfo
	err := s.store.Do(ctx, s.reconciler(), func(tx *state.Txn) error {
		t := tx.Manifest().Tasks[task]
		if t == nil {
			return exit.New(exit.NotFound, "not_found", "unknown task: "+task)
		}
		if t.Lease == nil {
			return exit.New(exit.LeaseHeld, "lease_lost", "lease expired or lost; re-claim "+task)
		}
		if t.Lease.Owner != owner {
			return exit.New(exit.LeaseHeld, "lease_held", "task is leased by "+t.Lease.Owner)
		}
		next := *t.Lease
		next.Expires = tx.Now().Add(s.cfg.LeaseTTL)
		if werr := s.store.WriteLease(task, &next); werr != nil {
			return werr
		}
		*t.Lease = next
		tx.MarkDirty()
		info = taskInfo(t)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return info, nil
}

func (s *Service) ownerOf(o string) string {
	if o != "" {
		return o
	}
	return defaultOwner()
}

// newClaimLease builds a durable, process-surviving lease. PID=0 makes it
// TTL-authoritative (never pid-probed): the claiming process exits immediately.
func (s *Service) newClaimLease(owner string) *state.Lease {
	now := s.clock.Now()
	return &state.Lease{
		Owner: owner, PID: 0, Host: hostname(),
		Nonce: s.clock.NewID(), AcquiredAt: now, Expires: now.Add(s.cfg.LeaseTTL),
	}
}

// shortID returns a short, branch-safe suffix from a clock id. clock.Real's id
// is "hex(nanos)-<rand>"; the fake's is "opNNNN". Take the random suffix after
// the last '-', else the whole id.
func shortID(id string) string {
	if i := strings.LastIndexByte(id, '-'); i >= 0 && i+1 < len(id) {
		return id[i+1:]
	}
	return id
}

func isReason(err error, reason string) bool {
	var e *exit.Error
	return errors.As(err, &e) && e.Reason == reason
}
