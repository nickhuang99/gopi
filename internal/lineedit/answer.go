package lineedit

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/sgr"
)

// answer.go — the scrollback-safe streamed-answer renderer (A5).
//
// The defect it closes: writing every streamed delta straight to the screen
// makes the live region so tall that each delta repaints the whole answer, and
// the answer fights the terminal's own scrolling. The policy, borrowed from the
// codex TUI (`live_wrap.rs` `drain_commit_ready`, :94-108), is one rule:
//
//	A FINALIZED LINE LEAVES THE LIVE REGION AND IS NEVER REPAINTED AGAIN.
//
// The renderer therefore holds only the tail of the answer that is still being
// written (the in-flight line), and commits everything before it exactly once.
// The streamed deltas cost one write each — the committed text is not re-sent,
// not rewritten, and (with the region installed) not even movable: see the
// region's scroll-region invariant.
//
// HONEST BOUND (what this buys and what it costs): the answer band is the
// region between the editing chrome and the status row, so an answer longer
// than the band scrolls the band and its oldest visible rows are pushed out of
// view. The terminal's scrollback is not grown by the answer, because the live
// row deliberately sits outside the scroll region (that is what keeps advancing
// a line from scrolling the live row into the band). The win is the one the
// increment was for — no repaint of an ever-taller answer, no fight with
// auto-scroll, and an explicitly reserved status row — and the devdoc states
// this trade-off in full.
//
// It is deliberately terminal-free: it emits bytes through an io.Writer and
// tracks a one-line status string. Screen-content truth is asserted by feeding
// those bytes into Screen (the vt100 cell grid), which is the roadmap's
// verification recipe (§5.7) — "the callback fired" cannot see a repainted row.

// Renderer bounds and cadence. Every window and bound is named (Rule 1): these
// are exactly the values a future reader must be able to find in one place.
const (
	// answerFlushWindow is how long a completed word may sit buffered before
	// the in-flight line is flushed — the readable "pop-in" cadence. Long
	// enough for a word to arrive, short enough that the answer never lags.
	answerFlushWindow = 30 * time.Millisecond
	// answerMaxBufferedBytes caps the untouched in-flight text. Beyond it the
	// renderer writes through immediately, so a very long uninterrupted line
	// still shows progress (and the buffer never grows without bound).
	answerMaxBufferedBytes = 512
	// answerMaxHeldLineBytes caps how long an UNBROKEN line may be held back
	// for the whole-line decision (drain → unfinishedLineWouldWrap). It is a
	// backstop, not a policy: the hold ends the moment the line's break
	// arrives, which for a real stream is within a few deltas, and a line
	// longer than this is held no further — it takes the piecewise provisional
	// paint rather than making the decision buffer grow without bound.
	answerMaxHeldLineBytes = answerMaxBufferedBytes * 4
	// rowEstimateDivisor and lineSegmentsSlack size the per-burst row slice:
	// roughly one row per this many bytes, plus a fixed floor so a short burst
	// does not allocate at all. Both are hints for the allocator only — the
	// slice grows if the estimate is short, so neither can truncate an answer.
	rowEstimateDivisor = 24
	lineSegmentsSlack  = 2
	// answerMaxPrefixBytes caps how many bytes the renderer will hold back as a
	// possibly-split multi-byte rune. A real UTF-8 rune is at most utf8.UTFMax
	// bytes, so the effective hold-back is min(RuneSafeCut, this) — the bound
	// exists so a pathological "unfinished rune" tail can never accumulate.
	answerMaxPrefixBytes = 4
)

// Turn structure constants. They are NAMED (Rule 1) because they are the
// text-UI's layout policy: the marker every turn's answer starts with, the rule
// that ends a turn, and the minimum content width the turn header needs before
// it degrades to the bare label. A future reader changes a glyph here and the
// header, the rule and the tests all follow — nothing is spelled inline.
const (
	// TurnMarker is the bullet every turn's FIRST committed line is prefixed
	// with. It is what makes two consecutive turns distinguishable in the band:
	// without it the next turn's opening paragraph simply continues the previous
	// turn's last line, and the reader cannot tell where one ended (the measured
	// defect: row 11 "Done." of turn 1 immediately followed by row 12 "Here is
	// the answer …" of turn 2, with no separation at all).
	TurnMarker = "▌ "
	// TurnMarkerSplit marks a committed line that CONTINUES the turn's first
	// line — a wrap, or a line that arrived while the marker was being written.
	// It is exactly as wide as TurnMarker (two cells), so the marker column of a
	// turn lines up with itself down the whole answer: a wrapped bullet list
	// keeps its left edge, which is the "column alignment" half of the turn
	// structure.
	TurnMarkerSplit = "▌ "
	// ThinkingGutter is the marker a committed REASONING line carries. It is the
	// same two cells as the answer gutters, so a turn's left edge stays one
	// column whether the line is the model thinking or the model answering, and
	// the glyph (rather than the column) says which one it is.
	ThinkingGutter = "· "
	// NoteContinuation is the gutter a NOTE row carries when it CONTINUES the
	// note row above it (a tool-activity row wider than the band). It is exactly
	// as wide as every other gutter — the note glyph column is one column down
	// the whole activity block — and the glyph is what separates a continuation
	// from a fresh row: `⚙ call bash …` that runs off the terminal is followed by
	// `↳ rest of the command`, so a reader (and a copy-paste) can see at a glance
	// that the second row is the same command's tail rather than a second call.
	//
	// Why the marker exists at all: the wrap is only HALF the defect. The other
	// half is that the continued row looked exactly like any other note row, so
	// the transcript's record of "what ran" was ambiguous even when the text
	// itself was whole (the operator's dump: the continuation of a long bash row
	// began with the same blank two-cell indent as the next `⚙` row).
	//
	// IT IS DOCUMENTED, NOT INVENTED. `↳` is one row of NoteRowLegend, the
	// activity block's own glyph table below — the enumeration a reader (and the
	// operator filing a defect against it) can consult. `↳` rather than `…`
	// because the row is NOT short: it is the same command continued, and `…`
	// already means "cells were dropped" one line up in NoteCutMarker.
	NoteContinuation = "↳ "
	// TurnRuleGlyph is the glyph the between-turns rule is drawn from.
	TurnRuleGlyph = "─"
	// turnLabelLead is the spacing between the rule and the label (and the
	// spacing of the label's closing run into the rule when the label follows).
	turnLabelLead = " "
	// turnLabelTail is the spacing between a rule label and the closing rule.
	turnLabelTail = " "
	// turnRuleMinCells is the fewest cells the closing rule run may occupy. A
	// closing run shorter than this is not a rule but a stray glyph, so a narrow
	// terminal drops the run and keeps the label: the labelled-half degradation
	// (turnLabelHalfCells) has to look deliberate, never broken.
	turnRuleMinCells = 4
	// turnLabelHalfCells is the width below which a header cannot carry a rule on
	// both sides of the label: it degrades to ONE lead rule plus the label
	// instead of two stubs.
	turnLabelHalfCells = 24
	// turnLabelLeadCells is how many cells of lead rule a degenerate (very
	// narrow) header keeps before the label; below it the rule is dropped
	// entirely and the label stands alone, which still separates the turns.
	turnLabelLeadCells = 2
	// turnHeaderMinCells is the width below which the header omits the rule
	// altogether and prints the bare label (a rule that renders as a single
	// glyph beside a label reads as damage).
	turnHeaderMinCells = 8
)

// TurnInfo describes the turn whose answer is about to stream. It is passed to
// BeginTurn; the zero value (TurnIndex == 0) means "the caller does not keep a
// turn counter", in which case the renderer emits NO header and NO marker and
// behaves exactly as it did before turn structure existed. That degradation is
// deliberate and stated: a piped/cooked session has no band to frame.
type TurnInfo struct {
	// Index is the 1-based turn number (the REPL's own counter).
	Index int
	// OpenedAt is WHEN the turn-opening row was written — the moment the band
	// header prints between the ordinal and the prompt
	// (sessionevent.FormatTurnStamp renders it, the SAME function the web turn
	// banner uses, so the two faces cannot spell one turn's time two ways).
	//
	// IT IS READ FROM THE RECORD, NEVER FROM A CLOCK. The value reaches a caller
	// from gopilog.Iteration.TurnOpenedAt, i.e. the timestamp of the row that
	// OPENED the turn, so a replay of a session from last week prints last
	// week's moment. A caller that has no stamp leaves this zero and, with Stamp
	// set, the header prints the explicit placeholder — never the current time,
	// which would be a banner stating the wrong day (Rule 15).
	OpenedAt time.Time
	// Stamp declares that this header states WHEN the turn opened, and it is a
	// separate bit from OpenedAt for one reason: the zero value must keep the
	// pre-existing label EXACTLY. A caller that only wants the rule geometry (a
	// probe, a header rendered with no record behind it) sets neither field and
	// gets the bytes it always got; a caller that HAS the turn's row sets Stamp
	// and lets OpenedAt say what to print — the moment, or the placeholder when
	// the row recorded none.
	//
	// It is the criterion, not a convenience: "this header carries a moment" and
	// "the moment is 2026-11-08 12:31:20" are two different statements, and a
	// surface that conflated them could not print an absence at all.
	Stamp bool
	// Prompt is the user's message, as typed. The header shows it as a label so
	// the band records what was asked, not only what was answered — the
	// scrollback keeps the turn's question beside its answer.
	//
	// It is the message AS TYPED, one line: the header does not expand it, and a
	// spilled paste's reference stays the reference. The block's continuation
	// rows (TurnHeaderLines) are derived FROM this field, so the struct is
	// unchanged by the multi-line header and every existing literal keeps its
	// exact meaning — the zero value is still "name no turn, draw no chrome".
	Prompt string
	// Spill describes the turn whose input was a PASTE SPILL rather than text
	// the operator typed: the bytes are on disk, the input line carries only the
	// reference `[paste N: /abs/path]`, and the header must say so in its own
	// SHAPE — a reader who cannot tell a spilled turn from a typed one has been
	// shown a file path where the turn's question should be, which is exactly the
	// defect this field closes (see spillHeader).
	//
	// The zero value (Spilled false) is the ordinary turn, and the ordinary
	// header's rendering is untouched by anything here. Every field below is
	// MEASURED by the caller that knows the payload — the file's own counts and
	// its opening lines — and never re-derived by the renderer, which has no
	// access to the payload and must not guess its size.
	Spill TurnSpill
}

// TurnSpill is what one paste spill's header needs in order to describe the
// turn's input instead of repeating it: how big the payload was, where it landed,
// and enough of its opening lines to recognise the content by.
type TurnSpill struct {
	// Spilled marks the turn's input as a paste spill. It is the ONE bit the
	// renderer switches form on; every other field is read only when it is set.
	Spilled bool
	// Bytes is the size of the spilled file in BYTES, verbatim as pasted; Runes
	// is how many runes those bytes decode to. BOTH are shown: an operator pasting
	// a log reads bytes and one pasting CJK text reads runes, and the pair is also
	// the cheap check that the file holds what was sent.
	Bytes int
	// Runes is how many runes the spilled bytes decode to.
	Runes int
	// Lines is how many lines the payload holds (a payload with no newline is one
	// line, not zero). It is what the elision marker counts down from, so it is
	// stated even when no line of the head fits.
	Lines int
	// Head is the payload's opening lines, in order, one entry per line, each
	// already stripped of its trailing newline. It is the ONLY part of the payload
	// the header may show, and the header decides how many entries fit — the rest
	// is MARKED, never silently dropped (turnSpillHeadMaxLines bounds the reader's
	// capture; the header's own cell budget bounds the display).
	Head []string
	// Handle is the header's handle on the bytes: the ABSOLUTE PATH of the
	// spilled file, cut to the band's budget with the row's own marker when it
	// does not fit.
	//
	// It is deliberately the PATH and not the reference line verbatim. The
	// reference spells its own size (`[paste N: /abs/path]`), and that N is the
	// count the editor had when it last rewrote the reference — it lags the file
	// while a paste is still arriving, which is exactly the stale number this
	// header exists to replace. Reprinting the reference would put that number
	// back on the screen beside the verified one.
	Handle string
}

// StatusSep separates the status line's fields. It is defined HERE — the
// package that composes and truncates the row — and read by the callers that
// build the label (Rule 14: a shared value has exactly one definition). The
// field split in fitStatusFields and the join in the caller therefore cannot
// drift apart.
const StatusSep = StatusSepAir + StatusSepGlyph + StatusSepAir

// StatusSepGlyph is StatusSep's separator rune and StatusSepAir the spacing
// around it. They are split out because a SECOND separator is derived from them
// (StatusSepGroup, below) and a Go constant cannot be sliced: composing both
// spellings from these two leaves the row with ONE source for the glyph, so a
// change to the separator's look carries to both instead of leaving the group's
// description pointing at a string the row no longer draws. The composition
// order is stated once, by StatusSep — air, glyph, air — so a reader sees the
// exact string the row draws rather than inferring it from the pair.
const (
	StatusSepGlyph = "·"
	StatusSepAir   = " "
)

// statusEllipsis marks a status field the row had to cut short. It is the
// difference between "the terminal is narrow" and "this is corrupted data":
// "⚙ read /home/user/work/GoPi/interna" reads as a broken path, while
// "⚙ read /home/user/work/GoPi/interna…" reads as a field that continues. It is
// one display cell wide — a hard requirement, because the row's one-screen-row
// guarantee is arithmetic on cell widths (see fitField).
const statusEllipsis = "…"

// NoteCutMarker is the glyph a STATUS-row field carries at its END when the row
// has to stop in the middle of a token that offered no boundary to break at. It
// is the status row's own "this field was shortened" marker, referenced rather
// than re-spelled, so the transcript keeps ONE vocabulary for a cut: a field ends
// with this marker exactly when something follows it that the row could not carry,
// and a field that ends WITHOUT it is either whole or a break the text itself
// offered.
//
// IT IS NO LONGER A NOTE-ROW GLYPH, and the removal is deliberate rather than a
// loss of vocabulary. A status field is one screen row by construction and DROPS
// its tail (fitField), so its marker stands for text that is gone. A note row does
// not drop anything — the tail continues on the next row, which carries
// NoteContinuation — so a marker here was drawn INSIDE the note's own cells while
// standing for nothing, and a reader selecting those cells copied it into the
// command (the 112-column grid: `… && find . -ma…` | `xdepth 2 …`, which pastes as
// `-ma…xdepth`). The grid is the artifact, so the note path stopped drawing it —
// see cutAtBoundary's last arm and copied_text_test.go.
const NoteCutMarker = statusEllipsis

// noCutLabel is "this row wears no cut label": the zero value of the label the
// body's cut path carries (writeRow), named because it is compared against and
// returned as a value — the alternative is a bare "" at five sites, which reads
// as "no text" at exactly the place that means "no MARKER" (Rule 1).
const noCutLabel = ""

// StatusEllipsis is statusEllipsis for callers OUTSIDE this package: the chat
// status row's own tests assert that a reserved column was not cut, and they must
// read the marker from the one place that renders it rather than spelling "…"
// again (Rule 14 — a second spelling is a second definition).
const StatusEllipsis = statusEllipsis

// NoteRowLegend is the activity block's GLYPH TABLE: every gutter a note
// (tool-activity) row can carry or be affected by, each with what a reader should
// read it as. It exists because the operator's bug row named `↳` an UNDEFINED
// symbol — the renderer printed a glyph that no table, help text or document said
// anything about, so a reader had no way to learn what it meant.
//
// It is a VALUE, not prose (Rule 14): every glyph below is the constant the
// renderer itself draws with, so the table cannot drift from the screen — a
// renamed glyph renames its row here, and TestNoteRowLegendNamesEveryGlyph
// asserts each entry is present. Nothing is spelled twice.
//
// The block reads, line by line:
//
//	⚙ call bash cd ~/work/GoPi ; echo "=== branch ===" ; echo "=== worktrees ===" ;
//	↳ echo "=== does the branch exist anywhere locally? ===" ; echo fix/pushback
//
// The cut marker's row is the table's SUBTRACTION as much as its content: a note
// row never wears it any more (that is what makes the two rows above pastable),
// so the row says where the glyph lives and, in the same breath, that the reader
// will not find it here. A table that quietly dropped it would leave the status
// row's `…` unexplained, and a table that kept describing it as a note glyph would
// send the reader looking for a mark the fix removed.
const NoteRowLegend = "" +
	ToolGlyph + "  a fresh tool-activity row: the call STARTED here.\n" +
	NoteContinuation + "  this row CONTINUES the note row above it (that row was wider than the band);\n" +
	"     the rest of the same command or path is on this row — so a note's rows\n" +
	"     join back into the whole command, with nothing missing and nothing added.\n" +
	NoteCutMarker + "  the STATUS row's marker, and NOT a note row's: a status field is one\n" +
	"     screen row, so the field's tail is dropped. A note row never carries it —\n" +
	"     its tail is on the row below (previous glyph), and a marker here would be a\n" +
	"     character the reader copies into a command that no shell runs.\n" +
	"     A note row carrying NEITHER gutter is whole.\n"

// Status is the one-line status shown on the region's reserved last row. It is
// assembled from counters the chat REPL already keeps (the model name, the
// turn counter, the running token total), so the status line adds no state of
// its own.
type Status struct {
	// Label is the left part (the rolling hint: model, cadence, turn count).
	// It is the UNALIGNED form: it is what the row shows when Columns is empty,
	// and it is what a narrow terminal degrades to (see statusLine).
	Label string
	// Live is the right part (what the agent is doing right now).
	Live string
	// LabelEmphasis is what the label's MEASURED-QUALITY spans are wrapped with,
	// and it is EMPTY for every caller that has no such span — "" means "the
	// palette's own StatusLabel/ToolRow emphasis, exactly as before", so this
	// field changes nothing until a producer names one.
	//
	// WHY THE PRODUCER DECIDES (cmd/agent's cache hit rate, the row's first
	// tiered emphasis): colour must follow a MEASUREMENT, and only the producer
	// knows which span of the label is one. The palette styles a field WHOLE
	// (styledFields splits the label on StatusSep), while the score rides INSIDE
	// the metrics column, which is one field — so a renderer that re-derived the
	// score's position from the text would be matching on a string, the same
	// defect Thinking's note rejects for the live hint. The producer therefore
	// composes the styled label (LabelEmphasis is its opening sequence) and hands
	// the layout the UNSTYLED row through PrepareLabel, so the cells measured and
	// the cells drawn are the same row.
	LabelEmphasis string
	// Emphasis is the style the LabelEmphasis span wears. It is resolved by the
	// PRODUCER (Status.WithEmphasis) from this palette's tier fields —
	// StatusGood / StatusFair / StatusBad — because choosing the tier is a fact
	// about the MEASUREMENT ("is 4% a hit rate to worry about?") while choosing
	// the hue is a fact about the palette ("which green is `ok` here?"). The zero
	// Style is the no-emphasis state and emits nothing.
	Emphasis Style
	// Thinking says the live hint describes work IN FLIGHT ("the agent is
	// running") rather than the idle prompt. It is the producer's own fact —
	// the REPL knows whether a turn is running — and it exists so the emphasis
	// for the two states is chosen by the SOURCE rather than guessed from the
	// hint's words (matching on "thinking…" would be a bare-string keyword, and
	// it would silently drop the emphasis the day the wording changed). It is
	// ORTHOGONAL to the column layout below: Thinking decides the STATE the row
	// is declaring, the columns decide the CELLS the row is drawn in, and one
	// Status carries both so neither fact needs a second field on the source.
	Thinking bool
	// Columns, when non-empty, is the LEFT part's column layout: each entry is
	// one field (model, iteration, mode, tool, tokens) rendered in a FIXED width,
	// so a field appearing or vanishing — the tool row rising while a tool runs —
	// or growing a digit (iter 9 → iter 10) cannot shift the fields after it.
	// That is the "the columns move while the agent works" defect this closes:
	// the row was a single concatenation, so every repaint that changed a field's
	// LENGTH slid everything to its right.
	Columns []StatusColumn
	// LiveWidth reserves a fixed block at the row's RIGHT edge for Live (the
	// hint is left-padded inside it), so the live hint's own column is stable too
	// — "thinking…" and "ready" occupy the same cells instead of dragging the
	// right edge around.
	LiveWidth int
	// LayoutCols pins the width the aligned layout is composed at. The reserved
	// column widths describe ONE geometry, and composing at the terminal's
	// current width would move every field's start cell the moment the window is
	// resized — which would make "the columns hold still" true only on the
	// terminal the assertion happened to run on. layoutCols() centralizes that
	// choice: the pinned width when set, the caller's own below it.
	LayoutCols int
}

// layoutCols is the width the aligned status layout is composed at: the pinned
// geometry when the composer named one, otherwise the caller's width — and never
// MORE than the caller's width, because the terminal is the row's hard bound. On
// a terminal narrower than the pinned layout the aligned form is asked to fit
// where it is (and refuses), instead of being composed at 125 cells on a
// 60-column terminal — a row wider than the screen wraps, which costs the
// editing line a row, which is the one thing the layout must never sacrifice.
func (s Status) layoutCols(cols int) int {
	if s.LayoutCols <= 0 {
		return cols
	}
	if cols <= 0 || s.LayoutCols < cols {
		return s.LayoutCols
	}
	return cols
}

// Text renders the status line as the single line actually shown. With Columns
// set it composes them at their natural widths (the unaligned form: Text is the
// plain-text rendering, and the aligned layout is produced by statusLine, which
// is the only thing that knows the row's width).
func (s Status) Text() string {
	label := s.PrepareLabel(MonoPalette)
	if label == "" {
		return s.Live
	}
	if s.Live == "" {
		return label
	}
	return label + StatusLineGap + s.Live
}

// PrepareLabel returns the label with its measured-quality span emphasised — the
// UNSTYLED label when the producer named no emphasis, or when the caller renders
// monochrome (MonoPalette).
//
// IT IS THE LAYOUT'S OWN PREPARATION, not a second rendering path: the plain row
// (statusLine) and the styled row (styledStatusLine) both start here, and the
// emphasised span is composed from the UNSTYLED field text, so the SGR bytes can
// never be measured as display cells (the shear defect this package's ordering
// exists to prevent).
//
// The span is FOUND in the plain label rather than re-composed from the values,
// because a second composition of one field is how the drawn row and the
// measured row drift apart: the producer's span text is the same field the
// aligned columns carry (cmd/agent composes both from statusCacheField).
func (s Status) PrepareLabel(pal Palette) string {
	label := s.Label
	if len(s.Columns) > 0 {
		// The ALIGNED row's label is the columns joined: the producer composes each
		// column's text, so its emphasis rides in the column (cmd/agent composes
		// the metrics column's styled span there). The plain Label is not used on
		// this path — it is the fallback composition's own row.
		texts := make([]string, 0, len(s.Columns))
		for _, c := range s.Columns {
			if c.Text != "" {
				texts = append(texts, c.Text)
			}
		}
		label = strings.Join(texts, StatusSep)
	}
	return s.emphasiseLabel(label, pal)
}

// Emphasis is the style the LabelEmphasis span wears — already resolved to this
// palette's own tier by the producer (Status.WithEmphasis), never chosen here:
// which tier a measurement falls in is the producer's fact, which HUE that tier
// is is the palette's. The zero Style emits nothing, so a Status built without
// one behaves exactly as it did before this field existed.
func (s Status) emphasiseLabel(label string, pal Palette) string {
	if s.LabelEmphasis == "" || s.Emphasis.Empty() {
		return label
	}
	if !strings.Contains(label, s.LabelEmphasis) {
		// The span the producer marked is not in this label: the unaligned
		// fallback composes its own tail (statusCtxBase) and may spell the score
		// differently. Nothing is guessed and nothing is emitted — the label is
		// returned whole, which is the honest answer when the emphasis has no
		// span to apply to.
		return label
	}
	return strings.ReplaceAll(label, s.LabelEmphasis, s.Emphasis.Wrap(s.LabelEmphasis))
}

// Line renders the status row within cols display cells — the row as it is
// actually drawn (the aligned column layout when it fits, the historic
// field-dropping composition when it does not). It is the exported form of the
// row's composition, so a caller (or a test) can assert the CELLS a row occupies
// without re-deriving the layout arithmetic.
func (s Status) Line(cols int) string { return statusLine(s, cols) }

// StyledLine renders the row with the palette's emphasis — the exported form of
// what the region path paints (styledStatusLine). It exists because the row's
// emphasis is decided in TWO places that must agree: the producer names WHICH
// span is a measurement and which tier it falls in (Status/StatusColumn), and
// this package turns that into sequences. A caller asserting that its own
// producer-side decision actually reaches the screen otherwise has no way to see
// the styled row short of driving a terminal — and a channel that is inert on the
// real path is exactly the silent failure those assertions exist to catch.
//
// It is the SAME function the renderer calls, never a second implementation: one
// composition means a producer-side assertion and the painted row cannot disagree.
func (s Status) StyledLine(cols int, pal Palette) string { return styledStatusLine(s, cols, pal) }

// StatusColumn is one field of the status row's aligned layout (Status.Columns):
// the field's text and the display cells reserved for it. Width 0 means "the
// text's own width" — the field is laid out as-is and takes no part in the
// column arithmetic.
type StatusColumn struct {
	Text  string
	Width int
	// EmphasisSpan is the substring of Text that is a MEASURED QUALITY of this
	// column's own read-out rather than part of the field's identity, and
	// Emphasis is the style that span wears. Both are the PRODUCER's facts: it
	// knows which cells of the field it composed are a measurement and how the
	// measurement was graded; this package knows only how to draw.
	//
	// WHY THE EMPHASIS TRAVELS WITH THE COLUMN. The aligned row's cells are
	// composed by the producer (Text/Width), and styledFields wraps a whole
	// FIELD by its identity (the label, or tool activity) — so a score that
	// rides INSIDE one field has no way to wear a style chosen from its own
	// VALUE. The alternative that looks simpler — wrap the field first, then
	// splice a second sequence around the score (a ReplaceAll over the styled
	// text) — is WRONG, and measurably so: the inner sequence closes with a
	// reset, which ends the OUTER field's colour as well, so every character of
	// that field AFTER the score drops to the terminal's default. That is the
	// nested-reset defect.
	//
	// So the span is emphasised by RE-STATEMENT, never by interior splicing: the
	// row walks the field's text and re-opens the enclosing field style after
	// each emphasised span (statusFieldEmphasis below), which keeps the
	// remaining characters of the column in the column's own colour no matter
	// how many spans it carries.
	//
	// An empty EmphasisSpan or the zero Emphasis is the no-emphasis state and
	// emits exactly the bytes this column emitted before the fields existed —
	// which is what keeps the monochrome path byte-identical.
	EmphasisSpan string
	Emphasis     Style

	// EmphasisSpans is the SAME channel for a field that carries MORE THAN ONE
	// measured quality — the chat row's two hit-rate readings (this session's
	// cumulative rate and this turn's), each graded from its own value and
	// therefore possibly wearing a different tier. The producer names each span
	// and its style; when the list is non-empty it is the channel, and the single
	// EmphasisSpan above is the one-span spelling of it (a field that carries one
	// measurement keeps writing the two fields it always did).
	//
	// WHY A LIST AND NOT TWO FIELDS: the number of measurements a field carries is
	// the PRODUCER's fact, and a struct with two span fields would have to be
	// widened again for the third. The walk below is the same walk either way —
	// find the span in the remaining text, wrap it, re-state the enclosing field —
	// so one list costs no new rendering rule.
	EmphasisSpans []StatusEmphasisSpan
}

// StatusEmphasisSpan is ONE measured quality inside a status field's text: the
// exact substring the producer composed, and the style it wears. Both are the
// producer's facts (see StatusColumn.EmphasisSpan); this package only draws them.
type StatusEmphasisSpan struct {
	Text  string
	Style Style
}

// emphasisSpans resolves the field's effective span list: the multi-span channel
// when the producer used it, and otherwise the single-span fields as a one-element
// list — so a renderer walks ONE list and the two spellings cannot draw
// differently.
func (c StatusColumn) emphasisSpans() []StatusEmphasisSpan {
	if len(c.EmphasisSpans) > 0 {
		return c.EmphasisSpans
	}
	if c.EmphasisSpan == "" || c.Emphasis.Empty() {
		return nil
	}
	return []StatusEmphasisSpan{{Text: c.EmphasisSpan, Style: c.Emphasis}}
}

// BudgetWidth is the pinned width of the application's status row, in display
// cells: the terminal width the row's whole geometry is designed around, and the
// number every narrower terminal degrades from. It is a definition HERE — the
// package that owns cell arithmetic and the row's composition — and read by the
// callers that pin a geometry to it (Rule 14: a shared value has exactly one
// definition, in the package every consumer can import).
//
// It is not a preference: 100 columns is the width the layout's own grid
// evidence is captured at, and the defect it exists to stop is a caller pinning
// its geometry one cell past it (measured: the status geometry was built for 101,
// which meant the aligned row never appeared on the 100-column terminal it was
// designed for). A caller that pins to this constant cannot restate it wrong; a
// caller that retypes 100 is the bug, not a copy of it.
const BudgetWidth = 100

// StatusLineGap separates the status row's field block from the live hint. It is
// one definition (Rule 14): the unaligned composition and the aligned one must
// put the same gap between the fields and the hint, or a repaint that switches
// between them would move the hint.
const StatusLineGap = "  "

// The separators' widths in DISPLAY CELLS. A layout's geometry is arithmetic on
// cells, and a caller's pinned width must be a COMPILE-TIME constant (so the
// budget check below can be a compile error), while a string's width can only be
// measured at run time — hence the pair. The numbers are not free-standing: a
// caller that used len() on these strings instead came out one cell too wide per
// separator, which is how the pinned status geometry ended up past a
// 100-column terminal (see statusColumnsAtCols). Keeping the pair in ONE place
// and tying it to the strings is the point; TestStatusSeparatorCellWidths
// asserts the agreement, so a separator edit that forgets the constant fails.
const (
	// StatusSepCells is StatusSep's width in cells.
	StatusSepCells = 3
	// StatusColumnSepCells is StatusColumnSep's width in cells: StatusSep plus
	// the one cell of air it adds.
	StatusColumnSepCells = StatusSepCells + 1
	// StatusLineGapCells is StatusLineGap's width in cells.
	StatusLineGapCells = 2
	// StatusSepGroupCells is StatusSepGroup's width in cells, derived from
	// StatusSepCells rather than restated: the group separator IS the field
	// separator minus its trailing cell of air, so its width is that one less —
	// and a width a layout is built on has no business being a number a reader
	// keeps in step by hand. It is stated here because a caller's geometry is
	// COMPILE-TIME arithmetic this number is entered into: a group of sub-fields
	// built on a wrong width would overflow its column without any string
	// changing.
	StatusSepGroupCells = StatusSepCells - 1
)

