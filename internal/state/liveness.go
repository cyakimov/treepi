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
// only ever shortens the wait below the TTL. A lease with PID <= 0 is a durable,
// process-surviving lease (a claim heartbeat lease): it is never probed, so only
// its TTL frees it - because the claiming process has long since exited.
func DefaultLiveness(host string) LivenessFunc {
	return func(l *Lease, _ time.Time) bool {
		if l.PID <= 0 {
			return true
		}
		if l.Host != host {
			return true
		}
		return processAlive(l.PID)
	}
}
