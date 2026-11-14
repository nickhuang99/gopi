// filtered_report_test.go — the SEAL's and `task show`'s half of the
// filtered-package claim record: the same reading the leaf adjudicates, carried
// onto the two surfaces a reviewer actually reaches (the row's append-only
// digest, and the report section `task show` renders).
//
// The fixture is the delivery run's own report line (run 01a12ee4), which that
// run measured against a RED package: `go test ./internal/standards/` printed
// FAIL exit 1 on the same rev the filtered command printed `ok` from.

package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/taskreport"
)

// filteredSeamReport is the motivating report shape: every required section
// present, the package-level reading taken from a `-run`-narrowed command.
const filteredSeamReport = `# Task Report: bound the holder scan

## Completed
- the fix is on the branch

## Files
- internal/scratch/scan.go

## Verification
- **Build**: ` + "`go build ./...`" + ` → exit 0 (no output).
- **Tests**: ` + "`go test -run 'TestConfigDoc|TestCensusGate' ./internal/standards/`" + ` → ` + "`ok`" + `.

## Not Done
- (nothing)
`

// TestSealTaskReportRecordsTheFilteredPackageClaim: the seal RECORDS the
// reading on the row's digest — with the remedy — and never rewrites the report.
func TestSealTaskReportRecordsTheFilteredPackageClaim(t *testing.T) {
	const taskID = "t-filtered-seam"
	dir := reportSandbox(t, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, taskreport.FileName)
	if err := os.WriteFile(path, []byte(filteredSeamReport), 0o644); err != nil {
		t.Fatalf("seed report: %v", err)
	}

	line, err := SealTaskReport(taskID)
	if err != nil {
		t.Fatalf("SealTaskReport: %v", err)
	}
	if !strings.Contains(line, taskreport.FilteredClaimDigestPrefix) {
		t.Fatalf("the row's digest records nothing about a package-level reading taken from a "+
			"filter-narrowed command:\n%q", line)
	}
	if !strings.Contains(line, taskreport.FilteredClaimRemedy) {
		t.Errorf("the record must carry the remedy (re-run the package itself), got:\n%q", line)
	}
	if !strings.Contains(line, "-run") {
		t.Errorf("the record must name the flag that narrowed the selection, got:\n%q", line)
	}
	// Recorded, never rewritten: the authored report keeps every byte.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(body) != filteredSeamReport {
		t.Errorf("the seal rewrote the report; got:\n%s", body)
	}
}

// TestSealTaskReportIsSilentOnAWholePackageReading: the counterexample at the
// seam — the honest shape must leave no finding on the row.
func TestSealTaskReportIsSilentOnAWholePackageReading(t *testing.T) {
	const taskID = "t-filtered-silent"
	dir := reportSandbox(t, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	honest := strings.Replace(filteredSeamReport,
		"`go test -run 'TestConfigDoc|TestCensusGate' ./internal/standards/`",
		"`make test-budget PKGS='./internal/standards'`", 1)
	if err := os.WriteFile(filepath.Join(dir, taskreport.FileName), []byte(honest), 0o644); err != nil {
		t.Fatalf("seed report: %v", err)
	}
	line, err := SealTaskReport(taskID)
	if err != nil {
		t.Fatalf("SealTaskReport: %v", err)
	}
	if strings.Contains(line, taskreport.FilteredClaimDigestPrefix) {
		t.Errorf("false positive on an honest whole-package reading:\n%q", line)
	}
}

// TestTaskReportShowBlockRendersTheFilteredPackageClaim: the finding reaches the
// surface the operator actually reads — `task show` — beside the authored text,
// never instead of it.
func TestTaskReportShowBlockRendersTheFilteredPackageClaim(t *testing.T) {
	const taskID = "t-filtered-show"
	dir := reportSandbox(t, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, taskreport.FileName), []byte(filteredSeamReport), 0o644); err != nil {
		t.Fatalf("seed report: %v", err)
	}
	block := TaskReportShowBlock(taskID, 0, "")
	if !strings.Contains(block, TaskShowReportFilteredLabel) {
		t.Fatalf("`task show` must render the recorded finding under %q:\n%s", TaskShowReportFilteredLabel, block)
	}
	for _, want := range []string{taskreport.FilteredClaimWarningLabel, "-run", "go test"} {
		if !strings.Contains(block, want) {
			t.Errorf("the show block does not name %q:\n%s", want, block)
		}
	}
	// The authored sentence is still rendered: the finding rides BESIDE it.
	if !strings.Contains(block, "the fix is on the branch") {
		t.Errorf("the show block must still render the report itself:\n%s", block)
	}
}

// TestTaskReportShowBlockIsSilentOnAWholePackageReading: no finding, no label —
// the honest report must not be labelled.
func TestTaskReportShowBlockIsSilentOnAWholePackageReading(t *testing.T) {
	const taskID = "t-filtered-show-silent"
	dir := reportSandbox(t, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	honest := strings.Replace(filteredSeamReport,
		"`go test -run 'TestConfigDoc|TestCensusGate' ./internal/standards/`",
		"`go test ./internal/standards/`", 1)
	if err := os.WriteFile(filepath.Join(dir, taskreport.FileName), []byte(honest), 0o644); err != nil {
		t.Fatalf("seed report: %v", err)
	}
	block := TaskReportShowBlock(taskID, 0, "")
	if strings.Contains(block, TaskShowReportFilteredLabel) {
		t.Errorf("false positive on an honest whole-package reading:\n%s", block)
	}
}
