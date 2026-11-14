#!/bin/bash
# rpi_wg3_arm.sh — RESIDENTIAL EGRESS ARM, run ON the RPi5.
#
# Follows memory #733 (rpi5-search) exactly: bring the tunnel up, verify the
# handshake, point DNS at the tunnel, verify connectivity, run the search probe,
# and take the tunnel DOWN again — the teardown is not optional (long-lived UDP
# to the VPN endpoint is what gets a client blocked) and it is wired into a trap
# so it happens on every exit path.
#
# What this arm answers: is the *IP reputation* the binding constraint? Same
# instrument (headed chromium + CDP, masked ws) as the search-bot CDP arm, only
# the egress changes — datacenter IPv4/IPv6 vs a residential exit.
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

status() {
  echo "wg3_unit=$(systemctl is-active wg-quick@wg3 2>&1)"
  echo "wg3_link=$(ip -br link show wg3 2>&1)"
  echo "wg3_handshake=$(sudo -n wg show wg3 2>/dev/null | grep -c 'latest handshake')"
  echo "exit_v4=$(curl -4 -s --max-time 10 https://ifconfig.me 2>&1 | head -c 40)"
}

restore() {
  echo "=== RESTORE (trap): taking wg3 down + putting DNS back ==="
  sudo -n wg-quick down wg3 2>&1 | tail -3
  sudo -n resolvectl domain eth0 '~.' 2>/dev/null
  sudo -n resolvectl default-route eth0 true 2>/dev/null
  sudo -n resolvectl flush-caches 2>/dev/null
  sleep 2
  status
}
trap restore EXIT

echo "=== PRE STATE ==="
status

echo "=== 1) bring the tunnel up ==="
sudo -n wg-quick up wg3 2>&1 | tail -5
sleep 5
echo "handshake=$(sudo -n wg show wg3 2>/dev/null | grep 'latest handshake' | tail -1)"

echo "=== 2) DNS through the tunnel (memory #733: eth0 search domain must be cleared) ==="
sudo -n resolvectl flush-caches 2>/dev/null
sudo -n resolvectl domain wg3 '~.' 2>/dev/null
sudo -n resolvectl domain eth0 '' 2>/dev/null
sudo -n resolvectl default-route wg3 true 2>/dev/null
sudo -n resolvectl dns wg3 8.8.4.4 8.8.8.8 2>/dev/null
sudo -n resolvectl default-route eth0 false 2>/dev/null
sleep 2

echo "=== 3) connectivity + EXIT IP (the variable under test) ==="
ping -c1 -4 -W3 8.8.8.8 2>&1 | tail -2
echo "exit_v4=$(curl -4 -s --max-time 12 https://ifconfig.me 2>&1 | head -c 40)"
echo "exit_v6=$(curl -6 -s --max-time 8 https://ifconfig.me 2>&1 | head -c 40)"
curl -4 -s -o "$OUT/curl-serp.html" -w "curl_google http=%{http_code} remote_ip=%{remote_ip} bytes=%{size_download}\n" \
  "https://www.google.com/search?q=golang+context+deadline" 2>&1 | tail -1
printf 'curl challenge markers: sorry=%s enablejs=%s h3=%s\n' \
  "$(grep -c 'sorry/index' "$OUT/curl-serp.html" 2>/dev/null)" \
  "$(grep -c 'enablejs' "$OUT/curl-serp.html" 2>/dev/null)" \
  "$(grep -oc '<h3' "$OUT/curl-serp.html" 2>/dev/null)"

echo "=== 4) headed chromium + CDP probe on the residential exit ==="
python3 "$PROBEDIR"/rpi_arm_probe.py "golang context deadline" 2>&1 | tee "$OUT/probe.json"

echo "=== 5) teardown happens in the trap ==="
