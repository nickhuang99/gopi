#!/bin/bash
# remote-probe-a.sh — Arm A (production gateway, CDP, IPv6) with its 15-min /sorry
# cache momentarily bypassed so the cell yields a REAL navigation instead of a
# cached answer. Production behaviour is restored at the end of this script
# (gateway restarted normally, env override gone, health verified).
#
# Why: with the cache warm, GET /search returns in 0ms with the cached message —
# that is a statement about the IP, not a fresh observation. Arm A must be
# measured with the same instrument freshness as arms B/C (a live navigation).
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

echo "=== STOP PRODUCTION GATEWAY (Chrome stays up: only the gateway restarts) ==="
pkill -9 -f google_cdp_server 2>/dev/null
sleep 2
echo "gateway_procs_left=$(pgrep -fc google_cdp_server 2>/dev/null || echo 0)"
ss -ltn | grep -q ":9232" && echo "9232 still listening" || echo "9232 free"

echo "=== START GATEWAY WITH GOPI_GW_FORCE_PROBE=1 (fresh navigation) ==="
GOPI_GW_FORCE_PROBE=1 setsid nohup python3 "$PROBEDIR"/google_cdp_server.py --port 9232 \
  </dev/null >"$OUT/gw-probe.log" 2>&1 &
disown
sleep 5
echo "health=$(curl -s -m 10 http://127.0.0.1:9232/health)"
echo "--- forced search (Arm A, CDP, IPv6) ---"
curl -s -m 60 "http://127.0.0.1:9232/search?q=golang+context+deadline&n=5" | tee "$OUT/armA-probe.json"
echo
echo "--- probe gateway log ---"
cat "$OUT/gw-probe.log"

echo "=== RESTORE PRODUCTION GATEWAY (no env override) ==="
pkill -9 -f google_cdp_server 2>/dev/null
sleep 2
systemctl start google-cdp.service
sleep 8
echo "service=$(systemctl is-active google-cdp.service)"
echo "health=$(curl -s -m 10 http://127.0.0.1:9232/health)"
echo "search=$(curl -s -m 30 'http://127.0.0.1:9232/search?q=golang+context+deadline&n=3')"
