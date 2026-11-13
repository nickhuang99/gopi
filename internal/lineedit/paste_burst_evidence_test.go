package lineedit

import (
	"strings"
	"testing"
	"time"
)

// paste_burst_evidence_test.go — THE REGRESSION GUARD for the defect this file's
// commit closes: the Enter suppression window is chosen from the EVIDENCE the
// read carried, not from one flat number.
//
// THE DEFECT (measured here, from the value decision outward): NoteKeys armed the
// burst on ANY typed read carrying PasteBurstMinChars (3) keys, and
// suppressEnterAt then swallowed every Enter arriving within
// PasteEnterSuppressWindow (120 ms). A terminal is free to COALESCE a few typed
// keys — or to deliver a short block with no newline in it — into one read, and
// nothing then distinguishes it from a paste. So an operator who typed three
// characters that arrived as one read and pressed Enter 50 ms later got a
// literal newline in the buffer instead of a submitted line, with only a warning
// to explain it.
//
// THE FIX, at the same two seams: a burst the terminal FRAMED (CSI 200 ~ / 201 ~,
// which the decoder already reports as KeyPasteStart / KeyPasteEnd) keeps
// PasteEnterSuppressWindow — the terminal said "paste", and a paste's bytes are
// all in flight at once. A burst armed only by read SIZE has no such statement
// behind it, so it is judged by PasteBurstCharInterval — the gap this package
// already defines as "the same arrival" — while an Enter arriving later than that
// is a keypress.
//
// NOTHING HERE TUNES A NUMBER (Rule 10.3): PasteEnterSuppressWindow is untouched
// at 120 ms; the two named windows are SCOPE-selected, and no third value is
// introduced (Rule 1/13/14).

const (
	// sizeArmedEnterGap is the Enter's delay after a SIZE-armed read: past the
	// "same arrival" interval, well inside the 120 ms paste window. It is the
	// measured human gap the defect was reported at — a reader who presses Enter
	// after the characters have already landed — expressed as a value in this
	// package's own terms rather than as a raw literal at each use.
	sizeArmedEnterGap = PasteBurstCharInterval + 42*time.Millisecond
	// framedEnterGap is the same delay for a FRAMED burst: inside both windows,
	// so it must keep being suppressed whatever the evidence rule does.
	framedEnterGap = PasteEnterSuppressWindow / 2
)

