// Package testutil — shared suite scratch-root isolation + the mechanical
// BAN on /tmp (and on the root filesystem in general) as a test/dev runtime
// workspace.
//
// ROOT CAUSE THIS FILE EXISTS FOR (three incidents, one mechanism):
//
//  1. The internal/tools suite hang (t-1788361069, round 2): every
//     t.TempDir() lands under TMPDIR, whose system default is /tmp — the
//     BOOT DEVICE (measured fsync latency 49-225ms there, with multi-second
//     stalls under concurrent-agent I/O). SQLite fsyncs the database file at
//     the WAL close-time checkpoint; ONE stalled fsync holds the
//     process-wide modernc/sqlite lock and freezes every later open behind
//     it, so the suite dies at whatever test happened to be running.
//  2. The internal/agent flake this pin also fixes
//     (TestT2_E3_ResumeCarriesForwardCommittedConclusions): write_task_area
//     bounds its whole DB path — COLD-FILE CREATION + full knowledge-base
//     schema init (~10-17ms idle, 40+ DDL statements) + the insert — by a
//     single deadline (tools.taskAreaDBTimeout). On the boot device that
//     cold creation stalls past the budget, and the call fails with
//     "write_task_area: schema: context deadline exceeded" (or the open-step
//     variant without the "schema:" prefix). Full analysis:
//     devdoc/write-task-area-schema-timeout.md (5-why + A/B measurements).
//  3. The peer1 resource exhaustion (2026-09-11, devdoc/
//     peer1-resource-exhaustion-20260911.md): an unbounded `while :; do dd
//     ... conv=fsync; done` stress loop wrote into /tmp while suites wrote
//     13,044 abandoned scratch directories there, taking `/` from 19G free
//     to 1.5G free with irreversible D-state processes. The lesson recorded
//     then — "一切临时产物必须走 /home 或 ~/workspace，绝不落到 /tmp" — is
//     what this file now enforces MECHANICALLY (not by convention).
//
// THE CONTRACT (three parts, all enforced here):
//
//	A. LOCATION — a suite's scratch root is <home>/workspace/gopi-test-scratch/
//	   <suite>/<run>, and the home is NEVER os.TempDir(). The location is
//	   asserted, hard: AssertSuiteScratchRootSafe fails a suite whose scratch
//	   root shares a device with `/` (i.e. /tmp).
//	B. BACKING — the scratch home is a REAL DIRECTORY on the block device
//	   (operator ruling, 2026-09-18: "just for a few ms performance? no, no
//	   no! don't use tmpfs. change settings to workspace. now"). The tmpfs
//	   route is RETIRED: nothing is mounted, nothing is linked, and no refusal
//	   in this tree prescribes it any more. The cost is real and was measured
//	   before the ruling (601s vs 237s for internal/tools on ext4 /home vs
//	   tmpfs; 411ms vs 16.8ms per fresh ledger DB) and the operator accepted it
//	   deliberately. What stays enforced is the half that is not a preference:
//	   scratch is never on the `/` device (/tmp's fsync problem AND the
//	   2026-09-11 exhaustion).
//	C. BOUNDED — scratch is age-GC'd (>ScratchStaleAge) and never deleted
//	   while a live process holds it (/proc/<pid>/cwd + /proc/<pid>/fd).
//
// Both packages' TestMain pin TMPDIR through this ONE implementation
// (Rule 14: the pin, its override and its statfs classification used to live
// in internal/tools alone, which is exactly why internal/agent kept flaking
// on the same boot-device fsync) — see internal/tools/testmain_test.go
// (hang guard + ratio-attributed stall ceiling) and
// internal/agent/testmain_isolation_test.go (pin assertion).
package testutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

const (
	// TempDirEnvName is the standard environment variable every
	// testing.T.TempDir() / os.MkdirTemp("") consumer honours. Pinning it is
	// the whole mechanism — no test needs to know its scratch data moved.
	TempDirEnvName = scratch.TmpDirEnvName
	// SuiteTempRootEnvName lets an operator pin where a suite's scratch data
	// goes (e.g. a dedicated fast volume). When unset, the workspace scratch
	// home is used. Read ONLY through SuiteTempRootOverride (Rule 2).
	SuiteTempRootEnvName = "GOPI_TEST_TMPDIR"
	// ShmDir is the standard Linux shared-memory filesystem — a tmpfs, so no
	// fsync ever reaches a block device. It is NOT a scratch target (a
	// tmpfs is small and shared); it stays referenced for the legacy
	// migration reader in the suite tests. The VALUE has one definition
	// (worktreeroots.ShmDir, Rule 14); this name is the suite-facing alias.
	ShmDir = worktreeroots.ShmDir
	// SuiteTempDirPattern names the per-run scratch root.
	SuiteTempDirPattern = "gopi-suite-tmp-"

	// WorkspaceHomeEnvName overrides the home directory whose
	// <home>/workspace holds the scratch home. It exists so tests can point
	// the whole scratch tree at a fixture (Rule 2: ONE reader, below).
	// The VALUE is defined once, in internal/scratch (Rule 14).
	WorkspaceHomeEnvName = scratch.WorkspaceHomeEnvName
	// WorkspaceDirName is the shared workspace directory under the home —
	// the ONLY filesystem tree a suite's bulk output may land in.
	WorkspaceDirName = scratch.WorkspaceDirName
	// ScratchRootDirName is the fixed scratch home under the workspace. One
	// well-known directory means one GC target, one disk-usage accounting
	// site, and one obvious thing to delete by hand in an emergency.
	ScratchRootDirName = scratch.RootDirName
	// ScratchQueueDirName holds suite-less, large, throwaway artifacts
	// (load-test files, dumps) that must still be age-GC'd. Same home, same
	// GC — but not a pin target, so concurrent suites never delete each
	// other's queue entries out from under a running load generator.
	ScratchQueueDirName = scratch.QueueDirName

	// ScratchGCIntervalEnvName (alias of scratch.GCIntervalEnvName) is the
	// sweep's cadence knob, in minutes. It is read in exactly one place,
	// scratchGCInterval (cmd/agent/scratch_gc_tick.go), which gates
	// scratchGCTick; the production door to that tick is the `--tick` arm of
	// `gopi tool scratch-gc`. CORRECTED 2026-10-30: this used to say "read by
	// the RESIDENT tick (cmd/agent's worker/merger)", and no resident component
	// calls it any more (worker round 187d05c; merger tick deleted 2026-09-19).
	// 0 disables it.
	ScratchGCIntervalEnvName = scratch.GCIntervalEnvName

	// tmpfsMagic / ramfsMagic are ALIASES of the ONE definition in
	// internal/scratch (Rule 14): production code asks the same question
	// (AssertGoCacheOffShm refuses a shared build cache on RAM), and the
	// classification must not exist twice.
	tmpfsMagic = scratch.TmpfsMagic
	ramfsMagic = scratch.RamfsMagic
	// ext4Magic names the block-backed case in the failure text, so the
	// reader is told WHICH filesystem is at fault (rule 15: no guessing).
	ext4Magic = 0xef53

	// dirPerm / filePerm are the standard owns-everything modes for the
	// scratch tree. They are ALIASES of the one definition in
	// internal/scratch (Rule 14: the tree's writer, its sweep and its tests
	// must agree on one value, and internal/scratch is the package both can
	// import — the sweep itself moved there so production code can trigger it).
	dirPerm  = scratch.DirPerm
	filePerm = scratch.FilePerm

	// SuiteScratchUnsafeExitCode is the process exit code a suite returns
	// when its scratch root lands on the root filesystem. It is a distinct,
	// documented code so a CI wrapper can recognise the refusal.
	SuiteScratchUnsafeExitCode = 97
	// SuiteScratchNoSpaceExitCode is the process exit code a suite returns when
	// its chosen scratch root has too little free space to run in. It is
	// separate from SuiteScratchUnsafeExitCode because the two need different
	// fixes (relocate the root vs. give the root room), and a wrapper that reads
	// the status alone must be able to tell them apart.
	SuiteScratchNoSpaceExitCode = 98
)