// StatusSepGroup is the light separator that joins the SUB-FIELDS inside one
// column of the aligned status row, in the pair to the heavier StatusColumnSep
// that joins the columns themselves. It is one definition (Rule 14) because the
// distinction it draws IS the row's contract: a top-level column boundary is a
// column boundary (StatusColumnSep), a boundary inside a column is a field
// boundary (this). Two columns of meaning, two spellings.
//
// It is composed from the same two constants as StatusSep (air + glyph, without
// the trailing air) rather than a retyped " ·": that keeps ONE source for the
// separator's glyph, so a change to how the row separates things carries here
// automatically instead of leaving this constant describing a string the row no
// longer draws.
const StatusSepGroup = StatusSepAir + StatusSepGlyph

// StatusColumnSep separates two columns of the ALIGNED status row. It is
// StatusSep plus one cell of air, and the extra cell is not decoration: an
// aligned layout whose arithmetic is "the sum of the reserved widths" must be
// able to state its own total, and a separator that renders TWO cells while the
// fields around it are reserved in whole cells makes that total a half-claim
// (measured: the pinned width computed from StatusSep alone came out one cell
// short per pair, so the row's tail drifted by four cells).
const StatusColumnSep = StatusSep + " "

// padStatusColumn renders one column's text into exactly width display cells:
// padded on the right when it is short, cut with the row's ellipsis marker when
// it is long (so a truncated field reads as continuing, exactly as fitField
// guarantees for the unaligned row). A width <= 0 means no reservation.
func padStatusColumn(text string, width int) string {
	if width <= 0 {
		return text
	}
	w := displayWidth(text)
	switch {
	case w == width:
		return text
	case w < width:
		return text + strings.Repeat(" ", width-w)
	default:
		return fitField(text, width)
	}
}

// statusLineColumns renders the ALIGNED status row: the fixed-width field
// columns, then the live hint right-aligned inside its own reserved block so its
// left edge is a column of its own.
//
// It is TOTAL in cols like statusLine, and it degrades in ONE honest step: when
// the aligned form does not fit (a narrow terminal), the row falls back to the
// historic unaligned composition, which drops whole fields from the right and
// keeps the live hint — i.e. on a small terminal the row shows what it always
// showed, and the alignment is what a wide terminal adds. Falling back rather
// than dropping columns here is deliberate: dropping a padded column would keep
// its padding's cost while losing its information, which is the worst of both.
//
// ok is false when the caller must use the unaligned path.
func statusLineColumns(s Status, cols int) (string, bool) {
	if len(s.Columns) == 0 {
		return "", false
	}
	// The form is chosen from the TERMINAL WIDTH alone (a terminal that hosts
	// the pinned geometry shows aligned columns on every repaint; one that does
	// not shows the historic composition on every repaint). That is deliberate:
	// deciding per repaint on "does today's value fit" would flip the row's form
	// when a token count appeared — the columns would move exactly at the
	// transition, which is the defect this layout closes.
	//
	// The comparison is against the width the caller PINNED (LayoutCols), never
	// against the width of the columns that happen to be present: a column list
	// is a function of the VALUES (the token field appears when a turn reports
	// tokens, the tool field when a tool runs), so measuring today's list would
	// make the row's form a function of the values after all — measured, the
	// form flipped on one and the same 100-column terminal the moment the token
	// field arrived (aligned 97 cells → unaligned 50). Pinning means "this
	// geometry, or the historic composition": one terminal, one form, for the
	// whole session.
	if !s.alignedFits(cols) {
		return "", false
	}
	layout := s.layoutCols(cols)
	live := s.Live
	if s.LiveWidth > 0 {
		live = padStatusColumn(live, s.LiveWidth)
	}
	// EVERY column occupies its reserved cells, a valueless one included: the
	// field the row has no value for is rendered as its own width in spaces, so
	// the columns after it stay where they are (a skipped column would pull the
	// rest of the row left by that width — measured).
	fields := make([]string, 0, len(s.Columns))
	for _, c := range s.Columns {
		text := c.Text
		if text == "" {
			text = strings.Repeat(" ", max(c.Width, 0))
		}
		fields = append(fields, padStatusColumn(text, c.Width))
	}
	left := strings.Join(fields, StatusColumnSep)
	pad := layout - displayWidth(left) - displayWidth(live)
	if pad < displayWidth(StatusLineGap) {
		return "", false
	}
	if displayWidth(left)+displayWidth(live)+displayWidth(StatusLineGap) > cols {
		return "", false
	}
	return left + strings.Repeat(" ", pad) + strings.TrimRight(live, " "), true
}

// alignedFits reports whether the terminal hosts the ALIGNED layout: the row is
// composed at the geometry the caller PINNED (LayoutCols), and every column fits
// inside it.
//
// The terminal-width half is what makes the form a property of the terminal
// rather than of the values: below the pin the historic composition is used, so
// the row cannot be asked to render 101 cells into an 80-column screen (a row
// wider than the screen wraps, and a wrapped status row costs the editing line a
// row). The column half guards a caller that pinned a width its own columns do
// not fit inside — its columns are then composed where they must fit or the row
// degrades, but the pinned geometry is never silently truncated.
func (s Status) alignedFits(cols int) bool {
	if s.LayoutCols > 0 && (cols <= 0 || s.LayoutCols > cols) {
		return false
	}
	return s.layoutCols(cols) >= s.alignedMinCols()
}

// alignedMinCols is the narrowest terminal that hosts the aligned layout: the
// reserved widths plus the separators that join them. It is DERIVED from the
// layouts themselves (never a hand-copied number), so it cannot drift from what
// the aligned composition actually draws.
func (s Status) alignedMinCols() int {
	total := s.LiveWidth
	for _, c := range s.Columns {
		total += max(c.Width, 0)
	}
	if n := len(s.Columns); n > 0 {
		total += (n - 1) * displayWidth(StatusColumnSep)
	}
	total += displayWidth(StatusLineGap)
	return total
}

// max is the larger of a and b (Go's builtin max landed after this module's
// target; one local definition keeps the arithmetic readable).
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// AnswerRenderer writes a streamed answer so that finished text is never
// repainted: completed lines are committed OUT of the live region exactly once,
// and only the in-flight line stays live.
// answer renders the streamed model answer into the region's live row, and
// editor is the seam back to the queue that decides WHO applies a turn-side call
// (see answerqueue.go — the renderer is routed while a read loop runs).
type AnswerRenderer struct {
	out    *region
	cols   int
	status func() Status

	// editor is the editor this renderer was built by (nil for a renderer a test
	// built alone). It is what lets a turn-side call see that a READ LOOP owns
	// this renderer and publish instead of mutating it — read-only after
	// construction, so it needs no synchronisation.
	editor *Editor

	// pending is the text of the in-flight line that has not been written yet
	// (the word-cadence buffer).
	pending strings.Builder
	// prefix holds finished lines whose commit is briefly deferred: the next
	// delta may continue the line (a \r\n split across deltas), and a line
	// must be committed once, not twice.
	prefix string
	// partial holds the beginning of a multi-byte rune whose remaining bytes
	// are still on the wire (a delta boundary and a rune boundary are
	// independent, and half a rune commits as a replacement character).
	partial string

	// inFlight is true while text has been written to the live row and not yet
	// committed by a line break — the state the renderer's own counter of live
	// rows is derived from.
	inFlight bool
	// liveText is what the live row ALREADY shows (this renderer's own record of
	// it), and liveRow is the row it shows on. They exist so a commit can carry
	// the displayed head FORWARD instead of re-committing it: the in-flight line
	// arrives in bursts and the row may be scrolled or placed between two of
	// them, so re-writing the head puts it in the wrong cell — measured as a
	// literal duplication (`…the read too` + `The path is devdo` on one row)
	// when a mid-line burst boundary coincided with a commit.
	liveText string
	liveRow  int
	// liveCol is the display column the live row's text currently ends at: the
	// in-flight line arrives in several bursts and each must append, not
	// overwrite.
	liveCol int
	// marked is true while the turn's body is being written: every committed
	// line of the turn then carries the marker column (TurnMarker for the
	// turn's first committed line, TurnMarkerSplit for the rest — equal width,
	// so the body's left edge lines up). It is set by BeginTurn and cleared by
	// EndTurn; a renderer driven by Begin (no turn counter) never sets it and
	// commits unmarked lines exactly as before.
	marked bool
	// ledged is true once the turn's FIRST committed line has been written, so
	// the marker is emitted once per turn rather than per line.
	ledged bool
	// thinking is true while the model's REASONING is being streamed: the
	// committed lines then carry the thinking gutter instead of the answer
	// marker, so the two kinds of text are distinguishable at a glance instead
	// of flowing together as one undifferentiated block.
	thinking bool
	// thinkingWrote is true once a thinking line has been committed during the
	// current block: only then does ending the block cost a spacing row (a
	// block that streamed nothing must not add a blank line).
	thinkingWrote bool
	// turnPrompt is the prompt of the turn now open (BeginTurn's own TurnInfo).
	// It is kept because the operator's request row (layers.go's LayerRequest)
	// is drawn from the SAME two values the header is built from, and the header
	// is committed before anything that would carry them again.
	turnPrompt string
	// turnIndex is BeginTurn's own index, kept for the SAME reason the cut-label
	// ledger keeps the row: a record a reader must be able to trace back. An
	// ellipsis on screen is not actionable; "turn 8, row 41, drawn by the
	// renderer" is (answer_cut_labels.go).
	turnIndex int
	// cutLabels counts and locates every cut label this renderer DREW — the one
	// cell on which the drawn row differs from the answer's own text
	// (answer_cut_labels.go). It is written by the read loop at a cut and read by
	// the caller at a turn boundary, which is why it carries its own permit.
	cutLabels cutLabelLedger
	// requested is true once this turn's request row has been drawn. ONE row per
	// turn: it is the operator's own line, and a turn that drew it twice would
	// claim two submissions where there was one.
	requested bool
	// notes is the queue of CALLER rows waiting to be committed into the band
	// (see Note). It is filled from whatever goroutine publishes a note and
	// emptied only by drainNotes, which the read loop drives — so a note never
	// interleaves its writes with the answer's (the two-writer defect this
	// package's publish path exists to remove: the note's producer is the same
	// turn goroutine that streams the answer, and it may not place a cursor
	// while the read loop is drawing a row).
	notes []noteRow
	// notesMu guards the queue above, and it is REQUIRED rather than defensive:
	// the queue's producer is not always the read loop. A FOREIGN caller (a
	// command's own output, the flow seam's WriteLines) appends from its own
	// goroutine, and it even ASKS whether rows are pending (bandwait.go's
	// waitNoteWrite → Noting) while the read loop drains — so an unguarded slice
	// header is read and written from two goroutines with no happens-before.
	// Measured 2026-10-27 (task 01a1756e), `go test ./cmd/agent -race`:
	//
	//	WARNING: DATA RACE
	//	Write at … by goroutine 3905:  AnswerRenderer.drainNotes()
	//	    answer.go:1550 ← Editor.drainNotes ← drainBandFrames ← repaint ← Run
	//	Previous read at … by goroutine 3896:  AnswerRenderer.Noting()
	//	    answer.go:1474 ← Editor.waitNoteWrite ← awaitForeignNote ← NoteRuns
	//
	// The lock is taken for the HEADER only (length, the take, the append): the
	// rows themselves are immutable once queued, and everything drainNotes does
	// with them is on the read loop alone.
	notesMu sync.Mutex
	// noting is true while drainNotes is committing the queued rows. It is the
	// ONLY state in which the marker column is suppressed (see lineGutter): a
	// note writes its own glyph at column 0, so the turn's two-cell gutter would
	// push it one column right of the glyphs it belongs beside — the defect the
	// reference layout's own activity rows do not have.
	noting bool
	// noteContinuing is true while drainNotes is committing the rows a SINGLE
	// note was split into (see writeNote). It only ever moves lineGutter from ""
	// to NoteContinuation, so no existing row shifts by a cell: the turn body,
	// the answer, the thinking gutter and the first row of a note are all drawn
	// exactly as they were, and the gutter appears on rows this defect itself
	// created.
	noteContinuing bool
	// committed counts the committed lines (assertions read it; it is also the
	// cheapest proof that a line was not re-committed). Atomic: the read loop
	// increments it and a caller reads it back (cmd/agent's stream test compares
	// it against the number of lines it streamed), and a plain int there would be
	// the harness's own race — one that would hide the production one the test
	// exists to find.
	committed atomic.Int64
	// writes counts every write the renderer performed, i.e. the repaint cost
	// of the stream. It is the number the bounded-repaint assertion bounds.
	//
	// Atomic because TWO goroutines reach write(): the turn's own goroutine
	// (Delta -> drain -> commitPrefix) and the editor's read loop (Tick ->
	// drawStatus). The counter is the part the race detector flagged; the
	// counter alone is what this file owns — see Writes()'s doc for the
	// byte-interleaving exposure that remains and is NOT fixed here.
	writes atomic.Int64
	// statusWrites counts the status-line repaints that actually wrote (a
	// repaint whose text is unchanged writes nothing). Atomic: it is a
	// test-visible proof, read from the test's goroutine while the read loop
	// increments it.
	statusWrites atomic.Int64

	lastFlush time.Time
	now       func() time.Time
	// drawnStatus is what the status row currently shows (zero value = nothing
	// drawn yet). It is the renderer's own, read-loop-private record: a repaint
	// that would change nothing writes nothing.
	drawnStatus statusKey
	// statusDirty marks a PUBLISH the read loop has not drawn yet. It is
	// written by the publishing goroutine and consumed by the read loop, so it
	// is atomic — it is the ONLY renderer state a second goroutine may touch.
	// A publish therefore never writes to the terminal (that would be a second
	// writer on one fd); it raises this flag, wakes the loop, and the loop
	// draws. Sticky until consumed, so a publish can never be MISSED.
	statusDirty atomic.Bool
	// notify wakes the read loop out of a blocked poll after a publish. A no-op
	// sink means the status reaches the screen on the next wake; the caller can
	// ask (Editor.NotifyEnabled) and say so instead of letting the user wonder.
	notify Notifier
	// statusPublishes counts the publishes this renderer was asked to draw
	// (Editor.PublishStatus → repaint). It is the counterpart of statusWrites:
	// a publish that changes nothing must cost ZERO writes.
	//
	// Atomic because the counter is the DRAW proof a test waits on — read from
	// the test's goroutine while the read loop increments it. A plain int would
	// make the harness itself a data race, and a harness race hides the
	// production one the test exists to find.
	statusPublishes atomic.Int64

	// pal is the renderer's palette: the emphasis applied to the answer body and
	// to the status row. It defaults to MonoPalette (the zero value), so a
	// renderer nobody configured emits NOT ONE SGR byte — the historic output —
	// and an SGR byte in a capture is always the result of a deliberate
	// SetPalette with a colour-capable decision behind it (colour.go).
	pal Palette
	// composer decides, per answer LINE, which body style (if any) it carries.
	// It is an interface so the render path never parses anything (Rule 1b: no
	// format knowledge inside the renderer) and a test can drive any shape.
	// nil means "no composition": the whole body is one Answer-styled stream.
	composer AnswerComposer
	// compPrefix holds an in-flight line whose emphasis cannot be decided yet.
	// A line is decidable only when it is COMPLETE (a fence marker is one whole
	// line), so a partial floor of a code block is parked here until its break
	// arrives — never painted, because a decision that arrived after the
	// characters were on the row would have to insert a sequence BEHIND the
	// cursor and shift what is already displayed. The hold is text only, so a
	// line's EMPHASIS can wait while its characters never do.
	compPrefix string
	// tableRows holds a run of markdown pipe rows whose block is not settled
	// yet: they are painted the moment the run ENDS (by the line that ends it)
	// or at Finish, and until then nothing of them is on the screen (see
	// table.go). It is text state, never layout — what it defers is only which
	// form these rows are drawn in, and a stream that stops mid-table resolves
	// it at Finish rather than dropping the rows.
	tableRows []string
}

// NewAnswerRenderer builds a renderer over a terminal region. out must not be
// nil; status may be nil (then no status row is drawn).
func NewAnswerRenderer(out *region, status func() Status) *AnswerRenderer {
	return &AnswerRenderer{
		out: out, cols: defaultEditorCols, status: status, now: time.Now, notify: noNotify{},
		// The shipped composer classifies the answer body's lines (fences and
		// diffs). It is installed by default because that classification is what
		// the ANSWER surface is for; a caller can drop it (SetComposer(nil)) to
		// draw the body as one unstyled stream.
		composer: &LineComposer{},
	}
}

// SetStatus installs the status source (nil for no status row). It is called
// once, while the editor is being built — before the read loop starts — so it is
// deliberately NOT synchronised: the values the source READS are the caller's
// business (cmd/agent publishes them through PublishStatus).
func (r *AnswerRenderer) SetStatus(fn func() Status) { r.status = fn }

// SetNotification installs the wake seam a publish uses to interrupt the read
// loop's blocked poll. The renderer never calls it itself — only the editor
// does (SetNotification), because the terminal owns the pipe.
func (r *AnswerRenderer) SetNotification(n Notifier) {
	if n == nil {
		n = noNotify{}
	}
	r.notify = n
}

// SetClock replaces the cadence clock (tests drive the flush window without
// sleeping — a millisecond window asserted with a real clock is flaky).
func (r *AnswerRenderer) SetClock(now func() time.Time) {
	if now != nil {
		r.now = now
	}
}

// SetCols publishes the drawing width.
func (r *AnswerRenderer) SetCols(cols int) {
	if cols > 0 {
		r.cols = cols
	}
}

// SetPalette installs the emphasis table the renderer draws with. It is called
// once, while the editor is being built (before the read loop starts), like
// SetStatus. The zero value leaves the historic monochrome output.
func (r *AnswerRenderer) SetPalette(p Palette) { r.pal = p }

// SetComposer installs the answer-body composition (which line carries which
// body style). A nil composer — the default — styles the whole body as Answer.
func (r *AnswerRenderer) SetComposer(c AnswerComposer) { r.composer = c }

// Committed returns how many lines have been committed to the scrollback.
func (r *AnswerRenderer) Committed() int { return int(r.committed.Load()) }

// Writes returns how many writes the streamed answer cost (the repaint count).
//
// HONEST BOUND: the counter is atomic, but the BYTES of a turn's answer and the
// bytes of the read loop's status repaint are still emitted from two goroutines
// without frame coordination (measured under -race once the flake fix let the
// full 30-row stream run to completion: AnswerRenderer.write on the turn's
// goroutine vs AnswerRenderer.write on the read loop). A cursor placement and the
// text it was for can therefore interleave on one fd. That is the same
// two-writer family this package's PublishStatus path removes for the STATUS
// source, and it is NOT fixed here: it needs the answer's writes coordinated
// through the read loop, which is its own task (see the report's "Not Done").
func (r *AnswerRenderer) Writes() int { return int(r.writes.Load()) }

// StatusWrites returns how many of those writes were status-line repaints.
func (r *AnswerRenderer) StatusWrites() int { return int(r.statusWrites.Load()) }

// LiveRow is the row the band's in-flight answer line is written on, read from
// the LAYOUT that owns it (the region), never from a cached copy: the layout can
// move between two writes (the editing line grows and takes rows away from the
// band), and the row is a property of the layout, not of this renderer.
func (r *AnswerRenderer) LiveRow() int {
	if r.out == nil {
		return 0
	}
	return r.out.LiveRow()
}

// Rows is how many rows the answer band occupies — the room an in-flight line
// has before its own scrolling region stops advancing.
func (r *AnswerRenderer) Rows() int {
	if r.out == nil {
		return 0
	}
	return r.out.BandRows()
}

// reanchorLiveRow takes the layout's CURRENT live row and, when it moved,
// re-opens the in-flight line there. It is called at the top of every live-row
// write — the exact seam where the stale row used to be used.
//
// The head the row already displayed is REPAINTED rather than merely forgotten:
// the rows the layout took away belong to the editing line now, and the editor's
// own repaint erases them (`ESC[<editorTop>;1H ESC[2K`), so a line that kept only
// its tail would be committed headless — the second shape this defect produced
// (`▌     bold conclusion follows`). Repainting is safe because the row's own
// text is what this renderer carries, and the row it is painted on is the one the
// layout reserves for it.
//
// The style is the caller's provisional one (the block style of an unfinished
// line, as writeLive uses): the head was painted provisionally, and an unfinished
// line's emphasis is deferred, never guessed.
func (r *AnswerRenderer) reanchorLiveRow(st Style) bool {
	if !r.out.Active() {
		return false
	}
	// The width rides the same layout: a move that changed the live row changed
	// the wrapping width with it, and a row wrapped for the old width would be
	// cut by the new one.
	if cols := r.out.Cols(); cols > 0 {
		r.cols = cols
	}
	row := r.LiveRow()
	if row == r.liveRow {
		return false
	}
	head := r.liveText
	previous := r.liveRow
	r.liveRow, r.liveCol, r.liveText = row, 0, ""
	if head != "" {
		// THE ROW THE HEAD WAS PAINTED ON IS CLEARED FIRST, and it is not
		// tidiness: re-painting the head on the NEW live row leaves the cells it
		// already occupies on the OLD one exactly where they are, and that row is
		// still inside the band. The band scrolls it into the committed reading on
		// its own, so the reader sees the line's beginning TWICE — the
		// left-behind copy first, whole, then the re-painted one — which is the
		// operator's PARTIAL REPEAT (measured 2026-11-10, session
		// 61f5992a-c126-4a55-935e-68fc80bebe19, dump rows 1817/1818: one line of
		// the answer at the band's width, then the same line again beginning a
		// few characters in and opening with the continuation indent). The
		// trigger is the layout transition every submitted message causes — the
		// editing line giving a row back while the answer is mid-line — so it
		// needs neither a wrap in the middle of the stream nor a turn end, which
		// is why the two neighbouring guards (the wrap carry, Finish's carry)
		// never saw it: their fixtures make the line wrap, or never fill a row.
		//
		// Only a row that is STILL the band's is cleared: a move in the other
		// direction (the editing line growing, the band contracting) can leave the
		// old live row below the band's bottom, and that row now belongs to the
		// editing line — erasing it would wipe the user's own input.
		r.clearStaleHeadRow(previous)
		r.writeRow(answerRow{text: head, st: st}, head)
	}
	return true
}

// clearStaleHeadRow erases the cells a carried head left on the row it was
// painted on, once that row is no longer the live one — the ONE place both
// re-anchor paths ask the question (writeLive's and commitPrefix's), so the two
// cannot disagree about whether the stale copy is cleared.
//
// The guard is rowInBand and it is not decoration: a layout move in the OTHER
// direction (the editing line growing, the band contracting) leaves the old live
// row BELOW the band's bottom, and by then that row belongs to the editing line —
// erasing it would wipe the user's own input.
func (r *AnswerRenderer) clearStaleHeadRow(row int) {
	if !r.rowInBand(row) {
		return
	}
	r.placeAndWritePrefixed(escEraseToEnd, row, 0, "")
}

// rowInBand reports whether row is one of the rows the answer band owns RIGHT
// NOW. It is the guard a repaint uses to decide whether the row it is moving off
// is still the band's to clear (see clearStaleHeadRow): the band is the
// contiguous run ending at the live row, so its top is derived from the same two
// numbers the layout publishes, never from a second copy of the arithmetic.
func (r *AnswerRenderer) rowInBand(row int) bool {
	if !r.out.Active() {
		return false
	}
	rows := r.out.BandRows()
	if rows <= 0 {
		return false
	}
	top := r.out.LiveRow() - rows + 1
	return row >= top && row <= r.out.LiveRow()
}

// refreshLiveRow re-reads the row the band's live line lives on and reports
// whether it MOVED since the last write. True also means every row-relative
// piece of state this renderer kept is now stale (the wrap width and the column
// the in-flight text stopped at), so its callers re-derive those too.
//
// WHY THIS EXISTS (measured 2026-09-26). The renderer read LiveRow() once per
// disk/predecessor call and then CACHED it in liveRow, while the layout it draws
// through moves underneath it: the editing line grows (the submitted message is
// laid out in full, so it may occupy more rows), the band CONTRACTS by the same
// rows, and the row the answer was about to be written on becomes the editing
// line's FIRST row. A stale liveRow is then one row below the band's own bottom,
// and the next write does exactly what its arithmetic says — it places the
// cursor there and paints the answer's head onto the EDITING LINE's row:
//
//	ESC[10;7H  ESC[1mbold conclusion ESC[0m     ← the layout had already moved
//	ESC[10;1H  ESC[2K                            ← the editor's next repaint erases it
//
// The frozen artifact then holds `▌     bold conclusion follows` where the band
// row is `the **bold conclusion** follows`, i.e. a row whose middle is BLANKED,
// and the dump gate times out at 5s waiting for a needle that row can never
// carry. The commit that follows reads LiveRow() fresh, so the answer's TAIL
// lands on the right row while its HEAD does not — which is why the artifact
// looks like a torn frame rather than a missing one.
func (r *AnswerRenderer) refreshLiveRow() bool {
	row := r.LiveRow()
	if row == r.liveRow {
		return false
	}
	r.liveRow = row
	return true
}

// Begin starts one answer: the live row is emptied and the cadence clock reset.
// It is called once per turn, before the first delta, and — like Delta and
// Finish — it does NOT draw from its caller's goroutine: the status is PUBLISHED
// (raised and woken), and the editor's read loop draws it. Begin runs on the
// turn's own goroutine while the read loop is drawing the same row, so a
// drawStatus here was a second writer on the same state (measured under -race:
// Begin -> drawStatus raced Tick -> drawStatus on drawnStatus and on the status
// row). Raising the flag is the same road a publish takes anywhere else, and the
// flag is sticky, so the draw is LATE by one wake, never lost.
func (r *AnswerRenderer) Begin() {
	if r.routed(answerOp{kind: answerOpBegin}) {
		return
	}
	r.beginNow()
}

// beginNow is Begin's own body, and it is a method rather than an inline block
// because it has TWO callers with different rights: the read loop, which owns
// the renderer while it runs (this method applied from the queue), and the
// caller itself, when no loop is running (a unit test, the pty cases, a renderer
// no editor built — see routed).
func (r *AnswerRenderer) beginNow() {
	r.pending.Reset()
	r.prefix = ""
	r.partial = ""
	r.liveText = ""
	r.compPrefix = ""
	r.tableRows = nil
	r.inFlight = false
	// The answer body's composition state is reset through the seam that owns
	// it: the composer is a per-stream classifier, so a stale fence would carry
	// the previous answer's code emphasis into this one.
	r.resetComposer()
	// The turn's marker/reasoning ledger is reset here too, in the SAME place, so
	// a turn's gutter state and its body composition can never disagree about
	// which answer is being started (a stale marker would prefix THIS answer's
	// first line with the previous turn's claim).
	r.marked = false
	r.ledged = false
	r.thinking = false
	r.thinkingWrote = false
	r.clearLiveRow()
	r.lastFlush = r.now()
	r.PublishStatus()
}

// Delta feeds one streamed fragment. Completed lines leave the live region
// immediately (and exactly once); the rest stays in flight.
func (r *AnswerRenderer) Delta(s string) {
	if r.routed(answerOp{kind: answerOpDelta, text: s}) {
		return
	}
	r.deltaNow(s)
}

// deltaNow is Delta's own body: the rune carry, the cadence buffer and the
// flush. It runs on the goroutine that owns the renderer (see beginNow).
func (r *AnswerRenderer) deltaNow(s string) {
	if s == "" {
		return
	}
	// A carriage return inside a delta is text, not a cursor motion: the
	// renderer owns the live row's cursor. Normalizing here keeps a stray CR
	// from moving the cursor to column 0 mid-word (Screen would render the
	// overwrite faithfully — that is, correctly showing the damage).
	s = strings.ReplaceAll(s, escCarriage, "")
	if r.partial != "" {
		// The head of this delta completes the rune the previous one split.
		s = r.partial + s
		r.partial = ""
	}
	r.pending.WriteString(s)
	r.drain(false)
}

// Finish ends the answer: everything still in flight is written and committed,
// so the tail is never dropped and nothing is left live.
func (r *AnswerRenderer) Finish() {
	if r.routed(answerOp{kind: answerOpFinish}) {
		return
	}
	r.finishNow()
}

