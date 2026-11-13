package taskreport

// delegated.go — "the RESULT of this gate lives at <path>" as a shape to be
// REMEMBERED, never as a verdict.
//
// THE MEASURED CASE (row 01a12d6b, 2026-10-12). A run's done-gate chain is
// build → tests → write the report → task(action=commit/review), so "the report
// is written and the row is complete" and "the acceptance command has no verdict
// yet" are the SAME state. The run's `## Verification` therefore carried:
//
//	- **Tests**: `make test-budget PKGS='./internal/testutil'` → see the stream at
//	  `artifacts/make-testbudget-testutil.txt`.
//
// That sentence states NO result — it hands the conclusion to a path — and
// nothing at close-out looked at it. The run afterwards observed EXIT=2 with two
// red tests, and the report stayed as it was: a reviewer reading
// "Tests: … see the stream at <path>" cannot tell a green acceptance from one
// that was never green, and the path is not even required to exist.
//
// WHAT THIS FILE OWNS is only the TEXT half: recognising the sentence that
// delegates a gate conclusion to a path while stating no result, and returning
// the paths it names. Whether each path EXISTS at seal time is a FILESYSTEM
// fact and stays with the caller (internal/tools), which owns the report's own
// directory (Rule 14: this package stays a LEAF — stdlib only, constants and
// pure text functions, callers own every side effect).
//
// THE ACTION IS (A) RECORD — never a rewrite, never a refusal — the discipline
// gateclaim.go established and the reason is also theirs: this project's reports
// are full of honest sentences whose subject the seal cannot see (a stream
// measured in a SIBLING worktree, a red proof captured at another commit, an
// artifact written by the harness). A refusal keyed on this text alone would
// refuse honest runs, and a gate that fires on honest work is switched off
// within a day (Rule 31's own measured justification, devdoc/rule31-ran-check.md).
//
// THE PREDICATE IS CONSERVATIVE, and the direction is deliberate: the failure
// this check must never make is stamping a report that DID state its result. So
// a sentence needs FOUR things at once — an instrument that produced the result,
// a path token to delegate to, a delegation phrase, and NO result marker in its
// own prose — and the result test is applied to the prose with the path tokens
// REMOVED, because a filename is not a verdict (`artifacts/red-capture.txt` is
// how a run names a red proof it DID state elsewhere).

import (
	"fmt"
	"strings"
)

