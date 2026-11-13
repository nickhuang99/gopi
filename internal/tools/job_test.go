package tools

import (
	"strings"
	"syscall"
	"testing"
	"time"
)

// cleanupJobs kills every registered job and clears the registry so tests
// never leak detached processes.
func cleanupJobs(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		for _, id := range registeredJobIDs() {
			JobKill(id)
		}
		ResetJobs()
	})
}

const (
	// jobRegistryProbeCmd is the throwaway command the registry-count test
	// starts. Deliberately NOT an instantly-exiting command: the test's
	// point is a registered *running* job, so the probe must still be alive
	// when the count is asserted.
	jobRegistryProbeCmd = "sleep 1"
	// jobOrphanNoticeWait bounds the wait for an orphaned probe job to be
	// reaped (its process is unreachable once ResetJobs dropped it).
	jobOrphanNoticeWait = 5 * time.Second
)

// drainOrphanNotice waits for a job that the registry can no longer reach
// (ResetJobs dropped it before it exited) and consumes the completion
// notice its reaper appends. Without this, that notice sits in the
// process-global queue until the NEXT test drains it — TestJobNoNoticeOnKill
// asserts an EMPTY queue right after its kill, so a leaked notice lands
// inside its assertion window and fails it (suite-order leak, invisible
// while the suite took minutes to reach that test; it surfaced as soon as
// the scratch-root fix made the run fast — see testsuite_temproot_test.go).
func drainOrphanNotice(t *testing.T, jobID string, job *Job) {
	t.Helper()
	select {
	case <-job.done:
	case <-time.After(jobOrphanNoticeWait):
		t.Fatalf("orphaned job %s was not reaped within %v", jobID, jobOrphanNoticeWait)
	}
	if stale := TakeJobNotices(); len(stale) != 0 {
		t.Logf("drained %d notice(s) from orphaned job(s) %+v", len(stale), stale)
	}
}

// registeredJobIDs snapshots the current job ids (thread-safe).
func registeredJobIDs() []string {
	theJobs.mu.Lock()
	defer theJobs.mu.Unlock()
	ids := make([]string, 0, len(theJobs.jobs))
	for id := range theJobs.jobs {
		ids = append(ids, id)
	}
	return ids
}

// waitJobOutput polls JobOutput until the job leaves "running" (bounded).
func waitJobOutput(t *testing.T, jobID string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := JobOutput(jobID)
		if err != nil {
			t.Fatalf("JobOutput(%s): %v", jobID, err)
		}
		if !strings.Contains(out, "status=running") {
			return out
		}
		time.Sleep(20 * time.Millisecond) //stylegate:allow-sleep poll interval while the job transitions to running
	}
	t.Fatalf("job %s still running after %s", jobID, timeout)
	return ""
}

func TestJobStartOutput(t *testing.T) {
	cleanupJobs(t)
	out, err := JobStart("echo hello-from-job; echo err-line >&2")
	if err != nil {
		t.Fatalf("JobStart: %v", err)
	}
	jobID := extractJobID(t, out)

	// Long enough that the job has surely finished, short enough for CI.
	final := waitJobOutput(t, jobID, 5*time.Second)
	if !strings.Contains(final, "status=exited code=0") {
		t.Errorf("expected exited code=0, got: %q", final)
	}
	if !strings.Contains(final, "hello-from-job") {
		t.Errorf("expected stdout captured, got: %q", final)
	}
	if !strings.Contains(final, "err-line") {
		t.Errorf("expected stderr captured, got: %q", final)
	}
}

