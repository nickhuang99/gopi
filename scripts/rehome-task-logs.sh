#!/usr/bin/env bash
# rehome-task-logs.sh — one-time cleanup of the RETIRED per-worker log area
# (<gopi-dir>/logs/worker-<ts>-<i>.log and the equally anonymous
# gopi-<ts>.log), re-homing every line that CAN be attributed to the task
# session directory it belongs to and deleting what cannot.
#
# WHY (operator ruling, epic task-session-cwd, 2026-09-12): a task's output
# must be reachable FROM ITS TASK ID. The retired shapes carry a timestamp and
# an index instead, so given a failed task id there is no path to its output —
# and the timestamps of concurrent workers overlap (the 04:12 incident started
# 6 workers inside one ~70 s window). Those files are therefore stale data
# nobody can trace. They are ALSO not worthless: a worker log whose own body
# names a claimed task id IS that task's output, and belongs in
# <gopi-dir>/sessions/<task-id>/ beside the JSONL, the raw log, the report and
# the attachments (the ONE directory a task owns).
#
# THE CLASSIFICATION IS PER FILE, FROM THE FILE'S OWN CONTENT, never from its
# name, its mtime, or a neighbouring record:
#
#   worker-<ts>-<i>.log with 0 claims  → DELETE. An idle worker's banner and
#       "queue drained" lines belong to no task; there is nothing to attribute
#       and nothing to learn (measured on the real profile: 2 of 20 files).
#   worker-<ts>-<i>.log with 1 claim   → the whole file is that task's worker
#       output → APPEND it to <sessions>/<task>/output.legacy-<stamp>.log
#       ("output*.log" is the glob every reader uses — `gopi task show <id>`
#       prints exactly that list).
#   worker-<ts>-<i>.log with N claims  → SPLIT at the claim boundaries: claim
#       i owns [claim_i, claim_{i+1}-1], the last claim owns the rest — so each
#       task receives ONLY its own round (measured: 13 of 20 files served 2..20
#       tasks, so a whole-file copy to one task would have mis-attributed the
#       rest — the exact defect this epic removes).
#   gopi-<ts>.log                      → DELETE. Produced by a retired
#       leveled FileLogger that recorded NO task id and NO session id
#       anywhere in the file (verified over all 71 on the real profile), so
#       no attribution is derivable by any means — an artifact nobody can
#       attribute is useless, and keeping it only invites the next reader to
#       spend time trying.
#
# NEVER TOUCHED: every other file in the log area (merger.log, the watchdog
# logs, worker-startup.log, worker-autostart.log, …) — they are process-scoped
# records that are still live, and they are not the retired shape.
#
# SAFETY: dry-run is the DEFAULT (nothing moves, the plan prints); --apply is
# explicit. Content is APPENDED, never truncated, so a destination that already
# holds a run's output keeps it. A source is removed ONLY after its bytes are
# at their destination, so an interrupted run is resumable and a second
# --apply is a no-op (the sources are gone).
#
# Usage:
#   rehome-task-logs.sh [--dry-run|--apply] [--home DIR] [--logs DIR] [--sessions DIR]
#     --home DIR       gopi data dir        (default: $GOPI_HOME/.gopi, else ~/.gopi)
#     --logs DIR       the log area to sweep (default: <home>/logs)
#     --sessions DIR   the sessions tree     (default: <home>/sessions)
#     --dry-run        classify + print the plan, change nothing (DEFAULT)
#     --apply          perform the appends and deletions
#
# Exit: 0 on success; 2 on usage error or an unusable destination.

set -euo pipefail

