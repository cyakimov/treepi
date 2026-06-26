package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cyakimov/treepi/internal/core"
	"github.com/cyakimov/treepi/internal/exit"
)

func claimCmd() *cobra.Command {
	var owner, typ string
	var noCreate bool
	c := &cobra.Command{
		Use:   "claim [<task>]",
		Short: "Claim a free worktree (or create one) with a durable lease",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := openService(cmd)
			if err != nil {
				return fail(cmd, "claim", err)
			}
			req := core.ClaimRequest{Owner: owner, Type: typ, NoCreate: noCreate}
			if len(args) == 1 {
				req.Task = args[0]
			}
			info, err := svc.Claim(cmd.Context(), req)
			if err != nil {
				return fail(cmd, "claim", err)
			}
			if jsonMode(cmd) {
				return emitJSONWarn(cmd.OutOrStdout(), "claim", info, svc.Warnings())
			}
			lo := ""
			if info.Lease != nil {
				lo = info.Lease.Owner
			}
			fmt.Fprintf(cmd.OutOrStdout(), "claimed %s at %s (slot %d, owner %s)\n", info.Branch, info.Path, info.Slot, lo)
			return nil
		},
	}
	c.Flags().StringVar(&owner, "owner", "", "lease owner (default $TREEPI_OWNER or user@host)")
	c.Flags().StringVar(&typ, "type", "", "branch type when creating (default from config)")
	c.Flags().BoolVar(&noCreate, "no-create", false, "fail if no free worktree exists instead of creating one")
	return c
}

func releaseCmd() *cobra.Command {
	var force, rm bool
	c := &cobra.Command{
		Use:   "release <task>",
		Short: "Release a task's lease (optionally removing the worktree)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := openService(cmd)
			if err != nil {
				return fail(cmd, "release", err)
			}
			res, err := svc.Release(cmd.Context(), args[0], core.ReleaseOptions{Force: force, Rm: rm})
			if err != nil {
				return fail(cmd, "release", err)
			}
			if jsonMode(cmd) {
				return emitJSON(cmd.OutOrStdout(), "release", res)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "released %s\n", res.Task)
			return nil
		},
	}
	c.Flags().BoolVarP(&force, "force", "f", false, "release a lease held by another owner")
	c.Flags().BoolVar(&rm, "rm", false, "also remove the worktree and branch")
	return c
}

func renewCmd() *cobra.Command {
	var owner string
	c := &cobra.Command{
		Use:   "renew <task>",
		Short: "Extend a claimed task's lease (heartbeat)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := openService(cmd)
			if err != nil {
				return fail(cmd, "renew", err)
			}
			info, err := svc.Renew(cmd.Context(), args[0], owner)
			if err != nil {
				return fail(cmd, "renew", err)
			}
			if jsonMode(cmd) {
				return emitJSON(cmd.OutOrStdout(), "renew", info)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "renewed %s\n", info.Task)
			return nil
		},
	}
	c.Flags().StringVar(&owner, "owner", "", "lease owner (default $TREEPI_OWNER or user@host)")
	return c
}

func initCmd() *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "init",
		Short: "Scaffold .treepi.toml + hook stubs and bootstrap .gitignore",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := openService(cmd)
			if err != nil {
				return fail(cmd, "init", err)
			}
			res, err := svc.Init(cmd.Context(), force)
			if err != nil {
				return fail(cmd, "init", err)
			}
			if jsonMode(cmd) {
				return emitJSON(cmd.OutOrStdout(), "init", res)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "initialized %s (%d file(s))\n", res.ConfigPath, len(res.Created))
			return nil
		},
	}
	c.Flags().BoolVarP(&force, "force", "f", false, "overwrite an existing .treepi.toml and hook stubs")
	return c
}

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
				return emitJSONWarn(cmd.OutOrStdout(), "new", info, svc.Warnings())
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

func mergeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "merge <task>",
		Short: "Rebase, verify, and fast-forward a task into trunk, then clean up",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := openService(cmd)
			if err != nil {
				return fail(cmd, "merge", err)
			}
			res, err := svc.Merge(cmd.Context(), args[0])
			if err != nil {
				return fail(cmd, "merge", err)
			}
			if jsonMode(cmd) {
				return emitJSONWarn(cmd.OutOrStdout(), "merge", res, svc.Warnings())
			}
			fmt.Fprintf(cmd.OutOrStdout(), "merged %s into %s\n", res.Branch, res.Trunk)
			return nil
		},
	}
}

func rmCmd() *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "rm <task>",
		Short: "Remove a task's worktree and branch (snapshotted first)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := openService(cmd)
			if err != nil {
				return fail(cmd, "rm", err)
			}
			res, err := svc.Remove(cmd.Context(), args[0], force)
			if err != nil {
				return fail(cmd, "rm", err)
			}
			if jsonMode(cmd) {
				return emitJSONWarn(cmd.OutOrStdout(), "rm", res, svc.Warnings())
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s (%s)\n", res.Task, res.Branch)
			return nil
		},
	}
	c.Flags().BoolVarP(&force, "force", "f", false, "remove a dirty or leased worktree")
	return c
}

func runCmd() *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:                   "run <task> -- <cmd>...   (or --all -- <cmd>...)",
		Short:                 "Run a command in one or all worktrees",
		DisableFlagsInUseLine: true,
		Args:                  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			task, argv := "", args
			if dash := cmd.ArgsLenAtDash(); dash >= 0 {
				pre := args[:dash]
				argv = args[dash:]
				if !all && len(pre) >= 1 {
					task = pre[0]
				}
			} else if !all && len(args) >= 1 {
				task, argv = args[0], args[1:]
			}
			svc, err := openService(cmd)
			if err != nil {
				return fail(cmd, "run", err)
			}
			res, runErr := svc.Run(cmd.Context(), task, all, argv)
			if jsonMode(cmd) {
				_ = emitJSON(cmd.OutOrStdout(), "run", res)
				return runErr
			}
			if runErr != nil {
				return fail(cmd, "run", runErr)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "run in every worktree")
	return c
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
				return emitJSONWarn(cmd.OutOrStdout(), "sync", info, svc.Warnings())
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
