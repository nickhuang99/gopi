package lineedit

import (
	"io"
	"strings"
	"sync"

	"github.com/nickhuang99/gopi/internal/sgr"
)

// region.go — the top-region compositor (A5).
//
// The editor reserves the screen's UPPER rows and streams the answer into a
// scrolling band at the very TOP; the chrome (editing line, completion menu,
// status line) sits BELOW the band. `CSI top;bottom r` confines LF to
// `[top, bottom]`: one LF at the band's last row (the answer's LIVE row)
// scrolls the band up by one, which is how a finalized line is promoted out of
// the live row — the codex TUI's mechanism (insert_history.rs:343-352, reset at
// :363-372), which reaches the same arrangement: a scroll region whose top
// margin is the screen's FIRST row and the composer below it.
//
// WHY THE BAND IS AT THE TOP — the one property this layout exists for.
// A terminal saves a line into its scrollback ONLY when that line is pushed off
// the SCREEN's first row. With a region whose top margin is below row 1, the
// rows scrolled off that top margin are DISCARDED — a "committed" answer line
// would be destroyed rather than kept (measured on real xterm 372: region
// [1;19] kept LINE-001 after 60 lines, region [2;19] lost lines 1..43 — see
// devdoc/chat-m2-streaming.md, round 5/6). The band is therefore anchored at
// the screen's first row and only its HEIGHT varies: the region top never
// moves, so every finalized line is pushed off the screen's top row (and thus
// saved) by construction, never off an interior margin (and thus destroyed).
//
// Screen layout (rows counted from the top; R = terminal height):
//
//	row 1 .. bandRowsMin           the answer band (the scroll region; its LAST
//	                               row is the in-flight answer's live row, and
//	                               its FIRST row is the screen's first row)
//	next editorRows                the editing line (multi-row: wraps/newlines)
//	next menuRows                  the completion menu (A7), drawn BELOW the line
//	row R                          the one-line status (model/iter/tokens)
//
// The three roles are separate ROWS with no arithmetic tying them together:
// the menu changes the band's BOTTOM (one CSI sequence) and never moves the
// editing line, so typing does not jump the cursor. The band is the only part
// that scrolls; the chrome below it is placed absolutely and is never carried
// away by streamed output.
//
// Everything the compositor writes outside plain text goes through an optional
// hook, so tests drive the layout without a terminal while production passes
// nil and only the real CSI sequences are emitted.

// Layout bounds. Named (Rule 1) so the arithmetic is readable in one place —
// these are policy, not literals.
const (
	// editorRowsCap is the MOST rows the editing line may draw when the terminal
	// has room for them — a cosmetic ceiling, NOT the row budget. The budget is
	// derived from the terminal's height (EditorRowsCap): the room above the
	// band's floor and the status row fills up as the user types, and a fixed
	// cap would silently discard the user's own typing (measured: a 200-char
	// line on a 12-row, 40-col terminal occupies 6 rows; capped at 4, everything
	// past row 4 was invisible while editing — the buffer kept it, the screen
	// did not). The buffer is never affected: this is a DISPLAY bound.
	//
	// IT IS THE OPERATIVE BOUND ON A REAL TERMINAL, and that is the second
	// incident this number carries: on a 24-row xterm the derived budget is 20,
	// so 12 is what actually decides, and a pasted 200-line message — a
	// bracketed paste carries its newlines as CONTENT, so the buffer really
	// holds 200 lines — was drawn as its first 12 rows with NOTHING on screen
	// naming the other 188. The ceiling is legitimate (the editing line must not
	// hand the whole screen to itself); the SILENCE was the defect, and it is
	// render.go's scrolling window that now answers it: the window follows the
	// cursor and a marked row states how many rows are off the window.
	editorRowsCap = 12
	// regionRowsMin is the fewest rows the answer band may have.
	regionRowsMin = 2
	// menuRowsCap is the most rows the completion menu may occupy.
	menuRowsCap = 5
	// statusRows is how many rows the status line occupies (always the last).
	statusRows = 1

	// editorChromeRows is how many rows the editing line needs for something
	// OTHER than the text the user is reading — the marked row that states how
	// many window rows are off the window ("… 40 lines above …") and the marked
	// row that names the scroll keys. They are CHROME in the strict sense: they
	// are rows the editing line occupies that carry no part of the buffer.
	// Reserving them here is what makes the scroll mechanism reachable at EVERY
	// terminal height — before it, the two rows were drawn ONLY when the layout
	// happened to leave a spare row (a tall terminal), so on a 24-row xterm the
	// mechanism and its marker were both invisible at exactly the moment they
	// were needed. EditorRowsMin (see editorRowsBudget) is what guarantees the
	// text keeps a row of its own beside them.
	editorChromeRows = 2

	// editorTopRow is the screen row the BAND starts on. It is the screen's
	// first row, and that is the whole point of the layout: a terminal saves a
	// line into its scrollback only when the line is pushed off the screen's
	// first row, so anchoring the band there is what makes a finalized answer
	// line SURVIVE instead of being discarded at an interior scroll-region
	// margin (measured on real xterm 372 — devdoc/chat-m2-streaming.md,
	// rounds 5/6). It is a named constant because it is the layout's central
	// invariant, not an incidental zero.
	editorTopRow = 0
	// minEditorRows is the fewest rows the editing line may draw — ONE, and it
	// is a floor rather than a wish because the editing line is the row the
	// layout must never sacrifice: the band yields to it, the menu yields to it,
	// and a terminal that cannot host even this much does not get the reserved
	// layout at all (rowsMinUsable, which the caller reports by name).
	minEditorRows = 1
	// EditorRowsMin is the fewest rows the editing line OCCUPIES under the
	// reserved layout: the text window it must always have (minEditorRows) plus
	// the chrome rows the scroll mechanism and its marker need
	// (editorChromeRows). It is computed from those two, never restated — a
	// second spelling is a second answer (Rule 14) — and it is what
	// editorRowsBudget floors at, so a terminal that hosts the layout at all
	// hosts the mechanism too.
	EditorRowsMin = minEditorRows + editorChromeRows
	// rowsMinUsable is the smallest terminal that gets a region at all: the
	// minimum band + the editing line's OWN floor (EditorRowsMin: its text window
	// plus the scroll chrome) + the status row. Below it the old full-screen
	// behaviour is kept (and the caller says so) rather than drawing chrome over
	// the user's own text.
	//
	// IT IS DERIVED FROM EditorRowsMin, not from minEditorRows, and that is the
	// deliberate consequence of the scrolling window (window.go): every terminal
	// that hosts the layout hosts the mechanism AND the one text row the mechanism
	// scrolls, because a layout that could host the editing line but not its
	// chrome would be a layout in which the user's input cannot be reached — the
	// defect this epic closes, in miniature. The price is paid once, at the
	// frontier: a 4-row terminal that used to get the layout now gets the
	// announced full-screen fallback instead, because 4 rows cannot host both the
	// band's floor and the editing line's.
	rowsMinUsable = regionRowsMin + EditorRowsMin + statusRows
)

