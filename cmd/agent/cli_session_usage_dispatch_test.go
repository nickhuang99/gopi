// cli_session_usage_dispatch_test.go — the SESSION FAMILY page's syntax rule,
// and the red proof that it can fail.
//
// WHAT THIS FILE IS FOR, in one sentence: `gopi session -h` must not promise,
// under its `Usage:` heading, a command an operator cannot type — and the shape
// that promise took here was an ALTERNATION in the subcommand slot
// (`gopi session checkpoint list|export`), which the dispatcher answers with its
// own refusal (`unknown subcommand "list|export"`, exit 1).
//
// IT IS THE SIBLING of cmd/agent/cli_checkpoint_usage_dispatch_test.go, one page
// away, and the relationship is the interesting part. Master ce507a8f withdrew
// that exact line from checkpointUsage's Usage block (keeping the shorthand as
// prose) and left a note there saying the SESSION page still printed it — the
// finding is quoted verbatim in its comment ("`sessionUsage` still PRINTS `gopi
// session checkpoint list|export` under its `Usage:` heading … recorded in the
// report as a follow-up"). This file is that follow-up: the same ruling, applied
// to the page that still carried the line.
//
// WHERE THE TWO PAGES DIFFER, and why the fix is not a copy. On the checkpoint
// page the alternation was a synopsis of the page's OWN two subcommands; on the
// session page it is a CROSS-REFERENCE — the session page does not dispatch
// `list|export` at all, it points at a third-level group (`gopi session
// checkpoint …`) whose own page teaches the pair. So withdrawing the line must
// keep the reader's route to that group, and it does: the paragraph that
// replaces it names both real commands AND the shorthand, which is exactly what
// the hierarchy guard's mustAlsoAppear assertion reads.
package main

import (
	"regexp"
	"strings"
	"testing"
)

// sfUsage... are the page geometry's named constants (Rule 1): the heading whose
// lines are promises, the program word every synopsis opens with, and the two
// display shapes a synopsis line has on these pages.
const (
	// sfUsageSectionUsage is the heading whose lines are argv an operator will
	// type. It is the same value cpUsageSectionUsage carries, and it is spelled
	// here rather than shared because the two files' derivations are
	// deliberately independent (a bug in one must not hide the other).
	sfUsageSectionUsage = "Usage"

	// sfUsageProgramWord is the word every synopsis on these pages opens with.
	sfUsageProgramWord = "gopi"

	// sfUsageDescriptionGap is the run of spaces that opens a synopsis line's
	// DESCRIPTION column. The page's own geometry: an invocation never contains
	// two consecutive spaces, so the first such run at or past
	// sfUsageDescriptionMinCol is the boundary and not a guess.
	sfUsageDescriptionGap = "  "

	// sfUsageDescriptionMinCol is the column that boundary may not precede —
	// the invocation column starts at 2, so a double space before this is
	// inside the argv (`gopi session log  --x`), never the description.
	sfUsageDescriptionMinCol = 18

	// sfUsageIndent is the indent a synopsis line carries on these pages.
	sfUsageIndent = "  "

	// sfUsageSynopsisArrow is the separator between a synopsis and its one-line
	// gloss (`gopi session log <id> …    → the JSONL session log reader`). It
	// is named because the derivation cuts it: the gloss is prose.
	sfUsageSynopsisArrow = "→"

	// sfUsageDisjunction is the alternation separator. A synopsis FIELD
	// carrying it advertises TWO commands in the slot of one.
	sfUsageDisjunction = "|"

	// sfUsageWordSep is the space between two argv words.
	sfUsageWordSep = " "

	// sfUsageMinSynopses is the floor on the derivation: the family page prints
	// seven `gopi session …` synopsis lines today, so a scan that finds fewer
	// than six is blind and its green would prove nothing (Rule 15: a scan that
	// found nothing must be distinguishable from a scan that never ran).
	sfUsageMinSynopses = 6
)

