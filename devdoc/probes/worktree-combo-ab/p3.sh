#!/usr/bin/env bash
# p3.sh — residual-risk probe: a HAND-EDITED (novel) conflicted file inside a
# stale merge is still classified SAFE by an ancestry-only rule, because the
# rule cannot tell a reverse-diff residue from hand-written text.
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
n=p3
rm -rf "$n" "$n-wt"
git init -q "$n"
git -C "$n" config user.email v@e.c
git -C "$n" config user.name verifier
cd "$n"
printf 'line-A\n' > f.txt; git add f.txt; git commit -qm A
A=$(git rev-parse HEAD)
git checkout -qb x "$A"; printf 'line-X\n' > f.txt; git commit -qam X; X=$(git rev-parse HEAD)
git checkout -qb y "$A"; printf 'line-Y\n' > f.txt; git commit -qam Y; Y=$(git rev-parse HEAD)
git checkout -q master 2>/dev/null || git checkout -q -b master "$A"
git merge -q --no-edit x >/dev/null 2>&1
git merge -q --no-edit y >/dev/null 2>&1 || { printf 'line-XY-resolved\n' > f.txt; git add f.txt; git commit -qam "merge x+y (resolved)" >/dev/null; }
git merge-base --is-ancestor "$Y" master || git merge -q --no-edit "$Y" >/dev/null 2>&1
gopi_baseline_worktree_add "../$n-wt" "$X" >/dev/null || exit 2
cd "../$n-wt"
git -c user.email=v@e.c -c user.name=verifier merge y >/dev/null 2>&1
# the crucial twist: the human hand-writes content that NOBODY ever committed
printf 'HAND-WRITTEN NOVEL CONTENT, never committed anywhere\n' > f.txt
echo "worktree status (residue):"
git status --porcelain | sed 's/^/  /'
echo "is the hand-written text in any object? (grep the object store for the string)"
if git rev-list --all --objects | awk '{print $1}' | git cat-file --batch-check='%(objecttype) %(objectname)' 2>/dev/null | awk '$1=="blob"{print $2}' | while read -r b; do git cat-file blob "$b" 2>/dev/null | grep -q 'HAND-WRITTEN NOVEL' && echo "  FOUND in blob $b"; done; then rm -f ..; fi
echo "  (no FOUND line above = the text exists ONLY in the working tree)"
cd ..
echo "--- preflight verdict on this hand-edited stale merge:"
bash preflight.sh "$n-wt" master; echo "exit=$?"
