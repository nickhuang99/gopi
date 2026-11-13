#!/bin/bash
# restart_stack.sh — clean restart of the search-bot Google SERP stack:
#   Xvfb :99 + headed Chrome (CDP :9222, IPv6-forced) + CDP gateway (:9232).
# Use when the gateway is down / results degraded: bash /tmp/restart_stack.sh
pkill -9 -f google_cdp_server 2>/dev/null
pkill -9 -x chrome 2>/dev/null
pkill -9 -x google-chrome 2>/dev/null
pkill -9 -x Xvfb 2>/dev/null
sleep 3
bash /tmp/start_chrome.sh
# verify chrome version
curl -s -m 3 http://127.0.0.1:9222/json/version | python3 -c "import sys,json;print('chrome', json.load(sys.stdin)['Browser'])"
setsid nohup python3 /tmp/google_cdp_server.py --port 9232 >/tmp/cdp_server.log 2>&1 &
echo "gw_pid=$!"
sleep 4
echo "--- gw log ---"
cat /tmp/cdp_server.log
echo "--- gw smoke test ---"
timeout 60 python3 -c "
import urllib.request, json
try:
    d=json.load(urllib.request.urlopen('http://127.0.0.1:9232/search?q=smoke+test&n=3',timeout=50))
    print(json.dumps(d,ensure_ascii=False)[:600])
except Exception as e:
    print('QUERYERR', repr(e))
"
