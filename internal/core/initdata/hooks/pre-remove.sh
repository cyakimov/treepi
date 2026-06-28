#!/usr/bin/env bash
# pre_remove: runs before a worktree is discarded without merging. The same
# teardown as post_merge for any per-worktree resources. Runs while the tree still
# exists; a non-zero exit with on_failure="abort" keeps the tree.
set -euo pipefail

exit 0
