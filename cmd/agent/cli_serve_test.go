package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// The child-process lock holder used by the serve-lock regression tests: an
// INDEPENDENT process holds the flock (the production shape), so a check that
// exempts the acquiring process's own pid cannot weaken the assertion.
const (
	serveLockHolderHelperEnv = "GOPI_TEST_SERVE_LOCK_HOLDER"
	serveLockHolderReadyEnv  = "GOPI_TEST_SERVE_LOCK_READY"

	// serveLockHolderFailEnv arms the helper's Rule 5 REGRESSION body: it takes
	// the flock and then exits with its own error on stderr, BEFORE signalling
	// readiness. serveLockHolderFailStderrEnv carries the line it prints.
	serveLockHolderFailEnv        = "GOPI_TEST_SERVE_LOCK_FAIL_BEFORE_READY"
	serveLockHolderFailStderrEnv  = "GOPI_TEST_SERVE_LOCK_FAIL_STDERR"
	serveLockHolderFailStderrWord = "fixture: refusing to signal readiness"
	// serveLockHolderFailExitCode is the code that body exits with, chosen
	// non-zero and distinctive so the parent's failure message can be asserted
	// to carry the CHILD's status rather than a generic one.
	serveLockHolderFailExitCode = 66

	// serveLockHolderHoldDuration is how long the helper keeps holding the
	// flock (bounded: t.Cleanup kills it, this is the backstop).
	serveLockHolderHoldDuration = 60 * time.Second

	// serveLockHolderMinWait is the FLOOR of the readiness handshake.
	//
	// MEASURED 2026-10-12 on this host (load ~4): the holder reaches readiness
	// 5.25–6.25s after spawn on an IDLE machine — the cost is the test BINARY's
	// own suite startup (testutil's PinSuiteForTestMain pins, plus the close-out
	// sweeps), which the helper pays before its body runs; `-test.run='^$'`
	// alone measures 5.02s. The budget this replaces was a FIXED 100 × 50ms =
	// 5.00s, i.e. BELOW its own fixture's floor — the handshake was a race
	// against the suite's startup and lost on any load (1 FAIL / 5 runs measured
	// here). The budget is now derived (see serveLockHolderWaitBudget); this
	// constant is the floor that derivation falls back to.
	serveLockHolderMinWait = 30 * time.Second

	// serveLockHolderSampleMultiple scales the budget by a MEASURED startup
	// sample of this same fixture, so a loaded machine widens the window
	// instead of turning the test red.
	serveLockHolderSampleMultiple = 3

	// serveLockHolderDeadlineReserve is held back from the test's own deadline:
	// the handshake must be the thing that FAILS (naming the child's state),
	// never Go's -test.timeout panicking over the whole binary.
	serveLockHolderDeadlineReserve = 15 * time.Second

	// serveLockHolderDefaultDeadline mirrors testing's own default -test.timeout
	// (10m). It is used only when the flag cannot be read.
	serveLockHolderDefaultDeadline = 10 * time.Minute

	// serveLockHolderWaitTick is the poll period of the readiness handshake —
	// it bounds how long after readiness the parent notices, not how long it is
	// willing to wait (that is serveLockHolderWaitBudget).
	serveLockHolderWaitTick = 50 * time.Millisecond

	// serveLockHolderStderrCap bounds what the parent will hold of the child's
	// stderr: a fixture that spews must not turn a verdict read into an
	// unbounded read.
	serveLockHolderStderrCap = 8 << 10

	// serveLockHolderDrainGrace is how long the parent waits for a killed
	// child's last words and exit status before it stops draining.
	serveLockHolderDrainGrace = 2 * time.Second
)

