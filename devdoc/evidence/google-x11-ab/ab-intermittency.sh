#!/bin/bash
# ab-intermittency.sh — is the /sorry wall PERMANENT for an IP, or does it vary
# per request? One Chrome, one constant IP, N sequential navigations.
#
# WHY THIS DECIDES THE EPIC: a single passing run (X11@IPv4, round r1) is either
#   H1 evidence that X11 actuation defeats the block, or
#   H2 an intermittent pass that any actuation method gets by chance.
# If the SAME Chrome at the SAME IP cycles between SERP and /sorry across plain
# F5 reloads — no actuation change involved — then H2 is the explanation and H1
# needs no further explanation. The reload is the control: it holds actuation
# constant and varies only TIME.
#
# Usage: bash ab-intermittency.sh [FAMILY] [N]     default IPV4 10
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
FAMILY="${1:-IPV4}"
N="${2:-10}"
OUT=""$PROBEDIR"/abint"
DISP=":95"
PROFILE="$OUT/profile-$FAMILY"
QUERY="golang context deadline"
V4_IP="142.251.156.119"
V6_IP="[2001:4860:482a:7700::]"
mkdir -p "$OUT"
TLOG="$OUT/reload-$FAMILY.log"
: >"$TLOG"

if [ "$FAMILY" = "IPV4" ]; then
  MAP="MAP www.google.com $V4_IP,MAP google.com $V4_IP"
else
  MAP="MAP www.google.com $V6_IP,MAP google.com $V6_IP"
fi

restore() {
  echo "=== RESTORE (trap) ==="
  pkill -9 -f "user-data-dir=$PROFILE" 2>/dev/null
  pkill -9 -f "Xvfb $DISP " 2>/dev/null
  sleep 2
  rm -f "$X11SOCKDIR"/X95
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

pkill -9 -f "user-data-dir=$PROFILE" 2>/dev/null
pkill -9 -f "Xvfb $DISP " 2>/dev/null
sleep 2
rm -rf "$PROFILE"; mkdir -p "$PROFILE"
setsid nohup Xvfb "$DISP" -screen 0 1280x1024x24 >"$OUT/xvfb.log" 2>&1 &
sleep 2
export DISPLAY="$DISP"
setsid nohup google-chrome --no-sandbox --disable-dev-shm-usage \
  --disable-blink-features=AutomationControlled \
  --exclude-switches=enable-automation \
  --no-first-run --no-default-browser-check --disable-default-apps \
  --disable-quic --host-resolver-rules="$MAP" \
  --user-data-dir="$PROFILE" --window-position=0,0 --window-size=1280,1000 \
  "https://www.google.com/" >"$OUT/chrome.log" 2>&1 &

main_wid() {
  local w t
  for w in $(xdotool search --name "." 2>/dev/null); do
    t=$(xdotool getwindowname "$w" 2>/dev/null)
    case "$t" in ""|"Chromium clipboard"|"google-chrome") ;; *) echo "$w"; return ;; esac
  done
}
i=0
while [ "$i" -lt 20 ]; do sleep 1; [ -n "$(main_wid)" ] && break; i=$((i+1)); done
WID=$(main_wid)
xdotool windowraise "$WID"; xdotool windowfocus "$WID"

# actuation #0: one typed search to put the browser on the /search URL
xdotool key --clearmodifiers ctrl+l; sleep 1
xdotool type --clearmodifiers --delay 40 "$QUERY"; sleep 1
xdotool key --clearmodifiers Return; sleep 12

serp=0; wall=0
for k in $(seq 1 "$N"); do
  T=$(xdotool getwindowname "$(main_wid)" 2>/dev/null)
  case "$T" in
    *"$QUERY"*"Google Search"*) kind=SERP; serp=$((serp+1)) ;;
    *) kind=WALL; wall=$((wall+1)) ;;
  esac
  echo "[$FAMILY reload#$k] $kind :: $T" >>"$TLOG"
  echo "[$FAMILY reload#$k] $kind"
  [ "$k" -lt "$N" ] && { xdotool key --clearmodifiers F5; sleep 7; }
done
import -window root "$OUT/final-$FAMILY.png" 2>/dev/null
echo "SUMMARY $FAMILY: serp=$serp wall=$wall of $N reloads"
unset DISPLAY
pkill -9 -f "user-data-dir=$PROFILE" 2>/dev/null
sleep 1
