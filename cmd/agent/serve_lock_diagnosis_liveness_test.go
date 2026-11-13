// serve_lock_diagnosis_liveness_test.go — the refusal DIAGNOSTIC must name the
// stray holder AND its LIVENESS, and read the kernel's lock table on the inode
// the acquirer was ACTUALLY refused on.
//
// THE DEFECT (epic serve-lock-diagnosis-liveness). A refusal read
// "another merge process is already running (lock ~/.gopi/serve.lock held by pid
// N)" while /proc/locks carried NO record for the lock file's inode and pid N
// was dead. There are two independent reasons a pid read from any lock view is
// not liveness evidence:
//
//	(a) a flock lives on the INODE — a REPLACED lock file (unlink + recreate)
//	    leaves a holder locking an inode the PATH no longer names, so a table
//	    join on the path's inode reports "no records" for a lock that is very
//	    much held, and the pid stamped in the file belongs to nobody;
//	(b) a flocked descriptor DUPLICATED into another process keeps the flock
//	    alive after its OWNER DIES — the kernel's own table then names a
//	    CORPSE while a live stranger holds the lock.
//
// WHAT TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness DRIVES
// (the production refusal path — acquireServeLock, the function every
// `gopi merger` start goes through — with a REPLACED lock file):
//
//	stage 1  a helper takes the flock on the lock path and leaks the descriptor
//	         to a child through os/exec ExtraFiles (the inheritance mechanism
//	         the daemon face uses); the path is then REPLACED, so that holder
//	         keeps the flock on an inode no name points at — a STRAY holder,
//	         still ALIVE;
//	stage 2  a second helper takes the flock on the FRESH (replaced) inode,
//	         leaks it the same way and DIES — so the inode the acquirer will be
//	         refused on carries a kernel record naming a DEAD pid;
//	stage 3  acquireServeLock is called and REFUSED (EWOULDBLOCK, the only
//	         errno that licenses the live-daemon diagnosis), and the refusal
//	         must carry `lock-stray-holder:` with the stray holder's pid and
//	         liveness, and `lock-kernel-records:` for the inode the acquired
//	         descriptor was refused on.
//
// The rig asserts its own premises (the kernel really records the flock; the
// path's inode really changed; the leaked holder really outlived its owner), so
// a host where the mechanism behaves differently reports it UNPROVEN instead of
// passing on an empty rig.
//
// Zero load, no thresholds, no timing races (Rule 27): every process here is a
// child of this test or is killed by its explicit pid, and every wait is a
// bounded poll on a condition the rig itself creates.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// The diagnosis rig's names (Rule 1b: env names, modes and the pid separator
// are spelled once here and referenced by the helper body below).
const (
	// diagnosisModeEnv / diagnosisPathEnv / diagnosisMarkerEnv /
	// diagnosisDieEnv drive the helper roles (the re-exec target no-ops under
	// an ordinary `go test` run: the mode is absent).
	diagnosisModeEnv   = "GOPI_TEST_SERVE_DIAGNOSIS_MODE"
	diagnosisPathEnv   = "GOPI_TEST_SERVE_DIAGNOSIS_LOCK"
	diagnosisMarkerEnv = "GOPI_TEST_SERVE_DIAGNOSIS_MARKER"
	diagnosisDieEnv    = "GOPI_TEST_SERVE_DIAGNOSIS_DIE"

	// diagnosisModeHold is the owner half: flock the path, leak the SAME
	// descriptor to a second process (ExtraFiles), publish both pids.
	diagnosisModeHold = "hold"
	// diagnosisModeInherit is the leaked half: it owns nothing but the
	// descriptor it inherited on fd 3, so it must simply STAY ALIVE — its
	// existence is what keeps the flock held after the owner dies.
	diagnosisModeInherit = "inherit"
	// diagnosisModeExit is the "die at once" helper the liveness-wording test
	// spawns to obtain a genuinely dead pid.
	diagnosisModeExit = "exit"

	// diagnosisModeFail is the Rule 5 REGRESSION body: the helper takes the
	// flock and then dies, in the CHILD, BEFORE it publishes its pids — with
	// its own words on stderr and its own exit status. It is the fixture that
	// makes "the child DIED" observable, as opposed to "the child was SLOW".
	diagnosisModeFail = "fail"

	// diagnosisFailStderrEnv carries the line the fail body prints, and
	// diagnosisFailExitCode is the code it exits with — chosen non-zero and
	// distinctive so the parent's failure text can be asserted to carry the
	// CHILD's status rather than a generic one.
	diagnosisFailStderrEnv = "GOPI_TEST_SERVE_DIAGNOSIS_FAIL_STDERR"
	diagnosisFailExitCode  = 66

	// diagnosisHelperFaultExitCode is the helper's own exit for a fault in its
	// body (a lock path it could not open, a marker it could not write). It is
	// reported as a normal child status to the parent, never as a panic: the
	// helper process never reaches m.Run, so there is no *testing.T to Fatalf.
	diagnosisHelperFaultExitCode = 67
	// diagnosisHelperUnknownModeExitCode is the helper's exit for an unrecognised
	// mode — the dispatch is a fixed table, so this means a caller passed a value
	// the rig does not define.
	diagnosisHelperUnknownModeExitCode = 68

	// diagnosisDieYes is the die-env value that makes the OWNER exit right
	// after it has leaked the descriptor.
	diagnosisDieYes = "1"

	// diagnosisPidSeparator separates the two pids the owner publishes
	// ("<owner> <leak>") — shared by the writer and the parser so the two
	// cannot drift (Rule 1b).
	diagnosisPidSeparator = " "

	// diagnosisHoldSec bounds a helper holder's life. It is a backstop only:
	// the test kills every holder by pid before it expires.
	diagnosisHoldSec = 60
)

