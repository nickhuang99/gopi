package lineedit

import (
	"strings"
	"sync"
)

// draft.go — the editing line's DRAFT SEGMENT: text that is NOT (yet) the
// operator's own, drawn in the line, and REPLACED as the producer refines it.
//
// WHY THE INPUT LINE NEEDS A SECOND KIND OF TEXT. `InsertIntoLine` (lineinsert.go)
// exists for a producer whose text is FINISHED: a recogniser's final transcript,
// which the operator may backspace like anything else they typed. A STREAMING
// recogniser has the opposite shape — it emits the same sentence again and again
// as it hears more of it ("温度", "温度是", "温度是多少"), and every emission
// supersedes the last. Appending those would print the sentence three times, and
// editing the buffer rune by rune to retract the previous one would make the
// producer a second writer of the user's own text — it could not tell which
// runes were its own, and a keystroke landing in the middle of the retraction
// would corrupt both.
//
// SO THE DRAFT IS A SEGMENT THE MODEL KNOWS ABOUT, and the ONE writer (the read
// loop) is still the only thing that touches it:
//
//   - the draft's TEXT lives outside the buffer, in the editor's own queue (the
//     same level-triggered road lineinsert.go established);
//   - the LAYOUT lays the buffer and the draft out as one line — the draft
//     immediately follows the buffer, exactly where `InsertIntoLine` would have
//     put it;
//   - the DRAFT'S RUNES ARE NOT IN Value, so no buffer edit can touch them, no
//     kill/backspace can leave a fragment behind, and the RETRACTION is a
//     replace of one string rather than a delete of N runes;
//   - it is drawn under the palette's Draft style, so a reader sees at a glance
//     which part of the line is not yet theirs.
//
// SETTLING. When the producer's answer becomes final it calls SetDraft(text, …)
// with done=true, and the read loop moves the draft into the buffer through the
// ordinary insert road and clears the segment. From that moment the text IS the
// operator's: editable, backspace-able, and indistinguishable from anything they
// typed.
//
// THE CURSOR IS EXCLUDED, NOT THE DRAFT. The caret can never be placed inside the
// draft (the draft is not in Value, so there is no position for it to be at), and
// that is the honest arrangement rather than a limitation: text the operator
// cannot edit should not accept their caret. The caret keeps its place in the
// buffer, so an operator editing their own draft while the recogniser talks is
// neither interrupted nor overwritten.

// lineDraft holds the pending draft text and the level that says one is waiting.
// The mutex guards the DATA only — the read loop is still the only goroutine that
// touches the editing model.
type lineDraft struct {
	mu sync.Mutex
	// text is the draft as the producer last stated it, or "" when there is no
	// draft. It is the WHOLE draft every time, not a delta: the producer's own
	// answer is a snapshot, so replacing it is the contract (accumulating would
	// reproduce the repetition this segment exists to avoid).
	text string
	// done reports that the producer has declared this text FINAL, so the read
	// loop will move it into the buffer rather than keep drawing it as a draft.
	// It travels WITH the text in one call so a settle can never be applied to a
	// later draft (the two fields are one producer's decision).
	done bool
	// active is the LEVEL (lineinsert.go's discipline): a draft is waiting, or
	// the loop is inside the drain. Sticky, because the loop can be parked in an
	// unbounded read when the producer writes.
	active bool
}

// SetDraft states the producer's CURRENT draft text: it REPLACES whatever the
// previous call stated, and it is drawn in the editing line immediately after the
// operator's own text.
//
// An empty text CLEARS the draft (the producer withdrew it, or the utterance was
// abandoned), which is a distinct state from "no call was ever made" only in that
// it is explicit — both draw nothing.
//
// done=true additionally SETTLES it: the read loop moves the text into the buffer
// through the ordinary insert road and clears the segment, so the text becomes
// the operator's own from that moment on.
//
// It is safe to call from any goroutine — the producer's own is the intended one —
// and it reports whether the editor was able to carry the draft at all: a nil
// editor, or one that is already closed, takes nothing, and a producer that does
// not hear that would keep a draft the operator never sees (Rule 15).
func (e *Editor) SetDraft(text string, done bool) bool {
	if e == nil {
		return false
	}
	e.drafts.mu.Lock()
	e.drafts.text = text
	e.drafts.done = e.drafts.done || done
	e.drafts.active = true
	e.drafts.mu.Unlock()
	// The wake is the same one lineinsert.go uses, and for the same reason: the
	// level carries the data past a parked read, and the wake only shortens the
	// wait for the reader that is already awake.
	if e.notify != nil && e.notify.NotifyEnabled() {
		e.notify.Notify()
	}
	return true
}

