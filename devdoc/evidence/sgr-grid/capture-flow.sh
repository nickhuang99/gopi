#!/usr/bin/env bash
# capture-flow.sh — the ONE command that reproduces the AGENT's flow output
# tearing the chat layout (and, on a fixed checkout, its absence).
#
# Why it exists: capture.sh's scripted shot set drives a MOCK model whose rounds
# carry no reasoning stream, and capture-help.sh photographs the `/help` panel —
# neither produces the state this defect lives in, which is a REAL turn: a live
# reasoning block (`[thinking] …`) written by `internal/agent` straight to
# os.Stderr, plus the `[gopi]` notice that rides the same stream, arriving while
# the reserved layout owns the screen. On the unfixed build the reasoning block
# tears the answer band's rows and the notice overwrites the STATUS row mid-line
# (task 01a0c0a7).
#
# Usage:
#   capture-flow.sh <label> <binary>
#     label    before | after | anything — the prefix of the emitted files
#     binary   the gopi binary to run (build it from the checkout under test)
#
# Output (next to this script):
#   <label>-turn-1-flow.grid.txt       the screen at the instant the agent's own
#                                      flow output has arrived (anchor: the
#                                      clamped-budget notice, an observed string)
#   <label>-turn-1-flow.grid.attr.txt  the same screen plus per-cell attributes
#   <label>-turn-1-flow.raw.txt        the exact PTY byte stream
#   <label>-turn-2-answer.*            the same three, after the answer landed
#
# The turn uses the REAL endpoint, not the mock: the reasoning stream only exists
# when the model sends one, and the notice below is produced by the app from the
# budget knobs, so both are reproducible — but the words of the answer are the
# model's, which is why no shot is anchored on them.
#
# ENV: the model env (DEEPSEEK_API_KEY and friends) is PASSED THROUGH here, unlike
# the other capture scripts: this shot needs a real reasoning stream. GOPI_HOME
# still points at a THROWAWAY tree and LANG/LC_ALL stay C.utf8 (`·` and `⚙` are
# mojibake under a C locale). Rule 22: the throwaway tree is under the workspace,
# never on /.
#
# THE CLAMPED NOTICE, and why the shot is anchored on it: reportEffectiveThinking
# Budget logs it once per run, and ONLY when the configured budget differs from
# the effective one — so GOPI_MAX_TOKENS=16384 with GOPI_THINKING_BUDGET=16384
# produces it deterministically, before the answer. It is the one flow-output
# line this harness can predict word for word, so it is what the shot waits for.
set -uo pipefail

LABEL="${1:?usage: capture-flow.sh <label> <binary>}"
BIN="${2:?usage: capture-flow.sh <label> <binary>}"
EVID="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

COLS="${COLS:-100}"
ROWS="${ROWS:-30}"
# ANSWER_WAIT — how long the second shot waits for the answer after the notice
# landed. The words are the model's, so there is no substring to anchor on; the
# wait is bounded and the shot is captioned for what it is ("after the answer").
ANSWER_WAIT="${ANSWER_WAIT:-25}"
WORK="${WORK:-$HOME/workspace/_tmp/sgr-flow-capture-$LABEL}"
mkdir -p "$WORK" || exit 1
[ -x "$BIN" ] || { echo "FAIL: no executable at $BIN" >&2; exit 1; }

# The instrument proves itself before it observes anything: a capture from an
# attribute-blind emulator would make every reading below a guess.
python3 "$EVID/termgrid.py" --selftest >"$WORK/selftest.log" 2>&1 || {
    echo "FAIL: termgrid --selftest failed (see $WORK/selftest.log)" >&2; exit 1; }

SCRIPT="$WORK/script-flow.txt"
cat >"$SCRIPT" <<SCRIPT_EOF
sleep 2.5
waitfor gopi chat
type hello world
submit
# The anchor: the app's own clamped-budget line — agent flow output, produced by
# the app, on screen. At this instant the reasoning stream is live too.
waitfor clamped to 8192
shot turn-1-flow
sleep $ANSWER_WAIT
shot turn-2-answer
exit
SCRIPT_EOF

cd "$WORK" || exit 1
rm -rf "$WORK/home"
env -u GOPI_TASK_REPORT_PATH -u GOPI_TASK_REPORT_OWNER -u GOPI_EXECUTOR_SESSION_ID \
    -u GOPI_MEMORY_DB \
    "GOPI_HOME=$WORK/home" "GOPI_HUMAN_TERMINAL=1" \
    "GOPI_MAX_TOKENS=16384" "GOPI_THINKING_BUDGET=16384" \
    "LANG=C.utf8" "LC_ALL=C.utf8" "TERM=xterm-256color" \
    python3 "$EVID/termgrid.py" "$BIN" "$COLS" "$ROWS" "$SCRIPT" \
    >"$WORK/grid-$LABEL.log" 2>&1
echo "grid log: $WORK/grid-$LABEL.log ($(tail -1 "$WORK/grid-$LABEL.log"))"

for f in turn-1-flow turn-2-answer; do
    cp -f "$WORK/$f.grid.txt"      "$EVID/$LABEL-$f.grid.txt"      2>/dev/null
    cp -f "$WORK/$f.grid.attr.txt" "$EVID/$LABEL-$f.grid.attr.txt" 2>/dev/null
    cp -f "$WORK/$f.raw.txt"       "$EVID/$LABEL-$f.raw.txt"       2>/dev/null
done
ls -1 "$EVID/$LABEL"-turn-*.grid.txt 2>/dev/null || {
    echo "FAIL: no shot was written — the run never reached a shot op" >&2; exit 1; }
