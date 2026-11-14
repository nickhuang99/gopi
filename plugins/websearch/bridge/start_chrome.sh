#!/bin/bash
# start_chrome.sh — Xvfb + headed google-chrome (CDP :9222) on search-bot.
#
# THE IPv6 FIX (Epic: google search quality):
#   search-bot's IPv4 (DigitalOcean datacenter) is blocked by google.com with the
#   hard reCAPTCHA /sorry wall (even for a headed Chrome — Runtime.evaluate hangs).
#   Its IPv6 (2001:db8:4:1d0::/64) only gets the soft "enablejs" wall, which a
#   JS-capable browser passes automatically. Chrome's own Happy-Eyeballs resolver
#   ignores the OS getaddrinfo IPv6 preference, so we FORCE google.com -> Google
#   IPv6 anycast (2001:4860:482a:7700::) via --host-resolver-rules.
#
# Usage (on search-bot, as root): bash /tmp/start_chrome.sh
pkill -9 -f google_cdp_server 2>/dev/null
pkill -9 -x chrome 2>/dev/null
pkill -9 -x google-chrome 2>/dev/null
pkill -9 -x Xvfb 2>/dev/null
sleep 2
rm -f /tmp/.X99-lock /tmp/.X11-unix/X99
setsid Xvfb :99 -screen 0 1280x1024x24 </dev/null >/tmp/xvfb.log 2>&1 &
echo "xvfb_started"
sleep 2
rm -rf /tmp/chromeprofile && mkdir -p /tmp/chromeprofile
setsid env DISPLAY=:99 google-chrome --no-sandbox --disable-dev-shm-usage \
    --remote-debugging-port=9222 --disable-blink-features=AutomationControlled \
    --exclude-switches=enable-automation --no-first-run --disable-default-apps \
    --host-resolver-rules="MAP www.google.com [2001:4860:482a:7700::],MAP google.com [2001:4860:482a:7700::]" \
    --disable-quic \
    --user-data-dir=/tmp/chromeprofile about:blank </dev/null >/tmp/chromium.log 2>&1 &
echo "chrome_pid=$!"
# wait for CDP endpoint
for i in $(seq 1 20); do
  if curl -s -m 2 -o /dev/null http://127.0.0.1:9222/json/version; then
    echo "cdp_up_after=${i}s"; break
  fi
  sleep 1
done
echo "--- CDP version ---"
curl -s -m 3 http://127.0.0.1:9222/json/version
echo
