package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// ---------------------------------------------------------------------------
// TestMain hermeticity pin for the LEDGER and the wrapper-child identity
// (epic: reframe-telemetry-doublecount, 2026-09-10).
//
// The incident: a task with TWO real task(action=reframe) dispatches (session
// JSONL) carried TWENTY-TWO task_events rows (WHERE event='reframe'), so
// `gopi task verdicts` — the anti-bureaucracy meter — read pure noise.
//
// Two ambient inputs make a test process impersonate the task it tests:
//
//  1. GOPI_TASK_WRAPPER_ID. maybeRegisterSessionTask latches autoTaskRowID to
//     the inherited wrapper id, so every test that drives a real tool dispatch
//     (TestReframeSwap_BigBriefing and friends) writes its telemetry under the
//     WRAPPER task's id — inside the developer's REAL ~/.gopi/memory.db,
//     because GOPI_MEMORY_DB was unset.
//  2. GOPI_MEMORY_DB unset → memoryDBPath() falls through to the default
//     ~/.gopi/memory.db.
//
// The suite neutralizes both once, for the whole package: the ledger path is
// PINNED to a per-run scratch root (never merely unset — an ambient export
// would otherwise still point at the real profile) and the inherited
// wrapper-child identity is dropped. Individual tests may still override the
// path with t.Setenv (t.Setenv wins for that test's duration).
//
// The propagation fix itself lives in internal/tools/childenv.go (spawned
// shells no longer inherit the identity); this pin is the test-side half.
// Regression coverage for the writer is
// internal/agent/reframe_telemetry_test.go.
// ---------------------------------------------------------------------------

// agentSuiteName labels this suite's scratch root.
const agentSuiteName = "agent"

// agentSuiteScratchPrefix names the throwaway ledger root this suite used to
// create as a SIBLING of its profile (`<scratch>/gopi-agent-suite-ledger-*`).
//
// KEPT, with no production use, because two things must remember it: the
// comment above pinSuiteLedgerEnv (which records the defect) and the assertion
// in TestSuiteLedgerPin_IsActive that the pinned ledger is not spelled that
// way. It is exactly the shape the guard catches, so the name outlives the
// mechanism on purpose (Rule 23: a failed path is information).
const agentSuiteScratchPrefix = "gopi-agent-suite-ledger-"

// agentSuiteProfileDirPerm is the mode of the `.gopi` directory this pin
// materialises inside its own HOME (the empty directory keeps every resolver's
// ErrNotExist path out of the hot loop, exactly like PinSuiteGopiProfile's own
// `.gopi` creation).
const agentSuiteProfileDirPerm os.FileMode = 0o755

