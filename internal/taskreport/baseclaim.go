package taskreport

// baseclaim.go — "this briefing was measured at base <rev>" as a CLAIM to be
// re-measured at dispatch time, never as a fact.
//
// THE MEASURED CASE (row 01a136a7-dccd, 2026-10-14). The row's briefing carried
// the sentence "on master, `gopi tool doccheck` exits 1 (row #1212, base
// 2f697e5)" plus a verbatim reproduction block labelled "(measured 2026-10-14,
// base 2f697e5)", and demanded a repair of the frozen permit table. The repair
// (34693afb3) is the DIRECT CHILD of that very base — it landed at 02:45:57,
// i.e. master's tip WAS the repair 20 minutes BEFORE the row was authored
// (03:05:33) and 56 minutes before the child's cut point (c06e04e22, 03:42:03).
// The bug report was HONEST when first submitted (02:41:51, master tip then =
// 2f697e5: `gopi tool doccheck` really did exit 1); the two sibling rows filed
// at 03:05:20/03:05:33 were authored from the SAME evidence text without
// re-measuring its premise. The dispatch pipeline was RIGHT — the ground's base
// (a0693ba) and the cut point are both current: what had gone stale was the
// briefing's own SELF-DECLARED base revision, and it is the only element of the
// text that is a claim about the object graph rather than prose.
//
// WHY THIS IS NOT unlandedclaim.go's JOB. That scanner needs a sentence to
// ASSERT the integration branch lacks something ("not an ancestor", "never
// landed", …). This briefing asserted nothing of the kind — it reported a
// measurement taken at a revision, which is why the existing seam was
// structurally silent on it (measured: 0 freshness stamps for this briefing's
// text). A base declaration needs no assertion phrase: the revision it names IS
// its claim, and "the integration branch has moved past it" is decidable from
// the object graph alone.
//
// The package stays a LEAF (stdlib only, constants + pure text functions), and
// the judgement is not made here either: the caller measures the base against
// the integration branch through internal/tools/master_claim.go, whose exit
// status is the verdict (Rules 8/13 — reuse, never a second git prober).
//
// THE PREDICATE IS NARROW ON PURPOSE, twice over, because the failure this check
// must never make is stamping an honest briefing (the seam's positive control
// exists for exactly that):
//
//  1. a claim exists only where a base WORD and a COMMIT-SHAPED token stand
//     together on one line. A brief naming a base with no revision, or a
//     revision with no base, yields NO claim — not an unmeasured one — so the
//     ordinary briefing that merely says "the base case" produces nothing.
//  2. the word is matched as a STANDALONE word ("base commit", "base 2f697e5",
//     "基线"), never as a substring: "codebase " and "database " carry the
//     letters but name no revision, and a substring match would fire on them.

import "strings"

const (
	// baseClaimMaxClaims caps how many base declarations a briefing yields. The
	// first few name the pattern, and a briefing with a systemic problem must
	// not push the child's prompt past what a reader reads (the cap discipline
	// unlandedClaimMaxSentences follows).
	baseClaimMaxClaims = 8

	// baseClaimLineMaxRunes bounds the line a claim quotes in the record the
	// caller renders: a briefing line can be a paragraph, and a record has to
	// stay a record.
	baseClaimLineMaxRunes = 200

	// baseClaimEllipsis marks a line the caller's excerpt truncated.
	baseClaimEllipsis = "…"

	// baseClaimLookaheadRunes bounds how far AFTER the base word a revision may
	// be named and still belong to it — the prose shapes this scanner must read
	// are "base 2f697e5", "base commit 2f697e5" and the table cell
	// "| base commit | 2f697e5 |", all far inside this window, while a revision
	// discussed a sentence later is no longer attributable to the base word.
	baseClaimLookaheadRunes = 32
)

// baseClaimWords are the standalone words that DECLARE a base revision. "base"
// is the repo's own spelling in every measured briefing and in this epic's
// acceptance criterion; the Chinese form is carried because briefings in this
// repo are written in both languages and "基线" is a standalone word in a way no
// Latin substring is.
var baseClaimWords = []string{"base", "基线"}

// BaseClaim is one declaration of the revision a briefing was measured at,
// together with the revision it names.
type BaseClaim struct {
	// Line is the briefing line the declaration was read from, verbatim: the
	// caller quotes it, and nothing here rewrites prose.
	Line string
	// Commit is the commit-shaped token standing after the base word. A
	// BaseClaim is only ever produced WITH one — a base word alone declares no
	// revision to measure and is therefore not this scanner's subject.
	Commit string
}

// baseClaimGlueWords names the base words that may take the glue exception —
// the CJK spelling(s), never a Latin one.
//
// THE MEASURED CASE (epic row 01a13715, 2026-10-15): the Chinese spelling of the
// base word is GLUED to its own revision in this repo's briefings —
// "基线2f697e5", no space, no fence — and the Latin word-boundary rule upstream
// rejected it, because the rune after "基线" is a Latin hex digit and the guard
// read the word and its revision as one longer word. The seam was then SILENTLY
// silent: indistinguishable from "this briefing declares no base".
//
// The exception is deliberately NARROW, and its narrowness is the whole reason
// it is safe: it applies ONLY to a non-Latin word in this list (a Latin word
// followed by a hex digit is still "codebase"/"database" territory) and ONLY
// when that rune can START a revision (isHexDigit) — so "基线zz2f697e5" is still
// refused, and the glue must still be followed by a hex run of revision length
// for anything to be claimed at all.
var baseClaimGlueWords = []string{"基线"}

// baseClaimRevisionVerbs are the git verbs that OWN the revision standing beside
// them: when one of these names the text between a base word and a
// commit-shaped token, that token belongs to the COMMAND and declares no base.
//
// THE MEASURED CASE (row 01a14511, 2026-10-16): `gopi task submit` stamped
//
//	[Base revision — a declared base was re-measured at submit]
//	warning: 1 declared base revision(s) … RE-MEASURE BEFORE REPAIRING
//	- base 202399cf is 61 commit(s) behind 21a72442 …
//	  payload line: "$ git merge-base --is-ancestor 202399cfe 21a72442d && echo YES"
//
// on a line whose WHOLE PURPOSE is to state that the fix is an ANCESTOR of the
// integration tip. The warned-about revision was an ancestor BY CONSTRUCTION, so
// the stamp could only ever be wrong there: "merge-base" carries a standalone
// "base" (the hyphen is not a Latin word rune), "is-ancestor" stands inside the
// baseClaimLookaheadRunes window, and the scanner read the CHECK as a
// DECLARATION. A warning that sends a reader to re-measure the wrong object
// costs more than silence (Rule 15).
//
// The criterion is therefore the verb NEIGHBOURHOOD, never standalone-ness
// alone: the text between the base word and the candidate revision names a
// revision verb, so the revision is that command's argument.
var baseClaimRevisionVerbs = []string{
	"merge-base", "is-ancestor", "rev-list", "rev-parse", "cat-file",
	"log", "show", "diff",
}

