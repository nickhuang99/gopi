#!/bin/bash
# amend-arm-b1.sh — task 01a0a981-2858 (amendment to 01a0a4dd).
#
# Measures, on the remotebox host, first-hand:
#   (1) the WEAK curl header-suite data point, with raw byte counts + challenge
#       markers, both egress families;
#   (2) ARM B1 — the critical 2x2 cell: X11/XTEST-only actuation (NO CDP at all)
#       at a DATACENTER IPv4 egress.
#
# Inherited hard mechanics (all measured by the parent, reused verbatim so this
# cell is comparable with the parent's cells):
#   * ONE Chrome at a time on this 957MB / 1 vCPU host (a second concurrent
#     Chrome caused a global OOM that killed production) -> production is
#     PAUSED and a trap restores it on EVERY exit path.
#   * Own display (:95) + own user-data-dir; production owns :99 + :9222/9232.
#   * NO --remote-debugging-port for this arm (CDP is the variable under test).
#   * No WM on this host: windowraise + windowfocus (XSetInputFocus), never
#     windowactivate (needs _NET_ACTIVE_WINDOW, absent without a WM).
#   * xdotool presence is VERIFIED here, not assumed (the parent's briefing
#     said it was absent; it is installed).
#   * pkill lives in THIS FILE, never inline over ssh (inline self-matches the
#     ssh command line and kills the session).
#   * Chrome caps --host-resolver-rules display for testing purposes; the
#     "unsupported command-line flag" infobar is a browser-UI artifact rendered
#     after the response. It is recorded in every screenshot, see the devdoc.
#
# Usage: bash amend-arm-b1.sh [OUTDIR]
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
OUT="${1:-"$PROBEDIR"/amend-ab1}"
DISP=":95"
PORT_NEIGHBOURS="922[0-9]"
PROFILE="$OUT/profile-b1"
QUERY="golang context deadline"
mkdir -p "$OUT"
exec > >(tee -a "$OUT/run.log") 2>&1

echo "=== ARM B1 (X11-only, no CDP, datacenter IPv4) — $(date -u +%FT%TZ) ==="

# ---------------------------------------------------------------- phase 0: preflight
echo "--- phase 0: preflight (verify, do not assume) ---"
for t in xdotool Xvfb google-chrome import curl python3; do
  printf '%s=%s\n' "$t" "$(command -v "$t" || echo MISSING)"
done
[ -x "$(command -v xdotool)" ] || { echo "FATAL xdotool missing"; exit 3; }
free -m | sed -n 2p
echo "shm_device=$(stat -c %d /dev/shm) root_device=$(stat -c %d /)"
echo "cdp_ports_before=[$(ss -ltn 2>/dev/null | grep -E "$PORT_NEIGHBOURS" | tr '\n' ' ')]"

# measured egress targets (re-measured at run time; never trusted from a briefing)
V4_IP="$(getent ahostsv4 www.google.com | awk 'NR==1{print $1}')"
V6_IP="$(getent ahostsv6 www.google.com | awk 'NR==1{print $1}')"
echo "measured_v4=$V4_IP measured_v6=$V6_IP"

# ------------------------------------------------- phase 1: the WEAK curl test
# Limits (this is why it is weak): curl varies HTTP HEADERS only. It cannot vary
# the TLS/JA3 fingerprint, cannot execute JS, and has no input dynamics.
CHROME_UA="Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36"
SEARCH_URL="https://www.google.com/search?q=golang+context+deadline&hl=en&gl=us"

markers() {  # $1 = html file, $2 = label
  local f="$1" lbl="$2"
  printf '%s: bytes=%s sorry=%s enablejs=%s h3=%s\n' "$lbl" "$(wc -c <"$f")" \
    "$(grep -c -iE 'sorry/index|Our systems have detected unusual traffic' "$f")" \
    "$(grep -c -i 'enablejs' "$f")" \
    "$(grep -o -i '<h3' "$f" | wc -l)"
}

echo "--- phase 1: WEAK curl data point (headers only) ---"
curl -s --max-time 20 -o "$OUT/curl-bare-v4.html" -w 'curl_bare_v4 http=%{http_code} remote_ip=%{remote_ip} bytes=%{size_download}\n' "$SEARCH_URL"
curl -6 -s --max-time 20 -o "$OUT/curl-bare-v6.html" -w 'curl_bare_v6 http=%{http_code} remote_ip=%{remote_ip} bytes=%{size_download}\n' "$SEARCH_URL"
curl -s --max-time 20 -A "$CHROME_UA" \
  -H 'Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8' \
  -H 'Accept-Language: en-US,en;q=0.9' -H 'Sec-Fetch-Dest: document' -H 'Sec-Fetch-Mode: navigate' \
  -H 'Sec-Fetch-Site: none' -H 'Sec-Fetch-User: ?1' -H 'Upgrade-Insecure-Requests: 1' \
  -H 'sec-ch-ua: "Chromium";v="151", "Not:A-Brand";v="24"' -H 'sec-ch-ua-mobile: ?0' \
  -H 'sec-ch-ua-platform: "Linux"' \
  -o "$OUT/curl-headers-v4.html" -w 'curl_headers_v4 http=%{http_code} remote_ip=%{remote_ip} bytes=%{size_download}\n' "$SEARCH_URL"
