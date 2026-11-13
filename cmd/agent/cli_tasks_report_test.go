package main

// cli_tasks_report_test.go — the `task show` FACE of the bounded task report
// (epic task-report). The unit tests in internal/tools prove the writer/reader
// seam; this file proves the operator-visible surface: the report lands in the
// output `gopi task show` already prints, and a row without one degrades
// honestly. Both drive the REAL tools.TaskShow renderer (the same function the
// `show` verb calls) — no model, no child process.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/taskreport"
	"github.com/nick/gopi/internal/tools"
)

// TestTaskShowRendersTaskReport is the acceptance check for the epic's second
// deliverable: `task show <id>` surfaces the report's content (plus its path)
// in the same output the operator already reads.
func TestTaskShowRendersTaskReport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, "memory.db"))

	const taskID = "t-show-report"
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "report render check",
		TaskID:  taskID,
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	path, err := tools.TaskReportPath(taskID)
	if err != nil {
		t.Fatalf("TaskReportPath: %v", err)
	}
	body := "# Task Report: render me\n\n## Completed\n- the seam\n\n" +
		"## Files\n- internal/tools/task_report.go\n\n" +
		"## Verification\n- go test ./internal/tools: ok\n\n## Not Done\n- (nothing)\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("seed report: %v", err)
	}

	show, err := tools.TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	for _, want := range []string{tools.TaskShowReportLabel, path, "## Completed", "- the seam", "## Not Done"} {
		if !strings.Contains(show, want) {
			t.Errorf("task show missing %q:\n%s", want, show)
		}
	}
}

// TestTaskShowDegradesHonestlyWithoutReport: a row whose report was never
// written (or was pruned) renders an explicit line — the operator can tell
// "no report" from "report says the task did nothing" (epic acceptance:
// historical tasks degrade honestly rather than pretending).
func TestTaskShowDegradesHonestlyWithoutReport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, "memory.db"))

	const taskID = "t-show-noreport"
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "legacy row",
		TaskID:  taskID,
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	show, err := tools.TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, tools.TaskShowReportLabel) {
		t.Fatalf("task show must carry an explicit report line:\n%s", show)
	}
	// A row created NOW is past the seam's cutoff, so the honest line is the
	// plain gap note (a pre-seam row would carry the legacy note — see the
	// unit test for both branches).
	if !strings.Contains(show, taskreport.NoneNote) {
		t.Errorf("post-seam row with no report must render %q:\n%s", taskreport.NoneNote, show)
	}
}
