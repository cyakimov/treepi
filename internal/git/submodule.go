package git

import (
	"context"
	"strings"
)

// UninitializedSubmodules returns the paths of submodules recorded in dir's
// index but not yet checked out. `git submodule status` prefixes such entries
// with '-'. A repo with no submodules returns nil. This exists because
// `git worktree add` does not initialize submodules - a post_create hook must.
func (c *Client) UninitializedSubmodules(ctx context.Context, dir string) ([]string, error) {
	out, err := c.out(ctx, dir, "submodule", "status")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "-") {
			continue
		}
		if fields := strings.Fields(line[1:]); len(fields) >= 2 {
			paths = append(paths, fields[1])
		}
	}
	return paths, nil
}
