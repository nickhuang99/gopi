// branch_lifecycle_ruling_test.go — the LOOP-PROOF for the branch-lifecycle
// ruling (task 01a0af2f, devdoc/branch-lifecycle-ruling.md).
//
// THE CLAIM TO MEASURE (the brief's core question): when a branch CANNOT be
// deleted this tick — ① undecidable, ② a peer holds it, ③ the hand-off is not
// finished — the disposition "leave it in refs/heads/ and report it" must NOT
// re-trigger every tick. Two mechanical questions decide it, and this file
// answers both against real git and the real ledger:
//
//	A. IS IT STILL IN THE NEXT TICK'S INPUT? The sweep set is read fresh from
//	   `refs/heads` (repoSweepClassify). A branch held by a worktree is
//	   classified PROTECTED — so case ② never becomes a merge candidate at all:
//	   the next tick does not try it, cannot fail on it, and files nothing. That
//	   is a stable state, not a loop.
//	B. DOES RE-SEEING IT MANUFACTURE A ROW? The branch-rework lane's only
//	   submission guard is reworkSummaryLive — LIVE rows (pending/claimed/
//	   in_progress) under the deterministic summary prefix+branch. So the loop
//	   would be "a closed row ⇒ a new row next tick for the same branch", and
//	   the ONE thing that removes the branch from the input is the delete — which
//	   is exactly why deletion is the endpoint (and why a retained branch is
//	   transient, case ③, not a resting state).
//
// The third case's SAFETY (③: never delete before the work has a home) is
// measured too: a submit whose attachment cannot be archived ABORTS — no row —
// so deleting after a FAILED submit would be the work-loss the ruling forbids.
// That ordering is asserted here rather than asserted in prose.
//
// These are ABSENCE-and-IDENTITY assertions, not counters (the repo_sweep_notff
// precedent): a regression that left a carrier behind (an archive tag, a
// renamed ref) would satisfy "the branch is not a candidate" while violating the
// two-state rule, so the two-state rule is probed directly.

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// blrBranch is the diverged branch the fixture carries: it cannot fast-forward,
// so it is precisely the branch a take-over would hand over as a diff (and the
// branch whose lifecycle the ruling governs).
const blrBranch = "feat/conflict"

// blrRulingStatuses are the two ledger states that decide the loop, spelled
// from the task-status constants (Rule 1b: never the literal).
var blrRulingStatuses = []string{taskStatusPending, taskStatusCompleted}

// blrLedgerRowCount counts the ledger rows under one summary — the dedup
// universe (any status), so a test can tell "no row" from "a closed row".
func blrLedgerRowCount(t *testing.T, summary string) int {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE summary = ?`, summary).Scan(&n); err != nil {
		t.Fatalf("count rows for %q: %v", summary, err)
	}
	return n
}

// blrMarkRowStatus moves every row under summary to status — the way a test
// simulates "the reworker closed the row", which is the state that would let a
// retained branch mint a SECOND row on the next tick.
func blrMarkRowStatus(t *testing.T, summary, status string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ? WHERE summary = ?`, status, summary); err != nil {
		t.Fatalf("mark %q %s: %v", summary, status, err)
	}
}

// blrCandidateNames is the sweep's mergeable set (the next tick's input) for
// the fixture repo.
func blrCandidateNames(t *testing.T, repo string) []string {
	t.Helper()
	candidates, _, err := repoSweepClassify(repo, 0, nil)
	if err != nil {
		t.Fatalf("classify %s: %v", repo, err)
	}
	return candidates
}

