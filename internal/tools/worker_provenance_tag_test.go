package tools

// worker_provenance_tag_test.go — the M4.20 ① worker-provenance tag contract,
// pinned where the BUG REPORT said it was broken.
//
// The report (bug: "--worker flow never stamps the worker provenance tag —
// TaskRunWritebackTagged is dead code under the serve daemon") claimed
// TaskRunWritebackTagged early-returns (nil, nil) behind a
// `strings.TrimSpace(os.Getenv(TaskWrapperEnv)) == ""` gate, making the tag
// unreachable in the worker flow. The gate does NOT exist in this file's
// production path (it never did on this line of history — see
// devdoc/worker-provenance-tag.md §1), and a real `gopi worker --once` run
// stamps `worker:<host>` end-to-end. What was genuinely MISSING is the
// hermetic guard the report's search implies: no test pinned that the tag's
// host half is resolved from the PROCESS's own environment (os.Hostname),
// with NO wrapper env var set.
//
// This file closes exactly that hole: it deliberately UNSETS the wrapper env
// and asserts the tag is still produced and still reaches the ledger row —
// so a future env gate of the reported shape fails HERE, not in production.

import (
	"os"
	"strings"
	"testing"
)

// TestWorkerProvenanceTagNoWrapperEnv_HostFromProcessEnv pins the resolution
// contract: `worker:<hostname>` is derived from the DAEMON/PROCESS's own
// environment (os.Hostname) — it must NOT depend on TaskWrapperEnv, which
// only a `task run` WRAPPER sets on its CHILD. A worker/serve daemon has no
// wrapper env; if the tag ever regresses to a wrapper-env gate, this test
// fails with the exact symptom the bug report described (empty tag).
func TestWorkerProvenanceTagNoWrapperEnv_HostFromProcessEnv(t *testing.T) {
	// The reported trigger: no wrapper env in the process environment.
	t.Setenv(TaskWrapperEnv, "")
	if v := strings.TrimSpace(os.Getenv(TaskWrapperEnv)); v != "" {
		t.Fatalf("precondition: %s must be unset, got %q", TaskWrapperEnv, v)
	}

	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "unknown-host" // the production fallback (WorkerTaskTag)
	}
	want := WorkerTaskTagPrefix + host
	if got := WorkerTaskTag(); got != want {
		t.Fatalf("WorkerTaskTag() = %q, want %q (host resolved from the process env, not %s)",
			got, want, TaskWrapperEnv)
	}
	if !strings.HasPrefix(want, "worker:") {
		t.Fatalf("tag %q lost the worker: namespace prefix", want)
	}
}

// TestWorkerProvenanceTagReachesLedgerWithoutWrapperEnv is the end-to-end
// ledger face of the same contract: with NO wrapper env set, the worker-loop
// writeback primitive still writes a row whose tags END WITH worker:<host>
// (the acceptance shape — the tag is appended to the row's existing tags).
func TestWorkerProvenanceTagReachesLedgerWithoutWrapperEnv(t *testing.T) {
	path := t.TempDir() + "/memory.db"
	t.Setenv("GOPI_MEMORY_DB", path)
	t.Setenv(TaskWrapperEnv, "") // the daemon case: no wrapper process spawned us

	const rowID = "t-wprov-nogate-1"
	if _, err := TaskNew("worker provenance row", rowID, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	// Pre-seed the submitter's existing tags through the same env-resolved
	// ledger open the writeback uses — the union must preserve them in front.
	seed, closeSeed, err := OpenWorkerProbeDB()
	if err != nil {
		t.Fatalf("OpenWorkerProbeDB: %v", err)
	}
	if _, err := seed.Exec(`UPDATE tasks SET tags = ? WHERE task_id = ?`, "repro", rowID); err != nil {
		closeSeed()
		t.Fatalf("seed tags: %v", err)
	}
	closeSeed()

	if _, err := TaskRunWritebackTagged(rowID, TaskStatusCompleted, "done"); err != nil {
		t.Fatalf("TaskRunWritebackTagged: %v", err)
	}

	tags, err := taskTagsByID(rowID)
	if err != nil {
		t.Fatalf("taskTagsByID: %v", err)
	}
	if !strings.HasSuffix(tags, WorkerTaskTag()) {
		t.Fatalf("tags = %q, want suffix %q (existing tags preserved, worker tag appended)",
			tags, WorkerTaskTag())
	}
	if !strings.HasPrefix(tags, "repro,") {
		t.Fatalf("tags = %q, want the pre-existing tags preserved in front", tags)
	}

	// Idempotent: a second writeback (crash-recovery re-run) must not double
	// the tag — the union is a set.
	if _, err := TaskRunWritebackTagged(rowID, TaskStatusCompleted, "done again"); err != nil {
		t.Fatalf("second TaskRunWritebackTagged: %v", err)
	}
	again, err := taskTagsByID(rowID)
	if err != nil {
		t.Fatalf("taskTagsByID (second): %v", err)
	}
	if strings.Count(again, WorkerTaskTag()) != 1 {
		t.Fatalf("tags = %q, want exactly one %q after a re-run", again, WorkerTaskTag())
	}
}
