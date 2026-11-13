package lineedit

// inject.go — the KEY INJECTION seam: raw BYTES entering the editor's own input
// path, through the SAME decoder a keyboard goes through.
//
// WHY A SOURCE DECORATOR AND NOT A NEW EDITOR METHOD (the whole design in one
// paragraph). The editor's input path has exactly one shape: Run reads a Source
// (ReadEvent), and every byte it gets goes through Decoder.Feed → handleKey.
// That is where "a key" is defined — arrows resolve to KeyUp/KeyDown, \r to
// KeyEnter, \t to KeyTab — and it is where the editor's own policies live: the
// completion menu owns Enter and Up/Down, a lone ESC is the menu's close key,
// and a paste burst is classified from the timing between reads. An injection
// path that called handleKey directly, or that edited Value, would be a SECOND
// key path: it could not be the thing a keyboard is, and every one of those
// policies would have to be re-implemented beside the one that already works.
// So the injector sits at the Source boundary (below the decoder, above the
// terminal) and delivers the RAW BYTES of the key. The editor cannot tell an
// injected arrow from a typed one, because the bytes are identical and the
// decoder is the same object.
//
// ONE KEY PER READ — the framing rule, and it is not cosmetic. The editor
// classifies a paste by TWO independent laws, both of which a naive multi-byte
// write would trip:
//
//	1. PasteThreshold — one read at or above ~4 KiB is a paste by SIZE.
//	2. The burst law (Value.NoteKeys) — ONE READ carrying at least
//	   PasteBurstMinChars (3) keys from a TYPED origin arms the suppression, and
//	   an Enter arriving within the burst's window of that read is read as
//	   pasted CONTENT (a newline in the buffer) and NOT as a submit. The window
//	   depends on the EVIDENCE the read carried (Value.burstWindow): a burst the
//	   terminal FRAMED (CSI 200 ~ / 201 ~) gets PasteEnterSuppressWindow
//	   (120 ms), while one armed only by the read's SIZE — which is what a
//	   coalesced write of "/quit\r" looks like — gets PasteBurstCharInterval
//	   (8 ms). The unit is the SIZE OF ONE READ, never an accumulation across
//	   reads: a run of one-key reads is a person typing, however fast the keys
//	   arrived.
//
// A single write of "/quit\r" is exactly three contiguous characters plus an
// Enter, arriving as one read: it does not look like typing, so the \r becomes
// a newline and the line never submits — measured, and the reason this file
// delivers one key per read and ACKNOWLEDGES the editor's consumption of that
// read before it delivers the next. The acknowledgement is what puts a real
// interval between two injected keys: the editor has read key N and come back
// for N+1, which is the same round trip a keyboard's bytes make.
//
// AN INJECTED READ IS EXEMPT FROM THE BURST LAW, by origin and not by timing:
// ReadOriginInjected never arms it, and an injected Enter is never suppressed,
// so a script cannot have its submit swallowed by a window it did not open. The
// pacing above still exists, but it is no longer what stands between an
// injected key and a live read loop (see injectKeyIntervalFactor).
//
// WHAT THIS FILE DOES NOT DO: it does not press a key for the editor (the
// editor reads when it reads — an injected key queues and is consumed at the
// next read, which may be the next prompt), it does not arbitrate between an
// injected key and a real one (there is ONE queue and no priority), and it does
// not claim a key was READ until the read loop has actually taken it.

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// The injector's own bounds. Every value is named (Rule 1) because each one is
// a policy decision with a failure mode behind it, not an arbitrary size.
const (
	// injectQueueCap bounds bytes queued but NOT yet read. It is a bound on the
	// INJECTOR's own backlog, so a caller that outruns the read loop is refused
	// loudly instead of growing a queue the read loop can never drain inside the
	// session (Rule 15).
	injectQueueCap = 4096
	// injectReadBytes bounds ONE synthesized read: the buffer the read loop
	// hands us is an editReadBuf-sized slice, and a key's byte sequence is never
	// near this, so the cap only documents that a key is delivered whole.
	injectReadBytes = 64
	// injectDrainPollMS is how often InjectKey re-checks whether the read loop
	// took the key it queued.
	injectDrainPollMS = 5
	// injectKeyIntervalFactor scales the editor's OWN Enter-suppression window
	// into the gap every injected key is paced by. WHICH window, and why not the
	// intra-burst interval: the pacing predates the origin law below and is kept
	// CONSERVATIVELY, with the larger window as its base.
	//
	// THE BURST LAW NO LONGER DEPENDS ON THIS GAP (measured 2026-10, task
	// 01a189b3): an injected read carries ReadOriginInjected, so it never arms
	// the burst and an injected Enter is never suppressed — the trap this
	// constant was born from (a 3-byte ARROW arriving as ONE read, arming the
	// burst on its own, so the following Enter became a newline) is now refused
	// by ORIGIN rather than by timing.
	//
	// WHY THE GAP IS STILL HERE: it is what keeps one key per read when the
	// terminal decodes several queued bytes at once — the injector pops ONE unit
	// per ReadEvent, but two units arriving inside one poll of the real terminal
	// are delivered back to back, and the paste-store's continuation rule and
	// the completion-menu walks are both written against "a key arrives as its
	// own read". The gap is a bound on how fast a script can drive that loop,
	// not a paste workaround. Nobody has measured the tree WITHOUT it since the
	// origin law landed; that measurement is the one that would let this value
	// shrink, and until then the conservative base stays.
	injectKeyIntervalFactor = 2
	// injectDrainSlack is added to one pacing gap to bound the wait for the read
	// loop to take an injected key. It exceeds the gap by construction, so a key
	// that IS being read reports ReadByEditor=true rather than a spurious
	// "queued but unread".
	injectDrainSlack = 100 * time.Millisecond
	// injectKeyDefaultCeiling is the per-session key ceiling used when a caller
	// asks for none (interfaces take a number; a bound that is not declared is
	// not a bound).
	injectKeyDefaultCeiling = 64
)

