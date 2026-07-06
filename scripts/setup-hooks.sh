#!/usr/bin/env bash
#
# One-time setup: point git at the repo's committed hooks (scripts/hooks/) so
# they actually run. Committing a file under scripts/hooks/ does NOT make git
# execute it — git only looks in core.hooksPath (default .git/hooks/, which is
# never checked into version control) for hooks to run. This script wires that
# up for the current clone.
#
# Usage:
#   bash scripts/setup-hooks.sh
#
# Equivalent one-liner, if you'd rather not run the script:
#   git config core.hooksPath scripts/hooks

set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

git config core.hooksPath scripts/hooks

# This repo pins core.fileMode=false (Windows contributors' checkouts don't
# track the POSIX executable bit meaningfully), so scripts/hooks/pre-push may
# land on disk without its executable bit set regardless of what git's index
# records. git invokes core.hooksPath entries by exec'ing them directly (it
# does not run them through `bash`), so the bit must be set locally for the
# hook to fire at all — chmod it explicitly rather than depending on checkout
# behavior.
chmod +x scripts/hooks/pre-push

echo "git hooks path set to scripts/hooks (core.hooksPath)."
echo "scripts/hooks/pre-push is now active: it will run the patch-coverage"
echo "gate before any push that includes Go file changes, and skip fast for"
echo "docs-only pushes. Bypass in an emergency with: git push --no-verify"
