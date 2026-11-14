#!/usr/bin/env bash
# cleanup.sh <label> <binary> — probe 3: what does the screen look like after /quit?
set -uo pipefail
LABEL="${1:?usage}"; BIN="${2:?usage}"
EVID=/home/user/workspace/gopi-01a0d5082fea7dbcb2238fc103f10350/devdoc/evidence/sgr-grid
WORK="$HOME/workspace/_tmp/ui-probe/$LABEL"
mkdir -p "$WORK" || exit 1
SCRIPT="$WORK/script-cleanup.txt"
cat >"$SCRIPT" <<'SCRIPT_EOF'
sleep 2.5
waitfor gopi chat
shot 0-before
type /quit
submit
sleep 1.0
shot 1-just-after-quit
exit
shot 2-after-exit
SCRIPT_EOF
cd "$WORK" || exit 1
rm -rf "$WORK/home"
env -u DEEPSEEK_API_KEY -u DEEPSEEK_BASE_URL -u DEEPSEEK_MODEL \
    -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN -u ANTHROPIC_BASE_URL -u ANTHROPIC_MODEL \
    -u GOPI_TASK_REPORT_PATH -u GOPI_TASK_REPORT_OWNER -u GOPI_EXECUTOR_SESSION_ID \
    -u GOPI_MEMORY_DB \
    "GOPI_HOME=$WORK/home" "GOPI_HUMAN_TERMINAL=1" \
    "LANG=C.utf8" "LC_ALL=C.utf8" "TERM=xterm-256color" \
    python3 "$EVID/termgrid.py" "$BIN" 100 30 "$SCRIPT" >"$WORK/grid.log" 2>&1
echo "exit=$? log=$WORK/grid.log"; tail -3 "$WORK/grid.log"
for f in 0-before 1-just-after-quit 2-after-exit; do echo "===== $f"; sed -n '1,31p' "$WORK/$f.grid.txt" 2>/dev/null; done
echo "===== tail of raw stream (vis):"
tail -c 400 "$WORK/2-after-exit.raw.txt" | cat -v
