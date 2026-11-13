package lineedit

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/sgr"
	"github.com/nick/gopi/internal/testutil"
)

// palette_test.go — the acceptance for the colour/emphasis layer.
//
// The four gates, in the order the epic states them:
//
//  1. Byte-exact in BOTH directions: with NO_COLOR=1, TERM=dumb, --no-color or a
//     non-TTY sink the capture contains ZERO SGR bytes; on a TTY the same run
//     contains them. The assertion is on the BYTES, never on a flag: "the layer
//     is off" is a claim about what reached the terminal.
//  2. No information from colour alone: the monochrome capture is the same
//     characters as the coloured one, so a reader with no colour loses emphasis
//     and nothing else.
//  3. The surfaces are emphasised where the epic names them (banner, prompt,
//     status label vs live hint, tool row, answer body, code/diff, warning,
//     error) — asserted on the composed stream, with the attribute spans read
//     back from the bytes under test rather than from the (separate) attribute
//     grid instrument.
//  4. Sizing is untouched: the same run under both capabilities produces the
//     same COLUMN arithmetic (the cell-content check), so an SGR byte can never
//     be counted as a display cell.

// paletteTestCols/paletteTestRows are the geometry the palette tests draw at.
const (
	paletteTestCols = 40
	paletteTestRows = 10
)

// promptForPalette is the editing line's prompt in these tests: a short prompt
// whose emphasis is easy to spot in the byte stream.
const promptForPalette = "you> "

// paletteCapture is one rendered surface: what was written, in order.
type paletteCapture struct {
	out *recordingWriter
}

func newPaletteCapture() *paletteCapture { return &paletteCapture{out: &recordingWriter{}} }

// bytes is the whole capture, concatenated.
func (c *paletteCapture) bytes() string {
	var b strings.Builder
	for _, chunk := range c.out.snapshot() {
		b.Write(chunk)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// 1. The capability decision itself: one place, every input.
// ---------------------------------------------------------------------------

// TestColourWantedDecisionMatrix pins the ONE capability decision's precedence.
// Each row is an independent way for colour to be off; the last row is the only
// one that turns it on.
func TestColourWantedDecisionMatrix(t *testing.T) {
	// A pty is what "the sink is a terminal" means. Without /dev/ptmx on this
	// host the TTY-arm rows cannot be exercised, and the test SKIPS those rows
	// rather than asserting a weakened version of them.
	master := paletteOpenPty(t)
	tty := paletteSlaveFile(t, master)
	//stylegate:ignore closes the pty master the fixture opened
	defer func() { _ = master.Close() }()

	cases := []struct {
		name       string
		noColor    bool
		noColorEnv bool
		term       string
		sink       *os.File
		want       ColourMode
	}{
		{name: "flag disables on a TTY", noColor: true, sink: tty, want: ColourOff},
		{name: "NO_COLOR env disables on a TTY", noColorEnv: true, sink: tty, want: ColourOff},
		{name: "TERM=dumb disables on a TTY", term: TermDumb, sink: tty, want: ColourOff},
		{name: "a non-tty sink disables", sink: nil, want: ColourOff},
		{name: "a TTY with no objection enables", term: "xterm-256color", sink: tty, want: ColourOn},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// NO_COLOR's PRESENCE is the signal, so absence must be explicit:
			// an unset variable and an empty one are different inputs, and
			// t.Setenv cannot express the ABSENT case (it always leaves the
			// variable present). This is the reviewed os.Setenv/restore pair.
			if tc.noColorEnv {
				// env-leak-ok: t.Setenv cannot express ABSENCE — the very case
				// under test (NO_COLOR="" disables, unset does not). The restore
				// is testutil.SnapshotEnv, which captures with os.LookupEnv so
				// the variable comes back ABSENT rather than empty.
				restore := testutil.SnapshotEnv(t, EnvNoColor)
				defer restore()
				// env-leak-ok: as above — the empty value is the input under test.
				//stylegate:ignore child-process env handoff the child reads at exec time, where a scoped t.Setenv would be erased before the child runs; the restore is testutil.SnapshotEnv, which brings the variable back ABSENT rather than empty
				if err := os.Setenv(EnvNoColor, ""); err != nil {
					t.Fatalf("set %s: %v", EnvNoColor, err)
				}
			} else {
				// env-leak-ok: as above — the ABSENT value is the input under test.
				restore := testutil.SnapshotEnv(t, EnvNoColor)
				defer restore()
				// env-leak-ok: as above — see testutil.SnapshotEnv for the restore.
				if err := os.Unsetenv(EnvNoColor); err != nil {
					t.Fatalf("unset %s: %v", EnvNoColor, err)
				}
			}
			t.Setenv(EnvTerm, tc.term)
			if got := ColourWanted(tc.noColor, tc.sink); got != tc.want {
				t.Fatalf("ColourWanted(noColor=%v, NO_COLOR set=%v, TERM=%q, tty=%v) = %v, want %v",
					tc.noColor, tc.noColorEnv, tc.term, tc.sink != nil, got, tc.want)
			}
			// The table follows the mode in the SAME direction, and the check
			// is on the emitted BYTES rather than on the struct: "colour is off"
			// is a claim about the wire, not about which variable was chosen.
			table := PaletteFor(ColourWanted(tc.noColor, tc.sink))
			emits := hasSGR(table.Banner.Wrap("x") + table.Error.Wrap("y"))
			if emits != (tc.want == ColourOn) {
				t.Fatalf("PaletteFor(mode=%v) emits SGR = %v, want %v", tc.want, emits, tc.want == ColourOn)
			}
		})
	}
}

