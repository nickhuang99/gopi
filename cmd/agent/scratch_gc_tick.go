// The scratch sweep's RESIDENT trigger.
//
// WHY IT LIVES IN THE WORKER/MERGER TICK AND NOT IN A NEW DAEMON: the sweep
// bounds <home>/workspace/gopi-test-scratch, which only suites write. Until
// now it ran INLINE IN THE TEST PIN (internal/testutil.PinSuiteTempRoot), so a
// host that stopped running suites kept every root its killed suites abandoned
// — and the standalone hook meant to cover that case
// (GOPI_TEST_SCRATCH_GC_MIN) had NO READER whatsoever: a hook with no consumer.
//
// THE TRIGGER IS A LIVE OPERATOR VERB, NOT A RESIDENT TICK (corrected
// 2026-10-30, task 01a18ef7). This header used to say the sweep rides "the
// throttled lifecycle passes" that `gopi task worker` and `gopi merger` "already
// run". MEASURED, and every part of that sentence was false:
//
//	`grep -rn "scratchGCSweep(\|scratchGCSweepAsync(" --include=*.go . | grep -v _test.go`
//	  → cmd/agent/scratch_gc_tick.go:430 (the definition)
//	    cmd/agent/cli_scratch_gc.go:853 (the operator verb's --tick arm)
//	    cmd/agent/scratch_gc_tick.go:494 (the async definition, called by nobody
//	                                     in production — see below)
//
// so NOTHING resident fires this sweep. The task-lifecycle pass it compares
// itself to was deleted the same day (task_lifecycle_tick.go carries its
// per-leg verdicts), and the worker round's tick call went in 187d05c. What
// DOES reach scratchGCTick today is `gopi tool scratch-gc --tick`, whose arm
// documents itself as exactly that one-shot face, plus the test pin's own GC —
// which is the same coverage the sweep had before the resident trigger was
// ever claimed.
//
// NOTE FOR THE NEXT READER: `scratchGCSweepAsync` (the off-the-caller's-
// goroutine face) and its single-flight latch now have NO production caller at
// all; the comment at its definition still says "the face the worker's claim
// loop uses". That is a separate duty's cleanup, filed as its own bug row
// rather than done here (this task's scope is the task-lifecycle pass).
//
// Two properties are non-negotiable, because this runs inside a task-executing
// daemon:
//
//  1. FAIL-OPEN. Every failure mode of the sweep (no scratch home, an
//     unreadable entry, a held lock, a /proc we may not read) is LOGGED and the
//     tick continues. A disk-hygiene sweep must never be able to fail the host
//     task — the worst acceptable outcome is "stale scratch survived one more
//     tick".
//  2. THROTTLED, and OFF unless configured. GOPI_TEST_SCRATCH_GC_MIN is the
//     cadence in minutes (the env's historical semantics, reused verbatim —
//     it was documented as "every N minutes" when it had no reader); 0 or a
//     negative value disables the resident sweep entirely, as does the
//     settings.json switch scratch_gc (false). The default is
//     scratchGCDefaultIntervalMin.
package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/shot"
)

