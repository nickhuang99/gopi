package tools

// The CANDIDATE SET of the dead-holder reclaim scan — the epic "dead-holder 回收扫描
// 的候选集合".
//
// THE DEFECT. r.LiveRow is FALSE for exactly the rows whose status is terminal
// (pending/claimed/in_progress set it TRUE — reclaimDeadLocalHolderRowsFrom,
// finishCleanupDeadLocalHolder), so a COMPLETED row whose recorded directory is
// gone — the ordinary end state of every finished run — reached reclaimRefuse and
// printed
//
//	dead-holder reclaim <branch> (<id>) skipped: repo directory /x is gone — this
//	row cannot be reconciled here
//
// on every tick, forever (the ledger is append-only; nothing deletes the row and
// the arm's own contract is that the row is not its to remove). A finished row is
// not a defect to reconcile, so the sentence was pure noise sitting beside the
// refusals an operator MUST read.
//
// THE JUDGE IS THE CANDIDATE SET, NOT A "report it only once" MARKER. The
// rejected earlier attempt (718f1f6d5) silenced the line AFTER printing it once
// and marking the row's trail; the operator refused that criterion because the
// row was still being treated as an anomaly. What this file pins instead is that
// such a row never enters the report set at all: silence from the FIRST pass, no
// trail marker, no aggregate count, nothing (reclaimTerminalRowUnreachable).
//
// THE FOUR BOUNDARIES, each measured through the REAL pass
// (ReclaimDeadHolderWorktrees) and the REAL ledger, never a predicate in
// isolation:
//
//	① terminal row + ground GONE            → SILENT (nothing in the report set)
//	② live-status row + ground GONE          → REPORTED (Rule 15: the state that
//	                                          strands a row must stay visible)
//	③ terminal row + ground EXISTS and clean → FREED silently (no warning line)
//	④ ANY status + the ground is ANOTHER repo's worktree → SILENT, directory kept
//	                                          (Rule 42: never delete what is not
//	                                          this row's to delete)

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reclaimCandidateRowAllowsNothing asserts the strongest form of "this row is not
// in the report": the pass says nothing about it, returns no line for it, and
// writes no marker on its trail.
func reclaimCandidateRowAllowsNothing(t *testing.T, taskID, stderr string, lines []string) {
	t.Helper()
	if strings.Contains(stderr, taskID) {
		t.Errorf("%s must not appear in the pass's report at all (a terminal row whose ground is gone is the "+
			"ordinary end state of a finished run — not an anomaly to disclose; a line about it is exactly the "+
			"noise this epic removes):\n%s", taskID, stderr)
	}
	for _, line := range lines {
		if strings.Contains(line, taskID) {
			t.Errorf("%s must not be named in the returned repair list (a refusal is not a repair): %q",
				taskID, line)
		}
	}
	if markers := reclaimMarkerEvents(t, taskID); len(markers) != 0 {
		t.Errorf("%s must carry NO %s marker: nothing was disclosed, so nothing needed recording: %v",
			taskID, reclaimUnreachableMarker, markers)
	}
}

// TestReclaimCandidateSetTerminalRowWithGoneRepoIsSilent is boundary ① at the
// smallest sample: one COMPLETED row whose recorded repo does not exist on this
// host. Its holder is provably dead, so it really is a candidate — the ground is
// simply not here any more.
func TestReclaimCandidateSetTerminalRowWithGoneRepoIsSilent(t *testing.T) {
	if _, err := os.Stat("/proc"); err != nil {
		t.Skip("/proc is unavailable — the dead-holder shape cannot be modelled")
	}
	freshReclaimRefusalMemo(t)
	root := fossilLedgerEnv(t)
	const id = "t-cand-terminal-gone"
	repo := filepath.Join(root, "other-repo")
	wt := filepath.Join(root, "wt")
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "candidate set", Payload: "p", TaskID: id, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, id, taskStatusCompleted, repo, "feat/cand-gone", wt, hostPid(t, deadPID(t)))
	if _, err := os.Stat(repo); !os.IsNotExist(err) {
		t.Fatalf("precondition: the decoy repo %s must not exist (err=%v)", repo, err)
	}

	var lines []string
	stderr := captureStderrDuring(t, func() { lines = ReclaimDeadHolderWorktrees(true) })

	// The row is a genuine CANDIDATE (the scan found it), and it left the report
	// set before any wording was chosen. Both halves are asserted: silence that
	// came from the scan never seeing the row would prove nothing.
	rows, err := reclaimDeadHolderRows()
	if err != nil {
		t.Fatalf("ledger scan: %v", err)
	}
	seen := false
	for _, r := range rows {
		if r.TaskID == id {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("precondition: %s must be a candidate of the scan (otherwise the silence below measures "+
			"the wrong thing)", id)
	}
	reclaimCandidateRowAllowsNothing(t, id, stderr, lines)

	// A SECOND pass is silent too — not because of a memo or a marker, but
	// because the row was never in the set.
	freshReclaimRefusalMemo(t)
	stderr2 := captureStderrDuring(t, func() { lines = ReclaimDeadHolderWorktrees(true) })
	reclaimCandidateRowAllowsNothing(t, id, stderr2, lines)
}

