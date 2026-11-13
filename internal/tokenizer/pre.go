// pre.go — the file's pre_tokenizer chain: three Split(isolated) stages.
//
// The chain is (tokenizer.json; each stage verified separately against
// tokenizers 0.22.1 — see this run's evidence dir):
//
//	1. Split(\p{N}{1,3}, isolated)
//	2. Split([\u4e00-\u9fa5\u3040-\u309f\u30a0-\u30ff]+, isolated)
//	3. Split(<the big alternation>, isolated)
//	4. ByteLevel(add_prefix_space=false, use_regex=FALSE) — maps bytes, no split
//
// "Isolated" means the MATCHES and the text BETWEEN them are both emitted, in
// order, and nothing is rejoined. That is what makes stage 1 matter: stage 3
// alone on "abc123def" yields "abc","12","3def" (the optional-lead alternative
// eating the "3"), while stage 1 first cuts "123", so the CHAIN gives
// "abc","123","def".
//
// Stage 3 is hand-written rather than regexp because one of its six
// alternatives is `\s+(?!\S)`, a LOOKAHEAD: RE2 has none, and the lookahead is
// not cosmetic. On "  hello" it makes the alternative match ONE space (greedy
// two, lookahead sees a non-space, give back one), leaving " hello" for the
// optional-lead alternative — measured, and exactly the split "  " → "Ġ","Ġhello".
// Collapsing it into `\s+` moves every split boundary of a leading space run, so
// the alternative is implemented literally instead.

package tokenizer

import (
	"fmt"
	"regexp"
	"unicode"
)

// pre holds the two compiled stages. Stage 3's matcher is code (see grouping).
type pre struct {
	digits *regexp.Regexp
	cjk    *regexp.Regexp
}

// stage patterns, as the file writes them.
const (
	digitPattern = `\p{N}{1,3}`
	cjkPattern   = `[\x{4E00}-\x{9FA5}\x{3040}-\x{309F}\x{30A0}-\x{30FF}]+`
)

func compilePre() (pre, error) {
	var p pre
	var err error
	if p.digits, err = regexp.Compile(digitPattern); err != nil {
		return p, fmt.Errorf("compile digit split: %w", err)
	}
	if p.cjk, err = regexp.Compile(cjkPattern); err != nil {
		return p, fmt.Errorf("compile cjk split: %w", err)
	}
	return p, nil
}

// splitPre runs the chain over s and returns its pieces, reusing out's array.
func (p *pre) splitPre(s string, out []string) []string {
	out = append(out[:0], s)
	out = splitStage(p.digits, out)
	out = splitStage(p.cjk, out)
	out = splitGroupingAll(out)
	return out
}

// splitStage applies one regexp Split(isolated) stage to every piece. The output
// is a FRESH slice: writing into pieces[:0] while ranging over pieces would read
// back what the stage just wrote.
func splitStage(re *regexp.Regexp, pieces []string) []string {
	out := make([]string, 0, len(pieces)+1)
	for _, piece := range pieces {
		last := 0
		for _, m := range re.FindAllStringIndex(piece, -1) {
			if m[0] > last {
				out = append(out, piece[last:m[0]])
			}
			out = append(out, piece[m[0]:m[1]])
			last = m[1]
		}
		if last < len(piece) {
			out = append(out, piece[last:])
		}
	}
	return out
}

// splitGroupingAll applies stage 3 to every piece.
func splitGroupingAll(pieces []string) []string {
	out := make([]string, 0, len(pieces)+1)
	for _, piece := range pieces {
		out = splitGrouping(piece, out)
	}
	return out
}

