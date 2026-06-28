package exit

import (
	"errors"
	"fmt"
	"testing"
)

func TestCodeOf(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Code
	}{
		{"nil is OK", nil, OK},
		{"plain error is Internal", errors.New("boom"), Internal},
		{"typed error carries its code", New(Conflict, "conflict", "rebase conflict"), Conflict},
		{"wrapped typed error is unwrapped", fmt.Errorf("ctx: %w", New(LockBusy, "lock_busy", "held")), LockBusy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CodeOf(tt.err); got != tt.want {
				t.Fatalf("CodeOf() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestErrorMessage(t *testing.T) {
	e := Wrap(VerifyFailed, "verify_failed", "verify failed", errors.New("exit status 1"))
	if got, want := e.Error(), "verify failed: exit status 1"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(e, e.Err) {
		t.Fatalf("expected Unwrap to expose the cause")
	}
}
