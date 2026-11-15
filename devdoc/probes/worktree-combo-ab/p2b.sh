#!/usr/bin/env bash
# p2b.sh — SHARP safety twin: a worktree parked in a REAL in-progress merge
# (HEAD an ancestor of the trunk, MERGE_HEAD a commit that is NOT), i.e. the
# merge is mid-flight and the other side carries unique work. The rule must
# REFUSE it — this is the case the ancestry check exists for.
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
n=p2b
rm -rf "$n" "$n-wt"
git init -q "$n"
git -C "$n" config user.email v@e.c
git -C "$n" config user.name verifier
cd "$n"
printf 'line-A\n' > f.txt
git add f.txt
git commit -qm A
A=$(git rev-parse HEAD)
git checkout -qb x "$A"
printf 'line-X\n' > f.txt
git commit -qam X
X=$(git rev-parse HEAD)
git checkout -qb z "$A"
printf 'line-Z-UNIQUE\n' > f.txt
git commit -qam "Z unique never merged"
Z=$(git rev-parse HEAD)
git checkout -q master 2>/dev/null || git checkout -q -b master "$A"
git merge -q --no-edit x >/dev/null 2>&1      # master absorbs X only; Z stays unmerged
gopi_baseline_worktree_add "../$n-wt" "$X" >/dev/null || exit 2
cd "../$n-wt"
git -c user.email=v@e.c -c user.name=verifier merge z >/dev/null 2>&1
echo "worktree HEAD   = $(git rev-parse --short HEAD)  (ancestor of master: $(git merge-base --is-ancestor $(git rev-parse HEAD) master && echo YES || echo NO))"
echo "worktree MERGE_HEAD = $(git rev-parse --verify MERGE_HEAD 2>/dev/null | cut -c1-8)  (== z-unique $Z)"
echo "MERGE_HEAD ancestor of master? $(git merge-base --is-ancestor $(git rev-parse MERGE_HEAD) master && echo YES || echo NO)"
echo "status:"; git status --porcelain | sed 's/^/  /'
cd ..
echo "=== preflight (trunk=master) ==="
bash preflight.sh "$n-wt" master
echo "preflight exit=$?"
echo "worktree still present? $([ -e "$n-wt" ] && echo YES || echo NO)"
echo "MERGE_HEAD still reachable from that worktree? $(git -C "$n-wt" rev-parse --verify MERGE_HEAD >/dev/null 2>&1 && echo YES || echo NO)"
echo "unique commit still reachable from any ref? $(git -C "$n" rev-list --all --not master | wc -l) commit(s)"
