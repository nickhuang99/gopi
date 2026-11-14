package tools

// zz_redproof_filtered_test.go — the A/B red proof for the filtered-package
// claim record, written against the PRE-FIX API ONLY (SealTaskReport + the
// digest line it returns + reportSandbox from task_report_test.go; no field,
// constant or function added by the change), so the very same file compiles and
// runs on both trees:
//
//	pre-fix tree (master before this branch): FAILS — the seal returns a digest
//	  that says nothing about a package-level test reading taken from a
//	  `-run`-narrowed command, so the delivery run 01a12ee4's report line
//	  `ok github.com/nickhuang99/gopi/internal/standards` reaches the reviewer looking
//	  like a green gate on a rev where that package printed FAIL exit 1.
//	post-fix tree: PASSES — the same reading is recorded on the row's digest,
//	  with the flag that narrowed it and the remedy.
//
// Reproduce:
//
//	git worktree add --detach <scratch>/filtered-ab master
//	cp internal/tools/zz_redproof_filtered_test.go <scratch>/filtered-ab/internal/tools/
//	cd <scratch>/filtered-ab && go test -run TestZZRedProofFilteredPackageClaim ./internal/tools/
//
// The asserted record prefix is a LITERAL here on purpose: this file must compile
// against the pre-fix tree, where no constant for it exists (the
// zz_redproof_delegated / zz_redproof_rancheck / zz_redproof_gateclaim
// precedent). Production code never uses the literal — it references the package
// constant (Rules 1b/13).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zzRedProofFilteredPrefix is the digest prefix the fix records a
// filter-narrowed package reading under (taskreport.FilteredClaimDigestPrefix's
// value, spelled out because this file must compile without the fix).
const zzRedProofFilteredPrefix = "report-filtered-package-claim: "

// zzRedProofFilteredRemedy is a distinctive fragment of the remedy the fix
// attaches (taskreport.FilteredClaimRemedy's opening words, spelled out for the
// same reason — and asserted as a property, not as the whole sentence, so the
// finding is not pinned to prose that a later reader may reword).
const zzRedProofFilteredRemedy = "filter REMOVES tests from the selection"

// zzRedProofFilteredReport is the motivating report shape (run 01a12ee4,
// 2026-10-12), verbatim in structure: the run had selected ./internal/standards
// in its own PKGS, killed that run, replaced it with a self-authored `-run`
// filter that matched no gate test, and carried the package-level `ok` line in
// its report — while `go test ./internal/standards/` on the same rev was
// FAIL exit 1.
const zzRedProofFilteredReport = `# Task Report: bound the holder scan

## Completed
- the fix is on the branch

## Files
- internal/scratch/scan.go

## Verification
- **Build**: ` + "`go build ./...`" + ` → exit 0 (no output).
- **Tests** (branch-scoped, the diff's own packages):
  ` + "`go test -run 'Holder|LiveHolders|ProfileRoot|ScratchGC|Sandbox' ./internal/scratch/ ./internal/tools/`" + `
  → ` + "`ok github.com/nickhuang99/gopi/internal/scratch 23.1s`" + `;
  ` + "`go test -run 'TestConfigDoc|TestCensusGate|TestRepoHygiene' ./internal/standards/`" + ` → ` + "`ok`" + `.

## Not Done
- (nothing)
`

func TestZZRedProofFilteredPackageClaim(t *testing.T) {
	const taskID = "t-zz-redproof-filtered"
	dir := reportSandbox(t, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "task-report.md") // taskreport.FileName's value
	if err := os.WriteFile(path, []byte(zzRedProofFilteredReport), 0o644); err != nil {
		t.Fatalf("seed report: %v", err)
	}

	line, err := SealTaskReport(taskID)
	if err != nil {
		t.Fatalf("SealTaskReport: %v", err)
	}
	if !strings.Contains(line, zzRedProofFilteredPrefix) {
		t.Fatalf("UNRECORDED FILTERED READING: the report quotes a package-level `ok` from a command "+
			"narrowed by -run, and the row's digest records nothing about it: %q", line)
	}
	if !strings.Contains(line, "-run") {
		t.Errorf("the record must name the flag that narrowed the selection, got %q", line)
	}
	if !strings.Contains(line, zzRedProofFilteredRemedy) {
		t.Errorf("the record must carry the remedy, got %q", line)
	}
	// Recorded, never rewritten: the authored report keeps every byte.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(body) != zzRedProofFilteredReport {
		t.Errorf("the seal rewrote the report; got:\n%s", body)
	}
}
