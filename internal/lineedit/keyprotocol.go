package lineedit

// keyprotocol.go — the terminal-capability negotiation for Shift+Enter, run
// ONCE at raw-mode entry.
//
// THE DEFECT. In raw mode the terminal sends 0x0d for Enter, and a Shift+Enter
// that the terminal does not tell us apart sends the SAME 0x0d: the information
// ("a modifier was held") was destroyed before any byte reached this process, so
// no decoder can recover it. internal/lineedit/keys.go already reads the
// disambiguated form — `CSI 13;2u` → KeyNewline (keys.go, mapFinal 'u') — what
// was missing is the negotiation that makes the terminal SEND it: the kitty
// keyboard protocol's disambiguation enhancement.
//
// THE NEGOTIATION (sgr/kitty.go owns the spelling):
//
//	CSI ? u        ask    — the terminal answers `CSI ? flags u`, or says nothing
//	CSI > 1 u      push   — ask for disambiguate-escape-codes
//	CSI < 1 u      pop    — restore whatever the flags were before the push
//
// THE PROBE IS BOUNDED AND IT NEVER EATS A KEYSTROKE. Two failures matter and
// neither is allowed:
//
//   - A terminal that does not answer must not leave the process waiting: the
//     probe has a fixed window (keyboardProbeWindow), so `gopi chat` starts on a
//     non-supporting terminal instead of hanging on a reply that will never come.
//   - A keystroke typed DURING the window is the operator's, not ours. Every byte
//     read here goes through a strict parser: the moment the head of the buffer
//     is not a reply (or is a complete reply followed by more bytes), the bytes
//     are kept in ORDER in a prefetch queue and the caller's next ReadEvent
//     returns them before anything else. The probe stops at the first byte that
//     is not part of a reply — it never waits out its window once the operator
//     has started typing.

import (
	"time"

	"golang.org/x/sys/unix"

	"github.com/nickhuang99/gopi/internal/sgr"
)

// KeyboardProtocol is the probe's verdict, and it is a three-state answer on
// purpose: "we did not ask" (no terminal, nothing to negotiate with), "the
// terminal answered" and "it stayed silent" are DIFFERENT facts, and the caller
// has to be able to print the difference (Rule 15 — a capability that failed
// silently is a capability nobody can debug).
type KeyboardProtocol int

const (
	// KeyboardProtocolUnavailable: no probe was run — there is no terminal to
	// negotiate with (a nil/absent descriptor). The caller must not tell the
	// operator anything about a negotiation that never happened.
	KeyboardProtocolUnavailable KeyboardProtocol = iota
	// KeyboardProtocolUnsupported: the query went out and the window closed with
	// no reply. This is the terminal shape that needs the Alt+Enter advice.
	KeyboardProtocolUnsupported
	// KeyboardProtocolSupported: the terminal answered `CSI ? flags u`, and the
	// disambiguation enhancement was pushed.
	KeyboardProtocolSupported
)

// String names each state for a diagnostic line (Rule 1b: the words a reader
// sees are a vocabulary, not three typed literals).
func (k KeyboardProtocol) String() string {
	switch k {
	case KeyboardProtocolSupported:
		return "supported"
	case KeyboardProtocolUnsupported:
		return "no-reply"
	case KeyboardProtocolUnavailable:
		return "not-probed"
	}
	return "unknown"
}

// The probe's bounds. Each is a named constant (Rule 1) because each one is a
// policy decision with an observable failure behind it:
const (
	// keyboardProbeWindow is how long the reply may take. The terminal answers
	// from the same machine (the reply is written by the emulator that owns the
	// pty), so a local run answers in microseconds; the window is sized for a
	// remote session's round trip. It is a CEILING ON A WAIT, not a measured
	// duration: on a terminal that supports the protocol the probe returns the
	// moment the reply arrives, and on one that does not it costs exactly this
	// much once, at startup.
	keyboardProbeWindow = 200 * time.Millisecond
	// keyboardProbeSliceMS is the largest slice the probe polls for. It exists
	// only so the deadline is re-checked while parked; a single long poll would
	// also return on input, but the loop's exit condition ("the window is over")
	// has to be evaluated on a clock THIS code owns, not on the terminal's
	// behaviour.
	keyboardProbeSliceMS = 20
)

