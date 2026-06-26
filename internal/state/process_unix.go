//go:build !windows

package state

import (
	"errors"
	"os"
	"syscall"
)

// processAlive reports whether a local process with the given pid exists. It
// sends signal 0 (no signal delivered) and treats EPERM - the process exists
// but is owned by another user - as alive.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
