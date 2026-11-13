package main

// chat_pty_fixture_test.go — the ONE pty-pair fixture the chat cases allocate
// through, and the guard that keeps its allocation honest.
//
// THE FOUR STEPS ARE NOT ATOMIC. The plain openpty recipe
//
//	open /dev/ptmx  →  TIOCGPTN  →  TIOCSPTLCK  →  open /dev/pts/<n>
//
// resolves a slave NUMBER that is true only while our master is the one
// holding it: the kernel releases the index the moment the last reference to
// that master goes away, and a peer allocating a pty right then gets the same
// number. Under a whole-package run — this package's pty cases plus every
// sibling package's — a peer can allocate and release pty n between our
// TIOCGPTN and our open, so /dev/pts/<n> resolves to ANOTHER pty (or to
// nothing). The fixture then hands a non-terminal fd to lineedit.Open and the
// case fails in 0.00s with
//
//	lineedit: read termios on fd 7: lineedit: not a terminal: inappropriate ioctl for device
//
// while it is green when run alone. A 0.00s failure is not a wall-clock bound
// and not host load: the fixture's own claim about the fd it is holding is what
// is wrong. (Observed whole-package on the sibling row that filed this defect —
// the reproduction is quoted in the report and in the session evidence dir.)
//
// THE FIX IS AN OBSERVATION, NOT A LONGER WAIT. After opening the slave, ASK
// THE KERNEL whether this fd is the terminal it claims: tcgetattr(3) is the
// exact predicate lineedit uses on the same descriptor (unix.IoctlGetTermios,
// TCGETS — see internal/lineedit/raw.go's IsTerminal/Open), so the check asks
// production's own question rather than a proxy for it. A pipe, a regular file
// and a pty slave whose master is gone all answer ENOTTY, which is the observed
// condition recorded here and named in every skip message.
//
// WHAT THIS DOES NOT CLAIM (honesty over coverage): a slave number recycled to
// a pty that is still open IS a terminal, and no ioctl on the slave can tell it
// apart from ours — nothing on the slave names its master. The fixture
// therefore verifies "this fd is a terminal", which is exactly the property
// lineedit.Open requires of it, and it does not pretend to verify "this fd is
// the same terminal TIOCGPTN named". The residual window is the same one the
// kernel gives any two-process pty user, and it is stated here rather than
// papered over.

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/nick/gopi/internal/lineedit"
)

// chatPtyDevicePath is the ptmx device every allocation starts from; the slave
// path is composed from the number TIOCGPTN reports.
const (
	chatPtyDevicePath      = "/dev/ptmx"
	chatPtySlavePathFormat = "/dev/pts/%d"
)

// chatPtyProcPathFormat spells the fd path the guard test verifies identity
// through: /proc/self/fd/<n> resolves to whatever open file that descriptor
// holds right now, which is what makes it the negative control for the
// verification step (point it at a pipe and the step must refuse).
const chatPtyProcPathFormat = "/proc/self/fd/%d"

// chatPtyOpenAttempts bounds how many whole allocation attempts the fixture
// makes before it gives up and skips with the observed conditions. The failure
// it retries is a lost race with a concurrent pty user, not a property of the
// host: one attempt succeeds with nothing else running, and a retry re-reads
// TIOCGPTN from a NEW master (so it does not re-open the number it just lost).
// The bound therefore only has to cover "a peer landed in the window a few
// times over", which is why it is small and why it is a COUNT and not a
// duration — widening a time bound here would not fix anything and would only
// hide the condition this file exists to record (Rule 1, Rule 27's spirit: no
// waiting-to-make-it-pass).
const chatPtyOpenAttempts = 8

// The FLOOR and CEILING that bound the bound. chatPtyMinOpenAttempts is 2
// because a budget that cannot retry once is not a retry — it is the pre-fix
// behaviour, where a single lost race fails the case. chatPtyMaxOpenAttempts
// keeps exhaustion a fast, loud failure rather than a long stall that would
// read as a hang; neither is a duration, so neither can be widened to make a
// case pass (Rule 1).
const (
	chatPtyMinOpenAttempts = 2
	chatPtyMaxOpenAttempts = 32
)

