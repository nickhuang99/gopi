package lineedit

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// paste_burst_origin_test.go — the EDITOR-level half of the paste-burst fix: the
// read's ORIGIN participates in the classification, and a suppressed Enter is
// ANNOUNCED once per burst instead of vanishing (Rule 15).
//
// The Value decision table (value_test.go's A3 arms) pins the policy. What it
// cannot pin is the wiring: whether the origin the editor stamps onto a read is
// the origin Value is asked with, and whether the warning is emitted through the
// seam an operator can actually see. Both are asserted here, through applyRead —
// the read loop's own decode pass — so a future edit that reverts either half
// fails a test rather than a user's next Enter.

// pasteBurstWarnMarker is the prefix every editor warning carries (the warn
// seam's own spelling, warnInputLocked's shape). Named so the assertions read as
// "the operator was told", not as an escaped bracket soup.
const pasteBurstWarnMarker = "[gopi] editor: "

// The two blocks the cases deliver through the REAL read path (applyRead), named
// so the buffer assertions state what was typed rather than re-spelling it.
const (
	// typedFastKeys is what four one-key reads insert: the text a fast typist
	// produced, which must be submitted as one line.
	typedFastKeys = "aaaa"
	// pastedBlock is what ONE read of four keys inserts: one paste's text.
	pastedBlock = "abcd"
)

// newWarningEditor builds an editor whose warnings land in a buffer the test can
// read. The buffer is the errOut seam NewChatEditor installs in production, so
// this asserts the SAME sink an operator's terminal sees.
func newWarningEditor(t *testing.T) (*Editor, *fakeClock, *bytes.Buffer) {
	t.Helper()
	ed, _, clock, _ := newScriptedEditor(t, nil, 80, nil)
	var warns bytes.Buffer
	ed.errOut = &warns
	return ed, clock, &warns
}

// suppressedEnters counts the warning lines in buf. A count (not a substring
// check) is what makes "ONCE per burst" assertable: a regression that printed the
// line per swallowed newline would still contain the marker.
func suppressedEnters(buf *bytes.Buffer) int {
	return strings.Count(buf.String(), pasteBurstWarnMarker)
}

// TestPasteBurstRequiresOneBigRead is the defect's regression guard at the editor
// level: four keys arriving 8 ms apart (a fast human, an IME commit, a key
// repeat) used to ACCUMULATE into a burst across reads, and the Enter after them
// became a newline with no explanation. Now the unit is ONE read, so those four
// reads are typing and the Enter submits.
func TestPasteBurstRequiresOneBigRead(t *testing.T) {
	ed, clock, warns := newWarningEditor(t)

	// Four separate reads of ONE key each, spaced by the intra-burst interval —
	// the exact cadence that used to arm the burst.
	for i := 0; i < 4; i++ {
		ed.applyRead([]byte("a"))
		clock.advance(PasteBurstCharInterval)
	}
	if ed.val.BurstActive() {
		t.Fatal("four one-key reads at PasteBurstCharInterval armed the burst — fast typing is still being read as a paste")
	}
	if _, done, err := ed.handleKey(Key{Code: KeyEnter, Origin: ReadOriginTyped}); err != nil || !done {
		t.Fatalf("the Enter after four fast keys did not submit: done=%v err=%v (done=false means it was read as pasted content)", done, err)
	}
	if got := ed.val.Text(); got != typedFastKeys {
		t.Fatalf("the buffer holds %q, want %q — the Enter submitted, so it must not have inserted a newline", got, typedFastKeys)
	}
	if n := suppressedEnters(warns); n != 0 {
		t.Fatalf("%d paste warning(s) printed over a burst that never happened: %q", n, warns.String())
	}
}

