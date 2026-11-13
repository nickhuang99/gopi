package lineedit

// answer_boundary_splice_test.go — the SECOND attrs/text relation, constructed.
//
// The guard in answer_placement_test.go injects a read-loop repaint at ONE
// boundary: the write immediately after a bare placement. That is the window
// shape (a) was measured on, and it is enough to catch a placement that does not
// ride its text.
//
// The SECOND relation the chat-dump flake produced is not that window's
// signature. Measured (session 01a0deee, frozen artifact):
//
//	008|▌ the                 follows   attrs 008|0:40=default
//
// — the row's ATTRS are all default while its TEXT loses a contiguous run
// (`bold conclusion `, the span's own cells). Two things follow, and both are
// read off that ROW, never off a comparison of one block of the artifact against
// another:
//
//   - a frame assembled from two FINISHED frames cannot look like this: a
//     finished frame carries the bold cells, so a splice of two frames carries
//     SOME emphasis somewhere, not none;
//   - a partially applied SGR cannot either: the lost cells carry NO style at
//     all, which is only possible when the bytes that would have painted them —
//     the SGR and the characters TOGETHER — never reached that row.
//
// That is the signature of a SPAN PIECE's write landing on another row. A piece's
// bytes are ONE write of the form `open, text, reset` (Style.Wrap), and the
// renderer's own stream shows it: `ESC[10;7H ESC[1m bold conclusion ESC[0m`. A
// piece lost WHOLE leaves the band row with NEITHER its characters NOR its
// attributes — which is exactly the row quoted above. The loss needs a window
// between the write that POSITIONS the piece and the write that carries it: the
// same seam as shape (a), one boundary later.
//
// WHAT THESE TESTS DO. The invariant test splices a repaint in after EVERY write
// and asserts the BAND is unchanged — a repaint only ever rewrites the rows the
// read loop owns, so no interleaving of them can alter a single cell of an answer
// row, text or attribute. The second test CONSTRUCTS the pre-fix emission shape
// from production's own bytes (a placed write split back into `place` + `text` —
// byte for byte what the pre-fix renderer emitted, see b7574284's diff: two calls,
// `r.out.place(...)` then `r.write(...)`) and shows that shape reproducing the
// artifact's row, while the SAME bytes as production emits them today do not. The
// only difference between those two runs is whether the placement rides the text,
// which is what makes it a statement about the fix and nothing else.
//
// The interleaving is CONSTRUCTED, never sampled: at the sampled rate the race
// needs ~1 hit per 360 iterations, and a reduced flake rate proves nothing.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The renderer's two emission primitives whose NAME is compared against the AST
// in TestEveryRendererEmissionIsSelfPositioning. They are constants rather than
// literals at the comparison (Rule 1): the names have a single definition site,
// region.go's method set, and a rename there must fail that guard by name rather
// than silently stop matching.
const (
	emissionBarePlace = "place"
	emissionBareEmit  = "emitTo"
)

// spanSpliceLine is shape (b)'s own content: a plain prefix, a bold span, a plain
// tail — the three-piece row the frozen artifact held. The markers are the
// tokenizer's, so the row is drawn as `the bold conclusion follows` with the
// span's cells bold.
const (
	spanSplicePlain = "the "
	spanSpliceSpan  = "bold conclusion "
	spanSpliceTail  = "follows"
	spanSpliceLine  = spanSplicePlain + BoldMarker + spanSpliceSpan + BoldMarker + spanSpliceTail
	// spanSpliceDrawn is what the row SHOWS (markers consumed). The bold run's
	// cell range inside the DRAWN text is derived rather than typed: the prefix's
	// width is the column the span starts at and the span's length is how many
	// cells must be bold.
	spanSpliceDrawn     = spanSplicePlain + spanSpliceSpan + spanSpliceTail
	spanSpliceSpanCol   = len(spanSplicePlain)
	spanSpliceSpanCells = len(spanSpliceSpan)
	// spanSpliceBlankRun is the damaged row's shape in one piece: the span's
	// cells, blank. It is what a lost piece leaves behind.
	spanSpliceBlankRun = " "
)

