package lineedit

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// screendump.go — the STRUCTURED export of a Screen's already-interpreted state.
//
// WHY THIS EXISTS. A text-mode surface is the one UI that can be observed
// WITHOUT pixels: the bytes a renderer writes are already an interpreted cell
// grid, and Screen keeps that grid. What was missing was not a reader but an
// INTERFACE — Screen.Line/Contents answer "what does row 3 say", which is a
// question a human asks. A layout assertion asks different questions: where is
// the cursor, is this row padded to the full width, which columns carry reverse
// video, does the panel boundary sit at column 23 on every row, what did the
// scroll region confine. Those need the grid's STRUCTURE, and a screenshot is
// the most expensive and least precise way to get it (Rule 10.2: render truth
// is cell-level, never a vision model's prose).
//
// THE CONTRACT. A ScreenDump is a SNAPSHOT: it copies the grid, never aliases
// it, so the dump a caller holds keeps saying what the screen said at the
// instant Dump() was called even while the renderer keeps painting. It is
// self-describing — geometry, cursor, scroll region, whether per-cell
// attributes are live — and it carries an explicit BLIND SPOT list: the things
// this representation provably cannot see. A dump that silently omitted what it
// cannot represent would invite an assertion it cannot support.
//
// COLUMN EXACTNESS. Grid rows are CELL rows: exactly one rune per cell, the
// full width, and the second column of a wide (CJK) rune is the continuation
// cell the grid stores. Therefore the rune index of a grid row IS the 0-based
// cell column, always — for ASCII and for CJK alike. (The BYTE index is not:
// a CJK rune is one cell but three bytes. The Lines section, which skips
// continuation columns, is not column-exact either, and says so — it is the
// human-readable rendering, not the assertion surface.)

// DumpVersion is the schema version of a screen dump. A reader that asserts on
// a dump's shape must be able to tell which shape it has, so the version is
// carried in both renderings rather than assumed from the file's name.
const DumpVersion = 1

// The dump's text rendering vocabulary. These are read by agents (grep) and
// asserted by tests, so each is spelled ONCE here and referenced (Rule 1b/13).
const (
	// DumpAttrRunLabel is the dump's attribute-run section name, EXPORTED
	// because it is part of an interface two packages agree on rather than a
	// private rendering detail: the agent's system prompt (internal/agent)
	// must name the section the model will actually grep for when it reads a
	// screen back, and a prompt that teaches a section name the dump stopped
	// printing is a dead instruction. Rule 14: one definition, in the package
	// that OWNS the format, spelled by the consumer.
	DumpAttrRunLabel    = "attrs_begin"
	dumpHeaderPrefix    = "#gopi-screen-dump"
	dumpGridBegin       = "grid_begin"
	dumpGridEnd         = "grid_end"
	dumpLinesBegin      = "lines_begin"
	dumpLinesEnd        = "lines_end"
	dumpAttrBegin       = DumpAttrRunLabel
	dumpAttrEnd         = "attrs_end"
	dumpScrollbackBegin = "scrollback_begin"
	dumpScrollbackEnd   = "scrollback_end"
	// dumpScrollbackTruncated is the one line a BOUNDED rendering writes in place
	// of the rows it held back. It is a line of its own rather than a suffix on a
	// row so a reader can grep for it and a truncation can never be mistaken for
	// the screen's own text. EXPORTED because it is part of the boundary two
	// packages agree on: a caller that hands a bounded rendering to a model must
	// be able to assert the notice is there (Rule 14 — one definition, in the
	// package that owns the format).
	DumpScrollbackTruncatedLabel = "scrollback_truncated"
	dumpScrollbackTruncated      = DumpScrollbackTruncatedLabel
	dumpBlindBegin               = "blind_spots_begin"
	dumpBlindEnd                 = "blind_spots_end"
	// dumpRowSep separates a row's index from its content. It is a SINGLE
	// character cut out with strings.Cut, so a row whose own text contains the
	// separator still parses: only the FIRST one is the index boundary.
	dumpRowSep = "|"
	// dumpRowIndexDigits zero-pads a row index, so "row 12" is the literal
	// prefix 012| and a grep for a row cannot also match row 123.
	dumpRowIndexDigits = 3
	// dumpAttrCellSep / dumpAttrStyleSep spell one attribute run:
	// "<startCol>:<cells>=<style>".
	dumpAttrRangeSep = ":"
	dumpAttrStyleSep = "="
	dumpAttrRunSep   = " "
)

