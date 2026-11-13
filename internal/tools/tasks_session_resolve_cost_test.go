package tools

// Regression guards for the session-log RESOLUTION COST (2026-09-14).
//
// SYMPTOM: `gopi task list --status in_progress` took 8-48s while every other
// --status filter answered in 0.02s. The filter is not the difference: the
// per-row session probe runs ONLY for in_progress rows, and each probe walked
// the operator's ENTIRE sessions tree (70,438 entries here) twice — once in
// findSessionJSONLFlat and once in the deep pass — to recover a session id the
// ROW ALREADY CARRIED. Measured with strace: 287k openat / 575k getdents64 for
// a 6-row list.
//
// The tests below pin the ORDERING the fix relies on, because ordering is the
// cost contract: a row's own deterministic layout must be answered by stat-ing
// that path, and any scanning pass may only run when those paths miss. A
// regression that re-orders them restores the full tree walk without changing
// any output — which is exactly what these assertions catch.

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile fails the test when the fixture cannot be laid down — a silent
// fixture failure would make the assertions below vacuous.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestTaskFindSessionJSONLAddressesTheIdBeforeWalkingTheTree is the cost
// contract of the ADDRESSED-FIRST pass (epic session-log-addressed-first,
// 2026-10-07): when the log sits in a layout the SESSION ID alone names — the
// bare session dir `<id>/session.jsonl`, or the legacy flat `<id>.jsonl` — the
// resolution must reach it with address probes and walk the sessions tree ZERO
// times.
//
// WHY IT IS A SEPARATE TEST from its row-keyed sibling: the row-keyed entry
// point cannot express this case. It is called with the ROW's task id, while
// this entry point is called by the three `task show` audit lookups
// (taskSessionLogPath / taskModelFromSession / taskBriefingFromSession) with a
// session id whose directory is named by ANOTHER row — so for the whole class
// of context-loss re-homed sessions the row-keyed fast path is INERT and every
// render paid a full tree walk (measured 2026-10-07 on the live ledger: 84 of
// 623 session-carrying rows, 13%, resolve only through the walk).
//
// It also pins the VALUE, not just the cost: the addressed answer must be the
// file the walk returns, so the reordering can never change which log a row
// shows.
func TestTaskFindSessionJSONLAddressesTheIdBeforeWalkingTheTree(t *testing.T) {
	cases := []struct {
		name string
		rel  string // the record's path relative to the sessions root
	}{
		{"bare session dir", filepath.Join(costSessionID, taskBareSessionRecordName)},
		{"legacy flat record", costSessionID + taskSessionJSONLExt},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			want := filepath.Join(root, c.rel)
			writeFile(t, want, sessionStartLine(costSessionID))

			scans := countSessionTreeScans(t)
			got := taskFindSessionJSONL(root, costSessionID)
			// The COST is asserted FIRST so a reverted addressed pass reddens on
			// the walk count — the property this test exists for — rather than on
			// a value that a root-level record the walk never reads would mask.
			if n := scans(); n != 0 {
				t.Fatalf("resolving the %s layout walked the sessions tree %d time(s); an address the "+
					"session id itself names must cost zero walks", c.name, n)
			}
			if got != want {
				t.Fatalf("resolved %q, want the addressed record %q", got, want)
			}
		})
	}
}

// TestTaskFindSessionJSONLKeepsTheWalkAsTheFallback is the other half of the
// contract, and the reason the change is a REORDERING and not a narrowing: the
// task-run layout (`<task-id>/<session-id>.jsonl`) is NOT addressable from a
// session id — the directory is named by a task id the id does not carry — so
// it must still resolve, through the walk, exactly as before.
func TestTaskFindSessionJSONLKeepsTheWalkAsTheFallback(t *testing.T) {
	const foreignTaskDir = "0-foreign-task-dir"
	root := t.TempDir()
	want := filepath.Join(root, foreignTaskDir, costSessionID+taskSessionJSONLExt)
	writeFile(t, want, sessionStartLine(costSessionID))

	scans := countSessionTreeScans(t)
	if got := taskFindSessionJSONL(root, costSessionID); got != want {
		t.Fatalf("the walk fallback lost the record: got %q, want %q", got, want)
	}
	if n := scans(); n == 0 {
		t.Fatal("a record only the walk can reach must still be reached BY the walk (got 0 walks)")
	}
}