// sfUsageSynopsisLineRe matches a synopsis line of a session-family Usage block:
// the two-space indent, the program word, the family word, and at least one
// further word. The FAMILY address is the live one (verb-retirement phase 1
// moved the four session verbs under the group), which is why the family word is
// part of the pattern rather than an optional prefix.
var sfUsageSynopsisLineRe = regexp.MustCompile(
	`^` + sfUsageIndent + sfUsageProgramWord + ` ` + CliVerbSession + ` \S`)

// --- the derivation ----------------------------------------------------------

// sfUsageSynopsisLines returns every synopsis line under a page's `Usage:`
// heading, cut at the description column, in page order.
//
// The SECTION is tracked, not assumed: the family page has prose headings of its
// own, and only the lines under `Usage:` are promises. A line naming another
// verb than the page's own would be returned too — which is what keeps this
// derivation honest about what a page PRINTS (the checkpoint page prints the
// resume synopsis for the same reason).
func sfUsageSynopsisLines(page string) []string {
	var out []string
	section := ""
	for _, line := range strings.Split(page, "\n") {
		if isUsageSectionHeader(line) {
			section = strings.TrimSuffix(line, ":")
			continue
		}
		if section != sfUsageSectionUsage {
			continue
		}
		if !sfUsageSynopsisLineRe.MatchString(line) {
			continue
		}
		if inv := sfUsageCut(line); inv != "" {
			out = append(out, inv)
		}
	}
	return out
}

// sfUsageCut cuts a synopsis line down to its invocation: the gloss after the
// page's `→` separator is prose, and the description column (two or more spaces
// at or past sfUsageDescriptionMinCol) ends the argv.
func sfUsageCut(line string) string {
	if i := strings.Index(line, sfUsageSynopsisArrow); i >= 0 {
		line = line[:i]
	}
	if i := strings.Index(line, sfUsageDescriptionGap); i >= sfUsageDescriptionMinCol {
		line = line[:i]
	}
	return strings.TrimSpace(line)
}

// sfUsageSynopsisFields returns a synopsis line's argv words.
func sfUsageSynopsisFields(synopsis string) []string {
	return strings.Fields(sfUsageCut(synopsis))
}

// sfUsageDisjunctiveField returns the FIRST argv word carrying the alternation
// separator, or "" when the line spells none. It is the pure half of the rule
// (a t.Errorf cannot be asserted from outside the testing package, and an
// unasserted guard is a guard nobody has watched go red — Rule 5).
//
// THE WORD SLOT IS WHAT IS CHECKED, and the choice is deliberate: the check runs
// on the DERIVED argv words, not on the raw line, so an alternation in the
// PROSE half (`→ the raw-log snapshot registry`, or a description that says
// "chat|web") cannot produce a false red. What is refused is an alternation in a
// word position — the slot the dispatcher reads.
func sfUsageDisjunctiveField(synopsis string) string {
	for _, f := range sfUsageSynopsisFields(synopsis) {
		if strings.Contains(f, sfUsageDisjunction) {
			return f
		}
	}
	return ""
}

// sfUsageAssertNoDisjunction is the family page's syntax rule: no synopsis under
// `Usage:` may carry an alternation in a word slot.
func sfUsageAssertNoDisjunction(t *testing.T, synopsis string) {
	t.Helper()
	if f := sfUsageDisjunctiveField(synopsis); f != "" {
		t.Errorf("Usage line %q writes the word %q — an ALTERNATION, not an "+
			"invocation. A synopsis under `%s:` is a PROMISE: the dispatcher "+
			"answers the word after the verb, and the alternation is not a word it "+
			"accepts (`gopi session checkpoint list|export` → exit 1, unknown "+
			"subcommand). Spell one line per command, and keep the shorthand in "+
			"prose, where it is read as the note it is — the ruling behind this "+
			"line's withdrawal from the page.",
			synopsis, f, sfUsageSectionUsage)
	}
}

// --- the gate ----------------------------------------------------------------

