#!/usr/bin/env bash
# screen.sh — instrument 2: photograph the chat UI on a REAL X11 desktop.
#
# Xvfb → a real xterm → the real binary in tools mode → the scripted LLM →
# `xdotool` as the hands → `import` as the eye. This is the only instrument that
# shows what the terminal ACTUALLY PAINTED (fallback fonts, real colour, real
# wide-character handling); termgrid.py is the one that is byte-exact. Both run
# the SAME script so the two can be read against each other.
#
# Invoked by capture.sh (which owns the build, the script and the log); it can
# also be run directly for a single-instrument probe:
#   screen.sh <label> <bin> <workdir>
#
# PITFALL (encoded here, measured 2026-09-19): `xterm -u8` is REQUIRED. Under
# a non-UTF-8 locale the UI's "·" and "⚙" come out as mojibake in the PNG, which
# reads as a UI defect and is not one. The locale is also set explicitly
# (LANG/LC_ALL=C.utf8) — the two together, because either alone was not enough.
#
# No device load anywhere here (Rule 27): Xvfb is a virtual framebuffer, the
# sleeps are fixed scripted waits, and the model is a local HTTP stub.
set -uo pipefail

LABEL="${1:?usage: screen.sh <label> <bin> <workdir>}"
BIN="${2:?usage: screen.sh <label> <bin> <workdir>}"
WORK="${3:?usage: screen.sh <label> <bin> <workdir>}"
EVID="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

COLS="${COLS:-100}"
ROWS="${ROWS:-30}"
DISPLAY_NUM="${DISPLAY_NUM:-:78}"
PORT="${PORT:-8792}"

# HOLD_SEC — how long the model's 2nd call stays open, i.e. how long the state
# "a turn is in flight" exists and can be photographed. The X11 instrument needs
# ~7 s merely to TYPE the prompt, so the mock's 3 s default is over before its
# "toolrunning" shot is due; a longer hold is what makes the state observable at
# all (measured 2026-09-19: with the short default, shot 2 and shot 3 came out
# BYTE-IDENTICAL and shot 2 was mislabeled).
# Both knobs normalize a BLANK inherited value (`${VAR:-x}` does not fire when a
# variable is set-but-empty) — a blank FAKELLM_HOLD_SEC crashed the mock at
# import time and the capture then blamed the app. Rule 1a: named, one place.
HOLD_SEC="${HOLD_SEC:-14}"; HOLD_SEC="${HOLD_SEC:-14}"
WAIT_SEC="${WAIT_SEC:-60}";  WAIT_SEC="${WAIT_SEC:-60}"
MARK="$WORK/fakellm-mark-screen-$LABEL"

stub_started() {  # the just-launched mock must be alive (see capture.sh)
    local name="$1"
    kill -0 "${LLM_PID:-0}" 2>/dev/null && return 0
    echo "screen.sh: the mock model ($name) died at startup — see $WORK/fakellm-$name-$LABEL.log" >&2
    exit 1
}

# The stub is the model: if it is not answering, a capture photographs an app
# that is waiting, and every reading off it is a fiction. The two ways it dies
# silently are a stale port holder and a bad env value.
stub_alive() {
    kill -0 "${LLM_PID:-0}" 2>/dev/null && return 0
    echo "screen.sh: the mock model is not running — see $WORK/fakellm-screen-$LABEL.log" >&2
    return 1
}

# wait_mark N — block until the stub has served N requests. This is the same
# discipline the grid instrument uses on the screen text: anchor on observed
# state, never on a guessed sleep. A sleep anchors on the clock; the caption of
# the shot then claims a state the clock never promised (the mislabeled shot).
wait_mark() {
    local want="$1" state="${2:-arrived}" i=0 got
    stub_alive || return 1
    while [ "$i" -lt $((WAIT_SEC * 4)) ]; do
        got="$(cat "$MARK" 2>/dev/null)"
        if [ -n "$got" ] && [ "${got%% *}" = "$want" ] && [ "${got##* }" = "$state" ]; then
            return 0
        fi
        sleep 0.25
        i=$((i + 1))
    done
    echo "screen.sh: timed out waiting for request $want state=$state (mark='${got:-none}')" >&2
    return 1
}

cleanup() {
    for p in "${XTERM_PID:-}" "${LLM_PID:-}" "${XVFB_PID:-}"; do
        [[ -n "$p" ]] && kill "$p" 2>/dev/null
    done
    return 0
}
trap cleanup EXIT

Xvfb "$DISPLAY_NUM" -screen 0 1400x900x24 -nolisten tcp >"$WORK/xvfb-$LABEL.log" 2>&1 &
XVFB_PID=$!
sleep 1

rm -f "$MARK"
FAKELLM_HOLD_SEC="$HOLD_SEC" FAKELLM_MARK="$MARK" \
python3 "$EVID/fakellm.py" "$PORT" styled "$WORK/bodies-screen-$LABEL.log" \
    >"$WORK/fakellm-screen-$LABEL.log" 2>&1 &
LLM_PID=$!
sleep 0.5
stub_started screen
sleep 0.2

export DISPLAY="$DISPLAY_NUM"

# The same scrub capture.sh applies to the grid instrument: a PNG capture that
# inherited an API key talks to the REAL model and photographs a different
# session every time.
scrub=(env -u DEEPSEEK_API_KEY -u DEEPSEEK_BASE_URL -u DEEPSEEK_MODEL
           -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN
           -u ANTHROPIC_BASE_URL -u ANTHROPIC_MODEL
           -u GOPI_TASK_REPORT_PATH -u GOPI_TASK_REPORT_OWNER -u GOPI_EXECUTOR_SESSION_ID
           -u GOPI_MEMORY_DB
           "GOPI_HOME=$WORK/home-screen"
           "ANTHROPIC_AUTH_TOKEN=capture-key"
           "ANTHROPIC_BASE_URL=http://127.0.0.1:$PORT"
           "GOPI_HUMAN_TERMINAL=1"
           "LANG=C.utf8" "LC_ALL=C.utf8" "TERM=xterm-256color")