// The SPELLING shapes a key name is written in, and the vocabulary that
// recognizes them (Rule 1b: each literal carries meaning and is compared, so it
// is named once here and referenced by the predicate, the refusal sentence and
// the tests).
//
// WHY A SHAPE PREDICATE RATHER THAN A BIGGER TABLE (the design decision, stated
// because it is the difference between a fix and a patch). The editor's own
// decoder maps sixteen control bytes to real keys (decodeControl: 0x01→Home,
// 0x15→KillToStart, …) and the injectable table names exactly ONE of them
// (ctrl-c). Extending the table would fix the sixteen spellings somebody
// enumerated, leave the seventeenth, and — worse — make `ctrl-u` a NAME, i.e.
// claim the vocabulary owns a key it deliberately does not. The defect is not
// "a key is missing from the table"; it is that a value which is plainly A
// SPELLING OF A KEY took the literal-bytes arm, so six letters of it were typed
// into the operator's line while the tool reported success. So the fix is a
// SHAPE test on the value, and the shape is what is refused — with the legal set
// named, which is the actionable half.
const (
	// injectKeyNameFormPrefix is the family prefix the vocabulary spells a
	// control key with. `ctrl-c` is a table entry; `ctrl-u` is the same family,
	// which is precisely why the shape test — not an enumeration — is what
	// closes it.
	injectKeyNameFormPrefix = "ctrl-"
	// injectEscapeSpellingLead is the character an escape spelling starts with:
	// `\u0015`, `\x15`, `\n`, `\027` are all the same class of value — a way of
	// WRITING a byte, never a byte.
	injectEscapeSpellingLead = `\`
	// injectEscapeSpellingLetters is the C-escape body vocabulary: one of these
	// alone after the lead spells a single control byte (`\n` = LF, `\t` = TAB,
	// `\e` = ESC). `\q` and `\Users` are NOT in it, which is what keeps the
	// refusal conservative (an ordinary backslash a caller means literally must
	// still be injectable).
	injectEscapeSpellingLetters = "nterabfv"
	// injectEscapeSpellingHexLead is the hex body's introducer (`\u…`, `\x…`).
	injectEscapeSpellingHexLead = "ux"
	// injectEscapeSpellingDigitsMax bounds the numeric body a spelling may
	// carry. It exists to keep the predicate from calling an ordinary
	// backslash-and-digits string a spelling, not because the value is used:
	// a body longer than this is not a reference anybody writes.
	injectEscapeSpellingDigitsMax = 8
)

// keySpellingShape reports whether v is a SPELLING of a key — a value that must
// be REFUSED rather than injected as its own bytes — and names which shape it
// matched, so the sentence can say what the caller actually passed.
//
// The shapes, in the order they are tested:
//
//  1. injectShapeCase: the value equals a table entry under case folding
//     ("Down", "Enter") — the same typo class, and the same harm: six letters
//     where a keystroke was meant.
//  2. injectShapeControl: the ctrl- family (case-insensitive), whether or not
//     the member is in the table.
//  3. injectShapeEscape: an escape spelling of one byte.
//
// IT IS CONSERVATIVE ON PURPOSE. Everything not matching a shape stays on the
// literal-bytes arm, because that arm is the seam's whole point (a rune like "/"
// opens the completion menu and no name produces it) — so a refusal that fired
// on ordinary content would take the tool away from the callers it exists for.
func keySpellingShape(v string) (string, bool) {
	if v == "" {
		return "", false
	}
	folded := strings.ToLower(v)
	if _, _, ok := injectableKeyBytes(folded); ok && folded != v {
		return injectShapeCase, true
	}
	if strings.HasPrefix(folded, injectKeyNameFormPrefix) {
		return injectShapeControl, true
	}
	if !strings.HasPrefix(v, injectEscapeSpellingLead) {
		return "", false
	}
	body := v[len(injectEscapeSpellingLead):]
	if body == "" {
		return "", false
	}
	switch {
	case strings.ContainsRune(injectEscapeSpellingHexLead, rune(body[0])):
		if isDigitBody(body[1:], isHexDigit) {
			return injectShapeEscape, true
		}
	case body[0] >= '0' && body[0] <= '7':
		if isDigitBody(body[1:], isOctalDigit) {
			return injectShapeEscape, true
		}
	case len(body) == 1 && strings.ContainsRune(injectEscapeSpellingLetters, rune(body[0])):
		return injectShapeEscape, true
	}
	return "", false
}

// The shape labels keySpellingShape returns. They are the refusal's own
// vocabulary: the sentence names WHAT the caller passed, not merely that it was
// wrong (Rule 15).
const (
	injectShapeCase    = "a key name in the wrong case"
	injectShapeControl = "a control-key name"
	injectShapeEscape  = "an escape spelling of a byte"
)

// isDigitBody reports whether s is 1..injectEscapeSpellingDigitsMax characters
// of the given digit class — the numeric body a spelling may carry.
func isDigitBody(s string, ok func(byte) bool) bool {
	if s == "" || len(s) > injectEscapeSpellingDigitsMax {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !ok(s[i]) {
			return false
		}
	}
	return true
}

// isHexDigit / isOctalDigit are the two digit classes isDigitBody is asked
// about. Hand-rolled rather than strconv so the predicate reads a body it has
// already bounded, with no error to discard (Rule 15).
func isHexDigit(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func isOctalDigit(b byte) bool { return b >= '0' && b <= '7' }

// keySpellingRefusal is the sentence a caller gets when it spells a key instead
// of naming or passing one. It names the WHOLE legal set (the same discipline the
// tool's action enum follows) and the second route — the bytes — so the caller
// can act on it without a round trip spent guessing (Rule 15).
func keySpellingRefusal(v, shape string) string {
	return fmt.Sprintf("refusing to inject %q: it is %s, not a key's bytes — injecting it would type its characters into the operator's editing line while reporting success, so it is refused instead (Rule 15). Pass a NAMED key (%s), or the key's own RAW BYTES — the byte itself as ONE character (e.g. the byte 0x15 for ctrl-u), never an escape spelling of it.",
		v, shape, strings.Join(InjectableKeyNames(), "/"))
}

// The injectable keys, ONE definition each: the name the tool wire uses and the
// RAW BYTES a keyboard emits (Rule 1b/13 — the tool, the refusal sentences and
// the tests all read these).
//
// The byte sequences are the terminal's own wire values, not a second
// vocabulary: a real arrow key arrives as ESC [ A/B/C/D, a real Backspace as
// DEL (0x7f) in raw mode (ICRNL/ICANON are cleared — see rawState), a real
// Enter as CR (0x0d — that is WHY raw mode clears ICRNL). LABEL is the KeyCode
// the editor's own decoder yields for the bytes, and a test asserts every entry
// against the decoder, so the table cannot drift from the thing it claims to
// be.
const (
	// KeyNameUp / KeyNameDown move the completion menu's selection while it is
	// open, and recall history while it is closed.
	KeyNameUp   = "up"
	KeyNameDown = "down"
	// KeyNameLeft / KeyNameRight are cursor motions inside the editing line.
	KeyNameLeft  = "left"
	KeyNameRight = "right"
	// KeyNameEnter is CR: submit, or ACCEPT the open completion menu.
	KeyNameEnter = "enter"
	// KeyNameTab is the completion-accept key.
	KeyNameTab = "tab"
	// KeyNameBackspace / KeyNameDelete edit the line (the first also clears the
	// line at column 0 — the shortcut a user has).
	KeyNameBackspace = "backspace"
	KeyNameDelete    = "delete"
	// KeyNameEscape is a LONE Escape: the completion menu's close key (the
	// decoder resolves the ambiguity with an escape-sequence prefix by the
	// editor's bounded ESC wait, exactly as it does for a keyboard).
	KeyNameEscape = "escape"
	// KeyNameHome / KeyNameEnd are the line's two ends.
	KeyNameHome = "home"
	KeyNameEnd  = "end"
	// KeyNameCtrlC is the interrupt byte (0x03). In raw mode ISIG is cleared, so
	// it arrives as a BYTE and the editor's own handler decides: it ends the
	// editing session (Result.Interrupted). It is the one injectable key whose
	// effect is to close the surface.
	KeyNameCtrlC = "ctrl-c"
)

// injectedKey is one row of the table.
type injectedKey struct {
	name  string
	bytes []byte
	label KeyCode
}

// injectableKeys is the table. The BYTES are composed from the escape
// vocabulary the rest of the package writes (escCSI), so a change to the CSI
// prefix cannot leave this table speaking the old one.
var injectableKeys = []injectedKey{
	{KeyNameUp, []byte(escCSI + "A"), KeyUp},
	{KeyNameDown, []byte(escCSI + "B"), KeyDown},
	{KeyNameRight, []byte(escCSI + "C"), KeyRight},
	{KeyNameLeft, []byte(escCSI + "D"), KeyLeft},
	{KeyNameEnter, []byte{crByte}, KeyEnter},
	{KeyNameTab, []byte{tabByte}, KeyTab},
	{KeyNameBackspace, []byte{delByte}, KeyBackspace},
	{KeyNameDelete, []byte(escCSI + "3~"), KeyDelete},
	{KeyNameEscape, []byte{escByte}, KeyEscape},
	{KeyNameHome, []byte(escCSI + "H"), KeyHome},
	{KeyNameEnd, []byte(escCSI + "F"), KeyEnd},
	{KeyNameCtrlC, []byte{ctrlC}, KeyInterrupt},
}

// InjectableKeyNames lists the injectable key names, sorted, for the sentence a
// caller gets when it names a key that does not exist (a refusal names the
// valid list — the same discipline the tool's action enum follows).
func InjectableKeyNames() []string {
	names := make([]string, 0, len(injectableKeys))
	for _, k := range injectableKeys {
		names = append(names, k.name)
	}
	sort.Strings(names)
	return names
}

// injectableKeyBytes resolves a key name to its raw bytes.
func injectableKeyBytes(name string) ([]byte, KeyCode, bool) {
	for _, k := range injectableKeys {
		if k.name == name {
			return k.bytes, k.label, true
		}
	}
	return nil, KeyNone, false
}

// InjectResult is one injection's outcome, reported as FACTS rather than as a
// sentence: the surface that owns the console decides how to say it, and the
// tool decides what the model reads.
//
// ReadByEditor is deliberately separate from Accepted: a key can be queued while
// the editor is not reading (the turn that injects it may BE the turn in
// flight), and reporting that as "typed" would be a claim nothing measured.
type InjectResult struct {
	// Name is the key that was injected.
	Name string
	// Accepted reports that the bytes are queued (or were already consumed).
	Accepted bool
	// ReadByEditor reports that the read loop consumed the key before this call
	// answered.
	ReadByEditor bool
	// Pending is how many bytes were still queued when this call answered.
	Pending int
	// Used is how many keys this session has injected, INCLUDING this one.
	Used int
	// Bound is the session ceiling in force.
	Bound int
	// Last reports that this key spent the last of the ceiling.
	Last bool
	// Why carries the sentence when Accepted is false.
	Why string
}

// injectQueue is the FIFO of pending read units. A UNIT is the byte group one
// InjectKey queues, and delivering a unit whole-or-in-chunks is what keeps the
// "one key per read" framing true (see the file header).
type injectQueue struct {
	mu     sync.Mutex
	data   []byte
	ends   []int // ascending offsets: the end of each pending unit
	closed bool
}

// put appends one unit. It refuses (loudly) when the backlog bound would be
// exceeded, so a caller cannot outrun the read loop silently.
func (q *injectQueue) put(b []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return fmt.Errorf("the editor's input path is closed — this run is no longer reading keys")
	}
	if len(q.data)+len(b) > injectQueueCap {
		return fmt.Errorf("the injected-key backlog is full (%d bytes queued, bound %d): the editor has not read the previous keys yet", len(q.data), injectQueueCap)
	}
	q.data = append(q.data, b...)
	q.ends = append(q.ends, len(q.data))
	return nil
}

// take pops the next read unit into buf, capped by the destination's size. ok is
// false when nothing is queued.
func (q *injectQueue) take(buf []byte) (int, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.data) == 0 {
		return 0, false
	}
	n := q.ends[0]
	if n > len(buf) {
		n = len(buf)
	}
	if n > injectReadBytes {
		n = injectReadBytes
	}
	copy(buf, q.data[:n])
	q.data = q.data[n:]
	for i := range q.ends {
		q.ends[i] -= n
	}
	if q.ends[0] <= 0 {
		q.ends = q.ends[1:]
	}
	if len(q.data) == 0 {
		q.data, q.ends = nil, nil
	}
	return n, true
}

// pending reports whether bytes are queued.
func (q *injectQueue) pending() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.data) > 0
}

// length reports the queue's byte length (the number a notice quotes).
func (q *injectQueue) length() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.data)
}

// close stops accepting keys (the editor is gone).
func (q *injectQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
}

// Injector is the Source decorator: it hands the editor everything the inner
// source does, PLUS the bytes an in-process caller injects. It is a Source, so
// it is installed exactly where a terminal is (NewChatEditor(term, …)), and the
// editor's decoder is unchanged behind it.
type Injector struct {
	inner Source
	q     *injectQueue
	// seam is the inner source's wake notifier, resolved once (see notifier).
	seam Notifier

	mu   sync.Mutex
	used int
	// session is the ceiling this process DECLARED for one session — the value
	// the constructor was given. It is immutable after construction: a bound a
	// caller can raise is not a bound (the tool's clamp is a second line of
	// defence behind this one, never the only one).
	session int
	// bound is the ceiling in force for the CURRENT PERIOD — the stretch between
	// two lines a human wrote. It starts at `session` and ResetKeys
	// re-establishes it (see ResetKeys: the reset is the carrier's, never a tool
	// action).
	bound int
	// lastOrigin records WHICH ARM served the read ReadEvent last returned — the
	// injected queue or the inner source. It is the one fact a caller above
	// cannot recover: both arms hand back bytes and an identical Event, and a
	// consumer that must distinguish a script's keystroke from a human's
	// (keysource.go) needs the answer from the object that knows it. Guarded by
	// mu; read by ReadOrigin.
	lastOrigin ReadOrigin

	// lastAt is when the previous key's bytes were DELIVERED to the editor.
	// It is the injector's half of the framing guarantee: the read loop must not
	// get two units back to back, or they are read as ONE batch (measured on the
	// eval harness before this field existed). Holding the next unit until
	// injectKeyInterval() has passed on the editor's clock is what makes an
	// injected key its own read. NOTE (2026-10, the origin law): the gap is no
	// longer what protects an injected Enter from the paste burst — origin does
	// that — it keeps the framing, i.e. one key per read.
	lastAt time.Time
}

// Compile-time proof that the injector is a source the editor can read.
var _ Source = (*Injector)(nil)

// injectKeyInterval is the gap the injector keeps between two keys: the editor's
// own Enter-suppression window, scaled (see injectKeyIntervalFactor).
func injectKeyInterval() time.Duration {
	return PasteEnterSuppressWindow * injectKeyIntervalFactor
}

// injectDrainBudget is how long InjectKey waits for the read loop to take the key
// it queued: one pacing gap plus a margin, so a key that is really being read is
// reported as read rather than as a spurious timeout. The expiry is NOT an error
// — the editor may legitimately not be reading yet (the turn that injects a key
// IS the turn in flight), and that outcome is reported as ReadByEditor=false.
func injectDrainBudget() time.Duration {
	return injectKeyInterval() + injectDrainSlack
}

// NewInjector wraps src so an in-process caller can inject keys. ceiling bounds
// how many keys ONE session may inject (<= 0 selects injectKeyDefaultCeiling);
// it is per-injector, and the injector is built once per chat session, which is
// what "per session" means here.
func NewInjector(src Source, ceiling int) *Injector {
	if ceiling <= 0 {
		ceiling = injectKeyDefaultCeiling
	}
	return &Injector{inner: src, q: &injectQueue{}, session: ceiling, bound: ceiling}
}

// InjectKey queues the named key's RAW BYTES as ONE read unit and waits (briefly)
// InjectKey queues ONE key as raw bytes and waits (briefly) for the editor's
// read loop to take it. The argument is resolved by ONE documented rule, so a
// caller never has to guess which half it is in:
//
//  1. the value IS a named key (see InjectableKeyNames) → that key's wire bytes;
//  2. anything else → the value's OWN BYTES, literally.
//
// It is name-first rather than a syntax (no prefix, no escaping grammar) because
// the vocabulary is small, closed, and asserted entry-by-entry against the
// decoder — and because a caller wanting a literal "/" or "q" has no name it could
// be shadowed by. The consequence is stated, not hidden: a value that is exactly a
// key's NAME cannot be injected as those literal letters; inject them one byte at
// a time (InjectBytes) if that is what is wanted.
//
// The ceiling is charged on ACCEPTANCE, not on consumption: a key that is
// queued has been spent, and a budget that only counted keys the editor had
// already read would let a caller with a slow read loop queue the whole bound
// and then some. When this key spends the last of the ceiling, Last is set — the
// caller reports the exhaustion on the same call that consumed it, never one key
// later (a bound whose arrival is discovered after the fact is the silent
// exhaustion this epic forbids).
func (inj *Injector) InjectKey(name string) InjectResult {
	return inj.InjectKeyBounded(name, 0)
}

// InjectKeyBounded is InjectKey with the PERIOD's own ceiling supplied by the
// caller (0 = the injector's constructor ceiling). It exists because the two
// numbers are different things and the caller is the only one that knows the
// first:
//
//   - the CONSTRUCTOR ceiling is this process's declaration of "how many keys
//     one session may inject at all";
//   - the PERIOD ceiling is what the tool's caller asked for (`max_keys`),
//     clamped into the declared range by the tool, and it must bind or the knob
//     is decoration.
//
// A caller-supplied bound can only TIGHTEN the period (see tightestPeriodBound):
// a caller must not be able to raise the process's own declaration by passing a
// larger number, and the tool's clamp is a second line of defence behind that
// rule, never the only one.
func (inj *Injector) InjectKeyBounded(name string, bound int) InjectResult {
	if name == "" {
		return injectUnitResult(inj, "", false, bound, "no key: pass a key name or the raw bytes of one key")
	}
	// THE RAW VALUE IS TRIED FIRST, and that order is load-bearing: `" "` and a
	// bare LF are ONE BYTE of real input, and a trim applied before the lookup
	// turns both into the empty string — i.e. into "no key" — so the two most
	// ordinary bytes in a line (a space, a newline in a multiline draft) could not
	// be injected at all. Measured on the pre-fix seam: InjectKey(" ") and
	// InjectKey("\n") both answered `no key: pass a key name or the raw bytes of
	// one key`, which is the seam denying the very thing it asks for (Rule 15).
	if seq, _, ok := injectableKeyBytes(name); ok {
		return inj.injectUnit(name, seq, bound)
	}
	// The model's own padding is forgiven on the NAME arm only (it is the arm that
	// reads a word), so a caller that sends " up " still gets the key…
	trimmed := strings.TrimSpace(name)
	if trimmed != "" && trimmed != name {
		if seq, _, ok := injectableKeyBytes(trimmed); ok {
			return inj.injectUnit(trimmed, seq, bound)
		}
	}
	// …and a SPELLING of a key is refused, never injected as its own characters
	// (see keySpellingShape for what the shapes are and why a shape test rather
	// than a longer enumeration closes them). The refusal charges NOTHING: it is
	// not an injection, and a refusal that spent the caller's budget would be a
	// second silent cost on the same call.
	if shape, ok := keySpellingShape(trimmed); ok {
		return injectUnitResult(inj, name, false, bound, keySpellingRefusal(name, shape))
	}
	return inj.InjectBytesBounded(name, bound)
}

