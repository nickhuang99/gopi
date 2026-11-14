package main

// daemon_scope_note_test.go — the WORDING half of the epic "merger restart
// giving some warning/error message" (2026-09-13).
//
// WHY A WORDING TEST AT ALL: the defect the operator reported was not that the
// daemon failed to detach — it is that a normal, unavoidable state was printed
// as a failure ("WARNING: … starting with setsid only — this daemon WILL die
// when the session scope is torn down"), which sends the reader looking for a
// bug that does not exist. The fix has two halves: the availability probe must
// stop FABRICATING a reason (internal/cgroupscope), and the operator-facing
// verdict must be the daemon's MEASURED cgroup, phrased as a plain note.
//
// These cases pin the second half on SYNTHESIZED inputs (the host's delegation
// state must not decide whether the wording is tested): the note must name the
// measured cgroup, must not be warning/error-shaped, and must never present the
// pre-start PREDICTION as if it were a reading.

import (
	"errors"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/cgroupscope"
)

const (
	// dsNoteSessionScope is a cgroup-relative path shaped like a login session
	// scope (the membership whose teardown SIGTERMs the daemon) — the fact the
	// note exists to report.
	dsNoteSessionScope = "/user.slice/user-1000.slice/session-18855.scope"
	// dsNoteEscapedScope is the same process AFTER a successful escape: the
	// daemon's own cgroup under the user manager. Nothing may be reported here.
	dsNoteEscapedScope = "/user.slice/user-1000.slice/user@1000.service/gopi-daemon-merger"
	// dsNoteProg is the face's program name in the note (mergerProgTopLevel's
	// value); a literal here is what makes the assertion independent of the
	// constant it mirrors.
	dsNoteProg = "gopi merger"
)

// dsNoteUnavailDecision is the decision the note is rendered for when no escape
// is available on the host — the case the operator's transcript shows.
func dsNoteUnavailDecision() daemonScopeDecision {
	return daemonScopeDecision{mode: daemonScopeModeNone, detail: "no user bus; no write on the slice"}
}

// TestDaemonScopeDegradedNote_HasNoAlarmWording is the minimal repro guard for
// the operator's report (epic "merger restart giving some warning/error
// message", 2026-09-13). It reads the CONSTANT the parent renders, so it fails
// on the pre-fix code (which spelled "WARNING" and "WILL die") without needing
// this host's delegation state — and it is the case that fails RED on master
// while the fix is absent.
func TestDaemonScopeDegradedNote_HasNoAlarmWording(t *testing.T) {
	for _, bad := range []string{"WARNING", "WILL die", "error", "is a directory"} {
		if strings.Contains(daemonScopeDegradedNote, bad) {
			t.Errorf("the degraded note carries alarm wording %q — a normal, unavoidable state must read as a plain note:\n%s",
				bad, daemonScopeDegradedNote)
		}
	}
	if !strings.Contains(daemonScopeDegradedNote, "could not leave the login session scope") {
		t.Errorf("the note must keep the sentence the e2e suite asserts verbatim:\n%s", daemonScopeDegradedNote)
	}
}

// TestDaemonScopeNote_InSessionScopeIsAPlainNote pins the operator-visible
// outcome for the case they reported: the daemon really is still in the login
// session scope, and the note says so WITHOUT the warning/error shape, naming
// the cgroup that was MEASURED (not the probe's prediction).
func TestDaemonScopeNote_InSessionScopeIsAPlainNote(t *testing.T) {
	line := daemonScopeNoteFor(dsNoteProg, dsNoteSessionScope, nil, dsNoteUnavailDecision())
	if line == "" {
		t.Fatal("a daemon measured inside a login session scope must be reported")
	}
	// The contract other tests assert verbatim (cmd/agent/daemon_cgroup_e2e_test.go):
	// the sentence must survive the rewording.
	if !strings.Contains(line, "could not leave the login session scope") {
		t.Errorf("the note must keep the sentence the e2e asserts, got %q", line)
	}
	if !strings.Contains(line, dsNoteSessionScope) {
		t.Errorf("the note must name the MEASURED cgroup %q, got %q", dsNoteSessionScope, line)
	}
	// The shape that made the operator file this epic: no alarm words.
	for _, bad := range []string{"WARNING", "WILL die", "error", "failed"} {
		if strings.Contains(line, bad) {
			t.Errorf("the note is not a plain note: it carries %q:\n%s", bad, line)
		}
	}
}

