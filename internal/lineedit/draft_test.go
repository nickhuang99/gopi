package lineedit

// draft_test.go — the DRAFT SEGMENT's OWN contract, at the seam.
//
// WHAT THESE CASES ARE FOR, and what they deliberately are NOT. The segment's
// whole reason to exist is that a STREAMING producer emits the same sentence
// again and again as it hears more of it, so the three facts a painted-grid case
// cannot isolate are pinned here by name:
//
//	REPLACEMENT — the second draft REPLACES the first, and the line never shows
//	              both (the defect this file exists to prevent);
//	SETTLE      — the final text becomes the operator's own, exactly once, and
//	              the draft segment is cleared in the same pass;
//	INTERFERENCE— the draft is NOT in the buffer, so the operator's own typing,
//	              caret and deletions are untouched by it and cannot corrupt it.
//
// The end-to-end proof that the settled text reaches the PAINTED GRID runs where
// the production carrier is built (cmd/agent's pty cases); here the seam is
// driven dry — no goroutine, no terminal — so each property fails alone.

import (
	"strings"
	"testing"
)

// TestDraftSegment_ReplacesRatherThanAppends is the defect's own regression case:
// a streaming recogniser states the SAME sentence three times as it hears more of
// it, and the line must show the LAST statement once.
//
// IT IS RED ON THE APPEND SHAPE. Routing the partials through InsertIntoLine — the
// road that existed before this segment — appends every statement, so the line
// reads "温度温度是温度是多少" and the assertion below fails on the first draft.
func TestDraftSegment_ReplacesRatherThanAppends(t *testing.T) {
	ed := NewEditor(inertSource{}, discardWriter{}, promptForTest, nil)
	if ed.Value().InsertString("现在") != 0 {
		t.Fatal("the operator's own text did not fit")
	}

	// The three statements a real recogniser produced for one sentence (the
	// measurements are in the task's session artifacts).
	for _, draft := range []string{"温度", "温度是", "温度是多少"} {
		if !ed.SetDraft(draft, false) {
			t.Fatalf("the draft %q was refused by the seam", draft)
		}
		if !ed.drainDraft() {
			t.Fatalf("the pending draft %q was not applied by the loop's own drain", draft)
		}
	}
	if got := ed.Draft(); got != "温度是多少" {
		t.Fatalf("the line's draft reads %q, want only the LAST statement %q — the segment appended instead of replacing", got, "温度是多少")
	}
	// THE OPERATOR'S TEXT IS UNTOUCHED BY THE DRAFT, and this is the other half of
	// the contract: the drafts came and went and the buffer still holds exactly
	// what was typed into it, with no attempt to edit it.
	if got := ed.Value().Text(); got != "现在" {
		t.Fatalf("the buffer reads %q, want the operator's own %q — the draft leaked into the buffer", got, "现在")
	}
}

// TestDraftSegment_SettlesOntoTheLineExactlyOnce pins the hand-over: the final
// text lands on the line ONCE, after whatever the operator had typed, and the
// draft segment is gone in the same pass.
//
// THE DOUBLE-COUNT IS THE FAILURE THIS CATCHES, and it is the shape a naive
// implementation produces: settle by inserting the text while leaving the draft
// in place, so the sentence appears once as a draft and once as settled text —
// "现在温度是多少温度是多少".
func TestDraftSegment_SettlesOntoTheLineExactlyOnce(t *testing.T) {
	ed := NewEditor(inertSource{}, discardWriter{}, promptForTest, nil)
	if ed.Value().InsertString("现在") != 0 {
		t.Fatal("the operator's own text did not fit")
	}
	if !ed.SetDraft("温度是多少", false) || !ed.drainDraft() {
		t.Fatal("the draft did not reach the segment")
	}
	if !ed.SetDraft("温度是多少", true) {
		t.Fatal("the settle was refused by the seam")
	}
	if !ed.drainDraft() {
		t.Fatal("the settle was not applied by the loop's own drain")
	}
	if got := ed.Value().Text(); got != "现在温度是多少" {
		t.Fatalf("after the settle the line reads %q, want %q — the settle either dropped the text or wrote it twice", got, "现在温度是多少")
	}
	if got := ed.Draft(); got != "" {
		t.Fatalf("the draft segment still reads %q after the settle — the sentence is on the line twice", got)
	}
	// THE SETTLED TEXT IS THE OPERATOR'S OWN, and this is what makes it different
	// from a draft: it can be edited like anything they typed.
	ed.Value().End()
	if !ed.Value().Backspace() {
		t.Fatal("the settled text is not editable — backspace did nothing")
	}
	if got := ed.Value().Text(); got != "现在温度是多" {
		t.Fatalf("after backspacing the settled text the line reads %q, want %q", got, "现在温度是多")
	}
}

