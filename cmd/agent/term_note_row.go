package main

// <!-- doc-class: normative -->

// term_note_row.go — the SYSTEM'S OWN EXPLANATORY ROWS (`/info`'s block, a
// `/voice` notice) as termRow values, so they are drawn DIM and are still
// copyable.
//
// WHY A SECOND PRODUCER AND NOT A SECOND SHAPE. term_tool_row.go settled the
// vocabulary this project uses for "a row of the console that is not the
// conversation": termCell carries one styled run, termRow carries the Render
// (cells, with style) and the CopyText (the bytes a reader pastes), and
// termToolStyles is the ONE table resolved from the run's colour capability. This
// file adds no vocabulary of its own — it produces termRow values and reads the
// SAME table. The only thing it decides is WHICH ROLE of that table a system
// explanation wears: dim.
//
// WHY DIM AND NOTHING ELSE. The operator's ruling (2026-10-22) is that the
// system's remarks about itself are LOW-CONTRAST and never loud: `Dim` says
// "this is the surface talking, not the answer", and the ban is on red and on
// highlight — a refusal, a failure or a transcript row is INFORMATION the
// operator may need, and an emphasis that shouts makes a normal state look like
// an incident. Dim is also the emphasis that survives every palette tier
// (16-colour, 256-colour, truecolor, and mono), because it is an attribute
// rather than an index.
//
// WHY THE VALUES AND THE COMMANDS STAY AT THE DEFAULT FOREGROUND. A /info value
// is the operator's OWN material — a session UUID, a path, a command they will
// paste into a shell — exactly as a tool row's command is. So it takes
// termToolStyles.Command (sgr.Default: the terminal's own foreground) while the
// LABELS around it take the dim role. That split is termToolStyles' own
// distinction (Meta is "a hint beside the payload"), reused rather than
// re-decided.
//
// THE COPY CONTRACT (the acceptance criterion this file exists to make true):
//   - CopyText carries no escape byte in ANY mode — the render is cells, and the
//     SGR is applied by whoever draws them;
//   - the cells' texts, concatenated, are EXACTLY the bytes the historic flow
//     path wrote (asserted in term_note_row_test.go against chatInfoBlock);
//   - a cell's text is never padded, indented or decorated — /info's block is
//     the one surface whose reason for existing is that a selected line can be
//     pasted, and the paste is the cell's text.
//
// NOTHING HERE TOUCHES A TERMINAL, A CLOCK OR A GLOBAL: every function is pure
// in its arguments, which is what lets the tests compare cells byte for byte
// without a pty (Rule 31's unit half; the pty half is the chat_info band test).

import (
	"io"
	"strings"

	"github.com/nickhuang99/gopi/internal/sgr"
)

// termSystemStyle is the ONE role a system explanation wears out of the run's
// table: Meta, the field termToolStyles documents as "a hint beside the payload"
// and resolves DIM. It is a named accessor rather than a field read at every
// call site so that "which emphasis does the surface speak in" is answered in
// one place, and so a producer names the QUESTION rather than the field.
func termSystemStyle(st termToolStyles) sgr.Style { return st.Meta }

// termNoteRows renders ONE system explanation — a block of plain text the
// surface composed — as rows of cells, each row drawn in the system's dim voice.
//
// A row per LINE, because the band commits one row per call: a caller that
// handed a whole block to one note would get the block's line breaks folded into
// a single row's cells (or, worse, into the width arithmetic).
//
// The trailing newline is a ROW BOUNDARY, not content: the historic writers put
// one line per row and the band does the same, so a text ending in "\n" is not
// given an extra empty row. A blank line INSIDE the block is kept — it is shape.
func termNoteRows(text string, st termToolStyles) []termRow {
	text = strings.TrimSuffix(text, "\n")
	lines := strings.Split(text, "\n")
	rows := make([]termRow, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, termRow{
			CopyText: line,
			Render:   []termCell{{Text: line, Style: termSystemStyle(st)}},
			Colour:   st.On,
		})
	}
	return rows
}

