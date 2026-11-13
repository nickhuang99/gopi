#!/usr/bin/env bash
# scratch-gc-watchdog.sh — the HOST-LEVEL watchdog face of the suite scratch
# tree's sweep.
#
# WHY THIS FILE EXISTS: the scratch home ($HOME/workspace/gopi-test-scratch) is
# bounded today by two faces that both need something to be running —
#   * the test pin (internal/testutil.PinSuiteTempRoot, inline), and
#   * the resident tick (cmd/agent/scratch_gc_tick.go, on the worker/merger
#     loop).
# A host that runs no suites AND keeps no resident gopi is bounded by neither:
# an orphaned per-run root from a suite killed mid-run would sit there until
# someone ran a test again. This script is the third face — the one an OS
# scheduler drives (systemd --user timer, see
# ~/.config/systemd/user/gopi-scratch-gc-watchdog.{service,timer}) — so the
# tree is bounded by a CLOCK, not by an activity.
#
# WHAT IT DOES: one synchronous `gopi tool scratch-gc` — the one-shot verb whose
# core is internal/scratch.SweepHome, i.e. the SAME sweep (and the same hard
# rule: a directory held by a live process — /proc cwd+fd scan and the
# .holder registration — is never removed). No sweeping logic lives here; this
# file only arranges the environment, a lock and a loud failure path.
#
# HARDENING (all three learned from this host's own incidents):
#   1. PATH IS EXPLICIT. A systemd --user unit inherits a minimal environment,
#      NOT a login shell's (the scheduler does not source your profile), so
#      "gopi" may not resolve and "go" certainly may not. Everything this
#      script invokes is either an absolute path or a directory named below.
#   2. FAIL LOUD, AND NEVER FALL THROUGH TO THE AGENT. A binary that does not
#      carry the verb (a merge that did not rebuild, Rule 12 — or a PATH hit on
#      an older install) must be REFUSED here, not invoked: an unrecognised
#      first argv word is a PROMPT to gopi's agent loop, so a stale watchdog
#      would quietly start an LLM session every 30 minutes instead of
#      sweeping. The probe below runs the verb with -h (the top-level help
#      scan exits before any agent work) and requires the usage head to BE
#      this verb's; anything else is logged as FAIL with a non-zero exit. The
#      "silent no-op watchdog" is the failure mode this design most has to
#      avoid, and a token-burning one is worse.
#   3. ONE RUN AT A TIME. A sweep whose previous run outlived the interval is
#      serialised by flock — the sweep takes its own lock inside the home, but
#      the LOG would interleave without this one.
#
# EXIT STATUS: 0 when the sweep ran (any removed count, including 0). Non-zero
# only when the watchdog could not do its job — a systemd timer's status is
# the operator's signal.

set -u -o pipefail

