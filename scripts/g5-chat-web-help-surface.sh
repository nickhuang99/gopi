#!/usr/bin/env bash
# g5-chat-web-help-surface.sh — the RE-DERIVATION command behind
# devdoc/gopi-chat-web-help-surface-inventory.md (G5 of the help-surface
# hardening, briefings/brief-help-surface-hardening.md).
#
# WHY A SCRIPT AND NOT A HAND-WRITTEN TABLE. The briefing's deliverable is an
# inventory of "every verb/flag the chat/UI + web-tool group's help advertises".
# A table typed by hand describes the help as it was on the day somebody read
# it; this one is derived from the binary's OWN usage text every time it runs,
# and cmd/agent/cli_chat_web_help_surface_basic_test.go re-derives the same list
# in Go (g5DeriveFlags) so a surface or flag added to the help without a row —
# or a row claiming something the help no longer prints — fails the build rather
# than leaving the table stale.
#
# WHAT IS DERIVED, AND FROM WHERE:
#   * the surface list = the surfaces named in the derive calls below, each read from
#     `gopi <surface> -h` (the binary's own text).
#   * the flag set     = every `--flag` token on a line whose shape is the
#     SURFACE'S, i.e. one of the three the scanner knows:
#         - a synopsis HEAD        `^ {0,2}gopi …`
#         - a bracket CONTINUATION `^ {18,}\[`
#         - a flag DEFINITION line `^ {0,2}-{1,2}NAME[ ,-]+[A-Za-z<{]`
#     Prose paragraphs quote flags too (webFetchUsage's description of its own
#     JSON payload names `--max-chars`), and a whole-page grep over-reports
#     exactly those. `--help` is excluded by name: it is the universal help
#     spelling a reader already typed, not a knob of the surface.
#   * the SCOPE column: `gopi chat list` and `gopi chat backfill` share one page
#     (chatListUsage), so each is scanned from its OWN synopsis head — a
#     page-wide scan would attribute each one the other's flags.
#
# USAGE
#   scripts/g5-chat-web-help-surface.sh [path-to-gopi]
# Default binary: ./bin/gopi
#   (build it first: CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ )
#
# OUTPUT (TSV, one row per surface, tab-separated):
#   surface <TAB> flag1,flag2,… <TAB> flag count
# Tab-separated so a reviewer can diff it against the inventory table without a
# Markdown-aware tool.
set -euo pipefail

BIN="${1:-./bin/gopi}"
if [ ! -x "$BIN" ]; then
    echo "g5-chat-web-help-surface.sh: $BIN is not executable — build it with" >&2
    echo "  CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/" >&2
    exit 1
fi

derive() { # derive <surface> [scope-verb]
    local surface="$1" scope="${2:-}" help
    # shellcheck disable=SC2086 # a surface is deliberately word-split into argv
    help="$("$BIN" $surface -h 2>&1)"
    printf '%s\n' "$help" | awk -v surface="$surface" -v scope="$scope" '
        function collect(s,   n, parts, i, f) {
            n = split(s, parts, /--/)
            for (i = 2; i <= n; i++) {
                if (match(parts[i], /^[a-z][a-z-]*/)) {
                    f = substr(parts[i], 1, RLENGTH)
                    if (f == "help") continue
                    if (index("," flags ",", "," f ",") == 0) flags = (flags == "" ? f : flags "," f)
                }
            }
        }
        function wrap(s) { return (s ~ /^ {18,}\[/) }
        function head(s) { return (s ~ /^ {0,2}gopi /) }
        function defn(s) { return (s ~ /^ {0,2}-{1,2}[A-Za-z][A-Za-z-]*(, *-{1,2}[A-Za-z][A-Za-z-]*)? +[A-Za-z<{]/) }
        function emit(   count, arr, i, j, key, joined) {
            count = split(flags, arr, ",")
            if (count == 1 && arr[1] == "") { printf "%s\t\t0\n", surface; return }
            for (i = 2; i <= count; i++) {
                key = arr[i]; j = i - 1
                while (j >= 1 && arr[j] > key) { arr[j + 1] = arr[j]; j-- }
                arr[j + 1] = key
            }
            joined = ""
            for (i = 1; i <= count; i++) joined = (i == 1 ? arr[i] : joined "," arr[i])
            printf "%s\t%s\t%d\n", surface, joined, count
        }
        scope != "" {
            if ($0 ~ ("^  gopi chat " scope "([ ]|$)")) { in_scope = 1; collect($0); next }
            if (head($0)) { in_scope = 0 }
            if (in_scope && (wrap($0) || defn($0))) collect($0)
            next
        }
        { if (head($0) || wrap($0) || defn($0)) collect($0) }
        END { emit() }
    '
}

# The group's surfaces. For a scoped row the first argument is the argv that
# prints the page (`chat list` → chatListUsage) and the second names the synopsis
# block to read, so a page shared by two rows does not lend one the other's
# flags.
#
# The surface spellings are the ones the help ADVERTISES, read through the
# family address the dispatcher accepts — verb-retirement (2026-10-05) moved
# `gopi proxy` into the tool group, so the row is derived as `gopi tool proxy`;
# `gopi proxy` itself is refused by name. `chat list` is kept in this list on
# purpose even though it has no inventory row any more: its synopsis head is
# what scopes `chat backfill`'s block (the two share chatListUsage), and the
# retired word still has to be derived so the scoped row cannot quietly inherit
# its flags.
derive "chat"
derive "chat list" "list"
derive "chat backfill" "backfill"
derive "web"
derive "web search"
derive "web fetch"
derive "cli"
derive "cli --once"
# The tool group's proxy — NOT `gopi proxy` (retired; that spelling is refused
# by name, so deriving it yields a refusal message and a flagless row).
derive "tool proxy"
