# treepi 🌱

A small tool I built for my Git worktree workflow, shared in case it fits yours too.

`treepi` helps me keep a worktree per task: create one, set it up with project hooks, and merge the work back when it's ready.
It runs on macOS, Linux, and Windows, with a `tp` shell shortcut for everyday use.

A few things it does:

- **A little room to grow.** Worktrees live at `<repo>.worktrees/<task>/`, beside the repository, so tools scanning your checkout don't wander into other tasks.
- **A route back to trunk.** Merge rebases onto the latest upstream tip, runs any configured checks, fast-forwards trunk, and removes the task's worktree and branch.
- **A way to retrace your steps.** Operations are journaled for `undo`, with snapshots before destructive steps and reconciliation that flags interrupted worktree creation as `orphaned`.

## Install

### Homebrew · macOS / Linux

```sh
brew install cyakimov/tap/treepi
```

### Install script · macOS / Linux

The script downloads a release, verifies its SHA-256 checksum, and installs `treepi` plus a `tp` symlink.
The default destination is `/usr/local/bin`; set `TREEPI_INSTALL_DIR` to choose another directory.

```sh
curl -fsSL https://raw.githubusercontent.com/cyakimov/treepi/main/install.sh | sh
```

### Go · any supported platform

With Go installed:

```sh
go install github.com/cyakimov/treepi/cmd/treepi@latest
```

This installs the `treepi` binary; the shell setup below provides `tp`.
Make sure Go's binary directory is on your `PATH`.

### Release archives · including Windows

Download the archive for your OS and architecture from [GitHub Releases](https://github.com/cyakimov/treepi/releases), extract it, and place the binary on your `PATH`.

### Give your shell a shortcut 🐚

For bash or zsh, add this to your shell configuration (`~/.bashrc` or `~/.zshrc`):

```sh
eval "$(treepi shell-init)"
```

For fish, add this to `~/.config/fish/config.fish`:

```fish
treepi shell-init fish | source
```

Reload your shell configuration or open a new terminal.
The `tp` function forwards commands to `treepi`, lists worktrees when called without arguments, and makes `tp cd <task>` change your current directory.
The directory shortcut supports bash, zsh, and fish; in other shells, use `treepi where <task>` to find the path.

## Plant your first worktree 🌱

Start in an existing Git repository, with the shell shortcut loaded:

```sh
treepi init                       # scaffold project config and hook stubs
```

Review `.treepi.toml` and `.treepi/hooks/`, customize them for your project, and commit them before creating a task.
The generated setup hook uses bash; on Windows, use an available bash installation or configure a PowerShell hook instead.

```sh
git add .treepi.toml .treepi/hooks .gitignore
git commit -m "Configure treepi"

tp new feat login-fix             # create branch feat/login-fix and its worktree
tp cd login-fix                   # enter the worktree

# Make your changes, then commit them.
git add <files-you-changed>
git commit -m "Fix login"

cd -                             # return to the original repository
tp merge login-fix               # rebase, run configured checks, merge, clean up
```

Commit your task's changes before merging, and keep the trunk worktree clean too.
Run merge from outside the task's worktree, since a successful merge removes it.

The type is optional: `tp new login-fix` creates branch `login-fix`.
Branch names follow `[branch].template`, which defaults to `{type}/{task}`.

## What's in the toolbox?

| Command | What it does |
| --- | --- |
| `tp` or `tp ls` | List worktrees and their status. |
| `tp new feat login-fix` | Create a task worktree from trunk. |
| `tp cd login-fix` | Enter a worktree using the shell function. |
| `tp where login-fix` | Print a worktree's path. |
| `tp sync login-fix` | Rebase a task onto the latest trunk. |
| `tp merge login-fix` | Rebase, run configured checks, fast-forward trunk, and clean up. |
| `tp rm login-fix api-fix` | Remove one or more worktrees and their branches. |
| `tp rm --force login-fix` | Remove a dirty worktree, taking a snapshot first. |
| `tp run login-fix -- git status` | Run a command in one worktree. |
| `tp run --all -- git status` | Run a command in every task worktree. |
| `tp undo` | Reverse the last journaled mutating operation. |

Use `treepi <command> --help` for command options.
All `tp` examples above also work with `treepi`, except that changing your shell's directory requires the `tp` function.

## Make it feel at home

Project configuration lives in `.treepi.toml`.
Treepi handles worktrees; your hooks handle dependencies, environment files, databases, and other project setup.
See the [hooks guide](docs/HOOKS.md) for lifecycle events, examples, and failure behavior.

### Check before merging

Build/test verification is opt-in; the generated configuration leaves it disabled.
For example, in a Go project:

```toml
[merge]
verify = ["go", "test", "./..."]
```

The command runs in the task worktree after rebasing and before trunk advances.
If it fails, the merge stops and the task branch stays rebased for you to inspect and fix.
For more involved checks, configure a `pre_merge` hook.
Commands are argument arrays, with no implicit shell.

### Keep ignored files in mind

Snapshots honor `.gitignore` by default.
If you want an ignored file restored by `undo` after removal, include it explicitly:

```toml
[snapshot]
include_ignored = [".env", ".env.local"]
```

Otherwise, copying or recreating ignored files is your setup hook's job.

## For scripts and other helpers 🤖

Task commands support `--json`, so callers can inspect structured results:

```sh
treepi ls --json
treepi rm login-fix api-fix --json
```

The envelope uses `treepi_version`, `op`, and `ok`, plus `data`, `warnings`, and `error` when applicable.
Errors include a machine-readable `code` and a human-readable `message`.
See the [exit-code definitions](internal/exit/exit.go) for process exit codes.

Batch removal processes each distinct task in order and continues if a task is missing, dirty, locked, or rejected by a hook.
It exits nonzero if any task fails; one `treepi undo` restores the worktrees changed by that batch.

For a successful single-task removal, `data` is `{task, branch}`.
With multiple names, it is `{removed: [...], failed: [{task, code, message}]}`.
A partially failed batch sets `ok: false` and `error.code: "rm_failed"`.
Warnings about ignored files excluded from snapshots appear on stderr and in `warnings`.

## License

[Apache-2.0](LICENSE).
