#!/bin/bash
# armb_up.sh — ARM B of the headed-X11 actuation A/B.
#
# Starts a SECOND, fully independent headed Chrome for the X11-only arm:
#   * separate display    :98  (production owns :99 — never touched)
#   * separate profile    "$PROBEDIR"/armb-profile
#   * NO --remote-debugging-port  (CDP must not be used for this arm; the
#     production gateway holds the only ws connection per target anyway)
#   * SAME IPv6 egress as Arm A, via the identical --host-resolver-rules map
#     (constant-IP isolation: only the actuation method differs)
#
# Usage: bash armb_up.sh [DISPLAY_NUM] [IPV4|IPV6]
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
DISPLAY_NUM="${1:-:98}"
FAMILY="${2:-IPV6}"
# one fresh profile per arm family: no cookie/cache carry-over between arms
PROFILE=""$PROBEDIR"/armb-profile-${FAMILY}"
V4_MAP="MAP www.google.com 142.251.156.119,MAP google.com 142.251.156.119"
V6_MAP="MAP www.google.com [2001:4860:482a:7700::],MAP google.com [2001:4860:482a:7700::]"
if [ "$FAMILY" = "IPV4" ]; then MAP="$V4_MAP"; else MAP="$V6_MAP"; fi

pkill -9 -f "Xvfb $DISPLAY_NUM" 2>/dev/null
pkill -9 -f "user-data-dir=$PROFILE" 2>/dev/null
sleep 2
rm -rf "$PROFILE"; mkdir -p "$PROFILE"
setsid nohup Xvfb "$DISPLAY_NUM" -screen 0 1280x1024x24 >"$PROBEDIR"/armb-xvfb.log 2>&1 &
sleep 2
export DISPLAY="$DISPLAY_NUM"
setsid nohup google-chrome \
  --no-sandbox --disable-dev-shm-usage \
  --disable-blink-features=AutomationControlled \
  --exclude-switches=enable-automation \
  --no-first-run --no-default-browser-check --disable-default-apps \
  --disable-quic \
  --host-resolver-rules="$MAP" \
  --user-data-dir="$PROFILE" \
  --window-position=0,0 --window-size=1280,1000 \
  "https://www.google.com/" >"$PROBEDIR"/armb-chrome.log 2>&1 &

echo "family=$FAMILY map=$MAP"
echo "waiting for a LOADED window on $DISPLAY_NUM ..."
# wait for a Chrome window that is past the blank "Untitled" phase (page loaded)
WID=""
for _ in $(seq 1 40); do
  sleep 1
  for w in $(DISPLAY=$DISPLAY_NUM xdotool search --name "Chrome" 2>/dev/null); do
    t=$(DISPLAY=$DISPLAY_NUM xdotool getwindowname "$w" 2>/dev/null)
    case "$t" in ""|*Untitled*) ;; *) WID=$w; break;; esac
  done
  [ -n "$WID" ] && break
done
echo "loaded_wid=$WID"
echo "=== windows on $DISPLAY_NUM ==="
for w in $(DISPLAY=$DISPLAY_NUM xdotool search --name "." 2>/dev/null); do
  echo "wid=$w name=[$(DISPLAY=$DISPLAY_NUM xdotool getwindowname "$w" 2>/dev/null)]"
done
echo "=== chrome pid ==="
pgrep -f "user-data-dir=$PROFILE" | head -1
