#!/bin/bash
# ab-run-v3.sh — sequential, OOM-safe headed-X11 A/B on search-bot.
#
# Protocol constraints (measured, not assumed):
#  * search-bot: 957MB RAM / 1 vCPU. A SECOND concurrent Chrome caused a GLOBAL
#    OOM that killed the production Chrome (dmesg 2026-09-16 11:30:05Z,
#    task_memcg=session scope, oom_score_adj=300). ONE Chrome at a time is the
#    only safe protocol, so production is paused for the run and restored once
#    by a trap (which also covers error exits — no production path left broken).
#  * The systemd unit runs the watchdog; stopping the unit stops the watchdog,
#    so it cannot pkill the arm's Chrome mid-run.
#  * Production owns :99 / profile "$PROBEDIR"/chromeprofile / CDP :9222. Each arm uses
#    :97 + its own fresh profile + NO --remote-debugging-port (X11-ONLY, no CDP:
#    the production gateway holds the single ws connection per target anyway).
#  * Egress family is pinned with --host-resolver-rules so the IP is CONSTANT
#    and only the actuation method differs from the CDP Arm A.
#  * All input is XTEST via xdotool. No WM on this host -> windowraise +
#    windowfocus (XSetInputFocus); windowactivate needs _NET_ACTIVE_WINDOW.
#  * Window TITLE is the machine-readable per-arm signal: a SERP title carries
#    the query, a wall's title is the bare URL.
#
# Usage: bash ab-run-v3.sh [TAG]        (defaults TAG=r2; runs IPV6 then IPV4)
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
TAG="${1:-r2}"
DISP=":97"
OUT=""$PROBEDIR"/abv3"
QUERY="${QUERY:-golang context deadline}"
V4_IP="142.251.156.119"
V6_IP="[2001:4860:482a:7700::]"
mkdir -p "$OUT"

restore() {
  echo "=== RESTORE (trap): stopping arms, restarting production ==="
  pkill -9 -f "user-data-dir=$OUT/profile-" 2>/dev/null
  pkill -9 -f "Xvfb $DISP " 2>/dev/null
  sleep 2
  rm -f "$X11SOCKDIR"/X97
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
free -m | sed -n 2p

main_wid() {
  local w t
  for w in $(xdotool search --name "." 2>/dev/null); do
    t=$(xdotool getwindowname "$w" 2>/dev/null)
    case "$t" in ""|"Chromium clipboard"|"google-chrome") ;; *) echo "$w"; return ;; esac
  done
}

run_arm() { # $1 FAMILY [IPV6|IPV4]
  local FAMILY="$1"
  local PROFILE="$OUT/profile-$TAG-$FAMILY"
  local TLOG="$OUT/titles-$TAG-$FAMILY.log"
  local MAP
  if [ "$FAMILY" = "IPV4" ]; then
    MAP="MAP www.google.com $V4_IP,MAP google.com $V4_IP"
  else
    MAP="MAP www.google.com $V6_IP,MAP google.com $V6_IP"
  fi

  echo "=== EGRESS MEASUREMENT ($TAG $FAMILY) ==="
  curl -4 -s -o "$OUT/curl4-$TAG-$FAMILY.html" -w "curl_v4 http=%{http_code} remote_ip=%{remote_ip} bytes=%{size_download}\n" \
    "https://www.google.com/search?q=${QUERY// /+}"
  curl -6 -s -o "$OUT/curl6-$TAG-$FAMILY.html" -w "curl_v6 http=%{http_code} remote_ip=%{remote_ip} bytes=%{size_download}\n" \
    "https://www.google.com/search?q=${QUERY// /+}"

  pkill -9 -f "user-data-dir=$PROFILE" 2>/dev/null
  pkill -9 -f "Xvfb $DISP " 2>/dev/null
  sleep 2
  rm -rf "$PROFILE"; mkdir -p "$PROFILE"
  setsid nohup Xvfb "$DISP" -screen 0 1280x1024x24 >"$OUT/xvfb-$TAG-$FAMILY.log" 2>&1 &
  sleep 2
  export DISPLAY="$DISP"
  setsid nohup google-chrome --no-sandbox --disable-dev-shm-usage \
    --disable-blink-features=AutomationControlled \
    --exclude-switches=enable-automation \
    --no-first-run --no-default-browser-check --disable-default-apps \
    --disable-quic --host-resolver-rules="$MAP" \
    --user-data-dir="$PROFILE" --window-position=0,0 --window-size=1280,1000 \
    "https://www.google.com/" >"$OUT/chrome-$TAG-$FAMILY.log" 2>&1 &

  : >"$TLOG"
  poll() { # $1 phase  $2 seconds
    local i=0 w t
    while [ "$i" -lt "$2" ]; do
      for w in $(xdotool search --name "." 2>/dev/null); do
        t=$(xdotool getwindowname "$w" 2>/dev/null)
        case "$t" in ""|"Chromium clipboard"|"google-chrome") ;; *)
          echo "[$TAG-$FAMILY $1 t+${i}s wid=$w] $t" >>"$TLOG" ;; esac
      done
      i=$((i+1)); sleep 1
    done
  }

  poll "landing" 14
  local WID; WID=$(main_wid)
  echo "[$TAG-$FAMILY] wid=$WID landing_title=[$(xdotool getwindowname "$WID" 2>/dev/null)]"
  [ -n "$WID" ] && { xdotool windowraise "$WID"; xdotool windowfocus "$WID"; }
  import -window root "$OUT/$TAG-$FAMILY-01-landing.png" 2>/dev/null

  # ACTUATION — pure XTEST, zero CDP:
  xdotool key --clearmodifiers ctrl+l; sleep 1
  xdotool type --clearmodifiers --delay 40 "$QUERY"; sleep 1
  import -window root "$OUT/$TAG-$FAMILY-02-typed.png" 2>/dev/null
  xdotool key --clearmodifiers Return
  poll "post-enter" 15
  import -window root "$OUT/$TAG-$FAMILY-03-verdict.png" 2>/dev/null
  xdotool key --clearmodifiers F5
  poll "post-f5" 10
  import -window root "$OUT/$TAG-$FAMILY-04-after-f5.png" 2>/dev/null

  echo "[$TAG-$FAMILY] FINAL title = $(xdotool getwindowname "$(main_wid)" 2>/dev/null)"
  echo "[$TAG-$FAMILY] distinct titles:"
  grep -oP '^\[[^]]+\]\s\K.*' "$TLOG" | sed 's/ - Google Chrome$//' | sort -u
  # the SAME url curl-side, so the record carries the static-HTML control
  unset DISPLAY
  pkill -9 -f "user-data-dir=$PROFILE" 2>/dev/null
  sleep 2
}

# ordered: IPv6 first (the family the production path uses), then IPv4
run_arm IPV6
run_arm IPV4
echo "=== ARMS DONE ==="
ls -la "$OUT"/*.png 2>/dev/null | tail -10
echo "=== saved-HTML challenge markers ==="
for f in "$OUT"/curl*.html; do
  printf '%s: sorry=%s enablejs=%s results_h3=%s\n' "$(basename "$f")" \
    "$(grep -c 'sorry/index' "$f" 2>/dev/null)" \
    "$(grep -c 'enablejs' "$f" 2>/dev/null)" \
    "$(grep -oc '<h3' "$f" 2>/dev/null)"
done
