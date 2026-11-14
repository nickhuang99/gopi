package tools

// The DELIVERABLE-TIP criterion's regression tests (epic 01a13bbb).
//
// The contract, in the operator's terms:
//
//   - a row that claims `completed` while the tip of its OWN recorded
//     deliverable.branch is reachable from no ref gets a NAMED, ATTRIBUTED
//     record on its own trail, carrying the tip (from the live ref, or from the
//     removal-audit record of its deletion) — and the outcome still stands,
//     because the measurement is a READ that deletes and restores nothing;
//   - the CONTROL case passes untouched: a row whose deliverable records no
//     branch is never judged, and no door is touched for it;
//   - a tip that a ref still contains, or that the recorded delivery point
//     holds, or that reachable history names, is SILENT — the false-positive
//     class this criterion must not reproduce;
//   - an unreadable repository is reported as `unchecked`, never as
//     "no ref holds it" (Rule 15);
//   - the operator's switch may REFUSE the `completed` writeback, and then the
//     row stays OPEN with the record already on its trail.
import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/removalaudit"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// tipFixture builds the deadHolderReclaimFixture's repo/worktree/branch and
// pins a PRIVATE removal-audit sink, so this arm's records and readings never
// touch the operator's own log (Rule 34's shape: a test gets its environment
// from the scaffold).
func tipFixture(t *testing.T) (repo, wt, branch string) {
	t.Helper()
	repo, wt, branch = deadHolderReclaimFixture(t)
	sink := filepath.Join(t.TempDir(), "removal-audit.log")
	t.Setenv(removalaudit.EnvSink, sink)
	return repo, wt, branch
}

// retireGround drops the linked-worktree registration and the branch's ref —
// the ORDER the close-out chain uses (worktree remove → branch delete), and the
// order this arm needs: deleting a ref that a live registration has checked out
// leaves that worktree's HEAD unborn, which reads as an uncommitted deliverable
// and hands the writeback to the run-residue arm instead of this one.
func retireGround(t *testing.T, repo, wt, branch string) {
	t.Helper()
	gitRun(t, repo, "worktree", "remove", wt)
	gitRun(t, repo, "update-ref", "-d", "refs/heads/"+branch)
}

// deleteRefAndRecord deletes the branch's ref the way the close-out chain does
// — the tip is captured FIRST, then the ref goes, then the removal-audit record
// carries the pair — so the fixture's state is the incident's own (object alive,
// no ref, a record naming the tip).
func deleteRefAndRecord(t *testing.T, repo, wt, branch, taskID string) string {
	t.Helper()
	tip := strings.TrimSpace(covLifeGit(t, repo, covLifeGitBinary, "rev-parse", "refs/heads/"+branch))
	if tip == "" {
		t.Fatalf("fixture: refs/heads/%s did not resolve", branch)
	}
	removalaudit.BranchDeleteAt(removalaudit.ArmFinishCleanupBranchForce, repo, branch, taskID, tip,
		"", "fixture: the close-out chain's own force delete")
	retireGround(t, repo, wt, branch)
	if got := strings.TrimSpace(covLifeGit(t, repo, covLifeGitBinary, "for-each-ref", "--contains", tip, "--format=%(refname)")); got != "" {
		t.Fatalf("fixture: the tip is still contained by %q — the fixture did not reproduce the incident", got)
	}
	return tip
}

// TestDeliverableTipUnreachableIsRecorded (the defect class): with the ref gone
// and nothing holding the tip, the row's writeback leaves an attributed record
// naming the tip and the branch, and the terminal status still stands.
func TestDeliverableTipUnreachableIsRecorded(t *testing.T) {
	repo, wt, branch := tipFixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-tip-gone", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-tip-gone", repo, branch, wt)
	if _, err := TaskStart("t-tip-gone", "worker-1"); err != nil {
		t.Fatal(err)
	}
	tip := deleteRefAndRecord(t, repo, wt, branch, "t-tip-gone")

	if _, err := TaskRunWriteback("t-tip-gone", TaskStatusCompleted, "done — elapsed=1s"); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	detail := lastEventDetail(t, "t-tip-gone", deliverableTipUnreachableEvent)
	if detail == "" {
		t.Fatalf("SILENT LOSS: the row claims %s while its own deliverable %s resolves to no ref "+
			"(tip %s) and NOTHING on its trail says so", TaskStatusCompleted, branch, tip)
	}
	for _, want := range []string{deliverableTipUnreachableMarker, "t-tip-gone", branch, tip} {
		if !strings.Contains(detail, want) {
			t.Errorf("the record must name %q — got %q", want, detail)
		}
	}
	// The outcome stands: nothing is deleted, moved or restored by the arm.
	if st := taskStatusNow(t, "t-tip-gone"); st != TaskStatusCompleted {
		t.Fatalf("status = %q, want the authoritative terminal write to stand (%q)", st, TaskStatusCompleted)
	}
}

