package main

// cli_tasks_merger_help_atoms_test.go — the guard for the `-h` pages of the git
// atoms that SURVIVE the one-shot merge verb's removal (epic merge-verb-removal,
// item 3; commit 8cf39fb33).
//
// WHY THIS FILE EXISTS. 8cf39fb33 deleted `gopi merge` and its implementation
// (`merge_run.go`, `merge_backstop.go`, `merge_union.go`, 8k LOC), and took the
// three test files that guarded the OLD surface with it —
// cli_tasks_merger_help_surface_test.go (1151 lines),
// cli_tasks_merger_help_note_reader_gate_test.go (620),
// cli_tasks_merger_help_carrier_split_test.go (294). That was the right call for
// most of them: they drove `mergeFaceFlagRows` / `mergeCanonicalOnlyFlag` /
// `mergeCanonicalFlagPointer`, and every one of those symbols died with the
// parser. `grep -c` over the surviving tree returns 0 for all of them, so a
// guard that kept them would not compile against a surface that exists.
//
// WHAT DIED WITH THEM THAT DID NOT HAVE TO. The atom pages themselves SURVIVE —
// cli_tasks_merger_help.go still renders `gopi task merge -h` (from
// mergeAtomFlagLines + taskMergeUsageHead) and the shared pointer page
// mergerAtomUsageHead for the five siblings in taskverbs.MergerVerbs. Those
// pages had four independent properties worth guarding, and after 8cf39fb33 not
// one of them was pinned by anything:
//
//  1. an atom's `-h` is the VERB's own page — not the 20 KB family blob, exit 0,
//     taking no lock and moving no ref. This is the defect class the deleted
//     file's header describes (measured 2026-09-25: all five atoms printed
//     byte-identical tasksUsage);
//  2. the merge page advertises exactly the flags taskFlagMember admits for it —
//     the biconditional, both directions (an advertised-but-refused flag and a
//     admitted-but-hidden flag are the same defect mirrored);
//  3. those spellings come through the mergeAtom*FlagName constants, never a
//     bare literal, so the page and the parser cannot check two different lists;
//  4. the pages POINT at the manual, they do not become a second one.
//
// So this file is the SURVIVING half, rewritten against the surface that is
// actually here. It deliberately does NOT resurrect the canonical-face tests:
// that face is gone, and a source-text assertion over a deleted implementation
// can only be satisfied by keeping the corpse in the tree — exactly what the
// item-3 deletion was for. Where a `-h` page must still say "the canonical face
// is gone", that fact is asserted as the page stating it, not as code existing.
//
// Rule 17: t.Setenv where anything is set. Rule 22/32: every path is a
// t.TempDir(). Rule 10.4: every predicate below carries its own red arm.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/taskverbs"
)

// atomsHelpFlag is the help spelling this file drives: the package's OWN
// constant for the short usage-query word (workerFlagHelp, main.go), never a
// re-typed literal (Rule 1b) — the dispatcher's scan compares against that
// constant, so a page that stopped matching it is a defect this gate must see.
const atomsHelpFlag = workerFlagHelp

// atomsRunShapedFragments is the vocabulary an atom's RUN face prints when it
// actually acts: the counter lines of `gopi task merge` / `audit-merges` /
// `adopt` / `merged-cleanup`. A `-h` page carrying one of these would be
// evidence that the page came from a RUN — which is the whole point of
// measuring the alternative, since the two argvs differ by one token.
//
//nolint:gochecknoglobals // the measured shape of a run, read by the gate
var atomsRunShapedFragments = []string{
	"merged=", "deleted=", "dispatched=", "duped=", "scanned=", "adopted=",
	"removed=", "would-reap=", "submitted=", "skipped=", "backlog audit",
}

// atomsAtomListVerb reads the shared pointer page's atom-list line shape:
// `    gopi task <verb>` (indent exactly 4) with a description on the same
// line, and the verb resolving to a merger atom.
//
// The indent is the discriminator the page itself uses: the SYNOPSIS block
// (indent 2) is the page's own verb, and this list is POINTERS beside it. A
// predicate that ignored indentation would count the page's own synopsis as an
// atom and inflate the surfaced set.
func atomsAtomListVerb(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) != atomsAtomListIndent {
		return "", false
	}
	const head = "gopi task "
	if !strings.HasPrefix(trimmed, head) {
		return "", false
	}
	rest := strings.TrimPrefix(trimmed, head)
	i := strings.Index(rest, " ")
	if i < 0 {
		return "", false
	}
	verb := rest[:i]
	for _, v := range taskverbs.MergerVerbs {
		if v == verb {
			return verb, true
		}
	}
	return "", false
}