const (
	// delegatedClaimMaxSentences caps how many delegating sentences a report
	// yields: a report with a systemic problem must not push the caller's
	// digest past what an operator reads, and the first few name the pattern
	// (the cap discipline claimSentences/implClaimMaxSentences follow).
	delegatedClaimMaxSentences = 3

	// delegatedClaimSentenceMaxRunes bounds a quoted sentence in the record the
	// caller renders: a bullet can be a paragraph, and a stamp has to stay a
	// stamp (the shape unlandedClaimSentenceMaxRunes pinned).
	delegatedClaimSentenceMaxRunes = 200

	// delegatedClaimEllipsis marks a sentence the record truncated.
	delegatedClaimEllipsis = "…"

	// delegatedClaimSeparator separates recorded delegations from each other
	// and a delegation's paths from its sentence. It never occurs in a sentence
	// — claimSentences already cut on "|", so a sentence cannot contain it.
	delegatedClaimSeparator = " | "

	// delegatedPathMaxTokens caps the paths recorded per sentence: a sentence
	// naming a dozen artifacts is one defect, not twelve.
	delegatedPathMaxTokens = 3

	// DelegatedDigestPrefix introduces the recorded finding in the ledger's
	// result digest: the delegating sentences ride the append-only task_events
	// detail, so a reviewer reaches them from the row after the session
	// directory is pruned (the durability the report path itself gets).
	DelegatedDigestPrefix = "report-result-delegated: "

	// DelegatedWarningLabel is the stderr label of the seal-time finding — the
	// same `[task] `-prefixed family the gate-claim record and the ran-check
	// record belong to, so one line of a close-out names its own family.
	DelegatedWarningLabel = "result-delegation"

	// delegatedBacktick is the inline-code fence a path is usually spelled in.
	delegatedBacktick = "`"

	// delegatedPathSlash is the separator a path token must carry: a bare
	// filename ("out.txt") is a file in the reading shell's own directory, not
	// an address a reader can follow, and the motivating sentence names
	// `artifacts/…` (relative to the run's session directory).
	delegatedPathSlash = "/"

	// DisabledPathHomePrefix marks the other absolute spelling a report uses
	// for the run's own home ("~/.gopi/sessions/<id>/artifacts/x.txt"). It is
	// EXPORTED so the caller (which owns `os.Stat`) can refuse to guess a home
	// directory for it instead of measuring the wrong file (Rule 14).
	DelegatedPathHomePrefix = "~/"

	// delegatedPathExtensionSep / delegatedPathExtensionMinLen /
	// delegatedPathExtensionMaxLen shape the LAST segment of a path token: a
	// delegation names a FILE to read (`.txt`, `.md`, `.log`, `.json`), which is
	// what separates `artifacts/stream.txt` from the INSTRUMENT's own argv
	// (`./bin/gopi`, `./internal/testutil`) — the instrument is not the
	// artifact, and a directory the report merely ran in is not a result.
	delegatedPathExtensionSep = "."
	delegatedPathExtensionMin = 1
	delegatedPathExtensionMax = 8
	delegatedSchemeSeparator  = "://"
	delegatedTrailingTrim     = "`'\"),.;:"
	delegatedLeadingTrim      = "`'\"(<["
	// delegatedPathMinTokenRunes is the shortest token that can be an address.
	delegatedPathMinTokenRunes = 3

	// delegatedSectionLabelJoin joins two section headings when a verdict was
	// read from both (the union DelegatedClaimsIn adjudicates): the caller
	// renders it as the label of the text that was read.
	delegatedSectionLabelJoin = " + "

	// delegatedFence is the markdown code fence. What a fence holds is a
	// TRANSCRIPT — the `Ran:` block a report pastes verbatim, a shell script,
	// an artifact's dump — not the report's prose about an outcome, so the
	// adjudication SKIPS it. Measured: without this clause the only hits a
	// 369-report corpus produced beyond the motivating sentence were a pasted
	// shell script (session 01a0dec6) and nothing else; and a `Ran:` block
	// listing `./bin/gopi … 2>&1 | tee artifacts/x.txt` is exactly the surface
	// Rule 31 wants RUN, so flagging it would punish the behaviour the rules ask
	// for.
	delegatedFence = "```"

	// delegatedPathWindowRunes bounds how far AFTER a delegation phrase a path
	// may sit and still be the thing that phrase hands the reader to. Measured:
	// the window is what separates the motivating sentence ("see the stream at
	// `artifacts/…`", the path immediately after the phrase) from a sentence
	// that states a result and then says "(see Not Done)" in passing while a
	// path appears EARLIER in it (session 01a10f9c, whose hit this window
	// removes).
	delegatedPathWindowRunes = 120
)

// delegatedInstruments answer "what produced the result this sentence is
// delegating?" — the command names a report uses. A sentence that names no
// instrument has not delegated a MEASURED conclusion anywhere; it is prose.
var delegatedInstruments = []string{
	"make ",
	"go test",
	"go build",
	"go run",
	"go vet",
	"gopi ",
	"./bin/gopi",
	"gofmt",
}

// delegatedPhrases are the ways a report hands a conclusion to a path instead
// of stating it. Both spellings this project's reports actually use are here
// (English "see the stream at" — the motivating sentence — and the Chinese
// "见 …" / "详见 …" its operator-facing reports use).
var delegatedPhrases = []string{
	"see the stream at",
	"see the stream",
	"stream at",
	"see the output at",
	"output at",
	"log at",
	"see ",
	"see:",
	"refer to ",
	"cf. ",
	"见 ",
	"详见",
	"参见",
}

// delegatedResultMarkers are the phrases that make a sentence STATE a result —
// the shape this check must never stamp. Deliberately NARROWER than
// gateclaim's claimResults/claimEvidences, and for a measured reason: the
// motivating sentence carries the delegation arrow "→" ("`make …` → see the
// stream at `…`"), so the arrow — an evidence marker in the gate-claim
// vocabulary — is NOT a result here, while the appendage that follows it is
// ("→ ok", "→ 0 red").
var delegatedResultMarkers = []string{
	"exit=",
	"exit code",
	"exit 0",
	"exit 1",
	"exit ",
	"exited",
	" ok",
	"ok ",
	"=ok",
	"pass",
	"green",
	"red",
	"clean",
	"empty",
	"failed",
	"failure",
	"fails",
	"output:",
	"observed",
	"measured",
	"success",
	"silent",
	"zero",
	"no output",
	"turns ",
}

