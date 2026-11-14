package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// Suite LEDGER isolation — the pin that stops a suite from writing (or reading)
// the operator's REAL ledger.
//
// ROOT CAUSE THIS FILE EXISTS FOR (2026-09-15, devdoc/reclaim-fossil-20260915.md):
// the scratch-root pin fixed where a suite's throwaway FILES land, and the
// gopi-profile pin fixed which SESSIONS tree an audit lookup walks — but the
// ledger path was still ambient. A suite that never names GOPI_MEMORY_DB
// resolves the default (<gopi dir>/memory.db), and when a test also leaves
// GOPI_HOME ambient that default IS the operator's live ledger. Measured
// consequence: cmd/agent's worker-recycle fixture INSERTed the row
// `t-stale-sibling` (deliverable {"repo":"/tmp/other-repo", …}) straight into
// ~/.gopi/memory.db — a real row the operator never created, which then made
// the merger's dead-holder reclaim print, every pass, forever:
//
//	[task-notify] dead-holder reclaim  (t-stale-sibling) skipped:
//	    repo directory /tmp/other-repo is gone — this row cannot be reconciled here
//
// The ledger is the one durable artifact a test must never touch: unlike a
// scratch file it is APPEND-ONLY (no delete verb exists — `task delete` is
// explicitly rejected by the CLI), so a leaked row is not a mess that gets
// cleaned up, it is a permanent lie in the operator's task history. The pin is
// therefore the same recipe as suiteenv.go / suiteprofile.go — "a test that
// wants the durable thing must NAME it" — applied to the last ambient root.
//
// It is BLANK, not a copy: a test that needs rows seeds them into its own
// sandbox (every in-process fixture already t.Setenv's GOPI_MEMORY_DB, and the
// E2E fixtures pass a sandboxed ledger through the child env). Copying the live
// ledger would reintroduce the read cost the profile pin removed and would put
// the operator's task history into every test process.

const (
	// suiteLedgerDirName is the per-run directory holding the sandbox ledger
	// (inside the pinned scratch root, same layout as the settings sandbox).
	//
	// KEPT, because a ledger written here is still a sandbox: nothing outside a
	// fixture depends on the exact leaf, and the guard below treats the whole
	// scratch root as non-ambient. What CHANGED (2026-11-04, task 01a1a647) is
	// the DEFAULT path PinSuiteLedgerEnv hands out — see
	// suiteLedgerProfileHomeDirName.
	suiteLedgerDirName = "suite-ledger"

	// suiteLedgerProfileHomeDirName is the HOME whose .gopi IS the default
	// sandbox ledger: <scratch>/suite-gopi-home/.gopi/memory.db, i.e. INSIDE the
	// profile PinSuiteGopiProfile pins as GOPI_HOME.
	//
	// WHY THE JOIN IS THE POINT, NOT THE DIRECTORY (measured 2026-11-04, task
	// 01a1a647). Every gopi run that has BOTH a ledger and a notify target
	// resolves them through two INDEPENDENT channels: the ledger from
	// GOPI_MEMORY_DB, the target from the PROFILE ROOT's settings.json. A run
	// whose ledger is not its profile's own would deliver its events into
	// somebody else's sink, so deliverTaskNotify grew the gate
	// taskNotifyDelivers/taskNotifyOriginLedger: a FOREIGN ledger's events are
	// recorded and reported but NOT broadcast.
	//
	// That gate reads a PROPERTY — "is this ledger this profile's own?" — and
	// the suite violated it BY CONSTRUCTION: this pin pointed the ledger at
	// <scratch>/suite-ledger/memory.db while PinSuiteGopiProfile pointed
	// GOPI_HOME at <scratch>/suite-gopi-home. The two are SIBLINGS, so the
	// ledger's profile was not the suite's, EVERY suite in this repository
	// became a foreign-ledger run, every event it fired was suppressed, and four
	// notify-delivery tests went red (TestRebuildBuildFailureNotifies,
	// TestRebuildVersionMismatchNotifies,
	// TestRepoSweepWouldPruneReportsLiveBranchAndMutatesNothing,
	// TestStalePayloadZombieEvidence). The assertions were right; the FIXTURE
	// was the defect. The fix is not a second directory — it is THIS ONE, spelled
	// through the same constant the profile pin uses and joined through
	// config.ProfileDirIn, so the two pins cannot drift apart again.
	suiteLedgerProfileHomeDirName = SuiteGopiProfileDirName
)