curl -6 -s --max-time 20 -A "$CHROME_UA" \
  -H 'Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8' \
  -H 'Accept-Language: en-US,en;q=0.9' -H 'Sec-Fetch-Dest: document' -H 'Sec-Fetch-Mode: navigate' \
  -H 'Sec-Fetch-Site: none' -H 'Sec-Fetch-User: ?1' -H 'Upgrade-Insecure-Requests: 1' \
  -H 'sec-ch-ua: "Chromium";v="151", "Not:A-Brand";v="24"' -H 'sec-ch-ua-mobile: ?0' \
  -H 'sec-ch-ua-platform: "Linux"' \
  -o "$OUT/curl-headers-v6.html" -w 'curl_headers_v6 http=%{http_code} remote_ip=%{remote_ip} bytes=%{size_download}\n' "$SEARCH_URL"
for p in bare-v4 bare-v6 headers-v4 headers-v6; do markers "$OUT/curl-$p.html" "curl_$p"; done

# ------------------------------------------------------------------ phase 2: Arm B1
restore() {
  echo "=== RESTORE (trap): stopping arm, restarting production ==="
  pkill -9 -f "user-data-dir=$PROFILE" 2>/dev/null
  pkill -9 -f "Xvfb $DISP " 2>/dev/null
  sleep 2
  rm -f ""$X11SOCKDIR"/X${DISP#:}"
  systemctl start google-cdp.service
  sleep 8
  echo "restore_service=$(systemctl is-active google-cdp.service)"
  echo "restore_health=$(curl -s -m 10 http://127.0.0.1:9232/health)"
}
trap restore EXIT

echo "--- phase 2: pause production (one Chrome at a time) ---"
systemctl stop google-cdp.service
sleep 3
pkill -9 -f "user-data-dir="$PROBEDIR"/chromeprofile" 2>/dev/null
pkill -9 -f "Xvfb :99 " 2>/dev/null
sleep 2
echo "prod_active=$(systemctl is-active google-cdp.service)"
echo "prod_chrome_left=$(pgrep -fc 'user-data-dir="$PROBEDIR"/chromeprofile' 2>/dev/null || echo 0)"

MAP="MAP www.google.com $V4_IP,MAP google.com $V4_IP"
echo "--- phase 3: ARM B1 launch (X11 only, no CDP) map=$MAP ---"
pkill -9 -f "user-data-dir=$PROFILE" 2>/dev/null
rm -rf "$PROFILE"; mkdir -p "$PROFILE"
setsid nohup Xvfb "$DISP" -screen 0 1280x1024x24 >"$OUT/xvfb.log" 2>&1 &
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
  "https://www.google.com/" >"$OUT/chrome.log" 2>&1 &
CHROME_START=$!
echo "chrome_start_pid=$CHROME_START"

# a bare sleep is not a wait: wait for the X socket AND the window
i=0; while [ ! -S ""$X11SOCKDIR"/X${DISP#:}" ] && [ "$i" -lt 20 ]; do i=$((i+1)); sleep 1; done
echo "x_socket_present=$([ -S ""$X11SOCKDIR"/X${DISP#:}" ] && echo yes || echo no)"

TLOG="$OUT/titles.log"; : >"$TLOG"
poll_titles() {
  local label="$1" n="$2" i=0 w t
  while [ "$i" -lt "$n" ]; do
    for w in $(xdotool search --name "." 2>/dev/null); do
      t=$(xdotool getwindowname "$w" 2>/dev/null)
      case "$t" in ""|"Chromium clipboard"|"google-chrome") ;; *) echo "[$label t+${i}s wid=$w] $t" >>"$TLOG" ;; esac
    done
    i=$((i+1)); sleep 1
  done
}
main_wid() {
  local w t
  for w in $(xdotool search --name "." 2>/dev/null); do
    t=$(xdotool getwindowname "$w" 2>/dev/null)
    case "$t" in ""|"Chromium clipboard"|"google-chrome") ;; *) echo "$w"; return ;; esac
  done
}

poll_titles landing 12
WID=$(main_wid)
echo "loaded_wid=$WID title=[$(xdotool getwindowname "$WID" 2>/dev/null)]"
[ -n "$WID" ] && { xdotool windowraise "$WID"; xdotool windowfocus "$WID"; }
import -window root "$OUT/B1-IPV4-01-landing.png" 2>/dev/null

# --- no-CDP assertions (structural proof this arm carries no CDP artifact) ---
CPID=$(pgrep -f "user-data-dir=$PROFILE" | head -1)
echo "chrome_pid=$CPID"
echo "cmdline_has_remote_debugging_port=$(tr '\0' ' ' <"/proc/$CPID/cmdline" 2>/dev/null | grep -c 'remote-debugging-port')"
echo "our_listening_cdp_ports=[$(ss -ltnp 2>/dev/null | grep -E "$PORT_NEIGHBOURS" | grep -c "pid=$CPID,")]"
echo "cdp_ports_all=[$(ss -ltn 2>/dev/null | grep -E "$PORT_NEIGHBOURS" | tr '\n' ' ')]"

# --- actuation: XTEST only ---
xdotool key --clearmodifiers ctrl+l; sleep 1
xdotool type --clearmodifiers --delay 40 "$QUERY"; sleep 1
import -window root "$OUT/B1-IPV4-02-typed.png" 2>/dev/null
xdotool key --clearmodifiers Return
poll_titles post-enter 18
import -window root "$OUT/B1-IPV4-03-verdict.png" 2>/dev/null
xdotool key --clearmodifiers F5
poll_titles post-f5 10
import -window root "$OUT/B1-IPV4-04-after-f5.png" 2>/dev/null

echo "=== B1 title log ==="
cat "$TLOG"
echo "=== B1 artifacts ==="
ls -la "$OUT" | grep -E 'B1-IPV4|curl|titles'
echo "=== ARM B1 DONE ==="
