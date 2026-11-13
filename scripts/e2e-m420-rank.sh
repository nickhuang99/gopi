#!/usr/bin/env bash
# M4.20-E: bilateral rank-convergence sim (same-host two-node sim, #1135
# recipe — file-channel export/import, real gopi binary both sides).
# Node A walks a task to completed; node B holds the same row mirrored as
# in_progress. After the delta exchange BOTH sides converge to the terminal
# verdict (rank 3) — no regression, no conflict residue.
set -u
# Scratch root under the shared workspace scratch home (never /tmp).
# See scripts/lib/tmp-ban.sh.
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/tmp-ban.sh
. "$HERE/lib/tmp-ban.sh"
ROOT="$(gopi_scratch_mkdir m420-rank)" || { echo "FATAL: scratch root unavailable" >&2; exit 2; }
trap gopi_scratch_cleanup EXIT INT TERM
if gopi_dry_run_requested "$@"; then
    gopi_dry_run_report e2e-m420-rank "$ROOT" \
        "bilateral rank-convergence sim: seed row, both nodes mirror it, delta exchange converges both to terminal verdict" \
        sqlite3
    exit $?
fi
GOPI_A="$ROOT/a" GOPI_B="$ROOT/b"
mkdir -p "$GOPI_A" "$GOPI_B"
PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); echo "PASS: $1"; }
bad() { FAIL=$((FAIL+1)); echo "FAIL: $1"; }
A() { GOPI_HOME="$GOPI_A" ./bin/gopi "$@"; }
B() { GOPI_HOME="$GOPI_B" GOPI_SYNC_NODE="node-b" ./bin/gopi "$@"; }
qa() { sqlite3 -cmd ".timeout 5000" "$GOPI_A/.gopi/memory.db" "$1"; }
qb() { sqlite3 -cmd ".timeout 5000" "$GOPI_B/.gopi/memory.db" "$1"; }

# --- Seed: A submits; B receives the pending row through the file channel.
A task submit "m420 rank convergence probe" --payload "convergence payload" >/dev/null 2>&1
TID=$(qa "SELECT task_id FROM tasks WHERE summary LIKE 'm420 rank convergence probe%'")
[ -n "${TID:-}" ] && ok "seed submit → $TID" || { bad "seed submit"; exit 1; }
A memsync --export-tasks --out "$ROOT/seed.json" || { bad "A seed export"; exit 1; }
B memsync --import-tasks "$ROOT/seed.json" | grep -q "queued=1" \
  && ok "B imported pending row" || bad "B import"

# --- A claims + completes its copy (terminal rank 3, anchor advances).
NOW=$(date +%s)
A task update "$TID" --status in_progress >/dev/null 2>&1
sleep 1
A task update "$TID" --status completed >/dev/null 2>&1
[ "$(qa "SELECT status FROM tasks WHERE task_id='$TID'")" = "completed" ] \
  && ok "A row completed (terminal rank 3)" || bad "A terminal"

# --- B mirrors the row as in_progress (a live claim at an OLDER anchor).
qb "UPDATE tasks SET status='in_progress', claimed_by='node-b:1', claimed_at=$NOW, updated_at=$((NOW-10)) WHERE task_id='$TID'"
[ "$(qb "SELECT status FROM tasks WHERE task_id='$TID'")" = "in_progress" ] \
  && ok "B row in_progress (older-anchor mirror)" || bad "B mirror status"

# --- Exchange: A exports its delta; B imports (the rank ladder applies:
# in_progress(2) → completed(3) is FORWARD — B converges).
A memsync --export-tasks --out "$ROOT/delta.json" >/dev/null 2>&1
B memsync --import-tasks "$ROOT/delta.json" >/dev/null 2>&1
SB=$(qb "SELECT status FROM tasks WHERE task_id='$TID'")
[ "$SB" = "completed" ] \
  && ok "B converged to the terminal verdict (rank forward, no regression)" \
  || bad "B did not converge (got $SB)"

# --- Reverse: B's (now converged) row exports; A absorbs the echo as an
# idempotent no-op — third pass silent.
B memsync --export-tasks --out "$ROOT/back.json" >/dev/null 2>&1
BEFORE=$(qa "SELECT status||updated_at FROM tasks WHERE task_id='$TID'")
A memsync --import-tasks "$ROOT/back.json" >/dev/null 2>&1
AFTER=$(qa "SELECT status||updated_at FROM tasks WHERE task_id='$TID'")
[ "$BEFORE" = "$AFTER" ] \
  && ok "A echo no-op (idempotent, nothing rewritten)" || bad "A echo disturbed"

# --- Verdicts.
[ $FAIL -eq 0 ] && { echo "=== bilateral rank sim: ALL PASS ($PASS assertions) ==="; exit 0; }
echo "=== bilateral rank sim: $FAIL FAILURES / $PASS passes ==="; exit 1
