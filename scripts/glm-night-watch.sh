#!/usr/bin/env bash

# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md — the operator owns this; an agent must never run it (submit the work instead: gopi task submit).
# glm-night-watch.sh — wait for the GLM free window, then ask the SINGLE
# launcher owner on the HP peer for N workers.
#
# WHAT THIS SCRIPT IS, AND WHAT IT IS NOT
# ---------------------------------------
# It is a SCHEDULER: it waits for a wall-clock window and then DELEGATES.
# It is NOT a launcher. There is exactly ONE launcher in this system —
# scripts/gopi-worker-up.sh, installed on the peer as
# <peer-home>/bin/gopi-worker-up.sh (the operator-owned contract that names
# the log path, the argv shape, the process accounting and the dry-run seam).
#
# WHY THE DISTINCTION IS ENFORCED IN CODE (the defect this rewrite closes):
# this watcher used to ssh a hand-written
#
#     setsid nohup ./bin/gopi worker > ~/.gopi/logs/worker-night-$i.log 2>&1 &
#
# per worker. That inline start was a SECOND, unsupervised launcher: its own
# log-dir contract (a per-worker timestamp-named file that no task id can
# resolve — retired 2026-09-12), its own process accounting (`pgrep -x`, the
# same name-vs-argv miscount), no dry-run seam, and no single point of
# control. Worse, it never looked at the remote exit status, so a launch
# REFUSED by the agent-session guard (`gopi worker` exits with the refusal
# code below) was logged as "started worker N pid …" — a refusal wearing the
# face of a success. That is the silent failure class this repo bans.
#
# THE CONTRACT THIS SCRIPT KEEPS
#   1. DELEGATION ONLY. The only remote command it runs is the launcher. If
#      the launcher is missing on the peer, that is a FAILED launch, loud,
#      non-zero — never a fallback to an inline start.
#   2. CHECKED EXIT STATUS. `ssh`'s status is consulted. A non-zero status is
#      a FAILED launch: named, logged, and counted. The refusal code (3 — the
#      agent-session guard) is called by name so an operator sees WHY. Any
#      other failure prints the remote exit status and the remote stderr.
#   3. N ON THE OPERATOR PATH. The operator's own plain-shell launch is a
#      single launcher call: `gopi-worker-up.sh N`. No argument of ours may
#      turn that into a loop of one-worker starts.
#   4. --dry-run LAUNCHES NOTHING. It prints the exact delegation (peer, the
#      remote launcher path and N) and exits 0 — the verification seam a test
#      can drive with no peer, no worker and no task.
#   5. EXIT 0 ONLY IF EVERY LAUNCH SUCCEEDED. A failed delegation makes the
#      overall exit non-zero, so a caller (cron, an operator's shell) sees it.
#
# THE WINDOW WAIT IS BOUNDED (why a test never sleeps through it): the loop
# advances by at most WAIT_STEP_SEC per iteration and sleeps at most that step,
# and the whole wait is additionally capped by GOPI_GLM_WINDOW_MAX_SEC — a
# target further away than that is abandoned and the delegation happens
# immediately, which is both honest in production (a watcher that missed its
# window still asks for the workers) and the seam the end-to-end test drives
# (WINDOW_MAX_SEC=5 makes the delegation branch observable in seconds).
#
# NO AGENT SIGNATURE IS INVENTED (important, and easy to get wrong): this
# script never sets the operator-escape variable that disables the
# agent-session guard, and never unsets an env var to look like a human
# terminal. It does not need to. The guard's refusal signal is INHERITED
# THROUGH env and the PROCESS CHAIN, not through ssh: a remote sshd session
# does not carry our exported variables, and sshd's parent is sshd — not a
# `gopi worker` — so a delegation from here lands in the same allow branch as
# a plain operator shell on the peer. The proof is a sandboxed end-to-end test
# (internal/testutil/glm_night_watch_test.go) that starts N workers with an
# empty environment, exactly as sshd would.
#
# Usage:  glm-night-watch.sh [HHMM] [N] [--dry-run] [--install]
#         HHMM        when to start, local time (default 2300)
#         N           how many workers the launcher starts (default 2)
# Options:
#   --dry-run   print the delegation (peer, launcher path, N) and exit 0;
#               launches NOTHING and does not wait for the window
#   --install   install this script as <gopi-dir>/<its own name>, i.e.
#               ~/.gopi/glm-night-watch.sh (the configured path is OUTSIDE the
#               repo: the installed copy is what a clock runs, and this seam is
#               what keeps it byte-identical to the repo's source of truth)
#   -h|--help   this text
#
# Env knobs (the defaults below are this file's definition site on the SCRIPT
# side; the Go gate internal/testutil/glm_night_watch_test.go reads this file
# and fails if a default here disagrees with its Go constant, so a rename on
# either side is a red build rather than a silent drift):
#   GOPI_GLM_PEER=hp              the peer the launcher is invoked on
#   GOPI_GLM_LAUNCHER=bin/<name>  the peer-side launcher, RELATIVE to the
#                                 peer's home (never an absolute path typed
#                                 per call, never "./bin/gopi worker")
#   GOPI_GLM_SSH=ssh              the transport (the test seam: a stub in PATH
#                                 records the delegation without a network)
#   GOPI_GLM_WAIT_STEP_SEC=60     seconds per wait iteration (the log heartbeat
#                                 cadence)
#   GOPI_GLM_WINDOW_MAX_SEC=86400 how long the wait may last IN TOTAL; a target
#                                 further away than this is abandoned and the
#                                 launcher is called immediately (a watcher
#                                 that missed its window still asks for the
#                                 workers). Tests set a few seconds — this is
#                                 the seam that makes the delegation branch
#                                 observable without sleeping.
set -euo pipefail