// finishNow is Finish's own body: the tail carry, the commit, the table release
// and the live row's release (see beginNow for who runs it).
func (r *AnswerRenderer) finishNow() {
	if r.partial != "" {
		// An unterminated rune at the very end of the stream is emitted as-is:
		// the model sent it, so dropping it would decide the answer's text.
		r.pending.WriteString(r.partial)
		r.partial = ""
	}
	// A no-break delta PAINTS its text and leaves the buffer empty (see drain),
	// so what remains in flight is what the live row already DISPLAYS (liveText)
	// followed by anything still queued. The displayed head goes through the
	// same carry as a commit, so it is never written twice.
	if tail := r.pending.String(); tail != "" || r.liveText != "" {
		// The tail is appended as PLAIN TEXT and committed through the same path
		// as every other finished line (commitPrefix), which is where the
		// composer is asked about it. Deciding it here instead would ask the
		// composer twice about one line — and a fence asked twice closes itself.
		// What the live row already DISPLAYS (liveText) is carried forward:
		// master appends it to the text being committed as well as the tail, so
		// the committed row is the WHOLE line repainted at column 0 — the
		// branch's own reason for clearing liveText is that its characters are
		// not rewritten, but a commit that started at the tail would drop the
		// head the user can already see, so the head belongs in the committed
		// text (decideLine then decides the emphasis on the whole line at once).
		// The carry must be tested against EVERYTHING already queued for this
		// commit, not just the finished-line buffer: the parked fragment
		// (compPrefix) is appended right below, and the stream that reaches here
		// with a break in its last delta leaves liveText as the whole line —
		// already in the park, because that is where the unfinished line went.
		// Appending it to both is the measured
		// `the bold conclusion followsthe bold conclusion follows`.
		//
		// AND THE TAIL IS PART OF THAT "EVERYTHING" — the half that was missing
		// (measured 2026-09-27, task 01a0e433, on a NARROW fixture that never
		// wraps). The in-flight line reaches Finish in TWO registrations that
		// need not be disjoint: the park holds the line as the burst that was
		// WITHHELD left it, and the live row's own text is what the paint drew,
		// so the same line can be queued once by each:
		//
		//	park = "07-00 … 09-02 "        (the withheld burst's own text)
		//	tail = "09-03 09-04 09-05 尾"  (the painted continuation)
		//	live = park                    (the painted row IS the parked text)
		//
		// The suffix test saw only the PARK, which does carry the live text, and
		// so concluded the row was already covered — while the TAIL, appended
		// below and in no other registration, was queued in front of the park
		// instead of behind it. The committed line came out as `tail + park`:
		// the line's own end glued to the whole sentence it belongs to, i.e. the
		// sentence twice, the second copy cut by the row's width — the
		// operator's partial repeat, and the reason it is not a wrap carry: it
		// needs no wrap, only a burst boundary inside the LAST line of a turn.
		// A break in the last delta parks nothing and appends nothing, which is
		// why the same fixture with its final terminator is clean.
		queued := r.prefix + r.compPrefix + tail
		if !carryAlreadyQueued(queued, r.compPrefix, r.liveText) {
			r.prefix += r.liveText
			// Queued as part of the line: commitPrefix must not carry it a second
			// time.
			r.liveText = ""
		}
		// When the text is ALREADY queued it is left registered as the live
		// row's text, so commitPrefix still knows WHAT THE ROW SHOWS: the
		// provisional paint is measured there to clear the cells a wider literal
		// spelling left behind (commitLines). Clearing it here would lose that
		// measurement and leave the residue on a committed row.
		//
		// The two halves go in the order the LINE has them: the park is the
		// line's earlier half (the withholding burst's text) and the tail is its
		// continuation, so the park is appended FIRST. Appending the tail first
		// was the inversion above — the committed row read end-then-beginning.
		if r.compPrefix != "" {
			r.prefix += r.compPrefix
			r.compPrefix = ""
		}
		r.prefix += tail
		r.pending.Reset()
	}
	// A pipe row whose successor never arrived is still PARKED (a run of pipe
	// rows is not painted until a line ends it, so a table can be decided from
	// the whole block; see commitLines/table.go). With no successor coming, the
	// hold is over: the run is painted now, and leaving it parked would DROP the
	// answer's last rows. (The in-flight branch above releases the park itself
	// when it runs; this is the other road into Finish.)
	if r.compPrefix != "" {
		r.prefix += r.compPrefix
		r.compPrefix = ""
	}
	r.commitPrefix()
	r.paintTable()
	r.releaseLive()
	// The status row is NOT drawn here: Finish runs on the turn's own goroutine
	// and the read loop owns every write (see Begin — the same measured race).
	r.PublishStatus()
}

// Tick draws the status line. It is called by the EDITOR'S READ LOOP — the one
// place that owns terminal writes — never by a caller from its own goroutine:
// that interleaves its cursor placement with the read loop's repaint (the
// two-writer defect this package's publish path exists to remove). Callers
// publish instead (PublishStatus), which raises a flag and wakes the loop; the
// loop then calls this.
func (r *AnswerRenderer) Tick() {
	r.statusPublishes.Add(1)
	r.statusDirty.Store(false)
	r.drawStatus()
}

// noteRow is one CALLER row waiting to be committed into the band: the text as
// it will be SHOWN (no SGR — the palette is applied at the moment of the write,
// so an escape byte can never be measured as a cell) and the style it is drawn
// under.
type noteRow struct {
	text string
	st   Style
	// copied marks a row that must be COPIED OUT OF THE SCREEN — a line of a
	// block the operator selects and pastes. It suppresses the continuation
	// marker a wrapped row would otherwise open with (`↳ `), for the same reason
	// answerRow.copied suppresses the turn's gutter: a marker inside the cells a
	// selection carries is a paste that does not run. The row keeps the band's
	// own left edge; a wrap is still a wrap (the layout's business), it simply
	// does not put a glyph into the text.
	copied bool
	// runs is a caller-composed row's PER-RUN emphasis (NoteRuns): the row's
	// text is the runs' texts concatenated, and each run is drawn under its own
	// style instead of the row's single st. It exists so a caller whose row
	// MEANS several things at once — cmd/agent's tool-activity row, where the
	// glyph, the tool name and the command are three roles — can say so in
	// DATA rather than composing SGR into the string it hands the band (which
	// would put escape bytes into the width arithmetic and into the cells a
	// copy carries). Empty for every ordinary note, which is therefore drawn
	// exactly as before.
	runs []NoteRun
}

// NoteRun is one styled run of a caller-composed note row (see NoteRuns).
//
// The style is this package's Style (the drawing vocabulary the band already
// uses), NOT a raw sequence: a caller converts its cell-grid style once at the
// seam (StyleFromSGR), so the band never parses SGR and a monochrome run costs
// no byte.
type NoteRun struct {
	Text  string
	Style Style
}

// NoteRunText is the visible text of a run list: what the row SHOWS. It is the
// ONE definition of "the row's characters" for a caller-composed row, so a
// caller that measures the row (cmd/agent's width test) measures the same text
// the band draws.
func NoteRunText(runs []NoteRun) string {
	var b strings.Builder
	for _, r := range runs {
		b.WriteString(r.Text)
	}
	return b.String()
}

// Note queues one row of CALLER-OWNED text — an activity row, a notice, a
// failure line — to be committed into the answer band as a durable row, and
// wakes the read loop that commits it.
//
// WHY IT IS NOT A WRITE. The band has exactly one writer (the editor's read
// loop), and a caller that wrote the row itself would interleave its cursor
// placement with the read loop's repaint — the measured two-writer defect this
// package's whole publish path exists to remove. So a note takes the road a
// status publish takes: it is QUEUED here, the flag is raised, the wake pipe
// interrupts the loop's blocked poll, and drainNotes commits the rows.
//
// WHY A ROW AND NOT A LOG. The defect it closes: the chat REPL reported a tool
// call on the STATUS row and CLEARED it when the turn ended, so the transcript
// kept no record that a tool had ever run — a reader could not tell which
// commands produced the answer, and a scrolling transcript lost the activity
// entirely. The reference TUI prints one durable row per call ("┊ 💻 $ …") and
// keeps it in the transcript; this is the band's equivalent.
//
// It is a NO-OP with no reserved layout installed: the cooked path's byte
// stream is its contract, and it is produced by the caller's own writer there
// (cmd/agent writes the historic flow bytes when BandActive() is false), so a
// note must never add a byte to it.
func (r *AnswerRenderer) Note(text string, st Style) {
	if r == nil || r.out == nil || !r.out.Active() {
		return
	}
	if r.routed(answerOp{kind: answerOpNote, text: text, st: st}) {
		return
	}
	r.noteNow(text, st)
}

// NoteRuns queues ONE caller row composed of styled RUNS (see NoteRun): the
// same road Note takes (queued, drained by the read loop), differing only in
// where the emphasis comes from. It is the seam a caller uses when the row's
// parts are different roles rather than one emphasised line.
func (r *AnswerRenderer) NoteRuns(runs []NoteRun) {
	r.noteRunsCopied(runs, false)
}

// noteRunsCopied is the ONE body both entry points share, so the queued path
// carries the "this row is a paste source" fact (answerOp.copied) rather than
// losing it at the seam.

// NoteRunsCopied is NoteRuns for a row a reader SELECTS AND PASTES: the same
// cells and the same emphasis, with the continuation marker off (see
// noteRow.copied). It is a separate entry point rather than a flag on the run
// list because "this row is a paste source" is a property of the ROW's purpose,
// decided by the caller that composed it, and a run is one styled span.
func (r *AnswerRenderer) NoteRunsCopied(runs []NoteRun) {
	r.noteRunsCopied(runs, true)
}

func (r *AnswerRenderer) noteRunsCopied(runs []NoteRun, copied bool) {
	if r == nil || r.out == nil || !r.out.Active() || len(runs) == 0 {
		return
	}
	if r.routed(answerOp{kind: answerOpNote, runs: runs, copied: copied}) {
		return
	}
	r.noteNowRunsCopied(runs, copied)
}

// noteNowRuns is NoteRuns' own body; the run list is SPLIT into rows on its own
// newlines exactly as Note splits its text, so a multi-row run list arrives at
// the band as the rows the caller described.
func (r *AnswerRenderer) noteNowRuns(runs []NoteRun) { r.noteNowRunsCopied(runs, false) }

func (r *AnswerRenderer) noteNowRunsCopied(runs []NoteRun, copied bool) {
	rows := make([]noteRow, 0, len(runs))
	for _, line := range splitNoteRuns(runs) {
		rows = append(rows, noteRow{text: NoteRunText(line), runs: line, copied: copied})
	}
	if len(rows) > 0 {
		r.notesMu.Lock()
		r.notes = append(r.notes, rows...)
		r.notesMu.Unlock()
	}
	r.PublishStatus()
}

// splitNoteRuns splits a run list at its newlines: each returned list is one
// row's runs, with the break characters removed (they are row boundaries, not
// content — the band commits one row per call).
func splitNoteRuns(runs []NoteRun) [][]NoteRun {
	var rows [][]NoteRun
	cur := []NoteRun{}
	flush := func() { rows = append(rows, cur); cur = []NoteRun{} }
	for _, run := range runs {
		text := run.Text
		for {
			i := strings.IndexByte(text, '\n')
			if i < 0 {
				break
			}
			if i > 0 {
				cur = append(cur, NoteRun{Text: text[:i], Style: run.Style})
			}
			flush()
			text = text[i+1:]
		}
		if text != "" {
			cur = append(cur, NoteRun{Text: text, Style: run.Style})
		}
	}
	if len(cur) > 0 || len(rows) == 0 {
		flush()
	}
	return rows
}

// noteNow is Note's own body: the rows are SPLIT here and appended to the queue
// the read loop drains through drainNotes — the caller-row queue, which is
// renderer state and therefore the owner's to append to (see beginNow).
func (r *AnswerRenderer) noteNow(text string, st Style) {
	rows := make([]noteRow, 0, 1)
	for _, line := range strings.Split(strings.TrimRight(text, escNewline), "\n") {
		rows = append(rows, noteRow{text: line, st: st})
	}
	if len(rows) > 0 {
		r.notesMu.Lock()
		r.notes = append(r.notes, rows...)
		r.notesMu.Unlock()
	}
	// PublishStatus's own wake: the queue is drained by the read loop, so the
	// note reaches the screen on the next wake — immediately, because the wake
	// pipe interrupts the loop's blocked poll (a note visible only after the
	// next keystroke is the defect the wake exists to prevent).
	r.PublishStatus()
}

// notesAllCopied reports whether EVERY row of a drained queue is a paste source.
// Such a queue is a command's own output (the /info block, a /voice notice), not
// a turn's remark, so it is committed without the turn's request row above it.
// It takes the SNAPSHOT drainNotes took, never the live queue (see notesMu).
func notesAllCopied(notes []noteRow) bool {
	if len(notes) == 0 {
		return false
	}
	for _, n := range notes {
		if !n.copied {
			return false
		}
	}
	return true
}

// takeNotes removes and returns every queued caller row, in order. It is the
// drain's snapshot: a producer that appends WHILE the read loop commits keeps
// its rows for the next drain rather than losing them to a clear afterwards.
func (r *AnswerRenderer) takeNotes() []noteRow {
	r.notesMu.Lock()
	defer r.notesMu.Unlock()
	if len(r.notes) == 0 {
		return nil
	}
	notes := r.notes
	r.notes = nil
	return notes
}

// Noting reports whether there is a queued caller row waiting to be committed.
// It is the read loop's drain condition (see drainNotes); the flag it is paired
// with is the status publish, which is sticky, so a queued row is drawn LATE at
// worst — never lost.
func (r *AnswerRenderer) Noting() bool {
	if r == nil {
		return false
	}
	r.notesMu.Lock()
	defer r.notesMu.Unlock()
	return len(r.notes) > 0
}

// drainNotes commits every queued caller row into the band, in order, and
// reports whether it committed any.
//
// THE ORDER INSIDE IS THE CONTRACT. Whatever the ANSWER still has in flight is
// committed FIRST: the live row IS the line currently being written, and
// committing the note on top of it would either overwrite that text or land the
// note mid-line. So the note promotes the in-flight row out of the way, commits
// its own rows, and leaves a fresh live row for the answer's next delta — which
// then continues BELOW the note, in the order the user's terminal shows.
//
// The rows are committed through the SAME writeRow/finalizeLiveRow path every
// answer row takes (one write + one scroll per row, never a repaint), so the
// band has one notion of a row and a note cannot disagree with an answer line
// about geometry. The style is the caller's (cmd/agent passes the palette's
// tool-row style, exactly as the status row uses it), so a note carries the
// same emphasis on both of the surfaces that describe one tool call.
func (r *AnswerRenderer) drainNotes() bool {
	if r == nil || r.out == nil || !r.out.Active() {
		return false
	}
	// TAKE the queue under notesMu — the producer may be a foreign goroutine
	// (see the field's comment). The take is a SNAPSHOT: rows appended while this
	// drain runs stay in the queue for the next one instead of being dropped by
	// a clear at the end, and the publish that queued them has already woken the
	// read loop (PublishStatus), so the next drain is one wake away.
	notes := r.takeNotes()
	if len(notes) == 0 {
		return false
	}
	// The in-flight answer text goes first: it is the row the note must not land
	// on. Finish's own carry (liveText + the queued tail) is what makes the
	// commit lossless — the characters the user can already see are part of the
	// committed row rather than rewritten by it.
	if tail := r.pending.String(); tail != "" || r.liveText != "" {
		// THE CELLS THE IN-FLIGHT PAINT LEFT ARE CLEARED HERE, and it is the same
		// defect the layout-shift fix closed, arriving through THIS door: the head
		// is folded into the prefix and its registration is dropped, so
		// commitPrefix sees head == "" and has nothing to erase — while the row
		// still shows every cell that paint drew. The commit then writes the whole
		// line from column 0 ON TOP of them, and the row reads
		// `<line's beginning><the same line again>` — the operator's partial
		// repeat, measured frame by frame on a note injected mid-line (2026-11-10:
		// before the drain, row 19 held `它当时是我在气头上拿「self-re`; after it,
		// row 17 held that fragment TWICE).
		//
		// The clear goes through the ONE predicate the other two doors use
		// (clearStaleHeadRow -> rowInBand), so a layout move in between cannot
		// make these three disagree.
		if r.liveText != "" {
			r.clearStaleHeadRow(r.liveRow)
			// THE PAINT'S OWN PARK IS THE SAME TEXT, registered twice: computeRows
			// parks an unterminated segment in compPrefix AND paints it, and this
			// branch is folding that paint's `liveText` into the prefix. Leaving the
			// park behind makes commitLines prepend it a second time — measured
			// (2026-11-10) on a note injected mid-line at cols=101: the row read
			// `它当时是我在气头上拿「self-re它当时是我在气头上拿「self-re`, i.e. the
			// line's first 29 cells twice, which is the operator's partial repeat.
			// The invariant commitLines' unconditional join relies on is exactly this
			// one: a non-empty park is ALWAYS text the line does not carry yet.
			r.compPrefix = ""
		}
		r.prefix += r.liveText + tail
		r.liveText = ""
		r.liveCol = 0
		r.pending.Reset()
	}
	// A QUEUE OF PASTE SOURCES SKIPS THE PREFIX CARRY TOO: commitPrefix promotes
	// an in-flight row into the band, and for a command's own output there is
	// none — measured 2026-10-22 on a real PTY grid, `/info` opened at row 11
	// through WriteLines and at row 12 through the note path with every following
	// row shifted by one. The block's text was identical; the extra row was not.
	pasteSource := notesAllCopied(notes)
	if !pasteSource {
		r.commitPrefix()
	}
	// The turn's FIRST committed row is its REQUEST: the operator's own line,
	// drawn as the head of the block it belongs to. It is emitted HERE — before
	// the note rows below, and with the note path's gutter suppression not yet
	// on — because in the ordinary tool turn the turn's first band row IS the
	// note: this is the seam that must not let a call row open a block whose
	// request header arrives afterwards.
	// A QUEUE OF PASTE SOURCES SKIPS THE REQUEST ROW, and that is a layout fix
	// rather than a preference: `/info`'s block and a `/voice` notice reach the
	// band from a command the operator typed at the PROMPT, not from a turn, and
	// on the panel path (WriteLines) they were committed with no request row
	// above them. Drawing one here added a row that master never printed —
	// measured 2026-10-22 on a real PTY grid: base opened the block at row 11,
	// the note path at row 12, i.e. one extra blank row in front of it (Rule 31;
	// the unit tests could not see it because they assert the block's ROWS, not
	// the grid's row numbers).
	if !notesAllCopied(notes) {
		r.drawRequestRow()
	}
	// The row the note is written on is the band's LIVE row, asked for HERE
	// rather than inherited from liveRow: a note may be the FIRST thing that
	// reaches the band (a tool call before the turn's first delta, so before any
	// Begin → clearLiveRow), and an unset liveRow is the SCREEN's first row —
	// where the commit scroll would push the row's own text off the band's top
	// margin and lose it (measured: the row vanished and the grid stayed empty).
	// The band's live row is a property of the layout, so it is read from the
	// layout at the moment of the write.
	r.liveRow = r.out.LiveRow()
	r.liveCol = 0
	// The note's own rows are written with NO marker column (see lineGutter):
	// each carries its own glyph at column 0.
	r.noting = true
	for _, n := range notes {
		r.writeNote(n)
	}
	r.noting = false
	r.noteContinuing = false
	r.inFlight = false
	return true
}

// PublishStatus announces that the status source's values changed. It performs
// NO terminal I/O: it raises the pending flag and wakes the read loop, which is
// the only goroutine that draws. Calling it from another goroutine is therefore
// safe and cannot interleave a cursor placement with a repaint.
//
// The flag is sticky until the loop draws, so a publish can be SEEN LATE (the
// next wake) but never LOST — and the wake exists to make "late" the next
// millisecond rather than the next keystroke.
func (r *AnswerRenderer) PublishStatus() {
	if r == nil {
		return
	}
	r.statusDirty.Store(true)
	if r.notify != nil {
		r.notify.Notify()
	}
}

// StatusPublishPending reports whether a publish is waiting to be drawn. It is
// the read loop's own query (the flag is consumed by Tick, i.e. by the draw
// that honours it).
func (r *AnswerRenderer) StatusPublishPending() bool {
	return r != nil && r.statusDirty.Load()
}

// StatusPublishes returns how many status publishes were drawn (each a repaint
// request). A publish whose values did not change costs zero writes — the
// bounded-repaint assertion in the tests is stated against this pair.
func (r *AnswerRenderer) StatusPublishes() int { return int(r.statusPublishes.Load()) }

// Drain writes the in-flight text that the cadence window has made ready.
// force skips the cadence check (used when a line break ends the word).
func (r *AnswerRenderer) drain(force bool) {
	if r.pending.Len() == 0 {
		return
	}
	text := r.pending.String()
	// Hold back a split multi-byte rune: writing half a rune now would render
	// as a replacement character. On force (a line break or Finish) the caller
	// has already supplied the rest, so the hold-back is skipped.
	if hold := RuneSafeCut(text); hold > 0 && !force {
		if hold > answerMaxPrefixBytes+utf8.UTFMax {
			hold = 0 // a long unreal rune tail is text, not a split rune
		} else {
			r.partial = text[len(text)-hold:]
			text = text[:len(text)-hold]
			r.pending.Reset()
			r.pending.WriteString(text)
		}
	}
	cut := lineBreakCut(text)
	if cut < 0 && !force && !r.ready(text) {
		return
	}
	// A FLUSH MAY NOT HAND A WRAPPING LINE TO writeLive IN PIECES: a whole
	// line's break is the only instant its emphasis (and its markers) can be
	// decided, so the text that reaches the live row must be either text whose
	// line was COMPLETE when it was handed over, or the line's WHOLE
	// (unbroken) text — never a prefix of it that happens to fill a band row.
	// That is what leaves a committed row provisional forever (see releaseLive
	// and unfinishedLineWouldWrap).
	if cut < 0 && r.unfinishedLineWouldWrap(text) {
		r.lastFlush = r.now()
		return
	}
	// Split the buffer at the LAST line break. Everything up to and including
	// that break is FINALIZED (it goes to the band exactly once, through
	// prefix); whatever follows is still in flight and is written to the live
	// row. The two halves must not overlap: writing the finalized text live AND
	// queueing it for commit paints every line twice.
	last := strings.LastIndexByte(text, '\n')
	if last < 0 {
		// No finished line: the whole buffer stays on the live row. Its EMPHASIS
		// is provisional (the style of the block the stream is inside) — a
		// streaming answer may never wait for a decision — and the decision is
		// taken when this line's break arrives, over the line's whole text.
		//
		// EXCEPT a pipe row, whose paint is withheld until the line after it
		// says what it is (table.go). That hold has to be taken HERE as well as
		// in computeRows: a buffer carrying no break never reaches computeRows,
		// and it was this path that painted the `| ` fragment of a table row
		// live (measured), leaving the rest of the row to be committed as its
		// head — the headless row itself.
		if looksPipedRow(text) {
			r.pending.Reset()
			r.compPrefix += text
			r.lastFlush = r.now()
			return
		}
		r.pending.Reset()
		r.writeLive(text)
		r.lastFlush = r.now()
		return
	}
	body := text[:last]
	tail := text[last+1:]
	// THE BREAK MAY BE THE FRAME'S WHOLE CONTENT — and then the line it
	// TERMINATES is the text the live row already SHOWS. The split above leaves
	// an empty body (`text[:last]` with `last == 0`), commitPrefix finds no
	// queued text and promotes nothing, and the finished line stays LIVE — so
	// the next line is painted onto the same row (writeLive appends at liveCol)
	// and several logical answer lines land in ONE band row. Which of the two
	// shapes a reader sees is decided by nothing but the CARRIER's chunk size:
	// measured on a real pty, the same three-line answer came out as one row at
	// one byte per frame and as three rows at four (this package's
	// answer_linebreak_cadence_test.go is the smallest sample that separates
	// them).
	//
	// The row the break terminates is therefore ROUTED like any other finished
	// line — through routeBody and commitPrefix, the ordinary path: the composer
	// is asked about the whole line, the head is reconciled with what the row
	// already shows (headIsInRow), and nothing is written twice. It is
	// deliberately NOT promoted with finalizeLiveRow, which would commit the
	// row's PROVISIONAL paint verbatim, markers and all, onto a band row nothing
	// ever repaints: that road was measured painting `the **bold conclusion**
	// follows` LITERALLY (emphasis_chunked_test.go, chunk = 1) when the fix was
	// first attempted that way.
	//
	// liveText is what the line to commit IS, not a separate copy of it: carried
	// as the commit's head it makes headIsInRow prefix nothing, because the row's
	// decided text already opens with it. Routing it here is also what takes the
	// row out of the live state, which is what stops the next line from being
	// appended to it.
	//
	// ONE EXCEPTION, and it is a hold rather than a decision: a parked pipe row
	// (compPrefix) is the unfinished FIRST HALF of a line whose fate the row
	// after it decides, so what the live row shows is not a finished line at all
	// and promoting it would paint a row the table policy is still deciding
	// (tableHolding — the same reason the no-break path withholds it). Note that
	// compPrefix is otherwise NOT a test of "is a line in flight": it doubles as
	// the record of the paint's own unterminated segment (computeRows), so it is
	// non-empty for the ordinary painted tail this branch exists for.
	if body == "" && r.liveText != "" && !r.tableHolding() {
		body = r.liveText
		// THE PAINT'S OWN PARK GOES WITH IT, and this is the line that makes the
		// join in commitLines decidable by CONSTRUCTION rather than by content.
		// computeRows parks an unterminated segment in compPrefix AND paints it,
		// so the ordinary painted tail is registered TWICE: once as the park,
		// once as liveText. Promoting liveText into body above queues the paint,
		// so the park is now a second registration of text this commit already
		// carries — and leaving it there made the join prepend it again, which is
		// measured duplication: a 60-cell `=` rule line at cols=60 reached the
		// band as 120 cells. Clearing it here restores the invariant the join
		// relies on (a non-empty park is always text the line does not carry
		// yet), and no information is lost: the park's characters ARE liveText's
		// characters (this branch exists precisely because the two agree).
		r.compPrefix = ""
	}
	// The buffer is emptied HERE, not merely rewritten with the tail: the tail
	// below is PAINTED on the live row, so leaving it queued means the next
	// drain appends to it and commits it a second time — the measured
	// duplicated-head defect (`…the read too` immediately followed by
	// `The path is devdo` on one row), whose trigger is an ordinary delta that
	// carries a line break AND the next line's beginning. What the live row
	// shows is tracked by liveText, which is what a later commit carries.
	r.pending.Reset()
	// The body's COMPLETE lines are ROUTED, not appended raw: a markdown pipe row
	// among them belongs in the park, where the run it is part of is decided as a
	// block (routeBody/table.go). Appending it raw is what split a table between
	// two owners — rows painted as prose while the run sat parked.
	r.routeBody(body)
	r.commitPrefix()
	// The completed lines are released into prefix, but the tail is painted on
	// the live row — unless the tail belongs to a markdown table, whose rows are
	// held until the block can be decided (computeRows/tableHold, table.go).
	// A piped tail is the FIRST HALF of the next line, not a row of its own: it
	// is parked as the line's head so the continuation can be joined to it.
	// Painting it live left the continuation to be read as a whole line without
	// its head (measured: `| Non-interactive faces | ...` was parked as
	// `Non-interactive faces | ...` and reached the screen stripped).
	switch {
	case tail == "":
	case looksPipedRow(tail):
		r.compPrefix += tail
	case !r.tableHolding():
		r.writeLive(tail)
	}
	r.lastFlush = r.now()
}

// lineBreakCut reports the index of a line break that is safe to act on, or -1.
// It exists for the one case where the naive "the last byte is a break" test is
// wrong: a CR at the very end of a delta may be the first half of a CRLF whose
// LF is in the next delta — and a line must be committed once, not twice.
func lineBreakCut(text string) int {
	if strings.HasSuffix(text, escCarriage) {
		return -1
	}
	idx := strings.LastIndexByte(text, '\n')
	if idx >= 0 && idx == len(text)-1 {
		return idx
	}
	if idx >= 0 {
		return idx
	}
	return -1
}

// ready reports whether the flush cadence has elapsed (or the buffer is over
// its bound, which must always make progress).
//
// The cadence is a WINDOW, not a word boundary: once a window's worth of text
// has been in flight it is painted, whatever its last byte is. Gating it on the
// buffer ending in a SPACE (the shape this carried before) made the in-flight
// unit a whole WORD — so a delta stream that carries no space, or carries its
// spaces late (a word split across token frames), showed the reader NOTHING
// until a line break or Finish: measured with a pty-driven run of the real
// binary against a word-per-delta carrier, where the answer appeared at its
// line breaks only.
//
// Reverting to a word-boundary gate re-introduces that defect: the live row
// stays empty for the whole turn.
func (r *AnswerRenderer) ready(text string) bool {
	if len(text) >= answerMaxBufferedBytes {
		return true
	}
	return r.now().Sub(r.lastFlush) >= answerFlushWindow
}

// commitPrefix finalizes the finished lines exactly once: each line's text is
// written onto the live row (appending at liveCol, which is 0 for a fresh line)
// and then committed with ONE region scroll, which promotes the whole live row
// into the band and leaves a fresh empty live row behind. Nothing written here
// is ever written again — that is the A5 invariant, and `committed` is the
// counter that proves it.
//
// A line wider than the terminal needs no wrapping loop: writeLive finalizes a
// row the moment the line fills it (one scroll per row), so the wrap is handled
// by the same rule as a line break — one write + one scroll per row, never a
// repaint.
func (r *AnswerRenderer) commitPrefix() {
	if r.prefix == "" {
		return
	}
	// Whatever the live row already DISPLAYS is the head of this line and must
	// not be written again: the band's commit works by scrolling the whole row
	// into place, so the head the user can already see is carried along by the
	// scroll. Writing it a second time lands it at the cursor's current column
	// instead of at column 0 — the duplicated-head defect this carry exists to
	// prevent (measured: `…the read too` immediately followed by `The path is
	// devdo` on one row, when a mid-line burst boundary coincided with a
	// commit). The head is therefore SPLIT OFF the text to commit, and the
	// committed row is the head plus as much of the remainder as fits it.
	//
	// The head is the text the live row already DISPLAYS — raw, because that is
	// what its cells hold: it is decided into spans by headSpans at the one seam
	// that draws it (fitCommitRow), so a partial paint's literal markers are not
	// carried onto a row the band never repaints.
	head := r.liveText
	// rawHead is what the partial paint actually DREW. Its markers are literal
	// cells on the live row, so the erase that clears a wider provisional paint
	// measures THIS against the decided head's width.
	rawHead := r.liveText
	// rawCol is the COLUMN that provisional paint ended at — the number the erase
	// is really about. A provisional paint and the decided row it is superseded by
	// do not have to START at the same column: an in-flight line inside a fence is
	// painted with the code layer's own row shape from its first cell
	// (answerRow.gutterGlyph), while a line that arrived before the fence opened
	// was drawn two cells in. Comparing the two TEXTS against each other misses
	// that offset, and the cells the provisional paint reached past the decided
	// row stayed on the grid — measured 2026-10-02 on a live pty: the opening
	// marker row came out as ```` ```bashsh ````, the leftover `sh` of the
	// provisional `▌ ```bash` sticking out past the decided ```` ```bash ````.
	rawCol := r.liveCol
	r.liveText = ""
	// THE LAYOUT MAY HAVE MOVED SINCE THE HEAD WAS PAINTED, and then the head's
	// cells are on the row it was painted on, not on the row being committed.
	// That stale copy has to be CLEARED, or the band scrolls it into the
	// committed reading as a row of its own and the reader sees the line's
	// beginning twice — the operator's partial repeat (see reanchorLiveRow for
	// the measurement and the two guards whose fixtures cannot reach it; this is
	// the same defect arriving through the buffered door, where nothing calls
	// writeLive between the layout move and the commit).
	previous := r.liveRow
	// The layout may have moved since the head was painted; the commit's own
	// scroll goes to the CURRENT live row, so the head is re-anchored with it
	// instead of being carried to a row the band no longer has.
	r.refreshLiveRow()
	r.liveRow = r.out.LiveRow()
	if previous != r.liveRow && head != "" {
		r.clearStaleHeadRow(previous)
	}
	r.liveCol = 0
	lines := strings.Split(r.prefix, "\n")
	r.prefix = ""
	// THESE LINES ARE COMPLETE BY CONSTRUCTION — the split removed their
	// terminators — so this is where the composer is asked about them: the
	// emphasis of a line is decided once, on the whole line, before any of it is
	// written. A line's text already displayed (the head, painted by an earlier
	// burst) is part of the same decision but is NOT rewritten to change its
	// emphasis: what the user can see is never repainted.
	//
	// An EXTRA line is carried in front of it and painted out of sight — not
	// written to the band, but SENT THROUGH writeRow, so the row's cells are
	// measured by the one component that owns the geometry. The head widens only
	// when its markers were dropped (a partial line paints them literally, the
	// decided one does not), and a row that starts WIDER than the row it was
	// painted on must be trimmed to what that row can hold before anything is
	// written. Painting head+line while the cell arithmetic is computed from line
	// alone is the shear this exists to prevent: a 5-cell `**bold**` head
	// measured against a 2-cell `bold` row wraps the wrong text onto the wrong
	// row, and the band is never repainted to repair it.
	r.commitLines(lines, head, rawHead, lines[0], rawCol)
	// The last commit scrolled a fresh (empty) live row into place; drop the
	// in-flight marker so the next delta treats the row as new.
	r.inFlight = false
}