// ErrScratchRootOnRootFS is the sentinel a caller can match with errors.Is
// when a scratch root shares a device with `/` (Rule 1b: the message is
// built from constants, never a bare literal at every call site).
var ErrScratchRootOnRootFS = errors.New("scratch root is on the same filesystem as /")

// suiteTempRoot is the scratch root PinSuiteTempRoot pinned TMPDIR to
// ("" when the pin did not engage).
var suiteTempRoot string

// scratchEnvOnce guards the one-time environment preparation the pin does.
var scratchEnvOnce sync.Once

// SuiteTempRootOverride is the ONE place GOPI_TEST_TMPDIR is read (Rule 2).
func SuiteTempRootOverride() string {
	return os.Getenv(SuiteTempRootEnvName)
}

// WorkspaceHome is the home whose workspace holds the scratch home. The
// GOPI_TEST_HOME override is read HERE and nowhere else (Rule 2).
func WorkspaceHome() string {
	if home := os.Getenv(WorkspaceHomeEnvName); home != "" {
		return home
	}
	return workspaceHomeFromOS()
}

// workspaceHomeFromOS is the no-override branch: os.UserHomeDir (which honours
// $HOME) with $HOME as the last resort. Split out so the override test can
// exercise both branches without unsetting the environment of the whole
// process (the value is read per call, never cached — Rule 2: one reader).
func workspaceHomeFromOS() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return os.Getenv("HOME") // last resort; still never os.TempDir()
	}
	return home
}

// ScratchHomeDir is the tree every suite's scratch data lives in:
// <workspace-home>/workspace/gopi-test-scratch. It is under the workspace on
// purpose — the `/` partition (147G, 87% full, and the partition the
// 2026-09-11 exhaustion filled) is never a scratch target, while the home
// workspace is the one tree the whole repo already uses for bulk artifacts.
func ScratchHomeDir() string {
	if override := SuiteTempRootOverride(); override != "" {
		// An operator pin is a scratch HOME in the sense GC needs: a tree
		// whose subdirectories are this suite system's throwaway runs.
		return scratch.CanonicalPath(override)
	}
	return scratch.CanonicalPath(filepath.Join(WorkspaceHome(), WorkspaceDirName, ScratchRootDirName))
}

// ScratchRootQueue returns the suite-less large-artifact queue inside the
// scratch home. Callers that keep multi-hundred-MB files alive on purpose
// (load generators, dumps) MUST use it: it is under the GC'd scratch home
// and never on `/`.
func ScratchRootQueue() string {
	return filepath.Join(ScratchHomeDir(), ScratchQueueDirName)
}

// SuiteTempRootDir picks the scratch parent: an explicit operator override
// first, else the workspace scratch home. There is ONE parent — the scratch
// home itself (see ScratchHomeMountHint): the memory-backed base this used to
// resolve onto is RETIRED (operator ruling, 2026-09-18), and os.TempDir() is
// never a candidate.
func SuiteTempRootDir() string {
	return ScratchHomeDir()
}

// SuiteTempRoot is the root the suite's scratch data actually lands on: the
// pinned root once PinSuiteTempRoot engaged, else the ambient temp root.
func SuiteTempRoot() string {
	if suiteTempRoot != "" {
		return suiteTempRoot
	}
	return os.TempDir()
}

// SuiteTempRootPinned reports whether a pin engaged at all.
func SuiteTempRootPinned() bool { return suiteTempRoot != "" }