const (
	// scratchGCDefaultIntervalMin is the resident sweep's default cadence, in
	// minutes. 60 is chosen against the two clocks that matter:
	//
	//   - the sweep's own stale WINDOW (scratch.DefaultStaleAge = 6h, or the
	//     operator's value: scratch.ResolveStaleAge). A cadence an order of
	//     magnitude finer than
	//     orphan is collected within ~1/6 of its own staleness window — the
	//     tree's steady-state size is bounded by ~1h of abandoned scratch, not
	//     by ~6h of it.
	//   - the sibling per-process pass clock that rides the same tick
	//     (the merger's --every default = 5min; the pre-2026-11-05 sibling
	//     taskListSweepThrottleMin = 10min went with the stale sweep).
	//     An hourly sweep is the COARSEST useful cadence on those
	//     ticks: it costs one ReadDir of a directory holding tens of entries,
	//     which is noise next to the merge/build work a tick already does, and
	//     it is frequent enough that a host which merely keeps a worker alive
	//     never accumulates a morning's leftovers.
	//
	// A faster cadence buys nothing (the threshold is hours away) and a slower
	// one lets a busy suite host double its steady-state scratch.
	scratchGCDefaultIntervalMin = 60

	// scratchGCSettingsKey is the settings.json switch that turns the resident
	// sweep off without unsetting any environment variable
	// (`"scratch_gc": false`). Config + env, as the contract requires: the env
	// is the operator's shell-level override, the settings file is the
	// deployment's durable switch.
	scratchGCSettingsKey = "scratch_gc"

	// scratchGCRoleLabel names the sweep in the tick's log lines, so an
	// operator reading a worker/merger log knows which resident component
	// swept (both call the same one function).
	scratchGCRoleLabel = "scratch-gc"

	// workerRoleLabel / mergerRoleLabel name the two EXISTING resident
	// components that carry the sweep — the answer to "no new daemon" is
	// readable in the logs: the sweep's line is bracketed by the component
	// whose tick fired it.
	workerRoleLabel = "worker"
	mergerRoleLabel = "merger"

	// scratchGCSweepSlowWarn is the threshold past which an ASYNCHRONOUS sweep
	// reports its own duration. It is a REPORTING threshold, not a limit: the
	// sweep is off the claim loop, so a slow one costs disk-hygiene latency and
	// nothing else — but a cost that is never printed is how the 12-18s scan
	// stayed invisible while it ate operator stop budgets (Rule 15).
	scratchGCSweepSlowWarn = 5 * time.Second
)

// scratchGCSweepState is the single-flight latch for scratchGCSweepAsync: one
// sweep at a time per process, so a tick arriving mid-sweep is a no-op rather
// than a second concurrent /proc scan.
//
// `done` is the IN-FLIGHT FLIGHT'S handle, and it is what makes the asynchronous
// face JOINABLE: it is non-nil exactly while a sweep goroutine exists, and that
// goroutine closes it as its last statement. See scratchGCSweepInFlightDone /
// joinScratchGCSweepFlight for the two reasons a caller needs it (the regression
// test's join, and the suite close-out's "measure a process whose instruments
// have stopped, never one mid-flight").
var scratchGCSweepState struct {
	mu      sync.Mutex
	running bool
	done    chan struct{}
}

// resetScratchGCSweepFlight returns the single-flight latch to its IDLE shape:
// no sweep running and no flight left to join. `done` must be cleared together
// with `running`, or a later joiner would wait on a stale (possibly closed)
// channel and believe it had joined a flight it never saw.
//
// It exists for TESTS, which drive the tick in-process and must not inherit a
// sibling case's flight (a sibling may have left one in flight on purpose);
// production never needs it, because a CLI daemon is one loop.
//
// It does NOT cancel a sweep that is already running — nothing can end another
// goroutine's work — it only stops PUBLISHING it to a new joiner. That is safe
// because each flight closes its own handle (scratchGCSweepAsync), so clearing
// this field can never make a completing goroutine close a nil channel.
func resetScratchGCSweepFlight() {
	scratchGCSweepState.mu.Lock()
	scratchGCSweepState.running = false
	scratchGCSweepState.done = nil
	scratchGCSweepState.mu.Unlock()
}

// scratchGCSweepInFlightDone returns the in-flight sweep's completion channel,
// or nil when no sweep is running. It is the OBSERVABLE half of the join: a
// caller that must not sample the process mid-sweep (the suite's close-out
// goroutine sweep) selects on it with its own bound instead of blocking, so a
// wedged sweep can never hang the measurement — it is reported as parked, which
// is a defect and stays one.
func scratchGCSweepInFlightDone() <-chan struct{} {
	scratchGCSweepState.mu.Lock()
	defer scratchGCSweepState.mu.Unlock()
	return scratchGCSweepState.done
}

// joinScratchGCSweepFlight blocks until the sweep in flight (if any) has
// FINISHED — the goroutine's last statement, not the moment its body returned.
// It is idempotent and safe from any goroutine, and it returns immediately when
// nothing is in flight (including when it is called twice).
func joinScratchGCSweepFlight() {
	if done := scratchGCSweepInFlightDone(); done != nil {
		<-done
	}
}

// scratchGCLastTick is the process-wide clock for the sweep's throttle (a
// CLI daemon is one loop, so the clock is per-process — the same shape
// taskListSweepLastTick uses). It used to name taskLifecycleLastTick beside it;
// that clock was deleted with the task-lifecycle pass, 2026-10-30.
var scratchGCLastTick time.Time

