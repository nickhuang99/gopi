package main

// cli_tasks_worker.go — the WORKER LOOP (`gopi worker`): the general
// task-management service (epic serve/worker-split 2026-09-03, nick's final
// design). The worker is the ONLY task-execution process — it runs
// identically on BOTH the client (HP) and the server (peerbox), while
// `task serve`/`task merger` were the SERVER-ONLY merge pass — the retired
// merger tick (deleted 2026-09-19; its live half is mergerPassOnce). It ran
// ONE tick, scheduled by the host (a cronjob since daemon-machinery-removal,
// 2026-09-17), never a detached process.
//
// A worker is a PLAIN process (no daemon machinery beyond its own loop —
// parallelism is the number of worker processes; death is stateless — every
// durable fact lives in the task ledger + sync channel). One loop round:
//
//	① NOTHING RECONCILES THE LEDGER HERE (corrected 2026-10-30, task
//	   01a18ef7). This bullet used to list the stale sweep and the
//	   finish-cleanup sweep as "task-lifecycle passes owned by the worker".
//	   MEASURED: neither ran. The worker round's tick call was removed by
//	   187d05c (operator ruling 2026-10-26), the pass itself had no production
//	   caller left and was DELETED — task_lifecycle_tick.go carries the per-leg
//	   verdicts — and serveFinishCleanupSweep does not exist in this tree (its
//	   only remaining mentions were this comment and cli_tasks.go's). The
//	   worker declares the dispatch boundary and does nothing else
//	   (worker_round_dispatch_only_test.go enforces it).
//	② claim the next pending task (TaskNextFiltered CAS — the M4.20 起点
//	   原子拒绝: a lost claim race or a peer-refused claim broadcast
//	   YIELDS to the next round instead of executing, so two workers never
//	   double-run one row);
//	③ execute it via the EXISTING task run machinery (child `gopi` run with
//	   the payload as briefing);
//	④ terminal state lands through the standard writeback, whose M4.19 ②
//	   must-succeed push (PushTerminalTaskSync) guarantees the terminal
//	   status reaches every sync peer — no worker-side push code here;
//	⑤ delivery-time worktree removal (resource-responsibility principle):
//	   the party that REQUIRES the worktree cleans it up. Careful — the
//	   finish-cleanup CHAIN named here is RETIRED (2026-10-30, task 01a18799
//	   slice 3/4, commit 58b5618b), so there is no worker-side re-attempt
//	   sweep any more; under Rule 6c a clone ground publishes itself at its
//	   own END hook, and the surviving dead-holder reclaim (merger pass) only
//	   frees a DIRECTORY left by a provably dead holder;
//	⑥ claim the next task. When nothing claimable remains the worker
//	   re-checks after workerIdleInterval; after workerIdleExits
//	   CONSECUTIVE empty rounds (no task executed in between) it exits 0
//	   on its own — a worker is a task-DRAIN tool, not a daemon (nick 9/1
//	   design: it runs and self-dies). The M4.20-D drain-exit is preserved
//	   as the `--once` flag (a drained queue then exits 0 immediately,
//	   SUCCESS not error); GOPI_WORKER_IDLE_EXITS=0 opts back into the old
//	   wait-forever behavior.
//
// The worker deliberately reuses runTaskRun (the single-runner semantics,
// repo guardrails, claim broadcast, writeback chain all live there) and
// adds only the loop, the yield, the idle wait and the sync legs around it.
// CORRECTED 2026-10-30 (task 01a18ef7): this used to say "the lifecycle
// passes", which the round no longer runs (worker round 187d05c; the pass
// itself was deleted — see the header).

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/daemonlifetime"
	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/shot"
	"github.com/nickhuang99/gopi/internal/signalrec"
	"github.com/nickhuang99/gopi/internal/tools"
	"github.com/nickhuang99/gopi/internal/worktreerelease"
)

// Worker-loop constants (Rule 1: every literal lives here, one place).
const (
	// workerDefaultMaxRounds bounds the whole loop: a worker dies after this
	// many rounds EVEN IF the queue keeps refilling (a runaway submitter
	// must not pin the machine forever). 0 = unlimited (daemon semantics:
	// the loop ends only when the user kills it, or --once drains).
	workerDefaultMaxRounds = 0

	// workerMaxRoundsEnv overrides workerDefaultMaxRounds (seconds-free: a
	// plain round count; malformed/negative values fall back to the default
	// with a warning — a typo must not unbound the loop silently either way).
	workerMaxRoundsEnv = "GOPI_WORKER_MAX_ROUNDS"

	// workerIdleRecheckMS: when the queue looks drained, one final re-check
	// after this pause guards against a submit racing the Empty verdict
	// (the claim CAS makes a missed row harmless — the next claim wins it).
	workerIdleRecheckMS = 1000

	// workerIdleIntervalEnv gates the drained-queue wait (seconds): how long
	// a drained worker sleeps before re-checking the queue (the long-running
	// daemon tick; M4.20-D drain-exit is the --once face). Absent/0/negative
	// = the default; a malformed value falls back to the default with a
	// warning (Rule 2: one reader, workerIdleInterval).
	// Declared in internal/config (Rule 14: one definition in the leaf package
	// that also owns the catalog row and the `--help-env` entry for this knob),
	// aliased here because this file holds its ONE reader.
	workerIdleIntervalEnv = config.WorkerIdleIntervalEnv

	// workerIdleIntervalDefault is the drained-queue wait when the env is
	// unset: a worker re-checks every 30s — fast enough that a freshly
	// submitted task starts within half a minute, slow enough that an idle
	// worker costs nothing.
	workerIdleIntervalDefault = 30 * time.Second

	// workerDefaultIdleExits bounds an idle worker's life: after this many
	// CONSECUTIVE drained rounds (queue empty, no task executed between
	// them — a busy round always resets the count) the worker exits 0 on
	// its own. A worker is a task-DRAIN tool, not a daemon (nick 9/1: it
	// runs and self-dies); default 3 × the drained-queue wait = ~90s of
	// quiet before a drained worker stops waiting for a Ctrl+C that should
	// not be needed. 0 = never exit on idle (the pre-fix daemon wait).
	//
	// It is expressed against the WAIT CONSTANT above, deliberately: the
	// "≈90s of quiet" claim in this comment is the reason the value exists,
	// so a change to the wait must carry the life budget with it instead of
	// silently turning the quiet window into something else.
	workerDefaultIdleExits = 3

	// workerIdleExitsEnv overrides workerDefaultIdleExits (a plain count
	// of consecutive empty rounds; malformed/negative values fall back to
	// the default with a warning — Rule 2: one reader, workerIdleExits).
	workerIdleExitsEnv = "GOPI_WORKER_IDLE_EXITS"

	// workerExplicitExitOK / workerExplicitExitFailed are the exit codes of
	// `gopi worker <task-id>` (Rule 1b: declared once so the code and the
	// usage text cannot drift). They are deliberately the NARROW pair the
	// retired `gopi task run <id>` used: 0 = the named row ran and wrote back a
	// completion, 1 = it failed or could not be run at all — the contract a
	// shell script wrapping one row depends on, and the reason the named-row
	// face is NOT the loop (a failed round must not be swallowed).
	workerExplicitExitOK     = 0
	workerExplicitExitFailed = 1

	// workerExcludedTags: rows tagged with any of these are never claimed by
	// the worker loop (human/other-dispatcher handling — the merger's
	// --skip-tag anti-misclaim guard, same contract, fixed list). The agency
	// pass's proposal rows join the list by REFERENCING their own constant
	// (internal/tools.QuarantineTagSet → agencyscan.TagQuiet): a proposal is
	// addressed to the operator, never to a claimer, so a disabled-by-default
	// miner can never turn into an auto-dispatch machine.
	workerExcludedTags = "pa-queue," + tools.QuarantineTagSet
)

// workerRoundOwnReleaseFmt is the ONE line the producer-side release prints per
// row it acted on (released, refused-by-name, or repaired) — the same rendering
// shape the round's other close-out lines use, so an operator greps one prefix
// for the whole close-out.
const workerRoundOwnReleaseFmt = "worker: own-worktree-release: %s\n"

// workerRoundOwnWorktrees releases the worktree(s) of every row THIS worker
// process finished, at the moment its round ends.
//
// WHY HERE AND NOT ONLY IN THE SWEEP (live row 01a095b2, 2026-09-12): the claim
// stamp a round writes is this long-lived `gopi worker` PID. The sweep's
// dead-holder reclaim requires a PROVABLY DEAD pid, so the moment a row
// finished its directory became invisible to that arm — `git worktree list`
// kept naming it, scripts/safe-merge.sh gate [c] kept refusing the branch, and
// the finished deliverable was withheld with no line anywhere saying why. The
// producer is the one party that knows the row is over without inferring it.
//
// The pass is DEFERRED so it runs on EVERY way a round can end (a normal
// `worker: run failed` continue, a return, a panic) — the round's own claim
// stamp is still installed either way, so the rows it releases are exactly the
// ones this process ran. Lines join the ROUND's own record (workerRoundAnnounce,
// the same sink as the claim and the workdir move): the worker keeps the
// stream it was launched with for the whole round (epic child-stream-task-output
// — the EXECUTOR CHILD's stream, which holds the task's own text, is the one
// the spawn seam redirects into the row's session directory), and refusals
// additionally reach the notify sink.
//
// THE CWD RELEASE RUNS HERE TOO, and in this order: the reclaim needs a usable
// cwd, and only AFTER a directory has been removed does standing in it pin the
// deleted inode. The worker loop is the archetype the release obligation was
// written for — it is a long-lived process that outlives many rounds, and a
// round that leaves it standing in a removed worktree keeps that directory's
// inode (and every file under it) alive until the worker itself exits, which
// is exactly the holder finish-cleanup guard ③ keeps reporting. One
// implementation: internal/worktreerelease (Rules 13/14) — never a second
// chdir spelling here.
func workerRoundOwnWorktrees() {
	workerRoundAnnounce("[worker] [own-release] claimed by this worker: %s\n", tools.TaskClaimOwner(""))
	for _, line := range tools.ReclaimOwnWorktreesAtCloseoutWithin() {
		workerRoundAnnounce(workerRoundOwnReleaseFmt, line)
	}
	// (The ownership-independent half runs INSIDE the call above, under the same
	// close-out mark — see tools.ReclaimOwnWorktreesAtCloseout for why the two
	// halves must not be two calls.)
	// ReleaseReporting writes the release lines to the given sink: the round's
	// own stdout, i.e. the stream the worker was launched with (the same sink
	// workerRoundAnnounce above writes to). One destination for the round's
	// record (Rule 2) — the cwd-release half of the pass included.
	worktreerelease.ReleaseReporting(os.Stdout, workerProgTopLevel)
}