// PinSuiteTempRoot points TMPDIR at a per-run directory inside the scratch
// home and GCs stale scratch in the same breath (the sweep is what keeps the
// home bounded). It is called by every suite's TestMain BEFORE any test
// runs, so no test needs to know its scratch data moved.
//
// Errors are REPORTED, never swallowed (Rule 15b) — and never fatal: a suite
// whose scratch home is unavailable should still run, on the ambient root,
// rather than fail for an infrastructure reason. The location and backing
// assertions (AssertSuiteScratchRootSafe / TestSuiteScratchRootIsMemoryBacked)
// are what turn a bad root into a visible failure.
func PinSuiteTempRoot(suite string) {
	scratchEnvOnce.Do(prepareScratchEnv)
	home := ScratchHomeDir()
	if override := SuiteTempRootOverride(); override != "" {
		// For a test that points the scratch home at its own fixture: pin
		// INSIDE the fixture so the test cannot write into (or GC-collect
		// from) the real scratch tree (Rule 6: a test never touches the
		// developer's real state).
		home = filepath.Join(override, fixtureRunDirName(suite))
		if err := os.MkdirAll(home, dirPerm); err != nil {
			fmt.Fprintf(os.Stderr, "testutil: fixture scratch home %s: %v\n", home, err)
			return
		}
	}
	if n, err := GCStaleScratchRoot(); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: scratch GC: %v\n", err)
	} else if n > 0 {
		fmt.Fprintf(os.Stderr, "testutil: scratch GC removed %d stale entr%s older than %v\n",
			n, pluralY(n), scratch.ResolveStaleAge())
	}
	// The SECOND sweep: roots left in the shared memory base by the pin that
	// predates the scratch home (see legacy_residue.go). It is namespace-
	// limited and age/holder-bounded the same way, and running it on every pin
	// is what bounds the residue without a watchdog.
	if n, err := GCLegacyScratchResidue(); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: legacy %s GC: %v\n", legacyResidueLabel, err)
	} else if n > 0 {
		fmt.Fprintf(os.Stderr, "testutil: legacy %s GC removed %d stale entr%s in %v older than %v\n",
			legacyResidueLabel, n, pluralY(n), MemoryBase(), scratch.ResolveStaleAge())
	}
	if err := os.MkdirAll(home, dirPerm); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: fixture scratch home %s: %v\n", home, err)
		return
	}
	// WHERE this run's throwaway trees go (scratch_placement.go): a root the
	// CALLER pinned keeps its location (the explicit layer beats the ambient
	// default); otherwise a memory-backed root is preferred over the device the
	// scratch home sits on, because a fresh ledger's first open pays that
	// device's journal commits. Either way the placement is reported, so a
	// device-backed run is never quiet about what its ledgers pay.
	pinned := ""
	if SuiteTempRootOverride() != "" {
		pinned = home
	}
	placement := scratchPlacementFor(pinned, config.TmpDirFromCallerEnv())
	scratchPlacementDecision = placement
	if placement.dir != "" {
		home = placement.dir
	}
	if err := os.MkdirAll(home, dirPerm); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: scratch root home %s: %v\n", home, err)
		return
	}
	reportScratchPlacement(suite, home, placement)
	// Canonicalise BEFORE pinning TMPDIR: the scratch home may be a symlink
	// (the stopped scripts/setup-scratch-home.sh used to link
	// <home>/workspace/gopi-test-scratch → /dev/shm/gopi-test-scratch on
	// unprivileged hosts; that backing is RETIRED by the 2026-09-18 ruling, the
	// linkage it left may not be). getcwd(2) — and therefore os.Getwd() — always
	// reports the RESOLVED path, so a TMPDIR spelled through the symlink
	// makes every test that compares a chdir'd cwd against its own t.TempDir()
	// path red (cmd/agent TestResumeChdirToWorktree) — a symlink would
	// otherwise silently become a test-breaking difference.
	home = scratch.CanonicalPath(home)
	// The BOUNDED PREFLIGHT (Rule 15): report a root with too little room
	// BEFORE anything is written into it. Without it the run fails later as an
	// unrelated-looking error about a path the suite itself just created —
	// measured on this host: `repo "/dev/shm/suite-cmd-agent-run-<N>/…" is not a
	// git work tree` while /dev/shm had 160 MiB free. The report is loud and
	// measured (`assertScratchRootHasRoom`), and the run still proceeds when a
	// root can be created: a small suite may well fit, and refusing here would
	// turn a warning into a new failure mode of its own.
	if err := assertScratchRootHasRoom(home); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: suite %q: %v\n", suite, err)
	}
	root, err := os.MkdirTemp(home, scratchRunDirName(suite))
	if err != nil {
		// The HARD half of the guard: no root means no run, and the failure
		// names the path, the filesystem and the free bytes rather than leaving
		// the suite to die of a misleading error later (Rule 15).
		msg, code := scratchRootCreationRefusal(suite, home, err)
		fmt.Fprint(os.Stderr, msg)
		os.Exit(code)
	}
	if err := os.Setenv(TempDirEnvName, root); err != nil {
		_ = os.RemoveAll(root) //stylegate:ignore best-effort temp cleanup; the OS/harness sweeps leftovers too
		fmt.Fprintf(os.Stderr, "testutil: pinning %s: %v\n", TempDirEnvName, err)
		return
	}
	registerScratchHolder(root)
	// The SIGNAL exit path: a run ended by SIGTERM/SIGINT/SIGHUP/SIGQUIT skips
	// the defer stack and the post-m.Run cleanup, and measured on this host
	// (2026-10-22) that is the shape all 19 `/dev/shm/suite-*-run-*` roots had —
	// every one registered a pid that no longer exists. The handler below removes
	// THIS run's root, citing its own `.holder` registration, so it can never
	// reach another run's live root in the shared base.
	armSuiteRunRootReapOnSignal(root)
	suiteTempRoot = root
}

// runSuffixPattern separates the suite name from the run's random suffix.
const runSuffixPattern = "-run-"

// ScratchHomeEnvName publishes the scratch home to child processes.
const ScratchHomeEnvName = "GOPI_TEST_SCRATCH_HOME"

// scratchRunDirName is the per-run directory name pattern for a suite: the
// suite name is kept in the name so a stray directory in the scratch home
// names its owner, and the run suffix keeps concurrent runs apart.
func scratchRunDirName(suite string) string {
	if suite == "" {
		suite = unknownSuiteName
	}
	return "suite-" + suite + runSuffixPattern
}

