package taskreport

// gateclaim.go — THE GATE-CLAIM CONTRACT: what a report may state about a
// gate, and what the harness does about it.
//
// WHY (the measured case, task 01a0af50, 2026-09-17): the report's Gate status
// line asserted "`git status --porcelain` empty in both worktrees (Rule 30)"
// while the SAME run's close-out machinery printed five entries for that very
// worktree.
//
// devdoc-ref-ok: the listing quoted below is the MACHINERY's raw output,
// reproduced verbatim; the devdoc path in it names a file that run had created
// and left untracked, so it is a dirty-worktree entry, not a citation.
//
// ("worktree is DIRTY (uncommitted work) — left for a human —
// entries: M cmd/agent/merger_takeover.go | ?? cmd/agent/merger_takeover_scope.go
// | ?? devdoc/merger-takeover-scope-escape.md"). Both statements are about the
// same worktree, in the same run: the report asserted a measurement its own
// process had just contradicted. A reviewer has no instrument for that — the
// claim is doing the work of a measurement nobody performed.
//
// THE CONTRACT (Shape A where the claim is the agent's, Shape B where the fact
// is the framework's, and the two combine — see devdoc/report-gate-claim.md):
//
//   - Shape A — a gate claim carries the observation it rests on: the command
//     and the output, the exit code, the measured path (a red proof of an
//     operator's verbatim failure; a /proc/<pid>/cgroup reading). The claim
//     the report really owns is "I ran X and saw Y", and that is checkable.
//   - Shape B — a fact the FRAMEWORK measures at close-out (the worktree's
//     porcelain state) is the framework's to report, on the row's own trail.
//     The report does not restate it; if it must be mentioned, it names the
//     framework as the source, which is itself an observation (it points at
//     the measurement that exists).
//   - The framework does NOT re-run the gates a run claims (that doubles
//     cost). It RECORDS the sentences that assert a result with nothing
//     behind them, quoting them verbatim on the run's own append-only trail,
//     and says so on stderr. It never rewrites a run's prose — the artifact of
//     record keeps its author's sentences byte for byte, and the finding
//     rides beside them (Rule 15 — no silent failure, no silent rewrite).
//
// WHERE it fires: SealTaskReport, at the run's terminal writeback — the ONE
// choke point every close-out passes through (the placement the residue
// record uses, so no door bypasses it).
//
// WHAT it adjudicates: the `SectionEvidence` section when the report declares
// one; otherwise the run's `## Verification` section (the motivating shape —
// a report that made its gate claims under Verification, with no evidence
// section at all); otherwise the whole body.
//
// The predicate is deliberately CONSERVATIVE (the shape of the contract, not
// the shape of a lie detector): a sentence is flagged only when it names a
// framework-measured ground and observes nothing, or asserts a result word
// with neither an observation, an observation verb, nor the instrument that
// produced the result. The motivating run's TRUE claims — the red proof, the
// cgroup acceptance measurement — are observation-bearing sentences and are
// untouched by it; solving this epic by suppressing them would be the worse
// defect.
//
// The package is a LEAF: stdlib only, constants + pure text functions, so the
// writer (internal/tools), the reader and every future face share ONE
// definition of the contract (Rule 14).

import "strings"

