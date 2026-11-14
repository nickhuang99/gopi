package main

// worker unknown-argument refusal (Rule 15, devdoc/worker-unknown-flag.md).
//
// THE DEFECT: isWorkerSubcommand recognized only -h/--help, --once and
// --worker-log-root[=], and IGNORED every other word — so a launcher passing
// an option the deployed binary predates got a SILENTLY DEGRADED worker (the
// option dropped, the loop started, nothing said so). That is what let HP's
// stale binary swallow --worker-log-root: the worker ran with output
// attribution OFF and no line reported it.
//
// WHAT THESE TESTS PIN (all against the REAL binary — the property under test
// is "does the shipped CLI refuse", and a unit assertion on the predicate
// would not prove the refusal happens BEFORE the loop, which is where the
// silent degradation lived):
//
//	① an unrecognized word (dash-flag or positional) is REFUSED with the
//	   usage-class exit code (workerUnexpectedArgExitCode = 2, distinct from
//	   the agent-spawn guard's 3) and a message NAMING it;
//	② the loop did NOT start (no WorkerLoopStartLine banner) — the refusal
//	   precedes runTaskWorker;
//	③ nothing was CLAIMED: a queued pending row is still pending after the
//	   refusal (the refusal precedes guardVerbOrExit and the loop);
//	④ the recognized vocabulary still parses: -h/--help (exit 0), --once
//	   (loop starts);
//	⑤ THE RETIRED OPTION is refused WITH its replacement named
//	   (--worker-log-root → <gopi-dir>/sessions/<task-id>/output.log). The
//	   option is gone (epic child-stream-task-output: the destination belongs
//	   to the row, resolved at the executor-child spawn seam); a launcher
//	   still pointing workers at a root must hear why and what to do instead,
//	   not get a quietly different layout.
//	⑥ THE ROW IS POSITIONAL (epic worker-positional-taskid, 2026-09-18):
//	   `gopi worker <task-id>` runs that row, and the retired `--id <task-id>`
//	   spelling is refused WITH the replacement named — never answered with the
//	   stale-binary hint, because the flag was this binary's own a moment ago
//	   (see TestWorkerPositionalRowIsAcceptedAndTheRetiredFlagIsRefused).

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// workerArgSandbox arms a hermetic single-node worker environment and returns
// the sandbox's ledger path: an empty GOPI_HOME, an EXISTING (empty) ledger, no
// sync peers, no lifecycle notify target, and the child-failure escape so a loop
// that DOES start cannot reach a model or a peer. GOPI_TASK_CHILD_FAIL_TEST
// also terminates the loop quickly (a drained sandbox queue).
//
// The ledger is MATERIALISED here, not merely named (Rule 32): the callers' own
// probes read it back through tools.LedgerReadOnlyDSN — a `mode=ro` open — and a
// read-only open of a path that does not exist fails inside the driver with
// `unable to open database file: out of memory (14)`, a message that names
// neither the missing file nor the probe. That is how this fixture sat red in
// every subtest at 0.01s (before any work) instead of reporting its own defect.
// The worker this sandbox arms also has a ledger to CLAIM into, so "an empty
// ledger" means a real, schema-bearing one, not a file some later open happens
// to create.
//
// The path comes from the scaffold, which pins GOPI_MEMORY_DB to it and masks
// the legacy spelling (Rule 17), and is created through tools.EnsureLedger —
// the SAME provisioning face `gopi config setup` calls (cli_setup.go), so the file
// carries production's schema and production's own pragmas.
func workerArgSandbox(t *testing.T) (home, dbPath string) {
	t.Helper()
	home = t.TempDir()
	dbPath = testutil.NewTestDBPath(t)
	if err := tools.EnsureLedger(dbPath); err != nil {
		t.Fatalf("arm the sandbox ledger %s: %v", dbPath, err)
	}
	return home, dbPath
}