// TestMonoPaletteEmitsNothing proves the zero table is really zero: not a reset,
// not an empty sequence — no byte at all, for every field.
func TestMonoPaletteEmitsNothing(t *testing.T) {
	text := "answer body"
	for name, st := range map[string]Style{
		"banner": MonoPalette.Banner, "prompt": MonoPalette.Prompt,
		"statusLabel": MonoPalette.StatusLabel, "statusLive": MonoPalette.StatusLive,
		"thinking": MonoPalette.Thinking, "toolRow": MonoPalette.ToolRow,
		"toolCall": MonoPalette.ToolCall,
		"answer":   MonoPalette.Answer, "code": MonoPalette.Code,
		"diffAdd": MonoPalette.DiffAdd, "diffDel": MonoPalette.DiffDel,
		"hint": MonoPalette.Hint, "warning": MonoPalette.Warning, "error": MonoPalette.Error,
	} {
		if got := st.Wrap(text); got != text {
			t.Fatalf("MonoPalette.%s.Wrap(%q) = %q, want the text unchanged", name, text, got)
		}
		if st.Open() != "" || st.Close() != "" {
			t.Fatalf("MonoPalette.%s opened/closed (%q/%q), want empty", name, st.Open(), st.Close())
		}
	}
	if hasSGR(MonoPalette.Banner.Wrap("x") + MonoPalette.Error.Wrap("y")) {
		t.Fatal("the monochrome table emitted an SGR sequence")
	}
}