// commitLines paints the complete lines this commit carries, in order: the
// first takes the head the live row already shows, the rest are fresh rows.
//
// It exists because one line's TEXT can be more than one band row: a markdown
// table is recognised by its delimiter row, and the form it is painted in — an
// aligned grid, or transposed field/value blocks when the grid cannot fit the
// band — is decided from the WHOLE block (see table.go). Every line it paints
// is still cut, styled and guttered by writeRow, which keeps the cell
// arithmetic in one owner (Rule 1b).
//
// THE BLOCK IS NOT DECIDED BY THE BURST THAT REVEALS IT. A delimiter row can
// arrive in a burst of its own — the ordinary 64-byte token stream does exactly
// that — and at that instant the block has a header and NO records, which is not
// enough to decide a form from and not enough to paint: measured, the header was
// painted as a pipe row, the delimiter row as a row of dashes, and the records
// that arrived next came back one stripped of its head (a paint that depends on
// where a burst falls is the operator's `◌◌` damage reproduced by the fix meant
// to remove it). So a run of pipe rows is parked in r.tableRows and painted by
// the line that ENDS it, or by Finish.
func (r *AnswerRenderer) commitLines(lines []string, head, rawHead, first string, rawCol int) {
	// The park belongs BEFORE these lines: a run of pipe rows was held back
	// while the rest of the answer arrived, and the line that ended the run is
	// the first line here (see routeBody, where the park is filled).
	r.paintTable()
	for i, line := range lines {
		h := ""
		if i == 0 {
			h = head
		}
		// A line the stream split in the middle of an emphasis pair has to be
		// decided as the WHOLE line: the tokenizer answers about a balanced pair,
		// and a fragment holds only one half of it, so deciding the fragment alone
		// keeps the markers literal forever — measured on a 16-byte delta that cut
		// `the **bold concl` | `usion** follows`, where the committed row came out
		// as `the **bold conclusion** follows` with no emphasis at all. The
		// fragment is this line's earlier half, not a separate line (the same rule
		// routeBody already applies to a parked pipe row), and the head is not
		// carried beside it: fitCommitRow sees the head's text already inside the
		// row (headIsInRow) and prefixes nothing.
		// The parked fragment is this line's EARLIER HALF — whatever it holds —
		// so the line is decided as the whole of it: the composer is a STATEFUL
		// classifier (a fence opener toggles the block for every line after it,
		// see LineComposer.StyleFor), and deciding a fragment alone asks it about
		// a line that does not exist — measured 2026-10-02: a streamed
		// ```` ```bash ```` reached the composer in three frames, the fragment
		// ```` as ```` only, the fence never opened, and the block's own rows were
		// classified as PROSE — so they opened with the response layer's gutter
		// glyph and the reader's copy of the block carried two characters of the
		// band's chrome (the paste leg this file exists for, defeated by nothing
		// but the carrier's chunk size).
		// THE JOIN IS UNCONDITIONAL, AND THAT IS THE FACT THE GUARD GOT WRONG.
		// The parked fragment is a SEPARATE registration of this line's earlier
		// half: `r.prefix` never contains it. The only path that folds it into
		// the queued text is routeBody's pipe-row arm, and that arm CLEARS it —
		// so by the time a line reaches here, a non-empty compPrefix is always
		// text this line does not carry yet. The guard that used to stand here
		// (`!strings.HasPrefix(line, r.compPrefix)`) asked the question by
		// CONTENT, and content cannot answer it: a line made of one repeated
		// character (`strings.Repeat("y", 200)`, an `=====` rule, a box-drawing
		// run) has the parked half as a verbatim prefix of its own remainder, so
		// the guard read "already joined" and dropped the half — measured at
		// cols=121 with a 64-byte carrier: 136 of 200 cells reached the band,
		// and at 100 bytes only 100. The same defect on a CJK line lost the
		// body's head entirely. A join may be decided by WHO registered the
		// text (construction), never by what the characters happen to be.
		// THE RESIDUAL CONTENT TEST, AND WHY IT IS GONE (measured 2026-11-08,
		// cols=100 with a 1/2/5-byte carrier). Replacing the prefix test above
		// with an IDENTITY test (`line != r.compPrefix`) closed the PREFIX shape
		// and left the EQUALITY shape open: when a carrier boundary falls exactly
		// on a band row, the parked half is one full row of text and the
		// remainder the stream sends next is its own length again — for a line of
		// ONE REPEATED CHARACTER at exactly 2*cols those two registrations are
		// byte-for-byte the same string, so the test compared two contents, read
		// "same registration", skipped the join and dropped the park. Measured:
		// 100 of 200 cells at cols=100 for bursts 1, 2 and 5 (and NOT at 13,
		// which is why the first fix's grid missed it) — the row that was painted
		// was then committed as the whole line. `line` and `r.compPrefix` are
		// DIFFERENT REGISTRATIONS even when their bytes agree; the only thing
		// that can say "this line already carries the park" is the park's own
		// presence, and a non-empty park is always text this line does not carry
		// yet (r.prefix never holds it, and the one arm that folds it in —
		// routeBody's pipe-row arm — clears it). So the join is decided by
		// CONSTRUCTION ALONE.
		if r.compPrefix != "" {
			line, r.compPrefix = r.compPrefix+line, ""
		}
		row := r.decideRow(line)
		row, ok := fitCommitRow(row, h, line, r.cols)
		if !ok {
			// The row cannot be drawn BESIDE the head the live row already shows
			// (the head alone fills this row): the head IS this row's whole
			// content, so it is promoted AS IT STANDS and the line goes on at the
			// fresh row below. `ok == false` used to reach the stale-cell erase
			// below with an EMPTY row — the erase wiped exactly the head's cells,
			// the only copy of them, and promoted the row carrying nothing.
			r.finalizeLiveRow()
			r.writeLive(line)
			continue
		}
		r.writeRow(row, row.text)
		if i == 0 {
			// A provisional paint can be WIDER than the row committed over it: a
			// partial line draws its markers literally, the decided row drops them
			// (h above is the decided spelling for exactly this reason), so the
			// cells past the committed row still hold the tail of a spelling
			// nobody decided. Clearing them at the tail of the row being committed
			// in the NEXT delta: measured `the bold conclusion follows` +
			// `low`. It costs nothing when there is nothing stale (the widths
			// agree and no sequence is written at all).
			// The comparison is on the COLUMN both paints reached (see rawCol):
			// the decided row's text plus the column IT opened at, against the
			// provisional paint's own end column. Comparing the two texts alone
			// held only while both started at the same cell.
			if rawCol > r.liveCol {
				r.placeAndWritePrefixed(escEraseToEnd, r.liveRow, r.liveCol, "")
			}
		}
		r.finalizeLiveRow()
	}
}

// routeBody appends a burst's COMPLETE lines to the commit buffer, holding
// markdown pipe rows back in the table park instead (see table.go). A pipe row's
// text is decided only by the row after it, so a run of them is kept WHOLE —
// never partly in the park and partly in the buffer, which is exactly how a
// table row came back with its front cut away.
func (r *AnswerRenderer) routeBody(body string) {
	// A burst boundary can cut a table row in half: the first half was parked
	// (compPrefix) by an earlier drain and the body CONTINUES it, so the parked
	// fragment is rejoined BEFORE the split — the lines of a pipe row are not
	// separate lines, and treating the continuation as one is the whole defect.
	// Measured: `| Non-interactive faces | \`4b9133c0\`…` arrived as
	// `Non-interactive faces | …`, its leading `| ` parked, so the row was a
	// non-pipe line and went to the commit buffer as prose while the rest of its
	// block sat in the park — the row whose HEAD was gone.
	if r.compPrefix != "" && looksPipedRow(r.compPrefix) {
		body, r.compPrefix = r.compPrefix+body, ""
	}
	// The ordinary body is appended VERBATIM: splitting and re-joining it would
	// re-add a terminator to its last (empty) element and commit a blank row the
	// answer never had (measured: an empty reasoning block gained a row, and a
	// plain answer did not reach the grid at all).
	if !strings.Contains(body, string(tablePipe)) {
		r.prefix += body
		return
	}
	for _, line := range strings.Split(body, "\n") {
		if looksPipedRow(line) {
			r.tableRows = append(r.tableRows, line)
			continue
		}
		r.prefix += line + "\n"
	}
}

// paintTable paints the parked run of pipe rows: the grid or the transposed
// field/value blocks, per the policy in table.go. A run that is NOT a table —
// pipe rows with no delimiter row under the first of them — is painted as the
// prose it is, one row per line, so the park costs the stream nothing but the
// delay that made the decision possible.
func (r *AnswerRenderer) paintTable() {
	rows := r.tableRows
	r.tableRows = nil
	if len(rows) == 0 {
		return
	}
	// The live row's partial paint is SUPERSEDED by the parked rows: the rows
	// below are those lines' whole text, painted on the same row. Leaving it
	// registered as the row's visible text made it the HEAD of the next line
	// committed, and fitCommitRow trims a row to what its head leaves — measured
	// as a table row arriving with its front cut away (`3b46107`, `d836a0`).
	// Nothing of it is lost: the parked line IS that text, and it is painted
	// whole here.
	r.liveText = ""
	content := r.contentCols()
	if tbl, consumed, ok := tableBlockAt(rows, 0, content); ok {
		for _, text := range tbl.form(content) {
			row := r.decideRow(text)
			row, drawable := fitCommitRow(row, "", text, r.cols)
			if drawable {
				r.writeRow(row, row.text)
			}
			r.finalizeLiveRow()
		}
		// A run can hold MORE than one table (a table, a blank, a table) — the
		// rows behind the one just painted go through the same decision.
		if consumed < len(rows) {
			r.tableRows = rows[consumed:]
			r.paintTable()
		}
		return
	}
	for _, line := range rows {
		row := r.decideRow(line)
		row, ok := fitCommitRow(row, "", line, r.cols)
		if ok {
			r.writeRow(row, row.text)
		}
		r.finalizeLiveRow()
	}
}

// contentCols is the band's CONTENT width: the cells a body row has left after
// the turn's gutter. It is the width a table grid must fit — the grid is
// painted as a body row, so its pipes and padding live inside this budget, and
// asking r.cols instead would let a "fitting" grid wrap (and a wrapped grid is
// not a grid).
func (r *AnswerRenderer) contentCols() int {
	if g := r.lineGutter(); g != "" {
		if w := r.cols - displayWidth(g); w > 0 {
			return w
		}
	}
	return r.cols
}

// releaseLive writes any in-flight text without committing it (used at the end
// of an answer, where the last line is deliberately NOT force-committed: the
// turn's final line stays live so Finish leaves the screen in the same single
// live row the whole answer used).
//
// This is the path that lets an over-long line out of the hold below: the line
// reaches the live row WHOLE, so writeLive's wrap promotes rows that were
// decided (the composer answered about the whole line above), and no provisional
// row is ever committed. Master's residue came from the opposite order — the row
// was promoted while the line was still unfinished (rowForPartial, markers
// literal), and a committed band row is never repainted:
//
//	chunk=6  row "…here **and the" / " bold conclusion follows** done"
//	         — markers on a committed row, and a longer line glues the
//	         provisional head on TWICE (the measured "paints markers liter…
//	         paints markers liter…"), because the decoder that then decided the
//	         whole line measured only its TAIL as new text and re-drew the head
//	         the band was still showing.
func (r *AnswerRenderer) releaseLive() {
	if r.pending.Len() == 0 {
		return
	}
	text := r.pending.String()
	r.pending.Reset()
	r.writeLive(text)
}

// unfinishedLineWouldWrap reports whether the in-flight text, painted onto the
// row a partial line is already occupying, would FILL that row — i.e. whether
// the very next write would promote a row into the band while the line is still
// unfinished.
//
// The measure is the renderer's own band arithmetic: the cells the row already
// shows (liveText, the carry the paint keeps for exactly this kind of question)
// plus the cells of the text about to join them, against the band's content
// width (which has the gutter's column already taken out, so a wide CJK line is
// judged by what the screen would show). An emphasis marker occupies no cell, so
// a marker-heavy line is not held for room it does not need.
func (r *AnswerRenderer) unfinishedLineWouldWrap(text string) bool {
	if len(r.liveText)+len(text) > answerMaxHeldLineBytes {
		// The backstop: a line this long is painted piecewise rather than making
		// the in-flight buffer without bound.
		return false
	}
	cols := r.contentCols()
	return cols > 0 && displayWidth(r.liveText)+displayWidth(text) > cols
}

// answerRow is one row a burst paints: its TEXT (characters only, no SGR) and
// the style it is drawn under — or, when spans is non-empty, the SPANS it is
// drawn as, each with its own emphasis. Keeping text and style separate until
// the moment of the write is what lets the renderer CUT a row by display cells
// and give both halves the same emphasis — the cut is arithmetic on characters,
// and the emphasis is applied after it, so an SGR byte can never be measured as
// a column.
//
// The two forms are exclusive: `st` draws the whole row in one style (code,
// diff, or the provisional style of an unfinished line), `spans` draws it as the
// body's emphasis layer decided (a complete plain line). A row never carries
// both.
type answerRow struct {
	text  string
	st    Style
	spans []LineSpan
	// skip is how many BYTES of `text` earlier rows of this same row have
	// already drawn (the head the row cutter cut off — writeRow's cutCols). The
	// spans index into the ROW's text, so a second row of a wrapped line must
	// be told where its characters START inside that text; consuming the spans
	// from their first byte for every row is what painted a span's tail over
	// the next row's plain characters (the partial-bold defect:
	// TestEmphasisRowCutKeepsTheWholeSpanBold). Zero for the first row of a row
	// and for every row that carries no spans, which is the path every
	// unemphasised line takes.
	skip int
	// allowCutLabel permits this row to wear the right-edge cut label (see
	// writeRow). It is true for a row cut out of a whole SOURCE line — where
	// the class test can be read off the row's own text — and false for a row
	// whose text is a CARRY: a head that was painted provisionally, or a
	// fragment replayed by commitPrefix, is a slice of a line whose full text
	// this row does not hold, so a break read off the slice would be a guess
	// about a boundary the row cannot see (and a row that already ends with the
	// glyph would be told it was cut when it merely ends).
	allowCutLabel bool
	// copied marks a row that is COPIED OUT OF THE SCREEN to be run — a line of
	// the answer's code/tool layer (a fenced or diff line), whose reader is as
	// often a paste as an eye. Its turn gutter is NOT drawn: no `▌ ` glyph, and
	// not its cells either, because a cell is a character and a character is
	// what a copy carries (a selection cannot tell the block's reserved column
	// from the command's own leading space). The row opens at column 0 and its
	// text IS the answer's line, byte for byte. See gutterGlyph.
	copied bool
}

// gutterGlyph reports whether this row's turn gutter is drawn at all.
//
// WHY A CODE ROW DRAWS NO GUTTER (measured 2026-10-02, the operator's paste leg:
// select a fenced command on the band → paste into a shell → the FIRST line dies
// with the word `▌`). The band's fenced block IS the answer's copying material,
// and every row of the answer opened with the turn's two-cell `▌ ` (TurnMarker,
// U+258C) — an ink that is not the command and not whitespace, so it cannot be
// pasted past. TWO candidates were rejected by measurement rather than taste:
//
//   - draw the gutter's two CELLS blank (keep the column, drop the glyph): the
//     row then still opens with `  `, and the acceptance is BYTE-EXACT — the
//     text a reader copies out of a block IS the command, not the command plus
//     the block's reserved column. A selection has no way to tell the two apart.
//   - strip on export: there IS no export. The copy is the TERMINAL's own
//     selection off the cell grid, so the grid is the artifact, and a stripper
//     would have to sit inside every reader of it (the operator's selection, the
//     dump's row TEXT, a paste into the model).
//
// So the layer's rows open at column 0, and the criterion is the one the layer
// ALREADY satisfies on its other half: a tool-activity row (`⚙ bash …`) is drawn
// with lineGutter() == "" (see r.noting) and has always opened at column 0 —
// the code/tool family is the reader's copying material, and its two halves now
// agree about where a copyable row begins. The prose rows keep the glyph, and
// with it the property TurnMarker exists for (see its comment: two consecutive
// turns' paragraphs, measured, ran together with no way to tell where one
// ended) — separability is a PROSE shape, and the fenced block marks itself with
// the answer's own fence rows, as it does in every other markdown reader.
func (r answerRow) gutterGlyph() bool { return !r.copied }

// writeLive writes the in-flight text onto the live row. With a region
// installed the live row is addressed ABSOLUTELY first, so the write never
// depends on where the previous output left the cursor (raw mode cleared OPOST,
// so a bare LF moves down without returning the column — a flow model would
// drift one column per line).
//
// The text APPENDS at the live row's current column (liveCol), because the
// in-flight line arrives in several cadence bursts and each burst must continue
// where the previous one stopped: placing the cursor at column 0 per burst made
// every burst overwrite the one before it.
//
// A row that fills the live row is FINALIZED — committed into the band with one
// scroll — and the remainder continues on the fresh live row. That is the same
// policy as a line break, applied to the wrap boundary, and it is why a long
// in-flight line loses no text (truncating at the row's end instead would
// silently drop the answer's tail until a newline happened to arrive).
func (r *AnswerRenderer) writeLive(text string) {
	if text == "" {
		r.inFlight = false
		return
	}
	if !r.out.Active() {
		// No region: the historic full-screen behaviour — the live row is
		// simply the current output position, one style for the whole chunk.
		r.write(r.pal.Answer.Wrap(stripSGR(text)))
		r.inFlight = true
		return
	}
	// The layout may have moved since the last write (the editing line grew and
	// took rows from the band). Re-anchor BEFORE any placement is composed, so
	// this burst's cursor sequence targets the band row the layout reserves —
	// never the editing line's row, whose repaint would erase it.
	r.reanchorLiveRow(r.pal.Answer)
	// This path carries the IN-FLIGHT tail: whatever arrived after the last
	// line break. Its finished lines (a burst may carry several) are decided
	// here; the unfinished one is painted under the style of the block the
	// stream is currently inside, because a streaming answer's characters may
	// never wait for a decision.
	//
	// The turn's marker column is applied per ROW inside writeRow (master's grid
	// invariant): each row the composition decides is a body row of the turn and
	// opens at the block's column, and every row a line WRAPS onto re-opens it.
	// It stays OUT of this function because the branch decides rows per LINE and
	// this function only hands them over — the gutter belongs where a row is
	// actually painted.
	for _, row := range r.rowsFor(text) {
		r.writeRow(row, row.text)
	}
}

// decideLine returns the style for one COMPLETE answer line, giving the composer
// the line's whole text — including any of it that an earlier burst already
// painted (compPrefix) — so a fence marker split across two bursts is still
// recognised as a fence.
func (r *AnswerRenderer) decideLine(line string) Style {
	if r.composer == nil {
		return r.pal.Answer
	}
	decision := line
	if r.compPrefix != "" {
		decision = r.compPrefix + line
		r.compPrefix = ""
	}
	return styleFor(r.pal, r.composer.StyleFor(decision))
}

// writeNote commits ONE caller-queued note row — as many band rows as its text
// needs.
//
// It exists because a note is a COMMAND, not prose. writeRow's continuation
// policy (cut at the row's last cell, continue on a fresh live row) is right for
// an answer line, whose wrap is the reader's own line break: an answer's row ends
// wherever the text reaches the edge, and nothing is lost. A note is the record
// of what the agent RAN, and its reader is as often a copy-paste as an eye — so
// the same policy there produced a transcript that does not execute:
//
//	⚙ call bash cd ~/work/GoPi; echo "=== s
//	tatus ==="; git status -sb | head -20
//
// (the operator's chat dump; a shell word split across two rows, and the
// continuation carrying the blank two-cell indent of a FRESH row, so nothing said
// the two rows were one command). Its sibling in the same transcript is
// `git branch+ fix/p` | `…` — a branch name cut in half.
//
// So a note wraps by a stricter rule than writeRow's:
//
//   - the break is taken at a BOUNDARY the text itself has — a space, or for a
//     long token with no space in it, one of cutNoteBoundary's separator
//     characters — which is what keeps an identifier, a path or a shell word on
//     one row. There is no character loss and no ellipsis: the tail continues on
//     the next row in full, which is the difference from the status row, whose
//     one-screen-row invariant forces it to DROP the tail (fitField).
//   - the next row opens with NoteContinuation (lineGutter), which is what makes
//     a continued row readable as one.
//   - the gutter's two cells are reserved BEFORE the boundary search, so a row
//     that needs the gutter can never end up two cells over the band.
//   - a head that still cuts a token carries NO marker: it is a plain PREFIX of
//     the text and the rest of the token opens the next row, so the rows are
//     LOSSLESS — strip the continuation gutter, join them, and the note is back
//     character for character. The note path therefore draws no cut glyph at all
//     (see NoteCutMarker — it survives for the status row's vocabulary, which
//     this row no longer deserves a glyph in); and the middle of the activity
//     block is where a reader is most likely to be reading a command rather than
//     prose, so a marker stolen from the row's own cells is text the reader
//     copies into a shell and cannot run (`…` between `-ma` and `xdepth`).
//   - the row width is the band's own (r.cols) rather than "the live row's
//     remaining cells", because a note is written at liveCol 0 on the band's live
//     row: the two were the same number here, and stating the smaller of them let
//     a note one cell wider than the screen render into the right margin.
//
// The answer and the turn's own chrome are untouched: this rule applies to the
// note path only, where the reader's expectation is a command rather than prose.
func (r *AnswerRenderer) writeNote(n noteRow) {
	text := n.text
	first := true
	// consumed is how many BYTES of the row's text the rows already committed
	// have drawn — the walk's ledger into n.runs (see noteRow.runs). It is only
	// read when the row is caller-composed, and it is byte-exact because a note
	// row's text is plain characters (no escape) by construction.
	consumed := 0
	for {
		cont, head, rest := r.cutNoteRowCopied(text, first, n.copied)
		if cont && !n.copied {
			// A COPYABLE row never opens its continuation with a marker: the glyph
			// would land inside the cells a selection carries (noteRow.copied).
			r.noteContinuing = true
		}
		if len(n.runs) == 0 {
			r.writeRow(answerRow{text: head, st: n.st}, head)
		} else {
			r.writeNoteRunRow(n.runs, consumed, head)
			consumed += len(head)
		}
		r.finalizeLiveRow()
		r.noteContinuing = false
		if rest == "" || head == "" {
			// rest == "": the note is fully committed. head == "": a band with no
			// cells to write into — the row is emitted empty rather than looping
			// on a cut that can never make progress (Rule 15: a bound this tight
			// is counted, not trusted).
			return
		}
		text = rest
		first = false
	}
}

// writeNoteRunRow draws ONE band row of a caller-composed note: head is the
// slice of the row's text that fits this row, at OFFSET into that text, and the
// row is drawn as the RUNS that slice covers — each under its own style.
//
// WHY THE SLICE IS TAKEN FROM THE RUNS AND NOT FROM THE TEXT. The cutter's
// boundary is a byte offset into the text (see cutAtBoundary), and the runs are
// the same bytes partitioned differently. Walking the runs to that offset is
// what makes a colour boundary survive a WRAP: the operator's 200-cell command
// crosses two band rows, and each row must draw the same emphasis its cells
// would have had on one.
//
// Every row is marked copied: a note row opens at column 0 with its own glyph
// and must not inherit the turn's `▌ ` gutter (lineGutter already answers ""
// while noting — this keeps the row honest even if that ever changes).
func (r *AnswerRenderer) writeNoteRunRow(runs []NoteRun, offset int, head string) {
	// ONE WRITE PER ROW, and that is the fix for a measured defect rather than a
	// preference: this row used to go through writeRow's per-piece loop, where
	// every piece is placed with its OWN absolute cursor sequence, and on a real
	// 80-column pty the pieces landed on top of one another (the grid read
	// `⚙⚙ ⚙ call⚙ call ⚙ call bash…` — the row's own prefix overprinted by each
	// later piece). A caller-composed row is a row of KNOWN cells, so it is
	// composed into one byte string and written at one position: no cursor
	// arithmetic per piece, and the cells of the command stay contiguous in the
	// stream a dump mirrors.
	// THE INTERSECTION IS TAKEN IN ROW COORDINATES, then shifted into head. A
	// run occupies [pos, next) of the row's text; this band row draws
	// [offset, offset+len(head)). The slice for the run is the intersection,
	// shifted by offset — computing it in the RUN's coordinates (the first
	// version of this function did) hands back a growing PREFIX of the row for
	// every run, which is what drew the operator's row as
	// `⚙⚙ ⚙ call⚙ call ⚙ call bash…` on a real pty.
	var b strings.Builder
	pos, end := 0, offset+len(head)
	for _, run := range runs {
		next := pos + len(run.Text)
		lo := pos
		if lo < offset {
			lo = offset
		}
		hi := next
		if hi > end {
			hi = end
		}
		if lo >= hi {
			pos = next
			continue
		}
		b.WriteString(run.Style.Wrap(head[lo-offset : hi-offset]))
		pos = next
	}
	out := b.String()
	if out == "" {
		// Bookkeeping mismatch (cannot happen: the runs concatenate to the row's
		// text). The head is drawn plain rather than dropped (Rule 15).
		out = head
	}
	// THE CONTINUATION COLUMN IS OPENED HERE, exactly as writeRow opens it: a row
	// that continues the note above carries NoteContinuation at column 0 and its
	// text begins after it, so the block keeps ONE left edge and a wrapped call
	// is distinguishable from a second one. cutNoteRow has already reserved the
	// marker's cells from this row's budget (see its `room`), so the head fits.
	if g := r.lineGutter(); g != "" && r.liveCol == 0 {
		r.writeGutter(g, true)
	}
	r.placeAndWrite(r.liveRow, r.liveCol, out)
	r.liveText += head
	r.liveCol += displayWidth(head)
	r.inFlight = true
}

// cutNoteRow answers how much of a note's text its next band row may carry: the
// prefix that fits (the continuation marker included, and never past r.cols) and
// the untouched remainder — plus whether the row it describes is a CONTINUATION
// (the caller then opens it with NoteContinuation, whose cells this function has
// already reserved).
//
// A note's continuation gutter is what the row's text is measured against, so the
// reservation happens HERE rather than at the draw: the boundary search must know
// how many cells it really has, or it hands back a head that does not fit once
// the gutter is written.
func (r *AnswerRenderer) cutNoteRow(text string, first bool) (cont bool, head, tail string) {
	return r.cutNoteRowCopied(text, first, false)
}

// cutNoteRowCopied is cutNoteRow for a row that is a PASTE SOURCE. Its cut is the
// HARD column cut the panel path uses (region.writeBandLine → cutCols), not the
// word-boundary search: `cutAtBoundary` breaks at a space or a separator and the
// break CONSUMES that character (noteTail), which is invisible when a remark is
// only read and is a changed paste when the row is a command block. Measured
// 2026-10-22 (Rule 31): `/info`'s `session dir  …/.gopi/sessions/<uuid>` came out
// as two rows split at a `/` with the block's own characters no longer
// reassembling into `chatInfoBlock`'s bytes — the same block that panel path drew
// as one hard-split row. So a copied row keeps the column cut and therefore the
// panel's layout AND its characters; a marker is never added for it either
// (noteRow.copied).
func (r *AnswerRenderer) cutNoteRowCopied(text string, first, copied bool) (cont bool, head, tail string) {
	if r.cols <= 0 {
		return false, text, ""
	}
	if copied {
		head, tail = cutCols(text, r.cols)
		return false, head, tail
	}
	// A CONTINUATION row reserves the marker's cells before the boundary search,
	// so a row that carries the gutter can never end up two cells over the band.
	// The FIRST row of a note carries no gutter (it has no predecessor to
	// continue), so it gets the band's own width — exactly what writeRow's
	// liveCol arithmetic gave it before, and therefore no reflow of any existing
	// transcript.
	cont = !first && displayWidth(NoteContinuation) < r.cols
	if fitsOnWidth(text, r.cols) {
		// The WHOLE remaining note fits the band on one row: no boundary search is
		// needed, and the row is the caller's text unchanged — this is the common
		// case, and the one that must stay byte-identical to what cmd/agent has
		// always printed.
		//
		// The continuation flag still reports the truth: a row that FINISHES a
		// wrapped note is a continuation of the row above it, and clearing the
		// flag here (measured: the second row of the operator's own command came
		// back unmarked) is exactly the ambiguity this marker exists to remove.
		return cont, text, ""
	}
	room := r.cols
	if cont {
		room = r.cols - displayWidth(NoteContinuation)
	}
	head, rest := cutAtBoundary(text, room)
	return cont, head, noteTail(text, rest)
}

