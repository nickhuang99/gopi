package tools

// task_writeback_lost_test.go — THE MINIMAL REPRO for a terminal writeback the
// ledger REFUSED, which used to leave its reason on a stderr line belonging to
// a process that had already exited.
//
// THE MEASURED SHAPE (this host's ledger, 2026-10-27): the wrapper's five
// failure-branch call sites called writebackOutcome as an expression statement,
// so the ledger's refusal was discarded. A row whose `failed` write did not
// land stayed `in_progress` with a dead holder — four such rows were read off
// one peer — and nothing on the row said why.
//
// The fixture drives the REAL refusal (a `cancelled` row is TERMINAL: no status
// write leaves it — taskAllowedTransitions) through the REAL writer, and asserts
// the loss reaches the surface an operator reads AFTERWARDS: `task show`.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// refusedWriteback returns the ledger's OWN refusal for a terminal row: the
// error text belongs to the gate that produced it, so the test never invents
// one and never re-derives it (Rule 15).
func refusedWriteback(t *testing.T, row string) error {
	t.Helper()
	if _, err := TaskNew("writeback-lost fixture", row, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	if _, err := TaskUpdateReason(row, TaskStatusCancelled, "fixture: terminal row", ""); err != nil {
		t.Fatalf("seed a terminal row: %v", err)
	}
	_, wbErr := TaskRunWriteback(row, TaskStatusFailed, "the run failed")
	if wbErr == nil {
		t.Fatalf("fixture premise broken: the ledger ACCEPTED a failed writeback on a cancelled row, "+
			"so there is no refusal to report on row %s", row)
	}
	return wbErr
}

// TestTaskWritebackLostIsRecordedOnTheRowItself is the repro: a refused
// terminal write leaves a verdict on the row's own trail, so "why is this row
// still open?" is answerable from `task show` alone.
//
// RED ON THE PRE-FIX TREE: nothing called TaskWritebackLost at all, so the
// trail held no such event and the show printed the status with no reason.
func TestTaskWritebackLostIsRecordedOnTheRowItself(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	const row = "t-writeback-lost-trail"

	wbErr := refusedWriteback(t, row)
	loss := TaskWritebackLost(row, TaskStatusFailed, wbErr)

	if !loss.TrailRecorded {
		t.Fatalf("the loss did not reach the row's own trail (note: %q) — the whole point is that a "+
			"survivable record exists after the process is gone", loss.TrailNote)
	}
	if loss.TrailNote != "" {
		t.Fatalf("TrailNote must be empty when the trail write landed, got %q", loss.TrailNote)
	}
	if !strings.Contains(loss.Line, row) {
		t.Errorf("the sentence must name the row, got %q", loss.Line)
	}
	if !strings.Contains(loss.Line, TaskStatusFailed) {
		t.Errorf("the sentence must name the status that did not land, got %q", loss.Line)
	}
	if !strings.Contains(loss.Line, wbErr.Error()) {
		t.Errorf("the sentence must quote the ledger's OWN refusal (never a re-derived guess); got:\n%s\nwant it to contain:\n%s",
			loss.Line, wbErr.Error())
	}

	// The row is left EXACTLY as the ledger left it: reporting must never
	// become disposing (the retired zombie sweep's job).
	if st, err := TaskStatusOf(row); err != nil || st != TaskStatusCancelled {
		t.Fatalf("status = %q, err = %v — a report must not move the row", st, err)
	}

	// AND THE OPERATOR-FACING SURFACE RENDERS IT: membership in
	// verdictTrailEventNames is what puts it on `task show`.
	show, err := TaskShow(row)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, WritebackLostEvent) || !strings.Contains(show, "DID NOT LAND") {
		t.Fatalf("`task show` must carry the writeback verdict and its reason; got:\n%s", show)
	}
}

// TestTaskWritebackLostJoinsBothRegistries pins the ONE constant, two
// consumers shape (Rule 1b/13): the notify alarm and the row's render read the
// same name, so neither can drift from the other.
func TestTaskWritebackLostJoinsBothRegistries(t *testing.T) {
	if !taskShowVerdictEventName(WritebackLostEvent) {
		t.Errorf("%q is not in verdictTrailEventNames, so `task show` would never render it",
			WritebackLostEvent)
	}
	found := false
	for _, alarm := range taskNotifyAlarmEvents {
		if alarm == WritebackLostEvent {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("%q is not in taskNotifyAlarmEvents, so GOPI_TASK_NOTIFY_EVENTS=all would silently "+
			"drop the alarm for the operator who asked for everything", WritebackLostEvent)
	}
}

// TestTaskWritebackLostReportsItsOwnFailure is the honesty half: when even the
// trail write cannot land, the sentence must SAY so rather than read as a clean
// record. A reporter that hides its own failure is the defect it exists to
// catch (Rule 15), and on this defect's own shape the notify alarm plus stderr
// are then the last witnesses.
func TestTaskWritebackLostReportsItsOwnFailure(t *testing.T) {
	// A path that IS a directory: the ledger cannot be opened at all, and (unlike
	// a missing file, which openTasksDB CREATES through ensureKnowledgeBase) this
	// state does not resolve itself — the reporter must say so rather than fall
	// silent or claim a record it never wrote.
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	if err := os.MkdirAll(dbPath, 0o755); err != nil {
		t.Fatalf("seed an unopenable ledger path: %v", err)
	}
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	loss := TaskWritebackLost("t-writeback-lost-unwritable", TaskStatusFailed,
		errStub("ledger refused the write"))

	if loss.TrailRecorded {
		t.Fatal("TrailRecorded must be false when the ledger could not be opened")
	}
	if strings.TrimSpace(loss.TrailNote) == "" {
		t.Fatal("TrailNote must carry WHY the trail write failed — an unexplained false is a silent failure")
	}
	if !strings.Contains(loss.Line, "the trail entry could NOT be written either") {
		t.Fatalf("the sentence must disclose the second failure, got %q", loss.Line)
	}
	if !strings.Contains(loss.Line, loss.TrailNote) {
		t.Fatalf("the sentence must carry the reason the trail write failed, got %q", loss.Line)
	}
}

// errStub is a bare error carrying an asserted text, so a rewrite cannot
// quietly narrow what reaches the operator.
type errStub string

func (e errStub) Error() string { return string(e) }