// TestAcquireServeLock: M4.5 ③ — the serve daemon is single-instance via an
// flock (LOCK_EX|LOCK_NB) on ~/.gopi/serve.lock. The FIRST acquirer wins
// (the file gets the holder's pid); a second acquirer on the same machine is
// REFUSED with an error naming the holder. Releasing (closing) the file
// lets the next acquirer in.
func TestAcquireServeLock(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)            // acquireServeLock resolves ~/.gopi/serve.lock
	t.Setenv(config.GopiDirEnv, dir) // …and GOPI_HOME outranks HOME, so pin it too

	lock, err := acquireServeLock()
	if err != nil {
		t.Fatalf("first acquireServeLock must win: %v", err)
	}
	// The lock file exists and carries this process's pid.
	pidBytes, err := os.ReadFile(filepath.Join(dir, ".gopi", serveLockFilename))
	if err != nil {
		t.Fatalf("lock file missing: %v", err)
	}
	if got := strings.TrimSpace(string(pidBytes)); got == "" {
		t.Errorf("lock file must carry the holder pid, got empty")
	}
	// A second acquisition in THIS process is ADMITTED (serve_lock_self_hold.go):
	// flock lives on the open file description, so the pre-fix acquisition here
	// was refused by its own guard — the defect that made `gopi task
	// finish-cleanup --apply` refuse itself, naming its own pid as "another merge
	// process". Refusal of a REAL second writer is what this lock exists for and
	// is pinned by a SEPARATE process in TestServeLockThirdPartyHolderStillRefuses.
	selfAdmitted, err := acquireServeLock()
	if err != nil {
		t.Fatalf("a second acquisition in this process must be admitted, got: %v", err)
	}
	if selfAdmitted != nil {
		selfAdmitted.Close() //stylegate:ignore the admitted handle is non-locking; closing it is what proves the outer flock is untouched
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("the admitted second acquisition must not have dropped the first flock: %v", err)
	}
	lock.Close() //stylegate:ignore test releases the serve flock to prove re-acquisition; a close error cannot mask a held lock, which is what the following acquire asserts
	// After release, the lock is acquirable again.
	lock2, err := acquireServeLock()
	if err != nil {
		t.Fatalf("re-acquire after release must win: %v", err)
	}
	lock2.Close() //stylegate:ignore test releases the second serve flock at test end; the descriptor is reclaimed by the OS and the assertion already ran
}

// TestAcquireServeLock_RefusesAfterUnlinkUnderneath is the regression guard
// for the 2026-09-12 double-merge incident: flock lives on the INODE, so
// removing the lock PATH while a daemon runs leaves that daemon locking an
// inode no name points at, while the path gets a FRESH inode whose lock is
// free. Pre-fix, the next acquirer WON that fresh file while the first daemon
// kept merging — the REALIZED double-merge (in the field: an `--once` pass ran
// concurrently with the resident daemon, and in the hermetic A/B the old
// binary printed "[merger] --once: single pass done" alongside the daemon).
//
// The holder is a REAL separate process here (the same shape as production),
// so no pid-exemption of the acquiring process can weaken the assertion:
// a genuine stray holder must REFUSE a second daemon, a read-only probe must
// SEE it, and the repair path must leave the path locked by its own process.
func TestAcquireServeLock_RefusesAfterUnlinkUnderneath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)            // acquireServeLock resolves ~/.gopi/serve.lock
	t.Setenv(config.GopiDirEnv, dir) // …and GOPI_HOME outranks HOME, so pin it too
	gopiDir := filepath.Join(dir, ".gopi")
	path := filepath.Join(gopiDir, serveLockFilename)
	if err := os.MkdirAll(gopiDir, serveLockDirMode); err != nil {
		t.Fatal(err)
	}

	// A child process holds the lock and reports when it is up — exactly what
	// a running merger does. (It is stopped via t.Cleanup so a failing
	// assertion never leaves a stray holder behind.)
	ready := filepath.Join(dir, "holder.ready")
	holder := startHolderHandshake(t, holderHandshakeOptions{lockPath: path, readyPath: ready})
	t.Cleanup(func() { holder.killAndReap(t) })
	startup := holder.waitReady(t, ready)

	// The unlink UNDERNEATH the holder — the exact mechanism of the incident.
	if err := os.Remove(path); err != nil {
		t.Fatalf("unlink the held lock: %v", err)
	}

	// A read-only probe must SEE the orphan state: the path is free, yet a
	// live process still locks an unlinked copy of it.
	if got := tools.OrphanLockHolder(path); got != holder.pid {
		t.Fatalf("OrphanLockHolder = %d, want the holder %d (the unlink-underneath state must be detectable)", got, holder.pid)
	}

	// Pre-fix this acquire WON the fresh file while the holder kept its
	// detached lock — the double-merge. It must be refused.
	if _, err := acquireServeLock(); err == nil || !strings.Contains(err.Error(), "UNLINKED copy") {
		t.Fatalf("a stray unlinked holder must refuse a second daemon, got err=%v", err)
	}

	// Drop the independent holder; from here this process plays the resident
	// daemon whose lock path was replaced. The startup sample the handshake
	// measured is logged together with the window that was ACTUALLY used and
	// the window this run's own sample would give NOW — so a reader sees, on the
	// real machine, both the budget in force and what the self-calibration
	// computes from a measurement (it stays latent while this fixture's startup
	// is well under the floor, which is the point of the floor).
	t.Logf("holder reached readiness in %s (window used %s; re-derived from that sample now: %s)",
		startup.Round(time.Millisecond),
		holder.budget.Round(time.Millisecond),
		serveLockHolderWaitBudget(serveLockHolderTestDeadline(), startup, startup).Round(time.Millisecond))
	holder.killAndReap(t)
	own, err := lockAndVerifyIdentity(path)
	if err != nil {
		t.Fatalf("acquiring the fresh file after the stray holder is gone must succeed: %v", err)
	}

	// The per-tick repair a resident daemon runs: its flock is detached
	// (path unlinked underneath it), so it drops that lock and re-takes the
	// current path — making the single-instance guard true again.
	if err := os.Remove(path); err != nil {
		t.Fatalf("unlink underneath ourselves: %v", err)
	}
	repaired := ensureServeLockIdentity(own, "test-merger")
	if ok, err := sameFileAsPath(repaired, path); err != nil || !ok {
		t.Fatalf("repair must leave the path locked by this process, ok=%v err=%v", ok, err)
	}
	// After the repair the guard is TRUE again, and the proof is that the flock
	// is genuinely held: a same-process second acquisition is now ADMITTED
	// (serve_lock_self_hold.go — the self-held lock is this process's own guard),
	// while a third party is still refused (TestServeLockThirdPartyHolderStillRefuses).
	if _, err := acquireServeLock(); err != nil {
		t.Fatalf("after the repair the same process must be admitted its own lock, got err=%v", err)
	}
	repaired.Close() //stylegate:ignore test cleanup: the assertions have run; the OS releases the fd
}

