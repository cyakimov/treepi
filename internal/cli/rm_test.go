package cli

import (
	"context"
	"io"
	"testing"
)

// TestRmRequiresArg checks that `rm` needs at least one task (MinimumNArgs(1)).
// The arg check fails before opening a service, so it needs no git repo.
func TestRmRequiresArg(t *testing.T) {
	root := newRoot()
	root.SetArgs([]string{"rm"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected an error for rm with no task")
	}
}
