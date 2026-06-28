package git

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestWorktreeAddArgs(t *testing.T) {
	f := newFakeRunner()
	c := NewClient(f)
	if err := c.WorktreeAdd(context.Background(), "/repo", "/repo.worktrees/x", "feat/x", "origin/main"); err != nil {
		t.Fatal(err)
	}
	want := "worktree add -b feat/x /repo.worktrees/x origin/main"
	if got := strings.Join(f.lastCall(), " "); got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestUpdateRefCASArgs(t *testing.T) {
	f := newFakeRunner()
	c := NewClient(f)
	if err := c.UpdateRefCAS(context.Background(), "/repo", "refs/heads/main", "new", "old"); err != nil {
		t.Fatal(err)
	}
	want := "update-ref refs/heads/main new old"
	if got := strings.Join(f.lastCall(), " "); got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestIsAncestorClassifiesExit1AsFalse(t *testing.T) {
	ctx := context.Background()

	// exit 1 => not an ancestor, no error
	f := newFakeRunner().on("merge-base --is-ancestor a b", fakeResult{err: fakeExit{1}})
	if ok, err := NewClient(f).IsAncestor(ctx, "/repo", "a", "b"); err != nil || ok {
		t.Fatalf("exit1: got ok=%v err=%v, want false,nil", ok, err)
	}

	// exit 0 => ancestor
	f = newFakeRunner() // default result is success
	if ok, err := NewClient(f).IsAncestor(ctx, "/repo", "a", "b"); err != nil || !ok {
		t.Fatalf("exit0: got ok=%v err=%v, want true,nil", ok, err)
	}

	// other exit => surfaced as error
	f = newFakeRunner().on("merge-base --is-ancestor a b", fakeResult{err: fakeExit{128}, stderr: "fatal"})
	if _, err := NewClient(f).IsAncestor(ctx, "/repo", "a", "b"); err == nil {
		t.Fatalf("exit128: expected an error")
	}
}

func TestResolveRefMissing(t *testing.T) {
	f := newFakeRunner().on("rev-parse --verify --quiet nope^{commit}", fakeResult{err: fakeExit{1}})
	_, err := NewClient(f).ResolveRef(context.Background(), "/repo", "nope")
	if !errors.Is(err, ErrRefNotFound) {
		t.Fatalf("got %v, want ErrRefNotFound", err)
	}
}

func TestUpstreamRefNone(t *testing.T) {
	f := newFakeRunner().on(
		"rev-parse --symbolic-full-name --verify --quiet main@{upstream}",
		fakeResult{err: fakeExit{1}},
	)
	_, err := NewClient(f).UpstreamRef(context.Background(), "/repo", "main")
	if !errors.Is(err, ErrNoUpstream) {
		t.Fatalf("got %v, want ErrNoUpstream", err)
	}
}

func TestCheckRefFormatRejectsLeadingDash(t *testing.T) {
	// no runner call expected for the leading-dash short-circuit
	f := newFakeRunner()
	if err := NewClient(f).CheckRefFormat(context.Background(), "-x"); err == nil {
		t.Fatalf("expected rejection of leading-dash branch")
	}
	if len(f.calls) != 0 {
		t.Fatalf("did not expect a git call for the leading-dash case")
	}
}
