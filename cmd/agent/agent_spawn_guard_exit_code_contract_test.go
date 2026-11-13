package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

// agent_spawn_guard_exit_code_contract_test.go — the DOCUMENTATION ↔ BEHAVIOUR
// contract for the worker verb's exit codes.
//
// THE DEFECT this file exists for (measured on the shipped binary, 2026-09):
// `gopi worker -h` documented {0,1} while the agent-spawn guard refused with 3
// — for `worker <task-id>`, `worker --once` and `worker --daemon` alike. The
// refusal is a deliberate outcome (the operator's ruling of 2026-09-12; see
// internal/config/agentguard.go), so the fix is not to renumber it: it is to
// DOCUMENT it on the page and to NAME it in the refusal itself.
//
// WHY THE PAGE IS STILL A PLAIN LITERAL. The paragraph could be rendered from
// the constants with a fmt template, but workerUsageHead is printed from more
// than one site, and a missed render would ship `%!d(int=3)` to a user's
// terminal — a worse defect than the one being fixed. So the page keeps its
// prose and these assertions do the binding instead: the code strings are
// built HERE, from config.AgentSpawnGuardExitCodeLabel +
// config.AgentGuardRefusalExitCode, and compared against the page's bytes. The
// constants the guard PRINTS are therefore the constants the page is MEASURED
// against — a page that drops the label or the code turns red.
//
// Rule 31: the subject is the shipped surface, so the behavioural half runs the
// REAL binary rather than unit-calling the predicate.

// agentSessionFixtureValue is the agent-session id the fixture exports. Its
// value mirrors the live 04:12 session's shape (agent_spawn_guard_test.go), so
// the fixture reproduces the real signature rather than a guess.
const agentSessionFixtureValue = "6424c4c1-0adb-48e8-bf43-d6d0baae5e0e"

// The exit-codes paragraph's bounds on the worker page: the heading it opens
// with, and the sentence group that closes it. Used to scope the "documented
// exactly once" assertion — a code documented twice, in two words, is how
// `{0,1}` grew a missing 3.
const (
	workerExitCodesParaHeading = "Exit codes:"
	workerExitCodesParaEnd     = "\n  Read-only faces"
)

// agentSessionEnvForGuard is the simulated agent-session environment: the
// signature a gopi agent session exports and everything it spawns inherits.
func agentSessionEnvForGuard(env func(...string) []string) []string {
	return env(
		config.AgentSessionEnv+"="+agentSessionFixtureValue,
		config.AgentSessionAncestorEnv+"=1", // an ancestor pid, never ours
	)
}

// workerRefusalExitCodeText renders the refusal code the way the guard prints
// it AND the way the page must carry it — one rendering, two readers.
func workerRefusalExitCodeText() string {
	return config.AgentSpawnGuardExitCodeLabel + strconv.Itoa(config.AgentGuardRefusalExitCode)
}

// workerUsageExitCodeParagraph slices the page's exit-codes paragraph.
func workerUsageExitCodeParagraph() (string, bool) {
	start := strings.Index(workerUsageHead, workerExitCodesParaHeading)
	if start < 0 {
		return "", false
	}
	rest := workerUsageHead[start:]
	if end := strings.Index(rest, workerExitCodesParaEnd); end >= 0 {
		return rest[:end], true
	}
	return rest, true
}

// TestGuardRefusalNamesTheWorkerLockFile is the drift guard for the one mirror
// this fix adds: internal/config cannot import cmd/agent, so
// config.AgentSpawnGuardLockName mirrors workerLockFilename — the same shape,
// and the same reason, as TestDaemonEnvLeakVarsMirrorTools beside it. A drift
// would make the refusal claim a lock file the worker never takes.
func TestGuardRefusalNamesTheWorkerLockFile(t *testing.T) {
	if config.AgentSpawnGuardLockName != workerLockFilename {
		t.Fatalf("config.AgentSpawnGuardLockName = %q, cmd/agent's workerLockFilename = %q — the refusal would name a lock file the worker never takes",
			config.AgentSpawnGuardLockName, workerLockFilename)
	}
}

