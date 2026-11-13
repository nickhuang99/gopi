package main

// Regression (epic resume-session-target-latch, 2026-10-28): `gopi session resume
// <task-id>` resolved the task id as if it were a session-log FILENAME.
//
// THE DEFECT, as it was measured before the fix. A row's run directory is named
// by the TASK id while the run logs inside it are named by the run's uuid
// (sessions/<task_id>/<run-uuid>.jsonl, measured). A resolver that stats
// sessions/<task_id>.jsonl — or that reads only the row's `session_id` COLUMN —
// therefore walks a path that cannot exist for the ordinary row, fails, and the
// caller's fallbacks then continued some OTHER session (the column is documented
// as "the session that SUBMITTED or executed the row", so for a row filed by a
// chat it names the chat). A recovery must land on the row's own work or not run
// at all.
//
// THE THREE CASES this file pins, one per shape the sessions tree produces:
//
//	(a) the row records an EXPLICIT --task session_id and its own run directory
//	    holds NO run log (the pre-layout / re-homed row): the column is the only
//	    pointer that exists, so the resume resolves through it.
//	(b) the row's run directory holds a run LOG named by a uuid (jsonl) whose
//	    co-located raw log is the checkpoint, with the session_id column EMPTY:
//	    the resolution must find that file through the directory, not the column.
//	(c) neither: no run log, no checkpoint, no session_id. The resume must FAIL
//	    with a non-zero exit AND create no new session — the
//	    silent-fresh-session half of the defect (Rule 15).
//
// TWO ROOTS, ONE FIXTURE: the in-process resolver reads the SESSIONS ROOT of the
// process's own profile (config.GopiDir — the suite pins it), while the child
// binary reads its GOPI_HOME. The fixture is therefore seeded into BOTH, which
// is what keeps each assertion honest about the side it measures (the trap the
// neighbouring resume fixtures document).
//
// RED BEFORE THE FIX: at the parent commit case (b) falls through to session-id
// resolution and reports "cannot resolve session id", and case (c) — with a
// session_id naming a submitter chat — resolves to that OTHER session and runs.
//
// Zero real API calls: the mock LLM answers the resumed round with plain text.
// Sandboxed GOPI_HOME / GOPI_MEMORY_DB; the suite pins the scratch root off the
// / partition (Rule 22).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/tools"
)

const (
	// resumeTaskTargetRowID is the fixture ROW (a task id) — never a uuid: the
	// whole point is that the resolver must map it to the row's run directory.
	resumeTaskTargetRowID = "t-resume-task-target"

	// resumeTaskTargetRunUUID is the RUN's id, i.e. the base name of the run
	// log and of its co-located raw log inside sessions/<task>/.
	resumeTaskTargetRunUUID = "9f9f9f9f-0000-1111-2222-333333333333"

	// resumeTaskTargetSubmitterSession is the session the row's `session_id`
	// COLUMN names in case (c): the submitter chat. It has a real run directory
	// with a real checkpoint, which is what made the pre-fix substitution look
	// like a success.
	resumeTaskTargetSubmitterSession = "c0c0c0c0-4444-5555-6666-777777777777"
)

// sessionsRootOf is the sessions directory of a profile's GOPI_HOME.
func sessionsRootOf(gopiHome string) string {
	return filepath.Join(gopiHome, config.ProfileDirName, config.SessionsDirName)
}

// processSessionsRoot is the sessions directory THIS process resolves — the one
// resolveResumeTarget reads in-process (config.GopiDir, pinned by the suite).
func processSessionsRoot(t *testing.T) string {
	t.Helper()
	gd, err := config.GopiDir()
	if err != nil {
		t.Fatalf("resolve this process's profile (%s): %v", config.GopiDirEnv, err)
	}
	return filepath.Join(gd, config.SessionsDirName)
}

