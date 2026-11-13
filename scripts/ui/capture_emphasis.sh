#!/usr/bin/env bash
# capture_emphasis.sh — render the chat surfaces' emphasis into a PNG, and dump
# the attribute grid beside it.
#
# What it does, and why each step exists:
#
#   1. Runs the palette capture emitter (internal/lineedit) to write the REAL
#      renderers' byte stream twice: with the colour capability on and off. The
#      bytes are produced by the shipped code, never hand-written.
#   2. Plays each capture under a real terminal (xterm on a private Xvfb display),
#      so the PNG is what a user would see — the terminal does the attribute
#      rendering, not our test code.
#   3. Screenshots the xterm window to a PNG (pixel truth, engineering rule 10.2).
#   4. Dumps the SGR attribute spans of the capture as text, column by column, so
#      the emphasis is legible as DATA as well as as an image — an image alone
#      cannot distinguish "the label is grey" from "the label is grey and the live
#      hint is too".
#
# The PNG is never the only evidence: the byte-exact assertions live in
# internal/lineedit/palette_test.go, and the attribute dump is the machine-
# readable half of this script's output.
#
# Usage: scripts/ui/capture_emphasis.sh <out-dir>
set -euo pipefail

out_dir="${1:?usage: capture_emphasis.sh <out-dir>}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# Every temporary path lives off the / partition (Rule 22). The scratch ROOT is
# not spelled here: scripts/lib/tmp-ban.sh is the ONE reader of it
# (GOPI_TEST_SCRATCH_HOME, else GOPI_TEST_HOME's workspace — the same tree the
# Go suite GC sweeps), and gopi_scratch_mkdir asserts by DEVICE ID that the
# directory it hands back is off `/` and registers it for trap-time cleanup.
# /tmp shares `/`'s device on this host, so it is never a candidate and there is
# no silent fallback to it.
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$here/../lib/tmp-ban.sh"
work="$(gopi_scratch_mkdir palette-capture)" || exit 1
display_num=":99"

# cap_go_timeout is the per-package budget of the ONE `go test` below. Named
# here (Rule 1a/2), GOPI_TEST_TIMEOUT its only override — the same default
# scripts/test-budget.sh declares, so the whole tree names ONE number. Without
# it the call inherits Go's 10m cap, which a loaded host trips.
cap_go_timeout="${GOPI_TEST_TIMEOUT:-25m}"

# A UTF-8 locale must exist for the terminal to render the CLIs' UTF-8 output
# (em-dash, the tool glyph). The script refuses rather than producing an image
# full of mojibake it would have to explain.
utf8_locale=""
for cand in C.UTF-8 C.utf8 en_US.UTF-8; do
  if locale -a 2>/dev/null | grep -qx "$cand"; then utf8_locale="$cand"; break; fi
done
if [ -z "$utf8_locale" ]; then
  echo "capture_emphasis: no UTF-8 locale available — the capture would be mojibake" >&2
  exit 1
fi

cleanup() {
  [ -n "${xterm_pid:-}" ] && kill "$xterm_pid" 2>/dev/null || true
  [ -n "${xvfb_pid:-}" ] && kill "$xvfb_pid" 2>/dev/null || true
  # The helper's own cleanup: kill the children this shell started (they may
  # still be writing into the scratch dir) and remove every directory it
  # registered — $work included.
  gopi_scratch_cleanup
}
trap cleanup EXIT

mkdir -p "$out_dir" "$work"

# 1. the real byte streams
( cd "$repo_root" && GOPI_PALETTE_CAPTURE_DIR="$work" \
    go test ./internal/lineedit/ -run TestPaletteCaptureGolden -count=1 -timeout "$cap_go_timeout" >/dev/null )

# 2. a private display and a terminal to play them in
Xvfb "$display_num" -screen 0 1024x768x24 >/dev/null 2>&1 &
xvfb_pid=$!
sleep 1

for pair in "colour:$work/capture-colour.ansi" "mono:$work/capture-mono.txt" "chrome:$work/capture-chrome.ansi"; do
  name="${pair%%:*}"
  file="${pair#*:}"
  png="$out_dir/palette-$name.png"

  # -u8 AND a UTF-8 locale: -u8 tells xterm the INPUT is UTF-8, but the LC_CTYPE
  # the child inherits decides the font/encoding table it renders with. Under
  # the default C locale the em-dash and the tool glyph come out as mojibake —
  # a locale defect the image would misattribute to the renderer.
  LANG="$utf8_locale" LC_ALL="$utf8_locale" DISPLAY="$display_num" \
    xterm -u8 -geometry 120x34 -fa Monospace -fs 11 \
    -bg '#101014' -fg '#d0d0d0' -e sh -c "cat '$file'; sleep 30" &
  xterm_pid=$!
  sleep 2

  DISPLAY="$display_num" import -window root "$png"
  kill "$xterm_pid" 2>/dev/null || true
  wait "$xterm_pid" 2>/dev/null || true
  xterm_pid=""

  echo "capture_emphasis: wrote $png"
done

# 3. the attribute dump: every SGR span of the colour capture, in order, as text.
dump="$out_dir/palette-attributes.txt"
{
  echo "# SGR attribute spans of the chat capture (colour-capable run)."
  echo "# Each line: the SGR sequence, then the text it wraps. Produced from the"
  echo "# rendered bytes, not from the palette's constants."
  awk 'BEGIN { RS="\x1b\\[" ; ORS="" } NR>1 {
        i = index($0, "m")
        if (i > 0) { print "SGR[" substr($0,1,i-1) "] " substr($0,i+1) "\n" }
        else { print substr($0, 1) }
      }' "$work/capture-colour.ansi"
} > "$dump"
echo "capture_emphasis: wrote $dump"