// TestDeliverableTipControlCaseIsNeverJudged: the control group the acceptance
// requires — a row whose deliverable records no branch is not judged at all,
// and its writeback is untouched.
func TestDeliverableTipControlCaseIsNeverJudged(t *testing.T) {
	repo, _, _ := tipFixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-tip-none", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	// A deliverable that names the repo but NO branch (the ordinary row).
	seedDeliverable(t, "t-tip-none", repo, "", "")
	if _, err := TaskStart("t-tip-none", "worker-1"); err != nil {
		t.Fatal(err)
	}
	out, err := TaskDeliverableTipReachable("t-tip-none")
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if out.Verdict != deliverableTipVerdictNoBranch || out.Note != "" {
		t.Fatalf("a row that records no branch must NOT be judged, got %+v", out)
	}
	if _, err := TaskRunWriteback("t-tip-none", TaskStatusCompleted, "done"); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	if detail := lastEventDetail(t, "t-tip-none", deliverableTipUnreachableEvent); detail != "" {
		t.Fatalf("no record may be filed for a row that names no branch — got %q", detail)
	}
}

// TestDeliverableTipReachableShapesAreSilent: the three ways a tip is accounted
// for — its own ref still exists, another ref contains it, or the recorded
// delivery point holds it — produce no record and no refusal.
func TestDeliverableTipReachableShapesAreSilent(t *testing.T) {
	t.Run("own ref still there", func(t *testing.T) {
		repo, wt, branch := tipFixture(t)
		submitRow(t, "t-tip-live", repo, branch, wt)
		out, err := TaskDeliverableTipReachable("t-tip-live")
		if err != nil {
			t.Fatalf("judge: %v", err)
		}
		if out.Verdict != deliverableTipVerdictReachable {
			t.Fatalf("a live ref holds its own tip — verdict %q (%s)", out.Verdict, out.Note)
		}
	})
	t.Run("delivery point holds the tip", func(t *testing.T) {
		repo, wt, branch := tipFixture(t)
		submitRow(t, "t-tip-delivered", repo, branch, wt)
		// The delivery point is a LOCAL repo (a bare clone) so the test needs no
		// network: the close-out chain's own channel (ls-remote) is what is
		// measured, and a path is a valid target for it.
		delivery := filepath.Join(t.TempDir(), "delivery.git")
		gitRun(t, repo, "clone", "--bare", repo, delivery)
		gitRun(t, repo, "push", delivery, "refs/heads/"+branch+":refs/heads/"+branch)
		tip := strings.TrimSpace(covLifeGit(t, repo, covLifeGitBinary, "rev-parse", "refs/heads/"+branch))
		removalaudit.BranchDeleteAt(removalaudit.ArmFinishCleanupBranchForce, repo, branch, "t-tip-delivered", tip,
			delivery, "fixture: pushed to a local delivery point")
		retireGround(t, repo, wt, branch)
		out, err := TaskDeliverableTipReachable("t-tip-delivered")
		if err != nil {
			t.Fatalf("judge: %v", err)
		}
		if out.Verdict != deliverableTipVerdictReachable {
			t.Fatalf("the delivery point holds %s — verdict %q (%s)", tip, out.Verdict, out.Note)
		}
	})
	t.Run("another ref contains the tip", func(t *testing.T) {
		repo, wt, branch := tipFixture(t)
		submitRow(t, "t-tip-merged", repo, branch, wt)
		tip := strings.TrimSpace(covLifeGit(t, repo, covLifeGitBinary, "rev-parse", "refs/heads/"+branch))
		// A merge commit on master: the ordinary tail of a landed branch whose
		// ref was consumed. `--no-ff` so the tip is on master's ancestry.
		gitRun(t, repo, "checkout", "master")
		gitRun(t, repo, "merge", "--no-ff", "-m", "merge "+branch, branch)
		removalaudit.BranchDeleteAt(removalaudit.ArmFinishCleanupBranchForce, repo, branch, "t-tip-merged", tip,
			"", "fixture: the ref was consumed after the landing")
		retireGround(t, repo, wt, branch)
		out, err := TaskDeliverableTipReachable("t-tip-merged")
		if err != nil {
			t.Fatalf("judge: %v", err)
		}
		if out.Verdict != deliverableTipVerdictReachable {
			t.Fatalf("master's merge commit contains %s — verdict %q (%s)", tip, out.Verdict, out.Note)
		}
	})
}