// TestPasteBurstSizeArmedEnterFiftyMillisecondsLaterSubmits is THE regression
// test. It fails on the pre-change tree (the Enter 50 ms after a three-key read
// was swallowed and the line never submitted) and passes after.
//
// It asserts BOTH halves of the criterion on the same run, because a fix that
// only stopped swallowing Enters would recreate the defect 120 ms was there for
// ("pasted N lines = N turns"): the size-armed burst must be narrow, and the
// framed burst must still be wide.
func TestPasteBurstSizeArmedEnterFiftyMillisecondsLaterSubmits(t *testing.T) {
	base := time.Unix(editorTestEpoch, 0)

	t.Run("value: three keys in one read, Enter 50ms later submits", func(t *testing.T) {
		v := NewValue(0, 0)
		v.NoteKeys(PasteBurstMinChars, ReadOriginTyped, base)
		if !v.BurstActive() {
			t.Fatalf("ONE typed read of %d keys did not arm the burst", PasteBurstMinChars)
		}
		if v.BurstFramed() {
			t.Fatal("a burst armed only by read SIZE reported itself as framed — a terminal that coalesced typed keys would then keep the 120 ms window")
		}
		if v.Enter(ReadOriginTyped, base.Add(sizeArmedEnterGap)) {
			t.Fatalf("an Enter %v after a SIZE-armed read was read as pasted content; the operator pressed Enter on a line they had finished typing and it must submit (window in force: %v, the size-armed one)", sizeArmedEnterGap, v.burstWindow())
		}
		if got := v.Text(); got != "" {
			t.Fatalf("text = %q, want no newline in the buffer — the Enter submitted, so nothing was inserted", got)
		}
	})

	t.Run("value: a framed burst 50ms later is still content", func(t *testing.T) {
		v := NewValue(0, 0)
		v.NoteBurst(PasteBurstMinChars, ReadOriginTyped, base) // the terminal bracketed it
		if !v.BurstActive() || !v.BurstFramed() {
			t.Fatalf("a framed burst did not record itself as framed (active=%v framed=%v)", v.BurstActive(), v.BurstFramed())
		}
		if got := v.burstWindow(); got != PasteEnterSuppressWindow {
			t.Fatalf("a framed burst's window is %v, want PasteEnterSuppressWindow (%v) — a paste's bytes are all in flight at once, and narrowing this window is what would turn a pasted block back into N turns", got, PasteEnterSuppressWindow)
		}
		if !v.Enter(ReadOriginTyped, base.Add(sizeArmedEnterGap)) {
			t.Fatalf("an Enter %v after a FRAMED paste submitted the line — a CR inside a bracketed paste is content", sizeArmedEnterGap)
		}
		if got := v.Text(); got != "\n" {
			t.Fatalf("text = %q, want the pasted newline kept in the buffer", got)
		}
	})

	t.Run("value: a size-armed burst still catches its own tail", func(t *testing.T) {
		// The reason the narrow window is not ZERO: a paste the terminal did not
		// frame still has a trailing CR delivered back to back with its bytes,
		// and that newline belongs to the paste.
		v := NewValue(0, 0)
		v.NoteKeys(PasteBurstMinChars, ReadOriginTyped, base)
		if !v.Enter(ReadOriginTyped, base.Add(PasteBurstCharInterval)) {
			t.Fatal("an Enter arriving WITHIN PasteBurstCharInterval of a size-armed read was submitted — the paste's own trailing CR must stay content")
		}
	})

	t.Run("editor: the same read through the real decode path", func(t *testing.T) {
		ed, clock, warns := newWarningEditor(t)

		// ONE read of three typed keys — a terminal coalescing a fast typist, a
		// slow link, a key repeat. applyRead is the read loop's own decode pass,
		// so this is the production path and not a hand-set Value.
		ed.applyRead([]byte("abc"))
		if !ed.val.BurstActive() {
			t.Fatalf("ONE read of %d keys did not arm the burst", PasteBurstMinChars)
		}
		if ed.val.BurstFramed() {
			t.Fatal("a read with no bracketed-paste markers reported itself as framed")
		}

		clock.advance(sizeArmedEnterGap)
		if _, done, err := ed.handleKey(Key{Code: KeyEnter, Origin: ReadOriginTyped}); err != nil || !done {
			t.Fatalf("the Enter %v after the read did not submit: done=%v err=%v — the line never goes out", sizeArmedEnterGap, done, err)
		}
		if got, want := ed.val.Text(), "abc"; got != want {
			t.Fatalf("the buffer holds %q, want %q — the Enter submitted, so it must not have inserted a newline", got, want)
		}
		if n := suppressedEnters(warns); n != 0 {
			t.Fatalf("%d paste warning(s) printed for an Enter that submitted — there was nothing to warn about: %q", n, warns.String())
		}
	})
}

