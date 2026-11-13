package taskreport

// unlandedclaim.go — "X has NOT LANDED on the integration branch" as a CLAIM to
// be re-measured at dispatch time, never as a fact.
//
// THE MEASURED CASE (row 01a12c7b, 2026-10-12): an epic briefing carried the
// sentence "row 01a12c4a is completed but its fix never reached master, fix the
// read path in this run's worktree", rendered from a SNAPSHOT of the
// integration branch taken at submit time (base 305ae03d0, 03:37:55). The
// identical fix (9867ed461) was an ancestor of origin/master by 03:44:05, and
// the run was handed the briefing at 03:58 — so the run spent its whole budget
// re-implementing landed work and then had to deduplicate against the branch it
// had not been told about.
//
// This file is the MIRROR of implclaim.go, and it is deliberately the SAME
// machinery read backwards: implclaim.go finds the sentences that assert the
// integration branch ALREADY HAS something (a claim that can be FALSE), this one
// finds the sentences that assert it does NOT (a premise that can go STALE).
// The judgement is not made here either — it is the object-graph measurement of
// internal/tools/master_claim.go (`git merge-base --is-ancestor <c> master`,
// whose EXIT STATUS is the verdict), taken by the caller AGAINST THE ROW'S OWN
// REPO at the moment the child is spawned.
//
// The predicate is CONSERVATIVE in the same direction implclaim.go chose: the
// failure this check must never make is stamping an honest sentence. So a
// sentence needs THREE things at once — a token naming the integration branch, a
// phrase asserting the thing is not there, and no explicit landed marker — and a
// body whose claims cannot be measured yields no stamp at all (the caller STATES
// that, it does not pass it off as a pass: Rule 15).
//
// The package stays a LEAF (stdlib only, constants + pure text functions), so
// the spawn seam, the CLI face and any future reader share ONE definition of
// "what a stale premise looks like" (Rule 14).

import "strings"

const (
	// unlandedClaimMaxSentences caps how many claims a briefing yields. A
	// briefing with a systemic problem must not push the child's prompt past
	// what a reader reads, and the first few name the pattern (the same cap
	// discipline implClaimMaxSentences follows).
	unlandedClaimMaxSentences = 32

	// unlandedClaimSentenceMaxRunes bounds a quoted sentence in the record the
	// caller renders: a briefing bullet can be a paragraph, and a stamp has to
	// stay a stamp.
	unlandedClaimSentenceMaxRunes = 200

	// unlandedClaimEllipsis marks a sentence the stamp truncated.
	unlandedClaimEllipsis = "…"

	// unlandedClaimParenOpen / unlandedClaimParenClose are the OTHER shape a
	// revision is named in (beside the backtick fence implClaimFencedAt owns):
	// the motivating row's sentence spells its subject and its revision as "The
	// row 01a12c4a … its fix (9867ed461)" — a row id and a sha have the same
	// shape, so the ENCLOSING punctuation is the only discriminator the text
	// carries.
	unlandedClaimParenOpen  = "("
	unlandedClaimParenClose = ")"
)

// unlandedClaimBranchTokens are the shapes a sentence uses to NAME the
// integration branch BESIDE its real name: reports and briefings in this repo
// say "master", and they also say it in Chinese. The branch's own name is a
// PARAMETER (the caller passes tools.IntegrationBranchName), so this leaf never
// mirrors the literal (Rules 1b/13/14) — these are only the aliases.
var unlandedClaimBranchTokens = []string{
	"主分支", "主库", "主干", "主线",
}

// unlandedClaimAssertions are the phrases that assert the thing is NOT on the
// integration branch. Every phrase here is branch-agnostic on purpose: the
// branch is decided by unlandedClaimBranchTokens, so a phrase carrying the
// branch name would mirror the literal the parameter exists to avoid.
var unlandedClaimAssertions = []string{
	"not an ancestor", "not on", "isn't on", "never landed", "not landed",
	"did not land", "didn't land", "has not landed", "hasn't landed",
	"never merged", "not merged", "was not merged", "absent from",
	"missing from", "not in", "failed to land", "fails to land", "unlanded",
	"未落地", "没有落地", "未落到", "没落到", "未合入", "未合并", "不在",
}

// unlandedClaimLandedMarkers are the phrases that make a sentence say the thing
// IS there — the truthful shape for this detector, and the reason the assertion
// list above can be as broad as it is ("not landed" would otherwise fire on
// "… has already landed"). Their presence anywhere in the sentence excludes it.
var unlandedClaimLandedMarkers = []string{
	"already landed", "already merged", "already on", "already an ancestor",
	"has landed", "is an ancestor", "已落地", "已合入", "已合并", "已经落地",
}

