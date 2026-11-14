#!/usr/bin/env bash
# preflight.sh — the DELIVERED DECISION RULE, re-implemented by the verifier:
#   an abandoned worktree parked in an in-progress merge may be aborted+removed
#   ONLY IF both merge sides are already ancestors of the trunk (nothing unique).
# usage: preflight.sh <worktree-path> <trunk-ref>
set -u
wt="$1"; trunk="$2"
h=$(git -C "$wt" rev-parse HEAD 2>/dev/null) || { echo "UNSAFE: no HEAD"; exit 1; }
mh=$(git -C "$wt" rev-parse -q --verify MERGE_HEAD 2>/dev/null)
if [ -z "$mh" ]; then echo "INFO: no in-progress merge (MERGE_HEAD absent)"; fi
unsafe=0
dirty=$(git -C "$wt" status --porcelain | grep -c . || true)
if [ -n "$mh" ]; then
  git -C "$wt" merge-base --is-ancestor "$h"  "$trunk" || { echo "UNSAFE: HEAD $h is NOT an ancestor of $trunk"; unsafe=1; }
  git -C "$wt" merge-base --is-ancestor "$mh" "$trunk" || { echo "UNSAFE: MERGE_HEAD $mh is NOT an ancestor of $trunk"; unsafe=1; }
else
  git -C "$wt" merge-base --is-ancestor "$h" "$trunk" || { echo "UNSAFE: no merge in progress and HEAD $h (unique commit) is NOT an ancestor of $trunk"; unsafe=1; }
  if [ "$dirty" -gt 0 ]; then
    echo "UNSAFE: dirty worktree with NO in-progress merge ($dirty entries) — not a stale-merge replay"; unsafe=1
  fi
fi
# dirty residue carrying content neither side ever committed = unique work
while read -r line; do
  [ -z "$line" ] && continue
  st=${line:0:2}; p=${line:3}
  echo "RESIDUE[$st] $p"
done < <(git -C "$wt" status --porcelain | grep -E '^(\?\?|[ MARC][MD])' || true)
if [ "$unsafe" -ne 0 ]; then echo "VERDICT=UNSAFE (refuse: unique work present)"; exit 2; fi
echo "VERDICT=SAFE (both sides ancestors of $trunk — abort+remove loses nothing)"
exit 0
