#!/usr/bin/env bash
# migrate-sessions.sh — session-dir merge (tracking-safe migration of flat
# session logs to the 3-layout sessions tree).
#
# Epic: ~/.gopi/sessions historically held 1230 flat <uuid>.jsonl files
# alongside the newer per-session directories. Migration rules:
#   - TRUE task session   (tasks.session_id=<uuid> and some task_id!=<uuid>)   → sessions/<task_id>/<uuid>.jsonl
#   - OLD AUTO mirror row (task_id==session_id==<uuid>, skeleton runs)         → sessions/<uuid>/<uuid>.jsonl
#   - BARE / ORPHAN       (no tasks row matches session_id=<uuid>)             → sessions/<uuid>/session.jsonl
# plus a metadata.json in every destination directory.
#
# Hard constraints honored:
#   * idempotent — re-running after success is a no-op; a partially-migrated
#     tree completes (source files still at root are moved; already-moved
#     files are never touched again)
#   * mv semantics — same-fs rename, EXDEV fallback copy+rm (never clobbers)
#   * NO overwrite of an existing destination (skip + report)
#   * tasks table NEVER modified (read-only classification)
#   * NO file deletion
#   * raw logs are NOT moved: raw resolution is location-independent
#     (ResolveRawLogForSession uses the session_start-recorded absolute
#     rawLogPath first, timestamp pairing in raw/ second) and gopi raw's
#     recorded paths are absolute — moving raw logs would BREAK the reader.
#     Only sessions/*.jsonl are migrated.
#   * DB classification source: memory.db tasks table (session_id column)
#
# Usage:
#   migrate-sessions.sh [--dry-run|--execute] [--home DIR] [--db PATH]
#     --home DIR   gopi data root (default: ~/.gopi)
#     --db PATH    tasks DB (default: <home>/memory.db)
#     --dry-run    classify + print the plan, move nothing  (DEFAULT)
#     --execute    perform the moves (writes .migration-manifest.json)
#
# Exit: 0 on success; 2 on usage/classification failure. Dry-run never moves.

set -u

# Scratch root (the DB snapshot) lives under the shared workspace scratch home,
# never /tmp — see scripts/lib/tmp-ban.sh (the 2026-09-11 / exhaustion).
# shellcheck source=lib/tmp-ban.sh
. "$(cd "$(dirname "$0")" && pwd)/lib/tmp-ban.sh"

# ---- constants (Rule 1/2: named, one place) ---------------------------------
DB_FILE_DEFAULT="memory.db"
SESSIONS_DIR_NAME="sessions"
MANIFEST_NAME=".migration-manifest.json"
CLASS_AUTO="auto"      # task_id==session_id  (skeleton mirror rows)
CLASS_TRUE="true"      # task_id!=session_id  (real dispatched task)
CLASS_ORPHAN="orphan"  # no tasks row session_id match
META_BASENAME="metadata.json"
BARE_FILE_NAME="session.jsonl"

# ---- arg parsing ------------------------------------------------------------
MODE="dry-run"
HOME_DIR="${HOME}/.gopi"
DB_PATH=""
while [ $# -gt 0 ]; do
    case "$1" in
        --dry-run) MODE="dry-run" ;;
        --execute) MODE="execute" ;;
        --home) HOME_DIR="$2"; shift ;;
        --db) DB_PATH="$2"; shift ;;
        --help|-h)
            sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'
            exit 0 ;;
        *) echo "unknown arg: $1" >&2; exit 2 ;;
    esac
    shift
done
SESS_DIR="${HOME_DIR}/${SESSIONS_DIR_NAME}"
[ -n "$DB_PATH" ] || DB_PATH="${HOME_DIR}/${DB_FILE_DEFAULT}"

