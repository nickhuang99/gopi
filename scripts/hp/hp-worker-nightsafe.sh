#!/bin/bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
#
# ============================================================================
# RETIRED 2026-09-26 — THIS SCRIPT MUST NOT RUN UNATTENDED. READ THIS FIRST.
# ============================================================================
#
# OPERATOR RULING (2026-09-26), verbatim because it is not up for
# re-litigation:
#
#     "这个 cronjob 必须终止，我们不能依赖这种系统来做，它掩盖了问题。"
#
# It carries the SAME hazard as its sister hp-worker-autostart.sh, and this
# file was audited before being changed (brief §4: "check it; do not assume").
# Its night window (`23:00-09:00`) is what made it worse, not better: the
# window is precisely when nobody is watching, so a scheduled caller here would
# be the same net wearing a clock. An automatic caller is REFUSED LOUDLY; an
# operator may still run it by hand, inside the window, in a terminal.
#
# THE MEASUREMENT THAT PRODUCED THE RULING (recorded once; do not re-derive):
#   `crontab -l` carried `*/10 * * * * /home/nick/bin/hp-worker-autostart.sh`.
#   Its log ~/.gopi/logs/worker-autostart.log was 36,398 lines and recorded
#   1,214 delegations. The log shows the shape plainly — the line fired, a
#   worker started, the count read zero seconds later, ten minutes later it
#   fired again:
#
#     [12:50:01] no worker alive, 3 pending — delegating to launcher 1
#     started worker 1: pid 2709846
#     live gopi workers: 0 (pids: none)
#     [12:50:05] launcher accepted: 1 worker(s) …
#     [13:00:01] no worker alive, 3 pending — delegating to launcher 1
#
# THE CONSEQUENCE — the net MASKED the failure instead of exposing it
# (掩盖了问题), and that is measured, not asserted: those historical pids are
# gone, so the zero count was HONEST. Something was killing the workers, and
# the scheduled line kept replacing them — 1,214 times. For an unknown period
# it silently converted "workers keep dying" into "there is always a worker
# alive", which is exactly why the underlying failure was never diagnosed. A
# net that keeps a hand moving while nobody watches makes "what did that?"
# unanswerable. The defect is a DEPENDENCY defect, not a code defect (GOPI.md
# Rule 35): the schedule was added to make two starters reliable and it covered
# the failure up instead.
#
# THE GATE — the same TWO arms as the sister script, neither forgeable from a
# crontab line:
#
#   1. SCHEDULER ANCESTRY — the kernel's own process table is walked for the
#      scheduler in this script's ancestry (`cron`, `atd`, `anacron`): the tell
#      is WHAT THE CALLER IS, never what the environment says, so a wrapper
#      script's `SHELL=/bin/sh` cannot claim a watched context.
#   2. NO TERMINAL — a watched run has one on stdin; cron supplies none and no
#      crontab line can manufacture a tty (a kernel property, not a variable).
#
# There is deliberately NO escape hatch: no --force, no GOPI_ALLOW_AUTOSTART,
# no alternate cadence, no exemption for --dry-run — and no replacement
# launcher (systemd timer, cron line, daemon): an escape hatch is the ruling
# reversed.
#
# The repo-side guard is internal/testutil/hp_worker_launchers_test.go, which
# pins this contract (scheduler context → refusal, non-zero, message present).
#
# WHAT THIS SCRIPT IS NOT (the defect this rewrite closed, 2026-09-12):
# it used to run the worker start ITSELF —
#
#     setsid nohup ./bin/gopi worker >> "$LOG" 2>&1 < /dev/null &
#     echo "started pid $!" >> "$LOG"
#
# — and never looked at the child's exit status, so a launch REFUSED by the
# agent-session guard (exit 3) was logged exactly like a success. That inline
# start was a SECOND launcher (its own log contract, its own `pgrep -x`
# accounting, no seams) — the single launcher is ~/bin/gopi-worker-up.sh,
# installed from <checkout>/scripts/gopi-worker-up.sh.
#
# WHAT IT STILL DOES for the one caller that remains (the operator) — four jobs
# only:
#   1. the night window (its OWN reason to exist — the one thing the launcher
#      does not know about),
#   2. LOOK BEFORE ASKING (the launcher's --count seam + a pending-work gate),
#   3. DELEGATE (ONE call carrying the count),
#   4. CHECK THE STATUS (loud FAILED + explicit "no worker was started by this
#      script" + non-zero exit).
#
# Usage:  hp-worker-nightsafe.sh [--dry-run] [-h|--help]
#   --dry-run  print the launcher call this script would make and exit 0;
#              asks for NOTHING and starts NOTHING. It does NOT bypass the
#              window gate — a dry run reports what a real run would do NOW —
#              and it is NOT an exemption from the retirement gate either:
#              reading what would happen is still running it.
#   -h|--help  print this header — it carries the ruling. Reading the ruling is
#              not running the script, so help needs no terminal.
#
# Env knobs (the window bounds are knobs so the behaviour is testable without
# waiting for a wall clock):
#   GOPI_WORKER_LAUNCHER=~/bin/gopi-worker-up.sh  the single launcher
#   GOPI_WORKER_REPO=~/work/GoPi                  the checkout the launcher uses
#   GOPI_WORKER_N=1                               workers to ask for
#   GOPI_NIGHT_START_HOUR=23                      window opens (local hour)
#   GOPI_NIGHT_END_HOUR=9                         window closes (local hour)
#   GOPI_WORKER_NIGHTSAFE_LOG=<gopi-dir>/logs/worker-night-cron.log
set -uo pipefail

