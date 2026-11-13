package tools

// task_briefing_prose_test_names_test.go — the SEVENTH leg of the dispatch seam,
// driven through a REAL tree whose test files carry REAL function names.
//
// WHY THIS FILE EXISTS, in the incident's own terms (bug row 01a18f86): the epic
// named its tests in a SENTENCE — 「三条 delivery-retry 测试 … 必须转绿」 — and two
// of those names exist nowhere on that ground. The executor ran `go test -run
// '<them>'`, got `no tests to run` and exit 0, and reported a green that measured
// nothing. The sixth clause (task_briefing_run_alternatives.go) reads `-run`
// TOKENS and is silent on that shape by its own contract; this leg covers the
// prose shape, and it is driven over a real directory for the same reason its
// sibling is: a clause that judged names by grepping the briefing for its own
// words would reproduce the defect one level up.
//
// SEVEN LEGS:
//
//	① PROSE DEAD NAME — a sentence naming one live and one dead test stamps the
//	   clause, naming ONLY the dead one.
//	② POSITIVE CONTROL — a sentence whose every name is live stamps NOTHING,
//	   and still leaves a COUNT per name, so the ordinary briefing is silent
//	   while the measurement stays visible.
//	③ THE VACUUM FLOOR — no ground, a ground with no `*_test.go`, and a
//	   ground whose files yield no `func Test…` line are each STATED as
//	   unmeasured; a census that judged nothing never passes as green.
//	④ IDEMPOTENCE — a second pass over a stamped briefing appends nothing and
//	   says so.
//	⑤ PREFIX IS LIVE — `-run` matches UNANCHORED, so a name that is a strict
//	   prefix of a live test selects it and must NOT be stamped.
//	⑥ THE SIBLING OWNS `-run` LINES — a name standing on a `go test … -run`
//	   line is handed to the sixth clause and recorded as SKIP, never judged
//	   twice.
//	⑦ NO NAME IS A NO-OP — a briefing naming no `Test…` identifier is silent,
//	   both returns empty.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// proseTestFixtureFile is the file inside the fixture package carrying the names
// the reaching briefings select and the spellings the scan must refuse.
const proseTestFixtureFile = "probe_test.go"

// proseTestFixtureBody declares two live tests, one hook, and — deliberately —
// the spellings a lazy implementation would count: a `Test…` word in a COMMENT, a
// `Test…` word inside a string literal, and a helper that merely CALLS a
// test-looking name.
const proseTestFixtureBody = `package probe

// TestThisIsAComment is prose about a test, not a test.
const proseHelperLiteral = "\tTestIndentedInAStringIsNotATest("

func TestMain(m *testing.M) { os.Exit(m.Run()) }

func TestProbeAlpha(t *testing.T) {}

func TestProbeBetamax(t *testing.T) {}

func helperThatCallsTestProbeAlpha() { TestProbeAlpha(nil) }
`