// scratchGCTick is the resident pass: it does nothing at all unless the
// cadence says it is due. Its callers are MEASURED, not assumed (2026-10-30):
// the `--tick` arm of `gopi tool scratch-gc` (cli_scratch_gc.go), which is the
// ONE production door, and the tests that drive the throttle directly. It used
// to be fired by the worker's round and the merger's tick — both of those calls
// are gone (worker round: 187d05c; merger tick: deleted 2026-09-19; the
// task-lifecycle pass that claimed to carry it: deleted 2026-10-30, task
// 01a18ef7), so no resident component sweeps scratch today. See this file's
// header for the measurement.
// It returns the number of entries it removed (for
// the caller's log/tests); errors are swallowed BY DESIGN, after being
// reported — see the fail-open note above (Rule 15: no SILENT failure; the
// failure is loudly logged, the host task is never affected).
func scratchGCTick(role string, now time.Time) int {
	interval, ok := scratchGCInterval()
	if !ok {
		return 0 // disabled by env or settings: never sweep, never even stat
	}
	if !scratchGCLastTick.IsZero() && now.Sub(scratchGCLastTick) < interval {
		return 0 // not due yet (the throttle is what keeps a busy tick cheap)
	}
	scratchGCLastTick = now
	home, err := scratch.EnsureHomeDir()
	if err != nil {
		// A host that never ran a suite (or a read-only home) is the expected
		// non-event: report once per tick, never fail anything.
		fmt.Fprintf(os.Stderr, "[%s] [%s] scratch home unavailable: %v\n", role, scratchGCRoleLabel, err)
		return 0
	}
	removed, lockVerdict, err := scratch.SweepHomeDetailed(home, scratch.ResolveStaleAge())
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] [%s] sweep of %s failed (host task unaffected): %v\n",
			role, scratchGCRoleLabel, home, err)
		return removed
	}
	// THE LOCK EVIDENCE. A resident pass that deferred to another GC (or broke a
	// crashed GC's abandoned lock) used to be indistinguishable from a pass that
	// found nothing: neither printed anything, and the sweep ran again next tick
	// with the same silent outcome. See cmd/agent's scratchGCLockLogFmt and
	// internal/scratch/gc_lock.go.
	reportResidentScratchGCLock(role, home, lockVerdict)
	if removed > 0 {
		fmt.Printf("[%s] [%s] removed %d stale scratch entr%s older than %s from %s\n",
			role, scratchGCRoleLabel, removed, scratchGCPlural(removed), scratch.ResolveStaleAge(), home)
	}
	// The CAPTURE UNIT arm rides the same tick, the same cadence and the same
	// off switch, and it is deliberately placed BEFORE the other arms: each of
	// them can return early on its own failure, and an orphaned chrome must not
	// depend on an unrelated tree being sweepable. The reclamation is
	// mechanical and policy-free here (internal/scratch decides which units are
	// reclaimable; internal/shot owns the one teardown argv), and fail-open like
	// everything else on this tick.
	shot.ReclaimOrphanedUnitsReporting(role)
	// The memory-BASE arm rides the same tick (the same one cadence, the same
	// off switch). It is the residue the home sweep can NEVER see: an agent
	// session's ad-hoc directory created straight in the tmpfs base
	// (gopi-verify-*/gocache-*/probe-*) has no collector otherwise — measured
	// at 2.6 GB / 702 entries filling /dev/shm to 100% and breaking builds
	// (devdoc/scratch-base-residue.md). The base is HOST configuration, so it
	// is resolved here (ONE reader, scratch.ResolveBase) rather than assumed.
	baseRoot := scratch.ResolveBase()
	// The SUITE-RUN family is REPORTED, never deleted, from here — and the
	// comment that used to sit below this call ("The resident tick does NOT
	// delete this family") is now what the code actually does. It did not use to
	// be: this line called scratch.SweepBaseSuiteRuns, the EXPLICIT arm, which
	// never consults the automatic stand-down and calls removeCollectable
	// unconditionally, so every resident tick deleted the aged per-run suite
	// roots of the SHARED memory base — the roots of the suites running on this
	// host, and the family base_suite_runs.go reserves for the operator's
	// one-shot `gopi tool scratch-gc base`. Measured on this host 2026-10-01: 36
	// `suite-*` roots already older than 30 minutes, 29 older than two hours, out
	// of 54 — so a 60-minute tick with a 6h stale window meets a non-empty victim
	// set every time it fires. Nothing on the machine said so, because the tick
	// had been eating the count along with the roots — which is also why the
	// 2026-09-28 leak (146 roots / 5.9G of a 7.8G tmpfs) can re-form invisibly
	// behind the deletion.
	//
	// The report is the cheap half and the actionable half stays with the
	// operator. Both counts are printed (Rule 15: a count that was taken and
	// then discarded is a failure that got quieter as it was measured), and the
	// total the tick returns includes neither — nothing of this family is
	// removed here.
	autoNow, suiteKept, suiteFresh, suiteErr := scratch.BaseSuiteRunReport(baseRoot, scratch.ResolveStaleAge())
	if suiteErr == nil && autoNow+suiteKept+suiteFresh > 0 {
		fmt.Printf("[%s] [%s] memory base %s holds %d suite run root(s) (%d stale+unheld, %d held by a live process, "+
			"%d inside the %s window) — reclaim with: gopi %s %s\n", role, scratchGCRoleLabel, baseRoot,
			autoNow+suiteKept+suiteFresh, autoNow, suiteKept, suiteFresh, scratch.ResolveStaleAge(), CliVerbScratchGC, scratchGCBaseFlag)
	} else if suiteErr != nil {
		// Fail-open like every other arm: an unreadable base or one this process
		// must not sweep is LOGGED and the tick continues.
		fmt.Fprintf(os.Stderr, "[%s] [%s] memory base suite-run report skipped (host task unaffected): %v\n",
			role, scratchGCRoleLabel, suiteErr)
	}
	baseRemoved, baseErr := scratch.SweepBase(baseRoot, scratch.ResolveStaleAge())
	if baseErr != nil {
		// Fail-open like the home arm: a base this process must not sweep (a
		// base on `/`, an unreadable one) is LOGGED and the tick continues —
		// the host task is never affected.
		fmt.Fprintf(os.Stderr, "[%s] [%s] memory base sweep skipped (host task unaffected): %v\n",
			role, scratchGCRoleLabel, baseErr)
		return removed
	}
	if baseRemoved > 0 {
		fmt.Printf("[%s] [%s] removed %d stale base residue entr%s older than %s from %s\n",
			role, scratchGCRoleLabel, baseRemoved, scratchGCPlural(baseRemoved), scratch.ResolveStaleAge(), scratch.ResolveBase())
	}
	// The DECLARED-TREES arm rides the same tick, the same cadence and the same
	// off switch — and its ABSENCE is the defect this call site closes.
	// MEASURED (host-b, 2026-10-17): `/home` at 100%, a `git worktree
	// add` inside cmd/agent's own suite dying with ENOSPC, while `gopi tool
	// scratch-gc trees --dry-run` reported 45.7 GB collectable in the shared
	// build cache, 5.4 GB of aged `*.raw.log`, 1.27 GB of abandoned `go-build*`
	// and 8.1 GB held by the test-budget tree. Every byte of it was
	// age-and-holder vetted; none of it was ever collected, because the declared
	// set had no caller outside the CLI verb, and the host-level watchdog
	// written to drive that verb (`scripts/scratch-gc-watchdog.sh`) is not
	// installed on this host at all. The predicate lives in internal/scratch
	// (SweepDeclaredTreesAutomatic), so this call site decides nothing: it
	// renders one line per tree that gave something back and reports a row that
	// could not be walked ONCE, without failing the host task.
	//
	// Placement is deliberate: BEFORE the sandbox arm, because the arms below
	// this point can return early on their own refusal, and a declared tree must
	// not be left unbounded because an unrelated sandbox root was misconfigured.
	declaredRemoved := reportResidentDeclaredTrees(role, declaredTreesSweepBody())
	// The PROFILE-ROOT arm rides the same tick, the same cadence and the same
	// off switch — and it has NO deleting face here. It runs the OBSERVATION
	// only (scratch.ProfileRootReport): a direct child of the gopi profile root
	// that no registry row and no residue family explains is REPORTED by name,
	// and nothing on this line removes anything.
	//
	// WHY REPORT-ONLY IS THE WHOLE DESIGN AND NOT A MISSING FEATURE: the profile
	// root holds the live ledger, the settings and the session history, and the
	// measured precedent for an automatic deleter here is bad — a resident tick
	// deleted a shared family its own comment declared off-limits, on a machine
	// where nobody could see it happen (base_suite_runs.go, memory #7096). The
	// reclaimable half (the hand-made per-run evidence family) is reachable from
	// exactly one place, the operator's `gopi tool scratch-gc profile`, so the deletion
	// an operator vetted is the deletion that happens. The undeclared children
	// are printed in full: this line is the ONLY thing on the machine that can
	// say "a directory appeared under the profile root and nobody declared it",
	// which is what four hand-made evidence directories (684 MB) sat behind for
	// ten days.
	//
	// Fail-open like every sibling: an unreadable root (or a host with no
	// profile yet) is LOGGED and the tick continues.
	profileRoot, profileErr := scratch.ResolveProfileRoot()
	switch {
	case profileErr != nil:
		fmt.Fprintf(os.Stderr, "[%s] [%s] profile-root report skipped (host task unaffected): %v\n",
			role, scratchGCRoleLabel, profileErr)
	default:
		if verdicts, verr := scratch.ProfileRootReport(profileRoot, scratch.ResolveStaleAge()); verr != nil {
			if !errors.Is(verr, scratch.ErrProfileRootMissing) {
				fmt.Fprintf(os.Stderr, "[%s] [%s] profile-root report skipped (host task unaffected): %v\n",
					role, scratchGCRoleLabel, verr)
			}
		} else if counts := scratch.TallyProfileRoot(verdicts); counts.Undeclared > 0 {
			fmt.Printf("[%s] [%s] profile root %s holds %d undeclared child(ren) of %d: %s — report: gopi %s %s\n",
				role, scratchGCRoleLabel, profileRoot, counts.Undeclared, counts.Children,
				strings.Join(scratchGCProfileUndeclaredNames(verdicts), ", "), CliVerbScratchGC, scratchGCProfileFlag)
		}
	}
	// The AGENT SANDBOX arm rides the same tick and the same off switch. It has
	// to: a sandbox is created on hosts that run neither a suite nor a suite's
	// cleanup, so `t.Cleanup`-style reclamation cannot be relied on (it never
	// runs on SIGTERM/SIGKILL — the whole reason this arm exists). Riding the
	// EXISTING tick adds zero resident processes; the predicate and the
	// conservative liveness rule live in internal/scratch (SweepSandboxes), so
	// this call site decides nothing.
	//
	// Fail-open like its siblings: a root the operator has not configured, an
	// unreadable one, or one that is refused (the filesystem root, the user's
	// home, the gopi profile) is LOGGED and the tick continues.
	sandboxRoot, sandboxSource := sandboxRootForSweep()
	if sErr := scratch.SandboxRootRefusal(sandboxRoot); sErr != nil {
		fmt.Fprintf(os.Stderr, "[%s] [%s] sandbox sweep skipped (host task unaffected): %v\n",
			role, scratchGCRoleLabel, sErr)
		return removed + baseRemoved + declaredRemoved
	}
	sandboxRemoved, sandboxBytes, sandboxLock, sErr := scratch.SweepSandboxesDetailed(sandboxRoot, scratch.SandboxStaleAge)
	if sErr != nil {
		fmt.Fprintf(os.Stderr, "[%s] [%s] sandbox sweep of %s failed (host task unaffected): %v\n",
			role, scratchGCRoleLabel, sandboxRoot, sErr)
		return removed + baseRemoved + declaredRemoved
	}
	reportResidentScratchGCLock(role, sandboxRoot, sandboxLock)
	if sandboxRemoved > 0 {
		fmt.Printf("[%s] [%s] removed %d abandoned sandbox(es) older than %s from %s (bytes=%d, root %s)\n",
			role, scratchGCRoleLabel, sandboxRemoved, scratch.SandboxStaleAge, sandboxRoot, sandboxBytes, sandboxSource)
	}
	// The STOP-RECEIPT arm rides the same tick, the same cadence and the same
	// off switch. It is the reclaim half of the ACK_<pid> channel: the sender
	// that read a receipt ARCHIVES it (cmd/agent/cli_worker_ack.go) and nothing
	// else ever will — a sender that dies before reading leaves one standing
	// forever. Both paths are ages of ONE rule there (an unread receipt is
	// archived; an archived one is collected), so this call site decides
	// nothing: it is placed LAST because every arm above can return early on
	// its own refusal, and a bounded control directory must not depend on an
	// unrelated sandbox root being configured.
	//
	// No new scheduler and no new switch (Rule 35): this is one ReadDir of a
	// directory holding a handful of small files, on a tick that already
	// exists, under the off switch that already exists.
	sweepWorkerAckArchiveQuietly(role, time.Now())
	// The FIXTURE-ROW RECLAIM arm rides the same tick, the same cadence and the
	// same off switch (task_fixture_reclaim_tick.go carries the reasoning and
	// the Rule 35 accounting). It is placed LAST for the same reason the receipt
	// arm is: every arm above can return early on its own refusal, and a pending
	// fixture row whose author is gone must not stay in the queue because an
	// unrelated scratch root was misconfigured. Fail-open like its siblings — an
	// unreadable ledger costs one line and never fails the host task.
	fixtureReclaimTick(role)
	return removed + baseRemoved + declaredRemoved + sandboxRemoved
}

