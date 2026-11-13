#!/usr/bin/env bash
# merger-loop-hang-capture.sh — run the failing E2E case and, while the loop is
# hung, capture the evidence that names the blocking call.
#
# WHY A CAPTURE SCRIPT AND NOT A TEST ASSERTION: the state we need (the thread's
# kernel wait channel, the pending/caught signal masks, the Go goroutine dump
# naming the blocking call) exists only while the loop is alive — a test that
# fails afterwards can only report its timeout. So the case runs in the
# background and this script samples the process it started.
#
# IDENTIFICATION (GOPI.md Rule 24): the loop is found by the EXACT BINARY PATH it
# executes (/proc/<pid>/exe) together with the test-binary parentage — never by
# `pgrep -f "gopi merger --every"`, which on this host matches 70-minute-old
# loops from other tasks.
#
# THE HANG PREDICATE is LOG STALENESS, not liveness: a loop 5s away from the end
# of its own 30s sleep is alive and healthy. The loop prints a sleep line and
# then MUST print the next tick boundary within interval + one tick's work, so a
# log that has not moved for STALE_BOUND seconds is provably past the interval
# the loop itself announced.
#
# NO LOAD IS GENERATED (GOPI.md Rule 27): the script only reads /proc and sends
# signals.
#
# USAGE: scripts/merger-loop-hang-capture.sh [--case NAME] [--secs S] [--out DIR]
# EXIT: 0 = hang captured; 3 = no hang in the window (not an error of the rig)

set -u

CASE="TestMergerStopEndsALiveLoop"
SECS=120
STALE_BOUND=45

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$ROOT/bin/gopi"
OUT="${GOPI_RIG_WORKSPACE:-$HOME/workspace/gopi-test-scratch}/merger-loop-exit-capture/$(date +%s)"
PKG=./cmd/agent/
while [ $# -gt 0 ]; do
  case "$1" in
    --case) CASE="$2"; shift 2 ;;
    --secs) SECS="$2"; shift 2 ;;
    --out)  OUT="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
mkdir -p "$OUT"
echo "capture: case=$CASE bin=$BIN stale_bound=${STALE_BOUND}s window=${SECS}s out=$OUT"

# `-count=1` so a cached PASS can never stand in for a live observation.
GOPI_TEST_BIN="$BIN" go test "$PKG" -run "$CASE" -count=1 -v -timeout 15m \
  > "$OUT/test.log" 2>&1 &
TESTPID=$!
echo "capture: go test pid=$TESTPID"

# --- find the loop by exact exe path + its parentage ------------------------
loop_pid=""
for _ in $(seq 1 1200); do
  for p in $(ls /proc 2>/dev/null | grep -E '^[0-9]+$'); do
    [ "$(readlink "/proc/$p/exe" 2>/dev/null)" = "$BIN" ] || continue
    ppid=$(awk '{print $4}' "/proc/$p/stat" 2>/dev/null)
    [ -n "$ppid" ] || continue
    if tr '\0' ' ' < "/proc/$ppid/cmdline" 2>/dev/null | grep -q -- "-test.run=$CASE\|-test.paniconexit0"; then
      loop_pid=$p
      break
    fi
  done
  [ -n "$loop_pid" ] && break
  kill -0 "$TESTPID" 2>/dev/null || { echo "capture: go test exited before a loop appeared"; break; }
  sleep 0.1
done

if [ -z "$loop_pid" ]; then
  echo "capture: NO loop found by exact exe path — see $OUT/test.log" >&2
  tail -30 "$OUT/test.log" >&2
  exit 2
fi

{
  echo "loop_pid=$loop_pid"
  echo "argv: $(tr '\0' ' ' < /proc/$loop_pid/cmdline)"
  echo "exe: $(readlink /proc/$loop_pid/exe)"
  echo "cwd: $(readlink /proc/$loop_pid/cwd)"
  echo "stdout: $(readlink /proc/$loop_pid/fd/1)"
  echo "environ:"
  tr '\0' '\n' < /proc/$loop_pid/environ | sort
} > "$OUT/loop-identity.txt" 2>&1
LOOP_LOG="$(readlink "/proc/$loop_pid/fd/1")"
echo "capture: loop pid=$loop_pid log=$LOOP_LOG"

