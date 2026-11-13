package tools

// task_briefing_gate_assert_test.go — the DISPATCH-TIME half of the
// gate-assertion seam, driven through REAL git in a REAL repository (the shape
// task_briefing_base_test.go uses, because the verdict here is a commit distance
// and a text match would not be one).
//
// FIVE ARMS, and the controls matter more than the positive:
//
//	① RED — a briefing asserting "SURFACE is RED on master" is stamped when the
//	   ROW's authoring moment resolves to a revision the integration branch has
//	   moved past. The stamp names the surface, the authoring moment, the
//	   revision it resolves to and the distance, and it carries the instruction
//	   and the DENIAL of the completion reading.
//	② POSITIVE CONTROL — the SAME detector, a row authored AT the integration
//	   tip, stamps NOTHING and RECORDS the zero distance. A guard that stamped
//	   everything is as useless as one that stamps nothing, and here the noise is
//	   worse than noise: a child told to re-measure a genuinely due surface
//	   learns to ignore the clause.
//	③ THE UNREADABLE ARM (Rule 15) — an assertion the scanner SAW but could not
//	   attribute to a surface is STATED, never silently dropped and never
//	   stamped: nothing was measured, so nothing may be asserted.
//	④ THE UNANCHORED ARM (Rule 15) — a row whose authoring time was not read (or
//	   a ground that is not a repo) leaves every assertion UNMEASURED. The seam
//	   must not invent a moment; a reading about a moment the author never wrote
//	   at would be a false positive on an honest briefing.
//	⑤ THE ORDINARY BRIEFING — no such assertion at all yields no clause and no
//	   record: a briefing-in / no-op-out, indistinguishable from the seam never
//	   having run.

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	gateAssertFixtureMsg        = "seed"
	gateAssertFixtureStepMsg    = "step"
	gateAssertFixtureStepSecs   = 60
	gateAssertFixtureBaseEpoch  = 1700000000
	gateAssertFixtureSurfaceRun = "`go test -run TestDocClassBannerMatchesManifest ./internal/standards/`"
)

// gateAssertFixture is the two inputs the arms need: a repository whose
// integration branch carries commits at KNOWN epoch seconds, and the epochs of
// those commits (so a test can author a row "at" one of them).
type gateAssertFixture struct {
	Repo string
	// Tip is the integration tip; its epoch is the last entry of Epochs.
	Tip string
	// Epochs are the commit dates, oldest first — the moments a row can be
	// authored at. The FIRST one is a revision many commits behind the tip.
	Epochs []int64
}

// gateAssertFixtureAhead is how many commits the fixture's integration branch
// carries, so the expected distance of the oldest epoch is a named quantity
// rather than a number re-derived in each arm.
const gateAssertFixtureAhead = 3

// gateAssertBuildRepo builds the fixture with REAL git commands, each commit
// carrying an EXPLICIT author/committer date: `git rev-list --before=<epoch>` is
// the seam's own anchor, and a fixture built from wall-clock commits could not
// pin a distance at all.
func gateAssertBuildRepo(t *testing.T) gateAssertFixture {
	t.Helper()
	repo := t.TempDir()
	epochs := make([]int64, 0, gateAssertFixtureAhead+1)
	git := func(at int64, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), premiseGitTimeout)
		defer cancel()
		stamp := strconv.FormatInt(at, 10) + " +0000"
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp,
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(gateAssertFixtureBaseEpoch, "init", "-q", "-b", IntegrationBranchName)
	epochs = append(epochs, gateAssertFixtureBaseEpoch)
	git(gateAssertFixtureBaseEpoch, "commit", "--allow-empty", "-q", "-m", gateAssertFixtureMsg)
	for i := 1; i <= gateAssertFixtureAhead; i++ {
		at := int64(gateAssertFixtureBaseEpoch + i*gateAssertFixtureStepSecs)
		git(at, "commit", "--allow-empty", "-q", "-m", gateAssertFixtureStepMsg)
		epochs = append(epochs, at)
	}
	tip := git(gateAssertFixtureBaseEpoch, "rev-parse", "HEAD")
	return gateAssertFixture{Repo: repo, Tip: tip, Epochs: epochs}
}

// gateAssertBriefing renders the motivating sentence shape: a fenced command
// asserted RED on the integration branch.
func gateAssertBriefing(surface string) string {
	return "# Symptom\n\n" + surface + " is RED on master, in a file this branch does not touch:\n\n" +
		"```\n--- FAIL: TestDocClassBannerMatchesManifest (0.01s)\n```\n\n" +
		"## What to do\n\nGive the file its banner and re-run the gate.\n"
}