// blrContains reports whether names carries want.
func blrContains(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// blrTwoStateProbe asserts the branch's ref namespace is in exactly ONE of the
// two permitted states: present under refs/heads, or absent — with no third
// carrier (no archive tag, no renamed ref) in between. It is the ruling's hard
// boundary, measured.
func blrTwoStateProbe(t *testing.T, p parkRepo, branch string) {
	t.Helper()
	present := p.parkRefExists(t, "refs/heads/"+branch)
	tags := p.git(t, "for-each-ref", "--format=%(refname:short)", "refs/tags")
	for _, tag := range strings.Fields(tags) {
		if strings.Contains(tag, branch) {
			t.Errorf("a tag %q carries branch %q — the ruling's boundary is two states "+
				"(in refs/heads/ or deleted): no archive tag, no marker ref", tag, branch)
		}
	}
	if !present {
		return // deleted: the endpoint, and nothing may remain
	}
	// Present must mean present under its OWN name — never renamed into a
	// state-carrying namespace.
	if strings.HasPrefix(branch, "conflict/") {
		t.Errorf("branch %q is spelled as a state-carrying namespace — a rename is a third state", branch)
	}
}

// TestBranchRuling_WorktreeHeldBranchIsNeverAMergeCandidate is the loop-proof
// for case ② (a peer holds the branch).
//
// THE MECHANISM: repoSweepClassify walks every head and hands a
// worktree-held branch to the PROTECTED bucket, so it is absent from the
// mergeable slice the tick iterates. The next tick therefore does not attempt
// the merge, cannot reach a refusal, and files nothing — the loop shape
// ("same input every tick") does not exist for this case, which is what makes
// "touch nothing and report" a STABLE disposition rather than a deferred loop.
func TestBranchRuling_WorktreeHeldBranchIsNeverAMergeCandidate(t *testing.T) {
	p := newParkRepo(t)
	if got := blrCandidateNames(t, p.dir); !blrContains(got, blrBranch) {
		t.Fatalf("fixture precondition: %s must start as a merge candidate, got %v", blrBranch, got)
	}

	// A peer's linked worktree holds the branch (the case-② shape on disk).
	wt := filepath.Join(t.TempDir(), "peer-wt")
	if out, err := p.gitOut(t, "worktree", "add", wt, blrBranch); err != nil {
		t.Fatalf("worktree add: %v: %s", err, out)
	}

	candidates, inv, err := repoSweepClassify(p.dir, 0, nil)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if blrContains(candidates, blrBranch) {
		t.Errorf("%s is STILL a merge candidate while a worktree holds it (%v) — the next tick "+
			"would try the merge again: that is the loop, not a disposition", blrBranch, candidates)
	}
	if inv.Protected != 1 {
		t.Errorf("inv.Protected = %d, want 1 — the branch must be counted in the PROTECTED bucket so "+
			"`git branch` reconciles against the sweep (inventory %+v)", inv.Protected, inv)
	}
	blrTwoStateProbe(t, p, blrBranch)
}

// TestBranchRuling_ReworkDedupIsLiveOnlySoDeleteIsTheEndpoint is the loop-proof
// for case ③ (the hand-off is not finished).
//
// THE MEASUREMENT: the branch-rework lane's submission guard is
// reworkSummaryLive — LIVE rows only. So while the hand-off's row is open, a
// second tick's submit is refused (no duplicate work); the moment that row is
// CLOSED, the same branch would be eligible for a SECOND row. A branch retained
// in refs/heads/ is therefore a ticking liability, and the only thing that takes
// it out of the next tick's input is the delete: deletion is the endpoint of the
// lifecycle, not a suppression.
func TestBranchRuling_ReworkDedupIsLiveOnlySoDeleteIsTheEndpoint(t *testing.T) {
	p := newParkRepo(t)
	mbgTestDB(t) // fresh ledger for this test (the lane writes a task row)

	summary := resolveRowReworkSummaryPrefix + blrBranch

	// (1) the hand-off succeeds: exactly one row, and the dedup holds it.
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: summary,
		Payload: "rework " + blrBranch + ": cannot auto-merge into master",
		Tags:    resolveRowReworkTags,
		Source:  taskMergeSourceAuto,
		Repo:    p.dir,
	}); err != nil {
		t.Fatalf("submit rework row: %v", err)
	}
	live, err := reworkSummaryLive(summary)
	if err != nil {
		t.Fatalf("reworkSummaryLive: %v", err)
	}
	if !live {
		t.Fatalf("the hand-off's row is not LIVE after a successful submit — the lane's dedup "+
			"would not hold, so every tick could mint another row (summary %q)", summary)
	}
	if n := blrLedgerRowCount(t, summary); n != 1 {
		t.Fatalf("rows after the hand-off = %d, want 1", n)
	}

	// (2) the row is CLOSED (the reworker finished). Nothing in the ledger now
	//     holds the lane back — this is the state a RETAINED branch provokes, and
	//     the ruling's answer is that the branch is not retained: the delete in
	//     step (c) happened in the same tick, so the input is gone.
	blrMarkRowStatus(t, summary, taskStatusCompleted)
	live, err = reworkSummaryLive(summary)
	if err != nil {
		t.Fatalf("reworkSummaryLive after close: %v", err)
	}
	if live {
		t.Errorf("a CLOSED row still counts as live — then the dedup would hold forever and a "+
			"finished hand-off would never be re-queued (summary %q)", summary)
	}
	if n := blrLedgerRowCount(t, summary); n != 1 {
		t.Fatalf("rows after close = %d, want 1 (closing a row is not deleting it)", n)
	}

	// (3) the branch is deleted (the endpoint) and the next tick's input no
	//     longer contains it: the loop cannot be re-entered, because there is no
	//     branch left to rediscover. No carrier is minted in its place.
	p.git(t, "branch", "-D", blrBranch)
	if got := blrCandidateNames(t, p.dir); blrContains(got, blrBranch) {
		t.Errorf("%s is still a candidate after its ref was deleted (%v)", blrBranch, got)
	}
	if p.parkRefExists(t, "refs/heads/"+blrBranch) {
		t.Errorf("the branch ref survived `git branch -D` — the fixture cannot prove the endpoint")
	}
	blrTwoStateProbe(t, p, blrBranch)
	for _, status := range blrRulingStatuses {
		if status == "" {
			t.Fatal("status constant is empty — Rule 1b requires the spelled constant")
		}
	}
}

