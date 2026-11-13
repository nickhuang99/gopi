package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// TestMain: scratch-root isolation + notify-env hermeticity, both installed by
// the shared entry point (testutil.PinSuiteForTestMain).
//
// The notify half used to live HERE as a five-variable Unsetenv loop (mirroring
// internal/tools) on the theory that dropping GOPI_TASK_NOTIFY_* made the suite
// hermetic. It did not: config.ResolveNotify resolves env > settings.json, and
// the durable layer reads ResolveSettingsPath() — the ambient GOPI_SETTINGS,
// i.e. the operator's real ~/.gopi/settings.json, whose notify.cmd appends to
// the LIVE sink. This suite drives the CLI end to end, so every task-lifecycle
// test delivered there (measured 2026-09-12: ~200 test task-ids in the live sink
// in two minutes). The shared pin now also points GOPI_SETTINGS at an empty
// per-run sandbox path. Root cause + the guard: internal/testutil/suiteenv.go.
func TestMain(m *testing.M) {
	// The daemon-leak spawner child (daemon_test_leak_e2e_test.go) must return
	// BEFORE the pins and m.Run: it exists to be SIGKILLed, so running a suite
	// inside it would be an own goal.
	if RunDaemonLeakSpawnerChild() {
		return
	}
	// The serve-lock diagnosis rig's re-exec'd holder helper
	// (serve_lock_diagnosis_liveness_test.go) is a FIXTURE, not a run of the
	// suite: it must be dispatched HERE, before any pin, or it takes a
	// whole-package cross-run lock in its own scratch home and the NEXT helper the
	// rig spawns judges it a sibling run and refuses with exit 99 — the measured
	// "the lock-holder helper EXITED before publishing its pids … BROKEN FIXTURE"
	// defect of 2026-11-08.
	if RunServeLockDiagnosisHelperChild() {
		return
	}
	// The borrowed-fd worker-lock guard's re-exec'd child
	// (borrowed_fd_worker_child_test.go) takes the inherited lock through the
	// production entry point and reports whether its descriptor survived. It must
	// never run the suite, for the same reason the spawner child must not.
	if RunBorrowedFDWorkerChild() {
		return
	}
	// The descriptor-holder child of merger_restart_takeover_contract_test.go
	// (RunMergerStopHolderChild) used to be re-exec'd here. It went with that
	// case when the resident merger daemon was retired (2026-09-19,
	// devdoc/merger-retired.md): the hold-child existed to stand a
	// merger-shaped flock holder up, and there is no merger flock to hold.
	testutil.PinSuiteForTestMain(cmdAgentSuiteName)
	// The turn counter the index rows read (internal/tools/chat_turns.go) is
	// INJECTED, not imported — the package cycle forbids the other direction.
	// main() installs it for the binary; this suite drives the same renderers
	// in-process, so it installs the SAME function here. Without it every
	// `session list` assertion would silently test the unmeasured branch.
	installSessionTurnsReader()
	// ...and the OTHER direction of the same cycle (gopilog → the ledger's
	// `chats.turn_count`): the suite drives `session log` and `session stats`
	// in-process too, so a missing reader here would leave THOSE assertions
	// testing the unmeasured branch while the binary prints a number.
	installLedgerTurnCounter()
	// Bind every daemon this suite starts (directly, or through a launcher
	// child) to THIS test process's lifetime, and sweep the orphans of earlier
	// runs. Without the binding a killed test binary leaves its daemons running
	// for hours (the /dev/shm exhaustion of 2026-09-14); with it the kernel's own
	// descriptor teardown takes them down under SIGKILL, where no cleanup hook
	// can run. A failure to arm is reported, never shrugged off: the suite must
	// not believe it is protected when it is not.
	testutil.ArmAndSweepForSuite(cmdAgentSuiteName)
	// The WRITE side of the profile isolation: this suite resolves the gopi data
	// root IN-PROCESS in several fixtures (`tools.ControlDir(workerStateDirName)`
	// in status_action_conflict_e2e_test.go and the residue rigs) and those calls
	// read config.GopiDir() from THIS process's environment. A child's env
	// (runGopiIn's env vector) cannot relocate the parent, so without this pin a
	// fixture seeds its "live-looking" worker state file in the OPERATOR's real
	// ~/.gopi/worker — where every later `gopi worker --status` reported it as a
	// phantom worker pid. Root cause, red-proof and the phantom-worker incident:
	// devdoc/worker-phantom-selfspawn.md.
	testutil.PinSuiteGopiProfile(cmdAgentSuiteName)
	// The daemon handshake pair this process may have INHERITED (a worker-daemon
	// parent) is dropped for the whole suite — the in-process worker tests take
	// the daemon path otherwise and refuse.
	unpinAmbientDaemonHandshake()
	// This suite drives the real CLI, so it creates whole GOPI_HOME sandboxes
	// and scratch trees — none may land on the / filesystem
	// (devdoc/tmp-ban-scratch-root.md).
	//
	// The stall watchdog is armed LAST, so it observes a suite that is fully
	// pinned: Go's own 10m panic arrives with a stack that names no cause, while
	// this reporter says which child the suite is still waiting on (or that no
	// child is alive and the stall is inside the binary). It never exits, never
	// kills and never changes what a test observes — see
	// suite_stall_watchdog_test.go for the measured shapes it names.
	// THE PACKAGE'S OWN WALL-CLOCK BUDGET, declared BEFORE the stall watchdog
	// reads the flag: the watchdog derives its own budget as a share of
	// `-test.timeout` (suiteStallBudgetFrom), so raising the timeout afterwards
	// would lift Go's alarm while leaving the reporter tuned to the OLD, smaller
	// budget — a bare run would then be told "suite stall" on a healthy suite.
	// Order is the mechanism here, not a style choice.
	declareSuitePackageBudget()
	stopWatchdog := startSuiteStallWatchdog(suiteStallBudgetDuration())
	// THE CALLER'S CHECKOUT, armed around the WHOLE run (suite_caller_head_guard_test.go).
	// A run inside a worker's clone ground was measured leaving that ground on
	// `master` — the tree under test and the tested binary thereafter disagreeing,
	// which surfaced as eleven REFUSED arms and exit 1 on the whole package. The
	// reading is taken HERE rather than in a case because the trigger was the
	// package as a whole: a per-case guard can be green while another case moves
	// the tree, and the checkout that moves is the CALLER's, not the fixture's.
	armSuiteCallerHeadGuard()
	code := m.Run()
	stopWatchdog()
	if moved := reportSuiteCallerHeadAtCloseOut(suiteCallerHeadBefore); moved > 0 && code == 0 {
		code = 1
	}
	// THE FRESHNESS GUARD'S ABSTENTIONS, measured the same way and for the same
	// reason: an e2e guard that abstained skipped its test, and a skip is
	// indistinguishable from a pass in the exit code. This run's e2e paths are
	// then UNVERIFIED, and the package must say so out loud rather than read as a
	// clean green (Rule 15: silence is the defect).
	//
	// It deliberately does NOT change the exit code. An abstention is an
	// environment fact, not a verdict about this package: the difference the guard
	// found is entirely carried by commits that landed under the run, and the tree
	// moved because somebody else was working. Turning that into a red would put
	// the 7% of false reds back — the defect this arm exists to remove — and
	// would buy nothing, because the line below already denies the green on the
	// only thing that matters (which e2e paths were actually verified).
	reportTreeMoveAbstentionsAtCloseOut()
	// The child sweep, AFTER the tests: the watchdog above only SPEAKS, and only
	// on a stall measured in minutes — a leaked child of a test that finished
	// never tripped it. This is the measurement TestSuiteLeavesNoLeakedChildren
	// asserts while a *testing.T still exists; the call below re-reports it (and
	// sweeps again) so a child leaked by a LATER case is still stopped, still
	// named in this run's log, and still fails the run — a leak that only got
	// written to a log is what the reporter already did.
	if over := reportLeakedChildrenAtCloseOut(); over > 0 && code == 0 {
		code = 1
	}
	// The same close-out for parked GOROUTINES: a leaked reader or server holds
	// the binary open exactly like a child does, and it is invisible to the
	// child sweep (the driver above only speaks at a stall measured in minutes).
	//
	// The INSTRUMENT is stopped first. The resident scratch sweep runs OFF the
	// worker loop's goroutine by design (scratchGCSweepAsync: 12-18s of /proc
	// scanning must not stand between an operator's --stop and the round boundary
	// that honours it), so at this instant it can legitimately still be RUNNABLE
	// — and a goroutine that is merely in flight is indistinguishable, in the
	// dump, from one that leaked. Measured 2026-10-13 (task 01a1343): a fully
	// green -run set reported
	//
	//	suite goroutine sweep: parked repository goroutines: 1
	//	  internal/scratch.readProcFile(...)  (scratchGCTick → BaseSuiteRunReport)
	//	FAIL github.com/nick/gopi/cmd/agent
	//
	// The threshold is NOT relaxed for it and no frame is excluded by name (that
	// would hide the false positive AND every genuine instance): the tick
	// publishes a join, the close-out waits on it, and anything still in the dump
	// afterwards is work somebody started and never ended.
	joinResidentSweepBeforeSampling()
	// The same rule for the OTHER instrument this process carries: testutil's
	// signal arm (armSuiteRunRootReapOnSignal) is a goroutine parked on a signal
	// channel BY DESIGN for the whole life of the process — it exists to reap
	// this run's scratch root if a SIGTERM/SIGINT ends the run. At close-out it
	// is therefore always parked, and until 2026-10-22 this sweep counted it as
	// a leak of this repository (measured on a run whose only test PASSED):
	//
	//	suite goroutine sweep: parked repository goroutines: 1
	//	  testutil.armSuiteRunRootReapOnSignal.func1()  suiterootsignal.go:77
	//	FAIL	github.com/nick/gopi/cmd/agent	1.454s
	//
	// — green tests, red run, an exit code that stops carrying a signal. The
	// threshold below is NOT relaxed for it and no frame is excluded by name (the
	// walk-the-dump filter refuses only framework frames and its own): the arm
	// publishes a JOIN, the close-out takes it BEFORE sampling, and anything
	// still in the dump afterwards is work somebody started and never ended.
	// FinishSuiteForTestMain joins it too, so every other suite gets this for
	// free; a second join is a no-op.
	testutil.JoinSuiteRunRootSignalHandler()
	if parked := reportSuiteGoroutinesAtCloseOut(); parked > 0 && code == 0 {
		code = 1
	}
	testutil.FinishSuiteForTestMain()
	os.Exit(code)
}

