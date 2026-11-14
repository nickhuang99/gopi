package main

// cli_tasks_rancheck_seam_test.go — Rule 31's checked face, driven E2E through
// the REAL `gopi worker` wrapper (the writebackOutcome → tools.SealTaskReport
// path), not merely through the seal function.
//
// WHY THIS FILE EXISTS: the unit tests in internal/tools prove the seal's
// behaviour when it IS called. This one proves the close-out path calls it and
// that the operator MEETS the finding — the two halves a unit test cannot
// cover (Rule 31's own standard, turned on this change: "a surface you did not
// execute is not delivered").
//
// The motivating shape is the report that shipped 2026-09-21's broken
// `gopi task sync --push-only --task-id T`: `Build:`/`Tests:` only, nothing
// stating that any surface had been run — and nothing stating a reason it could
// not be.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/taskreport"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// ranSeamPreRule31Report is the pre-Rule-31 report a model writes in the red
// arm: every required section present, no `Ran:` line, and a `## Not Done` that
// claims nothing was skipped — so neither the line nor a stated limit exists.
const ranSeamPreRule31Report = `# Task Report: the manual push face

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

// ranSeamRanReport is the green arm: the same report with Rule 31's line.
const ranSeamRanReport = `# Task Report: the manual push face

## Completed
- implemented --task-id on the manual push verb

## Files
- internal/tools/memsync.go

## Verification
- Build:  go build ./...: OK
- Tests:  go test ./internal/tools: ok
- Ran:    ./bin/gopi task sync --push-only --task-id 01a0c2b1-...
- Result: exit status 1 — claim-remote: want 1..8 row(s), got 2181

## Not Done
- (nothing)
`

// ranSeamRun submits a row and executes it with a mock model whose ONE tool call
// writes reportBody to the harness-exported path, returning the wrapper's
// stderr and the `task show` output.
func ranSeamRun(t *testing.T, taskID, reportBody string) (workerStderr, show string) {
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
				"command": "cat > \"$" + taskreport.PathEnv + "\" <<'RANREPORT_EOF'\n" + reportBody + "RANREPORT_EOF\n",
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
	// The report must have arrived through the REAL hand-off before anything is
	// asserted about the finding (an absent file would make the red arm pass for
	// the wrong reason — the seal's skeleton also records the gap).
	wantPath := filepath.Join(gopiHome, ".gopi", config.SessionsDirName, taskID, taskreport.FileName)
	body, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("report not at the harness-exported path %s: %v", wantPath, err)
	}
	if !strings.Contains(string(body), "the manual push face") {
		t.Fatalf("the model's authored report did not reach the seal:\n%s", body)
	}
	showOut, stderr, code := runGopiMapped(t, []string{"task", "show", taskID}, "", env, work)
	if code != 0 {
		t.Fatalf("task show: code=%d stderr=%s", code, stderr)
	}
	return workerStderr, showOut
}

// TestWorkerRunRecordsTheMissingRanLineE2E is the RED arm of Rule 31's check on
// the REAL close-out: the run completes (nothing is refused), the wrapper's
// stderr names the finding, the ledger digest records it, and `task show`
// renders it beside the authored report.
func TestWorkerRunRecordsTheMissingRanLineE2E(t *testing.T) {
	workerStderr, show := ranSeamRun(t, "01a11500-0000-7000-8000-000000000014", ranSeamPreRule31Report)

	if !strings.Contains(workerStderr, taskreport.RanCheckWarningLabel) {
		t.Errorf("the close-out must print the Rule 31 finding on stderr, got:\n%s", workerStderr)
	}
	if !strings.Contains(workerStderr, taskreport.RanLinePrefix) {
		t.Errorf("the finding must name the line Rule 31 wants, got:\n%s", workerStderr)
	}
	if !strings.Contains(show, tools.TaskShowReportRanLabel) {
		t.Errorf("`task show` must render the recorded finding under %q:\n%s", tools.TaskShowReportRanLabel, show)
	}
	if !strings.Contains(show, taskreport.RanCheckDigestPrefix) {
		t.Errorf("the row's recorded digest must carry the finding:\n%s", show)
	}
	// The report body itself is still rendered: the finding rides BESIDE the
	// authored text (the record-not-rewrite invariant).
	if !strings.Contains(show, "## Completed") || !strings.Contains(show, "the manual push face") {
		t.Errorf("the authored report must still render:\n%s", show)
	}
}

// TestWorkerRunIsSilentWhenTheRanLineIsPresentE2E is the GREEN arm on the same
// real path: the report that carries Rule 31's line draws no finding — on
// stderr, in the digest, or in `task show`. A check that fired here would be
// noise on every compliant run.
func TestWorkerRunIsSilentWhenTheRanLineIsPresentE2E(t *testing.T) {
	workerStderr, show := ranSeamRun(t, "01a11500-0000-7000-8000-000000000015", ranSeamRanReport)

	if strings.Contains(workerStderr, taskreport.RanCheckWarningLabel) {
		t.Errorf("a report carrying a `Ran:` line must draw no finding, got:\n%s", workerStderr)
	}
	if strings.Contains(show, tools.TaskShowReportRanLabel) {
		t.Errorf("`task show` must render no finding:\n%s", show)
	}
	if strings.Contains(show, taskreport.RanCheckDigestPrefix) {
		t.Errorf("the digest must record no gap:\n%s", show)
	}
}
