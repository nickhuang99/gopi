package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

// Regression tests for the startup orphan backfill's false positives.
//
// Before this change BackfillOrphanCrashDumps dumped EVERY session JSONL
// without a session_end tail (excluding only the process's own session), so
// a LIVE sibling session — and every session of a long batch — was written
// up as a crash at each startup ("backfilled 14 orphan session dump(s)"),
// and a dump whose session later ended cleanly was never retracted. The two
// guards under test: the live-window mtime probe and the reconciliation
// pass.

const testOrphanAge = 2 * time.Hour // well past orphanLiveWindow (15m)

// orphanTestSessionsDir creates an isolated GOPI_HOME and returns its
// sessions tree.
func orphanTestSessionsDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv(config.GopiDirEnv, tmp)
	dir := filepath.Join(tmp, ".gopi", config.SessionsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeSessionJSONL writes a session JSONL into dir with an mtime backdated by
// age (0 = written just now, i.e. a LIVE session), returning its path.
func writeSessionJSONL(t *testing.T, dir, name, content string, age time.Duration) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	backdate(t, path, age)
	return path
}

// backdate ages a file's mtime — the input the live-window probe reads.
func backdate(t *testing.T, path string, age time.Duration) {
	t.Helper()
	mt := time.Now().Add(-age)
	if err := os.Chtimes(path, mt, mt); err != nil {
		t.Fatal(err)
	}
}

// dumpDocOf reads a dump file into a map (test-local JSON reader).
func dumpDocOf(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read dump %s: %v", path, err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("dump %s is not valid JSON: %v", path, err)
	}
	return doc
}

