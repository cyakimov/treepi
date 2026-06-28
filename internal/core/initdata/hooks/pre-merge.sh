#!/usr/bin/env bash
# pre_merge: runs on the rebased tip before the merge into trunk. The verify gate
# - run the build/tests here. A non-zero exit aborts the merge (the branch is
# left rebased, nothing tracked is lost).
set -euo pipefail

# Example:
#   ( cd "$TREEPI_WORKTREE_PATH" && your-build-and-test-command )

exit 0
