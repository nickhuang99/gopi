package lineedit

import "strings"

// compose.go — the answer body's line composer: the decision of which lines are
// CODE, which are DIFF additions/removals, and which are ordinary prose.
//
// The decision lives OUTSIDE the renderer's placement and measurement code: a
// composer is asked about one COMPLETE line and answers with a line style; the
// renderer styles what it is told. That is deliberate (Rule 1b): the renderer
// must never learn a format, or a change to the format becomes a change to the
// layout arithmetic that the cell-grid assertions protect.
//
// A line can only be decided when it is COMPLETE — a fence marker is one line
// long, and half of it would decide a fence that is not there — so the renderer
// holds an unfinished line's text UNSTYLED until its break arrives (see
// AnswerRenderer.drain). The hold is text-only, never a style: what the user
// can see never waits for the decision, only its emphasis does.

// FenceMarkers are the line prefixes that open and close a code block in an
// answer. They are named constants (Rule 1b) because the surfaces that render
// an answer and the ones that classify it must agree on ONE spelling.
const (
	// FenceMarker is the triple-backtick fence (with or without a language).
	FenceMarker = "```"
	// FenceMarkerAlt is the indented fence a model sometimes emits instead.
	FenceMarkerAlt = "~~~"
)

// DIFF markers: the first character of a diff line inside a code block. They are
// the unified-diff convention (`+`/`-`), and they are read ONLY inside a fence,
// where a leading +/- is a diff rather than prose punctuation.
const (
	diffAddMarker = '+'
	diffDelMarker = '-'
)

// diffHunkMarker is the unified-diff hunk header's prefix ("@@ -1,3 +1,4 @@").
const diffHunkMarker = "@@"

// LineStyle names what a line of an answer IS, in terms the palette has a style
// for. It is the composer's whole vocabulary: a composer never returns a colour,
// only a meaning.
type LineStyle int

const (
	// LinePlain is ordinary answer prose.
	LinePlain LineStyle = iota
	// LineCode is a line inside a fenced code block.
	LineCode
	// LineDiffAdd is a line ADDED by a diff.
	LineDiffAdd
	// LineDiffDel is a line REMOVED by a diff.
	LineDiffDel
)

// AnswerComposer decides an answer line's style. The renderer asks it in stream
// order — every complete line exactly once, plus the style of the block an
// unfinished line belongs to — and it is driven from the single goroutine that
// feeds the answer.
type AnswerComposer interface {
	// StyleFor reports what a COMPLETE line is. The line carries no trailing
	// newline. It is the authoritative decision, and it advances the composer's
	// state (a fence opens or closes here).
	StyleFor(line string) LineStyle
	// Open reports the style an UNFINISHED line already belongs to: the block
	// the stream is inside right now. It exists so the renderer can paint each
	// burst the moment it arrives — a line's emphasis may lag its characters,
	// but its characters must never wait for the emphasis (that would turn a
	// streaming answer into a paragraph-at-a-time one).
	Open() LineStyle
}

// LineComposer is the shipped composer: fenced code blocks, and diff lines
// inside them. It is a stateful, single-stream classifier — state is the block
// no more: it never inspects a line's content beyond the fence marker and the
// diff marker.
type LineComposer struct {
	// open is the marker that opened the block the stream is inside, in the form
	// its CLOSING marker must match; the zero value means "not in a block" and is
	// read through inFence, never compared directly (Rule 13).
	//
	// It is the composer's only state that survives a line, and it is the MARKER
	// rather than a bool because a fence marker toggles the block (see
	// asFenceMarker): a marker that is not this block's own delimiter is CONTENT,
	// not the end of the block.
	open fenceMarker
}

// fenceMarker is a fence marker as the classifier reads it: the line's leading
// whitespace, and the fence character with how many of it the line carries.
//
// Both halves are needed to decide whether a marker is a block's DELIMITER or the
// block's CONTENT, because a block's own opening marker is the one its closing
// marker must repeat — the same character and the same run, at the same
// indentation. That is the rule a quote INSIDE a block relies on: an answer that
// pastes a screen capture carries the capture's own marker (indented, or a longer
// run) inside the fence that quotes it, and that inner marker is text of the
// block, not the end of it.
//
// THE RULE IS MEASURED, not a taste (Rule 27's discipline, session corpus
// 2026-09-26): over 65,414 stored assistant answers, the marker must match on
// (character, run, indentation) to close — 47 lines were being misclassified as
// prose that belong inside their block, and the only 4 lines it newly calls prose
// are themselves the defect's own victims (session a8553c96: a quoted capture
// whose inner marker used to leave the answer's two closing paragraphs painted as
// code). The alternative — requiring an opening marker to be UNINDENTED — was
// measured too and REJECTED: it cost 1,456 indented code lines their code style,
// because indenting a whole block is a habit in our own answers.
type fenceMarker struct {
	// indent is the line's leading whitespace, byte for byte: indentation is how
	// a block is nested, so a marker at another indentation belongs to another
	// block (or to the block's own content).
	indent string
	// char is the fence character (FenceMarker's or FenceMarkerAlt's) and run is
	// how many of it the line carries: a longer run is a marker of a block that
	// quotes one opened with a shorter run, never that block's own delimiter.
	char byte
	run  int
}

