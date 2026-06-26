#!/usr/bin/env bash
# post_sync: runs after a worktree is rebased onto trunk. Re-install if the
# lockfile changed, re-apply migrations, etc. TREEPI_OLD_HEAD / TREEPI_NEW_HEAD
# bracket the rebase. Failure is a warning (the rebase already committed).
set -euo pipefail

exit 0