// noteTail is the remainder a boundary cut left, with the single space the break
// consumed removed: the break IS the space between two words, so keeping it would
// indent the continuation by one cell the head did not spend.
func noteTail(text string, rest int) string {
	rest = clamp(rest, 0, len(text))
	tail := text[rest:]
	if strings.HasPrefix(tail, noteBoundarySpace) {
		tail = tail[len(noteBoundarySpace):]
	}
	return tail
}

// noteBoundarySpace is the space a boundary break consumes (see noteTail).
const noteBoundarySpace = " "

// cutNoteBoundaries are the characters a token is allowed to break at when no
// space will serve. They are the separators a reader tells a command's parts by —
// a path, a branch name, a flag and a key/value pair all break at one of them —
// so an identifier is never cut in the middle while a legal break exists anywhere
// inside the row's cells.
const cutNoteBoundaries = "/-._=:,"

// fitsOnWidth reports whether s occupies at most width display cells.
func fitsOnWidth(s string, width int) bool { return displayWidth(s) <= width }

// cutAtBoundary returns the text a note row may carry and the BYTE OFFSET where
// the next row's text starts. The break is the LAST boundary inside the row's
// cells — a space (consumed by the break) or one of cutNoteBoundaries (kept,
// because a path's own separator belongs to the text that continues), and the
// text's own end when it all fits.
//
// A row whose cells end INSIDE a token that has no boundary in them is the one
// case with no honest break: the head is then that token's cut PREFIX and the
// next row opens with the token's remainder — lossless, like every other arm
// here. It carries no marker, and that is the point of this path: see the last
// arm below for the sighting that settled it.
func cutAtBoundary(text string, width int) (head string, rest int) {
	if width <= 0 {
		return "", 0
	}
	fit, tail := cutCols(text, width)
	if tail == "" {
		return text, len(text)
	}
	end := len(fit)
	// A SPACE is the break a reader expects, and it is preferred over a
	// word-internal one — breaking at a hyphen of `fix/pushback-rule31-evidence`
	// ends the row on a fragment the reader has to re-read from the next row,
	// while the space before it ends the row on a whole word.
	//
	// …but only when the space actually USES the row (below noteWrapMinFill the
	// space break is refused and the search falls back to a separator, then to the
	// marked word cut — see noteWrapMinFillNum for the measurement), AND only when
	// the break really IS a boundary of the text's own structure.
	//
	// THAT SECOND CONDITION IS THE MEASURED DEFECT (a real 117x26 pty, the chat's
	// own ⚙ call row). The operator's command is a shell one-liner whose long
	// arguments are QUOTED, so the spaces a naive search finds inside `"=== … ==="`
	// are spaces INSIDE A SINGLE SHELL WORD. Breaking there splits a shell word
	// and — because the row was cut at a boundary — prints no `…` either:
	//
	//	⚙ call bash cd ~/work/GoPi ; … echo "=== status ===" ; echo "===        ← row 0
	//	↳ worktrees (the + prefix) ===" ; …                                    ← row 1
	//
	// Row 0 ends inside `=== worktrees`, a quoted argument's own interior, while
	// 48 cells of row 1 sat UNUSED: the row could have carried the rest, and the
	// reader copying a row gets a fragment of a token either way. A break at a
	// space is honest only where the text itself treats that space as a break —
	// outside quotes — which is what lastUnquoted keeps.
	//
	// …and when NO space outside a quoted run is in reach, the search must NOT
	// fall through to a separator inside one: the separators a shell line has
	// where its spaces are quoted are exactly the ones the row cannot break at
	// either (`"a/b"`, `"=== x ==="`, a URL in quotes — a break at their `/` or `=`
	// cuts the QUOTED ARGUMENT, which is the defect one level down). Measured on
	// the fixture's own third row: `↳ "=== does the branch exist anywhere locally? =`
	// — a quoted argument split at its trailing `=`, still unmarked, because a
	// separator break is a boundary break. So a note row's search keeps exactly
	// the boundaries the text offers: an unquoted space, an UNQUOTED separator, or
	// the marked word cut.
	if b := lastUnquoted(text[:end], end, noteBoundarySpace); b >= 0 && fillsNoteRow(text[:b], width) {
		// The break IS a space: it is consumed, so the continuation does not
		// inherit one cell of indent the head never spent.
		return text[:b], b
	}
	// …and a separator is a boundary only OUTSIDE a quoted run, for the same
	// reason: `"=== does the branch exist anywhere locally? ==="` has an `=` and a
	// `?` in it, and a row cut at one of them splits a single shell word — the
	// defect one level down, and still unmarked, because a separator break is a
	// boundary break. So a note row breaks where the READER would: at an unquoted
	// space, at an unquoted separator, or (when the text offers neither) at a
	// point it SAYS is a cut.
	if b := lastUnquoted(text[:end], end, cutNoteBoundaries); b >= 0 {
		// A separator is KEPT in the head: a path's own "/" belongs to the text
		// that continues, and dropping it would end the row with a token the
		// reader has to re-read from the next row.
		return text[:b+1], b + 1
	}
	if end == 0 {
		// A single rune wider than the whole row: emit it rather than loop.
		_, size := utf8.DecodeRuneInString(text)
		return text[:size], size
	}
	if !noteBreakInsideWord(text, end) {
		// The break is a RUNE boundary the text itself offers (a wide CJK run has
		// no spaces to break at, so every one of its runes is such a boundary, and
		// so is a run of punctuation): the row ends there and the next row takes
		// the rest. No glyph: marking it would claim a corruption that is not
		// there, and truncating it would drop characters the row had room for.
		return fit, end
	}
	// The row's cells end INSIDE a WORD — a bare identifier with no separator in
	// reach (a long hash, a URL path segment, a token with no punctuation). There
	// is no boundary to take, so the head is the word's cut PREFIX and the rest of
	// that word opens the next row: the row stops where its cells run out and the
	// next row is marked as a continuation (NoteContinuation), which is the whole
	// signal the reader gets — and the whole signal they need.
	//
	// IT CARRIES NO GLYPH, AND THAT IS THE MEASURED DEFECT THIS ARM USED TO HAVE.
	// The arm once returned text[:mark] + statusEllipsis. The status row's marker
	// is the right vocabulary for a status FIELD, whose one-screen-row invariant
	// DROPS the tail, so `…` there stands for text that is GONE. Here nothing is
	// gone — rest starts where the marked text stopped — so the glyph stands for
	// nothing, and it is drawn inside the note's own cells. The note is the record
	// of the command that RAN, read by a copy-paste as often as by an eye, so the
	// mark did not decorate the row: it CORRUPTED it. The operator's grid, a 112
	// column `gopi chat`:
	//
	//	009|… && find . -ma…
	//	010|xdepth 2 -type f …
	//
	// — `-ma` | `xdepth` is the single token `-maxdepth`, no character lost, but
	// selecting the two rows and pasting gives `-ma…xdepth`, which no shell runs.
	// The grid IS the artifact (there is no export step between the screen and the
	// clipboard — copied_text_test.go), so the marker has to leave the cells, and
	// the continuation gutter is all the reader needs to know the row was cut:
	// `↳` already means "this row continues the one above", while `…` means the
	// opposite (something was thrown away) — see NoteContinuation's own comment.
	return fit, end
}

// noteWrapMinFillNum / noteWrapMinFillDen are the fraction of a note row's
// budget a SPACE break must use before it is preferred over a word-internal cut.
// One half: a break that leaves a row less than half full is not a wrap, it is
// the transcript wasting a row (see cutAtBoundary — the long-token case).
const (
	noteWrapMinFillNum = 1
	noteWrapMinFillDen = 2
)

// fillsNoteRow reports whether head uses at least noteWrapMinFill of the width.
func fillsNoteRow(head string, width int) bool {
	return displayWidth(head)*noteWrapMinFillDen >= width*noteWrapMinFillNum
}

// noteQuoteChars are the characters that OPEN and CLOSE a quoted shell word — the
// contexts in which a space is text rather than a break (see lastUnquoted).
const noteQuoteChars = `"'`

// lastUnquoted is cutAtBoundary's own boundary scan: the byte offset of the last
// rune of s that IS a member of cls (or, for the space class, a space) while lying
// OUTSIDE every quoted run, or -1 when s holds none. Both classes a note row may
// break at go through it, so a quoted argument cannot be cut at its `/`, its `=`
// or a space inside it.
//
// // WHY A QUOTE SCAN AND NOT A RICHER SHELL PARSE: the text is a command being
// DESCRIBED, not executed, and the defect is one shape — a break printed in the
// middle of a quoted argument. Recognising the two quote characters is the
// smallest rule that separates "the text breaks here" from "a space happens to be
// here", and it cannot make a row worse: a text with no quotes is scanned exactly
// as before.
//
// A backslash-escaped byte is text and never a quote (a shell would not see it as
// one). An UNCLOSED quote simply leaves every later byte "inside" — the
// conservative direction, because it refuses a space break rather than taking one
// inside a word.
//
// The scan tracks quoting from the START of the text, because an offset's
// quotedness is a property of everything before it — one forward pass is what
// knows it. limit is the byte offset the row can carry (`cutAtBoundary` passes
// cutCols' own end), and the scan covers [0, limit), so a boundary sitting on the
// row's LAST cell is still a candidate.
func lastUnquoted(s string, limit int, cls string) int {
	if limit > len(s) {
		limit = len(s)
	}
	inside := false
	var quote byte
	best := -1
	for i := 0; i < limit; i++ {
		c := s[i]
		if c == '\\' {
			i++ // the escaped byte is text, whatever it is
			continue
		}
		if inside {
			if c == quote {
				inside = false
			}
			continue
		}
		if strings.IndexByte(noteQuoteChars, c) >= 0 {
			inside, quote = true, c
			continue
		}
		if cls == noteBoundarySpace {
			if c == noteBoundarySpace[0] {
				best = i
			}
			continue
		}
		if strings.IndexByte(cls, c) >= 0 {
			best = i
		}
	}
	return best
}

// noteBreakInsideWord reports whether a break at byte offset end lands in the
// middle of a WORD — a run of narrow ASCII letters, digits or underscores, the
// class every identifier, path component and shell word is built from. It is the
// one break no marker-free cut may take: `fix/pushback-rule31-evid` | `ence` is
// the operator's own sighting, a copy of either row does not run, and the reader
// has no way to tell the row was cut.
//
// A break beside a WIDE rune (CJK) is NOT inside a word: those scripts do not
// separate words with spaces, so every one of their runes is a legal break and a
// marker there would flag text that is not damaged. Neither is a break beside
// whitespace or punctuation, which the reader already sees as a boundary.
func noteBreakInsideWord(text string, end int) bool {
	if end <= 0 || end >= len(text) {
		return false
	}
	before, _ := utf8.DecodeLastRuneInString(text[:end])
	after, _ := utf8.DecodeRuneInString(text[end:])
	return isNarrowWordRune(before) && isNarrowWordRune(after)
}

