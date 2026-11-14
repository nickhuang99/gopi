package tools

// Pre-merge check-failure TEXT tests (the delivery path's refusal detail).
//
// THE DEFECT (measured, task 01a12ba1): the check runner quoted a failed check
// with mergeBuildGateTail(mergeBuildGateErrorText(output)) — the HEAD of the
// output, cut again to 300 bytes on the way into the log. For a compiler that
// is right (mergeBuildGateErrorText exists because `go build` states the reason
// first). For `go test` it is exactly wrong: the output is one block per
// package as each package finishes, so its head is a PASSING package's `ok`
// line, and the refusal an operator read named no cause at all.
//
// Two arms, deliberately separate:
//
//	① the PURE extraction, whose input is the litteral text a real toolchain
//	   produces (both measured orders of `go test` output, a compiler's head
//	   diagnostics, a command that is neither);
//	② the RUNNER, executed for real against a revision whose
//	   ./internal/standards fails while six sibling packages pass — the refusal
//	   must carry that package's message (Rule 31: run the surface).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/scratch"
)

const (
	// pmcSettingsName is the settings file the fixture writes.
	pmcSettingsName = "settings.json"
	// pmcGoModBody is the fixture module: stdlib only (GOPROXY=off, and the
	// checks run against the gate's own module cache).
	pmcGoModBody = "module pmcfixture\n\ngo 1.21\n"
	// pmcFailPkgDir / pmcFailPkgFile are the failing package the refusal must
	// name — the shape of the real `internal/standards` at the incident.
	pmcFailPkgDir  = "internal/standards"
	pmcFailPkgFile = "std_test.go"
	// pmcPassPkgCount is how many passing packages lead the run. Six of them, in
	// the order `go test` reports (package PATH), fill the pre-fix quote's whole
	// line budget before the failing package is reached.
	// pmcPassPkgCount is how many passing packages lead the run, and it is
	// DERIVED from the pre-fix quote's own line budget: the passing packages are
	// reported before the failing one (package PATH order), so this many lines
	// spend the whole budget before the failure is ever reached. A smaller
	// fixture would let the pre-fix quote carry the message and quietly lose the
	// red proof (measured: with six passing packages the head still contained
	// the FAIL block).
	pmcPassPkgCount = mergeCheckPreFixHeadLines

	// mergeCheckPreFixHeadLines is the line budget the pre-fix quote spent on the
	// output's head (mergeBuildGateErrorLines — the extraction it used).
	mergeCheckPreFixHeadLines = mergeBuildGateErrorLines
	// pmcCheckCmd is the check the runner is given: the command whose failing
	// output the refusal must quote.
	pmcCheckCmd = "go test ./..."
	// pmcCheckTimeoutSec bounds the fixture's own test run.
	pmcCheckTimeoutSec = 120
	// pmcFilePerm is the fixture's file mode.
	pmcFilePerm = 0o644
	// pmcFailSummaryLine / pmcOkLine are the toolchain's own output shapes,
	// spelled once so the fixtures and the assertions cannot drift apart.
	// pmcPassPkgDir is the directory the passing packages live under: it sorts
	// BEFORE pmcFailPkgDir, and that is what puts the FAIL block after the
	// passing lines (measured: `go test` reports packages in package-PATH order,
	// not in completion order).
	pmcPassPkgDir = "internal/a"

	pmcFailSummaryLine = "FAIL\tpmcfixture/internal/standards\t0.003s"
	pmcOkLine          = "ok  \tpmcfixture/p1\t0.001s"
)

// pmcAssertionText is the line a reader must be able to act on: the failing
// test's own message (the reason the check failed). It is the text the pre-fix
// head-and-300-bytes quote did NOT contain.
const pmcAssertionText = `published anchor pkg/a.go:3 says "beta" but the source line is "alpha" - the document was NOT regenerated`

// pmcFailBlock returns the failing package's own block, exactly as `go test`
// prints it.
func pmcFailBlock() string {
	return strings.Join([]string{
		"--- FAIL: TestPublishedAnchorResolves (0.00s)",
		"    " + pmcFailPkgFile + ":11: " + pmcAssertionText,
		"FAIL",
		pmcFailSummaryLine,
	}, "\n")
}

