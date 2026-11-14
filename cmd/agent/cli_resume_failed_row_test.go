// Resume gate on a TERMINAL row — the incident that prompted this gate
// (ledger row 01a097f8 "how to resume in_progress task", 2026-09-13).
//
// WHAT HAPPENED (reconstructed from the live ledger + session logs; the full
// evidence chain is devdoc/resume-on-failed.md): the row was marked `failed`
// by the post-merge verify of its own merge, and hours later the operator ran
// `gopi session resume <task-id>`. The resume RAN the agent — it spent 684k tokens and
// ended exit_code 0 — yet the operator's `gopi task show` still said `failed`,
// read as "the resume failed again".
//
// The mechanism is a STATUS/PROCESS split at the gate: decideResumeByTaskID
// only ever refuses for a verified-LIVE or unverifiable holder, so a row with
// NO holder (the shape every terminal row has — the identity stamp is cleared
// on the terminal transition) reads `verdict: n/a`. The resume therefore always
// proceeded, while nothing in the resume path moved the row off `failed`:
// runResume's ledger touches were opt-in provenance (TaskTagResumed /
// TaskStampRun) and tools.TaskStampSelfClaim, whose UPDATE was pinned
// `WHERE status='in_progress'` — so on a `failed` row the ownership handoff
// silently matched ZERO rows and the status was untouched (Rule 15: a no-op
// write was reported as success).
//
// BOTH HALVES ARE FIXED (2026-10-29, the two-level resume policy): the status
// axis now ADMITS a failed row, and the handoff now MOVES it to in_progress in
// the same write. A resume of a failed row is therefore visible end to end —
// same task id, same session id, same directory.
//
// These tests PIN that behaviour so the decision "do we allow such a resume?"
// (devdoc/resume-on-failed.md) is made against an executable statement of what
// the code does today, not against prose. If the product decision changes, the
// posture test is the one to flip — deliberately, in a reviewable diff.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tools"
)

// failedRowFallbackSessionID is the "dead end" session id the incident row's
// resumed session id falls back to: the LIVE resumed session had no recorded
// raw log of its own, so the resolution chain ends at the ROW's own session
// (4616b463-… in the incident — the session that delivered the branch). The
// point of this fixture is the shape, not the resolution: the rerun is armed
// with the dead end as its fallback.
const failedRowFallbackSessionID = "4616b463-e896-4d9a-8e7c-738dd5f6465d"

// failedRowSeed submits a row, runs it (pending → in_progress, no holder
// stamp — the shape a row has between the ledger's claim clearing and the
// reclaim), then records the downstream FAIL that made it terminal. This is
// the incident row's lifecycle without the wrapper's claim stamp.
func failedRowSeed(t *testing.T, taskID string) {
	t.Helper()
	resumeGateSeedRow(t, taskID, "")
	if _, err := tools.TaskUpdateReason(taskID, tools.TaskStatusInProgress, "run started", ""); err != nil {
		t.Fatalf("start %s: %v", taskID, err)
	}
	if _, err := tools.TaskUpdateReason(taskID, tools.TaskStatusFailed,
		"judged FAIL downstream (post-merge verify of the delivered branch)", ""); err != nil {
		t.Fatalf("mark %s failed: %v", taskID, err)
	}
}

