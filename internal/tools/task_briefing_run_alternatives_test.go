package tools

// task_briefing_run_alternatives_test.go — the SIXTH leg of the dispatch seam,
// driven through a REAL tree whose test files carry REAL function names.
//
// WHY THIS FILE EXISTS, in the epic's own terms: the defect is an acceptance
// line that names a test nothing implements, reads green, and hides a whole
// family that never ran. A clause that judged alternatives by GREPPING the
// briefing for its own words would reproduce exactly that defect one level up —
// so every leg below drives the real scan over a real directory, and the
// negative control (leg ②) is the one that keeps the clause from stamping every
// briefing it meets.
//
// SEVEN LEGS:
//
//	① DEAD ALTERNATIVE — a pattern with one name the package implements and one
//	   it does not gets the stamp, naming ONLY the dead one.
//	② POSITIVE CONTROL — the SAME shape where every alternative reaches a test
//	   stamps NOTHING (the ordinary briefing), and still leaves a COUNT per
//	   alternative in the record.
//	③ NON-TEST FILE LITERALS DO NOT COUNT — a `func Test…` spelling inside a
//	   comment, an indented string literal, and a production .go file is not a
//	   test function, so a name that only appears that way is still DEAD.
//	④ THE VACUUM FLOOR — a package directory with no _test.go, a package
//	   directory that does not exist, and a recursive pattern are each STATED as
//	   unmeasured; a scan that judged nothing never passes as green (Rule 15).
//	⑤ THE SUBTEST ARM — `TestX/Sub` counts its top-level function and says the
//	   subtest half is outside a source scan.
//	⑥ NO PATTERN IS A NO-OP — the ordinary briefing that names no `-run` is
//	   silent (the reason this cannot degrade every briefing it touches).
//	⑦ IDEMPOTENCE — a second pass over a stamped briefing appends nothing and
//	   says so.
//	⑨ THE REPLACEMENT LINE (epic 01a1abcd, 2026-11-05) — the clause does not
//	   stop at naming the dead alternative: it SUPPLIES the same command with the
//	   surviving alternatives, because a reader handed only a finding has to
//	   reconstruct the selector from prose. Its arms: the honest repair (a live
//	   family beside the dead name), the live-one-LAST shape (the epic's own
//	   line), and the stated absence when every alternative is dead.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runAltTestPackage is the package pattern every fixture briefing names — the
// real directory the fixture tree materialises, so the scan under test resolves
// a directory the way an acceptance command does.
const runAltTestPackage = "./internal/probe"

// runAltFixtureTestFile is the file inside that package carrying the functions
// the reaching alternatives select.
const runAltFixtureTestFile = "probe_test.go"

// runAltFixtureBody declares the names leg ① and leg ② select and the spellings
// legs ③ and ⑦ must refuse. Note what is deliberately present: a `Test…` word in
// a COMMENT, one inside an indented string literal, a helper that merely CALLS a
// test-looking name, and a `TestMain` hook — none of which a `-run` pattern can
// select, and all of which a lazy implementation would count.
const runAltFixtureBody = `package probe

// TestThisIsAComment is prose about a test, not a test.
const runAltHelperLiteral = "\tTestIndentedInAStringIsNotATest("

func TestMain(m *testing.M) { os.Exit(m.Run()) }

func TestProbeAlpha(t *testing.T) {}

func TestProbeBeta(t *testing.T) {}

func helperThatCallsTestProbeAlpha() { TestProbeAlpha(nil) }
`