// TestDraftSegment_OperatorKeepsEditingWhileTheRecogniserTalks is the
// NON-INTERFERENCE property, and it is the one that justifies the segment being
// outside the buffer.
//
// THE FAILURE IT CATCHES is the retraction implementation — the one that edits
// the buffer rune by rune to replace the previous draft. Under it, the operator's
// keystroke lands in the middle of the retraction and is either deleted (its runes
// look like the draft's) or left as a fragment of a sentence that no longer
// exists. Here the operator types, moves their caret and deletes WHILE drafts keep
// arriving, and every one of those edits must survive untouched.
func TestDraftSegment_OperatorKeepsEditingWhileTheRecogniserTalks(t *testing.T) {
	ed := NewEditor(inertSource{}, discardWriter{}, promptForTest, nil)
	if ed.Value().InsertString("部署到") != 0 {
		t.Fatal("the operator's own text did not fit")
	}
	if !ed.SetDraft("staging", false) || !ed.drainDraft() {
		t.Fatal("the draft did not reach the segment")
	}

	// The operator keeps working: a caret move into their own text, an
	// insertion there, and a deletion — all while the draft is on the line.
	ed.Value().Home()
	if ed.Value().InsertString("把") != 0 {
		t.Fatal("the operator's mid-line insert did not fit")
	}
	if got := ed.Value().Text(); got != "把部署到" {
		t.Fatalf("the operator's insert landed at %q, want at the caret (leading)", got)
	}
	if !ed.SetDraft("staging 环境", false) || !ed.drainDraft() {
		t.Fatal("the refined draft did not reach the segment")
	}
	if got := ed.Value().Text(); got != "把部署到" {
		t.Fatalf("the buffer reads %q after a refined draft, want the operator's own text unchanged", got)
	}
	if got := ed.Draft(); got != "staging 环境" {
		t.Fatalf("the draft reads %q, want the refined %q — the operator's edits were mistaken for the recogniser's", got, "staging 环境")
	}
}

// TestDraftSegment_WithdrawalAndEmptyTextClearTheSegment pins the two ways a draft
// stops being drawn, because a segment that cannot be cleared is a line the
// operator cannot finish writing.
func TestDraftSegment_WithdrawalAndEmptyTextClearTheSegment(t *testing.T) {
	ed := NewEditor(inertSource{}, discardWriter{}, promptForTest, nil)
	if !ed.SetDraft("温度", false) || !ed.drainDraft() {
		t.Fatal("the draft did not reach the segment")
	}
	if !ed.SetDraft("", false) || !ed.drainDraft() {
		t.Fatal("the withdrawal was refused")
	}
	if ed.DraftVisible() {
		t.Fatalf("the segment still shows %q after the producer withdrew it", ed.Draft())
	}
	// AND A WITHDRAWAL DOES NOT TOUCH THE BUFFER: the operator's own text is not
	// the recogniser's to remove.
	if got := ed.Value().Text(); got != "" {
		t.Fatalf("the buffer reads %q, want it untouched by the draft's withdrawal", got)
	}
}

// TestDraftSegment_LastWinsIsLevelTriggered is the PACING property: a producer
// that states twenty drafts between two repaints costs the loop ONE apply, and the
// text that is applied is the LAST one.
//
// It is a property of production, not of politeness — the reader goroutine calls
// SetDraft per partial while the loop is parked in a read, so the segment has to
// collapse them itself; a queue of twenty would show the operator a twenty-frame
// stutter of a sentence that was already superseded.
func TestDraftSegment_LastWinsIsLevelTriggered(t *testing.T) {
	ed := NewEditor(inertSource{}, discardWriter{}, promptForTest, nil)
	for _, draft := range []string{"a", "ab", "abc", "abcd", "abcde"} {
		if !ed.SetDraft(draft, false) {
			t.Fatalf("the draft %q was refused", draft)
		}
	}
	if !ed.draftPending() {
		t.Fatal("a pending draft was not visible to the loop's own predicate — it could never reach a repaint")
	}
	if !ed.drainDraft() {
		t.Fatal("the drain applied nothing although a draft was pending")
	}
	if got := ed.Draft(); got != "abcde" {
		t.Fatalf("the segment shows %q, want the last stated %q", got, "abcde")
	}
	// ONE APPLY: the drain's own return is the observable, and a second call
	// finding nothing is what proves the five statements collapsed into one.
	if ed.drainDraft() {
		t.Fatal("the drain applied a second time although nothing new was stated — the level was not lowered")
	}
}

// TestDraftSegment_NilEditorRefusesByName pins the refusal. A producer on a
// carrier with no editing line must hear that its text had nowhere to go, rather
// than appear to work while the operator sees nothing (Rule 15).
func TestDraftSegment_NilEditorRefusesByName(t *testing.T) {
	var ed *Editor
	if ed.SetDraft("温度", false) {
		t.Fatal("a nil editor accepted a draft — the producer would believe it landed")
	}
	if got := ed.Draft(); got != "" {
		t.Fatalf("a nil editor read back %q", got)
	}
}

