#!/usr/bin/env bash
# capture-flowwrap.sh — the ONE command that photographs an OVER-WIDE AGENT FLOW
# ROW arriving at the reserved layout's band, and how it wraps.
#
# Why it exists: capture-flow.sh's shot set is driven by the REAL reasoning
# endpoint (it needs a live model and a clamped-budget notice), and capture.sh /
# capture-help.sh photograph the answer and the /help panel. None of them
# produces the state this residual lives in: ONE agent flow row (the `[thinking]
# …` reasoning line) wider than the band, so the band's own row loop has to
# decide where it breaks.
#
# Usage:
#   capture-flowwrap.sh <label> <binary>
#     label    before | after | anything — the prefix of the emitted files
#     binary   the gopi binary to run (build it from the checkout under test)
#
# Output (next to this script):
#   <label>-ui-6-flowwrap.grid.txt       the screen with the over-wide flow row
#   <label>-ui-6-flowwrap.grid.attr.txt  the same, plus per-cell attributes
#   <label>-ui-6-flowwrap.raw.txt        the exact PTY byte stream
#
# The model is OFFLINE and DETERMINISTIC: flowwrapmock.py speaks the same
# Anthropic-SSE wire format as fakellm.py but streams a reasoning block of a known
# LENGTH — long enough to wrap several times at 100 columns — so the shot is
# anchored on the mock's own text rather than on a model's choice of words, and
# the run costs no API spend and touches no network beyond 127.0.0.1.
#
# ENV SCRUB is capture.sh's contract: every API key and base URL is unset and
# repointed at the local stub, GOPI_HOME points at a THROWAWAY tree (never the
# operator's ~/.gopi), LANG/LC_ALL are C.utf8 (`·` and `⚙` are mojibake under a C
# locale), and the throwaway tree lives under the workspace, never on / (Rule 22).
set -uo pipefail

LABEL="${1:?usage: capture-flowwrap.sh <label> <binary>}"
BIN="${2:?usage: capture-flowwrap.sh <label> <binary>}"
EVID="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

COLS="${COLS:-100}"
ROWS="${ROWS:-30}"
PORT="${FLOWWRAP_PORT:-8793}"
WORK="${WORK:-$HOME/workspace/_tmp/sgr-flowwrap-capture-$LABEL}"
mkdir -p "$WORK" || exit 1
[ -x "$BIN" ] || { echo "FAIL: no executable at $BIN" >&2; exit 1; }

# The instrument proves itself before it observes anything: a capture from an
# attribute-blind emulator would make every reading below a guess.
python3 "$EVID/termgrid.py" --selftest >"$WORK/selftest.log" 2>&1 || {
    echo "FAIL: termgrid --selftest failed (see $WORK/selftest.log)" >&2; exit 1; }

MARK="$WORK/mock-mark-$LABEL"
rm -f "$MARK"
python3 "$EVID/flowwrapmock.py" "$PORT" "$MARK" >"$WORK/mock-$LABEL.log" 2>&1 &
MOCK_PID=$!
cleanup() { kill "${MOCK_PID:-0}" 2>/dev/null; return 0; }
trap cleanup EXIT
sleep 0.5
kill -0 "$MOCK_PID" 2>/dev/null || {
    echo "FAIL: the mock model died at startup (see $WORK/mock-$LABEL.log) — the capture would photograph an app waiting for a model that never answers" >&2
    exit 1; }

SCRIPT="$WORK/script-flowwrap.txt"
cat >"$SCRIPT" <<SCRIPT_EOF
sleep 2.5
waitfor gopi chat
type hello world
submit
# The anchor is the mock's OWN reasoning text (an observed flow row on screen),
# never a guessed sleep: "flowwrap" is the stub's fill token, and by the time its
# LAST occurrence is on screen the whole over-wide row has been rendered.
waitfor flowwrapflowwrapflowwrap
shot ui-6-flowwrap
type /quit
submit
exit
SCRIPT_EOF

cd "$WORK" || exit 1
rm -rf "$WORK/home"
env -u DEEPSEEK_API_KEY -u DEEPSEEK_BASE_URL -u DEEPSEEK_MODEL \
    -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN -u ANTHROPIC_BASE_URL -u ANTHROPIC_MODEL \
    -u GOPI_TASK_REPORT_PATH -u GOPI_TASK_REPORT_OWNER -u GOPI_EXECUTOR_SESSION_ID \
    -u GOPI_MEMORY_DB \
    "GOPI_HOME=$WORK/home" "GOPI_HUMAN_TERMINAL=1" \
    "ANTHROPIC_AUTH_TOKEN=capture-key" \
    "ANTHROPIC_BASE_URL=http://127.0.0.1:$PORT" \
    "GOPI_MAX_TOKENS=16384" "GOPI_THINKING_BUDGET=16384" \
    "LANG=C.utf8" "LC_ALL=C.utf8" "TERM=xterm-256color" \
    python3 "$EVID/termgrid.py" "$BIN" "$COLS" "$ROWS" "$SCRIPT" \
    >"$WORK/grid-$LABEL.log" 2>&1
RC=$?
echo "grid log: $WORK/grid-$LABEL.log ($(tail -1 "$WORK/grid-$LABEL.log"))"

for f in ui-6-flowwrap; do
    cp -f "$WORK/$f.grid.txt"      "$EVID/$LABEL-$f.grid.txt"      2>/dev/null
    cp -f "$WORK/$f.grid.attr.txt" "$EVID/$LABEL-$f.grid.attr.txt" 2>/dev/null
    cp -f "$WORK/$f.raw.txt"       "$EVID/$LABEL-$f.raw.txt"       2>/dev/null
done
ls -1 "$EVID/$LABEL"-ui-6-flowwrap.* >/dev/null 2>&1 || {
    echo "FAIL: no shot was written — the run never reached a shot op (see grid-$LABEL.log; a waitfor TIMEOUT names the missing state)" >&2
    exit 1; }
[ "$RC" = "0" ] || { echo "FAIL: the grid run exited $RC" >&2; exit 1; }

# --- the capture's own check: the wrapped row's WIDTHS are the fact ----------
# A shot is evidence only if it is readable as the fact it claims. The claim is
# "the over-wide reasoning row is wrapped at the band's own width (COLS)', by the
# producer", and the measured difference is in the WIDTHS of its first two rows:
# a producer wrapping at COLS fills each non-final row to exactly COLS cells,
# while the layout's own re-cut comes up short by the escape bytes it counted as
# cells (measured before: 96, because `[thinking] ` opens with a dim marker).
python3 - "$EVID/$LABEL-ui-6-flowwrap.grid.txt" "$COLS" <<'PY' || exit 1
import sys
grid = [l.split("|", 1)[1] if "|" in l else "" for l in open(sys.argv[1]).read().split("\n")]
cols = int(sys.argv[2])
rows = [(i, r) for i, r in enumerate(grid) if r.strip()]
if len(rows) < 2:
    print("FAIL: the grid carries no rendered row"); sys.exit(1)
widths = [len(r) for _, r in rows]
print("rendered rows (screen row: cells): %s" % ", ".join("%d:%d" % (i, len(r)) for i, r in rows))
print("full-width rows (== %d cells): %d" % (cols, sum(1 for w in widths if w == cols)))
PY
echo "wrote $EVID/$LABEL-ui-6-flowwrap.{grid,grid.attr,raw}.txt"
