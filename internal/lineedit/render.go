package lineedit

import (
	"io"
	"math"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sgr"
)

// Renderer draws one editing line (prompt + buffer) onto a terminal, re-drawing
// in place as the buffer changes.
//
// THE EDITING LINE IS PAINTED: THE PROMPT IN ONE STYLE, THE USER'S BUFFER IN
// ANOTHER. The palette styles BOTH halves — the prompt by Prompt, every cell
// after it by Buffer — so the row the user is looking at while typing is part of
// the system's colour like the status row above it.
//
// THIS REVERSES THE EARLIER "THE BUFFER IS NOT STYLED" DECISION (C1), and the
// reason is worth keeping, because the old rule was not careless — it was
// answering a different question. Its argument: *what the user typed is theirs,
// and colouring it would put the palette's judgement on their input.* That is a
// good reason to refuse a judgement, and it is not a reason to leave the text at
// the terminal's default colour: SGR 39 is a colour too (whatever the theme
// declares for plain text), and choosing it for the input was a colour decision
// made by omitting one. Measured on the operator's screen, the row read
// `0:5=fg=colour-186 bold 5:112=default` — a pale-gold prompt beside
// default-coloured input, on a screen whose status row is gold. The operator
// reversed the verdict after complaining twice in a row; a surface that is the
// only uncoloured chrome on screen is not "neutral", it is the one that looks
// broken.
//
// WHAT ACTUALLY ANSWERS THE OLD OBJECTION IS THE VALUE, NOT THE OLD RULE: the
// buffer takes the SAME gold family as the rest of the interface (the floor's
// named gold, the cube's #FFD700, Hermes' primary — see each table). It marks
// "this is the editing line", never "this input is good/bad", and it encodes no
// meaning the text does not already carry — so the monochrome rule (colour never
// carries information alone) is untouched. NOTHING IS JUDGED; THE ROW IS PAINTED.
//
// The emphasis is applied to a row AFTER the layout has been computed
// (layoutText measures plain text), and the drawing code adds the row's spans to
// it — zero bytes on a monochrome palette (Style.Wrap returns its text and the
// zero table yields no spans), so the historic output is byte-identical when
// colour is off.
//
// Layout model: prompt and buffer form one logical text that wraps at the
// terminal width (a shell does the same), and an embedded newline starts a new
// row — which is what makes multiline (and pasted) input readable rather than a
// single wound-around line. The renderer remembers how many rows above the
// cursor its own output occupies (cursorRows) and how many it drew in total
// (drawnRows), which is what makes an in-place redraw possible: walk up, repaint
// every row (erasing each first, so a shorter line leaves no residue), then walk
// back down to the cursor cell.
//
// Rendering is verified by feeding these bytes through Screen (a vt100 cell
// grid) and asserting the grid: "the callback fired" cannot see a cursor one
// column off (engineering rule 10.2).
type Renderer struct {
	out        io.Writer
	prompt     string
	promptCols int
	// palAtomic is the emphasis table for the editing line. The zero value
	// (mono) leaves the historic bytes untouched.
	//
	// WHY A POINTER, AND WHY ATOMIC. The palette is CONFIGURATION, not state:
	// it is published once per surface (colour.go resolves the capability
	// decision a single time) and every redraw READS it. A Palette is an
	// immutable value — a set of named SGR emitters, none of which a reader ever
	// writes — so publishing it as a pointer snapshot gives the readers a
	// happens-before edge with the publisher and lets the value itself stay
	// shared without a copy per frame. A MUTEX would have said the same thing
	// more expensively (every row's emphasis would take a lock) and would have
	// implied the table is mutable in place; it is not.
	palAtomic atomic.Pointer[Palette]
	// promptSegs is the prompt's rune count split into the prompt text and the
	// number of leading cells it occupies, remembered so the emphasis prefix can
	// be placed on the same rows the plain prompt occupies. promptRunes is that
	// text as runes — the layout works in runes, so the segment split must too.
	promptRunes []rune
	cursorRows  int // rows above the cursor that our own output occupies
	// drawnRows is how many rows the last Redraw painted. It has ONE writer
	// (the read loop's Redraw/Finish/Reset) and NUMEROUS READERS, several of
	// which are on other goroutines — a surface asking how tall the editing line
	// is does not run on the loop. Atomic rather than plain because the readers
	// outside the loop would otherwise race the store; not a lock, because no
	// second writer is being admitted (Rule 15: the single-writer invariant is
	// the thing being kept).
	drawnRows atomic.Int64
	// cursorCol is the cursor's column in the last layout, remembered so a
	// component that draws BELOW the editing rows (the completion menu) can
	// hand the cursor back to where the user is typing.
	cursorCol int
	// placedRow is the screen row the last Redraw started on (the layout's
	// editing row at that time). A redraw whose start row MOVED must erase the
	// rows it vacated: they are now the band's or the menu's, and nothing else
	// would ever clear them (measured: the editing line's old row kept the
	// typed text after the menu pushed the line down).
	placedRow int

	// window is the SCROLLING WINDOW of the last Redraw: which buffer rows the
	// editing line showed, how many it hid on each side, and the caret's row among
	// them. It is what a scroll key reads to move by BUFFER rows instead of by
	// screen rows (the drawn row's index is not the buffer's row — a marked row
	// takes an index of its own), and it is the published state the marker rows
	// are derived from (window.go).
	window editingWindow
	// pinTop and scrolled are the scroll key's own state: while scrolled, the
	// window does not follow the caret and its first TEXT row is pinTop (a buffer
	// row). Both are cleared whenever the line's content is not the user's ongoing
	// typing (a redraw that fits, a new line, a submit, a recalled history entry)
	// — a pinned window left behind would hide the very text a new line is about.
	pinTop   int
	scrolled bool
	// noWindowKeys turns OFF the key statement on the editing line's MARKER rows
	// (windowScrollKeys). It is SET by a read-only performance surface: `gopi
	// chat --replay` types a recording into this editor and OWNS it, so the
	// watcher's keys are not this window's keys — a row telling them "PgUp/PgDn
	// scroll · Alt+↑/↓ one row" was advertising a binding the surface refuses,
	// and it was redrawn on every frame while the driver typed (measured
	// 2026-10-22 on a real pty: 103 occurrences in one replay, 33 of them after
	// the first key press). The COUNTS stay drawn — "… 8 lines above" is a fact
	// about the user's own text; only the key names go. The ZERO VALUE SHOWS THE
	// KEYS (Rule: the default is the historic behaviour, and a flag whose zero
	// value silently changed every other editor's screen would be a trap).
	noWindowKeys bool

	// draftAt is where the DRAFT'S first rune landed on the row grid, as recorded
	// by the layout of the LAST Redraw. The emphasis pass runs per row after the
	// layout, so it reads the position the layout produced — never a second
	// derivation of it (Rule 14, and see layoutTextDraftPos: a cell-vs-rune mix-up
	// here is a live defect on wide text).
	draftAt draftPos
	// draft is the DRAFT SEGMENT the editing line carries after the buffer
	// (draft.go): text a streaming producer is still refining, laid out as the
	// continuation of the user's own line but drawn under the Draft emphasis and
	// kept OUT of the buffer. Empty means no draft — the ordinary state, and the
	// one every caller before this field existed was in.
	draft string

	// region, when set, confines this renderer's output to a screen region and
	// replaces its relative placement with absolute placement. The renderer's
	// contract is unchanged either way: it draws prompt+buffer at the cursor
	// and leaves the cursor on the editing row.
	region *region
}

