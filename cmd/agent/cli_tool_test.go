package main

// cli_tool_test.go — the contract of the `tool` GROUP verb (command-hierarchy
// epic, slice 1/5). Four claims are pinned here, and each one is a way the
// group could quietly stop being what it says it is:
//
//  1. VOCABULARY — every word the group forwards to is a dispatched top-level
//     verb, and the help page lists EXACTLY those words (never one the group
//     would refuse, never a forwardable word the page forgets). Both directions
//     are asserted against the tables themselves, so a word added to one side
//     only fails here.
//  2. FORWARDING — `gopi tool <word> -h` and `gopi <word> -h` are ONE command:
//     the probe runs BOTH on the built binary and compares exit status and
//     output byte for byte. That is the epic's whole requirement ("a pure
//     name→implementation map, nothing implemented twice"), and it is the one
//     claim a source-level check cannot make (Rule 31: run the surface).
//  3. REFUSAL — an unknown word under the group is refused BY NAME, non-zero,
//     and mints nothing: no session directory, hence no ledger row, hence no
//     agent run. The alternative is what this whole file exists to prevent.
//  4. COMPLETION — `gopi tool <TAB>` offers the dispatcher's own vocabulary.
//
// ONE DECLARED EXCEPTION, and it is asserted rather than exempted: `web` is
// both a top-level command (the browser chat carrier) and a row of this group
// (the door to its `search`/`fetch` tool faces). Claim 2's "the bare spelling
// must refuse" is therefore replaced for that ONE word by a stronger claim —
// both spellings must reach the SAME page — and claim 1 keeps asserting the two
// facts that make a second door harmless (dispatched yes, refused no). The
// declaration, its reason and the page that prints it live in cli_tool.go
// (toolGroupSecondDoorWord); the rationale is in devdoc/cli-naming-convention.md.

import (
	"os/exec"
	"strings"
	"testing"
)

// toolForwardedWords is the group's vocabulary as the TABLES declare it — the
// words a reader is told about. The predicate map is the dispatcher's copy of
// the same decision; TestToolGroupVocabularyMatchesHelp pins the two together.
func toolForwardedWords() []string {
	out := make([]string, 0, len(toolSubcommandPredicates))
	for _, c := range toolLeafCommands {
		out = append(out, c.word)
	}
	for _, g := range toolGroupCommands {
		out = append(out, g.word)
	}
	return out
}

// TestToolGroupVocabularyMatchesHelp pins the two directions between the help
// tables and the dispatch map: a word the page advertises must be forwardable
// (else the page promises a refusal), and a word the group forwards to must be
// listed with a description (else the command is invisible on its own page).
func TestToolGroupVocabularyMatchesHelp(t *testing.T) {
	listed := map[string]bool{}
	for _, w := range toolForwardedWords() {
		if listed[w] {
			t.Fatalf("the tool help tables list %q twice — a word is one entry", w)
		}
		listed[w] = true
		if _, ok := toolSubcommandPredicates[w]; !ok {
			t.Errorf("the tool page advertises %q but the group cannot forward to it — the page would promise a refusal", w)
		}
	}
	for w := range toolSubcommandPredicates {
		if !listed[w] {
			t.Errorf("the group forwards to %q but the tool page does not describe it — the command is undiscoverable (`gopi tool -h`)", w)
		}
	}
	if len(listed) < 10 {
		t.Fatalf("the tool vocabulary has %d words — the tables are broken, not the surface", len(listed))
	}
	// The page must state its two blocks by name (the epic's rendering rule:
	// a reader has to be able to tell a direct command from a grouped one).
	for _, want := range []string{
		"Direct commands", "Grouped commands", "internal — gates and scripts",
		CliVerbSandbox + " <" + SandboxCreateVerb,
		CliVerbWeb + " <",
	} {
		if !strings.Contains(toolUsage, want) {
			t.Errorf("`gopi tool -h` is missing %q — the page must split direct from grouped commands and warn about the internal entries", want)
		}
	}
	// Every leaf word appears as its OWN line of the direct block, and every
	// internal one carries the marker.
	for _, c := range toolLeafCommands {
		if !strings.Contains(toolUsage, "  "+c.word+" ") {
			t.Errorf("`gopi tool -h` has no direct-command line for %q", c.word)
		}
		if got := strings.Contains(toolUsageLineContaining(toolUsage, "  "+c.word+" "), "(internal)"); got != c.internal {
			t.Errorf("`gopi tool -h` line for %q: internal marker = %v, want %v", c.word, got, c.internal)
		}
	}
}