// Layout is the resolved reserved layout, exported for callers that must place
// something on screen (or assert which row is which). The band is ABOVE the
// editing line — the scrollback condition — so nothing may assume the editing
// line is the screen's first row.
type Layout struct {
	EditorTop  int // 0-based first row of the editing line
	EditorRows int // rows the editing line occupies
	MenuTop    int // 0-based row of the menu's first line
	MenuRows   int // menu rows reserved (0 when no menu is on screen)
	BandTop    int // 0-based first row of the answer band (the screen's first row)
	BandBottom int // 0-based last row of the band == the LIVE answer row
	StatusRow  int // 0-based status row
}

// escResetScrollRegion takes the terminal out of any scrolling region
// (`CSI r`, codex insert_history.rs:363-372). The sequence comes from the one
// escape vocabulary (internal/sgr) rather than being composed here.
var escResetScrollRegion = sgr.ResetScrollRegionSeq()

// region is the reserved screen layout, in 0-based row indices internally and
// 1-based vt100 rows on the wire.
type region struct {
	// mu guards the published layout state — active, cols and plan — which is
	// read from a DIFFERENT goroutine than the one that writes it. The editor's
	// read loop owns the layout and republishes it (a menu opens, the editing
	// line grows), while the chat REPL's own goroutine reads it to refresh the
	// status row (StatusTick -> StatusRow/LiveRow) — measured as a real data
	// race under -race, not a theoretical one. A row index is a number: the
	// reader either sees the previous layout or the next one, never a torn pair
	// of fields from two layouts.
	mu sync.RWMutex

	// active is true once a scroll region has been installed. Inactive, every
	// operation degrades to flow output — the historic behaviour — so a
	// terminal too small for the layout is never left in a state it cannot
	// repaint.
	active bool
	rows   int
	cols   int

	// menuRows is the current menu height (0 = closed) and editorRowsWanted is
	// how many rows the editing line wants; both feed computePlan.
	menuRows         int
	editorRowsWanted int
	plan             plan

	// paintedBase / paintedRows record the menu rows currently on screen, so
	// the next paint (or a close) erases exactly what it vacates.
	paintedBase int
	paintedRows int

	outFn io.Writer

	// Test hooks (nil in production): every non-text write goes through one.
	onScrollRegion func(top, bottom int)
	onPlace        func(row, col int)
	onScrollUp     func(n int)
}

// newRegion builds an inactive (full-screen) region writing to out.
func newRegion(out io.Writer) *region {
	return &region{rows: defaultEditorCols, cols: defaultEditorCols, outFn: out}
}

// state returns the PUBLISHED layout — everything a second goroutine may read —
// in ONE acquisition, so a reader can never pair the editing row of one layout
// with the status row of another. Readers outside the editor's own loop (the
// chat REPL refreshing its status row, the tests replaying a screen) take this
// instead of touching the fields, and every writer below takes r.mu.
func (r *region) state() (active bool, rows, cols int, p plan) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.active, r.rows, r.cols, r.plan
}

