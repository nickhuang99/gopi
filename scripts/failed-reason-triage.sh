#!/usr/bin/env bash
# failed-reason-triage.sh — triage the ledger rows that carry the fact-free
# "task failed" reason: for each one, name WHAT closed it and whether the row
# can be decided (reissue vs tag resolved) from the ledger alone.
#
# WHY THIS EXISTS (epic failed-reason-attribution, measured 2026-10-07): 179 of
# 330 `failed` rows on this ledger carried reason='task failed' — a sentence
# that asserts a cause the row does not have. The cause WAS collected by the
# same call that closed the row (it rode the row's own task_events trail); only
# the reason column fell back to the literal. The fix (internal/tools/tasks.go,
# taskFailedReason) makes every NEW failed row carry its cause; this script is
# how the HISTORICAL backlog is decided, one row at a time, without touching a
# single row (Rule 26: repair narrow, never rewrite the backlog in bulk).
#
# CONSTRAINTS IT RESPECTS
#   * READ-ONLY: the sqlite handle is `mode=ro` and every statement is a
#     SELECT — the ledger is append-only history and this script cannot write a
#     row even by accident.
#   * NO WRITES AT ALL: no temp file, no mktemp, no /tmp (Rule 22) — stdout only.
#   * NAMED CONSTANTS, mirrored not redefined (Rules 1b/13/14): each value below
#     is the production value it asks about, printed with the class it decides.
#
# Usage:
#   scripts/failed-reason-triage.sh                 # every literal-reason failed row
#   scripts/failed-reason-triage.sh <id> [<id>...]  # just those rows (sampling)
set -euo pipefail

# --- the one historical spelling this script selects on ------------------------
# The pre-fix default reason, frozen: it is a HISTORICAL value (no longer
# produced by any code path after the fix), so it is named once, here, and the
# report quotes the same string.
HISTORICAL_DEFAULT_REASON='task failed'

# The tag the adjudication convention writes on a row a human/machine has
# already disposed of (internal/tools' taskTagResolved — the tombstone that
# makes `task list --status failed` hide the row).
RESOLVED_TAG='resolved'

# The per-run session directory root: $HOME/.gopi/sessions/<task-id>/ (the
# run's own address, named by the row's id; the session_id column names a
# DIFFERENT directory, keyed by session uuid).
sessions_root() { printf '%s\n' "${GOPI_HOME:-$HOME}/.gopi/sessions"; }

db=${GOPI_MEMORY_DB:-$HOME/.gopi/memory.db}
sql() { sqlite3 -cmd '.timeout 8000' "file:${db}?mode=ro" "$@"; }

if [[ ! -r $db ]]; then
    echo "failed-reason-triage: ledger not readable: $db" >&2
    exit 2
fi

# --- the worklist -------------------------------------------------------------
# One line per row, 0x1f-separated (a byte no column here can carry, so a
# digest full of pipes and spaces survives verbatim): id / reason / tags /
# session_id / latest trail detail. The row FILTER (the ids named on the
# command line) is applied in the shell loop below — an id list spliced into
# SQL is one quoting bug away from a silently empty worklist, and a triage
# that reports nothing must never be mistaken for a triage that found nothing
# (Rule 15).
US=$'\x1f'
sql() { sqlite3 -cmd '.timeout 8000' -separator "$US" "file:${db}?mode=ro" "$@"; }

select_rows() {
    sql "
        SELECT t.task_id,
               replace(coalesce(t.reason,''), char(10), ' '),
               replace(coalesce(t.tags,''),   char(10), ' '),
               coalesce(t.session_id,''),
               replace(coalesce((SELECT e.detail FROM task_events e
                                  WHERE e.task_id = t.task_id
                                  ORDER BY e.id DESC LIMIT 1), ''), char(10), ' ')
          FROM tasks t
         WHERE t.status='failed' AND t.reason='${HISTORICAL_DEFAULT_REASON}'
         ORDER BY t.task_id;"
}