// toolUsageLineContaining returns the first line of page containing needle
// (diagnostics only — `lineOf` is taken by peak_guard_removal_test.go).
func toolUsageLineContaining(page, needle string) string {
	for _, l := range strings.Split(page, "\n") {
		if strings.Contains(l, needle) {
			return l
		}
	}
	return ""
}

// TestToolGroupWordsAreTheOnlyDoor is the anti-restatement half, RESTATED by
// the verb-retirement epic (2026-10-05). The old form required every word the
// group forwards to be a top-level verb as well — "one implementation, two
// spellings" — and the operator ruled that arrangement out (不要再残留别名).
// The new form states the SAME invariant from the other side: every word the
// group serves must be REFUSED at top level, so exactly two facts are checked —
//
//	a word the group serves is NOT dispatched (else the refusal would shadow
//	the real command and one of the two would be unreachable), and it IS
//	refused by name (else the bare spelling falls through to the bare-prompt
//	path and buys a real agent run).
//
// A word satisfying neither is a word an operator can lose to the prompt path
// by accident — the accident cli_undispatched_tool_words.go and
// cli_verb_aliases.go exist to prevent.
func TestToolGroupWordsAreTheOnlyDoor(t *testing.T) {
	dispatched := dispatchedVerbWords(t)
	for w := range toolSubcommandPredicates {
		if toolGroupWordIsADeclaredSecondDoor(w) {
			// THE ONE DECLARED EXCEPTION (cli_tool.go's toolGroupSecondDoorWord
			// — its reason is on the group's own page and in
			// devdoc/cli-naming-convention.md). The exemption is not "nothing is
			// asserted about this word": the two facts that make a second door
			// harmless are asserted HERE, and a third test below runs both doors
			// and compares their pages byte for byte.
			//
			//	(a) the word IS still dispatched at top level — an exception
			//	    declared for a word that is no longer dispatched is STALE and
			//	    must be deleted (Rule 39's ratchet, one table over: a permit
			//	    whose class no longer fires is an error, not a leftover);
			//	(b) the word is NOT refused anywhere at argv[0] — a word that is
			//	    both dispatched and refused is exactly the unreachable-command
			//	    defect this test exists for, and the exception may not be used
			//	    to smuggle one past it.
			if !dispatched[w] {
				t.Errorf("the group declares %q a second door, but this binary does NOT dispatch it at top level — the declaration is stale; delete it (a word with no top-level door is simply a normal row of this table)", w)
			}
			if reason := undispatchedToolWordRefusalReason([]string{w}); reason != "" {
				t.Errorf("the group declares %q a second door, but %q IS refused at argv[0] — a word that is both dispatched and refused makes the command unreachable behind its own refusal", w, w)
			}
			continue
		}
		if dispatched[w] {
			t.Errorf("the group forwards to %q, which this binary ALSO dispatches at top level — the local tools have one door; a second one makes one of the two unreachable", w)
		}
		if undispatchedToolWordRefusalReason([]string{w}) == "" {
			t.Errorf("the group forwards to %q, but %q is refused NOWHERE at argv[0] — `gopi %s` would fall through to the bare-prompt path and buy an agent run", w, w, w)
		}
	}
}

// THE DECLARED SECOND DOOR'S TYPED HALF LIVES IN cli_tool.go
// (toolGroupWordIsADeclaredSecondDoor / toolGroupSecondDoorWord). The
// peer branch 01a1144e3f6e… had carried its own copy of the same exemption as
// a TEST-local list (toolDoubleDoorWords + isToolDoubleDoorWord) together with
// TestToolDoubleDoorWordsAreReallyDoubleDoors. Both spellings assert the SAME
// two facts about the SAME word (`web`: still dispatched at top level, still
// served by the group), so at the merge they collided rather than composed —
// the exemption is a fact about the BINARY, and one fact belongs in one place.
// The typed half was kept (it is what the page and the dispatch read); the
// test-local copy and its bespoke test were dropped here, their assertions
// still covered by the typed half's own contracts below:
// TestToolGroupWordsAreTheOnlyDoor (dispatched + not refused) and
// TestToolGroupDeclaredSecondDoorIsPrintedAndPinned (the table, the page, the
// doors). Dropping a duplicate is the point of the merge, not a loss of
// coverage: nothing here asserted a fact the typed half does not.

