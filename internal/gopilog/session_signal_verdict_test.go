package gopilog

// session_signal_verdict_test.go — the READER half of the signal-verdict-event
// contract (epic signalrec-session-log-verdict, 2026-09-17).
//
// WHY THIS FILE EXISTS, and why it lives here. A killed gopi process appends a
// "signal_verdict" event to its own session JSONL (internal/signalrec). The
// session readers must tolerate that event — and this package reads a session
// file with THREE parsers, which do NOT agree about junk:
//
//	ParseFile            json.Unmarshal per line, HARD ERROR on any failure
//	                     (the unified reader behind `gopi session log --jsonl/--chat`,
//	                     `gopi session stats`), reached through cli_log/cli_stats
//	ValidateSessionFile  unmarshals per line, silently `continue` on failure
//	SessionRawLogPath    same silent skip
//
// The tolerant pair would hide the appended line; the strict one would fail the
// WHOLE file. So the format decision (a structured JSON event with its own type,
// rather than the prose line the constant's doc originally promised) is settled
// by running the real readers over a real file, HERE, where they live — the
// writer's own test cannot import this package (gopilog → agent → tools →
// signalrec is an import cycle).
//
// The last arm is the CONTRAST that keeps this test from being vacuous: it
// proves the strict reader really does reject an unparsable line, so a green
// first arm means "this event parses", not "nothing here ever fails".

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// svSessionID is the fixture session's uuid.
const svSessionID = "b91e4c77-3f52-4d18-a6c0-51e7b2d9f304"

// svRawLogName is the raw-log file name the fixture's session_start names.
const svRawLogName = "raw-fixture.log"

// TestSignalVerdictEventIsReadableByEverySessionParser pins the reader story:
// the event a killed process appends neither breaks the strict parser nor
// changes what the tolerant ones resolve.
func TestSignalVerdictEventIsReadableByEverySessionParser(t *testing.T) {
	path := filepath.Join(t.TempDir(), sessionlayout.RecordName)
	body := strings.Join([]string{
		svLine(t, map[string]any{
			"type":       sessionevent.EventTypeSessionStart,
			"sessionId":  svSessionID,
			"model":      "fixture-model",
			"rawLogPath": filepath.Join(filepath.Dir(path), svRawLogName),
		}),
		svLine(t, map[string]any{
			"type": "user",
			"message": map[string]any{
				"role":    "user",
				"content": "a task the run never finished, because a signal arrived",
			},
		}),
		svLine(t, map[string]any{
			"type":      sessionevent.EventTypeSignalVerdict,
			"timestamp": "2026-09-17T12:00:00.000000+08:00",
			"signal":    "SIGTERM",
			"pid":       4242,
			"role":      "worker",
			"sourced":   false,
		}),
	}, "")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write the session fixture: %v", err)
	}

	// ARM 1: the STRICT reader. Any unparsable line makes this fail outright, so
	// a nil error is the verdict "the appended event parses".
	sess, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile rejected a session carrying a %q event (%v) — the unified reader must tolerate the verdict event:\n%s",
			sessionevent.EventTypeSignalVerdict, err, body)
	}
	if sess.ID != svSessionID {
		t.Errorf("parsed session id = %q, want %q — the extra event must not disturb the session's identity", sess.ID, svSessionID)
	}
	if sess.Task == "" {
		t.Error("the session's user prompt was lost — the verdict event must not displace the conversation")
	}

	// ARM 2: ValidateSessionFile (tolerant, but it must still find its
	// session_start).
	if err := ValidateSessionFile(path); err != nil {
		t.Errorf("ValidateSessionFile = %v, want nil over a session carrying a verdict event", err)
	}

	// ARM 3: SessionRawLogPath resolves the pair from session_start — the
	// co-located-raw-log contract must survive the extra event.
	if got := SessionRawLogPath(path); !strings.HasSuffix(got, svRawLogName) {
		t.Errorf("SessionRawLogPath = %q, want the fixture's %q", got, svRawLogName)
	}

	// ARM 4: FormatJSONL is the pass-through behind `gopi session log <id> --json`; the
	// event must be visible in it, i.e. not skipped on the way out.
	out, err := FormatJSONL(path)
	if err != nil {
		t.Fatalf("FormatJSONL = %v, want the raw session text", err)
	}
	if !strings.Contains(out, sessionevent.EventTypeSignalVerdict) {
		t.Errorf("FormatJSONL dropped the verdict event — `gopi session log --json` must show the kill witness")
	}
}

// TestSessionParserRejectsAProseVerdictLine is the CONTRAST arm: it pins WHY the
// writer emits a JSON event rather than the prose line the retired env knob's
// doc promised. With a prose line in the same slot, the strict reader fails the
// whole file — which is exactly the "gopi session log --jsonl cannot read the session an
// operator opens after a kill" defect the format decision removes.
func TestSessionParserRejectsAProseVerdictLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), sessionlayout.RecordName)
	body := strings.Join([]string{
		svLine(t, map[string]any{
			"type":      sessionevent.EventTypeSessionStart,
			"sessionId": svSessionID,
		}),
		"[gopi] worker: SIGTERM received with unsourced provenance — \n",
	}, "")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write the prose fixture: %v", err)
	}
	if _, err := ParseFile(path); err == nil {
		t.Fatal("ParseFile ACCEPTED a prose line in a session log — then the format decision is unnecessary and this test is wrong; re-check the writer")
	}
	// The tolerant reader hides the same line — which is the other half of the
	// story: prose is invisible to one parser and fatal to the other.
	if err := ValidateSessionFile(path); err != nil {
		t.Errorf("ValidateSessionFile = %v over a session whose session_start parsed, want nil (it skips the prose line silently — that skip is the defect prose would cause)", err)
	}
}

// svLine renders one JSONL line.
func svLine(t *testing.T, ev map[string]any) string {
	t.Helper()
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal fixture event: %v", err)
	}
	return string(b) + "\n"
}