// defaultEditorCols is the width assumed when the kernel cannot report one.
const defaultEditorCols = 80

// NewRenderer builds a renderer that draws to out.
func NewRenderer(out io.Writer) *Renderer { return &Renderer{out: out} }

// SetRegion attaches the screen region the renderer draws inside and returns
// it, so the caller can enable/disable the region and place the rows below the
// editor. A nil region detaches it (full-screen placement, the historic
// behaviour).
func (r *Renderer) SetRegion(reg *region) *region {
	r.region = reg
	return reg
}

// Region returns the attached region (nil when the renderer owns the whole
// screen).
func (r *Renderer) Region() *region { return r.region }

// DrawnRows returns how many rows the last Redraw painted (the menu starts
// directly below them).
func (r *Renderer) DrawnRows() int { return int(r.drawnRows.Load()) }

// Window returns the scrolling window the last Redraw resolved (window.go): the
// buffer rows it showed, the rows it hid on each side, and the caret's row. An
// empty window (Rows 0) means no reserved layout was in force, i.e. nothing was
// windowed at all.
func (r *Renderer) Window() editingWindow { return r.window }

// SetWindowPin pins the window's first text row for the next redraw (a buffer
// row), or unpins it with pinned=false so the window follows the caret again.
// It is the scroll key's handle on the drawing: the editor resolves new offsets
// from the window, and the renderer draws what the editor pinned.
func (r *Renderer) SetWindowPin(top int, pinned bool) {
	r.pinTop = top
	r.scrolled = pinned
}

// unpinWindow returns the window to caret-following. It is called wherever the
// line's content is not the continuation of the user's own typing (a new line, a
// submit, a history recall): a pinned window left behind would show the middle of
// a message the user is no longer editing.
func (r *Renderer) unpinWindow() {
	r.pinTop = 0
	r.scrolled = false
}

// SetWindowKeyRow turns the key statement ON the marker rows (the default) or
// OFF. It exists for the ONE surface that owns the editing line and refuses the
// keys that statement would advertise: `gopi chat --replay`, where the watching
// operator types into a recording and every key is the control layer's, so
// "PgUp/PgDn scroll · Alt+↑/↓ one row" names a binding that does nothing here.
//
// WHAT IT DOES NOT TURN OFF: the marker rows themselves and their COUNTS. "…
// 8 lines above" is a fact about the user's text, and a surface that hid it would
// be lying about the buffer instead of about a key (window.go's markerRow keeps
// the count and drops only the key clause). The name says KEY ROW because that is
// the whole of what it controls — a reader looking for "hide the markers" is
// looking for a different switch this surface must never pull.
func (r *Renderer) SetWindowKeyRow(on bool) {
	r.noWindowKeys = !on
}

// WindowKeyRow is the READER beside SetWindowKeyRow: whether the marker rows
// currently state the scroll keys. It exists so a surface can assert the state it
// put the renderer in rather than trusting the call it just made — the same
// reader/writer pair SetInputLocked/InputLocked has, for the same reason.
func (r *Renderer) WindowKeyRow() bool { return !r.noWindowKeys }

// Prompt publishes the prompt text that precedes the buffer.
func (r *Renderer) Prompt(prompt string) {
	r.prompt = prompt
	r.promptCols = displayWidth(prompt)
	r.promptRunes = []rune(prompt)
}

// SetPalette installs the emphasis table for the editing line (the prompt). It
// is called while the renderer is built, before the first redraw.
//
// It stores a SNAPSHOT, so a caller that publishes the table after the read loop
// is already running still hands the loop a whole value (see palAtomic: the
// table is configuration, and every reader loads the published pointer).
func (r *Renderer) SetPalette(p Palette) { r.palAtomic.Store(&p) }

