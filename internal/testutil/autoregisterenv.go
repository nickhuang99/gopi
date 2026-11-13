package testutil

import (
	"fmt"
	"os"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// Suite AUTO-REGISTER env hermeticity (the 2026-10-29 clone-channel red).
//
// ROOT CAUSE THIS FILE EXISTS FOR. internal/agent's start hook decides whether
// a session mints a ledger row from ITS OWN PROCESS ENVIRONMENT
// (`autoTaskRegisterEnabled` → `getenv(config.TaskAutoRegisterEnv)`), and the
// clone-channel round exports the OFF value into every child it spawns
// (`cmd/agent/devworkspace_channel.go`, `agent.AutoTaskRegisterOff`) so a round
// does not mint a mirror row. `go test ./internal/agent/` run from inside such a
// round inherits it. Measured (task 01a188a8, baseline tree 6bd7fa5):
//
//	GOPI_AUTO_TASK_REGISTER=0   go test ./internal/agent/ -count=1  → FAIL, 35 arms
//	GOPI_AUTO_TASK_REGISTER=1   go test ./internal/agent/ -count=1  → ok, 1427 tests
//	(env -u GOPI_AUTO_TASK_REGISTER) go test … -count=1             → ok, 1427 tests
//
// Every one of the 35 reds reads "start hook did not register a row": the arms
// exercise the auto-registration path and none of them pinned the switch, so
// the AMBIENT value decided their verdict. That is a test that is not sealed
// against its environment, not a product defect — the product honoured the
// ambient value exactly as documented.
//
// WHY THE FIX IS HERE AND NOT IN THE 35 ARMS (Rule 35: one convergence, no new
// mechanism). Pinning the variable in each arm is 35 edits that the 36th arm
// forgets; the suites already share ONE entry point (PinSuiteForTestMain), and
// this pin rides it exactly as the notify (suiteenv.go), git-routing (gitenv.go)
// and ledger (ledgerpin.go) pins do. A test that WANTS the switch OFF says so
// with t.Setenv in its own body — that still wins, because t.Setenv sets the
// process environment for that test's duration and this pin only runs once, in
// TestMain, before any test.
//
// WHY THE VARIABLE IS DROPPED RATHER THAN PINNED TO A VALUE. The switch is
// default-ON and the repository's own contract tests assert the ON direction
// (`TestAutoTaskRegisterOnAndEndHook` registers a row; the OFF direction is
// asserted with an explicit t.Setenv in `TestAutoTaskRegisterKillSwitch`).
// Dropping the name restores exactly the environment those arms were written
// for — the same state an ordinary developer shell has — instead of substituting
// a second opinion about what the default should be. A pin that WROTE "1" would
// also mask a genuine regression in the default-on direction.
//
// The name is config.TaskAutoRegisterEnv (Rule 14): this package can import
// neither internal/agent nor internal/tools (both of their test binaries import
// testutil), so the shared leaf is the only place the spelling can live.
//
// Rule 35's four questions, answered:
//   - What breaks without it: 35 arms in internal/agent report on the ambient
//     value instead of on the code; the same tree is green or red depending on
//     which shell started it (measured above).
//   - Why not in-process: the read is `os.Getenv` inside internal/agent, at
//     whatever moment a test's start hook fires; no package-level default can
//     reach it, and a per-arm t.Setenv is the 35-edit duplicate this file
//     replaces.
//   - Owner / reclaim: nothing is created. os.Unsetenv removes a name from this
//     process's environment; the kernel reclaims it at exit. No file, no hook,
//     no resident process, nothing to age out.
//   - What the next reader sees: the dropped name is REPORTED on stderr (Rule
//     15), and AssertSuiteAutoRegisterEnvHermetic is the in-suite guard a suite
//     runs to prove the pin holds.

// PinSuiteAutoRegisterEnv drops the auto-register kill switch from THIS
// process's environment, so a suite started from a clone-channel round (or any
// shell that exported it) does not hand the switch to every test that never
// asked about it.
//
// The value is REPORTED when one was dropped, never swallowed (Rule 15b): an
// operator who exported the variable learns that this suite does not honour it,
// instead of silently running in a different environment than the one they set.
// Unlike the git-routing pin, the value is NOT kept for a re-arm: no suite has a
// legitimate child that must inherit an ambient OFF, and one that does sets it
// itself with t.Setenv (a test) or on the child's own environment (production
// code, which builds it explicitly — cmd/agent's round child).
func PinSuiteAutoRegisterEnv(suite string) {
	value, ok := os.LookupEnv(config.TaskAutoRegisterEnv)
	if !ok {
		return
	}
	if err := os.Unsetenv(config.TaskAutoRegisterEnv); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: suite %q: %s not dropped: %v — an ambient OFF makes every "+
			"auto-registration arm report on the environment instead of on the code (2026-10-29: 35 reds)\n",
			suite, config.TaskAutoRegisterEnv, err)
		return
	}
	fmt.Fprintf(os.Stderr, "testutil: suite %q: dropped %s=%q from this process — the auto-register "+
		"switch is read from the PROCESS environment (internal/agent's start hook), so an ambient value "+
		"decides 35 arms' verdicts; a test that wants OFF pins it with t.Setenv\n",
		suite, config.TaskAutoRegisterEnv, value)
}

// AssertSuiteAutoRegisterEnvHermetic is the in-suite guard: it fails when the
// auto-register switch is still present in this process's environment, i.e. when
// the suite entry point did not install the pin. A suite that adds a TestMain of
// its own, or an entry-point edit that drops the call, reopens the ambient
// dependence — and the leak is invisible from inside the suite, which is why the
// first fix (2026-10-29) needed this assertion beside it.
func AssertSuiteAutoRegisterEnvHermetic(t *testing.T) {
	t.Helper()
	if v, present := os.LookupEnv(config.TaskAutoRegisterEnv); present {
		t.Errorf("%s=%q is set in the suite environment — the suite entry point must drop it "+
			"(PinSuiteAutoRegisterEnv): the auto-register switch is read from the process environment, so "+
			"an ambient value makes every auto-registration arm report on the environment instead of on the "+
			"code (2026-10-29: 35 arms red under =0, green under =1). A test that wants OFF pins it with t.Setenv",
			config.TaskAutoRegisterEnv, v)
	}
}
