package testutil

// fixturelifetime_test.go — the proof that a FIXTURE child dies with its owning
// test binary, at the kernel level, with no cleanup hook running.
//
// EVERY PROCESS BELOW IS REAL. The defect is a fact about the kernel's
// descriptor teardown and about what /proc reports after a SIGKILL, so a
// simulated fixture could only assert its own assumptions. The shape is the
// measured incident:
//
//	holder (a re-exec'd test binary, playing `go test` being killed)
//	  └─ victim (a re-exec'd test binary, playing `blackhole.py` / signalrec.test)
//	the holder is SIGKILLed — no handler, no defer, no t.Cleanup can run —
//	and the victim must be gone.
//
// The RED PROOF is a mutation of the mechanism itself, not of this file: with
// `armed = daemonlifetime.WatchFromEnv()` replaced by `armed = true` in
// WatchFixtureBinding — i.e. the child reports a binding it never installed —
// the death assertion below FAILS with the victim still standing, which is the
// leaked shape. The mutation is reverted afterwards; the measured output is in
// the task report.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/daemonlifetime"
)

const (
	// fixtureHolderGuardEnv turns a re-exec'd test binary into the HOLDER: it
	// arms its own lifetime binding, starts the victim through the fixture
	// seam, and blocks — it never cleans up, because a SIGKILLed holder cannot.
	fixtureHolderGuardEnv = "GOPI_TEST_FIXTURE_HOLDER"
	// fixtureHolderReportEnv carries the path the HOLDER must hand its victim
	// as the victim's binding-verdict report.
	fixtureHolderReportEnv = "GOPI_TEST_FIXTURE_HOLDER_REPORT"
	// fixtureVictimGuardEnv turns a re-exec'd test binary into the VICTIM: it
	// binds itself to the inherited death pipe, states the verdict, and blocks.
	fixtureVictimGuardEnv = "GOPI_TEST_FIXTURE_VICTIM"
	// fixtureHolderVictimLogEnv names the file the HOLDER must give its victim as
	// stdout+stderr. A fixture child is KILLED by its own watchdog, on purpose,
	// and its last words are the only record of how it got there — on a pipe
	// nobody drains, those words are lost twice over. MEASURED 2026-09-26: with
	// the victim's stdout/stderr wired to an undrained pipe, the child died with
	// SIGPIPE (its own stderr line failing to land) BEFORE it reached the code
	// the case was about, and the failure read "the child never reached its
	// arming code" with no cause at all. A file survives the child.
	fixtureHolderVictimLogEnv = "GOPI_TEST_FIXTURE_HOLDER_VICTIM_LOG"
	// fixtureVictimGrandchildEnv carries the path the VICTIM writes the pid of
	// the grandchild it forks into — the process the group-scoped reaper must
	// reach without knowing about it.
	fixtureVictimGrandchildEnv = "GOPI_TEST_FIXTURE_VICTIM_GRANDCHILD"
	// fixtureVictimGrandchildBinary is the portable stand-in the victim forks.
	// It is a real, long-lived process that inherits the victim's group (no
	// Setpgid of its own), which is what makes it reaping evidence.
	fixtureVictimGrandchildBinary = "sleep"
	// fixtureVictimGrandchildSeconds outlives any single case: the grandchild
	// is always killed by the group-scoped reaper, never by expiring (a sleep
	// that ended mid-case would make a liveness assertion pass vacuously).
	fixtureVictimGrandchildSeconds = "300"
	// fixtureNoTestPattern is the -test.run pattern a fixture child is started
	// with: it matches no test, so a child that somehow reached m.Run would
	// execute nothing.
	fixtureNoTestPattern = "^$"
	// fixtureHolderReadyToken leads the holder's stderr line naming its victim,
	// so the parent can tell "the holder spoke" from unrelated output.
	fixtureHolderReadyToken = "fixture-holder-ready"
	// fixtureWaitBudget bounds every "it must be gone" wait. The mechanism is a
	// kernel HUP on a closed descriptor, so anything slow here is a real
	// failure, not scheduling.
	fixtureWaitBudget = 30 * time.Second
	// fixtureReadyBudget bounds the wait for the holder's readiness line: the
	// holder does no work beyond arming and spawning.
	fixtureReadyBudget = 30 * time.Second
	// fixturePollInterval is the liveness/report poll step.
	fixturePollInterval = 20 * time.Millisecond
	// fixtureForeverSleep is the blocking fixture's wait interval. It is a
	// timer loop and NOT `select {}`: with the watchdog goroutine as the only
	// other one, a bare select lets the runtime declare "all goroutines are
	// asleep - deadlock!" and exit — which would look exactly like the binding
	// working (the same trap internal/testutil/lifetimeprobe.go documents).
	fixtureForeverSleep = time.Hour
	// fixtureExitUnbound is the exit code of a fixture child whose binding did
	// not arm. Its report says armed=false as well, so the parent learns the
	// same fact twice — the code for a test that happens to run the child by
	// hand, the file for the automated guard.
	fixtureExitUnbound = 97
)

