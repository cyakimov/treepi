#!/usr/bin/env bash
# post_create: runs after a new worktree is created. Turn the bare checkout into a
# runnable, isolated environment - carry over gitignored files, install deps, and
# derive per-worktree ports/DBs from the stable $TREEPI_SLOT.
#
# Env: TREEPI_TASK TREEPI_TYPE TREEPI_BRANCH TREEPI_SLOT TREEPI_TRUNK
#      TREEPI_REPO_ROOT TREEPI_MAIN_PATH TREEPI_WORKTREE_PATH TREEPI_BASE_DIR
#      TREEPI_EXTRAS_FILE  (write NDJSON here, or to fd 3, to report values back)
set -euo pipefail

# Example - derive an isolated port, carry over env, install deps:
#   port=$((5173 + TREEPI_SLOT))
#   [ -f "$TREEPI_MAIN_PATH/.env" ] && cp "$TREEPI_MAIN_PATH/.env" "$TREEPI_WORKTREE_PATH/.env"
#   echo "PORT=$port" >> "$TREEPI_WORKTREE_PATH/.env"
#   ( cd "$TREEPI_WORKTREE_PATH" && your-install-command )
#   printf '{"extras":{"port":%d}}\n' "$port" > "$TREEPI_EXTRAS_FILE"

exit 0
