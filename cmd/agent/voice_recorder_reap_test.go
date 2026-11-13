package main

// voice_recorder_reap_test.go — THE RECORDER'S TWO OBLIGATIONS TO ITS CALLER
// (bug 01a163df, measured 2026-10-22).
//
// THE DEFECT this file guards, as it appeared in the process table of a live
// `gopi chat`:
//
//	PID      PPID     PGID     ETIME   CMD
//	1155901  1134893  1134893  10:42   [parecord] <defunct>
//	1173820  1134893  1134893  04:39   [parecord] <defunct>
//	1181728  1134893  1134893  02:56   [parecord] <defunct>
//
// Three recorders, all children of the chat (PPID 1134893), all in the CHAT'S
// OWN process group (PGID 1134893), all ZOMBIES — exited, never waited for. Two
// separate failures produced that table and each has its own case below:
//
//	pgroup-is-ours-not-their — the recorder must LEAD ITS OWN GROUP. Without
//	   Setpgid it inherits the caller's, and any group signal a tool later sends
//	   at "the child's group" reaches the operator's terminal (SIGKILL cannot be
//	   caught, so no handler can soften it).
//	ignored-stop-still-reaped — the grace expiry must KILL and then WAIT. The
//	   old branch only returned an error, on the assumption that "the caller's
//	   context kills it": the context kills the pid, and the `exited` channel was
//	   read by nobody once the grace select had fired, so even a recorder that
//	   DID exit became a zombie.
//
// WHY parecord IS MODELLED AS IGNORING THE STOP SIGNAL: it does. It has no
// duration flag, its window is closed by SIGINT — and a recorder that misses
// the signal (a wedged device read, a stopped process) is exactly the case the
// grace exists for. The fixture is a `sleep` that IGNORES SIGINT, `exec`ed so
// the pid under test is the process that ignores it rather than a shell above
// it: that is what makes "did the KILL land on THIS pid" readable.
//
// Every bound is a named constant (Rule 1) and every wait is a POLL against the
// process table, never a sleep the verdict rests on.

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	// recorderFixtureSleepSec is the fixture recorder's sleep: long enough that
	// no case can pass by the process ending on its own.
	recorderFixtureSleepSec = 600
	// recorderFixtureReadyWait is the readiness handshake — the fixture is up and
	// has installed its signal disposition before the code under test runs.
	recorderFixtureReadyWait = 5 * time.Second
	// recorderFixtureReadyPoll is the poll step for that handshake and for the
	// process-table waits below.
	recorderFixtureReadyPoll = 20 * time.Millisecond
	// recorderFixtureReapWait is the window the reaped-process check is polled
	// in. A reaped pid is gone from /proc immediately, so this is a readiness
	// window, not a budget.
	recorderFixtureReapWait = 2 * time.Second
	// recorderFixtureFastStopSec is the headroom added to the forced stop's
	// BOUNDED ending (grace + force-kill window): it is an upper bound on the
	// forced path, so it is generous on purpose and no case rests on it being
	// tight. The normal path is NOT bounded here — see the graceful case for why
	// a wall-clock upper bound on it was a host-speed flake.
	recorderFixtureFastStopSec = time.Second
)

// recorderFixtureIgnoringBody ignores SIGINT and then REPLACES the shell with the
// sleep (exec), so the pid this test holds is the process that ignores the
// signal. SIG_IGN survives execve, which is why the disposition is installed in
// the shell and still in force in the sleep.
func recorderFixtureIgnoringBody() string {
	return "trap '' INT; exec sleep " + strconv.Itoa(recorderFixtureSleepSec)
}

// recorderFixturePoliteBody honours SIGINT by terminating, the way arecord does
// once its buffer is flushed.
func recorderFixturePoliteBody() string {
	return "exec sleep " + strconv.Itoa(recorderFixtureSleepSec)
}