// workerRoundAnnounce renders one line of the ROUND's own record on the
// worker's stdout — the stream the launcher launched this worker with (it
// pipes it into the fixed pre-claim record, <gopi-dir>/logs/worker-startup.log
// at launch, or nothing at all when it drains inline).
//
// It exists for the `--once` face: a one-shot worker's printed lines ARE the
// call's result (`scripts/gopi-worker-up.sh` and its deploy seams read them),
// so the three facts the launcher greps — claim, attribution, workdir — must
// reach it. Since the epic child-stream-task-output the worker holds its own
// stream for the whole round (only the EXECUTOR CHILD's stream is pointed at
// the row's output log — and, since epic worker-foreground-console, copied to
// this same stream too unless this process is a detached daemon), the round's
// record IS the process's stdout.
//
// It STAYS pure stdout (never the host lifecycle record of host_lifecycle_log.go):
// these lines are the round's account — the row it claimed, the worktree it
// moved into — and the declared per-host file is the component's LIFECYCLE
// record (start/drain/stop), not a per-task transcript. A reader asking "given
// the task id, where is its account?" is answered by the row's session
// directory, and mirroring a claim line into a shared component file would put
// task text where no task id reaches it — the boundary this epic keeps.
func workerRoundAnnounce(format string, args ...interface{}) {
	// THE ROUND'S ONE STDOUT DOOR also feeds the screen-byte recording when the
	// round armed one (worker_screen_stream.go): the operator's line and the
	// artifact's frame are the same bytes, which is what makes the artifact an
	// "equivalent output" record rather than a second, differently-shaped one.
	// Unarmed, this resolves to os.Stdout and costs one mutex read.
	fmt.Fprintf(workerScreenStreamWriter(), format, args...)
}

// workerCtx is THIS process's control plumbing (state file + durable request
// reader). It is INERT for the foreground face — every method is a no-op — so
// the round code above calls it unconditionally: one shape, no branching at the
// call sites. A resident daemon fills it in runTaskWorkerLoop.
var workerCtx workerDaemonCtx

// There is NO lifecycle throttle clock in this file and NO lifecycle pass to
// throttle: the pass and its clock (taskLifecycleLastTick,
// taskLifecycleThrottleMin) were DELETED on 2026-10-30 (task 01a18ef7),
// together with every leg that had no carrier. What this file still does is
// take and release the lifecycle OWNERSHIP mark
// (acquireTaskLifecycleOwnership in task_lifecycle_tick.go) around the loop,
// and dispatch rows — never reconcile the ledger (operator ruling 2026-10-26,
// enforced by worker_round_dispatch_only_test.go). The one duty the pass
// carried that an operator still needs — the ownership-independent worktree
// reap — lives on the live verb `gopi task merged-cleanup`
// (serveWorktreeReapSweep, cli_tasks.go).

// runTaskWorker implements `gopi worker`: the general task-management loop.
// A drained queue does NOT hold the worker forever: it re-checks after
// workerIdleInterval and, after workerIdleExits consecutive empty rounds,
// exits 0 on its own (a worker runs and self-dies). once=true preserves the
// M4.20-D drain-exit semantics: drain → exit 0 immediately.
//
// `explicit` (the POSITIONAL `<task-id>`, epics worker-run-entry 2026-09-16 and
// worker-positional-taskid 2026-09-18) runs exactly that row and NOTHING else:
// no claim scan, no loop, no idle wait — one round whose target the operator
// named. The process then exits with the row's own outcome,
// which is what makes the verb usable as a plain foreground runner and as a
// scripted one. It is mutually exclusive with `--once` (both were refused
// together at the argv scan, where both words are visible).
func runTaskWorker(once bool, explicit string, opts *tasksOptions) {
	// LIFETIME BINDING (opt-in, and it belongs to the VERB, not to one face).
	//
	// When a harness handed this process the read end of a death pipe, a
	// watchdog goroutine ends it the instant that harness dies — SIGKILL
	// included, with no cleanup hook to run. The DAEMON face has armed this
	// since the daemon flag landed (workerDaemonRun), but the FOREGROUND face
	// never did — and that omission is a measured orphan, not a hypothetical:
	// pid 3634507, spawned by this package's e2e suite
	// (TestWorkerDaemon_ForegroundWorkerIgnoresTheLock), alive 2 days 4 hours
	// after its test process died, carrying GOPI_DAEMON_DEATHSIG_HOLD_FD=200 in
	// its own environ — it INHERITED the binding and never watched it, so fd
	// 200 was an inert descriptor and the worker simply outlived everyone.
	//
	// The binding is armed HERE rather than in the daemon face alone because
	// every face of `gopi worker` is a long-lived loop a harness may spawn:
	// a face that ignores the descriptor it was handed is the leak. It is
	// opt-in by construction (ArmedFD is false unless the variable names a
	// descriptor this process actually holds), so an operator running `gopi
	// worker` in a terminal is unaffected — no pipe, no watchdog, exactly the
	// behaviour before this call existed.
	daemonlifetime.WatchFromEnv()
	// RESIDENT DAEMON CHILD (cli_worker_daemon.go): the caller is a `gopi worker
	// --daemon` parent that handed this process an inherited single-instance
	// flock. That lock is what makes a second `--daemon` refuse; the FOREGROUND
	// face (below) never touches it, so a developer can always run a worker in
	// the front while the daemon is up.
	if workerDaemonChild() {
		workerDaemonRun(once)
		return
	}
	// NAMED ROW (`gopi worker <task-id>`): its OWN face, never the loop. The
	// row is handed over as the round's own opts so workerRound reads a NAMED
	// round and takes the no-claim arm — the loop is not entered at all, so
	// "ONE row, then exit" is a property of the call graph rather than of a
	// return value (epic worker-taskid-one-row: the two faces of `gopi worker`
	// must not be one code path with a flag, which is what a mode argument
	// eventually becomes).
	if explicit != "" {
		named := opts
		if named == nil {
			named = &tasksOptions{}
		}
		named.taskID = explicit
		runTaskWorkerExplicit(explicit, named)
		return
	}
	runTaskWorkerLoop(once)
}

