#!/usr/bin/env bash
# post_merge: runs after a merge removes the worktree. Tear down any per-worktree
# resources the post_create hook set up. Runs in the trunk worktree; failure is a
# warning (the merge already committed).
set -euo pipefail

exit 0
