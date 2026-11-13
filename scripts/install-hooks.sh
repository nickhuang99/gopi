#!/bin/bash
# install-hooks.sh — sync scripts/git-hooks/* into .git/hooks/ (cp + chmod).
# Kills the version-skew class (.git/hooks drifting from the tracked copy):
# run in the SAME commit cycle that changes a hook.
# (Recommended by devdoc/hook-investigation-report.md — standing risk since v1.)
set -euo pipefail
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
for src in "$REPO"/scripts/git-hooks/*; do
  name="$(basename "$src")"
  case "$name" in *.sample) continue ;; esac
  cp "$src" "$REPO/.git/hooks/$name"
  chmod +x "$REPO/.git/hooks/$name"
  echo "installed: .git/hooks/$name"
done
