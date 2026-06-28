package git

import (
	"context"
	"errors"
	"strings"
)

// ErrConflict indicates a rebase (or merge) stopped on conflicts. Callers take
// the conflict-snapshot path: capture, abort, record, mark needs-resolution.
var ErrConflict = errors.New("rebase conflict")

// ErrNoUpstream indicates a branch has no configured upstream / remote-tracking
// ref, so the caller falls back to the local trunk ref (offline path).
var ErrNoUpstream = errors.New("no upstream")

// UpstreamRef returns the remote-tracking ref for branch (e.g.
// "refs/remotes/origin/main"), honoring a non-origin remote. Returns
// ErrNoUpstream when the branch has no upstream configured.
func (c *Client) UpstreamRef(ctx context.Context, dir, branch string) (string, error) {
	ref, err := c.out(ctx, dir, "rev-parse", "--symbolic-full-name", "--verify", "--quiet", branch+"@{upstream}")
	if err != nil {
		var ce *CmdError
		if errors.As(err, &ce) && ce.ExitCode() == 1 {
			return "", ErrNoUpstream
		}
		return "", err
	}
	if strings.TrimSpace(ref) == "" {
		return "", ErrNoUpstream
	}
	return ref, nil
}

// Fetch updates remote-tracking refs for trunk from remote.
func (c *Client) Fetch(ctx context.Context, dir, remote, trunk string) error {
	return c.runVoid(ctx, dir, "fetch", "--quiet", remote, trunk)
}

// Rebase rebases the worktree at dir onto base. On conflict it returns
// ErrConflict (the rebase is left in progress; the caller captures then aborts).
func (c *Client) Rebase(ctx context.Context, dir, base string) error {
	_, err := c.out(ctx, dir, "rebase", base)
	if err == nil {
		return nil
	}
	if c.rebaseInProgress(ctx, dir) {
		return ErrConflict
	}
	return err
}

// RebaseAbort aborts an in-progress rebase, returning the worktree to a clean
// pre-rebase state.
func (c *Client) RebaseAbort(ctx context.Context, dir string) error {
	return c.runVoid(ctx, dir, "rebase", "--abort")
}

// rebaseInProgress reports whether a rebase stopped with unmerged paths - the
// signal that distinguishes a conflict (which the caller captures + aborts)
// from any other rebase failure.
func (c *Client) rebaseInProgress(ctx context.Context, dir string) bool {
	out, err := c.out(ctx, dir, "ls-files", "--unmerged")
	return err == nil && strings.TrimSpace(out) != ""
}

// MergeFFOnly fast-forwards the current branch in the worktree at dir to
// branch, refusing (error) if it is not a fast-forward.
func (c *Client) MergeFFOnly(ctx context.Context, dir, branch string) error {
	return c.runVoid(ctx, dir, "merge", "--ff-only", branch)
}
