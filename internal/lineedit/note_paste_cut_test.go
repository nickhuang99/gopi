package lineedit

import "testing"

// TestNoteRowsForPasteKeepThePanelsHardCut is the regression guard for two layout
// changes this epic made and then had to UNDO (measured 2026-10-22 on a real run,
// Rule 31 — both were invisible to the unit tests written first):
//
//  1. `/info`'s block used to reach the band through the PANEL path
//     (region.writeBandLine → cutCols), i.e. a HARD column cut. Routed through the
//     note path it broke at a word/separator instead and noteTail CONSUMED the
//     break character, so `session dir  …/.gopi/sessions/<uuid>` no longer
//     reassembled into chatInfoBlock's bytes — the paste stopped being the block.
//  2. A `/voice` row took the same path and gained a `↳ ` continuation marker it
//     never had on master, i.e. an emphasis-only change had moved the layout.
//
// BOTH are properties of a PASTE SOURCE row, so both are asserted here on the
// renderer's own cutters: a copied row takes the column cut and no marker, while
// an ordinary note row keeps the marker (a wrapped REMARK is one remark).
func TestNoteRowsForPasteKeepThePanelsHardCut(t *testing.T) {
	const cols = 20
	// A token longer than the band with separators inside is where the two cutters
	// disagree: cutCols splits at the column, cutAtBoundary backs up to a boundary
	// and the break consumes it.
	const long = "session dir  /a/b/c/d/e/f/g/uuid"

	r := &AnswerRenderer{cols: cols}
	cont, head, tail := r.cutNoteRowCopied(long, true, true)
	if cont {
		t.Errorf("a copied row claimed a continuation marker (cont=true) — the glyph would land in the pasted cells")
	}
	if head != long[:cols] {
		t.Errorf("a copied row was not cut at the band's own column:\n head=%q\n want=%q", head, long[:cols])
	}
	if head+tail != long {
		t.Errorf("a copied row LOST characters across the cut — head+tail=%q, row=%q", head+tail, long)
	}

	// The ordinary note row is the opposite, deliberately: it reserves the marker
	// and breaks at a boundary.
	cont2, head2, _ := r.cutNoteRowCopied(long, false, false)
	if !cont2 {
		t.Errorf("an ordinary note row's continuation lost its marker")
	}
	if len(head2) > cols-displayWidth(NoteContinuation) {
		t.Errorf("the ordinary row's head %q is wider than the cells its marker left (%d)", head2, cols-displayWidth(NoteContinuation))
	}
}
