package testsuite

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/testutil"
	"golang.org/x/sys/unix"
)

// The PTY harness's own bounds. Every value is named (Rule 1); the two timing
// values mirror internal/lineedit's windows by REFERENCE (never by a copied
// literal), so the harness cannot drift away from the policy it is testing.
const (
	// ptyPromptTimeout bounds the wait for the child's prompt. A PTY read has no
	// natural deadline, so the harness imposes one (a test deadline, never a
	// production timer).
	ptyPromptTimeout = 60 * time.Second
	// ptyReadBuf is the read buffer for one PTY drain.
	ptyReadBuf = 4096
	// ptyTypeGap separates keystrokes that must NOT look like a paste burst: an
	// order of magnitude above the burst's own interval.
	ptyTypeGap = 300 * time.Millisecond
	// ptyPostBurstGap is how long the harness waits after a burst before sending
	// the submit Enter: an order of magnitude above the editor's Enter-suppression
	// window, so the submit is unambiguously the user's own Enter.
	ptyPostBurstGap = 1 * time.Second
	// ptyQuitTimeout bounds the wait for the child to exit after /quit.
	ptyQuitTimeout = 60 * time.Second
	// ptyDrainStopTimeout bounds how long stop() waits for the drain goroutine to
	// return once the child is killed and the master closed. It is SHORT on
	// purpose: a master read can only end because the child is gone, so waiting
	// longer buys nothing — and a drain that still has not returned is REPORTED,
	// never hidden (Rule 15).
	ptyDrainStopTimeout = 5 * time.Second
	// ptyResizeCols / ptyResizeRows are the geometry the resize test installs.
	// Both must DIFFER from each other and stay small enough that the editor's
	// bottom region still fits; the point is only that a real SIGWINCH arrives.
	ptyResizeCols = 100
	ptyResizeRows = 30
	// ptyResizeSettled is how long the harness waits after the resize before
	// typing again. The editor redraws on the signal, and a keystroke that lands
	// mid-redraw would be tested as a race rather than as the freeze this test
	// exists to catch.
	ptyResizeSettled = 300 * time.Millisecond
	// chatPromptMark is the marker the harness waits for the prompt by. It is the
	// prompt's visible text WITHOUT its trailing blank: the renderer trims a
	// row's trailing cells and positions the cursor with a CSI instead (the cell
	// grid the editor draws shows "you>" at columns 0-3 and the cursor at column
	// 5), so a trailing space never appears in the byte stream. A literal is used
	// because the prompt lives in package main; the cmd/agent-side grid test
	// asserts the rendered row against the real chatPrompt constant.
	chatPromptMark = "you>"
)

// burstGap is the paste-burst spacing the harness uses when it feeds a "pasted"
// block: it must be BELOW the editor's PasteBurstCharInterval, which is why it is
// derived from that policy value rather than spelled.
var burstGap = lineedit.PasteBurstCharInterval / 2

// ptySession drives a gopi child on a real pseudo-terminal: the slave end is
// passed as stdin/stdout/stderr together with Setsid+Setctty, so the child's
// stdin IS a character device with termios and the raw-mode editor path runs. It
// is the only layer the scripted editor tests cannot cover.
type ptySession struct {
	cmd    *exec.Cmd
	master *os.File
	rx     chan byte
	// done is closed when drain() returns — the ONE reading that says the read
	// goroutine is gone. Without it a leaked session is INVISIBLE: the case
	// finishes and the goroutine stays parked for the rest of the binary's life
	// (measured on this suite: seven masters still open, six `gopi chat`
	// children still running, one test binary blocked in -test.count=2).
	done chan struct{}
	// stopc is closed by stop(). It is what a drain parked on the BOUNDED send
	// (rx full, nobody reading any more) is waiting for, so teardown never
	// depends on a reader appearing.
	stopc chan struct{}
	// stopOnce makes the teardown idempotent: a case that already ran close()
	// still gets its t.Cleanup, and neither path may close stopc twice.
	stopOnce sync.Once
	// waitOnce/waitDone/waitErr are the ONE cmd.Wait for this child, shared by
	// close() and stop(). Go allows exactly one Wait per process, so a teardown
	// that raced the case's own close() would get "Wait was already called"
	// instead of a status. Reaping on the stop path is also what keeps a case
	// that never reached close() from leaving a ZOMBIE behind (measured on the
	// baseline run: one `[gopi] <defunct>` per such case).
	waitOnce sync.Once
	waitDone chan struct{}
	waitErr  error
}

