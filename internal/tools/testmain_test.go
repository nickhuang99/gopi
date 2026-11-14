package tools

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/signalrec"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// toolsSuiteName labels this suite's scratch root (its name appears in the
// run directory, so a leftover names its owner).
const toolsSuiteName = "tools"

// toolsBuildCacheSandboxName names the cache root this suite gives the merger
// BUILD gate for the length of a run.
const toolsBuildCacheSandboxName = "gopi-tools-suite-build-cache"

// toolsBuildCacheSandboxDir is the sandbox this run installed ("" when none
// engaged) — the path the suite removes after m.Run.
var toolsBuildCacheSandboxDir string

// toolsBuildCacheSandboxSignal holds the signal subscription installed by
// toolsReapBuildCacheSandboxOnSignal. A local would be collectable while the
// process is still alive, and a collected subscription is a signal handler that
// silently stops firing (the sandbox would then survive exactly the signal this
// arm exists for). "" when no sandbox engaged.
var toolsBuildCacheSandboxSignal chan os.Signal

// toolsPinnedChildLocale is the locale this suite pins onto the git children
// its fixtures spawn — and onto THIS process, which those children inherit.
//
// WHY THE SUITE PINS IT AT ALL. This package runs REAL git (a bare server repo
// + a client clone: "mocks cannot prove [git semantics]", the header of
// task_sync_git_pushback_test.go), and git emits its diagnostics in the
// caller's locale — the production lanes embed that stderr verbatim in the
// refusals an operator reads (finishCleanupReasonSeparator), so a test that
// asserts what the refusal carries must not depend on the host's ambient
// locale.
//
// MEASURED 2026-10-12 on host-b, `LC_ALL=zh_CN.utf8 LANG=zh_CN.utf8`:
// TestPushbackUnresolvedRemoteNameIsDiagnosedByTheRealLane failed with
//
//	the refusal must keep git's own words: "... ssh: Could not resolve hostname
//	ghost: Name or service not known\r\nfatal: 无法读取远程仓库。\n\n请确认您有正确的
//	访问权限并且仓库存在。 …"
//
// — the refusal DID keep git's sentence, in Chinese. The product behaved
// exactly as designed; the ASSERTION was the locale-dependent half. The same
// applies to every other English phrase this suite asserts about git's
// diagnostics: the failure mode belongs to the OPERATOR'S HOST, not to the code
// under test.
//
// WHY THE PIN LIVES IN TestMain AND NOT IN THE TEST. The suite's fixtures call
// `pbRun` (os.Environ()) and its assertions read git's stderr — but the PROCESS
// UNDER TEST also spawns git, through gitPushbackGitRun → gitPeerRun, which
// INHERITS this process's environment (its own doc: "The environment is
// inherited verbatim"). So the locale must be in force for the WHOLE binary,
// not for one test: pinning it here means ONE fix covers every git child this
// suite spawns, now and later — a new test cannot silently reintroduce the
// dependence.
//
// The value is the same "C" the production bash child uses (bashDefaultLocale),
// for the same reason: byte-stable, English diagnostics. Set with os.Setenv in
// TestMain because there is no *testing.T to scope it to; the harness restores
// the process environment itself, so nothing leaks into a sibling suite.
//
//	env-leak-ok: process-wide child-locale pin installed in TestMain (no *testing.T exists there); it replaces the inherited locale for the length of the run, which is the property under test — pbRun and the inherited-environment gitPeerRun both read it.
const toolsPinnedChildLocale = "C"

// toolsPinChildLocale forces the locale every git child of this suite runs
// under. LC_MESSAGES and LANGUAGE are stripped rather than set: LC_ALL wins
// over them in the reference implementation, but leaving a gettext variable
// pointing at a translation this suite did not choose is a second, silent
// input to a suite whose assertions quote English sentences verbatim
// (bashChildEnv strips exactly these four, for the same reason).
func toolsPinChildLocale() {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "LANG", "LC_ALL", "LC_MESSAGES", "LANGUAGE":
			// Unset first: the pins below are the ONE writer of these. Same
			// process-wide carve-out as the two Setenv calls below — the
			// stylegate test-setenv rule reports os.Setenv in a TEST FILE
			// (ceiling 0), and these are TestMain's, with no *testing.T to
			// scope them to.
			os.Unsetenv(name) //stylegate:ignore process-wide child-locale pin installed in TestMain (no *testing.T exists there) // env-leak-ok: TestMain-only, immediately followed by the pins below
		}
	}
	os.Setenv("LANG", toolsPinnedChildLocale)   //stylegate:ignore process-wide child-locale pin installed in TestMain (no *testing.T exists there); it replaces the inherited locale for the length of the run, which is the property under test (pbRun and the inherited-environment gitPeerRun both read it) // env-leak-ok: TestMain has no *testing.T to scope it to
	os.Setenv("LC_ALL", toolsPinnedChildLocale) //stylegate:ignore process-wide child-locale pin installed in TestMain (no *testing.T exists there); it replaces the inherited locale for the length of the run, which is the property under test (pbRun and the inherited-environment gitPeerRun both read it) // env-leak-ok: TestMain has no *testing.T to scope it to
}

