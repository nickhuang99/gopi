package taskreport

// gateclaim_test.go — the GATE-CLAIM contract's pure face (gateclaim.go).
//
// The measured case is quoted verbatim, both halves: the claim the run report
// made, and the sentence the same run's close-out machinery printed about the
// same worktree. The red-proof shape lives HERE — the offending sentence must
// be reported, the two TRUE claims of the same run (the cgroup acceptance
// measurement and the red proof) must NOT be.

import (
	"strings"
	"testing"
)

// gateClaimMeasuredSentence is the offending sentence as the report worded it:
// a framework-measured ground (the worktree's porcelain state) asserted with a
// result ("empty") and no observation anywhere in the sentence.
const gateClaimMeasuredSentence = "`git status --porcelain` empty in both worktrees (Rule 30)"

// gateClaimContradiction is the same run's close-out sentence about the very
// same worktree — the measurement that makes the claim above unmeasured.
const gateClaimContradiction = "worktree is DIRTY (uncommitted work) — left for a human — entries: " +
	"M cmd/agent/merger_takeover.go | ?? devdoc/merger-takeover-scope-escape.md"

// gateClaimTrueCgroupSentence is one of the run's TRUE claims (the cgroup
// acceptance measurement): it must remain expressible — the epic exists to
// stop unmeasured claims, not to silence measured ones.
const gateClaimTrueCgroupSentence = "real-child acceptance measures `/proc/<pid>/cgroup` at " +
	"`gopi-takeover-002-518e44ac26a2.scope` (not `session-18205.scope`)"

// gateClaimTrueRedProofSentence is the other TRUE claim: the red-then-green
// proof, with its measured transition.
const gateClaimTrueRedProofSentence = "red proof reproduces the operator's verbatim `permission denied` " +
	"and turns green with the fix"

// evidenceBody wraps lines into a report body carrying the evidence section.
func evidenceBody(verificationLines, evidenceLines []string) string {
	var b strings.Builder
	b.WriteString("# Task Report: x\n\n## Completed\n- did things\n\n## Files\n- a.go\n\n")
	b.WriteString(VerificationHeading() + "\n")
	for _, l := range verificationLines {
		b.WriteString("- " + l + "\n")
	}
	b.WriteString("\n" + SectionEvidence + "\n")
	for _, l := range evidenceLines {
		b.WriteString("- " + l + "\n")
	}
	b.WriteString("\n## Not Done\n- (nothing)\n")
	return b.String()
}

// TestGateClaimRejectsTheMeasuredCase is the contract's red proof: the sentence
// the motivating report actually wrote is reported, quoted verbatim, under the
// evidence heading.
func TestGateClaimRejectsTheMeasuredCase(t *testing.T) {
	body := evidenceBody([]string{"build ok, tests PASS"}, []string{gateClaimMeasuredSentence})
	v := GateClaimVerdict(body)
	if v.Section != SectionEvidence {
		t.Fatalf("evidence section not adjudicated: %+v", v)
	}
	if len(v.MissingSentences) != 1 || v.MissingSentences[0] != gateClaimMeasuredSentence {
		t.Fatalf("the unmeasured worktree claim must be reported verbatim, got %q", v.MissingSentences)
	}
	if !strings.Contains(DigestRecord(v), gateClaimMeasuredSentence) {
		t.Errorf("the finding must reach the digest record: %q", DigestRecord(v))
	}
}

// TestGateClaimFindsTheMotivatingShapeWithoutAnEvidenceSection: the measured
// report had NO evidence section — its gate claims sat under `## Verification`.
// The adjudication must reach them there (otherwise the contract would be
// satisfied by simply not writing the section, which is the loophole the
// motivating case used).
func TestGateClaimFindsTheMotivatingShapeWithoutAnEvidenceSection(t *testing.T) {
	body := "# Task Report: x\n\n## Completed\n- a\n\n## Files\n- b\n\n" +
		VerificationHeading() + "\n- " + gateClaimMeasuredSentence + "\n- " + gateClaimContradiction + "\n\n## Not Done\n- c\n"
	v := GateClaimVerdict(body)
	if v.Section != VerificationHeading() {
		t.Fatalf("the Verification section must be adjudicated when no evidence section exists: %+v", v)
	}
	if len(v.MissingSentences) != 1 || v.MissingSentences[0] != gateClaimMeasuredSentence {
		t.Fatalf("want the unobserved claim alone, got %q", v.MissingSentences)
	}
}

