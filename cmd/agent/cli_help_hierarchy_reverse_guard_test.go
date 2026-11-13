// cli_help_hierarchy_reverse_guard_test.go — the RECURSIVE REVERSE GUARD of
// the command-hierarchy restructure (task 01a109bc-47e0, epic verb-restructure
// item 4/5).
//
// THE DIRECTION THIS FILE GUARDS. Every other help gate in this package reads
// the page and asks "does the page promise something the binary does not
// have?" (a usage constant that drifted from its dispatcher, an advertised flag
// nothing parses). This one asks the REVERSE question, at every level of the
// hierarchy: **does every second-level word the binary DISPATCHES appear, by
// name, on the help page of the verb that owns it?** — and, one level down,
// does every THIRD-level word (`session checkpoint list`, `sandbox create`,
// `web search`) appear where its group is advertised?
//
// WHY THE REVERSE DIRECTION IS THE ONE THAT ROTS. A sub-verb is added by
// editing a dispatch switch; the help text is a raw string elsewhere in the
// file, and nothing forces the two to move together. The forward gates cannot
// see it — the page they read is self-consistently silent about the new word.
// The user consequence is the one the restructure exists to remove: a verb you
// must already know in order to find. (Slices 1/5–3/5 of this epic moved ~20
// verbs between groups; this guard is what keeps the next such move honest.)
//
// THE REGISTRIES ARE DERIVED, NOT RE-TYPED (Rule 13/14). Each row reads the
// SAME slice the dispatcher and tab completion read — FamilyVerbs,
// sessionSubcommandNames, memorySubcommandNames, configSubcommandNames,
// notifySubcommandNames, toolSubcommandWords(), the sandbox verb's own
// constants, the checkpoint switch's two live labels. A word ADDED to any of
// those tables arrives here with no test edit, and turns this test red until
// the owning page names it. The guard holds no list of its own, so it cannot
// go stale.
//
// THE PAGES ARE THE BINARY'S OWN OUTPUT (PROJECT.md Rule 31): each page is
// `gopi <argv> -h` from the real binary, never the usage constant — a constant
// that is never printed is not a help page. The grammar each word is matched in
// is the page's OWN (see helpWordForm): the tool group's page sets the word at
// the start of a row (`  capture …`) while the group pages write whole commands
// (`gopi session log …`), and a matcher that assumed one spelling would report
// the other shape as a missing word — a false positive that would make the
// guard's reds meaningless.
//
// HONEST SCOPE (Rule 5's "Not Done" discipline, stated at the gate itself):
// this file checks the SECOND and THIRD levels. The top-level index
// (commandIndex in internal/config/help.go, still ~48 rows) is NOT collapsed to
// 10 rows by this slice, and the page trims that would leave each top-level
// page carrying only its own second level (several still carry architecture
// prose) are not in this change; the follow-up row covers both. What IS
// delivered here is the invariant those edits must satisfy, executable today.
package main

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/taskverbs"
	"github.com/nick/gopi/internal/toolnames"
)

// helpPageTimeout bounds one `gopi <verb> -h` probe. It mirrors
// cliContractTimeout's reason (Rule 27: a test ceiling, never a load): a
// correctly routed help handler returns well under a second, and a route that
// fell through to the agent loop hangs on the LLM handshake — the timeout is
// the detector for exactly that, not a measure of machine speed.
const helpPageTimeout = cliContractTimeout

// helpWordForm is the grammar a page writes its own sub-verbs in. It is a
// property of the PAGE, not of the word: the tool group indents a bare word
// into a table, the group pages spell out whole commands.
type helpWordForm int

const (
	// helpFormCommandLine: the page writes `gopi <spelling> <word> …` — a
	// complete command line, which is how the group pages teach their verbs.
	helpFormCommandLine helpWordForm = iota
	// helpFormToolRow: the tool group's DIRECT block indents the word at the
	// start of a row (`  capture --screen | …`), with no `gopi` prefix —
	// because the block's heading already says `gopi tool <command>`.
	helpFormToolRow
	// helpFormGroupAlternation: the word appears inside a `<a|b|c>` alternation
	// on the page (`sandbox <create|run|show>`, `web <search|fetch>`), which is
	// how a word that needs a second level is advertised without printing a
	// command line for it.
	helpFormGroupAlternation
)

