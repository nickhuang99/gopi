#!/bin/bash
# amend2-paired-ab.sh — task 01a0a981-2858 (AMENDMENT to 01a0a4dd).
#
# Runs, on the remotebox host, the cell the 2x2 amendment asks for, PAIRED IN TIME
# against its CDP twin at a CONSTANT datacenter IPv4 egress:
#
#   cell B1 = headed Chrome, NO --remote-debugging-port, actuated by XTEST only
#   cell A1 = headed Chrome + debug port, actuated by CDP Runtime.evaluate
#   both    : --host-resolver-rules pins the SAME google IPv4 literal
#
# Plus a `navigator.webdriver` proof that does NOT use CDP (the proof instrument
# must not be the variable under test): a local file:// page paints the value
# into the rendered pixels, read back by screenshot.
#
# Inherited hard mechanics (all measured by the parent task; reused verbatim so
# these cells stay comparable with the parent's):
#   * ONE Chrome at a time on this 957MB / 1 vCPU host (a second concurrent
#     Chrome caused a global OOM that killed production and the unit) ->
#     production google-cdp.service is PAUSED and a trap restores it on EVERY
#     exit path (including error exits).
#   * Own display + own user-data-dir; production owns :99 + :9222/9232.
#   * No WM on this host: windowraise + windowfocus (XSetInputFocus).
#     `windowactivate` needs _NET_ACTIVE_WINDOW and fails without a WM.
#   * pkill lives in THIS FILE, never inline over ssh (an inline pattern
#     self-matches the ssh command line and kills the session).
#   * xdotool presence is VERIFIED, not assumed (the parent's briefing claimed
#     it was absent; it is installed).
#   * Tool presence, egress IP and challenge markers are measured at run time
#     and printed; nothing is trusted from a briefing.
#
# Usage: bash amend2-paired-ab.sh [OUTDIR] [ROUNDS]
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
OUT="${1:-"$PROBEDIR"/amend2}"
ROUNDS="${2:-2}"
DISP=":95"
CDP_PORT=9223
QUERY="golang context deadline"
mkdir -p "$OUT"
exec > >(tee -a "$OUT/run.log") 2>&1

echo "=== amend2 paired AB (X11/no-CDP vs CDP @ constant datacenter IPv4) — $(date -u +%FT%TZ) ==="

# ------------------------------------------------------------------ phase 0: preflight
echo "--- phase 0: preflight (verify, do not assume) ---"
for t in xdotool Xvfb google-chrome import curl python3; do
  printf '%s=%s\n' "$t" "$(command -v "$t" || echo MISSING)"
done
free -m | sed -n 2p
V4_IP="$(getent ahostsv4 www.google.com | awk 'NR==1{print $1}')"
echo "measured_family_ipv4=$V4_IP"
MAP="MAP www.google.com $V4_IP,MAP google.com $V4_IP"
echo "egress_identity=$(curl -4 -s -m 8 https://ipinfo.io/json | tr -d '\n' | cut -c1-160)"
echo "cdp_ports_before=[$(ss -ltn 2>/dev/null | grep -E '922[0-9]' | tr '\n' ' ')]"

# The navigator.webdriver probe page. Rendered pixels are the evidence, so the
# value is painted large; read back with a screenshot (NOT via CDP).
cat >"$OUT/wd.html" <<'HTML'
<html><head><title>webdriver-probe</title></head>
<body style="background:#fff;font:28px monospace">
<div>navigator.webdriver = <b id="w" style="font-size:64px">?</b></div>
<div>navigator.userAgent = <span id="u"></span></div>
<script>
  document.getElementById("w").textContent = String(navigator.webdriver);
  document.getElementById("u").textContent = navigator.userAgent;
</script>
</body></html>
HTML

restore() {
  echo "=== RESTORE (trap): stopping arm, restarting production — $(date -u +%FT%TZ) ==="
  pkill -9 -f "user-data-dir=$OUT/" 2>/dev/null
  pkill -9 -f "Xvfb $DISP " 2>/dev/null
  sleep 2
  rm -f ""$X11SOCKDIR"/X${DISP#:}"
  systemctl start google-cdp.service
  sleep 8
  echo "restore_service=$(systemctl is-active google-cdp.service)"
  echo "restore_ports=[$(ss -ltn 2>/dev/null | grep -E '922[0-9]|923[0-9]' | tr '\n' ' ')]"
  echo "restore_health=$(curl -s -m 10 http://127.0.0.1:9232/health)"
}
trap restore EXIT