// TestServeLockHolderHelperProcess is not a test: it is the child-process
// body that holds the serve lock for
// TestAcquireServeLock_RefusesAfterUnlinkUnderneath (the production shape —
// an independent holder, not the test process itself). It exits immediately
// unless the env names a lock path.
//
// With serveLockHolderFailEnv set it runs the Rule 5 REGRESSION body instead:
// it takes the flock and then fails, in the child, BEFORE signalling
// readiness — the fixture that makes "the child died" observable. It reports
// through its own stderr and its own exit status, because those two are
// exactly what a test can observe from a child that never reached readiness.
func TestServeLockHolderHelperProcess(t *testing.T) {
	path := os.Getenv(serveLockHolderHelperEnv)
	if path == "" {
		t.Skip("helper process body — driven by TestAcquireServeLock_RefusesAfterUnlinkUnderneath")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, serveLockFileMode)
	if err != nil {
		t.Fatalf("helper open: %v", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("helper flock: %v", err)
	}
	if os.Getenv(serveLockHolderFailEnv) != "" {
		fmt.Fprintln(os.Stderr, os.Getenv(serveLockHolderFailStderrEnv))
		os.Exit(serveLockHolderFailExitCode) // no return path: the point is the exit STATUS
	}
	if err := os.WriteFile(os.Getenv(serveLockHolderReadyEnv), []byte("1"), 0o644); err != nil {
		t.Fatalf("helper ready marker: %v", err)
	}
	time.Sleep(serveLockHolderHoldDuration) //stylegate:allow-sleep synthetic holder hold: the helper keeps its lock for this window, so waiting it out IS the fixture
}

// ── the readiness handshake of the child-process lock holder ────────────────
//
// WHY THIS IS MACHINERY AND NOT A POLL LOOP. The pre-fix handshake waited a
// FIXED 5s and then said "helper never signalled readiness" — a sentence that
// names the PARENT's budget and says nothing about the CHILD. So two opposite
// states were indistinguishable at the exact moment it mattered: a child that
// was merely slow (the measured 5.25–6.25s startup above — a working fixture
// reported as broken) and a child that had crashed taking the flock with it
// (a broken fixture that a longer budget would have reported as a hang). The
// handshake below separates them by construction: the child's exit is an EVENT
// the parent observes (its exit status and its own stderr are reported
// immediately), and the budget is derived from the test's deadline instead of
// typed in.

// holderTestSink is where the handshake reports a failure: *testing.T in every
// real case, a recorder in the Rule 5 regression case — so the message the
// parent WOULD print for a dead child is itself assertable. It keeps t.Fatalf's
// contract (a failure ends the case) without hard-coding *testing.T.
type holderTestSink interface {
	Helper()
	Fatalf(format string, args ...any)
}

// holderStderr is the child's captured stderr, bounded (Rule 15: the child's
// own error is the one thing that tells a crash from a hang, so it is read, not
// discarded to the suite's log where nobody correlates it).
type holderStderr struct {
	mu       sync.Mutex
	buf      []byte
	truncate bool
	done     chan struct{}
}

// newHolderStderr is the ONE constructor: a zero-valued holderStderr has a nil
// done channel, and capture closes it (a nil-channel close panics).
func newHolderStderr() *holderStderr { return &holderStderr{done: make(chan struct{})} }

// holderStderrOrNone words an empty capture, so a failure never prints an empty
// section that reads like a swallowed error (Rule 15b).
func (s *holderStderr) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.buf) == 0 {
		return "(the child wrote nothing to stderr)"
	}
	out := string(s.buf)
	if s.truncate {
		out += "\n[truncated — the child wrote more than the capture cap]"
	}
	return out
}