// fixtureDirPrefix names the per-run directory a caller-pinned scratch HOME gets
// (see PinSuiteTempRoot): the fixture keeps the run's root one level BELOW the
// override, so a test that points the scratch home at its own tree can never have
// the real scratch home's contents mixed into it.
const fixtureDirPrefix = "fixture-"

// fixtureRunDirName is the ONE definition of that directory's name, shared by the
// pin that creates it and the scope resolver that must reach into it (Rule 13: a
// name that two places compute is a name that will drift).
func fixtureRunDirName(suite string) string {
	return fixtureDirPrefix + scratchRunDirName(suite)
}

// unknownSuiteName labels a pin that did not name its suite.
const unknownSuiteName = "unnamed"

// prepareScratchEnv publishes the scratch home to child processes (a suite
// that shells out to `go test` gives its grandchildren the same home, so a
// grandchild's scratch root stays attributable and GC-visible instead of
// defaulting to /tmp) — once per process, best-effort.
func prepareScratchEnv() {
	if err := os.Setenv(ScratchHomeEnvName, ScratchHomeDir()); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: publishing %s: %v\n", ScratchHomeEnvName, err)
	}
	// The host's cheap root, made usable before the placement is decided
	// (scratch_placement.go): a run whose /dev/shm is absent otherwise lands on
	// the device the scratch home sits on and pays its journal commits.
	ensureMemoryBase()
}

// pluralY words the GC report without an i18n dependency.
func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// RunSuiteMain is the ONE-LINE entry point every suite's TestMain uses when
// it has no setup of its own: it pins the scratch root, REFUSES to run on
// `/`, runs the suite and cleans the scratch root up. Suites with their own
// setup (env hermeticity, ledger pinning) call PinSuiteForTestMain at the top
// and FinishSuiteForTestMain before os.Exit instead.
//
// Both entry points also install the notify-environment sandbox
// (PinSuiteNotifyEnv, see suiteenv.go) — a suite must never inherit the
// operator's real settings.json, whose notify.cmd writes the live sink.
func RunSuiteMain(m *testing.M, suite string) {
	PinSuiteForTestMain(suite)
	// The lifetime binding, in the ONE entry point 34 suites already share: the
	// write end of the death pipe is held by this test binary alone, so any
	// fixture child that adopts the inherited read end (testutil.
	// WatchFixtureBinding in the child) dies with the suite — SIGKILL included —
	// instead of reparenting to init and standing in a task worktree. The sweep
	// beside it collects daemons an earlier run of THIS suite left in its own
	// scope (the scope prefix is the suite's name, so one suite can never sweep
	// another's).
	//
	// Arm only; a suite whose children do not adopt yet is not made worse: the
	// arm is inert until a child watches it (Rule 15 — ArmAndSweepForSuite
	// reports an arm that failed, and an unadopted pipe leaks nothing).
	ArmAndSweepForSuite(suite)
	code := m.Run()
	FinishSuiteForTestMain()
	os.Exit(code)
}

// UnderTestRootLeaf and the pin/provider live in internal/worktreeroots
// (scratch_pin.go) — the package that owns the resolver. testutil only INSTALLS
// the provider, so the two halves cannot drift (Rule 14).

// pinUnderTestWorktreeRoot installs the worktree-root provider that keeps a TEST
// PROCESS off the operator's real worktree root (measured defect: 174 orphan
// `~/workspace/gopi-<uuid>` stubs whose gitdirs died with the /dev/shm suite run
// that created them — see internal/worktreeroots/scratch_pin.go).
//
// WHY IT IS INSTALLED HERE, ONCE, FOR EVERY SUITE: the producers are PRODUCTION
// resolvers (tools.TaskRunGroundFor → config.ResolveRepoWorktreeRoot →
// worktreeroots.Resolve/DefaultRoot), and a test whose fixture repo declares no
// worktree root reaches the DEFAULT — $HOME/workspace — with nothing at its call
// site to notice. A pin each affected test must remember to call is not a
// mechanism; installing it in the ONE suite entry point every suite already uses
// is (34 suites call RunSuiteMain/PinSuiteForTestMain).
//
// The redirected root is a child of THIS RUN's scratch root — the per-run
// directory the suite entry point already creates and removes — so:
//
//   - it is never the operator's real root, and never a leftover: the existing
//     FinishSuiteForTestMain removes the whole per-run tree (Rule 35: no new
//     reaper, no new cron, nothing persistent is created);
//   - it is per-RUN, so two suites cannot share one, and a re-run gets a fresh
//     one (the directory is created lazily by the provider, only when something
//     actually resolves a root);
//   - an UNPINNED run (the GOTESTSUITE_NO_PIN escape) falls back to the scratch
//     HOME, which is still off `/` — never the operator's workspace.
//
// A provider that cannot create the directory returns "" and REPORTS it (Rule
// 15b): the resolution then behaves exactly as before this pin, which is the
// honest answer — a silent fallback to the operator's root is the defect.
func pinUnderTestWorktreeRoot() {
	worktreeroots.ProvideUnderTestRoot(func() string {
		// SuiteTempRoot() is the pinned per-run root, or — on the documented
		// GOTESTSUITE_NO_PIN escape for a read-only container — the ambient temp
		// root the escape already accepted for ALL of this suite's scratch. It is
		// never the operator's workspace either way, which is the property this
		// pin exists for.
		dir := worktreeroots.UnderTestRootPath(SuiteTempRoot())
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			fmt.Fprintf(os.Stderr, "testutil: test worktree root %s: %v — resolutions fall back to the unpinned default\n", dir, err)
			return ""
		}
		return dir
	})
}

