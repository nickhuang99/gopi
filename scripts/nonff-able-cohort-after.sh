#!/usr/bin/env bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# nonff-able-cohort-after.sh — the "AFTER" half of the delivery-point-sync
# measurement. WHAT IT CAN MEASURE TODAY is the BEFORE-STATE of a cohort of
# branches in a CLONE of the repo: is each one already fast-forwardable into the
# integration branch, how far ahead/behind, and what its ledger row's trail says.
# WHAT IT NO LONGER MEASURES is delivery — the verb that produced an "after"
# (`gopi task finish-cleanup`) was retired in 58b5618b; see "THE VERB IT DROVE IS
# RETIRED" below, which also says why the run therefore ends exit 2.
#
#   usage: scripts/nonff-able-cohort-after.sh [--main REPO] [--gopi BIN] [branch ...]
#
#     --main REPO   the repo to clone (default: the current repo)
#     --gopi BIN    the gopi binary to drive (default: bin/gopi next to this repo)
#     branch ...    the cohort; OMITTED, the default selector takes every branch
#                   the clone's origin advertises except the integration branch,
#                   archive/*, symbolic refs and the branch this clone has
#                   checked out (see the selector's own note for why each one is
#                   excluded — two of them were the defect this revision fixes)
#
# WHAT IT MEASURES, and why the number is not simply "how many became ff-able":
# the epic's property is that a branch is NOT DELIVERED while it cannot be
# fast-forwarded into the integration branch. The verb therefore has two honest
# outcomes and the script reports them separately:
#
#   * delivered  — the sync merged the integration branch, the repo's DECLARED
#                  build compiled the merged state, the worktree was released
#                  (`refused=0`) and the branch is now ff-able;
#   * refused    — a REAL semantic conflict (or a merged state that does not
#                  build): the branch keeps its check-out, its tip is unchanged,
#                  `premerge-refused` is on the row, and it is NOT handed over as
#                  an undiagnosed merge problem.
#
# A refusal is the gate working. What must FALL is the count of branches that
# leave close-out non-ff-able and UNEXPLAINED — that is what the script's
# summary line separates (`unexplained=0` is the pass condition).
#
# THE VERB IT DROVE IS RETIRED, SO THE DELIVERY HALF IS RETIRED WITH IT
# (2026-10-31, task 01a18f07). `gopi task finish-cleanup` was retired in
# 58b5618b (task 01a18799, slice 3/4) together with the worktree+branch delivery
# semantics it served: the spelling answers a REMOVAL sentence and exits 1, so
# nothing is delivered by typing it, and a run releases its own ground as it
# ends. Driving it would print the removal N times and measure the harness, not
# the branch — scoring every branch "not delivered" for a reason that lives
# outside every branch. So this file no longer scores delivery at all (the
# `delivered/refused/unexplained` counters are GONE, not kept at a dead expected
# value), and it ends exit 2 with that reason stated. What it still MEASURES is
# the half that is measurable today and is what its default selector used to get
# wrong: WHICH branches this clone can actually set up, and each one's real
# before-state (ancestry, distance behind, and its ledger row's trail). Restoring
# a delivery verdict needs a live close-out verb — a replacement for that verb,
# not a re-spelling of this script.
#
# ISOLATION (each one bought with a measured failure):
#   * ONE clone for the whole cohort — the git database and the build cache are
#     per-repo; a second clone only re-copies the object store (Rule 29's economy);
#   * a PER-ITERATION GOPI_HOME, ledger and worktree dir — a shared home let one
#     killed iteration's serve lock refuse the next one, and a reused worktree
#     dir made `git worktree add` fail with "already exists" on iteration 2+;
#   * a stray index.lock is cleared between iterations (a killed git leaves it).
#
# Rule 22: the scratch root is measured against `/`, never spelled — and the
# script REFUSES to run if it lands on the `/` partition.
# Rule 12: the build command is read from the host's declared settings, never
# invented; a repo with no declaration merges and reports the abstention.
#
# THE LEDGER CHANNEL IS GO, NOT `sqlite3` (Rule 35 + Rule 15). This script seeds
# one `completed` row per cohort branch and then reads that row's `task_events`
# trail to decide whether a non-ff-able branch was REFUSED for a recorded reason
# or left UNEXPLAINED. It used to do both through the `sqlite3` command line,
# which is NOT installed on this host: the query returned nothing, nothing is
# what an unexplained branch looks like, so the summary read
# `refused=0 … UNEXPLAINED=<cohort size>` and the script exited 1 for a reason
# that existed only on its stdio. `scripts/lib/ledger-sql.sh` compiles a
# 150-line `database/sql` helper over modernc.org/sqlite (already a module
# dependency, pure Go, no CGO) into this run's scratch root, and every ledger
# read/write below goes through it with its exit status CHECKED. A missing Go
# toolchain or a broken build is a hard exit 2 with the reason — never an empty
# trail carried into the verdict.
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=lib/worktree-fresh.sh
. "$repo_root/scripts/lib/worktree-fresh.sh" || { echo "worktree-fresh: scripts/lib/worktree-fresh.sh missing" >&2; exit 2; }
# shellcheck source=lib/ledger-sql.sh
. "$repo_root/scripts/lib/ledger-sql.sh" || { echo "ledger-sql: scripts/lib/ledger-sql.sh missing" >&2; exit 2; }
MAIN="$repo_root"
GOPI="$repo_root/bin/gopi"
cohort=()

