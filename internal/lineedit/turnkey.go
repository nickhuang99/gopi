package lineedit

// turnkey.go — WHICH KEY abandoned the turn.
//
// WHY AN ORIGIN AND NOT A BARE BOOL (the defect this file closes). The editor's
// turn-interrupt seam was installed for Ctrl+C and its action reported a plain
// bool: "I claimed the key". That is all the editor needs to decide what to do
// with the keystroke — but the CALLER needs more, because the same cancel
// reaches three different inputs (the raw 0x03 byte, a lone Escape, and a real
// SIGINT on a terminal whose ISIG is on) and it has to name the right one in the
// operator's marker and in the session JSONL. Without the origin the carrier
// would print "interrupted by ctrl-c" for an Escape, and the record would be a
// lie that no reader could detect (Rule 15: a failure names its cause).
//
// The vocabulary lives HERE, in the package that decodes the keys, so the name a
// carrier records is the name of the input event that carried it — not a second
// spelling invented at the call site (Rule 14).

// KeyOrigin is the terminal input that asked for a turn to be abandoned. It is
// the ORIGIN, not the reason: "the turn was abandoned by the operator" is the
// reason (agent.InterruptSourceOperator), and the origin narrows it to the
// keystroke or signal the operator used.
type KeyOrigin string

const (
	// OriginCtrlC is the raw 0x03 byte, classified by the editor's key decoder
	// (a raw-mode terminal clears ISIG, so Ctrl+C never becomes a signal on that
	// path).
	OriginCtrlC KeyOrigin = "ctrl-c"
	// OriginEscape is a LONE Escape — the decoder reports it only once the
	// ambiguity with an escape-sequence prefix has been resolved by the editor's
	// bounded ESC wait, so it can never be confused with an arrow key's prefix.
	OriginEscape KeyOrigin = "escape"
	// OriginSignal is a real SIGINT, delivered to a cooked input (piped stdin,
	// --no-editor, or a terminal that could not enter raw mode). It is the same
	// hardware key as OriginCtrlC on a different transport, and the two are
	// deliberately NOT collapsed: which transport delivered the key is exactly
	// what a reader debugging "the interrupt did not fire" needs to know.
	OriginSignal KeyOrigin = "signal"
	// OriginEOF is Ctrl+D on an empty line — the operator asking for the session
	// to END — pressed while a turn is still in flight.
	//
	// WHY IT IS AN INTERRUPT ORIGIN AT ALL (measured, task 01a1650f). The key's
	// meaning at the prompt is "end the input", and the REPL answers that by
	// leaving. Mid-turn the same key has to mean the same thing, and reaching
	// that meaning is what needs the cancel: the REPL's main loop is blocked
	// INSIDE the turn (ag.Chat), so the end-of-input marker it would act on
	// cannot be read until the turn returns on its own — which, against a peer
	// that never answers, is never. The measured consequence was not a slow
	// exit but a DEAF console: the reader goroutine saw the EOF, queued it and
	// retired, so from that instant NOBODY was reading the terminal and the
	// Ctrl+C that would have stopped the turn did nothing either (pty probe,
	// 2026-10-23: Ctrl+D at t+5.4s, Ctrl+C at t+35.4s, still alive at t+65s).
	// So the EOF claims the turn the way the other two keys do, and the queued
	// marker then ends the session at the turn boundary.
	OriginEOF KeyOrigin = "ctrl-d"
)

// KeyOriginSet is every defined origin, in the order a reader scans them. It
// exists so a consumer can be EXHAUSTIVE rather than a membership list that
// drifts when a member is added (Rule 13: a constant is referenced, and an
// exhaustive check is the only guard against one being bypassed) — Valid below
// ranges over it, and so does the carrier's origin→reason mapping test.
var KeyOriginSet = []KeyOrigin{OriginCtrlC, OriginEscape, OriginSignal, OriginEOF}

// Valid reports whether the origin is one of the defined values. A caller that
// records an origin it invented would poison the session JSONL's grouping (the
// file is read by scripts), so the set is closeable and this is the predicate
// that closes it.
func (o KeyOrigin) Valid() bool {
	for _, k := range KeyOriginSet {
		if o == k {
			return true
		}
	}
	return false
}
