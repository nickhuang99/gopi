package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// THE SESSION VERBS' ROW PATH — the three shapes plus the refusal, pinned at
// the resolver the verbs actually call (findSessionLogPath).
//
// WHY THESE ARE RESOLVER TESTS AND NOT VERB TESTS: the defect measured on
// 2026-10-29 was a RESOLUTION disagreement between two verbs over one operand
// (`session log` exit=1 / `session resume` exit=0 on the same row id), so what
// must not drift is the answer this function gives. The verbs' own dispatch is
// walked for real by the Rule 31 probes recorded in the task report.
func resolverTestProfile(t *testing.T) (dir, sessionsRoot string) {
	t.Helper()
	home := testutil.TestScratchDir(t, "sess-resolver-home")
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv("HOME", home)
	gd, err := config.GopiDir()
	if err != nil {
		t.Fatalf("GopiDir: %v", err)
	}
	if gd != filepath.Join(home, config.ProfileDirName) {
		t.Fatalf("profile root = %q, want the pinned sandbox profile", gd)
	}
	return gd, filepath.Join(gd, config.SessionsDirName)
}

// seedRecord writes a session record whose session_start names sid and tid.
func seedRecord(t *testing.T, path, sid, tid string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	line := `{"type":"session_start","sessionId":"` + sid + `"`
	if tid != "" {
		line += `,"taskId":"` + tid + `"`
	}
	line += "}\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestSessionLogResolvesARowByItsOwnRunDirectory: SHAPE 1. The row id is the
// operand, its directory names it, and the record inside is named by the RUN
// uuid — a name the operand can never spell. This is the shape that answered
// "Session not found".
func TestSessionLogResolvesARowByItsOwnRunDirectory(t *testing.T) {
	_, sessionsRoot := resolverTestProfile(t)
	const rowID = "rowshape1"
	rec := filepath.Join(sessionsRoot, rowID, "3f0a-run-uuid"+sessionlayout.JSONLSuffix)
	seedRecord(t, rec, "3f0a-run-uuid", rowID)

	got, err := findSessionLogPath(sessionsRoot, rowID)
	if err != nil {
		t.Fatalf("findSessionLogPath(%s): %v", rowID, err)
	}
	if got != rec {
		t.Fatalf("resolved %q, want the row's own record %q", got, rec)
	}
}

// TestSessionLogResolvesARowThroughItsSessionColumn: SHAPE 2. The row's own
// directory is gone (the re-homed run) and the ledger column names a bare
// session record. The verb must serve it — and must SAY the ownership rests on
// the column (Rule 15), never quieten where the answer came from.
func TestSessionLogResolvesARowThroughItsSessionColumn(t *testing.T) {
	_, sessionsRoot := resolverTestProfile(t)
	const rowID = "rowshape2"
	const col = "colshape2"
	if _, err := tools.TaskNew("column row", rowID, col, ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	rec := filepath.Join(sessionsRoot, col, sessionlayout.RecordName)
	seedRecord(t, rec, col, "")

	got, err := findSessionLogPath(sessionsRoot, rowID)
	if err != nil {
		t.Fatalf("findSessionLogPath(%s): %v", rowID, err)
	}
	if got != rec {
		t.Fatalf("resolved %q, want the column's record %q", got, rec)
	}
}

// TestSessionLogRefusesWhenTheRowOwnsNoRecord: SHAPE 3. Nothing anywhere — and
// the resolver must answer "nothing", which is what makes the verb exit
// non-zero instead of opening some other session. The assertion is on the EMPTY
// answer, because a non-empty one here would be a substitution.
func TestSessionLogRefusesWhenTheRowOwnsNoRecord(t *testing.T) {
	_, sessionsRoot := resolverTestProfile(t)
	const rowID = "rowshape3"
	if _, err := tools.TaskNew("no checkpoint row", rowID, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	// A DECOY: a session that exists and could be mistaken for the answer.
	seedRecord(t, filepath.Join(sessionsRoot, "unrelated", sessionlayout.RecordName), "unrelated", "")

	got, err := findSessionLogPath(sessionsRoot, rowID)
	if err != nil {
		t.Fatalf("findSessionLogPath(%s): %v", rowID, err)
	}
	if got != "" {
		t.Fatalf("resolved %q for a row that owns no record — a substitution", got)
	}
}

// TestSessionLogRefusesARecordThatBelongsToAnotherRow: the row's column names a
// record whose own session_start declares a DIFFERENT row. The evidence
// contradicts itself, so the answer is an ERROR (the verb exits non-zero), never
// the other row's conversation.
func TestSessionLogRefusesARecordThatBelongsToAnotherRow(t *testing.T) {
	_, sessionsRoot := resolverTestProfile(t)
	const rowID = "rowshape4"
	const other = "rowother4"
	const col = "colshape4"
	if _, err := tools.TaskNew("foreign-owned record", rowID, col, ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	seedRecord(t, filepath.Join(sessionsRoot, col, sessionlayout.RecordName), col, other)

	got, err := findSessionLogPath(sessionsRoot, rowID)
	if err == nil {
		t.Fatalf("served %q for a record owned by %s", got, other)
	}
	if !strings.Contains(err.Error(), other) || !strings.Contains(err.Error(), rowID) {
		t.Errorf("refusal %q must name both rows", err.Error())
	}
}

// TestSessionLogKeepsTheIdLayoutsWorking: a plain SESSION id (a chat, no ledger
// row) resolves exactly as it did before this change — the row path is an
// addition ahead of the id layouts, never a replacement for them.
func TestSessionLogKeepsTheIdLayoutsWorking(t *testing.T) {
	_, sessionsRoot := resolverTestProfile(t)
	const sid = "chat-session-9"
	rec := filepath.Join(sessionsRoot, sid, sessionlayout.RecordName)
	seedRecord(t, rec, sid, "")

	got, err := findSessionLogPath(sessionsRoot, sid)
	if err != nil {
		t.Fatalf("findSessionLogPath(%s): %v", sid, err)
	}
	if got != rec {
		t.Fatalf("resolved %q, want %q", got, rec)
	}
}