// pmcOkLines returns n passing packages' success lines (the head the pre-fix
// quote kept).
func pmcOkLines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "ok  \tpmcfixture/p%d\t0.00%ds\n", i, i)
	}
	return b.String()
}

// TestMergeCheckFailureText_KeepsTheFailingTestsMessageBothOrders is the core
// regression: whatever order the toolchain prints the blocks in — MEASURED both
// ways on this host, the order following the PACKAGE PATH, so a failing package
// that sorts FIRST prints its block first and one that sorts LAST prints it at
// the end — the FAILING package's own message survives the cut.
//
// The same test states the RED PROOF explicitly: the quoting this change
// replaces (the head, cut again to the 300-byte log bound) is asserted to LOSE
// that message in the failure-last order — the order the incident measured — so
// a "fix" that quietly kept the old quoting cannot pass this test.
func TestMergeCheckFailureText_KeepsTheFailingTestsMessageBothOrders(t *testing.T) {
	orders := map[string]struct {
		out     string
		redHead bool
	}{
		// The order the synthetic-module probe measured: the failing package's
		// block first, the passing packages' lines after it. Here the head alone
		// happens to carry the message — which is exactly why the SECOND order
		// below is the one that states the defect.
		"failure-first (a failing package sorting FIRST)": {
			out: pmcFailBlock() + "\n" + pmcOkLines(pmcPassPkgCount) + "FAIL\n", redHead: false},
		// The incident's shape: the failing package sorts AFTER the passing ones
		// (in the real tree, `internal/standards` follows everything under
		// `internal/<a..r>`), so the head is spent before the failure is printed.
		"failure-last (the incident's order)": {
			out: pmcOkLines(pmcPassPkgCount) + pmcFailBlock() + "\nFAIL\n", redHead: true},
	}
	for name, order := range orders {
		t.Run(name, func(t *testing.T) {
			old := mergeBuildGateTail(mergeBuildGateErrorText(order.out))
			if order.redHead {
				// THE RED PROOF, measured: the head window spent its 12 lines on
				// the passing packages that sort before the failure, and the
				// 300-byte bound the log applies then cut the message in half —
				// so the quote an operator read carried neither the failing
				// test's message nor the name of the package that failed.
				if strings.Contains(old, pmcAssertionText) || strings.Contains(old, pmcFailSummaryLine) {
					t.Fatalf("the pre-fix quote already carried the failure (%q) — the fixture does not reproduce the "+
						"defect it exists for", old)
				}
				if !strings.Contains(old, pmcOkLine) {
					t.Fatalf("the pre-fix quote must open with the PASSING packages' lines (that is where its budget "+
						"went), got %q", old)
				}
			}
			got := mergeCheckFailureText(pmcCheckCmd, order.out)
			if !strings.Contains(got, pmcAssertionText) {
				t.Fatalf("the refusal must carry the failing test's message, got:\n%s", got)
			}
			if !strings.Contains(got, mergeCheckTestResultPrefix+mergeCheckTestFailVerb) {
				t.Errorf("the refusal must name the failing test, got:\n%s", got)
			}
			if !strings.Contains(got, pmcFailSummaryLine) {
				t.Errorf("the refusal must name the failing PACKAGE, got:\n%s", got)
			}
			if n := strings.Count(got, "\n") + 1; n > mergeBuildGateErrorLines {
				t.Errorf("kept %d lines, want at most %d:\n%s", n, mergeBuildGateErrorLines, got)
			}
			if len(got) > mergeBuildGateErrorMaxBytes {
				t.Errorf("kept %d bytes, want at most %d", len(got), mergeBuildGateErrorMaxBytes)
			}
			if strings.Contains(got, pmcOkLine) {
				t.Errorf("a PASSING package's success line must not be what the refusal quotes:\n%s", got)
			}
		})
	}
}

