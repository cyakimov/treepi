package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cyakimov/treepi/internal/config"
	"github.com/cyakimov/treepi/internal/exit"
	"github.com/cyakimov/treepi/internal/git"
	"github.com/cyakimov/treepi/internal/state"
)

// RemoveResult reports a removed task.
type RemoveResult struct {
	Task   string `json:"task"`
	Branch string `json:"branch"`
}

// taskError pairs a requested task with why it could not be removed.
type taskError struct {
	task string
	err  error
}

// Remove discards each task's worktree and branch. It is best-effort over the
// given tasks: a task that is unknown, dirty (without --force), or the one the
// caller is standing in is skipped and reported, while the rest are removed. It
// always snapshots first, so even a forced discard is recoverable via undo; on a
// forced discard it warns which gitignored paths will not be recoverable. Every
// successful removal is captured in a single journal op, so one `undo` restores
// the whole batch at once.
func (s *Service) Remove(ctx context.Context, tasks []string, force bool) ([]RemoveResult, error) {
	m, err := s.store.View()
	if err != nil {
		return nil, err
	}

	names := dedupe(tasks)
	root := s.repo.Root
	cwd, _ := os.Getwd()
	ident := git.Identity{Name: defaultOwner()}
	opID := s.clock.NewID()

	var (
		results  []RemoveResult
		steps    []state.Step
		failures []taskError
	)
	tasksBefore := map[string]*state.Task{}

	for _, name := range names {
		t := m.Tasks[name]
		if t == nil {
			failures = append(failures, taskError{name, exit.New(exit.NotFound, "not_found", "unknown task: "+name)})
			continue
		}
		dir, branch := t.Path, t.Branch

		// Refuse (skip) the worktree the caller is standing in; cd elsewhere first.
		if cwd != "" {
			c, d := filepath.Clean(cwd), filepath.Clean(dir)
			if c == d || strings.HasPrefix(c, d+string(filepath.Separator)) {
				failures = append(failures, taskError{name, exit.New(exit.Refused, "cwd_in_target",
					"refusing to remove the worktree you are inside; cd elsewhere first")})
				continue
			}
		}
		clean, _ := s.git.IsClean(ctx, dir)
		if !clean && !force {
			failures = append(failures, taskError{name, exit.New(exit.Refused, "dirty",
				"worktree has uncommitted changes (use --force; work is snapshotted regardless)")})
			continue
		}

		before := *t
		branchOID, _ := s.git.ResolveRef(ctx, dir, "HEAD")

		// Snapshot first so undo can recover even a forced discard.
		snapRef := state.SnapshotRef(name, "prerm", s.clock.NewID())
		_, snapCreated, _ := s.git.Snapshot(ctx, dir, snapRef, ident, s.cfg.IncludeIgnored)

		if force && !clean {
			if ig, _ := s.git.IgnoredPaths(ctx, dir); len(ig) > 0 {
				fmt.Fprintf(s.warn, "treepi: %d gitignored path(s) will NOT be recoverable by undo (e.g. %s)\n", len(ig), ig[0])
			}
		}

		// pre_remove hook (runs before any destruction, so abort is honored: the
		// tree is left intact and the task is reported, not removed). Default warn -
		// a teardown failure should not strand a tree.
		hc := s.baseHookContext(ctx, config.EventPreRemove, name, t.Type, branch, dir)
		hc.Op = opID
		if spec, herr := s.fireHook(ctx, hc); herr != nil {
			if spec.OnFailure != config.OnFailureWarn {
				failures = append(failures, taskError{name, exit.Wrap(exit.HookAbort, "pre_remove_aborted",
					"pre_remove hook failed; worktree left intact", herr)})
				continue
			}
			s.warnf("pre_remove hook failed (continuing with removal): %v", herr)
		}

		// git worktree remove may error when the worktree dir was already deleted
		// out from under us; the prune below and the manifest delete still clean up,
		// so the removal is treated as done.
		_ = s.git.WorktreeRemove(ctx, root, dir, force)
		_ = s.git.BranchDelete(ctx, root, branch, true) // recoverable via undo (branchOID)

		steps = append(steps, state.Step{Kind: state.StepAddWorktree, Path: dir, Branch: branch, From: branchOID})
		if snapCreated {
			steps = append(steps, state.Step{Kind: state.StepRestoreTree, Path: dir, Snapshot: snapRef})
		}
		tasksBefore[name] = &before
		results = append(results, RemoveResult{Task: name, Branch: branch})
	}

	if len(results) > 0 {
		_ = s.git.WorktreePrune(ctx, root)

		// Journal every successful removal as one op so a single undo reverses the batch.
		if err := s.store.Do(ctx, nil, func(tx *state.Txn) error {
			for name := range tasksBefore {
				delete(tx.Manifest().Tasks, name)
			}
			tx.AppendBegin(&state.Op{
				ID: opID, Kind: "rm", StartedAt: s.clock.Now(), Phase: "removed",
				Steps: steps, TasksBefore: tasksBefore,
			})
			tx.AppendCommit(opID, "removed")
			tx.MarkDirty()
			return nil
		}); err != nil {
			return nil, err
		}
	}

	return results, aggregateRemoveErr(names, failures)
}

// dedupe returns the input with duplicates removed, preserving first-seen order.
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

// aggregateRemoveErr turns per-task failures into the command's error. A single
// requested task keeps its exact error (message, wrapped cause, and exit code),
// so `tp rm <one>` behaves exactly as before; multiple requested tasks collapse
// to one error that names each failure, carrying the first failure's code.
func aggregateRemoveErr(names []string, failures []taskError) error {
	if len(failures) == 0 {
		return nil
	}
	if len(names) == 1 {
		return failures[0].err
	}
	code, reason := exit.Internal, "rm_failed"
	var te *exit.Error
	if errors.As(failures[0].err, &te) {
		code, reason = te.Code, te.Reason
	}
	parts := make([]string, len(failures))
	for i, f := range failures {
		parts[i] = f.task + " (" + removeReason(f.err) + ")"
	}
	return exit.New(code, reason, "could not remove some worktrees: "+strings.Join(parts, ", "))
}

// removeReason is a short, human phrase for why one task could not be removed,
// used when enumerating failures across a batch.
func removeReason(err error) string {
	var te *exit.Error
	if !errors.As(err, &te) {
		return err.Error()
	}
	switch te.Reason {
	case "not_found":
		return "unknown task"
	case "cwd_in_target":
		return "you are inside it"
	case "dirty":
		return "uncommitted changes"
	case "pre_remove_aborted":
		return "pre_remove hook aborted"
	default:
		return te.Reason
	}
}
