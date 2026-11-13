#!/usr/bin/env bash
# chat_replay_xtest.sh — REAL KEYBOARD EVENTS for `gopi chat --replay`, injected
# through XTEST, observed on the channel the playback controls actually write.
#
# WHY THIS EXISTS (task 01a1071b). The playback controls were implemented and unit
# tested at the injection seam (`input.injector`) — a path NO HUMAN EVER TAKES. A
# watcher's keys are decoded by the terminal, read by the editor's raw-mode reader,
# stamped with their ORIGIN, and only then offered to the control layer's key gate.
# Every one of those legs can fail invisibly to a unit test. The operator's
# requirement is literal: "使用 xdotool/xtest 来测试 key-in 事件" — inject real X11
# key events into a real terminal and watch the real surface answer.
#
# THE CHANNEL, and why it is this one. xterm's own pty session log (-l -lf) holds
# every byte the run wrote, in order: the control layer's OSD lines (`chatNoticef`
# → stderr) and the REPL's own output. A measured fact about the pair: the OSD line
# is drawn and then REPAINTED OVER by the reserved chat layout, so the screen DUMP
# (--dump-screen) is the wrong instrument for a control key and the right one only
# for "did this surface paint at all". Reading the wrong one would report the
# feature missing while it works — the instrument is chosen per channel.
#
# WHICH CONTROLS, and the evidence each must produce:
#   space → PAUSED        space → resumed      left → step back
#   right → step forward  up → jumped back     down → jumped forward
#   ?     → the keys line q     → the run ENDS (defect ①) with the watcher's code
#
# THE DEFECT-① ARM is the last. Before the fix, `q` stopped the driver's typing and
# left the process parked at a prompt whose answers still came from the recording —
# the "confusing mode" the operator photographed — and the run had to be killed by
# hand. Here the run must exit BY ITSELF, and its exit code names the ending: 0 for
# the watcher's own leave, not 1 for a driver failure.
#
# CLEANLINESS (Rules 22/30): the private display, the terminal, its session log and
# the dumps all live under a scratch directory taken from scripts/lib/tmp-ban.sh
# (gopi_scratch_mkdir asserts it is off the `/` partition BY DEVICE ID), never
# inside the worktree and never on an ambient temp root.
#
# USAGE:  scripts/replay/chat_replay_xtest.sh [--bin PATH] [--keep]

set -uo pipefail

BIN=""
KEEP=0
while [ $# -gt 0 ]; do
  case "$1" in
  --bin)
    BIN="$2"
    shift 2
    ;;
  --keep)
    KEEP=1
    shift
    ;;
  *)
    echo "usage: $0 [--bin PATH] [--keep]" >&2
    exit 2
    ;;
  esac
done
[ -n "$BIN" ] || BIN=$(command -v gopi || true)
[ -x "$BIN" ] || {
  echo "no gopi binary: pass --bin PATH" >&2
  exit 2
}

# The scratch directory comes from the repo's OWN shared helper, never from a
# path this script invents. scripts/lib/tmp-ban.sh is the ONE definition of the
# contract; gopi_scratch_mkdir creates the per-run directory under a root it has
# ASSERTED off `/` by device id (Rule 22 — the criterion is the device, not the
# spelling, so "$TMPDIR" pointing at /tmp was the same defect) and registers it
# for trap-time removal, which is what lets --keep hand the operator the path.
#
# shellcheck source=lib/tmp-ban.sh
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
. "$HERE/../lib/tmp-ban.sh" || {
  echo "tmp-ban: scripts/lib/tmp-ban.sh missing" >&2
  exit 2
}
SB="$(gopi_scratch_mkdir chat-replay-xtest)" || {
  echo "chat_replay_xtest: the scratch root is unavailable" >&2
  exit 2
}
trap '[ "$KEEP" = 1 ] || gopi_scratch_cleanup' EXIT INT TERM

DISPLAY_NUM="${GOPI_XTEST_DISPLAY:-:96}"
DUMP="$SB/chat-screen.dump"
PTYLOG="$SB/pty.log"
EXITFILE="$SB/exit-code"
REC="$SB/session.jsonl"

fail() {
  echo "FAIL: $*" >&2
  echo "--- pty log tail ---" >&2
  tail -c 1500 "$PTYLOG" 2>/dev/null >&2
  echo "--- scratch (kept): $SB" >&2
  KEEP=1
  exit 1
}

# ---- 1. the private display -------------------------------------------------
Xvfb "$DISPLAY_NUM" -screen 0 1200x420x24 >"$SB/xvfb.log" 2>&1 &
for _ in $(seq 1 60); do
  DISPLAY="$DISPLAY_NUM" "$BIN" tool act info >/dev/null 2>&1 && break
  sleep 0.1
done
DISPLAY="$DISPLAY_NUM" "$BIN" tool act info >/dev/null 2>&1 || fail "the private display $DISPLAY_NUM never answered"

