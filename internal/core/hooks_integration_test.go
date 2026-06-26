//go:build integration && !windows

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

func revParse(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse %s: %v", ref, err)
	}
	return strings.TrimSpace(string(out))
}

// commitOnTask makes a commit in the task's worktree so it is mergeable.
func commitOnTask(t *testing.T, dir, task, file string) {
	t.Helper()
	wt := filepath.Join(dir+".worktrees", task)
	if err := os.WriteFile(filepath.Join(wt, file), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, wt, "add", ".")
	run(t, wt, "commit", "-q", "-m", "work")
}

// writeHook writes a bash hook script and returns its path.
func writeHook(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "hook.sh")
	script := "#!/usr/bin/env bash\nset -euo pipefail\n" + body + "\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func cfgWithHooks(h map[string]config.HookSpec) config.Config {
	c := config.Default()
	c.Hooks = h
	return c
}

func TestIntegrationPostCreateExtras(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	hook := writeHook(t, `printf '{"extras":{"port":"5173","slot":"%s"}}\n' "$TREEPI_SLOT" >&3`)
	cfg := cfgWithHooks(map[string]config.HookSpec{
		config.EventPostCreate: {Command: []string{"bash", hook}},
	})
	svc, err := Open(ctx, dir, cfg, clock.Real{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	info, err := svc.New(ctx, "feat", "demo")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if info.Extras["port"] != "5173" {
		t.Errorf("post_create extras not in result: %v", info.Extras)
	}
	if info.Extras["slot"] != "0" {
		t.Errorf("TREEPI_SLOT not seen by hook: %v", info.Extras)
	}
	// The extras must also be persisted in the manifest (the ls/dash path).
	tasks, err := svc.List(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Extras["port"] != "5173" {
		t.Errorf("extras not persisted to manifest: %+v", tasks)
	}
}

func TestIntegrationPostCreateRollback(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	hook := writeHook(t, `exit 1`) // default on_failure for post_create is rollback
	cfg := cfgWithHooks(map[string]config.HookSpec{
		config.EventPostCreate: {Command: []string{"bash", hook}},
	})
	svc, err := Open(ctx, dir, cfg, clock.Real{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.New(ctx, "feat", "demo")
	if exit.CodeOf(err) != exit.HookAbort {
		t.Fatalf("exit code = %d, want %d (HookAbort); err=%v", exit.CodeOf(err), exit.HookAbort, err)
	}
	if _, statErr := os.Stat(filepath.Join(dir+".worktrees", "demo")); !os.IsNotExist(statErr) {
		t.Error("worktree should have been removed on rollback")
	}
	if gitOK(dir, "rev-parse", "--verify", "refs/heads/feat/demo") {
		t.Error("branch should have been deleted on rollback")
	}
	tasks, err := svc.List(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected no tasks after rollback, got %d", len(tasks))
	}
}

func TestIntegrationPostCreateWarnDoesNotRollback(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	hook := writeHook(t, `exit 1`)
	cfg := cfgWithHooks(map[string]config.HookSpec{
		config.EventPostCreate: {Command: []string{"bash", hook}, OnFailure: config.OnFailureWarn},
	})
	svc, err := Open(ctx, dir, cfg, clock.Real{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	info, err := svc.New(ctx, "feat", "demo")
	if err != nil {
		t.Fatalf("warn hook should not fail New: %v", err)
	}
	if info.Status != "ready" {
		t.Errorf("status = %q, want ready", info.Status)
	}
	if len(svc.Warnings()) == 0 {
		t.Error("expected a warning to be recorded")
	}
}

func TestIntegrationPostSyncExtras(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	pc := writeHook(t, `printf '{"extras":{"phase":"created"}}\n' >&3`)
	ps := writeHook(t, `printf '{"extras":{"phase":"synced"}}\n' >&3`)
	cfg := cfgWithHooks(map[string]config.HookSpec{
		config.EventPostCreate: {Command: []string{"bash", pc}},
		config.EventPostSync:   {Command: []string{"bash", ps}},
	})
	svc, err := Open(ctx, dir, cfg, clock.Real{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.New(ctx, "feat", "demo"); err != nil {
		t.Fatal(err)
	}
	info, err := svc.Sync(ctx, "demo")
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if info.Extras["phase"] != "synced" {
		t.Errorf("post_sync extras not merged: %v", info.Extras)
	}
}

func TestIntegrationPreMergeAbort(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	hook := writeHook(t, `exit 1`) // default on_failure for pre_merge is abort
	cfg := cfgWithHooks(map[string]config.HookSpec{
		config.EventPreMerge: {Command: []string{"bash", hook}},
	})
	svc, err := Open(ctx, dir, cfg, clock.Real{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.New(ctx, "feat", "demo"); err != nil {
		t.Fatal(err)
	}
	commitOnTask(t, dir, "demo", "feature.txt")
	trunkBefore := revParse(t, dir, "refs/heads/main")

	_, err = svc.Merge(ctx, "demo")
	if exit.CodeOf(err) != exit.HookAbort {
		t.Fatalf("exit = %d, want %d (HookAbort); err=%v", exit.CodeOf(err), exit.HookAbort, err)
	}
	if !gitOK(dir, "rev-parse", "--verify", "refs/heads/feat/demo") {
		t.Error("branch should be left intact after pre_merge abort")
	}
	if got := revParse(t, dir, "refs/heads/main"); got != trunkBefore {
		t.Errorf("trunk advanced (%s -> %s) despite pre_merge abort", trunkBefore, got)
	}
}

func TestIntegrationVerifyRunsBeforePreMerge(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	hook := writeHook(t, `exit 1`)
	cfg := cfgWithHooks(map[string]config.HookSpec{
		config.EventPreMerge: {Command: []string{"bash", hook}},
	})
	cfg.Verify = []string{"bash", "-c", "exit 1"} // verify fails first -> exit 8, not 9
	svc, err := Open(ctx, dir, cfg, clock.Real{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.New(ctx, "feat", "demo"); err != nil {
		t.Fatal(err)
	}
	commitOnTask(t, dir, "demo", "feature.txt")

	_, err = svc.Merge(ctx, "demo")
	if exit.CodeOf(err) != exit.VerifyFailed {
		t.Fatalf("exit = %d, want %d (VerifyFailed first); err=%v", exit.CodeOf(err), exit.VerifyFailed, err)
	}
}

func TestIntegrationPostMergeWarnDoesNotFail(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	hook := writeHook(t, `exit 1`) // post_merge defaults to warn
	cfg := cfgWithHooks(map[string]config.HookSpec{
		config.EventPostMerge: {Command: []string{"bash", hook}},
	})
	svc, err := Open(ctx, dir, cfg, clock.Real{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.New(ctx, "feat", "demo"); err != nil {
		t.Fatal(err)
	}
	commitOnTask(t, dir, "demo", "feature.txt")

	if _, err := svc.Merge(ctx, "demo"); err != nil {
		t.Fatalf("post_merge warn must not fail the merge: %v", err)
	}
	if len(svc.Warnings()) == 0 {
		t.Error("expected a post_merge warning")
	}
	if gitOK(dir, "rev-parse", "--verify", "refs/heads/feat/demo") {
		t.Error("branch should be gone after a successful merge")
	}
}

func TestIntegrationPreRemoveAbort(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	hook := writeHook(t, `exit 1`)
	cfg := cfgWithHooks(map[string]config.HookSpec{
		config.EventPreRemove: {Command: []string{"bash", hook}, OnFailure: config.OnFailureAbort},
	})
	svc, err := Open(ctx, dir, cfg, clock.Real{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.New(ctx, "feat", "demo"); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Remove(ctx, "demo", false)
	if exit.CodeOf(err) != exit.HookAbort {
		t.Fatalf("exit = %d, want %d (HookAbort); err=%v", exit.CodeOf(err), exit.HookAbort, err)
	}
	if _, statErr := os.Stat(filepath.Join(dir+".worktrees", "demo")); statErr != nil {
		t.Error("worktree should be left intact after pre_remove abort")
	}
	if !gitOK(dir, "rev-parse", "--verify", "refs/heads/feat/demo") {
		t.Error("branch should be left intact after pre_remove abort")
	}
}