// ledgerChoice is the ledger override a test NAMED in the parent process (if
// any). Kept as a pair because both spellings are the same override.
type ledgerChoice struct{ db, legacy string }

// named reports whether a test actually chose a ledger.
func (c ledgerChoice) named() bool { return c.db != "" || c.legacy != "" }

// path is the chosen ledger, preferring the current spelling.
func (c ledgerChoice) path() string {
	if c.db != "" {
		return c.db
	}
	return c.legacy
}

// honoursInheritedLedger is the pin's decision rule, kept separate so it can
// be tested as the predicate it is: leave an inherited choice ALONE when a test
// named one (it may be handing the ledger to a child process on purpose), and
// pin regardless when the choice is the operator's own live file — protection
// beats a choice that would write the append-only task history.
func honoursInheritedLedger(c ledgerChoice) bool {
	return c.named() && !ledgerIsAmbient(c.path())
}

var (
	inheritedLedgerOnce sync.Once
	inheritedLedger     ledgerChoice
)

// inheritedLedgerOverride reports the ledger override this process INHERITED,
// captured before this process pinned anything — so a second pin call cannot
// mistake its own sandbox for a parent's choice.
func inheritedLedgerOverride() ledgerChoice {
	inheritedLedgerOnce.Do(func() {
		inheritedLedger = ledgerChoice{
			db:     strings.TrimSpace(os.Getenv(config.MemoryDBEnv)),
			legacy: strings.TrimSpace(os.Getenv(config.MemoryDBEnvLegacy)),
		}
	})
	return inheritedLedger
}