// workerArgEnv is the environment for one refusal/accepted probe: the parent
// environment with the agent-spawn signature and the guard knobs STRIPPED
// (the operator-shell shape — a refusal assertion must not be confusable with
// the guard's own refusal), plus the hermetic sandbox.
func workerArgEnv(t *testing.T, home, dbPath string) []string {
	t.Helper()
	env := stripAgentSignature(guardTestEnv(t, home)(
		"GOPI_MEMORY_DB="+dbPath,
		"GOPI_SETTINGS="+filepath.Join(home, "nonexistent.json"),
		"GOPI_COMPUTE_NODES=",
		"GOPI_SYNC_PEERS=",
		config.SkipClaimBroadcastEnv+"=1",
		taskChildFailTestEnv+"=1",
		workerIdleExitsEnv+"=1",
		workerIdleIntervalEnv+"=1",
	))
	return env
}

// seedPendingWorkerArgRow queues one real pending row through the REAL binary,
// so ③'s "nothing claimed" assertion reads a row that a running loop really
// would have claimed.
// seedPendingWorkerArgRowIn seeds one pending row through the REAL binary, from
// an EXPLICIT child working directory: `gopi task submit` refuses to run from a
// non-repository cwd ("is not a git work tree"), so a case whose repo-context pin
// is a fixture repository passes it here — the row then carries that fixture's
// repo as its deliverable, which is what the child's later `task show` reads.
func seedPendingWorkerArgRowIn(t *testing.T, bin string, env []string, taskID, dir string) {
	t.Helper()
	if out, code := runGopiInDir(t, bin, env, "", dir, CliVerbTask, "submit",
		"unknown-arg refusal probe", "--payload", "must never run",
		"--task-id", taskID); code != 0 {
		t.Fatalf("seeding task %s: exit=%d\n%s", taskID, code, out)
	}
}

// assertWorkerArgRowStillPending is ③: the ledger row is untouched by a
// refused launch (a launch that had reached the loop would have claimed it).
func assertWorkerArgRowStillPending(t *testing.T, bin string, env []string, taskID, out string) {
	t.Helper()
	show, code := runGopiInDir(t, bin, env, "", "", CliVerbTask, "show", taskID)
	if code != 0 {
		t.Fatalf("task show %s: exit=%d\n%s", taskID, code, show)
	}
	if !strings.Contains(show, tools.TaskStatusPending) {
		t.Errorf("the refused launch CLAIMED the queued row — want status %s:\n%s", tools.TaskStatusPending, show)
	}
	if strings.Contains(out, "worker: output attributed to task "+taskID) {
		t.Errorf("the refused launch executed the queued row:\n%s", out)
	}
}

