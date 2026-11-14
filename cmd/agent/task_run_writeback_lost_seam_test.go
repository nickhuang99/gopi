package main

// task_run_writeback_lost_seam_test.go — THE END-TO-END of the shared
// failed-branch writeback seam: a refused terminal write must leave its reason
// on BOTH streams the run's reader may be looking at.
//
// WHY BOTH. Two measured defects, one rule:
//
//	① the run log. `gopi task run`/a worker round writes the row's terminal
//	   status; a refusal used to be dropped, so the record an evidence reader
//	   opens afterwards said nothing (the measured row 01a16f7c: the escalation
//	   sentence existed on stderr and `grep -c` in that run's output.log found
//	   0 — cli_tasks_exec_output.go's execRunFacts exists exactly for this);
//	② stderr. For a DETACHED worker the run log's path is not known yet to the
//	   reader watching the daemon, so the spawning stream must carry it too —
//	   the same two-destination shape the ground refusals above use.
//
// The refusal is the REAL one: a `cancelled` row is terminal (no status write
// leaves it — taskAllowedTransitions), so TaskRunWriteback returns the ledger's
// own error and the seam has something true to report.
//
// This drives the PRODUCTION door (writebackFailure), never the arm directly: a
// call to tools.TaskWritebackLost would prove the recorder works, not that the
// wrapper calls it.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tools"
)

