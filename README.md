# treepi

A project-agnostic git worktree orchestrator for humans and autonomous agents.

`treepi` (alias `tp`) turns "one worktree per task" into a real workflow: it cuts a worktree from your trunk, runs your setup hooks, keeps a live dashboard, and integrates the work back with a verified fast-forward merge - all with a Jujutsu-inspired safety net (an op-log you can `undo`, an auto-snapshot before anything destructive, and self-healing cleanup after a crashed agent).

> Status: v1 implementation complete (all commands, lifecycle hooks, durable agent leases, the interactive dashboard, and cross-platform packaging), pending its first tagged release.

## Why another worktree tool

The space is crowded (treehouse, gwq, phantom, worktrunk, ...), but nothing combines the three things that matter for parallel humans *and* agents:

1. **Trees live beside the repo, not inside it.** Worktrees land at `<repo>.worktrees/<task>/`, so eslint, tsc, language servers, file watchers, and docker build contexts never recurse into them. You operate by task name and never hand-navigate a path.
2. **First-class, verified merge-back.** `treepi merge` rebases onto the real upstream tip, runs your build/test gate, fast-forwards trunk, and cleans up the branch and tree - resumable and recover-to-main if anything fails.
3. **A safety layer on plain git.** Every mutating operation is journaled with an inverse (`treepi undo`), uncommitted work is snapshotted before any destructive step, and a crashed agent's orphans are reconciled automatically. No virtual filesystem, no magic - just inspectable git underneath.

## Agnostic core, policy in hooks

`treepi` knows nothing about ports, databases, languages, or build tools. A fresh worktree is a bare checkout; your repo's committed `.treepi.toml` lifecycle hooks turn it into a runnable, isolated environment - carrying over gitignored files, installing deps, and deriving per-worktree ports/databases from a stable per-tree slot. See [docs/HOOKS.md](docs/HOOKS.md).

## Agents are first-class

Every command speaks `--json` with a stable envelope and a documented exit-code taxonomy, plus an atomic `claim` that hands an agent a free worktree (and its slot) or fails cleanly. The agent path runs the exact same code as the human path - there is no separate server.

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

Each of these installs the `treepi` binary plus a `tp` alias. For `tp cd <task>` - and bare `tp` to open the dashboard and `cd` into the tree you pick - add the shell wrapper to your rc:

```sh
eval "$(treepi shell-init)"   # bash/zsh; `treepi shell-init fish` for fish
```

## Usage

```sh
treepi init                 # scaffold .treepi.toml + hook stubs (commit them)
treepi new feat login-fix   # cut a worktree on feat/login-fix from trunk
tp                          # open the live dashboard (filter, sync/merge/rm, cd in)
treepi ls                   # static, scriptable table (add --json for agents)
treepi sync login-fix       # rebase the tree onto the latest trunk
treepi merge login-fix      # rebase, verify, fast-forward into trunk, then clean up
treepi rm login-fix         # discard a tree (snapshotted first; undo-able)
treepi undo                 # reverse the last mutating operation
```

For autonomous agents:

```sh
treepi claim --json         # grab a free worktree + slot (or create one), durably leased
treepi renew <task>         # heartbeat the lease (default TTL 2h)
treepi release <task> --rm  # drop the lease and remove the tree
```

Every command accepts `--json` (a stable `{treepi_version, op, ok, data, warnings, error}` envelope) and returns a documented exit code, so an agent branches on the outcome structurally without parsing prose.

## License

Apache-2.0. See [LICENSE](LICENSE).
