// cli_checkpoint_usage_dispatch_test.go — the checkpoint page's Usage block IS
// the dispatcher's contract.
//
// THE DEFECT. `gopi session checkpoint -h` prints a Usage block whose third synopsis is
// `gopi session checkpoint resume <log> [continuation...]`, while the switch below it
// accepted only list|export. Measured on the pre-fix binary (master 79807932):
//
//	$ gopi session checkpoint resume /nonexistent/foo.log
//	gopi session checkpoint: unknown subcommand "resume"      (exit 1)
//
// A usage page is the first thing an operator reads and the only place a
// command's argv is promised (PROJECT.md Rule 31); a synopsis it prints and the
// dispatcher refuses is a help-surface lie.
//
// WHAT THIS FILE PINS, DERIVED RATHER THAN TRANSCRIBED: every command line under
// the `Usage:` heading of `checkpointUsage` — the constant the binary prints —
// must be ACCEPTED by the dispatcher, and the dispatcher's own refusal
// (`unknown subcommand`) must never surface for one of them. A synopsis added to
// the page with no dispatch case therefore fails HERE by construction instead of
// shipping as a refusal.
//
// IT IS NOT AN ERROR-STRING TEST. Only the line's SUBCOMMAND WORD reaches the
// dispatch assertion; the words after it are TARGETS/DIRECTORIES, substituted
// with the fixture's own real ones, and a refusal arriving from that resolution
// is a refusal made PAST the dispatcher — which is the green this test exists
// for. `unknown subcommand` means argv never left the switch.
//
// Rule 31: the probe drives the REAL binary through gopiTestBinPath
// ($GOPI_TEST_BIN overrides it — that is how the red proof was taken, against
// the pre-fix binary). Rule 34/22: the child runs in a sandbox profile built by
// the group's own fixture (g4NewFixture in
// cli_session_help_surface_basic_test.go), never the operator's profile.
package main

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// cpUsageSubcmd... are the subcommand words this page dispatches. `list` and
// `export` are spelled once here for the regression assertions below; `resume`
// comes from the constant the dispatcher itself compares against
// (checkpointSubcmdResume), so the page's promise and the switch cannot drift.
const (
	cpUsageSubcmdList   = "list"
	cpUsageSubcmdExport = "export"
)

// cpUsageSectionUsage is the heading whose lines are promises: everything under
// it is argv an operator will type. (A synopsis under `Examples:` is a worked
// example, not a surface — the page's own section rule.)
const cpUsageSectionUsage = "Usage"

// cpResumeRemovedPointer is the RENDERED removal statement — the same function
// the dispatcher prints through (checkpointResumeRemovedMessage), so the test
// cannot pin a sentence the binary no longer says.
var cpResumeRemovedPointer = checkpointResumeRemovedMessage()

// cpUnknownSubcommand is the DISPATCHER's own refusal, quoted from
// cli_checkpoint.go's message. It is the one string whose presence means "this
// argv never left the switch" — the defect this file was written for, in the
// dispatcher's own words.
const cpUnknownSubcommand = "unknown subcommand"

// cpUsageProbeTarget is the unresolvable target a synopsis's `<...>` placeholder
// becomes. The name is relative (the child resolves it inside its own sandbox):
// no file by this name exists in the fixture profile, and none is created.
const cpUsageProbeTarget = "no-such-checkpoint.log"

// cpUsageFieldsMinimum is the shortest line worth probing: the program word, the
// verb and one more word (`gopi session checkpoint list` carries the `[--dir DIR]` tail,
// so the minimum is genuinely reached rather than assumed).
const cpUsageFieldsMinimum = 3

// cpUsageProgramWord is the program word every synopsis on the page opens with.
const cpUsageProgramWord = "gopi"

