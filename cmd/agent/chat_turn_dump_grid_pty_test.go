package main

// chat_turn_dump_grid_pty_test.go — the AUTONOMOUS re-verification of the T1/T2
// answer-band render defects: one real 117x26 pty, the REAL chat layout and the
// REAL `--dump-screen` sink, a whole turn driven with NO keyboard input.
//
// WHY THIS FILE EXISTS (the defect that made it impossible; measured 2026-09-27,
// task 01a0e433). The handoff doc
// `devdoc/handoff-20260927-chat-render-and-infobar-defects.md` §0 tells the next
// agent to re-verify T1/T2 by reading its OWN screen:
//
//	gopi chat --dump-screen        # arm at launch
//	gopi ui_state --target chat    # read your own painted grid
//
// That method was NOT executable without an operator, because the artifact is
// written only when the paint stream CHANGES and a chat REPL's last paint for a
// turn IS the answer:
//
//   - no nudge → the artifact stays on a pre-answer frame. Measured on an armed
//     117x26 run: `frames=35 dumps=28 … turn 0 · ready`, the answer absent from
//     the band, byte-identical over 40 samples spread across 10s.
//   - nudge (type `x`, then DEL, to force a repaint) → the answer appears, but
//     the sample can catch the repaint MID-PAINT, and the artifact then carries
//     literal escape-sequence cells as text (`…1449-150m`, `…1451-1524;104H`).
//     THE CELLS ARE REAL; THE ATTRIBUTION IS NOT (corrected 2026-09-28, task
//     01a0e81b). A frame boundary is where the defect is REACHED, not what
//     produces it — the interpreter reads every escape of every frame. What put
//     the parameters on the grid is the carry policy in screen.go (holdPending):
//     a held sequence past its bound was truncated to its LAST bytes, dropping
//     the introducer and re-offering the parameters as printable characters.
//     Repro without a pty: internal/lineedit/screen_introducer_lost_test.go.
//
// So an agent could observe the very screen it had just watched draw — and could
// not read it. This case closes that gap: it drives the turn to its end, calls
// the SAME settle the REPL calls at the turn boundary
// (`chatInput.SettleDump` → `uiclient.ScreenDumper.Settle`), and asserts on the
// artifact's own grid — band plus scrollback — with no input after the submitted
// line.
//
// WHAT IS ASSERTED, and why each leg is a criterion rather than a snapshot:
//
//	carries    — the artifact's band+scrollback carries the WHOLE answer, in
//	             order, with every painted run spelled the way the renderer's own
//	             tokenizer spells it. This is the leg that is RED on the pre-fix
//	             tree (the file holds the pre-answer frame).
//	no-dup     — T1: no run of turnDumpRepeatRunCells cells of the reading occurs
//	             twice (task 01a0e2f5's partial repeat).
//	no-literal — T2: no literal emphasis marker (`**`) survives into a cell, and
//	             the backticks that DO appear are balanced per row.
//	style      — T2: every token of the emphasized WRAPPING line is painted BOLD,
//	             on whichever row the wrap put it — the style break the
//	             provisional-paint commit produced.
//	no-fragment— the artifact carries no SGR parameter run as literal cells: the
//	             signature of a capture taken while a cursor placement was still
//	             on the wire (what the typing workaround measured).
//
// The reading is band+SCROLLBACK, never the band alone: the band's whole
// mechanism is that a committed line LEAVES it (region.commitScroll emits one LF
// inside the scroll region), so a band-only reading is a window on a moving
// boundary whose width is a function of cadence — the instrument gap
// internal/lineedit/answer_wrap_scroll_dup_test.go records at length.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/sgr"
	"github.com/nick/gopi/internal/uiclient"
)

// The pty this case runs on. Both numbers are named because the case is ABOUT
// them: 117 columns is the width the handoff doc's live runs measured, and it is
// narrow enough that the fixture must WRAP (so T1/T2's wrap path is exercised);
// 26 rows is the height the reserved layout wants and the height of the grid.
const (
	turnDumpPtyCols = 117
	turnDumpPtyRows = 26
)

// turnDumpTokenGap separates the fixture's tokens. It is a space, so the
// renderer's own wrap policy breaks BETWEEN tokens whenever it can — a fixture
// whose wrap column fell mid-token would test the wrapper instead of the band.
const turnDumpTokenGap = " "