// TestReclaimCandidateSetTerminalRowWithPrunedRegistrationIsSilent is boundary ①
// at the shape a FIXED ledger is actually in: the real repo still exists, the
// row's directory is gone, and git already dropped the registration (the "nothing
// left to converge" state — no repair is due, so nothing is reported).
func TestReclaimCandidateSetTerminalRowWithPrunedRegistrationIsSilent(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	const id = "t-cand-terminal-pruned"
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "candidate set", Payload: "p", TaskID: id, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, id, taskStatusCompleted, repo, branch, wt, hostPid(t, deadPID(t)))

	// Remove the directory THROUGH git (so the registration goes with it), which
	// is the ordinary end state of a released ground.
	gitRun(t, repo, finishCleanupWorktreeVerb, finishCleanupWorktreeRemoveVerb, "--force", wt)
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("precondition: the directory must be gone (err=%v)", err)
	}
	if worktreeListed(t, repo, wt) {
		t.Fatalf("precondition: the registration must already be dropped: %s", worktreeList(t, repo))
	}

	freshReclaimRefusalMemo(t)
	var lines []string
	stderr := captureStderrDuring(t, func() { lines = ReclaimDeadHolderWorktrees(true) })
	reclaimCandidateRowAllowsNothing(t, id, stderr, lines)
	if err := gitBranchExists(t, repo, branch); err != nil {
		t.Errorf("the branch is the deliverable and must survive: %v", err)
	}
}

// TestReclaimCandidateSetLiveRowWithGoneRepoIsReported is boundary ②: the SAME
// gone ground on a row whose life is NOT over. This is the state that strands a
// row (not finished, not resumable, not re-dispatchable), so it must be named —
// the fix removes noise about finished rows, never the disclosure of a live one.
func TestReclaimCandidateSetLiveRowWithGoneRepoIsReported(t *testing.T) {
	if _, err := os.Stat("/proc"); err != nil {
		t.Skip("/proc is unavailable — the dead-holder shape cannot be modelled")
	}
	freshReclaimRefusalMemo(t)
	root := fossilLedgerEnv(t)
	const id = "t-cand-live-gone"
	repo := filepath.Join(root, "other-repo")
	wt := filepath.Join(root, "wt")
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "candidate set", Payload: "p", TaskID: id, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, id, taskStatusInProgress, repo, "feat/cand-live", wt, hostPid(t, deadPID(t)))

	stderr := captureStderrDuring(t, func() { ReclaimDeadHolderWorktrees(true) })
	if !strings.Contains(stderr, id) {
		t.Fatalf("a LIVE-status row whose ground vanished must be REPORTED — nothing else will ever close "+
			"the row out (Rule 15: the disclosure is never traded for quiet):\n%s", stderr)
	}
	if !strings.Contains(stderr, repo) || !strings.Contains(stderr, reclaimUnreachableRepoGoneToken) {
		t.Fatalf("the report must name the ground and the reason, got:\n%s", stderr)
	}
}

// TestReclaimCandidateSetTerminalRowWithExistingGroundIsFreedSilently is boundary
// ③: the row whose life is over still owns a REAL, clean directory. That is this
// arm's job — free it — and the freeing is not a warning: the returned repair list
// names it, the operator sink says nothing, and no refusal marker lands.
func TestReclaimCandidateSetTerminalRowWithExistingGroundIsFreedSilently(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	const id = "t-cand-terminal-live-ground"
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "candidate set", Payload: "p", TaskID: id, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, id, taskStatusCompleted, repo, branch, wt, hostPid(t, deadPID(t)))
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("fixture: the ground must exist before the pass: %v", err)
	}

	freshReclaimRefusalMemo(t)
	var lines []string
	stderr := captureStderrDuring(t, func() { lines = ReclaimDeadHolderWorktrees(true) })

	// ① the ground IS freed, and the report says so with the removal marker.
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("a terminal row with an existing clean ground must be FREED (stat err=%v); pass lines:\n%s",
			err, strings.Join(lines, "\n"))
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, id) || !strings.Contains(joined, reclaimRemovedMarker) {
		t.Fatalf("the free must be reported with %q and name the row:\n%s", reclaimRemovedMarker, joined)
	}
	// ② SILENT on the operator sink: a successful reclaim is not a warning.
	if strings.Contains(stderr, id) || strings.Contains(stderr, "dead-holder reclaim") {
		t.Errorf("a successful reclaim must not warn — the operator sink is for refusals and failures:\n%s", stderr)
	}
	if markers := reclaimMarkerEvents(t, id); len(markers) != 0 {
		t.Errorf("%s was freed, not refused — it must carry no %s marker: %v", id, reclaimUnreachableMarker, markers)
	}
	// ③ the branch survives: this arm never mutates a ref.
	if err := gitBranchExists(t, repo, branch); err != nil {
		t.Errorf("the branch is the deliverable and must survive: %v", err)
	}
}