// TestSuiteNotifyEnvIsHermetic is this suite's guard on the shared invariant —
// the CLI lifecycle tests are the ones that leaked.
func TestSuiteNotifyEnvIsHermetic(t *testing.T) {
	testutil.AssertSuiteNotifyEnvHermetic(t)
	testutil.AssertSuiteHasNoNotifyTarget(t)
}

// TestSuiteLedgerIsSandboxed guards the LAST ambient root. This suite drives
// the real CLI end to end, so a test that does not name its own ledger resolves
// the operator's live ~/.gopi/memory.db — and the ledger is APPEND-ONLY, so a
// seeded fixture row is permanent. Exactly that happened here: the
// worker-recycle fixture's `t-stale-sibling` row (deliverable naming a decoy
// /tmp/other-repo) reached the live ledger and made the merger report it every
// pass (devdoc/reclaim-fossil-20260915.md). Dropping PinSuiteLedgerEnv from the
// suite entry point (testutil.PinSuiteForTestMain) makes this red.
func TestSuiteLedgerIsSandboxed(t *testing.T) {
	testutil.AssertSuiteLedgerIsPinned(t)
	// ...and INSIDE THIS SUITE'S OWN PROFILE, which the guard above cannot see:
	// it asks only "is this the operator's real file?", and a ledger pinned
	// BESIDE the profile (a sibling directory) passes that question while still
	// making every event this suite fires FOREIGN to taskNotifyDelivers — which
	// then suppresses it, so a notify test fails on the absence of the log it
	// asked for.
	//
	// MEASURED HERE (2026-11-04, task 01a1a69f, at 59d5de4): PinSuiteLedgerEnv
	// pins the ledger under the profile, so this guard is GREEN today — and it
	// is the arm that would have caught the sibling shape the fixtures below
	// still carried. Six tests in this package (the Notify|Stale|Sweep set) were
	// red because THEY pinned GOPI_MEMORY_DB=<tmp>/memory.db beside
	// GOPI_HOME=<tmp>, and each of them fires a real stale/notify event that the
	// gate withheld. Their fix is sandboxProfileLedger
	// (task_cli_testhelpers_test.go), which spells the ledger through
	// config.ProfilePathIn so the two pins cannot drift apart.
	testutil.AssertSuiteLedgerIsInsideItsOwnProfile(t)
}