// TestGateClaimAcceptsTheRunsTrueClaims: the cgroup acceptance measurement and
// the red proof stay expressible — they carry their observation, so the
// contract is silent about them. Solving the epic by suppressing evidence would
// fail here.
func TestGateClaimAcceptsTheRunsTrueClaims(t *testing.T) {
	for _, sentence := range []string{gateClaimTrueCgroupSentence, gateClaimTrueRedProofSentence} {
		body := evidenceBody(nil, []string{sentence})
		if v := GateClaimVerdict(body); len(v.MissingSentences) != 0 {
			t.Errorf("a claim with its observation must pass: %q → missing %q", sentence, v.MissingSentences)
		}
	}
	// The same two claims in ONE sentence, as the motivating gate-status line
	// worded them, must also pass.
	joined := gateClaimTrueRedProofSentence + "; " + gateClaimTrueCgroupSentence
	if v := GateClaimVerdict(evidenceBody(nil, []string{joined})); len(v.MissingSentences) != 0 {
		t.Errorf("the run's own gate-status wording must pass: %q", v.MissingSentences)
	}
}

// TestGateClaimAcceptsObservations pins the OBSERVATION vocabulary itself: an
// exit code, a pasted output, a measured reading, a delegated parenthesis.
func TestGateClaimAcceptsObservations(t *testing.T) {
	accepted := []string{
		"`go test ./internal/tools -run TestSeal -count=1` → ok github.com/nick/gopi/internal/tools 2.1s",
		"output: exit=0",
		"`go vet ./...`: exit code 0",
		"`gofmt -l internal/tools` output: (empty)",
		"`gopi task show t-1` printed task_report: /dev/shm/x/task-report.md",
		"targeted tests measured green",
	}
	for _, s := range accepted {
		v := GateClaimVerdict(evidenceBody(nil, []string{s}))
		if len(v.MissingSentences) != 0 {
			t.Errorf("observation not honoured: %q → missing %q", s, v.MissingSentences)
		}
	}
}

// TestGateClaimAcceptsAWrappedBulletObservation is the regression pin for the
// false positive this epic's OWN report produced: a bullet whose arrow lands on
// the marker line and whose evidence is the next indented line ("- post-fix run
// (t+2019s, this branch): the same -run selection →\n  `ok github.com/… 3.340s`.").
// Splitting per physical line judged the first line alone — the instrument and
// its ok were on the line below — and flagged a claim that was measured. The
// unit is the BULLET (marker + its wrapped lines).
func TestGateClaimAcceptsAWrappedBulletObservation(t *testing.T) {
	wrapped := "- post-fix run (t+2019s, this branch): the same `-run` selection →\n" +
		"  `ok github.com/nick/gopi/internal/tools 3.340s`.\n" +
		"- bare assertion with nothing behind it: `git status --porcelain` empty\n"
	v := GateClaimVerdict(evidenceBody(nil, []string{wrapped}))
	for _, s := range v.MissingSentences {
		if strings.Contains(s, "3.340s") {
			t.Errorf("a wrapped bullet's own evidence must satisfy its claim, got flagged: %q", s)
		}
	}
	// The neighbouring bare claim on the same section must STILL be flagged —
	// the fix must not blind the predicate.
	if len(v.MissingSentences) != 1 {
		t.Fatalf("want exactly the unobserved claim flagged, got %q", v.MissingSentences)
	}
	if !strings.Contains(v.MissingSentences[0], "empty") {
		t.Errorf("the unobserved claim should be the flagged one, got %q", v.MissingSentences[0])
	}
}

func TestGateClaimAcceptsTheProjectTemplatesCompressedObservation(t *testing.T) {
	for _, s := range []string{
		"Build: `go build ./...`: OK",
		"Tests: `go test ./internal/tools`: 12/12 PASS",
		"Commit: cbed046b test(merger): the take-over child is measured OUTSIDE the session scope",
	} {
		if v := GateClaimVerdict(evidenceBody(nil, []string{s})); len(v.MissingSentences) != 0 {
			t.Errorf("the project's own template line must pass: %q → %q", s, v.MissingSentences)
		}
	}
}

// TestGateClaimAcceptsAFrameworkDelegation: the Shape-B half — a run that must
// mention a framework-measured fact names its source, which IS an observation
// (it points at the measurement that exists).
func TestGateClaimAcceptsAFrameworkDelegation(t *testing.T) {
	for _, s := range []string{
		"worktree state: the framework report carries the measurement (not restated here)",
		"my worktree is clean — see the framework report for the measurement",
	} {
		v := GateClaimVerdict(evidenceBody(nil, []string{s}))
		if len(v.MissingSentences) != 0 {
			t.Errorf("a delegation to the framework's own measurement must pass: %q → %q", s, v.MissingSentences)
		}
	}
}

// TestGateClaimRejectsTheGreyVariants: the claim in fewer words, the status
// command under another spelling, and the project's own closing sentence —
// all framework grounds, all unobserved.
func TestGateClaimRejectsTheGreyVariants(t *testing.T) {
	rejected := []string{
		"my worktree is clean (Rule 30)",
		"`git status` empty in both worktrees",
		"worktree clean, branch pushed",
		"`git status --porcelain` prints nothing in my worktree",
	}
	for _, s := range rejected {
		v := GateClaimVerdict(evidenceBody(nil, []string{s}))
		if len(v.MissingSentences) == 0 {
			t.Errorf("unobserved framework claim must be reported: %q", s)
		}
	}
}