// attrBoldCell / attrPlainCell spell a bold mask as characters, so a failure can
// print the mask beside the text: a boolean slice prints as `[false true …]` and
// hides the RUN structure, which is the evidence this defect is about.
const (
	attrBoldCell  = '#'
	attrPlainCell = '.'
)

// boundarySplicer splices a read-loop repaint into the stream at a chosen write
// boundary and nothing else: `after` is the 0-based index of the write it follows
// (negative = after EVERY write, the maximal interleaving). It records nothing
// itself — the repaint writes through the SAME wrapper, so the recorder behind it
// holds one ordered stream containing both writers' bytes, exactly as the
// terminal saw them.
//
// It re-enters safely: the repaint's own writes are not spliced (a repaint
// interrupting a repaint is not an interleaving the read loop can produce —
// there is one loop).
type boundarySplicer struct {
	inner   *recordingWriter
	repaint func()
	// splitPlacements models the PRE-FIX renderer's emission shape: a write that
	// both positions and draws was emitted there as TWO calls, `place` then
	// `write`, so its bytes arrived as two writes. The bytes are production's own
	// — only the chunk boundary differs.
	splitPlacements bool
	after           int
	// injected counts the repaints actually spliced in, so a test can refuse to
	// pass vacuously.
	injected int
	// seen counts the layout's own writes through this wrapper (a split writes
	// twice, and the repaint's writes do not count).
	seen  int
	inSpl bool
}

func (w *boundarySplicer) Write(p []byte) (int, error) {
	if w.splitPlacements {
		if head := placementSeqLen(p); head > 0 && head < len(p) {
			if _, err := w.Write(p[:head]); err != nil {
				return 0, err
			}
			if _, err := w.Write(p[head:]); err != nil {
				return 0, err
			}
			return len(p), nil
		}
	}
	n, err := w.inner.Write(p)
	if err != nil {
		return n, err
	}
	if w.inSpl || len(p) == 0 {
		return n, nil
	}
	idx := w.seen
	w.seen++
	if w.after >= 0 && idx != w.after {
		return n, nil
	}
	w.inSpl = true
	if w.repaint != nil {
		w.repaint()
	}
	w.injected++
	w.inSpl = false
	return n, nil
}

// spanSpliceCase is one harness construction: whether the read loop's repaints are
// spliced in, at which boundary, and whether the renderer's emission shape is the
// pre-fix one.
type spanSpliceCase struct {
	name            string
	splice          bool
	after           int
	splitPlacements bool
}

// spanSpliceHarness builds the A5 layout with a splicing writer in front of the
// recorder. The colour-capable palette is installed explicitly: the emphasis
// reaches the wire only when the palette renders one, and a monochrome palette
// would make the bold half of this guard vacuous.
func spanSpliceHarness(t *testing.T, c spanSpliceCase) (*a5Harness, *boundarySplicer, *recordingWriter) {
	t.Helper()
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	src := &scriptedSource{width: a5WideCols, height: a5Rows, clock: clock}
	rec := &recordingWriter{}
	spl := &boundarySplicer{inner: rec, after: c.after, splitPlacements: c.splitPlacements}
	ed := NewChatEditor(src, spl, promptForTest, nil, nil, nil)
	ed.SetClock(clock.Now)
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true", a5Rows)
	}
	ed.SetPalette(BasicPalette)
	if c.splice {
		spl.repaint = func() { ed.repaint() }
	}
	h := &a5Harness{ed: ed, out: rec, screen: NewScreen(a5WideCols, a5Rows), region: ed.region, answer: ed.Answer(), clock: clock, screenCols: a5WideCols, screenRows: a5Rows}
	h.answer.SetCols(a5WideCols)
	return h, spl, rec
}

// paintSpanTurn drives shape (b)'s content through the harness and replays every
// recorded write into the screen — the SAME bytes a real run's sink is fed, in the
// order the terminal received them. The break is INCLUDED: emphasis is decided
// when the line's break arrives, so the pieces the row is painted from are real by
// the time they are written (the measured case — the tap recorded a DECIDED span).
func paintSpanTurn(t *testing.T, c spanSpliceCase) (*a5Harness, *boundarySplicer, *recordingWriter) {
	t.Helper()
	h, spl, rec := spanSpliceHarness(t, c)
	h.answer.Begin()
	h.answer.BeginTurn(TurnInfo{Index: 1, Prompt: promptForTest})
	h.clock.advance(answerFlushWindow)
	h.answer.Delta(spanSpliceLine + "\n")
	h.replay()
	return h, spl, rec
}

