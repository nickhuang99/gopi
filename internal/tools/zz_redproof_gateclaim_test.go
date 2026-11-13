package tools

// zz_redproof_gateclaim_test.go — the A/B red proof for the report-gate-claim
// epic, written against the PRE-FIX API ONLY (SealTaskReport + the digest line
// it returns; no field, constant or function added by the fix), so the very
// same file compiles and runs on both trees:
//
//	pre-fix tree (master 9cae9d59): FAILS — the seal returns a digest that
//	  records nothing about the report's unobserved gate claim, so the claim
//	  the run made ("`git status --porcelain` empty in both worktrees") reaches
//	  the reviewer with no record that anything looked.
//	post-fix tree: PASSES — the same claim is recorded on the row's digest,
//	  quoted verbatim, and the report file is left byte-identical.
//
// Reproduce:
//
//	git worktree add --detach /dev/shm/gopi-gc-ab master
//	cp internal/tools/zz_redproof_gateclaim_test.go /dev/shm/gopi-gc-ab/internal/tools/
//	cp internal/tools/task_report_test.go /dev/shm/gopi-gc-ab/internal/tools/   # same helpers
//	cd /dev/shm/gopi-gc-ab && go test -run TestZZRedProofUnobservedGateClaim ./internal/tools/
//
// The asserted record prefix is a LITERAL here on purpose: this file must
// compile against the pre-fix tree, where no constant for it exists (the
// zz_redproof_reclaim_promise precedent). Production code never uses the
// literal — it references the package constant (Rules 1b/13).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zzRedProofClaimPrefix is the digest prefix the fix records unobserved gate
// claims under (taskreport.ClaimDigestPrefix's value, spelled out because this
// file must compile without the fix).
const zzRedProofClaimPrefix = "gate-claims-unobserved: "

// zzRedProofReport is the motivating report: its Verification section makes the
// measured run's claim, and the next line is what that same run's close-out
// machinery printed about the very same worktree.
const zzRedProofReport = `# Task Report: the takeover ladder

## Completed
- the fix is on the branch

## Files
- cmd/agent/merger_takeover_scope.go

## Verification
- ` + "`git status --porcelain` empty in both worktrees (Rule 30)" + `
- worktree is DIRTY (uncommitted work) — left for a human — entries: M cmd/agent/merger_takeover.go

## Not Done
- nothing
`

func TestZZRedProofUnobservedGateClaim(t *testing.T) {
	const taskID = "t-zz-redproof-gateclaim"
	dir := reportSandbox(t, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "task-report.md") // taskreport.FileName's value
	if err := os.WriteFile(path, []byte(zzRedProofReport), 0o644); err != nil {
		t.Fatalf("seed report: %v", err)
	}

	line, err := SealTaskReport(taskID)
	if err != nil {
		t.Fatalf("SealTaskReport: %v", err)
	}
	if !strings.Contains(line, zzRedProofClaimPrefix) {
		t.Fatalf("UNRECORDED CLAIM: the report asserts an empty worktree while the same run's close-out says DIRTY, and the row's digest records nothing about it: %q", line)
	}
	if !strings.Contains(line, "`git status --porcelain` empty in both worktrees (Rule 30)") {
		t.Errorf("the record must quote the offending sentence verbatim, got %q", line)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(body) != zzRedProofReport {
		t.Errorf("the seal must not rewrite the authored report:\n--- got ---\n%s", body)
	}
}
