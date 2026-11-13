package lineedit

import "strings"

// emphasis.go — the answer body's INTRA-LINE emphasis layer: the second half of
// the composer's job (compose.go decides a whole LINE's style; this file decides
// the emphasis of a LINE SPAN inside it).
//
// The rule that shapes everything here is the one compose.go states for lines:
// THE RENDERER NEVER LEARNS A FORMAT. A markdown marker is a fact about the
// FORMAT, so the only place the marker's spelling appears is the tokenizer
// below; every renderer (AnswerRenderer.writeRow, uiclient.wrapEntry) receives
// spans — "this text, this emphasis" — and never a marker. A renderer that
// tested for a marker would make a format change a layout-arithmetic change,
// which is exactly what the cell-grid assertions protect.
//
// The other half of the contract is ARITHMETIC: a marker occupies ZERO display
// cells. The tokenizer therefore removes markers from the text it emits (a span
// carries the text WITHOUT them), so every consumer that measures cells — the
// row cutter, the wrap, the gutter column — measures the text the terminal
// actually draws and needs no correction term for a marker.
//
// TWO callers, ONE tokenizer:
//
//   - `gopi ui` (internal/uiclient) tokenizes each streamed line and draws the
//     text with plain SGR, because it hands its frames to bubbletea, which
//     counts bytes, not cells, and cannot read the package's escape vocabulary.
//   - the banded editor's AnswerRenderer tokenizes the line, PAINTS the visible
//     text with no markers, and emphasises each span through the palette, so
//     the per-cell attribute grid (screenattr.go) records Bold exactly on the
//     bold span's cells.

// Emphasis is how strongly a span of an answer line is emphasised. It is the
// composer layer's vocabulary of INTRA-LINE emphasis — one value today, because
// the format's own scope is one marker, and a named set rather than an int so a
// future "code span inside prose" is a value here rather than a second
// parameter threaded through every renderer.
type Emphasis int

const (
	// EmphasisNone is ordinary answer text.
	EmphasisNone Emphasis = iota
	// EmphasisBold is intra-line bold.
	EmphasisBold
)

// BoldMarker is the intra-line emphasis marker. It is defined HERE — the
// tokenizer that reads it — and not by the renderers, so the surfaces that
// measure the text never mention it (Rule 1b), and the web page's own spelling
// of the same marker is pinned to this constant by a regression test (Rule 14:
// one definition, checked).
const BoldMarker = "**"

// boldMarkerLen is the marker's length in bytes, used by the tokenizer's scan.
const boldMarkerLen = len(BoldMarker)

// LineSpan is one piece of an answer line: the text as the terminal must draw
// it — markers REMOVED — and the emphasis it carries.
//
// The text is marker-free by construction, which is what makes the display
// arithmetic honest: `displayWidth(SpanText(spans)) <= displayWidth(line)`,
// with equality when the line carries no marker and a shortfall of
// 2 * len(BoldMarker) per bold span otherwise. A renderer never has to know
// why a line lost two characters.
type LineSpan struct {
	Text     string
	Emphasis Emphasis
}

