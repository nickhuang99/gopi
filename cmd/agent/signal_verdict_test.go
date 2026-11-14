// F1 — the ledger's unsourced-signal attribution said `task ""` on the line after
// one that knew the task id (measured 2026-09-27, one session log, adjacent lines):
//
//	SENDER UNSOURCED SIGNAL SIGINT TASK 01a0deb7-41d4-778a-ba4b-6c91ce448956 AT 2026-09-27T02:39:54+08:00
//	signal provenance — SIGINT (UNSOURCED — no gopi stop path announced this kill …)
//	the ledger will carry unsourced for task ""
//
// Two producers, one piece of knowledge: the FIRST line renders the worker's own
// claimed-round latch (workerStopTargetTask), and the ledger line was keyed on
// signalrec's ANNOUNCED id, which a resident worker never has. The assertions below
// are about AGREEMENT — the two lines must name the same row — and about what the
// line says when there genuinely is no row, which must not be an empty value.
package main

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/signalrec"
)

// stopVerdictTestTaskID is a ledger-shaped id for the fixture. It is the id from
// the measured session log, so the test names a real observation.
const stopVerdictTestTaskID = "01a0deb7-41d4-778a-ba4b-6c91ce448956"

// clearStopVerdictTaskLatch empties the worker's claimed-round latch and restores
// whatever it held, so this test cannot leak state into a sibling.
func clearStopVerdictTaskLatch(t *testing.T) {
	t.Helper()
	prev := workerStopTargetTask()
	workerStopState.mu.Lock()
	workerStopState.task = ""
	workerStopState.mu.Unlock()
	t.Cleanup(func() {
		workerStopState.mu.Lock()
		workerStopState.task = prev
		workerStopState.mu.Unlock()
	})
}

// clearAnnnouncedTaskID empties every channel the recorder READS the announced id
// from — the declaration var and the two launch-handoff identities — because this
// suite runs under a real handoff (GOPI_TASK_ID/GOPI_TASK_RUN_ID are set for the
// session's own task), and a test of the UNANNOUNCED case must actually be in it.
func clearAnnouncedTaskID(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		signalrec.TaskIDEnv,
		signalrec.TaskRunEnvName,
		signalrec.TaskWrapperEnvName,
	} {
		t.Setenv(name, "")
	}
}

func TestStopVerdictAttributionAgreesWithTheProvenanceLine(t *testing.T) {
	clearAnnouncedTaskID(t)
	clearStopVerdictTaskLatch(t)
	workerRecordStopTask(stopVerdictTestTaskID)

	if got := stopVerdictTaskID(); got != stopVerdictTestTaskID {
		t.Fatalf("stopVerdictTaskID() = %q, want the claimed round %q — the worker's OWN observation must win over an "+
			"announcement it never had", got, stopVerdictTestTaskID)
	}
	// THE FIX, as agreement: the reservation line and the verdict line name one row.
	provenance := workerTerminationProvenance()
	if !strings.Contains(provenance, stopVerdictTestTaskID) {
		t.Fatalf("the termination provenance does not carry the claimed round:\n%s", provenance)
	}
	verdict := stopVerdictLedgerLine(workerRoleLabel, stopVerdictTaskID())
	if !strings.Contains(verdict, stopVerdictTestTaskID) {
		t.Errorf("the ledger verdict does not carry the same task id the line above it printed — this is the measured "+
			"disagreement:\n  %s\n  %s", provenance, verdict)
	}
	if strings.Contains(verdict, `task ""`) {
		t.Errorf("the ledger verdict still prints an empty task id:\n%s", verdict)
	}
}

func TestStopVerdictFallsBackToTheAnnouncedTaskID(t *testing.T) {
	// The case the announcement exists FOR: a process that claims no round has no
	// latch to observe, and the launch handoff is the only evidence there is.
	clearStopVerdictTaskLatch(t)
	clearAnnouncedTaskID(t)
	t.Setenv(signalrec.TaskIDEnv, stopVerdictTestTaskID)

	if got := stopVerdictTaskID(); got != stopVerdictTestTaskID {
		t.Fatalf("stopVerdictTaskID() = %q, want the announced %q — the fallback is what keeps a "+
			"`gopi task run` child attributable", got, stopVerdictTestTaskID)
	}
}

func TestStopVerdictNamesTheGapWhenThereIsNoRow(t *testing.T) {
	// NEITHER source: a resident worker between rounds, killed before its first
	// claim. The line must state the finding and the reason — never an empty value
	// that reads like a value.
	clearStopVerdictTaskLatch(t)
	clearAnnouncedTaskID(t)

	if got := stopVerdictTaskID(); got != "" {
		t.Fatalf("stopVerdictTaskID() = %q, want empty when nothing is announced and nothing was claimed", got)
	}
	verdict := stopVerdictLedgerLine(workerRoleLabel, "")
	if strings.Contains(verdict, `task ""`) {
		t.Errorf("the verdict line still renders an empty task id instead of the reason:\n%s", verdict)
	}
	if !strings.Contains(verdict, stopVerdictUnattributable) {
		t.Errorf("the verdict line does not state WHY no event can be written:\n%s", verdict)
	}
	if !strings.Contains(verdict, signalrec.VerdictUnsourced) {
		t.Errorf("the verdict line no longer names the verdict it is about:\n%s", verdict)
	}
	// The absence is stated as an absence of an EVENT, so the reader cannot read
	// "the event was written with no id" out of it.
	if !strings.Contains(verdict, "no ") {
		t.Errorf("the verdict line does not say the event will NOT be written:\n%s", verdict)
	}
}
