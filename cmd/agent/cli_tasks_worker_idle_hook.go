package main

// cli_tasks_worker_idle_hook.go — the worker loop's drained-verdict
// synchronization hook (test seam).
//
// WHY IT EXISTS: the idle-exit tests must seed a task *between two drained
// rounds* — verdict ① has banked and its "(N/M idle rounds)" line has been
// printed, and only then does the busy task appear so the NEXT round claims
// it. Seeding that with a wall-clock sleep is a race: under load the claim
// scan runs later than the sleep assumed, the task is claimed in the very
// round whose verdict never banked, and the pre-task count line is never
// printed (TestWorkerBusyRoundResetsIdleStreak flaked ~1-in-5 on both
// b684656 and 3ce88c2 — pre-existing, not a product bug).
//
// The hook removes the guesswork: runTaskWorker announces each drained
// verdict through notifyWorkerIdleDrain the INSTANT the wait line is
// printed, so a test's observer runs inside the loop goroutine — the loop
// is synchronously blocked while the observer seeds its row, which means
// the row is committed before the next claim scan can possibly run. No
// sleep, no ordering assumption, no wall clock.
//
// Production impact is one nil-slice range: with no observer armed (every
// non-test run) notifyWorkerIdleDrain does nothing.

// workerIdleDrainHook observes ONE drained verdict: round is the 1-based
// loop round the verdict belongs to, streak the consecutive-empty-round
// count it just produced (the "(streak/idleExits idle rounds)" number).
type workerIdleDrainHook func(round, streak int)

// workerIdleDrainHooks holds the observers armed for this process; empty in
// production (only tests arm it, via armWorkerIdleDrainHook).
var workerIdleDrainHooks []workerIdleDrainHook

// notifyWorkerIdleDrain announces a drained verdict to every armed observer
// — called by runTaskWorker immediately after printing the wait line, so an
// observer can never run before that line exists. No-op when none is armed;
// errors are the observer's own business (it holds its *testing.T), so the
// loop never blocks on anything but the observer's own work.
func notifyWorkerIdleDrain(round, streak int) {
	for _, hook := range workerIdleDrainHooks {
		hook(round, streak)
	}
}
