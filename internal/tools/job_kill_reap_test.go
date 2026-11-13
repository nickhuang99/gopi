package tools

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// job_kill_reap_test.go — the two arms that make "killed" mean the TREE is gone.
//
// ARM 1 (the defect): a job whose own command runs `setsid …` puts its real work
// in ANOTHER session and group (scripts/test-budget.sh launches `go test`
// exactly that way), so JobKill's group signal cannot reach it. The sweep by the
// job's own marker must kill it, and the read-back must show it gone.
//
// ARM 2 (the positive control): an unrelated process that carries no marker must
// SURVIVE the same JobKill. Without this arm "kill everything you can see" would
// pass arm 1 and no test would notice (Rule 42: a signal needs provable
// ownership, not a licence).
//
// Both arms assert on /proc, not on the tool's own words — except that arm 1 also
// requires JobKill's result to SAY what it did.

const (
	// jobReapGrandchildSleep is how long the detached grandchild sleeps: long
	// enough that "it is gone" can only be the reap's doing.
	jobReapGrandchildSleep = "300"
	// jobReapPidFileWait bounds the wait for the detached grandchild to publish
	// its pid (its bash must fork, setsid, then exec).
	jobReapPidFileWait = 5 * time.Second
	// jobReapGoneWait bounds the read-back that asserts the grandchild died.
	jobReapGoneWait = 5 * time.Second
	// jobReapPollStep is the interval both bounded waits sleep between readings.
	jobReapPollStep = 20 * time.Millisecond
	// jobReapControlSleep is the unrelated survivor's lifetime.
	jobReapControlSleep = "60"
)

// waitJobPidFile waits for the detached grandchild to write its pid, and returns
// it. The pid file is inside the test's own temp dir (off `/`, Rule 22).
func waitJobPidFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(jobReapPidFileWait)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil {
			if pid, cerr := strconv.Atoi(strings.TrimSpace(string(b))); cerr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(jobReapPollStep) //stylegate:allow-sleep bounded poll for a file a spawned child writes; the kernel offers no event for it
	}
	t.Fatalf("the detached grandchild never published its pid in %s within %v", path, jobReapPidFileWait)
	return 0
}

// jobPidGoneFor reports whether pid stops being a LIVE process within the
// window. A zombie counts as gone (the kernel has ended it; its parent — the
// kernel's reaper, after reparenting — owes the wait, not this test).
func jobPidGoneFor(t *testing.T, pid int, window time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		if !PidAlive(pid) || pidIsZombie(pid) {
			return true
		}
		time.Sleep(jobReapPollStep) //stylegate:allow-sleep bounded liveness poll after a signal; kill(2) only queues, so death must be measured
	}
	return false
}

// TestJobKillKillsDetachedGrandchild is the regression guard for the epic: a
// descendant that left the job's session (setsid) is still the job's own work,
// and killing the job must leave nothing of it behind.
func TestJobKillKillsDetachedGrandchild(t *testing.T) {
	cleanupJobs(t)
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "grandchild.pid")

	// `setsid bash -c 'echo $$ > f; exec sleep …'` is the exact shape of the
	// measured leak: a NEW session (so a group signal from JobKill cannot reach
	// it) whose process is the job's descendant and therefore carries the job's
	// marker.
	cmdline := "setsid bash -c 'echo $$ > " + pidFile + "; exec sleep " + jobReapGrandchildSleep + "' > /dev/null 2>&1 & sleep " + jobReapGrandchildSleep
	out, err := JobStart(cmdline)
	if err != nil {
		t.Fatalf("JobStart: %v", err)
	}
	jobID := extractJobID(t, out)

	grandchild := waitJobPidFile(t, pidFile)
	t.Cleanup(func() {
		// A failed assertion must not leave the fixture running: this is the
		// test's OWN process, created by this test (Rule 42). The kill's error
		// is READ, not dropped (Rule 15b): ESRCH is the expected answer when the
		// reap under test already did it, and anything else means the fixture is
		// still out there and the next run inherits it.
		if PidAlive(grandchild) && !pidIsZombie(grandchild) {
			kerr := syscall.Kill(grandchild, syscall.SIGKILL)
			if kerr != nil && !errors.Is(kerr, syscall.ESRCH) {
				t.Logf("cleanup: the detached grandchild (pid %d) could not be killed: %v", grandchild, kerr)
			}
		}
	})

	job, err := lookupJob(jobID)
	if err != nil {
		t.Fatalf("lookupJob: %v", err)
	}
	// THE PREMISE: the grandchild is in a DIFFERENT session from the job, which
	// is why the group signal alone was never going to reach it.
	jobSid, ok1 := procSessionOf(job.PID)
	gcSid, ok2 := procSessionOf(grandchild)
	if !ok1 || !ok2 {
		t.Fatalf("could not read sessions (job pid %d, grandchild pid %d)", job.PID, grandchild)
	}
	if jobSid == gcSid {
		t.Fatalf("fixture broken: the grandchild (pid %d) shares session %d with the job (pid %d) — it never detached", grandchild, jobSid, job.PID)
	}

	killOut, err := JobKill(jobID)
	if err != nil {
		t.Fatalf("JobKill: %v", err)
	}

	if !jobPidGoneFor(t, grandchild, jobReapGoneWait) {
		t.Errorf("the job's detached grandchild (pid %d, session %d ≠ job session %d) is STILL ALIVE after JobKill — the group signal cannot reach it and the marker sweep did not either; JobKill said: %q",
			grandchild, gcSid, jobSid, killOut)
	}
	if !strings.Contains(killOut, "killed") {
		t.Errorf("expected a kill confirmation, got: %q", killOut)
	}
	if strings.Contains(killOut, "SURVIVOR") {
		t.Errorf("JobKill reported survivors although the grandchild is gone: %q", killOut)
	}
}