// diagnosisPollTick is the POLL PERIOD of every wait in this file: it bounds how
// long after a condition becomes true the parent notices it, never how long it is
// willing to wait (that is a budget — diagnosisReadyBudget for the helper's
// readiness marker, diagnosisPollWindow for the rig's own conditions).
const diagnosisPollTick = 25 * time.Millisecond

// The readiness window of the re-exec'd helper is DERIVED, never typed in.
//
// The helper is THIS test binary re-exec'd, so before its body runs it pays the
// suite's whole TestMain startup (testutil's pins plus the close-out sweeps) —
// `agent.test -test.run='^$'` with no test selected. That cost was MEASURED at
// 5.02s on the host where the sibling fixture of this same suite is known (the
// report of commit 99e2eacac) and at 0.77-0.93s on this host, 2026-10-12 (see
// diagnosisMeasuredSuiteStartupFloor, which carries the worst of the two). The
// window this replaces was a FIXED diagnosisPollTicks × diagnosisPollTick =
// 100 × 25ms = 2.50s — BELOW the measured floor, so the case raced the suite's
// startup and lost 2 of 3 runs under load. The window is therefore derived from
// the MEASURED floor and the run's own deadline; see diagnosisReadyBudget.
const (
	// diagnosisReadyFloorMultiple is what the window is allowed to be, as a
	// multiple of the worst measured fixture floor. That floor is 5.02s, so the
	// derived floor cannot sit below it: 8 × 5.02s ≈ 40s, i.e. a start would
	// have to take 8× the measured cost before this window is the thing that
	// failed.
	diagnosisReadyFloorMultiple = 8

	// diagnosisReadySampleMultiple scales the window by a startup a PREVIOUS
	// handshake in the same run MEASURED, so a loaded machine widens the window
	// instead of turning the case red.
	diagnosisReadySampleMultiple = 3

	// diagnosisReadyDeadlineReserve is held back from the run's own deadline:
	// THIS handshake must be what fails (naming the child's state), never Go's
	// -test.timeout panicking over the whole binary.
	diagnosisReadyDeadlineReserve = 15 * time.Second

	// diagnosisReadyDefaultDeadline mirrors testing's own default -test.timeout
	// (10m). It is read only when the flag cannot be (Rule 15a: no silent zero).
	diagnosisReadyDefaultDeadline = 10 * time.Minute

	// diagnosisMeasuredSuiteStartupFloor is the worst fixture floor MEASURED for
	// this suite: how long `agent.test -test.run='^$'` — this binary with NO test
	// selected — needs before it exits, because the helper pays the suite's whole
	// TestMain (testutil's pins + close-out sweeps) before its body runs.
	//
	// MEASURED at 5.02s for the sibling fixture of this same suite
	// (cmd/agent/cli_serve_test.go's holder, commit 99e2eacac's report).
	// MEASURED AGAIN on this host 2026-10-12 (nick-sager, 8 cores, load 8-12):
	// `-test.run='^$'` 0.77-0.93s (n=5) and the diagnosis helper reached its body
	// 0.78-1.05s after spawn (n=15, median 0.863s) — this host is simply faster.
	// The constant therefore carries the WORST measurement, not this host's: a
	// window has to cover the slowest host this suite runs on, and a floor
	// derived from the fastest one would be the same defect with a new number.
	diagnosisMeasuredSuiteStartupFloor = 5020 * time.Millisecond

	// diagnosisReadyFloor is the FLOOR of the readiness window: what this
	// helper costs before its own body can run, with room to spare. It is
	// diagnosisReadyFloorMultiple × the worst MEASURED fixture floor above, so
	// the window can never be typed below the cost it waits for. No window in
	// this file may be smaller: the pre-fix 100 × 25ms = 2.50s was, and that is
	// exactly why the case failed 2 of 3 runs under load.
	diagnosisReadyFloor = diagnosisReadyFloorMultiple * diagnosisMeasuredSuiteStartupFloor

	// diagnosisPollWindow bounds every wait in this file that polls a condition
	// the RIG itself produces (a helper's death, the kernel's lock row, the
	// rig's own inode replacement). These are not startup costs — they are
	// kernel/file-system facts the rig has just asked for — so they keep a
	// modest fixed bound, deliberately NOT below the measured fixture floor
	// either, so no window in this file is typed under the cost it is nearest to.
	diagnosisPollWindow = 2 * diagnosisMeasuredSuiteStartupFloor

	// diagnosisStderrCap bounds what the parent holds of the child's stderr: a
	// fixture that spews must not turn a verdict read into an unbounded read.
	diagnosisStderrCap = 8 << 10

	// diagnosisDrainGrace is how long the parent waits for a killed child's
	// last words and exit status before it stops draining.
	diagnosisDrainGrace = 2 * time.Second
)

// diagnosisProcessStart is the fact diagnosisFloorNow is measured from, and it
// is taken by a PACKAGE-LEVEL initializer: Go runs those before main (and so
// before TestMain, before the suite's pins and before m.Run), which makes this
// the binary's own startup clock — the same quantity the helper pays when it is
// re-exec'd. Taking it here rather than editing TestMain keeps this fix inside
// the file it repairs (no collateral line shifts in a shared file).
var diagnosisProcessStart = time.Now()

// diagnosisStartupOnce/diagnosisStartupValue freeze the first reading above, so
// the value is this run's startup COST rather than the run's elapsed time: a
// later test must not see a "startup" that has grown to five minutes.
var (
	diagnosisStartupOnce  sync.Once
	diagnosisStartupValue time.Duration
)

