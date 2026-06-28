package git

import (
	"context"
	"strings"
)

// Worktree is one entry from `git worktree list --porcelain`.
type Worktree struct {
	Path     string
	Head     string // OID, or "" when detached output omits it
	Branch   string // full ref (refs/heads/...), "" when detached
	Detached bool
	Bare     bool
	Locked   bool
	Prunable bool
}

// ShortBranch returns the branch name without the refs/heads/ prefix.
func (w Worktree) ShortBranch() string {
	return strings.TrimPrefix(w.Branch, "refs/heads/")
}

// parseWorktreeList parses the porcelain output of `git worktree list
// --porcelain`. Records are separated by blank lines; each line is a
// "key value" or a bare attribute keyword.
func parseWorktreeList(out []byte) []Worktree {
	var (
		res []Worktree
		cur Worktree
		set bool
	)
	flush := func() {
		if set {
			res = append(res, cur)
		}
		cur, set = Worktree{}, false
	}
	for _, raw := range strings.Split(string(out), "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" {
			flush()
			continue
		}
		set = true
		key, val, _ := strings.Cut(line, " ")
		switch key {
		case "worktree":
			cur.Path = val
		case "HEAD":
			cur.Head = val
		case "branch":
			cur.Branch = val
		case "detached":
			cur.Detached = true
		case "bare":
			cur.Bare = true
		case "locked":
			cur.Locked = true
		case "prunable":
			cur.Prunable = true
		}
	}
	flush()
	return res
}

// WorktreeList returns all worktrees registered in the repo at dir.
func (c *Client) WorktreeList(ctx context.Context, dir string) ([]Worktree, error) {
	out, err := c.out(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktreeList([]byte(out)), nil
}

// WorktreeAdd creates a worktree at path checked out on a new branch cut from
// startPoint (an OID or ref).
func (c *Client) WorktreeAdd(ctx context.Context, dir, path, branch, startPoint string) error {
	return c.runVoid(ctx, dir, "worktree", "add", "-b", branch, path, startPoint)
}

// WorktreeRemove removes the worktree at path. force allows removal of a dirty
// or locked tree. Run from a directory that is not inside the target.
func (c *Client) WorktreeRemove(ctx context.Context, dir, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	return c.runVoid(ctx, dir, args...)
}

// WorktreePrune drops administrative entries for worktrees whose directories
// have been deleted out from under git.
func (c *Client) WorktreePrune(ctx context.Context, dir string) error {
	return c.runVoid(ctx, dir, "worktree", "prune")
}

// FindWorktreeOnBranch returns the worktree (if any) currently checked out on
// the given short branch name, used to detect whether trunk is checked out.
func FindWorktreeOnBranch(wts []Worktree, branch string) (Worktree, bool) {
	for _, w := range wts {
		if !w.Detached && w.ShortBranch() == branch {
			return w, true
		}
	}
	return Worktree{}, false
}
