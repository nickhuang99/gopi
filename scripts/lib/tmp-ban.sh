# shellcheck shell=bash
# tmp-ban.sh — the shared scratch-root contract for GoPi dev/test scripts.
#
# WHY THIS FILE EXISTS: on this host `/tmp` lives on the 147G `/` partition
# (87% full). The 2026-09-11 exhaustion (devdoc/peerbox-resource-exhaustion-20260911.md)
# came from an unbounded `while :; do dd ... conv=fsync; done` stress loop
# writing into /tmp while test suites abandoned 13,044 scratch directories
# there — `/` went from 19G to 1.5G free with processes stuck in D state.
#
# THE CONTRACT every script sourcing this file gets:
#   1. scratch data lives under <home>/workspace/gopi-test-scratch/queue/ —
#      the SAME tree the Go suites use (internal/testutil/scratchroot.go), so
#      one GC sweep (GCStaleScratchRoot) bounds both;
#   2. the root filesystem is asserted, not assumed: gopi_assert_off_root_fs
#      fails the script when a path shares a device with `/` (the Python side
#      is scripts/lib/tmp_ban.py);
#   3. a loop that could run forever is bounded by TIME and by ITERATION COUNT
#      (gopi_bounded_loop) — a `timeout`-wrapped loop is the floor, never the
#      ceiling: an iteration cap is what stops a fast loop from spinning.
#
# Usage (source, do not execute):
#     . "$(dirname "$0")/lib/tmp-ban.sh"
#     WORK="$(gopi_scratch_mkdir merge-e2e)"   # under ~/workspace, never /tmp
#     trap gopi_scratch_cleanup EXIT INT TERM  # children + scratch removed
#
# NOTE: sourcing this file deliberately does NOT change the caller's shell
# options (`set -e/-u/pipefail` stay the caller's business) — an inherited
# option is a hidden behavior change in a script that did not ask for it.

# gopi_x11_socket_dir prints the X11 socket directory. It is the ONE path under
# /tmp this repository keeps, and it is kept for the same reason
# internal/shot/x11.go keeps it in Go: it is not scratch — a unix SOCKET in it is
# created by the X server itself, never written by us, so the "off / by device id"
# rule (Rule 22) does not apply to it. It is defined HERE, once, so no probe
# script has to spell it and each one reaches it through the same accessor
# (Rule 14: a shared value lives in one place, in a package every consumer can
# reach).
gopi_x11_socket_dir() {
    printf '%s\n' "${GOPI_X11_SOCKET_DIR:-/tmp/.X11-unix}"
}

# gopi_x11_base_dir prints the directory the X server's own lock files live in
# (/tmp/.X99-lock and friends) — the same ONE kept path, one level up.
gopi_x11_base_dir() {
    printf '%s\n' "${GOPI_X11_BASE_DIR:-/tmp}"
}

# gopi_scratch_home prints the scratch home. GOPI_TEST_SCRATCH_HOME (published
# by the Go suites via testutil.ScratchHomeEnvName) wins, then GOPI_TEST_HOME
# (testutil.WorkspaceHomeEnvName). This is the ONLY reader of either variable
# in shell (the single-source rule the Go side follows too).
gopi_scratch_home() {
    if [ -n "${GOPI_TEST_SCRATCH_HOME:-}" ]; then
        printf '%s\n' "$GOPI_TEST_SCRATCH_HOME"
        return 0
    fi
    : "${GOPI_TEST_HOME:=${HOME:?HOME is unset — refusing to guess a scratch root}}"
    printf '%s\n' "${GOPI_TEST_HOME}/workspace/gopi-test-scratch"
}

# gopi_scratch_queue prints the suite-less queue inside the scratch home: the
# only place a script's large throwaway artifacts (load files, DB copies,
# build output) may land. It is age-GC'd by the Go sweeps and is NEVER /tmp.
gopi_scratch_queue() {
    printf '%s\n' "$(gopi_scratch_home)/queue"
}