// diagnosisSuiteStartup is this run's measured startup cost for a process of
// this very fixture: how long this binary needed before the suite asked for it
// (≈ the helper's own cost before its body runs). It is the run's OWN
// measurement, not a guess about the host, and a run that somehow measured
// nothing returns 0 — which diagnosisFloorNow answers with the named floor
// rather than a silent zero (Rule 15a).
func diagnosisSuiteStartup() time.Duration {
	diagnosisStartupOnce.Do(func() { diagnosisStartupValue = time.Since(diagnosisProcessStart) })
	return diagnosisStartupValue
}

// diagnosisDeadlineOf is the wall-clock budget the WHOLE run has: Go's own
// -test.timeout, which a run may set. Reading it (rather than assuming the
// default) is what ties the window to the run's configuration.
func diagnosisDeadlineOf() time.Duration {
	f := flag.Lookup("test.timeout")
	if f == nil {
		return diagnosisReadyDefaultDeadline
	}
	d, err := time.ParseDuration(f.Value.String())
	if err != nil || d <= 0 {
		return diagnosisReadyDefaultDeadline
	}
	return d
}

// diagnosisReadyBudget is the readiness window itself, as a PURE function so it
// is testable on its own (Rule 5): how long a helper that was started at
// startedAt may still wait for its readiness marker, given the whole run's
// deadline, this run's own measured suite startup, and a startup sample an
// earlier handshake MEASURED (0 = none).
//
// It is a floor (the caller's `floor`, which diagnosisFloorNow derives from the
// worst measured fixture floor), raised by diagnosisReadySampleMultiple × the
// measured sample once a sample exists — so the window tracks THIS machine — and
// always capped by what is left of the deadline minus
// diagnosisReadyDeadlineReserve, so this handshake fails naming the child instead
// of panicking with the binary on Go's -test.timeout.
func diagnosisReadyBudget(deadline time.Duration, elapsed, measuredStartup time.Duration, floor time.Duration) time.Duration {
	budget := floor
	if measuredStartup > 0 {
		if scaled := measuredStartup * diagnosisReadySampleMultiple; scaled > budget {
			budget = scaled
		}
	}
	if remaining := deadline - elapsed - diagnosisReadyDeadlineReserve; remaining < budget {
		budget = remaining
	}
	if budget < diagnosisPollTick {
		return diagnosisPollTick
	}
	return budget
}

// diagnosisFloorNow is the floor of the readiness window for THIS run: the
// measured fixture floor (diagnosisReadyFloor), which is NEVER undercut, grown
// by this run's own suite startup when that measurement says a helper of this
// binary costs more here than the reference measurement did.
//
// WHY THE MEASURED FLOOR IS A LOWER BOUND AND NOT THE WHOLE ANSWER (measured
// 2026-10-12): this process's own startup is the wrong quantity to derive the
// window from on its own — in a run selecting a handful of tests it measures
// ~0.8s on this host, while the same binary re-exec'd as the HELPER pays the
// whole suite startup before its body (5.02s on the host where that fixture is
// known). Deriving the floor from the parent's startup alone would have rebuilt
// the same defect under a different constant, so the floor carries the worst
// measured fixture floor and only GROWS from the run's own measurement.
func diagnosisFloorNow() time.Duration {
	floor := diagnosisReadyFloor
	startup := diagnosisSuiteStartup()
	if startup <= 0 {
		return floor
	}
	if fromRun := startup * diagnosisReadyFloorMultiple; fromRun > floor {
		floor = fromRun
	}
	return floor
}

// diagnosisLockPath pins a private profile and returns the lock path inside it
// (the shape every serve-lock test in this package uses: the lock resolves
// through config.GopiDir, which GOPI_HOME outranks HOME for, so BOTH must be
// pinned for the test to stay off the real profile).
func diagnosisLockPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv(config.GopiDirEnv, dir)
	gopiDir := filepath.Join(dir, ".gopi")
	if err := os.MkdirAll(gopiDir, serveLockDirMode); err != nil {
		t.Fatalf("create %s: %v", gopiDir, err)
	}
	return filepath.Join(gopiDir, serveLockFilename)
}

// diagnosisOwnerPids starts the owner helper (flock the path, leak the
// descriptor to a child) and returns "<owner> <leak>" pids. die=true makes the
// owner exit the moment the leak is published — the shape that leaves the
// flock held by a live process while the kernel record names a DEAD pid.
func diagnosisOwnerPids(t *testing.T, lockPath, marker string, die bool) (owner, leak int) {
	t.Helper()
	dieVal := "0"
	if die {
		dieVal = diagnosisDieYes
	}
	h := startDiagnosisHelper(t,
		diagnosisModeEnv+"="+diagnosisModeHold,
		diagnosisPathEnv+"="+lockPath,
		diagnosisMarkerEnv+"="+marker,
		diagnosisDieEnv+"="+dieVal,
	)
	t.Cleanup(h.killAndReap)
	line := diagnosisWaitMarker(t, h, marker)
	fields := strings.Split(line, diagnosisPidSeparator)
	if len(fields) != 2 {
		t.Fatalf("the owner marker %q must carry two pids separated by %q", line, diagnosisPidSeparator)
	}
	owner, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatalf("owner pid %q in the marker: %v", fields[0], err)
	}
	leak, err = strconv.Atoi(fields[1])
	if err != nil {
		t.Fatalf("leaked-holder pid %q in the marker: %v", fields[1], err)
	}
	return owner, leak
}

// ── the readiness handshake of the re-exec'd helper ────────────────────────

// diagnosisTestSink is the half of *testing.T the handshake needs, so the
// regression case can hand it a RECORDING sink (diagnosisRecordedFailure) and
// assert on the exact text a real *testing.T would print. A recorder keeps
// Fatalf's contract — the case is over — without hard-coding *testing.T.
type diagnosisTestSink interface {
	Helper()
	Fatalf(format string, args ...any)
}