// TestGateClaimIgnoresNarrativeAndNotDone: a narrative sentence naming a
// ground (the run READ git status and then wrote a guard), and an honest
// admission of something NOT done, are not gate claims.
func TestGateClaimIgnoresNarrativeAndNotDone(t *testing.T) {
	body := "# Task Report: x\n\n## Completed\n- read `git status` output and then wrote the guard\n\n" +
		"## Files\n- a.go\n\n" + VerificationHeading() + "\n- `go build ./...`: OK\n\n" +
		SectionEvidence + "\n- `go vet ./...`: exit code 0\n\n" +
		"## Not Done\n- `git status --porcelain` was NOT run in the sibling tree (no worktree available)\n"
	if v := GateClaimVerdict(body); len(v.MissingSentences) != 0 {
		t.Errorf("narrative / honest-not-done text must not be flagged: %q", v.MissingSentences)
	}
}

// TestGateClaimWithoutAnySectionAdjudicatesTheWholeBody: a report with neither
// an evidence nor a Verification section still cannot assert an unobserved
// framework measurement — the fallback keeps the loophole closed.
func TestGateClaimWithoutAnySectionAdjudicatesTheWholeBody(t *testing.T) {
	body := "# Task Report: x\n\n## Completed\n- " + gateClaimMeasuredSentence + "\n\n## Files\n- a.go\n"
	v := GateClaimVerdict(body)
	if v.Section != "" {
		t.Fatalf("with no section the whole body is adjudicated: %+v", v)
	}
	if len(v.MissingSentences) != 1 {
		t.Fatalf("want the unobserved claim reported from the body, got %q", v.MissingSentences)
	}
}

// TestGateClaimIsSilentOnACleanReport: a report that makes no unobserved gate
// claim has nothing to record — the false-positive guard that keeps the seal
// from becoming noise on every ordinary run.
func TestGateClaimIsSilentOnACleanReport(t *testing.T) {
	body := "# Task Report: x\n\n## Completed\n- a\n\n## Files\n- b\n\n## Verification\n- `go build ./...`: OK\n\n## Not Done\n- c\n"
	if v := GateClaimVerdict(body); len(v.MissingSentences) != 0 {
		t.Errorf("an ordinary report must pass silently: %q", v.MissingSentences)
	}
	if DigestRecord(GateClaimVerdict(body)) != "" || ClaimWarning("t-x", GateClaimVerdict(body)) != "" {
		t.Errorf("a clean verdict must render nothing")
	}
}

// TestDigestRecordCapsAndClaimWarningNames: the record is bounded (a systemic
// report must not push the row's digest past what an operator reads) and the
// warning names the offending sentences and where they were found.
func TestDigestRecordCapsAndClaimWarningNames(t *testing.T) {
	lines := []string{}
	for i := 0; i < MaxRecordedClaimSentences+2; i++ {
		lines = append(lines, "worktree is clean (gag "+string(rune('a'+i))+")")
	}
	v := GateClaimVerdict(evidenceBody(nil, lines))
	rec := DigestRecord(v)
	if !strings.HasPrefix(rec, ClaimDigestPrefix) {
		t.Fatalf("record must carry the prefix: %q", rec)
	}
	if got := strings.Count(rec, claimRecordSeparator) + 1; got != MaxRecordedClaimSentences {
		t.Errorf("recorded sentences = %d, want the cap %d: %q", got, MaxRecordedClaimSentences, rec)
	}
	warn := ClaimWarning("t-cap", v)
	for _, want := range []string{ClaimWarningLabel, "t-cap", SectionEvidence, "worktree is clean (gag a)"} {
		if !strings.Contains(warn, want) {
			t.Errorf("warning must carry %q:\n%s", want, warn)
		}
	}
}

// TestSkeletonCarriesTheEvidenceSection: a run that authored nothing still gets
// the section, with the two accepted shapes stated where the model that reads
// the skeleton sees them — and the skeleton still satisfies the required
// section contract (the evidence section is NOT a required narrative section,
// so historical reports are never flagged for lacking it).
func TestSkeletonCarriesTheEvidenceSection(t *testing.T) {
	body := Skeleton("t-x", []string{FormatFact("task_id", "t-x")})
	if !hasSection(body, SectionEvidence) || !strings.Contains(body, EvidencePlaceholder) {
		t.Fatalf("skeleton must carry the evidence section + its placeholder:\n%s", body)
	}
	if missing := MissingSections(body); len(missing) > 0 {
		t.Errorf("skeleton missing required sections %v", missing)
	}
	if hasSection("## Completed\n- a\n", SectionEvidence) {
		t.Errorf("RequiredSections must not contain the evidence section")
	}
}
