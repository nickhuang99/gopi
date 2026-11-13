package main

// cli_tasks_delegated_seam_test.go — the result-delegation record, driven E2E
// through the REAL `gopi worker` wrapper (writebackOutcome → tools.SealTaskReport),
// not merely through the seal function.
//
// WHY THIS FILE EXISTS: the unit tests in internal/tools prove what the seal does
// when it IS called. This one proves the close-out path calls it and that the
// operator MEETS the finding — the two halves a unit test cannot cover (Rule 31's
// own standard, turned on this change).
//
// The motivating shape is row 01a12d6b's report (2026-10-12): `## Verification`
// hands the acceptance conclusion to a path ("→ see the stream at
// `artifacts/make-testbudget-testutil.txt`") and states no result for it — and
// that run's acceptance stream afterwards exited 2 with two red tests. The report
// is authored BEFORE the acceptance command runs (the done-gate chain is build →
// tests → report → commit), so the delegation is the only place its conclusion
// ever lived.

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

// delegatedSeamReport is the red arm: the motivating sentence, and no result for
// it anywhere.
const delegatedSeamReport = `# Task Report: capture an identity once the child's argv is published

## Completed
- the fix is on the branch

## Files
- internal/testutil/proc_identity.go

## Verification
- **Build**: ` + "`go build ./...`" + ` → OK (exit 0).
- **Tests**: ` + "`make test-budget PKGS='./internal/testutil'`" + ` → see the stream at
  ` + "`artifacts/make-testbudget-testutil.txt`" + `.
- **Ran**: ` + "`go test -count 1 ./internal/testutil`" + `

## Not Done
- the pre-existing red pair, proven at my own base commit
`

// delegatedSeamStatedReport is the green arm: the SAME report with the
// conclusion stated beside the path — a reviewer can check the sentence against
// the artifact, which is all this record asks for.
const delegatedSeamStatedReport = `# Task Report: capture an identity once the child's argv is published

## Completed
- the fix is on the branch

## Files
- internal/testutil/proc_identity.go

## Verification
- **Build**: ` + "`go build ./...`" + ` → OK (exit 0).
- **Tests**: ` + "`make test-budget PKGS='./internal/testutil'`" + ` → see the stream at
  ` + "`artifacts/make-testbudget-testutil.txt`" + ` (exit=2, 2 red).
- **Ran**: ` + "`go test -count 1 ./internal/testutil`" + `

## Not Done
- the pre-existing red pair, proven at my own base commit
`

// delegatedSeamRun submits a row and executes it with a mock model whose ONE
// tool call writes reportBody to the harness-exported path, returning the
// wrapper's stderr and the `task show` output (the rancheck seam test's shape,
// reused so the two records are driven by ONE harness pattern).
func delegatedSeamRun(t *testing.T, taskID, reportBody string) (workerStderr, show string) {
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
				"command": "cat > \"$" + taskreport.PathEnv + "\" <<'DELEGATED_EOF'\n" + reportBody + "DELEGATED_EOF\n",
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
	if !strings.Contains(string(body), "see the stream at") {
		t.Fatalf("the model's authored report did not reach the seal:\n%s", body)
	}
	showOut, stderr, code := runGopiMapped(t, []string{"task", "show", taskID}, "", env, work)
	if code != 0 {
		t.Fatalf("task show: code=%d stderr=%s", code, stderr)
	}
	return workerStderr, showOut
}

// TestWorkerRunRecordsADelegatedGateResultE2E is the RED arm on the REAL
// close-out: the run completes (nothing is refused), the wrapper's stderr names
// the finding, the ledger digest records it with the path's seal-time status,
// and `task show` renders it beside the authored report.
func TestWorkerRunRecordsADelegatedGateResultE2E(t *testing.T) {
	workerStderr, show := delegatedSeamRun(t, "01a11500-0000-7000-8000-000000000021", delegatedSeamReport)

	if !strings.Contains(workerStderr, taskreport.DelegatedWarningLabel) {
		t.Errorf("the close-out must print the delegation finding on stderr, got:\n%s", workerStderr)
	}
	if !strings.Contains(workerStderr, "see the stream at") {
		t.Errorf("the finding must quote the author's own sentence, got:\n%s", workerStderr)
	}
	if !strings.Contains(show, tools.TaskShowReportDelegatedLabel) {
		t.Errorf("`task show` must render the recorded finding under %q:\n%s", tools.TaskShowReportDelegatedLabel, show)
	}
	if !strings.Contains(show, taskreport.DelegatedDigestPrefix) {
		t.Errorf("the row's recorded digest must carry the finding:\n%s", show)
	}
	// The report body itself is still rendered: the finding rides BESIDE the
	// authored text (the record-not-rewrite invariant).
	if !strings.Contains(show, "## Completed") || !strings.Contains(show, "the fix is on the branch") {
		t.Errorf("the authored report must still render:\n%s", show)
	}
}

// TestWorkerRunIsSilentWhenTheResultIsStatedE2E is the GREEN arm on the same
// real path: the report that states its conclusion draws no finding — on stderr,
// in the digest, or in `task show`. A check that fired here would be noise on
// every honest run, which is the failure mode this whole family refuses.
func TestWorkerRunIsSilentWhenTheResultIsStatedE2E(t *testing.T) {
	workerStderr, show := delegatedSeamRun(t, "01a11500-0000-7000-8000-000000000022", delegatedSeamStatedReport)

	if strings.Contains(workerStderr, taskreport.DelegatedWarningLabel) {
		t.Errorf("a report stating its result must draw no finding, got:\n%s", workerStderr)
	}
	if strings.Contains(show, tools.TaskShowReportDelegatedLabel) {
		t.Errorf("`task show` must render no finding:\n%s", show)
	}
	if strings.Contains(show, taskreport.DelegatedDigestPrefix) {
		t.Errorf("the digest must record no delegation:\n%s", show)
	}
}
