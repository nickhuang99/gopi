package lineedit

import (
	"strings"
	"sync/atomic"
	"time"
	"unicode"
)

// Paste-burst detection. The values are the POSIX-terminal paste heuristic
// codex uses (PASTE_BURST_MIN_CHARS = 3, PASTE_ENTER_SUPPRESS_WINDOW = 120 ms —
// codex paste_burst.rs:157-161), because they are the measured behaviour of real
// terminals: a terminal pastes a whole block in ONE write, i.e. one read, while
// a human typing delivers a key per read. They are named (Rule 1) so the window
// this defect turns on can be read in one place.
//
// PASTE_BURST_CHAR_INTERVAL survives as PasteBurstCharInterval, and says what a
// gap between reads MEANS — but it no longer decides the classification on its
// own: a run of reads close together is not a paste (that is what put a fast
// human, an IME commit and a key-repeat in the paste class), only one read big
// enough is. Callers that need to space keystrokes apart (internal/shot's text
// capture) still derive their gap from it.
const (
	// PasteBurstMinChars is how many keys ONE READ must carry before the input
	// counts as a paste rather than typing.
	//
	// THE UNIT IS THE READ, NOT A RUN OF READS — and that distinction is the
	// whole fix this constant now backs. The classifier used to accumulate
	// characters across reads whose gaps were under PasteBurstCharInterval,
	// which cannot tell a paste from a fast human (or an IME committing a
	// word, or a key-repeat): three keystrokes in 24 ms armed the burst and the
	// operator's next Enter became a newline with no explanation. A terminal
	// delivers a REAL paste as one write, i.e. one read, so "one read carries
	// at least this many keys" is the honest test, and it is also the property
	// the answer to "pasted N lines = one message" rests on: a single written
	// block still arms the burst, so a newline inside it is still content.
	PasteBurstMinChars = 3
	// PasteBurstCharInterval is the largest gap between two READS that still
	// counts as the same arrival. It no longer classifies anything by itself
	// (NoteKeys decides from ONE read's size — see PasteBurstMinChars); it
	// states what a gap means, and callers that must space keystrokes apart
	// derive their pacing from it (internal/shot's text capture).
	PasteBurstCharInterval = 8 * time.Millisecond
	// PasteEnterSuppressWindow is how long after the END of a burst an Enter is
	// still read as content (a newline inside the paste) rather than as a
	// submit. This is the window that turns "pasted N lines = N turns" into
	// "pasted N lines = one message with N lines" — the defect it closes shows
	// up with text whose line endings are CRLF, where every pasted CR is, byte
	// for byte, the Enter key.
	//
	// IT IS THE WINDOW FOR A BURST THE TERMINAL FRAMED (CSI 200 ~ / 201 ~), and
	// only for that one. 120 ms is a statement about how a paste ARRIVES — the
	// block's own bytes are all in flight at once — and it is only defensible
	// for a burst the terminal itself bracketed. A burst armed on read SIZE
	// alone (see NoteKeys) has no such statement behind it, so it is judged by
	// PasteBurstCharInterval instead: the gap this package already defines as
	// "the same arrival". The value below is untouched; the EVIDENCE the read
	// carries decides which of the two applies (Value.burstWindow). See
	// PasteBurstCharInterval's own note for the measured defect this scoping
	// closes.
	PasteEnterSuppressWindow = 120 * time.Millisecond
)

// Buffer bounds: a message is not a file, and an unbounded buffer lets a
// pasted megabyte be sent as one turn (and, before the bound, an unbounded
// string builder grow under the renderer).
const (
	// valueMaxRunesDefault caps the buffer length in runes.
	valueMaxRunesDefault = 64 * 1024
	// valueMaxBytesDefault caps the buffer length in bytes (a rune may be 4
	// bytes wide, so the byte bound is the one that protects the wire).
	valueMaxBytesDefault = 256 * 1024
)

