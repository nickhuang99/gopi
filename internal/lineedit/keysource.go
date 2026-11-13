package lineedit

// keysource.go — WHO A READ'S BYTES CAME FROM, and the OWNER's seat at the key
// seam.
//
// TWO FACTS LIVE HERE, and they are the two halves of one problem. A chat
// surface driven by a script (`gopi chat --replay`) has to be able to (a) tell
// the keys the SCRIPT delivered from the keys a HUMAN typed, and (b) act on the
// human's keys itself — pause the run, step it, leave it — without opening a
// SECOND reader on the terminal and without inventing a key vocabulary the
// editor does not have.
//
// (a) ORIGIN. The injector (inject.go) is the only path by which raw bytes enter
// the editor without a terminal, and the editor cannot tell its bytes from a
// keyboard's — that is the seam's whole point. So the distinction is made where
// it is KNOWN: the injector says which of its two arms served the read it just
// returned, the editor asks its source at the moment it decodes, and every
// decoded Key carries the answer in Key.Origin. Nothing downstream has to guess
// from timing, from content, or from a second channel.
//
// (b) THE SEAT. The editor's key loop is the one place a decoded key becomes an
// action, so it is the one place an owner can be OFFERED a key before that
// happens (KeyGate.GateKey). A run that owns the surface — a replay driver with
// playback controls — consumes the keys IT defines and lets every other key
// through unchanged, instead of re-reading the terminal (which would steal bytes
// from the decoder that already owns them) or editing the buffer from outside
// (which is a second writer of the layout's only writer).
//
// BOTH ARE NO-OPS BY DEFAULT, and that is a property of the code, not of a
// promise: a source that does not implement ReadOriginSource reports
// ReadOriginTyped directly, and an editor with no gate installed calls nothing
// at all (the gate is a nil interface, tested once per read, never a stub that
// gets called and returns false).

// ReadOrigin says where the bytes of ONE read came from.
type ReadOrigin uint8

const (
	// ReadOriginTyped is a read delivered by the terminal the editor was opened
	// on — a human's keystroke, or a paste they made. It is the ZERO value on
	// purpose: every existing caller, and every path in this package that has no
	// opinion about provenance, means exactly what it meant before this file
	// existed (Rule 15a — a zero value carrying the historic meaning cannot be
	// mistaken for a missing one).
	ReadOriginTyped ReadOrigin = iota
	// ReadOriginInjected is a read delivered by an in-process injector
	// (Injector.ReadEvent): a recorded turn being typed back, a tool driving the
	// surface, a test. It is NEVER a human's key.
	ReadOriginInjected
)

// ReadOriginSource is the optional seam a SOURCE implements when it can say
// where the bytes of its LAST read came from. The editor asks for it once per
// read; a source that does not implement it (every terminal, every test fixture
// that has not been taught otherwise) reports ReadOriginTyped.
type ReadOriginSource interface {
	// ReadOrigin reports the origin of the read ReadEvent just returned. It is
	// only meaningful IMMEDIATELY after that call, on the same goroutine — which
	// is how the editor uses it (read, then decode).
	ReadOrigin() ReadOrigin
}

// KeyGate is the OWNER's seat at the editor's key seam: the key loop offers it
// every decoded key, in the order the keys arrived, BEFORE the editor acts.
//
// IT IS INSTALLABLE, NOT IMPLEMENTED. The editor carries a nil interface by
// default and then calls nothing — an owner that wants a seat installs one
// (SetKeyGate). Three methods, one question each:
//
//	GateKey(k)      Is this key YOURS? true = the owner consumed it, and the
//	                editor must NOT apply it (no buffer edit, no menu motion, no
//	                submit). false = the owner does not want it and the editor
//	                proceeds exactly as it did before this seam existed.
//	KeyUnclaimed(k) The editor is NOT going to act on this key anyway (a decoded
//	                key with no handler — an Alt-modified rune, the one shape the
//	                decoder yields that the editor ignores). It is reported AFTER
//	                GateKey declined it, so an owner can claim such a key by
//	                returning true there and only sees this when it did not.
//	KeyRelease(k)   The editor IS going to act on this key (it was neither
//	                consumed by the gate nor ignored by the editor). It is the
//	                owner's notification that the run moved on, carrying the same
//	                Key — so Key.Origin says whether a human's keystroke or the
//	                script's own typing moved it.
//
// THE CONTRACT IS TOTAL AND DISJOINT: for every key of every read the owner is
// offered exactly one of {consumed at GateKey, unclaimed at KeyUnclaimed,
// released at KeyRelease}, in arrival order. That is what makes "the owner's
// bookkeeping counted every key exactly once" assertable rather than described.
type KeyGate interface {
	GateKey(k Key) bool
	KeyUnclaimed(k Key)
	KeyRelease(k Key)
}

// editorIgnoresKey reports whether the editor has NO handler for a key: the
// decoder yields it, the key loop passes it by, and the surface behaves as if it
// had never arrived. It is the predicate behind KeyGate.KeyUnclaimed, and it is
// deliberately ONE definition — an owner told "the editor ignores this" must be
// told it for the same keys the editor really does ignore (Rule 13).
func editorIgnoresKey(k Key) bool {
	// A decoded key the loop has no arm for: an Alt-modified rune (the read
	// loop's rune handling is `k.Code == KeyRune && !k.Alt`) and KeyNone (which
	// Feed never yields, and which an owner driving handleKey by hand may).
	if k.Code == KeyNone {
		return true
	}
	return k.Code == KeyRune && k.Alt
}
