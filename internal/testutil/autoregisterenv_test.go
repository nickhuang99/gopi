package testutil

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// The two-sided proof for the auto-register env pin (autoregisterenv.go): the
// anchor is GREEN in this suite (which installs the pin) and RED in a re-exec'd
// child that reproduces the PRE-FIX shape — every other pin engaged, the
// auto-register switch left exactly as the ambient environment hands it over.
//
// Without the raw half, the guard could be satisfied by an entry point that
// happens to work on a developer shell and reopens the ambient dependence the
// moment a clone-channel round starts the suite. Without the pinned half, the
// raw proof could be satisfied by a guard that fails unconditionally.

// suiteAutoRegisterRawEnvName asks a re-exec'd child to run in the PRE-FIX
// suite shape: scratch root, notify and ledger pinned as before, the
// auto-register switch deliberately left ambient. The child must FAIL.
const suiteAutoRegisterRawEnvName = "GOPI_TESTUTIL_AUTOREGISTER_RAW"

// suiteAutoRegisterAnchorTest is the child's one test, selected with -test.run.
const suiteAutoRegisterAnchorTest = "TestSuiteAutoRegisterEnvAnchorProbe"

// TestSuiteAutoRegisterEnvAnchorProbe is the guard's body: it runs in THIS
// process (pinned shape, must pass) and in both re-exec children.
func TestSuiteAutoRegisterEnvAnchorProbe(t *testing.T) {
	AssertSuiteAutoRegisterEnvHermetic(t)
}

// TestSuiteAutoRegisterEnvIsHermetic is the in-process assertion: this suite's
// entry point dropped the switch before any test ran, so no arm here reports on
// the shell that started the run.
func TestSuiteAutoRegisterEnvIsHermetic(t *testing.T) {
	if os.Getenv(reexecQueryEnvName) != "" || os.Getenv(suiteAutoRegisterRawEnvName) != "" {
		t.Skip("re-exec child — it deliberately runs without the pin")
	}
	AssertSuiteAutoRegisterEnvHermetic(t)
}

// TestSuiteAutoRegisterRawSuiteFails: a suite in the pre-fix shape, started with
// the clone-channel's ambient OFF value, must fail the anchor and name both the
// variable and the fix.
func TestSuiteAutoRegisterRawSuiteFails(t *testing.T) {
	if os.Getenv(reexecQueryEnvName) != "" {
		t.Skip("already inside the re-exec child — the parent owns this assertion")
	}
	cmd := exec.Command(os.Args[0], "-test.run", "^"+suiteAutoRegisterAnchorTest+"$", "-test.v")
	cmd.Env = append(scrubbedNotifyEnv(),
		suiteAutoRegisterRawEnvName+"=1",
		reexecQueryEnvName+"=1",
		// The ambient value the clone-channel round exports (the 2026-10-29
		// red): spelled through the ONE holder of the name, never typed here.
		config.TaskAutoRegisterEnv+"=0")
	out, err := cmd.CombinedOutput()
	text := string(out)
	exit, ok := exitCodeOf(err)
	if !ok {
		t.Fatalf("child did not exit with a status (err = %v):\n%s", err, text)
	}
	if exit == 0 {
		t.Fatalf("a suite in the PRE-FIX shape (auto-register switch left ambient) ran the anchor GREEN — "+
			"the ambient value decides its arms' verdicts:\n%s", text)
	}
	// An undiagnosable guard is a quiet guard: the failure must name the
	// variable the child carried and the pin that closes it.
	if !strings.Contains(text, config.TaskAutoRegisterEnv) {
		t.Fatalf("the child failed without naming %s — the message must say which variable leaked:\n%s",
			config.TaskAutoRegisterEnv, text)
	}
	if !strings.Contains(text, "PinSuiteAutoRegisterEnv") {
		t.Fatalf("the child's failure does not name the fix (PinSuiteAutoRegisterEnv):\n%s", text)
	}
}

// autoRegisterEnvWireName is the WIRE spelling of the switch, pinned by literal
// on purpose: the literal IS the contract, and the constant must equal it. It is
// the same shape internal/agent's TestAutoTaskRegisterOff_IsOff uses — a rename
// of the constant without renaming the spawners' contract fails HERE instead of
// silently leaving a round's children auto-registering again.
const autoRegisterEnvWireName = "GOPI_AUTO_TASK_REGISTER"

// TestAutoRegisterEnvNameIsTheWireSpelling guards the constant this pin drops:
// a value that drifted from the wire spelling would make PinSuiteAutoRegisterEnv
// drop a variable nothing reads, i.e. a pin that looks installed and seals
// nothing (Rule 15 — a silent no-op is the defect, not the inconvenience).
func TestAutoRegisterEnvNameIsTheWireSpelling(t *testing.T) {
	if config.TaskAutoRegisterEnv != autoRegisterEnvWireName {
		t.Fatalf("config.TaskAutoRegisterEnv = %q, want the wire spelling %q — a drifted name makes "+
			"PinSuiteAutoRegisterEnv drop a variable no reader consumes",
			config.TaskAutoRegisterEnv, autoRegisterEnvWireName)
	}
}

// TestAutoRegisterPinActuallyDropsTheValue is the behavioural half of the proof
// above, measured on THIS process instead of on a child: the suite entry point
// ran before any test, so the variable must be absent here even when the shell
// that started the run exported it. It re-uses the same assertion the guard
// gives every suite — one predicate, two callers, so the two cannot disagree.
func TestAutoRegisterPinActuallyDropsTheValue(t *testing.T) {
	if os.Getenv(reexecQueryEnvName) != "" || os.Getenv(suiteAutoRegisterRawEnvName) != "" {
		t.Skip("re-exec child — it deliberately runs without the pin")
	}
	if _, present := os.LookupEnv(config.TaskAutoRegisterEnv); present {
		t.Fatalf("%s is present in this suite's environment after the entry point ran — the pin did not "+
			"engage, so the 35 auto-registration arms in internal/agent would report on the ambient value "+
			"again", config.TaskAutoRegisterEnv)
	}
}

// TestSuiteAutoRegisterPinnedShapePasses is the positive control — and the
// measurement the epic actually asks for: the SAME ambient OFF value that makes
// the raw child red must leave a pinned suite green. The child takes the normal
// entry-point path, so the pin drops the switch before any test runs.
func TestSuiteAutoRegisterPinnedShapePasses(t *testing.T) {
	if os.Getenv(reexecQueryEnvName) != "" {
		t.Skip("already inside the re-exec child — the parent owns this assertion")
	}
	cmd := exec.Command(os.Args[0], "-test.run", "^"+suiteAutoRegisterAnchorTest+"$", "-test.v")
	cmd.Env = append(scrubbedNotifyEnv(),
		reexecQueryEnvName+"=1",
		config.TaskAutoRegisterEnv+"=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("a suite with the auto-register pin engaged must pass the anchor even though the ambient "+
			"environment carried %s=0: %v\n%s", config.TaskAutoRegisterEnv, err, string(out))
	}
}
