package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/heavytest"
	"github.com/nick/gopi/internal/procname"
	"github.com/nick/gopi/internal/testutil"
)

// agent_spawn_guard_test.go — the END-TO-END proof of the operator's ruling
// (2026-09-12): `gopi worker` and `gopi task run` must REFUSE to start when the
// process sits inside an agent session, and must work untouched from an
// operator-style shell.
//
// Both halves run the REAL binary, because the property under test is "does the
// shipped CLI refuse" — a unit assertion on the predicate would not prove the
// hooks are wired (the hooks are exactly where a guard quietly does nothing).
//
// ENV DISCIPLINE (this is the subtle part): these tests run INSIDE a gopi agent
// session when the repo's own agents run the suite — the parent environment
// therefore already carries the signature, and `exec.Command` inherits it. That
// is precisely why the AGENT case can be built by pure inheritance, and why the
// OPERATOR case must strip the signature explicitly. Both directions are real
// measurements, not simulations.

// guardTestEnv is the sandboxed environment for a guard test: the current
// process environment (so a real agent signature is present whenever there is
// one) with the guard's own knobs cleared so a case can set exactly what it
// means to test. The returned setter replaces a variable for the child only.
//
// THE DAEMON-HANDSHAKE VARS ARE STRIPPED TOO, and that is a TEST-ISOLATION
// requirement, not a guard relaxation (the guard itself is untouched). These
// tests run inside a gopi agent session on the host they were written for — and
// when that agent session is a WORKER-DAEMON CHILD, os.Environ() carries the
// daemon handshake pair this process inherited:
//
//   - workerDaemonEnv  — the child-identity marker (its wire value is defined
//     ONCE, in cmd/agent/cli_worker_daemon.go), which routes `gopi worker`
//     into workerDaemonRun instead of the ordinary loop (runTaskWorker), and
//   - daemonReadyEnv   — the inherited handshake fd (wire value defined ONCE,
//     in cmd/agent/cli_daemon_spawn.go).
//
// Inherited into the "operator-style" child they are a LIE about that child:
// it is not a daemon child, and fd 3 in it is an ordinary file, so
// workerDaemonInheritLock correctly refuses ("inherited worker lock fd 3 is not
// a usable file") and the positive cases go FALSELY red — the production
// refusal is right, the test fixture was wrong. An operator's own shell has
// neither variable, so a case that wants the daemon-child shape must state it
// explicitly with the setter (see the isolation regression test alongside).
func guardTestEnv(t *testing.T, home string) func(...string) []string {
	t.Helper()
	base := make([]string, 0, len(os.Environ())+4)
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case config.AgentSpawnGuardOverrideEnv, config.AgentSpawnGuardOffEnv, config.HumanTerminalEnv,
			workerDaemonEnv, daemonReadyEnv:
			continue // a case sets these deliberately (or leaves them unset)
		}
		base = append(base, kv)
	}
	base = append(base, "GOPI_HOME="+home)
	// No peak-hour exemption is needed here (and none exists): this file's
	// subject IS the spending start (it launches real `gopi worker --once` /
	// `task run` to prove the LAUNCH rules), and the peak-hour cost guard that
	// used to refuse such a start inside a 2x window was removed outright
	// (devdoc/peak-guard-removal.md) — the clock can no longer change a verdict.
	return func(extra ...string) []string {
		return append(append([]string{}, base...), extra...)
	}
}

// workerVerbArgv is the argv under test for the worker loop. `--once` is
// deliberate: a worker that ever DID start must drain and exit on its own, so a
// regression cannot hang the suite (the 04:12 failure mode, in a test).
const workerVerbArgv = "--once"

// TestAgentSpawnGuardRefusesWorkerFromAgentSession is the NEGATIVE case the
// acceptance criteria name: a process whose environment carries an agent
// signature must be refused, with the dedicated exit code and a message that
// names the signal and the remedy.
func TestAgentSpawnGuardRefusesWorkerFromAgentSession(t *testing.T) {
	bin := testutil.GopiBin(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	env := guardTestEnv(t, t.TempDir())
	// The simulated agent-spawned environment: exactly the signature a gopi
	// agent session exports and its children inherit (values from the live
	// 04:12 session, so the fixture mirrors reality rather than a guess).
	agentEnv := env(
		config.AgentSessionEnv+"=6424c4c1-0adb-48e8-bf43-d6d0baae5e0e",
		config.HermesSessionIDEnv+"=20260912_040123_ec45a1",
		config.AgentSessionAncestorEnv+"=1", // an ancestor pid, never ours
	)

	for _, argv := range [][]string{
		{CliVerbWorker, workerVerbArgv},
		{CliVerbWorker, "t-nonexistent"},
	} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			// Bounded one-shot fixture child (internal/testutil/runfixture.go).
			out, err := testutil.FixtureCommandEnv(bin, agentEnv, argv...)
			code := exitCodeOf(t, err)
			if code != config.AgentGuardRefusalExitCode {
				t.Fatalf("`gopi %s` from an agent session: exit=%d want %d (refusal)\noutput:\n%s",
					strings.Join(argv, " "), code, config.AgentGuardRefusalExitCode, out)
			}
			for _, want := range []string{"inside an agent session", "gopi-worker-up.sh", "gopi task submit"} {
				if !strings.Contains(string(out), want) {
					t.Errorf("refusal output missing %q:\n%s", want, out)
				}
			}
			if strings.Contains(string(out), "worker: loop start") {
				t.Errorf("the worker loop STARTED despite the refusal:\n%s", out)
			}
		})
	}
}