// diagnosisStderr is the child's captured stderr, bounded (Rule 15: the child's
// own words are the one thing that tells a crash from a hang, so they are read
// rather than dropped into the suite's log where nobody correlates them).
type diagnosisStderr struct {
	mu       sync.Mutex
	buf      []byte
	truncate bool
	done     chan struct{}
}

// newDiagnosisStderr is the ONE constructor: a zero-valued diagnosisStderr has
// a nil done channel and capture closes it (closing a nil channel panics).
func newDiagnosisStderr() *diagnosisStderr { return &diagnosisStderr{done: make(chan struct{})} }

// text words an empty capture, so a failure never prints a blank section that
// reads like a swallowed error (Rule 15b).
func (s *diagnosisStderr) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.buf) == 0 {
		return "(the helper wrote nothing to stderr)"
	}
	out := string(s.buf)
	if s.truncate {
		out += "\n[truncated — the helper wrote more than the capture cap]"
	}
	return out
}

// capture drains the helper's stderr until EOF, keeping at most
// diagnosisStderrCap bytes and SAYING so when it had to stop.
func (s *diagnosisStderr) capture(r io.Reader) {
	defer close(s.done)
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		s.mu.Lock()
		if len(s.buf) < diagnosisStderrCap {
			s.buf = append(s.buf, sc.Bytes()...)
			s.buf = append(s.buf, '\n')
		} else {
			s.truncate = true
		}
		s.mu.Unlock()
	}
}

// diagnosisHandshake is the parent's side of ONE helper spawn: the process, the
// moment it started, the captured stderr, the window its wait used, and the
// EVENT that says the child is gone.
type diagnosisHandshake struct {
	cmd       *exec.Cmd
	pid       int
	stderr    *diagnosisStderr
	startedAt time.Time
	// exited carries the result of the ONE cmd.Wait this handshake owns, so the
	// child's death can be OBSERVED while the parent is still waiting. Nothing
	// else may call cmd.Wait.
	exited chan error
	// measuredStartup is a startup cost THIS RUN already measured for this same
	// fixture (0 = none yet), which diagnosisReadyBudget scales the window by.
	measuredStartup time.Duration
	// budget is the window the wait actually used, so a failure message states
	// the number instead of recomputing it.
	budget time.Duration
}

// startDiagnosisHelper spawns the helper as an INDEPENDENT process with its
// stderr captured and its exit turned into an event.
//
// A spawn failure kills the case: a fixture that could not be started is an
// environment fault, never a case that quietly proves nothing.
func startDiagnosisHelper(t *testing.T, env ...string) *diagnosisHandshake {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestServeLockDiagnosisHelperProcess")
	cmd.Env = append(os.Environ(), env...)
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("capture the helper's stderr: %v", err)
	}
	startedAt := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the helper: %v", err)
	}
	h := &diagnosisHandshake{
		cmd:       cmd,
		pid:       cmd.Process.Pid,
		stderr:    newDiagnosisStderr(),
		startedAt: startedAt,
		exited:    make(chan error, 1),
	}
	go h.stderr.capture(stderrPipe)
	// ONE reaper goroutine per handshake: this is the event that makes a crash
	// distinguishable from slowness, and it is why no other code may wait.
	go func() { h.exited <- cmd.Wait() }()
	return h
}

// killAndReap stops the helper and collapses the wait into "it is gone". It
// never masks an assertion: the flock is released by the child's death, which
// is the point.
func (h *diagnosisHandshake) killAndReap() {
	if h.cmd.Process != nil {
		//stylegate:ignore teardown of the helper fixture; the kill releases the
		// child's flock, which is what the caller wants, and cannot mask an
		// assertion
		_ = h.cmd.Process.Kill()
	}
	select {
	case <-h.exited:
	case <-time.After(diagnosisDrainGrace):
	}
}

// drain gives a just-exited child's pipe a bounded moment to deliver its last
// words, so a failure message carries them instead of racing the reader
// goroutine.
func (h *diagnosisHandshake) drain() {
	select {
	case <-h.stderr.done:
	case <-time.After(diagnosisDrainGrace):
	}
}