// Draft returns the draft text as published: what the line is drawing right now.
// A foreign caller (a test, the producer asking "is my text still the current
// one?") reads it here rather than through the buffer, because the draft is
// deliberately not IN the buffer.
func (e *Editor) Draft() string {
	if e == nil {
		return ""
	}
	e.drafts.mu.Lock()
	defer e.drafts.mu.Unlock()
	return e.drafts.text
}

// DraftVisible reports whether a draft segment is in the line — the predicate
// behind "the line still carries text that is not the operator's own".
func (e *Editor) DraftVisible() bool {
	return e.Draft() != ""
}

// draftPending reports whether a draft write is waiting to be applied.
func (e *Editor) draftPending() bool {
	e.drafts.mu.Lock()
	defer e.drafts.mu.Unlock()
	return e.drafts.active
}

// drainDraft applies the producer's LAST stated draft, on the caller's goroutine
// — which is the read loop. It reports whether it changed anything.
//
// IT SETTLES FIRST, if the producer said done: the text goes into the buffer
// through the ordinary insert road (so it is the operator's own from then on) and
// the segment is cleared in the SAME pass, which is what makes the hand-over
// atomic from the line's point of view — the operator never sees the text twice
// (once as a draft and once as a buffer) nor loses it in between.
//
// IT IS LEVEL-TRIGGERED AND LAST-WINS, which is the whole point: a producer that
// states twenty drafts between two repaints costs the read loop ONE replace, not
// twenty — the intermediate texts were never shown and never will be, and that is
// the correct behaviour for a snapshot producer.
func (e *Editor) drainDraft() bool {
	e.drafts.mu.Lock()
	if !e.drafts.active {
		e.drafts.mu.Unlock()
		return false
	}
	text, done := e.drafts.text, e.drafts.done
	// The level is LOWERED here, under the same lock that read the data, so a
	// producer that writes after this line raises it again — the level can never
	// be lowered over a draft that is still waiting.
	e.drafts.active = false
	e.drafts.done = false
	e.drafts.mu.Unlock()

	if done {
		// SETTLE: the text becomes the operator's own. The segment is cleared
		// whatever the insert road answers, because a draft the producer has
		// declared final must not keep being drawn as a draft — a stale draft
		// beside the settled text is exactly the duplication this file exists to
		// prevent. A refusal by the buffer bound is reported by the insert road
		// itself (applyLineInsertText warns), so nothing here is silent.
		e.drafts.mu.Lock()
		e.drafts.text = ""
		e.drafts.mu.Unlock()
		if text != "" {
			// A SETTLED DRAFT IS DICTATION: the producer that declared it final is
			// a recogniser, so the runes it hands over keep that provenance and a
			// submitted line holding them can say where they came from
			// (valuevoice.go). Nothing else settles a draft — the draft segment
			// exists for the streaming recogniser alone (draft.go's header).
			e.applyLineInsertText(lineInsert{text: text, place: LineInsertAtEnd, dictated: true})
		}
		return true
	}
	if text == "" {
		// The producer withdrew the draft: nothing to draw, and the NEXT repaint
		// (which is what called this) erases the cells it occupied because the
		// row it lays out no longer carries them.
		e.drafts.mu.Lock()
		e.drafts.text = ""
		e.drafts.mu.Unlock()
		return true
	}
	return true
}

// draftTextForDrawing returns the draft as the LAYOUT must see it on one drawn
// row, or "" when this row carries none.
//
// The draft follows the buffer on the line, so only the row that carries the
// buffer's END can carry the beginning of the draft, and a draft longer than the
// rest of that row continues on the rows below. firstRow is the row the buffer's
// text ends on; rows after it carry the draft's continuation.
func draftTextForDrawing(draft string, row, firstRow int) string {
	if draft == "" || row < firstRow {
		return ""
	}
	return draft
}

// draftDisplayText is the draft's text with its own control characters
// neutralised the way the buffer's are: a newline inside a draft would break the
// row arithmetic that decides where the caret is, and the producer's text is
// inserted verbatim on settle — so the DRAWN form replaces the breaks and the
// settled form does not (the buffer itself has no such restriction: the operator
// may type a newline, and the insert road handles it).
func draftDisplayText(draft string) string {
	if !strings.ContainsAny(draft, "\r\n") {
		return draft
	}
	r := strings.NewReplacer("\r\n", draftBreakPlaceholder, "\n", draftBreakPlaceholder, "\r", draftBreakPlaceholder)
	return r.Replace(draft)
}

// draftBreakPlaceholder is what a line break inside a draft is DRAWN as. It is a
// single cell, so the layout arithmetic stays exact, and it is a visible marker
// rather than a space so the operator can see that the recogniser emitted a break
// their own text will carry once settled.
const draftBreakPlaceholder = "⏎"
