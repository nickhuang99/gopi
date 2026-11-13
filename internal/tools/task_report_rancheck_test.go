package tools

// task_report_rancheck_test.go — the SEAL's half of Rule 31's checked face
// (internal/taskreport/runcheck.go): the measured case, end to end through
// SealTaskReport, the ONE choke point every close-out passes through, and
// through the reader (`task show`).
//
// The motivating case (2026-09-21): `gopi task sync --push-only --task-id T`
// documented a guarantee in its own help text, compiled, passed the suite, and
// was reported done — then failed on its FIRST real invocation. Its report
// carried the pre-Rule-31 shape (`Build:` / `Tests:` only), so no line in it
// could have asked whether anything was ever run. The seal now records that
// gap; it records it and NEVER refuses (the action choice, measured in
// devdoc/rule31-ran-check.md).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/taskreport"
)

// ranCheckSealPreRule31 is the pre-Rule-31 report shape (Build:/Tests: only,
// nothing stating a limit) — the shape that shipped the broken command.
const ranCheckSealPreRule31 = `# Task Report: the manual push face

## Completed
- implemented --task-id on ` + "`gopi task sync --push-only`" + `

## Files
- internal/tools/memsync.go

## Verification
- Build: ` + "`go build ./...`" + `: OK
- Tests:  ` + "`go test ./internal/tools`" + `: ok
- Commit: cbed046b feat(sync): the manual push face

## Not Done
- (nothing)
`

// ranCheckSealHonest is the same run's report when the surface could not be
// executed and the report SAYS SO — Rule 31's documented escape.
const ranCheckSealHonest = `# Task Report: the cross-host leg

## Completed
- wired the transport

## Files
- internal/tools/memsync.go

## Verification
- Build: ` + "`go build ./...`" + `: OK
- Tests:  ` + "`go test ./internal/tools`" + `: ok
- Blocked: needs the ` + "`hp`" + ` peer host, unreachable from this sandbox

## Not Done
- the push leg could not be run here: it needs the ` + "`hp`" + ` host
`

// ranCheckSealRan is the same report with Rule 31's line.
const ranCheckSealRan = `# Task Report: the manual push face

## Completed
- implemented --task-id on ` + "`gopi task sync --push-only`" + `

## Files
- internal/tools/memsync.go

## Verification
- Build:  ` + "`go build ./...`" + `: OK
- Tests:  ` + "`go test ./internal/tools`" + `: ok
- Ran:    ` + "`./bin/gopi task sync --push-only --task-id 01a0c2b1-...`" + `
- Result: exit status 1 — stderr: claim-remote: want 1..8 row(s), got 2181

## Not Done
- (nothing)
`

// TestSealRecordsTheMissingRanLine is the RED-proof shape at the seam, and the
// ACTION assertion: the gap is RECORDED on the row's digest and printed on
// stderr, while the seal still succeeds and the report file is left
// byte-identical (no door rewrites a run's prose, and nothing is refused).
func TestSealRecordsTheMissingRanLine(t *testing.T) {
	const taskID = "t-report-rancheck-red"
	_, path := seedReport(t, taskID, ranCheckSealPreRule31)

	line, err := SealTaskReport(taskID)
	if err != nil {
		t.Fatalf("SealTaskReport must RECORD, never refuse: %v", err)
	}
	if !strings.Contains(line, taskreport.RanCheckDigestPrefix) {
		t.Fatalf("the missing `Ran:` line must be RECORDED on the row's digest, got %q", line)
	}
	if !strings.Contains(line, taskreport.VerificationHeading()) {
		t.Errorf("the record must name the heading it adjudicated, got %q", line)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(after) != ranCheckSealPreRule31 {
		t.Errorf("the seal must never rewrite an authored report:\n--- want ---\n%s\n--- got ---\n%s",
			ranCheckSealPreRule31, after)
	}
}

// TestSealIsSilentWhenTheLineOrAnHonestBlockerIsStated: the two GREEN
// directions at the seam — a real `Ran:` line, and a stated reason the surface
// could not be executed. The second is the honesty case: a check that flags it
// punishes exactly the behaviour Rule 31 documents as acceptable.
func TestSealIsSilentWhenTheLineOrAnHonestBlockerIsStated(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"real Ran: line", ranCheckSealRan},
		{"stated blocker", ranCheckSealHonest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taskID := "t-report-rancheck-" + strings.ReplaceAll(tc.name, " ", "-")
			_, path := seedReport(t, taskID, tc.body)

			line, err := SealTaskReport(taskID)
			if err != nil {
				t.Fatalf("SealTaskReport: %v", err)
			}
			if strings.Contains(line, taskreport.RanCheckDigestPrefix) {
				t.Errorf("%s must not be recorded as a gap, got %q", tc.name, line)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if string(after) != tc.body {
				t.Errorf("the seal must not rewrite the report:\n%s", after)
			}
		})
	}
}

