package rundir

// Tests for the run-directory mapping and the run-record scan — the leaf the
// rest of the change rests on (epic run-identity-unify, 2026-09-20).

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/sessionlayout"
)

// writeJSONL writes one log file with the given lines.
func writeJSONL(t *testing.T, path string, lines ...string) {
	t.Helper()
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestDirIsTheRunIDUnderSessions pins the ONE mapping: a run's directory is the
// sessions root plus its run id, verbatim.
func TestDirIsTheRunIDUnderSessions(t *testing.T) {
	got := Dir("/data/.gopi", "t-123")
	want := filepath.Join("/data/.gopi", config.SessionsDirName, "t-123")
	if got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
	// Whitespace is trimmed, never smuggled into a path.
	if got := Dir("/d", "  t-9  "); got != filepath.Join("/d", config.SessionsDirName, "t-9") {
		t.Errorf("Dir with padding = %q", got)
	}
}

// TestRecordsTaskRunDirEnumeratesEveryRunNewestLast is the task-run layout: one
// record per run log, ordered by the run's own start, side files excluded.
func TestRecordsTaskRunDirEnumeratesEveryRunNewestLast(t *testing.T) {
	dir := t.TempDir()
	writeJSONL(t, filepath.Join(dir, "run-a.jsonl"),
		`{"type":"session_start","sessionId":"run-a","model":"m-a","timestamp":"2026-01-01T00:00:00Z"}`)
	writeJSONL(t, filepath.Join(dir, "run-b.jsonl"),
		`{"type":"session_start","sessionId":"run-b","model":"m-b","timestamp":"2026-02-02T00:00:00Z"}`)
	// A side file of the SAME runs: it must never be counted as a third run.
	writeJSONL(t, filepath.Join(dir, "signal-provenance.jsonl"), `{"kind":"signal"}`)

	recs := RecordsDir(dir)
	if len(recs) != 2 {
		t.Fatalf("records = %d, want 2: %+v", len(recs), recs)
	}
	if recs[0].RunID != "run-a" || recs[1].RunID != "run-b" {
		t.Errorf("order = %q,%q — want oldest first", recs[0].RunID, recs[1].RunID)
	}
	if recs[1].Model != "m-b" {
		t.Errorf("model = %q, want the session_start value m-b", recs[1].Model)
	}
	latest, ok := Latest(recs)
	if !ok || latest.RunID != "run-b" {
		t.Errorf("Latest = %q,%v — want run-b", latest.RunID, ok)
	}
}

// TestRecordsBareDirIsOneRunNamedByItsDirectory is the bare (chat) layout: the
// record's file name is fixed, so only the DIRECTORY names the run.
func TestRecordsBareDirIsOneRunNamedByItsDirectory(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "sess-uuid")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeJSONL(t, filepath.Join(dir, recordNameForTest()),
		`{"type":"session_start","sessionId":"sess-uuid","timestamp":"2026-01-01T00:00:00Z"}`)
	// An artifact beside the single record must not become a second run.
	writeJSONL(t, filepath.Join(dir, "console.log.jsonl"), `{"noise":true}`)

	recs := RecordsDir(dir)
	if len(recs) != 1 {
		t.Fatalf("bare dir records = %d, want exactly 1: %+v", len(recs), recs)
	}
	if recs[0].RunID != "sess-uuid" {
		t.Errorf("bare run id = %q, want the DIRECTORY name sess-uuid", recs[0].RunID)
	}
}

// TestUnfinishedRunReportsProgressNotAVerdict: a log with no session_end is
// unfinished, and its progress is the last request event's iteration — never a
// fabricated completion (Rule 15).
func TestUnfinishedRunReportsProgressNotAVerdict(t *testing.T) {
	dir := t.TempDir()
	writeJSONL(t, filepath.Join(dir, "run-x.jsonl"),
		`{"type":"session_start","sessionId":"run-x","timestamp":"2026-01-01T00:00:00Z"}`,
		`{"type":"request","iteration":7}`,
		`{"type":"tool_result","name":"bash"}`)
	rec := RecordsDir(dir)[0]
	if rec.HasEnd {
		t.Errorf("HasEnd = true for a log with no session_end")
	}
	if rec.Turns != 7 {
		t.Errorf("Turns = %d, want the last request iteration 7", rec.Turns)
	}
	if !rec.HasStart {
		t.Errorf("HasStart = false — the fixture carries a session_start")
	}
}

// TestFinishedRunReadsItsTerminalFacts: session_end is the verdict, exit code
// included (0 is a real value, so HasExit distinguishes "exit 0" from "absent").
func TestFinishedRunReadsItsTerminalFacts(t *testing.T) {
	dir := t.TempDir()
	writeJSONL(t, filepath.Join(dir, "run-y.jsonl"),
		`{"type":"session_start","sessionId":"run-y","timestamp":"2026-01-01T00:00:00Z"}`,
		`{"type":"session_end","elapsed_sec":12.5,"exit_code":0,"total_iterations":42,"total_tokens":99}`)
	rec := RecordsDir(dir)[0]
	if !rec.HasEnd || rec.Turns != 42 || rec.Tokens != 99 || rec.ElapsedSec != 12.5 {
		t.Errorf("terminal facts = %+v", rec)
	}
	if !rec.HasExit || rec.ExitCode != 0 {
		t.Errorf("HasExit/ExitCode = %v/%d — exit 0 must be a recorded value", rec.HasExit, rec.ExitCode)
	}
}