SELF="${0##*/}"
LAUNCHER="${GOPI_WORKER_LAUNCHER:-$HOME/bin/gopi-worker-up.sh}"
REPO="${GOPI_WORKER_REPO:-$HOME/work/GoPi}"
N="${GOPI_WORKER_N:-1}"
START_HOUR="${GOPI_NIGHT_START_HOUR:-23}"
END_HOUR="${GOPI_NIGHT_END_HOUR:-9}"
REFUSAL_EXIT_CODE="${GOPI_WORKER_REFUSAL_EXIT_CODE:-3}"
# The knob's NAME, defined once: the FAILED line spells out the machine-readable
# form ($GOPI_WORKER_REFUSAL_EXIT_CODE=3) so a grep on either side finds it.
REFUSAL_KNOB="GOPI_WORKER_REFUSAL_EXIT_CODE"

# --- the retirement gate's constants (Rule 1: every value named once) ------
RETIRED_ON="2026-09-26"
# The ruling, verbatim, in the file that has to answer for it.
OPERATOR_RULING='这个 cronjob 必须终止，我们不能依赖这种系统来做，它掩盖了问题。'
# The measured delegation count (the log's 36,398 lines carry these).
DELEGATION_MEASUREMENT='1,214 delegations'
# The status a retirement refusal returns. It is deliberately NOT the guard's
# refusal code: "this script is retired" and "the launch was refused by the
# guard" are two different facts, and an operator reading `echo $?` must be
# able to tell them apart. 78 is EX_CONFIG in sysexits(3) — a configuration
# error, which is what calling a retired script is.
RETIRED_EXIT_CODE=78
# The kernel interface the ancestry probe reads. /proc is not a path to invent:
# the process table IS the evidence for what the caller is.
PROC_ROOT="/proc"
PROC_SELF="$PROC_ROOT/self"
PROC_STAT_NAME="stat"
# The comm values that mean the caller is automatic. Exact whole-value matches:
# `crond` is a different daemon and `crontab -e` in an operator's shell is a
# different process entirely.
SCHEDULER_COMM_MARKERS="cron atd anacron"
# The two arm names as they appear in the refusal report.
SCHEDULER_CONTEXT="scheduler-ancestry"
UNATTENDED_CONTEXT="no-terminal"