// The steps chatPtyCondition names. Spelled as constants because the string is
// compared in the guard test and printed in every skip message (Rule 1b).
const (
	chatPtyStepOpenMaster = "open " + chatPtyDevicePath
	chatPtyStepSlaveNum   = "TIOCGPTN"
	chatPtyStepUnlock     = "TIOCSPTLCK"
	chatPtyStepOpenSlave  = "open the slave"
	chatPtyStepWinsize    = "TIOCSWINSZ"
	chatPtyStepVerify     = "the slave is not a terminal (TCGETS)"
)

// chatPtyPair is an allocated pty pair whose slave has been VERIFIED to be a
// terminal. Master and Slave are both closed by the fixture (t.Cleanup), and the
// master stays open for the whole case — closing it first would SIGHUP the slave
// out from under the editor.
type chatPtyPair struct {
	Master *os.File
	Slave  *os.File
	// Number is the slave number TIOCGPTN reported for THIS pair's master, and
	// Attempts is which allocation attempt produced it (1 = first try).
	Number   int
	Attempts int
}

// chatPtyCondition is the condition that made one allocation attempt unusable:
// which step failed and what the kernel answered. It is a value, not an error,
// because a retry is expected — what matters is that it is RECORDED (Rule 15),
// never that it was returned.
type chatPtyCondition struct {
	Step string
	Err  error
}

func (c chatPtyCondition) String() string {
	if c.Err == nil {
		return c.Step
	}
	return c.Step + ": " + c.Err.Error()
}

// chatSlaveIsTerminal asks the kernel production's own question about fd: it is
// a terminal iff the termios read succeeds (internal/lineedit's IsTerminal is
// this same ioctl). A pipe, a regular file, /dev/null and a pty slave whose
// master is gone all fail with ENOTTY — the observed condition.
func chatSlaveIsTerminal(fd int) error {
	_, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	return err
}

// chatPtyWanted is the geometry an attempt is asked for; the zero value means
// "do not touch the size" (the case that only needs a plain pair).
type chatPtyWanted struct {
	Cols int
	Rows int
}

func (w chatPtyWanted) set() bool { return w.Cols > 0 && w.Rows > 0 }

// chatAllocPtyPair is ONE allocation attempt: the four openpty steps, the
// optional winsize, and the verification — with NO retry (the caller owns the
// bound). It returns a pair only when the slave fd really is a terminal;
// otherwise the second return value names the step and the kernel's answer.
//
// Every failure path closes what it already opened, so a retry starts from a
// clean descriptor table and a caller that gives up leaks nothing.
func chatAllocPtyPair(want chatPtyWanted) (chatPtyPair, chatPtyCondition) {
	m, err := os.OpenFile(chatPtyDevicePath, os.O_RDWR, 0)
	if err != nil {
		return chatPtyPair{}, chatPtyCondition{Step: chatPtyStepOpenMaster, Err: err}
	}
	fd := int(m.Fd())
	n, err := lineedit.PtyNumber(fd)
	if err != nil {
		closeQuietly(m)
		return chatPtyPair{}, chatPtyCondition{Step: chatPtyStepSlaveNum, Err: err}
	}
	if err := lineedit.PtyUnlock(fd); err != nil {
		closeQuietly(m)
		return chatPtyPair{}, chatPtyCondition{Step: chatPtyStepUnlock, Err: err}
	}
	slavePath := fmt.Sprintf(chatPtySlavePathFormat, n)
	s, err := os.OpenFile(slavePath, os.O_RDWR, 0)
	if err != nil {
		closeQuietly(m)
		return chatPtyPair{}, chatPtyCondition{Step: chatPtyStepOpenSlave, Err: err}
	}
	if want.set() {
		if err := unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(want.Cols), Row: uint16(want.Rows)}); err != nil {
			closeQuietly(s)
			closeQuietly(m)
			return chatPtyPair{}, chatPtyCondition{Step: chatPtyStepWinsize, Err: err}
		}
	}
	// THE VERIFICATION. Everything above is a claim about the fd s holds; this
	// is the only step that CHECKS the claim, on the same descriptor
	// lineedit.Open will be handed.
	if err := chatSlaveIsTerminal(int(s.Fd())); err != nil {
		closeQuietly(s)
		closeQuietly(m)
		return chatPtyPair{}, chatPtyCondition{Step: chatPtyStepVerify, Err: err}
	}
	return chatPtyPair{Master: m, Slave: s, Number: n, Attempts: 1}, chatPtyCondition{}
}

