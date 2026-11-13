package lineedit

// keyprotocol_editor_test.go — the DECODE-SIDE regression that the negotiation
// exists for, asserted at the editor's own boundary.
//
// The claim under test is not "CSI-u decodes to KeyNewline" (that is
// value_test.go's table, and it predates this work). It is the pair of END-TO-END
// consequences the epic is about:
//
//	CSI 13;2u  (what a disambiguating terminal sends for Shift+Enter)
//	           → a newline IN the buffer, and Run does NOT return a line
//	0x0d       (Enter)                       → Run RETURNS the line
//	ESC CR     (Alt+Enter, the fallback the chat layer advises)
//	           → a newline IN the buffer, and Run does NOT return a line
//
// A table over the decoder would pass even if the editor submitted the line
// anyway, which is exactly the information the epic is about; so the assertion is
// on the editor's RESULT and on the buffer it hands back.
//
// WHY NO SLEEP IS NEEDED BETWEEN THE STEPS, and why this is not a paste flake:
// the paste-burst clock is the editor's own (Editor.SetClock), so the script
// advances it by a whole burst window before the submitting Enter. Without that
// the three keys would be one burst and the Enter would be read as pasted
// content — which is the editor's documented policy, not a defect this case
// exists to re-assert (inject.go's header measures that failure).

import (
	"strings"
	"testing"
)

// keyProtocolBurstGap is how far the fake clock moves the editor past a burst
// window before the Enter that must SUBMIT. It is the editor's own window scaled
// up, so the assertion cannot be sitting on a boundary.
const keyProtocolBurstGap = 10 * PasteEnterSuppressWindow

// runKeyProtocolScript drives one editor run over a scripted source and reports
// what Run returned plus the text left in the buffer.
func runKeyProtocolScript(t *testing.T, steps []scriptStep, advanceBeforeEnter []int) (Result, string) {
	t.Helper()
	// The clock steps: every index listed in advanceBeforeEnter gets
	// keyProtocolBurstGap applied before its step is delivered.
	gapped := make([]scriptStep, 0, len(steps))
	j := 0
	for i, st := range steps {
		if j < len(advanceBeforeEnter) && advanceBeforeEnter[j] == i {
			st.advance = keyProtocolBurstGap
			j++
		}
		gapped = append(gapped, st)
	}
	ed, src, clock, _ := newScriptedEditor(t, gapped, editorTestWidth, NewHistory(0, ""))
	// The scripted source's clock is the editor's own: the burst classification
	// reads it through Editor.SetClock, installed by newScriptedEditor.
	_ = clock
	_ = src
	res, err := ed.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res, ed.Value().Text()
}

// editorTestWidth is the editing width the cases below run at. It is wide enough
// that wrapping never enters the picture: the subject is which bytes SUBMIT, not
// how a long line is laid out.
const editorTestWidth = 40

// TestKeyProtocolShiftEnterDoesNotSubmit is the acceptance's decode half: the
// disambiguated Shift+Enter inserts a newline and the turn is NOT sent, while the
// CR that follows it (a genuinely pressed Enter) IS.
func TestKeyProtocolShiftEnterDoesNotSubmit(t *testing.T) {
	steps := []scriptStep{
		{data: []byte("ab")},
		{data: []byte("\x1b[13;2u")}, // Shift+Enter, kitty disambiguation
		{data: []byte("cd")},
		{data: []byte("\r")}, // Enter
	}
	// The gap lands BEFORE the submitting Enter (step index 3): the preceding
	// three keys settle first, so the \r is its own event, not a burst tail.
	res, buf := runKeyProtocolScript(t, steps, []int{3})
	if res.EOF || res.Interrupted {
		t.Fatalf("Run returned EOF=%v interrupted=%v, want a submitted line", res.EOF, res.Interrupted)
	}
	if !strings.Contains(buf, "\n") {
		t.Fatalf("the buffer holds %q — Shift+Enter did not insert a newline", buf)
	}
	if strings.Contains(res.Line, "\x1b") {
		t.Fatalf("the submitted line carries the raw sequence: %q", res.Line)
	}
	if !strings.Contains(res.Line, "ab") || !strings.Contains(res.Line, "cd") {
		t.Fatalf("the submitted line is %q, want both halves of the message", res.Line)
	}
}

