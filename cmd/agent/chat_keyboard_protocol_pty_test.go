package main

// chat_keyboard_protocol_pty_test.go — the REPL's notice about the keyboard
// negotiation, measured over a REAL pty where the test drives the terminal.
//
// WHAT IS ASSERTED, and why it takes a pty. The notice's whole value is that it
// appears ONCE on a terminal that does not answer, and NEVER on one that does (a
// terminal that got the push has nothing to apologise for). Both halves are
// properties of the descriptor the input layer was built on, so the case builds
// the REAL chat input over a pty whose master the test drives as the emulator —
// the same lineedit.Open + chatInput path `gopi chat` takes.
//
// The two shapes:
//
//	silent terminal (xterm, tmux, …) → exactly ONE "Alt+Enter" line, verdict no-reply
//	kitty answering `CSI ? 1 u`      → no advice at all, verdict supported
//
// The notice is read back from the input layer's OWN notices stream (errOut),
// which is the surface the statement is made on — not a proxy for it.

import (
	"bytes"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sgr"
)

const (
	// chatKeyProtocolNoticeKey is the advice's distinguishing text — the key a
	// terminal without the protocol can still send.
	chatKeyProtocolNoticeKey = "Alt+Enter"
	// chatKeyProtocolCaptureCap bounds the notices buffer. The notice is one
	// line; the cap is a runaway guard, not a size the assertions depend on.
	chatKeyProtocolCaptureCap = 1 << 16
)

// chatKeyProtocolCapture is the notices sink: the stream the operator reads the
// REPL's own degradations on.
type chatKeyProtocolCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *chatKeyProtocolCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.buf.Len() < chatKeyProtocolCaptureCap {
		c.buf.Write(p)
	}
	return len(p), nil
}

func (c *chatKeyProtocolCapture) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// chatKeyProtocolPeer is the emulator: it watches what the program wrote and, if
// asked to, answers the keyboard-protocol query.
type chatKeyProtocolPeer struct {
	master *os.File
	answer bool
	mu     sync.Mutex
	seen   bytes.Buffer
}

func (p *chatKeyProtocolPeer) text() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seen.String()
}

func (p *chatKeyProtocolPeer) run() {
	buf := make([]byte, 256)
	answered := false
	for {
		n, err := p.master.Read(buf)
		if n > 0 {
			p.mu.Lock()
			p.seen.Write(buf[:n])
			sawQuery := strings.Contains(p.seen.String(), sgr.KeyboardProtocolQuerySeq())
			p.mu.Unlock()
			if sawQuery && p.answer && !answered {
				answered = true
				// The reply a supporting terminal sends: `CSI ? 1 u`.
				_, _ = p.master.Write([]byte(sgr.KeyboardProtocolReplySeq(sgr.KeyboardProtocolDisambiguateEscapeCodes))) //stylegate:ignore the emulator's own write to the pty it drives
			}
		}
		if err != nil {
			return
		}
	}
}

// chatKeyProtocolRun builds the real chat input over a pty, runs the REPL's own
// announcement, and returns the text the notices stream received (verbose=false,
// so the stderr plumbing line is not part of what is asserted here).
func chatKeyProtocolRun(t *testing.T, answer bool, verbose bool) string {
	t.Helper()
	pair := chatOpenPtyPair(t, 80, 24)
	var peer chatKeyProtocolPeer
	peer.master = pair.Master
	peer.answer = answer
	go peer.run()

	// THE PRODUCTION WIRING, minus the pieces only a real run owns: lineedit.Open
	// on the pty slave — which is where the negotiation happens — and then the
	// chat input layer OVER that very terminal, the order
	// newChatInputWithPalette performs at cli_chat.go.
	capture := &chatKeyProtocolCapture{}
	term, err := lineedit.Open(pair.Slave, pair.Slave)
	if err != nil {
		t.Fatalf("lineedit.Open on the pty slave: %v", err)
	}
	t.Cleanup(func() {
		// deliberately ignored: the case's own teardown of the terminal it opened.
		_ = term.Close()
	})
	in := newChatInputWithTerminal(term, pair.Slave, chatPrompt)
	in.errOut = capture
	// The REPL's own two calls, in the order cli_chat.go makes them: the
	// announcement (which now only READS the verdict) and the notice the run HOLDS
	// for the startup screen. This case is the path with NO screen to state it, so
	// the held notice is printed through the stream — which is what this capture
	// is.
	verdict := chatAnnounceKeyboardProtocol(in, verbose)
	chatPanelNoticesStream(in, chatStartupNotices(true, nil, "", verdict))
	return capture.text()
}

