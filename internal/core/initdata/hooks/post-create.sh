#!/usr/bin/env bash
# post_create: runs after a new worktree is created. Turn the bare checkout into a
# runnable environment - carry over gitignored files the repo needs and install
# dependencies.
#
# Env: TREEPI_TASK TREEPI_TYPE TREEPI_BRANCH TREEPI_TRUNK TREEPI_REPO_ROOT
#      TREEPI_MAIN_PATH TREEPI_WORKTREE_PATH TREEPI_BASE_DIR TREEPI_OP
#      TREEPI_CONFIG_PATH TREEPI_VERSION TREEPI_HAS_SUBMODULES
set -euo pipefail

# Example - carry over a gitignored .env from the trunk checkout, install deps:
#   MAIN="${TREEPI_MAIN_PATH:-$TREEPI_REPO_ROOT}"
#   [ -f "$MAIN/.env" ] && cp "$MAIN/.env" "$TREEPI_WORKTREE_PATH/.env"
#   ( cd "$TREEPI_WORKTREE_PATH" && your-install-command )

exit 0