// InjectBytes queues RAW BYTES as ONE read unit — the same path InjectKey takes
// with the sequence supplied by the caller instead of named. It is what makes
// this a BYTE-level seam rather than a menu of shortcuts: a caller can inject a
// rune (the "/" that opens the completion menu), a whole escape sequence the
// table does not name, or a sequence of its own — and every one is framed, paced
// and charged exactly as a named key is.
//
// WHY THIS IS NOT action=type: this path delivers BYTES to the editor's DECODER
// as one keystroke's worth of input, so the editor's own policy decides what they
// mean (a rune lands in the buffer, a CSI sequence is a motion, CR submits).
// action=type enqueues a whole LINE as the next TURN and never enters the editor.
func (inj *Injector) InjectBytes(raw string) InjectResult {
	return inj.InjectBytesBounded(raw, 0)
}

// InjectBytesBounded is InjectBytes with an explicit period ceiling (see
// InjectKeyBounded for why the two bounds are separate).
func (inj *Injector) InjectBytesBounded(raw string, bound int) InjectResult {
	if raw == "" {
		return injectUnitResult(inj, "", false, bound, "no bytes to inject: pass the raw bytes of one key (a rune, or an escape sequence)")
	}
	if len(raw) > injectReadBytes {
		return injectUnitResult(inj, raw, false, bound, fmt.Sprintf("the byte sequence is %d bytes, beyond one key's bound of %d — one injected key is one read unit",
			len(raw), injectReadBytes))
	}
	return inj.injectUnit(raw, []byte(raw), bound)
}

