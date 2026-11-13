package taskreport

// gateassert.go — "SURFACE X is RED on the integration branch" as a CLAIM whose
// MEASUREMENT MOMENT has to be recovered from the ROW's own authoring time,
// because the sentence declines to name one.
//
// THE MEASURED CASE (row 01a136be-e3b2-71b9-9219-0b4a2258feff, 2026-10-16): that
// row's briefing opened with
//
//	`go test -run TestDocClassBannerMatchesManifest ./internal/standards/` is RED
//	on master, in a file this branch does not touch:
//
// and demanded the doc-class banner repair. The defect was real WHEN THE ROW WAS
// AUTHORED (2026-10-14 03:30:42, integration tip 82605ae57: the file's DOC-INDEX
// row said "historical" and the file did not open with its banner) and it was
// repaired 22 minutes later (f468c3101, 04:49:50), landed by 05:35:57 — and the
// row was DISPATCHED two days later (2026-10-16 08:52:47) with nothing on it
// saying so. The dispatched run's first job was therefore to re-measure a
// premise that had been dead for two days, and its report could only say so
// after the fact.
//
// WHY THE TWO SHIPPED INSTRUMENTS ARE STRUCTURALLY BLIND TO IT, measured on that
// row's own briefing text (probe output: claims=0 / misses=0):
//
//   - BaseClaimsIn (baseclaim.go) fires only where a base WORD and a
//     COMMIT-SHAPED token stand on ONE line. This sentence declares no base and
//     names no revision at all — its whole subject is a SURFACE and a MOMENT.
//   - UnlandedClaimsIn (unlandedclaim.go) fires only on a sentence ASSERTING the
//     integration branch does NOT have something ("not an ancestor", "never
//     landed", …). This sentence asserts the opposite direction: the branch HAS
//     a red surface.
//
// POSITIVE CONTROL for that claim of blindness: the same seam DOES stamp the
// sibling spelling that names a revision (`base 2f697e5`) — so the gap is the
// SPELLING, not the seam.
//
// WHAT THIS FILE IS. A leaf scanner that recognises the sentence shape — a RED
// VERDICT WORD, the integration branch's name, and at least one SURFACE-SHAPED
// token, all on ONE line — and yields the surface it names. It measures nothing
// and judges nothing: the MOMENT the assertion was made is a fact about the ROW
// (its created_at), the revision that moment resolves to is read off the object
// graph, and both live one level up, in internal/tools (the caller already holds
// the repo handle and the ledger; a second prober here would be a second answer
// — Rules 8/13/14).
//
// THE PREDICATE IS NARROW WHERE IT CAN BE HONEST, and it says so where it cannot:
//
//  1. the word must be a STANDALONE word on both sides. "red" inside "hundred"
//     names no verdict, and the Latin boundary rule is baseclaim.go's own
//     (baseClaimLatinWordRune) rather than a second one.
//  2. a NEGATED verdict is not an assertion: "not red", "no longer red" and
//     "never red" would otherwise make the scanner fire on the sentence that
//     reports the repair.
//  3. a line with the verdict word and the branch but NO surface token is a
//     GateAssertionMiss, not a silent nothing: the caller prints it and says
//     whether it could say anything (Rule 15 — a refusal the caller cannot see
//     is indistinguishable from a briefing that asserts no such thing).
//
// WHAT IT DELIBERATELY DOES NOT DO: it does not decide whether the surface is
// still red (only a re-run can), it does not read the ledger, it does not touch
// the filesystem or git, and it never rewrites the author's line.

import "strings"

