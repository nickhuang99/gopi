package tools

import (
	"strings"
	"testing"
	"time"
)

// TestJobNoticeOnCompletion verifies P0-3: a naturally-finished job lands a
// JobNotice in the queue (drained via TakeJobNotices) — the agent loop
// injects it as a user message, so the model never busy-polls.
func TestJobNoticeOnCompletion(t *testing.T) {
	cleanupJobs(t)
	// Drain any stale notices from previous tests.
	TakeJobNotices()

	out, err := JobStart("echo done; sleep 0.3; echo finished")
	if err != nil {
		t.Fatalf("JobStart: %v", err)
	}
	jobID := extractJobID(t, out)

	// The reaper enqueues the notice asynchronously — bounded wait.
	deadline := time.Now().Add(5 * time.Second)
	var notices []JobNotice
	for time.Now().Before(deadline) {
		notices = TakeJobNotices()
		if len(notices) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond) //stylegate:allow-sleep leak window: a wrongly enqueued notice surfaces only after the reaper tick
	}
	if len(notices) == 0 {
		t.Fatal("no job notice after 5s — reaper did not enqueue completion")
	}
	n := notices[0]
	if n.JobID != jobID {
		t.Errorf("notice JobID=%q, want %q", n.JobID, jobID)
	}
	if n.Status != JobExited {
		t.Errorf("notice Status=%q, want exited", n.Status)
	}
	if n.ExitCode != 0 {
		t.Errorf("notice ExitCode=%d, want 0", n.ExitCode)
	}
	if !strings.Contains(n.Command, "echo done") {
		t.Errorf("notice Command=%q missing job command", n.Command)
	}
	// Notices are drained atomically: a second call returns nothing new.
	if extra := TakeJobNotices(); len(extra) != 0 {
		t.Errorf("TakeJobNotices after drain returned %d notices", len(extra))
	}
}

// TestJobNoNoticeOnKill verifies the kill action produces NO notice (the kill is
// synchronous — the caller already knows the outcome).
func TestJobNoNoticeOnKill(t *testing.T) {
	cleanupJobs(t)
	TakeJobNotices()

	out, err := JobStart(jobNoticeKillProbeCmd)
	if err != nil {
		t.Fatalf("JobStart: %v", err)
	}
	jobID := extractJobID(t, out)
	if _, err := JobKill(jobID); err != nil {
		t.Fatalf("JobKill: %v", err)
	}
	// Give the reaper a moment; a wrongly-enqueued notice would appear here.
	time.Sleep(jobNoticeLeakWindow) //stylegate:allow-sleep leak window: a wrongly enqueued notice surfaces only after the reaper tick
	if notices := TakeJobNotices(); len(notices) != 0 {
		t.Errorf("killed job produced %d notices: %+v", len(notices), notices)
	}
}

const (
	// jobNoticeKillProbeCmd is a command that must still be running when the
	// kill lands (the kill is what is under test).
	jobNoticeKillProbeCmd = "sleep 30"
	// jobNoticeLeakProbeCmd is the short-lived orphan of the leak repro. The
	// real predecessor uses a 1s probe; the repro compresses it so the
	// orphan's notice is guaranteed to land well inside the leak window
	// below (deterministic, not load-dependent).
	jobNoticeLeakProbeCmd = "sleep 0.1"
	// jobNoticeLeakWindow is how long the "no notice" assertion below waits
	// for a leaked notice to appear — long enough to cover the repro's
	// orphan lifetime with margin.
	jobNoticeLeakWindow = 300 * time.Millisecond
)

// TestOrphanedJobNoticeDoesNotLeak — regression guard for the suite-order
// notice leak that the scratch-root fix exposed (t-1788361069 round 2). A
// test that starts a job and then calls ResetJobs() ORPHANS the process: the
// registry no longer holds it, so cleanupJobs cannot kill it — but its
// reaper still appends an exited JobNotice whenever it ends. That notice
// stays in the process-global queue and lands inside the next test that
// asserts an empty queue (TestJobNoNoticeOnKill), failing a test that is
// itself correct. The leak is invisible while the suite takes minutes to
// reach that assertion; it appeared the moment the suite became fast.
//
// The fix's contract — the PRODUCER waits out its own orphan and consumes
// the notice (drainOrphanNotice, called by TestJobRegistryRegistrationCount)
// — is what this test locks: removing that drain makes phase 2 below see the
// orphan's notice and go red.
func TestOrphanedJobNoticeDoesNotLeak(t *testing.T) {
	// ---------------- phase 1: the poisoning predecessor ----------------
	cleanupJobs(t)
	TakeJobNotices()
	out, err := JobStart(jobNoticeLeakProbeCmd)
	if err != nil {
		t.Fatalf("JobStart: %v", err)
	}
	orphanID := extractJobID(t, out)
	orphan, err := lookupJob(orphanID)
	if err != nil {
		t.Fatalf("lookupJob(%s): %v", orphanID, err)
	}
	ResetJobs() // the orphan is now unreachable — its reaper still fires
	// The producer's obligation: wait the orphan out and consume its notice.
	drainOrphanNotice(t, orphanID, orphan)

	// ---------------- phase 2: a later test's empty-queue window ----------
	cleanupJobs(t)
	out, err = JobStart(jobNoticeKillProbeCmd)
	if err != nil {
		t.Fatalf("JobStart: %v", err)
	}
	if _, err := JobKill(extractJobID(t, out)); err != nil {
		t.Fatalf("JobKill: %v", err)
	}
	time.Sleep(jobNoticeLeakWindow) //stylegate:allow-sleep leak window: a wrongly enqueued notice surfaces only after the reaper tick
	if notices := TakeJobNotices(); len(notices) != 0 {
		t.Fatalf("an earlier test's orphaned job leaked %d notice(s) into this window: %+v — the producing test must drain its own orphan (drainOrphanNotice)", len(notices), notices)
	}
}

// TestJobNoticeExitCodePreserved verifies non-zero exit codes land in the
// notice (the model must see build failures without polling).
func TestJobNoticeExitCodePreserved(t *testing.T) {
	cleanupJobs(t)
	TakeJobNotices()

	out, err := JobStart("exit 7")
	if err != nil {
		t.Fatalf("JobStart: %v", err)
	}
	// Only the exit code matters here — no need to bind the job id.
	_ = extractJobID(t, out)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if notices := TakeJobNotices(); len(notices) > 0 {
			if notices[0].ExitCode != 7 {
				t.Fatalf("notice ExitCode=%d, want 7", notices[0].ExitCode)
			}
			return
		}
		time.Sleep(20 * time.Millisecond) //stylegate:allow-sleep leak window: a wrongly enqueued notice surfaces only after the reaper tick
	}
	t.Fatal("no notice after 5s")
}
