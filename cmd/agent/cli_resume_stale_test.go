package main

// The resume path no longer sweeps (2026-11-05).
//
// WHAT THIS FILE USED TO PIN (M4.8 ②): a resumed instance swept stale rows as
// its FIRST act — a zombie in_progress row was reported on the recovery path
// too, "without any task run or serve daemon". That framing is exactly what the
// operator's ruling retired: one process reporting another process's health.
// The whole carrier is deleted (the detector, `task list --sweep`, the persisted
// throttle stamp, the re-arm UPDATE and both prologues — `task run` and
// `session resume`), so the assertion is now the INVERSE and it is a stronger
// one: the recovery path is a PURE READ. It touches no row and fires no event.
//
// The test keeps its fixture shape on purpose — a backdated in_progress row with
// a DEAD claim pid is the most stale-looking row constructible here, so "no
// event was written" cannot be dismissed as "there was nothing to report".

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// TestResumePathDoesNotSweep is the inverse of the retired
// TestResumeSweepsStaleRows: the fixture is maximally stale, and the recovery
// path must still write NOTHING.
func TestResumePathDoesNotSweep(t *testing.T) {
	_, path := sandboxProfileLedger(t)
	host, err := os.Hostname()
	if err != nil {
		t.Skipf("no hostname: %v", err)
	}

	// kill -9 reproduction at the resume boundary: a claimed row whose pid is
	// dead is the shape the deleted sweep existed to report.
	zombiePid := testutil.StartReapedProcess(t, testutil.MockDaemonBinary, testutil.MockDaemonHoldSeconds)

	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "resume-path zombie fixture",
		Payload: "resume e2e fixture (the sweep it used to check is deleted)",
		TaskID:  "t-resume-zombie",
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	db, err := openRawTasksDB(path)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour).Unix()
	if _, err := db.Exec(`UPDATE tasks SET status = 'in_progress', claimed_by = ?, created_at = ?, updated_at = ? WHERE task_id = 't-resume-zombie'`,
		host+":"+itoaPID(zombiePid), old, old); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	var beforeStatus string
	var beforeUpdated float64
	if err := db.QueryRow(`SELECT status, updated_at FROM tasks WHERE task_id = 't-resume-zombie'`).
		Scan(&beforeStatus, &beforeUpdated); err != nil {
		t.Fatalf("read the row before the run: %v", err)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	// A notify sink that WOULD receive an event: the absence of the file is
	// then a statement about the code path, not about the config.
	log := filepath.Join(t.TempDir(), "notify.log")
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat >> "+log)
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", "all")
	t.Setenv("GOPI_TASK_NOTIFY_ASYNC", "0")
	t.Setenv("GOPI_TASK_NOTIFY_WEBHOOK", "") // hermetic: the dev shell may export a real webhook

	// A checkpointless raw log: runResume bails with 1 AFTER whatever prologue
	// it has — the shape that let the old test observe the sweep's side effect.
	rawLog := filepath.Join(t.TempDir(), "gopi-raw-resume-test.log")
	if err := os.WriteFile(rawLog, []byte("not a checkpoint\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runResume([]string{rawLog}); code != 1 {
		t.Fatalf("checkpointless log must exit 1, got %d", code)
	}

	// ① no event reached the sink — not a "stale" one, not any one.
	if body, err := os.ReadFile(log); err == nil {
		if strings.Contains(string(body), `"event":"stale"`) {
			t.Fatalf("the recovery path must not report stale rows — that sweep is deleted "+
				"(the row it reported is still the fixture's business, not resume's):\n%s", body)
		}
		if strings.TrimSpace(string(body)) != "" {
			t.Errorf("the recovery path must fire NO event at all, got:\n%s", body)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("read the notify sink: %v — an unreadable sink is not a passing assertion", err)
	}

	// ② no ledger row was WRITTEN. This is the half that catches a "sweep"
	// reintroduced as a quiet status/updated_at bump: a pure read leaves the
	// row byte-identical, and the deleted re-arm UPDATE was exactly such a
	// bump (it touched updated_at while keeping the status).
	db2, err := openRawTasksDB(path)
	if err != nil {
		t.Fatalf("re-open raw db: %v", err)
	}
	defer db2.Close() //stylegate:ignore the test's own handle
	var afterStatus string
	var afterUpdated float64
	if err := db2.QueryRow(`SELECT status, updated_at FROM tasks WHERE task_id = 't-resume-zombie'`).
		Scan(&afterStatus, &afterUpdated); err != nil {
		t.Fatalf("read the row after the run: %v", err)
	}
	if afterStatus != beforeStatus || afterUpdated != beforeUpdated {
		t.Fatalf("the recovery path MUTATED the ledger row: status %q→%q, updated_at %v→%v — "+
			"a read verb must not rewrite a row (the sweep's re-arm UPDATE is deleted)",
			beforeStatus, afterStatus, beforeUpdated, afterUpdated)
	}
}

// TestResumeHookDoesNotDisturbResolution still passes now that the sweep
// prologue is gone — the arg resolution the resume path depends on is
// unchanged by the deletion (full coverage lives in cli_resume_test.go).
func TestResumeHookDoesNotDisturbResolution(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gopi-raw-hook.log")
	if err := os.WriteFile(p, []byte("→ REQUEST\n{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveResumeLogPath(p)
	if err != nil || got != p {
		t.Fatalf("resolveResumeLogPath broke after the sweep prologue was deleted: %q err=%v", got, err)
	}
}