const (
	// SectionEvidence is the GATE-CLAIM section: where a run states a gate
	// result together with the observation that stands behind it. It answers
	// the reviewer's question "did anything actually look?" — either with the
	// observation itself or by naming the framework-owned measurement that
	// carries it.
	SectionEvidence = "## Evidence (self-filed)"

	// EvidencePlaceholder is the harness skeleton's body for the evidence
	// section: the two accepted shapes, stated where a model that reads the
	// skeleton (and every reviewer) sees them.
	EvidencePlaceholder = "one line per gate claim: the command and the output you saw, or one of the " +
		"two accepted shapes (self-filed evidence WITH its output — e.g. exit code, /proc measurement, " +
		"red-then-green proof; or a claim about something the framework measures itself — the worktree state, " +
		"the branch's merge state — which the framework reports and this report must not restate)"

	// GroundWorktreePorcelain / GroundWorktreeStatus are the worktree-state
	// grounds the framework measures for the run at close-out — the evidence a
	// report lacks by construction, whichever way the agent ran the command.
	// Matched against the LOWERCASED sentence, so the spelling the motivating
	// case used ("git status --porcelain") and the tool result's own casing
	// ("git status") are both caught.
	GroundWorktreePorcelain = "git status --porcelain"
	GroundWorktreeStatus    = "git status"

	// GroundFrameworkReport is the sanctioned Shape-B delegation: the sentence
	// NAMES the framework's own measurement as the source of the fact.
	GroundFrameworkReport = "framework report"

	// SectionHeadingPrefix is the markdown level-2 prefix every section
	// heading shares — one definition the section scanner reads with the
	// headings themselves (Rule 14).
	SectionHeadingPrefix = "## "

	// sectionHeadingIndentChars are the characters that put a line at column 0.
	// They exist here so the heading test says "the line starts at column 0"
	// without writing the character set inline twice (Rule 1), and so a future
	// reader meets the one definition of indentation this package recognises.
	sectionHeadingIndentChars = " \t"

	// MaxRecordedClaimSentences caps how many offending sentences the digest
	// records: a report with a systemic problem must not push the row's digest
	// past what an operator reads, and the first few name the pattern.
	MaxRecordedClaimSentences = 3

	// ClaimWarningLabel is the stderr label of the seal-time finding.
	ClaimWarningLabel = "gate-claim (unobserved)"

	// ClaimDigestPrefix introduces the recorded finding in the ledger's result
	// digest: the sentences ride the append-only task_events detail, so a
	// reviewer reaches them from the row even after the session directory is
	// pruned (the same durability the report path itself gets).
	ClaimDigestPrefix = "gate-claims-unobserved: "

	// claimRecordSeparator separates the recorded sentences, which are quoted
	// verbatim — gateSentenceSeparators already cut on it, so a sentence can
	// never contain it.
	claimRecordSeparator = " | "
)

// claimEvidences are the phrases that MAKE a sentence a statement of
// observation: a pasted output, a bash result's own exit marker, the arrow the
// run's reports use for a measured transition, a ground-truth path, or a
// delegation to the framework's measurement. Matched case-insensitively
// ANYWHERE in one sentence — including inside a parenthesized caveat, because
// the motivating sentence and its caveat live on one line.
var claimEvidences = []string{
	"output:",
	"exit=",
	"exit code",
	"exit 0",
	"exit 1",
	"→",
	"->",
	"observed",
	"measured",
	"/proc/",
	"framework",
}

// claimResults are the words that ASSERT an outcome — the shape that needs the
// observation behind it.
var claimResults = []string{
	"pass",
	"passed",
	"green",
	"clean",
	"empty",
	"ok",
	"exit 0",
	"success",
	"succeed",
	"succeeded",
	"verified",
	"reproduces",
	"reproduce",
	"reproduced",
	"fails",
	"failed",
	"failure",
	"red",
	"reports",
	"reported",
	"is free",
	"free",
}

// claimObservationVerbs mark a sentence as a statement OF OBSERVATION — what
// the run ran or what it SAW. Both of the motivating run's TRUE claims live
// here ("reproduces the operator's verbatim permission denied and turns green";
// "acceptance measures /proc/<pid>/cgroup at …"), which is exactly why they
// stay expressible.
var claimObservationVerbs = []string{
	"ran ",
	"run ",
	"runs ",
	"running ",
	"executed ",
	"turns ",
	"turning ",
	"measure",
	"measures",
	"reproduces",
	"reproduced",
	"read ",
	"reads ",
	"printed",
	"prints",
	"wrote",
	"writes",
	"found ",
	"shows ",
	"showed",
	"observed",
}

