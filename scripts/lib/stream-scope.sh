# shellcheck shell=bash
# stream-scope.sh — the SHARED predicate behind "this stream carries a package
# the run did not ask for".
#
# WHY IT IS A LIBRARY AND NOT FIVE LINES INSIDE test-budget.sh
# ------------------------------------------------------------
# The defect this guards (2026-09-19) was a shared write target: every budget
# run redirected `go test -json` into the SAME host-wide file, so one run's
# summary was rendered from a stream holding a neighbour's events (measured: one
# file with 2422 cmd/agent events followed by 1328 internal/tools events). A
# reporter inside the script would only be exercised by a real concurrent
# accident — i.e. never on purpose. As a library, the predicate is callable with
# synthetic inputs, so the gate (internal/testutil testbudget_gate_test.go)
# asserts the mingled case directly instead of hoping for one.
#
# The functions take FILE PATHS, not "the current run": a caller decides what to
# compare, and the comparison is a pure function of two files.
#
# Usage (source, do not execute):
#     . "$(dirname "$0")/lib/stream-scope.sh"
#     gopi_scope_packages "$module" ./internal/tools/ > scope.txt
#     gopi_stream_packages "$stream" > stream.txt
#     gopi_stream_scope_anomalies "$module" scope.txt stream.txt

# gopi_stream_packages STREAM — the distinct Package values the JSON stream
# carries, one per line. Read with grep -a: a stream can hold a NUL byte (a test
# feeding binary data to a tool) and plain grep would then call the whole file
# binary and print nothing (Rule 15 — the silent empty answer).
gopi_stream_packages() {
    local stream="${1:?gopi_stream_packages needs a stream path}"
    grep -a -o -- '"Package":"[^"]*"' "$stream" 2>/dev/null |
        sed 's/^"Package":"//; s/"$//' | sort -u
}

# gopi_scope_packages MODULE PATTERN... — the import paths the patterns address,
# one per line. `./...` (and the bare module root) is the whole module: its
# scope is MODULE itself, of which every package in the module is a descendant.
# Flags and their values are stepped over, so
# `gopi_scope_packages M -run TestX ./a/` yields M/a, not M/TestX.
gopi_scope_packages() {
    local module="${1:?gopi_scope_packages needs the module import path}"
    shift
    local a skip_next=0
    for a in "$@"; do
        if [ "$skip_next" -eq 1 ]; then
            skip_next=0
            continue
        fi
        case "$a" in
            -run | -bench | -timeout | -count | -coverprofile | -cpu | -parallel | -list | -vet | -tags)
                skip_next=1
                continue
                ;;
            -* | "") continue ;;
        esac
        a="${a%/...}"
        a="${a%/}"
        case "$a" in
            "" | ".")
                printf '%s\n' "$module"
                ;;
            *)
                printf '%s\n' "$module/${a#./}"
                ;;
        esac
    done | sort -u
}

# gopi_stream_scope_anomalies MODULE SCOPE_FILE STREAM_FILE — the packages in
# STREAM_FILE that no entry of SCOPE_FILE covers, one per line (empty = the
# stream is within scope).
#
# "Covers" = the scope entry is the package itself or an ancestor directory of
# it: `M/internal/tools` covers `M/internal/tools/sub`, and a `./internal/tools/`
# scope must not be flagged for the packages under it. Packages from ANOTHER
# MODULE are skipped — another module's package is outside every scope by
# definition (that is the module boundary, not a shared stream), and a caller
# (the smoke gate) legitimately executes this pipeline from a fixture module.
gopi_stream_scope_anomalies() {
    local module="${1:?gopi_stream_scope_anomalies needs the module import path}"
    local scope_file="${2:?gopi_stream_scope_anomalies needs a scope file}"
    local stream_file="${3:?gopi_stream_scope_anomalies needs a stream file}"
    if [ ! -s "$scope_file" ] || [ ! -s "$stream_file" ]; then
        return 0
    fi
    awk -v module="$module" '
        NR == FNR { scope[FNR] = $0; n = FNR; next }
        {
            if ($0 != module && index($0, module "/") != 1) next
            for (i = 1; i <= n; i++) {
                if (scope[i] == $0) next
                if (index($0, scope[i] "/") == 1) next
            }
            print $0
        }
    ' "$scope_file" "$stream_file"
}