// keyboardProtocolProbe installs the negotiation on a terminal that has just
// entered raw mode. It is called by Open, before the caller can read anything,
// so the prefetch queue it may fill is already in place for the first ReadEvent.
//
// It never fails the open: a terminal that does not answer is a DEGRADATION, and
// degrading (plus telling the caller, which KeyboardProtocol exposes) is the
// behaviour — refusing to run `gopi chat` on an xterm would be absurd.
func (t *Terminal) keyboardProtocolProbe() {
	if t.in == nil || t.inputFD < fdMinValid {
		t.keyboard = KeyboardProtocolUnavailable
		return
	}
	t.write(sgr.KeyboardProtocolQuerySeq())
	deadline := time.Now().Add(keyboardProbeWindow)
	var buf []byte
	for {
		if !time.Now().Before(deadline) {
			break
		}
		fds := []unix.PollFd{{Fd: int32(t.inputFD), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, keyboardProbeSliceMS)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			// The poll itself failed: that is not "the terminal said no", but it
			// IS the end of the probe, and the flags stay at unsupported — the
			// caller prints the advice, which is the safe direction (Alt+Enter
			// works everywhere).
			break
		}
		if n == 0 {
			continue
		}
		if fds[0].Revents&(unix.POLLIN|unix.POLLHUP) == 0 {
			break
		}
		chunk := make([]byte, readBufBytes)
		got, rerr := t.in.Read(chunk)
		if got > 0 {
			buf = append(buf, chunk[:got]...)
		}
		if rerr != nil {
			// A read error (including EOF) ends the probe with whatever bytes
			// arrived; they are replayed below.
			break
		}
		reply, verdict := sgr.ParseKeyboardProtocolReply(buf)
		switch verdict {
		case sgr.KeyboardProtocolComplete:
			t.keyboardFlags = reply.Flags
			t.keyboard = KeyboardProtocolSupported
			t.addPrefetch(buf[reply.Bytes:])
			t.write(sgr.KeyboardProtocolPushSeq(sgr.KeyboardProtocolDisambiguateEscapeCodes))
			t.keyboardPushed = true
			return
		case sgr.KeyboardProtocolNoReply:
			// The head of the buffer is not a reply: the operator typed. Stop
			// asking (the terminal had its chance and the bytes in hand are not
			// an answer) and keep every byte for the decoder, in order.
			t.keyboard = KeyboardProtocolUnsupported
			t.addPrefetch(buf)
			return
		}
	}
	t.keyboard = KeyboardProtocolUnsupported
	t.addPrefetch(buf)
}

// KeyboardProtocol reports the capability verdict of the probe that ran at
// Open. A caller that gets KeyboardProtocolUnsupported is expected to say so
// once and name the alternative (Alt+Enter) — that is the whole point of the
// state being observable (Rule 15).
func (t *Terminal) KeyboardProtocol() KeyboardProtocol { return t.keyboard }

// KeyboardProtocolFlags reports the flags the terminal answered with, and
// whether an answer arrived at all (flags are legitimately 0, so the bool is
// not redundant). It is what a verbose diagnostic prints beside the verdict.
func (t *Terminal) KeyboardProtocolFlags() (int, bool) {
	return t.keyboardFlags, t.keyboard == KeyboardProtocolSupported
}

// Prefetched returns a copy of the bytes the probe read but did not consume
// (an operator's keystrokes that arrived inside the probe window). They are
// returned to the caller as input, in order, before anything read afterwards.
// It exists for the test that has to OBSERVE the ordering guarantee, and for a
// caller that wants to report what was held back.
// The queue itself is master's ONE replay buffer (raw.go: `prefetch` +
// `probeMu`, takePrefetch/addPrefetch): the colour probe and this one hold their
// held-back bytes in the SAME queue, so "the bytes the operator typed while a
// probe was parked" has a single order and a single owner. This probe's writer
// is addPrefetch, its reader takePrefetch, and the probe_pty_test's own
// Prefetched() (a byte COUNT) is the observable both probes report through.
func (t *Terminal) PrefetchedBytes() []byte {
	t.probeMu.Lock()
	defer t.probeMu.Unlock()
	return append([]byte(nil), t.prefetch...)
}

// releaseKeyboardProtocol pops the terminal's keyboard-mode stack on the way
// out, restoring whatever the flags were before our push. It is called from
// Close, so every exit path the terminal already restores (a normal exit, a
// deferred Close, a panic unwinding, and the fatal-signal handler in
// CaptureSignals, which calls Close) restores this too — one restore path, not
// a second one that could be forgotten.
//
// Idempotent: the flag records whether OUR push is still on the stack, so a
// close after a close does not pop an entry somebody else pushed.
func (t *Terminal) releaseKeyboardProtocol() {
	if !t.keyboardPushed {
		return
	}
	t.keyboardPushed = false
	t.write(sgr.KeyboardProtocolPopSeq(sgr.KeyboardProtocolPopEntries))
}