// reap starts (at most once) the goroutine that waits for the child and returns a
// channel closed when its status is known. p.waitErr is readable after it closes.
func (p *ptySession) reap() <-chan struct{} {
	p.waitOnce.Do(func() {
		p.waitDone = make(chan struct{})
		go func() {
			p.waitErr = p.cmd.Wait()
			close(p.waitDone)
		}()
	})
	return p.waitDone
}

// openPty opens a PTY pair and resolves the slave's device path.
func openPty(t *testing.T) (master *os.File, slavePath string) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skip("no /dev/ptmx on this host: the raw-mode editor cannot be exercised without a pty")
	}
	fd := int(master.Fd())
	n, err := lineedit.PtyNumber(fd)
	if err != nil {
		_ = master.Close() //stylegate:ignore closing the master on the skip path
		t.Skip("TIOCGPTN unavailable: the kernel does not expose pty slave numbering here")
	}
	if err := lineedit.PtyUnlock(fd); err != nil {
		_ = master.Close() //stylegate:ignore closing the master on the skip path
		t.Skip("TIOCSPTLCK unavailable: the kernel does not allow unlocking the pty slave")
	}
	return master, fmt.Sprintf("/dev/pts/%d", n)
}

// startPTYChat launches `gopi chat` on a fresh PTY.
func startPTYChat(t *testing.T, args []string, env []string, dir string) *ptySession {
	t.Helper()
	master, slavePath := openPty(t)
	slave, err := os.OpenFile(slavePath, os.O_RDWR, 0)
	if err != nil {
		_ = master.Close() //stylegate:ignore closing the master on the skip path
		t.Skip("cannot open the pty slave device: this host exposes no usable pty pair")
	}
	cmd := exec.Command(testutil.GopiBin(t), args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		_ = slave.Close()  //stylegate:ignore closing both ends on the fatal path
		_ = master.Close() //stylegate:ignore closing both ends on the fatal path
		t.Fatalf("start gopi chat on a PTY: %v", err)
	}
	_ = slave.Close() //stylegate:ignore the child holds its own descriptor for the slave
	return newPTYSession(t, cmd, master)
}

// newPTYSession binds a started child to a pty session: it builds the session,
// starts the drain goroutine and REGISTERS ITS OWN TEARDOWN.
//
// THE TEARDOWN IS REGISTERED HERE, NOT LEFT TO EACH CASE, and that is the fix.
// A case that fails mid-turn never reaches its own close(), and then nothing
// killed the child and nothing could stop the drain goroutine. Measured on this
// suite (2026-11-10, `-run TestChatPTY -count=2`): SEVEN ptmx descriptors still
// held by the test binary and SIX `gopi chat` children still alive long after
// their tests had reported — several of them from cases that never call close()
// at all (the banner / band-clean / surface-fact trio). t.Cleanup runs on every
// exit path including t.Fatalf, so the harness owns its own lifetime instead of
// trusting each case to be well behaved on the way out.
//
// It is the ONE constructor for the three independent harnesses in this package
// (startPTYChat, startPtyChild, startPTYChatStderrFile): before it, each one
// spelled its own `&ptySession{...}` and each one therefore had to remember its
// own cleanup — and two of the three did not.
func newPTYSession(t *testing.T, cmd *exec.Cmd, master *os.File) *ptySession {
	t.Helper()
	p := &ptySession{
		cmd:    cmd,
		master: master,
		rx:     make(chan byte, ptyReadBuf),
		done:   make(chan struct{}),
		stopc:  make(chan struct{}),
	}
	go p.drain()
	t.Cleanup(p.stop)
	return p
}

