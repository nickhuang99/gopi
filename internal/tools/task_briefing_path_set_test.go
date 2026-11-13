package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pathSetBriefingFixture is the SHAPE of row 01a1447c's own briefing — the row
// this clause was measured from — reduced to the parts the clause reads.
const pathSetBriefingFixture = "## 要做的\n" +
	"\n" +
	"用 manifest 的**唯一写入者**逐行重生成这 12 行（Rule 36：manifest 是 DATA，绝不手改单元格）。\n" +
	"\n" +
	"```bash\n" +
	"for p in devdoc/a.md \\\n" +
	"         devdoc/b.md \\\n" +
	"         devdoc/c.md; do\n" +
	"  python3 tools/docclass/classify.py . --only \"$p\"\n" +
	"done\n" +
	"```\n"

// TestBriefingPathSet_StampsThePinnedSet is the RED-then-GREEN arm: the incident's
// shape gets an instruction naming the surface's report-only mode, the count it
// must diff against, and the explicit denial that a moved set means the work is
// done.
func TestBriefingPathSet_StampsThePinnedSet(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"devdoc/a.md", "devdoc/b.md", "devdoc/c.md"} {
		writeBriefingFixtureFile(t, dir, p)
	}
	clause, record := TaskBriefingPathSetRefresh(dir, pathSetBriefingFixture)
	if clause == "" {
		t.Fatalf("no clause appended; record = %v", record)
	}
	for _, want := range []string{
		TaskBriefingPathSetLabel,
		TaskBriefingPathSetVerifyOnly,
		"12 行",
		"devdoc/a.md",
		"This clause is NOT evidence that the work is done.",
	} {
		if !strings.Contains(clause, want) {
			t.Errorf("the clause must carry %q; got:\n%s", want, clause)
		}
	}
	// The clause must SAY it did not measure — the honest half (Rule 15).
	if !strings.Contains(clause, "DID NOT RE-MEASURE IT FOR YOU") {
		t.Errorf("the clause must state that this seam did not re-measure the set; got:\n%s", clause)
	}
	if len(record) == 0 {
		t.Fatal("a stamp must leave a record line stating what was examined")
	}
}

// TestBriefingPathSet_OrdinaryBriefingIsSilent is the POSITIVE CONTROL: a briefing
// that pins no path set appends nothing and says nothing — the reason this clause
// cannot degrade every dispatch.
func TestBriefingPathSet_OrdinaryBriefingIsSilent(t *testing.T) {
	clause, record := TaskBriefingPathSetRefresh(t.TempDir(), "Fix the timeout wording in one error message.\n")
	if clause != "" || len(record) != 0 {
		t.Fatalf("clause = %q, record = %v; an ordinary briefing must be untouched", clause, record)
	}
}

// TestBriefingPathSet_SecondPassAppendsNothing pins idempotence: the clause QUOTES
// the briefing line it contradicts, so a re-read of a stamped briefing must not
// re-fire on its own quote (a resume, a re-dispatch, an operator paste).
func TestBriefingPathSet_SecondPassAppendsNothing(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"devdoc/a.md", "devdoc/b.md", "devdoc/c.md"} {
		writeBriefingFixtureFile(t, dir, p)
	}
	clause, _ := TaskBriefingPathSetRefresh(dir, pathSetBriefingFixture)
	if clause == "" {
		t.Fatal("first pass must stamp")
	}
	second, record := TaskBriefingPathSetRefresh(dir, pathSetBriefingFixture+"\n\n"+clause)
	if second != "" {
		t.Fatalf("second pass appended a clause:\n%s", second)
	}
	if len(record) == 0 || !strings.Contains(record[0], taskBriefingPathSetAlreadyStampedReason) {
		t.Fatalf("second pass must STATE why it appended nothing, record = %v", record)
	}
}

// TestBriefingPathSet_NoGroundIsStated is the unmeasured arm: with no ground to
// resolve the paths against, nothing is stamped and the reason is recorded — a
// silent skip here would be indistinguishable from a briefing that pins no set.
func TestBriefingPathSet_NoGroundIsStated(t *testing.T) {
	clause, record := TaskBriefingPathSetRefresh("", pathSetBriefingFixture)
	if clause != "" {
		t.Fatalf("no ground must stamp nothing, got:\n%s", clause)
	}
	if len(record) == 0 || !strings.Contains(record[0], taskBriefingPathSetNoGroundReason) {
		t.Fatalf("the no-ground arm must be STATED, record = %v", record)
	}
}

// TestBriefingPathSet_SetOutsideTheGroundIsStated is the arm a pinned set from
// ANOTHER checkout reaches: every path lies outside this ground, so the set is not
// a population of this tree and the clause names that instead of stamping a count
// the tree cannot answer for.
func TestBriefingPathSet_SetOutsideTheGroundIsStated(t *testing.T) {
	clause, record := TaskBriefingPathSetRefresh(t.TempDir(), pathSetBriefingFixture)
	if clause != "" {
		t.Fatalf("a set entirely outside the ground must stamp nothing, got:\n%s", clause)
	}
	if len(record) == 0 || !strings.Contains(record[0], taskBriefingPathSetOutsideReason) {
		t.Fatalf("the outside-the-ground arm must be STATED, record = %v", record)
	}
}

// TestBriefingPathSet_PathsTextStatesItsOwnElision pins the bound: a set longer
// than the clause renders is elided, and the elision is visible in the child's
// text rather than leaving it believing it was handed the whole set (Rule 15).
func TestBriefingPathSet_PathsTextStatesItsOwnElision(t *testing.T) {
	var paths []string
	for i := 0; i < taskBriefingPathSetMaxPaths+3; i++ {
		paths = append(paths, "devdoc/p.md")
	}
	text := TaskBriefingPathSetPathsText(paths)
	if !strings.Contains(text, "3 more") {
		t.Fatalf("elision must be stated, got %q", text)
	}
}

// writeBriefingFixtureFile materialises one file under dir, so the clause's
// ground-relative resolution has something real to resolve — the path resolver it
// shares with the figure clause is a filesystem question, not a string one.
func writeBriefingFixtureFile(t *testing.T, dir, rel string) {
	t.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(abs, []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}
