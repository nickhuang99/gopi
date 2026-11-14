package main

// Resume-path terminal-status E2E (epic resume-writes-terminal-status,
// 2026-09-22).
//
// THE OPERATOR'S REPORT, reproduced through the REAL binary: `gopi session resume
// <task-id>` runs to completion — exit 0, work committed, worktree reclaimed —
// but the row is NEVER re-closed, so it stays `failed` with `ended_at` frozen
// at the FIRST run's death forever. The cleanup half was fixed by the earlier
// resume-finish-cleanup epic; this file owns the STATUS half, and the fixture is
// the exact shape the report describes.
//
// The RED PROOF is this test itself: delete the AdoptLedgerRow call in
// runResume (or the latch in internal/agent/taskregister.go), rebuild bin/gopi,
// and TestResumeWritesTerminalStatus fails with the row still reading `failed`.
// A stale binary makes it pass falsely — rebuild first (the trap that fooled
// the resume-finish-cleanup epic's first red attempt).
//
// Zero real API calls: the mock LLM answers the resumed round with plain text,
// which the loop treats as the session's final answer. Sandboxed GOPI_HOME /
// GOPI_MEMORY_DB, t.TempDir() throughout (the suite pins the scratch root off
// the / partition — Rule 22).

import (
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// resumeStatusTaskID is the fixture row's id — a task id (NOT a session
	// UUID), which is what makes `gopi session resume <task-id>` take the task-target
	// branch (resolveResumeTarget).
	resumeStatusTaskID = "t-resume-status-e2e"

	// resumeStatusRunName is the fixture run's id (its log's base name); the
	// run directory is named by the ROW, the log by the run.
	resumeStatusRunName = "aaaaaaaa-1111-2222-3333-444444444444"

	// resumeDeadRunEndedAt is the dead run's ended_at, deliberately far in the
	// past so ANY write is visible as movement (the operator's "frozen" stamp).
	resumeDeadRunEndedAt = 1000000000.0

	// resumeDeadRunReason is the residue's reason text — it must survive a
	// refused run and be replaced by a real one otherwise.
	resumeDeadRunReason = "resume-status fixture: the first run died mid-flight"
)

// resumeRowStatus is the row facts the acceptance asserts on: the terminal
// status and the ended_at stamp the operator watches for MOVEMENT.
type resumeRowStatus struct {
	status  string
	endedAt sql.NullFloat64
	reason  string
}

// readResumeStatusRow reads the fixture row through its own database handle.
func readResumeStatusRow(t *testing.T, dbPath string) resumeRowStatus {
	t.Helper()
	db, err := openRawTasksDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close() //stylegate:ignore the fixture's own handle, closed when this reader returns
	var st resumeRowStatus
	if err := db.QueryRow(
		`SELECT status, ended_at, reason FROM tasks WHERE task_id = ?`, resumeStatusTaskID,
	).Scan(&st.status, &st.endedAt, &st.reason); err != nil {
		t.Fatalf("read row: %v", err)
	}
	return st
}

// seedResumeRow creates the row the dead run left behind: FAILED (the residue
// shape), with an ended_at from that death — the stamp the fix must move.
func seedResumeRow(t *testing.T, dbPath, summary string) {
	t.Helper()
	// THE ROW'S DELIVERABLE REPO IS PINNED (Rule 29 — measured 2026-10-28).
	// With no Repo, TaskSubmit stamps THIS PROCESS'S CWD as deliverable.repo, and
	// the resumed child's close-out then restores that path as the main checkout:
	// `git checkout master` in whatever checkout the suite is running in. Trapped
	// by a post-checkout hook naming `gopi session resume t-resume-status-e2e` as
	// the ancestor of the `git checkout master` (20:07:16, this run's own ground).
	// The fixture owns its ground instead — the sibling helper
	// seedRowIntoProfile's pin, asserted there through `task show`.
	repo := resumeLatchFixtureRepo(t)
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: summary,
		Payload: "finish the deliverable",
		TaskID:  resumeStatusTaskID,
		Source:  "human",
		Tags:    "e2e",
		Repo:    repo,
	}); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	db, err := openRawTasksDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// A terminal row with a real ended_at: exactly what `gopi task show`
	// rendered as "ended" while the operator knew the resume had finished.
	if _, err := db.Exec(
		`UPDATE tasks SET status = 'failed', ended_at = ?, reason = ? WHERE task_id = ?`,
		resumeDeadRunEndedAt, resumeDeadRunReason, resumeStatusTaskID,
	); err != nil {
		t.Fatalf("shape row: %v", err)
	}
	db.Close() //stylegate:ignore the fixture's own handle, closed at the end of the seed
}

