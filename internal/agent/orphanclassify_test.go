package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/procstat"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// The classification matrix (epic: startup crash-dump backfill
// misreporting). Every cell is asserted separately, because the original bug
// was exactly one collapsed cell: "no session_end in the tail" was treated as
// a synonym for "crashed", which made a live session and a cleanly-ended-but-
// unstamped session look identical to a kill -9.
//
//	clause 1 \ clause 2      | session_end present | session_end absent
//	------------------------+---------------------+------------------------
//	process ALIVE            | live  (not a crash) | live  (not a crash)
//	process GONE             | live  (not a crash) | CRASHED
//	liveness UNKNOWN         | unknown (silent)    | unknown (silent)
//
// Fixtures are real session files written through the production logger
// where the shape matters (session_start + stamp), and hand-written JSONL
// where the test needs a shape the logger cannot produce (a stamp pointing
// at a dead PID, a truncated tail, an unreadable file).

// deadPID is a PID that provably does not exist: the maximum PID on a Linux
// system is below pidMaxLinux, and no process is ever assigned a value above
// it (kernel.pid_max defaults to 4194304, 2^22). Using max+1 keeps the test
// deterministic — a live "reaped but unreaped" zombie would be a false
// negative, so we must not pick a plausible-but-maybe-live number.
const (
	pidMaxLinux = 4194304
	deadPID     = pidMaxLinux + 1
)

// sessionLine renders one JSONL session event line.
func sessionLine(fields map[string]interface{}) string {
	b, _ := json.Marshal(fields) //stylegate:ignore marshalling a fixture map built in-process cannot fail (no channels/funcs reach this value)
	return string(b) + "\n"
}

// writeSessionFixture writes a session JSONL into a fresh gopi dir and
// returns the file path, its run dir, and the sessions root.
func writeSessionFixture(t *testing.T, name string, lines ...string) (string, string, string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv(config.GopiDirEnv, tmp)
	sessionsDir := filepath.Join(tmp, ".gopi", config.SessionsDirName)
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sessionsDir, name)
	content := ""
	for _, l := range lines {
		content += l
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, sessionsDir, tmp
}

// startLine builds a session_start event, optionally carrying the liveness
// stamp (pid 0 = the pre-fix shape with no stamp at all).
func classifyStartLine(sessionID string, pid int) string {
	f := map[string]interface{}{
		"type":      sessionevent.EventTypeSessionStart,
		"sessionId": sessionID,
		"model":     "test-model",
	}
	if pid > 0 {
		f[ProcessAliveStampField] = pid
	}
	return sessionLine(f)
}

// endLine builds a clean session_end event.
func classifyEndLine(exitCode int) string {
	return sessionLine(map[string]interface{}{
		"type":      sessionevent.EventTypeSessionEnd,
		"exit_code": exitCode,
	})
}

// --- the five matrix rows -------------------------------------------------

// TestClassifyOrphan_LiveSessionWithDeadWriterPID is the LAST row of the
// matrix: the stamp names a process that is gone and no session_end exists —
// the genuine kill -9 case, the ONLY combination a dump may report.
func TestClassifyOrphan_LiveSessionWithDeadWriterPID(t *testing.T) {
	path, _, _ := writeSessionFixture(t, "crashed.jsonl",
		classifyStartLine("crashed", deadPID))

	v := ClassifyOrphanSession(path)
	if v.Status != OrphanCrashed {
		t.Fatalf("status = %q (%s), want %q", v.Status, v.Reason, OrphanCrashed)
	}
}

// TestClassifyOrphan_LivePIDIsNotACrash is THE regression test for the epic:
// a session whose writer process is still alive has no session_end because it
// is STILL BEING WRITTEN. The old predicate called this a crash and printed
// it in the startup count.
func TestClassifyOrphan_LivePIDIsNotACrash(t *testing.T) {
	path, _, _ := writeSessionFixture(t, "live.jsonl",
		classifyStartLine("live", os.Getpid())) // this test process = demonstrably alive

	v := ClassifyOrphanSession(path)
	if v.Status != OrphanLive {
		t.Fatalf("status = %q (%s), want %q — a live writer must never be reported as a crash",
			v.Status, v.Reason, OrphanLive)
	}
}

