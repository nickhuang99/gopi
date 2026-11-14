package main

// term_tool_row.go — the tool-activity row (`⚙ call …` / `↳ result …`) as ONE
// pure renderer.
//
// THE SHAPE IS DELIBERATE, and its two halves exist for two different readers.
//
//	⚙ call bash  git log --oneline -3 | head -20
//	↳ result bash  e5b1c0f worktree at 585507c, clean
//
// WHY THE COPY TEXT IS A FIELD OF ITS OWN. A tool row's most valuable property
// is that the operator can SELECT THE COMMAND AND RUN IT. That property does not
// survive a renderer that composes decoration and command into one string: the
// moment a glyph, an indent, a continuation gutter or a truncation glyph lands
// inside the command's cells, a paste stops being the command (measured twice on
// this surface — the `▌ ` turn gutter killed every pasted line, and a mid-token
// `…` turned `-maxdepth` into `-ma…xdepth`). So the row carries the command as
// CopyText — the event's own bytes, untouched — and the RENDER as a list of
// cells whose concatenated TEXT is the one visible line. The renderer never
// inserts a break, an ellipsis or a border INTO the command; decoration lives in
// cells that come before it.
//
// WHY CELLS AND NOT A STRING. The two halves of a tool row are read at different
// speeds: the glyph and the tool name answer "what happened" at a glance, the
// command is the payload, and the result digest is a hint. That is an emphasis
// question, and an emphasis question answered by composing SGR into a string is
// answered a second time by every caller that wants a different table. A cell
// list puts the emphasis in DATA (one sgr.Style per cell), so the same row draws
// identically on the band, in the `/cli` page and in a dump, and a monochrome
// run is the same data with every style empty — which is why NO_COLOR emits ZERO
// escape bytes rather than "sequences that happen to be resets".
//
// WHY IT IS DIFFERENT FROM chatInfoBlock. /info is COPYABLE and nothing else: its
// rows open at column 0 and carry no chrome at all. A tool row is both
// GLANCEABLE (it is a live event, and the operator scans for "did it call bash?")
// and copyable in exactly ONE region (the command). The two shapes therefore
// disagree on purpose: /info refuses a marker column, a tool row opens with one
// because the marker is what makes the row scannable — and the copyable region
// starts after it, which is why CopyText is carried as data rather than read off
// the rendered line.
//
// THE ONE LINE THE OPERATOR SEES IS THE JOINT OF THE CELLS, so the layout
// arithmetic belongs to whoever draws it (the band's own wrap, the terminal's
// autowrap for a cooked run). This file performs no wrapping and no truncation
// of the command: a 200-cell command is ONE row of cells here, and it stays one
// command when it is drawn across two screen rows (the band's note cutter
// continues it and marks the continuation — see lineedit.NoteContinuation).
//
// THE RESULT HALF IS THE ONE PLACE A CUT IS ALLOWED, and it is not a command: a
// result digest is a hint about what came back, so it is capped (termToolResultMaxRunes)
// and marked with `…`. The glyph differs (`↳`, the continuation marker the
// transcript already uses) and the style is dim, because a result is the echo of
// an action rather than the action.
//
// NOTHING HERE CHANGES THE WIRE. ToolCallSummary / ToolResultDigest compose
// ev.Summary before this file sees it (internal/agent/chat_ops.go), and the web
// transcript replays those exact bytes; this renderer only decides how to DRAW
// the summary it was handed.

import (
	"strings"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sgr"
)

// termToolResultMaxRunes caps the result half of a row (see the header: a digest
// may be cut, a command may not).
const termToolResultMaxRunes = 100

// termToolResultEllipsis marks a cut result digest.
const termToolResultEllipsis = "…"

// termCell is one styled run of a tool row: the text it shows and the SGR state
// every one of its characters is drawn under.
//
// The style is a sgr.Style — the project's ONE definition of "what colour is
// this cell" — and not a raw sequence, so a caller may inspect a row's emphasis
// (a test asserting the call and the result halves differ) without parsing bytes
// it composed itself.
type termCell struct {
	Text  string
	Style sgr.Style
}

// termRow is ONE tool-activity row, split by the two questions its readers ask:
// what is the command (Copyable), and how is it drawn (Render).
//
//	CopyText  the event's own bytes, verbatim — the command for a call (the
//	          UNCUT one when the event carries it, see ToolEvent.Command), the
//	          result digest for a result. Never a substring of Render: a
//	          renderer that inserted a glyph would otherwise silently change the
//	          value a paste produces.
//	Render    the cells of the visible row, in order. Their TEXT, concatenated,
//	          is exactly the line the operator sees; CopyText appears inside it
//	          contiguously and unchanged for a call.
//	Meta      the right-hand summary (exit code / duration / line count). It is
//	          EMPTY today and that is a statement about the wire rather than an
//	          unfinished field: agent.ToolEvent carries kind, name and summary
//	          and nothing else, and this epic must not change ToolCallSummary /
//	          ToolResultDigest (the web transcript replays their bytes). The field
//	          exists so the shape is settled where the data can arrive later
//	          without a second row renderer.
type termRow struct {
	CopyText string
	Render   []termCell
	Meta     string
	// Colour says the cells carry emphasis to DRAW. It is a field rather than a
	// question asked of the styles because sgr.Style's ZERO VALUE IS BLACK, not
	// "no style" (only sgr.Default() is the terminal default) — so a renderer
	// that inferred "no colour" from a style would paint the monochrome row
	// black. One flag, set once from the table the run resolved.
	Colour bool
}

