package lineedit

import (
	"strconv"
	"strings"
)

// window.go — the editing line's SCROLLING WINDOW: what the user sees of their
// own input, what is said about the rows the window does not reach, and the keys
// that move it.
//
// The defect this closes is a disclosure one. The editing line's row budget
// (editorRowsBudget) is finite, so a message larger than the window — a pasted
// 200-line block, whose newlines are CONTENT and therefore really are 200 buffer
// lines — was drawn as its first budget rows and NOTHING on screen said the other
// 188 existed. The user read their own pasted text, scrolled nowhere, and
// concluded the rest had been dropped; the buffer held all of it. Silent
// truncation of the user's own input is the defect; the ROW BOUND itself is
// legitimate (a layout must bound the editing line — that is the band's floor and
// the status row).
//
// THE SHAPE OF THE ANSWER, and the three rules it obeys:
//
//  1. The window FOLLOWS THE CARET when the text outgrows the budget. The head
//     window the renderer used to draw (and still draws while the input fits,
//     which is every ordinary message) put the caret's row off-window the moment
//     the line grew: the user's own typing moved out of sight below the last
//     drawn row. Past the budget the window keeps the caret's neighbourhood, so
//     what the user is typing is on screen.
//  2. What the window does not reach is STATED on the editing line, in the
//     product's own elision idiom (cutField/statusEllipsis — the same "…" the
//     status row and the request row cut with): the row above the text says how
//     many rows are hidden above it, the row below says how many are hidden
//     below. Both markers are CUT to the terminal's width rather than wrapped
//     (a wrapped marker would take a second row the layout did not give it).
//  3. The mechanism is a KEY the user can press and a LINE that names it: the
//     key row states the scroll keys (never "you should have known"), and it is
//     printed only when it is true — with the whole message on screen the row is
//     free text, because a mechanism advertised where there is nothing to scroll
//     is noise. The rows the mechanism needs are reserved by the LAYOUT
//     (editorChromeRows/EditorRowsMin in region.go), not borrowed from the text
//     window: borrowing them is why the marker and the keys were previously
//     visible only on a tall terminal, i.e. exactly when they were not needed.
//
// NOTHING HERE IS A BUFFER OPERATION: the buffer keeps every rune (a display
// bound is never an input bound), and the window is a pure function of the drawn
// rows, the caret and the row budget — so it is asserted on the rendered cell
// grid like every other layout decision in this package.

// The scrolling window's markers, spelled once (Rule 1). They carry meaning —
// they are what a user reads to learn that their input continues off the window —
// so they are constants and not inline strings.
const (
	// windowMarkAbove is the prefix of the row that stands in for the buffer rows
	// hidden ABOVE the window (older text). It renders as `… 12 lines above`.
	windowMarkAbove = "… "
	// windowMarkAboveTail completes the above-marker, with the count between.
	windowMarkAboveTail = " lines above"
	// windowMarkBelowTail completes the below-marker: rows past the window's end.
	windowMarkBelowTail = " lines below"
	// windowMarkJoin separates the two halves of the COMBINED marker — the one
	// row a terminal at the layout's floor can host, which has to state both
	// counts and the keys at once.
	windowMarkJoin = " · "
	// windowScrollKeys is what the key row prints: the mechanism, named where the
	// user is looking. Both bindings are spelled because they are one mechanism
	// (the paged motion and the single-row motion).
	windowScrollKeys = "PgUp/PgDn scroll · Alt+↑/↓ one row"
	// windowKeyRowPrefix leads the key row — the same ellipsis idiom, so a reader
	// sees at once that the row is chrome and not their text.
	windowKeyRowPrefix = "… "
)