// toolsPinBuildCacheSandbox points the gate's shared build cache at a throwaway
// directory for the WHOLE suite.
//
// Without it, a suite that exercises a real gate BUILD writes its cache into the
// developer's REAL <home>/workspace/gopi-build-cache/<repo-key>/ — measured: one
// targeted run of this package's gate tests left 110M of one-off per-test keys
// there. A test run must never populate the operator's cache, for the same
// reason it must never deliver into their ledger
// (testutil.PinSuiteLedgerEnv) or their notify sink (PinSuiteNotifyEnv): the
// cache is a real artifact of the operator's work, and the suite's fixtures are
// not.
//
// The sandbox must itself be on DISK AND OUTSIDE THE COLLECTED SCRATCH TREE:
// each candidate is MEASURED and any under RAM, on `/`'s device, or inside the
// scratch home is skipped — the same criterion the gate applies to the cache it
// is given (and the third one is why the suite's own scratch root cannot host
// it: the gate refuses that placement, correctly).
//
// It is set ONCE, in TestMain, before any test can reach the gate; a test that
// needs a different root overrides it with t.Setenv (Rule 17). t.Setenv is not
// available there — TestMain has no *testing.T — so the process-wide assignment
// is the documented exception:
//
//	env-leak-ok: TestMain installs the suite's process-wide sandbox before any
//	test runs; there is no *testing.T to scope it to, and the value is a
//	throwaway directory the suite removes after m.Run.
func toolsPinBuildCacheSandbox() {
	candidates := toolsBuildCacheSandboxCandidates()
	for _, dir := range candidates {
		if !toolsDirIsDiskSafe(dir) {
			continue
		}
		if err := os.MkdirAll(dir, scratch.DirPerm); err != nil {
			continue
		}
		toolsBuildCacheSandboxDir = dir
		os.Setenv(scratch.GoCacheRootEnvName, dir) //stylegate:ignore process-wide suite sandbox installed in TestMain, where no *testing.T exists to scope it to; the directory is a throwaway removed after m.Run // env-leak-ok: process-wide suite sandbox installed in TestMain (no *testing.T exists there); throwaway directory removed after m.Run
		return
	}
	// Rule 15b: a sandbox that did not engage must not pass silently — the
	// consequence is a test run writing into the operator's real cache.
	fmt.Fprintf(os.Stderr, "tools suite: no disk-backed build-cache sandbox could be created (tried %s) — a gate build in this suite would write into the operator's real cache\n", strings.Join(candidates, ", "))
}