if [ ! -d "$SESS_DIR" ]; then echo "no sessions dir: $SESS_DIR" >&2; exit 2; fi
if [ ! -r "$DB_PATH" ]; then echo "no tasks DB: $DB_PATH" >&2; exit 2; fi
if ! command -v sqlite3 >/dev/null 2>&1; then echo "sqlite3 required" >&2; exit 2; fi
if ! command -v jq >/dev/null 2>&1; then echo "jq required" >&2; exit 2; fi

# Classification reads ONE consistent DB snapshot (the tasks table is a live
# ledger — a row written between plan and move would silently reclassify a
# file). .backup is a consistent WAL-safe copy; classify() and row_meta()
# query $DB_SNAP, never the live DB.
DB_SNAP_DIR="$(gopi_scratch_mkdir migrate-db)" || { echo "cannot create a scratch dir" >&2; exit 2; }
trap 'gopi_scratch_cleanup' EXIT INT TERM
DB_SNAP="$DB_SNAP_DIR/gopi-migrate-db"
if ! sqlite3 "$DB_PATH" ".backup '$DB_SNAP'" 2>/dev/null; then
    echo "cannot snapshot tasks DB $DB_PATH" >&2; exit 2
fi
DB_PATH="$DB_SNAP"

# ---- helpers ----------------------------------------------------------------
err() { echo "ERROR: $*" >&2; }

# uuid_ok UUID → 0 when it is a canonical uuid v4 shape (flat file anchor).
uuid_ok() { [[ "$1" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]]; }

# classify UUID -> prints "<class>|<task_id>" ; task_id empty for orphan/auto
#  TRUE:  any tasks row session_id=<uuid> whose task_id != <uuid>; among all
#         such rows the EARLIEST (min created_at, tie lexicographic task_id)
#         is the placement task_id.
#  AUTO:  rows exist but EVERY row has task_id==<uuid>.
#  ORPHAN:no row has session_id=<uuid>.
classify() {
    local uuid="$1"
    # shellcheck disable=SC2016  # SQL single-quote safety: uuid is [0-9a-f-]
    local rows
    rows=$(sqlite3 -separator $'\x1f' "$DB_PATH" \
        "SELECT task_id, created_at FROM tasks WHERE session_id='${uuid}' ORDER BY created_at ASC, task_id ASC;" 2>/dev/null)
    if [ -z "$rows" ]; then echo "${CLASS_ORPHAN}|"; return; fi
    local task_id nonauto=""
    while IFS= read -r line; do
        [ -z "$line" ] && continue
        task_id="${line%%$'\x1f'*}"
        if [ "$task_id" != "$uuid" ]; then nonauto="$task_id"; break; fi
    done <<< "$rows"
    if [ -n "$nonauto" ]; then echo "${CLASS_TRUE}|${nonauto}"; else echo "${CLASS_AUTO}|${uuid}"; fi
}

# anchor_id FILE -> display session id used for classification: the basename
# for uuid-shaped flat files, else the session_start sessionId (the tracking
# anchor tasks/session readers resolve on). Empty when neither is usable.
anchor_id() {
    local f="$1" b uuid
    b=$(basename "$f"); uuid="${b%.jsonl}"
    if uuid_ok "$uuid"; then echo "$uuid"; return 0; fi
    head -1 "$f" | jq -r 'select(.type=="session_start") | .sessionId // empty' 2>/dev/null
}

# row_meta UUID -> "<model>|<created_unix>" from the FIRST matching row's
# model / created_at (prefer tasks row; empty model falls back to
# session_start model inside write_meta). '|' is the separator (task_ids and
# models never contain it).
row_meta() {
    local uuid="$1"
    sqlite3 -separator '|' "$DB_PATH" \
        "SELECT COALESCE(model,''), COALESCE(created_at,0) FROM tasks WHERE session_id='${uuid}' ORDER BY created_at ASC, task_id ASC LIMIT 1;" 2>/dev/null
}

# start_meta FILE -> "<model>|<created_iso>" from session_start.
start_meta() {
    local f="$1"
    head -1 "$f" | jq -r '[.model // "", (.ts // "")] | @tsv' 2>/dev/null | awk -F'\t' '{print $1"|"$2}'
}