// TestSessionUsageLinesAreDispatchable is the red-proof gate: every `gopi
// session …` synopsis under the family page's `Usage:` heading must (a) carry no
// alternation in a word slot, and (b) actually BE an invocation — the derived
// argv is handed to the real binary, and the DISPATCHER's own refusal
// (`unknown subcommand`) must never come back.
//
// The two halves catch opposite mistakes. A guard on syntax alone would let a
// synopsis name a word nobody dispatches (`gopi session frobnicate`); a probe
// alone would drive an alternation silently, since `list|export` is ONE argv
// word to the shell and the refusal it earns would be indistinguishable from a
// resolution error. Together they pin "the page promises only what the
// dispatcher answers", which is the property both this file's sibling and the
// checkpoint page's own gate exist for.
func TestSessionUsageLinesAreDispatchable(t *testing.T) {
	fx := g4NewFixture(t)
	lines := sfUsageSynopsisLines(sessionUsage)
	if len(lines) < sfUsageMinSynopses {
		t.Fatalf("the derivation found only %d synopsis line(s) in sessionUsage — "+
			"the scanner is blind, so a green run would prove nothing:\n  %s",
			len(lines), strings.Join(lines, "\n  "))
	}
	inspected := 0
	for _, line := range lines {
		sfUsageAssertNoDisjunction(t, line)
		if n := len(sfUsageSynopsisFields(line)); n < sfUsageSynopsisMinFields {
			t.Errorf("Usage line %q carries only %d word(s) — a synopsis names a "+
				"sub-verb (program, family, then the word the dispatcher reads)", line, n)
			continue
		}
		argv := append([]string{CliVerbSession}, sfUsageProbeArgs(line)...)
		inspected++
		t.Run(strings.Join(argv, "_"), func(t *testing.T) {
			stdout, stderr, code := fx.sb.g4RunSplit(t, argv...)
			out := stdout + stderr
			if strings.Contains(out, cpUnknownSubcommand) {
				t.Fatalf("`gopi %s` is printed under sessionUsage's `%s:` heading "+
					"and the dispatcher REFUSES it (exit %d):\n%s\n"+
					"The usage block is the dispatcher's contract: make the word "+
					"one the switch answers, or take the line off the page. (The "+
					"word after the verb is a SUBCOMMAND NAME — an alternation "+
					"there is the defect this file was written for.)",
					strings.Join(argv, " "), sfUsageSectionUsage, code, out)
			}
			// The page's canonical listing line is a COMPLETE invocation: it
			// must actually list, not merely avoid the dispatcher's refusal.
			// Asserting the effect is what keeps this gate from passing on an
			// argv that dies silently further down.
			if len(argv) > 1 && argv[1] == sessionSubcmdList && code != 0 {
				t.Fatalf("`gopi %s` is the family page's canonical listing line and "+
					"exited %d:\n%s", strings.Join(argv, " "), code, out)
			}
		})
	}
	if inspected < sfUsageMinSynopses {
		t.Fatalf("only %d synopsis line(s) reached the dispatcher — the "+
			"derivation is blind", inspected)
	}
}

// sfUsageSynopsisMinFields is the shortest line that IS a synopsis: the program
// word, the family word, and the sub-verb the dispatcher reads.
const sfUsageSynopsisMinFields = 3

// sfUsageProbeArgs turns a synopsis into the argv of the LIVE family address: the
// program word and the family word are dropped (the caller puts the family word
// back and the binary's own path is argv[0]), an optional group is removed whole
// (the gate asserts the BASE form the page promises — cpUsageOptionalGroupRe is
// the SAME rule the sibling page's probe applies, referenced rather than
// re-spelled), and a `<...>` placeholder becomes the probe's unresolvable target,
// so a refusal from resolution is recognisably past the dispatcher.
func sfUsageProbeArgs(synopsis string) []string {
	fields := sfUsageSynopsisFields(cpUsageOptionalGroupRe.ReplaceAllString(synopsis, sfUsageWordSep))
	for len(fields) > 0 && (fields[0] == sfUsageProgramWord || fields[0] == CliVerbSession) {
		fields = fields[1:]
	}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if strings.HasPrefix(f, "<") && strings.HasSuffix(f, ">") {
			out = append(out, cpUsageProbeTarget)
			continue
		}
		out = append(out, f)
	}
	return out
}

