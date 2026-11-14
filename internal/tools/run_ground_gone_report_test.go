package tools

// run_ground_gone_report_test.go — the REPORT leg's GONE rung (epic
// run-ground-gone-report).
//
// THE DEFECT THIS PINS, measured on this host 2026-10-27 by running the real
// surface:
//
//	$ ./bin/gopi task sweep-worktrees --dry-run
//	run-ground anchors: collectable=759 kept=236
//	# stat over the 759 collectable lines:
//	EXISTS=10 GONE=749
//
// 98.7% of the plan named directories that were not on disk. The REMOVAL leg had
// asked `os.Stat` all along (its GONE RUNG, run_ground_anchor.go), so an --apply
// would skip 749 of its 759 candidates at the very first rung — while the dry
// run, the axis' ONLY deliverable, could not say so. Report and act disagreed
// about the same directory, and the disagreement was invisible in both outputs.
//
// WHAT IS PINNED HERE, and both directions are needed:
//
//	① the anchored ground whose directory does NOT exist is never collectable —
//	  the pre-fix behaviour that made the dry run's figure false;
//	② it is REPORTED as `gone`, with the removal leg's own sentence
//	  (runGroundCollectGoneReason), so an operator sees where the candidate went
//	  instead of meeting a number that silently shrank;
//	③ the count splits it out (`gone=` beside `collectable=`), because a stale
//	  anchor folded into `kept` reads exactly like a healthy keep;
//	④ the removal leg still refuses it with the same sentence — the delete gates
//	  are untouched by this change, and the two legs render ONE finding (Rule 14);
//	⑤ POSITIVE CONTROL: an anchored ground whose directory DOES exist is not
//	  labelled gone, so the rung is measuring existence and not something else.
//
// Every case drives the REAL partitioner / reporter / collector. Nothing is
// stubbed: the whole point is that one of the two legs did not look at the disk.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// goneReportFixture is one sink holding two anchored grounds: one whose
// directory was never created, one that is really there.
type goneReportFixture struct {
	sink   string
	gone   string // anchored, directory DOES NOT exist
	exists string // anchored, directory DOES exist
}

// newGoneReportFixture stages the two anchors with a taker that is dead by
// measurement (both), aged past the age floor — the state the axis reaches after
// a run ends. The ONLY difference between the two rows is whether the directory
// is on disk, which is what makes case ⑤ a control rather than a second feature.
func newGoneReportFixture(t *testing.T) goneReportFixture {
	t.Helper()
	sink := anchorSink(t)
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	root := t.TempDir()
	const repo = "/fixture/repo"

	gone := filepath.Join(root, "ground-that-was-never-created")
	// Deliberately NOT created: the stale-anchor shape the axis meets 749 times.
	if _, err := os.Stat(gone); err == nil {
		t.Fatalf("fixture: %s must not exist for this test to mean anything", gone)
	}

	exists := filepath.Join(root, "ground-that-is-there")
	if err := os.MkdirAll(exists, 0o755); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	dead := deadPidOnThisHost(t)
	for _, tc := range []struct{ dir, branch, id string }{
		{gone, "task/01agonereport", "01agone-0000-7000-8000-000000000001"},
		{exists, "task/01aexistsreport", "01aexists-000-7000-8000-000000000002"},
	} {
		writeAnchorRecordForTest(t, sink, repo, tc.dir, tc.branch, tc.id, strconv.Itoa(dead), "999999999", strconv.Itoa(dead))
		anchorAgeForTest(t, sink, tc.dir, 48*time.Hour)
	}
	return goneReportFixture{sink: sink, gone: gone, exists: exists}
}

