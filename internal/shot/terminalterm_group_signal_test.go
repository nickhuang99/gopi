package shot

// terminalterm_group_signal_test.go — the regression guard for Rule 42 on the
// filmed terminal's TEARDOWN.
//
// THE DEFECT THIS PINS (measured shape, not a hypothesis): StartFilmTerminalSpec
// used to set only Pdeathsig, so the terminal inherited THIS RUN's process group,
// while Close addressed the terminal's pid as a GROUP ("kill(-t.PID, SIGTERM)").
// A pid is not a group id, and it is not even stable: the group under that number
// is either absent (ESRCH, which the old code tolerated) or — after the kernel
// recycled the pid — somebody else's, whose every member would have received the
// signal. Rule 42's criterion is that a signal's recipient must be a process or
// group the caller CREATED and STILL HOLDS; "the number was ours a moment ago"
// is not that.
//
// Each arm is aimed at a fixture THIS TEST spawned, so no arm can reach a
// stranger — and every arm measures BOTH directions: a refusal must leave the
// process ALIVE (read back from the kernel, not inferred from an error), and the
// accepted delivery must leave the whole group GONE.

import (
	"bufio"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	// groupFixtureSleepSeconds keeps a fixture alive long past the test.
	groupFixtureSleepSeconds = "300"
	// groupSignalPollStep / groupSignalPollBound are the bounded wait for a
	// signalled process to disappear — a poll, never a fixed sleep, because the
	// claim is "it is gone" and a sleep would only ever prove "it had time".
	groupSignalPollStep  = 20 * time.Millisecond
	groupSignalPollBound = 5 * time.Second
	// The substrings each refusal must carry, so an arm fails on the REASON and
	// not merely on "some error came back".
	groupRefusalNotLeader  = "not the leader of its own group"
	groupRefusalIncarnated = "incarnation changed"
	groupRefusalNoIdentity = "no incarnation"
	// groupFixtureShellPID is the shell line that prints its background child's
	// pid and waits: the printed pid is a member of the group WITHOUT being this
	// test's child, which is how "the signal reached the GROUP" is measurable.
	groupFixtureShellPID = "sleep " + groupFixtureSleepSeconds + " & echo $!; wait"
)

// startGroupFixture spawns a process this test owns. lead=true puts it in its
// own new group (Setpgid — the shape the fixed spawner uses, pgid == pid);
// lead=false leaves it in THIS test's group, which is exactly the state the
// unfixed spawner left a filmed terminal in.
func startGroupFixture(t *testing.T, lead bool) (*exec.Cmd, int) {
	t.Helper()
	cmd := exec.Command("/bin/sleep", groupFixtureSleepSeconds)
	// Pdeathsig rides the leader it already has: the process under test IS a
	// direct child (no fork), so the kernel's parent-death signal reaches it and
	// a SIGKILLed test binary leaves no fixture behind. The cwd is not this
	// fixture's subject (the GROUP is), so it is parked outside the checkout.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: lead, Pdeathsig: syscall.SIGKILL}
	cmd.Dir = t.TempDir()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the fixture: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(pid, syscall.SIGKILL) //stylegate:ignore teardown of the fixture THIS test spawned; its exit status is never the assertion
		_ = cmd.Wait()                         //stylegate:ignore reaps the fixture killed on the line above, so the test leaves no zombie
	})
	return cmd, pid
}

// groupOf reads the live group of pid, so a precondition is MEASURED rather than
// assumed by the fixture's construction.
func groupOf(t *testing.T, pid int) int {
	t.Helper()
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatalf("read the process group of pid %d: %v", pid, err)
	}
	return pgid
}

// alreadyReaped tells Close that this terminal's exit was observed elsewhere, so
// Close does not go on to reap it. Every REFUSAL arm needs this: after refusing,
// Close still reaps, and the UNFIXED code (which refuses nothing) would then take
// a blocking Wait4 on a fixture that is deliberately still alive — the arm would
// HANG instead of failing by assertion. What these arms measure is the signal
// decision, and the reap is not part of it.
func alreadyReaped(term *FilmTerminal) *FilmTerminal {
	term.reaped = true
	return term
}