// TestWorkerUsageHeadDocumentsEveryRefusalExitCode is the page half of the
// contract: the paragraph enumerates every code the verb can return for a
// REFUSED launch, bound to the constants the guard uses, and it names the
// anchor flag the refusal points the reader at.
func TestWorkerUsageHeadDocumentsEveryRefusalExitCode(t *testing.T) {
	para, ok := workerUsageExitCodeParagraph()
	if !ok {
		t.Fatalf("workerUsageHead has no %q paragraph:\n%s", workerExitCodesParaHeading, workerUsageHead)
	}
	for _, rendered := range []string{
		config.AgentSpawnGuardExitCodeLabel + strconv.Itoa(workerUnexpectedArgExitCode),
		workerRefusalExitCodeText(),
	} {
		if !strings.Contains(para, rendered) {
			t.Errorf("the exit-codes paragraph does not document %q — the refusal names that code, so the page must carry it too:\n%s",
				rendered, para)
		}
	}
	// The run's own report stays documented in the words the loop contract
	// tests already pin ("exit 0" / "exit 1").
	for _, want := range []string{"exits 0", "1 when it failed"} {
		if !strings.Contains(para, want) {
			t.Errorf("the exit-codes paragraph lost %q (the run's own report):\n%s", want, para)
		}
	}
	// The refusal's pointer (`gopi <verb> -h`) must name a flag this page
	// answers to.
	if !strings.Contains(workerUsageHead, config.AgentSpawnGuardHelpAnchor) {
		t.Errorf("workerUsageHead no longer mentions %q, the flag the refusal's pointer names:\n%s",
			config.AgentSpawnGuardHelpAnchor, workerUsageHead)
	}
}

// TestWorkerUsageHeadNamesEachRefusalExitCodeOnce is the anti-drift half: each
// refusal code is rendered EXACTLY once on the page, so a second spelling of
// one code cannot quietly appear beside the first.
func TestWorkerUsageHeadNamesEachRefusalExitCodeOnce(t *testing.T) {
	para, ok := workerUsageExitCodeParagraph()
	if !ok {
		t.Fatalf("workerUsageHead has no exit-codes paragraph:\n%s", workerUsageHead)
	}
	for _, rendered := range []string{
		config.AgentSpawnGuardExitCodeLabel + strconv.Itoa(workerUnexpectedArgExitCode),
		workerRefusalExitCodeText(),
	} {
		if got := strings.Count(para, rendered); got != 1 {
			t.Errorf("the exit-codes paragraph renders %q %d time(s), want exactly 1:\n%s", rendered, got, para)
		}
		if got := strings.Count(workerUsageHead, rendered); got != 1 {
			t.Errorf("workerUsageHead renders %q %d time(s), want exactly 1 (the paragraph is its one home):\n%s",
				rendered, got, workerUsageHead)
		}
	}
}