// TestMergeCheckFailureText_ReadsTheBlockOutOfAnIndentedReport is the regression
// for the shape that reached an operator on 2026-10-31 (task 01a18f6e): the
// lane quotes a REPORT (`scripts/test-budget.sh`), not raw `go test` output, and
// the report indents its marker line and the test's own body under it. The
// detector used to require `--- FAIL:` at column 0, so it matched NOTHING, the
// extraction fell back to a both-ends quote, and the two windows it kept held
// the run's header and an unrelated `[setup failed]` package — the failing
// test's name never appeared in the refusal an operator read.
//
// Three things are pinned here, and the THIRD is the one that makes the quote
// worth reading: the block ENDS where the quoting text resumes (the next
// section), never at the end of the output.
func TestMergeCheckFailureText_ReadsTheBlockOutOfAnIndentedReport(t *testing.T) {
	const reportIndent = "  "
	const bodyIndent = "      "
	var b strings.Builder
	// The report's own preamble — the head window the pre-fix quote spent itself on.
	fmt.Fprintf(&b, "=== gopi test budget run ===\n")
	fmt.Fprintf(&b, "packages: ./cmd/agent/... ./internal/standards/...\n")
	fmt.Fprintf(&b, "host:     load 18.36 13.91 12.65  cores 32\n")
	fmt.Fprintf(&b, "%s\n", mergeBuildGateErrorElision)
	// An UNRELATED package that failed to build, quoted by an earlier section.
	fmt.Fprintf(&b, "%sFAIL pmcfixture/scripts/lib/ledgersql [setup failed]\n", bodyIndent)
	fmt.Fprintf(&b, "%s(no compiler diagnostic carries this package's import path)\n", bodyIndent)
	// THE failing test, indented exactly as the report renders it.
	fmt.Fprintf(&b, "%s--- FAIL: %s  (pmcfixture/internal/standards)\n", reportIndent, pmcFailTestName)
	fmt.Fprintf(&b, "%s=== RUN   %s\n", bodyIndent, pmcFailTestName)
	fmt.Fprintf(&b, "%s%s\n", bodyIndent, pmcAssertionText)
	fmt.Fprintf(&b, "%s--- FAIL: %s (0.00s)\n", bodyIndent, pmcFailTestName)
	// The report RESUMES: its next section, at the marker's own column. Without
	// the indent-aware end, this line and everything after it were quoted as if
	// they were the failing test's own output.
	fmt.Fprintf(&b, "%sfull stream: /scratch/run-1793408602-3647113.json\n", reportIndent)
	fmt.Fprintf(&b, "%sstamp: 2026-10-31T09:03:22+08:00 host=host-a exit=1 outcome=fail\n", reportIndent)

	got := mergeCheckFailureText(pmcCheckCmd, b.String())
	if !strings.Contains(got, pmcFailTestName) {
		t.Fatalf("the refusal must name the failing test %q, got:\n%s", pmcFailTestName, got)
	}
	if !strings.Contains(got, pmcAssertionText) {
		t.Errorf("the refusal must carry the failing test's own message, got:\n%s", got)
	}
	if !strings.Contains(got, pmcFailPkgDir) {
		t.Errorf("the refusal must name the failing package %q, got:\n%s", pmcFailPkgDir, got)
	}
	if strings.Contains(got, "ledgersql") {
		t.Errorf("the quote must not carry an UNRELATED package's [setup failed] line — the block is bounded by the "+
			"quoting text resuming, not by the end of the output:\n%s", got)
	}
	if strings.Contains(got, "stamp:") {
		t.Errorf("the quote must stop where the report resumes, got:\n%s", got)
	}
	if t.Failed() {
		t.Logf("the refusal an operator reads: %s", got)
	}
}

// TestMergeCheckFailureText_LeavesACompilerDiagnosticAtTheHead pins the other
// direction (the one that must NOT change): a declaring check longer than the
// budget whose reason is printed first keeps its head — the behaviour
// mergeBuildGateErrorText was written for, and the shape every build contract
// in this repo has.
func TestMergeCheckFailureText_LeavesACompilerDiagnosticAtTheHead(t *testing.T) {
	lines := make([]string, 0, mergeBuildGateErrorLines*3)
	for i := 0; i < mergeBuildGateErrorLines*3; i++ {
		lines = append(lines, fmt.Sprintf("cmd/agent/merger_self_restart.go:%d:47: undefined: mergerDaemonLogDirMode", 373+i))
	}
	out := strings.Join(lines, "\n") + "\nexit status 1\n"
	got := mergeCheckFailureText(mergeBuildGateGoDefaultCmd, out)
	want := mergeBuildGateErrorText(out)
	if got != want {
		t.Fatalf("a non-test check must be quoted exactly as before\n got: %q\nwant: %q", got, want)
	}
	if !strings.HasPrefix(got, "cmd/agent/merger_self_restart.go:373:47: undefined: mergerDaemonLogDirMode") {
		t.Fatalf("the first diagnostic must survive the cap, got %q", got)
	}
}