// bandSignature renders every row ABOVE the editing line as `text|bold-mask`, so
// two frames can be compared on both halves of the pair at once — the relation the
// artifact got wrong. The editor's own rows are outside the band (the read loop
// owns them and is not part of the claim), and so is the status row. The mask's
// trailing plain cells are trimmed: the padding is not a run.
func bandSignature(h *a5Harness) []string {
	top := h.region.Plan().EditorTop
	out := make([]string, 0, top)
	for row := 0; row < top; row++ {
		line, styles := h.screen.StyledLine(row)
		var mask strings.Builder
		for col := 0; col < h.screenCols; col++ {
			if col < len(styles) && styles[col].Bold {
				mask.WriteByte(attrBoldCell)
				continue
			}
			mask.WriteByte(attrPlainCell)
		}
		out = append(out, strings.TrimRight(line, " ")+"|"+strings.TrimRight(mask.String(), string(attrPlainCell)))
	}
	return out
}

// bandReport renders the band plus the write stream for a failure message. The
// writes are quoted with the stdlib's own quoting (Rule 14 — never a hand-rolled
// escape table), so a control byte is visible rather than moving the terminal the
// report is printed on.
func bandReport(h *a5Harness, rec *recordingWriter) string {
	var b strings.Builder
	b.WriteString("--- band (row text|row bold mask) ---\n")
	b.WriteString(strings.Join(bandSignature(h), "\n"))
	b.WriteString("\n--- the paint stream, in write order ---\n")
	for i, w := range rec.snapshot() {
		b.WriteString("write[")
		b.WriteString(strconv.Itoa(i))
		b.WriteString("] = ")
		b.WriteString(strconv.Quote(string(w)))
		b.WriteString("\n")
	}
	return b.String()
}

// turnBandRow is the band row the turn's answer was painted on: the one carrying
// the turn's gutter glyph. The band's OTHER rows are the turn header and padding.
func turnBandRow(t *testing.T, h *a5Harness) int {
	t.Helper()
	gutter := h.answer.lineGutter()
	top := h.region.Plan().EditorTop
	for row := 0; row < top; row++ {
		if gutter != "" && strings.HasPrefix(h.screen.Line(row), gutter) {
			return row
		}
	}
	t.Fatalf("no band row opens with the turn's gutter %q: this turn's row is not on the grid\n%s",
		gutter, strings.Join(bandSignature(h), "\n"))
	return -1
}

// requireSpanOnTheTurnRow asserts the relation the artifact must satisfy: the
// drawn text is on the turn's band row, contiguously, and the span's own cells are
// bold. `label` names the construction, so a failure says WHICH emission shape
// broke it. It returns the row and the drawn text's rune offset inside it (the
// gutter sits to its left, and a column IS a rune index — the grid's own
// contract).
func requireSpanOnTheTurnRow(t *testing.T, h *a5Harness, rec *recordingWriter, label string) (int, int) {
	t.Helper()
	row := turnBandRow(t, h)
	text := h.screen.Line(row)
	off := indexRunes([]rune(text), spanSpliceDrawn)
	if off < 0 {
		t.Fatalf("%s: the turn's band row %d = %q does not carry %q — the row's text lost a contiguous run\n%s",
			label, row, text, spanSpliceDrawn, bandReport(h, rec))
	}
	_, styles := h.screen.StyledLine(row)
	for c := spanSpliceSpanCol; c < spanSpliceSpanCol+spanSpliceSpanCells; c++ {
		if off+c >= len(styles) || !styles[off+c].Bold {
			t.Fatalf("%s: row %d = %q has no bold cell at text column %d (row column %d) — the span's attributes never reached this row\n%s",
				label, row, text, c, off+c, bandReport(h, rec))
		}
	}
	return row, off
}