// reportResidentDeclaredTrees prints the resident DECLARED-TREES arm's evidence
// and returns the number of entries it collected.
//
// WHY IT PRINTS PER TREE AND NOT A TOTAL: the two things a reader of a worker's
// log must be able to answer are "which tree gave bytes back" and "was a tree I
// care about touched at all". A total answers neither — and the failure this arm
// closes was precisely an arm that printed NOTHING while 45 GB sat collectable,
// indistinguishable in the log from a tree that was walked and found clean.
//
// A row that could not be walked is reported on stderr ONCE, with its own error,
// and never counted as a zero-collection success (Rule 15: "could not look" is
// never "nothing there"). The pass is fail-open like every other arm on this
// tick: an unreadable tree costs a line and never fails the host task.
func reportResidentDeclaredTrees(role string, results []scratch.DeclaredTreeSweepResult) int {
	total := 0
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(os.Stderr, "[%s] [%s] declared tree %s could not be swept (host task unaffected): %v\n",
				role, scratchGCRoleLabel, r.Name, r.Err)
			continue
		}
		if r.Removed == 0 {
			continue
		}
		fmt.Printf(scratchGCDeclaredTreesLogFmt+"\n", role, "", r.Removed, r.Age, false, r.Name, r.Bytes)
		total += r.Removed
	}
	return total
}