// TestTaskReportShowBlockNamesTheMissingRanLine: the READER half. The finding
// must reach the surface the operator actually reads (`gopi task show`) — a
// record an operator never meets is half a record — and it must ride BESIDE the
// authored body, never replace it.
func TestTaskReportShowBlockNamesTheMissingRanLine(t *testing.T) {
	const taskID = "t-report-rancheck-show"
	seedReport(t, taskID, ranCheckSealPreRule31)

	block := TaskReportShowBlock(taskID, 0, "")
	if !strings.Contains(block, TaskShowReportRanLabel) {
		t.Fatalf("task show must name the Rule 31 finding under %q:\n%s", TaskShowReportRanLabel, block)
	}
	if !strings.Contains(block, taskreport.RanLinePrefix) {
		t.Errorf("the finding must name the line Rule 31 wants:\n%s", block)
	}
	if !strings.Contains(block, "## Verification") {
		t.Errorf("the report body must still render under the finding:\n%s", block)
	}
}

// TestTaskReportShowBlockIsSilentAboutRanEvidence: the reader's
// false-positive guard (including the honesty case).
func TestTaskReportShowBlockIsSilentAboutRanEvidence(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"ran", ranCheckSealRan},
		{"honest blocker", ranCheckSealHonest},
	} {
		taskID := "t-report-rancheck-quiet-" + strings.ReplaceAll(tc.name, " ", "-")
		seedReport(t, taskID, tc.body)
		if block := TaskReportShowBlock(taskID, 0, ""); strings.Contains(block, TaskShowReportRanLabel) {
			t.Errorf("%s: no finding must render:\n%s", tc.name, block)
		}
	}
}

// TestSealSkeletonDeclaresNoInvocationInsteadOfAnEmptyRanLine: the WRITER half
// of "an empty `Ran:` is never emitted as a measurement". The harness skeleton
// runs nothing, so its verification section NAMES the tokens with an explicit
// no-invocation marker — and the ran-check reads that skeleton as a gap (it
// states no invocation), not as a satisfied contract.
func TestSealSkeletonDeclaresNoInvocationInsteadOfAnEmptyRanLine(t *testing.T) {
	const taskID = "t-report-rancheck-skeleton"
	_, path := seedReport(t, taskID, "  \n")

	line, err := SealTaskReport(taskID)
	if err != nil {
		t.Fatalf("SealTaskReport: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, taskreport.RanLinePrefix) {
		t.Fatalf("the skeleton's verification section must NAME the Rule 31 tokens:\n%s", got)
	}
	for _, line := range strings.Split(got, "\n") {
		s := strings.TrimSpace(line)
		if strings.HasSuffix(s, taskreport.RanLinePrefix) || strings.HasSuffix(s, taskreport.RanLinePrefix+" ") {
			t.Errorf("the skeleton must never emit an EMPTY `%s` line (it would read as a measurement): %q",
				taskreport.RanLinePrefix, s)
		}
	}
	if verdict := taskreport.RunCheckVerdict(got); !verdict.Missing() {
		t.Errorf("the skeleton states no invocation and no reason, so the gap must be recorded: %+v", verdict)
	}
	if !strings.Contains(line, taskreport.RanCheckDigestPrefix) {
		t.Errorf("the skeleton's own gap must reach the digest: %q", line)
	}
}

// TestSealRunsTheRanCheckOverItsOwnWrittenReport is the self-check the epic
// implies: the check is exercised on the exact bytes the seal leaves on disk
// (not on a fixture the test built separately), for both a satisfied and an
// unsatisfied report.
func TestSealRunsTheRanCheckOverItsOwnWrittenReport(t *testing.T) {
	dir := reportSandbox(t, "t-report-rancheck-selfcheck")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, taskreport.FileName)
	if err := os.WriteFile(path, []byte(ranCheckSealPreRule31), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := SealTaskReport("t-report-rancheck-selfcheck"); err != nil {
		t.Fatalf("SealTaskReport: %v", err)
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if v := taskreport.RunCheckVerdict(string(onDisk)); !v.Missing() {
		t.Errorf("the sealed bytes must carry the recorded gap: %+v", v)
	}
}