const (
	// gateAssertionMaxClaims caps how many assertions a briefing yields: the
	// stamp quotes each one, and a briefing with a systemic problem must not
	// push the child's prompt past what a reader reads (baseClaimMaxClaims'
	// discipline).
	gateAssertionMaxClaims = 6

	// gateAssertionLineMaxRunes bounds the line the clause quotes.
	gateAssertionLineMaxRunes = 200

	// gateAssertionEllipsis marks a line the caller's excerpt truncated.
	gateAssertionEllipsis = "…"

	// gateAssertionNegationWindow is how far BEFORE a verdict word a negation is
	// looked for. It is small on purpose: the negations this must catch stand
	// immediately before the word ("not red", "no longer red"), while a negation
	// a clause away belongs to another clause.
	gateAssertionNegationWindow = 12

	// gateAssertionBindingWindowRunes is how far AFTER a verdict word the
	// integration branch may stand and still be the thing that verdict is ABOUT.
	//
	// IT IS THE PREDICATE'S SECOND HALF, and it exists because of a MEASURED
	// false positive on the motivating briefing's own text, which carries a
	// SECOND line mentioning both the branch and the word "red":
	//
	//	Every `go test ./internal/standards/` on master reports this red beside whatever the branch
	//
	// There the branch comes FIRST and the verdict is a REFERENCE to an assertion
	// made elsewhere ("reports this red"). A co-residence rule stamped it, and a
	// stamp on a line that asserts nothing is a false positive on an honest
	// briefing. Binding the verdict to a branch standing AFTER it ("is RED on
	// master", "fails on master") separates the assertion from the reference; the
	// window is sized to this repo's real spellings.
	gateAssertionBindingWindowRunes = 24

	// gateAssertionBranchlessMarkers are the ANCHOR SPELLINGS that bind a verdict
	// WITHOUT naming the integration branch, and they are the SECOND half of this
	// predicate's binding rule — measured, not invented.
	//
	// THE MEASURED CASE (row 01a1432e-4441-71a6-bd4c-f94fce6cbe11, dispatched
	// 2026-10-16 22:06): its SUMMARY — the sentence `taskRunBriefing` puts at the
	// TOP of the child's briefing — read
	//
	//	…`TestDocClassBannerMatchesManifest` 在 pristine 树上即红
	//
	// and the seam scanned it BLIND (probe on this tree: claims=0, misses=0),
	// because the branch-binding rule above needs the integration branch's name
	// standing after the verdict word and this sentence names a TREE STATE
	// instead. The row was then dispatched against a premise its own base tip had
	// already fixed — the same cost the branch-naming arm was built to remove.
	//
	gateAssertionBranchlessMarker = "pristine"
)

// gateAssertionVerdictWords are the standalone words that state a surface is
// BROKEN at the moment the sentence describes. They are lower-cased here and
// matched case-insensitively (the measured spelling is upper-case "RED" inside
// running prose), and the author's own spelling is what the clause quotes.
var gateAssertionVerdictWords = []string{
	"red",
	"fails",
	"failing",
	"broken",
}

// gateAssertionCJKVerdictWords are the verdict spellings of this repo's OTHER
// working language — the class that made row 01a1432e's assertion invisible
// (its summary ends "…在 pristine 树上即红", where 红 IS "red"). They are kept
// SEPARATE from the Latin list because they take the REVERSED binding direction
// (anchor before the verdict: CJK word order), which the Latin list must not.
var gateAssertionCJKVerdictWords = []string{gateAssertionCJKVerdictWord}

// gateAssertionNegations are the word shapes that turn a verdict into its
// DENIAL. The trailing space is part of each spelling: "not red" is a denial,
// "note red" is not one.
var gateAssertionNegations = []string{"not ", "no ", "never ", "nor ", "isn't ", "is not "}

// gateAssertionBranchlessMarkers are the Latin anchor spellings of the
// branch-less arm, listed rather than inlined so a second spelling is added in
// ONE place (Rules 1/13).
//
// A marker is a STANDALONE word (gateAssertionWordPresent), so "pristine" in
// running prose about a pristine checkout before a build IS the anchor this
// reads — which is correct: such a sentence asserts the surface is red at an
// unmodified tree, and that is exactly the claim whose moment must be
// re-anchored.
var gateAssertionBranchlessMarkers = []string{gateAssertionBranchlessMarker}

// gateAssertionBranchlessCJKMarker is the same anchor in THIS repo's other
// working language: row 01a1432e's own spelling ("在 pristine 树上", 树上 = "on
// the tree"). It is matched as a SUBSTRING rather than a Latin word, because CJK
// prose does not space its words — "pristine 树" and "pristine树" are the same
// two words, and the Latin boundary rule would refuse the unspaced spelling.
//
// It is deliberately NOT added to gateAssertionBranchlessMarkers: that list goes
// through the Latin boundary rule, and a substring there would be a second
// matching rule wearing the first one's name (Rules 13/14).
const gateAssertionBranchlessCJKMarker = "树上"

// gateAssertionCJKVerdictWord is "red" in this repo's other working language
// (row 01a1432e's "即红"). Named rather than inlined like every other verdict
// spelling (Rules 1/13).
const gateAssertionCJKVerdictWord = "红"