// delegatedResultSections returns the heading label of the text a delegation
// can live in and that text itself: the UNION of the Verification section and
// the evidence section, whichever are present, in the order the report states
// them (Verification first — the section a report compresses its gate results
// into). Both "" when the body carries neither section, so the caller
// adjudicates the whole body.
func delegatedResultSections(body string) (string, string) {
	var label string
	var b strings.Builder
	seen := map[string]bool{}
	for _, heading := range []string{VerificationHeading(), SectionEvidence} {
		if heading == "" || seen[heading] {
			continue
		}
		seen[heading] = true
		found, text := sectionBody(body, heading)
		if found == "" {
			continue
		}
		if label == "" {
			label = found
		} else {
			label += delegatedSectionLabelJoin + found
		}
		b.WriteString(text)
		b.WriteString("\n")
	}
	if label == "" {
		return "", ""
	}
	return label, stripDelegatedFences(b.String())
}

// stripDelegatedFences removes every fenced block (and the fence lines
// themselves) from the text under adjudication. A fence holds a transcript, not
// the report's prose about an outcome — see delegatedFence.
func stripDelegatedFences(text string) string {
	lines := strings.Split(text, "\n")
	var b strings.Builder
	inside := false
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), delegatedFence) {
			inside = !inside
			continue
		}
		if inside {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// DelegatedClaim is one sentence that delegates a gate conclusion to a path
// while stating no result.
type DelegatedClaim struct {
	// Sentence is the claim, quoted verbatim (a record must carry the author's
	// own words; nothing here rewrites prose).
	Sentence string
	// Paths are the path tokens the sentence delegates to, in order of
	// appearance, deduplicated (empty is impossible — a claim without a path
	// is not a delegation).
	Paths []string
}

// DelegatedClaims is the scanner's verdict over a report body: the section that
// was read and the delegating sentences it holds.
type DelegatedClaims struct {
	// Section is the heading the claims were read from ("" when the whole body
	// was read — a report with neither an evidence nor a verification section).
	Section string
	// Claims are the delegating sentences, in order of appearance, deduplicated
	// by sentence.
	Claims []DelegatedClaim
}

// DelegatedClaimsIn scans a report body for sentences that delegate a gate
// result to a path. The text it reads is the UNION of the two sections a report
// states results in — Verification and the evidence section — when either is
// present, else the whole body.
//
// The union is deliberate, and it is a MEASURED correction to the three-way
// choice GateClaimVerdict/ImplClaimsIn make: the motivating report carries BOTH
// sections, and its delegating sentence sits under `## Verification` while the
// evidence section (which a run fills with its red proofs) is present and
// non-empty. A "first present section wins" rule reads the evidence section and
// misses the defect entirely — measured against the motivating report itself
// (session 01a12d6b: claims=0 with the three-way choice, claims=1 with the
// union). Neither section can be dropped, and a report with no section at all
// is still adjudicated on its whole body (so deleting a heading cannot dodge
// this check).
func DelegatedClaimsIn(body string) DelegatedClaims {
	v := DelegatedClaims{}
	text := body
	if heading, union := delegatedResultSections(body); union != "" {
		v.Section, text = heading, union
	}
	seen := map[string]bool{}
	for _, s := range claimSentences(text) {
		if seen[s] {
			continue
		}
		anchor := delegatedPhraseIndex(s)
		if anchor < 0 {
			continue
		}
		paths := delegatedPaths(s, anchor)
		if len(paths) == 0 || !sentenceDelegatesResult(s, paths) {
			continue
		}
		seen[s] = true
		v.Claims = append(v.Claims, DelegatedClaim{Sentence: DelegatedSentenceExcerpt(s), Paths: paths})
		if len(v.Claims) >= delegatedClaimMaxSentences {
			break
		}
	}
	return v
}

// delegatedPhraseIndex returns the rune index at which the sentence hands the
// reader to something else (the EARLIEST delegation phrase), or -1 when the
// sentence delegates nothing. It is the anchor the path window is measured
// from: what a delegation hands over is what comes AFTER it.
func delegatedPhraseIndex(sentence string) int {
	low := strings.ToLower(sentence)
	best := -1
	for _, p := range delegatedPhrases {
		i := strings.Index(low, p)
		if i < 0 {
			continue
		}
		if best < 0 || i < best {
			best = i
		}
	}
	if best < 0 {
		return -1
	}
	return len([]rune(low[:best]))
}

// sentenceDelegatesResult is the ONE predicate: does this sentence hand a
// measured conclusion to a path while stating no result of its own?
//
// Order matters and is the whole design:
//  1. name the INSTRUMENT that produced the result → otherwise it is prose;
//  2. name a delegation PHRASE → otherwise it is a finding, not a hand-off;
//  3. state no RESULT in its own prose → otherwise it delegated an ALREADY
//     STATED conclusion and a reader is not left guessing; the result test runs
//     on the sentence with its path tokens removed, because a filename is not a
//     verdict.
func sentenceDelegatesResult(sentence string, paths []string) bool {
	low := strings.ToLower(sentence)
	if !hasAny(low, delegatedInstruments) {
		return false
	}
	if !hasAny(low, delegatedPhrases) {
		return false
	}
	return !hasAny(delegatedProse(sentence, paths), delegatedResultMarkers)
}

// delegatedProse strips every path token (and the fence around it) from a
// sentence, so the result test judges what the author WROTE about the outcome
// rather than the names of the files they wrote it into.
func delegatedProse(sentence string, paths []string) string {
	prose := strings.ToLower(sentence)
	for _, p := range paths {
		prose = strings.ReplaceAll(prose, strings.ToLower(delegatedBacktick+p+delegatedBacktick), " ")
		prose = strings.ReplaceAll(prose, strings.ToLower(p), " ")
	}
	return prose
}

// delegatedPaths extracts the path tokens a sentence hands the reader to: a
// whitespace-delimited token starting AT OR AFTER the delegation phrase (and
// within delegatedPathWindowRunes of it — see that constant), carrying a
// separator, spelled as a file (its last segment has a short extension), never
// a URL. Deduplicated, capped, in order of appearance.
func delegatedPaths(sentence string, anchor int) []string {
	var out []string
	seen := map[string]bool{}
	runes := []rune(sentence)
	start := 0
	for i := 0; i <= len(runes); i++ {
		if i < len(runes) && !isDelegatedSpace(runes[i]) {
			continue
		}
		if i > start && start >= anchor && start <= anchor+delegatedPathWindowRunes {
			tok := strings.Trim(string(runes[start:i]), delegatedTrailingTrim)
			tok = strings.TrimLeft(tok, delegatedLeadingTrim)
			tok = strings.TrimRight(tok, delegatedTrailingTrim)
			if delegatedPathShape(tok) && !seen[tok] {
				seen[tok] = true
				out = append(out, tok)
				if len(out) >= delegatedPathMaxTokens {
					break
				}
			}
		}
		start = i + 1
	}
	return out
}

// isDelegatedSpace reports whether r separates tokens (the whitespace a report
// wraps at — a newline inside a bullet is a continuation, not a boundary).
func isDelegatedSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

// delegatedPathShape reports whether a token is a path a reader can follow to
// an artifact: it carries a separator, is long enough to be an address, is not
// a URL, and its last segment is a FILE (a short extension) rather than the
// instrument's own argv.
func delegatedPathShape(tok string) bool {
	if len(tok) < delegatedPathMinTokenRunes {
		return false
	}
	if !strings.Contains(tok, delegatedPathSlash) && !strings.HasPrefix(tok, DelegatedPathHomePrefix) {
		return false
	}
	if strings.Contains(tok, delegatedSchemeSeparator) {
		return false
	}
	last := tok[strings.LastIndex(tok, delegatedPathSlash)+1:]
	i := strings.LastIndex(last, delegatedPathExtensionSep)
	if i <= 0 {
		return false
	}
	ext := last[i+len(delegatedPathExtensionSep):]
	if len(ext) < delegatedPathExtensionMin || len(ext) > delegatedPathExtensionMax {
		return false
	}
	for _, r := range ext {
		if !isDelegatedExtRune(r) {
			return false
		}
	}
	return true
}

// isDelegatedExtRune reports whether r may appear in an artifact's extension.
func isDelegatedExtRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	return false
}