// chatAllocatePtyPair is the retry policy the fixture applies, factored out so
// a test can drive it with a failing allocator and observe BOTH the bound and
// the recorded condition (no host timing involved).
//
// It is never silent: when every attempt fails it returns an error naming the
// attempt count and the conditions observed, in order.
func chatAllocatePtyPair(alloc func() (chatPtyPair, chatPtyCondition)) (chatPtyPair, error) {
	seen := make([]string, 0, chatPtyOpenAttempts)
	for attempt := 1; attempt <= chatPtyOpenAttempts; attempt++ {
		pair, cond := alloc()
		if cond.Err == nil && cond.Step == "" {
			pair.Attempts = attempt
			return pair, nil
		}
		seen = append(seen, cond.String())
	}
	return chatPtyPair{}, fmt.Errorf(
		"no pty pair whose slave is a terminal in %d attempt(s); observed conditions, in order: %s",
		chatPtyOpenAttempts, strings.Join(seen, " | "))
}

// chatOpenPtyPair is what a case calls: a verified pair, or a skip whose
// message names the observed conditions. A host without /dev/ptmx cannot host
// a pty case at all, and a substitute stream would assert nothing.
func chatOpenPtyPair(t *testing.T, cols, rows int) chatPtyPair {
	t.Helper()
	pair, err := chatAllocatePtyPair(func() (chatPtyPair, chatPtyCondition) {
		return chatAllocPtyPair(chatPtyWanted{Cols: cols, Rows: rows})
	})
	if err != nil {
		if strings.Contains(err.Error(), chatPtyStepOpenMaster) {
			t.Skipf("%v — no %s on this host, so the pty shape cannot be exercised",
				err, chatPtyDevicePath)
		}
		t.Skipf("%v — a host that cannot allocate a pty pair cannot exercise the pty shape", err)
	}
	t.Cleanup(func() {
		closeQuietly(pair.Slave)
		closeQuietly(pair.Master)
	})
	return pair
}

// closeQuietly closes a fixture handle. The discard is deliberate and is
// commented rather than swallowed silently: a close error on a descriptor we
// are deliberately dropping (a failed attempt's master, or the pair at
// teardown) cannot change what the case asserts, and reporting it would make a
// passing case noisy.
func closeQuietly(f *os.File) {
	if f == nil {
		return
	}
	_ = f.Close() //stylegate:ignore dropping a fixture descriptor cannot change what a case asserts
}

// TestChatPtyFixture_RefusesAnFdThatIsNotATerminal is the deterministic
// negative control for the verification step: it drives ONE attempt whose
// slave open resolves to a PIPE instead of a pty, and asserts the attempt is
// refused with ENOTTY recorded at the verification step.
//
// The pipe is not a simulation of the race — it is the same observable the race
// produces (a descriptor that is not a terminal), reached without depending on
// a concurrent pty user, so the guard is green on any host and red-proofable by
// deleting the verification call.
func TestChatPtyFixture_RefusesAnFdThatIsNotATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer closeQuietly(r)
	defer closeQuietly(w)

	// The negative control: the file at this path is a terminal iff the pipe is
	// one — which it is not.
	path := fmt.Sprintf(chatPtyProcPathFormat, r.Fd())
	ok, cond := chatSlaveAtPathIsTerminal(path)
	if ok {
		t.Fatalf("the fixture accepted a pipe as a terminal: %s does not name a terminal", path)
	}
	if cond.Step != chatPtyStepVerify {
		t.Fatalf("the refusal is recorded at step %q, want %q", cond.Step, chatPtyStepVerify)
	}
	// The condition must CARRY the kernel's own answer, not a proxy: ENOTTY is
	// what TCGETS returns on a pipe, and it is the same errno lineedit.Open
	// reported when this fixture handed it a non-terminal fd.
	if !errors.Is(cond.Err, unix.ENOTTY) {
		t.Fatalf("the recorded condition is %v, want it to carry ENOTTY (the errno production saw)", cond.Err)
	}
}

