//go:build integration

package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyakimov/treepi/internal/exit"
)

// worktreePath is where treepi puts a task's worktree: a sibling <root>.worktrees/<task>.
func worktreePath(dir, task string) string { return filepath.Join(dir+".worktrees", task) }

func TestIntegrationRmMany(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc := openSvc(t, ctx, dir)

	for _, task := range []string{"a", "b"} {
		if _, err := svc.New(ctx, "feat", task); err != nil {
			t.Fatal(err)
		}
	}

	results, err := svc.Remove(ctx, []string{"a", "b"}, false)
	if err != nil {
		t.Fatalf("rm a b: %v", err)
	}
	if len(results.Removed) != 2 {
		t.Fatalf("results = %+v, want 2", results)
	}
	for _, task := range []string{"a", "b"} {
		if _, statErr := os.Stat(worktreePath(dir, task)); !os.IsNotExist(statErr) {
			t.Errorf("worktree %s still present", task)
		}
		if gitOK(dir, "rev-parse", "--verify", "--quiet", "refs/heads/feat/"+task) {
			t.Errorf("branch feat/%s still exists", task)
		}
	}
	if list, _ := svc.List(ctx, false); len(list) != 0 {
		t.Fatalf("manifest not empty: %d tasks", len(list))
	}
}

// TestIntegrationRmBestEffort: an unknown name is skipped and reported while the
// valid trees are removed; the command still fails (non-zero) and names the bad one.
func TestIntegrationRmBestEffort(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc := openSvc(t, ctx, dir)

	for _, task := range []string{"a", "c"} {
		if _, err := svc.New(ctx, "feat", task); err != nil {
			t.Fatal(err)
		}
	}

	results, err := svc.Remove(ctx, []string{"a", "bogus", "c"}, false)
	if exit.CodeOf(err) == exit.OK {
		t.Fatalf("exit = OK, want non-zero; err=%v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error %v does not name the failed task", err)
	}
	if len(results.Removed) != 2 {
		t.Fatalf("results = %+v, want 2 (a and c removed)", results)
	}
	for _, task := range []string{"a", "c"} {
		if _, statErr := os.Stat(worktreePath(dir, task)); !os.IsNotExist(statErr) {
			t.Errorf("worktree %s should have been removed", task)
		}
	}
}

