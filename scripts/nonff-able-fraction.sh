#!/usr/bin/env bash
# nonff-able-fraction.sh — measure the cohort property the delivery-point sync
# exists to move: how many of a repo's branches can NOT be fast-forwarded into
# the integration branch (i.e. how many would hand the merger a merge problem).
#
#   usage: scripts/nonff-able-fraction.sh [repo] [integration-ref] [days]
#
#     repo            default: the current directory (must be a git repo)
#     integration-ref default: master (falls back to main when master is absent)
#     days            default: 0 = every local branch; >0 = only branches whose
#                     tip commit is younger than N days (a "newly produced"
#                     window — the cohort the epic's before/after compares)
#
# WHY this is a criterion and not an opinion: "aligned with master" has exactly
# one mechanical reading for a branch the merger must fast-forward —
# `git merge-base --is-ancestor <integration> <branch>`. A branch for which that
# exits 0 is ff-able (nothing to merge); every other branch costs the merger a
# real merge with real conflicts. The script reports BOTH the per-branch verdict
# and the fraction, so a report can quote the measurement rather than a summary.
#
# Rule 22: this script creates no temporary workspace at all (no scratch dir, no
# /tmp spelling) — it only reads git.
set -uo pipefail

repo="${1:-.}"
integration="${2:-}"
days="${3:-0}"

if ! git -C "$repo" rev-parse --git-dir >/dev/null 2>&1; then
  echo "nonff-able-fraction: $repo is not a git repository" >&2
  exit 2
fi

if [ -z "$integration" ]; then
  if git -C "$repo" rev-parse --verify --quiet refs/heads/master >/dev/null; then
    integration=master
  elif git -C "$repo" rev-parse --verify --quiet refs/heads/main >/dev/null; then
    integration=main
  else
    echo "nonff-able-fraction: no integration branch (master/main) in $repo" >&2
    exit 2
  fi
fi

# Archive-ledger tags are not branches; the integration branch itself is never
# a candidate (it cannot be behind itself).
mapfile -t branches < <(git -C "$repo" for-each-ref --format='%(refname:short)' \
  "refs/heads/" | grep -v -x -e "$integration" -e master -e main | grep -v '^archive/')

total=0
ffable=0
behind=0
printf '%-52s %-9s %s\n' "BRANCH" "FF-ABLE" "BEHIND-BYTES"
for branch in "${branches[@]}"; do
  [ -n "$branch" ] || continue
  if [ "$days" != "0" ]; then
    tip_epoch="$(git -C "$repo" log -1 --format=%ct "$branch" 2>/dev/null)"
    [ -n "$tip_epoch" ] || continue
    now_epoch="$(date +%s)"
    if [ $(( now_epoch - tip_epoch )) -gt $(( days * 86400 )) ]; then
      continue
    fi
  fi
  total=$((total + 1))
  if git -C "$repo" merge-base --is-ancestor "$integration" "$branch" 2>/dev/null; then
    ffable=$((ffable + 1))
    printf '%-52s %-9s %s\n' "$branch" "yes" "0"
  else
    behind=$((behind + 1))
    count="$(git -C "$repo" rev-list --count "$branch..$integration" 2>/dev/null)"
    printf '%-52s %-9s %s\n' "$branch" "NO" "${count:-?}"
  fi
done

if [ "$total" -eq 0 ]; then
  echo "nonff-able-fraction: no branch in the measured cohort (repo=$repo ref=$integration days=$days)" >&2
  exit 3
fi

# The fraction is printed as an exact rational AND a percentage: a report that
# quotes 87.5% must be checkable against 7/8.
printf '\ncohort: repo=%s integration=%s days=%s branches=%d\n' "$repo" "$integration" "$days" "$total"
printf 'ff-able: %d  not-ff-able: %d  non-ff-able-fraction: %d/%d (%.1f%%)\n' \
  "$ffable" "$behind" "$behind" "$total" "$(awk -v b="$behind" -v t="$total" 'BEGIN{printf "%.4f", 100*b/t}')"
