package tools

// task_briefing_census_revision_test.go — the anchor that dates every census the
// dispatch seam stamps into a child's briefing (epic 01a1abcd, 2026-11-05).
//
// THE DEFECT, and why an anchor is the fix. The seam's clauses quote MEASURED
// numbers ("this ground counts 3105 top-level test function(s) there", "7 of
// these path(s) do not resolve inside this ground"). A tree is not a constant —
// the child commits into it, a sibling landing moves it, a re-dispatch reads a
// different one — so a count with nothing beside it is an observation wearing the
// clothes of a standing fact. This clause is the date. It changes NO judgement
// (which names are dead stays the sixth clause's business); it makes the numbers
// answerable: "3105 vs 3094 — at which revision?"
//
// FIVE LEGS:
//
//	① MEASURED — a real repository fixture answers with its own commit, the
//	   clause names it, and the answer tracks HEAD (commit again ⇒ the clause
//	   names the NEW commit). A stamp that always printed the same string would
//	   pass a "does it say something" arm and fail this one.
//	② UNREAD IS STATED, NEVER OMITTED — a directory that is not a repository
//	   yields the unread sentence carrying git's own exit and stderr, so the
//	   reader can reproduce the failure (Rule 15).
//	③ NO GROUND AT ALL — an empty path states its own reason instead of
//	   rendering "" (which would append nothing and read like success).
//	④ ONE LINE — whatever git prints (its stderr is multi-line), the sentence
//	   the child reads is one line, because the anchor is a paragraph and an
//	   embedded newline splits one finding into two.
//	⑤ THE CONSTANT IS THE PRODUCER'S — the seam's own marker and the clause's
//	   opening words come from the exported constants, so no arm can pass on a
//	   second literal that has drifted from the sentence under test.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// revFixtureRepo builds a real one-commit repository and returns its directory
// and short HEAD. It is a REAL git (not a stub) because the clause's whole claim
// is that it reports what git answers — a fake would let a broken invocation pass.
func revFixtureRepo(t *testing.T) (dir, rev string) {
	t.Helper()
	dir = t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
			"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "fixture")
	if err := os.WriteFile(filepath.Join(dir, "fixture.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "fixture.txt")
	run("commit", "-q", "-m", "fixture: one")
	return dir, run("rev-parse", "--short", "HEAD")
}

// ① MEASURED — the anchor names the ground's own commit, and it MOVES with HEAD.
func TestBriefingCensusRevision_NamesTheGroundsOwnCommit(t *testing.T) {
	dir, rev := revFixtureRepo(t)
	clause, record := TaskBriefingCensusRevisionClause(dir)

	if !strings.HasPrefix(clause, TaskBriefingCensusRevisionAnchor+" "+rev) {
		t.Fatalf("clause does not open with %q followed by the ground's HEAD %q:\n%s",
			TaskBriefingCensusRevisionAnchor, rev, clause)
	}
	if !strings.Contains(clause, dir) {
		t.Errorf("clause does not name the ground it measured:\n%s", clause)
	}
	if strings.Contains(clause, "UNREAD") {
		t.Errorf("a readable ground produced the unread arm:\n%s", clause)
	}
	if !strings.Contains(record, rev) {
		t.Errorf("the seam record does not carry the revision: %q", record)
	}

	// THE ANSWER TRACKS THE GROUND. A producer that printed a constant (or the
	// empty string) would satisfy every arm above; this one cannot.
	if err := os.WriteFile(filepath.Join(dir, "fixture.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "fixture.txt"}, {"commit", "-q", "-m", "fixture: two"}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
			"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	next, err := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	moved, _ := TaskBriefingCensusRevisionClause(dir)
	if !strings.HasPrefix(moved, TaskBriefingCensusRevisionAnchor+" "+strings.TrimSpace(string(next))) {
		t.Fatalf("the anchor did not follow HEAD after a new commit: got %q want prefix %q",
			moved, strings.TrimSpace(string(next)))
	}
	if strings.TrimSpace(string(next)) == rev {
		t.Skip("the fixture's second commit hashed to the same short id as the first — the movement arm cannot distinguish them here")
	}
}

// ② UNREAD IS STATED — a directory that is not a git repository says so, with
// git's own exit code and its stderr, and denies the counts as today's facts.
func TestBriefingCensusRevision_AnUnreadRevisionIsStated(t *testing.T) {
	dir := t.TempDir() // a real directory, deliberately NOT a repository
	clause, record := TaskBriefingCensusRevisionClause(dir)

	if !strings.Contains(clause, "UNREAD") {
		t.Fatalf("an unresolvable revision produced a clause that does not say UNREAD:\n%s", clause)
	}
	if !strings.Contains(clause, "exit=") {
		t.Errorf("the stated reason carries no exit code (unreproducible finding):\n%s", clause)
	}
	if !strings.Contains(clause, "rev-parse") {
		t.Errorf("the stated reason does not name the command that failed:\n%s", clause)
	}
	if !strings.Contains(clause, "must not be quoted as today's") {
		t.Errorf("the unread arm does not deny the counts as today's facts:\n%s", clause)
	}
	if !strings.Contains(record, "UNREAD") && !strings.Contains(record, "did not resolve") {
		t.Errorf("the seam record does not state the unread revision: %q", record)
	}
}

// ③ NO GROUND — an empty path states its own reason rather than rendering "".
func TestBriefingCensusRevision_NoGroundIsStatedNotSilent(t *testing.T) {
	for _, empty := range []string{"", "   ", "\n"} {
		clause, record := TaskBriefingCensusRevisionClause(empty)
		if strings.TrimSpace(clause) == "" {
			t.Fatalf("an empty ground rendered an empty clause (a silent skip dressed as success): %q", empty)
		}
		if !strings.Contains(clause, briefingCensusRevisionNoGroundReason) {
			t.Errorf("clause for ground %q does not state %q:\n%s", empty,
				briefingCensusRevisionNoGroundReason, clause)
		}
		if !strings.Contains(record, briefingCensusRevisionNoGroundReason) {
			t.Errorf("record for ground %q does not state the reason: %q", empty, record)
		}
	}
}

// ④ ONE LINE — git's multi-line stderr never splits the sentence the child reads.
func TestBriefingCensusRevision_TheSentenceIsOneLine(t *testing.T) {
	dir := t.TempDir()
	clause, record := TaskBriefingCensusRevisionClause(dir)
	if strings.Contains(strings.TrimSuffix(clause, "\n"), "\n") {
		t.Fatalf("the anchor carried an embedded newline (one finding split into two):\n%q", clause)
	}
	if strings.Contains(strings.TrimSuffix(record, "\n"), "\n") {
		t.Fatalf("the record line carried an embedded newline:\n%q", record)
	}
}

// ⑤ THE CONSTANT IS THE PRODUCER'S — the opening words come from the exported
// constant, so a drifted second literal cannot satisfy these arms.
func TestBriefingCensusRevision_TheOpeningIsTheExportedConstant(t *testing.T) {
	dir, _ := revFixtureRepo(t)
	clause, _ := TaskBriefingCensusRevisionClause(dir)
	if !strings.HasPrefix(clause, TaskBriefingCensusRevisionAnchor) {
		t.Fatalf("clause does not open with TaskBriefingCensusRevisionAnchor (%q):\n%s",
			TaskBriefingCensusRevisionAnchor, clause)
	}
	if !strings.Contains(TaskBriefingCensusRevisionLineFmt, "%s") {
		t.Errorf("the seam marker is not a format carrying the target: %q", TaskBriefingCensusRevisionLineFmt)
	}
}

// ⑥ THE REAL GROUND — the arm that runs against THIS repository rather than a
// fixture, because a clause that only works on a temp dir nobody dispatches from
// has not been exercised on the shape it will meet (Rule 31). The expected value
// is read independently, with plain `git`, so the two readings can disagree.
func TestBriefingCensusRevision_OverThisGround(t *testing.T) {
	ground := "../.."
	out, err := exec.Command("git", "-C", ground, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		t.Skipf("this checkout is not a git ground (%v) — the fixture legs cover the shape", err)
	}
	rev := strings.TrimSpace(string(out))
	clause, record := TaskBriefingCensusRevisionClause(ground)
	if !strings.HasPrefix(clause, TaskBriefingCensusRevisionAnchor+" "+rev) {
		t.Fatalf("the real ground's anchor is not this ground's HEAD %q:\n%s", rev, clause)
	}
	t.Logf("real-ground anchor: %s", strings.TrimSpace(clause))
	t.Logf("real-ground record: %s", strings.TrimSpace(record))
}
