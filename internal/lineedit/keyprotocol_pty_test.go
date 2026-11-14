//go:build linux

package lineedit

// keyprotocol_pty_test.go — the keyboard-protocol negotiation measured over a
// REAL pty pair, where the test acts as the terminal emulator.
//
// WHY A PTY AND NOT A DOUBLE. The subject is a NEGOTIATION: this process writes
// `CSI ? u` to the terminal and decides what it may do NEXT from the answer that
// comes back on stdin. A double would let the test answer its own question, and
// the properties that matter — the push is emitted only after a real reply
// arrived, silence is bounded, and the bytes the operator typed inside the window
// are replayed IN ORDER — are properties of the descriptor and the clock, not of
// an interface. So the test allocates a pty, hands the SLAVE to Open (exactly the
// tty a real program gets, both directions), and drives the MASTER as the
// emulator: it reads what the program emitted and writes what a terminal would
// send back.
//
// The three shapes (Rule 31 — the surface is executed, not reasoned about):
//
//	kitty answers `CSI ? 1 u`  → `CSI > 1 u` goes out, verdict = supported
//	nothing is written back    → no `CSI > 1 u`, verdict = no-reply (bounded)
//	the operator types instead → the bytes are NOT eaten; they come back in order
//
// and the exit shape: after Close the pty receives `CSI < 1 u`, once.

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/sgr"
)

// The bounds the pty cases need. Each is a bound on a scheduling or wire
// detail, never a duration an assertion depends on (Rule 1).
const (
	// keyProtocolPeerWait is how long a case waits for a byte the peer should
	// have received. On a healthy host it is reached only on a FAILURE — the
	// sequence never arrived.
	keyProtocolPeerWait = 3 * time.Second
	// keyProtocolPeerStep is how often that wait re-reads the peer's capture.
	keyProtocolPeerStep = 5 * time.Millisecond
	// keyProtocolQuietWait is how long a case watches for a sequence that must
	// NOT appear. It is bounded the same way and is deliberately short: the
	// probe's own window has already expired by the time it is used, and a
	// sequence that had been written would be on the wire.
	keyProtocolQuietWait = 150 * time.Millisecond
	// keyProtocolSniffBytes bounds one peer read.
	keyProtocolSniffBytes = 256
	// keyProtocolSubmitWait is how long a case waits for a Run it EXPECTS to
	// return. It is reached only on a failure — a submitted line is immediate.
	keyProtocolSubmitWait = 3 * time.Second
	// keyProtocolNoSubmitWait is how long a case watches a Run it expects to stay
	// parked, so "it did not submit" is a measured absence and not a race. It is
	// a multiple of the editor's own paste-suppression window: past that window
	// a return would have happened.
	keyProtocolNoSubmitWait = 5 * PasteEnterSuppressWindow
)

// keyProtocolPeer is the terminal emulator's side of the pty: it records every
// byte the program emitted and writes back whatever the shape under test
// requires.
type keyProtocolPeer struct {
	master *os.File
	mu     sync.Mutex
	seen   bytes.Buffer
	done   chan struct{}
	closed sync.Once

	// replyTo is the sequence the peer answers, and reply is what it writes back
	// ('\r' meaning "carriage return" for a CRLF-dedup case is NOT what this is
	// for: see the cases). reply == nil makes the peer silent.
	replyTo string
	reply   []byte
	// typed is written to the pty IMMEDIATELY after the query is seen, with no
	// reply — the "the operator typed during the window" shape.
	typed []byte
}

// snapshot returns the bytes received so far.
func (p *keyProtocolPeer) snapshot() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]byte(nil), p.seen.Bytes()...)
}

// read runs the emulator's read side until the master is closed.
func (p *keyProtocolPeer) read() {
	buf := make([]byte, keyProtocolSniffBytes)
	answered := false
	for {
		n, err := p.master.Read(buf)
		if n > 0 {
			p.mu.Lock()
			p.seen.Write(buf[:n])
			p.mu.Unlock()
			if !answered && p.replyTo != "" && bytes.Contains(p.snapshot(), []byte(p.replyTo)) {
				answered = true
				if len(p.typed) > 0 {
					// The operator typed first: the bytes go out and NO answer
					// follows, which is the case that must not eat them.
					_, _ = p.master.Write(p.typed) //stylegate:ignore the emulator's own write to the pty it drives
					continue
				}
				_, _ = p.master.Write(p.reply) //stylegate:ignore the emulator's own write to the pty it drives
			}
		}
		if err != nil {
			return
		}
	}
}