// palette returns the published emphasis table (the zero table when none was
// installed — the historic monochrome bytes).
func (r *Renderer) palette() Palette {
	if p := r.palAtomic.Load(); p != nil {
		return *p
	}
	return Palette{}
}

// SetDraft publishes the DRAFT SEGMENT this renderer must lay out after the
// buffer (draft.go). It is the read loop's own call — the draft's text lives in
// the editor's queue and the loop was the only writer of the line before this
// field existed, so the invariant is unchanged: the renderer is never told
// anything by a goroutine that is not the loop.
//
// It takes the text rather than reading it from the editor so the layout is a
// pure function of what was published (the same shape Prompt already has), which
// is what lets a test drive the renderer directly.
func (r *Renderer) SetDraft(text string) { r.draft = text }

// Control writes a raw control sequence (e.g. a screen clear) that is not part
// of the prompt+buffer layout.
func (r *Renderer) Control(seq string) {
	if r.out == nil {
		return
	}
	// deliberately ignored: a failed render is feedback, not state — the
	// terminal's own state is restored on the exit path regardless.
	_, _ = io.WriteString(r.out, seq)
}

// ControlAt writes a raw control sequence at an ABSOLUTE cell as ONE write.
//
// It exists because the renderer's output stream is shared with the editor's
// read loop: a placement written on its own is a window the loop can land in,
// and the bytes it was positioning then go to whatever row that repaint left
// the cursor on (the measured cause of a band row whose text and attrs
// disagree). A caller that must both position the cursor and write text has to
// do it in one write, and this is that write's ONE definition.
func (r *Renderer) ControlAt(row, col int, seq string) {
	if r.out == nil || seq == "" {
		return
	}
	if r.region == nil || !r.region.Active() {
		// No region: the full-screen path owns the cursor with relative moves,
		// and an absolute cell would fight it. The caller's sequence is written
		// where the cursor is, exactly as Control does.
		r.Control(seq)
		return
	}
	var b strings.Builder
	r.region.placeInto(&b, row, col)
	b.WriteString(seq)
	// deliberately ignored for Control's own reason: a failed repaint is not
	// state.
	_, _ = io.WriteString(r.out, b.String())
}

// Redraw repaints the prompt+buffer in place, cursor at v.Cursor().
//
// With a region attached the placement is ABSOLUTE (`CSI row;col H` to the
// editing line's FIRST row, then the same relative moves as the full-screen
// path); without one it is the historic relative walk. Absolute placement is
// what makes a redraw independent of where streamed output left the cursor, and
// it is what lets the reserved layout sit BELOW the answer band: the renderer
// never assumes its first row is the screen's first row, it asks the layout
// (region.EditorTop) — the same shape as the full-screen path, which also walks
// down from its own first row.
func (r *Renderer) Redraw(v *Value, width int) {
	if r.out == nil || width <= 0 {
		return
	}
	rows, cursorRow, cursorCol, at := layoutTextDraftPos(r.prompt, v.Text(), r.draft, v.Cursor(), width)
	r.draftAt = at
	var b strings.Builder
	var reg *region
	if r.region != nil && r.region.Active() {
		reg = r.region
		// THE BOUND THE LAYOUT OWNS: the editing line may draw EditingRows rows
		// and no more. Doing it HERE (rather than trusting the caller's own cap)
		// is what makes the reserved rows real — a redraw that drew past its
		// bound would write on the menu's rows or, at the band's expense, push
		// the layout's published geometry out of agreement with the screen.
		//
		// The window keeps the line's HEAD, so the prompt is always visible: the
		// editing line is what the user is looking at, and a `you>` that scrolled
		// out of its own row would leave the prompt unreadable. The buffer keeps
		// the whole text (a DISPLAY bound, never an input bound) and the cursor
		// is left on the last drawn row rather than being pushed off-screen.
		//
		// CORRECTION (2nd incident, pasted input): "keep the head" is right for
		// the FIRST rows and wrong for a line that does not fit at all — the
		// drawn rows are now the scrolling window renderWindow resolves (the
		// caret's neighbourhood once the text outgrows the budget, the head while
		// it fits, a key-pinned span while the user is scrolling), and the rows
		// the window does not reach are STATED ON the editing line itself — never
		// silently dropped. The head-only rule survives intact for every input
		// that fits, which is why the yield-order test's terminal is unaffected.
		if maxRows := reg.EditingRows(); maxRows > 0 {
			var win editingWindow
			rows, win = renderWindow(rows, cursorRow, cursorCol, maxRows, width, r.pinTop, r.scrolled, !r.noWindowKeys)
			r.window = win
			cursorRow, cursorCol = win.cursorRow, win.cursorCol
		}
		if cursorRow < 0 {
			cursorRow = 0
		}
		// A redraw whose start row MOVED (the layout gave the editing line a
		// different row because the menu opened, the band shrank, or the buffer
		// grew): the rows the previous drawing occupied belong to somebody else
		// now, so erase them before drawing. Without this the vacated row keeps
		// the previous text for ever — it is outside every other paint's reach.
		if drawn := int(r.drawnRows.Load()); drawn > 0 && r.placedRow != reg.EditorTop() {
			r.eraseRows(&b, reg, r.placedRow, drawn)
		}
		// Start at the editing line's first row; every move below is relative to
		// it and the renderer walks back down to cursorRow, so the terminal's
		// cursor ends on the editing row whatever the previous output did.
		reg.placeInto(&b, reg.EditorTop(), 0)
	} else {
		// Always start at column 0 of the current row: a previous redraw left
		// the cursor wherever its own placement put it, and erasing from there
		// would shift the whole line.
		b.WriteString(escCarriage)
		if r.cursorRows > 0 {
			b.WriteString(moveCursor('A', r.cursorRows))
		}
	}
	total := len(rows)
	if drawn := int(r.drawnRows.Load()); drawn > total {
		total = drawn // erase the rows left by a previously longer line
	}
	// WITH A REGION, THE REPAINT SPAN IS THE LAYOUT'S. `total` above exists to
	// erase rows a previously LONGER line left behind, and in the full-screen path
	// those rows are the renderer's to clear (nobody else draws there). Under the
	// reserved layout they are NOT: the editing line owns exactly
	// [EditorTop, EditorTop+EditorRows), the rows BELOW it are the menu's and the
	// status row's, and the rows it GAVE UP when it shrank were already cleared by
	// region.layout's own "the band expands into the editing line's rows" carry
	// (clearRowsSeq) — which is where that contract lives (Rule 14: one owner per
	// row). Letting the loop run past the span placed the cursor below the screen
	// as well: placements clamp, so the withdrawn rows all landed on the LAST
	// screen row and the walk-up back to the caret then started from a row it never
	// reached — measured 2026-10-04 (epic 01a11811, the `drawnRows > len(rows)`
	// direction): 15 rows of input → 13 (windowed, 12 drawn) → 6, and the caret
	// came to rest on the editing line's FIRST row instead of the buffer's last
	// (grid row 17 where 22 was due).
	if reg != nil {
		if span := reg.EditingRows(); total > span {
			total = span
		}
	}
	step := func(i int) string {
		if reg != nil {
			var sb strings.Builder
			reg.placeInto(&sb, reg.EditorTop()+i, 0)
			return sb.String()
		}
		// ESC[1B + CR, NOT a bare LF: raw mode cleared OPOST, so a bare LF moves
		// down without returning the column.
		return escCarriage + moveCursor('B', 1)
	}
	for i := 0; i < total; i++ {
		if i > 0 {
			b.WriteString(step(i))
		}
		b.WriteString(escEraseLine)
		if i < len(rows) {
			b.WriteString(wrapSegs(rows[i], r.editingRowSegs(i, width)))
		}
	}
	if up := total - 1 - cursorRow; up > 0 {
		b.WriteString(moveCursor('A', up))
	}
	b.WriteString(escCarriage)
	if cursorCol > 0 {
		b.WriteString(moveCursor('C', cursorCol))
	}
	r.cursorRows = cursorRow
	r.cursorCol = cursorCol
	r.drawnRows.Store(int64(len(rows)))
	if reg != nil {
		r.placedRow = reg.EditorTop()
	}
	// deliberately ignored: as above — a render failure must not abort editing.
	_, _ = io.WriteString(r.out, b.String())
}

