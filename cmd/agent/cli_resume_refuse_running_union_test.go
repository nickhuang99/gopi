// THE REFUSAL SENTENCE IS A UNION, AND THIS IS THE ARM THAT FAILS IF A MERGE
// DROPS EITHER HALF (delivery round 2/3, 2026-11-05).
//
// `resumeGateRefuseRunning` is where two epics met on one line: the resume gate
// stopped treating pid liveness as the verdict (epic resume-gate-is-the-lock, so
// the sentence must say the LOCK arbitrated — not the pid), and the progress axis
// was appended to the same sentence (bug epic 01a1ac0f, resumeProgressNote — so
// the sentence must carry whether the holder is progressing or idle). A conflict
// resolved by keeping master's text, or by keeping ours, compiles and reads fine
// and silently deletes the other half.
//
// WHY THE ASSERTION IS ON RENDERED TEXT AND NOT ON THE STRUCT. The defect a
// half-merge produces is not a wrong value, it is a SENTENCE an operator acts on:
// "wait for the holder" sent somebody to wait 5h18m on a process with 24s of CPU
// (the measured incident that epic was filed for). So the arm drives
// resumeReportRefuseRunning — the ONE renderer — and asserts BOTH facts are in
// the bytes.
//
// THE %!s(MISSING) ARM IS THE FORMAT-SLOT GUARD. Adding or removing one of the
// two `%s` verbs is as easy as deleting a line, and a mismatched argument list
// makes fmt print `%!s(MISSING)` — so the second arm asserts that marker is
// absent on a row whose progress axis prints NOTHING. That is the ordinary case
// ("axis running, or unmeasurable"), and it is exactly the case a gofmt-clean
// format-string edit can break with no other symptom.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// resumeRefuseRunningFixture seeds a row whose holder is THIS process (a live
// identity, so the gate reaches resumeGateRefuseRunning with no lock) and whose
// own transcript is `age` old, then returns the row id.
//
// The transcript is what the progress axis reads, and it is placed at the ONE
// address the axis resolves for a row (`<profile>/sessions/<id>/session.jsonl`,
// reached through taskFindSessionJSONLForRow) rather than at a path invented
// here.
func resumeRefuseRunningFixture(t *testing.T, taskID string, age time.Duration) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	dir := filepath.Join(config.ProfileDirIn(home), config.SessionsDirName, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed the run directory: %v", err)
	}
	record := filepath.Join(dir, sessionlayout.RecordName)
	if err := os.WriteFile(record, []byte("{\"type\":\"user\"}\n"), 0o644); err != nil {
		t.Fatalf("seed the transcript: %v", err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(record, when, when); err != nil {
		t.Fatalf("age the transcript: %v", err)
	}
	resumeGateSeedRow(t, taskID, statusWordTestHolder(t, os.Getpid()))
	return taskID
}

// TestResumeRefuseRunningSentenceCarriesBothAxes pins the UNION: a verified-LIVE
// holder that NO run lock arbitrated is refused with BOTH the lock's own reading
// of the situation and the progress axis.
func TestResumeRefuseRunningSentenceCarriesBothAxes(t *testing.T) {
	testutil.NewTestDBPath(t)
	// The measured shape: silent for hours, so the axis says `idle` and carries
	// the recovery command.
	const taskID = "01a1ac12-0000-7000-8000-00000000u001"
	resumeRefuseRunningFixture(t, taskID, 5*time.Hour+18*time.Minute)

	st, err := tools.TaskProcessStatusFor(taskID)
	if err != nil || st == nil {
		t.Fatalf("read the fixture row back: %v", err)
	}
	if !st.Progress.Idle() {
		t.Fatalf("fixture is vacuous: the progress axis reads %q (%s), so this arm cannot prove the axis is carried — the transcript placement or its age is wrong",
			st.Progress.State, st.Progress.Why)
	}

	var buf bytes.Buffer
	resumeReportRefuseRunning(&buf, "gopi session resume", taskID)
	got := buf.String()

	// HALF 1 — the LOCK's reading. Without it the operator is told the pid decided
	// a claim the pid no longer decides (the bug epic resume-gate-is-the-lock closed).
	if !strings.Contains(got, "NO run lock could be taken") || !strings.Contains(got, "nothing arbitrated the claim") {
		t.Fatalf("the refusal no longer says the LOCK failed to arbitrate, so it reads as a pid verdict again:\n%s", got)
	}
	// HALF 2 — the PROGRESS axis, with the command that recovers the row. Without
	// it an operator waits on a holder that will never finish.
	if !strings.Contains(got, "PROGRESS: ") {
		t.Fatalf("the refusal no longer carries the progress axis (resumeProgressNote was dropped from the sentence):\n%s", got)
	}
	if !strings.Contains(got, tools.TakeOverFlagSpelling) {
		t.Fatalf("the progress axis no longer names the recovery command %q:\n%s", tools.TakeOverFlagSpelling, got)
	}
	// Neither half may be a format-slot casualty.
	if strings.Contains(got, "%!s(MISSING)") || strings.Contains(got, "%!") {
		t.Fatalf("the refusal's format string and its arguments disagree:\n%s", got)
	}
}

// TestResumeRefuseRunningSentenceFillsTheEmptyProgressSlot is the ordinary-case
// arm: when the axis prints nothing (here: a transcript that advanced a moment
// ago, so the holder is RUNNING), the sentence must still render cleanly — the
// empty `%s` slot must be filled by the empty string, not left as a stray verb.
//
// It also pins the silence itself: an axis that measured `running` must NOT be
// dressed as a finding (Rule 15 — an unmeasured axis is not a verdict, and a
// measured-and-fine one is not news).
func TestResumeRefuseRunningSentenceFillsTheEmptyProgressSlot(t *testing.T) {
	testutil.NewTestDBPath(t)
	const taskID = "01a1ac12-0000-7000-8000-00000000u002"
	// Fresh transcript: well inside the idle threshold.
	resumeRefuseRunningFixture(t, taskID, 0)

	st, err := tools.TaskProcessStatusFor(taskID)
	if err != nil || st == nil {
		t.Fatalf("read the fixture row back: %v", err)
	}
	if st.Progress.Idle() {
		t.Fatalf("fixture is vacuous: the axis already reads idle (%s), so the empty-slot case is not exercised", st.Progress.Why)
	}

	var buf bytes.Buffer
	resumeReportRefuseRunning(&buf, "", taskID)
	got := buf.String()

	if strings.Contains(got, "%!") {
		t.Fatalf("the empty progress slot is not filled cleanly:\n%s", got)
	}
	if strings.Contains(got, "PROGRESS: ") {
		t.Fatalf("a measured-and-running axis must print nothing (a sentence there claims a finding that decided nothing):\n%s", got)
	}
	// The prog name falls back to the ONE constant rather than printing a blank
	// subject — an empty caller must not produce ": refusing — …".
	if !strings.HasPrefix(got, resumeProgName+": refusing") {
		t.Fatalf("an empty prog name did not fall back to %q:\n%s", resumeProgName, got)
	}
	if !strings.Contains(got, "nothing arbitrated the claim") {
		t.Fatalf("the lock half is missing:\n%s", got)
	}
}