// UnlandedClaim is one sentence that asserts the integration branch does NOT
// have something, together with the revision it names as its subject ("" when
// it names none — itself a finding: a premise nobody can re-measure).
type UnlandedClaim struct {
	// Sentence is the claim, quoted verbatim (nothing here rewrites prose).
	Sentence string
	// Commit is the commit-shaped token the sentence names ("" when it names
	// none).
	Commit string
	// Branch is the integration branch name the sentence used.
	Branch string
}

// UnlandedClaims is the scanner's verdict over a briefing body: the section that
// was read and the claims it makes.
type UnlandedClaims struct {
	// Section is the heading the claims were read from ("" when the whole body
	// was read — a briefing with neither an evidence nor a verification
	// section, which is the ordinary shape for a dispatched row).
	Section string
	// Claims are the claims, in order of appearance, deduplicated by sentence.
	Claims []UnlandedClaim
}

// UnlandedClaimsIn scans a briefing body for sentences asserting the named
// integration branch does NOT have something. The sections it reads are the ones
// a report states results in — Evidence when the body declares one, else
// Verification, else the whole body — the SAME three-way choice ImplClaimsIn and
// GateClaimVerdict make, so the three readers can never disagree about where a
// result lives.
func UnlandedClaimsIn(body, integrationBranch string) UnlandedClaims {
	v := UnlandedClaims{}
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
		if !unlandedClaimSentence(s, branch) || seen[s] {
			continue
		}
		seen[s] = true
		v.Claims = append(v.Claims, UnlandedClaim{Sentence: s, Commit: unlandedClaimCommit(s), Branch: branch})
		if len(v.Claims) >= unlandedClaimMaxSentences {
			break
		}
	}
	return v
}

// UnlandedClaimSentenceExcerpt quotes a claim for a stamped line, bounded so the
// record stays a record (the stamp's own budget, Rule 1: no bare width).
func UnlandedClaimSentenceExcerpt(sentence string) string {
	s := strings.TrimSpace(sentence)
	r := []rune(s)
	if len(r) <= unlandedClaimSentenceMaxRunes {
		return s
	}
	return string(r[:unlandedClaimSentenceMaxRunes]) + unlandedClaimEllipsis
}

// unlandedClaimSentence is the ONE predicate over one sentence: it names the
// integration branch, it asserts the thing is not there, and it carries no
// landed marker.
func unlandedClaimSentence(sentence, branch string) bool {
	low := strings.ToLower(sentence)
	if hasAny(low, unlandedClaimLandedMarkers) {
		return false
	}
	if !unlandedClaimNamesBranch(low, branch) {
		return false
	}
	return hasAny(low, unlandedClaimAssertions)
}

// unlandedClaimNamesBranch reports whether the sentence names the integration
// branch — by the name it was given, or by one of the aliases above.
func unlandedClaimNamesBranch(low, branch string) bool {
	if strings.Contains(low, strings.ToLower(branch)) {
		return true
	}
	return hasAny(low, unlandedClaimBranchTokens)
}

// unlandedClaimCommit extracts the commit-shaped token a sentence names ("" when
// it names none). The ENCLOSED shapes win — a backtick fence, or parentheses —
// because that is how a revision is NAMED in prose, and it is the only
// discriminator available in this leaf: the motivating row's own sentence spells
// a ROW id and its fix side by side ("The row 01a12c4a is completed but its fix
// (9867ed461) never landed on master"), and the two have the SAME shape (eight
// hex characters carrying a letter and a digit). Choosing the enclosed token is
// what reads the revision and not the row. When nothing is enclosed the first
// shaped token is taken, and a token that is really a row id then measures
// UNMEASURED — STATED as such by the caller (Rule 15), never a pass.
func unlandedClaimCommit(sentence string) string {
	for _, tok := range implClaimTokens(sentence) {
		if unlandedClaimEnclosedAt(sentence, tok) && implClaimShaShape(tok) {
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

// unlandedClaimEnclosedAt reports whether tok appears in one of the shapes a
// revision is named in: a backtick fence (implClaimFencedAt — that fence's one
// definition lives with its owner, Rule 14) or a parenthesised pair.
func unlandedClaimEnclosedAt(sentence, tok string) bool {
	return strings.Contains(sentence, unlandedClaimParenOpen+tok+unlandedClaimParenClose) ||
		implClaimFencedAt(sentence, tok)
}