// PinSuiteLedgerEnv points the ledger env vars at a BLANK per-run file inside
// the pinned scratch root, so no test can read or write the operator's real
// ledger by accident.
//
// It does NOT overrule an INHERITED override. A test that points the ledger at
// its own sandbox and hands it to a CHILD process (the shape
// `syncTestDB` + TestSyncImportTwoProcessConcurrent uses: the child re-execs
// the same test binary with the parent's DB in its env, and the whole point is
// that both processes import into THE SAME file) is naming a ledger on
// purpose. Overwriting it here would send the children to an empty sandbox
// while the parent counted rows in its own — measured 2026-09-15: an
// unconditional pin made that test red for the wrong reason. The pin exists to
// stop the FALLBACK to the operator's file, never to make a decision the test
// already made; a test that names nothing under a pinned scratch root still
// gets the sandbox below.
//
// Errors are REPORTED, never swallowed (Rule 15b) — and never fatal, exactly
// like the scratch-root and gopi-profile pins: a suite whose scratch home is
// unavailable still runs, with the ambient-exposure made visible on stderr.
func PinSuiteLedgerEnv(suite string) {
	if !SuiteTempRootPinned() {
		fmt.Fprintf(os.Stderr,
			"testutil: suite %q: scratch root is NOT pinned, so %s is left AMBIENT — a test that opens the "+
				"ledger without naming its own path reads AND WRITES %s (the operator's task history, which is "+
				"append-only: a leaked row is permanent). This is the accepted %s escape path.\n",
			suite, config.MemoryDBEnv, ambientLedgerPath(), gotestSuiteNoPinEnvName)
		return
	}
	if inherited := inheritedLedgerOverride(); honoursInheritedLedger(inherited) {
		name, value := config.MemoryDBEnv, inherited.db
		if inherited.db == "" {
			name, value = config.MemoryDBEnvLegacy, inherited.legacy
		}
		fmt.Fprintf(os.Stderr,
			"testutil: suite %q: %s=%q is INHERITED (a test named this ledger, typically to hand it to a "+
				"child process) — the pin leaves it alone rather than overruling a deliberate choice. "+
				"Nothing falls back to %s.\n",
			suite, name, value, ambientLedgerPath())
		return
	}
	home := filepath.Join(SuiteTempRoot(), suiteLedgerProfileHomeDirName)
	// The ledger must be a file OF THE PROFILE this suite pins, so the profile
	// root is materialised here through the ONE join (config.ProfileDirIn) — a
	// second `filepath.Join(home, ".gopi")` is exactly how the creator and the
	// reader stop agreeing (Rule 14).
	dir := config.ProfileDirIn(home)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: suite %q: ledger sandbox %s: %v — %s left ambient\n",
			suite, dir, err, config.MemoryDBEnv)
		return
	}
	ledger := filepath.Join(dir, config.MemoryDBFileName)
	if err := os.Setenv(config.MemoryDBEnv, ledger); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: suite %q: pinning %s: %v\n", suite, config.MemoryDBEnv, err)
		return
	}
	// The LEGACY spelling is the same override under an older name, so leaving
	// it ambient would smuggle the real ledger straight back in
	// (memoryDBPath's fallback chain reads it). "" — not Unsetenv — because a
	// child process reads it at exec time and the tools layer treats an empty
	// value as "unset".
	if err := os.Setenv(config.MemoryDBEnvLegacy, ""); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: suite %q: masking %s: %v\n", suite, config.MemoryDBEnvLegacy, err)
	}
	// ANNOUNCE the cost this sandbox carries, measured (Rule 22's device-id
	// measurement, never a path spelling). A device-backed ledger sandbox is not
	// a correctness bug, so this is a notice and not a failure — but it decides
	// whether a suite can finish inside its own budget on a loaded host, and
	// silence there is the defect (Rule 15): the operator reads one line and
	// knows which device every fresh ledger's first open is queueing behind.
	if notice := LedgerSandboxBackingNotice(ledger); notice != "" {
		fmt.Fprintf(os.Stderr, "testutil: suite %q: %s\n", suite, notice)
	}
}

// LedgerSandboxBackingNotice reports, in one sentence, whether the ledger
// sandbox at ledger carries a durability cost a suite must expect — or "" when
// the sandbox is provably on a memory filesystem (where the first-open schema
// build's commits have nothing to sync, see internal/tools/firstOpenSyncMode).
//
// It exists because "the suite is slow" and "the suite's throwaway ledger is on
// a block device" are the same fact reported from two ends, and only one of
// them names the device. The classification is the measured one
// (internal/scratch.MemoryFilesystem), so a symlinked or renamed scratch home
// cannot make it lie.
func LedgerSandboxBackingNotice(ledger string) string {
	if strings.TrimSpace(ledger) == "" {
		return ""
	}
	mem, fsName, err := scratch.MemoryFilesystem(filepath.Dir(ledger))
	if err != nil {
		return fmt.Sprintf("the ledger sandbox %s cannot be classified (%v) — this suite's fresh ledgers are "+
			"assumed to pay a device's commits; put the scratch home on a measured memory filesystem",
			ledger, err)
	}
	if mem {
		return ""
	}
	// The remediation sentence is deliberately GONE (operator ruling,
	// 2026-09-18): "don't use tmpfs. change settings to workspace." A
	// block-backed scratch home is now the intended state, so this line states
	// the cost and stops — it must not keep advising a route the operator
	// rejected. What remains is the part that is still news: which device the
	// suite's fresh ledgers queue behind, so a slow run is not a mystery.
	return fmt.Sprintf("the ledger sandbox %s is on %s (device-backed): every fresh ledger's first open pays "+
		"that device's journal commits — the operator's chosen scratch home, and the reason a loaded host costs "+
		"a suite more wall clock than its budget assumes", ledger, fsName)
}