// turnDumpCutLabel reports whether a grid cell holds the band's RIGHT-EDGE CUT
// LABEL rather than a character of the answer.
//
// The label is a rendering glyph, not a character of the answer: the band's row
// cutter spends one of the row's cells on it and displaces the character that
// held that cell onto the row below (see answer.go's cutLabelFor and
// devdoc/answer-row-cut-vocabulary.md). A walk that compares the band against the
// answer's own TEXT therefore meets a rune the source does not contain, at the
// row a wrap landed in the middle of a WORD — exactly this fixture's shape (34
// `emphNN` tokens, whose run the wrap splits). Dropping it is what keeps the
// comparison about the answer rather than about the glyph that says the row was
// shortened; the cells on either side are still asserted, and the CELLS the walk
// keeps keep their own grid columns, so a style read is unaffected.
//
// The glyph comes from the one place that renders it (lineedit.AnswerCutLabel —
// the BAND's continuation mark, `↵`; it was `lineedit.StatusEllipsis` until task
// 01a1a325 separated the band's cut from the status row's elision), never a second
// spelling of the rune (Rule 14).
func turnDumpCutLabel(r rune) bool { return string(r) == lineedit.AnswerCutLabel }

// turnDumpBoldTokens is the token count of the emphasized line: wide enough to
// wrap at 117 columns after the turn's gutter, so the emphasis spans the wrap.
const turnDumpBoldTokens = 34

// turnDumpBodyLines is how many plain lines follow the emphasized one: more than
// the band's own row budget, so the emphasized line scrolls OFF the band and
// into the scrollback — the reading then covers what a reader of the artifact
// actually has, rather than a window that happened to be wide enough.
const turnDumpBodyLines = 40

// turnDumpTailSentence closes the answer. It is the LAST thing painted, so a
// frame frozen before the turn's tail cannot carry it — which is what makes the
// `carries` leg the pre-fix RED leg.
const turnDumpTailSentence = "the answer ends here"

// turnDumpPromptLine is the line that opened the turn. One short word, so
// nothing of the ANSWER can be confused with what the operator typed (the dump
// gate's own measured confusion: `you> + added **d**`).
const turnDumpPromptLine = "go"

// turnDumpRepeatRunCells is how long a run of cells must be before finding it
// twice counts as duplication. It is the SAME threshold the package guard for
// this defect uses (internal/lineedit/answer_wrap_scroll_dup_test.go's
// dupRepeatRunCells, which is test-only and therefore not importable): the two
// cases must agree about what "a repeated run" means, so the value is stated
// here with its twin named rather than left implicit.
const turnDumpRepeatRunCells = 10

// turnDumpEscapeFragmentMinDigits is how many digits must precede a CSI final
// byte before a row is called a literal escape fragment (`…1449-150m`,
// `…1524;104H`). Two is enough to exclude ordinary prose with a number followed
// by `m` or `H`.
const turnDumpEscapeFragmentMinDigits = 2

// The fixture's two token families. Disjoint, so no run of cells can be a piece
// of both.
const (
	turnDumpBoldPrefix  = "emph"
	turnDumpPlainPrefix = "plain"
)

// turnDumpToken is the fixture's token shape: "%02d<family>" — the INDEX comes
// FIRST, and the reason is the T1 leg. A token spelled "<family>%02d" shares its
// whole prefix with nine of its siblings (`plain01`..`plain09` all begin
// `plain0`), so ten consecutive cells such as `…plain0` occur several times in a
// legitimate answer — and the duplication detector then reports the FIXTURE.
// With the index first the varying pair lands inside every ten-cell window, so a
// repeated run can only mean the renderer wrote something twice.
func turnDumpToken(prefix string, i int) string {
	return string([]byte{byte('0' + (i/10)%10), byte('0' + i%10)}) + prefix
}