// capture drains the child's stderr until EOF, keeping at most
// serveLockHolderStderrCap bytes.
func (s *holderStderr) capture(r io.Reader) {
	defer close(s.done)
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		s.mu.Lock()
		if len(s.buf) < serveLockHolderStderrCap {
			s.buf = append(s.buf, sc.Bytes()...)
			s.buf = append(s.buf, '\n')
		} else {
			s.truncate = true
		}
		s.mu.Unlock()
	}
}

// holderHandshake is the parent's side of ONE holder spawn: the process, the
// budget its wait is allowed, the moment it was started, and the event that
// says it is gone.
type holderHandshake struct {
	cmd       *exec.Cmd
	pid       int
	stderr    *holderStderr
	startedAt time.Time
	exited    chan error
	// measuredStartup is a startup cost this RUN already measured for this same
	// fixture (0 = none). waitReady hands it to serveLockHolderWaitBudget, which
	// is the self-calibrating half of the window.
	measuredStartup time.Duration
	// budget is the window waitReady actually used (set by waitReady), so a
	// caller — and a failure message — can state the number rather than
	// recompute it.
	budget time.Duration
}

// holderHandshakeOptions is what varies between the real fixture and the
// regression one.
type holderHandshakeOptions struct {
	lockPath  string
	readyPath string
	// failBeforeReady runs the helper's Rule 5 failure body (flock taken, then
	// exit carrying its own error) instead of the readiness handshake.
	failBeforeReady bool
	// measuredStartup is a startup cost THIS RUN already measured for this same
	// fixture, or 0 when this is the first handshake (the normal case: these
	// tests spawn ONE holder each, so the floor applies and the scaling stays
	// latent until a host's startup approaches it — the reading it does is the
	// caller's measurement, and its contract is exercised directly by
	// TestServeLockHolderWaitBudget_TracksTheDeadlineAndTheMeasuredStartup).
	measuredStartup time.Duration
}

// serveLockHolderTestDeadline is the wall-clock budget the WHOLE test has: Go's
// own -test.timeout, which a run may set. Reading it (rather than assuming the
// default) is what ties the handshake to the run's configuration.
func serveLockHolderTestDeadline() time.Duration {
	f := flag.Lookup("test.timeout")
	if f == nil {
		return serveLockHolderDefaultDeadline
	}
	d, err := time.ParseDuration(f.Value.String())
	if err != nil || d <= 0 {
		return serveLockHolderDefaultDeadline
	}
	return d
}

