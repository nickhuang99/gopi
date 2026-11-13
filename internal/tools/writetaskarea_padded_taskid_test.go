package tools

// Verbatim from archive/feat/taskid-whitespace-normalize
// (internal/tools/writetaskarea_test.go, added by commit 13677824), split out
// so the salvage adds no edit to the tag's own files.
//
// TestWriteTaskArea_PaddedTaskIDKeysTheSameRow: the working-area row is
// keyed by the task_id VERBATIM — before the seam, a padded id keyed a
// second row for the SAME task (a split working area for one task).

import (
	"strings"
	"testing"
)

func TestWriteTaskArea_PaddedTaskIDKeysTheSameRow(t *testing.T) {
	tempMemoryDB(t)
	if _, err := WriteTaskArea(WriteTaskAreaOptions{
		SessionID: "sess-pad", TaskID: taskIDWSResidualPad, Status: "completed", Conclusion: "padded write",
	}); err != nil {
		t.Fatalf("WriteTaskArea(padded): %v", err)
	}
	got, err := SessionConclusions("sess-pad")
	if err != nil {
		t.Fatalf("SessionConclusions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d conclusions, want 1", len(got))
	}
	if got[0].TaskID != taskIDWSResidualSeed {
		t.Errorf("stored task_id = %q, want the trimmed %q", got[0].TaskID, taskIDWSResidualSeed)
	}
	// A whitespace-only id is still the empty-id refusal (trim → empty).
	if _, err := WriteTaskArea(WriteTaskAreaOptions{
		SessionID: "sess-pad", TaskID: " \n ", Status: "completed", Conclusion: "blank id",
	}); err == nil || !strings.Contains(err.Error(), "task_id must not be empty") {
		t.Fatalf("blank task_id = %v, want the empty-task_id refusal", err)
	}
}