// TestBasicPaletteCoversEverySurface asserts the named table actually names every
// field the epic requires: a field left at the zero style would silently render
// that surface plain even with colour on (and no other test would notice, because
// a plain field is indistinguishable from a correctly-monochrome one).
func TestBasicPaletteCoversEverySurface(t *testing.T) {
	fields := map[string]Style{
		"banner": BasicPalette.Banner, "prompt": BasicPalette.Prompt,
		"buffer":      BasicPalette.Buffer,
		"statusLabel": BasicPalette.StatusLabel, "statusLive": BasicPalette.StatusLive,
		"thinking": BasicPalette.Thinking, "toolRow": BasicPalette.ToolRow,
		"toolCall": BasicPalette.ToolCall,
		"code":     BasicPalette.Code, "diffAdd": BasicPalette.DiffAdd,
		"diffDel": BasicPalette.DiffDel, "hint": BasicPalette.Hint,
		"warning": BasicPalette.Warning, "error": BasicPalette.Error,
	}
	for name, st := range fields {
		if st.Empty() {
			t.Errorf("BasicPalette.%s is empty — the surface would render plain with colour on", name)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. The editing line: prompt emphasis, buffer untouched, byte-exact off.
// ---------------------------------------------------------------------------

// paletteEditor builds an editor + region at the palette tests' geometry, with
// the given palette installed the way production installs it.
func paletteEditor(t *testing.T, pal Palette) (*Editor, *paletteCapture) {
	t.Helper()
	cap := newPaletteCapture()
	reg := newRegion(cap.out)
	if !reg.Enable(paletteTestRows, paletteTestCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want true", paletteTestRows, paletteTestCols)
	}
	ed := &Editor{
		val:    NewValue(0, 0),
		now:    time.Now,
		dec:    NewDecoder(),
		menu:   newCompletionState(nil),
		region: reg,
	}
	ed.render = NewRenderer(cap.out)
	ed.render.Prompt(promptForPalette)
	ed.render.SetRegion(reg)
	ed.answer = NewAnswerRenderer(reg, nil)
	ed.answer.SetCols(reg.Cols())
	ed.SetPalette(pal)
	return ed, cap
}

// TestEditorPromptAndBufferAreDifferentlyStyled asserts the two halves of the
// editing line separately: the prompt carries the prompt style, and the user's
// own text carries the BUFFER style.
//
// IT RE-DERIVES TestEditorPromptIsEmphasisedAndBufferIsNot, WHICH C1
// OVERRULED — that test asserted the buffer was NEVER styled ("their input is
// theirs"), and the operator reversed the ruling: the input must take the
// system's gold. Its positive half survives unchanged (the prompt IS emphasised,
// and not by the buffer's style); its negative half is turned around and made
// STRONGER: the buffer is not merely "not the prompt's style", it is the
// palette's own buffer style — asserted on the RENDERED CELLS below, where the
// old test could only assert the absence of a substring.
func TestEditorPromptAndBufferAreDifferentlyStyled(t *testing.T) {
	ed, cap := paletteEditor(t, BasicPalette)
	ed.val.SetText("hello")
	ed.render.Redraw(ed.val, paletteTestCols)

	got := cap.bytes()
	wantPrompt := BasicPalette.Prompt.Wrap(promptForPalette)
	if !strings.Contains(got, wantPrompt) {
		t.Fatalf("capture does not contain the emphasised prompt %q:\n%q", wantPrompt, got)
	}
	// The buffer is styled — BY ITS OWN STYLE, and never by the prompt's: the
	// two are distinct fields, and a renderer that reached for the prompt's
	// style for the rest of the row would be visible here.
	wantBuffer := BasicPalette.Buffer.Wrap("hello")
	if !strings.Contains(got, wantBuffer) {
		t.Fatalf("the buffer is not drawn under the palette's buffer style %q — the input is back on the terminal's default colour:\n%q", wantBuffer, got)
	}
	if strings.Contains(got, BasicPalette.Prompt.Wrap("hello")) {
		t.Fatalf("the BUFFER was emphasised by the PROMPT's style:\n%q", got)
	}
	// ...and the two fields are not one decision wearing two names.
	if BasicPalette.Buffer.Open() == BasicPalette.Prompt.Open() {
		t.Error("the buffer and the prompt share one sequence — the editing line's two halves are indistinguishable")
	}
	// The styling may not move a cell: the drawn text is still the prompt
	// followed by exactly the buffer.
	if !strings.Contains(stripSGR(got), promptForPalette+"hello") {
		t.Fatalf("the drawn text is not the prompt followed by the buffer:\n%q", stripSGR(got))
	}
}

// TestEditorBufferGoldIsOnTheGrid is the epic's acceptance on the CELL GRID: the
// real Redraw bytes are replayed through the vt100 Screen, and the editing row's
// BUFFER cells are read back as attributes. The claim being checked is not "we
// emitted some SGR bytes" but "the cells after the prompt are painted with the
// table's gold and are NOT the terminal's default" — which is the defect exactly
// as the operator's screen showed it (`0:5=fg=colour-186 bold 5:112=default`).
//
// The row is located by its TEXT (the prompt + the typed buffer), so the
// assertion cannot be satisfied by painting gold on a row where the editing line
// is not.
func TestEditorBufferGoldIsOnTheGrid(t *testing.T) {
	const typed = "hello"
	for _, tc := range editingRowCases() {
		t.Run(tc.name, func(t *testing.T) {
			ed, cap := paletteEditor(t, tc.pal)
			ed.val.SetText(typed)
			ed.render.Redraw(ed.val, paletteTestCols)

			sc := renderScreen(t, []byte(cap.bytes()), paletteTestCols, paletteTestRows)
			row, col := gridRowAndColOf(t, sc, promptForPalette+typed)
			bufferCol := col + displayWidth(promptForPalette)

			// 1. THE DEFECT ITSELF: no cell of the buffer is the default
			// foreground any more. Checked on EVERY buffer cell, not one sample,
			// because the reported screen carried the gold prompt and the
			// default-coloured input ON THE SAME ROW.
			for i := 0; i < displayWidth(typed); i++ {
				cell := sc.Attr(row, bufferCol+i)
				if cell.Fg == sgr.DefaultColor && cell.FgRGB == sgr.NoRGB {
					t.Fatalf("%s: buffer cell %d (row %d col %d) carries the terminal's DEFAULT foreground — this is the reported defect (screen truth: `5:112=default`):\n%s",
						tc.name, i, row, bufferCol+i, sc.Dump().Text())
				}
			}

			// 2. ...and it carries THIS TABLE's gold, in the spelling the tier
			// can pin: a 24-bit value where the table has one, a palette index
			// otherwise. `want` is READ BACK FROM THE TABLE's own Buffer
			// sequence (declaredStyle), so the assertion cannot agree with
			// itself about a colour the table does not paint.
			want := declaredStyle(t, tc.pal.Buffer, tc.name+" Buffer")
			if want.Fg == sgr.DefaultColor && want.FgRGB == sgr.NoRGB {
				t.Fatalf("%s: the table's Buffer field declares no colour at all (%q) — an unstyled buffer is the defect this test exists for", tc.name, tc.pal.Buffer.Open())
			}
			if got := sc.Attr(row, bufferCol); !got.Equal(want) {
				t.Errorf("%s: the first buffer cell carries %+v, want the table's own Buffer style %+v (%q) — the input is not painted with this table's gold",
					tc.name, got, want, tc.pal.Buffer.Open())
			}

			// 3. The PROMPT cell is still the prompt's own style — the change
			// must not have flattened the two halves into one.
			promptWant := declaredStyle(t, tc.pal.Prompt, tc.name+" Prompt")
			if prompt := sc.Attr(row, col); !prompt.Equal(promptWant) {
				t.Errorf("%s: the prompt cell carries %+v, want the table's Prompt style %+v", tc.name, prompt, promptWant)
			}
			if sc.Attr(row, col).Equal(want) {
				t.Errorf("%s: the prompt cell and the buffer cell carry the same style — the two halves of the editing line are indistinguishable", tc.name)
			}

			// The evidence artifact: the raw bytes as emitted plus the grid dump
			// the assertions read, the same shape the status-row capture writes.
			if dir := paletteCaptureDir(); dir != "" {
				raw := []byte(cap.bytes())
				if err := os.WriteFile(filepath.Join(dir, "editing-row-"+tc.name+".ansi"), raw, 0o644); err != nil {
					t.Fatalf("write raw capture: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "editing-row-grid-"+tc.name+".txt"), []byte(sc.Dump().Text()), 0o644); err != nil {
					t.Fatalf("write grid dump: %v", err)
				}
			}
		})
	}
}

// editingRowCase is one tier's case: the table, and nothing else. The colour the
// assertion expects is READ BACK FROM THE TABLE's own Buffer sequence
// (declaredStyle below), so no case can pass by naming a colour the table does
// not paint — and no colour is typed into this file (Rule 13/14: the table is the
// definition, reference it).
type editingRowCase struct {
	name string
	pal  Palette
}

// editingRowCases are the four tiers the epic names.
func editingRowCases() []editingRowCase {
	return []editingRowCase{
		{name: "basic", pal: BasicPalette},
		{name: "palette256", pal: Palette256},
		{name: "hermes-dark", pal: HermesDarkPalette},
		{name: "hermes-light", pal: HermesLightPalette},
	}
}

// declaredStyle reads back the sgr.Style a table field's own sequence declares:
// every SGR sequence the field carries is parsed and applied by the package that
// owns SGR parsing, so the answer is what a terminal would record for those bytes
// — not what the test believes the field contains. A field that opens several
// sequences (the tables compose attributes in front of the colour) accumulates
// them, exactly as a terminal does.
//
// It scans with statusRowColourSequences — the ONE definition of "the sequences
// in a byte string" this package already has (palette_status_salience_test.go) —
// rather than re-splitting on the introducer (Rule 14: one definition).
func declaredStyle(t *testing.T, st Style, what string) sgr.Style {
	t.Helper()
	// sgr.Default(), NOT the zero Style: a zero sgr.Style is not "unpainted" —
	// its BgRGB/FgRGB read 0, which the grid renders as the colour #000000, so a
	// field that names only a foreground would have been compared against a
	// string that claims a black background nobody painted.
	out := sgr.Default()
	for _, seq := range statusRowColourSequences(st.Open()) {
		var params []int
		for _, f := range strings.Split(seq, string(sgr.ParamSeparator)) {
			if f == "" {
				continue
			}
			n, err := strconv.Atoi(f)
			if err != nil {
				t.Fatalf("%s = %q carries parameter %q, which is not a number", what, st.Open(), f)
			}
			params = append(params, n)
		}
		for i := 0; i < len(params); i++ {
			i = sgr.ApplyParam(&out, params, i)
		}
	}
	return out
}

// gridRowAndColOf returns the grid row and column at which text begins, or fails
// when the text is not on the grid at all (a renderer that drew nothing must not
// be able to pass an absence-of-default check by drawing nowhere).
func gridRowAndColOf(t *testing.T, sc *Screen, text string) (int, int) {
	t.Helper()
	for row := 0; row < sc.Rows; row++ {
		if col := strings.Index(sc.Line(row), text); col >= 0 {
			return row, displayWidth(sc.Line(row)[:col])
		}
	}
	t.Fatalf("%q is not on the grid:\n%s", text, sc.Dump().Text())
	return 0, 0
}

// TestEditorPromptEmphasisIsStableAcrossRedraws is the anti-double-emphasis
// guard: the prompt's style must be opened EXACTLY ONCE per redraw no matter how
// many times the line is repainted, so the emphasis cannot accumulate a pair of
// sequences per keystroke.
func TestEditorPromptEmphasisIsStableAcrossRedraws(t *testing.T) {
	ed, cap := paletteEditor(t, BasicPalette)
	for i := 0; i < 5; i++ {
		ed.val.InsertString("x")
		ed.render.Redraw(ed.val, paletteTestCols)
	}
	if n := strings.Count(cap.bytes(), BasicPalette.Prompt.Open()); n != 5 {
		t.Fatalf("prompt opened %d times over 5 redraws, want exactly 1 per redraw:\n%q", n, cap.bytes())
	}
}

// TestEditorWithColourOffEmitsNoSGR is the byte-exact half on the editing line:
// the whole redraw contains ZERO SGR, and it is the SAME characters as the
// coloured run (stripSGR of the coloured capture).
func TestEditorWithColourOffEmitsNoSGR(t *testing.T) {
	for _, tc := range []struct {
		name string
		pal  Palette
	}{
		{name: "MonoPalette"},
		{name: "PaletteFor(ColourOff)", pal: PaletteFor(ColourOff)},
		{name: "PaletteForEnv with the flag", pal: PaletteForEnv(true, nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ed, cap := paletteEditor(t, tc.pal)
			ed.val.SetText("hello")
			ed.render.Redraw(ed.val, paletteTestCols)
			if hasSGR(cap.bytes()) {
				t.Fatalf("colour-off redraw emitted SGR bytes:\n%q", cap.bytes())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The monochrome byte identity: the C1 change must cost ZERO bytes with colour
// off.
// ---------------------------------------------------------------------------

// monoGoldenCase is one editing-line shape whose colour-off bytes are pinned.
type monoGoldenCase struct {
	name string
	text string
	cols int
	// edRows is the region's editing-row budget, which the renderer CLIPS to:
	// the wrapped case needs two rows or the continuation row is never drawn
	// (and a golden that never draws the second row would pin nothing about it).
	edRows int
	// golden is the EXACT byte string the pre-C1 tree emitted for this shape.
	//
	// It was not typed from the new code: it was measured on a detached worktree
	// at the commit this change is built on (10dcd490) by running the same
	// redraw with MonoPalette, and the same three strings came back from both
	// trees. That is what makes this a regression assertion rather than a
	// description of the current output — the value predates the change.
	//
	// IT CARRIES THE PRE-TAKEOVER INSTALL, and that is deliberate: its first
	// bytes ARE the install emission of the tree that measured it — `EC[1;8r`
	// (the scroll region) then `EC[1;1H` (the cursor homed at the band's top).
	// The takeover (region.Enable's ED(2), commit 6544dad5, 2026-10-02) landed
	// AFTER the measurement commit (2026-09-28), so it is not in these strings:
	// monoInstallPrefix measures the install block as it is TODAY and
	// monoGoldenWant composes it with the golden's tail, rather than the whole
	// install being re-typed into each golden by hand. A re-typed install would
	// keep this test green through exactly the class of drift it exists to
	// catch — the shared prefix moving for a reason unrelated to the monochrome
	// identity.
	golden string
}

// monoInstallPrefix returns the bytes region.Enable emits on install at the
// goldens' geometry — the emission every golden in monoGoldenCases() starts
// with. It is MEASURED from the renderer here, never typed: the install block is
// region.Enable's private business, and the goldens must not encode a copy of it
// (Rule 13 — a constant bypassed by a literal is a defect).
//
// The capture of a fresh paletteEditor holds the install emission and nothing
// else: paletteEditor enables the region and never redraws.
func monoInstallPrefix(t *testing.T) string {
	t.Helper()
	_, cap := paletteEditor(t, MonoPalette)
	return cap.bytes()
}

// monoGoldenWant composes the expectation for one case: the install block as it
// is TODAY, followed by the golden's own tail — everything the drawing produced
// after the install.
//
// The golden's tail is taken by TRIMMING the pre-takeover install prefix from
// the golden, not by an offset: preTakeoverInstall is the install minus its
// takeover tail (see monoInstallPrefix), so the assertion below this one can
// first prove the golden really does still begin with the install it was
// measured under. A golden whose head is something else was typed by hand (or
// the install moved) and the comparison would silently be about two different
// blocks.
func monoGoldenWant(t *testing.T, golden string) string {
	t.Helper()
	install := monoInstallPrefix(t)
	preTakeover := strings.TrimSuffix(install, escClearScreen)
	if !strings.HasPrefix(golden, preTakeover) {
		t.Fatalf("golden %q does not begin with the install block it was measured under (%q) — the comparison would be between two different emissions", golden, preTakeover)
	}
	return install + strings.TrimPrefix(golden, preTakeover)
}

// monoGoldenCases are the three shapes that between them cover the editing
// renderer's drawing paths: a prompt with no buffer at all, a single-row buffer,
// and a buffer that wraps onto a continuation row (the row the buffer's own
// style span is now the FIRST and only span on, which is the case a
// prompt-only implementation would miss).
func monoGoldenCases() []monoGoldenCase {
	return []monoGoldenCase{
		{
			name: "prompt-only", text: "", cols: paletteTestCols, edRows: 1,
			golden: "\x1b[1;8r\x1b[1;1H\x1b[9;1H\x1b[2Kyou>\r\x1b[5C", // pre-takeover install: "\x1b[1;8r\x1b[1;1H"
		},
		{
			name: "single-row-buffer", text: "hello", cols: paletteTestCols, edRows: 1,
			golden: "\x1b[1;8r\x1b[1;1H\x1b[9;1H\x1b[2Kyou> hello\r\x1b[10C", // pre-takeover install: "\x1b[1;8r\x1b[1;1H"
		},
		{
			name: "wrapped-buffer", text: "hello world", cols: 8, edRows: 2,
			golden: "\x1b[1;8r\x1b[1;1H\x1b[8;1H\n\x1b[1;7r\x1b[8;1H\x1b[2Kyou> hel\x1b[9;1H\x1b[2Klo world\r\x1b[7C", // pre-takeover install: "\x1b[1;8r\x1b[1;1H"
		},
	}
}

// redrawEditingLine draws one editing line under a palette and returns the bytes
// that reached the sink — the same shape the goldens were measured with.
func redrawEditingLine(t *testing.T, pal Palette, text string, cols, edRows int) string {
	t.Helper()
	ed, cap := paletteEditor(t, pal)
	ed.region.SetEditorRows(edRows)
	ed.val.SetText(text)
	ed.render.Redraw(ed.val, cols)
	return cap.bytes()
}

// TestEditorMonochromeBytesAreUnchanged is the epic's second acceptance clause:
// with the monochrome table the emitted bytes equal the pre-change bytes EXACTLY.
//
// It is a byte-for-byte comparison against a golden measured on the tree BEFORE
// the buffer was styled (see monoGoldenCase.golden), not an assertion that some
// substring is absent — "contains no SGR" is satisfied by a renderer that emits
// different bytes for another reason, and the point of the clause is that a
// colour-off user's terminal receives precisely what it received before.
//
// Three tables are driven through the assertion, and that is deliberate: the
// identity belongs to the CAPABILITY (PaletteFor(ColourOff) is what an uncoloured
// run actually resolves), so it is not enough that the zero-value table a test
// names happens to emit nothing. If a future field is added to the monochrome
// path with a value, or PaletteFor stops returning the zero table, these cases
// fail.
//
// WHAT THIS TEST IS BLIND TO, AND WHY, since it is the whole reason this test was
// red and the RENDERER was not at fault. It compares one redraw's bytes, so it
// says nothing about the INSTALL block that precedes the redraw — and the install
// block is emitted by region.Enable, not by the redraw. That split is not a hole
// in this test but the reason it could only ever fail for the redraw's own
// reasons: `region.Enable`'s takeover is asserted by its own test on the real
// screen (takeover_test.go: the filler rows are gone from the grid, and the
// erase follows the home), which is the observable the takeover exists for and
// which this byte comparison cannot express. A future change to the install
// block therefore moves the goldens' HEAD (the golden still holds the install as
// it was measured; monoGoldenWant composes the two) and does NOT make the
// monochrome identity claim false — the redrawing half is what these strings
// pin.
//
// The guard this leaves in place is the CHEAP one, and it is judged sufficient:
// the drift was caught by a plain `go test`, the two halves are each pinned by
// the assertion that can actually observe them, and a third "the goldens are not
// stale" check would have to re-measure the pre-change tree at run time — which
// is exactly the tangle the goldens exist to avoid. The expensive guard, if one
// is ever wanted, is not in this test: it is the delivery-point sync that makes
// "the branch is one commit behind the base" visible BEFORE a run reports done
// (Rule 30a), which is the mechanism whose absence produced this drift.
func TestEditorMonochromeBytesAreUnchanged(t *testing.T) {
	off := []struct {
		name string
		pal  Palette
	}{
		{name: "MonoPalette", pal: MonoPalette},
		{name: "PaletteFor(ColourOff)", pal: PaletteFor(ColourOff)},
		{name: "PaletteForEnv with the flag", pal: PaletteForEnv(true, nil)},
	}
	for _, tc := range monoGoldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			want := monoGoldenWant(t, tc.golden)
			for _, oc := range off {
				got := redrawEditingLine(t, oc.pal, tc.text, tc.cols, tc.edRows)
				if got != want {
					t.Errorf("%s under %s emits different bytes than the pre-C1 tree:\n got %q\nwant %q", tc.name, oc.name, got, want)
				}
				if hasSGR(got) {
					t.Errorf("%s under %s emitted SGR bytes — the monochrome path is not byte-identical by construction any more:\n%q", tc.name, oc.name, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 3. The status row: label vs live hint, thinking vs idle, tool row.
// ---------------------------------------------------------------------------

// TestStatusRowEmphasisDistinguishesLabelLiveAndToolRow checks the three
// distinctions the epic names on the ONE status row: the label, the live hint,
// and the tool-activity row are each drawn under their own style (and the tool
// row is not drawn as a generic label).
func TestStatusRowEmphasisDistinguishesLabelLiveAndToolRow(t *testing.T) {
	label := "model-x" + StatusSep + ToolGlyph + " bash ls" + StatusSep + "12 tokens"
	row := styledStatusLine(Status{Label: label, Live: "ready"}, 200, BasicPalette)

	if !strings.Contains(row, BasicPalette.ToolRow.Wrap(ToolGlyph+" bash ls")) {
		t.Fatalf("the tool-activity row is not emphasised as tool activity:\n%q", row)
	}
	if !strings.Contains(row, BasicPalette.StatusLabel.Wrap("model-x")) {
		t.Fatalf("the label is not emphasised as the label:\n%q", row)
	}
	if !strings.Contains(row, BasicPalette.StatusLive.Wrap("ready")) {
		t.Fatalf("the live hint is not emphasised as live:\n%q", row)
	}
	if stripSGR(row) != label+"  ready" {
		t.Fatalf("emphasis changed the row's TEXT:\n got %q\nwant %q", stripSGR(row), label+"  ready")
	}
}

// TestStatusThinkingUsesItsOwnEmphasisAndIsAFlagNotAWording asserts the
// "thinking label vs live hint" distinction is driven by the producer's FACT,
// not by matching the hint's words: the same hint text is emphasised
// differently depending on Status.Thinking.
func TestStatusThinkingUsesItsOwnEmphasisAndIsAFlagNotAWording(t *testing.T) {
	s := Status{Label: "model-x", Live: "ready"}
	idle := styledStatusLine(s, 200, BasicPalette)
	s.Thinking = true
	busy := styledStatusLine(s, 200, BasicPalette)

	if !strings.Contains(busy, BasicPalette.Thinking.Wrap("ready")) {
		t.Fatalf("a thinking status did not use the thinking emphasis:\n%q", busy)
	}
	if strings.Contains(idle, BasicPalette.Thinking.Open()) {
		t.Fatalf("an idle status used the thinking emphasis:\n%q", idle)
	}
	if stripSGR(idle) != stripSGR(busy) {
		t.Fatalf("the two states changed the row's TEXT (%q vs %q)", stripSGR(idle), stripSGR(busy))
	}
}

// TestStatusRowMonochromeKeepsTheRowIdentical: with colour off the status row is
// byte-identical to the historic rendering (the styling is additive, and the
// truncation arithmetic is untouched).
func TestStatusRowMonochromeKeepsTheRowIdentical(t *testing.T) {
	cases := []Status{
		{Label: "model-x" + StatusSep + ToolGlyph + " bash ls", Live: "thinking…", Thinking: true},
		{Label: "m", Live: "ready"},
		{Label: "long-model-name" + StatusSep + "turn 3", Live: ""},
	}
	for _, s := range cases {
		for _, cols := range []int{1, 5, 12, 24, 80} {
			plain := statusLine(s, cols)
			if got := styledStatusLine(s, cols, MonoPalette); got != plain {
				t.Fatalf("monochrome status row at %d cols = %q, want %q", cols, got, plain)
			}
			styled := styledStatusLine(s, cols, BasicPalette)
			if stripSGR(styled) != plain {
				t.Fatalf("styled status row at %d cols changed the TEXT:\n got %q\nwant %q", cols, stripSGR(styled), plain)
			}
			if displayWidth(stripSGR(styled)) > cols && cols > 0 {
				t.Fatalf("status row overflows %d cols (%d cells): %q", cols, displayWidth(stripSGR(styled)), styled)
			}
		}
	}
}

// TestAnswerRendererStatusRowEmphasisReachesTheScreen drives the REAL paint path
// (drawStatus → write) and asserts the bytes that reached the sink carry the
// emphasis, so the status styling is not merely a pure function.
func TestAnswerRendererStatusRowEmphasisReachesTheScreen(t *testing.T) {
	cap := newPaletteCapture()
	reg := newRegion(cap.out)
	if !reg.Enable(paletteTestRows, paletteTestCols) {
		t.Fatal("region.Enable = false, want true")
	}
	r := NewAnswerRenderer(reg, func() Status { return Status{Label: "model-x", Live: "ready"} })
	r.SetCols(reg.Cols())
	r.SetPalette(BasicPalette)
	r.Tick()

	got := cap.bytes()
	if !strings.Contains(got, BasicPalette.StatusLabel.Open()) {
		t.Fatalf("the painted status row carries no label emphasis:\n%q", got)
	}
	if !strings.Contains(got, BasicPalette.StatusLive.Open()) {
		t.Fatalf("the painted status row carries no live emphasis:\n%q", got)
	}
	if !strings.Contains(stripSGR(got), "model-x") {
		t.Fatalf("the painted status row lost its text:\n%q", got)
	}
}

// ---------------------------------------------------------------------------
// 4. The answer body: code, diff, and the floor under the styling.
// ---------------------------------------------------------------------------

// TestComposeMarksCodeAndDiffLines proves the composer classifies fences and diff
// lines, and that the marker is applied per LINE.
func TestComposeMarksCodeAndDiffLines(t *testing.T) {
	c := &LineComposer{}
	cases := []struct {
		line string
		want LineStyle
	}{
		{"prose", LinePlain},
		{"```go", LineCode},
		{"func main() {}", LineCode},
		{"-old line", LineDiffDel},
		{"+new line", LineDiffAdd},
		{"@@ -1,2 +1,3 @@", LineCode},
		{"```", LineCode},
		{"prose again", LinePlain},
		{"- not a diff", LinePlain},
	}
	for _, tc := range cases {
		if got := c.StyleFor(tc.line); got != tc.want {
			t.Errorf("StyleFor(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

// TestAnswerBodyCarriesCodeAndDiffEmphasis asserts the emphasis reaches the
// capture for a fenced code block and a diff inside it, each line under its own
// style — and that the TEXT is unchanged from the monochrome run.
func TestAnswerBodyCarriesCodeAndDiffEmphasis(t *testing.T) {
	answer := "Here is the patch:\n```diff\n-old\n+new\n```\nDone.\n"

	coloured := paletteAnswerCapture(t, BasicPalette, answer)
	mono := paletteAnswerCapture(t, MonoPalette, answer)

	if !strings.Contains(coloured, BasicPalette.Code.Open()) {
		t.Fatalf("no code emphasis in the answer capture:\n%q", coloured)
	}
	// The row's text is written WITHOUT its terminator (the newline is the
	// renderer's own row break), so the emphasis wraps exactly the line's text.
	if !strings.Contains(coloured, BasicPalette.DiffAdd.Wrap("+new")) {
		t.Fatalf("the added line is not emphasised as an addition:\n%q", coloured)
	}
	if !strings.Contains(coloured, BasicPalette.DiffDel.Wrap("-old")) {
		t.Fatalf("the removed line is not emphasised as a removal:\n%q", coloured)
	}
	if hasSGR(mono) {
		t.Fatalf("the monochrome answer capture carries SGR:\n%q", mono)
	}
	// The comparison is on the CHARACTER streams: stripSGR removes every CSI
	// sequence, cursor placements included, so what is left is the text in the
	// order the user reads it — and it must be the same under both capabilities.
	if stripSGR(coloured) != stripSGR(mono) {
		t.Fatalf("emphasis changed the answer's text:\n coloured %q\n mono     %q", stripSGR(coloured), stripSGR(mono))
	}
}

// TestAnswerBodyEmphasisIsNotCarriedByColourAlone is the monochrome-readability
// gate: the two captures differ ONLY by SGR bytes, so every word, glyph and line
// break a reader needs is present with colour off.
func TestAnswerBodyEmphasisIsNotCarriedByColourAlone(t *testing.T) {
	answer := "intro\n```\ncode line\n```\n-removed\n+added\nend\n"
	coloured := paletteAnswerCapture(t, BasicPalette, answer)
	mono := paletteAnswerCapture(t, MonoPalette, answer)

	if !hasSGR(coloured) {
		t.Fatal("the coloured capture carries no SGR at all — the test would prove nothing")
	}
	if stripSGR(coloured) != stripSGR(mono) {
		t.Fatalf("the coloured run conveys something the monochrome run does not:\n coloured(stripped) %q\n mono(stripped) %q", stripSGR(coloured), stripSGR(mono))
	}
	if got := stripSGR(coloured); !strings.Contains(got, "code line") || !strings.Contains(got, "-removed") {
		t.Fatalf("the monochrome reading lost content: %q", got)
	}
}

// TestSGRBytesAreNeverCountedAsCells is the sizing gate: the identical run under
// both capabilities must place the same CHARACTERS in the same cells. It replays
// each capture through the cell grid and compares the visible contents, so a
// renderer that measured an SGR byte as a column would fail here.
func TestSGRBytesAreNeverCountedAsCells(t *testing.T) {
	answer := "line one\n```\ncode\n```\nline two\n"
	coloured := paletteAnswerCapture(t, BasicPalette, answer)
	mono := paletteAnswerCapture(t, MonoPalette, answer)

	cs := NewScreen(paletteTestCols, paletteTestRows)
	ms := NewScreen(paletteTestCols, paletteTestRows)
	cs.Feed([]byte(coloured))
	ms.Feed([]byte(mono))
	if cs.Contents() != ms.Contents() {
		t.Fatalf("the grid differs between colour on and off — an SGR byte moved a cell:\n coloured:\n%s\n mono:\n%s",
			cs.Contents(), ms.Contents())
	}
	if !strings.Contains(cs.Contents(), "code") {
		t.Fatalf("the answer text did not reach the grid:\n%s", cs.Contents())
	}
}

// TestAnswerRendererColourOffEmitsNoSGRThroughEveryPath drives the renderer's
// whole surface (Begin/Delta/Finish/Tick and the live-row wrap path) with the
// monochrome table and asserts not one SGR byte escapes.
func TestAnswerRendererColourOffEmitsNoSGRThroughEveryPath(t *testing.T) {
	cap := newPaletteCapture()
	reg := newRegion(cap.out)
	if !reg.Enable(paletteTestRows, paletteTestCols) {
		t.Fatal("region.Enable = false, want true")
	}
	r := NewAnswerRenderer(reg, func() Status { return Status{Label: "m", Live: "ready", Thinking: true} })
	r.SetCols(reg.Cols())
	r.SetPalette(PaletteForEnv(true, nil))
	r.Begin()
	r.Delta("a very long line that must wrap across the band's width several times over\n")
	r.Delta("```\ncode\n```\n")
	r.Tick()
	r.Finish()
	if hasSGR(cap.bytes()) {
		t.Fatalf("colour-off answer stream emitted SGR bytes:\n%q", cap.bytes())
	}
}

// ---------------------------------------------------------------------------
// 5. The named surfaces the epic lists, one assertion each.
// ---------------------------------------------------------------------------

// TestNamedSurfacesCarryTheirOwnStyle is the epic's surface checklist as a test:
// every field the epic names emits SOMETHING in colour and NOTHING in monochrome.
func TestNamedSurfacesCarryTheirOwnStyle(t *testing.T) {
	// Each surface is paired with the SAME surface's style under the two
	// capabilities: what must differ is the emitted BYTES, not which variable a
	// caller happened to read.
	surfaces := map[string][2]Style{
		"banner":        {BasicPalette.Banner, MonoPalette.Banner},
		"prompt":        {BasicPalette.Prompt, MonoPalette.Prompt},
		"status label":  {BasicPalette.StatusLabel, MonoPalette.StatusLabel},
		"live hint":     {BasicPalette.StatusLive, MonoPalette.StatusLive},
		"thinking":      {BasicPalette.Thinking, MonoPalette.Thinking},
		"tool activity": {BasicPalette.ToolRow, MonoPalette.ToolRow},
		"code":          {BasicPalette.Code, MonoPalette.Code},
		"diff added":    {BasicPalette.DiffAdd, MonoPalette.DiffAdd},
		"diff removed":  {BasicPalette.DiffDel, MonoPalette.DiffDel},
		"hint":          {BasicPalette.Hint, MonoPalette.Hint},
		"warning":       {BasicPalette.Warning, MonoPalette.Warning},
		"error":         {BasicPalette.Error, MonoPalette.Error},
	}
	for name, pair := range surfaces {
		on, off := pair[0], pair[1]
		if !hasSGR(on.Wrap("x")) {
			t.Errorf("%s: %+v emits no emphasis with colour on", name, on)
		}
		if got := off.Wrap("x"); got != "x" || hasSGR(got) {
			t.Errorf("%s: %q emitted under ColourOff, want the bare text", name, got)
		}
	}
	// The answer body is the BASELINE text at full strength: its staying
	// unstyled is the palette's decision, not a gap — stated here so the
	// exemption is explicit rather than forgotten.
	if hasSGR(BasicPalette.Answer.Wrap("x")) {
		t.Error("the answer body carries an emphasis: it is the baseline, not a highlighted surface")
	}
}

// TestWarningAndErrorAreDistinguishable checks the two "something is wrong"
// surfaces are not the same emphasis: a warning the user can continue past and an
// error must not read identically.
func TestWarningAndErrorAreDistinguishable(t *testing.T) {
	if BasicPalette.Warning.Open() == BasicPalette.Error.Open() {
		t.Fatal("warning and error share one emphasis — the reader cannot tell them apart")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// paletteAnswerCapture feeds an answer through a real AnswerRenderer over a real
// region with the given palette, and returns the whole byte stream.
func paletteAnswerCapture(t *testing.T, pal Palette, answer string) string {
	t.Helper()
	cap := newPaletteCapture()
	reg := newRegion(cap.out)
	if !reg.Enable(paletteTestRows, paletteTestCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want true", paletteTestRows, paletteTestCols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	r.SetPalette(pal)
	r.Begin()
	for _, line := range strings.SplitAfter(answer, "\n") {
		if line == "" {
			continue
		}
		r.Delta(line)
	}
	r.Finish()
	return cap.bytes()
}

// paletteOpenPty opens a pty master, skipping the test when the host cannot
// provide one (the TTY half of the decision is then genuinely untestable here).
func paletteOpenPty(t *testing.T) *os.File {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no /dev/ptmx on this host: %v", err)
	}
	fd := int(master.Fd())
	if _, err := PtyNumber(fd); err != nil {
		_ = master.Close() //stylegate:ignore closes the pty master the fixture opened
		t.Skipf("TIOCGPTN unavailable: %v", err)
	}
	if err := PtyUnlock(fd); err != nil {
		_ = master.Close() //stylegate:ignore closes the pty master the fixture opened
		t.Skipf("TIOCSPTLCK unavailable: %v", err)
	}
	return master
}

// paletteSlaveFile opens the pty's slave device — the file the decision is asked
// about, so the test answers "is this sink a terminal?" with a real terminal.
func paletteSlaveFile(t *testing.T, master *os.File) *os.File {
	t.Helper()
	fd := int(master.Fd())
	n, err := PtyNumber(fd)
	if err != nil {
		t.Fatalf("TIOCGPTN: %v", err)
	}
	path := filepath.Join("/dev/pts", itoa(n))
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Skipf("cannot open pty slave %s: %v", path, err)
	}
	//stylegate:ignore close of a probe/temp file whose content was already consumed
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// TestColourOnReachesATtyCapture is the positive half of the byte-exact gate at
// the LEVEL OF THE SINK: a real pty receives SGR bytes when colour is on, and
// nothing when the decision says off. It exercises the terminal's own fd, so the
// "TTY ⇒ colour" arm is measured rather than assumed.
func TestColourOnReachesATtyCapture(t *testing.T) {
	master := paletteOpenPty(t)
	//stylegate:ignore closes the pty master the fixture opened
	defer func() { _ = master.Close() }()
	slave := paletteSlaveFile(t, master)

	if ColourWanted(false, slave) != ColourOn {
		t.Skip("this host's pty is not reported as a terminal by the kernel probe; the TTY arm cannot be exercised")
	}
	pal := PaletteFor(ColourWanted(false, slave))
	if !hasSGR(pal.Banner.Wrap("x")) {
		t.Fatal("a colour-capable decision produced a palette that emits nothing")
	}
	off := PaletteFor(ColourWanted(true, slave))
	if hasSGR(off.Banner.Wrap("x")) {
		t.Fatal("the flag was ignored for a TTY sink")
	}
}

// TestNoColorEnvPresenceIsTheSignal pins the convention's sharp edge: an EMPTY
// NO_COLOR still disables (presence, not value) — the case a "value != ”" check
// silently gets wrong.
func TestNoColorEnvPresenceIsTheSignal(t *testing.T) {
	t.Setenv(EnvNoColor, "")
	if ColourWanted(false, nil) != ColourOff {
		t.Fatal("an empty NO_COLOR did not disable colour")
	}
}
