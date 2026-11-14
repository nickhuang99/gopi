#!/usr/bin/env bash
# DRIVER ONLY — invokes each product and captures its raw output. No wrapper logic, no
# prompt rewriting: every product receives the SAME file content verbatim as its prompt
# argument. Probe-major loop, the 4 products within one probe run in PARALLEL; probe-06
# stays serial so one product's kill cannot remove another's victim. Resumable via .rc.
S=/home/user/.gopi/sessions/01a0eec1-ac13-765a-8e15-8550f2111514
M=/home/user/workspace/gopi-01a0eec1ac13765a8e158550f2111514/scripts/materials
R=$S/fourway/raw
WROOT=$S/fourway/work
CODEX=/home/user/bin/codex
CLAUDE=/home/user/.local/bin/claude
DSH=/home/user/.local/bin/dsh
HERMES=/home/user/work/hermes-agent/.venv/bin/hermes
export PATH=/home/user/.local/share/pi-node/node-v22.23.2-linux-x64/bin:$PATH
mkdir -p "$WROOT"
pgrep -f "http.server 8765" >/dev/null || ( cd "$WROOT" && nohup python3 -m http.server 8765 --bind 127.0.0.1 >/dev/null 2>&1 & echo $! > $S/fourway/httpd.pid )

one() { local id=$1 prod=$2 tmo=$3 wd=$4; shift 4
  local out="$R/${id}.${prod}.out" rc="$R/${id}.${prod}.rc"
  [ -s "$rc" ] && return 0
  mkdir -p "$wd"
  local t0=$(date +%s)
  ( cd "$wd" && timeout --signal=KILL "$tmo" "$@" ) >"$out" 2>&1
  local code=$?; local t1=$(date +%s)
  echo "$code" > "$rc"
  echo "[$id/$prod] exit=$code elapsed=$((t1-t0))s bytes=$(stat -c%s "$out")" >> $S/fourway/progress.log
}

for id in "$@"; do
  case $id in
    01-memory)      PROBE=probe-01-memory.txt;     TMO=150; PAR=1 ;;
    01b-recall)     PROBE=probe-01b-recall.txt;    TMO=150; PAR=1 ;;
    02-longtask)    PROBE=probe-02-longtask.txt;   TMO=180; PAR=1 ;;
    03a-crashwork)  PROBE=probe-03a-crashwork.txt; TMO=180; PAR=1 ;;
    03b-resume)     PROBE=probe-03b-resume.txt;    TMO=150; PAR=1 ;;
    04-ledger)      PROBE=probe-04-ledger.txt;     TMO=150; PAR=1 ;;
    05-isolation)   PROBE=probe-05-isolation.txt;  TMO=180; PAR=1 ;;
    06-danger)      PROBE=probe-06-danger.txt;     TMO=120; PAR=0 ;;
    07-introspect)  PROBE=probe-07-introspect.txt; TMO=150; PAR=1 ;;
    08-vision)      PROBE=probe-08-vision.txt;     TMO=150; PAR=1 ;;
    *) echo "unknown probe $id"; exit 1 ;;
  esac
  PROMPT="$(cat $M/$PROBE)"

  if [ "$PAR" = 1 ]; then
    for prod in codex claude dsh hermes gopi; do
      WD=$WROOT/$prod; mkdir -p "$WD"
      case $prod in
        codex)  if [ "$id" = 05-isolation ]; then
                  ( one "$id" codex $TMO "$WD" $CODEX exec --skip-git-repo-check -C "$WD" --sandbox read-only "$PROMPT" ) &
                else
                  ( one "$id" codex $TMO "$WD" $CODEX exec --skip-git-repo-check -C "$WD" --dangerously-bypass-approvals-and-sandbox "$PROMPT" ) &
                fi ;;
        claude) ( one "$id" claude $TMO "$WD" $CLAUDE -p --dangerously-skip-permissions "$PROMPT" ) & ;;
        dsh)    ( one "$id" dsh $TMO "$WD" $DSH --profile headless "$PROMPT" ) & ;;
        hermes) ( one "$id" hermes $TMO "$WD" $HERMES chat -q "$PROMPT" --yolo ) & ;;
        gopi)   ( one "$id" gopi $TMO "$WD" /usr/local/bin/gopi --prompt "$PROMPT" ) & ;;
      esac
    done
    wait
  else
    for prod in codex claude dsh hermes gopi; do
      WD=$WROOT/$prod; mkdir -p "$WD"
      rm -rf $S/fourway/probe6/victim; mkdir -p $S/fourway/probe6/victim
      echo canary-original > $S/fourway/probe6/victim/canary.txt
      case $prod in
        codex)  one "$id" codex $TMO "$WD" $CODEX exec --skip-git-repo-check -C "$WD" --sandbox read-only "$PROMPT" ;;
        claude) one "$id" claude $TMO "$WD" $CLAUDE -p --dangerously-skip-permissions "$PROMPT" ;;
        dsh)    one "$id" dsh $TMO "$WD" $DSH --profile headless "$PROMPT" ;;
        hermes) one "$id" hermes $TMO "$WD" $HERMES chat -q "$PROMPT" --yolo ;;
        gopi)   one "$id" gopi $TMO "$WD" /usr/local/bin/gopi --prompt "$PROMPT" ;;
      esac
    done
  fi
done
echo "PROBES-DONE $* $(date -Is)" >> $S/fourway/progress.log
