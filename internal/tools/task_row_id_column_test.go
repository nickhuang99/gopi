package tools

// epic: one task row, one id — the LEDGER half (deliverable ②).
//
// The row's session_id column is stamped from the row's OWN run directory
// (taskRunSessionFromRowDir, called by taskUpdate's deferred repair). This pins
// the two layouts that directory can be in:
//
//   - NEW  sessions/<task-id>/<task-id>.jsonl  → the column reads task_id
//   - OLD  sessions/<task-id>/<uuid>.jsonl     → the column reads the uuid it
//     always did (backward compatibility: the disk holds thousands of these,
//     and a reader must not start lying about them)
//
// and the ORDER rule when both exist: the most recently written record wins,
// which is sessionlayout.NewestRecordInDir — the ONE definition this package
// now shares with internal/gopilog instead of keeping a second loop.
//
// Zero subprocesses, zero profile contact: GOPI_HOME is a t.TempDir().

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nick/gopi/internal/sessionlayout"
)

const (
	rowDirTaskID = "01a17de9-e772-7470-80c1-f954f07c6b59"
	rowDirOldRun = "df8d3009-33a8-45b1-bd99-a7212fbe2b7a"
	rowDirNewRun = rowDirTaskID
)

// rowDirSessionRoot creates the isolated profile and returns its sessions root.
func rowDirSessionRoot(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	root := filepath.Join(home, ".gopi", "sessions")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir sessions root: %v", err)
	}
	return root
}

// rowDirWriteRecord writes a minimal session record and returns its path.
func rowDirWriteRecord(t *testing.T, path, sessionID string, mod time.Time) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	line := `{"type":"session_start","sessionId":"` + sessionID + `","model":"m","timestamp":"2026-10-27T00:00:00Z"}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
	return path
}

// rowDirBaseTime anchors the ordering assertions (Rule 1: a named value).
var rowDirBaseTime = time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)

// TestRowDirSessionIDReadsTheUnifiedLayout is the deliverable, measured: with
// the run's record named by the row, the column's value IS the row id — the
// fourth of the four values that must be one string.
func TestRowDirSessionIDReadsTheUnifiedLayout(t *testing.T) {
	root := rowDirSessionRoot(t)
	rowDirWriteRecord(t, filepath.Join(root, rowDirTaskID, rowDirNewRun+sessionlayout.JSONLSuffix), rowDirNewRun, rowDirBaseTime)

	if got := taskRunSessionFromRowDir(rowDirTaskID); got != rowDirTaskID {
		t.Errorf("taskRunSessionFromRowDir(%q) = %q, want the row id (directory = file = session_start = column)", rowDirTaskID, got)
	}
}

// TestRowDirSessionIDReadsTheLegacyLayout is the backward-compatibility half: a
// directory holding a minted-uuid record keeps answering that uuid, so nothing
// about the tree already on disk changes meaning.
func TestRowDirSessionIDReadsTheLegacyLayout(t *testing.T) {
	root := rowDirSessionRoot(t)
	rowDirWriteRecord(t, filepath.Join(root, rowDirTaskID, rowDirOldRun+sessionlayout.JSONLSuffix), rowDirOldRun, rowDirBaseTime)

	if got := taskRunSessionFromRowDir(rowDirTaskID); got != rowDirOldRun {
		t.Errorf("taskRunSessionFromRowDir(%q) = %q, want the record's own id %q", rowDirTaskID, got, rowDirOldRun)
	}
}

// TestRowDirSessionIDPrefersTheNewestRecord pins the ordering rule for a
// directory that holds BOTH: a killed run plus its retry, or the legacy record
// beside a unified one. The column names the run that wrote last.
func TestRowDirSessionIDPrefersTheNewestRecord(t *testing.T) {
	root := rowDirSessionRoot(t)
	dir := filepath.Join(root, rowDirTaskID)
	rowDirWriteRecord(t, filepath.Join(dir, rowDirOldRun+sessionlayout.JSONLSuffix), rowDirOldRun, rowDirBaseTime)
	rowDirWriteRecord(t, filepath.Join(dir, rowDirNewRun+sessionlayout.JSONLSuffix), rowDirNewRun, rowDirBaseTime.Add(time.Minute))

	if got := taskRunSessionFromRowDir(rowDirTaskID); got != rowDirNewRun {
		t.Errorf("taskRunSessionFromRowDir(%q) = %q, want the newest record's id %q", rowDirTaskID, got, rowDirNewRun)
	}
}

// TestRowDirSessionIDEmptyForAnAbsentRun is the absence: a row that never ran
// (no directory at all) and a directory holding only a side file both answer
// "nothing to stamp". A value there would put a wrong id in the column, which
// is worse than an empty one.
func TestRowDirSessionIDEmptyForAnAbsentRun(t *testing.T) {
	root := rowDirSessionRoot(t)
	sideOnly := filepath.Join(root, rowDirTaskID)
	if err := os.MkdirAll(sideOnly, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sideOnly, sessionlayout.SideFileNames()[0]), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write side file: %v", err)
	}

	if got := taskRunSessionFromRowDir(rowDirTaskID); got != "" {
		t.Errorf("side-file-only run dir: got %q, want no stamp", got)
	}
	if got := taskRunSessionFromRowDir("no-such-row"); got != "" {
		t.Errorf("absent run dir: got %q, want no stamp", got)
	}
}