// waitForSeq blocks until the emulator has received seq, reporting what it saw.
func (p *keyProtocolPeer) waitForSeq(t *testing.T, seq string) {
	t.Helper()
	deadline := time.Now().Add(keyProtocolPeerWait)
	for {
		if bytes.Contains(p.snapshot(), []byte(seq)) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the program never wrote %q; the pty received %q", seq, p.snapshot())
		}
		time.Sleep(keyProtocolPeerStep) //stylegate:allow-sleep bounded synchronization wait on bytes reaching the pty master
	}
}

// refusesSeq reports whether seq stays absent for the quiet window.
func (p *keyProtocolPeer) refusesSeq(seq string) bool {
	deadline := time.Now().Add(keyProtocolQuietWait)
	for time.Now().Before(deadline) {
		if bytes.Contains(p.snapshot(), []byte(seq)) {
			return false
		}
		time.Sleep(keyProtocolPeerStep) //stylegate:allow-sleep bounded watch for a sequence that must not appear
	}
	return !bytes.Contains(p.snapshot(), []byte(seq))
}

// newKeyProtocolRig allocates a pty, starts the emulator, and returns both.
func newKeyProtocolRig(t *testing.T, peer *keyProtocolPeer) (*Terminal, *keyProtocolPeer) {
	t.Helper()
	master, slave := openInputPendingPTY(t)
	t.Cleanup(func() {
		// deliberately ignored: the fixture's own teardown of a descriptor it owns.
		_ = slave.Close()
		// deliberately ignored: as above.
		_ = master.Close()
	})
	peer.master = master
	peer.done = make(chan struct{})
	go func() {
		defer close(peer.done)
		peer.read()
	}()
	// The program's stdout IS its tty (a real chat run writes the screen to the
	// same descriptor it reads keys from), so the emulator sees every sequence
	// the terminal emits.
	term, err := Open(slave, slave)
	if err != nil {
		t.Fatalf("lineedit.Open on a pty slave: %v", err)
	}
	return term, peer
}

// TestKeyProtocolKittyAnswersAndGetsThePush is the supported path, end to end:
// the query goes out, the emulator answers `CSI ? 1 u`, and ONLY THEN does the
// push `CSI > 1 u` appear on the wire.
func TestKeyProtocolKittyAnswersAndGetsThePush(t *testing.T) {
	peer := &keyProtocolPeer{
		replyTo: sgr.KeyboardProtocolQuerySeq(),
		reply:   []byte(sgr.KeyboardProtocolReplySeq(sgr.KeyboardProtocolDisambiguateEscapeCodes)),
	}
	term, peer := newKeyProtocolRig(t, peer)
	defer func() {
		// deliberately ignored: the case's own teardown; the restore is asserted below.
		_ = term.Close()
	}()

	peer.waitForSeq(t, sgr.KeyboardProtocolQuerySeq())
	peer.waitForSeq(t, sgr.KeyboardProtocolPushSeq(sgr.KeyboardProtocolDisambiguateEscapeCodes))

	if got := term.KeyboardProtocol(); got != KeyboardProtocolSupported {
		t.Fatalf("KeyboardProtocol() = %v, want supported (the emulator answered `CSI ? 1 u`)", got)
	}
	flags, ok := term.KeyboardProtocolFlags()
	if !ok || flags != sgr.KeyboardProtocolDisambiguateEscapeCodes {
		t.Fatalf("KeyboardProtocolFlags() = (%d, %v), want (%d, true)", flags, ok, sgr.KeyboardProtocolDisambiguateEscapeCodes)
	}

	// The exit shape: Close restores the terminal's keyboard mode with the pop.
	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	peer.waitForSeq(t, sgr.KeyboardProtocolPopSeq(sgr.KeyboardProtocolPopEntries))
	// …and exactly once: a second Close is a no-op, so nothing new is written.
	before := len(peer.snapshot())
	if err := term.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	time.Sleep(keyProtocolQuietWait) //stylegate:allow-sleep bounded watch for output a no-op Close must not produce
	if after := len(peer.snapshot()); after != before {
		t.Fatalf("a second Close wrote %d more byte(s): %q", after-before, peer.snapshot()[before:])
	}
	if n := bytes.Count(peer.snapshot(), []byte(sgr.KeyboardProtocolPopSeq(sgr.KeyboardProtocolPopEntries))); n != 1 {
		t.Fatalf("the pty received the pop %d time(s), want exactly 1", n)
	}
}

