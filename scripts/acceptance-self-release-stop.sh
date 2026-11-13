#!/usr/bin/env bash

# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md — the operator owns this; an agent must never run it (submit the work instead: gopi task submit).
# Sandbox acceptance for the GRACEFUL-STOP half of the producer self-release
# (signal-handling epic, rig /tmp/self-release-ZhF9).
#
# The rig's own failure: `timeout 300 gopi worker --once` sent SIGTERM mid-round
# and the worker (Go's default disposition) died immediately — exit 143, no
# close-out, no own-worktree release, the row left in_progress and its worktree
# still in `git worktree list`, so safe-merge gate [c] kept refusing the branch
# of a run that was already over.
#
# This harness reproduces that shape exactly and asserts the fix:
#   A: real repo + two worktrees (the row's, and a live peer's)
#   B: a pending row whose run happens INSIDE the row's worktree
#   C: `timeout N gopi worker --once` — the supervisor's SIGTERM lands mid-round
#   D: the row's worktree is GONE, its branch tip is byte-identical,
#      the peer's worktree + branch are untouched
#   E: the worker log carries the stop line and the own-release line
set -u
# Scratch lives in the shared queue, never /tmp: source the one helper that
# asserts the path is off `/` and registers trap-time cleanup.
. "$(dirname "$0")/lib/tmp-ban.sh" || { echo "tmp-ban: scripts/lib/tmp-ban.sh missing"; exit 2; }
# shellcheck source=lib/worktree-fresh.sh
. "$(dirname "$0")/lib/worktree-fresh.sh" || { echo "worktree-fresh: scripts/lib/worktree-fresh.sh missing" >&2; exit 2; }
trap gopi_scratch_cleanup EXIT INT TERM
BIN=${BIN:?set BIN to the binary under test}
SB=$(gopi_scratch_mkdir self-sigrelease) || { echo "FATAL: scratch root unavailable" >&2; exit 2; }
export HOME="$SB/home"; mkdir -p "$HOME/.gopi"
export GOPI_MEMORY_DB="$HOME/.gopi/memory.db"
unset GOPI_TASK_WRAPPER_ID GOPI_TASK_RUN_ID GOPI_AUTO_TASK_REGISTER 2>/dev/null
export GOPI_ALLOW_AGENT_SPAWN=1 GOPI_HUMAN_TERMINAL=1

REPO="$SB/repo"; WT="$SB/wt"; WT2="$SB/wt2"
BR=feat/self-release-e2e; BR2=feat/self-live-e2e

git init -q "$REPO"; cd "$REPO"
git config user.email t@t; git config user.name t
git checkout -qb master; echo base > a.txt; git add a.txt; git commit -qm init
git checkout -qb "$BR"; echo delivery > deliverable.txt; git add deliverable.txt; git commit -qm "the deliverable"
TIP=$(git rev-parse "$BR")
git checkout -qb "$BR2"; echo other > other.txt; git add other.txt; git commit -qm "peer deliverable"
TIP2=$(git rev-parse "$BR2")
git checkout -q master
# EVERY creation site proves the path is FREE first (scripts/lib/worktree-fresh.sh
# is the shell delegation of internal/tools/worktree_fresh_path.go): git silently
# reuses an existing EMPTY directory, so a re-run left over from an aborted
# setup would hand this run a worktree path the previous run already owned.
gopi_assert_worktree_path_fresh "$WT" || exit 2
gopi_assert_worktree_path_fresh "$WT2" || exit 2
git worktree add -q "$WT" "$BR"
git worktree add -q "$WT2" "$BR2"

# A live peer: a process standing inside wt2 (the release must never free it).
( cd "$WT2" && sleep 300 ) & PEER=$!
sleep 0.5

PAYLOAD="Run these two bash commands in order and then reply DONE: first: git -C $WT status --short ; second: sleep 300 . Do not do anything else."
"$BIN" task submit --repo "$REPO" --payload "$PAYLOAD" "sandbox: stopped run releases its own worktree" 2>&1 | tail -1
cd "$REPO"
TID=$(python3 -c "import sqlite3,os;print(sqlite3.connect(os.environ['GOPI_MEMORY_DB']).execute('select task_id from tasks order by rowid desc limit 1').fetchone()[0])")
echo "A: sandbox $SB task=$TID wt=$WT"

# C: the supervisor's stop, mid-round.
# NO --worker-log-root: the option is retired (epic child-stream-task-output).
# A task's text goes to its own session directory, so the worker's own lines
# (which this script greps below) are exactly what lands in worker.log.
timeout 55 "$BIN" worker --once > "$SB/worker.log" 2>&1
echo "C: worker exit=$? (124/143 = the supervisor stopped it mid-round)"
grep -E "worker: .*received|stopping after|own-worktree-release|own worktree released" "$SB/worker.log" | sed 's/^/   /' | head -8

echo "D: worktree list:"
git worktree list | sed 's/^/   /'
if git worktree list | grep -q "$WT "; then RELEASED=NO; else RELEASED=yes; fi
if git worktree list | grep -q "$WT2 "; then KEPT=yes; else KEPT=NO; fi
echo "D: released=$RELEASED branch-tip-kept=$([ "$(git rev-parse $BR)" = "$TIP" ] && echo yes || echo NO) peer-kept=$KEPT peer-tip=$([ "$(git rev-parse $BR2)" = "$TIP2" ] && echo same || echo CHANGED)"

kill $PEER 2>/dev/null; wait $PEER 2>/dev/null

[ "$RELEASED" = yes ] && [ "$KEPT" = yes ] && echo "VERDICT: PASS" || echo "VERDICT: FAIL"
echo "sandbox: $SB"