// seedRunDirUnder writes ONE run log named by a uuid plus its co-located raw
// log (the checkpoint) into <root>/<runID>/, and registers its removal.
func seedRunDirUnder(t *testing.T, root, runID string) {
	t.Helper()
	dir := filepath.Join(root, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) }) //stylegate:ignore a fixture directory this test created, removed at its own end
	if err := os.WriteFile(filepath.Join(dir, resumeTaskTargetRunUUID+scratch.SessionRawLogFileSuffix),
		[]byte("→ REQUEST\n"+checkpointBody()+"\n"), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	rawJSON, err := json.Marshal(filepath.Join(dir, resumeTaskTargetRunUUID+scratch.SessionRawLogFileSuffix))
	if err != nil {
		t.Fatalf("marshal raw path: %v", err)
	}
	lines := []string{
		`{"type":"session_start","sessionId":"` + resumeTaskTargetRunUUID + `","model":"deepseek-v4-flash","rawLogPath":` + string(rawJSON) + `,"timestamp":"2026-09-01T00:00:00Z"}`,
		`{"type":"session_end","elapsed_sec":1.0,"exit_code":0}`,
	}
	if err := os.WriteFile(filepath.Join(dir, resumeTaskTargetRunUUID+".jsonl"),
		[]byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write session log: %v", err)
	}
}

// seedCheckpointlessRunDir writes a run dir holding a run LOG but no checkpoint
// — the row that ran and kept nothing a resume can continue.
func seedCheckpointlessRunDir(t *testing.T, root, runID string) {
	t.Helper()
	dir := filepath.Join(root, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) }) //stylegate:ignore a fixture directory this test created, removed at its own end
	if err := os.WriteFile(filepath.Join(dir, resumeTaskTargetRunUUID+".jsonl"),
		[]byte(`{"type":"session_end","elapsed_sec":1.0,"exit_code":0}`+"\n"), 0o644); err != nil {
		t.Fatalf("write session log: %v", err)
	}
}

// seedBareSessionAt materialises a BARE session run directory — the shape a
// chat/one-shot run has: <root>/<sessionID>/<sessionID>.jsonl plus the
// co-located raw log the record names. This is the layout the ledger's
// `session_id` COLUMN can actually be resolved through, which is why case (a)
// uses it and cases (b)/(c) use the task-id-named directory instead.
func seedBareSessionAt(t *testing.T, root, sessionID string) {
	t.Helper()
	dir := filepath.Join(root, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) }) //stylegate:ignore a fixture directory this test created, removed at its own end
	raw := filepath.Join(dir, sessionID+scratch.SessionRawLogFileSuffix)
	if err := os.WriteFile(raw, []byte("→ REQUEST\n"+checkpointBody()+"\n"), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	rawJSON, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal raw path: %v", err)
	}
	lines := []string{
		`{"type":"session_start","sessionId":"` + sessionID + `","model":"deepseek-v4-flash","rawLogPath":` + string(rawJSON) + `,"timestamp":"2026-09-01T00:00:00Z"}`,
		`{"type":"session_end","elapsed_sec":1.0,"exit_code":0}`,
	}
	if err := os.WriteFile(filepath.Join(dir, sessionID+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write session log: %v", err)
	}
}

// setRowSessionID writes the row's `session_id` COLUMN through the fixture's own
// ledger handle — the column the pre-fix resolver read INSTEAD of the run
// directory.
func setRowSessionID(t *testing.T, sessionID string) {
	t.Helper()
	dbPath := os.Getenv(config.MemoryDBEnv)
	db, err := openRawTasksDB(dbPath)
	if err != nil {
		t.Fatalf("open ledger %s: %v", dbPath, err)
	}
	defer db.Close() //stylegate:ignore the fixture's own handle, closed as this helper returns
	if _, err := db.Exec(`UPDATE tasks SET session_id = ? WHERE task_id = ?`,
		sessionID, resumeTaskTargetRowID); err != nil {
		t.Fatalf("set session_id: %v", err)
	}
}

