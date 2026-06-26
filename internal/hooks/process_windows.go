//go:build windows

package hooks

import (
	"os"
	"os/exec"
)

// setProcGroup is a no-op on Windows: there is no POSIX process group. The
// context-driven Cancel (killGroup) plus WaitDelay terminate the direct child;
// this mirrors internal/state's best-effort Windows posture.
func setProcGroup(*exec.Cmd) {}

// killGroup kills the direct child (best-effort).
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// newExtraPipe returns no pipe on Windows: os/exec's ExtraFiles (fd 3) is
// unsupported there, so hooks report values back via TREEPI_EXTRAS_FILE only.
func newExtraPipe() (read, write *os.File, err error) {
	return nil, nil, nil
}