// cpUsageDisjunction is the page's alternation separator: a synopsis field
// written `list|export` advertises TWO commands in the slot of one. It is a
// constant because the derivation and the guard below key on the same separator
// (Rule 1/13), and because the ruling it enforces is quoted from it.
//
// THE RULING (master ce507a8f, and devdoc/gopi-session-help-surface-inventory.md,
// "A synopsis the dispatcher refused"): the checkpoint page once printed a third
// Usage line — `gopi session checkpoint list|export [--dir DIR]` — a note about
// the two subcommands spelled AS IF it were an invocation. The dispatcher
// answers the word AFTER the verb, `list|export` is not a word it accepts, and
// the line was measured refused (`session checkpoint list|export` exits 1 on its
// own refusal) while both real lines run. It was withdrawn from the Usage block
// and kept as prose. So on THIS page the separator is a defect signal, not a
// shorthand to expand: a synopsis is a promise, and a promise the dispatcher
// cannot honour is fixed ON THE PAGE (see cpUsageArgv's own comment).
//
// THE SAME SEPARATOR IS REFUSED CROSS-GROUP NOW, and that is where the general
// rule lives: cli_help_shorthand_gate_test.go classifies every row of every page
// in this family and admits a `|` only at bracket depth >= 1 (an optional group
// the page delimits) or as a worked shell example. At depth 0, glued, it is an
// advertised token nobody can type. This page's guard stays because it is the
// page-scoped, dispatcher-driven half: the cross-group gate reads the CONSTANT
// and never runs the binary, and it is the binary run that catches an address
// the page spells with words the dispatcher does not answer.
const cpUsageDisjunction = "|"

// cpUsageHelpSpelling is the page's own help door — a Usage row that names no
// sub-verb. It is real and it exits 0, but it is not part of the dispatcher's
// sub-command switch, so the known-set comparison skips it by name.
const cpUsageHelpSpelling = "-h"

// cpUsageMinInspected is the floor on the derivation: after the `resume`
// synopsis was REMOVED from this page (epic verb-restructure, 2026-10-05) it
// prints TWO Usage synopses — `list` and `export` — so a scan that inspects
// fewer than two is blind and its green would prove nothing. The floor moved
// WITH the page, deliberately: a floor left at 3 would fail the build for a page
// that is correct, and a floor dropped to 0 would pass on a page that lost both.
const cpUsageMinInspected = 2

// cpUsageCommandLine cuts a synopsis at its description column: the invocation is
// what precedes the first run of two or more spaces (an invocation never
// contains a double space — the page's own column rule, the reason this cut is
// the description boundary and not a guess).
func cpUsageCommandLine(line string) string {
	return strings.TrimSpace(strings.SplitN(line, "  ", 2)[0])
}

// --- the derivation ----------------------------------------------------------

// cpUsageCommandLines returns every command line under the `Usage:` heading of a
// usage page, in page order: the two-space-indented `gopi <verb> …` synopsis
// lines, cut at the page's description column (two or more spaces — the page's
// own geometry: an invocation never contains a double space).
//
// The section is TRACKED, not assumed, and the VERB is read off the line, never
// off the page: the checkpoint page prints the resume synopsis too (the two
// commands share one text), so a line naming another verb is that verb's promise
// — it is returned here and filtered by the caller, which is what keeps this
// derivation honest about what the page prints.
func cpUsageCommandLines(page string) []string {
	var out []string
	section := ""
	for _, line := range strings.Split(page, "\n") {
		if isUsageSectionHeader(line) {
			section = strings.TrimSuffix(line, ":")
			continue
		}
		if section != cpUsageSectionUsage {
			continue
		}
		trimmed := strings.TrimLeft(line, " ")
		if len(line)-len(trimmed) < 2 || !strings.HasPrefix(trimmed, cpUsageProgramWord+" ") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < cpUsageFieldsMinimum {
			continue
		}
		out = append(out, cpUsageCommandLine(trimmed))
	}
	return out
}

// isUsageSectionHeader reports whether a line is a bare `Words:` title at column
// 1 — the page's section rule (the checkpoint page has `Usage:` and prose
// headings; only the first is a promise list).
func isUsageSectionHeader(line string) bool {
	if line == "" || line[0] == ' ' || !strings.HasSuffix(line, ":") {
		return false
	}
	return !strings.Contains(strings.TrimSuffix(line, ":"), " ")
}