// TestDaemonScopeNote_EscapedDaemonIsNotReported is the other direction, and it
// is what keeps the note from LYING about a good start: when the measured cgroup
// is outside any session scope, the line must be empty — the escape's mechanism
// is already named by the pre-start line.
func TestDaemonScopeNote_EscapedDaemonIsNotReported(t *testing.T) {
	if line := daemonScopeNoteFor(dsNoteProg, dsNoteEscapedScope, nil, dsNoteUnavailDecision()); line != "" {
		t.Errorf("a daemon measured OUTSIDE a session scope must produce no note, got %q", line)
	}
}

// TestDaemonScopeNote_UnreadableCgroupNamesThePredictionAsAPrediction pins the
// honesty rule for the ONE case where the prediction still reaches the operator:
// the measurement itself failed. The line must say the reading is missing and
// label the probe as the probe — never quietly pass the prediction off as the
// daemon's real cgroup.
func TestDaemonScopeNote_UnreadableCgroupNamesThePredictionAsAPrediction(t *testing.T) {
	d := dsNoteUnavailDecision()
	line := daemonScopeNoteFor(dsNoteProg, "", errors.New("open /proc/9/cgroup: no such file"), d)
	if line == "" {
		t.Fatal("an unreadable cgroup with an unavailable escape must be reported")
	}
	if !strings.Contains(line, "could not be read") {
		t.Errorf("the note must admit the measurement is missing, got %q", line)
	}
	if !strings.Contains(line, "pre-start probe") {
		t.Errorf("the note must label the prediction as the probe it is, got %q", line)
	}
	if !strings.Contains(line, d.detail) {
		t.Errorf("the note must carry the probe's reason %q, got %q", d.detail, line)
	}
	// But an UNREADABLE cgroup on a daemon the probe expected to escape is not
	// worth a note: the probe's own line already reported the mechanism.
	if line := daemonScopeNoteFor(dsNoteProg, "", errors.New("boom"),
		daemonScopeDecision{mode: daemonScopeModeCgfs}); line != "" {
		t.Errorf("an unreadable cgroup after a successful escape decision must stay silent, got %q", line)
	}
}

// TestDaemonScopeDegradedNote_IsASingleLine is the "don't paper the prompt" half:
// the degraded case is the NORMAL outcome on this host, so the note is read on
// every restart. It must stay one line — a mechanism list pasted inline is what
// made the original message unreadable.
func TestDaemonScopeDegradedNote_IsASingleLine(t *testing.T) {
	multi := daemonScopeDecision{
		mode:   daemonScopeModeNone,
		detail: scopeNoneDetail(errors.New("line one\nline two\nline three"), errors.New("bus\nmore")),
	}
	line := daemonScopeNoteFor(dsNoteProg, dsNoteSessionScope, nil, multi)
	if strings.Contains(line, "line two") || strings.Contains(line, "more") {
		t.Errorf("multi-line reasons must be reduced to their first line, got %q", line)
	}
	if strings.Count(strings.TrimRight(line, "\n"), "\n") != 0 {
		t.Errorf("the note must be one line, got %q", line)
	}
}

// TestDaemonScopeNote_SessionScopeClassifierIsTheSharedOne is a guard against a
// second, drifting notion of "in a login session scope": the note must use the
// same predicate every other verdict uses (cgroupscope.InSessionScope), so the
// wording and the verdict can never disagree.
func TestDaemonScopeNote_SessionScopeClassifierIsTheSharedOne(t *testing.T) {
	if !cgroupscope.InSessionScope(dsNoteSessionScope) {
		t.Fatalf("%q must classify as a login session scope — the fixture is wrong", dsNoteSessionScope)
	}
	if cgroupscope.InSessionScope(dsNoteEscapedScope) {
		t.Fatalf("%q must NOT classify as a login session scope — the fixture is wrong", dsNoteEscapedScope)
	}
}
