#!/usr/bin/env bash
# Scratch-rig reproduction for the 2026-09-14 incident (task 01a09d67):
# an ACTIVE session's linked worktree was deleted mid-run by an external sweep.
#
# What it proves, end to end, through the REAL CLI (`gopi task sweep-worktrees
# --apply`), with NO reliance on /proc/<pid>/cwd (unreadable across sessions
# under kernel.yama.ptrace_scope=1 — the reason the old probe failed open):
#
#   [1] POSITIVE  a live-status row whose claimed holder pid is DEAD on this
#       host but whose recorded RUNNER is a live process → the worktree and its
#       git registration SURVIVE the apply pass.
#   [2] NEGATIVE  the guard is not a blanket refusal: a worktree whose runner
#       really exited IS reclaimed. The two DIRECTORY-deleting arms
#       (finish-cleanup ③ and reclaimGitRemove) have no non-resident CLI face —
#       measured: the reclaim leg runs only from mergerPassOnce /
#       runTaskLifecycleTick, i.e. inside a resident daemon — so this direction
#       is verified by the package test that reaches the arm directly, invoked
#       here so the rig covers both directions.
#
# Everything lives under the scratch tree (Rule 22 — never on the / partition;
# off `/` by MEASUREMENT, not by the retired /dev/shm default).
set -u

BIN="${BIN:-$(cd "$(dirname "$0")/.." && pwd)/bin/gopi}"
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib/tmp-ban.sh
. "$HERE/lib/tmp-ban.sh" || { echo "tmp-ban: scripts/lib/tmp-ban.sh missing" >&2; exit 2; }
# shellcheck source=lib/worktree-fresh.sh
. "$HERE/lib/worktree-fresh.sh" || { echo "worktree-fresh: scripts/lib/worktree-fresh.sh missing" >&2; exit 2; }
ROOT="${GOPI_TEST_SCRATCH:-$(gopi_scratch_home)}"
# The rig's own tree is a registered scratch directory under the queue: the trap
# removes it (and its children) even when the rig fails halfway.
RIG="$(gopi_scratch_mkdir live-holder-rig)" || { echo "FATAL: scratch root unavailable" >&2; exit 2; }
trap gopi_scratch_cleanup EXIT INT TERM
# rig_go_timeout is this rig's per-package test budget (Rule 1a: named once).
# The rig runs ONE test, but it runs it against the REAL ledger plumbing, and on
# a loaded host internal/tools can exceed Go's 10m default — a FAIL that read
# like a broken assertion while proving nothing (scripts/test-budget.sh's
# rationale). Declaring the budget keeps a load trip legible.
# THE BINDING CONSTRAINT ON THIS RIG IS THE TEST BUDGET, NOT A COST GUARD: it
# used to `export GOPI_IGNORE_PEAK_HOURS=1` to get past the peak-hour cost guard.
# That guard, its env reader and that escape hatch were REMOVED OUTRIGHT by
# fix/remove-peak-guard (commit bb0c4c1a, devdoc/peak-guard-removal.md), so
# GOPI_IGNORE_PEAK_HOURS is RETIRED — read by no one, and setting it changes
# nothing. No peak-hour override is needed to run this rig (or its suite) at any
# hour; the only budget that can trip it is the test budget below.
rig_go_timeout="${RIG_GO_TIMEOUT:-20m}"
export TMPDIR="$ROOT" TMP="$ROOT" TEMP="$ROOT"
PASS=0; FAIL=0
ok()  { echo "  PASS  $*"; PASS=$((PASS+1)); }
bad() { echo "  FAIL  $*"; FAIL=$((FAIL+1)); }

[ -x "$BIN" ] || { echo "missing binary: $BIN"; exit 2; }
mkdir -p "$RIG"

# prepare_rig <name> — isolated HOME (own memory.db), repo + linked worktree.
# Writes the three paths into $RIG/<name>/paths (never returns them via a
# command substitution: a substitution waits for EVERY child holding the pipe
# open, including a backgrounded runner — the bug this rig hit first).
prepare_rig() {
  local name="$1"
  local home="$RIG/$name/home" repo="$RIG/$name/repo" wt="$RIG/$name/wt"
  mkdir -p "$home/.gopi" "$repo"
  sqlite3 "$HOME/.gopi/memory.db" ".dump" > "$RIG/$name/schema.sql"
  sqlite3 "$home/.gopi/memory.db" < "$RIG/$name/schema.sql" >/dev/null
  (
    cd "$repo"
    git init -q . && git config user.email t@t && git config user.name t
    git checkout -q -b master
    echo base > a.txt && git add a.txt && git commit -qm init
    git checkout -q -b feat/live-holder
    echo work > work.txt && git add work.txt && git commit -qm work
    git checkout -q master
    gopi_assert_worktree_path_fresh "$wt" || exit 2
    git worktree add -q "$wt" feat/live-holder
  ) >/dev/null 2>&1
  # Age the REGISTRATION's creation record — the sweep's age gate measures
  # `<repo>/.git/worktrees/<name>/gitdir` (then HEAD), NOT the worktree
  # directory and NOT `commondir` (internal/tools/worktree_sweep.go
  # worktreeSweepRegistrationAge: "first hit wins, in this fixed order",
  # deliberately not the newest mtime because any `git status` bumps the git
  # dir). A fresh record is `keep [age-threshold] age=0h` (younger than
  # --min-age-h 1), so the positive arm never reaches the ledger-ownership arm
  # it exists to prove — a red that reads like a broken assertion while
  # proving nothing.
  find "$repo/.git/worktrees" -mindepth 2 -maxdepth 2 \( -name gitdir -o -name HEAD \) \
    -exec touch -d '3 days ago' {} + 2>/dev/null
  printf '%s\n%s\n%s\n' "$home" "$repo" "$wt" > "$RIG/$name/paths"
}