// The named field labels of the text rendering's header block.
const (
	dumpCursorLabel       = "cursor"
	dumpScrollRegionLabel = "scroll_region"
	dumpAttrsLabel        = "attrs"
	dumpRowLabel          = "row"
	dumpColLabel          = "col"
	dumpTopLabel          = "top"
	dumpBottomLabel       = "bottom"
	dumpCellsLabel        = "cells"
	dumpCountLabel        = "count"
	dumpLinesLabel        = "lines"
	dumpRecordedLabel     = "recorded"
	dumpColsLabel         = "cols"
	dumpRowsLabel         = "rows"
	dumpValueSep          = "="
	dumpFieldSep          = " "
	// The three fields of dumpScrollbackTruncated: what this rendering shows,
	// what the artifact holds in total, and what was therefore held back.
	dumpShownLabel   = "shown"
	dumpTotalLabel   = "total"
	dumpOmittedLabel = "omitted"
)

// dumpScrollbackUnbounded is the bound that holds NOTHING back: it is what Text
// passes to the shared renderer, and a negative value is this constant's only
// possible spelling as an argument (a 0 means "show no scrollback at all", which
// is a real request and must not be read as "no bound").
const dumpScrollbackUnbounded = -1

// DumpScrollbackSeeRows is how many scrollback rows a TOOL RETURN carries. It is
// the chat surface's `action=see` budget, not the artifact's: the artifact keeps
// the whole history (`Scrollback`, `Text`, `ui_state`), and only the rendering
// handed to a model is bounded.
//
// THE VALUE IS MEASURED, NOT CHOSEN. A 24-row screen whose turn history had
// grown to 278 turns arrived as 2,783,126 characters, of which the grid was
// 2,948 — the scrollback section was 99.9% of one `see` (2026-11-06). The tool's
// own question is "what is on the screen NOW": the grid answers it, and the
// scrollback is context a caller asks for with a different call (Transcript, or
// `gopi ui_state` on the artifact). 200 rows is ~8 screens of recent history —
// enough that the tail a reader reaches for in practice is present and the
// truncation line still reports the true total — and it holds a `see` in the
// tens of kilobytes instead of the megabytes.
const DumpScrollbackSeeRows = 200

// dumpScrollbackWindow resolves which rows a rendering shows: from is the first
// index written, shown is how many. An unbounded (negative) bound writes
// everything and reports the whole length; a non-negative bound writes the most
// recent rows, so a reader always sees the screen's own neighborhood rather than
// the start of a history that may predate it.
func dumpScrollbackWindow(total, maxRows int) (from, shown int) {
	if maxRows < 0 || maxRows >= total {
		return 0, total
	}
	return total - maxRows, maxRows
}

