// Package taskreport is the single source of truth for the BOUNDED TASK
// REPORT seam (Rule 14: a value shared across packages lives in ONE small
// exported-constant leaf package — never mirrored, never typed inline).
//
// The problem it solves: a finished task's story lived only in a 10k-line
// session JSONL. An operator asking "what did this task do?" had to feed a
// jsonl to a model. The seam below makes the story a BOUNDED, deterministic
// artifact in the task's OWN session directory, handed to the executor by the
// harness (a path in the environment — NEVER model-guessed) and rendered by
// `gopi task show`.
//
// The values here are the WIRE contract between four faces, and each one is
// the SAME string at every face — that is the entire point of the package:
//
//	FileName / PathEnv   the path the harness exports and the model writes to
//	RequiredSections     the section contract both the writer and the reader check
//	MaxBytes             the hard length bound (the "bounded" in bounded report)
//	DigestLinePrefix     how the ledger's result digest names the report path
//	LegacyNote/NoneNote  the HONEST degradation lines (no fake backfill)
//
// The GATE-CLAIM contract (gateclaim.go) lives here too, and for the same
// Rule-14 reason: "may this sentence assert that gate result?" is asked by the
// writer (the seal's skeleton + the ledger record) and answered over the text
// the reader (`task show`) prints, so the predicate and the section it reads
// must be ONE definition both faces import.
//
// The package is a LEAF: stdlib only, no project imports, constants + pure
// text functions — so every consumer (internal/tools, internal/agent,
// cmd/agent) can import it without a cycle (the tasktags/taskverdict
// precedent).
package taskreport

import (
	"strconv"
	"strings"
)

const (
	// FileName is the report's file name inside the task's session directory
	// (<gopi-dir>/sessions/<task_id>/task-report.md). Fixed, never
	// timestamped: "given the task id, where is its report?" must have ONE
	// answer (the WorkerLogFileName precedent — the operator walks from a
	// task id to every artifact it produced without a directory scan).
	FileName = "task-report.md"

	// PathEnv is the environment variable the harness sets on a `task run`
	// child BEFORE spawn, carrying the report's ABSOLUTE path. Rule 2: one
	// writer (the wrapper's pre-spawn cmd.Env), and the readers are the
	// executor (whose prompt names it) and the harness seal step.
	//
	// Why the harness supplies the path rather than the model deriving it:
	// the session directory is a harness-side fact (it can be relocated by
	// GOPI_HOME, migrated from the legacy task-sessions root, or pruned) —
	// a model that guesses the path writes its report where nobody reads.
	PathEnv = "GOPI_TASK_REPORT_PATH"

	// MaxBytes is the report length cap: the seam's whole reason to exist is
	// "readable without reading 10k lines", so an unbounded report defeats
	// it. The writer bounds model-authored content that exceeds this; the
	// harness skeleton is far under it by construction.
	MaxBytes = 32 * 1024

	// ShowBodyMaxBytes caps how much of the report body `task show` prints.
	// Smaller than MaxBytes on purpose: show is a terminal digest, and the
	// full file's path is rendered right above the body for the operator who
	// wants every byte.
	ShowBodyMaxBytes = 8 * 1024

	// DigestLinePrefix is how the run's result digest names the report path
	// (the WorkerOutputLogSummaryLine precedent): it rides the append-only
	// task_events detail, so the path survives even after the session
	// directory is pruned — the ledger, not the filesystem, is the index.
	DigestLinePrefix = "task-report: "

	// truncationMarkerPrefix introduces the truncation note; the cap itself is
	// interpolated from MaxBytes so the printed number can never drift from
	// the enforced one (Rule 1: the value lives in one place).
	truncationMarkerPrefix = "\n\n> [truncated by gopi: report exceeded "

	// SkeletonPlaceholder marks the honest hole a harness-written skeleton
	// leaves: the model authored no report, so the harness states the facts
	// it OWNS and says plainly which parts are unknown (never a fabricated
	// narrative — an invented "what was done" is worse than an empty one).
	SkeletonPlaceholder = "* (not recorded — the model wrote no report; the harness recorded the facts below)"

	// LegacyCutoffDate is the day this seam shipped. A row created BEFORE it
	// never had the chance to carry a report, so show says so explicitly
	// rather than implying the task failed to report (no fake backfill).
	LegacyCutoffDate = "2026-09-12"

	// LegacyNote is the honest degradation line for a pre-seam row.
	LegacyNote = "no report recorded (pre-" + LegacyCutoffDate + " task)"

	// NoneNote is the honest degradation line for a post-seam row with no
	// report — a real gap worth investigating, never a legacy artifact.
	NoneNote = "no report recorded"

	// purposeVerification / purposeNotDone are RequiredSections' Verification
	// and Not Done entries identified by PURPOSE rather than by position: any
	// face that must look at one of those sections (VerificationHeading,
	// NotDoneHeading, gateclaim.go's verdict, runcheck.go's honesty clause)
	// reads its heading through that one accessor (Rules 1b/13).
	purposeVerification = "build / tests / commit hash — the evidence"
	purposeNotDone      = "what was deferred or skipped"
)