// LineSpans tokenizes one answer line into spans. It is a PURE function of the
// line's text: it holds no state, so an unfinished line can be asked again when
// its next burst arrives (see SpanLine) and a line split across two frames
// tokensizes to the same spans as the whole line.
//
// The grammar is ONE rule, deliberately (the format's whole scope here):
//
//	A marker-open is a BoldMarker that is followed by text, and the span ends
//	at the NEXT BoldMarker. `**bold**` is one bold span; the markers vanish.
//
// Everything else is literal text, and every one of the shapes below is
// deliberate rather than accidental:
//
//   - an UNCLOSED marker stays literal (`**hi` → one span "**hi"), matching the
//     format's own rule that an unterminated fence ends at the end of the text;
//   - an EMPTY span is not a span: `****` is literal text, because a marker pair
//     with nothing between it is not emphasis but punctuation the model typed;
//   - text before, between and after spans is its own span, and a line with no
//     marker returns exactly one span.
//
// The one caller that must NOT ask about an unfinished line is the renderer
// (see AnswerRenderer.computeRows): a line whose end has not arrived may still
// grow its closing marker, so the renderer paints it provisionally and decides
// when its break arrives.
func LineSpans(line string) []LineSpan {
	if line == "" {
		// An empty line has no text to draw: no span at all, rather than one
		// carrying nothing (a zero-length run would make "the spans' text is the
		// line" true for the wrong reason, and give a renderer a run to style
		// that draws no cell).
		return nil
	}
	// The common case — no marker at all — allocates one span and is the shape
	// every non-emphasised line of every answer takes.
	i := strings.Index(line, BoldMarker)
	if i < 0 {
		return []LineSpan{{Text: line, Emphasis: EmphasisNone}}
	}
	spans := make([]LineSpan, 0, 4)
	rest := line
	for {
		i = strings.Index(rest, BoldMarker)
		if i < 0 {
			if rest != "" {
				spans = append(spans, LineSpan{Text: rest})
			}
			return spans
		}
		if i > 0 {
			spans = append(spans, LineSpan{Text: rest[:i]})
			rest = rest[i:]
		}
		// rest now STARTS with the marker: either it opens a span, or it is one
		// of the literal shapes below.
		body := rest[boldMarkerLen:]
		end := strings.Index(body, BoldMarker)
		if end < 0 {
			// Unclosed: the marker and everything after it is literal text.
			spans = append(spans, LineSpan{Text: rest})
			return spans
		}
		if end == 0 {
			// A marker immediately followed by a marker: an EMPTY span, which is
			// not emphasis. BOTH markers stay literal and the scan resumes AFTER
			// them, so a later, properly closed pair on the same line still
			// counts (`a****b **c**` draws `a****b c`).
			spans = append(spans, LineSpan{Text: BoldMarker + BoldMarker})
			rest = body[boldMarkerLen:]
			continue
		}
		spans = append(spans, LineSpan{Text: body[:end], Emphasis: EmphasisBold})
		rest = body[end+boldMarkerLen:]
	}
}

// SpanText returns the text the spans draw — the line with its markers removed,
// i.e. exactly what a terminal must show for that line. It is the ONE place a
// caller reads "what will the screen show", so a width assertion is stated
// against the drawn text rather than against the raw line (which still carries
// the markers).
func SpanText(spans []LineSpan) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

// SpanLine tokenizes one answer line, splitting at newlines first: the caller
// streams whole lines (the renderer hands over complete lines; `gopi ui` appends
// deltas to one entry), so the newline is never part of a span's text and a
// multi-line entry wraps by line with each line tokenized on its own.
func SpanLine(line string) []LineSpan {
	if !strings.Contains(line, escNewline) {
		return LineSpans(line)
	}
	var out []LineSpan
	for _, seg := range lineSegments(line) {
		content, terminated := lineEndingSplit(seg)
		out = append(out, LineSpans(content)...)
		if terminated {
			out = append(out, LineSpan{Text: escNewline})
		}
	}
	return out
}

// hasBoldMarker reports whether a line carries the format's intra-line emphasis
// marker at all. It exists so a renderer can ask the FORMAT LAYER whether there
// is anything to tokenize — with the marker's spelling staying in this file
// (Rule 1b) — and so the overwhelmingly common line (no marker anywhere) skips
// the span bookkeeping entirely.
func hasBoldMarker(line string) bool { return strings.Contains(line, BoldMarker) }

// DisplaySpans returns the spans of an answer line AS THE SURFACE MUST DRAW IT:
// one span per emphasis run with the markers removed. It is SpanLine's exported
// face, and it is what a caller that hands lines to a renderer of its own asks
// for — a caller never needs the marker, only the runs.
func DisplaySpans(line string) []LineSpan { return SpanLine(line) }

// DisplayText returns the text of an answer line as the screen shows it, with
// the intra-line markers removed. A caller that measures a line's WIDTH (or a
// test that compares a capture against what the operator should read) asks this,
// never the raw line: the raw line still carries markers that occupy no cell.
func DisplayText(line string) string { return SpanText(SpanLine(line)) }

