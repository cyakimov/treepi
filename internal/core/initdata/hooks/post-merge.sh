#!/usr/bin/env bash
# post_merge: runs after a merge removes the worktree. Tear down per-worktree
# resources keyed off $TREEPI_SLOT (drop the scoped DB, free the port). Runs in
# the trunk worktree; failure is a warning (the merge already committed).
set -euo pipefail

exit 0