// TestSessionUsageDisjunctionIsRefused is the syntax rule's own red proof (Rule
// 10.5), mirroring TestCheckpointUsageDisjunctionIsRefused one page away: the
// RED case is the line the page USED to print — reproduced here verbatim, so the
// guard is watched failing on the shape it exists for — and the GREEN case is
// the page's live text, driven through the same derivation the gate uses.
//
// WHY THE RED CASE IS THE HISTORICAL TEXT RATHER THAN A FABRICATION: master's own
// inventory doc records the refused invocation and its measured exit, and the
// withdrawal IS the change this rule encodes. A guard whose red case was invented
// would prove only that the regex matches a string nobody ever shipped.
func TestSessionUsageDisjunctionIsRefused(t *testing.T) {
	// The withdrawn line, verbatim (cmd/agent/cli_session.go before this change;
	// the same line devdoc/gopi-session-help-surface-inventory.md quotes as the
	// one the checkpoint page had already withdrawn).
	const sfUsageWithdrawnLine = sfUsageIndent + sfUsageProgramWord + " " + CliVerbSession + " " +
		CliVerbCheckpoint + " " + checkpointSubcmdList + sfUsageDisjunction + checkpointSubcmdExport +
		"    " + sfUsageSynopsisArrow + " the raw-log snapshot registry"
	want := checkpointSubcmdList + sfUsageDisjunction + checkpointSubcmdExport
	if got := sfUsageDisjunctiveField(sfUsageWithdrawnLine); got != want {
		t.Fatalf("sfUsageDisjunctiveField(%q) = %q, want %q — the guard no longer "+
			"catches the line this page had to withdraw, so it cannot protect the "+
			"property the withdrawal rests on", sfUsageWithdrawnLine, got, want)
	}
	// The red case is a real promise, not a shape the derivation cannot meet:
	// the line is a `gopi session …` synopsis in the Usage block, which is what
	// makes it reachable by the gate above.
	if lines := sfUsageSynopsisLines(sfUsageSectionUsage + ":\n" + sfUsageWithdrawnLine); len(lines) != 1 {
		t.Fatalf("the withdrawn line is not recognized as a Usage synopsis by the "+
			"derivation (%d line(s)) — the red proof is driving a shape the gate "+
			"could never have seen", len(lines))
	}

	// GREEN: the page's live synopses carry no alternation in a word slot.
	lines := sfUsageSynopsisLines(sessionUsage)
	if len(lines) < sfUsageMinSynopses {
		t.Fatalf("the derivation found only %d synopsis line(s) in sessionUsage", len(lines))
	}
	for _, line := range lines {
		if got := sfUsageDisjunctiveField(line); got != "" {
			t.Errorf("the live page prints the word %q in %q — a synopsis must "+
				"name ONE invocation", got, line)
		}
	}

	// THE SHORTHAND STAYS ON THE PAGE, AS PROSE — the half that keeps this from
	// being a deletion. The pair is what the reader needs (`checkpoint` is not a
	// word this page dispatches, so the shorthand is the cross-reference), and
	// the hierarchy guard asserts the same string at its new home.
	shorthand := CliVerbSession + sfUsageWordSep + CliVerbCheckpoint + sfUsageWordSep +
		checkpointSubcmdList + sfUsageDisjunction + checkpointSubcmdExport
	if !strings.Contains(sessionUsage, shorthand) {
		t.Errorf("sessionUsage no longer carries %q anywhere — the checkpoint "+
			"cross-reference must survive the withdrawal as PROSE, not disappear "+
			"with the line it replaced", shorthand)
	}
	// And it is prose rather than a synopsis: no line the derivation reads as an
	// invocation may contain it, which is the difference between the two homes
	// (a synopsis is driven as argv; prose is read).
	for _, line := range lines {
		if strings.Contains(line, shorthand) {
			t.Errorf("the shorthand appears inside the Usage synopsis %q — it "+
				"belongs in the paragraph, where the reader meets it as a note "+
				"rather than as something to type", line)
		}
	}
}