// serveLockHolderWaitBudget is the adaptive logic itself, as a PURE function so
// it is testable on its own (Rule 5): how long the handshake may still wait,
// given the whole test deadline, how much of it is already spent, and a
// MEASURED startup sample of this same fixture (0 = none measured yet).
//
// It is a floor (serveLockHolderMinWait), raised by
// serveLockHolderSampleMultiple × the measured startup once a sample exists —
// so the window tracks the machine — and always capped by what is left of the
// deadline, minus serveLockHolderDeadlineReserve, so this handshake fails
// naming the child instead of panicking with the binary on -test.timeout.
func serveLockHolderWaitBudget(deadline, elapsed, measuredStartup time.Duration) time.Duration {
	budget := serveLockHolderMinWait
	if measuredStartup > 0 {
		if scaled := measuredStartup * serveLockHolderSampleMultiple; scaled > budget {
			budget = scaled
		}
	}
	if remaining := deadline - elapsed - serveLockHolderDeadlineReserve; remaining < budget {
		budget = remaining
	}
	if budget < serveLockHolderWaitTick {
		return serveLockHolderWaitTick
	}
	return budget
}

// startHolderHandshake spawns an INDEPENDENT process holding the flock — the
// production shape, so no pid-exemption of the acquiring process can weaken a
// later assertion — with its stderr captured and its exit turned into an event.
//
// A spawn failure kills the case: a fixture that could not be started is an
// environment fault, never a case that quietly proves nothing.
func startHolderHandshake(t *testing.T, opts holderHandshakeOptions) *holderHandshake {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestServeLockHolderHelperProcess")
	// testutil.FixtureChildEnv is the ONE env assembler for a fixture child: an
	// override REPLACES any ambient entry of the same name instead of sitting
	// behind it (Go's os.Getenv returns the FIRST match in the block), so the
	// value that must win here is the one this test sets.
	// See internal/testutil/fixturelifetime.go.
	overrides := []string{
		serveLockHolderHelperEnv + "=" + opts.lockPath,
		serveLockHolderReadyEnv + "=" + opts.readyPath,
	}
	if opts.failBeforeReady {
		overrides = append(overrides,
			serveLockHolderFailEnv+"=1",
			serveLockHolderFailStderrEnv+"="+serveLockHolderFailStderrWord,
		)
	}
	cmd.Env = testutil.FixtureChildEnv(os.Environ(), overrides...)
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("capture the holder's stderr: %v", err)
	}
	startedAt := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start holder: %v", err)
	}
	h := &holderHandshake{
		cmd:       cmd,
		pid:       cmd.Process.Pid,
		stderr:    newHolderStderr(),
		startedAt: startedAt,
		exited:    make(chan error, 1),
	}
	go h.stderr.capture(stderrPipe)
	// ONE reaper goroutine per handshake, so "the child is gone" can be
	// OBSERVED while the parent is still waiting (this is the event that makes
	// a crash distinguishable from slowness) — and so no other code may call
	// cmd.Wait.
	go func() { h.exited <- cmd.Wait() }()
	return h
}

// killAndReap stops the holder and collapses the wait into "it is gone", the
// shape the tests use when they give up the fixture deliberately. It never
// masks an assertion: the flock is released by the child's death, which is the
// point.
func (h *holderHandshake) killAndReap(t *testing.T) {
	t.Helper()
	if h.cmd.Process != nil {
		//stylegate:ignore teardown of the holder fixture; the kill releases the
		// child's flock, which is what the caller wants, and cannot mask an
		// assertion
		_ = h.cmd.Process.Kill()
	}
	select {
	case <-h.exited:
	case <-time.After(serveLockHolderDrainGrace):
	}
}

// killOnCleanup arms t.Cleanup so a failing assertion never leaves a stray
// holder (or a stray goroutine draining its pipe) behind.
func (h *holderHandshake) killOnCleanup(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { h.killAndReap(t) })
}