while [ $# -gt 0 ]; do
  case "$1" in
    --main) MAIN="$(cd "$2" && pwd)"; shift 2 ;;
    --gopi) GOPI="$2"; shift 2 ;;
    *) cohort+=("$1"); shift ;;
  esac
done

# The scratch root: the caller's GOPI_COHORT_ROOT, else a sibling of the clone
# target under the workspace. Measured, never spelled (Rule 22).
W="${GOPI_COHORT_ROOT:-$(cd "$MAIN/.." && pwd)/gopi-cohort-after}"
L="$W/run"
if [ "$(stat -c %d "$W" 2>/dev/null)" = "$(stat -c %d /)" ]; then
  echo "nonff-able-cohort-after: $W is on the / partition — refusing (Rule 22)" >&2
  exit 2
fi
[ -x "$GOPI" ] || { echo "nonff-able-cohort-after: no gopi binary at $GOPI (pass --gopi)" >&2; exit 2; }

# The ledger channel, compiled ONCE into this run's scratch root (see the header
# note above and scripts/lib/ledger-sql.sh). Failing here is exit 2 with a stated
# reason: a cohort measured without a trail is not a measurement.
gopi_ledgersql_ensure "$repo_root" "$W" || {
  echo "nonff-able-cohort-after: the ledger channel (scripts/lib/ledgersql) could not be prepared — refusing to measure" >&2
  exit 2
}
echo "ledger channel: $GOPI_LEDGERSQL_BIN"

# Prove the channel can WRITE before trusting it to create the per-iteration
# ledgers, and prove it can READ what it wrote before trusting its "no rows".
probe_db="$W/$GOPI_LEDGERSQL_NAME/probe.db"
rm -f "$probe_db"
if ! gopi_ledgersql --db "$probe_db" "CREATE TABLE probe (x INTEGER)"; then
  echo "nonff-able-cohort-after: the ledger channel cannot create a ledger — refusing to measure" >&2
  exit 2
fi
if [ "$(gopi_ledgersql --read-only --db "$probe_db" "SELECT count(*) FROM probe")" != "0" ]; then
  echo "nonff-able-cohort-after: the ledger channel did not answer a known query with 0 — refusing to measure" >&2
  exit 2
fi
rm -f "$probe_db"


# THE DECLARED BUILD IS NO LONGER READ HERE, because nothing in this file runs
# a merge any more (see the header: the close-out verb that consumed `$BUILD`
# was retired in 58b5618b, and the per-iteration settings document below existed
# only to point that verb at the repo's declared build). Keeping the read would
# be an unmeasured knob advertised as measured (Rule 15); restoring a delivery
# verdict means restoring the verb AND this read together.
MODCACHE="$(go env GOMODCACHE)"

# ONE cohort run at a time, enforced by a lock — never by killing.
#
# An earlier revision called `pkill -f "$GOPI task finish-cleanup"` here, and it
# SIGNALLED AN UNRELATED LEGITIMATE RUN of the same binary out of existence
# (measured: the real-door transcript ends in `Terminated`, exit=143, mid-build)
# — a cohort script has no business killing processes it did not start, and
# `pkill -f` matches any run of that binary anywhere on the host (Rule 24's
# sibling trap: a pattern-matched kill is not evidence of ownership). The
# contention this was meant to prevent (serve locks, index.lock) is already
# handled structurally: every iteration gets its OWN GOPI_HOME, ledger and
# worktree directory, so two cohort runs would only waste CPU, and the lock
# below refuses the second one outright.
mkdir -p "$W"
exec 9>"$W/.cohort.lock"
if ! flock -n 9; then
  echo "nonff-able-cohort-after: another cohort run holds $W/.cohort.lock — refusing to interleave" >&2
  exit 2