// TestSpanPieceReachesItsBandRowUnderEveryInterleaving is the guard for shape (b):
// the band must be IDENTICAL with and without a repaint spliced after every write.
// The read loop only ever rewrites its own rows, so no interleaving of its
// repaints can change an answer cell — text or attribute.
func TestSpanPieceReachesItsBandRowUnderEveryInterleaving(t *testing.T) {
	clean, _, cleanRec := paintSpanTurn(t, spanSpliceCase{name: "uninterrupted", after: -1})
	spliced, spl, splicedRec := paintSpanTurn(t, spanSpliceCase{name: "spliced", splice: true, after: -1})
	if spl.injected == 0 {
		t.Fatal("the splicing writer injected nothing: the assertion below would be vacuous")
	}

	cleanRow, _ := requireSpanOnTheTurnRow(t, clean, cleanRec, "uninterrupted")
	t.Logf("uninterrupted band row %d: %s", cleanRow, bandSignature(clean)[cleanRow])
	requireSpanOnTheTurnRow(t, spliced, splicedRec, "repaint after every write")

	if a, b := strings.Join(bandSignature(clean), "\n"), strings.Join(bandSignature(spliced), "\n"); a != b {
		t.Fatalf("the answer band is not invariant under the read loop's repaints — a piece of the turn's row went to a row the repaint owns\n%s",
			bandReport(spliced, splicedRec))
	}
	t.Logf("repaints spliced in: %d; the band is unchanged (%d band rows compared cell by cell)",
		spl.injected, len(bandSignature(clean)))
}