// Blind spot identifiers. Each names a thing this representation CANNOT show.
// They are exported-ish by living in one place, because a report and a test
// both quote them and a renamed spot must not silently stop being listed.
const (
	// BlindSpotFont: the grid stores code points; the glyph is the emulator's.
	BlindSpotFont = "font"
	// BlindSpotIME: a composition happens in the input method, before the
	// application sees bytes, so no preedit state is ever in the grid.
	BlindSpotIME = "ime_composition"
	// BlindSpotRawBytes: the dump is the EFFECT of the byte stream, not the
	// stream. Sequence ORDER, framing and timing are gone.
	BlindSpotRawBytes = "raw_bytes"
	// BlindSpotGraphics: sixel / kitty graphics are skipped by the parser and
	// paint no cells, so an image on the screen is invisible here.
	BlindSpotGraphics = "graphics_protocols"
	// BlindSpotModes: private modes (?25h cursor visibility, ?1049h alternate
	// screen, ?2004h bracketed paste) are parsed and NOT recorded, so the dump
	// cannot say which screen the grid IS or whether the cursor is hidden.
	BlindSpotModes = "private_modes"
	// BlindSpotBlankCells: a cell holding a space and a cell never painted are
	// byte-identical in the grid — "this row is padded" is assertable, "this
	// row was WRITTEN as blanks" is not.
	BlindSpotBlankCells = "blank_vs_unpainted"
	// BlindSpotInteriorScroll: lines pushed off an INTERIOR scroll-region
	// margin are discarded, never saved to the scrollback.
	BlindSpotInteriorScroll = "interior_scroll_loss"
	// BlindSpotTiming: a dump is one instant; intermediate frames are not
	// observable, so a flicker or a wrong intermediate layout is invisible.
	BlindSpotTiming = "frame_timing"
)

// dumpBlindSpots is the list every dump carries. It is a package-level value
// because the blind spots belong to the REPRESENTATION (this cell grid), not to
// an instance of it: a second Screen cannot see more than the first.
var dumpBlindSpots = []DumpBlindSpot{
	{ID: BlindSpotFont, Detail: "glyph shapes and metrics are the terminal emulator's; the grid holds code points, so font, kerning and fallback are invisible"},
	{ID: BlindSpotIME, Detail: "an IME composes before the application receives bytes; preedit state never enters the grid"},
	{ID: BlindSpotRawBytes, Detail: "the dump is the interpreted EFFECT of the byte stream, not the stream: escape-sequence order, framing and inter-frame timing are not kept"},
	{ID: BlindSpotGraphics, Detail: "sixel / kitty graphics sequences are skipped by the parser and paint no cells, so an image on the screen leaves no trace here"},
	{ID: BlindSpotModes, Detail: "private modes (cursor visibility, alternate screen, bracketed paste) are parsed and not recorded: the dump describes the cell grid, it cannot say which screen the grid is"},
	{ID: BlindSpotBlankCells, Detail: "a space cell and a never-painted cell are identical in the grid: padding is assertable, 'this row was written as blanks' is not"},
	{ID: BlindSpotInteriorScroll, Detail: "a line pushed off an INTERIOR scroll-region margin is discarded by the terminal, not saved: history beyond the grid plus Scrollback() is unrecoverable"},
	{ID: BlindSpotTiming, Detail: "a dump is a single instant; intermediate frames are not observable, so a transient wrong layout is invisible to it"},
}

// DumpBlindSpot is one thing the representation cannot see, named and
// explained. Reported by every dump so a reader can bound its own conclusions.
type DumpBlindSpot struct {
	// ID is the stable short name (one of the BlindSpot* constants).
	ID string `json:"id"`
	// Detail is the sentence a reader needs to judge the limit.
	Detail string `json:"detail"`
}

// DumpCursor is the cursor's position and its presence. Row and col are
// 0-based CELL coordinates; the dump reports 0-based because the grid does, and
// says so here rather than leaving the reader to guess.
type DumpCursor struct {
	Row int `json:"row"`
	Col int `json:"col"`
}

// DumpRegion is an inclusive 0-based row range — the scrolling region.
type DumpRegion struct {
	Top    int `json:"top"`
	Bottom int `json:"bottom"`
}

// DumpAttrRun is a maximal run of consecutive cells in one row that share one
// SGR style. Runs are emitted over the FULL row width, so a reader can assert
// "columns 12..99 are reverse video" — the thing a colour claim is really about.
type DumpAttrRun struct {
	// Col is the 0-based cell column the run starts at.
	Col int `json:"col"`
	// Cells is how many consecutive cells the run covers (>= 1).
	Cells int `json:"cells"`
	// Style is the run's SGR style, as sgr.Style.String renders it: "default"
	// when nothing is set, otherwise its non-default parts ("reverse",
	// "bold fg=red", …).
	Style string `json:"style"`
}

