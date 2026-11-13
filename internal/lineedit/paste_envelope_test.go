package lineedit

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

// paste_envelope_test.go — the delivery-shape defect of a bracketed paste, and
// the recognition rule that repairs it.
//
// THE DEFECT (measured 2026-09-28 against a real pty): a paste was recognised ONLY
// by the size of one read (PasteThreshold, ~4 KiB). But the division of a paste
// into reads is the TERMINAL's to make, not ours: 64 KiB written to a pty arrives
// as 16 reads of 4095 bytes — the shape the spill was built for — while a paste
// the link delivers in SHORT writes arrives as reads of a few hundred bytes or
// fewer, every one of them below the threshold. Nothing spilled, the whole block
// was decoded into the buffer, and the 64 KiB value bound then cut the operator
// off tens of characters later. The paste was not lost — it was HEADED FOR THE
// WRONG PLACE, and its cost landed on the hand-typing that followed.
//
// THE RULE: the terminal says "a paste begins here" in its own voice — the
// bracketed-paste start marker (CSI 200 ~) at the head of the delivery — and that
// sentence does not depend on how the link chose to divide the reads. The marker
// therefore opens the block whatever the read's size is (Editor.pasteOpening),
// and the block's SIZE then decides what happens to it (Editor.pasteDecision):
// below the threshold and closed inside one read it is the short paste the
// historic path has always typed, and otherwise it is filed — which is the same
// threshold the editor always had, applied to bytes the terminal bracketed.
//
// THE PENDING() GUARD IS PRESERVED, and only where it still means something: a
// read that arrives while the decoder holds a fragment continues that fragment,
// so its bytes are not the whole of what is being delivered, and the SIZE rule
// keeps refusing such a read (spillableChunk). The envelope arm answers that
// question directly, because it JOINS the held fragment to the arriving bytes — so
// a marker the delivery split across two reads is recognised instead of being
// typed, while a fragment that is NOT the marker's head never opens a block.

// shortDeliveryRead is the read a link delivers when it hands the paste over in
// small pieces. It is deliberately far below PasteThreshold — that is the shape
// the size rule cannot see — and it is the fixture's OWN value (the production
// code has no such constant: how a paste is divided is the terminal's business).
const shortDeliveryRead = 512

// envelopeTestBody is the paste's content: whole lines of plain text, so no read
// ends inside a multi-byte sequence and the only escape bytes in the fixture are
// the framing the test adds itself.
const envelopeTestBody = "pasted content line with some words in it\n"

// envelopePasteBodyBytes is how much content the paste carries: well past
// PasteThreshold, and past the value bound the defect's symptom came from.
const envelopePasteBodyBytes = 64 * 1024

// envelopeTypedRunes is how many characters are typed AFTER the paste. The
// defect's symptom was that this typing was refused (the value bound had already
// been reached by the decoded paste), so the count is what the regression is
// about.
const envelopeTypedRunes = 200

// buildEnvelopeBody repeats envelopeTestBody up to n bytes.
func buildEnvelopeBody(n int) []byte {
	var b bytes.Buffer
	for b.Len() < n {
		b.WriteString(envelopeTestBody)
	}
	return b.Bytes()[:n]
}

// shortDeliveryReads splits a paste exactly as the delivery under test does: the
// framing and the body cut into reads of shortDeliveryRead bytes, in order. Every
// read is a whole step of the scripted source, so the editor sees the same
// sequence of read sizes the link produced.
func shortDeliveryReads(paste []byte) []scriptStep {
	var steps []scriptStep
	for i := 0; i < len(paste); i += shortDeliveryRead {
		steps = append(steps, scriptStep{data: paste[i:min(i+shortDeliveryRead, len(paste))]})
	}
	return steps
}

// TestPasteEnvelope_ShortReadsAreSpilledAndTypingSurvives is the regression test
// for the defect: a bracketed paste delivered in SHORT reads must be spilled —
// one file, one reference on the line, no paste text on the line — and the
// characters typed after it must all be accepted.
//
// It is RED on the pre-fix tree, and for the reason the operator reported: the
// short reads are below PasteThreshold, so nothing spills, the whole paste is
// decoded into the value, and the 200 typed characters are refused at the bound.
func TestPasteEnvelope_ShortReadsAreSpilledAndTypingSurvives(t *testing.T) {
	body := buildEnvelopeBody(envelopePasteBodyBytes)
	paste := append(append([]byte(bracketedPasteStart), body...), []byte(bracketedPasteEnd)...)

	steps := shortDeliveryReads(paste)
	typed := strings.Repeat("abcdefghij", envelopeTypedRunes/10)
	for _, r := range typed {
		steps = append(steps, scriptStep{data: []byte(string(r)), advance: submitGap})
	}
	steps = append(steps, scriptStep{data: []byte("\r"), advance: submitGap})

	store := newPasteStoreForTest(t)
	ed, _, _, out := newScriptedEditor(t, steps, pasteTestCols, nil)
	ed.SetPasteStore(store)
	res := runScript(t, ed)
	if len(res) != 1 {
		t.Fatalf("submitted lines = %d, want 1", len(res))
	}
	line := res[0].Line

	// ONE paste, ONE file, ONE reference: the reads are the delivery's division of
	// a single block, never several pastes.
	if got := strings.Count(line, pasteRefOpen); got != 1 {
		t.Fatalf("the line carries %d references, want ONE (a paste delivered over %d short reads is ONE paste): %q",
			got, len(steps), firstLine(line))
	}
	// The typed characters are all on the line, AFTER the reference: this is the
	// half of the symptom the operator felt ("typing is cut off right after").
	if !strings.HasSuffix(line, typed) {
		t.Fatalf("the %d characters typed after the paste are not on the line: it ends %q (line %d runes)",
			envelopeTypedRunes, tailOf(line, 60), utf8.RuneCountInString(line))
	}
	// The paste's own text is on the LINE nowhere: it went to the file.
	if strings.Contains(line, strings.TrimRight(envelopeTestBody, "\n")) {
		t.Fatal("the pasted content was typed onto the line: a spill must leave only the reference")
	}
	path := pastePathFromRef(t, line)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the reference names an unreadable file: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("the spill file is not the pasted content: %d bytes on disk vs %d pasted (first difference at %d)",
			len(got), len(body), firstDifference(got, body))
	}
	// A SECOND paste must not appear under the same directory: the delivery was
	// one block, so the session has one file for it.
	entries, err := os.ReadDir(store.Dir())
	if err != nil {
		t.Fatalf("the paste directory is unreadable: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("the paste directory holds %d files, want ONE (one paste, one file): %v", len(entries), entryNames(entries))
	}
	// The screen shows the reference and not the paste: the line is short.
	screen := wholeStreamScreen(t, out, pasteTestCols, pasteTestRows)
	if !strings.Contains(screen.Contents(), pasteRefOpen) {
		t.Fatalf("the rendered screen never carried the reference: %q", firstLine(screen.Contents()))
	}
	if strings.Contains(screen.Contents(), strings.TrimRight(envelopeTestBody, "\n")) {
		t.Fatal("the pasted content was PAINTED on the screen: the reference is what the line carries")
	}
}

