// Command treepi orchestrates git worktrees for humans and autonomous agents.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/cyakimov/treepi/internal/exit"
)

// version is overridden via -ldflags at release time.
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := newRootCmd().ExecuteContext(ctx); err != nil {
		var te *exit.Error
		if errors.As(err, &te) {
			if te.Msg != "" || te.Err != nil {
				fmt.Fprintln(os.Stderr, "treepi: "+te.Error())
			}
			os.Exit(int(te.Code))
		}
		fmt.Fprintln(os.Stderr, "treepi: "+err.Error())
		os.Exit(int(exit.Internal))
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "treepi",
		Short:         "Orchestrate git worktrees for humans and agents",
		Version:       version,
		SilenceErrors: true, // we render errors + map exit codes ourselves
		SilenceUsage:  true,
	}
	// Subcommands (new, ls, where, sync, merge, rm, run, claim, release, undo,
	// init, dash) are registered here as they are implemented.
	return root
}
