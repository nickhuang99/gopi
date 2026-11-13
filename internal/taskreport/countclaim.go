package taskreport

// countclaim.go — "the file carries N rows/lines" as a REMEASURABLE claim, never
// as a fact.
//
// THE MEASURED GAP THIS FILE EXISTS FOR (epic row 01a13781, 2026-10-14). A bug
// row's briefing reported a MEASUREMENT — "the table carries **168 rows** while
// the tree produces **167** incoherent anchor rows", reddening three
// internal/docclass tests on a clean tree at HEAD bbf8d9760 — and named the
// revision its measurements were taken at (base bbf8d9760). By the time the row
// was dispatched the premise was already repaired: the census and all three
// tests were GREEN at that very base, so the dispatched run paid to re-derive a
// measurement that was already green.
//
// The dispatch seam re-measured the declaration it COULD read — the base
// revision (task_briefing_premise.go's TaskBriefingPremiseBaseRefresh) — and
// that half is not silent: driven over the incident's own received prompt it
// produced a 978-character stamp. But the stamp's own positive control is the
// hole: a base that IS the integration tip stamps NOTHING
// (`if ahead == 0 { … continue }`, deliberate), so a briefing whose base
// declaration is CURRENT — or absent — while its measurement prose has gone
// stale reaches the child with no re-measure instruction at all.
//
// WHY NOTHING ELSE COVERS IT. internal/taskreport's other scanners key on
// ASSERTION PHRASES (unlandedclaim.go: "never landed", "not an ancestor") or on
// revisions standing beside a base word (baseclaim.go). A population figure
// asserts no phrase and names no revision, so it fell through every predicate in
// this package.
//
// THE PREDICATE IS DELIBERATELY THE NARROWEST ONE THAT CATCHES THE MEASURED
// SHAPE, because its false-positive surface is "every briefing that mentions a
// number" and a stamp on an honest briefing is worse than no stamp (the seam's
// positive control exists for exactly that reason):
//
//  1. a claim needs a COUNT DIGIT-ADJACENT to a counted UNIT ("168 rows",
//     "168 row(s)", "3 lines") — never merely two numbers on a line, and never a
//     number that is separated from the unit by prose ("252 anchor rows" is
//     therefore NOT a claim: the nearest count is another number entirely).
//     Adjacency is what keeps "the fix landed after three attempts, 41 commits
//     behind" from firing.
//  2. the count must not be REPORTED PAST-TENSE ("carried", "was", "had",
//     "measured"): the author is quoting what a TOOL printed at a past revision,
//     which is exactly the reading the seam must not re-stamp as a live claim.
//     Present tense — "carries", "has", "contains" — is the live-claim shape.
//  3. the line must name a FILE-LIKE token. This is the strongest filter of the
//     three and the reason the leaf never needs the filesystem: a figure that
//     names no file cannot be re-measured by the caller at all, and a claim
//     whose measurement is impossible must not be produced (Rule 15 — the caller
//     states it as UNMEASURED, it is never silently dropped here because the
//     token's existence is the caller's question to answer).
//
// The leaf stays PURE (no IO): it names the SHAPES, the caller decides what is
// re-measurable and what the figure means. That split is load-bearing — "the
// file exists in this repo" and "the file has N lines" are facts about a
// checkout, and this package has no checkout (Rules 8/13: the caller already
// holds the repo handle, and a second resolver here would be a second answer).

import "strings"

const (
	// countClaimMaxClaims caps how many figures a briefing yields. The stamp
	// quotes each claim, and a briefing with a systemic problem must not push
	// the child's prompt past what a reader reads (baseClaimMaxClaims'
	// discipline).
	countClaimMaxClaims = 6

	// countClaimLineMaxRunes bounds the line a claim quotes.
	countClaimLineMaxRunes = 200

	// countClaimSentenceWindow bounds how far apart a count and its unit may
	// stand and still be read as one phrase once markup is stripped. It is the
	// window that makes "168 rows", "168 `rows`" and "168 **rows**" one phrase
	// while leaving "168, and the rows" two.
	countClaimSentenceWindow = 2
)