// TestWithCheckpointPicksTheNewestResumableRun is the selection rule the resume
// path depends on: newest run that still owns a checkpoint, skipping a newest
// run whose checkpoint is gone.
func TestWithCheckpointPicksTheNewestResumableRun(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.raw.log")
	if err := os.WriteFile(keep, []byte("x"), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	writeJSONL(t, filepath.Join(dir, "run-1.jsonl"),
		`{"type":"session_start","sessionId":"run-1","timestamp":"2026-01-01T00:00:00Z","rawLogPath":"`+keep+`"}`)
	// Newer run, checkpoint recorded but the file is GONE.
	writeJSONL(t, filepath.Join(dir, "run-2.jsonl"),
		`{"type":"session_start","sessionId":"run-2","timestamp":"2026-02-01T00:00:00Z","rawLogPath":"/nowhere/gone.raw.log"}`)

	recs := RecordsDir(dir)
	rec, ok := WithCheckpoint(recs)
	if !ok || rec.RunID != "run-1" {
		t.Fatalf("WithCheckpoint = %q,%v — want the newest RESUMABLE run-1", rec.RunID, ok)
	}
	// ExistingRawLog is a definite answer: "" for the missing file, the path for
	// the present one.
	if got := ExistingRawLog(recs[1]); got != "" {
		t.Errorf("ExistingRawLog(missing) = %q, want \"\"", got)
	}
	if got := ExistingRawLog(recs[0]); got != keep {
		t.Errorf("ExistingRawLog(present) = %q, want %q", got, keep)
	}
	// The path is authoritative even when the base name does not match (a
	// custom GOPI_RAW_LOG_DIR run): only an EXISTING recorded path is used.
	other := filepath.Join(t.TempDir(), "custom-name.log")
	if err := os.WriteFile(other, []byte("y"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	writeJSONL(t, filepath.Join(dir, "run-3.jsonl"),
		`{"type":"session_start","sessionId":"run-3","timestamp":"2026-03-01T00:00:00Z","rawLogPath":"`+other+`"}`)
	if got := taskLatestCheckpointForTest(dir); got != other {
		t.Errorf("recorded custom path = %q, want %q (verbatim, not basename-matched)", got, other)
	}
}

// TestRecordsOnMissingDirIsEmptyNotAnError: absence is a value, and Exists is
// the probe that distinguishes "no directory" from "a directory with no logs".
func TestRecordsOnMissingDirIsEmptyNotAnError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nope")
	if recs := RecordsDir(dir); len(recs) != 0 {
		t.Errorf("records for a missing dir = %+v, want none", recs)
	}
	if Exists(dir) {
		t.Errorf("Exists(%s) = true for a path that is not there", dir)
	}
	if _, ok := Latest(nil); ok {
		t.Errorf("Latest(nil) reported a record")
	}
}

// taskLatestCheckpointForTest is the selection the resume path performs, spelled
// once here so the test does not re-implement it.
func taskLatestCheckpointForTest(dir string) string {
	recs := RecordsDir(dir)
	if rec, ok := WithCheckpoint(recs); ok {
		return ExistingRawLog(rec)
	}
	return ""
}

// recordNameForTest exposes internal/sessionlayout's fixed bare-record name to
// the assertions above without re-typing the literal (Rule 1b).
func recordNameForTest() string { return sessionlayout.RecordName }

// TestRunDirRawLogResolvesTheCoLocatedCheckpoint (epic resume-target-surface,
// 2026-09-26) pins the ONE resolver of the raw log that lives INSIDE a run
// directory. The run-directory layout's raw log is named `<run-id>.raw.log` by
// the same writer that mints the run directory (internal/agent's
// newRawLoggerInDir), so it carries NO record of its own — a run log beside it
// (when there is one) does not name it, and a profile whose ledger does not
// hold the row cannot learn the path from anything but the filesystem. That is
// the state this resolver is for: a raw log ORPHANED from its record is still
// the run's checkpoint.
func TestRunDirRawLogResolvesTheCoLocatedCheckpoint(t *testing.T) {
	dir := t.TempDir()
	// A run whose session log names no raw log: the checkpoint is co-located.
	writeJSONL(t, filepath.Join(dir, "run-1.jsonl"),
		`{"type":"session_start","sessionId":"run-1","timestamp":"2026-03-01T00:00:00Z"}`)
	raw := filepath.Join(dir, "run-1"+scratch.SessionRawLogFileSuffix)
	if err := os.WriteFile(raw, []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	if got := RunDirRawLog(dir); got != raw {
		t.Errorf("RunDirRawLog = %q, want %q", got, raw)
	}
	// The record-based resolution is exactly what CANNOT answer here — that is
	// the whole reason the directory-level resolver exists.
	if got := taskLatestCheckpointForTest(dir); got != "" {
		t.Errorf("record-based checkpoint = %q, want none (the log names no raw log)", got)
	}
	// A run directory with no raw log is a definite absence, not a fallback.
	if got := RunDirRawLog(t.TempDir()); got != "" {
		t.Errorf("RunDirRawLog on an empty dir = %q, want %q", got, "")
	}
	// A missing directory is an absence too — never a wildcard or a panic.
	if got := RunDirRawLog(filepath.Join(dir, "nope")); got != "" {
		t.Errorf("RunDirRawLog on a missing dir = %q, want %q", got, "")
	}
	// The directory itself is not a raw log, and neither is the recorder's
	// side file or the session jsonl: only the declared suffix counts.
	if got := RunDirRawLog(""); got != "" {
		t.Errorf("RunDirRawLog(\"\") = %q, want %q", got, "")
	}
}