// baseClaimProbeTallyWords are the words a line uses to REPORT this scanner's
// own answer — the tally of the probe table that MEASURES the predicate.
//
// THE MEASURED CASE (row 01a18232, 2026-10-28 — filed from row 01a17e04's
// submit receipt, evidence r2-submit-false-positive.txt). A payload whose subject
// IS this predicate carries a probe table, and `gopi task submit` stamped
//
//   - base a802b473 is 64 commit(s) behind 84beb309 …; payload line: "base a802b47  claims=1 misses=0"
//
// on that table's FIRST ROW. The row QUOTES the shape `base a802b47` to say what
// the scanner answers for it; the payload declares no base revision anywhere — it
// measures the predicate. So the warning sent its reader to re-measure a802b47
// (64 commits behind) instead of re-reading the scanner, which is the most
// expensive mistake this scanner can make (Rule 15).
//
// This is row 01a14511's failure mode in a second shell: there the COMMAND shape
// ("merge-base --is-ancestor") was exempted and the QUOTED shape still fired.
var baseClaimProbeTallyWords = []string{"claims", "misses"}

// baseClaimQuoteRunes are the characters that say "the author is NAMING this
// text, not writing it": a quoted or fenced shape is a SPECIMEN. "|" belongs to
// the class because a markdown table's cell rule is how every probe table in this
// repo is spelled — "| base a802b47 | claims=1 misses=0 |" quotes the specimen
// with the rule that opens the cell.
const baseClaimQuoteRunes = "`\"'|"

// baseClaimCloseRunes are the characters that CLOSE a quoted specimen right after
// a revision: a fence or a quote — never the cell rule ("|"), which closes every
// table DECLARATION too ("| base commit | 2f697e5 |"), and counting it would
// silence the shape the acceptance criterion requires to survive.
const baseClaimCloseRunes = "`\"'"

// baseClaimWordGlue is the hyphen that joins a base word into a longer compound
// ("merge-base", "work-base"): a word glued this way is a FRAGMENT of another
// word, not the standalone declaration this scanner reads.
const baseClaimWordGlue = '-'

// baseClaimRefTokens are the words that stand for the REPOSITORY'S OWN refs on a
// line that pins a revision to one, and they are the ONLY refs this arm reads
// without a qualifier.
//
// THE MEASURED CASE (row 01a18231, 2026-10-28 — filed from row 01a17e04's
// census). The predicate above requires a base WORD, so the spelling this repo's
// briefings actually use for their measurement object
//
//	### 1) 在 origin/master a802b47（detached）上跑那个测试
//
// produced claims=0 AND misses=0 — not "declared something I could not read" but
// "declared nothing at all" (Rule 15's two silences, indistinguishable at the
// seam). 7 of 1958 real payloads carry the `origin/master <sha>` spelling and
// not one had ever been measured; row 01a17dc1 then reported "RED on master"
// from a revision master had already left 44 minutes earlier.
//
// They are unqualified ON PURPOSE. A branch name is the CALLER's knowledge (the
// leaf never mirrors it — unlandedClaimBranchTokens sets the precedent, Rules
// 1b/13/14), and an arbitrary `foo/bar <sha>` in prose is a path, not a ref: a
// walk over every slash word would fire on file paths and on the `->` line of
// every fetch output. A ref carrying `refs/` (below) needs no list because its
// full spelling IS the qualification.
var baseClaimRefTokens = []string{"origin/master", "refs/remotes/origin/master"}

// baseClaimRefQualifiers are the words that turn a BARE branch name into a ref a
// reader can measure against. "on master a802b47" pins the revision to master
// exactly as "origin/master a802b47" does (measured: 01a17dc1's briefing says
// "detached 到 origin/master `a802b47`" and "在 origin/master a802b47 上跑"), but
// the bare word alone is what makes "the report on master reports this red"
// ordinary prose.
var baseClaimRefQualifiers = []string{"origin/", "refs/"}

// baseClaimRefBranchLeadIns are the words that pin a revision to a BARE branch
// name — the spelling "on master a802b47", which names exactly the same
// measurement object as "origin/master a802b47" (row 01a18231's epic names both
// as required positive controls).
//
// The lead-in is what makes the bare name a REF rather than an English word: the
// branch name itself is the caller's knowledge and is never mirrored here (Rules
// 1b/13/14, unlandedClaimBranchTokens' precedent), so the scan reads the SHAPE —
// lead-in, one name, then a commit-shaped token inside baseClaimLookaheadRunes.
// A sentence that merely mentions a branch ("the report on master reports this
// red") names no revision and stays silent, which is the whole reason the shape
// is required rather than the word.
var baseClaimRefBranchLeadIns = []string{"on", "at", "onto", "到", "在"}

// BaseClaimMiss is one base word standing beside a revision-LENGTH token the
// shape predicate REFUSES: a declaration this scanner could not read.
//
// It exists so the seam can TELL THE TWO SILENCES APART (Rule 15): the seam's
// caller cannot distinguish "the briefing declares no base revision" (nothing to
// measure, nothing to say) from "the briefing declares one in a spelling this
// predicate refuses" (a hole in the instrument) — and the second is exactly the
// defect the epic this file ships with was filed for.
type BaseClaimMiss struct {
	// Line is the briefing line, trimmed, verbatim.
	Line string
	// Token is the revision-length token the predicate refused.
	Token string
}

// BaseClaimsIn scans a briefing body for the revisions it declares as its base.
//
// It reads the WHOLE body, unlike the three section-scoped readers beside it
// (ImplClaimsIn / UnlandedClaimsIn / GateClaimVerdict read Evidence, else
// Verification, else the whole body). The reason is measured, not stylistic: a
// base declaration lives where the AUTHOR put the evidence — this epic's own
// text declares it inside its Reproduction section and inside a table row — and
// a reader that looked only under "## Evidence" would miss precisely the
// declarations that made this defect expensive.
func BaseClaimsIn(body string) []BaseClaim {
	var claims []BaseClaim
	seen := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		commit := baseClaimCommitIn(line)
		if commit == "" || seen[commit] {
			continue
		}
		seen[commit] = true
		claims = append(claims, BaseClaim{Line: strings.TrimSpace(line), Commit: commit})
		if len(claims) >= baseClaimMaxClaims {
			break
		}
	}
	return claims
}

