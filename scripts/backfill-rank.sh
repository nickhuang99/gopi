#!/usr/bin/env bash
# backfill-rank.sh — M4.23 A: legacy tasks rows conformed to the M4.20-B rank model.
#
# Idempotent (safe to re-run; second run is a no-op), dry-run by default,
# --apply commits in ONE transaction after a row-conservation + backup guard.
#
# Migration semantics (rank model: tasks.go taskStatusRank — pending 0 /
# claimed 1 / in_progress 2 / completed·failed·cancelled 3 / verified 4;
# reopened = legacy rank-0 rework marker):
#
#   ① reopened rows → reissue semantics (M4.20-C: reopened RETIRED):
#        old row → status=completed, tags+=reissued, deliverable note kept,
#        new row <old>-r1 status=pending parent=<old> tags+=reissue,bug,
#        summary="rework: <old summary> (reissue of <old>)".
#      P2.5 note: a verified/reopened pair that already made it to master
#      keeps BOTH rows terminal (old stays completed, no reissue row is
#      spawned when a r1 row ALREADY exists — re-run idempotency).
#   ② terminal-tier normalization: completed/failed/cancelled all sit on
#      rank 3 (peers — 后到覆盖) — statuses are kept as-is; the semantic
#      residue (why failed/cancelled) stays in tags, nothing is rewritten.
#      Orphan live rows (claimed/in_progress with an EMPTY claimed_by — the
#      pre-lock-model mirror residue) are reverted to pending: rank-0, fresh
#      claim by any worker; the old status stays in tags as claim-revert
#      provenance. Claimed/in_progress rows WITH a claim stamp are live
#      locks — never touched.
#   ③ clock-adjudication residue: rows whose updated_at < created_at
#      (equal-anchor conflict rows the legacy clock picked wrong) are
#      re-stamped updated_at = created_at so the rank model's equal-anchor
#      后到覆盖 compares real anchors. (Census on 2026-09-01: 0 rows —
#      the rule stays for idempotent completeness.)
#
# Row-conservation assertion: N_reopened_spawned == N_reopened (one reissue
# child per reopened row) and every other row is UPDATEd in place —
# total row count changes EXACTLY by +N_reopened. Zero rows lost.
set -euo pipefail

APPLY=0
DB="${GOPI_BACKFILL_DB:-$HOME/.gopi/memory.db}"
for a in "$@"; do
  case "$a" in
    --apply) APPLY=1 ;;
    --db=*) DB="${a#--db=}" ;;
    *) echo "usage: backfill-rank.sh [--apply] [--db=PATH]"; exit 2 ;;
  esac
done
[ -f "$DB" ] || { echo "no such db: $DB"; exit 1; }

BAK="$DB.bak-rank-backfill"
if [ "$APPLY" = 1 ] && [ ! -f "$BAK" ]; then
  cp "$DB" "$BAK"
  echo "backup: $BAK"
fi

SQL_CENSUS=$(cat <<'EOF'
SELECT 'reopened'          AS class, COUNT(*) AS n FROM tasks WHERE status='reopened'
UNION ALL SELECT 'live_orphan_claimed',    COUNT(*) FROM tasks WHERE status='claimed'    AND (claimed_by IS NULL OR claimed_by='')
UNION ALL SELECT 'live_orphan_inprogress', COUNT(*) FROM tasks WHERE status='in_progress' AND (claimed_by IS NULL OR claimed_by='')
UNION ALL SELECT 'live_locked',            COUNT(*) FROM tasks WHERE status IN ('claimed','in_progress') AND claimed_by IS NOT NULL AND claimed_by!=''
UNION ALL SELECT 'clock_ambiguous',        COUNT(*) FROM tasks WHERE status IN ('completed','failed','cancelled') AND updated_at < created_at
UNION ALL SELECT 'total',                  COUNT(*) FROM tasks;
EOF
)
echo "=== census ($DB) ==="
sqlite3 "$DB" "$SQL_CENSUS"

