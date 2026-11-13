#!/usr/bin/env bash
# merger-loop-hang-sample.sh — run the failing E2E case while SAMPLING, at 100ms
# resolution, the two things the stop-side failure conflates:
#
#   ① the LOOP's own progress (is it alive, has its log moved, which kernel call
#      is each thread parked in),
#   ② the FLOCK state (is the lock actually flocked, and by WHOSE file descriptor).
#
# WHY BOTH, AND WHY CONTINUOUSLY. A single sample cannot tell the two candidate
# mechanisms apart:
#
#   (a) the loop never reaches its tick boundary, so its own shutdown never runs;
#   (b) the loop exits promptly, but the flock outlives it — held by another open
#       file description — so a stop that observes "the flock went free" waits
#       for its whole budget.
#
# Mechanism (b) was already established for a take-over that inherits the lock
# (memory: merger --stop observing the flock; the epic's stop-side fix). This rig
# exists to find out WHICH one is live in THIS failure, with evidence rather than
# inference — which is why it samples the lock's fd OWNERSHIP continuously
# instead of reading it once after the fact.
#
# NO LOAD IS GENERATED (GOPI.md Rule 27): /proc reads, `flock -n` probes (a
# non-blocking lock attempt, no I/O) and one script-level sleep cadence.
#
# USAGE: scripts/merger-loop-hang-sample.sh [--case NAME] [--secs S]

set -u

CASE="TestMergerStopEndsALiveLoop"
SECS=180
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$ROOT/bin/gopi"
OUT="${GOPI_RIG_WORKSPACE:-$HOME/workspace/gopi-test-scratch}/merger-loop-exit-sample/$(date +%s)"
PKG=./cmd/agent/
while [ $# -gt 0 ]; do
  case "$1" in
    --case) CASE="$2"; shift 2 ;;
    --secs) SECS="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
mkdir -p "$OUT"
echo "sample: case=$CASE out=$OUT"

GOPI_TEST_BIN="$BIN" go test "$PKG" -run "$CASE" -count=1 -v -timeout 15m \
  > "$OUT/test.log" 2>&1 &
TESTPID=$!

# --- find the loop by EXACT exe path + test-binary parentage (Rule 24) ------
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
if [ -z "$loop_pid" ]; then echo "sample: no loop found"; wait "$TESTPID"; exit 2; fi

# The loop's OWN profile is the lock's home: read GOPI_HOME out of its environ
# (never a guessed path).
LOOP_HOME=$(tr '\0' '\n' < "/proc/$loop_pid/environ" | sed -n 's/^GOPI_HOME=//p')
LOCK="${LOOP_HOME}/.gopi/serve.lock"
LOOP_LOG=$(readlink "/proc/$loop_pid/fd/1")
echo "sample: loop=$loop_pid home=$LOOP_HOME lock=$LOCK log=$LOOP_LOG"
{ echo "loop_pid=$loop_pid"; echo "argv: $(tr '\0' ' ' < /proc/$loop_pid/cmdline)"; } > "$OUT/identity.txt"

# --- the sampler ------------------------------------------------------------
TSV="$OUT/sample.tsv"
echo -e "t\tloop_alive\tloop_state\tlog_mtime\tlog_last\tsleep_seen\tlock_file_pid\tflock_free\tholders" > "$TSV"

# flockFree asks the kernel whether the lock can be taken right now. `flock -n`
# on a file whose flock is held by ANY open file description (including one
# inherited by a child) is REFUSED — the answer the stop-side wait was built on.
flock_free() { flock -n "$LOCK" -c true 2>/dev/null && echo yes || echo no; }

# lock_holders lists every process holding a descriptor on the lock file — the
# exhaustive form of "who keeps the flock alive" (fdinfo read, no lock taken).
lock_holders() {
  local inode
  inode=$(stat -c %i "$LOCK" 2>/dev/null) || { echo -; return; }
  local out=""
  for d in /proc/[0-9]*; do
    local p=${d#/proc/} fd
    for fd in "$d"/fd/*; do
      [ -e "$fd" ] || continue
      if [ "$(stat -L -c %i "$fd" 2>/dev/null)" = "$inode" ]; then
        out="${out}${p}:${fd##*/}($(cat "$d/comm" 2>/dev/null)) "
      fi
    done
  done
  [ -n "$out" ] && echo "$out" || echo -
}

T0=$(date +%s.%N)
while kill -0 "$TESTPID" 2>/dev/null; do
  T=$(awk -v a="$T0" -v b="$(date +%s.%N)" 'BEGIN{printf "%.1f", b-a}')
  if kill -0 "$loop_pid" 2>/dev/null; then
    ST=$(awk '{print $3}' "/proc/$loop_pid/stat" 2>/dev/null)
    LM=$(stat -c %Y "$LOCK" 2>/dev/null || echo 0)
    LMT=$(stat -c %Y "$LOOP_LOG" 2>/dev/null || echo 0)
    LAST=$(tail -1 "$LOOP_LOG" 2>/dev/null | tr '\t' ' ' | cut -c1-60)
    if grep -q "sleeping " "$LOOP_LOG" 2>/dev/null; then SLEEP=yes; else SLEEP=no; fi
    FPID=$(head -c 40 "$LOCK" 2>/dev/null | tr -d '\n')
    printf '%s\talive\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
      "$T" "${ST:-?}" "$LMT" "$LAST" "$SLEEP" "${FPID:-?}" "$(flock_free)" "$(lock_holders)" >> "$TSV"
  else
    printf '%s\tGONE\t-\t-\t-\t-\t%s\t%s\t%s\n' "$T" "$(head -c 40 "$LOCK" 2>/dev/null | tr -d '\n')" "$(flock_free)" "$(lock_holders)" >> "$TSV"
    break
  fi
  sleep 0.1
done

wait "$TESTPID" 2>/dev/null
echo "sample: test log -> $OUT/test.log"
grep -E '^(---|===|ok|FAIL|PASS)' "$OUT/test.log" | tail -6
echo "sample: samples -> $TSV  ($(wc -l < "$TSV") rows)"
# The transitions only: one line per distinct (alive/state/flock/last-line)
# combination, so the sequence is readable without 600 identical rows.
awk -F'\t' 'NR>1{k=$2"|"$3"|"$8"|"$9; if(k!=p){print; p=k}}' "$TSV" | cut -c1-200