// claimInstruments answer "what produced this result?" — the command names a
// report uses when it compresses the observation ("`go test ./... -run X
// -count=1`: ok"). A sentence that names its instrument has named the source
// of the result, which is the compressed observation this project's own report
// template prescribes (PROJECT.md Rule 5's "- Build: <output>").
var claimInstruments = []string{
	"go build", "go test", "go vet", "go run", "gofmt", "git diff", "git log",
	"git rev-parse", "git show", "make ", "gopi ", "./", "/proc",
}

// gateSentenceSeparators split a section body into the claims it states. A
// newline alone is NOT a boundary: "|" is how the run's own gate-status line
// demarcates its claims, and ";" is how the close-out summary does.
var gateSentenceSeparators = ";|"

// claimBulletMarkers are the list markers a claim starts with — the two the
// report templates emit (the contract's "- " and the skeleton's "* ") plus the
// ordered form. ONE definition (Rule 1b), shared by the unit splitter and the
// per-unit marker strip.
var claimBulletMarkers = []string{claimBulletStar, claimBulletDash, claimBulletPlus}

// isClaimBulletLine reports whether a line opens a new list item. A wrapped
// bullet continues on its own indented line, so the unit a claim lives in is
// the BULLET (marker + its wrapped lines), not the physical line.
func isClaimBulletLine(line string) bool {
	s := strings.TrimSpace(line)
	for _, m := range claimBulletMarkers {
		if strings.HasPrefix(s, m) {
			return true
		}
	}
	if i := strings.Index(s, claimBulletDigit); i > 0 && digitsOnly(s[:i]) {
		return true
	}
	return false
}

// Bullet markers stripped from a candidate sentence before adjudication: they
// belong to the rendering, not to the sentence, and a claim quoted back to a
// reviewer must read as the author wrote it. The set is the two the report
// templates emit (the contract's "- " and the skeleton's "* ") plus the
// ordered form, each defined ONCE here (Rule 1b).
const (
	claimBulletStar  = "* "
	claimBulletDash  = "- "
	claimBulletPlus  = "+ "
	claimBulletDigit = ". "
)

// ClaimVerdict is the gate-claim check's verdict over a report body: Section
// names the text that was adjudicated (the evidence section's heading, the
// Verification section's heading, or "" for the whole body), and
// MissingSentences are the sentences that assert a gate result with no
// observation behind them (empty = every claim speaks to what was observed).
type ClaimVerdict struct {
	Section          string
	MissingSentences []string
}

// GateClaimVerdict adjudicates a report body's gate claims. It never mutates
// anything: this package owns the adjudication, the caller owns what to do
// with a missing observation.
func GateClaimVerdict(body string) ClaimVerdict {
	v := ClaimVerdict{Section: SectionEvidence}
	evidenceHeading, evidence := sectionBody(body, SectionEvidence)
	if evidenceHeading == "" {
		verificationHeading, verification := sectionBody(body, VerificationHeading())
		if verificationHeading != "" {
			v.Section, evidence = verificationHeading, verification
		} else {
			// No section declares the run's gate results: the whole body is
			// what the report asserts, so the whole body is adjudicated.
			v.Section, evidence = "", body
		}
	}
	v.MissingSentences = unobservedClaimSentences(evidence)
	return v
}

// DigestRecord renders the digest fragment for a verdict ("" when the report's
// claims are all observed / absent), capping the recorded sentences.
func DigestRecord(v ClaimVerdict) string {
	if len(v.MissingSentences) == 0 {
		return ""
	}
	n := len(v.MissingSentences)
	if n > MaxRecordedClaimSentences {
		n = MaxRecordedClaimSentences
	}
	return ClaimDigestPrefix + strings.Join(v.MissingSentences[:n], claimRecordSeparator)
}

