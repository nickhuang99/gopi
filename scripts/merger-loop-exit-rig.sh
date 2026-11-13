#!/usr/bin/env bash
# merger-loop-exit-rig.sh — observe how long a resident `gopi merger --every D`
# loop takes to answer SIGTERM, and CAPTURE THE STATE when it does not.
#
# WHY THIS EXISTS (epic merger-loop-exit-bounded, 2026-09-18):
#
# `gopi merger --stop` (cmd/agent/merger_restart.go) bounds its wait for the
# signalled loop's exit at mergerStopDeliverTimeout (60s) and exits 1 with
# "still holds <dir>/serve.lock after 1m0s" when the loop has not answered.
# The failing runs show a loop that is ALIVE past 60s with
#   - /proc/<pid>/status State: S, SigPnd 0 (the signal was consumed),
#   - SigCgt carrying SIGTERM (the disposition is Go's handler),
#   - every thread parked in futex_wait_queue_me / ep_poll,
#   - a log whose last line is the sleep edge and which never prints the stop
#     line — i.e. the loop's OWN shutdown never ran.
#
# THIS RIG DOES NOT GENERATE LOAD (GOPI.md Rule 27 — no dd/oflag=direct, no
# stress-ng, no parallel writers, no CPU saturation). It OBSERVES the host it is
# given: it starts one loop, signals it, and times the answer. On a busy host it
# reproduces the hang; on an idle host the same loop exits in well under a
# second — that difference IS the measurement, and it is taken from observing
# the loop, never from manufacturing contention.
#
# RULE 24: the loop under test is identified by ITS OWN profile's lock file
# (<home>/.gopi/serve.lock) and by the exact pid this script forked — never by
# `pgrep -f "gopi merger --every"`, which on this host matches 70-minute-old
# loops from other tasks.
#
# USAGE:  scripts/merger-loop-exit-rig.sh [--binary PATH] [--every D] [--budget S]
# EXIT:   0 = the loop answered within the budget; 1 = it did not (evidence kept)

set -u

BIN_DEFAULT="$(cd "$(dirname "$0")/.." && pwd)/bin/gopi"
BIN="$BIN_DEFAULT"
EVERY="30s"
BUDGET=90
# POLL_CAP bounds the SIGTERM-wait loop by ITERATION as well as by $BUDGET (Rule
# 22/Rule 27: a wall-clock guard is not a bound when one iteration can hang).
# 90s / 0.05s = 1800 polls, so 4000 cannot truncate a legitimate wait.
POLL_CAP=4000
while [ $# -gt 0 ]; do
  case "$1" in
    --binary) BIN="$2"; shift 2 ;;
    --every)  EVERY="$2"; shift 2 ;;
    --budget) BUDGET="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

# The observation workspace: an ordinary directory under the operator's
# workspace (never /tmp — GOPI.md Rule 22 states the criterion; the workspace is
# the operator's stated working space).
WS="${GOPI_RIG_WORKSPACE:-$HOME/workspace/gopi-test-scratch/merger-loop-exit-rig}"
OUT="$WS/$(date +%s)-$$"
HOME_DIR="$OUT/home"
mkdir -p "$HOME_DIR/.gopi" "$OUT"
echo '{"repos":{}}' > "$HOME_DIR/.gopi/settings.json"

LOG="$OUT/loop.log"
: > "$LOG"

# THE LOOP'S OWN ENVIRONMENT: a private profile, an empty repo table (so a tick
# has nothing to touch and the ONLY thing measured is the loop's exit path), no
# peers, no broadcast.
env -i \
  PATH="$PATH" \
  HOME="$HOME_DIR" \
  GOPI_HOME="$HOME_DIR" \
  GOPI_SETTINGS="$HOME_DIR/.gopi/settings.json" \
  GOPI_MEMORY_DB="$HOME_DIR/memory.db" \
  GOPI_COMPUTE_NODES= \
  GOPI_SYNC_PEERS= \
  "$BIN" merger --every "$EVERY" >>"$LOG" 2>&1 &