// decideRow draws a COMPLETE plain answer line: the composer's verdict for the
// LINE is taken as given (the caller has it), and the body's emphasis layer is
// asked about the line's spans. It is the emphasis layer's half of the row
// decision, kept here (not in answer.go) so the format knowledge stays in one
// file.
func (r *AnswerRenderer) decideRow(line string) answerRow {
	return r.rowFor(line, r.composer.StyleFor(line))
}

// headIsTheLinesOwnEarlierHalf reports whether `raw`, the line the row was decided
// from, CARRIES this head as its own earlier half — the rejoin commitLines performs
// when a burst boundary split a line inside an emphasis pair.
//
// WHY IT IS NOT headIsInRow, and why it is not "raw starts with head" either
// (measured 2026-09-29 at the operator's 117 columns, the reason this exists): the
// two heads of the split pair are not comparable as DECISIONS. When a delta
// boundary falls inside a pair, the live row shows a head whose closing marker is
// truncated — `**b*` for the line `a\n**b**` — so tokenizing that head keeps its
// opening `**` LITERAL (an unclosed pair must stay literal on screen, see
// LineSpans) and headIsInRow compares the literal spelling against a row decided
// from the whole line, answers "no", and glues the head back on. The committed row
// then reads `**b*b`: the operator's symptom, an emphasis boundary that does not
// match the source, on a row the band never repaints.
//
// The spelling is what settles it: `raw` here IS the rejoined whole line and it
// literally opens with the head's bytes, so the row's decided text already begins
// with those characters and nothing may be prefixed. The LENGTH is the second half
// of the test and it is not decorative: commitPrefix hands the SAME string as both
// the head and the line it was painted from (the partial paint drew exactly the
// head, and no more — the hold in unfinishedLineWouldWrap is what keeps a
// wrapping line whole), and an equality there means the head is the row's WHOLE
// spelling rather than an earlier half of it. Prefixing is then required, and it is
// what the wrap-space ledger's fixtures read: treating that equality as "already
// carried" drops the head from every row whose break arrives in its own delta.
func headIsTheLinesOwnEarlierHalf(raw, head string) bool {
	return head != "" && len(raw) > len(head) && strings.HasPrefix(raw, head)
}