// TestSpanPieceSplitFromItsPlacementIsTheArtifactRow is the discriminator, and it
// runs the SAME bytes twice. The only difference is the emission SHAPE at ONE
// boundary:
//
//	pre-fix (split)  — the write that carries the span arrives as two: the
//	                   placement alone, then the span's bytes. The read loop's
//	                   repaint lands between them and the span goes to the editing
//	                   line's row, where the next repaint erases it — so the band
//	                   row keeps a BLANK run where the span belongs and an
//	                   all-default attrs row beside it. That is the frozen
//	                   artifact's row, and it is asserted here POSITIVELY: the
//	                   prefix, the blank run of exactly the span's cells, the tail,
//	                   and not one bold cell on the row.
//	post-fix (whole) — the placement rides the span's bytes in ONE write, executed
//	                   by b7574284's placeAndWrite; there is no boundary to land
//	                   between, and the row is intact.
//
// The bytes are IDENTICAL in both runs (they are production's own, quoted in the
// failures); only the chunk boundary differs, which is exactly the property the
// landed fix changed.
func TestSpanPieceSplitFromItsPlacementIsTheArtifactRow(t *testing.T) {
	splitCase := spanSpliceCase{name: "pre-fix emission", splice: true, splitPlacements: true}

	// Pass 1 — the pre-fix emission shape with NO repaint spliced in: it locates
	// the boundary to splice, on the stream itself rather than by assuming an
	// index. The pre-fix renderer emitted the placement as its own write, so the
	// write that is EXACTLY a placement and is followed by the span's bytes is the
	// seam; it is the same pair b7574284's diff replaced with one placeAndWrite.
	_, _, dry := paintSpanTurn(t, spanSpliceCase{name: "dry run", after: -1, splitPlacements: true})
	dryWrites := dry.snapshot()
	seam := -1
	for i := 0; i+1 < len(dryWrites); i++ {
		w := dryWrites[i]
		if len(w) == 0 || placementSeqLen(w) != len(w) {
			continue
		}
		if strings.Contains(string(dryWrites[i+1]), spanSpliceSpan) {
			seam = i
			break
		}
	}
	if seam < 0 {
		t.Fatalf("the pre-fix emission shape offers no placement-only write before the span's bytes: there is no boundary to splice, and this guard would be vacuous\n%s",
			bandReport(&a5Harness{screen: NewScreen(a5WideCols, a5Rows), region: &region{}}, dry))
	}
	t.Logf("the seam the pre-fix renderer left open: write[%d] = %s then write[%d] = %s",
		seam, strconv.Quote(string(dryWrites[seam])), seam+1, strconv.Quote(string(dryWrites[seam+1])))

	// Pass 2 — the same emission shape, with the read loop's repaint landing
	// exactly in that seam and nowhere else: the artifact's own mechanism.
	splitCase.after = seam
	pre, spl, preRec := paintSpanTurn(t, splitCase)
	if spl.injected != 1 {
		t.Fatalf("the splicer injected %d repaint(s), want exactly 1: the seam is the subject of this test", spl.injected)
	}
	row := turnBandRow(t, pre)
	text := pre.screen.Line(row)
	rowRunes := []rune(text)
	off := indexRunes(rowRunes, spanSplicePlain)
	if off < 0 {
		t.Fatalf("the damaged row %d = %q does not even carry the plain prefix: this is not the artifact's shape\n%s",
			row, text, bandReport(pre, preRec))
	}
	want := spanSplicePlain + strings.Repeat(spanSpliceBlankRun, spanSpliceSpanCells) + spanSpliceTail
	if got := string(rowRunes[off:]); !strings.HasPrefix(got, want) {
		t.Fatalf("the damaged row %d = %q does not read as the artifact's row (%q): the span was not lost as a contiguous run\n%s",
			row, got, want, bandReport(pre, preRec))
	}
	_, styles := pre.screen.StyledLine(row)
	for c := 0; c < len(styles); c++ {
		if styles[c].Bold {
			t.Fatalf("the damaged row %d = %q carries a bold cell at column %d: the artifact's row is ALL-DEFAULT, and a construction that leaves any emphasis is a different defect\n%s",
				row, text, c, bandReport(pre, preRec))
		}
	}
	t.Logf("pre-fix emission shape reproduced the artifact's row:\n  text  %d|%q\n  attrs %d|0:%d=default  (not one bold cell; the span's %d cells blank)",
		row, text, row, len([]rune(text)), spanSpliceSpanCells)

	// Pass 3 — the SAME bytes, as production emits them today, under the SAME
	// mechanism at the boundary immediately before the span: the placement rides
	// the span, so the repaint has nothing to land between. The production stream
	// has fewer writes (no split), so the boundary is located on ITS own stream —
	// the same relation ("the write before the span's bytes"), never the same
	// absolute index.
	_, _, wholeDry := paintSpanTurn(t, spanSpliceCase{name: "dry run", after: -1})
	wholeSeam := -1
	for i, w := range wholeDry.snapshot() {
		if strings.Contains(string(w), spanSpliceSpan) {
			wholeSeam = i - 1
			break
		}
	}
	if wholeSeam < 0 {
		t.Fatalf("no write carries the span in the production stream: the post-fix half cannot be constructed\n%s",
			bandReport(&a5Harness{screen: NewScreen(a5WideCols, a5Rows), region: &region{}}, wholeDry))
	}
	whole, spl2, wholeRec := paintSpanTurn(t, spanSpliceCase{name: "production emission", splice: true, after: wholeSeam})
	if spl2.injected != 1 {
		t.Fatalf("the splicer injected %d repaint(s), want exactly 1: the post-fix half must be the same single seam", spl2.injected)
	}
	postRow, _ := requireSpanOnTheTurnRow(t, whole, wholeRec, "production emission under the same repaint")
	t.Logf("production emission under the same single repaint at that boundary: band row %d = %q|%s (intact)",
		postRow, strings.TrimRight(whole.screen.Line(postRow), " "), strings.TrimRight(bandSignature(whole)[postRow], string(attrPlainCell)))
}