gopi_dir() {
  if [ -n "${GOPI_HOME:-}" ]; then
    printf '%s/.gopi\n' "${GOPI_HOME%/}"
  else
    printf '%s/.gopi\n' "$HOME"
  fi
}
LOG="${GOPI_WORKER_NIGHTSAFE_LOG:-$(gopi_dir)/logs/worker-night-cron.log}"

DRY=0
while [ $# -gt 0 ]; do
  case "$1" in
    -h|--help) sed -n '2,/^set -uo pipefail$/p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    --dry-run) DRY=1; shift ;;
    *) echo "$SELF: unknown flag: $1" >&2; exit 2 ;;
  esac
done

log() {
  mkdir -p "$(dirname "$LOG")" 2>/dev/null || true
  echo "[$(date '+%F %T')] $*" >> "$LOG"
}

fail_loud() {
  local status="$1"
  log "FAILED: the launcher $LAUNCHER did not start a worker (exit $status) — the agent-session guard's refusal is \$$REFUSAL_KNOB=$REFUSAL_EXIT_CODE, and this script invents no override for it; no worker was started by $SELF"
  echo "$SELF: FAILED: launcher exited $status (refusal \$$REFUSAL_KNOB=$REFUSAL_EXIT_CODE) — no worker was started" >&2
  exit "$status"
}

# --- the retirement gate --------------------------------------------------
# refuse_retired is the ONE exit path of the gate: the record names the ruling,
# the reason and the offending context, goes to stderr AND the log, calls the
# launcher NOT AT ALL, and exits non-zero. It never falls through.
refuse_retired() {
  local context="$1"
  local evidence="$2"
  log "REFUSED: retired $RETIRED_ON by the operator ruling; context=$context ($evidence) — an automatic caller may not run this script: the net asked the launcher for a worker whenever none was alive ($DELEGATION_MEASUREMENT) and that MASKED a worker-death loop instead of exposing it; no launcher was called and no worker was started by $SELF"
  echo "$SELF: REFUSED: this script is RETIRED ($RETIRED_ON)." >&2
  echo "$SELF: the operator ruling is \"$OPERATOR_RULING\"" >&2
  echo "$SELF: the reason: this net asked the launcher for a worker whenever none was alive ($DELEGATION_MEASUREMENT), which MASKED a worker-death loop instead of exposing it." >&2
  echo "$SELF: context=$context ($evidence) — this is an AUTOMATIC calling context, which the ruling forbids." >&2
  echo "$SELF: no launcher was called and no worker was started." >&2
  echo "$SELF: an operator may still run it by hand in a terminal; there is no --force, no GOPI_ALLOW_AUTOSTART and no alternate cadence — an escape hatch is the ruling reversed." >&2
  exit "$RETIRED_EXIT_CODE"
}

# comm_is_scheduler: one marker at a time (a case pattern cannot carry a list).
comm_is_scheduler() {
  local marker
  for marker in $SCHEDULER_COMM_MARKERS; do
    if [ "$1" = "$marker" ]; then return 0; fi
  done
  return 1
}

# scheduler_ancestor prints "<pid> <comm>" of the nearest ancestor process whose
# image name is a scheduler — evidence read from the KERNEL's process table, so
# a wrapper script or an environment variable cannot forge a watched context.
# Exit status 1 means no scheduler ancestor (which is not, by itself, a reason
# to run: the terminal arm below still has to pass).
scheduler_ancestor() {
  local pid ppid comm line rest
  pid=$$
  while [ "$pid" -gt 1 ]; do
    [ -r "$PROC_ROOT/$pid/$PROC_STAT_NAME" ] || break
    line=""
    IFS= read -r line < "$PROC_ROOT/$pid/$PROC_STAT_NAME" || break
    # comm is the parenthesised field and may itself contain spaces or
    # parentheses, so the ONLY safe parse is: strip up to the FIRST "(", then
    # up to the LAST ")". Never a field index over the whole line.
    comm="${line#*(}"
    comm="${comm%)*}"
    # What follows the closing ")" is " state ppid pgrp … " — field 2 is ppid.
    rest="${line#*) }"
    ppid="$(printf '%s\n' "$rest" | awk '{print $2}')"
    if comm_is_scheduler "$comm"; then
      printf '%s %s\n' "$pid" "$comm"
      return 0
    fi
    pid="${ppid:-0}"
  done
  return 1
}

