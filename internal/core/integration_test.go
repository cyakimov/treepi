//go:build integration

package core

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyakimov/treepi/internal/clock"
	"github.com/cyakimov/treepi/internal/config"
	"github.com/cyakimov/treepi/internal/exit"
)

func gitEnv() []string {
	return append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// gitOK runs git and reports whether it exited zero (no fatal on failure).
func gitOK(dir string, args ...string) bool {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	return cmd.Run() == nil
}

func newRepo(t *testing.T) string {
	t.Helper()
	// hermetic config for the whole process so core's git client never signs.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-q", "-m", "init")
	t.Cleanup(func() { _ = os.RemoveAll(dir + ".worktrees") })
	return dir
}

func TestIntegrationNewListWhere(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()

	svc, err := Open(ctx, dir, config.Default(), clock.Real{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	info, err := svc.New(ctx, "feat", "login")
	if err != nil {
		t.Fatal(err)
	}
	if info.Branch != "feat/login" || info.Slot != 0 || info.Status != "ready" {
		t.Fatalf("new result = %+v", info)
	}
	// Sibling-adjacent: the worktree's parent is <resolved-repo-root>.worktrees.
	wantBase := svc.Repo().Root + ".worktrees"
	if got := filepath.Dir(info.Path); got != wantBase {
		t.Fatalf("worktree base = %q, want %q (sibling-adjacent)", got, wantBase)
	}
	if st, err := os.Stat(info.Path); err != nil || !st.IsDir() {
		t.Fatalf("worktree dir missing at %s: %v", info.Path, err)
	}

	// Second task gets the next slot.
	info2, err := svc.New(ctx, "fix", "bug")
	if err != nil {
		t.Fatal(err)
	}
	if info2.Slot != 1 {
		t.Fatalf("second slot = %d, want 1", info2.Slot)
	}

	// Duplicate task is refused.
	if _, err := svc.New(ctx, "feat", "login"); exit.CodeOf(err) != exit.Usage {
		t.Fatalf("duplicate task: got code %d, want Usage", exit.CodeOf(err))
	}

	// List sees both, sorted by name.
	list, err := svc.List(ctx, true)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: len=%d err=%v", len(list), err)
	}
	if list[0].Task != "bug" || list[1].Task != "login" {
		t.Fatalf("list order = %s, %s", list[0].Task, list[1].Task)
	}

	// Where resolves a path; unknown task is NotFound.
	if p, err := svc.Where(ctx, "login"); err != nil || p != info.Path {
		t.Fatalf("where login = %q, %v", p, err)
	}
	if _, err := svc.Where(ctx, "ghost"); exit.CodeOf(err) != exit.NotFound {
		t.Fatalf("where ghost: got code %d, want NotFound", exit.CodeOf(err))
	}
}

func TestIntegrationNewThenUndo(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc, err := Open(ctx, dir, config.Default(), clock.Real{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	info, err := svc.New(ctx, "feat", "x")
	if err != nil {
		t.Fatal(err)
	}

	res, err := svc.Undo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Reverted || res.Op != "new" {
		t.Fatalf("undo result = %+v", res)
	}
	// Worktree dir, branch, and manifest entry are all gone.
	if _, err := os.Stat(info.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree still present after undo: %v", err)
	}
	if gitOK(dir, "rev-parse", "--verify", "--quiet", "refs/heads/feat/x") {
		t.Fatal("branch feat/x still exists after undo")
	}
	if list, _ := svc.List(ctx, false); len(list) != 0 {
		t.Fatalf("manifest not empty after undo: %d tasks", len(list))
	}
	// A second undo is a no-op.
	if res2, _ := svc.Undo(ctx); res2.Reverted {
		t.Fatal("double undo should be a no-op")
	}
}

func TestIntegrationSyncRebasesOntoTrunk(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc, err := Open(ctx, dir, config.Default(), clock.Real{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.New(ctx, "feat", "x"); err != nil {
		t.Fatal(err)
	}
	// Advance trunk after the worktree was cut.
	run(t, dir, "commit", "--allow-empty", "-q", "-m", "trunk advance")

	info, err := svc.Sync(ctx, "x")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if info.Status != "ready" {
		t.Fatalf("status after sync = %s", info.Status)
	}
	// The feature branch now descends from the advanced trunk tip.
	if !gitOK(dir, "merge-base", "--is-ancestor", "main", "feat/x") {
		t.Fatal("feat/x was not rebased onto the advanced trunk")
	}
}