// countClaimUnits are the counted units a briefing uses for a FILE POPULATION —
// the only quantities the caller can re-measure from a checkout. "tests",
// "commits" and "characters" are deliberately absent: they name no file, so no
// re-measurement exists and a claim about them could only ever be noise.
//
// Both numbers are carried because this repo's briefings mix them freely in one
// sentence (the measured text says "carries **168 rows**" and "produces **167**
// incoherent anchor rows" and "168 row(s)" — the same figure spelled three ways).
var countClaimUnits = map[string]bool{
	"row":     true,
	"rows":    true,
	"line":    true,
	"lines":   true,
	"permit":  true, // "1 permit(s) in <file>" — the incident's own error line
	"permits": true,
}

// countClaimMarkupRunes are the emphasis/quote runes that may stand BETWEEN a
// count and its unit inside one phrase ("**168 rows**", "`168` rows"). They are
// stripped from the window, never from the quoted line: the stamp quotes the
// author's own spelling (baseClaimLineExcerpt's rule).
var countClaimMarkupRunes = "`*_"

// countClaimDeltaWords are the CHANGE words that make a figure a DELTA rather
// than a population — "0 permit(s) added, 168 permit(s) removed", "would write
// 1 row(s)", "+1 row", "branch rewrites 51 lines".
//
// THIS LIST IS MEASURED, NOT IMAGINED (probe over the ledger's own 400 most
// recent payloads, 2026-10-14, this epic's run): of 8 firings, 5 were delta or
// plan sentences and NONE of them is a population of a file — re-measuring
// "0 permits added" by counting the file's permits produces a number the author
// never asserted, and a stamp built on it is a false positive on an honest
// briefing. Dropping the shape costs one true positive over that corpus
// ("branch rewrites 51 lines", itself a delta about a file); keeping it costs
// every writer's "wrote N row(s)" line.
var countClaimDeltaWords = map[string]bool{
	"added":    true,
	"removed":  true,
	"removing": true,
	"adding":   true,
	"wrote":    true,
	"write":    true,
	"written":  true,
	"rewrites": true,
	"rewrote":  true,
	"delta":    true,
	"delta:":   true,
	"diff":     true,
	"new":      true,
}

// countClaimDeltaWindow is how many raw runes either side of a count+unit phrase
// are searched for a delta word: the phrases in the measured corpus put it
// immediately after the unit ("168 permit(s) removed") or immediately before the
// count ("would write 1 row(s)"), so a small window is enough and a wide one
// would start eating neighbouring clauses.
const countClaimDeltaWindow = 24

// countClaimPastTenseWords are the past-tense REPORTING verbs that put a figure
// in the past — the voice that says "a tool printed this at revision R", which
// is the reading the base clause already owns. A figure reported this way is not
// this scanner's subject (rule 2 of the header).
var countClaimPastTenseWords = map[string]bool{
	"carried":  true,
	"was":      true,
	"were":     true,
	"had":      true,
	"measured": true,
	"reported": true,
	"printed":  true,
	"said":     true,
	"scan":     true, // "scanned"
	"scanned":  true,
	"found":    true,
}

// CountClaim is one population figure a briefing asserts about a file.
type CountClaim struct {
	// Line is the briefing line the figure was read from, verbatim.
	Line string
	// Count is the figure the author wrote.
	Count int
	// Unit is the counted unit that figure was attached to ("rows", "line",
	// "permit") — carried so the record and the stamp can name what was counted
	// rather than assert a number in the abstract.
	Unit string
	// File is the file-like token standing on the same line: the thing the
	// figure is about, and the thing the caller must resolve to re-measure it.
	File string
}

