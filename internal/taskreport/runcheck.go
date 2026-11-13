package taskreport

// runcheck.go — RULE 31's checked face: does the report SAY the surface was
// RUN, and if not, does it state a reason?
//
// WHY (the measured case, 2026-09-21): `gopi task sync --push-only --task-id T`
// documents in its own help text that "with --task-id T exactly that row
// travels". It was implemented, it compiled, the suite passed, and it was
// reported done. ONE real invocation found it broken twice over
// (`memsync push: no sync_peers declared`; then `claim-remote: want 1..8
// row(s), got 2181`). The report said "implemented" and carried Build:/Tests:
// only — the pre-Rule-31 template — so the reviewer had no line that could
// have asked the question, and the operator found the defect instead.
//
// Rule 31 (GOPI.md) now states the standard: "a command you did not run is not
// delivered" — the report must carry a `Ran:` line with the exact invocation,
// and Rule 5's skeleton gained the `Ran:`/`Result:` lines to match. A rule is a
// sentence; a sentence is not a check (原则无效，检查才有效 — Rule 30's own
// phrasing, and the reason the dirty-worktree incident happened twice). This
// file is the check.
//
// THE CONTRACT, in three clauses (each one is a decision the code below makes
// ONCE, with the failing direction chosen deliberately):
//
//  1. WHAT IS LOOKED AT: the run's `## Verification` section, whose heading is
//     read from VerificationHeading() — never re-spelled here (Rules 1b/13).
//     No Verification section at all is a NO-OP: "the section is missing" is
//     MissingSections' finding, and re-deciding it here would double-report one
//     defect (and would let a report dodge this check by deleting the section
//     while MissingSections' own contract stands unchanged). When the heading is
//     empty the check no-ops rather than panicking — an absent section list is
//     not a report's fault.
//
//  2. WHAT SATISFIES IT: a `Ran:` TOKEN LINE inside that section — the token at
//     the start of a line (after optional indentation and ONE list marker),
//     with a non-empty value after the colon. That is the line-prefix semantics
//     MissingSections already uses ("a heading at the start of the trimmed line
//     satisfies the section; a mid-line mention does not"), applied to the line
//     Rule 31 requires. So `Ran: ./bin/gopi task sync --push-only …` satisfies
//     it, the word inside prose ("I did not run this") does not, and an EMPTY
//     `Ran:` does not either — an empty line is not a measurement, and letting
//     it satisfy the check would be the same silent hole in a new costume.
//
//  3. WHAT EXEMPTS IT (the honesty clause, and the one that decides whether
//     this is a useful check or one that punishes honesty): a run that genuinely
//     CANNOT run the surface says so, and Rule 31 says exactly that in
//     `## Not Done`. Two accepted forms, because a rule's one sentence and an
//     author's habit differ:
//     - the declared `Blocked:` token line, anywhere in the body (a limit
//       stated beside the section it belongs to, its value the reason), or
//     - a STATED LIMIT in the `## Not Done` section: at least one content line
//       that is not a content-free placeholder. "nothing" / "(none)" / "n/a" is
//       not a limit — it is a claim of completeness — and neither is the
//       harness skeleton's own "not recorded" marker (a hole, not a reason).
//     The exemption is deliberately LENIENT in the document's direction: the
//     failure this check must never make is flagging an honest report.
//
// THE ACTION IS (A) RECORD — never a refusal, and the justification is
// MEASURED, not aesthetic (see devdoc/rule31-ran-check.md): of the 899 real
// reports under tasks/ and devdoc/ that carry a `## Verification` section,
// 898 carry no `Ran:` line — they are the pre-Rule-31 shape, authored by runs
// that did report honestly and DID run their surfaces. A refusal keyed on this
// predicate would have refused ~99.9% of the reports in this repository, i.e.
// a gate that fires on honest work, which gets switched off within a day — and
// a disabled gate is worse than no gate. The seal therefore RECORDS the gap
// (the row's append-only digest + stderr) exactly as gateclaim.go records an
// unobserved claim, and an operator reads it at the moment the artifact of
// record is closed out. The record is a gap, not an accusation: it says the
// report does not SAY the surface was run.
//
// THE LIMIT OF THIS CHECK (stated so nobody mistakes it for something else): it
// adjudicates PRESENCE, never ADEQUACY. It cannot tell whether the invocation
// on the `Ran:` line was the right one, whether it reached the surface that
// changed, or whether its output was acceptable — that is a reviewer's
// judgment, and a mechanical check that pretended to make it would be lying.
//
// The package stays a LEAF (Rule 14): stdlib only, constants + pure text
// functions, so the writer (the seal), the reader (`task show`) and every
// future face share ONE definition of the contract.