// periodBound resolves the ceiling ONE injection is charged against: the
// caller's period bound when it is positive, capped at this process's own
// declaration. One resolver, so the refusal sentence, the charge and the
// reported bound can never disagree about the number.
// latchPeriodBound resolves AND FIXES the ceiling one injection is charged
// against. It is the only writer of `bound` after construction: the period's
// FIRST key fixes the period's ceiling (tightening this session's declaration
// when the caller asked for less, and NEVER raising it), and every later key in
// the same period inherits that number.
//
// WHY THE LATCH MATTERS, and it is the reason this is not a plain min() of the
// two arguments (measured: with a per-call min, a spent 4-key period answered a
// following call carrying `max_keys: 64` by raising itself back to 64 and
// accepting the key — the model had found a way to renew its own budget one
// operand at a time, which is the exact shape the bound exists to forbid). With
// the latch, the ceiling in force is decided ONCE per period, at its first key,
// by the caller who opened it.
func (inj *Injector) latchPeriodBound(caller int) int {
	inj.mu.Lock()
	defer inj.mu.Unlock()
	return inj.latchPeriodBoundLocked(caller)
}

func (inj *Injector) latchPeriodBoundLocked(caller int) int {
	if caller <= 0 {
		return inj.bound
	}
	if inj.used > 0 {
		// The period is under way: its ceiling was fixed by its first key.
		return inj.bound
	}
	inj.bound = inj.session
	if caller < inj.session {
		inj.bound = caller
	}
	return inj.bound
}

