package hooks

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/cyakimov/treepi/internal/config"
)

// TestHelperProcess is re-executed as the "hook" subprocess. It is inert unless
// TREEPI_TEST_HELPER=1 is set, so the normal `go test` run skips it.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("TREEPI_TEST_HELPER") != "1" {
		return
	}
	switch os.Getenv("HELPER_MODE") {
	case "fail":
		os.Exit(3)
	case "hang":
		time.Sleep(30 * time.Second)
	}
	os.Exit(0)
}

func helperSpec(timeout time.Duration) config.HookSpec {
	return config.HookSpec{
		Command: []string{os.Args[0], "-test.run=^TestHelperProcess$"},
		Timeout: timeout,
	}
}

func runHelper(t *testing.T, mode string, timeout time.Duration, hc Context) error {
	t.Helper()
	t.Setenv("TREEPI_TEST_HELPER", "1")
	t.Setenv("HELPER_MODE", mode)
	if hc.WorktreePath == "" {
		hc.WorktreePath = t.TempDir()
	}
	r := New(&bytes.Buffer{})
	return r.Run(context.Background(), helperSpec(timeout), hc)
}

func TestRunEmptyCommandIsNoop(t *testing.T) {
	r := New(nil)
	if err := r.Run(context.Background(), config.HookSpec{}, Context{Event: "post_create"}); err != nil {
		t.Fatalf("empty command: err=%v", err)
	}
}

func TestRunFailingHook(t *testing.T) {
	err := runHelper(t, "fail", 0, Context{Event: "pre_merge"})
	he, ok := err.(*HookError)
	if !ok {
		t.Fatalf("error = %T %v, want *HookError", err, err)
	}
	if he.Code != 3 || he.TimedOut {
		t.Errorf("HookError = %+v (want Code 3, not timed out)", he)
	}
}

func TestRunTimeoutKillsHook(t *testing.T) {
	start := time.Now()
	err := runHelper(t, "hang", 200*time.Millisecond, Context{Event: "post_create"})
	he, ok := err.(*HookError)
	if !ok {
		t.Fatalf("error = %T %v, want *HookError", err, err)
	}
	if !he.TimedOut {
		t.Errorf("HookError = %+v (want TimedOut)", he)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("timeout took %v; the process group was not killed promptly", elapsed)
	}
}
