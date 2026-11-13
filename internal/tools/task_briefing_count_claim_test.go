package tools

// task_briefing_count_claim_test.go — the THIRD leg of the dispatch seam, driven
// through a REAL tree whose files have REAL line counts.
//
// FIVE LEGS, and the second one is why this file exists:
//
//	① RED — a briefing quoting a population figure this ground reads differently
//	   gets the stamp, naming the file, the briefing's figure, the count taken
//	   HERE and what to do about it.
//	② POSITIVE CONTROL — the SAME briefing shape quoting a figure this ground
//	   AGREES with gets NO stamp. Without this leg, leg ① would pass on a
//	   function that stamps every briefing quoting any number — which is the
//	   false-positive surface the predicate was tuned against (measured: 8
//	   firings over 400 real payloads before the bounds landed, 1 after).
//	③ THE UNMEASURABLE ARM — a figure naming a file this ground does not have
//	   must be STATED (Rule 15), never read as a pass.
//	④ NO CLAIM — an ordinary briefing that quotes no figure is a silent no-op
//	   (the reason this clause cannot degrade every briefing it touches).
//	⑤ IDEMPOTENCE — a second pass over a stamped briefing appends nothing and
//	   says so.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// countClauseFixtureFile is the file the fixture briefings quote a figure about.
const countClauseFixtureFile = "internal/docclass/testdata/anchor-number-baseline.tsv"

// countClauseFixtureLines is the figure THIS ground actually carries — the
// number the positive-control leg must quote and the red leg must contradict.
const countClauseFixtureLines = 168

