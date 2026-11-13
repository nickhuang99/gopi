// Membership gate for the notify DEFAULT event set (tasknotify.go's decision
// site). The set is a hand-maintained list that must decide about every
// WRITABLE task status, so each writable status must be a MEMBER of the
// default set or an entry of the RECORDED-EXCLUSION table carrying its
// reason.
//
// THE DEFECT THIS CLOSES (measured 2026-10-12, bug report
// artifacts/bug-notify-allowlist-omits-superseded.md): `superseded` — a
// writable terminal status added 2026-10-07 — was in neither place, so
// `gopi task update <row> --status superseded` exited 0, the row read
// `superseded`, and its notify event was dropped, with nothing anywhere
// recording that anyone had decided that. A hand-spelled default list and a
// growing status vocabulary is the shape; this gate is what makes the drift
// impossible to reintroduce silently (the same shape as
// TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth for the alarm registry).
package tools

import (
	"strings"
	"testing"
)

// TestTaskNotifyEventsDefaultCoversEveryWritableStatus — every writable
// status is accounted for EXACTLY ONCE: a member of the default set, or a
// recorded exclusion with a non-empty reason.
//
// Three failures are possible and all three mean "someone must decide":
//   - a status in NEITHER table  → the `superseded` defect;
//   - a status in BOTH tables    → a contradiction (the set cannot deliver
//     and refuse the same event);
//   - an exclusion that is not writable (or a member that is not) → the
//     tables name a status no write can reach, so the accounting is fake.
func TestTaskNotifyEventsDefaultCoversEveryWritableStatus(t *testing.T) {
	if len(taskStatusWritable) == 0 {
		t.Fatal("taskStatusWritable is empty — the scan is mis-rooted, not the tree clean")
	}
	members := map[string]bool{}
	for _, s := range taskNotifyEventsMembers {
		if members[s] {
			t.Errorf("taskNotifyEventsMembers lists %q twice", s)
		}
		members[s] = true
	}
	if len(members) == 0 {
		t.Fatal("taskNotifyEventsMembers is empty — the default set would deliver nothing")
	}

	for _, status := range taskStatusWritable {
		inDefault := members[status]
		reason, excluded := taskNotifyEventsExcludedDefault[status]
		switch {
		case !inDefault && !excluded:
			t.Errorf("writable status %q is NEITHER a member of the default notify "+
				"event set NOR a recorded exclusion — a `task update --status %s` "+
				"would be delivered to nobody by default and no decision about it is "+
				"written down anywhere. Either add it to taskNotifyEventsMembers, or "+
				"record it in taskNotifyEventsExcludedDefault with the reason",
				status, status)
		case inDefault && excluded:
			t.Errorf("writable status %q is BOTH a default-set member and a recorded "+
				"exclusion (reason: %q) — the two tables contradict each other",
				status, reason)
		case excluded && strings.TrimSpace(reason) == "":
			t.Errorf("recorded exclusion %q carries no reason — an unexplained "+
				"exclusion is indistinguishable from the omission this gate exists to catch",
				status)
		}
	}

	// The tables may not name a status the operator can never write: an
	// exclusion for a non-writable status is a decision about nothing, and
	// it would satisfy the accounting above without deciding anything.
	writable := map[string]bool{}
	for _, s := range taskStatusWritable {
		writable[s] = true
	}
	for status := range taskNotifyEventsExcludedDefault {
		if !writable[status] {
			t.Errorf("taskNotifyEventsExcludedDefault excludes %q, which is not a "+
				"WRITABLE status — a `--status %s` write is refused, so an exclusion "+
				"for it accounts for nothing (did you mean a writable status?)",
				status, status)
		}
	}
	for _, s := range taskNotifyEventsMembers {
		if !writable[s] {
			t.Errorf("taskNotifyEventsMembers lists %q, which is not a WRITABLE status "+
				"— the default set must name statuses a transition can actually reach", s)
		}
	}

	// The rendered wire string is the members, in order, and nothing else:
	// the default a caller quotes in a diagnostic must be the same set the
	// gate just checked (a hand-spelled string is exactly how the two drifted
	// apart).
	rendered := taskNotifyEventsDefault()
	want := strings.Join(taskNotifyEventsMembers, ",")
	if rendered != want {
		t.Errorf("taskNotifyEventsDefault() = %q, want the members %q", rendered, want)
	}
	for _, status := range taskStatusWritable {
		if members[status] == strings.Contains(","+rendered+",", ","+status+",") {
			continue
		}
		t.Errorf("rendered default %q disagrees with the member table about %q", rendered, status)
	}
}