// cpUsageOptionalGroupRe matches an optional tail group of a synopsis
// (`[--dir DIR]`, `[continuation...]`). The gate asserts the BASE form the page
// promises — the mandatory words an operator can always type — so an optional
// group is removed whole rather than leaked into argv as a stray positional.
var cpUsageOptionalGroupRe = regexp.MustCompile(`\[[^\]]*\]`)

// cpUsageAtHeading is the word a bare synopsis starts with when the page uses
// the family-inclusive form under a `Usage:` heading:
//
//	gopi session checkpoint list [--dir DIR]
//	^    ^        ^
//	|    |        the SUBCOMMAND this gate dispatches
//	|    the FAMILY word the verb-retirement phase 1 movement introduced
//	the program word
//
// Both are DERIVED (the program word is the constant every synopsis opens with,
// the family word the one the dispatcher itself answers to), never typed, so a
// rename does not silently make the probe drive a word the binary refuses.
var cpUsageAtHeading = []string{cpUsageProgramWord, CliVerbSession}

// cpUsageArgv turns a synopsis into the argv an operator would type: the leading
// words the page spells and the gate's own argv does NOT carry are dropped (the
// program word, then the family word when the synopsis names it — the child is
// started with the binary's path, and the gate inserts `session` itself), an
// optional group is removed, and a `<...>` placeholder becomes the probe's
// unresolvable target — so a refusal the probe sees comes from RESOLUTION (past
// the dispatcher), never from argv the page does not promise.
//
// THE FAMILY PREFIX IS DROPPED WHEN THE PAGE SPELLS IT, AND THE MISSING FORM IS
// A HELP DEFECT RATHER THAN SOMETHING THIS PROBE PAPER OVER. The defect that
// forced this (measured on master 2026-10-06): `checkpointUsage` printed its
// third synopsis with the program word but WITHOUT the family,
//
//	gopi checkpoint list|export [--dir DIR]
//
// while each of its siblings printed `gopi session checkpoint …`. Driven
// literally that line's argv began `… session gopi …` — a spelling no operator
// types and the G4 inventory derivation had to file as the address
// `gopi session gopi checkpoint l…`. An invocation the dispatcher cannot honour
// is fixed ON THE PAGE, and both probes then agree by construction.
func cpUsageArgv(synopsis string) []string {
	fields := strings.Fields(cpUsageOptionalGroupRe.ReplaceAllString(synopsis, " "))
	for _, want := range cpUsageAtHeading {
		if len(fields) > 0 && fields[0] == want {
			fields = fields[1:]
		}
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

// cpUsageAssertNoDisjunction is the checkpoint page's SYNTAX rule — the same
// rule its sibling `gopi session -h` page needed, which that page now carries in
// its own file (cmd/agent/cli_session_usage_dispatch_test.go; the line that
// needed it is quoted below).
//
// It runs on the RAW synopsis, and that is deliberate: the derivation below is
// LOSSY. cpUsageArgv maps every `<...>` placeholder to cpUsageProbeTarget, so a
// field that is secretly an alternation would reach the dispatcher as a word the
// page prints nowhere — exactly the `session gopi checkpoint l…` nonsense address
// master's own comment records. Checking after the derivation would be checking
// the probe's spelling, not the page's.
//
// WHY THE SAME RULE IS WORTH KEEPING ON THIS PAGE AFTER CE507A8F — the honest
// measurement. Master reached this page's syntax rule the EXPENSIVE way: the
// shorthand LINE existed, was driven, was refused, and was withdrawn (prose
// now). The ruling that fell out is the one encoded here — a synopsis under
// `Usage:` is a promise, and `list|export` is not a word the dispatcher answers
// — so the guard makes the WITHDRAWAL structural instead of historical: master
// re-derived the page by hand, this refuses the shape mechanically.
//
// AND THE RULE WAS NOT HYPOTHETICAL ON THE SIBLING PAGE — measured on master
// `014358b2` during that rework, and FIXED there since (2026-10-07):
//
//	$ gopi session checkpoint list|export
//	gopi session checkpoint: unknown subcommand "list|export"        (exit 1)
//	$ gopi session checkpoint list      → exit 0   (lists; --dir DIR honoured)
//	$ gopi session checkpoint export    → exit 1   (missing <log>: the resolver)
//
// `sessionUsage` used to PRINT `gopi session checkpoint list|export` under its
// `Usage:` heading (cmd/agent/cli_session.go) — the same class of defect, one
// page away. It is WITHDRAWN now (the shorthand survives as the paragraph naming
// both real commands), and the fix carries its own page-scoped guard:
// cmd/agent/cli_session_usage_dispatch_test.go holds the family page to this very
// syntax rule and drives its synopses through the dispatcher. The rule is
// therefore uniform across the two pages, and so is the reason: under `Usage:` a
// line is a promise, and the word after the verb is a SUBCOMMAND NAME.
func cpUsageAssertNoDisjunction(t *testing.T, line string) {
	t.Helper()
	if f := cpUsageDisjunctiveField(line); f != "" {
		t.Errorf("Usage line %q writes %q — an ALTERNATION (%q) in a synopsis, not "+
			"an invocation. A synopsis under `%s:` is a PROMISE: the dispatcher "+
			"answers the word after the verb, and it does not answer %q. Spell one "+
			"line per command, or move the shorthand into prose where it is read as "+
			"a note rather than driven as argv (the ruling behind the checkpoint "+
			"page's own third synopsis being withdrawn).",
			line, f, cpUsageDisjunction, cpUsageSectionUsage, f)
	}
}

// cpUsageDisjunctiveField returns the FIRST field of a synopsis that carries the
// page's alternation separator, or "" when the line spells none. It is the pure
// half of the guard above, split out so the rule itself is testable: a `t.Errorf`
// cannot be asserted from outside the testing package, and an unasserted guard is
// a guard nobody has watched go red (Rule 5 — every rule ships the minimal
// repro that proves it can fail).
func cpUsageDisjunctiveField(line string) string {
	for _, f := range strings.Fields(cpUsageOptionalGroupRe.ReplaceAllString(line, " ")) {
		if strings.Contains(f, cpUsageDisjunction) {
			return f
		}
	}
	return ""
}

// --- the gate ----------------------------------------------------------------

// TestCheckpointUsageLinesAreDispatchable is the red-proof gate: every command
// line under `checkpoint -h`'s Usage block that names THIS verb is handed to the
// real dispatcher, and the dispatcher's refusal must not appear. The second half
// compares the two sets in both directions — what the page promises and what the
// switch accepts must be the same set.
//
// ONE LINE, ONE INVOCATION, ONE PROBE — deliberately, and this is the point the
// archived branch differs on. That branch DERIVED the alternation: it split a
// field like `list|export` into one argv per alternative and drove each, so a
// green run meant "every alternative the page advertises also dispatches". The
// derivation is not kept, because the premise is not this page's any more: after
// master ce507a8f the page prints no alternation at all (the one it had was
// refused by the dispatcher and was withdrawn as prose), so an expansion here
// would be machinery whose input the guard above REFUSES. Deriving a shape the
// page is forbidden to use would paper over the very defect it expands — the
// `cpUsageProbeTarget` substitution would have turned `list|export` into a word
// the page prints nowhere and driven the dispatcher with it. The rule that
// survives is the branch's INSPECTION of the separator, not its expansion of it,
// and it survives as a refusal at the source (cpUsageAssertNoDisjunction) rather
// than as a probe.
func TestCheckpointUsageLinesAreDispatchable(t *testing.T) {
	fx := g4NewFixture(t)
	lines := cpUsageCommandLines(checkpointUsage)
	if len(lines) < cpUsageMinInspected {
		t.Fatalf("the derivation found only %d Usage line(s) in checkpointUsage — "+
			"the scanner is blind, so a green run would prove nothing:\n  %s",
			len(lines), strings.Join(lines, "\n  "))
	}
	accepted := map[string]bool{}
	for _, s := range checkpointCompletionNames {
		accepted[s] = true
	}
	covered := map[string]bool{}
	inspected := 0
	for _, line := range lines {
		cpUsageAssertNoDisjunction(t, line)
		argv := cpUsageArgv(line)
		if len(argv) < 2 {
			t.Errorf("Usage line %q names no subcommand", line)
			continue
		}
		if argv[0] != CliVerbCheckpoint {
			// The shared page prints the THREE-LEVEL spelling
			// (`gopi session checkpoint list …`) — the checkpoint word sits at
			// argv[1] there. Read it wherever it sits: the alternative was this
			// whole gate going silently blind (measured at HEAD: `only 0
			// checkpoint Usage line(s) were inspected` on a page that prints
			// two of them), because every line failed this test and was
			// skipped as a sibling verb's synopsis.
			if len(argv) < 3 || argv[1] != CliVerbCheckpoint {
				// The shared page also prints the SIBLING verb's synopsis
				// (`gopi session resume …`); that line is pinned by the resume
				// group's own gate (TestG4EveryAdvertisedSurfaceRuns drives it
				// for real). The dispatcher asserted HERE is the checkpoint
				// switch.
				continue
			}
			argv = argv[1:]
		}
		inspected++
		verb := argv[1]
		if verb == cpUsageHelpSpelling {
			// The page's own help door (`gopi session checkpoint -h`). It is
			// real and it exits 0, but it is not a SUB-VERB: the known set is
			// the dispatcher's sub-command switch, and the family's hierarchy
			// guard (cli_help_hierarchy_reverse_guard_test.go) owns every page's
			// door. Folded into the known set instead, it would fail on a page
			// that is correct; skipped, the two LIVE sub-verbs still have to be
			// printed and to work.
			continue
		}
		covered[verb] = true
		t.Run(strings.Join(argv, "_"), func(t *testing.T) {
			stdout, stderr, code := fx.sb.g4RunSplit(t, argv...)
			out := stdout + stderr
			if strings.Contains(out, cpUnknownSubcommand) {
				t.Fatalf("`gopi %s` is printed under checkpointUsage's `%s:` "+
					"heading and the dispatcher REFUSES it (exit %d):\n%s\n"+
					"The usage block is the dispatcher's contract: make the line a "+
					"real, forwarded path, or take it off the page.",
					strings.Join(argv, " "), cpUsageSectionUsage, code, out)
			}
			// The `list` line is a complete invocation: with the fixture's own
			// raw dir it must actually list. Asserting the effect here is what
			// keeps this gate from passing on an argv that dies silently.
			if verb == cpUsageSubcmdList && code != 0 {
				t.Fatalf("`gopi %s` is the page's canonical listing line and exited "+
					"%d:\n%s", strings.Join(argv, " "), code, out)
			}
		})
	}
	if inspected < cpUsageMinInspected {
		t.Fatalf("only %d checkpoint Usage line(s) were inspected — the derivation "+
			"is blind", inspected)
	}
	// Both directions: accepted-but-printed-nowhere is undiscoverable;
	// printed-but-not-accepted is the bug this file exists for.
	for verb := range accepted {
		if !covered[verb] {
			t.Errorf("`gopi %s %s` dispatches (and completion offers it) but no "+
				"Usage line of the page prints it — an accepted subcommand nobody "+
				"can discover", CliVerbCheckpoint, verb)
		}
	}
	for verb := range covered {
		if !accepted[verb] {
			t.Errorf("the page prints `gopi %s %s` but the dispatcher's known set "+
				"%v does not carry it", CliVerbCheckpoint, verb, checkpointCompletionNames)
		}
	}
}

// TestCheckpointUsageDisjunctionIsRefused is the syntax rule's own red proof
// (Rule 5): it drives cpUsageDisjunctiveField with the line the page USED to
// print — the one master ce507a8f withdrew because the dispatcher refused it —
// and with the page's live lines, which must be clean.
//
// The REFUSED spelling is reproduced here as a HISTORICAL fixture, and the case
// for that is measured rather than stylistic: master's own inventory doc records
// the refused invocation, and the withdrawal is the change this page's rule
// encodes. A guard whose red case is never reproduced is a guard that could be
// satisfied by never being reachable.
func TestCheckpointUsageDisjunctionIsRefused(t *testing.T) {
	// RED: the checkpoint page's withdrawn third synopsis, verbatim from
	// devdoc/gopi-session-help-surface-inventory.md ("A synopsis the dispatcher
	// refused"). It is an alternation in the subcommand slot, which is why the
	// dispatcher answered `unknown subcommand "list|export"`.
	const cpUsageWithdrawnLine = "gopi session checkpoint list|export [--dir DIR]"
	if got := cpUsageDisjunctiveField(cpUsageWithdrawnLine); got != cpUsageSubcmdList+cpUsageDisjunction+cpUsageSubcmdExport {
		t.Fatalf("cpUsageDisjunctiveField(%q) = %q, want %q — the guard no longer "+
			"catches the shorthand this page had to withdraw, so it cannot protect "+
			"the property the withdrawal rests on",
			cpUsageWithdrawnLine, got, cpUsageSubcmdList+cpUsageDisjunction+cpUsageSubcmdExport)
	}

	// GREEN: the page's live synopses carry no alternation — one line, one
	// command. Driven through the same derivation the gate uses, so this test
	// cannot pass on a page the gate would reject (or vice versa).
	lines := cpUsageCommandLines(checkpointUsage)
	if len(lines) < cpUsageMinInspected {
		t.Fatalf("the derivation found only %d Usage line(s) in checkpointUsage", len(lines))
	}
	for _, line := range lines {
		if got := cpUsageDisjunctiveField(line); got != "" {
			t.Errorf("the live page prints %q in %q — a synopsis must name ONE "+
				"invocation, and the dispatcher does not answer an alternation", got, line)
		}
	}

	// WHY THE RULE IS CHECKED ON THE RAW LINE AND NOT ON THE DERIVED ARGV. The
	// derivation substitutes every `<...>` placeholder with the probe's
	// unresolvable target, so a disjunction hidden in a placeholder reaches the
	// dispatcher as a word the page prints NOWHERE. This pins that lossiness, so
	// a later reader who is tempted to move the guard after cpUsageArgv can see
	// what it would stop catching.
	const cpUsageDisjunctivePlaceholder = "gopi session checkpoint <" + cpUsageSubcmdList + cpUsageDisjunction + cpUsageSubcmdExport + ">"
	derived := cpUsageArgv(cpUsageDisjunctivePlaceholder)
	if len(derived) == 0 || derived[len(derived)-1] != cpUsageProbeTarget {
		t.Fatalf("cpUsageArgv(%q) = %v — want a trailing %q: the derivation "+
			"substitutes the placeholder, which is exactly why the disjunction "+
			"guard must read the RAW synopsis",
			cpUsageDisjunctivePlaceholder, derived, cpUsageProbeTarget)
	}
	if cpUsageDisjunctiveField(cpUsageDisjunctivePlaceholder) == "" {
		t.Errorf("the raw-line guard missed a disjunction inside a placeholder — " +
			"the substitution above would have hidden it from the dispatcher probe")
	}
}

// TestCheckpointResumeLineIsRefusedByName is the REMOVED sub-verb's contract

// (epic verb-restructure, 2026-10-05). It replaces the forwarded-face test that
// used to sit here: `checkpoint resume` and `session resume` were the same
// continuation reachable two ways, and the decision was to keep ONE spelling
// (`gopi session resume <log>`, also the top-level `gopi session resume <log>`).
//
// WHAT IS ASSERTED, and why each half matters:
//
//   - the word still ARRIVES here and the refusal NAMES ITS HOME (both the
//     session-family spelling and the top-level one). A removed sub-verb that
//     fell through to "unknown subcommand" would leave the operator with a
//     pointer to nothing; the refusal is a forwarding ADDRESS, not an error.
//   - it is NOT OBEYED: the exit is non-zero and the resume path is never
//     reached (no resume usage, no resolver refusal) — because obeying it would
//     resume from a snapshot under a name the page no longer teaches.
//   - it is NOT IGNORED: every shape of the spelling is exercised (bare, with a
//     target, with a continuation tail) and each must produce the refusal — a
//     removed sub-verb read as `list` with a positional would exit 0 (the
//     silent-typo class this group's own flag refusals exist for).
func TestCheckpointResumeLineIsRefusedByName(t *testing.T) {
	fx := g4NewFixture(t)
	for _, rest := range [][]string{
		{checkpointSubcmdResume},
		{checkpointSubcmdResume, cpUsageProbeTarget},
		{checkpointSubcmdResume, cpUsageProbeTarget, "continue please"},
	} {
		argv := append([]string{CliVerbCheckpoint}, rest...)
		stdout, stderr, code := fx.sb.g4RunSplit(t, argv...)
		out := stdout + stderr
		if code == 0 {
			t.Errorf("`gopi %s` exited 0 — a removed sub-verb must not be obeyed:\n%s",
				strings.Join(argv, " "), out)
		}
		if !strings.Contains(out, cpResumeRemovedPointer) {
			t.Errorf("`gopi %s` must be refused with the pointer to its one live spelling (%q):\n%s",
				strings.Join(argv, " "), cpResumeRemovedPointer, out)
		}
		if strings.Contains(out, cpUnknownSubcommand) {
			t.Errorf("`gopi %s` fell through to the generic unknown-subcommand refusal — "+
				"a word this group RETIRED must be answered by name, or the operator "+
				"cannot tell a removal from a typo:\n%s", strings.Join(argv, " "), out)
		}
		// The pointer must name the word that was removed AND the spelling that
		// actually runs. It must NOT name the RETIRED TOP-LEVEL spelling as a
		// live one: since verb-retirement phase 1 `gopi resume <log>` is itself
		// refused, so a pointer offering it sends the reader to a second
		// refusal.
		if !strings.Contains(cpResumeRemovedPointer, CliVerbResume) ||
			!strings.Contains(cpResumeRemovedPointer, "gopi "+CliVerbSession+" "+CliVerbResume) {
			t.Errorf("`gopi %s`: the refusal must name the removed word (%s) and its live spelling (`%s %s`):\n%s",
				strings.Join(argv, " "), CliVerbResume, CliVerbSession, CliVerbResume, out)
		}
		if strings.Contains(cpResumeRemovedPointer, "gopi "+CliVerbResume+" ") {
			t.Errorf("`gopi %s`: the refusal offers the RETIRED top-level spelling `gopi %s <log>` as a live one — "+
				"that command is refused by name now, so the pointer leads to a second refusal:\n%s",
				strings.Join(argv, " "), CliVerbResume, out)
		}
	}
}

// TestCheckpointListAndExportUnchanged is the "keep working unchanged" half of
// the acceptance: the two pre-existing surfaces still reach their own code path
// with the same observable output.
func TestCheckpointListAndExportUnchanged(t *testing.T) {
	fx := g4NewFixture(t)
	t.Run("list prints the raw-log table for the fixture's own dir", func(t *testing.T) {
		stdout, stderr, code := fx.sb.g4RunSplit(t, CliVerbCheckpoint, cpUsageSubcmdList,
			"--dir", filepath.Dir(fx.raw))
		if code != 0 {
			t.Fatalf("`gopi session checkpoint %s --dir <real dir>` exited %d:\n%s%s",
				cpUsageSubcmdList, code, stdout, stderr)
		}
		if !strings.Contains(stdout, "reqs") {
			t.Errorf("the listing header is gone:\n%s", stdout)
		}
	})
	t.Run("export prints the checkpoint JSON of the fixture's raw log", func(t *testing.T) {
		stdout, stderr, code := fx.sb.g4RunSplit(t, CliVerbCheckpoint, cpUsageSubcmdExport, fx.raw)
		if code != 0 {
			t.Fatalf("`gopi session checkpoint %s <fixture raw log>` exited %d:\n%s%s",
				cpUsageSubcmdExport, code, stdout, stderr)
		}
		if !strings.Contains(stdout, `"model"`) {
			t.Errorf("the exported checkpoint body is gone:\n%s", stdout)
		}
	})
	t.Run("export of an absent log is still refused by name", func(t *testing.T) {
		_, stderr, code := fx.sb.g4RunSplit(t, CliVerbCheckpoint, cpUsageSubcmdExport, cpUsageProbeTarget)
		if code == 0 {
			t.Fatalf("`gopi session checkpoint %s %s` exited 0 for a log that does not exist",
				cpUsageSubcmdExport, cpUsageProbeTarget)
		}
		if !strings.Contains(stderr, "no raw log matching") {
			t.Errorf("export must keep naming the unresolvable target:\n%s", stderr)
		}
	})
}
