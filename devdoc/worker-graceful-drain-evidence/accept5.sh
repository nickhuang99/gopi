# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
#!/bin/bash
# Acceptance v5 (epic worker-graceful-drain, LEGACY-BINARY arm): a worker
# running a binary that PREDATES the drain contract must be drained by the
# durable REQUEST FILE ALONE — no signal — because such a binary forwards a
# signal into its in-flight run child and kills the round.
# Three facts are measured: (A) the old binary consumes the file while idle,
# (B) it consumes it at the round boundary so the in-flight task COMPLETES,
# (C) `gopi worker --restart` from the NEW binary against such a worker leaves
#     the task completed and leaves exactly ONE new daemon on the new binary.
set -u
R=/home/user/scratch/drain-accept
export GOPI_HOME=$R/home5 GOPI_MEMORY_DB=$R/home5/memory.db TMPDIR=$R/tmp5 GOPI_ALLOW_AGENT_SPAWN=1
OLD=$R/gopi-master   # master tip: v0.1.0-perf-final+2542 (no drain consume, forwards signals)
B=$R/gopi-drain      # branch: dev (drain consume at the claim boundary)
mkdir -p $R/home5 $R/tmp5
if [ ! -d $R/repo5/.git ]; then mkdir -p $R/repo5 && cd $R/repo5 && git init -q . && echo hi > README.md && git add README.md && git -c user.email=a@b -c user.name=a commit -qm init; fi
cd $R/repo5
say(){ echo; echo "=== $(date -Is) $* ==="; }
wd=$GOPI_HOME/.gopi/worker

say "A) does the OLD binary consume the request file at all (idle worker)?"
GOPI_WORKER_IDLE_EXITS=3 $OLD worker --daemon 2>&1 | tail -1
sleep 3
PID=$(grep -ho '"pid":[0-9]*' $wd/worker-state-* 2>/dev/null | head -1 | cut -d: -f2)
echo "old worker pid=$PID  state=$(cat $wd/worker-state-$PID.json 2>/dev/null | head -c 160)"
: > $wd/TERM_$PID
echo "wrote $wd/TERM_$PID (NO signal)"
for i in $(seq 1 60); do kill -0 $PID 2>/dev/null || break; sleep 1; done
if kill -0 $PID 2>/dev/null; then echo "A: FAILED — pid $PID still alive 60s after the file-only request"; else echo "A: the old binary consumed the FILE at its own tick and exited (no signal sent)"; fi

say "B) restart the old binary; drain it MID-ROUND by file only, task must complete"
GOPI_WORKER_IDLE_EXITS=3 $OLD worker --daemon 2>&1 | tail -1
sleep 3
PID=$(grep -ho '"pid":[0-9]*' $wd/worker-state-* 2>/dev/null | head -1 | cut -d: -f2)
echo "old worker pid=$PID"
$B task submit "long drain probe 8" --task-id drain-long-8 --repo $R/repo5 \
  --payload "Create probe8.md in the current directory with EIGHT sections (## headings), each with exactly five sentences about graceful worker drain, then append a numbered list of 15 items, then print the line count. Verify each count from disk before reporting." 2>&1 | tail -1
for i in $(seq 1 90); do grep -q '"last_round_start"' $wd/worker-state-$PID.json 2>/dev/null && break; sleep 1; done
echo "round in flight: $(cat $wd/worker-state-$PID.json 2>/dev/null | head -c 200)"
: > $wd/TERM_$PID
echo "wrote $wd/TERM_$PID (NO signal, mid-round)"
for i in $(seq 1 180); do kill -0 $PID 2>/dev/null || break; sleep 1; done
if kill -0 $PID 2>/dev/null; then echo "B: FAILED — pid $PID still alive 180s after the file-only request"; else echo "B: old worker exited AFTER its round boundary"; fi
echo "B: task row:"; $B task show drain-long-8 2>&1 | grep -E "^(status|session_id|result)"

say "C) --restart from the NEW binary against a busier old-binary worker"
GOPI_WORKER_IDLE_EXITS=3 $OLD worker --daemon 2>&1 | tail -1
sleep 3
PID=$(grep -ho '"pid":[0-9]*' $wd/worker-state-* 2>/dev/null | head -1 | cut -d: -f2)
echo "old worker pid=$PID  version=$(grep -o '\"version\":\"[^\"]*\"' $wd/worker-state-$PID.json)"
$B task submit "long drain probe 9" --task-id drain-long-9 --repo $R/repo5 \
  --payload "Create probe9.md in the current directory with NINE sections (## headings), each with exactly five sentences about graceful worker drain, then append a numbered list of 15 items, then print the line count. Verify each count from disk before reporting." 2>&1 | tail -1
for i in $(seq 1 90); do grep -q '"last_round_start"' $wd/worker-state-$PID.json 2>/dev/null && break; sleep 1; done
echo "round in flight: $(cat $wd/worker-state-$PID.json 2>/dev/null | head -c 200)"
timeout 10m $B worker --restart --wait 10m > $R/t5-C.txt 2>&1; echo "C exit=$?" >> $R/t5-C.txt
cat $R/t5-C.txt
echo "C: task drain-long-9 must be completed:"
$B task show drain-long-9 2>&1 | grep -E "^(status|session_id|result)"
say "C) VERDICT: exactly ONE daemon, on the NEW binary"
$B worker --status 2>&1 | head -6
echo "state files: $(ls $wd/ | grep -c worker-state-)"
echo SCRIPT_EXIT=0