fi

rm -rf "$L"; mkdir -p "$L"
git clone -q "$MAIN" "$L/repo" || exit 2
git -C "$L/repo" config user.email cohort@local
git -C "$L/repo" config user.name cohort
echo "clone: $L/repo @ $(git -C "$L/repo" rev-parse --short HEAD)"
echo "build: not read — nothing here runs a merge any more (the close-out verb it fed was retired in 58b5618b)"

if [ "${#cohort[@]}" -eq 0 ]; then
  # THE SELECTOR, and the two defects this revision removes.
  #
  # A fresh clone carries only the integration branch as a LOCAL ref — the
  # cohort must come from the clone's origin refs, or the default cohort is
  # empty and the measurement reads green-blind (measured: "cohort: 0 branches").
  # Reading `refs/remotes/origin/` with `%(refname:short)` and then stripping a
  # literal `origin/` prefix produced TWO unmeasurable entries besides the real
  # branches:
  #
  #   ① the SYMBOLIC `refs/remotes/origin/HEAD` renders as the bare short name
  #      `origin` (there is no slash for the `sed` to strip), so the selector
  #      manufactured a pseudo-branch literally named `origin` — `git branch -f
  #      origin origin/origin` cannot resolve, so it scored as "absent — skipped";
  #   ② the branch the CLONE ITSELF has checked out arrives as `origin/task/…`
  #      and was selected too — but `git worktree add` REFUSES a branch that is
  #      already checked out in the same repository, so it could never be set up.
  #
  # Measured on a fresh clone of this repo (2026-10-31): "cohort: 2 branches
  # … not-measured=2" — a cohort of exactly the two entries that cannot be
  # measured. The selector now asks git for the branches themselves instead of
  # deriving names from strings: symbolic refs are dropped by git (not by a
  # prefix strip), the integration branch and `archive/*` stay excluded, and the
  # branch this clone has checked out is excluded by NAME rather than being
  # selected and then refused by `git worktree add`.
  checked_out="$(git -C "$L/repo" symbolic-ref --quiet --short HEAD || true)"
  mapfile -t cohort < <(
    git -C "$L/repo" for-each-ref \
      --format='%(if)%(symref)%(then)%(end)%(refname:lstrip=3)' \
      refs/remotes/origin/ \
      | grep -v -x -e '' -e HEAD -e master -e main \
      | grep -v -e '^archive/' \
      | { if [ -n "$checked_out" ]; then grep -v -x -F "$checked_out"; else cat; fi; }
  )
  echo "cohort selector: ${#cohort[@]} measurable branch(es) from refs/remotes/origin/" \
       "(excluded: the integration branch, archive/*, symbolic refs, and the checked-out '${checked_out:-<detached HEAD>}')"
fi

i=0
# Every branch that could not be set up is COUNTED and NAMED (Rule 15) and the
# run ends exit 2 — a short cohort with a green-looking summary is a lie about
# coverage, and that is what this file reported before (2 selected, 0 measured).
skipped=0
measured=0
# A pass over an empty cohort is not a pass: refuse rather than report 0/0
# (found by running this script — it reported "cohort: 0 branches … 0/0" and
# exited 0 on a fresh clone whose branches were not read at all).
if [ "${#cohort[@]}" -eq 0 ]; then
  echo "nonff-able-cohort-after: the cohort is EMPTY — nothing was measured (refusing)" >&2
  exit 2