// runTaskWorkerExplicit is `gopi worker <task-id>`: ONE round on a NAMED
// row, in the foreground, with the process's exit code carrying the outcome.
//
// ONE ROW, AND NO CODE PATH THAT COULD REACH A SECOND (epic
// worker-taskid-one-row, briefing "gopi worker taskid should exit"): the row
// is passed to the round as its NAMED round, and the round's claim arm exists
// only for the LOOP face — so `gopi worker <task-id>` cannot claim a different
// row, loop, or wait for idle even if someone later turns the once/swallow
// decisions of this function around. That is deliberate: the guarantee is
// STRUCTURAL (the claim arm is unreachable from here), not a return value a
// later edit can invert. Measured against a queue holding three pending rows,
// the named row ran and the other two stayed PENDING
// (TestWorkerExplicitRunsExactlyOneRow).
//
// WHAT IT IS NOT: a second runner. It calls the same workerRound →
// runTaskRunOnce funnel every other face uses (the drain guard included;
// corrected 2026-10-30 — the lifecycle passes it used to name were deleted),
// so an explicitly run row gets the worker's provenance,
// output attribution, singleton guard and own-worktree release by construction
// — the "no more different handling" contract of the epic. What it DROPS is
// only the loop: no claim of a DIFFERENT row, no idle wait, no self-exit
// heuristics — the operator named the work, so the process ends when it is
// done.
//
// EXIT CODE (the retired `gopi task run <id>` contract, kept): 0 when the row
// ran and wrote back a completion, non-zero when it failed or could not be run
// at all. The worker LOOP deliberately swallows a failed round and continues;
// a named row is the case where the caller is a shell script that must know.
func runTaskWorkerExplicit(taskID string, opts *tasksOptions) {
	// The same graceful-stop contract as every other runner face: a SIGTERM
	// must not kill the process mid-defer, or the terminal writeback and the
	// own-worktree release never happen (one implementation, Rule 14).
	uninstallStop := installWorkerStopSignals()
	defer uninstallStop()
	// ROUND-SCOPED FORWARDER — the piece this face was missing, and the reason
	// `gopi worker <task-id>` could not be stopped.
	//
	// THE MEASURED DEFECT (2026-10-27): the handler above only RECORDS the
	// request; the goroutine that reads its wake-up (workerStopNudge) is started
	// by startWorkerStopForwarder, and the LOOP face starts it while this one did
	// not. So a SIGINT/SIGTERM mid-round recorded a stop, wrote its signal
	// record, nudged a channel nobody read — and the round stayed blocked in
	// cmd.Wait() on its run child to the end. Observed: +70s after the SIGINT the
	// wrapper was still `Sl` and the row was written back `completed`, i.e. the
	// operator's stop was silently discarded.
	//
	// Same contract as the loop (Rule 14: one runner, so a named run is not the
	// one face an operator can neither see nor stop): the forwarder delivers a
	// BARE signal to the child, while a DURABLE drain (`--stop`/`--restart`,
	// which wrote the request file) still forwards NOTHING — that shape keeps its
	// "let the in-flight round finish" guarantee (forwardWorkerStopDuringRound).
	stopForwarder := startWorkerStopForwarder()
	defer stopForwarder()
	// TASK-LIFETIME OWNERSHIP: this process is the task-lifecycle owner while
	// it runs, exactly as the loop is — without the mark a resident merger's
	// tick in the same process would run a duplicate sweep (see the loop).
	releaseTaskLifecycle := acquireTaskLifecycleOwnership()
	defer releaseTaskLifecycle()
	announceWorkerDisplay()
	workerCtx = workerCtxFor(os.Getpid())
	// The named row is ADDRESSABLE like any other round: publish it, watch for
	// a durable stop request, and let `--status` report it while it runs —
	// deliberately identical to the loop's prologue (Rule 14: one runner, so a
	// named run is not the one face an operator can neither see nor stop).
	pruneWorkerStateResidue()
	if workerCtx.writer != nil {
		defer workerCtx.writer.Forget()
	}
	defer workerCtx.cleanupRequests()
	// The watcher outlives a RETURN (it blocks on a ticker, not on this
	// function), so its teardown is deferred beside the request cleanup: a
	// harness that drives this face in-process must not leave the poll parked.
	// Installed into a LOCAL first, then deferred as a bare call: `defer
	// workerCtx.installFileWatcher()()` happens to evaluate the install here too
	// (the function VALUE is the call's operand, evaluated at the defer
	// statement), but `defer workerCtx.installFileWatcher()` — one paren short,
	// the shape a reader is one typo from — would defer the INSTALL itself, so
	// the row would run with no durable-stop reader at all while this line still
	// looked right. The local makes both halves explicit and gives the teardown a
	// name in every stack trace.
	stopFileWatcher := workerCtx.installFileWatcher()
	defer stopFileWatcher()
	ran, runErr := workerRoundNamed(taskID, opts)
	// CLOSE-OUT UNDER A STOP (the loop's own boundary, Rule 14): the forwarder
	// above tells an in-flight child to wind down, but the bare-signal contract
	// is also BOUNDED — the supervisor that sent the signal is about to escalate
	// to SIGKILL itself, so whatever of the round is still running (a tool's
	// grandchild, the bash job) is waited for up to workerStopGraceDefault and
	// then terminated, which is what lets THIS process's exit be the end of the
	// work it started. A DURABLE drain — or a TERMINAL HANGUP, which asks the
	// same question through workerRoundMustFinish — is left alone on purpose:
	// both mean "let the in-flight round finish", and this face's round already
	// ran to its own close-out (writeback + own-worktree release) before we got
	// here.
	if workerStopRequested() {
		workerCtx.consumeDrainRequests() // re-read the file: it is what makes this stop a DRAIN
		if !workerRoundMustFinish() {
			awaitWorkerStopChild(false)
		}
	}
	if !ran {
		if workerStopRequested() {
			fmt.Fprintln(os.Stderr, workerStopLine(workerStopReason()))
		}
		// Nothing ran: the row was refused before execution (unknown id,
		// terminal status, a live holder, a yielded claim) or a stop landed at
		// the boundary. workerRound/runTaskRunOnce already printed the reason
		// (Rule 15) — this exit code is its machine-readable half.
		os.Exit(workerExplicitExitFailed)
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "%v\n", runErr)
		os.Exit(workerExplicitExitFailed)
	}
	os.Exit(workerExplicitExitOK)
}

