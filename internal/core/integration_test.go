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

func gitTip(t *testing.T, dir, rev string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", rev)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse %s: %v", rev, err)
	}
	return strings.TrimSpace(string(out))
}

// advanceTask commits on a task's worktree so it is one commit ahead of trunk.
func mergeSetup(t *testing.T) (string, *Service, *TaskInfo) {
	t.Helper()
	dir := newRepo(t)
	svc, err := Open(context.Background(), dir, config.Default(), clock.Real{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	info, err := svc.New(context.Background(), "feat", "x")
	if err != nil {
		t.Fatal(err)
	}
	run(t, info.Path, "commit", "--allow-empty", "-q", "-m", "x work")
	return dir, svc, info
}

func TestIntegrationMergeTrunkCheckedOutStaysClean(t *testing.T) {
	dir, svc, info := mergeSetup(t)
	ctx := context.Background()
	featTip := gitTip(t, info.Path, "HEAD")

	if _, err := svc.Merge(ctx, "x"); err != nil {
		t.Fatalf("merge: %v", err)
	}
	// Trunk fast-forwarded to the feature tip.
	if got := gitTip(t, dir, "main"); got != featTip {
		t.Fatalf("main = %s, want feature tip %s", got, featTip)
	}
	// C3: the checked-out trunk worktree is clean (no phantom modifications).
	if !gitOK(dir, "diff", "--quiet") || !gitOK(dir, "diff", "--cached", "--quiet") {
		t.Fatal("trunk worktree is dirty after merge (phantom modifications)")
	}
	// Worktree, branch, and manifest entry are gone.
	if _, err := os.Stat(info.Path); !os.IsNotExist(err) {
		t.Fatal("feature worktree still present")
	}
	if gitOK(dir, "rev-parse", "--verify", "--quiet", "refs/heads/feat/x") {
		t.Fatal("feat/x branch still exists")
	}
	if list, _ := svc.List(ctx, false); len(list) != 0 {
		t.Fatalf("manifest not empty: %d", len(list))
	}
}

func TestIntegrationMergeTrunkNotCheckedOut(t *testing.T) {
	dir, svc, info := mergeSetup(t)
	ctx := context.Background()
	featTip := gitTip(t, info.Path, "HEAD")
	// Detach the main worktree so trunk is not checked out anywhere.
	run(t, dir, "switch", "--detach", "-q")

	if _, err := svc.Merge(ctx, "x"); err != nil {
		t.Fatalf("merge (not checked out): %v", err)
	}
	if got := gitTip(t, dir, "refs/heads/main"); got != featTip {
		t.Fatalf("main ref = %s, want %s (CAS ff)", got, featTip)
	}
}

func TestIntegrationMergeThenUndoRewindsTrunk(t *testing.T) {
	dir, svc, info := mergeSetup(t)
	ctx := context.Background()
	preMain := gitTip(t, dir, "main")

	if _, err := svc.Merge(ctx, "x"); err != nil {
		t.Fatalf("merge: %v", err)
	}
	res, err := svc.Undo(ctx)
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if !res.Reverted || res.Op != "merge" {
		t.Fatalf("undo result = %+v", res)
	}
	// Trunk rewound, the checked-out trunk worktree clean, branch + worktree + task back.
	if got := gitTip(t, dir, "main"); got != preMain {
		t.Fatalf("main = %s, want pre-merge %s", got, preMain)
	}
	if !gitOK(dir, "diff", "--quiet") {
		t.Fatal("trunk worktree dirty after undo (rewind corrupted it)")
	}
	if !gitOK(dir, "rev-parse", "--verify", "--quiet", "refs/heads/feat/x") {
		t.Fatal("feat/x branch not restored")
	}
	if _, err := os.Stat(info.Path); err != nil {
		t.Fatalf("feature worktree not restored: %v", err)
	}
	if list, _ := svc.List(ctx, false); len(list) != 1 {
		t.Fatalf("task not restored to manifest: %d", len(list))
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
