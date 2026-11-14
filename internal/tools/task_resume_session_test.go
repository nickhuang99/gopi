package tools

// The RESUME-TARGET resolution half of the session_id defect (measured on this
// host, 2026-10-28). The ledger's `session_id` column is the SUBMITTER's
// session for an agent-filed row — 1511 rows carried one, only 269 agreed with
// the session record in their OWN run directory, and one chat session
// (07d98407-…) was named by 47 rows it had merely submitted. Both recovery arms
// (the dead-holder resume, the abnormal-child-exit retry) used that column as
// their `gopi session resume` target, so the recovery of such a row continued
// ANOTHER conversation.
//
// The tests below pin the resolution on the production surface
// (TaskResumeSessionFor) with a REAL ledger row (testutil.NewTestDBPath, Rule 32)
// and a REAL run directory in the task-run layout — never a fabricated map. The
// column keeps its submitter semantics: nothing here writes it, and the
// anti-self-reference claim guard that depends on it is untouched.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tasksource"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// resumeSessionTestRoot pins GOPI_HOME at a per-test directory AND gives the
// test its own ledger, so both the run directory the resolver reads and the row
// it reads are this test's — never the operator's live profile (Rule 32).
func resumeSessionTestRoot(t *testing.T) {
	t.Helper()
	t.Setenv(config.GopiDirEnv, t.TempDir())
	testutil.NewTestDBPath(t)
}

