#!/usr/bin/env bash
# chat-ui-observe.sh — SEE and MEASURE the `gopi chat` terminal UI in ONE command.
#
# WHAT IT IS FOR. A change to the chat chrome (the status row, the prompt, the
# banner) is a change to PIXELS, and those pixels are produced by a terminal
# emulator, not by our code. This script closes the loop: it boots the chat in a
# private terminal on a private display, grabs the screen with the shipped
# `gopi capture` verb (the same X11 GetImage client the agent's screen_capture
# tool uses), and measures every text band of the capture.
#
# WHAT IT PRINTS (the four things a UI claim needs):
#   1. png        — the artifact path
#   2. sha256_16  — capture identity, from `gopi capture` itself: run it before
#                   and after a change and the hashes say whether the screen moved
#   3. bg         — the dominant background colour of the capture
#   4. per-band   — for EVERY text band: rows, background, brightest pixel and
#                   its WCAG contrast, median pixel and its contrast, and the
#                   CORE PIXEL FRACTION (share of text pixels at >=90% of the
#                   band's own maximum contrast).
#
# WHY CORE FRACTION. WCAG contrast is computed from the brightest pixel of a
# glyph (the stroke centre). A dim colour lowers that maximum; a light face with
# wide anti-aliasing lowers the FRACTION of pixels near it. "Hard to read" can be
# either, and the two need different fixes — so both are measured. See
# devdoc/chat-ui-self-inspection.md for the instrument's blindness.
#
# THE BAND ORDER IS THE SCREEN ORDER: the last band is the LAST text line the
# surface drew, which for `gopi chat` is the status row (it is painted on the
# region's reserved bottom row, below the editing line). `--bands N` narrows the
# table to the last N bands; `--band-row` prints the last band only.
#
# Usage:
#   scripts/ui/chat-ui-observe.sh --bin ./bin/gopi --out /path/after.png
#   scripts/ui/chat-ui-observe.sh --bin ./bin/gopi --out /path/after.png --bands 3
#
# Exit status is the measurement's: non-zero only if the chat never painted or
# no band was found (a capture with no text is not a measurement).
set -euo pipefail

# Rule 22: the probe scratch comes from scripts/lib/tmp-ban.sh — the ONE
# reader of the scratch root, asserted off `/` by DEVICE ID and registered
# for trap-time cleanup. The X11 SOCKET directory is the ONE /tmp path this
# repository keeps (internal/shot/x11.go keeps the same one in Go): a unix
# socket there is created by the X server, never written by us, so it is not
# scratch and the off-/ rule does not apply to it. Both come from the helper,
# so neither is spelled here.
. "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/../lib/tmp-ban.sh"
X11SOCKDIR="$(gopi_x11_socket_dir)"
PROBEDIR="$(gopi_scratch_mkdir x11-probe)" || exit 1
trap 'gopi_scratch_cleanup' EXIT

bin=""
out=""
bands=0
seconds=8
chat_args=()
# The terminal's OWN colours are a measurement input, not a convenience: SGR 90
# ("bright black") and SGR 2 (dim) are resolved BY THE TERMINAL against its
# background, so the same byte is 5.6:1 on the dark theme and 3.9:1 on a light
# one. The defaults below are the theme this machine's gopi chat actually runs
# in (gnome-terminal, Ubuntu dark): background #300A24, foreground #FFFFFF —
# read out of a live capture of the operator's own window. Keep the two in sync
# with that terminal, or the measurement is of a different screen than the one
# the complaint is about (see devdoc/chat-ui-self-inspection.md).
term_bg="#300A24"
term_fg="#FFFFFF"

# The terminal geometry and font the measurement runs at. The status row's
# COLUMN layout is width-dependent (see lineedit's layoutCols), so the geometry
# is part of the measurement's identity and is printed with the result.
geometry="120x34"
font_size=11

while [ $# -gt 0 ]; do
  case "$1" in
    --bin) bin="$2"; shift 2 ;;
    --out) out="$2"; shift 2 ;;
    --bands) bands="$2"; shift 2 ;;
    --seconds) seconds="$2"; shift 2 ;;
    --geometry) geometry="$2"; shift 2 ;;
    --font-size) font_size="$2"; shift 2 ;;
    --chat-arg) chat_args+=("$2"); shift 2 ;;
    *) echo "chat-ui-observe: unknown argument $1" >&2; exit 2 ;;
  esac
