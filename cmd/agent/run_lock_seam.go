package main

// THE RUN-SCOPE LOCK AT THE RUN FUNNEL'S ENTRY (epic run-lock-per-run-dir,
// 2026-11-05).
//
// What it is: before this process does anything at all with a run — before the
// singleton guard, before the claim CAS, before the session workdir move, before
// the child is spawned — it takes an exclusive, non-blocking KERNEL flock on
// <run-dir>/run.lock (internal/runlock). Two processes cannot execute the same
// run directory; the second one is refused AT THE LOCK and never reaches the
// ledger.
//
// Why the kernel and not a status column: the ledger's claimed/in_progress
// column was doing mutual exclusion, progress display and crash recovery at
// once, and it does the first one badly in three measured ways — it is written
// BEFORE the child is spawned (a crash in between leaves a row that says
// "running" and runs nothing), it is never cleared when the holder dies (a row
// stayed in_progress 8h38m after its process was gone), and clearing it needs a
// cleaner (a resident mechanism whose own failure is invisible). A flock has
// none of the three: the kernel holds it, the kernel releases it the instant the
// process ends — a clean exit, a panic, a `kill -9` — and nothing has to sweep.
//
// WHERE IT SITS, AND THE ONE ARM IT CANNOT PRECEDE. The lock is taken as EARLY
// as this process knows which run directory it is about to execute:
//
//   - an EXPLICIT row (`gopi task run <id>`, a worker's NAMED round): the id is
//     in hand at entry, so the lock is taken BEFORE the singleton guard and
//     BEFORE the claim CAS — a refused run leaves the ledger untouched.
//   - a WORKER round whose row was already claimed (`workerWriteback`): the
//     claim happened in the round's scan (workerRoundClaiming's CAS), i.e.
//     before this function is entered, so the id is not knowable any earlier
//     than its entry — the lock is taken at the first statement that has it.
//   - a run that POLLS for the next row (`target` empty): the CAS that hands
//     out the id is the same call that claims it, so the lock necessarily
//     follows that claim.
//
// The two arms after the first are honest limits of "lock before claim", not
// choices: the id they must lock is produced by the claim they would have to
// precede. What the lock still guarantees them is the property that matters —
// the run itself never executes twice, because the refusal happens before the
// session workdir move and before the child is spawned.
//
// WHAT IT IS NOT: it does not change any ledger transition (claimed ->
// in_progress keeps its exact timing), it adds no resident process and no sweep
// (Rule 35 — the kernel is the cleaner), and it is per RUN DIRECTORY, never a
// machine-wide singleton, so two different rows execute concurrently.

import (
	"fmt"
	"os"

	"github.com/nickhuang99/gopi/internal/runlock"
	"github.com/nickhuang99/gopi/internal/tools"
)

// taskRunLockDir resolves the run directory whose lock this process must hold.
// The directory is the SAME one the run's logs, report and attachments land in
// (tools.TaskSessionDir — Rule 14: one address, not a second derivation), so
// "the directory being executed" and "the directory being written" can never be
// two different answers.
func taskRunLockDir(target string) (string, error) {
	dir, err := tools.TaskSessionDir(target)
	if err != nil {
		return "", fmt.Errorf("%s: run lock: resolve the run directory of %s: %w", taskProgName, target, err)
	}
	return dir, nil
}

// runLockHeldDir is the run directory THIS PROCESS currently holds the lock on,
// or "" when it holds none. It is what lets two funnels in the same process —
// a worker ROUND, which locks before it enters the run directory, and the run
// path it then calls, which would otherwise lock a second time — agree instead
// of colliding.
//
// This is NOT re-entrancy. flock is per open file description, so a second
// open() in the same process WOULD be refused by its own first descriptor: the
// worker would deadlock against itself. The variable makes the hand-off
// explicit and keeps ONE held descriptor per process, which is the honest
// shape — a process is either executing a run directory or it is not.
var runLockHeldDir string