// toolsBuildCacheSandboxCandidates is the ORDERED list of places this suite
// offers the gate for the length of a run, each judged by toolsDirIsDiskSafe.
//
// IT IS A LIST BECAUSE THE HOST DECIDES, and the host that broke the first
// version is the ordinary unprivileged CLIENT (measured 2026-10-11, host
// host-a, the run that filed task 01a12b54): there the two historical
// candidates are BOTH refused by the very rules this arm exists to honour —
// the scratch run root sits on /dev/shm (RAM: the 2026-09-15 exhaustion, ban 2)
// and $HOME/.cache shares the `/` device (Linux with a single large root
// filesystem, ban 1). With no third candidate the arm had nothing to engage, so
//
//	(a) the suite guard TestSuiteBuildCacheSandboxIsPinned was RED, and
//	(b) the merger BUILD gate — given no pin — resolved its own default root
//	    under the test worktree root, landed in the collected scratch tree, and
//	    ABSTAINED, which is what turned the whole merge/gate family red.
//
// THE THIRD CANDIDATE is the directory the PRODUCTION gate resolves by default
// for a real repo — <workspace home>/workspace/gopi-build-cache — carrying the
// suite's own sandbox name and this run's pid. That tree is the ONE place that is
// off `/` on a single-partition host AND outside the collected scratch tree, so
// the GATE accepts it as well as this arm does, which is the property that
// matters: a sandbox the gate refuses leaves the suite proving nothing but its
// own abstention. It is deliberately a CHILD of that directory (never the
// per-repo key the gate writes for a real repo): the suite creates it, the suite
// removes it in toolsRemoveBuildCacheSandbox, and the operator's real cache
// directories sit beside it untouched.
//
// The USER-CACHE candidate stays LAST rather than second: it is legal only where
// $HOME is on its own filesystem, and on those hosts the gate refuses it for the
// same reason the suite's own placement rules would (same device as `/`). Ranking
// a root the gate refuses ABOVE one it accepts is how the two halves came to
// disagree in the first place.
//
// The per-run pid suffix keeps concurrent suites from sharing (and deleting) one
// another's sandbox.
func toolsBuildCacheSandboxCandidates() []string {
	candidates := []string{filepath.Join(testutil.SuiteTempRoot(), toolsBuildCacheSandboxName)}
	candidates = append(candidates, filepath.Join(testutil.WorkspaceHome(), worktreeroots.WorkspaceDirName,
		worktreeroots.SharedBuildCacheDirName, fmt.Sprintf("%s-%d", toolsBuildCacheSandboxName, os.Getpid())))
	if base, err := os.UserCacheDir(); err == nil {
		candidates = append(candidates, filepath.Join(base, fmt.Sprintf("%s-%d", toolsBuildCacheSandboxName, os.Getpid())))
	}
	return candidates
}

// toolsRemoveBuildCacheSandbox removes the sandbox this run installed (a
// long-lived host must not accumulate one directory per test run). A removal
// failure is REPORTED, never swallowed (Rule 15b).
func toolsRemoveBuildCacheSandbox() {
	if toolsBuildCacheSandboxDir == "" {
		return
	}
	if err := os.RemoveAll(toolsBuildCacheSandboxDir); err != nil {
		fmt.Fprintf(os.Stderr, "tools suite: build-cache sandbox %s not removed: %v\n", toolsBuildCacheSandboxDir, err)
	}
}

// toolsReapBuildCacheSandboxOnSignal reaps the sandbox when a TERMINATING signal
// ends this run — the exit path that produced the residue this arm exists for.
//
// MEASURED 2026-10-12 on this host (task 01a13032-4c50): the shared build-cache
// root held 686 `gopi-tools-suite-build-cache-<pid>` directories / 1.9 GiB and
// not one named a live process. They are not the clean path's output — a clean
// run creates its sandbox and removes it 2.7 s later (measured: one directory
// created and removed inside a single `make test-budget` run) — they are the
// output of runs that DIED. A clean exit runs toolsRemoveBuildCacheSandbox after
// m.Run; a signal skips the defer stack AND that statement, which is why a
// signal handler is the only in-process exit that can clean up.
//
// WHAT DELIVERS EACH KIND OF ENDING — MEASURED on this host, not assumed:
//
//	clean exit -> toolsRemoveBuildCacheSandbox after m.Run reaps it. Measured:
//	              one `make test-budget` run created its sandbox and removed it
//	              2.7 s later; nothing survived.
//	external signal (SIGTERM/SIGINT/SIGHUP/SIGQUIT) -> THIS handler. Measured:
//	              RED without it (the sandbox survived the kill and the child
//	              never died — Notify disables the default action), GREEN with it,
//	              and the child still ends BY SIGTERM.
//	the suite's own timeout clock -> NOT reaped, and no in-process code can reap
//	              it. scripts/test-budget.sh passes ONE budget as `go test
//	              -timeout` (25m; the only reader of GOPI_TEST_TIMEOUT) and a bare
//	              `go test ./...` gets the toolchain's 10m default. MEASURED how
//	              that path ends: `tools.test -test.timeout 4s` leaves `$? = 2` —
//	              the testing package stops the binary with os.Exit(2), NOT with a
//	              signal — and a `go test -timeout 4s` run of the same case left
//	              its sandbox on disk with no handler line in its output. A
//	              budget-expired run therefore strands its shell in-process, and
//	              the 2026-10-12 census's shells are preponderantly this kind (662
//	              of 686 created on the single day the suite ran under the budget).
//	SIGKILL, and a WEDGED run whose signal never arrives -> nothing. A kill that
//	              cannot be handled leaves an empty, pid-named shell; reclaiming
//	              that is a SWEEP's job (Rule 35 — no second reaper, no new
//	              dependency), not this function's.
//
// WHY IT IS NOT A DEFER: a signal does not run the defer stack, so nothing in
// this function's callers could reach it.
//
// THE RE-RAISE IS THE ACTIVE HALF, AND IT IS NOT DECORATION (measured: the first
// version of this arm hung the run for 90s and then failed). signal.Notify
// DISABLES a signal's default action — it does not merely observe it — so
// returning from the handler leaves the process RUNNING: a TERMed run would
// clean up its sandbox and then never die, turning "the budget killed a long
// run" into "the budget killed nothing". After the sandbox is gone the
// subscription is stopped and the SAME signal is raised again at this process,
// which is what makes the run die with the signal's own status — the status a
// caller saw before this arm existed (a `go test -timeout` kill reports timeout
// only if the terminal state is the one it sent).
func toolsReapBuildCacheSandboxOnSignal() {
	if toolsBuildCacheSandboxDir == "" {
		return
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP, syscall.SIGQUIT)
	go func() {
		sig, ok := <-ch
		if !ok {
			return
		}
		fmt.Fprintf(os.Stderr, "tools suite: %v — removing the build-cache sandbox %s this run installed\n",
			sig, toolsBuildCacheSandboxDir)
		toolsRemoveBuildCacheSandbox()
		// Stop first, re-raise second: without the Stop the re-raise would be
		// delivered straight back to this channel and the loop would never end.
		signal.Stop(ch)
		toolsReraise(sig)
	}()
	// The channel is held in a package var rather than dropped: a signal
	// subscription the garbage collector can collect is a subscription that can
	// disappear before the signal it exists for (the same reason
	// toolsBuildCacheSandboxDir is a package var and not a local).
	toolsBuildCacheSandboxSignal = ch
}

