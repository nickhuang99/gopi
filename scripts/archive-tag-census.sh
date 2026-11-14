#!/usr/bin/env bash
# archive-tag-census.sh — classify every refs/tags/archive/* tag in a repo.
#
# WHY THIS EXISTS (epic merger-remove-park, 2026-09-17).
# The retired park lane minted `archive/<branch>` for every branch that would
# not fast-forward, and `repoSweepArchiveExempt` then made that tag a PERMANENT
# merge exclusion. The lane is gone; the tags it (and other lanes) already
# minted are not, and the operator owns the decision about them — deleting a tag
# is destructive and is NOT done by any code path here. This script produces the
# evidence that decision needs: a machine-readable class per tag, plus the two
# facts that say whether the tag still holds work (is the tagged tip in master?
# how many commits is it ahead?).
#
# THE CLASSES the briefing asks to separate (plus the undecidable rest):
#
#   ① HUMAN / LEDGER ARCHIVE — an ANNOTATED tag (`git cat-file -t` == tag: only
#      a deliberate `git tag -a` makes one, never the merger) or a name in the
#      `hp-unique-*` ledger namespace. The exemption is CORRECT for these and
#      they should stay.
#   ② PARK TAG — the branch has a `resolve merge conflict for <branch> in <repo>`
#      row in the ledger, which is exactly the summary the park lane filed.
#      Sub-split by whether the tagged tip is already an ancestor of master:
#      ②a already-in-master means nothing is held; ②b still-ahead means the
#      tagged commit still carries unmerged work.
#   ③ BUILD-GATE REFUSAL — the branch has a rework row from the merge-time BUILD
#      gate. That lane is DELIBERATELY KEPT (a branch that does not compile must
#      leave the queue, and the gate still mints these tags), so these are NOT
#      park residue: a cleanup that deleted them would delete live machinery's
#      own output.
#   D UNDECIDED — none of the above: no annotated tag, no ledger row naming the
#      branch. These are listed for a HUMAN, never guessed at: the briefing's
#      hard boundary is that an undecidable item is listed with its reason, not
#      deleted.
#
# The ledger is OPTIONAL (a cousin repo may have none): pass --db PATH, or set
# GOPI_MEMORY_DB, or let it default. A missing ledger only moves rows from ②/③
# into D — it never changes ①, which is decided from git alone.
#
# OUTPUT: TSV on stdout, one row per tag — class, subclass, commits-ahead,
# annotated, branch-ref-present, branch name, tag — followed by a counted
# summary on stdout (so `| tee` and `> file` both keep the evidence).
#
# Pure git + optional sqlite3. Writes NOTHING into the repo it inspects and
# creates no temp files (the two ledgers of names live in bash associative
# arrays), so it is safe to run against a foreign checkout.
#
# VERIFIED (2026-09-17): run on /home/user/work/GoPi it reports 159 rows —
# ①2 ②a45 ②b20 ③1 D91; on an empty repo (no tags) it reports "total=0" and exits
# 0 both with and without a ledger, so a zero-tag repo is a readable answer
# rather than a crash.

set -euo pipefail

repo="."
db="${GOPI_MEMORY_DB:-$HOME/.gopi/memory.db}"
while [ $# -gt 0 ]; do
  case "$1" in
    --repo) repo="$2"; shift 2 ;;
    --db)   db="$2";   shift 2 ;;
    -h|--help) sed -n '2,46p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

git_repo=(git -C "$repo")
command -v "${git_repo[0]}" >/dev/null 2>&1 || { echo "git not found" >&2; exit 1; }

# --- the ledger's two name sets -------------------------------------------
have_db=0
if command -v sqlite3 >/dev/null 2>&1 && [ -f "$db" ]; then
  have_db=1
fi

declare -A PARK=() BUILD=()
while IFS= read -r name; do
  [ -n "$name" ] && PARK["$name"]=1
done < <(
  [ "$have_db" = 1 ] || true
  [ "$have_db" = 1 ] && sqlite3 "$db" \
    "SELECT summary FROM tasks WHERE summary LIKE 'resolve merge conflict for %';" |
    sed -E 's/^resolve merge conflict for (.*) in \/.*$/\1/' || true
)
while IFS= read -r name; do
  [ -n "$name" ] && BUILD["$name"]=1
done < <(
  [ "$have_db" = 1 ] || true
  [ "$have_db" = 1 ] && sqlite3 "$db" \
    "SELECT summary FROM tasks WHERE summary LIKE '%rework:%';" |
    sed -E 's/.*rework: ([^ ]+).*/\1/' || true
)

# --- classify --------------------------------------------------------------
printf 'class\tsubclass\tahead\tannotated\tref_present\tname\ttag\n'
declare -A COUNTS=()

while IFS= read -r tag; do
  [ -n "$tag" ] || continue
  name="${tag#archive/}"
  obj="$(git -C "$repo" cat-file -t "$tag" 2>/dev/null || echo unknown)"
  annotated=no; [ "$obj" = "tag" ] && annotated=yes
  ahead="$(git -C "$repo" rev-list --count "master..$tag" 2>/dev/null || echo '?')"
  if git -C "$repo" merge-base --is-ancestor "$tag" master 2>/dev/null; then
    anc=in-master
  else
    anc=ahead-of-master
  fi
  if git -C "$repo" rev-parse --verify --quiet "refs/heads/$name" >/dev/null; then
    ref_present=yes
  else
    ref_present=no
  fi

  cls="D"; sub="undecided-no-ledger-row"
  case "$name" in
    hp-unique-*) cls="①"; sub="hp-unique-ledger" ;;
    *)
      if [ "$annotated" = yes ]; then
        cls="①"; sub="human-annotated"
      elif [ -n "${PARK[$name]:-}" ]; then
        cls="②"
        if [ "$anc" = in-master ]; then sub="park-tag-already-in-master"; else sub="park-tag-still-ahead"; fi
      elif [ -n "${BUILD[$name]:-}" ]; then
        cls="③"; sub="build-gate-refusal"
      fi
      ;;
  esac
  COUNTS["$cls $sub"]=$(( ${COUNTS["$cls $sub"]:-0} + 1 ))
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$cls" "$sub" "$ahead" "$annotated" "$ref_present" "$name" "$tag"
done < <(git -C "$repo" tag -l 'archive/*' | sort)

# --- counted summary -------------------------------------------------------
echo
echo "== class counts (ledger: $([ "$have_db" = 1 ] && echo "$db" || echo 'NONE — ②/③ fall into D')) =="
{
  for k in "${!COUNTS[@]}"; do printf '%6d  %s\n' "${COUNTS[$k]}" "$k"; done
} | sort -k2
total=0; for v in "${COUNTS[@]}"; do total=$(( total + v )); done
printf '\ntotal=%d  park-still-ahead=%d  park-already-in-master=%d\n' \
  "$total" "${COUNTS["② park-tag-still-ahead"]:-0}" "${COUNTS["② park-tag-already-in-master"]:-0}"