// reportResidentScratchGCLock is the resident tick's lock evidence line — the
// same fact the CLI prints, in the tick's own `[role] [label]` prefix shape.
//
// WHY A RESIDENT PASS MUST SAY IT: this loop is the only sweeper on a host that
// runs no suite, so a silent defer here means the sweep did not run and NOTHING
// will say so. A `.gc.lock` left by a crashed sweep used to do exactly that, tick
// after tick (Rule 15; epic 01a1300b). The evidence comes from ONE renderer
// (scratch.GCLockEvidence), so the two faces cannot drift.
func reportResidentScratchGCLock(role, tree string, v scratch.GCLockVerdict) {
	if !scratch.GCLockNeedsReporting(v) {
		return
	}
	fmt.Printf("[%s] [%s] scratch-gc lock %s tree=%s\n", role, scratchGCRoleLabel, scratch.GCLockEvidence(v), tree)
}

// scratchGCSweepBody is the sweep BODY, behind a package-level seam so a test
// can make one sweep's cost explicit (the epic's regression proof) WITHOUT
// manufacturing device load (Rule 27 bans that). BOTH faces below call it, so a
// stubbed body blocks the synchronous and the asynchronous caller alike — which
// is what makes the regression test's red proof honest.
var scratchGCSweepBody = func(role string) { scratchGCTick(role, time.Now()) }