// toolsReraise re-sends sig to this process with its ORIGINAL disposition
// restored, which is the part that actually ends the run (see the caller's
// header: signal.Notify disables the default action, so a handler that only
// observes leaves the process alive forever).
//
// It returns only if the process survives the signal — a caller that is still
// running after this must not continue as though it had died, so the failure is
// reported (Rule 15b) rather than swallowed.
//
// NO `signal.Reset` (PROJECT.md Rule 42): Reset is `cancel(sig, disableSignal)`
// over EVERY registered channel, so it dismantles a subscription another package
// installed as a side effect of this arm's own exit. What replaces it is a
// RELEASE: every subscriber that means to die drops its own registration, and
// whatever is left is entitled to receive the raise.
//
// MEASURED on this host (2026-10-24), because the honest answer here is not the
// tidy one. This arm has a PEER registration in a `go test` binary —
// internal/testutil/scratchroot.go arms the suite-root reap handler
// (signal.Notify over SIGTERM) for every suite through PinSuiteForTestMain — and
// the child-suite scenario
// (TestKilledSuiteRunReapsItsBuildCacheSandbox, which reads WaitStatus) was run
// in three shapes:
//
//	as committed (Reset)                       → child died by SIGTERM
//	Reset removed, no release at all           → child died by SIGTERM
//	Reset removed + this release (as below)    → child died by SIGTERM
//
// So the DEATH does not depend on this function at all: when a peer channel is
// still registered the raise reaches it, and that peer's own fatal path
// (internal/testutil's reraiseSignal, which stops its own channel and re-raises
// with no Reset) ends the process — which is the ordered outcome, and the reason
// this arm can drop Reset without becoming the unkillable shape Rule 42 exists to
// prevent. The release below is therefore stated as what it is: it stops the
// RECORDER's process-lifetime registration when one exists (a gopi child), so the
// raise reaches the default disposition or that peer instead of vanishing into a
// recorder that has already written its line. It is NOT load-bearing for the
// suite's exit, and saying otherwise would be an unmeasured claim.
func toolsReraise(sig os.Signal) {
	_ = signalrec.ReleaseSignalHandlers()
	if s, ok := sig.(syscall.Signal); ok {
		if err := syscall.Kill(os.Getpid(), s); err != nil {
			fmt.Fprintf(os.Stderr, "tools suite: re-raising %v did not reach this process: %v\n", sig, err)
		}
	}
}

