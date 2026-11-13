package uiclient

// dump_introducer_lost_test.go — the armed dump must never carry a control
// sequence's PARAMETERS as cells.
//
// THE OBSERVED DEFECT (task 01a0e81b, session 884eca28's armed grid). A dump row
// read, verbatim:
//
//	002|  行号/列偏移，并用 `see` 的 attrs 判断高亮 span 是从反引号;61H24;73H5;230m
//
// `;61H24;73H5;230m` is three placed sequences — a cursor placement with its
// first parameter omitted (`ESC[;61H`), a second placement (`ESC[24;73H`) and an
// SGR (`ESC[38;5;230m`) — whose every introducer (`ESC[`) is gone. What is left
// is a `;`-led run of CSI parameters, which no producer writes as prose, painted
// on the grid as text. The mechanism is the interpreter's carry policy: it kept
// the TAIL of a held sequence (the half without the introducer) instead of
// holding the sequence or discarding it whole, so the parameters stopped being a
// sequence and became printable characters (internal/lineedit/screen.go,
// holdPending; the deterministic repro is
// internal/lineedit/screen_introducer_lost_test.go).
//
// WHY THIS CASE IS AT THE SINK AND NOT ONLY AT THE INTERPRETER. The sink is the
// surface an agent actually reads back (`--dump-screen`), and it feeds the
// interpreter ONE Write at a time — which is exactly where a read boundary can
// fall inside a sequence. The criterion is the artifact's own TEXT, so it holds
// whichever branch the interpreter took.

import (
	"strings"
	"testing"
)

// dumpOverlongOSCPayloadBytes is how much payload the unterminated sequence
// carries. It only has to EXCEED the interpreter's carry bound (lineedit's
// maxPendingEscape, 512 bytes): the case is about what happens past the bound.
const dumpOverlongOSCPayloadBytes = 4096

// dumpParameterRun is the `;`-led parameter shape a lost introducer leaves
// behind. It is asserted by SHAPE, not by the observed fragment's digits: the
// digits are a function of the layout's cursor, while the `;`-led parameter run
// is the defect.
const dumpParameterRun = ";9SEMI"

// TestScreenDumperNeverPaintsAnOverlongSequenceAsCells drives the sink the way
// the paint stream does — one Write per read — with a sequence longer than the
// interpreter's carry bound, then reads the ARTIFACT back and asserts no cell of
// the sequence's own payload survived as text.
func TestScreenDumperNeverPaintsAnOverlongSequenceAsCells(t *testing.T) {
	d, path := newTestDumper(t, DumpModeANSI, 32, 4)

	// A real bubbletea program opens with an OSC query for the terminal's
	// background colour (`ESC]11;?`). Here it never terminates and carries far
	// more payload than the bound, so the interpreter must decide what to do
	// with the excess — and "keep the last 512 bytes of it" is what painted the
	// parameters.
	long := "\x1b]11;?" + strings.Repeat(dumpParameterRun, dumpOverlongOSCPayloadBytes/len(dumpParameterRun))
	if _, err := d.Write([]byte(long)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// The stream resynchronizes and the surface keeps painting.
	if _, err := d.Write([]byte("\x1b[1;1Hafter")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := d.Settle(); err != nil {
		t.Fatalf("Settle: %v", err)
	}

	text := readFile(t, path)
	if strings.Contains(text, dumpParameterRun) {
		row := ""
		for _, line := range strings.Split(text, "\n") {
			if strings.Contains(line, dumpParameterRun) {
				row = line
				break
			}
		}
		t.Fatalf("the artifact carries a control sequence's PARAMETERS as cells (%q): the interpreter kept a headless fragment of a held sequence, so the bytes stopped being a sequence and became printable text — the observed `;61H24;73H5;230m` row", row)
	}
}
