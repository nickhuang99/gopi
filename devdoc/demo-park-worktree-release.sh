#!/bin/bash
# Devdoc demo for the park lane's third step (D1) and the park briefing's
# self-verify polarity (D2) — epic merger-park-worktree-release.
#
# Usage: bash devdoc/demo-park-worktree-release.sh <path-to-gopi> <label>
#
# Run it TWICE, against the two binaries, to see the defect and the fix:
#
#   bash devdoc/demo-park-worktree-release.sh <master-bin> before
#   bash devdoc/demo-park-worktree-release.sh <fixed-bin> after
#
# The scenario is the LIVE incident's end state: a branch that is ALREADY parked
# (its archive/<branch> ref is in place, so it is out of every merge queue)
# while a linked worktree still has it checked out. That state is invisible to
# every other arm — the candidate scan drops worktree-protected branches before
# any decision, and the archive exemption hides it from the merge paths — so the
# tick prints NOTHING about it and the directory stays on disk forever.
#
# Rule 22: the workspace lives under GOPI_TMPDIR (the workspace default is
# /home/user/workspace/_tmp, device 66309 — NOT the / partition), and the script
# asserts that before writing anything.
set -u
GOPI_BIN="$1"
LABEL="$2"
D="${GOPI_TMPDIR:-/home/user/workspace/_tmp}/gopi-park-demo-${LABEL}"
SCRATCH_ROOT="${GOPI_TMPDIR:-/home/user/workspace/_tmp}"

# THE FRESH-PATH GUARD (scripts/lib/worktree-fresh.sh — the shell half of
# internal/tools/worktree_fresh_path.go). This script DEMOS the park lane by
# creating a linked worktree by hand, so it is itself a creation site: git
# silently reuses an existing EMPTY directory, and a demo that inherited one
# would show the "branch trapped in a worktree" state in a tree it did not
# create. The `rm -rf "$D"` below is this script's OWN scratch (a path it
# named and owns), which is why the guard comes after it, immediately before
# the add.
# shellcheck source=../scripts/lib/worktree-fresh.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/scripts/lib/worktree-fresh.sh" \
  || { echo "demo: scripts/lib/worktree-fresh.sh missing" >&2; exit 2; }

# Rule 22 criterion, measured: the temp root must NOT be on the / partition.
if [ "$(stat -c %d /)" = "$(stat -c %d "$SCRATCH_ROOT")" ]; then
  echo "REFUSING: temp root $SCRATCH_ROOT is on the / partition" >&2
  exit 2
fi

rm -rf "$D"
mkdir -p "$D/home/.gopi" "$D/repo"
export GOPI_HOME="$D/home"
export GOPI_TMPDIR="$D/scratch"
export GOPI_SETTINGS="$D/home/.gopi/settings.json"
R="$D/repo"
mkdir -p "$GOPI_TMPDIR"

git -C "$R" init -q -b master .
git -C "$R" config user.email demo@demo
git -C "$R" config user.name demo
printf 'base\n' > "$R/shared.txt"
git -C "$R" add -A
git -C "$R" commit -qm seed
git -C "$R" checkout -q -b feat/trapped
printf 'branch side\n' > "$R/shared.txt"
git -C "$R" commit -qam 'branch work'
git -C "$R" checkout -q master
printf 'master side\n' > "$R/shared.txt"
git -C "$R" commit -qam 'master work'

# THE LEGACY PARK's residue: the archive ref (the park's first step) with the
# branch still checked out in a linked worktree — exactly the state the third
# step exists to clear.
git -C "$R" tag -f "archive/feat/trapped" feat/trapped >/dev/null
gopi_assert_worktree_path_fresh "$D/wt" || exit 2
git -C "$R" worktree add -q "$D/wt" feat/trapped

printf '{"repos": {"%s": {"build": ""}}}\n' "$R" > "$GOPI_SETTINGS"

echo "########## $LABEL ##########"
echo "--- BEFORE: git worktree list"
git -C "$R" worktree list
echo "--- BEFORE: archive refs: $(git -C "$R" tag -l 'archive/*' | tr '\n' ' ')"
echo "--- one merger tick:"
"$GOPI_BIN" merger --once --db "$D/home/tasks.db" 2>&1 | grep -E '\[merger\]' | head -12
echo "--- AFTER: git worktree list"
git -C "$R" worktree list
if git -C "$R" rev-parse --verify --quiet refs/heads/feat/trapped >/dev/null; then
  echo "--- AFTER: branch ref PRESERVED (a park is a ref, never a branch -d)"
else
  echo "--- AFTER: branch ref MISSING — WRONG"
fi
echo "--- AFTER: archive tag: $(git -C "$R" tag -l 'archive/*' | tr '\n' ' ')"
echo "--- ledger rows (a drain must file NOTHING new): $(sqlite3 "$D/home/tasks.db" 'select count(*) from tasks;' 2>/dev/null || echo 0)"
