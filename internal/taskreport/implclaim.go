package taskreport

// implclaim.go — "X IS ALREADY ON THE INTEGRATION BRANCH" as a CLAIM to be
// re-measured, never as a fact.
//
// THE MEASURED CASE (session 01a0f3de): auditing a feature against master, the
// run took the tip of an ARCHIVED branch (feat/capture-self-window, tip
// e1701ac8, held alive only by the tag refs/tags/archive/feat/capture-self-window)
// and reported it as how master implements the feature — "already on master, no
// gap" — while its own observations in the same run said the branch ref was
// gone. Both judgements were about one fact, and nothing forced them to agree:
// a commit kept reachable by an archive tag answers every `git show`, so an
// operation that SUCCEEDS reads exactly like evidence.
//
// This file owns only the TEXT half of the fix — finding the sentences that
// make such a claim and the commit each one names — because that half is a
// reading of prose and has to live somewhere reviewable. The JUDGEMENT is not
// made here: it is an object-graph measurement taken by the caller
// (internal/tools/master_claim.go: `git merge-base --is-ancestor <c> master`,
// whose EXIT STATUS is the verdict). Splitting the two is what keeps the
// criterion mechanical — this package is a LEAF (stdlib only, constants + pure
// text functions), so the seal, the CLI face and any future reader share ONE
// definition of "what a claim looks like" (Rule 14).
//
// The predicate is deliberately CONSERVATIVE, in the same direction
// runcheck.go chose: the failure this check must never make is flagging an
// honest report. A sentence that says the work NEVER landed ("e1701ac8 is not
// an ancestor of master", "never merged") is the truthful shape and is
// excluded by the negation vocabulary below. What remains is the sentence that
// asserts the integration branch ALREADY HAS the thing.

import (
	"strings"
	"unicode"
)

// Integration branch tokens: the branch NAME is a parameter (the caller passes
// tools.IntegrationBranchName) so the leaf never mirrors the literal (Rules
// 1b/13/14). Only the shapes a sentence uses to NAME it live here.
const (
	// implClaimBacktick is the inline-code fence: a revision named in a report
	// is usually fenced, and a fenced token is unambiguous whatever it holds.
	implClaimBacktick = "`"

	// implClaimMaxSentences caps how many claims a body yields: a report with a
	// systemic problem must not push a caller's digest past what an operator
	// reads, and the first few name the pattern.
	implClaimMaxSentences = 32

	// implClaimShaMinLen / implClaimShaMaxLen bound a commit-shaped token. git
	// accepts an unambiguous abbreviation, and this repo's reports use the
	// 8-digit form; 40 is a full sha1.
	implClaimShaMinLen = 7
	implClaimShaMaxLen = 40
)

// implClaimNegations are the markers that make a sentence the TRUTHFUL shape
// ("the work never landed") rather than an implementation claim. Their presence
// anywhere in the sentence excludes it — deliberately over-broad, because a
// false record on an honest report is the defect this check must not create.
var implClaimNegations = []string{
	"not an ancestor", "isn't", " is not ", "not on", "not merged", "never",
	"no longer", "absent", "missing", "does not", "doesn't", "did not", "didn't",
	"cannot", "can't", "unlanded", "fails to", "未", "不在", "没有", "非",
}

// implClaimPredicates are the words that make a sentence an assertion that the
// integration branch ALREADY HAS the thing: "already landed on master",
// "master already implements …", "exists on master", "superseded by master",
// "no gap", "no genuine delta". At least one must appear alongside the branch
// name for the sentence to be a claim.
var implClaimPredicates = []string{
	"already", "exists", "exist on", "implements", "implemented", "has it",
	"landed", "present on", "supersed", "no gap", "no genuine", "no delta",
	"redundant", "duplicate", "covered on", "available on", "已实现", "已有",
	"已经", "无缺口",
}

// Commit-shaped tokens are found by scanning, not by a regexp (the leaf stays
// stdlib-simple). The shape is pinned by two properties, and the second is what
// keeps ordinary English out: a token of only [a-f] letters is a word
// ("defaced", "effaced"), and a token of only digits is a count or a year — a
// real abbreviated sha essentially always carries BOTH.
const (
	implClaimShaNeedsLetter = true
	implClaimShaNeedsDigit  = true
)

// ImplClaim is one sentence that asserts the integration branch already has
// something, together with the commit it names as its proof ("" when the
// sentence names none — itself a finding: a claim nobody can re-measure).
type ImplClaim struct {
	// Sentence is the claim, quoted verbatim (a record must carry the author's
	// own words; nothing here rewrites prose).
	Sentence string
	// Commit is the commit-shaped token the sentence names ("" when it names
	// none).
	Commit string
	// Branch is the integration branch name the sentence used.
	Branch string
}

