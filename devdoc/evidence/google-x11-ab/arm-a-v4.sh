#!/bin/bash
# arm-a-v4.sh — ARM A at IPv4: the missing cell of the 2x2 (CDP actuation, IPv4 egress).
#
# Arm A on search-bot is CDP + IPv6 (production). Arm B/C are X11 + (IPv6/IPv4).
# Without CDP@IPv4 the matrix cannot separate ACTUATION METHOD from EGRESS FAMILY.
# This runs the SAME CDP instrument (arm-a-cdp-probe.py, using the gateway's own
# CDPConn) against a second Chrome forced to IPv4, on ITS OWN display (:96), own
# profile, own debug port (:9223 — :9222 belongs to production).
#
# Protocol (same as ab-run-v3.sh): production is paused for the run because
# search-bot has 957MB RAM and a second concurrent Chrome already caused one
# global OOM; a trap restores production on EVERY exit path.
#
# Usage: bash arm-a-v4.sh [TAG]
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
TAG="${1:-r1}"
DISP=":96"
PORT=9223
OUT=""$PROBEDIR"/armav4"
PROFILE="$OUT/profile-$TAG"
MAP="MAP www.google.com 142.251.156.119,MAP google.com 142.251.156.119"
mkdir -p "$OUT"

restore() {
  echo "=== RESTORE (trap) ==="
  pkill -9 -f "user-data-dir=$PROFILE" 2>/dev/null
  pkill -9 -f "Xvfb $DISP " 2>/dev/null
  sleep 2
  rm -f "$X11SOCKDIR"/X96
  systemctl start google-cdp.service
  sleep 8
  echo "restore_service=$(systemctl is-active google-cdp.service)"
  echo "restore_health=$(curl -s -m 10 http://127.0.0.1:9232/health)"
}
trap restore EXIT

echo "=== PAUSE PRODUCTION ==="
systemctl stop google-cdp.service
sleep 3
pkill -9 -f "user-data-dir="$PROBEDIR"/chromeprofile" 2>/dev/null
pkill -9 -f "Xvfb :99 " 2>/dev/null
sleep 2
echo "prod_active=$(systemctl is-active google-cdp.service)"
echo "prod_chrome_left=$(pgrep -fc 'user-data-dir="$PROBEDIR"/chromeprofile' 2>/dev/null || echo 0)"

echo "=== EGRESS MEASUREMENT ($TAG CDP-IPV4) ==="
curl -4 -s -o /dev/null -w "curl_v4 remote_ip=%{remote_ip} http=%{http_code}\n" \
  "https://www.google.com/search?q=golang+context+deadline"

pkill -9 -f "user-data-dir=$PROFILE" 2>/dev/null
rm -rf "$PROFILE"; mkdir -p "$PROFILE"
setsid nohup Xvfb "$DISP" -screen 0 1280x1024x24 >"$OUT/xvfb-$TAG.log" 2>&1 &
sleep 2
export DISPLAY="$DISP"
setsid nohup google-chrome --no-sandbox --disable-dev-shm-usage \
  --remote-debugging-port=$PORT \
  --disable-blink-features=AutomationControlled \
  --exclude-switches=enable-automation \
  --no-first-run --no-default-browser-check --disable-default-apps \
  --disable-quic --host-resolver-rules="$MAP" \
  --user-data-dir="$PROFILE" --window-position=0,0 --window-size=1280,1000 \
  "about:blank" >"$OUT/chrome-$TAG.log" 2>&1 &
unset DISPLAY

# wait for the debug endpoint (max 30s), then verify the FORCED family before probing
i=0
until curl -s -m 3 "http://127.0.0.1:$PORT/json/version" >/dev/null 2>&1; do
  i=$((i+1)); [ "$i" -ge 15 ] && { echo "CDP endpoint never came up"; break; }
  sleep 2
done
echo "cdp_version=$(curl -s -m 3 http://127.0.0.1:$PORT/json/version | head -c 200)"

python3 "$PROBEDIR"/arm-a-cdp-probe.py --cdp-port $PORT --label "$TAG-CDP-IPV4" \
  --warmup --query "golang context deadline" >"$OUT/probe-$TAG-CDP-IPV4.json" 2>"$OUT/probe-$TAG.err"
echo "probe_rc=$?"
cat "$OUT/probe-$TAG-CDP-IPV4.json"
echo "--- stderr ---"; tail -5 "$OUT/probe-$TAG.err"

# X11 screenshot of the SAME window, so this cell carries a pixel artifact too
export DISPLAY="$DISP"
WID=$(xdotool search --name "." 2>/dev/null | while read w; do
        t=$(xdotool getwindowname "$w" 2>/dev/null)
        case "$t" in ""|"Chromium clipboard"|"google-chrome") ;; *) echo "$w"; break ;; esac
      done | head -1)
echo "[$TAG-CDP-IPV4] wid=$WID title=[$(xdotool getwindowname "$WID" 2>/dev/null)]"
xdotool windowraise "$WID" 2>/dev/null
import -window root "$OUT/$TAG-CDP-IPV4-verdict.png" 2>/dev/null
unset DISPLAY
echo "=== ARM A IPv4 DONE ==="
