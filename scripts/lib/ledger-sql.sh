#!/usr/bin/env bash
# ledger-sql.sh — the ZERO-DEPENDENCY way a shell script reads or writes a gopi
# ledger (`memory.db`).
#
# WHY THIS EXISTS. Scripts in this repo used to shell out to the `sqlite3` command
# line. It is not installed on this host (GOPI.md §3), and a script that keeps
# depending on it fails in the worst possible shape: the query returns NOTHING,
# the script reads that silence as "there is no data", and the verdict it prints
# is about something else entirely (Rule 15 — silent failure). Adding an external
# command the repo must have installed is also a standing dependency this project
# does not take (Rule 35). So the channel is Go: `scripts/lib/ledgersql` is a
# tiny `database/sql` program over modernc.org/sqlite — ALREADY a module
# dependency, pure Go, no CGO — compiled on demand into the calling script's
# scratch root.
#
# USAGE
#
#   . scripts/lib/ledger-sql.sh
#   gopi_ledgersql_ensure "$repo_root" "$scratch_root" || exit 2   # sets GOPI_LEDGERSQL_BIN
#   gopi_ledgersql --read-only --db "$DB" "SELECT status FROM tasks WHERE task_id='x'"
#   gopi_ledgersql --db "$DB" "INSERT INTO tasks (…) VALUES (…)"
#
# A failure is OURS: `gopi_ledgersql` returns the helper's non-zero status and
# its message goes to stderr. The CALLER must act on it (abort with a reason) —
# never `2>/dev/null` it away, which is exactly how the old code turned "no
# sqlite3 on this host" into "every branch is unexplained".
#
# RULE 22: the scratch root (and the go tool's own GOTMPDIR inside it) is
# measured against `/`, never spelled — this refuses when they are the same
# filesystem.
#
# The functions are prefixed and this file is sourced; it deliberately does NOT
# run `set -e` (the caller owns its own shell options).

# Where the Go source lives, relative to the repo root the caller passes.
GOPI_LEDGERSQL_SRC_REL="scripts/lib/ledgersql"
# The executable the Go build produces.
GOPI_LEDGERSQL_NAME="ledgersql"
# The exported path of the compiled helper; empty until gopi_ledgersql_ensure.
GOPI_LEDGERSQL_BIN=""

# gopi_ledgersql_fail MESSAGE... — one shape for every refusal here: the words
# `ledger-sql:` so a reader can grep them, then the reason, on stderr.
gopi_ledgersql_fail() {
    echo "ledger-sql: $*" >&2
}

# gopi_ledgersql_ensure REPO_ROOT SCRATCH_ROOT — compile (or reuse) the helper
# under SCRATCH_ROOT and export its path in GOPI_LEDGERSQL_BIN.
#
# Returns 2 with a stated reason when: the Go source is missing, the scratch root
# is on the `/` partition (Rule 22), the `go` toolchain is absent, or the build
# itself fails. A rebuild is needed only when the binary is missing or older than
# the source, so a cohort loop pays the cost once.
gopi_ledgersql_ensure() {
    local repo_root="${1:?gopi_ledgersql_ensure needs the repo root}"
    local scratch_root="${2:?gopi_ledgersql_ensure needs a scratch root}"
    local src="$repo_root/$GOPI_LEDGERSQL_SRC_REL"
    local build_root="$scratch_root/$GOPI_LEDGERSQL_NAME"
    local bin="$build_root/$GOPI_LEDGERSQL_NAME"

    if [ ! -f "$src/ledgersql.go" ]; then
        gopi_ledgersql_fail "no Go source at $src/ledgersql.go — cannot build the ledger channel"
        return 2
    fi

    # Rule 22: the build output and the go tool's temp files must NOT land on `/`.
    local root_dev scratch_dev
    root_dev="$(stat -c %d / 2>/dev/null)"
    mkdir -p "$build_root" || { gopi_ledgersql_fail "cannot create $build_root"; return 2; }
    scratch_dev="$(stat -c %d "$build_root" 2>/dev/null)"
    if [ -z "$root_dev" ] || [ -z "$scratch_dev" ] || [ "$root_dev" = "$scratch_dev" ]; then
        gopi_ledgersql_fail "$build_root is on the / partition (device ${scratch_dev:-unknown}) — refusing (Rule 22)"
        return 2
    fi

    if [ -x "$bin" ] && [ "$bin" -nt "$src/ledgersql.go" ]; then
        GOPI_LEDGERSQL_BIN="$bin"
        return 0
    fi

    if ! command -v go >/dev/null 2>&1; then
        gopi_ledgersql_fail "the go toolchain is not on PATH — it is the ledger channel now that sqlite3 is not available"
        return 2
    fi

    # GOTMPDIR/GOCACHE stay inside the scratch root for the same Rule 22 reason.
    # GOTMPDIR must EXIST — `go build` fails outright ("creating work dir") otherwise.
    # The build log is a FIXED path inside that scratch root, not a mktemp: the
    # scripts gate requires anything creating scratch of its own to route through
    # scripts/lib/tmp-ban.sh, and this file needs no uniqueness (the path is
    # already per-run and outside the source tree).
    mkdir -p "$build_root/gotmp" || { gopi_ledgersql_fail "cannot create $build_root/gotmp"; return 2; }
    local build_log="$build_root/build.log"
    if ! ( cd "$repo_root" && \
           GOTMPDIR="$build_root/gotmp" \
           GOFLAGS="-mod=mod" \
           CGO_ENABLED=0 \
           go build -o "$bin" "./$GOPI_LEDGERSQL_SRC_REL/" ) >"$build_log" 2>&1; then
        gopi_ledgersql_fail "go build ./$GOPI_LEDGERSQL_SRC_REL/ failed (log: $build_log)"
        sed -n '1,20p' "$build_log" >&2
        return 2
    fi
    rm -f "$build_log"

    if [ ! -x "$bin" ]; then
        gopi_ledgersql_fail "go build reported success but $bin is not executable"
        return 2
    fi
    GOPI_LEDGERSQL_BIN="$bin"
    return 0
}

# gopi_ledgersql [--read-only] --db PATH SQL... — run ONE statement through the
# helper. The caller decides what a non-zero status means; nothing is swallowed
# here on purpose.
gopi_ledgersql() {
    if [ -z "$GOPI_LEDGERSQL_BIN" ] || [ ! -x "$GOPI_LEDGERSQL_BIN" ]; then
        gopi_ledgersql_fail "no helper binary — call gopi_ledgersql_ensure first"
        return 2
    fi
    "$GOPI_LEDGERSQL_BIN" "$@"
}
