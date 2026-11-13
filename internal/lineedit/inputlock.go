package lineedit

import (
	"strconv"
)

// inputlock.go — the EDITOR'S OWN READ-ONLY STATE: a hold that a surface owning
// the editing line can take, so the keys a watcher types cannot change the text
// the surface is driving.
//
// WHY THE EDITOR AND NOT ONLY A KEY GATE (defect ②, `gopi chat --replay`,
// 2026-10-05). The replay driver types the recording INTO this editor; the line
// buffer is therefore the performance surface, and a watcher who presses a
// character mid-performance edits the artifact the next recorded keystroke is
// appended to — the pollution is irreversible, because the bytes that were going
// to be typed have already been decided. A key GATE (keysource.go) can hold such
// a key off, and the replay's control layer does exactly that at its own seam.
// What a gate cannot do is make the READ-ONLY property TRUE OF THE EDITOR: a gate
// is installed by one caller, absent for every other, and consulted only on the
// key path (applyRead) — while the editor has a second path that reaches
// handleKey (the flushed lone Escape) and a future one would be added without
// anyone remembering the promise. So the state lives here, beside the buffer it
// protects, and the ONE predicate below decides what "the line" means for both
// arms (Rule 13: a gate and a lock that judged mutability differently would
// disagree about the same key).
//
// WHAT IT IS NOT. It is not a mode of the terminal, it does not stop the read
// loop, and it never refuses a key by DROPPING it in silence: a held key is
// reported to the editor's diagnostic stream (Rule 15 — the defect this pair of
// fixes closes was exactly a key that did nothing and said nothing), and the
// surface that took the hold announces the state to its user in its own words.
// It also does not cover the non-key writers of the line (an in-process
// InsertIntoLine producer): those are not a watcher's keys, and a hold that
// claimed to cover them would be claiming something it does not check.

// keyDropped is the ONE question the key loop asks before it applies a key, and
// it is asked at BOTH places a decoded key reaches handleKey (the read's own
// decoder loop and the flushed-escape arm). True means the key was consumed —
// either the surface's key gate claimed it, or this editor's read-only hold
// refused it — and must NOT be applied.
//
// THE ORDER IS THE CONTRACT OF BOTH SEAMS. The gate asks first: a surface that
// wantS a key must be able to claim it even when the hold would have dropped it,
// and a surface's own control key (the replay's pause, step and quit) is exactly
// that key. Only a key nobody claimed is then judged against the hold.
func (e *Editor) keyDropped(k Key) bool {
	if e.dispatchKeyGate(k) {
		return true
	}
	return e.holdKey(k)
}

// MutatesLine reports whether a key, IF THE EDITOR ACTED ON IT, would change the
// editing line's text. It is the ONE definition of "this key would edit the
// buffer", read by both arms named above: the editor's own lock (below) and a
// surface's key gate (cmd/agent's replay control layer), so the two cannot
// disagree about the same key.
//
// THE LINE IS THE PREDICATE, and that is what keeps it honest: motion (Home, End,
// ←/→, PageUp/Down, the scroll keys) and the two inert keys (KeyTab with no menu
// open, KeyPasteStart/End) do NOT mutate, so they stay available to whoever owns
// the surface; every key whose handler writes the buffer — a rune, a line break, a
// deletion, a kill, a yank, and EOF (which deletes forward whenever the line is
// not empty) — does.
func MutatesLine(k Key) bool {
	switch k.Code {
	case KeyRune:
		// A bare Alt+<rune> is not a binding the editor owns (handleKey's own
		// first arm): it inserts nothing, so it is not a mutation.
		return !k.Alt
	case KeyEnter, KeyNewline, KeyBackspace, KeyDelete,
		KeyKillToStart, KeyKillToEnd, KeyKillWordBack, KeyKillWordForward,
		KeyYank, KeyEOF:
		return true
	case KeyTab:
		// Tab is inert with no menu open and REPLACES the line with the selected
		// candidate when one is: a mutation is possible, so the key is one.
		return true
	}
	return false
}

// SetInputLocked arms or releases the hold, and RESETS the drop count so the
// number always describes the current episode rather than the process's history.
// Arming is idempotent: a surface that arms a hold it already had loses nothing
// and mis-reports nothing.
func (e *Editor) SetInputLocked(locked bool) {
	e.inputLocked = locked
	e.inputLockDrops = 0
}

