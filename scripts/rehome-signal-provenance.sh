#!/usr/bin/env bash
# rehome-signal-provenance.sh — one-time migration of the RETIRED public
# signal-provenance sink (<gopi-dir>/logs/signal-provenance.log) into the
# SESSION DIR of every process whose record it holds.
#
# WHY (epic signalrec-session-attribution, 2026-09-14): the sink used to be ONE
# host-global file, which is exactly the artifact this epic removes — a record
# written precisely to explain why a run died was un-walkable from the run.
# internal/signalrec now appends to the owning process's own session dir
# (<gopi-dir>/sessions/<id>/signal-provenance.jsonl, beside session.jsonl,
# *.raw.log and task-report.md), and never writes the public path again. This
# script moves what the old code left behind, so the historical evidence is
# reachable from the id too.
#
# THE CLASSIFICATION IS PER LINE, FROM THE LINE'S OWN RECORDED FIELDS, never
# from the line's position, its ts, or a neighbouring line:
#
#   1. "session_dir"  — the destination the NEW writer recorded in the record
#                        itself. Authoritative when present.
#   2. "task_run"     — the ledger task id the record was executing; its
#                        session dir is <sessions>/<task_run>.
#   3. "session"      — the run/session uuid; its dir is <sessions>/<session>.
#
# Candidates are tried IN THAT ORDER and the destination must PROVE itself a
# session dir (a directory that exists and holds metadata.json or a *.jsonl) —
# the same proof internal/signalrec.isSessionDir demands, so a migrated record
# lands only where a fresh record would have landed. A directory that would
# have to be CREATED is NOT a destination: the migration never invents a
# session for an id whose container is gone (a wrong owner is worse than a
# stale line, and it would read as evidence).
#
# TIMESTAMPS ARE PRESERVED BY CONSTRUCTION: the record's "ts" is a field inside
# the JSON line, the line is copied BYTE-FOR-BYTE, and the destination is
# APPEND-ONLY — nothing is re-serialised, re-stamped or sorted.
#
# UNATTRIBUTABLE LINES ARE NEVER LOST SILENTLY, and they are never filed under
# a global/public name either (that would recreate the defect): they are
# printed IN FULL, counted, and — unless --discard-unattributable is passed —
# they KEEP the source file in place so the operator can decide. The source is
# removed only when every line reached a destination.
#
# NOT TOUCHED: every other file in the log area (merger.log, worker.log,
# scratch-gc-watchdog.log, glm-night-watch.log, nightwatch.log,
# night-progress.log). Those are PROCESS-SCOPED, not session-scoped, records —
# see devdoc/signalrec-session-attribution.md §C for the per-file argument.
#
# SAFETY: dry-run is the DEFAULT (nothing moves, the plan prints); --apply is
# explicit. Content is APPENDED, never truncated. A line's bytes reach the
# destination BEFORE the source is removed, so an interrupted run is resumable
# and a second --apply is a no-op (the source is gone).
#
# Usage:
#   rehome-signal-provenance.sh [--dry-run|--apply] [--home DIR] [--logs DIR]
#                               [--sessions DIR] [--discard-unattributable]
#     --home DIR       gopi data dir        (default: $GOPI_HOME/.gopi, else ~/.gopi)
#     --logs DIR       the log area holding the retired sink (default: <home>/logs)
#     --sessions DIR   the sessions tree     (default: <home>/sessions)
#     --discard-unattributable  remove the source even when some lines found no
#                               session dir (their text is still printed)
#     --dry-run        classify + print the plan, change nothing (DEFAULT)
#     --apply          perform the appends and the source removal
#
# Exit: 0 on success; 2 on usage error or an unusable destination.

set -euo pipefail

# ---- constants (Rule 1: every literal lives here, one place) ----------------
# The retired public sink's file name — the SAME string internal/signalrec
# spells as signalrec.LogFileName (it is no longer a write destination there,
# only this migration's input).
RETIRED_SINK_NAME='signal-provenance.log'
# The destination file inside a session dir — internal/signalrec.FileName.
SESSIONS_FILE_NAME='signal-provenance.jsonl'
# The session container's identity files (internal/signalrec's sessionMetadataName
# and its JSONL requirement): a directory must carry one of them to count.
METADATA_NAME='metadata.json'
JSONL_SUFFIX='.jsonl'
# The record fields this migration attributes by, in priority order.
FIELD_SESSION_DIR='"session_dir"'
FIELD_TASK_RUN='"task_run"'
FIELD_SESSION='"session"'
SESSIONS_LEAF='sessions'

MODE='dry-run'
DISCARD_UNATTRIBUTED=0
HOME_DIR="${GOPI_HOME:+${GOPI_HOME%/}/.gopi}"
HOME_DIR="${HOME_DIR:-${HOME}/.gopi}"
LOGS_DIR=''
SESSIONS_DIR=''

# ---- arg parsing -----------------------------------------------------------
while (($#)); do
  case "$1" in
    --dry-run) MODE='dry-run'; shift ;;
    --apply) MODE='apply'; shift ;;
    --discard-unattributable) DISCARD_UNATTRIBUTED=1; shift ;;
    --home) HOME_DIR="${2%/}"; shift 2 ;;
    --logs) LOGS_DIR="${2%/}"; shift 2 ;;
    --sessions) SESSIONS_DIR="${2%/}"; shift 2 ;;
    -h|--help) sed -n '2,/^set -euo pipefail$/p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "rehome-signal-provenance: unexpected argument '$1' (see --help)" >&2; exit 2 ;;
  esac
done

