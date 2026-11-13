#!/usr/bin/env bash
# merger-loop-sigquit-dump.sh — while the C2 case is failing, dump the HUNG
# loop's goroutines (SIGQUIT) so the blocking call is NAMED, not guessed.
#
# WHY SIGQUIT AND NOT MORE /proc POKING: the loop in the failing shape is ALIVE,
# parked in state S, and — measured by the sampler — still printing its sleep
# lines every interval while NEVER printing the stop line, i.e. it walks past
# `mergerStopRequested`/`mergerSleepOrStop` every 30s without ever seeing the
# signal on its channel. /proc can only say "S / futex". The Go goroutine dump
# says WHICH call each goroutine is in, the position of the `os/signal.loop`
# goroutine, and whether the signal channel has a value — the facts needed to
# name the mechanism.
#
# IDENTIFICATION (GOPI.md Rule 24): EXACT /proc/<pid>/exe path + test-binary
# parentage. Never `pgrep -f`.
# NO LOAD IS GENERATED (Rule 27): /proc reads and two signals.
#
# USAGE: scripts/merger-loop-sigquit-dump.sh [--case NAME] [--at S] [--out DIR]

set -u

CASE="TestMergerStopEndsALiveLoop"
AT=35
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$ROOT/bin/gopi"
OUT="${GOPI_RIG_WORKSPACE:-$HOME/workspace/gopi-test-scratch}/merger-loop-exit-dump/$(date +%s)"
PKG=./cmd/agent/
while [ $# -gt 0 ]; do
  case "$1" in
    --case) CASE="$2"; shift 2 ;;
    --at)   AT="$2"; shift 2 ;;
    --out)  OUT="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
mkdir -p "$OUT"

GOPI_TEST_BIN="$BIN" go test "$PKG" -run "$CASE" -count=1 -v -timeout 15m > "$OUT/test.log" 2>&1 &
TESTPID=$!
echo "dump: go test pid=$TESTPID out=$OUT"

loop_pid=""
for _ in $(seq 1 2400); do
  for p in $(ls /proc 2>/dev/null | grep -E '^[0-9]+$'); do
    [ "$(readlink "/proc/$p/exe" 2>/dev/null)" = "$BIN" ] || continue
    ppid=$(awk '{print $4}' "/proc/$p/stat" 2>/dev/null)
    [ -n "$ppid" ] || continue
    if tr '\0' ' ' < "/proc/$ppid/cmdline" 2>/dev/null | grep -q -- "-test.run=$CASE\|-test.paniconexit0"; then
      loop_pid=$p; break
    fi
  done
  [ -n "$loop_pid" ] && break
  kill -0 "$TESTPID" 2>/dev/null || break
  sleep 0.05
done
[ -z "$loop_pid" ] && { echo "dump: no loop found"; wait "$TESTPID"; exit 2; }

LOOP_LOG=$(readlink "/proc/$loop_pid/fd/1")
echo "dump: loop=$loop_pid log=$LOOP_LOG"
{
  echo "argv: $(tr '\0' ' ' < /proc/$loop_pid/cmdline)"
  echo "exe: $(readlink /proc/$loop_pid/exe)"
  echo "started_at_monotonic_ns=$(awk '{print $22}' /proc/$loop_pid/stat)"
} > "$OUT/identity.txt"

# Wait until the stop has certainly been requested (the case sends it ~1-2s
# after the loop takes the lock) and then dump — the loop is expected to be
# mid-interval or mid-tick, which is the state to explain.
sleep "$AT"
if ! kill -0 "$loop_pid" 2>/dev/null; then
  echo "dump: the loop already exited before t+${AT}s (no hang this run)"
  wait "$TESTPID" 2>/dev/null
  exit 3
fi

{
  echo "=== state at t+${AT}s (pid $loop_pid) ==="
  grep -E '^(State|Threads|SigPnd|ShdPnd|SigBlk|SigIgn|SigCgt|voluntary_ctxt_switches|nonvoluntary_ctxt_switches):' "/proc/$loop_pid/status"
  echo "=== pending signals, per thread (all threads) ==="
  for t in /proc/$loop_pid/task/*; do
    printf '%s\tSigPnd=%s\tSigBlk=%s\twchan=%s\n' \
      "$(basename "$t")" \
      "$(grep -m1 '^SigPnd:' "$t/status" 2>/dev/null | tr -d ' ')" \
      "$(grep -m1 '^SigBlk:' "$t/status" 2>/dev/null | tr -d ' ')" \
      "$(cat "$t/wchan" 2>/dev/null)"
  done
  echo "=== log tail ==="
  tail -4 "$LOOP_LOG"
} > "$OUT/state.txt" 2>&1
cat "$OUT/state.txt"

# The goroutine dump goes to the loop's stderr, which is the case's loop.log.
kill -QUIT "$loop_pid" 2>/dev/null
for _ in $(seq 1 200); do kill -0 "$loop_pid" 2>/dev/null || break; sleep 0.05; done
kill -KILL "$loop_pid" 2>/dev/null
wait "$loop_pid" 2>/dev/null
cp "$LOOP_LOG" "$OUT/loop.log" 2>/dev/null
grep -n "^goroutine \|SIGQUIT\|^created by" "$OUT/loop.log" 2>/dev/null | tail -40 > "$OUT/goroutines.txt"
echo "dump: goroutine summary -> $OUT/goroutines.txt"
cat "$OUT/goroutines.txt"

wait "$TESTPID" 2>/dev/null
grep -E '^(---|ok|FAIL|PASS)' "$OUT/test.log" | tail -5