// InputLocked reports whether the hold is armed. It is the READER beside
// SetInputLocked, and it exists so a surface can assert the state it believes it
// put the editor in instead of trusting the call it just made.
func (e *Editor) InputLocked() bool { return e.inputLocked }

// InputLockDrops reports how many keys the hold has refused since it was armed.
// A hold that silently ate a watcher's keystrokes and one that refused nothing
// would otherwise be the same observable (Rule 15), so the count is what makes
// "the line was protected" a measured claim rather than an intended one.
func (e *Editor) InputLockDrops() int { return e.inputLockDrops }

// resolveBareEscape decides whether a lone ESC the decoder is HOLDING still
// belongs to the read that produced it, and resolves it as the Escape key when
// the incoming read comes from a DIFFERENT PRODUCER. It reports whether the ESC
// was resolved. The caller then hands the key through the ordinary two arms
// (keyDropped, then handleKey) exactly as a timed-out flush would.
//
// WHY THIS EXISTS — the measured defect (2026-10-27, `gopi chat --replay`, real
// pty, 36 Esc presses): 8 of them were lost with NO state word, no LEAVING, the
// replay still typing and the process alive to its deadline. The mechanism is
// not a dropped byte; it is a FUSION. An ESC is the prefix of every escape
// sequence, so the decoder holds a bare one (`BareEscape`) and waits escWaitMS
// for the rest. `gopi chat --replay` runs TWO producers into the same editor: the
// watcher's terminal and the replay driver's injector, and the driver types a
// recorded chunk every injectKeyInterval (240 ms) — well inside the window. When
// the driver's byte arrived first, `Decoder.Feed` joined the two into ONE key:
// Alt+<rune>. `stampKeyOrigins` then stamped that fused key with the origin of
// the read that COMPLETED it (injected), and `chatReplayControl.GateKey` declines
// every key that is not typed — by design, so the recording's own spaces and
// arrows are not claimed as watcher input. So the Escape reached nobody: the key
// did not exist any more, and nothing anywhere could report it (Rule 15). The
// driver's own later bytes were then applied normally, which is why the replay
// looked untouched.
//
// WHY IT IS A PROVENANCE FACT AND NOT A LONGER TIMEOUT. A human cannot type half
// an escape sequence and then have another program complete it: `\x1b` followed
// by a key from the REPLAY DRIVER is not the arrow the watcher meant, whatever
// the delay. A longer window would make the loss rarer and the fusion later; the
// predicate below makes it impossible, and it costs nothing when one producer is
// on the wire (the ordinary terminal case, where every read has the same origin).
//
// WHY THE ASYMMETRY: a typed ESC followed by an INJECTED byte is resolved because
// the production consumer is the human-owned one. The reverse (an injected ESC
// and then a typed key) is left alone: the injector delivers its units one read
// at a time and paces them, so a lone ESC from it is a complete key in its own
// right, and the ordinary escWaitMS flush resolves it.
func (e *Editor) resolveBareEscape(origin ReadOrigin) bool {
	if !e.dec.BareEscape() {
		return false
	}
	// THE PREDICATE ONLY DECIDES; it does not consume. The caller resolves the
	// held byte through flushEscapedKey, which is the ONE arm that takes it out
	// of the decoder — a predicate that consumed it made the key vanish a second
	// time (measured while fixing this: the gate saw only the injected rune).
	return origin != e.escOrigin
}

// flushEscapedKey hands a resolved (or timed-out) lone ESC through the SAME road
// a decoded key travels: the owner's seat first, then the handler. It is ONE
// function so the three arms that can resolve an ESC — the bounded wait's expiry,
// a read from another producer, and any future one — cannot reach handleKey by
// three different routes (the defect the EventNone arm's comment records).
func (e *Editor) flushEscapedKey() (Result, bool, error) {
	k := e.dec.FlushEscape()
	if k.Code == KeyNone {
		return Result{}, false, nil
	}
	k.Origin = e.escOrigin
	if e.keyDropped(k) {
		return Result{}, false, nil
	}
	e.keyGateRelease(k)
	res, done, err := e.handleKey(k)
	if err != nil {
		return Result{}, false, err
	}
	if done {
		return res, true, nil
	}
	e.repaint()
	return Result{}, false, nil
}