// runAltFixtureTree materialises the package the fixture briefings name, so the
// census taken by the clause is a real read of real files.
func runAltFixtureTree(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	dir := filepath.Join(repo, "internal", "probe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, runAltFixtureTestFile), []byte(runAltFixtureBody), 0o644); err != nil {
		t.Fatal(err)
	}
	// A PRODUCTION file carrying the same spelling: a `func Test…` line in a
	// non-test file is a helper or a literal, and the scan reads only
	// *_test.go (leg ③).
	prod := "package probe\n\nfunc TestOnlyInProduction() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "probe.go"), []byte(prod), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

// runAltBriefing renders the acceptance-command shape the incident carried: a
// `go test <pkg> -run '<A|B>'` line inside prose.
func runAltBriefing(alternatives string) string {
	return "## Acceptance\n\n" +
		"go test " + runAltTestPackage + "/ -run '" + alternatives + "'\n\n" +
		"This line is the gate.\n"
}

// ① DEAD ALTERNATIVE — the measured incident shape: one name that exists, one
// that does not.
func TestBriefingRunAlt_ADeadAlternativeIsNamed(t *testing.T) {
	repo := runAltFixtureTree(t)
	clause, record := TaskBriefingRunAlternativesRefresh(repo, runAltBriefing("TestProbeAlpha|TestTaskReclaimSelfDelivery"))

	if clause == "" {
		t.Fatalf("no clause stamped for a pattern with a dead alternative; record=%v", record)
	}
	if !strings.Contains(clause, TaskBriefingRunAlternativesLabel) {
		t.Errorf("clause does not carry the exported label: %q", clause)
	}
	if !strings.Contains(clause, "TestTaskReclaimSelfDelivery") {
		t.Errorf("clause does not name the DEAD alternative: %q", clause)
	}
	if strings.Contains(clause, "- alternative \"TestProbeAlpha\"") {
		t.Errorf("clause names TestProbeAlpha, which DOES reach a test:\n%s", clause)
	}
	if !strings.Contains(clause, TaskBriefingRunAlternativesVerifyOnly) {
		t.Errorf("clause does not carry the instruction constant: %q", clause)
	}
	if !strings.Contains(clause, "NOT evidence that the work is done") {
		t.Errorf("clause does not deny the completion reading: %q", clause)
	}
	// The ZERO is also restated in the record, beside its census line (Rule 15).
	if !recordNames(t, record, "ZERO REACH", "TestTaskReclaimSelfDelivery") {
		t.Errorf("record carries no ZERO REACH line for the dead alternative: %v", record)
	}
}

// ② POSITIVE CONTROL — every alternative reaching a test stamps NOTHING, and
// the census still prints a count for each, so the ordinary briefing is silent
// but the measurement is not invisible.
func TestBriefingRunAlt_AFullyReachingPatternStampsNothing(t *testing.T) {
	repo := runAltFixtureTree(t)
	clause, record := TaskBriefingRunAlternativesRefresh(repo, runAltBriefing("TestProbeAlpha|TestProbeBeta"))

	if clause != "" {
		t.Fatalf("stamped a briefing whose every alternative reaches a test: %q", clause)
	}
	if !recordNames(t, record, "TestProbeAlpha", "1 top-level test function(s) of 2") {
		t.Errorf("census line for TestProbeAlpha missing or miscounted: %v", record)
	}
	if !recordNames(t, record, "TestProbeBeta") {
		t.Errorf("census line for TestProbeBeta missing: %v", record)
	}
}

// ③ NON-TEST-FILE LITERALS DO NOT COUNT. The names below appear in this tree
// only inside a comment, an indented string literal, or a production .go file —
// and they must therefore still be DEAD.
func TestBriefingRunAlt_NonTestFileLiteralsDoNotCount(t *testing.T) {
	repo := runAltFixtureTree(t)
	clause, record := TaskBriefingRunAlternativesRefresh(repo,
		runAltBriefing("TestThisIsAComment|TestIndentedInAStringIsNotATest|TestOnlyInProduction|TestMain"))

	if clause == "" {
		t.Fatalf("a pattern whose names appear only as non-test spellings stamped nothing; record=%v", record)
	}
	for _, dead := range []string{
		"TestThisIsAComment",
		"TestIndentedInAStringIsNotATest",
		"TestOnlyInProduction",
		"TestMain",
	} {
		if !strings.Contains(clause, dead) {
			t.Errorf("clause does not name %q as dead (a spelling that is not a top-level test):\n%s", dead, clause)
		}
	}
}

// ④ THE VACUUM FLOOR — a scan that judged nothing is UNMEASURED, never green.
func TestBriefingRunAlt_AScanThatJudgedNothingIsStated(t *testing.T) {
	t.Run("no test files", func(t *testing.T) {
		repo := t.TempDir()
		if err := os.MkdirAll(filepath.Join(repo, "internal", "probe"), 0o755); err != nil {
			t.Fatal(err)
		}
		clause, record := TaskBriefingRunAlternativesRefresh(repo, runAltBriefing("TestAnything"))
		if clause != "" {
			t.Fatalf("stamped a verdict on a package this scan could not read: %q", clause)
		}
		if !recordNames(t, record, "UNMEASURED", runAltNoTestFilesReason) {
			t.Errorf("a directory with no _test.go must be STATED as unmeasured: %v", record)
		}
	})
	t.Run("missing directory", func(t *testing.T) {
		repo := t.TempDir()
		_, record := TaskBriefingRunAlternativesRefresh(repo, runAltBriefing("TestAnything"))
		if !recordNames(t, record, "UNMEASURED", runAltMissingDirReason) {
			t.Errorf("a package directory this ground does not carry must be STATED: %v", record)
		}
	})
	t.Run("recursive pattern", func(t *testing.T) {
		repo := runAltFixtureTree(t)
		briefing := "go test ./... -run 'TestMissing'\n"
		clause, record := TaskBriefingRunAlternativesRefresh(repo, briefing)
		if clause != "" {
			t.Fatalf("stamped a verdict on a `./...` pattern it does not expand: %q", clause)
		}
		if !recordNames(t, record, "UNMEASURED", runAltRecursiveReason) {
			t.Errorf("a recursive package pattern must be STATED: %v", record)
		}
	})
	t.Run("no ground", func(t *testing.T) {
		_, record := TaskBriefingRunAlternativesRefresh("", runAltBriefing("TestMissing"))
		if !recordNames(t, record, "UNMEASURED", runAltNoRepoReason) {
			t.Errorf("an absent ground must be STATED: %v", record)
		}
	})
}

// ⑤ THE SUBTEST ARM — `TestProbeAlpha/Case` counts its top-level function and
// states that the subtest half is outside a source scan.
func TestBriefingRunAlt_ASubtestNamesItsTopLevelFunction(t *testing.T) {
	repo := runAltFixtureTree(t)
	clause, record := TaskBriefingRunAlternativesRefresh(repo, runAltBriefing("TestProbeAlpha/SomeCase"))

	if clause != "" {
		t.Fatalf("stamped a subtest whose top-level function exists: %q", clause)
	}
	if !recordNames(t, record, "SUBTEST", "TestProbeAlpha") {
		t.Errorf("the subtest arm is not stated: %v", record)
	}
}

// ⑥ NO PATTERN IS A NO-OP — the ordinary briefing.
func TestBriefingRunAlt_NoPatternIsANoOp(t *testing.T) {
	repo := runAltFixtureTree(t)
	clause, record := TaskBriefingRunAlternativesRefresh(repo, "## Acceptance\n\ngo build ./...\n")
	// why: correct — the empty set is a statement about the INPUT (a briefing naming no `go test … -run`), not a guard that suppressed an action: runAltPatternsIn returns before any scan is opened, so there is no idempotent step this could exclude. The positive half is carried in this same file by legs ① and ③: the SAME producer, given a briefing WITH a `-run`, returns both a clause and record lines — so the silence here is the pattern reader's own early return, not a producer that cannot speak.
	if clause != "" || len(record) != 0 {
		t.Fatalf("a briefing naming no -run pattern must be silent: clause=%q record=%v", clause, record)
	}
}

// ⑦ IDEMPOTENCE — a second pass says so and appends nothing.
func TestBriefingRunAlt_ASecondPassAppendsNothing(t *testing.T) {
	repo := runAltFixtureTree(t)
	first, _ := TaskBriefingRunAlternativesRefresh(repo, runAltBriefing("TestMissingOne"))
	if first == "" {
		t.Fatal("first pass stamped nothing")
	}
	stamped := runAltBriefing("TestMissingOne") + "\n\n" + first
	second, record := TaskBriefingRunAlternativesRefresh(repo, stamped)
	if second != "" {
		t.Fatalf("second pass appended a clause again: %q", second)
	}
	if !recordNames(t, record, "UNMEASURED", runAltAlreadyStampedText) {
		t.Errorf("second pass did not state that it skipped: %v", record)
	}
}

// ⑧ MARKDOWN INLINE CODE — the measured FALSE stamp (bug row 01a1a6f6), and the
// reason runAltTokens has a backtick arm at all.
//
// The incident: a briefing wrapped its acceptance command in one markdown code
// span, the trailing tick was glued onto the LAST alternative, that alternative
// matched no test, and the clause stamped ZERO REACH over a test that exists and
// passes. The clause's instruction would then delete a live acceptance check —
// Rule 36's defect mirrored (a check that EXISTS, asserted to exist nowhere).
//
// The judgement under test is PAIRING, never "strip every backtick": a tick
// inside a single-quoted value is content (leg ⑧c), and a lone tick stays a
// character of its token (leg ⑧d).
const (
	// runAltTickAlternatives is the pattern the markdown arms select: TWO live
	// alternatives, so a glued trailing tick corrupts exactly the last one.
	runAltTickAlternatives = "TestProbeAlpha|TestProbeBeta"
	// runAltTickBriefingFmt renders the SAME acceptance command in the two
	// spellings the incident turned on — %s prefixes and suffixes the bare
	// spelling with nothing, and the wrapped one with one code span.
	runAltTickBriefingFmt = "## Acceptance\n\n%sgo test " + runAltTestPackage + "/ -run '%s'%s 绿；\n"
	// runAltTickMark is markdown's inline-code delimiter, spelled once for the
	// fixtures (the production side owns its own constant).
	runAltTickMark = "`"
	// runAltTickEmpty is the NO-DELIMITER spelling's own value, named so the
	// pair of briefings reads as two substitutions of one format and not as a
	// magic empty string.
	runAltTickEmpty = ""
	// runAltTickQuotedPipe is a backtick PAIR inside a single-quoted value:
	// shell quoting owns those bytes, so they are content and must survive.
	runAltTickQuotedPipe = "echo '`a|b`'"
	// runAltTickLone is an UNPAIRED tick OUTSIDE any quote, which must survive
	// too: an odd count leaves one tick with nothing to pair with, and dropping
	// it would remove bytes from somebody's token.
	runAltTickLone = "go test " + runAltTestPackage + "/ -run TestProbeAlpha`"
	// runAltTokenSep joins tokens for comparison; a byte no fixture token
	// carries, so a comparison cannot pass by accidental adjacency.
	runAltTokenSep = "\x00"
)

// runAltTickBriefing renders the acceptance command of the incident in one of
// its two spellings: markdown-wrapped or bare.
func runAltTickBriefing(mark string) string {
	return fmt.Sprintf(runAltTickBriefingFmt, mark, runAltTickAlternatives, mark)
}

// ⑧a THE REPRO — the same command, with and without markdown, must produce
// BYTE-IDENTICAL patterns, and the wrapped one must NOT be stamped. Reverting
// the backtick arm of runAltTokens turns this red: the wrapped spelling's last
// alternative becomes "TestProbeBeta`", the census reaches nothing, and a ZERO
// REACH clause is stamped.
func TestBriefingRunAlt_MarkdownTicksDoNotChangeThePattern(t *testing.T) {
	repo := runAltFixtureTree(t)
	bare := runAltTickBriefing(runAltTickEmpty)
	wrapped := runAltTickBriefing(runAltTickMark)

	barePatterns := runAltPatternsIn(bare)
	wrappedPatterns := runAltPatternsIn(wrapped)
	if len(barePatterns) != 1 || len(wrappedPatterns) != 1 {
		t.Fatalf("each spelling must yield exactly one -run pattern: bare=%v wrapped=%v",
			barePatterns, wrappedPatterns)
	}
	if wrappedPatterns[0].pattern != barePatterns[0].pattern {
		t.Fatalf("markdown ticks changed the pattern: bare=%q wrapped=%q",
			barePatterns[0].pattern, wrappedPatterns[0].pattern)
	}
	if wrappedPatterns[0].pattern != runAltTickAlternatives {
		t.Fatalf("the pattern is not the command's own value: %q", wrappedPatterns[0].pattern)
	}
	if wrappedPatterns[0].pkg != barePatterns[0].pkg {
		t.Errorf("markdown ticks changed the package pattern: bare=%q wrapped=%q",
			barePatterns[0].pkg, wrappedPatterns[0].pkg)
	}

	// THE OUTCOME THE INCIDENT MEASURED: no clause, and no ZERO REACH line.
	for name, briefing := range map[string]string{"bare": bare, "markdown": wrapped} {
		clause, record := TaskBriefingRunAlternativesRefresh(repo, briefing)
		if clause != "" {
			t.Errorf("%s spelling stamped a clause over TWO live alternatives:\n%s", name, clause)
		}
		if recordNames(t, record, runAltZeroReachArrow) {
			t.Errorf("%s spelling reported ZERO REACH for a test the package implements: %v", name, record)
		}
		if !recordNames(t, record, runAltCensusArrow, runAltTickAlternatives) {
			t.Errorf("%s spelling carries no census line for the pattern: %v", name, record)
		}
	}
}

// ⑧b THE TOKENIZER, directly — the same command in both spellings yields the
// same TOKENS, which is the level the defect lived at.
func TestRunAltTokens_MarkdownCodeSpanMatchesTheBareCommand(t *testing.T) {
	bare := strings.Join(runAltTokens(runAltTickBriefing(runAltTickEmpty)), runAltTokenSep)
	wrapped := strings.Join(runAltTokens(runAltTickBriefing(runAltTickMark)), runAltTokenSep)
	if wrapped != bare {
		t.Fatalf("token streams differ:\n bare: %q\nwrapped: %q", bare, wrapped)
	}
	if !strings.Contains(wrapped, runAltTickAlternatives) {
		t.Errorf("the -run value is not a whole token: %q", wrapped)
	}
}

// ⑧c A TICK PAIR INSIDE A SINGLE-QUOTED VALUE IS CONTENT — the direction that
// keeps this fix from becoming "strip every backtick".
func TestRunAltTokens_TickPairInsideQuotesIsContent(t *testing.T) {
	got := strings.Join(runAltTokens(runAltTickQuotedPipe), runAltTokenSep)
	want := strings.Join([]string{"echo", "`a|b`"}, runAltTokenSep)
	if got != want {
		t.Fatalf("a tick pair inside a single-quoted value must survive as content: got %q want %q", got, want)
	}
}

// ⑧d AN UNPAIRED TICK IS NOT MARKDOWN — it stays a character of its token,
// because a tick with no partner on its line is prose, and silently removing
// bytes from a token is the failure mode Rule 15 forbids.
func TestRunAltTokens_LoneTickStaysInItsToken(t *testing.T) {
	toks := runAltTokens(runAltTickLone)
	last := ""
	if len(toks) > 0 {
		last = toks[len(toks)-1]
	}
	if last != "TestProbeAlpha`" {
		t.Fatalf("a lone tick must remain in its token: tokens=%v last=%q", toks, last)
	}
}

// recordNames reports whether ONE record line carries all the given tokens —
// the record is a list of lines, and a check that let two tokens sit on two
// lines would pass on an accidental pairing.
func recordNames(t *testing.T, record []string, tokens ...string) bool {
	t.Helper()
	for _, line := range record {
		all := true
		for _, tok := range tokens {
			if !strings.Contains(line, tok) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// ⑨ THE REPLACEMENT LINE — the finding alone leaves the reader to reconstruct a
// selector from prose, which is the same failure one layer down (a repair that
// exists only as an instruction). The clause SUPPLIES the command instead.
func TestBriefingRunAlt_TheReplacementLineSurvivesTheDeadName(t *testing.T) {
	repo := runAltFixtureTree(t)
	clause, _ := TaskBriefingRunAlternativesRefresh(repo, runAltBriefing("TestProbeAlpha|TestTaskReclaimSelfDelivery"))

	want := strings.TrimRight(fmt.Sprintf(taskBriefingRunAltRepairFmt, runAltTestPackage, "TestProbeAlpha"), "\n")
	if !strings.Contains(clause, want) {
		t.Fatalf("clause carries no REPLACEMENT line %q:\n%s", want, clause)
	}
	// The clause DOES quote the author's original line (it must — that is the
	// evidence), so this arm reads the REPLACEMENT line ALONE: the pasteable
	// command is the one that must not carry the dead name.
	for _, line := range strings.Split(clause, "\n") {
		if !strings.Contains(line, "REPLACEMENT (") {
			continue
		}
		if strings.Contains(line, "TestTaskReclaimSelfDelivery") {
			t.Errorf("the REPLACEMENT line still carries the dead alternative: %q", line)
		}
		if !strings.Contains(line, want) {
			t.Errorf("the REPLACEMENT line is not the computed repair: %q", line)
		}
	}
}

// ⑨b THE LIVE ALTERNATIVE STANDS LAST — the epic's own measured line shape. A
// producer that kept only the alternatives it had ALREADY seen live would render
// an empty selector here, so this leg fails on that implementation.
func TestBriefingRunAlt_TheLiveAlternativeAfterTheDeadOneIsKept(t *testing.T) {
	repo := runAltFixtureTree(t)
	clause, _ := TaskBriefingRunAlternativesRefresh(repo,
		runAltBriefing("TestTaskReclaimSelfDelivery|TestProbeAlpha|TestProbeBeta"))

	want := strings.TrimRight(fmt.Sprintf(taskBriefingRunAltRepairFmt, runAltTestPackage, "TestProbeAlpha|TestProbeBeta"), "\n")
	if !strings.Contains(clause, want) {
		t.Fatalf("the live alternatives AFTER the dead one were dropped — want %q:\n%s", want, clause)
	}
}

// ⑨c EVERY ALTERNATIVE DEAD — a stated absence, never silence: silence reads
// like a repair that exists (Rule 15).
func TestBriefingRunAlt_AllDeadStatesThereIsNoReplacement(t *testing.T) {
	repo := runAltFixtureTree(t)
	clause, _ := TaskBriefingRunAlternativesRefresh(repo, runAltBriefing("TestMissingOne|TestMissingTwo"))

	want := strings.TrimRight(fmt.Sprintf(taskBriefingRunAltNoRepairFmt, 2), "\n")
	if !strings.Contains(clause, want) {
		t.Fatalf("all-dead pattern did not state the absent replacement %q:\n%s", want, clause)
	}
	if strings.Contains(clause, taskBriefingRunAltRepairFmt) {
		t.Errorf("all-dead pattern rendered a REPLACEMENT line with no live alternative:\n%s", clause)
	}
}
