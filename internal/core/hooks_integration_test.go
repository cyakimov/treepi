//go:build integration && !windows

package core

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/cyakimov/treepi/internal/clock"
	"github.com/cyakimov/treepi/internal/config"
	"github.com/cyakimov/treepi/internal/exit"
)

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