// TestSuiteGopiProfileIsSandboxed is the guard on the WRITE half of the profile
// isolation: this suite's gopi data root must be an EMPTY sandbox inside the
// pinned scratch root, so an in-process fixture can never write worker/merger
// state into the operator's real profile. Dropping PinSuiteGopiProfile from
// TestMain makes this red — and that pin is what the phantom-worker report
// (14 stale `worker-state-*` files in the real profile, each attributed to a
// test-binary pid) traced back to.
func TestSuiteGopiProfileIsSandboxed(t *testing.T) {
	testutil.AssertSuiteGopiProfileIsPinned(t)
}

// ambientDaemonHandshakeEnv is the daemon handshake pair a worker-daemon CHILD
// inherits from its spawner: workerDaemonEnv (the identity marker) plus
// daemonReadyEnv (the handshake fd number). Both named constants live in this
// package (Rule 1b: no bare literal).
//
// WHY THE SUITE DROPS THEM (Rule 7 background): cmd/agent drives the worker
// both as a CHILD PROCESS (guardTestEnv strips them for those cases) and
// IN-PROCESS (runTaskWorker is called directly by the serve/worker-split tests,
// where no child env exists to strip). In-process, the pair makes THIS process
// look like a daemon child: runTaskWorker takes the daemon path
// (workerDaemonRun → workerDaemonInheritLock) and refuses with
// "inherited worker lock fd 3 is not a usable file" — measured 2026-09-13 on
// TestRunTaskWorker_OnceSweepsZombieFirst, which fails under a worker-daemon
// parent and passes when the pair is unset. The tests mean to be the FOREGROUND
// worker (a plain process with no inherited lock fd), so the suite states that
// environment instead of inheriting it. No test needs the daemon-child shape by
// ambient inheritance: the daemon tests set the marker deliberately
// (t.Setenv / an explicit child env), and worker_daemon_flag_e2e_test.go builds
// its child from the operator's shape on purpose.
var ambientDaemonHandshakeEnv = []string{workerDaemonEnv, daemonReadyEnv}