// turnDumpAnswer is the fixture answer: one emphasized line that must wrap, then
// enough plain lines to push it into the scrollback, then the tail sentence.
//
// The markers are REAL answer text (the model emits `**`), and they are what the
// T2 leg is about: a renderer that commits a provisional paint onto a committed
// row prints the markers literally, so the emphasis shows up as TEXT instead of
// as style.
func turnDumpAnswer() string {
	var b strings.Builder
	// The plain body FIRST: it is what scrolls the band, and the artifact is read
	// as band+scrollback, so the body is where the reading leg and T1 look.
	for i := 1; i <= turnDumpBodyLines; i++ {
		b.WriteString(turnDumpToken(turnDumpPlainPrefix, i))
		b.WriteString("\n")
	}
	// The emphasized WRAPPING line next, and LAST but for the tail: a line that
	// has scrolled into the scrollback would leave the grid, and the grid is what
	// holds the per-cell STYLES (the scrollback is text, because a background
	// style on a line that has left the screen has nowhere to be observed).
	b.WriteString(lineedit.BoldMarker)
	for i := 1; i <= turnDumpBoldTokens; i++ {
		if i > 1 {
			b.WriteString(turnDumpTokenGap)
		}
		b.WriteString(turnDumpToken(turnDumpBoldPrefix, i))
	}
	b.WriteString(lineedit.BoldMarker)
	b.WriteString("\n")
	b.WriteString(turnDumpTailSentence)
	b.WriteString("\n")
	return b.String()
}