// --- the fixture children (run from this package's TestMain) ----------------

// runFixtureHolderChild is the holder child's entry point; returns true when
// THIS process is a holder, in which case TestMain must return without running
// the suite.
func runFixtureHolderChild() bool {
	if os.Getenv(fixtureHolderGuardEnv) == "" {
		return false
	}
	runFixtureHolder()
	return true
}

// runFixtureVictimChild is the victim child's entry point; returns true when
// THIS process is a victim.
func runFixtureVictimChild() bool {
	if os.Getenv(fixtureVictimGuardEnv) == "" {
		return false
	}
	runFixtureVictim()
	return true
}

// runFixtureHolder arms its OWN binding — it is the process whose death must
// take the victim down, so it must hold the write end itself — then starts the
// victim through the fixture seam and blocks.
//
// It deliberately inherits the SUITE's pipe in its environment (every child of
// the test binary does) and REPLACES it by arming: the point of the case is that
// killing the HOLDER kills the victim, and a victim bound to the suite's pipe
// would survive, because that write end is still held by the test binary.
func runFixtureHolder() {
	if err := daemonlifetime.ArmThisProcess(); err != nil {
		failFixture("holder: arm this holder's binding: " + err.Error())
	}
	report := os.Getenv(fixtureHolderReportEnv)
	if report == "" {
		failFixture("holder: " + fixtureHolderReportEnv + " is empty — the victim's binding verdict would have nowhere to land")
	}
	exe, err := os.Executable()
	if err != nil {
		failFixture("holder: locate this test binary: " + err.Error())
	}
	cmd := exec.Command(exe, "-test.run", fixtureNoTestPattern)
	cmd.Env = fixtureChildEnv(os.Environ(), fixtureVictimGuardEnv+"=1")
	victimOut, err := victimOutputTarget()
	if err != nil {
		failFixture("holder: open the victim's output target: " + err.Error())
	}
	cmd.Stdout = victimOut
	cmd.Stderr = victimOut
	// BindFixtureChild reads the HOLD variable this holder just armed, so the
	// victim is bound to THIS process and to no other.
	if err := BindFixtureChild(cmd, report); err != nil {
		failFixture("holder: bind the victim: " + err.Error())
	}
	if err := cmd.Start(); err != nil {
		failFixture("holder: start the victim: " + err.Error())
	}
	// The victim is NOT waited for: the holder is about to be SIGKILLed, and a
	// holder that reaped it would be cleaning up — the very thing this case
	// proves is not needed.
	fmt.Fprintf(os.Stderr, "testutil: %s pid %d victim %d\n", fixtureHolderReadyToken, os.Getpid(), cmd.Process.Pid)
	for {
		//stylegate:allow-sleep the fixture's own lifetime hold: this is a child process whose only job is to stay alive until its test binary is killed
		time.Sleep(fixtureForeverSleep)
	}
}

// runFixtureVictim binds itself to the inherited death pipe, states the verdict
// in the two channels a fixture child has, forks its grandchild when asked, and
// blocks until the watchdog kills it.
func runFixtureVictim() {
	err := WatchFixtureBinding()
	if err != nil {
		fmt.Fprintf(os.Stderr, "testutil: fixture victim: %v\n", err)
		// The report has already been written (unarmed), which is what the
		// parent's guard reads; exiting non-zero is the second, human-facing
		// statement of the same fact (Rule 15 — reported, never silent).
		os.Exit(fixtureExitUnbound)
	}
	spawnFixtureGrandchild()
	for {
		//stylegate:allow-sleep the fixture's own lifetime hold: this is a child process whose only job is to stay alive until its test binary is killed
		time.Sleep(fixtureForeverSleep)
	}
}