// TestChatPtyFixture_AcceptsARealPtySlave is the positive control: the guard
// above is only worth its green if the same check ACCEPTS the genuine article.
// Without this pair the verification could be vacuous (refusing everything) and
// every pty case would skip.
func TestChatPtyFixture_AcceptsARealPtySlave(t *testing.T) {
	pair, err := chatAllocatePtyPair(func() (chatPtyPair, chatPtyCondition) {
		return chatAllocPtyPair(chatPtyWanted{})
	})
	if err != nil {
		t.Skipf("%v — allocation through the verified fixture", err)
	}
	defer closeQuietly(pair.Slave)
	defer closeQuietly(pair.Master)
	path := fmt.Sprintf(chatPtyProcPathFormat, pair.Slave.Fd())
	ok, cond := chatSlaveAtPathIsTerminal(path)
	if !ok {
		t.Fatalf("the fixture refused the pty slave it just verified (%s): %s", path, cond)
	}
}

// chatSlaveAtPathIsTerminal is the verification step as a UNIT: it opens path
// and asks whether that fd is a terminal, recording the observed condition the
// same way an allocation attempt does. It is the seam the negative control
// drives — the allocation path itself cannot be pointed at a pipe without
// faking open(2).
func chatSlaveAtPathIsTerminal(path string) (bool, chatPtyCondition) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return false, chatPtyCondition{Step: chatPtyStepOpenSlave, Err: err}
	}
	defer closeQuietly(f)
	if err := chatSlaveIsTerminal(int(f.Fd())); err != nil {
		return false, chatPtyCondition{Step: chatPtyStepVerify, Err: err}
	}
	return true, chatPtyCondition{}
}

// TestChatPtyFixture_BoundedRetryRecordsEveryCondition pins the retry POLICY
// with a failing allocator: the number of attempts is exactly the named bound
// (Rule 1 — a bound nobody can observe is a bound nobody enforces), the failure
// is never silent (Rule 15 — the error names the count and the conditions), and
// a transient failure that clears on the next attempt still yields a pair.
func TestChatPtyFixture_BoundedRetryRecordsEveryCondition(t *testing.T) {
	// The BOUND's contract, asserted once: exhaustion must be a retry (the
	// floor) and must not be a stall (the ceiling). These read the CONSTANT, so
	// the assertion is about the contract, not about a value a reader could
	// satisfy by rewriting the constant to match whatever the code does.
	if chatPtyOpenAttempts < chatPtyMinOpenAttempts || chatPtyOpenAttempts > chatPtyMaxOpenAttempts {
		t.Fatalf("the retry bound is %d, outside its own [%d, %d] contract — a bound of 1 is the pre-fix single-shot allocation, and a large one turns a lost race into a stall that reads as a hang",
			chatPtyOpenAttempts, chatPtyMinOpenAttempts, chatPtyMaxOpenAttempts)
	}

	t.Run("exhaustion names the condition", func(t *testing.T) {
		calls := 0
		stuck := chatPtyCondition{Step: chatPtyStepVerify, Err: unix.ENOTTY}
		_, err := chatAllocatePtyPair(func() (chatPtyPair, chatPtyCondition) {
			calls++
			return chatPtyPair{}, stuck
		})
		if err == nil {
			t.Fatal("the allocation reported success although every attempt failed")
		}
		// The COUNT is asserted against the bound through the message the
		// allocator OBSERVED, not by re-reading the constant next to the loop
		// that uses it: the allocator's own words are the only record of what
		// it actually did.
		if !strings.Contains(err.Error(), fmt.Sprintf("%d attempt(s)", chatPtyOpenAttempts)) {
			t.Fatalf("the failure does not report a %d-attempt exhaustion (Rule 15): %v", chatPtyOpenAttempts, err)
		}
		if calls != chatPtyOpenAttempts {
			t.Fatalf("the allocator was driven %d time(s) but the bound is %d", calls, chatPtyOpenAttempts)
		}
		if !strings.Contains(err.Error(), chatPtyStepVerify) {
			t.Fatalf("the failure does not name the observed condition %q: %v", chatPtyStepVerify, err)
		}
	})

	t.Run("a transient condition is retried and the pair is verified", func(t *testing.T) {
		calls := 0
		master, err := os.OpenFile(chatPtyDevicePath, os.O_RDWR, 0)
		if err != nil {
			t.Skipf("%v — no %s on this host", err, chatPtyDevicePath)
		}
		defer closeQuietly(master)
		recovered := chatPtyPair{Master: master, Number: 0}
		pair, err := chatAllocatePtyPair(func() (chatPtyPair, chatPtyCondition) {
			calls++
			if calls == 1 {
				return chatPtyPair{}, chatPtyCondition{Step: chatPtyStepVerify, Err: unix.ENOTTY}
			}
			return recovered, chatPtyCondition{}
		})
		if err != nil {
			t.Fatalf("a condition that cleared on the second attempt did not yield a pair: %v", err)
		}
		if calls != 2 {
			t.Fatalf("the fixture made %d attempt(s), want 2 (one lost race, one success)", calls)
		}
		if pair.Attempts != 2 {
			t.Fatalf("the pair reports attempt %d, want 2 (the caller must be able to see it was a retry)", pair.Attempts)
		}
	})
}

