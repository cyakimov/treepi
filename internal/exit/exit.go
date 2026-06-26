// Package exit defines treepi's process exit codes and the typed error that
// carries them. It is a leaf package with no internal dependencies: any package
// may return an *exit.Error, and cmd/treepi maps it to an os.Exit code via
// errors.As. The Reason field is a stable, machine-readable string surfaced in
// the --json envelope so agents can branch without parsing prose.
package exit

import (
	"errors"
	"fmt"
)

// Code is a treepi process exit code. The zero value (OK) means success.
type Code int

// The exit-code taxonomy. Agents branch on these without parsing text.
const (
	OK             Code = 0
	Internal       Code = 1  // unexpected internal failure
	Usage          Code = 2  // bad usage or invalid config
	Refused        Code = 3  // a guard refused the operation
	Conflict       Code = 4  // rebase/merge conflict; needs resolution
	NoneAvailable  Code = 5  // claim: no free worktree
	SlotsExhausted Code = 6  // claim: slot range exhausted
	LeaseHeld      Code = 7  // leased by another owner
	VerifyFailed   Code = 8  // merge verify command failed
	HookAbort      Code = 9  // a hook aborted the operation
	NotFound       Code = 10 // unknown task
	LockBusy       Code = 11 // state lock is held (retryable)
	NoTrunk        Code = 12 // no trunk / no remote could be resolved
)

// Error is treepi's typed error. Code is the process exit code; Reason is a
// stable machine code (e.g. "on_trunk", "dirty_trunk"); Msg is for humans.
type Error struct {
	Code   Code
	Op     string // the operation that failed, e.g. "merge"
	Reason string // stable machine code, e.g. "dirty_trunk"
	Msg    string // human-facing message
	Err    error  // wrapped cause, if any
}

func (e *Error) Error() string {
	switch {
	case e.Msg != "" && e.Err != nil:
		return fmt.Sprintf("%s: %v", e.Msg, e.Err)
	case e.Msg != "":
		return e.Msg
	case e.Err != nil:
		return e.Err.Error()
	default:
		return e.Reason
	}
}

func (e *Error) Unwrap() error { return e.Err }

// New builds an *Error with no wrapped cause.
func New(code Code, reason, msg string) *Error {
	return &Error{Code: code, Reason: reason, Msg: msg}
}

// Wrap builds an *Error wrapping cause.
func Wrap(code Code, reason, msg string, cause error) *Error {
	return &Error{Code: code, Reason: reason, Msg: msg, Err: cause}
}

// CodeOf extracts the exit Code from any error: OK for nil, the carried Code for
// an *Error anywhere in the chain, else Internal.
func CodeOf(err error) Code {
	if err == nil {
		return OK
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return Internal
}