PID=$!
echo "rig: pid=$PID bin=$BIN every=$EVERY budget=${BUDGET}s out=$OUT"

# Wait for the loop's own banner — never a sleep guess (its absence must be an
# error, not a silent proceed).
for _ in $(seq 1 200); do
  if grep -q "running the tick every" "$LOG" 2>/dev/null; then break; fi
  if ! kill -0 "$PID" 2>/dev/null; then
    echo "rig: the loop exited before its banner — see $LOG" >&2
    sed -n '1,40p' "$LOG" >&2
    exit 2
  fi
  sleep 0.05
done

LOCK="$HOME_DIR/.gopi/serve.lock"
echo "rig: lock=$LOCK holder=$(cat "$LOCK" 2>/dev/null | tr -d '\n')"

# THE MEASUREMENT: the signalled pid's exit, on the wall clock.
T0=$(date +%s.%N)
kill -TERM "$PID"
ANSWERED=0
# The poll is bounded TWICE — by $BUDGET on the wall clock and by
# POLL_CAP on the iteration count. The iteration cap is the one Rule 27/Rule 22
# require: a wall-clock guard alone is not a bound if an iteration can hang
# (`date`, `awk`, a stuck child), and the 2026-09-11 exhaustion was exactly one
# unpinned loop. POLL_CAP × the 0.05s sleep is well under any allowed budget.
for ((POLL=0; POLL<POLL_CAP; POLL++)); do
  if ! kill -0 "$PID" 2>/dev/null; then ANSWERED=1; break; fi
  # a zombie counts as GONE (the loop's work is over; only this script's wait remains)
  ST=$(awk '{print $3}' "/proc/$PID/stat" 2>/dev/null)
  if [ "$ST" = "Z" ]; then ANSWERED=1; break; fi
  ELAPSED=$(awk -v a="$T0" -v b="$(date +%s.%N)" 'BEGIN{printf "%.2f", b-a}')
  if awk -v e="$ELAPSED" -v b="$BUDGET" 'BEGIN{exit !(e>b)}'; then break; fi
  sleep 0.05
done
T1=$(date +%s.%N)
WALL=$(awk -v a="$T0" -v b="$T1" 'BEGIN{printf "%.2f", b-a}')

if [ "$ANSWERED" = "1" ]; then
  echo "rig: THE LOOP ANSWERED SIGTERM in ${WALL}s"
  tail -3 "$LOG"
  wait "$PID" 2>/dev/null
  exit 0
fi

# --- THE HANG: capture the state, then dump the goroutines ------------------
echo "rig: *** THE LOOP DID NOT ANSWER within ${BUDGET}s (wall=${WALL}s) ***"
{
  echo "=== /proc/$PID/status (state / pending / caught) ==="
  grep -E '^(State|SigPnd|ShdPnd|SigBlk|SigIgn|SigCgt|Threads):' "/proc/$PID/status" 2>/dev/null
  echo "=== wchan per thread ==="
  for t in /proc/$PID/task/*; do
    printf '%s %s\n' "$(basename "$t")" "$(cat "$t/wchan" 2>/dev/null)"
  done | sort | uniq -c -f1 | sort -rn | head -10
  echo "=== log tail ==="
  tail -5 "$LOG"
} | tee "$OUT/hang-state.txt" >&2

# SIGQUIT dumps every goroutine with GOTRACEBACK=all — the exact blocking call.
kill -QUIT "$PID" 2>/dev/null
for _ in $(seq 1 100); do
  if ! kill -0 "$PID" 2>/dev/null; then break; fi
  sleep 0.05
done
tail -c 20000 "$LOG" > "$OUT/goroutine-dump.txt" 2>/dev/null || true
kill -0 "$PID" 2>/dev/null && kill -KILL "$PID" 2>/dev/null
wait "$PID" 2>/dev/null
echo "rig: goroutine dump -> $OUT/goroutine-dump.txt"
grep -n "goroutine [0-9]* \[" "$OUT/goroutine-dump.txt" | head -20
exit 1
