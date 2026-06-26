// Package git is stateless plumbing over the real `git` binary, behind a Runner
// interface so tests can fake command results. It returns OIDs and parsed
// porcelain output; it never reads or writes treepi state.
package git

import (
	"bytes"
	"context"
	"os"
	"os/exec"
)

// Runner executes a single git invocation in dir and returns its output. A
// non-zero exit is reported as a non-nil err (an *exec.ExitError); callers
// classify it against stderr.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) (stdout, stderr []byte, err error)
}

// ExecRunner runs the real git binary. Env entries are appended to the process
// environment (used to pass GIT_AUTHOR_*/GIT_COMMITTER_* for identity-stamped
// snapshots, and a throwaway GIT_INDEX_FILE for the isolated-index capture).
type ExecRunner struct {
	Bin string   // git binary; defaults to "git"
	Env []string // extra environment, appended to os.Environ()
}

// Run implements Runner.
func (r ExecRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, []byte, error) {
	bin := r.Bin
	if bin == "" {
		bin = "git"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	if len(r.Env) > 0 {
		cmd.Env = append(os.Environ(), r.Env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}
