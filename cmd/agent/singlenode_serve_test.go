package main

// Single-node regression test (epic 2026-09-02, acceptance 1).
//
// WHAT THIS FILE USED TO CARRY, and why it is gone: two cases measured the
// serve-tick GIT sync pass's empty-topology short-circuit (`serveGitSyncSweep`
// returning before it advanced its throttle clock). That pass is DELETED
// (2026-11-05) together with the cross-node sync family it belonged to — it had
// no leg left to run — so the cases were measuring a function that no longer
// exists. They are removed rather than skipped: a skipped test is a deleted test
// wearing a passing green (Rule 27), and a case asserting a deleted mechanism's
// behaviour would start passing for the wrong reason the moment a new pass grew
// a similar-looking gate.
//
// What survives is the LOCAL half of the same epic, which never involved the
// git channel: the worker round's claim path on an empty topology. The
// single-node shape is `sync_peers: []` with `GOPI_SYNC_PEERS` unset/empty —
// HasSyncPeers() reads env then settings.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tools"
)

// TestRunWorkerClaimLocalOnlyWithNoPeers (epic acceptance 1, worker face):
// with the single-node shape (`sync_peers: []` + `compute_nodes: []` —
// empty topology, NOT the GOPI_SKIP_CLAIM_BROADCAST env escape), a full
// worker round that claims and runs a task must take the LOCAL-CAS-only
// path: the single-node gate print fires (proving `!config.HasSyncPeers()`
// branch, not the env kill-switch), and the run completes through the
// writeback — zero remote protocol legs. The hermetic child escape keeps
// the run child-free (no model call); the claim+writeback path is real.
// The claim/run path moved from the serve tick to the worker in the
// serve/worker-split epic — the merger never claims or runs tasks.
func TestRunWorkerClaimLocalOnlyWithNoPeers(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv("GOPI_HOME", sandbox)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(sandbox, "memory.db"))
	// Single-node shape: no compute nodes, NO sync peers — and crucially
	// NO GOPI_SKIP_CLAIM_BROADCAST. The no-peer HasSyncPeers() gate is the
	// path under test (it must take precedence and fire on its own).
	t.Setenv("GOPI_COMPUTE_NODES", "")
	t.Setenv(config.SyncPeersEnv, "")
	t.Setenv("GOPI_SETTINGS", filepath.Join(sandbox, "nonexistent.json"))
	t.Setenv(taskChildFailTestEnv, "1") // child fails without spawning

	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "single-node local claim fixture",
		Payload: "claim local only", // dispatchable payload
		TaskID:  "t-singlenode-claim",
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// The round under test must be claim→run→writeback. Nothing to arm: the
	// worker round carries no lifecycle leg (deleted 2026-10-30, see
	// task_lifecycle_tick.go), so the round cannot stray into one.

	out := captureStdout(t, func() {
		// Must RETURN (not os.Exit) — os.Exit(1) here would kill the test
		// binary. The hermetic child writes the row failed (synthetic child
		// failure) — the claim happened, the remote leg must NOT have.
		runTaskWorker(true, "", nil) // --once: claim → local run → drain → exit 0
	})

	// THE ASSERTION INVERTED WITH THE MECHANISM (batch 4 of the cross-node
	// refactor). It used to require `[claim-lock] task sync OFF` — proof that
	// the single-node gate had FIRED, i.e. that the runner had entered the
	// cross-node claim lock and short-circuited it. That protocol is deleted,
	// so there is no gate to fire and no line to print, and requiring the
	// line would now pin a mechanism nobody can reach.
	//
	// What this case exists to pin survives exactly: a worker round on the
	// single-node topology claims LOCALLY and reaches the writeback with zero
	// remote legs. The absence half is the new evidence — the path is no
	// longer gated AT ALL, which the old assertion could not tell apart from
	// "gated, then short-circuited".
	if strings.Contains(out, "[claim-lock]") {
		t.Fatalf("the single-node path printed a [claim-lock] line — the cross-node claim lock is "+
			"deleted, so nothing should announce it any more; worker output:\n%s", out)
	}
	// The claim+writeback completed: row is terminal failed (synthetic
	// child failure), never left mid-run in_progress.
	row, err := tools.TaskShow("t-singlenode-claim")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(row, "status:") || !strings.Contains(row, "failed") {
		t.Fatalf("claim+run must write the row terminal, got:\n%s", row)
	}
}