// toolsDirIsDiskSafe reports whether a directory is a place a build cache may
// live: off RAM, off the `/` filesystem, and OUTSIDE the collected scratch tree
// — MEASURED (a spelling proves nothing, and the suite's scratch home is reached
// through a symlink).
//
// IT ASKS THE GATE'S OWN PREDICATES (Rule 14), because the property that decides
// whether this pin is worth anything is "the merger BUILD gate accepts a cache
// under this path": the suite and the gate must not be able to disagree about
// where a cache may live. The three bans used to be re-spelled here (a
// hand-rolled memory check plus a device compare against the directory's PARENT),
// and the re-spelling had two defects of its own: measuring the PARENT made a
// not-yet-created candidate unmeasurable, and sharesRootFS-alone does not know
// that a path under the scratch home is collected at any age. The predicates
// below classify a path that does not exist yet by its nearest EXISTING ancestor,
// which is exactly the question (a directory cannot be a mount point before it
// exists).
//
// THE THIRD HALF, AND WHY IT IS HERE (2026-09-18). The cache's placement rules
// are exactly these three (the gate enforces the same set): off `/`
// (scratch.AssertGoCacheOffRootFS — the 2026-09-11 exhaustion), off RAM
// (scratch.AssertGoCacheOffShm — the 2026-09-15 one, whose cache lived in the
// scratch class), and outside the scratch HOME (scratch.IsInScratchTree —
// scratch is designed to be COLLECTED, so a reusable cache may not live in it).
//
// While the scratch home was a tmpfs, the second rule also refused the first
// candidate below, so this sandbox landed on os.UserCacheDir by ACCIDENT. 524156af
// retired the memory backing and the accident cannot fire any more: the
// framework temp root is now block-backed, it passed the two device checks, and
// the suite pinned a cache root inside the tree the age GC collects — whereupon
// the gate (correctly) refused it and every test that relies on this pin
// ABSTAINED. The candidate list must therefore MEASURE the tree, not inherit the
// tmpfs era's accident.
// toolsDirIsDiskSafe applies the gate's THREE placement bans to a candidate arm
// directory — one call per ban, never a re-spelling (see above): a caller that
// keeps its own version of a ban is a caller that can disagree with the gate
// about it. Both device bans resolve a not-yet-created path to its nearest
// EXISTING ancestor, so a candidate that does not exist yet is still measurable
// (and the suite calls this BEFORE it creates anything).
func toolsDirIsDiskSafe(dir string) bool {
	if err := scratch.AssertGoCacheOffRootFS(dir); err != nil {
		return false
	}
	if err := scratch.AssertGoCacheOffShm(dir); err != nil {
		return false
	}
	return !scratch.IsInScratchTree(dir)
}

// TestSuiteBuildCacheSandboxIsPinned is the guard on the arm above: the suite
// must never let a gate build fall back to the operator's real cache root. It is
// the same shape as the notify/profile hermeticity guards — an entry-point edit
// that drops the pin would otherwise be invisible from inside the suite.
func TestSuiteBuildCacheSandboxIsPinned(t *testing.T) {
	root := os.Getenv(scratch.GoCacheRootEnvName)
	if root == "" {
		t.Fatalf("%s is unset: a gate build in this suite would write into the operator's real <home>/workspace/%s",
			scratch.GoCacheRootEnvName, worktreeroots.SharedBuildCacheDirName)
	}
	if !strings.Contains(root, toolsBuildCacheSandboxName) {
		t.Fatalf("%s=%q is not this suite's sandbox (%s)", scratch.GoCacheRootEnvName, root, toolsBuildCacheSandboxName)
	}
	if !toolsDirIsDiskSafe(filepath.Dir(root)) {
		t.Fatalf("the build-cache sandbox root %s is on RAM, on the / device, or inside the collected scratch tree %s — the gate refuses all three, so the suite would prove nothing but its own abstention",
			root, scratch.HomeDir())
	}
}

