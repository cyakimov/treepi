//go:build integration

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cyakimov/treepi/internal/clock"
	"github.com/cyakimov/treepi/internal/config"
	"github.com/cyakimov/treepi/internal/exit"
)

func leasePID(t *testing.T, dir, task string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".git", "treepi", "leases", task+".lease"))
	if err != nil {
		t.Fatalf("read lease file for %s: %v", task, err)
	}
	var lf struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(b, &lf); err != nil {
		t.Fatal(err)
	}
	return lf.PID
}

func TestIntegrationClaimFreeTree(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc := openSvc(t, ctx, dir)
	if _, err := svc.New(ctx, "feat", "demo"); err != nil {
		t.Fatal(err)
	}
	info, err := svc.Claim(ctx, ClaimRequest{})
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if info.Task != "demo" {
		t.Errorf("claimed %q, want demo", info.Task)
	}
	if info.Lease == nil {
		t.Fatal("claim must produce a lease")
	}
	if pid := leasePID(t, dir, "demo"); pid != 0 {
		t.Errorf("claim lease PID = %d, want 0 (durable/TTL-only)", pid)
	}
}

func TestIntegrationClaimCreatesWhenNoneFree(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	info, err := openSvc(t, ctx, dir).Claim(ctx, ClaimRequest{})
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if !strings.HasPrefix(info.Task, "claim-") {
		t.Errorf("auto-created task = %q, want claim-*", info.Task)
	}
	if info.Lease == nil {
		t.Error("created claim must hold a lease")
	}
}

func TestIntegrationClaimNoCreateExit5(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	_, err := openSvc(t, ctx, dir).Claim(ctx, ClaimRequest{NoCreate: true})
	if exit.CodeOf(err) != exit.NoneAvailable {
		t.Fatalf("exit = %d, want %d (NoneAvailable)", exit.CodeOf(err), exit.NoneAvailable)
	}
}

func TestIntegrationDoubleClaimDistinct(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc := openSvc(t, ctx, dir)
	if _, err := svc.New(ctx, "feat", "demo"); err != nil {
		t.Fatal(err)
	}
	first, err := svc.Claim(ctx, ClaimRequest{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Claim(ctx, ClaimRequest{})
	if err != nil {
		t.Fatalf("second claim should create a fresh tree: %v", err)
	}
	if second.Task == first.Task {
		t.Errorf("second claim returned the already-claimed task %q", first.Task)
	}
	if second.Slot == first.Slot {
		t.Errorf("two claims share slot %d", first.Slot)
	}
}

func TestIntegrationReleaseNonOwner(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc := openSvc(t, ctx, dir)
	if _, err := svc.New(ctx, "feat", "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Claim(ctx, ClaimRequest{Task: "demo", Owner: "someone-else"}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Release(ctx, "demo", ReleaseOptions{})
	if exit.CodeOf(err) != exit.LeaseHeld {
		t.Fatalf("release by non-owner exit = %d, want %d (LeaseHeld)", exit.CodeOf(err), exit.LeaseHeld)
	}
	res, err := svc.Release(ctx, "demo", ReleaseOptions{Force: true})
	if err != nil {
		t.Fatalf("forced release: %v", err)
	}
	if !res.LeaseCleared {
		t.Error("forced release should clear the lease")
	}
}

func TestIntegrationReleaseRm(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc := openSvc(t, ctx, dir)
	if _, err := svc.New(ctx, "feat", "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Claim(ctx, ClaimRequest{Task: "demo"}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Release(ctx, "demo", ReleaseOptions{Rm: true})
	if err != nil {
		t.Fatalf("release --rm: %v", err)
	}
	if !res.Removed {
		t.Error("release --rm should remove the worktree")
	}
	if _, statErr := os.Stat(filepath.Join(dir+".worktrees", "demo")); !os.IsNotExist(statErr) {
		t.Error("worktree should be gone after release --rm")
	}
}

func TestIntegrationRenewExtendsLease(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	fake := &clock.Fake{T: time.Unix(1000, 0).UTC()}
	svc, err := Open(ctx, dir, config.Default(), fake, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.New(ctx, "feat", "demo"); err != nil {
		t.Fatal(err)
	}
	first, err := svc.Claim(ctx, ClaimRequest{Task: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	exp1 := first.Lease.Expires

	fake.Advance(time.Minute)
	second, err := svc.Renew(ctx, "demo", "")
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if !second.Lease.Expires.After(exp1) {
		t.Errorf("renew did not extend the lease: %v -> %v", exp1, second.Lease.Expires)
	}
}

func TestIntegrationConcurrentClaimsDistinct(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	svc := openSvc(t, ctx, dir)
	const n = 4
	for i := 0; i < n; i++ {
		if _, err := svc.New(ctx, "feat", fmt.Sprintf("t%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	// n claimers race for n free trees: the flock + manifest CAS must hand each a
	// distinct tree (no creation - NoCreate isolates the claim race from git).
	type result struct {
		info *TaskInfo
		err  error
	}
	ch := make(chan result, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			info, err := svc.Claim(ctx, ClaimRequest{NoCreate: true})
			ch <- result{info, err}
		}()
	}
	wg.Wait()
	close(ch)

	tasks := map[string]bool{}
	for r := range ch {
		if r.err != nil {
			t.Errorf("concurrent claim failed: %v", r.err)
			continue
		}
		if tasks[r.info.Task] {
			t.Errorf("task %q claimed twice", r.info.Task)
		}
		tasks[r.info.Task] = true
	}
	if len(tasks) != n {
		t.Errorf("got %d distinct claims, want %d", len(tasks), n)
	}
}

func TestIntegrationRenewLapsedLease(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()
	fake := &clock.Fake{T: time.Unix(1000, 0).UTC()}
	svc, err := Open(ctx, dir, config.Default(), fake, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.New(ctx, "feat", "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Claim(ctx, ClaimRequest{Task: "demo"}); err != nil {
		t.Fatal(err)
	}
	fake.Advance(3 * time.Hour) // past the 2h TTL -> reconcile clears it
	_, err = svc.Renew(ctx, "demo", "")
	if exit.CodeOf(err) != exit.LeaseHeld {
		t.Fatalf("renew of a lapsed lease exit = %d, want %d (LeaseHeld)", exit.CodeOf(err), exit.LeaseHeld)
	}
}
