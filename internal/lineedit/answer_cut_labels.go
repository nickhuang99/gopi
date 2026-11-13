package lineedit

// answer_cut_labels.go — THE RENDERER'S OWN RECORD OF THE CELLS IT DREW THAT
// THE ANSWER DID NOT CONTAIN.
//
// THE MEASURED DEFECT THIS EXISTS FOR (task 01a1603f, the chat ellipsis epic).
// An answer body reaches the operator's screen carrying a cell in places the
// model's own wire text does not have one: the band's right-edge cut draws the
// band's cut label (`cutLabelFor` → `writeCutLabel` → `answerCutLabel`, `↵`; it
// was the status row's `…` when this record was written, see
// answer_cut_label_glyph.go for why that spelling moved) in the row's LAST cell,
// the row's last character is displaced onto the row below, and the source is
// untouched (`liveText` deliberately never sees the label). A reader comparing
// the screen against `raw.log` therefore sees two copies of "the same" body that
// disagree, and NOTHING on the screen says which layer put the character there.
// The console reported it as a model violation ("the prompt forbids it") because
// the only thing it could see was the drawn cell.
//
// SO THE FACT IS RECORDED WHERE IT IS PRODUCED — inside writeCutLabel, the ONE
// place this glyph is drawn — rather than re-derived later from the source. A
// second derivation (asking cutLabelFor again over the body) is exactly the
// second definition Rule 14 forbids: it can disagree with the paint (a row that
// was measured differently, emphasis that moved the drawn cells, a caller that
// wraps to another width), and a disagreement here would misattribute the
// character in the one report a reader trusts.
//
// WHAT A RECORD IS, AND IS NOT. It names the TURN and the LIVE ROW the label was
// drawn on. It is not the answer's text, it never rewrites anything, and it is
// never rendered back out: the drawn bytes stay exactly the bytes the answer
// path produced (Rule 15 — report, never silently repair).
//
// BOUNDED (Rule 1: named constants, no bare literals). A body long enough to cut
// thousands of rows is ONE defect, not thousands — the ledger keeps the first
// `cutLabelLedgerCap` rows and COUNTS the rest, so the bound is never a silent
// truncation (Rule 15): CutLabels reports the total beside the kept rows.

const (
	// cutLabelLedgerCap is how many cut-label records one renderer keeps. It is
	// the same order as the answer-band report caps (a per-turn defect list, not
	// a log), and the count beside it makes the bound honest.
	cutLabelLedgerCap = 256

	// cutLabelRowUnknown is the row a record carries when the label was drawn
	// before any live row existed (a renderer a test built and never began).
	// Named rather than a bare -1 so a reader can tell "unknown" from "row -1".
	cutLabelRowUnknown = -1
)

// CutLabelRecord is ONE cell this renderer drew that the answer's own text did
// not contain: the product's cut label on the row it was drawn on, in the turn
// it was drawn in.
type CutLabelRecord struct {
	// Turn is the 1-based turn index the row belongs to (0 when the renderer was
	// never given an index — a renderer a test built alone).
	Turn int
	// Row is the LIVE ROW the label was drawn on, i.e. the row's identity inside
	// the renderer's own scrollback accounting — not a screen row and not a
	// source offset.
	Row int
}

// cutLabelLedger is the bounded record. It is its own type so the mutex that
// makes it readable from the turn-side goroutine stays with the data it guards
// (the renderer's fields are the read loop's; this one is written by the loop
// and read by the turn boundary).
type cutLabelLedger struct {
	mu    chan struct{} // a lock, not a channel of work: see lock/unlock
	rows  []CutLabelRecord
	total int
}

// lock takes the ledger's permit. A buffered channel is used instead of a mutex
// so the type stays copyable-in-spirit and needs no pointer receiver on the
// renderer's value accessors; it is acquired only on a cut (a row boundary) and
// released immediately, which is not a hot path.
func (l *cutLabelLedger) lock() {
	if l.mu == nil {
		l.mu = make(chan struct{}, 1)
	}
	l.mu <- struct{}{}
}

func (l *cutLabelLedger) unlock() { <-l.mu }

// add records one drawn label, keeping at most cutLabelLedgerCap rows but
// always counting.
func (l *cutLabelLedger) add(turn, row int) {
	l.lock()
	defer l.unlock()
	l.total++
	if len(l.rows) >= cutLabelLedgerCap {
		return
	}
	l.rows = append(l.rows, CutLabelRecord{Turn: turn, Row: row})
}

// snapshot returns the kept rows (a copy, so a caller may hold it across a later
// cut) and the total number of labels drawn.
func (l *cutLabelLedger) snapshot() ([]CutLabelRecord, int) {
	l.lock()
	defer l.unlock()
	rows := make([]CutLabelRecord, len(l.rows))
	copy(rows, l.rows)
	return rows, l.total
}

// CutLabels reports every cut label this renderer has drawn: the rows it kept
// (bounded by cutLabelLedgerCap) and the TOTAL it drew. It is the accessor a
// caller uses to answer "is this character one the ANSWER wrote, or one the
// RENDERER drew?" — the question the ellipsis report exists to answer.
//
// It is a RECORD, never a repair: nothing here changes what was drawn, and the
// answer's own text is never rebuilt from it.
func (r *AnswerRenderer) CutLabels() (rows []CutLabelRecord, total int) {
	if r == nil {
		return nil, 0
	}
	return r.cutLabels.snapshot()
}
