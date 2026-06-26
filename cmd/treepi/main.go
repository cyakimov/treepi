// Command treepi orchestrates git worktrees for humans and autonomous agents.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/cyakimov/treepi/internal/cli"
	"github.com/cyakimov/treepi/internal/exit"
)

// version is overridden via -ldflags at release time.
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The cli layer renders all output (text or --json) and returns the error
	// solely to drive the exit code; nothing is printed here.
	if err := cli.Execute(ctx, version); err != nil {
		os.Exit(int(exit.CodeOf(err)))
	}
}
