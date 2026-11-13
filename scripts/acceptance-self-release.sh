#!/usr/bin/env bash
# Sandbox acceptance: a PRODUCER releases its OWN worktree when it finishes.
#
# SANDBOX TREE: <scratch home>/queue/queue-self-release-<pid> owned by THIS run
# (helpers below) — never /tmp, whose exhaustion is documented in
# scripts/lib/tmp-ban.sh. The old /tmp sandbox survived on the
# `GOPI_ACCEPT_ALLOW_TMP_SANDBOX` escape hatch; the helper is the contract now,
# so the script drops the escape instead of relying on it.
#
#   SB1 : repo + two linked worktrees (real git).
#   HOME2: a FRESH ledger for this run (so nothing here is credited to a sweep
#          over a previous run's rows).
#   R1  : the run's own row names the worktree it ran in => the worker round's
#         close-out frees that directory, keeps the branch (the deliverable),
#         and safe-merge gate [c] stops refusing it.
#   R2  : the same pass must NOT free ground somebody is standing in — a live
#         process inside the worktree => a NAMED refusal, directory survives.
#
# WHY THE ROW'S deliverable.worktree MATTERS (the gap in the first version of
# this sandbox): the release pass is keyed on "rows claimed by THIS process
# whose deliverable names a worktree". A row whose deliverable is
# {"repo":...} alone has NOTHING to release and the pass correctly prints
# nothing — which looks exactly like the defect. In a real run the executor
# seam (tools.TaskDeliverableWorktreeBash) stamps deliverable.worktree the
# first time the run touches git inside a linked worktree of its repo; the
# brief below asks for exactly that round, and the script ALSO stamps the row
# itself so the release arm is exercised even when the child never gets there
# (test escape: GOPI_TASK_CHILD_FAIL_TEST=1 keeps the round bounded).
#
# usage: acceptance-self-release.sh [SANDBOX_DIR]
#   no argument      -> a fresh sandbox under the SHARED scratch home
#                       (<home>/workspace/gopi-test-scratch/queue), off `/`,
#                       trap-cleaned — never /tmp (scripts/lib/tmp-ban.sh; the
#                       2026-09-11 exhaustion is why)
#   SANDBOX_DIR      -> reuse its repo/wt/wt2 (a fresh ledger is still used)
set -u
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Scratch lives in the shared queue, never /tmp: source the one helper that
# asserts the path is off `/` and registers trap-time cleanup. The trap itself
# is installed just below by cleanup_sandbox (not here), because a REUSE run
# keeps the operator's sandbox.
# shellcheck source=lib/tmp-ban.sh
. "$HERE/lib/tmp-ban.sh" || { echo "tmp-ban: scripts/lib/tmp-ban.sh missing"; exit 2; }
# shellcheck source=lib/worktree-fresh.sh
. "$HERE/lib/worktree-fresh.sh" || { echo "worktree-fresh: scripts/lib/worktree-fresh.sh missing" >&2; exit 2; }
# The release worktree is DERIVED from this checkout, never a host path.
WT_ROOT=${WT_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}
GOPI=${GOPI:-$WT_ROOT/bin/gopi}
SM=${SM:-$WT_ROOT/scripts/safe-merge.sh}
unset GOPI_TASK_WRAPPER_ID GOPI_TASK_RUN_ID GOPI_AUTO_TASK_REGISTER 2>/dev/null
# Scratch allocation goes through the shared helper: each directory is asserted
# off `/`, REGISTERED for trap-time removal, and named queue-<sub>-<pid> so the
# age GC recognises it too. This root never names an SB arm (both arms mint
# their own directory below); it is the registered parent the helper hands back,
# and it is what makes a crash between source-time and arm entry collectable.
LEDGER_ROOT="$(gopi_scratch_mkdir self-release)" || { echo "FATAL: scratch root unavailable" >&2; exit 2; }
# A reuse run's sandbox belongs to the operator (SANDBOX_DIR was passed in):
# only a self-built sandbox is thrown away, and then the trap reaches children
# before it removes anything. Either way the AGE GC bounds the scratch home.
REUSE=0
cleanup_sandbox() {
  if [ "$REUSE" = 1 ]; then
    echo "kept reused sandbox: $SB1 (self-built trees are removed on exit)"
    return 0
  fi
  gopi_scratch_cleanup
}
trap cleanup_sandbox EXIT INT TERM

