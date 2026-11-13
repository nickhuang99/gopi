#!/usr/bin/env bash
# g4-session-help-surface.sh — the RE-DERIVATION command behind
# devdoc/gopi-session-help-surface-inventory.md (G4 of the help-surface
# hardening: the session-inspection + resume group — `gopi session log`,
# `gopi session stats`, `gopi session raw`, `gopi session checkpoint
# list|export`, `gopi session resume`).
#
# THE GROUP'S SPELLING MOVED (verb-retirement phase 1, 2026-10-05). The five
# verbs were retired from the TOP LEVEL — `gopi log …` is refused by name now —
# so this script probes the LIVE address, `gopi session <verb> -h`, for ALL FIVE.
# Driving the old spellings would print five refusal messages and derive an
# inventory OF THEM. The SAME holds for this script's OWN earlier page word:
# `checkpoint` was retired too, so the bare spelling of that page (program word +
# `checkpoint`, no family) is refused at argv[0] as well, and probing it derives
# nothing at all for the one verb whose page word still looked right.
#
# THE FAMILY PAGE (`gopi session -h`) IS NOT ONE OF THE FIVE PAGES, AND THAT IS
# A BOUNDARY RATHER THAN AN OMISSION (measured 2026-10-07). The family page's
# seven synopses advertise the SUB-VERB, not flags its own page parses, and it is
# the page a reader LANDS on rather than the page a verb is documented by — so it
# belongs to the hierarchy guard, not to this inventory. The division was checked
# the hard way on the day the family page's eighth line was withdrawn (the
# shorthand `gopi session checkpoint list|export`, an alternation in a subcommand
# slot): the line neither appears in this script's output before nor after the
# withdrawal, because this script never reads that page. The reachability the
# withdrawal had to preserve is asserted where it lives —
# `TestFamilyPageStillTeachesTheCheckpointShorthand` (the shorthand, as prose, on
# the family page) and the synopsis-shape refusal in
# `cmd/agent/cli_session_usage_dispatch_test.go`.
#
# WHAT "LIVE" MEANS IS MEASURED, NOT ASSUMED: a page word is the SPELLING this
# script types, and the five below are the ones the binary answers. The list is
# the group the briefing names — the same five the Go side holds in `g4Pages`
# (cmd/agent/cli_session_help_surface_basic_test.go), each paired with the usage
# constant the binary prints from it, so both derivations read one text.
#
# WHY A SCRIPT AND NOT A HAND-WRITTEN TABLE. The deliverable is an inventory of
# "every verb/flag the LIVE help advertises for this group". A table typed by
# hand describes the help as it was on the day somebody read it; this one is
# derived from the binary's OWN usage text every time it is run, and the Go side
# re-derives the same list from the same usage constants
# (TestG4HelpSurfaceMatchesInventory), so a line added to a usage constant
# without a row — or a row claiming an invocation the help no longer prints —
# fails the build rather than leaving the table stale.
#
# WHAT IS DERIVED, AND FROM WHERE. One row per SYNOPSIS LINE the five help pages
# print: `gopi session <verb> …` at two-space indent. The invocation is cut at
# the description column, which on every one of these pages opens with TWO OR
# MORE spaces (the invocations themselves never contain a double space), so the
# cut is the first `  +` at or past column 18. The VERB comes from the line
# itself, not from the page: the checkpoint page prints the `gopi session resume
# …` synopses too (the two commands share one page).
#
# A SYNOPSIS THAT BREAKS THE PAGE'S OWN ADDRESS IS A HELP DEFECT, NOT A ROW.
# The checkpoint page's third Usage line printed `gopi checkpoint list|export
# [--dir DIR]` — the program word with no family, on a page whose every sibling
# spells `gopi session checkpoint …`. Under the family rule above that line is
# NOT an invocation this script may report (it is not the live address), and the
# Go derivation, which reads the family prefix off every synopsis, could only
# file it as the nonsense address `gopi session gopi checkpoint l…`. It is fixed
# ON THE PAGE (cmd/agent/cli_checkpoint.go) rather than filtered here, so both
# derivations see the same corrected surface.
#
# THAT LINE ALSO LEFT A SECOND TRACE, AND IT IS WHAT THE MATCHER BELOW IS FOR
# (measured 2026-10-07). While the third synopsis existed, this matcher accepted
# the bare spelling as well as the family one — the family word was OPTIONAL in
# the pattern — so the earlier, pre-retirement page word `checkpoint -h` (with
# the program word before it) was still read as a page address. It is not one:
# `checkpoint` was retired from the TOP LEVEL (the bare spelling is refused at
# argv[0], naming `session checkpoint`), so that probe answers with a refusal,
# prints no synopsis line, and the page's rows come out at ZERO while the loop
# still exits 0 — a silent under-report, which is exactly the failure mode this
# script must not have. The matcher is therefore the LIVE family address ONLY,
# and the retired bare spelling appears nowhere in this file
# (TestG4DerivationScriptDrivesEverySurfaceAddress holds it there).
#
# THE SECTION IS PART OF THE ROW. The group's pages have three sections that
# carry invocations — `Usage:` (the canonical surface), `Flags can be
# combined:` (worked combinations), `Examples:` (the resume examples, which is
# where `--task` is spelled) — and they are NOT the same claim: a `Usage:` line
# is a promise the group keeps for every invocation, a `Examples:` line is a
# worked example. The section travels in the output so the inventory (and its
# gate) can tell them apart.
#
# A SECTION HEADER IS A LINE THAT IS A BARE `Words:` TITLE; a section ends at
# the next one. Prose that quotes a flag (`--db PATH` in another group's
# "Common flags:") is therefore never read as a surface, because it is neither a
# header nor a two-space-indented `gopi …` line.
#
# USAGE
#   scripts/g4-session-help-surface.sh [path-to-gopi]
# Default binary: ./bin/gopi (build it first:
#   CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ ).
#
# OUTPUT (TSV, one row per canonical invocation, in help order, deduplicated):
#   verb <TAB> section <TAB> invocation
# Tab-separated so the READER can diff it against the checked-in table in the
# inventory doc without a Markdown-aware tool.
set -euo pipefail