// isNarrowWordRune is noteBreakInsideWord's class test: an ASCII letter, digit or
// underscore. Deliberately narrow (single-byte runes only): a wide rune is a
// script's own character rather than a token, so it never makes a break "inside a
// word".
func isNarrowWordRune(r rune) bool {
	if r >= utf8.RuneSelf {
		return false
	}
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// lastNoteBoundaryOld was the single-class form; the two-class form above
// supersedes it (a space wins a tie over a separator).
// writeRow writes one row of already-decided text to the live row: it is cut by
// display CELLS and the emphasis is applied to each piece AFTER the cut, so an
// SGR byte is never measured as a column. A row that fills the live row is
// finalized (committed into the band with one scroll) and the remainder
// continues on the fresh live row — the policy that keeps a long line from
// losing its tail.
//
// The row's intra-line emphasis reaches the screen HERE and nowhere else: the
// cut is arithmetic on the row's PLAIN characters (row.text, which is what the
// row is drawn as and therefore what occupies cells), and the pieces the cut is
// then split into carry the emphasis. A row whose text lost its markers in the
// tokenizer therefore costs no correction here — the markers were never in the
// arithmetic — and a monochrome palette renders every piece as its plain text,
// so the bytes are the row unchanged (the colour-off invariant).
func (r *AnswerRenderer) writeRow(row answerRow, text string) {
	if text == "" {
		r.inFlight = false
		return
	}
	// The turn's marker column goes FIRST, once per committed line: it is
	// written at the live row's column 0, so the body of the turn starts two
	// cells in and every line of the turn shares that left edge. It is only
	// ever emitted when the line it prefixes has content (an empty line — a
	// paragraph break — keeps its blank row, which is what makes paragraphs
	// readable inside a turn).
	// The turn's marker column is opened for the FIRST row of this call (the
	// gutter glyph) and re-opened for every row the text WRAPS onto inside the
	// loop below (a continuation indent of the same width, so no glyph appears
	// mid-line but the row's text still starts at the block's column). That is
	// what makes the turn's left edge a single cell column for EVERY body row —
	// a wrapped row included — which is the property the grid assertions read.
	// The line's only content may be its own break (\n): then the row is the
	// paragraph break and keeps its blank row.
	if g := r.lineGutter(); g != "" && r.liveCol == 0 && row.gutterGlyph() {
		r.writeGutter(g, true)
	}
	for {
		avail := r.cols - r.liveCol
		if avail <= 0 {
			// The row is exactly full: finalize it and start a fresh one.
			r.finalizeLiveRow()
			avail = r.cols
		}
		head, tail, halved := bandCut(text, avail)
		if head == "" && tail != "" {
			// A single character wider than the room left on this row: the row
			// is given up so the character starts on a fresh one (a wide rune is
			// never split across rows).
			r.finalizeLiveRow()
			continue
		}
		// THE RIGHT-EDGE CUT'S VOCABULARY (see cutLabelFor). A row whose head
		// ends between two NARROW WORD runes — an identifier, a path, a shell
		// word — and which has a tail to continue with has stopped for a reason
		// the reader cannot see: it is exactly a WRAPPED row's shape, so the
		// operator cannot tell a narrow terminal from a corrupt row. The class is
		// decided on the SOURCE (cutLabelFor, over the row's own whole text),
		// never on the drawn cells, and the row then SPENDS ONE CELL of its
		// budget on the product's cut label: the label displaces exactly the
		// row's LAST character, and that character LEADS the row below (it is the
		// tail's first character), so the band still shows every cell of the
		// answer, once.
		//
		// Three cases carry no label, each because it would lie about the cut: a
		// line that ENDS here (nothing follows, so nothing was cut — the tail is
		// empty); a row whose last character is a WIDE rune (that cell is the
		// rune's own continuation column, so there is no character to displace
		// without leaving the row short); and a row whose text is a CARRY rather
		// than a whole source line (answerRow.allowCutLabel). A fourth — the wrap
		// SPACE the continuation consumes (trimWrapSpace below) — is refused by
		// the class itself: a space is not a word rune, so the reader already sees
		// that boundary.
		cutLabel := noCutLabel
		displayed := head
		// A cut the row can SEE the end of wears no label: when one character is
		// the whole tail the row has left over (its last cell was just given up),
		// there is no next row — the displaced character is the rest of the line,
		// so the mark would claim a continuation the band never draws.
		if tail != "" && runeCount(tail) > 1 && row.allowCutLabel && r.cutLabelFor(text, len(head), halved) {
			// THE BAND'S OWN GLYPH, not the status row's ellipsis: this cut
			// DISPLACES a character onto the row below, it drops nothing, so it
			// may not wear the mark whose meaning is "text was omitted"
			// (answer_cut_label_glyph.go carries the measured misreading).
			cutLabel = answerCutLabel
			displayed = cutLabelCell(head)
			tail = text[len(displayed):]
		}
		for _, piece := range row.piecesFor(displayed, r.pal) {
			r.placeAndWrite(r.liveRow, r.liveCol, piece.bytes)
			// The carry records what the row SHOWS (the plain characters), not
			// the bytes written for them: a later commit replays this text, and
			// replaying SGR bytes would double the emphasis and, worse, put
			// escape bytes into the width arithmetic.
			r.liveText += piece.text
			r.liveCol += displayWidth(piece.text)
		}
		if cutLabel != noCutLabel {
			r.writeCutLabel(displayed, cutLabel, row.st)
		}
		// The spans index into the row's WHOLE text, so the next row of this row
		// is told how much of it this one drew: without it the walk restarted at
		// the spans' first byte and emphasised the wrong characters (the
		// partial-bold defect). It is a no-op for a row with no spans, which is
		// every row of every unemphasised answer.
		//
		// The ledger is what the next row of this row starts its span walk at, and
		// it is therefore a count of the bytes of row.text that this pass put
		// BEHIND the walk — the cells it drew, the character a cut label
		// displaced onto the row below, AND the wrap spaces the continuation
		// CONSUMES (trimWrapSpace drops them from the drawn text, but the spans
		// were built over row.text WITH them). The count is taken against the
		// text the next pass will actually walk — the tail as this pass leaves
		// it — rather than against the drawn cells, because the two differ
		// exactly by those consumed spaces: counting only what was drawn left the
		// ledger one byte SHORT per space-consuming wrap, so the mark drifted one
		// character forward for every wrap of the row (the operator's 117-column
		// capture measured one column per wrap). A row whose label displaced its
		// last character is NOT a special case here: that character leads the row
		// below, so it is already inside the bytes this pass removed.
		r.inFlight = true
		if tail == "" {
			return
		}
		// A line that filled its row wraps: the continuation starts on a fresh
		// row, still inside the turn's block, with the indent the block's column
		// requires (never the glyph — the glyph marks a LINE of the turn, and a
		// wrap is the same line).
		// Whatever is still on the row past this one is STALE: a provisional paint
		// drew an unfinished line's markers as literal cells, so it can be wider
		// than the decided row put over it — and the band never repaints a
		// promoted row. Cleared here, while the row is still live. A row already
		// full to its last cell has nothing stale on it, so the guard is the
		// arithmetic and not an optimisation: the erase at column == width addresses
		// the next row.
		if r.liveCol < r.cols {
			r.placeAndWritePrefixed(escEraseToEnd, r.liveRow, r.liveCol, "")
		}
		r.finalizeLiveRow()
		if g := r.lineGutter(); g != "" && row.gutterGlyph() {
			r.writeGutter(g, false)
			// The continuation row has just re-opened the block's column, so a
			// wrap space at the head of the text would be drawn INSIDE it and
			// the row's first character would land one cell past the column
			// (see trimWrapSpace). The row that OPENS a line is untouched: its
			// leading spaces are the author's indentation.
			tail = trimWrapSpace(tail)
		}
		row.skip += len(text) - len(tail)
		text = tail
	}
}

// answerPiece is one run of a row's text together with the exact bytes to write
// for it (the run's characters under its own emphasis). The two are kept apart
// because they answer different questions: `text` is what the row SHOWS — the
// live-row carry and the cell count are arithmetic on it — and `bytes` is what
// goes on the wire, which is the same characters plus SGR for a colour-capable
// palette and the very same characters for a monochrome one.
type answerPiece struct {
	text  string
	bytes string
	// st is the style the piece's bytes were wrapped in — kept so two adjacent
	// pieces carrying the SAME style can be coalesced into one (see appendPiece:
	// the coalescing is what keeps a monochrome run's bytes identical to the
	// un-emphasised answer's).
	st Style
}

// piecesFor answers the pieces of `head`, the first head-cells-worth of this
// row: one piece per emphasis run the row's spans describe. It is the ONE place
// a span becomes bytes, and it is deliberately asked per ROW rather than per
// line — the row cutter hands over the characters that fit the row it is filling
// (see cutCols), so a wrapped emphasised span is emphasised on both of its rows
// without the cutter knowing what emphasis is.
//
// A row with no spans (a code line, a diff line, or an unfinished line painted
// provisionally) is one piece in the row's own style. A monochrome palette makes
// every piece its plain text, so the concatenation of the pieces is `head` byte
// for byte — which is the colour-off invariant stated at the only place that
// could break it.
func (row answerRow) piecesFor(head string, pal Palette) []answerPiece {
	if head == "" {
		return nil
	}
	if len(row.spans) == 0 {
		return []answerPiece{{text: head, bytes: row.st.Wrap(head), st: row.st}}
	}
	pieces := make([]answerPiece, 0, 4)
	rest := head
	// A row the cutter cut is the row's text from row.skip on: the spans are
	// walked from there, so a span the cut ENDED is not re-opened on the row that
	// continues it, and a span the cut BEGAN mid-line — or a span that spans the
	// cut — is emphasised from its own first byte drawn here (see answerRow.skip).
	// Without this the walk restarted at the spans' first byte for EVERY row, so
	// the second row of a wrapped line was styled as if its text were the row's
	// text: a span's tail emphasised the plain characters that followed it.
	pos := row.skip
	for _, sp := range row.spans {
		if rest == "" {
			break
		}
		if pos > 0 {
			// The span is wholly or partly behind this row: skip as much of it as
			// the row has already drawn. A skip past a whole span moves to the
			// next one; a skip INTO a span starts the piece at that byte.
			if pos >= len(sp.Text) {
				pos -= len(sp.Text)
				continue
			}
			partial := sp.Text[pos:]
			pos = 0
			n := len(partial)
			if n > len(rest) {
				n = len(rest)
			}
			part := rest[:n]
			rest = rest[n:]
			pieces = appendPiece(pieces, answerPiece{text: part, bytes: styleForSpan(pal, sp.Emphasis).Wrap(part), st: styleForSpan(pal, sp.Emphasis)})
			continue
		}
		n := len(sp.Text)
		if n > len(rest) {
			n = len(rest)
		}
		part := rest[:n]
		rest = rest[n:]
		pieces = appendPiece(pieces, answerPiece{text: part, bytes: styleForSpan(pal, sp.Emphasis).Wrap(part), st: styleForSpan(pal, sp.Emphasis)})
	}
	if rest != "" {
		// The spans do not cover the row (they never should — a row is built
		// with text == SpanText(spans)). The remainder is written as plain body
		// text rather than dropped: losing answer characters to a bookkeeping
		// mismatch is the one failure this path must not have, and the
		// assertion that keeps the invariant is TestAnswerRowTextIsItsSpanText.
		pieces = appendPiece(pieces, answerPiece{text: rest, bytes: row.st.Wrap(rest), st: row.st})
	}
	return pieces
}

// appendPiece adds a piece, COALESCING it with the previous one when the two
// carry the same style. The coalescing is not an optimisation: the row's pieces
// are written one at a time, each preceded by its own absolute cursor placement
// (the renderer's cursor bookkeeping), so a split that carries no style change
// would put extra cursor motion on the wire — and under a monochrome palette,
// where EVERY style is the empty one, that is the only difference a marker could
// make to the byte stream. Merging same-style neighbours is therefore what makes
// the colour-off stream byte-identical to the same answer written without
// emphasis (TestEmphasisColourOffBytesAreUnchanged), rather than merely
// character-identical.
func appendPiece(pieces []answerPiece, p answerPiece) []answerPiece {
	if n := len(pieces); n > 0 && pieces[n-1].st == p.st {
		pieces[n-1].text += p.text
		pieces[n-1].bytes += p.bytes
		return pieces
	}
	return append(pieces, p)
}

// rowsFor returns the rows a burst is painted as, with their emphasis resolved.
// With a composer the burst is split into LINES and each complete line is asked
// about ONCE; without one the whole burst is a single row in the answer body's
// own style.
func (r *AnswerRenderer) rowsFor(text string) []answerRow {
	if r.composer == nil {
		return []answerRow{{text: text, st: r.pal.Answer, allowCutLabel: true}}
	}
	return r.computeRows(text)
}

// rowFor draws a COMPLETE line: the composer decides the LINE's style, and — on
// a plain line, where the body's own emphasis layer is in charge — the tokenizer
// decides the SPANS inside it.
//
// The gate on the span path is the composer's verdict, not a marker test in the
// renderer (Rule 1b): inside a fence the line is CODE, and code's `**` is the
// literal the code block says it is, so a code or diff line is drawn in one
// style as the text it is. On a plain line the whole body is the tokenizer's
// business, markers included.
//
// The line's KIND is the layer table's business (layers.go): a fenced or diff
// line belongs to LayerCodeTool, whose style the table answers — the mapping
// from "what a line IS" to "how the table draws it" therefore has ONE home, and
// this function no longer reaches for styleFor itself.
func (r *AnswerRenderer) rowFor(line string, ls LineStyle) answerRow {
	st := layerStyleFor(r.pal, layerForLine(ls), ls)
	// A code/tool-layer line is the reader's COPYING material (see
	// answerRow.copied): its gutter keeps its two cells and loses its glyph.
	copied := layerForLine(ls) == LayerCodeTool
	// The marker's SPELLING is a question for the format layer, so the renderer
	// asks it there (hasBoldMarker) rather than looking for the marker itself;
	// a line that certainly carries no emphasis takes the plain-text shape and
	// pays for no span bookkeeping — which is every line of every answer today.
	//
	// allowCutLabel: this row's text IS a whole source line, so the row cutter
	// may read the break class off it (see answerRow.allowCutLabel).
	if ls != LinePlain || !hasBoldMarker(line) {
		return answerRow{text: line, st: st, allowCutLabel: true, copied: copied}
	}
	spans := LineSpans(line)
	return answerRow{text: SpanText(spans), st: st, spans: spans, allowCutLabel: true, copied: copied}
}

// rowForPartial draws an UNFINISHED line, whose emphasis cannot be decided yet.
//
// The decision is deferred, not guessed: the visible text is painted now — a
// streaming answer's characters may never wait for a marker to close — but it is
// painted WITHOUT emphasis and WITH its markers intact, because the marker's
// removal is itself part of the undecided emphasis. Removing a marker on the
// strength of a split point would be committing to a decision the next burst can
// revoke (`**bo` → `ld**` is bold; `**bo` → `ld` is literal `**bold`), and text
// a reader can see may not change its letters when the next frame arrives.
//
// The line is decided once, when its break arrives: computeRows' terminated
// branch asks the tokenizer about the line's WHOLE text, so `**bo` + `ld**\n`
// paints `bold` bold in one go, and `**bo` + `ld\n` paints `**bold` — the
// unclosed-marker rule, at the moment the line became knowable.
func (r *AnswerRenderer) rowForPartial(seg string) answerRow {
	style := r.composer.Open()
	// An in-flight line INSIDE a fence is the code block's own text, so it paints
	// under the code layer's gutter rule from its first cell (see
	// answerRow.gutterGlyph): a provisional paint that carried the glyph and a
	// decision that dropped it would leave the choice to WHERE a burst fell.
	return answerRow{text: seg, st: styleFor(r.pal, style), copied: style == LineCode}
}

// computeRows lays the burst out as ROWS, with the answer's emphasis resolved per
// LINE. It splits at newline boundaries (lineSegments) and never at display
// columns: the row's width is enforced by writeLive's cut, the one component
// that knows the drawing width — so the emphasis layer holds no row arithmetic
// and cannot shear a row.
//
// The composer is asked ONCE PER COMPLETE LINE, and it is asked about the WHOLE
// line: the text of the line already painted by earlier bursts is carried in
// compPrefix, so a fence marker split across two bursts is still recognised as a
// fence. What was already painted is never repainted — the decision changes the
// emphasis of the text that FOLLOWS the marker, not the bytes already on the row.
//
// The unfinished tail of the burst is painted PROVISIONALLY, under the style of
// the block the stream is currently inside (composer.Open): a line's emphasis
// may lag its characters, but its characters never wait for it.
func (r *AnswerRenderer) computeRows(text string) []answerRow {
	rows := make([]answerRow, 0, len(text)/rowEstimateDivisor+lineSegmentsSlack)
	for _, seg := range lineSegments(text) {
		content, terminated := lineEndingSplit(seg)
		if !terminated {
			r.compPrefix += content
			// A pipe row's provisional paint is WITHHELD: its fate is decided by
			// the line AFTER it — it is a table's header iff a delimiter row
			// follows it (see table.go) — and nothing in the row itself says
			// which. The line is painted once, whole, when its successor settles
			// what it is; the band never repaints a committed row, so guessing
			// here is not recoverable.
			if looksPipedRow(r.compPrefix) {
				continue
			}
			rows = append(rows, r.rowForPartial(seg))
			continue
		}
		decision, partial := content, ""
		if r.compPrefix != "" {
			decision, partial = r.compPrefix+content, r.compPrefix
			r.compPrefix = ""
		}
		// The head is the text the live row ALREADY shows: a decided spelling
		// (headSpans, see there) of the partial this line's earlier half was
		// painted with, so its markers are not carried onto a row the band never
		// repaints.
		row, ok := fitCommitRow(r.rowFor(decision, r.composer.StyleFor(decision)), partial, seg, r.cols)
		if !ok {
			// Not even the text the stream declared fits one row (a run of WIDE
			// characters the terminal cannot hold): the row is kept as the text
			// the row can hold and the remainder is left for the next burst,
			// rather than written and sheared.
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// tableHolding reports whether the tail in flight belongs to a markdown table
// (see table.go): the stream is inside, or immediately past, a run of pipe rows
// whose block is not painted until it is complete enough to decide a form from.
func (r *AnswerRenderer) tableHolding() bool {
	if r.tableRows != nil {
		return true
	}
	return looksPipedRow(r.compPrefix)
}

// resetComposer returns the composer to its pristine, stream-start state: the
// composer is a per-STREAM classifier, so a stale fence from the previous answer
// must not carry this answer's code emphasis into prose.
//
// The reset goes through the LineComposer's own EndStream when the composer is
// one, and only falls back to overwriting the struct for a type that has no such
// seam — overwriting a struct is not something this renderer may do to a composer
// it did not build (the `*c = LineComposer{}` was the ONLY reset, and for any
// other implementation it was a silent no-op: the stale fence then survived the
// answer boundary with nothing to report it, Rule 15).
func (r *AnswerRenderer) resetComposer() {
	if c, ok := r.composer.(streamResetter); ok {
		c.EndStream()
		return
	}
	if c, ok := r.composer.(*LineComposer); ok {
		*c = LineComposer{}
	}
}

// cutLabelFor decides whether a row cut at byte offset `cut` earns the band's
// cut label (`answerCutLabel` — `↵`, the BAND's own continuation mark; it is
// deliberately not the status row's `…`, see answer_cut_label_glyph.go).
//
// Its meaning is NARROW, and the narrowing is the disposition `bandCut` added
// (band_word_move.go): a row wears it only when the token it stops inside is
// wider than the WHOLE row, i.e. the one cut that has no wider row to move the
// token to. Every other mid-token break moves the token down whole, so no label
// is owed there — the row ended where the reader's own token does.
//
// `halved` is bandCut's verdict on the SAME cut it took, so the label is read off
// the row's geometry rather than re-derived from the source: a re-derivation here
// would label rows bandCut had already repaired. The one question answered HERE is
// the label's own — it displaces the row's LAST character, which must be a
// SINGLE-CELL rune (a wide rune's last cell is its own continuation column, so
// there is no whole cell to give up) — plus the row's own ends, where there is
// nothing to displace.
func (r *AnswerRenderer) cutLabelFor(text string, cut int, halved bool) bool {
	if cut <= 0 || cut >= len(text) {
		return false
	}
	last := runeAtEnd(text[:cut])
	if last == 0 || runeCells(last) != 1 {
		return false
	}
	return halved
}

// runeCount is how many characters text holds — the arithmetic the cut label's
// last case needs (a tail of one character is the whole remainder of the line).
func runeCount(text string) int { return utf8.RuneCountInString(text) }

// cutLabelCell is the head's text as the row shows it: the whole head under a
// budget one cell SHORTER, i.e. the head's last character removed so the label
// can take its cell.
//
// The head was cut for the row's full width, so a plain slice by one character
// yields exactly one column less — which is what makes the label FREE at the
// grid level: the row is as wide as it was, the label's cell is the cell the
// displaced character held, and the displaced character is what the caller hands
// to the row below.
func cutLabelCell(head string) string {
	_, size := utf8.DecodeLastRuneInString(head)
	return head[:len(head)-size]
}

// runeAtEnd is head's last rune, or 0 when head is empty.
func runeAtEnd(head string) rune {
	if head == noCutLabel {
		return 0
	}
	last, _ := utf8.DecodeLastRuneInString(head)
	return last
}

// writeCutLabel draws the cut label in the row's own trailing STYLE, so the
// label never starts a marked run of its own: a cut that lands inside a bold run
// ends that run after its last drawn character, and the glyph that says "this row
// was shortened" is styled as the row's body rather than as the thing that was cut
// off.
//
// It asks piecesFor for the row's head PLUS the label under a row of NO SPANS and
// takes the last piece — which is what the piece walk does to that text, with the
// label now the rune at the cut offset. A row with no spans is one plain piece of
// its own style, and that is the style the label wears; the constant-colour
// invariant is why the walk needs no second rule (a label styled by a rule of its
// own would be the second definition this avoids).
// writeCutLabel draws the row's cut label in the ROW's own style (the style of
// the run the cut ended), so a cut closes the marked run it interrupted instead of
// letting the glyph that announces it become a styled run of its own.
//
// It counts the label's cell in liveCol — the screen column, not the source
// offset — which is what keeps the row's width arithmetic honest while the label
// itself stays out of the source (liveText). The character the label displaced is
// NOT lost here and is not carried in a ledger: it is exactly the first character
// of the text writeRow continues with (`tail`), which is painted on the row below
// by the same call. A second copy of it anywhere — a "seed" re-applied to a
// continuation — is the duplication the cell-loss rig catches.
func (r *AnswerRenderer) writeCutLabel(displayed, label string, st Style) {
	// THE RECORD OF WHAT WAS NOT THE ANSWER'S OWN (see cutLabels). A cut label
	// is the ONE cell this renderer puts on screen that the model did not write:
	// it says "this row was shortened", and it is deliberately absent from
	// liveText because liveText is the ANSWER's source. That absence is exactly
	// why a reader comparing the screen against the wire needs this record: a
	// `…` on a row can be the operator's own text (the model wrote it) or this
	// glyph, and the drawn cells cannot tell them apart. Recording the fact HERE,
	// at the one place the glyph is drawn, is what lets a report name the layer
	// instead of guessing at it — a caller re-deriving the cut class from the
	// source would be a second derivation that can disagree with the paint.
	r.cutLabels.add(r.turnIndex, r.liveRow)
	r.placeAndWrite(r.liveRow, r.liveCol, st.Wrap(label))
	// The label is NOT added to liveText: liveText is the row's SOURCE (what a
	// later commit replays as the row's text), and the label is not a character
	// of the answer — it is the renderer telling the reader the row was
	// shortened. liveCol still counts its cell, because the column arithmetic is
	// about the screen, not about the source.
	r.liveCol += displayWidth(label)
}

// AnswerBoundary closes the classifier's stream at an ASSISTANT-MESSAGE boundary:
// the caller has seen one message of the turn end and the next begin, and the
// block the previous message left open is that message's unfinished quote.
//
// It exists because the renderer cannot see that boundary. Its input is one
// stream of deltas, and a turn may carry SEVERAL assistant messages (a tool call
// ends one and the next answer begins), so a caller that knows the message ended
// says so here. Without it the classifier's open block outlives its text —
// measured: a message ending inside a fence it never closed leaves the NEXT
// message's table row painted as code with its `**bold**` markers literal, which
// is the operator's reported screen (evidence: internal/lineedit's red test).
//
// It is safe to call more often than necessary: the answer end (Begin) calls the
// same reset, so a boundary announced twice is one reset.
// AnswerBoundary closes one ASSISTANT MESSAGE: the composing classifier's open
// block is dropped, so a fence the message left open cannot carry the next
// message's prose into code. It is a routed call like the rest of the family —
// the composer's fence is renderer state the read loop reads when it draws a row
// (LineComposer.StyleFor ← decideLine ← commitLines), so resetting it from the
// turn's goroutine while the loop classifies a line was one of the measured
// pairs (inFence↔EndStream).
func (r *AnswerRenderer) AnswerBoundary() {
	if r.routed(answerOp{kind: answerOpBoundary}) {
		return
	}
	r.answerBoundaryNow()
}

// answerBoundaryNow is AnswerBoundary's own body (see beginNow for who runs it),
// and it carries the composer through the seam that OWNS the reset — the
// composer read from Composer() at the one moment the renderer is this
// goroutine's.
func (r *AnswerRenderer) answerBoundaryNow() {
	r.resetComposer()
}

// streamResetter is the reset seam, asked for by BEHAVIOUR rather than by type:
// a composer that knows how to end a stream implements it (LineComposer does),
// and one that does not is left alone rather than overwritten.
type streamResetter interface {
	// EndStream drops state that belonged to the stream that just ended.
	EndStream()
}

// writeGutter opens a live row at the turn's marker column: the glyph when the
// row starts a LINE of the turn (glyph=true), or its width in spaces when the
// row continues one (a wrap). Either way the cells reserved are exactly
// displayWidth(g) — the width the block's column is built on — and the turn's
// ledger of "a reasoning/answer line was written" is updated here, in one place,
// so BeginThinking/EndThinking cannot disagree with what is on the screen.
//
// It is called from writeLive (the row start) and from writeRow's wrap path (the
// continuation), so a row painted by the per-line composer and a row wrapped by
// the cell cut share ONE gutter rule and one ledger update.
func (r *AnswerRenderer) writeGutter(g string, glyph bool) {
	text := g
	if !glyph {
		text = strings.Repeat(" ", displayWidth(g))
	}
	r.placeAndWrite(r.liveRow, 0, text)
	// The gutter is NOT added to liveText, which is defined as "what this row's
	// BODY shows": every carry path (writeRow's head, commitPrefix's head,
	// Finish's tail) treats liveText as the row's own text, so a gutter carried
	// that way is REPLAYED as body cells — measured as the turn marker drawn
	// twice on one row (`▌ ▌`, the operator's sighting) on a markdown table
	// wider than the band, where the carry fires. A gutter is a COLUMN, not
	// text: it is drawn here, and every path that later paints this row re-opens
	// it from lineGutter() at liveCol 0. liveCol still counts its cells, because
	// the column arithmetic is about the screen, not about the carry.
	r.liveCol += displayWidth(text)
	if r.thinking {
		r.thinkingWrote = true
	} else {
		r.ledged = true
	}
}

// finalizeLiveRow promotes the live row into the band with one scroll and
// resets the live column: the row's text is now committed and is never written
// again (the A5 invariant, counted in committed).
func (r *AnswerRenderer) finalizeLiveRow() {
	r.out.commitScroll()
	r.committed.Add(1)
	// The parked in-flight line belongs to the ROW BEING COMMITTED, so a
	// finalize drops it. That is not tidiness: compPrefix is the row's
	// unfinished text, and a row this function promotes has none left. Leaving
	// it parked made the NEXT line's row arithmetic compute on
	// <parked>+<line>, a string that line never was — measured on a real turn at
	// 117 and 80 columns with a markdown table wider than the band: the header
	// BeginTurn paints with no trailing break stays parked, and the row that
	// follows is composed as header+row, so the row's text is TRIMMED to make
	// room for a head no row is showing. That is the operator's row whose HEAD
	// was eaten while its neighbour (whose predecessor was an ordinary complete
	// line) was whole.
	r.compPrefix = ""
	r.liveCol = 0
	r.liveText = ""
	// The promoted row is the layout's CURRENT live row: the promotion may run
	// after the layout moved (a commit triggered by a submitted message whose
	// editing line grew), and re-reading keeps every later write on the band.
	r.refreshLiveRow()
	r.liveRow = r.out.LiveRow()
}

// clearLiveRow empties the answer's live row (start of a turn).
func (r *AnswerRenderer) clearLiveRow() {
	r.liveCol = 0
	r.liveText = ""
	if !r.out.Active() {
		return
	}
	// The live row is the band's last row; an absolute erase is the only safe
	// way to clear it without touching what is above it.
	r.liveRow = r.out.LiveRow()
	r.placeAndWritePrefixed(escEraseLine, r.liveRow, 0, "")
}

// forgetDrawnStatus drops the record of what the status row currently shows, so
// the next draw WRITES instead of being skipped as "nothing changed".
//
// It exists for the one caller that knows the row was disturbed by something the
// renderer did not do: a diagnostic written to errOut over the layout (see
// Editor.repaintAfterDegradation). The renderer's "this is what the row shows"
// record is only a fact about its own writes, and a sentence printed through the
// row makes it a stale fact — the shape that would otherwise keep a covered
// status row covered for ever.
func (r *AnswerRenderer) forgetDrawnStatus() {
	if r == nil {
		return
	}
	r.drawnStatus = statusKey{}
}

// drawStatus repaints the status line on the region's reserved bottom row. The
// status row is OUTSIDE the region, so repainting it cannot move the answer
// (and it is the only row that is legitimately repainted).
//
// It draws only when what is on the row would CHANGE (row, width or text). That
// is not an optimisation for its own sake: the row is repainted by the read loop
// on every status PUBLISH, and a publish storm of identical values (a tick every
// event) must not become a write storm. The key includes the row and the width
// so a resize still redraws (a new geometry is a new row for the same text).
func (r *AnswerRenderer) drawStatus() {
	if r.status == nil || !r.out.Active() {
		return
	}
	key := statusKey{row: r.out.StatusRow(), cols: r.cols, text: styledStatusLine(r.status(), r.cols, r.pal)}
	if key == r.drawnStatus {
		return
	}
	r.drawnStatus = key
	r.statusWrites.Add(1)
	r.placeAndWritePrefixed(escEraseLine, key.row, 0, key.text)
}

// statusKey identifies the status line currently painted: the row it is on, the
// width it was composed at, and its text. Equal key = nothing to repaint.
type statusKey struct {
	row  int
	cols int
	text string
}

// statusLine composes the one-line status within cols cells. It is the single
// definition of the status row's rendering, and it is TOTAL in cols: whatever
// it returns occupies at most cols display cells, so the row it is drawn on
// holds exactly one screen row (a wrapped status line would take a row from the
// editing line above it — the one thing the layout must never sacrifice).
//
// Two policies cooperate, in this order:
//
//  1. Whole fields are dropped from the right (fitStatusFields) — a missing
//     trailing field reads as a narrow terminal, and the live hint is the one
//     part that must survive (it is what changes while the agent works).
//  2. The LAST resort — a single field too long for the room it has, i.e. a
//     long tool digest on a narrow terminal — is cut with an ellipsis
//     (fitField). A cut string would read as corrupted data; a cut WITH the
//     marker reads as a field that continues.
//
// width <= 0 means "no bound" (the caller drew nothing, or the width is
// unknown) and is the only input for which the result may exceed the row.
func statusLine(s Status, cols int) string {
	if cols <= 0 {
		return s.Text()
	}
	if aligned, ok := statusLineColumns(s, s.layoutCols(cols)); ok {
		return aligned
	}
	live := fitField(s.Live, cols)
	label := s.Label
	if live == "" {
		return fitField(label, cols)
	}
	// The separator between label and live text (matching Status.Text).
	gap := StatusLineGap
	room := cols - displayWidth(live) - displayWidth(gap)
	if room < 0 {
		// Not even the live hint fits: show as much of it as there is room for.
		return live
	}
	label = fitStatusFields(label, room)
	if label == "" {
		return live
	}
	return label + gap + live
}

// styledStatusLine is statusLine plus the palette's emphasis: the row is composed
// and TRUNCATED first (exactly as the monochrome path does), and only then are
// the leading label fields wrapped — the live hint keeps its own, stronger
// style, and the tool-activity row is styled as tool activity rather than as a
// generic label. Because styling happens after the fit, it can never move a
// field across the row's cell budget: the row is still exactly one screen row.
//
// The styling is derived from what the monochrome row ALREADY says — the left
// part is the label, the right part is the live hint (statusLine's own
// composition decides where the split falls) — so colour adds emphasis to a
// distinction the text already makes, and never carries one of its own.
//
// ON THE ALIGNED PATH the fields are the producer's own columns
// (statusLineColumns), and a column may carry a MEASURED-QUALITY span inside its
// text (StatusColumn.Emphasis). Such a span cannot be styled by wrapping the
// column: styledFields' style is per-FIELD identity, and an inner sequence
// spliced into an already-styled field would reset the field's colour at its own
// close (the nested-reset defect). The columns are therefore rendered by
// statusAlignedRow, which re-states the field style across every span.
func styledStatusLine(s Status, cols int, pal Palette) string {
	plain := statusLine(s, cols)
	if pal == MonoPalette {
		return plain
	}
	if row, ok := statusAlignedRow(s, cols, plain, pal); ok {
		return row
	}
	label, live := splitStatusRow(plain, s, cols)
	// The UNALIGNED path styles the label field by field exactly as it always
	// has (styledFields' identity split on StatusSep, the separator left bare) —
	// with the measured-quality span re-stated inside whichever field carries it.
	// One implementation, so the two paths cannot disagree about what a field
	// looks like; the aligned path composes its own columns only because their
	// boundaries are the producer's list, not a separator this package would have
	// to match on.
	styledLabel := styledFieldsEmphasis(label, StatusColumn{EmphasisSpan: s.LabelEmphasis, Emphasis: s.Emphasis}, pal)
	if live == "" {
		return styledLabel
	}
	liveStyle := pal.StatusLive
	if s.Thinking {
		liveStyle = pal.Thinking
	}
	return styledLabel + liveStyle.Wrap(live)
}

// statusAlignedRow renders the ALIGNED status row with each column's own
// emphasis, and reports false when the row on screen is not that form.
//
// WHY A SECOND COMPOSITION RATHER THAN A POST-PASS OVER `plain`. The column
// boundaries are only recoverable from the producer's own column list: `plain`
// is a joined, padded string, and splitting it back apart would mean matching
// the separator (a string) inside a rendered row — the bare-string keyword this
// row's other decisions explicitly refuse. The composition is repeated instead,
// from the SAME inputs statusLineColumns used (the columns' padded texts, their
// separators and the layout width), through padStatusColumn and the same two
// budget checks — which is why the result is asserted CELL-FOR-CELL equal to the
// plain row before it is returned: a second composition that disagreed with the
// first would draw a row the fit was never applied to, and that is a failure
// here (false), never a styled row with the wrong cells.
func statusAlignedRow(s Status, cols int, plain string, pal Palette) (string, bool) {
	if len(s.Columns) == 0 {
		return "", false
	}
	layout := s.layoutCols(cols)
	if !s.alignedFits(cols) {
		return "", false
	}
	live := s.Live
	if s.LiveWidth > 0 {
		live = padStatusColumn(live, s.LiveWidth)
	}
	fields := make([]string, 0, len(s.Columns))
	// Fields are emitted one column at a time: the column's styled text, then
	// the heavier column separator. Joining first and styling after would need
	// the boundaries again (see the note above).
	rendered := make([]string, 0, len(s.Columns))
	for _, c := range s.Columns {
		text := c.Text
		if text == "" {
			text = strings.Repeat(" ", max(c.Width, 0))
		}
		fields = append(fields, padStatusColumn(text, c.Width))
	}
	// The plain row's own text, recomposed from the same fields, is the check
	// that this rendering sees the row the fit decided (never a wider one).
	left := strings.Join(fields, StatusColumnSep)
	pad := layout - displayWidth(left) - displayWidth(live)
	if pad < displayWidth(StatusLineGap) {
		return "", false
	}
	if displayWidth(left)+displayWidth(live)+displayWidth(StatusLineGap) > cols {
		return "", false
	}
	plainRow := left + strings.Repeat(" ", pad) + strings.TrimRight(live, " ")
	if plainRow != plain {
		return "", false
	}
	for i, c := range s.Columns {
		if i > 0 {
			// The column separator is emitted UNWRAPPED, exactly as the plain
			// row's strings.Join emits it: the separator is the row's chrome,
			// not a field, and it carries no emphasis of its own (the styled
			// path has always left it unstyled — styledFields splits the label
			// on the separator and joins it back outside the wrapped fields).
			rendered = append(rendered, StatusColumnSep)
		}
		rendered = append(rendered, styledStatusField(fields[i], c, pal))
	}
	liveStyle := pal.StatusLive
	if s.Thinking {
		liveStyle = pal.Thinking
	}
	return strings.Join(rendered, "") +
		strings.Repeat(" ", pad) +
		liveStyle.Wrap(strings.TrimRight(live, " ")), true
}

// styledStatusField renders ONE column's padded text in the emphasis its
// identity calls for, with any measured-quality span inside it RE-STATED rather
// than spliced.
//
// The closing reset is REQUIRED here, not decorative: the field's colour must
// not leak into the row's plain fill (the padding) or into the live hint, so
// every path that opens the field style also closes it.
func styledStatusField(text string, c StatusColumn, pal Palette) string {
	field := pal.StatusLabel
	if isToolRow(text) {
		field = pal.ToolRow
	}
	return wrapStatusField(text, c, field)
}

// styledFieldsEmphasis is styledFields — the field-by-field identity styling of
// the UNALIGNED label — with the producer's measured-quality span re-stated
// inside whichever field carries it.
//
// It exists so ONE implementation serves both paths: the aligned row styles its
// columns (whose boundaries are the producer's own list) and the unaligned row
// styles its StatusSep-separated fields, and both re-state the span through
// wrapStatusField. Two implementations of the re-statement would be two places
// for the nested-reset defect to come back.
func styledFieldsEmphasis(label string, c StatusColumn, pal Palette) string {
	if label == "" || (pal.StatusLabel.Empty() && pal.ToolRow.Empty()) {
		return label
	}
	fields := strings.Split(label, StatusSep)
	for i, f := range fields {
		field := pal.StatusLabel
		if isToolRow(f) {
			field = pal.ToolRow
		}
		fields[i] = wrapStatusField(f, c, field)
	}
	return strings.Join(fields, StatusSep)
}

// wrapStatusField wraps one field's WHOLE text in the field's own style, with the
// producer's span re-stated inside it. An empty field style (a table that names
// none) still styles the span: the span's emphasis is a fact about the
// measurement, and the style it wears was resolved against the table by the
// producer — dropping it because the ENCLOSING field happens to be unstyled would
// be a silent non-effect (Rule 15).
func wrapStatusField(text string, c StatusColumn, field Style) string {
	spans := c.emphasisSpans()
	if field.Empty() {
		if len(spans) == 0 {
			return text
		}
		return statusFieldEmphasis(text, c, Style{})
	}
	if len(spans) == 0 {
		return field.Wrap(text)
	}
	return field.Open() + statusFieldEmphasis(text, c, field) + field.Close()
}

// statusFieldEmphasis renders text with every occurrence of the column's
// measured-quality span wrapped in the span's own emphasis, and the ENCLOSING
// field style re-opened after each one.
//
// THE RE-STATEMENT IS THE WHOLE POINT. Wrapping the span emits a close
// (sgrReset at the end of the span's Wrap), and a reset ends the field's colour
// too — so without re-opening the field style every character after the span
// would print in the terminal's default colour. Re-opening through Open()/Close()
// is not "splicing a span into a styled string" (the defect this replaces): the
// field style here covers the WHOLE field, and the span is a nested,
// self-closing run inside it, which is what SGR supports. Close() emits the
// reset only when the field style actually opened one, so a style whose Open is
// empty stays byte-free.
//
// The span is FOUND in the text (the producer named the exact substring it
// composed). A span that is not in this field is left alone: guessing at a
// near-match would emphasise characters the producer never measured, and the
// honest no-op is what keeps the producer and the renderer from disagreeing.
func statusFieldEmphasis(text string, c StatusColumn, field Style) string {
	spans := c.emphasisSpans()
	if len(spans) == 0 {
		return text
	}
	// The spans are consumed LEFT TO RIGHT and each one at most ONCE: the search
	// for the next span starts after the previous span's own cells, and a span
	// whose text a previous one already consumed is skipped. That is the old
	// single-span rule — only the FIRST occurrence is emphasised, because a
	// repeated substring is a different fact that happens to print the same
	// characters — lifted to a list without becoming a different rule.
	out := ""
	rest := text
	used := make(map[string]bool, len(spans))
	for _, sp := range spans {
		if sp.Text == "" || sp.Style.Empty() || used[sp.Text] {
			continue
		}
		i := strings.Index(rest, sp.Text)
		if i < 0 {
			// The producer named a span this field does not carry: left alone, so
			// the honest no-op stands for this span while the others still draw.
			continue
		}
		used[sp.Text] = true
		out += rest[:i] + sp.Style.Wrap(sp.Text)
		rest = rest[i+len(sp.Text):]
		if rest != "" {
			// The span's Wrap closed with a reset, which also ended the FIELD's
			// style: RE-STATE the field for the cells that follow, or every one of
			// them would print in the terminal's default colour.
			out += field.Open()
		}
	}
	// The field's closing reset is emitted by the CALLER, which is what opened the
	// field: this helper only ever re-opens what the caller opened.
	return out + rest
}

// splitStatusRow recovers the label/live split statusLine applied, so the styled
// row emphasises the same two parts the plain row composes. The live hint is
// whatever the plain row ended with (it is the part that must survive a narrow
// terminal, and statusLine keeps it verbatim); everything before it is the
// label. When there is no live text the whole row is the label.
func splitStatusRow(plain string, s Status, cols int) (label, live string) {
	if s.Live == "" || cols <= 0 {
		return plain, ""
	}
	fitted := fitField(s.Live, cols)
	if fitted == "" || !strings.HasSuffix(plain, fitted) || plain == fitted {
		return plain, ""
	}
	return strings.TrimSuffix(plain, fitted), fitted
}

// fitStatusFields keeps the leading whole fields of label that fit within room
// cells (fields are separated by the status line's own separator, so the split
// and the join use one definition of it). When not even the first field fits,
// that field is cut with an ellipsis — there is nothing smaller to show, and a
// cut WITHOUT the marker would read as a corrupted value.
func fitStatusFields(label string, room int) string {
	if displayWidth(label) <= room {
		return label
	}
	fields := strings.Split(label, StatusSep)
	if len(fields) <= 1 {
		return fitField(label, room)
	}
	kept := fields[:0:0]
	used := 0
	for _, f := range fields {
		w := displayWidth(f)
		if len(kept) > 0 {
			w += displayWidth(StatusSep)
		}
		if used+w > room {
			break
		}
		kept = append(kept, f)
		used += w
	}
	if len(kept) == 0 {
		return fitField(fields[0], room)
	}
	return strings.Join(kept, StatusSep)
}

// cutField renders s within at most room display cells, ending it with the
// ellipsis marker EXACTLY when it had to be cut. It is the ONE definition of
// the reserved-marker arithmetic (Rule 14): the marker's own cell is taken out
// of the budget BEFORE the cut, so a marked field is bounded by room — never
// room+1 — which is what lets the status row and the turn header each stay one
// screen row. A field that fits is returned untouched: the marker means "there
// is more", and a complete value must never wear it.
//
// The caller keeps its own view of a budget with no room in it: this helper
// renders s whenever it fits room, and a non-positive room that cannot hold s
// degrades to the marker alone (the identity of what was cut is gone, the fact
// that something was is not).
func cutField(s string, room int) string {
	if displayWidth(s) <= room {
		return s
	}
	return cutFieldHead(s, room) + statusEllipsis
}

// cutFieldHead is the text cutField leaves before its marker: the head of s that
// fits room MINUS the marker's own cell. It exists because the caller that must
// know WHERE the cut happened (the turn header's multi-line block, which
// continues the label on the next row) may not re-derive the arithmetic — a
// second derivation is how a tail starts one character away from the row above it
// (Rule 14: cutField's own cut, referenced).
func cutFieldHead(s string, room int) string {
	return truncateCols(s, room-displayWidth(statusEllipsis))
}

// fitField renders s within at most room display cells, ending it with the
// ellipsis marker when it had to be cut. The result is bounded by room — that
// bound is what keeps the status row one screen row — and the marker is
// reserved for BEFORE the cut (cutField), so marking never pushes the field one
// cell over. A field with no room at all is not shown at all: unlike the turn
// header, the status row has nothing honest to draw in zero cells.
func fitField(s string, room int) string {
	if room <= 0 {
		return ""
	}
	return cutField(s, room)
}

// write sends one chunk to the terminal and counts it as a repaint. The counter
// is the stream's cost, which is what the bounded-repaint assertion bounds.
func (r *AnswerRenderer) write(s string) {
	if s == "" {
		return
	}
	r.writes.Add(1)
	if r.out.outFn == nil {
		return
	}
	// deliberately ignored: a failed repaint is not state — the terminal is
	// restored on the exit path regardless (Rule 15 states the discard rather
	// than hiding it).
	_, _ = r.out.out().Write([]byte(s))
}

// placeAndWrite paints text at an ABSOLUTE cell as ONE unit of emission.
//
// This is not tidiness: the answer renderer SHARES its byte stream with the
// editor's read loop, which repaints the editing line (and the status row)
// between any two write(2) calls. A placement and the text it positions are
// therefore one indivisible unit or the read loop can land BETWEEN them — and
// the text then goes to whatever row that repaint left the cursor on.
//
// Measured on the paint stream (2026-09-27), the exact interleaving:
//
//	CSI 10;13H                                            <- place at row 9, col 12
//	CSI 11;1H CSI 2K CSI 1m CSI 32m "you>" CSI 0m CR CSI 5C   <- read loop repaints the editing line
//	CSI 1m "beta" CSI 0m                                  <- the span's TEXT, on the editing line's row
//
// So the frozen artifact held `▌ alpha     beta and gamma`: its ATTRS spelled
// the COMPLETE line (`0:2=default 2:5=bold 7:5=default 12:4=bold …` — the
// emphasis map the placement had already written) while its TEXT missed the
// 5-cell run at cols 7-11, which was never painted at all; `beta` landed on the
// editing line and the read loop's repaint erased it. That is the "text and
// attrs disagree" row the dump gate timed out on.
//
// A row that fills the live row was NOT the only hole: every `place` +
// `write` pair in this file had the same window (the gutter, the status row,
// the live-row clear).
//
// The region's own invariant is why the placement is composed into the SAME
// buffer rather than written ahead of it (placeInto's doc): a placement must
// not jump the queue, because writing the cursor directly while buffering the
// text would put the cursor in the wrong cell.
func (r *AnswerRenderer) placeAndWrite(row, col int, text string) {
	r.placeAndWritePrefixed("", row, col, text)
}

// placeAndWritePrefixed is placeAndWrite with a leading control sequence — the
// erase a repaint needs — riding the SAME write, so the unit repaints in one
// atomic step whatever the read loop does in between. The order inside the
// write is PLACE, prefix, text: the cursor sequence must land before the erase,
// or the erase clears whatever row the cursor happened to be on.
func (r *AnswerRenderer) placeAndWritePrefixed(prefix string, row, col int, text string) {
	if prefix == "" && text == "" {
		return
	}
	if r.out.outFn == nil {
		return
	}
	if !r.out.Active() {
		// No region: the historic full-screen behaviour. There is no absolute
		// cell to name — the text rides the current output position — so the
		// prefix still leads it inside the same write.
		r.write(prefix + text)
		return
	}
	var b strings.Builder
	// PLACE first, then the erase, then the text: the cursor sequence must land
	// before the erase, or the erase clears whatever row the cursor happened to
	// be on instead of this one (the exact bytes the pre-fix pair produced, now
	// in ONE write).
	r.out.placeInto(&b, row, col)
	b.WriteString(prefix)
	b.WriteString(text)
	r.write(b.String())
}

// placementSeqLen returns the byte length of a leading absolute cursor
// placement (`CSI row ; col H`), or 0 when the chunk does not start with one.
// It is the ONE definition of "this chunk exists only to position the next
// one" — the property the answer renderer's writes must never have, because the
// editor's read loop may write between any two of them.
//
// It is derived from sgr's own byte vocabulary, never spelled: a second copy of
// the sequence's grammar is how a parser and a composer drift apart.
func placementSeqLen(p []byte) int {
	if len(p) < len(sgr.CSI)+numPlacementParams+1 || !strings.HasPrefix(string(p), sgr.CSI) {
		return 0
	}
	i := len(sgr.CSI)
	for params := 0; params < numPlacementParams; params++ {
		if params > 0 {
			if i >= len(p) || p[i] != sgr.ParamSeparator {
				return 0
			}
			i++
		}
		digits := 0
		for i < len(p) && p[i] >= sgr.ParamDigitMin && p[i] <= sgr.ParamDigitMax {
			i++
			digits++
		}
		if digits == 0 {
			return 0
		}
	}
	if i >= len(p) || (p[i] != sgr.FinalCUP && p[i] != sgr.FinalCUPAlt) {
		return 0
	}
	return i + 1
}

// placementSeqRow decodes the 0-based row a leading placement names.
func placementSeqRow(p []byte) (int, bool) {
	n := placementSeqLen(p)
	if n == 0 {
		return 0, false
	}
	body := string(p[len(sgr.CSI) : n-1])
	row, _, ok := strings.Cut(body, string(sgr.ParamSeparator))
	if !ok {
		return 0, false
	}
	// The wire numbers rows from 1 (sgr.CursorSeq); the caller reads screen rows.
	v, err := strconv.Atoi(row)
	if err != nil || v < 1 {
		return 0, false
	}
	return v - 1, true
}

// numPlacementParams is how many parameters an absolute placement carries
// (row, col). A `CSI H` with both defaulted is home — also a placement, but one
// this project composes through sgr.CursorHomeSeq, so it never arrives as a
// bare two-parameter form from the answer renderer.
const numPlacementParams = 2

// TruncateToCols cuts s to at most width display cells, with no marker. It is
// truncateCols' exported form, for a PRODUCER that must compose a field inside a
// budget it already knows (cmd/agent's live block: the elapsed prefix is cut to
// the cells the state word leaves, because the tail cut that would otherwise
// apply shears the STATE off the block instead — see chatStatusSource.status).
//
// The distinction from fitField matters and is not cosmetic: fitField reserves
// the ellipsis marker BEFORE the cut, which is right for a field the reader must
// understand as truncated, and wrong for a prefix whose own abbreviation is
// self-evident ("2m…" is a time) — and whose marker would spend the cell it was
// reserving to signal.
func TruncateToCols(s string, width int) string { return truncateCols(s, width) }

// truncateCols cuts s to at most width display cells (a wide rune that would
// straddle the edge is dropped rather than half-drawn). A control sequence
// draws no cell, so it is kept whole and costs no width (Rule 14 - the same
// escape-aware measure displayWidth and cutCols use).
func truncateCols(s string, width int) string {
	if width <= 0 {
		return ""
	}
	var (
		b   strings.Builder
		col int
	)
	for i := 0; i < len(s); {
		if n := sgr.EscapeSeqLen(s[i:]); n > 0 {
			b.WriteString(s[i : i+n])
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		w := runeCells(r)
		if col+w > width {
			return b.String()
		}
		b.WriteString(s[i : i+size])
		col += w
		i += size
	}
	return b.String()
}

// RuneSafeCut reports how many bytes at the end of s must be held back so that
// s can be split without cutting a multi-byte rune in half. It exists because a
// delta boundary and a rune boundary are independent: the renderer writes bytes,
// and half a rune written now would render as a replacement character.
func RuneSafeCut(s string) int {
	if s == "" {
		return 0
	}
	// Walk back over continuation bytes to the start of the last rune.
	for i := len(s) - 1; i >= 0 && i > len(s)-utf8.UTFMax; i-- {
		if utf8.RuneStart(s[i]) {
			if utf8.FullRune([]byte(s[i:])) {
				return 0
			}
			return len(s) - i
		}
	}
	return 0
}

// cutCols splits s at the last rune boundary that fits within width display
// cells and returns the fitting head plus the untouched tail. It is the same
// break rule as wrapCols, except that a single rune WIDER than the whole width
// is emitted in the head rather than looped on forever (dropping it would
// silently lose answer text).
//
// ESCAPE-AWARE: a terminal control sequence draws no cell, so it rides the row
// whole and is neither counted against the width nor cut in half. Both halves of
// that were measured: counting `\x1b[2m` as four cells shortened every row
// after it, and cutting after `\x1b[` printed the sequence's own parameters
// as text (the band's next row read literally `31mbbbb...`).
func cutCols(s string, width int) (head, tail string) {
	if width <= 0 {
		return "", s
	}
	var b strings.Builder
	col := 0
	i := 0
	for i < len(s) {
		if n := sgr.EscapeSeqLen(s[i:]); n > 0 {
			b.WriteString(s[i : i+n])
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		w := runeCells(r)
		if col+w > width {
			if b.Len() == 0 {
				// The first rune alone overflows the width: emit it and move on.
				return s[:i+size], s[i+size:]
			}
			return s[:i], s[i:]
		}
		b.WriteString(s[i : i+size])
		col += w
		i += size
	}
	return s, ""
}

// WrapToCols splits s into rows of at most width DISPLAY columns each — the
// break rule the reserved answer band applies to its own live row, exported so
// a caller that streams to a terminal WITHOUT a band wraps to the same policy
// (Rule 14: one wrap policy, one definition; a second implementation of "where
// does a row end" would drift from this one the moment either side changes).
// A wide rune is never split across the boundary: one that does not fit ends
// the row instead of being half-drawn, so a CJK answer stays readable. A row
// ends at a WORD boundary before it ends at a cell boundary (see wrapCols): the
// grid is what the operator copies, so a token is kept whole.
// width <= 0 means "no wrapping": s comes back unchanged, as ONE row.
func WrapToCols(s string, width int) []string {
	return wrapCols(s, width)
}

// wrapCols splits a row of text into as many display rows of at most width
// cells as it needs, breaking BETWEEN RUNES (a wide rune that would straddle
// the edge starts the next row rather than being half-drawn) and AT A WORD
// BOUNDARY wherever the source offers one. A single over-wide rune (wider than
// the terminal) is emitted alone: dropping it would silently lose answer text.
//
// WHY THE WORD BOUNDARY DECIDES. A row of the answer is a COPYING surface — the
// operator reads it and copies a token out of it — so a break inside a word
// hands back something that does not run: `COMMANDS.md` ending a row and `md`
// starting the next is a defect of the deliverable, not a cosmetic choice. The
// row therefore ends at the LAST boundary before the edge (wrapBoundaryBefore):
// after whitespace, after one of wrapSoftBreakPunct, or between two wide runs
// (CJK text breaks between characters — it does not separate words with
// spaces). The break character stays on the row it was written on.
//
// A CELL-BOUNDARY BREAK IS STILL THE FALLBACK, for the one case that leaves no
// choice: a single token longer than a whole row. It cannot be moved to a wider
// row, because there is none.
//
// The invariant both breaks keep: the rows reassemble to s exactly — every byte
// once, in order, the only thing added being the row breaks.
func wrapCols(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	if s == "" {
		return []string{""}
	}
	var out []string
	from := 0
	for from < len(s) {
		// Measure one row, starting at `from`: i walks the source, col counts
		// its cells, last remembers the latest boundary the source offered.
		i, col, last := from, 0, -1
		// esc: the row carries an SGR sequence, so it may not break at a
		// boundary — a boundary break re-reads the text past it on the next row,
		// and a sequence's effect would then start on a row it was not written on.
		esc := false
		cut := -1
		for i < len(s) {
			if n := sgr.EscapeSeqLen(s[i:]); n > 0 {
				esc = true
				i += n
				continue
			}
			r, size := utf8.DecodeRuneInString(s[i:])
			w := runeCells(r)
			if col+w > width {
				if i == from {
					// A single rune wider than the whole row: it goes alone.
					i += size
				}
				cut = i
				break
			}
			col += w
			i += size
			if wrapBreakAfter(r, s, i) {
				last = i
			}
		}
		switch {
		case i >= len(s):
			out = append(out, s[from:])
			return out
		case !esc && last > from:
			// End at the boundary — UNLESS that boundary falls INSIDE a token,
			// in which case the token moves down whole instead
			// (wrap_token_boundary.go: the boundary arm consults the whole-token
			// question, and the token has the final word). Then re-read the text
			// past it as the next row's own (its own boundaries are decided from
			// its own start).
			last = wrapBoundaryWholeTokenStart(s, from, last, width)
			out = append(out, s[from:last])
			from = last
		default:
			// No boundary offered in front of the edge, so the row ends by the
			// CELL cut — unless the cut would HALVE a token that fits a whole
			// row: that token moves down whole instead (wrap_whole_word_move.go,
			// the fourth disposition; a token wider than a row still keeps the
			// cell cut, because there is no wider row to move it to).
			cut = wrapWholeWordShift(s, from, cut, width)
			out = append(out, s[from:cut])
			from = cut
		}
	}
	return out
}

// wrapSoftBreakPunct are the narrow characters a row may end AFTER: the
// separators of a path, a flag or a shell word (`/ - _ ? & = , ; ) ]`). The
// character stays on the row it was written on, so a line read alone still
// carries the punctuation that ended it. `.` is deliberately NOT in this set:
// it is the character that holds `COMMANDS.md` and `v1.2.3` together, which is
// exactly the token this policy exists to keep whole.
const wrapSoftBreakPunct = "/-_?&=,;)]"

// wrapBreakAfter reports whether s[:end] ends at a boundary a row may end at,
// given that the rune just consumed was r.
//
// Three boundaries, one of them a PAIR: whitespace after a narrow rune, one of
// wrapSoftBreakPunct after a narrow rune, and the junction of a wide rune with
// a narrow one IN EITHER DIRECTION. That last one is one statement, not two: the
// source changes script at the junction, so no token straddles it — CJK text
// separates its words with no spaces, so the wide↔wide junction is the only
// boundary CJK text has, and a Latin/digit token written INSIDE a CJK run
// (`319 KB`, `regenerable artefact`) is narrow on both its edges. Recognising
// only wide↔wide left `…说明文字|regenerableartefact` with no boundary at all,
// so the row fell through to the CELL cut and halved the very token the policy
// exists to keep copyable.
//
// A rune followed by whitespace is NOT a boundary in the wide half: the space
// belongs to the row that ends after it (the first branch), so breaking in front
// of it would only start the next row with a space it did not need.
func wrapBreakAfter(r rune, s string, end int) bool {
	if end >= len(s) {
		// The end of the text is the end of the row: nothing follows to keep
		// whole, and a boundary there would only split the row early.
		return false
	}
	if r == ' ' || r == '\t' {
		return true
	}
	if !isWideRune(r) && strings.ContainsRune(wrapSoftBreakPunct, r) {
		return true
	}
	nr, _ := utf8.DecodeRuneInString(s[end:])
	if nr == ' ' || nr == '\t' {
		return false
	}
	return isWideRune(r) || isWideRune(nr)
}

// wrapBoundaryBreak ends a row at the boundary recorded at byte offset `last`
// and returns the row plus the tail that moves to the next one.
//
// The tail is the part of the row's collected text that lies PAST the boundary.
// It is put back verbatim (the collected text is the source's own bytes, minus
// the sequences this row refuses to consider — `esc` is false, so it is the
// source), so this function decides WHERE a row ends and never WHAT it carries:
// cutPos is the collected-prefix length at the boundary and cutCol its cell
// count, which is exactly the two numbers needed to slice the tail back out.
func wrapBoundaryBreak(s string, last, i, cutPos, cutCol, have int) (row, rest string) {
	if cutPos <= 0 || cutPos >= have {
		// No collected prefix to split (the boundary IS the row's end): the row
		// is every byte read so far and nothing moves.
		return s[:i], ""
	}
	return s[:last], s[last:i]
}

// ── turn structure ───────────────────────────────────────────────────────────
//
// A band of committed answer lines carries no record of WHICH turn each line
// belongs to. Two consecutive turns therefore read as one continuous answer —
// the measured defect: turn 1's last line ("Done.") sits directly above turn 2's
// first paragraph, and the only hint that they are different turns is the status
// row's turn counter. The functions below close that with three pieces of
// chrome, all of them drawn through the SAME live-row/commit path the answer
// uses (so they inherit the scrollback-safety invariant instead of adding a
// second writer):
//
//  1. a HEADER line at the start of each turn — `── turn N · <prompt> ───…` —
//     which names the turn and repeats what was asked;
//  2. a MARKER column on every committed line of the turn (`▌ ` / `▌ `, equal
//     width) so the turn's body is visibly one block, aligned down its left
//     edge;
//  3. a closing RULE line between turns (the same glyph run, without a label)
//     written at the END of a turn's stream, so the next turn's header never
//     lands flush against the previous answer.

// TurnHeader renders the header line for one turn: a lead rule, the turn's
// label (`turn N · <prompt>`), and a closing rule that fills the remaining
// width. It is TOTAL in cols — the returned string never occupies more than cols
// display cells, so the header cannot wrap and steal a row from the band (the
// same one-screen-row discipline the status row follows) — and it degrades in
// two honest steps on a narrow terminal instead of rendering a stub:
//
//   - below turnLabelHalfCells the header keeps ONE lead rule and the label
//     (two rule stubs and a label do not fit);
//   - below turnHeaderMinCells the rule is dropped entirely and the label stands
//     alone — still a separator, with no glyph that could read as damage.
//
// EVERY cut is marked: when the label does not fit its budget the header ends
// the label with the status row's own ellipsis marker (statusEllipsis, through
// the shared cutField), so a cut prompt reads as continuing rather than as
// silently truncated data. The marker is reserved OUT of the budget before the
// cut, never appended past cols — that is what keeps the total-in-cols claim
// true on the marked path too. A header wide enough for the whole label carries
// no marker at all: the marker means "there is more", and it must not appear on
// a prompt the header in fact shows in full.
func TurnHeader(info TurnInfo, cols int) string {
	if info.Spill.Spilled {
		// THE SPILL FORM: a spilled turn gets its own header, reached before any
		// of the ordinary path's decisions are taken. Keeping it OUT of the body
		// below is the no-regression half of this change — an ordinary header's
		// bytes are exactly what they were — while the shape a reader sees is
		// unmistakably different, which is the point: the operator's one-line
		// prompt is not a file path, and a header that reprinted the reference as
		// if it were the prompt told the reader the opposite.
		return spillHeader(info, cols)
	}
	return turnHeaderLine(turnLabelWithin(info, turnHeaderLabelRoom(cols)), cols)
}

// turnLabelWithin composes the turn's label so that the PROMPT yields first when
// the band is narrow — never the moment.
//
// WHY THIS IS NOT cutField ON THE WHOLE LABEL. The label is
// `turn N · <moment> · <prompt>`, and cutting it with one budget truncates
// whichever field happens to straddle the cut. On a 40-column band that is the
// MOMENT (`turn 1 · 2026-11-08 09:14…`), which is the one field this whole change
// exists to state: a truncated clock is not a smaller version of the answer, it
// is a different (and false) one. So the fixed part is composed first, and the
// prompt — the part a reader can recover from the answer below, or from the
// record — is what gets the remaining cells, with the marker.
//
// room<=0 leaves the label whole: the narrow degradations downstream (bare label,
// bare rule) already decide what fits, and they are the code that owns those
// widths.
func turnLabelWithin(info TurnInfo, room int) string {
	label := turnLabel(info)
	if room <= 0 || displayWidth(label) <= room {
		return label
	}
	if !info.Stamp {
		// No moment to protect: the pre-existing behaviour is byte-identical and
		// stays that way (cutField applies to the composed label downstream).
		return label
	}
	fixed := turnLabelTurn + strconv.Itoa(info.Index) + sessionevent.TurnStampSep +
		sessionevent.FormatTurnStamp(info.OpenedAt)
	if displayWidth(fixed) > room {
		// THE ORDINAL AND THE MOMENT TOGETHER DO NOT FIT. Cutting them would
		// truncate the CLOCK — a false statement, not a shorter one (a reader
		// cannot tell 09:14 from 09:41 in "09:14…"). The moment is the field this
		// header exists to state, so it takes the room whole and the ORDINAL
		// yields: the turn number is available from the status row and from the
		// record, while a mangled clock is available from nowhere. Precedent: the
		// spill header's own narrow ladder drops its identity field for exactly
		// this reason.
		//
		// When even the moment alone does not fit, NO moment is stated: a clock cut
		// to `2026-1…` reads as a real time and is not one (Rule 15 — an absence is
		// the honest answer, and the shared placeholder is what the formatter
		// already uses for it). The label then degrades to what fits.
		if atomic := sessionevent.FormatTurnStamp(info.OpenedAt); displayWidth(atomic) <= room {
			return atomic
		}
		if p := strings.TrimSpace(info.Prompt); p != "" {
			budget := room - displayWidth(turnLabelTurn) - displayWidth(strconv.Itoa(info.Index)) - displayWidth(StatusSep)
			if budget > 0 {
				return turnLabelTurn + strconv.Itoa(info.Index) + StatusSep + cutField(p, budget)
			}
		}
		return turnLabelTurn + strconv.Itoa(info.Index)
	}
	p := strings.TrimSpace(info.Prompt)
	if p == "" {
		return cutField(fixed, room)
	}
	budget := room - displayWidth(fixed) - displayWidth(StatusSep)
	if budget <= 0 {
		// Not even a prompt cell survives beside the ordinal and the moment: the
		// moment still wins, and the prompt is dropped rather than stated as a
		// dangling separator.
		return cutField(fixed, room)
	}
	return fixed + StatusSep + cutField(p, budget)
}

// turnHeaderLine composes the FIRST header row from an already-composed label:
// the lead rule, the label (cut to its budget, marked when it had to be), and
// the closing rule that fills the rest of the row.
//
// It is the one row every degradation shares, and it is what TurnHeader renders:
// a single-row header is exactly this row with no continuation above it, so the
// un-numbered/bare-label/narrow spellings cannot drift between the two entry
// points (Rule 14 — one composition, referenced).
func turnHeaderLine(label string, cols int) string {
	if cols <= 0 {
		// An unknown or zero width draws nothing, and no prompt is cut to
		// make it fit: the label's own cells would be an invisible overflow,
		// which is exactly what total-in-cols forbids. (TurnRule answers the
		// same width the same way.)
		return ""
	}
	if cols < turnHeaderMinCells {
		return cutField(label, cols)
	}
	if cols < turnLabelHalfCells {
		body := cols - displayWidth(turnLabelLead)
		if body < turnRuleMinCells {
			// Not even the lead rule, its spacing and a rule-worth of label
			// fit together: the label stands alone (the degradation the doc
			// above names), marked if it had to be cut.
			return cutField(label, cols)
		}
		lead := turnRuleMinCells
		if room := body - displayWidth(label); room < lead {
			lead = room
		}
		if lead < turnLabelLeadCells {
			return cutField(label, cols)
		}
		return strings.Repeat(TurnRuleGlyph, lead) + turnLabelLead + cutField(label, body-lead)
	}
	lead := turnRuleMinCells
	label = cutField(label, turnHeaderLabelRoom(cols))
	if label == "" {
		// No label cell survives between the two rules at all: a header with
		// nothing between them is damage, so it degrades to the bare rule (the
		// same honest step as the narrow cases above). The budget is derived
		// from the constants, so this is a guard, not a path: lead + lead +
		// tail alone is inside every cols this branch can see.
		return strings.Repeat(TurnRuleGlyph, cols)
	}
	used := lead + displayWidth(turnLabelLead) + displayWidth(label) + displayWidth(turnLabelTail)
	if used > cols {
		return cutField(label, cols)
	}
	return strings.Repeat(TurnRuleGlyph, lead) + turnLabelLead + label +
		turnLabelTail + strings.Repeat(TurnRuleGlyph, cols-used)
}

// ── the turn header's SPILL FORM ────────────────────────────────────────────
//
// A turn whose input was a PASTE SPILL has no typed prompt to repeat: the input
// line carries a reference (`[paste N: /abs/path]`) and the bytes are on disk.
// The ordinary header would reprint that reference — a reader cannot tell such a
// turn from one whose prompt happens to be a path, and the `N` a reference
// carries is the count the editor had when it last rewrote it, which lags the
// file. The functions below give that turn a header of its own, whose fields are
// the FILE's: its true size, its own line count, and enough of its opening lines
// to recognise the paste by — beside the handle on the bytes.
//
// The form, in the order the band can afford it:
//
//	paste 7 · 65536 bytes · 65536 runes · 11 lines (+8 more lines) · $ git status · On branch master · [paste 65536: /…/paste-….
//
// The ordinary header is untouched by all of it: TurnHeader reaches this form
// only when the turn carries a spill, and every other path renders exactly the
// bytes it rendered before.

// TurnSpillLabelName is the head word every spill header opens with — the
// shape's NAME, so an operator scanning scrollback sees the same mark on every
// spilled turn, and the one thing that makes the form recognisable at a glance
// beside `──── turn N · <prompt>` (the whole point of the specialised form).
//
// It is the REFERENCE's own marker word rather than a second spelling of "paste"
// (Rule 14): the header names the form with the same word the input line's
// reference carries, so the two vocabularies cannot drift apart.
const TurnSpillLabelName = PasteRefKeyword

// The spill header's vocabulary. The mark words are named (Rule 1b) because the
// renderer writes them and the readers outside this package — the pty cases that
// assert on the painted grid — identify the form by them: a header is read by an
// operator scanning a transcript, and `65536 bytes · 65536 runes · 11 lines` is a
// shape a reader recognises, while three bare numbers are three numbers.
const (
	TurnSpillBytesMark = " bytes"
	TurnSpillRunesMark = " runes"
	TurnSpillLinesMark = " lines"
	// TurnSpillHeadMark labels the count of the payload's opening lines the
	// header actually DREW, so the reader sees how much of the paste this header
	// is quoting rather than inferring it from the row's length.
	TurnSpillHeadMark = "head"
	// turnSpillHeadAir is the single cell between a mark word and its number
	// (`head 3`), and between the identity word and the turn index (`paste 7`).
	turnSpillHeadAir = " "
)

// The elision marker's spelling, as named parts (Rule 1b): a header is a SUMMARY
// of a payload longer than itself, and the marker states in the open how many of
// the payload's lines it did not draw — never a silent omission (Rule 15).
const (
	turnSpillMoreSingular = 1
	turnSpillMoreOne      = " (+1 more line)"
	turnSpillMoreFormat   = " (+%d more lines)"
)

// turnSpillHeadMaxLines is how many of the payload's opening lines the header may
// draw, and turnSpillHeadMaxCells how many cells of each it is worth.
//
// They bound the DISPLAY of the head, not the read: ReadSpillFacts captures at
// the same line count and at its own byte bound, so "show a few lines" cannot
// come to mean "carry the payload".
const (
	turnSpillHeadMaxLines = 3
	turnSpillHeadMaxCells = 72
)

// The head line's own degradation ladder. A head line is kept only while the
// WHOLE composition (the size statement, the correctly counted marker and that
// line) still fits, so a narrower band shows FEWER lines and a BIGGER count —
// never a silently shorter string (spillSummaryFit).
const (
	// turnSpillHeadMinCells is the fewest cells a drawn head line may occupy.
	// Below it the line is a fragment rather than a line and the header stops
	// adding them: the size statement and the marker already say there was a
	// payload, and a two-cell stub of prose reads as damage.
	turnSpillHeadMinCells = 12
	// turnSpillHeadShrink is the step a head line's budget walks DOWN by while
	// the header looks for a width whose whole composition fits. It is a step,
	// not a threshold: the search is bounded below (turnSpillHeadMinCells) and
	// deterministic, which is what keeps the rendering reproducible.
	turnSpillHeadShrink = 4
)

// turnSpillHandleMinCells is the FLOOR reserved for the handle on the bytes: the
// fewest cells of the path the header will spend before it drops the description
// instead. A path cut below it names no file, so the header prefers the handle
// whole-bodied (cut to the row with the marker) over a description with no way to
// reach the payload.
//
// It is 20 cells because that is what it takes to name the directory that holds
// the spill and the file inside it (`/…/paste/paste-202…`), which is what a
// reader needs in order to open the payload by hand.
const turnSpillHandleMinCells = 20

// spillHeader renders the header of a turn whose input was a PASTE SPILL. It is
// the ordinary header's sibling: the same total-in-cols discipline, the same
// narrow-width ladder and the same cut marker (cutField), over a different BODY —
// a summary of the payload in place of the prompt.
//
// WHAT A READER GETS, in the order the band can afford it:
//
//   - the identity field (`paste 7`), which says "this turn was a paste" in the
//     place the ordinary header says `turn 7`;
//   - the size statement: bytes AND runes, so both readers are served and the
//     pair is checkable against the file;
//   - the payload's own line count and the marker stating how many of those
//     lines the header did not draw;
//   - up to turnSpillHeadMaxLines of the payload's opening lines, so the operator
//     recognises WHICH paste this turn was without opening the file;
//   - the handle on the bytes (the path, cut with the marker when the band is
//     short), because the payload is one open away and this is the only place the
//     operator sees where it landed.
//
// DEGRADATION is the ordinary ladder, unchanged: below turnLabelHalfCells the
// body is the content alone or behind one lead rule, and below
// turnHeaderMinCells it stands alone. Nothing here can produce a row wider than
// cols, and no field is cut without the marker.
func spillHeader(info TurnInfo, cols int) string {
	if cols <= 0 {
		return ""
	}
	content := spillContent(info.Spill, cols)
	if cols < turnHeaderMinCells || cols < turnLabelHalfCells {
		// THE NARROW LADDER, shared with the ordinary header. The body here is
		// the CONTENT — the identity field would spend the cells on "paste 7",
		// which the summary already implies, while what a narrow reader most
		// needs is the size of the thing that arrived.
		body := cols - displayWidth(turnLabelLead)
		if body < turnRuleMinCells {
			return cutField(content, cols)
		}
		lead := turnRuleMinCells
		if room := body - displayWidth(content); room < lead {
			lead = room
		}
		if lead < turnLabelLeadCells {
			return cutField(content, cols)
		}
		return strings.Repeat(TurnRuleGlyph, lead) + turnLabelLead + cutField(content, body-lead)
	}
	lead := turnRuleMinCells
	room := turnHeaderLabelRoom(cols)
	// THE IDENTITY FIELD IS FIRST, exactly where the ordinary header puts `turn
	// N`: it is what makes the form recognisable at a glance, and the summary then
	// takes what is left (`paste 3 · 65536 bytes · … · <path>`). A band with no
	// room for both keeps the SUMMARY — the strictly more informative of the two,
	// since it carries the size, the marked omission count and the handle on the
	// bytes — and a band that cannot hold the summary either falls back to the
	// identity alone, which still says a spill happened.
	id := cutField(spillLabel(info), room)
	body := spillContent(info.Spill, room-displayWidth(id)-displayWidth(StatusSep))
	if id == "" {
		body = spillContent(info.Spill, room)
	} else if body == "" {
		body = id
	} else {
		body = id + StatusSep + body
	}
	if body == "" {
		return strings.Repeat(TurnRuleGlyph, cols)
	}
	used := lead + displayWidth(turnLabelLead) + displayWidth(body) + displayWidth(turnLabelTail)
	if used > cols {
		return cutField(body, cols)
	}
	return strings.Repeat(TurnRuleGlyph, lead) + turnLabelLead + body +
		turnLabelTail + strings.Repeat(TurnRuleGlyph, cols-used)
}

// spillLabel is the spill header's identity field: `paste 7`. It is the SHORT
// form on purpose — the handle on the bytes is the summary's own last field
// (spillContent), and spelling the path twice put the same file name in two
// places while leaving no room to say what had been pasted.
func spillLabel(info TurnInfo) string {
	if info.Index <= 0 {
		// A caller that keeps no turn counter gets the form's name without a
		// number, never a `paste 0` a reader would take for a turn.
		return TurnSpillLabelName
	}
	return TurnSpillLabelName + turnSpillHeadAir + strconv.Itoa(info.Index)
}

// spillContent composes the spill header's body within `room` cells: the summary
// (what was pasted — the reason this form exists) and the handle on the bytes,
// each given what the other does not need.
//
// THE SPLIT IS DERIVED FROM THE PAYLOAD, never guessed: the description's floor is
// what it takes to state the size AND the marker for the lines that are not drawn,
// and the handle takes what is left (capped at its whole length) but never less
// than turnSpillHandleMinCells. When the band cannot afford both, the HANDLE alone
// is the body — a description with no file to open is worse than a path, because
// the payload is one open away and this header is the only place the operator sees
// where it went — and the handle is then cut with the marker rather than shortened
// silently.
func spillContent(spill TurnSpill, room int) string {
	if room <= 0 {
		return ""
	}
	handle := strings.TrimSpace(spill.Handle)
	if handle == "" {
		// Nothing to open the payload with: the summary is the whole body, and it
		// still states its size and what it does not show.
		return spill.spillSummaryFit(room)
	}
	sep := displayWidth(StatusSep)
	floor := displayWidth(spill.spillSizeStatement(0) + turnSpillMoreMarker(spill.Lines))
	handleRoom := room - sep - floor
	if want := displayWidth(handle); handleRoom > want {
		handleRoom = want
	}
	if handleRoom < turnSpillHandleMinCells {
		return cutField(handle, room)
	}
	summary := spill.spillSummaryFit(room - sep - handleRoom)
	if summary == "" {
		return cutField(handle, room)
	}
	return summary + StatusSep + cutField(handle, handleRoom)
}

// spillSizeStatement is the SIZE STATEMENT alone: the payload's bytes, its runes
// and its own line count — plus, when the header drew lines of it, how many.
//
// `shown` is passed in rather than taken from Head because the header may fit
// fewer lines than the caller captured: the count a reader sees must be the count
// the header DREW, and the difference is exactly what turnSpillMoreMarker states.
func (s TurnSpill) spillSizeStatement(shown int) string {
	fields := strconv.Itoa(s.Bytes) + TurnSpillBytesMark + StatusSep +
		strconv.Itoa(s.Runes) + TurnSpillRunesMark + StatusSep +
		strconv.Itoa(s.Lines) + TurnSpillLinesMark
	if shown > 0 {
		fields += StatusSep + TurnSpillHeadMark + turnSpillHeadAir + strconv.Itoa(shown)
	}
	return fields
}

// spillSummary is the ONE composer of the spill header's descriptive half: the
// size statement, the marker stating how many of the payload's lines are NOT
// drawn, and — in order — the payload's opening lines the band could afford. The
// fields are joined by the row's own separator.
//
// ONE composer, so the string a budget is measured against and the string the
// header draws cannot drift apart: a second composer beside the renderer is how a
// header and its own budget start disagreeing about what "the spill summary" is.
func (s TurnSpill) spillSummary(head []string) string {
	fields := append([]string{s.spillSizeStatement(len(head)) + turnSpillMoreMarker(s.Lines-len(head))}, head...)
	return strings.Join(fields, StatusSep)
}

// spillSummaryFit is the most a spill summary can say within `room` cells: the
// size statement, the elision marker that accounts for every line not drawn, and
// the largest number of the payload's opening lines whose WHOLE composition still
// fits — composed that way rather than truncated afterwards.
//
// THIS IS THE "NEVER SILENTLY OMIT" RULE, made arithmetic: the marker is part of
// every candidate, so a line is kept only when the header can also state what it
// left out. A band too narrow even for the complete statement gets that statement
// CUT (cutField, which appends the row's own marker): the summary degrades to a
// marked fragment rather than to a number with no unit.
func (s TurnSpill) spillSummaryFit(room int) string {
	if room <= 0 {
		return ""
	}
	complete := s.spillSummary(nil)
	if displayWidth(complete) > room {
		return cutField(s.spillSizeStatement(0), room)
	}
	best := complete
	head := make([]string, 0, turnSpillHeadMaxLines)
	for _, raw := range s.Head {
		if len(head) >= turnSpillHeadMaxLines {
			break
		}
		text := strings.TrimSpace(raw)
		if text == "" {
			continue
		}
		line := ""
		for lineRoom := turnSpillHeadMaxCells; lineRoom >= turnSpillHeadMinCells; lineRoom -= turnSpillHeadShrink {
			candidate := spillHeadLine(text, lineRoom)
			trial := append(append([]string{}, head...), candidate)
			if displayWidth(s.spillSummary(trial)) <= room {
				line = candidate
				break
			}
		}
		if line == "" {
			break
		}
		head = append(head, line)
		best = s.spillSummary(head)
	}
	return best
}

// spillHeadLine renders one head line inside the header: its cells, trimmed of
// surrounding whitespace, cut to the head's own budget with the row's own
// ellipsis marker when it did not fit (cutField — one glyph for a reader to
// learn, not two).
func spillHeadLine(line string, room int) string {
	return cutField(strings.TrimSpace(line), room)
}

// turnSpillMoreMarker states, in the open, how many of the payload's lines the
// header did NOT draw — the CLI convention this case asks for
// (`... (+1 more line)`). A summary that dropped lines without saying so would be
// exactly the silent omission the header may not commit (Rule 15).
//
// It carries a singular spelling because "1 more lines" is the kind of visible
// breakage a reader files a defect about. Zero lines dropped is the EMPTY string,
// not a marker: `(+0 more lines)` would claim an omission that did not happen.
func turnSpillMoreMarker(dropped int) string {
	if dropped <= 0 {
		return ""
	}
	if dropped == turnSpillMoreSingular {
		return turnSpillMoreOne
	}
	return fmt.Sprintf(turnSpillMoreFormat, dropped)
}

// ── the turn header as a MULTI-LINE BLOCK ───────────────────────────────────
//
// The header above is ONE row by construction: cutField shortens the label until
// it fits, and a long prompt therefore becomes a prefix with an ellipsis —
// measured on a real turn, a paragraph of input read as
// `── turn 1 · <first few words>… ───`, and a reader cannot tell how much of the
// question the band dropped. The block below is the larger answer to the same
// requirement: the prompt gets a LINE BUDGET instead of a single row, and what
// does not fit is stated as a COUNT (`... (+N more line(s))`) rather than implied
// by an ellipsis.
//
// Three properties are the whole design, and each is asserted by the package's
// own tests:
//
//  1. it degrades IN ORDER. The rule/label/rule row (`turnHeaderLine`) stays the
//     block's FIRST row, so the narrow-width chain (turnLabelHalfCells,
//     turnHeaderMinCells) is untouched; the block only decides how many rows the
//     prompt may CONTINUE onto. A block that would be one row IS TurnHeader's
//     own string, exactly.
//  2. every row is TOTAL in cols — the continuation rows are composed by
//     turnHeaderRowCut over a budget of (cols − the indent), and the count row
//     goes through cutField — so no header row can wrap and steal a band row
//     that BeginTurn's per-row finalize did not count.
//  3. the LAST row states what happened. The prompt ends the block, or the
//     block's last row is TurnHeaderMoreMarker's sentence counting the rows it
//     did not draw. A block whose last row is neither would be a header that
//     lost text silently, which is the defect this block exists to close.

// The block's own bounds. Every value is named (Rule 1) because each one is a
// screen-budget decision a reader must be able to find in one place.
const (
	// turnHeaderContinuationLines is how many extra rows a typed prompt's block
	// may spend beyond the first. A header is CHROME — it frames the answer, it
	// is not the answer — so the cap keeps a pathological prompt from pushing
	// the whole answer out of the band while still showing a paragraph's worth
	// of the question. Measured at 80 columns against turnHeaderBlockWord: the
	// typed fixture's block is 5 rows (1 + 3 + the count row) and the paste
	// fixture's is 6 (1 + 4 + the count row).
	turnHeaderContinuationLines = 3
	// turnHeaderPasteContinuationLines is the same budget for a prompt that
	// carries a spilled paste: the reference is `[paste <runes>: /abs/path]` and
	// its PATH is the operand of the reader's next action (open the file, or
	// copy it), so the paste leg gets the larger budget — a single cut row would
	// hide the very thing the turn is about.
	turnHeaderPasteContinuationLines = 4
	// turnHeaderContinuationLeadCells is how far a continuation row is indented
	// past the lead rule's own cells, so the block reads as one label rather than
	// as several headers stacked: the rule glyph appears on the FIRST row only,
	// and the rows under it start one word's width in.
	turnHeaderContinuationLeadCells = 2
	// turnHeaderBlockMinHiddenRows is how many continuation rows the HIDDEN
	// remainder must span before the block is worth composing at all: the
	// single-row header already ends in the cut marker when the label does not
	// fit, so a block that showed one row's worth more would spend band rows to
	// restate that marker. Two rows is the point where the count row tells a
	// reader something the marker did not.
	turnHeaderBlockMinHiddenRows = 2
	// turnHeaderMinContinuationCells is the fewest cells a continuation row must
	// be able to carry before the block uses one at all: below it the rows would
	// hold a word each and cost the band more than they tell the reader, so the
	// block declines and the single-row header (with its own degradation chain)
	// stands.
	turnHeaderMinContinuationCells = 8
)

// TurnHeaderMoreMarker is the fixed head of the block's last row when the prompt
// did not fit: the row spells
//
//	... (+N more line(s))
//
// with N the number of FURTHER screen rows the unseen remainder would take at
// this width (turnHeaderMoreRows). It is exported because readers outside this
// package — the pty cases that read the painted grid, and the callers that
// document the header's shape — identify the state by it (Rule 14: one spelling,
// referenced rather than re-typed).
//
// The spelling is deliberately not the cut vocabulary's ellipsis glyph: `…` is
// this package's "this ROW was shortened" mark, and a row carrying `…` as an
// ordinary character (a path, a hash, a typed ellipsis) is then not a mark a
// reader may read as one. The block therefore spells its state in WORDS, and the
// three dots belong to that sentence rather than being a second ellipsis.
const TurnHeaderMoreMarker = "... (+"

// The rest of the marker sentence: ` more line` + (s) + `)`.
const (
	turnHeaderMoreTail       = " more line"
	turnHeaderMorePluralTail = "s"
	turnHeaderMoreClose      = ")"
)

// TurnHeaderLines renders the turn header as a BLOCK of rows, each at most cols
// display cells: the header row (TurnHeader's own shape, first), then up to
// turnHeaderContinuationLines continuation rows carrying the rest of the prompt.
//
// The LAST row is always one of two honest states, never a silent cut:
//
//   - the whole prompt is on the block (no marker at all);
//   - the prompt had more to show, and the last row is TurnHeaderMoreMarker's
//     sentence, counting the rows that were NOT drawn.
//
// The returned slice is never empty when TurnHeader would draw something, and its
// first element is EXACTLY TurnHeader(info, cols) — a block that is one row and a
// single-row header are the same string, so no narrow-width or short-prompt
// spelling changes (Rule 14: one composition, two entry points).
func TurnHeaderLines(info TurnInfo, cols int) []string {
	first := TurnHeader(info, cols)
	if first == "" || cols < turnLabelHalfCells {
		// No width at all, or a width where the header is itself degrading (bare
		// label / half rule): the block does not spend rows it could not label.
		return []string{first}
	}
	tail := turnHeaderTail(info, cols)
	if tail == "" {
		// The label already fits the first row's label budget beside its rules:
		// nothing continues this header.
		return []string{first}
	}
	budget := turnHeaderContinuationLines
	if len(PasteRefPaths(turnLabel(info))) > 0 {
		// A SPILLED PASTE: the reference must be legible whole, so the paste leg
		// gets the larger budget.
		budget = turnHeaderPasteContinuationLines
	}
	rows, more := turnHeaderContinuationRows(tail, cols, budget)
	lines := append([]string{first}, rows...)
	if more > 0 {
		lines = append(lines, turnHeaderMoreRow(more, cols))
	}
	return lines
}

// turnHeaderLabelRoom is the cells the label may occupy on the FIRST header row:
// the row less the lead rule, its spacing, the closing rule and its spacing. It
// is the expression turnHeaderLine cuts the label with, read from ONE place
// (Rule 14) — the block below derives its tail from that same cut, and a second
// spelling of the arithmetic is how a tail starts a character away from the row
// above it.
func turnHeaderLabelRoom(cols int) int {
	return cols - 2*turnRuleMinCells - 2*displayWidth(turnLabelLead) - turnRuleMinCells
}

// turnHeaderTail is everything the FIRST header row could not carry, carried
// WHOLE (the break's own space consumed, the note block's convention): the cut
// point is read from cutField's own arithmetic through cutFieldHead, so the tail
// starts exactly one byte past the last character row 0 drew — no character is
// dropped and none is drawn twice.
func turnHeaderTail(info TurnInfo, cols int) string {
	budget := turnHeaderLabelRoom(cols)
	// THE SAME COMPOSITION THE DRAWN ROW USED (Rule 14): the tail is "what row 0
	// could not carry", so it must be cut from the label that was actually
	// composed for that budget. Reading `turnLabel` whole here would start the
	// continuation one field earlier than the row above ended, and a stamped
	// header would then draw its moment twice.
	label := turnLabelWithin(info, budget)

	if budget <= 0 {
		return ""
	}
	if displayWidth(label) <= budget {
		return ""
	}
	rest := noteTail(label, len(cutFieldHead(label, budget)))
	if turnHeaderMoreRows(rest, turnHeaderContinuationWidth(cols)) < turnHeaderBlockMinHiddenRows {
		// The hidden remainder is a row's worth or less, and the single-row header
		// already ends in the cut marker: composing a block would spend a band row
		// to show text one row deep, so the header stays the row it was (the
		// unchanged path every existing caller and assertion takes).
		return ""
	}
	return rest
}

// turnHeaderContinuationIndent is how many cells a continuation row spends before
// its text: the lead rule's cells plus the label's own spacing plus the extra
// indent, so the block's rows line up under the rule the first row opened with.
func turnHeaderContinuationIndent(cols int) int {
	indent := turnRuleMinCells + displayWidth(turnLabelLead) + turnHeaderContinuationLeadCells
	if indent > cols-turnHeaderMinContinuationCells {
		return cols - turnHeaderMinContinuationCells
	}
	return indent
}

// turnHeaderContinuationWidth is the cells of TEXT a continuation row carries:
// what is left of the row after the indent.
func turnHeaderContinuationWidth(cols int) int {
	return cols - turnHeaderContinuationIndent(cols)
}

// turnHeaderContinuationRows lays text out over at most budget rows of
// turnHeaderContinuationWidth(cols) text cells each, each row indented to the
// block's label column. It returns the rows it drew and how many FURTHER rows
// the leftover would take (0 when nothing was left over) — the N of the count
// row, so the count is measured with the same cutter that drew the rows and the
// two cannot disagree about what a row holds.
//
// text is cut with turnHeaderRowCut — the SAME boundaries the note block's
// cutter reads (an unquoted space that uses the row, then an unquoted
// separator), minus its marked word cut, because a header row may not replace
// characters with a marker — and every row is at most its width by construction,
// which is what keeps the drawn block and BeginTurn's per-row finalize ledger in
// agreement.
func turnHeaderContinuationRows(text string, cols, budget int) (rows []string, more int) {
	indent := turnHeaderContinuationIndent(cols)
	width := turnHeaderContinuationWidth(cols)
	if width < turnHeaderMinContinuationCells {
		// DEFENSIVE: the block only composes at cols ≥ turnLabelHalfCells, and the
		// indent is a fixed 7 cells, so this arm is unreachable from
		// TurnHeaderLines and exists so a future caller cannot silently overrun a
		// row. No rows are drawn and the count names the whole remainder — never a
		// silent cut.
		return nil, turnHeaderMoreRows(text, cols)
	}
	pad := strings.Repeat(" ", indent)
	for len(rows) < budget && text != "" {
		head, used := turnHeaderRowCut(text, width)
		next := noteTail(text, used)
		// A cut that could not advance (a single rune wider than the row) would
		// spin the loop and fill the budget with blank rows: the block ends here
		// instead and the count row names what is left.
		if head == "" && next == text {
			return rows, turnHeaderMoreRows(text, width)
		}
		rows = append(rows, pad+head)
		text = next
	}
	return rows, turnHeaderMoreRows(text, width)
}

// turnHeaderRowCut is the block's own row cutter: the text one continuation row
// carries on this width, and the offset the next row starts at.
//
// It is cutAtBoundary WITHOUT the marked word cut, and that difference is the
// point: a prompt's own characters may not be DROPPED. The note block may replace
// the middle of an unbreakable token with `…` because a call row is a summary of
// a command the transcript still holds, but the header's block IS the record of
// what was asked — and the character it dropped would be a character of the
// question, counted nowhere because a paste reference's path has no boundary a
// row can break at. So this cutter always returns a PREFIX of the text (lossless:
// head + the rest is the whole input) and prefers the break a reader expects — an
// unquoted space that uses the row, then an unquoted separator — falling back to
// the rune boundary the width itself gives.
func turnHeaderRowCut(text string, width int) (head string, rest int) {
	fit, tail := cutCols(text, width)
	if tail == "" {
		return text, len(text)
	}
	end := len(fit)
	if b := lastUnquoted(text[:end], end, noteBoundarySpace); b >= 0 && fillsNoteRow(text[:b], width) {
		// The break IS a space: it is consumed with the break (noteTail), so the
		// continuation does not inherit an indent the row never spent.
		return text[:b], b
	}
	if b := lastUnquoted(text[:end], end, cutNoteBoundaries); b >= 0 {
		// A separator is KEPT (a path's own `/` belongs to the text that
		// continues), so the row does not end on a token the reader must re-read.
		return text[:b+1], b + 1
	}
	return fit, end
}

// turnHeaderMoreRows is how many further continuation rows text would take — the N
// of the block's count row. It is measured with the SAME cutter the rows were,
// so the count and the drawn rows cannot disagree about what a row holds.
func turnHeaderMoreRows(text string, width int) int {
	if width < turnHeaderMinContinuationCells {
		width = turnHeaderMinContinuationCells
	}
	n := 0
	for text != "" {
		head, used := turnHeaderRowCut(text, width)
		next := noteTail(text, used)
		if head == "" && next == text {
			// A single rune wider than the row: it does take a row of its own,
			// and counting it is the honest answer (it IS text the reader is not
			// being shown).
			return n + 1
		}
		n++
		text = next
	}
	return n
}

// turnHeaderMoreRow spells the block's last row for n unseen rows at width cols:
// TurnHeaderMoreMarker + the count + the singular/plural tail.
//
// A spilled paste gets no row of its OWN here: its reference is carried by the
// continuation rows, and the paste leg's larger budget
// (turnHeaderPasteContinuationLines) is what keeps the reference — size and
// absolute path together — legible ABOVE this count, so the row stays one fact
// (how much was not drawn) rather than two.
//
// The row degrades in WORDS when the width is tight — the closing `)`, then the
// plural `s`, then the tail ` more line` — because the COUNT is the one fact this
// row exists to state: a narrow row still reads `... (+N`. Only a width that
// cannot hold the marker's own head plus the digits falls back to cutField, the
// package's MARKED cut, so even then nothing is dropped silently.
func turnHeaderMoreRow(n int, cols int) string {
	head := TurnHeaderMoreMarker + strconv.Itoa(n)
	tail := turnHeaderMoreTail
	if n != 1 {
		tail += turnHeaderMorePluralTail
	}
	for _, line := range []string{head + tail + turnHeaderMoreClose, head + tail, head} {
		if displayWidth(line) <= cols {
			return line
		}
	}
	return cutField(head, cols)
}

// TurnRule renders the unlabelled rule that ends a turn: a full width of the
// rule glyph, bounded by cols. It is what makes the boundary between two turns
// visible even when the next turn has not started yet (the status row returns to
// "ready" under a closed turn rather than under a naked answer).
func TurnRule(cols int) string {
	if cols <= 0 {
		return ""
	}
	return strings.Repeat(TurnRuleGlyph, cols)
}

// turnLabel is the turn's one-line identity ("turn N · <prompt>"), composed at
// full length: whoever renders it decides the budget, and cuts it with cutField
// so a shortened label carries the marker.
func turnLabel(info TurnInfo) string {
	label := turnLabelTurn + strconv.Itoa(info.Index)
	if info.Stamp {
		// THE MOMENT, or the shared formatter's explicit absence. It rides
		// BETWEEN the ordinal and the prompt so the label reads "turn 7 ·
		// 2026-11-08 12:31:20 · <prompt>" — the same three fields, in the same
		// order, that the web banner puts on one line — and so a narrow band
		// cuts the PROMPT's tail rather than the time.
		label += sessionevent.TurnStampSep + sessionevent.FormatTurnStamp(info.OpenedAt)
	}
	if p := strings.TrimSpace(info.Prompt); p != "" {
		label += StatusSep + p
	}
	return label
}

// turnLabelTurn is the header's turn word (the label reads "turn 3 · <prompt>").
const turnLabelTurn = "turn "

// BeginTurn starts a turn: the header is committed into the band (exactly once,
// in order) and the turn's body is marked so its committed lines carry the
// marker column. It replaces Begin for a caller that keeps a turn counter; a
// zero TurnInfo emits no chrome at all (Begin's behaviour, one call).
//
// The header goes through the SAME writeLive/finalizeLiveRow path as the answer
// text. That is not a convenience: the band is the only scroll region on the
// screen, and a header written any other way (a second writer, a direct stderr
// print) would fight the live row — the "one writer per row" invariant the chat
// path already paid for once.
func (r *AnswerRenderer) BeginTurn(info TurnInfo) {
	if r.routed(answerOp{kind: answerOpBeginTurn, info: info}) {
		return
	}
	r.beginTurnNow(info)
}

// beginTurnNow is BeginTurn's own body (see beginNow for who runs it).
func (r *AnswerRenderer) beginTurnNow(info TurnInfo) {
	r.Begin()
	if info.Index <= 0 || !r.out.Active() {
		return
	}
	// The prompt is kept because the REQUEST row (layers.go, LayerRequest) is
	// built from the same two values the header names, and the header is
	// committed before anything that would carry them again.
	r.turnPrompt = info.Prompt
	r.turnIndex = info.Index
	r.requested = false
	// The header is CHROME, not body: it is drawn at column 0 across the whole
	// width (a two-cell gutter would push its rule past the right edge and wrap
	// the header onto a second row — measured, and it costs the band a row). The
	// marker is therefore armed only once the header's LAST row is committed.
	//
	// The header is written ROW BY ROW (writeLive + finalizeLiveRow, the same
	// path every committed line takes), one row per finalize, so each header row
	// is a committed row in the scrollback ledger exactly like an answer row and
	// the band's row accounting needs no special case.
	for _, line := range TurnHeaderLines(info, r.cols) {
		r.writeLive(line)
		r.finalizeLiveRow()
	}
	r.marked = true
	// The REQUEST row: the operator's own line, committed as the FIRST row of
	// the turn's body — before any tool call (announced when the model asks for
	// it) and before any answer fragment. Committing it HERE, rather than with
	// the first answer line, is what makes the append order the causal order
	// without any reordering: everything the turn paints afterwards lands below
	// it. Nothing is reserved and no row is ever rewritten.
	r.drawRequestRow()
}

// EndTurn closes a turn: a rule is committed below the last answer line, so the
// next turn's header is never flush against this turn's answer, and the turn's
// marker state is dropped. It is called AFTER Finish (the tail must be committed
// first, or the rule would land above it).
func (r *AnswerRenderer) EndTurn() {
	if r.routed(answerOp{kind: answerOpEndTurn}) {
		return
	}
	r.endTurnNow()
}

// endTurnNow is EndTurn's own body (see beginNow for who runs it).
func (r *AnswerRenderer) endTurnNow() {
	if !r.marked || !r.out.Active() {
		return
	}
	// The rule is CHROME, like the header: full width from column 0, no gutter
	// (and never a reasoning line — a turn that streamed nothing but reasoning
	// must not close its block under the thinking gutter; the rule ends the
	// TURN, and the answer is what the turn was for). The marker is dropped
	// BEFORE the write so the rule's own row cannot pick it up.
	r.thinking = false
	r.marked = false
	r.writeLive(TurnRule(r.cols))
	r.finalizeLiveRow()
}

// lineGutter is the marker a line committed right now would carry, or "" for an
// unmarked line. It is the ONE place the gutter is chosen, so a marked answer
// line, a thinking line and the turn's opening line cannot disagree about the
// column the block starts in: every gutter below is exactly two display cells,
// which is what makes the turns (and the reasoning inside a turn) line up down
// their left edge instead of forming a ragged staircase.
func (r *AnswerRenderer) lineGutter() string {
	if r.noting {
		// A NOTE row carries its own glyph at column 0 (a tool call's "⚙ bash …",
		// the same shape the reference TUI prints): the turn's two-cell gutter
		// would indent it one column right of the rows it belongs beside, and the
		// activity block would then have two left edges instead of one.
		//
		// Since the note's rows are reserved through r.cols explicitly (see
		// writeNote) the reserved width is the band's own, not the caller's edit
		// row: the two differ on the xterm legend layout, and stating the smaller
		// of them would under-reserve every note row by the difference.
		if r.noteContinuing {
			// …except a row that CONTINUES the note above it, which carries the
			// continuation marker: same two cells, a different glyph, so the
			// activity block keeps one left edge and a continued command is
			// distinguishable from a fresh one (see NoteContinuation).
			return NoteContinuation
		}
		return ""
	}
	if !r.marked || r.cols <= displayWidth(TurnMarker)+minGutterContentCells {
		// No turn chrome (the caller keeps no turn counter), or a terminal too
		// narrow to give the gutter AND a cell of content: an unmarked line is
		// the honest degradation — a gutter that eats the line's only cell
		// would be worse than no gutter at all.
		return ""
	}
	// The body's own layer decides the marker (layers.go): reasoning and answer
	// are the two marked layers here, and their rows are the same two cells wide
	// so the turn's left edge stays one column.
	switch {
	case r.thinking:
		return layerMarker(LayerThinking)
	case !r.ledged:
		return layerMarker(LayerResponse)
	default:
		return layerContinuation(LayerResponse)
	}
}

// minGutterContentCells is how many cells of actual LINE content must survive
// beside the gutter for the gutter to be worth drawing. One cell is a char: a
// terminal that can show the gutter and a character gets it; one that cannot
// gets plain text.
const minGutterContentCells = 1

// isWrapSpace reports whether r is a cell a wrap may consume: the ASCII space,
// which has no ink and whose loss a reader cannot see. It is a named predicate
// rather than a literal comparison at the cut, so "which characters may a wrap
// consume" has ONE definition (Rule 1).
func isWrapSpace(r rune) bool { return r == ' ' }

// trimWrapSpace drops the run of wrap spaces at the head of a row's text and
// reports the text that row must draw.
//
// WHY IT IS NOT COSMETIC. A row that FOLLOWS a wrap re-opens the marker column:
// writeGutter draws the block's displayWidth(TurnMarker) cells, and the text then
// starts at that column. A space left at the head of the text is drawn INSIDE
// the column, so the row's first visible character lands one cell further in and
// the block's left edge reads as a staircase —
//
//	▌ 这一段中文答案用来检查换行之后的列是否对齐，中间夹着 ASCII
//	   token 和 code 片段，然后再来一段中文以确保换行发生多次
//
// where the continuation opens at cell 3 against the block's 2. Measured
// 2026-09-27 on master: `cols=60 burst=7: body row 18 opens at cell 3, want the
// block's column 2`. The same arithmetic is what leaves a row opening OUTSIDE
// the column entirely on the operator's dump (`tatus ==="; git status -sb`
// beneath `▌ cd /home/user/work/GoPi; … git branch -vv`).
//
// It is applied ONLY to a CONTINUATION row — one the renderer opens with the
// blank gutter because the line it belongs to is still being drawn — and only to
// the spaces a wrap itself produced. A row that OPENS a line keeps its text as
// given: leading spaces there are the author's indentation, and the band draws
// them.
func trimWrapSpace(text string) string {
	if text == "" || !isWrapSpace(rune(text[0])) {
		return text
	}
	trimmed := strings.TrimLeftFunc(text, isWrapSpace)
	return trimmed
}

// BeginThinking opens the model's reasoning block inside the current turn: the
// lines committed from now on carry the thinking gutter, so what the model is
// working out is visibly NOT the answer. It is idempotent (a stream of thinking
// deltas may each open the block) and a no-op on a renderer with no turn chrome
// or no region — the fallback path's bytes do not move.
func (r *AnswerRenderer) BeginThinking() {
	if r.routed(answerOp{kind: answerOpBeginThinking}) {
		return
	}
	r.beginThinkingNow()
}

// beginThinkingNow is BeginThinking's own body (see beginNow for who runs it).
func (r *AnswerRenderer) beginThinkingNow() {
	if !r.out.Active() {
		return
	}
	r.thinking = true
}

// EndThinking closes the reasoning block and applies the SPACING RULE between
// thinking and answer: the reasoning's own (in-flight) line is committed, then
// exactly ONE blank row is committed, so the answer starts on a fresh line with
// a visible gap instead of continuing the reasoning's last line. The blank row
// is written only when the block actually committed something — a block that
// streamed nothing adds no gap.
//
// It is idempotent and safe to call before every answer delta: it returns
// immediately when no reasoning block is open, which is the common case.
func (r *AnswerRenderer) EndThinking() {
	if r.routed(answerOp{kind: answerOpEndThinking}) {
		return
	}
	r.endThinkingNow()
}

// endThinkingNow is EndThinking's own body (see beginNow for who runs it).
func (r *AnswerRenderer) endThinkingNow() {
	if !r.thinking {
		return
	}
	r.thinking = false
	if !r.thinkingWrote {
		return
	}
	r.thinkingWrote = false
	if r.liveText != "" {
		// The reasoning's last line was still in flight on the live row (the
		// stream ended without a break): commit it first, or the blank row
		// would land ON it.
		r.finalizeLiveRow()
	}
	// The spacing row: an empty live row committed with one scroll. It is the
	// turn's own paragraph break — the same blank-row mechanism the answer's
	// own empty lines use, so the band has exactly one notion of a blank row.
	r.finalizeLiveRow()
}

// ThinkingWrote reports whether a reasoning line has been committed since the
// last EndThinking. Tests read it; production never branches on it.
func (r *AnswerRenderer) ThinkingWrote() bool { return r.thinkingWrote }

// ── the TURN-SIDE calls, routed ──────────────────────────────────────────────
//
// Every method that MUTATES renderer state (the in-flight buffers, the live row
// and column, the marker and reasoning ledger, the caller-row queue, the
// composer's fence) first asks routed() whether this goroutine may touch that
// state right now. The rule is the package's own invariant, applied to the
// renderer:
//
//	THE READ LOOP OWNS THE RENDERER WHILE IT RUNS. Every field is then applied by
//	the loop alone, through the queued op (answerqueue.go). A turn-side call
//	PUBLISHES instead of mutating, exactly as Editor.PublishStatus publishes a
//	status repaint, Editor.Note publishes a caller row and Editor.WriteLines
//	publishes a band frame.
//
//	WITH NO READ LOOP RUNNING the caller IS the owner: a unit test drives the
//	engine directly (51 call sites in this package's own tests), the pty cases
//	drive a renderer that has no loop, and a renderer no editor built belongs to
//	whoever holds it. Those callers take the historic road — the work is done
//	here — which is what keeps them byte-for-byte the same program.
//
// WHY THE ANSWER IS "PUBLISH", not "lock the fields": a lock over the state
// would leave the two goroutines' passes just as interleaved and merely stop the
// detector from seeing it (Rule 15). Routing removes the interleaving: the state
// has ONE owner at any instant, and the reads the loop performs while drawing a
// row (the region's live row, the band width, the composer's fence) are on the
// same goroutine as the mutations.
//
// The identity test is the same one the band seam uses (bandwait.go): the
// goroutine id of the loop, compared for EQUALITY. The editor's wrapper decides
// "someone else owns me" from `loopGoid`; a renderer with no editor behind it
// never routes, because nobody else can reach it.
func (r *AnswerRenderer) routed(op answerOp) bool {
	if r == nil || r.out == nil || !r.out.Active() {
		// No reserved layout: the caller's own write path is the historic one
		// (see Finish — a renderer with no band publishes nothing).
		return false
	}
	q := r.answerQueueOf()
	if q == nil {
		return false
	}
	if q.loopGoid() == 0 || q.onLoop() {
		// No loop is running (the caller owns the renderer), or the caller IS
		// the loop (the drain's own application — re-entering the queue here
		// would loop forever).
		return false
	}
	q.queue(op)
	return true
}

// answerQueueOf returns the editor-side seam that owns this renderer, or nil
// when no editor built it (a test's own renderer, or one whose region was taken
// down).
func (r *AnswerRenderer) answerQueueOf() *answerQueue {
	if r.editor == nil {
		return nil
	}
	return &r.editor.answers
}

// loopGoid is the owned editor's read-loop identity, 0 when no loop is running.
func (q *answerQueue) loopGoid() int64 {
	if q.editor == nil {
		return 0
	}
	return q.editor.loopGoid.Load()
}

// onLoop reports whether the CALLER is the goroutine running the read loop.
func (q *answerQueue) onLoop() bool {
	return q.editor != nil && q.editor.onLoopGoroutine()
}

// queue raises the level and wakes the loop. It is queueAnswerOp, reached from
// the renderer (which is where the call originated).
func (q *answerQueue) queue(op answerOp) { q.editor.queueAnswerOp(op) }

// Composer returns the answer-body composer this renderer classifies lines
// with, so a caller that must ask the classifier a question (the turn's own
// answer boundary) asks the OBJECT rather than a copy of its state. Copying the
// fence out of the renderer would read renderer state from whatever goroutine
// holds the pointer; asking the composer keeps the fence in the composer, where
// the read loop and the turn share it through its own methods.
func (r *AnswerRenderer) Composer() AnswerComposer { return r.composer }
