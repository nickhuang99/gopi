package main

// task_run_closeout_record_test.go — THE CLOSE-OUT VERDICT BELONGS TO THE RUN'S
// OWN RECORD, not only to a stream that dies with the daemon.
//
// THE MEASURED FAILURE THIS PINS (row 01a16f7c, 2026-10-26). A clone-channel
// worker run hit the delivery-proof refusal twice (04:36:54 and 04:37:46, both
// recorded as `delivery-proof-owed` on the row) and the wrapper escalated with
// "task … was NOT closed out". That sentence is the ONLY account of why the run
// did not close — and it went to os.Stderr alone, which for a detached worker is
// a shared stream no task id can reach. The run's own record was therefore
// silent:
//
//	$ grep -c "was NOT closed out" \
//	    ~/.gopi/sessions/01a16f7c-f54e-7143-bbc7-538d4a4f1819/output.log
//	0
//
// while the row read `completed` (written 78 minutes later by a different
// writer). Anyone reading the record afterwards — the next agent, a reviewer,
// an operator triaging "why is this row still open?" — finds no reason at all.
// The runner's own rule for every other close-out line is the one this arm was
// breaking: a fact ABOUT a run is written through execRunFacts, which is the
// run's log PLUS the spawning stderr whenever that log exists
// (cli_tasks_exec_output.go).
//
// The check is on the SINK, which is the whole subject: the same call must land
// on the run's record. Redirecting execRunFacts is not a shortcut around the
// production path — it IS the production path's writer, and the test asserts the
// escalation writes to it.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tools"
)

// TestCloseoutEscalationReachesTheRunsOwnRecord is the regression: the
// escalation line is written through execRunFacts, so a run whose log exists
// carries the verdict that explains it.
func TestCloseoutEscalationReachesTheRunsOwnRecord(t *testing.T) {
	prev := execRunFacts
	var record bytes.Buffer
	execRunFacts = &record
	t.Cleanup(func() { execRunFacts = prev })

	const taskID = "01a16f7c-f54e-7143-bbc7-538d4a4f1819"
	// The measured shape: the writeback was refused, the ledger still holds a
	// non-terminal status (the delivery-proof refusal leaves the row OPEN).
	err := taskRunCloseoutEscalate(taskID, errCloseoutRefused{}, tools.TaskStatusInProgress, nil)
	if err == nil {
		t.Fatal("an escalation must return the error that gives the wrapper its exit code")
	}
	if !strings.Contains(err.Error(), taskID) || !strings.Contains(err.Error(), "NOT closed out") {
		t.Fatalf("the returned error must name the row and its verdict, got: %v", err)
	}
	got := record.String()
	if !strings.Contains(got, taskRunCloseoutEscalationMarker) {
		t.Fatalf("the close-out verdict never reached the run's own record (execRunFacts): the run log would "+
			"carry no reason at all — the measured defect on row %s. Record contents: %q", taskID, got)
	}
	if !strings.Contains(got, taskID) {
		t.Fatalf("the recorded line must name the row, got %q", got)
	}
	// AND IT STILL EXPLAINS ITSELF: the writeback's own words ride the line, so
	// the record answers "why" and not just "no".
	if !strings.Contains(got, "delivery proof refused") {
		t.Fatalf("the recorded line must quote the refusal, got %q", got)
	}
}

// errCloseoutRefused stands in for the delivery-proof refusal the writeback
// returns — a sentinel whose text is asserted above, so a future rewrite cannot
// quietly narrow the line to "the writeback reported an error".
type errCloseoutRefused struct{}

func (errCloseoutRefused) Error() string { return "delivery proof refused: not at its destination" }