BIN="${1:-./bin/gopi}"
if [ ! -x "$BIN" ]; then
    echo "g4-session-help-surface.sh: $BIN is not executable — build it with" >&2
    echo "  CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/" >&2
    exit 1
fi

# THE PAGES ARE DECLARED, ONE LINE PER PAGE — the surface this script derives, in
# the Go derivation's own page order (`g4Pages`: log, stats, raw, checkpoint,
# resume) so the two outputs diff line for line. Each entry is the address a
# reader types, and the probe, the matcher and the verb attribution are all
# derived from this ONE list — nothing below retypes a page.
#
# WHY THE LIST IS DATA. A derivation script is an artifact no in-Go gate runs, so
# its page list is the thing that can silently go dead: driving a page word the
# dispatcher has RETIRED prints a refusal message (no synopsis line at all) and
# contributes ZERO rows while the script still exits 0. Measured, twice: the flat
# top-level spellings (`log`/`stats`/`raw`/`resume`) derived four pages of
# refusals, and the bare `checkpoint` page word — this script's own earlier
# spelling — derived nothing for the fifth. The list is therefore readable, and
# TestG4DerivationScriptDrivesEverySurfaceAddress holds every entry to the LIVE
# family address the binary answers.
# shellcheck disable=SC2034 # the list is read by the loop below and by nothing else
G4_ADDRESSES=(
    "gopi session log"
    "gopi session stats"
    "gopi session raw"
    "gopi session checkpoint"
    "gopi session resume"
)

# The program word + family word, read OFF the list rather than retyped: the
# first entry minus its verb is the address prefix every live synopsis carries.
G4_FAMILY="${G4_ADDRESSES[0]% *}"
G4_FAMILY_WORD="${G4_FAMILY#* }"

# The matcher's verb alternation, likewise read off the list — so a page added
# above is matchable below, and the pattern can only ever name the addresses
# declared. A synopsis spelled with a RETIRED address is not matched, which is
# deliberate: the page's own address rule is what this script reports, and a
# stray bare line is a help defect for the gates in cmd/agent/, not a row here.
G4_VERB_ALTERNATION="$(for a in "${G4_ADDRESSES[@]}"; do printf '%s|' "${a##* }"; done)"
G4_VERB_ALTERNATION="${G4_VERB_ALTERNATION%|}"

# The help flag the page is asked for — the SAME word the Go side holds in its
# own constant (HelpFlag, `cmd/agent/cli_verbs.go`), spelled once here so the
# probe cannot ask a page for something the binary does not answer.
HELP_FLAG=-h

# The matcher: two-space indent, the LIVE family address, then the verb slot and
# a space. It is THE contract between the probe and this script's output.
G4_INVOCATION_RE="^  ${G4_FAMILY} (${G4_VERB_ALTERNATION}) "

# Every one of the five pages is driven through the session group — `checkpoint`
# included, whose page is printed by the family address like the other four, and
# is the same page the checkpointUsage constant holds.
for address in "${G4_ADDRESSES[@]}"; do
    # The verb is the entry's own last word, and the probe spends the entry minus
    # its program word — because `$BIN` IS the program word (argv[0]). Every word
    # here is DERIVED from the declared entry, so nothing below retypes a page.
    verb="${address##* }"
    # shellcheck disable=SC2086 # the address is a deliberate multi-word argv
    "$BIN" ${address#* } "$HELP_FLAG" 2>&1 | awk -v page="$verb" -v invocation_re="$G4_INVOCATION_RE" -v family="$G4_FAMILY_WORD" '
        # A section header: a bare `Title:` line (no indent).
        /^[A-Za-z(][^ ].*:$/ { section = $0; sub(/:$/, "", section); next }
        # An invocation line: two-space indent, the LIVE family address
        # `gopi session <verb> …` — the only address this script reports, so a
        # page that momentarily carries a bare retired spelling contributes no
        # row instead of a row of refusal text.
        $0 ~ invocation_re {
            line = $0
            # A shell pipe ends the invocation: the worked example half after
            # `| jq …` is prose about the example, not argv.
            p = index(line, " | ")
            if (p > 0) line = substr(line, 1, p - 1)
            cut = 0
            for (i = 18; i <= length(line); i++) {
                # the description column opens with two or more spaces
                if (substr(line, i, 1) == " " && substr(line, i + 1, 1) == " ") { cut = i; break }
            }
            inv = (cut ? substr(line, 1, cut - 1) : line)
            sub(/^ +/, "", inv)
            sub(/ +$/, "", inv)
            if (!(inv in seen)) {
                seen[inv] = 1
                printf "%s\t%s\t%s\n", inv2verb(inv, page), (section == "" ? "Usage" : section), inv
            }
        }
        # The verb comes from the invocation text itself — the word after the
        # program word and the family word (every live synopsis spells the
        # family; `family` is passed in rather than retyped) — never from the
        # page: the checkpoint page prints the resume synopsis too.
        function inv2verb(inv, page,   tok) {
            split(inv, tok, " ")
            if (tok[2] == "") return page
            return (tok[2] == family ? tok[3] : tok[2])
        }
    '
done
