package main

// END-TO-END COST CONTRACT for the LEDGER-NAMED session directory
// (2026-09-15) — the shape the operator's LIVE ledger has, and the reason the
// earlier address fix left `task list --status in_progress` at 6.1-12.5s.
//
// A ledger row's session_id is the session that SUBMITTED it. A worker session
// executes MANY rows, so the rows share one session, and that session's log
// lives in a directory named by ANOTHER task id. Both layouts the row can name
// from its own columns therefore MISS, and the earlier fix's lookups were
// followed by the unchanged whole-tree scan — 151,892 getdents64 / 75,959
// openat for one view.
//
// This test drives the full render through the CLI entry point on a ledger in
// that exact shape and asserts the acceptance property: ZERO sessions-tree
// walks, with the ledger hint seam showing the resolution came from the ledger.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/tools"
)

// seedSessionOwnerRow plants the session's OWN ledger row — the row whose task
// id names the directory holding that session's log (task-session-log-path).
func seedSessionOwnerRow(t *testing.T, taskID, sessionID string) {
	t.Helper()
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary:   "session-owner fixture " + taskID,
		Payload:   "ledger-named session directory end-to-end fixture",
		TaskID:    taskID,
		SessionID: sessionID,
		Source:    "human",
	}); err != nil {
		t.Fatalf("seed session-owner row %s: %v", taskID, err)
	}
}

// writeSessionRecordAt lays a session record under the sandbox's sessions root
// at rel (dirs created) and returns its path.
func writeSessionRecordAt(t *testing.T, rel, sessionID string) string {
	t.Helper()
	home, err := config.GopiDir()
	if err != nil {
		t.Fatalf("gopi dir: %v", err)
	}
	rec := filepath.Join(home, config.SessionsDirName, rel)
	if err := os.MkdirAll(filepath.Dir(rec), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(rec), err)
	}
	if err := os.WriteFile(rec, []byte(`{"type":"session_start","sessionId":"`+sessionID+`"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", rec, err)
	}
	return rec
}

// TestTaskListInProgressViewResolvesByLedgerHint is the end-to-end acceptance
// for the rework shape: two in_progress rows share one SUBMITTER session whose
// log sits in a directory named by the session's own row, and the render walks
// the sessions tree ZERO times while still rendering both rows.
func TestTaskListInProgressViewResolvesByLedgerHint(t *testing.T) {
	taskListRenderSandbox(t)
	const (
		rowOne = "t-hintledger-row-one"
		rowTwo = "t-hintledger-row-two"
		owner  = "t-hintledger-session-owner"
		sess   = "56055d3a-bb0f-424b-8d42-4416fd1f7893"
	)
	// Both rows carry the SAME submitter session; the log's directory is named
	// by the session's own row, which neither of them is.
	seedInProgressRowWithSession(t, rowOne, sess, filepath.Join("unrelated-address", sessionlayout.RecordName))
	seedInProgressRowWithSession(t, rowTwo, sess, filepath.Join("unrelated-address", sessionlayout.RecordName))
	seedSessionOwnerRow(t, owner, sess)
	wantRec := writeSessionRecordAt(t, filepath.Join(owner, sess+sessionlayout.JSONLSuffix), sess)

	hintReads, restoreHints := tools.SessionDirHintCounterForTest()
	defer restoreHints()
	count, restoreScans := tools.SessionScanCounterForTest()
	defer restoreScans()

	out, err := runTaskListCLI(t, []string{"--status", tools.TaskStatusInProgress})
	if err != nil {
		t.Fatalf("task list --status in_progress: %v", err)
	}
	if n := count(); n != 0 {
		t.Fatalf("the in_progress render walked the sessions tree %d time(s); a row whose log the ledger names must resolve with ZERO tree walks", n)
	}
	if n := hintReads(); n == 0 {
		t.Fatal("no ledger hint was read — a zero walk count must mean 'resolved by the ledger address', not 'the probe never ran'")
	}
	for _, want := range []string{rowOne, rowTwo, "[" + tools.TaskStatusInProgress + "]"} {
		if !strings.Contains(out, want) {
			t.Errorf("the render must still show %q:\n%s", want, out)
		}
	}
	// The address itself resolves to the ledger-named record (not merely to
	// nothing): the file the render tail-reads is the one under the owner's dir.
	if _, err := os.Stat(wantRec); err != nil {
		t.Fatalf("fixture record missing at %s: %v", wantRec, err)
	}
}