// PinSuiteForTestMain is the hard half of the entry point: pin the scratch
// root, then ASSERT it is not on the / filesystem — a suite that would write
// to /tmp exits with SuiteScratchUnsafeExitCode instead of running.
//
// An UNENGAGED pin is the implicit-fallback case and is refused first, with
// its own message: when PinSuiteTempRoot could not create the per-run root,
// SuiteTempRoot() is the ambient temp root (os.TempDir() → /tmp → the boot
// device on a default host). Running anyway is the silent failure the ban
// exists to prevent, so the refusal is a fatal exit, not a skip.
//
// GOTESTSUITE_NO_PIN is the documented operator escape for the rare host where
// the scratch home genuinely cannot exist (a read-only container). It is read
// HERE and nowhere else (Rule 2), and it must be set deliberately: the default
// is to refuse.
func PinSuiteForTestMain(suite string) {
	PinSuiteTempRoot(suite)
	// The WORKTREE-ROOT pin rides the scratch pin: it redirects the production
	// default root into THIS run's scratch, so no in-process producer can mint a
	// worktree in the operator's real workspace (see pinUnderTestWorktreeRoot).
	// Installed before the fatality checks for the same reason the notify sandbox
	// is: it must hold on every path that can still reach a producer.
	pinUnderTestWorktreeRoot()
	// The roots an EARLIER run of THIS suite left behind, whose recorded owner the
	// kernel confirms dead — collected now, before m.Run, from the trees this
	// suite's own pin lands in (see ReapDeadSuiteRunRootsAtSuiteStart). It lives
	// here rather than in ArmAndSweepForSuite because not every suite calls that
	// one (internal/tools pins and finishes without it), and it must run AFTER the
	// pin so the scope includes the placement this run chose.
	ReapDeadSuiteRunRootsAtSuiteStart(suite)
	// The base-residue arm OFF for the duration of a suite: every suite in this
	// repository runs on the developer's own host, whose /dev/shm holds their
	// REAL ad-hoc residue (measured: 2.6 GB / 702 entries before the manual
	// reclaim). The automatic base arm would have every `go test` sweep the
	// developer's live memory base — a test run must never be the actor that
	// deletes the operator's directories (Rule 6). The arm is re-enabled per
	// test by the one test that deliberately exercises it, and a caller that
	// CONFIGURES the base (GOPI_SCRATCH_BASE) is unaffected: an explicit knob is
	// an explicit instruction.
	scratch.SetAutoBaseResidue(false)
	// Notify-env sandbox: BEFORE the fatality checks, so the volatile notify
	// vars are dropped even on the GOTESTSUITE_NO_PIN escape path. Without
	// this a suite that walks the task state machine delivers into the
	// operator's live sink through the ambient settings.json (suiteenv.go).
	PinSuiteNotifyEnv(suite)
	// The git ROUTING pin (gitenv.go): a suite started from a shell that
	// exported GIT_DIR/GIT_WORK_TREE/GIT_INDEX_FILE/... must not carry it into
	// any git child it spawns. cmd.Dir cannot override those names, which is
	// how 12 fixture commits landed on real master on 2026-10-13 — the suite
	// drops them here, at the ONE entry point every suite shares, and thus
	// before any test can build a fixture.
	PinSuiteGitEnv(suite)
	// Ledger sandbox, the last ambient root (ledgerpin.go): a suite that never
	// names GOPI_MEMORY_DB opens the operator's REAL ledger, and the ledger is
	// append-only — so a fixture row written there is permanent (that is how
	// t-stale-sibling made the merger report a phantom /tmp repo for days).
	// Every suite gets this one; a test that wants rows names its own DB.
	PinSuiteLedgerEnv(suite)
	// Auto-register switch, the same class of ambient input: the switch is read
	// from the PROCESS environment by internal/agent's start hook, and a
	// clone-channel round exports the OFF value into every child it spawns — so
	// a suite started from a round reports on that value instead of on the code
	// (2026-10-29: 35 arms red under =0, green under =1 on one tree). Dropped
	// here, once, for every suite (autoregisterenv.go).
	PinSuiteAutoRegisterEnv(suite)
	// CROSS-RUN EXCLUSION, before anything runs (suitepackage.go): two whole-suite
	// runs of ONE package share the scratch home, the machine's TCP surface and
	// the operator profile's ledger, so they must not interleave. A sibling in
	// flight is REFUSED with its pid, its start instant and the tree it works in
	// (never a silent wait: a silent wait is reported as `NO PROGRESS` about the
	// wrong thing). It is taken here — after every pin, before m.Run — so the
	// refusal costs the whole suite nothing; the release rides
	// FinishSuiteForTestMain, and the KERNEL releases it even on kill -9.
	if _, err := AcquireSuitePackageLock(suite); err != nil {
		// An infrastructure failure (the home could not be written), NOT an answer
		// about a sibling. Reported, and the suite runs: refusing here would turn a
		// sick scratch home into "somebody else is running", which is the wrong
		// diagnosis (Rule 15).
		fmt.Fprintf(os.Stderr, "testutil: suite %q: %v\n", suite, err)
	}
	if !SuiteTempRootPinned() {
		if os.Getenv(gotestSuiteNoPinEnvName) != "" {
			// The escape path still has a floor: the pin did not engage, so the
			// suite runs wherever the ambient temp root points (os.TempDir() →
			// /tmp → the boot device on a default host). Report the MEASURED
			// verdict rather than only warning in the abstract (tmpdir epic,
			// 2026-09-14): an operator who opted out still learns whether the
			// fallback is on `/`, which is the one fact that decides whether
			// opting out is survivable here.
			fmt.Fprintf(os.Stderr,
				"testutil: %s is set and the scratch-root pin did not engage — continuing on the ambient temp root %s. "+
					"Scratch data will land wherever os.TempDir() points: acceptable ONLY on a host where the scratch "+
					"home %s cannot exist (a read-only container); anywhere else this reintroduces the 2026-09-11 "+
					"exhaustion (13,044 abandoned scratch dirs on the boot device).\n",
				gotestSuiteNoPinEnvName, SuiteTempRoot(), ScratchHomeDir())
			if err := scratch.AssertTempRootOffRootFS(); err != nil {
				fmt.Fprintf(os.Stderr, "testutil: %s\n", err)
			}
			return
		}
		fmt.Fprintf(os.Stderr, "\nFATAL: suite %q: the scratch-root pin did NOT engage, so the suite fell back to the "+
			"AMBIENT temp root %s (the value of %s). On a default host that is /tmp — the boot device, and the "+
			"partition the 2026-09-11 exhaustion filled. "+
			"FIX: make the scratch home usable: %s — or, if this host genuinely cannot hold one (a read-only "+
			"container), set %s=1 to accept ambient scratch explicitly: %v\n\n",
			suite, SuiteTempRoot(), TempDirEnvName, ScratchHomeMountHint(), gotestSuiteNoPinEnvName,
			assertScratchPathSafe(SuiteTempRoot()))
		os.Exit(SuiteScratchUnsafeExitCode)
	}
	FatalAssertSuiteScratchRootSafe(suite)
}

