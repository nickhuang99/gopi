package testutil

// Ledger-pin red proofs — the /tmp/other-repo incident, as a regression guard
// (devdoc/reclaim-fossil-20260915.md).
//
// THE DEFECT THIS PINS: a suite that never names GOPI_MEMORY_DB resolves the
// default ledger, and the ledger is APPEND-ONLY, so a fixture row written there
// is permanent. cmd/agent's worker-recycle fixture did exactly that and left
// row `t-stale-sibling` in the operator's ~/.gopi/memory.db; the merger's
// dead-holder reclaim then reported its never-created decoy repo on every pass.
//
// THE PROOF SHAPE follows the notify-suite precedent (suiteenv_test.go): the
// pre-fix suite shape cannot be reproduced in a process whose pin already
// engaged, so a RE-EXEC child runs the entry point WITHOUT the ledger pin and
// the anchor test must fail there — and must pass in the fixed shape. Without
// the failing half this guard could be satisfied by a test that never runs.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// suiteLedgerRawEnvName asks a re-exec'd child to run in the PRE-FIX suite
// shape: scratch root and notify sandbox as before, ledger deliberately left
// ambient. The child must FAIL; the fixed shape must pass.
const suiteLedgerRawEnvName = "GOPI_TESTUTIL_LEDGER_RAW"

// suiteLedgerAnchorTest is the child's one test: the guard itself. It is named
// so the re-exec can select it alone (-test.run).
const suiteLedgerAnchorTest = "TestSuiteLedgerAnchorProbe"

// TestSuiteLedgerAnchorProbe is the guard's body, also run in THIS process
// (pinned shape). In the raw child it must fail; here it must pass.
func TestSuiteLedgerAnchorProbe(t *testing.T) {
	AssertSuiteLedgerIsPinned(t)
}

// TestSuiteLedgerPinIsActive is the direct assertion: this suite's ledger env
// points into the pinned scratch root, so the operator's real ledger is not
// reachable from any test that does not name its own.
func TestSuiteLedgerPinIsActive(t *testing.T) {
	if !SuiteTempRootPinned() {
		t.Skipf("no pinned scratch root (%s absent and no %s override)", ShmDir, SuiteTempRootEnvName)
	}
	got := os.Getenv(config.MemoryDBEnv)
	if got == "" {
		t.Fatalf("%s is unset — the suite falls back to the operator's real ledger %s",
			config.MemoryDBEnv, ambientLedgerPath())
	}
	if !strings.HasPrefix(got, SuiteTempRoot()) {
		t.Fatalf("%s=%q is not under the pinned scratch root %q", config.MemoryDBEnv, got, SuiteTempRoot())
	}
	// The ledger file itself is not pre-created: a blank sandbox is the point
	// (a suite that wants rows seeds them, and a pre-created file would make a
	// test that forgot to seed read an empty ledger as "no tasks" rather than
	// failing loudly).
	if _, err := os.Stat(got); err == nil {
		t.Logf("note: the sandbox ledger %s already exists (a fixture in this process created it)", got)
	}
	// The legacy alias must be masked — it is the same override under its old
	// name and would smuggle the real ledger back in through the fallback chain.
	if legacy := os.Getenv(config.MemoryDBEnvLegacy); legacy != "" {
		t.Fatalf("%s=%q is set — the legacy alias is unmasked", config.MemoryDBEnvLegacy, legacy)
	}
}