// periodBound reports the ceiling in force for a caller that is NOT injecting
// (a refusal before the queue is touched, or a report). It never moves the
// number: a refusal must not be the event that decides a period's ceiling, or a
// rejected call could tighten a period that already started.
func (inj *Injector) periodBound(caller int) int {
	inj.mu.Lock()
	defer inj.mu.Unlock()
	if inj.used > 0 || caller <= 0 {
		return inj.bound
	}
	if caller < inj.session {
		return caller
	}
	return inj.session
}

// injectUnitResult builds the refusal shape for a unit that was never queued (an
// unknown key, an empty one, or an over-long sequence).
func injectUnitResult(inj *Injector, label string, accepted bool, bound int, why string) InjectResult {
	return InjectResult{
		Name: label, Accepted: accepted, Why: why,
		Pending: inj.q.length(), Used: inj.Used(), Bound: inj.periodBound(bound),
	}
}

// injectUnit is the ONE injection path: charge the ceiling, queue the bytes as a
// single read unit, wait for the read loop, and report what happened.
//
// The ceiling is charged on ACCEPTANCE, not on consumption: a key that is queued
// has been spent, and a budget that only counted keys the editor had already read
// would let a caller with a slow read loop queue the whole bound and then some.
// When this unit spends the last of the ceiling, Last is set — the exhaustion is
// reported on the SAME call that consumed it, never one unit later (a bound whose
// arrival is discovered after the fact is the silent exhaustion this seam
// forbids).
func (inj *Injector) injectUnit(label string, seq []byte, callerBound int) InjectResult {
	used, bound := inj.Used(), inj.latchPeriodBound(callerBound)
	refuse := func(why string) InjectResult {
		return InjectResult{Name: label, Accepted: false, Used: used, Bound: bound, Pending: inj.q.length(), Why: why}
	}
	if used >= bound {
		return refuse(fmt.Sprintf("this period's key ceiling is reached (%d of %d keys injected) — it does not raise itself, and only a line the OPERATOR submits opens a fresh period", used, bound))
	}
	if err := inj.q.put(seq); err != nil {
		return refuse(err.Error())
	}
	// WAKE THE PARKED READ: the read loop's wait is unbounded whenever nothing
	// is outstanding, so a queued key that did not notify anyone would wait for
	// the next real keystroke (measured: 3 bytes sat unread for a full second).
	inj.wake()
	inj.mu.Lock()
	inj.used++
	used = inj.used
	inj.mu.Unlock()

	read := inj.awaitDrain()
	return InjectResult{
		Name: label, Accepted: true, ReadByEditor: read,
		Pending: inj.q.length(), Used: used, Bound: bound,
		Last: used >= bound,
	}
}

