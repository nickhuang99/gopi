#!/usr/bin/env bash
# backfill-task-model.sh — mechanical backfill of the tasks ledger's model
# column for rows with model='' that CAN be resolved from their child
# session JSONL's session_start event (task-submit-model epic ③).
#
# Sources, in order of authority (mirrors gopi's own taskModelFromSession):
#   1. tasks.session_id → ~/.gopi/sessions/<session_id>.jsonl (first line,
#      "model":"...") — the authoritative source for any row with a session.
#   2. tasks.task_id == a session id (auto-registered rows; also legacy
#      pre-session_id rows where the task_id IS the session id).
#
# Safety rails (the #1081 recipe):
#   - DRY-RUN by default: prints the exact UPDATEs it would run.
#   - --apply commits, but ONLY when a backup memory.db.bak-task-backfill
#     exists next to the DB (refuses otherwise).
#   - One transaction; only rows with model='' are touched (a submit-time
#     --model or auto-row creation stamp is never overwritten).
#   - Unresolvable rows are LEFT empty — "(unknown)" honestly reports
#     missing data (epic ④: no display change, fix the data instead).
set -euo pipefail

APPLY=0
DB="${GOPI_MEMORY_DB:-$HOME/.gopi/memory.db}"
for arg in "$@"; do
  case "$arg" in
    --apply) APPLY=1 ;;
    --db) : ;; # consumed with value below
    --db=*) DB="${arg#--db=}" ;;
  esac
done
# paired form: --db PATH
if [ "${1:-}" = "--db" ] && [ -n "${2:-}" ]; then DB="$2"; fi

if [ ! -f "$DB" ]; then echo "no such DB: $DB" >&2; exit 1; fi
BACKUP="$DB.bak-task-backfill"
if [ "$APPLY" = "1" ] && [ ! -f "$BACKUP" ]; then
  echo "REFUSING --apply: no backup at $BACKUP (cp '$DB' '$BACKUP' first)" >&2
  exit 1
fi

SESSDIR="$HOME/.gopi/sessions"

# Collect (task_id, session_id) candidates: rows with empty model that have
# ANY session pointer (session_id column, or the task_id itself as a legacy
# session id). Single clean column output — sqlite3 CLI has no chr() (#1081).
# NOTE: the dot-commands and the SQL go as SEPARATE argv entries — a
# multiline single argument with dot-commands + SQL made sqlite3 3.37 return
# nothing (the .separator line swallowed the rest; two-arg form is reliable).
QUERY="SELECT task_id || '|' || COALESCE(session_id, '')
FROM tasks
WHERE (model IS NULL OR model = '')
  AND (COALESCE(session_id, '') != '' OR task_id LIKE '%-%')
ORDER BY id;"

model_for_session() {
  local sid="$1" f
  [ -n "$sid" ] || return 0
  f="$SESSDIR/$sid.jsonl"
  [ -f "$f" ] || return 0
  head -1 "$f" | sed -n 's/.*"model":"\([^"]*\)".*/\1/p'
}

declare -a UPDATES=()
RESOLVED=0; UNRESOLVED=0
while IFS= read -r line; do
  [ -n "$line" ] || continue
  tid="${line%%|*}"; sid="${line#*|}"
  m="$(model_for_session "$sid")"
  if [ -z "$m" ]; then m="$(model_for_session "$tid")"; fi
  if [ -n "$m" ]; then
    RESOLVED=$((RESOLVED+1))
    UPDATES+=("$tid"$'\x1f'"$m")
    echo "would backfill: $tid -> $m"
  else
    UNRESOLVED=$((UNRESOLVED+1))
    echo "unresolvable (left empty): $tid (session $sid)"
  fi
done < <(sqlite3 "$DB" ".mode list" "$QUERY")

echo "----"
echo "resolved=$RESOLVED unresolved=$UNRESOLVED db=$DB mode=$([ "$APPLY" = 1 ] && echo APPLY || echo DRY-RUN)"

if [ "$APPLY" != "1" ]; then
  echo "dry run — pass --apply (after creating the backup) to commit"
  exit 0
fi

[ "${#UPDATES[@]}" -gt 0 ] || { echo "nothing to apply"; exit 0; }

# One transaction; the model='' guard re-checked per row (never overwrite).
{
  echo "BEGIN;"
  for u in "${UPDATES[@]}"; do
    tid="${u%%$'\x1f'*}"; m="${u#*$'\x1f'}"
    printf "UPDATE tasks SET model = '%s', updated_at = strftime('%%s','now') WHERE task_id = '%s' AND (model IS NULL OR model = '');\n" \
      "${m//\'/\'\'}" "${tid//\'/\'\'}"
  done
  echo "COMMIT;"
  echo "PRAGMA integrity_check;"
} | sqlite3 "$DB"

echo "applied ${#UPDATES[@]} backfill(s) to $DB"
