//go:build windows

package state

// processAlive is best-effort on Windows: without a cheap pid-existence probe
// we rely on the lease TTL, so we report the owner as alive and never steal a
// lease before it expires.
func processAlive(pid int) bool { return pid > 0 }