// TestPasteEnvelope_MarkerSplitAcrossReadsIsStillPaste is the boundary the
// envelope arm exists to read across: the terminal's start marker is six bytes,
// and a delivery may divide it between two reads. The block must still be
// recognised — and the marker's halves must not be filed as the paste's own first
// characters.
//
// The marker is delivered one byte short of whole, with the rest at the head of
// the next read; the body follows in short reads. The first read is deliberately
// BELOW PasteThreshold, so the size arm cannot be what recognises it.
func TestPasteEnvelope_MarkerSplitAcrossReadsIsStillPaste(t *testing.T) {
	body := buildEnvelopeBody(envelopePasteBodyBytes)
	paste := append(append([]byte(bracketedPasteStart), body...), []byte(bracketedPasteEnd)...)
	// The split point: inside the marker, so its tail rides the next read.
	splitAt := len(pasteStartMarker) - 2
	if splitAt <= 0 || splitAt >= len(paste) {
		t.Fatalf("the fixture's split point %d is outside the paste", splitAt)
	}
	steps := []scriptStep{{data: paste[:splitAt]}}
	steps = append(steps, shortDeliveryReads(paste[splitAt:])...)
	steps = append(steps, scriptStep{data: []byte("\r"), advance: submitGap})

	store := newPasteStoreForTest(t)
	ed, _, _, _ := newScriptedEditor(t, steps, pasteTestCols, nil)
	ed.SetPasteStore(store)
	res := runScript(t, ed)
	if len(res) != 1 {
		t.Fatalf("submitted lines = %d, want 1", len(res))
	}
	// The marker's first half is a bare ESC in the decoder's buffer when the
	// second read arrives. It must not be typed (an ESC is not content) and it
	// must not become the file's first bytes.
	if strings.ContainsAny(res[0].Line, "\x1b") {
		t.Fatalf("the split marker was typed onto the line: %q", firstLine(res[0].Line))
	}
	path := pastePathFromRef(t, res[0].Line)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the reference names an unreadable file: %v", err)
	}
	if bytes.HasPrefix(got, []byte{0x1b}) {
		t.Fatalf("the file begins with an escape byte: the marker's tail was filed as content (first 12 bytes %q)",
			headOf(got, 12))
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("the spill file is not the pasted content: %d bytes on disk vs %d pasted (first difference at %d)",
			len(got), len(body), firstDifference(got, body))
	}
}

// TestPasteEnvelope_ShortPasteStillTakesTheHistoricPath is the OTHER side of the
// rule, and it is what keeps the change from being "spill everything": a
// bracketed paste that fits in one read and is below the paste threshold is the
// SHORT paste the editor has always typed. It must be typed exactly as before —
// CRLF collapsing to one line break, no file, no reference.
//
// Without this arm the envelope rule would file a three-word paste, and a pasted
// newline would stop submitting as the line break it has always been.
func TestPasteEnvelope_ShortPasteStillTakesTheHistoricPath(t *testing.T) {
	body := strings.Repeat("x", PasteThreshold-shortPasteSlack)
	data := []byte(bracketedPasteStart + body + "\r\n")
	if len(data) >= PasteThreshold {
		t.Fatalf("the fixture must stay under the threshold: %d bytes", len(data))
	}
	store := newPasteStoreForTest(t)
	ed, _, _, _ := newScriptedEditor(t, []scriptStep{{data: data}}, pasteTestCols, nil)
	ed.SetPasteStore(store)

	res, err := ed.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.EOF {
		t.Fatalf("a short paste ended the line (result %+v): a pasted CRLF must never submit", res)
	}
	if got := ed.Value().Text(); got != body+"\n" {
		t.Fatalf("buffer = %q, want the pasted text with CRLF collapsed to one break", firstLine(got))
	}
	if _, err := os.ReadDir(store.Dir()); err == nil {
		t.Fatalf("a short paste was spilled: %s exists", store.Dir())
	}
}

// headOf trims an assertion message to the first n bytes, quoted.
func headOf(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n])
}

// tailOf trims an assertion message to the end of a string.
func tailOf(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// entryNames lists a directory's entries, for an assertion message.
func entryNames(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