// TestTaskFindSessionJSONLAddressingPreservesTheScanPrecedence pins WHICH file
// resolves when one session id has a record in more than one layout — the
// question a reordering can silently change. The direction PINNED here is the
// one the entry point already had: the flat record (`<id>.jsonl`) is asked for
// before the bare dir (`<id>/session.jsonl`), which is also where
// taskFindSessionJSONLForRow asks for it.
//
// The walk is deliberately NOT used as the oracle for the pair of addressable
// layouts, and that is a MEASURED fact rather than a convenience: `os.ReadDir`
// returns entries in an ARBITRARY order (probed 2026-10-07 on this host — a
// root holding `<task>/`, `<id>/`, `<id>.jsonl` came back task-dir, bare-dir,
// flat-file, i.e. neither sorted nor files-first), so when one session exists
// in several layouts the walk's answer is filesystem-dependent. What this
// change can and must preserve is the ORDER the entry point itself uses, which
// is a property of the code; the walk stays last, exactly where the flat pass
// already sat, so nothing that the walk answered BEFORE is now answered by an
// address that outranks it (the flat/bare order is unchanged; the walk keeps
// every layout it ever resolved).
func TestTaskFindSessionJSONLAddressingPreservesTheScanPrecedence(t *testing.T) {
	root := t.TempDir()
	run := filepath.Join(root, "0-foreign-task-dir", costSessionID+taskSessionJSONLExt)
	writeFile(t, run, sessionStartLine(costSessionID))
	bare := filepath.Join(root, costSessionID, taskBareSessionRecordName)
	writeFile(t, bare, sessionStartLine(costSessionID))
	flat := filepath.Join(root, costSessionID+taskSessionJSONLExt)
	writeFile(t, flat, sessionStartLine(costSessionID))

	// The pre-existing answer, which this change must not move.
	if got := taskFindSessionJSONL(root, costSessionID); got != flat {
		t.Fatalf("addressed-first returned %q, want %q (the order the resolution "+
			"had before the addressed pass: flat, then bare, then the walk)", got, flat)
	}

	// Remove the flat record: the bare dir is the next address asked for.
	if err := os.Remove(flat); err != nil {
		t.Fatalf("remove flat: %v", err)
	}
	if got := taskFindSessionJSONL(root, costSessionID); got != bare {
		t.Fatalf("addressed-first returned %q, want the bare record %q", got, bare)
	}

	// Remove the bare record too: only the task-run record is left, which no
	// address names. It must still resolve — through the walk — and the walk
	// must be the thing that answered.
	if err := os.RemoveAll(filepath.Join(root, costSessionID)); err != nil {
		t.Fatalf("remove bare dir: %v", err)
	}
	clearSessionTreeCache()
	scans := countSessionTreeScans(t)
	if got := taskFindSessionJSONL(root, costSessionID); got != run {
		t.Fatalf("the walk fallback lost the task-run record: got %q, want %q", got, run)
	}
	if n := scans(); n == 0 {
		t.Fatal("a record only the walk can reach must still be reached BY the walk (got 0 walks)")
	}
}