fi
for BR in "${cohort[@]}"; do
  i=$((i+1)); TID="cohort-before-$i"
  H="$L/home-$i"; WT="$L/wt-$i"; DB="$L/memory-$i.db"
  mkdir -p "$H"
  if ! git -C "$L/repo" rev-parse --verify --quiet "refs/heads/$BR" >/dev/null; then
    git -C "$L/repo" branch -f "$BR" "origin/$BR" >/dev/null 2>&1 \
      || { echo "!! $BR: absent from the clone and from origin — skipped (NOT measured)" >&2; skipped=$((skipped+1)); continue; }
  fi
  gopi_assert_worktree_path_fresh "$WT" || { echo "!! $BR: worktree path taken — skipped (NOT measured)" >&2; skipped=$((skipped+1)); continue; }
  # The refusal text is quoted, never swallowed: "worktree add failed" alone
  # would hide WHICH of git's refusals fired (already checked out, bad ref, …).
  if ! add_out="$(git -C "$L/repo" worktree add -q "$WT" "$BR" 2>&1)"; then
    echo "!! $BR: git worktree add refused — skipped (NOT measured): $add_out" >&2
    skipped=$((skipped+1)); continue
  fi

  if git -C "$L/repo" merge-base --is-ancestor master "$BR"; then b=YES; else b=NO; fi
  behind=$(git -C "$L/repo" rev-list --count "$BR..master")
  ahead=$(git -C "$L/repo" rev-list --count "master..$BR")

  # The scratch ledger's SCHEMA comes from gopi itself (its own task-list open
  # creates it) — and that status is CHECKED: a ledger with no `tasks` table
  # would make the seed below fail, which on a silent path is indistinguishable
  # from "this branch's row has no trail".
  if ! "$GOPI" task list --db "$DB" >/dev/null 2>&1; then
    echo "!! $BR: \`gopi task list --db $DB\` failed — cannot prepare this iteration's ledger" >&2
    exit 2
  fi
  # One row per branch, recording the branch and its ground as the deliverable
  # block — the shape a close-out would read. The row is written through the
  # ledger channel (never a command line this host does not have) and its exit
  # status is checked.
  if ! gopi_ledgersql --db "$DB" "INSERT OR REPLACE INTO tasks (task_id, summary, status, deliverable, source, created_at, updated_at)
    VALUES ('$TID', 'cohort before-state', 'completed',
            '{\"repo\":\"$L/repo\",\"branch\":\"$BR\",\"worktree\":\"$WT\"}', 'auto', 1, 1)"; then
    echo "!! $BR: could not seed the ledger row $TID in $DB — refusing to measure a branch whose record cannot be written" >&2
    exit 2
  fi
  # THE TRAIL read is the one query the per-branch line leans on, so its failure
  # can NEVER be read as "this row has no events": a non-zero status here is a
  # stated abort, and only an ANSWERED query with an empty result counts as
  # "no events".
  if ! trail="$(gopi_ledgersql --read-only --db "$DB" \
        "SELECT COALESCE(group_concat(event,' ;; '),'') FROM task_events WHERE task_id='$TID' ORDER BY id;")"; then
    echo "!! $BR: reading task_events for $TID failed — the trail is UNMEASURED, refusing to report this branch" >&2
    exit 2
  fi

  printf -- '-> %-48s tip=%s ff-able=%s behind=%s ahead=%s trail=%s\n' \
    "$BR" "$(git -C "$L/repo" rev-parse --short "$BR")" "$b" "$behind" "$ahead" "${trail:-(read, no events)}"
  measured=$((measured+1))

  git -C "$L/repo" worktree remove --force "$WT" >/dev/null 2>&1 || true
  git -C "$L/repo" worktree prune
  rm -f "$L/repo/.git/index.lock" 2>/dev/null || true
done

echo
echo "cohort: ${#cohort[@]} branch(es) selected   set up and MEASURED=$measured   not-measured=$skipped"
echo "  per branch: ff-able = \`git merge-base --is-ancestor master <branch>\`; behind/ahead = \`git rev-list --count\`;"
echo "  trail = the seeded row's task_events read through the ledger channel (\"(read, no events)\" = answered, empty)."
# THE DELIVERY HALF IS RETIRED WITH THE VERB IT DROVE, and this is stated, not
# scored. `gopi task finish-cleanup` was retired in 58b5618b (task 01a18799,
# slice 3/4) with the worktree+branch delivery semantics it served, so nothing
# in this file can produce a "delivered / refused / unexplained" verdict any
# more — that verb refusing to run is not a property of any branch. Saying so
# and exiting 2 is the honest ending; printing a delivery number here would be
# the same lie the sqlite3 bug told (a number that moved for a reason living
# outside the branches).
echo "nonff-able-cohort-after: the DELIVERY half of this measurement is retired — \`gopi task finish-cleanup\` was removed in 58b5618b," >&2
echo "  so the before-state above is what this run measured and no delivered/refused/unexplained verdict is reported (exit 2)" >&2
if [ "$skipped" -ne 0 ]; then
  echo "nonff-able-cohort-after: $skipped of ${#cohort[@]} selected branch(es) could not be set up — the cohort was NOT fully measured (exit 2)" >&2
fi
exit 2
