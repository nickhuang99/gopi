#!/bin/bash
# rpi_probe_wg3up.sh — residential-egress arm, run ON the RPi5 with wg3 already up.
#
# Previous failure mode (fixed here): chromium died with
# "ui/gfx/x/connection.cc: X connection error received" because Xvfb was not yet
# accepting connections when chromium started — a bare `sleep 2` is not a wait.
# This version WAITS for the X socket ("$X11SOCKDIR"/X99) and for
# /json/version before touching anything else.
#
# No ImageMagick on this host (`import: command not found`), so the pixel
# artifact is taken through CDP Page.captureScreenshot — the arm's actuation is
# CDP anyway, so this adds no new mechanism.
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
OUT="$PROBEDIR"/rpiwg3
mkdir -p "$OUT"

echo "=== DNS via the tunnel ==="
sudo -n resolvectl flush-caches 2>/dev/null
sudo -n resolvectl domain wg3 '~.' 2>/dev/null
sudo -n resolvectl domain eth0 '' 2>/dev/null
sudo -n resolvectl default-route wg3 true 2>/dev/null
sudo -n resolvectl dns wg3 8.8.4.4 8.8.8.8 2>/dev/null
sudo -n resolvectl default-route eth0 false 2>/dev/null
sleep 2

echo "=== exit identity (the variable under test) ==="
echo "exit_v4=$(curl -4 -s --max-time 15 https://ifconfig.me 2>&1 | head -c 40)"
curl -4 -s -o "$OUT/curl-serp.html" -w "curl_google http=%{http_code} remote_ip=%{remote_ip} bytes=%{size_download}\n" \
  "https://www.google.com/search?q=golang+context+deadline"
printf 'curl markers: sorry=%s enablejs=%s h3=%s\n' \
  "$(grep -c 'sorry/index' "$OUT/curl-serp.html" 2>/dev/null)" \
  "$(grep -c 'enablejs' "$OUT/curl-serp.html" 2>/dev/null)" \
  "$(grep -oc '<h3' "$OUT/curl-serp.html" 2>/dev/null)"

echo "=== bring up Xvfb (WAIT for the socket, not a sleep) ==="
pkill -9 -f "Xvfb :99" 2>/dev/null
pkill -9 -f "chromium --disable-blink" 2>/dev/null
sleep 2
rm -f "$PROBEDIR"/.X99-lock "$X11SOCKDIR"/X99
setsid nohup Xvfb :99 -screen 0 1280x1024x24 >"$OUT/xvfb.log" 2>&1 &
n=0
while [ ! -e "$X11SOCKDIR"/X99 ]; do
  n=$((n+1)); [ "$n" -ge 20 ] && { echo "XVFB_NEVER_CAME_UP"; cat "$OUT/xvfb.log"; break; }
  sleep 1
done
echo "xvfb_socket=$([ -e "$X11SOCKDIR"/X99 ] && echo present || echo MISSING) after ${n}s"

echo "=== start headed chromium + CDP (WAIT for the debug endpoint) ==="
DISPLAY=:99 setsid nohup chromium --disable-blink-features=AutomationControlled \
  --no-first-run --remote-debugging-port=9222 about:blank >"$OUT/chrome.log" 2>&1 &
n=0
until curl -s -m 3 http://127.0.0.1:9222/json/version >"$OUT/version.json" 2>/dev/null && [ -s "$OUT/version.json" ]; do
  n=$((n+1)); [ "$n" -ge 20 ] && { echo "CDP_NEVER_CAME_UP"; tail -5 "$OUT/chrome.log"; break; }
  sleep 1
done
echo "cdp_version=$(head -c 120 "$OUT/version.json")"
echo "targets=$(curl -s -m 5 http://127.0.0.1:9222/json/list | python3 -c 'import json,sys;print([(t["type"],t["url"][:40]) for t in json.load(sys.stdin)])' 2>&1 | tail -1)"

echo "=== CHALLENGE probe: real SERP navigation + reading + screenshot ==="
python3 - <<'PYEOF' 2>&1 | tee "$OUT/serp.json"
import base64, json, sys, time
sys.path.insert(0, "/tmp")
from google_cdp_server import CDPConn, cdp_eval, get_page_target
URL = "https://www.google.com/search?q=golang+context+deadline&num=8"
out = {"nav_url": URL}
conn = None
try:
    for attempt in range(5):
        ws = get_page_target()
        if ws:
            out["target_ws"] = ws
            break
        time.sleep(2)
    path = (ws or "").replace("ws://127.0.0.1:9222", "")
    if not path.startswith("/"):
        raise RuntimeError("no page target: %r" % ws)
    conn = CDPConn(path)
    out["sanity"] = cdp_eval(conn, "1+1", budget=6)
    try:
        cdp_eval(conn, "location.href=%s;" % json.dumps(URL), budget=5)
    except Exception:
        pass
    time.sleep(7)
    out["landed_href"] = cdp_eval(conn, "location.href", budget=8)
    out["title"] = cdp_eval(conn, "document.title", budget=8)
    out["body_head"] = (cdp_eval(conn, "(document.body&&document.body.innerText||'').slice(0,900)", budget=8) or "")
    out["h3_titles"] = json.loads(cdp_eval(
        conn, "JSON.stringify(Array.prototype.slice.call(document.querySelectorAll('h3'))"
              ".slice(0,10).map(function(h){return h.textContent.trim()}))", budget=8) or "[]")
    low = (out["body_head"] or "").lower()
    out["sorry"] = int("sorry/index" in (out["landed_href"] or "") or "unusual traffic" in low)
    out["enablejs"] = int("enablejs" in (out["landed_href"] or "") or "enablejs" in low)
    r = conn.call("Page.captureScreenshot", {"format": "png"}, budget=15)
    data = r.get("data") if r else None
    if data:
        with open(""$PROBEDIR"/rpiwg3/residential-verdict.png", "wb") as f:
            f.write(base64.b64decode(data))
        out["screenshot"] = ""$PROBEDIR"/rpiwg3/residential-verdict.png"
except Exception as e:
    out["error"] = repr(e)
print(json.dumps(out, ensure_ascii=False, indent=1))
PYEOF

ls -la "$OUT"/*.png 2>/dev/null
echo "=== teardown ==="
pkill -9 -f "chromium --disable-blink" 2>/dev/null
pkill -9 -f "Xvfb :99" 2>/dev/null
sudo -n wg-quick down wg3 2>&1 | tail -2
sudo -n resolvectl domain eth0 '~.' 2>/dev/null
sudo -n resolvectl default-route eth0 true 2>/dev/null
sudo -n resolvectl flush-caches 2>/dev/null
sleep 2
echo "wg3_after=$(ip -br addr show wg3 2>&1 | tr -s ' ')"
echo "exit_v4_after=$(curl -4 -s --max-time 10 https://ifconfig.me 2>&1 | head -c 40)"