// CountClaimsIn scans a briefing body for "this file carries N <unit>" figures.
//
// It reads the whole body, like BaseClaimsIn and unlike the three
// section-scoped readers beside it, and for the same measured reason: an
// author's figures live where the author put the evidence, which in the
// incident's text is a Reproduction section and a failing-test transcript.
//
// An empty result is the ordinary case (most briefings assert no population
// figure about a file) and is the reason this predicate cannot degrade every
// briefing the seam touches.
func CountClaimsIn(body string) []CountClaim {
	var claims []CountClaim
	seen := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		c, ok := countClaimIn(line)
		if !ok {
			continue
		}
		key := c.File + "\x00" + itoa(c.Count)
		if seen[key] {
			continue
		}
		seen[key] = true
		claims = append(claims, c)
		if len(claims) >= countClaimMaxClaims {
			break
		}
	}
	return claims
}

// CountClaimLineExcerpt quotes a figure for a record or a stamped line, bounded
// so the record stays a record.
func CountClaimLineExcerpt(line string) string {
	s := strings.TrimSpace(line)
	r := []rune(s)
	if len(r) <= countClaimLineMaxRunes {
		return s
	}
	return string(r[:countClaimLineMaxRunes]) + baseClaimEllipsis
}

// countClaimIn is the ONE predicate over one line.
func countClaimIn(line string) (CountClaim, bool) {
	file := countClaimFileIn(line)
	if file == "" {
		return CountClaim{}, false
	}
	count, unit, at, ok := countClaimFigureIn(line)
	if !ok {
		return CountClaim{}, false
	}
	// The figure and the file must belong to the same assertion. Measured: the
	// incident's own sentence names the file BEFORE the figure ("the table
	// `…anchor-number-baseline.tsv` carries **168 rows**") in one shape and
	// AFTER it in another ("table carries 167 row(s) in devdoc/DOC-INDEX.tsv"),
	// so DIRECTION is not a usable rule — a proximity bound is. The caller's
	// re-measurability filter then does the rest: a figure attributed to a file
	// that does not exist is reported UNMEASURED, never stamped.
	if !countClaimFileNear(line, file, at) {
		return CountClaim{}, false
	}
	return CountClaim{Line: strings.TrimSpace(line), Count: count, Unit: unit, File: file}, true
}

// countClaimFileNear reports whether the file token stands within
// countClaimFileWindow bytes of the unit phrase's start — the bound that keeps
// attribution local while leaving the author's own word order alone.
func countClaimFileNear(line, file string, unitAt int) bool {
	i := strings.Index(line, file)
	if i < 0 {
		return false
	}
	d := i - unitAt
	if d < 0 {
		d = -d
	}
	return d <= countClaimFileWindow
}

// countClaimFileWindow is how far a file token may stand from the figure it is
// attributed to. It is measured, not guessed: over the ledger's 400 most recent
// payloads (2026-10-14) the true positives all sit within ~40 bytes, while the
// false ones the ordering rule was trying to exclude were already excluded by
// the delta/past/zero rules above.
const countClaimFileWindow = 160