// diagnosisWaitMarker waits (bounded, with a DEAD helper reported as an EVENT
// rather than as a timeout) for the helper's marker, and returns it.
//
// The window is DERIVED (diagnosisReadyBudget): the floor is this run's own
// measured suite startup × diagnosisReadyFloorMultiple, so it can never sit
// below the cost of re-exec'ing this binary — the defect this replaces was a
// fixed 100 × 25ms = 2.50s typed below a 5.02s measured fixture floor.
func diagnosisWaitMarker(sink diagnosisTestSink, h *diagnosisHandshake, marker string) string {
	t, isTest := sink.(*testing.T)
	sink.Helper()
	budget := diagnosisReadyBudget(diagnosisDeadlineOf(), time.Since(h.startedAt), h.measuredStartup, diagnosisFloorNow())
	h.budget = budget
	expires := h.startedAt.Add(budget)
	for time.Now().Before(expires) {
		if b, err := os.ReadFile(marker); err == nil {
			if line := strings.TrimSpace(string(b)); line != "" {
				h.measuredStartup = time.Since(h.startedAt)
				// The measurement this fix is built on, reported with every run
				// so the window can be compared against the cost it waits for
				// (Rule 38: the figure travels with the run that measured it).
				if isTest {
					t.Logf("diagnosis: the helper reached its body %s after spawn with a %s window (fixture floor %s)",
						h.measuredStartup.Round(time.Millisecond), budget, diagnosisMeasuredSuiteStartupFloor)
				}
				return line
			}
		}
		// The helper's DEATH is an event, not a timeout: report its own exit
		// status and its own stderr the moment it happens. Without this arm a
		// broken fixture and a slow host are the same sentence, and the fix for
		// one is the opposite of the fix for the other.
		select {
		case err := <-h.exited:
			h.drain()
			sink.Fatalf("the lock-holder helper EXITED before publishing its pids at %s — it died with %v.\n"+
				"This is a BROKEN FIXTURE, not a slow host (pid %d, started %s ago, window %s).\nhelper stderr:\n%s",
				marker, err, h.pid, time.Since(h.startedAt).Round(time.Millisecond), budget, h.stderr.text())
			return "" // UNREACHABLE for *testing.T, load-bearing for any sink that merely records (see diagnosisRecordedFailure): without it the loop keeps polling and the later TIMEOUT verdict overwrites this correct one
		default:
		}
		time.Sleep(diagnosisPollTick) //stylegate:allow-sleep poll tick of the readiness handshake; the loop exits as soon as the marker appears and a dead child is caught by the exit event, not by this sleep
	}
	h.drain()
	state := "the helper is still RUNNING"
	select {
	case err := <-h.exited:
		state = fmt.Sprintf("the helper EXITED (%v) as the window ran out", err)
	default:
	}
	sink.Fatalf("the lock-holder helper did not publish its pids at %s within %s (pid %d, started %s ago): %s.\n"+
		"The two positions are distinguishable by construction: a helper that DIES is reported as an EXIT event with its own status and stderr, so this verdict means it never crashed.\nhelper stderr:\n%s",
		marker, budget, h.pid, time.Since(h.startedAt).Round(time.Millisecond), state, h.stderr.text())
	return ""
}

// diagnosisWaitDead waits (bounded) for pid to stop being alive, so a
// DEAD-owner premise is established by observation rather than by sleeping
// hopefully.
func diagnosisWaitDead(t *testing.T, pid int, why string) {
	t.Helper()
	expires := time.Now().Add(diagnosisPollWindow)
	for time.Now().Before(expires) {
		if !tools.PidAlive(pid) {
			return
		}
		time.Sleep(diagnosisPollTick) //stylegate:allow-sleep bounded poll for the exited holder to be reaped
	}
	t.Fatalf("pid %d is still alive %s after the request (%s)", pid, diagnosisPollWindow, why)
}

// diagnosisKill SIGKILLs an explicit pid (never a pattern — an operator may
// have live gopi daemons) and waits for it to disappear.
//
// The pid is the one the helper PUBLISHED in its marker file, i.e. a number
// read back from disk, so Rule 42's ownership half cannot be claimed from the
// handle: the incarnation is read at delivery time and a recycled pid is REFUSED
// rather than killed. A refusal is not a failure here — the wait below still
// decides whether the holder is gone.
func diagnosisKill(t *testing.T, pid int) {
	t.Helper()
	if pid <= 0 {
		return
	}
	if id, err := testutil.ReadProcIdentity(pid); err == nil {
		//stylegate:ignore killing this test's own holder; the guard refuses a recycled pid, and the wait below fails loudly if the pid survived
		_ = testutil.KillIfIdentical(id, syscall.SIGKILL)
	}
	diagnosisWaitDead(t, pid, "killed by this test's cleanup")
}

// diagnosisInode reports the inode the path resolves to right now.
func diagnosisInode(t *testing.T, path string) uint64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	ino := tools.InodeOfFileInfo(fi)
	if ino == 0 {
		t.Fatalf("stat %s: no inode (this test needs the kernel's own inode)", path)
	}
	return ino
}

// diagnosisFdInode reports the inode of an OPEN descriptor — the join key the
// production diagnosis must use.
func diagnosisFdInode(t *testing.T, f *os.File) uint64 {
	t.Helper()
	fi, err := f.Stat()
	if err != nil {
		t.Fatalf("fstat %s: %v", f.Name(), err)
	}
	ino := tools.InodeOfFileInfo(fi)
	if ino == 0 {
		t.Fatalf("fstat %s: no inode", f.Name())
	}
	return ino
}

// diagnosisKernelRecords returns the kernel's FLOCK records for ino through the
// PRODUCTION reader (tools.KernelFlockRecords) — the same parser the diagnostic
// uses, so the test's instrument and the diagnosis cannot disagree about what
// the kernel said.
func diagnosisKernelRecords(t *testing.T, ino uint64) []string {
	t.Helper()
	records, err := tools.KernelFlockRecords(ino)
	if err != nil {
		t.Fatalf("read /proc/locks: %v (the diagnostic cannot be validated without the kernel's own table)", err)
	}
	return records
}

// diagnosisWaitKernelRecordFor waits (bounded) until the kernel records a FLOCK
// on ino naming pid, returning that record. It is how the rig proves its
// instrument is armed BEFORE it asserts on the diagnostic, so a missing record
// fails as "the mechanism is unproven on this host", never as a wrong message.
//
// The bound is diagnosisPollWindow — this polls a condition the RIG itself just
// produced (a flock it took milliseconds ago), not a re-exec'd binary's startup,
// so the readiness budget's derivation does not apply here and neither does the
// 5.02s startup floor it is built from.
func diagnosisWaitKernelRecordFor(t *testing.T, ino uint64, pid int, why string) string {
	t.Helper()
	want := strconv.Itoa(pid)
	expires := time.Now().Add(diagnosisPollWindow)
	for time.Now().Before(expires) {
		for _, rec := range diagnosisKernelRecords(t, ino) {
			if strings.Contains(rec, want) {
				return rec
			}
		}
		time.Sleep(diagnosisPollTick) //stylegate:allow-sleep bounded poll for the kernel's lock row to appear/follow the holder
	}
	t.Fatalf("the kernel recorded no FLOCK on inode %d naming pid %d within %s (%s) — records seen: %v",
		ino, pid, diagnosisPollWindow, why, diagnosisKernelRecords(t, ino))
	return ""
}

