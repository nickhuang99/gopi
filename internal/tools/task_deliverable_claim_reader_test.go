package tools

// Claim-time ground stamp → the merger's status READER, in ONE observation
// (epic branch↔task linkage, item [2/5], 2026-09-18).
//
// WHY THIS EXISTS BESIDE the predecessor's task_deliverable_claim_stamp_test.go
// (which pins the WRITER's blob contract): the epic's acceptance is not "the
// row carries the bytes", it is "the merger's status reader stops reporting
// the gap by label for a row the claim stamped". The predecessor was killed
// before that half was ever produced, and nothing in the repo asserted it — a
// writer that writes a blob no reader loads, or writes the pair under a
// spelling the reader does not read, would have passed every existing test.
//
// So this test drives the SAME path production does, end to end:
//
//	claim + start the row (in_progress) → taskWorktreeStampClaimer (the ONE
//	claim-time writer the wrapper calls) → BranchAttributionReport (the
//	production reader: real git worktree listing, real row loader) →
//	mergeCandidateLines + ownerClause (the status render's own sentence
//	builder) → assert the branch names the task id and the gap sentence is
//	NOT there.
//
// MEASURED LIVE, 2026-09-18 (same shape, real claim, real ledger copy):
// `gopi merger --status` rendered
//
//   - fix/claim-stamp-live-probe — NOT a candidate: a checkout holds it
//     (/home/nick/workspace/_claim-stamp-live/stamp-wt) and a live process IS
//     working in that checkout (pid …) — task 01a0b324-38cf-7bea-baa8-38fa623869db
//
// while the sibling control branch (a worktree nobody claimed) still printed
// the gap sentence. This test is that observation, minus the live child.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// TestClaimStampClosesTheStatusGapOnARealClaimedRow is the epic's acceptance:
// a claimed row's stamped ground reaches the operator render as an ATTRIBUTION
// (branch → task id), not as the gap sentence.
func TestClaimStampClosesTheStatusGapOnARealClaimedRow(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	repo, wt := branchBackfillFixture(t)
	const branch = "feat/backfill-e2e"

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "claim stamp → status reader", Repo: repo})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := adoptionTaskID(t, out)

	// The reader BEFORE the claim: the fixture's branch is registered in a
	// worktree and no row names it — this is the gap the epic is about, and
	// asserting it here is what makes the after-state meaningful (a test that
	// only asserted the after-state could pass on a reader that never printed
	// the gap at all).
	beforeLine := claimStampStatusLine(t, repo, branch)
	if beforeLine == "" {
		t.Fatalf("the reader must answer for %s", branch)
	}
	if !strings.Contains(beforeLine, candidateNoRowNote) {
		t.Fatalf("BEFORE the claim the render must state the gap for an unclaimed checkout, got:\n%s", beforeLine)
	}

	// The claim, exactly as the wrapper performs it (claim → start → stamp).
	if err := TaskClaimExplicit(taskID, "claimstamp:reader"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := TaskStart(taskID, "claimstamp:reader"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, _, written := taskWorktreeStampClaimer(taskID, repo, wt); !written {
		t.Fatalf("the claim-time stamp must record the ground (repo=%s worktree=%s)", repo, wt)
	}
	if got := claimGroundRowStatus(t, taskID); got != TaskStatusInProgress {
		t.Fatalf("the row must still be IN FLIGHT when the reader is driven, got %q", got)
	}

	// AFTER: the same reader, the same branch, one observation later.
	afterLine := claimStampStatusLine(t, repo, branch)
	if strings.Contains(afterLine, candidateNoRowNote) {
		t.Fatalf("the claim stamp did NOT close the reader's gap — the render still says nobody names this branch:\n%s", afterLine)
	}
	if !strings.Contains(afterLine, "task "+taskID) {
		t.Fatalf("the render must name the owning task id %s, got:\n%s", taskID, afterLine)
	}
	if !strings.Contains(afterLine, wt) {
		t.Fatalf("the render must name the checkout the row records (%s), got:\n%s", wt, afterLine)
	}
}

// claimStampStatusLine renders the merger status candidate line for ONE branch
// through the PRODUCTION reader and the PRODUCTION sentence builder — the same
// two functions merger_status.go / the repo sweep call. It never re-derives a
// verdict: BranchAttributionReport loads the rows through the real loader
// (branchAttributionRows) and measures the checkout through real git, and
// ownerClause is the render's own gap/attribution sentence.
func claimStampStatusLine(t *testing.T, repo, branch string) string {
	t.Helper()
	decisions, err := BranchAttributionReport(repo)
	if err != nil {
		t.Fatalf("BranchAttributionReport(%s): %v", repo, err)
	}
	for _, d := range decisions {
		if d.Branch != branch {
			continue
		}
		line := BranchAttributionLine{
			Branch: d.Branch, Verdict: string(d.Verdict), Task: d.Task,
			Checkout: d.Checkout, Worktree: d.Worktree, Held: d.Held,
			Inside: d.HeldNames, ProbeMeasured: true,
			RunnerKnown: d.RunnerKnown, RunnerLive: d.RunnerLive,
			Age: d.Age, AgeKnown: d.AgeKnown,
		}
		cands := mergeCandidateLines([]BranchAttributionLine{line})
		if len(cands) != 1 {
			t.Fatalf("expected one candidate line for %s, got %d", branch, len(cands))
		}
		return cands[0].Note
	}
	return ""
}

// TestClaimStampRecordsTheClaimMomentNotEmpty_Creation pins the WRITE-ORDER
// attribution fixed on 2026-09-18: the claim-time write used to be logged as
// "at creation", which claimed that the ground was learned when the executor
// created it. MEASURED live, the claim write lands BEFORE the child exists
// (the wrapper logs `executor cwd →` after it), so the line was misreporting
// the moment it records — the writer's own evidence about the ground, wrong by
// one seam. The two moments are now distinct spellings, and this test asserts
// the claim seam reports the CLAIM moment (a regression to the single
// "creation" wording reddens it).
func TestClaimStampRecordsTheClaimMomentNotEmpty_Creation(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	repo, wt := branchBackfillFixture(t)

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "claim stamp moment", Repo: repo})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := adoptionTaskID(t, out)

	var lines []string
	restore := taskWorktreeStampLogStub(t, func(format string, args ...any) {
		lines = append(lines, format)
	})
	_, _, written := taskWorktreeStampClaimer(taskID, repo, wt)
	restore()
	if !written {
		t.Fatalf("claim-time stamp did not write")
	}
	if len(lines) != 1 {
		t.Fatalf("expected exactly ONE outcome line, got %d: %v", len(lines), lines)
	}
	if lines[0] != taskWorktreeStampLineFmt {
		t.Fatalf("the outcome line must be the ONE shared format, got %q", lines[0])
	}
	if got := taskWorktreeStampMomentArg(taskWorktreeStampMomentClaim); got != "claim" {
		t.Fatalf("the claim moment must render as %q, got %q", "claim", got)
	}
	if got := taskWorktreeStampMomentArg(taskWorktreeStampMomentCreation); got != "creation" {
		t.Fatalf("the creation moment must render as %q, got %q", "creation", got)
	}
	// And the CREATION seam still says creation: the shared writer keeps its
	// two readers distinguishable (Rule 14: one writer, one spelling per fact).
	second := claimStampSecondRow(t, repo)
	var created []string
	restore = taskWorktreeStampLogStub(t, func(format string, args ...any) {
		created = append(created, fmt.Sprintf(format, args...))
	})
	if err := taskBackfillDeliverableWorktree(second, wt, "feat/backfill-e2e"); err != nil {
		t.Fatalf("creation-path write: %v", err)
	}
	restore()
	if len(created) != 1 {
		t.Fatalf("the creation seam must report its own write once, got %d: %v", len(created), created)
	}
	if !strings.HasSuffix(created[0], " at creation") {
		t.Fatalf("the creation seam must record the CREATION moment, got %q", created[0])
	}
	if taskWorktreeStampMomentArg(taskWorktreeStampMomentCreation) == taskWorktreeStampMomentArg(taskWorktreeStampMomentClaim) {
		t.Fatalf("the two moments must not render identically — the whole point is telling them apart")
	}
}

// taskWorktreeStampLogStub rebinds the outcome-line sink for one arm and
// returns the restore func (serial by construction: the sink is package state).
func taskWorktreeStampLogStub(t *testing.T, fn func(string, ...any)) func() {
	t.Helper()
	prev := taskWorktreeStampLog
	taskWorktreeStampLog = fn
	return func() { taskWorktreeStampLog = prev }
}

// claimStampSecondRow submits (and returns) another row in the SAME fixture
// repo — repo-only (it names no worktree yet), so the creation seam's write
// path is reachable for it.
func claimStampSecondRow(t *testing.T, repo string) string {
	t.Helper()
	out, err := TaskSubmit(TaskSubmitOptions{Summary: "claim stamp creation seam", Repo: repo})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	return adoptionTaskID(t, out)
}
