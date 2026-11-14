// Resume gate, DELIVERY axis (2026-10-30) — the second test of the mechanism,
// driven through THIS package's face.
//
// WHY A SECOND TEST AND NOT A SECOND COPY OF THE FIRST. The axis is measured in
// internal/tools (task_resume_delivery_axis_test.go: LANDED refuses, UNLANDED
// admits, UNKNOWN refuses, on real repositories). What THIS package owns is the
// other half — `gopi session resume`'s own gate (`decideResumeByTaskID`) and the
// notice it prints (`resumeReportDeliveryAdmission`). Those are different code
// with a different failure mode: the tools-layer view can be perfectly correct
// while the CLI still refuses the row, which is exactly the defect this axis
// closes (a decision computed and never acted on).
//
// MEASURED BEFORE THE FIX: three `completed` rows held fifteen commits whose
// tips were ancestors of NOTHING, and this gate answered
// `resumeGateRefuseStatus` for every one of them.
package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tools"
)

// resumeAxisGit runs git for this package's fixture.
func resumeAxisGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// resumeAxisSeed makes the row the gate reads: a REAL repository whose
// `task/<name>` branch carries a commit master does not have, and a ledger row
// that reached `completed` through the real writers with a deliverable naming
// that branch. `landed` fast-forwards master onto the branch first, so the two
// arms differ in exactly ONE fact.
func resumeAxisSeed(t *testing.T, taskID, name string, landed bool) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	resumeAxisGit(t, repo, "init", "-q", "-b", "master")
	resumeAxisGit(t, repo, "config", "user.email", "axis@example.invalid")
	resumeAxisGit(t, repo, "config", "user.name", "resume delivery axis")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resumeAxisGit(t, repo, "add", "f.txt")
	resumeAxisGit(t, repo, "commit", "-q", "-m", "c1")
	branch := "task/" + name
	resumeAxisGit(t, repo, "checkout", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, "g.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resumeAxisGit(t, repo, "add", "g.txt")
	resumeAxisGit(t, repo, "commit", "-q", "-m", "c2")
	resumeAxisGit(t, repo, "checkout", "-q", "master")
	if landed {
		resumeAxisGit(t, repo, "merge", "-q", "--ff-only", branch)
	}

	taskID_ := taskID
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "resume delivery axis " + taskID_,
		Payload: "fixture row",
		TaskID:  taskID_,
		Source:  "human",
	}); err != nil {
		t.Fatalf("submit %s: %v", taskID_, err)
	}
	if _, err := tools.TaskUpdateReason(taskID_, tools.TaskStatusInProgress, "fixture", ""); err != nil {
		t.Fatalf("start %s: %v", taskID_, err)
	}
	if _, err := tools.TaskRunWriteback(taskID_, tools.TaskStatusCompleted, "fixture: completed"); err != nil {
		t.Fatalf("writeback %s: %v", taskID_, err)
	}
	resumeAxisStampDeliverable(t, taskID, repo, branch)
	return repo
}

// resumeAxisStampDeliverable writes the deliverable blob. It is a direct column
// write because NO product writer sets deliverable.branch on an already-terminal
// row — the field is stamped by submit/claim, and this fixture needs it to name
// the branch whose delivery is under test.
func resumeAxisStampDeliverable(t *testing.T, taskID, repo, branch string) {
	t.Helper()
	blob := fmt.Sprintf(`{"repo":%q,"branch":%q}`, repo, branch)
	db, err := sql.Open(prefetchDriverName, tools.LedgerDSN(tools.MemoryDBPath()))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET deliverable = ? WHERE task_id = ?`, blob, taskID); err != nil {
		t.Fatalf("stamp deliverable on %s: %v", taskID, err)
	}
}

// TestResumeGateAdmitsACompletedButUnlandedRow is the defect, end to end, on the
// gate this package owns: a `completed` row whose work never reached the
// integration branch MUST be resumable — and the notice must say so, naming the
// status it overrode.
func TestResumeGateAdmitsACompletedButUnlandedRow(t *testing.T) {
	withResumeAxisLedger(t)
	const taskID = "t-resume-axis-unlanded"
	repo := resumeAxisSeed(t, taskID, "unlanded", false)

	// The fixture's premise, measured (Rule 41): the branch is really NOT on
	// master, otherwise this test would pass for the wrong reason.
	cmd := exec.Command("git", "-C", repo, "merge-base", "--is-ancestor",
		"refs/heads/task/unlanded", "master")
	if err := cmd.Run(); err == nil {
		t.Fatalf("fixture premise broken: task/unlanded IS already on master")
	}

	st, err := tools.TaskProcessStatusFor(taskID)
	if err != nil {
		t.Fatalf("status %s: %v", taskID, err)
	}
	if st.Status != tools.TaskStatusCompleted {
		t.Fatalf("status = %q, want %q", st.Status, tools.TaskStatusCompleted)
	}
	if st.DeliveryVerdict != tools.TaskResumeDeliveryUnlanded {
		t.Fatalf("delivery = %q (%s), want UNLANDED", st.DeliveryVerdict, st.DeliveryWhy)
	}
	if got := decideResumeByTaskID(taskID); got != resumeGateProceed {
		t.Fatalf("gate = %d, want resumeGateProceed — a completed row whose work never landed has something to resume (%s)", got, st.DeliveryWhy)
	}

	// THE NOTICE: a surprising admission must state its evidence, or the operator
	// cannot tell it from a bug.
	var buf bytes.Buffer
	resumeReportDeliveryAdmission(&buf, "gopi session resume", taskID)
	out := buf.String()
	for _, want := range []string{taskID, tools.TaskStatusCompleted, "never landed"} {
		if !strings.Contains(out, want) {
			t.Errorf("the admission notice does not carry %q:\n%s", want, out)
		}
	}
}

// TestResumeGateStillRefusesALandedCompletedRow is the other direction on the
// same gate: when the work DID land, the refusal stands and the notice prints
// NOTHING (an ordinary refusal already speaks for itself; a caveat there would
// claim a measurement that decided nothing).
func TestResumeGateStillRefusesALandedCompletedRow(t *testing.T) {
	withResumeAxisLedger(t)
	const taskID = "t-resume-axis-landed"
	resumeAxisSeed(t, taskID, "landed", true)

	st, err := tools.TaskProcessStatusFor(taskID)
	if err != nil {
		t.Fatalf("status %s: %v", taskID, err)
	}
	if st.DeliveryVerdict != tools.TaskResumeDeliveryLanded {
		t.Fatalf("delivery = %q (%s), want LANDED", st.DeliveryVerdict, st.DeliveryWhy)
	}
	if got := decideResumeByTaskID(taskID); got != resumeGateRefuseStatus {
		t.Fatalf("gate = %d, want resumeGateRefuseStatus — the work is on master, a resume would duplicate it", got)
	}
	var buf bytes.Buffer
	resumeReportDeliveryAdmission(&buf, "gopi session resume", taskID)
	if buf.Len() != 0 {
		t.Fatalf("the admission notice printed for a REFUSED row:\n%s", buf.String())
	}
}

// withResumeAxisLedger points this process at a fresh ledger for one test. The
// command is run under a profile of its own so the operator's live ledger is
// never a target (Rule 32's refusal, one layer up).
func withResumeAxisLedger(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(dir, "memory.db"))
}
