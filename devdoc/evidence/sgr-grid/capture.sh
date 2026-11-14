#!/usr/bin/env bash
# capture.sh — ONE command that captures the `gopi chat --tools` UI two ways:
# the cell grid with its SGR attributes, and a PNG of a real X11 terminal.
#
# This is the self-observation instrument for chat-UI work. It exists because
# the question "does this row render in colour / dim / bold, and WHERE" had no
# machine-readable answer: a screenshot is pixels and the vision model reads it
# back as prose (measured: `·` → `Å:`, UUIDs split across the wrong line), while
# the byte capture dropped SGR entirely ("SGR changes colour, not cells").
#
#   Instrument 1 — PTY bytes → grid (termgrid.py, a VT100 cell grid that now
#     records per-cell SGR: fg/bg, bold, dim). This is the EXACTNESS instrument:
#     "the status row is overwritten at column 34" is a fact here, and so is
#     "columns 4-6 of row 12 are fg=1" (red).
#   Instrument 2 — Xvfb + xterm + xdotool + import → PNG read back by
#     vision_analyze. This is the PIXEL instrument: the only one that shows what
#     the terminal actually painted, and the one a human would look at.
#
# Both run the SAME script against the SAME scripted fake model, so the two
# captures can be read against each other.
#
# Usage:
#   capture.sh <label> [repo-root]
#     label       before | after | anything — the prefix of the emitted files
#     repo-root   the checkout to BUILD and run (default: this repo). The point
#                 of the label is that the SAME script, model and geometry are
#                 used on two checkouts, so only the code differs.
#
# Output (under devdoc/evidence/sgr-grid/, i.e. the evidence dir):
#   <label>-ui-1-boot.grid.attr.txt         grid + per-cell attribute runs
#   <label>-ui-{2-answer-start,3-answer-done,4-quit}.{grid,grid.attr,raw}.txt
#   <label>-screen-{1-boot,2-answer-start,3-answer-done,4-quit}.png
#   capture-<label>.log                     the run's own log
#
# Every shot is named for the STATE the instrument OBSERVED before taking it
# (see the script below: each shot is preceded by a `waitfor` on that state),
# because a caption is a claim and a claim needs an observation. An earlier
# version guessed with sleeps and produced a "toolrunning" shot that was really
# the finished answer — byte-identical to its neighbour (measured 2026-09-19).
#
# TWO KNOWN PITFALLS, both already measured — the script encodes both:
#   1. LANG=C gives MOJIBAKE. The UI draws "·" and "⚙"; a C locale makes xterm
#      and the app disagree about those bytes, and the capture shows garbage.
#      The script sets LANG/LC_ALL=C.utf8 AND passes `xterm -u8` (UTF-8 mode);
#      either one alone was not enough.
#   2. A WHOLE-LINE WRITE ARRIVES AS A PASTE BURST. Writing a line in one go
#      makes the editor's paste-suppression window eat the submit CR as pasted
#      content — the line never runs and the harness silently tests nothing
#      (measured 2026-09-19: a slash-command probe read as "the output never
#      printed"). termgrid.py's `type` op writes character by character and the
#      `submit` op waits the window out; the X11 instrument types with
#      `xdotool type --delay` and a separate Return.
#
# ENV SCRUB: the ambient environment MUST be scrubbed. A capture that inherited
# DEEPSEEK_API_KEY/ANTHROPIC_BASE_URL talked to the REAL API (measured
# 2026-09-19: the "grid" run reasoned about memory rules and fired
# `bash find /` at the root filesystem). Every API key and base URL is unset
# and repointed at the local scripted server, and GOPI_HOME points at a
# THROWAWAY directory so a capture never reads or writes the operator's real
# ~/.gopi (memory, sessions, task ledger).
#
# Rule 22: the throwaway tree lives under the workspace, never on /.
set -uo pipefail

LABEL="${1:?usage: capture.sh <label> [repo-root]}"
REPO="${2:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)}"
EVID="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

COLS="${COLS:-100}"
ROWS="${ROWS:-30}"
DISPLAY_NUM="${DISPLAY_NUM:-:78}"
GRID_PORT="${GRID_PORT:-8791}"
SCREEN_PORT="${SCREEN_PORT:-8792}"
INSECURE_HTTP="${INSECURE_HTTP:-http://127.0.0.1:$SCREEN_PORT}"
# HOLD_SEC — how long the mock's 2nd call stays open, i.e. how long the state
# "the turn is in flight" exists to be photographed. It must outlast the
# instrument's own typing time (~7 s in the X11 instrument), or a shot captioned
# "toolrunning" lands after the turn finished (measured 2026-09-19: byte-identical
# shots 2 and 3). Blank is normalized here for the same reason as below (Rule 1a).
HOLD_SEC="${HOLD_SEC:-14}"; HOLD_SEC="${HOLD_SEC:-14}"

