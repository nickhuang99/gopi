#!/usr/bin/env bash
# build_twins.sh — build the A/B probe pair for the abandoned-worktree decision rule.
#   stale  : both merge sides already ancestors of master (the gopi-combo shape)
#   unique : same shape PLUS a unique non-ancestor commit reachable only there
set -u
# The worktree this probe parks is its EXPERIMENTAL BASE (a detached checkout at
# a revision the probe then runs decisions against), so it is asserted through
# the shared contract rather than assumed — an unverified base tree is what
# produced the 2026-09-19 phantom verdict (GOPI.md Rule 10.3: no trusted base
# without an observation). HERE is resolved BEFORE the cd, so a caller's path
# spelling cannot change which contract file is sourced.
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=../../scripts/lib/baseline-verify.sh
. "$HERE/../../scripts/lib/baseline-verify.sh" || { echo "baseline-verify: $HERE/../../scripts/lib/baseline-verify.sh missing" >&2; exit 2; }
cd "$(dirname "$0")"

mk() { # $1 = twin name ; $2 = 1 to add unique non-ancestor work
  n="$1"; uni="$2"
  rm -rf "$n" "$n-wt"
  git init -q "$n"
  git -C "$n" config user.email v@e.c
  git -C "$n" config user.name verifier
  cd "$n"
  printf 'line-A\n' > f.txt
  git add f.txt
  git commit -qm A
  A=$(git rev-parse HEAD)
  # branch x: modifies f.txt line 1
  git checkout -qb x "$A"
  printf 'line-X\n' > f.txt
  git commit -qam X
  X=$(git rev-parse HEAD)
  # branch y: modifies f.txt line 1 differently (guaranteed conflict)
  git checkout -qb y "$A"
  printf 'line-Y\n' > f.txt
  git commit -qam Y
  Y=$(git rev-parse HEAD)
  # master absorbs BOTH sides
  git checkout -q master 2>/dev/null || git checkout -q -b master "$A"
  git merge -q --no-edit x >/dev/null 2>&1
  if ! git merge -q --no-edit y >/dev/null 2>&1; then
    printf 'line-XY-resolved\n' > f.txt
    git add f.txt
    git commit -qam "merge x+y (conflict resolved)" >/dev/null
  fi
  git merge-base --is-ancestor "$X" master || git merge -q --no-edit "$X" >/dev/null 2>&1
  git merge-base --is-ancestor "$Y" master || git merge -q --no-edit "$Y" >/dev/null 2>&1
  # the abandoned worktree: detached at X (an ancestor), replaying merge of y
  gopi_baseline_worktree_add "../$n-wt" "$X" >/dev/null || exit 2
  cd "../$n-wt"
  git -c user.email=v@e.c -c user.name=verifier merge y >/dev/null 2>&1
  echo "  [$n] merge replay exit=$? ; MERGE_HEAD=$(git rev-parse -q --verify MERGE_HEAD | cut -c1-8) ; UU:"
  git status --porcelain
  if [ "$uni" = "1" ]; then
    # unique work: a commit reachable ONLY from this worktree
    git -c user.email=v@e.c -c user.name=verifier commit -qam "UNIQUE local resolution" >/dev/null 2>&1
    UNIQ=$(git rev-parse HEAD)
    echo "  [$n] unique commit (non-ancestor) = $(git rev-parse --short "$UNIQ")"
    echo "$UNIQ" > ../"$n".unique
  fi
  cd ..
  echo "  [$n] master=$(git -C "$n" rev-parse --short master) wtHEAD=$(git -C "$n-wt" rev-parse --short HEAD)"
}

echo "=== building twins ==="
mk stale 0
mk unique 1
echo "=== done ==="
