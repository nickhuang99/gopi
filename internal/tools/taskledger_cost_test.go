package tools

// COST + BEHAVIOUR contract for the LEDGER-NAMED session directory
// (2026-09-15) — the shape that made the earlier address fix inert.
//
// MEASURED DEFECT (operator's live ledger): a ledger row's session_id is the
// session that SUBMITTED it, and a worker session executes MANY rows, so four
// in_progress rows resolved to ONE submitter session whose log lives in a
// directory named by ANOTHER task id. Both layouts a row can name from its own
// columns (— <session-id>/session.jsonl and <row-task-id>/<session-id>.jsonl)
// missed, so every probe fell through to the whole-sessions-tree scan and the
// view still cost 6.1-12.5s (strace: 151,892 getdents64 / 75,959 openat) with
// the audit layer's own view of the same ledger (`task show`) answering in
// milliseconds.
//
// The tests below pin BOTH halves: the ledger-named directory is reached by
// lookup (ZERO tree walks), and it is authoritative while the scan stays the
// fallback — because a cost fix that silently narrowed what resolves would be a
// behaviour change wearing a perf commit's clothes.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/flaketest"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// hintFixtureLedger points the tasks layer at a private ledger for the test.
// The hint read resolves its DB through memoryDBPath() (GOPI_MEMORY_DB > the
// legacy alias > the default), so the env var is what isolates it.
func hintFixtureLedger(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hint-ledger.db")
	t.Setenv(memoryDBEnv, path)
	return path
}

// seedSubmitterRow plants the session's OWN row — the row whose task id names
// the directory holding that session's log (task-session-log-path).
func seedSubmitterRow(t *testing.T, taskID, sessionID string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary:   "hint fixture session row " + taskID,
		Payload:   "ledger-named session directory fixture",
		TaskID:    taskID,
		SessionID: sessionID,
		Source:    "human",
	}); err != nil {
		t.Fatalf("seed submitter row %s: %v", taskID, err)
	}
	if hints := queryTaskSessionDirHints(sessionID); len(hints) == 0 {
		t.Fatalf("the ledger must name %s for session %s (hints=%v)", taskID, sessionID, hints)
	}
}

