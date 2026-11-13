package tools

// zz_redproof_rancheck_test.go — the A/B red proof for Rule 31's checked face,
// written against the PRE-FIX API ONLY (SealTaskReport + the digest line it
// returns; no field, constant or function added by the change), so the very
// same file compiles and runs on both trees:
//
//	pre-fix tree (master d2703f1f): FAILS — the seal returns a digest that says
//	  nothing about the report's missing `Ran:` line, so a report that never
//	  states its surface was run reaches the reviewer looking complete.
//	post-fix tree: PASSES — the same gap is recorded on the row's digest.
//
// Reproduce:
//
//	git worktree add --detach /home/nick/workspace/gopi-rule31-ab master
//	cp internal/tools/zz_redproof_rancheck_test.go internal/tools/task_report_test.go \
//	   /home/nick/workspace/gopi-rule31-ab/internal/tools/
//	cd /home/nick/workspace/gopi-rule31-ab && go test -run TestZZRedProofMissingRanLine ./internal/tools/
//
// The asserted record prefix is a LITERAL here on purpose: this file must
// compile against the pre-fix tree, where no constant for it exists (the
// zz_redproof_gateclaim precedent). Production code never uses the literal —
// it references the package constant (Rules 1b/13).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zzRedProofRanPrefix is the digest prefix the fix records a missing `Ran:`
// line under (taskreport.RanCheckDigestPrefix's value, spelled out because this
// file must compile without the fix).
const zzRedProofRanPrefix = "ran-line-missing: "

// zzRedProofRanToken is the line token Rule 31 asks the report to carry —
// spelled out for the same compile-without-the-fix reason (the pre-fix tree
// defines no constant for it either).
const zzRedProofRanToken = "Ran:"

// zzRedProofRanReport is the motivating report shape (2026-09-21): every
// required section present, `## Verification` carrying Build:/Tests: only, and
// a `## Not Done` that claims nothing was skipped — so neither Rule 31's line
// nor a stated reason exists, and nothing in the report can ask whether any
// surface was ever run.
const zzRedProofRanReport = `# Task Report: the manual push face

## Completed
- implemented --task-id on the manual push verb

## Files
- internal/tools/memsync.go

## Verification
- Build: go build ./...: OK
- Tests:  go test ./internal/tools: ok
- Commit: cbed046b

## Not Done
- (nothing)
`

func TestZZRedProofMissingRanLine(t *testing.T) {
	const taskID = "t-zz-redproof-rancheck"
	dir := reportSandbox(t, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "task-report.md") // taskreport.FileName's value
	if err := os.WriteFile(path, []byte(zzRedProofRanReport), 0o644); err != nil {
		t.Fatalf("seed report: %v", err)
	}

	line, err := SealTaskReport(taskID)
	if err != nil {
		t.Fatalf("SealTaskReport: %v", err)
	}
	if !strings.Contains(line, zzRedProofRanPrefix) {
		t.Fatalf("UNRECORDED GAP: the report never states that any surface was run and offers no reason, and the row's digest records nothing about it: %q", line)
	}
	// The record must reach the text `task show` renders (the operator's face):
	// the reader re-adjudicates the body and names the line the contract wants.
	if block := TaskReportShowBlock(taskID, 0, line); !strings.Contains(block, zzRedProofRanToken) {
		t.Errorf("`task show` must name the missing %q line beside the report, got:\n%s", zzRedProofRanToken, block)
	}
	// Recorded, never rewritten: the authored report keeps every byte.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(body) != zzRedProofRanReport {
		t.Errorf("the seal must not rewrite the report:\n%s", body)
	}
}