// TestWorkerUnknownArgumentIsRefused is the acceptance criterion: every word
// the worker verb does not recognize is refused loudly, exits
// workerUnexpectedArgExitCode, names the offending word, never starts the
// loop and never claims a queued task.
func TestWorkerUnknownArgumentIsRefused(t *testing.T) {
	bin := buildGopiForWorkerHelp(t)
	for _, tc := range []struct {
		name  string
		argv  []string
		named string // the word the message must quote
	}{
		{
			name:  "unknown long flag",
			argv:  []string{CliVerbWorker, "--totally-bogus-flag-xyz"},
			named: "--totally-bogus-flag-xyz",
		},
		{
			// The interaction that matters: a RECOGNIZED flag does not
			// license ignoring the rest of the argv.
			name:  "unknown flag after a recognized one",
			argv:  []string{CliVerbWorker, workerFlagOnce, "--totally-bogus-flag-xyz"},
			named: "--totally-bogus-flag-xyz",
		},
		{
			name:  "unknown short flag",
			argv:  []string{CliVerbWorker, "-x"},
			named: "-x",
		},
		{
			// A newer gopi's `--flag=value` spelling of an option this binary
			// predates: the whole token is one unrecognized word.
			name:  "unknown flag with an inline value",
			argv:  []string{CliVerbWorker, "--output-root=/tmp/x"},
			named: "--output-root=/tmp/x",
		},
		{
			// ONE positional is the ROW (epic worker-positional-taskid), so the
			// refusal case is the SECOND one: a stray word must never be
			// dropped, which is what keeps one-row-per-invocation true.
			name:  "second positional",
			argv:  []string{CliVerbWorker, "t-first", "stray-positional"},
			named: "stray-positional",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, dbPath := workerArgSandbox(t)
			env := workerArgEnv(t, home, dbPath)
			const taskID = "0aeef03f-ef1d-5f26-b1ca-92a7f3317aa3"
			// The row is seeded FROM THE FIXTURE REPO and the probe runs from it:
			// `task submit` refuses a non-repository cwd, so the non-git sandbox
			// would make this test fail for an unrelated reason (measured, this
			// epic's re-run). The fixture repo has exactly one worktree — the main
			// checkout — so the child's repo context is this test's own throwaway
			// tree and no unowned registration is reachable. See
			// repo_context_sandbox_test.go.
			fixture := SandboxRepoContextInFixtureRepo(t)
			seedPendingWorkerArgRowIn(t, bin, env, taskID, fixture.Dir)

			out, code := runGopiInDir(t, bin, env, "", fixture.Dir, tc.argv...)
			// ① usage-class exit, distinct from the guard's 3.
			if code != workerUnexpectedArgExitCode {
				t.Fatalf("`gopi %s`: exit=%d want %d (usage-class refusal)\noutput:\n%s",
					strings.Join(tc.argv, " "), code, workerUnexpectedArgExitCode, out)
			}
			if code == config.AgentGuardRefusalExitCode {
				t.Errorf("a usage refusal must not reuse the agent-spawn guard's exit code %d", config.AgentGuardRefusalExitCode)
			}
			// ① the message names the offending word + the remedy.
			for _, want := range []string{
				"unexpected argument " + strconv.Quote(tc.named),
				"refusing to start the worker loop",
				workerFlagOnce,           // the accepted vocabulary is listed…
				workerDaemonFlag,         // …every accepted word of it…
				workerLogRootRetiredFlag, // …plus the retired one, so an old launcher learns why
				workerFlagHelp,
				"STALE", // …and a stale deployed binary is named as the cause.
			} {
				if !strings.Contains(out, want) {
					t.Errorf("refusal output missing %q:\n%s", want, out)
				}
			}
			// ② the loop never started.
			if strings.Contains(out, procname.WorkerLoopStartLine) {
				t.Errorf("the worker loop STARTED despite an unknown argument:\n%s", out)
			}
			// ③ nothing was claimed.
			assertWorkerArgRowStillPending(t, bin, env, taskID, out)
		})
	}
}

