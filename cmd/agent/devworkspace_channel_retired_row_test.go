package main

// devworkspace_channel_retired_row_test.go — the RETIRED-ROW boundary driven
// through the PRODUCTION status reader (row 01a18392).
//
// devworkspace_delivery_loop_test.go proves the LOOP's arithmetic against an
// injected status reader (cheap, no ledger). This file proves the other half,
// the one a fake cannot reach: that `cloneRunState.stopIfRowRetired` in
// production — `rowStatus` nil, i.e. tools.TaskRowStatusAt — really reads the
// LEDGER, really sees an operator's `cancelled`, and really stops the loop
// before an attempt is spent or an agent round is spawned.
//
// The two are deliberately separate. A test that injects the status reader can
// only show that the loop reacts to a STRING; the defect this fixes was that the
// loop never asked the ledger at all, so the seam that must be pinned is the
// read itself, against a ledger this test wrote.

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/devworkspace"
	"github.com/nick/gopi/internal/tools"
)

const retiredRowTaskID = "01a18392-0000-7000-8000-00000000e7e1"

// retiredRowLedger points this test at a REAL, private ledger and seeds the row
// whose status is the variable under test. The returned state reads that row
// through PRODUCTION's reader (rowStatus nil) and records attempts/spawns in the
// returned struct rather than in the state's own fields.
func retiredRowLedger(t *testing.T, status string) (*cloneRunState, *retiredRowTally) {
	t.Helper()
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	t.Setenv(memoryDBEnvLegacy, "")
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "retired-row delivery loop fixture",
		Payload: "the delivery loop reads this row's status at its round boundary",
		TaskID:  retiredRowTaskID,
		Source:  "human",
	}); err != nil {
		t.Fatalf("submit the fixture row: %v", err)
	}
	if status != tools.TaskStatusPending {
		if _, err := tools.TaskUpdateReason(retiredRowTaskID, status, "fixture status", ""); err != nil {
			t.Fatalf("move the fixture row to %s: %v", status, err)
		}
	}
	// AND THE LEDGER REALLY SAYS SO — otherwise every assertion below would be
	// measuring this test's own intent rather than the reader's answer.
	got, err := tools.TaskRowStatusAt(retiredRowTaskID)
	if err != nil {
		t.Fatalf("read the seeded status back: %v", err)
	}
	if got != status {
		t.Fatalf("seeded status = %q, ledger reports %q", status, got)
	}

	tally := &retiredRowTally{}
	s := &cloneRunState{
		cfg: devworkspace.Config{TaskID: retiredRowTaskID},
		dir: "/ws/" + retiredRowTaskID,
		// rowStatus is LEFT NIL ON PURPOSE: that is production's wiring, and the
		// seam under test is the real tools.TaskRowStatusAt.
	}
	s.attempt = func() (devworkspace.Result, *devworkspace.Blocker, error) {
		tally.attempts++
		return devworkspace.Result{Dir: s.dir}, dirtyBlocker(), nil
	}
	s.agentRound = func(dir, briefing string) error {
		tally.agentRounds++
		return nil
	}
	return s, tally
}

// retiredRowTally counts what the loop SPENT, which the state's own `rounds`
// field cannot (the loop overwrites it, and exhausted()'s closing report round
// is not an attempt).
type retiredRowTally struct {
	attempts    int
	agentRounds int
}

// TestRetiredRowStopsTheLoopThroughTheRealLedger is the acceptance criterion on
// the PRODUCTION seam: a cancelled row stops finish() at its first boundary, so
// no attempt is spent, no agent round is spawned, and neither of the two
// terminal sentences is written (the single `rounds == -1` marker would mean a
// round started).
func TestRetiredRowStopsTheLoopThroughTheRealLedger(t *testing.T) {
	s, tally := retiredRowLedger(t, tools.TaskStatusCancelled)
	var log strings.Builder
	res, err := s.finish(&log)

	if err == nil || !errors.Is(err, errRowRetired) {
		t.Fatalf("a cancelled row must stop the loop with the retirement classification, got %v", err)
	}
	if tally.agentRounds != 0 {
		t.Fatalf("agent rounds = %d, want 0 (a cancelled row must spawn nothing)", tally.agentRounds)
	}
	if tally.attempts != 0 {
		t.Fatalf("attempts = %d, want 0 (no attempt may be spent on a cancelled row)", tally.attempts)
	}
	if res.Pushed {
		t.Fatalf("nothing was pushed, so the result must not claim one: %+v", res)
	}
	for _, banned := range []string{"delivery did not land", "nothing to deliver"} {
		if strings.Contains(log.String(), banned) || strings.Contains(err.Error(), banned) {
			t.Fatalf("a cancelled row must not be written with %q:\n%s\n%s", banned, log.String(), err.Error())
		}
	}
	if !strings.Contains(err.Error(), tools.TaskStatusCancelled) {
		t.Fatalf("the reason must name the status the ledger reported: %s", err.Error())
	}
}

// TestLiveRowStillSpendsItsRoundsThroughTheRealLedger is the control: the same
// production reader, the same ledger, an in_progress row — and the loop runs to
// its named limit exactly as before. Without this arm a "fix" that stopped every
// run would pass the test above.
func TestLiveRowStillSpendsItsRoundsThroughTheRealLedger(t *testing.T) {
	s, tally := retiredRowLedger(t, tools.TaskStatusInProgress)
	var log strings.Builder
	_, err := s.finish(&log)

	if err == nil || errors.Is(err, errRowRetired) {
		t.Fatalf("an in_progress row must run the loop, got %v", err)
	}
	if tally.attempts != devworkspace.MaxDeliveryRounds {
		t.Fatalf("attempts = %d, want the named limit %d (unchanged behaviour)", tally.attempts, devworkspace.MaxDeliveryRounds)
	}
	if s.rounds != devworkspace.MaxDeliveryRounds {
		t.Fatalf("rounds = %d, want the named limit %d", s.rounds, devworkspace.MaxDeliveryRounds)
	}
	if !strings.Contains(err.Error(), "delivery did not land") {
		t.Fatalf("an exhausted live row keeps its own sentence: %v", err)
	}
}