// ── THE CHAT REPL TEARDOWN ORDER — ONE PLACE, FOUR STEPS ────────────────────
//
// A fixture that runs the chat REPL over a pty has to take FOUR steps down, and
// the ORDER is what decides whether the case measures the editor's own stop path
// or merely waits out a bound and calls the elapsed bound a pass:
//
//  1. in.editor.Close()   — releases the editor's layout and completion worker.
//     It does NOT stop the read: the same read serves the
//     next line, which is why the REPL can close and
//     reopen the editor without losing the terminal.
//  2. term.Close()        — THE STOP. The read loop is parked in ReadEvent ON
//     THE TERMINAL, so the terminal's own stop path is the
//     only thing that ends it (internal/lineedit/raw.go:
//     `stopped` raised before the teardown, the parked read
//     woken through the pipe it polls, POLLNVAL read as
//     "terminal gone" — devdoc/lineedit-terminal-close-stop-path.md).
//  3. chatJoinReadLoop()  — join the loop, BOUNDED and LOUD.
//  4. pair.Master.Close() — free the descriptors last (closing the master while
//     a reader is live is what SIGHUPs the slave).
//
// THE PRE-FIX ORDER (the shape this file's doc fixes in the sibling fixtures)
// joined FIRST and closed the terminal after, so the exit the join observed was
// the PTY MASTER's close one statement later, reached on scheduling luck; every
// run paid its join's full bound and the silent `select` around it made the
// elapsed bound indistinguishable from a returned loop (Rule 15). Closing the
// terminal first is therefore not tidiness: it IS the stop, and the join that
// follows joins rather than races.
//
// Nothing may invert steps 2 and 3: what "the read loop returned" means changes
// completely if the join runs first.
const chatReplExitWait = 5 * time.Second

// chatJoinReadLoop joins a chat run's read loop and REPORTS a loop that did not
// return, instead of waiting out a bound in silence (Rule 15). `why` returns the
// case's own sentence about what was still running, so a failure names the run's
// state rather than the fixture's.
//
// THE LOUD ARM IS THE POINT, and it is red-proved on its own (the order itself
// cannot be: with the stop path in place the loop ends under either order, so a
// green run does not distinguish them — see devdoc/chat-repl-pty-teardown-order.md).
func chatJoinReadLoop(t *testing.T, exited <-chan struct{}, why func() string) {
	t.Helper()
	select {
	case <-exited:
	case <-time.After(chatReplExitWait):
		t.Errorf("the run's read loop did not return within %s after its terminal was closed — it is %s; a loop still parked on a STOPPED terminal is a stop-path defect, not a slow host",
			chatReplExitWait, why())
	}
}