// ClaimWarning renders the operator line(s) for a verdict: the finding's label,
// the section it was found under, and each offending sentence. Callers print
// it on stderr — a finding that is recorded but never shown is half a record.
func ClaimWarning(taskID string, v ClaimVerdict) string {
	if len(v.MissingSentences) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(ClaimWarningLabel)
	if taskID != "" {
		b.WriteString(" ")
		b.WriteString(taskID)
	}
	b.WriteString(" — under ")
	if v.Section == "" {
		b.WriteString("the report body")
	} else {
		b.WriteString(v.Section)
	}
	b.WriteString(": assert an observation or name the framework's own report\n")
	for _, s := range v.MissingSentences {
		b.WriteString("  ")
		b.WriteString(s)
		b.WriteString("\n")
	}
	return b.String()
}

// isSectionHeadingLine reports whether line is a section heading: the heading
// text at COLUMN 0 of a line of its own.
//
// REQUIRE COLUMN 0, and that is the whole of the test. `strings.TrimSpace` made
// an INDENTED line indistinguishable from a heading, so a wrapped bullet whose
// text opened with `## Verification` was read as the heading itself:
// sectionBody started there, returned the bullet's fragment as the section body,
// and the real `## Verification` section below it — three `- Ran:` lines and all
// — was never read. RunCheckVerdict therefore recorded `ran-line-missing`
// against a report that stated every invocation (task 01a19466).
//
// The END side asks the same question with SectionHeadingPrefix, so the two
// edges of a section can never disagree about which line is a section's own.
//
// WHAT THIS DELIBERATELY DOES NOT REQUIRE, and the measurement behind it: a
// stricter predicate also asks that the heading text be followed by a space or
// the end of the line (markdown's own rule), which keeps a column-0 sentence
// that OPENS with a heading's words (`## Files — and the rest of the sentence`)
// out of the heading set. Measured over the 1450 real task reports on this host,
// that extra requirement changes NO RunCheckVerdict reading and turns 121
// `## Not Done（如实）` / `## Not Done (honest)` headings — the author's own
// truthful suffix, a real heading in every reader's eye — into "missing
// section" findings. A gate that fires on honest work gets switched off (the
// measured reasoning behind Rule 31's record-not-refuse choice), so the
// separator is NOT required: the residual is a column-0 continuation that opens
// with a heading's text, which no indented wrap can produce.
func isSectionHeadingLine(line, heading string) bool {
	if heading == "" {
		return false
	}
	if line != strings.TrimLeft(line, sectionHeadingIndentChars) {
		return false // the line is INDENTED: a continuation, never a heading
	}
	return strings.HasPrefix(line, heading)
}

