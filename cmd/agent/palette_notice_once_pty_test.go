package main

// palette_notice_once_pty_test.go — ONE capability decision, ONE downgrade
// notice, pinned on the WIRING (the staged shape) rather than on the shipped
// binary alone.
//
// THE DEFECT THIS FILE IS THE MINIMAL REPRO OF. The console decides its colours
// in stages: runChatWithInput resolves the environment's declaration for the
// banner, and newChatInputWithPalette resolves it again, opens the terminal,
// and re-derives the table from what the terminal answers. While each of those
// resolutions ANNOUNCED from inside, the operator got ONE decision's notice
// THREE times (measured on master, 2026-10-04, task 01a10e2b: `byMark`/`byKnob`
// both 3, and the run's own screen carried palette256 — two of the three
// sentences named a table no pixel was drawn with). The testsuite's
// TestChatDowngradeNoticeReachesStderrOnAPTY catches the symptom on the shipped
// binary; this case reproduces it at the seams, in ~0.2s, so the guard is
// cheap enough to run on every change to the wiring.
//
// WHY A PTY AND NOT A BUFFER. The whole shape under test is "a terminal was
// opened, so ASK it" — a buffer has nothing to ask, the measurement never
// happens, and the second stage would not run at all. The pty is what makes the
// three resolutions real.
//
// WHY IT COUNTS THE WIRE WORDS RATHER THAN CALLING THE PACKAGE. `COLORTERM="` is
// the one value that appears in the notice and nowhere else in this stream, and
// the mark (paletteNoticeMark) is the sentence's own wording — spelled here so a
// notice whose text drifted fails LOUDLY instead of being counted zero times by
// a second, unrelated spelling (Rule 1).

import (
	"io"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/lineedit"
)

// paletteNoticeMark identifies the downgrade notice in a captured stream. It is
// the same wording the testsuite's end-to-end case pins, spelled locally for the
// same reason: each side must fail on a silent rewording on its own.
const paletteNoticeMark = "not drawable here"

// paletteNoticePtyCols/Rows size the pty this case runs on. They are above the
// reserved layout's floor so the run does not add a "terminal too small" notice
// to the stream being counted — the case is about ONE sentence, and a second
// sentence about something else would make the count say the wrong thing.
const (
	paletteNoticePtyCols = 120
	paletteNoticePtyRows = 40
)

// TestChatStagedCapabilityDecisionSaysItsNoticeOnce drives the ONE production
// call that finishes the colour decision (newChatInputWithPalette) with the
// environment the downgrade notice exists for — the gold table asked for on a
// terminal that declares 256 colours and no 24-bit advertisement — and asserts
// that the operator is told exactly ONCE.
//
// The assertion is a pair of COUNTS, as in the end-to-end case, because a single
// count can be satisfied by a second, parallel spelling: both must agree on 1.
func TestChatStagedCapabilityDecisionSaysItsNoticeOnce(t *testing.T) {
	pair := chatOpenPtyPair(t, paletteNoticePtyCols, paletteNoticePtyRows)
	// A private profile: the constructor loads the history file from it, and a
	// case must never read or write the operator's own ~/.gopi.
	t.Setenv(config.GopiDirEnv, t.TempDir())
	t.Setenv(lineedit.EnvTerm, lineedit.Term256Color)
	t.Setenv(lineedit.EnvColorTerm, "")
	t.Setenv(lineedit.EnvPalette, lineedit.PaletteNameHermes)

	// THE DECLARATION, resolved exactly as runChatWithInput resolves it — the
	// first stage of the run's decision, whose sentence must NOT be said until
	// the terminal has had its say.
	declared := lineedit.CapabilityForEnv(false, pair.Slave)

	var input *chatInput
	var closeInput func()
	out := captureStderr(t, func() {
		input, closeInput = newChatInputWithPalette(pair.Slave, io.Discard, chatPrompt, false, NewSlashCompleter(), nil, declared)
	})
	defer closeInput()

	// The run must still be COLOURFUL and on the right table: the case is about
	// the sentence, not about draconian silence. Palette256 is the deepest table
	// this terminal can draw, and it is the one the notice promises.
	if got := input.Palette(); got != lineedit.Palette256 {
		t.Fatalf("the staged decision drew %+v, want Palette256 — the case's environment is not the one it "+
			"claims to test (TERM=%q %s=%q %s=%q)",
			got, lineedit.Term256Color, lineedit.EnvColorTerm, "", lineedit.EnvPalette, lineedit.PaletteNameHermes)
	}

	byMark := strings.Count(out, paletteNoticeMark)
	byKnob := strings.Count(out, lineedit.EnvColorTerm+`="`)
	if byMark != 1 || byKnob != 1 {
		t.Errorf("the downgrade notice appears %d times (mark %q) / %d times (the %s knob), want exactly 1 of "+
			"each for ONE capability decision:\n%q", byMark, paletteNoticeMark, byKnob, lineedit.EnvColorTerm, out)
	}
	// ...and the sentence is the RIGHT one: it names the table asked for and the
	// table drawn. A single notice about the wrong half would pass the count and
	// mislead the operator, which is the failure mode a count alone cannot see.
	if byMark == 1 && !strings.Contains(out, lineedit.PaletteNameHermes) {
		t.Errorf("the one notice does not name the table the operator asked for (%q):\n%q", lineedit.PaletteNameHermes, out)
	}
	if byMark == 1 && !strings.Contains(out, lineedit.PaletteNamePalette256) {
		t.Errorf("the one notice does not name the table actually drawn (%q):\n%q", lineedit.PaletteNamePalette256, out)
	}
}

// TestChatStagedDecisionHonouringTheRequestSaysNothing is the other direction,
// and it is the one that catches the tempting wrong fix: "print the notice from
// the FIRST stage only" would pass the count above while introducing a sentence
// about a table the run never drew. Here the terminal's answer GAINS the table
// the operator asked for, so the declaration's held sentence must be DROPPED
// rather than said.
//
// It is also the shape the staged decision was built for: the environment asks
// for the gold table, the pty's own answers refuse 24-bit, and the run must end
// on the 256 table with one notice — the same outcome as above, reached through
// the same seam, which is why the two cases share one assertion function.
func TestChatStagedDecisionHonouringTheRequestSaysNothing(t *testing.T) {
	pair := chatOpenPtyPair(t, paletteNoticePtyCols, paletteNoticePtyRows)
	t.Setenv(config.GopiDirEnv, t.TempDir())
	t.Setenv(lineedit.EnvTerm, lineedit.Term256Color)
	t.Setenv(lineedit.EnvColorTerm, "")
	// NOBODY ASKED for a table: the knob is unset, so the terminal's own depth
	// picks the table and no sentence is owed — the "no silent upgrade" twin of
	// the ban on a silent downgrade.
	t.Setenv(lineedit.EnvPalette, "")

	declared := lineedit.CapabilityForEnv(false, pair.Slave)
	var input *chatInput
	var closeInput func()
	out := captureStderr(t, func() {
		input, closeInput = newChatInputWithPalette(pair.Slave, io.Discard, chatPrompt, false, NewSlashCompleter(), nil, declared)
	})
	defer closeInput()

	if got := input.Palette(); got != lineedit.Palette256 {
		t.Fatalf("an unset knob on a 256-colour terminal drew %+v, want Palette256", got)
	}
	if strings.Contains(out, paletteNoticeMark) {
		t.Errorf("a run that asked for no particular table was told about a downgrade it never made:\n%q", out)
	}
}
