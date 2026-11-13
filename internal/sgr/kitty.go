package sgr

// kitty.go — the kitty keyboard protocol's wire vocabulary (query / push / pop /
// reply), beside the SGR and private-mode sequences this package already owns.
//
// WHY IT LIVES HERE, and not in internal/lineedit where it is used. The
// protocol is a WIRE FORMAT with two sides: this project EMITS `CSI > 1 u` and
// must RECOGNISE the terminal's own `CSI ? 1 u` reply. internal/lineedit would
// have to spell the request and parse the reply against the same literal
// grammar, and a drift between the two spellings is invisible — the query is
// sent, the reply is not recognised, and the feature silently never turns on.
// One definition of the grammar, one parser, one constant per protocol fact
// (Rule 13/14). internal/sgr is a leaf (stdlib only), so this adds no edge.
//
// The three dispositions (kitty keyboard protocol spec, "Progressive
// enhancement"):
//
//	CSI ? u          query the current flags — the terminal answers `CSI ? flags u`
//	CSI > flags u    PUSH flags onto the terminal's keyboard-mode stack
//	CSI < number u   POP number entries (restoring whatever the flags were)
//
// The flags are a bit field; the ONE bit this project asks for is
// DisambiguateEscapeCodes (0b1), which is what makes Shift+Enter arrive as
// `CSI 13;2u` instead of the same 0x0d a bare Enter sends — the ambiguity the
// rest of the tree cannot undo, because by then the information is gone.

import (
	"strconv"
)

// FinalCSIu terminates the kitty keyboard protocol's four sequences. It is a CSI
// final byte like the rest of the block above — the terminal writes the byte
// 'u' there, not a digit — so it is named with them rather than typed at a call
// site (Rule 1b).
const FinalCSIu = 'u'

// The flags of the kitty keyboard protocol's progressive enhancement, and the
// number of stack entries a single pop restores. Only one flag bit is defined
// here: the ones this project does not ask for (report event types, alternate
// keys, all keys as escape codes, associated text) are NOT named, because a
// constant nothing sends is a claim about behaviour nobody can observe.
const (
	// KeyboardProtocolDisambiguateEscapeCodes (0b1) is the enhancement that
	// makes Shift+Enter distinguishable: with it the terminal reports the key
	// as `CSI 13;2u`, without it as a bare 0x0d — identical to Enter.
	KeyboardProtocolDisambiguateEscapeCodes = 1

	// KeyboardProtocolPushEntries is how many stack entries a push COSTS, and
	// KeyboardProtocolPopEntries how many one pop removes. They are the same
	// number by construction: the pair of sequences must be symmetric or a run
	// would leave the operator's terminal keyboard mode changed after exit.
	KeyboardProtocolPushEntries = 1
	KeyboardProtocolPopEntries  = 1

	// maxKeyboardProtocolFlags bounds the flags parameter ParseKeyboardProtocolReply
	// will accept; beyond it the sequence is malformed. A protocol bit field is not
	// going to grow past this, and the bound is what makes the parser's overflow
	// check below meaningful rather than decorative.
	maxKeyboardProtocolFlags = 1 << 30
)

// KeyboardProtocolQuerySeq asks the terminal for its current enhancement flags:
// `CSI ? u`. The terminal answers with KeyboardProtocolReplySeq's shape, or does
// not answer at all — which is the ONLY observable difference between a terminal
// that supports the protocol and one that does not (Rule 15: the caller must
// treat silence as a result, not as a pending answer).
func KeyboardProtocolQuerySeq() string {
	return CSI + string(PrivatePrefix) + string(FinalCSIu)
}

// KeyboardProtocolPushSeq pushes flags onto the terminal's keyboard-mode stack:
// `CSI > flags u`. Pushing (rather than setting absolutely) is what makes the
// restoration correct on exit: whatever the shell or a parent program had
// configured comes back when we pop, without us having to know what it was.
func KeyboardProtocolPushSeq(flags int) string {
	return CSI + string(PrivatePrefixGreater) + strconv.Itoa(flags) + string(FinalCSIu)
}

// KeyboardProtocolPopSeq pops n entries from the keyboard-mode stack: `CSI < n u`.
// A pop of an empty stack resets the flags, which is the safe direction — a
// terminal left with the disambiguation on would report keys the operator's
// other programs do not expect.
func KeyboardProtocolPopSeq(n int) string {
	return CSI + string(PrivatePrefixAlt) + strconv.Itoa(n) + string(FinalCSIu)
}