// TestBriefingGateAssert_AStaleAssertionIsStampedAndTellsTheChildToReMeasure is
// leg ① — plus the clause's boundary, asserted on the text the child receives: a
// moved branch must NOT be readable as completion.
func TestBriefingGateAssert_AStaleAssertionIsStampedAndTellsTheChildToReMeasure(t *testing.T) {
	f := gateAssertBuildRepo(t)
	clause, record := TaskBriefingGateAssertRefresh(f.Repo, float64(f.Epochs[0]), gateAssertBriefing(gateAssertFixtureSurfaceRun))
	if clause == "" {
		t.Fatalf("a briefing asserting a surface RED on %s as of %s (%d commit(s) behind the tip) got no "+
			"stamp; record: %v", IntegrationBranchName, formatTaskTime(float64(f.Epochs[0])), gateAssertFixtureAhead, record)
	}
	for _, want := range []string{
		TaskBriefingGateAssertLabel,
		TaskBriefingGateAssertReMeasure,
		"NOT evidence that the work is done", // the forbidden reading, denied
		gateAssertFixtureSurfaceRun,          // the surface, so the child can re-run it
		formatTaskTime(float64(f.Epochs[0])), // the anchor that produced the reading
		strconv.FormatInt(f.Epochs[0], 10),   // the epoch the command takes
		strconv.Itoa(gateAssertFixtureAhead), // the measured distance
	} {
		if !strings.Contains(clause, want) {
			t.Errorf("the clause must carry %q; clause:\n%s", want, clause)
		}
	}
	// The clause renders the SAME measurement the record reports, and the
	// distance it states is the one this fixture was built with (Rules 8/13:
	// measure once, read twice).
	wantRec := "is " + strconv.Itoa(gateAssertFixtureAhead) + " commit(s) BEHIND"
	if len(record) != 1 || !strings.Contains(record[0], wantRec) || !strings.Contains(record[0], "STAMPED") {
		t.Fatalf("the record must carry the stale verdict and the measured distance; got %v", record)
	}
	// The revision the clause resolved must be the revision the authoring MOMENT
	// resolves to — read back through the seam's own resolver, so the arm cannot
	// pass by agreeing with a second, differently-derived sha.
	authored, err := gateAssertRevisionAt(f.Repo, IntegrationClaimTargetRef(f.Repo), f.Epochs[0])
	if err != nil || authored == "" {
		t.Fatalf("fixture: the authoring moment must resolve to a revision: %v (%q)", err, authored)
	}
	if !strings.Contains(clause, masterClaimShortSha(authored)) {
		t.Errorf("the clause must name the revision the authoring moment resolves to (%s); clause:\n%s",
			masterClaimShortSha(authored), clause)
	}
	if tip := masterClaimShortSha(f.Tip); tip == masterClaimShortSha(authored) {
		t.Fatalf("fixture: the authored revision and the tip must differ, or this arm proves nothing")
	}
}

// TestBriefingGateAssert_ARowAuthoredAtTheTipIsThePositiveControl is leg ②: the
// SAME briefing, authored AT the integration tip, must stamp NOTHING and say why.
func TestBriefingGateAssert_ARowAuthoredAtTheTipIsThePositiveControl(t *testing.T) {
	f := gateAssertBuildRepo(t)
	tipEpoch := f.Epochs[len(f.Epochs)-1] + 1
	clause, record := TaskBriefingGateAssertRefresh(f.Repo, float64(tipEpoch), gateAssertBriefing(gateAssertFixtureSurfaceRun))
	if clause != "" {
		t.Fatalf("a row authored at the branch's own tip was stamped — a clause telling the child to "+
			"re-measure a genuinely due surface is noise the reader learns to ignore; clause:\n%s", clause)
	}
	if len(record) != 1 || !strings.Contains(record[0], "0 commit(s) behind") {
		t.Fatalf("the control measurement must still be RECORDED, with the measured zero distance; got %v", record)
	}
}

// TestBriefingGateAssert_AnUnreadableAssertionIsStatedNotSilent is leg ③. The
// fixture line is REAL repo prose: a briefing of this epic describing the shape
// in the abstract names a verdict and the branch but no surface at all.
func TestBriefingGateAssert_AnUnreadableAssertionIsStatedNotSilent(t *testing.T) {
	f := gateAssertBuildRepo(t)
	const body = "The third stamp: a briefing of the form \"names its measurement point as a " +
		"**branch/worktree** and asserts **\\\"<surface> is RED on master\\\"**.\"\n"
	clause, record := TaskBriefingGateAssertRefresh(f.Repo, float64(f.Epochs[0]), body)
	if clause != "" {
		t.Fatalf("nothing was measured (the line names no surface), so nothing may be stamped; clause:\n%s", clause)
	}
	if len(record) != 1 || !strings.Contains(record[0], "UNREADABLE") {
		t.Fatalf("the seam must STATE the assertion it could not read (Rule 15), never report the same "+
			"nothing as a briefing with no such assertion; got %v", record)
	}
}

