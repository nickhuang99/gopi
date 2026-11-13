package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionlayout"
)

// TestOrphanDumpNamesTheSessionsRealIdentity pins the ID a backfilled crash
// dump claims, and then pins the retraction it enables.
//
// THE MEASURED DEFECT (2026-10-20, this host's own tree): every orphan dump in
// the bare-session layout carried `"session_id": "session"` — 142 of them —
// because the writer derived the id from the record's BASENAME and that
// basename is the fixed `session.jsonl`. The reader that retires a dump once
// its session has recorded a `session_end` derived the id it expected from the
// DUMP's basename instead, which on `session.dump.json` is also "session": the
// two wrong values AGREE, so the check passed and nothing anywhere reported the
// false identity. A dump is evidence; one that names a session which does not
// exist cannot be reconciled, listed, or trusted.
//
// Both arms are asserted here because they are one defect: the WRITE must name
// the session (`<uuid>`), and the RETRACT must verify that claim against the
// record rather than against a name it computed from the dump itself.
func TestOrphanDumpNamesTheSessionsRealIdentity(t *testing.T) {
	const sessionID = "8616b46c-db23-41cb-a657-c3a519abd998"
	tmp := t.TempDir()
	t.Setenv(config.GopiDirEnv, tmp)
	sessionsDir := filepath.Join(tmp, ".gopi", "sessions")
	sessionDir := filepath.Join(sessionsDir, sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The BARE-session layout: the record is the fixed name `session.jsonl`,
	// and the uuid lives in its session_start row and in the directory name.
	record := filepath.Join(sessionDir, sessionlayout.RecordName)
	body := classifyStartLine(sessionID, deadPID) + "\n"
	if err := os.WriteFile(record, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	backdate(t, record, 2*60*60*1e9)

	if w, _ := BackfillOrphanCrashDumps(); w != 1 {
		t.Fatalf("BackfillOrphanCrashDumps wrote %d dump(s), want 1", w)
	}
	dumpPath := filepath.Join(sessionDir, "session.dump.json")
	raw, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("backfill dump missing: %v", err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("bad backfill dump: %v", err)
	}
	if got := doc["session_id"]; got != sessionID {
		t.Fatalf("dump claims session_id = %v, want the session's real id %q — a dump MUST NOT name the record's fixed file name", got, sessionID)
	}

	// THE RETRACTION ARM: once that session records its `session_end`, the dump
	// is a false claim and must be retired. It only is if the reader compares
	// the claim against the RECORD's identity — comparing against the dump's
	// own basename would make the wrong value agree with itself forever.
	if err := os.WriteFile(record, []byte(body+`{"type":"session_end","exit_code":0}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backdate(t, record, 2*60*60*1e9)
	if _, retracted := BackfillOrphanCrashDumps(); retracted != 1 {
		t.Fatalf("retracted = %d, want 1 — the session ended cleanly, so the orphan claim is false and must be retired", retracted)
	}
	if _, err := os.Stat(dumpPath); err == nil {
		t.Fatalf("the stale orphan dump survived the session's clean end: %s", dumpPath)
	}
}
