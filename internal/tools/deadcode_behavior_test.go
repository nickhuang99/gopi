package tools

import (
	"strings"
	"testing"
)

// These tests lock behavior AROUND the functions the Phase-2 cleanup may
// delete (JobCount / Bash), so deletion is provably safe: if a deleted
// function's surrounding behavior still holds without it, the deletion
// cannot regress current behavior.

// ---- Bash vs BashInDir equivalence ----

// TestBashEqualsBashInDirEmpty locks that Bash(cmd) produces byte-identical
// output to BashInDir(cmd, "") — the empty-workdir form Bash *is* an alias
// for. If Phase 2 deletes Bash, the loop must keep dispatching through
// BashInDir(command, "") and get the same result.
func TestBashEqualsBashInDirEmpty(t *testing.T) {
	if RejectDangerous {
		// These are harmless; keep the guard in case a future pattern matches.
		prev := RejectDangerous
		RejectDangerous = false
		t.Cleanup(func() { RejectDangerous = prev })
	}
	outBash, errBash := Bash("printf 'hello %s\\n' world")
	if errBash != nil {
		t.Fatalf("Bash error: %v", errBash)
	}
	outInDir, errInDir := BashInDir("printf 'hello %s\\n' world", "")
	if errInDir != nil {
		t.Fatalf("BashInDir error: %v", errInDir)
	}
	if outBash != outInDir {
		t.Errorf("Bash != BashInDir(\"\",):\nBash:       %q\nBashInDir:  %q", outBash, outInDir)
	}
}

// TestBashInDir_NonEmptyDiffers locks the workdir semantics so a refactor
// cannot silently drop the workdir distinction: BashInDir with a real
// directory is NOT identical to Bash (the command runs in that directory).
func TestBashInDir_NonEmptyDiffers(t *testing.T) {
	dir := chdirTemp(t)
	out, err := BashInDir("pwd", dir)
	if err != nil {
		t.Fatalf("BashInDir in temp dir: %v", err)
	}
	if !strings.Contains(out, dir) {
		t.Errorf("expected pwd to contain workdir %q, got %q", dir, out)
	}
}

// ---- job registry count semantics ----

// TestJobRegistryRegistrationCount locks the registry contract that JobCount
// previously exposed: each JobStart adds exactly one job, and ResetJobs
// empties the registry. Phase 2 may delete the JobCount accessor itself, but
// the underlying registry helpers (the merged job tool's start/output/kill
// handlers) must keep feeding the same count semantics.
func TestJobRegistryRegistrationCount(t *testing.T) {
	ResetJobs()
	cleanupJobs(t)

	before := len(registeredJobIDs())
	id, err := JobStart(jobRegistryProbeCmd)
	if err != nil {
		t.Fatalf("JobStart error: %v", err)
	}
	if len(registeredJobIDs()) != before+1 {
		t.Errorf("expected exactly 1 new job registered, got count %d (before %d)",
			len(registeredJobIDs()), before)
	}
	if id == "" {
		t.Fatal("empty job id returned by JobStart")
	}
	// Grab the job handle BEFORE the reset below: ResetJobs empties the
	// registry, which orphans the process (cleanupJobs can no longer kill
	// it), yet its reaper still appends an exited JobNotice when it ends.
	// Waiting on the orphan here is what stops that notice from landing in
	// a LATER test's notification window (the suite-order leak that broke
	// TestJobNoNoticeOnKill once the scratch-root fix made the suite fast
	// enough for the window to be hit — see jobOrphanNoticeWait).
	orphan, err := lookupJob(extractJobID(t, id))
	if err != nil {
		t.Fatalf("lookupJob(%s): %v", id, err)
	}

	// ResetJobs clears the registry.
	ResetJobs()
	if len(registeredJobIDs()) != 0 {
		t.Errorf("ResetJobs left %d registered jobs, want 0", len(registeredJobIDs()))
	}

	waitOrphanReaped(t, id, orphan)
}

// waitOrphanReaped is the drainOrphanNotice helper under the name the
// registry test reads best by; both funnel into the same bounded wait.
func waitOrphanReaped(t *testing.T, jobID string, job *Job) {
	t.Helper()
	drainOrphanNotice(t, jobID, job)
}

// TestJobRegistry_UnknownIDNotRegistered locks that a job that was never
// started (or was reset away) is not visible — the registry only counts
// successfully started jobs.
func TestJobRegistry_UnknownIDNotRegistered(t *testing.T) {
	ResetJobs()
	cleanupJobs(t)
	for _, id := range registeredJobIDs() {
		if id == "job-does-not-exist" {
			t.Fatal("phantom job id present before any start")
		}
	}
}