// Enable installs the layout on a terminal of rows × cols. It reports false
// when the terminal is too small — the caller then keeps full-screen output
// and says so (never a silent layout change).
func (r *region) Enable(rows, cols int) bool {
	if rows < rowsMinUsable || cols <= 0 {
		return false
	}
	r.mu.Lock()
	r.rows, r.cols = rows, cols
	r.active = true
	r.menuRows = 0
	r.editorRowsWanted = 1
	r.mu.Unlock()
	r.layout()
	// THE TAKEOVER, and the anchor is part of it: the band's top margin is the
	// screen's FIRST row (Rule: the scrollback condition), so the same emission
	// homes the cursor there rather than at a bare 1.
	//
	// WHY THE SCREEN IS EMPTIED HERE, and not by the layout alone. The reserved
	// layout takes rows; it did not take the CELLS those rows already held. The
	// rows above the band's top are inside the band (BandTop is row 0), so the
	// band's own writes DO cover them — but only the band's rows, and only once
	// something is committed into it: everything the process or the shell wrote
	// before the layout was installed whose rows the band never repaints stays
	// lit above the startup screen for the whole session. Measured on a real pty
	// (evidence/fill.raw, 100x30, 40 filler lines pre-printed, then `gopi chat`):
	// the settled grid kept `FILLER-LINE-39/40` and the run's own two `[gopi]`
	// startup notices at rows 0..4 with the box beginning at row 5 — the screen
	// was shared rather than taken over. The reference CLI the operator asked
	// this surface to match opens on a CLEAN page.
	//
	// The takeover is ED(2) — "erase the whole display" — and it is emitted after
	// the cursor is homed, because ED erases FROM the cursor. Deliberately NOT
	// ED(3): the scrollback is not this surface's to discard (the startup screen
	// belongs in scrollback, region.go's own reason for anchoring the band at row
	// 0), and ED(2) leaves every line the terminal has already scrolled off the
	// screen exactly where it was.
	r.emitTo(sgr.CursorSeq(editorTopRow+1, editorTopRow+1) + escClearScreen)
	return true
}

// Resize re-installs the layout for a new geometry. A terminal that can no
// longer host the layout takes it DOWN (the scroll region is reset and the
// renderer returns to the full-screen path, where the editing line is always on
// screen) instead of keeping a stale geometry: the alternative — staying active
// with the old rows — places the editing line at a row the shrunken terminal no
// longer has, which is the "you> pushed off-screen" failure this layout must
// never produce. The caller announces the degradation and names the height the
// composer needs (Rule 15). Returns the new active state.
func (r *region) Resize(rows, cols int) bool {
	if !r.Active() {
		return false
	}
	if rows < rowsMinUsable || cols <= 0 {
		r.Disable()
		return false
	}
	r.mu.Lock()
	r.rows, r.cols = rows, cols
	r.mu.Unlock()
	r.layout()
	return true
}

// Cols is the drawing width the region was installed with — the width the answer
// renderer must wrap and compose its status line at. It is read from the region
// (one source of truth) rather than kept as a second copy of the terminal size.
func (r *region) Cols() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cols
}

// Disable returns the terminal to the full-screen layout.
func (r *region) Disable() {
	if !r.Active() {
		return
	}
	r.resetLayout()
	r.emitTo(escResetScrollRegion)
}

// Reset erases the reserved layout's rows and returns the terminal to the
// full-screen layout, leaving the cursor at the screen's first cell. It is the
// CHAT REPL's CLOSE-OUT: the last owner of the screen hands it back in a state
// the next program can draw into.
//
// WHY this is not Disable. Taking the scroll region down restores the terminal's
// frame, not the screen: the rows the layout owned (the answer band, the editing
// line, the status row) keep whatever was last painted on them, and the cursor
// stays parked on the editing row. Measured on the real PTY (termgrid.py,
// 100x30): after `/quit` the shell's next command ran ON the `you>` row, and the
// band's last rows plus the status row were still on screen — the operator's
// "the work area is not cleaned up, and the next prompt lands in the middle of
// it" (attachment tmp/ui_cleanup.txt).
//
// The hole is the cursor's position: ED erases from the CURSOR, so erasing
// without parking first leaves rows above the parked cursor painted. Hence: reset
// the region, home the cursor, erase the display — in that order — and the
// scrollback survives, because ED(2) never touches a line the terminal has
// already scrolled off the screen.
func (r *region) Reset() {
	if !r.Active() {
		return
	}
	r.Disable()
	r.emitTo(sgr.CursorHomeSeq())
	r.emitTo(escClearScreen)
}

// Active reports whether the layout is installed.
func (r *region) Active() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.active
}

// resetLayout drops the published layout to the degenerate one (no band, no
// menu, the editing line at the screen's first row). It is the state the region
// is in before any Enable — and the state a caller that has TORN THE LAYOUT DOWN
// must leave it in, whatever the order it tears it down in.
//
// WHY it is needed at all: a reader outside the editor's own loop (the chat
// REPL refreshing its status row publishes the region's LIVE ROW, which is a row
// of the layout) may look while the editor is closing. The published state and
// the layout have to move together, so the transitions live here rather than in
// whatever order a caller happens to close in.
func (r *region) resetLayout() {
	r.mu.Lock()
	r.active = false
	r.menuRows = 0
	r.plan = computePlan(r.rows, r.editorRowsWanted, 0)
	r.mu.Unlock()
}

// menuRowsNow reads the published menu height (the editor's loop writes it; a
// second goroutine never does, but the read must still be guarded — a racing
// read is a race whether or not the value is interesting).
func (r *region) menuRowsNow() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.menuRows
}