// unpinAmbientDaemonHandshake drops the daemon handshake pair this process may
// have inherited from a worker-daemon parent, so the suite runs in the
// FOREGROUND-worker shape its tests mean to exercise.
//
// No restore is attempted, matching PinSuiteNotifyEnv's precedent: TestMain
// os.Exit()s immediately after m.Run, so a restore would be unreachable code
// that only invites an env-leak-shaped review hit. NOT t.Setenv (Rule 17):
// TestMain is not a *testing.T. The failure is REPORTED, never swallowed
// (Rule 15b) — an ambient value left in place is exactly the leak this pin
// exists to close.
func unpinAmbientDaemonHandshake() {
	for _, name := range ambientDaemonHandshakeEnv {
		// env-leak-ok: TestMain has no *testing.T (t.Setenv is unavailable) and
		// the whole point is that the pair must be absent for EVERY test in the
		// suite, not restored per test; there is nothing to leak past, since
		// TestMain os.Exit()s immediately after m.Run (same shape as
		// testutil.PinSuiteNotifyEnv, whose own sites carry this reason).
		if err := os.Unsetenv(name); err != nil {
			fmt.Fprintf(os.Stderr, "testutil: %s: suite pin did not drop %s: %v\n", cmdAgentSuiteName, name, err)
		}
	}
}

// cmdAgentSuiteName labels this suite's scratch root.
const cmdAgentSuiteName = "cmd-agent"