// TestToolGroupForwardsToTheSameImplementation re-states the group's promise by
// EXECUTION after the retirement, because the old form is no longer
// expressible: it compared `gopi tool <word> -h` with `gopi <word> -h` and
// demanded the same page, and the bare spelling is refused now. Two facts
// replace it, both driven on the real binary:
//
//	the group REACHES a page — exit 0, non-empty output, no session minted; and
//	the bare spelling REFUSES, naming exactly that group spelling — so the
//	refusal cannot drift away from the door it points at.
//
// This is the pair that matters: a forwarder that reached nothing, or an
// address naming a door that does not open, fails here.
// The comment above TestToolGroupWordsAreTheOnlyDoor records the one declared
// exception to the "refused at top level" half. This test drives the OTHER half
// for that word — the claim that replaces "the bare spelling must refuse",
// because for a declared second door the bare spelling must instead reach the
// SAME page the group reaches. That is the whole value of an exception declared
// rather than hidden: what it replaces is asserted, not dropped.
func TestToolGroupForwardsToTheSameImplementation(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	for _, word := range toolForwardedWords() {
		t.Run(word, func(t *testing.T) {
			viaGroup, errGroup := runCLIContractProbeEnv(t, bin, home, CliVerbTool, word, HelpFlag)
			if errGroup != nil || strings.TrimSpace(viaGroup) == "" {
				t.Fatalf("`gopi %s %s %s` exit ok=%v, output %q — the group reached no page", CliVerbTool, word, HelpFlag, errGroup == nil, viaGroup)
			}
			assertNoCLIContractSession(t, CliVerbTool+" "+word, home)
			// RAW: the BARE spelling is the subject.
			direct, errDirect := runCLIContractProbeRaw(t, bin, home, word, HelpFlag)
			if toolGroupWordIsADeclaredSecondDoor(word) {
				// DECLARED SECOND DOOR: the bare word is a live command (the
				// browser chat carrier) and the group is a second spelling of
				// the SAME predicate — never a second page to drift from it. So
				// the observable claim is equality, not refusal.
				if errDirect != nil {
					t.Fatalf("`gopi %s %s` exited %v — this word is the group's declared second door: the bare spelling is a live command and must still work\n--- output ---\n%s", word, HelpFlag, errDirect, direct)
				}
				if direct != viaGroup {
					t.Errorf("`gopi %s %s` and `gopi %s %s %s` printed DIFFERENT pages — a declared second door is only harmless while both spellings reach ONE implementation (one predicate, one page, one exit status)\n--- bare ---\n%s\n--- group ---\n%s",
						word, HelpFlag, CliVerbTool, word, HelpFlag, direct, viaGroup)
				}
				return
			}
			if errDirect == nil {
				t.Fatalf("`gopi %s %s` exited 0 — the bare spelling must refuse now that the group is its only door\n--- output ---\n%s", word, HelpFlag, direct)
			}
			if !strings.Contains(direct, CliVerbTool+" "+word) {
				t.Errorf("`gopi %s %s`: the refusal does not name `gopi %s %s` — the address and the door have drifted apart\n--- output ---\n%s",
					word, HelpFlag, CliVerbTool, word, direct)
			}
		})
	}
}