// SetMenuRows publishes the menu's current height and re-installs the band (the
// band's BOTTOM contracts as the chrome below it grows; its top margin — the
// scrollback condition — never moves). It reports whether the layout moved.
func (r *region) SetMenuRows(n int) bool {
	if n < 0 {
		n = 0
	}
	if n > menuRowsCap {
		n = menuRowsCap
	}
	if r.menuRowsNow() == n {
		return false
	}
	r.mu.Lock()
	r.menuRows = n
	r.mu.Unlock()
	return r.layout()
}

// plan is the resolved screen layout for one (terminal, menu, editor) shape.
// It is computed by computePlan, a PURE function, so the layout arithmetic is
// testable without a terminal and exists in exactly one place.
//
// Every role is a span of rows (0-based, inclusive) rather than a single
// position: each part moves as a block — the band's TOP is fixed at the screen's
// first row, the editing line's rows follow its BOTTOM, and the menu follows the
// editing line — so no part is positioned by arithmetic over another's contents.
type plan struct {
	EditorTop  int // 0-based first row of the editing line
	EditorRows int // rows the editing line may draw (1..editorRowsCap)
	MenuTop    int // 0-based row of the menu's first line
	MenuRows   int // menu rows that fit (0 = closed)
	BandTop    int // 0-based first row of the answer band (the screen's first row)
	BandBottom int // 0-based last row of the band == the LIVE answer row
	StatusRow  int // 0-based status row
}

// computePlan resolves the layout. Top to bottom: the answer band (whose TOP is
// the screen's first row — the scrollback condition), the editing line, the
// completion menu, the status line.
//
// THE YIELD ORDER is the rule this arithmetic encodes, and it is why the plan is
// computed in one pass rather than patched afterwards. The editing line is the
// row the user is looking at, so it is the LAST thing to give way — never the
// first:
//
//  1. The FLOOR is what nothing may breach: regionRowsMin for the band (below
//     which the caller's rowsMinUsable drops the whole reserved layout) plus
//     minEditorRows for the editing line. rowsMinUsable guarantees that floor
//     fits, so an ACTIVE layout always hosts both.
//  2. The editing line then takes what it asked for above that floor, bounded by
//     the terminal's DERIVED row budget (EditorRowsCap: `rows - statusRows -
//     regionRowsMin`, clamped to the cosmetic ceiling editorRowsCap and floored
//     at EditorRowsMin — the text window plus the editing line's scroll chrome).
//     The band's
//     floor bounds it upward, and the menu is resolved against this NUMBER
//     (step 3) rather than against a wish that the menu would then have to
//     shrink. The budget grows with the terminal, which is the whole point: on a
//     tall terminal the user reads their whole wrapped message while typing
//     instead of seeing a fixed 4 rows of it.
//  3. The menu takes what is left once the editing line AND the band's minimum
//     are served (capped by menuRowsCap, which the region publishes): a menu
//     never eats a row the editing line drew on or the band's floor.
//  4. The band absorbs the RESIDUAL — EVERY row nobody claimed, with no cap.
//     There is deliberately no maximum: the band is the only part anchored to
//     the screen's first row while the chrome is anchored to the status row at
//     the bottom, so a row the band does not take is a DEAD ROW between the two
//     — chrome floating in the middle of an empty screen, which is the
//     "composer is not at the bottom" defect the cap used to produce (measured:
//     at 30 rows the cap of 12 left rows 13..28 blank and the `you>` line sat
//     at row 13, seventeen rows above the status row it belongs to).
//
// The band therefore never grows at the editing line's expense: it is what is
// left over, and on a short terminal it sits at exactly its minimum while the
// chrome above the status row keeps every row it needs. The old arithmetic
// placed the editing line at `regionRowsMin` and then let the band/status
// arithmetic eat the rows it needed, which is how a `you>` line ended up off the
// screen at small heights.
//
// Below rowsMinUsable there is no layout at all: the returned plan is a
// degenerate placeholder the caller never installs (no scroll region is emitted
// for it), and its editing line is pinned to the screen's first row so a caller
// drawing through it cannot be pushed off-screen by a band that is not there. A
// surface that DRAWS from that plan must keep the editor on screen by other
// means (cmd/agent's cooked path does: no reserved rows are drawn at all).
func computePlan(rows, editorRowsWanted, menuRowsWanted int) plan {
	if rows < rowsMinUsable {
		return plan{
			EditorTop:  editorTopRow,
			EditorRows: minEditorRows,
			BandTop:    editorTopRow,
			BandBottom: editorTopRow + regionRowsMin - 1,
			StatusRow:  rows - statusRows,
		}
	}
	p := plan{
		BandTop:   editorTopRow,
		StatusRow: rows - statusRows,
	}
	// free is every row above the status line: the rows the band, the editing
	// line and the menu have to share.
	free := rows - statusRows

	// 2. The editing line: the rows it asked for, bounded by the terminal's own
	//    budget — what is left of `free` once the band's floor is reserved
	//    (EditorRowsCap, the ONE definition of that arithmetic) — and never below
	//    its minimum. The menu yields to this number (step 3), so it is resolved
	//    first.
	editingRows := editorRowsWanted
	if budget := editorRowsBudget(rows); editingRows > budget {
		editingRows = budget
	}
	if editingRows < minEditorRows {
		editingRows = minEditorRows
	}

	// 3. The menu: what is left once the editing line AND the band's minimum are
	//    served, capped by the region's own menu budget (the cap is enforced by
	//    SetMenuRows; re-applying it here keeps computePlan total for direct
	//    callers).
	menuRows := menuRowsWanted
	if menuRows > menuRowsCap {
		menuRows = menuRowsCap
	}
	if room := free - editingRows - regionRowsMin; menuRows > room {
		menuRows = room
	}
	if menuRows < 0 {
		menuRows = 0
	}

	// 4. The band: the residual, bounded only by its own minimum. The residual
	//    is the point — the band is the region that must reach the status row,
	//    so it takes every row nobody else claimed.
	bandRows := free - editingRows - menuRows
	if bandRows < regionRowsMin {
		bandRows = regionRowsMin
	}

	// Lay the three parts out top to bottom: the band first (its top margin is
	// the screen's first row — the scrollback condition), then the editing line,
	// then the menu directly below it.
	p.EditorTop = p.BandTop + bandRows
	p.EditorRows = editingRows
	p.MenuRows = menuRows
	p.MenuTop = p.EditorTop + p.EditorRows
	p.BandBottom = p.EditorTop - 1
	return p
}

