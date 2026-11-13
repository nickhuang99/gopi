#!/usr/bin/env bash
# g1-task-help-surface.sh — the RE-DERIVATION command behind
# devdoc/gopi-task-help-surface-inventory.md (G1 of the help-surface hardening,
# briefings/brief-help-surface-hardening.md).
#
# WHY A SCRIPT AND NOT A HAND-WRITTEN TABLE. The briefing's deliverable is an
# inventory of "every `gopi task` verb/flag the help text advertises". A table
# typed by hand describes the help as it was on the day somebody read it; this
# one is derived from the binary's OWN usage text every time it is run, and the
# Go side re-derives the same list in cmd/agent/cli_tasks_help_surface_basic_test.go
# (TestTaskHelpSurfaceMatchesInventory), so a verb added to the help without a
# row — or a row claiming a verb the help no longer prints — fails the build
# rather than leaving the table stale.
#
# WHAT IS DERIVED, AND FROM WHERE:
#   * the verb set  = the `gopi task <verb> …` SYNOPSIS HEADS of `gopi task -h`
#                     (the ledger group's own help; the group PONTS at the sync
#                     and merger pages for verbs whose home is elsewhere, and a
#                     pointer line is not a synopsis — see the "Not ledger
#                     verbs" section of the help, which carries no head).
#   * the flag set  = every `--flag` spelled inside a verb's synopsis, INCLUDING
#                     its wrapped continuation lines (`submit` wraps over five).
#
# USAGE
#   scripts/g1-task-help-surface.sh [path-to-gopi]
# Default binary: ./bin/gopi (build it first:
#   CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ ).
#
# OUTPUT (TSV, one row per verb, tab-separated):
#   verb <TAB> flag1,flag2,… <TAB> flag count
# Tab-separated so the READER can diff it against the checked-in table in the
# inventory doc without a Markdown-aware tool.
set -euo pipefail

BIN="${1:-./bin/gopi}"
if [ ! -x "$BIN" ]; then
    echo "g1-task-help-surface.sh: $BIN is not executable — build it with" >&2
    echo "  CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/" >&2
    exit 1
fi

HELP="$("$BIN" task -h 2>&1)"

# One pass in awk, mirroring the Go derivation exactly:
#   * a synopsis head is a line starting "  gopi task <lowercase-verb>"
#   * its continuation lines are the wrap lines: indent >= 19 spaces, and the
#     line carries bracket groups (submit wraps once per flag group)
#   * a synopsis ENDS at the first line that is neither (the description block,
#     which is where prose lives — its flags are PROSE, not the surface, e.g.
#     the words "--db PATH" in "Common flags:" belong to no verb)
printf '%s\n' "$HELP" | awk '
    /^  gopi task [a-z]/ {
        if (verb != "") emit()
        verb = $3
        flags = ""
        collect($0)
        next
    }
    verb != "" && /^ {18,}\[/ {
        collect($0)
        next
    }
    verb != "" { emit() }
    END { if (verb != "") emit() }

    function collect(line,   n, parts, i, f) {
        n = split(line, parts, /--/)
        for (i = 2; i <= n; i++) {
            # take the flag name: lowercase letters and dashes only
            if (match(parts[i], /^[a-z][a-z-]*/)) {
                f = substr(parts[i], 1, RLENGTH)
                if (index("," flags ",", "," f ",") == 0) {
                    flags = (flags == "" ? f : flags "," f)
                }
            }
        }
    }
    # emit SORTS the flag names, so this output is diffable against the Go-side
    # derivation (g1AdvertisedFlags, which sorts) and against a previous run.
    function emit(   count, arr, i, j, key) {
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