// TestResumeTaskTargetResolution is the three-case regression the epic asks for.
func TestResumeTaskTargetResolution(t *testing.T) {
	// SUBTEST (a) — the column, for a row with no run directory of its own.
	t.Run("a_row_with_non_empty_session_id_resolves_to_that_session", func(t *testing.T) {
		gopiHome, _ := resumeLatchSandbox(t)
		seedRowIntoProfile(t, gopiHome, resumeTaskTargetRowID)
		// THE PRECONDITION IS ESTABLISHED, NOT ASSUMED: this case is about a row
		// whose OWN run directory does not exist, so any directory left under
		// sessions/<row-id> is removed first. It is not a hypothetical — the
		// suite's shared profile keeps a run directory behind when a case is
		// interrupted before its t.Cleanup runs (measured 2026-10-28: a killed
		// `go test` left sessions/t-resume-task-target in the shared suite home
		// and this case then failed with "holds no run whose raw log still
		// exists", i.e. it measured the residue instead of its own fixture).
		for _, root := range []string{processSessionsRoot(t), sessionsRootOf(gopiHome)} {
			if err := os.RemoveAll(filepath.Join(root, resumeTaskTargetRowID)); err != nil {
				t.Fatalf("clear the row's run directory under %s: %v — this case needs a row with NO run directory of its own", root, err)
			}
		}
		// The submitter's session lives in BOTH roots (see the file header).
		seedBareSessionAt(t, processSessionsRoot(t), resumeTaskTargetSubmitterSession)
		seedBareSessionAt(t, sessionsRootOf(gopiHome), resumeTaskTargetSubmitterSession)
		setRowSessionID(t, resumeTaskTargetSubmitterSession)

		got, err := resolveResumeTarget(resumeTaskTargetRowID)
		if err != nil {
			t.Fatalf("resolveResumeTarget(%q) errored: %v — the row's session_id is the only pointer that exists here",
				resumeTaskTargetRowID, err)
		}
		want := filepath.Join(processSessionsRoot(t), resumeTaskTargetSubmitterSession,
			resumeTaskTargetSubmitterSession+scratch.SessionRawLogFileSuffix)
		if got != want {
			t.Fatalf("resolveResumeTarget(%q) = %q, want %q", resumeTaskTargetRowID, got, want)
		}
		if !resumeTargetWasTaskID {
			t.Fatalf("a row-view resolution must be task-mode: resumeTargetWasTaskID=%v", resumeTargetWasTaskID)
		}
		if resumeTargetRowID != resumeTaskTargetRowID {
			t.Fatalf("the row view must answer for the ROW, got rowID=%q", resumeTargetRowID)
		}
	})

	// SUBTEST (b) — the UUID-NAMED run log inside a task-id-named directory,
	// with the column EMPTY. This is the shape the resolver could never reach
	// by stat-ing sessions/<task-id>.jsonl.
	t.Run("b_empty_session_id_but_a_uuid_named_run_log_in_the_rows_directory", func(t *testing.T) {
		gopiHome, env := resumeLatchSandbox(t)
		seedRowIntoProfile(t, gopiHome, resumeTaskTargetRowID)
		seedRunDirUnder(t, processSessionsRoot(t), resumeTaskTargetRowID)
		seedRunDirUnder(t, sessionsRootOf(gopiHome), resumeTaskTargetRowID)

		got, err := resolveResumeTarget(resumeTaskTargetRowID)
		if err != nil {
			t.Fatalf("resolveResumeTarget(%q) errored: %v — the row's own run directory holds the checkpoint",
				resumeTaskTargetRowID, err)
		}
		want := filepath.Join(processSessionsRoot(t), resumeTaskTargetRowID,
			resumeTaskTargetRunUUID+scratch.SessionRawLogFileSuffix)
		if got != want {
			t.Fatalf("resolveResumeTarget(%q) = %q, want %q", resumeTaskTargetRowID, got, want)
		}

		// ...and the REAL binary continues that run: the replayed checkpoint is
		// the row's own run's, named by its uuid.
		_, stderr, code := runGopiMapped(t,
			[]string{CliVerbSession, CliVerbResume, resumeTaskTargetRowID, resumeLatchContinuationFile(t)},
			"", env, "")
		if code != 0 {
			t.Fatalf("the row's own checkpoint must resume: code=%d\nstderr:\n%s", code, stderr)
		}
		if !strings.Contains(stderr, resumeTaskTargetRunUUID) {
			t.Errorf("the resume must continue the row's own run (%s), stderr:\n%s", resumeTaskTargetRunUUID, stderr)
		}
	})

	// SUBTEST (c) — neither pointer. A hard error, and NO new session.
	t.Run("c_neither_session_id_nor_a_checkpoint_exits_non_zero_and_starts_nothing", func(t *testing.T) {
		gopiHome, env := resumeLatchSandbox(t)
		seedRowIntoProfile(t, gopiHome, resumeTaskTargetRowID)
		// The row ran, kept no checkpoint; the column names ANOTHER session that
		// does have one — the exact substitution the pre-fix chain performed.
		seedCheckpointlessRunDir(t, processSessionsRoot(t), resumeTaskTargetRowID)
		seedCheckpointlessRunDir(t, sessionsRootOf(gopiHome), resumeTaskTargetRowID)
		seedBareSessionAt(t, processSessionsRoot(t), resumeTaskTargetSubmitterSession)
		seedBareSessionAt(t, sessionsRootOf(gopiHome), resumeTaskTargetSubmitterSession)
		setRowSessionID(t, resumeTaskTargetSubmitterSession)

		if _, err := resolveResumeTarget(resumeTaskTargetRowID); err == nil {
			t.Fatal("a row with no checkpoint of its own must NOT resolve — the pre-fix chain continued another session")
		} else if !strings.Contains(err.Error(), resumeTaskTargetRowID) {
			t.Errorf("the refusal must name the row, got: %v", err)
		}

		before := countSessionDirs(t, sessionsRootOf(gopiHome))
		_, stderr, code := runGopiMapped(t,
			[]string{CliVerbSession, CliVerbResume, resumeTaskTargetRowID, resumeLatchContinuationFile(t)},
			"", env, "")
		if code == 0 {
			t.Fatalf("an unresolvable row must exit non-zero, got 0\nstderr:\n%s", stderr)
		}
		if !strings.Contains(stderr, resumeTaskTargetRowID) {
			t.Errorf("the refusal must name the row on stderr, got:\n%s", stderr)
		}
		if after := countSessionDirs(t, sessionsRootOf(gopiHome)); after != before {
			t.Errorf("an unresolvable resume must start NO session: %d entries in the sessions root before, %d after",
				before, after)
		}
	})
}

