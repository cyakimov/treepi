package hooks

import (
	"bytes"
	"context"
	"os"
	"runtime"
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
	case "extras_file":
		// Report back via the cross-platform file channel; echo two env vars so
		// the parent can assert the TREEPI_* contract reached the child.
		p := os.Getenv("TREEPI_EXTRAS_FILE")
		payload := `{"extras":{"slot":"` + os.Getenv("TREEPI_SLOT") + `","event":"` + os.Getenv("TREEPI_EVENT") + `"}}` + "\n"
		_ = os.WriteFile(p, []byte(payload), 0o644)
	case "extras_fd3":
		if f := os.NewFile(3, "fd3"); f != nil {
			_, _ = f.WriteString(`{"extras":{"port":5173}}` + "\n")
			_ = f.Close()
		}
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

func runHelper(t *testing.T, mode string, timeout time.Duration, hc Context) (map[string]any, error) {
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
	extras, err := r.Run(context.Background(), config.HookSpec{}, Context{Event: "post_create"})
	if err != nil || extras != nil {
		t.Fatalf("empty command: extras=%v err=%v", extras, err)
	}
}

func TestRunExtrasViaFileAndEnvContract(t *testing.T) {
	extras, err := runHelper(t, "extras_file", 0, Context{Event: "post_create", Slot: 7})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if extras["slot"] != "7" {
		t.Errorf("TREEPI_SLOT not seen by hook; extras=%v", extras)
	}
	if extras["event"] != "post_create" {
		t.Errorf("TREEPI_EVENT not seen by hook; extras=%v", extras)
	}
}

func TestRunExtrasViaFd3(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fd 3 (ExtraFiles) is unsupported on Windows; EXTRAS_FILE is the channel there")
	}
	extras, err := runHelper(t, "extras_fd3", 0, Context{Event: "post_create"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, ok := extras["port"]; !ok || got != float64(5173) {
		t.Errorf("fd-3 extras = %v (want port 5173)", extras)
	}
}

func TestRunFailingHook(t *testing.T) {
	_, err := runHelper(t, "fail", 0, Context{Event: "pre_merge"})
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
	_, err := runHelper(t, "hang", 200*time.Millisecond, Context{Event: "post_create"})
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