// gateAssertionAnchorBeforeWindowRunes is how far BEFORE a CJK verdict word its
// anchor may stand and still bind — the reversed-direction window. It is sized to
// the measured spellings ("在 pristine 树上即红", "在 master 上红"), where the
// anchor and the verdict are separated by at most a locative particle or two.
const gateAssertionAnchorBeforeWindowRunes = 24

// gateAssertionCJKNegations are the characters that turn a CJK verdict into its
// DENIAL — the CJK half of gateAssertionNegations, and NOT a widening of it: the
// Latin list is spelled with a trailing space ("not "), which a CJK particle
// standing directly against the verdict character ("不红了") never has. Measured
// shape: a row reporting the repair ("已经不红了") was otherwise read as the
// assertion, i.e. the scanner would stamp the briefing that already carries the
// answer.
var gateAssertionCJKNegations = []string{"不", "未", "没", "别"}

// gateAssertionCJKNegationWindowRunes is how far before a CJK verdict its
// negation may stand. One character is the measured spelling ("不红"); two
// covers the emphatic form ("绝非红") without reaching into another clause.
const gateAssertionCJKNegationWindowRunes = 2

// gateAssertionCJKNegatedBefore reports whether a CJK negation stands in the
// window before offset at.
func gateAssertionCJKNegatedBefore(low string, at int) bool {
	before := gateAssertionRuneTail(low[:at], gateAssertionCJKNegationWindowRunes)
	for _, n := range gateAssertionCJKNegations {
		if strings.Contains(before, n) {
			return true
		}
	}
	return false
}

// gateAssertionTestNamePrefix is the Go test-function shape ("TestDocClass…")
// that names a surface without a fence, a path or a revision — the second
// spelling this repo's briefings use for "this test is red".
const gateAssertionTestNamePrefix = "Test"

// gateAssertionSurfacePathSeparators / gateAssertionSurfaceFileSuffix are the
// file-like shape: a token that names a REPO PATH ("internal/standards/…",
// "devdoc/…") is a surface the child can re-measure, while a bare word is not.
const (
	gateAssertionSurfacePathSep  = "/"
	gateAssertionSurfaceFileMark = "."
)

// GateAssertion is one "SURFACE is RED on <branch>" sentence: the line, the
// surface-shaped token it names, and the branch word it used.
//
// It carries no measurement and no verdict — the MOMENT the assertion was made
// is the row's own created_at and is not visible from the text, which is exactly
// why this shape needed its own scanner.
type GateAssertion struct {
	// Line is the briefing line the assertion was read from, verbatim.
	Line string
	// Surface is the surface-shaped token the line names — a fenced run, a
	// test-function name, a repo path, or a revision-shaped anchor. A
	// GateAssertion is only ever produced WITH one: a line without it is a
	// GateAssertionMiss.
	Surface string
	// Branch is the integration branch word the line used ("master"), or — for a
	// BRANCH-LESS assertion, one that binds its verdict to a tree state instead
	// ("…在 pristine 树上即红") — the marker it used ("pristine"). Either way it is
	// the author's own spelling: the caller renders it, never re-derives it.
	Branch string
}

// GateAssertionMiss is one line that names a verdict and the integration branch
// but NO surface-shaped token: an assertion whose subject this scanner could not
// read.
//
// It exists so the seam can TELL THE TWO SILENCES APART (Rule 15): "this
// briefing asserts no red surface" (nothing to re-measure, nothing to say) and
// "this briefing asserts one and the scanner could not see what" (a hole the
// caller must state). The token-less arm is a REAL shape in this repo's prose
// ("the banner sweep is RED on master") and is not a bug in the briefing.
type GateAssertionMiss struct {
	// Line is the briefing line, trimmed, verbatim.
	Line string
	// Branch is the branch word the line used.
	Branch string
}

