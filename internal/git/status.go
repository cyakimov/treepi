package git

import (
	"bytes"
	"context"
	"strconv"
	"strings"
)

// IsClean reports whether the working tree at dir has no changes (no modified,
// staged, or untracked-non-ignored files). Mirrors the justfile's
// `[ -z "$(git status --porcelain)" ]` guard.
func (c *Client) IsClean(ctx context.Context, dir string) (bool, error) {
	out, err := c.out(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "", nil
}

// AheadBehind reports how many commits HEAD (or rev) is ahead of and behind
// base. It is the data behind the dashboard's ahead/behind column.
func (c *Client) AheadBehind(ctx context.Context, dir, base, rev string) (ahead, behind int, err error) {
	out, err := c.out(ctx, dir, "rev-list", "--left-right", "--count", base+"..."+rev)
	if err != nil {
		return 0, 0, err
	}
	return parseAheadBehind(out)
}

// parseAheadBehind parses the two tab-separated counts from
// `rev-list --left-right --count base...rev`: left = behind, right = ahead.
func parseAheadBehind(s string) (ahead, behind int, err error) {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) != 2 {
		return 0, 0, nil
	}
	behind, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, err
	}
	ahead, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, err
	}
	return ahead, behind, nil
}

// IgnoredPaths lists gitignored files present in the worktree at dir. Used by
// `rm --force` to warn which paths the snapshot will NOT recover.
func (c *Client) IgnoredPaths(ctx context.Context, dir string) ([]string, error) {
	out, err := c.out(ctx, dir, "ls-files", "--others", "--ignored", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	var paths []string
	for _, line := range bytes.Split([]byte(out), []byte("\n")) {
		if p := strings.TrimSpace(string(line)); p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}