// atomsAtomListIndent is the indent the shared page's atom list uses — 4 for the
// row, against 2 for the page's own synopsis block. Named because the predicate
// above turns on the difference (Rule 1b).
const atomsAtomListIndent = 4

// atomsContains is a tiny membership helper (these lists are a handful of
// entries).
func atomsContains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// atomsHelpOffenses is THE PREDICATE: everything a page printed for
// `gopi task <verb> -h` does wrong. `verb` is the atom whose help was asked for.
//
// It is one function so the red arm below can run it over a RE-PLANTED pre-fix
// page and over the real one, without the arm restating any of the checks.
func atomsHelpOffenses(verb, page string) []string {
	var out []string
	if verb == "" {
		return append(out, "no verb was asked about — the gate would be vacuous")
	}
	if strings.TrimSpace(page) == "" {
		return append(out, "the page is EMPTY — a verb with no page at all")
	}
	if cliUsageTitle(page) == cliUsageTitle(tasksUsage) {
		out = append(out, "prints the FAMILY blob ("+cliUsageTitle(tasksUsage)+") instead of the verb's own page — the measured defect: every atom's -h was byte-identical to `gopi task -h`")
	}
	if !strings.Contains(page, verb) {
		out = append(out, "never names the verb that was asked about ("+verb+") — the reader cannot tell which spelling he typed")
	}
	for _, frag := range atomsRunShapedFragments {
		if strings.Contains(page, frag) {
			out = append(out, "carries the run-shaped fragment "+frag+" — a help page must not look like a run's output")
		}
	}
	return out
}

// TestMergerAtomHelpIsItsOwnPagePerVerb is the gate: for every advertised atom,
// the REAL binary answers `-h` with that verb's own page, exits 0, runs nothing,
// and creates no lock.
//
// The surface is DERIVED, not authored: the atoms are read out of the page the
// siblings actually print, so a verb added to the vocabulary is graded the
// moment it is written, and a derivation that went blind fails the floor below
// instead of passing.
func TestMergerAtomHelpIsItsOwnPagePerVerb(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	env := cliContractEnv(home)

	masterBefore := atomsGitRevParse(t, "master")

	siblingPage := taskMergerAtomUsage(taskverbs.Adopt)
	var atoms []string
	for _, line := range strings.Split(siblingPage, "\n") {
		if v, ok := atomsAtomListVerb(line); ok && v != taskverbs.Merger {
			// `merger` is the RETIRED daemon's alias: its -h keeps the one-line
			// pointer and never reaches the page arm, so it is not part of this
			// gate's surface. Its contract (names the replacement, exits 0) is
			// pinned by cli_contracts_test.go — driving it here would re-measure
			// another test's subject and would read a refusal SENTENCE as a help
			// page.
			atoms = append(atoms, v)
		}
	}
	sort.Strings(atoms)
	if len(atoms) < atomsMinAtoms {
		t.Fatalf("the sibling page advertises only %d atoms (%v) — the derivation is blind, so a green run proves nothing", len(atoms), atoms)
	}
	for _, v := range taskverbs.MergerVerbs {
		if v == taskverbs.Merger {
			continue
		}
		if !atomsContains(atoms, v) {
			t.Errorf("the sibling page does not advertise %q, which taskverbs.MergerVerbs declares — the page and the vocabulary disagree", v)
		}
	}

	verbs := append([]string{taskverbs.Merge}, atoms...)
	for _, verb := range verbs {
		t.Run(verb, func(t *testing.T) {
			cmd := exec.Command(bin, "task", verb, atomsHelpFlag)
			cmd.Env = env
			raw, err := cmd.CombinedOutput()
			out := string(raw)
			if err != nil {
				t.Fatalf("`gopi task %s %s` exited non-zero (%v) — a usage query answers whatever the state of the world:\n%s", verb, atomsHelpFlag, err, out)
			}
			for _, offense := range atomsHelpOffenses(verb, out) {
				t.Errorf("`gopi task %s %s`: %s", verb, atomsHelpFlag, offense)
			}
			page := taskMergerAtomUsage(verb)
			if page == "" {
				t.Fatalf("taskMergerAtomUsage(%q) is empty — the advertised atom has no page to print", verb)
			}
			if first, _, _ := strings.Cut(strings.TrimSpace(out), "\n"); first != cliUsageTitle(page) {
				t.Errorf("`gopi task %s %s` opened with %q, want the page's own title %q", verb, atomsHelpFlag, first, cliUsageTitle(page))
			}
			for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
				if w := config.DisplayWidth(line); w > config.HelpWrapCol {
					t.Errorf("`gopi task %s %s` line is %d display cols (> %d): %q", verb, atomsHelpFlag, w, config.HelpWrapCol, line)
				}
			}
		})
	}

	// THE CONTROL FLOOR. A help query takes NO lock and touches no repo: the lock
	// file is the observable ("a help page that created the lock file would
	// already have written into the operator's profile") and master's tip is the
	// other half of the same claim.
	if _, err := os.Stat(filepath.Join(home, config.ProfileDirName, serveLockFilename)); err == nil {
		t.Errorf("%s exists after the %s probes — a usage query must take no lock", serveLockFilename, atomsHelpFlag)
	}
	if now := atomsGitRevParse(t, "master"); now != masterBefore {
		t.Errorf("master moved across the %s probes: %s → %s — a help query must run nothing", atomsHelpFlag, masterBefore, now)
	}
}