// diagnosisLine returns the one line of a refusal carrying the given named
// prefix, failing when it is absent (a missing line IS the defect, not a silent
// zero value).
func diagnosisLine(t *testing.T, refusal, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(refusal, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	t.Fatalf("the refusal carries no %q line:\n%s", strings.TrimSpace(prefix), refusal)
	return ""
}

// TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness is the
// regression guard for the epic: the refusal must carry the stray holder with
// its LIVENESS and the kernel's records for the inode the acquired descriptor
// was refused on — driven through the PRODUCTION path (acquireServeLock) with a
// REPLACED lock file. On the pre-change wiring the refusal carries neither line
// (the mutant run is quoted in devdoc/serve-lock-diagnosis-liveness.md).
func TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness(t *testing.T) {
	lockPath := diagnosisLockPath(t)

	// --- stage 1: the stray holder (flock on an inode the path no longer
	// names), still ALIVE -------------------------------------------------
	owner1, strayPid := diagnosisOwnerPids(t, lockPath, lockPath+".stray.pids", false)
	t.Cleanup(func() { diagnosisKill(t, strayPid); diagnosisKill(t, owner1) })
	originalInode := diagnosisInode(t, lockPath)
	// The instrument is validated BEFORE it is used: the kernel really records
	// a held flock on this host, so an empty answer later is evidence.
	diagnosisWaitKernelRecordFor(t, originalInode, owner1, "the flock on the original lock file must be recorded by the kernel")

	// The REPLACEMENT: unlink the held path and let a fresh file take the name.
	// The holder keeps its flock on the OLD (now unlinked) inode — that is what
	// a stray holder is made of.
	if err := os.Remove(lockPath); err != nil {
		t.Fatalf("remove the held lock path: %v", err)
	}
	fresh, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, serveLockFileMode)
	if err != nil {
		t.Fatalf("recreate the lock path: %v", err)
	}
	if err := fresh.Close(); err != nil {
		t.Fatalf("close the recreated lock path: %v", err)
	}
	refusedInode := diagnosisInode(t, lockPath)
	if refusedInode == originalInode {
		t.Fatalf("the recreated path reused inode %d — no replacement happened, so this test would prove nothing", originalInode)
	}
	if stray := tools.OrphanLockHolder(lockPath); stray <= 0 || !tools.PidAlive(stray) {
		t.Fatalf("OrphanLockHolder(%s) = %d (alive=%v): the stray holder is not visible, so the rig is not armed",
			lockPath, stray, tools.PidAlive(stray))
	}

	// --- stage 2: the flock the acquirer WILL be refused on is held through a
	// leaked descriptor whose OWNER IS DEAD — the kernel then records a corpse
	// --------------------------------------------------------------------
	owner2, leak2 := diagnosisOwnerPids(t, lockPath, lockPath+".fresh.pids", true)
	t.Cleanup(func() { diagnosisKill(t, leak2) })
	diagnosisWaitDead(t, owner2, "the second owner exits as soon as it has leaked the descriptor")
	diagnosisWaitKernelRecordFor(t, refusedInode, owner2,
		"the flock on the replaced inode is kept by the leaked descriptor")

	// --- stage 3: the PRODUCTION refusal -----------------------------------
	_, acquireErr := acquireServeLock()
	if acquireErr == nil {
		t.Fatalf("acquireServeLock WON the lock although pid %d still holds a flock on inode %d — the double-merge guard is down", leak2, refusedInode)
	}
	diag := acquireErr.Error()
	if !strings.Contains(diag, "already running") {
		t.Fatalf("the refusal must be the CONTENTION arm (EWOULDBLOCK), got:\n%s", diag)
	}

	// (i) the stray holder, named WITH its liveness — a bare pid is exactly the
	// artifact this epic removes.
	strayLine := diagnosisLine(t, diag, serveLockStrayHolderPrefix)
	wantStray := tools.OrphanLockHolder(lockPath)
	if wantStray <= 0 {
		t.Fatalf("the stray holder disappeared between the refusal and its assertion — the rig cannot be judged")
	}
	if !strings.Contains(strayLine, strconv.Itoa(wantStray)) {
		t.Errorf("the stray-holder line must name the stray holder pid %d, got:\n%s", wantStray, diag)
	}
	if !strings.Contains(strayLine, serveLockHolderLive) {
		t.Errorf("the stray-holder line must report the holder's LIVENESS (%q) — a bare pid is the artifact this diagnostic removes, got:\n%s", serveLockHolderLive, diag)
	}

	// (ii) the kernel's own table, joined on the inode the acquired descriptor
	// was refused on, reporting the DEAD owner the kernel still names.
	kernelLine := diagnosisLine(t, diag, serveLockKernelRecordsPrefix)
	if !strings.Contains(kernelLine, fmt.Sprintf(" %d:", refusedInode)) {
		t.Errorf("the kernel-records line must be matched on the refused inode %d (fstat of the acquired descriptor), got:\n%s", refusedInode, diag)
	}
	if !strings.Contains(kernelLine, strconv.Itoa(owner2)) {
		t.Errorf("the kernel records must name the flock's owner pid %d (DEAD, and the kernel still records it), got:\n%s", owner2, diag)
	}
	if strings.Contains(kernelLine, serveLockKernelNoRecords) {
		t.Errorf("the kernel holds a FLOCK on the refused inode, so the line must not say %q:\n%s", serveLockKernelNoRecords, diag)
	}
	if tools.PidAlive(owner2) {
		t.Errorf("owner pid %d must be dead at this point — the corpse-in-the-table premise was not established", owner2)
	}
}