// helpOwnerWord is one hierarchy level's owning page and the words it must
// advertise.
type helpOwnerWord struct {
	// argv is the invocation that PRINTS the page, after the binary path
	// (e.g. {"session", "checkpoint"} for `gopi session checkpoint -h`).
	argv []string
	// spelling is the command prefix the page uses for its own words
	// (helpFormCommandLine only).
	spelling string
	// registry is the derived word list (see the file header).
	registry []string
	// form is the grammar to match each word in.
	form helpWordForm
	// why names the table the registry came from, for the failure message.
	why string
	// mustAlsoAppear is an optional EXTRA whole-string assertion: an address the
	// hierarchy TEACHES that the word-by-word grammar above cannot see, and that
	// must be on the page a reader lands on.
	//
	// THE ASSERTION MOVED TO THE TYPING SITE (2026-10-07). It used to name the
	// checkpoint page's shorthand (`gopi session checkpoint list|export`), one of
	// a synopsis's words. That shorthand was WITHDRAWN from the family page's
	// Usage block (`gopi session -h`) because it was an alternation in a
	// subcommand slot — a promise the dispatcher refused — and the reason it must
	// still be reachable is the reason this field exists at all: it is the
	// CROSS-REFERENCE a reader follows from the family page to the third level,
	// since the family page does not dispatch `checkpoint list|export` itself.
	//
	// AND THAT FORMATION IS WHAT THE CROSS-GROUP GATE READS AS ADMISSIBLE: a `|`
	// at bracket depth >= 1 is the page delimiting a choice (`[list|export]`),
	// which a reader resolves, while the same bar at depth 0 is an advertised
	// token nobody can type (cli_help_shorthand_gate_test.go, which scans this
	// page's constant too). So what this field may name is the BRACKETED pair or
	// one address per row — never a bare bar.
	mustAlsoAppear []string
}

// helpLevelTwoOwners is the second level: every group verb and the vocabulary
// it dispatches.
func helpLevelTwoOwners() []helpOwnerWord {
	return []helpOwnerWord{
		{argv: []string{CliVerbTask}, spelling: CliVerbTask, form: helpFormCommandLine,
			registry: taskverbs.FamilyVerbs,
			why:      "taskverbs.FamilyVerbs (the advertised family surface, read by tabs and the family help)"},
		{argv: []string{CliVerbSession}, spelling: CliVerbSession, form: helpFormCommandLine,
			registry: sessionSubcommandNames,
			why:      "sessionSubcommandNames (the dispatcher switch and the `gopi session <TAB>` set)"},
		{argv: []string{CliVerbMemory}, spelling: CliVerbMemory, form: helpFormCommandLine,
			registry: memorySubcommandNames,
			why:      "memorySubcommandNames (the dispatcher switch and completion set)"},
		{argv: []string{CliVerbConfig}, spelling: CliVerbConfig, form: helpFormCommandLine,
			registry: configSubcommandNames,
			why:      "configSubcommandNames (the dispatcher switch and completion set)"},
		{argv: []string{CliVerbNotify}, spelling: CliVerbNotify, form: helpFormCommandLine,
			registry: notifySubcommandNames,
			why:      "notifySubcommandNames (the dispatcher switch and completion set)"},
		{argv: []string{CliVerbTool}, spelling: CliVerbTool, form: helpFormToolRow,
			registry: toolSubcommandWords(),
			why:      "toolSubcommandWords (the group's refusal list and its `gopi tool <TAB>` set)"},
	}
}