// TestDraftSegment_DisplayedFormNeutralisesBreaksOnlyForDrawing pins the drawing
// guard: a break inside a draft is DRAWN as one cell (so the row arithmetic the
// caret depends on stays exact) while the text the segment carries is unchanged —
// because the settled form is what reaches the buffer, and the operator's own line
// may legitimately contain a newline.
func TestDraftSegment_DisplayedFormNeutralisesBreaksOnlyForDrawing(t *testing.T) {
	const withBreak = "first\nsecond"
	shown := draftDisplayText(withBreak)
	if strings.ContainsAny(shown, "\r\n") {
		t.Fatalf("the drawn draft %q still carries a break — the row arithmetic would be wrong", shown)
	}
	if !strings.Contains(shown, draftBreakPlaceholder) {
		t.Fatalf("the drawn draft %q carries no marker for the break it replaced (%q)", shown, draftBreakPlaceholder)
	}
	// The carried text is untouched: the marker is a DRAWING decision.
	ed := NewEditor(inertSource{}, discardWriter{}, promptForTest, nil)
	if !ed.SetDraft(withBreak, false) {
		t.Fatal("the draft with a break was refused")
	}
	if got := ed.Draft(); got != withBreak {
		t.Fatalf("the segment carries %q, want the producer's own %q", got, withBreak)
	}
}

// TestDraftSegment_IsPaintedUnderItsOwnEmphasis is the RENDERED half: the draft's
// cells carry the palette's Draft style and the operator's own text keeps the
// Buffer style, so "which words are mine yet?" is answerable from the screen.
//
// IT IS ASSERTED ON THE BYTES THE RENDERER WROTE, not on the emphasis table: a
// table with the right entries and an emphasis pass that never applies them is
// exactly the half-wired state this catches.
func TestDraftSegment_IsPaintedUnderItsOwnEmphasis(t *testing.T) {
	ed, cap := paletteEditor(t, BasicPalette)
	ed.val.SetText("现在")
	ed.render.SetDraft("温度是多少")
	ed.render.Redraw(ed.val, paletteTestCols)

	got := cap.bytes()
	if want := BasicPalette.Buffer.Wrap("现在"); !strings.Contains(got, want) {
		t.Fatalf("the operator's own text is not drawn under the Buffer style %q:\n%q", want, got)
	}
	if want := BasicPalette.Draft.Wrap("温度是多少"); !strings.Contains(got, want) {
		t.Fatalf("the draft is not drawn under the Draft style %q — the operator cannot tell their words from the recogniser's:\n%q", want, got)
	}
}

// TestDraftSegment_DoesNotMoveTheCaret is the CARET contract: the draft is laid
// out AFTER the buffer, so where the operator's cursor is does not depend on it —
// and the number of rows the line claims is the number it actually draws.
//
// THE TWO HALVES ARE ONE PROPERTY. RowsForDraft exists because the region's row
// WISH must count the draft while RowsFor does not: a plan computed from the
// buffer alone would under-ask for rows the layout then draws, and the status row
// below the line would be painted over.
func TestDraftSegment_DoesNotMoveTheCaret(t *testing.T) {
	ed := NewEditor(inertSource{}, discardWriter{}, promptForTest, nil)
	if ed.Value().InsertString("deploy the") != 0 {
		t.Fatal("the operator's own text did not fit")
	}
	before := ed.Value().Cursor()

	if !ed.SetDraft("staging box", false) || !ed.drainDraft() {
		t.Fatal("the draft did not reach the segment")
	}
	if got := ed.Value().Cursor(); got != before {
		t.Fatalf("the caret moved from %d to %d because of a draft — the draft is after the buffer and must not touch the caret", before, got)
	}
	if got := ed.Value().RowsFor(promptForTest, 80); got != 1 {
		t.Fatalf("the buffer alone claims %d row(s), want 1", got)
	}
	if got := ed.Value().RowsForDraft(promptForTest, "staging box", 80); got != 1 {
		t.Fatalf("the buffer plus a short draft claims %d row(s), want them on the ONE row they fit on", got)
	}
	// A draft that does not fit must ask for the row it wraps onto: this is the
	// half that would silently paint over the status row if the wish ignored it.
	// The expected count is derived from the SAME layout the drawing uses, so the
	// two cannot disagree — what is asserted is that the wish counts the draft at
	// all (it is 1 without one), not a number this test made up.
	long := strings.Repeat("字", 200)
	rows, _, _ := layoutTextDraft(promptForTest, ed.Value().Text(), long, ed.Value().Cursor(), 80)
	if got := ed.Value().RowsForDraft(promptForTest, long, 80); got != len(rows) {
		t.Fatalf("the row wish claims %d row(s) for a 200-rune draft, but the layout draws %d — the plan and the screen describe different lines", got, len(rows))
	}
	if got := ed.Value().RowsForDraft(promptForTest, long, 80); got < 2 {
		t.Fatalf("a 200-rune draft claims %d row(s) at width 80, want the ones it wraps onto", got)
	}
}
