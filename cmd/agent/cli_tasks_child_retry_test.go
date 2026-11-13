package main

// cli_tasks_child_retry_test.go — the acceptance tests of the wrapped-executor
// child's bounded automatic retry (epic wrapped-executor-child, 4/5).
//
// THE THREE FACTS THE EPIC NAMES, and where each is pinned here:
//
//	① the ladder is BOUNDED: an abnormally-exited child is resumed at most
//	  tools.TaskRunRetryCap times and then handed over ONCE with the named
//	  reason — no further spawn, no unbounded restart loop.
//	② a JUDGED failure is NOT a crash: a child that closed its own row before
//	  exiting non-zero is never retried (the whole point of the discriminator).
//	③ the counter SURVIVES A RESTART: the attempt count is read back off the
//	  row (an append-only tag trail), so a wrapper that dies cannot forget how
//	  many attempts it already spent — a FRESH process continues the ladder
//	  instead of restarting it.
//
// The tests drive the wrapper's real retry arm (taskRunAbnormalChildRetry)
// in-process with the spawn seam stubbed (taskRunResumeSpawn), so they assert
// the DECISION — how many spawns, with which reason, recorded where — without
// starting real runs. The ledger they read and write is a real one.

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/tools"
)

// The dead-holder fixture's bounds and names (Rule 1: no inline literals).
const (
	// retryDeadHolderProgram is the fixture holder child itself: any program
	// that does not exit lets the kill land, and the choice is the kernel's
	// coreutils. It is named, not typed inline (Rule 1b).
	retryDeadHolderProgram = "sleep"
	// retryDeadHolderHoldSec is that child's own lifetime — it only has to
	// outlive the kill that makes it a zombie.
	retryDeadHolderHoldSec = "30"
	// retryDeadHolderWait bounds the wait for SIGKILL to land — the child is
	// not a zombie until it has actually died. A host that reaped the child
	// for us never satisfies it, so the wait reports instead of hanging.
	retryDeadHolderWait = 5 * time.Second
	// retryDeadHolderPoll is the poll cadence for that wait.
	retryDeadHolderPoll = 10 * time.Millisecond
)

// retryFixture is one hermetic retry playground: a fresh ledger holding a row
// in the state the wrapper sees when it reaches its failed-child return (the
// run is in_progress, the producing process is gone).
type retryFixture struct {
	ledger string
	row    string
	spawns int
}

// newRetryFixture builds the fixture and stubs the spawn seam for the duration
// of the test: every call records that it happened and reports a live pid, so
// the ladder believes a run started (the shape of a real successful spawn)
// without one existing.
func newRetryFixture(t *testing.T, taskID string) *retryFixture {
	t.Helper()
	f := &retryFixture{ledger: newRetryLedger(t), row: taskID}
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		TaskID:  taskID,
		Summary: "abnormal child retry fixture",
		Payload: "fixture row",
		Source:  "human",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	// Drive the row to in_progress with a holder whose pid is PROVABLY gone
	// (see retryDeadHolderPID), so the double-run gate admits a resume. The
	// dead stamp must be FRESH (a stale one is judged unreadable and refuses —
	// correctly), hence the row is stamped at THIS instant.
	if _, err := tools.TaskUpdateWithResult(taskID, tools.TaskStatusInProgress, "run in flight"); err != nil {
		t.Fatalf("in_progress: %v", err)
	}
	retrySeedDeadHolder(t, taskID, retryDeadHolderPID(t))

	origSpawn := taskRunResumeSpawn
	t.Cleanup(func() { taskRunResumeSpawn = origSpawn })
	taskRunResumeSpawn = func(taskID, target string) (string, string, int, error) {
		f.spawns++
		return "gopi session resume " + target + " --task " + taskID, t.TempDir() + "/resume.log", os.Getpid(), nil
	}
	return f
}

// newRetryLedger points THIS process at a fresh task ledger (the env var the
// tools package resolves at every call — one place, so the fixture cannot half
// sandbox the process).
func newRetryLedger(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/memory.db"
	t.Setenv("GOPI_MEMORY_DB", path)
	return path
}

