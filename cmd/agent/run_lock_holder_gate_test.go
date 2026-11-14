// THE HOLDER GATE READS THE KERNEL, NOT THE PID (epic resume-gate-is-the-lock,
// 2026-11-05).
//
// THE DEFECT THIS FILE PINS. `gopi session resume <task-id>` answered "is this
// run being executed?" from the ledger's process identity: pid + start ticks +
// cmdline, resolved against /proc. That triple answers whether a process with
// that identity EXISTS, which is not the question. Measured on this host, the
// same evening, the verbatim same refusal for two opposite situations: a holder
// 5h18m old with 24s of CPU and 36 threads parked in futex (zero progress —
// refusing it was WRONG) and a holder 2m25s into a live round (refusing it was
// RIGHT).
//
// THE FIX, and what these arms measure. The run-scope kernel flock is taken
// FIRST, and it is the arbiter: the kernel grants that lock to one process, so
// HOLDING it IS "nothing is executing this run directory" — and it is released
// the instant a holder dies, SIGKILL included, with no sweep to trust. A LIVE
// ledger claim then becomes an override that SAYS SO; with no lock taken the
// claim keeps its refusal (the absence of a measurement is never an admission).
//
// CRITERION ② IS A HARD CONSTRAINT AND IS PINNED HERE: the gate may not be
// widened by deleting the guard. A second process that does NOT hold the
// directory is refused by the kernel before it ever reaches the fold, so the
// override can only ever fire for a caller that actually holds it.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tools"
)

// resumeHolderGateRowID is the row this file's gate fixture claims, and the
// basename of the run directory the same fixture locks. They are the SAME id on
// purpose: the lock's address is the checkpoint's own run directory, and the
// run directory of a row is named after that row (internal/scratch's minted
// session-container name). A fixture that used two different ids would measure
// the fold against a directory that belongs to nobody.
const resumeHolderGateRowID = "01a1ac12-0000-7b63-82b0-0deadbeef001"

// seedResumeHolderGateRow claims and starts a row whose recorded holder is THIS
// process — the "verified-LIVE holder" shape the gate has to arbitrate. The
// holder must be a REAL incarnation (host + pid + start ticks + cmdline), or
// the ladder reports unknown/foreign and the fold under test never runs.
func seedResumeHolderGateRow(t *testing.T, taskID string) {
	t.Helper()
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "resume holder gate fixture " + taskID,
		Payload: "resume holder gate fixture",
		TaskID:  taskID,
		Source:  "human",
	}); err != nil {
		t.Fatalf("submit %s: %v", taskID, err)
	}
	host, err := os.Hostname()
	if err != nil {
		t.Skipf("no hostname: %v", err)
	}
	holder := host + ":" + strconv.Itoa(os.Getpid())
	if err := tools.TaskClaimExplicit(taskID, holder); err != nil {
		t.Fatalf("claim %s as %s: %v", taskID, holder, err)
	}
	if _, err := tools.TaskStart(taskID, holder); err != nil {
		t.Fatalf("start %s as %s: %v", taskID, holder, err)
	}
}

// TestStaleHolderIsAdmittedWhenTheLockArbitrates is the epic's headline arm:
// the ledger names a LIVE pid, the run directory is NOT being executed, this
// process takes the lock, and the resume is ADMITTED — with the overridden pid
// announced, never silently.
func TestStaleHolderIsAdmittedWhenTheLockArbitrates(t *testing.T) {
	// The ledger and the sessions root are both private to this test (Rule 32:
	// the DB path comes from the scaffold, never from the operator's profile).
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	fx := newResumeSeamFixture(t)
	seedResumeHolderGateRow(t, resumeHolderGateRowID)

	// The holder IS live: the gate's raw answer is the CLAIM, not a refusal.
	if got := decideResumeByTaskID(resumeHolderGateRowID); got != resumeGateHolderLive {
		t.Fatalf("raw gate = %d, want resumeGateHolderLive for a verified-live holder", got)
	}

	// Nothing has arbitrated the directory yet in this process, so the claim
	// still stands as a refusal — this is criterion ② stated as an assertion
	// (admitting on the ledger alone would run a live row twice).
	if got := resumeDecideWithLock(resumeHolderGateRowID, false); got != resumeGateRefuseRunning {
		t.Fatalf("unarbitrated gate = %d, want resumeGateRefuseRunning", got)
	}

	// THE MEASUREMENT: take the real run-scope lock. The kernel grants it
	// because nothing else executes this directory, and the seam reports that
	// it arbitrated.
	rel, arbitrated, err := resumeTakeRunLock(resumeProgName, fx.rawLog)
	if err != nil {
		t.Fatalf("resumeTakeRunLock on an unexecuted run directory: %v", err)
	}
	defer rel()
	if !arbitrated {
		t.Fatal("the seam took the lock but reports arbitrated=false — the fold below would keep a refusal the kernel disproved")
	}

	if got := resumeDecideWithLock(resumeHolderGateRowID, arbitrated); got != resumeGateHolderLive {
		t.Fatalf("arbitrated gate = %d, want resumeGateHolderLive (pid alive, directory not executed)", got)
	}

	// THE ADMISSION IS LOUD (Rule 15): the operator must be able to see WHICH
	// pid was overridden and on what evidence, or an admitted-by-lock resume is
	// indistinguishable from one whose holder was simply dead.
	var out bytes.Buffer
	resumeReportHolderOverride(&out, resumeProgName, resumeHolderGateRowID)
	msg := out.String()
	if msg == "" {
		t.Fatal("the holder override printed nothing — an admission that overrides a LIVE holder must say so")
	}
	if !strings.Contains(msg, strconv.Itoa(os.Getpid())) {
		t.Errorf("the override notice does not name the overridden pid %d:\n%s", os.Getpid(), msg)
	}
	if !strings.Contains(msg, "run-scope lock") {
		t.Errorf("the override notice does not name the evidence (the run-scope lock):\n%s", msg)
	}
}