// editingRowSegs returns the emphasis of one drawn row of the editing line: the
// leading runes that came from the PROMPT under the prompt's style, and every
// cell after them — the user's buffer — under the buffer's style.
//
// The prompt's span comes first because the prompt is drawn first and the two
// halves form one wrapped text, so the prompt's own wrap determines how many of
// its runes land on each row — prompting alone at the same width gives exactly
// those counts (a wrap boundary cannot fall inside the prompt differently when
// more text follows it). It is a row-local decision either way: the count is
// stable across redraws, and a row the prompt does not reach at all is buffer
// from its first cell.
//
// EVERY ROW IS COVERED, not only the rows the prompt occupies: a buffer rune that
// wrapped onto a row of its own is still the editing line, and a multi-row input
// is exactly the case the default-coloured defect was visible in.
//
// A monochrome palette yields no spans here (both styles are the zero Style, so
// the run is dropped) and the historic bytes are untouched.
func (r *Renderer) editingRowSegs(row int, width int) []seg {
	// A MARKED ROW carries no buffer text, so it carries no buffer emphasis
	// either: styling the marker row as the user's own input would put the
	// palette's "your text" colour on a row that is the product talking (and the
	// prompt's span arithmetic does not apply to a row the window synthesised).
	if r.region != nil && r.region.Active() && len(r.window.offsets) > 0 && r.window.windowOffsetsAt(row) == windowChromeOffset {
		return nil
	}
	segs := r.promptEmphasis(row, width)
	pal := r.palette()
	if pal.Buffer.Empty() && (r.draft == "" || pal.Draft.Empty()) {
		return segs
	}
	// The rest of the row — everything after the prompt's own runes on it — is
	// the user's buffer, and where the DRAFT SEGMENT begins the row changes
	// emphasis: the draft is drawn under Draft's style so the operator can see at
	// a glance which words are theirs and which are still being guessed
	// (draft.go). The walk in wrapSegs is positional, so these spans simply
	// follow the prompt's: on a row the prompt never reached there is no span
	// before it and the buffer's style starts at the row's first cell, which is
	// how a wrapped continuation row is covered.
	if r.draft == "" || pal.Draft.Empty() {
		return append(segs, seg{n: bufferRestRunes, st: pal.Buffer})
	}
	// The prompt's runes on this row are already consumed by `segs`, so the
	// arithmetic below counts from the first cell the prompt did not take.
	promptCells := 0
	for _, sg := range segs {
		promptCells += sg.n
	}
	if lead, ok := r.draftLeadOnRow(row, promptCells); ok {
		if lead > 0 {
			segs = append(segs, seg{n: lead, st: pal.Buffer})
		}
		segs = append(segs, seg{n: bufferRestRunes, st: pal.Draft})
		return segs
	}
	return append(segs, seg{n: bufferRestRunes, st: pal.Buffer})
}