// TestTaskFindSessionJSONLPrefersTheIdsOwnDirOverAForeignTaskDir pins the ONE
// precedence this epic deliberately MOVED, with its reason, because a silent
// move is exactly what the sibling tests above exist to prevent.
//
// The shape: one session id has a record in its OWN bare dir
// (`<id>/session.jsonl`) and ALSO in a task-run dir whose name sorts EARLIER
// (`<task-id>/<id>.jsonl`). Before this change the entry point asked the flat
// layout and then WALKED, and the walk returns whichever of the two dirs its
// readdir order reaches first — so the foreign task dir won. Now the id's own
// dir is asked before the walk and wins, because it is a stronger address (the
// directory is NAMED by the id), which is the same preference
// taskFindSessionJSONLForRow already gives the row's own dir.
//
// WHY THIS IS ACCEPTABLE, measured rather than argued (2026-10-07, live
// profile): the walk's answer is decided by a DIRECTORY NAME — a property that
// has nothing to do with which file is the session's log — and the shape is not
// observed on this host at all: of 623 rows carrying a session_id, 258 session
// dirs are bare and 308 session ids live in task-run dirs, with a ZERO
// intersection. The sibling test pinning walk-identity covers the layouts that
// DO coexist (one candidate each), and the row-addressed path keeps the
// name-order comparison where it has a task id to compare with.
func TestTaskFindSessionJSONLPrefersTheIdsOwnDirOverAForeignTaskDir(t *testing.T) {
	root := t.TempDir()
	// The foreign task dir sorts FIRST, so the walk reaches it before the id's
	// own dir.
	foreign := filepath.Join(root, "0-foreign-task-dir", costSessionID+taskSessionJSONLExt)
	writeFile(t, foreign, sessionStartLine(costSessionID))
	own := filepath.Join(root, costSessionID, taskBareSessionRecordName)
	writeFile(t, own, sessionStartLine(costSessionID))

	clearSessionTreeCache()
	if oracle := taskFindSessionJSONLDeepOnly(root, costSessionID); oracle != foreign {
		t.Fatalf("fixture/oracle mismatch: the walk reached %q, expected the earlier-sorting task dir %q", oracle, foreign)
	}
	if got := taskFindSessionJSONL(root, costSessionID); got != own {
		t.Fatalf("the id's own dir must be asked before the walk: got %q, want %q (the walk returns %q)",
			got, own, foreign)
	}
}

// sessionStartLine is the minimum session record the classifier and the
// first-line reader agree on: a JSONL whose first line carries sessionId.
func sessionStartLine(sessionID string) string {
	return `{"type":"session_start","sessionId":"` + sessionID + `"}` + "\n"
}

// TestFindSessionJSONLFlatIsExactOnly pins the resolution rule (epic
// full-id-no-prefix): an exact basename match resolves, and a TRUNCATED id
// resolves to NOTHING — it is never prefix-matched. The pre-fix code returned
// the first prefix hit (one pass, with the exact match preferred), which is how
// a listing's 8-character chip could stand in for a session id and hand back an
// arbitrary record.
func TestFindSessionJSONLFlatIsExactOnly(t *testing.T) {
	dir := t.TempDir()
	id := "abcd1234-0000-4000-8000-000000000000"
	// A near-miss record sorts FIRST: a prefix-accepting lookup would hand it
	// back for the truncated operand.
	other := filepath.Join(dir, "abcd1234-1111-4111-8111-111111111111"+taskSessionJSONLExt)
	exact := filepath.Join(dir, id+taskSessionJSONLExt)
	writeFile(t, other, sessionStartLine("x"))
	writeFile(t, exact, sessionStartLine("x"))

	if got := findSessionJSONLFlat(dir, id); got != exact {
		t.Fatalf("exact match not resolved: got %s, want %s", got, exact)
	}
	if got := findSessionJSONLFlat(dir, "abcd1234"); got != "" {
		t.Fatalf("truncated id must NOT resolve to a prefix hit, got %q", got)
	}
	if got := findSessionJSONLFlat(dir, "no-such-session"); got != "" {
		t.Fatalf("unmatched id must resolve to empty, got %q", got)
	}
}