// editingWindow is the resolved state of one editing-line repaint: which buffer
// rows the window shows, where the caret lands among them, and how many rows are
// hidden on each side. `offsets` is indexed BY DRAWN ROW and holds each row's
// buffer row (the index of the wrapped row it carries), or windowChromeOffset for
// a marked row — which is what lets a scroll key move the window by BUFFER rows
// instead of by screen rows, and lets the editor ask "is the caret on the row I
// think it is".
type editingWindow struct {
	offsets []int
	// cursorRow / cursorCol are the caret's cell among the drawn rows, and
	// cursorOffset is the buffer row the caret's row carries.
	cursorRow    int
	cursorCol    int
	cursorOffset int
	// above / below are the rows hidden above and below the window (0 = the whole
	// line is on screen, which is the ordinary case).
	above int
	below int
	// rows is how many rows the editing line drew (the layout's EditorRows).
	rows int
	// total is how many rows the buffer needs at this width — the number the
	// markers account for.
	total int
	// pinned is true when the window was placed by a scroll key rather than by
	// following the caret.
	pinned bool
}

// windowChromeOffset marks a drawn row that carries no buffer row (a marked row).
const windowChromeOffset = -1

// renderWindow bounds the drawn rows of the editing line to maxRows and reports
// the window it resolved.
//
// pinTop/pinned are the SCROLL KEY's state (see Editor.scrollEditingLine): while
// pinned, the window's first TEXT row is pinTop and the window does not follow
// the caret — that is the whole point of pressing the key, and the caret is then
// CLAMPED into the text it can see (a terminal cursor cannot be placed on a row
// the window did not draw). Without a pin the window follows the caret when the
// text does not fit (rule 1 in the file header) and keeps the head while it does
// (the historic behaviour, and the one the yield-order test pins).
func renderWindow(all []string, cursorRow, cursorCol, maxRows, width, pinTop int, pinned, keys bool) ([]string, editingWindow) {
	n := len(all)
	if n == 0 {
		return all, editingWindow{rows: 0, total: 0}
	}
	if maxRows <= 0 || n <= maxRows {
		// The whole line fits: no window, no markers, and every row is its own
		// buffer row. This is the ordinary editing session.
		offs := make([]int, n)
		for i := range offs {
			offs[i] = i
		}
		return all, editingWindow{
			offsets: offs, cursorRow: cursorRow, cursorCol: cursorCol, cursorOffset: cursorRow,
			rows: n, total: n,
		}
	}
	// The markers' room is taken from the layout's own reservation, and the text
	// window is floored at ONE row: a terminal that can host the layout at all
	// shows at least one row of the user's text beside the marker (the two-row
	// floor of editorRowsBudget is what makes that fit).
	textRows := maxRows - editorChromeRows
	if textRows < minEditorRows {
		textRows = minEditorRows
	}
	markers := maxRows - textRows
	if markers > editorChromeRows {
		markers = editorChromeRows
	}

	top := 0
	switch {
	case pinned && pinTop >= 0:
		top = pinTop
	default:
		// Follow the caret, caret LAST — the placement the historic head window
		// reached at the end of the buffer, so the ordinary "I am typing at the
		// end" case is unchanged. With the text taller than the window the caret
		// is therefore on the last text row.
		top = cursorRow - (textRows - 1)
	}
	// Clamp into the text's own span: whenever the window can be placed where the
	// text really has rows hidden on BOTH sides, it is; at either end of the
	// buffer that is impossible, and the near end's slack is what no marker has
	// to stand for.
	if top < 0 {
		top = 0
	}
	if top > n-textRows {
		top = n - textRows
	}
	// The row above the text states the count hidden ABOVE it, so when nothing is
	// hidden above (the window starts at the buffer's first row) that row is not
	// needed — and its cell goes to the TEXT instead: one more row of the user's
	// own input on screen, which is what the row was worth.
	if markers >= editorChromeRows && top == 0 && n > textRows+1 {
		textRows++
		if top > n-textRows {
			top = n - textRows
		}
	}
	above, below := top, n-textRows-top
	// leadMarker is whether the row above the text carries a marker (it is drawn
	// only when the buffer really has rows hidden above the window).
	leadMarker := markers >= editorChromeRows && above > 0

	drawn := make([]string, 0, maxRows)
	offsets := make([]int, 0, maxRows)
	// A marker is drawn only where there is something to account for: the row
	// above the text states the rows hidden ABOVE it. When only one chrome row is
	// available it goes BELOW and states both counts and the keys in one line —
	// the layout's floor case (region.go's EditorRowsMin) still gets the
	// mechanism, in one row instead of two.
	if leadMarker {
		drawn = append(drawn, windowMarkerRow(above, -1, false, width))
		offsets = append(offsets, windowChromeOffset)
	}

	for i := 0; i < textRows; i++ {
		drawn = append(drawn, all[top+i])
		offsets = append(offsets, top+i)
	}

	switch {
	case markers >= editorChromeRows && (keys || below > 0):
		// The row below the text: the count hidden below it, and — when the
		// window's own keys are this surface's business — the statement of the
		// mechanism. NOTHING HIDDEN BELOW AND NO KEY STATEMENT DUE (an owning
		// surface that claims those keys, e.g. `gopi chat --replay`): the row is
		// not drawn at all, because it would carry nothing but a sentence nobody
		// may read — the blank case is a row of the user's own text, not a row of
		// chrome with no content.
		drawn = append(drawn, windowMarkerRow(-1, below, keys, width))
		offsets = append(offsets, windowChromeOffset)
	case markers == 1 && (keys || below > 0):
		drawn = append(drawn, windowMarkerRow(above, below, keys, width))
		offsets = append(offsets, windowChromeOffset)
	}

	// The caret's cell among the DRAWN rows — and a DRAWN index is what it must
	// be. The row above the text is a drawn row like any other, so a text row's
	// drawn index is its index among the text rows PLUS that marker; publishing
	// the text index instead is the defect this arithmetic replaces (measured
	// 2026-10-04, epic 01a11811): the renderer walks back UP from the last drawn
	// row to cursorRow, so a caret one index short ended up one screen row too
	// high — on the buffer row ABOVE the user's own last row — and the column it
	// was then moved to was measured off that wrong row as well. The boundary is
	// exactly where a marked row first appears, i.e. the height at which the input
	// stops fitting and `… PgUp/PgDn scroll · Alt+↑/↓ one row` is drawn.
	//
	// The caret's buffer row is CLAMPED into the text span when a pinned window
	// left it outside, because a terminal cursor cannot be placed on a row the
	// window did not draw.
	cur := cursorRow
	if cur < top {
		cur = top
	}
	if cur > top+textRows-1 {
		cur = top + textRows - 1
	}
	row := cur - top
	if leadMarker {
		row++
	}
	col := cursorCol
	if cur != cursorRow {
		// The caret's row is not on screen: it is placed at the end of the row it
		// is clamped onto, exactly as the head-clip did — the terminal's cursor
		// must stay inside the editing line's span.
		col = cursorCell(drawn[row], drawn[row], width)
	}
	return drawn, editingWindow{
		offsets: offsets, cursorRow: row, cursorCol: col, cursorOffset: cur,
		above: above, below: below, rows: len(drawn), total: n, pinned: pinned,
	}
}