readonly REPO_DEFAULT="${HOME}/work/GoPi"
readonly BIN_REL="bin/gopi"
readonly VERB="scratch-gc"
# VERB_DOOR is the FAMILY door the verb is reached through: `gopi tool
# scratch-gc`. Since the verb-retirement epic (2026-10-05, cmd/agent/
# cli_verb_aliases.go) the bare word `gopi scratch-gc` is no longer dispatched —
# it prints a RETIREMENT NOTICE to stderr and exits 0, i.e. it exits ZERO
# without ever sweeping. That is the exact trap hardening 2 exists for: the
# probe below compares the usage head, so a bare-word invocation would fail the
# probe (loud, correct) — but only because the notice is not this verb's usage
# head. Naming the door here keeps the probe and the sweeps on the one spelling
# the binary really dispatches (Rule 1: the value comes from its constant; the
# door word itself is a CLI verb, cli_verbs.go CliVerbTool).
readonly VERB_DOOR="tool"
# TREES_ARM selects the DECLARED-TREES face of the same verb: the trees this
# repository's own commands create and never clean (the GOTMPDIR tree's leftover
# go-build* dirs, the flat raw-log dir, the session-tree *.raw.log, and the
# shared Go build cache the merge/build gate fills per checkout). One verb, two
# faces, ONE lock (below) — never a second watchdog. The face is TREE-AGNOSTIC:
# it walks whatever internal/scratch declares, so a newly declared tree needs no
# change here (only the probe text below, which names them for the operator).
readonly TREES_ARM="trees"
readonly FLAG_HELP="-h"
# VERB_HELP_PREFIX is the shape of the verb's usage head ("gopi tool
# scratch-gc …"), the probe's acceptance token (see hardening 2). It is DERIVED
# from VERB_DOOR + VERB, never typed twice (Rule 13).
readonly VERB_HELP_PREFIX="gopi ${VERB_DOOR} ${VERB}"
readonly LOCK_FILE="${HOME}/.gopi/scratch-gc-watchdog.lock"
readonly LOG_FILE="${HOME}/.gopi/logs/scratch-gc-watchdog.log"
readonly GO_BIN_DIR="/usr/local/go/bin"
readonly SYSTEM_BIN_DIRS="/usr/local/bin:/usr/bin:/bin"

# log <message…> — one timestamped line to stdout (systemd captures it into the
# journal) and to the durable log file (survives journald rotation).
log() {
  local line
  line="$(date -Is) scratch-gc-watchdog $*"
  printf '%s\n' "${line}"
  printf '%s\n' "${line}" >>"${LOG_FILE}" 2>/dev/null || true
}

# resolve_binary — the gopi binary to drive, in preference order:
#   1. $GOPI_SCRATCH_GC_BIN (an operator's explicit override, e.g. a worktree)
#   2. <repo>/bin/gopi — the checkout the merger rebuilds after every merge
#      (Rule 12), so the timer runs the code that is on master
#   3. gopi on PATH — a packaged/system install
resolve_binary() {
  if [[ -n "${GOPI_SCRATCH_GC_BIN:-}" && -x "${GOPI_SCRATCH_GC_BIN}" ]]; then
    printf '%s' "${GOPI_SCRATCH_GC_BIN}"
    return 0
  fi
  local repo="${GOPI_SCRATCH_GC_REPO:-${REPO_DEFAULT}}"
  if [[ -x "${repo}/${BIN_REL}" ]]; then
    printf '%s' "${repo}/${BIN_REL}"
    return 0
  fi
  command -v gopi || return 1
}

# run_face <arm-args…> — one synchronous sweep face; its stdout (the logfmt
# line(s)) is logged to the journal AND the durable log, and a failure is
# REPORTED but never aborts the remaining faces (the home sweep must still run
# when the declared-trees arm hit a problem, and vice versa).
run_face() {
  local out rc
  if out="$("${bin}" "${VERB_DOOR}" "${VERB}" "$@" 2>&1)"; then
    rc=0
  else
    rc=$?
  fi
  printf '%s\n' "${out}" | while IFS= read -r line; do
    [[ -n "${line}" ]] && log "${line}"
  done
  if [[ ${rc} -ne 0 ]]; then
    log "FAIL ${bin} ${VERB_DOOR} ${VERB} $* exited ${rc} — that face of the sweep may not have run"
  fi
  return "${rc}"
}

