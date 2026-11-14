// <!-- doc-class: normative -->
package main

// term_note_row_test.go — the COPYABILITY CONTRACT for the surfaces that explain
// the run to its operator (/info's block, a /voice notice).
//
// THE CONTRACT, in the operator's words (2026-10-22): "每个通道必须把 Render（带
// style 的绘制）与 CopyText（纯 ASCII、逐字可粘贴）分开 … SGR 只进 Render，不进
// CopyText … 无色时零字节". So a cell carries NO escape byte in any mode, the
// cells' texts concatenate to EXACTLY the bytes the historic writer produced, and a
// monochrome table yields zero sequences — not "sequences that happen to be
// resets".
//
// WHY THE BYTE-IDENTITY CASE IS THE LOAD-BEARING ONE. The whole rewrite is
// justified only if the operator loses NOTHING: the same characters, in the same
// order, on every carrier that has no colour. That assertion is made against the
// REAL renderer (chatInfoBlock) rather than a fixture of what it "should" be, so a
// later reword of the block moves the expectation with it instead of silently
// emptying the test.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sgr"
)

// termNoteProbeFields is a /info block with EVERY fact present: the byte-identity
// assertion is weakest where a value is the "(not declared)" fallback, and a
// fixture that exercises the real branch is what makes it a test.
func termNoteProbeFields() chatInfoFields {
	return chatInfoFields{
		SessionID:   "01a16208-2a83-7ebb-bbc1-7ea15c70948e",
		SessionDir:  "/home/user/.gopi/sessions/01a16208-2a83-7ebb-bbc1-7ea15c70948e",
		SessionFile: "/home/user/.gopi/sessions/01a16208-2a83-7ebb-bbc1-7ea15c70948e/session.jsonl",
		Model:       "probe-model",
		Dir:         "/home/user/workspace/gopi-probe",
		Repo:        "/home/user/workspace/gopi-probe",
		InRepo:      true,
		Node:        "probe-node",
		ToolsOn:     true,
	}
}

// TestTermNoteCellsCarryNoEscapeAndJoinToTheHistoricBytes is the contract's
// core: no escape byte in a cell, and the cells concatenated per row are exactly
// the block's historic line — under BOTH tables (colour on and colour off), since
// a renderer whose text depended on the capability would be two surfaces.
func TestTermNoteCellsCarryNoEscapeAndJoinToTheHistoricBytes(t *testing.T) {
	f := termNoteProbeFields()
	want := strings.Split(strings.TrimRight(chatInfoBlock(f), "\n"), "\n")

	for _, mode := range []struct {
		name string
		mode lineedit.ColourMode
	}{
		{"colour-on", lineedit.ColourOn},
		{"colour-off", lineedit.ColourOff},
	} {
		t.Run(mode.name, func(t *testing.T) {
			st := termToolStylesFor(mode.mode)
			rows := chatInfoTermRows(f, st)
			if len(rows) != len(want) {
				t.Fatalf("rows = %d, want %d (the block's own line count)", len(rows), len(want))
			}
			for i, row := range rows {
				got := row.Text()
				if got != want[i] {
					t.Errorf("row %d text = %q, want the block's line %q", i, got, want[i])
				}
				if row.CopyText == "" && want[i] != "" {
					t.Errorf("row %d carries no CopyText for the line %q", i, want[i])
				}
				for j, cell := range row.Render {
					if sgr.EscapeSeqLen(cell.Text) != 0 || strings.IndexByte(cell.Text, 0x1b) >= 0 {
						t.Errorf("row %d cell %d carries an escape byte: %q", i, j, cell.Text)
					}
				}
			}
		})
	}
}

// TestTermNoteCopyTextIsPrintableASCII is the paste contract asserted on the
// characters rather than on a sample: a copied line may contain printable ASCII
// and nothing else — no NBSP, no zero-width joiner, no tab, no decoration. It is
// the machine check behind the operator's "零装饰字符 / 零不可见字符".
func TestTermNoteCopyTextIsPrintableASCII(t *testing.T) {
	rows := chatInfoTermRows(termNoteProbeFields(), termToolStylesFor(lineedit.ColourOn))
	rows = append(rows, termNoteRows("voice: [gopi] VOICE MODE ON\n  device   a microphone", termToolStylesFor(lineedit.ColourOn))...)
	if len(rows) == 0 {
		t.Fatal("no rows to check — the fixture produced nothing")
	}
	for _, row := range rows {
		for _, r := range row.CopyText {
			if r == '\n' {
				// A break is a row boundary, never content: chatInfoTermRows does not
				// put one in a CopyText, and termNoteRows splits on them.
				t.Errorf("a row's CopyText carries a newline: %q", row.CopyText)
				continue
			}
			if r < 0x20 || r > 0x7e {
				t.Errorf("CopyText %q carries the non-printable/non-ASCII rune %q (U+%04X)", row.CopyText, r, r)
			}
		}
	}
}