// retryDeadHolderPID returns a pid that NO live process can own, and the child
// that holds it is returned so teardown can reap it.
//
// The FIRST shape here was "one past the highest /proc entry", on the theory
// that the kernel's pid range starts at 1 and the fixture never spawns. That
// premise is false and it is the whole defect: the number is not RESERVED, it
// is the very NEXT pid the kernel will hand out (pids are allocated
// sequentially), so a freshly spawned process is the MOST likely owner of it
// — and at spawn-heavy moments something always owns it. Measured 2026-09-18:
// the classifier (taskIdentityVerdict) fell past the identity axes (the row
// carried an EMPTY proc triple, which is also a shape production's own
// TaskStampHolder refuses to write) onto the legacy name guard
// (taskPidIsRunner: /proc/<pid>/comm starts with "gopi"), found a live
// gopi-named process wearing the number, and answered `running` — so all four
// retry tests died at this helper's precondition instead of the ladder.
//
// The shape that IS hermetic: an UNREAPED child of this process. Reaping is
// what frees a pid, so a child that is killed and never waited for keeps its
// number reserved for good — no other process can wear it — while
// `pidAlive` already reads its zombie state 'Z' as DEAD (taskzombie.go, the
// shape a SIGKILLed wrapper leaves). The verdict is therefore `dead` for a
// reason that rests on the kernel's reservation, not on pid arithmetic or on
// how busy the host is.
//
// The child's /proc/<pid>/stat stays readable after death, so the row can
// carry a REAL recorded identity (retrySeedDeadHolder stamps it through the
// production writer) — no fabricated ticks, and the classifier now decides on
// the identity axis instead of the coarse name guard.
func retryDeadHolderPID(t *testing.T) (pid int) {
	t.Helper()
	child := exec.Command(retryDeadHolderProgram, retryDeadHolderHoldSec)
	if err := child.Start(); err != nil {
		t.Fatalf("start dead-holder fixture child: %v", err)
	}
	pid = child.Process.Pid
	// Teardown REAPS the child: not reaping is what made the pid hermetic
	// during the test, and not reaping forever would leak a zombie per test.
	//stylegate:ignore reaping the fixture child the test spawned; its exit status is not the assertion
	t.Cleanup(func() { _ = child.Wait() })
	if err := child.Process.Kill(); err != nil {
		t.Fatalf("kill dead-holder fixture child %d: %v", pid, err)
	}
	// SIGKILL is delivered, but the child is not a zombie until it has run to
	// its death — wait for the state rather than for a duration, so the
	// fixture never t.Fatalf's for being a few microseconds early.
	deadline := time.Now().Add(retryDeadHolderWait)
	for {
		if retryHolderIsDead(pid) {
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatalf("dead-holder fixture child %d never became a zombie within %s — this host reaped it for us", pid, retryDeadHolderWait)
		}
		//stylegate:allow-sleep bounded poll interval: the loop's own deadline owns the outcome and exits on the first matching observation
		time.Sleep(retryDeadHolderPoll)
	}
}

// retryHolderIsDead reports whether the ledger's own liveness instrument calls
// pid DEAD — asked of the production predicate (tools.PidAlive, the ONE
// zombie-aware signal-0 probe) rather than a second /proc parse here.
//
// The zombie is what makes this true and what makes the pid HERMETIC: reaping
// is what frees a pid, so an unreaped corpse keeps its number reserved for the
// whole test, and the same predicate the verdict is built on already reads
// state 'Z' as dead (Rules 13/14: one implementation, asked — never mirrored).
func retryHolderIsDead(pid int) bool { return !tools.PidAlive(pid) }

// retrySeedDeadHolder rewrites the row's claim trail to that holder, and
// records the holder's IDENTITY the way production does.
//
// Both halves matter and they are the two faces of the same requirement: a
// dead pid makes the verdict `dead` (resumable), and a RECORDED identity keeps
// the verdict off the legacy name guard — which is what answered `running`
// when the row carried an empty triple.
//
// It stamps through tools.TaskStampHolder (Rule 14: the production writer, not
// a second implementation) rather than raw SQL, which buys two things: the
// identity is captured by the same code path every real claim uses, and a pid
// whose identity is unreadable is REFUSED by that writer instead of silently
// degrading to the shape that produced this bug.
func retrySeedDeadHolder(t *testing.T, taskID string, pid int) {
	t.Helper()
	host, herr := os.Hostname()
	if herr != nil {
		t.Fatalf("hostname: %v", herr)
	}
	if err := tools.TaskStampHolder(taskID, host+":"+strconv.Itoa(pid), pid); err != nil {
		t.Fatalf("seed dead holder (pid %d): %v", pid, err)
	}
	// Re-measured, never assumed (Rule 15): the gate must read the row as
	// RESUMABLE, or the fixture would silently test the refusal path instead
	// of the ladder.
	st, serr := tools.TaskProcessStatusFor(taskID)
	if serr != nil {
		t.Fatalf("fixture precondition: dead-holder row unreadable: %v", serr)
	}
	ok, verdict := tools.TaskRunChildResumable(taskID, os.Getpid())
	if !ok {
		t.Fatalf("fixture precondition: the dead-holder row must be resumable, got verdict %q", verdict)
	}
	// And the precondition must hold for the RIGHT reason: the verdict has to
	// rest on the recorded identity, not on the legacy comm-name guard that
	// the empty triple fell through to. Without this, a regression that drops
	// the identity would still pass the check above (a pid the host happens to
	// recycle onto a non-gopi process reads `recycled` — also resumable), and
	// the four tests would quietly walk a path they were not written for.
	if !st.IdentityRecorded {
		t.Fatalf("fixture precondition: the dead-holder row carries NO recorded identity (verdict %q) — the verdict came from the legacy name guard, not the identity the fixture must pin", verdict)
	}
}

