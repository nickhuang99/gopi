#!/usr/bin/env bash
# D2 disposition evidence — READ-ONLY, reproducible.
#
# What it answers (the epic's deliverable 4): of the merge-backlog family rows
# that are already TERMINAL, which ones name a branch that is provably still
# unmerged (the D2 wrong row), and of the archive-tag population, how much work
# is sitting behind a tag ahead of master.
#
# CONSTRAINTS IT RESPECTS
#   * NO ledger mutation: the sqlite handle is opened mode=ro and every
#     statement is a SELECT — this script cannot write a row even by accident.
#   * NO /tmp: it writes no file at all (stdout only), and mktemp is never used.
#   * NO magic literals: every branch/tag/summary shape it matches is a named
#     constant below, mirroring the production constants it consumes.
set -euo pipefail

# --- single-source mirror of the production constants (Rule 1b/13/14) --------
# These values MUST equal internal/tools/task_merge_backlog*.go; the disposition
# script is evidence, not a second definition, so it prints the production
# value it asks about.
SUMMARY_PREFIX='merge-backlog:'
MERGE_BACKLOG_TAG='merge-backlog'
ARCHIVE_TAG_PREFIX='archive/'
MERGE_TARGET='master'

repo_root=$(git rev-parse --show-toplevel)
db=${GOPI_MEMORY_DB:-$HOME/.gopi/memory.db}
sql() { sqlite3 -cmd '.timeout 8000' "file:${db}?mode=ro" "$@"; }

echo "## 1. Archive-tag population (git, read-only)"
total=0; ahead=0
# no temp files: accumulate in a variable
ahead_list=""
while read -r tag; do
  [ -n "$tag" ] || continue
  total=$((total + 1))
  n=$(git rev-list --count "$MERGE_TARGET..$tag" 2>/dev/null || echo 0)
  if [ "$n" -gt 0 ]; then
    ahead=$((ahead + 1))
    ahead_list="${ahead_list}${n}	${tag}"$'\n'
  fi
done < <(git tag -l "${ARCHIVE_TAG_PREFIX}*")
echo "- archive tags total: **${total}**"
echo "- archive tags still AHEAD of ${MERGE_TARGET} (work kept, nothing merged): **${ahead}**"
echo "- archive tags already contained by ${MERGE_TARGET} (archived after a real merge): **$((total - ahead))**"
echo
echo "Largest dropped work sitting behind an archive tag (commits ahead of ${MERGE_TARGET}):"
printf '%s' "$ahead_list" | sort -rn | head -12 | awk -F'\t' '{printf "    %4d  %s\n", $1, $2}'
echo

echo "## 2. Terminal merge-backlog family rows vs their branch (ledger, read-only)"
# The family test MIRRORS the production one (mergeBacklogFamilyBranch): the
# scan face is the prefix + first field, and the tag face is the exact tag.
# An umbrella row like "merge backlog: land the 5 branches …" (a space, not a
# dash, and no branch ref) is correctly NOT family — it is the epic, not a
# per-branch decision row, and must not be graded as one.
rows=$(sql -separator '|' "SELECT task_id, status, summary, COALESCE(tags,'') FROM tasks
  WHERE (summary LIKE '${SUMMARY_PREFIX}%' OR tags LIKE '%${MERGE_BACKLOG_TAG}%')
    AND status IN ('completed','verified','cancelled')
  ORDER BY status, task_id;")
printf '%-38s %-10s %-9s %s\n' TASK_ID STATUS VERDICT BRANCH
while IFS='|' read -r id status summary tags; do
  [ -n "$id" ] || continue
  branch=""
  if [ "${summary#"$SUMMARY_PREFIX"}" != "$summary" ]; then
    branch=$(printf '%s' "${summary#"$SUMMARY_PREFIX"}" | awk '{print $1}')
  fi
  if [ -z "$branch" ]; then
    case ",$tags," in *",$MERGE_BACKLOG_TAG,"*) branch="<tag-only, no branch named>" ;; esac
  fi
  if [ -z "$branch" ]; then
    printf '%-38s %-10s %-9s %s\n' "$id" "$status" "not-family" "-"
    continue
  fi
  # The same three probes the guard uses, in the same order: branch ref
  # (heads → remotes → plain), then the archive tag (exact, then the
  # slash-flattened loser-archive name form).
  n=""; ref=""
  for cand in "refs/heads/${branch}" "refs/remotes/${branch}" "${branch}"; do
    if git rev-parse --verify -q "$cand" >/dev/null 2>&1; then
      ref=$cand; n=$(git rev-list --count "${MERGE_TARGET}..${ref}"); break
    fi
  done
  if [ -z "$n" ]; then
    flat=${branch//\//-}
    for t in $(git tag -l "${ARCHIVE_TAG_PREFIX}*${flat}*") $(git tag -l "${ARCHIVE_TAG_PREFIX}*${branch}*"); do
      ref=$t; n=$(git rev-list --count "${MERGE_TARGET}..${t}"); break
    done
  fi
  if [ -z "$n" ]; then
    verdict="NO-REF"
  elif [ "$n" -eq 0 ]; then
    verdict="merged"
  else
    verdict="UNMERGED"
  fi
  printf '%-38s %-10s %-9s %s (ahead=%s, %s)\n' "$id" "$status" "$verdict" "$branch" "${n:-?}" "${ref:-no ref/tag}"
done <<< "$rows"
echo
echo "A terminal row is WRONG (the D2 defect) when VERDICT=UNMERGED: the branch is provably"
echo "still ahead of ${MERGE_TARGET}, so the row's status claims a decision nobody made."
echo "A cancelled row with NO-REF is NOT wrong: cancelled is the honest 'not merged' word."
echo

echo "## 3. Fingerprint of the D2 write path (task_events trail, read-only)"
for id in t-1788218212 t-1788218223 t-1788218295; do
  ev=$(sql "SELECT count(*) FROM task_events WHERE task_id='${id}';")
  events=$(sql "SELECT group_concat(DISTINCT event) FROM task_events WHERE task_id='${id}';")
  printf '%-14s task_events=%-3s kinds=%s\n' "$id" "$ev" "${events:-<none>}"
done
echo "(a row written through the LOCAL door always leaves a transition event;"
echo " the D2 row has none — the sync door's raw UPDATE is the fingerprint)"