// proseTestFixtureTree materialises a ground carrying that package, plus a
// production file holding the same spelling (`func TestOnlyInProduction`) which a
// census of `*_test.go` files must not count.
func proseTestFixtureTree(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	dir := filepath.Join(repo, "internal", "probe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, proseTestFixtureFile), []byte(proseTestFixtureBody), 0o644); err != nil {
		t.Fatal(err)
	}
	prod := "package probe\n\nfunc TestOnlyInProduction() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "probe.go"), []byte(prod), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

// proseTestBriefing renders the incident's shape: names in a SENTENCE, with no
// `-run` token anywhere (that is what the sixth clause owns).
func proseTestBriefing(sentence string) string {
	return "## Acceptance\n\n" + sentence + "\n"
}

// ① PROSE DEAD NAME — the measured incident shape: one name that exists, one
// that does not, written in prose.
func TestBriefingProseTestNames_ADeadNameInProseIsNamed(t *testing.T) {
	repo := proseTestFixtureTree(t)
	clause, record := TaskBriefingProseTestNamesRefresh(repo,
		proseTestBriefing("三条测试 TestProbeAlpha 与 TestDeliveryRetryIsIdempotentOnADeliveredRow 必须转绿。"))

	if clause == "" {
		t.Fatalf("no clause stamped for a prose sentence naming a dead test; record=%v", record)
	}
	if !strings.Contains(clause, TaskBriefingProseTestNamesLabel) {
		t.Errorf("clause does not carry the exported label: %q", clause)
	}
	if !strings.Contains(clause, "TestDeliveryRetryIsIdempotentOnADeliveredRow") {
		t.Errorf("clause does not name the DEAD test: %q", clause)
	}
	if strings.Contains(clause, `- "TestProbeAlpha" reaches`) {
		t.Errorf("clause names TestProbeAlpha, which DOES exist:\n%s", clause)
	}
	if !strings.Contains(clause, TaskBriefingProseTestNamesVerifyOnly) {
		t.Errorf("clause does not carry the instruction constant: %q", clause)
	}
	if !strings.Contains(clause, "NOT evidence that the work is done") {
		t.Errorf("clause does not deny the completion reading: %q", clause)
	}
	// The ZERO is restated in the record beside its census line (Rule 15).
	if !recordNames(t, record, "ZERO REACH", "TestDeliveryRetryIsIdempotentOnADeliveredRow") {
		t.Errorf("record carries no ZERO REACH line for the dead name: %v", record)
	}
	// And the census names the live one too, with its count.
	if !recordNames(t, record, "TestProbeAlpha", "1 top-level test function(s) of 2") {
		t.Errorf("census line for the live name missing or miscounted: %v", record)
	}
}

// ② POSITIVE CONTROL — every name present stamps NOTHING.
func TestBriefingProseTestNames_AFullyLiveSentenceStampsNothing(t *testing.T) {
	repo := proseTestFixtureTree(t)
	clause, record := TaskBriefingProseTestNamesRefresh(repo,
		proseTestBriefing("TestProbeAlpha 和 TestProbeBetamax 都必须转绿。"))

	if clause != "" {
		t.Fatalf("stamped a briefing whose every name exists: %q", clause)
	}
	if !recordNames(t, record, "TestProbeAlpha", "1 top-level test function(s) of 2") {
		t.Errorf("census line for TestProbeAlpha missing or miscounted: %v", record)
	}
	if !recordNames(t, record, "TestProbeBetamax") {
		t.Errorf("census line for TestProbeBetamax missing: %v", record)
	}
}

// ③ THE VACUUM FLOOR — a census that judged nothing is UNMEASURED, never green.
func TestBriefingProseTestNames_ACensusThatJudgedNothingIsStated(t *testing.T) {
	const dead = "TestDeliveryRetryBlockedThenDeliveredOnTheSamePath"
	t.Run("no ground", func(t *testing.T) {
		clause, record := TaskBriefingProseTestNamesRefresh("", proseTestBriefing(dead+" 必须转绿"))
		if clause != "" {
			t.Fatalf("stamped a verdict with no ground to census: %q", clause)
		}
		if !recordNames(t, record, "UNMEASURED", proseTestNoRepoReason) {
			t.Errorf("an absent ground must be STATED: %v", record)
		}
	})
	t.Run("no test files", func(t *testing.T) {
		repo := t.TempDir()
		if err := os.MkdirAll(filepath.Join(repo, "internal", "probe"), 0o755); err != nil {
			t.Fatal(err)
		}
		clause, record := TaskBriefingProseTestNamesRefresh(repo, proseTestBriefing(dead+" 必须转绿"))
		if clause != "" {
			t.Fatalf("stamped a verdict on a ground with no *_test.go: %q", clause)
		}
		if !recordNames(t, record, "UNMEASURED", proseTestNoTestFilesReason) {
			t.Errorf("a ground with no *_test.go must be STATED as unmeasured: %v", record)
		}
	})
	t.Run("no test names", func(t *testing.T) {
		repo := t.TempDir()
		dir := filepath.Join(repo, "internal", "probe")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, proseTestFixtureFile),
			[]byte("package probe\n\nfunc helper() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		clause, record := TaskBriefingProseTestNamesRefresh(repo, proseTestBriefing(dead+" 必须转绿"))
		if clause != "" {
			t.Fatalf("stamped a verdict on a corpus that yielded no test names: %q", clause)
		}
		if !recordNames(t, record, "UNMEASURED", proseTestNoNamesReason) {
			t.Errorf("a corpus with no `func Test…` line must be STATED: %v", record)
		}
	})
}

// ④ IDEMPOTENCE — a second pass appends nothing and says so.
func TestBriefingProseTestNames_ASecondPassAppendsNothing(t *testing.T) {
	repo := proseTestFixtureTree(t)
	briefing := proseTestBriefing("TestDeliveryRetryNowhere 必须转绿")
	first, _ := TaskBriefingProseTestNamesRefresh(repo, briefing)
	if first == "" {
		t.Fatal("first pass stamped nothing")
	}
	second, record := TaskBriefingProseTestNamesRefresh(repo, briefing+"\n\n"+first)
	if second != "" {
		t.Fatalf("second pass appended a clause again: %q", second)
	}
	if !recordNames(t, record, "UNMEASURED", proseTestAlreadyStamped) {
		t.Errorf("second pass did not state that it skipped: %v", record)
	}
}

// ⑤ PREFIX IS LIVE — `go test -run` matches unanchored, so `TestProbeAlpha` is
// selected by a briefing naming `TestProbe` — which therefore must not be stamped.
func TestBriefingProseTestNames_APrefixOfALiveTestIsLive(t *testing.T) {
	repo := proseTestFixtureTree(t)
	clause, record := TaskBriefingProseTestNamesRefresh(repo, proseTestBriefing("TestProbe 必须转绿"))
	if clause != "" {
		t.Fatalf("stamped %q, which SELECTS live tests under -run's unanchored matching: %q", "TestProbe", clause)
	}
	if !recordNames(t, record, "TestProbe", "2 top-level test function(s) of 2") {
		t.Errorf("the prefix should reach both live tests: %v", record)
	}
}

// ⑥ THE SIBLING OWNS `-run` LINES — a name on a `go test … -run` line is the
// sixth clause's, and this one records the hand-off rather than judging it twice.
func TestBriefingProseTestNames_ARunLineNameIsHandedToTheSibling(t *testing.T) {
	repo := proseTestFixtureTree(t)
	briefing := "## Acceptance\n\n" +
		"go test ./internal/probe/ -run 'TestProbeNowhere' -count=1\n"
	clause, record := TaskBriefingProseTestNamesRefresh(repo, briefing)
	if clause != "" {
		t.Fatalf("judged a name the SIXTH clause owns: %q", clause)
	}
	if !recordNames(t, record, "SKIP", "TestProbeNowhere", proseTestSiblingOwnedReason) {
		t.Errorf("the hand-off to the sibling is not recorded: %v", record)
	}
	// The sibling really does judge it — the control that makes the hand-off
	// honest rather than an excuse.
	sibling, sibRecord := TaskBriefingRunAlternativesRefresh(repo, briefing)
	if sibling == "" {
		t.Fatalf("the sixth clause is silent on a dead `-run` alternative: %v", sibRecord)
	}
	if !strings.Contains(sibling, "TestProbeNowhere") {
		t.Errorf("the sixth clause did not name the dead alternative: %q", sibling)
	}
}

// ⑦ NO NAME IS A NO-OP — the ordinary briefing.
func TestBriefingProseTestNames_NoNameIsANoOp(t *testing.T) {
	repo := proseTestFixtureTree(t)
	clause, record := TaskBriefingProseTestNamesRefresh(repo, "## Acceptance\n\ngo build ./...\n")
	if clause != "" || len(record) != 0 {
		t.Fatalf("a briefing naming no Test… identifier must be silent: clause=%q record=%v", clause, record)
	}
}

// ⑧ THE SCAN REFUSES NON-TEST SPELLINGS — a name appearing only in a comment, a
// string literal, or a production .go file is still DEAD.
func TestBriefingProseTestNames_NonTestSpellingsDoNotCount(t *testing.T) {
	repo := proseTestFixtureTree(t)
	clause, record := TaskBriefingProseTestNamesRefresh(repo,
		proseTestBriefing("TestThisIsAComment 和 TestIndentedInAStringIsNotATest 和 TestOnlyInProduction 必须转绿"))
	if clause == "" {
		t.Fatalf("a sentence naming only non-test spellings stamped nothing; record=%v", record)
	}
	for _, dead := range []string{"TestThisIsAComment", "TestIndentedInAStringIsNotATest", "TestOnlyInProduction"} {
		if !strings.Contains(clause, dead) {
			t.Errorf("clause does not name %q as dead (a spelling that is not a top-level test):\n%s", dead, clause)
		}
	}
}

// ⑨ A PATH COMPONENT IS NOT A NAME — MEASURED on this clause's first real run:
// by the time it judges a briefing, the ground announcement is already in it, and
// a Go test's temp directories are named after the test that made them, so
// `/dev/shm/…/TestSomeTest2174970253/005/gopi-…` carries `Test…` path components.
// Stamping those would accuse the one briefing that is perfectly correct.
func TestBriefingProseTestNames_APathComponentIsNotAName(t *testing.T) {
	repo := proseTestFixtureTree(t)
	ground := "/dev/shm/suite-cmd-agent-run-1/TestBriefingProseTestNames_APathComponentIsNotAName2174970253/005/gopi-2a7c5b91"
	briefing := proseTestBriefing("THIS RUN ALREADY HAS ITS WORKTREE. You are standing in it: " + ground +
		" — and TestProbeAlpha 必须转绿。")
	clause, _ := TaskBriefingProseTestNamesRefresh(repo, briefing)
	if clause != "" {
		t.Fatalf("stamped a bare PATH COMPONENT as a dead test name: %q", clause)
	}
	// The boundary must not swallow the legitimate shapes beside it: a name in
	// parentheses, in backticks, and a subtest selector all still count.
	clause, record := TaskBriefingProseTestNamesRefresh(repo, proseTestBriefing(
		"(TestProbeAlpha) 与 `TestProbeBetamax/DarkCase` 必须转绿，但 TestNoSuchThing 不。"))
	if clause == "" || !strings.Contains(clause, "TestNoSuchThing") {
		t.Fatalf("the boundary rule swallowed a real dead name; clause=%q record=%v", clause, record)
	}
	for _, live := range []string{"TestProbeAlpha", "TestProbeBetamax"} {
		if strings.Contains(clause, `- "`+live+`" reaches`) {
			t.Errorf("%q exists and must not be stamped:\n%s", live, clause)
		}
	}
}
