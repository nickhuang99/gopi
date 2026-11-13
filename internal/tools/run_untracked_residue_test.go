package tools

// Arm 1's ENFORCEMENT half — regression tests that live on the FIXED tree (the
// zz_-prefixed red proof is the A/B artifact; see
// zz_redproof_run_untracked_residue_test.go for the baseline command).
//
// The contract under test, in the operator's terms:
//
//   - a run that closes out `completed` while its OWN recorded worktree still
//     carries untracked-only scratch gets a NAMED, ATTRIBUTED, non-silent
//     record on its own trail (the classifier's wording + the entries + the
//     worktree) — the run is told, and the next sweep has a handle;
//   - a TRACKED change keeps today's refusal unchanged (today = no run-residue
//     record at all; the residual is the sweep's reclaimSkipDirtyReason);
//   - the measurement is scoped to the row's OWN code worktree: the main
//     checkout and a detached scratch ground are never blamed on the run;
//   - nothing is deleted and no branch is touched (guard strength unchanged:
//     `--force` appears nowhere in this arm).
import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReclaimRunResidueCheckAttributesTheRunsOwnWorktree: the check resolves
// the row's recorded worktree, measures it, and produces ONE attributed
// sentence carrying the classifier's own wording.
func TestReclaimRunResidueCheckAttributesTheRunsOwnWorktree(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	if err := os.WriteFile(filepath.Join(wt, "build.out"), []byte("residue\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-attr", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-attr", repo, branch, wt)

	out, err := ReclaimRunResidueCheck("t-attr")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !out.Attributed || out.Blocked {
		t.Fatalf("want an ATTRIBUTED verdict for the run's own worktree, got %+v", out)
	}
	if out.Worktree != wt {
		t.Fatalf("worktree = %q, want the row's recorded %q", out.Worktree, wt)
	}
	for _, want := range []string{reclaimUntrackedResidueMarker, "t-attr", wt, "build.out"} {
		if !strings.Contains(out.Note, want) {
			t.Errorf("the refusal must name %q — got %q", want, out.Note)
		}
	}
	// Nothing is deleted by a measurement.
	if _, err := os.Stat(filepath.Join(wt, "build.out")); err != nil {
		t.Fatalf("the residue must survive the check (never auto-deleted): %v", err)
	}
}

// TestReclaimRunResidueCheckCleanAndTrackedAreSilent: a clean own worktree
// produces no note. The TRACKED case is asserted in its own test below: it is
// heard by this arm (it is the epic's worse shape — an uncommitted
// deliverable), so it is not "silent" any more.
func TestReclaimRunResidueCheckCleanAndTrackedAreSilent(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write string
	}{
		{"clean", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, wt, branch := deadHolderReclaimFixture(t)
			if tc.write != "" {
				if err := os.WriteFile(filepath.Join(wt, tc.write), []byte("edited\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-silent", Source: "human"}); err != nil {
				t.Fatal(err)
			}
			seedDeliverable(t, "t-silent", repo, branch, wt)
			out, err := ReclaimRunResidueCheck("t-silent")
			if err != nil {
				t.Fatalf("check: %v", err)
			}
			if out.Note != "" {
				t.Fatalf("%s: no run-residue note may be produced, got %q (porcelain=%q)", tc.name, out.Note, out.Porcelain)
			}
		})
	}
}

// TestReclaimRunResidueCheckNeverBlamesTheMainCheckout is the guard that keeps
// this arm honest: a run parked in the SHARED main checkout (the 2026-09-16
// leak shape — untracked verify reports landing in master) is NOT attributed,
// because a refusal naming the main checkout would be evidence nobody has.
// The row's deliverable here records the main checkout as its worktree, which
// git will not report as a linked worktree of that branch.
func TestReclaimRunResidueCheckNeverBlamesTheMainCheckout(t *testing.T) {
	repo, _, branch := deadHolderReclaimFixture(t)
	if err := os.WriteFile(filepath.Join(repo, "stray.out"), []byte("leak\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-main", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-main", repo, branch, repo) // worktree == MAIN checkout
	out, err := ReclaimRunResidueCheck("t-main")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if out.Note != "" || out.Attributed {
		t.Fatalf("the main checkout's untracked files are NOT this run's to answer for, got %+v", out)
	}
}

// TestReclaimRunResidueWritebackRecordsOnceAndNeverRollsBackTheOutcome: the
// writeback path files ONE attributed record with the entries quoted, leaves
// the terminal status as the authoritative fact (completed stands), and does
// not drip a second record when the same writeback is re-landed.
func TestReclaimRunResidueWritebackRecordsOnceAndNeverRollsBackTheOutcome(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	_ = repo
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-once", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-once", repo, branch, wt)
	if _, err := TaskStart("t-once", "worker-1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "build.out"), []byte("residue\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskRunWriteback("t-once", TaskStatusCompleted, "done"); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()

	count := func() int {
		var n int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM task_events WHERE task_id = ? AND event LIKE ?`,
			"t-once", reclaimRunResidueEvent+"%").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if got := count(); got != 1 {
		t.Fatalf("want exactly ONE close-out residue record, got %d", got)
	}
	var detail string
	if err := db.QueryRowContext(ctx,
		`SELECT detail FROM task_events WHERE task_id = ? ORDER BY id DESC LIMIT 1`, "t-once").Scan(&detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "build.out") {
		t.Fatalf("the record must quote the offending entry back, got %q", detail)
	}
	// The terminal fact is untouched: the outcome is not rolled back.
	tk, err := TaskShow("t-once")
	if err != nil || !strings.Contains(tk, "status:         "+TaskStatusCompleted) {
		t.Fatalf("the completed outcome must stand (the record is a side-channel): %q err=%v", tk, err)
	}
	// A re-landed writeback (the same run writing back twice) files nothing new.
	reclaimRunResidueAtWriteback("t-once", TaskStatusCompleted)
	if got := count(); got != 1 {
		t.Fatalf("a re-landed writeback must not drip a second record, got %d", got)
	}
}

// TestReclaimRunResidueEnforceSwitchRefusesTheWriteback: with the operator's
// switch on, the SAME verdict refuses the completed writeback — the row stays
// OPEN (the run can clean its scratch and write back), the residue is on the
// trail, and the refusal is itself a ledger fact. With the switch off the
// default record semantics hold (no refusal).
func TestReclaimRunResidueEnforceSwitchRefusesTheWriteback(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	_ = repo
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-hard", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-hard", repo, branch, wt)
	if _, err := TaskStart("t-hard", "worker-1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "scratch.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(reclaimEnvEnforce, "1")
	_, err := TaskRunWriteback("t-hard", TaskStatusCompleted, "done")
	if err == nil || !strings.Contains(err.Error(), reclaimUntrackedResidueMarker) {
		t.Fatalf("the enforce switch must refuse the writeback in the classifier's own words, got %v", err)
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM tasks WHERE task_id = ?`, "t-hard").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status == TaskStatusCompleted {
		t.Fatalf("a refused writeback must leave the row open, got %q", status)
	}
	var refused int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM task_events WHERE task_id = ? AND event = ?`,
		"t-hard", reclaimRunResidueRefusedEvent).Scan(&refused); err != nil {
		t.Fatal(err)
	}
	if refused == 0 {
		t.Fatalf("the refusal must be a ledger fact (event %q)", reclaimRunResidueRefusedEvent)
	}
}

// TestReclaimRunResidueUnattributableGroundIsMeasuredNotBlamed: a row whose
// recorded ground exists but is NOT git-attributable (a scratch-root directory
// put in the deliverable) still yields the residue-shaped verdict — recorded as
// the "unattributed" block — so the drift is named instead of silently
// under-reporting. Nothing there is deleted either.
func TestReclaimRunResidueUnattributableGroundIsMeasuredNotBlamed(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	// A SECOND linked worktree of the same repo, off the row's branch: the row
	// records it, git registers it, and it is NOT the main checkout — the shape
	// where the arm must measure yet refuse to attribute.
	other := filepath.Join(t.TempDir(), "sibling-wt")
	gitRun(t, repo, "worktree", "add", "-q", other, "-b", "feat/sibling")
	if err := os.WriteFile(filepath.Join(other, "stray.out"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-block", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-block", repo, branch, other)
	out, err := ReclaimRunResidueCheck("t-block")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !out.Blocked || out.Attributed {
		t.Fatalf("want the unattributed BLOCK for a ground git will not attribute to this row, got %+v", out)
	}
	if !strings.Contains(out.Note, reclaimUntrackedResidueMarker) || !strings.Contains(out.Note, "stray.out") {
		t.Fatalf("the block must carry the wording and the entry, got %q", out.Note)
	}
	if _, err := os.Stat(filepath.Join(other, "stray.out")); err != nil {
		t.Fatalf("nothing may be deleted from the ground: %v", err)
	}
	_ = wt
}

// TestReclaimRunResidueArmNeverForcesAndNeverTouchesABranch closes the guard
// (Rule 13): the enforcement arm's own source may not introduce `--force` or a
// branch-mutating git verb — the removal arms stay absent, so a worktree is
// never removed and a branch is never touched by this half.
func TestReclaimRunResidueArmNeverForcesAndNeverTouchesABranch(t *testing.T) {
	b, err := os.ReadFile("task_reclaim.go")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	src := string(b)
	start := strings.Index(src, reclaimRunResidueArmMarker)
	end := strings.Index(src, "// reclaimGitRemove runs the ordinary")
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("the enforcement arm's boundaries moved — update this gate rather than deleting it")
	}
	// The gate reads the arm's CODE, not its prose: the doc comment above the
	// arm deliberately NAMES the removal command and the force flag to say they
	// stay absent, and a comment cannot delete a file. Stripping line comments
	// keeps the ban on what executes.
	arm := stripGoLineComments(src[start:end])
	for _, banned := range []string{
		"--force",                   // the guard this whole epic may never weaken
		"os.Remove", "os.RemoveAll", // no deletion of a run's scratch by this arm
		gitWorktreeRemoveArgsJoined(), // no `git worktree remove` anywhere in it
	} {
		if strings.Contains(arm, banned) {
			t.Errorf("the enforcement arm must not carry %q — it records, it never removes or mutates (Rule 13)", banned)
		}
	}
}

// gitWorktreeRemoveArgsJoined is the literal spelling of the removal call the
// arm must NOT contain (`git worktree remove`). It is assembled from the
// vocabulary constants the removal arm itself uses, so this gate names the same
// command the code would — never a re-typed copy of it (Rule 1/14) — while
// staying outside the arm's region (the gate, not the arm, holds the spelling).
func gitWorktreeRemoveArgsJoined() string {
	return finishCleanupWorktreeVerb + " " + finishCleanupWorktreeRemoveVerb
}

// stripGoLineComments drops each line's `//` tail so a source-scanning gate
// reads what EXECUTES (the arm's own doc comment names the removal command it
// forswears — a comment is not a call).
func stripGoLineComments(src string) string {
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
func TestRunOwnUntrackedResidue_TrackedChangeKeepsTodaysRefusal(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	_ = repo
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s2", Payload: "p", TaskID: "t-tracked", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-tracked", repo, branch, wt)
	if _, err := TaskStart("t-tracked", "worker-1"); err != nil {
		t.Fatal(err)
	}
	// A TRACKED modification in the same worktree: somebody's WORK, not the
	// run's hygiene. It must keep today's handling EXACTLY — the run's own
	// residue record never speaks for it. (The writeback itself is now refused
	// for this shape by the BINDING gate; that refusal is pinned in
	// dirty_closeout_binding_test.go and is deliberately NOT what this file
	// asserts — here the property is only that the untracked marker is never
	// filed against tracked work.)
	if err := os.WriteFile(filepath.Join(wt, "work.txt"), []byte("deliverable\nreal edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _ = TaskRunWriteback("t-tracked", TaskStatusCompleted, "done") //stylegate:ignore writeback in a run fixture; the ledger state read back afterwards is the assertion
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM task_events WHERE task_id = ? AND detail LIKE ?`,
		"t-tracked", "%"+reclaimUntrackedResidueMarker+"%").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a TRACKED change must keep today's refusal — no run-residue record may be "+
			"filed against somebody's uncommitted work (found %d)", n)
	}
}