// TestKeyProtocolPopOnTheSignalPath proves the OTHER exit path restores the
// terminal's keyboard mode: CaptureSignals' handler calls the same Close the
// normal path does, so the pop rides it (the epic's "Close/CaptureSignals
// path", one restore path rather than a second one that could be forgotten).
//
// THE SIGNAL CHOSEN, and why it is honest. The handler's body is: run the
// per-turn handler (none installed here) → Close() → signal.Reset(sigs) →
// re-raise with the DEFAULT disposition. A case that re-raised SIGTERM would
// kill the test binary, so the capture names SIGWINCH: its default action is
// "ignore", which means the re-raise is observed (the process survives it) while
// every other step — including the pop — is the production one. What this case
// therefore does NOT claim is anything about exit STATUS on a fatal signal; that
// is unchanged by this task and belongs to the machinery that already measures it.
func TestKeyProtocolPopOnTheSignalPath(t *testing.T) {
	peer := &keyProtocolPeer{
		replyTo: sgr.KeyboardProtocolQuerySeq(),
		reply:   []byte(sgr.KeyboardProtocolReplySeq(sgr.KeyboardProtocolDisambiguateEscapeCodes)),
	}
	term, peer := newKeyProtocolRig(t, peer)
	peer.waitForSeq(t, sgr.KeyboardProtocolPushSeq(sgr.KeyboardProtocolDisambiguateEscapeCodes))
	if got := term.KeyboardProtocol(); got != KeyboardProtocolSupported {
		t.Fatalf("the emulator's answer did not produce the supported verdict (%v)", got)
	}
	unsubscribe := term.CaptureSignals(syscall.SIGWINCH)
	defer unsubscribe()

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatalf("raise SIGWINCH: %v", err)
	}
	peer.waitForSeq(t, sgr.KeyboardProtocolPopSeq(sgr.KeyboardProtocolPopEntries))
	if n := bytes.Count(peer.snapshot(), []byte(sgr.KeyboardProtocolPopSeq(sgr.KeyboardProtocolPopEntries))); n != 1 {
		t.Fatalf("the signal path wrote the pop %d time(s), want exactly 1", n)
	}
	// The handler ran to its end: the terminal object is closed, so the pop is
	// not merely pending — the restore path completed.
	if !term.closed {
		t.Fatal("the captured signal did not take the terminal through Close")
	}
}

// TestKeyProtocolSilentTerminalIsBoundedAndNeverPushes is the unsupported path:
// a terminal that does not answer must cost a bounded wait, must NOT receive the
// push, and must be REPORTED (the verdict is what the chat layer prints the
// Alt+Enter advice from).
func TestKeyProtocolSilentTerminalIsBoundedAndNeverPushes(t *testing.T) {
	peer := &keyProtocolPeer{replyTo: sgr.KeyboardProtocolQuerySeq()}
	start := time.Now()
	term, peer := newKeyProtocolRig(t, peer)
	defer func() {
		// deliberately ignored: the case's own teardown.
		_ = term.Close()
	}()
	elapsed := time.Since(start)

	peer.waitForSeq(t, sgr.KeyboardProtocolQuerySeq())
	if got := term.KeyboardProtocol(); got != KeyboardProtocolUnsupported {
		t.Fatalf("KeyboardProtocol() = %v, want no-reply (nothing answered the query)", got)
	}
	if _, ok := term.KeyboardProtocolFlags(); ok {
		t.Fatal("KeyboardProtocolFlags() reported an answer for a terminal that never wrote one")
	}
	if !peer.refusesSeq(sgr.KeyboardProtocolPushSeq(sgr.KeyboardProtocolDisambiguateEscapeCodes)) {
		t.Fatalf("the program pushed the enhancement on a silent terminal: %q", peer.snapshot())
	}
	// The bound: Open waited for the reply, and no longer. The assertion is a
	// CEILING with generous slack (the probe's own window plus scheduling), so it
	// can only fail if the probe stopped honouring its deadline.
	if maxWait := keyboardProbeWindow + keyProtocolPeerWait; elapsed > maxWait {
		t.Fatalf("Open on a silent terminal took %v, over the %v ceiling: the probe is not bounded", elapsed, maxWait)
	}
}