// atomsMinAtoms is the floor under the derivation above: the four non-merging
// atoms the list carries. A page that lost rows must be RED, not quietly graded
// over fewer verbs (Rule 15 — a scan that found nothing is not a clean scan).
const atomsMinAtoms = 4

// atomsGitRevParse reads one ref's tip in the repository under test — the
// observable half of "the -h path ran nothing".
func atomsGitRevParse(t *testing.T, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", ref).CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse %s: %v\n%s", ref, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestMergerAtomHelpGateIsNotBlind is the RED ARM, run over the REAL trigger: the
// pre-fix `-h` scan — the code that shipped, with no case for any atom — is
// re-planted here, and the page IT prints must be flagged for every advertised
// atom. The same predicate over the real page must find nothing.
func TestMergerAtomHelpGateIsNotBlind(t *testing.T) {
	// THE RE-PLANTED PRE-FIX SCAN. Its atom-carrying cases are exactly the ones
	// the dispatcher had before the fix: the sync family, the retired daemon
	// aliases, the retired run spelling — and nothing for the git atoms.
	preFixScanPrintsPage := func(verb string) string {
		switch verb {
		case taskverbs.SyncPass, taskverbs.Sync, taskverbs.Diff,
			taskverbs.Harvest, taskverbs.Push,
			taskverbs.Serve, taskverbs.Merger, taskverbs.Run:
			return "a page of that verb's own"
		}
		return tasksUsage
	}

	verbs := append([]string{taskverbs.Merge}, taskverbs.MergerVerbs...)
	for _, verb := range verbs {
		planted := preFixScanPrintsPage(verb)
		if len(atomsHelpOffenses(verb, planted)) == 0 {
			t.Errorf("the gate did NOT flag `gopi task %s %s` printing the family blob — the re-planted pre-fix scan passes, so the gate is blind to the defect it exists for", verb, atomsHelpFlag)
		}
		// ...and the real page must pass the SAME predicate, or the gate would be
		// "always red" and the proof would prove nothing.
		if offenses := atomsHelpOffenses(verb, taskMergerAtomUsage(verb)); len(offenses) > 0 {
			t.Errorf("the gate flags the FIXED page for %q: %s", verb, strings.Join(offenses, "; "))
		}
	}
	// A page that is empty, and a call with no verb, are both defects the
	// predicate must name rather than pass (Rule 15: a blank answer is not an
	// answer).
	if len(atomsHelpOffenses(taskverbs.Merge, "")) == 0 {
		t.Error("an EMPTY page must be flagged")
	}
	if len(atomsHelpOffenses("", taskverbs.Merge)) == 0 {
		t.Error("an empty verb must be flagged (the gate is vacuous without one)")
	}
}

// --- the atom's own flag region, and the predicates over it -------------------

// atomsAdmittedRegionPrefix opens every row of the flag set `gopi task merge`
// ADMITS. The line HEAD is the whole discriminator, on purpose: it is what an
// operator reads, so a spelling printed under the wrong head is the defect, and
// a gate that cut the region by anything else would be grading the renderer's
// internals instead of the page.
const atomsAdmittedRegionPrefix = "  gopi task merge "

// atomsFlagTokenRe matches one flag spelling on a printed page line: the long
// form and the short help form this CLI has. The `-h` alternative needs the word
// boundary so it cannot be cut out of the middle of a long spelling.
var atomsFlagTokenRe = regexp.MustCompile(`(?:--[a-z][a-z-]*|-h)\b`)

// atomsRegionFlags returns, in page order, the flag spellings the lines of one
// region carry.
func atomsRegionFlags(page, prefix string) []string {
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(page, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		for _, f := range atomsFlagTokenRe.FindAllString(line, -1) {
			if seen[f] {
				continue
			}
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

// atomsAdmittedNames is the four spellings the atom admits, as the constants
// declare them — the ONE list this file checks against, so a name added to the
// vocabulary is graded and a name removed turns the floors red.
//
//nolint:gochecknoglobals // the atom's admitted set, read by every check below
var atomsAdmittedNames = []string{
	mergeAtomAllFlagName, mergeAtomRepoFlagName, mergeAtomSubmitFlagName, mergeAtomDBFlagName,
}

// atomsAtomFlagWording pins the sentence that must sit beside each admitted
// spelling. This is a WORDING pin, not decoration: a NAME/VALUE swap among the
// four rows leaves the token set identical, so the token-level checks above
// cannot see it and this table is what does. The wording is the operator-visible
// half of the row; it moves only when the row genuinely moves.
//
//nolint:gochecknoglobals // a wording-pin table, read by the gate below
var atomsAtomFlagWording = map[string]string{
	mergeAtomAllFlagName:    "every local branch",
	mergeAtomRepoFlagName:   "instead of the cwd",
	mergeAtomSubmitFlagName: "conflicts into the ledger",
	mergeAtomDBFlagName:     "memory db path",
}

// atomsTaskMergeAdmissionCaseCode returns the CODE lines (comments dropped) of
// the `case taskverbs.Merge:` arm inside taskFlagMember — read from the source
// the binary is built from.
//
// It exists so the biconditional below has a real second side: the four
// spellings must come through the named constants, so the gate's own list is the
// SAME list the parser reads. A bare literal added to that arm would make the
// two different things while both stayed green — this extraction notices.
func atomsTaskMergeAdmissionCaseCode(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("cli_tasks.go")
	if err != nil {
		t.Fatalf("read cli_tasks.go: %v", err)
	}
	src := string(raw)
	fn := strings.Index(src, "func taskFlagMember(")
	if fn < 0 {
		t.Fatal("taskFlagMember not found in cli_tasks.go — the gate would be vacuous")
	}
	// THE ANCHOR CARRIES ITS NEWLINE, and that is not cosmetic (the homograph
	// trap, Rule 43). `taskverbs.Merge` is a PREFIX of `taskverbs.MergedCleanup`,
	// which is a case arm EARLIER in this same switch — a bare
	// `strings.Index(src, "case taskverbs.Merge:")` matches nothing at the Merge
	// arm and lands on `case taskverbs.MergedCleanup:`, where the extraction
	// "succeeds" and grades an arm this test has nothing to do with (measured
	// while writing this file: named=0 of 4 constants, because the graded body
	// was merged-cleanup's `case "db":`). The trailing newline is what makes the
	// two distinct.
	//
	// src[i:]-relative arithmetic: strings.Index with a start offset returns an
	// ABSOLUTE index, so the found offset is used as-is and never added to the
	// function's offset again (a double count lands the slice in unrelated
	// source, where the extraction would "succeed" and grade a different arm —
	// the floors below refuse a body that does not look like the arm).
	at := strings.Index(src[fn:], atomsMergeArmAnchor)
	if at < 0 {
		t.Fatal("taskFlagMember has no `case taskverbs.Merge:` arm — the atom admits nothing, which the page would have to state")
	}
	body := src[fn+at:]
	// The arm's END is the NEXT `case taskverbs.`, searched from index 1 because
	// the body OPENS with the one this arm is named for (searching from 0 would
	// stop at once and leave a one-line body, which the floors refuse rather
	// than silently grade).
	if next := strings.Index(body[1:], "case taskverbs."); next > 0 {
		body = body[:next+1]
	}
	var code []string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		code = append(code, trimmed)
	}
	if len(code) < 2 {
		t.Fatal("the extraction produced no code lines — the arm's shape changed and this gate went blind")
	}
	return code
}

// TestTaskMergeFlagSetsAreAttributedTheWayTheParsersBehave is the biconditional
// the epic asks for, in both directions and on ONE surface (the page the -h scan
// prints):
//
//	advertised to THIS verb  ⇔  admitted by taskFlagMember("merge", …)
//	the shared help pair    ⇒  printed, and never counted as the atom's own
//
// It is what keeps the class from coming back: the page may not promise a flag
// the parser refuses, and the parser may not admit a flag the page hides.
func TestTaskMergeFlagSetsAreAttributedTheWayTheParsersBehave(t *testing.T) {
	page := taskMergerAtomUsage(taskverbs.Merge)
	if strings.TrimSpace(page) == "" {
		t.Fatal("the merge atom has no page")
	}
	admitted := atomsRegionFlags(page, atomsAdmittedRegionPrefix)
	// `-h` / `--help` are dispatched by the family's usage scan BEFORE any parser
	// runs, so they belong to no face's flag SET: removed once, here, rather than
	// excused at every assertion below (Rule 15 — an exception stated once is
	// reviewable, four scattered ones are not).
	shared := []string{workerFlagHelp, workerFlagHelpLong}
	if !atomsContains(admitted, workerFlagHelp) {
		t.Errorf("the page no longer shows %s among the atom's own rows — the one spelling that must always work is invisible to the reader", workerFlagHelp)
	}
	var atomOnly []string
	for _, f := range admitted {
		if !atomsContains(shared, f) {
			atomOnly = append(atomOnly, f)
		}
	}
	admitted = atomOnly

	// THE FLOOR. A region the cutter failed to read yields no tokens and every
	// assertion below would pass over an empty set.
	if len(admitted) < len(atomsAdmittedNames) {
		t.Fatalf("the atom's own flag region yielded %d spellings (%v) — the region cutter is blind", len(admitted), admitted)
	}

	// ① Every spelling the page puts on THIS verb is one the parser admits.
	for _, f := range admitted {
		if !taskFlagMember(taskverbs.Merge, strings.TrimPrefix(f, "--")) {
			t.Errorf("`task merge -h` advertises %s to this verb, but taskFlagMember refuses it — an advertised flag is either obeyed or refused by name (Rule 15); "+
				"the atom's set is %v", f, atomsProbeAdmittedSpellings())
		}
	}

	// ② Every spelling the parser admits is ON the page (the other direction: a
	// hidden flag is a flag the operator can only find by trying it).
	for _, name := range atomsAdmittedNames {
		if !taskFlagMember(taskverbs.Merge, name) {
			t.Errorf("%s is no longer admitted by taskFlagMember(\"merge\", …) — either re-admit it or take it out of the page and this gate (the page and the parser must agree)", name)
			continue
		}
		if !atomsContains(admitted, mergeAtomFlagPrefix+name) {
			t.Errorf("taskFlagMember admits --%s for this verb but the page's own flag region does not show it (%v)", name, admitted)
		}
	}

	// ③ ORDER: the rows must appear in the order the constants are declared
	// (all/repo/submit/db — the family's own reading order). A re-ordering is not
	// cosmetic here: it is how a row silently moves onto a neighbour's
	// description.
	last := -1
	for _, name := range atomsAdmittedNames {
		f := mergeAtomFlagPrefix + name
		at := strings.Index(page, f)
		if at < 0 {
			t.Errorf("the page never shows %s", f)
			continue
		}
		if at < last {
			t.Errorf("the page shows %s out of the declaration order (%v) — the rows moved, so a description may have moved with them", f, atomsAdmittedNames)
		}
		last = at
	}

	// ④ The WORDING of each row, keyed by the flag's own name constant: the check
	// a token-level gate cannot make (see atomsAtomFlagWording).
	for name, want := range atomsAtomFlagWording {
		line := atomsRegionLineCarrying(page, atomsAdmittedRegionPrefix, mergeAtomFlagPrefix+name)
		if line == "" {
			continue // already reported by ②
		}
		if !strings.Contains(line, want) {
			t.Errorf("the page's --%s row reads %q, which does not carry %q — the row and its flag have drifted apart", name, strings.TrimSpace(line), want)
		}
	}
}

// atomsProbeAdmittedSpellings lists the atom's admitted spellings — used only to
// make a failure message actionable.
func atomsProbeAdmittedSpellings() []string {
	out := make([]string, 0, len(atomsAdmittedNames))
	for _, name := range atomsAdmittedNames {
		out = append(out, mergeAtomFlagPrefix+name)
	}
	return out
}

// atomsRegionLineCarrying returns the page line of one region that carries a
// spelling (the first one), or "".
func atomsRegionLineCarrying(page, prefix, flag string) string {
	for _, line := range strings.Split(page, "\n") {
		if strings.HasPrefix(line, prefix) && strings.Contains(line, flag) {
			return line
		}
	}
	return ""
}

// TestTaskMergeFlagAttributionGateIsNotBlind is the RED ARM for the
// biconditional above (Rule 10.4 — a gate you have never seen fail is a gate you
// cannot trust). It re-plants the PRE-FIX page — the exact shape that shipped:
// the deleted engine's flag block printed under THIS verb's head, so a reader
// would type `gopi task merge --status`, which this atom has never read.
func TestTaskMergeFlagAttributionGateIsNotBlind(t *testing.T) {
	// The pre-fix page, reduced to the shape that matters: flags the atom does
	// not admit, printed as its own.
	preFix := "gopi task merge — merge accepted branches (the git atom).\n\n" +
		"THE MERGE FACE'S FLAGS (identical at 'gopi merge'):\n" +
		atomsAdmittedRegionPrefix + "--status   report each repo's archived set\n" +
		atomsAdmittedRegionPrefix + "--fast     git-only run\n"

	flagCount := 0
	for _, f := range atomsRegionFlags(preFix, atomsAdmittedRegionPrefix) {
		flagCount++
		if taskFlagMember(taskverbs.Merge, strings.TrimPrefix(f, "--")) {
			t.Errorf("the re-planted pre-fix page's %s is admitted by the parser — the proof needs a spelling the atom really refuses", f)
		}
	}
	if flagCount < atomsMinPlantedFlags {
		t.Fatalf("the planted page yielded only %d spellings — the region cutter is blind and the proof proves nothing", flagCount)
	}

	// ...and the REAL page must pass the same predicate, or the gate would be
	// "always red" and the proof would prove nothing either.
	page := taskMergerAtomUsage(taskverbs.Merge)
	for _, f := range atomsRegionFlags(page, atomsAdmittedRegionPrefix) {
		if f == workerFlagHelp || f == workerFlagHelpLong {
			continue
		}
		if !taskFlagMember(taskverbs.Merge, strings.TrimPrefix(f, "--")) {
			t.Errorf("the FIXED page still advertises %s to this verb while the parser refuses it", f)
		}
	}

	// And the real predicate must be red for spellings the DELETED engine owned —
	// otherwise the arm could be proving itself against a page nobody would write.
	for _, f := range []string{"--status", "--fast", "--no-verify", "--every", "--branch", "--handover-budget"} {
		if taskFlagMember(taskverbs.Merge, strings.TrimPrefix(f, "--")) {
			t.Errorf("taskFlagMember(\"merge\", %q) = true — the deleted engine's flag is still admitted by this atom, so the page must either show it or the parser must refuse it", f)
		}
	}
}

// atomsMinPlantedFlags is the floor under the red arm's planted page: fewer than
// this and the region cutter, not the predicate, is what the arm measured.
const atomsMinPlantedFlags = 2

// atomsMergeArmAnchor is the EXACT source spelling of taskFlagMember's merge arm,
// newline included. The trailing newline is load-bearing: `taskverbs.Merge` is a
// prefix of `taskverbs.MergedCleanup`, so without it the search lands on the
// merged-cleanup arm (see atomsTaskMergeAdmissionCaseCode).
const atomsMergeArmAnchor = "case taskverbs.Merge:\n"

// atomsAdmissionIdentifiers are the SOURCE-TEXT spelling of the four constants —
// not their values. This is the one place in this file where an identifier is
// written as a string, and it cannot be avoided: the needle being searched for
// in taskFlagMember's source IS the identifier, so `mergeAtomAllFlagName` cannot
// be derived from the constant whose value is "all" (that search finds nothing,
// and a gate that finds nothing must not pass — the floor below catches exactly
// that, which is how this was measured while writing the file).
var atomsAdmissionIdentifiers = []string{
	"mergeAtomAllFlagName", "mergeAtomRepoFlagName",
	"mergeAtomSubmitFlagName", "mergeAtomDBFlagName",
}

// TestTaskMergeAdmissionTableNamesNoBareFlagLiteral keeps the gate's own second
// side honest: the `case taskverbs.Merge:` arm of taskFlagMember must reach
// every spelling through the mergeAtom*FlagName constants, so the list this file
// checks against (and the page renders from) is the SAME list the parser reads.
// A bare `"all"` typed into that arm would make the two different things while
// both stayed green (Rule 1/13).
func TestTaskMergeAdmissionTableNamesNoBareFlagLiteral(t *testing.T) {
	code := atomsTaskMergeAdmissionCaseCode(t)
	named := 0
	for _, line := range code {
		if strings.Contains(line, `"`) {
			t.Errorf("the admission arm carries a bare literal (%q) — the spellings must come from the mergeAtom*FlagName constants, or the page and this gate would be checking a list the parser does not use", line)
		}
		for _, name := range atomsAdmissionIdentifiers {
			named += strings.Count(line, name)
		}
	}
	if named < len(atomsAdmissionIdentifiers) {
		t.Fatalf("the admission arm names only %d of the four constants — the extraction lost lines and this gate would pass vacuously", named)
	}
}

// TestTaskMergerAtomHelpCannotBecomeASecondManual is the epic's own invariant
// applied to the pages: they POINT, they do not re-teach. A re-taught
// `gopi task <atom>` SYNOPSIS line (the shape the CLI-surface restructure removed
// from the family usage) is the defect — this asserts the pages do not
// reintroduce it under another name, and that every atom they name is a
// taskverbs constant (Rules 1/13).
func TestTaskMergerAtomHelpCannotBecomeASecondManual(t *testing.T) {
	pages := append([]string{taskverbs.Merge}, taskverbs.MergerVerbs...)
	named := 0
	for _, pageVerb := range pages {
		page := taskMergerAtomUsage(pageVerb)
		for _, line := range strings.Split(page, "\n") {
			if verb, ok := atomsAtomSynopsisVerb(line, pageVerb); ok {
				t.Errorf("the %q page re-teaches the SYNOPSIS of %q: %q — the atoms' home is the manual; these pages point at it, they do not document it", pageVerb, verb, strings.TrimSpace(line))
			}
			named += strings.Count(line, "gopi task ")
		}
	}
	if named < len(taskverbs.MergerVerbs) {
		t.Fatalf("the pages name `gopi task …` only %d times — the pointer is missing, so a green run proves nothing", named)
	}
	// Every atom in the vocabulary gets a page: a verb added to MergerVerbs with
	// no page would fall back to the family blob, which is the defect.
	for _, v := range taskverbs.MergerVerbs {
		if taskMergerAtomUsage(v) == "" {
			t.Errorf("taskverbs.MergerVerbs declares %q but taskMergerAtomUsage has no page for it — it would print the family blob again", v)
		}
	}
	if got := taskMergerAtomUsage("frobnicate"); got != "" {
		t.Errorf("taskMergerAtomUsage answered for a spelling it does not own (%q) — an unknown verb must keep the caller's own fallback", got)
	}
}

// atomsAtomSynopsisVerb reports whether a printed page line carries a family
// SYNOPSIS head — indent EXACTLY 2 then `gopi task <verb>` — for a verb OTHER
// than the page's own, returning that verb.
//
// pageVerb is excluded because the merge atom's page DOES open with its own
// synopsis block: that is the verb's own page, and its synopses are the forms the
// operator can type. What must not appear is ANOTHER atom's synopsis: the atoms
// are one manual, and these pages are the doors to it, listing them as POINTERS
// (indent 4, description on the same line, which this predicate deliberately
// does not match).
func atomsAtomSynopsisVerb(line, pageVerb string) (string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) != atomsSynopsisIndent {
		return "", false
	}
	const head = "gopi task "
	if !strings.HasPrefix(trimmed, head) {
		return "", false
	}
	rest := strings.TrimPrefix(trimmed, head)
	verb := rest
	if i := strings.IndexAny(rest, " \t[<"); i >= 0 {
		verb = rest[:i]
	}
	if verb == pageVerb {
		return "", false
	}
	for _, v := range taskverbs.MergerVerbs {
		if v == verb {
			return verb, true
		}
	}
	return "", false
}

// atomsSynopsisIndent is the indent a family SYNOPSIS head uses (see
// atomsAtomSynopsisVerb) — 2, against 4 for the pointer list.
const atomsSynopsisIndent = 2

// TestTaskUnknownSubcommandStillNamesTheWholeDispatchVocabulary is the CONTROL
// for the dispatcher's own claim: the want list is derived from
// taskverbs.DispatchVerbs, so every declared spelling — the merger atoms
// included — is already named there.
func TestTaskUnknownSubcommandStillNamesTheWholeDispatchVocabulary(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	cmd := exec.Command(bin, "task", "frobnicate")
	cmd.Env = cliContractEnv(home)
	raw, err := cmd.CombinedOutput()
	out := string(raw)
	if err == nil {
		t.Fatalf("`gopi task frobnicate` exited 0 — an unknown subcommand must fail:\n%s", out)
	}
	if !strings.Contains(out, `unknown subcommand "frobnicate"`) {
		t.Errorf("the refusal must name the unknown spelling:\n%s", out)
	}
	var missing []string
	for _, v := range taskverbs.DispatchVerbs {
		if !strings.Contains(out, v) {
			missing = append(missing, v)
		}
	}
	if len(missing) > 0 {
		t.Errorf("the want list is missing %v — it is derived from taskverbs.DispatchVerbs and must carry every declared spelling", missing)
	}
}

// TestAtomPageCarriesNoDanglingNotePointer pins the class "a `-h` page promises a
// note it does not carry": `gopi task merge -h` once pointed forward at an image
// self-check note and printed no such note.
//
// The defect is decided by THE predicate, never by a phrase held here — one
// spelling of the defect covered would leave the class walking around it.
// helpNotePageOffenses (cli_help_note_region.go) decides it from the page's
// STRUCTURE: a forward pointer on a page with no MARKED NOTE REGION is the
// offense, whatever the wording.
func TestAtomPageCarriesNoDanglingNotePointer(t *testing.T) {
	page := taskMergerAtomUsage(taskverbs.Merge)
	if page == "" {
		t.Fatal("taskMergerAtomUsage returned an empty page — this gate would pass vacuously")
	}
	for _, offense := range helpNotePageOffenses(page) {
		t.Errorf("`gopi task %s %s` %s", taskverbs.Merge, atomsHelpFlag, offense)
	}

	// RED ARM — the class mutation, run through the SAME predicate the line above
	// ran: the promise RE-SPELLED by LOCATION on a page carrying no note region.
	// The premise is asserted rather than assumed: the arm's page really is
	// region-free, or this is not the arm it claims to be.
	//
	// TWO spellings, because one of them is the whole reason this arm exists: the
	// first is the re-spelling the DELETED phrase-match body was blind to, and the
	// second is the one a closed seven-word vocabulary was still blind to
	// (measured 2026-10-16: offenses=0 while "just under"/"below" on the SAME page
	// gave 1 — the identical false negative one layer down).
	for _, spelling := range atomsNotePointerRespellings {
		armPage := page + "\n  " + spelling + "\n"
		if regions := helpNoteRegions(armPage); len(regions) != 0 {
			t.Fatalf("the red arm's page carries %d note region(s) — a page WITH a region is the good case, so this arm would prove nothing about a dangling pointer", len(regions))
		}
		if offenses := helpNotePageOffenses(armPage); len(offenses) == 0 {
			t.Errorf("the predicate passed a page whose forward pointer is RE-SPELLED (%q) with no note region — the class is back:\n%s", spelling, armPage)
		}
	}
}

// atomsNotePointerRespellings are the re-spellings the predicate's own vocabulary
// was measured blind to — kept verbatim so these arms fail for the reasons that
// were actually observed rather than for reasons invented here:
//
//   - the first is the one the DELETED phrase-match body ("note below") stayed
//     green on (measured 2026-10-13);
//   - the second is the one helpNotePageOffenses' first vocabulary — the closed
//     seven-word list — stayed green on (measured 2026-10-16).
//
//nolint:gochecknoglobals // the measured re-spellings, read by the red arm
var atomsNotePointerRespellings = []string{
	"see the self-check note just under this row.",
	"see the self-check note after this row.",
}
