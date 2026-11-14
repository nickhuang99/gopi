#!/usr/bin/env bash
# capture-help.sh — the ONE command that reproduces the `/help` layout defect
# (and, on a fixed checkout, its absence) with the committed SGR-aware
# instrument.
#
# Why it exists: capture.sh's scripted shot set is FIXED (four shots, none of
# them a `/help` state — see the note in capture.sh's script: on master the help
# panel is painted interleaved with the live status row, so a "-4-help" caption
# could not be anchored on any panel substring). This script is that missing
# shot: it types a `/help` line, submits it, and photographs the screen AFTER
# the panel is up.
#
# Usage:
#   capture-help.sh <label> <binary> [turn]
#     label    before | after | anything — the prefix of the emitted files
#     binary   the gopi binary to run (build it from the checkout under test)
#     turn     the submitted line (default `/help quit`; use `/help` for the
#              full panel, which is taller than the band)
#
# Output (next to this script):
#   <label>-ui-5-help.grid.txt       the screen after the panel, one line per row
#   <label>-ui-5-help.grid.attr.txt  the same screen plus per-cell attribute runs
#   <label>-ui-5-help.raw.txt        the exact PTY byte stream
#
# ENV SCRUB is the same contract capture.sh states: every API key and base URL
# is unset, GOPI_HOME points at a THROWAWAY tree, and LANG/LC_ALL are C.utf8
# (a C locale makes `·` and `⚙` mojibake). Rule 22: the throwaway tree lives
# under the workspace, never on /.
set -uo pipefail

LABEL="${1:?usage: capture-help.sh <label> <binary> [turn]}"
BIN="${2:?usage: capture-help.sh <label> <binary> [turn]}"
TURN="${3:-/help quit}"
EVID="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

COLS="${COLS:-100}"
ROWS="${ROWS:-30}"
WORK="${WORK:-$HOME/workspace/_tmp/sgr-help-capture-$LABEL}"
mkdir -p "$WORK" || exit 1
[ -x "$BIN" ] || { echo "FAIL: no executable at $BIN" >&2; exit 1; }

# The instrument proves itself before it observes anything: a capture from an
# attribute-blind emulator would make every colour claim below a guess.
python3 "$EVID/termgrid.py" --selftest >"$WORK/selftest.log" 2>&1 || {
    echo "FAIL: termgrid --selftest failed (see $WORK/selftest.log)" >&2; exit 1; }

SCRIPT="$WORK/script-help.txt"
cat >"$SCRIPT" <<SCRIPT_EOF
sleep 2.5
waitfor gopi chat
type $TURN
submit
sleep 2.0
shot ui-5-help
exit
SCRIPT_EOF

cd "$WORK" || exit 1
rm -rf "$WORK/home"
env -u DEEPSEEK_API_KEY -u DEEPSEEK_BASE_URL -u DEEPSEEK_MODEL \
    -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN -u ANTHROPIC_BASE_URL -u ANTHROPIC_MODEL \
    -u GOPI_TASK_REPORT_PATH -u GOPI_TASK_REPORT_OWNER -u GOPI_EXECUTOR_SESSION_ID \
    -u GOPI_MEMORY_DB \
    "GOPI_HOME=$WORK/home" "GOPI_HUMAN_TERMINAL=1" \
    "LANG=C.utf8" "LC_ALL=C.utf8" "TERM=xterm-256color" \
    python3 "$EVID/termgrid.py" "$BIN" "$COLS" "$ROWS" "$SCRIPT" \
    >"$WORK/grid-$LABEL.log" 2>&1
echo "grid log: $WORK/grid-$LABEL.log ($(tail -1 "$WORK/grid-$LABEL.log"))"

for f in ui-5-help; do
    cp -f "$WORK/$f.grid.txt"      "$EVID/$LABEL-$f.grid.txt"      2>/dev/null
    cp -f "$WORK/$f.grid.attr.txt" "$EVID/$LABEL-$f.grid.attr.txt" 2>/dev/null
    cp -f "$WORK/$f.raw.txt"       "$EVID/$LABEL-$f.raw.txt"       2>/dev/null
done
ls -1 "$EVID/$LABEL"-ui-5-help.* 2>/dev/null || {
    echo "FAIL: no shot was written — the run never reached the shot op" >&2; exit 1; }
