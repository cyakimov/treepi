package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Client is the typed entry point to git plumbing. It is stateless: every
// method shells out through the Runner and returns OIDs or parsed porcelain.
type Client struct {
	run Runner
}

// NewClient wraps a Runner. Pass an ExecRunner in production or a fake in tests.
func NewClient(r Runner) *Client { return &Client{run: r} }

// Identity is the author/committer used for treepi's internal commits (the
// isolated-index snapshots). It is passed explicitly so commit-tree works even
// when the repo or environment has no configured user.name/user.email.
type Identity struct {
	Name  string
	Email string
}

// DefaultIdentity is used when a snapshot caller supplies none.
var DefaultIdentity = Identity{Name: "treepi", Email: "treepi@localhost"}

func (id Identity) env() []string {
	n, e := id.Name, id.Email
	if n == "" {
		n = DefaultIdentity.Name
	}
	if e == "" {
		e = DefaultIdentity.Email
	}
	return []string{
		"GIT_AUTHOR_NAME=" + n, "GIT_AUTHOR_EMAIL=" + e,
		"GIT_COMMITTER_NAME=" + n, "GIT_COMMITTER_EMAIL=" + e,
	}
}

// CmdError is a failed git invocation; it carries the stderr and the process
// exit code so callers can classify (e.g. a rebase conflict) without re-running.
type CmdError struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *CmdError) Error() string {
	s := strings.TrimSpace(e.Stderr)
	if s == "" {
		return fmt.Sprintf("git %s: %v", strings.Join(e.Args, " "), e.Err)
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), s)
}

func (e *CmdError) Unwrap() error { return e.Err }

// ExitCode reports the git process exit code, or -1 if it did not run/exit.
// It matches anything that reports an exit code (*exec.ExitError in production,
// a fake in tests).
func (e *CmdError) ExitCode() int {
	var ec interface{ ExitCode() int }
	if errors.As(e.Err, &ec) {
		return ec.ExitCode()
	}
	return -1
}

// out runs git and returns trimmed stdout, or a *CmdError.
func (c *Client) out(ctx context.Context, dir string, args ...string) (string, error) {
	stdout, stderr, err := c.run.Run(ctx, dir, args...)
	if err != nil {
		return "", &CmdError{Args: args, Stderr: string(stderr), Err: err}
	}
	return strings.TrimSpace(string(stdout)), nil
}

// outEnv is like out but with extra environment (identity, GIT_INDEX_FILE). It
// requires an *ExecRunner; a fake Runner ignores Env, which is fine for tests
// that assert on args rather than real git behavior.
func (c *Client) outEnv(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	r := c.run
	if er, ok := r.(ExecRunner); ok {
		er.Env = append(append([]string{}, er.Env...), env...)
		r = er
	}
	stdout, stderr, err := r.Run(ctx, dir, args...)
	if err != nil {
		return "", &CmdError{Args: args, Stderr: string(stderr), Err: err}
	}
	return strings.TrimSpace(string(stdout)), nil
}

// run executes git for side effects, discarding stdout.
func (c *Client) runVoid(ctx context.Context, dir string, args ...string) error {
	_, err := c.out(ctx, dir, args...)
	return err
}
