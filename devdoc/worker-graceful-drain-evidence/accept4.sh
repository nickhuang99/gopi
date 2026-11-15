#!/bin/bash
# Acceptance v4 (epic worker-graceful-drain): the REPLACEMENT daemon must run
# the CURRENT binary — proof = the old worker is the MASTER build
# (v0.1.0-perf-final+2542) and the new one reports the branch build's version,
# while the in-flight task still completes.
set -u
R=/home/user/scratch/drain-accept
export GOPI_HOME=$R/home4 GOPI_MEMORY_DB=$R/home4/memory.db TMPDIR=$R/tmp4 GOPI_ALLOW_AGENT_SPAWN=1
OLD=$R/gopi-master   # master tip build: v0.1.0-perf-final+2542
B=$R/gopi-drain      # branch build: dev
mkdir -p $R/home4 $R/tmp4
if [ ! -d $R/repo4/.git ]; then mkdir -p $R/repo4 && cd $R/repo4 && git init -q . && echo hi > README.md && git add README.md && git -c user.email=a@b -c user.name=a commit -qm init; fi
cd $R/repo4
say(){ echo; echo "=== $(date -Is) $* ==="; }

say "versions: OLD=$($OLD version | head -1)  NEW=$($B version | head -1)"

say "start the worker daemon from the MASTER binary (the worker being replaced)"
GOPI_WORKER_IDLE_EXITS=3 $OLD worker --daemon 2>&1 | tail -1
sleep 2
$B worker --status 2>&1 | head -3

say "enqueue the long task and wait for its round to be in flight"
$B task submit "long drain probe 7" --task-id drain-long-7 --repo $R/repo4 \
  --payload "Create probe7.md in the current directory with SEVEN sections (## headings), each with exactly five sentences about graceful worker drain, then append a numbered list of 15 items, then print the line count. Verify each count from disk before reporting." 2>&1 | tail -1
for i in $(seq 1 90); do grep -q '"last_round_start"' $GOPI_HOME/.gopi/worker/worker-state-* 2>/dev/null && break; sleep 1; done
echo "round in flight: $(cat $GOPI_HOME/.gopi/worker/worker-state-* 2>/dev/null | head -c 220)"

say "RESTART with the branch binary (drains the master worker, spawns the new one)"
timeout 10m $B worker --restart --wait 10m > $R/t4-A.txt 2>&1; echo "A exit=$?" >> $R/t4-A.txt
cat $R/t4-A.txt

say "VERDICT: exactly ONE daemon, running the CURRENT (branch) binary"
$B worker --status 2>&1 | head -6
echo "state files: $(ls $GOPI_HOME/.gopi/worker/ | grep -c worker-state-)"
echo "old binary pid (should be gone): $(cat $R/home4/.gopi/worker/worker-state-*.json 2>/dev/null | grep -o '\"pid\":[0-9]*' | head -3)"

say "task drain-long-7 must be completed (NOT failed by the restart)"
$B task show drain-long-7 2>&1 | grep -E "^(status|session_id|result)"
echo SCRIPT_EXIT=0