// Value is the editing buffer: a rune slice plus a rune cursor, and the
// paste-burst state that decides whether a newline means "newline" or
// "submit".
//
// It is deliberately terminal-free: every mutation is a method call, so the
// editing behaviour is asserted directly (and the renderer, which does touch a
// terminal, asserts what was drawn instead).
//
// ONE WRITER: the buffer below is edited by the READ LOOP's own goroutine —
// Editor.Run, and the drains and repaints it performs — or, when there is no
// read loop at all, by the single goroutine that built the buffer (every
// direct-drive unit test). A reader that is NOT that goroutine reads the
// PUBLISHED SNAPSHOT (pub) instead, and the two roads meet in one door:
// Value.Text/Len/Cursor decide per call, with an exact goroutine-identity
// comparison, whether the caller is the owner — never a flag a caller sets, and
// never a call site a reader must remember (valueowner.go carries the invariant,
// the measured defect and the rejected alternatives).
type Value struct {
	// pub is the published snapshot: written by the owner on every mutation,
	// read by every other goroutine. It holds the owner's IDENTITY beside the
	// data — one atomic slot, so a foreign reader's own decision about which
	// road to take cannot disagree with the buffer the owner was editing. It is
	// NOT a guard over the fields below, which stay unguarded on purpose
	// (Rule 15: a lock here would leave two writers interleaving and merely
	// hide it from the detector).
	pub atomic.Value
	// builderGoid is the goroutine that built this buffer (its owner whenever no
	// read loop that owns it — recorded INTO the value by Run's audience note, so
	// the value layer answers "who may read the live fields?" from the same fact
	// every other seam uses rather than from a copy that could drift
	// (valueowner.go).
	loopGoid atomic.Int64

	runes []rune
	pos   int    // cursor position in runes (0..len(runes))
	kill  []rune // yank buffer for the kill commands
	// voice is the PROVENANCE of each rune on the line: true means "this
	// character is still the one a dictation producer inserted, untouched"
	// (valuevoice.go carries the rule, the defect it prevents, and why the bits
	// live here rather than in the producer). It is ALWAYS the same length as
	// runes — every mutation that splices one splices the other — and it is a
	// parallel slice rather than a range list on purpose: a range list would
	// have to be repaired by every mutation anyway, and a stale range is a
	// FALSE PROVENANCE CLAIM rather than a rendering glitch.
	voice   []bool
	maxRune int
	maxByte int
	changed bool // set by any mutation, cleared by Changed()

	// Paste-burst state.
	//
	// THE BURST IS A PROPERTY OF ONE READ, under two conditions: the read
	// carried at least PasteBurstMinChars keys, and it came from the terminal
	// the operator is typing on (Key.Origin == ReadOriginTyped). Anything else
	// — a key at a time, or an INJECTED read — is never a paste, so it can
	// neither arm the suppression nor have its Enter swallowed.
	lastBurstKeys int       // keys the read that armed the burst carried (the warning names it)
	burstEnd      time.Time // arrival of the last key that was part of the burst
	burstActive   bool      // the current burst reached PasteBurstMinChars
	// burstFramed is WHY the burst armed, and it is the whole of this defect's
	// fix: TRUE means the terminal itself bracketed the block (CSI 200 ~ … CSI
	// 201 ~) — it SAID "this is a paste" — so PasteEnterSuppressWindow applies.
	// FALSE means nothing but the read's SIZE said so (one read carrying
	// PasteBurstMinChars keys), which a terminal that coalesced a few typed keys,
	// or that delivered a short non-newline-bearing block, also satisfies for no
	// reason at all — a burst of that kind gets PasteBurstCharInterval.
	burstFramed bool
}

// valueSnapshot is the buffer's CONTENTS as one reader sees them — text and
// cursor from the SAME instant, which is what a foreign reader gets instead of
// sampling a buffer the owner is editing.
type valueSnapshot struct {
	runes []rune
	// voice is the provenance bits that belong to THESE runes — copied in the
	// same publication, so a foreign reader can never pair a count of dictated
	// runes with a buffer those runes are no longer in (valuevoice.go).
	voice []bool
	pos   int
}