// ResetKeys opens a FRESH key period: the used count goes back to zero and, if
// the caller asks for one, a new period ceiling takes effect. It is the action
// side of the contract ChatSurfaceUserTurn implements (see that function for
// WHICH arm may call it and why the answer must come from construction, never
// from inspecting the value).
//
// IT IS NOT A TOOL ACTION and there is deliberately no wire spelling of it: the
// counter it moves is the guarantee that a model cannot spend an unbounded
// number of keystrokes, and a bound the bounded party can lift is not a bound.
// The only caller is the carrier, at the boundary where it holds a line the
// model did not write.
//
// The CEILING a reset leaves in force is this process's own DECLARATION again
// (the constructor's value), optionally tightened by a period bound the caller
// supplies. Two consequences, both deliberate: a period that asked for fewer
// keys does not become the session's new normal, and a reset can never RAISE the
// ceiling — the only direction it may move is down, and only to a value the
// caller had already asked for. One reader (periodBound) decides the number
// actually charged.
func (inj *Injector) ResetKeys(periodBound int) {
	inj.mu.Lock()
	defer inj.mu.Unlock()
	inj.used = 0
	inj.bound = inj.session
	if periodBound > 0 && periodBound < inj.session {
		inj.bound = periodBound
	}
}

// InjectKeys queues several keys in order, stopping at the first refusal. It
// exists so a caller that wants "Down, then Enter" pays one round trip's worth of
// reasoning instead of two calls' worth of prose; the framing (one key per read)
// is unchanged, because each key is queued as its own unit.
func (inj *Injector) InjectKeys(names ...string) []InjectResult {
	out := make([]InjectResult, 0, len(names))
	for _, n := range names {
		res := inj.InjectKey(n)
		out = append(out, res)
		if !res.Accepted {
			break
		}
	}
	return out
}

