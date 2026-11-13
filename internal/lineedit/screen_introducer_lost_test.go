package lineedit

// screen_introducer_lost_test.go — a control sequence's INTRODUCER must never be
// lost while its parameters survive, because the parameters then paint as cells.
//
// THE CLASS THIS PINS. Every reader of the paint channel agrees on one rule
// (internal/sgr/sgr.go's EscapeSeqLen, rule 14): the bytes from ESC up to a
// sequence's final byte draw NO cell. That rule has a precondition nobody
// states — the ESC has to still be there when the bytes are interpreted. When
// the interpreter's own carry policy drops the INTRODUCER (`ESC[`) and keeps
// what follows, the parameters stop being a sequence and become ordinary
// printable characters, and they land on the grid as text:
//
//	`;61H24;73H5;230m`
//
// That is the observed fragment's exact shape (task 01a0e81b): a run of CSI
// PARAMETERS with the leading `ESC[` gone — a `;`-led run, which no producer
// ever writes as prose. The two policies below each drop the introducer, and
// each is reachable from the dump sink's own call pattern
// (internal/uiclient/dump.go feeds the Screen one Write at a time, and a Write
// boundary is the OS's choice, never the producer's):
//
//	1. Screen.Resize cleared the held tail, so a `ESC[` that had arrived alone
//	   was discarded while the sequence's parameters arrived in the next write;
//	2. holdPending's bound kept the LAST maxPendingEscape bytes of an
//	   over-long tail — i.e. dropped the FRONT, which is exactly where the
//	   introducer is.
//
// Both are fixed in the same commit by one policy: the parser may hold a
// partial sequence or discard it WHOLE, and it must never keep a fragment whose
// introducer it has thrown away. What the user (and the agent reading the dump
// back) then sees is either the whole sequence consumed or nothing at all —
// never its parameters as text.

import (
	"strings"
	"testing"
)

// introProbeCols/Rows is a small grid: the criterion is about which runes become
// CELLS, so the geometry only has to be wide enough to hold the parameters.
const (
	introProbeCols = 40
	introProbeRows = 4
)

// introCUP is the cursor placement a band paints its own rows with, spelled
// the way the producer spells it (sgr.CursorSeq). Its first parameter is
// OMITTED, which is the CSI convention for "row 1" — and it is why the observed
// fragment begins with a bare `;`.
const introCUP = "\x1b[;61H"

// introCUP2 and introSGR complete the observed fragment: a second placement
// (row 24, col 73 — the band's own editing row) and an SGR. Their concatenation
// minus the introducers is the literal cell run `;61H24;73H5;230m`.
const (
	introCUP2 = "\x1b[24;73H"
	introSGR  = "\x1b[38;5;230m"
)

// introFragment is the parameter run that must never reach a cell: what the
// three sequences above look like once every `ESC[` is gone.
const introFragment = ";61H24;73H5;230m"

// TestScreenResizeKeepsAHeldSequenceIntroducer pins defect 1. A read boundary
// can leave the parser holding a bare `ESC[`; a Resize between that read and the
// next one must not turn the sequence's parameters into text.
//
// THE OBSERVABLE ASSERTS THE PROPERTY, not a policy: after the parameters arrive
// the grid must show NO cell of them, whichever branch the interpreter took
// (holding the introducer applies the sequence — a cursor placement draws
// nothing; discarding it whole paints nothing either).
func TestScreenResizeKeepsAHeldSequenceIntroducer(t *testing.T) {
	// The grid starts one column wider, so the Resize in the middle is a REAL
	// one: a same-size Resize is a documented no-op and would assert nothing.
	s := NewScreen(introProbeCols+1, introProbeRows)
	// The write boundary falls between the introducer and the parameters —
	// the OS's choice, not the producer's.
	s.Feed([]byte("\x1b["))
	// A re-measure lands between the two reads (the dump sink resizes the grid
	// when the layout publishes its geometry).
	s.Resize(introProbeCols, introProbeRows)
	s.Feed([]byte(";61H"))
	s.Feed([]byte(introCUP2))
	s.Feed([]byte(introSGR))

	got := s.Contents()
	if strings.Contains(got, introFragment) || strings.Contains(got, ";61H") {
		t.Fatalf("the grid carries a CSI PARAMETER run as literal cells: %q — the parser dropped the sequence's introducer (`ESC[`) and painted its parameters as text; that is the observed `%s` fragment's shape",
			got, introFragment)
	}
}

// TestScreenFeedNeverPaintsAnOverlongHeldSequenceAsText pins defect 2: the carry
// bound. A stream that never completes its sequence must not grow the parser's
// state without limit — but bounding it by keeping the LAST maxPendingEscape
// bytes throws away the introducer and re-offers the rest as ordinary
// characters, so a hostile or desynchronized stream paints its own payload.
//
// THE CRITERION: nothing of the held sequence may reach a cell. Dropping it
// whole, or holding it whole, both satisfy that; keeping a headless fragment
// does not.
func TestScreenFeedNeverPaintsAnOverlongHeldSequenceAsText(t *testing.T) {
	s := NewScreen(introProbeCols, introProbeRows)
	// An unterminated OSC — the shape that makes a reader hold rather than
	// interpret (a real bubbletea program asks for the background colour with
	// `ESC]11;?`) — carrying far more payload than the bound allows.
	payload := strings.Repeat("SEMI;9", maxPendingEscape)
	s.Feed([]byte("\x1b]11;?" + payload))
	if len(s.pending) > maxPendingEscape {
		t.Fatalf("carry buffer grew to %d bytes, bound is %d", len(s.pending), maxPendingEscape)
	}
	// The stream resynchronizes: a following write carries ordinary text.
	s.Feed([]byte("AFTER"))

	for row := 0; row < introProbeRows; row++ {
		line := s.Line(row)
		if strings.Contains(line, "SEMI;9") || strings.Contains(line, "SEMI") {
			t.Fatalf("grid row %d carries the held sequence's own payload as cells (%q): the carry bound dropped the introducer and kept the body, so the parameters painted as text",
				row, line)
		}
	}
}
