package lineedit

import "unicode/utf8"

// valuevoice.go — WHICH RUNES ON THE EDITING LINE CAME FROM DICTATION.
//
// THE INFORMATION THIS KEEPS, and why it has to be kept HERE. A `/voice`
// transcript is appended to the editing line through the same insert road a
// completer uses (lineinsert.go), so from that moment the text is the
// operator's: editable, backspace-able, and — until this file existed —
// INDISTINGUISHABLE from what they typed. A carrier that wants to tell the
// model "this line was dictated" therefore has to answer one question at the
// moment of submit: DOES THE LINE STILL CONTAIN VOICE-ORIGINATED CHARACTERS?
//
// THAT QUESTION IS NOT "WAS VOICE MODE OPEN", and the difference is the whole
// design. An operator who dictates a sentence, deletes it and types their own
// must carry no marker: the line that gets submitted is pure typing, and a
// provenance claim about it would be a lie the model cannot check. A boolean
// latch on the mode ("voice was on this turn") cannot express that; a per-rune
// bookkeeping can, and it costs one bit per rune.
//
// WHY THE BITS LIVE BESIDE THE RUNES IN Value AND NOT IN THE PRODUCER. The
// producer cannot track the offsets: every buffer mutation moves them
// (Backspace, DeleteForward, KillToStart/KillToEnd/KillWordBack, a completion's
// ReplaceRange, a history recall's SetText), and those mutations happen on the
// read loop, whose buffer only the loop may write (valueowner.go). A producer
// that maintained its own ranges would be a second writer of the line by
// another name — the exact defect draft.go was written to avoid.
//
// THE DISCIPLINE IS THE BUFFER'S OWN. Every mutation that changes a rune's
// identity clears that rune's bit to `voicePlain`; only the ONE insert road
// that a dictation producer takes stamps `voiceOriginated`. So the invariant a
// reader can rely on is: a `true` bit means "this rune is still the character
// the recogniser produced, untouched".

// voicePlain is the provenance of a rune that is NOT the recogniser's: typed,
// pasted, recalled from history, or walked in by the completion menu. It is the
// zero value on purpose — an unset bit cannot be mistaken for a dictation.
const voicePlain = false

// voiceOriginated is the provenance of a rune a dictation producer inserted and
// nobody has touched since.
const voiceOriginated = true

// VoiceRunes reports how many runes in the buffer still carry dictation
// provenance.
//
// IT IS A COUNT, NOT A BOOLEAN, because the caller's question has two honest
// answers from one measurement: "is there any" (a count > 0) and "how much" (a
// prefix that quotes the size). A bool would make the second impossible without
// a second field that could disagree with the first.
func (v *Value) VoiceRunes() int {
	if v == nil {
		return 0
	}
	// THE READ ROAD IS THE SAME ONE Text/Len/Cursor TAKE (valueowner.go): the
	// owner reads the live bits, every other goroutine reads the bits published
	// beside the runes it is reading. Asking the question on the wrong road
	// would pair a count with a buffer it does not belong to — a provenance
	// claim about text that was never submitted.
	_, snap := v.read()
	n := 0
	for _, spoke := range snap.voice {
		if spoke {
			n++
		}
	}
	return n
}

// runeWidth is the encoded width of one rune in bytes: the same measurement
// InsertRunes makes inline, extracted so the two insert roads (plain and
// dictated) cannot disagree about how much room a text needs. An invalid rune
// encodes as U+FFFD, which is three bytes.
func runeWidth(r rune) int {
	w := utf8.RuneLen(r)
	if w < 0 {
		w = len(string(r))
	}
	return w
}

// insertRunesStamped inserts rs at the cursor and stamps each inserted rune with
// origin. It is InsertRunes' body with ONE extra piece of bookkeeping, and it
// exists as a separate unexported entry so InsertRunes keeps its contract (every
// caller before this file inserted text that was not dictation) without a mode
// flag that a reader would have to reason about.
func (v *Value) insertRunesStamped(rs []rune, origin bool) int {
	if len(rs) == 0 {
		return 0
	}
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
	v.spliceRunesStamped(rs[:fit], origin)
	return refused
}

// spliceRunesStamped is spliceRunes with the provenance bits carried alongside.
// The two edits are ONE atomic pair: a rune spliced without its bit (or the bit
// without its rune) would make VoiceRunes name characters that are not on the
// line, which is the false provenance claim this file exists to make
// impossible.
func (v *Value) spliceRunesStamped(rs []rune, origin bool) {
	n := len(v.runes)
	v.runes = append(v.runes, make([]rune, len(rs))...)
	copy(v.runes[v.pos+len(rs):], v.runes[v.pos:n])
	copy(v.runes[v.pos:v.pos+len(rs)], rs)
	// The bits are moved the same way and in the same order as the runes: the
	// tail first (so the two copies never overlap), then the inserted span.
	v.voice = append(v.voice, make([]bool, len(rs))...)
	copy(v.voice[v.pos+len(rs):], v.voice[v.pos:n])
	for i := range rs {
		v.voice[v.pos+i] = origin
	}
	v.pos += len(rs)
	v.tick()
}

// deleteVoiceRange removes the provenance bits for the rune range [from, to)
// that a mutation is about to delete. It is called by every deletion BEFORE the
// runes are closed over, because after the splice the range means something
// else.
func (v *Value) deleteVoiceRange(from, to int) {
	if from < 0 {
		from = 0
	}
	if to > len(v.voice) {
		to = len(v.voice)
	}
	if from >= to {
		return
	}
	v.voice = append(v.voice[:from], v.voice[to:]...)
}

// replaceVoiceRunes rebuilds the provenance bits for the WHOLE buffer from a
// replacement text, clearing every bit. It is the bookkeeping for the mutations
// whose new text is not a splice of the old (SetText, Reset): the operator's own
// source for those is a recall or an empty line, and neither contains dictation
// characters, so every rune is stamped plain.
func (v *Value) replaceVoiceRunes() {
	if cap(v.voice) >= len(v.runes) {
		v.voice = v.voice[:len(v.runes)]
		for i := range v.voice {
			v.voice[i] = voicePlain
		}
		return
	}
	v.voice = make([]bool, len(v.runes))
}

// copyVoice copies the provenance bits for a snapshot. It is the read side's
// counterpart to publish(): a foreign reader must see the bits that belong to
// the runes it is reading, never a later mutation's.
func (v *Value) copyVoice() []bool {
	return append([]bool(nil), v.voice...)
}
