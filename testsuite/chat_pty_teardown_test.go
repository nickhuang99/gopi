package testsuite

// chat_pty_teardown_test.go — the regression guard for the pty harness's LIFETIME
// defect (fixed 2026-11-10):
//
//	the harnesses built a ptySession and started drain(), but registered no
//	teardown of their own — or, in one shape, only a child kill that left the
//	master descriptor open and the drain goroutine parked inside syscall.Read. A
//	case that failed mid-turn therefore never reached its own close(), and the
//	goroutine outlived it for the rest of the test binary's life while its
//	`gopi chat` child kept running.
//
// MEASURED BEFORE THE FIX (`-run TestChatPTY -count=2`, 708.8s, this suite):
// SEVEN `/dev/ptmx` descriptors still held by the test binary after 48 tests had
// reported, SIX `gopi chat` children still alive minutes after their own test
// ended, and one `[gopi] <defunct>`. The whole-package run is what surfaced the
// cost, as `panic: test timed out` pointing at drain's read in chat_pty_test.go.
//
// THE ASSERTIONS ARE PROCESS-LEVEL FACTS, NOT IMPLEMENTATION DETAILS, and that is
// deliberate: they are measured with /proc/self/fd and a goroutine stack scan, so
// the SAME test compiles and runs against the pre-fix harness (where it is RED)
// and against the fixed one (where it is GREEN). A guard that needed the new API
// to compile could not have proved anything about the old code.
//
// THE CHILD IS THE TEST'S OWN (Rule 42): started here, on a pty opened here, and
// the only process signalled is this session's own p.cmd.

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/testutil"
)

const (
	// ptyTeardownChildCommand never exits on its own: the case under test
	// deliberately never closes it, so the only thing that can end it is the
	// harness's own teardown. `exec` replaces the shell so the process that holds
	// the slave IS the one that gets signalled.
	ptyTeardownChildCommand = "exec sleep 3600"
	// ptyTeardownSettle lets the teardown's own effects become observable (a
	// killed child's descriptors close asynchronously with its exit).
	ptyTeardownSettle = 200 * time.Millisecond
	// ptyTeardownAbandonedAnswer is the mock's scripted reply: the abandoned case
	// never submits a turn, so the text is only there because NewMockLLM requires
	// a script.
	ptyTeardownAbandonedAnswer = "abandoned-case answer"
	// ptyTeardownDrainGoroutine is the frame a leaked session's read goroutine
	// shows up as in a runtime stack dump. It is the harness's own function name —
	// the one the whole-package timeout dump pointed at.
	ptyTeardownDrainGoroutine = "(*ptySession).drain("
)

// countPtyMastersInThisProcess reports how many of this process's open file
// descriptors are pty masters. It reads /proc/self/fd, so it measures the REAL
// handle table rather than a bookkeeping counter that could agree with a leak.
func countPtyMastersInThisProcess(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skipf("this host does not expose /proc/self/fd: %v", err)
	}
	n := 0
	for _, e := range entries {
		target, lerr := os.Readlink("/proc/self/fd/" + e.Name())
		if lerr != nil {
			continue // the entry can vanish between ReadDir and Readlink (the fd was just closed)
		}
		if target == ptyMasterDevicePath {
			n++
		}
	}
	return n
}

// liveDrainGoroutines counts the stack frames that name the harness's read
// goroutine. A returned goroutine is gone from the dump, so this is exactly the
// property the fix is about: drain() must not outlive its case.
func liveDrainGoroutines() int {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	stack := string(buf[:n])
	count := 0
	for _, line := range strings.Split(stack, "\n") {
		if strings.Contains(line, ptyTeardownDrainGoroutine) {
			count++
		}
	}
	return count
}

// ptyMasterDevicePath is the name /proc/self/fd reports for an open pty master.
const ptyMasterDevicePath = "/dev/ptmx"

// TestPtySessionTeardownReleasesWhatAnAbandonedCaseWouldLeak is the red proof.
// The inner subtest starts a session and returns WITHOUT closing it — exactly what
// the banner / band-clean / surface-fact cases do — and the outer test then
// measures, from the PROCESS, what the harness did on its own.
//
// Pre-fix this is RED on both counts: the drain goroutine is still parked in
// syscall.Read (holding a master, and holding the child's write(2) with it) and
// the master descriptor is still in the handle table.
func TestPtySessionTeardownReleasesWhatAnAbandonedCaseWouldLeak(t *testing.T) {
	mastersBefore := countPtyMastersInThisProcess(t)
	drainsBefore := liveDrainGoroutines()

	t.Run("abandoned", func(t *testing.T) {
		// The REAL constructor, driven the way the real cases drive it: a chat on
		// a pty, abandoned without close(). (startPtyChild is the other shape and
		// is covered by the idempotence case below; this one is the case that
		// actually leaked — the banner / band-clean / surface-fact shape.)
		home := t.TempDir()
		work := t.TempDir()
		llm := testutil.NewMockLLM(t, testutil.Resp{Text: ptyTeardownAbandonedAnswer})
		defer llm.Close()
		_ = startPTYChat(t, []string{"chat"}, testutil.MockEnv(t, home, llm.URL()), work)
		// NO close(), NO kill, NO /quit: the harness must not depend on this case
		// being well behaved on the way out — that assumption is the defect.
	})

	// The teardown registered by the constructor is synchronous up to a bounded
	// wait, so it has already run by the time t.Run returns; the sleep only lets a
	// killed child's descriptors close (its exit is asynchronous with the kill).
	time.Sleep(ptyTeardownSettle) //stylegate:allow-sleep a killed child's descriptors close with its exit, which is asynchronous with the kill

	if got, want := liveDrainGoroutines(), drainsBefore; got > want {
		t.Fatalf("drain goroutines: %d before the abandoned case, %d after — the read goroutine outlived its case (want no growth); a leaked drain holds one pty master and blocks the child's write(2), which is what makes a whole-package run hang instead of fail",
			drainsBefore, got)
	}
	if got, want := countPtyMastersInThisProcess(t), mastersBefore; got > want {
		t.Fatalf("open pty masters: %d before the abandoned case, %d after — the master descriptor was never closed (want no growth); the measured cost is one fd per abandoned case and, on the baseline run of this suite, seven of them plus six still-running `gopi chat` children",
			mastersBefore, got)
	}
	t.Logf("ABANDONED-CASE TEARDOWN: pty masters %d→%d, drain goroutines %d→%d",
		mastersBefore, countPtyMastersInThisProcess(t), drainsBefore, liveDrainGoroutines())
}

// TestPtySessionStopIsIdempotent pins the other half of the contract: t.Cleanup
// runs even after a case already called close(), so the teardown must be safe a
// second time. A double close of the stop channel would panic on the HAPPY path —
// i.e. on every well-behaved case — so this is worth one cheap assertion rather
// than discovering it in the whole-package run.
func TestPtySessionStopIsIdempotent(t *testing.T) {
	p := startPtyChild(t, "sh", "-c", ptyTeardownChildCommand)
	p.stop()
	p.stop() // and the constructor's own t.Cleanup calls it a third time

	select {
	case <-p.done:
	case <-time.After(ptyDrainStopTimeout):
		t.Fatalf("drain() did not return within %s of stop() — the read goroutine cannot be cancelled by closing the master, so it must be ended by this session's child dying (pid %s)",
			ptyDrainStopTimeout, strconv.Itoa(p.cmd.Process.Pid))
	}
}