// TestJobKillSparesUnmarkedProcess is the positive control: the same JobKill must
// leave a process that carries no marker of this job alone. It is the arm that
// fails if ownership is ever widened to "everything I can see" or "everything in
// my cwd".
func TestJobKillSparesUnmarkedProcess(t *testing.T) {
	cleanupJobs(t)

	control := exec.Command("sleep", jobReapControlSleep)
	// Pdeathsig, so the control cannot outlive this test BINARY (armed here
	// rather than through testutil.StartMockProcessCmd because the arm's subject
	// is JobKill's ownership rule, and the fixture must stay OUTSIDE every
	// group/marker the job machinery owns — the reaping helper gives the child a
	// process group of its own and a neutral cwd, which is a different stand-in
	// than "a bystander the job never saw"). t.Cleanup below kills and REAPS it
	// on every in-process exit path; this flag covers the one path where no Go
	// code runs: a SIGKILLed or -timeout'd test binary, whose leftover `sleep 60`
	// would otherwise stand in the checkout and pin the ground IN USE.
	control.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := control.Start(); err != nil {
		t.Fatalf("start control process: %v", err)
	}
	t.Cleanup(func() {
		// Both errors are READ rather than dropped (Rule 15b). The control is
		// this test's own child, so the only expected failures are "it had
		// already exited" (os.ErrProcessDone / ESRCH) for the kill and the
		// reaper's own "signal: killed" report for the wait — anything else
		// means the fixture outlived the test, which a later run would pay for.
		if kerr := control.Process.Kill(); kerr != nil &&
			!errors.Is(kerr, os.ErrProcessDone) && !errors.Is(kerr, syscall.ESRCH) {
			t.Logf("cleanup: the unmarked control (pid %d) could not be killed: %v", control.Process.Pid, kerr)
		}
		if _, werr := control.Process.Wait(); werr != nil {
			var exitErr *exec.ExitError
			if !errors.As(werr, &exitErr) {
				t.Logf("cleanup: the unmarked control (pid %d) could not be reaped: %v", control.Process.Pid, werr)
			}
		}
	})
	controlPid := control.Process.Pid

	out, err := JobStart("sleep " + jobReapGrandchildSleep)
	if err != nil {
		t.Fatalf("JobStart: %v", err)
	}
	jobID := extractJobID(t, out)

	killOut, err := JobKill(jobID)
	if err != nil {
		t.Fatalf("JobKill: %v", err)
	}
	if !strings.Contains(killOut, "killed") {
		t.Errorf("expected a kill confirmation, got: %q", killOut)
	}

	// The control must be UNTOUCHED: read /proc, not the tool's word.
	if !PidAlive(controlPid) || pidIsZombie(controlPid) {
		t.Fatalf("the unrelated control process (pid %d) was killed by job %s — JobKill reached a process it does not own (Rule 42); result: %q",
			controlPid, jobID, killOut)
	}
}

