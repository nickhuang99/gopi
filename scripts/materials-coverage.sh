#!/usr/bin/env bash
# materials-coverage.sh — the COVERAGE CENSUS of docs/archive/demo/materials/ (M0 skeleton).
#
# GENERATOR: scripts/materials-coverage.sh
# BASELINE:  193 material entries, taken from the epic briefing (M0) §4, which
#            breaks it down as 36 top-level commands + 17 task subcommands +
#            16 memory subcommands + 4 sandbox subcommands + 3 checkpoint
#            subcommands + 27 global flags + 90 environment variables = 193.
#            THAT BASELINE IS NOT IN THIS REPO — it is quoted, not measured, and
#            it is labelled as such in the TSV header (`baseline_source`). The
#            measured counterpart, per face, at the commit named by
#            `measured_commit`, lives in docs/archive/demo/materials/README.md §6 with
#            the exact command that produced each number. Two numbers of
#            different origin are never silently substituted for one another
#            (GOPI.md Rule 38).
#
# WHAT IT ANSWERS: of the 193 surfaces the baseline counts, how many have a
#            COMPLETE material entry in the tree — and, for every entry that is
#            not complete, WHICH of the six conditions it fails.
#
# RUN IT:
#   bash scripts/materials-coverage.sh                 # human summary, stdout only
#   bash scripts/materials-coverage.sh --tsv           # TSV to stdout
#   bash scripts/materials-coverage.sh --only finding  # narrow the detail lines
#   bash scripts/materials-coverage.sh --out /tmp/x.tsv
#
# READ-ONLY CONTRACT (hard): this script performs READ operations only on the
#   materials tree. It never writes, renames, chmods or deletes anything there —
#   not even a temporary file. It asserts that itself: the tree's content
#   listing is hashed before and after the scan, and a difference is an
#   ASSERT FAIL with a non-zero exit. See README.md §8.
#
# ZERO DEPENDENCY (GOPI.md Rule 35): bash + coreutils + git. No cron, no daemon,
#   no state file, no new database. Nothing persists after this process exits.
#
# SCRATCH (Rule 22): the one private working directory this script needs comes
#   from the repo's own shared helper (scripts/lib/tmp-ban.sh) — never a
#   hand-rolled `mktemp`. The helper asserts the path is provably OFF the `/`
#   partition and registers it for trap-time removal, which is why the census
#   cannot leave residue behind on the boot device (the 2026-09-11 incident).
#
# ISOLATION: nothing here reads or writes the operator's gopi profile — this is
#   a filesystem census, not a ledger reader.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
. "$ROOT/scripts/lib/tmp-ban.sh" || { echo "materials-coverage: scripts/lib/tmp-ban.sh missing" >&2; exit 2; }

# --- named constants (GOPI.md Rule 1: no bare literals for limits/names) -----
GENERATOR="scripts/materials-coverage.sh"
GENERATOR_VERSION=1
BASELINE=193
BASELINE_SOURCE="epic briefing (M0) §4 — 36+17+16+4+3+27+90 (see docs/archive/demo/materials/README.md §6)"
DEFAULT_TREE_REL="docs/archive/demo/materials"
CARD="00-entry.md"
INPUT="01-input.txt"
OUTPUT="02-output.txt"
RAW="03-raw.png"
ANNOTATED="04-annotated.png"
FINDING="05-finding.md"
# The six conditions, in report order. `complete` is their conjunction.
CONDITIONS=(card input output raw annotated finding)
EXIT_MARK="exit="
PNG_MAGIC="89504e470d0a1a0a"
NAME_RE='^[0-9]{3}-[a-z0-9][a-z0-9-]*$'
ANN_YES="yes"
ANN_NO="no"
RC_OK=0
RC_ASSERT=1
RC_REFUSED=2

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TREE="${GOPI_MATERIALS_DIR:-$ROOT/$DEFAULT_TREE_REL}"
MODE="summary"     # summary | tsv
ONLY=""
OUT=""

usage() { sed -n '2,40p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tree)   TREE="${2:-}"; shift 2 ;;
    --only)   ONLY="${2:-}"; shift 2 ;;
    --tsv)    MODE="tsv";    shift ;;
    --out)    OUT="${2:-}";  shift 2 ;;
    -h|--help) usage; exit $RC_OK ;;
    *) echo "materials-coverage: unknown argument '$1' (try -h)" >&2; exit $RC_REFUSED ;;
  esac