// TestKeyProtocolProbeReplaysTypedBytesInOrder is the constraint that the
// negotiation must never eat a keystroke: the operator types while the probe is
// parked, the terminal answers nothing, and those bytes come back to the caller
// IN ORDER — before anything read afterwards.
func TestKeyProtocolProbeReplaysTypedBytesInOrder(t *testing.T) {
	typed := []byte("hello")
	peer := &keyProtocolPeer{replyTo: sgr.KeyboardProtocolQuerySeq(), typed: typed}
	term, peer := newKeyProtocolRig(t, peer)
	defer func() {
		// deliberately ignored: the case's own teardown.
		_ = term.Close()
	}()

	// Wait until the program really belongs to "unsupported" AND the probe's
	// window is over: the bytes must be replayed, not merely about to be.
	deadline := time.Now().Add(keyProtocolPeerWait)
	for {
		if term.KeyboardProtocol() == KeyboardProtocolUnsupported {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the probe never gave up on the silent terminal (%v)", term.KeyboardProtocol())
		}
		time.Sleep(keyProtocolPeerStep) //stylegate:allow-sleep bounded wait for the probe's verdict
	}
	// The emulator wrote the operator's bytes the moment it saw the query — that
	// is the whole shape being tested. Wait (bounded) until the PROBE has taken
	// them out of the descriptor: from here on they can only come from the
	// prefetch queue, which is what makes the ordering assertion below about the
	// queue and not about the kernel's buffer.
	for len(term.PrefetchedBytes()) < len(typed) {
		if time.Now().After(deadline) {
			t.Fatalf("the probe never took the typed bytes (%q of %q)", term.PrefetchedBytes(), typed)
		}
		time.Sleep(keyProtocolPeerStep) //stylegate:allow-sleep bounded wait for the probe to drain the typed bytes
	}

	// Every typed byte comes back, in order, through the SAME read path the
	// editor uses.
	var got []byte
	buf := make([]byte, keyProtocolSniffBytes)
	for len(got) < len(typed) {
		ev, err := term.ReadEvent(buf, 0)
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("ReadEvent: %v", err)
		}
		if ev.Kind != EventInput || ev.Bytes == 0 {
			t.Fatalf("ReadEvent returned %+v while prefetched bytes were pending", ev)
		}
		got = append(got, buf[:ev.Bytes]...)
	}
	if string(got) != string(typed) {
		t.Fatalf("the prefetched bytes came back as %q, want %q (order and content are the contract)", got, typed)
	}
	if got := term.PrefetchedBytes(); len(got) != 0 {
		t.Fatalf("Prefetched() still holds %q after the read loop drained it", got)
	}
}