// drain copies the master's output until it fails (the child is gone) or until
// stop() tears the session down.
//
// THE MASTER READ ITSELF CANNOT BE CANCELLED, and that is MEASURED, not assumed
// (probe_deadline_read.go in this run's session dir): a master opened with
// os.OpenFile stays in BLOCKING mode, so internal/poll never parks it on the
// netpoll — SetReadDeadline returns nil and the pending read IGNORES it, and
// closing the *os.File does not unblock a read already inside syscall.Read
// either (measured: still blocked 3s after Close). The only thing that ends this
// read is the peer going away: the slave side closing, i.e. the CHILD exiting.
// So the child's death is part of the teardown contract (stop kills it), and the
// send below must never be the thing that wedges — a drain blocked on rx while
// nobody reads it holds the goroutine AND the child's write(2) forever, which is
// exactly the stack `panic: test timed out` printed at chat_pty_test.go:123.
func (p *ptySession) drain() {
	defer close(p.done)
	defer close(p.rx)
	buf := make([]byte, ptyReadBuf)
	for {
		n, err := p.master.Read(buf)
		for i := 0; i < n; i++ {
			select {
			case p.rx <- buf[i]:
			case <-p.stopc:
				// The session was torn down while this byte was still
				// undelivered: nobody will read rx again, so blocking on the
				// send would strand the goroutine. Drop the tail and leave.
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// stop tears the session down: it kills the child this process started, closes
// the master it opened, and waits (bounded) for the drain goroutine to notice.
// It is IDEMPOTENT, so t.Cleanup can run it after a case already called close().
//
// Rule 42: the only process signalled is p.cmd's own, and the only descriptor
// closed is the master this harness itself opened.
func (p *ptySession) stop() {
	p.stopOnce.Do(func() {
		close(p.stopc)
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill() //stylegate:ignore the exit status is no longer meaningful on the teardown path
		}
		_ = p.master.Close() //stylegate:ignore the master is this harness's own descriptor
		// Reap the child this teardown just killed: exactly one Wait per process
		// (shared with close() through reap()), and the only thing that keeps a
		// case which never reached close() from leaving a zombie.
		reaped := p.reap()
		// The read already inside syscall.Read returns only because the child is
		// gone (its slave descriptors closed with it). Both waits are BOUNDED, so
		// nothing here can hang a suite — and a goroutine that still did not
		// return is REPORTED rather than hidden (Rule 15).
		drainTimer := time.NewTimer(ptyDrainStopTimeout)
		defer drainTimer.Stop()
		select {
		case <-p.done:
		case <-drainTimer.C:
			fmt.Fprintf(os.Stderr, "ptySession.stop: the drain goroutine is still parked %s after the child was killed and the master closed (args: %v)\n",
				ptyDrainStopTimeout, p.cmd.Args)
		}
		waitTimer := time.NewTimer(ptyDrainStopTimeout)
		defer waitTimer.Stop()
		select {
		case <-reaped:
		case <-waitTimer.C:
			fmt.Fprintf(os.Stderr, "ptySession.stop: the child %v was not reaped within %s of SIGKILL\n",
				p.cmd.Args, ptyDrainStopTimeout)
		}
	})
}

// write sends bytes to the child as a terminal would.
func (p *ptySession) write(t *testing.T, s string) {
	t.Helper()
	if _, err := p.master.WriteString(s); err != nil {
		t.Fatalf("write to the PTY: %v", err)
	}
}

// writeGapped sends each byte with a pause, so the run is typed, not pasted.
func (p *ptySession) writeGapped(t *testing.T, s string) {
	t.Helper()
	for _, r := range s {
		p.write(t, string(r))
		time.Sleep(ptyTypeGap) //stylegate:allow-sleep keeps keystrokes separated beyond the editor's burst window
	}
}

// writeBurst sends bytes back-to-back — what a terminal does with a paste.
func (p *ptySession) writeBurst(t *testing.T, s string) {
	t.Helper()
	p.write(t, s)
	time.Sleep(burstGap) //stylegate:allow-sleep models the terminal's own paste pacing
}

// waitFor reads the child's output until want appears or the deadline passes. It
// is a synchronization WAIT (it returns the instant the marker arrives), not a
// sleep: the timeout only exists so a hang reports the child's output.
func (p *ptySession) waitFor(t *testing.T, want string, timeout time.Duration) string {
	t.Helper()
	var got strings.Builder
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case b, ok := <-p.rx:
			if !ok {
				t.Fatalf("the PTY closed while waiting for %q; output so far:\n%s", want, got.String())
			}
			got.WriteByte(b)
			if strings.Contains(got.String(), want) {
				return got.String()
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %q; output so far:\n%s", want, got.String())
		}
	}
}

// waitForTurnInFlight waits until the mock has SERVED request n — the one
// deterministic "the turn is running" fact this harness can read.
//
// IT REPLACES A WAIT ON THE STREAMED TEXT, and that replacement is a measured
// repair, not a preference. The old sync was waitFor(t, "fragment-a", …), and on
// this tree that marker only reaches the pty when the answer's stream ENDS: the
// live answer band paints each fragment glyph by glyph with its own cursor and
// colour escapes between them, so the contiguous literal "fragment-a" never
// appears mid-paint. Measured at a361a9d
// (`go test ./testsuite/ -run 'TestZZLandedProfile' -count=1 -v`):
//
//	fragment-a@1.68s … SENTINEL-END-OF-ANSWER@1.68s
//
// — "the stream started" and "the stream finished" are the same instant. A
// harness that waits on it can only aim its key at a turn that is already over,
// which is what the two interrupt acceptance tests did (measured: key written at
// 2.007s, sentinel landing at 1.689s). The mock's request count is true while the
// turn is really in flight (served at ~128ms, sentinel at ~1.685s on the same
// run), so it is the sync those cases use.
func waitForTurnInFlight(t *testing.T, llm *testutil.MockLLM, n int) {
	t.Helper()
	deadline := time.Now().Add(ptyPromptTimeout)
	for time.Now().Before(deadline) {
		if llm.RequestCount() >= n {
			return
		}
		time.Sleep(ptyHoldPollGap) //stylegate:allow-sleep bounded poll of the mock's own request count; it returns the instant the request lands
	}
	t.Fatalf("the mock never served request %d (served=%d): this case needs a turn really in flight", n, llm.RequestCount())
}

// close waits for the child to exit and returns its exit code.
//
// IT DRAINS THE PTY WHILE IT WAITS, and that is not a detail. The child's own
// shutdown writes (the prompt's restoration, the farewell row, the band's final
// frame); if this function only waits on cmd.Wait() and reads nothing, the pty
// kernel buffer fills, the child's last write(2) parks, and the exit NEVER
// happens — which then surfaces as the misleading "chat did not exit within
// 1m0s" instead of as the read starvation that caused it (measured: the delta=1
// case of TestChatEmphasisColourAlignment_OnRealPTY, 2026-10-30).
func (p *ptySession) close(t *testing.T) int {
	t.Helper()
	// ONE Wait per child, shared with stop(): a teardown that races this call
	// must not turn the exit status into "Wait was already called".
	done := p.reap()
	deadline := time.NewTimer(ptyQuitTimeout)
	defer deadline.Stop()
	rx := p.rx
	exited := false
	for !exited || rx != nil {
		select {
		case _, ok := <-rx:
			if !ok {
				// The drain goroutine finished: the master saw its read fail.
				rx = nil
			}
		case <-done:
			exited = true
			done = nil
		case <-deadline.C:
			// ONE teardown for both exit paths: stop() kills THIS child, closes
			// THIS master and waits (bounded) for the drain goroutine. The
			// goroutine used to be left behind here — the failure path killed
			// the process and returned with drain still parked in read(2), and
			// with the rx producer still holding a master nobody could close.
			p.stop()
			t.Fatalf("chat did not exit within %s", ptyQuitTimeout)
		}
	}
	// The child is gone, so the master's read fails on its own; stop() is what
	// CLOSES the descriptor, and it is idempotent against the t.Cleanup
	// registration startPTYChat already made.
	p.stop()
	if p.waitErr == nil {
		return 0
	}
	if ee, ok := p.waitErr.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	t.Fatalf("chat process: %v", p.waitErr)
	return 1
}

// pumpFor READS the child's output for up to d and appends it to buf. It reports
// whether want (when non-empty) is now present in buf.
//
// WHY THIS EXISTS — the sleep-polling trap. `startPTYChat` drains the master on a
// goroutine that forwards into the bounded channel p.rx (ptyReadBuf). A caller
// that waits with time.Sleep and only inspects an artifact through it therefore
// reads NOTHING from rx: the channel fills, the drain goroutine blocks on its
// send, nobody calls read(master), the pty's kernel buffer fills and the child's
// write(2) freezes mid-answer. The symptom is an artifact whose counter stops, a
// tail marker that never lands and a shutdown wait that times out — three
// misleading names for one cause. A wait that polls anything must keep consuming
// the pty, and this is the ONE helper that does so (measured 2026-10-23 and
// 2026-10-30: delta=1 red at 98s, green at 23s once the wait consumed rx).
func (p *ptySession) pumpFor(buf *strings.Builder, want string, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case b, ok := <-p.rx:
			if !ok {
				// The child is gone and the drain goroutine has closed the
				// stream: wait out the interval instead of re-selecting on a
				// closed channel (a hot spin would burn the wait budget and
				// report nothing).
				<-timer.C
				return want != "" && strings.Contains(buf.String(), want)
			}
			buf.WriteByte(b)
			if want != "" && strings.Contains(buf.String(), want) {
				return true
			}
		case <-timer.C:
			return want != "" && strings.Contains(buf.String(), want)
		}
	}
}

