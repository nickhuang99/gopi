#!/bin/bash
# repro-arm-x11.sh — headed-Chrome-via-X11-only actuation probe (no CDP at all).
#
# Runs ONE arm on the remotebox host:
#   * its own Xvfb display (default :97 — NEVER :99, which is production)
#   * a headed google-chrome with NO --remote-debugging-port (Arm B forbids CDP:
#     the production gateway holds the only ws connection per target)
#   * egress family chosen by --host-resolver-rules (IPV6 or IPV4), so the IP
#     stays constant while only the ACTUATION METHOD differs from the CDP arm
#   * all input via XTEST (xdotool): windowraise + windowfocus (XSetInputFocus),
#     ctrl+l, type, Return, F5. No window manager exists on this host, so
#     windowactivate/_NET_ACTIVE_WINDOW is unavailable.
#   * window TITLES are polled to a log — a machine-readable per-arm signal
#     (a SERP title carries the query; a wall's title is the bare URL).
#
# Usage: bash repro-arm-x11.sh [IPV6|IPV4] [DISPLAY_NUM] [OUTDIR]
# Temp root: REMOTE="$PROBEDIR"/repro-arm-x11 — this host's /tmp is tmpfs on its own
# device (measured: stat -c %d differs from /), i.e. off the / partition (Rule 22).
set -u

# Rule 22: the probe scratch comes from scripts/lib/tmp-ban.sh — the ONE
# reader of the scratch root, asserted off `/` by DEVICE ID and registered
# for trap-time cleanup. The X11 SOCKET directory is the ONE /tmp path this
# repository keeps (internal/shot/x11.go keeps the same one in Go): a unix
# socket there is created by the X server, never written by us, so it is not
# scratch and the off-/ rule does not apply to it. Both come from the helper,
# so neither is spelled here.
. "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/../../../scripts/lib/tmp-ban.sh"
X11SOCKDIR="$(gopi_x11_socket_dir)"
PROBEDIR="$(gopi_scratch_mkdir x11-probe)" || exit 1
trap 'gopi_scratch_cleanup' EXIT
FAMILY="${1:-IPV6}"
DISP="${2:-:97}"
OUT="${3:-"$PROBEDIR"/repro-arm-x11}"

PROFILE="$OUT/profile-$FAMILY"
mkdir -p "$OUT"

# measured egress targets (curl from this host; re-measure before trusting)
V4_IP="142.251.156.119"
V6_IP="[2001:4860:482a:7700::]"
if [ "$FAMILY" = "IPV4" ]; then
  MAP="MAP www.google.com $V4_IP,MAP google.com $V4_IP"
else
  MAP="MAP www.google.com $V6_IP,MAP google.com $V6_IP"
fi

# teardown of THIS arm only (patterns never match the production profile/display)
pkill -9 -f "user-data-dir=$PROFILE" 2>/dev/null
pkill -9 -f "Xvfb $DISP " 2>/dev/null
sleep 2
rm -rf "$PROFILE"; mkdir -p "$PROFILE"

setsid nohup Xvfb "$DISP" -screen 0 1280x1024x24 >"$OUT/xvfb-$FAMILY.log" 2>&1 &
sleep 2
export DISPLAY="$DISP"

setsid nohup google-chrome \
  --no-sandbox --disable-dev-shm-usage \
  --disable-blink-features=AutomationControlled \
  --exclude-switches=enable-automation \
  --no-first-run --no-default-browser-check --disable-default-apps \
  --disable-quic \
  --host-resolver-rules="$MAP" \
  --user-data-dir="$PROFILE" \
  --window-position=0,0 --window-size=1280,1000 \
  "https://www.google.com/" >"$OUT/chrome-$FAMILY.log" 2>&1 &

TLOG="$OUT/titles-$FAMILY.log"
: >"$TLOG"

# poll the browser window title: the machine-readable signal for this probe
poll_titles() {  # $1 = phase label, $2 = seconds
  local label="$1" n="$2" i=0 w t
  while [ "$i" -lt "$n" ]; do
    for w in $(xdotool search --name "." 2>/dev/null); do
      t=$(xdotool getwindowname "$w" 2>/dev/null)
      case "$t" in
        ""|"Chromium clipboard"|"google-chrome") ;;
        *) echo "[$FAMILY $label t+${i}s wid=$w] $t" >>"$TLOG" ;;
      esac
    done
    i=$((i + 1)); sleep 1
  done
}

main_wid() {  # the loaded browser window id (title not a placeholder)
  local w t
  for w in $(xdotool search --name "." 2>/dev/null); do
    t=$(xdotool getwindowname "$w" 2>/dev/null)
    case "$t" in
      ""|"Chromium clipboard"|"google-chrome") ;;
      *) echo "$w"; return ;;
    esac
  done
}

echo "=== $FAMILY on $DISP ; map=$MAP ==="
poll_titles "landing" 12
WID=$(main_wid)
echo "[$FAMILY] loaded_wid=$WID title=[$(xdotool getwindowname "$WID" 2>/dev/null)]"
[ -n "$WID" ] && { xdotool windowraise "$WID"; xdotool windowfocus "$WID"; }
import -window root "$OUT/$FAMILY-01-landing.png" 2>/dev/null

# actuation #1: ctrl+l (focus the omnibox) + type + Return — the reliable path
xdotool key --clearmodifiers ctrl+l; sleep 1
xdotool type --clearmodifiers --delay 40 "golang context deadline"
sleep 1
import -window root "$OUT/$FAMILY-02-typed.png" 2>/dev/null
xdotool key --clearmodifiers Return
poll_titles "post-enter" 15
import -window root "$OUT/$FAMILY-03-verdict.png" 2>/dev/null

# actuation #2: F5 reload — is the wall stable under a fresh navigation?
xdotool key --clearmodifiers F5
poll_titles "post-f5" 10
import -window root "$OUT/$FAMILY-04-after-f5.png" 2>/dev/null

echo "=== $FAMILY title log ==="
cat "$TLOG"
echo "=== $FAMILY artifacts ==="
ls -la "$OUT" | grep -i "$FAMILY"
echo "[$FAMILY] chrome_pid=$(pgrep -f "user-data-dir=$PROFILE" | head -1)"
