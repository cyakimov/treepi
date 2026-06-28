//go:build integration

package core

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyakimov/treepi/internal/clock"
	"github.com/cyakimov/treepi/internal/config"
	"github.com/cyakimov/treepi/internal/exit"
)

func openSvc(t *testing.T, ctx context.Context, dir string) *Service {
	t.Helper()
	svc, err := Open(ctx, dir, config.Default(), clock.Real{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func fileHas(t *testing.T, path, substr string) bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Contains(string(b), substr)
}

func TestIntegrationInitScaffolds(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	res, err := openSvc(t, ctx, dir).Init(ctx, false)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !fileHas(t, filepath.Join(dir, ".treepi.toml"), "[hooks.post_create]") {
		t.Error(".treepi.toml missing the hooks section")
	}
	for _, n := range []string{"post-create.sh", "post-sync.sh", "pre-merge.sh", "post-merge.sh", "pre-remove.sh"} {
		if _, err := os.Stat(filepath.Join(dir, ".treepi", "hooks", n)); err != nil {
			t.Errorf("missing hook stub %s", n)
		}
	}
	if !res.GitignoreUpdated {
		t.Error("expected .gitignore to be updated")
	}
	if !fileHas(t, filepath.Join(dir, ".gitignore"), ".treepi.local.toml") {
		t.Error(".gitignore missing the local-override line")
	}
}

func TestIntegrationInitRefusesExistingThenForces(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc := openSvc(t, ctx, dir)
	if _, err := svc.Init(ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Init(ctx, false); exit.CodeOf(err) != exit.Refused {
		t.Fatalf("second init exit = %d, want %d (Refused)", exit.CodeOf(err), exit.Refused)
	}
	if _, err := svc.Init(ctx, true); err != nil {
		t.Fatalf("--force should overwrite: %v", err)
	}
}

func TestIntegrationInitGitignoreIdempotent(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules\n.treepi.local.toml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := openSvc(t, ctx, dir).Init(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.GitignoreUpdated {
		t.Error("should not re-add an already-present .gitignore line")
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if n := strings.Count(string(b), ".treepi.local.toml"); n != 1 {
		t.Errorf(".gitignore has %d occurrences of the line, want 1", n)
	}
}

func TestIntegrationInitRefusesLinkedWorktree(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	linked := filepath.Join(t.TempDir(), "linked")
	run(t, dir, "worktree", "add", "-b", "tmp", linked)
	_, err := openSvc(t, ctx, linked).Init(ctx, false)
	if exit.CodeOf(err) != exit.Usage {
		t.Fatalf("init in a linked worktree exit = %d, want %d (Usage); err=%v", exit.CodeOf(err), exit.Usage, err)
	}
}