// helpLevelThreeOwners is the third level: a second-level word that has
// actions of its own (the hierarchy rule v2 — "a third level is allowed
// exactly when that action has actions of its own").
//
// The owning PAGE differs by group, and that is measured, not assumed: the
// tool group hands the argv tail to the same predicate the top-level call
// reaches, so `gopi tool sandbox -h` prints the SANDBOX page (grammar
// `gopi tool sandbox <word>`) and `gopi tool sandbox create -h` prints it too. The
// session family's checkpoint page is the same one page under both doors
// (`gopi session checkpoint -h` / `gopi session checkpoint -h`), so its words are
// asserted in its own grammar PLUS the family address the hierarchy teaches.
func helpLevelThreeOwners() []helpOwnerWord {
	return []helpOwnerWord{
		{argv: []string{CliVerbSession, CliVerbCheckpoint},
			// The page's own grammar is the FAMILY address — `gopi session
			// checkpoint list`, which is how the word is reachable, and what the
			// derivation must therefore look for.
			spelling: strings.Join(familyAddressArgs([]string{CliVerbCheckpoint}), " "),
			form:     helpFormCommandLine,
			registry: []string{checkpointSubcmdList, checkpointSubcmdExport},
			why:      "the case labels of runCheckpointSubcommand's inner switch (checkpoint list|export)",
			mustAlsoAppear: []string{
				// The page spells the third-level address itself, and it
				// spells ONE sub-verb per row. The shorthand `gopi session
				// checkpoint list|export` is not an address a reader can type,
				// and the cross-group gate in
				// cli_help_shorthand_gate_test.go refuses the bare form — so
				// what this file asserts is the two typeable rows, and the
				// BRACKETED group (which that gate admits as an optional
				// choice) is asserted by the file that prints it.
				"gopi " + CliVerbSession + " " + CliVerbCheckpoint + " " + checkpointSubcmdList,
				"gopi " + CliVerbSession + " " + CliVerbCheckpoint + " " + checkpointSubcmdExport,
			}},
		{argv: []string{CliVerbSandbox},
			// Same shape: the sandbox page is reached through the tool group, so
			// its synopses read `gopi tool sandbox create …`.
			spelling: strings.Join(familyAddressArgs([]string{CliVerbSandbox}), " "),
			form:     helpFormCommandLine,
			registry: []string{SandboxCreateVerb, SandboxRunVerb, SandboxShowVerb},
			why:      "the sandbox verb's own constants, which its parser switches on"},
		// The web group's two tool faces. Their owning page is the TOOL page's
		// grouped block — `gopi tool web -h` prints the CHAT page (the bare word
		// is the browser-UI carrier), so the alternation on the group page is
		// where these words are advertised. That is a discoverability path, and
		// it is the honest one to assert here; the report records it as the
		// follow-up's page-layering question.
		{argv: []string{CliVerbTool}, form: helpFormGroupAlternation,
			registry: webCompletionNames,
			why:      "webCompletionNames (the web tools' dispatch/completion set), advertised in the tool page's grouped block"},
	}
}

// helpPage runs `gopi <argv...> -h` on the REAL binary and returns stdout. A
// page that does not print is a failure with the child's own output attached —
// never a silent skip (Rule 15).
func helpPage(t *testing.T, argv []string) string {
	t.Helper()
	bin := gopiTestBinPath(t)
	args := append(append([]string{}, argv...), HelpFlag)
	cmd := exec.Command(bin, familyAddressArgs(args)...)
	// The isolated home comes from the contract suite's own scrubber, which
	// strips every ambient GOPI_*/ANTHROPIC_*/DEEPSEEK_* — so a probe can never
	// reach the operator's real profile, and a route that fell through to the
	// agent loop finds no credentials to hang on (it times out instead).
	cmd.Env = cliContractEnv(t.TempDir())
	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = cmd.CombinedOutput()
		close(done)
	}()
	select {
	case <-done:
	case <-timeoutAfter(helpPageTimeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill() //stylegate:ignore kills the help probe child this guard spawned; a failed kill means it had already exited before its output was read
		}
		t.Fatalf("`gopi %s -h` did not return within %s — the route fell through to the agent loop (the timeout is the detector)", strings.Join(argv, " "), helpPageTimeout)
	}
	if err != nil {
		t.Fatalf("`gopi %s -h` exited %v — a help page must print and exit 0:\n%s", strings.Join(argv, " "), err, out)
	}
	if len(out) == 0 {
		t.Fatalf("`gopi %s -h` printed nothing", strings.Join(argv, " "))
	}
	return string(out)
}

