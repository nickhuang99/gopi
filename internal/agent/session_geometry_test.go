package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// TestSessionLoggerRecordsTerminalGeometry is the RECORDING deliverable: the
// session a run writes says which terminal it was on, in the header row a replay
// reads, and the same object lands in metadata.json — one measurement, two
// readers, one key.
//
// The geometry asserted is whatever THIS test binary measured (under `go test`
// that is the unknown state, since stdout is a pipe), because the assertion is
// about the RECORDING carrying the measurement rather than about the host having
// a terminal: a run with no terminal must still record a state, and the shape it
// records must round-trip to the same thing a replay reads.
func TestSessionLoggerRecordsTerminalGeometry(t *testing.T) {
	_, sessionsDir := tempHome(t)
	runUUID := newUUIDv4()
	taskID := "t-geometry-record-test"

	sl, err := NewSessionLoggerForRun("test-model", "", runUUID, []string{taskID})
	if err != nil {
		t.Fatalf("NewSessionLoggerForRun failed: %v", err)
	}
	measurement := sl.geometry
	if err := sl.Close(); err != nil {
		t.Fatalf("close the session logger: %v", err)
	}

	if measurement.State == "" {
		t.Fatalf("the logger recorded no geometry state at all: %+v", measurement)
	}
	if measurement.State == sessionevent.GeometryStateMeasured && !measurement.Known() {
		t.Fatalf("the logger recorded %q without a usable size: %+v", measurement.State, measurement)
	}

	// The JSONL header: the row a replay parses.
	raw, err := os.ReadFile(sl.Path())
	if err != nil {
		t.Fatalf("read the session JSONL: %v", err)
	}
	var header map[string]json.RawMessage
	if uerr := json.Unmarshal(sessionFirstRow(t, raw), &header); uerr != nil {
		t.Fatalf("the session's first row is not an object: %v", uerr)
	}
	if got := string(header["type"]); got != `"`+sessionevent.EventTypeSessionStart+`"` {
		t.Fatalf("the first row is %s, want the session_start event", got)
	}
	decoded, ok, derr := sessionevent.DecodeTerminalGeometry(header[sessionevent.FieldTerminalGeometry])
	if derr != nil || !ok {
		t.Fatalf("session_start geometry = (ok=%v, err=%v) — the record does not carry the field", ok, derr)
	}
	if decoded.State != measurement.State || decoded.Cols != measurement.Cols || decoded.Rows != measurement.Rows {
		t.Fatalf("session_start recorded %+v, but the logger measured %+v", decoded, measurement)
	}

	// metadata.json: the same object, the same key.
	metaRaw, err := os.ReadFile(filepath.Join(sessionsDir, taskID, "metadata.json"))
	if err != nil {
		t.Fatalf("read metadata.json: %v", err)
	}
	var meta map[string]json.RawMessage
	if uerr := json.Unmarshal(metaRaw, &meta); uerr != nil {
		t.Fatalf("metadata.json is not an object: %v", uerr)
	}
	fromMeta, ok, derr := sessionevent.DecodeTerminalGeometry(meta[sessionevent.FieldTerminalGeometry])
	if derr != nil || !ok {
		t.Fatalf("metadata.json geometry = (ok=%v, err=%v) — the two readers disagree about the field", ok, derr)
	}
	if fromMeta != decoded {
		t.Fatalf("metadata.json says %+v where session_start says %+v — one measurement must not become two", fromMeta, decoded)
	}
}

// TestSessionLoggerGeometryIsUnknownWithoutATerminal is the fail-closed half:
// when no standard descriptor is a terminal the record says UNKNOWN with a
// reason, and it does NOT write a zero pair that a later reader could mistake
// for a measured size.
func TestSessionLoggerGeometryIsUnknownWithoutATerminal(t *testing.T) {
	_, _ = tempHome(t)
	sl, err := NewSessionLoggerForRun("test-model", "", newUUIDv4(), []string{"t-geometry-unknown-test"})
	if err != nil {
		t.Fatalf("NewSessionLoggerForRun failed: %v", err)
	}
	measurement := sl.geometry
	if err := sl.Close(); err != nil {
		t.Fatalf("close the session logger: %v", err)
	}

	if measurement.Known() {
		t.Skipf("this test binary really does have a terminal (%+v) — the unknown assertion is not about this run", measurement)
	}
	if measurement.State != sessionevent.GeometryStateUnknown {
		t.Fatalf("state = %q, want %q", measurement.State, sessionevent.GeometryStateUnknown)
	}
	if measurement.Reason == "" {
		t.Fatalf("the unknown geometry carries no reason: %+v", measurement)
	}
	if measurement.Cols != 0 || measurement.Rows != 0 {
		t.Fatalf("the unknown geometry carries numbers (%+v) that would read as a measured size", measurement)
	}
}

// firstLine returns the first non-empty line of a JSONL file.
func sessionFirstRow(t *testing.T, raw []byte) []byte {
	t.Helper()
	for _, line := range sessionSplitLines(raw) {
		if len(line) > 0 {
			return line
		}
	}
	t.Fatalf("the session JSONL has no rows at all")
	return nil
}

// sessionSplitLines splits on newlines without pulling in strings just for this: the
// rows are newline-delimited JSON, and a trailing partial row is returned as-is
// (it is still the row being looked at).
func sessionSplitLines(raw []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, b := range raw {
		if b == '\n' {
			out = append(out, raw[start:i])
			start = i + 1
		}
	}
	if start < len(raw) {
		out = append(out, raw[start:])
	}
	return out
}