// runTaskWorkerLoop is the worker loop itself, shared by the foreground face
// and the resident daemon (whose lock + state file were wired by
// workerDaemonRun before this call — see workerDaemonCtx).
func runTaskWorkerLoop(once bool) {
	// SANDBOX PRECONDITION on the hermetic test escape (incident 2026-09-16, the
	// 5 burned rows): an armed GOPI_TASK_CHILD_FAIL_TEST with no sandbox root
	// makes EVERY claimed row fail synthetically. The gate sits HERE — before the
	// banner, before the lifecycle ownership mark, and above all before the first
	// claim — so a refusal changes no row's status (devdoc/task-child-fail-
	// escape-sandbox.md).
	assertTaskChildFailEscapeSandboxed(taskChildFailVerbWorker)
	// SCREEN BYTE RECORDING — THE NO-ROUND CASE (epic screen-stream).
	//
	// A guard for the shape a loop produces that a round cannot: a run that was
	// ASKED for the recording and never started a round at all (an idle or drained
	// queue). Nothing is opened here — a stream with no round's lines would be the
	// empty artifact the epic forbids — so this boundary only OWNS the sentence:
	// at the loop's end, if the switch was armed and no round ever armed a stream,
	// it says 本 run 无屏幕输出 and names the decision. The round face opens and
	// closes its own (workerRoundRun → workerScreenStreamOpen/Close), so the two
	// cases cannot produce two writers on one path.
	loopScreen := watchWorkerScreenStreamNoRound()
	defer loopScreen.report()
	maxRounds := workerMaxRounds()
	idleExits := workerIdleExits()
	// TASK-LIFETIME OWNERSHIP (task_lifecycle_tick.go): this loop is the
	// process's task-lifecycle owner for as long as it lives (a task host runs
	// one loop per process; `go test` runs several). The mark is taken BEFORE
	// the banner, so the banner is already covered. CORRECTED 2026-10-30: the
	// thing the mark used to YIELD to — a resident merger tick in the same
	// process — is gone (deleted 2026-09-19; the pass it guarded was deleted
	// 2026-10-30, task 01a18ef7), so no second sweep runs beside this one. The
	// mark is kept because it is the ownership record other faces read, and the
	// release is a DEFER so a loop that returns early — or panics and is
	// recovered further up the stack — still hands it back.
	releaseTaskLifecycle := acquireTaskLifecycleOwnership()
	defer releaseTaskLifecycle()
	// DISPLAY PREFLIGHT (epic worker-spawn-doors, GAP 1): the loop OWNS the
	// display verdict, so it is printed here — before the first round, once,
	// for every spawn door (the launcher script's detached worker, the
	// foreground face, and `--daemon`), which all converge on this function.
	// It never fails the start: DISPLAY is a capability, not a precondition.
	announceWorkerDisplay()
	// GRACEFUL STOP (signal-handling epic): a SIGINT/SIGTERM used to kill the
	// worker outright (Go's default disposition), so a round that was already
	// executing died mid-defer — its terminal writeback never ran and the
	// producer's own-worktree release (workerRoundOwnWorktrees) never fired,
	// leaving the finished row's directory in `git worktree list` and its
	// branch refused by safe-merge gate [c] forever (sandbox rig
	// /tmp/self-release-*, worker exit=143, no own-release line). The handler
	// turns the signal into a request: stop claiming, tell the in-flight run
	// child to wind down, and let the round's own defer chain finish.
	uninstallStop := installWorkerStopSignals()
	defer uninstallStop()
	// ROUND-SCOPED FORWARDER (devdoc/worker-term-exit-salvage.md U1, measured
	// 2026-09-17: a bare SIGTERM parked in futex_wait_queue_me because the
	// stop was only forwarded at the round boundary): the stop HANDLER only
	// records the request, and the round
	// blocks in cmd.Wait() on its run child — so without this, a bare SIGTERM
	// mid-round parked the worker forever (nothing ever told the child), and
	// SIGKILL was the operator's only exit. The forwarder delivers the stop to
	// the child while the round is in flight; the DRAIN shape is still left
	// untouched (only a bare signal/force is forwarded), so "let the in-flight
	// round finish" keeps its guarantee.
	stopForwarder := startWorkerStopForwarder()
	defer stopForwarder()
	// STATE FILE + DURABLE STOP (epic worker-daemon-singleton): the daemon half
	// publishes what it is doing so `--status` can report it, and reads the
	// operator's durable request file at each claim boundary (the file survives
	// a missed signal). Both are no-ops for the foreground face, where nothing
	// addresses this process by pid.
	workerCtx = workerCtxFor(os.Getpid())
	// The state files of DEAD pids are swept here, at the one moment a worker is
	// willing to mutate this profile's state directory: `--status` must stay
	// read-only, so a status query can never be what heals the pile (measured
	// 2026-09-13: 66 residue files made `--status` unreadable — see
	// cli_worker_status.go). A worker start is a mutating act by definition.
	pruneWorkerStateResidue()
	if workerCtx.writer != nil {
		defer workerCtx.writer.Forget()
	}
	defer workerCtx.cleanupRequests()
	// The watcher blocks on a ticker, so it survives a RETURN from this
	// function; its teardown is deferred beside the request cleanup. Production
	// exits the process here anyway, but this package's suite drives the loop
	// in-process, and a poll left parked fails the suite's close-out sweep.
	// Installed into a LOCAL first, then deferred (see runTaskWorkerExplicit for
	// why the one-liner spelling is a trap worth removing).
	stopFileWatcher := workerCtx.installFileWatcher()
	defer stopFileWatcher()
	// THE HOST LIFECYCLE RECORD (host_lifecycle_log.go): the loop's own lines —
	// start / drained / self-exit / stop — are the record an operator reads to
	// answer "what did the workers do, and who stopped them?". They are
	// MIRRORED into this profile's ONE declared per-host file
	// (<gopi-dir>/logs/worker.log, the same path the --daemon face already
	// writes) while still reaching the stream this worker was launched with, and
	// the STARTUP LINE names that path so a reader never has to know a session
	// id, a task id or a launcher to find it.
	//
	// WHERE IT IS DELIBERATELY NOT ATTACHED, and why each is not a fallback:
	//   * a daemon child — its stream IS that file (the spawn seam redirected
	//     it before the fork), so mirroring would double every line;
	//   * the --once face — a one-shot call's stdout is its result, and the
	//     launcher and E2E suites read it.
	// Attached BEFORE the banner, so the banner is the file's first round line.
	attachHostLifecycleLog(workerProgTopLevel, workerDaemonLogBaseName, once)
	defer closeHostLifecycleLog()
	// ⓪′ SHOT-UNIT RECLAMATION AT START-UP (epic shot-reclaim-trigger). The
	// epic's second new trigger, and the one that covers the host shape that
	// produced the measured leak: a worker is started whenever work is
	// dispatched, so this fires on every host that ever runs a task — unlike the
	// tick below, which only fires while a round is IN FLIGHT (a worker self-exits
	// after a drained queue, so the tick was not running when six units
	// accumulated on a quiet host).
	//
	// IT SITS BEFORE THE FIRST CLAIM, and that is deliberate: the reclamation is
	// what an operator wants to have happened before a new task starts, and a
	// start-up pass cannot delay or obstruct a claim (it is one ReadDir plus one
	// liveness read per registration, and it never fails this loop — see
	// shot.RenewNotice's fail-open contract).
	//
	// ON THE LOOP ONLY, not the --once face or the named-row face: --once is the
	// unit/E2E seam (Rule 6 — a test worker must not sweep real state), exactly
	// as the sibling ticks are guarded (`if !once` below), and a named row is
	// ADDRESSED work that must not spend its budget on host hygiene. A host that
	// runs tasks uses the loop (`gopi worker`, foreground or --daemon, both of
	// which enter here).
	if !once {
		shot.RenewNotice(shot.RenewRoleWorkerStartup)
	}

	// ⓪‴ THE SCRATCH SWEEP AT START-UP — the same placement, the same `!once`
	// guard and the same fail-open contract as the shot reclamation immediately
	// above, and for the same measured reason: it must fire on every host that
	// ever runs a task, NOT only while a round is in flight.
	//
	// THE DEFECT THIS CLOSES (measured 2026-10-31, task 01a18f45): the sweep had
	// NO production caller at all. `grep -rn "scratchGCSweepBody\|scratchGCSweep(\|scratchGCTick("
	// --include=*.go cmd/agent/ | grep -v _test` returns only the three
	// definitions plus the operator's `--tick` CLI arm — the worker round's call
	// went in 187d05c (the 2026-10-26 ruling that a round is claim → run →
	// report) and the merger's tick was deleted 2026-09-19, so the orphaned
	// `scratchGCSweepAsync` (written for that merger tick) has had no caller —
	// scratch_gc_tick.go's own doc comment still opens by explaining it "LIVES
	// IN THE WORKER/MERGER TICK", which is exactly the arrangement 187d05c
	// removed. (CORRECTION, same run: an earlier draft of this comment quoted
	// that header as saying "NOTHING resident fires this sweep" — a string that
	// exists NOWHERE in the tree (`grep -rn "NOTHING resident" --include=*.go .`
	// returns only the comment line it was typed into). The claim was inferred
	// from the code's shape and written as if quoted; the arrangement it names
	// is real, the quotation was not.)
	//
	// THE CONSEQUENCE, measured on this host the same day: /home/user/workspace
	// held 119 `go-build*` directories / 9.0 GB (+2 more appearing per hour at
	// ~1.4 GB/h), every one of them abandoned by a killed build, and the tree's
	// OWN window was not the problem — `gopi tool scratch-gc trees --dry-run`
	// printed `tree=go-tmp present=true entries=119 ... collectable=1`, i.e. the
	// predicate WORKS and only 1 entry had aged past GoTmpStaleAge (24h) while
	// 118 sat inside it. Nothing collected any of them because nothing ran the
	// tick.
	//
	// WHY START-UP AND NOT THE ROUND: the 2026-10-26 ruling is that a worker
	// CLAIMS, RUNS and REPORTS — a round boundary must never wait on a /proc
	// scan (measured 2026-10-03: 12-18s per firing, 1.4 cores, which ate an
	// operator's `--restart --wait 20s`). Start-up is before the first claim, so
	// it can obstruct nothing, and it is the exact argument the shot arm above
	// records. The sweep is ASYNC (scratchGCSweepAsync: off the caller's
	// goroutine, single-flight, joinable) so even a slow first pass cannot delay
	// the first claim either.
	//
	// IT IS NOT A NEW MECHANISM (Rule 35): the tick, its cadence
	// (GOPI_TEST_SCRATCH_GC_MIN, default 60min), its off switch
	// (settings.json `scratch_gc`), its age window and its whole predicate live
	// in internal/scratch and are unchanged. This line is the CALLER that
	// scratch_gc_tick.go's header says is missing, and adding a caller to an
	// existing bounded mechanism is the opposite of adding a dependency.
	//
	// ON THE LOOP ONLY, never `--once` and never a named row: --once is the
	// unit/E2E seam (Rule 6 — a test worker must not sweep real state) and a
	// named row is ADDRESSED work whose budget must not be spent on host
	// hygiene.
	//
	// THE JOIN IS NOT TAKEN, deliberately. scratchGCSweepAsync returns the
	// flight's join so a caller CAN wait, and the sibling boundary test
	// (worker_scratch_sweep_offboundary_test.go ③) asserts precisely that this
	// loop does NOT: a loop whose exit waits on a /proc scan is a loop whose
	// stop budget waits on it too — the same defect, moved from the round top to
	// the round never-ending. Measured here 2026-10-31: a `defer join...()`
	// spelling made the loop return only after the sweep finished, and the
	// positive test failed by name ("did not return while the sweep was still in
	// flight"). Fire-and-forget is the contract the shot arm above also keeps:
	// the flight's own fail-open reporting is its whole result.
	if !once {
		scratchGCSweepAsync(workerRoleLabel)
	}

	// idleStreak counts CONSECUTIVE drained rounds (no task executed
	// between them). A claimed-and-run round resets it to 0 — only quiet
	// rounds accumulate toward the self-exit.
	idleStreak := 0
	if once {
		hostLifecyclePrintln(procname.WorkerLoopStartLine + " --once (claim → run → must-succeed push → next; exit on drained queue)")
	} else {
		hostLifecyclePrint(procname.WorkerLoopStartLine+" (claim → run → must-succeed push → next; waits %s on drained queue, self-exits after %d idle round(s))\n", workerIdleInterval(), idleExits)
	}
	// THE IMAGE SELF-CHECK, ONCE PER CLAIM LOOP (image_selfcheck.go). This loop
	// decides which rows execute, and it runs for hours, so a stale image means
	// claims decided by a binary whose file was replaced under it — the measured
	// incident (`readlink -f /proc/<pid>/exe` → `… (deleted)`).
	//
	// THE VERDICT IS REPORTED, NOT FATAL HERE, and the difference from the merge
	// loop is deliberate: this loop ALSO carries the housekeeping ticks
	// (lifecycle, scratch GC, review sweeps) that a stale image performs
	// correctly, so exiting would turn a stale image into a new outage. The
	// report is owed at the CLAIM, which is the decision it is about, and it is
	// printed before the first claim either way.
	//
	// ON THE LOOP ONLY (not --once): --once is the unit/E2E seam (Rule 6 — a
	// test worker must not touch real state), exactly as the sibling ticks are
	// guarded above.
	if !once {
		if _, ok := residentImagePrologue(workerProgTopLevel); ok {
			fmt.Printf(imageSelfCheckArmedOnce, workerProgTopLevel, os.Getpid())
		}
	}
	// CGROUP ESCAPE (cli_daemon_scope.go, epic daemon-cgroup-detach): a worker
	// started by `gopi worker --daemon` leaves the login session scope BEFORE
	// it proves readiness — so the parent's "started" verdict and the scope
	// membership the verdict implies are the same fact. A no-op for the
	// foreground face (no scope env is stamped outside the seam), and it never
	// fails the start: an unavailable escape is announced in the log.
	if workerDaemonChild() {
		daemonScopeChildEscape(workerProgTopLevel, CliVerbWorker)
	}
	// SELF-DETACHED CHILD (cli_worker_daemon.go): the caller is a `gopi worker
	// --daemon` parent blocked on this process's readiness byte. The byte goes
	// out HERE — after the loop banner is on the stream (so the caller's log
	// opens with the proof that the loop ran) and after every pre-loop resolver
	// has succeeded — because the parent prints "started" on the strength of it.
	// A no-op for every other launch: the handshake fd exists only in a child
	// the shared seam spawned (daemonReadyFD is -1 otherwise).
	workerDaemonNoteReady()
	// THE IMAGE IS RE-CHECKED AT EVERY ROUND BOUNDARY, not only at the prologue
	// (epic merge-image-selfcheck broadcast half, row 01a18301). The prologue
	// check above is the merge loop's shape and it is NOT sufficient here: the
	// measured incident began with a CLEAN prologue — the worker started at
	// 18:50:43 on revision 84beb309, and the artifact was not replaced until
	// hours later. A prologue-only check passed, then said nothing for the rest
	// of the process's life, while every delivery verdict this loop printed came
	// from code two merged fixes short of the disk (20c15007c, 45eb68ece), so a
	// row that HAD been delivered was reported as "delivery did not land".
	//
	// THE VERDICT IS REPORTED, NOT FATAL — the same asymmetry the prologue
	// explains: this loop also carries housekeeping a stale image performs
	// correctly, so exiting would turn a stale image into an outage. What the
	// round owes is the SENTENCE, at the decision point, ONCE PER CHANGE
	// (workerImageRoundCheck), so a log with no such line means the image stayed
	// current and a log WITH it names both revisions.
	imageAlarm := newWorkerImageRoundCheck()
	for round := 1; maxRounds == 0 || round <= maxRounds; round++ {
		imageAlarm.Report(workerProgTopLevel)
		// ⓪⁻ CLAIM LOOP OPEN (Rule 15 observable, measured 2026-10-03): the
		// durable stop request is consumed at a round boundary, so a `--stop`
		// that arrives while this loop is still in its start-up prologue cannot
		// be honoured until the prologue ends. Publishing the boundary here —
		// the FIRST thing a round does, before the drain consume and before the
		// ledger is opened — is what lets an operator (and the E2E that drives
		// this surface) tell "the round has not started yet" apart from "the
		// round is not ending"; without it, a stop bound that expired during
		// start-up reported the wrong wait.
		workerCtx.markClaiming()
		// ⓘ PER-ROUND LOCK IDENTITY (see workerDaemonGuardIdentity): a replaced
		// worker.lock silently detaches this daemon's guard while the next
		// daemon can take the fresh file, so the identity is re-proved BEFORE
		// any work in the round. A daemon that finds another process on the
		// fresh lock STOPS here rather than sweeping beside it.
		if workerDaemonGuardIdentity() {
			return
		}
		// ⓪ CLAIM-BOUNDARY DRAIN CONSUME (epic worker-graceful-drain): read the
		// durable request files at the TOP of every round. This is the door the
		// briefing names — the request is consumed where claiming would
		// otherwise happen, so a stop is honoured even when the signal never
		// arrived and even when the file lands between two ticks. A pending
		// request means: claim NOTHING more, finish what is in flight, exit.
		workerCtx.consumeDrainRequests()
		if workerStopRequested() {
			hostLifecyclePrintln(workerLeaveLine(workerStopReason(), workerStopEscalated()))
			recordStopVerdict(workerRoleLabel, "")
			return
		}
		// ① THE ROUND IS DISPATCH AND NOTHING ELSE — the BOUNDARY DECLARATION.
		//
		// A worker claims a row, runs it and reports it. That is the whole
		// loop, and it is a rule rather than a preference (operator ruling,
		// 2026-10-26): ledger reconciliation, ground cleanup, scratch GC and
		// notify scanning are NOT the worker's business, and no leg of that
		// kind may be added back here.
		//
		// WHAT USED TO RIDE THIS BOUNDARY, AND WHERE IT WENT. Every leg below
		// was carried by this round because some resident component "had to"
		// run it — a REACHABILITY argument ("something must be running it"),
		// never a SCOPE argument ("this duty is a task dispatcher's"). That is
		// how a claim loop accreted four unrelated duties and became the thing
		// an operator could not reason about. Each now has an EXPLICIT one-shot
		// command an operator runs by name (Rule 15: no duty is silently
		// dropped). A LEG IS ONLY "COVERED" BY A COMMAND THAT ACTUALLY RUNS IT —
		// partial coverage by a neighbouring verb is not coverage, and the
		// assignments below were each MEASURED rather than assumed:
		//
		//	the stale report                → DELETED 2026-11-05 (read, not reported)
		//	the failed-needs-reissue census  → `gopi task list --status failed`
		//	the close-out pass alone            → `gopi task finish-cleanup --apply`
		//	scratch/temp directory GC           → `gopi tool scratch-gc tick`
		//	ref / code delivery                 → DELETED 2026-11-05 with the pass (below)
		//
		// Every leg of that pass has since been given its verdict (2026-10-30,
		// task 01a18ef7), so nothing here is waiting on a `reconcile` verb that
		// would run them together: the stale report is GONE (its carriers, its
		// persisted throttle stamp and its re-arm UPDATE were deleted
		// 2026-11-05 — a stale row is READ as stale, not reported by a
		// neighbour); the failed-needs-reissue census runs in the
		// `gopi task list` render; the WIP snapshot runs on the `failed`
		// writeback; and the signal-closeout, transport-reroute and periodic
		// legs were deleted with their engines (task_lifecycle_tick.go's header
		// carries the per-leg measurement). scratchGCTick
		// (scratch_gc_tick.go) still exists and rides the tick above.
		//
		// THE GUARD IS A TEST, not this comment:
		// worker_round_dispatch_only_test.go parses this loop's round-① region
		// and FAILS on any *Tick / *Sweep / *GC / *Reap call (proved red against
		// a planted one, on the real tree). A comment cannot enforce itself; a
		// scan can.
		//
		// WHY A DRAINED QUEUE IS NOT AN EXCUSE ANY MORE: the old shape fired
		// the passes EVERY round (idle or busy) so "abandoned close-outs
		// complete even when the queue is empty". That guarantee is now the
		// OPERATOR's — a worker that self-exits after workerDefaultIdleExits
		// empty rounds was never a reliable carrier for it anyway (the whole
		// reason the merger accreted them, and the merger is retired too).

		// ①″ THE MERGE PASS IS NOT DRIVEN HERE, AND NOT BY THIS PROCESS. A
		// resident MERGE TICK used to ride this same round (GOPI_MERGE_TICK_MIN,
		// default 15min), spawning `gopi merge --fast` as a BLOCKING child. It is
		// RETIRED 2026-09-27 (devdoc/worker-merge-tick-retired.md): merging is
		// `gopi merge`'s job, and a worker that runs it stops being the thing that
		// runs tasks — it goes offline for the duration of the child, and two live
		// workers then contend for the merge flock (observed: two tasks "both
		// waiting for another merge's lock"). The worker's job is the worktree and
		// the branch — in before the child, out after it — and nothing else.
		//
		// WHAT DRIVES UNATTENDED MERGING NOW: NOTHING, BY DESIGN. `gopi task merge`
		// is a foreground command the operator runs. No timer, no daemon and no
		// replacement driver exists or is to be added: an operator who wants the
		// pile swept asks for it by name. The retired knob is not silently
		// ignored — `gopi config show` explains it under the key's own row
		// (internal/config/retired_settings_keys.go), and no other command prints it.

		// ①‴ NOTHING HERE DRIVES THE MEMORY-ASSET SYNC — RETIRED 2026-10-25, and
		// the round no longer pays for it. The retired resident tick spawned one
		// cross-node ledger-sync child per declared peer every 30 minutes, and
		// each of those runs BLOCKED the round for 315-534s (5-9 minutes,
		// measured in ~/.gopi/logs/worker.log) — a task queue that stops
		// claiming for the duration of a knowledge-base push. That verb itself is
		// GONE TOO (`gopi memory sync`, deleted by 5677cc6e: the word now exits 1
		// with `gopi memory: unknown subcommand "sync"`), so there is no hand-run
		// spelling of it to point a reader at either. The store-triggered round in
		// internal/tools that USED to carry a lesson across on the store that wrote
		// it was deleted 2026-10-27 with that whole leg
		// (devdoc/memory-store-sync-decouple.md).
		// WHAT SURVIVES is a manual ONE-WAY COPY, not a sync: `gopi memory backup`
		// ships a verified snapshot of this ledger to a declared memory_peers peer
		// and imports nothing back (config.LedgerCopyForwardReplacement states it).
		// WHAT DRIVES UNATTENDED CONVERGENCE NOW: NOTHING, BY DESIGN — two hosts'
		// knowledge bases diverge until a human acts, which is the
		// operator's explicit decision. No timer, no daemon and no replacement
		// driver exists or is to be added (Rule 35); the retired knob still
		// SPEAKS rather than being silently ignored, on the configuration verb
		// that owns it (internal/config/retired_settings_keys.go).

		// ①⁗ THE CODE-DELIVERY LEG rides the same round (task 01a13440 — the
		// cadence/face half of "bare master is not a revision"). This is the
		// resident carrier that was MISSING: the git/ref pass (serveGitSyncSweep)
		// advances this host's integration ref (refs/heads/master) to the peer's
		// tip, and until now it was reached only from the serve tick and the
		// merger pass — on a CLIENT host that keeps a worker and no serve and no
		// merger, nobody advanced it at all. Measured 2026-10-13 on host-a:
		// the local ref was 141 commits behind the peer's tip and a purely
		// ancestor of it, so every statement of the form "gate X is red on
		// master" was unrecheckable and already-fixed defects read as still red
		// (devdoc/bare-master-is-not-a-revision.md, the measurement half).
		//
		// ZERO NEW MACHINERY (Rule 35): the round already carries the lifecycle
		// sweep, the scratch sweep and the memory-sync tick; this is one more
		// call on the same tick, and the cadence is the clock the pass ALREADY
		// owns. (The comment here used to name the git-pass interval variable
		// and a `serveGitSyncInterval` reader — NEITHER exists: the knob was
		// advertised but had no production reader and was removed 2026-11-05,
		// and the leg's carrier is now the clone channel's delivery loop.) No
		// cron line, no timer unit, no script, no state file, no second daemon —
		// the shape task_lifecycle_tick.go and scratch_gc_tick.go established.
		//
		// ITS PRECONDITIONS ARE THE PASS'S OWN, checked inside it and checked
		// cheap: no declared sync peer = the pass returns before touching git
		// (single-node hosts pay one boolean), and the throttle clock keeps a
		// fast round from git-pulsing. Fail-soft like every other leg: the
		// pass reports its own failures on stderr and can never fail a round —
		// a worker that stopped running tasks because a fetch timed out would
		// be a worse defect than the one this leg fixes.
		//
		// ①⁗′ IT DOES NOT RIDE THIS ROUND EITHER (operator ruling, 2026-10-26 —
		// the same ruling that took the lifecycle and scratch sweeps off this
		// boundary). Advancing the integration ref is CODE DELIVERY, not
		// dispatching: a worker that stops claiming for a fetch is no longer the
		// thing that runs tasks, which is exactly the failure this leg was
		// originally added to paper over.
		//
		// The measurement above still holds and is why the leg may not simply
		// vanish: on a CLIENT host with a worker and no merger, an unadvanced
		// ref left every "gate X is red on master" statement unrecheckable
		// (devdoc/bare-master-is-not-a-revision.md). The carrier changed again,
		// and it changed to a MECHANISM rather than a verb: `gopi task sync-pass
		// --once` (cli_tasks_sync_pass.go — now DELETED, measured: the verb answers
		// "removed — the cross-node sync surface is DELETED (2026-11-05)") was the
		// face that ran the SAME leg (serveGitSyncSweep) with the same interval
		// clock and the same declared-topology precondition. Work now crosses hosts
		// through the GIT ref a row's deliverable publishes to (the clone channel's
		// delivery leg), and no verb opens a session against a peer's ledger.
		//
		// The `once` guard that used to protect this call is gone WITH the call:
		// a worker no longer reaches a leg that could move a real branch ref
		// (Rule 6), so there is nothing left to skip on --once.

		ran, _ := workerRoundClaiming() //stylegate:ignore the claiming verdict is what the loop acts on; the discarded error is the round's own already-recorded failure, so treating it as 'no round ran' is the safe arm
		// ①′ THE ROUND REFUSED TO CLAIM BECAUSE THIS NODE IS TRANSPORT-DEAD
		// (worker_claim_api_gate.go). This is NOT a drained queue and must not
		// be reported as one: the refusal line is already out (with the measured
		// evidence), so the loop either exits --once with a line that says what
		// happened, or backs off and re-measures — the row is still pending and
		// a link that comes back is used without an operator. The settled
		// re-check below is deliberately skipped: it would run a second ladder
		// against an endpoint this round just proved dead.
		if !ran && workerApiGateConsumeRefusal() {
			if once {
				hostLifecyclePrintln(workerApiGateRefusedExitLine)
				workerApiGateRecordRefusedExit()
				return
			}
			workerApiGateBackoffWait()
			continue
		}
		if ran {
			// A round that completed before the stop request banked its work;
			// a stop that arrived DURING the round must NOT have touched the
			// child (a drain lets the round finish — see awaitWorkerStopChild),
			// so the round returns on its own. Either way the loop must not
			// claim again.
			if workerStopRequested() {
				workerCtx.consumeDrainRequests() // re-read the file: it is what makes this stop a DRAIN
				awaitWorkerStopChild(workerRoundMustFinish())
				hostLifecyclePrintln(workerLeaveLine(workerStopReason(), workerStopEscalated()))
				recordStopVerdict(workerRoleLabel, "")
				return
			}
			// Straight into the next round — a busy queue needs no idle
			// wait, and a claimed-and-run round RESETS the idle streak
			// (only consecutive empty rounds accumulate toward the exit).
			idleStreak = 0
			continue
		}
		// Empty verdict: one settled re-check (a submit racing the claim
		// scan is caught here), then either drain-exit (--once) or the
		// idle-exit counting (long-running face).
		if workerStopRequested() {
			hostLifecyclePrintln(workerStopLine(workerStopReason()))
			recordStopVerdict(workerRoleLabel, "")
			return
		}
		time.Sleep(workerIdleRecheckMS * time.Millisecond)
		//stylegate:ignore the claiming verdict is what the loop acts on; the discarded error is the round's own already-recorded failure, so treating it as 'no round ran' is the safe arm
		if recheck, _ := workerRoundClaiming(); recheck {
			continue
		}
		// ②′ THE QUEUE IS EMPTY HERE — and only here. There is no peer leg
		// behind this point: a drained queue is the end of the round
		// (devdoc/worker-round-audit.md, epic 01a186d6). Taking a peer's row
		// was a WRITE to another ledger reached from a claim loop; the
		// transfer itself is still available, by name, as
		// `gopi task receive --takeover`.
		if once {
			// M4.20-D drained-exit semantics: always exit 0 (a drained queue
			// is SUCCESS for a worker — not an error). The line tells ops
			// whether OTHER holders still own live rows (claim-anti-reflow
			// keeps those visible) or this node is fully idle.
			if n := tools.LiveRowsOwnedByOthers(); n > 0 {
				hostLifecyclePrint("worker: queue drained (other holder active: %d rows)\n", n)
			} else {
				hostLifecyclePrintln("worker: queue drained — exiting")
			}
			return
		}
		// Long-running face: nothing claimable across the settled re-check —
		// count this drained round toward the idle self-exit.
		idleStreak++
		if idleExits > 0 && idleStreak >= idleExits {
			// Run-and-self-die (nick 9/1): a worker is a drain tool, not a
			// daemon. N consecutive empty rounds ≈ no new work is coming —
			// exit 0 (drained is SUCCESS, never an error).
			if n := tools.LiveRowsOwnedByOthers(); n > 0 {
				hostLifecyclePrint("worker: %d idle round(s) with no new task — exiting 0 (other holder active: %d rows)\n", idleStreak, n)
			} else {
				hostLifecyclePrint("worker: %d idle round(s) with no new task — exiting 0\n", idleStreak)
			}
			return
		}
		if idleExits == 0 {
			// Explicit opt-out (GOPI_WORKER_IDLE_EXITS=0): the pre-fix
			// daemon wait — never self-exit on an idle queue. The wait is
			// STOP-AWARE (workerIdleDrainedWait): a raw sleep here made a
			// long idle interval a long STOP delay, because the graceful
			// stop is a flag (SIGTERM keeps its disposition only through
			// installWorkerStopSignals) and the durable request file is the
			// second door (`worker --stop`). Both must wake this wait.
			workerCtx.setIdle(fmt.Sprintf("queue drained — waiting %s", workerIdleInterval()))
			hostLifecyclePrint("worker: queue drained — waiting %s (new tasks picked up next tick; kill to stop)\n", workerIdleInterval())
			workerIdleDrainedWait(workerIdleInterval())
			continue
		}
		workerCtx.setIdle(fmt.Sprintf("queue drained (%d/%d idle rounds)", idleStreak, idleExits))
		hostLifecyclePrint("worker: queue drained (%d/%d idle rounds) — waiting %s (new tasks picked up next tick)\n", idleStreak, idleExits, workerIdleInterval())
		// The verdict is banked and its line is out — announce it BEFORE the
		// wait, so an armed observer seeds its next-round task while this
		// round is still sleeping (the hook exists so the idle tests need no
		// wall-clock guess; see cli_tasks_worker_idle_hook.go).
		notifyWorkerIdleDrain(round, idleStreak)
		// A stop that arrives during the drained wait must not hold the
		// process for the rest of the interval: wake on the timer OR the
		// request, then loop back and exit through the same check.
		waitWorkerIdleOrStop(workerIdleInterval())
	}
	hostLifecyclePrint("worker: max rounds (%d) reached — exiting\n", maxRounds)
}