// spawnFixtureGrandchild forks the long-lived stand-in INHERITING the victim's
// process group (no Setpgid of its own) and records its pid. It is what makes
// the reaper's group scope observable: the reaper only ever knew the victim.
func spawnFixtureGrandchild() {
	path := os.Getenv(fixtureVictimGrandchildEnv)
	if path == "" {
		return
	}
	cmd := exec.Command(fixtureVictimGrandchildBinary, fixtureVictimGrandchildSeconds)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		failFixture("victim: start the grandchild: " + err.Error())
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), fixtureBindingReportMode); err != nil {
		failFixture("victim: record the grandchild pid: " + err.Error())
	}
}

// victimOutputTarget opens the file the victim's stdout+stderr write to, or
// returns this process's stderr when no file was requested (a fixture started by
// hand keeps printing to the terminal).
//
// The holder does not wait for the victim and is about to be SIGKILLed, so
// nothing will ever drain a pipe: a file is the only target whose contents are
// readable after both processes are gone.
func victimOutputTarget() (*os.File, error) {
	path := os.Getenv(fixtureHolderVictimLogEnv)
	if path == "" {
		return os.Stderr, nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fixtureBindingReportMode)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// failFixture reports a fixture child's fault and exits non-zero. A fixture
// that cannot set itself up must never look like one that did.
func failFixture(msg string) {
	fmt.Fprintf(os.Stderr, "testutil: %s\n", msg)
	os.Exit(fixtureExitUnbound)
}

// --- the parent-side cases --------------------------------------------------

// TestFixtureChildDiesWithItsHolderSIGKILLed is the incident's shape, end to
// end: a holder starts a fixture victim, the holder is SIGKILLed (no handler, no
// defer, no t.Cleanup — exactly what `go test -timeout` and `kill -9` do to a
// test binary), and the victim must be GONE.
//
// Nothing in the test acts on the victim: it is not signalled, not reaped, not
// named to any cleanup. The kernel closes the holder's write end, the victim's
// own watchdog (production code, in a real child) reads the HUP and SIGKILLs
// itself. That is the property the leak needed.
func TestFixtureChildDiesWithItsHolderSIGKILLed(t *testing.T) {
	RequireFixtureBinding(t)
	report := FixtureBindingReportPath(t, "victim-of-sigkilled-holder")
	removeFileIfPresent(t, report)
	victimLog := FixtureBindingReportPath(t, "victim-of-sigkilled-holder.log")
	removeFileIfPresent(t, victimLog)

	holder, holderPID, victimAnnounced := startFixtureHolder(t, report, victimLog)
	victimPID := awaitFixtureVerdict(t, report, victimAnnounced, victimLog)

	// The victim is ALIVE and NOT a zombie before the kill — signal 0 alone
	// accepts the corpse of a child that died early, and the case would then
	// prove nothing. /proc's own state letter is the instrument.
	id := awaitLiveFixture(t, victimPID)
	t.Logf("holder pid %d started victim pid %d, bound to the holder's death pipe", holderPID, victimPID)

	// SIGKILL the holder: no handler, no defer, no t.Cleanup, no cooperation.
	if err := syscall.Kill(holderPID, syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL the holder pid %d: %v", holderPID, err)
	}
	_ = holder.Wait() //stylegate:ignore reaping the holder we just killed so it cannot linger as a zombie; the verdict is the victim's death, not this exit status

	if !awaitIdentityGone(t, id, fixtureWaitBudget) {
		// Clean the leak up so a failing case does not itself leave the orphan
		// this epic is about; the assertion below is what reports the failure.
		_ = syscall.Kill(victimPID, syscall.SIGKILL) //stylegate:ignore best-effort cleanup of the leaked fixture child after the assertion already failed
		t.Fatalf("fixture victim pid %d SURVIVED its holder's SIGKILL — this is the leak: it would reparent to init and stand in the worktree", victimPID)
	}
	t.Logf("fixture victim pid %d died with its holder pid %d — the binding held under SIGKILL", victimPID, holderPID)
}

// TestFixtureChildIsReapedByItsOwnGroup covers the OTHER exit path: the test
// binary finishing normally (or failing, or panicking). The death pipe cannot
// help there — the process is still alive — so the fixture child is put in its
// own process group and the shared reaper signals exactly that group.
//
// The assertion that makes it evidence rather than a restatement: the victim
// forks a GRANDCHILD the test never learned of through the spawn API, and both
// must be gone after Reap. A pid-scoped reaper would leave the grandchild.
func TestFixtureChildIsReapedByItsOwnGroup(t *testing.T) {
	RequireFixtureBinding(t)
	report := FixtureBindingReportPath(t, "victim-group-scope")
	removeFileIfPresent(t, report)
	grandchildFile := FixtureBindingReportPath(t, "victim-group-scope-grandchild")
	removeFileIfPresent(t, grandchildFile)

	cmd := exec.Command(os.Args[0], "-test.run", fixtureNoTestPattern)
	cmd.Env = fixtureChildEnv(os.Environ(), fixtureVictimGuardEnv+"=1",
		fixtureVictimGrandchildEnv+"="+grandchildFile)
	mp, _ := StartFixtureChild(t, cmd, "victim-group-scope")
	victimPID := awaitFixtureVerdict(t, report, mp.Pid(), "")
	grandchildPID := awaitFixturePidFile(t, grandchildFile)
	awaitLiveFixture(t, victimPID)
	awaitLiveFixture(t, grandchildPID)

	// The child owns its group and the reaper is scoped to it: a group leader's
	// pgid IS its pid, and the grandchild inherits that group.
	if mp.Pgid() != mp.Pid() {
		t.Fatalf("the fixture child's group is %d, want its own pid %d — the reaper would signal someone else's group", mp.Pgid(), mp.Pid())
	}
	mp.Reap()

	for _, p := range []struct {
		what string
		pid  int
	}{{"fixture child", victimPID}, {"its grandchild", grandchildPID}} {
		id, err := ReadProcIdentity(p.pid)
		if err != nil {
			// No longer in /proc at all is the verdict we want.
			continue
		}
		if alive, reason := id.Alive(); alive {
			_ = syscall.Kill(p.pid, syscall.SIGKILL) //stylegate:ignore best-effort cleanup of the surviving fixture child after the assertion already failed
			t.Errorf("%s pid %d survived the group-scoped reap (%s) — an in-process exit path would leak it", p.what, p.pid, reason)
		}
	}
}

// TestBindFixtureChildRefusesWithoutAnArmedSuite pins the Rule 15 half: a spawn
// with no binding in force is REFUSED with a message naming the missing
// variable, never started unbound-but-looking-fine.
func TestBindFixtureChildRefusesWithoutAnArmedSuite(t *testing.T) {
	// The variable is unset for this case only, so the suite's own real binding
	// is untouched (t.Setenv restores it).
	t.Setenv(daemonlifetime.HoldEnvName, "")
	cmd := exec.Command(os.Args[0], "-test.run", fixtureNoTestPattern)
	cmd.Env = os.Environ()
	err := BindFixtureChild(cmd, filepath.Join(t.TempDir(), "report"))
	if err == nil {
		t.Fatal("BindFixtureChild accepted a spawn with no lifetime binding armed — a fixture child started here would outlive the suite")
	}
	if !strings.Contains(err.Error(), daemonlifetime.HoldEnvName) {
		t.Errorf("the refusal must name the missing variable (%s), got %q", daemonlifetime.HoldEnvName, err)
	}
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Setpgid {
		t.Error("the refusal must not have shaped the command it refused")
	}
}

// TestFixtureChildBindingVerdictRefusesAnUnarmedReport is the reader's own
// contract, exercised with the bytes a child really writes: an ARMED line
// yields the pid, and every shape that is not that — an unarmed verdict, an
// empty file, a line with no pid — is a REFUSAL.
//
// It exists because the parent's guard turns these into a failed case, and the
// failure must be attributable to the child's statement rather than to the
// reader's luck.
func TestFixtureChildBindingVerdictRefusesAnUnarmedReport(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantPID  int
		wantFail bool
	}{
		{"armed", fixtureBindingReportLine(true, 4242), 4242, false},
		{"unarmed", fixtureBindingReportLine(false, 4242), 0, true},
		{"empty", "", 0, true},
		{"no pid", fixtureBindingArmedWord + "\n", 0, true},
		{"pid zero", fixtureBindingArmedWord + " pid=0\n", 0, true},
	}
	for _, tc := range cases {
		pid, why := fixtureChildBindingVerdict([]byte(tc.raw))
		if tc.wantFail {
			if why == "" {
				t.Errorf("%s: verdict %q was accepted — a fixture child that did not bind itself must be refused", tc.name, tc.raw)
			}
			continue
		}
		if why != "" {
			t.Errorf("%s: verdict %q was refused (%s)", tc.name, tc.raw, why)
		}
		if pid != tc.wantPID {
			t.Errorf("%s: pid = %d, want %d", tc.name, pid, tc.wantPID)
		}
	}
}