# watched_terminal: an operator's run has a terminal on stdin. A scheduler
# supplies none, and no crontab line can manufacture one.
watched_terminal() {
  [ -t 0 ]
}

SCHEDULER_EVIDENCE="$(scheduler_ancestor)" || SCHEDULER_EVIDENCE=""
if [ -n "$SCHEDULER_EVIDENCE" ]; then
  refuse_retired "$SCHEDULER_CONTEXT" "ancestor ${SCHEDULER_EVIDENCE%% *} is $(printf '%s' "$SCHEDULER_EVIDENCE" | cut -d' ' -f2)"
fi
if ! watched_terminal; then
  refuse_retired "$UNATTENDED_CONTEXT" "stdin is not a terminal"
fi
# --- end of the retirement gate: from here the run is a watched one --------

# --- 1. the night window --------------------------------------------------
hour="$(date +%H)"
hour=$((10#$hour))
if [ "$hour" -ge "$START_HOUR" ] || [ "$hour" -lt "$END_HOUR" ]; then
  :
else
  exit 0
fi

if [ "$DRY" -eq 1 ]; then
  echo "$SELF dry-run: window=${START_HOUR}:00-${END_HOUR}:00 now=$(date '+%H:%M')"
  echo "$SELF dry-run: launcher=$LAUNCHER repo=$REPO n=$N log=$LOG"
  echo "$SELF dry-run: would run -> GOPI_WORKER_REPO=$REPO $LAUNCHER $N"
  echo "$SELF dry-run: this script starts no worker itself; the launcher owns the spawn, the log path and the argv shape"
  exit 0
fi

# A missing launcher is a failed launch: loud, non-zero, never a fallback to an
# inline start (that fallback is the defect). Checked AFTER --dry-run, which
# starts nothing and therefore needs no launcher to exist yet.
if [ ! -x "$LAUNCHER" ]; then
  fail_loud 127
fi

# --- 2. look before asking ------------------------------------------------
counts="$("$LAUNCHER" --count 2>&1)" || fail_loud $?
workers="$(printf '%s\n' "$counts" | sed -n 's/^live [^ ]* workers: \([0-9][0-9]*\).*/\1/p' | head -1)"
if [ -z "$workers" ]; then
  log "FAILED: $LAUNCHER --count produced no worker number (output: $counts); no worker was started by $SELF"
  echo "$SELF: FAILED: cannot read the live worker count — no worker was started" >&2
  exit 1
fi
if [ "$workers" -gt 0 ]; then
  exit 0
fi

BIN="$REPO/bin/gopi"
if [ ! -x "$BIN" ]; then
  log "FAILED: $BIN is missing or not executable — cannot check for pending work; no worker was started by $SELF"
  echo "$SELF: FAILED: $BIN missing/not executable — no worker was started" >&2
  exit 1
fi
pending="$("$BIN" task list --status pending 2>/dev/null | grep -c '\[pending\]')" || pending=0
if [ "${pending:-0}" -eq 0 ]; then
  exit 0
fi

# --- 3. delegate, once ----------------------------------------------------
log "no worker alive in the night window, $pending pending — delegating to $LAUNCHER $N (watched run, retirement gate passed)"
# --- 4. checked status ----------------------------------------------------
GOPI_WORKER_REPO="$REPO" "$LAUNCHER" "$N" >> "$LOG" 2>&1
status=$?
if [ "$status" -ne 0 ]; then
  fail_loud "$status"
fi
log "launcher accepted: $N worker(s) via $LAUNCHER (it started no worker itself — the launcher did)"