// baseClaimMinMissTokenLen is the length at which a refused token is worth
// stating: shorter than an unambiguous git abbreviation there is nothing a
// reader could have meant as a revision, and stating it would make the record
// fire on ordinary prose (the reason floats are not claims either).
const baseClaimMinMissTokenLen = implClaimShaMinLen

// BaseClaimMissesIn lists the base words standing beside a revision-LENGTH token
// that this scanner's shape predicate REFUSES — the declarations it could not
// read ("base 12345678": no letter; "base deadbeef": no digit; an over-long
// token).
//
// It is the RECORD half of the predicate, and it exists because a refusal the
// caller cannot see is indistinguishable from a briefing that declares no base
// at all (Rule 15). The caller decides what to say; this leaf only names the
// shape it refused, and the token verbatim (the author's own spelling).
//
// It stays SILENT on every line that does not carry a base word the boundary
// guard accepts, so the ordinary briefing is untouched — including the shape the
// guard refuses entirely ("this codebase 12345678 grew a database 34693afb3",
// where the letters of "base" belong to longer words and nothing was declared).
func BaseClaimMissesIn(body string) []BaseClaimMiss {
	var out []BaseClaimMiss
	seen := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		if baseClaimBaseWordIn(line) < 0 && baseClaimRefPinIn(line) < 0 {
			continue
		}
		for _, tok := range baseClaimRefusedTokensIn(line) {
			if seen[tok] {
				continue
			}
			seen[tok] = true
			out = append(out, BaseClaimMiss{Line: strings.TrimSpace(line), Token: tok})
		}
		if tok := baseClaimRefMissTokenIn(line); tok != "" && !seen[tok] {
			seen[tok] = true
			out = append(out, BaseClaimMiss{Line: strings.TrimSpace(line), Token: tok})
		}
	}
	return out
}