# ---- 2. the recording and a private profile ---------------------------------
# GOPI_HOME is a HOME, not a data root: the profile (settings, sessions, ledger)
# is <GOPI_HOME>/.gopi. Handing it <SB>/home/.gopi resolved one level deeper —
# into <SB>/home/.gopi/.gopi — so the run read an empty profile while the one a
# reader resolves stayed untouched; config.LookupSandboxProfileMisuse now refuses
# that spelling outright.
export GOPI_HOME="$SB/home"
mkdir -p "$GOPI_HOME"
{
  for i in $(seq 1 12); do
    echo "{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":\"recorded turn $i: an operator prompt long enough that the driver spends real time typing it into the editor\"}}"
    echo "{\"type\":\"assistant\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"answer $i: the recorded reply to that prompt\"}]}}"
    echo '{"type":"request","cache_hit":1,"cache_miss":2,"prompt_tokens":3}'
  done
} >"$REC"

# ---- 3. the real terminal, with the run's exit code recorded -----------------
# THE EXIT CODE IS THE DEFECT-① EVIDENCE, so the shell that owns the process
# writes it: which code the run chose is the difference between "the watcher left"
# (0) and "the driver failed" (1), and a process list cannot tell the two apart.
DISPLAY="$DISPLAY_NUM" setsid xterm -geometry 120x22 -T GOPI-XTEST \
  -l -lf "$PTYLOG" \
  -e bash -c '"$1" chat --replay "$2" --dump-screen="$3"; echo "$?" >"$4"' \
  _ "$BIN" "$REC" "$DUMP" "$EXITFILE" &

# ---- the injection helpers --------------------------------------------------
# Keys go to the window the pointer is inside: a bare Xvfb reports PointerRoot
# focus, so the POINTER is what routes them (internal/shot's documented
# behaviour). The pointer never leaves the terminal's rectangle.
press() {
  DISPLAY="$DISPLAY_NUM" "$BIN" tool act move 300 130 >/dev/null
  DISPLAY="$DISPLAY_NUM" "$BIN" tool act key --key "$1" >/dev/null || fail "injecting key $1"
  sleep 0.9
}
press_rune() {
  DISPLAY="$DISPLAY_NUM" "$BIN" tool act move 300 130 >/dev/null
  DISPLAY="$DISPLAY_NUM" "$BIN" tool act type --text "$1" >/dev/null || fail "typing $1"
  sleep 0.9
}

# THE OBSERVATION PRIMITIVES. Two instrument facts are handled here, both
# measured rather than assumed, and each would otherwise report a feature broken
# while it works:
#
#   1. the pty log WRAPS at the terminal's width, so a long OSD sentence arrives
#      split across log lines;
#   2. the log holds the terminal's ESCAPE STREAM, not just text, so cursor
#      positioning lands BETWEEN words of the very sentence being matched
#      (measurably: "watcher left the replay" arrives with a `[85C` in the middle).
#
# So every needle is compared against a projection with the escapes removed and
# all whitespace dropped. A needle that is genuinely absent stays absent — the
# normalisation hides the instrument, never a missing line.
esc_strip() {
  sed -e 's/\x1b\[[0-9;?]*[A-Za-z]//g' \
    -e 's/\x1b\][^\x07]*\x07//g' \
    -e 's/\x1b[()][A-Z0-9]//g' \
    -e 's/\x1b[=>]//g'
}
log_text() { [ -f "$PTYLOG" ] && tr -d '\000' <"$PTYLOG" || printf ''; }
log_flat() { log_text | esc_strip | tr -d '[:space:]'; }
flat_of() { printf '%s' "$1" | sed -e 's/\x1b\[[0-9;?]*[A-Za-z]//g' | tr -d '[:space:]'; }

expect_log() {
  local label="$1" want="$2" got
  got=$(log_flat)
  case "$got" in
  *"$(flat_of "$want")"*) echo "  ok   $label" ;;
  *) fail "$label: the run never wrote \"$want\"" ;;
  esac
}

# ---- 4. wait for the surface to BE a replay ---------------------------------
for _ in $(seq 1 160); do
  case "$(log_flat)" in
  *"playback:"*) break ;;
  esac
  sleep 0.25
done
expect_log "startup: the replay announced its controls" "playback: space pause/resume"

# ---- 5. every control key, one at a time ------------------------------------
echo "== real XTest keys, one at a time =="
press space
expect_log "space → PAUSED" "PAUSED at beat"
press space
expect_log "space → resumed" "resumed at beat"
press left
expect_log "left → step back" "step back"
press right
expect_log "right → step forward" "step forward"
press up
expect_log "up → jumped back" "jumped back"
press down
expect_log "down → jumped forward" "jumped forward"
press_rune '?'
expect_log "? → the keys line again" "jump 5 beats"

# ---- 6. defect ① — the watcher's quit ENDS the run --------------------------
echo "== defect ①: q must END the replay, not just stop its typing =="
press_rune 'q'
expect_log "q → the watcher left" "the watcher left the replay"

for _ in $(seq 1 80); do
  [ -s "$EXITFILE" ] && break
  sleep 0.25
done
[ -s "$EXITFILE" ] || fail "the replay did not exit after q — the confusing mode (defect ①) is still live: a process parked at a prompt no line can reach"
CODE=$(tr -d '[:space:]' <"$EXITFILE")
expect_log "the driver reported the leave as a LEAVE" "the replay ended at the watcher's request"
case "$CODE" in
0) echo "  ok   the replay exited ON ITS OWN with the watcher's own code 0 (not the driver-failure code 1)" ;;
1) fail "the run exited 1: the watcher's leave was reported as a driver FAILURE" ;;
*) fail "the run exited with code $CODE" ;;
esac

echo "ALL XTEST CHECKS PASSED — scratch: $SB"
