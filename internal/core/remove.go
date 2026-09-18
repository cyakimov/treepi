package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cyakimov/treepi/internal/config"
	"github.com/cyakimov/treepi/internal/exit"
	"github.com/cyakimov/treepi/internal/git"
	"github.com/cyakimov/treepi/internal/state"
)

type RemoveResult struct {
	Task   string `json:"task"`
	Branch string `json:"branch"`
}

type RemoveFailure struct {
	Task    string `json:"task"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type RemoveBatchResult struct {
	Removed []RemoveResult  `json:"removed"`
	Failed  []RemoveFailure `json:"failed"`
}

type taskError struct {
	task string
	err  error
}

// Remove processes distinct task names in input order and records all changed
// worktrees in one undo operation. A failure in one task leaves later tasks
// eligible for removal unless the failed task could not be restored.
func (s *Service) Remove(ctx context.Context, tasks []string, force bool) (*RemoveBatchResult, error) {
	m, err := s.store.View()
	if err != nil {
		return nil, err
	}
	names := dedupe(tasks)
	result := &RemoveBatchResult{Removed: []RemoveResult{}, Failed: []RemoveFailure{}}
	var failures []taskError
	var steps []state.Step
	tasksBefore := map[string]*state.Task{}
	opID := s.clock.NewID()

	for _, name := range names {
		if err := ctx.Err(); err != nil {
			addRemoveFailure(result, &failures, name,
				exit.Wrap(exit.Internal, "operation_canceled", "removal canceled", err))
			break
		}
		t := m.Tasks[name]
		if t == nil {
			addRemoveFailure(result, &failures, name, exit.New(exit.NotFound, "not_found", "unknown task: "+name))
			continue
		}
		inverse, removed, err := s.removeOne(ctx, t, force, opID)
		if len(inverse) > 0 {
			before := *t
			tasksBefore[name] = &before
			steps = append(steps, inverse...)
		}
		if removed {
			result.Removed = append(result.Removed, RemoveResult{Task: name, Branch: t.Branch})
		}
		if err != nil {
			addRemoveFailure(result, &failures, name, err)
			if len(inverse) > 0 && !removed {
				break
			}
		}
	}

	if len(steps) > 0 {
		if err := s.store.Do(ctx, nil, func(tx *state.Txn) error {
			for name, before := range tasksBefore {
				current := tx.Manifest().Tasks[name]
				if current == nil || *current != *before {
					return exit.New(exit.Refused, "task_changed", "task changed during removal: "+name)
				}
			}
			for _, removed := range result.Removed {
				delete(tx.Manifest().Tasks, removed.Task)
			}
			tx.AppendBegin(&state.Op{
				ID: opID, Kind: "rm", StartedAt: s.clock.Now(), Phase: "removed",
				Steps: steps, TasksBefore: tasksBefore,
			})
			tx.AppendCommit(opID, "removed")
			return nil
		}); err != nil {
			return nil, s.compensateRemove(ctx, opID, steps, tasksBefore, err)
		}
	}
	return result, aggregateRemoveErr(names, failures)
}

func addRemoveFailure(result *RemoveBatchResult, failures *[]taskError, name string, err error) {
	code := "internal"
	var typed *exit.Error
	if errors.As(err, &typed) && typed.Reason != "" {
		code = typed.Reason
	}
	result.Failed = append(result.Failed, RemoveFailure{Task: name, Code: code, Message: err.Error()})
	*failures = append(*failures, taskError{task: name, err: err})
}

func (s *Service) removeOne(ctx context.Context, t *state.Task, force bool, opID string) ([]state.Step, bool, error) {
	name, dir, branch, root := t.Name, t.Path, t.Branch, s.repo.Root
	if cwd, err := os.Getwd(); err == nil {
		c, d := canonicalRemovePath(cwd), canonicalRemovePath(dir)
		if c == d || strings.HasPrefix(c, d+string(filepath.Separator)) {
			return nil, false, exit.New(exit.Refused, "cwd_in_target",
				"refusing to remove the worktree you are inside; cd elsewhere first")
		}
	}
	branchOID, err := s.git.ResolveRef(ctx, root, "refs/heads/"+branch)
	if err != nil {
		return nil, false, exit.Wrap(exit.Refused, "branch_missing", "cannot remove task with a missing branch", err)
	}
	live, err := s.checkRemoveWorktree(ctx, dir, branch, branchOID)
	if err != nil {
		return nil, false, err
	}

	var ignored []string
	var snapshot string
	if live {
		clean, err := s.git.IsClean(ctx, dir)
		if err != nil {
			return nil, false, exit.Wrap(exit.Internal, "status_failed", "could not check worktree status", err)
		}
		if !clean && !force {
			return nil, false, exit.New(exit.Refused, "dirty",
				"worktree has uncommitted changes (use --force; work is snapshotted regardless)")
		}
		ignored, err = s.git.IgnoredPaths(ctx, dir)
		if err != nil {
			return nil, false, exit.Wrap(exit.Internal, "ignored_paths_failed", "could not inspect ignored files", err)
		}
		ref := state.SnapshotRef(name, "prerm", s.clock.NewID())
		oid, created, err := s.git.Snapshot(ctx, dir, ref, git.Identity{Name: defaultOwner()}, s.cfg.IncludeIgnored)
		if err != nil {
			return nil, false, exit.Wrap(exit.Internal, "snapshot_failed", "could not snapshot worktree", err)
		}
		if created {
			snapshot = ref
		}
		if len(ignored) > 0 {
			captured, err := s.git.SnapshotPaths(ctx, root, oid)
			if err != nil {
				return nil, false, exit.Wrap(exit.Internal, "snapshot_check_failed", "could not inspect snapshot", err)
			}
			ignored = uncapturedPaths(ignored, captured)
		}
	}

	hc := s.baseHookContext(ctx, config.EventPreRemove, name, t.Type, branch, dir)
	hc.Op = opID
	if spec, herr := s.fireHook(ctx, hc); herr != nil {
		if spec.OnFailure != config.OnFailureWarn {
			return nil, false, exit.Wrap(exit.HookAbort, "pre_remove_aborted",
				"pre_remove hook failed; worktree left intact", herr)
		}
		s.warnf("pre_remove hook failed (continuing with removal): %v", herr)
	}

	inverse := []state.Step{{Kind: state.StepAddWorktree, Path: dir, Branch: branch, From: branchOID}}
	if snapshot != "" {
		inverse = append(inverse, state.Step{Kind: state.StepRestoreTree, Path: dir, Snapshot: snapshot})
	}
	removeErr := s.git.WorktreeRemove(ctx, root, dir, force)
	finishCtx, cancel := removeFinishContext(ctx)
	defer cancel()
	gone, checkErr := s.worktreeGone(finishCtx, dir)
	if !gone || checkErr != nil {
		if checkErr != nil {
			removeErr = errors.Join(removeErr, checkErr)
		}
		if removeErr == nil {
			removeErr = errors.New("target worktree still exists")
		}
		return nil, false, exit.Wrap(exit.Internal, "worktree_remove_failed", "could not remove worktree", removeErr)
	}
	if len(ignored) > 0 {
		s.warnf("%s: %d gitignored path(s) will NOT be recoverable by undo (e.g. %s)", name, len(ignored), ignored[0])
	}
	if err := s.git.BranchDelete(finishCtx, root, branch, true); err != nil {
		if gone, checkErr := s.branchGone(finishCtx, branch); !gone || checkErr != nil {
			return s.restoreAfterDeleteFailure(finishCtx, inverse, err)
		}
	}
	if gone, err := s.branchGone(finishCtx, branch); !gone || err != nil {
		if err == nil {
			err = errors.New("branch still exists")
		}
		return s.restoreAfterDeleteFailure(finishCtx, inverse, err)
	}
	return inverse, true, nil
}

func canonicalRemovePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

func (s *Service) checkRemoveWorktree(ctx context.Context, dir, branch, oid string) (bool, error) {
	_, statErr := os.Stat(dir)
	live := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return false, exit.Wrap(exit.Internal, "worktree_stat_failed", "could not inspect worktree", statErr)
	}
	wts, err := s.git.WorktreeList(ctx, s.repo.Root)
	if err != nil {
		return false, exit.Wrap(exit.Internal, "worktree_list_failed", "could not list worktrees", err)
	}
	for _, wt := range wts {
		if filepath.Clean(wt.Path) != filepath.Clean(dir) {
			continue
		}
		if wt.Locked {
			return false, exit.New(exit.Refused, "worktree_locked", "worktree is locked; unlock it first")
		}
		if wt.Branch != "refs/heads/"+branch || wt.Head != oid {
			return false, exit.New(exit.Refused, "worktree_changed", "worktree no longer matches the recorded branch")
		}
		return live, nil
	}
	if live {
		return false, exit.New(exit.Refused, "worktree_changed", "worktree is not registered with Git")
	}
	return false, nil
}

func (s *Service) worktreeGone(ctx context.Context, dir string) (bool, error) {
	if _, err := os.Stat(dir); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	wts, err := s.git.WorktreeList(ctx, s.repo.Root)
	if err != nil {
		return false, err
	}
	for _, wt := range wts {
		if filepath.Clean(wt.Path) != filepath.Clean(dir) {
			continue
		}
		if !wt.Prunable || wt.Locked {
			return false, nil
		}
		if err := s.git.WorktreePrune(ctx, s.repo.Root); err != nil {
			return false, err
		}
		pruned, err := s.git.WorktreeList(ctx, s.repo.Root)
		if err != nil {
			return false, err
		}
		for _, remaining := range pruned {
			if filepath.Clean(remaining.Path) == filepath.Clean(dir) {
				return false, nil
			}
		}
		return true, nil
	}
	return true, nil
}

func (s *Service) branchGone(ctx context.Context, branch string) (bool, error) {
	_, err := s.git.ResolveRef(ctx, s.repo.Root, "refs/heads/"+branch)
	if errors.Is(err, git.ErrRefNotFound) {
		return true, nil
	}
	return false, err
}

func (s *Service) restoreAfterDeleteFailure(ctx context.Context, inverse []state.Step, cause error) ([]state.Step, bool, error) {
	if err := applyRemoveSteps(ctx, s.executor(), inverse); err != nil {
		return inverse, false, exit.Wrap(exit.Internal, "recovery_failed",
			"branch deletion failed and worktree could not be restored", errors.Join(cause, err))
	}
	return nil, false, exit.Wrap(exit.Internal, "branch_delete_failed", "could not delete branch; worktree restored", cause)
}

func uncapturedPaths(ignored []string, captured map[string]bool) []string {
	var missing []string
	for _, path := range ignored {
		if !captured[path] {
			missing = append(missing, path)
		}
	}
	return missing
}

func applyRemoveSteps(ctx context.Context, exec state.Executor, steps []state.Step) error {
	for _, step := range steps {
		if err := exec.Apply(ctx, step); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) compensateRemove(ctx context.Context, opID string, steps []state.Step, before map[string]*state.Task, cause error) error {
	ctx, cancel := removeFinishContext(ctx)
	defer cancel()
	gitErr := applyRemoveSteps(ctx, s.executor(), steps)
	stateErr := s.store.Do(ctx, nil, func(tx *state.Txn) error {
		for name, task := range before {
			current := tx.Manifest().Tasks[name]
			if current != nil {
				if *current != *task {
					return exit.New(exit.Refused, "task_changed", "task changed during removal: "+name)
				}
				continue
			}
			tx.Manifest().Tasks[name] = task
			tx.MarkDirty()
		}
		return nil
	})
	last, journalErr := s.store.LastCommittedOp()
	if gitErr == nil && stateErr == nil && journalErr == nil && last != nil && last.ID == opID {
		journalErr = s.store.Do(ctx, nil, func(tx *state.Txn) error {
			marker := &state.Op{ID: opID + ".undo", Kind: "undo", StartedAt: s.clock.Now()}
			tx.AppendBegin(marker)
			tx.AppendCommit(marker.ID, "done")
			return nil
		})
	}
	return exit.Wrap(exit.Internal, "remove_persist_failed", "could not record removal; attempted restoration",
		errors.Join(cause, gitErr, stateErr, journalErr))
}

func removeFinishContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func aggregateRemoveErr(names []string, failures []taskError) error {
	if len(failures) == 0 {
		return nil
	}
	if len(names) == 1 {
		return failures[0].err
	}
	code := exit.NotFound
	priority := map[exit.Code]int{exit.NotFound: 1, exit.Refused: 2, exit.HookAbort: 3, exit.Internal: 4}
	parts := make([]string, len(failures))
	for i, failure := range failures {
		candidate := exit.CodeOf(failure.err)
		if priority[candidate] > priority[code] {
			code = candidate
		}
		parts[i] = failure.task + " (" + removeReason(failure.err) + ")"
	}
	return exit.New(code, "rm_failed", "could not remove worktrees: "+strings.Join(parts, ", "))
}

func removeReason(err error) string {
	var typed *exit.Error
	if !errors.As(err, &typed) {
		return err.Error()
	}
	switch typed.Reason {
	case "not_found":
		return "unknown task"
	case "cwd_in_target":
		return "you are inside it"
	case "dirty":
		return "uncommitted changes"
	case "pre_remove_aborted":
		return "pre_remove hook aborted"
	default:
		return typed.Reason
	}
}