// pinSuiteLedgerEnv points the knowledge-base path overrides at a per-run
// ledger that IS A FILE OF THIS SUITE'S OWN PROFILE, and drops the
// wrapper-child identity. A failure is REPORTED on stderr, never swallowed
// (Rule 15b) — and never fatal, exactly like the scratch-root and
// gopi-profile pins.
//
// Rule 17 (test env isolation) — why these four sites are an ESCAPE, not a
// baseline entry: this is suite-initialization code. TestMain calls it before
// m.Run, so no *testing.T exists in scope and t.Setenv is structurally
// unavailable. The pin must ALSO be inherited by the child processes the tests
// spawn (they read GOPI_MEMORY_DB at exec time), which t.Setenv's
// restore-at-test-end semantics cannot express. Each site therefore carries a
// reasoned inline env-leak-ok escape — the gate records them as reviewed
// true positives (visible in TestStandardGateDataReport) rather than as the
// legacy-debt entries they were baselined as from 2026-09-10.
//
// THE LEDGER MUST BE A FILE OF THE PROFILE (measured 2026-11-04, task
// 01a1a8a3). Every gopi run that has both a ledger and a notify target
// resolves them through two INDEPENDENT channels: the ledger from
// GOPI_MEMORY_DB, the notify target from the PROFILE ROOT's settings.json. A
// ledger that is not its profile's own would deliver events into somebody
// else's sink, so deliverTaskNotify grew taskNotifyDelivers /
// taskNotifyOriginLedger: a FOREIGN ledger's events are recorded and reported
// but NOT broadcast.
//
// That gate reads a PROPERTY — "is this ledger this profile's own?", i.e. does
// it equal <GOPI_HOME>/.gopi/memory.db — and this pin violated it BY
// CONSTRUCTION: it named <scratch>/gopi-agent-suite-ledger-*/memory.db while
// testutil.PinSuiteGopiProfile pointed GOPI_HOME at <scratch>/suite-gopi-home.
// The two are SIBLINGS, so every event this suite fired was suppressed and the
// notify-delivery tests went red on the ABSENCE of the log they had just asked
// the code to write (TestAgent_StuckLoop_TerminatesAndNotifies,
// TestAgent_StuckToolfulLoop_TerminatesAndNotifies,
// TestAgent_WallclockCap_GracefulFinish: "loop-stuck notify not captured: open
// …/notify.jsonl: no such file or directory"). The assertions were right; the
// FIXTURE was the defect.
//
// The fix is NOT a second path spelling: the ledger is joined through
// config.ProfilePathIn(config.GopiDirEnv's home, config.MemoryDBFileName) —
// the SAME two definitions the profile pin and the tools layer resolve
// through — so the two pins cannot drift apart again (Rule 2/13/14). The
// regression guard is TestSuiteLedgerIsInsideItsOwnProfile below, which is
// testutil.AssertSuiteLedgerIsInsideItsOwnProfile applied to THIS suite.
//
// The root is testutil.SuiteTempRoot(), NEVER os.MkdirTemp("", …): the empty
// dir argument means os.TempDir(), i.e. /tmp on a default host — the boot
// device the 2026-09-11 exhaustion filled. PinSuiteTempRoot has already run
// (TestMain pins before calling this), so SuiteTempRoot() is the per-run
// scratch root; spelling that explicitly is what makes the channel impossible
// to reintroduce under a later refactor (the /tmp ban's Go-test gate,
// assertTestSourcesCreateNoTmpPath, now fails on the ambient form).
func pinSuiteLedgerEnv() {
	home := os.Getenv(config.GopiDirEnv)
	if home == "" {
		// Never swallowed (Rule 15b): with no HOME there is no profile for the
		// ledger to belong to, and pinning a bare path here is exactly the
		// sibling shape this function exists to stop.
		fmt.Fprintf(os.Stderr, "testutil: agent suite: %s is unset, so the ledger cannot be a file OF "+
			"this suite's profile — %s left unpinned\n", config.GopiDirEnv, config.MemoryDBEnv)
		return
	}
	profileDir := config.ProfileDirIn(home)
	if !strings.HasPrefix(profileDir, testutil.SuiteTempRoot()) {
		// A profile outside the scratch root is the developer's own: pinning
		// the ledger inside it would point a test at the LIVE append-only task
		// history. Refuse rather than write there.
		fmt.Fprintf(os.Stderr, "testutil: agent suite: profile %s is outside the scratch root %s — "+
			"%s left unpinned rather than pointed at a real profile\n",
			profileDir, testutil.SuiteTempRoot(), config.MemoryDBEnv)
		return
	}
	if err := os.MkdirAll(profileDir, agentSuiteProfileDirPerm); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: agent suite: ledger profile dir %s: %v — %s left unpinned\n",
			profileDir, err, config.MemoryDBEnv)
		return
	}
	ledger := config.ProfilePathIn(home, config.MemoryDBFileName)
	_ = os.Setenv(memoryDBEnv, ledger)         //stylegate:ignore child-process env handoff; the child treats an empty value as 'unset' // env-leak-ok: suite pin before m.Run — no *testing.T exists, so t.Setenv is structurally unavailable; spawned children read GOPI_MEMORY_DB at exec time
	_ = os.Setenv(memoryDBEnvLegacy, "")       //stylegate:ignore child-process env handoff; the child treats an empty value as 'unset' // env-leak-ok: suite pin before m.Run — no *testing.T exists; "" masks an ambient legacy value so the child treats it as unset
	_ = os.Unsetenv(tools.TaskWrapperEnv)      // env-leak-ok: suite pin before m.Run — no *testing.T exists; an inherited wrapper-child identity must not reach the tests
	_ = os.Unsetenv(tools.TaskChildSessionEnv) // env-leak-ok: suite pin before m.Run — no *testing.T exists; child sessions must not inherit this process's task identity
}