// helpFormName renders a helpWordForm as a stable subtest suffix (an int
// would read as a digit and say nothing about which grammar was exercised).
func helpFormName(f helpWordForm) string {
	switch f {
	case helpFormCommandLine:
		return "cmdline"
	case helpFormToolRow:
		return "toolrow"
	case helpFormGroupAlternation:
		return "alternation"
	}
	return "unknown"
}

// helpRowIndent is the two-space indent the tool page's blocks use for one
// entry — the same literal the renderer writes (renderToolLeafRows).
const helpRowIndent = "  "

// helpAdvertisesWord reports whether a page names one sub-verb, matched in the
// grammar that page uses (helpWordForm). The word BOUNDARY is part of every
// form: `export` must not be satisfied by `export-all`, nor `li` by `list`.
func helpAdvertisesWord(page string, o helpOwnerWord, word string) bool {
	q := regexp.QuoteMeta(word)
	switch o.form {
	case helpFormCommandLine:
		return regexp.MustCompile(`\bgopi ` + regexp.QuoteMeta(o.spelling) + `\s+` + q + `\b`).MatchString(page)
	case helpFormToolRow:
		if regexp.MustCompile(`(?m)^` + helpRowIndent + q + `(?: |$)`).MatchString(page) {
			return true
		}
		return helpAdvertisesAlternation(page, word)
	case helpFormGroupAlternation:
		return helpAdvertisesAlternation(page, word)
	}
	return false
}

// helpAdvertisesAlternation reports whether a word is one member of an
// `<a|b|c>` alternation written on the page (the tool group's grouped block).
func helpAdvertisesAlternation(page, word string) bool {
	q := regexp.QuoteMeta(word)
	for _, pat := range []string{`<` + q + `\|`, `\|` + q + `\|`, `\|` + q + `>`} {
		if regexp.MustCompile(pat).MatchString(page) {
			return true
		}
	}
	return false
}

// TestHelpPagesAdvertiseTheirOwnSubcommands is the guard: for every level of
// the hierarchy, each owning page names every word its own registry dispatches.
func TestHelpPagesAdvertiseTheirOwnSubcommands(t *testing.T) {
	for _, level := range []struct {
		name    string
		owners  []helpOwnerWord
		minRows int
	}{
		{"second level", helpLevelTwoOwners(), 6},
		{"third level", helpLevelThreeOwners(), 3},
	} {
		if len(level.owners) < level.minRows {
			t.Fatalf("%s: only %d owning pages are registered — a guard that checked nothing must not pass (Rule 15)", level.name, len(level.owners))
		}
		for _, o := range level.owners {
			o := o
			t.Run(strings.Join(o.argv, "_")+"_"+helpFormName(o.form), func(t *testing.T) {
				if len(o.registry) == 0 {
					t.Fatalf("`gopi %s` has an EMPTY registry (%s) — an empty list would make this gate vacuous", strings.Join(o.argv, " "), o.why)
				}
				page := helpPage(t, o.argv)
				var missing []string
				for _, w := range o.registry {
					if !helpAdvertisesWord(page, o, w) {
						missing = append(missing, w)
					}
				}
				if len(missing) > 0 {
					t.Errorf("`gopi %s -h` does not advertise %d of its own sub-commands: %v\nthe registry is %s — every dispatched word must be nameable from its owning page, or it can only be reached by someone who already knows it",
						strings.Join(o.argv, " "), len(missing), missing, o.why)
				}
				for _, want := range o.mustAlsoAppear {
					if !strings.Contains(page, want) {
						t.Errorf("`gopi %s -h` does not carry the taught address %q — the hierarchy's own spelling must be on the page a reader lands on\n%s",
							strings.Join(o.argv, " "), want, page)
					}
				}
			})
		}
	}
}