// declaredTreesSweepBody is the DECLARED-TREES arm's seam, in the same shape and
// for the same reason as scratchGCSweepBody above: the arm's own outcome is what
// a test must read, and a test that re-called scratch.SweepDeclaredTreesAutomatic
// itself would bypass the CALL SITE — i.e. it would pass on a tick that never
// reaches the arm, which is precisely the defect this wiring closes (measured
// 2026-10-17: a red proof written against the arm instead of the call site came
// back GREEN on the mutated tree, because another arm of the same tick removed
// the fixture). A spy here observes the wiring itself.
var declaredTreesSweepBody = scratch.SweepDeclaredTreesAutomatic

// scratchGCSweep is the operator face's one-line call site: the `--tick` arm of
// `gopi tool scratch-gc` (cli_scratch_gc.go:853). It was the worker/merger
// tick's call site too, and CORRECTED 2026-10-30: neither process calls it any
// more (the worker round's call went in 187d05c, the merger tick in
// 2026-09-19), so this comment no longer claims a resident caller — see the
// file header for the measurement.
//
// DEPRECATED FOR A ROUND BOUNDARY: a caller that runs this INLINE puts the
// sweep's /proc scan on its own round boundary — see scratchGCSweepAsync, which
// was written for runTaskWorkerLoop's round. That loop no longer calls either
// face, so the async form currently has no production caller at all; it is kept
// only until its own removal is decided (filed as a separate row).
func scratchGCSweep(role string) { scratchGCSweepBody(role) }