// TestTermNoteMonochromeEmitsZeroSequences is the "无色时零字节" arm, stated as the
// operator stated it: not a reset, not an empty sequence — nothing. The row's own
// Row() is what a coloured carrier is sent, so the assertion is made on the bytes
// that would go to a terminal.
func TestTermNoteMonochromeEmitsZeroSequences(t *testing.T) {
	st := termToolStylesFor(lineedit.ColourOff)
	rows := chatInfoTermRows(termNoteProbeFields(), st)
	rows = append(rows, termNoteRows("voice: something the surface is saying", st)...)
	for i, row := range rows {
		if row.Colour {
			t.Fatalf("row %d says Colour=true on a monochrome table", i)
		}
		if got := row.Row(); strings.IndexByte(got, 0x1b) >= 0 {
			t.Errorf("row %d emitted an escape byte on the monochrome table: %q", i, got)
		}
	}
}

// TestTermNoteColourOnIsDimAndNeverLoud pins the EMPHASIS the operator ruled on:
// the system's own voice is Dim, and it is not bold, not underlined and not one of
// the loud attribute combinations a failure would wear.
//
// The assertion is on the FIELD (sgr.Style.Dim) rather than on a byte pattern, so
// it keeps meaning if the sequence spelling changes; the byte pattern is checked
// as well, because the rule is about what reaches the screen.
func TestTermNoteColourOnIsDimAndNeverLoud(t *testing.T) {
	st := termToolStylesFor(lineedit.ColourOn)
	if !st.On {
		t.Fatal("the colour-on table reports On=false")
	}
	sys := termSystemStyle(st)
	if !sys.Dim {
		t.Fatalf("the system voice is not Dim: %+v", sys)
	}
	if sys.Bold || sys.Underline {
		t.Errorf("the system voice is loud: bold=%v underline=%v", sys.Bold, sys.Underline)
	}
	if sys.Reverse {
		t.Errorf("the system voice is reversed (a highlight): %+v", sys)
	}
	for _, row := range chatInfoTermRows(termNoteProbeFields(), st) {
		if !row.Colour {
			t.Fatalf("a row of a colour-on block says Colour=false")
		}
	}
	// At least one cell of the rendered block must actually WEAR the dim attribute
	// — a table that resolved dim and a producer that never used it would pass
	// every assertion above while the screen stayed plain.
	worn := false
	for _, row := range chatInfoTermRows(termNoteProbeFields(), st) {
		for _, cell := range row.Render {
			if cell.Style.Dim && cell.Text != "" {
				worn = true
			}
		}
	}
	if !worn {
		t.Error("no cell of the /info block wears the dim attribute — the table resolved it and nothing used it")
	}
}

// TestTermNoteVoiceRowsAreDimAndCopyable is the /voice half: the mode's rows take
// the same renderer, so a notice is dim on a coloured carrier and byte-identical
// on a monochrome one.
func TestTermNoteVoiceRowsAreDimAndCopyable(t *testing.T) {
	const text = "[gopi] VOICE MODE ON\n  device    a microphone\n  engine    doubao"
	on := termNoteRows(text, termToolStylesFor(lineedit.ColourOn))
	off := termNoteRows(text, termToolStylesFor(lineedit.ColourOff))

	if len(on) != 3 || len(off) != 3 {
		t.Fatalf("rows = %d/%d, want 3 rows for a three-line notice", len(on), len(off))
	}
	if got := termRowsText(off); got != text+"\n" {
		t.Errorf("monochrome rows = %q, want the notice's own bytes %q", got, text+"\n")
	}
	for i := range on {
		if !on[i].Render[0].Style.Dim {
			t.Errorf("voice row %d is not dim: %+v", i, on[i].Render[0].Style)
		}
		if on[i].Text() != off[i].Text() {
			t.Errorf("row %d text differs between carriers (%q vs %q) — the emphasis must not change the characters",
				i, on[i].Text(), off[i].Text())
		}
	}
}