// TestServeLockRefusalDiagnosisReadsTheDescriptorNotThePath pins the IDENTITY
// half: the kernel table must be joined on the ACQUIRED DESCRIPTOR's inode, not
// on whatever the lock PATH names now. The two differ exactly in the state the
// epic is about — a replacement — and a diagnostic that stats the path reports
// "no records" for the inode it was actually refused on.
//
// It calls the production renderer directly because the divergence cannot be
// produced by acquireServeLock itself (its descriptor is opened from the path,
// so the two coincide by construction). The end-to-end wiring is covered by
// TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness.
func TestServeLockRefusalDiagnosisReadsTheDescriptorNotThePath(t *testing.T) {
	lockPath := diagnosisLockPath(t)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, serveLockFileMode)
	if err != nil {
		t.Fatalf("open %s: %v", lockPath, err)
	}
	t.Cleanup(func() { _ = f.Close() }) //stylegate:ignore test teardown close; the assertion on the diagnostic has already run
	// A REAL flock: the kernel records it against THIS process, so the join the
	// diagnostic performs has something to find (and something to miss).
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("flock %s: %v", lockPath, err)
	}
	descriptorInode := diagnosisFdInode(t, f)
	diagnosisWaitKernelRecordFor(t, descriptorInode, os.Getpid(), "the descriptor's flock must be recorded by the kernel")

	// The replacement: the descriptor keeps the old inode, the path gets a new one.
	if err := os.Remove(lockPath); err != nil {
		t.Fatalf("remove the lock path: %v", err)
	}
	fresh, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, serveLockFileMode)
	if err != nil {
		t.Fatalf("recreate the lock path: %v", err)
	}
	if err := fresh.Close(); err != nil {
		t.Fatalf("close the recreated lock path: %v", err)
	}
	pathInode := diagnosisInode(t, lockPath)
	if pathInode == descriptorInode {
		t.Fatalf("the recreated path reused inode %d — the two identities coincide, so this test would prove nothing", descriptorInode)
	}

	kernelLine := diagnosisLine(t, serveLockRefusalDiagnosis(f, lockPath), serveLockKernelRecordsPrefix)
	if !strings.Contains(kernelLine, strconv.FormatUint(descriptorInode, 10)) {
		t.Errorf("the kernel records must be joined on the DESCRIPTOR's inode %d, got:\n%s", descriptorInode, kernelLine)
	}
	if !strings.Contains(kernelLine, strconv.Itoa(os.Getpid())) {
		t.Errorf("the kernel records must name this process (%d) as the descriptor's flock owner, got:\n%s", os.Getpid(), kernelLine)
	}
	if strings.Contains(kernelLine, strconv.FormatUint(pathInode, 10)) && pathInode != descriptorInode {
		t.Errorf("the kernel records were joined on the PATH's inode %d — a replaced lock file makes those two different files and the refusal happened on the descriptor's (%d), got:\n%s", pathInode, descriptorInode, kernelLine)
	}
}

// TestServeLockLivenessNoteReportsADeadHolder pins the DEAD half of the
// liveness wording with a genuinely dead pid (a reaped child), so the two words
// are a measurement rather than constants that happen to be printed. The ALIVE
// half is asserted end-to-end by
// TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness.
func TestServeLockLivenessNoteReportsADeadHolder(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestServeLockDiagnosisHelperProcess")
	cmd.Env = append(os.Environ(), diagnosisModeEnv+"="+diagnosisModeExit)
	err := cmd.Run() // it exits at once; a nonzero code would mean the helper body changed
	if cmd.Process == nil {
		t.Fatalf("the exit-helper never started: %v", err)
	}
	pid := cmd.Process.Pid
	diagnosisWaitDead(t, pid, "the exit helper returns immediately and is reaped by Run")
	if got := serveLockLivenessNote(pid); got != serveLockHolderDead {
		t.Errorf("serveLockLivenessNote(%d) = %q, want %q (the pid is reaped and cannot be a live holder)", pid, got, serveLockHolderDead)
	}
	if got := serveLockLivenessNote(os.Getpid()); got != serveLockHolderLive {
		t.Errorf("serveLockLivenessNote(this process) = %q, want %q", got, serveLockHolderLive)
	}
}

// TestServeLockDiagnosisHelperProcess is not a test: it is the rig's holder
// body, driven entirely by the environment (with no mode set it no-ops, so an
// ordinary `go test` run is untouched).
//
//	mode "hold"    flock the given path, re-exec THIS binary with the flocked
//	               descriptor as ExtraFiles[0] (the os/exec inheritance the
//	               daemon face uses), publish "<owner> <leak>", then either
//	               sleep (die=0) or exit at once (die=1) — the latter leaves the
//	               flock held by the leaked descriptor while the kernel record
//	               keeps naming the dead owner.
//	mode "inherit" the leaked descriptor's keeper: it holds fd 3 and only has to
//	               stay alive.
//	mode "exit"    exit immediately, so a test can obtain a genuinely dead pid.
//	mode "fail"    take the flock on the given path and die at once — the Rule 5
//	               regression body: the parent must report THIS child's exit
//	               status and stderr, not a slow host.
func TestServeLockDiagnosisHelperProcess(t *testing.T) {
	if mode := os.Getenv(diagnosisModeEnv); mode != "" {
		// UNREACHABLE BY CONSTRUCTION: TestMain hands a helper-mode process to
		// serveLockDiagnosisHelperMain and exits before m.Run. Reaching this line
		// means the TestMain dispatch was removed or a new spawn bypassed it, so
		// it fails LOUDLY rather than re-entering the suite pins that caused the
		// helper's exit 99.
		t.Fatalf("helper mode %q reached the suite's TestMain dispatch bypass — see RunServeLockDiagnosisHelperChild", mode)
	}
	t.Skip("helper process body — dispatched by TestMain; see RunServeLockDiagnosisHelperChild")
}