import "strings"

const (
	// RanLinePrefix is Rule 31's line token: the report states the surface it
	// executed, verbatim, with its real arguments. Rule 5's skeleton emits it,
	// and the check looks for exactly this token at the start of a line.
	RanLinePrefix = "Ran:"

	// ResultLinePrefix is the companion token Rule 5's skeleton emits next to
	// it (what the run actually observed). It is NOT part of the predicate —
	// the observation belongs to the same statement as the invocation, and a
	// `Result:`-only report has stated no invocation at all — but it is named
	// here so the contract's two line tokens have ONE definition (Rule 14)
	// rather than being re-spelled by any face that prints them.
	ResultLinePrefix = "Result:"

	// BlockerLinePrefix is the declared-limit token (contract clause 3): a line
	// whose value is the reason the surface could not be run. It may sit
	// anywhere in the report — the exemption must never be lost because the
	// author stated the limit beside the section it belongs to instead of
	// inside `## Not Done`.
	BlockerLinePrefix = "Blocked:"

	// RanCheckDigestPrefix introduces the recorded finding in the ledger's
	// result digest. It rides the append-only task_events detail (the durability
	// the report path and the gate-claim record already get), so a reviewer
	// reaches the gap from the row after the session directory is pruned.
	RanCheckDigestPrefix = "ran-line-missing: "

	// RanCheckWarningLabel is the stderr label of the seal-time finding — the
	// same `[task] `-prefixed family the gate-claim record and the Rule-30
	// residue arm belong to, so one line of a close-out names its own family.
	RanCheckWarningLabel = "ran-check"
)

// RanCheckNote is the finding's sentence: what is absent, where it belongs, and
// what satisfies it. A record that says only "missing" leaves the reader to
// guess the remedy; this one names the line to write.
const RanCheckNote = "no `" + RanLinePrefix + "` line — Rule 31 wants the exact invocation that exercised the changed surface, with its observed output (a harness-written skeleton counts as absent: an empty `" +
	RanLinePrefix + "` is not a measurement)"

// skeletonHolePrefix is the harness skeleton's own placeholder opening, matched
// against a marker-stripped `## Not Done` line. The skeleton does NOT state a
// limit ("the model wrote no report" is a hole, not a reason), so it must not
// exempt the report from the check. The match is the placeholder's opening
// WITHOUT its list marker — the marker is stripped before matching, and the
// skeleton emits the marker itself (sectionPlaceholder → `* (not recorded…)`).
// TestSkeletonHolePrefixMatchesThePlaceholder pins this against
// SkeletonPlaceholder so the two can never drift (Rule 13).
const skeletonHolePrefix = "(not recorded"

// declaredRanNoValueMarkers are the values that declare "this token names no
// invocation" — the skeleton's own hole marker (skeletonNoInvocationValue)
// among them, since that is what the harness's fallback emits. They are NOT a
// `Ran:` line (clause 2: an empty `Ran:` is not a measurement) and they are NOT
// a stated limit either, so the finding stands and the reason is looked for
// where Rule 31 puts it (`## Not Done` / BlockerLinePrefix).
var declaredRanNoValueMarkers = []string{
	skeletonNoInvocationValue, skeletonNoObservationValue,
	"(empty)", "(none)", "(nothing)", "n/a", "—", "-", "(not run)", "(unknown)",
}

// skeletonNoInvocationValue is the skeleton's declared no-invocation marker:
// the harness runs nothing, and the marker says so instead of leaving a bare
// colon that could be mistaken for a measurement. The predicate recognises it
// so the skeleton is never read as satisfying Rule 31 — while a model that
// copies it has declared a reason, not an invocation.
const skeletonNoInvocationValue = "(no invocation — the harness executed no surface itself)"

// skeletonNoObservationValue is the companion marker under ResultLinePrefix.
const skeletonNoObservationValue = "(no observation)"

// ranValueIsEmpty reports whether a `Ran:` line's value fails to state an
// invocation: nothing at all, or one of the declared no-value markers.
func ranValueIsEmpty(val string) bool {
	low := strings.ToLower(strings.TrimSpace(val))
	if low == "" {
		return true
	}
	for _, m := range declaredRanNoValueMarkers {
		if strings.HasPrefix(low, strings.ToLower(m)) {
			return true
		}
	}
	return false
}

// noLimitPhrases are the content-free `## Not Done` entries: they assert that
// nothing was left undone, which is the opposite of stating a limit. Matched
// against the marker-stripped, trimmed line.
var noLimitPhrases = []string{
	"(nothing)", "nothing", "(none)", "none", "(n/a)", "n/a", "-", "—",
}

