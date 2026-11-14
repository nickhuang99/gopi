#!/bin/bash
# amend3-residential-transport.sh — task (amendment to 01a0a4dd), cells A2/B2 feasibility.
#
# QUESTION the 2x2 needs answered for its residential ROW (A2 = CDP x residential,
# B2 = X11-no-CDP x residential): is there a residential egress anywhere in this
# fleet on which a browser could even attempt the google search page?
#
# This script measures the RESIDENTIAL host's own egress (the HP laptop, the only
# genuinely residential path available — the same home link that peerbox and rpi5
# sit behind), distinguishing THREE different failure modes that a naive curl
# cannot tell apart:
#   1. DNS poisoning        -> typed name resolves to a non-Google address
#   2. network-level block  -> TCP 443 to a REAL google front-end never completes
#   3. application challenge-> TCP+TLS succeed, Google answers with a wall
# Only (3) could host cells A2/B2. (1) can be bypassed with --resolve; (2) cannot.
#
# Family pinning is explicit (curl -4 / -6) and the DNS is bypassed with
# --resolve, so a poisoned resolver cannot be mistaken for a blocked transport.
# Controls (bing, and a known-good non-google host) are measured in the same run.
set -u
echo "=== residential transport probe — $(date -u +%FT%TZ) ==="
echo "host=$(hostname)"

echo "--- 1. egress identity (what this IP IS, measured not inferred) ---"
curl -s -4 --max-time 15 https://ipinfo.io/json || echo "ipinfo_v4_failed"
echo
curl -s -6 --max-time 10 https://ipinfo.io/json || echo "ipinfo_v6_failed(no v6 egress)"
echo

echo "--- 2. DNS answer for the name (poisoning check) ---"
for n in www.google.com google.com www.bing.com; do
  printf '%s -> ' "$n"
  getent ahostsv4 "$n" | awk '{print $1}' | sort -u | tr '\n' ' '
  echo
done

echo "--- 3. typed-name reachability, family pinned ---"
for f in 4 6; do
  printf 'google-v%s typed: ' "$f"
  curl -s -o /dev/null -w 'code=%{http_code} bytes=%{size_download} ip=%{remote_ip}\n' \
    -"$f" --max-time 8 https://www.google.com/ 2>&1 || echo "exit=$?"
done
printf 'bing-v4 typed (CONTROL): '
curl -s -o /dev/null -w 'code=%{http_code} ip=%{remote_ip}\n' -4 --max-time 8 https://www.bing.com/ 2>&1 || echo "exit=$?"

echo "--- 4. DNS BYPASSED: the real google front-end IPs, --resolve ---"
# These are the IPv4 literals measured on the remotebox (142.251.15x.119 family)
# and used as --host-resolver-rules pins by every arm of the parent experiment.
for ip in 142.251.156.119 142.251.153.119 142.251.151.119; do
  printf 'resolve %s: ' "$ip"
  curl -s -o /dev/null -w 'code=%{http_code} bytes=%{size_download} ip=%{remote_ip} connect=%{time_connect}\n' \
    -4 --max-time 10 --resolve "www.google.com:443:$ip" https://www.google.com/ 2>&1 || echo "exit=$?"
done

echo "--- 5. raw TCP 443 to the real IP (no DNS, no TLS, no curl) ---"
for ip in 142.251.156.119 142.251.153.119; do
  printf 'tcp443 %s: ' "$ip"
  if timeout 8 bash -c "exec 3<>/dev/tcp/$ip/443" 2>/dev/null; then echo OPEN; else echo "BLOCKED/TIMEOUT(exit=$?)"; fi
done
printf 'tcp443 202.89.233.100 (bing, CONTROL): '
if timeout 8 bash -c 'exec 3<>/dev/tcp/202.89.233.100/443' 2>/dev/null; then echo OPEN; else echo "BLOCKED/TIMEOUT(exit=$?)"; fi

echo "--- 6. TLS handshake to the real IP with the correct SNI ---"
for ip in 142.251.156.119; do
  echo "openssl s_client -connect $ip:443 -servername www.google.com:"
  timeout 12 bash -c "echo | openssl s_client -connect $ip:443 -servername www.google.com 2>&1 | head -8" || echo "handshake_failed"
done

echo "--- 7. upstream path (where the block sits) ---"
ip -4 route get 142.251.156.119 2>&1 | head -2

echo "=== END residential transport probe — $(date -u +%FT%TZ) ==="