// gotestSuiteNoPinEnvName is the documented opt-out from the unengaged-pin
// refusal. Unset (the default) means: refuse to run rather than write scratch
// to the ambient temp root.
const gotestSuiteNoPinEnvName = "GOTESTSUITE_NO_PIN"

// FinishSuiteForTestMain removes the pinned per-run scratch root (the last
// thing a TestMain does before os.Exit).
//
// It JOINS the signal arm FIRST, and the order is the whole point: the arm is a
// goroutine parked on a signal channel for the life of the process, so any suite
// that dumps goroutines before calling this one would report it as a leak (the
// `./cmd/agent` defect of 2026-10-22: green tests, `parked repository goroutines:
// 1`, exit 1). Joining here gives EVERY suite the arm for free — the suites that
// sample first (cmd/agent does, in its own close-out) call
// JoinSuiteRunRootSignalHandler themselves, and this call is then a no-op.
//
// The join is safe to pair with the removal below: once the handler has left its
// park, nothing reaps the root behind CleanupSuiteTempRoot's back, and a signal
// arriving after the join takes its default disposition (the documented trade in
// JoinSuiteRunRootSignalHandler) instead of racing the removal.
func FinishSuiteForTestMain() {
	JoinSuiteRunRootSignalHandler()
	// The cross-run exclusion is released AFTER the root is gone: the lock's
	// holder is this run, and a sibling that starts in the gap must find a
	// finished run (root removed) rather than a half-torn one.
	CleanupSuiteTempRoot()
	if err := ReleaseSuitePackageLock(); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: %v\n", err)
	}
}

// CleanupSuiteTempRoot removes the pinned per-run root (a long-lived host
// must not accumulate scratch entries). TestMain calls this after m.Run().
func CleanupSuiteTempRoot() {
	if suiteTempRoot == "" {
		return
	}
	root := suiteTempRoot
	suiteTempRoot = ""
	if err := os.RemoveAll(root); err != nil {
		// Never swallow: a leaked scratch root is invisible otherwise, and
		// the message must not pretend the cleanup happened (Rule 15b).
		fmt.Fprintf(os.Stderr, "testutil: scratch root %s not removed: %v\n", root, err)
	}
}

// TestDeviceScratchDir creates a scratch directory on a NON-memory (block-
// backed) filesystem inside the scratch home, for a test whose SUBJECT is the
// device — a fixture that asserts on an fsync's cost, or on the schema batch a
// device-backed first open chooses.
//
// WHY IT IS A HELPER AND NOT A LOCAL os.MkdirTemp("."): the obvious way for such
// a fixture to reach a real device is to create its directory inside the package
// SOURCE TREE (the checkout is a block device), and that is the leak this
// function exists to close. Measured 2026-09-21: five
// `internal/tools/firstopen-device-<rand>/` directories holding 341 test `.db`
// files sat UNTRACKED inside the package tree, and an earlier `git add` sweep
// captured 106 of them in a commit (c2f5a9de). The repo-root junk gate reads
// `git ls-files` and validates repo-ROOT paths only, so a NESTED tree is outside
// its field of view: the gate was not wrong, it was blind to that shape. A
// fixture that must write somewhere with a device property has to be TOLD where
// that is, not left to infer it from its own location.
//
// The location is ScratchRootQueue() — the shared, GC'd, attributed scratch tree
// that is already off `/` (Rule 22) and outside every checkout. The device
// PROPERTY is then MEASURED, never spelled: a host (or an operator pin) whose
// scratch home is a memory filesystem has no device to measure, so the caller is
// told to skip rather than handed a tmpfs and a meaningless number. The
// measurement is internal/scratch's (Rule 14), the same one the scratch-root
// assertion uses.
//
// The directory is removed in t.Cleanup; a killed test binary never runs it, and
// that residue is age-GC'd in the scratch home rather than committed.
func TestDeviceScratchDir(t *testing.T, sub string) string {
	t.Helper()
	dir := filepath.Join(ScratchRootQueue(), fmt.Sprintf("%sdevice-%s-%d", scratch.QueueMarker, sub, os.Getpid()))
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		t.Fatalf("device scratch dir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("device scratch dir %s not removed: %v", dir, err) // Rule 15b
		}
	})
	if memBacked, description := ScratchRootFilesystem(dir); memBacked {
		t.Skipf("the scratch home is %s, so it carries no block device to measure — this fixture's subject "+
			"(the device arm) does not exist here; use an ordinary t.TempDir() fixture on such a host",
			description)
	}
	return dir
}

// TestScratchDir creates a scratch directory under the shared scratch home
// for a test that needs LARGE throwaway files (load generators, dumps), and
// removes it in t.Cleanup. It exists so such a test has no reason to invent
// a path of its own — and, in particular, no reason to reach for /tmp.
// The directory is age-GC eligible as well (belt and braces: a killed test
// binary never runs t.Cleanup).
func TestScratchDir(t *testing.T, sub string) string {
	t.Helper()
	dir := filepath.Join(ScratchRootQueue(), fmt.Sprintf("%s%s-%d", scratch.QueueMarker, sub, os.Getpid()))
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		t.Fatalf("scratch dir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("scratch dir %s not removed: %v", dir, err) // Rule 15b
		}
	})
	return dir
}