// GateAssertionsIn scans a briefing body for sentences asserting that a SURFACE
// is RED on the integration branch, and yields the surface each one names.
//
// It reads the WHOLE body, like BaseClaimsIn and unlike the three section-scoped
// readers beside it: the motivating sentence stands at the TOP of the briefing,
// before any heading at all, and a reader that looked only under "## Evidence"
// would miss precisely the sentences that made this defect expensive.
//
// An empty result is the ordinary case (most briefings assert no such thing) and
// is the reason this predicate cannot degrade every briefing the seam touches.
func GateAssertionsIn(body, integrationBranch string) []GateAssertion {
	branch := strings.TrimSpace(integrationBranch)
	if branch == "" {
		return nil
	}
	var out []GateAssertion
	seen := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		word, ok := gateAssertionBranchWordIn(line, branch)
		if !ok {
			continue
		}
		surface := gateAssertionSurfaceIn(line)
		if surface == "" || seen[surface] {
			continue
		}
		seen[surface] = true
		out = append(out, GateAssertion{Line: strings.TrimSpace(line), Surface: surface, Branch: word})
		if len(out) >= gateAssertionMaxClaims {
			break
		}
	}
	return out
}

// GateAssertionMissesIn lists the lines that name a verdict and the integration
// branch but no surface-shaped token — the assertions this scanner could not
// read.
//
// The caller states each one and stamps NOTHING for it: the moment such a
// sentence was made is still a fact about the row, but a clause quoting no
// surface would send the child to re-measure nothing (Rule 15).
func GateAssertionMissesIn(body, integrationBranch string) []GateAssertionMiss {
	branch := strings.TrimSpace(integrationBranch)
	if branch == "" {
		return nil
	}
	var out []GateAssertionMiss
	for _, line := range strings.Split(body, "\n") {
		word, ok := gateAssertionBranchWordIn(line, branch)
		if !ok {
			continue
		}
		if gateAssertionSurfaceIn(line) != "" {
			continue
		}
		out = append(out, GateAssertionMiss{Line: strings.TrimSpace(line), Branch: word})
	}
	return out
}

// GateAssertionLineExcerpt quotes an assertion for a record or a stamped line,
// bounded so the record stays a record (BaseClaimLineExcerpt's contract).
func GateAssertionLineExcerpt(line string) string {
	s := strings.TrimSpace(line)
	r := []rune(s)
	if len(r) <= gateAssertionLineMaxRunes {
		return s
	}
	return string(r[:gateAssertionLineMaxRunes]) + gateAssertionEllipsis
}

// gateAssertionBranchWordIn is the ONE predicate for "this line ASSERTS a red
// verdict, and here is the anchor the verdict is bound to": a verdict word must
// stand on the line as a STANDALONE, un-negated word with its anchor inside the
// binding window AFTER it. The anchor is the integration branch's own name, or —
// the branch-less arm — one of gateAssertionBranchlessMarkers. The returned word
// is the author's own spelling of the anchor, and "" with false means the line
// asserts nothing.
//
// The binding DIRECTION is this predicate's load-bearing half, and it is
// measured rather than stylistic: the motivating briefing's own text carries the
// line
//
//	Every `go test ./internal/standards/` on master reports this red beside whatever the branch
//
// where the branch and the word "red" are merely CO-RESIDENT — the verdict there
// REFERS to an assertion made one line earlier. Reading that as an assertion
// stamps a surface nobody asserted anything about; measured while writing this
// file, the co-residence rule produced exactly that false positive on the real
// briefing, and the binding rule is what removed it.
//
// THE BRANCH-LESS ARM DOES NOT WEAKEN THAT RULE, and the reason is structural:
// it reads the SAME window in the SAME direction and only widens WHICH anchor
// counts. A co-resident "red … pristine" line is still silent unless the marker
// stands within gateAssertionBindingWindowRunes AFTER an un-negated verdict word
// — and the measured false positive above carries neither a marker nor a branch
// in that position.
func gateAssertionBranchWordIn(line, branch string) (string, bool) {
	low := strings.ToLower(line)
	if word, ok := gateAssertionVerdictWordIn(low, branch); ok {
		return word, true
	}
	for _, m := range gateAssertionBranchlessMarkers {
		if word, ok := gateAssertionVerdictWordIn(low, m); ok {
			return word, true
		}
	}
	if word, ok := gateAssertionCJKAnchorWordIn(low, branch); ok {
		return word, true
	}
	return "", false
}