// workerIdleExits resolves the consecutive-drained-round self-exit bound
// (Rule 2: the env is read in ONE place; the constant is the default). 0 =
// never self-exit on idle (the pre-fix daemon wait, for operators who want
// a resident auto-consume worker).
func workerIdleExits() int {
	raw := strings.TrimSpace(os.Getenv(workerIdleExitsEnv))
	if raw == "" {
		return workerDefaultIdleExits
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		fmt.Fprintf(os.Stderr, "worker: %s=%q is not a non-negative integer — using default %d\n",
			workerIdleExitsEnv, raw, workerDefaultIdleExits)
		return workerDefaultIdleExits
	}
	return n
}

// workerIdleInterval resolves the drained-queue wait (Rule 2: the env is
// read in ONE place; the constant is the default).
func workerIdleInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv(workerIdleIntervalEnv))
	if raw == "" {
		return workerIdleIntervalDefault
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		fmt.Fprintf(os.Stderr, "worker: %s=%q is not a positive integer (seconds) — using default %s\n",
			workerIdleIntervalEnv, raw, workerIdleIntervalDefault)
		return workerIdleIntervalDefault
	}
	return time.Duration(n) * time.Second
}

// workerMaxRounds resolves the loop cap (Rule 2: the env is read in ONE
// place; the constant is the default).
func workerMaxRounds() int {
	raw := strings.TrimSpace(os.Getenv(workerMaxRoundsEnv))
	if raw == "" {
		return workerDefaultMaxRounds
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		fmt.Fprintf(os.Stderr, "worker: %s=%q is not a non-negative integer — using default %d\n",
			workerMaxRoundsEnv, raw, workerDefaultMaxRounds)
		return workerDefaultMaxRounds
	}
	return n
}