// TestEveryRendererEmissionIsSelfPositioning is the MECHANICAL half of the same
// guard, and it is what keeps this seam from reopening at the NEXT call site.
//
// answer.go's renderer shares its byte stream with the editor's read loop, which
// repaints the editing line between any two write(2) calls. A call to the
// region's bare `place` (or its primitive `emitTo`) therefore leaves a window at
// that LINE by construction, whatever a behavioural test happens to cover: the
// span piece lost in shape (b) went that way, and so did shape (a)'s.
//
// The rule is a property of the tree, so it is asserted against the tree: an
// emission that positions the cursor must be composed with the bytes it
// positions (`placeAndWrite` / `placeAndWritePrefixed` / `emitAt` /
// `ControlAt`), never issued on its own. The tree is PARSED rather than
// grep'd (a pattern over text cannot tell a call from a mention in a comment),
// and the receiver is checked, so a parser's own cursor bookkeeping — which does
// not share the renderer's stream — is not swept in by name.
func TestEveryRendererEmissionIsSelfPositioning(t *testing.T) {
	const (
		emissionSeamGlob   = "*.go"
		emissionTestSuff   = "_test.go"
		emissionRegionRecv = ".out"
	)
	blocked := map[string]bool{
		emissionBarePlace: true, // region.place: writes the cursor and nothing else
		emissionBareEmit:  true, // region.emitTo: the same, one layer down
	}
	files, err := filepath.Glob(emissionSeamGlob)
	if err != nil {
		t.Fatalf("list the package's sources: %v", err)
	}
	scanned := 0
	var hits []string
	for _, name := range files {
		if strings.HasSuffix(name, emissionTestSuff) {
			continue
		}
		blob, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s up to the seam: %v", name, err)
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, blob, 0)
		if err != nil {
			t.Fatalf("parse %s up to the seam: %v", name, err)
		}
		scanned++
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !blocked[sel.Sel.Name] {
				return true
			}
			var recv strings.Builder
			if err := printer.Fprint(&recv, fset, sel.X); err != nil {
				return true
			}
			if !strings.HasSuffix(strings.TrimSpace(recv.String()), emissionRegionRecv) {
				return true
			}
			pos := fset.Position(call.Pos())
			hits = append(hits, fmt.Sprintf("%s:%d  %s.%s(...)", pos.Filename, pos.Line, recv.String(), sel.Sel.Name))
			return true
		})
	}
	if scanned == 0 {
		t.Fatal("no production source was parsed: the assertion below would be vacuous")
	}
	if len(hits) != 0 {
		t.Fatalf("%d emission(s) position the cursor on their own — the read loop's repaint lands between such a call and the write it positions, and those bytes go to whatever row the repaint left the cursor on (shape (a) and shape (b) are both this seam)\n%s\ncompose them instead: placeAndWrite / placeAndWritePrefixed / emitAt / ControlAt",
			len(hits), strings.Join(hits, "\n"))
	}
	t.Logf("%d production file(s) parsed: every cursor placement in the package rides the bytes it positions", scanned)
}

// TestShapeBIsAOneWriteWindow proves the seam POSITIVELY, the way the landed fix
// was decided: it takes production's OWN bytes for one span piece — the write
// that carries it, quoted in the failure — and asks whether that write positions
// the span before drawing it. A write that does is immune to any repaint; a write
// that does not is exactly shape (b)'s row (a blank run where the span belongs,
// with a placement-only write before it).
//
// The "pre-fix" half is the same bytes with the placement SPLIT OFF as its own
// write, which is byte-for-byte what the pre-fix renderer emitted (b7574284's
// diff: `r.out.place(...)` then `r.write(...)`).
func TestShapeBIsAOneWriteWindow(t *testing.T) {
	_, _, rec := paintSpanTurn(t, spanSpliceCase{name: "production bytes", after: -1})
	spanIdx := -1
	for i, w := range rec.snapshot() {
		if strings.Contains(string(w), spanSpliceSpan) {
			spanIdx = i
			break
		}
	}
	if spanIdx < 0 {
		t.Fatalf("no production write carries the span %q", spanSpliceSpan)
	}
	piece := rec.snapshot()[spanIdx]
	if unplacedText(piece) {
		t.Fatalf("the write carrying the span draws cells before naming a cell — it is a window by construction: %s",
			strconv.Quote(string(piece)))
	}
	t.Logf("production's own span write: %s — it names its cell first, so no repaint can land between the two",
		strconv.Quote(string(piece)))

	// The pre-fix shape of the SAME bytes: the placement as its own write.
	head := placementSeqLen(piece)
	if head == 0 || head == len(piece) {
		t.Fatalf("the span's write %s does not lead with a placement, so the pre-fix split cannot be built from it",
			strconv.Quote(string(piece)))
	}
	placeOnly, textOnly := piece[:head], piece[head:]
	if !unplacedText(textOnly) {
		t.Fatalf("the text half %s is not the unplaced fragment the pre-fix pair wrote", strconv.Quote(string(textOnly)))
	}
	t.Logf("pre-fix emission shape for those bytes: %s then %s — a bare placement, then the span",
		strconv.Quote(string(placeOnly)), strconv.Quote(string(textOnly)))
	t.Logf("artifact's row from that shape: a blank run of %d cells where the span belongs, attrs all default",
		spanSpliceSpanCells)
}