// windowMarkerRow composes one marked row of the editing line: the count(s) it
// stands for and, when asked, the keys that move the window.
//
// above/below are the counts to state (-1 = not this row's business), keys asks
// for the key statement. The result is CUT to width with the product's own
// elision marker rather than wrapped: a marker row that wrapped would take a
// second row of the layout, and the layout's arithmetic is not the renderer's to
// change (Rule: computePlan owns the rows).
func windowMarkerRow(above, below int, keys bool, width int) string {
	if above >= 0 && below >= 0 {
		// The floor case: one row, both counts, and the keys.
		s := windowMarkAbove + strconv.Itoa(above) + windowMarkAboveTail +
			windowMarkJoin + strconv.Itoa(below) + windowMarkBelowTail +
			windowMarkJoin + windowScrollKeys
		return markerRow(s, width)
	}
	if above >= 0 {
		return markerRow(windowMarkAbove+strconv.Itoa(above)+windowMarkAboveTail, width)
	}
	if below > 0 {
		s := windowMarkAbove + strconv.Itoa(below) + windowMarkBelowTail
		if keys {
			s += windowMarkJoin + windowScrollKeys
		}
		return markerRow(s, width)
	}
	// Nothing is hidden below: this row is the key statement alone — the
	// mechanism named on the editing line, where the user is looking.
	return markerRow(windowKeyRowPrefix+windowScrollKeys, width)
}