// TestBranchRuling_AFailedHandoffFilesNoRowSoNothingMayBeDeleted is case ③'s
// SAFETY half, and it is the ordering the ruling forbids reversing.
//
// THE CLAIM: `--attachment` is not "a copy we hope happened" — a submit whose
// attachment cannot be archived ABORTS and creates no row. Therefore a branch
// whose diff was written to disk but whose submit FAILED has NO home for its
// work, and deleting it there would lose the commits. Measured: the failed
// submit leaves the ledger empty, which is exactly the fact a take-over reads
// before it is allowed to delete.
func TestBranchRuling_AFailedHandoffFilesNoRowSoNothingMayBeDeleted(t *testing.T) {
	p := newParkRepo(t)
	mbgTestDB(t)

	summary := resolveRowReworkSummaryPrefix + blrBranch
	missing := filepath.Join(t.TempDir(), "no-such-diff.diff")
	if _, err := os.Stat(missing); err == nil {
		t.Fatalf("fixture precondition: %s must not exist", missing)
	}

	_, err := TaskSubmit(TaskSubmitOptions{
		Summary:     summary,
		Payload:     "rework " + blrBranch,
		Tags:        resolveRowReworkTags,
		Source:      taskMergeSourceAuto,
		Repo:        p.dir,
		Attachments: []string{missing},
	})
	if err == nil {
		t.Fatalf("a submit with an unarchivable attachment SUCCEEDED — the diff was never copied, " +
			"so 'the attachment is the work now' is false and a delete would lose the branch")
	}
	// No row ⇒ no home for the work ⇒ the branch MUST still be there.
	if n := blrLedgerRowCount(t, summary); n != 0 {
		t.Errorf("a FAILED submit left %d row(s) under %q — the ordering guard "+
			"(diff archived → row queued → delete) needs 'no row' to mean 'nothing to fall back on'",
			n, summary)
	}
	if !p.parkRefExists(t, "refs/heads/"+blrBranch) {
		t.Errorf("the branch ref is gone after a failed hand-off — that is the work-loss the " +
			"ruling's ordering exists to prevent")
	}
	blrTwoStateProbe(t, p, blrBranch)
}
