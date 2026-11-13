package tools

// REGRESSION for the sessions-tree memo's staleness (measured 2026-10-29).
//
// THE DEFECT. taskSessionTreeRecords memoised the whole-tree walk PER PROCESS
// and never invalidated it. That is sound for the one-shot CLI the memo was
// written for and wrong for any process that outlives one answer — and
// cmd/agent's suite is exactly that process: its cases share one pinned
// profile, so a walk taken while serving an earlier case froze the sessions
// tree, and a later case that seeded sessions/<id>/ into that same root had it
// resolved against the FROZEN list. The observable failure was
//
//	TestResumeTaskTargetResolution/a_row_with_non_empty_session_id_resolves_to_that_session
//	    resolveResumeTarget("t-resume-task-target") errored: … its run directory
//	    (…/sessions/t-resume-task-target) holds no run whose raw log still
//	    exists …
//
// i.e. a lookup about a session that WAS on disk, answered against a list taken
// before it existed. The same test passes alone (-run, three times) because
// nothing has walked the tree yet.
//
// WHICH SEED IS THE REPRO, and why the obvious one is not: a BARE session dir
// (<root>/<uuid>/session.jsonl) is ADDRESSABLE — sessionBareRecordPath stats it
// directly — so it never reaches the walk and would measure nothing. The layout
// the walk OWNS is the task-run one, <root>/<task-id>/<uuid>.jsonl, whose dir is
// named by a task id and not by the session id: that record is reachable ONLY by
// walking. The seed below is therefore the walk-only layout.
//
// THE FIX this test pins: the cache entry carries the ROOT FINGERPRINT it was
// taken at, and a hit is served only while the root still has it. The proof is
// the ORDER, not a mock: warm the memo, seed a session dir, resolve.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionlayout"
)

const (
	// memoWarmTaskID names the first task-run dir — the walk's seed, and the
	// warming record.
	memoWarmTaskID = "t-memo-warm-row"
	// memoSeededTaskID names the task-run dir that appears AFTER the walk
	// warmed: the shape a later test case (or a long-lived face) produces.
	memoSeededTaskID = "t-memo-late-row"
	// memoWarmID / memoSeededID are the two RUN ids inside those dirs.
	memoWarmID   = "0dead0be-0000-0000-0000-000000000000"
	memoSeededID = "0feed0be-1111-1111-1111-111111111111"
)

// memoSeedTaskRunDir writes the WALK-ONLY layout: <root>/<taskID>/<runID>.jsonl
// naming its co-located raw log. Addressable from neither the session id nor a
// bare-dir probe, so only taskSessionTreeRecords can answer it.
func memoSeedTaskRunDir(t *testing.T, root, taskID, runID string) string {
	t.Helper()
	dir := filepath.Join(root, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	raw := filepath.Join(dir, runID+".raw.log")
	if err := os.WriteFile(raw, []byte("→ REQUEST\n"), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	line := `{"type":"session_start","sessionId":"` + runID + `","model":"m","rawLogPath":"` + raw + `"}` + "\n"
	rec := filepath.Join(dir, runID+sessionlayout.JSONLSuffix)
	if err := os.WriteFile(rec, []byte(line), 0o644); err != nil {
		t.Fatalf("write session record: %v", err)
	}
	return rec
}

// TestSessionTreeMemoDoesNotOutliveTheTreeItDescribes is the minimal repro:
// warm, seed, resolve. It fails when a cached walk is served after the root it
// describes has changed.
func TestSessionTreeMemoDoesNotOutliveTheTreeItDescribes(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	root := filepath.Join(home, config.ProfileDirName, config.SessionsDirName)
	memoSeedTaskRunDir(t, root, memoWarmTaskID, memoWarmID)
	clearSessionTreeCache()

	// 1. WARM: the walk resolves the first run-only record and memoises.
	if got := taskFindSessionJSONL(root, memoWarmID); got == "" {
		t.Fatalf("the warming record must resolve through the walk")
	}

	// 2. The tree GROWS — a second task-run dir appears after the walk.
	want := memoSeedTaskRunDir(t, root, memoSeededTaskID, memoSeededID)

	// 3. The very next resolution must see it.
	if got := taskFindSessionJSONL(root, memoSeededID); got != want {
		t.Fatalf("a session created after the memoised walk resolved to %q, want %q — the walk was served from a list taken before it existed (the memo outlived its tree)", got, want)
	}
}

// TestSessionTreeMemoStillSavesTheWalkWhenTheTreeHoldsStill is the OTHER side
// of the same contract, and it is why the fingerprint is a getdents + stat
// rather than "never cache": while the root does not change, the walk must
// happen ONCE. A fix that invalidated on every call would pass the test above
// and re-introduce the O(rows × tree) cost the memo exists to remove.
func TestSessionTreeMemoStillSavesTheWalkWhenTheTreeHoldsStill(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	root := filepath.Join(home, config.ProfileDirName, config.SessionsDirName)
	memoSeedTaskRunDir(t, root, memoWarmTaskID, memoWarmID)
	clearSessionTreeCache()

	scans := countSessionTreeScans(t)
	for i := 0; i < 3; i++ {
		if got := taskFindSessionJSONL(root, memoWarmID); got == "" {
			t.Fatalf("resolution %d found nothing while the tree held still", i)
		}
	}
	if n := scans(); n != 1 {
		t.Fatalf("resolving the same tree three times walked it %d time(s), want exactly 1 — the memo must still hold while the root's fingerprint does", n)
	}
}