// Section is one required report section: the heading the writer must emit
// and the operator question it answers.
type Section struct {
	// Heading is the markdown heading, matched as a whole line prefix.
	Heading string
	// Purpose is the operator question this section answers (documentation —
	// the same list is the writer's contract and the reader's check).
	Purpose string
}

// RequiredSections is the report contract: the four operator questions
// (what / which files+branch+commit / how verified / what's left) as headings.
// Order is the render order — the skeleton emits them in exactly this order,
// and Rule 5's minimum-content template is the project-side mirror of it.
//
// The GATE-CLAIM section (SectionEvidence) is NOT in this list: it is not a
// narrative requirement but the slot a gate claim carries its observation in,
// so it is emitted by the skeleton (SkeletonSections) — an AUTHORED report is
// never rewritten (its sentences are the artifact of record) — without turning
// every historical report that predates the contract into a "missing section"
// warning.
var RequiredSections = []Section{
	{Heading: "## Completed", Purpose: "what was implemented"},
	{Heading: "## Not Done", Purpose: purposeNotDone},
	{Heading: "## Verification", Purpose: purposeVerification},
	{Heading: "## Files", Purpose: "files touched, branch, commit, artifact paths"},
}

// SkeletonSections is RequiredSections plus the gate-claim section, in the
// order a harness-written report emits them — the evidence section sitting
// directly after the verification section whose claims it carries.
//
// A var (not a const) and computed once: the insertion point is read from
// VerificationHeading(), so moving that section in RequiredSections moves the
// evidence section with it (Rules 1b/13 — one definition, never a re-spelled
// heading).
var SkeletonSections = func() []Section {
	out := make([]Section, 0, len(RequiredSections)+1)
	for _, s := range RequiredSections {
		out = append(out, s)
		if s.Heading == VerificationHeading() {
			out = append(out, Section{Heading: SectionEvidence,
				Purpose: "each gate claim with the observation behind it (or the framework-owned measurement that carries it)"})
		}
	}
	return out
}()

// VerificationHeading is RequiredSections' Verification heading, read from the
// one definition rather than re-spelled (Rules 1b/13). The evidence section is
// spliced after it, and any face that must look at that section (the seal, a
// caller's prompt text) reads the heading here.
func VerificationHeading() string {
	for _, s := range RequiredSections {
		if s.Purpose == purposeVerification {
			return s.Heading
		}
	}
	return ""
}

// NotDoneHeading is RequiredSections' Not Done heading, read from the one
// definition (the VerificationHeading precedent, Rules 1b/13). It is the
// section Rule 31's honesty clause reads: a run that could not execute the
// surface says so HERE (or on the declared BlockerLinePrefix token).
func NotDoneHeading() string {
	for _, s := range RequiredSections {
		if s.Purpose == purposeNotDone {
			return s.Heading
		}
	}
	return ""
}

// MissingSections returns the required headings absent from body (empty =
// the contract is satisfied). Matching is line-prefix based on the RAW line, so
// "## Completed" at column 0 satisfies the section while a mention of the words
// mid-line does not (a report that merely SAYS "completed" is not a report), and
// an INDENTED continuation that opens with the heading's text does not either —
// the failing direction measured on the real corpus: an indented `## Not Done`
// inside a bullet satisfied the section, so a report whose Not Done section was
// absent was reported complete.
func MissingSections(body string) []string {
	var missing []string
	for _, s := range RequiredSections {
		if !hasSection(body, s.Heading) {
			missing = append(missing, s.Heading)
		}
	}
	return missing
}