// TestPasteBurstSuppressedEnterIsAnnouncedOnce pins both halves of the honest
// failure: ONE read big enough IS a paste (so the multi-line-block property
// survives), and the swallowed Enter says so — once per burst, not once per
// newline.
func TestPasteBurstSuppressedEnterIsAnnouncedOnce(t *testing.T) {
	ed, clock, warns := newWarningEditor(t)

	// ONE read delivering four keys: a real paste.
	ed.applyRead([]byte("abcd"))
	if !ed.val.BurstActive() {
		t.Fatalf("ONE read of 4 keys did not arm the burst — a pasted multiline block would submit per line")
	}

	// Two Enters inside the window: both are content, one warning.
	for i := 0; i < 2; i++ {
		if _, done, err := ed.handleKey(Key{Code: KeyEnter, Origin: ReadOriginTyped}); err != nil || done {
			t.Fatalf("Enter %d inside the paste submitted the line: done=%v err=%v", i+1, done, err)
		}
		clock.advance(time.Millisecond)
	}
	if got := ed.val.Text(); got != pastedBlock+"\n\n" {
		t.Fatalf("the buffer holds %q, want %q — the two suppressed newlines kept as content", got, pastedBlock+"\n\n")
	}
	if n := suppressedEnters(warns); n != 1 {
		t.Fatalf("%d warning(s) for ONE burst, want exactly 1 — the throttle is what keeps a pasted block from printing a line per newline: %q", n, warns.String())
	}
	// The line must NAME the suppression, WHICH window applied and how many were
	// dropped, or it is a message the reader cannot act on. The window named is
	// the one in force for THIS burst's evidence: this read carried no
	// bracketed-paste marker, so the burst was armed by its SIZE and is judged by
	// PasteBurstCharInterval (Value.burstWindow) — printing 120 ms here would be a
	// false statement about the read that just happened.
	//
	// THE REMEDY HALF IS GONE ON PURPOSE (2026-11-10): "press Enter again once
	// the window has passed to send" was 60 of the sentence's ~300 cells, and at
	// 101 columns the whole sentence was three rows written over the layout's
	// status row. What the operator cannot derive on their own stays; the advice
	// does not (the newline is already in the buffer and the next Enter sends).
	// The width contract itself is asserted in degradation_width_test.go.
	msg := warns.String()
	for _, want := range []string{"Enter kept as newline", PasteBurstCharInterval.String(), "suppressed"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the warning does not carry %q — a reader cannot act on it: %q", want, msg)
		}
	}

	// The burst ENDS at one of two events, and the throttle releases with it: a
	// read that is not a paste, or an Enter that submits. Here it is the submit:
	// past the window the same key sends the line, so the paste is over.
	clock.advance(PasteEnterSuppressWindow + time.Millisecond)
	if _, done, err := ed.handleKey(Key{Code: KeyEnter, Origin: ReadOriginTyped}); err != nil || !done {
		t.Fatalf("an Enter past the window did not submit: done=%v err=%v", done, err)
	}
	if n := suppressedEnters(warns); n != 1 {
		t.Fatalf("the submitting Enter printed a warning: %d line(s): %q", n, warns.String())
	}

	// A second paste is a NEW burst and therefore news again — the throttle is
	// per burst, not per session, or an operator would stop being told after the
	// first one.
	ed.applyRead([]byte("efgh"))
	if _, done, err := ed.handleKey(Key{Code: KeyEnter, Origin: ReadOriginTyped}); err != nil || done {
		t.Fatalf("the Enter inside the second paste submitted (or failed) the line: done=%v err=%v", done, err)
	}
	if n := suppressedEnters(warns); n != 2 {
		t.Fatalf("%d warning(s) after a second burst, want 2 — the throttle must release when the burst ends: %q", n, warns.String())
	}
}

// TestInjectedEnterIsNeverSwallowed is the ORIGIN half: a script replaying a
// turn, a tool driving the surface and a test all want their keys applied, and
// none of them is a person pasting onto this terminal. An injected read never
// arms the suppression, and an injected Enter submits even inside a window a
// real paste opened.
func TestInjectedEnterIsNeverSwallowed(t *testing.T) {
	ed, _, _ := newWarningEditor(t)
	// An injected read of four keys: NOT a paste.
	ed.val.NoteKeys(4, ReadOriginInjected, ed.now())
	if ed.val.BurstActive() {
		t.Fatal("an INJECTED read of 4 keys armed the burst — a tool's own keys would then be swallowed")
	}
	if _, done, err := ed.handleKey(Key{Code: KeyEnter, Origin: ReadOriginInjected}); err != nil || !done {
		t.Fatalf("an injected Enter did not submit: done=%v err=%v", done, err)
	}

	// And a real paste does not capture a later injected Enter either.
	ed2, _, _ := newWarningEditor(t)
	ed2.applyRead([]byte("abcd")) // arms the burst
	if !ed2.val.BurstActive() {
		t.Fatal("ONE read of 4 keys did not arm the burst")
	}
	if _, done, err := ed2.handleKey(Key{Code: KeyEnter, Origin: ReadOriginInjected}); err != nil || !done {
		t.Fatalf("an injected Enter arriving inside a paste's window was swallowed: done=%v err=%v", done, err)
	}
}
