package tools

// THE HOLDER-GONE READING — the regression proof for the defect measured
// 2026-10-27 on row 01a17aba (`gopi task status --worktree <ground>`):
//
//	holder:      host-a:1698361 (claim stamp is fresh for this run)
//	verdict:     dead
//	live:        pid 1698361 alive=no
//
// /proc/1698361 did not exist. The suffix was rendered from the AGE axis alone
// (claimStampFresh: the stamp was written with the row's transition), so the
// executor's death was asserted twice and denied once on the same screen — and
// an operator reading "fresh for this run" concludes the ground is IN USE.
//
// These tests assert the BEHAVIOUR of the surfaces an operator reads (the
// rendered report and the wire field), not the internal classification alone:
// a fix that renames a constant without changing what the verb prints is not
// the fix.

import (
	"os"
	"strings"
	"testing"
)

// TestClaimStampVerdictWithdrawsFreshForADeadHolder is the minimal repro: the
// same row shape the incident carried (in_progress, a FRESH local claim stamp,
// the named pid gone) must stop reading "fresh".
func TestClaimStampVerdictWithdrawsFreshForADeadHolder(t *testing.T) {
	dead := deadPID(t)
	row := &Task{
		Status:          taskStatusInProgress,
		ClaimedBy:       hostPid(t, dead),
		HasClaim:        true,
		ClaimedAt:       fixtureClaimInstant,
		HasInProgressAt: true,
		InProgressAt:    fixtureClaimInstant,
	}
	// The fixture must carry the CLASSIFICATION THE BUG IS ABOUT: the age axis
	// calls this stamp fresh (a test whose row is already stale proves nothing).
	if got := claimStampFreshness(row); got != claimStampFresh {
		t.Fatalf("fixture broken: age axis = %q, want %q", got, claimStampFresh)
	}
	if got := claimStampVerdict(row); got != claimStampHolderGone {
		t.Fatalf("claimStampVerdict(dead holder) = %q, want %q — a dead holder is not a fresh run", got, claimStampHolderGone)
	}
}

// TestTaskStatusReportNeverCallsADeadHolderFresh is the surface assertion: the
// rendered `holder:` line — the exact line the operator read — must not carry
// the fresh sentence, and must say the holder is gone.
func TestTaskStatusReportNeverCallsADeadHolderFresh(t *testing.T) {
	dead := deadPID(t)
	row := &Task{
		Status:          taskStatusInProgress,
		ClaimedBy:       hostPid(t, dead),
		HasClaim:        true,
		ClaimedAt:       fixtureClaimInstant,
		HasInProgressAt: true,
		InProgressAt:    fixtureClaimInstant,
	}
	report := TaskProcessStatusReport(taskProcessStatus(row))

	if strings.Contains(report, claimStampFreshSuffixText) {
		t.Fatalf("DEFECT: the report calls a dead holder fresh:\n%s", report)
	}
	if !strings.Contains(report, claimStampHolderGoneSuffixText) {
		t.Fatalf("the report must state that the stamp's holder is gone:\n%s", report)
	}
	// The verdict is untouched by this fix: the pid layer already said dead.
	if got := taskProcessStatus(row).Verdict; got != TaskProcessStateDead {
		t.Fatalf("verdict = %q, want %q — withdrawing 'fresh' must not weaken the pid layer", got, TaskProcessStateDead)
	}
}

// TestClaimStampVerdictKeepsFreshForALiveHolder is the guard against the
// obvious over-correction: a LIVE local holder keeps the fresh reading, and on
// the OTHER axes nothing changes value.
func TestClaimStampVerdictKeepsFreshForALiveHolder(t *testing.T) {
	live := &Task{
		Status: taskStatusInProgress, ClaimedBy: hostPid(t, os.Getpid()), HasClaim: true,
		ClaimedAt: fixtureClaimInstant, HasInProgressAt: true, InProgressAt: fixtureClaimInstant,
	}
	if got := claimStampVerdict(live); got != claimStampFresh {
		t.Errorf("a live holder = %q, want %q", got, claimStampFresh)
	}
	// stale (a previous claimant) stays stale whatever its pid is doing.
	stale := &Task{
		Status: taskStatusInProgress, ClaimedBy: hostPid(t, deadPID(t)), HasClaim: true,
		ClaimedAt: fixtureClaimInstant - claimStampSkewSec - 1, HasInProgressAt: true, InProgressAt: fixtureClaimInstant,
	}
	if got := claimStampVerdict(stale); got != claimStampStale {
		t.Errorf("a stale stamp = %q, want %q", got, claimStampStale)
	}
	// A FOREIGN stamp's pid names a process on ANOTHER host: unknowable here, so
	// the age axis' answer stands (Rule 15a — never manufacture a death).
	foreign := &Task{
		Status: taskStatusInProgress, ClaimedBy: fixtureForeignClaimHolder, HasClaim: true,
		ClaimedAt: fixtureClaimInstant, HasInProgressAt: true, InProgressAt: fixtureClaimInstant,
	}
	if got := claimStampVerdict(foreign); got != claimStampFresh {
		t.Errorf("a foreign stamp = %q, want %q (the pid is not ours to probe)", got, claimStampFresh)
	}
}

// TestClaimStampHolderLiveProbesTheKernelNow pins the three readings on the two
// pids this host can answer for with certainty: a live one and a reaped one.
func TestClaimStampHolderLiveProbesTheKernelNow(t *testing.T) {
	if !claimStampHolderLive(os.Getpid()) {
		t.Errorf("this process (pid %d) must read alive", os.Getpid())
	}
	if claimStampHolderLive(deadPID(t)) {
		t.Error("a reaped child must read dead")
	}
	if !claimStampHolderLive(0) {
		t.Error("pid 0 is 'nothing to measure' — an absent pid may never be read as dead")
	}
}

// fixtureClaimInstant is the shared timestamp of the claim/start pair: the two
// are written milliseconds apart, so the row is FRESH on the age axis.
const fixtureClaimInstant = 1_700_000_000.0

// fixtureForeignClaimHolder is a claim stamp minted on another machine (the
// pid it names means nothing on this host).
const fixtureForeignClaimHolder = "another-host:1698361"

// The two sentences the report renders for the freshness axis, taken from the
// renderer itself so the assertions cannot drift from the words an operator
// reads (Rule 14 — one definition).
var (
	claimStampFreshSuffixText      = taskFreshnessSuffix(claimStampFresh)
	claimStampHolderGoneSuffixText = taskFreshnessSuffix(claimStampHolderGone)
)