// countClaimFigureIn finds the FIRST count-digit-adjacent unit phrase on the
// line whose count is not reported in the past tense, returning the figure, the
// unit, and the byte offset the unit stood at (the caller uses the offset to
// attribute the file token to the same clause).
func countClaimFigureIn(line string) (count int, unit string, at int, ok bool) {
	low := strings.ToLower(line)
	for i := 0; i < len(low); i++ {
		if !isASCIIDigit(low[i]) {
			continue
		}
		// Walk back to the start of this digit run, then of any markup glued
		// to it, so the reported count is the whole number.
		j := i
		for j > 0 && isASCIIDigit(low[j-1]) {
			j--
		}
		n, nOk := atoi(low[j : i+1])
		if !nOk {
			continue
		}
		k := i + 1
		// Markup may stand between the count and its unit ("168 **rows**").
		probe := k
		for probe < len(low) && (low[probe] == ' ' || innerRuneIn(low[probe], countClaimMarkupRunes)) {
			probe++
		}
		if probe-k > countClaimSentenceWindow {
			i = k - 1
			continue
		}
		u, end := countClaimUnitAt(low, probe)
		if u == "" {
			i = k - 1
			continue
		}
		if countClaimPastTenseBefore(low, j) {
			i = end - 1
			continue
		}
		// A SIGNED figure is a delta, not a population: "+1 row". The sign
		// stands immediately before the digits, so it is read at the count, not
		// from a window (measured shape 01a12e63 — "(-1 row)"/"(+1 row)" lists).
		if countClaimSignedAt(low, j) {
			i = end - 1
			continue
		}
		// Rule 2 of the header, refined by the corpus measurement: a figure that
		// is a DELTA or a PLAN is not a population, and re-measuring it by
		// counting the file would stamp a number the author never asserted.
		if countClaimDeltaNear(low, j, end) {
			i = end - 1
			continue
		}
		// A ZERO figure is never a population claim in the measured corpus: it is
		// a delta ("0 permit(s) added"), a scan line ("0 incoherent"), or an
		// empty-file restatement whose re-measurement instructs the child to do
		// nothing. Requiring a positive figure is what removes them, and the cost
		// is a claim no run would have acted on (Rule 1: the bound is named, not
		// inferred at the site).
		if n < countClaimMinCount {
			i = end - 1
			continue
		}
		return n, u, probe, true
	}
	return 0, "", 0, false
}

// countClaimMinCount is the smallest figure this scanner will call a population
// claim (see the zero rule at the call site).
const countClaimMinCount = 1

// countClaimSignedAt reports whether the digit run starting at j carries a '+'
// or '-' sign immediately before it (allowing the markup runes an author writes
// around the figure).
func countClaimSignedAt(low string, j int) bool {
	k := j
	for k > 0 && (low[k-1] == ' ' || innerRuneIn(low[k-1], countClaimMarkupRunes)) {
		k--
	}
	return k > 0 && (low[k-1] == '+' || low[k-1] == '-')
}

// countClaimDeltaNear reports whether a change word stands within
// countClaimDeltaWindow runes either side of the count+unit phrase
// [from,to) — the measured shape of a delta/plan figure rather than a
// population ("...168 permit(s) removed", "would write 1 row(s)").
func countClaimDeltaNear(low string, from, to int) bool {
	lo := from - countClaimDeltaWindow
	if lo < 0 {
		lo = 0
	}
	hi := to + countClaimDeltaWindow
	if hi > len(low) {
		hi = len(low)
	}
	for _, w := range strings.FieldsFunc(low[lo:hi], func(r rune) bool {
		return !baseClaimLatinWordRune(r)
	}) {
		if countClaimDeltaWords[w] {
			return true
		}
	}
	return false
}

// countClaimUnitAt returns the counted unit starting at offset at, together with
// the offset just past it. A unit must be bounded on the right by a non-letter,
// so "rows" matches while "rowspan" does not.
func countClaimUnitAt(low string, at int) (string, int) {
	for end := at + 1; end <= len(low) && end-at <= 8; end++ {
		w := low[at:end]
		if !countClaimUnits[w] {
			continue
		}
		if end < len(low) && baseClaimLatinWordRune(rune(low[end])) {
			continue
		}
		return w, end
	}
	return "", at
}

// countClaimPastTenseBefore reports whether the words immediately before the
// figure put it in the past ("the table carried 168 rows"). It reads at most
// countClaimPastTenseLookback words back and stops at a clause boundary, so a
// past-tense verb belonging to a neighbouring clause does not silence a live
// figure.
func countClaimPastTenseBefore(low string, at int) bool {
	head := low[:at]
	fields := strings.FieldsFunc(head, func(r rune) bool {
		return !baseClaimLatinWordRune(r)
	})
	// The last field is the fragment the count was glued to (possibly empty
	// after trailing markup); the REPORTING VERB is behind it.
	for i := len(fields) - 1; i >= 0 && i >= len(fields)-3; i-- {
		if countClaimPastTenseWords[fields[i]] {
			return true
		}
	}
	return false
}