// resize delivers a REAL SIGWINCH to the child the way a terminal emulator
// does: set the kernel's window size on the pty, which makes the tty layer
// signal the foreground process group. Nothing here sends the signal directly —
// a hand-raised SIGWINCH would not prove the path a user's resize takes, and
// the production bug this test pins lived in that path.
func (p *ptySession) resize(t *testing.T, cols, rows uint16) {
	t.Helper()
	if err := unix.IoctlSetWinsize(int(p.master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: cols, Row: rows}); err != nil {
		t.Fatalf("set the pty window size to %dx%d: %v", cols, rows, err)
	}
}

// TestChatPTY_ResizeDoesNotFreezeTheEditor pins the M1 defect that only a real
// signal could reach: the resize self-pipe was drained on a BLOCKING descriptor,
// so the FIRST SIGWINCH parked the reader in pipe_read forever — the editor went
// deaf and every later keystroke was lost, while no test noticed because none
// raised the signal. The assertion is therefore behavioural and end-to-end: after
// a real resize, typing must still submit a turn. A frozen reader fails on the
// wait, not on an internal callback count.
func TestChatPTY_ResizeDoesNotFreezeTheEditor(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: "answer after resize."})
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat"}, testutil.MockEnv(t, home, llm.URL()), work)
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	chat.resize(t, ptyResizeCols, ptyResizeRows)
	time.Sleep(ptyResizeSettled) //stylegate:allow-sleep lets the resize redraw finish before typing, so the keystroke tests the drain and not a redraw race

	chat.writeGapped(t, "typed after a resize")
	chat.write(t, "\r")
	out := chat.waitFor(t, "answer after resize.", ptyPromptTimeout)
	if !strings.Contains(out, chatPromptMark) {
		t.Fatalf("the prompt was never re-rendered after the resize; output:\n%s", out)
	}

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0", code)
	}

	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	sess.AssertCounts(t, map[string]int{"user": 1, "assistant": 1, "session_end": 1})
	if got := sess.UserContents(); len(got) != 1 || got[0] != "typed after a resize" {
		t.Fatalf("user messages = %v, want the line typed after the resize", got)
	}
}