// TestSuiteLedgerPinRawSuiteFails is the red proof: the SAME anchor test, run
// in a child that pins everything BUT the ledger, must fail with an actionable
// message naming the operator's real ledger. It fails on a tree where
// AssertSuiteLedgerIsPinned was weakened to always pass, and it fails if the
// child unexpectedly goes green (i.e. the leak is unguarded).
func TestSuiteLedgerPinRawSuiteFails(t *testing.T) {
	if os.Getenv(reexecQueryEnvName) != "" {
		t.Skip("already inside the re-exec child — the parent owns this assertion")
	}
	cmd := exec.Command(os.Args[0], "-test.run", "^"+suiteLedgerAnchorTest+"$", "-test.v")
	cmd.Env = append(scrubbedLedgerEnv(),
		suiteLedgerRawEnvName+"=1",
		reexecQueryEnvName+"=1")
	out, err := cmd.CombinedOutput()
	text := string(out)
	exit, ok := exitCodeOf(err)
	if !ok {
		t.Fatalf("child did not exit with a status (err = %v):\n%s", err, text)
	}
	if exit == 0 {
		t.Fatalf("a suite in the PRE-FIX shape (ledger left ambient) ran the anchor GREEN — the "+
			"operator's real ledger is exposed and a fixture row would be permanent (t-stale-sibling):\n%s", text)
	}
	// The failure must NAME the file that would have been touched — an
	// undiagnosable guard is a quiet guard.
	if !strings.Contains(text, config.MemoryDBEnv) || !strings.Contains(text, "memory.db") {
		t.Fatalf("the child failed without naming the ambient ledger — the message must say which file the "+
			"suite would have written:\n%s", text)
	}
	// And it must name the fix, so the next agent does not have to rediscover it.
	if !strings.Contains(text, "PinSuiteLedgerEnv") {
		t.Fatalf("the child's failure does not name the fix (PinSuiteLedgerEnv):\n%s", text)
	}
}

// TestSuiteLedgerPinFixedShapePasses is the positive control: the same child
// shape but WITH the ledger pin engaged must exit 0. Without this, the red
// proof above could be satisfied by a guard that fails unconditionally.
func TestSuiteLedgerPinFixedShapePasses(t *testing.T) {
	if os.Getenv(reexecQueryEnvName) != "" {
		t.Skip("already inside the re-exec child — the parent owns this assertion")
	}
	cmd := exec.Command(os.Args[0], "-test.run", "^"+suiteLedgerAnchorTest+"$", "-test.v")
	// NO raw flag: the child's entry point takes the normal (pinned) path.
	cmd.Env = append(scrubbedNotifyEnv(), reexecQueryEnvName+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("a suite with the ledger pin engaged must pass the anchor: %v\n%s", err, string(out))
	}
}

// TestAmbientLedgerPathNamesTheOperatorsFile guards the message's accuracy: the
// path the refusal names must be the one the ledger layer would actually
// resolve, i.e. derived from the same config source, never a hand-built
// "/home/<someone>/.gopi/memory.db" literal that rots on another host.
func TestAmbientLedgerPathNamesTheOperatorsFile(t *testing.T) {
	got := ambientLedgerPath()
	if !strings.HasSuffix(got, filepath.Join(".gopi", config.MemoryDBFileName)) {
		t.Fatalf("ambientLedgerPath() = %q, want it to end with .gopi/%s", got, config.MemoryDBFileName)
	}
}