// TestAbnormalChildRetryIsBoundedAndHandsOverOnce is acceptance ①: N abnormal
// exits spend exactly `cap` attempts, each recorded on the row's append-only
// trail, and the cap handover fires ONCE with the named reason and NO further
// spawn.
func TestAbnormalChildRetryIsBoundedAndHandsOverOnce(t *testing.T) {
	f := newRetryFixture(t, "t-retry-bounded")

	// Every abnormal exit the wrapper sees is the SAME call: the row is open
	// (nobody judged the work), so each pass must spend one attempt.
	for pass := 1; pass <= tools.TaskRunRetryCap; pass++ {
		f := f
		closedByChild, status, err := tools.TaskRowTerminalAt(f.row)
		if err != nil {
			t.Fatalf("pass %d: row status: %v", pass, err)
		}
		if closedByChild {
			t.Fatalf("pass %d: the fixture row must start OPEN (the abnormal shape), got status %q", pass, status)
		}
		taskRunAbnormalChildRetry(f.row, closedByChild, status, err)
		if f.spawns != pass {
			t.Fatalf("pass %d: spawns = %d, want %d — every abnormal exit before the cap must resume", pass, f.spawns, pass)
		}
		if got, cerr := tools.TaskRunRetryCounts(f.row); cerr != nil {
			t.Fatalf("pass %d: counter: %v", pass, cerr)
		} else if got != pass {
			t.Fatalf("pass %d: the ledger counts %d attempt(s), want %d — the trail must record each one", pass, got, pass)
		}
	}

	// The cap is now SPENT: the next abnormal exit must NOT resume; it must
	// hand over once, with the named reason, and leave the row failed.
	before := f.spawns
	closedByChild, status, err := tools.TaskRowTerminalAt(f.row)
	if err != nil {
		t.Fatalf("cap pass: row status: %v", err)
	}
	taskRunAbnormalChildRetry(f.row, closedByChild, status, err)
	if f.spawns != before {
		t.Fatalf("at the cap the ladder resumed again (spawns %d → %d) — an unbounded loop", before, f.spawns)
	}
	show, serr := tools.TaskShow(f.row)
	if serr != nil {
		t.Fatalf("show: %v", serr)
	}
	wantReason := tools.TaskRunRetryCapReason()
	if !strings.Contains(show, wantReason) {
		t.Fatalf("the cap handover must record its NAMED reason on the row.\nwant: %s\ngot:\n%s", wantReason, show)
	}
	if !strings.Contains(show, strconv.Itoa(tools.TaskRunRetryCap)) {
		t.Errorf("the handover reason must name the cap it enforced:\n%s", show)
	}
	// A second pass at the cap is idempotent: still no spawn, still one row.
	taskRunAbnormalChildRetry(f.row, closedByChild, status, err)
	if f.spawns != before {
		t.Fatalf("a second pass at the cap spawned again (%d) — the ladder must be terminal", f.spawns)
	}
}

// TestJudgedChildFailureIsNotRetried is acceptance ②: a child that CLOSED ITS
// OWN ROW before exiting non-zero judged its task — its failure is a result,
// not a crash — so the wrapper must not resume it.
func TestJudgedChildFailureIsNotRetried(t *testing.T) {
	f := newRetryFixture(t, "t-retry-judged")
	// The child's own close-out: it wrote the terminal row itself (the shape
	// every judging child leaves) BEFORE the wrapper's return ran.
	if _, err := tools.TaskUpdateWithResult(f.row, tools.TaskStatusFailed, "the child judged this task failed"); err != nil {
		t.Fatalf("child close-out: %v", err)
	}
	closedByChild, status, err := tools.TaskRowTerminalAt(f.row)
	if err != nil {
		t.Fatalf("row status: %v", err)
	}
	if !closedByChild || status != tools.TaskStatusFailed {
		t.Fatalf("fixture precondition: the row must read terminal failed, got closed=%v status=%q", closedByChild, status)
	}
	taskRunAbnormalChildRetry(f.row, closedByChild, status, err)
	if f.spawns != 0 {
		t.Fatalf("a judged failure was resumed (%d spawn(s)) — a verdict must never be retried", f.spawns)
	}
	if got, cerr := tools.TaskRunRetryCounts(f.row); cerr != nil {
		t.Fatalf("counter: %v", cerr)
	} else if got != 0 {
		t.Fatalf("a refused retry must not spend an attempt, got %d", got)
	}
}

