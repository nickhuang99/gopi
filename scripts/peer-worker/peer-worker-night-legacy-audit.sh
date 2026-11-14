#!/bin/bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# peer-worker-night-legacy-audit.sh — HP 夜间 legacy-audit 执行器（GLM 免费窗口 23:00-09:00）
#
# SWEEP DISPOSITION (2026-09-12, devdoc/peer-inline-launchers-20260912.md):
# this was the THIRD inline worker-start path found on HP — an executable .sh
# that ran `/usr/local/bin/gopi worker --once` in a loop and never looked at
# the exit status (a refusal read exactly like a success, the same defect class
# as the two starters). NOTHING schedules it (no cron entry, no systemd unit,
# no shell-startup reference) — it is a hand-run tool — but the silent-success
# hole is fixed here: every round's status is checked, a non-zero round stops
# the loop and is reported loudly with the refusal code named.
#
# The inline `--once` round is DELIBERATE and is not the defect: this is a
# bounded per-round drainer for one campaign, not a launcher-shaped spawn
# (no setsid/nohup, no second log contract, no process accounting). A general
# worker launch belongs to the ONE launcher: ~/bin/gopi-worker-up.sh.
#
# 用法: bash peer-worker-night-legacy-audit.sh
# 每批: gopi worker --once（领一个 legacy-audit 任务 → GLM 审查 → 操作 audit DB）

cd /tmp
export PATH=/usr/local/bin:$PATH

# The agent-session guard's refusal status, named once (never a bare 3).
GOPI_WORKER_REFUSAL_EXIT_CODE="${GOPI_WORKER_REFUSAL_EXIT_CODE:-3}"
# The worker binary this drainer drives — a knob only so the refusal path is
# testable with a stub (the acceptance probe); the default is the real binary.
GOPI_WORKER_BIN="${GOPI_WORKER_BIN:-/usr/local/bin/gopi}"

# 确认 GLM settings 生效（settings.json -> .glm_settings.json）
if [ "$(readlink ~/.gopi/settings.json)" != ".glm_settings.json" ]; then
    ln -sf .glm_settings.json ~/.gopi/settings.json
fi

count_pending() {
    sqlite3 ~/.gopi/memory.db "SELECT COUNT(*) FROM tasks WHERE summary LIKE 'legacy-audit%' AND status='pending'"
}

echo "[$(date '+%H:%M')] legacy-audit 夜间执行开始——GLM 免费窗口"
echo "[$(date '+%H:%M')] 台账 pending: $(count_pending)"

# 串行消化——每批领一个跑完再下一个（worker --once 领最高优先）
for i in 1 2 3 4 5 6 7 8; do
    echo "[$(date '+%H:%M')] round $i: 起 worker"
    "$GOPI_WORKER_BIN" worker --once >> ~/.gopi/night-audit-worker.log 2>&1
    status=$?
    # CHECKED STATUS (the fix): a non-zero round is a FAILED round — named,
    # logged, with the refusal code spelled out and the round count recorded,
    # then the loop STOPS (silent continuation is the defect this closes).
    if [ "$status" -ne 0 ]; then
        echo "[$(date '+%H:%M')] FAILED: round $i — $GOPI_WORKER_BIN worker --once exited $status (refusal \$GOPI_WORKER_REFUSAL_EXIT_CODE=$GOPI_WORKER_REFUSAL_EXIT_CODE); no further round was started (see ~/.gopi/night-audit-worker.log)" | tee -a ~/.gopi/night-audit.log >&2
        exit "$status"
    fi
    LEFT=$(count_pending)
    echo "[$(date '+%H:%M')] round $i 完成——剩余 legacy-audit pending: $LEFT"
    [ "$LEFT" = "0" ] && break
    sleep 5
done

echo "[$(date '+%H:%M')] 全部完成——剩余: $(count_pending)"