// hasSection reports whether body carries heading as a heading — the same
// structural test sectionBody starts a section with (isSectionHeadingLine, one
// definition shared by the reader and the check), so the two can never disagree
// about which line is a section's own.
func hasSection(body, heading string) bool {
	for _, line := range strings.Split(body, "\n") {
		if isSectionHeadingLine(line, heading) {
			return true
		}
	}
	return false
}

// Bound caps body at MaxBytes, cutting on a line boundary when one is
// available and appending TruncationMarker. Reports whether it truncated.
// The bound is on BYTES because the cap exists to protect a reader's
// context budget, and bytes are what a reader pays.
func Bound(body string) (string, bool) {
	if len(body) <= MaxBytes {
		return body, false
	}
	cut := body[:MaxBytes]
	if i := strings.LastIndex(cut, "\n"); i > 0 {
		cut = cut[:i]
	}
	return cut + TruncationMarker(), true
}

// Skeleton composes the harness-written fallback report: the required
// Skeleton composes the harness-written fallback report: the skeleton section
// list (SkeletonSections — the required headings plus the gate-claim section,
// in contract order) in that order, each carrying its placeholder (the plain
// "not recorded" marker, or the gate-claim contract's own wording under the
// evidence heading), followed by the facts the harness owns (task id, status,
// session, branch, commit, …) as bullet lines. facts entries are pre-rendered
// "key: value" strings — the caller owns fact collection, this package owns
// the shape.
func Skeleton(title string, facts []string) string {
	var b strings.Builder
	b.WriteString("# Task Report: ")
	b.WriteString(title)
	b.WriteString("\n\n")
	for _, s := range SkeletonSections {
		b.WriteString(s.Heading)
		b.WriteString("\n")
		b.WriteString(sectionPlaceholder(s))
		b.WriteString("\n\n")
	}
	b.WriteString("## Harness facts\n")
	for _, f := range facts {
		b.WriteString("- ")
		b.WriteString(f)
		b.WriteString("\n")
	}
	return b.String()
}

// IsSkeleton answers "did the MODEL author this report, or is this the harness's
// own fallback?" — the ONE reader of that question (Rule 14), because the answer
// is what separates a run that explained producing nothing from a run that said
// nothing at all.
//
// THE MEASURED CASE (row 01a1b48a-8746, 2026-11-07): a dispatched bug row
// contributed no commit, and its 1138-byte `task-report.md` is byte-for-byte the
// skeleton below (four placeholder sections + `## Harness facts`), while its
// predecessor counter-proof rows carry authored prose. Both close `completed`
// with the SAME channel sentence, so without this question the ledger cannot
// tell "I checked and there was nothing to change" from "nothing happened".
//
// THE CRITERION IS THE PLACEHOLDER, not the size and not the section count: a
// report is the harness's fallback only if it carries SkeletonPlaceholder (the
// marker the skeleton emits under every narrative section). Its own evidence
// section's wording and the harness-facts block are NOT the criterion — a model
// may reasonably quote the evidence contract, and facts are facts.
//
// LENIENT DIRECTION, stated as a case: anything that is not recognisably the
// skeleton is AUTHORED. Accusing a real report of being empty is the failure this
// direction rules out (the same choice runcheck.go's statedLimitLine made).
func IsSkeleton(body string) bool {
	return strings.Contains(body, SkeletonPlaceholder)
}

// sectionPlaceholder is the skeleton body of one section: the plain "not
// recorded" marker everywhere, the contract's own wording under the evidence
// heading — the section exists to teach the two accepted shapes, so its
// skeleton body is that teaching, not a hole — and the Rule 31 line tokens
// under the verification heading (the ONE place the choice is made, so a
// section added to RequiredSections can never silently get the wrong body).
//
// WHY THE VERIFICATION PLACEHOLDER IS NOT AN EMPTY `Ran:` LINE: the harness
// skeleton runs nothing, so it has no invocation to state. Emitting
// `Ran:` with no value would hand every reader a line that LOOKS like the
// measurement Rule 31 asks for while measuring nothing — and the ran-check
// would have to treat it as absent anyway. The section therefore NAMES the
// tokens (so a model that reads the skeleton knows what the contract wants)
// without asserting one.
func sectionPlaceholder(s Section) string {
	switch s.Heading {
	case SectionEvidence:
		return EvidencePlaceholder
	case VerificationHeading():
		return verificationPlaceholder()
	default:
		return SkeletonPlaceholder
	}
}

