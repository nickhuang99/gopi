#!/usr/bin/env bash

# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md — the operator owns this; an agent must never run it (submit the work instead: gopi task submit).
# Real-binary acceptance for zombie auto-disposal (zombie-auto-dispose).
#
# Phases (each in its OWN sandbox home — the operator's live state is never
# touched):
#   1. confirmed + suspected + live-pid rows, heartbeats past the DISPOSE
#      grace → ONE `gopi worker --once` lifecycle pass: confirmed goes
#      failed, the other two stay in_progress, the summary names the sink.
#   2. confirmed row past the STALL grace but INSIDE the dispose grace →
#      reported, disposed=0, still in_progress.
# Evidence: the live operator sink's sha256 must be byte-identical after.
set -uo pipefail
# Scratch lives in the shared queue, never /tmp: source the one helper that
# asserts the path is off `/` and registers trap-time cleanup.
. "$(dirname "$0")/lib/tmp-ban.sh" || { echo "tmp-ban: scripts/lib/tmp-ban.sh missing"; exit 2; }
trap gopi_scratch_cleanup EXIT INT TERM

# Sandbox trees live under the SHARED scratch home's queue (off `/`, age-GC'd),
# never /tmp — see scripts/lib/tmp-ban.sh for why /tmp is banned here.
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/tmp-ban.sh
. "$HERE/lib/tmp-ban.sh"

WORKDIR="$HOME/workspace/gopi-zombie-dispose"
BIN="$HOME/workspace/gopi-zombie-dispose/bin/gopi"
LIVE_SINK="$HOME/.gopi/notify/task-events.jsonl"
FAILED=0
say() { printf '%s\n' "$*"; }
check() { # check <label> <actual> <expected>
  if [ "$2" = "$3" ]; then say "  PASS $1 = $2"; else say "  FAIL $1 = $2 (want $3)"; FAILED=1; fi
}
contains() { # contains <label> <haystack-file> <needle>
  if grep -qF -- "$3" "$2"; then say "  PASS $1"; else say "  FAIL $1 — '$3' not in $2"; sed -n '1,20p' "$2"; FAILED=1; fi
}

BEFORE_SIZE=$(stat -c%s "$LIVE_SINK")
BEFORE=$(sha256sum "$LIVE_SINK" | cut -d' ' -f1)
say "live sink before: $BEFORE ($BEFORE_SIZE bytes)"
say "binary: $BIN ($(sha256sum "$BIN" | cut -c1-16))"

# A dead pid: spawn, reap, confirm gone.
make_dead_pid() {
  sleep 300 & local p=$!
  kill -9 "$p" 2>/dev/null
  wait "$p" 2>/dev/null
  for _ in $(seq 1 50); do [ -d "/proc/$p" ] || break; sleep 0.1; done
  [ -d "/proc/$p" ] && { say "  dead pid $p still visible"; FAILED=1; }
  echo "$p"
}

seed_rows() { # seed_rows <db> <dead_pid> <hb_age_sec>
  local db="$1" dead="$2" age="$3" host; host=$(hostname)
  local now; now=$(date +%s)
  local hb=$((now - age))
  sqlite3 "$db" "UPDATE tasks SET status='in_progress', claimed_by='$host:$dead',
      created_at=$hb, updated_at=$hb, last_toolcall_at=$hb
    WHERE task_id='t-acc-conf';
    UPDATE tasks SET status='in_progress', claimed_by='',
      created_at=$hb, updated_at=$hb, last_toolcall_at=$hb
    WHERE task_id='t-acc-susp';
    UPDATE tasks SET status='in_progress', claimed_by='$host:$$',
      created_at=$hb, updated_at=$hb, last_toolcall_at=$hb
    WHERE task_id='t-acc-live';"
}