// noGroupUnder reports whether the number names NO existing process group, so an
// arm whose whole point is that the guard is what stops the signal can never
// reach a stranger even when the guard is removed (Rule 42: a red proof is no
// licence to signal what this test does not hold).
func noGroupUnder(pid int) bool {
	return syscall.Kill(-pid, 0) == syscall.ESRCH
}

// alive reports whether the kernel still lists pid at all.
func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// goneWithin polls (bounded) until every given pid has disappeared, and reports
// the ones that did not.
func goneWithin(pids ...int) []int {
	deadline := time.Now().Add(groupSignalPollBound)
	for {
		var survivors []int
		for _, pid := range pids {
			if alive(pid) {
				survivors = append(survivors, pid)
			}
		}
		if len(survivors) == 0 || time.Now().After(deadline) {
			return survivors
		}
		time.Sleep(groupSignalPollStep) //stylegate:allow-sleep this is the poll CADENCE of the bounded loop above; there is no event to wait on (/proc has no notification), and the loop fails BY NAME at groupSignalPollBound rather than racing it
	}
}

// TestFilmTerminalCloseRefusesAGroupItDoesNotLead is the RED path the defect
// shipped: the terminal is NOT the leader of its group (it inherited this run's),
// so "-pid" addresses a group this run never created. Close must refuse, say why,
// and leave the process measurably alive.
func TestFilmTerminalCloseRefusesAGroupItDoesNotLead(t *testing.T) {
	cmd, pid := startGroupFixture(t, false)
	if pgid := groupOf(t, pid); pgid == pid {
		t.Fatalf("the fixture unexpectedly leads its own group (pgid %d == pid %d) — the arm cannot test the inherited-group shape", pgid, pid)
	}
	if !noGroupUnder(pid) {
		t.Skipf("a process group with id %d exists — the arm's precondition (no group under that number) does not hold, so an unguarded signal here could reach a group this test does not hold", pid)
	}
	term := alreadyReaped(&FilmTerminal{PID: pid, StartTicks: displayStartTicks(pid), cmd: cmd})

	err := term.Close()
	if err == nil {
		t.Fatalf("Close signalled a group it does not lead (pid %d) without refusing — Rule 42: a group signal must reach a group this run created", pid)
	}
	if !strings.Contains(err.Error(), groupRefusalNotLeader) {
		t.Fatalf("the refusal does not name the criterion (want %q): %v", groupRefusalNotLeader, err)
	}
	if term.groupSignalled {
		t.Fatalf("the group signal was recorded as DELIVERED while Close also returned a refusal: %v", err)
	}
	if !alive(pid) {
		t.Fatalf("pid %d is gone after a REFUSED close — the refusal must not signal anything", pid)
	}
}

// TestFilmTerminalCloseRefusesARecycledPid is the dangerous half: the record's
// pid still names a process, and that process still leads a group of its own —
// but it is a DIFFERENT process (a different incarnation), so the group under
// that number is a stranger's. Rule 42 forbids delivering there.
func TestFilmTerminalCloseRefusesARecycledPid(t *testing.T) {
	cmd, pid := startGroupFixture(t, true)
	ticks := displayStartTicks(pid)
	if ticks == "" {
		t.Skipf("the host does not expose /proc/%d/stat starttime — the incarnation check has nothing to compare", pid)
	}
	if pgid := groupOf(t, pid); pgid != pid {
		t.Fatalf("the fixture does not lead its own group (pgid %d, pid %d) — this arm needs the leadership invariant to HOLD so only the incarnation can refuse", pgid, pid)
	}
	stale, err := strconv.Atoi(ticks)
	if err != nil {
		t.Fatalf("starttime %q is not a number: %v", ticks, err)
	}
	term := alreadyReaped(&FilmTerminal{PID: pid, StartTicks: strconv.Itoa(stale + 1), cmd: cmd})

	cerr := term.Close()
	if cerr == nil {
		t.Fatalf("Close signalled the group of a pid whose incarnation had changed (pid %d) — a recycled pid must never be signalled", pid)
	}
	if !strings.Contains(cerr.Error(), groupRefusalIncarnated) {
		t.Fatalf("the refusal does not name the incarnation (want %q): %v", groupRefusalIncarnated, cerr)
	}
	if term.groupSignalled {
		t.Fatalf("the group signal was recorded as DELIVERED while Close returned a refusal: %v", cerr)
	}
	if !alive(pid) {
		t.Fatalf("pid %d is gone after a REFUSED close — the refusal must not signal anything", pid)
	}
}

