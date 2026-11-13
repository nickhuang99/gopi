package taskreport

// driftclaim.go — "this briefing pins a SET OF PATHS, together with a COUNT, as
// the population to work off" — the claim shape whose SUBJECT IS ALIVE.
//
// THE MEASURED CASE (row 01a1447c-253e-74a2-8329-1c37447c9e59, 2026-10-16). That
// row's briefing pinned twelve paths as the stored-cell drift population of
// devdoc/DOC-INDEX.tsv and made "exactly these twelve rows change" an ACCEPTANCE
// criterion (`git diff --stat -- devdoc/DOC-INDEX.tsv` 的变化行数与 12 行一致).
// The twelve paths were a SINGLE SNAPSHOT taken by the previous row at 19:11 —
// and the sibling commit cc9c1fee8 (authored 18:16, merged 19:34) had already
// re-derived every one of them. Measured on the row's own base: ten of the twelve
// were compliant and the tree's real drift population was a DIFFERENT set of
// twelve, two of which were class moves that cannot be cleared by re-deriving a
// cell at all. So the acceptance criterion was UNREACHABLE at that base, and the
// dispatched run had to find that out for itself before it could start.
//
// WHY THE THREE SHIPPED SCANNERS ARE STRUCTURALLY BLIND TO IT, measured on the
// row's own briefing text (probe output in this run's session artifacts):
//
//   - CountClaimsIn (countclaim.go) requires a count DIGIT-ADJACENT to an
//     ENGLISH counted unit (row/rows/line/lines/permit/permits) AND a file-like
//     token on the same line. The incident's text counts in Chinese (`12 行`,
//     `12 条`) and its path list is a multi-line `for` loop, so the scanner
//     returned 0 — the figure clause appended nothing, and the run received no
//     instruction at all.
//   - BaseClaimsIn (baseclaim.go) fires on a base word beside a commit-shaped
//     token. It DID fire here (the text declares `base 1c5b086`) and the base
//     clause did stamp — but that clause's instruction is generic ("run THAT
//     surface again"), because a base behind the tip is all it can see. It never
//     says WHICH surface, never says the population is a SET, and never warns
//     that the set itself is what moved.
//   - GateAssertionsIn (gateassert.go) fires on a RED verdict word followed by
//     the integration branch's name. This text asserts no verdict on master.
//
// WHAT THIS FILE IS. A leaf scanner over ONE line: a SET-COUNT phrase (`12 行`,
// `12 条`, `12 row(s)`, `3 paths`) standing on a line that also names at least
// one path-shaped token and is SHAPED as an enumeration (two or more path tokens,
// or an enumeration hint — `：`, `、`, `paths`, `set`, `list`). It measures
// nothing, resolves nothing and judges nothing: whether those paths exist in the
// ground, what the surface reports today, and what the clause may conclude are
// the caller's questions (Rules 8/13 — one resolver, and it is internal/tools').
//
// IT REUSES THE PACKAGE'S OWN PREDICATES rather than growing a second vocabulary:
// the English figure shape, the delta/past-tense exclusions, the digit parser and
// the path-shaped-token test are countclaim.go's (countClaimFigureIn,
// countClaimDeltaNear, countClaimFileShaped, atoi, countClaimMaxDigits). Only the
// CJK counted units and the enumeration shape are new here — the two things no
// existing predicate reads.
//
// NARROW ON PURPOSE: the false-positive surface of a rule about "a number next to
// a path" is enormous (every evidence line, every `--only <path>` command), and a
// stamp on an honest briefing is worse than the silence this file fixes — the
// same asymmetry baseclaim.go and gateassert.go were built under. Hence the
// enumeration requirement: one path beside a count is a figure ABOUT a file; two
// or more under an enumeration hint is a POPULATION.

import "strings"