// TestKeyProtocolAltEnterDoesNotSubmit pins the FALLBACK the chat layer advises
// on a terminal that answered nothing: Alt+Enter (`ESC CR`, the two-byte form
// decodeEscape already maps to KeyNewline) inserts a newline and does not submit.
// If this case ever fails, the advice printed to the operator is a lie.
func TestKeyProtocolAltEnterDoesNotSubmit(t *testing.T) {
	steps := []scriptStep{
		{data: []byte("ab")},
		{data: []byte("\x1b\r")}, // Alt+Enter
		{data: []byte("cd")},
		{data: []byte("\r")},
	}
	res, buf := runKeyProtocolScript(t, steps, []int{3})
	if res.EOF || res.Interrupted {
		t.Fatalf("Run returned EOF=%v interrupted=%v, want a submitted line", res.EOF, res.Interrupted)
	}
	if !strings.Contains(buf, "\n") {
		t.Fatalf("the buffer holds %q — Alt+Enter did not insert a newline", buf)
	}
	if !strings.Contains(res.Line, "ab") || !strings.Contains(res.Line, "cd") {
		t.Fatalf("the submitted line is %q, want both halves of the message", res.Line)
	}
}

// TestKeyProtocolBareEnterStillSubmits is the regression guard in the other
// direction: the negotiation must not have changed what a PLAIN Enter does. A
// terminal that got `CSI > 1 u` still sends 0x0d for Enter (the protocol keeps
// it deliberately), and it must still submit — immediately and on its own.
func TestKeyProtocolBareEnterStillSubmits(t *testing.T) {
	steps := []scriptStep{{data: []byte("hi")}, {data: []byte("\r")}}
	res, _ := runKeyProtocolScript(t, steps, []int{1})
	if res.EOF || res.Interrupted {
		t.Fatalf("Run returned EOF=%v interrupted=%v, want a submitted line", res.EOF, res.Interrupted)
	}
	if strings.TrimRight(res.Line, "\n") != "hi" {
		t.Fatalf("the submitted line is %q, want %q", res.Line, "hi")
	}
}

// TestKeyProtocolPrefetchBytesReachTheDecoder pins the ORDERING guarantee at the
// seam where it is consumed: a Terminal whose prefetch holds bytes hands them to
// the reader FIRST, whole, and only then reads the descriptor. The bytes are the
// operator's own — typed while the probe was parked — so losing or reordering one
// is losing a keystroke.
//
// This case is hermetic (no pty): it measures the QUEUE's contract with the read
// path, which is the part the pty case cannot isolate from the kernel's buffer.
func TestKeyProtocolPrefetchBytesReachTheDecoder(t *testing.T) {
	typed := []byte("\x1b[13;2u") // a key sequence, not a reply: what the probe must hand on
	// keyProtocolNoDescriptor is the fd these hermetic terminals carry: a value
	// below fdMinValid, i.e. "there is no descriptor to poll". Without it this
	// case would poll the TEST PROCESS's stdin (fd 0), which is exactly the kind
	// of accidental coupling a queue-only assertion must not have.
	const keyProtocolNoDescriptor = -1
	t2 := &Terminal{prefetch: append([]byte(nil), typed...), inputFD: keyProtocolNoDescriptor}
	buf := make([]byte, readBufBytes)
	ev, err := t2.ReadEvent(buf, 0)
	if err != nil {
		t.Fatalf("ReadEvent: %v", err)
	}
	if ev.Kind != EventInput || ev.Bytes != len(typed) {
		t.Fatalf("ReadEvent = %+v, want all %d prefetched bytes as one input event", ev, len(typed))
	}
	if string(buf[:ev.Bytes]) != string(typed) {
		t.Fatalf("prefetched bytes came back as %q, want %q", buf[:ev.Bytes], typed)
	}
	if len(t2.PrefetchedBytes()) != 0 {
		t.Fatalf("the queue still holds %q after the read", t2.PrefetchedBytes())
	}
	if t2.InputPending() {
		t.Fatal("InputPending() is true with an empty queue and no descriptor — it must report what is really pending")
	}

	// A prefix of the queue is delivered in FULL: the read path never splits it
	// in a way that would let a later byte overtake an earlier one.
	t3 := &Terminal{prefetch: append([]byte(nil), typed...), inputFD: keyProtocolNoDescriptor}
	small := make([]byte, 1)
	ev, err = t3.ReadEvent(small, 0)
	if err != nil {
		t.Fatalf("ReadEvent (small buffer): %v", err)
	}
	if ev.Bytes != len(small) || small[0] != typed[0] {
		t.Fatalf("ReadEvent with a %d-byte buffer returned %d byte(s) %q", len(small), ev.Bytes, small[:ev.Bytes])
	}
	if got := t3.PrefetchedBytes(); string(got) != string(typed[1:]) {
		t.Fatalf("after a 1-byte read the queue holds %q, want %q", got, typed[1:])
	}
}

// that must not be written. It is a bound on a write that either happened or did
// not — not a duration any assertion depends on.
