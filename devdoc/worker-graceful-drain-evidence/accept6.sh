#!/bin/bash
# Acceptance v6: the version-aware delivery arm.
# C) new CLI --restart against a MASTER-binary worker with a task in flight →
#    FILE-ONLY request, task must COMPLETE, exactly one new daemon.
# D) new CLI --restart against a NEW-binary worker (same version) → the SIGTERM
#    fast path is still used (the signal arm did not get lost).
set -u
R=/home/user/scratch/drain-accept
export GOPI_HOME=$R/home6 GOPI_MEMORY_DB=$R/home6/memory.db TMPDIR=$R/tmp6 GOPI_ALLOW_AGENT_SPAWN=1
OLD=$R/gopi-master
B=$R/gopi-drain
mkdir -p $R/home6 $R/tmp6
if [ ! -d $R/repo6/.git ]; then mkdir -p $R/repo6 && cd $R/repo6 && git init -q . && echo hi > README.md && git add README.md && git -c user.email=a@b -c user.name=a commit -qm init; fi
cd $R/repo6
say(){ echo; echo "=== $(date -Is) $* ==="; }
wd=$GOPI_HOME/.gopi/worker

say "C) foreign (master) binary worker, task in flight, --restart from the NEW binary"
GOPI_WORKER_IDLE_EXITS=3 $OLD worker --daemon 2>&1 | tail -1
sleep 3
PID=$(grep -ho '"pid":[0-9]*' $wd/worker-state-* 2>/dev/null | head -1 | cut -d: -f2)
echo "old worker pid=$PID version=$(grep -o '\"version\":\"[^\"]*\"' $wd/worker-state-$PID.json)  this CLI version=$(echo)"
$B task submit "long drain probe 10" --task-id drain-long-10 --repo $R/repo6 \
  --payload "Create probe10.md in the current directory with TEN sections (## headings), each with exactly five sentences about graceful worker drain, then append a numbered list of 15 items, then print the line count. Verify each count from disk before reporting." 2>&1 | tail -1
for i in $(seq 1 90); do grep -q '"last_round_start"' $wd/worker-state-$PID.json 2>/dev/null && break; sleep 1; done
echo "round in flight: $(cat $wd/worker-state-$PID.json 2>/dev/null | head -c 220)"
timeout 10m $B worker --restart --wait 10m > $R/t6-C.txt 2>&1; echo "C exit=$?" >> $R/t6-C.txt
cat $R/t6-C.txt
echo "C: task drain-long-10 must be COMPLETED:"
$B task show drain-long-10 2>&1 | grep -E "^(status|session_id|result)"
$B worker --status 2>&1 | head -5
echo "state files: $(ls $wd/ | grep -c worker-state-)"

say "D) SAME-binary worker: the signal fast path must still be taken"
GOPI_WORKER_IDLE_EXITS=3 $B worker --daemon 2>&1 | tail -1
sleep 3
PID=$(grep -ho '"pid":[0-9]*' $wd/worker-state-* 2>/dev/null | head -1 | cut -d: -f2)
echo "worker pid=$PID version=$(grep -o '\"version\":\"[^\"]*\"' $wd/worker-state-$PID.json)"
timeout 5m $B worker --stop --wait 5m > $R/t6-D.txt 2>&1; echo "D exit=$?" >> $R/t6-D.txt
head -4 $R/t6-D.txt
grep -c "asking pid .* (SIGTERM, bound" $R/t6-D.txt | sed 's/^/D: SIGTERM arm lines = /'
say "D) VERDICT: no daemon left"
$B worker --status 2>&1 | head -3
echo SCRIPT_EXIT=0