// fitCommitRow reconciles the row being committed with the text the live row
// ALREADY shows, and reports whether the row is drawable.
//
// The head is what an earlier burst painted on this row (the unfinished tail of
// the stream). It is carried forward — the band's commit scrolls the row into
// place, and the head must not be written twice or dropped — but it is carried as
// the text the row will SHOW (headSpans below), never as the spelling it was
// painted with, and its width is the width of that text.
//
// The reconciliation is the cutter's own arithmetic (cutCols on the text the row
// will SHOW), never a second measurement: the head is trimmed to the cells the
// row can hold, and the row's text becomes head+line — which is the row's spans
// with their leftover head text in front. `ok` is false only when not even the
// head fits (a wide character wider than the terminal, which the caller deals
// with by leaving the line for the next burst).
//
// It returns a value rather than mutating in place, so the caller's row stays the
// single decision that was made about the line.
func fitCommitRow(row answerRow, head, raw string, cols int) (answerRow, bool) {
	if head == "" {
		return row, true
	}
	spans := headSpans(head)
	shown := SpanText(spans)
	// THE HEAD AS A SPELLING, before it is a decision. `raw` is the line the row
	// was decided from, and a head that is a VERBATIM prefix of it is the same
	// characters in the stream's own spelling: the row draws the line, which opens
	// with those characters, so prefixing the head would draw them twice.
	//
	// This is the test headIsInRow cannot make, and it is the one the marker pair
	// needs (measured 2026-09-29 at the operator's 117 columns): when a delta
	// boundary falls INSIDE a pair (`a\n` + `**b` + `**`), the live row shows the
	// head's spelling `**b*` — its closing marker truncated, so the head's own
	// tokenization keeps the first `**` literal and folds the wrong marker. The row
	// below was decided from the WHOLE line `**b**` and draws `b`; comparing the
	// two as decisions answers "no", the head is glued back on, and the committed
	// row reads `**b*b` — the operator's symptom, an emphasis boundary that does
	// not match the source, on a row the band never repaints.
	if headIsTheLinesOwnEarlierHalf(raw, head) || headIsInRow(row.text, shown) {
		// The row's own decided text ALREADY opens with the head: the text was
		// decided from the whole line (compPrefix + the fragment the stream split
		// it into, or the live row's own carried text), so the head's characters
		// are in it. Prefixing them again draws the head twice — the measured
		// `the bold conclusion followsthe bold conclusion follows` (the
		// operator's terminal is 60+ columns, so both copies fit and the
		// duplicate is exactly what the reader sees, with nothing to repair it).
		return row, true
	}
	avail := cols - displayWidth(shown)
	if avail <= 0 {
		return answerRow{}, false
	}
	// The head's cells are already paid for; the row takes what is left of it.
	keep, _ := cutCols(row.text, avail)
	if keep == "" && row.text != "" {
		return answerRow{}, false
	}
	if keep == row.text {
		// The head did not cost the row anything (no markers were dropped, or
		// the row had room to spare): the row's own text is unchanged.
		//
		// The head still has to be PREFIXED, and this is the one case where the
		// caller cannot leave it out. The caller re-opens the committed row at
		// column 0 (commitPrefix sets liveCol = 0 before writing), so the row's
		// own text overwrites the head's cells; a row committed without the head
		// in its text loses the head — measured on a line that fully filled the
		// live row and acquired its break in the NEXT delta (the ordinary 1-3
		// byte token stream): the row's first characters were painted, the break
		// arrived alone, and the committed text started at the tail, so the head
		// was carried nowhere. On the operator's screen that is a row whose head
		// is gone beside a neighbour that is whole.
		return row.withHead(spans), true
	}
	return row.withHead(spans), true
}

// carryAlreadyQueued reports whether the text the live row already SHOWS is
// already part of the text this commit is about to queue — the question Finish's
// carry asks before appending it a second time.
//
// WHY IT IS NOT A SUFFIX TEST ON THE WHOLE QUEUE. The queue is
// `prefix + park + tail` and the live row's text belongs to ONE of those, not
// necessarily to the end of the concatenation: a burst boundary inside the stream's
// LAST line leaves the line's earlier half in the park and its continuation in the
// tail, and the painted row shows the park's text — so a test that only asks the
// concatenation's suffix sees the live text in the middle, judges it absent, and
// appends a second copy of a line that is already queued. Measured on a narrow
// (non-wrapping) fixture, which is the whole reason this is not the wrap carry
// family: the park ends with the shown text and the tail does not, so the relation
// is "the PARK carries it", not "the queue ends with it".
//
// A shown text that is a suffix of the PARK is therefore already in the commit, and
// appending it puts the line in twice — the operator's partial repeat (`尾` glued to
// the sentence it ends, the second copy cut by the row's width).
func carryAlreadyQueued(queued, park, shown string) bool {
	if shown == "" {
		return true
	}
	if strings.HasSuffix(strings.TrimSuffix(queued, "\n"), shown) {
		return true
	}
	// The park is queued as the line's EARLIER half (see Finish): text the row
	// shows that the park ends with is already in this commit.
	return park != "" && strings.HasSuffix(park, shown)
}

