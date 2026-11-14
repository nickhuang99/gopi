package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// ownLogProfile pins a PRIVATE profile and returns its sessions root, the
// ledger DB and the row-layout helper (Rule 32: the DB comes from the
// scaffold; the profile from the scratch tree, Rule 22).
func ownLogProfile(t *testing.T) (sessionsRoot string) {
	t.Helper()
	home := testutil.TestScratchDir(t, "own-log-home")
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv("HOME", home)
	return filepath.Join(home, config.ProfileDirName, config.SessionsDirName)
}

// ownLogRecord writes a record whose session_start names sessionID and taskID.
func ownLogRecord(t *testing.T, path, sessionID, taskID string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	line := `{"type":"session_start","sessionId":"` + sessionID + `"`
	if taskID != "" {
		line += `,"taskId":"` + taskID + `"`
	}
	line += "}\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestTaskOwnSessionLogRowRunDirectory: SHAPE 1 — the row's own directory holds
// the run record (named by the run's uuid), and the ROW id is the operand. This
// is the shape the task id could never name as a file, i.e. the defect.
func TestTaskOwnSessionLogRowRunDirectory(t *testing.T) {
	root := ownLogProfile(t)
	const rowID = "row-aaaaaaaa"
	rec := filepath.Join(root, rowID, "run-uuid-1111"+sessionlayout.JSONLSuffix)
	ownLogRecord(t, rec, "run-uuid-1111", rowID)

	got, err := TaskOwnSessionLog(rowID)
	if err != nil {
		t.Fatalf("TaskOwnSessionLog(%s): %v", rowID, err)
	}
	if got.Path != rec {
		t.Errorf("Path = %q, want the run directory's own record %q", got.Path, rec)
	}
	if got.Source != TaskOwnLogSourceRowRunDir {
		t.Errorf("Source = %q, want %q", got.Source, TaskOwnLogSourceRowRunDir)
	}
	if got.Note != "" {
		t.Errorf("Note = %q, want none for the strong answer", got.Note)
	}
}

// TestTaskOwnSessionLogNewestRecordWins: several serial runs in one directory
// (the killed-run-plus-resume shape) resolve to the NEWEST record, never to a
// readdir-first guess.
func TestTaskOwnSessionLogNewestRecordWins(t *testing.T) {
	root := ownLogProfile(t)
	const rowID = "row-bbbbbbbb"
	dir := filepath.Join(root, rowID)
	older := filepath.Join(dir, "run-1"+sessionlayout.JSONLSuffix)
	newer := filepath.Join(dir, "run-2"+sessionlayout.JSONLSuffix)
	ownLogRecord(t, older, "run-1", rowID)
	ownLogRecord(t, newer, "run-2", rowID)
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(older, past, past); err != nil {
		t.Fatalf("chtimes older: %v", err)
	}

	got, err := TaskOwnSessionLog(rowID)
	if err != nil {
		t.Fatalf("TaskOwnSessionLog: %v", err)
	}
	if got.Path != newer {
		t.Errorf("Path = %q, want the newest record %q (not %q)", got.Path, newer, older)
	}
}

// TestTaskOwnSessionLogSessionColumn: SHAPE 2 — the row's own directory is gone
// (the re-homed run, the shape 223 live rows have) and the ledger's session_id
// column names the record. The answer is that record, ANNOTATED, because its
// ownership rests on the column alone.
func TestTaskOwnSessionLogSessionColumn(t *testing.T) {
	root := ownLogProfile(t)
	const rowID = "row-cccccccc"
	const col = "col-session-2222"
	if _, err := TaskNew("column row", rowID, col, ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	rec := filepath.Join(root, col, sessionlayout.RecordName)
	ownLogRecord(t, rec, col, "")

	got, err := TaskOwnSessionLog(rowID)
	if err != nil {
		t.Fatalf("TaskOwnSessionLog: %v", err)
	}
	if got.Path != rec {
		t.Errorf("Path = %q, want the column's record %q", got.Path, rec)
	}
	if got.Source != TaskOwnLogSourceRowColumn {
		t.Errorf("Source = %q, want %q", got.Source, TaskOwnLogSourceRowColumn)
	}
	if !strings.Contains(got.Note, col) {
		t.Errorf("Note = %q, want it to name the column value %q", got.Note, col)
	}
}

// TestTaskOwnSessionLogRefusesAnotherRowsRecord: the column names a record that
// says it ran a DIFFERENT row. Serving it would replay that row's conversation
// (the substitution this whole resolution exists to stop), so it is a refusal
// that names both ids — not a fallthrough and not a silent pick.
func TestTaskOwnSessionLogRefusesAnotherRowsRecord(t *testing.T) {
	root := ownLogProfile(t)
	const rowID = "row-dddddddd"
	const otherRow = "row-eeeeeeee"
	const col = "col-session-3333"
	if _, err := TaskNew("foreign record row", rowID, col, ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	ownLogRecord(t, filepath.Join(root, col, sessionlayout.RecordName), col, otherRow)

	got, err := TaskOwnSessionLog(rowID)
	if err == nil {
		t.Fatalf("TaskOwnSessionLog served %q for a record owned by another row", got.Path)
	}
	for _, want := range []string{rowID, otherRow, col} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name %q", err.Error(), want)
		}
	}
}

// TestTaskOwnSessionLogNothingAnywhere: SHAPE 3 — no run directory, no column,
// no record. The answer is the ZERO value, which is what makes the caller's
// "resume target names no checkpoint" a non-zero exit instead of a new session.
func TestTaskOwnSessionLogNothingAnywhere(t *testing.T) {
	ownLogProfile(t)
	const rowID = "row-ffffffff"
	if _, err := TaskNew("empty row", rowID, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	got, err := TaskOwnSessionLog(rowID)
	if err != nil {
		t.Fatalf("TaskOwnSessionLog: %v", err)
	}
	if got.Path != "" || got.Source != "" {
		t.Errorf("resolved %+v for a row with no evidence, want the zero answer", got)
	}

	// An operand that is no row at all is the same zero answer: the caller keeps
	// its own session-id resolution untouched.
	if got, err := TaskOwnSessionLog("not-a-row-at-all"); err != nil || got.Path != "" {
		t.Errorf("TaskOwnSessionLog(non-row) = (%+v, %v), want the zero answer", got, err)
	}
}