// RunCheck is the run-check's verdict over a report body.
type RunCheck struct {
	// Section is the heading whose content was adjudicated — VerificationHeading()
	// when the report carries that section, "" when it does not (the no-op case:
	// MissingSections owns a missing section, so nothing is recorded here).
	Section string
	// RanLine reports whether a `Ran:` token line with a non-empty value was
	// found in the adjudicated section.
	RanLine bool
	// Blocker is the stated limit that exempts the report ("" = none): the
	// declared `Blocked:` value, or the first stated-limit line of `## Not Done`.
	// It is kept for the record, so the trail shows WHY a silent verdict was
	// silent rather than only that it was.
	Blocker string
}

// Missing reports whether the finding fires: the section was adjudicated, it
// carries no `Ran:` line, and the report states no limit. This is the ONLY
// predicate a caller needs — the three clauses above are decided here, once.
func (c RunCheck) Missing() bool {
	return c.Section != "" && !c.RanLine && c.Blocker == ""
}

// RunCheckVerdict adjudicates a report body. It never mutates anything: this
// package owns the adjudication, the caller owns what to do with the gap.
func RunCheckVerdict(body string) RunCheck {
	v := RunCheck{}
	heading := VerificationHeading()
	if heading == "" {
		return v // no contract heading is defined — nothing to adjudicate
	}
	// A section that is ABSENT contributes nothing (MissingSections owns that
	// finding); a section that is present but empty IS adjudicated — it says
	// nothing about the surface, which is exactly what this check looks for.
	found, verification := sectionBody(body, heading)
	if found == "" {
		return v
	}
	v.Section = found
	v.RanLine = !ranValueIsEmpty(ranValue(verification))
	v.Blocker = statedBlocker(body)
	return v
}

// statedBlocker returns the report's stated limit ("" when it states none):
// the declared `Blocked:` value when present, else the first stated-limit line
// of the `## Not Done` section. The declared token wins so a report that both
// declares a limit and lists deferred items records the reason it gave.
func statedBlocker(body string) string {
	if val, ok := tokenLineValue(body, BlockerLinePrefix); ok && val != "" {
		return val
	}
	if heading := NotDoneHeading(); heading != "" {
		if _, notDone := sectionBody(body, heading); notDone != "" {
			for _, line := range strings.Split(notDone, "\n") {
				if s := trimClaimBullet(strings.TrimSpace(line)); statedLimitLine(s) {
					return s
				}
			}
		}
	}
	return ""
}

// statedLimitLine reports whether a `## Not Done` line STATES a limit, as
// opposed to naming nothing (noLimitPhrases) or leaving the harness's own hole
// (skeletonHolePrefix). An empty line states nothing.
func statedLimitLine(s string) bool {
	if s == "" || strings.HasPrefix(s, skeletonHolePrefix) {
		return false
	}
	low := strings.ToLower(s)
	for _, p := range noLimitPhrases {
		if low == p {
			return false
		}
	}
	return true
}

// tokenLineValue returns the value of the first line opening with token and
// whether such a line exists. A token inside prose never matches: the comparison
// is anchored at the start of the line (after indentation and one list marker),
// so "I did not run: …" is not a `Ran:` line (the false-positive direction the
// epic names).
func tokenLineValue(body, token string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		s := trimClaimBullet(strings.TrimSpace(line))
		if strings.HasPrefix(s, token) {
			return strings.TrimSpace(strings.TrimPrefix(s, token)), true
		}
	}
	return "", false
}

// ranValue returns the value of the section's `Ran:` line ("" when the section
// carries none) — the token-line read the predicate is built on, named so the
// clause-2 decision lives in one place.
func ranValue(section string) string {
	val, _ := tokenLineValue(section, RanLinePrefix)
	return val
}

// RunCheckRecord renders the digest fragment for a verdict ("" when nothing is
// missing) — the recorded index a reviewer reads off the row's own trail.
func RunCheckRecord(c RunCheck) string {
	if !c.Missing() {
		return ""
	}
	return RanCheckDigestPrefix + c.Section + " " + RanCheckNote
}

// RunCheckWarning renders the operator line(s) for a verdict: the finding's
// label, the heading it was found under, and the remedy. Callers print it on
// stderr — a finding that is recorded but never shown is half a record.
func RunCheckWarning(taskID string, c RunCheck) string {
	if !c.Missing() {
		return ""
	}
	var b strings.Builder
	b.WriteString(RanCheckWarningLabel)
	if taskID != "" {
		b.WriteString(" ")
		b.WriteString(taskID)
	}
	b.WriteString(" — under ")
	b.WriteString(c.Section)
	b.WriteString(": ")
	b.WriteString(RanCheckNote)
	b.WriteString("\n")
	return b.String()
}