// TestRunGroundSweepReportSeparatesGoneGrounds is the minimal reproduction: an// anchor whose directory does not exist must not reach the collectable list, must
// be reported as gone with the removal leg's sentence, and must be counted
// apart — while an identical anchor whose directory exists stays unaffected.
func TestRunGroundSweepReportSeparatesGoneGrounds(t *testing.T) {
	f := newGoneReportFixture(t)

	collectable, kept, err := RunGroundCollectableGrounds(time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("RunGroundCollectableGrounds: %v", err)
	}
	lines := RunGroundSweepReport(collectable, kept)
	t.Logf("summary: %s", RunGroundSweepHeader(collectable, kept))
	t.Logf("read-out:\n  %s", strings.Join(lines, "\n  "))

	// ① THE REPRODUCTION: the gone ground is not a CANDIDATE. On the pre-fix tree
	// a dead taker with an expired session cleared every rung below the presence
	// question, so it was listed as `collectable` — that is the false figure the
	// dry run printed 759 times, 749 of them about directories that were gone.
	if groundCollectableIn(collectable, f.gone) {
		t.Fatalf("an anchor whose directory does not exist must never be a collectable candidate (this is the 749-of-759 defect); collectable=%v",
			groundPaths(collectable))
	}

	// ② it is REPORTED, as gone, with the removal leg's own sentence.
	var why string
	for _, v := range collectable {
		if sameWorkingPath(v.Anchor.Worktree, f.gone) {
			why = v.Reason
			if !v.Gone {
				t.Errorf("the anchor's ground does not exist, so the verdict must carry Gone=true; got %+v", v)
			}
		}
	}
	if !strings.Contains(why, runGroundCollectGoneReason) {
		t.Fatalf("the read-out must state the gone fact in the removal leg's own words (%q); got %q (collectable: %v)",
			runGroundCollectGoneReason, why, groundPaths(collectable))
	}
	var goneLines int
	for _, line := range lines {
		if !strings.Contains(line, f.gone) {
			continue
		}
		goneLines++
		if !strings.HasPrefix(line, runGroundSweepGoneLabel) {
			t.Errorf("the gone ground's line must carry the gone disposition; got %q", line)
		}
	}
	if goneLines != 1 {
		t.Errorf("the gone ground must appear on exactly one read-out line; got %d in %v", goneLines, lines)
	}

	// ③ the count splits it out — collectable counts only directories that EXIST.
	collectableN, goneN, keptN := RunGroundSweepCounts(collectable, kept)
	t.Logf("counts: collectable=%d gone=%d kept=%d (lists: %d/%d)", collectableN, goneN, keptN, len(collectable), len(kept))
	if goneN != 1 {
		t.Errorf("exactly one fixture ground is gone; the tally says gone=%d", goneN)
	}
	if collectableN+goneN != len(collectable) {
		t.Errorf("every candidate must be counted exactly once (existing or gone): collectable=%d gone=%d list=%d",
			collectableN, goneN, len(collectable))
	}
	if collectableN != len(collectable)-1 {
		t.Errorf("the pre-fix tally counted the gone anchor; the fixed one must not: collectable=%d list=%d", collectableN, len(collectable))
	}
	if keptN != len(kept) {
		t.Errorf("the kept figure must be the kept list: header=%d list=%d", keptN, len(kept))
	}
	if !strings.Contains(RunGroundSweepHeader(collectable, kept), "gone=1") {
		t.Errorf("the operator-facing summary must print the stale-anchor figure; got %q", RunGroundSweepHeader(collectable, kept))
	}

	// ⑤ POSITIVE CONTROL: the sibling anchor whose directory exists is NOT
	// labelled gone — otherwise the rung would be rejecting every anchor, and
	// case ① would pass for the wrong reason.
	for _, v := range collectable {
		if sameWorkingPath(v.Anchor.Worktree, f.exists) && v.Gone {
			t.Errorf("a ground that EXISTS must never be labelled gone")
		}
	}
	for _, v := range kept {
		if sameWorkingPath(v.Anchor.Worktree, f.exists) && v.Gone {
			t.Errorf("a ground that EXISTS must never be labelled gone (kept: %q)", v.Reason)
		}
	}
	// And it is still a real candidate on the axis (the presence rung must not
	// have swallowed the case it was not about).
	if !groundCollectableIn(collectable, f.exists) {
		t.Errorf("the existing ground must still be a collectable candidate; collectable=%v kept=%v",
			groundPaths(collectable), groundPaths(kept))
	}
}

// TestRunGroundCollectSameSentenceForTheSameGoneDirectory pins Rule 14 across the
// TWO legs: the report leg and the removal leg must answer the same directory
// with the same finding, from ONE measurement.
//
// The second half is the reason this test exists at all rather than trusting the
// first: the report is what the operator reads, the removal leg is what acts, and
// the original defect was exactly these two disagreeing. So the removal leg is
// driven here with a HAND-BUILT list (the shape it still accepts) and must render
// byte-identical wording — which also proves this change did not touch the delete
// gates: the list it was given still skips, it does not remove.
func TestRunGroundCollectSameSentenceForTheSameGoneDirectory(t *testing.T) {
	f := newGoneReportFixture(t)

	collectable, kept, err := RunGroundCollectableGrounds(time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("RunGroundCollectableGrounds: %v", err)
	}
	_ = kept
	var reported string
	for _, v := range collectable {
		if sameWorkingPath(v.Anchor.Worktree, f.gone) {
			reported = v.Reason
		}
	}
	if reported == "" {
		t.Fatalf("the gone ground must be reported; collectable=%v", groundPaths(collectable))
	}

	// The act leg, asked directly about the same directory.
	presence, why := runGroundMeasureGround(f.gone)
	if presence != runGroundGroundGone {
		t.Fatalf("the shared measurement must call a missing directory GONE; got presence=%v why=%q", presence, why)
	}

	// A hand-built candidate list (what a caller outside the scan can still
	// produce) must reach the same skip sentence — and must remove nothing.
	hand := []RunGroundLiveness{{Anchor: RunGroundAnchor{Worktree: f.gone, Repo: "/fixture/repo"}}}
	removed, skipped, cerr := RunGroundCollect(hand)
	if cerr != nil {
		t.Fatalf("RunGroundCollect: %v", cerr)
	}
	t.Logf("apply over the hand-built list: removed=%v skipped=%v", removed, skipped)
	if containsPath(removed, f.gone) {
		t.Fatalf("nothing can be removed from a directory that is not there; removed=%v", removed)
	}
	var skippedWhy string
	for _, s := range skipped {
		if strings.Contains(s, f.gone) {
			skippedWhy = s
		}
	}
	if !strings.Contains(skippedWhy, runGroundCollectGoneReason) {
		t.Fatalf("the removal leg must render the SAME gone sentence as the report leg; report=%q skip=%q", reported, skippedWhy)
	}
	if reported != runGroundCollectGoneReason {
		t.Errorf("the report's whole reason must BE the shared sentence (no second wording); got %q", reported)
	}
}

// groundCollectableIn reports whether dir is a COLLECTABLE CANDIDATE of a scan —
// the question the defect is about. It is deliberately not groundIn: the gone
// grounds travel in the same slice (so the removal leg's input is unchanged),
// and a helper that ignored the flag would answer "collectable" for exactly the
// anchors this fix separates.
func groundCollectableIn(list []RunGroundLiveness, dir string) bool {
	for _, v := range list {
		if sameWorkingPath(v.Anchor.Worktree, dir) && !v.Gone {
			return true
		}
	}
	return false
}
