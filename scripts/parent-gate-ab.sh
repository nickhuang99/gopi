#!/usr/bin/env bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# Rule 31 A/B red proof at the OPERATOR surface: the SAME commands against a
# pre-fix (master ccdd5f1e) binary and the branch binary, on separate sandbox
# ledgers under the temp root (the child-fail escape MEASURES its sandbox —
# Rule 22 — so a profile outside the temp root is refused).
#
# FIXTURE: the parent is walked to `in_progress` through the ordinary operator
# transition ladder (pending → claimed → in_progress) — a chain whose parent is
# RUNNING. A non-pending row is never a funnel candidate (_only_ pending rows
# are selected), so the CHILD is the only pending row in the queue: the funnel
# must decide about the child directly. That is the condition under test.
#
# `gopi task next` is retired (M4.5), so the claim FUNNEL is exercised through
# the real dispatcher — `gopi worker --once`, which claims via TaskNextFiltered
# — with GOPI_TASK_CHILD_FAIL_TEST=1 (the hermetic escape: a claimed row is
# written back failed with NO model child spawned).
#
#   usage: parent-gate-ab.sh <bin-base> <root-base> <bin-gate> <root-gate>
set -u
BASE_BIN="$1"; BASE_ROOT="$2"; GATE_BIN="$3"; GATE_ROOT="$4"
# OPERATOR_DOOR_BAD counts the arms whose assertion did NOT hold. It is exit
# status, not prose: an A/B transcript whose "must still run" line printed an
# error reads identically to one that ran the row, so the run has to carry its
# own verdict out.
OPERATOR_DOOR_BAD=0
# TMPDIR must sit OFF `/` (a full root device fails every suite, measured
# 2026-10-17) but it must not name ONE operator's home: the caller's TMPDIR
# wins, then the derived default, then the operator's own workspace.
export TMPDIR="${TMPDIR:-${GOPI_SCRATCH:-$HOME/workspace/_tmp}}"

run() { # <label> <binary> <root>
  local LABEL="$1" BIN="$2" ROOT="$3"
  rm -rf "$ROOT"; mkdir -p "$ROOT/ledger" "$ROOT/home"   # ROOT must sit UNDER the temp root
  export GOPI_MEMORY_DB="$ROOT/ledger/memory.db"
  export GOPI_HOME="$ROOT/home"
  # This harness runs INSIDE an agent session; the worker refuses to start with
  # an inherited session id (a correct guard) — clear the session env so the
  # dispatcher surface under test sees an ordinary operator shell.
  unset GOPI_AGENT_SESSION GOPI_EXECUTOR_SESSION_ID GOPI_TASK_RUN_ID GOPI_TASK_REPORT_PATH \
        GOPI_TASK_REPORT_OWNER GOPI_TASK_RUN_WRAPPER_PID GOPI_TASK_WORKDIR GOPI_TASK_RUN_OUTCOME
  echo "################ $LABEL ################"
  "$BIN" task submit "AB parent" --payload "parent briefing" --source human | sed 's/^/  /'
  local PARENT
  # `task list` prints `#<id>  [status]  <summary> …`, so the ID is COLUMN 1
  # with a leading `#`. COLUMN 2 is the status bracket — reading it as the id
  # yields the literal `[pending]`, the parent link silently never forms, and
  # every assertion below then measures a queue that has no parent in it.
  PARENT=$("$BIN" task list --status pending | awk '/AB parent/{print $1}' | sed 's/^#//' | head -1)
  "$BIN" task submit "AB child" --payload "child briefing" --parent-task-id "$PARENT" --source human | sed 's/^/  /'
  echo "  parent=$PARENT (walked to in_progress: a RUNNING parent)"
  for st in claimed in_progress; do "$BIN" task update "$PARENT" --status "$st" >/dev/null; done
  echo
  echo "\$ gopi task list --status pending   (only the CHILD is a funnel candidate)"
  "$BIN" task list --status pending | sed 's/  (model.*//'
  echo
  echo "\$ GOPI_TASK_CHILD_FAIL_TEST=1 gopi worker --once"
  GOPI_TASK_CHILD_FAIL_TEST=1 "$BIN" worker --once 2>&1 | grep -E "claimed |claimed:|no pending|drain|refus" | head -4 | sed 's/^/  /'
  echo
  echo "\$ gopi task list --status pending   (after the round)"
  "$BIN" task list --status pending | sed 's/  (model.*//'
  echo

  # ---- the snapshot for the OPERATOR-DOOR arm (the gate must NOT block it) ---
  echo "\$ gopi task submit \"AB child2\" --parent-task-id $PARENT   (a second parked child)"
  "$BIN" task submit "AB child2" --payload "second child briefing" --parent-task-id "$PARENT" --source human | sed 's/^/  /'
  local CHILD2
  CHILD2=$("$BIN" task list --status pending | awk '/AB child2/{print $1}' | sed 's/^#//' | head -1)
  # The OPERATOR door is `gopi worker <task-id>` — the EXPLICIT id is the
  # operator's own door and it bypasses the parent gate on purpose (Rule 33:
  # "gopi worker <explicit task-id> still runs a parked row — the gate governs
  # the DISPATCH funnel, the explicit id is the operator's own door"). The old
  # spelling `gopi task run <id>` was retired (epic worker-run-entry) and now
  # answers a removal sentence, so it measured the notice instead of the door.
  echo "\$ gopi worker $CHILD2   (the OPERATOR door: must still run a parked row)"
  local DOOR_OUT
  DOOR_OUT=$(GOPI_TASK_CHILD_FAIL_TEST=1 "$BIN" worker "$CHILD2" 2>&1 \
    | grep -iE "claimed|running gopi|refus|status|failed" | head -4)
  echo "$DOOR_OUT" | sed 's/^/  /'
  # THE ASSERTION, not the transcript: the operator door DID run the parked row
  # iff the child actually started and the hermetic escape then wrote the row
  # back failed. `running gopi with task <id> as briefing` is the dispatcher's
  # own line for "the row was handed to a child"; the synthetic-failure line is
  # the child's writeback. Printing those lines without asserting them is what
  # let the previous spelling (a done / retired verb) look like a measurement.
  if echo "$DOOR_OUT" | grep -q "running gopi with task $CHILD2 as briefing" \
     && echo "$DOOR_OUT" | grep -q "synthetic child failure"; then
    echo "  [ok] the operator door RAN the parked row ($CHILD2): child started + writeback present"
  else
    echo "  [BAD] the operator door did NOT run the parked row $CHILD2 — the arm asserts nothing"
    OPERATOR_DOOR_BAD=$((OPERATOR_DOOR_BAD+1))
  fi
  echo "\$ gopi task list --status failed   (the row the explicit run consumed)"
  "$BIN" task list --status failed | sed 's/  (model.*//' | sed 's/^/  /'
}

run "BASE — master ccdd5f1e (gate ABSENT)" "$BASE_BIN" "$BASE_ROOT"
echo
run "GATE — feat/parent-task-claim-gate (gate PRESENT)" "$GATE_BIN" "$GATE_ROOT"
echo
if [ "$OPERATOR_DOOR_BAD" -ne 0 ]; then
  echo "OPERATOR-DOOR: $OPERATOR_DOOR_BAD arm(s) FAILED — the operator door did not run its parked row" >&2
  exit 1
fi
echo "OPERATOR-DOOR: every arm RAN its parked row through the explicit-id door (gopi worker <id>)"
