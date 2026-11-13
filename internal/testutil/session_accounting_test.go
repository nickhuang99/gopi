package testutil

// Session accounting: the unit LoadSession counts is the SESSION, not the
// *.jsonl file. This file is the regression guard for the pre-existing
// master-red case (cmd/agent's TestWebToolsSignalShutdownWritesSessionEnd and
// testsuite's TestTask_BarePrompt_SIGTERM_WritesSessionEnd both failed with
// "2 session files" for ONE session): every SIGTERM path legitimately writes
// <uuid>/signal-provenance.jsonl BESIDE <uuid>/session.jsonl — a side file of
// the SAME session (signalrec's provenance record for that process), never a
// second session.
//
// The fix is the accounting, never the file: deleting or renaming the
// provenance record would make these assertions green while destroying the
// evidence the post-mortem path exists to keep. So the guard has two halves:
//   - the single-session fixture (record + side file) must count ONE session;
//   - the leaked-session fixture (two session dirs) must still be caught —
//     otherwise "count sessions" would degrade into "count nothing".

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/signalrec"
)

const (
	// acctSessionID is the session UUID used by the fixtures.
	acctSessionID = "11111111-2222-4333-8444-555555555555"
	// acctOtherSessionID is a second, distinct session UUID (the leak case).
	acctOtherSessionID = "99999999-8888-4777-8666-555555555555"
	// acctUnknownSideFile is a *.jsonl that is NEITHER the session record nor
	// a known side file — it must be reported, not silently ignored.
	acctUnknownSideFile = "some-other-artifact.jsonl"
)

// captureT implements just enough of testing.TB to OBSERVE a helper's
// Fatalf/Logf instead of failing the test that runs the probe (the fixtures
// below assert on the diagnosis). A nil embedded testing.TB is never called:
// LoadSession only uses Helper, Fatalf and Logf.
//
// Fatalf must stop the caller exactly as testing.T's does (runtime.Goexit) —
// a fatal ends the helper, so a fake that returned instead would let the
// helper run past its own precondition and panic. It panics with a sentinel
// that captureFatal recovers; the recorded messages are the return value.
type captureT struct {
	testing.TB
	fatals  []string
	logLine []string
}

// fatalSentinel marks a recorded Fatalf (it never escapes captureFatal).
type fatalSentinel struct{ msg string }

func (c *captureT) Helper() {}
func (c *captureT) Logf(format string, args ...interface{}) {
	c.logLine = append(c.logLine, fmt.Sprintf(format, args...))
}

func (c *captureT) Fatalf(format string, args ...interface{}) {
	c.fatals = append(c.fatals, fmt.Sprintf(format, args...))
	panic(fatalSentinel{msg: fmt.Sprintf(format, args...)})
}

// Errorf records like Fatalf (no helper here aborts on a non-fatal error, and
// this package's assertions must not be silently swallowed).
func (c *captureT) Errorf(format string, args ...interface{}) {
	c.fatals = append(c.fatals, fmt.Sprintf(format, args...))
}

// captureFatal runs fn against a captureT and returns it, absorbing the
// sentinel a Fatalf raises. A real panic propagates.
func captureFatal(fn func(tb testing.TB)) *captureT {
	ct := &captureT{}
	func() {
		defer func() {
			r := recover()
			if r == nil {
				return
			}
			if _, ok := r.(fatalSentinel); ok {
				return
			}
			panic(r)
		}()
		fn(ct)
	}()
	return ct
}

// acctHome builds a sandbox GOPI_HOME and returns it with its sessions root.
func acctHome(t *testing.T) (home, root string) {
	t.Helper()
	home = t.TempDir()
	root = filepath.Join(home, ".gopi", config.SessionsDirName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir sessions root: %v", err)
	}
	return home, root
}

