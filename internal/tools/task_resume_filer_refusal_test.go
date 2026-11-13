package tools

// THE FILER IS NOT A RESUME ADDRESS — the regression for the phantom-completion
// defect measured on this host 2026-11-01 (row 01a19075-80b5-7a85-b0df-02a55f1c8b16).
//
// WHAT HAPPENED, from the ledger and the filesystem rather than from a
// description of them:
//
//   - the row NEVER RAN. Its directory holds metadata.json + output.log and no
//     run log at all, and it was in `failed` at 12:51:12.
//   - its `session_id` column was EMPTY; its `submitter_session` column named
//     the chat session 01a18d61-509f-… that had filed it.
//   - at 12:51:48 the delivery guard wrote `delivery-proof-unverified` ("the
//     tip … is the very commit this run's ground was cut from, so the run
//     produced NO commit of its own").
//   - at 12:51:49 and again at 12:51:50 the row was written `completed` —
//     TWICE, and the first one carries a run-stats digest,
//     `elapsed=28.5s iter=9 tokens=541766`, that is EXACTLY the session_end the
//     FILER session recorded at 12:51:47.078 (28.530042935s / 9/541766).
//   - the filer's own transcript ends with
//     `{"type":"task_row_closed","task_id":"01a19075-…","status":"completed"}`
//     at 12:51:50.559 — i.e. a `gopi session resume` of the FILER, pointed at
//     this row by `--task`, wrote this row's terminal status out of its own
//     run's statistics.
//   - the reach that armed it: TaskResumeTarget's legacy fallback took
//     `st.SessionID`, which taskProcessSessionOf had filled with the FILER
//     because the row had no run of its own, and returned the FILER's
//     checkpoint. Measured directly on the live ledger before the fix:
//
//     TaskResumeTarget: raw="…/01a18d61-509f-…/01a18d61-509f-….raw.log" err=<nil>
//     TaskResumeSessionFor: SessionID="01a19075-…" Source="row-id" … (the OTHER
//     resolver already refused this substitution in 2026-10-28; the resume
//     TARGET resolver had not)
//
// THE RULE THE FIX ENFORCES, and the reason it lives in the resolver rather than
// in the resume command: a recovery must land on the row's own work or not run
// at all. A row with no run of its own has NO address; answering with the
// filer's conversation is not a weaker answer, it is a different question.
//
// These tests are HERMETIC (Rule 32): testutil.NewTestDBPath gives each one its
// own ledger, GOPI_HOME is pinned per test, and no live profile is touched.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/tasksource"
	"github.com/nick/gopi/internal/testutil"
)

const (
	// resumeFilerRowID is a row that NEVER RAN — the shape of 01a19075-80b5.
	resumeFilerRowID = "t-resume-filer-row"

	// resumeFilerSession is the session that FILED the row. It has a real
	// checkpoint on disk, which is what made the pre-fix substitution look like
	// a working recovery instead of a misdirection.
	resumeFilerSession = "c1c1c1c1-1111-4111-8111-c1c1c1c1c1c1"
)

// resumeFilerTestRoot pins GOPI_HOME at a per-test directory AND gives the test
// its own ledger (Rule 32), so both the sessions tree the resolver reads and the
// row it reads belong to this test.
func resumeFilerTestRoot(t *testing.T) {
	t.Helper()
	t.Setenv(config.GopiDirEnv, t.TempDir())
	testutil.NewTestDBPath(t)
}

