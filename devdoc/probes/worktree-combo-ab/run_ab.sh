#!/usr/bin/env bash
# run_ab.sh — run the A/B probe for the abandoned-worktree decision rule.
#   P1 stale twin : preflight says SAFE -> abort+remove -> prove nothing lost
#   P2 unique twin: preflight says UNSAFE -> NOT removed -> prove the commit
#                   it protects really is unreachable from master (i.e. removal
#                   WOULD have lost work) — the safety twin.
set -u
cd "$(dirname "$0")"

echo "=================== P1: STALE twin (should be removably-safe) ==================="
bash preflight.sh stale-wt master; rc=$?; echo "preflight exit=$rc"
if [ "$rc" -eq 0 ]; then
  h=$(git -C stale-wt rev-parse HEAD)
  echo "action: git -C stale-wt merge --abort ; git -C stale worktree remove --force stale-wt"
  git -C stale-wt merge --abort && echo "  abort exit=0"
  git -C stale worktree remove --force "$PWD/stale-wt" && echo "  remove exit=0"
fi
echo "lost-check: commits unreachable from master reachable only via the worktree:"
git -C stale rev-list --all --not master | wc -l
echo "  stale-wt exists? $([ -e stale-wt ] && echo YES || echo NO)"
echo "  .git/worktrees/stale-wt exists? $([ -d stale/.git/worktrees/stale-wt ] && echo YES || echo NO)"
echo "  worktree list:"; git -C stale worktree list
echo "  prune dry-run:"; git -C stale worktree prune -n -v; echo "  (empty above = no dangling entry)"
echo "  repo still sound: $(git -C stale fsck --no-progress 2>&1 | head -3 | tr '\n' ' ')"

echo
echo "=============== P2: UNIQUE twin (safety twin — must be REFUSED) ==============="
uni=$(cat unique.unique)
echo "unique commit = $uni"
echo "is it an ancestor of master? $(git -C unique merge-base --is-ancestor "$uni" master && echo YES || echo NO)"
echo "commits reachable only outside master: $(git -C unique rev-list --all --not master | wc -l)"
bash preflight.sh unique-wt master; rc=$?; echo "preflight exit=$rc"
if [ "$rc" -eq 0 ]; then
  echo "!!! RULE FAILURE: preflight would have removed a worktree with unique work"
else
  echo "action: REFUSED — nothing removed (no abort, no worktree remove)"
fi
echo "  unique-wt still exists? $([ -e unique-wt ] && echo YES || echo NO)"
echo "  .git/worktrees/unique-wt still registered? $([ -d unique/.git/worktrees/unique-wt ] && echo YES || echo NO)"
echo "  worktree HEAD preserved: $(git -C unique-wt rev-parse --short HEAD) (== $uni?)"
echo "  RESIDUE listing from preflight (what the rule saw):"
git -C unique-wt status --porcelain | sed 's/^/    /'
echo "  worktree list:"; git -C unique worktree list