echo "--- pause production (one-Chrome-at-a-time protocol) ---"
systemctl stop google-cdp.service
sleep 3
pkill -9 -f "user-data-dir="$PROBEDIR"/chromeprofile" 2>/dev/null
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

start_chrome() { # $1=profile  $2=port ("" = NO CDP)  $3=url
  local PROFILE="$1" PORT="$2" URL="$3" extra=""
  pkill -9 -f "user-data-dir=$PROFILE" 2>/dev/null
  pkill -9 -f "Xvfb $DISP " 2>/dev/null
  sleep 2
  rm -rf "$PROFILE"; mkdir -p "$PROFILE"
  setsid nohup Xvfb "$DISP" -screen 0 1280x1024x24 >"$OUT/xvfb.log" 2>&1 &
  # a bare sleep is not a wait: wait for the socket AND for X to answer
  local i=0
  while [ "$i" -lt 20 ]; do
    [ -S ""$X11SOCKDIR"/X${DISP#:}" ] && DISPLAY=$DISP xdotool getdisplaygeometry >/dev/null 2>&1 && break
    i=$((i+1)); sleep 1
  done
  echo "xvfb_ready_after=${i}s"
  [ -n "$PORT" ] && extra="--remote-debugging-port=$PORT"
  DISPLAY=$DISP setsid nohup google-chrome --no-sandbox --disable-dev-shm-usage \
    $extra \
    --disable-blink-features=AutomationControlled \
    --exclude-switches=enable-automation \
    --no-first-run --no-default-browser-check --disable-default-apps \
    --disable-quic --host-resolver-rules="$MAP" \
    --user-data-dir="$PROFILE" --window-position=0,0 --window-size=1280,1000 \
    "$URL" >"$OUT/chrome.log" 2>&1 &
  i=0
  while [ "$i" -lt 25 ]; do
    sleep 1
    [ -n "$(DISPLAY=$DISP xdotool search --name 'Google\|webdriver' 2>/dev/null)" ] && break
    i=$((i+1))
  done
  echo "chrome_window_ready_after=${i}s"
}

record() { printf '%s\t%s\t%s\t%s\t%s\n' "$(date -u +%FT%TZ)" "$1" "$2" "$3" "$4" >>"$OUT/result.tsv"; echo "[$1 $2] $3 :: $4"; }

# ------------------------------------------------------- phase 1: webdriver proof
# Same browser build, same flags, ONLY the debug port differs — so the value
# difference (if any) is attributable to the CDP surface, and the proof itself
# never opens CDP (it is a rendered page read by screenshot).
wd_probe() { # $1=label  $2=port ("" = none)
  local LABEL="$1" PORT="$2"
  start_chrome "$OUT/p-wd-$LABEL" "$PORT" "file://$OUT/wd.html"
  export DISPLAY="$DISP"
  local WID; WID=$(main_wid)
  [ -n "$WID" ] && { xdotool windowraise "$WID"; xdotool windowfocus "$WID"; }
  sleep 3
  import -window root "$OUT/wd-$LABEL.png" 2>/dev/null
  echo "[wd-$LABEL] title=[$(xdotool getwindowname "$(main_wid)" 2>/dev/null)] png=$OUT/wd-$LABEL.png"
  unset DISPLAY
  pkill -9 -f "user-data-dir=$OUT/p-wd-$LABEL" 2>/dev/null
  sleep 3
}
wd_probe noCDP ""
wd_probe CDP "$CDP_PORT"