// TestBackfill_OrphanLiveWindowAndReconcile is the epic's acceptance test,
// covering the three required cases on ONE session file:
//
//	(i)   fresh mtime, no session_end   → 0 dumps (a live session)
//	(ii)  the same file once QUIET      → 1 dump  (a dead session)
//	(iii) session_end appended later    → the stale dump is retracted
func TestBackfill_OrphanLiveWindowAndReconcile(t *testing.T) {
	dir := orphanTestSessionsDir(t)
	jsonl := writeSessionJSONL(t, dir, "session-1.jsonl", deadStampStartLine("session-1"), 0)
	dump := strings.TrimSuffix(jsonl, sessionJSONLSuffix) + dumpFileSuffix

	// (i) LIVE: the JSONL was written this instant. No session_end is
	// EXPECTED here — reporting it would be the false positive.
	if got := DetectOrphanSessions(); len(got) != 0 {
		t.Fatalf("live session detected as orphan: %v (the live window must exclude a file written just now)", got)
	}
	if written, retracted := BackfillOrphanCrashDumps(); written != 0 || retracted != 0 {
		t.Fatalf("Backfill on a live session = (%d, %d), want (0, 0)", written, retracted)
	}
	if _, err := os.Stat(dump); err == nil {
		t.Fatalf("live session was dumped: %s", dump)
	}

	// (ii) DEAD: same file, same content — only the mtime moved past the
	// window (exactly what kill -9 does: the JSONL stops being written).
	backdate(t, jsonl, testOrphanAge)
	written, retracted := BackfillOrphanCrashDumps()
	if written != 1 || retracted != 0 {
		t.Fatalf("Backfill on a quiet session = (%d, %d), want (1, 0)", written, retracted)
	}
	doc := dumpDocOf(t, dump)
	if doc["orphan"] != true || doc["category"] != orphanDumpCategory || doc["session_id"] != "session-1" {
		t.Fatalf("orphan dump = %v", doc)
	}
	if _, ok := doc["crash"]; ok {
		t.Fatalf("orphan dump claims a crash category: %v", doc)
	}

	// (iii) RECONCILE: the session ended cleanly after the dump was written.
	f, err := os.OpenFile(jsonl, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(endLine); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	written, retracted = BackfillOrphanCrashDumps()
	if written != 0 || retracted != 1 {
		t.Fatalf("Backfill after session_end = (%d, %d), want (0, 1) — the stale dump must be retracted", written, retracted)
	}
	if _, err := os.Stat(dump); err == nil {
		t.Fatalf("stale dump survived reconciliation: %s", dump)
	}
	// Idempotent: nothing left to write or retract.
	if written, retracted = BackfillOrphanCrashDumps(); written != 0 || retracted != 0 {
		t.Fatalf("repeat Backfill = (%d, %d), want (0, 0)", written, retracted)
	}
}

// TestBackfill_TrueKill9StaysVisible: the guard must not cost a REAL crash
// its dump. A session killed mid-run never gets a session_end and its mtime
// freezes — so it stays dumped across repeated startups (never retracted).
func TestBackfill_TrueKill9StaysVisible(t *testing.T) {
	dir := orphanTestSessionsDir(t)
	jsonl := writeSessionJSONL(t, dir, "killed.jsonl", deadStampStartLine("killed"), testOrphanAge)
	dump := strings.TrimSuffix(jsonl, sessionJSONLSuffix) + dumpFileSuffix

	for round := 1; round <= 2; round++ {
		written, retracted := BackfillOrphanCrashDumps()
		if retracted != 0 {
			t.Fatalf("round %d retracted a genuine crash dump (kill -9 visibility lost)", round)
		}
		if round == 1 && written != 1 {
			t.Fatalf("round %d written = %d, want 1", round, written)
		}
		if round == 2 && written != 0 {
			t.Fatalf("round %d rewritten — not idempotent", round)
		}
		if _, err := os.Stat(dump); err != nil {
			t.Fatalf("round %d: genuine crash dump vanished: %v", round, err)
		}
	}
}

// TestBackfill_ReconcileSparesForeignDumps: reconciliation deletes ONLY the
// orphan backfills. An error-path crash dump (a taxonomy category, no orphan
// marker) and a hand-written dump sitting next to an ended session must both
// survive — one malformed reader must not be able to wipe evidence. The
// LEGACY orphan dump shape (category "crash", written before the retraction
// existed) is the one exception: those files are the false-positive backlog
// and must be cleared.
func TestBackfill_ReconcileSparesForeignDumps(t *testing.T) {
	dir := orphanTestSessionsDir(t)
	ended := deadStampStartLine("ended") + endLine

	// An error-path dump: session.go's WriteCrashDump shape (no "orphan") —
	// its category may well be the legacy "crash" marker, so the orphan flag
	// is what protects it.
	path := writeSessionJSONL(t, dir, "errdump.jsonl", ended, testOrphanAge)
	errDump := strings.TrimSuffix(path, sessionJSONLSuffix) + dumpFileSuffix
	if err := os.WriteFile(errDump, []byte(`{"type":"`+crashDumpDocType+`","session_id":"errdump","category":"`+legacyOrphanDumpCategory+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// A hand-written doc that merely mentions "orphan".
	path2 := writeSessionJSONL(t, dir, "hand.jsonl", ended, testOrphanAge)
	handDump := strings.TrimSuffix(path2, sessionJSONLSuffix) + dumpFileSuffix
	if err := os.WriteFile(handDump, []byte(`{"note":"orphan","category":"`+orphanDumpCategory+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// A dump naming a DIFFERENT session than the JSONL beside it.
	path3 := writeSessionJSONL(t, dir, "mismatch.jsonl", ended, testOrphanAge)
	mismatchDump := strings.TrimSuffix(path3, sessionJSONLSuffix) + dumpFileSuffix
	if err := os.WriteFile(mismatchDump, []byte(`{"type":"`+crashDumpDocType+`","session_id":"somebody-else","category":"`+orphanDumpCategory+`","orphan":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// A malformed dump (not JSON) next to an ended session.
	path4 := writeSessionJSONL(t, dir, "broken.jsonl", ended, testOrphanAge)
	brokenDump := strings.TrimSuffix(path4, sessionJSONLSuffix) + dumpFileSuffix
	if err := os.WriteFile(brokenDump, []byte("not json at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	if written, retracted := BackfillOrphanCrashDumps(); written != 0 || retracted != 0 {
		t.Fatalf("Backfill = (%d, %d), want (0, 0)", written, retracted)
	}
	for _, p := range []string{errDump, handDump, mismatchDump, brokenDump} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("reconciliation deleted a foreign dump: %s (%v)", p, err)
		}
	}
}

// TestBackfill_ReconcileClearsLegacyOrphanDumps: the ~118 dumps already on
// disk were written by the OLD backfill — category "crash" AND session_id
// holding the JSONL basename ("legacy.jsonl", not "legacy") — and they cannot
// be rewritten. Verified against the real tree: staging 25 of the actual
// stale dump/JSONL pairs (every one carrying both legacy quirks) and running
// the real binary retracted all 25. A fix that understood only the new
// spelling would leave the whole false-positive backlog in place.
func TestBackfill_ReconcileClearsLegacyOrphanDumps(t *testing.T) {
	dir := orphanTestSessionsDir(t)
	jsonl := writeSessionJSONL(t, dir, "legacy.jsonl", deadStampStartLine("legacy")+endLine, testOrphanAge)
	dump := strings.TrimSuffix(jsonl, sessionJSONLSuffix) + dumpFileSuffix
	legacy := `{"type":"` + crashDumpDocType + `","session_id":"legacy.jsonl","category":"` + legacyOrphanDumpCategory + `","orphan":true,"recoverable_hint":true}`
	if err := os.WriteFile(dump, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	written, retracted := BackfillOrphanCrashDumps()
	if written != 0 || retracted != 1 {
		t.Fatalf("Backfill = (%d, %d), want (0, 1) — the legacy orphan dump must be cleared", written, retracted)
	}
	if _, err := os.Stat(dump); err == nil {
		t.Fatalf("legacy orphan dump survived reconciliation: %s", dump)
	}
	// The current writer's own spelling (bare id, new category) must clear too.
	if err := os.WriteFile(dump, []byte(`{"type":"`+crashDumpDocType+`","session_id":"legacy","category":"`+orphanDumpCategory+`","orphan":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if written, retracted := BackfillOrphanCrashDumps(); written != 0 || retracted != 1 {
		t.Fatalf("Backfill on the current spelling = (%d, %d), want (0, 1)", written, retracted)
	}
}

// TestBackfill_ReconcileSparesOwnLiveSession: the CURRENT process's session is
// excluded from the scan, and its dump (if any) must not be retracted by the
// same call — an exclusion must be symmetric across write and retract.
func TestBackfill_ReconcileSparesOwnLiveSession(t *testing.T) {
	dir := orphanTestSessionsDir(t)
	jsonl := writeSessionJSONL(t, dir, "self.jsonl", deadStampStartLine("self"), testOrphanAge)
	dump := strings.TrimSuffix(jsonl, sessionJSONLSuffix) + dumpFileSuffix

	// First round (no exclusion): dumped.
	if written, _ := BackfillOrphanCrashDumps(); written != 1 {
		t.Fatalf("written = %d, want 1", written)
	}
	// The session ends cleanly AND is declared as self: keep the dump, stay
	// out of the scan entirely.
	f, err := os.OpenFile(jsonl, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(endLine); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if written, retracted := BackfillOrphanCrashDumps("self"); written != 0 || retracted != 0 {
		t.Fatalf("Backfill(self) = (%d, %d), want (0, 0)", written, retracted)
	}
	if _, err := os.Stat(dump); err != nil {
		t.Fatalf("excluded session's dump was retracted: %v", err)
	}
}

// TestOrphanLiveWindowEffective: the window is the named default and the env
// override is the only reader (Rule 2), including the "0 = default" and
// invalid spellings.
func TestOrphanLiveWindowEffective(t *testing.T) {
	if got := orphanLiveWindowEffective(); got != orphanLiveWindow {
		t.Fatalf("default window = %v, want %v", got, orphanLiveWindow)
	}
	t.Setenv(orphanLiveWindowSecEnv, "60")
	if got := orphanLiveWindowEffective(); got != time.Minute {
		t.Fatalf("override = %v, want 1m", got)
	}
	t.Setenv(orphanLiveWindowSecEnv, "0")
	if got := orphanLiveWindowEffective(); got != orphanLiveWindow {
		t.Fatalf("0 must mean default, got %v", got)
	}
	t.Setenv(orphanLiveWindowSecEnv, "not-a-number")
	if got := orphanLiveWindowEffective(); got != orphanLiveWindow {
		t.Fatalf("invalid must fall back to default, got %v", got)
	}
}