// TestToolGroupDeclaredSecondDoorIsPrintedAndPinned is the exception's own
// contract, in the three places a silent exception would rot:
//
//	(a) THE TABLE — exactly one word is declared, and it is a word the grouped
//	    table really carries (-h advertises it). A declaration naming a word the
//	    page no longer lists is stale;
//	(b) THE PAGE — `gopi tool -h`, RUN on the real binary, carries the reason,
//	    and the reason is the ONE constant (never a second prose copy that can
//	    drift from the declaration): the reader who asks "why is this row
//	    different" is answered where they ask it;
//	(c) THE DOORS — both spellings reach one implementation, already asserted
//	    word by word above and re-stated here for THIS word so the exception is
//	    pinned even if the vocabulary loop is ever narrowed.
//
// A page with the exception removed prints no paragraph about it
// (toolGroupSecondDoorNote returns ""), so the sentence can never outlive the
// thing it describes.
func TestToolGroupDeclaredSecondDoorIsPrintedAndPinned(t *testing.T) {
	declared := 0
	listed := false
	for _, g := range toolGroupCommands {
		if !toolGroupWordIsADeclaredSecondDoor(g.word) {
			continue
		}
		declared++
		listed = true
	}
	if !listed {
		t.Fatalf("toolGroupWordIsADeclaredSecondDoor names %q, but no grouped row carries it — the declaration and the table disagree",
			toolGroupSecondDoorWord)
	}
	if _, ok := toolSubcommandPredicates[toolGroupSecondDoorWord]; !ok {
		t.Fatalf("the declaration names %q, which the group's forwarder map does not carry — the exception must describe a word this group really serves", toolGroupSecondDoorWord)
	}
	if declared != 1 {
		t.Errorf("%d of the grouped rows are declared second doors, want exactly 1 — a second exception needs its own reason, not a widened predicate", declared)
	}
	if got := toolGroupSecondDoorNote(); got == "" || !strings.Contains(got, toolGroupSecondDoorReason) {
		t.Fatalf("the group's page would carry no reason for its declared second door (note %q) — an exception nobody explains is a silent asymmetry", got)
	}
	// (b) the page is a surface: print it and look.
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	page, err := runCLIContractProbeEnv(t, bin, home, CliVerbTool, HelpFlag)
	if err != nil {
		t.Fatalf("`gopi %s %s` exited %v — the group's own page must print:\n%s", CliVerbTool, HelpFlag, err, page)
	}
	if !strings.Contains(page, toolGroupSecondDoorReason) {
		t.Errorf("`gopi %s %s` does not carry the declared exception's reason %q — the reader who asks why this row differs gets no answer\n--- page ---\n%s",
			CliVerbTool, HelpFlag, toolGroupSecondDoorReason, page)
	}
	// (c) one implementation, read off the two real invocations.
	viaGroup, errGroup := runCLIContractProbeEnv(t, bin, home, CliVerbTool, toolGroupSecondDoorWord, HelpFlag)
	bare, errBare := runCLIContractProbeRaw(t, bin, home, toolGroupSecondDoorWord, HelpFlag)
	if errGroup != nil || errBare != nil {
		t.Fatalf("the two doors of %q exited ok=%v/%v — both must print a page:\n--- group ---\n%s\n--- bare ---\n%s",
			toolGroupSecondDoorWord, errGroup == nil, errBare == nil, viaGroup, bare)
	}
	if viaGroup != bare {
		t.Errorf("`gopi %s %s %s` and `gopi %s %s` printed different pages — the declared second door is only harmless while both spellings are ONE implementation",
			CliVerbTool, toolGroupSecondDoorWord, HelpFlag, toolGroupSecondDoorWord, HelpFlag)
	}
	assertNoCLIContractSession(t, CliVerbTool+" "+toolGroupSecondDoorWord, home)
}

// TestToolGroupUnknownWordIsRefusedByNameNoRun is the acceptance probe for the
// refusal: a word the group does not know exits non-zero, NAMES the word and
// the vocabulary, and mints NOTHING. Without it the misspellings would reach
// the run path (the first argument is taken as the prompt) and cost a real
// agent run — the exact accident cli_undispatched_tool_words.go was written
// for, one level down.
func TestToolGroupUnknownWordIsRefusedByNameNoRun(t *testing.T) {
	bin := gopiTestBinPath(t)
	for _, args := range [][]string{
		{CliVerbTool, "nosuchword"},
		{CliVerbTool, "nosuchword", "-h"},
		{CliVerbTool, "Code-Search"}, // case is not a spelling the group accepts
	} {
		argv := strings.Join(args, " ")
		t.Run(argv, func(t *testing.T) {
			home := t.TempDir()
			out, err := runCLIContractProbeEnv(t, bin, home, args...)
			if err == nil {
				t.Fatalf("gopi %s: exit 0 — an unknown word was accepted\n--- output ---\n%s", argv, out)
			}
			for _, want := range []string{args[1], "gopi " + CliVerbTool, CliVerbCodeSearch} {
				if !strings.Contains(out, want) {
					t.Errorf("gopi %s: the refusal does not mention %q\n--- output ---\n%s", argv, want, out)
				}
			}
			assertNoCLIContractSession(t, argv, home)
		})
	}
}