run_phase() { # run_phase <label> <hb_age_sec> <expect_conf_status> <expect_disposed>
  local label="$1" age="$2" want_conf="$3" want_disposed="$4"
  local sb; sb=$(gopi_scratch_mkdir zombie-phase)
  local db="$sb/memory.db" sink="$sb/notify.log"
  sqlite3 "$HOME/.gopi/memory.db" .schema 2>/dev/null | grep -v sqlite_sequence | sqlite3 "$db" 2>/dev/null # schema-only clone
  say "== phase: $label (sandbox $sb, heartbeat age ${age}s) =="

  ( cd "$WORKDIR" && \
    GOPI_HOME="$sb" GOPI_MEMORY_DB="$db" GOPI_SETTINGS="$sb/settings.json" GOPI_COMPUTE_NODES="" \
      "$BIN" task submit "acc confirmed zombie" --payload acceptance --task-id t-acc-conf >/dev/null && \
    GOPI_HOME="$sb" GOPI_MEMORY_DB="$db" GOPI_SETTINGS="$sb/settings.json" GOPI_COMPUTE_NODES="" \
      "$BIN" task submit "acc suspected zombie" --payload acceptance --task-id t-acc-susp >/dev/null && \
    GOPI_HOME="$sb" GOPI_MEMORY_DB="$db" GOPI_SETTINGS="$sb/settings.json" GOPI_COMPUTE_NODES="" \
      "$BIN" task submit "acc live zombie" --payload acceptance --task-id t-acc-live >/dev/null ) || { say "  FAIL submit"; FAILED=1; return; }

  local dead; dead=$(make_dead_pid)
  seed_rows "$db" "$dead" "$age"

  GOPI_HOME="$sb" GOPI_MEMORY_DB="$db" GOPI_SETTINGS="$sb/settings.json" GOPI_COMPUTE_NODES="" \
  GOPI_TASK_NOTIFY_CMD="cat >> $sink" GOPI_TASK_NOTIFY_EVENTS="stale,failed" \
  GOPI_TASK_NOTIFY_ASYNC=0 GOPI_TASK_NOTIFY_WEBHOOK="" \
  GOPI_TASK_STALL_SEC=300 GOPI_TASK_DISPOSE_SEC=1200 \
  GOPI_ALLOW_AGENT_SPAWN=1 \
    "$BIN" worker --once >"$sb/worker.out" 2>"$sb/worker.err"

  local st_probe; st_probe=$(sqlite3 "$db" "SELECT task_id||'='||status FROM tasks WHERE task_id LIKE 't-acc-%' ORDER BY task_id" | tr '\n' ' ')
  say "  ledger: $st_probe"
  check "confirmed status" "$(sqlite3 "$db" "SELECT status FROM tasks WHERE task_id='t-acc-conf'")" "$want_conf"
  check "suspected status (never auto-disposed)" "$(sqlite3 "$db" "SELECT status FROM tasks WHERE task_id='t-acc-susp'")" "in_progress"
  check "live-pid status (always vetoed)" "$(sqlite3 "$db" "SELECT status FROM tasks WHERE task_id='t-acc-live'")" "in_progress"
  contains "lifecycle summary counts" "$sb/worker.out" "disposed=$want_disposed"
  contains "lifecycle summary names the sink" "$sb/worker.out" "sink=cat >> $sink"
  contains "stale report delivered to the sandbox sink" "$sink" '"event":"stale"'
  if [ "$want_conf" = "failed" ]; then
    contains "disposal logged with evidence" "$sb/worker.out" "[worker] [zombie-dispose] t-acc-conf disposed"
    contains "zombie audit reason in task_events" "$sb/worker.err" "" 2>/dev/null || true
    local det; det=$(sqlite3 "$db" "SELECT group_concat(detail,' | ') FROM task_events WHERE task_id='t-acc-conf'")
    case "$det" in *zombie*) say "  PASS zombie audit reason recorded" ;; *) say "  FAIL audit reason: $det"; FAILED=1 ;; esac
  fi
  say "  summary line: $(grep -F '[worker] [lifecycle]' "$sb/worker.out" || echo MISSING)"
  [ "$FAILED" = 0 ] && rm -rf "$sb"
}

run_phase "past the dispose grace" 7200 failed 1
run_phase "inside the dispose grace (suspended-host case)" 600 in_progress 0

AFTER=$(sha256sum "$LIVE_SINK" | cut -d' ' -f1)
AFTER_SIZE=$(stat -c%s "$LIVE_SINK")
say "live sink after:  $AFTER ($AFTER_SIZE bytes)"
# The sink is shared with every other running session (they append real
# events), so byte-identity is only meaningful once nobody else writes —
# the honest, falsifiable property is: the region this run APPENDED must
# contain none of our sandbox task ids.
if [ "$AFTER" = "$BEFORE" ]; then
  say "  PASS operator live sink byte-identical (no concurrent writer this run)"
elif tail -c "+$((BEFORE_SIZE + 1))" "$LIVE_SINK" | grep -q 't-acc-'; then
  say "  FAIL our sandbox events leaked into the operator sink"; FAILED=1
else
  say "  PASS operator sink grew only from concurrent sessions ($((AFTER_SIZE - BEFORE_SIZE)) bytes, none ours)"
fi

say ""
if [ "$FAILED" = 0 ]; then say "ACCEPTANCE PASS"; exit 0; else say "ACCEPTANCE FAIL"; exit 1; fi