// verificationPlaceholder renders the verification section's skeleton body: the
// standard hole marker plus the line tokens Rule 31 requires, read from their
// one definition (Rule 14).
func verificationPlaceholder() string {
	return SkeletonPlaceholder + "\n" +
		tokenLine(RanLinePrefix, skeletonNoInvocationValue) + "\n" +
		tokenLine(ResultLinePrefix, skeletonNoObservationValue)
}

// (see runcheck.go), which this file's skeleton emits behind its explicit
// no-invocation marker.
//
// The marker is the point: the harness runs nothing, so it must NEVER emit a
// `Ran:` line that LOOKS like a measurement (an empty one is not a measurement,
// and runcheck.go reads it as absent anyway) — it names the tokens the contract
// wants and says plainly that it has no value for them.
func tokenLine(token, value string) string {
	return token + " " + value
}

// FormatFact renders one "key: value" bullet line for Skeleton's facts list.
func FormatFact(key, value string) string {
	if strings.TrimSpace(value) == "" {
		value = "(unknown)"
	}
	return key + ": " + value
}

// DigestLine is the ledger line naming the report path (and, when known, its
// size + any missing required sections) — the recorded index that outlives the
// session directory.
func DigestLine(path string, size int, missingSections []string) string {
	var b strings.Builder
	b.WriteString(DigestLinePrefix)
	b.WriteString(path)
	b.WriteString(" (")
	b.WriteString(strconv.Itoa(size))
	b.WriteString(" bytes")
	if len(missingSections) > 0 {
		b.WriteString(", missing sections: ")
		b.WriteString(strings.Join(missingSections, ", "))
	}
	b.WriteString(")")
	return b.String()
}

// PathFromDigest extracts the report path from a ledger result digest, or ""
// when the digest names no report (a pre-seam row / a run that recorded none).
// The digest is free text around the line, so the value ends at the first
// space or "(" after the prefix.
func PathFromDigest(digest string) string {
	i := strings.Index(digest, DigestLinePrefix)
	if i < 0 {
		return ""
	}
	rest := digest[i+len(DigestLinePrefix):]
	if j := strings.IndexAny(rest, " \t\n("); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}

// DegradationNote returns the honest line for a task with no report on disk:
// the legacy note for a row created before the seam shipped, the plain gap
// note otherwise. createdAtSec is the row's created_at (unix seconds); 0
// (unknown) is treated as post-seam — an unknown timestamp must not be
// dressed up as history.
func DegradationNote(createdAtSec float64) string {
	if createdAtSec > 0 && createdAtSec < LegacyCutoffUnix {
		return LegacyNote
	}
	return NoneNote
}

// LegacyCutoffUnix is LegacyCutoffDate as unix seconds (2026-09-12 00:00 UTC).
// Declared as a number rather than parsed at each call: formatting a date at
// every show is work with no reader (Rule 1 — one place, one value).
const LegacyCutoffUnix = 1789171200

// TruncationMarker is appended when content is bounded, naming the cause
// explicitly: a silently shortened report reads as the author's own ending
// (Rule 15 — no silent failure).
func TruncationMarker() string {
	return truncationMarkerPrefix + strconv.Itoa(MaxBytes) + " bytes; full text capped at write time]"
}

// TruncateBody caps a body for terminal display at ShowBodyMaxBytes, appending
// the marker so a reader knows the file continues.
func TruncateBody(body string) (string, bool) {
	if len(body) <= ShowBodyMaxBytes {
		return body, false
	}
	cut := body[:ShowBodyMaxBytes]
	if i := strings.LastIndex(cut, "\n"); i > 0 {
		cut = cut[:i]
	}
	return cut + "\n" + TruncationMarker(), true
}
