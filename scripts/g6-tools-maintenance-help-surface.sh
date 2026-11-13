#!/usr/bin/env bash
# g6-tools-maintenance-help-surface.sh — the RE-DERIVATION command behind
# devdoc/gopi-tools-maintenance-help-surface-inventory.md (G6 of the
# help-surface hardening epic, briefings/brief-help-surface-hardening.md).
#
# WHY A SCRIPT AND NOT A HAND-TYPED TABLE. The deliverable is "every invocation
# form the G6 help pages ADVERTISE, and whether it has a basic test". A table
# typed by hand describes the help as it was the day somebody read it; this one
# is derived from the binary's OWN usage text every time it runs, and
# cmd/agent/cli_tools_maintenance_help_surface_basic_test.go re-derives the same
# list in-process (from the usage CONSTANTS the help printer itself reads) and
# fails when the checked-in table and the derivation disagree — so the table
# cannot go stale silently.
#
# EVERY AWK PROGRAM LIVES IN A .awk FILE NEXT TO THIS SCRIPT. Two reasons, both
# learned the hard way while writing it: a single-quoted shell heredoc cannot
# carry an apostrophe anywhere in its comments (the shell closes the quote and
# the parser reports a syntax error at an unrelated line), and a program that
# long is unreadable inline. The .awk files are also where the scanner rules are
# documented at length.
#
# WHAT IS DERIVED, AND FROM WHERE:
#   * one record per GROUP VERB: `gopi <verb> -h`.
#   * the TOP-LEVEL page (`gopi -h`) under two pseudo-verbs:
#       "(top-level)"        the Usage, Inputs and Commands blocks — the
#                            invocation FORMS the briefing names ("<prompt>",
#                            input.txt, @PATH, @-, a .png positional, -h,
#                            --help-env, --prompt, and the commands the page
#                            lists). The three blocks are scanned by the same
#                            rules but keep their own shape handling (an Inputs
#                            entry is not a `gopi …` line, so it is rendered as
#                            `gopi <shape>` to stay copy-pasteable);
#       "(top-level flags)"  the Options block — a flag TABLE, whose claim is
#                            "this spelling is admitted", graded by the flag
#                            sweep rather than by a per-form invocation.
#   * the FLAG SET of a form: every `--flag` spelled on the form itself, sorted.
#
# USAGE
#   scripts/g6-tools-maintenance-help-surface.sh [path-to-gopi]
# Default binary: ./bin/gopi (build it first:
#   CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ ).
#
# OUTPUT (TSV, one row per distinct form, keyed by verb first):
#   verb <TAB> invocation <TAB> flag1,flag2,… <TAB> flag count
# Tab-separated so a reader can diff it, or join it into the Markdown table,
# without a Markdown-aware tool.
set -euo pipefail

BIN="${1:-./bin/gopi}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ ! -x "$BIN" ]; then
    echo "g6-tools-maintenance-help-surface.sh: $BIN is not executable — build it with" >&2
    echo "  CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/" >&2
    exit 1
fi

# The G6 group, in the order the top-level help lists it. Binary spellings, not
# prose: this list must equal g6GroupVerbs in the Go gate, and the gate fails
# when a page gains a form for a verb that is not here.
G6_VERBS="vision_analyze text2img screen_capture act voice_input shot code-search archive worker config completion scratch-gc version"

# THE DOOR EACH VERB IS REACHED THROUGH (verb-retirement phase 1, 2026-10-06).
#
# The help pages are the same DOCUMENT whatever address reaches them, and the
# forms they advertise are the group spelling (`gopi tool act …`) — but the page
# is only PRINTED through that spelling: `gopi act -h` is refused by name now,
# so a derivation that drove the flat word would scan a one-line refusal and
# report the verb as contributing nothing. This table is where "which argv
# prints this page" lives, and it is the ONLY place: the awk scanners below
# key their rows by the VERB (`G6_VERBS`), never by the address, so a row's
# verb column keeps the group's own word while its form column carries the
# spelling a reader types.
#
# Three addresses, and each is a binary spelling rather than prose:
#   * `tool`     — the local-tool family (the operator's ruling: it is the ONE
#                  entry point, `gopi tool <verb>`);
#   * `task`     — archive was absorbed by the task ledger;
#   * `config`   — completion is an act of configuration.
# A verb with no door of its own (`worker`, `config`, `version`) is reached by
# its own word and gets the bare spelling, which is why the table is spelled out
# rather than derived from a prefix. `merge` was the fourth and LEFT the list
# with its command surface (epic merge-removal, item 2): a verb this binary does
# not print a page for cannot be derived, and a derivation that drove it would
# scan a one-line retirement refusal and report it as contributing nothing.
G6_DOOR_vision_analyze="tool"
G6_DOOR_text2img="tool"
G6_DOOR_screen_capture="tool"
G6_DOOR_act="tool"
G6_DOOR_voice_input="tool"
G6_DOOR_shot="tool"
G6_DOOR_code_search="tool"
G6_DOOR_archive="task"
G6_DOOR_worker=""
G6_DOOR_config=""
G6_DOOR_completion="config"
G6_DOOR_scratch_gc="tool"
G6_DOOR_version=""

# g6_page_address echoes the argv that prints one verb's help page. The verb's
# dashes are untangled for the variable lookup (`code-search` → code_search)
# because a shell variable may not carry a dash.
g6_page_address() { # g6_page_address <verb>
    local verb="$1" door
    door="$(g6_door "$1")"
    if [ -n "$door" ]; then
        printf '%s %s' "$door" "$verb"
        return
    fi
    printf '%s' "$verb"
}

# g6_door echoes the verb's door word ALONE ("" when it has none) — the value
# the awk scanner needs to re-derive the page's flat spellings. Both readers ask
# this ONE function, so the address a page is read through and the spelling its
# forms are rewritten to cannot drift apart (Rule 13).
g6_door() { # g6_door <verb>
    local key="G6_DOOR_${1//-/_}"
    printf '%s' "${!key:-}"
}

printf '%s\n' "$G6_VERBS" | tr ' ' '\n' | while read -r verb; do
    [ -n "$verb" ] || continue
    # shellcheck disable=SC2086 # the address is a deliberate 1- or 2-word argv
    "$BIN" $(g6_page_address "$verb") -h 2>&1 |
        awk -v verb="$verb" -v door="$(g6_door "$verb")" -f "$HERE/g6-help-forms.awk"
done

"$BIN" -h 2>&1 | awk -f "$HERE/g6-top-level.awk"