// termToolStyles is the row's emphasis table, resolved ONCE at startup (the same
// discipline PaletteFor has: a render path branches on no capability). It is
// three named roles, not a general palette: the row's own chrome, the payload,
// and the result echo.
//
// ColourOff (NO_COLOR, TERM=dumb, a non-terminal sink) yields the ZERO table and
// On=false, and the row then emits plain text — no sequence at all, not a reset.
type termToolStyles struct {
	// On says the table carries colour. False means every cell is drawn raw.
	On bool
	// Glyph is the row's marker column (`⚙ ` for a call, `↳ ` for a result).
	Glyph sgr.Style
	// Name is the kind and the tool name — the half that answers "what ran".
	Name sgr.Style
	// Command is the payload. It is the terminal's OWN foreground (sgr.Default):
	// the command is the operator's material, not the surface's judgement, and
	// it is the region they copy.
	Command sgr.Style
	// Meta is the right-hand summary (dim: it is a hint beside the payload).
	Meta sgr.Style
	// ResultGlyph / ResultText are the result row's dim echo. They are separate
	// fields from Glyph/Name for the reason Reasoning is separate from Draft in
	// lineedit.Palette: a call and its result may share an emphasis today and
	// must still be able to differ tomorrow.
	ResultGlyph sgr.Style
	ResultText  sgr.Style
}

// termToolStylesFor resolves the row's table from the run's colour capability.
// It is the ONLY mapping capability → table, so "colour off" is one decision
// here exactly as it is one decision in lineedit.PaletteFor.
func termToolStylesFor(mode lineedit.ColourMode) termToolStyles {
	if mode == lineedit.ColourOff {
		// NOT the zero table: sgr.Style's zero value is BLACK (Fg 0, Bg 0), so a
		// monochrome row built from it would draw black on black. Every field is
		// the terminal DEFAULT instead, which is what "no colour" means on a
		// terminal.
		mono := sgr.Default()
		return termToolStyles{
			Glyph: mono, Name: mono, Command: mono, Meta: mono,
			ResultGlyph: mono, ResultText: mono,
		}
	}
	return termToolStyles{
		On:          true,
		Glyph:       termCellStyle(true, false, true),
		Name:        termCellStyle(true, false, true),
		Command:     sgr.Default(),
		Meta:        termCellStyle(false, true, false),
		ResultGlyph: termCellStyle(false, true, false),
		ResultText:  termCellStyle(false, true, false),
	}
}

// termToolStylesFromPalette derives the row's table from the run's palette.
//
// WHY FROM THE PALETTE AND NOT FROM A SECOND CAPABILITY READ. The palette is the
// run's ONE colour decision (lineedit resolves it once, with its one downgrade
// notice); a monochrome palette is the ZERO table, and lineedit documents that
// zero value as "colours off" (Style's own comment: the zero value is the
// monochrome style). So "does this run have colour" is asked of the thing that
// already answers it, and this file never looks at NO_COLOR or at a tty.
func termToolStylesFromPalette(pal lineedit.Palette) termToolStyles {
	if pal.ToolCall.Empty() {
		return termToolStyles{}
	}
	return termToolStylesFor(lineedit.ColourOn)
}

// termCellStyle builds a style from emphasis flags. sgr.Style's zero value is
// BLACK (its Fg field is an index), so a style that means "no colour" must name
// the terminal default — this helper is the only place that has to remember it.
func termCellStyle(bold, dim, underline bool) sgr.Style {
	st := sgr.Default()
	st.Bold, st.Dim, st.Underline = bold, dim, underline
	return st
}