const (
	// pathSetClaimMaxClaims caps how many set-claims a briefing yields. The clause
	// quotes each one, and a briefing with a systemic problem must not push the
	// child's prompt past what a reader reads (baseClaimMaxClaims' discipline).
	pathSetClaimMaxClaims = 4

	// pathSetClaimLineMaxRunes bounds the line a claim quotes.
	pathSetClaimLineMaxRunes = 200

	// pathSetClaimMinCount is the smallest figure this scanner reads as a SET
	// count: "0 paths" is never a population an acceptance criterion is written
	// against (countclaim.go's zero rule, same reason).
	pathSetClaimMinCount = 1
)

// pathSetClaimCJKUnits are the counted units this repo's Chinese briefings attach
// to a path population. Latin units are deliberately absent: countClaimUnits
// already carries them, and a second spelling of "rows" here would be the
// duplicate vocabulary Rules 1/13 forbid.
var pathSetClaimCJKUnits = []string{
	"条",  // 12 条路径 — "items", the default counter
	"行",  // 12 行 — rows/lines
	"个",  // 12 个单元格 — cells/units
	"项",  // 12 项 — entries
	"路径", // 12 路径 — paths
	"处",  // 12 处 — places
}

// PathSetClaim is one "N <unit> of paths" population a briefing pins.
type PathSetClaim struct {
	// Line is the briefing line the claim was read from, verbatim.
	Line string
	// Count is the figure the author wrote — the snapshot's size, and the number
	// an acceptance criterion is usually written against.
	Count int
	// Unit is the counted unit that figure was attached to ("行", "条", "row(s)").
	Unit string
	// Paths are the path-shaped tokens the LIST BLOCK names, in the order they
	// stand. At least pathSetClaimMinBlockLines are guaranteed by the predicate.
	Paths []string
}

// PathSetClaimsIn scans a briefing body for pinned path-set populations.
//
// THE SHAPE IT READS IS THE ONE THE INCIDENT ACTUALLY HAS, and that shape is not
// one line: the count stands in prose ("用 manifest 的唯一写入者逐行重生成这 12 行")
// and the paths are a LIST BLOCK below it — twelve consecutive lines carrying one
// path-shaped token each (the `for p in …` enumeration). A line-scoped rule was
// tried first and measured WRONG on the incident's own text: it fired on a
// parenthesis that named a command and a file ("`./bin/gopi tool doccheck` …
// evidence/doccheck-before.txt）") and reported a one-path "set" that is not the
// pin at all. The block is what makes the enumeration legible, and the count is
// recovered from a bounded look-behind.
//
// An empty result is the ordinary case — most briefings pin no path set — and is
// why this cannot degrade every briefing the seam touches.
func PathSetClaimsIn(body string) []PathSetClaim {
	lines := strings.Split(body, "\n")
	var claims []PathSetClaim
	seen := map[string]bool{}
	for i := 0; i < len(lines); i++ {
		if len(pathSetClaimPathsIn(lines[i])) != pathSetClaimOnePath {
			continue
		}
		block, end := pathSetClaimBlockAt(lines, i)
		if len(block) < pathSetClaimMinBlockLines {
			continue
		}
		count, unit, ok := pathSetClaimCountAbove(lines, i)
		if !ok {
			// The list block is real but nothing above it counts it: a set
			// nobody quantified is not the claim this clause stamps (an
			// acceptance criterion is written against a FIGURE).
			i = end - 1
			continue
		}
		key := itoa(count) + "\x00" + strings.Join(block, ",")
		if !seen[key] {
			seen[key] = true
			claims = append(claims, PathSetClaim{
				Line:  countLineText(lines, i),
				Count: count,
				Unit:  unit,
				Paths: block,
			})
		}
		i = end - 1
		if len(claims) >= pathSetClaimMaxClaims {
			break
		}
	}
	return claims
}

// pathSetClaimOnePath is how many path-shaped tokens make a line a LIST ENTRY:
// exactly one. A line naming two paths is prose ABOUT paths, not an entry in a
// list of them — the measured discriminator that keeps the incident's own
// parenthesis ("`./bin/gopi tool doccheck` … evidence/doccheck-before.txt）") from
// being read as a set member.
const pathSetClaimOnePath = 1