// TestKeyProtocolShiftEnterReachesTheEditorAndDoesNotSubmit is the acceptance's
// decode half measured THROUGH THE DESCRIPTOR the editor really reads keys on
// (Rule 31): the terminal negotiates, the emulator then sends what a
// disambiguating terminal sends for Shift+Enter, and the editor is driven for
// real — the assertion is the submit COUNT, which is what the epic is about.
//
//	CSI 13;2u  → the buffer gains a newline and Run does NOT return   (count 0)
//	0x0d       → Run RETURNS the line, newline included               (count 1)
//	0x0d again → Run RETURNS again, empty                             (count 2)
//
// "It did not submit" is the absence of an event, so it is MEASURED as one: Run
// is driven on a goroutine and the case watches it for longer than the paste
// window before concluding that nothing came back. A case that only checked the
// final line could not tell "Shift+Enter did not submit" from "Shift+Enter
// submitted and the CR was swallowed".
func TestKeyProtocolShiftEnterReachesTheEditorAndDoesNotSubmit(t *testing.T) {
	peer := &keyProtocolPeer{
		replyTo: sgr.KeyboardProtocolQuerySeq(),
		reply:   []byte(sgr.KeyboardProtocolReplySeq(sgr.KeyboardProtocolDisambiguateEscapeCodes)),
	}
	term, peer := newKeyProtocolRig(t, peer)
	defer func() {
		// deliberately ignored: the case's own teardown; the restore is asserted
		// by the cases above.
		_ = term.Close()
	}()
	peer.waitForSeq(t, sgr.KeyboardProtocolPushSeq(sgr.KeyboardProtocolDisambiguateEscapeCodes))

	ed := NewEditor(term, io.Discard, promptForTest, nil)
	write := func(s string) {
		t.Helper()
		if _, err := peer.master.Write([]byte(s)); err != nil { //stylegate:ignore the emulator writing the keys a keyboard would send
			t.Fatalf("emulator write %q: %v", s, err)
		}
	}

	// Turn 1: "ab", Shift+Enter, "cd" — and NO submit yet.
	write("ab")
	write("\x1b[13;2u")
	write("cd")
	// The absence is measured over a window longer than the editor's own paste
	// suppression, so "nothing returned" cannot be a scheduling coincidence.
	pending := keyProtocolDriveAsync(ed)
	select {
	case out := <-pending:
		t.Fatalf("Shift+Enter submitted: Run returned %+v (err %v) — the submit count is not 0", out.res, out.err)
	case <-time.After(keyProtocolNoSubmitWait):
	}
	buf := ed.Value().Text()
	if !strings.Contains(buf, "\n") {
		t.Fatalf("the buffer holds %q: Shift+Enter inserted no newline", buf)
	}
	if strings.Contains(buf, "\x1b") {
		t.Fatalf("the buffer holds the raw sequence: %q", buf)
	}

	// Enter: the same Run that was parked above must now return, carrying the
	// newline the Shift+Enter put in the buffer. Submit count 1.
	write("\r")
	var first keyProtocolRunOutcome
	select {
	case first = <-pending:
	case <-time.After(keyProtocolSubmitWait):
		t.Fatal("Enter did not submit: Run never returned after the CR")
	}
	if first.err != nil {
		t.Fatalf("Run: %v", first.err)
	}
	if first.res.EOF || first.res.Interrupted {
		t.Fatalf("Run returned EOF=%v interrupted=%v, want a submitted line", first.res.EOF, first.res.Interrupted)
	}
	if line := strings.TrimRight(first.res.Line, "\n"); line != "ab\ncd" {
		t.Fatalf("the submitted line is %q, want %q (Shift+Enter's newline belongs IN the message)", first.res.Line, "ab\ncd")
	}

	// Turn 2: a bare Enter submits an empty line — count 2, and proof the
	// negotiation did not leave the editor in a state where Enter means newline.
	write("\r")
	second := keyProtocolDrive(t, ed)
	if second.timed {
		t.Fatal("the second Enter did not submit: Run never returned")
	}
	if second.err != nil {
		t.Fatalf("second Run: %v", second.err)
	}
	if strings.TrimRight(second.res.Line, "\n") != "" {
		t.Fatalf("the second submitted line is %q, want empty", second.res.Line)
	}
}

// TestKeyProtocolAltEnterFallbackWorksOnASilentTerminal is the epic's other
// acceptance half, measured the same way: on a terminal that answered NOTHING
// (so the chat layer advises Alt+Enter), the advised key must really insert a
// newline and not submit — and it must do so through the SAME descriptor and
// the SAME editor, with the probe's prefetch in between.
//
// Alt+Enter is `ESC CR`, the two-byte form the decoder already maps to
// KeyNewline; nothing in this task added it. What the case pins is that the
// advice printed to the operator on an unsupported terminal is not a lie.
func TestKeyProtocolAltEnterFallbackWorksOnASilentTerminal(t *testing.T) {
	peer := &keyProtocolPeer{replyTo: sgr.KeyboardProtocolQuerySeq()} // never answers
	term, peer := newKeyProtocolRig(t, peer)
	defer func() {
		// deliberately ignored: the case's own teardown.
		_ = term.Close()
	}()
	if got := term.KeyboardProtocol(); got != KeyboardProtocolUnsupported {
		t.Fatalf("KeyboardProtocol() = %v, want the no-reply verdict (this is the path the advice is printed on)", got)
	}
	if !peer.refusesSeq(sgr.KeyboardProtocolPushSeq(sgr.KeyboardProtocolDisambiguateEscapeCodes)) {
		t.Fatalf("the enhancement was pushed on a terminal that never answered: %q", peer.snapshot())
	}

	ed := NewEditor(term, io.Discard, promptForTest, nil)
	if _, err := peer.master.Write([]byte("ab\x1b\rcd")); err != nil { //stylegate:ignore the emulator writing the keys a keyboard would send
		t.Fatalf("emulator write: %v", err)
	}
	pending := keyProtocolDriveAsync(ed)
	select {
	case out := <-pending:
		t.Fatalf("Alt+Enter submitted: Run returned %+v (err %v)", out.res, out.err)
	case <-time.After(keyProtocolNoSubmitWait):
	}
	if buf := ed.Value().Text(); !strings.Contains(buf, "\n") {
		t.Fatalf("the buffer holds %q: Alt+Enter inserted no newline", buf)
	}
	if _, err := peer.master.Write([]byte("\r")); err != nil { //stylegate:ignore the emulator writing the keys a keyboard would send
		t.Fatalf("emulator write: %v", err)
	}
	var out keyProtocolRunOutcome
	select {
	case out = <-pending:
	case <-time.After(keyProtocolSubmitWait):
		t.Fatal("Enter did not submit after the Alt+Enter fallback")
	}
	if out.err != nil {
		t.Fatalf("Run: %v", out.err)
	}
	if line := strings.TrimRight(out.res.Line, "\n"); line != "ab\ncd" {
		t.Fatalf("the submitted line is %q, want %q", out.res.Line, "ab\ncd")
	}
}

