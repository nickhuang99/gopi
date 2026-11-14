#!/usr/bin/env bash
# capture.sh — drive the REAL `gopi ui` under a pty and capture its own screen
# dump WHILE THE SURFACE IS LIVE.
#
#   usage:  GOPI_UI_SANDBOX=<sandbox dir> capture.sh /path/to/gopi
#
# WHY "LIVE" MATTERS, and it is not a detail: bubbletea wipes its own panel on
# exit (it rewinds to the top of the panel and emits `CSI J`), so the dump a
# reader finds AFTER the process is gone describes a screen the terminal has
# already cleared — honest, and useless as evidence of a layout. An agent that
# observes a running surface reads the file WHILE IT RUNS; this script does
# exactly that, and keeps both artifacts:
#
#   ui-screen.dump.live    the dump of the painted screen (the evidence)
#   ui-screen.dump.live.json  its machine-checkable twin
#   pty-transcript.txt     the raw byte stream the surface wrote (the cost
#                          comparison's "pixel path" baseline is a grab of the
#                          whole display; this is the text the dump replaces)
#
# The sandbox comes from `gopi sandbox create` (Rule 34) — never a hand-built
# directory. Nothing is written outside $HERE and the sandbox.

set -u

HERE=$(cd "$(dirname "$0")" && pwd)
BIN=${1:?usage: capture.sh /path/to/gopi}
SB=${GOPI_UI_SANDBOX:?set GOPI_UI_SANDBOX to a directory from: gopi sandbox create}

DUMP="$HERE/ui-screen.dump"
TRANSCRIPT="$HERE/pty-transcript.txt"

# A STALE dump must not be mistaken for a painted one: the readiness check below
# reads the sink's own frame counter, and last run's file has a non-zero one.
# (Measured: without this, "painted after 0s" and the captured dump was the
# PREVIOUS run's — an evidence file that quietly describes another run.)
rm -f "$DUMP" "$DUMP.json" "$DUMP.live" "$DUMP.live.json"

# The terminal's geometry, given to the pty explicitly: `script` otherwise
# passes the caller's, and the layout under test must be the one the dump
# reports.
ROWS=30
COLS=100
# READY MARKER, then a bounded wait. A fixed sleep is wrong here: the surface
# paints its first frame only after the in-process engine is up (loading
# GOPI.md, opening the session, the banner), which took ~15s in the sandbox this
# script was written in — a 10s sleep captured an EMPTY dump (frames=0) while
# the terminal was already showing a layout. So the wait is on the SUBJECT: the
# dump's own frame counter going non-zero.
# The budget is the MEASURED startup, not a guess: in a cold sandbox
# (a fresh GOPI_HOME, an empty SQLite ledger) the surface's first painted
# frame was 45 seconds after launch — engine construction, the GOPI.md load,
# ledger init and the banner all precede the first draw. Measured: a 40s
# budget captured an EMPTY dump (frames=0) on every run.
READY_TIMEOUT=100
# A q is sent to the pty instead: it is the TUI's own quit key, so the surface
# leaves cleanly and `script` flushes the transcript. (Measured: a run that was
# SIGKILLed left a ZERO-byte transcript — `script` writes its typescript on
# exit, so killing it loses the very bytes the cost comparison needs.)
KILL_AFTER=$((READY_TIMEOUT + 25))

# The input side is SELF-SYNCHRONIZING: it waits for the evidence file the
# capture step below produces, so the quit key is sent only once the surface has
# genuinely painted — a fixed sleep would send it into the engine's banner on a
# slow start and never reach the TUI.
{
  while [ ! -s "$DUMP.live" ] && [ "$(date +%s)" -lt "$(( $(date +%s) + KILL_AFTER ))" ]; do sleep 1; done
  sleep 1
  printf 'q'
  sleep 10
} | timeout "$KILL_AFTER" env \
  GOPI_HOME="$SB" GOPI_SETTINGS="$SB/settings.json" \
  GOPI_UI_SELFTEST=1 TERM=xterm-256color \
  script -q -c "stty rows $ROWS cols $COLS; GOPI_HOME=$SB GOPI_SETTINGS=$SB/settings.json GOPI_UI_SELFTEST=1 $BIN ui --addr 127.0.0.1:0 --dump-screen=$DUMP" /dev/null \
  >"$TRANSCRIPT" 2>&1 &
BG=$!

# Wait for the surface to have PAINTED: the sink reports how many render writes
# it interpreted, and zero means the terminal is still showing the engine's
# banner rather than the TUI.
waited=0
frames=""
while [ "$waited" -lt "$READY_TIMEOUT" ]; do
  if [ -f "$DUMP" ]; then
    frames=$(head -1 "$DUMP" | sed -n 's/.*[^0-9]frames=\([0-9][0-9]*\).*/\1/p')
    if [ -n "$frames" ] && [ "$frames" -gt 0 ]; then
      break
    fi
  fi
  sleep 1
  waited=$((waited + 1))
done
echo "painted after ${waited}s (frames=${frames:-unknown})"

cp "$DUMP" "$DUMP.live"
cp "$DUMP.json" "$DUMP.live.json"

echo "live dump:  $DUMP.live ($(wc -c <"$DUMP.live") bytes)"
echo "live json:  $DUMP.live.json ($(wc -c <"$DUMP.live.json") bytes)"
echo "transcript: $TRANSCRIPT ($(wc -c <"$TRANSCRIPT") bytes)"

wait "$BG"
echo "run ended"