// pathSetClaimMinBlockLines is how many consecutive list-entry lines make a LIST
// BLOCK. Two is the smallest run that is an enumeration by construction: a
// single one-path line is prose (or a plan line) far more often than it is a set.
const pathSetClaimMinBlockLines = 2

// pathSetClaimMaxBlockLines bounds the block this scanner will read — a briefing
// quoting a longer list than this is reported as far as this bound reaches, and
// the elision is the clause's own STATED bound (task_briefing_path_set.go's
// taskBriefingPathSetMaxPaths), never a silent truncation (Rule 15).
const pathSetClaimMaxBlockLines = 32

// pathSetClaimCountLookbackLines bounds how far ABOVE a list block its count may
// stand. It is measured, not guessed: in the incident's text the count sits five
// lines above the block (a fence and a blank line intervene), while a count two
// paragraphs up belongs to another subject.
const pathSetClaimCountLookbackLines = 8

// pathSetClaimBlockAt collects the contiguous run of list-entry lines starting at
// start, returning the entries and the index one past the run's end.
func pathSetClaimBlockAt(lines []string, start int) ([]string, int) {
	var entries []string
	i := start
	for ; i < len(lines) && i-start < pathSetClaimMaxBlockLines; i++ {
		p := pathSetClaimPathsIn(lines[i])
		if len(p) != pathSetClaimOnePath || !pathSetClaimEntryLine(lines[i], p[0], i == start) {
			break
		}
		entries = append(entries, p[0])
	}
	return entries, i
}

// pathSetClaimEntryLine reports whether a one-path line is a list ENTRY rather
// than a command that happens to name a file.
//
// The measured discriminator is what is LEFT of the path once the shell's own
// list punctuation is set aside: an entry is the path (optionally with a
// continuation or a separator after it), while a command carries arguments BEFORE
// it.
//
// devdoc-ref-ok: the paths in the next three lines quote the INCIDENT's text
// (a shell listing), not citations of documents.
//
// In the incident's text the entries are `devdoc/…md \` and the loop's last is
// `devdoc/…md; do`, whereas the body's `python3 tools/docclass/classify.py . --only
// devdoc/one.md` has four fields in front of the path — which is why the block
// stops there instead of counting the classifier as a member. Measured: the first
// implementation read that body line as a thirteenth member and reported a set of
// four at the wrong end.
//
// The `for <var> in ` header is the ONE prefix allowed, and only on the block's
// first line: it is how the list is introduced, and a second one inside a block
// would be a second list.
func pathSetClaimEntryLine(line, path string, first bool) bool {
	f := strings.Fields(strings.TrimSpace(line))
	for len(f) > 0 && pathSetClaimTrailerField(f[len(f)-1]) {
		f = f[:len(f)-1]
	}
	if len(f) == 0 {
		return false
	}
	if pathSetClaimFieldPath(f[len(f)-1]) != path {
		return false
	}
	head := f[:len(f)-1]
	if len(head) == 0 {
		return true
	}
	return first && len(head) == pathSetClaimLoopHeadFields &&
		head[0] == pathSetClaimLoopKeyword && head[pathSetClaimLoopHeadFields-1] == pathSetClaimLoopIn
}

// pathSetClaimFieldPath is the path a field carries, with the punctuation the
// shell glues to a list entry's tail (`;`, `,`) and the quoting an author writes
// around a token stripped off.
func pathSetClaimFieldPath(field string) string {
	return strings.TrimRight(field, ";,`\"'")
}

// pathSetClaimTrailerField reports whether a field is list punctuation rather
// than content: a line continuation, the loop's `do`, or a field that is nothing
// but separators once its trailing punctuation is stripped.
func pathSetClaimTrailerField(field string) bool {
	if field == "\\" || field == "do" {
		return true
	}
	return pathSetClaimFieldPath(field) == ""
}

// The shell loop header's shape, named (Rule 1): `for`, a variable, `in`.
const (
	pathSetClaimLoopHeadFields = 3
	pathSetClaimLoopKeyword    = "for"
	pathSetClaimLoopIn         = "in"
)