// TestClassifyOrphan_CleanEndIsNotACrash: the process died (or was never
// recorded) but the session wrote session_end — it ended cleanly, so it is
// not a crash. This is the second half of the 117-dump misreport.
func TestClassifyOrphan_CleanEndIsNotACrash(t *testing.T) {
	path, _, _ := writeSessionFixture(t, "clean.jsonl",
		classifyStartLine("clean", deadPID), classifyEndLine(0))

	v := ClassifyOrphanSession(path)
	if v.Status != OrphanLive {
		t.Fatalf("status = %q (%s), want %q — a session with session_end ended cleanly",
			v.Status, v.Reason, OrphanLive)
	}
}

// TestClassifyOrphan_UnstampedIsUnknownNotCrash: a session written before the
// liveness stamp existed (all pre-fix logs) cannot be classified. Rule 15: it
// must be SILENT, never counted as a crash — the absence of evidence is not
// evidence of a crash.
func TestClassifyOrphan_UnstampedIsUnknownNotCrash(t *testing.T) {
	path, _, _ := writeSessionFixture(t, "unstamped.jsonl", classifyStartLine("unstamped", 0))

	v := ClassifyOrphanSession(path)
	if v.Status != OrphanUnknown {
		t.Fatalf("status = %q (%s), want %q", v.Status, v.Reason, OrphanUnknown)
	}
	if v.Status == OrphanCrashed {
		t.Fatal("an unstamped session must never be reported as a crash")
	}
}