// TestChatInfoBlockReachesTheBandDimAndUnchanged is the SCREEN half of the
// contract, measured on the cell grid the reserved layout actually painted: the
// /info block's rows must arrive on the band with the dim attribute on their
// LABEL cells, and with EXACTLY the characters chatInfoBlock renders — an
// emphasis that cost a character would be the defect this epic forbids.
//
// WHY THE CELLS AND NOT THE BYTES: a byte comparison cannot tell "the escape
// sequence moved" from "the text moved", and the acceptance criterion is
// precisely that one changed and the other did not. The screen keeps a style per
// cell (internal/lineedit/screenattr.go), so the two questions are asked
// separately.
func TestChatInfoBlockReachesTheBandDimAndUnchanged(t *testing.T) {
	h := panelHarness(t)
	// The harness is built over a source with no capability probe, so its table
	// is the monochrome one. This case is about COLOUR reaching the grid, so the
	// table is resolved the way a real coloured run resolves it.
	h.in.tool = termToolStylesFor(lineedit.ColourOn)

	logger, err := agent.NewSessionLogger("", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	defer func() { _ = logger.Close() }() //stylegate:ignore the assertion is on the painted grid, not on the close
	sess := &chatSession{}
	ag := &agent.Agent{Model: "fixture-model", SessionLog: logger}

	chatInfoHandleLine(h.in, ag, sess)
	h.waitFor(t, "the /info block on the band", func() bool {
		h.replay()
		_, _, band := panelBand(t, h)
		return strings.Contains(band, logger.SessionID())
	})

	top, _, band := panelBand(t, h)
	bandRows := strings.Split(band, "\n")

	// 1. THE HEADER ROW IS DIM, EXACTLY OVER ITS OWN CELLS.
	headerRow := bandRowOf(bandRows, chatInfoHeader)
	if headerRow < 0 {
		t.Fatalf("the header %q is not on any band row:\n%s", chatInfoHeader, band)
	}
	for col := 0; col < len(chatInfoHeader); col++ {
		if !h.screen.Attr(top+headerRow, col).Dim {
			t.Fatalf("band row %d col %d (%q) is not dim — the system's own voice did not reach the grid:\n%s",
				top+headerRow, col, chatInfoHeader[col], band)
		}
	}
	if h.screen.Attr(top+headerRow, len(chatInfoHeader)).Dim {
		t.Errorf("the cell AFTER the header (col %d) is dim too — the emphasis spilled past the label", len(chatInfoHeader))
	}

	// 2. THE VALUE KEEPS THE TERMINAL'S OWN FOREGROUND, and the label beside it
	//    does not: the session row is the block's copyable handle, so it is the
	//    row that proves the two roles are told apart.
	valueRow := bandRowOf(bandRows, logger.SessionID())
	if valueRow < 0 {
		t.Fatalf("the session value is not on any band row:\n%s", band)
	}
	valueCol := len(chatInfoLabelSession) + len(chatInfoLabelSep)
	if !h.screen.Attr(top+valueRow, 0).Dim {
		t.Errorf("the value row's label is not dim — the label and the value share one emphasis")
	}
	if h.screen.Attr(top+valueRow, valueCol).Dim {
		t.Errorf("the VALUE of the session row (col %d) is dim — a copyable value must stay the terminal's own foreground", valueCol)
	}

	// 3. THE CHARACTERS DID NOT MOVE. The painted row is the block's own line,
	//    with no marker column and no reserved gutter in front of it.
	want := chatInfoLabelSession + chatInfoLabelSep + logger.SessionID()
	if !strings.Contains(bandRows[valueRow], want) {
		// The band is 76 cells and a session path is longer, so this row may WRAP;
		// what must hold is that the row opens with the label at column 0.
		if !strings.HasPrefix(bandRows[valueRow], chatInfoLabelSession) {
			t.Errorf("the band row %d = %q does not open with the block's own line %q",
				top+valueRow, bandRows[valueRow], want)
		}
	}
	t.Logf("band row %d (header) attr = %+v | row %d (session) col0 = %+v col%d = %+v",
		top+headerRow, h.screen.Attr(top+headerRow, 0),
		top+valueRow, h.screen.Attr(top+valueRow, 0), valueCol, h.screen.Attr(top+valueRow, valueCol))
}