// NewValue builds an empty buffer with the given bounds; non-positive bounds
// select the defaults.
func NewValue(maxRunes, maxBytes int) *Value {
	if maxRunes <= 0 {
		maxRunes = valueMaxRunesDefault
	}
	if maxBytes <= 0 {
		maxBytes = valueMaxBytesDefault
	}
	// The published snapshot starts as the empty buffer it really is: a reader
	// served by the copy (everyone, until a read loop takes the buffer up) reads
	// that, never a zero valueSnapshot.
	return &Value{maxRune: maxRunes, maxByte: maxBytes}
}

// Text returns the buffer as a string.
//
// Text is the buffer's content, and it is also THE read that tells a goroutine
// which road it is on: the owner reads the live buffer (the test-only hook below
// fires there), a foreign reader reads the published snapshot. The two roads
// cannot disagree about who is who, because the identity and the data are decided
// together in publish (valueowner.go) — and they do not disagree about the
// CONTENT either: every mutation republishes, so a reader outside the loop sees
// the buffer at the last mutation the loop finished.
func (v *Value) Text() string {
	runBandModelReadHook()
	if v == nil {
		return ""
	}
	_, s := v.read()
	return string(s.runes)
}

// Len returns the buffer length in runes.
func (v *Value) Len() int {
	if v == nil {
		return 0
	}
	_, s := v.read()
	return len(s.runes)
}

// Cursor returns the cursor position in runes.
func (v *Value) Cursor() int {
	if v == nil {
		return 0
	}
	_, s := v.read()
	return s.pos
}

// Changed reports whether a mutation happened since the last call, and clears
// the flag (redraw bookkeeping without a second state variable).
func (v *Value) Changed() bool {
	c := v.changed
	v.changed = false
	return c
}

// tick marks the buffer dirty AND publishes the new contents (plus the cursor,
// which a motion has just moved).
//
// Every mutation funnels through here — the splice, the delete, the kill, the
// cursor motion and the history recall — and that is deliberate: it is the ONE
// place an edit becomes visible to a reader that is not the owner, so "did you
// remember to publish?" has no call site to get wrong (Rule 14).
func (v *Value) tick() {
	v.changed = true
	v.publish()
}

// Reset empties the buffer and clears the paste-burst state (a fresh line
// starts with no history navigation and no pending newline).
func (v *Value) Reset() {
	v.runes = v.runes[:0]
	// A fresh line carries no dictation: the provenance bits are emptied with
	// the runes they describe (valuevoice.go).
	v.voice = v.voice[:0]
	v.pos = 0
	v.resetBurst()
	v.tick()
}

// SetText replaces the buffer (history recall) and puts the cursor at the end.
func (v *Value) SetText(s string) {
	r := []rune(s)
	if len(r) > v.maxRune {
		r = r[:v.maxRune]
	}
	v.runes = append(v.runes[:0], r...)
	// A recalled line is the OPERATOR'S OWN text, not a transcript: every rune
	// is stamped plain, because a history entry that once held dictated text is
	// being recalled as a message the person chose to send again (valuevoice.go).
	v.replaceVoiceRunes()
	v.pos = len(v.runes)
	v.tick()
}

// InsertRune inserts r at the cursor. It reports false when the buffer bound
// refuses the insertion — the caller is expected to signal the refusal (a
// bell), never to pretend the character was accepted (Rule 15: no silent
// failure).
func (v *Value) InsertRune(r rune) bool {
	return v.InsertRunes([]rune{r}) == 0
}