// TestClassifyOrphan_UnreadableIsUnknown: a missing/undecodable session file
// yields unknown (never a crash).
func TestClassifyOrphan_UnreadableIsUnknown(t *testing.T) {
	tmp := t.TempDir()
	missing := filepath.Join(tmp, "does-not-exist.jsonl")
	if v := ClassifyOrphanSession(missing); v.Status != OrphanUnknown {
		t.Fatalf("missing file status = %q (%s), want %q", v.Status, v.Reason, OrphanUnknown)
	}

	t.Setenv(config.GopiDirEnv, tmp)
	sessionsDir := filepath.Join(tmp, ".gopi", config.SessionsDirName)
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	garbage := filepath.Join(sessionsDir, "garbage.jsonl")
	if err := os.WriteFile(garbage, []byte("not json at all\x00\xff\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v := ClassifyOrphanSession(garbage); v.Status != OrphanUnknown {
		t.Fatalf("garbage file status = %q (%s), want %q", v.Status, v.Reason, OrphanUnknown)
	}
}

// TestClassifyOrphan_TruncatedTail: a session whose LAST LINE was cut off
// mid-write (the SIGKILL signature) but whose writer is gone and which never
// wrote session_end IS a crash — truncation must not push the verdict to
// unknown, because the evidence (gone writer + no clean end) is complete.
func TestClassifyOrphan_TruncatedTail(t *testing.T) {
	path, _, _ := writeSessionFixture(t, "truncated.jsonl",
		classifyStartLine("truncated", deadPID),
		`{"type":"assistant","content":"half a li`) // no newline, no session_end

	v := ClassifyOrphanSession(path)
	if v.Status != OrphanCrashed {
		t.Fatalf("status = %q (%s), want %q", v.Status, v.Reason, OrphanCrashed)
	}
}

// TestClassifyOrphan_TruncatedTailLive is the other half of the truncation
// cell: the same truncated file, but the writer is alive (the read happened
// mid-append). Not a crash.
func TestClassifyOrphan_TruncatedTailLive(t *testing.T) {
	path, _, _ := writeSessionFixture(t, "trunc-live.jsonl",
		classifyStartLine("trunc-live", os.Getpid()),
		`{"type":"assistant","content":"half a li`)

	if v := ClassifyOrphanSession(path); v.Status != OrphanLive {
		t.Fatalf("status = %q (%s), want %q", v.Status, v.Reason, OrphanLive)
	}
}

// TestClassifyOrphan_ProbeContract pins the procstat three-valued contract at
// the boundary the classifier depends on: an impossible PID reads GONE, our
// own PID reads ALIVE, and "no PID" reads UNKNOWN (never GONE — that is what
// would turn every unstamped session into a crash).
func TestClassifyOrphan_ProbeContract(t *testing.T) {
	if got := procstat.Alive(deadPID); got != procstat.VerdictGone {
		t.Fatalf("Alive(deadPID) = %q, want %q", got, procstat.VerdictGone)
	}
	if got := procstat.Alive(os.Getpid()); got != procstat.VerdictAlive {
		t.Fatalf("Alive(self) = %q, want %q", got, procstat.VerdictAlive)
	}
	for _, pid := range []int{0, -1} {
		if got := procstat.Alive(pid); got != procstat.VerdictUnknown {
			t.Fatalf("Alive(%d) = %q, want %q (absent PID is missing evidence, not death)",
				pid, got, procstat.VerdictUnknown)
		}
	}
}

// --- the detector must honour the matrix, not just the classifier ---------

// TestDetectOrphanSessions_Matrix drives the REAL scan over all five fixture
// kinds at once and asserts that exactly the crashed one is reported.
func TestDetectOrphanSessions_Matrix(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(config.GopiDirEnv, tmp)
	sessionsDir := filepath.Join(tmp, ".gopi", config.SessionsDirName)
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixtures := map[string]string{
		"live.jsonl":      classifyStartLine("live", os.Getpid()),
		"clean.jsonl":     classifyStartLine("clean", deadPID) + classifyEndLine(0),
		"unstamped.jsonl": classifyStartLine("unstamped", 0),
		"crashed.jsonl":   classifyStartLine("crashed", deadPID),
		"truncated.jsonl": classifyStartLine("truncated", deadPID) + `{"type":"assistant"`, // no newline
		"garbage.jsonl":   "not json\n",
	}
	for name, content := range fixtures {
		path := filepath.Join(sessionsDir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		// Quiet past the live window: the sweep classifies only files nobody
		// is writing any more (a fresh JSONL is a live session, not a crash).
		backdate(t, path, testOrphanAge)
	}

	orphans := DetectOrphanSessions()
	if len(orphans) != 2 {
		t.Fatalf("orphans = %v, want exactly [crashed.jsonl truncated.jsonl]", orphans)
	}
	got := map[string]bool{}
	for _, o := range orphans {
		got[filepath.Base(o)] = true
	}
	for _, want := range []string{"crashed.jsonl", "truncated.jsonl"} {
		if !got[want] {
			t.Errorf("%s missing from orphans %v", want, orphans)
		}
	}
	for _, forbidden := range []string{"live.jsonl", "clean.jsonl", "unstamped.jsonl", "garbage.jsonl"} {
		if got[forbidden] {
			t.Errorf("%s reported as a crash — it must be silent", forbidden)
		}
	}
}

// TestBackfillOrphanCrashDumps_RetiresStaleDump is the reconciliation test:
// a dump written for a session that LATER ended cleanly (the 117-dump class)
// must be retired, not left asserting a crash forever.
func TestBackfillOrphanCrashDumps_RetiresStaleDump(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(config.GopiDirEnv, tmp)
	sessionsDir := filepath.Join(tmp, ".gopi", config.SessionsDirName)
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The session ended cleanly, but an earlier (buggy) sweep left a dump.
	jsonl := filepath.Join(sessionsDir, "clean.jsonl")
	if err := os.WriteFile(jsonl, []byte(deadStampStartLine("clean")+classifyEndLine(0)), 0o644); err != nil {
		t.Fatal(err)
	}
	backdate(t, jsonl, testOrphanAge) // quiet past the live window
	dump := strings_TrimSuffixHelper(jsonl) + dumpFileSuffix
	if err := os.WriteFile(dump, []byte(`{"type":"`+crashDumpDocType+`","session_id":"clean","category":"`+orphanDumpCategory+`","orphan":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// BackfillOrphanCrashDumps returns (written, retracted): nothing crashed,
	// so nothing is written — the stale dump is retracted by the reconciler.
	if w, r := BackfillOrphanCrashDumps(); w != 0 || r != 1 {
		t.Fatalf("backfill = (written %d, retracted %d), want (0, 1) (nothing crashed; the stale dump is retracted)", w, r)
	}
	if _, err := os.Stat(dump); !os.IsNotExist(err) {
		t.Fatalf("stale dump survived: %v (a session that ended cleanly must not keep a crash record)", err)
	}
}

// TestBackfillOrphanCrashDumps_CountsOnlyCrashes: the reported number is the
// number of PROVEN crashes — live and unknown sessions contribute zero, so a
// plain start on a healthy tree prints nothing.
func TestBackfillOrphanCrashDumps_CountsOnlyCrashes(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(config.GopiDirEnv, tmp)
	sessionsDir := filepath.Join(tmp, ".gopi", config.SessionsDirName)
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A healthy tree: a live session + a cleanly-ended one + a legacy
	// unstamped one. The pre-fix code reported 2 of these 3 as crashes.
	for name, content := range map[string]string{
		"live.jsonl":      classifyStartLine("live", os.Getpid()),
		"clean.jsonl":     deadStampStartLine("clean") + classifyEndLine(0),
		"unstamped.jsonl": classifyStartLine("unstamped", 0),
	} {
		path := filepath.Join(sessionsDir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		backdate(t, path, testOrphanAge) // quiet past the live window
	}
	if w, r := BackfillOrphanCrashDumps(); w != 0 || r != 0 {
		t.Fatalf("backfill on a healthy tree = (written %d, retracted %d), want (0, 0) (the phantom-count regression)", w, r)
	}
	if w, r := BackfillOrphanCrashDumps(); w != 0 || r != 0 {
		t.Fatalf("second backfill = (written %d, retracted %d), want (0, 0)", w, r)
	}
}

// TestBackfillOrphanCrashDumps_ReportsOnlyTheKilledSession: the acceptance
// case — a genuinely killed session is named (dump written), the live ones
// are not.
func TestBackfillOrphanCrashDumps_ReportsOnlyTheKilledSession(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(config.GopiDirEnv, tmp)
	sessionsDir := filepath.Join(tmp, ".gopi", config.SessionsDirName)
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"alive.jsonl": classifyStartLine("alive", os.Getpid()),
		"killed.jsonl": deadStampStartLine("killed") +
			sessionLine(map[string]interface{}{"type": "assistant"}),
	} {
		path := filepath.Join(sessionsDir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		backdate(t, path, testOrphanAge) // quiet past the live window
	}
	if w, r := BackfillOrphanCrashDumps(); w != 1 || r != 0 {
		t.Fatalf("backfill = (written %d, retracted %d), want (1, 0) (only the killed session)", w, r)
	}
	killedDump := filepath.Join(sessionsDir, "killed"+dumpFileSuffix)
	if _, err := os.Stat(killedDump); err != nil {
		t.Fatalf("killed session dump missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionsDir, "alive"+dumpFileSuffix)); !os.IsNotExist(err) {
		t.Fatalf("live session got a dump: %v", err)
	}
}

// strings_TrimSuffixHelper keeps the test local to path arithmetic the
// production mapping performs (orphanDumpPath works on the JSONL path).
func strings_TrimSuffixHelper(p string) string {
	return strings.TrimSuffix(p, sessionlayout.JSONLSuffix)
}

var _ = fmt.Sprintf // fmt kept available for fixture diagnostics

// deadStampStartLine is a session_start event whose liveness stamp names a
// PID that provably does not exist. Fixtures that need a dead writer use it;
// the classify* helpers above are the flexible form.
func deadStampStartLine(sessionID string) string {
	return classifyStartLine(sessionID, deadPID)
}