// The lifecycle sweep used to live here as workerLifecyclePasses — a
// worker-loop-only pass, which is exactly the coupling that stranded confirmed
// zombies and finish-cleanup work on a host with no worker alive. It then moved
// to runTaskLifecycleTick in task_lifecycle_tick.go, whose worker-round call was
// removed by 187d05c (operator ruling 2026-10-26: a worker dispatches, it does
// not reconcile the ledger), and on 2026-10-30 (task 01a18ef7) the whole pass
// was DELETED with every leg that had no carrier. Nothing replaced it inside
// this loop, and nothing should: the per-leg verdicts and the measurement behind
// them are in task_lifecycle_tick.go's header. The one duty an operator still
// needs from it — the ownership-independent worktree reap — lives on the live
// verb `gopi task merged-cleanup` (serveWorktreeReapSweep, cli_tasks.go).

// workerRowSource is HOW a round got the row it runs — the ONE difference
// between the worker's two faces, and the only thing the round's own record
// renders differently (a row this process CLAIMED from the queue vs a row the
// OPERATOR named on the argv). It is a closed enumeration on purpose: there is
// no value meaning "claim if nothing was named", which is exactly the shape
// that made the named face able to run a second row (epic worker-taskid-one-row).
type workerRowSource string

const (
	// workerRowClaimed — the LOOP face claimed this row (TaskNextFiltered CAS).
	// The wire value deliberately does NOT reuse a task-status spelling
	// ("claimed" is tools.TaskStatusClaimed): this enumerates HOW a round got
	// its row, not what state the row is in, and two meanings sharing one
	// string is how a later reader conflates them (Rule 1b/Rule 13).
	workerRowClaimed workerRowSource = "queue-claim"
	// workerRowNamed — the operator named this row (`gopi worker <task-id>`).
	// No claim happened, and the announce line says so: a named round must never
	// print that it claimed a row it was handed.
	workerRowNamed workerRowSource = "named-argv"

	// The announce lines, one per source (Rule 1b: the wording lives in a
	// constant, and the pair is asserted by the round's own tests). There is no
	// third line: a round's row is either claimed from this queue or named on
	// the argv, and the peer-transfer source that used to carry its own line is
	// gone (devdoc/worker-round-audit.md).
	workerRowClaimedAnnounceFmt = "worker: claimed %s\n"
	workerRowNamedAnnounceFmt   = "worker: running %s (explicit)\n"
)