# --- wait for the TRUE HANG -------------------------------------------------
log_mtime() { stat -c %Y "$1" 2>/dev/null || echo 0; }
START=$(date +%s)
hang=0
while :; do
  NOW=$(date +%s)
  if kill -0 "$loop_pid" 2>/dev/null; then
    if grep -q "sleeping " "$LOOP_LOG" 2>/dev/null; then
      STALE=$(( NOW - $(log_mtime "$LOOP_LOG") ))
      if [ "$STALE" -ge "$STALE_BOUND" ]; then
        echo "capture: HANG — the loop is alive and its log is ${STALE}s stale (bound ${STALE_BOUND}s)"
        hang=1
        break
      fi
    fi
  else
    echo "capture: the loop exited before the bound (no hang this run)"
    wait "$TESTPID" 2>/dev/null
    exit 3
  fi
  [ $(( NOW - START )) -ge "$SECS" ] && break
  sleep 0.2
done

if [ "$hang" = "0" ]; then
  kill -KILL "$loop_pid" 2>/dev/null
  wait "$TESTPID" 2>/dev/null
  echo "capture: no hang observed within ${SECS}s — see $OUT/test.log"
  exit 3
fi

# --- ① the state: how the thread is parked, and what the log last said ------
{
  echo "=== captured with the loop HUNG (pid $loop_pid) ==="
  grep -E '^(Name|State|Tgid|Pid|PPid|Threads|SigPnd|ShdPnd|SigBlk|SigIgn|SigCgt|voluntary_ctxt_switches|nonvoluntary_ctxt_switches):' "/proc/$loop_pid/status"
  echo "=== per-thread wchan (kernel wait channel) ==="
  for t in /proc/$loop_pid/task/*; do
    printf '%s\t%s\n' "$(cat "$t/comm" 2>/dev/null)" "$(cat "$t/wchan" 2>/dev/null)"
  done | sort | uniq -c | sort -rn
  echo "=== per-thread syscall+state ==="
  for t in /proc/$loop_pid/task/*; do
    printf '%s\tstate=%s\tsyscall=%s\n' "$(basename "$t")" "$(awk '{print $3}' "$t/stat" 2>/dev/null)" "$(cat "$t/syscall" 2>/dev/null)"
  done | sort -k2
  echo "=== live children of the loop ==="
  ps -o pid,ppid,stat,etimes,args -e 2>/dev/null | awk -v p="$loop_pid" '$2==p'
  echo "=== the loop's own log (tail 8) ==="
  tail -8 "$LOOP_LOG" 2>/dev/null
} > "$OUT/hang-state.txt" 2>&1
cat "$OUT/hang-state.txt"

# --- ② the goroutine dump: the blocking call, in Go terms -------------------
# SIGQUIT makes Go print every goroutine with its stack (GOTRACEBACK's default
# output) to the process's stderr — which this loop's stdout/stderr is. That
# dump names the exact blocking call, with no inference.
kill -QUIT "$loop_pid" 2>/dev/null
for _ in $(seq 1 200); do
  kill -0 "$loop_pid" 2>/dev/null || break
  sleep 0.05
done
kill -KILL "$loop_pid" 2>/dev/null
wait "$loop_pid" 2>/dev/null
tail -c 60000 "$LOOP_LOG" 2>/dev/null > "$OUT/goroutine-dump.txt"
echo "capture: goroutine dump -> $OUT/goroutine-dump.txt"
grep -E '^(goroutine [0-9]+ \[)' "$OUT/goroutine-dump.txt" | head -30

wait "$TESTPID" 2>/dev/null
echo "capture: test log -> $OUT/test.log"
grep -E '^(---|===|ok|FAIL|PASS)' "$OUT/test.log" | tail -10