// pathSetClaimCountAbove recovers the figure the block is a population of: the
// FIRST count+set-unit phrase standing within pathSetClaimCountLookbackLines
// above the block (the nearest one — a count further up describes another
// subject). It returns ok=false when nothing counts the block.
func pathSetClaimCountAbove(lines []string, blockStart int) (count int, unit string, ok bool) {
	for i := blockStart - 1; i >= 0 && blockStart-i <= pathSetClaimCountLookbackLines; i-- {
		if n, u, _, hit := pathSetClaimFigureIn(lines[i]); hit {
			return n, u, true
		}
	}
	return 0, "", false
}

// countLineText is the line the clause QUOTES for a claim: the counted line when
// one stands in the look-behind window, so the child sees the sentence whose
// figure the criterion was written against.
func countLineText(lines []string, blockStart int) string {
	for i := blockStart - 1; i >= 0 && blockStart-i <= pathSetClaimCountLookbackLines; i-- {
		if _, _, _, hit := pathSetClaimFigureIn(lines[i]); hit {
			return strings.TrimSpace(lines[i])
		}
	}
	return strings.TrimSpace(lines[blockStart])
}

// PathSetClaimLineExcerpt quotes a set-claim for a record or a stamped line,
// bounded so the record stays a record.
func PathSetClaimLineExcerpt(line string) string {
	s := strings.TrimSpace(line)
	r := []rune(s)
	if len(r) <= pathSetClaimLineMaxRunes {
		return s
	}
	return string(r[:pathSetClaimLineMaxRunes]) + baseClaimEllipsis
}

// pathSetClaimFigureIn reads the SET COUNT on the line: a count whose unit is
// either the package's Latin vocabulary (countclaim.go's own reader, reused) or
// one of this file's CJK counters.
//
// The Latin half is tried first so a line carrying both (`12 rows / 12 行`)
// reports the shape the package already knows, and the CJK half runs only where
// that returned nothing — never as a second answer to the same question.
func pathSetClaimFigureIn(line string) (count int, unit string, at int, ok bool) {
	if n, u, a, hit := countClaimFigureIn(line); hit {
		return n, u, a, true
	}
	for i := 0; i < len(line); i++ {
		if !isASCIIDigit(line[i]) {
			continue
		}
		j := i
		for j > 0 && isASCIIDigit(line[j-1]) {
			j--
		}
		n, nOk := atoi(line[j : i+1])
		if !nOk || n < pathSetClaimMinCount {
			continue
		}
		k := i + 1
		// "12 行" carries a space between figure and counter; "12行" does not.
		if k < len(line) && line[k] == ' ' {
			k++
		}
		for _, u := range pathSetClaimCJKUnits {
			if !strings.HasPrefix(line[k:], u) {
				continue
			}
			return n, u, k, true
		}
	}
	return 0, "", 0, false
}

// pathSetClaimPathsIn collects every path-shaped token on the line, in order and
// without duplicates, using countclaim.go's ONE path predicate
// (countClaimFileShaped) so this file cannot disagree with the figure clause
// about what a path is.
func pathSetClaimPathsIn(line string) []string {
	var paths []string
	seen := map[string]bool{}
	for _, raw := range strings.Fields(line) {
		tok := pathSetClaimTrimToken(raw)
		if tok == "" || !countClaimFileShaped(tok) {
			continue
		}
		if seen[tok] {
			continue
		}
		seen[tok] = true
		paths = append(paths, tok)
	}
	return paths
}

// pathSetClaimTrimToken strips the punctuation an author writes around a token
// for grammar. It mirrors countclaim.go's trim, with the CJK full stop and the
// enumeration separators this repo's Chinese briefings use added — a token
// followed by `、` or `。` is still the path.
func pathSetClaimTrimToken(raw string) string {
	tok := strings.Trim(raw, "`*_\"'()[]<>,;:!?、。，：（）《》")
	for len(tok) > 0 && strings.HasSuffix(tok, ".") {
		tok = tok[:len(tok)-1]
	}
	return tok
}
