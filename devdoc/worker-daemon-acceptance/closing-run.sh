#!/bin/sh
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# Epic 5/5 closing acceptance on the real binary. Isolated GOPI_HOME; real worker
# pool untouched (host-wide --status proves it at the end).
set -u
B="$1"; NH="$2"
run(){ echo "\$ $*"; "$@"; echo "exit=$?"; echo; }
echo "===== run started $(date -Is) host=$(hostname) ====="
echo "### scratch device ids (Rule 22): dev(scratch)=$(stat -c %d "$NH") dev(/)=$(stat -c %d /)"
echo "### binary: $B"
"$B" --version; echo
echo "### git HEAD of the acceptance build"; git -C /home/user/work/GoPi rev-parse HEAD; echo
echo "### real worker pool BEFORE (must be unchanged at the end)"
pgrep -a gopi-worker; echo

echo "### (a) zero-worker honesty (clean seam GOPI_PROC_ROOT -> empty dir)"
mkdir -p "$NH/emptyproc"
echo "\$ GOPI_HOME=$NH GOPI_PROC_ROOT=$NH/emptyproc $B worker --status"
GOPI_HOME=$NH GOPI_PROC_ROOT=$NH/emptyproc "$B" worker --status; echo "exit=$?"; echo

echo "### (c) --daemon refused from an agent session (no override)"
GOPI_HOME=$NH "$B" worker --daemon; echo "exit=$?"; echo

echo "### (b) detach --start, then one honest --status line per live worker"
export GOPI_HOME=$NH GOPI_ALLOW_AGENT_SPAWN=1 GOPI_WORKER_IDLE_EXITS=0 GOPI_WORKER_IDLE_SEC=5
run "$B" worker --start
sleep 2
run "$B" worker --status
echo "### daemon identity (PPID 1 / own SID / own state record)"
DPID=$(ls "$NH/.gopi/worker/" 2>/dev/null | sed -n 's/^worker-state-\([0-9]*\)$/\1/p' | head -1)
echo "state record: $(ls "$NH/.gopi/worker/" | tr '\n' ' ')"
ps -o pid,ppid,sid,pgid,etime,stat,cmd -p "$DPID"; echo

echo "### (e) --daemon twice = refused, naming the holder"
GOPI_HOME=$NH GOPI_ALLOW_AGENT_SPAWN=1 "$B" worker --daemon; echo "exit=$?"; echo

echo "### (e) --restart = drain the old daemon THEN spawn a fresh one"
run "$B" worker --restart
sleep 2
DPID2=$(ls "$NH/.gopi/worker/" 2>/dev/null | sed -n 's/^worker-state-\([0-9]*\)$/\1/p' | head -1)
echo "old daemon pid $DPID -> new daemon pid $DPID2"
ps -o pid,ppid,sid,pgid,etime,stat,cmd -p "$DPID2"; echo
run "$B" worker --status

echo "### (d) --stop, graceful at the claim boundary + idempotent repeat"
run "$B" worker --stop
sleep 3
run "$B" worker --stop
echo "### daemon log tail (drain consumed at the claim boundary)"
tail -6 "$NH/.gopi/logs/worker.log"; echo

echo "### isolation proof: real worker pool AFTER (must equal BEFORE)"
pgrep -a gopi-worker; echo
echo "### operator drain/TERM request files (must be empty)"
ls ~/.gopi/worker/*.req 2>/dev/null; ls ~/.gopi/worker/ | grep -Ei 'drain|term|force' || echo "(none)"; echo
echo "===== run finished $(date -Is) ====="