[[ -n "$LOGS_DIR" ]] || LOGS_DIR="$HOME_DIR/logs"
[[ -n "$SESSIONS_DIR" ]] || SESSIONS_DIR="$HOME_DIR/$SESSIONS_LEAF"
SOURCE="$LOGS_DIR/$RETIRED_SINK_NAME"

if [[ ! -f "$SOURCE" ]]; then
  echo "rehome-signal-provenance: no retired sink at $SOURCE (nothing to do)"
  exit 0
fi

# ---- counters (the report a human reads) -----------------------------------
n_lines=0 n_filed=0 n_unattributed=0 bytes_filed=0
declare -A seen_dirs=()
declare -A filed_from=()
unattributed_lines=()

# json_field <line> <"name"> — the value of a flat string field of a one-object
# JSON line, or empty. The records are FLAT (no nested objects), so a bounded
# match is exact here; a line that is not JSON yields empty (= unattributable),
# never a guess. Escapes are not needed: values are paths/ids/uuids.
json_field() {
  local line="$1" field="$2" rest
  rest="${line#*$field:}"
  [[ "$rest" == "$line" ]] && return 0
  rest="${rest#\"}"
  printf '%s' "${rest%%\"*}"
}

# is_session_dir <path> — the proof internal/signalrec.isSessionDir demands: a
# real directory carrying the container's own marker or a session JSONL.
is_session_dir() {
  local dir="$1"
  [[ -d "$dir" ]] || return 1
  [[ -f "$dir/$METADATA_NAME" ]] && return 0
  local f
  for f in "$dir"/*"$JSONL_SUFFIX"; do
    [[ -f "$f" ]] && return 0
  done
  return 1
}

# dest_for <line> — resolves the session dir of one record. Prints the dir, or
# nothing when no candidate proves itself.
dest_for() {
  local line="$1" dir
  for field in "$FIELD_SESSION_DIR" "$FIELD_TASK_RUN" "$FIELD_SESSION"; do
    local val
    val="$(json_field "$line" "$field")"
    [[ -n "$val" ]] || continue
    case "$field" in
      "$FIELD_SESSION_DIR") dir="$val" ;;
      *) dir="$SESSIONS_DIR/$val" ;;
    esac
    if is_session_dir "$dir"; then
      printf '%s' "$dir"
      return 0
    fi
  done
  return 0
}

# ---- the sweep -------------------------------------------------------------
echo "rehome-signal-provenance: mode=$MODE source=$SOURCE sessions=$SESSIONS_DIR"

# The LINE COUNT is read up front for the before/after report; the file is read
# once, line by line, so the source is never re-parsed per line.
total_lines=$(wc -l < "$SOURCE")
while IFS= read -r line || [[ -n "$line" ]]; do
  [[ -n "${line//[[:space:]]/}" ]] || continue
  n_lines=$((n_lines + 1))
  dir="$(dest_for "$line")"
  if [[ -z "$dir" ]]; then
    n_unattributed=$((n_unattributed + 1))
    unattributed_lines+=("$line")
    printf '  line %s: UNATTRIBUTABLE (no recorded session_dir/task_run/session whose dir exists)\n' "$n_lines" >&2
    continue
  fi
  dest="$dir/$SESSIONS_FILE_NAME"
  if [[ "$MODE" == 'dry-run' ]]; then
    printf '  line %s → %s\n' "$n_lines" "$dest"
  else
    # APPEND, one line at a time: the byte order of the source is preserved and
    # a destination that already holds records keeps every byte of them.
    printf '%s\n' "$line" >> "$dest" || {
      echo "rehome-signal-provenance: cannot append to $dest — aborting (a record whose home is unwritable must not be deleted from its only copy)" >&2
      exit 2
    }
  fi
  n_filed=$((n_filed + 1))
  bytes_filed=$((bytes_filed + ${#line} + 1))
  seen_dirs["$dir"]=1
  filed_from["$dir"]=$(( ${filed_from["$dir"]:-0} + 1 ))
done < "$SOURCE"

# ---- the source's fate -----------------------------------------------------
if (( n_lines == 0 )); then
  echo "rehome-signal-provenance: $SOURCE is empty — ${MODE} done (filed=0 unattributed=0)"
  [[ "$MODE" == 'apply' ]] && rm -f "$SOURCE"
  exit 0
fi

if (( n_unattributed > 0 )); then
  {
    echo "rehome-signal-provenance: $n_unattributed of $n_lines line(s) could not be attributed — printed in full below."
    echo "rehome-signal-provenance: they are NOT filed anywhere global (that is the defect this migration removes); copy what you need from here."
  } >&2
  for l in "${unattributed_lines[@]}"; do printf '%s\n' "$l" >&2; done
  if (( ! DISCARD_UNATTRIBUTED )); then
    echo "rehome-signal-provenance: source KEPT at $SOURCE (pass --discard-unattributable to remove it)" >&2
    echo "rehome-signal-provenance: $MODE done — filed=$n_filed unattributed=$n_unattributed source=kept bytes-filed=$bytes_filed"
    exit 0
  fi
fi

if [[ "$MODE" == 'apply' ]]; then
  # The source is removed ONLY after every attributable line's bytes are at a
  # destination (each append above was checked), so an interrupted run is
  # resumable and a re-run is a no-op.
  rm -f "$SOURCE"
fi

echo "rehome-signal-provenance: $MODE done — lines=$n_lines filed=$n_filed unattributed=$n_unattributed dest-dirs=${#seen_dirs[@]} bytes-filed=$bytes_filed"
for d in "${!filed_from[@]}"; do
  printf 'rehome-signal-provenance:   %s ← %s line(s)\n' "$d" "${filed_from[$d]}"
done
if [[ "$MODE" == 'dry-run' ]]; then
  echo "rehome-signal-provenance: dry-run — nothing changed; re-run with --apply"
fi
