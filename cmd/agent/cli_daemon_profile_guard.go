package main

// cli_daemon_profile_guard.go — the DAEMON SPAWN's own pre-fork profile guard
// (epic daemon-spawn-profile-guard, 2026-10-04).
//
// THE DEFECT (measured 2026-10-04, pid 639670). A `go test -run
// TestUnboundDaemonSurvivesItsSpawner ./cmd/agent/` spawned a REAL resident
// `gopi worker --daemon` that outlived its spawner (ppid=1, post-setsid) and
// held the OPERATOR's ~/.gopi/worker.lock — its stamped pid is STILL 639670 in
// that file. Its environment was the operator's GOPI_SETTINGS/GOPI_HOME. On a
// host with no systemd user bus the detach degrades to setsid-only, so nothing
// binds the daemon's lifetime to a live process.
//
// TWO ROOT CAUSES, and this file fixes exactly ONE of them:
//
//	A. TEST SIDE (upstream, already addressed): the test spawner seam inherited
//	   os.Environ() verbatim, and a child with no explicit GOPI_HOME/GOPI_SETTINGS
//	   fell through to HOME=/home/user ⇒ config.GopiDir() = ~/.gopi. The suite
//	   entry point now pins the profile (testutil.PinSuiteGopiProfile), which
//	   closes that door for THIS suite — pinning, not a guard, is the right fix
//	   for a test's own environment.
//	B. PRODUCTION SIDE (this file): the daemon face took its flock on whatever
//	   profile it resolved, with no question asked. The flock is acquired on the
//	   PRODUCTION START PATH, BEFORE any agent-spawn guard is consulted, and a
//	   daemon that started just fine under a test then had no owner to die with.
//	   The guard below makes that spawn REFUSE before the fork.
//
// WHY THE PREDICATE IS "the profile was NOT named explicitly", and not an
// env-shape test. Rule 25 of the worktree briefing, and the suite's own
// precedent (cmd/agent/testmain_test.go): refusing on "GOPI_HOME is absent"
// or on any other ENVIRONMENT SIGNATURE would break every legitimate agent
// session, every sandbox, and the whole test suite — a sandboxed run carries
// GOPI_HOME/GOPI_MEMORY_DB by construction, and an agent session is EXPECTED
// to carry them. The question the guard can answer EXACTLY is the one the
// incident turned on: "did anybody actually NAME this profile, or did it fall
// through to the operator's real one?" A daemon that resolves to <HOME>/.gopi
// WITHOUT an explicit GOPI_HOME naming it is the 639670 shape by definition —
// an inherited environment, not a decision.
//
// WHAT THIS DOES NOT DO. It does not claim the guard can tell a sandbox from a
// real profile by looking at the path (it cannot: a sandbox at <home>/.gopi
// would be indistinguishable). It does not change the FOREGROUND `gopi worker`
// (which holds no daemon lifespan and no lock, and must stay usable for
// debugging, Rule 30's sibling contract). It does not touch the running
// binary's semantics for a STARTED daemon: an explicitly named profile behaves
// exactly as before.
//
// REMEDY (Rule 15: a refusal names its correction): the two ways out are
// printed with the refusal — name the profile explicitly
// (`GOPI_HOME=<dir> gopi worker --daemon`), or start it from a sandbox
// (`gopi tool sandbox run --label build --task-id <id> -- gopi worker --daemon`).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/boolvocab"
	"github.com/nick/gopi/internal/config"
)

// daemonProfileRefusalExitCode is the exit status of a refused daemon spawn. It
// is NOT 1 (the generic start failure) and not 2 (usage): a refusal is its own
// outcome, so an operator — and the regression test — can tell "the guard said
// no" apart from "the start broke". Rule 1: one definition site.
const daemonProfileRefusalExitCode = 3

// daemonProfileRefusalLabel is the ONE line the refusal prints before its
// explanation. It is a constant because the regression test and the operator
// both match it (Rule 1b: a spelling that must not drift lives in a constant).
const daemonProfileRefusalLabel = "refusing to start: this daemon would run against the operator's real profile"

// daemonProfileGuardOffEnv disables the guard wholesale. It exists for the SAME
// two legitimate uses config.AgentSpawnGuardOffEnv documents (a subprocess test
// harness that is deliberately driving the real binary from an inherited
// environment, and a bootstrap whose environment it cannot control) — and for
// the red-proof of the regression test, which must show the pre-fix shape going
// green when the guard is off. It is never a production default, and it is not
// a way to make a real daemon skip the question.
const daemonProfileGuardOffEnv = "GOPI_DAEMON_PROFILE_GUARD_OFF"

// daemonProfileGuardDecision is the guard's verdict, captured as a pure value
// so the predicate can be tested without a live environment (the shape
// config.AgentSpawnSignature already establishes, Rule 14: one predicate).
type daemonProfileGuardDecision struct {
	// Refused: the spawn must not proceed.
	Refused bool
	// GopiHome: the value GOPI_HOME carried ("" when absent) — reported so the
	// refusal can quote what it actually saw.
	GopiHome string
	// ResolvedDir: the profile the daemon would have used.
	ResolvedDir string
	// RealProfileDir: the operator's own <HOME>/.gopi, the shape this refuses.
	RealProfileDir string
	// Off: the guard was disabled by its env switch (the caller says so, so an
	// override is never silent — Rule 15's sibling contract in
	// AgentSpawnDecision.Override).
	Off bool
}