// termToolRow is the row renderer: a PURE function of the event and the table.
// It touches no terminal, no clock and no global, which is what lets the tests
// compare its cells byte for byte.
func termToolRow(ev agent.ToolEvent, st termToolStyles) termRow {
	summary := ev.Summary
	// THE CALL HALF CARRIES THE UNCUT COMMAND WHEN THERE IS ONE. ev.Summary is a
	// DIGEST (capped at toolCallSummaryMaxChars with a `…`), so a 200-cell
	// command reaches this function already cut — and this row's whole value is
	// that its command can be selected and run. ev.Command is the same argument
	// uncut (internal/agent: one selection, two renderings), so the copy text and
	// the command's own cell take it in preference, and the digest stays what it
	// is for every other tool: an empty Command falls back to Summary, which is
	// why a `read` row (path, not command) renders exactly as it did.
	payload := summary
	if ev.Command != "" && ev.Kind != agent.ToolEventKindResult {
		payload = ev.Command
	}
	if ev.Kind == agent.ToolEventKindResult {
		// A result may be cut (see the header); a call may not. The separator is
		// emitted only where something follows it, so a result with no digest is
		// `↳ result bash` rather than a row ending in a space (the space is part
		// of the marker column's grammar, not padding).
		cells := []termCell{
			{Text: lineedit.NoteContinuation, Style: st.ResultGlyph},
			{Text: string(ev.Kind), Style: st.ResultText},
		}
		if ev.Name != "" {
			cells = append(cells, termCell{Text: " " + ev.Name, Style: st.ResultText})
		}
		if d := termToolDigest(summary); d != "" {
			cells = append(cells, termCell{Text: " " + d, Style: st.ResultText})
		}
		return termRow{CopyText: summary, Render: cells, Colour: st.On}
	}
	cells := []termCell{
		{Text: lineedit.ToolGlyph, Style: st.Glyph},
		{Text: " ", Style: st.Command},
		{Text: string(ev.Kind), Style: st.Name},
	}
	if ev.Name != "" || payload != "" {
		cells = append(cells, termCell{Text: " ", Style: st.Command})
	}
	if ev.Name != "" {
		cells = append(cells, termCell{Text: ev.Name, Style: st.Name})
	}
	if payload != "" {
		if ev.Name != "" {
			cells = append(cells, termCell{Text: " ", Style: st.Command})
		}
		cells = append(cells, termCell{Text: payload, Style: st.Command})
	}
	return termRow{CopyText: payload, Render: cells, Colour: st.On}
}

// termToolDigest caps a result digest and marks the cut. It is rune-based, so a
// wide rune is never split (the same rule every other cut in this project
// follows).
func termToolDigest(s string) string {
	r := []rune(s)
	if len(r) <= termToolResultMaxRunes {
		return s
	}
	return string(r[:termToolResultMaxRunes]) + termToolResultEllipsis
}

// Text is the row's visible characters: the cells' texts concatenated, with no
// escape byte. It is what a carrier that cannot show colour draws (`/cli`'s
// HTML body, a cooked run's stderr), and it is the shape the row's own
// copy-paste test reads.
func (r termRow) Text() string {
	var b strings.Builder
	for _, c := range r.Render {
		b.WriteString(c.Text)
	}
	return b.String()
}

// Row is the row as the bytes a colour terminal is sent: each cell under its own
// style, coalesced so two neighbours sharing an emphasis cost one sequence —
// which is what makes the monochrome run byte-identical to Text.
//
// The sequences are ABSOLUTE (sgr.Style.Seq resets first), so a cell's bytes do
// not depend on what the previous cell left in force; that is what lets the band
// hand this row to the note path, which re-cuts it at a cell boundary without
// knowing what the escapes mean (every measuring path there — cutCols,
// displayWidth, cutAtBoundary — steps over escape sequences atomically).
func (r termRow) Row() string {
	var b strings.Builder
	for _, c := range r.Render {
		if c.Text == "" {
			continue
		}
		if !r.Colour {
			b.WriteString(c.Text)
			continue
		}
		b.WriteString(c.Style.Seq())
		b.WriteString(c.Text)
		b.WriteString(sgr.ResetSeq())
	}
	return b.String()
}

// termToolRuns is the row as the band's run vocabulary (lineedit.NoteRun): the
// same cells, converted ONCE here (lineedit.StyleFromSGR) so no sequence is ever
// parsed downstream. mask is the run's redaction pass, applied per cell — the
// row is built out of a call's arguments, which is where a path or an id lives,
// and the dumped grid is what a containment check reads.
func termToolRuns(r termRow, mask func(string) string) []lineedit.NoteRun {
	runs := make([]lineedit.NoteRun, 0, len(r.Render))
	for _, c := range r.Render {
		if c.Text == "" {
			continue
		}
		text := c.Text
		if mask != nil {
			text = mask(text)
		}
		runs = append(runs, lineedit.NoteRun{Text: text, Style: lineedit.StyleFromSGR(c.Style)})
	}
	return runs
}

// termToolRowText renders one tool row as the plain line a cooked sink receives.
// It exists so the historic bytes of `gopi chat --once` (and of a non-band chat)
// stay exactly what they were: the SAME cells, drawn without colour.
func termToolRowText(ev agent.ToolEvent) string {
	return termToolRow(ev, termToolStylesFor(lineedit.ColourOff)).Text()
}