// turnDumpDeltas splits the answer into streamed fragments, one per source line,
// which is the shape the agent's OnText delivers (`delta` per round-trip chunk).
// The split is at a line boundary: a case that cut a rune in half would assert
// the renderer's partial-rune buffer instead of its wrap carry.
func turnDumpDeltas(answer string) []string {
	parts := strings.SplitAfter(answer, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// turnDumpRequiredText is what the reading must carry, IN ORDER: the tail
// sentence (the last thing painted, so a pre-answer freeze cannot hold it) plus
// the first and last body lines (so a reading carrying only a fragment is not
// accepted).
func turnDumpRequiredText() []string {
	return []string{
		turnDumpToken(turnDumpPlainPrefix, 1),
		turnDumpToken(turnDumpPlainPrefix, turnDumpBodyLines),
		turnDumpToken(turnDumpBoldPrefix, 1),
		turnDumpTailSentence,
	}
}

// turnDumpStripCells removes escape sequences from a grid row, using the ONE
// definition of "which bytes draw no cell" (sgr.EscapeSeqLen). It is the reading
// step that makes a painted row comparable with the tokenizer's own spelling: a
// row that still carried a sequence would otherwise read as cells the answer
// never had.
func turnDumpStripCells(row string) string {
	var b strings.Builder
	for i := 0; i < len(row); {
		if n := sgr.EscapeSeqLen(row[i:]); n > 0 {
			i += n
			continue
		}
		b.WriteByte(row[i])
		i++
	}
	return b.String()
}

// turnDumpReading is one artifact read as the cells a reader sees: the
// scrollback first, then the grid — the order a terminal shows them in, and the
// order the package's own guard reads them in.
type turnDumpReading struct {
	rows     []string
	bandRows int
}

// cellsOf joins the reading into one cell string, with a single space where each
// row break was. The join is narrow on purpose: a wrap moves text between rows,
// and the reading must not depend on where the wrap landed.
func (r turnDumpReading) cellsOf() string {
	return strings.Join(strings.Fields(strings.Join(r.rows, " ")), " ")
}

// turnDumpRead turns a parsed artifact into the reading.
func turnDumpRead(cand uiclient.Candidate) turnDumpReading {
	rows := make([]string, 0, len(cand.Dump.Scrollback)+len(cand.Dump.Grid))
	for _, line := range cand.Dump.Scrollback {
		rows = append(rows, strings.TrimRight(turnDumpStripCells(line), " "))
	}
	for _, row := range cand.Dump.Grid {
		rows = append(rows, strings.TrimRight(turnDumpStripCells(row), " "))
	}
	return turnDumpReading{rows: rows, bandRows: len(cand.Dump.Grid)}
}

// TestChatTurnDump_ReadsTheWholeAnswerOnARealPtyWithoutInput is the case the
// epic asks for: a real armed chat surface on a real 117x26 pty, one whole turn
// driven with NO keyboard input after the line that opened it, and the T1/T2
// assertions run against the artifact's own cell grid.
func TestChatTurnDump_ReadsTheWholeAnswerOnARealPtyWithoutInput(t *testing.T) {
	pair := chatOpenPtyPair(t, turnDumpPtyCols, turnDumpPtyRows)
	if got := lineedit.TerminalCols(int(pair.Slave.Fd())); got != turnDumpPtyCols {
		t.Fatalf("the pty reports %d columns, want %d — the kernel did not take the size", got, turnDumpPtyCols)
	}

	sink := turnDumpSink(t)
	ed, err := lineedit.Open(pair.Slave, sink)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}
	defer func() { _ = ed.Close() }() //stylegate:ignore the harness's own terminal teardown

	// The REAL chat layout, installed at the pty's own width (the editor asks the
	// descriptor, so the geometry is the kernel's answer rather than a constant).
	chat := lineedit.NewChatEditor(ed, sink, chatPrompt, nil, nil, nil)
	chat.SetGeometryObserver(chatGeomObserver(sink))
	chat.SetPalette(lineedit.BasicPalette)
	if !chat.EnableRegion() {
		t.Fatalf("EnableRegion(pty %dx%d) = false: the case needs the reserved layout installed", turnDumpPtyCols, turnDumpPtyRows)
	}
	cols, rows, ok := chat.LayoutGeometry()
	if !ok || cols != turnDumpPtyCols || rows != turnDumpPtyRows {
		t.Fatalf("the layout installed at %dx%d (ok=%v), want the pty's %dx%d", cols, rows, ok, turnDumpPtyCols, turnDumpPtyRows)
	}
	// The sink's grid is the LAYOUT's geometry, and it says so: a grid built from
	// any other number would read the layout's own wraps as truncation.
	if geom, gcols, grows := sink.Geometry(); gcols != turnDumpPtyCols || grows != turnDumpPtyRows {
		t.Fatalf("the dump grid is %dx%d (geometry=%s), want the pty's %dx%d", gcols, grows, geom, turnDumpPtyCols, turnDumpPtyRows)
	}

	// The REPL's own input layer over the REAL editor — this is what makes the
	// case the production shape: *chatInput is the object the turn loop drives,
	// so its SettleDump is the call the turn loop makes.
	in := &chatInput{editor: chat, dumper: sink}
	if in.Answer() == nil {
		t.Fatal("the input layer reports no answer band although the layout is installed")
	}

	// THE TURN, driven the way cli_chat.go drives one: the turn renderer over the
	// REPL's input layer, its chrome opened, the stream fed, its tail committed,
	// then the flow seam flushed and the dump SETTLED — and NO input after this
	// point. Every write below reaches the sink synchronously, on this goroutine,
	// which is the same goroutine the REPL's turn runs on.
	renderer := newChatTurnRenderer(in)
	renderer.BeginTurn(1, turnDumpPromptLine)
	for _, delta := range turnDumpDeltas(turnDumpAnswer()) {
		renderer.Write(delta)
	}
	renderer.Finish()
	renderer.EndTurn()
	newChatFlowSeam(in, nil).Flush()
	in.SettleDump()

	// The artifact is the deliverable, so every assertion below reads the FILE.
	cand, err := uiclient.ReadDump(sink.Path())
	if err != nil {
		t.Fatalf("read the artifact back (%s): %v", sink.Path(), err)
	}
	read := turnDumpRead(cand)
	got := read.cellsOf()
	if got == "" {
		t.Fatalf("the artifact (%s) carries no cells at all (frames=%d dumps=%d): the reading is empty, so nothing below could be asserted",
			sink.Path(), cand.Frames, cand.Dumps)
	}

	// ── carries: the pre-fix RED leg ────────────────────────────────────────
	//
	// The artifact must carry the WHOLE answer, in order. Pre-fix this fails by
	// name: the file holds the frame the throttle last wrote BEFORE the answer,
	// and no further paint ever arrives to correct it, because the run is only
	// being observed.
	for _, want := range turnDumpRequiredText() {
		if !strings.Contains(got, want) {
			t.Fatalf("the dump's band+scrollback does not carry %q.\nThe artifact is settled at the turn boundary; before that it was written on a paint-stream CHANGE only, so an OBSERVED run froze on a frame before the turn's tail — this is the defect the case exists for.\nreading (%d rows, %d band rows):\n%s",
				want, len(read.rows), read.bandRows, got)
		}
	}

	// ── T1: no repeated run of cells ────────────────────────────────────────
	if run, found := turnDumpRepeatedRun(got); found {
		t.Fatalf("T1: the reading repeats the %d-cell run %q — the answer band duplicated part of a wrapping answer (task 01a0e2f5)", turnDumpRepeatRunCells, run)
	}

	// ── T2: no literal emphasis marker, balanced backticks ──────────────────
	if row, found := turnDumpLiteralMarker(read.rows); found {
		t.Fatalf("T2: a painted row carries the literal emphasis marker %s: %q — the renderer commits the DRAWING, never the marker (task 01a0e2f3)", lineedit.BoldMarker, row)
	}
	if row, found := turnDumpUnbalancedBacktick(read.rows); found {
		t.Fatalf("T2: a painted row carries an unbalanced backtick (the emphasis commit's stray marker): %q", row)
	}

	// ── the mid-paint capture's own signature must be gone ─────────────────
	if row, found := turnDumpEscapeFragment(read.rows); found {
		t.Fatalf("the artifact carries SGR parameters as literal CELLS: %q — that is the signature of a frame captured mid-paint, with a cursor placement still on the wire (the typing workaround measured `…1449-150m`, `…1451-1524;104H`)", row)
	}

	// ── the band scrolls: the mechanism the assertions rest on ─────────────
	//
	// A reading can only be about a SCROLLED band if the band really scrolled.
	// This is asserted rather than assumed because the pre-fix defect made it
	// false, and a case that silently stopped scrolling would go on passing
	// while testing something else entirely: the sink's grid must still have the
	// layout's scrolling region installed (not the whole screen), and the answer
	// must have left rows behind in the scrollback.
	if top, bottom := cand.Dump.ScrollRegion.Top, cand.Dump.ScrollRegion.Bottom; bottom >= turnDumpPtyRows-1 {
		t.Fatalf("the sink's grid holds the scrolling region %d-%d, which spans the whole %d-row screen: the layout's own `CSI 1;%dr` never reached it, so the band did not scroll and this reading is not the scrolled band the case is about",
			top, bottom, turnDumpPtyRows, turnDumpPtyRows-2)
	}
	if len(cand.Dump.Scrollback) == 0 {
		t.Fatalf("the sink's grid saved no scrollback: a %d-line answer cannot fit the band, so an empty history means the answer never scrolled through it", turnDumpBodyLines+2)
	}

	// ── T2: style continuity across the wrap ────────────────────────────────
	turnDumpAssertBoldSpansTheWrap(t, cand)
}

// turnDumpSink arms the REAL sink at a fresh path. It is built with no geometry
// of its own (0,0 → the model default) EXACTLY as production builds it, so the
// only thing that can give it the pty's width is the layout's own install — the
// regression cmd/agent/chat_dump_geometry_pty_test.go exists for.
func turnDumpSink(t *testing.T) *uiclient.ScreenDumper {
	t.Helper()
	d, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path: turnDumpPath(t),
		Identity: uiclient.DumpIdentity{
			Surface:          uiclient.SurfaceChat,
			PID:              0,
			Target:           chatDumpTarget(false),
			TerminalColsRows: [2]int{0, 0},
			Started:          uiDumpStartStamp(),
		},
	})
	if err != nil {
		t.Fatalf("arm the dump sink: %v", err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Errorf("close the dump sink: %v", err)
		}
	})
	return d
}