// keyProtocolRunOutcome is one driven Run: what it returned, or that the wait
// expired (which IS the "it did not submit" measurement).
type keyProtocolRunOutcome struct {
	res   Result
	err   error
	timed bool
}

// keyProtocolDrive runs one editor line and waits for it, reporting a timeout as
// an outcome rather than a failure — the caller decides what a parked Run means.
func keyProtocolDrive(t *testing.T, ed *Editor) keyProtocolRunOutcome {
	t.Helper()
	select {
	case out := <-keyProtocolDriveAsync(ed):
		return out
	case <-time.After(keyProtocolSubmitWait):
		return keyProtocolRunOutcome{timed: true}
	}
}

// keyProtocolDriveAsync starts one editor line and hands back the channel it
// will arrive on, so a case can watch for a submit it expects NOT to happen.
func keyProtocolDriveAsync(ed *Editor) <-chan keyProtocolRunOutcome {
	ch := make(chan keyProtocolRunOutcome, 1)
	go func() {
		res, err := ed.Run()
		ch <- keyProtocolRunOutcome{res: res, err: err}
	}()
	return ch
}

// TestKeyProtocolCRLFDedupSurvivesTheProbe pins the constraint that the probe
// changes NOTHING about ordinary input: after the negotiation, a CR still reads
// as a submit-shaped Enter and a CRLF pair still reads as ONE Enter — the
// dedup the editor relies on must not be broken by bytes having passed through
// the prefetch queue.
func TestKeyProtocolCRLFDedupSurvivesTheProbe(t *testing.T) {
	peer := &keyProtocolPeer{
		replyTo: sgr.KeyboardProtocolQuerySeq(),
		reply:   []byte(sgr.KeyboardProtocolReplySeq(sgr.KeyboardProtocolDisambiguateEscapeCodes)),
	}
	term, peer := newKeyProtocolRig(t, peer)
	defer func() {
		// deliberately ignored: the case's own teardown.
		_ = term.Close()
	}()
	peer.waitForSeq(t, sgr.KeyboardProtocolPushSeq(sgr.KeyboardProtocolDisambiguateEscapeCodes))

	// Everything below arrives through the pty, i.e. through the SAME descriptor
	// the probe read from, after raw mode was installed: Shift+Enter's
	// disambiguated form, then Enter+LF.
	_, err := peer.master.Write([]byte("\x1b[13;2u\r\n")) //stylegate:ignore the emulator writing the keys a keyboard would send
	if err != nil {
		t.Fatalf("emulator write: %v", err)
	}
	var got []byte
	buf := make([]byte, keyProtocolSniffBytes)
	deadline := time.Now().Add(keyProtocolPeerWait)
	for len(got) < len("\x1b[13;2u\r\n") {
		if time.Now().After(deadline) {
			t.Fatalf("only %q arrived; the emulator wrote %q", got, "\x1b[13;2u\r\n")
		}
		ev, err := term.ReadEvent(buf, keyboardProbeSliceMS)
		if err != nil {
			t.Fatalf("ReadEvent: %v", err)
		}
		if ev.Kind != EventInput {
			continue
		}
		got = append(got, buf[:ev.Bytes]...)
	}
	// The decoder — the ONE decoder, unchanged by this task — reads the same
	// bytes the same way whether or not a negotiation happened first.
	d := NewDecoder()
	keys := d.Feed(got)
	want := []KeyCode{KeyNewline, KeyEnter, KeyNewline}
	var codes []KeyCode
	for _, k := range keys {
		if k.Code != KeyNone {
			codes = append(codes, k.Code)
		}
	}
	if len(codes) != len(want) {
		t.Fatalf("decoded %v from %q, want %v", codes, got, want)
	}
	for i := range want {
		if codes[i] != want[i] {
			t.Fatalf("decoded %v from %q, want %v", codes, got, want)
		}
	}
}