// scratchGCSweepAsync is the ASYNCHRONOUS face of the sweep. It WAS the face
// the worker's claim loop used; measurably it is called by NO production code
// today (only cmd/agent/scratch_gc_tick_flight_test.go drives it), because the
// worker round's tick call went in 187d05c (operator ruling 2026-10-26: a
// worker dispatches, it does not reconcile the ledger) and nothing replaced it.
// The mechanism below is kept intact — and its docstring corrected — rather
// than deleted inside a task whose scope is the task-lifecycle pass; its
// removal is its own row.
//
// WHY THIS EXISTS (measured 2026-10-03, epic ws6-restart-20s): the sweep used
// to run INLINE at the top of every round of the worker's own loop. Its cost is
// a per-process scan of /proc — each live pid's cwd/fd/exe plus its ENTIRE
// /proc/<pid>/maps (bounded at 4 MiB by ProcMapsReadLimit) — driven by
// LiveHolders, then PathWithin per mapped file. On this host that is 12-18s per
// firing, and it is CPU-bound (measured: cpu_ticks 1 -> 93 -> 287 -> 505 -> 676
// over 5s, ~1.4 cores, with the loop goroutine caught by SIGQUIT inside
// scratch.readProcFile -> procMapsPaths).
//
// The consequence was NOT "the sweep is slow": the drain request is honoured
// exactly at the round TOP (consumeDrainRequests), so while the sweep ran the
// worker could not reach a boundary — and `gopi worker --restart --wait 20s`,
// whose bound is wall-clock, reported "STILL running 20s after the graceful
// stop request" on a daemon that had ALREADY consumed the request (its state
// read "draining since <t>", rounds 0, note "daemon starting"). A disk-hygiene
// pass was eating an operator's stop budget.
//
// TWO PROPERTIES, both deliberate:
//
//  1. OFF THE CALLER'S GOROUTINE. The loop returns immediately, so no round
//     boundary — and therefore no stop request — can ever wait on the sweep.
//     The sweep still runs exactly as often as before (its own throttle is
//     untouched); it just no longer stands between two rounds.
//  2. SINGLE-FLIGHT. One sweep at a time per process (the same rule the
//     throttle encoded for the serial loop); a tick that arrives while a sweep
//     is in flight is a no-op, never a second concurrent /proc scan. This also
//     keeps the sweep's own /proc work from multiplying on a slow host.
//
// Fail-open like its synchronous sibling: the sweep reports its own problems
// and can never fail the host task. A sweep that runs longer than
// scratchGCSweepSlowWarn says so (Rule 15: a cost that was measured and then
// discarded is a failure that got quieter as it was measured).
//
// IT RETURNS THE FLIGHT'S JOIN — see the third property below.
//
//  3. JOINABLE. It returns a function that blocks until the sweep goroutine has
//     ENDED (not merely until its body returned), and it publishes the same
//     handle as a channel (scratchGCSweepInFlightDone) for a caller that must
//     bound its wait. WHY THIS IS NOT OPTIONAL, measured 2026-10-13 (task
//     01a1343): the suite's close-out goroutine sweep samples the process's
//     whole goroutine set, and an asynchronous instrument still in flight at
//     that instant is indistinguishable — in the dump — from a leak:
//
//     suite goroutine sweep: parked repository goroutines: 1
//     github.com/nickhuang99/gopi/internal/scratch.readProcFile(...)
//     (...scratchGCTick → scratch.BaseSuiteRunReport → pathHeldByProcesses)
//     FAIL github.com/nickhuang99/gopi/cmd/agent   (a fully green -run set, exit 1)
//
//     The answer to "is this frame a leak?" is that the sweep is a RESIDENT
//     instrument of a host process: when the process exits it is withdrawn by
//     the kernel, and there is nothing to leak. What was wrong was that nobody
//     could WAIT for it — a stop that only signals leaves the other goroutine
//     runnable while the next instruction samples it. The join is the fix; the
//     sweep's threshold is untouched (exclusion by frame name would hide the
//     false positive AND every genuine instance with it).
//
// The caller does NOT wait at the round boundary — see the call site in
// cli_tasks_worker.go: waiting there is the 2026-10-03 defect this asynchronous
// face exists to remove.
func scratchGCSweepAsync(role string) func() {
	scratchGCSweepState.mu.Lock()
	if scratchGCSweepState.running {
		scratchGCSweepState.mu.Unlock()
		return joinScratchGCSweepFlight // this tick is a no-op, but the IN-FLIGHT sweep is still joinable
	}
	scratchGCSweepState.running = true
	// The goroutine closes THIS handle, never the shared field: another writer
	// (a test resetting the latch, a tick that starts the next flight) can
	// replace the field, and a completion path that reads it back would then
	// close a nil or somebody else's channel (measured: `panic: close of nil
	// channel` in a suite that resets the latch between cases).
	done := make(chan struct{})
	scratchGCSweepState.done = done
	scratchGCSweepState.mu.Unlock()
	go func() {
		defer func() {
			start := time.Now()
			if d := time.Since(start); d >= scratchGCSweepSlowWarn {
				fmt.Fprintf(os.Stderr, "[%s] [%s] sweep took %s (ran off the claim loop; no round boundary waited on it)\n",
					role, scratchGCRoleLabel, d.Round(time.Millisecond))
			}
			scratchGCSweepState.mu.Lock()
			scratchGCSweepState.running = false
			if scratchGCSweepState.done == done {
				scratchGCSweepState.done = nil // this flight still owns the published handle
			}
			scratchGCSweepState.mu.Unlock()
			// THE JOIN IS THE LAST STATEMENT, so "the join returned" means the
			// instrument is gone — including the reporting above and the latch
			// update, which are exactly the kind of tail a sampling close-out
			// used to catch.
			close(done)
		}()
		scratchGCSweepBody(role)
	}()
	return joinScratchGCSweepFlight
}