// ambientLedgerPath is the ledger path the pin REPLACES — the operator's real
// one. Computed the same way internal/tools resolves its default (so the
// message names the actual file a test would touch) and used for reporting
// only, never to read anything.
func ambientLedgerPath() string {
	if gd, err := config.GopiDir(); err == nil {
		return filepath.Join(gd, config.MemoryDBFileName)
	}
	return "~/.gopi/" + config.MemoryDBFileName
}

func ambientLedgerCandidates() []string {
	out := make([]string, 0, 2)
	add := func(p string) {
		if p == "" {
			return
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		for _, seen := range out {
			if seen == p {
				return
			}
		}
		out = append(out, p)
	}
	// 1. The profile default of the CURRENT gopi data root — UNLESS that root is
	//    the pinned scratch root itself. A sandboxed suite points GOPI_HOME at
	//    its own throwaway root, so this candidate is the path its fallback would
	//    land on; that is a leak only when the root is the DEVELOPER's. Measured
	//    2026-11-04 (task 01a1a647): with the ledger pinned inside the suite's
	//    own profile, this candidate became the pinned ledger itself and the
	//    guard reported "IS the developer's real ledger" about a throwaway file
	//    under /dev/shm — a guard that fires on the correct shape teaches the
	//    next reader to ignore it. A path inside the pinned scratch root is by
	//    definition a sandbox; candidate 2 below still names the operator's real
	//    file, which is never inside that root.
	if gd, err := config.GopiDir(); err == nil && !pathUnder(gd, SuiteTempRoot()) {
		add(filepath.Join(gd, config.MemoryDBFileName))
	}
	// 2. The OPERATOR's live file, derived from $HOME — which the harness pin
	//    never rewrites. Without this, a suite whose GOPI_HOME is sandboxed
	//    would call the operator's real ledger "not a candidate" and the guard
	//    would bless an env override pointing straight at it.
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, gopiProfileDirName, config.MemoryDBFileName))
	}
	return out
}

// ledgerIsAmbient reports whether a ledger a caller NAMED is one of the
// operator's real files — never a sandbox a test may legitimately use.
func ledgerIsAmbient(ledger string) bool {
	abs, err := filepath.Abs(ledger)
	if err != nil {
		return false
	}
	for _, c := range ambientLedgerCandidates() {
		if abs == c {
			return true
		}
	}
	return false
}

// AssertSuiteLedgerIsInsideItsOwnProfile is the guard on the RELATION between
// the two pins, which neither one can see alone.
//
// THE DEFECT IT PINS (measured 2026-11-04, task 01a1a647). deliverTaskNotify
// suppresses the events of a ledger that is not its profile's own —
// taskNotifyOriginLedger compares memoryDBPath() with defaultMemoryDBPath(),
// i.e. "<GOPI_HOME>/.gopi/memory.db". A suite pins the ledger
// (PinSuiteLedgerEnv) and the profile (PinSuiteGopiProfile) through two
// INDEPENDENT calls, and they used to land in SIBLING directories
// (<scratch>/suite-ledger/ vs <scratch>/suite-gopi-home/.gopi). Every suite in
// this repository was therefore a foreign-ledger run: its events were recorded
// and reported but never broadcast, and four notify-delivery tests in
// internal/tools went red asserting alarms their own code had fired.
//
// Neither existing guard could see it: AssertSuiteLedgerIsPinned asks "is this
// the operator's real file?" (no), and AssertSuiteGopiProfileIsPinned asks "is
// the profile a sandbox?" (yes). The missing question is whether the two are the
// SAME sandbox — this one.
func AssertSuiteLedgerIsInsideItsOwnProfile(t *testing.T) {
	t.Helper()
	if !SuiteTempRootPinned() {
		t.Skipf("no pinned scratch root (%s absent and no %s override)", ShmDir, SuiteTempRootEnvName)
	}
	ledger := strings.TrimSpace(os.Getenv(config.MemoryDBEnv))
	if ledger == "" {
		t.Skipf("%s is unset — AssertSuiteLedgerIsPinned owns that failure", config.MemoryDBEnv)
	}
	gd, err := config.GopiDir()
	if err != nil {
		t.Fatalf("resolving this suite's gopi data root: %v", err)
	}
	// Scope: the relation is only ENFORCEABLE where the suite pins a profile at
	// all (PinSuiteGopiProfile). A suite with an ambient GOPI_HOME has no profile
	// for its ledger to be inside, and this guard must not claim one.
	if !pathUnder(gd, SuiteTempRoot()) {
		t.Skipf("this suite pins no profile (%s resolves to %q, outside the scratch root %q), so there is "+
			"no profile for the ledger to be inside", config.GopiDirEnv, gd, SuiteTempRoot())
	}
	own := config.ProfilePathIn(filepath.Dir(gd), config.MemoryDBFileName)
	if filepath.Clean(ledger) != filepath.Clean(own) {
		t.Fatalf("this suite's ledger is %q but its own profile's ledger is %q — the two pins are not the "+
			"same sandbox, so taskNotifyDelivers reads every event this suite fires as coming from a FOREIGN "+
			"ledger and SUPPRESSES it (its events belong to that ledger's own sink). A notify test then "+
			"fails on the ABSENCE of the log it just asked the code to write. Pin the ledger INSIDE the "+
			"profile (%s), never beside it", ledger, own, "PinSuiteLedgerEnv")
	}
}