// awaitDrain waits, within injectDrainBudget, for the read loop to take the
// queued key. A timeout is NOT an error (see InjectResult.ReadByEditor).
func (inj *Injector) awaitDrain() bool {
	deadline := time.Now().Add(injectDrainBudget())
	for {
		if !inj.q.pending() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(injectDrainPollMS * time.Millisecond)
	}
}

// Pending reports how many injected bytes the editor has not read yet.
func (inj *Injector) Pending() int { return inj.q.length() }

// Used reports how many keys this session has injected, and Bound the ceiling in
// force — the pair a caller prints so the ceiling's approach is visible.
func (inj *Injector) Used() int {
	inj.mu.Lock()
	defer inj.mu.Unlock()
	return inj.used
}

// Bound reports the session ceiling in force.
func (inj *Injector) Bound() int {
	inj.mu.Lock()
	defer inj.mu.Unlock()
	return inj.bound
}

// Close stops accepting injected keys. It is called when the editor is torn
// down, so a key queued after the read loop is gone is refused instead of
// sitting in a queue nobody will ever read.
func (inj *Injector) Close() { inj.q.close() }

// ReadEvent delivers a queued key first, then the inner source's own event.
//
// IT DOES NOT SLICE THE CALLER'S READ, and the reason is a defect worth recording.
// The first design polled the inner source in fixed slices so queued bytes would
// be delivered promptly. Two things were wrong with it:
//
//  1. It rewrote the read's CONTRACT. A source may treat a bounded wait and an
//     unbounded one differently (the chat harness's script source expires a
//     bounded wait immediately and parks an unbounded one until its stop
//     arrives), so slicing made its stop path unreachable and the editor's read
//     loop never returned — "the editor's read loop did not return: the caller's
//     writes cannot be attributed" (measured).
//  2. It fought the editor's own design. The read wait is UNBOUNDED whenever
//     nothing is outstanding (editor.go decides `pollTimeoutBlock`), so there is
//     no poll to hide a queued key inside: with the slicing removed, a queued key
//     sat unread for a full second behind a parked read (measured).
//
// The mechanism that actually exists for this is the source's WAKE SEAM, and the
// injector now uses it: queueing a key notifies the inner source, the parked read
// returns EventWake, and the editor comes back here to take the key. The wake is
// a notification, not a different read — the caller's timeout still passes
// through untouched, and the injector only EVER wakes a source that has a real
// seam (NotifyEnabled() false means the seam is a no-op and the injector falls
// back to the inner read's own cadence).
//
// THE PACING LIVES HERE, at delivery. The gap that must be kept is between two
// READS (that is what the paste classifier measures), so the source that performs
// the reads is the only place that can guarantee it: a not-yet-due key is held
// with a park bounded by the caller's own wait, then delivered.
func (inj *Injector) ReadEvent(buf []byte, timeoutMS int) (Event, error) {
	if len(buf) == 0 {
		return Event{}, nil
	}
	if n, ok := inj.takeReady(buf); ok {
		inj.noteOrigin(ReadOriginInjected)
		return Event{Kind: EventInput, Bytes: n}, nil
	}
	if inj.hasPending() {
		// A key is queued but still being paced. Spend the remaining gap inside
		// the caller's own wait, then deliver it.
		inj.parkUntilDue(timeoutMS)
		if n, ok := inj.takeReady(buf); ok {
			inj.noteOrigin(ReadOriginInjected)
			return Event{Kind: EventInput, Bytes: n}, nil
		}
	}
	ev, err := inj.inner.ReadEvent(buf, timeoutMS)
	// THE INNER ARM IS THE TERMINAL'S, even when the event carries no bytes (a
	// resize, a wake): the editor asks for the origin immediately after this
	// call, and an unanswered question would default to "typed" — which is the
	// RIGHT answer for every arm that is not the queue, so this records it
	// explicitly rather than relying on the zero value surviving the read.
	inj.noteOrigin(ReadOriginTyped)
	return ev, err
}

