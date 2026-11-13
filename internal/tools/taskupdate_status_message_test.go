// Regression guard for the `gopi task update` status diagnostics (task
// fix/task-update-error-message). The operator's report:
//
//	$ gopi task update <id> --status cancelled   # succeeded
//	$ gopi task update <id> --status canceled    # "invalid status"
//	$ gopi task update <id> --status cancelled   # succeeded
//
// read as "failed then succeeded = bug". The write never failed — but three
// surfaces made that unreadable, and each has its own assertion below:
//
//  1. the invalid-status remedy offered `reopened`, a status NO write can
//     reach (no taskAllowedTransitions key) — following the advice produced a
//     second refusal;
//  2. an illegal move off a TERMINAL status rendered the empty allow-list as
//     "(allowed from cancelled: )";
//  3. the notify allow-list drop line sat on stderr next to the success
//     without saying it was a delivery filter rather than a failure.
package tools

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// newStatusMsgTask seeds one row and returns its id (fresh temp ledger).
func newStatusMsgTask(t *testing.T) string {
	t.Helper()
	testutil.NewTestDBPath(t)
	id := "t-statusmsg"
	if _, err := TaskNew("status message fixture", id, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	return id
}

// TestInvalidStatusRefusalOffersOnlyWritableStatuses — the `canceled` typo
// (the word in the operator's report) gets a named near miss, and the remedy
// list never offers the retired `reopened` (which the state machine would
// refuse one step later).
func TestInvalidStatusRefusalOffersOnlyWritableStatuses(t *testing.T) {
	id := newStatusMsgTask(t)

	_, err := TaskUpdate(id, "canceled")
	if err == nil {
		t.Fatal("TaskUpdate(canceled): want a refusal, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, `invalid status "canceled"`) {
		t.Errorf("refusal must quote the token: %q", msg)
	}
	if !strings.Contains(msg, `did you mean "cancelled"`) {
		t.Errorf("refusal must name the one-edit near miss: %q", msg)
	}
	if strings.Contains(msg, taskStatusReopened) {
		t.Errorf("refusal offers the retired status %q — the remedy must be writable: %q", taskStatusReopened, msg)
	}
	// The remedy list itself, not just its rendering: no retired token.
	if statusListHas(taskStatusWritable, taskStatusReopened) {
		t.Errorf("taskStatusWritable contains the retired %q", taskStatusReopened)
	}
	if len(taskStatusWritable) != len(taskStatusSet)-1 {
		t.Errorf("taskStatusWritable = %v, want taskStatusSet minus the retired token", taskStatusWritable)
	}
	// A token that IS a retired status explains the retirement instead.
	_, err = TaskUpdate(id, taskStatusReopened)
	if err == nil || !strings.Contains(err.Error(), "retired") {
		t.Errorf("TaskUpdate(reopened) = %v, want the M4.20-B retirement advice", err)
	}
}

// TestTaskListFilterKeepsAcceptingLegacyStatus — the WRITE remedy narrowed,
// but a read-only filter must still accept the retired token so a legacy row
// stays queryable. A regression here would hide legacy rows from `task list`.
func TestTaskListFilterKeepsAcceptingLegacyStatus(t *testing.T) {
	newStatusMsgTask(t)
	if _, err := TaskList(taskStatusReopened); err != nil {
		t.Fatalf("TaskList(%q) must accept the legacy filter, got %v", taskStatusReopened, err)
	}
	if _, err := TaskList("canceled"); err == nil || !strings.Contains(err.Error(), `did you mean "cancelled"`) {
		t.Fatalf("TaskList(canceled) = %v, want the same near-miss refusal", err)
	}
	if !statusListHas(taskStatusListForCLI, taskStatusReopened) {
		t.Fatalf("taskStatusListForCLI must list the legacy token: %v", taskStatusListForCLI)
	}
}

// TestIllegalTransitionFromTerminalNamesTerminality — the empty allow-list
// render ("allowed from cancelled: )") is gone; a terminal row says it is
// terminal and names the reissue path.
func TestIllegalTransitionFromTerminalNamesTerminality(t *testing.T) {
	id := newStatusMsgTask(t)
	if _, err := TaskUpdate(id, taskStatusCancelled); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	_, err := TaskUpdate(id, taskStatusCompleted)
	if err == nil {
		t.Fatal("completed off a cancelled row: want a refusal, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "TERMINAL") {
		t.Errorf("refusal must say the status is terminal: %q", msg)
	}
	if strings.Contains(msg, "(allowed from") && strings.Contains(msg, ": )") {
		t.Errorf("refusal renders an empty allow-list: %q", msg)
	}
	if !strings.Contains(msg, "REISSUE") {
		t.Errorf("refusal must name the repair path (reissue): %q", msg)
	}
}

// TestIllegalTransitionStillListsAllowedMoves — a NON-terminal row keeps the
// allow-list (the change must not throw that information away).
func TestIllegalTransitionStillListsAllowedMoves(t *testing.T) {
	id := newStatusMsgTask(t)                   // created in_progress
	_, err := TaskUpdate(id, taskStatusPending) // in_progress has no backward move
	if err == nil {
		t.Fatal("in_progress→pending: want a refusal, got nil")
	}
	if !strings.Contains(err.Error(), taskStatusCompleted) || !strings.Contains(err.Error(), taskStatusFailed) {
		t.Errorf("refusal must list the allowed moves from in_progress: %q", err.Error())
	}
}

// TestNotifyAllowListDropReadsAsAFilterNotAFailure — the stderr line the
// operator saw right after a SUCCESSFUL cancel. It must say the write
// succeeded, name the event, and name the knob that widens the filter.
func TestNotifyAllowListDropReadsAsAFilterNotAFailure(t *testing.T) {
	id := newStatusMsgTask(t)
	// Isolate from the host's real settings.json (a configured notify section
	// would be the resolver's second source) and configure a delivery target
	// whose event set does not include `cancelled` — exactly the operator's
	// state in the report.
	t.Setenv("GOPI_SETTINGS", filepath.Join(t.TempDir(), "absent-settings.json"))
	t.Setenv(taskNotifyCmdEnv, "cat >/dev/null")
	t.Setenv(taskNotifyEventsEnv, taskNotifyEventsDefault())

	log := captureStderr(t, func() {
		if _, err := TaskUpdate(id, taskStatusCancelled); err != nil {
			t.Errorf("cancel: %v", err)
		}
	})
	if !strings.Contains(log, "not in the events allow-list") {
		t.Fatalf("stderr = %q, want the allow-list drop diagnostic", log)
	}
	if !strings.Contains(log, "FILTER, not a failure") || !strings.Contains(log, "the write succeeded") {
		t.Errorf("the drop line must separate a delivery filter from a failure: %q", log)
	}
	if !strings.Contains(log, taskNotifyEventsEnv) {
		t.Errorf("the drop line must name the knob to change: %q", log)
	}
	if !strings.Contains(log, taskNotifyEventsDefault()) {
		t.Errorf("the drop line must quote the effective allow-list: %q", log)
	}
}

// TestStatusBytesNear covers the near-miss predicate directly (transposition,
// insertion, substitution, and the two negatives that must NOT suggest).
func TestStatusBytesNear(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"canceled", "cancelled", true},   // one inserted byte
		{"cancelled", "canceled", true},   // ... in either argument order
		{"complted", "completed", true},   // one inserted byte
		{"faild", "failed", true},         // one inserted byte
		{"cancelled", "cancelledx", true}, // one appended byte
		{"cancelled", "canceledx", false}, // two edits apart
		{"cancelled", "cancelled", false}, // identical is not "near"
		{"pending", "verified", false},    // unrelated
	}
	for _, c := range cases {
		if got := statusBytesNear(c.a, c.b); got != c.want {
			t.Errorf("statusBytesNear(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// statusListHas is a local membership probe (the package already declares a
// different containsString helper, so no name collision).
func statusListHas(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
