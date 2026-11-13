// Package film assembles a replay film from an already-captured frame sequence
// and proves the delivered artifact is what it claims to be.
//
// TWO HALVES, ONE PACKAGE, because they answer the same question from opposite
// ends — the assembler says what it produced, the checks say what is on disk:
//
//	Assemble()  — frames/*.png → film.gif (via internal/gifgen, the `gopi tool gif`
//	              backend) + replay.json (the arithmetic) + a readable summary.
//	CheckTraceability / CheckLegibility / CheckMotion / CheckWithholding /
//	CheckColour — the five self-证 criteria of tools/replay/verify.py, ported
//	              into Go so the proof outlives the Python pipeline.
//
// WHY THE CHECKS LIVE BESIDE THE ASSEMBLER (Rule 7, and verify.py's own header):
// a build that verifies itself reports what it produced; these report what is on
// disk, from the artifacts, and can fail on a film another process made. Keeping
// them in one package is what lets the tests drive the REAL produced artifact
// rather than a fixture shaped like one.
//
// THE PORT'S DISCIPLINE. Each ported check cites the verify.py line range it
// came from, in the code, and every divergence from that source is named with
// its reason at the point of divergence. The criteria were frozen verbatim
// before tools/replay was deleted, because a criterion that disappears with its
// script cannot be ported without weakening.
//
// The port's full reasoning — every substitution, every measured count, the
// evidence sets the suite drives and the rejected alternatives — is
// devdoc/film-assembly-and-verification.md, which is the standing description of
// this package.
//
// DEPENDENCIES. stdlib only (image/gif, image/png, image/color, encoding/base64,
// encoding/json, regexp) plus this repo's own packages: verify.py measured every
// frame with numpy+PIL, and the substitute here is Go's own decoders and one
// pass of scalar arithmetic per frame. ActuallyGo.mod is untouched by design
// (Rule 35: zero dependency is a discipline, not a preference).
package film

import (
	"regexp"
	"strings"
)

// ── the shape vocabulary, re-derived from verify.py's own constants ──────────
//
// Every value below is verify.py's, kept together so a divergence is a diff in
// one block rather than an archaeology exercise. The line numbers are from the
// frozen copy and from tools/replay/verify.py at commit c8e71550.

// gutterGlyphs is verify.py L145 (`GUTTER_GLYPHS`): the surface's row markers,
// which it draws where the source has an emphasis marker or a table border.
const gutterGlyphs = "▌⚙▸*`"

// The markdown table's own numbers, verify.py L182-184 (`TABLE_PIPE`,
// `TABLE_FIELD_SEP`, `TABLE_MIN_CELLS`) — internal/lineedit/table.go's
// tablePipe/tableFieldSep/tableMinCells, re-read there because the wrap locator
// has to know where a row breaks.
const (
	tablePipe    = "|"
	tableMinCell = 2
)

// tableDelimiterCells is verify.py's `table_delimiter_row` (L367-L378).
const (
	delimiterDash  = '-'
	delimiterColon = ':'
)

// normaliseSpace is what Python's str.split() joins on in normalize().
const normaliseSpace = " "

// dumpLinesBegin / dumpLinesEnd / dumpLinePrefix are the dump format's framing,
// verify.py L200-202 (and internal/lineedit/screen.go): the grid dump is the
// SURFACE's own self-observation, which is why visibility is measured against it
// and not against the PNG.
const (
	dumpLinesBegin = "lines_begin"
	dumpLinesEnd   = "lines_end"
)

// dumpLinePrefix matches `NNN|row text` — verify.py's `DUMP_LINE_PREFIX`.
var dumpLinePrefix = regexp.MustCompile(`^(\d{3})\|(.*)$`)

// ellipsis is the surface's own cut marker, verify.py L196 / internal/replay's
// Ellipsis (the ONE definition is internal/replay.Ellipsis; this package
// references it in checks.go rather than typing the rune at a use site).
const ellipsis = "…"

// ── surface form: what the SURFACE draws, not what the source wrote ──────────

// surfaceLine is verify.py's `surface_line` (L379-L404): one line's surface
// form, or ok=false when the line is SYNTAX the surface never paints at all.
//
// Which lines carry table geometry: those that BEGIN or END with a pipe — that
// is markdown's own row syntax, and the pipe is the surface's layout, not a
// character it drew. The test is on the line's ENDS rather than on a cell count
// because a line may be a ROW FRAGMENT: the ledger's fixed rune boundary, the
// turn's row budget and the band's own width all cut a table row, and a
// fragment's pipes are no less the surface's layout for having fewer cells.
func surfaceLine(line string) (string, bool) {
	if !(strings.HasPrefix(strings.TrimLeft(line, " \t"), tablePipe) ||
		strings.HasSuffix(strings.TrimRight(line, " \t"), tablePipe)) {
		return line, true
	}
	cells, ok := tableCells(line)
	if ok && tableDelimiterRow(cells) {
		return "", false
	}
	return strings.ReplaceAll(line, tablePipe, normaliseSpace), true
}