// RunServeLockDiagnosisHelperChild dispatches a re-exec'd helper of this rig
// BEFORE the suite pins anything, and is called from TestMain beside the other
// fixture children (RunDaemonLeakSpawnerChild, RunBorrowedFDWorkerChild).
//
// WHY IT MUST RUN BEFORE THE PINS (measured 2026-11-08). A helper is a FIXTURE,
// not a run of the suite: it runs one function and dies. Reaching the helper
// through m.Run means the helper process first executes the whole suite entry
// point — testutil.PinSuiteForTestMain → AcquireSuitePackageLock — and since that
// pin derives the scratch home from the PROCESS's own pins, the parent, its
// hold-helper and that helper's leak-child each computed a DIFFERENT lock
// directory. The second helper therefore found the FIRST helper's lock, judged a
// live pid that was its own PARENT'S child to be a SIBLING RUN (`verdict live`)
// and exited with testutil.SuiteBusyExitCode (99) before publishing the pid
// marker the test waits for. The test then reported "the lock-holder helper
// EXITED before publishing its pids … This is a BROKEN FIXTURE" while its parent
// was perfectly healthy. The branch, the two helpers' environments and the three
// rejected alternatives are recorded in
// devdoc/serve-lock-diagnosis-helper-suite-pin.md.
//
// It answers false when no helper mode is set, so an ordinary `go test` is
// unaffected.
func RunServeLockDiagnosisHelperChild() bool {
	mode := os.Getenv(diagnosisModeEnv)
	if mode == "" {
		return false
	}
	os.Exit(serveLockDiagnosisHelperMain(mode))
	return true // UNREACHABLE: os.Exit above ends the process
}

// serveLockDiagnosisHelperMain is the helper's whole life, with no *testing.T in
// sight: the process it runs in never reaches m.Run, so a t.Fatalf would have
// nothing to report. A fault therefore prints to stderr and returns a NON-ZERO
// code, which the parent observes as a normal child exit (the same channel the
// fail body uses, Rule 5).
func serveLockDiagnosisHelperMain(mode string) int {
	switch mode {
	case diagnosisModeExit:
		return 0
	case diagnosisModeFail:
		// The Rule 5 REGRESSION body: take the flock on the given path and then
		// DIE, before publishing anything, carrying this child's own words on
		// stderr and its own exit status. It is the fixture that makes "the
		// helper died" an observable event instead of a slow host.
		failPath := os.Getenv(diagnosisPathEnv)
		failFile, err := os.OpenFile(failPath, os.O_CREATE|os.O_RDWR, serveLockFileMode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "diagnosis fail helper open %s: %v\n", failPath, err)
			return diagnosisFailExitCode
		}
		if err := syscall.Flock(int(failFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			fmt.Fprintf(os.Stderr, "diagnosis fail helper flock %s: %v\n", failPath, err)
			return diagnosisFailExitCode
		}
		fmt.Fprintln(os.Stderr, os.Getenv(diagnosisFailStderrEnv))
		// The exit STATUS is the point: it is what a parent can observe from a
		// child that never reached readiness.
		return diagnosisFailExitCode
	case diagnosisModeInherit:
		// The leaked copy: this process owns nothing but the descriptor it
		// inherited on fd 3. Being ALIVE is the whole fixture.
		time.Sleep(diagnosisHoldSec * time.Second) //stylegate:allow-sleep the inherited-descriptor holder exists only to keep the flock held past its owner's death
		return 0
	case diagnosisModeHold:
		// fall through to the owner body below
	default:
		fmt.Fprintf(os.Stderr, "diagnosis helper: unknown mode %q\n", mode)
		return diagnosisHelperUnknownModeExitCode
	}

	lockPath := os.Getenv(diagnosisPathEnv)
	marker := os.Getenv(diagnosisMarkerEnv)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, serveLockFileMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "diagnosis helper open %s: %v\n", lockPath, err)
		return diagnosisHelperFaultExitCode
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		fmt.Fprintf(os.Stderr, "diagnosis helper flock %s: %v\n", lockPath, err)
		return diagnosisHelperFaultExitCode
	}
	leaked := exec.Command(os.Args[0], "-test.run=TestServeLockDiagnosisHelperProcess")
	leaked.Env = append(os.Environ(),
		diagnosisModeEnv+"="+diagnosisModeInherit,
		diagnosisPathEnv+"="+lockPath,
		diagnosisMarkerEnv+"="+marker,
	)
	// fd 3 carries the FLOCKED descriptor: the child inherits the lock, so it
	// survives this process's death (the measured mechanism, 2026-09-17).
	leaked.ExtraFiles = []*os.File{f}
	if err := leaked.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "diagnosis helper start the inheriting child: %v\n", err)
		return diagnosisHelperFaultExitCode
	}
	if err := os.WriteFile(marker, []byte(fmt.Sprintf("%d%s%d", os.Getpid(), diagnosisPidSeparator, leaked.Process.Pid)), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "diagnosis helper write marker: %v\n", err)
		return diagnosisHelperFaultExitCode
	}
	if os.Getenv(diagnosisDieEnv) == diagnosisDieYes {
		// The owner's death is the point of this shape: the flock stays held by
		// the leaked descriptor, and the kernel keeps naming THIS (now dead) pid.
		return 0
	}
	time.Sleep(diagnosisHoldSec * time.Second) //stylegate:allow-sleep the owner holder exists to keep its flock held while the test drives the refusal
	return 0
}