// TestDeliverableTipUnreadableRepoIsUnchecked: a reader that could not look must
// say so — `unchecked`, never the defect verdict (Rule 15).
func TestDeliverableTipUnreadableRepoIsUnchecked(t *testing.T) {
	tipFixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-tip-unread", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-tip-unread", filepath.Join(t.TempDir(), "no-such-repo"), "feat/vanished", "")
	out, err := TaskDeliverableTipReachable("t-tip-unread")
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if out.Verdict != deliverableTipVerdictUnchecked {
		t.Fatalf("an unreadable repository must read as %q, got %q (%s)", deliverableTipVerdictUnchecked, out.Verdict, out.Note)
	}
	if out.Note == "" || strings.Contains(out.Note, deliverableTipUnreachableMarker) {
		t.Fatalf("the unchecked note must NAME the gap and must never wear the refusal's sentence — got %q", out.Note)
	}
}

// TestDeliverableTipEnforceSwitchRefusesTheWriteback: the operator's switch
// leaves the row OPEN (status untouched) with the record already filed.
func TestDeliverableTipEnforceSwitchRefusesTheWriteback(t *testing.T) {
	repo, wt, branch := tipFixture(t)
	t.Setenv(deliverableTipUnreachableEnv, "1")
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-tip-refuse", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-tip-refuse", repo, branch, wt)
	if _, err := TaskStart("t-tip-refuse", "worker-1"); err != nil {
		t.Fatal(err)
	}
	deleteRefAndRecord(t, repo, wt, branch, "t-tip-refuse")
	if _, err := TaskRunWriteback("t-tip-refuse", TaskStatusCompleted, "done"); err == nil {
		t.Fatalf("the enforce switch must REFUSE the %s writeback of a row whose deliverable tip is unreachable", TaskStatusCompleted)
	}
	if st := taskStatusNow(t, "t-tip-refuse"); st == TaskStatusCompleted {
		t.Fatalf("the refused writeback must leave the row OPEN, status = %q", st)
	}
	if detail := lastEventDetail(t, "t-tip-refuse", deliverableTipUnreachableEvent); detail == "" {
		t.Fatalf("the still-open row must carry the record that explains it")
	}
	if detail := lastEventDetail(t, "t-tip-refuse", deliverableTipUnreachableRefusedEvent); detail == "" {
		t.Fatalf("the binding verdict must be filed as its own fact (%s)", deliverableTipUnreachableRefusedEvent)
	}
}

// ── fixtures ───────────────────────────────────────────────────────────────

// submitRow submits a row with the fixture's deliverable recorded and started,
// so a judge may be run against it.
func submitRow(t *testing.T, taskID, repo, branch, wt string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, taskID, repo, branch, wt)
	if _, err := TaskStart(taskID, "worker-1"); err != nil {
		t.Fatal(err)
	}
}

// lastEventDetail returns the newest detail filed under an event whose name has
// the given PREFIX ("" when none was filed) — so a test reads the same trail a
// reviewer would.
func lastEventDetail(t *testing.T, taskID, event string) string {
	t.Helper()
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	var detail string
	err := db.QueryRowContext(ctx,
		`SELECT detail FROM task_events WHERE task_id = ? AND event LIKE ? ORDER BY id DESC LIMIT 1`,
		taskID, event+"%").Scan(&detail)
	if err != nil {
		return ""
	}
	return detail
}

