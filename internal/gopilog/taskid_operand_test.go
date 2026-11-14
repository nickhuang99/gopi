package gopilog

// Regression (epic: one task row, one id — the BACKWARD-COMPATIBILITY half).
//
// A task run's directory is named by the ROW (rundir's address rule), while the
// files inside it were historically named by a MINTED uuid. So the operand the
// operator actually holds — the task id — named the DIRECTORY correctly and no
// file at all: the fast path's name candidates missed, and the tree walk
// compares session ids, where the task id appears nowhere. Measured before the
// fix: the row's own record was reachable only by its run uuid.
//
// WHAT THIS PINS: FindSession resolves a task-id operand to the record inside
// sessions/<task-id>/ under BOTH layouts, and does so WITHOUT the whole-tree
// walk (runDirRecordForID answers from the operand's own directory).
//
// Zero subprocesses, zero profile contact: every fixture lives in t.TempDir().

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// taskIDOperandBaseTime is the fixed timestamp the ordering test writes, so the
// assertion does not depend on filesystem timestamp granularity (Rule 1: a named
// value, not a literal at the call site).
var taskIDOperandBaseTime = time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)

const (
	// taskIDOperandRow is a fixture ROW id — the directory's name and the
	// operand every assertion below uses.
	taskIDOperandRow = "01a17de9-e772-7470-80c1-f954f07c6b59"

	// taskIDOperandOldRun is the run uuid of the LEGACY layout's record: the
	// file is named by it, and nothing in it mentions the row.
	taskIDOperandOldRun = "df8d3009-33a8-45b1-bd99-a7212fbe2b7a"

	// taskIDOperandNewRun is the unified layout's record name: the run's id IS
	// the row id, so the file, the session_start and the directory agree.
	taskIDOperandNewRun = taskIDOperandRow
)

// TestFindSessionResolvesATaskIDOperand pins both layouts by directory, which
// is what makes ONE rule serve the old tree (whose file names can never match
// the operand) and the new one.
func TestFindSessionResolvesATaskIDOperand(t *testing.T) {
	for _, tc := range []struct {
		name  string
		runID string
	}{
		{name: "legacy run-uuid record", runID: taskIDOperandOldRun},
		{name: "unified row-id record", runID: taskIDOperandNewRun},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			runDir := filepath.Join(root, taskIDOperandRow)
			want := writeRefRecord(t, filepath.Join(runDir, tc.runID+sessionlayout.JSONLSuffix), tc.runID)

			walksBefore, _ := sessionWalkProbe.walks.Load(), sessionWalkProbe.records.Load()
			got, err := FindSession(root, taskIDOperandRow)
			walksAfter := sessionWalkProbe.walks.Load()
			if err != nil {
				t.Fatalf("FindSession(%q): %v", taskIDOperandRow, err)
			}
			if got != want {
				t.Fatalf("FindSession(%q) = %q, want %q", taskIDOperandRow, got, want)
			}
			if walksAfter != walksBefore {
				t.Errorf("resolution walked the tree (%d → %d) — a task-id operand names its own directory", walksBefore, walksAfter)
			}

			// The display id is still the RECORD's own id, not the operand: a
			// reader that renders the session must not be told the row's id
			// when the record says otherwise (the old layout's honest answer).
			if id := FileToID(got); id != tc.runID {
				t.Errorf("FileToID(%q) = %q, want the record's own id %q", got, id, tc.runID)
			}
		})
	}
}

// TestFindSessionTaskIDOperandMissesStayMisses is the other side: a directory
// with no record, and an id no directory names, both keep the "not found"
// answer (empty path, nil error). A resolution that invented a record for a
// directory would turn "never ran" into "ran somewhere".
func TestFindSessionTaskIDOperandMissesStayMisses(t *testing.T) {
	root := t.TempDir()
	// A run directory that holds only an ARTIFACT (a side file), never a record.
	sideOnly := filepath.Join(root, taskIDOperandRow)
	if err := os.MkdirAll(sideOnly, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sideOnly, sessionlayout.SideFileNames()[0]), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, operand := range []string{taskIDOperandRow, "no-such-row-at-all"} {
		got, err := FindSession(root, operand)
		if err != nil {
			t.Fatalf("FindSession(%q): %v", operand, err)
		}
		if got != "" {
			t.Errorf("FindSession(%q) = %q, want the empty not-found answer", operand, got)
		}
	}
}

// TestNewestRecordInDirPicksTheLatest pins the ONE answer to "which record does
// this run directory stand for?" — the definition internal/gopilog's task-id
// fallback and internal/tools' ledger stamp both consume (Rules 13/14).
func TestNewestRecordInDirPicksTheLatest(t *testing.T) {
	dir := t.TempDir()
	older := writeRefRecord(t, filepath.Join(dir, taskIDOperandOldRun+sessionlayout.JSONLSuffix), taskIDOperandOldRun)
	newer := writeRefRecord(t, filepath.Join(dir, taskIDOperandNewRun+sessionlayout.JSONLSuffix), taskIDOperandNewRun)

	// writeRefRecord writes both files; make the order explicit and independent
	// of filesystem timestamp granularity.
	if err := os.Chtimes(older, taskIDOperandBaseTime, taskIDOperandBaseTime); err != nil {
		t.Fatal(err)
	}
	later := taskIDOperandBaseTime.Add(60 * time.Second)
	if err := os.Chtimes(newer, later, later); err != nil {
		t.Fatal(err)
	}

	rec, ok := sessionlayout.NewestRecordInDir(dir)
	if !ok {
		t.Fatalf("NewestRecordInDir(%q) found no record, want %q", dir, newer)
	}
	if rec.Path != newer {
		t.Errorf("NewestRecordInDir = %q, want the most recently written %q", rec.Path, newer)
	}

	if _, ok := sessionlayout.NewestRecordInDir(t.TempDir()); ok {
		t.Error("NewestRecordInDir reported a record for an empty directory")
	}
}
