// Package hooks runs treepi's lifecycle hooks. It resolves the configured
// command for an event and executes it with the TREEPI_* environment contract,
// streaming its output to a warnings writer (keeping the caller's stdout clean
// for --json). A per-hook timeout kills the whole process group. The package
// never decides on_failure policy: it reports a *HookError; the core layer
// applies rollback/abort/warn.
package hooks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/cyakimov/treepi/internal/config"
)

// Context is the full TREEPI_* environment contract handed to every hook (all
// paths absolute). OldHead/NewHead are set only on sync and merge.
type Context struct {
	Event         string
	Task          string
	Type          string
	Branch        string
	Trunk         string
	RepoRoot      string
	MainPath      string
	WorktreePath  string
	BaseDir       string
	Op            string
	ConfigPath    string
	Version       string
	HasSubmodules bool
	OldHead       string
	NewHead       string
}

// Runner executes lifecycle hooks. Construct it with New.
type Runner struct {
	warn io.Writer
}

// New returns a Runner that streams hook stdout/stderr to warn. A nil warn
// discards.
func New(warn io.Writer) *Runner {
	if warn == nil {
		warn = io.Discard
	}
	return &Runner{warn: warn}
}

// HookError reports a hook that failed or timed out. The core layer maps it to
// exit code 9 (hook-abort) and applies the spec's on_failure disposition.
type HookError struct {
	Event    string
	Code     int
	TimedOut bool
	Err      error
}

func (e *HookError) Error() string {
	switch {
	case e.TimedOut:
		return fmt.Sprintf("hook %s timed out", e.Event)
	case e.Code != 0:
		return fmt.Sprintf("hook %s exited with code %d", e.Event, e.Code)
	default:
		return fmt.Sprintf("hook %s failed: %v", e.Event, e.Err)
	}
}

func (e *HookError) Unwrap() error { return e.Err }

// Run executes spec.Command with hc's TREEPI_* environment. It returns a
// *HookError when the hook failed or timed out. An empty command is a no-op.
func (r *Runner) Run(ctx context.Context, spec config.HookSpec, hc Context) error {
	if len(spec.Command) == 0 {
		return nil
	}

	cctx := ctx
	if spec.Timeout > 0 {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(ctx, spec.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(cctx, spec.Command[0], spec.Command[1:]...) //nolint:gosec // argv comes from the repo's own committed config
	cmd.Dir = hc.WorktreePath
	cmd.Stdout = r.warn
	cmd.Stderr = r.warn
	cmd.Env = append(os.Environ(), buildEnv(hc)...)
	setProcGroup(cmd)
	cmd.Cancel = func() error { return killGroup(cmd) }
	cmd.WaitDelay = 2 * time.Second

	if err := cmd.Start(); err != nil {
		return &HookError{Event: hc.Event, Err: err}
	}
	if waitErr := cmd.Wait(); waitErr != nil {
		he := &HookError{Event: hc.Event, Err: waitErr}
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			he.TimedOut = true
		}
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			he.Code = ee.ExitCode()
		}
		return he
	}
	return nil
}

func buildEnv(hc Context) []string {
	env := []string{
		"TREEPI_EVENT=" + hc.Event,
		"TREEPI_TASK=" + hc.Task,
		"TREEPI_TYPE=" + hc.Type,
		"TREEPI_BRANCH=" + hc.Branch,
		"TREEPI_TRUNK=" + hc.Trunk,
		"TREEPI_REPO_ROOT=" + hc.RepoRoot,
		"TREEPI_MAIN_PATH=" + hc.MainPath,
		"TREEPI_WORKTREE_PATH=" + hc.WorktreePath,
		"TREEPI_BASE_DIR=" + hc.BaseDir,
		"TREEPI_OP=" + hc.Op,
		"TREEPI_CONFIG_PATH=" + hc.ConfigPath,
		"TREEPI_VERSION=" + hc.Version,
		"TREEPI_HAS_SUBMODULES=" + boolEnv(hc.HasSubmodules),
	}
	if hc.OldHead != "" {
		env = append(env, "TREEPI_OLD_HEAD="+hc.OldHead)
	}
	if hc.NewHead != "" {
		env = append(env, "TREEPI_NEW_HEAD="+hc.NewHead)
	}
	return env
}

func boolEnv(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