// TestBriefingGateAssert_AnUnanchoredRowIsStatedNotPassed is leg ④, in both of
// its shapes: a ground that is not a repo, and a row whose authoring time was not
// read. Both must STAMP nothing and SAY so — an invented anchor would be a
// reading about a moment the author never wrote at.
func TestBriefingGateAssert_AnUnanchoredRowIsStatedNotPassed(t *testing.T) {
	f := gateAssertBuildRepo(t)
	body := gateAssertBriefing(gateAssertFixtureSurfaceRun)
	for name, tc := range map[string]struct {
		repo       string
		authoredAt float64
	}{
		"no repo":               {"", float64(f.Epochs[0])},
		"uncaptured created_at": {f.Repo, 0},
	} {
		clause, record := TaskBriefingGateAssertRefresh(tc.repo, tc.authoredAt, body)
		if clause != "" {
			t.Errorf("%s: nothing was measured, so nothing may be stamped; clause:\n%s", name, clause)
			continue
		}
		if len(record) != 1 || !strings.Contains(record[0], "UNMEASURED") {
			t.Errorf("%s: an unanchored assertion must be STATED (never a silent pass); got %v", name, record)
		}
	}
}

// TestBriefingGateAssert_AnOrdinaryBriefingIsUntouched is leg ⑤ — the property
// that keeps this seam from degrading every briefing it touches.
func TestBriefingGateAssert_AnOrdinaryBriefingIsUntouched(t *testing.T) {
	f := gateAssertBuildRepo(t)
	for name, body := range map[string]string{
		"no assertion":       "# Fix the read path\n\nRe-implement the parser and add a test for the boundary.\n",
		"red ink, no branch": "# Fix the parser\n\nThe build is red in a hundred places.\n",
		"a landed premise":   "# Report\n\nThe fix 9867ed461 never landed on master; re-implement it.\n",
	} {
		clause, record := TaskBriefingGateAssertRefresh(f.Repo, float64(f.Epochs[0]), body)
		if clause != "" || len(record) != 0 {
			t.Errorf("%s: the briefing makes no gate assertion, so the seam must be silent "+
				"(clause=%q record=%v)", name, clause, record)
		}
	}
}

// TestBriefingGateAssert_ASecondPassAppendsNothingAndSaysSo pins the idempotence
// contract the two sibling halves carry for the same reason: the clause QUOTES
// the briefing line it contradicts, so a plain re-read of a stamped briefing
// would re-fire on its own quote (a resume, a re-dispatch, an operator paste).
func TestBriefingGateAssert_ASecondPassAppendsNothingAndSaysSo(t *testing.T) {
	f := gateAssertBuildRepo(t)
	body := gateAssertBriefing(gateAssertFixtureSurfaceRun)
	clause, _ := TaskBriefingGateAssertRefresh(f.Repo, float64(f.Epochs[0]), body)
	if clause == "" {
		t.Fatal("fixture: the first pass must stamp")
	}
	again, record := TaskBriefingGateAssertRefresh(f.Repo, float64(f.Epochs[0]), body+"\n\n"+clause)
	if again != "" {
		t.Fatalf("a second pass over a stamped briefing must append nothing; got:\n%s", again)
	}
	if len(record) != 1 || !strings.Contains(record[0], "already carries") {
		t.Fatalf("the second pass must SAY why it appended nothing; got %v", record)
	}
}

// TestBriefingGateAssert_AnUnreadablyOldMomentIsStatedNotGuessed pins the arm
// where `git rev-list --before` finds nothing at all: an epoch BEFORE the
// repository's first commit. The reading is unavailable, and the seam says so
// rather than resolving to the first commit and calling that the authoring
// revision (which would silently understate every distance).
func TestBriefingGateAssert_AnUnreadablyOldMomentIsStatedNotGuessed(t *testing.T) {
	f := gateAssertBuildRepo(t)
	clause, record := TaskBriefingGateAssertRefresh(f.Repo, float64(gateAssertFixtureBaseEpoch-1000), gateAssertBriefing(gateAssertFixtureSurfaceRun))
	if clause != "" {
		t.Fatalf("no revision stood at that moment, so nothing may be stamped; clause:\n%s", clause)
	}
	if len(record) != 1 || !strings.Contains(record[0], "UNMEASURED") {
		t.Fatalf("an unresolvable anchor must be STATED, never guessed at; got %v", record)
	}
}