// TestTaskNotifyEventsDecidedStatusesAreExactly — the exclusion table is not a
// dumping ground: it holds exactly the statuses the DECISION names, so a later
// "fix" that silences a status by moving it here has to change THIS list (and
// therefore the record) rather than pass unnoticed.
//
// The list is the decision in executable form: `pending` (a birth, not a
// transition), `cancelled` and `verified` (writable and deliverable on
// subscription, but a noise-budget call — the default is what an operator who
// configured nothing gets). Every other writable status is a default MEMBER.
func TestTaskNotifyEventsDecidedStatusesAreExactly(t *testing.T) {
	wantExcluded := []string{taskStatusPending, taskStatusCancelled, taskStatusVerified}
	wantMembers := []string{taskStatusClaimed, taskStatusInProgress, taskStatusCompleted, taskStatusFailed, taskStatusSuperseded}

	for _, s := range wantExcluded {
		if _, ok := taskNotifyEventsExcludedDefault[s]; !ok {
			t.Errorf("%q must be a recorded exclusion — if the decision changed, say so "+
				"here AND in the decision-site comment, never quietly", s)
		}
	}
	for _, s := range wantMembers {
		found := false
		for _, m := range taskNotifyEventsMembers {
			if m == s {
				found = true
			}
		}
		if !found {
			t.Errorf("%q must be a default-set member — if the decision changed, say so "+
				"here AND in the decision-site comment, never quietly", s)
		}
	}
	for status := range taskNotifyEventsExcludedDefault {
		known := false
		for _, s := range wantExcluded {
			if s == status {
				known = true
			}
		}
		if !known {
			t.Errorf("taskNotifyEventsExcludedDefault excludes %q, which this test does "+
				"not record as a decided exclusion — an exclusion is a POLICY CHANGE, not a "+
				"way to silence one status: update this list and the decision-site comment "+
				"in the same commit", status)
		}
	}
	if len(taskNotifyEventsExcludedDefault)+len(taskNotifyEventsMembers) != len(taskStatusWritable) {
		t.Errorf("decided statuses = %d excluded + %d members, want the %d writable statuses "+
			"to be accounted for exactly once",
			len(taskNotifyEventsExcludedDefault), len(taskNotifyEventsMembers), len(taskStatusWritable))
	}
}

// TestTaskNotifyEventsDefaultDeliversEveryMember — the behavioural half:
// with NO notify env and NO settings file, a transition to each default
// member is actually DELIVERED through the real filter+delivery path, and a
// transition to the recorded exclusion is not. This is what proves the
// membership table is not merely decorative: `superseded` reaching this
// table without reaching the default string would still drop the event.
func TestTaskNotifyEventsDefaultDeliversEveryMember(t *testing.T) {
	log := taskNotifyTestSetup(t)
	t.Setenv("GOPI_SETTINGS", t.TempDir()+"/absent-settings.json")
	t.Setenv(taskNotifyEventsEnv, "") // empty string = env present, so the DEFAULT is what loadTaskNotifyConfig falls back to

	for _, status := range taskNotifyEventsMembers {
		row := &Task{TaskID: "t-default-" + status, Status: status, Summary: "membership delivery probe"}
		notifyTaskTransition("probe-previous-"+status, row)
	}

	delivered := map[string]bool{}
	for _, p := range readNotifyLog(t, log) {
		delivered[p.Event] = true
	}
	for _, status := range taskNotifyEventsMembers {
		if !delivered[status] {
			t.Errorf("status %q is a default-set member but was NOT delivered — the "+
				"default string and the member table have drifted apart", status)
		}
	}
	for status := range taskNotifyEventsExcludedDefault {
		if delivered[status] {
			t.Errorf("status %q is a RECORDED EXCLUSION but was delivered by the "+
				"default set — the exclusion and the default string contradict each other", status)
		}
	}

	// `superseded` specifically: the measured defect. Called out by name so a
	// failure of this file names the bug it was written for.
	if !delivered[taskStatusSuperseded] {
		t.Errorf("`superseded` was not delivered by the default notify event set — " +
			"this is the exact regression the gate exists to catch (a Rule-33 parent-" +
			"release status whose transition released children silently)")
	}
}