// workerRoundClaiming is the LOOP face's round: claim ONE row from the queue
// (the M4.20 起点 CAS — exactly one concurrent claimer wins a row; a lost race
// loops internally and picks the next eligible row) and run it.
//
// Returns true when a row was run; false when nothing ran — the queue is empty,
// or the claim was ceded (a yield also counts as "nothing ran", and the
// caller's drained re-check tells a real drain from a transient refusal).
//
// THIS IS THE ONLY PLACE THE QUEUE SCAN LIVES (epic worker-taskid-one-row):
// `gopi worker <task-id>` calls workerRoundNamed, which has no claim arm and no
// path to one. The named face's "one row and nothing else" is therefore a
// property of the call graph, not of a boolean a later edit can flip.
func workerRoundClaiming() (bool, error) {
	// DRAIN GUARD (epic worker-graceful-drain): a stop request standing at the
	// moment of the claim must NOT claim. The loop's boundary check runs a
	// moment earlier, but a request can land in between (the watcher's tick, an
	// operator's file), and claiming then would start a round the operator has
	// already asked us not to start. Consume is idempotent, so calling it here
	// costs one stat and keeps ONE reader of the request (Rule 2).
	workerCtx.consumeDrainRequests()
	if workerStopRequested() {
		return false, nil
	}
	// ①′ PRE-CLAIM API REACHABILITY (worker_claim_api_gate.go, epic
	// api-preflight-claim-gate): BEFORE this node takes work, measure whether it
	// can reach the LLM endpoint at all. A row claimed on a transport-dead node
	// burns a full retry budget for nothing (measured 2026-10-29 on HP: 11m38s,
	// tokens=0, three retries, then `failed` — while a sibling node answered the
	// same edge IPs in 150ms), and the post-hoc reroute can only reopen the row
	// after those minutes are gone. Sitting HERE — after the drain guard, before
	// the claim scan — costs a dead node one bounded ladder and leaves the row
	// PENDING for a node that can reach the API.
	//
	// It is not a resident duty riding the claim loop: it is one synchronous,
	// bounded call with no state written (Rule 35), and the loop's existing
	// drained-queue arm is where a refused round waits before retrying, so a
	// node that comes back resumes with no operator.
	if !workerClaimApiGateDefault() {
		// The verdict is published for the LOOP (which owns the pace and the
		// exit line), never slept on here: the ladder itself is already
		// apiPreflightAttempts bounded connects, so a refused round costs one
		// measured preflight and no busy loop.
		workerApiGateMarkRefused()
		workerCtx.setIdle("api endpoint unreachable — not claiming")
		return false, nil
	}

	// ② Atomic claim: TaskNextFiltered's CAS is the M4.20 起点. Empty → nothing
	// claimable now.
	res, err := tools.TaskNextFiltered("", "", workerExcludedTags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "worker: claim: %v (yield this round)\n", err)
		return false, nil
	}
	if res.Empty {
		workerCtx.setIdle("queue drained")
		return false, nil
	}
	return workerRoundRun(res.Task.TaskID, nil, workerRowClaimed)
}

// workerRoundNamed is the NAMED face's round: run exactly `taskID` — the row
// the OPERATOR named on the argv — and nothing else, ever. There is no claim
// scan in this function and no way to reach one: the row is an INPUT, not a
// selection.
//
// The row's eligibility (unknown id, terminal status, a live ELSEWHERE holder)
// belongs to the run path below, which is where the ledger authority for it
// lives (runTaskRunOnce's keyed guard/claim arm, which also owns the "is a live
// process holding it?" ruling, so a named row keeps the singleton semantics the
// manual verb had). A second eligibility judgement here would be the
// duplicated-handling epic worker-run-entry removed.
func workerRoundNamed(taskID string, opts *tasksOptions) (bool, error) {
	// The drain guard applies to this face too: a stop request standing at this
	// moment must not start work the operator has already asked us to stop.
	workerCtx.consumeDrainRequests()
	if workerStopRequested() {
		return false, nil
	}
	if taskID == "" {
		// Unreachable by construction (runTaskWorker routes a non-empty row
		// here and refuses the empty one at the argv scan), so this is a LOUD
		// refusal rather than a silent fallback to claiming — that fallback is
		// precisely what would turn this face back into the loop (Rule 15).
		return false, fmt.Errorf("worker: named round without a task id — refusing to claim a row instead")
	}
	return workerRoundRun(taskID, opts, workerRowNamed)
}

