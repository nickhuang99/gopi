package tools

// delegated_report_test.go — the SEAL's half of the result-delegation check
// (delegated_report.go): the motivating case end to end through SealTaskReport,
// the ONE choke point every close-out passes through, and through the reader
// (`task show`).
//
// The motivating case (row 01a12d6b, 2026-10-12) is reproduced as text: the
// report's `## Verification` hands the acceptance conclusion to a path
// ("→ see the stream at `artifacts/make-testbudget-testutil.txt`"), the run's
// acceptance stream afterwards exited 2 with two red tests, and nothing at
// close-out looked at the sentence. The seal must RECORD it — with the path's
// existence measured at that very moment — never rewrite the report, never
// refuse the seal.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/taskreport"
)

// delegatedSealPath is the artifact the motivating sentence delegates to, named
// once here so the fixtures and the assertions cannot drift.
const delegatedSealPath = "artifacts/make-testbudget-testutil.txt"

// delegatedSealReport is the motivating report shape: every required section
// present, the acceptance conclusion delegated to a path, and no result stated
// for it.
const delegatedSealReport = `# Task Report: capture an identity once the child's argv is published

## Completed
- the fix is on the branch

## Files
- internal/testutil/proc_identity.go

## Verification
- **Build**: ` + "`go build ./...`" + ` → OK (exit 0).
- **Tests**: ` + "`make test-budget PKGS='./internal/testutil'`" + ` → see the stream at
  ` + "`artifacts/make-testbudget-testutil.txt`" + `.
- **Ran** (verbatim, on the fixed tree): ` + "`go test -count 1 ./internal/testutil`" + `

## Not Done
- the pre-existing red pair, proven at my own base commit
`

// sealDelegatedReport writes body as the task's report and seals it, returning
// the session dir and the seal's digest line.
func sealDelegatedReport(t *testing.T, taskID, body string) (string, string) {
	t.Helper()
	dir := reportSandbox(t, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, taskreport.FileName), []byte(body), 0o644); err != nil {
		t.Fatalf("seed report: %v", err)
	}
	line, err := SealTaskReport(taskID)
	if err != nil {
		t.Fatalf("SealTaskReport: %v", err)
	}
	return dir, line
}

// TestSealRecordsADanglingDelegation: the motivating shape, with the delegated
// path ABSENT — the strongest finding this check can make, because the report
// hands the reader to a file that does not exist and states no result either.
// The record must name the sentence, the path and the absence.
func TestSealRecordsADanglingDelegation(t *testing.T) {
	const taskID = "t-delegated-absent"
	dir, line := sealDelegatedReport(t, taskID, delegatedSealReport)

	if !strings.Contains(line, taskreport.DelegatedDigestPrefix) {
		t.Fatalf("the seal must record the delegation on the digest, got %q", line)
	}
	for _, want := range []string{"see the stream at", delegatedSealPath, taskreport.DelegatedPathStatusAbsent} {
		if !strings.Contains(line, want) {
			t.Errorf("digest record must contain %q, got %q", want, line)
		}
	}
	// Recorded, never rewritten: the authored report keeps every byte.
	got, err := os.ReadFile(filepath.Join(dir, taskreport.FileName))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != delegatedSealReport {
		t.Errorf("the seal rewrote the report; got:\n%s", got)
	}
	// The reader's face: `task show` names the finding and the path beside the
	// report (the operator's surface, not only the ledger's digest).
	block := TaskReportShowBlock(taskID, 0, line)
	for _, want := range []string{TaskShowReportDelegatedLabel, delegatedSealPath} {
		if !strings.Contains(block, want) {
			t.Errorf("task show must carry %q, got:\n%s", want, block)
		}
	}
}

