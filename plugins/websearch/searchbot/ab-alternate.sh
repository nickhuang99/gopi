#!/bin/bash
# ab-alternate.sh — PAIRED repetition of the two IPv4 cells, adjacent in time.
#
# THE QUESTION: abv3 r1 gave X11@IPv4 a full SERP, r2 gave it the /sorry wall,
# while CDP@IPv4 was walled twice. A single success cannot separate
#   H1 "the actuation method (X11 vs CDP) defeats the block"
# from
#   H2 "the block is time/rate-varying and one run happened to land in a gap".
# The discriminating instrument is a PAIRED alternation at constant IP: run the
# two cells back to back, repeat, and alternate the order each round so an order
# effect cannot masquerade as an actuation effect.
#
# Cells: X11  = headed Chrome, no --remote-debugging-port, actuated by XTEST only
#        CDP  = headed Chrome + debug port, actuated by Runtime.evaluate
# Both: IPv4 egress via --host-resolver-rules (142.251.156.119), own fresh
# profile, own display. Production paused for the whole run (957MB host — one
# Chrome at a time), restored by trap on every exit path.
#
# Usage: bash ab-alternate.sh [ROUNDS]      default 3
set -u
ROUNDS="${1:-3}"
OUT="/tmp/abalt"
QUERY="golang context deadline"
V4_IP="142.251.156.119"
MAP="MAP www.google.com $V4_IP,MAP google.com $V4_IP"
mkdir -p "$OUT"
: >"$OUT/result.tsv"

restore() {
  echo "=== RESTORE (trap) ==="
  pkill -9 -f "user-data-dir=$OUT/" 2>/dev/null
  pkill -9 -f "Xvfb :9" 2>/dev/null
  sleep 2
  rm -f /tmp/.X11-unix/X93 /tmp/.X11-unix/X94
  systemctl start google-cdp.service
  sleep 8
  echo "restore_service=$(systemctl is-active google-cdp.service)"
  echo "restore_ports=$(ss -ltn | grep -cE ':9222|:9232')"
  echo "restore_health=$(curl -s -m 10 http://127.0.0.1:9232/health)"
}
trap restore EXIT

echo "=== PAUSE PRODUCTION ==="
systemctl stop google-cdp.service
sleep 3
pkill -9 -f "user-data-dir=/tmp/chromeprofile" 2>/dev/null
pkill -9 -f "Xvfb :99 " 2>/dev/null
sleep 2
echo "prod_active=$(systemctl is-active google-cdp.service)"

main_wid() {
  local w t
  for w in $(xdotool search --name "." 2>/dev/null); do
    t=$(xdotool getwindowname "$w" 2>/dev/null)
    case "$t" in ""|"Chromium clipboard"|"google-chrome") ;; *) echo "$w"; return ;; esac
  done
}

start_chrome() { # $1 DISP  $2 PORT("" = none)  $3 PROFILE
  pkill -9 -f "user-data-dir=$3" 2>/dev/null
  pkill -9 -f "Xvfb $1 " 2>/dev/null
  sleep 2
  rm -rf "$3"; mkdir -p "$3"
  setsid nohup Xvfb "$1" -screen 0 1280x1024x24 >"$OUT/xvfb.log" 2>&1 &
  sleep 2
  local extra=""
  [ -n "$2" ] && extra="--remote-debugging-port=$2"
  DISPLAY="$1" setsid nohup google-chrome --no-sandbox --disable-dev-shm-usage \
    $extra \
    --disable-blink-features=AutomationControlled \
    --exclude-switches=enable-automation \
    --no-first-run --no-default-browser-check --disable-default-apps \
    --disable-quic --host-resolver-rules="$MAP" \
    --user-data-dir="$3" --window-position=0,0 --window-size=1280,1000 \
    "https://www.google.com/" >"$OUT/chrome.log" 2>&1 &
  local i=0
  while [ "$i" -lt 18 ]; do
    sleep 1
    [ -n "$(DISPLAY=$1 xdotool search --name 'Google' 2>/dev/null)" ] && break
    i=$((i+1))
  done
}