// AssertSuiteLedgerIsPinned is the one-line guard every suite runs as a test:
// it fails when the suite's ledger resolves to the operator's real file.
// Without it a future TestMain edit silently re-opens the leak — and the leak
// is invisible from inside the suite, which is exactly why the row above sat
// unnoticed until the merger printed it.
func AssertSuiteLedgerIsPinned(t *testing.T) {
	t.Helper()
	if !SuiteTempRootPinned() {
		t.Skipf("no pinned scratch root (%s absent and no %s override) — the suite runs on the ambient "+
			"ledger by design (%s)", ShmDir, SuiteTempRootEnvName, gotestSuiteNoPinEnvName)
	}
	ledger := strings.TrimSpace(os.Getenv(config.MemoryDBEnv))
	legacy := strings.TrimSpace(os.Getenv(config.MemoryDBEnvLegacy))
	if legacy != "" {
		// The legacy spelling is the same override under its old name: a test
		// may legitimately name either, but an ambient legacy value smuggles
		// the real ledger back in through the fallback chain.
		t.Fatalf("%s=%q is set — an ambient legacy ledger override smuggles the real ledger back in through "+
			"the fallback chain (%s masks it)", config.MemoryDBEnvLegacy, legacy, "PinSuiteLedgerEnv")
	}
	if ledger == "" {
		t.Fatalf("%s is unset — the suite fell back to the operator's real ledger (%s), and the ledger is "+
			"APPEND-ONLY, so a fixture row written there is permanent (that is how t-stale-sibling made the "+
			"merger report a phantom /tmp repo). The suite entry point must pin it (%s), or a test that wants "+
			"its OWN ledger must name it", config.MemoryDBEnv, ambientLedgerPath(), "PinSuiteLedgerEnv")
	}
	abs, err := filepath.Abs(ledger)
	if err != nil {
		t.Fatalf("resolving %s=%q: %v", config.MemoryDBEnv, ledger, err)
	}
	// The rule is "never the operator's REAL ledger", not "must be inside this
	// suite's scratch root": a test that names its own sandbox and hands it to
	// a CHILD process (syncTestDB + TestSyncImportTwoProcessConcurrent) is
	// pinning an explicit ledger that need not live under the scratch root,
	// and the suite pin deliberately leaves such a choice alone. What must
	// never happen is resolving to the live file.
	if ledgerIsAmbient(abs) {
		t.Fatalf("%s=%q IS the developer's real ledger (%s) — the suite would read (and could write) the "+
			"append-only task history instead of a sandbox. Name a throwaway ledger, or let the suite "+
			"entry point pin one (%s)", config.MemoryDBEnv, ledger, ambientLedgerPath(), "PinSuiteLedgerEnv")
	}
}
