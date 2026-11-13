package main

// agent_spawn_guard_env_isolation_test.go — the REGRESSION GUARD for the test
// FIXTURE (not for the production guard, which stays untouched).
//
// THE MEASURED DEFECT (why this file exists): when these tests run inside a
// worker-daemon child — exactly how the repo's own agents run the suite on this
// host — the test PROCESS env carries the daemon handshake pair this process
// inherited: workerDaemonEnv + daemonReadyEnv. Before the fix, guardTestEnv
// stripped only the guard's own knobs, so the pair leaked into the
// "operator-style" child of TestAgentSpawnGuardAllowsOperatorStyleLaunch /
// TestAgentSpawnGuardWorkerTaskRunChainIsAllowed /
// TestAgentSpawnGuardAllowsOperatorTerminalInsideAgentSession. That child is
// NOT a daemon child and its fd daemonControlChildFD is an ordinary file, so
// the production path (runTaskWorker → workerDaemonRun →
// workerDaemonInheritLock) correctly refused — "inherited worker lock fd is not
// a usable file" — and three POSITIVE cases went FALSELY red. The production
// refusal was right; the fixture was lying about the child.
//
// The two cases below make that failure mode reproducible on ANY host (the real
// occurrence needs a worker-daemon parent, which a plain `go test` cannot
// arrange), and they are the discriminating A/B evidence:
//
//	① in-process — with the pair in the process env, guardTestEnv must not carry
//	  them into the child env (RED on the pre-fix helper);
//	② end-to-end — a REAL `gopi worker --once` launched in exactly that shape
//	  must run and print the loop banner (RED on the pre-fix helper, which
//	  routes it into the daemon path where it refuses).
//
// The negative case is unmoved: TestAgentSpawnGuardRefusesWorkerFromAgentSession
// still builds its agent-shaped child by pure inheritance, because the guard's
// signature knobs are a different class (config.*Env, stripped by
// stripAgentSignature) from the daemon handshake pair.

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/heavytest"
	"github.com/nick/gopi/internal/procname"
	"github.com/nick/gopi/internal/testutil"
)

// envHas reports whether name is carried by env. It lives here (not with the
// helper under test) on purpose: this file is the FIXTURE's regression guard and
// must compile and run unchanged BEFORE and AFTER the fix — a helper shared with
// the code under test could not be run against the pre-fix helper at all.
func envHas(env []string, name string) bool {
	for _, kv := range env {
		if n, _, _ := strings.Cut(kv, "="); n == name {
			return true
		}
	}
	return false
}

// TestGuardTestEnvStripsDaemonHandshakeVars pins the helper's contract: the
// daemon handshake pair must NEVER reach a guard-test child env. The variables
// are set with t.Setenv (Rule 17) so the process env of THIS test carries them
// exactly as a worker-daemon parent leaves them — the helper reads the process
// env, so a case that only set them in the child would not exercise the leak.
func TestGuardTestEnvStripsDaemonHandshakeVars(t *testing.T) {
	t.Setenv(workerDaemonEnv, daemonMarkerValue)
	t.Setenv(daemonReadyEnv, strconv.Itoa(daemonControlChildFD))

	env := guardTestEnv(t, t.TempDir())()

	for _, name := range []string{workerDaemonEnv, daemonReadyEnv} {
		if envHas(env, name) {
			t.Errorf("guardTestEnv leaked %s into the child env — an operator-style launch must not inherit the daemon handshake: the child would be routed into workerDaemonRun (daemonChild) and refused, which is the false-red this fix removes", name)
		}
	}
	// The strip must be SURGICAL: the sandbox home has to survive it (a leak
	// fix that also drops GOPI_HOME would trade one false red for another).
	if !envHas(env, "GOPI_HOME") {
		t.Errorf("guardTestEnv dropped GOPI_HOME — the strip must remove only the daemon handshake pair")
	}
}

// TestSuiteProcessIsNotADaemonChild is the IN-PROCESS half's guard (the
// E2E case above covers the spawned-child half). The suite's TestMain drops the
// inherited daemon handshake pair, so this process — which calls runTaskWorker
// DIRECTLY in the serve/worker-split tests — must not look like a daemon child;
// pre-pin it did, and runTaskWorker took workerDaemonRun → workerDaemonInheritLock
// and refused (the measured TestRunTaskWorker_OnceSweepsZombieFirst failure).
// Under a plain developer shell the assertion passes vacuously (no pair was
// inherited) — the discriminating A/B is the same suite run under a daemon
// parent, where it is RED pre-pin and GREEN post-pin.
func TestSuiteProcessIsNotADaemonChild(t *testing.T) {
	if v, present := os.LookupEnv(workerDaemonEnv); present {
		t.Errorf("%s=%q survived into the suite process — the suite's TestMain pin must drop the daemon "+
			"handshake pair, or every in-process runTaskWorker test takes the daemon path and refuses",
			workerDaemonEnv, v)
	}
	if workerDaemonChild() {
		t.Errorf("workerDaemonChild() is true in the test process — an in-process runTaskWorker would be " +
			"routed into workerDaemonRun and refuse an inherited lock fd that does not exist")
	}
}

// TestAgentSpawnGuardOperatorStyleLaunchSurvivesInheritedDaemonHandshake is the
// end-to-end half: the production binary, launched the operator's way, must
// start the worker loop even when this test process inherited the daemon
// handshake pair. Pre-fix the child carries the pair, takes the daemon path and
// refuses (non-zero, no banner); post-fix the helper strips it and the loop
// starts — the same measurement the falsely-red positive cases make, isolated
// from the agent-signature machinery so the two failure modes cannot mask each
// other.
//
// The pair is installed with t.Setenv (Rule 17) rather than inherited: the
// suite's own TestMain pin drops any ambient copy, so this case states the
// parent's shape explicitly and stays discriminating on every host.
func TestAgentSpawnGuardOperatorStyleLaunchSurvivesInheritedDaemonHandshake(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	SandboxRepoContext(t) // repo-context sandbox: the worker child must not inherit this process's cwd (the live repo).
	bin := testutil.GopiBin(t)
	t.Setenv(workerDaemonEnv, daemonMarkerValue)
	t.Setenv(daemonReadyEnv, strconv.Itoa(daemonControlChildFD))

	// The operator's launch: no agent signature (that is a different case) and
	// exactly the fixture this file guards. Bounded one-shot fixture child
	// (internal/testutil/runfixture.go).
	env := stripAgentSignature(guardTestEnv(t, t.TempDir())())

	out, err := testutil.FixtureCommandEnv(bin, env, CliVerbWorker, workerVerbArgv)
	if code := exitCodeOf(t, err); code != 0 {
		t.Fatalf("operator-style `gopi worker %s` must run even when the test process inherited %s + %s: exit=%d\noutput:\n%s",
			workerVerbArgv, workerDaemonEnv, daemonReadyEnv, code, out)
	}
	if !strings.Contains(string(out), procname.WorkerLoopStartLine) {
		t.Errorf("operator-style launch did not start the loop:\n%s", out)
	}
	if strings.Contains(string(out), "inside an agent session") {
		t.Errorf("operator-style launch was refused by the agent guard:\n%s", out)
	}
}
