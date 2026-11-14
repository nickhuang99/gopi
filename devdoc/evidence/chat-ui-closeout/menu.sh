#!/usr/bin/env bash
# menu.sh <label> <binary> — probe: does Enter accept the /-menu selection?
# Reproduces the operator's report: navigate the menu, choose, and see whether
# the choice reaches the input slot.
set -uo pipefail
LABEL="${1:?usage: menu.sh <label> <binary>}"
BIN="${2:?usage: menu.sh <label> <binary>}"
EVID=/home/user/workspace/gopi-01a0d5082fea7dbcb2238fc103f10350/devdoc/evidence/sgr-grid
WORK="$HOME/workspace/_tmp/ui-probe/$LABEL"
mkdir -p "$WORK" || exit 1
[ -x "$BIN" ] || { echo "FAIL: no executable at $BIN" >&2; exit 1; }

SCRIPT="$WORK/script-menu.txt"
cat >"$SCRIPT" <<'SCRIPT_EOF'
sleep 2.5
waitfor gopi chat
type /
sleep 1.5
shot 1-menu-open
key down
sleep 0.4
shot 2-menu-down
key enter
sleep 1.2
shot 3-after-enter
send \x04
sleep 1.0
exit
SCRIPT_EOF

cd "$WORK" || exit 1
rm -rf "$WORK/home"
env -u DEEPSEEK_API_KEY -u DEEPSEEK_BASE_URL -u DEEPSEEK_MODEL \
    -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN -u ANTHROPIC_BASE_URL -u ANTHROPIC_MODEL \
    -u GOPI_TASK_REPORT_PATH -u GOPI_TASK_REPORT_OWNER -u GOPI_EXECUTOR_SESSION_ID \
    -u GOPI_MEMORY_DB \
    "GOPI_HOME=$WORK/home" "GOPI_HUMAN_TERMINAL=1" \
    "LANG=C.utf8" "LC_ALL=C.utf8" "TERM=xterm-256color" \
    python3 "$EVID/termgrid.py" "$BIN" 100 30 "$SCRIPT" \
    >"$WORK/grid.log" 2>&1
echo "exit=$? log=$WORK/grid.log"
for f in "$WORK"/1-menu-open.grid.txt "$WORK"/2-menu-down.grid.txt "$WORK"/3-after-enter.grid.txt; do
    echo "===== $f"; sed -n '1,32p' "$f" 2>/dev/null
done