// TestFilmTerminalCloseRefusesAMissingIncarnation pins the third refusal: an
// EMPTY starttime is not identity, and "no record" must not degrade into "signal
// the bare number".
func TestFilmTerminalCloseRefusesAMissingIncarnation(t *testing.T) {
	cmd, pid := startGroupFixture(t, true)
	term := alreadyReaped(&FilmTerminal{PID: pid, cmd: cmd})

	err := term.Close()
	if err == nil {
		t.Fatalf("Close signalled pid %d with no recorded incarnation — an empty identity is not usable identity (Rule 42)", pid)
	}
	if !strings.Contains(err.Error(), groupRefusalNoIdentity) {
		t.Fatalf("the refusal does not name the missing identity (want %q): %v", groupRefusalNoIdentity, err)
	}
	if !alive(pid) {
		t.Fatalf("pid %d is gone after a REFUSED close", pid)
	}
}

// TestFilmTerminalCloseSignalsOnlyTheGroupItCreated is the POSITIVE control, and
// it proves the other direction of the same sentence: when the terminal IS the
// leader of a group this run created, the group signal is delivered and reaches
// the group's members — including one that is not this process's own child.
func TestFilmTerminalCloseSignalsOnlyTheGroupItCreated(t *testing.T) {
	// The fixture MUST be a shell that forks a background `sleep` and prints its
	// pid: that printed pid is a member of the group WITHOUT being this test's
	// child, which is the only way "the -pgid signal reached the GROUP" is
	// measurable. StartMockProcessCmd cannot be used — it would put the child in
	// a group of its own choosing and reap it, replacing the very group this arm
	// delivers a signal to. The cleanup below is the lifetime arm for the forked
	// `sleep` (a Pdeathsig is not inherited, so it cannot reach it).
	//fixture-spawn-ok: group shape is the subject; see the comment above
	cmd := exec.Command("/bin/sh", "-c", groupFixtureShellPID)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("pipe the fixture's stdout: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the fixture: %v", err)
	}
	pid := cmd.Process.Pid
	leader := false
	t.Cleanup(func() {
		if !leader {
			_ = syscall.Kill(-pid, syscall.SIGKILL) //stylegate:ignore best-effort group teardown of the group THIS test created with Setpgid; the verdict was read from the kernel already
		}
		_ = cmd.Wait() //stylegate:ignore reaps the fixture's shell, which the group signal above killed; the exit status is not what the arm asserts
	})
	line, rerr := bufio.NewReader(out).ReadString('\n')
	if rerr != nil {
		t.Fatalf("read the fixture's background pid: %v", rerr)
	}
	member, aerr := strconv.Atoi(strings.TrimSpace(line))
	if aerr != nil {
		t.Fatalf("the fixture printed %q instead of a background pid: %v", strings.TrimSpace(line), aerr)
	}
	if pgid := groupOf(t, pid); pgid != pid {
		t.Fatalf("the fixture does not lead its own group (pgid %d, pid %d)", pgid, pid)
	}
	if groupOf(t, member) != pid {
		t.Fatalf("the background pid %d is not in the fixture's group %d — the arm cannot prove the signal reached the GROUP", member, pid)
	}
	leader = true

	term := &FilmTerminal{PID: pid, StartTicks: displayStartTicks(pid), cmd: cmd}
	if cerr := term.Close(); cerr != nil {
		t.Fatalf("Close refused a group this run created and still holds (pid %d, group %d): %v", pid, pid, cerr)
	}
	if !term.groupSignalled {
		t.Fatalf("Close stopped the terminal without recording a delivered group signal (pid %d)", pid)
	}
	if survivors := goneWithin(pid, member); len(survivors) != 0 {
		t.Fatalf("the group signal did not reach %v — the leader and the command it started are what -pgid is for", survivors)
	}
}