# --- the classifier -----------------------------------------------------------
# The classes are keyed on the LATEST trail detail — the writer that closed the
# row — because that is what the row itself records. Each class states its
# verdict, i.e. what a reader may DO with the row, and the evidence a sceptic
# can re-run.
#
#   ZOMBIE-DISPOSAL   the sweep closed it (heartbeat stall + dead claim pid):
#                     the work never finished — REISSUE.
#   ABNORMAL-CHILD    the run wrapper saw the child exit non-zero: REISSUE
#                     (the session may still be resumable).
#   RUN-CLOSEOUT      the END hook closed a row whose wrapper never wrote back:
#                     REISSUE after reading the row's own session.
#   LITTER-DISPOSAL   the never-started auto row was collected: REISSUE.
#   RESUME-HINT-ONLY  the trail carries only the generic resume hint — UNDECIDED
#                     without the session raw log.
#   NO-TRAIL          nothing was ever recorded; the session directory and the
#                     `resolved` tag are the only remaining evidence.
classify() {
    local detail="$1" tags="$2" tid="$3" sid="$4"
    case "$detail" in
        'zombie disposal'*)
            printf 'ZOMBIE-DISPOSAL\tREISSUE (work never finished: heartbeat stall + dead claim pid)\t%s' \
                "task_events.detail=${detail:0:100}"
            ;;
        'gopi run failed'*)
            printf 'ABNORMAL-CHILD\tREISSUE (the run wrapper recorded a non-zero child exit)\t%s' \
                "task_events.detail=${detail:0:100}"
            ;;
        'run closeout'*)
            printf 'RUN-CLOSEOUT\tREISSUE after reading the session\t%s' \
                "task_events.detail=${detail:0:100}"
            ;;
        'auto-run litter'*)
            printf 'LITTER-DISPOSAL\tREISSUE (no run ever started)\t%s' \
                "task_events.detail=${detail:0:100}"
            ;;
        *'FAILED — recoverable'*|*'FAILED — NOT recoverable'*)
            printf 'RESUME-HINT-ONLY\tUNDECIDED from the ledger (read the session raw log)\t%s' \
                "task_events.detail=${detail:0:60}…"
            ;;
        '')
            local dir="$(sessions_root)/${tid}" verdict
            if [[ -d $dir ]]; then
                verdict='UNDECIDED: the trail is empty but the run directory survives — read it'
            elif [[ -n $sid && -d "$(sessions_root)/${sid}" ]]; then
                verdict='UNDECIDED: the trail is empty; the session uuid directory survives — read it'
            elif [[ ",$tags," == *",${RESOLVED_TAG},"* ]]; then
                verdict="ALREADY-RESOLVED (carries the ${RESOLVED_TAG} tag — a human disposed of it)"
            else
                verdict='NO-EVIDENCE: no trail, no run directory, no session dir — tag it resolved'
            fi
            printf 'NO-TRAIL\t%s\t%s' "$verdict" "sessions/${tid} absent; session_id=${sid:-<none>}"
            ;;
        *)
            # The trail names a cause the writer set above does not cover (a
            # merge-verify FAIL, a pre-merge refusal, a stale align note …).
            # The reason column was starved, but the CAUSE is right there — so
            # the verdict is not "unknown", it is "read the evidence".
            printf 'DETAIL-PRESENT\tCAUSE RECORDED — read the evidence column\t%s' \
                "task_events.detail=${detail:0:100}"
            ;;
    esac
}

# --- main ---------------------------------------------------------------------
printf '## failed-reason triage — reason=%s, ledger=%s\n' "$HISTORICAL_DEFAULT_REASON" "$db"
printf '# generator: scripts/failed-reason-triage.sh (read-only; one line per row)\n'
printf 'task_id\tclass\tverdict\tevidence\n'
want=" $* "
n=0
while IFS="$US" read -r tid reason tags sid detail; do
    [[ -z ${tid:-} ]] && continue
    if [[ $# -gt 0 && $want != *" $tid "* ]]; then continue; fi
    # A herestring, not a process substitution: bash's `read` returns non-zero
    # on EOF-without-newline, and under `set -e` that would abort the whole
    # triage after ONE row (measured — the first version of this script exited
    # 1 having printed nothing). The herestring appends the delimiter, so the
    # read succeeds and the loop reaches every row.
    IFS=$'\t' read -r class verdict evidence <<<"$(classify "$detail" "$tags" "$tid" "$sid")"
    printf '%s\t%s\t%s\t%s\n' "$tid" "$class" "$verdict" "$evidence"
    n=$((n + 1))
done < <(select_rows)
printf '# rows triaged: %d\n' "$n"
if ((n == 0)); then
    echo 'failed-reason-triage: the worklist is EMPTY — either no row carries the historical reason'"'"'s value or the ledger/query is wrong; re-run without arguments' >&2
    exit 1
fi