// waitReady is the handshake: block until the child signals readiness, and
// report — immediately, and with the CHILD's own words — if it dies instead.
//
// On success it returns how long the child's startup took, which is the sample
// a later handshake in the same test scales its budget by.
func (h *holderHandshake) waitReady(sink holderTestSink, path string) time.Duration {
	sink.Helper()
	deadline := serveLockHolderTestDeadline()
	elapsed := time.Since(h.startedAt)
	budget := serveLockHolderWaitBudget(deadline, elapsed, h.measuredStartup)
	h.budget = budget
	expires := h.startedAt.Add(budget)
	for time.Now().Before(expires) {
		if _, err := os.Stat(path); err == nil {
			return time.Since(h.startedAt)
		}
		// The child's DEATH is an event, not a timeout: report its own status
		// and stderr the moment it happens.
		select {
		case err := <-h.exited:
			h.drain()
			sink.Fatalf("the lock-holder helper EXITED before signalling readiness at %s — it took the flock and died with %v.\n"+
				"This is a BROKEN FIXTURE, not a slow host (pid %d, started %s ago, budget %s).\nhelper stderr:\n%s",
				path, err, h.pid, time.Since(h.startedAt).Round(time.Millisecond), budget, h.stderr.text())
			// UNREACHABLE for *testing.T (Fatalf ends the goroutine) and
			// load-bearing for any sink that merely RECORDS the failure: without
			// it the loop keeps polling and the TIMEOUT verdict overwrites this
			// one — measured 2026-10-12, the exit verdict was correct and a
			// second Fatalf 30s later replaced it, so the assertion read a
			// slowness message for a child that had died at once.
			return 0
		default:
		}
		time.Sleep(serveLockHolderWaitTick) //stylegate:allow-sleep poll tick of the holder-ready handshake; the loop exits as soon as the marker file appears, and a dead child is caught by the exit event, not by this sleep
	}
	// Out of budget with the child still alive and silent: report its state, so
	// "slow" and "crashed" can never be read as the same thing again.
	h.drain()
	state := "the child is still RUNNING"
	select {
	case err := <-h.exited:
		// The race at the very end of the window: the loop's last check ran
		// microseconds before the exit landed. Report the exit, not a timeout.
		state = fmt.Sprintf("the child EXITED (%v) as the budget ran out", err)
	default:
	}
	sink.Fatalf("the lock-holder helper did not signal readiness at %s within %s (pid %d, started %s ago): %s.\n"+
		"Positions are distinguishable by construction: a dead child is reported as an EXIT event with its own stderr, so this verdict means the child never crashed.\nhelper stderr:\n%s",
		path, budget, h.pid, time.Since(h.startedAt).Round(time.Millisecond), state, h.stderr.text())
	return 0
}

// drain gives a just-exited child's pipe a bounded moment to deliver its last
// words, so a failure message carries them instead of racing the reader
// goroutine.
func (h *holderHandshake) drain() {
	select {
	case <-h.stderr.done:
	case <-time.After(serveLockHolderDrainGrace):
	}
}

// TestSameFileAsPath_DistinguishesFiles: the identity comparison that the
// unlink-underneath check rests on. Same descriptor vs path ⇒ true; a
// descriptor of ANOTHER file ⇒ false. (The replaced-inode case is covered
// end-to-end by TestAcquireServeLock_RefusesAfterUnlinkUnderneath, because a
// tmpfs may reuse a freed inode number and a raw inode-compare test would be
// flaky.)
func TestSameFileAsPath_DistinguishesFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.lock")
	b := filepath.Join(dir, "b.lock")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("x"), serveLockFileMode); err != nil {
			t.Fatalf("seed %s: %v", p, err)
		}
	}
	f, err := os.Open(a)
	if err != nil {
		t.Fatalf("open a: %v", err)
	}
	defer f.Close() //stylegate:ignore test cleanup: reading the fd is done, the OS reclaims it

	if ok, err := sameFileAsPath(f, a); err != nil || !ok {
		t.Errorf("same file must compare equal, ok=%v err=%v", ok, err)
	}
	if ok, err := sameFileAsPath(f, b); err != nil || ok {
		t.Errorf("a different file must NOT compare equal, ok=%v err=%v", ok, err)
	}
}