// TestMergeCheckFailureText_NoTestResultKeepsBothEnds: a check that failed
// WITHOUT printing a test result (a package that does not compile under `go
// test`, a non-Go tool) has no block to read out, so BOTH ends of its output
// are kept — the head can hold a diagnostic and the tail can hold a summary,
// and the elision marker says that lines were dropped between them.
func TestMergeCheckFailureText_NoTestResultKeepsBothEnds(t *testing.T) {
	var b strings.Builder
	for i := 0; i < mergeBuildGateErrorLines*3; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	b.WriteString("the last line is the summary\n")
	out := b.String()
	got := mergeCheckFailureText(pmcCheckCmd, out)
	if !strings.Contains(got, "line 0") {
		t.Errorf("the head must survive, got:\n%s", got)
	}
	if !strings.Contains(got, "the last line is the summary") {
		t.Errorf("the tail must survive, got:\n%s", got)
	}
	if !strings.Contains(got, mergeBuildGateErrorElision) {
		t.Errorf("a cut that dropped lines must say so, got:\n%s", got)
	}
	if len(got) > mergeBuildGateErrorMaxBytes {
		t.Errorf("kept %d bytes, want at most %d", len(got), mergeBuildGateErrorMaxBytes)
	}
	// And the same text, unchanged, when it fits whole: an elision marker in a
	// short failure would claim a cut that never happened.
	short := "one\n\ntwo\n"
	if got := mergeCheckFailureText(pmcCheckCmd, short); got != "one\ntwo" {
		t.Errorf("a short output must be quoted whole, got %q", got)
	}
}

// TestMergeCheckFailureText_OnlyTheGoTestVerbSelectsTheTestArm pins the command
// shape: `go test ./...` is the test arm, while a build, a vet, and a command
// that merely CONTAINS the verb elsewhere are not.
func TestMergeCheckFailureText_OnlyTheGoTestVerbSelectsTheTestArm(t *testing.T) {
	for _, cmd := range []string{"go test ./...", "go test -run X ./internal/tools/", "cd X && go test ./..."} {
		if !mergeCheckIsTestCommand(cmd) {
			t.Errorf("%q must be recognised as a test run", cmd)
		}
	}
	for _, cmd := range []string{"go build ./...", "go vet ./...", "./gotestsum", "sh -c 'go build ./...'"} {
		if mergeCheckIsTestCommand(cmd) {
			t.Errorf("%q must not be recognised as a test run", cmd)
		}
	}
}

// TestFinishCleanupChecksIn_QuotesTheFailingPackage is the RUNNER arm, on a real
// git repo and a real `go test ./...` (Rule 31): six packages pass and
// ./internal/standards fails, so the output's head is somebody else's `ok` line
// — and the refusal the runner returns must still carry the failing package's
// own message and its name.
func TestFinishCleanupChecksIn_QuotesTheFailingPackage(t *testing.T) {
	repo := pmcWriteRepo(t)
	pmcPointCaches(t)
	pmcCommitAll(t, repo, "fixture: six passing packages and one failing gate")

	res := finishCleanupChecksIn(repo,
		[]mergeCheck{{Name: mergeCheckBuildName, Cmd: pmcCheckCmd, TimeoutSec: pmcCheckTimeoutSec}})

	if res.Status != finishCleanupPreMergeStatusRefused {
		t.Fatalf("a failing check must REFUSE, got status=%q detail=%q", res.Status, res.Detail)
	}
	if res.Check != mergeCheckBuildName {
		t.Errorf("the refusal must name the check, got %q", res.Check)
	}
	if !strings.Contains(res.Detail, pmcAssertionText) {
		t.Errorf("the refusal must quote the failing test's message, got:\n%s", res.Detail)
	}
	if !strings.Contains(res.Detail, pmcFailPkgDir) {
		t.Errorf("the refusal must name the failing package %q, got:\n%s", pmcFailPkgDir, res.Detail)
	}
	// The quote is the toolchain's OWN line, verbatim: the test file and the
	// rendered message, with the file:line prefix the toolchain adds.
	if want := pmcFailPkgFile + ":"; !strings.Contains(res.Detail, want) {
		t.Errorf("the refusal must quote the toolchain's own line (want a %q prefix), got:\n%s", want, res.Detail)
	}
	if len(res.Detail) > mergeBuildGateErrorMaxBytes+len(res.Check)+len(pmcCheckCmd)+4 {
		t.Errorf("the refusal detail is %d bytes — it must respect the extraction's own bound", len(res.Detail))
	}
}