// InsertRunes inserts rs at the cursor in ONE splice, reporting how many runes
// the buffer bound refused (0 when all of them fit).
//
// ONE SPLICE IS THE POINT, and it is a measured one. The per-rune form spent a
// whole slice copy on every rune, so pasting N runes cost O(N²): measured on
// this host, 4 KiB of one InsertRune-at-a-time input took 61ms, 16 KiB took
// 936ms and 64 KiB took 23.0s — with the editor's read loop held inside the
// copy, i.e. the user's input frozen for the whole of it. Splitting into head,
// inserted runes and tail costs one copy for the whole chunk, so the same 64 KiB
// is a single memmove.
//
// The bound is honoured PER RUNE, never by truncating the middle of the input:
// the head is what fits, and the refusal count tells the caller how much was
// not accepted (Rule 15).
func (v *Value) InsertRunes(rs []rune) int {
	if len(rs) == 0 {
		return 0
	}
	// The byte bound is on the encoded buffer (the wire), and rune width varies:
	// it is measured per rune, the same accounting the per-rune form used.
	bytes := len(v.Text())
	roomRunes := v.maxRune - len(v.runes)
	fit := 0
	for fit < len(rs) && fit < roomRunes {
		w := runeWidth(rs[fit])
		if bytes+w > v.maxByte {
			break
		}
		bytes += w
		fit++
	}
	refused := len(rs) - fit
	if fit == 0 {
		return refused
	}
	v.spliceRunes(rs[:fit])
	return refused
}

// InsertString inserts a string at the cursor, reporting how many runes were
// refused by the buffer bound (0 when the whole string fit).
func (v *Value) InsertString(s string) int {
	return v.InsertRunes([]rune(s))
}

// spliceRunes inserts rs at the cursor in one copy of the tail — the single
// buffer mutation every bulk insert goes through. The tail is MOVED to its new
// home (pos+len(rs)) before the inserted runes are written, so the two copies
// never overlap the same cells: writing rs first would overwrite the tail when
// the inserted text is longer than the gap it replaces.
func (v *Value) spliceRunes(rs []rune) {
	v.spliceRunesStamped(rs, voicePlain)
}

// Backspace deletes the rune before the cursor, reporting whether it deleted.
func (v *Value) Backspace() bool {
	if v.pos == 0 {
		return false
	}
	v.deleteVoiceRange(v.pos-1, v.pos)
	v.runes = append(v.runes[:v.pos-1], v.runes[v.pos:]...)
	v.pos--
	v.tick()
	return true
}

// DeleteForward deletes the rune at the cursor.
func (v *Value) DeleteForward() bool {
	if v.pos >= len(v.runes) {
		return false
	}
	v.deleteVoiceRange(v.pos, v.pos+1)
	v.runes = append(v.runes[:v.pos], v.runes[v.pos+1:]...)
	v.tick()
	return true
}

// MoveLeft moves the cursor one rune left.
func (v *Value) MoveLeft() {
	if v.pos > 0 {
		v.pos--
		v.tick()
	}
}

// MoveRight moves the cursor one rune right.
func (v *Value) MoveRight() {
	if v.pos < len(v.runes) {
		v.pos++
		v.tick()
	}
}

// Home moves the cursor to the start of the line.
func (v *Value) Home() {
	v.pos = 0
	v.tick()
}

// End moves the cursor to the end of the line.
func (v *Value) End() {
	v.pos = len(v.runes)
	v.tick()
}

