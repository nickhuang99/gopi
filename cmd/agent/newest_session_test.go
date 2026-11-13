package main

// newest_session_test.go — testsuite extension for the task-submit-model
// backfill seam (devdoc/task-submit-model.md ②). newestSessionJSONLIn lives
// in package main (moving it would widen the surface — Rule 3), so its
// selection algorithm is pinned here: newest-in-window, parent-session
// exclusion, pre-window filtering, zero-window, missing-dir soft-fail.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/signalrec"
)

func writeSessFileAt(t *testing.T, dir, name string, mod time.Time) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestNewestSessionJSONLInPicksNewestInWindow(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-time.Hour)
	writeSessFileAt(t, dir, "old.jsonl", base)
	writeSessFileAt(t, dir, "new.jsonl", base.Add(5*time.Minute))
	writeSessFileAt(t, dir, "mid.jsonl", base.Add(2*time.Minute))
	writeSessFileAt(t, dir, "notes.txt", base.Add(9*time.Minute)) // non-jsonl ignored
	if got := newestSessionJSONLIn(dir, false, "", base.Add(-time.Minute)); got != "new" {
		t.Fatalf("got %q, want new (newest .jsonl in window)", got)
	}
}

func TestNewestSessionJSONLInExcludesParentSession(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-time.Hour)
	writeSessFileAt(t, dir, "parent.jsonl", base.Add(9*time.Minute))
	writeSessFileAt(t, dir, "child.jsonl", base.Add(1*time.Minute))
	if got := newestSessionJSONLIn(dir, false, "parent.jsonl", base); got != "child" {
		t.Fatalf("got %q, want child (parent excluded even though newest)", got)
	}
}

func TestNewestSessionJSONLInWindowAndMissingDir(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-time.Hour)
	writeSessFileAt(t, dir, "pre.jsonl", base.Add(-10*time.Minute)) // before window
	writeSessFileAt(t, dir, "in.jsonl", base.Add(1*time.Minute))
	if got := newestSessionJSONLIn(dir, false, "", base); got != "in" {
		t.Fatalf("pre-window file picked: got %q, want in", got)
	}
	// Zero started time = any file qualifies (the defensive default).
	if got := newestSessionJSONLIn(dir, false, "", time.Time{}); got != "in" {
		t.Fatalf("zero window: got %q, want in", got)
	}
	// Missing dir is a soft "" — the backfill is best-effort by contract.
	if got := newestSessionJSONLIn(filepath.Join(dir, "nope"), false, "", base); got != "" {
		t.Fatalf("missing dir: got %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// bug054 early-stamp poll tests (waitChildSessionJSONL). The poll finds the
// child's session JSONL in the run window and returns its id — the same
// selection newestSessionJSONLIn pins, plus the timing ceiling: a session
// that appears AFTER taskSessionPollMax yields "" (the writeback's deferred
// backfill takes over — never a run blocker). GOPI_HOME sandbox: the poll
// resolves the sessions dir through config.GopiDir like the real seam.
// ---------------------------------------------------------------------------

// TestWaitChildSessionJSONLPicksChildInWindow: a session file appearing
// inside the run window is found and returned as the child session id.
func TestWaitChildSessionJSONLPicksChildInWindow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	sessDir := filepath.Join(home, ".gopi", "sessions")
	taskDir := filepath.Join(sessDir, "t-poll-test")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	// Appear just after the window opens — like the child's spawn. The S1
	// task-run child writes sessions/<task_id>/<run-uuid>.jsonl.
	time.Sleep(50 * time.Millisecond) //stylegate:allow-sleep mtime granularity: the two session files must differ in timestamp
	writeSessFileAt(t, taskDir, "child-window.jsonl", time.Now())
	if got := waitChildSessionJSONL(started, "t-poll-test"); got != "child-window" {
		t.Fatalf("poll: got %q, want child-window", got)
	}
}

// TestWaitChildSessionJSONLTimeout: no session file in the window → "" after
// the ceiling (the deferred writeback path). The ceiling is short (5s), so
// the test only proves the no-file fast path: with no entries at all the
// poll returns "" promptly.
func TestWaitChildSessionJSONLTimeout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	// No sessions dir at all — newestSessionJSONLIn soft-fails "".
	started := time.Now().Add(-time.Hour) // window is already closed... but
	// waitChildSessionJSONL's deadline is started+Max; with a past started
	// the first check already misses → immediate "" (no sleeping).
	if got := waitChildSessionJSONL(started, "t-poll-test"); got != "" {
		t.Fatalf("poll: got %q, want empty (no session in window)", got)
	}
}

