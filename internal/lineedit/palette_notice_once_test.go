package lineedit

// palette_notice_once_test.go — the downgrade notice is ONE SENTENCE PER RUN,
// and this file pins the mechanism that makes that possible: the mapping can be
// asked for its sentence WITHOUT it being said.
//
// WHY A SEPARATE FILE, and why it is a mechanism test rather than another
// end-to-end count. A producer decides a run in STAGES: `gopi chat` resolves the
// environment's declaration, builds its input layer on that table, and then
// re-derives it from what the terminal answers — and only the LAST of those is
// the run's decision. While the mapping announced from inside (the shape this
// file's subject replaced), every stage spoke, so ONE decision reached the
// operator three times and two of the three sentences described a table no
// pixel was drawn with (measured 2026-10-04, task 01a10e2b, master 2f...: three
// notices, screen palette256). The remedy is that a stage can HOLD a sentence
// and drop it when a later stage overrules the table it is about — which is only
// possible if the mapping is divisible. This file asserts the division: the
// noticed form writes nothing, the announcing form still writes (Rule 15: the
// notice may not be dropped), and the two agree on the sentence, so a caller
// cannot hold one wording and print another.

import (
	"strings"
	"testing"
)

// TestPaletteForCapabilityNoticedHoldsTheSentenceAndWritesNothing pins the
// split itself: resolving with the noticed form produces the table and the
// sentence with ZERO bytes written, and the sentence is the same one the
// announcing form writes — read off the wire, not compared against a second
// construction of the text.
func TestPaletteForCapabilityNoticedHoldsTheSentenceAndWritesNothing(t *testing.T) {
	master := paletteOpenPty(t)
	defer func() { _ = master.Close() }() //stylegate:ignore closing the pty master is teardown, not a step whose failure matters
	tty := paletteSlaveFile(t, master)

	t.Setenv(EnvTerm, term256)
	t.Setenv(EnvColorTerm, "")
	t.Setenv(EnvPalette, PaletteNameHermes)
	declared := CapabilityForEnv(false, tty)

	var held Palette
	var notice string
	quiet := captureStderr(t, func() {
		held, notice = PaletteForCapabilityNoticed(declared)
	})
	if quiet != "" {
		t.Fatalf("the noticed form wrote %q — a producer that decides in stages must be able to resolve "+
			"a stage and keep the sentence unspoken (the run's sentence belongs to its FINAL decision)", quiet)
	}
	if held != Palette256 {
		t.Fatalf("the noticed form returned %+v, want Palette256 (the deepest table this terminal can draw)", held)
	}
	if notice == "" {
		t.Fatal("the noticed form returned NO sentence for a request the terminal cannot draw — the " +
			"silent collapse this epic removed is back in the half that decides the wording")
	}

	// The announcing form, on the same capability: exactly the sentence the
	// caller was holding, and exactly one line of it. One wording, one line —
	// otherwise a producer's "hold it, then say it" pair would say two different
	// things.
	var drew Palette
	said := captureStderr(t, func() {
		drew = PaletteForCapability(declared)
	})
	if drew != held {
		t.Fatalf("the announcing form drew %+v, want %+v — the two forms of ONE mapping disagree", drew, held)
	}
	if said != notice {
		t.Fatalf("the announcing form said\n%q\nbut the noticed form held\n%q\nthe sentence a caller holds and the "+
			"sentence a resolution returns must be one spelling of one downgrade (Rule 13)", said, notice)
	}
	if n := strings.Count(strings.TrimRight(said, "\n"), "\n"); n != 0 {
		t.Errorf("the notice spans %d lines, want exactly 1:\n%q", n+1, said)
	}
	if strings.Count(said, downgradeMark) != 1 {
		t.Errorf("one resolution produced %d notices, want exactly 1:\n%q", strings.Count(said, downgradeMark), said)
	}
}

// downgradeMark is the substring that identifies the notice on the wire. It is
// spelled here (and named) so the count is against the WIRE wording, not against
// a second call to the function that wrote it: a notice whose wording drifted
// away from this mark then fails LOUDLY instead of counting nothing. The
// testsuite's end-to-end case carries the same mark for the same reason.
const downgradeMark = "not drawable here"

// TestPaletteDowngradeNoticeNeverWrites pins the third entry point as a PURE
// query. It is what a caller that must hold the DECLARATION's sentence uses when
// it has no table to resolve yet, so it must be answerable without drawing
// anything (and without a table): the same capability answered twice yields the
// same sentence, and nothing reaches stderr either time.
func TestPaletteDowngradeNoticeNeverWrites(t *testing.T) {
	master := paletteOpenPty(t)
	defer func() { _ = master.Close() }() //stylegate:ignore closing the pty master is teardown, not a step whose failure matters
	tty := paletteSlaveFile(t, master)

	t.Setenv(EnvTerm, term256)
	t.Setenv(EnvColorTerm, "")
	t.Setenv(EnvPalette, PaletteNameHermes)

	var first, second string
	quiet := captureStderr(t, func() {
		first = PaletteDowngradeNotice(CapabilityForEnv(false, tty))
		second = PaletteDowngradeNotice(CapabilityForEnv(false, tty))
	})
	if quiet != "" {
		t.Fatalf("PaletteDowngradeNotice wrote %q — a producer asking what sentence it OWES must not have it said "+
			"on the way out (it says it once, at its finish)", quiet)
	}
	if first == "" {
		t.Fatal("a downgrade the terminal cannot draw produced no sentence")
	}
	if first != second {
		t.Fatalf("two identical capabilities produced different sentences:\n%q\n%q", first, second)
	}
	if !strings.Contains(first, downgradeMark) {
		t.Fatalf("the sentence is not the downgrade notice (no %q):\n%q", downgradeMark, first)
	}

	// ...and the shapes that owe NOTHING owe nothing here too: a request the
	// terminal can draw, and an unset knob (nobody asked for the table they are
	// not getting). A query that invented a sentence for these would make every
	// run print one.
	for _, tc := range []struct {
		name    string
		term    string
		palette string
	}{
		{"a drawable request", term256, PaletteNamePalette256},
		{"an unset knob follows the terminal", term256, PaletteNameUnset},
		{"the floor on the floor", "xterm", PaletteNameBasic},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvTerm, tc.term)
			t.Setenv(EnvPalette, tc.palette)
			if got := PaletteDowngradeNotice(CapabilityForEnv(false, tty)); got != "" {
				t.Fatalf("a run that got the table it asked for owes a sentence: %q", got)
			}
		})
	}
}