// TestPasteBurstTerminalFramingKeepsTheFullWindow is the OTHER half, at the
// editor seam: the terminal's own bracketed-paste markers (CSI 200 ~ / 201 ~) are
// what select PasteEnterSuppressWindow, and they select it on BOTH delivery
// paths — the marker the DECODER sees, and the marker the envelope path
// recognises. It also pins the coverage the size rule never had: a framed block
// SHORTER than PasteBurstMinChars, and one that carries no newline at all, is a
// paste the terminal stated, so its following Enter is still content.
func TestPasteBurstTerminalFramingKeepsTheFullWindow(t *testing.T) {
	start, end := string(PasteStartSeq), string(PasteEndSeq)

	t.Run("one read carrying the framing and a short block", func(t *testing.T) {
		ed, clock, warns := newWarningEditor(t)
		// Two keys inside the markers: BELOW PasteBurstMinChars, so the size
		// rule alone would not arm anything — the framing is the only evidence.
		ed.applyRead([]byte(start + "ab" + end))
		if !ed.val.BurstActive() {
			t.Fatal("a bracketed block did not arm the burst — the terminal stated it was a paste and nothing recorded it")
		}
		if !ed.val.BurstFramed() {
			t.Fatal("a read carrying CSI 200 ~ / 201 ~ did not mark the burst as framed")
		}
		clock.advance(framedEnterGap)
		if _, done, err := ed.handleKey(Key{Code: KeyEnter, Origin: ReadOriginTyped}); err != nil || done {
			t.Fatalf("the Enter %v after a bracketed paste submitted the line: done=%v err=%v", framedEnterGap, done, err)
		}
		if got, want := ed.val.Text(), "ab\n"; got != want {
			t.Fatalf("the buffer holds %q, want %q — the suppressed newline kept as content", got, want)
		}
		// The operator is still told, and told WHICH evidence did it: a message
		// that named the size rule here would describe a read that only carried
		// two keys and never tripped it.
		if n := suppressedEnters(warns); n != 1 {
			t.Fatalf("%d warning(s) for the suppressed Enter, want exactly 1: %q", n, warns.String())
		}
		msg := warns.String()
		if !strings.Contains(msg, "bracketed") {
			t.Fatalf("the warning does not say the terminal bracketed the paste, so a reader cannot tell this from a size classification: %q", msg)
		}
		if !strings.Contains(msg, PasteEnterSuppressWindow.String()) {
			t.Fatalf("the warning does not name the window in force (%v): %q", PasteEnterSuppressWindow, msg)
		}
	})

	t.Run("the closing marker arriving in its own read", func(t *testing.T) {
		// A short paste whose END marker lands in a later read — the delivery
		// shape the envelope path exists for. The second read carries no keys at
		// all (only the marker), so a key-count rule cannot see it; the framing
		// still has to keep the window open for the Enter that follows.
		ed, clock, warns := newWarningEditor(t)
		ed.applyRead([]byte(start + "ab"))
		if !ed.val.BurstFramed() {
			t.Fatal("the opening read's marker did not mark the burst as framed")
		}
		clock.advance(PasteBurstCharInterval)
		ed.applyRead([]byte(end))
		clock.advance(framedEnterGap)
		if _, done, err := ed.handleKey(Key{Code: KeyEnter, Origin: ReadOriginTyped}); err != nil || done {
			t.Fatalf("the Enter after the closing marker submitted the line: done=%v err=%v — a framed paste whose end arrives separately lost its window", done, err)
		}
		if n := suppressedEnters(warns); n != 1 {
			t.Fatalf("%d warning(s), want exactly 1: %q", n, warns.String())
		}
	})

	t.Run("the size-armed window is named in its own words", func(t *testing.T) {
		// The warning is the ONLY explanation an operator gets, so the two
		// evidence kinds must not print the same sentence: "the window is
		// 120ms" after a 3-key coalesced read would be a false statement.
		ed, clock, warns := newWarningEditor(t)
		ed.applyRead([]byte("abc"))
		// Inside the narrow window: still content, so the warning fires.
		clock.advance(PasteBurstCharInterval)
		if _, done, err := ed.handleKey(Key{Code: KeyEnter, Origin: ReadOriginTyped}); err != nil || done {
			t.Fatalf("the Enter one interval after the read submitted: done=%v err=%v", done, err)
		}
		msg := warns.String()
		if !strings.Contains(msg, PasteBurstCharInterval.String()) {
			t.Fatalf("the warning does not name the window that actually applied (%v): %q", PasteBurstCharInterval, msg)
		}
		if strings.Contains(msg, "bracketed") {
			t.Fatalf("the warning claims the terminal bracketed a read that carried no marker: %q", msg)
		}
	})

	t.Run("an injected framed read is still not a paste", func(t *testing.T) {
		// Provenance still wins: an injector's bytes did not come from the
		// operator's terminal, so even markers inside them cannot arm a
		// suppression on it (ReadOriginInjected never arms — keysource.go).
		v := NewValue(0, 0)
		v.NoteBurst(PasteBurstMinChars, ReadOriginInjected, time.Unix(editorTestEpoch, 0))
		if v.BurstActive() {
			t.Fatal("an INJECTED framed read armed the burst — a tool's own keys would be judged as a paste")
		}
	})
}