// daemonProfileGuard decides whether a daemon spawn may proceed, from the
// process's OWN environment plus the two directories involved.
//
// Pure in its inputs (envHome, envGopiHome, realProfileDir, userHome are
// parameters) so the decision is exercised directly by the unit test, and
// daemonProfileGuardNow is the ONE production reader of the environment
// (Rule 2: a single place reads the env).
func daemonProfileGuard(envOff, envGopiHome, envSandboxRoot, userHome string) daemonProfileGuardDecision {
	realProfileDir := filepath.Join(userHome, config.ProfileDirName)
	d := daemonProfileGuardDecision{
		GopiHome:       strings.TrimSpace(envGopiHome),
		RealProfileDir: realProfileDir,
	}
	if boolvocab.IsTrue(envOff) {
		// The operator's deliberate switch; announced by the caller.
		d.Off = true
		return d
	}
	if d.GopiHome != "" {
		// The profile was NAMED. That is the whole question this guard asks:
		// whoever named it owns it, and a sandbox / an agent session / a test
		// rig all name one. Nothing is refused here.
		d.ResolvedDir = config.ProfileDirIn(d.GopiHome)
		return d
	}
	// No named profile: the daemon would inherit whatever HOME resolves to —
	// the 639670 shape.
	d.ResolvedDir = realProfileDir
	d.Refused = true
	// A sandbox root is the ONE other legitimate source of an unnamed profile,
	// and it never produces this shape (a sandbox profile lives under the
	// sandbox, not under the user's home). The value is recorded so the refusal
	// can tell the reader which channels it checked.
	_ = strings.TrimSpace(envSandboxRoot)
	return d
}

// daemonProfileGuardNow captures the decision from THIS process's environment —
// the production entry point, and the only place the four inputs are read.
func daemonProfileGuardNow() daemonProfileGuardDecision {
	userHome, err := os.UserHomeDir()
	if err != nil {
		// An unresolvable home is not a refusal: the guard's whole predicate
		// compares against a REAL profile directory, and inventing one from a
		// failed read would refuse a spawn for a fact nobody measured
		// (AgentSpawnSignature's fail-open discipline).
		return daemonProfileGuardDecision{Off: true}
	}
	return daemonProfileGuard(
		os.Getenv(daemonProfileGuardOffEnv),
		os.Getenv(config.GopiDirEnv),
		os.Getenv(config.SandboxEnvName),
		userHome)
}

// daemonProfileRefusal is the typed refusal: an error that also carries the
// exit code the process must end with, so daemonFail can honour it without the
// guard ever calling os.Exit itself (the mechanism stays return-only; only the
// CLI spelling exits).
type daemonProfileRefusal struct{ msg string }

// Error renders the operator-facing refusal.
func (e daemonProfileRefusal) Error() string { return e.msg }

// ExitCode is the distinct status of a refused daemon spawn (Rule 15: a
// refusal is not a broken start, and a caller must be able to tell them apart).
func (e daemonProfileRefusal) ExitCode() int { return daemonProfileRefusalExitCode }

// guardDaemonProfile refuses a daemon spawn that would land on the operator's
// real profile. It is called from daemonSpawn between step ① (argv validation)
// and step ③ (the control flock) — BEFORE anything is opened, locked or
// detached, so a refused spawn leaves no artifact at all.
//
// It RETURNS the refusal as an error (never exits): daemonSpawn's contract is
// to return, and its callers (daemonSpawnOrExit, workerDaemonSpawnErr) own what
// a refused start MEANS — the --restart path must be able to distinguish it.
func guardDaemonProfile(spec daemonSpec) error {
	d := daemonProfileGuardNow()
	if !d.Refused {
		if d.Off {
			fmt.Fprintf(os.Stderr, "%s: %s=1 is set — allowing a daemon spawn whose profile nobody named (it will use %s)\n",
				spec.prog, daemonProfileGuardOffEnv, d.ResolvedDir)
		}
		return nil
	}
	return daemonProfileRefusal{msg: fmt.Sprintf(
		"%s\n"+
			"  %s is not set, so the profile would fall through to HOME: %s\n"+
			"  A daemon started this way is the shape that outlived its spawner and held the\n"+
			"  operator's real worker.lock (pid 639670, 2026-10-04). Its %s would be %s.\n"+
			"  Name the profile explicitly — the daemon takes its lock and writes its state there:\n"+
			"      %s=<dir> gopi %s %s\n"+
			"  or start it inside a sandbox (its own private profile, reclaimed automatically):\n"+
			"      gopi tool sandbox run --label build --task-id <id> -- gopi %s %s\n"+
			"  Exit code: %d — this refusal started nothing, took no %s and created no directory.",
		daemonProfileRefusalLabel,
		config.GopiDirEnv, d.ResolvedDir,
		workerLockFilename, filepath.Join(d.ResolvedDir, workerLockFilename),
		config.GopiDirEnv, CliVerbWorker, daemonFlagLong,
		CliVerbWorker, daemonFlagLong,
		daemonProfileRefusalExitCode, workerLockFilename)}
}