// noteOrigin records which arm served the read just returned.
func (inj *Injector) noteOrigin(o ReadOrigin) {
	inj.mu.Lock()
	inj.lastOrigin = o
	inj.mu.Unlock()
}

// ReadOrigin implements ReadOriginSource: it reports the arm that served the
// read ReadEvent last returned. It is meaningful immediately after that call, on
// the same goroutine (the editor's own use), and it is the ONLY public way to
// tell a script's keystroke from a human's — the bytes themselves are identical
// by design.
func (inj *Injector) ReadOrigin() ReadOrigin {
	inj.mu.Lock()
	defer inj.mu.Unlock()
	return inj.lastOrigin
}

// wake notifies the inner source that data is waiting, so a PARKED read returns
// and the editor comes back for the key. It goes through the package's ONE
// resolver (SourceNotifier), so both spellings a source may use are honoured, and
// it is silent when the source has no real seam (NotifyEnabled() false) — the key
// is still queued, it is simply taken on the inner read's own cadence.
func (inj *Injector) wake() {
	n := inj.notifier()
	if n == nil || !n.NotifyEnabled() {
		return
	}
	n.Notify()
}

// notifier is the source's wake seam, resolved once. It is the SAME seam the
// editor resolves for its own publishes, so a publish by this decorator cannot
// end up on a channel no parked reader watches.
func (inj *Injector) notifier() Notifier {
	inj.mu.Lock()
	defer inj.mu.Unlock()
	if inj.seam == nil {
		inj.seam = SourceNotifier(inj.inner)
	}
	return inj.seam
}

// hasPending reports whether a key is queued but not yet due.
func (inj *Injector) hasPending() bool {
	inj.mu.Lock()
	defer inj.mu.Unlock()
	return inj.q.length() > 0
}

// parkUntilDue waits until the pacing gap has elapsed, never past the caller's
// own wait (a negative timeoutMS is an unbounded wait).
func (inj *Injector) parkUntilDue(timeoutMS int) {
	inj.mu.Lock()
	wait := injectKeyInterval() - time.Since(inj.lastAt)
	inj.mu.Unlock()
	if wait <= 0 {
		return
	}
	if timeoutMS >= 0 {
		if budget := time.Duration(timeoutMS) * time.Millisecond; wait > budget {
			wait = budget
		}
	}
	time.Sleep(wait)
}

// takeReady pops the next read unit into buf, but ONLY once the pacing gap since
// the previous delivery has elapsed (see ReadEvent). A unit held back is not
// lost: the read loop comes back for it.
func (inj *Injector) takeReady(buf []byte) (int, bool) {
	inj.mu.Lock()
	wait := injectKeyInterval() - time.Since(inj.lastAt)
	inj.mu.Unlock()
	if wait > 0 {
		return 0, false
	}
	n, ok := inj.q.take(buf)
	if !ok {
		return 0, false
	}
	inj.mu.Lock()
	inj.lastAt = time.Now()
	inj.mu.Unlock()
	return n, true
}

// Width delegates the geometry to the inner source (the injector has none of its
// own).
func (inj *Injector) Width(fallback int) int { return inj.inner.Width(fallback) }

// Height delegates the geometry to the inner source.
func (inj *Injector) Height(fallback int) int { return inj.inner.Height(fallback) }

// InputPending reports the inner source's backlog OR the injected one, so the
// read loop's ordering guarantee (input already waiting is read before a
// publish is drawn) covers injected keys too.
func (inj *Injector) InputPending() bool {
	if inj.q.pending() {
		return true
	}
	if b, ok := inj.inner.(inputBacklog); ok {
		return b.InputPending()
	}
	return false
}

// WakeNotifier forwards the inner source's wake seam. It is forwarded, never
// re-invented, and it is resolved through the package's ONE resolver
// (SourceNotifier) rather than by asking for one spelling: a source may carry its
// notifier EITHER as the Notifier interface itself OR behind a WakeNotifier()
// accessor, and a decorator that knew only the second silently downgraded every
// publish to "visible on the next keystroke" for the first — measured: the chat
// harness's script source implements Notify/NotifyEnabled directly, so the
// reserved status row stopped refreshing and the live-hint case failed with "the
// harness's source supplied no wake seam". The resolver exists precisely so this
// mistake cannot be made twice.
//
// A source with no seam of its own answers with the no-op sink, whose
// NotifyEnabled() says so (Rule 15).
func (inj *Injector) WakeNotifier() Notifier {
	if n := inj.notifier(); n != nil {
		return n
	}
	return noNotify{}
}