# ------------------------------------------------------------ phase 2: the A/B
run_x11() { # $1=round   cell B1: XTEST only, NO debug port
  local R="$1" PROFILE="$OUT/p-x11-$R" TLOG="$OUT/titles-x11-r$R.log"
  start_chrome "$PROFILE" "" "https://www.google.com/"
  export DISPLAY="$DISP"
  local WID; WID=$(main_wid)
  [ -n "$WID" ] && { xdotool windowraise "$WID"; xdotool windowfocus "$WID"; }
  : >"$TLOG"
  poll() { local i=0 w t; while [ "$i" -lt "$2" ]; do
      for w in $(xdotool search --name "." 2>/dev/null); do
        t=$(xdotool getwindowname "$w" 2>/dev/null)
        case "$t" in ""|"Chromium clipboard"|"google-chrome") ;; *) echo "[x11-r$R $1 t+${i}s] $t" >>"$TLOG" ;; esac
      done; i=$((i+1)); sleep 1; done; }
  poll landing 12
  import -window root "$OUT/x11-r$R-01-landing.png" 2>/dev/null
  xdotool key --clearmodifiers ctrl+l; sleep 1
  xdotool type --clearmodifiers --delay 40 "$QUERY"; sleep 1
  import -window root "$OUT/x11-r$R-02-typed.png" 2>/dev/null
  xdotool key --clearmodifiers Return
  poll post-enter 16
  import -window root "$OUT/x11-r$R-03-verdict.png" 2>/dev/null
  local T; T=$(xdotool getwindowname "$(main_wid)" 2>/dev/null)
  unset DISPLAY
  case "$T" in
    *"$QUERY"*"Google Search"*|*"Google Search"*"$QUERY"*) record "B1-X11-r$R" "X11/no-CDP" "SERP-title" "$T" ;;
    *"google.com/search"*) record "B1-X11-r$R" "X11/no-CDP" "WALL-title-is-url(soft-enablejs-look)" "$T" ;;
    *"sorry"*)            record "B1-X11-r$R" "X11/no-CDP" "WALL-sorry-title" "$T" ;;
    *)                    record "B1-X11-r$R" "X11/no-CDP" "OTHER" "$T" ;;
  esac
  pkill -9 -f "user-data-dir=$PROFILE" 2>/dev/null
  sleep 3
}

run_cdp() { # $1=round   cell A1: CDP Runtime.evaluate
  local R="$1" PROFILE="$OUT/p-cdp-$R"
  start_chrome "$PROFILE" "$CDP_PORT" "https://www.google.com/"
  local i=0
  until curl -s -m 3 "http://127.0.0.1:$CDP_PORT/json/version" >/dev/null 2>&1; do
    i=$((i+1)); [ "$i" -ge 15 ] && break; sleep 2
  done
  echo "cdp_ready_after=${i} polls"
  python3 "$PROBEDIR"/arm-a-cdp-probe.py --cdp-port "$CDP_PORT" --label "A1-CDP-r$R" \
    --query "$QUERY" >"$OUT/cdp-r$R.json" 2>&1
  local S H T
  S=$(python3 -c "import json;d=json.load(open('$OUT/cdp-r$R.json'));print(d.get('sorry',0))" 2>/dev/null)
  H=$(python3 -c "import json;d=json.load(open('$OUT/cdp-r$R.json'));print(len(d.get('h3_titles') or []))" 2>/dev/null)
  T=$(python3 -c "import json;d=json.load(open('$OUT/cdp-r$R.json'));print((d.get('title') or '')[:120])" 2>/dev/null)
  DISPLAY=$DISP import -window root "$OUT/cdp-r$R-verdict.png" 2>/dev/null
  if [ "${H:-0}" -gt 0 ]; then record "A1-CDP-r$R" "CDP" "SERP-h3=$H" "$T"
  elif [ "${S:-0}" = "1" ]; then record "A1-CDP-r$R" "CDP" "WALL-sorry(hard)" "$T"
  else record "A1-CDP-r$R" "CDP" "OTHER-sorry=$S h3=$H" "$T"; fi
  pkill -9 -f "user-data-dir=$PROFILE" 2>/dev/null
  sleep 3
}

: >"$OUT/result.tsv"
for R in $(seq 1 "$ROUNDS"); do
  if [ $((R % 2)) -eq 1 ]; then
    echo "--- round $R: X11(B1) then CDP(A1) ---"; run_x11 "$R"; run_cdp "$R"
  else
    echo "--- round $R: CDP(A1) then X11(B1) ---"; run_cdp "$R"; run_x11 "$R"
  fi
done

echo "=== RESULT TABLE (tsv: utc cell actuation signal detail) ==="
cat "$OUT/result.tsv"
echo "=== raw curl wall-type markers at the same constant IP ==="
curl -4 -s -m 20 -o "$OUT/curl-v4-pinned.html" -w "curl_v4 http=%{http_code} remote_ip=%{remote_ip} bytes=%{size_download}\n" \
  "https://www.google.com/search?q=${QUERY// /+}&hl=en&gl=us"
printf 'markers: sorry=%s enablejs=%s h3=%s title=%s\n' \
  "$(grep -c 'sorry/index' "$OUT/curl-v4-pinned.html")" \
  "$(grep -c 'enablejs' "$OUT/curl-v4-pinned.html")" \
  "$(grep -oc '<h3' "$OUT/curl-v4-pinned.html" | head -1)" \
  "$(grep -o '<title>[^<]*' "$OUT/curl-v4-pinned.html" | head -1)"
echo "=== ARMS DONE ==="
ls -la "$OUT"/*.png 2>/dev/null | tail -12
