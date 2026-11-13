package tools

// task_resume_delivery_axis_test.go — the DELIVERY axis of the resume gate, and
// the reason it has to exist at all.
//
// THE DEFECT (measured 2026-10-30 on this host, three rows): the resume gate
// judged ONE axis — the ledger status — so `completed` read "this work is
// FINISHED" whatever its branch held. Fifteen commits sat in three such rows,
// each with a clean worktree, a live ground and a tip that is an ancestor of
// NOTHING (`git merge-base --is-ancestor <tip> master` → exit 1, and likewise
// origin/master). The status was TRUE and the conclusion drawn from it was
// FALSE, because "finished" is a claim about the WORK and a status column
// cannot make that claim alone.
//
// THE ARMS. Every fixture is a REAL git repo built in t.TempDir() — the
// smallest sample that makes the two comparisons real (Rule 10.4) — and every
// row goes through the REAL ledger writers (submit → claim → start → writeback),
// so no fixture stands for a state the product could not produce:
//
//	① LANDED    the row's tip IS an ancestor of the integration branch → the
//	            refusal STANDS (this is exactly the pre-fix behaviour, now
//	            EARNED rather than assumed).
//	② UNLANDED  the row's tip is contained in no integration ref → the row is
//	            ADMITTED, with the caveat naming the status it overrode.
//	③ UNKNOWN   the axis could not be read (no deliverable / no branch / no tip
//	            / no integration ref) → the refusal STANDS, and the view says
//	            WHICH axis was unreadable (Rule 15: an unknown is loud without
//	            being a soft "no").
//	④ the gate surfaces AGREE: `gopi task status` (TaskProcessStatusFor) and the
//	  wrapper's pre-flight (TaskRunChildResumable) return the same decision for
//	  the same row — they read ONE computation, not two (Rule 14).
//	⑤ the narrow arms are unchanged: an admitted status is never touched by this
//	  axis, and a status outside the finished-work set is never overridden.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// axisGit runs git in dir for a fixture and fails the test on error.
func axisGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// axisFixture is one real repository with an integration branch (`master`) and a
// second branch carrying one commit that master does NOT contain — the shape a
// task ground leaves behind when its delivery never ran.
type axisFixture struct {
	repo   string
	branch string
	tip    string // the branch's own commit — NOT on master
}