// TestSealRecordsAPresentDelegationWithItsSize: the delegated artifact IS there
// at seal time — the finding is then the weaker, true one ("recoverable, the
// report states no result itself"), and the record must not read as an
// endorsement. The size is measured, not guessed.
func TestSealRecordsAPresentDelegationWithItsSize(t *testing.T) {
	const taskID = "t-delegated-present"
	const payload = "the acceptance stream, measured by the run itself\n"
	// The artifact is placed BEFORE the seal, under the session dir the seal
	// resolves itself (one authority for the directory).
	probe := reportSandbox(t, taskID)
	// reportSandbox pins GOPI_HOME; create the artifact at the path the
	// delegation names, relative to the report's own directory.
	artDir := filepath.Join(probe, "artifacts")
	if err := os.MkdirAll(artDir, 0o755); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artDir, "make-testbudget-testutil.txt"), []byte(payload), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := os.WriteFile(filepath.Join(probe, taskreport.FileName), []byte(delegatedSealReport), 0o644); err != nil {
		t.Fatalf("seed report: %v", err)
	}
	line, err := SealTaskReport(taskID)
	if err != nil {
		t.Fatalf("SealTaskReport: %v", err)
	}
	if !strings.Contains(line, taskreport.DelegatedDigestPrefix) {
		t.Fatalf("the seal must record the delegation, got %q", line)
	}
	if want := taskreport.DelegatedPathStatusPresent(int64(len(payload))); !strings.Contains(line, want) {
		t.Errorf("the record must name the measured size, want %q in %q", want, line)
	}
}

// TestSealAcceptsADelegationThatStatesItsResult: the same shape WITH a result in
// the same sentence is a finished delegation — the artifact is what a reviewer
// checks the number against, and the report is not leaving them to guess.
func TestSealAcceptsADelegationThatStatesItsResult(t *testing.T) {
	const taskID = "t-delegated-resolved"
	body := strings.Replace(delegatedSealReport,
		"`artifacts/make-testbudget-testutil.txt`.",
		"`artifacts/make-testbudget-testutil.txt` (exit=0, 0 red).", 1)
	_, line := sealDelegatedReport(t, taskID, body)
	if strings.Contains(line, taskreport.DelegatedDigestPrefix) {
		t.Errorf("a delegation that states its result must not be recorded, got %q", line)
	}
}

// TestSealAcceptsAFencedRanBlock: Rule 31's `Ran:` block is the surface being
// EXECUTED. Flagging a fenced transcript — the very evidence Rule 31 asks for —
// would punish the behaviour the rules prescribe (the corpus measurement behind
// the fence clause in delegated.go).
func TestSealAcceptsAFencedRanBlock(t *testing.T) {
	const taskID = "t-delegated-ranblock"
	body := `# Task Report: x

## Verification
- **Ran** (verbatim):
  ` + "```bash" + `
  make test-budget PKGS='./internal/tools' 2>&1 | tee artifacts/budget.txt
  ` + "```" + `
- **Result**: exit=0.

## Not Done
- nothing
`
	_, line := sealDelegatedReport(t, taskID, body)
	if strings.Contains(line, taskreport.DelegatedDigestPrefix) {
		t.Errorf("a fenced invocation must not be recorded as a delegation, got %q", line)
	}
}

// TestSealRecordsNothingForAReportThatStatesItsResults: the conservative
// direction — a report whose Verification states what it saw must come out
// without the record, or the check punishes honest work and gets switched off.
func TestSealRecordsNothingForAReportThatStatesItsResults(t *testing.T) {
	const taskID = "t-delegated-clean"
	body := `# Task Report: x

## Completed
- the fix is on the branch

## Verification
- **Build**: ` + "`go build ./...`" + ` → OK (exit 0).
- **Tests**: ` + "`make test-budget PKGS='./internal/taskreport ./internal/tools'`" + ` → ok, 0 red.
- **Ran**: ` + "`gopi task show 01a12d6b`" + ` → the report renders.
- **Result**: exit=0.

## Not Done
- nothing
`
	_, line := sealDelegatedReport(t, taskID, body)
	if strings.Contains(line, taskreport.DelegatedDigestPrefix) {
		t.Errorf("an honest report must not be recorded, got %q", line)
	}
}

// TestSealRefusesNoSealOnADelegation: the action is RECORD — the seal returns a
// line and no error, and `completed` writebacks are untouched. This pins the
// epic's constraint (never refuse) as a test rather than a comment.
func TestSealRefusesNoSealOnADelegation(t *testing.T) {
	const taskID = "t-delegated-noerror"
	dir, line := sealDelegatedReport(t, taskID, delegatedSealReport)
	if line == "" {
		t.Fatalf("the seal must return a digest line even when it records a delegation")
	}
	// The report file is still there, sealed, byte for byte — the seal did its
	// ordinary job; the delegation cost the run nothing but a recorded line.
	if _, err := os.ReadFile(filepath.Join(dir, taskreport.FileName)); err != nil {
		t.Fatalf("the sealed report must exist: %v", err)
	}
}
