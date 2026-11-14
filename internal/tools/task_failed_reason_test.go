package tools

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// The epic failed-reason-attribution (measured 2026-10-07): 179 of 330 failed
// rows on the live ledger carried the literal "task failed" in their reason
// column, while 164 of them carried a genuine cause on their OWN task_events
// trail — written by the SAME call, in the SAME statement. These tests pin the
// derivation that closes that gap. They FAIL on the pre-fix tree (whose failed
// arm returned the literal), which is how the report's red proof was taken.

// failedReasonLiteral is the pre-fix default, kept here verbatim: it is the
// string these tests assert AGAINST, and naming it once is what lets a later
// reader see exactly which sentence the fix retired (Rules 1b/13).
const failedReasonLiteral = "task failed"

// failedReasonSeedRow creates a row and leaves it in_progress — the only state
// a `failed` transition may come from.
func failedReasonSeedRow(t *testing.T, id string) {
	t.Helper()
	if _, err := TaskNewOID(TaskNewOptions{TaskID: id, Summary: "fixture row " + id}); err != nil {
		t.Fatalf("create %s: %v", id, err)
	}
}

// failedReasonRow reads a row back through the package's own accessor.
func failedReasonRow(t *testing.T, id string) *Task {
	t.Helper()
	return taskRowByID(t, id)
}

// failedReasonAssertInformative is the shared assertion of the family: the row
// is failed AND its reason says something the old literal did not.
func failedReasonAssertInformative(t *testing.T, id, wantIn string) {
	t.Helper()
	row := failedReasonRow(t, id)
	if row.Status != taskStatusFailed {
		t.Fatalf("%s: status = %q, want failed", id, row.Status)
	}
	if row.Reason == failedReasonLiteral {
		t.Fatalf("%s: reason is still the fact-free literal %q — the cause the same write carried was discarded", id, failedReasonLiteral)
	}
	if strings.TrimSpace(row.Reason) == "" {
		t.Fatalf("%s: failed row closed with an EMPTY reason (Rule 15a)", id)
	}
	if !strings.HasPrefix(row.Reason, taskReasonFailedPrefix) {
		t.Fatalf("%s: reason %q must carry the failed prefix %q", id, row.Reason, taskReasonFailedPrefix)
	}
	if !strings.Contains(row.Reason, wantIn) {
		t.Fatalf("%s: reason %q must carry %q", id, row.Reason, wantIn)
	}
}

// TestFailedReasonCarriesTheWritebackDigest is the RED PROOF of the fix: the
// cause the caller passed as the writeback digest reaches the reason column,
// not only the trail.
func TestFailedReasonCarriesTheWritebackDigest(t *testing.T) {
	testutil.NewTestDBPath(t)
	const id = "t-failed-reason-digest"
	failedReasonSeedRow(t, id)

	const cause = "zombie disposal (zombie: heartbeat stalled past grace); claimed pid 4242 dead (peerbox)"
	if _, err := TaskUpdateWithResult(id, taskStatusFailed, cause); err != nil {
		t.Fatalf("failed writeback: %v", err)
	}
	failedReasonAssertInformative(t, id, "zombie disposal")

	// The trail is unchanged by the derivation (append-only, same writer).
	events := taskEventsFor(t, id)
	if len(events) == 0 || !strings.Contains(events[len(events)-1], cause) {
		t.Fatalf("the digest must ride the trail verbatim, got %v", events)
	}
}

// TestFailedReasonPrefersTheRunStatsTail: a real run digest opens with its
// stats run — the reason must quote the WHY after it, never the stats.
func TestFailedReasonPrefersTheRunStatsTail(t *testing.T) {
	testutil.NewTestDBPath(t)
	const id = "t-failed-reason-stats"
	failedReasonSeedRow(t, id)

	digest := taskReasonStatsPrefix + "12.5s iter=4 tokens=115821" + taskReasonSegmentSeparator +
		"FAILED — recoverable: `gopi session resume " + id + "`"
	if _, err := TaskUpdateWithResult(id, taskStatusFailed, digest); err != nil {
		t.Fatalf("failed writeback: %v", err)
	}
	failedReasonAssertInformative(t, id, "FAILED — recoverable")
	if strings.Contains(failedReasonRow(t, id).Reason, "elapsed=") {
		t.Fatalf("reason quotes the stats run instead of the cause: %q", failedReasonRow(t, id).Reason)
	}
}