// pmcStageCLIPackage stages the smoke entry point's own PREREQUISITE — the
// package `scripts/test-budget.sh` builds its tested binary from
// (finishCleanupCLIPackageDir). Without it the declared entry point reports the
// run as "not evidence about this tree" and the lane ABSTAINS instead of
// refusing, which is a different arm from the one these two tests assert.
func pmcStageCLIPackage(t *testing.T, dir string) {
	t.Helper()
	pmcWriteFile(t, filepath.Join(dir, finishCleanupCLIPackageDir, "main.go"),
		"package main\n\nfunc main() {}\n")
}

// TestBranchCheckRunQuotesTheFailingPackage is the END-TO-END arm on the
// surface that replaced the trial merge: the branch's OWN test run, in the
// branch's own worktree, at the branch's own revision. The branch introduces a
// package whose test fails, so the refusal must carry THAT package's message and
// its name — the incident's shape (a refusal that named a passing package and no
// cause) is exactly what this asserts against.
//
// It also pins the two halves of the lane's SCOPE: the packages come from the
// branch's diff against the integration branch, and the check runs in the ground
// the branch is checked out in.
func TestBranchCheckRunQuotesTheFailingPackage(t *testing.T) {
	f := newLocalSyncFixture(t)
	preMergeSeedIsolation(t)
	pmcPointCaches(t)
	// The tree must DECLARE the scope target before the lane will run anything
	// in it (Rule 12: the contract is read off the tree, never invented) — so
	// the fixture stages the repo's own Makefile and entry point, exactly as the
	// production lane will find them.
	pmsStageEntryPoint(t, pmsRepoRoot(t), f.wtDir)
	pmcStageCLIPackage(t, f.wtDir)
	// The branch commits the module and the failing package (the ground's own
	// revision is what is measured — no merge is involved any more).
	pmcWriteFile(t, filepath.Join(f.wtDir, mergeBuildGateGoModFile), pmcGoModBody)
	for i := 1; i <= pmcPassPkgCount; i++ {
		pmcWriteFile(t, filepath.Join(f.wtDir, pmcPassPkgDir, fmt.Sprintf("a%d", i), fmt.Sprintf("a%d_test.go", i)),
			fmt.Sprintf("package a%d\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) {}\n", i))
	}
	pmcWriteFile(t, filepath.Join(f.wtDir, pmcFailPkgDir, pmcFailPkgFile), pmcFailPkgBody())
	f.commitInWorktree(t, mergeBuildGateGoModFile, pmcGoModBody, "branch: the module")
	preMergeGit(t, f.wtDir, "add", "-A")
	preMergeGit(t, f.wtDir, "commit", "-q", "-m", "branch: a package whose test fails")

	res := finishCleanupBranchCheckVerify(context.Background(), f.repoDir, f.wtDir, f.featBranch)

	if res.Status != finishCleanupPreMergeStatusRefused {
		t.Fatalf("a branch whose own tests fail must REFUSE, got status=%q detail=%q", res.Status, res.Detail)
	}
	if !strings.Contains(res.Detail, finishCleanupBranchCheckFailMarker) {
		t.Errorf("the refusal must name the branch's own test run, got:\n%s", res.Detail)
	}
	if !strings.Contains(res.Detail, pmcFailPkgDir) {
		t.Errorf("the refusal must name %q, got:\n%s", pmcFailPkgDir, res.Detail)
	}
	if !strings.Contains(res.Detail, pmcFailTestName) {
		t.Errorf("the refusal must name the failing test %q, got:\n%s", pmcFailTestName, res.Detail)
	}
	t.Logf("the branch-check refusal an operator reads: %s", res.Detail)
}

