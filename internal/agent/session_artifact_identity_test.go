package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// sessionArtifactIdentity reads the recorded artifact-identity block out of a
// session container's session_start row — the reader shape every assertion
// below goes through, so the tests exercise the DECODER a future analysis pass
// will use rather than poking at raw strings.
func sessionArtifactIdentity(t *testing.T, sessionDir, jsonlName string) sessionevent.ArtifactIdentity {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(sessionDir, jsonlName))
	if err != nil {
		t.Fatalf("read %s: %v", jsonlName, err)
	}
	var header map[string]json.RawMessage
	if uerr := json.Unmarshal(sessionFirstRow(t, raw), &header); uerr != nil {
		t.Fatalf("the session's first row is not an object: %v", uerr)
	}
	decoded, ok, derr := sessionevent.DecodeArtifactIdentity(header[sessionevent.FieldArtifactIdentity])
	if derr != nil || !ok {
		t.Fatalf("session_start artifact identity = (ok=%v, err=%v) — the record does not carry %s",
			ok, derr, sessionevent.FieldArtifactIdentity)
	}
	return decoded
}

// sessionMetadataIdentity reads the same block out of the container's
// metadata.json — the second reader the epic requires (one computation, two
// readers, one key).
func sessionMetadataIdentity(t *testing.T, sessionDir string) sessionevent.ArtifactIdentity {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(sessionDir, sessionlayout.MetadataFileName))
	if err != nil {
		t.Fatalf("read metadata.json: %v", err)
	}
	var meta map[string]json.RawMessage
	if uerr := json.Unmarshal(raw, &meta); uerr != nil {
		t.Fatalf("metadata.json is not an object: %v", uerr)
	}
	decoded, ok, derr := sessionevent.DecodeArtifactIdentity(meta[sessionevent.FieldArtifactIdentity])
	if derr != nil || !ok {
		t.Fatalf("metadata.json artifact identity = (ok=%v, err=%v) — the file does not carry %s",
			ok, derr, sessionevent.FieldArtifactIdentity)
	}
	return decoded
}

// TestSessionLoggerRecordsArtifactIdentity is the RECORDING deliverable for the
// "which binary ran this run" question (epic: a task run's tool surfaces are
// executed by the WORKER's executable, which may live in another checkout —
// measured 2026-10-14, task 01a1355d).
//
// The assertion is deliberately about the RECORD carrying the block, not about
// which arm it falls in: under `go test` this test binary is built by the
// toolchain from a checkout it may or may not recognize, so the arm is
// whatever IT is. What must never happen is SILENCE — an absent block leaves a
// report's `Ran:`/`Result:` unpinnable, which is the defect.
func TestSessionLoggerRecordsArtifactIdentity(t *testing.T) {
	_, sessionsDir := tempHome(t)
	runUUID := newUUIDv4()
	taskID := "t-artifact-identity-record-test"
	container := filepath.Join(sessionsDir, taskID)

	sl, err := NewSessionLoggerForRun("test-model", "", runUUID, []string{taskID})
	if err != nil {
		t.Fatalf("NewSessionLoggerForRun failed: %v", err)
	}
	if err := sl.Close(); err != nil {
		t.Fatalf("close the session logger: %v", err)
	}

	recorded := sessionArtifactIdentity(t, container, runUUID+".jsonl")
	if recorded.Source == "" {
		t.Fatalf("the record carries no artifact-identity source at all: %+v", recorded)
	}
	switch recorded.Source {
	case sessionevent.ArtifactSourceToolchainVCS, sessionevent.ArtifactSourceLdflagsSelfDeclared:
		if recorded.Revision == "" {
			t.Fatalf("source %q carries no revision: %+v", recorded.Source, recorded)
		}
	case sessionevent.ArtifactSourceAbsent:
		if recorded.Reason == "" {
			t.Fatalf("the absent arm carries no reason — an absence must be STATED, not blank: %+v", recorded)
		}
	default:
		t.Fatalf("the record carries an unknown source %q: %+v", recorded.Source, recorded)
	}

	self, err := os.Executable()
	if err != nil || self == "" {
		t.Fatalf("os.Executable answered (%q, %v) — the path half cannot be asserted", self, err)
	}
	if recorded.ExePath != self {
		t.Fatalf("recorded exe %q, want this process's own %q", recorded.ExePath, self)
	}

	meta := sessionMetadataIdentity(t, container)
	if meta != recorded {
		t.Fatalf("metadata.json records %+v, session_start records %+v — the two readers disagree", meta, recorded)
	}
}
