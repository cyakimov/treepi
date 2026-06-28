package git

import (
	"context"
	"fmt"
	"os"
)

// Snapshot captures the working-tree state at dir into a commit parented on
// HEAD and points refName at it. It stages into a throwaway index (so the real
// index and working tree are never touched) and stamps an explicit identity (so
// commit-tree works without a configured user.name/email).
//
// By default it captures tracked + untracked-but-not-ignored state (git add -A
// honors .gitignore). includeIgnored globs are force-added on top, the opt-in
// path for precious gitignored files like .env.local.
//
// created is false and oid empty when the working tree is identical to HEAD, so
// callers skip pinning a redundant snapshot.
func (c *Client) Snapshot(ctx context.Context, dir, refName string, id Identity, includeIgnored []string) (oid string, created bool, err error) {
	head, err := c.ResolveRef(ctx, dir, "HEAD")
	if err != nil {
		return "", false, fmt.Errorf("snapshot: resolve HEAD: %w", err)
	}

	idxPath, err := tempIndexPath()
	if err != nil {
		return "", false, err
	}
	defer func() { _ = os.Remove(idxPath) }()

	env := append([]string{"GIT_INDEX_FILE=" + idxPath}, id.env()...)

	if _, err := c.outEnv(ctx, dir, env, "read-tree", "HEAD"); err != nil {
		return "", false, fmt.Errorf("snapshot: read-tree: %w", err)
	}
	if _, err := c.outEnv(ctx, dir, env, "add", "-A"); err != nil {
		return "", false, fmt.Errorf("snapshot: add: %w", err)
	}
	for _, glob := range includeIgnored {
		if glob == "" {
			continue
		}
		if _, err := c.outEnv(ctx, dir, env, "add", "-f", "--", glob); err != nil {
			return "", false, fmt.Errorf("snapshot: add ignored %q: %w", glob, err)
		}
	}
	tree, err := c.outEnv(ctx, dir, env, "write-tree")
	if err != nil {
		return "", false, fmt.Errorf("snapshot: write-tree: %w", err)
	}

	headTree, err := c.out(ctx, dir, "rev-parse", head+"^{tree}")
	if err != nil {
		return "", false, fmt.Errorf("snapshot: head tree: %w", err)
	}
	if tree == headTree {
		return "", false, nil // no-op: working tree identical to HEAD
	}

	oid, err = c.outEnv(ctx, dir, env, "commit-tree", tree, "-p", head, "-m", "treepi snapshot "+refName)
	if err != nil {
		return "", false, fmt.Errorf("snapshot: commit-tree: %w", err)
	}
	if err := c.SetRef(ctx, dir, refName, oid); err != nil {
		return "", false, fmt.Errorf("snapshot: update-ref: %w", err)
	}
	return oid, true, nil
}

// tempIndexPath returns a unique, non-existent path for a throwaway git index.
// git creates the file on first write; we only need the name to be free.
func tempIndexPath() (string, error) {
	f, err := os.CreateTemp("", "treepi-index-*")
	if err != nil {
		return "", fmt.Errorf("snapshot: temp index: %w", err)
	}
	name := f.Name()
	_ = f.Close()
	if err := os.Remove(name); err != nil {
		return "", fmt.Errorf("snapshot: temp index: %w", err)
	}
	return name, nil
}
