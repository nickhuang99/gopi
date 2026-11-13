package lineedit

import (
	"strings"
	"testing"
	"time"
)

// renderScreen replays everything the editor wrote through the vt100 cell grid
// and returns the screen — the same thing a user's terminal would be showing.
// This is the rule-10.2 verification: the assertion is on RENDERED CONTENT, so a
// cursor one column off, a stale row, or a line never drawn is visible to the
// test instead of invisible behind a "the callback fired" check.
func renderScreen(t *testing.T, out []byte, cols, rows int) *Screen {
	t.Helper()
	sc := NewScreen(cols, rows)
	sc.Feed(out)
	return sc
}

// editingScreen is the screen as it stood at the last redraw — the state the
// user is looking at while the line is still being edited.
func editingScreen(t *testing.T, out *recordingWriter, cols, rows int) *Screen {
	t.Helper()
	return renderScreen(t, out.upToLastRedraw(), cols, rows)
}

// TestLineEditorRender asserts the drawn line and the cursor cell for the
// simplest case: text typed at the end of the line.
func TestLineEditorRender(t *testing.T) {
	const typingPause = 60 * time.Millisecond
	steps := []scriptStep{
		{data: []byte("hello"), advance: typingPause},
		{timeout: true},
	}
	in, _, _, out := newScriptedEditor(t, steps, 80, nil)
	if _, err := in.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	sc := editingScreen(t, out, 80, 24)
	if got := sc.Line(0); got != "you> hello" {
		t.Fatalf("rendered row 0 = %q, want %q", got, "you> hello")
	}
	if sc.CurRow != 0 || sc.CurCol != len("you> hello") {
		t.Fatalf("cursor at (%d,%d), want (0,%d)", sc.CurRow, sc.CurCol, len("you> hello"))
	}
}

// TestLineEditorRenderMidLine asserts the redraw after moving the cursor: the
// text is unchanged but the CURSOR cell must be inside the line, which is the
// half of "rendered truth" a text-only assertion cannot see.
func TestLineEditorRenderMidLine(t *testing.T) {
	steps := []scriptStep{
		{data: []byte("abcdef")},
		{timeout: true, advance: time.Second},
		{data: []byte("\x1b[D\x1b[D")}, // two lefts: cursor between 'd' and 'e'
		{timeout: true},
	}
	in, _, _, out := newScriptedEditor(t, steps, 80, nil)
	if _, err := in.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	sc := editingScreen(t, out, 80, 24)
	if got := sc.Line(0); got != "you> abcdef" {
		t.Fatalf("rendered row 0 = %q, want %q", got, "you> abcdef")
	}
	if sc.CurCol != len("you> abcd") {
		t.Fatalf("cursor column = %d, want %d (two columns left of the end)", sc.CurCol, len("you> abcd"))
	}
}

// TestLineEditorRenderUnicodeCursorWidth is the wide-rune cursor check on a real
// drawing: a CJK character occupies two columns, so the cursor must sit two
// cells right of where a byte/rune count would put it.
func TestLineEditorRenderUnicodeCursorWidth(t *testing.T) {
	steps := []scriptStep{
		{data: []byte("知知")},
		{timeout: true},
	}
	in, _, _, out := newScriptedEditor(t, steps, 80, nil)
	if _, err := in.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	sc := editingScreen(t, out, 80, 24)
	if got := sc.Line(0); got != "you> 知知" {
		t.Fatalf("rendered row 0 = %q, want %q", got, "you> 知知")
	}
	// "you> " is 5 columns; each 知 is 2 → the cursor sits at column 9.
	if sc.CurCol != 9 {
		t.Fatalf("cursor column = %d, want 9 (wide runes are two cells each)", sc.CurCol)
	}
}

// TestLineEditorRenderShrink asserts the erase discipline: after deleting text
// the redrawn row carries NO residue from the longer line that was there before.
func TestLineEditorRenderShrink(t *testing.T) {
	steps := []scriptStep{
		{data: []byte("abcdefghij")},
		{timeout: true, advance: time.Second},
		{data: []byte("\x17")}, // Ctrl+W kills the word
		{timeout: true},
	}
	in, _, _, out := newScriptedEditor(t, steps, 80, nil)
	if _, err := in.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	sc := editingScreen(t, out, 80, 24)
	if got := sc.Line(0); got != "you>" {
		t.Fatalf("rendered row 0 = %q, want %q (no residue of the killed text)", got, "you>")
	}
	if crossRow := sc.Line(1); crossRow != "" {
		t.Fatalf("row 1 = %q, want empty (the shrink must not leave a drawn row)", crossRow)
	}
}

// TestLineEditorRenderMultiline asserts an embedded newline is drawn as its own
// row (multiline entry is readable, not one wound-around line) and the cursor
// lands on the last row.
func TestLineEditorRenderMultiline(t *testing.T) {
	steps := []scriptStep{
		{data: []byte("first")},
		{timeout: true, advance: time.Second},
		{data: []byte("\x1b\r")}, // Alt+Enter: a newline, never a submit
		{timeout: true, advance: time.Second},
		{data: []byte("second")},
		{timeout: true},
	}
	in, _, _, out := newScriptedEditor(t, steps, 80, nil)
	if _, err := in.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	sc := editingScreen(t, out, 80, 24)
	if got := sc.Line(0); got != "you> first" {
		t.Fatalf("rendered row 0 = %q, want %q", got, "you> first")
	}
	if got := sc.Line(1); got != "second" {
		t.Fatalf("rendered row 1 = %q, want %q", got, "second")
	}
	if sc.CurRow != 1 || sc.CurCol != len("second") {
		t.Fatalf("cursor at (%d,%d), want (1,%d)", sc.CurRow, sc.CurCol, len("second"))
	}
}