if [ $# -ge 1 ]; then
  # A REUSE run's sandbox is the operator's: only a self-built one is thrown
  # away, so the fresh ledger lives in its own registered scratch dir (helper
  # = off-`/` asserted, trap-cleaned, age-GC'd).
  REUSE=1
  SB1=$1; SB=$(gopi_scratch_mkdir self-release-ledger)
  REPO="$SB1/repo"; WT="$SB1/wt"; WT2="$SB1/wt2"
  export HOME="$SB/home"; mkdir -p "$HOME/.gopi"
  export GOPI_MEMORY_DB="$HOME/.gopi/memory.db"
  test -d "$WT" || { echo "SANDBOX CHECK: no $WT — pass a built sandbox or no argument"; exit 2; }
  # the ledger needs the row's repo to exist; the fixture below is skipped
  BR=feat/self-release-e2e; BR2=feat/self-live-e2e
else
  # A self-built sandbox IS this run's alone (trap reaches children before
  # removing it; the age GC bounds the scratch home as the backstop).
  SB1=$(gopi_scratch_mkdir self-release); SB=$SB1
  export HOME="$SB/home"; mkdir -p "$HOME/.gopi"
  export GOPI_MEMORY_DB="$HOME/.gopi/memory.db"
  REPO="$SB/repo"; WT="$SB/wt"; WT2="$SB/wt2"; BR=feat/self-release-e2e; BR2=feat/self-live-e2e
  git init -q "$REPO"; cd "$REPO"
  git config user.email t@t; git config user.name t
  git checkout -qb master; echo base > a.txt; git add a.txt; git commit -qm init
  git checkout -qb "$BR"; echo delivery > deliverable.txt; git add deliverable.txt; git commit -qm "the deliverable"
  git checkout -qb "$BR2"; echo other > other.txt; git add other.txt; git commit -qm "peer deliverable"
  git checkout -q master
  gopi_assert_worktree_path_fresh "$WT" || exit 2
  gopi_assert_worktree_path_fresh "$WT2" || exit 2
  git worktree add -q "$WT" "$BR"; git worktree add -q "$WT2" "$BR2"
fi
cd "$REPO"
TIP=$(git rev-parse "$BR"); TIP2=$(git rev-parse "$BR2")

# The row's deliverable (repo/branch/worktree) is what the worker's own
# release pass reads. It must be STAMPED HERE: the candidate query filters
# `deliverable LIKE '%"worktree"%'` (internal/tools/task_reclaim_self.go), while
# the run's own writeback REWRITES the deliverable to {"repo": ...} — a row that
# is never explicitly stamped therefore has no worktree to release, and the
# checks below can never pass.
stamp() { # stamp <task_id> <branch> <worktree>
  python3 - "$1" "$2" "$3" <<'PY'
import json, sqlite3, sys, os
tid, br, wt = sys.argv[1:4]
c = sqlite3.connect(os.environ["GOPI_MEMORY_DB"])
d = json.dumps({"repo": os.environ["REPO"], "branch": br, "worktree": wt})
cur = c.execute("update tasks set deliverable=? where task_id=?", (d, tid)); c.commit()
# The stamp is a HARD precondition of BOTH arms (the candidate query filters
# deliverable LIKE '%"worktree"%'): a row left repo-only has nothing to
# release, and every check below would then report a regression that is really
# a mis-stamped fixture. So the stamp verifies ITSELF — the update must match
# exactly this row and read back byte-equal, else the rig refuses here instead
# of failing downstream for the wrong reason.
if cur.rowcount != 1:
    raise SystemExit("STAMP: matched %d rows for task_id=%r (the id is wrong)" % (cur.rowcount, tid))
row = c.execute("select deliverable from tasks where task_id=?", (tid,)).fetchone()
back = row[0] if row else None
if not back or json.loads(back) != d:
    raise SystemExit("STAMP: read-back mismatch for %r: %r" % (tid, back))
print("STAMP: row %s -> %s (read back ok)" % (tid, d))
PY
  [ $? -eq 0 ] || { echo "VERDICT: FAIL (rig stamp did not land — the release pass would have no candidate)"; exit 3; }
}
newrow() { # newrow <summary> -> task id on stdout
  "$GOPI" task submit --repo "$REPO" "$1" >/dev/null 2>&1
  # Resolve the FULL ledger id: the `task list` display form is a truncated
  # `#NNN…`, which matches no row in the tasks table.
  python3 -c "import sqlite3,os;print(sqlite3.connect(os.environ['GOPI_MEMORY_DB']).execute('select task_id from tasks order by rowid desc limit 1').fetchone()[0])"
}
export REPO

echo "A: pre-state"
git worktree list | sed 's/^/   /'
bash "$SM" "$BR" >/dev/null 2>&1; echo "A: safe-merge pre exit=$? (non-zero expected: gate [c] occupied)"

########################################################################
echo "R1: the run's own worktree is released at its close-out"
T1=$(newrow "sandbox: producer releases its own worktree")
stamp "$T1" "$BR" "$WT"
GOPI_ALLOW_AGENT_SPAWN=1 GOPI_HUMAN_TERMINAL=1 GOPI_TASK_CHILD_FAIL_TEST=1 \
  "$GOPI" worker --once > "$SB1/worker-r1.log" 2>&1
echo "R1: worker exit=$?"
grep -E 'own-release|own-worktree-release' "$SB1/worker-r1.log" | sed 's/^/   /'
git worktree list | grep -q "$WT " && R1GONE=NO || R1GONE=yes
[ -d "$WT" ] && R1DIR=NO || R1DIR=yes
[ "$(git rev-parse "$BR")" = "$TIP" ] && R1TIP=yes || R1TIP=NO
bash "$SM" "$BR" >/dev/null 2>&1; R1SM=$?
grep -q 'own worktree released' "$SB1/worker-r1.log" && R1LINE=yes || R1LINE=NO
echo "R1: released=$R1GONE dir-gone=$R1DIR branch-tip-kept=$R1TIP safe-merge=$R1SM release-line=$R1LINE"

########################################################################
echo "R2: a live holder keeps its worktree (named refusal, never a silent one)"
# The holder MUST outlive the whole R2 round: a short sleep races R1/R2 and
# makes the guard look broken when the peer simply exited first (observed:
# R2 kept=NO with a legitimate release line). Bound it generously and kill it
# explicitly below.
( cd "$WT2" && sleep 900 ) & PEER=$!
sleep 1
T2=$(newrow "sandbox: a live holder keeps its worktree")
stamp "$T2" "$BR2" "$WT2"
GOPI_ALLOW_AGENT_SPAWN=1 GOPI_HUMAN_TERMINAL=1 GOPI_TASK_CHILD_FAIL_TEST=1 \
  "$GOPI" worker --once > "$SB1/worker-r2.log" 2>&1
echo "R2: worker exit=$?"
grep -E 'own-release|own-worktree-release' "$SB1/worker-r2.log" | sed 's/^/   /'
git worktree list | grep -q "$WT2 " && R2KEPT=yes || R2KEPT=NO
[ "$(git rev-parse "$BR2")" = "$TIP2" ] && R2TIP=yes || R2TIP=NO
grep -q 'own worktree release refused' "$SB1/worker-r2.log" && R2REF=yes || R2REF=NO
grep -q 'live process' "$SB1/worker-r2.log" && R2WHY=yes || R2WHY=NO
kill $PEER 2>/dev/null; wait $PEER 2>/dev/null
echo "R2: kept=$R2KEPT tip-kept=$R2TIP refusal-named=$R2REF reason-named=$R2WHY"

echo "final worktree list:"; git worktree list | sed 's/^/   /'
if [ "$R1GONE$R1DIR$R1TIP$R1LINE" = "yesyesyesyes" ] && [ "$R1SM" = 0 ] &&
   [ "$R2KEPT$R2TIP$R2REF$R2WHY" = "yesyesyesyes" ]; then
  echo "VERDICT: PASS"
else
  echo "VERDICT: FAIL"
fi
echo "sandbox: $SB1 ; ledger: $SB"
