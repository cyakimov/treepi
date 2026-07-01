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
	if len(results) != 2 {
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
	if len(results) != 2 {
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
	if len(results) != 1 {
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