// TestChatPTY_FailedTurnKeepsThePrompt is the reported bug (01a0d513) on the
// carrier it was reported from: a REAL terminal, the raw-mode editor, a
// tool-enabled turn whose model loops until the bounded ReAct cap.
//
// What the operator saw before the fix: the turn's work scrolled by, then the
// process was gone — no message, and the input prompt still on screen. So the
// assertion here is the two facts that were missing, both of them about the
// SCREEN: the failure is NAMED, and the `you>` prompt comes BACK so the next turn
// can be typed. A test that only checked the exit code would have passed on the
// broken binary (the exit code was a defensible 1; what was wrong was the silence
// and the death of the session).
func TestChatPTY_FailedTurnKeepsThePrompt(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	// The always-looping model: 40 tool rounds (the cap), then — for the NEXT
	// turn — a plain answer. The mock repeats its last entry past the script, so
	// the tail is what the surviving session's next turn receives.
	script := make([]testutil.Resp, 0, 41)
	for i := 0; i < 40; i++ {
		script = append(script, testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   fmt.Sprintf("call_pty_cap_%02d", i),
			Name: "read",
			Args: map[string]interface{}{"path": "/nonexistent-round-cap-probe"},
		}})
	}
	script = append(script, testutil.Resp{Text: "the session survived the failed turn."})
	llm := testutil.NewMockLLM(t, script...)
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat", "--tools"}, testutil.MockEnv(t, home, llm.URL()), work)
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	chat.writeGapped(t, "loop until the cap")
	chat.write(t, "\r")
	// The failure must be NAMED on the screen. The notice is committed row by
	// row (the band wraps it), so the wait is for its LAST fragment and the
	// assertions read the buffer that has by then accumulated all of it.
	out := chat.waitFor(t, "session is still open", ptyPromptTimeout)
	if !strings.Contains(out, "turn failed:") {
		t.Fatalf("the failed turn was not named on the terminal; screen output:\n%s", out)
	}
	if !strings.Contains(out, "tool rounds") {
		t.Fatalf("the failure notice does not say what the cap was; screen output:\n%s", out)
	}

	// And the prompt really came back: the next turn is accepted and answered.
	chat.writeGapped(t, "second turn")
	chat.write(t, "\r")
	out2 := chat.waitFor(t, "the session survived the failed turn.", ptyPromptTimeout)
	if !strings.Contains(out2, chatPromptMark) {
		t.Fatalf("the prompt was not re-rendered after the failed turn; output:\n%s", out2)
	}

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0 — the session ends at the deliberate /quit", code)
	}

	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	if code := sess.SessionEndExitCode(); code != 0 {
		t.Errorf("session_end exit_code = %d, want 0", code)
	}
	// Both turns are in ONE session, and the failed turn is the first of them.
	got := sess.UserContents()
	if len(got) != 2 || got[0] != "loop until the cap" || got[1] != "second turn" {
		t.Fatalf("user messages = %v, want both turns recorded in the surviving session", got)
	}
}