// writeResumeRunLog mints the row's run directory and the session log the resume
// target resolution reads: <gopi dir>/sessions/<task_id>/<run>.jsonl, with the
// run's RAW LOG (the checkpoint) beside it — rundir's per-run source.
func writeResumeRunLog(t *testing.T, gopiHome string) {
	t.Helper()
	runDir := filepath.Join(gopiHome, config.ProfileDirName, config.SessionsDirName, resumeStatusTaskID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rawPath := filepath.Join(runDir, resumeStatusRunName+"-raw.log")
	if err := os.WriteFile(rawPath, []byte("→ REQUEST\n"+checkpointBody()+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rawJSON, err := json.Marshal(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"type":"session_start","sessionId":"11111111-2222-3333-4444-555555555555","model":"deepseek-v4-flash","rawLogPath":` + string(rawJSON) + `,"timestamp":"2026-09-01T00:00:00Z"}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"finish the deliverable"}]}}`,
	}
	// The FIRST run did reach its end (exit 0) — the subtle residue shape:
	// the run's cleanup was fixed, its STATUS was never written.
	lines = append(lines, `{"type":"session_end","elapsed_sec":12.5,"total_iterations":3,"total_tokens":4242,"exit_code":0}`)
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(runDir, resumeStatusRunName+sessionlayout.JSONLSuffix), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// checkpointBody is the checkpoint payload the resume loader requires: the LAST
// "→ REQUEST" line carries a byte-exact full-context request.
func checkpointBody() string {
	return `{"model":"deepseek-v4-flash","max_tokens":16384,"messages":[` +
		`{"role":"user","content":[{"type":"text","text":"[EPIC GOAL]\nfinish the deliverable"}]},` +
		`{"role":"assistant","content":[{"type":"text","text":"working"}]}]}`
}

// resumeStatusSandbox builds the shared sandbox for the cases: a temp GOPI_HOME,
// a temp ledger, and a mock LLM whose plain-text answer ends the resumed session.
func resumeStatusSandbox(t *testing.T, text string) (dbPath, gopiHome string, env []string) {
	t.Helper()
	gopiHome = t.TempDir()
	dbPath = filepath.Join(t.TempDir(), "memory.db")
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: text})
	t.Cleanup(mock.Close)
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	env = testutil.MockEnv(t, gopiHome, mock.URL())
	return dbPath, gopiHome, append(env, "GOPI_MEMORY_DB="+dbPath, config.SkipClaimBroadcastEnv+"=1")
}

// TestResumeWritesTerminalStatus is the epic's headline acceptance, driven
// through the REAL binary and the REAL resume entry point: after
// `gopi session resume <task-id>` the row reads `completed` and its ended_at MOVED off
// the dead run's stamp.
//
// RED BEFORE THE FIX: without the adopt call site the row keeps the dead run's
// residue — status `failed`, ended_at 1000000000 — no matter how well the resume
// itself ran.
func TestResumeWritesTerminalStatus(t *testing.T) {
	dbPath, gopiHome, env := resumeStatusSandbox(t, "the deliverable is finished")
	seedResumeRow(t, dbPath, "resume status e2e")
	writeResumeRunLog(t, gopiHome)

	before := readResumeStatusRow(t, dbPath)
	if before.status != tools.TaskStatusFailed || !before.endedAt.Valid || before.endedAt.Float64 != resumeDeadRunEndedAt {
		t.Fatalf("fixture: expected the dead run's residue, got %+v", before)
	}

	stdout, stderr, code := runGopiMapped(t, []string{CliVerbSession, CliVerbResume, resumeStatusTaskID}, "", env, "")
	if code != 0 {
		t.Fatalf("resume must succeed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	after := readResumeStatusRow(t, dbPath)
	if after.status != tools.TaskStatusCompleted {
		t.Fatalf("a successful resume must close its row %s, got %q (the row still carries the dead run's verdict)\nstdout:\n%s\nstderr:\n%s",
			tools.TaskStatusCompleted, after.status, stdout, stderr)
	}
	if !after.endedAt.Valid || after.endedAt.Float64 <= before.endedAt.Float64 {
		t.Fatalf("ended_at must MOVE to this run's end (before=%v after=%v)",
			before.endedAt.Float64, after.endedAt)
	}
}

// TestResumeFailedRunLandsFailed is the honest-outcome half: a resume whose own
// run FAILS must land the row `failed` with THIS run's end stamp — never
// `completed`, and never left at the shape the previous run left.
//
// The residue here is in_progress (the commonest zombie shape: a run killed
// between its start and its end hook), so the write is observable as real
// movement rather than as a same-status no-op.
func TestResumeFailedRunLandsFailed(t *testing.T) {
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	// Fail=true makes the mock answer HTTP 400 — non-retryable, so the agent
	// surfaces a hard error and the run exits non-zero.
	mock := testutil.NewMockLLM(t, testutil.Resp{Fail: true})
	t.Cleanup(mock.Close)
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env, "GOPI_MEMORY_DB="+dbPath, config.SkipClaimBroadcastEnv+"=1")

	seedResumeRow(t, dbPath, "resume status e2e (failing)")
	writeResumeRunLog(t, gopiHome)
	// Reshape the residue into the died-mid-flight shape: still in_progress,
	// never ended.
	db, err := openRawTasksDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(`UPDATE tasks SET status = 'in_progress', ended_at = NULL WHERE task_id = ?`, resumeStatusTaskID); err != nil {
		t.Fatalf("reshape row: %v", err)
	}
	db.Close() //stylegate:ignore the fixture's own handle, closed at the end of the fixture

	before := readResumeStatusRow(t, dbPath)
	_, stderr, code := runGopiMapped(t, []string{CliVerbSession, CliVerbResume, resumeStatusTaskID}, "", env, "")
	if code == 0 {
		t.Fatalf("a failing resume must exit non-zero\nstderr:\n%s", stderr)
	}
	after := readResumeStatusRow(t, dbPath)
	if after.status != tools.TaskStatusFailed {
		t.Fatalf("a FAILED resume must land the row %s (never %s), got %q\nstderr:\n%s",
			tools.TaskStatusFailed, tools.TaskStatusCompleted, after.status, stderr)
	}
	if before.endedAt.Valid == after.endedAt.Valid && (!after.endedAt.Valid || after.endedAt.Float64 == before.endedAt.Float64) {
		t.Fatalf("the failed run's end must be stamped on the row (before=%+v after=%+v)", before, after)
	}
	if !after.endedAt.Valid {
		t.Fatalf("a terminal row carries ended_at, got %+v", after)
	}
}

// TestResumeWritesStatusWithoutForcingCleanup pins the two halves as
// independent: the resume succeeds, its close-out runs, and the row's
// deliverable is still refused/there — a close-out must never be a deletion.
//
// (The cleanup refusals are owned by cli_resume_cleanup_e2e_test.go's
// TestResumeKeepsADirtyWorktree; this test pins the STATUS side of the same run.)
func TestResumeWritesStatusWithoutForcingCleanup(t *testing.T) {
	dbPath, gopiHome, env := resumeStatusSandbox(t, "done")
	seedResumeRow(t, dbPath, "resume status e2e (deliverable present)")
	writeResumeRunLog(t, gopiHome)

	// The deliverable must name a REAL ground: the resume now resolves it
	// through the ONE channel-aware judge, and a bare t.TempDir() is not a work
	// tree at all (that test lives in cli_resume_worktree_test.go). This fixture
	// is a CLONE-channel workspace — the shape the resume path could not even
	// describe before, and the shape it must now enter.
	kept := cloneGroundFixture(t)
	blob := `{"repo":"` + kept + `","branch":"feat/kept","worktree":"` + kept + `"}`
	db, err := openRawTasksDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(`UPDATE tasks SET deliverable = ? WHERE task_id = ?`, blob, resumeStatusTaskID); err != nil {
		t.Fatalf("shape deliverable: %v", err)
	}
	db.Close() //stylegate:ignore the fixture's own handle, closed at the end of the fixture

	_, stderr, code := runGopiMapped(t, []string{CliVerbSession, CliVerbResume, resumeStatusTaskID}, "", env, "")
	if code != 0 {
		t.Fatalf("resume must succeed (cleanup is fail-soft): code=%d stderr=%s", code, stderr)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("the cleanup chain must not force-remove a directory it cannot verify: %v", err)
	}
	if info, err := os.Stat(filepath.Join(kept, ".git")); err != nil || !info.IsDir() {
		t.Fatalf("the fixture's ground must be a CLONE (.git a directory): err=%v", err)
	}
	// THE CHANNEL-AWARE ENTRY, as the operator sees it: the landing notice names
	// the ground it entered and WHICH MODEL it is (the clone workspace was the
	// shape this face could not describe). t.Logf keeps the actual text in the
	// run's evidence stream.
	t.Logf("resume stderr:\n%s", stderr)
	if !strings.Contains(stderr, kept) {
		t.Errorf("the resume must name the ground it entered (%s):\n%s", kept, stderr)
	}
	if !strings.Contains(stderr, "model clone") {
		t.Errorf("the landing notice must name the ground MODEL:\n%s", stderr)
	}
	if after := readResumeStatusRow(t, dbPath); after.status != tools.TaskStatusCompleted {
		t.Fatalf("the status write is independent of the cleanup verdict: got %q", after.status)
	}
}

// TestResumeRefusedLeavesTheRowAlone is the "a refused --task resume leaves the
// row exactly as it was" acceptance. Two refusals are possible and this test
// drives the SECOND one, which is the one a --task resume actually meets:
//
//   - the process-identity gate (a verified-LIVE holder) refuses and exits 1 —
//     but it refuses on the ROW'S TERMINAL STATUS first (`failed` → not
//     resumable), so a residue row never reaches it; the live-holder shape
//     belongs to cli_resume_gate_test.go, whose fixture is an in_progress row.
//   - the TARGET resolution: `gopi session resume <task-id>` on a row whose run wrote no
//     checkpoint at all cannot resume it. This is the case the acceptance asks
//     about — "a refused or dying --task resume leaves the row exactly as it
//     was" — and it is the one that exercises the new adopt/latch code: the
//     refusal happens before any agent exists, so no close-out may fire.
func TestResumeRefusedLeavesTheRowAlone(t *testing.T) {
	dbPath, gopiHome, env := resumeStatusSandbox(t, "done")
	seedResumeRow(t, dbPath, "resume status e2e (refused)")
	writeResumeRunLog(t, gopiHome)

	// Delete the only checkpoint: the row's run directory now holds a session
	// log but no raw log, so there is nothing to resume FROM.
	rawPath := filepath.Join(gopiHome, config.ProfileDirName, config.SessionsDirName, resumeStatusTaskID, resumeStatusRunName+"-raw.log")
	if err := os.Remove(rawPath); err != nil {
		t.Fatal(err)
	}
	before := readResumeStatusRow(t, dbPath)

	_, stderr, code := runGopiMapped(t, []string{CliVerbSession, CliVerbResume, resumeStatusTaskID}, "", env, "")
	if code == 0 {
		t.Fatalf("a resume with no recoverable checkpoint must refuse, got exit 0\nstderr:\n%s", stderr)
	}
	after := readResumeStatusRow(t, dbPath)
	if after.status != before.status || after.endedAt.Float64 != before.endedAt.Float64 || after.reason != before.reason {
		t.Fatalf("a refused resume must leave the row EXACTLY as it was: before=%+v after=%+v\nstderr:\n%s", before, after, stderr)
	}
}

// cloneGroundFixture builds a standalone clone (the clone channel's workspace
// shape: `.git` a DIRECTORY, registered in nobody's worktree list) and returns
// its path. It is the fixture the resume path must ENTER — and the one the
// worktree-era reading could not see as a ground at all.
func cloneGroundFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	clone := filepath.Join(root, "workspace")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		// testutil.GitEnv STRIPS the inherited repository routing (GIT_DIR and
		// friends) before pinning the fixture identity — `cmd.Dir` alone does
		// not override GIT_DIR, so a suite started from a tree that exports it
		// would drive these commits into the WRONG repository (the standards
		// gate Rule 13/15 exists for exactly that).
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run(root, "init", src)
	run(src, "config", "user.email", "t@t")
	run(src, "config", "user.name", "t")
	run(src, "checkout", "-b", "master")
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(src, "add", "a.txt")
	run(src, "commit", "-m", "init")
	run(root, "clone", "-q", src, clone)
	run(clone, "checkout", "-b", "feat/kept")
	return clone
}

// TestResumeRefusesAGoneGroundThroughTheCLI drives the REFUSAL through the real
// binary — the exit path an operator actually meets:
//
//	gopi session resume <task-id>   →   exit 1, nothing runs, the message names
//	                                    the recorded path, the model verdict, the
//	                                    judge's detail and a repair command
//
// No LLM call is made: the refusal happens before any agent exists, which is the
// point (the pre-fix face printed one warning line and ran the whole session in
// the caller's cwd).
func TestResumeRefusesAGoneGroundThroughTheCLI(t *testing.T) {
	dbPath, gopiHome, env := resumeStatusSandbox(t, "must never be reached")
	seedResumeRow(t, dbPath, "resume refuses a gone ground")
	writeResumeRunLog(t, gopiHome)

	gone := filepath.Join(t.TempDir(), "the-ground-that-was-swept")
	blob := `{"repo":"` + filepath.Dir(gone) + `","branch":"feat/gone","worktree":"` + gone + `"}`
	db, err := openRawTasksDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(`UPDATE tasks SET deliverable = ? WHERE task_id = ?`, blob, resumeStatusTaskID); err != nil {
		t.Fatalf("shape deliverable: %v", err)
	}
	db.Close() //stylegate:ignore the fixture's own handle, closed at the end of the fixture

	stdout, stderr, code := runGopiMapped(t, []string{CliVerbSession, CliVerbResume, resumeStatusTaskID}, "", env, "")
	t.Logf("resume exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	if code == 0 {
		t.Fatalf("a row whose recorded ground is GONE must exit non-zero\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	for _, want := range []string{
		"REFUSING to resume",
		gone,
		"model verdict:   missing",
		"has no .git entry",
		"repair:",
		"gopi task show " + resumeStatusTaskID,
		config.ResumeAllowMissingGroundEnv,
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the refusal must carry %q:\n%s", want, stderr)
		}
	}
	// A refused resume does not get far enough to run: the row keeps the dead
	// run's residue rather than gaining a completion.
	if after := readResumeStatusRow(t, dbPath); after.status != tools.TaskStatusFailed {
		t.Errorf("a refused resume must leave the row alone, got %q", after.status)
	}
}
