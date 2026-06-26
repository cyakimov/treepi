# treepi

A project-agnostic git worktree orchestrator for humans and autonomous agents.

`treepi` (alias `tp`) turns "one worktree per task" into a real workflow: it cuts a worktree from your trunk, runs your setup hooks, keeps a live dashboard, and integrates the work back with a verified fast-forward merge - all with a Jujutsu-inspired safety net (an op-log you can `undo`, an auto-snapshot before anything destructive, and self-healing cleanup after a crashed agent).

> Status: early development. The design is locked; the implementation is in progress.

## Why another worktree tool

The space is crowded (treehouse, gwq, phantom, worktrunk, ...), but nothing combines the three things that matter for parallel humans *and* agents:

1. **Trees live beside the repo, not inside it.** Worktrees land at `<repo>.worktrees/<task>/`, so eslint, tsc, language servers, file watchers, and docker build contexts never recurse into them. You operate by task name and never hand-navigate a path.
2. **First-class, verified merge-back.** `treepi merge` rebases onto the real upstream tip, runs your build/test gate, fast-forwards trunk, and cleans up the branch and tree - resumable and recover-to-main if anything fails.
3. **A safety layer on plain git.** Every mutating operation is journaled with an inverse (`treepi undo`), uncommitted work is snapshotted before any destructive step, and a crashed agent's orphans are reconciled automatically. No virtual filesystem, no magic - just inspectable git underneath.

## Agnostic core, policy in hooks

`treepi` knows nothing about ports, databases, languages, or build tools. A fresh worktree is a bare checkout; your repo's committed `.treepi.toml` lifecycle hooks turn it into a runnable, isolated environment - carrying over gitignored files, installing deps, and deriving per-worktree ports/databases from a stable per-tree slot. See [docs/HOOKS.md](docs/HOOKS.md).

## Agents are first-class

Every command speaks `--json` with a stable envelope and a documented exit-code taxonomy, plus an atomic `claim` that hands an agent a free worktree (and its slot) or fails cleanly. The agent path runs the exact same code as the human path - there is no separate server.

## License

Apache-2.0. See [LICENSE](LICENSE).