// TestBranchCheckIsRecordedUnderItsOwnVerdictName pins the audit trail: the
// verdict reaches the delivery's OWN row, under the name that verdict earns —
// so "the lane ran and passed", "it abstained" and "it proved the branch
// undeliverable" are three distinguishable observations on the ledger, never one
// silence (Rule 15).
func TestBranchCheckIsRecordedUnderItsOwnVerdictName(t *testing.T) {
	f := newLocalSyncFixture(t)
	dbPath := preMergeSeedIsolation(t)
	pmcPointCaches(t)
	pmsStageEntryPoint(t, pmsRepoRoot(t), f.wtDir)
	pmcStageCLIPackage(t, f.wtDir)
	pmcWriteFile(t, filepath.Join(f.wtDir, mergeBuildGateGoModFile), pmcGoModBody)
	pmcWriteFile(t, filepath.Join(f.wtDir, pmcFailPkgDir, pmcFailPkgFile), pmcFailPkgBody())
	preMergeGit(t, f.wtDir, "add", "-A")
	preMergeGit(t, f.wtDir, "commit", "-q", "-m", "branch: the failing package")

	res := finishCleanupBranchCheckVerify(context.Background(), f.repoDir, f.wtDir, f.featBranch)
	if res.Status != finishCleanupPreMergeStatusRefused {
		t.Fatalf("fixture invariant: the branch's tests must be red, got %+v", res)
	}
	finishCleanupBranchCheckReason("t-close-out-records", res)

	db, err := openTasksDB(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open the sandbox ledger: %v", err)
	}
	defer db.Close()
	event, detail := preMergeLaneEvent(t, db, "t-close-out-records", finishCleanupBranchCheckFailMarker)
	if event != finishCleanupPreMergeRefusedEvent {
		t.Fatalf("a red branch must be recorded as %q, got %q", finishCleanupPreMergeRefusedEvent, event)
	}
	if !strings.Contains(detail, pmcFailPkgDir) {
		t.Errorf("the recorded reason must carry the finding, got %q", detail)
	}
}

// TestBranchCheckVerdictNamesAreDistinctAndTotal pins the mapping itself: the
// three verdicts MUST map onto three different event names, and every status
// maps onto some name — an unrecognised verdict takes the refusal arm, because a
// name nobody can classify must never read back as a pass.
func TestBranchCheckVerdictNamesAreDistinctAndTotal(t *testing.T) {
	seen := map[string]string{}
	for _, status := range []string{
		finishCleanupPreMergeStatusVerified,
		finishCleanupPreMergeStatusSkipped,
		finishCleanupPreMergeStatusRefused,
		"some-status-nobody-declared",
	} {
		name := finishCleanupBranchCheckEventName(status)
		if name == "" {
			t.Fatalf("status %q maps onto an empty event name", status)
		}
		if prev, dup := seen[name]; dup && status != "some-status-nobody-declared" {
			t.Fatalf("statuses %q and %q share the event name %q — a reader cannot tell them apart", prev, status, name)
		}
		seen[name] = status
	}
	if got := finishCleanupBranchCheckEventName("some-status-nobody-declared"); got != finishCleanupPreMergeRefusedEvent {
		t.Errorf("an unknown verdict must take the refusal name, got %q", got)
	}
	if finishCleanupPreMergeVerifiedEvent == finishCleanupPreMergeUnverifiedEvent ||
		finishCleanupPreMergeVerifiedEvent == finishCleanupPreMergeRefusedEvent ||
		finishCleanupPreMergeUnverifiedEvent == finishCleanupPreMergeRefusedEvent {
		t.Errorf("the three verdict names must be distinct: %q %q %q",
			finishCleanupPreMergeVerifiedEvent, finishCleanupPreMergeUnverifiedEvent, finishCleanupPreMergeRefusedEvent)
	}
}

// pmcFailPkgBody is the failing package's source: one test that asserts a lie.
// It carries no timing trick — the ORDER that matters is decided by the
// package's PATH (pmcPassPkgDir/pmcFailPkgDir), which is what `go test` sorts.
func pmcFailPkgBody() string {
	return `package standardgate

import (
	"strings"
	"testing"
)

func TestPublishedAnchorResolves(t *testing.T) {
	got := strings.TrimSpace("alpha")
	if got != "beta" {
		t.Errorf("published anchor pkg/a.go:3 says %q but the source line is %q - the document was NOT regenerated", "beta", got)
	}
}
`
}