// startRecorderFixture spawns a fake recorder with the PRODUCTION group policy
// (voiceRecorderSysProcAttr) and the stdout pipe stopVoiceRecorder closes.
func startRecorderFixture(t *testing.T, body string) (*exec.Cmd, *os.File) {
	t.Helper()
	cmd := exec.Command("sh", "-c", body)
	cmd.SysProcAttr = voiceRecorderSysProcAttr()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the recorder fixture: %v", err)
	}
	// THE REAP IS THE FIXTURE'S OWN. `cmd.Wait` may be called once per child and
	// the code under test owns that call (stopVoiceRecorder reads `exited`
	// itself), so this hook must never call it unconditionally: it only has the
	// job of retiring a fixture a case LEFT RUNNING (the process-group case,
	// which must outlive itself to be read). That is what the leak below was.
	t.Cleanup(func() {
		// Fixture teardown only: on the stopVoiceRecorder cases the process is
		// already reaped and both calls are no-ops (ESRCH / "already waited").
		//
		// THE WAIT IS NOT OPTIONAL. A case that only STARTS the fixture (the
		// process-group case — the process must outlive the case to be read)
		// would otherwise leave a zombie behind, and the suite's own child sweep
		// measured exactly that on the first run of this file:
		// `leaked child pid=1210595 state=Z`. A test that leaves the very state
		// its subject is about is not a test of it.
		if cmd.Process != nil {
			_ = cmd.Process.Kill() //stylegate:ignore fixture teardown; the process is expected to be gone already
		}
		_ = cmd.Wait() //stylegate:ignore reaping the fixture is the point; a double Wait on an already-reaped child is expected
	})
	// The shell must be past its `trap`/`exec` before the stop signal is sent,
	// or the fixture would be told to ignore a signal it has not yet ignored and
	// the case would pass for the wrong reason. Reading the child's own SIGINT
	// disposition out of /proc is that proof.
	if !recorderWaitFor(func() bool { return recorderIgnoresInterrupt(cmd.Process.Pid) || !strings.Contains(body, "trap") }, recorderFixtureReadyWait) {
		t.Fatalf("the fixture never installed its signal disposition (pid %d)", cmd.Process.Pid)
	}
	return cmd, stdout.(*os.File)
}

// recorderIgnoresInterrupt reports whether pid's SIGINT disposition is SIG_IGN,
// read from /proc/<pid>/status (SigIgn is a bitmask over signal numbers, so
// bit N-1 is signal N).
func recorderIgnoresInterrupt(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return false
	}
	sigintMask := uint64(1) << (uint64(syscall.SIGINT) - 1)
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "SigIgn:") {
			continue
		}
		var mask uint64
		for _, hex := range strings.Fields(strings.TrimPrefix(strings.TrimPrefix(line, "SigIgn:"), "\t")) {
			var v uint64
			for _, c := range hex {
				var d uint64
				switch {
				case c >= '0' && c <= '9':
					d = uint64(c - '0')
				case c >= 'a' && c <= 'f':
					d = uint64(c-'a') + 10
				case c >= 'A' && c <= 'F':
					d = uint64(c-'A') + 10
				default:
					return false
				}
				v = v<<4 | d
			}
			mask |= v
		}
		return mask&sigintMask != 0
	}
	return false
}

// recorderWaitFor polls cond until it holds or the window expires.
func recorderWaitFor(cond func() bool, window time.Duration) bool {
	deadline := time.Now().Add(window)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(recorderFixtureReadyPoll) //stylegate:allow-sleep the poll step of a bounded wait on the process table; the verdict never rests on a fixed sleep
	}
}

// TestVoiceRecorderLeadsItsOwnProcessGroup is case (B): the spawned recorder's
// process group is ITS OWN, never the caller's.
//
// This is the structural half of the fix, and the half that survives even if
// the reap were wrong: a group signal addressed at the chat cannot reach a
// process that is not in the chat's group, and a signal addressed at the
// recorder's group cannot reach the chat.
func TestVoiceRecorderLeadsItsOwnProcessGroup(t *testing.T) {
	callerGroup, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		t.Fatalf("read this process's group: %v", err)
	}
	cmd, _ := startRecorderFixture(t, recorderFixtureIgnoringBody())
	pid := cmd.Process.Pid

	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatalf("read the recorder's group: %v", err)
	}
	if pgid == callerGroup {
		t.Errorf("the recorder (pid %d) is in the CALLER's process group %d — a group signal aimed at the caller's group takes the operator's terminal down (this is the measured parecord defect)", pid, pgid)
	}
	if pgid != pid {
		t.Errorf("the recorder's group is %d, want its own pid %d (Setpgid makes the child the leader of a fresh group)", pgid, pid)
	}
	if own := syscall.Getpid(); pgid == own {
		t.Errorf("the recorder's group is THIS process's pid %d — the policy did not isolate it", own)
	}
}