// TestTaskFindSessionJSONLForRowPrefersRowPathsOverScan is the cost contract:
// the row's own deterministic layouts must win over a deep-scan candidate that
// a lookup-first implementation never has to open.
func TestTaskFindSessionJSONLForRowPrefersRowPathsOverScan(t *testing.T) {
	const (
		sess = "11111111-2222-4333-8444-555555555555"
		task = "t-1789381146"
	)
	root := t.TempDir()
	// Decoy: a session DIR that carries the same sessionId and sorts BEFORE
	// the row's own dir, so a scan-first resolution would return it.
	writeFile(t, filepath.Join(root, "0-decoy", taskBareSessionRecordName), sessionStartLine(sess))

	// Layout 1: the bare session dir (<sessions>/<session-id>/session.jsonl).
	bare := filepath.Join(root, sess, taskBareSessionRecordName)
	writeFile(t, bare, sessionStartLine(sess))
	if got := taskFindSessionJSONLForRow(root, task, sess); got != bare {
		t.Fatalf("bare session dir not preferred over the scan: got %s, want %s", got, bare)
	}
	if err := os.RemoveAll(filepath.Join(root, sess)); err != nil {
		t.Fatalf("remove bare dir: %v", err)
	}

	// Layout 2: the task-run dir (<sessions>/<task-id>/<uuid>.jsonl).
	run := filepath.Join(root, task, sess+taskSessionJSONLExt)
	writeFile(t, run, sessionStartLine(sess))
	if got := taskFindSessionJSONLForRow(root, task, sess); got != run {
		t.Fatalf("task-run dir not preferred over the scan: got %s, want %s", got, run)
	}
	if err := os.RemoveAll(filepath.Join(root, task)); err != nil {
		t.Fatalf("remove task dir: %v", err)
	}

	// Layout 3 (legacy flat <sessions>/<uuid>.jsonl) is answered by the flat
	// pass, which is tried before the deep pass.
	flat := filepath.Join(root, sess+taskSessionJSONLExt)
	writeFile(t, flat, sessionStartLine(sess))
	if got := taskFindSessionJSONLForRow(root, task, sess); got != flat {
		t.Fatalf("flat record not preferred over the scan: got %s, want %s", got, flat)
	}
	if err := os.Remove(flat); err != nil {
		t.Fatalf("remove flat record: %v", err)
	}

	// No deterministic path exists: the scan is the FALLBACK and must still
	// find the decoy, so the reordering never narrowed what resolves.
	decoy := filepath.Join(root, "0-decoy", taskBareSessionRecordName)
	if got := taskFindSessionJSONLForRow(root, task, sess); got != decoy {
		t.Fatalf("deep-scan fallback lost: got %s, want %s", got, decoy)
	}

	// An unresolvable session stays unresolvable.
	if got := taskFindSessionJSONLForRow(root, task, "99999999-0000-4000-8000-000000000000"); got != "" {
		t.Fatalf("unknown session must resolve to empty, got %q", got)
	}
}

// TestTaskFindSessionJSONLForRowAnswersFromTheRowWithoutTheScan is the
// behavioural half of the cost guard: with a root large enough that a scan is
// expensive and a decoy that a scan WOULD match, the answer still comes from
// the row's own path. (The cost itself is proven by strace in the task report;
// this test fails if the lookup is ever demoted below a scanning pass.)
func TestTaskFindSessionJSONLForRowAnswersFromTheRowWithoutTheScan(t *testing.T) {
	const (
		sess = "22222222-3333-4444-8555-666666666666"
		task = "t-1789382000"
	)
	root := t.TempDir()
	for i := 0; i < 200; i++ {
		writeFile(t, filepath.Join(root, "decoy-"+string(rune('a'+i%26))+string(rune('a'+i/26)),
			taskBareSessionRecordName), sessionStartLine(sess))
	}
	want := filepath.Join(root, task, sess+taskSessionJSONLExt)
	writeFile(t, want, sessionStartLine(sess))
	if got := taskFindSessionJSONLForRow(root, task, sess); got != want {
		t.Fatalf("row path must win regardless of the decoy population: got %s, want %s", got, want)
	}
}
