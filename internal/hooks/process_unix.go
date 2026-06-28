//go:build !windows

package hooks

import (
	"errors"
	"os/exec"
	"syscall"
)

// setProcGroup puts the hook in its own process group so killGroup can signal
// the whole subtree on timeout, not just the direct child.
func setProcGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killGroup sends SIGKILL to the hook's process group. An already-exited group
// (ESRCH) is not an error.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}
