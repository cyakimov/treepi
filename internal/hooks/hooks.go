// Package hooks runs treepi's lifecycle hooks. It resolves the configured
// command for an event, executes it with the TREEPI_* environment contract,
// streams its output to a warnings writer (keeping the caller's stdout clean for
// --json), and drains the hook's report-back channels - fd 3 on unix, plus the
// TREEPI_EXTRAS_FILE on every OS - after the process exits. A per-hook timeout
// kills the whole process group. The package never decides on_failure policy: it
// reports a *HookError; the core layer applies rollback/abort/warn.
package hooks

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/cyakimov/treepi/internal/config"
)

// defaultDrainTimeout bounds how long Run waits to read the report-back channels
// after the hook exits, so a backgrounded grandchild that inherited fd 3 cannot
// hang treepi. The EXTRAS_FILE channel (a regular file) is unaffected by this.
const defaultDrainTimeout = 2 * time.Second

// Context is the full TREEPI_* environment contract handed to every hook (all
// paths absolute). OldHead/NewHead are set only on sync and merge.
type Context struct {
	Event         string
	Task          string
	Type          string
	Branch        string
	Slot          int
	Trunk         string
	RepoRoot      string
	MainPath      string
	WorktreePath  string
	BaseDir       string
	Op            string
	LeaseOwner    string
	ConfigPath    string
	Version       string
	HasSubmodules bool
	OldHead       string
	NewHead       string
}

// Runner executes lifecycle hooks. Construct it with New.
type Runner struct {
	warn         io.Writer
	drainTimeout time.Duration
}

// New returns a Runner that streams hook stdout/stderr to warn. A nil warn
// discards.
func New(warn io.Writer) *Runner {
	if warn == nil {
		warn = io.Discard
	}
	return &Runner{warn: warn, drainTimeout: defaultDrainTimeout}
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

// Run executes spec.Command with hc's TREEPI_* environment. It returns the
// merged extras the hook reported (may be nil) and a *HookError when the hook
// failed or timed out. An empty command is a no-op. On failure the extras are
// discarded - a half-run setup is not trusted.
func (r *Runner) Run(ctx context.Context, spec config.HookSpec, hc Context) (map[string]any, error) {
	if len(spec.Command) == 0 {
		return nil, nil
	}

	cctx := ctx
	if spec.Timeout > 0 {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(ctx, spec.Timeout)
		defer cancel()
	}

	// The EXTRAS_FILE channel works on every OS and never blocks (a regular file).
	extrasFile, err := os.CreateTemp("", "treepi-extras-*.ndjson")
	if err != nil {
		return nil, &HookError{Event: hc.Event, Err: err}
	}
	extrasPath := extrasFile.Name()
	_ = extrasFile.Close()
	defer func() { _ = os.Remove(extrasPath) }()

	cmd := exec.CommandContext(cctx, spec.Command[0], spec.Command[1:]...) //nolint:gosec // argv comes from the repo's own committed config
	cmd.Dir = hc.WorktreePath
	cmd.Stdout = r.warn
	cmd.Stderr = r.warn
	cmd.Env = append(os.Environ(), buildEnv(hc, extrasPath)...)
	setProcGroup(cmd)
	cmd.Cancel = func() error { return killGroup(cmd) }
	cmd.WaitDelay = 2 * time.Second

	// fd 3 channel (unix only; nil on Windows, where EXTRAS_FILE is the only one).
	pr, pw, perr := newExtraPipe()
	if perr != nil {
		return nil, &HookError{Event: hc.Event, Err: perr}
	}
	if pw != nil {
		cmd.ExtraFiles = []*os.File{pw}
	}

	if err := cmd.Start(); err != nil {
		closeFile(pr)
		closeFile(pw)
		return nil, &HookError{Event: hc.Event, Err: err}
	}
	// Drop the parent's write end so the only remaining fd-3 writers are the child
	// and anything it spawned; this lets the drain reach EOF when they all exit.
	closeFile(pw)

	waitErr := cmd.Wait()

	// Drain both channels AFTER the process exits, with an independent deadline.
	extras := map[string]any{}
	if pr != nil {
		drainPipe(pr, r.drainTimeout, extras)
		closeFile(pr)
	}
	if b, rerr := os.ReadFile(extrasPath); rerr == nil {
		mergeNDJSON(b, extras)
	}
	if len(extras) == 0 {
		extras = nil
	}

	if waitErr != nil {
		he := &HookError{Event: hc.Event, Err: waitErr}
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			he.TimedOut = true
		}
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			he.Code = ee.ExitCode()
		}
		return nil, he
	}
	return extras, nil
}

func drainPipe(pr *os.File, timeout time.Duration, dst map[string]any) {
	_ = pr.SetReadDeadline(time.Now().Add(timeout))
	b, _ := io.ReadAll(pr) // returns at EOF or the read deadline
	mergeNDJSON(b, dst)
}

// mergeNDJSON parses b as newline-delimited JSON and shallow-merges each line's
// "extras" map into dst (later keys win). Torn or unparseable lines are skipped.
func mergeNDJSON(b []byte, dst map[string]any) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var rec struct {
			Extras map[string]any `json:"extras"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		for k, v := range rec.Extras {
			dst[k] = v
		}
	}
}

func buildEnv(hc Context, extrasPath string) []string {
	env := []string{
		"TREEPI_EVENT=" + hc.Event,
		"TREEPI_TASK=" + hc.Task,
		"TREEPI_TYPE=" + hc.Type,
		"TREEPI_BRANCH=" + hc.Branch,
		"TREEPI_SLOT=" + strconv.Itoa(hc.Slot),
		"TREEPI_TRUNK=" + hc.Trunk,
		"TREEPI_REPO_ROOT=" + hc.RepoRoot,
		"TREEPI_MAIN_PATH=" + hc.MainPath,
		"TREEPI_WORKTREE_PATH=" + hc.WorktreePath,
		"TREEPI_BASE_DIR=" + hc.BaseDir,
		"TREEPI_OP=" + hc.Op,
		"TREEPI_LEASE_OWNER=" + hc.LeaseOwner,
		"TREEPI_CONFIG_PATH=" + hc.ConfigPath,
		"TREEPI_VERSION=" + hc.Version,
		"TREEPI_EXTRAS_FILE=" + extrasPath,
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

func closeFile(f *os.File) {
	if f != nil {
		_ = f.Close()
	}
}
