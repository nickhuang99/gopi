#!/bin/bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# HP sweep: every path that could auto-start a worker. Fast by design: the
# recursive /home/nick grep is bounded by depth and excludes dependency trees
# (an unbounded walk of the home directory takes minutes on this host).
set -u
H="$HOME"
EXCLUDES=(--exclude-dir=.git --exclude-dir=node_modules --exclude-dir=.cache
          --exclude-dir=.cargo --exclude-dir=.rustup --exclude-dir=.npm
          --exclude-dir=.mozilla --exclude-dir=.vscode --exclude-dir=.local
          --exclude-dir=.hermes)

echo "=== 1. crontab -l (per-user) ==="
crontab -l 2>&1

echo "=== 2. systemd --user units ==="
for u in "$H"/.config/systemd/user/*.service; do
  [ -e "$u" ] || continue
  echo "--- $u"; grep -E "^(Description|ExecStart)" "$u" 2>/dev/null
done
echo "--- user timers"
systemctl --user list-timers --all --no-pager 2>&1 | head -6

echo "=== 3. system cron ==="
ls /etc/cron.d /etc/cron.hourly /etc/cron.daily 2>&1 | head -20
grep -rn "gopi" /etc/cron.d /etc/crontab 2>/dev/null | head

echo "=== 4. .sh files under \$HOME (depth<=4, deps excluded) mentioning gopi ==="
find "$H" -maxdepth 4 -name '*.sh' -type f \
  -not -path '*/node_modules/*' -not -path '*/.cache/*' -not -path '*/.cargo/*' \
  -not -path '*/.rustup/*' -not -path '*/.npm/*' -not -path '*/.local/*' \
  -not -path '*/.vscode/*' 2>/dev/null | sort | while read -r f; do
    if grep -qI -e 'gopi' "$f" 2>/dev/null; then
      echo "HIT $f"
      grep -nI -e 'gopi worker' -e 'gopi-worker-up' -e 'worker-up.sh' "$f" 2>/dev/null | sed 's/^/      /'
    fi
done

echo "=== 5. files (any type) within depth 3 mentioning a worker-start shape ==="
grep -rIl --max-depth 3 "${EXCLUDES[@]}" \
  -e 'gopi worker' -e 'gopi-worker-up' -e 'worker-up.sh' "$H" 2>/dev/null | sort | head -30

echo "=== 6. the legacy audit script ==="
ls -la "$H/.gopi/hp-night-legacy-audit.sh" 2>&1
grep -rIn --max-depth 3 "${EXCLUDES[@]}" "hp-night-legacy-audit" "$H" 2>/dev/null | head
echo "--- legacy-audit task rows by status"
sqlite3 "$H/.gopi/memory.db" "select status,count(*) from tasks where summary like 'legacy-audit%' group by status" 2>&1

echo "=== 7. live gopi processes ==="
ps -eo pid,comm,args --no-headers | grep -E "[g]opi" || echo "(none)"

echo "=== 8. HP settings target ==="
ls -la "$H/.gopi/settings.json" "$H/.gopi/.glm_settings.json" 2>&1

echo "=== 9. installed launcher hash ==="
sha256sum "$H/.local/bin/gopi-worker-up.sh" 2>&1
