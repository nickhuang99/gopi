package main

// task_run_stage_record_test.go — THE STAGE BOUNDARIES the spawn seam writes.
//
// WHY THIS TEST EXISTS (row 01a1a60e-b419, 2026-11-04): a worker claimed a row
// at 20:17:02, its run log stopped at 20:17:11, the process was gone, and the
// row then read in_progress for 8h38m. `last_toolcall_at` could not answer
// "how long did it live?" because it is a CLAIM stamp (armed once, then on a
// ticker), so a run that died in 8 seconds and one that worked 8 hours left the
// same field. The seam now records WHERE it got to, and each line carries the
// instant it was written — the last line present is a fact about the process.
//
// THE ARMS, and why each is separate:
//
//	① success — spawning → spawned → reaped, in that order, each stamped;
//	② spawn FAILURE — spawning → spawn FAILED, and NO "child spawned" line.
//	  The absence of the spawned line must be unambiguous: without arm ② it
//	  would be indistinguishable from a truncated log (Rule 15).
//
// The child in arm ① is a REAL process (/bin/sh) started by the kernel, not a
// stub — the stage line reports cmd.Process.Pid, and a stub could not produce
// one. Arm ② is likewise the kernel's own answer: exec is handed an argv that
// does not exist, so Start fails for the real reason.

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// stageLines returns the recorded stage lines, in the order they were written.
func stageLines(record string) []string {
	var out []string
	for _, line := range strings.Split(record, "\n") {
		if strings.Contains(line, "run stage:") {
			out = append(out, line)
		}
	}
	return out
}

// stageStampOf extracts the RFC3339 instant FROM a stage line — the last field
// that parses as one. It is deliberately a parse rather than a column index:
// the four lines have different shapes, and the assertion this feeds ("every
// boundary carries an instant") must fail if the stamp is dropped from ANY of
// them rather than only from the one the test happens to slice.
func stageStampOf(line string) string {
	fields := strings.Fields(line)
	for i := len(fields) - 1; i >= 0; i-- {
		cand := strings.TrimRight(fields[i], "),;")
		cand = strings.TrimPrefix(cand, "at=")
		if _, err := time.Parse(taskRunStageTimeLayout, cand); err == nil {
			return cand
		}
	}
	return ""
}

// TestRunStageRecordCoversSpawnAndReap is arm ①: a real child, started and
// waited on through the seam's own two helpers, leaves three stamped lines in
// the run's record — and the reaped line names the child's outcome.
func TestRunStageRecordCoversSpawnAndReap(t *testing.T) {
	const row = "t-stage-success"

	var record bytes.Buffer
	started := time.Now()

	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := taskRunSpawnChild(cmd, &record, row, "", started); err != nil {
		t.Fatalf("a real child must start: %v", err)
	}
	waitErr := cmd.Wait()
	taskRunRecordReaped(&record, row, started, waitErr)

	lines := stageLines(record.String())
	if len(lines) != 3 {
		t.Fatalf("a child that started and was reaped must leave THREE stage lines (spawning / spawned / reaped), got %d:\n%s",
			len(lines), record.String())
	}
	// ORDER is the load-bearing half: a record that names the pid before the
	// spawn attempt would let a reader believe a child existed when none did.
	for i, want := range []string{"spawning child", "child spawned pid=", "child reaped err="} {
		if !strings.Contains(lines[i], want) {
			t.Errorf("stage line %d must be the %q boundary, got: %s", i, want, lines[i])
		}
	}
	// The pid in the spawned line is the kernel's, not a placeholder: it is
	// the number the seam read back from the process it created.
	if !strings.Contains(lines[1], "pid=") || strings.Contains(lines[1], "pid=0 ") {
		t.Errorf("the spawned line must carry the real child pid, got: %s", lines[1])
	}
	// The reaped line must carry the ELAPSED lifetime — that is the whole
	// point: today nobody can answer "8 seconds or 8 hours".
	if !strings.Contains(lines[2], "lived ") {
		t.Errorf("the reaped line must carry the time the child lived, got: %s", lines[2])
	}
	// Every line is stamped, so a reader diffs it against the ledger without
	// conversion (last_toolcall_at is a claim; this is a clock reading).
	for i, line := range lines {
		if _, perr := time.Parse(taskRunStageTimeLayout, stageStampOf(line)); perr != nil {
			t.Errorf("stage line %d carries no RFC3339 stamp (%v) — a boundary without an instant cannot "+
				"answer how long the run lived, which is the whole defect: %s", i, perr, line)
		}
	}
}

// TestRunStageRecordOnSpawnFailure has no "spawned" line is arm ②: when exec
// cannot start the child, the record ends at an EXPLICIT failure — spawning
// then "spawn FAILED" — and the absence of a spawned line is a statement the
// seam made, not a gap a reader has to guess about.
func TestRunStageRecordOnSpawnFailure(t *testing.T) {
	const row = "t-stage-failed"
	// A path no exec can run: the kernel answers ENOENT and Start returns it.
	const missing = "/nonexistent-gopi-stage-probe/child"

	var record bytes.Buffer
	started := time.Now()

	cmd := exec.Command(missing)
	err := taskRunSpawnChild(cmd, &record, row, "", started)
	if err == nil {
		t.Fatalf("starting %q must fail (this arm exists BECAUSE Start can fail): the seam recorded:\n%s",
			missing, record.String())
	}

	lines := stageLines(record.String())
	if len(lines) != 2 {
		t.Fatalf("a spawn that failed must leave TWO stage lines (spawning / spawn FAILED), got %d:\n%s",
			len(lines), record.String())
	}
	if !strings.Contains(lines[0], "spawning child") {
		t.Errorf("the first line must be the spawning boundary, got: %s", lines[0])
	}
	if !strings.Contains(lines[1], "child spawn FAILED") {
		t.Errorf("the second line must state the failure, got: %s", lines[1])
	}
	// THE RED CONDITION THIS ARM PINS: no "child spawned" line. Written as a
	// positive statement in the record — "no child process ever existed" — so
	// the reader is told, rather than left to infer from silence.
	if strings.Contains(record.String(), "child spawned pid=") {
		t.Errorf("a failed spawn must NOT claim a child existed:\n%s", record.String())
	}
	if !strings.Contains(record.String(), "no child process ever existed") {
		t.Errorf("the failure line must say the absence out loud (Rule 15), got:\n%s", record.String())
	}
	for i, line := range lines {
		if _, perr := time.Parse(taskRunStageTimeLayout, stageStampOf(line)); perr != nil {
			t.Errorf("stage line %d carries no RFC3339 stamp (%v) — the failure's own instant is what "+
				"separates 'died at exec' from 'died in the child': %s", i, perr, line)
		}
	}
}