// procSessionOf reads a pid's session id from /proc (the kernel's own statement
// about which session a process belongs to). (false) = unreadable.
func procSessionOf(pid int) (int, bool) {
	b, err := os.ReadFile(filepath.Join(procRootName, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, false
	}
	// "<pid> (<comm>) <state> <ppid> <pgrp> <session> …" — comm may contain
	// spaces and parentheses, so the fields start after the LAST ')'.
	i := strings.LastIndexByte(string(b), ')')
	if i < 0 || i+2 >= len(b) {
		return 0, false
	}
	fields := strings.Fields(string(b[i+2:]))
	if len(fields) < 4 {
		return 0, false
	}
	sid, err := strconv.Atoi(fields[3]) // state, ppid, pgrp, session
	if err != nil {
		return 0, false
	}
	return sid, true
}

// TestJobKillReapLineCarriesTheUndeliveredSignal is the regression guard for the
// sentence the sweep used to be unable to say. SignalIfSameIncarnation
// returns (outcome, error) as ONE fact, and the sweep read only the outcome, so
// a delivery that failed for a reason that was neither "recycled" nor "already
// gone" reached the operator as nothing at all — a pid that was in neither
// Signalled nor Survivors, and a "killed" line that did not mention it.
func TestJobKillReapLineCarriesTheUndeliveredSignal(t *testing.T) {
	const marker = "job-1-1"

	// NIL-SAFETY FIRST: a sweep that delivered nothing and has nothing to
	// report must stay silent, or every JobKill on a clean tree grows a line.
	if line := jobKillReapLine("job-1", marker, jobKillReapResult{}); line != "" {
		t.Errorf("an empty sweep rendered %q, want \"\" — the silent case is the common one", line)
	}

	undelivered := jobKillReapResult{Undelivered: []string{"pid 4242: delivered (permission denied)"}}
	line := jobKillReapLine("job-1", marker, undelivered)
	if !strings.Contains(line, "no detached descendant of this job was signalled") ||
		!strings.Contains(line, "pid 4242") {
		t.Errorf("a sweep with ONLY an undelivered pid rendered %q — it must say that nothing was signalled and name the pid", line)
	}
	if strings.Contains(line, "killed") {
		t.Errorf("the render claimed a kill for a sweep that delivered nothing: %q", line)
	}

	mixed := jobKillReapResult{Signalled: []int{7}, Undelivered: []string{"pid 4242: delivered (permission denied)"}}
	line = jobKillReapLine("job-1", marker, mixed)
	if !strings.Contains(line, "killed 1 detached descendant(s)") || !strings.Contains(line, "NOT delivered") {
		t.Errorf("a mixed sweep rendered %q — it must claim the delivered pid AND name the undelivered one", line)
	}
}

// TestJobKillReapLineNeverDropsAnUndeliveredPidBehindAHeadline pins the TWO
// sentences that replaced the "killed" claim and used to swallow it. Both are
// branches that RETURN EARLY, so neither reached the undelivered clause:
//
//   - SURVIVORS (measured before the fix: jobKillReapLine with
//     Survivors=[9] and one undelivered pid rendered "…did not stop them" and
//     named pid 4242 NOWHERE — a delivery that failed reached the operator as
//     nothing at all, which is the exact defect of this epic);
//   - a sweep that DELIVERED NOTHING because every pid was refused, which
//     rendered "killed 0 detached descendant(s)" — a kill sentence for a sweep
//     that killed none.
func TestJobKillReapLineNeverDropsAnUndeliveredPidBehindAHeadline(t *testing.T) {
	const marker = "job-1-1"
	const failure = "pid 4242: gone (operation not permitted)"

	survivors := jobKillReapResult{Survivors: []int{9}, Undelivered: []string{failure}}
	line := jobKillReapLine("job-1", marker, survivors)
	if !strings.Contains(line, "SURVIVORS REMAIN") {
		t.Fatalf("the survivors headline went missing: %q", line)
	}
	if !strings.Contains(line, "4242") || !strings.Contains(line, "operation not permitted") {
		t.Errorf("a survivors sweep rendered %q — the undelivered pid and its error must survive the headline (Rule 15: the pid is in neither Signalled nor Survivors, so this sentence is its ONLY voice)", line)
	}

	refusedOnly := jobKillReapResult{Recycled: 2, Undelivered: []string{failure}}
	line = jobKillReapLine("job-1", marker, refusedOnly)
	if strings.Contains(line, "killed") {
		t.Errorf("a sweep that delivered NOTHING rendered %q — \"killed\" must not be claimed for zero deliveries", line)
	}
	if !strings.Contains(line, "2 pid(s) refused") || !strings.Contains(line, "4242") {
		t.Errorf("a refusal-only sweep rendered %q — it must say how many were refused AND name the undelivered one", line)
	}

	// THE COMMON CASE STAYS SHORT: a delivered pid with nothing undelivered
	// must not grow a trailing clause.
	clean := jobKillReapResult{Signalled: []int{7}}
	line = jobKillReapLine("job-1", marker, clean)
	if strings.Contains(line, "NOT delivered") {
		t.Errorf("a clean sweep rendered %q — the undelivered clause must be empty when nothing is undelivered", line)
	}
}
