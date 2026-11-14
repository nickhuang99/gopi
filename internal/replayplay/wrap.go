package replayplay

// wrap.go — THE ONE WRAPPER FOR A ROW THAT CARRIES A MARK AT COLUMN 0, and the
// place this package stops owning a wrap policy of its own.
//
// THE DEFECT THE MARK HALF EXISTS FOR (measured 2026-10-28, task 01a18269,
// session 93043087 — a WORKER run, so the material is the session log and there
// is no screen dump to be cell-exact about). The replay laid a row's text out at
// the FULL canvas width and only THEN prefixed its continuation rows with the
// block's mark (`↳ `, roundResultLabel). The prefix therefore pushed the wrapped
// tail two cells past the canvas, and the next step — fitCells — silently dropped
// exactly those cells. Nothing said so: fitCells cannot tell "the row is over
// wide" from "the text is longer than the row", so it cut the TEXT.
//
// Measured at 200 columns on the session above:
//
//	↳ /home/user/workspace/01a17e74-…-393e636d3fce carries no commit to
//	↳ thing to push. This is NOT a refused pull and NOT a conflict — …
//
// The sentence's own words are "… carries no commit to deliver: … so there is
// nothing to pull and nothing to push." The "no" between the two rows existed in
// NO row at all, and `and`/`thing` read as if the text had been hard-chopped —
// which is exactly the symptom an operator reported as "truncated sentences".
// At the 80-column fallback the same defect bites every continuation row, so a
// worker-run replay looked uniformly truncated.
//
// AND THE DEFECT THE POLICY HALF EXISTS FOR (measured 2026-11-09, task
// 01a1c04f — this file's own reason to change). Fixing where the mark is put
// still left this package with a SECOND answer to "where does a row end": the
// old wrapTake broke at whitespace or at one of a private separator set
// ("/-._=:," — a mirror of internal/lineedit's list, and not the same one), while
// the live chat screen breaks through internal/lineedit's WrapToCols
// (whitespace, `/-_?&=,;)]`, or between two wide runes). Two rules drew the same
// content, so a word was whole on the live screen and chopped in the replay:
//
//	  的含义：实测gopi session replay … → header calls=286，正文^⚙ 行数= 286，一
//	  eader calls=8 正文0 个⚙；sessi…
//
// (`header` lost its `h`, `session` became `sessi`.) The rule below is therefore
// ONE rule in ONE place (Rule 14): a row's mark is part of the row, so it comes
// OUT of the wrap width — and the wrap itself is internal/lineedit.WrapToCols,
// the very function the live screen lays its own rows out with. This package
// contributes only the MARK arithmetic; it owns no break character.

import (
	"strings"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// wrapPrefixed lays text out as rows of at most cols CELLS. The first row of
// every paragraph wears firstPrefix and its continuations wear contPrefix; both
// prefixes are taken OUT of the wrap width, so a row plus its mark always fits
// the canvas and the text is never cut to make room for the mark.
//
// Explicit newlines are preserved (a paragraph each), and an empty paragraph is
// a row of its own — the shape a caller that counts rows already expects.
//
// WHERE THE BREAKS FALL IS NOT DECIDED HERE. Each row is laid out by wrapOneRow,
// which asks internal/lineedit.WrapToCols (see wrapOneRow): a break lands at the
// last word boundary before the edge, exactly as on the live screen, and only a
// token longer than a whole row falls back to a cell cut.
//
// A DEGENERATE CANVAS LOSES NO TEXT (Rule 15): when the mark alone would fill the
// row (cols <= the mark's own cells — a 1-cell canvas, a caller's --cols 1), the
// mark is dropped and the text wraps at the full canvas instead of printing
// nothing or eating a character per row.
func wrapPrefixed(text, firstPrefix, contPrefix string, cols int) []string {
	if cols <= 0 {
		return nil
	}
	var out []string
	for _, para := range strings.Split(text, "\n") {
		// EVERY row after the text's OWN first one wears contPrefix — including
		// the first row of a later paragraph. That is the discipline this
		// replaced (a mark per wrapped row), and it is what tells a reader that
		// a row at column 0 belongs to the block above it rather than being a
		// new one; the marks are simply taken out of the width now.
		mark, rest := contPrefix, para
		if len(out) == 0 {
			mark = firstPrefix
		}
		for {
			room := cols - cellsOf(mark)
			if room < 1 {
				mark, room = "", cols
			}
			head, tail := wrapOneRow(rest, room)
			out = append(out, mark+head)
			if tail == "" {
				break
			}
			rest, mark = tail, contPrefix
		}
	}
	return out
}

// wrapOneRow splits ONE row of text: the head that fits in room CELLS and the
// tail the next row carries.
//
// The break policy is internal/lineedit.WrapToCols — the function the live chat
// screen wraps its own answer and note rows with — and this function adds
// nothing to it but the row/tail split. That delegation is the whole point (Rule
// 14): a second implementation of "where does a row end" drifts from the first
// the moment either side changes, and the measured drift is what produced
// `header` → `eader` in the replay while the live screen had it whole.
//
// WrapToCols EXPORTS its reassembly invariant in its own doc comment: the rows
// it answers carry every byte of the source once, in order, the only thing added
// being the row breaks. So joining the rows past the first back together is the
// tail verbatim — no separator is consumed and none is added, and this package
// never has to know which character a row ended after.
//
// TWO CASES ARE NOT A WRAP AT ALL and are answered without calling it: an empty
// row (an empty paragraph is a row of its own, not zero rows) and a canvas that
// fits everything, where WrapToCols answers the text back as ONE row.
func wrapOneRow(rest string, room int) (head, tail string) {
	if rest == "" {
		return "", ""
	}
	rows := lineedit.WrapToCols(rest, room)
	if len(rows) <= 1 {
		return rest, ""
	}
	return rows[0], strings.Join(rows[1:], "")
}

// cellsOf is how many terminal CELLS s occupies — the measure a canvas is
// counted in, never bytes and never runes (a CJK rune spends two cells). It is
// internal/lineedit.DisplayWidth, which is that measurement's one definition
// (it also spends nothing on an SGR sequence); a local sum of runeCells would be
// a second answer to the same question (Rule 14).
func cellsOf(s string) int {
	return lineedit.DisplayWidth(s)
}