// turnDumpPath is where the case's artifact lives. It is the sink's OWN
// filename under the test's scratch directory, so `gopi ui_state`'s discovery
// contract (the file's NAME selects the surface) is what the reader sees too.
func turnDumpPath(t *testing.T) string {
	t.Helper()
	return t.TempDir() + "/" + uiclient.ChatDumpFileName
}

// turnDumpRepeatedRun reports the first run of turnDumpRepeatRunCells cells that
// occurs twice in the reading — T1 stated as a property of the cells.
//
// A run of ONE repeated character is skipped: a rule line (`──────`, the turn
// separator and the band's own chrome) is legitimately identical everywhere on
// the screen, and a detector that fired on it would report the layout's
// furniture instead of a duplicated answer. Duplication of an ANSWER is always a
// run of mixed text, so requiring two distinct characters keeps the leg about
// what it names.
func turnDumpRepeatedRun(cellsOf string) (string, bool) {
	cells := []rune(cellsOf)
	n := turnDumpRepeatRunCells
	if len(cells) < 2*n {
		return "", false
	}
	seen := make(map[string]bool, len(cells))
	for i := 0; i+n <= len(cells); i++ {
		run := string(cells[i : i+n])
		if strings.TrimSpace(run) == "" || turnDumpOneRepeatedRune(cells[i:i+n]) {
			continue
		}
		if seen[run] {
			return run, true
		}
		seen[run] = true
	}
	return "", false
}

