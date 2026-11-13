package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/tools"
)

// WHY THESE TWO TESTS EXIST (bug 01a190ba).
//
// A task run's session id IS the row's id (mintRunID), so the second and every
// later run of a row opens the SAME raw log. newRawLoggerInTaskRunDir decided
// "this run continues a prior one" from fi.Size() == 0 alone and then wrote
//
//	# gopi session raw HTTP log — resumed <ts> pid=N session=<row> resumedFrom=<row>
//
// i.e. a run declaring that it resumed ITSELF. The session record written by
// the very same run says the opposite (session_start carries no resumedFrom, or
// an empty one), so two artifacts by one process disagreed about one event.
//
// The two shapes are tested separately because they are two different claims:
//
//	① a second run of the row      — the resume source is the SAME session, so
//	                                 there is no OTHER session to name, and the
//	                                 header must name none (agreement with the
//	                                 record is asserted in the same test).
//	② bytes with no header at all  — "the file is not empty" is not "a prior run
//	                                 exists"; an unidentifiable prior may not be
//	                                 turned into a resume source either.

// rawLogTaskRunPath is the one spelling of a task run's raw log inside this
// test file — <sessions>/<row>/<row>.raw.log.
func rawLogTaskRunPath(sessionsRoot string) string {
	return filepath.Join(sessionsRoot, rowIdentityTaskID, rowIdentityTaskID+scratch.SessionRawLogFileSuffix)
}

// rawLogHeaderLines returns the lines of a raw log that are HEADERS (creation or
// resume) — the two prefixes that carry the file's run history.
func rawLogHeaderLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw log %s: %v", path, err)
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, rawLogHeaderPrefix) || strings.HasPrefix(line, rawLogResumeHeaderPrefix) {
			lines = append(lines, line)
		}
	}
	return lines
}

// TestTaskRunSecondRunNeverResumesItself is the regression guard for the exact
// defect: run the same task row TWICE and the raw log must never contain a
// resume header naming the session the header itself belongs to.
func TestTaskRunSecondRunNeverResumesItself(t *testing.T) {
	sessionsRoot := rowIdentityEnv(t, rowIdentityTaskID)
	// A re-dispatch witness is a DIFFERENT fact and must not leak in from the
	// environment this test happens to inherit (the suite may run inside a task
	// child): with it empty, the only thing the second run knows is what the
	// raw log itself witnesses.
	t.Setenv(tools.TaskRunResumeFromEnv, "")

	for run := 1; run <= 2; run++ {
		ag, err := NewAgent(rowIdentityModel)
		if err != nil {
			t.Fatalf("run %d: NewAgent: %v", run, err)
		}
		if ag.RawLog == nil || ag.SessionLog == nil {
			t.Fatalf("run %d: minted no logs", run)
		}
		if err := ag.RawLog.Close(); err != nil {
			t.Fatalf("run %d: close raw log: %v", run, err)
		}
		if err := ag.SessionLog.Close(); err != nil {
			t.Fatalf("run %d: close session log: %v", run, err)
		}
	}

	path := rawLogTaskRunPath(sessionsRoot)
	headers := rawLogHeaderLines(t, path)
	var resumeHeaders []string
	for _, line := range headers {
		if strings.HasPrefix(line, rawLogResumeHeaderPrefix) {
			resumeHeaders = append(resumeHeaders, line)
		}
	}
	// The continuation is still ANNOUNCED — one file must not claim two
	// creations (that is what the creation header's own contract says).
	if len(resumeHeaders) != 1 {
		t.Fatalf("second run left %d resume headers, want exactly 1 (headers: %v)", len(resumeHeaders), headers)
	}
	line := resumeHeaders[0]
	if strings.Contains(line, rawLogResumedFromFieldKey+rowIdentityTaskID) {
		t.Fatalf("the second run's raw log claims it resumed ITSELF:\n  %s\n"+
			"resumedFrom names the session being written (%s), which no run can continue.",
			line, rowIdentityTaskID)
	}
	if !strings.Contains(line, rawLogSessionFieldKey+rowIdentityTaskID) {
		t.Errorf("the resume header must still name the session it belongs to, got:\n  %s", line)
	}

	// AGREEMENT: the record the same runs wrote must not disagree with the raw
	// log about this event.
	recordPath := filepath.Join(sessionsRoot, rowIdentityTaskID, rowIdentityTaskID+".jsonl")
	body, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read session record %s: %v", recordPath, err)
	}
	selfResume := `"` + sessionevent.SessionStartFieldResumedFrom + `":"` + rowIdentityTaskID + `"`
	if strings.Contains(string(body), selfResume) {
		t.Fatalf("the session record names %s as its own resume source; the raw log and the record must not both be wrong in the same way", rowIdentityTaskID)
	}
}

// TestRawLogBytesAloneAreNotAResumeSource pins the second half: a non-empty raw
// log whose run history is unidentifiable (no header line — a hand-edited file,
// a foreign fragment, a head rewritten by tooling) is NOT evidence of "a prior
// run of this row". The run continues the file, but it names no resume source.
func TestRawLogBytesAloneAreNotAResumeSource(t *testing.T) {
	sessionsRoot := rowIdentityEnv(t, rowIdentityTaskID)
	t.Setenv(tools.TaskRunResumeFromEnv, "")

	path := rawLogTaskRunPath(sessionsRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	fragment := "← CHUNK\n{\"delta\":\"a fragment left by a run whose header is gone\"}\n"
	if err := os.WriteFile(path, []byte(fragment), 0644); err != nil {
		t.Fatalf("seed the headerless raw log: %v", err)
	}

	ag, err := NewAgent(rowIdentityModel)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if err := ag.RawLog.Close(); err != nil {
		t.Fatalf("close raw log: %v", err)
	}
	if err := ag.SessionLog.Close(); err != nil {
		t.Fatalf("close session log: %v", err)
	}

	headers := rawLogHeaderLines(t, path)
	var resumeHeaders []string
	for _, line := range headers {
		if strings.HasPrefix(line, rawLogResumeHeaderPrefix) {
			resumeHeaders = append(resumeHeaders, line)
		}
	}
	if len(resumeHeaders) != 1 {
		t.Fatalf("run left %d resume headers on a non-empty log, want exactly 1 (headers: %v)", len(resumeHeaders), headers)
	}
	if got := resumeHeaders[0]; strings.Contains(got, rawLogResumedFromFieldKey) {
		t.Fatalf("a headerless fragment was turned into a resume SOURCE:\n  %s\n"+
			"bytes in the file are not a witness of a prior run of this row.", got)
	}
}