// workerRoundRun is the round body BOTH faces execute: latch the row for
// termination provenance, publish it to the state record, enter the row's
// session directory, run it through the ONE run funnel (runTaskRunOnce), and
// close out.
//
// `source` selects the announce line and nothing else — everything else is
// identical by design, which is what gives a named run the worker's provenance,
// output attribution, singleton guard and own-worktree release without a second
// implementation (Rule 14). The CALLER (workerRoundClaiming / workerRoundNamed)
// owns how the row was obtained; this function never selects a row.
func workerRoundRun(target string, opts *tasksOptions, source workerRowSource) (bool, error) {
	// TARGET PROVENANCE (epic: "terminated received" is unattributable): latch
	// the row THIS round will run, so a termination line printed later in the
	// round's life — after its close-out, or from the loop's boundary — names
	// the task instead of "none". The latch is the LAST target and is never
	// cleared: 28 of 31 termination lines used to resolve to no task at all,
	// and the honest answer for a worker that ran a round is the row it ran.
	// It is latched HERE (after the explicit-target short-circuit and the claim
	// block, i.e. once `target` is final) so an EXPLICIT target is named too.
	workerRecordStopTask(target)
	// The round is now IN FLIGHT: publish it (a resident daemon's --status and
	// its stop path both read this — "what would a stop have to wait for?").
	workerCtx.setRoundStart("running " + target)
	// SCREEN BYTE RECORDING (epic screen-stream): the round's OWN record — the
	// lines below, the workdir move, the close-out — is a stream a reader may want
	// verbatim, so the same `uiclient.SgrStream` the chat arms is armed here behind
	// the ONE terminal-agnostic switch (GOPI_SCREEN_STREAM, parsed and graded by
	// internal/config/screenstream.go). Armed BEFORE the first announce line, or
	// the artifact would be missing the line that says which row this is.
	//
	// THE CLOSE IS DEFERRED HERE, which is the FIRST defer of this function, so it
	// runs LAST (defers are LIFO): the artifact holds the round's whole record, and
	// the report line it prints is not itself recorded — a stream whose final frame
	// is its own obituary would be a frame the run never wrote.
	screenStream := workerScreenStreamOpen(target)
	defer workerScreenStreamClose(screenStream)
	// The round's record: its own output log when the claim opens one, the
	// process stdout otherwise. `--once` is the drain-once deploy seam, so its
	// few lines are ALSO announced to the launcher (workerRoundAnnounce).
	// The line names HOW the row was obtained: a NAMED round was handed the row
	// by the operator (the run path below owns its claim), so its line must not
	// claim to have claimed it.
	switch source {
	case workerRowNamed:
		workerRoundAnnounce(workerRowNamedAnnounceFmt, target)
	default:
		workerRoundAnnounce(workerRowClaimedAnnounceFmt, target)
	}

	// ③ ④ Execute through the EXISTING task run path: TaskStart (the
	// in_progress flip), the M4.18 claim broadcast (start-point atomicity),
	// the child `gopi` run and the terminal writeback all live in
	// runTaskRunOnce. A broadcast REFUSAL or a failed child run returns an
	// error there and the standalone wrapper (runTaskRun) exits — the
	// worker dies stateless, exactly like any runner. workerWriteback
	// routes the terminal write through TaskRunWritebackTagged so the row
	// carries the worker provenance tag. ⑤ the child's own close-out runs
	// the delivery chain (push → worktree remove → branch -d) inside
	// internal/agent/taskregister.go; a row whose chain never ran is
	// re-attempted by `gopi task finish-cleanup --apply` (corrected
	// 2026-10-30: this line used to name "the lifecycle pass", which has no
	// production caller and was deleted — task_lifecycle_tick.go).
	// OUTPUT DESTINATION (epic child-stream-task-output): the task's own text
	// goes to its session dir because runTaskRunOnce points the EXECUTOR
	// CHILD's stdout/stderr at <session-dir>/output.log. The worker itself
	// keeps the stream it was launched with, so this round's own record (the
	// claim below, the workdir move, the close-out) reaches the launcher —
	// which is what the launcher greps to know the worker got to work.
	//
	// The state record's task/session publication rides the CLAIM boundary
	// (epic worker-status-record): the guard-clause chain below publishes what
	// the round knows — the session id when it resolved, the task alone when it
	// did not.
	sessionID, sErr := tools.TaskSessionID(target)
	if sErr != nil {
		fmt.Fprintf(os.Stderr, "worker: %s session_id lookup: %v\n", target, sErr)
		// Half-known, never unknown: the operator's "what is it running?"
		// question must survive a session-lookup failure.
		workerCtx.setRoundTask(target, "")
	} else {
		// The round is known: publish the task AND its session, so a
		// `--status` taken mid-round names the row an operator would have to
		// look at (the claim boundary this epic is about).
		workerCtx.setRoundTask(target, sessionID)
	}
	// SIGNAL PROVENANCE DESTINATION (epic signalrec-session-attribution,
	// 2026-09-14): the round's own records — the merger-side intents this
	// worker writes when it stops a bounded child, and a record for a signal
	// that lands on the worker itself — belong in the SAME session dir as the
	// round's output.log, claim and workdir. The claim is the first moment the
	// directory is knowable, so it is handed over here (Rule 2: one writer,
	// read by signalrec.sessionDest) and CLEARED at the round's end, so a
	// later round never files under a previous round's task.
	if sessDir, sdErr := tools.TaskSessionDir(target); sdErr == nil {
		// The SESSION LOG is deliberately "" here, and the empty value is not a
		// gap: the worker daemon hands over a task's session DIRECTORY, inside
		// which the round's CHILD writes its own run log. A worker that appended
		// its own verdict event into that file would forge the child's record —
		// the over-attribution this dir handoff exists to prevent. The child's
		// own recorder writes its verdict into the log its logger mints (the
		// SetSessionDir call in main.go's printLogPaths). ""
		// also CLEARS any previous handoff, so a worker can never append its
		// verdict to a round file.
		signalrec.SetSessionDir(sessDir, target, "")
	} else {
		// Rule 15: never silent. The record is still filed (the worker's own
		// resident process dir, or the inherited ledger id), but the operator
		// learns the round could not attribute to the task's dir.
		fmt.Fprintf(os.Stderr, "worker: %s signal provenance session dir unresolved: %v\n", target, sdErr)
	}
	// SESSION WORKDIR (epic task-session-cwd): with the id known, the round
	// STANDS in the task's own session directory — the same container its
	// output log, report and attachments live in (task_session_cwd.go). A
	// container that cannot be entered REVERTS the claim and does NOT run: the
	// executor child's stream is resolved into that directory by the spawn
	// seam (cli_tasks_exec_output.go), so a round that cannot enter it would
	// produce a run whose output nobody can find — the defect this epic
	// removes, never a degradation to accept quietly. The restore at the
	// round's end puts the worker back where it started, so round 2 does not
	// inherit round 1's directory.
	//
	// RUN-SCOPE LOCK, BEFORE the move (epic run-lock-per-run-dir): the row is
	// claimed and this process is about to make the run directory its own — so
	// the kernel lock on that directory is taken HERE, before the workdir move
	// and before the spawn.
	// A second process that reaches the same row is refused at this line with
	// the lock path and the holder's stamp, and it never enters the directory.
	//
	// It sits at the WORKER as well as in the run funnel on purpose: a worker
	// loops over rounds in ONE process, and the round's own workdir move +
	// child spawn happen from here — locking only inside runTaskRunOnce would
	// leave the move unguarded and would let a long round overlap the next one.
	// The funnel's own lock then recognises this process as the holder
	// (taskRunTakeLock's hand-off) instead of deadlocking against itself.
	relRunLock, lockErr := taskRunTakeLock(target)
	if lockErr != nil {
		// The claim is rolled back rather than left standing: a row claimed by a
		// process that will not run it is exactly the stale-row shape the lock
		// exists to make impossible. The rollback's failure is reported
		// separately (never swallowed).
		fmt.Fprintf(os.Stderr, "worker: %s\n", lockErr)
		if rbErr := tools.RevertClaimUnattributed(target); rbErr != nil {
			fmt.Fprintf(os.Stderr, "worker: rollback of %s failed: %v\n", target, rbErr)
		}
		return false, nil
	}
	defer relRunLock()
	wdLine, wdErr := taskWorkdirEnter(target)
	if wdErr != nil {
		fmt.Fprintf(os.Stderr, "worker: %s\n", taskWorkdirChdirRefuse(target, wdErr))
		if rbErr := tools.RevertClaimUnattributed(target); rbErr != nil {
			fmt.Fprintf(os.Stderr, "worker: rollback of %s failed: %v\n", target, rbErr)
		}
		return false, nil
	}
	workerRoundAnnounce("%s\n", wdLine)
	workerWriteback = target
	defer func() { workerWriteback = "" }()
	// ONE ROUND = ONE SET OF PROCESSES (task 01a15bd1): the pid the retry ladder
	// handed off belongs to THIS round only — a leftover from a previous round
	// is never waited on here (a recycled number would be somebody else's
	// process).
	workerRoundResetRetryResume()
	// bugfix-task-p1 semantics on the WORKER LOOP face: a failed run (child
	// failure, claim-lock yield, broadcast refusal) must NOT kill the
	// long-running worker. runTaskRunOnce returns an error on every failure
	// path — the CALLER decides what it means: the loop logs it and continues
	// to the next round, an explicitly named row turns it into the
	// process's exit code. Either way the row was already written back: a
	// child failure lands failed via writebackOutcome; a pre-execution refusal
	// leaves the row pending/reverted for the stale sweep or the next claim.
	// The --once drain-exit is separate: it ends only when the QUEUE is
	// drained, never on a row failure.
	//
	// The round executes with the caller's run options: a NAMED round carries
	// `opts.taskID` (the run path's keyed arm owns that row's guard and claim),
	// a CLAIMED one passes "" (the round's claim already picked the row and the
	// run path resumes it). The drain-excluded tag set is the round's own
	// (workerExcludedTags) whatever the caller passed.
	roundOpts := &tasksOptions{as: "", excludeSession: "", skipTags: workerExcludedTags}
	if opts != nil {
		*roundOpts = *opts
		roundOpts.skipTags = workerExcludedTags
	}
	if source == workerRowNamed {
		roundOpts.taskID = target
	}
	runErr := runTaskRunOnce(roundOpts)
	if runErr != nil && source == workerRowClaimed {
		// A CLAIMED round's failure is the loop's to swallow and continue past
		// (bugfix-task-p1). A NAMED round's failure is the process's own exit
		// code and is printed by the caller (runTaskWorkerExplicit) — printing
		// the "continuing" sentence here would promise a continuation that the
		// named face deliberately does not have.
		fmt.Fprintf(os.Stderr, "worker: run %s failed (continuing): %v\n", target, runErr)
	}
	// The round is over: leave the task's session directory FIRST (so the
	// next round — and everything the worker prints before it claims — lands
	// where the worker was started). One round = one task = one workdir.
	//
	// PRODUCER-SIDE RELEASE runs on the way OUT of this function (including the
	// `worker: run failed (continuing)` path and a panic): the worker's own
	// stderr is the launcher's stream — the round's text never went anywhere
	// else and there is no attribution to end (epic child-stream-task-output:
	// the executor child's stream, not the worker's, is the one pointed at the
	// row's output log).
	taskWorkdirRestore()
	// Signal provenance goes back to the worker's own process scope with the
	// workdir: the next round must not file its records under this round's
	// task (see the SetSessionDir call at the claim).
	signalrec.SetSessionDir("", "", "")
	//
	// A STOP SIGNAL lands here too: installWorkerStopSignals only records the
	// request and forwards it to the child, and the loop returns at the claim
	// BOUNDARY — so an interrupted round runs this very close-out instead of
	// being killed mid-defer. That is what makes "stopped" (signal-handling
	// epic, rig /tmp/self-release-*) release its ground exactly like "finished".
	// THE HANDED-OFF RETRY RESUME IS THE ROUND'S OWN CHILD TOO (task 01a15bd1):
	// the failure path's retry ladder SPAWNS a recovery run with Setsid and does
	// not wait for it, so without the wait below the round reports itself
	// finished while a process it started still stands in the row's worktree
	// (measured: 8m27s, ppid=systemd --user, cwd=the ground). The wait is
	// placed HERE — before the own-worktree release below — because that release
	// is the step a survivor's cwd blocks, and because "the round is over" must
	// mean the same thing to the release pass as it does to the operator.
	workerRoundAwaitRetryResume()
	defer workerRoundOwnWorktrees()

	// The round's work is DONE (its close-out defers above still have to run the
	// own-worktree release): publish the boundary so a --status reader sees an
	// idle daemon whose last round completed.
	workerCtx.setRoundEnd("last round ran " + target)
	return true, runErr
}

// workerWriteback is the task id the CURRENT worker round is executing —
// writebackOutcome consults it to route the terminal write through
// TaskRunWritebackTagged (worker provenance). Empty outside worker rounds
// (a plain `task run` keeps the tag-free writeback). Single-threaded per
// worker process, so a package-level latch is the whole state machine.
var workerWriteback string