// holdKey decides one key against the hold: it reports whether the key was HELD
// (i.e. dropped, and not to be applied), incrementing the count and telling the
// user why.
//
// ONLY A TYPED KEY IS HELD. An injected key is the surface's own typing — the
// recording being replayed is delivered through the injector, and the whole
// defect the hold closes is the two arriving at the same line; holding the
// injected arm too would make the surface unable to type at all.
func (e *Editor) holdKey(k Key) bool {
	if !e.inputLocked || k.Origin != ReadOriginTyped || !MutatesLine(k) {
		return false
	}
	e.inputLockDrops++
	e.warnInputLocked(k)
	return true
}

// warnInputLocked is the one line a held key costs, and it exists because the
// alternative is the silent failure this package's rules name: "I pressed a key
// and nothing happened" is unanswerable evidence for whoever is looking at the
// surface (Rule 15). It says WHICH key, that the line is held, and how many have
// been held — the three facts a reader needs to stop guessing.
func (e *Editor) warnInputLocked(k Key) {
	e.warn("the surface that owns this line is holding it — " + describeKey(k) +
		" was dropped; the line is not writable while the surface drives it (dropped so far: " +
		strconv.Itoa(e.inputLockDrops) + ")")
}

// warnPasteSuppressed is the one line a suppressed Enter costs, and it exists for
// the same reason warnInputLocked does: an Enter that silently becomes a newline
// is the failure Rule 15 forbids — "I pressed Enter and nothing happened" and
// "the program is wedged" are indistinguishable without it.
//
// IT IS SHORT ON PURPOSE, AND THE LENGTH IS THE POINT (measured 2026-11-10). It
// used to carry five facts and a remedy — the evidence, the key count, how far
// into the window the Enter arrived, the window's length, the running total AND
// "press Enter again once the window has passed to send" — which composed to
// ~300 cells: three rows at 101 columns. A diagnostic is written to errOut, OUTSIDE
// the reserved layout, so those three rows cost three rows of a fixed-height screen
// and the status row underneath them was covered until the next keystroke repainted
// it. What survives is what a reader cannot derive: WHICH evidence classified the
// read, the window that evidence carries (the two are different —
// Value.burstWindow), and how many Enters this surface has dropped. The remedy is
// dropped because it is not actionable information: the newline is already in the
// buffer and the next Enter sends.
//
// Every figure is READ from Value or derived from the window constants — a
// message carrying a literal that drifted from the policy would be its own
// defect, and a diagnostic that lies is worse than none.
func (e *Editor) warnPasteSuppressed() {
	evidence := strconv.Itoa(e.val.BurstKeys()) + " keys at once"
	if e.val.BurstFramed() {
		evidence = "bracketed paste"
	}
	e.warn("paste: Enter kept as newline (" + evidence +
		", window " + e.val.burstWindow().String() + ", " +
		strconv.Itoa(e.pasteSuppressed) + " suppressed)")
}

// describeKey names a key for a diagnostic line: a printable rune is shown AS
// the character (a reader who pressed `x` must recognise it), a key with a
// handler that writes the buffer is named by its WORD, and anything else falls
// back to its code so it is still identifiable.
func describeKey(k Key) string {
	if k.Code == KeyRune {
		if k.Alt {
			return "Alt+" + string(k.Rune)
		}
		return string(k.Rune)
	}
	switch k.Code {
	case KeyEnter:
		return "Return"
	case KeyNewline:
		return "newline"
	case KeyBackspace:
		return "Backspace"
	case KeyDelete:
		return "Delete"
	case KeyKillToStart:
		return "kill-to-start"
	case KeyKillToEnd:
		return "kill-to-end"
	case KeyKillWordBack:
		return "kill-word-back"
	case KeyKillWordForward:
		return "kill-word-forward"
	case KeyYank:
		return "yank"
	case KeyEOF:
		return "Ctrl+D"
	case KeyTab:
		return "Tab"
	}
	return "key#" + strconv.Itoa(int(k.Code))
}