// TestWorkerAcceptedArgumentsParse is the other half: the refusal must not
// overshoot — the vocabulary the deployed launcher (scripts/gopi-worker-up.sh)
// and the operators actually pass still parses.
func TestWorkerAcceptedArgumentsParse(t *testing.T) {
	// The BINARY is resolved FIRST: the resolver builds with a relative
	// `go build .`, which needs this package's directory, while the repo-context
	// pin below chdirs out of it (pin first and the build fails → the test SKIPS,
	// i.e. a silently green no-op instead of a measurement).
	bin := buildGopiForWorkerHelp(t)
	// repo-context sandbox: the accepted half drives the real binary's worker
	// entry (`--once` STARTS the loop, which ends in the close-out producer pass),
	// so the child must not inherit this process's cwd — the live repository.
	// See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	root := t.TempDir()

	// --once: the loop must START — proven by the banner, not by "no refusal".
	for _, tc := range []struct {
		name string
		argv []string
	}{
		{"once", []string{CliVerbWorker, workerFlagOnce}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, dbPath := workerArgSandbox(t)
			env := workerArgEnv(t, home, dbPath)
			out, code := runGopiIn(t, bin, env, "", tc.argv...)
			if code != 0 {
				t.Fatalf("`gopi %s` must be accepted: exit=%d\n%s", strings.Join(tc.argv, " "), code, out)
			}
			if !strings.Contains(out, procname.WorkerLoopStartLine) {
				t.Fatalf("accepted argv did not start the loop:\n%s", out)
			}
			if strings.Contains(out, "unexpected argument") {
				t.Fatalf("accepted argv was refused:\n%s", out)
			}
		})
	}

	// -h / --help: the zero-side-effect usage query (P0-1) — exit 0, prints the
	// usage head, starts NOTHING and claims nothing.
	for _, flag := range []string{workerFlagHelp, workerFlagHelpLong} {
		t.Run("help "+flag, func(t *testing.T) {
			home, dbPath := workerArgSandbox(t)
			env := workerArgEnv(t, home, dbPath)
			const taskID = "c22c2d24-a27d-5d57-abd1-733d1e841444"
			// The seeding row needs a RESOLVABLE repository (`task submit` refuses
			// a non-repository cwd), so this subtest pins the repo context to its
			// own throwaway fixture repo — which is still NOT the live repository,
			// and holds exactly one worktree, the main checkout. The probe then
			// runs from that same directory. See repo_context_sandbox_test.go.
			fixture := SandboxRepoContextInFixtureRepo(t)
			seedPendingWorkerArgRowIn(t, bin, env, taskID, fixture.Dir)

			out, code := runGopiInDir(t, bin, env, "", fixture.Dir, CliVerbWorker, flag)
			if code != 0 {
				t.Fatalf("`gopi worker %s` must stay a zero-side-effect usage query: exit=%d\n%s", flag, code, out)
			}
			if !strings.Contains(out, cliUsageTitle(workerUsageHead)) {
				t.Errorf("`gopi worker %s` did not print the usage head:\n%s", flag, out)
			}
			if strings.Contains(out, procname.WorkerLoopStartLine) {
				t.Errorf("`gopi worker %s` started the loop (the P0-1 bug):\n%s", flag, out)
			}
			assertWorkerArgRowStillPending(t, bin, env, taskID, out)
		})
	}

	// THE RETIRED OPTION (epic child-stream-task-output): the output root is
	// gone, and a launcher still spelling it must be REFUSED with the reason
	// and the replacement — never silently ignored (Rule 15), which is the
	// whole point of this file. Both spellings, and both positions relative to
	// --once, are covered because the old grammar accepted all four.
	for _, tc := range []struct {
		name string
		argv []string
	}{
		{"retired flag, separate value", []string{CliVerbWorker, workerLogRootRetiredFlag, root, workerFlagOnce}},
		{"retired flag, inline value", []string{CliVerbWorker, workerLogRootRetiredFlag + workerFlagValueSep + root, workerFlagOnce}},
		{"retired flag last", []string{CliVerbWorker, workerFlagOnce, workerLogRootRetiredFlag, root}},
		{"retired flag, no value", []string{CliVerbWorker, workerLogRootRetiredFlag}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, dbPath := workerArgSandbox(t)
			env := workerArgEnv(t, home, dbPath)
			out, code := runGopiIn(t, bin, env, "", tc.argv...)
			if code != workerUnexpectedArgExitCode {
				t.Fatalf("`gopi %s` must be refused now: exit=%d want %d\n%s",
					strings.Join(tc.argv, " "), code, workerUnexpectedArgExitCode, out)
			}
			for _, want := range []string{
				// The refusal names the offending TOKEN — the flag alone for
				// the separate/last/none spellings, and
				// `--worker-log-root=<dir>` for the inline one, so the
				// assertion is on the flag NAME the message must carry, not on
				// one of the four surfaces it can appear with.
				"unexpected argument " + strconv.Quote(workerLogRootRetiredFlag),
				workerLogRootRetiredFlag,        // named…
				"sessions/<task-id>/output.log", // …with the replacement the operator needs
				"refusing to start the worker loop",
			} {
				// The inline spelling quotes the token WITH its value; the
				// remainder of the message (the retired-flag note, the
				// replacement) is identical, and the flag name itself is
				// asserted on the line below.
				if strings.HasPrefix(tc.name, "retired flag, inline") && want == "unexpected argument "+strconv.Quote(workerLogRootRetiredFlag) {
					continue
				}
				if !strings.Contains(out, want) {
					t.Errorf("retired-flag refusal missing %q:\n%s", want, out)
				}
			}
			if strings.Contains(out, procname.WorkerLoopStartLine) {
				t.Errorf("the loop STARTED on a retired flag:\n%s", out)
			}
		})
	}
}

// TestWorkerUnknownArgumentMessageNamesTheStaleBinaryCase pins the Rule 15
// phrasing that makes the refusal USEFUL to a launcher: it must name the word,
// list what this binary accepts, and point at the stale-binary cause — the
// failure mode the defect produced on HP (an option newer than the deployed
// binary).
func TestWorkerUnknownArgumentMessageNamesTheStaleBinaryCase(t *testing.T) {
	msg := workerUnexpectedArgMessage("--worker-log-root-typo")
	for _, want := range []string{
		strconv.Quote("--worker-log-root-typo"),
		workerFlagHelp,
		workerFlagHelpLong,
		workerFlagOnce,
		workerLogRootRetiredFlag, // the retired option is NAMED, with its replacement
		"sessions/<task-id>/output.log",
		"STALE",
		"refusing to start the worker loop",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("workerUnexpectedArgMessage missing %q:\n%s", want, msg)
		}
	}
	if workerUnexpectedArgExitCode == config.AgentGuardRefusalExitCode {
		t.Fatalf("the usage refusal must stay distinct from the agent-spawn guard's %d", config.AgentGuardRefusalExitCode)
	}
}

