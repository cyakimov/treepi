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
	"github.com/cyakimov/treepi/internal/exit"
	"github.com/cyakimov/treepi/internal/git"
)

var version = "dev"

// loaded holds the once-resolved layered config. A malformed-config error is
// deferred to openService so commands that never open a service (--help,
// shell-init, completion) still run.
type loadedConfig struct {
	cfg config.Config
	err error
}

var loaded loadedConfig

// Execute builds the root command, resolves the layered config once, applies
// arg-routing (using the repo's own branch types), and runs it.
func Execute(ctx context.Context, v string) error {
	version = v
	loaded = resolveConfig(ctx)
	root := newRoot()
	known := map[string]bool{"help": true, "completion": true}
	for _, c := range root.Commands() {
		known[c.Name()] = true
		for _, a := range c.Aliases {
			known[a] = true
		}
	}
	root.SetArgs(rewriteArgs(os.Args[1:], known, loaded.cfg))
	return root.ExecuteContext(ctx)
}

// resolveConfig loads the layered configuration once. It resolves the repo root
// best-effort ("" when not inside a repo) so the repo-file layers can be read,
// and defers any load error for openService to surface as exit 2.
func resolveConfig(ctx context.Context) loadedConfig {
	cwd, err := os.Getwd()
	if err != nil {
		return loadedConfig{cfg: config.Default(), err: err}
	}
	home, _ := os.UserHomeDir()
	cfg, lerr := config.Load(config.LoadOptions{Root: repoRoot(ctx, cwd), Home: home, Getenv: os.Getenv})
	if lerr != nil {
		return loadedConfig{cfg: config.Default(), err: lerr}
	}
	return loadedConfig{cfg: cfg}
}

// repoRoot returns the absolute toplevel of the repo containing dir, or "" when
// dir is not inside a git worktree.
func repoRoot(ctx context.Context, dir string) string {
	root, err := git.NewClient(git.ExecRunner{}).Toplevel(ctx, dir)
	if err != nil {
		return ""
	}
	return root
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
	root.AddCommand(initCmd(), newCmd(), lsCmd(), whereCmd(), shellInitCmd(),
		syncCmd(), mergeCmd(), rmCmd(), runCmd(), undoCmd(),
		claimCmd(), releaseCmd(), renewCmd(), dashCmd())
	return root
}

// rewriteArgs implements the default-subcommand routing cobra lacks: bare `tp`
// opens the dashboard (ls until the TUI lands), and a known branch type as the
// first positional rewrites to `new <type> <task>`. It consults the resolved
// config's branch types, so a repo-defined type routes correctly.
func rewriteArgs(args []string, known map[string]bool, cfg config.Config) []string {
	if len(args) == 0 {
		return []string{"dash"} // bare `tp` opens the interactive dashboard
	}
	first := args[0]
	if strings.HasPrefix(first, "-") || known[first] {
		return args
	}
	if cfg.IsType(first) {
		return append([]string{"new"}, args...)
	}
	return args
}

func openService(cmd *cobra.Command) (*core.Service, error) {
	if loaded.err != nil {
		return nil, exit.Wrap(exit.Usage, "bad_config", "invalid treepi config", loaded.err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return core.Open(cmd.Context(), cwd, loaded.cfg, clock.Real{}, cmd.ErrOrStderr())
}

func jsonMode(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool("json")
	return v
}