// TestRetryCounterSurvivesARestart is acceptance ③: the cap lives in the
// ledger, not in the wrapper process — a FRESH wrapper (a new process, an empty
// in-memory state) continues the ladder from the count it reads back, and two
// more abnormal exits still end at the cap rather than restarting the ladder.
func TestRetryCounterSurvivesARestart(t *testing.T) {
	f := newRetryFixture(t, "t-retry-restart")

	// A first process spends two attempts…
	for pass := 0; pass < 2; pass++ {
		closedByChild, status, err := tools.TaskRowTerminalAt(f.row)
		if err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		taskRunAbnormalChildRetry(f.row, closedByChild, status, err)
	}
	if f.spawns != 2 {
		t.Fatalf("first process: spawns = %d, want 2", f.spawns)
	}

	// …and dies. A SECOND fixture — a new process image as far as the ledger
	// is concerned (nothing in memory is carried over) — reads the trail back
	// and must continue from 2, not from 0.
	restarted := &retryFixture{ledger: f.ledger, row: f.row}
	origSpawn := taskRunResumeSpawn
	t.Cleanup(func() { taskRunResumeSpawn = origSpawn })
	taskRunResumeSpawn = func(taskID, target string) (string, string, int, error) {
		restarted.spawns++
		return "gopi session resume " + target + " --task " + taskID, t.TempDir() + "/resume.log", os.Getpid(), nil
	}
	if spent, err := tools.TaskRunRetryCounts(f.row); err != nil {
		t.Fatalf("read back the trail: %v", err)
	} else if spent != 2 {
		t.Fatalf("the restarted process must read back 2 spent attempts, got %d", spent)
	}
	// Exactly ONE attempt is left in the ladder.
	closedByChild, status, err := tools.TaskRowTerminalAt(f.row)
	if err != nil {
		t.Fatalf("restart pass: %v", err)
	}
	taskRunAbnormalChildRetry(f.row, closedByChild, status, err)
	if restarted.spawns != 1 {
		t.Fatalf("the restarted ladder must spend its LAST attempt, got %d spawn(s)", restarted.spawns)
	}
	// And then hands over — the cap is a property of the ROW, not of a process.
	closedByChild, status, err = tools.TaskRowTerminalAt(f.row)
	if err != nil {
		t.Fatalf("cap pass: %v", err)
	}
	taskRunAbnormalChildRetry(f.row, closedByChild, status, err)
	if restarted.spawns != 1 {
		t.Fatalf("the restarted process outran the cap (spawns = %d) — the counter did not survive", restarted.spawns)
	}
	show, serr := tools.TaskShow(f.row)
	if serr != nil {
		t.Fatalf("show: %v", serr)
	}
	if !strings.Contains(show, tools.TaskRunRetryCapReason()) {
		t.Fatalf("the restarted process must reach the SAME cap handover:\n%s", show)
	}
}

// TestRetryRefusesWhenTheRowCannotBeRead: Rule 15 — an unreadable row is never
// promoted into a retry (the safe direction is "do not resume"), and the refusal
// is reported rather than silently skipped.
func TestRetryRefusesWhenTheRowCannotBeRead(t *testing.T) {
	newRetryLedger(t)
	f := &retryFixture{row: "t-retry-missing"}
	origSpawn := taskRunResumeSpawn
	t.Cleanup(func() { taskRunResumeSpawn = origSpawn })
	taskRunResumeSpawn = func(taskID, target string) (string, string, int, error) {
		f.spawns++
		return "", "", 0, nil
	}
	_, _, err := tools.TaskRowTerminalAt("t-retry-missing")
	if err == nil {
		t.Fatal("fixture precondition: a missing row must not resolve")
	}
	taskRunAbnormalChildRetry("t-retry-missing", false, "", err)
	if f.spawns != 0 {
		t.Fatalf("an unreadable row must never be resumed, got %d spawn(s)", f.spawns)
	}
}

// TestRetryRefusesWhenNoRunStarted: a spawn that produced no process (pid 0) is
// an exec failure, not the run's fault — it must NOT consume an attempt (the
// same contract the merger's dead-holder arm reads), so the ladder stays where
// it was and the next pass may still try.
func TestRetryRefusesWhenNoRunStarted(t *testing.T) {
	f := newRetryFixture(t, "t-retry-nostart")
	taskRunResumeSpawn = func(taskID, target string) (string, string, int, error) {
		return "", "", 0, os.ErrNotExist
	}
	closedByChild, status, err := tools.TaskRowTerminalAt(f.row)
	if err != nil {
		t.Fatalf("row status: %v", err)
	}
	taskRunAbnormalChildRetry(f.row, closedByChild, status, err)
	if got, cerr := tools.TaskRunRetryCounts(f.row); cerr != nil {
		t.Fatalf("counter: %v", cerr)
	} else if got != 0 {
		t.Fatalf("a spawn that started nothing must not consume an attempt, got %d", got)
	}
}
