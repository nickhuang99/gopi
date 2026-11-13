package uiclient

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/sgr"
)

// emphasis_test.go — `gopi ui`'s half of the intra-line emphasis layer.
//
// The claims, and why each is a test rather than a reading of the code:
//
//  1. the transcript's ROW carries the emphasis runs and every measurement
//     (wrap, indent, the full-width cut) works on the text the terminal DRAWS —
//     so a marker occupies no column, and a long emphasised line wraps into the
//     SAME rows as the plain one (TestEmphasisWrapCountIsUnchanged);
//  2. the model's View() stays READABLE AS TEXT: the marker is gone from what the
//     agent reads, and with the monochrome palette the view is byte-identical to
//     the same answer without emphasis (the sha256 comparison);
//  3. a row whose emphasis the stream has not finished is not committed: the
//     letters never change when the closing marker arrives;
//  4. the palette is the CALLER's capability decision (Config.Palette), never the
//     model's own — the zero value is plain text.

// TestEmphasisViewIsReadableAsText logs the frame the agent reads of an
// emphasised answer and pins what it may not contain: no marker, no escape byte
// (monochrome palette), and every streamed word still present. The log is
// deliberate evidence — the report quotes this frame instead of describing it.
func TestEmphasisViewIsReadableAsText(t *testing.T) {
	m := newTestModel(t)
	m.pal = lineedit.MonoPalette
	m.width = 68
	m.entries = []entry{
		{kind: entryUser, text: "what does the **cell arithmetic** say?"},
		assistantEntry("The answer: the **row shear** comes from counting a marker as a cell,\nand the fix is to measure the drawn text only."),
	}
	m.assistantIdx = -1
	v := m.View()
	t.Logf("View() of an emphasised answer (monochrome, width %d):\n%s", m.width, v)

	answerLine := ""
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "row shear") {
			answerLine = l
		}
	}
	if answerLine == "" {
		t.Fatalf("the answer never reached the View():\n%s", v)
	}
	if strings.Contains(answerLine, lineedit.BoldMarker) {
		t.Fatalf("the agent-read frame still spells the marker: %q", answerLine)
	}
	if !strings.Contains(answerLine, "comes from counting") {
		t.Fatalf("the streamed words were lost: %q", answerLine)
	}
	if strings.ContainsRune(v, 0x1b) {
		t.Fatalf("the monochrome View() carries an escape byte:\n%q", v)
	}
}

// emphasisBody is the answer the wrap/view assertions stream: prose with one bold
// span, long enough to wrap at the test width.
const emphasisBody = "The answer explains that the **row shear** comes from counting a " +
	"marker as a cell, and the fix is to measure the drawn text only."

// assistantRow builds the transcript row a streamed assistant answer produces,
// through the model's own constructor (assistantEntry — the path the delta
// stream and the done frame both use), so the span construction under test is
// the shipping one rather than a fixture's.
func assistantRow(t *testing.T, text string) entry {
	t.Helper()
	return assistantEntry(text)
}

// TestEntryCarriesEmphasisSpans pins the row's construction: an answer with a
// marker carries runs whose text is the DRAWN text, and an answer without one
// carries none (nil — the plain path, byte-identical to what it was before this
// layer existed).
func TestEntryCarriesEmphasisSpans(t *testing.T) {
	withMarker := assistantRow(t, emphasisBody)
	if len(withMarker.spans) == 0 {
		t.Fatal("the emphasised row carries no spans")
	}
	if got, want := withMarker.plainText(), strings.Replace(emphasisBody, "**", "", -1); got != want {
		t.Fatalf("plainText() = %q, want the markers removed %q", got, want)
	}
	bold := 0
	for _, sp := range withMarker.spans {
		switch sp.em {
		case lineedit.EmphasisBold:
			bold++
			if sp.text != "row shear" {
				t.Errorf("bold run = %q, want %q", sp.text, "row shear")
			}
		case lineedit.EmphasisNone:
		default:
			t.Errorf("run %q has emphasis %d, want none or the tokenizer's own bold", sp.text, sp.em)
		}
	}
	if bold != 1 {
		t.Errorf("bold runs = %d, want 1", bold)
	}

	plain := assistantRow(t, "no marker at all, just prose")
	if len(plain.spans) != 0 {
		t.Errorf("a row with no marker carries %d spans, want none", len(plain.spans))
	}
	if plain.text != "no marker at all, just prose" {
		t.Errorf("the plain row's text was altered: %q", plain.text)
	}
}