// --- helpers ----------------------------------------------------------------

// startFixtureHolder spawns a holder child through the shared reaper (so a
// failing case cannot leave a holder standing either) and waits for its
// readiness line on stderr — proof the victim exists BEFORE any kill, so "it is
// gone" cannot be vacuous.
func startFixtureHolder(t *testing.T, report, victimLog string) (*MockProcess, int, int) {
	t.Helper()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("holder readiness pipe: %v", err)
	}
	cmd := exec.Command(os.Args[0], "-test.run", fixtureNoTestPattern)
	cmd.Env = fixtureChildEnv(os.Environ(),
		fixtureHolderGuardEnv+"=1",
		fixtureHolderReportEnv+"="+report,
		fixtureHolderVictimLogEnv+"="+victimLog,
	)
	cmd.Stderr = pw
	mp := StartMockProcessCmd(t, cmd)
	_ = pw.Close() //stylegate:ignore the child holds its own dup; the parent's copy would keep the readiness read open forever
	announced := awaitFixtureHolderReady(t, pr, mp.Pid())
	_ = pr.Close() //stylegate:ignore readiness arrived, the pipe is done with
	return mp, mp.Pid(), announced
}

// awaitFixtureHolderReady scans the holder's stderr for its readiness line,
// reporting an early death as a failure that names what the holder printed. The
// rest of the pipe is then drained into the test log: a fixture child's own
// diagnostics are the only channel that explains why a bound child failed to
// appear, and a test that swallows them reports "the child never reached its
// arming code" with no cause (Rule 15b).
func awaitFixtureHolderReady(t *testing.T, r *os.File, holderPID int) int {
	t.Helper()
	done := make(chan string, 1)
	drain := make(chan struct{})
	go func() {
		buf := make([]byte, fixtureBindingReportMax)
		var seen strings.Builder
		reported := false
		for {
			n, err := r.Read(buf)
			if n > 0 {
				if !reported {
					seen.Write(buf[:n])
					if strings.Contains(seen.String(), fixtureHolderReadyToken) {
						reported = true
						done <- seen.String()
					}
				} else {
					t.Logf("fixture holder pid %d: %s", holderPID, strings.TrimSpace(string(buf[:n])))
				}
			}
			if err != nil {
				if !reported {
					done <- seen.String()
				}
				close(drain)
				return
			}
		}
	}()
	select {
	case seen := <-done:
		if !strings.Contains(seen, fixtureHolderReadyToken) {
			<-drain
			t.Fatalf("holder pid %d never reported its victim (it said: %q)", holderPID, strings.TrimSpace(seen))
		}
		// The readiness line and the victim's OWN first words often arrive in
		// one chunk: a child that refuses to bind writes its complaint within
		// milliseconds of being started. Logging the whole buffer is what makes
		// such a refusal visible in the case's output instead of only in the
		// part of the stream the drain branch happens to see later.
		t.Logf("fixture holder pid %d said: %s", holderPID, strings.TrimSpace(seen))
		return victimPidFromHolderLine(seen)
	case <-time.After(fixtureReadyBudget):
		t.Fatalf("holder pid %d did not report its victim within %s", holderPID, fixtureReadyBudget)
	}
	return 0
}

