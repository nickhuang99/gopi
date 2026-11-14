#!/bin/bash
# gw-force-probe.sh — run the search-bot gateway ONCE with the /sorry cache
# bypassed (GOPI_GW_FORCE_PROBE=1) so Arm A yields a REAL navigation, then put
# production back exactly as it was.
#
# WHY A SCRIPT FILE (not an inline ssh command): `pkill -f google_cdp_server`
# matches the invoking shell's OWN command line ("bash -c … google_cdp_server …")
# and kills it — the remote pkill self-kill. Keeping the pattern inside a script
# means the only cmdlines carrying it are the script and the gateway itself.
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
OUT="$PROBEDIR"/armA
mkdir -p "$OUT"

echo "=== stop gateway only (Chrome keeps running) ==="
pkill -9 -f "python3 "$PROBEDIR"/google_cdp_server.py" 2>/dev/null
sleep 2
echo "gateway_left=$(pgrep -fc 'python3 "$PROBEDIR"/google_cdp_server.py' 2>/dev/null || echo 0)"

echo "=== start gateway with the force-probe override ==="
GOPI_GW_FORCE_PROBE=1 setsid nohup python3 "$PROBEDIR"/google_cdp_server.py --port 9232 \
  </dev/null >"$OUT/gw-probe.log" 2>&1 &
echo "started_pid=$!"
sleep 6
echo "health=$(curl -s -m 10 http://127.0.0.1:9232/health)"
echo "--- forced search (Arm A = CDP + IPv6) ---"
curl -s -m 90 "http://127.0.0.1:9232/search?q=golang+context+deadline&n=5" | tee "$OUT/armA-probe.json"
echo
echo "--- gateway log ---"
tail -12 "$OUT/gw-probe.log"

echo "=== restore production gateway (no override) ==="
pkill -9 -f "python3 "$PROBEDIR"/google_cdp_server.py" 2>/dev/null
sleep 2
systemctl start google-cdp.service
sleep 8
echo "service=$(systemctl is-active google-cdp.service)"
echo "health=$(curl -s -m 10 http://127.0.0.1:9232/health)"
echo "search=$(curl -s -m 40 'http://127.0.0.1:9232/search?q=golang+context+deadline&n=3')"