// taskRunLockHeldByThisProcess reports whether dir is already locked by this
// process (the worker-round hand-off above).
func taskRunLockHeldByThisProcess(dir string) bool {
	return dir != "" && runLockHeldDir == dir
}

// taskRunTakeLock takes the run-scope lock for target and returns the function
// that releases it. Every caller MUST defer the returned release, including on
// the paths that return an error afterwards: the lock is held by the open
// descriptor, and a process that keeps running (a worker looping over rounds)
// would otherwise hold the previous round's run directory for the rest of its
// life.
//
// A refusal is FATAL for the run and says why out loud — the lock path and the
// holder's stamp — because a refusal an operator cannot act on is the same as a
// silent failure (PROJECT.md Rule 15).
func taskRunTakeLock(target string) (func(), error) {
	dir, err := taskRunLockDir(target)
	if err != nil {
		return nil, err
	}
	return taskRunTakeLockDir(dir, taskProgName, func(inner error) error {
		return fmt.Errorf("%s: refusing to run %s: %w", taskProgName, target, inner)
	})
}

// taskRunTakeLockDir is the ONE lock body, keyed on the DIRECTORY rather than
// on a target string, and it takes the sentence that OUGHT TO SURROUND a
// refusal (`refuse`) so each face names its own verb.
//
// Two callers need it and they arrive at the directory two different ways:
//
//   - taskRunTakeLock above, for a run funnel that holds a task id and lets
//     tools.TaskSessionDir name the directory;
//   - the RESUME seam (run_lock_resume_seam.go), which holds a CHECKPOINT and
//     whose directory is filepath.Dir(rawLogPath) — the directory the session
//     files are actually appended to.
//
// The second one may not go through tools.TaskSessionDir: that derivation is
// GopiDir-based, so on a profile whose sessions root is RELOCATED
// (config.SessionsRootEnv — `gopi chat --replay`) it names a DIFFERENT
// directory than the checkpoint lives in, and runlock.Lock would then
// MkdirAll and lock a directory that holds nothing. A lock taken beside the
// run instead of on it excludes nobody. So the body takes the directory it is
// given, and the addressing decision stays with each caller.
//
// The hand-off, the announcement and the release live HERE, once, so the two
// faces cannot behave differently about a directory this process already holds.
func taskRunTakeLockDir(dir, label string, refuse func(error) error) (func(), error) {
	if taskRunLockHeldByThisProcess(dir) {
		// The worker round already took this directory's lock and is about to
		// hand the row to this funnel: releasing it here would free the
		// directory mid-run, so the hand-off returns a no-op and the OUTER
		// holder keeps ownership of the descriptor.
		return func() {}, nil
	}
	h, err := runlock.Lock(dir)
	if err != nil {
		return nil, refuse(err)
	}
	runLockHeldDir = dir
	// Announce the held lock on the round's own stream: the line names the path
	// an operator (or a test) can stat, and it is the evidence that this
	// process, and not another, is the one executing the directory. `label` is
	// the CALLER's own prog name, so the line names the face the operator
	// actually typed — `gopi worker` on a run funnel, `gopi session resume` on
	// the resume seam — while the behaviour behind it stays one implementation.
	// The wording says WHO holds it, and that nobody else does: a bare "run lock
	// held" is the same shape as runlock.ErrHeld's refusal ("already locked by
	// another process"), and has been read as "another process is on this
	// directory" while the pid printed right beside it is the CALLER's own.
	fmt.Fprintf(os.Stderr, "%s: run lock held by THIS process: %s (pid %d) — no other process is executing this directory\n", label, h.Path(), os.Getpid())
	return func() {
		if rerr := h.Release(); rerr != nil {
			// Never silent, never fatal: the lock is already gone with the
			// descriptor's close, and the run's outcome must not change because
			// tidying was noisy.
			fmt.Fprintf(os.Stderr, "%s: run lock release: %v\n", label, rerr)
		}
		if runLockHeldDir == dir {
			runLockHeldDir = ""
		}
	}, nil
}