// ScreenDump is a snapshot of a Screen's interpreted state: everything a layout
// assertion needs, in one value, plus an explicit statement of its own blind
// spots.
type ScreenDump struct {
	// Version is DumpVersion at the time of the snapshot.
	Version int `json:"version"`
	// Cols / Rows are the grid's geometry in cells.
	Cols int `json:"cols"`
	Rows int `json:"rows"`
	// Cursor is the cursor position captured with the grid.
	Cursor DumpCursor `json:"cursor"`
	// ScrollRegion is the installed scrolling region (0-based, inclusive).
	ScrollRegion DumpRegion `json:"scroll_region"`
	// AttrsRecorded reports whether per-cell attributes are LIVE in this
	// build. It exists because the alternative — assuming the AttrRuns are
	// meaningful — is how a reader asserts colour on a grid that never
	// recorded any. A build without attribute recording must still SAY so.
	AttrsRecorded bool `json:"attrs_recorded"`
	// Lines is the readable rendering: one entry per row, trailing blanks
	// trimmed, a wide rune's continuation column skipped (Screen.Line). This
	// is what a human reads; it is NOT column-exact.
	Lines []string `json:"lines"`
	// Grid is the cell-exact rendering: one entry per row, exactly one rune
	// per cell, full width. The rune index of a Grid row IS the 0-based cell
	// column.
	Grid []string `json:"grid"`
	// AttrRuns holds, per row, the row's maximal same-style runs, covering the
	// full width. Empty only when AttrsRecorded is false.
	AttrRuns [][]DumpAttrRun `json:"attr_runs"`
	// Scrollback is the saved history, oldest first — the lines the terminal
	// pushed off the screen's first row.
	Scrollback []string `json:"scrollback"`
	// BlindSpots is what this representation cannot show.
	BlindSpots []DumpBlindSpot `json:"blind_spots"`
}

// Dump takes a snapshot of the screen's interpreted state.
//
// It copies every field it reports, so the returned value is immutable for the
// caller: the renderer can keep feeding the Screen without changing a dump that
// has already been handed out (a report quotes a dump long after the run it
// describes has ended).
func (s *Screen) Dump() ScreenDump {
	lines := make([]string, s.Rows)
	grid := make([]string, s.Rows)
	runs := make([][]DumpAttrRun, s.Rows)
	for r := 0; r < s.Rows; r++ {
		lines[r] = s.Line(r)
		// One rune per CELL, untrimmed: the width is the contract that makes
		// the rune index a column number.
		grid[r] = string(s.cells[r])
		runs[r] = s.attrRuns(r)
	}
	top, bottom := s.ScrollRegion()
	return ScreenDump{
		Version:      DumpVersion,
		Cols:         s.Cols,
		Rows:         s.Rows,
		Cursor:       DumpCursor{Row: s.CurRow, Col: s.CurCol},
		ScrollRegion: DumpRegion{Top: top, Bottom: bottom},
		// The grid records SGR per cell (screenattr.go); the flag reports the
		// FACT rather than the intent, so a reader never has to trust a comment.
		AttrsRecorded: true,
		Lines:         lines,
		Grid:          grid,
		AttrRuns:      runs,
		Scrollback:    s.Scrollback(),
		BlindSpots:    append([]DumpBlindSpot(nil), dumpBlindSpots...),
	}
}

// attrRuns collapses one row's per-cell styles into maximal runs over the FULL
// width. It walks every cell (never a sampled subset): a run list derived from
// "the cells that look interesting" would make the dump's own coverage a
// judgement call instead of a fact.
func (s *Screen) attrRuns(row int) []DumpAttrRun {
	if row < 0 || row >= s.Rows || s.Cols == 0 {
		return nil
	}
	runs := make([]DumpAttrRun, 0, dumpAttrRunCapacity)
	start := 0
	for col := 1; col <= s.Cols; col++ {
		// col == Cols closes the final run: one comparison, no tail case.
		if col < s.Cols && s.attrs[row][col].Equal(s.attrs[row][start]) {
			continue
		}
		runs = append(runs, DumpAttrRun{
			Col:   start,
			Cells: col - start,
			Style: s.attrs[row][start].String(),
		})
		start = col
	}
	return runs
}