// KeyboardProtocolReplySeq is the reply a supporting terminal sends to
// KeyboardProtocolQuerySeq: `CSI ? flags u`. It exists so the EMITTER side of
// the protocol has one spelling too — a test that has to produce a reply (or a
// reader that wants to recognise one) reaches for this rather than composing the
// bytes (Rule 1b).
func KeyboardProtocolReplySeq(flags int) string {
	return CSI + string(PrivatePrefix) + strconv.Itoa(flags) + string(FinalCSIu)
}

// KeyboardProtocolParse is the three-way verdict of looking for a reply in a
// byte buffer. The middle value is the one that matters: a caller reading a
// terminal STREAM cannot tell "not a reply" from "a reply split across two
// reads" by a bool, and guessing wrong either swallows a keystroke or waits
// forever for bytes that will never come.
type KeyboardProtocolParse int

const (
	// KeyboardProtocolNoReply means these bytes are not this sequence: they
	// belong to whoever reads keys, and the caller must hand them on unchanged.
	KeyboardProtocolNoReply KeyboardProtocolParse = iota
	// KeyboardProtocolPartial means the bytes are still a prefix of a reply —
	// read more and ask again.
	KeyboardProtocolPartial
	// KeyboardProtocolComplete means a whole reply is at the head of the buffer.
	KeyboardProtocolComplete
)

// KeyboardProtocolReply is one parsed answer to a flags query: the flags the
// terminal reported and the number of bytes the reply occupied in the buffer.
type KeyboardProtocolReply struct {
	Flags int
	Bytes int
}

// ParseKeyboardProtocolReply looks for a keyboard-protocol reply — `CSI ? flags u`
// — at the HEAD of b.
//
// It is deliberately strict, and it never guesses: the caller feeds whatever is
// left of the buffer to the ONE key decoder (internal/lineedit/keys.go), so a
// parser that claimed a partial or malformed sequence would EAT a keystroke the
// operator typed during the probe. Only the exact grammar above is a reply;
// anything else is NoReply and must be replayed to the decoder in order.
//
// The empty parameter list (`CSI ? u`) is accepted as flags 0: kitty's own
// query spelling omits the parameter, and a terminal answering "no enhancements
// are on" is a terminal that HAS the protocol.
func ParseKeyboardProtocolReply(b []byte) (KeyboardProtocolReply, KeyboardProtocolParse) {
	// Prefix comparison done by hand rather than with strings.HasPrefix so a
	// buffer SHORTER than the introducer is Partial, not NoReply: a single ESC
	// on the wire is the beginning of a reply as far as this parser can tell,
	// and the caller's deadline (not this function) decides when waiting ends.
	if len(b) < len(CSI) {
		if len(CSI) >= len(b) && string(CSI[:len(b)]) == string(b) {
			return KeyboardProtocolReply{}, KeyboardProtocolPartial
		}
		return KeyboardProtocolReply{}, KeyboardProtocolNoReply
	}
	if string(b[:len(CSI)]) != CSI {
		return KeyboardProtocolReply{}, KeyboardProtocolNoReply
	}
	i := len(CSI)
	// A REPLY always carries the private prefix. `CSI u` (no prefix) is a KEY
	// event (the "report all keys as escape codes" enhancement), not an answer.
	if i >= len(b) {
		return KeyboardProtocolReply{}, KeyboardProtocolPartial
	}
	if b[i] != PrivatePrefix {
		return KeyboardProtocolReply{}, KeyboardProtocolNoReply
	}
	i++
	// flags: zero or more digits; a missing parameter means 0.
	flags := 0
	for i < len(b) && b[i] >= ParamDigitMin && b[i] <= ParamDigitMax {
		digit := int(b[i] - ParamDigitMin)
		// Overflow guard: an absurd parameter is a malformed sequence, not a
		// number this project may wrap around into a bogus flag value.
		if flags > (maxKeyboardProtocolFlags-digit)/10 {
			return KeyboardProtocolReply{}, KeyboardProtocolNoReply
		}
		flags = flags*10 + digit
		i++
	}
	if i >= len(b) {
		return KeyboardProtocolReply{}, KeyboardProtocolPartial
	}
	if b[i] != FinalCSIu {
		// A different final byte is a different sequence — the private-prefix
		// family includes `CSI ? 25 h` and `CSI > 0 c`, which are not replies.
		return KeyboardProtocolReply{}, KeyboardProtocolNoReply
	}
	return KeyboardProtocolReply{Flags: flags, Bytes: i + 1}, KeyboardProtocolComplete
}

// There is deliberately no "is this terminal supported?" predicate here: the
// capability question is answered by the caller and its answer is "did an
// ANSWER arrive" (see internal/lineedit's probe). A helper mapping the flags
// value to a verdict would invite the caller to gate on the disambiguation BIT
// instead — wrong, because a terminal that reports flags == 0 HAS implemented
// the protocol and is exactly the one that most needs the push.