// baseClaimRefusedTokensIn returns the revision-LENGTH tokens on the line that
// the shape predicate refuses — the tokens a reader could have meant as a
// revision and this scanner did not accept. The tokens are taken from the line
// AS THE AUTHOR WROTE THEM (the record quotes the spelling, never a normalised
// one) and lower-cased only for the case-insensitive word-edge test.
func baseClaimRefusedTokensIn(line string) []string {
	low := strings.ToLower(line)
	var out []string
	for _, tok := range implClaimTokens(line) {
		if len(tok) < baseClaimMinMissTokenLen {
			continue
		}
		if implClaimShaShape(tok) {
			continue
		}
		if !baseClaimTokenAtWordEdge(low, strings.ToLower(tok)) {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// baseClaimTokenAtWordEdge reports whether tok is bounded by a non-word rune on
// at least one side. A token that is glued to Latin letters on BOTH sides is
// part of a longer word, and naming it as an unreadable revision would fire the
// record on prose (measured shape: "codebase 2f697e5 grew a database 34693afb3"
// must stay silent).
func baseClaimTokenAtWordEdge(low, tok string) bool {
	for i := 0; i+len(tok) <= len(low); i++ {
		if low[i:i+len(tok)] != tok {
			continue
		}
		leftFree := i == 0 || !baseClaimLatinWordRune(rune(low[i-1]))
		rightFree := i+len(tok) >= len(low) || !baseClaimLatinWordRune(rune(low[i+len(tok)]))
		if leftFree || rightFree {
			return true
		}
	}
	return false
}

// BaseClaimLineExcerpt quotes a base declaration for a record or a stamped line,
// bounded so the record stays a record (the stamp's own budget, Rule 1).
func BaseClaimLineExcerpt(line string) string {
	s := strings.TrimSpace(line)
	r := []rune(s)
	if len(r) <= baseClaimLineMaxRunes {
		return s
	}
	return string(r[:baseClaimLineMaxRunes]) + baseClaimEllipsis
}

// baseClaimCommitIn is the ONE predicate over one line: it finds a declared base
// and returns the revision named beside it ("" when the line declares no base,
// or declares one without naming a revision).
//
// TWO SPELLINGS DECLARE A MEASUREMENT, and they are alternatives, never a
// fallback: a base WORD ("base 2f697e5", "基线2f697e5") and a REF pinned to a
// revision ("origin/master a802b47", "on master a802b47", "| `refs/remotes/origin/master` | `c6e93569e` |").
// The second is the shape row 01a18231 was filed for: a revision attached to a
// ref is a MEASUREMENT OBJECT ("the ref WAS here when I looked"), which is
// exactly the claim the seam compares against the integration tip.
func baseClaimCommitIn(line string) string {
	i := baseClaimBaseWordIn(line)
	if i < 0 {
		return baseClaimRefCommitIn(line)
	}
	commit := baseClaimCommitAfter(line, i)
	if commit == "" || baseClaimRevisionVerbBefore(line, i, commit) {
		// No revision named, or the revision belongs to a git revision
		// COMMAND rather than to a base declaration (the measured defect: an
		// ancestry check stamped STALE, row 01a14511).
		return ""
	}
	if baseClaimQuotedShape(line, i, commit) {
		// The revision stands where it does only because the author is QUOTING
		// the shape — a probe table's specimen, not a declared measurement
		// (row 01a18232: the seam stamped the probe row and sent its reader to
		// re-measure a802b47 instead of the predicate).
		return ""
	}
	return commit
}

// baseClaimQuotedShape reports whether the revision at commit stands in a
// SPECIMEN on this line — a quoted, fenced or table-cell CITATION of the shape
// "base <rev>" rather than a declaration of the revision this briefing was
// measured at (row 01a18232).
//
// TWO conditions, and the second is what keeps the criterion from swallowing the
// declarations it exists to protect. BOTH are measured against the shapes this
// file already pins:
//
//	A — the base word stands INSIDE quotes: the last non-space rune before it is
//	    a quoting character (baseClaimQuoteRunes — a fence, a quote, or the cell
//	    rule that opens a markdown table cell).
//	B — the specimen is CLOSED: a closing quote stands immediately after the
//	    revision (baseClaimCloseRunes), or the line reports this scanner's OWN
//	    answer after it (baseClaimProbeTallyWords).
//
// MEASURED — the shapes this row must silence, and the ones it must keep:
//
//	| base a802b47 | claims=1 misses=0 |      QUOTED   — A: cell rule, B: tally
//	| `base a802b47` | claims=1 misses=0 |    QUOTED   — A: fence, B: fence+tally
//	`base a802b47`                            QUOTED   — A: fence, B: closing fence
//	"base a802b47 claims=1 misses=0"          QUOTED   — A: quote, B: tally
//	| base commit | 2f697e5 |                 DECLARES — A true (cell rule), B
//	                                          FALSE: no closing quote and no tally
//	base 2f697e5                              DECLARES — A false
//	(base 2f697e5)                            DECLARES — a parenthesis is not a
//	                                          quoting rune: the closure's own arm
//	                                          cites a measurement this way
//	measured at base 2f697e5 …                DECLARES — A false (prose lead-in)
//	origin/master a802b47 / on master a802b47 UNREACHED — the ref arm is a
//	                                          different arm and never sees this
//	                                          predicate (row 01a18231 stays whole)
func baseClaimQuotedShape(line string, after int, commit string) bool {
	if !baseClaimQuotedAt(line, baseClaimBaseWordStart(line, after)) {
		return false
	}
	if at := strings.Index(line[after:], commit); at >= 0 {
		tail := line[after+at+len(commit):]
		if tail != "" && strings.ContainsRune(baseClaimCloseRunes, rune(tail[0])) {
			return true
		}
	}
	return baseClaimTallyAfter(line, after, commit)
}

// baseClaimQuotedAt reports whether the base word starting at start stands
// inside quotes — the last non-space rune before it belongs to the quoting class.
// Whitespace is skipped because a table cell spells the rule and the word as
// "| base" (one space), which is the measured probe-table shape.
func baseClaimQuotedAt(line string, start int) bool {
	for i := start - 1; i >= 0; i-- {
		r := rune(line[i])
		if r == ' ' || r == '\t' {
			continue
		}
		return strings.ContainsRune(baseClaimQuoteRunes, r)
	}
	return false
}

// baseClaimBaseWordStart returns the offset at which the base word ending at
// after begins — read off the ONE word list (Rules 1/13), so the quoting test
// cannot drift from what the scanner matched.
func baseClaimBaseWordStart(line string, after int) int {
	low := strings.ToLower(line)
	head := low[:after]
	best := after
	for _, w := range baseClaimWords {
		if strings.HasSuffix(head, w) && len(head)-len(w) < best {
			best = len(head) - len(w)
		}
	}
	return best
}

// baseClaimTallyAfter reports whether a tally word stands after the revision on
// this line — the text this scanner itself prints beside a specimen
// (baseClaimProbeTallyWords: "claims", "misses").
//
// The span runs to the end of the LINE on purpose: a probe table writes the
// specimen and the tally in two CELLS ("| base a802b47 | claims=1 misses=0 |"),
// so a span bounded by the cell rule would miss the very shape this arm exists
// for. It is reached only after condition A held, so the line that declares a
// measurement inside quotes still declares it.
func baseClaimTallyAfter(line string, after int, commit string) bool {
	at := strings.Index(line[after:], commit)
	if at < 0 {
		return false
	}
	low := strings.ToLower(line[after+at+len(commit):])
	for _, w := range baseClaimProbeTallyWords {
		if baseClaimTermAtWordEdge(low, w) {
			return true
		}
	}
	return false
}

// baseClaimRefMissTokenIn returns the revision-LENGTH token pinned to a ref that
// the shape predicate REFUSES — the ref arm's half of the miss record.
//
// It exists for the same reason the base-word arm's does (Rule 15): a shape the
// scanner cannot read must be STATED, or "origin/master 12345678" is
// indistinguishable from a line that declares nothing. Only a ref that DECLARES
// is examined (baseClaimRefPinIn), so a command's operand never grows a record.
func baseClaimRefMissTokenIn(line string) string {
	at := baseClaimRefPinIn(line)
	if at < 0 {
		return ""
	}
	tail := line[at:]
	for _, tok := range baseClaimTokensWithin(tail, baseClaimLookaheadRunes) {
		if len(tok) < baseClaimMinMissTokenLen || implClaimShaShape(tok) {
			continue
		}
		return tok
	}
	return ""
}

// baseClaimRefCommitIn returns the revision a REF is pinned to on this line, or
// "" when no ref pins one — the SECOND spelling of a declared measurement.
//
// The criterion is POSITIONAL, and that is the whole reason it is not a
// substring test (the epic's constraint 5): the same `origin/master` has two
// faces in this repo's briefings, and only one of them is a declaration.
//
//	origin/master a802b47                    DECLARES — the ref is the subject,
//	                                         the revision is what it was at
//	git merge-base --is-ancestor f6031ead9 origin/master
//	                                         NOT — the ref is the comparison
//	                                         OPERAND and stands AFTER the revision
//	git log origin/master a802b47            NOT — the text before the ref names
//	                                         a git revision verb
//
// So the ref must be the FIRST thing on the line (nothing but fences, quotes,
// bullets, table rules and CJK prose before it), and the revision must be the
// first commit-shaped token AFTER it inside baseClaimLookaheadRunes — the same
// distance bound the base-word arm uses, applied to a token's START so a
// full-length revision is never cut (row 01a1373b).
func baseClaimRefCommitIn(line string) string {
	at := baseClaimRefPinIn(line)
	if at < 0 {
		return ""
	}
	commit := baseClaimCommitAfter(line, at)
	if commit == "" {
		return ""
	}
	if baseClaimRefQuotedShape(line, at, commit) {
		// The revision stands where it does only because the author is QUOTING
		// the shape — a probe table's specimen or a `BaseClaimsIn("…")` sample,
		// not a declared measurement (row 01a1a620: the seam stamped a payload
		// whose whole subject WAS this predicate, and sent its reader to
		// re-measure a revision that row never used).
		return ""
	}
	return commit
}

// baseClaimRefQuotedShape reports whether the revision a REF is pinned to stands
// in a SPECIMEN rather than in a declaration — the ref arm's half of
// baseClaimQuotedShape, stated as the same TWO conditions so the two legs cannot
// drift apart.
//
// THE MEASURED CASE (row 01a1a620, 2026-11-04). `gopi task submit` stamped
//
//	[Base revision — a declared base was re-measured at submit]
//	- base f2d6d393 is 821 commit(s) behind 8c967277 …; payload line: "2. **正解不许被
//	  误伤**：`BaseClaimsIn(\"on master a802b47\")`、`BaseClaimsIn(\"rebase 到
//	  origin/master f2d6d39 之上\")` 仍返回 1 条（01a18231 的验收面）。"
//
// on a payload that declared NO base at all: it QUOTED two positive controls to
// say what the predicate must keep answering. Row 01a18232 fixed exactly this on
// the base-word arm and left the ref arm unguarded, so the same warning fired on
// the ref spelling — measured on this rev BEFORE the fix:
//
//	`on master a802b47`                          claims=1   (specimen)
//	"on master a802b47"                          claims=1   (specimen)
//	| on master a802b47 | ok |                   claims=1   (specimen)
//	on master a802b47                            claims=1   (DECLARES)
//	rebase 到 origin/master f2d6d39 之上          claims=1   (DECLARES)
//
// The criterion is SHAPE, never the revision's availability (row 01a18232's
// ruling): an unresolvable revision must still be stated, or a typo'd real base
// would be silently excused (Rule 15). And it is the QUOTATION'S span, never the
// line's: nearly every declaration in this repo's briefings fences its revision
// ("- detached at origin/master `b12e9c743` 上复现"), so "is the revision fenced"
// would silence the arm this row must keep whole.
//
//	A — the ref stands inside a quotation of the surrounding PROSE: a quoting
//	    rune (baseClaimQuoteRunes — a fence, a quote, or the cell rule that opens
//	    a markdown table cell) stands before the ref, does not recur between the
//	    ref and the revision (a quotation that closed in between does not
//	    CONTAIN the pair), and does not sit DIRECTLY against the ref name — a
//	    fence hugging the pin is the pin's own fence, which is this repo's
//	    ordinary spelling of a declaration ("rebase 到 `origin/master f2d6d39`").
//	B — that same rune CLOSES after the revision: the first quoting rune after
//	    it is the same one, or the line reports this scanner's OWN answer after
//	    it (baseClaimProbeTallyWords). B reads "the first quoting rune after"
//	    rather than the base-word arm's "immediately after the revision" because
//	    a CJK clause closes AFTER its prose
//	    (`BaseClaimsIn("rebase 到 origin/master f2d6d39 之上")`), and a closure
//	    that demanded adjacency would miss exactly the quoted sample this row was
//	    filed for.
//
// MEASURED — the shapes each condition must answer, and the neighbour each
// condition exists to keep:
//
//	`on master a802b47`                          QUOTED   — A: fence, B: fence
//	"on master a802b47"                          QUOTED   — A: quote, B: quote
//	| on master a802b47 | ok |                   QUOTED   — A: cell rule, B: cell rule
//	`BaseClaimsIn("rebase 到 origin/master f2d6d39 之上")`
//	                                             QUOTED   — A: the quote is open at
//	                                             the ref and does not recur before
//	                                             the revision, B: it recurs after
//	                                             the CJK clause
//	| `refs/remotes/origin/master` | `c6e93569e` | 4 |
//	                                             DECLARES — A false: the fence sits
//	                                             directly against the ref name, so it
//	                                             is the pin's own fence and not a
//	                                             quotation of prose (row 01a18231's
//	                                             control)
//	- 交付提交：`174a137`（rebase 到 `origin/master f2d6d39` 之上）
//	                                             DECLARES — same reason (the fence
//	                                             hugs the ref), the spelling this
//	                                             repo's briefings use for a real base
//	| measured on master a802b47 | 4 |           QUOTED   — A: cell rule, B: cell rule
//	                                             (a cell holding the WHOLE pair cites
//	                                             the shape; a cell holding them apart
//	                                             declares)
//	on master a802b47                            DECLARES — A false
//	rebase 到 origin/master f2d6d39 之上          DECLARES — A false
//	### 1) 在 origin/master a802b47（detached）   DECLARES — A false
//	- detached at origin/master `b12e9c743`       DECLARES — A false: nothing quotes
//	                                             before the ref, so the fence after
//	                                             the revision is the revision's own
func baseClaimRefQuotedShape(line string, after int, commit string) bool {
	q := baseClaimRefQuoteSpanRune(line, baseClaimRefStart(line, after), commit, after)
	if q == 0 {
		return false
	}
	at := strings.Index(line[after:], commit)
	if at < 0 {
		// The revision was read off this line by construction; a location that
		// cannot be re-found is not a reason to invent a quotation (Rule 15).
		return false
	}
	tail := line[after+at+len(commit):]
	if baseClaimTallyAfter(line, after, commit) {
		return true
	}
	for _, r := range tail {
		if strings.ContainsRune(baseClaimQuoteRunes, r) {
			return r == q
		}
	}
	return false
}

// baseClaimRefQuoteSpanRune returns the quoting rune that OPENS a quotation
// around the ref→revision pair — the A condition of baseClaimRefQuotedShape — or
// 0 when there is none.
//
// The rune read is the last quoting rune before the ref, and it must still be
// open when the revision ends: a rune that recurs in between has already closed,
// which is what keeps a two-cell table row ("| `refs/remotes/origin/master` |
// `c6e93569e` |") a DECLARATION — there the fence closes on the ref itself, so
// the pair never shares one quotation — while "| on master a802b47 | ok |" (one
// cell holding the whole pair) is a specimen.
func baseClaimRefQuoteSpanRune(line string, start int, commit string, after int) rune {
	if start > 0 && strings.ContainsRune(baseClaimQuoteRunes, rune(line[start-1])) {
		// The quotation hugs the pin itself ("`origin/master f2d6d39`"): that is
		// the pin's own fence, the shape a declaration is usually WRITTEN in, and
		// reading it as a specimen would silence the arm's positive controls.
		return 0
	}
	q := rune(0)
	for _, r := range line[:start] {
		if strings.ContainsRune(baseClaimQuoteRunes, r) {
			q = r
		}
	}
	if q == 0 {
		return 0
	}
	at := strings.Index(line[after:], commit)
	if at < 0 {
		return 0
	}
	if strings.ContainsRune(line[start:after+at+len(commit)], q) {
		return 0
	}
	return q
}

// baseClaimRefStart returns the offset at which the ref NAME the pin ends after
// begins — read by walking back over the rune class that can carry a ref name
// forward (the ONE definition, baseClaimRefPathRune), so the span test cannot
// disagree with what the pin matched.
func baseClaimRefStart(line string, end int) int {
	i := end
	for i > 0 && baseClaimRefPathRune(rune(line[i-1])) {
		i--
	}
	return i
}

// baseClaimRefPinIn returns the byte offset just past the ref on line when the
// ref DECLARES (see baseClaimRefCommitIn), or -1.
func baseClaimRefPinIn(line string) int {
	low := strings.ToLower(line)
	for _, ref := range baseClaimRefTokens {
		for from := 0; ; {
			j := strings.Index(low[from:], ref)
			if j < 0 {
				break
			}
			at := from + j
			from = at + len(ref)
			end := at + len(ref)
			if !baseClaimRefBoundary(low, end) || baseClaimRefIsACommand(line) {
				continue
			}
			// The revision is looked for past the ref's own closing punctuation
			// (the "| `refs/remotes/origin/master` | `c6e93569e` |" cell), never
			// past the ref itself, so nothing between them can be read as one.
			return end
		}
	}
	for _, qual := range baseClaimRefQualifiers {
		for from := 0; ; {
			j := strings.Index(low[from:], qual)
			if j < 0 {
				break
			}
			at := from + j
			from = at + len(qual)
			start := at + len(qual)
			end, ok := baseClaimBareNameEnd(low, start)
			if !ok || !baseClaimRefBoundary(low, end) ||
				baseClaimRefIsACommand(line) {
				continue
			}
			return end
		}
	}
	if end, ok := baseClaimBareBranchPinEnd(line); ok {
		return end
	}
	return -1
}

// baseClaimBareBranchPinEnd returns the offset just past a BARE branch name that
// a lead-in pins to a commit-shaped token ("on master a802b47"), and whether the
// line carries that shape.
//
// The three conditions are all structural, and each one is a measured briefing
// line this epic's census found unmeasured:
//
//	on master a802b47                        DECLARES  (row 01a17dc1's spelling)
//	rebase 到 origin/master f2d6d39 之上      DECLARES  (already read via the
//	                                         branch's own name following "到")
//	- 交付提交：`174a137`（rebase 到 `origin/master f2d6d39` 之上）  DECLARES
//	the report on master reports this red     silent — no revision named
func baseClaimBareBranchPinEnd(line string) (int, bool) {
	low := strings.ToLower(line)
	for _, lead := range baseClaimRefBranchLeadIns {
		for from := 0; ; {
			j := strings.Index(low[from:], lead)
			if j < 0 {
				break
			}
			at := from + j
			from = at + len(lead)
			if !baseClaimTermAtWordEdge(low[at:at+len(lead)], lead) {
				continue
			}
			if at > 0 && !baseClaimLeadInSeparator(rune(low[at-1])) {
				continue
			}
			start := at + len(lead)
			if start < len(low) && low[start] != ' ' && low[start] != '\t' {
				continue
			}
			for start < len(low) && (low[start] == ' ' || low[start] == '\t') {
				start++
			}
			end, ok := baseClaimBareNameEnd(low, start)
			if !ok || !baseClaimRefBoundary(low, end) {
				continue
			}
			// A name is only a REF here when the revision stands right after it:
			// the shape "lead-in name revision", never "lead-in name … prose".
			if baseClaimCommitAfter(line, end) == "" {
				continue
			}
			return end, true
		}
	}
	return -1, false
}

// baseClaimLeadInSeparator reports whether r may stand immediately before a
// branch lead-in word: whitespace, the punctuation a briefing puts around prose,
// or a CJK rune (this repo's briefings are bilingual, and "都 origin/master 的"
// is prose rather than a command — the commit-shape requirement above is what
// keeps that line silent).
func baseClaimLeadInSeparator(r rune) bool {
	if r == ' ' || r == '\t' || r == '\r' {
		return true
	}
	return strings.ContainsRune(baseClaimRefLeadInRunes, r)
}

// baseClaimRefLeadInRunes is the ONE definition of the punctuation class a
// declaration may put around a ref or a lead-in: fences, quotes, brackets,
// bullets, table rules, escapes.
const baseClaimRefLeadInRunes = "`'\"|#*->=_~•[](){}.!:;,\\&"

// baseClaimRefIsACommand reports whether the line the ref stands on is a git
// COMMAND — the criterion the epic's constraint 5 makes mandatory, because the
// same `origin/master` is a measurement object in one briefing and a comparison
// operand in the next.
//
//	origin/master a802b47                                  DECLARES
//	$ git merge-base --is-ancestor f6031ead9 origin/master  COMMAND
//	$ git log origin/master a802b47                         COMMAND
//	- detached at origin/master `b12e9c743` 上复现          DECLARES
//
// The reading is the line's FIRST ASCII word, never a substring search: a
// command begins with its verb (or with `git`), while a declaration begins with
// its prose lead-in — a bullet, a heading, a table rule, or CJK. Testing
// "does the line contain a revision verb" instead would swallow the third line
// above, whose declaration is exactly what this arm exists to keep.
func baseClaimRefIsACommand(line string) bool {
	first := baseClaimFirstLatinWord(line)
	if first == "" {
		return false
	}
	if first == baseClaimGitWord {
		return true
	}
	for _, verb := range baseClaimRevisionVerbs {
		if first == verb {
			return true
		}
	}
	return false
}

// baseClaimGitWord is the git command word itself: a line beginning with it is a
// command however its refs are spelled.
const baseClaimGitWord = "git"

// baseClaimFirstLatinWord returns the line's first run of ref-name runes — the
// first word that can belong to a command — lower-cased, or "" when the line has
// none. It walks from the START: that is what makes the reading POSITIONAL.
func baseClaimFirstLatinWord(line string) string {
	low := strings.ToLower(line)
	for i := 0; i < len(low); {
		if !baseClaimRefNameRune(rune(low[i])) {
			i++
			continue
		}
		j := i
		for j < len(low) && baseClaimRefNameRune(rune(low[j])) {
			j++
		}
		return low[i:j]
	}
	return ""
}

// baseClaimBareNameEnd returns the end of the branch name starting at start, and
// whether it was one: a qualified ref must be FOLLOWED by a real name
// ("origin/ master" names nothing, and neither does a bare "refs/").
func baseClaimBareNameEnd(low string, start int) (int, bool) {
	i := start
	for i < len(low) && (baseClaimRefNameRune(rune(low[i])) || low[i] == baseClaimRefPathSeparator) {
		i++
	}
	return i, i > start
}

// baseClaimRefNameRune reports whether r can belong to a ref's name. A Latin
// word rune covers every branch name this repo cuts (task/, feat/, master),
// and the hyphen is added because names carry it.
func baseClaimRefNameRune(r rune) bool {
	return baseClaimLatinWordRune(r) || r == baseClaimWordGlue
}

// baseClaimRefBoundary reports whether the ref ending at offset end is a WHOLE
// ref: "refs/remotes/origin/master" must not be read as "origin/master" ending
// in the middle of a longer path, and "origin/master" must not be read out of
// "origin/master-ish".
func baseClaimRefBoundary(low string, end int) bool {
	return end >= len(low) || !baseClaimRefNameRune(rune(low[end])) &&
		low[end] != baseClaimRefPathSeparator
}

// baseClaimBaseWordIn returns the byte offset just past the standalone base word
// on line, or -1 when the base letters on that line do not form one: either the
// line carries no base word at all, or the one it carries is a FRAGMENT of a
// longer hyphenated compound ("merge-base").
func baseClaimBaseWordIn(line string) int {
	i := baseClaimWordAt(line)
	if i < 0 || baseClaimWordGlued(line, i) {
		return -1
	}
	return i
}

// baseClaimWordGlued reports whether the base word ending at offset end is a
// FRAGMENT of a longer hyphenated compound — the spelling "merge-base" uses, and
// the one the Latin word-edge guard cannot see (a hyphen is not a Latin word
// rune, so the fragment looks "standalone" while naming nothing).
//
// The test is narrow on purpose, and one-sided: the word must be the TAIL of the
// compound (a hyphen on its LEFT, with a Latin word rune before that). Every
// measured declaration shape is untouched ("base 2f697e5", "| base commit |",
// "(base 2f697e5)", "基线2f697e5"), while a compound that merely STARTS with the
// letters ("base-commit 2f697e5") is still the base word declaring its revision
// and is not dropped — a silent hole in the instrument is the defect this whole
// family of fixes exists to close (Rule 15).
func baseClaimWordGlued(line string, end int) bool {
	low := strings.ToLower(line)
	for _, w := range baseClaimWords {
		if end < len(w) || !strings.HasPrefix(low[end-len(w):], w) {
			continue
		}
		start := end - len(w)
		return start >= 2 && line[start-1] == baseClaimWordGlue &&
			baseClaimLatinWordRune(rune(line[start-2]))
	}
	return false
}

// baseClaimRevisionVerbBefore reports whether a git revision verb names the text
// BETWEEN the base word and the candidate revision — the criterion that keeps a
// revision an argument of a command instead of a declared base.
//
// The window read is the text from the end of the base word up to the START of
// the revision, so a verb standing elsewhere on the line ("base 2f697e5 (re-run
// with git show)") does not swallow a declaration that is not its argument — the
// neighbourhood is what lies BETWEEN, never the whole line.
func baseClaimRevisionVerbBefore(line string, after int, commit string) bool {
	i := strings.Index(line[after:], commit)
	if i < 0 {
		// The token the predicate returned was spelled on this line by
		// construction; an unreadable location is not a reason to invent a
		// verb — the declaration stands (Rule 15: measure, never guess).
		return false
	}
	between := strings.ToLower(line[after : after+i])
	for _, verb := range baseClaimRevisionVerbs {
		if baseClaimTermAtWordEdge(between, verb) {
			return true
		}
	}
	return false
}

// baseClaimTermAtWordEdge reports whether term appears in low bounded by
// NON-word runes on both sides — the property that keeps "log" out of "catalog"
// and "diff" out of "different". The hyphen is deliberately NOT a separating
// rune here: it is the verbs' own internal spelling ("rev-list", "cat-file"), so
// treating it as glue would refuse every multi-word verb in the list.
func baseClaimTermAtWordEdge(low, term string) bool {
	for from := 0; ; {
		j := strings.Index(low[from:], term)
		if j < 0 {
			return false
		}
		at := from + j
		from = at + len(term)
		if at > 0 && baseClaimLatinWordRune(rune(low[at-1])) {
			continue
		}
		if at+len(term) < len(low) && baseClaimLatinWordRune(rune(low[at+len(term)])) {
			continue
		}
		return true
	}
}

// baseClaimWordAt returns the byte offset just past the EARLIEST standalone base
// word on the line, or -1 when the line declares no base. A Latin word is
// standalone when neither neighbour is a Latin letter or digit — the property
// that rejects "codebase"/"database" (whose "base" is glued to a letter on the
// left) while accepting "(base 2f697e5)" and "| base commit |". A NON-Latin base
// word (baseClaimGlueWords) carries the one measured exception: it is standalone
// even when a hexadecimal rune follows it directly, because that is the revision
// it declares ("基线2f697e5").
func baseClaimWordAt(line string) int {
	low := strings.ToLower(line)
	best := -1
	for _, w := range baseClaimWords {
		for from := 0; ; {
			j := strings.Index(low[from:], w)
			if j < 0 {
				break
			}
			at := from + j
			from = at + len(w)
			if !baseClaimWordBoundary(low, at, len(w)) {
				continue
			}
			if best < 0 || at < best {
				best = at
			}
			break
		}
	}
	if best < 0 {
		return -1
	}
	for _, w := range baseClaimWords {
		if strings.HasPrefix(low[best:], w) {
			return best + len(w)
		}
	}
	return -1
}

// baseClaimWordBoundary reports whether the word of width n starting at at is
// standalone: a Latin run must not be glued to a Latin letter or digit on either
// side, and a NON-Latin base word (baseClaimGlueWords) may carry the exception
// above — it is still a word when the next rune can start a revision.
func baseClaimWordBoundary(low string, at, n int) bool {
	if at > 0 && baseClaimLatinWordRune(rune(low[at-1])) {
		return false
	}
	if at+n < len(low) && baseClaimLatinWordRune(rune(low[at+n])) {
		// The ONE exception: a non-Latin base word followed directly by a rune
		// that can start a hexadecimal revision is the word and its revision,
		// not a longer word (measured shape "基线2f697e5").
		if baseClaimNonLatinWord(low, at, n) && isHexDigit(rune(low[at+n])) {
			return true
		}
		return false
	}
	return true
}

// baseClaimNonLatinWord reports whether the matched base word is one of the
// spellings the glue exception applies to (the CJK word this repo's briefings
// use) — never a Latin word, whose glue IS how "codebase" and "database" are
// spelled.
func baseClaimNonLatinWord(low string, at, n int) bool {
	for _, w := range baseClaimGlueWords {
		if strings.HasPrefix(low[at:at+n], w) {
			return true
		}
	}
	return false
}

// baseClaimLatinWordRune reports whether r can glue to a Latin word — the class
// whose membership makes "codebase" one word and "base)" two.
func baseClaimLatinWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// baseClaimCommitAfter returns the revision named after offset at: the ENCLOSED
// commit-shaped token when the line fences one (the shape a revision takes when
// it is NAMED rather than mentioned), else the first bare shaped token that
// BEGINS inside the look-ahead window. "" when the window names none.
//
// THE WINDOW IS A BOUND ON DISTANCE, NEVER A CUT (row 01a1373b, 2026-10-15): it
// is applied to a token's START, so a token beginning inside the window is
// returned WHOLE, however long it runs. Applying it to the TEXT instead cut a
// full-length revision standing right after the base word down to
// baseClaimLookaheadRunes-1 characters ("base " + 40 hex read as 31 hex) and
// returned a revision the author never wrote — which both producer faces then
// quoted into re-runnable git commands (task_briefing_premise.go,
// task_briefing_base_submit.go). The enclosure check reads the untruncated tail
// for the same reason: a fenced token's closing fence lies past its own start.
func baseClaimCommitAfter(line string, at int) string {
	tail := line[at:]
	toks := baseClaimTokensWithin(tail, baseClaimLookaheadRunes)
	for _, tok := range toks {
		if implClaimShaShape(tok) && baseClaimEnclosedAt(tail, tok) {
			return tok
		}
	}
	for _, tok := range toks {
		if implClaimShaShape(tok) {
			return tok
		}
	}
	return ""
}

// baseClaimEnclosedAt reports whether tok appears in one of the shapes a
// revision is named in — a backtick fence (implClaimFencedAt, that fence's ONE
// definition) or a parenthesised pair (unlandedClaimParenOpen/Close, the OTHER
// shape's one definition).
func baseClaimEnclosedAt(text, tok string) bool {
	return strings.Contains(text, unlandedClaimParenOpen+tok+unlandedClaimParenClose) ||
		implClaimFencedAt(text, tok)
}

// baseClaimTokensWithin returns every commit-shaped CANDIDATE of s whose START
// offset lies within the first n runes of s, in order — the look-ahead window as
// a bound on DISTANCE, never a cut on TEXT (row 01a1373b).
//
// The tokenisation is implClaimTokens' (the ONE definition of "what is a hex
// run"), applied to the WHOLE tail so that a token beginning inside the window
// is returned at FULL length; the window decides only which of those tokens are
// close enough to the base word to belong to it. The cursor walk below is exact
// rather than a heuristic: implClaimTokens emits maximal, non-overlapping runs
// in order, so each successive search lands on that token's own occurrence.
func baseClaimTokensWithin(s string, n int) []string {
	toks := implClaimTokens(s)
	if len(toks) == 0 {
		return nil
	}
	// baseClaimRuneWindow read as a MEASURE: the byte length of the at-most-n-rune
	// prefix IS the exclusive boundary of the window.
	limit := len(baseClaimRuneWindow(s, n))
	var out []string
	from := 0
	for _, tok := range toks {
		i := strings.Index(s[from:], tok)
		if i < 0 {
			continue
		}
		start := from + i
		from = start + len(tok)
		if start >= limit || baseClaimNameFragmentAt(s, start, len(tok)) {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// baseClaimNameFragmentAt reports whether s[start:start+n] is a FRAGMENT of a
// longer REF NAME rather than a whole name standing on its own.
//
// THE MEASURED CASE (row 01a1a61f, 2026-11-04 — the THIRD shell of this family,
// after 01a14511's command shape and 01a18232's quoted shape). Every
// clone-channel briefing states, as a fact about its own ground,
//
//	checked out on branch task/01a1a5dfacbb7462b9f36f412e73d6ba
//
// The bare-branch arm reads the lead-in "on", the name "branch", and then looks
// for a revision after it — and the hex run INSIDE the branch name
// (`task/<32hex>`, this row's own task id) is commit-SHAPED, so the scanner
// reported "briefing base → UNMEASURED (git rev-parse could not resolve
// 01a1a5df…)" on a briefing that declares no base at all. Measured on this
// rev before the fix:
//
//	on branch task/01a1a5dfacbb7462b9f36f412e73d6ba      claims=1  → 01a1a5df…
//	on branch task01a1a5dfacbb7462b9f36f412e73d6ba       claims=1  → 01a1a5df…
//	on master a802b47                                    claims=1  → a802b47   (WANTED)
//
// The criterion is therefore the token's OWN BOUNDARY, not its shape: a
// revision is a NAME of its own, so it can never be the tail of a slash path or
// the tail of a longer word. One rune of a ref name on EITHER side is enough to
// make it a fragment:
//
//	task/01a1a5df…   '/' before it   FRAGMENT — the hex is the branch's suffix
//	task01a1a5df…    'k' before it   FRAGMENT — the hex is glued into a word
//	a802b47          ' ' before it   WHOLE    — the wanted declaration
//	基线2f697e5       CJK before it   WHOLE    — the CJK glue arm (non-ASCII is
//	                                 not a ref-name rune; see baseClaimLatinWordRune)
func baseClaimNameFragmentAt(s string, start, n int) bool {
	if start > 0 && baseClaimRefPathRune(rune(s[start-1])) {
		return true
	}
	end := start + n
	return end < len(s) && baseClaimRefPathRune(rune(s[end]))
}

// baseClaimRefPathRune reports whether r can carry a ref name FORWARD — the
// class a token may not be glued to and still be a name of its own: the ref-name
// runes plus the path separator that joins a name to its namespace ("task/<id>").
func baseClaimRefPathRune(r rune) bool {
	return baseClaimRefNameRune(r) || r == baseClaimRefPathSeparator
}

// baseClaimRefPathSeparator is the character that separates a ref's namespace
// from its name ("task/<id>", "refs/remotes/origin/master"), and the ONE
// definition of it: the name walk, the boundary test and the fragment test must
// agree, or a name would end in one place for one reader and another place for
// the next.
const baseClaimRefPathSeparator = '/'

// baseClaimRuneWindow returns the prefix of s holding at most n runes (a byte
// offset would cut a multibyte word in half, and briefings here carry Chinese).
// It is read as a MEASURE, never as text to scan: the caller takes its BYTE
// LENGTH as the exclusive boundary of a rune window, because truncating the text
// before tokenising is exactly what cut a bare full-length revision in half
// (row 01a1373b).
func baseClaimRuneWindow(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