// scratchGCInterval resolves the resident sweep's cadence (Rule 2: the env is
// read in exactly ONE place, here). ok=false means the sweep is DISABLED; the
// order is env first (an operator's explicit shell value always wins), then
// the settings switch, then the default.
//
//	unset            → scratchGCDefaultIntervalMin
//	"0" / negative   → disabled (off switch, no settings edit needed)
//	"45"             → 45 minutes
//	garbage          → the default, with a report (never a silent fallback)
func scratchGCInterval() (time.Duration, bool) {
	raw := strings.TrimSpace(os.Getenv(scratch.GCIntervalEnvName))
	if raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "scratch-gc: %s=%q is not an integer number of minutes — using default %d\n",
				scratch.GCIntervalEnvName, raw, scratchGCDefaultIntervalMin)
			return scratchGCDefaultIntervalMin * time.Minute, true
		}
		if n <= 0 {
			return 0, false // explicit off switch
		}
		return time.Duration(n) * time.Minute, true
	}
	if !scratchGCEnabledInSettings() {
		return 0, false
	}
	return scratchGCDefaultIntervalMin * time.Minute, true
}

// scratchGCEnabledInSettings reads the settings.json switch (<key>: false
// disables). A missing file, a missing key or an unparsable file all mean
// "enabled": the durable switch is opt-OUT, so a deployment that upgrades into
// this feature gets a bounded scratch tree without editing anything — the
// failure mode of a missing switch must not be "accumulate garbage forever".
func scratchGCEnabledInSettings() bool {
	path := config.ResolveSettingsPath()
	if path == "" {
		return true
	}
	cfg, err := config.FromSettingsFile(path)
	if err != nil {
		return true // unreadable settings: never let it disable disk hygiene
	}
	return cfg.ScratchGC == nil || *cfg.ScratchGC
}

// scratchGCPlural words the sweep's report without an i18n dependency.
func scratchGCPlural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