// dumpAttrRunCapacity is the initial capacity guess for a row's run list: a row
// usually carries a handful of styles (default text, a highlighted band, a
// dimmed rule). It only sizes a slice — a row with more runs grows it.
const dumpAttrRunCapacity = 8

// JSON renders the dump as indented JSON — the machine-checkable form. A
// marshal failure is returned, never swallowed (Rule 15b): a dump nobody can
// parse must say so rather than look like an empty screen.
func (d ScreenDump) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("screen dump: cannot render JSON: %w", err)
	}
	return b, nil
}

// Text renders the dump as readable, greppable text — the form a report quotes
// and an agent reads. The shape is deliberately line-oriented and sectioned:
//
//	#gopi-screen-dump v=1 cols=100 rows=30
//	cursor row=12 col=4
//	scroll_region top=0 bottom=29
//	attrs recorded=true
//	lines_begin ...
//
// Every row line is "<index>|<content>" with a zero-padded 3-digit index, so a
// reader asserts on a row by its literal prefix and cannot match a longer index
// by accident.
//
// Text is the WHOLE artifact, scrollback included — it is what `gopi ui_state`
// prints and what a replay compares, so its output must stay complete. A caller
// that hands the rendering to a READER WITH A BUDGET (a tool return that lands in
// a model's context) uses TextScoped instead.
func (d ScreenDump) Text() string { return d.text(dumpScrollbackUnbounded) }

// TextScoped renders EXACTLY what Text renders, except that the scrollback
// section carries at most maxScrollback rows — the most recent ones, with their
// original indices — and, when the dump holds more than that, one
// dumpScrollbackTruncated line naming how many rows were shown, held back and
// held in total.
//
// WHY A SEPARATE ENTRY AND NOT A FLAG ON Text. The two callers want opposite
// things from the same bytes: `ui_state` and the replay self-check need the
// artifact whole (a truncated history is not the history), while the chat
// surface's `action=see` tool return is one message in a model's context, and a
// 24-row screen that arrives with 59,497 scrollback rows measures ~2.8 million
// characters for one glance (measured 2026-11-06). Neither behaviour is the
// "right" default, so the bound is asked for by name at the caller that needs it
// and Text keeps its complete reading.
//
// A negative maxScrollback is dumpScrollbackUnbounded: the scrollback section is
// written whole and no truncation line appears, i.e. the same bytes Text emits.
func (d ScreenDump) TextScoped(maxScrollback int) string {
	return d.text(maxScrollback)
}

