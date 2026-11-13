package tools

// Regression for `gopi task update --force` — the CLI face of the operator's
// DELIVERY-PROOF escape (epic 01a17dcd).
//
// THE SHAPE. GOPI_TASK_DELIVERY_PROOF_ALLOW already turned a delivery-proof
// refusal into a loud record, but it was reachable only as an environment
// variable on the process — an operator closing a row by hand had to export it
// to use a decision they had already made. The flag is the same decision with a
// typed spelling. It does NOT skip the measurement: the arm still reads the
// row's tip and its declared destination, and a waived close is still RECORDED
// (delivery-proof-unverified), so "proven" and "let through by decision" stay
// distinguishable on the row's trail.
//
// THE CONTROL GROUP IS THE POINT (the epic's own acceptance): the same fixture,
// the same write, WITHOUT the flag, must still be REFUSED with the unchanged
// sentence. A test that only proved the release would pass on a guard that had
// been deleted.
//
// These tests drive the REAL write path (TaskUpdateReasonForced →
// taskUpdateDeliveryForced → taskUpdateStampedCore), against the same measured
// clone-channel fixture the delivery-proof arm's own tests use
// (task_delivery_proof_test.go): a commit that exists ONLY in the ground.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/testutil"
)

// dpForceFixture builds one undelivered row of the incident's exact shape and
// returns its ledger path. The ambient environment is pinned OFF, so a host
// that happens to carry GOPI_TASK_DELIVERY_PROOF_ALLOW cannot make the control
// group pass (or the flag group prove the wrong source) silently.
func dpForceFixture(t *testing.T, id string) string {
	t.Helper()
	t.Setenv(deliveryProofAllowEnv, "0")
	db := testutil.NewTestDBPath(t)
	ground, _, _, _ := dpCloneChannel(t)
	repo := filepath.Join(filepath.Dir(ground), "seed")
	dpSeedRow(t, db, id, "feat: work that never left the ground", taskStatusInProgress)
	dpWriteDelivery(t, db, id, repo, masterBranchName, ground)
	return db
}

// TestDeliveryUpdateForce_ReleasesAndRecords is the flag's positive arm: the
// write lands, the row is terminal, and the trail carries the waiver.
func TestDeliveryUpdateForce_ReleasesAndRecords(t *testing.T) {
	id := "dp-force-released"
	db := dpForceFixture(t, id)

	if _, err := TaskUpdateReasonForced(id, taskStatusCompleted, "operator waived the proof", "", true); err != nil {
		t.Fatalf("--force must let the write through, got: %v", err)
	}
	if got := dpRowStatus(t, db, id); got != taskStatusCompleted {
		t.Fatalf("row status = %q, want %q (the released write must land)", got, taskStatusCompleted)
	}
	if n := dpEventCount(t, db, id, DeliveryProofUnverifiedEvent); n == 0 {
		t.Errorf("a --force release must be RECORDED as %s — an unproven close that leaves no trace is "+
			"indistinguishable from a proven one", DeliveryProofUnverifiedEvent)
	}
	if n := dpEventCount(t, db, id, deliveryProofOwedEvent); n != 0 {
		t.Errorf("a released write must not also record %s (the row does not owe a delivery: it was let through)",
			deliveryProofOwedEvent)
	}
}

// TestDeliveryUpdateForce_ControlWithoutFlagStillRefuses is THE control group:
// exactly the same fixture and write, no flag, no env — refused exactly as
// before, row still open, refusal sentence unchanged.
//
// RED WHEN THE GUARD IS NEUTRALIZED (the release leaking into the default
// path): the write returns nil and the row lands terminal.
func TestDeliveryUpdateForce_ControlWithoutFlagStillRefuses(t *testing.T) {
	id := "dp-force-control"
	db := dpForceFixture(t, id)

	_, err := TaskUpdateReasonForced(id, taskStatusCompleted, "no flag was typed", "", false)
	if err == nil {
		t.Fatalf("without --force the undelivered claim MUST still be refused — the guard changed behaviour")
	}
	if !strings.Contains(err.Error(), deliveryProofMarker) {
		t.Errorf("the refusal sentence must be UNCHANGED (it names the undelivered deliverable), got: %v", err)
	}
	if got := dpRowStatus(t, db, id); got != taskStatusInProgress {
		t.Errorf("row status = %q, want %q (a refused write must not land)", got, taskStatusInProgress)
	}
	if n := dpEventCount(t, db, id, deliveryProofOwedEvent); n == 0 {
		t.Errorf("the refusal must still leave a %s trail entry", deliveryProofOwedEvent)
	}
	if n := dpEventCount(t, db, id, DeliveryProofUnverifiedEvent); n != 0 {
		t.Errorf("a REFUSED write must not record %s — that name means the write was released unproven",
			DeliveryProofUnverifiedEvent)
	}
}