// TestChatPTY_EditorRendersAndSubmits drives the raw-mode editor on a REAL
// pseudo-terminal. What only this test can prove: the editor binds to a terminal
// (raw mode entered, bracketed paste announced), the prompt+typed line are
// RENDERED (rule 10.2 — the assertion is on the drawn screen), a typed Enter
// submits, and the session ends cleanly with one user turn.
func TestChatPTY_EditorRendersAndSubmits(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: "pty answer."})
	defer llm.Close()

	// --verbose is what makes the banner appear (cmd/agent/cli_chat.go gates it
	// there, and its own lineedit test pins the same gate on a non-TTY). The
	// editor itself needs no flag: the bracketed-paste announcement below is
	// emitted either way, so this test would otherwise be asserting the flag,
	// not the terminal.
	chat := startPTYChat(t, []string{"chat", "--verbose"}, testutil.MockEnv(t, home, llm.URL()), work)
	out := chat.waitFor(t, chatPromptMark, ptyPromptTimeout)
	if !strings.Contains(out, "line editor active") {
		t.Fatalf("the editor did not announce itself on a TTY; output so far:\n%s", out)
	}
	// The announcement that needs NO flag: raw mode is on and bracketed paste is
	// enabled — the terminal state that only the editor path installs.
	if !strings.Contains(out, "\x1b[?2004h") {
		t.Fatalf("the raw-mode editor did not enable bracketed paste on the TTY; output so far:\n%s", out)
	}
	// The prompt is DRAWN as a rendered row (never asserted as "we wrote bytes").
	if !strings.Contains(out, chatPromptMark) {
		t.Fatalf("the prompt %q was never rendered; output so far:\n%s", chatPromptMark, out)
	}

	chat.writeGapped(t, "hello from a pty")
	chat.write(t, "\r")
	chat.waitFor(t, "pty answer.", ptyPromptTimeout)

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0", code)
	}

	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	sess.AssertCounts(t, map[string]int{"user": 1, "assistant": 1, "session_end": 1})
	if got := sess.UserContents(); len(got) != 1 || got[0] != "hello from a pty" {
		t.Fatalf("user messages = %v, want the one typed line", got)
	}
}

// TestChatPTY_PasteBurstIsOneTurn is the A3 defect end to end on a terminal: a
// 3-line block pasted as ONE burst must produce exactly ONE model request (and
// one 3-line user message), where the cooked reader produced one per line. The
// mock's request count is the assertion — it counts model turns, which is exactly
// what the defect multiplied.
func TestChatPTY_PasteBurstIsOneTurn(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: "paste acknowledgement."})
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat"}, testutil.MockEnv(t, home, llm.URL()), work)
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	// The pasted block, trailing newline included, as one burst.
	chat.writeBurst(t, "line one\nline two\nline three\n")
	// Wait out the Enter-suppression window, then send the user's own Enter.
	time.Sleep(ptyPostBurstGap) //stylegate:allow-sleep waits out the editor's Enter-suppression window so the submit is unambiguous
	chat.write(t, "\r")
	chat.waitFor(t, "paste acknowledgement.", ptyPromptTimeout)

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0", code)
	}

	if n := llm.RequestCount(); n != 1 {
		t.Fatalf("the 3-line paste produced %d model requests, want exactly 1 — a pasted block must be ONE turn", n)
	}
	sess := testutil.LoadSession(t, home)
	sess.AssertCounts(t, map[string]int{"user": 1, "assistant": 1, "session_end": 1})
	got := sess.UserContents()
	if len(got) != 1 {
		t.Fatalf("user messages = %v, want exactly one", got)
	}
	for _, want := range []string{"line one", "line two", "line three"} {
		if !strings.Contains(got[0], want) {
			t.Fatalf("the pasted user message %q is missing %q", got[0], want)
		}
	}
	if n := strings.Count(got[0], "\n"); n != 2 {
		t.Fatalf("the pasted user message carries %d separating newlines, want 2 (three lines): %q", n, got[0])
	}
}