// text is the ONE renderer both entry points share: the bound is the only thing
// that differs, so a section added here reaches both renderings and the two can
// never drift.
func (d ScreenDump) text(maxScrollback int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s v=%d %s=%d %s=%d\n",
		dumpHeaderPrefix, d.Version, dumpColsLabel, d.Cols, dumpRowsLabel, d.Rows)
	fmt.Fprintf(&b, "%s %s=%d %s=%d\n",
		dumpCursorLabel, dumpRowLabel, d.Cursor.Row, dumpColLabel, d.Cursor.Col)
	fmt.Fprintf(&b, "%s %s=%d %s=%d\n",
		dumpScrollRegionLabel, dumpTopLabel, d.ScrollRegion.Top, dumpBottomLabel, d.ScrollRegion.Bottom)
	fmt.Fprintf(&b, "%s %s=%t\n", dumpAttrsLabel, dumpRecordedLabel, d.AttrsRecorded)

	dumpSection(&b, dumpLinesBegin, dumpLinesEnd, d.Lines)
	dumpSection(&b, dumpGridBegin, dumpGridEnd, d.Grid)

	b.WriteString(dumpAttrBegin)
	b.WriteString("\n")
	for row, runs := range d.AttrRuns {
		b.WriteString(dumpRowIndex(row))
		b.WriteString(dumpRowSep)
		for i, run := range runs {
			if i > 0 {
				b.WriteString(dumpAttrRunSep)
			}
			b.WriteString(strconv.Itoa(run.Col))
			b.WriteString(dumpAttrRangeSep)
			b.WriteString(strconv.Itoa(run.Cells))
			b.WriteString(dumpAttrStyleSep)
			b.WriteString(run.Style)
		}
		b.WriteString("\n")
	}
	b.WriteString(dumpAttrEnd)
	b.WriteString("\n")

	// The scrollback section: the whole history, or its most recent
	// maxScrollback rows followed by ONE line that says so. The section markers
	// and the `scrollback_begin lines=` count are printed for BOTH renderings and
	// the count is always the dump's true total — a reader that greps the header
	// learns the size of the history even when the rows are not all here, and a
	// reader that sees dumpScrollbackTruncated knows the rows below it are a tail.
	fmt.Fprintf(&b, "%s %s=%d\n", dumpScrollbackBegin, dumpLinesLabel, len(d.Scrollback))
	from, shown := dumpScrollbackWindow(len(d.Scrollback), maxScrollback)
	for i := from; i < len(d.Scrollback); i++ {
		b.WriteString(dumpRowIndex(i))
		b.WriteString(dumpRowSep)
		b.WriteString(d.Scrollback[i])
		b.WriteString("\n")
	}
	if shown < len(d.Scrollback) {
		fmt.Fprintf(&b, "%s %s=%d %s=%d %s=%d — this rendering caps the scrollback section at %d row(s) and shows the MOST RECENT ones above; the artifact itself (and `gopi ui_state`) carries all %d\n",
			dumpScrollbackTruncated, dumpShownLabel, shown, dumpTotalLabel, len(d.Scrollback),
			dumpOmittedLabel, len(d.Scrollback)-shown, shown, len(d.Scrollback))
	}
	b.WriteString(dumpScrollbackEnd)
	b.WriteString("\n")

	fmt.Fprintf(&b, "%s %s=%d\n", dumpBlindBegin, dumpCountLabel, len(d.BlindSpots))
	for i, spot := range d.BlindSpots {
		b.WriteString(dumpRowIndex(i))
		b.WriteString(dumpRowSep)
		b.WriteString(spot.ID)
		b.WriteString(dumpRowSep)
		b.WriteString(spot.Detail)
		b.WriteString("\n")
	}
	b.WriteString(dumpBlindEnd)
	b.WriteString("\n")
	return b.String()
}

// dumpSection writes a begin marker, the indexed rows, and an end marker.
func dumpSection(b *strings.Builder, begin, end string, rows []string) {
	fmt.Fprintf(b, "%s %s=%d\n", begin, dumpCountLabel, len(rows))
	for i, row := range rows {
		b.WriteString(dumpRowIndex(i))
		b.WriteString(dumpRowSep)
		b.WriteString(row)
		b.WriteString("\n")
	}
	b.WriteString(end)
	b.WriteString("\n")
}

// dumpRowIndex zero-pads a row index to dumpRowIndexDigits. An index wider than
// the padding keeps all its digits, so the mapping row -> prefix stays exact
// even for a grid larger than 999 rows.
func dumpRowIndex(i int) string {
	s := strconv.Itoa(i)
	for len(s) < dumpRowIndexDigits {
		s = "0" + s
	}
	return s
}

// BlindSpotIDs returns the stable IDs of this dump's blind spots, in order.
// A report that must enumerate them reads this rather than copying the strings.
func (d ScreenDump) BlindSpotIDs() []string {
	ids := make([]string, 0, len(d.BlindSpots))
	for _, s := range d.BlindSpots {
		ids = append(ids, s.ID)
	}
	return ids
}

// CursorFieldSep is exported for callers that build an assertion from a dump's
// text rendering: it is the separator between the header block's words. It is
// the ONE spelling of that separator (Rule 14) — a test that split on a literal
// " " would stop matching the moment the rendering changed spacing.
const CursorFieldSep = dumpFieldSep
