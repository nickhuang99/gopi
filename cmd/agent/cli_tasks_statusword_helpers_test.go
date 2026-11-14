package main

// The OTHER TWO split-readers of a rendered `status:` field, pinned at the
// helper.
//
// The split itself lives once (Rule 14): statusWord in
// cli_tasks_statusword_split_test.go, over tools.StatusWordMarkerSep — the constant
// the renderer itself uses (internal/tools/statusword.go). Two of its callers
// had no test of their own:
//
//	taskShowStatus   (cli_tasks_reportfacts_seam_test.go)  — reads the ledger's
//	                 answer out of a `task show` render captured by a caller
//	cloneClaimStatusIs (clone_channel_delivery_before_claim_e2e_test.go) —
//	                 answers "is this row <word>?" out of the same render
//
// Both are PURE string→word functions, so they are judged here on a fixture
// field built from the production separator — no subprocess, no ledger, and
// therefore no chance that a marker which happens NOT to render (a live holder,
// a pending row) makes the assertion below pass vacuously. taskStatusOf, the
// third caller, is pinned at the real renderer instead
// (TestTaskStatusOfAnswersTheWordNotTheHolderMarker).
//
// WHY IT MATTERS, in one line: a caller comparing the WHOLE field against a
// status CONSTANT goes red for a holder-liveness measurement it never asked
// about — measured 2026-11-05 (task row 01a1a8c6-9a4e-76b4-8687-a09f65fba456):
// `ledger row status after : "in_progress + holder dead (pid 1885160 gone)"`
// against an escalation line carrying the bare word, a comparison that cannot
// be true by construction.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tools"
)

// statusWordHelperFixtureMarker is the marker TEXT this file appends after the
// production separator. The real wording lives in internal/tools
// (taskHolderGoneMarkerFmt, unexported) and is pinned there by
// TestHolderGoneMarkerOnDeadHolderListRow / …MarksARecycledPid; what the
// SPLITTER promises is narrower and is what these cases judge: everything from
// the first separator onward is dropped.
const statusWordHelperFixtureMarker = "holder dead (pid 1073741824 gone)"

// statusWordHelperFixtureField is the rendered field shape a caller actually
// receives — the status word, then the production separator, then a marker.
func statusWordHelperFixtureField() string {
	return tools.TaskStatusInProgress + tools.StatusWordMarkerSep + statusWordHelperFixtureMarker
}

// assertFixtureCarriesTheMarker is half ① of every case below: without it the
// helper assertion would be satisfied by a field that has nothing to split, and
// the test would stop measuring the split the moment the renderer's shape
// changed (Rule 15: a scan that found nothing must not read as a pass).
func assertFixtureCarriesTheMarker(t *testing.T, field string) {
	t.Helper()
	if !strings.Contains(field, tools.StatusWordMarkerSep) {
		t.Fatalf("the fixture field %q does not carry the production separator %q — "+
			"the split below would be untested; fix the fixture, not the helper",
			field, tools.StatusWordMarkerSep)
	}
}

// TestTaskShowStatusAnswersTheWordNotTheMarker is half ② for taskShowStatus:
// it must answer the status WORD, which is the value the ledger holds and the
// value a status constant carries.
func TestTaskShowStatusAnswersTheWordNotTheMarker(t *testing.T) {
	field := statusWordHelperFixtureField()
	assertFixtureCarriesTheMarker(t, field)

	// `task show` renders the field on its own line, column-aligned; the helper
	// scans the whole render, so the fixture is a one-line render.
	if got := taskShowStatus(t, "status:         "+field+"\n"); got != tools.TaskStatusInProgress {
		t.Fatalf("taskShowStatus() = %q, want %q — it is returning the rendered field "+
			"(status word + measured marker) instead of the status word.\n"+
			"rendered field: %q", got, tools.TaskStatusInProgress, field)
	}
}

// TestCloneClaimStatusIsAnswersTheWordNotTheMarker is the same for
// cloneClaimStatusIs, which answers a BOOLEAN: a whole-field comparison there
// reports "not completed" for a row that IS completed and whose holder was
// measured gone — the false failure this epic was filed for, in the other
// direction.
func TestCloneClaimStatusIsAnswersTheWordNotTheMarker(t *testing.T) {
	field := statusWordHelperFixtureField()
	assertFixtureCarriesTheMarker(t, field)
	render := "status:         " + field + "\n"

	if !cloneClaimStatusIs(t, render, tools.TaskStatusInProgress) {
		t.Fatalf("cloneClaimStatusIs(%q, %q) = false, want true — the whole field "+
			"(word + measured marker) is being compared to the status word.\n"+
			"rendered field: %q", render, tools.TaskStatusInProgress, field)
	}
	// The other direction, so the boolean is not pinned to "always true".
	if cloneClaimStatusIs(t, render, tools.TaskStatusFailed) {
		t.Fatalf("cloneClaimStatusIs answered true for %q on a row whose word is %q — "+
			"rendered field: %q", tools.TaskStatusFailed, tools.TaskStatusInProgress, field)
	}
}

// TestStatusWordHelpersLeaveAMarkerlessFieldAlone pins the no-op half: a state
// that measures nothing renders no marker, and the helpers must return it
// unchanged. Without this, a helper that unconditionally truncated at some
// later offset would still pass both cases above.
func TestStatusWordHelpersLeaveAMarkerlessFieldAlone(t *testing.T) {
	field := tools.TaskStatusPending
	if strings.Contains(field, tools.StatusWordMarkerSep) {
		t.Fatalf("the fixture word %q carries the separator — it cannot test the no-op half", field)
	}
	render := "status:         " + field + "\n"
	if got := taskShowStatus(t, render); got != field {
		t.Fatalf("taskShowStatus() = %q on a markerless field, want %q", got, field)
	}
	if !cloneClaimStatusIs(t, render, field) {
		t.Fatalf("cloneClaimStatusIs answered false for a markerless %q row", field)
	}
}