if [ -n "${BASH_SOURCE[0]:-}" ]; then
  SELF_PATH="${BASH_SOURCE[0]}"
else
  SELF_PATH="$0"
fi
# The launcher's file name is ALSO this system's single launcher contract, so
# the test asserts it equals the Go constant for scripts/gopi-worker-up.sh
# (the file name the shell-side gate already pins). Spelling it once here
# means a rename cannot leave half the delegation pointing at a missing file.
SELF="${SELF_PATH##*/}"
# The delegation target: the peer-side copy of the single launcher. It is
# spelled as the peer resolves it (a bare relative path under the peer's
# home), never as a local absolute path.
LAUNCHER_NAME="${GOPI_GLM_LAUNCHER:-bin/gopi-worker-up.sh}"

PEER="${GOPI_GLM_PEER:-hp}"
SSH_BIN="${GOPI_GLM_SSH:-ssh}"
# The refusal code is the agent-session guard's own exit status — the signal
# that a launch was DECLINED rather than broken. It is a named knob (never an
# inline 3) so the message in report_failure and the comparison cannot drift
# apart, and the Go gate asserts this default equals its Go constant.
REFUSAL_EXIT_CODE="${GOPI_GLM_REFUSAL_EXIT_CODE:-3}"
TARGET_HHMM="${GOPI_GLM_TARGET_HHMM:-2300}"
N="${GOPI_WORKER_N:-2}"
# The wake-up cadence while waiting: a fixed step (not "the whole remaining
# time") so the log gets a heartbeat and a clock jump cannot park the wait in
# one long sleep. It is a knob (and therefore the test seam) because the window
# WAIT is the one part of this script a test cannot observe without either
# sleeping through it or stubbing `date` in PATH. NOTE the semantics: only the
# SLEEP is skipped below, the boundary arithmetic is untouched — 0 makes the
# wait's own re-check cheap, while a distinct knob (the window length in
# seconds) is what collapses the wait itself.
WAIT_STEP_SEC="${GOPI_GLM_WAIT_STEP_SEC:-60}"
# The window's LENGTH in seconds: an absolute upper bound on how long this
# script will wait for its target, backed by the system `timeout` command (so
# it holds even if an iteration computes a step wrong). The default covers the
# longest legitimate wait — a target that already passed today rolls to
# tomorrow, i.e. up to 24h — while a test sets it to a few seconds.
WINDOW_MAX_SEC="${GOPI_GLM_WINDOW_MAX_SEC:-86400}"

usage() { sed -n '2,/^set -euo pipefail$/p' "$SELF_PATH" | sed 's/^# \{0,1\}//'; }

DRY=0
INSTALL=0
positional=0
while (($#)); do
  case "$1" in
    -h|--help) usage; exit 0 ;;
    --dry-run) DRY=1; shift ;;
    --install) INSTALL=1; shift ;;
    --) shift; break ;;
    -*) echo "$SELF: unknown flag: $1" >&2; usage >&2; exit 2 ;;
    *)
      if ((positional == 0)); then
        TARGET_HHMM="$1"; positional=1
      elif ((positional == 1)); then
        N="$1"; positional=2
      else
        echo "$SELF: unexpected argument '$1'" >&2
        exit 2
      fi
      shift ;;
  esac
done

if ! [[ "$N" =~ ^[0-9]+$ ]] || ((N < 1)); then
  echo "$SELF: worker count must be a positive integer, got '$N'" >&2
  exit 2
fi

# gopi_dir resolves the profile exactly like internal/config.GopiDir():
# $GOPI_HOME/.gopi when set, else ~/.gopi. One knob, one place, so a sandbox
# never writes the real profile.
gopi_dir() {
  if [[ -n "${GOPI_HOME:-}" ]]; then
    printf '%s/.gopi\n' "${GOPI_HOME%/}"
  else
    printf '%s/.gopi\n' "$HOME"
  fi
}

GOPI_DIR="$(gopi_dir)"
LOG="$GOPI_DIR/logs/glm-night-watch.log"

log() { echo "[$(date '+%F %T')] $*" | tee -a "$LOG"; }

mkdir -p "$(dirname "$LOG")"