// taskStatusNow reads the row's current status out of the ledger.
func taskStatusNow(t *testing.T, taskID string) string {
	t.Helper()
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM tasks WHERE task_id = ?`, taskID).Scan(&status); err != nil {
		t.Fatalf("read status of %s: %v", taskID, err)
	}
	return status
}

// TestDeliverableTipMarkerHasOneSpelling is the Rule 13/14 mirror gate: the
// marker phrase is PROSE a test (and an operator) matches, so a second copy of
// it anywhere in the production tree would be a value with two definitions —
// exactly the drift the mirror gates exist to refuse. The scan reads
// internal/tools' NON-test files and asserts the literal occurs once: at the
// constant that owns it.
func TestDeliverableTipMarkerHasOneSpelling(t *testing.T) {
	const literal = "its deliverable's tip is reachable from no ref"
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	scanned, hits := 0, 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		scanned++
		hits += strings.Count(string(body), literal)
	}
	if scanned < deliverableTipMirrorGateMinFiles {
		t.Fatalf("read-floor: the scan read %d production files — a scan that found nothing must not pass green (Rule 15)", scanned)
	}
	if hits != 1 {
		t.Fatalf("the marker %q must have exactly ONE definition in the production tree, found %d", literal, hits)
	}
}

// deliverableTipMirrorGateMinFiles is the read-floor of the mirror gate above:
// this package ships hundreds of production files, so a scan that read fewer is
// a scan that did not run.
const deliverableTipMirrorGateMinFiles = 50

// TestDeliverableTipProducedNothingIsNotALoss is the WRITEBACK half of the
// produced-nothing defect (measured 2026-10-31 on row 01a18d61-509f).
//
// THE INCIDENT, verbatim from that row's close-attempt.txt:
//
//	[task] run 01a18d61-… claims `completed` for /home/user/work/gopi, but its
//	  deliverable's tip is reachable from no ref: branch task/… resolves to no ref,
//	  no delivery point was recorded for it, and no reachable commit names this task
//	  id …; the work is unreachable and nothing was deleted, moved or restored by
//	  this arm
//
// Every clause of that sentence is TRUE and the conclusion is FALSE: the ground
// was still standing on the commit the branch was cut from (`rev-list --count
// HEAD --not origin/master` = 0, reflog = clone + checkout), so the run produced
// NO commit of its own. There was no work to be unreachable — yet the sentence an
// operator reads says to hunt for a missing commit, and a row whose deliverable
// was a DECOMPOSITION (four child rows) reads as a lost branch.
//
// THE FIX: a sixth verdict, `produced-nothing`, decided AFTER every reachability
// arm has said no and BEFORE the defect verdict may be written. The witnesses are
// the ones that already exist (Rules 8/13): the branch's own reflog (the cut
// point) when a ground is alive, and the commit's own stamps against the row's
// execution clock when it is not.
//
// THIS TEST DRIVES THE SECOND WITNESS — the reclaimed-workspace case, which is
// the incident's own shape — through the REAL surface: a ledger row, a real git
// repo whose tip was made BEFORE the run started, the ref deleted the way the
// close-out chain deletes it (with the removal-audit record carrying the tip), and
// then TaskDeliverableTipReachable's answer.
func TestDeliverableTipProducedNothingIsNotALoss(t *testing.T) {
	repo, branch, tip := producedNothingFixture(t)
	taskID := "t-tip-produced-nothing"
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, taskID, repo, branch, "")
	if _, err := TaskStart(taskID, "worker-1"); err != nil {
		t.Fatal(err)
	}
	// The close-out chain's own order: record the tip, delete the branch.
	removalaudit.BranchDeleteAt(removalaudit.ArmFinishCleanupBranchForce, repo, branch, taskID, tip,
		"", "fixture: the close-out chain's own force delete")
	gitRun(t, repo, "update-ref", "-d", "refs/heads/"+branch)

	out, err := TaskDeliverableTipReachable(taskID)
	if err != nil {
		t.Fatalf("TaskDeliverableTipReachable: %v", err)
	}
	if out.Verdict != deliverableTipVerdictProducedNothing {
		t.Fatalf("verdict = %q, want %q — nothing is unreachable when nothing was produced (note: %s)",
			out.Verdict, deliverableTipVerdictProducedNothing, out.Note)
	}
	if strings.Contains(out.Note, deliverableTipUnreachableMarker) {
		t.Fatalf("the produced-nothing note must NOT carry the unreachable marker %q:\n%s",
			deliverableTipUnreachableMarker, out.Note)
	}
	for _, want := range []string{"produced NO commit of its own", "NOTHING to look for", taskID} {
		if !strings.Contains(out.Note, want) {
			t.Fatalf("the note must say %q:\n%s", want, out.Note)
		}
	}
	// THE WRITEBACK ITSELF, not only the read: the row closes `completed` with the
	// conclusion on its trail — the seam the incident's sentence came from.
	if _, err := TaskRunWriteback(taskID, TaskStatusCompleted, "done — elapsed=1s"); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	// THE TRAIL CARRIES THE CONCLUSION UNDER ITS OWN NAME, and the DEFECT's name
	// is absent — the distinction the operator greps (Rule 15).
	if detail := lastEventDetail(t, taskID, deliverableTipProducedNothingEvent); detail == "" {
		t.Fatalf("the row's trail must record the produced-nothing conclusion under %q", deliverableTipProducedNothingEvent)
	}
	if detail := lastEventDetail(t, taskID, deliverableTipUnreachableEvent); detail != "" {
		t.Fatalf("no loss may be recorded for a ground that never produced a commit, got %q", detail)
	}
}

