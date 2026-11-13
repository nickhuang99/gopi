#!/usr/bin/env bash
# merger-loop-signal-window.sh — MEASURE the window in which a resident merger
# loop cannot answer SIGTERM, by signalling it at growing delays and timing each
# answer.
#
# THE QUESTION, stated as a measurement (GOPI.md Rule 27: measure the implicated
# call, never manufacture load): `merger --stop` signals the lock's stamped pid
# with SIGTERM and waits mergerStopDeliverTimeout (60s) for the flock to go free.
# For that ladder to work, the signal must reach the LOOP's own handler. Two
# ordered facts decide whether it does:
#
#   T_stamp   the moment the loop's pid appears in <home>/.gopi/serve.lock
#             (acquireServeLock completed — the instant `--stop` decides to
#             signal this pid), and
#   T_armed   the moment the loop's stop handler exists, i.e. signal.Notify has
#             run — which mergerResidentLoop does AFTER the lock is taken and
#             AFTER attachHostLifecycleLog.
#
# So this script times δ = T_armed - T_stamp (observed as the banner line, which
# mergerResidentLoop prints immediately after its signal.Notify) and then
# signals at a chosen delay, reporting whether the loop answered, and how long
# the answer took.
#
# NO LOAD IS GENERATED: one loop per trial, one signal, /proc reads.
#
# USAGE: scripts/merger-loop-signal-window.sh [--binary PATH] [--every D]
#                                             [--delay S] [--trials N]

set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$ROOT/bin/gopi"
EVERY="30s"
DELAY=0.0
TRIALS=1
while [ $# -gt 0 ]; do
  case "$1" in
    --binary) BIN="$2"; shift 2 ;;
    --every)  EVERY="$2"; shift 2 ;;
    --delay)  DELAY="$2"; shift 2 ;;
    --trials) TRIALS="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

WS="${GOPI_RIG_WORKSPACE:-$HOME/workspace/gopi-test-scratch}/merger-loop-signal-window/$(date +%s)"
mkdir -p "$WS"
echo "window: bin=$BIN every=$EVERY delay=${DELAY}s trials=$TRIALS out=$WS"

BANNER="running the tick every"

for i in $(seq 1 "$TRIALS"); do
  D="$WS/trial$i"; mkdir -p "$D/home/.gopi"
  echo '{"repos":{}}' > "$D/home/.gopi/settings.json"
  LOG="$D/loop.log"; : > "$LOG"
  LOCK="$D/home/.gopi/serve.lock"

  env -i PATH="$PATH" HOME="$D/home" GOPI_HOME="$D/home" \
    GOPI_SETTINGS="$D/home/.gopi/settings.json" GOPI_MEMORY_DB="$D/home/memory.db" \
    GOPI_COMPUTE_NODES= GOPI_SYNC_PEERS= \
    "$BIN" merger --every "$EVERY" >>"$LOG" 2>&1 &
  PID=$!

  # T_stamp: first moment the lock file carries this loop's pid.
  T0=$(date +%s.%N); T_STAMP=""
  for _ in $(seq 1 3000); do
    if [ "$(head -c 20 "$LOCK" 2>/dev/null | tr -d '\n' )" = "$PID" ]; then
      T_STAMP=$(date +%s.%N); break
    fi
    kill -0 "$PID" 2>/dev/null || break
    sleep 0.005
  done

  # T_armed: the banner, printed one line after signal.Notify.
  T_ARMED=""
  for _ in $(seq 1 6000); do
    if grep -q "$BANNER" "$LOG" 2>/dev/null; then T_ARMED=$(date +%s.%N); break; fi
    kill -0 "$PID" 2>/dev/null || break
    sleep 0.005
  done

  if [ -z "$T_STAMP" ] || [ -z "$T_ARMED" ]; then
    echo "trial$i: could not observe both edges (stamp='${T_STAMP:-none}' armed='${T_ARMED:-none}')" >&2
    kill -KILL "$PID" 2>/dev/null; wait "$PID" 2>/dev/null; continue
  fi
  GAP=$(awk -v a="$T_STAMP" -v b="$T_ARMED" 'BEGIN{printf "%.3f", b-a}')
  echo "trial$i: T_armed - T_stamp = ${GAP}s   (stamp measured at $(awk -v a="$T0" -v b="$T_STAMP" 'BEGIN{printf "%.3f", b-a}')s after spawn)"

  # Signal at T_stamp + DELAY — i.e. possibly INSIDE the window.
  if awk -v d="$DELAY" -v g="$GAP" 'BEGIN{exit !(d<g)}'; then
    echo "trial$i: ${DELAY}s after T_stamp is INSIDE the window (armed at ${GAP}s) — the signal may be consumed before the loop's handler exists"
  fi
  NOW=$(date +%s.%N)
  WAIT=$(awk -v n="$NOW" -v s="$T_STAMP" -v d="$DELAY" 'BEGIN{g=n-s-d; if(g<0)g=0; printf "%.3f", g}')
  sleep "$WAIT"
  TSIG=$(date +%s.%N)
  kill -TERM "$PID" 2>/dev/null
  ANSWERED=""; TANS=""
  for _ in $(seq 1 6000); do
    if ! kill -0 "$PID" 2>/dev/null; then ANSWERED=yes; TANS=$(date +%s.%N); break; fi
    ST=$(awk '{print $3}' "/proc/$PID/stat" 2>/dev/null)
    [ "$ST" = "Z" ] && { ANSWERED=yes; TANS=$(date +%s.%N); break; }
    sleep 0.01
  done
  if [ -n "$ANSWERED" ]; then
    W=$(awk -v a="$TSIG" -v b="$TANS" 'BEGIN{printf "%.2f", b-a}')
    echo "trial$i: ANSWERED in ${W}s; stop line: $(grep -c 'terminated received' "$LOG")"
    wait "$PID" 2>/dev/null
  else
    echo "trial$i: *** DID NOT ANSWER in 60s *** (log tail / last line)"
    tail -3 "$LOG"
    kill -KILL "$PID" 2>/dev/null; wait "$PID" 2>/dev/null
  fi
done
