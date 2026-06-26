# Hooks

A hook is a repo-defined script that treepi runs at a lifecycle moment - after it creates a worktree, before it merges one, before it removes one, and so on.
Hooks are how treepi stays completely agnostic about your project while still setting up a fully working, isolated environment for every worktree.

## Why hooks exist

A freshly-created worktree is a bare git checkout.
It is not runnable yet: it is missing everything that is gitignored (`node_modules`, `.env`, build artifacts, a local database), and if you started its dev server it would collide with your other worktrees on ports and databases.
Something has to bridge "bare checkout" to "fully set-up, isolated dev environment."

That something is either baked into the tool or pushed out to hooks.
If treepi baked it in, it would have to know your repo uses (say) bun + Vite + Postgres - which would defeat the goal of working for any project.
So treepi does the generic part (create the tree, assign a stable slot number, hand the hook paths + context) and the hook does the project-specific part:

```
treepi new feat login-fix
  hooks create the worktree (bare git checkout)        # treepi's job (generic)
  fires the post_create hook:                          # your repo's job (specific)
        cp ../main/web/.env.local  ./web/.env.local      # carry over gitignored secrets
        echo "VITE_PORT=$((5173 + TREEPI_SLOT))" >> ...   # isolate ports off the slot
        createdb "flame_slot$TREEPI_SLOT"                 # isolate the database
        (cd web && bun install)                          # install deps
```

treepi never learns what bun, Vite, or Postgres are.
Policy lives in the repo; mechanism lives in treepi.

## Hooks are optional

A repo with no hooks works fine - you just get a bare worktree.
Hooks are how you upgrade from "bare checkout" to "fully-provisioned, isolated dev environment."

## Lifecycle events

| Event | Fires | Typical job |
|---|---|---|
| `post_create` | after a worktree is created | the big one: carry over `.env`, install deps, derive per-worktree ports/DB from the slot, init submodules |
| `post_sync` | after a worktree is rebased onto trunk | re-install if the lockfile changed; re-apply migrations |
| `pre_merge` | before merging (on the rebased tip) | the verify gate - run the build/tests before the merge is allowed |
| `post_merge` | after a merge removes the tree | tear down per-worktree resources (drop the scoped DB, free the port) |
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

`TREEPI_EVENT`, `TREEPI_TASK`, `TREEPI_TYPE`, `TREEPI_BRANCH`, `TREEPI_SLOT`, `TREEPI_TRUNK`, `TREEPI_REPO_ROOT`, `TREEPI_MAIN_PATH` (the trunk worktree, if any), `TREEPI_WORKTREE_PATH` (this tree), `TREEPI_BASE_DIR`, `TREEPI_OP` (op id), `TREEPI_LEASE_OWNER`, `TREEPI_CONFIG_PATH`, `TREEPI_VERSION`, `TREEPI_EXTRAS_FILE` (write derived values here, an alternative to fd 3), `TREEPI_HAS_SUBMODULES`, plus op-specific values such as `TREEPI_OLD_HEAD` / `TREEPI_NEW_HEAD` on sync and merge.

The `TREEPI_SLOT` is a stable integer unique to each live worktree **within this repo**.
It is the key to deterministic isolation: derive `5173 + TREEPI_SLOT` for a port, `flame_slot$TREEPI_SLOT` for a database, and every worktree in this repo gets its own without any coordination.
(Slots are per-repo, so two different repos can both land on slot 0; derive a per-repo offset if you run dev servers across repos.)

## Reporting values back

A hook reports derived values into the worktree's state, shown in `treepi ls`, by writing NDJSON to **fd 3** or to the file named by `TREEPI_EXTRAS_FILE` (use the file on Windows / PowerShell):

```bash
printf '{"extras":{"ports":{"vite":%d},"db":"flame_slot%d"}}\n' "$vite" "$TREEPI_SLOT" >&3
```

treepi reads this channel after the hook **process exits**, so a hook must not background a long-running process that keeps fd 3 open (point such processes' fd 3 elsewhere, or use `TREEPI_EXTRAS_FILE`).

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

A complete `.treepi/hooks/post-create.sh` that gives the flame/hypervisor repo fully isolated parallel worktrees:

```bash
#!/usr/bin/env bash
set -euo pipefail

# Derive isolated ports from the stable slot - treepi knows none of these numbers.
vite=$((5173 + TREEPI_SLOT)); story=$((6006 + TREEPI_SLOT)); ctrl=$((8443 + TREEPI_SLOT))

# Carry over the gitignored env from the main checkout so the tree is runnable.
[ -f "$TREEPI_MAIN_PATH/web/.env.local" ] && cp "$TREEPI_MAIN_PATH/web/.env.local" "$TREEPI_WORKTREE_PATH/web/.env.local"
{ echo "VITE_PORT=$vite"; echo "STORYBOOK_PORT=$story"; echo "CONTROL_PORT=$ctrl"; } >> "$TREEPI_WORKTREE_PATH/web/.env.local"

# Scope a per-slot database; isolate the Go build cache.
createdb "flame_slot$TREEPI_SLOT" 2>/dev/null || true
export GOCACHE="$TREEPI_WORKTREE_PATH/.gocache"

# Install frontend deps.
( cd "$TREEPI_WORKTREE_PATH/web" && bun install )

# Report derived values back into treepi's state - shows in `treepi ls`.
printf '{"extras":{"ports":{"vite":%d,"storybook":%d,"control":%d},"db":"flame_slot%d"}}\n' \
  "$vite" "$story" "$ctrl" "$TREEPI_SLOT" >&3
```

If you want this `.env.local` recoverable by `treepi undo` after a `tp rm --force`, add it to `[snapshot].include_ignored` in `.treepi.toml`; otherwise treepi (which honors `.gitignore`) will not snapshot it, and recreating it is this hook's job.

The result: N parallel agents each run a full dev stack with zero port or database collisions (within the repo), while the `treepi` binary contains no mention of bun, Vite, Storybook, Postgres, or any port number.
