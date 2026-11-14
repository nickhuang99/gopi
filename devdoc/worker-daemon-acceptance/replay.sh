# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
set -u
B="$1"; NH="$2"; P="$3"
export GODEBUG=0
run(){ echo "\$ $*"; "$@"; echo "exit=$?"; echo; }
{
echo "### binary: $B"; $B --version; echo
echo "### git HEAD of the acceptance build"; git -C "$P" rev-parse HEAD; echo
echo "### (a) zero-worker honesty (clean seam GOPI_PROC_ROOT -> empty dir)"
mkdir -p "$NH/emptyproc"
echo "\$ GOPI_HOME=$NH GOPI_PROC_ROOT=$NH/emptyproc $B worker --status"
GOPI_HOME=$NH GOPI_PROC_ROOT=$NH/emptyproc "$B" worker --status; echo "exit=$?"; echo
echo "### (b/e) --start, fresh --status, --restart, --stop  (isolated profile)"
export GOPI_HOME=$NH GOPI_ALLOW_AGENT_SPAWN=1 GOPI_WORKER_IDLE_EXITS=0 GOPI_WORKER_IDLE_SEC=5
run "$B" worker --start
sleep 2
run "$B" worker --status
echo "### daemon identity (PPID 1 / own SID)"
DPID=$(ls "$NH/.gopi/worker/" 2>/dev/null | sed -n 's/^worker-state-\([0-9]*\)$/\1/p' | head -1)
ps -o pid,ppid,sid,etime,stat,cmd -p "$DPID"; echo
run "$B" worker --restart
sleep 2
DPID2=$(ls "$NH/.gopi/worker/" 2>/dev/null | sed -n 's/^worker-state-\([0-9]*\)$/\1/p' | head -1)
ps -o pid,ppid,sid,etime,stat,cmd -p "$DPID2"; echo
run "$B" worker --stop
run "$B" worker --stop
echo "### daemon log tail"
tail -4 "$NH/.gopi/logs/worker.log"
} 2>&1
