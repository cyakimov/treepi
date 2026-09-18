# treepi

Git worktrees done right.

`treepi` (alias `tp`) turns "one worktree per task" into a real workflow: it cuts a worktree from your trunk, runs your setup hooks, and integrates the work back with a verified fast-forward merge - all with a Jujutsu-inspired safety net (an op-log you can `undo`, an auto-snapshot before anything destructive, and reconciliation that flags a crashed-mid-create tree).

> Status: v1 implementation complete (all commands, lifecycle hooks, and cross-platform packaging), pending its first tagged release.

## Why another worktree tool

The space is crowded (treehouse, gwq, phantom, worktrunk, ...), but nothing combines the three things that matter when you run several worktrees in parallel:

1. **Trees live beside the repo, not inside it.** Worktrees land at `<repo>.worktrees/<task>/`, so eslint, tsc, language servers, file watchers, and docker build contexts never recurse into them. You operate by task name and never hand-navigate a path.
2. **First-class, verified merge-back.** `treepi merge` rebases onto the real upstream tip, runs your build/test gate, fast-forwards trunk, and cleans up the branch and tree - resumable and recover-to-main if anything fails.
3. **A safety layer on plain git.** Every mutating operation is journaled with an inverse (`treepi undo`), uncommitted work is snapshotted before any destructive step, and a tree that crashed mid-create is reconciled to an `orphaned` label rather than left in an unknown state. No virtual filesystem, no magic - just inspectable git underneath.

## Agnostic core, policy in hooks

`treepi` knows nothing about ports, databases, languages, or build tools. A fresh worktree is a bare checkout; your repo's committed `.treepi.toml` lifecycle hooks turn it into a runnable environment - carrying over gitignored files, installing deps, and running setup. See [docs/HOOKS.md](docs/HOOKS.md).

## Clean machine-readable output

Every command speaks `--json` with a stable envelope (`{treepi_version, op, ok, data, warnings, error}`) and a documented exit-code taxonomy, so a script branches on the outcome structurally without parsing prose. The same code runs whether you drive it by hand or from automation - there is no separate server.

## Install

**Homebrew** (macOS / Linux):

```sh
brew install cyakimov/tap/treepi
```

**Scoop** (Windows):

```powershell
scoop bucket add cyakimov https://github.com/cyakimov/scoop-bucket
scoop install treepi
```

**Script** (macOS / Linux, checksum-verified):

```sh
curl -fsSL https://raw.githubusercontent.com/cyakimov/treepi/main/install.sh | sh
```

**Go**:

```sh
go install github.com/cyakimov/treepi/cmd/treepi@latest
```

Each of these installs the `treepi` binary plus a `tp` alias. For `tp cd <task>` to change into a tree by name, add the shell wrapper to your rc:

```sh
eval "$(treepi shell-init)"   # bash/zsh; `treepi shell-init fish` for fish
```

## Usage

```sh
treepi init                 # scaffold .treepi.toml + hook stubs (commit them)
treepi new feat login-fix   # cut a worktree on feat/login-fix from trunk
treepi new login-fix        # no type -> branch is just login-fix
tp                          # bare `tp` is just `treepi ls`
treepi ls                   # static, scriptable table (add --json to script it)
tp cd login-fix             # cd into a tree by name (needs the shell wrapper)
treepi sync login-fix       # rebase the tree onto the latest trunk
treepi merge login-fix      # rebase, verify, fast-forward into trunk, then clean up
treepi rm login-fix api-fix # discard one or more trees (snapshotted first; undo-able)
treepi run login-fix -- cmd # run a command in a tree; `run --all` fans out
treepi undo                 # reverse the last mutating operation
```

The branch name comes from `[branch] template` (default `{type}/{task}`): the type is freeform, so `treepi new feat login-fix` gives `feat/login-fix` and `treepi new login-fix` collapses the empty type to just `login-fix`.

Every command accepts `--json` (a stable `{treepi_version, op, ok, data, warnings, error}` envelope) and returns a documented exit code, so a caller branches on the outcome structurally without parsing prose.

`treepi rm` processes each distinct task name in order, so a missing, dirty, locked, or hook-rejected task does not prevent eligible siblings from being removed.
It exits nonzero if any task fails, and one `treepi undo` restores the worktrees changed by the batch.
With `--json`, one task retains the original `{task, branch}` data object; multiple names return `data: {removed: [...], failed: [{task, code, message}]}`.
The batch envelope has `ok: false` and `error.code: "rm_failed"` when any task fails.
Warnings about gitignored files excluded from the snapshot appear on stderr and in `warnings`.

## License

Apache-2.0. See [LICENSE](LICENSE).