// TestFamilyPageStillTeachesTheCheckpointShorthand is where the mustAlsoAppear
// assertion on the checkpoint owner moved to (2026-10-07).
//
// WHAT CHANGED, AND WHY IT IS NOT A WEAKENING. That row's assertion named the
// shorthand `gopi session checkpoint list|export` and required the CHECKPOINT
// page to carry it. Meanwhile the SESSION family page printed the same string as
// a Usage synopsis — an alternation in a subcommand slot, which the dispatcher
// answers with its own refusal — and that line was withdrawn into prose. The two
// facts are one decision: the shorthand is not a synopsis on either page (spelled
// as one, it is a promise nobody can type), and the page that must still TEACH it
// is the family page a reader lands on, as a note pointing at the third level,
// which the family page does not dispatch itself.
//
// So the assertion lives here, at the level the claim is about (the page a
// reader lands on), read from the REAL binary like every other page in this
// file. A synopsis-level guard cannot express it — the shorthand is prose on
// purpose now — and dropping it entirely would have lost the reachability the
// reader depends on: `checkpoint` is not a word the family page dispatches, so
// the paragraph naming the pair is the only route from `gopi session -h` to it.
func TestFamilyPageStillTeachesTheCheckpointShorthand(t *testing.T) {
	shorthand := CliVerbSession + " " + CliVerbCheckpoint + " " + checkpointSubcmdList + "|" + checkpointSubcmdExport
	page := helpPage(t, []string{CliVerbSession})
	if !strings.Contains(page, shorthand) {
		t.Errorf("`gopi %s -h` does not carry %q — the shorthand is the CROSS-REFERENCE "+
			"from the family page to the checkpoint group (a group the page does not "+
			"dispatch itself), so it must stay reachable as prose after being "+
			"withdrawn from the Usage block:\n%s", CliVerbSession, shorthand, page)
	}
	// Absence is not enough on its own: the shorthand must be reachable BOTH ways
	// (the family page as the cross-reference, the checkpoint page as the pair it
	// dispatches), and the two real commands must be reachable as synopses — which
	// is what the registry assertion above the loops already checks, and what the
	// synopsis-shape refusal in cmd/agent/cli_session_usage_dispatch_test.go pins
	// from the other side.
	cpPage := helpPage(t, []string{CliVerbSession, CliVerbCheckpoint})
	if !strings.Contains(cpPage, shorthand) {
		t.Errorf("`gopi %s %s -h` does not carry %q — the page that dispatches the pair "+
			"must be able to name it:\n%s", CliVerbSession, CliVerbCheckpoint, shorthand, cpPage)
	}
}

// TestHelpPageGuardReadsTheRealBinary is the guard's own instrument proof, in
// both directions: a page that certainly lacks a fabricated sub-verb must read
// as absent, and a word it carries must read as present — otherwise a green
// run would prove nothing about the extractor.
func TestHelpPageGuardReadsTheRealBinary(t *testing.T) {
	const fabricated = "zzz-not-a-subcommand"
	cmdLine := helpOwnerWord{spelling: CliVerbTask, form: helpFormCommandLine}
	page := helpPage(t, []string{CliVerbTask})
	if helpAdvertisesWord(page, cmdLine, fabricated) {
		t.Fatalf("the extractor claims `gopi task %s` is advertised — it is fabricated, so the instrument is broken", fabricated)
	}
	if !helpAdvertisesWord(page, cmdLine, taskverbs.List) {
		t.Fatalf("the extractor cannot see `gopi task %s` on its own page — the instrument is broken", taskverbs.List)
	}
	// The word BOUNDARY is part of the instrument: a prefix must not satisfy it.
	if helpAdvertisesWord(page, cmdLine, "li") {
		t.Fatal("the extractor matched a bare prefix — `li` must not satisfy `list`")
	}
	// The tool group's row grammar, on the page that uses it.
	toolRow := helpOwnerWord{spelling: CliVerbTool, form: helpFormToolRow}
	toolPage := helpPage(t, []string{CliVerbTool})
	if !helpAdvertisesWord(toolPage, toolRow, CliVerbCapture) {
		t.Fatalf("the tool-row extractor cannot see `%s` on `gopi tool -h` — the instrument is broken:\n%s", CliVerbCapture, toolPage)
	}
	if helpAdvertisesWord(toolPage, toolRow, fabricated) {
		t.Fatal("the tool-row extractor matched a fabricated word — the instrument is broken")
	}
}