// writeSessionJSONL writes a minimal valid session JSONL (one session_start)
// at relPath under the sessions root.
func writeSessionJSONL(t *testing.T, root, relPath, sessionID string) string {
	t.Helper()
	p := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
	}
	line := fmt.Sprintf("{\"type\":%q,\"sessionId\":%q,\"timestamp\":\"2026-01-01T00:00:00Z\"}\n",
		sessionevent.EventTypeSessionStart, sessionID)
	if err := os.WriteFile(p, []byte(line), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// writeFile writes an arbitrary file, creating its parent dirs.
func writeFile(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// legacySessionFileCount replicates the PRE-FIX accounting (LoadSession
// before the side-file grouping): every *.jsonl under the sessions root, one
// level deep, counted as "a session file". It exists so the regression is
// pinned by an executable contrast rather than a comment — for the
// single-session fixture below it reports 2, and the test asserts it does, so
// restoring the file-count reintroduces a visible red here as well.
func legacySessionFileCount(root string) int {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	var files []string
	collect := func(d string, name string) {
		if filepath.Ext(name) == jsonlSuffix {
			files = append(files, filepath.Join(d, name))
		}
	}
	for _, e := range entries {
		if !e.IsDir() {
			collect(root, e.Name())
			continue
		}
		sub := filepath.Join(root, e.Name())
		subs, err := os.ReadDir(sub)
		if err != nil {
			continue
		}
		for _, se := range subs {
			if !se.IsDir() {
				collect(sub, se.Name())
			}
		}
	}
	return len(files)
}

// TestSessionAccounting_SideFileIsNotASession is the master-red regression:
// one bare session dir holding session.jsonl + signal-provenance.jsonl is ONE
// session, and LoadSession must say so rather than "2 session files".
func TestSessionAccounting_SideFileIsNotASession(t *testing.T) {
	home, root := acctHome(t)
	record := writeSessionJSONL(t, root, filepath.Join(acctSessionID, sessionRecordName), acctSessionID)
	var sess *Session
	// The side file the SIGTERM path writes — the production name, imported
	// from its owning package (no mirrored literal).
	side := writeFile(t, filepath.Join(root, acctSessionID, signalrec.FileName),
		"{\"kind\":\"signal\",\"signal\":\"SIGTERM\"}\n")

	refs, err := findSessionRecords(root)
	if err != nil {
		t.Fatalf("findSessionRecords: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("found %d sessions, want 1 — %s beside %s is a side file of the SAME session, not a second one: %v",
			len(refs), filepath.Base(side), filepath.Base(record), sessionRecordList(refs))
	}
	if refs[0].Path != record {
		t.Errorf("session record = %s, want the session.jsonl (%s)", refs[0].Path, record)
	}
	if len(refs[0].SideCar) != 1 || refs[0].SideCar[0] != signalrec.FileName {
		t.Errorf("side files = %v, want exactly [%s]", refs[0].SideCar, signalrec.FileName)
	}

	// The load-bearing contrast: the pre-fix accounting sees TWO files here.
	// If someone restores the file-count, this assertion is the tripwire.
	if got := legacySessionFileCount(root); got != 2 {
		t.Errorf("legacySessionFileCount = %d, want 2 — the fixture must still reproduce the old accounting (otherwise this guard no longer pins the regression)", got)
	}

	ct := captureFatal(func(tb testing.TB) { sess = LoadSession(tb, home) })
	if len(ct.fatals) != 0 {
		t.Fatalf("LoadSession failed on a single session: %v", ct.fatals)
	}
	if filepath.Base(sess.Path) != sessionRecordName {
		t.Errorf("loaded %s, want the session record", sess.Path)
	}
	if len(ct.logLine) == 0 {
		t.Errorf("LoadSession said nothing about the side file — a reader should be able to see why it was not counted")
	}
}

// TestSessionAccounting_LeakedSessionIsStillCaught is the other half: the
// grouping must not degrade into counting nothing. Two session dirs = two
// sessions = a leaked session, and the message names both.
func TestSessionAccounting_LeakedSessionIsStillCaught(t *testing.T) {
	home, root := acctHome(t)
	writeSessionJSONL(t, root, filepath.Join(acctSessionID, sessionRecordName), acctSessionID)
	writeSessionJSONL(t, root, filepath.Join(acctOtherSessionID, sessionRecordName), acctOtherSessionID)

	refs, err := findSessionRecords(root)
	if err != nil {
		t.Fatalf("findSessionRecords: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("found %d sessions, want 2 (two session dirs = a leaked session)", len(refs))
	}
	ct := captureFatal(func(tb testing.TB) { LoadSession(tb, home) })
	if len(ct.fatals) != 1 {
		t.Fatalf("LoadSession fatals = %v, want exactly one naming the leak", ct.fatals)
	}
	for _, id := range []string{acctSessionID, acctOtherSessionID} {
		if !strings.Contains(ct.fatals[0], id) {
			t.Errorf("leak message %q does not name session %s", ct.fatals[0], id)
		}
	}
}

// TestSessionAccounting_UnknownSideFileIsReported pins that the side-file
// allowance is a NAME list, not a blanket "extra *.jsonl is fine": an
// unrecognized file beside a session record is surfaced (visible in the
// assertion path), so a future artifact cannot slip in unnoticed.
func TestSessionAccounting_UnknownSideFileIsReported(t *testing.T) {
	_, root := acctHome(t)
	record := writeSessionJSONL(t, root, filepath.Join(acctSessionID, sessionRecordName), acctSessionID)
	writeFile(t, filepath.Join(root, acctSessionID, acctUnknownSideFile), "{}\n")

	refs, err := findSessionRecords(root)
	if err != nil {
		t.Fatalf("findSessionRecords: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("found %d sessions, want 1: %v", len(refs), sessionRecordList(refs))
	}
	if refs[0].Path != record {
		t.Errorf("session record = %s, want %s (the unknown file must never be taken as the record)", refs[0].Path, record)
	}
	list := sessionRecordList(refs)
	if !strings.Contains(list[0], acctUnknownSideFile) || !strings.Contains(list[0], "unknown") {
		t.Errorf("rendered sessions = %v, want the unknown .jsonl reported as unknown", list)
	}
}

// TestSessionAccounting_LayoutKeying pins the rule that makes the count
// unambiguous: inside a session DIR a stray *.jsonl is a side file (the dir
// already holds the record); directly under the sessions ROOT every *.jsonl
// IS a record (the flat layout has no dir for a side file to live in — so a
// bare signal-provenance.jsonl there is a stray and counts as a session).
func TestSessionAccounting_LayoutKeying(t *testing.T) {
	t.Run("flat layout counts each bare file", func(t *testing.T) {
		_, root := acctHome(t)
		writeSessionJSONL(t, root, acctSessionID+jsonlSuffix, acctSessionID)
		writeSessionJSONL(t, root, acctOtherSessionID+jsonlSuffix, acctOtherSessionID)
		refs, err := findSessionRecords(root)
		if err != nil {
			t.Fatalf("findSessionRecords: %v", err)
		}
		if len(refs) != 2 {
			t.Fatalf("flat layout: found %d sessions, want 2 (every flat *.jsonl is a record)", len(refs))
		}
	})

	t.Run("flat stray beside a session dir counts as a second session", func(t *testing.T) {
		_, root := acctHome(t)
		writeSessionJSONL(t, root, filepath.Join(acctSessionID, sessionRecordName), acctSessionID)
		// A bare *.jsonl at the ROOT is a legacy flat record (the flat layout
		// has no session dir for a side file to belong to), so it counts as a
		// second session — even when it wears a REGISTERED side-file name,
		// because here it sits beside a different session's dir: the registry
		// is consulted one level down, where a dir's membership means
		// something. Reporting it is the point: a stray is never excused.
		writeFile(t, filepath.Join(root, acctOtherSessionID+jsonlSuffix), "{}\n")
		refs, err := findSessionRecords(root)
		if err != nil {
			t.Fatalf("findSessionRecords: %v", err)
		}
		if len(refs) != 2 {
			t.Fatalf("found %d sessions, want 2 (the flat stray is reported, never name-excused): %v", len(refs), sessionRecordList(refs))
		}
	})

	t.Run("task-run dir holds one record in the run file", func(t *testing.T) {
		_, root := acctHome(t)
		want := writeSessionJSONL(t, root, filepath.Join("task-abc", acctSessionID+jsonlSuffix), acctSessionID)
		refs, err := findSessionRecords(root)
		if err != nil {
			t.Fatalf("findSessionRecords: %v", err)
		}
		if len(refs) != 1 || refs[0].Path != want {
			t.Fatalf("task-run dir: refs = %v, want the single run file", sessionRecordList(refs))
		}
	})

	t.Run("process-scoped provenance dir is not a session", func(t *testing.T) {
		home, root := acctHome(t)
		// A resident role with no task writes sessions/process-<pid>/signal-
		// provenance.jsonl and no session record — nothing to load there.
		writeFile(t, filepath.Join(root, signalrec.ProcessDirPrefix+"4242", signalrec.FileName), "{}\n")
		refs, err := findSessionRecords(root)
		if err != nil {
			t.Fatalf("findSessionRecords: %v", err)
		}
		if len(refs) != 0 {
			t.Fatalf("found %d sessions, want 0 (a provenance dir with no record is not a session): %v", len(refs), sessionRecordList(refs))
		}
		ct := captureFatal(func(tb testing.TB) { LoadSession(tb, home) })
		if len(ct.fatals) != 1 || !strings.Contains(ct.fatals[0], "no session record") {
			t.Fatalf("LoadSession fatals = %v, want 'no session record'", ct.fatals)
		}
	})
}

// TestSessionFixtureCountsOperatorTurnsNotHarnessRows is the minimal repro for
// the 2026-10-12 suite red (TestChatCookedFallback_NonTTY in cmd/agent and
// TestChat_HappyPath_TwoTurnsSameSession in testsuite, both on a clean base).
//
// ONE synthetic session record, exactly the shape a piped two-turn `gopi chat`
// leaves behind:
//
//   - ONE operator turn ("hello there");
//   - the harness TURN-STAMP row the request assembly wrote for that same turn
//     (internal/agent/clock.go's turnStampLine, which runs on every request);
//   - the per-run memory-prefetch record, which the chat executor opens with
//     O_CREATE|O_APPEND for the whole run because the knob is DEFAULT ON.
//
// Two pre-fix readings of that record were both wrong, and this test pins each
// one so a regression cannot come back silently:
//
//  1. the prefetch record read as "an unknown .jsonl" next to the session
//     (sessionlayout's registry did not name it), i.e. a feature working as
//     designed looked like a stray artifact;
//  2. the fixture counted the harness stamp as one of the OPERATOR's turns, so
//     two turns read as four.
//
// The fix is the classification and the accounting, never the files: both are
// legitimate artifacts of the same session, and deleting either would make the
// assertion green while destroying evidence the record exists to keep.
func TestSessionFixtureCountsOperatorTurnsNotHarnessRows(t *testing.T) {
	home, root := acctHome(t)
	dir := filepath.Join(root, acctSessionID)
	body := strings.Join([]string{
		fmt.Sprintf("{\"type\":%q,\"sessionId\":%q,\"timestamp\":\"2026-01-01T00:00:00Z\"}",
			sessionevent.EventTypeSessionStart, acctSessionID),
		`{"type":"user","promptId":"p1","message":{"role":"user","content":"hello there"}}`,
		`{"type":"user","promptId":"p2","message":{"role":"user","content":"` +
			sessionevent.HarnessMessagePrefix + `[t+0s] — first request of this session."}}`,
	}, "\n") + "\n"
	writeFile(t, filepath.Join(dir, sessionRecordName), body)
	// The prefetch record: empty is the normal state of a short run (the
	// executor creates it at start and appends one line per prefetch run).
	writeFile(t, filepath.Join(dir, config.MemoryPrefetchRecordName), "")

	// (1) The prefetch record is a REGISTERED side file of this session, so a
	// reader that lists side files names it — and names nothing as unknown.
	if !sessionlayout.IsSideFile(config.MemoryPrefetchRecordName) {
		t.Errorf("config.MemoryPrefetchRecordName = %q is not registered as a session side file — the record reads as a stray again",
			config.MemoryPrefetchRecordName)
	}
	if subs, err := os.ReadDir(dir); err == nil && len(subs) == 0 {
		t.Fatalf("fixture wrote nothing to %s", dir)
	}
	for _, name := range loadSessionSideFiles(dir) {
		if strings.Contains(name, "unknown") {
			t.Errorf("side file reported unknown: %v (registry: %v)", loadSessionSideFiles(dir), sessionlayout.SideFileNames())
		}
	}

	// (2) ONE session, and the OPERATOR's turns are what the fixture reports.
	sess := LoadSession(t, home)
	sess.AssertSingleSession(t)
	if got := sess.UserContents(); len(got) != 1 || got[0] != "hello there" {
		t.Errorf("UserContents = %q, want the single operator turn [hello there]", got)
	}
	if got := sess.HarnessUserContents(); len(got) != 1 {
		t.Errorf("HarnessUserContents = %q, want the one harness stamp row", got)
	}
	if got := sess.AllUserContents(); len(got) != 2 {
		t.Errorf("AllUserContents = %q, want BOTH rows (the operator's turn and the stamp)", got)
	}
	// The census a test asserts on must agree with the accessor it reads.
	sess.AssertCounts(t, map[string]int{eventTypeUser: 1})
	if got := sess.Counts()[eventTypeUser]; got != 2 {
		t.Errorf("raw user rows = %d, want 2 — Counts stays the RAW census; the turn reading lives in AssertCounts/UserContents", got)
	}
}

// TestSessionRecordPath pins the readiness probe used by spawned-binary tests:
// "" until a record exists, then the record itself — a lone side file or an
// absent tree is never "ready".
func TestSessionRecordPath(t *testing.T) {
	home, root := acctHome(t)
	if got := SessionRecordPath(filepath.Join(home, "nope")); got != "" {
		t.Errorf("missing root: SessionRecordPath = %q, want \"\"", got)
	}
	if got := SessionRecordPath(root); got != "" {
		t.Errorf("empty root: SessionRecordPath = %q, want \"\"", got)
	}
	writeFile(t, filepath.Join(root, acctSessionID, signalrec.FileName), "{}\n")
	if got := SessionRecordPath(root); got != "" {
		t.Errorf("side file only: SessionRecordPath = %q, want \"\" (a side file is not a session)", got)
	}
	want := writeSessionJSONL(t, root, filepath.Join(acctSessionID, sessionRecordName), acctSessionID)
	if got := SessionRecordPath(root); got != want {
		t.Errorf("SessionRecordPath = %q, want %q", got, want)
	}
}