// TestServeSkipTagFilter: M4.5 ③ — serve --skip-tag TAGS must make rows
// carrying any of those tags ineligible for claim (anti-misclaim), while
// untagged rows remain claimable.
func TestServeSkipTagFilter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)

	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "daemon-managed row",
		Payload: "p1",
		TaskID:  "t-daemon-1",
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed untagged: %v", err)
	}
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "[PA-QUEUE] human-managed row",
		Payload: "p2",
		Tags:    "pa-queue",
		TaskID:  "t-pa-1",
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed pa-queue: %v", err)
	}

	// With --skip-tag pa-queue, the pa-queue row is skipped (empty queue
	// outcome once only the daemon-managed row would... no: the OTHER row
	// is still eligible, so the claim lands on t-daemon-1).
	res, err := tools.TaskNextFiltered("tester", "", "pa-queue")
	if err != nil {
		t.Fatalf("TaskNextFiltered: %v", err)
	}
	if res.Empty || res.Task.TaskID != "t-daemon-1" {
		t.Fatalf("skip-tag must claim the untagged row, got %+v", res)
	}

	// When ONLY skipped-tag rows remain, the outcome is Empty (skip, never
	// misclaim).
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "another pa row", Payload: "p3", Tags: "pa-queue", TaskID: "t-pa-2",
	}); err != nil {
		t.Fatalf("seed pa-2: %v", err)
	}
	// t-pa-1 is still pending; a claimer skipping pa-queue and NOT eligible
	// for others must report empty. Exclude the session-less daemon row by
	// using excludeSession... simpler: fresh queue with only pa rows.
	// (t-daemon-1 was claimed above, so the queue holds only pa rows.)
	res, err = tools.TaskNextFiltered("tester", "", "pa-queue")
	if err != nil {
		t.Fatalf("TaskNextFiltered 2: %v", err)
	}
	if !res.Empty {
		t.Fatalf("skip-tag must NOT claim tagged rows, got %+v", res.Task)
	}
	// And without the filter the tagged row IS claimable.
	res, err = tools.TaskNext("tester", "")
	if err != nil {
		t.Fatalf("TaskNext: %v", err)
	}
	if res.Empty || res.Task.TaskID != "t-pa-1" {
		t.Fatalf("no-filter claim must reach the tagged row, got %+v", res)
	}
}

// TestRunTaskWorker_SurvivesFailedRun — bugfix-task-p1 regression on the
// WORKER face (epic serve/worker-split): the serve daemon used to os.Exit(1)
// on every failure path, so the first task that failed killed the dispatcher
// dead. The M4.20 worker loop must SURVIVE a failed run: runTaskRunOnce
// returns an error and the long-running worker logs-and-continues to the
// next round — a failing row lands `failed` (the writeback) and the loop
// keeps going (--once then drains and exits 0, having proven the failure
// did not kill it).
//
// Hermetic child failure: taskChildFailTestEnv=1 makes runTaskRunOnce treat
// the child as failed WITHOUT spawning it (no model call, no API key) —
// the REAL failed-child writeback + continue path.
func TestRunTaskWorker_SurvivesFailedRun(t *testing.T) {
	// repo-context sandbox: the in-process driver runs the round's own-worktree
	// release under THIS process's cwd — the live repository — so its pin is the
	// only control (no child env vector exists to carry one). See
	// repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	sandbox := t.TempDir()
	t.Setenv("GOPI_HOME", sandbox)
	path := filepath.Join(sandbox, "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)
	t.Setenv("GOPI_COMPUTE_NODES", "")
	t.Setenv(config.SkipClaimBroadcastEnv, "1") // single-node hermetic claim
	t.Setenv(taskChildFailTestEnv, "1")         // child fails without spawning
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "worker-survival fixture",
		Payload: "run the delivered fix's tests", // dispatchable payload
		TaskID:  "t-worker-survive",
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Fresh worker semantics: the only event under test is the claim→failed-run
	// →continue path. The merger sweeps (adoption/branch-scan) are NOT the
	// worker's, and the worker round carries no ledger-reconciliation leg at
	// all — the task-lifecycle pass was deleted 2026-10-30 — so there is no
	// clock left to throttle here.

	// Must RETURN (not os.Exit) — os.Exit(1) here would kill the test binary.
	runTaskWorker(true, "", nil) // --once: claim → failed run → continue → drain → exit
	// The claim + failed writeback both happened: the row is TERMINAL failed
	// (the synthetic child failure was written back, not left mid-run).
	row, err := tools.TaskShow("t-worker-survive")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(row, "status:") || !strings.Contains(row, "failed") {
		t.Fatalf("failed run must write the row failed, got:\n%s", row)
	}
	if !strings.Contains(row, "result:") || !strings.Contains(row, "synthetic child failure") {
		t.Fatalf("failed run must carry the failure in result:, got:\n%s", row)
	}
}