// TestIntegrationRmThenUndoRestoresBatch: the whole batch is one op, so a single
// undo restores every removed worktree and branch.
func TestIntegrationRmThenUndoRestoresBatch(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc := openSvc(t, ctx, dir)

	for _, task := range []string{"a", "b"} {
		if _, err := svc.New(ctx, "feat", task); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Remove(ctx, []string{"a", "b"}, false); err != nil {
		t.Fatalf("rm a b: %v", err)
	}

	res, err := svc.Undo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Reverted || res.Op != "rm" {
		t.Fatalf("undo result = %+v, want a reverted rm", res)
	}
	for _, task := range []string{"a", "b"} {
		if _, statErr := os.Stat(worktreePath(dir, task)); statErr != nil {
			t.Errorf("worktree %s not restored: %v", task, statErr)
		}
		if !gitOK(dir, "rev-parse", "--verify", "--quiet", "refs/heads/feat/"+task) {
			t.Errorf("branch feat/%s not restored", task)
		}
	}
	if list, _ := svc.List(ctx, false); len(list) != 2 {
		t.Fatalf("manifest has %d tasks after undo, want 2", len(list))
	}
}

// TestIntegrationRmDedupe: a repeated name is removed once and the op undoes
// cleanly (a corrupt double-step op would fail on undo).
func TestIntegrationRmDedupe(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc := openSvc(t, ctx, dir)

	if _, err := svc.New(ctx, "feat", "demo"); err != nil {
		t.Fatal(err)
	}
	results, err := svc.Remove(ctx, []string{"demo", "demo"}, false)
	if err != nil {
		t.Fatalf("rm demo demo: %v", err)
	}
	if len(results.Removed) != 1 {
		t.Fatalf("results = %+v, want 1 (deduped)", results)
	}
	res, err := svc.Undo(ctx)
	if err != nil {
		t.Fatalf("undo after deduped rm: %v", err)
	}
	if !res.Reverted || res.Op != "rm" {
		t.Fatalf("undo result = %+v", res)
	}
	if _, statErr := os.Stat(worktreePath(dir, "demo")); statErr != nil {
		t.Errorf("worktree demo not restored: %v", statErr)
	}
}

// TestIntegrationRmDirtyNeedsForce: a dirty tree is refused without --force
// (nothing removed) and removed with it.
func TestIntegrationRmDirtyNeedsForce(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc := openSvc(t, ctx, dir)

	if _, err := svc.New(ctx, "feat", "demo"); err != nil {
		t.Fatal(err)
	}
	wt := worktreePath(dir, "demo")
	if err := os.WriteFile(filepath.Join(wt, "scratch.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Remove(ctx, []string{"demo"}, false); exit.CodeOf(err) != exit.Refused {
		t.Fatalf("exit = %d, want Refused; err=%v", exit.CodeOf(err), err)
	}
	if _, statErr := os.Stat(wt); statErr != nil {
		t.Fatalf("worktree removed despite dirty and no --force: %v", statErr)
	}

	if _, err := svc.Remove(ctx, []string{"demo"}, true); err != nil {
		t.Fatalf("rm --force demo: %v", err)
	}
	if _, statErr := os.Stat(wt); !os.IsNotExist(statErr) {
		t.Errorf("worktree still present after --force rm")
	}
}

// TestIntegrationRmSingleUnknownIsVerbatim: a single unknown task keeps the exact
// message and exit code of the pre-batch command.
func TestIntegrationRmSingleUnknownIsVerbatim(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc := openSvc(t, ctx, dir)

	_, err := svc.Remove(ctx, []string{"bogus"}, false)
	if exit.CodeOf(err) != exit.NotFound {
		t.Fatalf("exit = %d, want NotFound; err=%v", exit.CodeOf(err), err)
	}
	if err == nil || err.Error() != "unknown task: bogus" {
		t.Fatalf("error = %v, want verbatim \"unknown task: bogus\"", err)
	}
}

func TestIntegrationRmLockedTaskDoesNotBlockSibling(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc := openSvc(t, ctx, dir)
	for _, name := range []string{"locked", "ready"} {
		if _, err := svc.New(ctx, "feat", name); err != nil {
			t.Fatal(err)
		}
	}
	run(t, dir, "worktree", "lock", worktreePath(dir, "locked"))
	result, err := svc.Remove(ctx, []string{"locked", "ready"}, true)
	if exit.CodeOf(err) != exit.Refused || len(result.Removed) != 1 || result.Removed[0].Task != "ready" {
		t.Fatalf("remove result = %+v, error = %v", result, err)
	}
	if len(result.Failed) != 1 || result.Failed[0].Code != "worktree_locked" {
		t.Fatalf("failures = %+v", result.Failed)
	}
	if _, err := os.Stat(worktreePath(dir, "locked")); err != nil {
		t.Fatalf("locked worktree changed: %v", err)
	}
	if _, err := os.Stat(worktreePath(dir, "ready")); !os.IsNotExist(err) {
		t.Fatalf("ready worktree remains: %v", err)
	}
	if _, err := svc.Undo(ctx); err != nil {
		t.Fatalf("undo valid removal: %v", err)
	}
	for _, name := range []string{"locked", "ready"} {
		if _, err := os.Stat(worktreePath(dir, name)); err != nil {
			t.Errorf("%s missing after undo: %v", name, err)
		}
	}
}

func TestIntegrationRmSnapshotFailureLeavesWorkIntact(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc := openSvc(t, ctx, dir)
	if _, err := svc.New(ctx, "feat", "demo"); err != nil {
		t.Fatal(err)
	}
	svc.cfg.IncludeIgnored = []string{"missing-file"}
	scratch := filepath.Join(worktreePath(dir, "demo"), "scratch.txt")
	if err := os.WriteFile(scratch, []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Remove(ctx, []string{"demo"}, true)
	if exit.CodeOf(err) != exit.Internal || len(result.Removed) != 0 || result.Failed[0].Code != "snapshot_failed" {
		t.Fatalf("remove result = %+v, error = %v", result, err)
	}
	if _, err := os.Stat(scratch); err != nil {
		t.Fatalf("unsnapshotted work was removed: %v", err)
	}
	if list, err := svc.List(ctx, false); err != nil || len(list) != 1 {
		t.Fatalf("manifest changed: list=%+v error=%v", list, err)
	}
}

func TestIntegrationRmIgnoredFileWarningAndRecovery(t *testing.T) {
	for _, included := range []bool{false, true} {
		name := "excluded"
		if included {
			name = "included"
		}
		t.Run(name, func(t *testing.T) {
			dir := newRepo(t)
			if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("secret.txt\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			run(t, dir, "add", ".gitignore")
			run(t, dir, "commit", "-q", "-m", "ignore secret")
			ctx := context.Background()
			svc := openSvc(t, ctx, dir)
			if _, err := svc.New(ctx, "feat", "demo"); err != nil {
				t.Fatal(err)
			}
			secret := filepath.Join(worktreePath(dir, "demo"), "secret.txt")
			if err := os.WriteFile(secret, []byte("secret\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if included {
				svc.cfg.IncludeIgnored = []string{"secret.txt"}
			}
			if _, err := svc.Remove(ctx, []string{"demo"}, true); err != nil {
				t.Fatal(err)
			}
			if (len(svc.Warnings()) > 0) == included {
				t.Fatalf("warnings = %+v, included = %v", svc.Warnings(), included)
			}
			if _, err := svc.Undo(ctx); err != nil {
				t.Fatal(err)
			}
			_, statErr := os.Stat(secret)
			if included && statErr != nil || !included && !os.IsNotExist(statErr) {
				t.Fatalf("secret after undo = %v, included = %v", statErr, included)
			}
		})
	}
}

func TestIntegrationRmOrphanedWorktree(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc := openSvc(t, ctx, dir)
	if _, err := svc.New(ctx, "feat", "demo"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(worktreePath(dir, "demo")); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Remove(ctx, []string{"demo"}, false)
	if err != nil || len(result.Removed) != 1 {
		t.Fatalf("remove orphan: result=%+v error=%v", result, err)
	}
	if _, err := svc.Undo(ctx); err != nil {
		t.Fatalf("undo orphan removal: %v", err)
	}
	if _, err := os.Stat(worktreePath(dir, "demo")); err != nil {
		t.Fatalf("orphan worktree not restored: %v", err)
	}
}

func TestIntegrationRmBranchDeleteFailureRestoresTree(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc := openSvc(t, ctx, dir)
	if _, err := svc.New(ctx, "feat", "demo"); err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(worktreePath(dir, "demo"), "scratch.txt")
	if err := os.WriteFile(scratch, []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(dir, ".git", "hooks", "reference-transaction")
	script := "#!/bin/sh\nif [ \"$1\" = prepared ]; then\n  while read old new ref; do\n    if [ \"$ref\" = refs/heads/feat/demo ] && [ \"$new\" = 0000000000000000000000000000000000000000 ]; then exit 1; fi\n  done\nfi\nexit 0\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Remove(ctx, []string{"demo"}, true)
	if exit.CodeOf(err) != exit.Internal || len(result.Removed) != 0 || result.Failed[0].Code != "branch_delete_failed" {
		t.Fatalf("remove result = %+v, error = %v", result, err)
	}
	if _, err := os.Stat(scratch); err != nil {
		t.Fatalf("worktree not restored: %v", err)
	}
	if list, err := svc.List(ctx, false); err != nil || len(list) != 1 {
		t.Fatalf("manifest changed: list=%+v error=%v", list, err)
	}
}

func TestIntegrationRmRecoveryFailureRemainsUndoable(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc := openSvc(t, ctx, dir)
	if _, err := svc.New(ctx, "feat", "demo"); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(dir, ".git", "hooks", "reference-transaction")
	script := "#!/bin/sh\nif [ \"$1\" = prepared ]; then\n  while read old new ref; do\n    if [ \"$ref\" = refs/heads/feat/demo ]; then exit 1; fi\n  done\nfi\nexit 0\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Remove(ctx, []string{"demo"}, false)
	if exit.CodeOf(err) != exit.Internal || len(result.Removed) != 0 || result.Failed[0].Code != "recovery_failed" {
		t.Fatalf("remove result = %+v, error = %v", result, err)
	}
	if _, err := os.Stat(worktreePath(dir, "demo")); !os.IsNotExist(err) {
		t.Fatalf("expected partial removal, got %v", err)
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Undo(ctx); err != nil {
		t.Fatalf("undo partial removal: %v", err)
	}
	if _, err := os.Stat(worktreePath(dir, "demo")); err != nil {
		t.Fatalf("worktree not restored: %v", err)
	}
	if list, err := svc.List(ctx, false); err != nil || len(list) != 1 {
		t.Fatalf("manifest after undo = %+v, error=%v", list, err)
	}
}

func TestIntegrationRmSkipsCurrentWorktreeInBatch(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc := openSvc(t, ctx, dir)
	for _, name := range []string{"inside", "other"} {
		if _, err := svc.New(ctx, "feat", name); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(worktreePath(dir, "inside"))
	result, err := svc.Remove(ctx, []string{"inside", "other"}, false)
	if exit.CodeOf(err) != exit.Refused || len(result.Removed) != 1 || result.Removed[0].Task != "other" {
		t.Fatalf("remove result = %+v, error = %v", result, err)
	}
	if len(result.Failed) != 1 || result.Failed[0].Code != "cwd_in_target" {
		t.Fatalf("failures = %+v", result.Failed)
	}
	if _, err := os.Stat(worktreePath(dir, "inside")); err != nil {
		t.Fatalf("current worktree removed: %v", err)
	}
}

func TestIntegrationRmUndoRetryAfterPartialRestore(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc := openSvc(t, ctx, dir)
	for _, name := range []string{"a", "b"} {
		if _, err := svc.New(ctx, "feat", name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Remove(ctx, []string{"a", "b"}, false); err != nil {
		t.Fatal(err)
	}
	obstacle := worktreePath(dir, "b")
	if err := os.Mkdir(obstacle, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Undo(ctx); err == nil {
		t.Fatal("expected undo to stop at conflicting path")
	}
	if _, err := os.Stat(worktreePath(dir, "a")); err != nil {
		t.Fatalf("first worktree was not restored: %v", err)
	}
	if err := os.Remove(obstacle); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Undo(ctx); err != nil {
		t.Fatalf("retry undo: %v", err)
	}
	if list, err := svc.List(ctx, false); err != nil || len(list) != 2 {
		t.Fatalf("manifest after retry = %+v, error=%v", list, err)
	}
}