// layout recomputes the plan and, when it changed, re-installs the scroll
// region. It reports whether the layout moved — the caller then repaints the
// editing line (the menu takes rows away from it).
//
// Only the band's BOTTOM is published: the top margin is always the screen's
// first row (a terminal only feeds its scrollback from a region anchored there),
// so a layout change never moves the margin the scrollback depends on — it
// resizes the band from below.
func (r *region) layout() bool {
	r.mu.Lock()
	p := computePlan(r.rows, r.editorRowsWanted, r.menuRows)
	was := r.plan
	changed := p != r.plan
	r.plan = p
	active := r.active
	r.mu.Unlock()
	if !changed || !active {
		return false
	}
	// A band that SHRINKS gives rows back from BELOW, and the rows it keeps are
	// still holding its content where the layout just stopped reserving them.
	// The editing line draws on the first row the band gave up, so without this
	// carry the row a committed answer sits on is ERASED by the next `you>`
	// repaint — the answer is painted, then overwritten, and an artifact read
	// afterwards reports `prefix=0` ("the paint lost it") for a line that WAS
	// painted. Measured 2026-09-26 (session 01a0dd28): the submitted message
	// wrapped, submit() raised the editing line's wish to 2 rows, the region went
	// `[1;24r` → `[1;23r`, and the answer row was gone from the settled dump
	// while a mid-delta frame still held it.
	//
	// The carry is the terminal's OWN promotion path: one LF at the band's live
	// row (the region's bottom) scrolls the whole band up by one row, so every
	// committed row keeps its text one row higher and the oldest one leaves
	// through the TOP into scrollback — preserved, never discarded. The rows the
	// band gives up are left blank for the editing line.
	if lost := was.BandBottom - p.BandBottom; lost > 0 {
		// ONE placement, ONE write: the carry is a burst of LFs and the read
		// loop must not land between the placement and them.
		r.emitAt(was.BandBottom, 0, strings.Repeat(escNewline, lost))
	}
	// The band EXPANDS into rows the editing line was using, and those rows still
	// hold the editing line's LAST PAINT. Clearing them first is what makes the
	// erase width cover the text being replaced — the same discipline the shrink
	// carry above applies in the other direction, and the one this writer was
	// missing (measured 2026-09-27, the operator's dump line 114).
	//
	// WHY THE VALUE IS `gained`, NOT THE WIDTH OF ANYTHING: the rows handed over
	// are exactly the ones between the OLD editing-line top and the NEW one
	// (EditorTop is BandTop + bandRows, so its movement IS the band's growth).
	// The editing line drew its own rows with `escEraseLine` before each of them
	// (render.go), so each retained row can hold text across its WHOLE width; the
	// band's live row is the only row an answer write REPLACES rather than
	// appends to, so the answer's first burst lands on that text and stops where
	// its own characters stop — the row keeps its full width and loses its middle,
	// with the old run's inline markup left unbalanced on screen. Measured on the
	// frame sequence: `▌ 我在 —— 自动 gc 已被禁用，仓库会持续膨胀。 之后它那一行归了 band
	// aaaa…`, i.e. the answer's sentence spliced onto the submitted message's
	// tail.
	//
	// `was.EditorRows > 0` excludes the degenerate plan the region holds before
	// its first Enable (EditorRows 0, EditorTop 0): there the "handed over" rows
	// are the whole screen, which the caller has not drawn yet and this function
	// must not wipe.
	if gained := p.EditorTop - was.EditorTop; gained > 0 && was.EditorRows > 0 {
		r.emitTo(r.clearRowsSeq(was.EditorTop, gained))
	}
	r.emitTo(sgr.ScrollRegionSeq(p.BandTop+1, p.BandBottom+1))
	return true
}