// TestFailedReasonWithoutAnyCauseDeclaresThat: a writer supplying neither a
// reason nor a digest gets a sentence that SAYS so — never one that
// impersonates a diagnosis (Rule 15).
func TestFailedReasonWithoutAnyCauseDeclaresThat(t *testing.T) {
	testutil.NewTestDBPath(t)
	const id = "t-failed-reason-bare"
	failedReasonSeedRow(t, id)

	if _, err := TaskUpdate(id, taskStatusFailed); err != nil {
		t.Fatalf("bare failed transition: %v", err)
	}
	if got := failedReasonRow(t, id).Reason; got != taskReasonFailedNoCause {
		t.Fatalf("reason = %q, want the declared no-cause sentence %q", got, taskReasonFailedNoCause)
	}
	if !strings.Contains(failedReasonRow(t, id).Reason, "no cause recorded") {
		t.Fatalf("reason must admit that no cause was recorded: %q", failedReasonRow(t, id).Reason)
	}
}

// TestFailedReasonCallerReasonAlwaysWins: an explicit --reason is written
// verbatim — the derivation never overrides a human's words.
func TestFailedReasonCallerReasonAlwaysWins(t *testing.T) {
	testutil.NewTestDBPath(t)
	const id = "t-failed-reason-explicit"
	failedReasonSeedRow(t, id)

	const explicit = "rework filed as 01a00000-0000-7000-8000-000000000001 after merging master"
	if _, err := TaskUpdateReason(id, taskStatusFailed, explicit, "gopi run failed: exit status 1"); err != nil {
		t.Fatalf("explicit reason: %v", err)
	}
	if got := failedReasonRow(t, id).Reason; got != explicit {
		t.Fatalf("reason = %q, want the caller's verbatim %q", got, explicit)
	}
}

// TestFailedReasonIsCapped: a long digest yields a readable one-line reason
// (its head decides reissue-vs-resolve; the tail is prose).
func TestFailedReasonIsCapped(t *testing.T) {
	got := taskFailedReason(strings.Repeat("x", taskReasonDigestMaxRunes+500))
	if !strings.HasSuffix(got, taskReasonDigestEllipsis) {
		t.Fatalf("a capped reason must be marked as capped: %q", got)
	}
	if n := len([]rune(got)); n > taskReasonDigestMaxRunes+len([]rune(taskReasonFailedPrefix))+1 {
		t.Fatalf("capped reason is %d runes, over the cap", n)
	}
}

// TestFailedReasonClosesTheWholeDoorFamily pins the FAMILY rather than the one
// call site: every door that closes a row `failed` must leave a reason that
// carries information or declares its absence.
func TestFailedReasonClosesTheWholeDoorFamily(t *testing.T) {
	testutil.NewTestDBPath(t)

	// Door ①: the harness writeback (`task run`'s outcome, TaskUpdateWithResult).
	const wbID = "t-failed-door-writeback"
	failedReasonSeedRow(t, wbID)
	if _, err := TaskRunWriteback(wbID, TaskStatusFailed, "gopi run failed: exit status 1"); err != nil {
		t.Fatalf("door①: writeback: %v", err)
	}
	failedReasonAssertInformative(t, wbID, "gopi run failed")

	// Door ③: the plain status move with no cause at all.
	const plID = "t-failed-door-plain"
	failedReasonSeedRow(t, plID)
	if _, err := TaskUpdate(plID, TaskStatusFailed); err != nil {
		t.Fatalf("door③: plain failed: %v", err)
	}
	failedReasonAssertInformative(t, plID, "no cause recorded")
}