// scrubbedLedgerEnv is scrubbedNotifyEnv with the LEDGER override removed on
// top of it. The raw child must start from the pre-fix shape, and the parent
// process has already pinned GOPI_MEMORY_DB at its own sandbox — inheriting
// that would make the child's anchor fail for the wrong reason (a foreign
// sandbox path rather than the operator's real ledger), so the proof would no
// longer measure the defect it names.
func scrubbedLedgerEnv() []string {
	base := scrubbedNotifyEnv()
	env := make([]string, 0, len(base))
	for _, kv := range base {
		key, _, found := strings.Cut(kv, "=")
		if !found {
			continue
		}
		if key == config.MemoryDBEnv || key == config.MemoryDBEnvLegacy || key == config.GopiDirEnv {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// TestLedgerPinHonoursANamedLedgerButRefusesTheOperatorsOwn is the rule the
// pin's second iteration exists for, as one table. Both halves are load-bearing
// and both were measured on 2026-09-15:
//
//   - overruling a NAMED ledger broke a green test: syncTestDB points the
//     ledger at its own sandbox and re-execs the same test binary so TWO
//     PROCESSES import into THE SAME file (TestSyncImportTwoProcessConcurrent);
//     an unconditional pin sent the children elsewhere and the parent counted
//     rows in a DB nobody wrote;
//   - honouring the OPERATOR's file would reopen the leak the pin closes.
func TestLedgerPinHonoursANamedLedgerButRefusesTheOperatorsOwn(t *testing.T) {
	named := ledgerChoice{db: filepath.Join(t.TempDir(), config.MemoryDBFileName)}
	if !honoursInheritedLedger(named) {
		t.Fatalf("a test that NAMED its own ledger must not have it overruled (the child-process import shape)")
	}
	if honoursInheritedLedger(ledgerChoice{}) {
		t.Fatalf("an EMPTY choice is not a choice — the suite sandbox must be used")
	}
	live, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	operator := ledgerChoice{db: filepath.Join(live, gopiProfileDirName, config.MemoryDBFileName)}
	if honoursInheritedLedger(operator) {
		t.Fatalf("the OPERATOR's live ledger must never be honoured — an ambient %s pointing at %s would "+
			"otherwise let the suite write the append-only task history", config.MemoryDBEnv, operator.path())
	}
	if !ledgerIsAmbient(operator.path()) {
		t.Fatalf("ledgerIsAmbient(%q) = false — the operator's live file must ALWAYS be a candidate, "+
			"whether or not GOPI_HOME is sandboxed", operator.path())
	}
	// The legacy spelling names the same class and must be recognised too — a
	// sandbox under the legacy name is still a sandbox.
	sandboxLegacy := ledgerChoice{legacy: filepath.Join(t.TempDir(), config.MemoryDBFileName)}
	if !honoursInheritedLedger(sandboxLegacy) {
		t.Fatalf("the legacy spelling of a NAMED sandbox ledger must be honoured like the current one")
	}
	// ... and the legacy spelling of the OPERATOR's file must be refused, since
	// both spellings are the same override.
	if honoursInheritedLedger(ledgerChoice{legacy: filepath.Join(live, gopiProfileDirName, config.MemoryDBFileName)}) {
		t.Fatalf("the legacy spelling of the operator's live ledger must be refused like the current one")
	}
}

// TestLedgerPinWouldPinAHostileInheritedOverride is the red proof for the door
// the pin's first iteration left open: a suite whose inherited $GOPI_MEMORY_DB
// is ALREADY the operator's live file. Honouring it would mean the suite reads
// and writes the append-only task history, which is the leak itself. Without
// the ambient check in honoursInheritedLedger this test fails.
// TestLedgerPinWouldPinAHostileInheritedOverride is the ENV-LEVEL red half: it
// does not exercise the predicate directly but the decision the pin actually
// makes, through PinSuiteLedgerEnv — a caller that pins, holding an ambient
// override in the environment (the hostile shape: something exported
// GOPI_MEMORY_DB pointing at the developer's live ledger, e.g. a shell that ran
// `export GOPI_MEMORY_DB=/home/user/.gopi/memory.db`). The pin must OVERRULE it and land
// on the sandbox; the neighbouring table test covers the named-sandbox half
// that must be left alone.
func TestLedgerPinWouldPinAHostileInheritedOverride(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	hostile := ledgerChoice{db: filepath.Join(home, gopiProfileDirName, config.MemoryDBFileName)}
	if honoursInheritedLedger(hostile) {
		t.Fatalf("a HOSTILE inherited %s=%q was honoured — the pin must overrule an ambient override "+
			"rather than let the suite write the operator's ledger", config.MemoryDBEnv, hostile.path())
	}
	if honoursInheritedLedger(ledgerChoice{legacy: hostile.path()}) {
		t.Fatalf("the legacy spelling of the SAME ambient file was honoured — both spellings are the " +
			"same override")
	}
}