// TestNewestSessionJSONLInSkipsSideFile is the session-RECORD regression for
// the model-backfill seam: in a BARE session dir (session.jsonl present), the
// provenance side file must never be picked as "the child's session" — the
// pre-fix "*.jsonl" suffix match would have returned "signal-provenance" for
// any window that contained it, stamping a session id that names no session.
func TestNewestSessionJSONLInSkipsSideFile(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-time.Hour)
	writeSessFileAt(t, dir, sessionlayout.RecordName, base.Add(time.Minute))
	writeSessFileAt(t, dir, signalrec.FileName, base.Add(9*time.Minute)) // newest, yet not a record

	// Excluding the record itself leaves nothing: the side file is not a
	// candidate, so "" (never "signal-provenance").
	if got := newestSessionJSONLIn(dir, false, sessionlayout.RecordName, base); got != "" {
		t.Fatalf("got %q, want \"\" — a provenance side file is not a session record", got)
	}
	// With the record eligible it is the one returned, despite the side
	// file's newer mtime.
	if got := newestSessionJSONLIn(dir, false, "", base); got != "session" {
		t.Fatalf("got %q, want session (the record, not the newer side file)", got)
	}
}

// TestNewestSessionJSONLAfterReadsOnlyTheRowsOwnRunDirectory is the RED-TO-GREEN
// pin for the WRITE-SOURCE half of epic 01a17df9, at the caller that hands the
// value to tools.TaskBackfillSession.
//
// THE MEASURED DEFECT: this seam used to fall back to the profile's sessions
// ROOT — the legacy FLAT layout — when the row's directory held nothing yet. A
// chat session is written CONTINUOUSLY, so inside any run window its flat
// record is NEWER than the run's own log, and the poll handed back the
// console's session id. Two worker-claimed rows were stamped with one chat
// session that way while their own sessions/<task-id>/ directories held the
// truth. The arm below reproduces exactly that shape: a flat chat record that is
// the newest thing under the profile, a row directory holding its own record,
// and a demand that the ROW'S OWN record be the answer.
func TestNewestSessionJSONLAfterReadsOnlyTheRowsOwnRunDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	root := filepath.Join(home, ".gopi", "sessions")
	rowDir := filepath.Join(root, "01a17abb-7de0-7479-a8a7-20340f489b5d")
	base := time.Now().Add(-time.Hour)
	if err := os.MkdirAll(rowDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The run's own record, written early in the window.
	writeSessFileAt(t, rowDir, "fa8048d0-9b19-4eee-a055-823de7e97569.jsonl", base)
	// The continuously written CHAT record at the legacy flat root — NEWER.
	writeSessFileAt(t, root, "07d98407-41a5-42db-875c-de0b077c790a.jsonl", base.Add(30*time.Minute))

	got := newestSessionJSONLAfter(base.Add(-time.Minute), "01a17abb-7de0-7479-a8a7-20340f489b5d")
	if got != "fa8048d0-9b19-4eee-a055-823de7e97569" {
		t.Fatalf("session = %q, want the row's OWN record (the newer flat chat record at the sessions root must never be adopted — that is the pollution)", got)
	}

	// A row directory with NO record answers "" — it never borrows the flat one.
	if bare := newestSessionJSONLAfter(base.Add(-time.Minute), "01a17da0-b73d-7c5f-bc67-7ca0fadf8149"); bare != "" {
		t.Fatalf("session = %q for a row with no record, want \"\" (the flat chat record is not this row's)", bare)
	}

	// And no task id at all answers "" rather than the flat root's newest.
	if none := newestSessionJSONLAfter(base.Add(-time.Minute), ""); none != "" {
		t.Fatalf("session = %q with no task id, want \"\"", none)
	}
}
