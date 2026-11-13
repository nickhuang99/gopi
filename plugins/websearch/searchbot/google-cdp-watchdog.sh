#!/bin/bash
# google-cdp-watchdog.sh — keep the Google SERP CDP stack alive on search-bot.
# Loop: ensure Xvfb+Chrome+gateway are up; health-check the gateway (:9232);
# restart the whole stack after 3 consecutive failures. Run as a systemd unit
# (google-cdp.service) with Restart=always so boot + crash recovery are covered.
# Logs to /tmp/google-cdp-watchdog.log (rotated by the script on restart).

LOG=/tmp/google-cdp-watchdog.log
HEALTH_URL="http://127.0.0.1:9232/health"
FAILS=0

log() { echo "$(date '+%F %T') $*" >>"$LOG"; }

ensure_stack() {
  if ! ss -tln 2>/dev/null | grep -q ":9232 "; then
    log "gateway down — restarting stack"
    bash /tmp/restart_stack.sh >>"$LOG" 2>&1
    sleep 5
    return 1
  fi
  if ! pgrep -x google-chrome >/dev/null && ! pgrep -x chrome >/dev/null; then
    log "chrome dead — restarting stack"
    bash /tmp/restart_stack.sh >>"$LOG" 2>&1
    sleep 5
    return 1
  fi
  return 0
}

while true; do
  ensure_stack
  # health check: gateway must answer within 10s
  if timeout 15 curl -s -m 10 "$HEALTH_URL" -o /dev/null 2>>"$LOG"; then
    FAILS=0
  else
    FAILS=$((FAILS + 1))
    log "health check fail #$FAILS"
    if [ "$FAILS" -ge 3 ]; then
      log "3 consecutive failures — full restart"
      bash /tmp/restart_stack.sh >>"$LOG" 2>&1
      FAILS=0
    fi
  fi
  sleep 15
done