// gateAssertionVerdictWordIn reports whether some Latin verdict word stands in
// low as a STANDALONE, un-negated word with anchor bound AFTER it, and returns
// that verdict word. The boundary rule is baseclaim.go's own
// (baseClaimLatinWordRune): "red" inside "hundred" and "fails" inside "failsafe"
// name no verdict, and a second rule here would be a second answer to the same
// question (Rules 13/14).
func gateAssertionVerdictWordIn(low, anchor string) (string, bool) {
	for _, w := range gateAssertionVerdictWords {
		for from := 0; ; {
			j := strings.Index(low[from:], w)
			if j < 0 {
				break
			}
			at := from + j
			from = at + len(w)
			if !gateAssertionWordBoundary(low, at, len(w)) {
				continue
			}
			if gateAssertionNegatedBefore(low, at) {
				continue
			}
			if gateAssertionBranchBoundAfter(low, at+len(w), anchor) {
				return anchor, true
			}
		}
	}
	return "", false
}

// gateAssertionCJKAnchorWordIn is the CJK half of the branch-less/branch arm,
// and it exists because CJK WORD ORDER REVERSES the Latin binding direction: the
// measured assertion says "在 pristine 树上即红" — anchor FIRST, verdict LAST —
// while every Latin spelling this predicate was built from puts the verdict
// first ("is RED on master"). Reading the Latin direction against row 01a1432e's
// own text measured claims=0, i.e. the sentence the epic exists for stayed
// invisible.
//
// THE REVERSED WINDOW IS NARROWER, NOT LOOSER, and the reason is the false
// positive this file already removed: co-residence is what made a "reports this
// red" line stamp. So the reversed arm accepts ONLY an anchor standing BEFORE the
// verdict word (inside gateAssertionAnchorBeforeWindowRunes), and it accepts NO
// anchor-after reading for a CJK verdict — measured on this host's corpus, the
// after-direction fires on "「恒红」不是 master 的缺陷" ("always-red is NOT a
// defect of master"), which asserts the opposite of a red surface.
func gateAssertionCJKAnchorWordIn(low, branch string) (string, bool) {
	for _, w := range gateAssertionCJKVerdictWords {
		for from := 0; ; {
			j := strings.Index(low[from:], w)
			if j < 0 {
				break
			}
			at := from + j
			from = at + len(w)
			if !gateAssertionWordBoundary(low, at, len(w)) {
				continue
			}
			if gateAssertionNegatedBefore(low, at) || gateAssertionCJKNegatedBefore(low, at) {
				continue
			}
			before := gateAssertionRuneTail(low[:at], gateAssertionAnchorBeforeWindowRunes)
			if strings.Contains(before, gateAssertionBranchlessCJKMarker) {
				return gateAssertionBranchlessCJKMarker, true
			}
			if branch != "" && gateAssertionWordPresent(before, branch) {
				return branch, true
			}
			for _, m := range gateAssertionBranchlessMarkers {
				if gateAssertionWordPresent(before, m) {
					return m, true
				}
			}
		}
	}
	return "", false
}

// gateAssertionRuneTail returns the LAST n runes of s (a byte cut would split a
// multibyte word — gateAssertionRuneWindow's mirror, same discipline).
func gateAssertionRuneTail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// gateAssertionBranchBoundAfter reports whether anchor stands as a STANDALONE
// word within the binding window that opens at byte offset from — the branch (or
// tree-state marker) the verdict word before it is about.
func gateAssertionBranchBoundAfter(low string, from int, anchor string) bool {
	if from >= len(low) {
		return false
	}
	return gateAssertionWordPresent(gateAssertionRuneWindow(low[from:], gateAssertionBindingWindowRunes), anchor)
}