// clearRowsSeq composes the placements and whole-row erases that blank count rows
// from startRow, as ONE write: the read loop repaints the editing line between
// any two writes, so a placement emitted on its own lets that repaint land before
// the erase it was positioning (the seam every emission in this package is
// composed to avoid).
//
// Each erase is `escEraseLine` — the WHOLE row, never the erase-to-end that this
// package bans for display code (escEraseToEnd exists only for the one case whose
// arithmetic needs it, in answer.go). A whole-row erase is what makes "the pad
// width equals the width of the text being replaced" hold for every replaced
// width at once: the cleared span is at least as wide as anything the previous
// owner of that row could have drawn there.
func (r *region) clearRowsSeq(startRow, count int) string {
	var b strings.Builder
	for i := 0; i < count; i++ {
		r.placeInto(&b, startRow+i, 0)
		b.WriteString(escEraseLine)
	}
	return b.String()
}

// editorRowsWantedNow reads the published editing-row wish (guarded for the
// same reason as menuRowsNow: the read must not race the editor's loop).
func (r *region) editorRowsWantedNow() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.editorRowsWanted
}

// SetEditorRows publishes how many rows the editing line wants, so the band can
// move below them.
func (r *region) SetEditorRows(n int) bool {
	if n < 1 {
		n = 1
	}
	if r.editorRowsWantedNow() == n {
		return false
	}
	r.mu.Lock()
	r.editorRowsWanted = n
	r.mu.Unlock()
	return r.layout()
}

// Plan exposes the resolved layout (the editor and the tests read one
// definition of where every role lives).
func (r *region) Plan() plan {
	_, _, _, p := r.state()
	return p
}

// StatusRow is the 0-based row of the one-line status.
func (r *region) StatusRow() int {
	active, _, _, p := r.state()
	if !active {
		return 0
	}
	return p.StatusRow
}

// EditorTop is the 0-based first row of the editing line: everything the
// renderer draws starts here, and it is the ONLY row the renderer needs to know
// about the layout (the renderer walks down from it, exactly as it walked down
// from the screen's first row before the inversion).
func (r *region) EditorTop() int {
	active, _, _, p := r.state()
	if !active {
		return editorTopRow
	}
	return p.EditorTop
}

// LiveRow is the 0-based row the in-flight answer is written on (the band's
// last row — the only row streamed output ever rewrites).
func (r *region) LiveRow() int {
	active, _, _, p := r.state()
	if !active {
		return 0
	}
	return p.BandBottom
}

// BandRows is how many rows the answer band has.
func (r *region) BandRows() int {
	active, _, _, p := r.state()
	if !active {
		return 0
	}
	return p.BandBottom - p.BandTop + 1
}

// MenuCapacity is how many menu rows the region's CURRENT plan leaves the menu.
// It is the menu's own budget (menuRowsCap) intersected with what that plan
// hosts.
//
// IT IS A DIAGNOSTIC, NOT A SIZING ACCESSOR — measured 2026-09-30 (task
// 01a0f253): before a menu is published the region's plan holds no menu, and a
// menu-less plan gives the menu's rows to the BAND, so MenuTop lands on the
// status row and this returns 0 whatever the terminal height (the same
// arithmetic defect MaxMenuRows carried). A caller SIZING a menu must ask
// MaxMenuRows(editorRows) — the resolution of the plan that will hold it.
func (r *region) MenuCapacity() int {
	active, _, _, p := r.state()
	if !active {
		return 0
	}
	n := menuRowsCap
	if room := p.StatusRow - p.MenuTop; n > room {
		n = room
	}
	if n < 0 {
		n = 0
	}
	return n
}

// MaxMenuRows is the tallest menu the layout can host from an editing-line
// height of editorRows. The editor reads it BEFORE publishing that height, so
// the rows it reserves for the line and the room it leaves the menu resolve
// from one definition instead of two guesses that can disagree.
//
// THE MENU MUST BE IN THE PLAN — measured 2026-09-30 (task 01a0f253). The form
// this replaces computed `computePlan(rows, editorRows, 0)`: a plan with NO menu
// in it. With no menu the menu's rows go to the BAND (the residual, step 4 of
// computePlan), so MenuTop lands ON the status row and `StatusRow - MenuTop` is
// ZERO at every terminal height (measured: 26, 30 and 50 rows all answer 0 for
// every editing-line height). The caller's clamp (editor.clampedMenu, which
// shrinks the menu to this number) was therefore INERT: a menu taller than the
// room kept its full page and overran the status row instead of shrinking —
// measured on a real 117x8 pty, where the five-row `/` menu was drawn on rows
// 3..7 while the layout's status row is row 7, so the status line was gone from
// the grid. Asking the SAME arithmetic with the menu's own maximum in the plan
// returns the room the layout really leaves it (computePlan step 3).
func (r *region) MaxMenuRows(editorRows int) int {
	active, rows, _, _ := r.state()
	if !active {
		return 0
	}
	return computePlan(rows, editorRows, menuRowsCap).MenuRows
}

// EditorRowsCap is how many rows the editing line MAY draw on this region's
// terminal: the derived budget over the region's own published height, so a
// caller asks ONE source of truth instead of re-deriving it (Rule 14). It is
// the number the editor publishes as its wish, and computePlan applies the same
// bound to what it reserves, so the plan and the request cannot disagree.
func (r *region) EditorRowsCap() int {
	_, rows, _, _ := r.state()
	return editorRowsBudget(rows)
}