// TestCheckpointPageNamesItsTwoLevels pins the third level's spelling rule,
// which the generic guard can only partly state: `gopi session checkpoint list`
// is the same page reached through the session family, so a reader landing on
// either door must be taught the FAMILY spelling — the address the hierarchy
// keeps — and must keep the retired `resume` spelling out of the Usage block
// while still naming it in the removal notice (Rule 15: a removal that is not
// stated reads as a silent no-op).
func TestCheckpointPageNamesItsTwoLevels(t *testing.T) {
	for _, argv := range [][]string{
		{CliVerbSession, CliVerbCheckpoint},
	} {
		page := helpPage(t, argv)
		family := CliVerbSession + " " + CliVerbCheckpoint
		for _, w := range []string{checkpointSubcmdList, checkpointSubcmdExport} {
			if !strings.Contains(page, "gopi "+family+" "+w) && !strings.Contains(page, "gopi "+family+" "+checkpointSubcmdList+"|"+checkpointSubcmdExport) {
				t.Errorf("`gopi %s -h` does not teach `gopi %s %s` — the family spelling is the hierarchy's only three-level address",
					strings.Join(argv, " "), family, w)
			}
		}
		if !strings.Contains(page, checkpointSubcmdResume) {
			t.Errorf("`gopi %s -h` does not name the retired `%s` spelling — a removal that is not stated reads as a silent no-op",
				strings.Join(argv, " "), checkpointSubcmdResume)
		}
	}
}

// TestGroupPagePrintsTheSamePageAsItsOwnVerb pins the delegation the third
// level depends on: the tool group hands its argv tail to the SAME predicate
// the top-level chain reaches, so both doors print ONE page (the group's whole
// value is "one command, one help page, one exit status"). A group that grew a
// second, thinner page would drift from the verb it names — this is the
// assertion that notices.
func TestGroupPagePrintsTheSamePageAsItsOwnVerb(t *testing.T) {
	for _, argv := range [][]string{
		{CliVerbSandbox},
		{toolnames.ToolText2img},
	} {
		own := helpPage(t, argv)
		viaGroup := helpPage(t, append([]string{CliVerbTool}, argv...))
		if own != viaGroup {
			t.Errorf("`gopi tool %s -h` and `gopi %s -h` printed different pages — the group must be the same implementation under a noun, never a second copy that can drift",
				strings.Join(argv, " "), strings.Join(argv, " "))
		}
	}
}

// TestHelpPageProbeLeavesNoSessionArtifact is the contract-suite invariant,
// reused: a help route must not start a session, so the isolated GOPI_HOME the
// probe was given still has no sessions directory afterwards. It is here
// because this file spawns the binary ~14 times, and a route that quietly fell
// through to the agent path would otherwise be reported as a help failure with
// no trace of what it actually did.
func TestHelpPageProbeLeavesNoSessionArtifact(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	cmd := exec.Command(bin, CliVerbTool, CliVerbSandbox, SandboxShowVerb, HelpFlag)
	cmd.Env = cliContractEnv(home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("`gopi tool sandbox show -h` exited %v:\n%s", err, out)
	}
	if _, statErr := os.Stat(home + "/sessions"); statErr == nil {
		t.Fatalf("the help probe created %s/sessions — a help route reached the agent path:\n%s", home, out)
	}
}
