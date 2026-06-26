package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cyakimov/treepi/internal/exit"
)

func newCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "new <type> <task>",
		Short: "Create a worktree on <type>/<task> cut from trunk",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			typ, task := "", args[0]
			if len(args) == 2 {
				typ, task = args[0], args[1]
			}
			svc, err := openService(cmd)
			if err != nil {
				return fail(cmd, "new", err)
			}
			info, err := svc.New(cmd.Context(), typ, task)
			if err != nil {
				return fail(cmd, "new", err)
			}
			if jsonMode(cmd) {
				return emitJSON(cmd.OutOrStdout(), "new", info)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created %s at %s (slot %d)\n", info.Branch, info.Path, info.Slot)
			return nil
		},
	}
}

func lsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Aliases: []string{"status"},
		Short:   "List worktrees",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := openService(cmd)
			if err != nil {
				return fail(cmd, "ls", err)
			}
			tasks, err := svc.List(cmd.Context(), true)
			if err != nil {
				return fail(cmd, "ls", err)
			}
			if jsonMode(cmd) {
				return emitJSON(cmd.OutOrStdout(), "ls", tasks)
			}
			renderTasks(cmd.OutOrStdout(), tasks)
			return nil
		},
	}
}

func whereCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "where <task>",
		Aliases: []string{"cd"},
		Short:   "Print the path of a task's worktree (use `tp cd` with shell-init)",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := openService(cmd)
			if err != nil {
				return fail(cmd, "where", err)
			}
			path, err := svc.Where(cmd.Context(), args[0])
			if err != nil {
				return fail(cmd, "where", err)
			}
			if jsonMode(cmd) {
				return emitJSON(cmd.OutOrStdout(), "where", map[string]string{"path": path})
			}
			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	}
}

func syncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync <task>",
		Short: "Rebase a task's worktree onto the latest trunk",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := openService(cmd)
			if err != nil {
				return fail(cmd, "sync", err)
			}
			info, err := svc.Sync(cmd.Context(), args[0])
			if err != nil {
				return fail(cmd, "sync", err)
			}
			if jsonMode(cmd) {
				return emitJSON(cmd.OutOrStdout(), "sync", info)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "synced %s onto %s\n", info.Task, svc.Repo().Trunk)
			return nil
		},
	}
}

func undoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "undo",
		Short: "Reverse the last mutating operation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := openService(cmd)
			if err != nil {
				return fail(cmd, "undo", err)
			}
			res, err := svc.Undo(cmd.Context())
			if err != nil {
				return fail(cmd, "undo", err)
			}
			if jsonMode(cmd) {
				return emitJSON(cmd.OutOrStdout(), "undo", res)
			}
			if res.Reverted {
				fmt.Fprintf(cmd.OutOrStdout(), "reverted %s (%s)\n", res.Op, res.OpID)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "nothing to undo")
			}
			return nil
		},
	}
}

func shellInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "shell-init [bash|zsh|fish]",
		Short:     "Print a shell function so `tp cd <task>` changes directory",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish"},
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := "bash"
			if len(args) == 1 {
				shell = args[0]
			}
			script, err := shellInitScript(shell)
			if err != nil {
				return fail(cmd, "shell-init", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), script)
			return nil
		},
	}
}

func shellInitScript(shell string) (string, error) {
	switch shell {
	case "bash", "zsh":
		return `tp() {
  if [ "$1" = "cd" ] && [ -n "$2" ]; then
    local d
    d="$(command treepi where "$2")" || return $?
    cd "$d"
  else
    command treepi "$@"
  fi
}`, nil
	case "fish":
		return `function tp
  if test "$argv[1]" = "cd"; and test -n "$argv[2]"
    set -l d (command treepi where "$argv[2]"); or return $status
    cd $d
  else
    command treepi $argv
  end
end`, nil
	default:
		return "", exit.New(exit.Usage, "unknown_shell", "unsupported shell: "+shell+" (bash|zsh|fish)")
	}
}