SQL_APPLY=$(cat <<'EOF'
BEGIN;

-- ① reopened → reissue (M4.20-C): old row terminal + one rework child each.
--    Idempotency: skip rows that already carry the reissued tag; skip spawn
--    when a r1 child already exists.
INSERT INTO tasks (task_id, summary, status, session_id, model,
                   parent_task_id, tags, payload, source, created_at, updated_at)
SELECT o.task_id || '-r1',
       'rework: ' || o.summary || ' (reissue of ' || o.task_id || ')',
       'pending', '', '', o.task_id,
       CASE WHEN o.tags='' THEN 'reissue,bug' ELSE o.tags || ',reissue,bug' END,
       o.payload, 'auto', o.updated_at, o.updated_at
FROM tasks o
WHERE o.status='reopened'
  AND o.tags NOT LIKE '%reissued%'
  AND NOT EXISTS (SELECT 1 FROM tasks c WHERE c.task_id = o.task_id || '-r1');

UPDATE tasks SET status='completed',
                 tags = CASE WHEN tags='' THEN 'reissued' ELSE tags || ',reissued' END
WHERE status='reopened'
  AND NOT EXISTS (SELECT 1 FROM tasks c WHERE c.task_id = task_id || '-r1'
                  AND c.status='pending')
  AND tags NOT LIKE '%reissued%';

-- ② live-orphan revert: claimed/in_progress with no claim stamp = mirror
--    residue → pending (rank 0). Status provenance kept in tags.
UPDATE tasks SET status='pending',
                 tags = CASE WHEN tags='' THEN 'claim-revert'
                             ELSE tags || ',claim-revert' END,
                 claimed_at = NULL
WHERE status IN ('claimed','in_progress')
  AND (claimed_by IS NULL OR claimed_by='');

-- ③ clock-adjudication residue: equal-anchor conflict rows re-stamped so
--    the rank model compares real anchors (updated_at < created_at is
--    impossible under the live writer — legacy clock artifact only).
UPDATE tasks SET updated_at = created_at
WHERE status IN ('completed','failed','cancelled') AND updated_at < created_at;

COMMIT;

-- row-conservation: reopened spawns exactly one child each; everything else
-- is in-place. assert: delta == reopened_count.
EOF
)

if [ "$APPLY" = 1 ]; then
  BEFORE=$(sqlite3 "$DB" "SELECT COUNT(*) FROM tasks;")
  REOPENED=$(sqlite3 "$DB" "SELECT COUNT(*) FROM tasks WHERE status='reopened';")
  ORPHANS=$(sqlite3 "$DB" "SELECT COUNT(*) FROM tasks WHERE status IN ('claimed','in_progress') AND (claimed_by IS NULL OR claimed_by='');")
  CLOCK=$(sqlite3 "$DB" "SELECT COUNT(*) FROM tasks WHERE status IN ('completed','failed','cancelled') AND updated_at < created_at;")
  sqlite3 "$DB" "$SQL_APPLY" >/dev/null
  AFTER=$(sqlite3 "$DB" "SELECT COUNT(*) FROM tasks;")
  EXPECTED=$((BEFORE + REOPENED))
  echo "=== apply ==="
  echo "before=$BEFORE reopened=$REOPENED orphans_reverted=$ORPHANS clock_restamped=$CLOCK"
  echo "after=$AFTER expected=$EXPECTED"
  if [ "$AFTER" -ne "$EXPECTED" ]; then
    echo "CONSERVATION FAIL: after=$AFTER expected=$EXPECTED — restoring backup"
    cp "$BAK" "$DB"
    exit 1
  fi
  INTEG=$(sqlite3 "$DB" "PRAGMA integrity_check;")
  [ "$INTEG" = "ok" ] || { echo "integrity_check: $INTEG — restoring backup"; cp "$BAK" "$DB"; exit 1; }
  echo "integrity_check: ok"
  echo "=== post-apply census ==="
  sqlite3 "$DB" "$SQL_CENSUS"
else
  echo "(dry-run — nothing written; add --apply to commit)"
fi