# gopi_assert_off_root_fs PATH — the mechanical half of the ban: compare the
# device id of PATH with the device id of `/` and FAIL with the fix when they
# match. An unmade path is an error too (never "assume safe").
gopi_assert_off_root_fs() {
    local path="$1" dev root_dev
    if [ ! -d "$path" ]; then
        echo "tmp-ban: $path is not a directory — cannot verify it is off /" >&2
        return 1
    fi
    dev="$(stat -c %d "$path")" || return 1
    root_dev="$(stat -c %d /)" || return 1
    if [ "$dev" = "$root_dev" ]; then
        cat >&2 <<EOF
tmp-ban: scratch path $path is on the SAME filesystem as / (device $dev).
  /tmp is that filesystem on this host: the 147G partition at 87% full whose
  exhaustion (2026-09-11) is why GoPi bans it as a test/dev workspace.
  FIX: use gopi_scratch_mkdir / gopi_scratch_queue (they land under
  $(gopi_scratch_home)), or unset TMPDIR if it points at /tmp.
EOF
        return 1
    fi
    return 0
}

# gopi_scratch_registry prints the per-shell cleanup registry FILE.
#
# WHY A FILE AND NOT A VARIABLE: callers must be able to write
# `WORK="$(gopi_scratch_mkdir sub)"` — and a command substitution runs in a
# SUBSHELL, so a variable assignment made inside gopi_scratch_mkdir is lost in
# the parent. With a variable-only registry every caller leaked its scratch
# directory (measured: the self-test left queue-selftest-<pid> behind) and
# only the age GC ever collected it. Appending to a file survives the
# subshell, so the trap really removes what the script created.
gopi_scratch_registry() {
    printf '%s\n' "${GOPI_SCRATCH_REGISTRY:-$(gopi_scratch_queue)/.cleanup-$$}"
}

# gopi_scratch_register DIR — record DIR for trap-time removal (idempotent).
gopi_scratch_register() {
    local dir="$1" reg
    [ -n "$dir" ] || return 0
    reg="$(gopi_scratch_registry)"
    if ! printf '%s\n' "$dir" >>"$reg" 2>/dev/null; then
        # The registry is a convenience on top of the age GC, never a reason
        # to fail the caller's work — report it, then continue (Rule 15b).
        echo "tmp-ban: cannot write cleanup registry $reg — the age GC will collect $dir" >&2
    fi
}

# gopi_scratch_mkdir SUB — create a per-run scratch directory under the queue,
# asserted off `/`, registered for cleanup, and printed on stdout
# (queue-<sub>-<pid>: the name the Go GC recognises as a queue entry).
gopi_scratch_mkdir() {
    local sub="${1:?gopi_scratch_mkdir needs a sub-name}"
    local queue dir
    queue="$(gopi_scratch_queue)"
    mkdir -p "$queue" || return 1
    gopi_assert_off_root_fs "$queue" || return 1
    dir="$queue/queue-${sub}-$$"
    if ! mkdir -p "$dir"; then
        echo "tmp-ban: cannot create scratch dir $dir" >&2
        return 1
    fi
    GOPI_SCRATCH_DIRS="${GOPI_SCRATCH_DIRS:-} $dir"
    gopi_scratch_register "$dir"
    printf '%s\n' "$dir"
}

# gopi_scratch_load FILE — the path a load generator writes its bulk output
# to: inside a scratch queue directory, never an ambient temp path.
gopi_scratch_load() {
    local name="${1:?gopi_scratch_load needs a file name}"
    local dir
    dir="$(gopi_scratch_mkdir "load")" || return 1
    printf '%s\n' "$dir/$name"
}