// TestResumeGateAdmitsTheFailedRow is the POSTURE TEST, and it has been FLIPPED
// back on purpose — twice, because the posture itself changed twice.
//
// THE RULING (2026-10-29): failure handling is TWO-LEVEL, and RESUME IS LEVEL 1
// — for a `failed` row as much as for an interrupted one. A failure judges one
// ATTEMPT (the binary was stale, the bug it hit has since been fixed, the run
// was killed). Rerunning the SAME row — same task id, same session id, same
// directory, checkpoint appended — is a new attempt at the same work, not a
// replay of a verdict. A COPY (`gopi task clone` → `<id>-c1`, new session, new
// workspace) is level 2, the remedy after a resume has been tried again.
//
// So the status axis ADMITS a failed row, and the two axes stay separately
// readable: the PROCESS axis still says n/a (the failed transition cleared the
// identity, so nobody is running it), and the STATUS axis no longer refuses it —
// which is exactly the pair that makes the resume safe to start.
//
// THE REFUSAL IS STILL REAL, and it is pinned by the completed/verified arm in
// internal/tools/task_run_retry_status_gate_test.go: FINISHED work is refused,
// and that refusal names the copy.
func TestResumeGateAdmitsTheFailedRow(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	failedRowSeed(t, "t-failed-row")

	st, _, err := tools.TaskResumeTarget("t-failed-row")
	if err != nil || st == nil {
		t.Fatalf("resume target: %v", err)
	}
	if st.Status != tools.TaskStatusFailed {
		t.Fatalf("status = %q, want %q", st.Status, tools.TaskStatusFailed)
	}
	// The PROCESS axis: nobody is running this row, and that answer must stay a
	// different fact from the status axis (a reader who cannot separate them
	// hunts the wrong thing).
	if st.Verdict != tools.TaskProcessStateNotApplicable {
		t.Errorf("verdict = %q, want %q — the failed row has no holder to verify", st.Verdict, tools.TaskProcessStateNotApplicable)
	}
	// The STATUS axis: LEVEL 1 — admit it.
	if !st.Resumable {
		t.Fatalf("resumable = false on a failed row (%q) — a failure judges one attempt, and level 1 is to resume that attempt", st.StatusRefusal)
	}
	if st.StatusRefusal != "" {
		t.Errorf("an admitted row carries a refusal (%q) — the two fields are exclusive", st.StatusRefusal)
	}
	// And the MECHANISM behind the worker's resume takes the same decision (one
	// answer, two faces — Rule 14).
	if ok, refusal := tools.TaskRunChildResumable("t-failed-row", os.Getpid()); !ok {
		t.Fatalf("TaskRunChildResumable REFUSED a failed row (%q) — `gopi worker --resume` could not continue the work", refusal)
	}
}

// TestResumeOnFailedRowMovesItBackToInProgress is the OTHER half of the incident,
// and it is the half the operator actually READ: the ownership handoff the resume
// performs used to be pinned `WHERE status='in_progress'`, so on a failed row it
// matched ZERO rows and reported success — the row kept saying `failed` (with
// `holder: (none)`) while a live agent ran, and "the resume failed again" was a
// reasonable reading of the ledger.
//
// THE FIX IS THE SAME RULING: the stamp IS the resume (level 1), so it moves the
// row to in_progress in the SAME statement that records the holder — one write,
// no read-then-write window, and the FROM status is the one it matched on.
//
// This test is the REGRESSION GUARD in both directions: the status must move off
// `failed` (or a resume is invisible), and it must NOT be moved by a stamp on a
// status the stamp has no business changing (the pending/terminal arms below).
func TestResumeOnFailedRowMovesItBackToInProgress(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	failedRowSeed(t, "t-stamp-resume")

	// The resume's ownership handoff, verbatim (cmd/agent/cli_resume.go).
	if err := tools.TaskStampSelfClaim("t-stamp-resume"); err != nil {
		t.Fatalf("self-claim: %v", err)
	}

	got, err := tools.TaskStatusOf("t-stamp-resume")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if got != tools.TaskStatusInProgress {
		t.Fatalf("status = %q after a resume's self-claim, want %q — a resume that leaves the row `failed` is the incident (the ledger shows no run)", got, tools.TaskStatusInProgress)
	}
	// AND THE IDENTITY DID NOT CHANGE: a resume continues THIS row, so the id
	// and its session stay put — the copy (`gopi task clone`) is what mints
	// `<id>-r1`, and it is level 2.
	st, err := tools.TaskShow("t-stamp-resume")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if strings.Contains(st, "t-stamp-resume-r1") {
		t.Fatalf("a resume minted a successor row — that is the COPY:\n%s", st)
	}
}

// TestResumeStampDoesNotTouchAFinishedRow is the counter-arm: the widened stamp
// admits failed→in_progress and nothing else. A completed row must stay
// completed — resuming finished work would run it twice.
func TestResumeStampDoesNotTouchAFinishedRow(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	// The FIXTURE walks pending → completed and never through `failed`, on
	// purpose. This test is about the STAMP's edge (a finished row is not
	// moved), so the row must REACH `completed` without exercising the
	// terminal-verdict gate: since a9c520356 a `failed → completed` writeback
	// is REFUSED unless the row carries a reopen-authorization record newer
	// than the verdict, and seeding one here would make the fixture assert the
	// gate it is not testing — reddening in its own setup, before the
	// assertion below ever ran. `failedRowSeed` stays where a failed row IS
	// the subject (the tests above and below this one).
	resumeGateSeedRow(t, "t-stamp-done", "")
	// pending has no edge to a terminal status (allowed from pending:
	// claimed|in_progress|cancelled|superseded), so the fixture walks the
	// ordinary run path to in_progress first — still never through `failed`.
	if _, err := tools.TaskUpdateReason("t-stamp-done", tools.TaskStatusInProgress, "run started", ""); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := tools.TaskUpdateReason("t-stamp-done", tools.TaskStatusCompleted, "delivered", ""); err != nil {
		t.Fatalf("complete: %v", err)
	}
	// The stamp is best-effort by contract, so its error is not the assertion —
	// the STATUS is.
	_ = tools.TaskStampSelfClaim("t-stamp-done") //stylegate:ignore the stamp of a completed row is a documented silent no-op (TaskStampHolder's WHERE excludes terminal rows); the assertion is the STATUS read on the next line
	got, err := tools.TaskStatusOf("t-stamp-done")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if got != tools.TaskStatusCompleted {
		t.Fatalf("status = %q after a self-claim on a completed row, want %q — the resume edge admits failed, never finished work", got, tools.TaskStatusCompleted)
	}
}