// splitGrouping is stage 3's Split(isolated) on one piece.
//
// The rune index → byte offset table comes from runeBoundaries, which reads each
// rune's width off the ENCODING (utf8.DecodeRuneInString). It must never be
// summed from runeLen: that is the width of a rune VALUE, so every invalid byte
// — one byte in the string, U+FFFD (3 wide) when read back — pushes the table
// two bytes right and the s[offs[i]:offs[j]] below then slices past the end of
// the string. That is the measured `slice bounds out of range` crash this
// function used to raise on a request carrying raw file bytes; see bytes.go's
// runeBoundaries.
func splitGrouping(s string, out []string) []string {
	rs, offs := runeBoundaries(s)

	gap := 0
	i := 0
	for i < len(rs) {
		j := groupingMatch(rs, i)
		if j <= i {
			i++
			continue
		}
		if i > gap {
			out = append(out, s[offs[gap]:offs[i]])
		}
		out = append(out, s[offs[i]:offs[j]])
		gap = j
		i = j
	}
	if gap < len(rs) {
		out = append(out, s[offs[gap]:])
	}
	return out
}

// groupingMatch tries stage 3's six alternatives at i, in the pattern's order,
// and returns the end index of the first that matches (or i for none).
//
//	1 [!\"#$%&'()*+,\-./:;<=>?@\[\\\]^_`{|}~][A-Za-z]+
//	2 [^\r\n\p{L}\p{P}\p{S}]?[\p{L}\p{M}]+
//	3 ` ?[\p{P}\p{S}]+[\r\n]*`
//	4 \s*[\r\n]+
//	5 \s+(?!\S)
//	6 \s+
//
// Each alternative uses greedy semantics with backtracking exactly where the
// pattern's own quantifiers would, and a failed alternative falls through to the
// next (that ordering is the whole of "leftmost-first").
func groupingMatch(rs []rune, i int) int {
	n := len(rs)
	// 1 — an ASCII punctuation byte, then a letter run.
	if isASCIINonAlnum(rs[i]) && i+1 < n && isASCIILetter(rs[i+1]) {
		j := i + 1
		for j < n && isASCIILetter(rs[j]) {
			j++
		}
		return j
	}
	// 2 — an optional non-(CR/LF/L/M/P/S) lead, then a letter/mark run.
	j := i
	if isOptionalLead(rs[i]) && i+1 < n && isLetterOrMark(rs[i+1]) {
		j = i + 1
	}
	if j < n && isLetterOrMark(rs[j]) {
		for j < n && isLetterOrMark(rs[j]) {
			j++
		}
		return j
	}
	// 3 — an optional ONE space, a punctuation/symbol run, then any CR/LF.
	j = i
	if rs[j] == ' ' {
		j++
	}
	if j < n && isPunctOrSymbol(rs[j]) {
		for j < n && isPunctOrSymbol(rs[j]) {
			j++
		}
		for j < n && (rs[j] == '\r' || rs[j] == '\n') {
			j++
		}
		return j
	}
	// 4 — \s*[\r\n]+ : the whole whitespace run, when it ends in CR or LF.
	if e := wsEnd(rs, i); e > i && (rs[e-1] == '\r' || rs[e-1] == '\n') {
		return e
	}
	// 5 — \s+(?!\S) : the run, minus one rune when a non-space follows.
	e := wsEnd(rs, i)
	if e > i {
		if e == n {
			return e
		}
		if e-i >= 2 {
			return e - 1
		}
		// A single whitespace before a non-space: alternative 5 fails, 6 takes it.
		return e
	}
	return i
}

// wsEnd returns the exclusive end of the maximal whitespace run starting at i.
func wsEnd(rs []rune, i int) int {
	j := i
	for j < len(rs) && unicode.IsSpace(rs[j]) {
		j++
	}
	return j
}

// isASCIINonAlnum is the class the pattern's first alternative spells out: every
// printable ASCII byte that is neither a letter nor a digit (ASCII \p{P} ∪ \p{S}).
func isASCIINonAlnum(r rune) bool {
	if r < '!' || r > '~' {
		return false
	}
	return !isASCIILetter(r) && !(r >= '0' && r <= '9')
}

func isASCIILetter(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }

func isLetterOrMark(r rune) bool { return unicode.IsLetter(r) || unicode.IsMark(r) }

func isPunctOrSymbol(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) }

// isOptionalLead is [^\r\n\p{L}\p{P}\p{S}].
func isOptionalLead(r rune) bool {
	if r == '\r' || r == '\n' {
		return false
	}
	return !isLetterOrMark(r) && !isPunctOrSymbol(r)
}