// ptyLiveSentWord / ptyLiveSentMark / ptyLiveIdleWord mirror the chat status
// row's live hint: the in-flight state word and the same word with one keepalive
// animation cell behind it (chat_stream.go's statusLiveSent /
// statusLiveInFlightGap / statusLiveAnimationChar / statusLiveIdle). They are
// spelled HERE because this suite drives the SHIPPED BINARY on a pty and cannot
// see a `package main` constant — the same reason chatPromptMark is spelled here.
const (
	ptyLiveSentWord = "sent"
	// ptyLiveSentMark is the state word with the field gap behind it. The gap is
	// a NO-BREAK SPACE (U+00A0, two bytes on the wire), not an ASCII space: the
	// row's own StatusSep is the field boundary the layout splits on, so a field
	// VALUE carrying one would be cut in half — the reason the gap is spelled
	// with a different character (see chat_stream.go's statusLiveInFlightGap).
	ptyLiveSentMark = ptyLiveSentWord + "\u00a0"
	ptyLiveIdleWord = "ready"
)

// ptyInFlightMark is the marker the harness waits for to know the turn has been
// ISSUED and its keepalive has repainted the row. The bare state word would not
// be enough — the row says "sent" the instant the state goes up and the
// animation's FIRST frame is that bare word — so the marker is the move OFF it:
// either animation cell behind the state. What it proves is exactly the operator's
// defect: the row was repainted WHILE the model was still being waited on, with no
// keystroke typed after the Enter.
const ptyInFlightMark = "sent\u00a0."

// TestChatPTY_LiveHintShowsTheTurnInFlight is the end-to-end check of the
// operator's Defect A on a real pseudo-terminal: while a turn is in flight, the
// STATUS ROW must say so, and it must keep saying so from the read loop's own
// repaints (not from a keystroke the operator happens to type).
//
// It is the surface the operator looked at, so it is driven the way the operator
// drove it: type a message, press Enter, and read the screen — NO keystroke after
// the Enter, because a keystroke is exactly what the defect needed to refresh the
// row before the fix.
func TestChatPTY_LiveHintShowsTheTurnInFlight(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	// The model is HELD mid-stream: the request is accepted, the SSE headers go
	// out, and the mock then blocks — so the turn stays in flight for as long as
	// the assertion needs, which is what makes "the row says in-flight while the
	// model has not answered" a deterministic claim rather than a race against a
	// fast mock. (A held request completes with an EMPTY stream when released —
	// the hold exists to keep the round-trip OPEN, so the assertion after the
	// release is about the row returning to idle, not about an answer text.)
	llm := testutil.NewMockLLM(t, testutil.Resp{Hold: true})
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat"}, testutil.MockEnv(t, home, llm.URL()), work)
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	chat.writeGapped(t, "is the row alive")
	chat.write(t, "\r")

	// The row carries the in-flight state WITH its keepalive animation, and no
	// keystroke follows the Enter.
	out := chat.waitFor(t, ptyInFlightMark, ptyPromptTimeout)
	if !strings.Contains(out, ptyLiveSentWord) {
		t.Fatalf("the status row never said the turn was in flight, with no keystroke typed after Enter; screen output:\n%s", out)
	}

	// Release the held round-trip: the turn completes and the row RETURNS to the
	// idle word — a stale in-flight state outliving its turn would be the mirror
	// of the defect, and it is what the keepalive must not cause.
	llm.ReleaseHold(1)
	chat.waitFor(t, ptyLiveIdleWord, ptyPromptTimeout)

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0 — the session ends at the deliberate /quit", code)
	}
}