// tableCells is verify.py's `table_cells` (L347-L366): a piped row's cells, or
// ok=false when the row is not a table row.
func tableCells(line string) ([]string, bool) {
	parts := strings.Split(line, tablePipe)
	if parts[0] != "" && strings.TrimSpace(parts[0]) != "" {
		return nil, false
	}
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
	}
	if len(parts) < tableMinCell {
		return nil, false
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out, true
}

// tableDelimiterRow is verify.py's `table_delimiter_row` (L367-L378): the
// markdown alignment row (`|---|---|`) is SYNTAX, not painted content.
func tableDelimiterRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		if !strings.ContainsRune(c, delimiterDash) ||
			strings.Trim(c, string(delimiterDash)+string(delimiterColon)) != "" {
			return false
		}
	}
	return true
}

// surface is verify.py's `surface` (L405-L427): what the SURFACE draws for text
// the source wrote as markdown. Applied to BOTH sides of every comparison, so it
// can only make two sides comparable.
func surface(s string) string {
	var keep []string
	for _, ln := range strings.Split(s, "\n") {
		out, ok := surfaceLine(ln)
		if !ok {
			continue
		}
		keep = append(keep, out)
	}
	return strings.Join(keep, "\n")
}

// bare is verify.py's `bare` (L428-L445): a string with the SURFACE's own
// decoration removed, whitespace intact.
//
// Whitespace is deliberately NOT touched here: folding it would destroy the one
// property a probe needs — that it is a PREFIX of the text as it was drawn.
func bare(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, ch := range s {
		if strings.ContainsRune(gutterGlyphs, ch) || ch == '*' || ch == '`' {
			continue
		}
		b.WriteRune(ch)
	}
	return surface(b.String())
}

// normalize is verify.py's `normalize` (L446-L459): the comparable form of a
// WHOLE drawn row or a WHOLE beat's text. Whitespace a wrap introduces is
// collapsed on both sides — the check is about the WORDS, which is the claim,
// and not about where the renderer broke a line.
func normalize(s string) string {
	return strings.Join(strings.Fields(bare(s)), normaliseSpace)
}

// skeleton is verify.py's `skeleton` (L936-L959): the characters that CARRY
// MEANING, in order, with the layout removed (decoration, table geometry and
// whitespace). Every other character is compared, in order, contiguously.
func skeleton(s string) string {
	var b strings.Builder
	for _, ch := range bare(s) {
		if isSpaceRune(ch) {
			continue
		}
		b.WriteRune(ch)
	}
	return b.String()
}

// isSpaceRune is Python's str.isspace() for the runes a terminal film carries.
func isSpaceRune(ch rune) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\v' || ch == '\f' || ch == '\r' ||
		ch == '\u00a0' || ch == '\u2028' || ch == '\u2029' || ch == '\u0085'
}

// ── rune-aware helpers: Python slices by CHARACTER, Go must too ──────────────
//
// Every slicing site in verify.py (probe cut, tail search, `texts[0][k:]`) is a
// rune slice. Working in []rune at those sites is what keeps the port from
// disagreeing with the text it is checking on any CJK or glyph rune the film
// carries.

func runesOf(s string) []rune { return []rune(s) }

func runeLen(s string) int { return len([]rune(s)) }

// prefixRunes is Python's s[:n].
func prefixRunes(s string, n int) string {
	r := []rune(s)
	if n >= len(r) {
		return s
	}
	if n < 0 {
		n = 0
	}
	return string(r[:n])
}

// suffixRunes is Python's s[-n:].
func suffixRunes(s string, n int) string {
	r := []rune(s)
	if n >= len(r) {
		return s
	}
	if n < 0 {
		n = 0
	}
	return string(r[len(r)-n:])
}

// fromRune is Python's s[k:].
func fromRune(s string, k int) string {
	r := []rune(s)
	if k >= len(r) {
		return ""
	}
	if k < 0 {
		k = 0
	}
	return string(r[k:])
}

// runeHasPrefix reports Python's s.startswith(p).
func runeHasPrefix(s, p string) bool { return strings.HasPrefix(s, p) }