// TestMain: scratch-root isolation + notify-env hermeticity, both installed by
// the shared entry point (testutil.PinSuiteForTestMain).
//
// The notify half used to live HERE as a five-variable Unsetenv loop, on the
// theory that dropping GOPI_TASK_NOTIFY_* made the suite hermetic. It did not:
// config.ResolveNotify resolves env > settings.json, and the durable layer reads
// ResolveSettingsPath() — the ambient GOPI_SETTINGS, i.e. the operator's real
// ~/.gopi/settings.json, whose notify.cmd appends to the LIVE sink
// (~/.gopi/notify/task-events.jsonl). Measured 2026-09-12: every test that walks
// the task state machine delivered there (one unrelated test alone wrote 655
// bytes; a full suite run flooded ~200 test task-ids in two minutes). The pin
// now also points GOPI_SETTINGS at an empty per-run sandbox path, so the durable
// layer resolves to nothing and a test that wants delivery must name it. Root
// cause + the guard: internal/testutil/suiteenv.go.
func TestMain(m *testing.M) {
	testutil.PinSuiteForTestMain(toolsSuiteName)
	// BOTH ARE Process-Wide Pins, deliberately set here and in this order: the
	// git child locale first (every git child below inherits it — including the
	// children the process under test spawns), then the build-cache sandbox.
	toolsPinChildLocale()
	toolsPinBuildCacheSandbox()
	// The signal handler for the sandbox armed above — the exit path that does
	// NOT run the removal after m.Run (see toolsReapBuildCacheSandboxOnSignal).
	toolsReapBuildCacheSandboxOnSignal()
	// The READ side of the same isolation (t-1788361069 round 3): the
	// scratch-root pin above stops the suite's throwaway DBs from fsyncing
	// the boot device, but a test that pins only GOPI_MEMORY_DB still leaves
	// config.GopiDir() pointing at the developer's REAL profile — and
	// `task show`'s session resolution reads EVERY session.jsonl under
	// ~/.gopi/sessions in its search for one id. That cost is linear in the
	// ambient tree size (measured 18.8s / 84.3s / 272.9s at 0 / 4k / 16k
	// entries for this suite's slowest test) and is the whole-suite hang that
	// survived the fsync fix. Pin an EMPTY per-run profile: a test that needs
	// a session file or the real ledger names its own GOPI_HOME.
	testutil.PinSuiteGopiProfile(toolsSuiteName)
	code := m.Run()
	toolsRemoveBuildCacheSandbox()
	// The pinned scratch root is per-run throwaway — remove it so a
	// long-lived host does not accumulate scratch entries.
	testutil.FinishSuiteForTestMain()
	os.Exit(code)
}

// TestSuiteNotifyEnvIsHermetic is this suite's one-line guard on the shared
// invariant: the suite must resolve NO notify target and must not carry an
// ambient settings path. Without it a future entry-point edit silently reopens
// the live-sink leak — a leak that is invisible from inside the suite, which is
// exactly why it survived the first fix.
func TestSuiteNotifyEnvIsHermetic(t *testing.T) {
	testutil.AssertSuiteNotifyEnvHermetic(t)
	testutil.AssertSuiteHasNoNotifyTarget(t)
}

// TestSuiteGopiProfileIsSandboxed is the guard on the READ half of the suite's
// isolation (t-1788361069 round 3): the suite's gopi data root must be an
// EMPTY sandbox inside the pinned scratch root, so no audit lookup walks the
// developer's real profile. Dropping PinSuiteGopiProfile from TestMain makes
// this red, and so does a fixture that leaks a session file into the sandbox.
func TestSuiteGopiProfileIsSandboxed(t *testing.T) {
	testutil.AssertSuiteGopiProfileIsPinned(t)
}

// TestSuiteLedgerIsSandboxed is the guard on the LAST ambient root: without it
// a test that never names GOPI_MEMORY_DB opens the operator's REAL ledger, and
// the ledger is APPEND-ONLY — a fixture row written there is permanent. This is
// the defect that left `t-stale-sibling` (deliverable naming the never-created
// /tmp/other-repo) in the live ledger, where the merger's dead-holder reclaim
// reported it on every pass (devdoc/reclaim-fossil-20260915.md).
func TestSuiteLedgerIsSandboxed(t *testing.T) {
	testutil.AssertSuiteLedgerIsPinned(t)
	// ...and INSIDE THIS SUITE'S OWN PROFILE, which the guard above cannot see:
	// it asks only "is this the operator's real file?", and a ledger pinned
	// BESIDE the profile (a sibling directory) passes that question while still
	// making every event this suite fires FOREIGN to taskNotifyDelivers — which
	// then suppresses it, so a notify test fails on the absence of the log it
	// asked for (measured 2026-11-04, task 01a1a647: four of them in this
	// package).
	testutil.AssertSuiteLedgerIsInsideItsOwnProfile(t)
}