// markerRow bounds a marked row to the terminal's width. It uses cutField — the
// same cut-and-mark the status row and the request row use — so a narrow terminal
// reads a shortened marker instead of a wrapped one (Rule 14: one elision).
func markerRow(s string, width int) string {
	if width <= 0 || displayWidth(s) <= width {
		return s
	}
	return cutField(s, width)
}

// windowOffsetsAt returns the buffer row of the drawn row at index i, or
// windowChromeOffset for a marked row. It is a reader over the published window,
// used by the editor to move the window by BUFFER rows.
func (w editingWindow) windowOffsetsAt(i int) int {
	if i < 0 || i >= len(w.offsets) {
		return windowChromeOffset
	}
	return w.offsets[i]
}

// textRowsOf reports how many TEXT rows the window drew: the rows that carry the
// user's own input, as opposed to the marked rows that account for the rest.
func (w editingWindow) textRowsOf() int {
	n := 0
	for _, off := range w.offsets {
		if off != windowChromeOffset {
			n++
		}
	}
	return n
}

// firstTextOffset is the buffer row the window's first TEXT row carries.
func (w editingWindow) firstTextOffset() int {
	for _, off := range w.offsets {
		if off != windowChromeOffset {
			return off
		}
	}
	return 0
}

// lastTextOffset is the buffer row the window's last TEXT row carries.
func (w editingWindow) lastTextOffset() int {
	for i := len(w.offsets) - 1; i >= 0; i-- {
		if off := w.offsets[i]; off != windowChromeOffset {
			return off
		}
	}
	return 0
}

// windowMarkerText is the marker's own prefix vocabulary exposed for the tests
// that assert a dump carries a marked row (kept as one definition rather than
// re-spelled in a test — Rule 13).
func windowMarkerText(s string) bool { return strings.HasPrefix(s, windowMarkAbove) }

// windowHidesRows reports whether the editing line's window reaches LESS than the
// buffer's own rows — i.e. whether the arrow keys are the window's keys rather
// than the history's.
//
// IT IS A SEPARATE QUESTION FROM "DID THE WINDOW MOVE", and the two must never be
// conflated (the defect this predicate exists to keep out): at either end of the
// buffer the scroll legitimately reports no movement, and a caller that read that
// as "the window does not own this key" fell through to the history and REPLACED
// the buffer — Value.SetText, no second copy — so a multi-line draft was lost for
// ever on one arrow press. Ask THIS question to decide whose key it is, and the
// scroll's own return value only for whether a repaint is owed.
func (e *Editor) windowHidesRows() bool {
	win := e.render.Window()
	return win.total > win.textRowsOf()
}

// scrollEditingLine applies one scroll key (KeyPageUp/KeyPageDown/KeyScrollUp/
// KeyScrollDown) to the editing line's window and reports whether it MOVED —
// false means the window is already at that end and the caller should not repaint
// (a repaint that changes nothing is the busy-loop this editor must not do).
//
// The motion is by BUFFER rows, read from the window the renderer published (the
// drawn row index is not the buffer's row: a marked row takes an index of its
// own). Paging moves by the window's own text height minus one, so a reader never
// loses the row they were looking at — the same overlap a pager keeps.
func (e *Editor) scrollEditingLine(up bool, page bool) bool {
	win := e.render.Window()
	if win.total <= win.textRowsOf() || win.textRowsOf() == 0 {
		// Nothing is off the window: the keys are inert (and the key row is not
		// on screen to advertise them).
		return false
	}
	step := 1
	if page {
		step = win.textRowsOf() - 1
		if step < 1 {
			step = 1
		}
	}
	top := win.firstTextOffset()
	if e.render.scrolled {
		// A pinned window continues from where it is, not from where the caret is.
		top = e.render.pinTop
	}
	if up {
		top -= step
		if top < 0 {
			top = 0
		}
	} else {
		top += step
	}
	// The furthest a window can start: the last text row it can show is the
	// buffer's last row.
	if maxTop := win.total - win.textRowsOf(); top > maxTop {
		top = maxTop
	}
	if top < 0 {
		top = 0
	}
	if e.render.scrolled && top == e.render.pinTop {
		return false
	}
	e.render.SetWindowPin(top, true)
	return true
}
