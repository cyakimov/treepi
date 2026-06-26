package state

import (
	"os"
	"time"
)

func hostName() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unknown"
	}
	return h
}

// DefaultLiveness probes a same-host lease by pid; a cross-host lease is treated
// as live so it is never stolen early (only its TTL can free it). The pid probe
// only ever shortens the wait below the TTL.
func DefaultLiveness(host string) LivenessFunc {
	return func(l *Lease, _ time.Time) bool {
		if l.Host != host {
			return true
		}
		return processAlive(l.PID)
	}
}