// draftLeadOnRow reports how many RUNES of a drawn row come before the DRAFT
// begins on it, and whether this row carries any part of the draft.
//
// It reads the position the layout recorded (r.draftAt) rather than deriving one,
// which is what makes the coloured span and the drawn text the same answer by
// construction: there is no second model of the wrapping to disagree with.
//
// promptRunes is how many of THIS row's leading runes the prompt took — the layout
// laid the prompt out first, so the draft's own row can begin with prompt runes and
// they are not the buffer's.
func (r *Renderer) draftLeadOnRow(row, promptRunes int) (int, bool) {
	if r.draft == "" || r.draftAt.row < 0 {
		return 0, false
	}
	if row < r.draftAt.row {
		return 0, false
	}
	if row > r.draftAt.row {
		// Rows AFTER the draft's first: the draft is one unbroken run of text and
		// nothing follows it, so once it has begun it owns every rune of every
		// following row. (The prompt precedes the buffer, and the draft comes after
		// both, so no prompt rune can appear here.)
		return 0, true
	}
	lead := r.draftAt.runesBefore - promptRunes
	if lead < 0 {
		return 0, true
	}
	return lead, true
}

// draftPos is where a draft begins on the row grid: the INDEX of the row its
// first rune landed on, and how many RUNES of that row come before it.
//
// RUNES, not cells, because that is the unit the emphasis spans are counted in
// (seg.n, wrapSegs). Mixing the two is a live defect on any wide-character text:
// two CJK runes are four cells, so a cell offset read as a rune offset colours
// twice as many characters as it should — measured while building this, on
// "现在" + "温度是多少".
type draftPos struct {
	row int
	// runesBefore is how many runes of the row come before the draft's first one.
	runesBefore int
}

// promptEmphasis returns the PROMPT's emphasis span of a drawn row: how many of
// the row's leading RUNES came from the prompt — or nothing at all when the
// prompt does not reach this row. It is the prompt HALF of editingRowSegs, which
// adds the buffer's span for everything after those runes.
func (r *Renderer) promptEmphasis(row int, width int) []seg {
	pal := r.palette()
	if pal.Prompt.Empty() || r.prompt == "" {
		return nil
	}
	spans := promptRowSpans(r.promptRunes, width)
	if row >= len(spans) || spans[row] == 0 {
		return nil
	}
	return []seg{{n: spans[row], st: pal.Prompt}}
}

// promptRowSpans reports how many of the prompt's runes land on each row when the
// prompt is wrapped at width — the same wrap layoutText applies to prompt+buffer,
// restricted to the prompt's own prefix of that text.
func promptRowSpans(prompt []rune, width int) []int {
	if width <= 0 {
		width = defaultEditorCols
	}
	var spans []int
	col, n := 0, 0
	for _, r := range prompt {
		w := runeCells(r)
		if col+w > width {
			spans = append(spans, n)
			col, n = 0, 0
		}
		col += w
		n++
	}
	return append(spans, n)
}

// wrapSegs renders a row's leading cells under the styles segs names: the first
// seg's cells take its style, the rest of the row is unstyled. Monochrome
// palettes produce the input unchanged (Style.Wrap returns its text), and an
// empty seg slice is a plain return, so the historic bytes are untouched when
// colour is off.
func wrapSegs(row string, segs []seg) string {
	if len(segs) == 0 || row == "" {
		return row
	}
	var b strings.Builder
	b.Grow(len(row) + sgrOverhead)
	rest := []rune(row)
	for _, sg := range segs {
		n := sg.n
		if n > len(rest) {
			n = len(rest)
		}
		if n <= 0 {
			continue
		}
		b.WriteString(sg.st.Wrap(string(rest[:n])))
		rest = rest[n:]
	}
	b.WriteString(string(rest))
	return b.String()
}

// bufferRestRunes is the RUNE COUNT that means "every rune left on this row":
// the editing row's second span is the user's buffer, whose length is whatever
// follows the prompt — a number the row itself knows, so naming it here would be
// a second definition of the row's width (Rule 13/14). It only has to exceed any
// row's rune count; wrapSegs clamps each span to what is actually left.
const bufferRestRunes = math.MaxInt

// seg is one emphasis span of a row: the next n RUNES are drawn under st.
type seg struct {
	n  int
	st Style
}

// eraseRows blanks count rows starting at startRow, through the region's own
// placement path, so the erasure goes to the same destination the drawing does
// (the region clamps the row to the screen it owns).
func (r *Renderer) eraseRows(b *strings.Builder, reg *region, startRow, count int) {
	for i := 0; i < count; i++ {
		reg.placeInto(b, startRow+i, 0)
		b.WriteString(escEraseLine)
	}
}

// Finish ends the editing line: the cursor is left on the row after the last
// drawn row and the layout is forgotten, so the next Prompt starts clean. The
// drawn text stays in the terminal's scrollback for the user to read.
func (r *Renderer) Finish() {
	if r.out == nil {
		return
	}
	var b strings.Builder
	if down := int(r.drawnRows.Load()) - 1 - r.cursorRows; down > 0 {
		b.WriteString(moveCursor('B', down))
	}
	b.WriteString(escCarriage + escNewline)
	r.cursorRows = 0
	r.drawnRows.Store(0)
	r.placedRow = 0
	// deliberately ignored: as above — a render failure must not abort editing.
	_, _ = io.WriteString(r.out, b.String())
}

