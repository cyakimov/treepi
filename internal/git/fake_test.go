package git

import (
	"context"
	"strings"
)

// fakeResult is a scripted response for one git invocation.
type fakeResult struct {
	stdout string
	stderr string
	err    error
}

// fakeRunner records the git invocations it receives and replays scripted
// results keyed by the joined argv. Unmatched calls return def.
type fakeRunner struct {
	calls   [][]string
	dirs    []string
	results map[string]fakeResult
	def     fakeResult
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{results: map[string]fakeResult{}}
}

func (f *fakeRunner) on(args string, r fakeResult) *fakeRunner {
	f.results[args] = r
	return f
}

func (f *fakeRunner) Run(_ context.Context, dir string, args ...string) ([]byte, []byte, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	f.dirs = append(f.dirs, dir)
	if r, ok := f.results[strings.Join(args, " ")]; ok {
		return []byte(r.stdout), []byte(r.stderr), r.err
	}
	return []byte(f.def.stdout), []byte(f.def.stderr), f.def.err
}

func (f *fakeRunner) lastCall() []string {
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}

// fakeExit is an error that reports a process exit code, standing in for
// *exec.ExitError so exit-code classification is unit-testable.
type fakeExit struct{ code int }

func (e fakeExit) Error() string  { return "exit status " + itoa(e.code) }
func (e fakeExit) ExitCode() int  { return e.code }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}