// newAxisFixture builds master@c1 → branch `task/<name>`@c2, and leaves the
// checkout standing on master so `merge-base --is-ancestor` is asked in the
// ordinary posture.
func newAxisFixture(t *testing.T, name string) axisFixture {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	axisGit(t, repo, "init", "-q", "-b", masterBranchName)
	axisGit(t, repo, "config", "user.email", "axis@example.invalid")
	axisGit(t, repo, "config", "user.name", "resume delivery axis")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	axisGit(t, repo, "add", "f.txt")
	axisGit(t, repo, "commit", "-q", "-m", "c1 on master")
	branch := "task/" + name
	axisGit(t, repo, "checkout", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, "g.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	axisGit(t, repo, "add", "g.txt")
	axisGit(t, repo, "commit", "-q", "-m", "c2 on the task branch")
	tip := axisGit(t, repo, "rev-parse", "HEAD")
	axisGit(t, repo, "checkout", "-q", masterBranchName)
	return axisFixture{repo: repo, branch: branch, tip: tip}
}

// axisSeedRow submits a row, walks it to `completed` through the REAL writers,
// and stamps a deliverable naming the fixture's repo and branch. The stamp is a
// direct column write for the same reason the other fixtures do it: no product
// writer exists that sets deliverable.branch on an already-terminal row.
func axisSeedRow(t *testing.T, taskID, repo, branch string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "resume delivery axis " + taskID,
		Payload: "fixture row",
		TaskID:  taskID,
		Source:  "human",
	}); err != nil {
		t.Fatalf("submit %s: %v", taskID, err)
	}
	if err := TaskClaimExplicit(taskID, hostPid(t, os.Getpid())); err != nil {
		t.Fatalf("claim %s: %v", taskID, err)
	}
	if _, err := TaskStart(taskID, hostPid(t, os.Getpid())); err != nil {
		t.Fatalf("start %s: %v", taskID, err)
	}
	if _, err := TaskRunWriteback(taskID, TaskStatusCompleted, "fixture: completed"); err != nil {
		t.Fatalf("writeback %s: %v", taskID, err)
	}
	if strings.TrimSpace(repo) == "" && strings.TrimSpace(branch) == "" {
		return // the "no deliverable at all" arm
	}
	blob := `{"repo":"` + repo + `","branch":"` + branch + `"}`
	ctx := t.Context()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET deliverable = ? WHERE task_id = ?`, blob, taskID); err != nil {
		t.Fatalf("stamp deliverable on %s: %v", taskID, err)
	}
}

// TestResumeDeliveryAxisLandedRefusesAndUnlandedAdmits is arm ① and ② on ONE
// axis, driven through the REAL resume gate (TaskRunChildResumable) and the REAL
// status view (TaskProcessStatusFor), because "the axis reads X" is worthless if
// the gate does not act on it — that separation is exactly the defect.
func TestResumeDeliveryAxisLandedRefusesAndUnlandedAdmits(t *testing.T) {
	testutil.NewTestDBPath(t)
	f := newAxisFixture(t, "unlanded")
	const taskID = "t-axis-unlanded"
	axisSeedRow(t, taskID, f.repo, f.branch)

	// The fixture's premise, measured rather than assumed: the tip really is
	// absent from master (Rule 41 — a fixture whose premise nobody checked
	// proves nothing about the code under it).
	if _, err := finishCleanupGitParsedStdout(t.Context(), f.repo, finishCleanupGitTimeout,
		finishCleanupPreMergeMergeBaseVerb, finishCleanupPreMergeAncestorFlag, f.tip, masterBranchName); err == nil {
		t.Fatalf("fixture premise broken: %s IS already on master", f.tip)
	}

	st, err := TaskProcessStatusFor(taskID)
	if err != nil {
		t.Fatalf("status %s: %v", taskID, err)
	}
	if st.Status != TaskStatusCompleted {
		t.Fatalf("status = %q, want %q", st.Status, TaskStatusCompleted)
	}
	if st.DeliveryVerdict != TaskResumeDeliveryUnlanded {
		t.Fatalf("delivery verdict = %q (%s), want %q — the tip is on no integration ref",
			st.DeliveryVerdict, st.DeliveryWhy, TaskResumeDeliveryUnlanded)
	}
	if st.StatusRefusal != "" {
		t.Errorf("status_refusal is set on a row the delivery axis admitted:\n%q", st.StatusRefusal)
	}
	if !st.Resumable {
		t.Fatalf("resumable = false on a completed row whose work never landed — the defect this axis exists for")
	}
	if !strings.Contains(st.StatusCaveat, TaskStatusCompleted) || !strings.Contains(st.StatusCaveat, "never landed") {
		t.Errorf("the caveat must name the overridden status AND the measurement that overrode it:\n%q", st.StatusCaveat)
	}
	report := TaskProcessStatusReport(st)
	if !strings.Contains(report, "delivery:    "+TaskResumeDeliveryUnlanded) {
		t.Errorf("the report does not render the axis:\n%s", report)
	}
	if !strings.Contains(report, "resumable:   yes") {
		t.Errorf("the report does not render the admission:\n%s", report)
	}

	// THE WRAPPER'S PRE-FLIGHT must reach the same decision (arm ④).
	ok, verdict := TaskRunChildResumable(taskID, os.Getpid())
	if !ok {
		t.Fatalf("TaskRunChildResumable refused an unlanded completed row (%q) — the two gate faces disagree", verdict)
	}
	if !strings.Contains(verdict, TaskResumeDeliveryUnlanded) {
		t.Errorf("the admission verdict does not name the axis that admitted it: %q", verdict)
	}
}

// TestResumeDeliveryAxisLandedStillRefuses is arm ①: when the work really did
// leave, the refusal STANDS — with the measurement quoted, so a reader can tell
// an earned refusal from the old assumption.
func TestResumeDeliveryAxisLandedStillRefuses(t *testing.T) {
	testutil.NewTestDBPath(t)
	f := newAxisFixture(t, "landed")
	// LAND the branch: fast-forward master onto the branch's commit — the
	// smallest true instance of "the work reached the integration branch".
	axisGit(t, f.repo, "merge", "-q", "--ff-only", f.branch)
	if _, err := finishCleanupGitParsedStdout(t.Context(), f.repo, finishCleanupGitTimeout,
		finishCleanupPreMergeMergeBaseVerb, finishCleanupPreMergeAncestorFlag, f.tip, masterBranchName); err != nil {
		t.Fatalf("fixture premise broken: %s is NOT on master after the merge: %v", f.tip, err)
	}
	const taskID = "t-axis-landed"
	axisSeedRow(t, taskID, f.repo, f.branch)

	st, err := TaskProcessStatusFor(taskID)
	if err != nil {
		t.Fatalf("status %s: %v", taskID, err)
	}
	if st.DeliveryVerdict != TaskResumeDeliveryLanded {
		t.Fatalf("delivery verdict = %q (%s), want %q", st.DeliveryVerdict, st.DeliveryWhy, TaskResumeDeliveryLanded)
	}
	if st.Resumable {
		t.Fatalf("resumable = true on a completed row whose work IS on master — a resume would duplicate delivered work")
	}
	if st.StatusRefusal == "" {
		t.Fatalf("status_refusal is empty on a landed completed row")
	}
	if ok, _ := TaskRunChildResumable(taskID, os.Getpid()); ok {
		t.Fatalf("TaskRunChildResumable admitted a LANDED completed row — the two gate faces disagree")
	}
}

// TestResumeDeliveryAxisUnknownKeepsTheRefusal is arm ③, and it is the arm that
// keeps the rule honest: an UNREADABLE axis is NOT an invitation. A row with no
// deliverable records no branch, so there is nothing to compare — and "I could
// not measure" must not become "there is evidence of owed work", which is the
// `completed`-row population whose admission was itself the defect an earlier
// fix closed.
func TestResumeDeliveryAxisUnknownKeepsTheRefusal(t *testing.T) {
	testutil.NewTestDBPath(t)
	const taskID = "t-axis-unknown"
	axisSeedRow(t, taskID, "", "")

	st, err := TaskProcessStatusFor(taskID)
	if err != nil {
		t.Fatalf("status %s: %v", taskID, err)
	}
	if st.DeliveryVerdict != TaskResumeDeliveryUnknown {
		t.Fatalf("delivery verdict = %q (%s), want %q for a row with no deliverable",
			st.DeliveryVerdict, st.DeliveryWhy, TaskResumeDeliveryUnknown)
	}
	if st.Resumable {
		t.Fatalf("resumable = true on an UNKNOWN verdict — an absent measurement was treated as evidence")
	}
	if st.StatusRefusal == "" {
		t.Fatalf("status_refusal is empty on an unknown verdict")
	}
	if ok, _ := TaskRunChildResumable(taskID, os.Getpid()); ok {
		t.Fatalf("TaskRunChildResumable admitted a row whose delivery axis could not be read")
	}
	// The unknown must be LOUD: the report names the axis and why it was
	// unreadable, so a refusal that rests on nothing does not look like one that
	// rests on a measurement (Rule 15).
	report := TaskProcessStatusReport(st)
	if !strings.Contains(report, "delivery:    "+TaskResumeDeliveryUnknown) {
		t.Errorf("the report does not state the unreadable axis:\n%s", report)
	}
	if !strings.Contains(report, "no deliverable") {
		t.Errorf("the report does not say WHAT could not be read:\n%s", report)
	}
}

// TestResumeDeliveryAxisNeverTouchesAdmittedStatuses is arm ⑤: an `in_progress`
// row has no finished-work claim to override, so the axis is not consulted and
// its fields stay empty. This is the arm that keeps the cost off the ordinary
// path — two git subprocesses per admitted row, buying a decision nobody
// disputed, is exactly the shape that makes an inspection verb slow.
func TestResumeDeliveryAxisNeverTouchesAdmittedStatuses(t *testing.T) {
	testutil.NewTestDBPath(t)
	const taskID = "t-axis-inprogress"
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "resume delivery axis " + taskID, Payload: "fixture row",
		TaskID: taskID, Source: "human",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := TaskClaimExplicit(taskID, hostPid(t, os.Getpid())); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := TaskStart(taskID, hostPid(t, os.Getpid())); err != nil {
		t.Fatalf("start: %v", err)
	}
	st, err := TaskProcessStatusFor(taskID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.DeliveryVerdict != "" || st.DeliveryWhy != "" {
		t.Fatalf("the delivery axis ran on an in_progress row (verdict=%q) — it must only be consulted for a finished-work status, or every admitted row pays two git probes",
			st.DeliveryVerdict)
	}
	// The HOLDER arm is untouched: this row's recorded identity is this very
	// test process, so it reads `running` and is un-resumable for the reason it
	// always was — the point here is only that the DELIVERY axis stayed out of it.
	if st.Verdict == TaskProcessStateRunning && st.Resumable {
		t.Fatalf("a running row read resumable — the holder arm changed: %+v", st)
	}
	if st.StatusRefusal != "" {
		t.Fatalf("status_refusal set on an admitted status: %q", st.StatusRefusal)
	}
}

// TestResumeDeliveryAxisLeavesSupersededAlone is the arm that keeps the rule
// from over-reaching, and it is here because the FIRST draft of the mechanism
// got it wrong: it asked the verdict alone, so a `superseded` row whose reissue
// had not landed yet would have been admitted — running the same work twice
// under two ids, which is the exact duplication this gate exists to prevent.
//
// `superseded` says the row was retired IN FAVOUR OF A SUCCESSOR: its work lives
// in ANOTHER row's delivery, so an unlanded tip here is the NORMAL shape of a
// reissue, not evidence of owed work.
func TestResumeDeliveryAxisLeavesSupersededAlone(t *testing.T) {
	testutil.NewTestDBPath(t)
	f := newAxisFixture(t, "superseded")
	const taskID = "t-axis-superseded"
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "resume delivery axis " + taskID, Payload: "fixture row",
		TaskID: taskID, Source: "human",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := TaskUpdateReason(taskID, TaskStatusInProgress, "fixture", ""); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := TaskUpdateReason(taskID, TaskStatusSuperseded, "retired in favour of a successor", ""); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	ctx := t.Context()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	blob := `{"repo":"` + f.repo + `","branch":"` + f.branch + `"}`
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET deliverable = ? WHERE task_id = ?`, blob, taskID); err != nil {
		t.Fatalf("stamp deliverable: %v", err)
	}

	st, err := TaskProcessStatusFor(taskID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Status != TaskStatusSuperseded {
		t.Fatalf("status = %q, want %q", st.Status, TaskStatusSuperseded)
	}
	if st.Resumable {
		t.Fatalf("resumable = true on a SUPERSEDED row — its work belongs to its successor's delivery, and resuming it would run that work twice")
	}
	if st.StatusRefusal == "" {
		t.Fatalf("status_refusal is empty on a superseded row")
	}
	if ok, _ := TaskRunChildResumable(taskID, os.Getpid()); ok {
		t.Fatalf("TaskRunChildResumable admitted a superseded row")
	}
}