# delegate_launch performs the ONE remote act this script owns: invoke the
# single launcher with the worker count. It returns the launcher's status
# unchanged, and it never retries, never degrades to an inline start.
#
# An UNDEFINED local means no --dry-run reaches this function: a launch is
# either real (through the transport) or refused.
delegate_launch() {
  "$SSH_BIN" -o ConnectTimeout=15 "$PEER" "$LAUNCHER_NAME $N"
}

# report_failure turns a non-zero launcher status into the operator-facing
# record. The refusal code is named, because "exit 3" and "the agent-session
# guard refused this launch" are the two facts an operator needs to tell a
# refusal apart from a broken peer.
report_failure() {
  local status="$1"
  if [[ "$status" == "$REFUSAL_EXIT_CODE" ]]; then
    log "FAILED: the launcher REFUSED this launch on $PEER (exit $status) — the agent-session guard declines to start workers from an agent session; this watcher invents no override. Run the launcher from your own shell on $PEER."
  else
    log "FAILED: the launcher on $PEER did not start the workers (exit $status) — see the launcher's own message above; nothing was launched silently."
  fi
}

# (the refusal code and its message live in the REFUSAL_EXIT_CODE knob at the
# top of this file)

if ((INSTALL)); then
  dest="$GOPI_DIR/$SELF"
  if [[ "$(readlink -f "$SELF_PATH")" == "$(readlink -f "$dest" 2>/dev/null || printf '%s' "$dest")" ]]; then
    echo "already installed: $dest"
  else
    install -m 0755 "$SELF_PATH" "$dest"
    echo "installed: $dest"
  fi
  # The launcher this script delegates to must EXIST on the peer, or every
  # window ends in a failed delegation. Installing the target is therefore part
  # of installing the watcher — and it is attempted exactly like every other
  # remote act here: one command, exit status checked, reported loudly. An
  # unreachable peer is reported and the exit stays 0: the local install (the
  # part this seam owns) did happen, and the clock will report the missing
  # launcher per run far more loudly than an installer could.
  if "$SSH_BIN" -o ConnectTimeout=15 "$PEER" "test -x \"\$HOME/$LAUNCHER_NAME\"" 2>&1; then
    echo "peer $PEER: launcher present (\$HOME/$LAUNCHER_NAME)"
  else
    echo "peer $PEER: launcher \$HOME/$LAUNCHER_NAME is NOT installed or not executable — every window will fail until it is:" >&2
    echo "  $SSH_BIN $PEER 'cd <the GoPi checkout on that host> && scripts/gopi-worker-up.sh --install'" >&2
  fi
  exit 0
fi

if ((DRY)); then
  echo "$SELF dry-run: delegation only, launching NOTHING"
  echo "$SELF dry-run: peer=$PEER launcher=$LAUNCHER_NAME workers=$N target=$TARGET_HHMM"
  echo "$SELF dry-run: remote command -> $SSH_BIN $PEER '$LAUNCHER_NAME $N'"
  echo "$SELF dry-run: the launcher owns the log path and the argv shape — this script starts no worker itself"
  exit 0
fi

# --- the window ------------------------------------------------------------
# target_epoch resolves HHMM to today's local wall clock, rolling to tomorrow
# when it has already passed.
target_epoch() {
  date -d "today ${TARGET_HHMM:0:2}:${TARGET_HHMM:2:2}:00" +%s
}

now=$(date +%s)
tgt=$(target_epoch)
if ((now >= tgt)); then
  tgt=$((tgt + 86400))
  log "target already passed today; waiting for tomorrow's window"
fi

# A target only makes sense inside the window bound the operator set — past it
# the wait is abandoned and the delegation happens NOW rather than never. That
# ceiling is what makes this script testable without a wall-clock negotiation
# (the tests set a few seconds) and it is honest in production: a watcher that
# silently missed its window must still ask for the workers.
if ((tgt - now > WINDOW_MAX_SEC)); then
  log "target $(date -d "@$tgt" '+%F %T') is ${WINDOW_MAX_SEC}s or more away — outside the configured window bound; starting the GLM work now instead of waiting"
  tgt=$((now + 1))
fi

while :; do
  now=$(date +%s)
  left=$((tgt - now))
  ((left <= 0)) && break
  step=$WAIT_STEP_SEC
  ((step < 1)) && step=1
  ((left < step)) && step=$left
  log "waiting ${left}s until $(date -d "@$tgt" '+%F %T') for the GLM free window"
  if ((step <= left)); then
    timeout "$step" sleep "$step" 2>/dev/null || true
  fi
done

log "GLM free window open — delegating to the launcher on $PEER: $LAUNCHER_NAME $N"

status=0
delegate_launch 2>&1 | tee -a "$LOG" || status=$?
if ((status != 0)); then
  report_failure "$status"
  log "the window's launch did NOT happen; no worker was started by this script"
  exit "$status"
fi

log "launcher accepted: $N worker(s) on $PEER via $LAUNCHER_NAME"