// writeSessionRecord lays a session record at root/rel (dirs created) and
// returns its path.
func writeSessionRecord(t *testing.T, root, rel, sessionID string) string {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
	}
	if err := os.WriteFile(p, []byte(sessionStartLine(sessionID)), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// TestRowAddressResolutionUsesTheLedgerNamedDir is the acceptance property for
// the rework shape: the row's own columns name NOTHING on disk, the ledger
// names the directory, and the resolution walks the sessions tree ZERO times.
func TestRowAddressResolutionUsesTheLedgerNamedDir(t *testing.T) {
	hintFixtureLedger(t)
	const (
		rowTaskID  = "01a09fd1-3f80-702d-98a3-d220d7f87814" // the ledger ROW's id
		subTaskID  = "01a09fa7-5c37-7767-b691-138f4bacc4da" // the session's OWN row
		sessionID  = "56055d3a-bb0f-424b-8d42-4416fd1f7893"
		decoyCount = 40
	)
	seedSubmitterRow(t, subTaskID, sessionID)

	root := t.TempDir()
	// The only place the session log exists: the directory named by the
	// session's OWN row (the submitter) — NOT by the row being rendered.
	want := writeSessionRecord(t, root, filepath.Join(subTaskID, sessionID+taskSessionJSONLExt), sessionID)
	// Decoys a scan would wade through, one of them matching the session, so
	// the assertion cannot pass by a walk that found nothing.
	for i := 0; i < decoyCount; i++ {
		writeSessionRecord(t, root, filepath.Join("decoy-"+itoa2(i), taskBareSessionRecordName), "ffffffff-0000-4000-8000-00000000000"+itoa1(i%10))
	}

	clearSessionTreeCache()
	scans := countSessionTreeScans(t)
	if got := taskFindSessionJSONLForRow(root, rowTaskID, sessionID); got != want {
		t.Fatalf("resolved %q, want the ledger-named record %q", got, want)
	}
	if n := scans(); n != 0 {
		t.Fatalf("the ledger-named resolution walked the sessions tree %d time(s); it must walk it ZERO times", n)
	}
}

// TestRowAddressResolutionFallsBackWhenNoLedgerHint proves the lookup did not
// NARROW what resolves: with no ledger entry naming the session, the unchanged
// tree scan still finds a record in a directory no address could name.
func TestRowAddressResolutionFallsBackWhenNoLedgerHint(t *testing.T) {
	hintFixtureLedger(t)
	const (
		rowTaskID = "t-hint-absent-row"
		sessionID = "11111111-2222-4333-8444-555555555555"
	)
	if hints := queryTaskSessionDirHints(sessionID); len(hints) != 0 {
		t.Fatalf("fixture session must be unknown to the ledger, got hints=%v", hints)
	}
	root := t.TempDir()
	want := writeSessionRecord(t, root, filepath.Join("0-foreign-layout", taskBareSessionRecordName), sessionID)

	clearSessionTreeCache()
	scans := countSessionTreeScans(t)
	if got := taskFindSessionJSONLForRow(root, rowTaskID, sessionID); got != want {
		t.Fatalf("the scan fallback must still resolve %q, got %q", want, got)
	}
	if n := scans(); n == 0 {
		t.Fatal("with no address known the resolution must fall back to the tree scan (got 0 walks)")
	}
}

// TestLedgerNamedDirOutranksTheScan pins the PRECEDENCE the fix relies on: the
// ledger-named directory answers before the scan, even when the scan's own
// (readdir-order) precedence would return a DIFFERENT file. The ledger's answer
// is the address — it is the file the session actually wrote.
func TestLedgerNamedDirOutranksTheScan(t *testing.T) {
	hintFixtureLedger(t)
	const (
		rowTaskID = "t-hint-precedence-row"
		subTaskID = "9-submitter"
		sessionID = "22222222-3333-4444-8555-666666666667"
	)
	seedSubmitterRow(t, subTaskID, sessionID)

	root := t.TempDir()
	// A scan-visible record in a directory that sorts FIRST (so readdir order
	// would pick it), and the ledger-named record that is the address.
	scanFirst := writeSessionRecord(t, root, filepath.Join("0-scan-first", taskBareSessionRecordName), sessionID)
	want := writeSessionRecord(t, root, filepath.Join(subTaskID, sessionID+taskSessionJSONLExt), sessionID)

	clearSessionTreeCache()
	scans := countSessionTreeScans(t)
	if got := taskFindSessionJSONLForRow(root, rowTaskID, sessionID); got != want {
		t.Fatalf("the ledger-named dir must answer before the scan: got %q, want %q (scan would return %q)", got, want, scanFirst)
	}
	if n := scans(); n != 0 {
		t.Fatalf("the ledger-named resolution walked the sessions tree %d time(s)", n)
	}
}

// TestSessionDirHintsAreMemoisedAndMissesAreNot pins the two cache decisions
// the cost fix depends on: a HIT is reused (a group of rows sharing one
// session pays ONE ledger read), a MISS is not cached (the ledger may not hold
// the row yet — a cached miss would suppress the address for the rest of the
// process).
func TestSessionDirHintsAreMemoisedAndMissesAreNot(t *testing.T) {
	hintFixtureLedger(t)
	const (
		subTaskID = "t-hint-memo-session-row"
		sessionID = "33333333-4444-4555-8666-777777777778"
		missID    = "44444444-5555-4666-8777-888888888889"
	)
	seedSubmitterRow(t, subTaskID, sessionID)

	hintReads, restoreHints := SessionDirHintCounterForTest()
	defer restoreHints()

	if got := taskSessionDirHints(sessionID); !hintHas(got, subTaskID) {
		t.Fatalf("first lookup must name %s, got %v", subTaskID, got)
	}
	if n := hintReads(); n != 1 {
		t.Fatalf("first lookup must read the ledger once, got %d", n)
	}
	if got := taskSessionDirHints(sessionID); !hintHas(got, subTaskID) {
		t.Fatalf("memoised lookup must still name %s, got %v", subTaskID, got)
	}
	if n := hintReads(); n != 1 {
		t.Fatalf("a HIT must be memoised (one ledger read for a whole row group), got %d reads", n)
	}
	// A miss is answered (nil) but never cached: the ledger may gain the row.
	if got := taskSessionDirHints(missID); got != nil {
		t.Fatalf("unknown session must yield no hint, got %v", got)
	}
	if got := taskSessionDirHints(missID); got != nil {
		t.Fatalf("unknown session must stay hintless, got %v", got)
	}
	if n := hintReads(); n != 3 {
		t.Fatalf("a MISS must NOT be cached (it must re-read), got %d reads", n)
	}
	// The session's row appears afterwards — the very next lookup must see it.
	seedSubmitterRow(t, "t-hint-late-row", missID)
	if got := taskSessionDirHints(missID); !hintHas(got, "t-hint-late-row") {
		t.Fatalf("a late-arriving ledger row must be picked up, got %v", got)
	}
}

// TestSessionDirHintsNameTheLedgerRowsForTheSession is the unit contract of the
// hint read: it returns the task ids of the rows carrying that session id
// (newest first), and nothing else — a row with a different session id must
// never be offered as a directory name.
func TestSessionDirHintsNameTheLedgerRowsForTheSession(t *testing.T) {
	hintFixtureLedger(t)
	const (
		sessionID = "55555555-6666-4777-8888-999999999990"
		otherID   = "66666666-7777-4888-8999-aaaaaaaaaaa1"
	)
	seedSubmitterRow(t, "t-hint-owner", sessionID)
	seedSubmitterRow(t, "t-hint-other", otherID)

	hints := taskSessionDirHints(sessionID)
	if len(hints) != 1 || hints[0] != "t-hint-owner" {
		t.Fatalf("hints for %s = %v, want exactly [t-hint-owner]", sessionID, hints)
	}
	if strings.Join(hints, ",") == strings.Join(taskSessionDirHints(otherID), ",") {
		t.Fatal("two sessions must not share one hint set")
	}
}

// The three session-record layouts the row-addressed lookup must keep honouring,
// named by the probe case that reports them (the names the test branches on
// live here — Rule 1b).
const (
	layoutNameTaskRun = "task-run"
	layoutNameBare    = "bare"
	layoutNameFlat    = "flat"
)

// taskSessionLayoutProbe is ONE session-record layout under test: the name the
// test reports it by, and the root-relative path that layout places the
// session's record at.
type taskSessionLayoutProbe struct {
	name string
	rel  string
}

// taskSessionLayoutProbes is the layout matrix as a SLICE — deliberately never a
// Go map, which ranges in RANDOMIZED order. Each probe is asserted against its
// OWN fixture root (see the test below), so the iteration order carries no
// meaning and must not vary between runs (Rule 13: the layout addresses are
// named once, by the shared constants, and referenced from here).
func taskSessionLayoutProbes(taskID, sessionID string) []taskSessionLayoutProbe {
	return []taskSessionLayoutProbe{
		{name: layoutNameTaskRun, rel: filepath.Join(taskID, sessionID+sessionlayout.JSONLSuffix)},
		{name: layoutNameBare, rel: filepath.Join(sessionID, taskBareSessionRecordName)},
		{name: layoutNameFlat, rel: sessionID + sessionlayout.JSONLSuffix},
	}
}

// TestTaskFindSessionJSONLStillHonoursEveryLayout guards the layout contract the
// lookup now sits inside: the bare, task-run and flat layouts keep resolving
// through the same entry point (so adding the ledger address did not displace
// them).
//
// WHY ONE FIXTURE ROOT PER LAYOUT (ruled + measured 2026-09-16): the three
// layouts are ALTERNATIVE ADDRESSES FOR THE SAME SESSION, not three records that
// coexist. The bare dir (<session-id>/session.jsonl) and the task-run dir
// (<task-id>/<session-id>.jsonl) both sit one level under ONE sessions root, so
// a single lookup hands back exactly ONE of them — decided by byte-sorted
// readdir order, the scan's own precedence (pinned by
// TestSessionLayoutPrecedenceIsTheScanOrder). Writing all three into one shared
// root and asserting each in turn therefore asserted something UNTRUE (whichever
// address the resolver legitimately prefers, the other two MUST fail) and its
// outcome was a coin flip over the fixture's map range — measured on master,
// ~1-2 red in 8 runs, always reading "flat layout resolved
// <session-id>/session.jsonl, want <session-id>.jsonl". Each case now gets its
// OWN root, where the layout under test is the ONLY record present, so a hit can
// only come from THAT layout being honoured. The assertion itself is unchanged
// and strict (exact path equality): the FIXTURE was wrong, the resolver was not.
//
// NOT VACUOUS: the resolution must also cost ZERO sessions-tree walks — i.e. the
// row-addressed lookup must ANSWER it, not have the scan fallback rescue it. For
// the two deep-scannable layouts (bare, task-run) a blind value assertion is
// unfalsifiable, because the deep scan reaches the very same file; the walk
// counter is what makes breaking an addressed probe fail the case it belongs to.
// The red proof per layout is in the task report.
func TestTaskFindSessionJSONLStillHonoursEveryLayout(t *testing.T) {
	flaketest.Declare("deterministic: the fixture wrote all three session-record layouts into ONE shared fixture root and walked them from a Go map (randomized iteration order); the three layouts are alternative ADDRESSES for one session, so whichever address the resolver legitimately prefers, the other two must fail — the verdict was a coin flip on the map's iteration order. Registered: testdata/flakes.tsv, owner 01a0aba1-44c8-7179-9f5c-635f1ac2b2e7.")
	hintFixtureLedger(t)
	const (
		taskID    = "t-layout-row"
		sessionID = "77777777-8888-4999-8aaa-bbbbbbbbbbb2"
	)
	probes := taskSessionLayoutProbes(taskID, sessionID)
	for _, probe := range probes {
		t.Run(probe.name, func(t *testing.T) {
			root := t.TempDir()
			want := writeSessionRecord(t, root, probe.rel, sessionID)
			assertOnlyThisLayoutIsPresent(t, root, probes, probe)
			// Bound BEFORE the lookup: the counter must see this case's whole
			// resolution, and it clears the walk memo first.
			scans := countSessionTreeScans(t)
			if got := taskFindSessionJSONLForRow(root, taskID, sessionID); got != want {
				t.Errorf("%s layout resolved %q, want %q", probe.name, got, want)
			}
			if n := scans(); n != 0 {
				t.Errorf("%s layout cost %d sessions-tree walk(s); the row-addressed lookup must resolve it, not the scan fallback", probe.name, n)
			}
		})
	}
}

// assertOnlyThisLayoutIsPresent is the FIXTURE's own guard: under root, the
// layout under test is the only one of the three addresses that exists. It is
// what keeps the per-layout cases independent — a case that silently carried a
// second layout's record would be asking the precedence question again, which is
// the very ambiguity this test was fixed to remove.
func assertOnlyThisLayoutIsPresent(t *testing.T, root string, probes []taskSessionLayoutProbe, under taskSessionLayoutProbe) {
	t.Helper()
	for _, other := range probes {
		if other.name == under.name {
			continue
		}
		p := filepath.Join(root, other.rel)
		_, err := os.Stat(p)
		if err == nil {
			t.Errorf("fixture leak: the %s layout's record also exists in the %s case's root (%s)", other.name, under.name, p)
			continue
		}
		if !os.IsNotExist(err) {
			t.Fatalf("stat %s: %v", p, err)
		}
	}
}

// TestSessionLayoutPrecedenceIsTheScanOrder pins WHICH address wins when ONE
// root carries SEVERAL layouts for the same session — the question the
// per-layout cases above deliberately do not ask. It is a PRODUCTION contract,
// not a fixture detail: the lookup may only replace the deep scan if it agrees
// with it, so the winner is the scan's own readdir (byte-sorted) order, which
// taskFindSessionJSONLInTaskDir reproduces by comparing the two dir names —
// the bare dir <session-id> is visited first exactly when the session id sorts
// before the task id.
//
// BOTH directions are pinned, each against BOTH entry points (the row-addressed
// lookup AND the deep scan it stands in for), with the flat record present as a
// loser in both: one direction alone cannot tell a fixed order from a lucky one,
// and a regression that swaps the probes passes one assertion while failing the
// other.
func TestSessionLayoutPrecedenceIsTheScanOrder(t *testing.T) {
	hintFixtureLedger(t)
	for _, tc := range []struct {
		name      string
		taskID    string
		sessionID string
		winner    string // the layout the scan's byte-sorted order yields
	}{
		// session id sorts BEFORE the task id: the bare dir is visited first.
		{name: "bare-dir-sorts-first", taskID: "t-bare-loses-row", sessionID: "22222222-3333-4444-8555-666666666667", winner: layoutNameBare},
		// session id sorts AFTER the task id: the task-run dir is visited first.
		{name: "task-run-dir-sorts-first", taskID: "0-run-wins-row", sessionID: "ffffffff-0000-4000-8000-000000000000", winner: layoutNameTaskRun},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			// byName is a KEYED lookup (each case names the one layout it
			// expects to win); nothing iterates it, so its order is irrelevant.
			// The fixture is still laid down from the SLICE, in slice order.
			byName := map[string]string{}
			for _, probe := range taskSessionLayoutProbes(tc.taskID, tc.sessionID) {
				byName[probe.name] = writeSessionRecord(t, root, probe.rel, tc.sessionID)
			}
			want := byName[tc.winner]
			// The flat record is NOT the row's own address: both addressed forms
			// win over it, so a flat record present alongside them must never be
			// preferred (findSessionJSONLFlat runs AFTER the addressed probes).
			if tc.winner == layoutNameFlat {
				t.Fatalf("fixture error: the flat layout can never win against an addressed layout")
			}
			clearSessionTreeCache()
			if got := taskFindSessionJSONLForRow(root, tc.taskID, tc.sessionID); got != want {
				t.Errorf("row lookup resolved %q, want the scan-ordered %s address %q", got, tc.winner, want)
			}
			clearSessionTreeCache()
			if got := taskFindSessionJSONLDeepOnly(root, tc.sessionID); got != want {
				t.Errorf("the deep scan resolved %q, want %q — the lookup may only replace a scan it agrees with", got, want)
			}
		})
	}
}

// itoa1/itoa2 are tiny decimal renderers for fixture names — the tests need
// distinct, deterministic directory names without pulling in a formatter.
func itoa1(n int) string { return string(rune('0' + n%10)) }

func itoa2(n int) string {
	const digits = "0123456789"
	if n < 10 {
		return string(digits[n])
	}
	return string(digits[n/10]) + string(digits[n%10])
}

// hintHas reports whether want is among the hint candidates.
func hintHas(got []string, want string) bool {
	for _, g := range got {
		if g == want {
			return true
		}
	}
	return false
}