// DelegatedPathStatus is the seal-time / read-time status of one delegated
// path. The STATUS VOCABULARY lives here, in the leaf, so the seal's record and
// the reader's render cannot describe the same path differently (Rule 14); the
// MEASUREMENT (`os.Stat`) stays with the caller, which owns the filesystem —
// this package never touches it.

const (
	// DelegatedPathStatusAbsent is the status of a path that is not there when
	// the report is closed out: a DANGLING delegation — the report hands the
	// reader to a file that does not exist, so the conclusion it delegates to
	// can never be recovered by anyone.
	DelegatedPathStatusAbsent = "ABSENT — no such file at seal time (dangling delegation)"

	// delegatedPathStatusPresentFmt renders a path that IS there, with its
	// size: the delegation is then RECOVERABLE, and what the record says is the
	// weaker, true thing — the report itself states no result, read the file.
	delegatedPathStatusPresentFmt = "present, %d bytes (recoverable — the report states no result itself)"

	// delegatedPathStatusUnreadableFmt renders a stat failure that is not
	// "not found" (a permission error, an I/O error): the path's existence is
	// UNKNOWN, and saying so is the honest record (Rule 15 — a silent unknown is
	// the defect).
	delegatedPathStatusUnreadableFmt = "status UNKNOWN — stat failed: %s"
)

