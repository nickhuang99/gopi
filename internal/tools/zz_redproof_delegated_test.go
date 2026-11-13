package tools

// zz_redproof_delegated_test.go — the A/B red proof for the result-delegation
// record, written against the PRE-FIX API ONLY (SealTaskReport + the digest line
// it returns + reportSandbox from task_report_test.go; no field, constant or
// function added by the change), so the very same file compiles and runs on both
// trees:
//
//	pre-fix tree (master 1508e3821): FAILS — the seal returns a digest that says
//	  nothing about the report handing its acceptance conclusion to a path, so a
//	  run whose acceptance stream exits 2 afterwards reaches the reviewer looking
//	  complete.
//	post-fix tree: PASSES — the same delegation is recorded, with the path's
//	  existence at seal time.
//
// Reproduce:
//
//	git worktree add --detach <scratch>/delegated-ab master
//	cp internal/tools/zz_redproof_delegated_test.go <scratch>/delegated-ab/internal/tools/
//	cd <scratch>/delegated-ab && go test -run TestZZRedProofDelegatedResult ./internal/tools/
//
// The asserted record prefix is a LITERAL here on purpose: this file must compile
// against the pre-fix tree, where no constant for it exists (the
// zz_redproof_rancheck / zz_redproof_gateclaim precedent). Production code never
// uses the literal — it references the package constant (Rules 1b/13).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zzRedProofDelegatedPrefix is the digest prefix the fix records a delegated
// gate conclusion under (taskreport.DelegatedDigestPrefix's value, spelled out
// because this file must compile without the fix).
const zzRedProofDelegatedPrefix = "report-result-delegated: "

// zzRedProofDelegatedAbsent is the status the fix stamps when the delegated path
// is not there at seal time (taskreport.DelegatedPathStatusAbsent's value,
// spelled out for the same reason).
const zzRedProofDelegatedAbsent = "ABSENT"

// zzRedProofDelegatedReport is the motivating report shape (row 01a12d6b,
// 2026-10-12), verbatim: every required section present, the acceptance
// conclusion handed to a path, no result stated for it — and, in the run that
// shipped it, an acceptance stream that afterwards exited 2 with two red tests.
const zzRedProofDelegatedReport = `# Task Report: capture an identity once the child's argv is published

## Completed
- the fix is on the branch

## Files
- internal/testutil/proc_identity.go

## Verification
- **Build**: ` + "`go build ./...`" + ` → OK (exit 0).
- **Tests**: ` + "`make test-budget PKGS='./internal/testutil'`" + ` → see the stream at
  ` + "`artifacts/make-testbudget-testutil.txt`" + `.

## Not Done
- the pre-existing red pair, proven at my own base commit
`

func TestZZRedProofDelegatedResult(t *testing.T) {
	const taskID = "t-zz-redproof-delegated"
	dir := reportSandbox(t, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "task-report.md") // taskreport.FileName's value
	if err := os.WriteFile(path, []byte(zzRedProofDelegatedReport), 0o644); err != nil {
		t.Fatalf("seed report: %v", err)
	}

	line, err := SealTaskReport(taskID)
	if err != nil {
		t.Fatalf("SealTaskReport: %v", err)
	}
	if !strings.Contains(line, zzRedProofDelegatedPrefix) {
		t.Fatalf("UNRECORDED DELEGATION: the report hands its acceptance conclusion to a path and states no result, and the row's digest records nothing about it: %q", line)
	}
	if !strings.Contains(line, "see the stream at") {
		t.Errorf("the record must quote the author's own sentence, got %q", line)
	}
	if !strings.Contains(line, "artifacts/make-testbudget-testutil.txt") {
		t.Errorf("the record must name the path the report delegates to, got %q", line)
	}
	if !strings.Contains(line, zzRedProofDelegatedAbsent) {
		t.Errorf("the record must say whether the path exists at seal time, got %q", line)
	}
	// Recorded, never rewritten: the authored report keeps every byte.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(body) != zzRedProofDelegatedReport {
		t.Errorf("the seal rewrote the report; got:\n%s", body)
	}
}