// PlaceCaret re-places the terminal's cursor on the editing line's CARET — the
// cell the user is typing into — as ONE absolute sequence (`CSI row;col H`).
//
// WHY IT EXISTS (measured on a real pty, 2026-10-08). Redraw ends by placing the
// caret, but it is not the last writer of the frame: the REGION's band write
// (region.WriteLines → writeBandLine) ends by parking the cursor on the band's
// LIVE row, and it runs AFTER the renderer in the repaint (drainBandFrames) and
// from SettleBand with no repaint at all. So a frame that carried a band row
// left the hardware cursor one row ABOVE the editing line — the operator's "the
// small white block above the input edit" — where it then stayed until the next
// keystroke repainted. Startup is exactly that frame: the startup screen and the
// side panel are committed through that same band write, so the FIRST screen a
// user sees has the block off the input and reads as "the prompt is not
// focused".
//
// The placement is ABSOLUTE and self-positioning (a CUP cannot land anywhere
// else), and it is idempotent: a frame whose caret was already there costs eight
// bytes and moves nothing. It is a no-op without a reserved layout and without a
// drawn editing line — the historic full-screen path owns its own cursor with
// relative moves and has no absolute row to place to, so its bytes are untouched
// (this call writes nothing where `Redraw` returned early, including after
// Finish/Reset forgot the line).
func (r *Renderer) PlaceCaret() {
	if r.out == nil || r.region == nil || !r.region.Active() || r.drawnRows.Load() <= 0 {
		return
	}
	// The placement goes through the region's OWN cursorTo, so the 0-based →
	// 1-based conversion has one definition in this package instead of two
	// (region.go:786; a second `+1` here is how the first version of this fix
	// landed the block one row high — measured).
	//
	// deliberately ignored: as everywhere in this renderer — a failed placement
	// is not state, and the next repaint rewrites it.
	_, _ = io.WriteString(r.out, cursorTo(r.region.EditorTop()+r.cursorRows, r.cursorCol))
}

// Reset forgets the layout (used after the screen was cleared underneath us).
func (r *Renderer) Reset() {
	r.cursorRows = 0
	r.drawnRows.Store(0)
	r.placedRow = 0
	r.unpinWindow()
	r.window = editingWindow{}
}

// layoutText turns prompt+text into the rows a terminal would show at width,
// with the cursor's row and column. cursor is a RUNE index into text (the prompt
// always precedes the cursor). Rows are trimmed of trailing blanks — each row is
// erased before it is drawn, so nothing needs padding.
func layoutText(prompt, text string, cursor, width int) (rows []string, cursorRow, cursorCol int) {
	return layoutTextDraft(prompt, text, "", cursor, width)
}

// layoutTextDraft is layoutText with a DRAFT SEGMENT appended after the buffer
// (draft.go). The draft is laid out as part of the SAME logical text — it follows
// the buffer on the line and wraps with it — which is what makes "where will my
// words land?" answerable while the operator speaks.
//
// THE CARET IS THE BUFFER'S, so the draft cannot move it: it is appended AFTER
// the cursor's rune index in the layout walk, and the walk stops recording the
// cursor's row at the buffer's own position. A caller with no draft passes "",
// and layoutRowsDraft's output for that input is the layout it always produced —
// which is why every existing caller is unaffected (layoutText above is that
// caller's entry point).
func layoutTextDraft(prompt, text, draft string, cursor, width int) (rows []string, cursorRow, cursorCol int) {
	rs, cr, cc, _ := layoutTextDraftPos(prompt, text, draft, cursor, width)
	return rs, cr, cc
}

// layoutTextDraftPos is layoutTextDraft plus WHERE THE DRAFT BEGINS, reported by
// the walk that laid the rows out.
//
// IT IS THE SAME WALK ON PURPOSE (Rule 14). The emphasis pass has to know which
// runes of a drawn row are the draft's, and the only correct answer is the one the
// layout produced — a second derivation (a formula over widths, say) would drift
// the moment either changed, and the drift would be invisible: the text drawn and
// the text coloured would disagree by a cell. So the walk RECORDS the position as
// it passes it, and the position is reported beside the rows rather than
// recomputed from them.
func layoutTextDraftPos(prompt, text, draft string, cursor, width int) (rows []string, cursorRow, cursorCol int, at draftPos) {
	rows, cursorRow, cursorPrefix, at := layoutRowsDraftPos(prompt, text, draft, cursor, width)
	cursorCol = cursorCell(rows[cursorRow], cursorPrefix, width)
	return rows, cursorRow, cursorCol, at
}

// cursorCell is the cursor's cell within its own row, given that row as drawn
// and the cells that precede the cursor ON THAT ROW (the layout loop records
// them, so no substring is re-measured and no escape byte can be mistaken for a
// cell).
//
// The pending-wrap cell matters because the cursor is not a character: it is the
// place the next one lands. On a row that fills every column the terminal's
// cursor sits on the LAST cell holding the pending-wrap flag, and the only
// address that puts it there is that cell — `CSI width C` clamps to it, which is
// the cell a grid records (Screen.CurCol with a cursor-forward past its last
// column; master addressed it at column == width and the grid clamped the report
// back to width-1, which is the disagreement the render-truth test catches).
// The armed wrap is the reason the column is not merely cosmetic: a cell the
// terminal already holds the pending wrap on is one keystroke from a two-row
// advance — the trap the streamed answer's width-only breaks hit (chat_stream.go).
func cursorCell(row, prefix string, width int) int {
	prefixCells := displayWidth(prefix)
	if prefixCells < displayWidth(row) {
		// Mid-row: the cursor sits just past the cells before it.
		return prefixCells
	}
	// The cursor is at the row's end (nothing follows it on this row): a row
	// with room to spare takes the cell just past its text, a FULL row takes
	// its last cell.
	return min(prefixCells, max(width-1, 0))
}

