#!/usr/bin/env bash
# M4.18 failure-semantics E2E (same-host two-node sim, #1135 recipe).
# A claims a task; B's row for the same id is in_progress (state conflict) →
# B's --claim-remote handler REFUSES the newer live mirror (a locally-claimed
# row is never clobbered) → A's ownership gate yields: local row
# cancel + sync-yielded → A's TaskNextFiltered never re-claims (zero repeat).
set -u
# Scratch root: the shared workspace scratch home (NEVER /tmp — the / filesystem
# is where the 2026-09-11 exhaustion happened). The helper asserts it off `/`
# and registers the directory for trap cleanup. See scripts/lib/tmp-ban.sh.
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/tmp-ban.sh
. "$HERE/lib/tmp-ban.sh"
ROOT="$(gopi_scratch_mkdir m418-yield)" || { echo "FATAL: scratch root unavailable" >&2; exit 2; }
trap gopi_scratch_cleanup EXIT INT TERM
if gopi_dry_run_requested "$@"; then
    gopi_dry_run_report e2e-m418-yield "$ROOT" \
        "two-node yield sim: seed task, A claims + B mirrors in_progress, export/import, A yields (zero repeat)" \
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
qa() { sqlite3 "$GOPI_A/.gopi/memory.db" "$1"; }
qb() { sqlite3 "$GOPI_B/.gopi/memory.db" "$1"; }
taskid() { sqlite3 "$1/.gopi/memory.db" "SELECT task_id FROM tasks LIMIT 1"; }

NOW=$(date +%s)
# --- Seed: A submits; B receives the pending row through the file channel.
A task submit "m418 yield probe" --payload p >/dev/null 2>&1
TID=$(taskid "$GOPI_A")
[ -n "${TID:-}" ] && ok "seed submit → $TID" || { bad "seed submit"; exit 1; }
A memsync --export-tasks --out "$ROOT/seed.json" >/dev/null 2>&1
B task list >/dev/null 2>&1   # bootstrap B's DB schema
B memsync --import-tasks "$ROOT/seed.json" 2>&1 | grep -q "queued=1" \
  && ok "B imported pending row" || bad "B import"

# --- B is the real owner: its row goes in_progress (locally claimed).
qb "UPDATE tasks SET status='in_progress', claimed_by='node-b', claimed_at=$NOW, updated_at=$NOW"
[ "$(qb "SELECT status FROM tasks WHERE task_id='$TID'")" = "in_progress" ] \
  && ok "B row in_progress (owner)" || bad "B owner status"

# --- A claims the same task locally.
qa "UPDATE tasks SET status='in_progress', claimed_by='node-a', claimed_at=$NOW, updated_at=$NOW"
[ "$(qa "SELECT status FROM tasks WHERE task_id='$TID'")" = "in_progress" ] \
  && ok "A claimed (in_progress)" || bad "A claim status"

# --- A's claim broadcast reaches B: a newer live mirror onto a LOCALLY
# claimed row must be refused (live-execution guard, syncDeltaApplyNewer).
LN=$((NOW + 5))
printf '{"version":2,"node":"node-a","exported_at":%s,"generator":"e2e-claim","rows":[{"task_id":"%s","summary":"m418 yield probe","payload":"p","status":"in_progress","claimed_by":"node-a","updated_at":%s,"source":"llm"}]}' \
  "$LN" "$TID" "$LN" > "$ROOT/claim.json"
RES=$(B task sync --claim-remote < "$ROOT/claim.json" 2>&1)
echo "$RES" | grep -q '"accepted":false' \
  && ok "B refused the claim (live-execution guard)" || { echo "raw: $RES"; bad "B should refuse"; }
[ "$(qb "SELECT claimed_by FROM tasks WHERE task_id='$TID'")" = "node-b" ] \
  && ok "B's claim untouched (owner keeps the row)" || bad "B claim clobbered"

# --- The refusal verdict reaches A's ownership gate → YieldClaimedTask
# (the exact function runTaskRun's abort path calls; the probe drives it
# against A's DB through the env-var bridge).
GOPI_HOME="$GOPI_A" GOPI_M418_YIELD_ID="$TID" \
  go test ./internal/tools/ -run TestM418YieldProbe -count=1 >/dev/null 2>&1 \
  && ok "A gate wrote the yield (YieldClaimedTask)" || bad "yield write"

ST_A=$(qa "SELECT status FROM tasks WHERE task_id='$TID'")
TG_A=$(qa "SELECT tags FROM tasks WHERE task_id='$TID'")
[ "$ST_A" = "cancelled" ] && ok "A row cancelled" || bad "A row status=$ST_A"
echo "$TG_A" | grep -q "sync-yielded" && ok "A tags carry sync-yielded" || bad "A tags=$TG_A"

# --- Zero repeat work: A's dispatcher must not re-claim the yielded row.
NXT=$(GOPI_HOME="$GOPI_A" ./bin/gopi task next --as probe 2>&1 || true)
echo "$NXT" | grep -q "$TID" \
  && bad "A re-claimed the yielded row (repeat work!)" \
  || ok "A TaskNext does not re-claim (zero repeat)"
# B keeps its own state and runs it.
[ "$(qb "SELECT status FROM tasks WHERE task_id='$TID'")" = "in_progress" ] \
  && ok "B row untouched (its own in_progress)" || bad "B row regressed"

echo "-----"
echo "RESULT: PASS=$PASS FAIL=$FAIL  (task=$TID)"
[ "$FAIL" = "0" ]