// TestEmphasisWrapCountIsUnchanged is the width invariance at the transcript
// layer: an emphasised answer wraps into the SAME lines as the same answer
// written without the markers, so the emphasis adds no layout arithmetic.
func TestEmphasisWrapCountIsUnchanged(t *testing.T) {
	const width = 40
	spans := assistantRow(t, emphasisBody)
	plain := assistantRow(t, strings.Replace(emphasisBody, "**", "", -1))

	emphModel := newTestModel(t)
	emphModel.pal = lineedit.MonoPalette
	plainModel := newTestModel(t)
	plainModel.pal = lineedit.MonoPalette

	got := emphModel.wrapEntry(spans, width)
	want := plainModel.wrapEntry(plain, width)
	if len(got) != len(want) {
		t.Fatalf("emphasised answer wraps into %d rows, plain into %d:\n emphasised: %q\n plain: %q",
			len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("row %d differs:\n emphasised: %q\n plain:      %q", i, got[i], want[i])
		}
	}
	// The drawn text is the plain answer's, character for character — the marker
	// took no cell on any row.
	if joined, wantJoined := strings.Join(got, "\n"), strings.Join(want, "\n"); joined != wantJoined {
		t.Fatalf("the emphasised transcript's characters differ:\n got %q\nwant %q", joined, wantJoined)
	}
}

// TestEmphasisColourOffViewIsByteIdentical is the acceptance's sha256 claim at
// the SURFACE the agent reads: with the monochrome palette the whole View() of an
// emphasised answer is the same BYTES as the View() of the same answer without
// the markers. A hash is used because "the same" is a claim about every byte, and
// a View() is where an escape byte could leak from (bubbletea is handed the whole
// frame).
func TestEmphasisColourOffViewIsByteIdentical(t *testing.T) {
	render := func(text string) (string, []byte) {
		m := newTestModel(t)
		m.pal = lineedit.MonoPalette
		m.entries = []entry{assistantEntry(text)}
		m.assistantIdx = -1
		m.quitting = false
		v := m.View()
		sum := sha256.Sum256([]byte(v))
		return v, sum[:]
	}

	withMarker, sumA := render(emphasisBody + "\nthe closing line has no marker.")
	without, sumB := render(strings.Replace(emphasisBody, "**", "", -1) + "\nthe closing line has no marker.")

	if sumA == nil || hex.EncodeToString(sumA) != hex.EncodeToString(sumB) {
		t.Fatalf("colour-off View() differs:\n ** sha256 %s\n  - sha256 %s\n--- with marker ---\n%s\n--- without ---\n%s",
			hex.EncodeToString(sumA), hex.EncodeToString(sumB), withMarker, without)
	}
	// No escape byte may reach the frame under the monochrome palette, and the
	// marker must not be readable in what the agent gets.
	if strings.ContainsRune(withMarker, 0x1b) {
		t.Fatalf("the monochrome View() carries an escape byte:\n%q", withMarker)
	}
	if strings.Contains(withMarker, "**") {
		t.Fatalf("the View() still spells the marker:\n%s", withMarker)
	}
	if !strings.Contains(withMarker, "row shear") {
		t.Fatalf("the drawn text is missing from the View():\n%s", withMarker)
	}

	// The colour-capable run must differ (the emphasis reached the frame) while
	// drawing the same printable text.
	colour := newTestModel(t)
	colour.pal = lineedit.BasicPalette
	colour.entries = []entry{assistantEntry(emphasisBody + "\nthe closing line has no marker.")}
	colour.assistantIdx = -1
	cv := colour.View()
	if !strings.Contains(cv, sgr.BoldSeq()) {
		t.Fatalf("the colour-capable View() carries no bold sequence:\n%q", cv)
	}
	if stripSGRForTest(cv) != stripSGRForTest(withMarker) {
		t.Fatalf("the coloured View() draws different characters:\n got %q\nwant %q",
			stripSGRForTest(cv), stripSGRForTest(withMarker))
	}
}

// TestEmphasisRowsSurviveStreaming is the streaming claim at the transcript: the
// row is rebuilt on every delta (the model appends to one entry), an UNCLOSED
// marker is drawn literally while the stream is unfinished, and the decided text
// keeps the letters it already showed once the closing marker arrives.
func TestEmphasisRowsSurviveStreaming(t *testing.T) {
	m := newTestModel(t)
	m.pal = lineedit.MonoPalette
	m.entries = []entry{assistantEntry("")}
	m.assistantIdx = 0
	m.width = 80

	// The delta path, taken exactly as `gopi ui` takes it: applyFrame is the ONE
	// place a delta becomes transcript text.
	for _, piece := range []string{"the **row", " shear** fix"} {
		m.applyFrame(Frame{Event: EventDelta, Data: `{"text":"` + piece + `"}`})
	}
	row := m.entries[0]
	if row.text != "the **row shear** fix" {
		t.Fatalf("the streamed row's raw text = %q, want the concatenated deltas", row.text)
	}
	if got, want := row.plainText(), "the row shear fix"; got != want {
		t.Fatalf("the streamed row draws %q, want %q", got, want)
	}
	// The emphasis survived the frame split: the bold run is "row shear", exactly
	// as it would have been for the whole line in one frame.
	var bold []string
	for _, sp := range row.spans {
		if sp.em == lineedit.EmphasisBold {
			bold = append(bold, sp.text)
		}
	}
	if strings.Join(bold, "|") != "row shear" {
		t.Fatalf("bold runs after the split = %v, want [row shear]", bold)
	}

	// The unfinished shape: the letters a reader already sees are the letters the
	// decided row shows, so nothing on screen changes when the rest arrives.
	open := assistantEntry("the **row shear")
	if open.plainText() != "the **row shear" {
		t.Fatalf("an unclosed marker did not stay literal: %q", open.plainText())
	}
	view := func(e entry) string {
		mm := newTestModel(t)
		mm.pal = lineedit.MonoPalette
		mm.width = 80
		mm.entries = []entry{e}
		return mm.View()
	}
	if !strings.Contains(view(open), "**row shear") {
		t.Fatalf("the unfinished row's View() lost its literal marker:\n%s", view(open))
	}
}