# The throwaway tree and the built binary live OUTSIDE the checkout (Rule 30:
# a run leaves its worktree clean — a build binary is not a deliverable).
WORK="${WORK:-$HOME/workspace/_tmp/sgr-grid-capture-$LABEL}"
mkdir -p "$WORK" || exit 1
BIN="$WORK/gopi"

log() { printf '%s\n' "$*" | tee -a "$EVID/capture-$LABEL.log"; }

: >"$EVID/capture-$LABEL.log"
log "capture.sh label=$LABEL repo=$REPO work=$WORK"

# --- the instrument proves itself before it observes anything ---------------
# The emulator's own two-colour selftest: if the grid drops SGR the capture
# below would be attribute-blind and every colour claim read off it would be a
# guess. Fail HERE instead.
if ! python3 "$EVID/termgrid.py" --selftest >>"$EVID/capture-$LABEL.log" 2>&1; then
    log "termgrid --selftest FAILED — the emulator does not record attributes; refusing to capture"
    exit 1
fi
log "termgrid selftest OK (per-cell attributes recorded)"

# --- the mock model must actually be answering ------------------------------
# The stub IS the model: a capture taken while it is dead photographs an app
# waiting for an answer, and every reading off it is a fiction about the app.
# The two ways it dies silently are a stale port holder and a bad env value, so
# both are checked BEFORE the run and the stub's pid is checked after the start.
port_holder() {  # port_holder <port> -> prints the holding pid, if any
    python3 - "$1" <<'PY'
import socket, sys
s = socket.socket()
try:
    s.connect(("127.0.0.1", int(sys.argv[1])))
    print("in-use")
except OSError:
    pass
finally:
    s.close()
PY
}

check_port_free() {
    local p="$1" who
    who="$(port_holder "$p")"
    [ -z "$who" ] && return 0
    log "FAIL: port $p is already in use — something else is bound there (a stale mock?), so the stub would fail to bind and the capture would photograph an app waiting for a model that never answers"
    return 1
}

stub_started() {  # stub_started <name> ; asserts the just-launched stub is alive
    local name="$1"
    if kill -0 "${LLM_PID:-0}" 2>/dev/null; then
        return 0
    fi
    log "FAIL: the mock model ($name) died at startup — see $WORK/fakellm-$name-$LABEL.log"
    exit 1
}

# --- the checkout under test must be reachable -------------------------------
# A missing repo-root otherwise surfaces as "build FAILED", which reads as a
# broken checkout when the real fault is the path. Measured 2026-09-19: a
# worktree removed by another session produced exactly that misdiagnosis.
BUILD_TIMEOUT="${BUILD_TIMEOUT:-600}"
if [ ! -d "$REPO" ]; then
    log "FAIL: repo-root $REPO does not exist — nothing to build. Pass an existing checkout (default: this repo)."
    exit 1
fi
if [ ! -d "$REPO/.git" ] && [ ! -f "$REPO/.git" ]; then
    log "FAIL: repo-root $REPO is not a git checkout (no .git) — a capture records the revision under test, so the tree must be a working tree."
    exit 1
fi

# --- build the checkout under test -----------------------------------------
if [ "${SKIP_BUILD:-0}" != "1" ]; then
    if ! (cd "$REPO" && CGO_ENABLED=0 go build -o "$BIN" ./cmd/agent/) >>"$EVID/capture-$LABEL.log" 2>&1; then
        log "build FAILED in $REPO (see the log tail above) — this is a BUILD fault in the checkout, not a capture fault"
        exit 1
    fi
    log "built $BIN ($(cd "$REPO" && git rev-parse --short HEAD 2>/dev/null))"