// victimPidFromHolderLine extracts the victim pid the holder announced. It is a
// diagnostic input only: the assertions read the child's own verdict file, and
// this number is what lets a FAILURE name the child's state instead of reporting
// "nothing appeared" with no cause.
func victimPidFromHolderLine(line string) int {
	const field = " victim "
	i := strings.Index(line, field)
	if i < 0 {
		return 0
	}
	rest := line[i+len(field):]
	end := strings.IndexAny(rest, " \t\r\n")
	if end >= 0 {
		rest = rest[:end]
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// fixtureProcState reads the kernel's own status words for a pid from
// /proc/<pid>/stat: the state letter, the parent, and for a process that has
// exited the exit code the kernel recorded. A child that came and went between
// two reads of this file is otherwise only visible as "it is not there", which
// names no cause — and the exit code is what distinguishes "it refused" from
// "it was killed" from "it never started" (Rule 15b: report, never swallow).
//
// The field numbers are the kernel's: field 3 is the state letter, field 4 the
// parent pid, field 52 (1-based, from proc(5)) the exit code of a reaped-but-
// unreaped-by-its-parent process. Only the tail fields are read, and a stat that
// does not have them reports what it can.
func fixtureProcState(base string) string {
	raw, err := os.ReadFile(filepath.Join(base, procStatName))
	if err != nil {
		return "(no /proc entry)"
	}
	text := strings.TrimSpace(string(raw))
	close := strings.LastIndex(text, ")")
	if close < 0 || close+1 >= len(text) {
		return "(unparsable stat)"
	}
	fields := strings.Fields(text[close+1:])
	if len(fields) < 2 {
		return "(unparsable stat)"
	}
	state, ppid := fields[0], fields[1]
	out := "state=" + state + " ppid=" + ppid
	if idx := fixtureProcStatExitCodeField - fixtureProcStatFirstTailField; idx >= 0 && idx < len(fields) {
		out += " exit_code=" + fields[idx]
	}
	return out
}

// fixtureChildState renders what /proc says about a fixture child that failed to
// report — its cmdline and the fixture variables it actually carries. A child
// that re-entered the wrong body, or inherited the wrong guard, is visible here
// and nowhere else; without it the failure is a silent "nothing happened" (Rule
// 15b).
func fixtureChildState(pid int) string {
	if pid <= 0 {
		return "the holder announced no victim pid"
	}
	base := filepath.Join(procDir, strconv.Itoa(pid))
	cmdline, cmdErr := os.ReadFile(filepath.Join(base, "cmdline"))
	rawEnv, envErr := os.ReadFile(filepath.Join(base, "environ"))
	var b strings.Builder
	fmt.Fprintf(&b, "pid %d %s cmdline=%q", pid, fixtureProcState(base), strings.ReplaceAll(string(cmdline), "\x00", " "))
	if cmdErr != nil {
		fmt.Fprintf(&b, " (cmdline unreadable: %v)", cmdErr)
	}
	if envErr != nil {
		fmt.Fprintf(&b, "; environ unreadable: %v", envErr)
		return b.String()
	}
	b.WriteString("; fixture env:")
	for _, entry := range strings.Split(string(rawEnv), "\x00") {
		if strings.Contains(entry, "FIXTURE") {
			fmt.Fprintf(&b, " %s", entry)
		}
	}
	return b.String()
}

// awaitFixtureVerdict waits for a fixture child's binding verdict and returns
// the pid it named, failing the case when the child never reported one (the
// reader's refusal is the assertion; this is only the wait).
func awaitFixtureVerdict(t *testing.T, report string, victimAnnounced int, victimLog string) int {
	t.Helper()
	deadline := time.Now().Add(fixtureReadyBudget)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(report)
		if err == nil && len(raw) > 0 {
			pid, why := fixtureChildBindingVerdict(raw)
			if why != "" {
				t.Fatalf("the fixture child did not bind itself: %s (its verdict: %q)", why, strings.TrimSpace(string(raw)))
			}
			return pid
		}
		//stylegate:allow-sleep bounded poll interval: the loop's own deadline owns the outcome and exits on the first matching observation
		time.Sleep(fixturePollInterval)
	}
	t.Fatalf("no fixture binding verdict appeared at %s within %s — the child never reached its arming code (%s) — its own output: %q",
		report, fixtureReadyBudget, fixtureChildState(victimAnnounced), readFixtureLog(victimLog))
	return 0
}

// awaitFixturePidFile waits for a pid a fixture child recorded and returns it.
func awaitFixturePidFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(fixtureReadyBudget)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			if n, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil && n > 0 {
				return n
			}
		}
		//stylegate:allow-sleep bounded poll interval: the loop's own deadline owns the outcome and exits on the first matching observation
		time.Sleep(fixturePollInterval)
	}
	t.Fatalf("no pid appeared in %s within %s", path, fixtureReadyBudget)
	return 0
}

