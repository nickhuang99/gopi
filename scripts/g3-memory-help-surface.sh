#!/usr/bin/env bash
# g3-memory-help-surface.sh — the RE-DERIVATION command behind
# devdoc/gopi-memory-help-surface-inventory.md (G3 of the help-surface
# hardening, the `gopi memory` knowledge-base group).
#
# WHY A SCRIPT AND NOT A HAND-WRITTEN TABLE. The deliverable is an inventory of
# "every `gopi memory` verb/flag the live help advertises". A table typed by
# hand describes the help as it was on the day somebody read it; this one is
# derived from the binary's OWN usage text every time it is run, and the Go side
# re-derives the same list in
# cmd/agent/cli_memory_help_surface_basic_test.go (TestMemoryHelpSurfaceMatches
# Inventory), so a verb added to the help without a row — or a row claiming a
# verb the help no longer prints — fails the build rather than leaving the table
# stale.
#
# WHAT IS DERIVED, AND FROM WHERE:
#   * the verb set  = the `gopi memory <verb> …` SYNOPSIS HEADS of
#                     `gopi memory -h` (the group's own page, which the binary
#                     prints from the memoryUsage constant).
#   * the flag set  = every `--flag` spelled inside a verb's synopsis, INCLUDING
#                     its wrapped continuation lines (`memory store` wraps its
#                     optional group onto a second line, `memory update` too).
#
# TWO SHAPES THIS SCRIPT MUST GET RIGHT (both were fumbled on the first pass of
# the G1 sibling, so they are called out here):
#   1. ONE VERB MAY HAVE MORE THAN ONE SYNOPSIS HEAD. `gopi memory provenance`
#      prints twice — the mode flags do not fit on one line — so a scanner that
#      emits on every head reports `provenance` twice (and with half its flags
#      each). Heads with the SAME verb are merged.
#   2. A SYNOPSIS ENDS AT ITS DESCRIPTION BLOCK. Prose in this help quotes flags
#      that belong to no verb ("--db PATH" in "Common flags:"), and a scanner
#      that keeps collecting past the synopsis attributes them to the last verb.
#
# USAGE
#   scripts/g3-memory-help-surface.sh [path-to-gopi]
# Default binary: ./bin/gopi (build it first:
#   CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ ).
#
# OUTPUT (TSV, one row per verb, tab-separated, flags sorted):
#   verb <TAB> flag1,flag2,… <TAB> flag count
# Tab-separated so a reader can diff it against the checked-in table in the
# inventory doc without a Markdown-aware tool.
set -euo pipefail

BIN="${1:-./bin/gopi}"
if [ ! -x "$BIN" ]; then
    echo "g3-memory-help-surface.sh: $BIN is not executable — build it with" >&2
    echo "  CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/" >&2
    exit 1
fi

HELP="$("$BIN" memory -h 2>&1)"

printf '%s\n' "$HELP" | awk '
    # A synopsis head: 2-space indent, "gopi memory <lowercase verb>". A verb
    # with SEVERAL heads (provenance prints twice, feedback four times — the
    # mode flags do not fit on one line) accumulates into the same row: the file
    # is emitted only when the VERB CHANGES, never between a verb and the prose
    # that describes it.
    /^  gopi memory [a-z]/ {
        v = $3
        if (v != verb) {
            if (verb != "") emit()
            verb = v
            flags = ""
        }
        collect($0)
        next
    }
    # A wrapped continuation: indented to the bracket column and itself opening
    # a bracket group (`[`) or continuing one across a pipe (`| --latent-…`).
    verb != "" && /^ {18,}[[]/ { collect($0); next }
    verb != "" && /^ {18,}\|/ { collect($0); next }
    # Every other line (a description block, prose) is not part of the surface:
    # it is neither collected nor a terminator, so a multi-head verb keeps
    # accumulating across its own description.
    END { if (verb != "") emit() }

    function collect(line,   n, parts, i, f) {
        n = split(line, parts, /--/)
        for (i = 2; i <= n; i++) {
            if (match(parts[i], /^[a-z][a-z-]*/)) {
                f = substr(parts[i], 1, RLENGTH)
                if (index("," flags ",", "," f ",") == 0) {
                    flags = (flags == "" ? f : flags "," f)
                }
            }
        }
    }
    # emit SORTS the flag names, so this output is diffable against the Go-side
    # derivation (which sorts) and against a previous run.
    function emit(   count, arr, i, j, key, joined) {
        count = split(flags, arr, ",")
        for (i = 2; i <= count; i++) {
            key = arr[i]
            for (j = i - 1; j >= 1 && arr[j] > key; j--) arr[j + 1] = arr[j]
            arr[j + 1] = key
        }
        joined = ""
        for (i = 1; i <= count; i++) joined = (i == 1 ? arr[i] : joined "," arr[i])
        printf "%s\t%s\t%d\n", verb, (count == 1 && arr[1] == "" ? "" : joined), (count == 1 && arr[1] == "" ? 0 : count)
        verb = ""
    }
'