// TestLineEditorRenderWraps asserts soft wrapping at the terminal width and that
// the cursor is placed on the wrapped row.
func TestLineEditorRenderWraps(t *testing.T) {
	// Width 20 with a 5-column prompt: 15 buffer columns fit on row 0.
	steps := []scriptStep{
		{data: []byte(strings.Repeat("x", 20))},
		{timeout: true},
	}
	in, _, _, out := newScriptedEditor(t, steps, 20, nil)
	if _, err := in.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	sc := editingScreen(t, out, 20, 24)
	if got := sc.Line(0); got != "you> "+strings.Repeat("x", 15) {
		t.Fatalf("rendered row 0 = %q, want the 15 columns that fit", got)
	}
	if got := sc.Line(1); got != "xxxxx" {
		t.Fatalf("rendered row 1 = %q, want the wrapped remainder", got)
	}
	if sc.CurRow != 1 || sc.CurCol != 5 {
		t.Fatalf("cursor at (%d,%d), want (1,5)", sc.CurRow, sc.CurCol)
	}
}

// TestLineEditorRenderSigwinchRedraw asserts the A1 SIGWINCH path: a resize
// notification repaints the SAME buffer at the NEW width. The assertion is on
// the rendered screen (row 0 must be re-wrapped), not on "a redraw happened".
func TestLineEditorRenderSigwinchRedraw(t *testing.T) {
	clock := &fakeClock{t: time.Unix(editorTestEpoch, 0)}
	src := &scriptedSource{
		width: 20,
		steps: []scriptStep{
			{data: []byte(strings.Repeat("y", 18))},
			{winch: true},
			{timeout: true},
		},
		clock: clock,
	}
	// The scripted source reports the width it was built with; the resize is
	// modelled by handing the editor a source whose width changes under it.
	src.width = 12
	out := &recordingWriter{}
	// The editor is built at width 20 and then resized to 12 by the fake.
	ed := NewEditor(&resizingSource{src: src, widths: []int{20, 12}}, out, promptForTest, nil)
	ed.SetClock(clock.Now)
	if _, err := ed.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	sc := NewScreen(12, 24)
	sc.Feed(out.upToLastRedraw())
	if got := sc.Line(0); got != "you> yyyyyyy" {
		t.Fatalf("rendered row 0 after resize = %q, want the 7 columns that now fit", got)
	}
	if got := sc.Line(1); got != "yyyyyyyyyyy" {
		t.Fatalf("rendered row 1 after resize = %q, want the remaining 11 columns", got)
	}
	if src.winchCount != 1 {
		t.Fatalf("the editor consumed %d resize notifications, want 1", src.winchCount)
	}
}

// resizingSource reports a different width after each read, so a resize can be
// driven without a real window-size change.
type resizingSource struct {
	src    *scriptedSource
	widths []int
	reads  int
}

// Height delegates to the underlying scripted source.
func (r *resizingSource) Height(fallback int) int { return r.src.Height(fallback) }

// ReadEvent delegates to the underlying scripted source.
func (r *resizingSource) ReadEvent(buf []byte, timeoutMS int) (Event, error) {
	ev, err := r.src.ReadEvent(buf, timeoutMS)
	r.reads++
	return ev, err
}

// Width walks the scripted widths, holding at the last one.
func (r *resizingSource) Width(fallback int) int {
	i := r.reads
	if i >= len(r.widths) {
		i = len(r.widths) - 1
	}
	if i < 0 {
		return fallback
	}
	return r.widths[i]
}

// TestLineEditorScreenGrid pins the cell grid itself: the assertions above are
// only as trustworthy as the model they are read through, so the grid's own
// cursor/erase/wrap handling is checked directly.
func TestLineEditorScreenGrid(t *testing.T) {
	sc := NewScreen(10, 5)
	sc.Feed([]byte("abc"))
	if got := sc.Line(0); got != "abc" || sc.CurCol != 3 {
		t.Fatalf("after abc: line=%q col=%d, want \"abc\" and 3", got, sc.CurCol)
	}
	// CR + CSI 2K erases the whole row; the cursor stays at column 0.
	sc.Feed([]byte("\r\x1b[2K"))
	if got := sc.Line(0); got != "" {
		t.Fatalf("after CR+EL(2): line = %q, want empty", got)
	}
	// Write past the right edge: the terminal wraps to the next row.
	sc.Feed([]byte(strings.Repeat("z", 12)))
	if got := sc.Line(0); got != strings.Repeat("z", 10) {
		t.Fatalf("row 0 = %q, want 10 z", got)
	}
	if got := sc.Line(1); got != "zz" {
		t.Fatalf("row 1 = %q, want the two wrapped z", got)
	}
	// CUP positions the cursor absolutely.
	sc.Feed([]byte("\x1b[3;5H"))
	if sc.CurRow != 2 || sc.CurCol != 4 {
		t.Fatalf("after CUP 3;5 cursor = (%d,%d), want (2,4)", sc.CurRow, sc.CurCol)
	}
}