// awaitLiveFixture waits until the pid is a live, non-zombie process and
// returns its identity — the precondition every death assertion in this file
// needs (a corpse would make the assertion vacuous).
func awaitLiveFixture(t *testing.T, pid int) ProcIdentity {
	t.Helper()
	deadline := time.Now().Add(fixtureReadyBudget)
	for time.Now().Before(deadline) {
		if id, err := ReadProcIdentity(pid); err == nil {
			if alive, _ := id.Alive(); alive {
				return id
			}
		}
		//stylegate:allow-sleep bounded poll interval: the loop's own deadline owns the outcome and exits on the first matching observation
		time.Sleep(fixturePollInterval)
	}
	t.Fatalf("fixture pid %d never became a live process", pid)
	return ProcIdentity{}
}

// no local awaitIdentityGone: daemonlifetime_test.go in this same package
// already owns the identity-aware wait (it takes a ProcIdentity so a recycled
// pid cannot read as a survivor, and falls back to reading one when the process
// may already be gone). The fixture cases here call THAT one — one definition of
// "the process is gone" in this package (Rules 13/14).

// fixtureChildEnv builds a fixture child's environment from a clean slate:
// every fixture GUARD variable this package uses is dropped, then the overrides
// are applied. This is not cosmetic — Go's os.Getenv returns the FIRST match in
// the environment block, so a blind append leaves the PARENT's value winning, and
// here the parent's value is the parent's own guard. MEASURED 2026-09-26: a
// victim started with `append(os.Environ(), victimGuard+"=1")` inherited the
// HOLDER's guard first, re-entered the holder path and started a victim of its
// own — the fixture recursed (each generation arming and spawning the next) until
// the case's own timeout, and no process ever reached the child body the case was
// about. The same trap is documented in internal/signalrec/srChildEnvFor.
func fixtureChildEnv(env []string, overrides ...string) []string {
	// The guards are CLEARED first (they are the routing variables a child must
	// never inherit from a parent playing a different role), then the overrides
	// are applied — both through the ONE environment assembler.
	cleared := make([]string, 0, len(fixtureGuardEnvNames()))
	for _, name := range fixtureGuardEnvNames() {
		cleared = append(cleared, name+"=")
	}
	return setFixtureEnv(setFixtureEnv(env, cleared...), overrides...)
}

