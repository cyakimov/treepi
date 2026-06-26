package git

import (
	"context"
	"errors"
	"strings"
)

// ErrRefNotFound is returned by ResolveRef when a ref does not resolve.
var ErrRefNotFound = errors.New("ref not found")

// CommonDir returns the absolute path of the shared .git directory (identical
// from every linked worktree). It uses --path-format=absolute because the bare
// --git-common-dir is relative from a subdirectory.
func (c *Client) CommonDir(ctx context.Context, dir string) (string, error) {
	return c.out(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
}

// Toplevel returns the absolute root of the working tree at dir.
func (c *Client) Toplevel(ctx context.Context, dir string) (string, error) {
	return c.out(ctx, dir, "rev-parse", "--show-toplevel")
}

// ResolveRef returns the OID a ref/revision points at, or ErrRefNotFound.
func (c *Client) ResolveRef(ctx context.Context, dir, rev string) (string, error) {
	oid, err := c.out(ctx, dir, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if err != nil {
		var ce *CmdError
		if errors.As(err, &ce) && ce.ExitCode() == 1 {
			return "", ErrRefNotFound
		}
		return "", err
	}
	if oid == "" {
		return "", ErrRefNotFound
	}
	return oid, nil
}

// RemoteHeadBranch returns the default branch a remote's HEAD points at (e.g.
// "main" for origin), or "" if origin/HEAD is unset. Used for trunk autodetect.
func (c *Client) RemoteHeadBranch(ctx context.Context, dir, remote string) (string, error) {
	ref, err := c.out(ctx, dir, "symbolic-ref", "--short", "--quiet", "refs/remotes/"+remote+"/HEAD")
	if err != nil {
		var ce *CmdError
		if errors.As(err, &ce) && ce.ExitCode() == 1 {
			return "", nil // origin/HEAD not set
		}
		return "", err
	}
	return strings.TrimPrefix(ref, remote+"/"), nil
}

// SymbolicHEAD returns the short branch name HEAD points at, or "" if detached.
func (c *Client) SymbolicHEAD(ctx context.Context, dir string) (string, error) {
	name, err := c.out(ctx, dir, "symbolic-ref", "--short", "--quiet", "HEAD")
	if err != nil {
		var ce *CmdError
		if errors.As(err, &ce) && ce.ExitCode() == 1 {
			return "", nil // detached HEAD
		}
		return "", err
	}
	return name, nil
}

// CheckRefFormat reports whether branch is a valid branch name. A nil return
// means valid; otherwise the error explains why (used to refuse bad task names
// before creating a worktree).
func (c *Client) CheckRefFormat(ctx context.Context, branch string) error {
	if strings.HasPrefix(branch, "-") {
		return errors.New("branch name may not start with '-'")
	}
	// check-ref-format runs without a repo; dir is irrelevant.
	_, err := c.out(ctx, "", "check-ref-format", "--branch", branch)
	if err != nil {
		return errors.New("invalid branch name: " + branch)
	}
	return nil
}

// IsAncestor reports whether ancestor is an ancestor of descendant (i.e. a
// fast-forward from ancestor to descendant is possible).
func (c *Client) IsAncestor(ctx context.Context, dir, ancestor, descendant string) (bool, error) {
	_, err := c.out(ctx, dir, "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	var ce *CmdError
	if errors.As(err, &ce) && ce.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

// UpdateRefCAS atomically moves ref from oldOID to newOID, refusing (error) if
// ref no longer points at oldOID. Pass an empty oldOID to create a new ref.
func (c *Client) UpdateRefCAS(ctx context.Context, dir, ref, newOID, oldOID string) error {
	return c.runVoid(ctx, dir, "update-ref", ref, newOID, oldOID)
}

// SetRef force-sets ref to oid (no CAS); used for snapshot/conflict refs that
// treepi exclusively owns.
func (c *Client) SetRef(ctx context.Context, dir, ref, oid string) error {
	return c.runVoid(ctx, dir, "update-ref", ref, oid)
}

// DeleteRef removes ref. Used to GC snapshot/conflict refs.
func (c *Client) DeleteRef(ctx context.Context, dir, ref string) error {
	return c.runVoid(ctx, dir, "update-ref", "-d", ref)
}

// Ref is a (name, oid) pair from for-each-ref.
type Ref struct {
	Name string
	OID  string
}

// ForEachHead returns every local branch ref and its OID.
func (c *Client) ForEachHead(ctx context.Context, dir string) ([]Ref, error) {
	out, err := c.out(ctx, dir, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads")
	if err != nil {
		return nil, err
	}
	var refs []Ref
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, oid, ok := strings.Cut(line, " ")
		if ok {
			refs = append(refs, Ref{Name: name, OID: oid})
		}
	}
	return refs, nil
}

// RestoreWorktreeFrom overlays the file contents of source (a commit/tree) onto
// the worktree at dir - used by undo to restore a captured snapshot's content.
func (c *Client) RestoreWorktreeFrom(ctx context.Context, dir, source string) error {
	return c.runVoid(ctx, dir, "checkout", source, "--", ".")
}

// ResetHard resets the worktree at dir (HEAD, index, and files) to oid. Used by
// undo to reverse a rebase cleanly, moving everything together.
func (c *Client) ResetHard(ctx context.Context, dir, oid string) error {
	return c.runVoid(ctx, dir, "reset", "--hard", oid)
}

// BranchDelete deletes a branch. With force, uses -D; otherwise -d (which
// refuses an unmerged branch). Callers in the no-trunk-worktree path prove
// merged-ness with IsAncestor first, since -d is HEAD-relative.
func (c *Client) BranchDelete(ctx context.Context, dir, branch string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	return c.runVoid(ctx, dir, "branch", flag, branch)
}
