# Hooks

A hook is a repo-defined script that treepi runs at a lifecycle moment - after it creates a worktree, before it merges one, before it removes one, and so on.
Hooks are how treepi stays completely agnostic about your project while still setting up a fully working, isolated environment for every worktree.

## Why hooks exist

A freshly-created worktree is a bare git checkout.
It is not runnable yet: it is missing everything that is gitignored (`node_modules`, `.env`, build artifacts, a local database).
Something has to bridge "bare checkout" to "fully set-up dev environment."

That something is either baked into the tool or pushed out to hooks.
If treepi baked it in, it would have to know your repo uses (say) bun + Vite + Postgres - which would defeat the goal of working for any project.
So treepi does the generic part (create the tree, hand the hook paths + context) and the hook does the project-specific part:

```
treepi new feat login-fix
  treepi creates the worktree (bare git checkout)      # treepi's job (generic)
  fires the post_create hook:                          # your repo's job (specific)
        cp ../main/web/.env.local  ./web/.env.local      # carry over gitignored secrets
        createdb "flame_login_fix"                        # set up the database
        (cd web && bun install)                          # install deps
```

treepi never learns what bun, Vite, or Postgres are.
Policy lives in the repo; mechanism lives in treepi.

## Hooks are optional

A repo with no hooks works fine - you just get a bare worktree.
Hooks are how you upgrade from "bare checkout" to "fully-provisioned dev environment."

## Lifecycle events

| Event | Fires | Typical job |
|---|---|---|
| `post_create` | after a worktree is created | the big one: carry over `.env`, install deps, set up the database, init submodules |
| `post_sync` | after a worktree is rebased onto trunk | re-install if the lockfile changed; re-apply migrations |
| `pre_merge` | before merging (on the rebased tip) | the verify gate - run the build/tests before the merge is allowed |
| `post_merge` | after a merge removes the tree | tear down per-worktree resources (drop the scoped DB) |
| `pre_remove` | before a worktree is removed | the same teardown when discarding a tree without merging |

## Where hooks live

Hooks are defined in the committed `.treepi.toml` at the repo root.
They belong in the repo (not in your personal `~/.config/treepi/`) for two reasons: the knowledge is project-specific (a home-level config cannot know one repo uses bun and another uses cargo), and committing them is what makes "clone the repo and the worktree workflow just works" true for every teammate and every agent.

```toml
[hooks.post_create]
command    = ["bash", ".treepi/hooks/post-create.sh"]
timeout    = "5m"
on_failure = "rollback"          # rollback | abort | warn
```

`command` is an argv array (no implicit shell) - a hook that wants a shell calls `["bash", "script.sh"]` explicitly.
On Windows use `["pwsh", "-File", ".treepi/hooks/post-create.ps1"]`.

## What a hook receives

Every hook runs with a `TREEPI_*` environment contract (all paths absolute):

`TREEPI_EVENT`, `TREEPI_TASK`, `TREEPI_TYPE`, `TREEPI_BRANCH`, `TREEPI_TRUNK`, `TREEPI_REPO_ROOT`, `TREEPI_MAIN_PATH` (the trunk worktree, if any), `TREEPI_WORKTREE_PATH` (this tree), `TREEPI_BASE_DIR`, `TREEPI_OP` (op id), `TREEPI_CONFIG_PATH`, `TREEPI_VERSION`, `TREEPI_HAS_SUBMODULES`, plus op-specific values such as `TREEPI_OLD_HEAD` / `TREEPI_NEW_HEAD` on sync and merge.

`TREEPI_TASK` is the stable name you address a worktree by, so it is the natural key for any per-worktree resource a hook sets up (for example `flame_$TREEPI_TASK` for a scoped database).
A hook succeeds or fails - it does not report values back to treepi.

## Worktree setup git does not do

`git worktree add` does NOT initialize submodules, copy sparse-checkout patterns, or smudge LFS files.
If your repo uses any of these, do it in `post_create` (`git submodule update --init`, restore sparse patterns, `git lfs pull`).
treepi warns (via `--json warnings[]` and `TREEPI_HAS_SUBMODULES`) when a fresh tree has uninitialized submodules.

## Failure semantics

Each hook declares `on_failure`:

- `pre_merge` defaults to `abort` - a non-zero exit stops the merge; the tree is left clean and intact, nothing tracked is lost.
- `post_create` defaults to `rollback` - a failure snapshots any partial work, then removes the just-created tree and branch, so a failed setup never leaks a half-created worktree.
- `post_sync`, `post_merge`, and `pre_remove` default to `warn` - the operation has already committed, so the failure is surfaced in `treepi ls` / `--json warnings[]` but not treated as fatal.

A per-hook `timeout` kills the process group if a hook hangs.

## Worked example

A complete `.treepi/hooks/post-create.sh` that makes a fresh worktree runnable:

```bash
#!/usr/bin/env bash
set -euo pipefail

# A fresh worktree is a bare checkout of tracked files only. Carry over the
# gitignored files the repo needs from the trunk checkout so the tree is runnable.
MAIN="${TREEPI_MAIN_PATH:-$TREEPI_REPO_ROOT}"
[ -f "$MAIN/web/.env.local" ] && cp "$MAIN/web/.env.local" "$TREEPI_WORKTREE_PATH/web/.env.local"

# Install frontend deps.
( cd "$TREEPI_WORKTREE_PATH/web" && bun install )
```

If you want this `.env.local` recoverable by `treepi undo` after a `tp rm --force`, add it to `[snapshot].include_ignored` in `.treepi.toml`; otherwise treepi (which honors `.gitignore`) will not snapshot it, and recreating it is this hook's job.

The result: a worktree that is immediately runnable, while the `treepi` binary contains no mention of bun, Vite, or any project-specific tool - the policy lives in your repo's committed hook.