func TestJobStartDetached(t *testing.T) {
	cleanupJobs(t)
	out, err := JobStart("sleep 60")
	if err != nil {
		t.Fatalf("JobStart: %v", err)
	}
	jobID := extractJobID(t, out)

	// The whole point of A2: the job must be a session leader (setsid), so it
	// is in its own process group and no bash-tool group kill can reach it.
	job, err := lookupJob(jobID)
	if err != nil {
		t.Fatalf("lookupJob: %v", err)
	}
	pgid, err := syscall.Getpgid(job.PID)
	if err != nil {
		t.Fatalf("Getpgid(%d): %v", job.PID, err)
	}
	if pgid != job.PID {
		t.Errorf("expected job pid %d to lead its own process group (setsid), pgid is %d", job.PID, pgid)
	}
	// And it must NOT be in the test process's group (where a bash-tool
	// group kill would have reached it).
	if pgid == syscall.Getpgrp() {
		t.Errorf("job pid %d shares the test process group %d — detach failed", job.PID, pgid)
	}
}

// TestJobSurvivesBashGroupKill is the E2E reproduction of the competition #4
// failure: a 2-minute bash timeout killed the background build because it
// shared the wrapper's process group. A job started via the start action must survive a
// bash tool group kill.
func TestJobSurvivesBashGroupKill(t *testing.T) {
	cleanupJobs(t)
	out, err := JobStart("sleep 60")
	if err != nil {
		t.Fatalf("JobStart: %v", err)
	}
	jobID := extractJobID(t, out)

	// A bash call that times out and gets its whole process group SIGKILLed.
	// The job must still be running afterwards.
	_, err = BashWithTimeout("sleep 30", 1)
	if err == nil {
		t.Fatal("expected the bash call to time out")
	}

	jobOut, err := JobOutput(jobID)
	if err != nil {
		t.Fatalf("JobOutput: %v", err)
	}
	if !strings.Contains(jobOut, "status=running") {
		t.Errorf("job died with the bash wrapper's group kill — detach broken: %q", jobOut)
	}
}

func TestJobKill(t *testing.T) {
	cleanupJobs(t)
	out, err := JobStart("sleep 60")
	if err != nil {
		t.Fatalf("JobStart: %v", err)
	}
	jobID := extractJobID(t, out)

	killOut, err := JobKill(jobID)
	if err != nil {
		t.Fatalf("JobKill: %v", err)
	}
	if !strings.Contains(killOut, "killed") {
		t.Errorf("expected kill confirmation, got: %q", killOut)
	}

	// Idempotent second kill reports the final state instead of failing.
	killOut2, err := JobKill(jobID)
	if err != nil {
		t.Fatalf("JobKill (second): %v", err)
	}
	if !strings.Contains(killOut2, "already") {
		t.Errorf("expected idempotent 'already ...' reply, got: %q", killOut2)
	}

	final := waitJobOutput(t, jobID, 5*time.Second)
	if !strings.Contains(final, "status=killed") {
		t.Errorf("expected status=killed after JobKill, got: %q", final)
	}
}

func TestJobOutputUnknown(t *testing.T) {
	cleanupJobs(t)
	_, err := JobOutput("job-999")
	if err == nil || !strings.Contains(err.Error(), "unknown job id") {
		t.Errorf("expected unknown-job error, got: %v", err)
	}
	_, err = JobKill("job-999")
	if err == nil || !strings.Contains(err.Error(), "unknown job id") {
		t.Errorf("expected unknown-job error, got: %v", err)
	}
}

func TestJobOutputRunning(t *testing.T) {
	cleanupJobs(t)
	out, err := JobStart("sleep 30; echo after-sleep")
	if err != nil {
		t.Fatalf("JobStart: %v", err)
	}
	jobID := extractJobID(t, out)

	// Immediately after start the job must report running, not exited.
	jobOut, err := JobOutput(jobID)
	if err != nil {
		t.Fatalf("JobOutput: %v", err)
	}
	if !strings.Contains(jobOut, "status=running") {
		t.Errorf("expected status=running right after start, got: %q", jobOut)
	}
}

func TestJobStartEmptyCommand(t *testing.T) {
	cleanupJobs(t)
	if _, err := JobStart("   "); err == nil {
		t.Error("expected empty-command error")
	}
}

// extractJobID parses the "job started: <id>" line of JobStart's result.
func extractJobID(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "job started: ") {
			return strings.TrimPrefix(line, "job started: ")
		}
	}
	t.Fatalf("no job id in JobStart result: %q", out)
	return ""
}
