package main

// cli_tasks_report_destination_test.go — the EXPORT DESTINATION proof
// (Rule 31: a surface is delivered when it is EXECUTED, not when it compiles).
//
// THE DEFECT THIS PINS (epic report-export-destination, 2026-10-28): the
// task-report seam exports an ABSOLUTE path to the executor child before
// spawn, and the child's contract — stated in the prompt and obeyed by every
// run that writes its own report — is `cat > "$GOPI_TASK_REPORT_PATH"`. The
// export resolved the path but never made its PARENT DIRECTORY a
// precondition, so a row whose session container did not exist on disk (never
// created by submit, pruned since, or a container the run itself removed) handed
// the child a destination that `cat >` cannot open: ENOENT, and the run's only
// report goes nowhere while the harness's own seal writes a skeleton somewhere
// else at close-out.
//
// WHAT THIS TEST DRIVES: the real `gopi worker <id>` wrapper (the ONE spawn
// seam, cmd/agent/cli_tasks.go), a REAL run with its session container DELETED
// first, and a child whose FIRST action is the contract's own write. The
// verdict is the filesystem: either the child's write landed inside the
// exported destination's directory (which therefore existed and was writable),
// or it did not.
//
// It asserts NOTHING about which process created the directory — only that an
// exported destination is one a run can write to. That is the criterion the
// epic states: "an exported path must be writable, else it must not enter the
// environment."

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/taskreport"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// reportDestinationProbeEnv carries the probe's verdict out of the child (the
// child's own stdout is the model transport, not a channel a test can read).
const reportDestinationProbeEnv = "GOPI_REPORT_DESTINATION_PROBE"

// The probe's two verdicts — the ONLY spellings the assertions below accept
// (Rule 1: the producer and the reader share one constant).
const (
	reportDestinationProbeWrote = "WRITE-OK"
	reportDestinationProbeFail  = "WRITE-FAIL"
)

// reportDestinationProbeCommand is the child's first tool call: it performs the
// report CONTRACT's own write into the exported destination, and records
// whether that succeeded. Deliberately NOT a `test -d`: existence without
// writability is the state this seam could plausibly leave behind (a directory
// owned by another user, a read-only mount), and the criterion is writability.
func reportDestinationProbeCommand() string {
	return `D="$(dirname "$` + taskreport.PathEnv + `")"
if printf 'probe\n' > "$D/.seam-write-probe"; then printf '` + reportDestinationProbeWrote + `\n' > "$` + reportDestinationProbeEnv + `"
else printf '` + reportDestinationProbeFail + `\n' > "$` + reportDestinationProbeEnv + `"
fi`
}

// TestExportedReportDestinationIsWritable is the regression arm: the wrapper
// exports a destination whose DIRECTORY EXISTS AND IS WRITABLE, even when the
// row's session container is absent at spawn time.
//
// RED on the pre-fix code: the export resolved the path and spawned the child
// without creating anything, so the child's `>` hit ENOENT and the probe file
// read `WRITE-FAIL` (the exact shape every run obeying the contract meets).
//
// GREEN after the fix: the export's ONE resolution materialises the parent (and
// proves it writable) BEFORE the pair enters the child's environment.
func TestExportedReportDestinationIsWritable(t *testing.T) {
	reportSeamBinary(t)
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	work := t.TempDir()
	repo := reportSeamFixtureRepo(t)

	const taskID = "01a11500-0000-7000-8000-0000000000e1"
	probe := filepath.Join(t.TempDir(), "seam-probe.txt")

	reportBody := "# Task Report: destination e2e\n\n## Completed\n- wrote the contract's own destination\n\n" +
		"## Files\n- none (seam proof)\n\n## Verification\n- the export destination was writable\n\n" +
		"## Not Done\n- (nothing)\n"
	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_probe_destination",
			Name: "bash",
			Args: map[string]interface{}{"command": reportDestinationProbeCommand()},
		}},
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_write_report",
			Name: "bash",
			Args: map[string]interface{}{
				"command": "cat > \"$" + taskreport.PathEnv + "\" <<'REPORT_EOF'\n" + reportBody + "REPORT_EOF\n",
			},
		}},
		testutil.Resp{Text: "[TASK_LIST_BEGIN]\n[]\n[TASK_LIST_END]\n" +
			"The destination probe and the report write are both finished."},
	)
	defer mock.Close()

	env := append(testutil.MockEnv(t, gopiHome, mock.URL()),
		"GOPI_MEMORY_DB="+dbPath,
		reportDestinationProbeEnv+"="+probe,
	)

	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"prove the exported report destination is writable",
		"--payload", "the seam must hand the child a destination it can write",
		"--task-id", taskID, "--repo", repo}, "", env, work); code != 0 {
		t.Fatalf("task submit: code=%d stderr=%s", code, stderr)
	}

	// DELETE the row's session container: the state this epic is about. It is
	// the state a pruned container, a legacy row and a run that cleaned up its
	// own directory all reach — and the state the export must survive.
	sessionDir := filepath.Join(gopiHome, ".gopi", config.SessionsDirName, taskID)
	if err := os.RemoveAll(sessionDir); err != nil {
		t.Fatalf("remove the row's session container %s: %v", sessionDir, err)
	}
	if fi, err := os.Stat(sessionDir); err == nil {
		t.Fatalf("fixture precondition: %s still exists (mode %v) — the run below would not observe the defect", sessionDir, fi.Mode())
	}

	if _, stderr, code := runGopiMapped(t, []string{"worker", taskID}, "", env, work); code != 0 {
		t.Fatalf("worker %s: code=%d stderr=%s", taskID, code, stderr)
	}

	body, err := os.ReadFile(probe)
	if err != nil {
		t.Fatalf("the child left no verdict at %s — it never reached the probe (err %v)", probe, err)
	}
	if got := strings.TrimSpace(string(body)); got != reportDestinationProbeWrote {
		t.Fatalf("the child's write into $(dirname $%s) reported %q, want %q — the exported destination %s did not exist (or was not writable) when the child received it. A run obeying the contract `cat > \"$%s\"` gets ENOENT here.",
			taskreport.PathEnv, got, reportDestinationProbeWrote, filepath.Join(sessionDir, taskreport.FileName), taskreport.PathEnv)
	}
	// The probe file is evidence, not a deliverable: the seam's own write must
	// have landed beside it in the same (now existing) directory.
	if _, err := os.Stat(filepath.Join(sessionDir, ".seam-write-probe")); err != nil {
		t.Errorf("the probe's own file is missing from %s: %v", sessionDir, err)
	}
	// And the report the model wrote through the same variable is there.
	if _, err := os.Stat(filepath.Join(sessionDir, taskreport.FileName)); err != nil {
		t.Errorf("report not at the exported path %s: %v", filepath.Join(sessionDir, taskreport.FileName), err)
	}
}