func TestMain(m *testing.M) {
	// Scratch-root isolation (shared with internal/tools): pin TMPDIR to the
	// suite's own scratch home BEFORE any scratch DB is created. Without this
	// pin every t.TempDir() in the suite lands on the ambient temp root (/tmp
	// on a default host, the boot device), where a fresh SQLite file's WAL
	// close-time checkpoint fsync — and the cold knowledge-base schema init
	// itself — stalls for seconds under host I/O load. That is the shape that
	// reds TestT2_E3_ResumeCarriesForwardCommittedConclusions with
	// "write_task_area: schema: context deadline exceeded": write_task_area
	// bounds cold-file creation + 40+ DDL statements + insert with ONE 5s
	// deadline. The production deadline stays as it is (the ledger is
	// bookkeeping, never the task) — the suite just stops asking it to
	// initialize a database on a saturated boot device. Analysis:
	// devdoc/write-task-area-schema-timeout.md.
	testutil.PinSuiteForTestMain(agentSuiteName)
	// AMBIENT PROFILE PIN (measured 2026-10-13, task 01a13233): the ledger pin
	// below names ONE file, so it leaves config.GopiDir() — the gopi DATA ROOT —
	// pointing at the developer's real profile. Several production audit lookups
	// the round's own close-out calls (taskFindSessionJSONL via
	// taskWorkdirFromSession / taskDeliverableBranchFromSession) answer a session
	// id by WALKING the sessions tree when the id is not addressable, so their
	// cost is linear in the operator's history: on this host ~86k session-dir
	// entries, and the walk reads every candidate's first line. MEASURED inside
	// TestRunCapturesTheResponseBodyAtTheRoundBoundary: 29.9s parked in
	// taskSessionStartID's bufio.Scanner over the real tree (SIGQUIT dump
	// alone.json.dump), 98.5s of that test's 100s budget in a scoped budget run,
	// with the test binary in state S and one thread in D on
	// wait_on_page_bit_common. An EMPTY per-run sandbox makes the walk trivial
	// and, unlike a wall-clock bound, does not hide the cost behind a timeout.
	testutil.PinSuiteGopiProfile(agentSuiteName)
	// The ledger lives INSIDE the suite profile this TestMain pins above
	// (config.ProfilePathIn(home, config.MemoryDBFileName)), so there is no
	// second per-run directory to reclaim here: FinishSuiteForTestMain removes
	// the whole scratch root, profile and ledger together.
	pinSuiteLedgerEnv()
	code := m.Run()
	testutil.FinishSuiteForTestMain() // per-run throwaway: never accumulate
	os.Exit(code)
}

// TestSuiteLedgerPin_IsActive proves the pin holds in the tests' own
// (otherwise ambient) environment: without it every reframe-dispatching test
// writes into the developer's profile ledger.
func TestSuiteLedgerPin_IsActive(t *testing.T) {
	pinned := os.Getenv(memoryDBEnv)
	if pinned == "" {
		t.Fatalf("TestMain left %s unset — the suite is pointed at the default profile ledger", memoryDBEnv)
	}
	// The ledger must be THIS suite's profile's own ledger file, not merely a
	// path somewhere under the scratch root: the sibling shape
	// `<scratch>/gopi-agent-suite-ledger-*/memory.db` satisfied the old
	// "inside the scratch root" reading and was still a FOREIGN ledger as far
	// as taskNotifyDelivers is concerned.
	if want := config.ProfilePathIn(os.Getenv(config.GopiDirEnv), config.MemoryDBFileName); filepath.Clean(pinned) != filepath.Clean(want) {
		t.Fatalf("%s=%q is not this suite's own profile ledger %q — a ledger beside the profile is judged "+
			"FOREIGN and every event the suite fires is suppressed", memoryDBEnv, pinned, want)
	}
	if strings.Contains(pinned, agentSuiteScratchPrefix) {
		t.Fatalf("%s=%q still uses the retired sibling scratch root %q — the ledger must be a file OF the "+
			"profile, never beside it", memoryDBEnv, pinned, agentSuiteScratchPrefix)
	}
	// The root must be under the PINNED scratch root, not merely named like it:
	// the ambient form (os.MkdirTemp("", …)) produced the same basename on the
	// ambient temp root (/tmp) while the suite's pin was silently bypassed.
	// Dropping testutil.SuiteTempRoot() from pinSuiteLedgerEnv makes this red.
	if root := testutil.SuiteTempRoot(); !strings.HasPrefix(pinned, root) {
		t.Fatalf("%s=%q is not under the pinned scratch root %q — the ledger scratch root was built "+
			"through the AMBIENT temp root, so the per-run cleanup never sees it", memoryDBEnv, pinned, root)
	}
	if w := os.Getenv(tools.TaskWrapperEnv); w != "" {
		t.Fatalf("TestMain left %s=%q — an inherited wrapper id makes this process write under a REAL task row",
			tools.TaskWrapperEnv, w)
	}
	// Behavioural half: the pinned path is the one the ledger layer resolves.
	if _, err := tools.TaskReframeStats(); err != nil {
		t.Fatalf("TaskReframeStats at the pinned path: %v", err)
	}
	if _, err := os.Stat(pinned); err != nil {
		t.Fatalf("the pinned ledger %s was never created by the tools layer: %v", pinned, err)
	}
}