// stripSGRForTest removes SGR sequences from a frame so the two capabilities can
// be compared on their PRINTABLE text. It is deliberately a second, small
// implementation rather than lineedit's unexported stripSGR: this test asserts
// about the FRAME `gopi ui` hands to bubbletea, and reading it through the
// renderer's own helper would test the helper.
func stripSGRForTest(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// quotedFenceAnswer is the answer shape session a8553c96 streamed on 2026-09-26:
// an answer that quotes its own screen, so the pasted capture's fence marker sits
// INDENTED inside the fence that quotes it. In `gopi chat` that inner marker used
// to flip the composer's block state for the rest of the answer, so the closing
// paragraph was classified as code and its `**bold**` was dropped.
//
// It is a real answer rather than a synthetic one on purpose: the transcript
// carrier has no fence classifier of its own (it tokenizes every row's text), so
// what this fixture pins is that the SURFACE where the operator reported the
// missing bold — the transcript — draws it, whatever the block state.
const quotedFenceAnswer = "here is what I saw:\n" +
	lineedit.FenceMarker + "\n" +
	"you> head -3 go.mod\n" +
	"      " + lineedit.FenceMarker + "\n" +
	"module github.com/nick/gopi\n" +
	lineedit.FenceMarker + "\n" +
	"the **bold conclusion** follows"

// TestSpanPathDrawsBoldAfterAQuotedFenceMarker exercises `gopi ui`'s half of the
// emphasis layer on that answer: the row's runs are built by the shipping
// constructor, the span path (renderSpanLine -> lineedit.SpanStyle) draws the bold
// run through the palette's own bold sequence, and the monochrome frame stays
// byte-identical to the same answer without markers.
func TestSpanPathDrawsBoldAfterAQuotedFenceMarker(t *testing.T) {
	row := assistantEntry(quotedFenceAnswer)
	if got, want := row.plainText(), strings.Replace(quotedFenceAnswer, lineedit.BoldMarker, "", -1); got != want {
		t.Fatalf("plainText() = %q, want the markers removed %q", got, want)
	}
	bold := 0
	for _, sp := range row.spans {
		if sp.em != lineedit.EmphasisBold {
			continue
		}
		bold++
		if sp.text != "bold conclusion" {
			t.Errorf("bold run = %q, want %q", sp.text, "bold conclusion")
		}
	}
	if bold != 1 {
		t.Fatalf("the row carries %d bold run(s), want 1 — the closing paragraph lost its emphasis", bold)
	}

	// The COLOUR-CAPABLE frame: the bold run is drawn through the palette's own
	// mapping (SpanStyle), which is the one place a span's emphasis becomes bytes.
	m := newTestModel(t)
	m.pal = lineedit.BasicPalette
	m.entries = []entry{row}
	m.assistantIdx = -1
	v := m.View()
	wantSeq := lineedit.SpanStyle(lineedit.BasicPalette, lineedit.EmphasisBold).Wrap("bold conclusion")
	if !strings.Contains(v, wantSeq) {
		t.Fatalf("the frame does not carry %q:\n%s", wantSeq, v)
	}
	t.Logf("View() bold run drawn as %q", wantSeq)

	// The MONOCHROME frame is the same characters with nothing emitted, and the
	// quoted fence line is still readable there (the capture is content, not
	// markup — the marker that used to be misread as a delimiter is text).
	mono := newTestModel(t)
	mono.pal = lineedit.MonoPalette
	mono.entries = []entry{row}
	mono.assistantIdx = -1
	mv := mono.View()
	if strings.ContainsRune(mv, 0x1b) {
		t.Fatalf("the monochrome frame carries an escape byte:\n%q", mv)
	}
	if strings.Contains(mv, lineedit.BoldMarker) {
		t.Fatalf("the monochrome frame still spells the marker:\n%s", mv)
	}
	if !strings.Contains(mv, "module github.com/nick/gopi") {
		t.Fatalf("the quoted capture was lost from the frame:\n%s", mv)
	}
	if !strings.Contains(mv, "the bold conclusion follows") {
		t.Fatalf("the closing prose line is missing from the frame:\n%s", mv)
	}
	if stripSGRForTest(v) != stripSGRForTest(mv) {
		t.Fatalf("the two capabilities draw different characters:\n colour %q\n mono   %q", stripSGRForTest(v), stripSGRForTest(mv))
	}
}