// ScratchRootFilesystem asks the kernel what filesystem type a scratch root
// sits on and reports (isMemoryBacked, description). An unreadable root is
// reported as "statfs failed", never guessed: the description is only ever
// used to attribute a measurement, so an unknown backing must not masquerade
// as either class (Rule 15).
func ScratchRootFilesystem(root string) (bool, string) {
	mem, description, err := scratch.MemoryFilesystem(root)
	if err != nil {
		return false, description
	}
	if mem {
		return true, description
	}
	var st syscall.Statfs_t
	if serr := syscall.Statfs(root, &st); serr == nil && uint64(st.Type) == ext4Magic {
		return false, "ext4/3 filesystem (block-backed, and the backing of both / and /home on this host)"
	}
	if strings.Contains(description, "NOT a memory filesystem") {
		return false, description
	}
	return false, description + " (NOT a memory filesystem)"
}

// ScratchDeviceID returns the kernel's filesystem identifier of path — the
// same key statfs reports for `/`. Two paths on the same identifier share one
// filesystem, and their bytes compete for one partition's space; that is the
// property the /tmp ban is about. An unreadable path is an error, never a
// guess (Rule 15).
//
// The measurement itself lives in internal/scratch (Rule 14: ONE definition —
// the runtime scratch fallback in that package needs the same question
// answered, and internal/testutil imports "testing", so this package could
// never be the shared home). This function is the suite-facing name for it.
func ScratchDeviceID(path string) (uint64, error) { return scratch.DeviceID(path) }

// RootDeviceID is the filesystem identifier of `/` — the partition the /tmp
// ban protects. Alias of the one definition in internal/scratch (Rule 14).
func RootDeviceID() (uint64, error) { return scratch.RootDeviceID() }

// ScratchRootSharesRootFS reports whether a scratch path lives on the same
// filesystem as `/` (true for /tmp). An error means the question could not be
// answered; callers must treat that as "not proven safe", never as "safe".
func ScratchRootSharesRootFS(path string) (bool, error) { return scratch.SharesRootFS(path) }

// AssertTempRootOffRootFS is the suite-facing name of the EFFECTIVE-temp-root
// ban (tmpdir epic, 2026-09-14): it measures wherever this process's temporary
// workspace actually points — an explicit TMPDIR when one is present, the
// implementation fallback when it is not (the distinction is read from the raw
// /proc/self/environ block, since os.Getenv cannot tell the two apart) — and
// refuses a root on the `/` filesystem with the measured device ids, the
// classification (explicit vs implicit) and the fix.
//
// It complements AssertSuiteScratchRootSafe rather than restating it: that one
// judges the SUITE's pinned scratch path, this one judges the TEMP ROOT the
// process and every child it spawns will use (t.TempDir(), os.MkdirTemp(""…),
// a shell's mktemp) — the surface that filled `/` with 23.5G of residue while
// the suite's own scratch root was already off `/`. The measurement lives in
// internal/scratch (Rule 14); this is the delegate.
func AssertTempRootOffRootFS() error { return scratch.AssertTempRootOffRootFS() }

// AssertSuiteScratchRootSafe is the HARD, mechanical version of the /tmp ban:
// it returns ErrScratchRootOnRootFS (wrapped, with the measured device ids
// and the fix) when the suite's scratch root shares a filesystem with `/`,
// and an error when the root cannot be measured at all.
//
// Every entry point calls it BEFORE running a single test, for EVERY pinned
// suite — a convention that is not asserted is a convention that regresses
// (Rule 13). It is deliberately an error rather than a warning: the whole
// point of the ban is that a warning is ignored, as the 2026-09-11 incident
// showed (13,044 abandoned scratch dirs in /tmp, `/` down to 1.5G free).
//
// The IMPLICIT fallback is refused one level up, in PinSuiteForTestMain: when
// no pin engaged (`SuiteTempRootPinned` is false) the suite is judged on
// whatever os.TempDir() reports — the AMBIENT temp root, /tmp on a default
// host, and therefore the same boot device this ban exists to keep scratch
// off. That case gets its own message because it needs a different fix than an
// operator who deliberately pinned a bad root — and because this assertion
// must stay a pure measurement (path → verdict), so a fresh host whose scratch
// home is merely absent still fails loudly rather than in two different voices.
func AssertSuiteScratchRootSafe(suite string) error {
	root := SuiteTempRoot()
	err := assertScratchPathSafe(root)
	if err == nil {
		return nil
	}
	if !SuiteTempRootPinned() {
		return fmt.Errorf("suite %q: the scratch-root pin did NOT engage, so the suite fell back to the "+
			"AMBIENT temp root %q (%s is whatever os.TempDir() reports — %s = %q, and the scratch home "+
			"%s could not be created): %w",
			suite, root, TempDirEnvName, TempDirEnvName, os.Getenv(TempDirEnvName), ScratchHomeDir(), err)
	}
	return fmt.Errorf("suite %q: %w", suite, err)
}

// assertScratchPathSafe is the measurement half of the assertion, split out
// so it can be exercised against a /tmp fixture without moving the suite's
// own root.
func assertScratchPathSafe(path string) error {
	rootDev, err := scratch.RootDeviceID()
	if err != nil {
		return fmt.Errorf("scratch root %q: cannot measure the filesystem of / — refusing to run: %w", path, err)
	}
	dev, err := scratch.DeviceID(path)
	if err != nil {
		return fmt.Errorf("scratch root %q: cannot measure its filesystem — refusing to run: %w", path, err)
	}
	if dev != rootDev {
		return nil
	}
	memBacked, description := ScratchRootFilesystem(path)
	if memBacked {
		// Same device as `/` AND a memory filesystem is a host-geometry
		// contradiction (tmpfs has its own fsid) — say so instead of
		// printing a fix that cannot apply (Rule 15).
		return fmt.Errorf("scratch root %q reports device 0x%x (the device of /) but is classified %s — "+
			"the measurement is inconsistent, refusing to run", path, dev, description)
	}
	return fmt.Errorf(
		"scratch root %q is on the SAME filesystem as / (device 0x%x, %s). "+
			"GoPi bans that: this host's / is the 147G partition the 2026-09-11 exhaustion filled "+
			"(unbounded /tmp stress loop + %d abandoned scratch dirs), and a SQLite scratch DB there pays a "+
			"block-device fsync at every WAL close (measured 49-225ms, seconds under load) that hangs the whole suite. "+
			"FIX: unset %s (or point it at a directory under %s), and make sure the scratch home exists: %s. "+
			"See devdoc/tmp-ban-scratch-root.md. %w",
		path, dev, description, 13044, SuiteTempRootEnvName,
		ScratchHomeDir(), ScratchHomeMountHint(), ErrScratchRootOnRootFS)
}