// TestDeliveryUpdateForce_EnvStillWorksAlone pins the other half of "one
// decision point": the environment variable keeps its exact pre-existing
// behaviour on the ordinary entry point, with no flag anywhere on the command
// line. If the flag had been implemented as a second, parallel branch, this is
// the case that would rot.
func TestDeliveryUpdateForce_EnvStillWorksAlone(t *testing.T) {
	id := "dp-force-env"
	db := dpForceFixture(t, id)
	t.Setenv(deliveryProofAllowEnv, "1")

	if _, err := TaskUpdateReasonForced(id, taskStatusCompleted, "env says allow", "", false); err != nil {
		t.Fatalf("%s=1 must still release the write on its own, got: %v", deliveryProofAllowEnv, err)
	}
	if got := dpRowStatus(t, db, id); got != taskStatusCompleted {
		t.Errorf("row status = %q, want %q", got, taskStatusCompleted)
	}
	if n := dpEventCount(t, db, id, DeliveryProofUnverifiedEvent); n == 0 {
		t.Errorf("the env release must record %s exactly like the flag does", DeliveryProofUnverifiedEvent)
	}
}

// TestDeliveryUpdateForce_ShowRendersTheWaiver pins the VISIBILITY half: the
// record on the trail is worthless if the surface a reviewer reads does not
// render it. `task show` selects verdict lines through
// verdictTrailEventNames, so `delivery-proof-unverified` must be a member —
// otherwise a waived close and a proven close look IDENTICAL on `task show`
// (measured while running this epic: the event was in task_events and the show
// printed nothing).
func TestDeliveryUpdateForce_ShowRendersTheWaiver(t *testing.T) {
	id := "dp-force-show"
	dpForceFixture(t, id)

	if _, err := TaskUpdateReasonForced(id, taskStatusCompleted, "operator waived the proof", "", true); err != nil {
		t.Fatalf("--force must let the write through, got: %v", err)
	}
	out, err := TaskShow(id)
	if err != nil {
		t.Fatalf("task show %s: %v", id, err)
	}
	if !strings.Contains(out, DeliveryProofUnverifiedEvent) {
		t.Errorf("`task show` does not render %s — a waived close would be indistinguishable from a proven one "+
			"on the one surface a reviewer reads. `task show`:\n%s", DeliveryProofUnverifiedEvent, out)
	}
	if !strings.Contains(out, cliverbs.TaskUpdateForceFlag) {
		t.Errorf("the rendered waiver must name the source that asked (%s); `task show`:\n%s",
			cliverbs.TaskUpdateForceFlag, out)
	}
}

// TestDeliveryProofAllowanceFor_OneDecisionPoint is the structural half of the
// epic's constraint ("no flag works / env does not" fork): BOTH sources are
// resolved by ONE function into ONE boolean, and the source it reports is the
// source that actually asked.
func TestDeliveryProofAllowanceFor_OneDecisionPoint(t *testing.T) {
	cases := []struct {
		name    string
		flag    bool
		env     string
		inForce bool
		byFlag  bool
		byEnv   bool
	}{
		{name: "neither", flag: false, env: "0"},
		{name: "flag only", flag: true, env: "0", inForce: true, byFlag: true},
		{name: "env only", flag: false, env: "1", inForce: true, byEnv: true},
		{name: "both", flag: true, env: "1", inForce: true, byFlag: true, byEnv: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(deliveryProofAllowEnv, tc.env)
			got := deliveryProofAllowanceFor(tc.flag)
			if got.InForce != tc.inForce || got.ByFlag != tc.byFlag || got.ByEnv != tc.byEnv {
				t.Fatalf("allowance(flag=%v, env=%q) = %+v, want {InForce:%v ByFlag:%v ByEnv:%v}",
					tc.flag, tc.env, got, tc.inForce, tc.byFlag, tc.byEnv)
			}
			src := got.source()
			// The reported source is the source that ASKED — never a guess and
			// never a source that stayed silent.
			if tc.byFlag != strings.Contains(src, cliverbs.TaskUpdateForceFlag) {
				t.Errorf("source() = %q; it names %s iff the flag asked (byFlag=%v)",
					src, cliverbs.TaskUpdateForceFlag, tc.byFlag)
			}
			if tc.byEnv != strings.Contains(src, deliveryProofAllowEnv) {
				t.Errorf("source() = %q; it names %s iff the env asked (byEnv=%v)",
					src, deliveryProofAllowEnv, tc.byEnv)
			}
		})
	}
}