// ImplClaims is the scanner's verdict over a report body: the section that was
// read and the claims it makes.
type ImplClaims struct {
	// Section is the heading the claims were read from ("" when the whole body
	// was read — a report with neither an evidence nor a verification section).
	Section string
	// Claims are the implementation claims, in order of appearance,
	// deduplicated by sentence.
	Claims []ImplClaim
}

// ImplClaimsIn scans a report body for implementation claims about the named
// integration branch. The sections it reads are the ones a report states
// results in — the evidence section when the report declares one, else
// Verification, else the whole body — the SAME three-way choice GateClaimVerdict
// makes, so the two records can never disagree about where a report's results
// live.
func ImplClaimsIn(body, integrationBranch string) ImplClaims {
	v := ImplClaims{}
	branch := strings.TrimSpace(integrationBranch)
	if branch == "" {
		return v
	}
	text := body
	if _, evidence := sectionBody(body, SectionEvidence); evidence != "" {
		v.Section = SectionEvidence
		text = evidence
	} else if heading, verification := sectionBody(body, VerificationHeading()); verification != "" {
		v.Section = heading
		text = verification
	}
	seen := map[string]bool{}
	for _, s := range claimSentences(text) {
		if !implClaimSentence(s, branch) || seen[s] {
			continue
		}
		seen[s] = true
		v.Claims = append(v.Claims, ImplClaim{Sentence: s, Commit: implClaimCommit(s), Branch: branch})
		if len(v.Claims) >= implClaimMaxSentences {
			break
		}
	}
	return v
}

// implClaimSentence is the ONE predicate over one sentence: it names the
// integration branch, it asserts that the branch already has the thing, and it
// is not one of the truthful negated shapes.
func implClaimSentence(sentence, branch string) bool {
	low := strings.ToLower(sentence)
	for _, n := range implClaimNegations {
		if strings.Contains(low, strings.ToLower(n)) {
			return false
		}
	}
	if !hasAny(low, []string{strings.ToLower(branch)}) {
		return false
	}
	return hasAny(low, implClaimPredicates)
}

// implClaimCommit extracts the commit-shaped token a sentence names ("" when
// it names none). A fenced token wins: a report that fences a revision is
// naming it as a revision, and a backticked token is unambiguous whatever
// letters it holds.
func implClaimCommit(sentence string) string {
	for _, tok := range implClaimTokens(sentence) {
		if implClaimFencedAt(sentence, tok) && implClaimShaShape(tok) {
			return tok
		}
	}
	for _, tok := range implClaimTokens(sentence) {
		if implClaimShaShape(tok) {
			return tok
		}
	}
	return ""
}

// implClaimTokens returns every maximal run of hex digits in the sentence, in
// order (the runs ARE bounded by non-hex characters, which is what stops a
// match inside a longer word).
func implClaimTokens(sentence string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range sentence {
		if isHexDigit(r) {
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

// implClaimShaLetter reports whether r is an ASCII hex LETTER. It accepts BOTH
// cases on purpose, and the measured reason is the epic this scanner shipped
// with (row 01a13715, 2026-10-15): the predicate used to look for [a-f] only, so
// an author who spelled a revision the way some tools print it ("2F697E5")
// produced no letter, the token was refused, and the seam went SILENTLY silent
// — indistinguishable from a briefing that declares no base at all. Measured
// against real git, an upper-case revision is the SAME object as its lower-case
// spelling (`git rev-parse --verify --quiet 95F4DE7^{commit}` →
// 95f4de7123f7…; `git merge-base --is-ancestor 95F4DE7 master` → exit=0), so
// refusing it was a false negative of the predicate, not a conservatism.
func implClaimShaLetter(r rune) bool {
	return (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

// implClaimShaShape decides whether a hex run is commit-SHAPED: long enough to
// be an abbreviation, short enough to be a sha1, and carrying both a letter and
// a digit (the property that separates a revision from an English word made of
// [a-f] and from a bare count).
func implClaimShaShape(tok string) bool {
	if len(tok) < implClaimShaMinLen || len(tok) > implClaimShaMaxLen {
		return false
	}
	// ONE definition of "a hex letter", case-insensitive: the helper's own doc
	// records the measured false negative this class used to carry ([a-f] only,
	// so "2F697E5" was silent).
	letter := strings.IndexFunc(tok, implClaimShaLetter) >= 0
	digit := strings.IndexFunc(tok, func(r rune) bool { return r >= '0' && r <= '9' }) >= 0
	if implClaimShaNeedsLetter && !letter {
		return false
	}
	if implClaimShaNeedsDigit && !digit {
		return false
	}
	return true
}

// implClaimFencedAt reports whether tok appears inside a backtick fence.
func implClaimFencedAt(sentence, tok string) bool {
	fenced := implClaimBacktick + tok + implClaimBacktick
	return strings.Contains(sentence, fenced)
}

// isHexDigit reports whether r is an ASCII hex digit.
func isHexDigit(r rune) bool {
	if r > unicode.MaxASCII {
		return false
	}
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}