// countClaimFileIn returns the file-like token on the line — the strongest
// filter this predicate has, and the only one that makes the claim re-measurable
// at all. A token is file-like when it carries a path separator or a dot and
// contains no whitespace; trailing punctuation the author wrote for grammar
// (",", ";", ")", "`", ".") is trimmed so the token is the path itself.
//
// It deliberately does NOT check the filesystem: that is the caller's question,
// and the leaf has no checkout.
func countClaimFileIn(line string) string {
	best := ""
	for _, raw := range strings.Fields(line) {
		tok := strings.Trim(raw, "`*_\"'()[]<>,;:!?")
		for len(tok) > 0 && strings.HasSuffix(tok, ".") {
			tok = tok[:len(tok)-1]
		}
		if tok == "" || !countClaimFileShaped(tok) {
			continue
		}
		// The LONGEST shaped token wins: a line naming a directory and a file
		// ("in internal/docclass and its testdata/x.tsv") is about the file.
		if len(tok) > len(best) {
			best = tok
		}
	}
	return best
}

// countClaimFileShaped reports whether tok can be a path in a checkout.
func countClaimFileShaped(tok string) bool {
	if strings.ContainsAny(tok, " \t") {
		return false
	}
	if !strings.Contains(tok, "/") && !strings.Contains(tok, ".") {
		return false
	}
	// A token with no LETTER is not a path at all, it is punctuation or a
	// number: the bare separator a wrapped line's tail leaves behind ("… 167
	// row(s)` /" — measured on this epic's own incident payload) passes every
	// other test here, and a claim whose "file" is "/" has no re-measurable
	// subject — the caller could only report it UNMEASURED. Rule 3 of the header
	// says a claim names a FILE, so a separator is refused at the predicate.
	if !countClaimFileStem(tok) {
		return false
	}
	// A bare "1.5" or "v0.1.0" is a number or a version, not a path: a path
	// carrying a dot must have a non-numeric stem on BOTH sides of it.
	if strings.IndexFunc(tok, func(r rune) bool { return r == '/' }) < 0 {
		dot := strings.Index(tok, ".")
		if dot <= 0 || dot == len(tok)-1 {
			return false
		}
		if !countClaimFileStem(tok[:dot]) || !countClaimFileStem(tok[dot+1:]) {
			return false
		}
	}
	return true
}

// countClaimFileStem reports whether s is a plausible path: at least one letter,
// and only path-ish runes across the WHOLE token (separators included).
func countClaimFileStem(s string) bool {
	letters := 0
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			letters++
		case r >= '0' && r <= '9', r == '-', r == '_', r == '.', r == '/':
		default:
			return false
		}
	}
	return letters > 0
}

// isASCIIDigit reports whether b is '0'..'9'.
func isASCIIDigit(b byte) bool { return b >= '0' && b <= '9' }

// innerRuneIn reports whether b is one of the runes in set.
func innerRuneIn(b byte, set string) bool { return strings.IndexByte(set, b) >= 0 }

// atoi parses a digit run of at most countClaimMaxDigits digits; a longer run is
// refused rather than overflowed (a briefing's figure is never 19 digits).
func atoi(s string) (int, bool) {
	if s == "" || len(s) > countClaimMaxDigits {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if !isASCIIDigit(s[i]) {
			return 0, false
		}
		n = n*10 + int(s[i]-'0')
	}
	return n, true
}

// itoa renders a small non-negative int without importing strconv into this
// leaf (the package's other files keep it stdlib-minimal; strconv is stdlib too,
// but one renderer here means the claim key cannot be spelled two ways).
//
// int64DecimalDigits is the scratch buffer's width: the most decimal digits an
// int64 can need (Rule 1 — a size taken from its defining fact, not typed).
const int64DecimalDigits = 20

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [int64DecimalDigits]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// countClaimMaxDigits bounds a figure this scanner will read.
const countClaimMaxDigits = 7