// TestAgentSpawnGuardAllowsOperatorStyleLaunch is the POSITIVE case: the same
// binary, the same argv, an environment WITHOUT the agent signature (what the
// operator's own terminal looks like) must start normally. The worker drains an
// empty sandbox queue and exits 0; the read-only verbs answer on the spot.
func TestAgentSpawnGuardAllowsOperatorStyleLaunch(t *testing.T) {
	bin := testutil.GopiBin(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	env := guardTestEnv(t, t.TempDir())
	operatorEnv := env() // signature stripped by guardTestEnv? no — inherited;
	// so strip it explicitly. An operator shell has NO agent signature, which
	// on a developer host is the ONLY difference that matters.
	operatorEnv = stripAgentSignature(operatorEnv)

	out, err := testutil.FixtureCommandEnv(bin, operatorEnv, CliVerbWorker, workerVerbArgv)
	if code := exitCodeOf(t, err); code != 0 {
		t.Fatalf("operator-style `gopi worker --once` must run: exit=%d\noutput:\n%s", code, out)
	}
	if !strings.Contains(string(out), "worker: loop start") {
		t.Errorf("operator-style launch did not start the loop:\n%s", out)
	}
	if strings.Contains(string(out), "inside an agent session") {
		t.Errorf("operator-style launch was refused:\n%s", out)
	}

	// Read-only verbs stay untouched even WITH the signature present.
	agentEnv := env(config.AgentSessionEnv + "=x")
	out, err = testutil.FixtureCommandEnv(bin, agentEnv, CliVerbTask, "list")
	if code := exitCodeOf(t, err); code != 0 {
		t.Fatalf("`gopi task list` must never be guarded: exit=%d\noutput:\n%s", code, out)
	}
}

// TestAgentSpawnGuardHelpIsNeverRefused: a usage query keeps working from an
// agent session (the P0-1 contract is "no side-effects", not "no answers") —
// the guard sits AFTER the -h scan on purpose.
func TestAgentSpawnGuardHelpIsNeverRefused(t *testing.T) {
	bin := testutil.GopiBin(t)
	env := guardTestEnv(t, t.TempDir())
	agentEnv := env(config.AgentSessionEnv+"=x", config.AgentSessionAncestorEnv+"=1")

	for _, argv := range [][]string{
		{CliVerbWorker, "-h"},
		{CliVerbTask, taskVerbRun, "-h"},
	} {
		out, err := testutil.FixtureCommandEnv(bin, agentEnv, argv...)
		if code := exitCodeOf(t, err); code != 0 {
			t.Errorf("`gopi %s` from an agent session must still print help: exit=%d\n%s",
				strings.Join(argv, " "), code, out)
		}
		if strings.Contains(string(out), "refusing to start") {
			t.Errorf("`gopi %s` was refused — the guard must sit after the -h scan:\n%s",
				strings.Join(argv, " "), out)
		}
	}
}

// TestAgentSpawnGuardWorkerTaskRunChainIsAllowed is the sanctioned-path proof:
// a REAL worker process (started the operator's way — signature stripped, so
// the launch is his) must be able to start its own `task run` child, which
// re-execs this same binary. That child inherits the worker's environment; the
// exemption rides on the process chain (parent argv == worker), not on a
// cleaned environment.
func TestAgentSpawnGuardWorkerTaskRunChainIsAllowed(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	binDir := t.TempDir()
	bin := filepath.Join(binDir, procname.NameGopi)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go.
	//
	// The FIXTURE-REPO variant, not the plain pin: this test's FIRST spawned
	// child is `task submit`, which resolves its own cwd as the repository the
	// row names — under the non-git sandbox cwd that submit is refused outright
	// ("--repo default: repo … is not a git work tree"), so the worker round
	// below it would never be reached. The fixture repo keeps the hazard closed
	// (its only registration is the main checkout, never removable).
	SandboxRepoContextInFixtureRepo(t)
	if err := os.Symlink(testutil.GopiBin(t), bin); err != nil {
		t.Fatalf("symlink gopi: %v", err)
	}
	// A real worker round: one pending row, a child that fails fast instead of
	// calling a model (the existing synthetic-child escape).
	home := t.TempDir()
	env := stripAgentSignature(guardTestEnv(t, home)(
		"GOPI_MEMORY_DB="+filepath.Join(t.TempDir(), "memory.db"),
		taskChildFailTestEnv+"=1",
		config.SkipClaimBroadcastEnv+"=1",
	))
	if out, code := runGopiIn(t, bin, env, "", CliVerbTask, "submit",
		"guard chain row", "--payload", "brief", "--task-id", "t-guard-chain"); code != 0 {
		t.Fatalf("submit: exit=%d\n%s", code, out)
	}
	out, code := runGopiIn(t, bin, env, "", CliVerbWorker, workerVerbArgv)
	if code != 0 {
		t.Fatalf("worker with a claimable row must drain and exit 0: exit=%d\n%s", code, out)
	}
	if strings.Contains(out, "inside an agent session") {
		t.Fatalf("the worker's own task-run child was refused (the sanctioned chain broke):\n%s", out)
	}
}

// TestAgentSpawnGuardAllowsOperatorTerminalInsideAgentSession is path (b) of
// the operator's own escape: he opens a shell INSIDE hermes (or inside a gopi
// agent session) and runs `gopi worker` from there. The environment genuinely
// is inherited from the agent, so no environment-only rule can separate that
// launch from an agent's — the operator declares the actor instead, by putting
// HumanTerminalEnv on HIS OWN command line (never in ~/.bashrc: hermes is his
// shell's child and would inherit the declaration, which would then speak for
// every agent shell under hermes — see the guard's constant). Both halves are
// measured here, on the same inherited signature: without the declaration the
// launch is REFUSED (the agent's shape), with it the loop STARTS (the
// operator's shape).
//
// The Gopi split is the explicit override for a human who will not put the
// declaration on his command line; it is asserted separately so each mechanism
// is proven alone.
func TestAgentSpawnGuardAllowsOperatorTerminalInsideAgentSession(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	SandboxRepoContext(t) // repo-context sandbox: the worker child must not inherit this process's cwd (the live repo).
	bin := testutil.GopiBin(t)
	home := t.TempDir()
	// What the operator's terminal inherits from hermes/gopi: ONLY the session
	// ids. The guard's own knobs are stripped by guardTestEnv so a case sets
	// exactly one mechanism — otherwise a Gopi in this suite's parent
	// environment would silently make every case pass.
	env := guardTestEnv(t, home)
	signature := []string{
		config.AgentSessionEnv + "=6424c4c1-0adb-48e8-bf43-d6d0baae5e0e",
		config.HermesSessionIDEnv + "=20260912_040123_ec45a1",
	}

	// ① No declaration: the inherited signature alone ⇒ refused (the agent's
	//    shape — this is what keeps the guard honest). Each child here is a
	//    bounded one-shot fixture child (internal/testutil/runfixture.go).
	out, err := testutil.FixtureCommandEnv(bin, env(signature...), CliVerbWorker, workerVerbArgv)
	if code := exitCodeOf(t, err); code != config.AgentGuardRefusalExitCode {
		t.Fatalf("inherited signature without a declaration: exit=%d want %d\n%s",
			code, config.AgentGuardRefusalExitCode, out)
	}
	if !strings.Contains(string(out), config.HermesSessionIDEnv) {
		t.Errorf("refusal must name the inherited signal %s:\n%s", config.HermesSessionIDEnv, out)
	}

	// ② The operator's declaration (HumanTerminalEnv, set on his command line
	//    and inherited by the child exactly like the signature is) ⇒ proceed.
	out, err = testutil.FixtureCommandEnv(bin,
		env(append(append([]string{}, signature...), config.HumanTerminalEnv+"=1")...),
		CliVerbWorker, workerVerbArgv)
	if code := exitCodeOf(t, err); code != 0 {
		t.Fatalf("operator declaration inside an inheriting shell must run: exit=%d\n%s", code, out)
	}
	if !strings.Contains(string(out), "worker: loop start") {
		t.Errorf("declared operator launch did not start the loop:\n%s", out)
	}

	// ③ The explicit per-run override ⇒ allowed, and LOUD about it (Rule 15:
	//    a bypass must never be silent).
	out, err = testutil.FixtureCommandEnv(bin,
		env(append(append([]string{}, signature...), config.AgentSpawnGuardOverrideEnv+"=1")...),
		CliVerbWorker, workerVerbArgv)
	if code := exitCodeOf(t, err); code != 0 {
		t.Fatalf("explicit override must be honored: exit=%d\n%s", code, out)
	}
	if !strings.Contains(string(out), "guard bypassed") {
		t.Errorf("an override must warn on stderr:\n%s", out)
	}

	// ④ A declaration with no signature at all — the plain operator terminal
	//    of the acceptance criteria — is untouched, loop included.
	out, err = testutil.FixtureCommandEnv(bin, env(config.HumanTerminalEnv+"=1"),
		CliVerbWorker, workerVerbArgv)
	if code := exitCodeOf(t, err); code != 0 {
		t.Fatalf("plain declared operator terminal must run: exit=%d\n%s", code, out)
	}
	if !strings.Contains(string(out), "worker: loop start") {
		t.Errorf("plain operator terminal did not start the loop:\n%s", out)
	}

	// ⑤ The obvious bypass — "wrap it in a launcher script" — is closed by
	//    construction: the guard judges the SCRIPT process's environment, so a
	//    launcher started from an agent session refuses exactly like the bare
	//    verb. A shell script that merely delegates is therefore not a hole.
	script := filepath.Join(t.TempDir(), "launder.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec \"$1\" worker "+workerVerbArgv+"\n"), 0o755); err != nil {
		t.Fatalf("write launcher script: %v", err)
	}
	// The agent session's environment, inherited by the launcher.
	out, err = testutil.FixtureCommandEnv("/bin/sh", env(signature...), script, bin)
	if code := exitCodeOf(t, err); code != config.AgentGuardRefusalExitCode {
		t.Fatalf("a launcher script run from an agent session must be refused: exit=%d\n%s", code, out)
	}
}