# gopi_kill_children — SIGTERM then SIGKILL everything this shell started in
# the background. Children of a stress script hold scratch directories open
# (a dd loop keeps writing after the script dies), so cleanup must reach them
# before the scratch tree is removed.
gopi_kill_children() {
    local kids
    kids="$(jobs -pr 2>/dev/null || true)"
    if [ -z "$kids" ]; then
        # children that outlived their parent shell's job table
        kids="$(pgrep -P $$ 2>/dev/null || true)"
    fi
    [ -z "$kids" ] && return 0
    # shellcheck disable=SC2086  # word splitting is the point: a pid list
    kill -TERM $kids 2>/dev/null || true
    sleep 0.2
    # shellcheck disable=SC2086
    kill -KILL $kids 2>/dev/null || true
    # shellcheck disable=SC2086
    wait $kids 2>/dev/null || true
}

# gopi_scratch_cleanup — the trap body: kill children FIRST (they may still be
# writing into the scratch dir), then remove every directory this script
# registered. Safe to call more than once and when nothing was created.
gopi_scratch_cleanup() {
    gopi_kill_children
    local dir reg
    reg="$(gopi_scratch_registry)"
    # Both channels: the in-shell variable (same-shell callers) and the
    # registry file (callers that captured the path through a subshell).
    for dir in ${GOPI_SCRATCH_DIRS:-}; do
        [ -n "$dir" ] || continue
        rm -rf "$dir"
    done
    if [ -f "$reg" ]; then
        while IFS= read -r dir; do
            [ -n "$dir" ] || continue
            rm -rf "$dir"
        done <"$reg"
        rm -f "$reg"
    fi
    GOPI_SCRATCH_DIRS=""
}

# gopi_bounded_loop SECONDS ITERATIONS CMD... — run CMD in a loop bounded by
# BOTH a hard wall-clock deadline and a maximum iteration count, so neither a
# fast spin (millions of iterations per second) nor a slow stall can run away.
#
# The loop is a `for` with an explicit counter (an iteration cap is
# structural, not a timeout) and the whole body is wrapped in `timeout`, which
# is also what the scripts static-check gate looks for.
gopi_bounded_loop() {
    local seconds="${1:?gopi_bounded_loop needs SECONDS}" iters="${2:?gopi_bounded_loop needs ITERATIONS}"
    shift 2
    if [ "$#" -eq 0 ]; then
        echo "tmp-ban: gopi_bounded_loop needs a command" >&2
        return 1
    fi
    local i
    for ((i = 0; i < iters; i++)); do
        timeout "$seconds" "$@" || return $?
    done
    return 0
}

# gopi_require_tools NAME... — fail fast with a clear message when a script's
# external dependencies (sqlite3, jq, ...) are missing, instead of failing
# halfway through with a confusing error.
gopi_require_tools() {
    local tool
    for tool in "$@"; do
        if ! command -v "$tool" >/dev/null 2>&1; then
            echo "tmp-ban: required tool '$tool' not found in PATH" >&2
            return 2
        fi
    done
    return 0
}

# gopi_dry_run_requested ARGS... — true when --dry-run is among the script's
# arguments. Every destructive script carries the flag so its plan can be
# inspected without touching anything.
gopi_dry_run_requested() {
    local arg
    for arg in "$@"; do
        case "$arg" in
            --dry-run | -n) return 0 ;;
        esac
    done
    return 1
}

# gopi_dry_run_report NAME SCRATCH DESC [REQUIRED_TOOL...] — the body of a
# --dry-run: print the scratch root (already created AND asserted off `/` by
# the caller, so the report proves the path is usable), the planned work, and
# the dependency preflight. Nothing else runs.
gopi_dry_run_report() {
    local name="$1" scratch="$2" desc="$3"
    shift 3
    echo "[dry-run] $name"
    echo "  scratch root : $scratch (off the / filesystem: verified)"
    echo "  would do     : $desc"
    if ! gopi_require_tools "$@"; then
        echo "[dry-run] FAILED prerequisites" >&2
        return 2
    fi
    echo "[dry-run] prerequisites OK — nothing was executed"
    return 0
}