// countClauseBuildTree materialises a tree carrying the fixture file with
// countClauseFixtureLines lines, so the count this seam takes is a real one over
// a real file (never a mocked number: the whole point of the clause is that the
// number comes from the tree).
func countClauseBuildTree(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	path := filepath.Join(repo, countClauseFixtureFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("doc\tpath\tanchor\n")
	for i := 1; i < countClauseFixtureLines; i++ {
		b.WriteString("devdoc/x.md\tinternal/x.go\t1\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

// countClauseBriefing renders the shape the incident's briefing carried: a
// population figure quoted about a named file, in the present tense, with the
// file in the same clause (row 01a13781's EPIC GOAL, verbatim in structure).
func countClauseBriefing(figure int) string {
	return "# Epic: repair the frozen permit table\n\n" +
		"On THIS run's worktree: `internal/docclass/testdata/anchor-number-baseline.tsv` " +
		"carries " + itoaForTest(figure) + " rows while the tree produces fewer, reddening three tests.\n"
}

// TestBriefingCount_AMovedFigureIsStamped is leg ① — the RED PROOF: the briefing
// quotes a figure this ground does not read, so the child must be told, with the
// measurement behind it.
func TestBriefingCount_AMovedFigureIsStamped(t *testing.T) {
	repo := countClauseBuildTree(t)
	const claimed = countClauseFixtureLines + 1 // the briefing's figure is stale
	clause, record := TaskBriefingCountRefresh(repo, countClauseBriefing(claimed))
	if clause == "" {
		t.Fatalf("a briefing quoting %d rows for a file this ground reads as %d got no stamp — "+
			"the child re-derives a measurement taken in another tree; record: %v",
			claimed, countClauseFixtureLines, record)
	}
	for _, want := range []string{
		TaskBriefingCountLabel,               // the clause's own marker
		countClauseFixtureFile,               // the file the figure was about
		itoaForTest(claimed),                 // the briefing's figure
		itoaForTest(countClauseFixtureLines), // what THIS ground reads
		TaskBriefingCountVerifyOnly,          // what to do about it
		"NOT evidence that the work is done", // the denial of the completion reading
	} {
		if !strings.Contains(clause, want) {
			t.Errorf("the stamped clause must carry %q; clause:\n%s", want, clause)
		}
	}
	// THE RECORD IS THE STAMP'S EVIDENCE (Rule 15).
	if len(record) != 1 || !strings.Contains(record[0], "STAMPED") {
		t.Fatalf("the seam's record must carry the moved verdict; got %v", record)
	}
	if !strings.Contains(record[0], itoaForTest(countClauseFixtureLines)) {
		t.Errorf("the record line must name the count taken here; got %q", record[0])
	}
}

// TestBriefingCount_AnAgreeingFigureIsNotStamped is leg ② — the POSITIVE CONTROL.
// The same detector, the same sentence shape, a figure this ground agrees with:
// the briefing stands, nothing is stamped, and the agreement still reaches the
// record.
func TestBriefingCount_AnAgreeingFigureIsNotStamped(t *testing.T) {
	repo := countClauseBuildTree(t)
	clause, record := TaskBriefingCountRefresh(repo, countClauseBriefing(countClauseFixtureLines))
	if clause != "" {
		t.Fatalf("an AGREEING figure was stamped — this clause would fire on every honest briefing that "+
			"quotes a current number; clause:\n%s", clause)
	}
	if len(record) != 1 || !strings.Contains(record[0], "agrees") {
		t.Fatalf("the positive control must still be STATED in the record (Rule 15); got %v", record)
	}
}

// TestBriefingCount_AMissingFileIsStatedNotPassed is leg ③ — the unmeasurable
// arm. A figure about a file this ground does not have is reported, never read as
// a pass and never as a refusal of the run.
func TestBriefingCount_AMissingFileIsStatedNotPassed(t *testing.T) {
	repo := countClauseBuildTree(t)
	briefing := "The table `internal/docclass/testdata/not-in-this-tree.tsv` carries 42 rows.\n"
	clause, record := TaskBriefingCountRefresh(repo, briefing)
	if clause != "" {
		t.Errorf("a figure that could not be measured must not be stamped; clause:\n%s", clause)
	}
	if len(record) != 1 || !strings.Contains(record[0], "UNMEASURED") {
		t.Fatalf("the unmeasured arm must be STATED, never silent (Rule 15); got %v", record)
	}
}

// TestBriefingCount_NoClaimIsANoOp is leg ④ — the ordinary briefing, and the
// reason this cannot degrade every briefing the seam touches.
func TestBriefingCount_NoClaimIsANoOp(t *testing.T) {
	repo := countClauseBuildTree(t)
	for _, briefing := range []string{
		"# Epic\n\nFix the read path in internal/tools/read.go.\n",
		"# Epic\n\nThe table carries rows while the tree produces fewer rows.\n",
		"# Epic\n\nThe table carried 168 rows before the repair.\n",
	} {
		clause, record := TaskBriefingCountRefresh(repo, briefing)
		if clause != "" || len(record) != 0 {
			t.Errorf("a briefing with no population figure must be a no-op; got clause=%q record=%v\n  briefing: %s",
				clause, record, briefing)
		}
	}
}

// TestBriefingCount_NoRepoIsStated is the sibling of leg ③ one level up: no tree
// to measure in is a stated reason, never a silent pass.
func TestBriefingCount_NoRepoIsStated(t *testing.T) {
	clause, record := TaskBriefingCountRefresh("", countClauseBriefing(countClauseFixtureLines))
	if clause != "" {
		t.Errorf("no repo ⇒ nothing measured ⇒ nothing stamped; got %q", clause)
	}
	if len(record) != 1 || !strings.Contains(record[0], "UNMEASURED") {
		t.Fatalf("an empty repo must be STATED (Rule 15); got %v", record)
	}
}

// TestBriefingCount_ASecondPassAppendsNothing is leg ⑤ — idempotence. The clause
// QUOTES the briefing line it contradicts, and that quote carries the file token
// the scanner looks for, so a resume/re-dispatch/operator paste must not stack a
// second copy (the same contract its two siblings keep).
func TestBriefingCount_ASecondPassAppendsNothing(t *testing.T) {
	repo := countClauseBuildTree(t)
	first, _ := TaskBriefingCountRefresh(repo, countClauseBriefing(countClauseFixtureLines+1))
	if first == "" {
		t.Fatal("leg ① regressed: the first pass stamped nothing")
	}
	stamped := countClauseBriefing(countClauseFixtureLines+1) + "\n\n" + first
	second, record := TaskBriefingCountRefresh(repo, stamped)
	if second != "" {
		t.Errorf("a second pass over a stamped briefing appended a clause:\n%s", second)
	}
	if len(record) != 1 || !strings.Contains(record[0], "already carries") {
		t.Fatalf("the second pass must SAY it appended nothing (Rule 15); got %v", record)
	}
}

// TestBriefingCount_CoversTheShapeTheBaseClauseCannot is THE test for this
// file's reason to exist, and it drives BOTH clauses side by side.
//
// The gap: TaskBriefingPremiseBaseRefresh stamps only when the declared base is
// BEHIND the tip — its positive control stamps nothing at ahead == 0, and a
// briefing declaring NO base produces no claim at all. So a briefing whose base
// declaration is CURRENT (here: the tip itself) while its quoted figure has gone
// stale reached the child unmarked. This leg pins the pair: the base clause is
// SILENT and the figure clause is NOT, from the same briefing, at the same seam.
func TestBriefingCount_CoversTheShapeTheBaseClauseCannot(t *testing.T) {
	repo := countClauseBuildTree(t)
	// Declaring the ground's own tip is the ahead == 0 arm — and the helper's
	// empty-repo case is the no-declaration arm; both are silent for the base
	// clause, which is exactly why the figure clause has to exist.
	_, tipOut := deployDriftGit(t, repo, "rev-parse", IntegrationBranchName)
	tip := strings.TrimSpace(tipOut)
	if tip == "" {
		t.Fatal("fixture tip unreadable")
	}
	briefing := countClauseBriefing(countClauseFixtureLines+1) +
		"\nMeasured at base " + tip + ".\n"
	baseClause, _ := TaskBriefingPremiseBaseRefresh(repo, briefing)
	if baseClause != "" {
		t.Fatalf("this leg needs the base clause to be SILENT (the measured gap); it stamped %d chars", len(baseClause))
	}
	clause, record := TaskBriefingCountRefresh(repo, briefing)
	if clause == "" {
		t.Fatalf("a briefing whose base is CURRENT and whose figure is STALE must still be stamped; record: %v", record)
	}
	if !strings.Contains(clause, TaskBriefingCountVerifyOnly) {
		t.Errorf("the clause must carry its instruction %q; clause:\n%s", TaskBriefingCountVerifyOnly, clause)
	}
}

// TestBriefingCount_CountsTheFixtureExactly pins the counter itself against the
// fixture: if the reader drifts, every leg above would still pass while measuring
// the wrong number, and the clause would stamp an honest briefing.
func TestBriefingCount_CountsTheFixtureExactly(t *testing.T) {
	repo := countClauseBuildTree(t)
	n, err := countFileLines(filepath.Join(repo, countClauseFixtureFile))
	if err != nil {
		t.Fatal(err)
	}
	if n != countClauseFixtureLines {
		t.Fatalf("countFileLines() = %d, want %d — the fixture and the counter disagree", n, countClauseFixtureLines)
	}
	// A file with no trailing newline still counts its last line.
	path := filepath.Join(repo, "no-trailing-newline.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, err = countFileLines(path); err != nil || n != 3 {
		t.Fatalf("countFileLines(no trailing newline) = %d, %v; want 3", n, err)
	}
	// A DIRECTORY is refused, stated — never counted as 0 lines.
	if _, err := countFileLines(filepath.Join(repo, "internal")); err == nil {
		t.Error("counting a directory must fail, not answer 0")
	}
}

// TestBriefingCount_OutsideTheGroundIsRefused pins the containment check: a
// figure naming a path outside this run's ground cannot be re-measured here, and
// the arm is stated rather than silently skipped.
func TestBriefingCount_OutsideTheGroundIsRefused(t *testing.T) {
	repo := countClauseBuildTree(t)
	if _, reason := taskBriefingCountPath(repo, "./../../etc/hosts"); reason == "" {
		t.Error("a path escaping the ground must be refused by name")
	}
	if _, reason := taskBriefingCountPath(repo, countClauseFixtureFile); reason != "" {
		t.Errorf("a path inside the ground must resolve; got reason %q", reason)
	}
}