// turnDumpOneRepeatedRune reports whether a run is one character repeated with
// single spaces interleaved — the shape of a horizontal rule.
func turnDumpOneRepeatedRune(run []rune) bool {
	var seen rune
	for _, r := range run {
		if r == ' ' {
			continue
		}
		if seen == 0 {
			seen = r
			continue
		}
		if r != seen {
			return false
		}
	}
	return true
}

// turnDumpLiteralMarker reports the first row carrying a literal emphasis
// marker. A painted cell NEVER carries one: the renderer's composer strips the
// marker when it decides the span, so a marker in the GRID means a provisional
// paint was committed as literal text — T2's own symptom.
func turnDumpLiteralMarker(rows []string) (string, bool) {
	for _, row := range rows {
		if strings.Contains(row, lineedit.BoldMarker) {
			return row, true
		}
	}
	return "", false
}

// turnDumpUnbalancedBacktick reports the first row with an ODD backtick count —
// the stray/unbalanced marker T2 produces when the emphasis commit glues the
// provisional paint onto a committed row.
func turnDumpUnbalancedBacktick(rows []string) (string, bool) {
	for _, row := range rows {
		if strings.Count(row, "`")%2 == 1 {
			return row, true
		}
	}
	return "", false
}

// turnDumpEscapeFragment reports the first row carrying SGR parameters painted
// as TEXT (`…49-150m`, `…1524;104H`). The detector needs digits AND a CSI final
// byte, so ordinary prose with a number cannot trip it.
func turnDumpEscapeFragment(rows []string) (string, bool) {
	for _, row := range rows {
		if turnDumpHasEscapeShape(row) {
			return row, true
		}
	}
	return "", false
}

// turnDumpHasEscapeShape is the fragment detector itself.
func turnDumpHasEscapeShape(row string) bool {
	digits := 0
	for _, r := range row {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case (r == 'm' || r == 'H') && digits >= turnDumpEscapeFragmentMinDigits:
			return true
		case r == ';' && digits > 0:
			// a parameter separator: the run continues through it
		default:
			digits = 0
		}
	}
	return false
}

// turnDumpAssertBoldSpansTheWrap asserts T2's style-continuity leg: every
// character of the emphasized SOURCE line is painted BOLD on whatever row the
// wrap put it.
//
// The reading it walks is the band's WHITESPACE-FREE cell stream — every
// non-space cell of the grid, in reading order, each one carrying its own
// (row, col). That is the only reading in which a wrap is invisible: the layout
// puts a gutter (`▌ `) at the start of a band row and TWO spaces at the start of
// a continuation row, and the wrap may fall in the MIDDLE of a token (measured
// on this fixture: `…16emph 17e` / `mph 18emph…`). Dropping whitespace makes the
// emphasized run contiguous again, so the assertion can be about STYLE rather
// than about where the wrapper decided to break.
func turnDumpAssertBoldSpansTheWrap(t *testing.T, cand uiclient.Candidate) {
	t.Helper()
	source := turnDumpEmphasizedSource(t)
	spanChars := strings.Join(strings.Fields(source.text), "")

	stream := turnDumpCellStream(cand)
	at := turnDumpIndexOfChars(stream.chars(), spanChars)
	if at < 0 {
		t.Fatalf("the emphasized source line %q is not painted on the band as a contiguous run of cells (band chars: %q)",
			spanChars, stream.chars())
	}
	cells := stream.cells[at : at+len([]rune(spanChars))]

	// The wrap must really split the span, or the case is not about the wrap.
	rows := map[int]bool{}
	for _, c := range cells {
		rows[c.row] = true
	}
	if len(rows) < 2 {
		t.Fatalf("the emphasized line's cells are painted on %d band row(s): at %d columns this line must wrap, so the fixture no longer exercises style continuity across the wrap",
			len(rows), turnDumpPtyCols)
	}

	// Continuity: EVERY cell of the span is bold, INCLUDING the ones the wrap
	// moved to a continuation row. THAT is the failure T2 produced — the tail of
	// a wrapped span lost its style while the provisional paint stayed behind.
	word := boldFieldWord(t)
	for _, c := range cells {
		style := turnDumpStyleAt(cand, c.row, c.col)
		if style == "" {
			t.Fatalf("the grid's attr runs do not cover row %d col %d (%q): the artifact's style map has a hole where the emphasis is", c.row, c.col, c.r)
		}
		if !strings.Contains(style, word) {
			t.Fatalf("the emphasized cell %q at row %d col %d is painted WITHOUT %s (style %q): the emphasis did not survive the wrap — T2's style break",
				c.r, c.row, c.col, word, style)
		}
	}
}