// exitCodeOf converts a Command error into an exit code (0 when err is nil).
func exitCodeOf(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if ok := asExitError(err, &ee); ok {
		return ee.ExitCode()
	}
	t.Fatalf("process failed to start: %v", err)
	return -1
}

// asExitError reports whether err (or anything it wraps) is an *exec.ExitError.
//
// It unwraps on purpose: the bounded fixture runner (internal/testutil/runfixture.go)
// wraps the child's own error with %w — exactly so the cause survives (Rule 16) —
// which means a bare type assertion would report a perfectly normal non-zero exit
// as "process failed to start". errors.As is the whole point of wrapping.
func asExitError(err error, target **exec.ExitError) bool {
	return errors.As(err, target)
}

// runGopiIn runs the given binary with an explicit env/stdin and returns the
// combined output plus the exit code.
func runGopiIn(t *testing.T, bin string, env []string, stdin string, args ...string) (string, int) {
	t.Helper()
	return runGopiInDir(t, bin, env, stdin, "", args...)
}

// runGopiInDir is runGopiIn with an EXPLICIT working directory for the child: an
// empty dir means "inherit this process's cwd" (the rcsIsSpawnCall/rcsIsRunner
// rule treats exactly that empty literal as NOT a repo-context pin, so a caller
// that wants a specific directory here must say so). It exists for the cases that
// cannot take the process-wide repo-context pin because they must keep running
// from the package directory — the peak-guard CONTROL binary is one: that build
// refuses `task run`/`worker --once` before they can act, so it is run to prove
// the REFUSAL, and it has to be able to build nothing from a sandbox cwd.
func runGopiInDir(t *testing.T, bin string, env []string, stdin string, dir string, args ...string) (string, int) {
	t.Helper()
	// NOT converted to the bounded fixture runner: this helper's contract
	// carries a per-child WORKING DIRECTORY (cmd.Dir) and STDIN, which
	// FixtureCommand/FixtureCommandEnv deliberately do not model (they are the
	// one-shot capture-only shape). Its callers — cli_tasks_worker_unknown_arg_test.go
	// and peak_guard_removal_test.go — depend on the pinned cwd.
	cmd := exec.Command(bin, familyAddressArgs(args)...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), exitCodeOf(t, err)
}

// stripAgentSignature removes the agent-spawn signature from an environment —
// the operator-shell shape. It is what makes the positive case a real
// measurement: the ONLY difference from the negative case is the signature.
func stripAgentSignature(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case config.AgentSessionEnv, config.HermesSessionIDEnv,
			config.AgentSessionAncestorEnv, config.HumanTerminalEnv,
			config.AgentSpawnGuardOverrideEnv, config.AgentSpawnGuardOffEnv:
			continue
		}
		out = append(out, kv)
	}
	return out
}