record() { # $1 cell  $2 round  $3 signal  $4 detail
  printf '%s\t%s\t%s\t%s\t%s\n' "$(date -u +%H:%M:%S)" "$2" "$1" "$3" "$4" >>"$OUT/result.tsv"
  echo "[$2 $1] $3 :: $4"
}

run_x11() { # $1 round
  local R="$1" DISP=":93" PROFILE="$OUT/p-x11-$R"
  start_chrome "$DISP" "" "$PROFILE"
  export DISPLAY="$DISP"
  local WID; WID=$(main_wid)
  [ -n "$WID" ] && { xdotool windowraise "$WID"; xdotool windowfocus "$WID"; }
  xdotool key --clearmodifiers ctrl+l; sleep 1
  xdotool type --clearmodifiers --delay 40 "$QUERY"; sleep 1
  xdotool key --clearmodifiers Return
  sleep 14
  local T; T=$(xdotool getwindowname "$(main_wid)" 2>/dev/null)
  import -window root "$OUT/x11-r$R.png" 2>/dev/null
  unset DISPLAY
  case "$T" in
    *"Google Search"*"$QUERY"*) record X11 "$R" "SERP-title" "$T" ;;
    *"$QUERY"*"Google Search"*) record X11 "$R" "SERP-title" "$T" ;;
    *) record X11 "$R" "WALL-title-is-url" "$T" ;;
  esac
  pkill -9 -f "user-data-dir=$PROFILE" 2>/dev/null
  sleep 2
}

run_cdp() { # $1 round
  local R="$1" DISP=":94" PORT=9233 PROFILE="$OUT/p-cdp-$R"
  start_chrome "$DISP" "$PORT" "$PROFILE"
  local i=0
  until curl -s -m 3 "http://127.0.0.1:$PORT/json/version" >/dev/null 2>&1; do
    i=$((i+1)); [ "$i" -ge 15 ] && break; sleep 2
  done
  python3 /tmp/arm-a-cdp-probe.py --cdp-port $PORT --label "CDP-r$R" \
    --query "$QUERY" >"$OUT/cdp-r$R.json" 2>&1
  local S H T
  S=$(python3 -c "import json;d=json.load(open('$OUT/cdp-r$R.json'));print(d.get('sorry',0))" 2>/dev/null)
  H=$(python3 -c "import json;d=json.load(open('$OUT/cdp-r$R.json'));print(len(d.get('h3_titles') or []))" 2>/dev/null)
  T=$(python3 -c "import json;d=json.load(open('$OUT/cdp-r$R.json'));print(d.get('title',''))" 2>/dev/null)
  DISPLAY=$DISP xdotool windowraise "$(DISPLAY=$DISP main_wid)" 2>/dev/null
  DISPLAY=$DISP import -window root "$OUT/cdp-r$R.png" 2>/dev/null
  if [ "${H:-0}" -gt 0 ]; then record CDP "$R" "SERP-h3=$H" "$T"
  elif [ "${S:-0}" = "1" ]; then record CDP "$R" "WALL-sorry" "$T"
  else record CDP "$R" "OTHER-sorry=$S h3=$H" "$T"; fi
  pkill -9 -f "user-data-dir=$PROFILE" 2>/dev/null
  sleep 2
}

for R in $(seq 1 "$ROUNDS"); do
  if [ $((R % 2)) -eq 1 ]; then
    echo "--- round $R: X11 then CDP ---"; run_x11 "$R"; run_cdp "$R"
  else
    echo "--- round $R: CDP then X11 ---"; run_cdp "$R"; run_x11 "$R"
  fi
done

echo "=== RESULT TABLE (tsv: time round cell signal detail) ==="
cat "$OUT/result.tsv"
echo "=== ARMS DONE ==="