# ---- constants (Rule 1: every literal lives here, one place) ----------------
# The retired per-worker shape: worker-<yyyymmdd>-<hhmmss>-<index>.log. It is
# anchored on the exact shape so the legitimate fixed files (worker-startup.log,
# worker-autostart.log, worker-night-*.log) are never matched.
RETIRED_WORKER_RE='^worker-[0-9]{8}-[0-9]{6}-[0-9]+\.log$'
# The retired leveled-logger shape: gopi-<yyyymmdd>-<hhmmss>.log.
RETIRED_GOPI_RE='^gopi-[0-9]{8}-[0-9]{6}\.log$'
# The claim line a worker prints the moment its atomic claim lands. It is the
# ONLY in-file proof of which task a slice belongs to.
CLAIM_PREFIX='worker: claimed '
# The retired name's prefix, stripped to build the destination stamp.
RETIRED_WORKER_PREFIX='worker-'
# Destination name: output.legacy-<stamp>.log inside the task's session dir.
# The stem and the suffix are the SAME strings the Go contract names
# (tools.WorkerLogFilePrefixStem / workerLogFileSuffix), so the destination is
# matched by the reader's glob ("output*.log") and a re-homed run is reachable
# exactly like a native one.
OUTPUT_STEM='output'
LEGACY_INFIX='.legacy-'
LOG_SUFFIX='.log'
SESSIONS_LEAF='sessions'

MODE='dry-run'
HOME_DIR="${GOPI_HOME:+${GOPI_HOME%/}/.gopi}"
HOME_DIR="${HOME_DIR:-${HOME}/.gopi}"
LOGS_DIR=''
SESSIONS_DIR=''

# ---- arg parsing -----------------------------------------------------------
while (($#)); do
  case "$1" in
    --dry-run) MODE='dry-run'; shift ;;
    --apply) MODE='apply'; shift ;;
    --home) HOME_DIR="${2%/}"; shift 2 ;;
    --logs) LOGS_DIR="${2%/}"; shift 2 ;;
    --sessions) SESSIONS_DIR="${2%/}"; shift 2 ;;
    -h|--help) sed -n '2,/^set -euo pipefail$/p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "rehome-task-logs: unexpected argument '$1' (see --help)" >&2; exit 2 ;;
  esac
done

[[ -n "$LOGS_DIR" ]] || LOGS_DIR="$HOME_DIR/logs"
[[ -n "$SESSIONS_DIR" ]] || SESSIONS_DIR="$HOME_DIR/$SESSIONS_LEAF"

if [[ ! -d "$LOGS_DIR" ]]; then
  echo "rehome-task-logs: no log area at $LOGS_DIR (nothing to do)" >&2
  exit 0
fi

# ---- counters (the report a human reads) -----------------------------------
n_moved=0 n_split=0 n_deleted_idle=0 n_deleted_anonymous=0 n_kept=0
bytes_moved=0 bytes_deleted=0

# Task ids seen, so a whole-run summary names every directory that gained bytes.
declare -A seen_tasks=()

# rehome_slice <src> <task_id> <start_line> <end_line> <stamp>
# Appends lines start..end of src to the task's session directory. The bytes
# reach the destination BEFORE the source is removed (the caller deletes only
# once every slice of the file has been written).
rehome_slice() {
  local src="$1" task="$2" start="$3" end="$4" stamp="$5"
  local dir="$SESSIONS_DIR/$task"
  local dest="$dir/${OUTPUT_STEM}${LEGACY_INFIX}${stamp}${LOG_SUFFIX}"
  local tmp="$dest.$$.part"
  (( end >= start )) || return 0
  if [[ "$MODE" == 'dry-run' ]]; then
    printf '  would re-home %s:%s-%s → %s\n' "$(basename "$src")" "$start" "$end" "$dest"
    seen_tasks["$task"]=1
    return 0
  fi
  if ! mkdir -p "$dir"; then
    echo "rehome-task-logs: cannot create $dir — aborting (a task whose output has no home must not be deleted from its only copy)" >&2
    exit 2
  fi
  if ! sed -n "${start},${end}p" "$src" > "$tmp"; then
    echo "rehome-task-logs: cannot read $src:$start-$end" >&2
    rm -f "$tmp"
    exit 2
  fi
  # APPEND: a destination that already holds output keeps every byte of it.
  if ! cat "$tmp" >> "$dest"; then
    echo "rehome-task-logs: cannot append to $dest" >&2
    rm -f "$tmp"
    exit 2
  fi
  rm -f "$tmp"
  seen_tasks["$task"]=1
}