// layoutRows wraps prompt+text into the rows a terminal would show at width and
// reports the row the cursor is on plus the cells preceding the cursor WITHIN
// that row. It is the LAYOUT half of layoutText — where each rune lands does not
// depend on where the cursor is — and it is what RowsFor asks when it needs only
// "how tall is this line".
func layoutRows(prompt, text string, cursor, width int) (rows []string, cursorRow int, cursorPrefix string) {
	return layoutRowsDraft(prompt, text, "", cursor, width)
}

// layoutRowsDraft is layoutRows with the draft segment appended after the
// buffer. The draft's own bytes are neutralised for DRAWING only
// (draftDisplayText): a break inside a draft would move the caret's row
// arithmetic, and the settled text keeps whatever the producer sent (the insert
// road is what handles a newline once the text is the operator's).
func layoutRowsDraft(prompt, text, draft string, cursor, width int) (rows []string, cursorRow int, cursorPrefix string) {
	rs, cr, cp, _ := layoutRowsDraftPos(prompt, text, draft, cursor, width)
	return rs, cr, cp
}

// layoutRowsDraftPos is layoutRowsDraft, recording where the draft's first rune
// lands (row index and RUNE offset within that row) as the walk passes it.
func layoutRowsDraftPos(prompt, text, draft string, cursor, width int) (rows []string, cursorRow int, cursorPrefix string, at draftPos) {
	at = draftPos{row: -1}
	if width <= 0 {
		width = defaultEditorCols
	}
	// THE DRAFT IS ONE LOGICAL LINE OF ITS OWN, glued to the buffer: the
	// combined text the walk below lays out is `buffer + draft`, so the draft
	// starts exactly where the buffer ends (on the same row when there is room,
	// wrapped onward when there is not) — the position the operator will find
	// it in once it settles.
	shownDraft := draftDisplayText(draft)
	textRunes := []rune(text + shownDraft)
	// bufferRunes is where the buffer ends and the draft begins, in the combined
	// rune string the walk below indexes — the marker the loop watches for.
	bufferRunes := len([]rune(text))
	promptRunes := []rune(prompt)
	if cursor > len(textRunes) {
		cursor = len(textRunes)
	}
	if cursor < 0 {
		cursor = 0
	}
	var (
		row       []rune
		col       int
		curRowSet bool
	)
	promptLen := len(promptRunes)
	total := promptLen + len(textRunes)
	for i := 0; i < total; i++ {
		var r rune
		if i < promptLen {
			r = promptRunes[i]
		} else {
			r = textRunes[i-promptLen]
		}
		if i == promptLen+bufferRunes && shownDraft != "" {
			// THE DRAFT'S FIRST RUNE, and the position recorded here is in the
			// unit the emphasis spans are counted in — RUNES within the row, never
			// cells (a cell offset read as a rune offset colours twice as many CJK
			// characters as it should).
			//
			// THE WRAP IS DECIDED HERE because the switch below has not run yet:
			// a first rune that does not fit starts a NEW row, and the position
			// recorded is then that row's beginning. Recording it after the
			// switch instead would read `row` after it had been reset, which is
			// how the two answers drift apart.
			if r != '\n' && col+runeCells(r) > width && len(row) > 0 {
				at = draftPos{row: len(rows) + 1, runesBefore: 0}
			} else {
				at = draftPos{row: len(rows), runesBefore: len(row)}
			}
		}
		if !curRowSet && i == promptLen+cursor {
			cursorRow, curRowSet = len(rows), true
			// The cursor's row prefix is decided by what the rune UNDER the
			// cursor does: mid-row it is the row so far, on a newline it is
			// still this row (the break comes after it), and a wrap moves the
			// cursor to a row that has nothing before it yet.
			cursorPrefix = string(row)
			if r != '\n' && col+runeCells(r) > width {
				cursorPrefix = ""
			}
		}
		switch {
		case r == '\n':
			rows = append(rows, trimRightSpaces(string(row)))
			row, col = nil, 0
		case col+runeCells(r) > width:
			// Wrap before the rune rather than splitting a wide character.
			rows = append(rows, trimRightSpaces(string(row)))
			row, col = []rune{r}, runeCells(r)
		default:
			row = append(row, r)
			col += runeCells(r)
		}
	}
	if !curRowSet {
		cursorRow = len(rows)
		cursorPrefix = string(row)
	}
	rows = append(rows, trimRightSpaces(string(row)))
	return rows, cursorRow, cursorPrefix, at
}

// trimRightSpaces drops a row's trailing blanks (an erased row is already blank
// to the right of what we write).
func trimRightSpaces(s string) string { return strings.TrimRight(s, " ") }

// moveCursor builds a relative cursor-move sequence (CSI n A/B/C/D), composed
// by the one escape vocabulary (internal/sgr) so the direction byte and the
// parameter order have one definition.
func moveCursor(dir byte, n int) string {
	return sgr.MoveCursorSeq(dir, n)
}

// sliceCols returns up to width display columns of s, starting at display column
// start (wide runes count two columns, combining marks none). Unused by the
// row-based renderer but kept for callers that need a column window of an
// arbitrary string.
func sliceCols(s string, start, width int) string {
	if width <= 0 {
		return ""
	}
	var (
		b       strings.Builder
		col     int
		written int
	)
	for _, r := range s {
		w := runeCells(r)
		if col+w <= start {
			col += w
			continue
		}
		if written+w > width {
			break
		}
		b.WriteRune(r)
		written += w
		col += w
	}
	return b.String()
}