// fixtureGuardEnvNames lists EVERY variable that routes a re-exec'd test binary
// into a fixture child body. One definition, used by the spawner that must drop
// them — a guard added to the set below and forgotten here is a recursion.
func fixtureGuardEnvNames() []string {
	return []string{
		fixtureHolderGuardEnv,
		fixtureHolderReportEnv,
		fixtureVictimGuardEnv,
		fixtureVictimGrandchildEnv,
	}
}

// readFixtureLog returns a fixture child's captured output for a failure
// message, trimmed and bounded. An empty path or an unreadable file yields a
// statement of that fact rather than a silent empty string (Rule 15b): "the
// child said nothing" and "we could not look" are different findings.
func readFixtureLog(path string) string {
	if path == "" {
		return "(no log was requested for this child)"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("(log %s unreadable: %v)", path, err)
	}
	text := strings.TrimSpace(string(raw))
	if len(text) > fixtureBindingReportMax {
		text = text[:fixtureBindingReportMax]
	}
	if text == "" {
		return "(the child wrote nothing to " + path + ")"
	}
	return text
}

// removeFileIfPresent clears a fixture artifact before a case, so a case can
// never be satisfied by an earlier case's leftover report.
func removeFileIfPresent(t *testing.T, path string) {
	t.Helper()
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("clear the stale fixture artifact %s: %v", path, err)
	}
}
