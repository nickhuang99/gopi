package agent

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE REFUSAL MUST BE TAKEN, NOT ANNOUNCED (epic 01a19434, measured 2026-11-01).
//
// refuseUnterminatedRecordTail refuses to append onto a record that ends
// mid-line, and its text promises "The session cannot be continued until the
// record is examined". That promise used to be false on the resumed path:
// NewAgentResuming printed the refusal to stderr and returned a usable agent,
// so the caller spent a full model round — 1,139,114 bytes of raw HTTP log on a
// 405,504-byte torn record — exited 0, and wrote ZERO bytes into any
// session.jsonl. The run's transcript existed only in the wire log.
//
// These two tests are that pair: the refusal now ABORTS the constructor with
// every asset of the continued run byte-identical, and a healthy record still
// continues exactly as before.
//
// Why the constructor and not the loop: it is the seam where the run is still
// side-effect-free. The raw logger APPENDS to the continued run's wire log
// (writeRawLogResumeHeader writes straight to the file, before any round), so a
// check placed only at the record's own open would still grow the wire log.

// resumeRefusalFixture lays down a continued run's two assets — the session
// record (with the given bytes) and its raw log — and returns their paths.
func resumeRefusalFixture(t *testing.T, recordBody string) (recordPath, rawLogPath string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("GOPI_HOME"), ".gopi", "sessions", "torn-resume")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the continued run's directory: %v", err)
	}
	recordPath = filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(recordPath, []byte(recordBody), 0o600); err != nil {
		t.Fatalf("write the record fixture: %v", err)
	}
	rawLogPath = filepath.Join(dir, "torn-resume.raw.log")
	if err := os.WriteFile(rawLogPath, []byte("# gopi session raw HTTP log — created 2026-11-01T00:00:00Z pid=1 session=torn-resume\n"), 0o644); err != nil {
		t.Fatalf("write the raw log fixture: %v", err)
	}
	return recordPath, rawLogPath
}

// resumedTornIdentity is the identity `gopi session resume` threads for a run
// that continues an EXISTING record: both paths named, so it takes the
// openResumeSessionRecord door (RunIdentity.SessionLogPath's doc).
func resumedTornIdentity(recordPath, rawLogPath string) RunIdentity {
	return RunIdentity{
		SessionID:      "torn-resume",
		ResumedFrom:    "torn-resume",
		RawLogPath:     rawLogPath,
		SessionLogPath: recordPath,
	}
}

// TestNewAgentResumingAbortsOnRefusedRecord is the acceptance case for the
// epic's fix: a resume of a torn record fails at the constructor, with the named
// refusal, BEFORE the run has grown anything.
func TestNewAgentResumingAbortsOnRefusedRecord(t *testing.T) {
	newTestAgentEnv(t)
	// The wire log must be ON: its size is the measurement that no round was
	// reached, and an OFF switch would make "no growth" true for the wrong
	// reason (the whole capture being disabled).
	t.Setenv(rawLogEnableEnv, "")

	recordPath, rawLogPath := resumeRefusalFixture(t, tornRecordBody)
	beforeRecord, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read the record before the run: %v", err)
	}
	beforeRaw, err := os.ReadFile(rawLogPath)
	if err != nil {
		t.Fatalf("read the raw log before the run: %v", err)
	}

	ag, err := NewAgentResuming("test-model", resumedTornIdentity(recordPath, rawLogPath))
	if err == nil {
		if ag != nil && ag.SessionLog != nil {
			// The close is the only cleanup this branch has, and its error is
			// the one fact that says the logger above was usable at all — so it
			// is asserted rather than discarded, even though the assertion
			// below already fails the test.
			if cerr := ag.SessionLog.Close(); cerr != nil {
				t.Errorf("close the logger of the agent that should never have been returned: %v", cerr)
			}
		}
		t.Fatalf("resuming a record that ends mid-line returned a usable agent — the refusal was announced, not taken")
	}
	if ag != nil {
		t.Fatalf("the refusal returned an agent too — a refused run must not be runnable")
	}
	var torn *UnterminatedRecordTailError
	if !errors.As(err, &torn) {
		t.Fatalf("the abort is not the named UnterminatedRecordTailError (got %T): %v", err, err)
	}
	if torn.Path != recordPath {
		t.Errorf("refusal names %q, want the continued record %q", torn.Path, recordPath)
	}
	if !strings.Contains(err.Error(), "cannot be continued until the record is examined") {
		t.Errorf("the abort does not carry the guard's own promise text:\n%s", err)
	}

	// THE RECORD IS UNTOUCHED — the existing arm's contract, re-asserted here
	// because this is the path a real `gopi session resume` takes.
	afterRecord, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read the record after the refusal: %v", err)
	}
	if !bytes.Equal(beforeRecord, afterRecord) {
		t.Fatalf("the refused resume modified the record:\nbefore=%q\nafter =%q", beforeRecord, afterRecord)
	}

	// AND SO IS THE WIRE LOG — this is the half that separates "refused before
	// any round" from "refused after the resume header". The raw logger's
	// header is written straight to the file, so a refusal taken any later
	// would show up here as growth.
	afterRaw, err := os.ReadFile(rawLogPath)
	if err != nil {
		t.Fatalf("read the raw log after the refusal: %v", err)
	}
	if !bytes.Equal(beforeRaw, afterRaw) {
		t.Fatalf("the refused resume appended to the raw log (%d -> %d bytes): the refusal was spent after the run had already started",
			len(beforeRaw), len(afterRaw))
	}
}

// TestNewAgentResumingContinuesHealthyRecord is the negative control: a record
// whose last byte is a newline is continuable, and the constructor must still
// build the agent and APPEND to both assets. Without it, "no growth" above could
// be satisfied by refusing everything.
func TestNewAgentResumingContinuesHealthyRecord(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv(rawLogEnableEnv, "")

	healthy := tornRecordBody[:strings.LastIndex(tornRecordBody, "\n")+1]
	recordPath, rawLogPath := resumeRefusalFixture(t, healthy)
	beforeRaw, err := os.ReadFile(rawLogPath)
	if err != nil {
		t.Fatalf("read the raw log before the run: %v", err)
	}

	ag, err := NewAgentResuming("test-model", resumedTornIdentity(recordPath, rawLogPath))
	if err != nil {
		t.Fatalf("a record whose last byte is a newline must be continuable: %v", err)
	}
	if ag == nil || ag.SessionLog == nil {
		t.Fatal("no agent, or no session log, for a continuable record")
	}
	if err := ag.SessionLog.Close(); err != nil {
		t.Fatalf("close the resumed session log: %v", err)
	}

	record, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read the record after the run: %v", err)
	}
	if n := strings.Count(string(record), `"type":"session_start"`); n != 2 {
		t.Fatalf("the resumed run's header did not reach the record (%d session_start rows, want 2)", n)
	}
	raw, err := os.ReadFile(rawLogPath)
	if err != nil {
		t.Fatalf("read the raw log after the run: %v", err)
	}
	if len(raw) <= len(beforeRaw) {
		t.Fatalf("the resumed run wrote no resume witness into the raw log (%d -> %d bytes)", len(beforeRaw), len(raw))
	}
}