// chatInfoTermRows renders /info's whole block as cells: the SAME text the pure
// byte renderer (chatInfoBlock) produces, partitioned into cells so the labels
// recede and the values stay the operator's own foreground.
//
// The partition is the renderer's only freedom, and it is bounded: the cells of
// a row concatenate to exactly the bytes chatInfoBlock writes for that row, which
// term_note_row_test.go asserts against the real block rather than trusting.
func chatInfoTermRows(f chatInfoFields, st termToolStyles) []termRow {
	label := termSystemStyle(st)
	value := st.Command
	row := func(cells ...termCell) termRow {
		var b strings.Builder
		for _, c := range cells {
			b.WriteString(c.Text)
		}
		return termRow{CopyText: b.String(), Render: cells, Colour: st.On}
	}
	// THE SHAPE IS THE BLOCK'S OWN (chatInfoBlock): the title, one row per fact,
	// a blank separator row, then the copy section's title and its commands. It is
	// reproduced rather than invented, because the bytes must stay identical.
	rows := []termRow{
		row(termCell{Text: chatInfoHeader, Style: label}),
	}
	for _, r := range chatInfoRows(f) {
		// CopyText is the VALUE and not the label: the reason to select a line of
		// this block is to paste the UUID, the path or the command, and a paste
		// that carried `session:` along would not be an argument to anything.
		r := r
		rows = append(rows, termRow{
			CopyText: r.value,
			Render: []termCell{
				{Text: r.label, Style: label},
				{Text: chatInfoLabelSep, Style: label},
				{Text: r.value, Style: value},
			},
			Colour: st.On,
		})
	}
	rows = append(rows,
		row(termCell{Text: "", Style: label}),
		row(termCell{Text: chatInfoReplayHeader, Style: label}),
	)
	for _, line := range chatInfoCommands(f) {
		// A copyable command takes the value role for the same reason a value
		// does: it is material the operator pastes, not a remark about it.
		rows = append(rows, row(termCell{Text: line, Style: value}))
	}
	return rows
}

// termWriteRows commits rows to a plain writer — the shape the START-UP notices
// use (chat_session_budget.go, chat_session_slots.go, chat_policy_flag.go).
//
// WHY IT IS HERE AND NOT A THIRD ROAD. A start-up notice fires before any
// `chatInput` exists, so it cannot use WriteNoticeRuns (that road needs the
// band's editor). It can still wear the run's system voice, because the run
// resolves the table ONCE in runChat and hands it down — and the drawing it owes
// is exactly what termRow already answers: Row() per row, one newline per row.
// With Colour=false Row() is the cells' text concatenated, i.e. the SAME bytes
// the historic `fmt.Fprintf(os.Stderr, "%s\n", …)` wrote; with Colour=true it is
// those bytes under their own absolute sequences. No second style system, no
// second cut, no second writer: a start-up notice differs from a /voice row only
// in that its bytes go straight out instead of through the band's queue.
func termWriteRows(w io.Writer, rows []termRow) {
	if w == nil {
		return
	}
	for _, r := range rows {
		_, _ = io.WriteString(w, r.Row()+termRowBoundary) //stylegate:ignore a start-up notice's stream failure must not abort the run it announces
	}
}

// termRowBoundary is the one byte that ends a row on the plain writer. Named
// (Rule 1b) because it IS the row boundary every reader of these lines counts:
// the start-up contract test asserts a drawn line ends with exactly one of these
// and that a sentence whose format already ends in "\n" does not get a second.
const termRowBoundary = "\n"

// termRowsText is the rows as the plain text a monochrome carrier shows: one
// row per line, no escape byte. It is the flow path's bytes and the shape the
// copy contract reads.
func termRowsText(rows []termRow) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(r.Text())
		b.WriteString("\n")
	}
	return b.String()
}