# write_meta DIR UUID TASK_ID  — merge into DIR/metadata.json:
# task_id/session_id/model/created (+ raw_log_path / raw_log_basename when the
# session_start records one). Never clobbers an existing metadata.json —
# merges missing keys only (submit skeleton fields survive, Rule: metadata
# from tasks row or session_start; never overwrite a sibling's richer meta).
write_meta() {
    local dir="$1" uuid="$2" task_id="$3"   # task_id "" = orphan (no task row)
    local meta_file="${dir}/${META_BASENAME}"
    local model="" created=""
    # Row lookup only when a task row exists (auto: task_id==uuid; true:
    # task_id!=uuid). Orphans have no row → session_start/file mtime only.
    local rows=""
    if [ -n "$task_id" ]; then
        rows=$(sqlite3 -separator '|' "$DB_PATH" \
            "SELECT COALESCE(model,''), COALESCE(created_at,0) FROM tasks WHERE session_id='${uuid}' ORDER BY created_at ASC, task_id ASC LIMIT 1;" 2>/dev/null)
        local rmodel rcreated
        IFS='|' read -r rmodel rcreated <<< "$rows"
        [ -n "$rmodel" ] && model="$rmodel"
        if [ -n "$rcreated" ] && [ "$rcreated" != "0" ]; then
            created=$(date -u -d "@${rcreated}" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null)
        fi
    fi
    # fall back to session_start header when the row lacked model/created
    local src="${dir}/${uuid}.jsonl"; [ -f "$src" ] || src="${dir}/${BARE_FILE_NAME}"
    if { [ -z "$model" ] || [ -z "$created" ]; } && [ -f "$src" ]; then
        local smodel screated
        IFS='|' read -r smodel screated <<< "$(start_meta "$src")"
        [ -z "$model" ] && model="$smodel"
        [ -z "$created" ] && created="$screated"
    fi
    # session_start created falls back to the JSONL file mtime
    if [ -z "$created" ] && [ -f "$src" ]; then
        created=$(date -u -r "$src" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null)
    fi
    # merge into any existing metadata (submit skeleton fields survive);
    # absent keys written, present keys never clobbered; empties dropped.
    if [ -f "$meta_file" ]; then
        jq --arg task_id "$task_id" --arg session_id "$uuid" \
           --arg model "$model" --arg created "$created" \
            '. as $m |
             (($m | has("task_id")) as $has_tid |
              (if $has_tid then $m else $m + (if $task_id != "" then {task_id: $task_id} else {} end) end)) |
             .session_id = (.session_id // $session_id) |
             .model = (.model // $model) |
             .created = (.created // $created) |
             with_entries(select(.value != "" and .value != null))' \
            "$meta_file" > "${meta_file}.tmp" && mv "${meta_file}.tmp" "$meta_file"
    else
        jq -n --arg task_id "$task_id" --arg session_id "$uuid" \
           --arg model "$model" --arg created "$created" \
            '{session_id: $session_id} +
             (if $task_id != "" then {task_id: $task_id} else {} end) +
             (if $model != "" then {model: $model} else {} end) +
             (if $created != "" then {created: $created} else {} end)' \
            > "$meta_file"
    fi
}

# move_no_overwrite SRC DST  — same-fs mv; EXDEV fallback cp+rm; destination
# exists → skip (idempotent). Returns 0 moved, 1 skipped, 2 failed.
move_no_overwrite() {
    local src="$1" dst="$2"
    [ -f "$dst" ] && { echo "  SKIP  $dst exists (idempotent)"; return 1; }
    if ! mkdir -p "$(dirname "$dst")"; then return 2; fi
    if mv "$src" "$dst" 2>/dev/null; then return 0; fi
    # cross-filesystem fallback: copy then remove (never delete on copy fail)
    if cp -p "$src" "$dst" 2>/dev/null && rm -f "$src"; then return 0; fi
    rm -f "$dst" 2>/dev/null
    return 2
}

# ---- main -------------------------------------------------------------------
MANIFEST="${SESS_DIR}/${MANIFEST_NAME}"
: > "$MANIFEST"   # rebuilt each run (sources still at root are the work set)

flat=()
for f in "$SESS_DIR"/*.jsonl; do
    [ -f "$f" ] || continue
    aid=$(anchor_id "$f")
    [ -n "$aid" ] && flat+=("$f|$aid")
done

total=${#flat[@]}
[ "$total" -eq 0 ] && { echo "no flat session files at root — nothing to do (idempotent)"; echo "$(date -Is) no-op" >> "$MANIFEST"; exit 0; }

echo "classifying $total flat session files against tasks table…"
declare -i c_true=0 c_auto=0 c_orphan=0
for entry in "${flat[@]}"; do
    f="${entry%%|*}"; b=$(basename "$f"); uuid="${entry#*|}"
    cls=$(classify "$uuid"); kind="${cls%%|*}"; tid="${cls#*|}"
    case "$kind" in
        "$CLASS_TRUE")   c_true=$((c_true+1)); dst_dir="$SESS_DIR/$tid" ;;
        "$CLASS_AUTO")   c_auto=$((c_auto+1)); dst_dir="$SESS_DIR/$uuid" ;;
        "$CLASS_ORPHAN") c_orphan=$((c_orphan+1)); dst_dir="$SESS_DIR/$uuid" ;;
        *) err "classify($uuid) → bad class '$kind'"; exit 2 ;;
    esac
    if [ "$kind" = "$CLASS_ORPHAN" ]; then dst="${dst_dir}/${BARE_FILE_NAME}"; else dst="${dst_dir}/${b}"; fi
    echo "  [$kind] $b → ${dst#$SESS_DIR/}"
done
echo "plan: $c_true true, $c_auto auto, $c_orphan orphan (total $total)"

if [ "$MODE" = "dry-run" ]; then
    echo "DRY-RUN — no files moved. Re-run with --execute to migrate."
    exit 0
fi

echo "executing…"
declare -i moved=0 skipped=0 failed=0
for entry in "${flat[@]}"; do
    f="${entry%%|*}"; b=$(basename "$f"); uuid="${entry#*|}"
    # re-classify at move time: a partially-migrated tree may already have
    # consumed some sources — only act on files still present at root.
    [ -f "$f" ] || { skipped=$((skipped+1)); continue; }
    cls=$(classify "$uuid"); kind="${cls%%|*}"; tid="${cls#*|}"
    case "$kind" in
        "$CLASS_TRUE")   dst_dir="$SESS_DIR/$tid" ;;
        "$CLASS_AUTO")   dst_dir="$SESS_DIR/$uuid" ;;
        "$CLASS_ORPHAN") dst_dir="$SESS_DIR/$uuid" ;;
        *) err "classify($uuid) → bad class '$kind'"; failed=$((failed+1)); continue ;;
    esac
    if [ "$kind" = "$CLASS_ORPHAN" ]; then dst="${dst_dir}/${BARE_FILE_NAME}"; else dst="${dst_dir}/${b}"; fi
    move_no_overwrite "$f" "$dst"
    rc=$?
    if [ $rc -eq 0 ]; then
        write_meta "$dst_dir" "$uuid" "$tid"
        # manifest: one JSON line per move (recovery = reverse the mapping)
        printf '{"src":"%s","dst":"%s","class":"%s","ts":"%s"}\n' \
            "$f" "$dst" "$kind" "$(date -Is)" >> "$MANIFEST"
        moved=$((moved+1))
    elif [ $rc -eq 1 ]; then
        skipped=$((skipped+1))
    else
        err "move failed: $f → $dst"
        failed=$((failed+1))
    fi
done
echo "result: moved=$moved skipped=$skipped failed=$failed (manifest: $MANIFEST)"
[ "$failed" -eq 0 ] || exit 2
exit 0