// countSessionDirs counts the entries under a sessions root — the "did a fresh
// session appear?" measurement. A missing root counts as zero.
func countSessionDirs(t *testing.T, root string) int {
	t.Helper()
	subs, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read sessions root %s: %v", root, err)
	}
	return len(subs)
}

// TestTaskResumeTargetNeverSubstitutesTheSubmitterSession pins the resolution
// itself at its own seam (internal/tools, no subprocess): a row whose run
// directory holds run logs but no checkpoint must answer "" — never the
// column's session.
//
// It is the unit-level statement of the measurement the E2E case (c) makes
// through the binary: the column's documented meaning is "the session that
// SUBMITTED or executed the row", so substituting it hands the operator a
// different conversation.
func TestTaskResumeTargetNeverSubstitutesTheSubmitterSession(t *testing.T) {
	gopiHome, _ := resumeLatchSandbox(t)
	seedRowIntoProfile(t, gopiHome, resumeTaskTargetRowID)
	root := processSessionsRoot(t)
	seedCheckpointlessRunDir(t, root, resumeTaskTargetRowID)
	seedBareSessionAt(t, root, resumeTaskTargetSubmitterSession)
	setRowSessionID(t, resumeTaskTargetSubmitterSession)

	st, raw, err := tools.TaskResumeTarget(resumeTaskTargetRowID)
	if err != nil {
		t.Fatalf("TaskResumeTarget: %v", err)
	}
	if st == nil {
		t.Fatal("TaskResumeTarget returned no row view")
	}
	if raw != "" {
		t.Fatalf("TaskResumeTarget answered %q — it must NOT substitute the submitter session %q for a row that kept no checkpoint",
			raw, resumeTaskTargetSubmitterSession)
	}
}