// TestBriefingGateAssert_IsBoundedByItsOwnGitTimeout keeps this file's git
// children inside the package's ONE ceiling (Rule 27: a test that can park the
// package's budget carries its own bound), asserted where the constant lives.
func TestBriefingGateAssert_IsBoundedByItsOwnGitTimeout(t *testing.T) {
	if premiseGitTimeout <= 0 || premiseGitTimeout > time.Minute {
		t.Fatalf("the fixture's git ceiling must be a small positive duration; got %v", premiseGitTimeout)
	}
}

// gateAssertFixtureBranchlessBriefing is row 01a1432e's OWN assertion, composed
// the way the seam receives it (summary header, then the sentence): the verdict
// is bound to a TREE STATE, not to the integration branch's name, and the
// sentence is in this repo's CJK word order. Measured on this tree before the
// branch-less arm existed: claims=0 / misses=0 — the row went out against a
// premise its own base tip had already fixed, which is the whole defect.
// gateAssertFixtureBranchlessAnchorWord is the anchor word row 01a1432e's own
// sentence uses, named once so the fixtures below cannot drift from it (Rule 1).
const gateAssertFixtureBranchlessAnchorWord = "pristine"

const gateAssertFixtureBranchlessBriefing = "[Task summary]\n" +
	"devdoc/removal-audit-shell-census.md 自相矛盾：文件顶 historical banner，DOC-INDEX.tsv 却归 normative，" +
	"TestDocClassBannerMatchesManifest 在 " + gateAssertFixtureBranchlessAnchorWord + " 树上即红\n"

// TestBriefingGateAssert_ABranchlessAssertionIsStampedWithItsOwnAnchor is the
// SPELLING-GAP leg: the same measurement, over the anchor the author actually
// used, with the clause naming THAT anchor instead of claiming the author wrote
// "on master".
func TestBriefingGateAssert_ABranchlessAssertionIsStampedWithItsOwnAnchor(t *testing.T) {
	f := gateAssertBuildRepo(t)
	clause, record := TaskBriefingGateAssertRefresh(f.Repo, float64(f.Epochs[0]), gateAssertFixtureBranchlessBriefing)
	if clause == "" {
		t.Fatalf("a branch-less assertion written %d commit(s) before the tip got no stamp; record: %v",
			gateAssertFixtureAhead, record)
	}
	for _, want := range []string{
		TaskBriefingGateAssertLabel,
		TaskBriefingGateAssertReMeasure,
		"TestDocClassBannerMatchesManifest", // the surface the sentence names
		gateAssertionBranchlessAnchorMarker, // the anchor the AUTHOR used
		"an unmodified checkout of",         // and what that anchor means, stated
		"NOT evidence that the work is done",
	} {
		if !strings.Contains(clause, want) {
			t.Errorf("the clause must carry %q; clause:\n%s", want, clause)
		}
	}
	// The one thing it must NOT say: that the author asserted a red MASTER. The
	// sentence asserts a red pristine TREE, which is a narrower claim, and the
	// clause exists to tell the child which claim it is re-measuring.
	if strings.Contains(clause, "- \"TestDocClassBannerMatchesManifest\" is asserted RED on master") {
		t.Errorf("the clause claimed an anchor the author did not write; clause:\n%s", clause)
	}
}

// TestBriefingGateAssert_ABranchlessRowAuthoredAtTheTipIsStillThePositiveControl
// pins the arm that keeps the seam silent on a fresh row: the branch-less
// spelling must not become a licence to stamp everything.
func TestBriefingGateAssert_ABranchlessRowAuthoredAtTheTipIsStillThePositiveControl(t *testing.T) {
	f := gateAssertBuildRepo(t)
	tipEpoch := f.Epochs[len(f.Epochs)-1]
	clause, record := TaskBriefingGateAssertRefresh(f.Repo, float64(tipEpoch), gateAssertFixtureBranchlessBriefing)
	if clause != "" {
		t.Fatalf("a row authored AT the tip asserts the branch as it stands, so nothing may be stamped; clause:\n%s", clause)
	}
	if len(record) == 0 {
		t.Fatal("the positive control must be RECORDED, not silently skipped (Rule 15)")
	}
	// The record must quote the line it read, which is the only place the
	// author's anchor word survives — a reading with no anchor named would be
	// indistinguishable from the branch-anchored arm.
	if !strings.Contains(strings.Join(record, "\n"), gateAssertFixtureBranchlessAnchorWord) {
		t.Errorf("the record must quote the anchor the author used; record: %v", record)
	}
}