// --- fixture (the two caches the runner requires, never the operator's) ---

// pmcWriteRepo materialises the fixture repo: a Go module with
// pmcPassPkgCount packages that pass and one package whose test fails.
func pmcWriteRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	pmcWriteFile(t, filepath.Join(repo, mergeBuildGateGoModFile), pmcGoModBody)
	for i := 1; i <= pmcPassPkgCount; i++ {
		dir := filepath.Join(repo, pmcPassPkgDir, fmt.Sprintf("a%d", i))
		pmcWriteFile(t, filepath.Join(dir, fmt.Sprintf("a%d.go", i)),
			fmt.Sprintf("package a%d\n\nfunc F() int { return %d }\n", i, i))
		pmcWriteFile(t, filepath.Join(dir, fmt.Sprintf("a%d_test.go", i)),
			fmt.Sprintf("package a%d\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) { _ = F() }\n", i))
	}
	pmcWriteFile(t, filepath.Join(repo, pmcFailPkgDir, pmcFailPkgFile), pmcFailPkgBody())
	// The ledger is sandboxed: the runner's own audit write must never reach the
	// operator's database (Rule 32).
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	settings := filepath.Join(t.TempDir(), pmcSettingsName)
	pmcWriteFile(t, settings, `{"repos":{}}`)
	t.Setenv("GOPI_SETTINGS", settings)
	return repo
}

// pmcWriteFile writes one fixture file, creating its directory.
func pmcWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), scratch.DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), pmcFilePerm); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// pmcCommitAll commits the fixture tree and returns the revision the checks run
// against (the runner takes a REV, never the working tree).
func pmcCommitAll(t *testing.T, repo, message string) string {
	t.Helper()
	preMergeGit(t, repo, "init", "-q", "-b", masterBranchName)
	preMergeGit(t, repo, "config", "user.email", "t@t")
	preMergeGit(t, repo, "config", "user.name", "t")
	preMergeGit(t, repo, "add", "-A")
	preMergeGit(t, repo, "commit", "-q", "-m", message)
	return preMergeGit(t, repo, "rev-parse", "HEAD")
}

// pmcPointCaches gives the runner the two caches it requires.
//
// THE BUILD CACHE is the suite's own sandbox: testmain_test.go installs it in
// TestMain through scratch.GoCacheRootEnvName, from a candidate list chosen so
// the PRODUCTION gate accepts the placement (off the `/` partition, outside the
// collected scratch tree), and a guard test fails the suite when that sandbox
// did not engage. This fixture therefore does NOT override the pin — the
// property being exercised is the runner's real resolution path, and overriding
// it here would test the override instead.
//
// THE MODULE CACHE is a prepared EMPTY cache of this test's own: all the
// runner's resolver asks is that the path EXISTS and that it carries the
// toolchain's `cache/download` subtree — the fixture module is stdlib-only, so
// nothing is ever fetched. It is pointed at the fixture through the documented
// override rather than at the operator's cache, so the run reads nothing of the
// operator's (Rule 22/32).
func pmcPointCaches(t *testing.T) {
	t.Helper()
	modCache := filepath.Join(t.TempDir(), "gomodcache")
	if err := os.MkdirAll(filepath.Join(modCache, mergeBuildGateCacheDownloadDirName, mergeBuildGateCacheDownloadSubtree), scratch.DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", modCache, err)
	}
	t.Setenv(mergeBuildGateModCacheEnvName, modCache)
	// The fixture module is stdlib-only, so no module fetch is allowed and no
	// go.sum is needed; the same flags the repo's own fixtures use.
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOPROXY", "off")
}

// pmcFailTestName is the failing test's own name — what an operator greps for.
// A refusal must carry it together with the package it lives in: the lane runs
// the repo's declared scoped entry point, whose summary prints the test's name
// and its package and points at its own raw JSON stream for the per-line
// diagnostics, so those two are what the refusal must carry. Declared on its own
// (not in the const block above) so that a comment about it cannot shift the
// line numbers the rest of that block's doc anchors resolve to.
const pmcFailTestName = "TestPublishedAnchorResolves"