// TestChatKeyboardProtocolNoticeOnSilentTerminal is the degrading path: the
// operator is told ONCE that Shift+Enter cannot be distinguished here and which
// key works instead.
func TestChatKeyboardProtocolNoticeOnSilentTerminal(t *testing.T) {
	text := chatKeyProtocolRun(t, false, false)

	if !strings.Contains(text, chatKeyProtocolNoticeKey) {
		t.Fatalf("a silent terminal produced no Alt+Enter advice; the notices stream held %q", text)
	}
	if n := strings.Count(text, chatKeyProtocolNoticeKey); n != 1 {
		t.Fatalf("the advice appeared %d time(s) in %q, want exactly once (a notice per read would fight the layout)", n, text)
	}
}

// TestChatKeyboardProtocolSilentOnSupportedTerminal is the other half of the same
// property: a terminal that answered has NOTHING to apologise for, and a notice
// there would be advice for a problem this run does not have.
func TestChatKeyboardProtocolSilentOnSupportedTerminal(t *testing.T) {
	text := chatKeyProtocolRun(t, true, false)

	if strings.Contains(text, chatKeyProtocolNoticeKey) {
		t.Fatalf("a terminal that answered got the Alt+Enter advice anyway: %q", text)
	}
}

// TestChatKeyboardProtocolVerboseStatesTheProbeResult pins the observability
// constraint (Rule 15): under --verbose the SUPPORTED path states its result, so
// "did the negotiation run?" is answerable without a debugger. The line goes to
// the process's stderr (startup plumbing), which is what this case captures.
func TestChatKeyboardProtocolVerboseStatesTheProbeResult(t *testing.T) {
	got := chatKeyProtocolVerboseRun(t, true)
	if !strings.Contains(got, lineedit.KeyboardProtocolSupported.String()) {
		t.Fatalf("--verbose on a terminal that answered printed %q, want it to name the %q verdict",
			got, lineedit.KeyboardProtocolSupported.String())
	}
	// …and the degrading path states its own result under verbose as well: the
	// measured fact is printed beside the advice, not only the advice.
	got = chatKeyProtocolVerboseRun(t, false)
	if !strings.Contains(got, lineedit.KeyboardProtocolUnsupported.String()) {
		t.Fatalf("--verbose on a silent terminal printed %q, want it to name the %q verdict",
			got, lineedit.KeyboardProtocolUnsupported.String())
	}
}

// chatKeyProtocolVerboseRun builds the real chat input over a pty and runs the
// REPL's own announcement with --verbose, returning BOTH streams: the advice
// lands on the notices stream and the measured result on stderr (startup
// plumbing), exactly as production splits them.
func chatKeyProtocolVerboseRun(t *testing.T, answer bool) string {
	t.Helper()
	pair := chatOpenPtyPair(t, 80, 24)
	var peer chatKeyProtocolPeer
	peer.master = pair.Master
	peer.answer = answer
	go peer.run()
	term, err := lineedit.Open(pair.Slave, pair.Slave)
	if err != nil {
		t.Fatalf("lineedit.Open on the pty slave: %v", err)
	}
	t.Cleanup(func() {
		// deliberately ignored: the case's own teardown of the terminal it opened.
		_ = term.Close()
	})
	capture := &chatKeyProtocolCapture{}
	in := newChatInputWithTerminal(term, pair.Slave, chatPrompt)
	in.errOut = capture
	var streamed string
	func() {
		streamed = captureStderr(t, func() {
			verdict := chatAnnounceKeyboardProtocol(in, true)
			chatPanelNoticesStream(in, chatStartupNotices(true, nil, "", verdict))
		})
	}()
	return streamed + capture.text()
}