main() {
  mkdir -p "$(dirname "${LOG_FILE}")" || true

  # Hardening 1: an explicit PATH (and an explicit settings file, so the
  # durable scratch_gc switch is the DEPLOYMENT's, not a stray shell's).
  export PATH="${GO_BIN_DIR}:${SYSTEM_BIN_DIRS}"
  export GOPI_SETTINGS="${GOPI_SETTINGS:-${HOME}/.gopi/settings.json}"

  local bin
  if ! bin="$(resolve_binary)"; then
    log "FAIL no gopi binary found (looked at \$GOPI_SCRATCH_GC_BIN, ${REPO_DEFAULT}/${BIN_REL}, PATH) — the scratch tree is NOT being swept"
    return 1
  fi

  # Hardening 3: serialise runs (and never wait forever on a stuck one).
  exec 9>"${LOCK_FILE}" || {
    log "FAIL cannot open lock ${LOCK_FILE}"
    return 1
  }
  if ! flock -w 60 9; then
    log "FAIL another scratch-gc watchdog run holds ${LOCK_FILE} — skipping this tick"
    return 1
  fi

  # Hardening 2: refuse a binary that does not carry the verb (see above).
  # The probe is a SHAPE check on the usage head — deliberately not a copy of
  # the Go usage prose, which would be a second source of truth (Rule 13).
  #
  # TWO probes, because this script now drives TWO faces of the verb: the bare
  # verb and the DECLARED-TREES arm. The second one is not decoration — a binary
  # that predates that arm (the state of master's bin/gopi until the merge
  # rebuild, Rule 12) would interpret `scratch-gc trees` as an AGENT PROMPT and
  # burn tokens every 30 minutes instead of sweeping. A binary that cannot
  # answer for the arm is refused BEFORE either sweep runs.
  local probe_head arm_head
  probe_head="$("${bin}" "${VERB_DOOR}" "${VERB}" "${FLAG_HELP}" 2>/dev/null | head -1)"
  if [[ "${probe_head}" != "${VERB_HELP_PREFIX}"* ]]; then
    log "FAIL ${bin} does not carry the '${VERB_DOOR} ${VERB}' verb (usage head: \"${probe_head}\") — refusing to invoke it (an unknown verb would be treated as an agent prompt). The scratch tree is NOT being swept; rebuild the repo binary."
    return 1
  fi
  # The FACES a binary can carry are a different matter from the verb: a binary
  # that predates the declared-trees arm (master's bin/gopi until the merge
  # rebuild, Rule 12) must NOT be handed that argv — an unrecognized word is a
  # prompt — but it must also not cost the host the face it CAN run. So the arm
  # probe DEGRADES LOUDLY rather than failing closed: the home sweep still runs,
  # and the log says exactly which trees are unguarded meanwhile. Failing closed
  # here would have STOPPED an existing protection in order to announce a
  # missing one — the worse of the two failures.
  local arm_head trees_arm_ok
  trees_arm_ok=1
  arm_head="$("${bin}" "${VERB_DOOR}" "${VERB}" "${TREES_ARM}" "${FLAG_HELP}" 2>/dev/null | head -1)"
  if [[ "${arm_head}" != "${VERB_HELP_PREFIX}"* ]]; then
    trees_arm_ok=0
    log "FAIL ${bin} does not carry the '${VERB_DOOR} ${VERB} ${TREES_ARM}' arm (usage head: \"${arm_head}\") — that face is SKIPPED (invoking it would be treated as an agent prompt). The DECLARED TREES (GOTMPDIR tree / raw / session raw.log / shared build cache) are NOT being swept; rebuild the repo binary. The scratch-home face below still runs."
  fi

  # The sweeps themselves: two synchronous one-shots (the suite scratch HOME,
  # then the DECLARED TREES), each reporting its own logfmt line. The declared
  # face echoes each line it printed by construction, so the durable log always
  # says WHICH trees were visited even when a sweep failed.
  local rc
  rc=0
  run_face || rc=$?
  if [[ ${rc} -eq 0 && ${trees_arm_ok} -eq 1 ]]; then
    run_face "${TREES_ARM}" || rc=$?
  fi

  if [[ ${rc} -ne 0 ]]; then
    # Hardening 2: a binary that does not know the verb (stale bin after a
    # merge, Rule 12) or a failed sweep is a LOUD, non-zero event.
    log "FAIL ${bin} ${VERB} exited ${rc} — the scratch tree may not have been swept"
    return "${rc}"
  fi
  return 0
}

main "$@"