// DelegatedPathStatusPresent renders the status of a delegated path that is
// present, with its size.
func DelegatedPathStatusPresent(size int64) string {
	return fmt.Sprintf(delegatedPathStatusPresentFmt, size)
}

// DelegatedPathStatusUnreadable renders the status of a delegated path whose
// existence could not be established (a stat error that is not "not found").
func DelegatedPathStatusUnreadable(err error) string {
	return fmt.Sprintf(delegatedPathStatusUnreadableFmt, err)
}

// DelegatedSentenceExcerpt bounds a quoted sentence for a record: a bullet can
// be a paragraph, and a stamp has to stay a stamp.
func DelegatedSentenceExcerpt(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= delegatedClaimSentenceMaxRunes {
		return string(r)
	}
	return string(r[:delegatedClaimSentenceMaxRunes]) + delegatedClaimEllipsis
}

// DelegatedWarning renders the operator line(s) for a verdict: the finding's
// label, the section it was found under, each delegating sentence, and the
// paths it names. The PATH STATUS (does it exist at seal time) is the caller's
// to add — it owns the filesystem (this package's leaf contract); a caller that
// passes no status appends nothing, which is a weaker record, never a wrong one.
func DelegatedWarning(taskID string, v DelegatedClaims, status map[string]string) string {
	if len(v.Claims) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(DelegatedWarningLabel)
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
	b.WriteString(": a gate conclusion is delegated to a path and no result is stated — state the result (and let the artifact stand behind it)\n")
	for _, c := range v.Claims {
		b.WriteString("  ")
		b.WriteString(c.Sentence)
		b.WriteString("\n")
		for _, p := range c.Paths {
			b.WriteString("    ")
			b.WriteString(p)
			if s := status[p]; s != "" {
				b.WriteString(" — ")
				b.WriteString(s)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// DelegatedRecord renders the digest fragment for a verdict ("" when the report
// delegates nothing). pathStatus maps a path to its seal-time status line (the
// caller's filesystem fact — "absent (dangling)" / "present, N bytes"); a path
// the caller did not status is recorded WITHOUT a verdict rather than with an
// invented one (Rule 15).
func DelegatedRecord(v DelegatedClaims, pathStatus map[string]string) string {
	if len(v.Claims) == 0 {
		return ""
	}
	parts := make([]string, 0, len(v.Claims))
	for _, c := range v.Claims {
		var p strings.Builder
		p.WriteString(c.Sentence)
		for _, path := range c.Paths {
			p.WriteString(delegatedClaimSeparator)
			p.WriteString(path)
			if s := pathStatus[path]; s != "" {
				p.WriteString(" [")
				p.WriteString(s)
				p.WriteString("]")
			}
		}
		parts = append(parts, p.String())
	}
	return DelegatedDigestPrefix + strings.Join(parts, delegatedClaimSeparator)
}
