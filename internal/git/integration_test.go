//go:build integration

package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var idEnv = []string{
	"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
	"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
}

func mustGit(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func newTestRepo(t *testing.T) (string, *Client) {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, nil, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, idEnv, "add", "tracked.txt")
	mustGit(t, dir, idEnv, "commit", "-q", "-m", "init")
	return dir, NewClient(ExecRunner{})
}

func TestIntegrationCommonDirIsAbsoluteFromSubdir(t *testing.T) {
	dir, c := newTestRepo(t)
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	fromRoot, err := c.CommonDir(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	fromSub, err := c.CommonDir(context.Background(), sub)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(fromRoot) || !filepath.IsAbs(fromSub) {
		t.Fatalf("common-dir not absolute: root=%q sub=%q", fromRoot, fromSub)
	}
	if fromRoot != fromSub {
		t.Fatalf("common-dir differs by cwd: root=%q sub=%q", fromRoot, fromSub)
	}
}

func TestIntegrationWorktreeRoundTrip(t *testing.T) {
	dir, c := newTestRepo(t)
	ctx := context.Background()
	wt := filepath.Join(t.TempDir(), "feat-x")
	if err := c.WorktreeAdd(ctx, dir, wt, "feat/x", "HEAD"); err != nil {
		t.Fatal(err)
	}
	wts, err := c.WorktreeList(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := FindWorktreeOnBranch(wts, "feat/x"); !ok {
		t.Fatalf("feat/x worktree not found in %+v", wts)
	}
}

func TestIntegrationSnapshotHonorsGitignore(t *testing.T) {
	dir, c := newTestRepo(t)
	ctx := context.Background()

	write(t, dir, ".gitignore", "ignored.txt\n")
	mustGit(t, dir, idEnv, "add", ".gitignore")
	mustGit(t, dir, idEnv, "commit", "-q", "-m", "gitignore")

	write(t, dir, "tracked.txt", "v2\n")     // modified tracked
	write(t, dir, "untracked.txt", "new\n")  // untracked, not ignored
	write(t, dir, "ignored.txt", "secret\n") // gitignored

	// Default snapshot: gitignored file must be excluded.
	oid, created, err := c.Snapshot(ctx, dir, "refs/treepi/snapshots/test/a", Identity{}, nil)
	if err != nil || !created {
		t.Fatalf("snapshot: oid=%q created=%v err=%v", oid, created, err)
	}
	files := treeFiles(t, dir, oid)
	if !files["tracked.txt"] || !files["untracked.txt"] {
		t.Fatalf("snapshot missing tracked/untracked: %v", files)
	}
	if files["ignored.txt"] {
		t.Fatalf("snapshot must NOT capture gitignored ignored.txt (C1): %v", files)
	}

	// Opt-in include_ignored: now the gitignored file is captured.
	oid2, _, err := c.Snapshot(ctx, dir, "refs/treepi/snapshots/test/b", Identity{}, []string{"ignored.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !treeFiles(t, dir, oid2)["ignored.txt"] {
		t.Fatalf("include_ignored snapshot must capture ignored.txt")
	}

	// The real index and working tree are untouched by the capture.
	if status := mustGit(t, dir, nil, "status", "--porcelain"); !strings.Contains(status, "tracked.txt") {
		t.Fatalf("snapshot disturbed the working tree/index: %q", status)
	}
}

func TestIntegrationSnapshotNoConfiguredIdentity(t *testing.T) {
	dir, _ := newTestRepo(t)
	ctx := context.Background()
	write(t, dir, "tracked.txt", "changed\n")

	// Nullify global+system git config so there is NO ambient identity; the
	// snapshot must still succeed because it stamps one explicitly (C5).
	noID := NewClient(ExecRunner{Env: []string{
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_CONFIG_SYSTEM=" + os.DevNull,
	}})
	oid, created, err := noID.Snapshot(ctx, dir, "refs/treepi/snapshots/test/id", Identity{}, nil)
	if err != nil || !created || oid == "" {
		t.Fatalf("snapshot without configured identity failed: oid=%q created=%v err=%v", oid, created, err)
	}
}

func TestIntegrationFastForwardCAS(t *testing.T) {
	dir, c := newTestRepo(t)
	ctx := context.Background()

	a, err := c.ResolveRef(ctx, dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	// A branch "target" at A, never checked out (the no-trunk-worktree path).
	mustGit(t, dir, idEnv, "branch", "target", a)
	// A feature branch one commit ahead.
	mustGit(t, dir, idEnv, "switch", "-q", "-c", "feat", a)
	write(t, dir, "tracked.txt", "feat\n")
	mustGit(t, dir, idEnv, "commit", "-q", "-am", "b")
	b := mustGit(t, dir, idEnv, "rev-parse", "HEAD")
	mustGit(t, dir, idEnv, "switch", "-q", "main")

	if ok, err := c.IsAncestor(ctx, dir, "target", "feat"); err != nil || !ok {
		t.Fatalf("IsAncestor(target, feat) = %v, %v; want true", ok, err)
	}
	if err := c.UpdateRefCAS(ctx, dir, "refs/heads/target", b, a); err != nil {
		t.Fatalf("CAS ff: %v", err)
	}
	if got, _ := c.ResolveRef(ctx, dir, "target"); got != b {
		t.Fatalf("target = %s, want %s", got, b)
	}
	// Stale-old CAS must fail.
	if err := c.UpdateRefCAS(ctx, dir, "refs/heads/target", a, a); err == nil {
		t.Fatalf("expected CAS to refuse a stale old-value")
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func treeFiles(t *testing.T, dir, oid string) map[string]bool {
	t.Helper()
	out := mustGit(t, dir, nil, "ls-tree", "-r", "--name-only", oid)
	files := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if p := strings.TrimSpace(line); p != "" {
			files[p] = true
		}
	}
	return files
}
