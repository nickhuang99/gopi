package tools

// task_run_retry_test.go — the LEDGER half of the wrapped-executor-child retry
// (epic 4/5): the append-only attempt counter and the two questions the
// wrapper asks before it resumes a row.

import (
	"strings"
	"testing"
)

// TestTaskRunRetryCountsReadsTheHighWaterMark: the cap is enforced on the
// HIGHEST ordinal the trail records, never on how many tags happen to be there
// — a duplicated or hand-edited trail must not buy an extra retry (the reader
// is conservative by construction).
func TestTaskRunRetryCountsReadsTheHighWaterMark(t *testing.T) {
	cases := []struct {
		name string
		tags string
		want int
	}{
		{"empty", "", 0},
		{"unrelated tags only", "bug,auto,dead-holder-resume:1", 0},
		{"one attempt", "run-retry:1", 1},
		{"three attempts", "bug,run-retry:1,run-retry:2,run-retry:3", 3},
		{"duplicate ordinal (crash between write and next attempt)", "run-retry:1,run-retry:1", 1},
		{"gap tolerance: the maximum wins", "run-retry:1,run-retry:7", 7},
		{"unparsable ordinal is not an attempt", "run-retry:x,run-retry:2", 2},
		{"whitespace is trimmed", " run-retry:1 , run-retry:2 ", 2},
		// The two counter namespaces must not read each other's trail.
		{"the dead-holder counter is a different ladder", "dead-holder-resume:5", 0},
	}
	for _, tc := range cases {
		if got := taskRunRetryCountsFromTags(tc.tags); got != tc.want {
			t.Errorf("%s: counts(%q) = %d, want %d", tc.name, tc.tags, got, tc.want)
		}
	}
}

// TestTaskRunRetryNoteAppendsAnOrdinal is the writer half, against a real
// ledger: each note extends the trail by ONE ordinal, so a reader (a later
// pass, or a restarted wrapper) sees the same count the writer meant.
func TestTaskRunRetryNoteAppendsAnOrdinal(t *testing.T) {
	dbPath := t.TempDir() + "/memory.db"
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	if _, err := TaskSubmit(TaskSubmitOptions{
		TaskID: "t-retry-note", Summary: "note append", Payload: "payload", Source: "human",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	for want := 1; want <= TaskRunRetryCap; want++ {
		if err := TaskRunRetryNote("t-retry-note"); err != nil {
			t.Fatalf("note %d: %v", want, err)
		}
		if got, err := TaskRunRetryCounts("t-retry-note"); err != nil {
			t.Fatalf("counts after note %d: %v", want, err)
		} else if got != want {
			t.Fatalf("after %d note(s) the trail reads %d", want, got)
		}
	}
	tags, err := TaskTags("t-retry-note")
	if err != nil {
		t.Fatalf("tags: %v", err)
	}
	if !strings.Contains(tags, taskRunRetryTagPrefix+"1") {
		t.Errorf("the trail must carry the ordinals themselves (append-only counter), got %q", tags)
	}
}

// TestTaskRunRetryCapReasonNamesTheCap: the handover message an operator reads
// is rendered from the constant that bounded the loop — a reworded message can
// never drift from the enforced cap.
func TestTaskRunRetryCapReasonNamesTheCap(t *testing.T) {
	reason := TaskRunRetryCapReason()
	if !strings.Contains(reason, "3") {
		t.Errorf("the handover reason must name the cap (%d): %q", TaskRunRetryCap, reason)
	}
	if !strings.Contains(reason, "abnormal") {
		t.Errorf("the handover reason must say WHY the ladder ended: %q", reason)
	}
}