// TestWorkerPositionalRowIsAcceptedAndTheRetiredFlagIsRefused is ⑥: the row
// travels POSITIONALLY (`gopi worker <task-id>`, epic worker-positional-taskid,
// 2026-09-18) and the retired `--id <task-id>` spelling is refused with the
// replacement named.
//
// THE TWO HALVES ARE ONE PROPERTY, so they are asserted together: the point of
// the spelling change is that a task id needs no keyword — if the positional
// were not ACCEPTED (i.e. still reached the refusal) the change would have
// removed the only way to name a row. The ACCEPT half is proven by the
// DISPATCH PATH, not by a model call: `runMethodFor` renders the recorded
// run_cmd, which is `gopi worker <target>` — and the render is driven by argv
// parsing everywhere else in this file. (A foreground `gopi worker t-abc` with
// a seeded row would spawn a real child and reach a model; the spawn rigs that
// do that live in cli_tasks_test.go / the singleton guard.)
func TestWorkerPositionalRowIsAcceptedAndTheRetiredFlagIsRefused(t *testing.T) {
	bin := buildGopiForWorkerHelp(t)
	SandboxRepoContext(t)
	fixture := SandboxRepoContextInFixtureRepo(t)
	home, dbPath := workerArgSandbox(t)
	env := workerArgEnv(t, home, dbPath)

	const taskID = "91c6b770-ee9a-50c7-93bd-af1f4c4402c3"
	seedPendingWorkerArgRowIn(t, bin, env, taskID, fixture.Dir)

	// ① the RETIRED spelling: refused, the offending token named, the
	// replacement named, no stale-binary hint (the flag was this binary's own),
	// and nothing claimed.
	out, code := runGopiIn(t, bin, env, "", CliVerbWorker, workerIdRetiredFlag, taskID)
	if code != workerUnexpectedArgExitCode {
		t.Fatalf("`gopi worker %s %s` must be refused: exit=%d want %d\n%s",
			workerIdRetiredFlag, taskID, code, workerUnexpectedArgExitCode, out)
	}
	for _, want := range []string{
		"unexpected argument " + strconv.Quote(workerIdRetiredFlag),
		CliVerbWorker + " <task-id>", // the replacement, named
		"POSITIONAL",
		"refusing to start the worker loop",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("retired-flag refusal missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "STALE") {
		t.Errorf("the retired spelling was reported as a stale binary — it is this binary's own former flag:\n%s", out)
	}
	if strings.Contains(out, procname.WorkerLoopStartLine) {
		t.Errorf("the worker loop STARTED on the retired spelling:\n%s", out)
	}
	assertWorkerArgRowStillPending(t, bin, env, taskID, out)

	// ② the inline spelling is named whole (`--id=t-abc`), so the operator sees
	// the token they typed, not a truncated flag name.
	out, code = runGopiIn(t, bin, env, "", CliVerbWorker, workerIdRetiredFlag+workerFlagValueSep+taskID)
	if code != workerUnexpectedArgExitCode {
		t.Fatalf("inline retired spelling must be refused: exit=%d\n%s", code, out)
	}
	if !strings.Contains(out, strconv.Quote(workerIdRetiredFlag+workerFlagValueSep+taskID)) {
		t.Errorf("the inline spelling was not quoted whole:\n%s", out)
	}

	// ③ the ACCEPTED half: the recorded run_cmd renders the POSITIONAL form, so
	// the recipe a crashed row stores is the command a shell really runs.
	if _, cmd := runMethodFor(taskID, &tasksOptions{taskID: taskID}); !strings.Contains(cmd, CliVerbWorker+" "+taskID) {
		t.Errorf("the recorded run_cmd must spell the row positionally, got %q", cmd)
	}
}