// TestSecondConcurrentResumeIsRefusedByTheLock is CRITERION ②, measured with
// real flock semantics across a real process boundary: a holder that owns the
// run directory makes the resume REFUSE, naming the lock. This is what keeps
// the override above from being a widening of the gate.
func TestSecondConcurrentResumeIsRefusedByTheLock(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	fx := newResumeSeamFixture(t)

	// A separate process takes the lock and holds it (the same probe the seam's
	// own arms use — no fake, no in-process hand-off).
	holder := startResumeLockHolder(t, fx.dir)
	if holder.Process == nil {
		t.Fatal("the holder probe did not start")
	}

	// The resume face's own entry point must refuse: this is the call
	// resumeSession makes before anything else.
	if _, arbitrated, err := resumeTakeRunLock(resumeProgName, fx.rawLog); err == nil {
		t.Fatalf("resumeTakeRunLock succeeded while another process holds the run directory — a concurrent resume would write the same session twice (arbitrated=%v)", arbitrated)
	} else if !strings.Contains(err.Error(), "run.lock") {
		t.Errorf("the refusal does not name the lock file it lost:\n%v", err)
	}

	// And the holder's death releases it with NO cleanup step — the whole
	// reason the kernel, not a sweep, is allowed to arbitrate.
	if err := holder.Process.Kill(); err != nil {
		t.Fatalf("kill the holding probe: %v", err)
	}
	if _, werr := holder.Process.Wait(); werr != nil {
		t.Logf("wait for the killed holder: %v (its exit status is not the assertion)", werr)
	}
	rel, arbitrated, err := resumeTakeRunLock(resumeProgName, fx.rawLog)
	if err != nil {
		t.Fatalf("the run directory is still locked after the holder died — the override would then depend on a stale-lock sweep: %v", err)
	}
	defer rel()
	if !arbitrated {
		t.Error("the seam took the lock after the holder died but reports arbitrated=false")
	}
}

// TestLiveHolderStillRefusedWithoutALock pins the OTHER half of the fold, and
// it is the half that costs a defect if it is wrong: when the checkpoint is not
// in a run directory of this profile, NO lock can be taken, the kernel said
// nothing, and a LIVE claim must keep its refusal (Rule 15: an unknown is never
// resolved in the admitting direction).
func TestLiveHolderStillRefusedWithoutALock(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	fx := newResumeSeamFixture(t)
	seedResumeHolderGateRow(t, resumeHolderGateRowID)

	// A checkpoint OUTSIDE the sessions root the fixture pinned: the seam's own
	// notice path, i.e. the shape where nothing arbitrates.
	outside := filepath.Join(t.TempDir(), resumeHolderGateRowID+".raw.jsonl")
	if strings.HasPrefix(outside, fx.root) {
		t.Fatalf("the fixture put the outside checkpoint INSIDE the sessions root (%s) — this arm would then measure the lockable shape", outside)
	}
	if err := os.WriteFile(outside, []byte("→ REQUEST\n{}\n"), 0o644); err != nil {
		t.Fatalf("write the outside checkpoint: %v", err)
	}
	rel, arbitrated, err := resumeTakeRunLock(resumeProgName, outside)
	if err != nil {
		// A hard error is also an honest answer (it refuses), but the notice
		// path is the one this arm wants to characterise.
		t.Fatalf("resumeTakeRunLock on an outside checkpoint: %v", err)
	}
	defer rel()
	if arbitrated {
		t.Fatal("the seam reports arbitrated=true for a checkpoint outside any run directory — it took no lock")
	}
	if got := resumeDecideWithLock(resumeHolderGateRowID, arbitrated); got != resumeGateRefuseRunning {
		t.Fatalf("gate = %d, want resumeGateRefuseRunning — with no lock the LIVE claim must stand", got)
	}
}