// FatalAssertSuiteScratchRootSafe is AssertSuiteScratchRootSafe for TestMain:
// it prints the actionable failure to stderr and exits with
// SuiteScratchUnsafeExitCode. TestMain cannot use t.Fatal, and a suite that
// silently runs in /tmp is exactly the failure mode being banned.
func FatalAssertSuiteScratchRootSafe(suite string) {
	if err := AssertSuiteScratchRootSafe(suite); err != nil {
		fmt.Fprintf(os.Stderr, "\nFATAL: %v\n\n", err)
		os.Exit(SuiteScratchUnsafeExitCode)
	}
}

// ---------------------------------------------------------------------------
// Bounded scratch: the age+holder sweep (implemented in internal/scratch).
//
// The sweep CORE moved to internal/scratch (gc.go + holder.go) because the
// only resident components that can trigger it are production code (cmd/agent's
// worker and merger tick loops) and this package imports "testing" — a
// production import of testutil would drag the test framework into the shipped
// binary. The suite-facing API below is unchanged (it delegates), so every
// suite, script and test that used it keeps working, and this file stays the
// place a suite author reads.
// ---------------------------------------------------------------------------

// ScratchStaleAge is the age past which an unheld scratch directory is garbage —
// the DEFAULT of the stale-window knob, as the suite layer sees it.
//
// It is an alias of the one definition in internal/scratch (Rule 14), and it is
// deliberately the DEFAULT rather than the EFFECTIVE window: a suite run is one
// of the processes the window is configured FOR, and a fixture must age against
// a number it can reason about while the operator's --scratch-gc-stale-min /
// GOPI_TEST_SCRATCH_STALE_MIN / settings value moves the live sweep. So this
// alias is used ONLY to age fixtures (the tests aging a directory to "past the
// default"); the SWEEP is invoked through GCStaleScratchRoot, which resolves
// the operator's window (scratch.ResolveStaleAge) so the tree a suite bounds is
// bounded by the window the operator actually chose.
const ScratchStaleAge = scratch.DefaultStaleAge

// GCStaleScratchRoot removes scratch entries under the scratch home that are
// older than the EFFECTIVE stale window (scratch.ResolveStaleAge: the
// operator's flag/env/settings value, defaulting to ScratchStaleAge) AND held
// by no live process. Returns how many entries were removed. Safe under
// concurrency (advisory lock inside the home), conservative (an entry whose
// holder check errored is KEPT — deleting a directory a live process cannot see
// is a data-loss bug, a leaked directory is only disk usage), and a no-op when
// the home does not exist.
//
// It is also the function the resident tick's suite-side twin delegates to, so
// the tree is bounded on a host that stops running suites.
func GCStaleScratchRoot() (int, error) { return GCStaleScratchRootOlderThan(scratch.ResolveStaleAge()) }

// GCStaleScratchRootOlderThan is GCStaleScratchRoot with an explicit age
// threshold (a watchdog's knob, and the boundary tests' hook).
func GCStaleScratchRootOlderThan(olderThan time.Duration) (int, error) {
	return scratch.SweepHome(ScratchHomeDir(), olderThan)
}

// ScratchDirHolder returns the PID of a live process holding dir as its
// working directory or as an open file descriptor ("" = nobody). It is the
// public face of the hard rule; see scratch.DirHolder for the guarantees.
func ScratchDirHolder(dir string) (string, error) { return scratch.DirHolder(dir) }

// ScratchHolderAlive reports whether the process that registered dir as its
// scratch root (the holder file PinSuiteTempRoot writes) is still running —
// the permission-free half of the hard rule. See scratch.HolderAlive.
func ScratchHolderAlive(dir string) (bool, string) { return scratch.HolderAlive(dir) }

// registerScratchHolder writes the liveness registration into a pinned run
// root (pid + kernel start time). Failure is reported, never fatal: the
// registration is an extra guarantee, not the only one (Rule 15b).
func registerScratchHolder(root string) { scratch.RegisterHolder(root) }

// holderFileName / dirPerm / filePerm aliases a test reads directly.
const holderFileName = scratch.HolderFileName

// pathWithin reports whether path is target itself or nested under it (the
// holder comparison's whole safety argument) — one definition, in
// internal/scratch.
func pathWithin(path, target string) bool { return scratch.PathWithin(path, target) }

// ScratchDBPath builds a path for a throwaway scratch DB directly on the
// pinned scratch root (parent directory created). A caller that cannot
// tolerate t.TempDir()'s cleanup timing may use this instead.
func ScratchDBPath(prefix, fileName string) (string, error) {
	dir, err := os.MkdirTemp(SuiteTempRoot(), prefix)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// ScratchHomeMountHint is the exact command that makes the scratch home usable,
// and the fix hint every failure message repeats. It is exported so callers
// quote ONE spelling of the fix.
//
// WHY IT IS A mkdir AND NOT A tmpfs MOUNT (operator ruling, 2026-09-18):
// "don't use tmpfs. change settings to workspace." The requirement was never
// memory backing — it is a scratch home OFF the `/` device (147G, the
// 2026-09-11 exhaustion site). /home is a different device, so an ordinary
// directory there satisfies AssertSuiteScratchRootSafe at zero memory cost.
// The tmpfs route bought wall-clock time (a measured 601s vs 237s for
// internal/tools) and was rejected as not worth a second filesystem the age GC
// can silently drop and `du /home` cannot see.
func ScratchHomeMountHint() string {
	return fmt.Sprintf("mkdir -p %s", ScratchHomeDir())
}
