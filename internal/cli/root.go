// Package cli wires the cobra command tree, the pre-parse arg-router (the
// default-subcommand behavior cobra lacks), and the presenter (text or --json).
// It is the only layer that knows cobra/stdout.
package cli

import (
	"context"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cyakimov/treepi/internal/clock"
	"github.com/cyakimov/treepi/internal/config"
	"github.com/cyakimov/treepi/internal/core"
)

var version = "dev"

// Execute builds the root command, applies arg-routing, and runs it.
func Execute(ctx context.Context, v string) error {
	version = v
	root := newRoot()
	known := map[string]bool{"help": true, "completion": true}
	for _, c := range root.Commands() {
		known[c.Name()] = true
		for _, a := range c.Aliases {
			known[a] = true
		}
	}
	root.SetArgs(rewriteArgs(os.Args[1:], known))
	return root.ExecuteContext(ctx)
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "treepi",
		Short:         "Orchestrate git worktrees for humans and agents",
		Version:       version,
		SilenceErrors: true, // the cli renders errors and main maps exit codes
		SilenceUsage:  true,
	}
	root.PersistentFlags().Bool("json", false, "emit machine-readable JSON")
	root.AddCommand(newCmd(), lsCmd(), whereCmd(), shellInitCmd(), syncCmd(), undoCmd())
	return root
}

// rewriteArgs implements the default-subcommand routing cobra lacks: bare `tp`
// opens the dashboard (ls until the TUI lands), and a known branch type as the
// first positional rewrites to `new <type> <task>`.
func rewriteArgs(args []string, known map[string]bool) []string {
	if len(args) == 0 {
		return []string{"ls"} // placeholder for the dashboard (task 8)
	}
	first := args[0]
	if strings.HasPrefix(first, "-") || known[first] {
		return args
	}
	if config.Default().IsType(first) {
		return append([]string{"new"}, args...)
	}
	return args
}

func openService(cmd *cobra.Command) (*core.Service, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return core.Open(cmd.Context(), cwd, config.Default(), clock.Real{}, cmd.ErrOrStderr())
}

func jsonMode(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool("json")
	return v
}
