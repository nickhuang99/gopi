package tools

// THE HONEST `run_dir:` LINE (measured on this host's live ledger, 2026-10-29).
//
// The defect these tests pin: `run_dir:` was rendered from the row's own id
// with no stat at all — `taskRunDir()` merely joins
// `<gopi dir>/sessions/<task_id>` — so the line printed a path that the reader
// was invited to open, and for 32 of 39 in_progress rows the list measured that
// directory MISSING. Every one of them carried the `[auto]` marker: an auto
// row's run executes in the PARENT session's directory, so the row-keyed
// directory is never created, and the very same output already printed that
// correct address on its `session_log:` line. One block, two addresses, one of
// them true — and the false one claiming to be the run's home.
//
// The fix is a rendering fix (dispatch semantics are untouched, Rule 15): an
// absent directory is labelled MISSING and the reader is pointed at the session
// directory the row's `session_log` really names. Both faces of the view —
// `task show`'s run block and `task status`'s report — read ONE value
// (TaskRunDirValue), so they cannot disagree again.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// autoRowShape builds the shape the live ledger measured: a row whose RUN
// DIRECTORY was never created (the `[auto]` rows — the run executes in the
// parent session's directory) and whose session_id names a session directory
// that DOES exist, holding the bare-session record.
//
// It returns the row id, the session id, and that session's directory — the
// address the honest line must name.
func autoRowShape(t *testing.T) (taskID, sessionID, sessDir string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	taskID = "t-auto-row"
	sessionID = "5e5510f0-0000-4000-8000-000000000001"
	sessDir = filepath.Join(home, config.ProfileDirName, config.SessionsDirName, sessionID)
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	rec := filepath.Join(sessDir, taskBareSessionRecordName)
	body := `{"type":"session_start","sessionId":"` + sessionID +
		`","model":"m","timestamp":"2026-01-01T00:00:00Z"}` + "\n"
	if err := os.WriteFile(rec, []byte(body), 0o644); err != nil {
		t.Fatalf("write session record: %v", err)
	}
	// The precondition the whole defect rests on: the row's OWN directory is
	// not there. Without this assertion a passing test could prove nothing.
	if dirExists(taskRunDir(taskID)) {
		t.Fatalf("fixture precondition broken: run directory %s exists", taskRunDir(taskID))
	}
	return taskID, sessionID, sessDir
}

// runDirLineOf returns the ONE rendered `run_dir:` line of a rendered view.
func runDirLineOf(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, TaskShowRunDirLabel) {
			return line
		}
	}
	t.Fatalf("view renders no %s line at all:\n%s", TaskShowRunDirLabel, out)
	return ""
}

// TestRunDirLine_AbsentDirectoryIsLabelledAndNamesTheSession is the `task show`
// arm: the auto shape must not hand the reader a bare, cd-able path.
func TestRunDirLine_AbsentDirectoryIsLabelledAndNamesTheSession(t *testing.T) {
	taskID, sessionID, sessDir := autoRowShape(t)
	if _, err := TaskNew("auto row", taskID, sessionID, "m"); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	out, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	line := runDirLineOf(t, out)
	if bare := taskShowField(TaskShowRunDirLabel, taskRunDir(taskID)); line == bare {
		t.Fatalf("run_dir still renders a bare path for a row whose directory does not exist:\n%s", line)
	}
	if !strings.Contains(line, TaskRunDirMissingMark) {
		t.Errorf("a missing run directory must carry the explicit marker %q:\n%s", TaskRunDirMissingMark, line)
	}
	if !strings.Contains(line, sessDir) {
		t.Errorf("the line must name the session directory %q that session_log really points at:\n%s", sessDir, line)
	}
}

// TestRunDirLine_StatusReportCarriesTheSameReading is the `task status` arm —
// the second face that used to render the same unverified path.
func TestRunDirLine_StatusReportCarriesTheSameReading(t *testing.T) {
	taskID, sessionID, sessDir := autoRowShape(t)
	st := &TaskProcessStatus{
		TaskID:    taskID,
		SessionID: sessionID,
		Status:    taskStatusInProgress,
		Verdict:   TaskProcessStateRunning,
	}
	out := TaskProcessStatusReport(st)
	line := runDirLineOf(t, out)
	if bare := taskShowField(TaskShowRunDirLabel, taskRunDir(taskID)); line == bare ||
		line == TaskShowRunDirLabel+strings.Repeat(" ", 16-len(TaskShowRunDirLabel))+taskRunDir(taskID) {
		t.Fatalf("status run_dir still renders a bare path for a missing directory:\n%s", line)
	}
	if !strings.Contains(line, TaskRunDirMissingMark) {
		t.Errorf("status: missing run directory must carry %q:\n%s", TaskRunDirMissingMark, line)
	}
	if !strings.Contains(line, sessDir) {
		t.Errorf("status: the line must name the session directory %q:\n%s", sessDir, line)
	}
}

// TestRunDirLine_ExistingDirectoryRendersVerbatim is the other direction: a row
// whose directory IS there keeps exactly today's output — the honesty must not
// cost a reader a line they already parse.
func TestRunDirLine_ExistingDirectoryRendersVerbatim(t *testing.T) {
	taskID, _ := pluralRunFixture(t)
	if !dirExists(taskRunDir(taskID)) {
		t.Fatalf("fixture precondition broken: run directory %s missing", taskRunDir(taskID))
	}
	line := runDirLineOf(t, strings.Join(taskRunLines(taskID, ""), "\n"))
	if want := taskShowField(TaskShowRunDirLabel, taskRunDir(taskID)); line != want {
		t.Errorf("an existing run directory must render verbatim:\n got %q\nwant %q", line, want)
	}
	if strings.Contains(line, TaskRunDirMissingMark) {
		t.Errorf("a present directory must never carry the missing marker:\n%s", line)
	}
}