// sectionBody returns the heading and the body under it (up to the next
// level-2 heading), both "" when the body carries no such section.
//
// Both edges are decided by isSectionHeadingLine, so an indented continuation
// that opens with a heading's text is prose on both: it cannot START a section
// and it cannot END one.
func sectionBody(body, heading string) (string, string) {
	if heading == "" {
		return "", ""
	}
	lines := strings.Split(body, "\n")
	start := -1
	for i, line := range lines {
		if isSectionHeadingLine(line, heading) {
			start = i
			break
		}
	}
	if start < 0 {
		return "", ""
	}
	var b strings.Builder
	for _, line := range lines[start+1:] {
		if isSectionHeadingLine(line, SectionHeadingPrefix) {
			break
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return heading, b.String()
}

// claimSentences splits a section body into the sentences a claim can live in.
//
// The unit is the BULLET, not the physical line: a wrapped bullet (marker on
// the first line, the rest indented underneath) is ONE claim, and splitting it
// per line separated `- post-fix run … →` from the `ok github.com/…` that
// carries its evidence — a measured false positive on this epic's own report.
// Separators split WITHIN a bullet; a newline ends one only when the next line
// opens a new bullet.
func claimSentences(body string) []string {
	var units []string
	var cur strings.Builder
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			units = append(units, s)
		}
		cur.Reset()
	}
	for _, line := range strings.Split(body, "\n") {
		if isClaimBulletLine(line) || strings.TrimSpace(line) == "" {
			flush()
		} else if cur.Len() > 0 {
			cur.WriteString(" ") // a wrapped continuation of the same claim
		}
		cur.WriteString(line)
		cur.WriteString("\n")
	}
	flush()

	var out []string
	for _, u := range units {
		fields := strings.FieldsFunc(u, func(r rune) bool {
			return strings.ContainsRune(gateSentenceSeparators, r)
		})
		for _, f := range fields {
			if s := trimClaimBullet(strings.TrimSpace(f)); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// trimClaimBullet removes one leading list marker from a candidate sentence.
func trimClaimBullet(s string) string {
	for _, m := range claimBulletMarkers {
		if strings.HasPrefix(s, m) {
			return strings.TrimSpace(strings.TrimPrefix(s, m))
		}
	}
	if i := strings.Index(s, claimBulletDigit); i > 0 && digitsOnly(s[:i]) {
		return strings.TrimSpace(s[i+len(claimBulletDigit):])
	}
	return s
}

// digitsOnly reports whether s is a non-empty run of ASCII digits.
func digitsOnly(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// unobservedClaimSentences returns the sentences of a section that assert a
// gate result with no observation behind them, deduplicated in order of
// appearance (a repeated sentence is one defect, not four).
func unobservedClaimSentences(section string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range claimSentences(section) {
		if !sentenceClaimsUnobserved(s) || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// sentenceClaimsUnobserved is the ONE predicate: does this sentence assert a
// gate result without observation, on a ground the framework owns?
//
// Order matters and is the whole design:
//  1. NAME a framework-owned ground (a measurement the report cannot own) and
//     observe nothing → the motivating defect, whatever the vocabulary.
//  2. observe something anywhere in the sentence ("output:", an exit code, a
//     "/proc/" reading, a delegation to the framework) → exits. Both of the
//     motivating run's TRUE claims land here.
//  3. no result word, but written as an observation verb → an honest
//     narrative, exits.
//  4. name the INSTRUMENT that produced the result ("go test …: ok") → the
//     compressed observation GOTI.md Rule 5's template prescribes, exits.
//  5. assert a result with neither observation nor instrument → flagged.
func sentenceClaimsUnobserved(sentence string) bool {
	low := strings.ToLower(sentence)
	if frameworkGroundClaim(low) {
		return true
	}
	if hasAny(low, claimEvidences) {
		return false
	}
	if !hasAny(low, claimResults) {
		return false
	}
	if hasAny(low, claimObservationVerbs) || hasAny(low, claimInstruments) {
		return false
	}
	return true
}

// frameworkGroundClaim applies rule 1: a sentence naming a framework-measured
// ground, asserting a result about it, and carrying no observation at all.
func frameworkGroundClaim(low string) bool {
	if !hasAny(low, []string{GroundWorktreePorcelain, GroundWorktreeStatus, GroundFrameworkReport}) {
		return false
	}
	if hasAny(low, claimEvidences) {
		return false
	}
	// The bare "is clean" / "empty" / "nothing" forms are the same claim in
	// fewer words ("my worktree is clean (Rule 30)", "git status --porcelain
	// prints nothing"), so the result test accepts them alongside the result
	// words — but ONLY on this branch, where the sentence has already named a
	// framework-measured ground (so "## Not Done: nothing" is never touched).
	if hasAny(low, claimResults) {
		return true
	}
	return hasAny(low, groundResultAssertions)
}

// groundResultAssertions are the two-word forms of a result assertion on a
// framework ground (Rule 1b: named, one definition).
var groundResultAssertions = []string{groundCleanAssertion, groundEmptyAssertion, groundNothingAssertion, groundNoOutputAssertion}

const (
	groundCleanAssertion    = "is clean"
	groundEmptyAssertion    = "empty"
	groundNothingAssertion  = "nothing"
	groundNoOutputAssertion = "no output"
)

// hasAny reports whether s contains any of the phrases.
func hasAny(s string, phrases []string) bool {
	for _, p := range phrases {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}