# rehome_worker_log <path> <name> — the whole claim-splitting rule for one
# retired worker log, so the sweep loop stays a reader.
rehome_worker_log() {
  local f="$1" name="$2"
  local stamp="${name#"$RETIRED_WORKER_PREFIX"}"
  stamp="${stamp%"$LOG_SUFFIX"}"
  local size total claim_lines
  size=$(wc -c < "$f")
  total=$(wc -l < "$f")
  claim_lines=$(grep -n "^${CLAIM_PREFIX}" "$f" | cut -d: -f1 || true)
  if [[ -z "$claim_lines" ]]; then
    printf '  %-34s %8s B  idle worker (no claim) → delete\n' "$name" "$size"
    n_deleted_idle=$((n_deleted_idle + 1)); bytes_deleted=$((bytes_deleted + size))
    [[ "$MODE" == 'apply' ]] && rm -f "$f"
    return 0
  fi
  local claim_arr=()
  mapfile -t claim_arr <<< "$claim_lines"
  local i ln end task start
  for i in "${!claim_arr[@]}"; do
    ln="${claim_arr[$i]}"
    [[ -n "$ln" ]] || continue
    if (( i + 1 < ${#claim_arr[@]} )); then
      end=$(( claim_arr[i + 1] - 1 ))
    else
      end="$total"
    fi
    task=$(sed -n "${ln}p" "$f" | awk -v p="$CLAIM_PREFIX" 'index($0,p)==1 {print $3}')
    if [[ -z "$task" ]]; then
      echo "rehome-task-logs: $name:$ln carries the claim prefix but no task id — skipping that slice" >&2
      continue
    fi
    # The slice of THIS claim: its claim line to the line before the next. A
    # single-claim file starts at line 1 instead, so the round's own banner
    # (which precedes the claim and belongs to no earlier task) travels with it.
    start="$ln"
    if (( ${#claim_arr[@]} == 1 )); then
      start=1
    fi
    rehome_slice "$f" "$task" "$start" "$end" "$stamp"
  done
  if (( ${#claim_arr[@]} == 1 )); then
    printf '  %-34s %8s B  1 claim → re-homed whole\n' "$name" "$size"
    n_moved=$((n_moved + 1))
  else
    printf '  %-34s %8s B  %s claims → split at claim boundaries\n' "$name" "$size" "${#claim_arr[@]}"
    n_split=$((n_split + 1))
  fi
  bytes_moved=$((bytes_moved + size))
  [[ "$MODE" == 'apply' ]] && rm -f "$f"
  return 0
}

# ---- the sweep -------------------------------------------------------------
echo "rehome-task-logs: mode=$MODE logs=$LOGS_DIR sessions=$SESSIONS_DIR"

for f in "$LOGS_DIR"/*; do
  [[ -f "$f" ]] || continue
  name="$(basename "$f")"
  case "$name" in
    *"$LOG_SUFFIX") ;;
    *) printf '  %-34s %8s B  kept (not a task-scoped log)\n' "$name" "$(wc -c < "$f")"
       n_kept=$((n_kept + 1)); continue ;;
  esac
  if [[ "$name" =~ $RETIRED_WORKER_RE ]]; then
    rehome_worker_log "$f" "$name"
    continue
  fi
  if [[ "$name" =~ $RETIRED_GOPI_RE ]]; then
    size=$(wc -c < "$f")
    printf '  %-34s %8s B  retired logger (no task id in body) → delete\n' "$name" "$size"
    n_deleted_anonymous=$((n_deleted_anonymous + 1)); bytes_deleted=$((bytes_deleted + size))
    [[ "$MODE" == 'apply' ]] && rm -f "$f"
    continue
  fi
  printf '  %-34s %8s B  kept (not a task-scoped log)\n' "$name" "$(wc -c < "$f")"
  n_kept=$((n_kept + 1))
done

echo "rehome-task-logs: $MODE done — re-homed=$n_moved split=$n_split deleted(idle)=$n_deleted_idle deleted(retired-logger)=$n_deleted_anonymous kept=$n_kept bytes_re-homed=$bytes_moved bytes_deleted=$bytes_deleted"
if ((${#seen_tasks[@]})); then
  printf 'rehome-task-logs: task dirs touched: %s\n' "${!seen_tasks[*]}"
fi
if [[ "$MODE" == 'dry-run' ]]; then
  echo "rehome-task-logs: dry-run — nothing changed; re-run with --apply"
fi