// producedNothingFixture builds a repo whose tip was committed LONG before any
// run this test can start (so the stamps witness answers "produced before the run
// began"), on a branch of its own, and returns (repo, branch, tip).
//
// The commit dates are the fixture's WHOLE POINT — a commit made "now" would fall
// inside the clock-skew window and the witness would answer "maybe this run's
// work", which is the control case (TestDeliverableTipUnreachableIsRecorded), not
// this one.
func producedNothingFixture(t *testing.T) (repo, branch, tip string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	sink := filepath.Join(t.TempDir(), "removal-audit.log")
	t.Setenv(removalaudit.EnvSink, sink)
	repo = t.TempDir()
	branch = "task/t-tip-produced-nothing"
	gitRun(t, repo, "init", "--quiet", "-b", "master")
	gitRun(t, repo, "config", "user.email", "t@t")
	gitRun(t, repo, "config", "user.name", "t")
	gitDatedCommit(t, repo, "a.txt", "base — committed before any run of this test could begin")
	gitRun(t, repo, "checkout", "--quiet", "-b", branch)
	// The deliverable lives ONLY on the branch, so deleting the branch leaves the
	// tip in no ref at all — the incident's exact state (a ref that resolves to
	// nothing while the object survives). A tip that master also holds would be
	// `reachable`, which is the CONTROL case, not this one.
	gitDatedCommit(t, repo, "work.txt", "the row's own deliverable")
	tip = strings.TrimSpace(covLifeGit(t, repo, covLifeGitBinary, "rev-parse", "HEAD"))
	gitRun(t, repo, "checkout", "--quiet", "master")
	if got := strings.TrimSpace(covLifeGit(t, repo, covLifeGitBinary, "for-each-ref", "--contains", tip, "--format=%(refname)")); got == "" {
		t.Fatalf("fixture: the tip %s must be held by %s before the delete", tip, branch)
	}
	return repo, branch, tip
}

// gitDatedCommit writes a file and commits it with BOTH stamps pinned to a time
// far in the past, so the commit is provably not any run's work.
func gitDatedCommit(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", name)
	cmd := exec.Command("git", "commit", "--quiet", "-m", "fixture: a commit older than any run")
	cmd.Dir = dir
	cmd.Env = testutil.GitEnv(
		"GIT_AUTHOR_DATE=2020-01-01T00:00:00 +0000",
		"GIT_COMMITTER_DATE=2020-01-01T00:00:00 +0000")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dated commit: %v: %s", err, strings.TrimSpace(string(out)))
	}
}