done
[ -n "$bin" ] || { echo "chat-ui-observe: --bin is required" >&2; exit 2; }
[ -n "$out" ] || { echo "chat-ui-observe: --out is required" >&2; exit 2; }
[ -x "$bin" ] || { echo "chat-ui-observe: $bin is not executable" >&2; exit 2; }

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
measure="$script_dir/chat-ui-measure.py"

# Every temporary path lives off the / partition (Rule 22): scripts/lib/tmp-ban.sh
# is the ONE reader of the scratch root (GOPI_TEST_SCRATCH_HOME, else
# GOPI_TEST_HOME's workspace — the tree the Go suite GC sweeps), and
# gopi_scratch_mkdir asserts by DEVICE ID that what it hands back is off `/`,
# registering it for trap-time cleanup. /tmp shares `/`'s device here, so it is
# never a candidate and there is no fallback to it.
. "$script_dir/../lib/tmp-ban.sh"
work="$(gopi_scratch_mkdir chat-ui-observe)" || exit 1

# A UTF-8 locale is required or the CLIs' UTF-8 chrome (the em-dash, the ⚙ tool
# glyph) is rendered as mojibake and the measurement is of the wrong glyphs.
utf8_locale=""
for cand in C.UTF-8 C.utf8 en_US.UTF-8; do
  if locale -a 2>/dev/null | grep -qx "$cand"; then utf8_locale="$cand"; break; fi
done
[ -n "$utf8_locale" ] || { echo "chat-ui-observe: no UTF-8 locale available" >&2; exit 1; }

# A private display per run: a capture must never read another window's pixels,
# and two runs must not fight over one display number.
display_num=""
for cand in 97 96 95 94 93; do
  if ! [ -e ""$X11SOCKDIR"/X$cand" ]; then display_num=":$cand"; break; fi
done
[ -n "$display_num" ] || { echo "chat-ui-observe: no free display in :93-:97" >&2; exit 1; }

xvfb_pid=""
xterm_pid=""
cleanup() {
  [ -n "$xterm_pid" ] && kill "$xterm_pid" 2>/dev/null || true
  [ -n "$xvfb_pid" ] && kill "$xvfb_pid" 2>/dev/null || true
  # The helper's own cleanup: kill the children this shell started, then remove
  # every scratch directory it registered — $work included.
  gopi_scratch_cleanup
}
trap cleanup EXIT

mkdir -p "$(dirname "$out")"

# Chat arguments are joined for the shell INSIDE the terminal (xterm's -e takes
# one command string). Each is quoted, so a flag with a space survives; the
# array itself is emptied into a space-joined literal, not eval'd.
chat_args_str=""
for a in "${chat_args[@]:-}"; do
  [ -n "$a" ] || continue
  chat_args_str="$chat_args_str '$a'"
done

Xvfb "$display_num" -screen 0 1600x900x24 >/dev/null 2>&1 &
xvfb_pid=$!
for _ in $(seq 1 40); do [ -e ""$X11SOCKDIR"/X${display_num#:}" ] && break; sleep 0.1; done

# The chat runs in a throwaway HOME with no credentials: this observes the
# CHROME, and a real session would send the operator's traffic. GOPI_SETTINGS
# empty means the settings table is absent, so no API key and no repo build
# command are picked up.
home="$work/home"
mkdir -p "$home"
: > "$work/env.txt"
envfile="$work/env.txt"
{
  echo "export HOME='$home'"
  echo "export GOPI_HOME='$home'"
  echo "export GOPI_SETTINGS='$home/settings.json'"
  echo "export PATH='$PATH'"
} > "$envfile"

# The chat surface itself: no model call is needed to observe the status row —
# booting the prompt paints the banner, the editing line and the status row.
LANG="$utf8_locale" LC_ALL="$utf8_locale" DISPLAY="$display_num" \
  xterm -u8 -geometry "$geometry" -fa Monospace -fs "$font_size" \
  -bg "$term_bg" -fg "$term_fg" \
  -e sh -c ". '$envfile'; cd '$work'; exec '$bin' chat $chat_args_str" &
xterm_pid=$!

sleep "$seconds"

json="$(DISPLAY="$display_num" gopi tool capture --display "$display_num" --screen --out "$out")"
png="$(printf '%s' "$json" | python3 -c 'import sys,json;print(json.load(sys.stdin)["path"])')"
sha="$(printf '%s' "$json" | python3 -c 'import sys,json;print(json.load(sys.stdin)["sha256_16"])')"

# The whole display is captured (the chat's own window is the only window on a
# private display), so the measurement sees exactly the chat's pixels.
echo "chat-ui-observe: png=$png sha256_16=$sha display=$display_num geometry=$geometry"

python3 "$measure" "$png" --max-bands "$bands"