# seed_row <home> <task_id> <claimed_by> <repo> <wt> <runner_pid> <runner_start> <runner_cmd>
seed_row() {
  sqlite3 "$1/.gopi/memory.db" \
    "INSERT INTO tasks (task_id, summary, payload, status, claimed_by, deliverable,
                        proc_pid, proc_start_ticks, proc_cmd, created_at, updated_at)
     VALUES ('$2', 'rig row', '', 'in_progress', '$3',
             '{\"repo\":\"$4\",\"branch\":\"feat/live-holder\",\"worktree\":\"$5\"}',
             $6, $7, '$8', strftime('%s','now'), strftime('%s','now'));"
}

# start_runner — a live process standing in for a row's runner; its identity is
# written to $RIG/runner (stdout/stderr detached: nothing may hold our pipes).
# A pid that is provably dead on this host stands in for the claim stamp.
start_runner() {
  sleep 300 >/dev/null 2>&1 &
  local pid=$!
  sleep 0.3
  local start cmd
  start=$(awk '{print $22}' "/proc/$pid/stat")
  cmd=$(tr '\0' ' ' < "/proc/$pid/cmdline")
  printf '%s\n%s\n%s\n' "$pid" "$start" "$cmd" > "$RIG/runner"
}

# dead_pid — a real child, reaped, so "dead on this host" is measured not assumed.
dead_pid() {
  local p
  true & p=$!
  wait "$p" 2>/dev/null
  echo "$p"
}

run_sweep() { # <home> <repo>
  ( cd "$2" && HOME="$1" GOPI_MEMORY_DB="$1/.gopi/memory.db" \
      "$BIN" task sweep-worktrees --apply --min-age-h 1 2>&1 )
}

run_case() { # <case> <runner-alive:yes|no>
  local name="$1" alive="$2" home repo wt dpid rpid rstart rcmd out
  echo "[$name]  runner-alive=$alive"
  prepare_rig "$name"
  home=$(sed -n 1p "$RIG/$name/paths")
  repo=$(sed -n 2p "$RIG/$name/paths")
  wt=$(sed -n 3p "$RIG/$name/paths")
  dpid=$(dead_pid)
  if [ "$alive" = yes ]; then
    start_runner
    rpid=$(sed -n 1p "$RIG/runner"); rstart=$(sed -n 2p "$RIG/runner"); rcmd=$(sed -n 3p "$RIG/runner")
  else
    true >/dev/null 2>&1 &
    rpid=$!; wait "$rpid" 2>/dev/null
    rstart=$(awk '{print $22}' "/proc/$rpid/stat" 2>/dev/null || echo 1)
    rcmd="true"
  fi
  seed_row "$home" "t-rig-$name" "peerbox:$dpid" "$repo" "$wt" "$rpid" "$rstart" "$rcmd"
  out=$(run_sweep "$home" "$repo")

  if [ "$alive" = yes ]; then
    [ -d "$wt" ] && ok "worktree directory survived the apply pass" || bad "worktree DELETED (incident reproduced)"
    git -C "$repo" worktree list 2>/dev/null | grep -qF "$wt" && ok "git registration survived" || bad "registration dropped"
    if echo "$out" | grep -q "reclaim.*$wt"; then
      bad "worktree RECLAIMED: $(echo "$out" | grep "$wt" | head -1)"
    elif echo "$out" | grep -q "keep.*$wt (owned by live task t-rig-$name"; then
      ok "kept as owned by the live runner's row (the fix's own verdict)"
    else
      bad "unexpected disposition: $(echo "$out" | grep "$wt" | head -1)"
    fi
  else
    [ -d "$wt" ] && bad "dead runner's worktree NOT reclaimed (over-conservative guard, or the rig failed vacuously)" || ok "dead runner's worktree reclaimed"
    echo "$out" | grep -q "reclaim.*$wt" && ok "sweep reported the reclaim of this worktree" || bad "no reclaim line for the dead runner's worktree: $(echo "$out" | grep "$wt" | head -1)"
  fi
  kill "$rpid" 2>/dev/null
  [ -n "${out:-}" ] && echo "  sweep output: $(echo "$out" | head -3 | tr '\n' ' ')"
}

echo "rig: $RIG"
run_case positive yes

echo "[2]  negative direction — the guard is not a blanket refusal"
if ( cd "$(dirname "$BIN")/.." && GOPI_TEST_SCRATCH_ROOT="$ROOT" TMPDIR="$ROOT" \
      go test ./internal/tools/ -run 'TestDeadHolderReclaimStillFreesAProvenDeadRunner' -count=1 -v -timeout "$rig_go_timeout" 2>&1 ) \
     | grep -q -- "--- PASS: TestDeadHolderReclaimStillFreesAProvenDeadRunner"; then
  ok "a provably dead runner's worktree is still reclaimed (arm reached by the package test)"
else
  bad "the dead-runner reclaim direction did not pass"
fi
echo
echo "rig results: PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
