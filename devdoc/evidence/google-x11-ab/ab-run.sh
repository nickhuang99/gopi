#!/bin/bash
# ab-run.sh — sequential headed-Chrome X11-actuation A/B on the remotebox host.
#
# WHY PRODUCTION IS PAUSED HERE (deliberate, restored by trap):
#   measured this task — the host has 957MB RAM / 1 vCPU. A SECOND Chrome while
#   the production one runs triggers a GLOBAL OOM: dmesg 2026-09-16 11:30:56
#   shows `Out of memory: Killed process 1340128 (chrome) task_memcg=
#   /system.slice/google-cdp.service`, i.e. the experiment killed production.
#   Racing production is therefore LESS safe than pausing it: one Chrome at a
#   time. The trap ALWAYS restarts google-cdp.service, even on interrupt.
#
# Each arm: own Xvfb display + own user-data-dir, NO --remote-debugging-port
# (CDP is forbidden for the X11 arm — the production gateway owns the only ws
# connection per target), egress family pinned by --host-resolver-rules so the
# IP stays constant while only the actuation method differs.
# All input is XTEST via xdotool; there is no WM on this host, so windowraise +
# windowfocus (XSetInputFocus) replaces windowactivate/_NET_ACTIVE_WINDOW.
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
OUT="${OUT:-"$PROBEDIR"/repro-arm-x11}"
QUERY="${QUERY:-golang context deadline}"
mkdir -p "$OUT"

cleanup_arm() {
  pkill -9 -f "user-data-dir=$OUT" 2>/dev/null
  pkill -9 -f "Xvfb :97 " 2>/dev/null
  sleep 2
  rm -f "$X11SOCKDIR"/X97
}

restore() {
  echo "=== RESTORE: stopping arm, restarting production ==="
  cleanup_arm
  systemctl start google-cdp.service
  echo "restore_rc=$?"
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
free -m | head -2

run_arm() { # $1 LABEL  $2 FAMILY(IPV6|IPV4)
  # NB: assignments are split — `local a=$1 b=$a` expands every word BEFORE
  # any assignment, so under `set -u` the b side reads an unset variable.
  local LABEL="$1"
  local FAMILY="$2"
  local DISP=":97"
  local PROFILE="$OUT/profile-$FAMILY"
  local MAP TLOG
  if [ "$FAMILY" = "IPV4" ]; then
    MAP="MAP www.google.com 142.251.156.119,MAP google.com 142.251.156.119"
  else
    MAP="MAP www.google.com [2001:4860:482a:7700::],MAP google.com [2001:4860:482a:7700::]"
  fi
  cleanup_arm; rm -rf "$PROFILE"; mkdir -p "$PROFILE"
  setsid nohup Xvfb "$DISP" -screen 0 1280x1024x24 >"$OUT/xvfb-$LABEL.log" 2>&1 &
  sleep 2
  export DISPLAY="$DISP"
  setsid nohup google-chrome --no-sandbox --disable-dev-shm-usage \
    --disable-blink-features=AutomationControlled \
    --exclude-switches=enable-automation \
    --no-first-run --no-default-browser-check --disable-default-apps \
    --disable-quic --host-resolver-rules="$MAP" \
    --user-data-dir="$PROFILE" --window-position=0,0 --window-size=1280,1000 \
    "https://www.google.com/" >"$OUT/chrome-$LABEL.log" 2>&1 &

  TLOG="$OUT/titles-$LABEL.log"; : >"$TLOG"
  main_wid() { local w t; for w in $(xdotool search --name "." 2>/dev/null); do
      t=$(xdotool getwindowname "$w" 2>/dev/null)
      case "$t" in ""|"Chromium clipboard"|"google-chrome") ;; *) echo "$w"; return;; esac
    done; }
  poll() { local i=0 w t; while [ "$i" -lt "$2" ]; do
      for w in $(xdotool search --name "." 2>/dev/null); do
        t=$(xdotool getwindowname "$w" 2>/dev/null)
        case "$t" in ""|"Chromium clipboard"|"google-chrome") ;; *)
          echo "[$LABEL $1 t+${i}s wid=$w] $t" >>"$TLOG";; esac
      done; i=$((i+1)); sleep 1; done; }

  # wait for a loaded window (title past the "Untitled" phase)
  local i=0 WID=""
  while [ "$i" -lt 40 ]; do
    sleep 1; WID=$(main_wid); [ -n "$WID" ] && break; i=$((i+1))
  done
  xdotool windowraise "$WID"; xdotool windowfocus "$WID"
  echo "[$LABEL] family=$FAMILY display=$DISP wid=$WID title=[$(xdotool getwindowname "$WID")]"
  poll "landing" 3
  import -window root "$OUT/$LABEL-01-landing.png" 2>/dev/null

  # ACTUATION: ctrl+l (omnibox) -> type -> Return; pure XTEST, zero CDP.
  xdotool key --clearmodifiers ctrl+l; sleep 1
  xdotool type --clearmodifiers --delay 40 "$QUERY"; sleep 1
  import -window root "$OUT/$LABEL-02-typed.png" 2>/dev/null
  xdotool key --clearmodifiers Return
  poll "post-enter" 14
  import -window root "$OUT/$LABEL-03-verdict.png" 2>/dev/null
  xdotool key --clearmodifiers F5
  poll "post-f5" 8
  import -window root "$OUT/$LABEL-04-after-f5.png" 2>/dev/null
  echo "[$LABEL] FINAL title = $(xdotool getwindowname "$(main_wid)" 2>/dev/null)"
  echo "[$LABEL] titles:"; cat "$TLOG"
  unset DISPLAY
}

run_arm ARMB-IPV6 IPV6
run_arm ARMC-IPV4 IPV4

echo "=== ARMS DONE ==="
ls -la "$OUT"/*.png