# `-u8`: UTF-8 mode, without which "·"/"⚙" are mojibake in the PNG (pitfall 1).
xterm -display "$DISPLAY_NUM" -u8 -T "gopi-capture" -geometry "${COLS}x${ROWS}" \
    -fa Monospace -fs 11 -bg black -fg white -e "${scrub[@]}" "$BIN" chat --tools &
XTERM_PID=$!

WIN=""
for _ in $(seq 1 60); do
    WIN=$(xdotool search --name gopi-capture 2>/dev/null | head -1)
    [[ -n "$WIN" ]] && break
    sleep 0.25
done
[[ -z "$WIN" ]] && { echo "screen.sh: no xterm window appeared" >&2; exit 1; }

# --- the input path must not depend on INPUT FOCUS -------------------------
# MEASURED 2026-09-19 (root cause of a whole capture going blank): this runs on
# a bare Xvfb with NO window manager, so `xdotool windowactivate` ALWAYS fails
# ("Your windowmanager claims not to support _NET_ACTIVE_WINDOW") and focus is
# whatever it happens to be. `xdotool type --window W` delivers an XSendEvent,
# which xterm accepts only while its window actually holds the input focus —
# and with no WM that focus is opportunistic. When it did not stick, EVERY
# keystroke went nowhere: the prompt was never typed, the stub was never called,
# and all four PNGs came out byte-identical (capture.sh's distinctness gate
# failed that run — correctly — but the instrument had silently tested nothing).
#
# A/B on a bare Xvfb, fresh display per case, each case sending its Return:
#   focus=no  XSendEvent (`type --window`) → landed NOTHING
#   focus=no  XTEST (`type`, no --window)  → landed  ← works with no focus at all
#   focus=yes both                          → landed
# XTEST delivers to the window under the POINTER when focus is PointerRoot
# (which is what a WM-less X server uses), so the pointer is placed inside the
# window first and every keystroke goes through XTEST. Focus is then an
# optimisation, not a dependency.
xdotool mousemove --window "$WIN" 20 20 2>/dev/null
xdotool windowfocus --sync "$WIN" 2>/dev/null   # best-effort; not relied upon
sleep 2.5
import -window root -display "$DISPLAY_NUM" "$EVID/$LABEL-screen-1-boot.png"

# PITFALL: a whole-line write is a PASTE BURST. `xdotool type --delay` types
# character by character and the Return is a SEPARATE key event after a pause —
# a key that arrives inside the editor's paste-burst window is swallowed as
# pasted content and the line never runs. Both events go through XTEST (no
# `--window`), for the focus-independence measured above.
type_line() {
    xdotool mousemove --window "$WIN" 20 20 2>/dev/null
    xdotool type --delay 150 -- "$1"
    sleep 1.2
    xdotool key Return
}

type_line "what is in devdoc/chat-mode-report.md? briefly"
# The typed prompt must have REACHED the stub before any shot claims a state
# that depends on the turn. Without this, a focus failure shows up as
# "timed out waiting for 2 streaming" 60 s later — which names the symptom and
# not the fault (measured 2026-09-19: exactly that misattribution).
if ! wait_mark 2 arrived; then
    echo "screen.sh: the typed prompt never reached the model (request 2 never arrived) — the input path did not land; see the focus note above" >&2
    exit 1
fi
# The shot's anchor is an OBSERVED state, not a sleep: `streaming` means the
# stub has delivered the FIRST answer delta, i.e. the answer has started
# arriving on screen. (Anchoring on the status row was measured wrong — it lags
# the answer, so shots 2 and 3 came out byte-identical in the first baseline.)
wait_mark 2 streaming
sleep 1.0
import -window root -display "$DISPLAY_NUM" "$EVID/$LABEL-screen-2-answer-start.png"
# Shot 3 is captioned "the answer is complete"; its owning fact is the 2nd call
# having CLOSED. Both shots are taken once: the PNG is a snapshot, so once the
# hold expires the state is gone and a retry would photograph whatever came next.
wait_mark 2 done
sleep 1.5
import -window root -display "$DISPLAY_NUM" "$EVID/$LABEL-screen-3-answer-done.png"

type_line "/quit"
# The shot's claim is "the app exited", so the retry loop below re-imports the
# frozen root until the window is ACTUALLY GONE — a PNG is only a snapshot, and
# a single import can land before the process has torn its window down.
MAX_RETRIES="${MAX_RETRIES:-40}"
shot_retry_from=0
for i in $(seq 1 "$MAX_RETRIES"); do
    if ! xdotool search --name gopi-capture >/dev/null 2>&1; then
        import -window root -display "$DISPLAY_NUM" "$EVID/$LABEL-screen-4-quit.png"
        shot_retry_from=$i
        break
    fi
    sleep 0.25
done
if [ "$shot_retry_from" = 0 ]; then
    # Still up: take the shot anyway (a capture should record what is there) and
    # say so on stderr — a caption nobody could back is reported, never dressed up.
    import -window root -display "$DISPLAY_NUM" "$EVID/$LABEL-screen-4-quit.png"
    echo "screen.sh: the window was still present after ${MAX_RETRIES} polls — -4-quit shows the last painted screen, not an exited app" >&2
fi
echo "screen capture: $LABEL"