// TestWritebackFailurePutsARefusedWriteOnBothStreams is the repro.
//
// RED ON THE PRE-FIX TREE: the five failure arms called writebackOutcome as an
// expression statement, so a refused `failed` write produced NO line on either
// stream and no record on the row — the exit status was the only account.
func TestWritebackFailurePutsARefusedWriteOnBothStreams(t *testing.T) {
	pinEmptyLedger(t)

	const row = "t-writeback-lost-seam"
	if _, err := tools.TaskNew("writeback-lost seam fixture", row, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	if _, err := tools.TaskUpdateReason(row, tools.TaskStatusCancelled, "fixture: terminal row", ""); err != nil {
		t.Fatalf("seed a terminal row: %v", err)
	}

	prevFacts := execRunFacts
	var record bytes.Buffer
	execRunFacts = &record
	t.Cleanup(func() { execRunFacts = prevFacts })

	streamed := captureStderrStream(t, func() {
		writebackFailure(row, tools.TaskStatusFailed, "the run failed", "", "")
	})

	if record.Len() == 0 {
		t.Fatalf("a refused `failed` writeback left NOTHING in the run's own record — the defect this seam " +
			"closes: the row stays open and the only account of it was the exit status of a process that " +
			"has already exited")
	}
	if streamed == "" {
		t.Fatal("the spawning stream said nothing either — a detached worker's reader does not know the run " +
			"log's path yet, so stderr is the copy that reaches them")
	}

	// BOTH streams carry the SAME sentence, and it is the seam's (not a
	// re-derived guess): the row, the status, and the ledger's own words.
	lineOf := func(s string) string {
		for _, line := range strings.Split(s, "\n") {
			if strings.Contains(line, row) && strings.Contains(line, "DID NOT LAND") {
				return line
			}
		}
		return ""
	}
	fromRecord, fromStream := lineOf(record.String()), lineOf(streamed)
	if fromRecord == "" {
		t.Fatalf("the run log does not carry the writeback verdict for %s; record contents:\n%s", row, record.String())
	}
	if fromStream == "" {
		t.Fatalf("stderr does not carry the writeback verdict for %s; stderr contents:\n%s", row, streamed)
	}
	if fromRecord != fromStream {
		t.Fatalf("the two destinations disagree, so one reader learns a different fact than the other:\n"+
			"  run log: %s\n  stderr:  %s", fromRecord, fromStream)
	}
	if !strings.Contains(fromRecord, tools.WritebackLostEvent) {
		t.Errorf("the sentence must name the event %q so a reader can find it again on the row's trail; got:\n%s",
			tools.WritebackLostEvent, fromRecord)
	}

	// AND THE ROW KEEPS ITS OWN VERDICT: reporting must never dispose the row
	// (the retired zombie sweep's job — devdoc/zombie-sweep-removed.md).
	if st, err := tools.TaskStatusOf(row); err != nil || st != tools.TaskStatusCancelled {
		t.Fatalf("status = %q, err = %v — a report must not move the row", st, err)
	}
}

// TestWritebackFailureSaysNothingWhenTheWriteLands is the control: the seam is
// a REPORTING wrapper, not an escalation. An ordinary failed writeback lands,
// so nothing is printed and no loss event is invented — a seam that announced a
// loss on every failure arm would be noise on the operator's alarm channel.
func TestWritebackFailureSaysNothingWhenTheWriteLands(t *testing.T) {
	pinEmptyLedger(t)

	const row = "t-writeback-lost-clean"
	if _, err := tools.TaskNew("clean failed writeback fixture", row, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	if _, err := tools.TaskUpdateReason(row, tools.TaskStatusInProgress, "", ""); err != nil {
		t.Fatalf("move the row to in_progress: %v", err)
	}

	prevFacts := execRunFacts
	var record bytes.Buffer
	execRunFacts = &record
	t.Cleanup(func() { execRunFacts = prevFacts })

	streamed := captureStderrStream(t, func() {
		writebackFailure(row, tools.TaskStatusFailed, "the run failed", "", "")
	})

	if strings.Contains(record.String(), "DID NOT LAND") {
		t.Fatalf("the seam reported a loss for a writeback that LANDED, so every failure arm would fire the "+
			"alarm: the reporting wrapper must be silent on success. Record:\n%s", record.String())
	}
	if strings.Contains(streamed, "DID NOT LAND") {
		t.Fatalf("stderr carried a loss sentence for a landed writeback: %q", streamed)
	}
	// The write really did land — the control's own premise, asserted rather
	// than assumed.
	if st, err := tools.TaskStatusOf(row); err != nil || st != tools.TaskStatusFailed {
		t.Fatalf("status = %q, err = %v — the control's writeback did not land, so the silence proves nothing",
			st, err)
	}
}

// pinEmptyLedger points the ledger at a fresh per-test database.
//
// The pin is the PACKAGE'S OWN established shape (cli_tasks_truncated_writeback_test.go,
// cli_tasks_worker_tag_flow_test.go): a file under t.TempDir() exported as
// GOPI_MEMORY_DB, so nothing here can resolve the operator's live ledger (Rule
// 32's substance) and the directory's removal is the harness's.
func pinEmptyLedger(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)
	if dir := filepath.Dir(path); dir == "" {
		t.Fatalf("the pinned ledger path has no directory: %q", path)
	}
}

// TestWritebackFailureDoesNotDoublePrintOnTheSpawnersStream pins the dedup: the
// spawn seam's DEFAULT is execRunFacts == os.Stderr (cli_tasks_exec_output.go),
// and printing both faces unconditionally put the sentence on the operator's
// stream twice — measured 2026-10-29 in a live refusal proof:
//
//	task …: the failed writeback DID NOT LAND — …
//	task …: the failed writeback DID NOT LAND — …
//
// One fact, one line per destination that is actually separate.
func TestWritebackFailureDoesNotDoublePrintOnTheSpawnersStream(t *testing.T) {
	pinEmptyLedger(t)

	const row = "t-writeback-lost-dedup"
	if _, err := tools.TaskNew("dedup fixture", row, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	if _, err := tools.TaskUpdateReason(row, tools.TaskStatusCancelled, "fixture: terminal row", ""); err != nil {
		t.Fatalf("seed a terminal row: %v", err)
	}

	// THE SAME-SINK STATE MUST BE ESTABLISHED INSIDE THE CAPTURE, not before it:
	// captureStderrStream swaps os.Stderr to a pipe, so a latch captured earlier
	// would point at the PRE-SWAP file and the two writes would land on
	// different destinations — the very condition this test must avoid. Measured
	// 2026-10-29: the first version of this test passed against the un-deduped
	// seam for exactly that reason (a green that measured nothing).
	prevFacts := execRunFacts
	t.Cleanup(func() { execRunFacts = prevFacts })

	streamed := captureStderrStream(t, func() {
		// The latch's own default, not a stand-in: execRunFacts starts as
		// os.Stderr and only the spawn seam replaces it.
		execRunFacts = os.Stderr
		writebackFailure(row, tools.TaskStatusFailed, "the run failed", "", "")
		execRunFacts = prevFacts
	})

	count := 0
	for _, line := range strings.Split(streamed, "\n") {
		if strings.Contains(line, row) && strings.Contains(line, "DID NOT LAND") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("the operator's stream carried the sentence %d time(s), want exactly 1 — the run's facts writer "+
			"IS stderr here, so the second write would be the same destination. Stream:\n%s", count, streamed)
	}
}
