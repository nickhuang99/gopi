#!/usr/bin/env bash
# End-to-end acceptance evidence for the BINDING dirty-close-out gate
# (epic: a dirty close-out is not `completed`).
#
# Drives the ACTUAL CLI binary against a REAL repo + a REAL linked worktree, with
# its own scratch ledger (no production ledger touched, Rule 22: the scratch root
# is measured off the `/` partition below).
#
# Usage: bash scripts/evidence-dirty-closeout.sh <path-to-gopi-binary>
set -u
GOPI_BIN="${1:?usage: evidence-dirty-closeout.sh <gopi binary>}"
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib/tmp-ban.sh
. "$HERE/lib/tmp-ban.sh" || { echo "tmp-ban: scripts/lib/tmp-ban.sh missing" >&2; exit 2; }
# shellcheck source=lib/worktree-fresh.sh
. "$HERE/lib/worktree-fresh.sh" || { echo "worktree-fresh: scripts/lib/worktree-fresh.sh missing" >&2; exit 2; }
# One scratch directory under the SHARED scratch home — off the `/` partition by
# measurement — instead of a fixed path under the retired shared-memory base.
S="$(gopi_scratch_mkdir dirty-closeout-e2e)" || { echo "FATAL: scratch root unavailable" >&2; exit 2; }
trap gopi_scratch_cleanup EXIT INT TERM
echo "### Rule 22 check: scratch dev id vs / dev id ###"
echo "scratch: $(stat -c %d "$S")   /: $(stat -c %d /)"
gopi_assert_off_root_fs "$S" || exit 2

export GOPI_MEMORY_DB="$S/ledger.db"
export GOPI_TMPDIR="$S"
G() { "$GOPI_BIN" "$@"; }

make_repo() { # $1 = name
  local r="$S/$1"
  mkdir -p "$r"; cd "$r" || exit 1
  git init -q -b master .
  git config user.email t@t; git config user.name t
  echo base > a.txt; git add a.txt; git commit -qm init
  git checkout -qb feat/run
  echo deliverable > work.txt; git add work.txt; git commit -qm work
  git checkout -q master
  gopi_assert_worktree_path_fresh "$S/$1-wt" || exit 2
  git worktree add -q "$S/$1-wt" feat/run
  echo "$r"
}

close_out() { # $1 = task id
  G task update "$1" --status completed --reason "run finished; all green" --db "$GOPI_MEMORY_DB"
  echo "  exit=$?"
}

row_status() {
  sqlite3 -readonly "file:$GOPI_MEMORY_DB?mode=ro" "SELECT status FROM tasks WHERE task_id='$1'"
}

seed_ground() { # $1 = task id, $2 = repo, $3 = worktree
  sqlite3 "$GOPI_MEMORY_DB" "UPDATE tasks SET deliverable='{\"repo\":\"$2\",\"branch\":\"feat/run\",\"worktree\":\"$3\"}' WHERE task_id='$1'"
  # The row must be CLAIMED before a close-out is a legal move
  # (pending → in_progress), which is the state a run is in when it writes back.
  G task update "$1" --status in_progress --reason "claimed by the e2e run" --db "$GOPI_MEMORY_DB" >/dev/null
}

echo
echo "################ CASE 1 — the run's OWN worktree holds UNCOMMITTED TRACKED WORK ################"
R=$(make_repo c1); W="$S/c1-wt"
G task submit "e2e: a dirty close-out is not completed" --payload "e2e fixture" --task-id t-e2e-dirty --db "$GOPI_MEMORY_DB" >/dev/null
seed_ground t-e2e-dirty "$R" "$W"
cd "$W" || exit 1
echo "### the run writes its deliverable and exits WITHOUT committing ###"
echo "real, finished work" >> work.txt
echo "a new file" > new_note.md
echo "### Rule 30 self-check (must print nothing for a clean close-out) ###"
git status --porcelain
echo "### close-out attempt ###"
close_out t-e2e-dirty
echo "### the row (acceptance 1: it must NOT read completed) ###"
echo "row status: $(row_status t-e2e-dirty)"
echo "### the row's audit trail ###"
sqlite3 -readonly "file:$GOPI_MEMORY_DB?mode=ro" \
  "SELECT event || ' :: ' || substr(detail,1,600) FROM task_events WHERE task_id='t-e2e-dirty' ORDER BY id"
echo "### the work is untouched (no auto-commit / stash / delete) ###"
git -C "$W" status --porcelain
git -C "$W" log --oneline -1

echo
echo "################ CASE 2 — committing the work clears the refusal ################"
cd "$W" || exit 1
git add work.txt new_note.md && git commit -qm "the deliverable, committed by its author"
close_out t-e2e-dirty
echo "### the row (acceptance 2: CLEAN closes out normally) ###"
echo "row status: $(row_status t-e2e-dirty)"

echo
echo "################ CASE 3 — untracked-only residue keeps today's handling ################"
R3=$(make_repo c3); W3="$S/c3-wt"
G task submit "e2e: untracked residue" --payload "e2e fixture" --task-id t-e2e-residue --db "$GOPI_MEMORY_DB" >/dev/null
seed_ground t-e2e-residue "$R3" "$W3"
cd "$W3" || exit 1
echo "build output" > build.out
close_out t-e2e-residue
echo "row status: $(row_status t-e2e-residue)   <- untracked litter is RECORDED, not the tracked gate's business"

echo
echo "################ CASE 4 — a SIBLING row's LIVE claim on the same worktree ################"
R4=$(make_repo c4); W4="$S/c4-wt"
G task submit "e2e: sibling holds the tree" --payload "e2e fixture" --task-id t-e2e-sib --db "$GOPI_MEMORY_DB" >/dev/null
G task submit "e2e: closing run" --payload "e2e fixture" --task-id t-e2e-close --db "$GOPI_MEMORY_DB" >/dev/null
seed_ground t-e2e-sib "$R4" "$W4"
seed_ground t-e2e-close "$R4" "$W4"
# A LIVE claim on the same directory: this shell's own pid is provably alive.
sqlite3 "$GOPI_MEMORY_DB" "UPDATE tasks SET claimed_by='$(hostname):$$' WHERE task_id='t-e2e-sib'"
cd "$W4" || exit 1
echo "the sibling's live edit" >> work.txt
close_out t-e2e-close
echo "row status: $(row_status t-e2e-close)  <- another run's LIVE work is never this run's refusal"

echo
echo "################ CASE 5 — the operator's reviewed override ################"
R5=$(make_repo c5); W5="$S/c5-wt"
G task submit "e2e: override" --payload "e2e fixture" --task-id t-e2e-override --db "$GOPI_MEMORY_DB" >/dev/null
seed_ground t-e2e-override "$R5" "$W5"
cd "$W5" || exit 1
echo "reviewed by a human" >> work.txt
echo "### close-out with GOPI_TASK_DIRTY_CLOSEOUT_ALLOW=1 ###"
GOPI_TASK_DIRTY_CLOSEOUT_ALLOW=1 close_out t-e2e-override
echo "row status: $(row_status t-e2e-override)"
sqlite3 -readonly "file:$GOPI_MEMORY_DB?mode=ro" \
  "SELECT event FROM task_events WHERE task_id='t-e2e-override' ORDER BY id"
echo
echo "### DONE — scratch: $S"