// TestReclaimCandidateSetForeignGroundIsSilentForAnyStatus is boundary ④, in both
// statuses: a row whose recorded path is a linked worktree of ANOTHER repo is
// never this arm's to touch (Rule 42) and — whatever the row's status — says
// nothing. The directory must be measurably untouched afterwards, and the row's
// status must not move: silence is not permission.
func TestReclaimCandidateSetForeignGroundIsSilentForAnyStatus(t *testing.T) {
	for _, status := range []string{taskStatusCompleted, taskStatusInProgress} {
		t.Run(status, func(t *testing.T) {
			repo, _, _ := deadHolderReclaimFixture(t)
			// A SECOND repo with its own linked worktree: the path the row names.
			other := filepath.Join(filepath.Dir(repo), "other-"+status)
			gitRun(t, filepath.Dir(repo), "init", other)
			gitRun(t, other, "config", "user.email", "t@t")
			gitRun(t, other, "config", "user.name", "t")
			gitRun(t, other, "checkout", "-b", "master")
			if err := os.WriteFile(filepath.Join(other, "b.txt"), []byte("other"), 0o644); err != nil {
				t.Fatal(err)
			}
			gitRun(t, other, "add", "b.txt")
			gitRun(t, other, "commit", "-m", "init")
			foreignWT := filepath.Join(filepath.Dir(repo), "foreign-wt-"+status)
			gitRun(t, other, finishCleanupWorktreeVerb, "add", "-b", "feat/foreign", foreignWT)

			id := "t-cand-foreign-" + status
			if _, err := TaskSubmit(TaskSubmitOptions{Summary: "candidate set", Payload: "p", TaskID: id, Source: "human"}); err != nil {
				t.Fatalf("submit: %v", err)
			}
			// The row claims repo (which exists) but the OTHER repo's directory.
			seedDeadHolderRow(t, id, status, repo, "feat/foreign", foreignWT, hostPid(t, deadPID(t)))

			before := reclaimRowStatus(t, id)
			freshReclaimRefusalMemo(t)
			var lines []string
			stderr := captureStderrDuring(t, func() { lines = ReclaimDeadHolderWorktrees(true) })
			reclaimCandidateRowAllowsNothing(t, id, stderr, lines)

			// RULE 42: the directory is not this row's to delete, so it is still
			// there and still a worktree of the other repo.
			if _, err := os.Stat(foreignWT); err != nil {
				t.Fatalf("a directory belonging to ANOTHER repo must never be removed (Rule 42): %v", err)
			}
			if !worktreeListed(t, other, foreignWT) {
				t.Errorf("the other repo's registration must be untouched: %s", worktreeList(t, other))
			}
			// Silence is not permission: nothing about the row moved either.
			if after := reclaimRowStatus(t, id); after != before {
				t.Errorf("%s: the pass must not change a row's status, %q → %q", id, before, after)
			}
		})
	}
}

// TestReclaimTerminalRowUnreachableCriterion is the classifier's own unit: the
// matrix it decides on, stated so a future edit cannot widen "silent" past the
// ground's identity (a live session on a terminal row, or a ground that exists and
// is merely busy, must stay reportable).
func TestReclaimTerminalRowUnreachableCriterion(t *testing.T) {
	terminal := reclaimDeadHolderRow{TaskID: "t", Status: taskStatusCompleted}
	live := reclaimDeadHolderRow{TaskID: "t", Status: taskStatusInProgress, LiveRow: true}

	if !reclaimTerminalRowUnreachable(terminal, reclaimStageGround) {
		t.Errorf("a terminal row whose GROUND is not here is the ordinary end state — it must be silent")
	}
	if !reclaimTerminalRowUnreachable(terminal, reclaimStageForeign) {
		t.Errorf("another repo's worktree is out of reach for EVERY status (Rule 42) — a terminal row's answer " +
			"and a live row's answer must agree here")
	}
	for _, stage := range []reclaimStage{reclaimStageRow, reclaimStageLive, reclaimStageOther} {
		if reclaimTerminalRowUnreachable(terminal, stage) {
			t.Errorf("stage %d must stay reportable even for a terminal row: a live session standing on it, or a "+
				"ground that exists and is merely busy, is what a human is being asked to read", stage)
		}
	}
	for _, stage := range []reclaimStage{reclaimStageGround, reclaimStageRow, reclaimStageLive, reclaimStageOther} {
		if reclaimTerminalRowUnreachable(live, stage) {
			t.Errorf("a LIVE-status row must never be silenced (stage %d): its ground vanishing is the one "+
				"state that strands the row", stage)
		}
	}
	if !reclaimTerminalRowUnreachable(live, reclaimStageForeign) {
		t.Errorf("another repo's worktree is silent for a live row too (Rule 42 / the epic's fourth boundary)")
	}
}