// fenceIndentChars are the characters a marker's indentation is made of.
const fenceIndentChars = " \t"

// AnswerGutters are the two-cell row prefixes the answer band ITSELF draws at
// column 0 — the band's own spellings, taken from their one definition site
// (answer.go's TurnMarker / TurnMarkerSplit / ThinkingGutter) rather than typed
// again (Rules 1b/14). They are exported because the band is drawn by this
// package and READ by others (internal/uiclient's CompareGutters strips exactly
// this list off a grid row): one list, one place, no second spelling to drift.
//
// They belong to the classifier because a line the answer CARRIES can hold one
// of them: every copy of the band's screen holds it (the row TEXT of a dump, a
// selection pasted into the transcript, an answer quoting its own screen), and
// the operator's minimal repro is exactly that shape — a fenced block whose
// every line, the opening marker's content and the CLOSING marker included,
// carries the band's `▌ ` prefix. `▌` (U+258C) is not one of fenceIndentChars,
// so a marker behind it was no marker at all: the block opened at the plain
// marker and never closed, and every later prose line was painted as code.
//
// The prefix is read as a ROW prefix — never as content, and never as
// indentation: whatever whitespace stands between it and the marker is still the
// marker's own indentation (see fenceMarker), so nesting is unaffected.
var answerGutters = [...]string{TurnMarker, TurnMarkerSplit, ThinkingGutter}

// AnswerGutters returns the band's row prefixes as a fresh slice — the read-only
// face of answerGutters, for a caller that must not see (or write) the array the
// classifier scans on every line.
func AnswerGutters() []string {
	out := make([]string, len(answerGutters))
	copy(out, answerGutters[:])
	return out
}

// trimAnswerGutter removes ONE leading answer-band gutter from a line, leaving a
// line that carries none untouched. It reads the line's SHAPE only: the cells the
// band draws stay exactly where they were (nothing here removes a character from
// what is drawn) — the classifier simply stops claiming a marker is invisible
// because the product's own glyph precedes it.
func trimAnswerGutter(line string) string {
	for _, g := range answerGutters {
		if strings.HasPrefix(line, g) {
			return line[len(g):]
		}
	}
	return line
}

// inFence reports whether the stream is inside a code block: the zero marker is
// "no block", and a parsed marker always carries a run (see asFenceMarker), so
// the two can never be confused.
func (c *LineComposer) inFence() bool { return c.open.run > 0 }

// StyleFor classifies one complete line.
func (c *LineComposer) StyleFor(line string) LineStyle {
	// The line may arrive WEARING the answer band's own row prefix (see
	// answerGutters): a display row copied out of the product carries it, and the
	// operator's repro is a fenced block whose content is exactly such rows. The
	// prefix is stepped over ONCE, before anything is decided, so a marker behind
	// it is the marker it is. This is not "knowing layout" (the composer still
	// reads the line alone, never a width or a row index): it is the same
	// tolerance the line's own indentation already gets, applied to the one glyph
	// the product draws at column 0.
	//
	// It is fixed HERE, in the classifier, rather than at the call sites, because
	// the composer is the ONE component that answers "is this line a fence" and
	// the stripping at every caller would be the same rule written N times
	// (Rule 1b/14) — the reason the classifier exists at all.
	body := trimAnswerGutter(line)
	if m, ok := asFenceMarker(body); ok {
		// The fence line itself is code, and it toggles the block: the marker both
		// opens and closes, which is the format. What it toggles is decided by
		// WHICH marker it is — not by the mere fact that it starts with one, which
		// is what used to leave one stray marker in charge of every later line.
		switch {
		case !c.inFence():
			c.open = m
		case c.open == m:
			c.open = fenceMarker{}
		}
		return LineCode
	}
	if !c.inFence() {
		return LinePlain
	}
	if strings.HasPrefix(strings.TrimSpace(line), diffHunkMarker) {
		return LineCode
	}
	// A diff line is a line whose FIRST character is the marker — not merely a
	// line that contains one, or every `--flag` in a code block would read as a
	// removal.
	if len(line) > 0 && (line[0] == diffAddMarker || line[0] == diffDelMarker) {
		// Inside a fence, a +/- run is a diff. The run's first line needs no
		// predecessor: a fenced block whose FIRST line is a +/- line is a diff
		// fragment, and the shell transcript convention ("+ added") is the same
		// shape. Inside the fence there is no prose to confuse it with.
		if line[0] == diffAddMarker {
			return LineDiffAdd
		}
		return LineDiffDel
	}
	return LineCode
}