// headIsInRow reports whether the row's decided text ALREADY carries the head's
// characters — the case where prefixing the head would draw it twice.
//
// WHY THE HEAD IS NOT CARRIED VERBATIM — the defect this closes (measured
// 2026-09-27, pty-driven `gopi chat` at 117x30 against a 6-byte token stream).
// The live row paints an unfinished line PROVISIONALLY and literately: its markers
// are still there, because removing one is part of a decision the renderer has not
// taken yet (rowForPartial). The head is then carried forward into the row being
// COMMITTED — whose text has been through the tokenizer and lost its markers.
// Gluing the two together draws the head's markers as literal cells on a committed
// band row, where nothing ever repaints them: the operator's screen showed
//
//	▌ So the answer is **not** a missing knob — it is a single reader.
//
// with the marked run carrying NO emphasis at all (the attributes of that row were
// one plain run). The failure was invisible to the unit cases because they hand
// the renderer a whole line at once; it needs the cadence window plus a stream
// whose deltas split the marker pair, so it appeared for every delta size up to
// the point where a whole line arrived in ONE flush (1..16 bytes red, 32 green).
//
// The comparison is what makes that carry safe in both directions: a head whose
// text is already in the row is prefixed NOTHING (otherwise the row shows it
// twice), and a head that is not is carried as the spans the row must draw
// (headSpans).
func headIsInRow(rowText, shown string) bool {
	if shown == "" {
		return true
	}
	if rowText == "" {
		return false
	}
	if strings.HasPrefix(rowText, shown) {
		return true
	}
	// A head the stream split in the middle of an emphasis pair has no closing
	// marker, so the tokenizer keeps that marker literal — an unclosed pair must
	// stay literal on screen (see LineSpans) — and `shown` is then WIDER than what
	// the line decided. The row was decided from the whole line, so the head is
	// present in it without the markers: compare the spelling the row would carry
	// had the pair closed, which is the head with its marker characters dropped.
	// (The marker spelling still comes from this file's BoldMarker — the one
	// definition, Rule 1b — never from a literal at the call site.)
	return strings.HasPrefix(rowText, strings.ReplaceAll(shown, BoldMarker, ""))
}

// headSpans is the carried head as the row must DRAW it: the head's own emphasis
// runs, with the markers the tokenizer consumes removed from their text and a
// marker that will never close kept literal.
//
// It is SPANS, not text, because the head's emphasis has to survive the carry.
// Carrying the head as one plain run draws its text unemphasised — the committed
// row reads correctly and the marked run carries no emphasis at all, because the
// tokenizer cannot recover a run from text whose markers it already removed (the
// `**` is the only thing that said `bold conclusion` was bold).
//
// Both halves of the head move together: the drawn TEXT loses the markers AND the
// cell budget the head costs is the drawn text's width, not the spelling's. That
// keeps the one arithmetic honest instead of adding a correction term — a
// `**bold**` head costs the drawn run's cells, not the spelling's (a committed row
// below it was cut with the markers already gone).
func headSpans(head string) []LineSpan {
	if head == "" {
		return nil
	}
	return LineSpans(head)
}

// withHead prefixes a row's text with the head the live row already shows,
// extending the row's SPANS by the same text so the promise "a row's text is its
// spans' text" survives the prefix (see headSpans — the head lands here already
// decided, so the prefix is text the row can draw).
func (row answerRow) withHead(spans []LineSpan) answerRow {
	if len(spans) == 0 {
		return row
	}
	row.text = SpanText(spans) + row.text
	carried := make([]LineSpan, 0, len(spans)+len(row.spans))
	carried = append(carried, spans...)
	row.spans = append(carried, row.spans...)
	return row
}

// HasBoldMarker reports whether text carries the intra-line emphasis marker at
// all — the question a surface that does NOT want the spans asks before it pays
// for them (`gopi ui` rebuilds a row's spans on every streamed delta, and most
// rows have no marker anywhere). The marker's SPELLING stays in this file
// (Rule 1b): a caller never sees what the marker is, only that there is one.
func HasBoldMarker(text string) bool { return strings.Contains(text, BoldMarker) }

// SpanStyle is the ONE mapping from a span's emphasis to the palette's style for
// it — the emphasis layer's counterpart of styleFor, exported because a second
// surface (`gopi ui`) draws the same spans and must reach the same table rather
// than inventing its own bytes (Rule 14: one definition, or the two surfaces
// disagree about what bold looks like).
func SpanStyle(pal Palette, em Emphasis) Style { return styleForSpan(pal, em) }