// resumeFilerSeedRow files a row with an EMPTY session_id and a
// submitter_session naming the filer — the ledger shape that armed the defect.
func resumeFilerSeedRow(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close() //stylegate:ignore the fixture's own handle, closed as this helper returns
	if _, err := db.ExecContext(ctx, `INSERT INTO tasks
		(task_id, summary, status, session_id, `+taskSubmitterColumnName+`, model,
		 parent_task_id, tags, payload, source, deliverable, priority, reason,
		 created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		resumeFilerRowID, "a row that never ran", taskStatusFailed,
		"", resumeFilerSession, "",
		"", "", "", tasksource.Agent, "", 0, "", 1.0, 1.0); err != nil {
		t.Fatalf("insert row %s: %v", resumeFilerRowID, err)
	}
	return resumeFilerRowID
}

// resumeFilerSeedCheckpoint materialises a BARE session run directory with a
// checkpoint — the shape the filer's own conversation has, and the file the
// pre-fix resolver handed back as this row's resume address.
func resumeFilerSeedCheckpoint(t *testing.T, sessionID string) string {
	t.Helper()
	root := filepath.Join(gopiDataRoot(), config.SessionsDirName)
	dir := filepath.Join(root, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	raw := filepath.Join(dir, sessionID+scratch.SessionRawLogFileSuffix)
	if err := os.WriteFile(raw, []byte("→ REQUEST\n"), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	body := `{"type":"session_start","sessionId":"` + sessionID + `","rawLogPath":"` + raw + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, sessionID+taskSessionJSONLExt), []byte(body), 0o644); err != nil {
		t.Fatalf("write session log: %v", err)
	}
	return raw
}

// TestTaskResumeTargetRefusesTheFilerSessionForARowThatNeverRan is the RED of the
// defect: before the fix this arm prints the filer's checkpoint path and the
// ladder above it launches a resume onto another conversation.
func TestTaskResumeTargetRefusesTheFilerSessionForARowThatNeverRan(t *testing.T) {
	resumeFilerTestRoot(t)
	resumeFilerSeedRow(t)
	filerCheckpoint := resumeFilerSeedCheckpoint(t, resumeFilerSession)

	// THE PRECONDITION IS MEASURED, NOT ASSUMED: the filer's checkpoint really
	// exists, so a "" answer below means the resolver REFUSED it rather than
	// failing to find it.
	if _, err := os.Stat(filerCheckpoint); err != nil {
		t.Fatalf("fixture: the filer's checkpoint must exist for this test to mean anything: %v", err)
	}

	st, raw, err := TaskResumeTarget(resumeFilerRowID)
	if err != nil {
		t.Fatalf("TaskResumeTarget(%q): %v", resumeFilerRowID, err)
	}
	if st == nil {
		t.Fatalf("TaskResumeTarget(%q) returned no row view", resumeFilerRowID)
	}
	if raw != "" {
		t.Fatalf("TaskResumeTarget answered %q — a row with NO run of its own has no resume address; answering the session that FILED it (%q) continues another conversation and that conversation writes this row's status (the measured defect)",
			raw, resumeFilerSession)
	}
	if !st.SessionIsFiler {
		t.Errorf("the row view must SAY the session it names is the filer (session_is_filer), so a caller can refuse it by reason; got SessionID=%q SessionIsFiler=%v",
			st.SessionID, st.SessionIsFiler)
	}
	if st.SessionID != resumeFilerSession {
		t.Errorf("SessionID = %q, want the filer %q — the view reports the fact even while the resolver refuses to use it",
			st.SessionID, resumeFilerSession)
	}
}

// TestTaskResumeTargetStillAnswersARowsOwnRun is the NEGATIVE control: the fix
// must not refuse a row that DOES have a run of its own, or it would break every
// recovery that works today.
func TestTaskResumeTargetStillAnswersARowsOwnRun(t *testing.T) {
	resumeFilerTestRoot(t)
	resumeFilerSeedRow(t)
	// The filer's conversation exists (so a wrong answer is available) …
	filerRaw := resumeFilerSeedCheckpoint(t, resumeFilerSession)
	// … AND this row has one of its own, under its own run directory.
	const ownRun = "d2d2d2d2-2222-4222-8222-d2d2d2d2d2d2"
	root := filepath.Join(gopiDataRoot(), config.SessionsDirName, resumeFilerRowID)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	raw := filepath.Join(root, ownRun+scratch.SessionRawLogFileSuffix)
	if err := os.WriteFile(raw, []byte("→ REQUEST\n"), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	body := `{"type":"session_start","sessionId":"` + ownRun + `","rawLogPath":"` + raw + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(root, ownRun+taskSessionJSONLExt), []byte(body), 0o644); err != nil {
		t.Fatalf("write run log: %v", err)
	}

	_, got, err := TaskResumeTarget(resumeFilerRowID)
	if err != nil {
		t.Fatalf("TaskResumeTarget(%q): %v", resumeFilerRowID, err)
	}
	if got != raw {
		t.Fatalf("TaskResumeTarget = %q, want the row's OWN run checkpoint %q", got, raw)
	}
	// THE GUARANTEE THIS ARM EXISTS FOR: the answer is the row's own
	// checkpoint, NOT the filer's. (SessionIsFiler still reads true here —
	// and correctly so: it describes the COLUMN, "SessionID's value came from
	// the filer column, so it is not one of this row's runs", which is a fact
	// about the value and not about whether a run exists. The resolver reaches
	// its own-run answer BEFORE the flag is consulted, which is exactly why
	// this arm must pass with the flag set.)
	if got == filerRaw {
		t.Fatalf("TaskResumeTarget answered the FILER's checkpoint %q — a row with a run of its own must resolve to that run", filerRaw)
	}
}