// gateAssertionRuneWindow returns the prefix of s holding at most n RUNES (a
// byte cut would split a multibyte word, and this repo's briefings carry
// Chinese). It is read as a MEASURE by its caller — baseclaim.go's
// baseClaimRuneWindow established the discipline.
func gateAssertionRuneWindow(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// gateAssertionNegatedBefore reports whether a negation stands within the window
// before offset at — "not red", "no longer red", "is not red".
func gateAssertionNegatedBefore(low string, at int) bool {
	from := at - gateAssertionNegationWindow
	if from < 0 {
		from = 0
	}
	before := low[from:at]
	for _, n := range gateAssertionNegations {
		if strings.Contains(before, n) {
			return true
		}
	}
	return false
}

// gateAssertionWordPresent reports whether w stands in line as a standalone
// word. Latin-only, like the base word's rule: this repo's briefings name the
// integration branch in Latin ("master"), and the boundary rule is what keeps
// "masterpiece" and "the master's" honest.
func gateAssertionWordPresent(line, w string) bool {
	low := strings.ToLower(line)
	lw := strings.ToLower(w)
	for from := 0; ; {
		j := strings.Index(low[from:], lw)
		if j < 0 {
			return false
		}
		at := from + j
		from = at + len(lw)
		if gateAssertionWordBoundary(low, at, len(lw)) {
			return true
		}
	}
}

// gateAssertionWordBoundary reports whether the word of width n starting at at
// is standalone: neither neighbour is a Latin letter, digit or underscore.
func gateAssertionWordBoundary(low string, at, n int) bool {
	if at > 0 && baseClaimLatinWordRune(rune(low[at-1])) {
		return false
	}
	if at+n < len(low) && baseClaimLatinWordRune(rune(low[at+n])) {
		return false
	}
	return true
}

// gateAssertionSurfaceIn returns the first surface-shaped token on the line —
// the thing the sentence says is red, and the only part of it the child can be
// sent back to. Four shapes, in the order the repo's briefings use them:
//
//	① a BACKTICK-FENCED run — the measured spelling ("`go test -run TestX ./pkg/`");
//	② a revision-shaped token — the anchor spelling, when the author named one;
//	③ a Go test-function name — "TestDocClassBannerMatchesManifest is RED";
//	④ a repo-path-shaped token — "devdoc/ledger-readonly-faces.md is broken".
//
// "" when the line names none, which is a MISS and not a pass.
func gateAssertionSurfaceIn(line string) string {
	if fenced := gateAssertionFencedRun(line); fenced != "" {
		return fenced
	}
	for _, tok := range implClaimTokens(line) {
		if implClaimShaShape(tok) {
			return tok
		}
	}
	if name := gateAssertionTestName(line); name != "" {
		return name
	}
	return gateAssertionPathLike(line)
}

// gateAssertionFencedRun returns the content of the FIRST non-empty backtick
// fence on the line. A fence is how this repo's briefings name a command or a
// file verbatim, and the fence is what tells a surface from surrounding prose.
func gateAssertionFencedRun(line string) string {
	parts := strings.Split(line, "`")
	// A fence needs a closing partner: indices 1, 3, 5, … are the fenced runs.
	for i := 1; i < len(parts); i += 2 {
		if s := strings.TrimSpace(parts[i]); s != "" {
			return s
		}
	}
	return ""
}

// gateAssertionTestName returns the first Go test-function name on the line —
// the letter run that follows the "Test" prefix and continues in the identifier
// alphabet. "" when the line names none.
func gateAssertionTestName(line string) string {
	for from := 0; ; {
		j := strings.Index(line[from:], gateAssertionTestNamePrefix)
		if j < 0 {
			return ""
		}
		at := from + j
		from = at + 1
		// The prefix itself must start a word: "LatestTest" is not a test name.
		if at > 0 && baseClaimLatinWordRune(rune(line[at-1])) {
			continue
		}
		end := at + len(gateAssertionTestNamePrefix)
		for end < len(line) && baseClaimLatinWordRune(rune(line[end])) {
			end++
		}
		if end == at+len(gateAssertionTestNamePrefix) {
			continue // the bare word "Test" names no function
		}
		// The character after the name must not be a lowercase letter or digit —
		// a Go test name continues in CamelCase from an upper-case rune after the
		// prefix ("TestFoo" not "Tests"), and a plain English word ("Testing the
		// seam") must not be read as one.
		if r := rune(line[at+len(gateAssertionTestNamePrefix)]); r < 'A' || r > 'Z' {
			continue
		}
		return line[at:end]
	}
}

// devdoc-ref-ok: the two tokens quoted below are SHAPES this function
// recognises, not citations — the subject is the path shape, not the document.
//
// gateAssertionPathLike returns the first repo-path-shaped token on the line: a
// bare token carrying both a path separator and a file mark (an example such
// as "devdoc/x.md", "internal/standards/"), which is a surface a child can go
// and re-measure.
func gateAssertionPathLike(line string) string {
	for _, f := range strings.Fields(line) {
		tok := strings.Trim(f, "`*_\"'(),:;[]")
		if tok == "" || !strings.Contains(tok, gateAssertionSurfacePathSep) {
			continue
		}
		if !strings.Contains(tok, gateAssertionSurfaceFileMark) {
			continue
		}
		return tok
	}
	return ""
}