// EndStream closes the STREAM this composer has been classifying: the block the
// stream is inside is dropped, so the state it carries cannot outlive the text it
// was derived from.
//
// THE RULE, and why it is this one (measured, session corpus 2026-09-26 — 2,598
// sessions, 79,924 turns, 81,157 stored assistant answers): a fence is a
// delimiter of a MESSAGE, not of a session. A block that a message opens and never
// closes is that message's unfinished quote — the next message is a new answer,
// and its prose is prose. Measured with the reader's own test (a line carrying an
// emphasis marker that markdown's own marker parity says is outside a block),
// over 35,808 such lines:
//
//	one composer for the whole SESSION (before this method)  26 lines called code
//	one composer per TURN                                   0
//	one composer per ANSWER                                 0
//
// The comparison is also the ARGUMENT for the boundary being a stream and not a
// bigger reset: the cost of the missing boundary is carried by answers that do not
// themselves end inside a block (0 of the 26 leaks had a preceding answer that
// ended in an open block — a fence that opens and never closes is defect enough,
// but it is not what leaves the NEXT answer's prose painted as code). Whatever
// scope a caller classifies — one turn or one assistant message — ONLY the two
// ends exist, and a classifier that cannot run past its own end is the whole of
// the guarantee. The narrower of the two (per answer) is a calling-convention
// choice, available to any caller that has a delimiter, and costs nothing here.
//
// It is not a repair of the text already classified: like the delivery of a style,
// it says only what the NEXT line is.
func (c *LineComposer) EndStream() { c.open = fenceMarker{} }

// Open reports the style an unfinished line belongs to: inside a fenced block
// every line is code, outside it every line is prose. It is a PROVISIONAL
// answer — used only for the text of a line whose ending has not arrived — and
// it is deliberately the block's style rather than the line's: a line cannot be
// classified as a diff until it is whole, and code inside a fence is the one
// thing already known.
func (c *LineComposer) Open() LineStyle {
	if c.inFence() {
		return LineCode
	}
	return LinePlain
}

// asFenceMarker reads a line as a fence marker, or reports that it is not one. A
// marker is a line whose text STARTS with the fence character repeated at least
// FenceMarker's length — the marker alone, or the marker followed by a language
// tag — and what it reports is the SHAPE a closing marker of that block must have
// (see fenceMarker).
//
// The marker is read from the line's FIRST non-blank character, so indentation is
// part of the marker rather than something trimmed away: `  ``` ` is a marker at
// indentation two, and (measured, above) indenting a whole block is a habit worth
// keeping.
func asFenceMarker(line string) (fenceMarker, bool) {
	indent := line[:len(line)-len(strings.TrimLeft(line, fenceIndentChars))]
	rest := line[len(indent):]
	if rest == "" {
		return fenceMarker{}, false
	}
	ch := rest[0]
	if ch != FenceMarker[0] && ch != FenceMarkerAlt[0] {
		return fenceMarker{}, false
	}
	run := 0
	for run < len(rest) && rest[run] == ch {
		run++
	}
	if run < len(FenceMarker) {
		return fenceMarker{}, false
	}
	return fenceMarker{indent: indent, char: ch, run: run}, true
}

// styleFor maps a line style to the palette's style for it. It is the ONE
// mapping from meaning to emphasis, so a surface cannot emphasise a code line
// by reaching for the diff colour.
func styleFor(pal Palette, ls LineStyle) Style {
	switch ls {
	case LineCode:
		return pal.Code
	case LineDiffAdd:
		return pal.DiffAdd
	case LineDiffDel:
		return pal.DiffDel
	default:
		return pal.Answer
	}
}

// styleForSpan maps a span's emphasis to the palette's style for it. It is the
// ONE mapping from span emphasis to emphasis (the counterpart of styleFor, and
// the reason no renderer compares a marker): the tokenizer decides WHAT a span
// is, and this decides how the palette draws it.
//
// A span's emphasis is a PROPERTY of the span, not a replacement for the line's
// style: an emphasised span inside a code line is still code. The renderer
// therefore applies this mapping only where the body's own emphasis layer is in
// charge (a plain line — see AnswerRenderer.spanStyle), so a fence's `**` stays
// the literal the code block says it is.
func styleForSpan(pal Palette, em Emphasis) Style {
	if em == EmphasisBold {
		return pal.AnswerBold
	}
	return pal.Answer
}