done

[[ -d "$TREE" ]] || { echo "materials-coverage: no materials tree at $TREE" >&2; exit $RC_REFUSED; }

# --- refusals that protect the tree it measures -------------------------------
# An --only value this generator does not implement must never silently select
# nothing (a filter that matches nothing reads exactly like a clean tree).
if [[ -n "$ONLY" ]]; then
  case "$ONLY" in
    card|input|output|raw|annotated|finding|complete|unreadable) ;;
    *) echo "materials-coverage: --only '$ONLY' is not a condition (card|input|output|raw|annotated|finding|complete|unreadable)" >&2
       exit $RC_REFUSED ;;
  esac
fi

# The generator's own artifact is not a material: writing it inside the tree
# would put a *.tsv in the next census's scan surface.
TREE_ABS="$(cd "$TREE" && pwd)"
if [[ -n "$OUT" ]]; then
  OUT_DIR="$(cd "$(dirname "$OUT")" 2>/dev/null && pwd || echo "")"
  if [[ -n "$OUT_DIR" && ( "$OUT_DIR" == "$TREE_ABS" || "$OUT_DIR" == "$TREE_ABS"/* ) ]]; then
    echo "materials-coverage: --out inside the materials tree is refused ($OUT)" >&2
    echo "  the generator's product is not a material entry; writing it there pollutes the scan surface" >&2
    exit $RC_REFUSED
  fi
fi

COMMIT="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
git -C "$ROOT" status --porcelain 2>/dev/null | grep -q . && COMMIT="$COMMIT+dirty"

# ---------------------------------------------------------------- read helpers
# png_magic <file> — SUCCEEDS (exit 0) iff the file STARTS with the 8-byte PNG
# signature. Sized by signature, not by extension: a renamed .txt is not a
# screenshot. A missing or empty file FAILS — that is the whole point of the
# predicate, so the guard clause returns 1 (false), never 0.
png_magic() {
  [[ -s "$1" ]] || return 1
  [[ "$(head -c 8 "$1" 2>/dev/null | od -An -tx1 | tr -d ' \n')" == "$PNG_MAGIC" ]]
}

# has_body <file> — SUCCEEDS iff the file holds a line with something besides
# blanks. Same guard-clause discipline as png_magic: absent ⇒ false.
has_body() {
  [[ -f "$1" ]] || return 1
  grep -qE '[^[:space:]]' "$1" 2>/dev/null
}

# tree_hash — a content listing of every file under the tree (path, size,
# sha256), sorted. The read-only assertion compares this before against after;
# it is `find`+`sha256sum`, i.e. reads only.
tree_hash() {
  find "$TREE_ABS" -type f -printf '%P\t%s\n' 2>/dev/null | sort > "$1.list"
  while IFS=$'\t' read -r rel _; do
    printf '%s\t%s\n' "$rel" "$(sha256sum "$TREE_ABS/$rel" 2>/dev/null | cut -c1-16)"
  done < "$1.list"
}

WORK="$(gopi_scratch_mkdir materials-coverage)" || { echo "materials-coverage: cannot create an off-\`/\` scratch dir" >&2; exit $RC_ASSERT; }
trap 'gopi_scratch_cleanup' EXIT

tree_hash "$WORK/before" > "$WORK/before.hash"

# ------------------------------------------------------------------- the scan
# One row per candidate entry. A candidate is every immediate subdirectory of
# the tree (a dot-directory is not a material): the directory IS the entry, so a
# directory missing its card shows up as an incomplete entry rather than
# vanishing from the census — an entry that disappears from the denominator is
# the failure mode this script exists to prevent.
ROWS="$WORK/rows.tsv"
: > "$ROWS"

n=0
while IFS= read -r dir; do
  name="$(basename "$dir")"
  case "$name" in .*) continue ;; esac
  [[ -d "$dir" ]] || continue
  n=$((n+1))

  card=no; input=no; output=no; raw=no; annotated=no; finding=no
  [[ -s "$dir/$CARD" ]] && has_body "$dir/$CARD" && card=yes
  [[ -s "$dir/$INPUT" ]] && input=yes
  # An output without an exit code is not evidence — Rule 31's distinction
  # between "it ran" and "it succeeded" collapsed back into one thing.
  if [[ -s "$dir/$OUTPUT" ]] && grep -qE "^${EXIT_MARK}[0-9]+" "$dir/$OUTPUT" 2>/dev/null; then output=yes; fi
  png_magic "$dir/$RAW"       && raw=yes
  png_magic "$dir/$ANNOTATED" && annotated=yes
  [[ -s "$dir/$FINDING" ]] && has_body "$dir/$FINDING" && finding=yes

  complete=yes
  for c in "${CONDITIONS[@]}"; do
    [[ "${!c}" == "yes" ]] || complete=no
  done

  # The card's own declaration decides whether the annotated plate is load-
  # bearing. Absent declaration ⇒ yes (conservative: over-report a contradiction
  # rather than let an unreadable entry pass as merely unfinished).
  ann_req="$(grep -oE "^[-*] *annotation_required: *(yes|no)" "$dir/$CARD" 2>/dev/null | head -1 | grep -oE '(yes|no)$')"
  [[ -n "$ann_req" ]] || ann_req="$ANN_YES"
  unreadable=no
  [[ "$ann_req" == "$ANN_YES" && "$annotated" == "no" ]] && unreadable=yes

  face="$(grep -oE "^[-*] *face: *[A-Za-z0-9_-]+" "$dir/$CARD" 2>/dev/null | head -1 | awk '{print $NF}')"
  [[ -n "$face" ]] || face="undeclared"

  name_ok=no
  [[ "$name" =~ $NAME_RE ]] && name_ok=yes

  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$name" "$face" "$card" "$input" "$output" "$raw" "$annotated" "$finding" \
    "$complete" "$unreadable" "$ann_req" "$name_ok" >> "$ROWS"
done < <(find "$TREE_ABS" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | sort)

# ------------------------------------------------------------- derived counts
count_where() { awk -F'\t' -v c="$1" -v v="$2" 'tolower($c)==v' "$ROWS" | grep -c .; }
PRODUCED="$(count_where 9 yes)"      # column 9 = complete
UNREAD="$(count_where 10 yes)"       # column 10 = unreadable
DIRS="$n"
# Rows actually written. Counted here rather than re-derived by `grep -c`, so
# the assertion compares two INDEPENDENT numbers (the scan's own counter against
# the file it produced) — a re-derived count would only prove the file is
# self-consistent, which is exactly what a dropped row would preserve.
ROWS_N=$(grep -c . "$ROWS")

# ------------------------------------------------------------------ the writer
emit_tsv() {
  echo "# generator: $GENERATOR"
  echo "# generator_version: $GENERATOR_VERSION"
  echo "# measured_commit: $COMMIT"
  echo "# baseline: $BASELINE"
  echo "# baseline_source: $BASELINE_SOURCE"
  printf 'name\tface\tcard\tinput\toutput\traw\tannotated\tfinding\tcomplete\tunreadable\tannotation_required\tname_ok\n'
  cat "$ROWS"
}

show_only() {  # show_only <column> — the rows whose <column> is yes
  awk -F'\t' -v c="$1" 'tolower($c)=="yes"' "$ROWS"
}

emit_summary() {
  printf 'materials-coverage: %s\n' "$GENERATOR"
  printf '  tree             : %s\n' "$TREE_ABS"
  printf '  measured commit  : %s\n' "$COMMIT"
  printf '  produced         : %s / %s   (complete entries / baseline)\n' "$PRODUCED" "$BASELINE"
  printf '  baseline source  : %s\n' "$BASELINE_SOURCE"
  printf '  candidate dirs   : %s (complete=%s incomplete=%s)\n' "$DIRS" "$PRODUCED" "$((DIRS-PRODUCED))"
  printf '  unreadable       : %s (annotation_required=yes but 04-annotated.png absent)\n' "$UNREAD"
  if [[ "$PRODUCED" -gt "$BASELINE" ]]; then
    printf '  WARNING          : produced exceeds the baseline — the baseline is stale; see README.md §6 before changing it\n'
  fi
  if [[ "$DIRS" -gt 0 ]]; then
    echo "  detail (name, predicates that are NOT satisfied):"
    awk -F'\t' '{
      miss="";
      split("3 4 5 6 7 8",cols," ");
      names[3]="card"; names[4]="input"; names[5]="output"; names[6]="raw"; names[7]="annotated"; names[8]="finding";
      for(i=1;i<=6;i++){c=cols[i]; if($c!="yes"){miss=miss (miss==""?"":",") names[c]}}
      if ($12!="yes") miss=miss (miss==""?"":",") "bad-name";
      printf("    %-40s %s\n", $1, (miss==""?"complete":miss));
    }' "$ROWS"
  fi
}

# ---------------------------------------------------------------- self-asserts
# 1) every row must satisfy the conjunction it claims: complete=yes ⇒ six yeses.
BADCOMPLETE=$(awk -F'\t' '$9=="yes" && ($3!="yes"||$4!="yes"||$5!="yes"||$6!="yes"||$7!="yes"||$8!="yes")' "$ROWS" | grep -c .)
# 2) rows partition: complete ∈ {yes,no} and unreadable ⊆ (annotated=no).
BADVOCAB=$(awk -F'\t' '($9!="yes"&&$9!="no")||($10!="yes"&&$10!="no")' "$ROWS" | grep -c .)
BADUNREAD=$(awk -F'\t' '$10=="yes" && $7=="yes"' "$ROWS" | grep -c .)
# 3) the produced count equals the rows it claims to count.
BADCOUNT=$(( PRODUCED - $(awk -F'\t' '$9=="yes"' "$ROWS" | grep -c .) ))

fail=0
[[ "$BADCOMPLETE" -eq 0 ]] || { echo "ASSERT FAIL: $BADCOMPLETE row(s) claim complete with a condition unsatisfied" >&2; fail=1; }
[[ "$BADVOCAB" -eq 0 ]]    || { echo "ASSERT FAIL: $BADVOCAB row(s) carry a value outside {yes,no}" >&2; fail=1; }
[[ "$BADUNREAD" -eq 0 ]]   || { echo "ASSERT FAIL: $BADUNREAD row(s) are 'unreadable' while 04-annotated.png is present" >&2; fail=1; }
[[ "$BADCOUNT" -eq 0 ]]    || { echo "ASSERT FAIL: produced count ($PRODUCED) disagrees with the rows ($((PRODUCED-BADCOUNT)))" >&2; fail=1; }
[[ "$ROWS_N" -eq "$DIRS" ]] || { echo "ASSERT FAIL: candidate dirs ($DIRS) != rows in the report ($ROWS_N) — a candidate vanished from the census" >&2; fail=1; }
[[ "$BASELINE" -gt 0 ]]    || { echo "ASSERT FAIL: baseline $BASELINE — a zero denominator makes every ratio meaningless" >&2; fail=1; }

# 4) READ-ONLY assertion — the tree must be byte-identical after the scan.
tree_hash "$WORK/after" > "$WORK/after.hash"
if ! diff -q "$WORK/before.hash" "$WORK/after.hash" >/dev/null; then
  echo "ASSERT FAIL: the materials tree CHANGED during a read-only census:\n$(diff "$WORK/before.hash" "$WORK/after.hash" | head -20)" >&2
  fail=1
fi

# -------------------------------------------------------------------- output
if [[ "$MODE" == "tsv" ]]; then
  if [[ -n "$OUT" ]]; then emit_tsv > "$OUT" || fail=1; else emit_tsv; fi
else
  emit_summary
  if [[ -n "$ONLY" ]]; then
    case "$ONLY" in
      complete)   echo "  --only complete : $PRODUCED row(s)"; show_only 9 | cut -f1 | sed 's/^/    /' ;;
      unreadable) echo "  --only unreadable: $UNREAD row(s)";  show_only 10 | cut -f1 | sed 's/^/    /' ;;
      card)       echo "  --only card     : $(count_where 3 yes) row(s)"; show_only 3 | cut -f1 | sed 's/^/    /' ;;
      input)      echo "  --only input    : $(count_where 4 yes) row(s)"; show_only 4 | cut -f1 | sed 's/^/    /' ;;
      output)     echo "  --only output   : $(count_where 5 yes) row(s)"; show_only 5 | cut -f1 | sed 's/^/    /' ;;
      raw)        echo "  --only raw      : $(count_where 6 yes) row(s)"; show_only 6 | cut -f1 | sed 's/^/    /' ;;
      annotated)  echo "  --only annotated: $(count_where 7 yes) row(s)"; show_only 7 | cut -f1 | sed 's/^/    /' ;;
      finding)    echo "  --only finding  : $(count_where 8 yes) row(s)"; show_only 8 | cut -f1 | sed 's/^/    /' ;;
    esac
  fi
fi

[[ "$fail" -eq 0 ]] || exit $RC_ASSERT
exit $RC_OK