// editorRowsBudget is how many rows the editing line may draw on a terminal of
// `rows` rows: every row above the status line, less the band's own floor
// (regionRowsMin), clamped to the cosmetic ceiling editorRowsCap.
//
// It is THE definition of the editing line's row budget, and it is derived from
// the terminal's height rather than fixed — the defect it closes was an absolute
// cap of 4 that clipped the user's own wrapped typing off the screen on a
// terminal with room to show it (the buffer kept the text; the screen did not).
// The band keeps its floor at every height, so the editor grows into the room
// the band cannot use instead of starving the layout's other half. computePlan
// applies this same bound, so the height the editor PUBLISHES and the height the
// layout RESERVES are one arithmetic.
func editorRowsBudget(rows int) int {
	if rows < rowsMinUsable {
		// Below the floor there is no reserved layout at all; a caller drawing
		// through the degenerate plan gets one row (see computePlan).
		return minEditorRows
	}
	budget := rows - statusRows - regionRowsMin
	if budget > editorRowsCap {
		budget = editorRowsCap
	}
	// The floor is EditorRowsMin, NOT minEditorRows: every layout that is
	// installed at all must host the scrolling window's chrome, because those
	// two marked rows are the mechanism by which the user reaches the rest of
	// their input (and reaches it without a mouse). Floors are idempotent, so
	// the smallest hosting terminal charges the same two rows to the band that
	// a 24-row terminal charges — the guarantee bought is the mechanism's,
	// and the cost is one row of band on a 6-row screen.
	if budget < EditorRowsMin {
		budget = EditorRowsMin
	}
	return budget
}

// EditingRows is how many rows the editing line may DRAW in the current layout.
// It is the plan's own bound — the renderer asks the layout instead of repeating
// the arithmetic, and a redraw can therefore never grow into the menu or the
// status row (the rows below the editing line are reserved).
func (r *region) EditingRows() int {
	active, _, _, p := r.state()
	if !active {
		return editorRowsCap
	}
	return p.EditorRows
}

// PassThrough is the drawing helper the full-screen renderer needs after the
// layout inversion: the wire path for one control sequence, with the same
// deliberate-discard rationale as the other emitters.
func (r *region) PassThrough(seq string) { r.emitTo(seq) }

// out is the destination for the compositor's control sequences.
func (r *region) out() io.Writer {
	if r.outFn == nil {
		return io.Discard
	}
	return r.outFn
}

// cursorTo renders an absolute cursor placement (1-based on the wire).
func cursorTo(row, col int) string {
	return sgr.CursorSeq(row+1, col+1)
}

// emit writes one control sequence, ignoring a write failure for the same
// reason the renderer does: a failed repaint is not state, and the terminal is
// restored on the exit path regardless (the deliberate discard is stated here
// rather than left implicit).
func (r *region) emit(seq string) {
	_, _ = io.WriteString(r.out(), seq) //stylegate:ignore a failed repaint is not state — the terminal is restored on the exit path regardless, and the compositor has no error channel back to the editor's read loop
}

// emitTo writes one control sequence straight to the region's writer (used by
// layout, which must not route through the hook-based emit path while
// installing itself).
func (r *region) emitTo(seq string) {
	if r.outFn == nil {
		return
	}
	_, _ = io.WriteString(r.outFn, seq) //stylegate:ignore same as emit: see the reason there
}

// place moves the cursor to a 0-based row/col with an ABSOLUTE sequence. An
// inactive region writes nothing (the full-screen renderer owns the cursor
// itself with relative moves).
func (r *region) place(row, col int) {
	if !r.Active() {
		return
	}
	row, col = r.clampCell(row, col)
	if r.onPlace != nil {
		r.onPlace(row, col)
		return
	}
	r.emitTo(cursorTo(row, col))
}

// placeInto writes an absolute placement into a caller's own byte buffer. The
// renderer needs this: it emits several placements and the text between them as
// ONE write, so a placement must not jump the queue (writing the cursor moves
// directly while buffering the text would put the cursor in the wrong cell).
func (r *region) placeInto(b *strings.Builder, row, col int) {
	if !r.Active() {
		return
	}
	row, col = r.clampCell(row, col)
	if r.onPlace != nil {
		r.onPlace(row, col)
		return
	}
	b.WriteString(cursorTo(row, col))
}

// emitAt places the cursor at an absolute cell AND writes seq, as ONE write.
//
// This is the region's own version of the renderer's placeAndWrite, and it
// exists for the same measured reason: the REGION shares its byte stream with
// the editor's read loop, so a placement and the bytes it positions must be one
// unit of emission or the loop can land between them. A bare placement
// (`CSI 11;1H` alone) leaves the next writer's bytes — a line head, an erase, a
// scroll — going to whatever row the loop's repaint left the cursor on, which
// is how a row ends up with a blanked middle while its attributes describe the
// complete line, and how a row is torn mid-write.
//
// With every placement riding the bytes it positions, an interleaving between
// two writers falls between two SELF-POSITIONING units and is harmless — which
// is the whole property, and the one the test asserts.
func (r *region) emitAt(row, col int, seq string) {
	if seq == "" {
		return
	}
	if !r.Active() {
		r.emit(seq)
		return
	}
	var b strings.Builder
	r.placeInto(&b, row, col)
	b.WriteString(seq)
	r.emit(b.String())
}