// TestToolGroupBareWordIsAUsageError pins the one shape that must NOT fall
// through either: `gopi tool` with nothing after it prints the page on stderr
// and exits non-zero, so a script can never read "I forgot the word" as a
// successful (empty) run.
func TestToolGroupBareWordIsAUsageError(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	out, err := runCLIContractProbeEnv(t, bin, home, CliVerbTool)
	if err == nil {
		t.Fatalf("bare `gopi %s` exited 0 — a missing word is a usage error\n--- output ---\n%s", CliVerbTool, out)
	}
	if !strings.Contains(out, toolUsageTitle) {
		t.Errorf("bare `gopi %s` did not print its page\n--- output ---\n%s", CliVerbTool, out)
	}
	assertNoCLIContractSession(t, CliVerbTool, home)
}

// TestToolCompletionNamesAreTheDispatcherVocabulary pins the completion set to
// the group's own map (Rule 13/14 — one definition): tab completion may never
// offer a word this group would refuse.
func TestToolCompletionNamesAreTheDispatcherVocabulary(t *testing.T) {
	if len(toolCompletionNames) != len(toolSubcommandPredicates) {
		t.Fatalf("toolCompletionNames has %d entries, the group forwards to %d — completion must BE the vocabulary", len(toolCompletionNames), len(toolSubcommandPredicates))
	}
	for _, w := range toolCompletionNames {
		if _, ok := toolSubcommandPredicates[w]; !ok {
			t.Errorf("toolCompletionNames offers %q, which the group would refuse — a dead completion candidate", w)
		}
	}
	if !containsString(topLevelCommandNames, CliVerbTool) {
		t.Errorf("topLevelCommandNames omits %q — `gopi <TAB>` cannot reach the group", CliVerbTool)
	}
}

// TestToolUsageTitleIsThePagesFirstLine keeps the contract fingerprint honest:
// cliContractInventory pins toolUsageTitle as `gopi tool -h`'s first line, so
// the constant and the page must not drift apart.
func TestToolUsageTitleIsThePagesFirstLine(t *testing.T) {
	first, _, _ := strings.Cut(toolUsage, "\n")
	if first != toolUsageTitle {
		t.Fatalf("toolUsage's first line is %q, but the fingerprint constant is %q — the contract suite would pin a page nobody prints", first, toolUsageTitle)
	}
}

// TestToolGroupIsDispatchedByMain asserts the argv predicate answers on the
// group word only, and that a leaf word is NOT claimed by it (the top-level
// chain must keep routing `gopi tool shot …` itself — the group is an addition, not
// a replacement).
func TestToolGroupIsDispatchedByMain(t *testing.T) {
	if isToolSubcommand([]string{}) {
		t.Errorf("isToolSubcommand(empty) = true")
	}
	if isToolSubcommand([]string{CliVerbShot}) {
		t.Errorf("isToolSubcommand(%q) = true — a leaf word is not the group", CliVerbShot)
	}
	if isToolSubcommand([]string{CliVerbTool + "x"}) {
		t.Errorf("the predicate must match the whole word, not a prefix")
	}
	// The dispatch chain calls it, in the same argv position as the others.
	preds := dispatchPredicatesInMain(t)
	found := false
	for _, p := range preds {
		if p == "ToolSubcommand" {
			found = true
		}
	}
	if !found {
		t.Fatalf("main.go's dispatch chain does not call isToolSubcommand — the group is unreachable (%v)", preds)
	}
}

// TestToolGroupHasNoSideEffectsInProcess is the cheap unit guard behind the
// forwarding probes: the map's entries must all be the leaves' own predicates
// (a non-nil function), so the group can never silently point at a no-op.
func TestToolGroupHasNoSideEffectsInProcess(t *testing.T) {
	for w, pred := range toolSubcommandPredicates {
		if pred == nil {
			t.Errorf("the tool group maps %q to a nil predicate — the word would be accepted and do nothing", w)
		}
	}
	// Sanity: the binary we probe is the one built from this tree.
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain in PATH — the probes above already ran the built binary")
	}
}
