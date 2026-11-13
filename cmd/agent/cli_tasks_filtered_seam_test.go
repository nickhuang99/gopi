package main

// cli_tasks_filtered_seam_test.go — the filtered-package claim record, driven
// E2E through the REAL `gopi worker` wrapper (writebackOutcome →
// tools.SealTaskReport), not merely through the seal function.
//
// WHY THIS FILE EXISTS: internal/tools proves what the seal does when it IS
// called; internal/taskreport proves the predicate. This one proves the close-out
// path calls it and that the operator MEETS the finding — the two halves a unit
// test cannot cover (Rule 31's own standard, turned on this change), and the
// pattern the sibling records' seam tests established
// (cli_tasks_delegated_seam_test.go, cli_tasks_rancheck_seam_test.go).
//
// The motivating shape is run 01a12ee4 (2026-10-12): the run selected
// `./internal/standards` in its own PKGS, killed that job, and replaced it with a
// self-authored `-run` filter that matches no gate test — so the report carried
// `ok github.com/nick/gopi/internal/standards` while the package's whole run on
// the same rev was FAIL exit 1.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/taskreport"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// filteredSeamRedReport is the RED arm: a package-level `ok` taken from a
// `-run`-narrowed command, in the report section a run states its gate results
// in.
const filteredSeamRedReport = `# Task Report: bound the holder scan

## Completed
- the fix is on the branch

## Files
- internal/scratch/scan.go

## Verification
- **Build**: ` + "`go build ./...`" + ` → exit 0 (no output).
- **Tests** (branch-scoped, the diff's own packages):
  ` + "`go test -run 'Holder|LiveHolders|ProfileRoot|ScratchGC|Sandbox' ./internal/scratch/ ./internal/tools/`" + ` → ` + "`ok github.com/nick/gopi/internal/scratch 23.1s`" + `;
  ` + "`go test -run 'TestConfigDoc|TestCensusGate|TestRepoHygiene' ./internal/standards/`" + ` → ` + "`ok`" + `.

## Not Done
- (nothing)
`

// filteredSeamWholeReport is the GREEN arm: the SAME run, reported the way this
// check asks for — the package's WHOLE run, whose package line IS a reading of
// the package. A check that fired here would be noise on every honest run, the
// failure mode this whole family refuses (the measurement is in
// devdoc/report-filtered-package-claim.md: 37% of the corpus is the branch-scoped
// habit, and refusing it would refuse the majority of this repo's honest runs).
const filteredSeamWholeReport = `# Task Report: bound the holder scan

## Completed
- the fix is on the branch

## Files
- internal/scratch/scan.go

## Verification
- **Build**: ` + "`go build ./...`" + ` → exit 0 (no output).
- **Tests**: ` + "`make test-budget PKGS='./internal/scratch'`" + ` → ` + "`ok github.com/nick/gopi/internal/scratch 23.1s`" + `;
  ` + "`make test-budget PKGS='./internal/standards'`" + ` → ` + "`ok github.com/nick/gopi/internal/standards 17.5s`" + `.

## Not Done
- (nothing)
`

// filteredSeamRun submits a row and executes it with a mock model whose ONE
// tool call writes reportBody to the harness-exported path, returning the
// wrapper's stderr and the `task show` output — the pattern
// cli_tasks_delegated_seam_test.go established, with ITS delegation-specific
// assertion (the `see the stream at` marker) left out, because a whole-package
// report is not a delegated one and must not be required to look like it.
func filteredSeamRun(t *testing.T, taskID, reportBody string) (workerStderr, show string) {
	t.Helper()
	reportSeamBinary(t)
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	work := t.TempDir()
	repo := reportSeamFixtureRepo(t)

	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_report",
			Name: "bash",
			Args: map[string]interface{}{
				"command": "cat > \"$" + taskreport.PathEnv + "\" <<'FILTERED_EOF'\n" + reportBody + "FILTERED_EOF\n",
			},
		}},
		testutil.Resp{Text: "[TASK_LIST_BEGIN]\n[]\n[TASK_LIST_END]\nTask complete."},
	)
	defer mock.Close()

	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env, "GOPI_MEMORY_DB="+dbPath)

	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"write the report", "--payload", "write the report to the harness-exported path",
		"--task-id", taskID, "--repo", repo}, "", env, work); code != 0 {
		t.Fatalf("task submit: code=%d stderr=%s", code, stderr)
	}
	_, workerStderr, code := runGopiMapped(t, []string{"worker", taskID}, "", env, work)
	if code != 0 {
		t.Fatalf("worker: code=%d stderr=%s", code, workerStderr)
	}
	wantPath := filepath.Join(gopiHome, ".gopi", config.SessionsDirName, taskID, taskreport.FileName)
	body, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("report not at the harness-exported path %s: %v", wantPath, err)
	}
	if !strings.Contains(string(body), "the fix is on the branch") {
		t.Fatalf("the model's authored report did not reach the seal:\n%s", body)
	}
	showOut, stderr, code := runGopiMapped(t, []string{"task", "show", taskID}, "", env, work)
	if code != 0 {
		t.Fatalf("task show: code=%d stderr=%s", code, stderr)
	}
	return workerStderr, showOut
}

// TestWorkerRunRecordsAFilteredPackageReadingE2E is the RED arm on the REAL
// close-out: the run completes (nothing is refused), the wrapper's stderr names
// the finding with its remedy, the ledger digest records it, and `task show`
// renders it beside the authored report.
func TestWorkerRunRecordsAFilteredPackageReadingE2E(t *testing.T) {
	workerStderr, show := filteredSeamRun(t, "01a11500-0000-7000-8000-000000000031", filteredSeamRedReport)

	if !strings.Contains(workerStderr, taskreport.FilteredClaimWarningLabel) {
		t.Errorf("the close-out must print the filtered-package finding on stderr, got:\n%s", workerStderr)
	}
	if !strings.Contains(workerStderr, taskreport.FilteredClaimRemedy) {
		t.Errorf("the finding must carry the remedy a reader can act on, got:\n%s", workerStderr)
	}
	if !strings.Contains(show, tools.TaskShowReportFilteredLabel) {
		t.Errorf("`task show` must render the recorded finding under %q:\n%s", tools.TaskShowReportFilteredLabel, show)
	}
	if !strings.Contains(show, taskreport.FilteredClaimDigestPrefix) {
		t.Errorf("the row's recorded digest must carry the finding:\n%s", show)
	}
	// The report body itself is still rendered: the finding rides BESIDE the
	// authored text (the record-not-rewrite invariant).
	if !strings.Contains(show, "## Completed") || !strings.Contains(show, "the fix is on the branch") {
		t.Errorf("the authored report must still render:\n%s", show)
	}
}

// TestWorkerRunIsSilentOnAWholePackageReadingE2E is the GREEN arm on the same
// real path: a report whose package lines come from whole-package runs draws no
// finding — on stderr, in the digest, or in `task show`.
func TestWorkerRunIsSilentOnAWholePackageReadingE2E(t *testing.T) {
	workerStderr, show := filteredSeamRun(t, "01a11500-0000-7000-8000-000000000032", filteredSeamWholeReport)

	if strings.Contains(workerStderr, taskreport.FilteredClaimWarningLabel) {
		t.Errorf("a whole-package reading must draw no finding, got:\n%s", workerStderr)
	}
	if strings.Contains(show, tools.TaskShowReportFilteredLabel) {
		t.Errorf("`task show` must render no finding:\n%s", show)
	}
	if strings.Contains(show, taskreport.FilteredClaimDigestPrefix) {
		t.Errorf("the digest must record no filtered reading:\n%s", show)
	}
}