// clampCell bounds a 0-based cell to the region's screen.
func (r *region) clampCell(row, col int) (int, int) {
	_, rows, cols, _ := r.state()
	if row < 0 {
		row = 0
	}
	if row >= rows {
		row = rows - 1
	}
	if col < 0 {
		col = 0
	}
	if col > cols {
		col = cols
	}
	return row, col
}

// commitScroll promotes the whole live row into the band with ONE scroll of the
// reserved region, then leaves the cursor on a fresh (empty) live row.
//
// With the band anchored at the screen's first row, that scroll pushes the
// band's top row OFF THE SCREEN — which is exactly when a terminal saves the row
// into its scrollback. This is the mechanism the whole layout exists for: the
// finalized answer line survives instead of being discarded at an interior
// margin.
//
// The cursor is placed ABSOLUTELY on the live row first. Relying on wherever the
// previous write left it is a defect, not a shortcut: the status line is drawn on
// a row BELOW the region, so a bare newline emitted after a status repaint moves
// the cursor off the bottom of the SCREEN — which scrolls the whole screen and
// wipes the editing row and the menu (measured: the chrome vanished mid-answer).
func (r *region) commitScroll() {
	if !r.Active() {
		return
	}
	if r.onScrollUp != nil {
		r.place(r.LiveRow(), 0)
		r.onScrollUp(1)
		return
	}
	r.emitAt(r.LiveRow(), 0, escNewline)
}

// RowsMinUsable is the smallest terminal height that gets the reserved chat
// layout (the same value rowsMinUsable applies). Exported so a caller can TELL
// the user the number it refused on, instead of writing "too small" with no
// figure (Rule 1: one definition, referenced).
func RowsMinUsable() int { return rowsMinUsable }

// WriteLines streams text into the answer band, the way a caller that is not
// the live answer (the REPL's own /help panel, a table printed by a slash
// command) must do it.
//
// WHY THIS EXISTS — the defect it closes. With the reserved layout installed,
// the terminal is left in a scrolling region ([1;bandBottom]) and the cursor is
// parked on the LAST row the editor drew. A plain `fmt.Println(out)` writes at
// that row and flows DOWNWARD: the text lands on the scrolling region's last
// row, each LF scrolls the band up by one, and the panel is painted on top of
// its own previous lines. Measured (SGR instrument, termgrid.py, 100×30):
//
//	12|you> Usage: /quit                       <- panel text on the EDITING row
//	13|                 End the interactive session and exit (Ctrl+D …)
//	14|                                                    The session's CHAT…
//
// Three rows of one panel interleaved with the band's committed rows — the
// "torn row" the epic reports.
//
// The rule is the one the answer renderer already follows: the live row is
// ADDRESSED, never flowed to, and a row is promoted out of the live row by ONE
// LF inside the scrolling region. So the text takes exactly the same path a
// committed answer line takes, and a line the user reads is never rewritten.
//
// It is a no-op (reported as such) on an inactive region: there the full-screen
// writer owns the cursor and the caller keeps the historic flow path.
func (r *region) WriteLines(text string) bool {
	if !r.Active() {
		return false
	}
	// Trailing breaks are dropped: the caller's text ends the panel, and every
	// blank line would cost a band row for nothing.
	text = strings.TrimRight(text, "\n")
	if text == "" {
		// A caller that passed ONLY breaks asked for a blank row (the REPL's
		// inter-turn separator). The flow path's own "\n" produced one, so one
		// blank row is emitted here too — otherwise the reserved path would
		// silently run two turns together while the piped path separates them.
		r.writeBandLine("")
		return true
	}
	for _, line := range strings.Split(text, "\n") {
		r.writeBandLine(line)
	}
	return true
}

// writeBandLine draws ONE line on the live row and promotes it into the band,
// exactly as a committed answer line is drawn (writeLive + one region scroll).
//
// A line wider than the band is SPLIT rather than wrapped by the terminal: a
// row that fills itself would otherwise leave the emulator's pending-wrap state
// armed, and the next absolute placement would then advance a row on its own —
// the same "one LF scrolls two rows" class of bug the answer renderer's own
// wrap handling exists to avoid.
func (r *region) writeBandLine(line string) {
	cols := r.Cols()
	if cols <= 0 {
		return
	}
	for {
		head, tail := cutCols(line, cols)
		// Clear the live row FIRST, then write into it: an erase AFTER the
		// write would wipe the line the scroll is about to promote. Each step
		// carries its own placement (emitAt) so the read loop cannot land
		// between a placement and the bytes it positions.
		r.emitAt(r.LiveRow(), 0, escEraseLine)
		r.emitAt(r.LiveRow(), 0, head)
		// ONE LF inside the scrolling region: the whole live row is promoted
		// out of the region and a fresh empty live row takes its place. The
		// absolute placement before it also clears the emulator's pending-wrap
		// state, so a row that fills every column cannot advance two rows here.
		r.emitAt(r.LiveRow(), 0, escNewline)
		if tail == "" {
			return
		}
		line = tail
	}
}