// turnDumpEmphasizedSource is the fixture's emphasized line and the character
// range the markers delimit, read with the renderer's OWN tokenizer — so the
// span the case asserts on is the span the renderer decided, not a second
// derivation.
type turnDumpSourceSpan struct {
	text string
}

func turnDumpEmphasizedSource(t *testing.T) turnDumpSourceSpan {
	t.Helper()
	for _, line := range strings.Split(strings.TrimRight(turnDumpAnswer(), "\n"), "\n") {
		if !strings.Contains(line, lineedit.BoldMarker) {
			continue
		}
		spans := lineedit.LineSpans(line)
		var b strings.Builder
		for _, span := range spans {
			if span.Emphasis == lineedit.EmphasisBold {
				b.WriteString(span.Text)
			}
		}
		if b.Len() == 0 {
			t.Fatalf("the emphasized line %q parsed to no emphasized text", line)
		}
		return turnDumpSourceSpan{text: b.String()}
	}
	t.Fatalf("the fixture answer carries no emphasis marker: %q", turnDumpAnswer())
	return turnDumpSourceSpan{}
}

// turnDumpCell is one painted cell of the band: its rune and where it lives.
type turnDumpCell struct {
	r        rune
	row, col int
}

// turnDumpStream is the band as a whitespace-free cell stream.
type turnDumpStream struct{ cells []turnDumpCell }

func (s turnDumpStream) chars() string {
	var b strings.Builder
	for _, c := range s.cells {
		b.WriteRune(c.r)
	}
	return b.String()
}

// turnDumpCellStream flattens the artifact's grid into its non-space cells, in
// reading order. A row is walked as RUNES so a column is a cell, not a byte.
//
// The band's cut label is dropped HERE rather than in chars(), because chars() is
// the flattened text this walk INDEXES while cells is the slice it indexes into:
// a drop applied to one and not the other would slice by an offset the other was
// not counted with. It is the only rune this walk drops, and it drops it for the
// reason turnDumpCutLabel states.
func turnDumpCellStream(cand uiclient.Candidate) turnDumpStream {
	var out turnDumpStream
	for row, line := range cand.Dump.Grid {
		for col, r := range []rune(line) {
			if r == ' ' || r == 0 || turnDumpCutLabel(r) {
				continue
			}
			out.cells = append(out.cells, turnDumpCell{r: r, row: row, col: col})
		}
	}
	return out
}

// turnDumpIndexOfChars is strings.Index over RUNES: the position where want
// occurs in haystack, in characters.
func turnDumpIndexOfChars(haystack, want string) int {
	h := []rune(haystack)
	w := []rune(want)
	if len(w) == 0 || len(w) > len(h) {
		return -1
	}
	for i := 0; i+len(w) <= len(h); i++ {
		if string(h[i:i+len(w)]) == want {
			return i
		}
	}
	return -1
}

// turnDumpPlacement is one located token: where the grid paints it.

// turnDumpFindToken returns the (row, col) of a token's FIRST cell in the
// artifact's grid, searching only the band (the emphasis leg is about a row the
// grid still holds).

// turnDumpStyleAt is the style the artifact's own attr runs report for one cell
// ("" when no run covers it — a hole in the map, which is its own defect).
func turnDumpStyleAt(cand uiclient.Candidate, row, col int) string {
	runs := cand.Dump.AttrRuns
	if row < 0 || row >= len(runs) {
		return ""
	}
	for _, run := range runs[row] {
		if col >= run.Col && col < run.Col+run.Cells {
			return run.Style
		}
	}
	return ""
}
