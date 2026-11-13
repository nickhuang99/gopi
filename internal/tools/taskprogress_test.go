package tools

// THE PROGRESS AXIS UNDER TEST (bug epic 01a1ac0f).
//
// What must stay true, and what each arm pins:
//
//  1. a transcript that advanced inside the threshold ⇒ running;
//  2. a transcript silent past the threshold ⇒ idle, and the answer NAMES the
//     silence and the recovery command (the epic's deliverable);
//  3. nothing to read ⇒ unmeasurable, never running and never idle (Rule 15b:
//     an unmeasured axis is not an answer);
//  4. the axis is INDEPENDENT of the holder verdict — a live pid that has gone
//     quiet must be reportable, which is the exact pair this epic was filed on
//     (holder 518745 alive 5h18m, 24s CPU, transcript silent since 16:57).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionlayout"
)

// seedProgressRun plants the run's OWN evidence — a session transcript at the
// address the ONE resolver reads — and returns the row plus the instant its
// mtime was written.
//
// The fixture pins GOPI_HOME (GOPI_HOME is a HOME; the profile root is
// <.gopi>/ under it — Rule 34's shape) and writes nothing into the operator's
// profile.
func seedProgressRun(t *testing.T, taskID string, advance time.Time) *Task {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	// GOPI_HOME is a HOME: the profile root is <.gopi>/ under it (Rule 34), so
	// the run directory is <home>/.gopi/sessions/<task_id> — written through
	// the profile-root helper, not spelled by hand.
	dir := filepath.Join(config.ProfileDirIn(home), config.SessionsDirName, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed the run directory: %v", err)
	}
	record := filepath.Join(dir, sessionlayout.RecordName)
	if err := os.WriteFile(record, []byte("{\"type\":\"user\"}\n"), 0o644); err != nil {
		t.Fatalf("seed the transcript: %v", err)
	}
	if err := os.Chtimes(record, advance, advance); err != nil {
		t.Fatalf("stamp the transcript's mtime: %v", err)
	}
	return &Task{TaskID: taskID, Status: taskStatusInProgress, SessionID: taskID}
}

// TestProgressAxisRunningIdleUnmeasurable drives the three states off the SAME
// measurement, changing only the transcript's age — so a passing run cannot be
// an artifact of the fixture's shape.
func TestProgressAxisRunningIdleUnmeasurable(t *testing.T) {
	const taskID = "01a1ac0f-0000-0000-0000-00000000fade"
	now := time.Now()

	// (1) advanced 30s ago ⇒ running.
	task := seedProgressRun(t, taskID, now.Add(-30*time.Second))
	if p := taskProgressAxisFor(task, os.Getpid(), now); p.State != TaskProgressRunning {
		t.Fatalf("transcript written 30s ago read as %q (%s) — want %q", p.State, p.Why, TaskProgressRunning)
	}

	// (2) silent for 5h18m ⇒ idle, and the silence is NAMED. This is the
	// measured shape of the row the epic was filed for.
	task = seedProgressRun(t, taskID, now.Add(-5*time.Hour-18*time.Minute))
	p := taskProgressAxisFor(task, os.Getpid(), now)
	if p.State != TaskProgressIdle {
		t.Fatalf("transcript silent 5h18m read as %q (%s) — want %q", p.State, p.Why, TaskProgressIdle)
	}
	if !p.Idle() {
		t.Fatal("TaskProgress.Idle() is false for State=idle")
	}
	if p.IdleSeconds < 19000 || p.IdleSeconds > 19200 {
		t.Fatalf("IdleSeconds = %.0f, want ≈19120 (5h18m) — the axis must report the silence it measured", p.IdleSeconds)
	}
	if !strings.Contains(p.Why, "--takeover") {
		t.Fatalf("the idle verdict does not name the recovery command: %q", p.Why)
	}
	if p.Evidence == "" || !strings.HasSuffix(p.Evidence, "session.jsonl") {
		t.Fatalf("Evidence = %q, want the run's own transcript", p.Evidence)
	}
	if !p.CPUKnown || p.CPUSeconds <= 0 {
		t.Fatalf("cpu reading missing (known=%v seconds=%v) — the axis must corroborate with the holder's own CPU time", p.CPUKnown, p.CPUSeconds)
	}

	// (3) no evidence at all ⇒ unmeasurable, with the search NAMED.
	empty := &Task{TaskID: "01a1ac0f-0000-0000-0000-00000000000e", Status: taskStatusInProgress, SessionID: "01a1ac0f-0000-0000-0000-00000000000e"}
	u := taskProgressAxisFor(empty, os.Getpid(), now)
	if u.State != TaskProgressUnmeasurable {
		t.Fatalf("a row with no transcript read as %q (%s) — want %q", u.State, u.Why, TaskProgressUnmeasurable)
	}
	if strings.TrimSpace(u.Why) == "" {
		t.Fatal("the unmeasurable verdict carries no reason — absence must be stated (Rule 15)")
	}
	if u.Idle() {
		t.Fatal("an unmeasured axis reports Idle() = true — 'not measured' must never read as 'idle'")
	}

	// (4) no holder pid ⇒ unmeasurable, NOT idle: nothing was judged.
	if p0 := taskProgressAxisFor(task, 0, now); p0.State != TaskProgressUnmeasurable {
		t.Fatalf("a row judged with no holder pid read as %q — want %q", p0.State, TaskProgressUnmeasurable)
	}
}

// TestProgressAxisThresholdIsTheNamedConstant pins the boundary itself, so a
// later change to taskProgressIdleAfter cannot silently invert the two sides.
func TestProgressAxisThresholdIsTheNamedConstant(t *testing.T) {
	const taskID = "01a1ac0f-0000-0000-0000-00000000b0de"
	now := time.Now()
	justInside := seedProgressRun(t, taskID, now.Add(-taskProgressIdleAfter).Add(time.Second))
	if p := taskProgressAxisFor(justInside, os.Getpid(), now); p.State != TaskProgressRunning {
		t.Fatalf("silence 1s inside the threshold read as %q — want %q", p.State, TaskProgressRunning)
	}
	justOutside := seedProgressRun(t, taskID, now.Add(-taskProgressIdleAfter).Add(-time.Second))
	if p := taskProgressAxisFor(justOutside, os.Getpid(), now); p.State != TaskProgressIdle {
		t.Fatalf("silence 1s past the threshold read as %q — want %q", p.State, TaskProgressIdle)
	}
}

// TestProgressAxisSplitsAliveFromWorking is the epic's own pair, on one row: a
// pid that IS alive and an axis that says it is NOT working. If a future change
// folds the axis back into the holder verdict, this arm is what fails.
func TestProgressAxisSplitsAliveFromWorking(t *testing.T) {
	const taskID = "01a1ac0f-0000-0000-0000-00000000a11e"
	now := time.Now()
	task := seedProgressRun(t, taskID, now.Add(-5*time.Hour-18*time.Minute))
	p := taskProgressAxisFor(task, os.Getpid(), now)
	if p.State != TaskProgressIdle {
		t.Fatalf("state = %q, want %q", p.State, TaskProgressIdle)
	}
	// The same pid, through the EXISTING liveness reader: it must still be
	// alive. Two facts, two answers, one process — that is the epic.
	if !pidAlive(os.Getpid()) {
		t.Fatal("pidAlive(this process) is false — the arm cannot distinguish 'alive' from 'working' without a live pid")
	}
}