// MoveWordLeft moves to the start of the previous word.
func (v *Value) MoveWordLeft() {
	i := v.pos
	for i > 0 && unicode.IsSpace(v.runes[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(v.runes[i-1]) {
		i--
	}
	v.pos = i
	v.tick()
}

// MoveWordRight moves past the end of the next word.
func (v *Value) MoveWordRight() {
	i := v.pos
	for i < len(v.runes) && unicode.IsSpace(v.runes[i]) {
		i++
	}
	for i < len(v.runes) && !unicode.IsSpace(v.runes[i]) {
		i++
	}
	v.pos = i
	v.tick()
}

// KillToStart deletes from the start of the buffer to the cursor (Ctrl+U) into
// the yank buffer.
func (v *Value) KillToStart() {
	if v.pos == 0 {
		return
	}
	v.kill = append(v.kill[:0], v.runes[:v.pos]...)
	v.deleteVoiceRange(0, v.pos)
	v.runes = append(v.runes[:0], v.runes[v.pos:]...)
	v.pos = 0
	v.tick()
}

// KillToEnd deletes from the cursor to the end of the buffer (Ctrl+K).
func (v *Value) KillToEnd() {
	if v.pos >= len(v.runes) {
		return
	}
	v.kill = append(v.kill[:0], v.runes[v.pos:]...)
	v.deleteVoiceRange(v.pos, len(v.runes))
	v.runes = v.runes[:v.pos]
	v.tick()
}

// KillWordBack deletes the word before the cursor (Ctrl+W).
func (v *Value) KillWordBack() {
	before := v.pos
	v.MoveWordLeft()
	if v.pos == before {
		return
	}
	v.kill = append(v.kill[:0], v.runes[v.pos:before]...)
	v.deleteVoiceRange(v.pos, before)
	v.runes = append(v.runes[:v.pos], v.runes[before:]...)
	v.tick()
}

// Yank reinserts the last killed text at the cursor (Ctrl+Y).
func (v *Value) Yank() {
	if len(v.kill) == 0 {
		return
	}
	v.InsertString(string(v.kill))
}

// resetBurst forgets the paste-burst state.
func (v *Value) resetBurst() {
	v.lastBurstKeys = 0
	v.burstActive = false
	v.burstFramed = false
}

// NoteBurst records that ONE read delivered a paste the TERMINAL FRAMED: the
// byte stream carried the bracketed-paste markers (CSI 200 ~ / CSI 201 ~), and
// the decoder reports them as KeyPasteStart / KeyPasteEnd. That marker is the
// terminal SAYING "this block is a paste", which no timing and no size rule can
// make (see PasteEnterSuppressWindow).
//
// keys is what the caller knows about the framed block's size and may be ZERO:
// when the block was spilled to a file nothing is decoded, and the operator's
// warning then names the paste without a key count rather than inventing one
// (Rule 15 — a diagnostic that lies is worse than none). The window is opened
// from now, the arrival of the read that showed the framing.
//
// AN INJECTED READ IS NEVER A PASTE, framing or not: the injector's bytes did
// not come from the operator's terminal, so they cannot be a paste made on it.
func (v *Value) NoteBurst(keys int, origin ReadOrigin, now time.Time) {
	if origin != ReadOriginTyped {
		return
	}
	if keys < 0 {
		keys = 0
	}
	v.lastBurstKeys = keys
	v.burstActive = true
	v.burstEnd = now
	v.burstFramed = true
}

// NoteKeys records that ONE read delivered n input keys from origin at time now,
// and decides whether that read was a paste.
//
// THE CRITERION IS THE READ, NOT A RUN OF READS. A read carrying at least
// PasteBurstMinChars keys is what a terminal hands us for a real paste (the
// whole block is one write), and arming on it keeps the property this feature
// exists for: a newline inside a pasted block stays content, so "pasted N lines"
// is still ONE message. Accumulating across reads was the defect — it put a fast
// human, an IME committing a word and a key-repeat into the same class as a
// paste, and then silently turned their Enter into a newline (Rule 15).
//
// AN INJECTED READ IS NEVER A PASTE, whatever its size. A script replaying a
// turn, a tool driving the surface, a test: none of them is a person pasting,
// and the caller that queued the keys is the one that wants them applied — so an
// injected read neither arms the suppression nor (see Enter) has its Enter
// swallowed. The origin arrives on every key of the read (stampKeyOrigins), so
// this is a fact the read carries and not a guess from timing.
//
// A read that does not arm the burst may END an armed one (see the tail of this
// function): the paste is a contiguous arrival, so a later keystroke means it is
// over — while the paste's own trailing newline, delivered back to back, stays
// part of it.
func (v *Value) NoteKeys(n int, origin ReadOrigin, now time.Time) {
	if n <= 0 {
		return
	}
	if origin == ReadOriginTyped && n >= PasteBurstMinChars {
		// ONE read, big enough: this is what a terminal hands over for a paste,
		// and the window runs from ITS arrival. But SIZE ALONE IS THE WEAKER
		// EVIDENCE — a terminal coalescing three typed keys produces the same
		// read — so the burst it arms is marked UNFRAMED and judged by
		// PasteBurstCharInterval (see burstWindow).
		v.lastBurstKeys = n
		v.burstActive = true
		v.burstEnd = now
		v.burstFramed = false
		return
	}
	// A read that is not a paste does not ARM the suppression. Whether it ENDS
	// an armed one is a timing question with two honest answers, and the
	// distinction is what keeps the feature from eating real keystrokes:
	//
	//   - a read arriving within PasteBurstCharInterval of the burst's last key
	//     is the paste's own TAIL. A terminal splits a written block at its
	//     buffer boundary and a pasted block's trailing CR often arrives as its
	//     own one-key read, back to back with the block: that newline belongs
	//     to the paste and must stay content.
	//   - a read arriving LATER than that interval is a separate keystroke: the
	//     operator typed something after the paste, so the paste is over. If
	//     the burst were kept, the Enter they pressed up to
	//     PasteEnterSuppressWindow (120 ms!) afterwards would be swallowed.
	if v.burstActive && now.Sub(v.burstEnd) > PasteBurstCharInterval {
		v.resetBurst()
	}
}

// BurstActive reports whether the input currently looks like a paste.
func (v *Value) BurstActive() bool { return v.burstActive }

// BurstKeys reports how many keys the read that armed the current burst carried.
// It is what the operator-facing warning names, so "why did my Enter become a
// newline" can be answered with a number instead of "the editor felt like it".
//
// IT IS ZERO WHEN THE FRAMING CARRIED NO COUNT (a bracketed paste spilled to a
// file decodes no keys), and that is a real answer, not a missing one: the
// warning then names the paste without inventing a key count.
func (v *Value) BurstKeys() int { return v.lastBurstKeys }

// burstWindow is HOW WIDE the Enter suppression is, decided by the EVIDENCE the
// read that armed the burst carried — never by a number tuned per case (Rule
// 10.3: PasteEnterSuppressWindow's 120 ms is untouched; it is SCOPED).
//
//   - a burst the terminal FRAMED (CSI 200 ~ / 201 ~) has the terminal's own
//     statement that the block is a paste, and a paste's bytes are all in
//     flight at once: PasteEnterSuppressWindow, unchanged, is the correct window
//     for it, and it is what keeps "pasted N lines = one message".
//   - a burst armed on SIZE alone carries no such statement. A terminal is free
//     to coalesce a few TYPED keys into one read (a slow ssh hop, a busy pty, a
//     key-repeat), and the operator's next Enter — pressed 50 ms later, long
//     after the keys were typed — then fell inside 120 ms and became a literal
//     newline. So that burst is judged by the gap this package already defines
//     as "the same arrival", PasteBurstCharInterval: an Enter arriving within it
//     is the paste's own tail, and one arriving later is a keypress.
//
// The two windows are the two named constants; no third value is introduced.
// This function is the ONE place the choice is made.
func (v *Value) burstWindow() time.Duration {
	if v.burstFramed {
		return PasteEnterSuppressWindow
	}
	return PasteBurstCharInterval
}

// BurstFramed reports whether the current burst was framed by the terminal
// (bracketed paste) rather than armed by the read's SIZE. The operator-facing
// warning names which evidence produced the suppression (Rule 15): the two are
// different claims about the input, so a reader must be able to tell them apart.
func (v *Value) BurstFramed() bool { return v.burstFramed }

// suppressEnterAt reports whether an Enter arriving at now must be read as
// pasted content rather than as a submit: it must arrive within the burst's own
// window (see burstWindow) of the read that armed it. A burst does not have to
// still be growing — the window starts at its last key — so a paste's trailing
// newline is caught even when the paste arrived in one read.
func (v *Value) suppressEnterAt(origin ReadOrigin, now time.Time) bool {
	return origin == ReadOriginTyped && v.burstActive && now.Sub(v.burstEnd) <= v.burstWindow()
}

// Enter classifies an Enter arriving at now from origin and returns whether it
// was suppressed as pasted content.
//
// A suppressed Enter becomes a newline in the buffer — that IS the fix for "a
// pasted N-line block = N model turns": no Enter that arrives with a paste may
// submit. A suppressed Enter extends the burst's end so a run of newlines inside
// one paste stays content. An INJECTED Enter is never suppressed: it was not
// typed at this terminal, so it cannot be part of a paste made on it.
func (v *Value) Enter(origin ReadOrigin, now time.Time) bool {
	if !v.suppressEnterAt(origin, now) {
		v.resetBurst()
		return false
	}
	if !v.InsertRune('\n') {
		// The buffer bound refused the newline: the paste is over-long, and
		// swallowing the Enter would leave the user unable to submit at all.
		// Submitting the truncated message is the honest outcome.
		v.resetBurst()
		return false
	}
	v.burstEnd = now
	return true
}

// colsUpTo is the display width of the first i runes of the buffer — the
// renderer needs COLUMNS, not runes, because a wide (CJK) rune occupies two
// cells and a combining mark none.
func (v *Value) colsUpTo(i int) int {
	if i > len(v.runes) {
		i = len(v.runes)
	}
	w := 0
	for _, r := range v.runes[:i] {
		w += runeCells(r)
	}
	return w
}

// Lines returns the buffer's line count (a multiline message renders and is
// reported as its own lines). It is a READER on the same terms as Text: the owner
// reads the live buffer, anyone else the published snapshot.
func (v *Value) Lines() int { return strings.Count(v.Text(), "\n") + 1 }

// RowsFor is how many display rows prompt+text occupy at width — the SAME
// arithmetic the renderer draws with, asked BEFORE the layout reserves rows for
// the editing line. It lives on Value so no caller re-derives "how tall is this
// line": a second derivation is a second answer, and the two drift (Rule 14).
//
// It is a READER on the same terms as Text: the owner reads the live buffer and
// anyone else the published snapshot, so a caller that asks "how tall is this
// line?" from another goroutine gets a consistent answer instead of a torn one.
func (v *Value) RowsFor(prompt string, width int) int {
	rows, _, _ := layoutText(prompt, v.Text(), v.Cursor(), width)
	return len(rows)
}

// RowsForDraft is RowsFor with a DRAFT SEGMENT appended (draft.go): how many rows
// the editing line REALLY occupies once the streaming producer's text is laid out
// after the buffer.
//
// The distinction matters because this number is the region's row WISH: asking
// for the buffer alone while the layout draws buffer+draft would leave the plan
// and the screen describing different lines, which is exactly the disagreement
// the caller's own comment warns about.
func (v *Value) RowsForDraft(prompt, draft string, width int) int {
	if draft == "" {
		return v.RowsFor(prompt, width)
	}
	rows, _, _ := layoutTextDraft(prompt, v.Text(), draft, v.Cursor(), width)
	return len(rows)
}

// ReplaceRange replaces the rune range [from, to) with text and leaves the
// cursor at the end of the replacement. It is the completion menu's buffer
// edit: one call, so the menu never manipulates the cursor and the text
// separately (a cursor set before the text would move under the insert).
func (v *Value) ReplaceRange(from, to int, text string) {
	if from < 0 {
		from = 0
	}
	if to > len(v.runes) {
		to = len(v.runes)
	}
	if to < from {
		to = from
	}
	ins := []rune(text)
	next := make([]rune, 0, len(v.runes)-(to-from)+len(ins))
	next = append(next, v.runes[:from]...)
	next = append(next, ins...)
	next = append(next, v.runes[to:]...)
	if len(next) > v.maxRune {
		next = next[:v.maxRune]
	}
	v.runes = next
	// THE COMPLETION MENU'S EDIT IS THE OPERATOR'S, never the recogniser's: the
	// replacement text is a candidate the person chose, so every rune in the new
	// span is stamped plain (valuevoice.go). Rebuilding the bits with the whole
	// buffer keeps the two slices the same length whichever side grew.
	v.replaceVoiceRunes()
	v.pos = from + len(ins)
	if v.pos > len(v.runes) {
		v.pos = len(v.runes)
	}
	v.tick()
}