// DisplayWidth is a string's width in terminal CELLS (a CJK glyph is two, a
// combining mark zero, a control sequence none). It is the exported face of
// displayWidth, which is the one definition of the measurement: a caller that
// bounds a ROW needs cells, and a second implementation would drift from this
// one the moment either changes (Rule 14).
func DisplayWidth(s string) int { return displayWidth(s) }

// displayWidth is a string's width in terminal CELLS (a CJK glyph is two, a
// combining mark zero, a control sequence none) — the measure every caller that
// bounds a row must use, because len(s) counts a multi-byte rune as its bytes
// and utf8.RuneCount counts a wide glyph as one.
//
// A CONTROL SEQUENCE IS ZERO CELLS, and that is not a detail: the agent's own
// flow output carries SGR (the dim reasoning marker), and a caller that counted
// those bytes as cells would size a row four cells too wide and then hand the
// band a row that does not fit it. It asks sgr.EscapeSeqLen — the ONE definition
// of "which bytes draw no cell" (Rule 14) — so the cutters and the measurers
// cannot disagree.
func displayWidth(s string) int {
	w := 0
	for i := 0; i < len(s); {
		if n := sgr.EscapeSeqLen(s[i:]); n > 0 {
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		w += runeCells(r)
		i += size
	}
	return w
}

// runeCells is a rune's display width: 0 for combining/zero-width marks, 2 for
// East Asian wide and fullwidth characters, 1 otherwise. Without this a CJK
// buffer would put the cursor two cells off per character, which the cell-grid
// assertion would (correctly) catch.
//
// THE THIRD CLASS IS ENVIRONMENT-DEPENDENT, and it is the one this function used
// to get WRONG. A rune whose East Asian Width is AMBIGUOUS (property A) is one
// cell in a Western terminal and two in a CJK one — box-drawing U+2500–257F,
// `—` U+2014, `→` U+2192 and the enclosed numbers are all A in full. The
// behaviour of isWideRune (below) is what decides, so the rule lives in one
// place: `A` is two cells exactly when the environment in force says so, and
// that decision is internal/config's (one definition, Rule 14 — see
// internal/config/ambiguous_width.go for WHY the environment, and not a
// measurement of the font, is what decides).
func runeCells(r rune) int {
	switch {
	case r == 0:
		return 0
	case r == '\n' || r == '\r':
		return 0
	case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r), unicode.Is(unicode.Cf, r):
		return 0
	case isWideRune(r):
		return 2
	default:
		return 1
	}
}

// wideRanges are the East Asian Wide/Fullwidth blocks (Unicode East Asian Width
// property W and F) — the practical set a terminal renders double-width; a rune
// outside the list is one cell. W/F is ENVIRONMENT-INDEPENDENT (always two
// cells, in every locale), which is why it is a local table and the A class is
// not.
//
// THREE OTHER COPIES OF A WIDE-RUNE PREDICATE EXIST IN THIS REPO, and naming
// them here is the point of this comment (Rule 14 — a mirror must at least be
// visible to the next reader):
//
//   - internal/config/width.go IsWideRune — the help-wrap budget's copy. It
//     answers the same W/F question for text that is never edited on a screen,
//     and it is the package the A class was moved INTO (ambiguous_width.go).
//   - internal/text2img/render.go isWideRuneForGrid — the offscreen grid
//     renderer's copy. It stays deliberately A-narrow: its background is
//     white, so an A-rune that a font paints 2 cells wide would be a cosmetic
//     mismatch inside an image, not a cursor in the wrong place.
//   - this one, which the editor's LAYOUT and its cell grid (screenattr.go,
//     screen.go) both read.
//
// Unifying the first two onto this table is a refactor of its own and is
// deliberately NOT part of the A-class fix.
var wideRanges = []struct{ lo, hi rune }{
	{0x1100, 0x115F}, // Hangul Jamo initial consonants
	{0x2E80, 0x303E}, // CJK radicals, Kangxi, CJK symbols/punctuation
	{0x3041, 0x33FF}, // Hiragana, Katakana, Bopomofo, Hangul compat, CJK compat
	{0x3400, 0x4DBF}, // CJK ext A
	{0x4E00, 0x9FFF}, // CJK unified ideographs
	{0xA000, 0xA4CF}, // Yi
	{0xAC00, 0xD7A3}, // Hangul syllables
	{0xF900, 0xFAFF}, // CJK compat ideographs
	{0xFE30, 0xFE6F}, // CJK compat forms, small form variants
	{0xFF00, 0xFF60}, // Fullwidth forms
	{0xFFE0, 0xFFE6}, // Fullwidth signs
	{0x1F300, 0x1F64F},
	{0x1F900, 0x1F9FF},
	{0x20000, 0x2FFFD},
	{0x30000, 0x3FFFD},
}

// isWideRune reports whether r renders two cells wide ON THE ENVIRONMENT GOPI IS
// DRAWING INTO: the W/F table above always, plus the AMBIGUOUS (A) class when
// the environment says such runes are wide.
//
// The A half is a DELEGATION, not a copy: config.IsAmbiguousWide owns both the
// range table and the environment decision (env knob > locale > narrow), so the
// editor, the help-wrap budget and any future caller cannot drift. That is the
// Rule 14 answer to "where does the third table live" — it lives in the ONE
// package all three can import, with exactly one definition.
//
// A caller that must be environment-INDEPENDENT (a golden transcript, the cell
// grid of a recorded dump) pins it with config.AmbiguousWidthEnv=narrow; a test
// that asserts the CJK reading pins it to wide. Neither has to touch a font.
func isWideRune(r rune) bool {
	for _, rg := range wideRanges {
		if r >= rg.lo && r <= rg.hi {
			return true
		}
	}
	return config.IsAmbiguousWide(r)
}