// TestWorkerRefusalExitCodeMatchesDocumentedPage is the measured half: the code
// the REAL binary returns from an agent session is the code the page documents,
// on the three faces the epic names (a named row, --once, --daemon) — with the
// refusal naming its own code, both operator escapes, and the lock it did NOT
// take.
func TestWorkerRefusalExitCodeMatchesDocumentedPage(t *testing.T) {
	bin := testutil.GopiBin(t)
	// repo-context sandbox (repo_context_sandbox_test.go): the child must never
	// inherit this process's cwd — the live repository — because a worker that
	// DID start would enumerate and free the developer's worktrees.
	SandboxRepoContext(t)
	home := t.TempDir()
	env := agentSessionEnvForGuard(guardTestEnv(t, home))
	lock := filepath.Join(home, config.ProfileDirName, workerLockFilename)
	para, ok := workerUsageExitCodeParagraph()
	if !ok {
		t.Fatalf("workerUsageHead has no exit-codes paragraph:\n%s", workerUsageHead)
	}
	if !strings.Contains(para, workerRefusalExitCodeText()) {
		t.Fatalf("the page does not document %q, so a refusal returning %d cannot agree with it:\n%s",
			workerRefusalExitCodeText(), config.AgentGuardRefusalExitCode, para)
	}

	for _, argv := range [][]string{
		{CliVerbWorker, "t-exit-contract-no-such-row"},
		{CliVerbWorker, workerVerbArgv},
		{CliVerbWorker, workerDaemonFlag},
	} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			out, err := testutil.FixtureCommandEnv(bin, env, argv...)
			code := exitCodeOf(t, err)
			if code != config.AgentGuardRefusalExitCode {
				t.Fatalf("`gopi %s` from an agent session: exit=%d, the page documents %d for the guard's refusal\noutput:\n%s",
					strings.Join(argv, " "), code, config.AgentGuardRefusalExitCode, out)
			}
			// The code that RETURNED is the code the PAGE carries — compared
			// through the same rendering, so the two cannot drift.
			if !strings.Contains(para, config.AgentSpawnGuardExitCodeLabel+strconv.Itoa(code)) {
				t.Errorf("the page documents no `%s%d` although that is what the refusal returned:\n%s",
					config.AgentSpawnGuardExitCodeLabel, code, para)
			}
			for _, want := range []string{
				config.AgentSpawnGuardExitCodeLabel + strconv.Itoa(code), // names its code
				config.HumanTerminalEnv,                                  // escape 1: the operator's own declaration
				config.AgentSpawnGuardOverrideEnv,                        // escape 2: the deliberate one-off
				workerLockFilename,                                       // says no lock was taken
			} {
				if !strings.Contains(string(out), want) {
					t.Errorf("the refusal must name %q:\n%s", want, out)
				}
			}
			if strings.Contains(string(out), "worker: loop start") {
				t.Errorf("the worker loop STARTED despite the refusal:\n%s", out)
			}
			if _, err := os.Stat(lock); err == nil {
				t.Errorf("the refusal created the worker lock %s — a refusal starts nothing and takes nothing", lock)
			}
		})
	}
}

// TestWorkerReadOnlyFacesExitZeroWithoutLock is the floor: the read-only and
// usage faces answer from an agent session too (rc 0, no refusal, no lock), and
// `-h` carries the code the refusal points the reader at — so the pointer lands
// on a page that documents it.
func TestWorkerReadOnlyFacesExitZeroWithoutLock(t *testing.T) {
	bin := testutil.GopiBin(t)
	// repo-context sandbox: same reason as the refusal test above.
	SandboxRepoContext(t)
	home := t.TempDir()
	env := agentSessionEnvForGuard(guardTestEnv(t, home))
	lock := filepath.Join(home, config.ProfileDirName, workerLockFilename)

	// The help faces answer WITH the page (so the refusal's pointer lands on a
	// page that documents the code); --status is a report, not the page, and is
	// held only to rc 0 and to claiming nothing.
	for _, tc := range []struct {
		argv     []string
		wantPage bool
	}{
		{[]string{CliVerbWorker, workerFlagHelp}, true},
		{[]string{CliVerbWorker, workerFlagHelpLong}, true},
		{[]string{CliVerbWorker, workerControlStatus}, false},
	} {
		argv := tc.argv
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			out, err := testutil.FixtureCommandEnv(bin, env, argv...)
			if code := exitCodeOf(t, err); code != 0 {
				t.Fatalf("`gopi %s` from an agent session must stay rc 0: exit=%d\n%s",
					strings.Join(argv, " "), code, out)
			}
			if strings.Contains(string(out), "refusing to start") {
				t.Errorf("`gopi %s` was refused — the guard sits after the help scan and never touches a read-only face:\n%s",
					strings.Join(argv, " "), out)
			}
			if _, err := os.Stat(lock); err == nil {
				t.Errorf("`gopi %s` created the worker lock %s — a read-only face claims nothing",
					strings.Join(argv, " "), lock)
			}
			if tc.wantPage && !strings.Contains(string(out), workerRefusalExitCodeText()) {
				t.Errorf("`gopi %s` does not document %q, the code the refusal returns:\n%s",
					strings.Join(argv, " "), workerRefusalExitCodeText(), out)
			}
		})
	}
}
