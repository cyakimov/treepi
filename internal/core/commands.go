package core

import (
	"context"
	"fmt"
	"os"

	"github.com/cyakimov/treepi/internal/exit"
	"github.com/cyakimov/treepi/internal/repo"
	"github.com/cyakimov/treepi/internal/state"
)

// New creates a worktree for task on branch <type>/<task>, cut from the latest
// trunk. It follows the two-tier model: a fast locked phase reserves the slot,
// lease, and a "creating" entry (journaling the inverse); the slow worktree add
// runs unlocked; a final fast locked phase flips the entry to "ready".
func (s *Service) New(ctx context.Context, typ, task string) (*TaskInfo, error) {
	if typ == "" {
		typ = s.cfg.DefaultType
	}
	if !s.cfg.IsType(typ) {
		return nil, exit.New(exit.Usage, "invalid_type", "unknown branch type: "+typ)
	}
	if task == "" {
		return nil, exit.New(exit.Usage, "invalid_task", "a task name is required")
	}
	branch := repo.BranchName(typ, task)
	if err := s.git.CheckRefFormat(ctx, branch); err != nil {
		return nil, exit.Wrap(exit.Usage, "invalid_refname", "invalid branch name "+branch, err)
	}
	if _, err := s.git.ResolveRef(ctx, s.repo.Root, "refs/heads/"+branch); err == nil {
		return nil, exit.New(exit.Usage, "branch_exists", "branch already exists: "+branch)
	}

	path := s.repo.TaskPath(task)
	base, offline, err := s.resolveBase(ctx)
	if err != nil {
		return nil, err
	}
	if offline {
		fmt.Fprintf(s.warn, "treepi: cutting from local %s (offline / no remote)\n", s.repo.Trunk)
	}

	opID := s.clock.NewID()
	now := s.clock.Now()
	lease := &state.Lease{
		Owner: defaultOwner(), PID: os.Getpid(), Host: hostname(),
		Nonce: s.clock.NewID(), AcquiredAt: now, Expires: now.Add(s.cfg.LeaseTTL),
	}

	// Phase A (locked, fast): reserve.
	var slot int
	if err := s.store.Do(ctx, s.reconciler(), func(tx *state.Txn) error {
		m := tx.Manifest()
		m.RepoRoot, m.WorktreesBase, m.Trunk = s.repo.Root, s.repo.BaseDir, s.repo.Trunk
		if _, exists := m.Tasks[task]; exists {
			return exit.New(exit.Usage, "task_exists", "task already exists: "+task)
		}
		sl, ok := m.LowestFreeSlot(s.cfg.SlotLo, s.cfg.SlotHi)
		if !ok {
			return exit.New(exit.SlotsExhausted, "slots_exhausted",
				fmt.Sprintf("no free slot in [%d,%d]", s.cfg.SlotLo, s.cfg.SlotHi))
		}
		slot = sl
		m.Tasks[task] = &state.Task{
			Name: task, Type: typ, Branch: branch, Path: path, Slot: slot,
			BaseSha: base, Status: state.StatusCreating, CreatedAt: now, Lease: lease,
		}
		tx.AppendBegin(&state.Op{
			ID: opID, Kind: "new", StartedAt: now, Phase: "reserved",
			Steps: []state.Step{
				{Kind: state.StepRemoveWorktree, Path: path, Task: task},
				{Kind: state.StepDeleteRef, Ref: "refs/heads/" + branch},
			},
		})
		return nil
	}); err != nil {
		return nil, err
	}
	_ = s.store.AcquireLease(task, lease) // durable creation guard

	// Phase B (unlocked, slow): create the worktree. Hooks land at task 7.
	if err := s.git.WorktreeAdd(ctx, s.repo.Root, path, branch, base); err != nil {
		s.rollbackNew(ctx, task, branch, path, opID)
		return nil, exit.Wrap(exit.Internal, "worktree_add_failed", "could not create worktree", err)
	}

	// Phase C (locked, fast): publish.
	var info *TaskInfo
	if err := s.store.Do(ctx, nil, func(tx *state.Txn) error {
		t := tx.Manifest().Tasks[task]
		if t == nil {
			return exit.New(exit.Internal, "task_vanished", "task entry vanished during create")
		}
		t.Status = state.StatusReady
		t.Lease = nil // creation guard released; only `claim` keeps a durable lease
		tx.AppendCommit(opID, "ready")
		tx.MarkDirty()
		info = taskInfo(t)
		return nil
	}); err != nil {
		return nil, err
	}
	_ = s.store.ReleaseLease(task)
	return info, nil
}

// rollbackNew undoes a partial New after a failed worktree add or hook.
func (s *Service) rollbackNew(ctx context.Context, task, branch, path, opID string) {
	_ = s.git.WorktreeRemove(ctx, s.repo.Root, path, true)
	_ = s.git.WorktreePrune(ctx, s.repo.Root)
	_ = s.git.BranchDelete(ctx, s.repo.Root, branch, true)
	_ = s.store.ReleaseLease(task)
	_ = s.store.Do(ctx, nil, func(tx *state.Txn) error {
		delete(tx.Manifest().Tasks, task)
		tx.AppendCommit(opID, "rolled-back")
		tx.MarkDirty()
		return nil
	})
}

// List returns every task. It is lock-free: a snapshot-read of the manifest plus
// a git worktree list, with orphan status computed for display only (never
// persisted). When detailed, the expensive per-tree status (ahead/behind,
// dirty) is filled in - the ls/dash path only.
func (s *Service) List(ctx context.Context, detailed bool) ([]TaskInfo, error) {
	m, err := s.store.View()
	if err != nil {
		return nil, err
	}
	views, err := s.reconciler().ListWorktrees(ctx)
	if err != nil {
		return nil, err
	}
	live := make(map[string]bool, len(views))
	for _, v := range views {
		live[cleanPath(v.Path)] = true
	}

	tasks := sortedTasks(m)
	out := make([]TaskInfo, 0, len(tasks))
	for _, t := range tasks {
		ti := taskInfo(t)
		if !live[cleanPath(t.Path)] && ti.Status != string(state.StatusOrphaned) {
			ti.Status = string(state.StatusOrphaned) // display-only
		}
		out = append(out, *ti)
	}
	if detailed {
		for i := range out {
			s.enrich(ctx, &out[i])
		}
	}
	return out, nil
}

// enrich fills ahead/behind and dirty for a task (best-effort; display only).
func (s *Service) enrich(ctx context.Context, ti *TaskInfo) {
	if ti.Status == string(state.StatusOrphaned) {
		return
	}
	if clean, err := s.git.IsClean(ctx, ti.Path); err == nil {
		ti.Dirty = !clean
	}
	if ahead, behind, err := s.git.AheadBehind(ctx, ti.Path, s.repo.Trunk, "HEAD"); err == nil {
		ti.Ahead, ti.Behind = ahead, behind
	}
}

// Where resolves a task name to its worktree path.
func (s *Service) Where(ctx context.Context, task string) (string, error) {
	m, err := s.store.View()
	if err != nil {
		return "", err
	}
	t := m.Tasks[task]
	if t == nil {
		return "", exit.New(exit.NotFound, "not_found", "unknown task: "+task)
	}
	return t.Path, nil
}