// resumeSessionTestSeedRow files one ledger row carrying a session_id COLUMN
// (the submitter's session) — the shape task_submit writes for an agent-filed
// row.
func resumeSessionTestSeedRow(t *testing.T, submittedBy string) string {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	id := idForTest("rsm")
	if _, err := db.ExecContext(ctx, `INSERT INTO tasks
		(task_id, summary, status, session_id, model, parent_task_id, tags, payload,
		 source, deliverable, priority, reason, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, "a row the resolver tests judge", taskStatusPending, submittedBy, "", "",
		"", "", tasksource.Agent, "", 0, "", 1.0, 1.0); err != nil {
		t.Fatalf("insert row %s: %v", id, err)
	}
	return id
}

// resumeSessionTestWriteRun writes ONE session record at the TASK-RUN layout
// `sessions/<task-id>/<session-id>.jsonl` — the very address
// taskRunSessionFromRowDir probes.
func resumeSessionTestWriteRun(t *testing.T, taskID, sessionID string) {
	t.Helper()
	dir := filepath.Join(gopiDataRoot(), config.SessionsDirName, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	body := `{"type":"session_start","sessionId":"` + sessionID + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, sessionID+taskSessionJSONLExt), []byte(body), 0o644); err != nil {
		t.Fatalf("write run record: %v", err)
	}
}

// TestTaskResumeSessionPrefersTheRowsOwnRunOverTheSubmitterColumn is the
// REPRODUCTION of the measured defect and its fix, in one arm: the column names
// the submitter, the row's run directory holds the run that actually executed
// it, and the resolver must answer the LATTER — with the disagreement reported.
func TestTaskResumeSessionPrefersTheRowsOwnRunOverTheSubmitterColumn(t *testing.T) {
	resumeSessionTestRoot(t)
	const submitter = "11111111-1111-4111-8111-111111111111"
	const executed = "22222222-2222-4222-8222-222222222222"
	id := resumeSessionTestSeedRow(t, submitter)
	resumeSessionTestWriteRun(t, id, executed)

	got := TaskResumeSessionFor(id)
	if got.SessionID != executed {
		t.Fatalf("resume target = %q, want the row's OWN run %q (the column names the submitter %q — resuming it replays another conversation)",
			got.SessionID, executed, submitter)
	}
	if got.Source != taskResumeSessionSourceRunDir {
		t.Errorf("source = %q, want %q", got.Source, taskResumeSessionSourceRunDir)
	}
	if got.Column != submitter {
		t.Errorf("Column = %q, want the ledger's verbatim value %q", got.Column, submitter)
	}
	if !got.Differs() {
		t.Error("Differs() must be true when the column names a session that did not run the row — an operator has to be able to see it")
	}
	note := got.Note()
	for _, want := range []string{submitter, executed, taskResumeSessionSourceRunDir} {
		if !strings.Contains(note, want) {
			t.Errorf("note must name %q:\n%s", want, note)
		}
	}
}

// TestTaskResumeSessionFallsBackToTheRowIDWhenNothingRan pins the second step of
// the ladder: a row with no run directory at all resolves to its OWN id (whose
// task-mode resolution reads that directory itself), NOT to the submitter's
// session and NOT to an empty string.
func TestTaskResumeSessionFallsBackToTheRowIDWhenNothingRan(t *testing.T) {
	resumeSessionTestRoot(t)
	const submitter = "33333333-3333-4333-8333-333333333333"
	id := resumeSessionTestSeedRow(t, submitter)

	got := TaskResumeSessionFor(id)
	if got.SessionID != id {
		t.Fatalf("resume target = %q, want the row id %q", got.SessionID, id)
	}
	if got.Source != taskResumeSessionSourceRowID {
		t.Errorf("source = %q, want %q", got.Source, taskResumeSessionSourceRowID)
	}
	if !got.Differs() {
		t.Error("the column still names the submitter, so the disagreement must still be reported")
	}
}

// TestTaskResumeSessionAgreesForARowThatRanUnderItsColumn is the NEGATIVE
// control (and the ordinary case): a row whose run directory holds exactly the
// session the column names resolves to that session, with NOTHING to report —
// so the fix cannot manufacture a note where there is no disagreement.
func TestTaskResumeSessionAgreesForARowThatRanUnderItsColumn(t *testing.T) {
	resumeSessionTestRoot(t)
	const ran = "44444444-4444-4444-8444-444444444444"
	id := resumeSessionTestSeedRow(t, ran)
	resumeSessionTestWriteRun(t, id, ran)

	got := TaskResumeSessionFor(id)
	if got.SessionID != ran || got.Source != taskResumeSessionSourceRunDir {
		t.Fatalf("resolved (%q from %q), want (%q from %q)", got.SessionID, got.Source, ran, taskResumeSessionSourceRunDir)
	}
	if got.Differs() || got.Note() != "" {
		t.Errorf("an agreeing row must produce no note; Differs=%v Note=%q", got.Differs(), got.Note())
	}
}

// TestTaskResumeSessionEmptyIDIsNotATarget: a blank id is the caller's error to
// report, never a resume target of "".
func TestTaskResumeSessionEmptyIDIsNotATarget(t *testing.T) {
	resumeSessionTestRoot(t)
	got := TaskResumeSessionFor("  ")
	if got.SessionID != "" || got.Source != taskResumeSessionSourceRowID {
		t.Fatalf("blank id resolved to (%q from %q), want (empty from %q)", got.SessionID, got.Source, taskResumeSessionSourceRowID)
	}
	if got.Differs() || got.Note() != "" {
		t.Errorf("no ledger reading means nothing to report; Differs=%v Note=%q", got.Differs(), got.Note())
	}
}

// TestDeadHolderArmPassesTheRowsOwnRunToTheSpawn is the ARM-LEVEL arm: it drives
// the real decision function (resumeDeadHolderRow) with a provably dead holder
// and a row whose ledger column names a DIFFERENT session than its run
// directory, and asserts WHICH target reached the spawn seam. This is the shape
// the defect had — the arm resolved `r.SessionID` itself — and it is the only
// place that can regress while the resolver above stays correct.
func TestDeadHolderArmPassesTheRowsOwnRunToTheSpawn(t *testing.T) {
	resumeSessionTestRoot(t)
	const submitter = "55555555-5555-4555-8555-555555555555"
	const executed = "66666666-6666-4666-8666-666666666666"
	id := resumeSessionTestSeedRow(t, submitter)
	resumeSessionTestWriteRun(t, id, executed)

	dead := taskStateTestDeadPid(t)
	if _, err := TaskUpdateReason(id, taskStatusInProgress, "", ""); err != nil {
		t.Fatalf("advance to in_progress: %v", err)
	}
	if err := TaskStampHolder(id, claimHolderOfPid(dead), dead); err == nil {
		t.Fatalf("stamping a dead pid must fail — the fixture needs the pid to be gone")
	}
	wts := t.TempDir()
	wtJSON := `{"repo":"` + t.TempDir() + `","` + DeliverableWorktreeKey + `":"` + wts + `","branch":"feat/rsm"}`

	// The seam returns a REAL living pid: the arm's ownership handoff stamps it
	// as the new holder, and a pid of 0 would take the "stamp failed" branch
	// (correctly) instead of the resumed one under test.
	child := testutil.StartMockProcess(t, testutil.MockDaemonBinary, testutil.MockDaemonHoldSeconds)
	defer child.Reap()

	var gotTarget, gotTask string
	prev := deadHolderResumeSpawnFn
	deadHolderResumeSpawnFn = func(repo, taskID, target string) (string, string, int, error) {
		gotTarget, gotTask = target, taskID
		return "gopi session resume " + target + " --task " + taskID, "/dev/null", child.Pid(), nil
	}
	t.Cleanup(func() { deadHolderResumeSpawnFn = prev })

	oc, judged := resumeDeadHolderRow("", deadHolderResumeRow{
		TaskID: id, SessionID: submitter, Status: taskStatusInProgress, Deliverable: wtJSON,
		ClaimedBy: claimHolderOfPid(dead),
		Ident:     procIdentity{Pid: dead, StartTicks: 4242, Cmd: "sleep 300"},
	}, wts)
	if !judged {
		t.Fatalf("a dead-holder row must be judged (got no outcome)")
	}
	if gotTask != id {
		t.Fatalf("spawn task = %q, want %q", gotTask, id)
	}
	if gotTarget != executed {
		t.Fatalf("spawn target = %q, want the row's OWN run %q — the ledger column %q names the SUBMITTER, and resuming it replays another conversation",
			gotTarget, executed, submitter)
	}
	if !strings.Contains(oc.Reason, submitter) {
		t.Errorf("the outcome must REPORT the column disagreement (Rule 15), got reason %q", oc.Reason)
	}
	if oc.SessionID != executed {
		t.Errorf("the outcome names session %q, want the resumed one %q", oc.SessionID, executed)
	}
}
