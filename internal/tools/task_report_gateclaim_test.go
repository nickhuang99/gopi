package tools

// task_report_gateclaim_test.go — the SEAL's half of the gate-claim contract
// (internal/taskreport/gateclaim.go): the measured case, end to end through
// SealTaskReport, the ONE choke point every close-out passes through.
//
// The motivating case (task 01a0af50, 2026-09-17) is reproduced here as text:
// the report's claim, and — in the same run — the close-out sentence that
// contradicted it. The seal must RECORD the unobserved claim (never rewrite
// it), accept the same run's TRUE claims (the cgroup acceptance measurement,
// the red proof), and splice the evidence section into an authored report
// without touching a byte of anybody's prose.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/taskreport"
)

// gateClaimReport is a model-authored report whose Verification section
// carries the motivating run's verbatim claim plus the close-out machinery's
// contradicting sentence — the two halves of the measured defect in ONE body.
const gateClaimReport = `# Task Report: the takeover ladder

## Completed
- the fix is on the branch

## Files
- cmd/agent/merger_takeover_scope.go

## Verification
- ` + "`git status --porcelain` empty in both worktrees (Rule 30)" + `
- the close-out machinery printed: worktree is DIRTY (uncommitted work) — left for a human — entries: ` +
	`M cmd/agent/merger_takeover.go | ?? devdoc/merger-takeover-scope-escape.md

## Not Done
- nothing
`

// gateClaimEvidenceReport is the SAME report with the claim's observation
// attached — Shape A. It must be accepted, and the run's own red proof and
// cgroup acceptance measurement must stay expressible (the epic's constraint
// against suppressing genuinely useful TRUE measurements).
const gateClaimEvidenceReport = `# Task Report: the takeover ladder

## Completed
- the fix is on the branch

## Files
- cmd/agent/merger_takeover_scope.go

## Verification
- ` + "`go build ./...`" + `: OK; ` + "`go vet ./...`" + ` exit 0

## ` + "Evidence (self-filed)" + `
- red proof reproduces the operator's verbatim ` + "`permission denied`" + ` and turns green with the fix ` +
	`(patch applied → FAIL, restored → PASS)
- real-child acceptance measures ` + "`/proc/<pid>/cgroup`" + ` at ` + "`gopi-takeover-002-518e44ac26a2.scope`" + `
- worktree state: the framework report carries that measurement

## Not Done
- nothing
`

// seedReport writes body to the task's report path.
func seedReport(t *testing.T, taskID, body string) (string, string) {
	t.Helper()
	dir := reportSandbox(t, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, taskreport.FileName)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("seed report: %v", err)
	}
	return dir, path
}