// TestResumeProvenanceMarksTheRerunButNotTheOutcome pins the ONE signal the
// resume does leave: the `resumed:<session>` tag (lineage, rendered as
// "resumed: ×N" by task show) and run_method=resume. Neither says whether the
// rerun succeeded — the row's status is the only outcome face, and it is the
// one the resume never touches. Read this test as the statement of the gap the
// report's recommendation targets.
func TestResumeProvenanceMarksTheRerunButNotTheOutcome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, "memory.db"))
	// The session id stamped below must name an artifact of THIS profile
	// (F16.1): a resume stamps the session it has just opened, so the fixture
	// lays that session down in the shape the writer uses.
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv("HOME", home)
	seedResumeSession(t, filepath.Join(home, config.ProfileDirName, config.SessionsDirName),
		"0a4a1f96-4716-43fb-a6af-801b7c149bb4")
	failedRowSeed(t, "t-resume-provenance")

	// What `gopi session resume <task-id>` writes when it starts (cli_resume.go): the
	// lineage tag for the fresh session, and the run provenance.
	if _, err := tools.TaskTagResumed("t-resume-provenance", "0a4a1f96-4716-43fb-a6af-801b7c149bb4"); err != nil {
		t.Fatalf("tag resumed: %v", err)
	}
	if err := tools.TaskStampRun("t-resume-provenance", "resume", "gopi session resume t-resume-provenance"); err != nil {
		t.Fatalf("stamp run: %v", err)
	}

	if n := tools.TaskResumeCount("t-resume-provenance"); n != 1 {
		t.Fatalf("resume count = %d, want 1 — the resume LINEAGE must be visible on the row", n)
	}
	method, _ := tools.TaskRunProvenance("t-resume-provenance")
	if method != "resume" {
		t.Fatalf("run_method = %q, want %q", method, "resume")
	}
	// …and the outcome face is unchanged: the rerun's success/failure has no
	// slot on a terminal row, which is the reported gap.
	st, err := tools.TaskStatusOf("t-resume-provenance")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st != tools.TaskStatusFailed {
		t.Fatalf("status = %q, want %q untouched — the resume writes lineage, never an outcome",
			st, tools.TaskStatusFailed)
	}
}

// TestResumeTargetOnTerminalRowStillResolvesTheRowSession pins the resolution
// leg of the incident: `gopi session resume <task-id>` resolves the row's own recorded
// session checkpoint whatever the row's status is, so a resume aimed at a
// FAILED row is armed with that session's already-delivered context — the
// "dead end" the report calls out. In the incident the resumed run's own fresh
// session had no recorded raw log, so the resolution chain fell back to the
// row's session (4616b463-…), the session that had delivered the branch.
//
// The fallback leg is pinned here as a VALUE (the row's own session id), which
// is what makes "resume a failed row replays the delivering session's context"
// a checkable statement rather than a narrative.
func TestResumeTargetOnTerminalRowStillResolvesTheRowSession(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary:   "resume gate fixture t-resume-terminal-target",
		Payload:   "resume gate fixture",
		TaskID:    "t-resume-terminal-target",
		Source:    "human",
		SessionID: failedRowFallbackSessionID,
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := tools.TaskUpdateReason("t-resume-terminal-target", tools.TaskStatusInProgress, "run started", ""); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := tools.TaskUpdateReason("t-resume-terminal-target", tools.TaskStatusFailed, "judged FAIL downstream", ""); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	st, _, err := tools.TaskResumeTarget("t-resume-terminal-target")
	if err != nil || st == nil {
		t.Fatalf("resume target: %v", err)
	}
	if st.SessionID != failedRowFallbackSessionID {
		t.Fatalf("resolved session = %q, want the row's own %q — the resume is armed with the delivering session's context",
			st.SessionID, failedRowFallbackSessionID)
	}
}