// TestStopVoiceRecorderForcesTheKillAndReapsARecorderThatIgnoresTheStopSignal is
// case (A): grace expiry means KILL AND WAIT, not "return an error and hope".
//
// The assertions are read from the PROCESS TABLE, because the defect was a
// process-table fact (`<defunct>`): the pid must be GONE, not merely signalled —
// kill(pid, 0) on a zombie SUCCEEDS, so a live-table reading is the only thing
// that separates "reaped" from "still ours".
func TestStopVoiceRecorderForcesTheKillAndReapsARecorderThatIgnoresTheStopSignal(t *testing.T) {
	cmd, stdout := startRecorderFixture(t, recorderFixtureIgnoringBody())
	pid := cmd.Process.Pid

	start := time.Now()
	err := stopVoiceRecorder(cmd, stdout)
	took := time.Since(start)

	// It must NOT return before the grace: the polite wait is what keeps a
	// recorder that only needs a moment from being killed mid-buffer.
	if took < voiceCaptureSignalGraceSec {
		t.Errorf("stopVoiceRecorder returned after %s, before the %s grace — a recorder that ignores the stop signal would be killed while it may still be draining", took, voiceCaptureSignalGraceSec)
	}
	// …and it must not run past the grace plus the force-kill window: the whole
	// point is a BOUNDED ending.
	if took > voiceCaptureSignalGraceSec+voiceCaptureForceKillGraceSec+recorderFixtureFastStopSec {
		t.Errorf("stopVoiceRecorder took %s — the forced stop is not bounded by grace %s + %s", took, voiceCaptureSignalGraceSec, voiceCaptureForceKillGraceSec)
	}
	// The failure is REPORTED (Rule 15), and it names the forced ending.
	if err == nil {
		t.Fatal("a recorder that ignored the stop signal produced no error — the forced stop must be visible to the caller")
	}
	if !strings.Contains(err.Error(), "was killed") {
		t.Errorf("error = %q, want it to name the forced kill", err)
	}
	// THE REAP: the pid is gone from the process table. A zombie would still
	// answer kill(pid, 0), so this is the assertion the defect fails.
	if !recorderWaitFor(func() bool { return syscall.Kill(pid, 0) == syscall.ESRCH }, recorderFixtureReapWait) {
		t.Fatalf("recorder pid %d is still in the process table after stopVoiceRecorder returned — it was not reaped (a zombie answers kill(pid,0), which is exactly the measured <defunct> state)", pid)
	}
	if _, statErr := os.Stat("/proc/" + strconv.Itoa(pid)); statErr == nil {
		t.Errorf("/proc/%d still exists after the forced stop — the child was not waited for", pid)
	}
	// The caller — in production, `gopi chat` — is untouched.
	if syscall.Kill(os.Getpid(), 0) != nil {
		t.Fatal("this process is gone: the stop took its own caller down")
	}
}

// TestStopVoiceRecorderReturnsNilWhenTheRecorderHonoursTheStopSignal keeps the
// NORMAL path honest: a recorder that exits on SIGINT is not reported as a
// forced kill, is not waited out to the grace, and leaves nothing behind.
//
// ITS VERDICT IS READ OFF THE PROCESS TABLE AND THE ERROR, NEVER OFF A CLOCK
// (measured flake, 2026-10-22). The first version also asserted
// `took < voiceCaptureSignalGraceSec` — a wall-clock upper bound on a path whose
// only obligation is "does not wait out the grace". On a loaded host the grace
// can expire between the signal and the child being SCHEDULED, the forced branch
// runs, and the case red-flags a stop that behaved correctly; the bound measured
// the machine, not the code. What the code must guarantee is the ORDER of its
// two endings — it must not ESCALATE to the forced kill — and that is visible in
// the error it returns (voiceCaptureStopKilledNote is the forced wording, the
// normal ending is the recorded `signal:` outcome). The order is also pinned
// from below by case (A), which asserts the same function DOES wait out the full
// grace for a recorder that ignores the signal: if the normal path had turned
// into the forced one, or the reverse, that pair disagrees.
func TestStopVoiceRecorderReturnsNilWhenTheRecorderHonoursTheStopSignal(t *testing.T) {
	cmd, stdout := startRecorderFixture(t, recorderFixturePoliteBody())
	pid := cmd.Process.Pid

	err := stopVoiceRecorder(cmd, stdout)

	// A recorder stopped BY the stop signal reports "signal: interrupt" — that is
	// the recorded outcome, not a failure, and voiceCaptureContextSensitiveError
	// classifies it. What must NOT happen is the forced-kill wording: that is the
	// escalation, and it is the ONE thing this case is about.
	if err != nil && strings.Contains(err.Error(), "was killed") {
		t.Errorf("error = %q: a polite recorder must not be reported as force-killed", err)
	}
	if err != nil && !strings.Contains(err.Error(), voiceCaptureKilledMarker) {
		t.Errorf("error = %q: a polite recorder's ending must be the recorded signal outcome (want %q), not a refusal", err, voiceCaptureKilledMarker)
	}
	// THE REAP, read from the process table: the pid is GONE. This is the same
	// criterion case (A) uses, and unlike a duration it is a fact about the
	// child rather than about the host that ran the case.
	if !recorderWaitFor(func() bool { return syscall.Kill(pid, 0) == syscall.ESRCH }, recorderFixtureReapWait) {
		t.Errorf("polite recorder pid %d is still in the process table — the normal path leaked a process", pid)
	}
}