// TestSealRecordsTheMotivatingRunsUnobservedClaim is the RED-proof shape at the
// seam: the claim sentence is RECORDED in the ledger's digest, quoted verbatim,
// while the report file itself is left byte-identical (no door rewrites a run's
// prose). On the pre-fix tree the digest carries no such record — that is the
// failing-then-passing difference proven by zz_redproof_gateclaim_test.go.
func TestSealRecordsTheMotivatingRunsUnobservedClaim(t *testing.T) {
	const taskID = "t-report-gateclaim-red"
	_, path := seedReport(t, taskID, gateClaimReport)

	line, err := SealTaskReport(taskID)
	if err != nil {
		t.Fatalf("SealTaskReport: %v", err)
	}
	if !strings.Contains(line, taskreport.ClaimDigestPrefix) {
		t.Fatalf("the unobserved worktree claim must be RECORDED on the row's digest, got %q", line)
	}
	if !strings.Contains(line, "`git status --porcelain` empty in both worktrees") {
		t.Errorf("the record must quote the offending sentence verbatim, got %q", line)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(after) != gateClaimReport {
		t.Errorf("the seal must never rewrite an authored report:\n--- want ---\n%s\n--- got ---\n%s", gateClaimReport, after)
	}
}

// TestSealAcceptsEvidenceBearingClaimsAndKeepsTheTrueOnes: Shape A — a claim
// with its observation is accepted — and the motivating run's TRUE measurements
// (the red proof, the cgroup acceptance reading) remain expressible. A fix that
// bought acceptance by suppressing evidence fails here.
func TestSealAcceptsEvidenceBearingClaimsAndKeepsTheTrueOnes(t *testing.T) {
	const taskID = "t-report-gateclaim-green"
	_, path := seedReport(t, taskID, gateClaimEvidenceReport)

	line, err := SealTaskReport(taskID)
	if err != nil {
		t.Fatalf("SealTaskReport: %v", err)
	}
	if strings.Contains(line, taskreport.ClaimDigestPrefix) {
		t.Errorf("claims carrying their observation / a framework delegation must not be recorded, got %q", line)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	for _, want := range []string{"reproduces the operator's verbatim", "/proc/<pid>/cgroup", "the framework report"} {
		if !strings.Contains(string(after), want) {
			t.Errorf("the run's TRUE claim %q must survive the seal:\n%s", want, after)
		}
	}
}

// TestSealSplicesTheEvidenceSectionIntoTheHarnessSkeleton: the WRITE half. The
// harness-owned skeleton (a run that authored nothing) carries the evidence
// section, placed directly after `## Verification` — the section whose claims
// it carries — with the placeholder stating the two accepted shapes and the
// cost of an unobserved claim. An AUTHORED body is never touched (proven
// above), because an artifact of record must keep its author's sentences.
func TestSealSplicesTheEvidenceSectionIntoTheHarnessSkeleton(t *testing.T) {
	const taskID = "t-report-gateclaim-skeleton"
	_, path := seedReport(t, taskID, "   \n")

	if _, err := SealTaskReport(taskID); err != nil {
		t.Fatalf("SealTaskReport: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	got := string(body)
	vi, ei := strings.Index(got, taskreport.VerificationHeading()), strings.Index(got, taskreport.SectionEvidence)
	if !(vi >= 0 && ei > vi) {
		t.Fatalf("the skeleton's evidence section must follow Verification (vi=%d ei=%d):\n%s", vi, ei, got)
	}
	// ... and it must be the NEXT section (sitting between Verification's body
	// and whatever section follows it), not appended at the end.
	rest := got[vi+len(taskreport.VerificationHeading()):]
	nextHeading := strings.Index(rest[ei-vi-len(taskreport.VerificationHeading())+len(taskreport.SectionEvidence):], "\n## ")
	if nextHeading < 0 {
		t.Fatalf("the skeleton must carry a section after the evidence one:\n%s", got)
	}
	if !strings.Contains(got, taskreport.EvidencePlaceholder) {
		t.Errorf("the section must state the two accepted shapes:\n%s", got)
	}
}

// TestTaskReportShowBlockNamesTheUnobservedClaim: the READER half of the
// contract. The finding must reach the surface the operator actually reads
// (`gopi task show`), not only the ledger's digest — a record an operator
// never meets is half a record. The block names the label AND quotes each
// offending sentence verbatim, beside the report that makes it.
func TestTaskReportShowBlockNamesTheUnobservedClaim(t *testing.T) {
	const taskID = "t-report-gateclaim-show"
	seedReport(t, taskID, gateClaimReport)

	block := TaskReportShowBlock(taskID, 0, "")
	if !strings.Contains(block, TaskShowReportClaimLabel) {
		t.Fatalf("task show must name the gate-claim finding under %q:\n%s", TaskShowReportClaimLabel, block)
	}
	if !strings.Contains(block, "`git status --porcelain` empty in both worktrees") {
		t.Errorf("the finding must quote the offending sentence verbatim:\n%s", block)
	}
	// The report body itself is still rendered — the finding rides BESIDE the
	// authored text, it never replaces it.
	if !strings.Contains(block, "## Verification") {
		t.Errorf("the report body must still render under the finding:\n%s", block)
	}
}

// TestTaskReportShowBlockIsSilentAboutObservedClaims: the reader's
// false-positive guard — a report whose claims all carry their observation
// renders no finding line at all.
func TestTaskReportShowBlockIsSilentAboutObservedClaims(t *testing.T) {
	const taskID = "t-report-gateclaim-show-quiet"
	seedReport(t, taskID, gateClaimEvidenceReport)

	block := TaskReportShowBlock(taskID, 0, "")
	if strings.Contains(block, TaskShowReportClaimLabel) {
		t.Errorf("an evidence-bearing report must render no finding:\n%s", block)
	}
}

// TestSealIsSilentAboutAReportWithNoGateClaims: a report that makes no gate
// claim has nothing to record — the false-positive guard that keeps the seal
// from becoming noise on every ordinary run.
func TestSealIsSilentAboutAReportWithNoGateClaims(t *testing.T) {
	const taskID = "t-report-gateclaim-quiet"
	quiet := "# Task Report: x\n\n## Completed\n- read the code and wrote the guard\n\n" +
		"## Files\n- a.go\n\n## Verification\n- `go build ./...`: OK\n\n## Not Done\n- (nothing)\n"
	seedReport(t, taskID, quiet)

	line, err := SealTaskReport(taskID)
	if err != nil {
		t.Fatalf("SealTaskReport: %v", err)
	}
	if strings.Contains(line, taskreport.ClaimDigestPrefix) {
		t.Errorf("a report with no gate claim must record nothing, got %q", line)
	}
}