// TestSuiteGopiProfileIsSandboxed is the guard on the READ half of this suite's
// isolation (task 01a13233, 2026-10-13). The ledger pin above names ONE file;
// config.GopiDir() is a DIFFERENT tree, and it is the one the round's own
// close-out walks: taskFinishCleanup → finishCleanupRowGroundBranch →
// taskWorkdirFromSession → taskFindSessionJSONL falls back to a walk of
// `<GopiDir>/sessions` that reads the first line of EVERY record when the id is
// not addressable. Ambient, that tree is the operator's history (~86k dirs on
// this host) and the walk is where the round boundary sat: 29.9s parked in
// taskSessionStartID's bufio.Scanner in the SIGQUIT dump, 98.5s for the whole
// test. Dropping testutil.PinSuiteGopiProfile from TestMain makes this red, and
// so does a fixture that materializes a session tree in the sandbox.
func TestSuiteGopiProfileIsSandboxed(t *testing.T) {
	testutil.AssertSuiteGopiProfileIsPinned(t)
}

// TestSuiteLedgerIsInsideItsOwnProfile is the guard on the RELATION between the
// two pins, which neither one can see alone (measured 2026-11-04, task
// 01a1a8a3). This suite pins the ledger (pinSuiteLedgerEnv) and the profile
// (testutil.PinSuiteGopiProfile) through two independent calls, and they used
// to land in SIBLING directories: `<scratch>/gopi-agent-suite-ledger-*/memory.db`
// beside `<scratch>/suite-gopi-home/.gopi/`. deliverTaskNotify suppresses a
// ledger that is not its profile's own (taskNotifyOriginLedger compares
// memoryDBPath() with `<GOPI_HOME>/.gopi/memory.db`), so every event this
// suite fired was recorded and reported but NEVER broadcast — and the three
// notify assertions below went red on the ABSENCE of the log they had asked
// the code to write:
//
//	TestAgent_StuckLoop_TerminatesAndNotifies
//	TestAgent_StuckToolfulLoop_TerminatesAndNotifies
//	TestAgent_WallclockCap_GracefulFinish
//
// Neither existing guard could see it: TestSuiteLedgerPin_IsActive asked "is
// this a sandbox?" (yes) and TestSuiteGopiProfileIsSandboxed asks "is the
// profile a sandbox?" (yes). The missing question is whether the two are the
// SAME sandbox — the shared assertion asks it and, on failure, names BOTH
// sides of the mismatch. Restoring the sibling shape in pinSuiteLedgerEnv
// makes this red.
func TestSuiteLedgerIsInsideItsOwnProfile(t *testing.T) {
	testutil.AssertSuiteLedgerIsInsideItsOwnProfile(t)
}

// TestSuiteNotifyEnvIsHermetic — the notify half of the same class (2026-09-12
// live-sink pollution): this suite drives reframe/claim/complete, so every one
// of those lifecycle steps would deliver through the AMBIENT settings.json
// (the operator's real ~/.gopi/settings.json, whose notify.cmd appends to the
// live sink) unless TestMain installed the shared sandbox.
func TestSuiteNotifyEnvIsHermetic(t *testing.T) {
	testutil.AssertSuiteNotifyEnvHermetic(t)
	testutil.AssertSuiteHasNoNotifyTarget(t)
}

// TestSuiteTempRootIsPinned — the isolation half of the flake fix, asserted
// (dropping testutil.PinSuiteTempRoot() from TestMain makes this red). It is
// the guard that stops the flake from silently returning: an unpinned suite
// puts every scratch DB back on the boot device, where the cold-DB step
// outruns write_task_area's deadline under I/O load.
func TestSuiteTempRootIsPinned(t *testing.T) {
	if !testutil.SuiteTempRootPinned() {
		t.Skipf("no fast scratch root available (%s absent and no %s override) — scratch stays on the ambient temp root",
			testutil.ShmDir, testutil.SuiteTempRootEnvName)
	}
	if got := os.Getenv(testutil.TempDirEnvName); got != testutil.SuiteTempRoot() {
		t.Fatalf("%s = %q, want the pinned scratch root %q", testutil.TempDirEnvName, got, testutil.SuiteTempRoot())
	}
	dir := t.TempDir()
	if !strings.HasPrefix(dir, testutil.SuiteTempRoot()) {
		t.Fatalf("t.TempDir() = %q, want a path under the pinned scratch root %q", dir, testutil.SuiteTempRoot())
	}
	if memBacked, description := testutil.ScratchRootFilesystem(testutil.SuiteTempRoot()); description == "" {
		t.Fatalf("scratch root %s has no filesystem classification (%s=%t) — the backing of the suite's "+
			"scratch root must be MEASURED, so a slow run can be attributed instead of guessed",
			testutil.SuiteTempRoot(), description, memBacked)
	}
	if strings.Contains(os.Getenv("PROBE_PASS"), "yes") {
		t.Fatal("PROBE_PASS=yes is a TEMPORARY probe-pass escape hatch and must not be shipped")
	}
}