else
    # SKIP_BUILD reuses the binary from a previous run of the SAME label, so the
    # binary may simply not be there — and the failure then surfaces as a
    # `FileNotFoundError` traceback from inside termgrid, i.e. it reads as an
    # emulator bug rather than a missing artifact. Measured 2026-09-19: a
    # `SKIP_BUILD=1` probe died that way and the log still claimed "built".
    # Gate here, and word the line for what actually happened.
    if [ ! -x "$BIN" ]; then
        log "FAIL: SKIP_BUILD=1 but no executable at $BIN — this run reuses a binary built by an earlier run of the SAME label; drop SKIP_BUILD (or set BIN/WORK) instead"
        exit 1
    fi
    log "reusing $BIN (SKIP_BUILD=1, $(cd "$REPO" && git rev-parse --short HEAD 2>/dev/null))"
fi

# --- the scripted session (identical for every label) -----------------------
SCRIPT="$WORK/script-ui.txt"
cat >"$SCRIPT" <<'SCRIPT_EOF'
# The ONE scripted session used for BEFORE and AFTER proof.
# Identical script, identical mock, identical geometry: only $BIN changes.
#
# Every shot is anchored on an OBSERVED shell row (waitfor), never on a guessed
# sleep: the caption is a state claim and only the screen can prove it. The
# first baseline guessed (sleep 1.2 / sleep 6) and its 2-toolrunning shot was
# byte-identical to 3-answer (measured 2026-09-19).
sleep 2.5
waitfor gopi chat
shot ui-1-boot
type what is in devdoc/chat-mode-report.md? briefly
submit
# "the answer has started arriving" — the FIRST observable line of the answer.
# The status row's own wording ("thinking…") is NOT usable as a shot anchor:
# measured 2026-09-19, it flips to the live state only when the model's first
# delta lands, and it was still readable as "ready"/"iter 0" in shots taken
# while the answer was already painted — the row is a weaker fact than the
# answer text itself, so the anchor is the answer text.
waitfor Here is the answer
shot ui-2-answer-start
# "the answer is complete" — its last rendered line is on screen.
waitfor Done.
shot ui-3-answer-done
# "the session ended" — the app process is gone (the `exit` op waits for it).
# NOTHING weaker backs this caption, so no `/help` shot is taken HERE: measured
# 2026-09-19 on master, the help panel is painted INTERLEAVED with the layout's
# own rows — the panel's first line landed on the EDITING row and each following
# line scrolled the band up over the panel's own previous line, so no substring
# of the panel was reliably on screen and a "-help" caption could not be backed
# by an observation. That interleaving was a real defect the instrument found
# (fixed on branch feat/chat-help-status-rows: the REPL's own text now streams
# through the answer band — `capture-help.sh` is the shot this script omits, and
# its before/after pair is the evidence for that fix).
type /quit
submit
exit
shot ui-4-quit
SCRIPT_EOF

cleanup() {
    [[ -n "${LLM_PID:-}" ]] && kill "$LLM_PID" 2>/dev/null
    [[ -n "${XVFB_PID:-}" ]] && kill "$XVFB_PID" 2>/dev/null
    [[ -n "${XTERM_PID:-}" ]] && kill "$XTERM_PID" 2>/dev/null
    return 0
}
trap cleanup EXIT

# The scrub list is a NAMED array (Rule 1b): one place to find every knob this
# capture must not inherit.
scrub=(env -u DEEPSEEK_API_KEY -u DEEPSEEK_BASE_URL -u DEEPSEEK_MODEL
           -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN
           -u ANTHROPIC_BASE_URL -u ANTHROPIC_MODEL
           -u GOPI_TASK_REPORT_PATH -u GOPI_TASK_REPORT_OWNER -u GOPI_EXECUTOR_SESSION_ID
           -u GOPI_MEMORY_DB
           "GOPI_HOME=$WORK/home"
           "ANTHROPIC_AUTH_TOKEN=capture-key"
           "ANTHROPIC_BASE_URL=http://127.0.0.1:$GRID_PORT"
           "GOPI_HUMAN_TERMINAL=1"
           "LANG=C.utf8" "LC_ALL=C.utf8" "TERM=xterm-256color")

check_port_free "$GRID_PORT" || exit 1
check_port_free "$SCREEN_PORT" || exit 1

# --- instrument 1: the grid -------------------------------------------------
cd "$WORK" || exit 1
rm -rf "$WORK/home"
rm -f "$WORK/fakellm-mark-grid-$LABEL"
FAKELLM_HOLD_SEC="$HOLD_SEC" FAKELLM_MARK="$WORK/fakellm-mark-grid-$LABEL" \
python3 "$EVID/fakellm.py" "$GRID_PORT" styled "$WORK/bodies-$LABEL.log" \
    >"$WORK/fakellm-grid-$LABEL.log" 2>&1 &
