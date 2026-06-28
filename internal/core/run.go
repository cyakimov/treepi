package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/cyakimov/treepi/internal/exit"
	"github.com/cyakimov/treepi/internal/state"
)

// RunResult is the exit code of a command run in one task's worktree.
type RunResult struct {
	Task string `json:"task"`
	Code int    `json:"code"`
}

// Run executes argv in one task's worktree, or in every task's when all is set.
// Child stdout/stderr pass through. It returns a result per task; the first
// non-zero child exit becomes the overall error (so an agent sees it).
func (s *Service) Run(ctx context.Context, task string, all bool, argv []string) ([]RunResult, error) {
	if len(argv) == 0 {
		return nil, exit.New(exit.Usage, "no_command", "no command given (treepi run <task> -- <cmd>)")
	}
	m, err := s.store.View()
	if err != nil {
		return nil, err
	}

	var targets []*state.Task
	if all {
		targets = sortedTasks(m)
	} else {
		t := m.Tasks[task]
		if t == nil {
			return nil, exit.New(exit.NotFound, "not_found", "unknown task: "+task)
		}
		targets = []*state.Task{t}
	}

	var (
		results []RunResult
		failed  *RunResult
	)
	for _, t := range targets {
		if all {
			fmt.Fprintf(s.warn, "=== %s (%s) ===\n", t.Name, t.Path)
		}
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Dir = t.Path
		cmd.Env = os.Environ()
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		code := 0
		if rerr := cmd.Run(); rerr != nil {
			var ee *exec.ExitError
			if errors.As(rerr, &ee) {
				code = ee.ExitCode()
			} else {
				code = 1
			}
		}
		res := RunResult{Task: t.Name, Code: code}
		results = append(results, res)
		if code != 0 && failed == nil {
			r := res
			failed = &r
		}
	}
	if failed != nil {
		return results, exit.New(exit.Internal, "run_failed",
			fmt.Sprintf("command exited %d in task %s", failed.Code, failed.Task))
	}
	return results, nil
}