LLM_PID=$!
sleep 0.5
stub_started grid

"${scrub[@]}" python3 "$EVID/termgrid.py" "$BIN" "$COLS" "$ROWS" "$SCRIPT" \
    >"$WORK/grid-$LABEL.log" 2>&1
GRID_RC=$?
for f in ui-1-boot ui-2-answer-start ui-3-answer-done ui-4-quit; do
    mv -f "$WORK/$f.grid.txt"      "$EVID/$LABEL-$f.grid.txt"       2>/dev/null
    mv -f "$WORK/$f.grid.attr.txt" "$EVID/$LABEL-$f.grid.attr.txt"  2>/dev/null
    mv -f "$WORK/$f.raw.txt"       "$EVID/$LABEL-$f.raw.txt"        2>/dev/null
done
log "grid capture: $LABEL-*.grid.attr.txt"
if [ "$GRID_RC" != "0" ]; then
    log "FAIL: the grid run exited $GRID_RC — a shot's caption never came true (see grid-$LABEL.log; a waitfor TIMEOUT names the missing state)"
    exit 1
fi

# The captured artifact must carry the two-colour probe, or nothing here is
# evidence about attributes: the probe line the MODEL emits (fakellm.py's
# STYLED scenario) is the sample — red then default then bold+dim. Shot 3 is
# anchored on the answer's last line, so the probe MUST be on screen by then.
if grep -q "fg=1" "$EVID/$LABEL-ui-3-answer-done.grid.attr.txt" 2>/dev/null; then
    log "attribute probe present in the answer dump (fg=1 run found)"
else
    log "FAIL: no fg= run in $LABEL-ui-3-answer-done.grid.attr.txt — the styled line never reached the screen"
    exit 1
fi

# The grid shots carry different captions, so they must be different screens.
# Same check as the PNG one below, on the exactness instrument: a duplicate here
# means a shot was anchored on the clock instead of on the state it names.
declare -A GSEEN
GDUP=""
for n in 1-boot 2-answer-start 3-answer-done 4-quit; do
    f="$EVID/$LABEL-ui-$n.grid.txt"
    [ -r "$f" ] || { log "grid shot missing: $LABEL-ui-$n.grid.txt"; exit 1; }
    h="$(sha256sum "$f" | cut -c1-16)"
    log "grid shot $n sha256=$h"
    if [ -n "${GSEEN[$h]:-}" ]; then
        GDUP="$GDUP $n=${GSEEN[$h]}"
    else
        GSEEN[$h]="$n"
    fi
done
if [ -n "$GDUP" ]; then
    log "FAIL: identical grid shots (different captions, same screen):$GDUP"
    exit 1
fi

# --- instrument 2: the pixels -----------------------------------------------
"$EVID/screen.sh" "$LABEL" "$BIN" "$WORK" >>"$EVID/capture-$LABEL.log" 2>&1 \
    || log "screen capture failed (non-fatal): the grid is the exactness instrument"

# --- the shots must be DISTINCT states --------------------------------------
# A capture whose shots are byte-identical is a broken instrument, not a quiet
# screen: two shots carrying different captions cannot be the same pixels.
# Measured 2026-09-19 (this check's reason for existing): the fixed-sleep
# anchors made `-2-toolrunning` and `-3-answer` byte-identical, so the committed
# baseline had a mislabeled shot nobody could see — same bytes, two captions.
# Records the hashes either way (the record is what a reviewer checks), and
# refuses to report success on a duplicate.
shot_hash() { sha256sum "$1" 2>/dev/null | cut -c1-16; }
declare -A SEEN
DUPLICATE=""
for n in 1-boot 2-answer-start 3-answer-done 4-quit; do
    f="$EVID/$LABEL-screen-$n.png"
    [ -r "$f" ] || { log "screen shot missing: $LABEL-screen-$n.png"; continue; }
    h="$(shot_hash "$f")"
    log "screen shot $n sha256=$h"
    if [ -n "${SEEN[$h]:-}" ]; then
        DUPLICATE="$DUPLICATE $n=${SEEN[$h]}@$h"
    else
        SEEN[$h]="$n"
    fi
done
if [ -n "$DUPLICATE" ]; then
    log "FAIL: identical screen shots (different captions, same pixels):$DUPLICATE — the shot anchors do not track observed state"
    exit 1
fi

log "done: $LABEL"
