package main

// Shared CLI test helpers for the stale-read-path E2E suite.
//
// WHERE THIS FILE CAME FROM (Rule 43, 2026-10-26). These helpers lived in
// cli_tasks_sweep_zombies_test.go, a test file for the `sweep-zombies` verb.
// The operator deleted that mechanism (devdoc/zombie-sweep-removed.md), but the
// file's symbols were NOT all the mechanism's: a hermetic child env, a stale-row
// seeder, a suite-scratch binary resolver and two raw-ledger readers are used by
// tests whose subject survives — the stale READ path, the merged-config path, the
// config/env pairing tests. Deleting the file outright would have taken those
// callers' ground away, so the generic half was moved here FIRST and the
// mechanism's own tests were then deleted, in the same commit (Rule 43's ordering:
// move-and-delete together, never one after the other).
//
// THE NAMES WERE ALREADY WRONG, so they did not come across verbatim. Each one
// was spelled after the verb that owned its first caller — `sweepZombiesEnv`,
// `seedSweepZombieCLI`, `sweepZombiesRequireBin` — which would have left a
// deleted mechanism's vocabulary attached to a live shared helper, and the next
// reader would have had to discover from history that the name means nothing
// (Rule 43's rule that a filename states why a thing was CREATED, not what lives
// inside it). They are renamed for what they DO.
//
// Deliberately NOT renamed: zwPinSweepToSandbox (task_lifecycle_tick_test.go) —
// repo_context_sandbox_guard_test.go names that identifier as one of the pin's
// entry points, so renaming it silently subtracts every lifecycle test from that
// gate's verdict.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/taskverbs"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// Build policy for the E2E binary (Rule 1: named constants, never an inline
// path).
const (
	// e2eGopiBinDir names the suite-lifetime directory, INSIDE the suite's
	// scratch root, that holds the binary built from THIS tree. The build
	// product must land outside the worktree (Rule 22/30): a linked worktree has
	// no bin/ (gitignored), and writing one there would dirty the tree.
	e2eGopiBinDir = "e2e-gopi-bin"
	// e2eGopiBinFileName is the binary's name inside that directory.
	e2eGopiBinFileName = "gopi"
	// e2eGopiBuildOutputLimit caps the build output carried into a failure
	// message — enough for a compiler error, never the whole log.
	e2eGopiBuildOutputLimit = 4096
)

// e2eGopiBinOnce/e2eGopiBinPath/e2eGopiBinErr cache the ONE build per test
// binary (the reportSeamBinary precedent, cli_tasks_report_seam_test.go): every
// case that resolves the E2E binary reuses the same freshly built binary instead
// of building per test.
//
//nolint:gochecknoglobals // process-level build cache
var (
	e2eGopiBinOnce sync.Once
	e2eGopiBinPath string
	e2eGopiBinErr  error
)

// requireE2EGopiBin resolves the gopi binary for the CLI E2E tests and FAILS
// (never skips) when it cannot: testutil.GopiBin SKIPS the test when GOPI_BIN is
// unset and bin/gopi is absent, and a skipped E2E reads as a passing one in CI
// output — observed live 2026-09-12 while red-proving a fix: the acceptance test
// "passed" against the WRONG binary (the main repo's pre-fix bin/gopi) with no
// failure signal at all. These tests are a regression guard, so their
// environment must be loud.
//
// The binary is BUILT FROM THIS TREE on first use (CGO_ENABLED=0, package `.` —
// this test lives in package main, so `.` IS the binary), into a suite-lifetime
// directory under testutil.SuiteTempRoot(). That replaces the earlier
// `../../bin/gopi` fallback, which was unfixable by construction: bin/ is
// gitignored, only the main repo (where someone ran `make build`) ever has it, so
// in a fresh linked worktree the resolver had nothing to resolve and all three of
// its callers FAILED — and on a shared host they flipped with whichever tree's
// bin/gopi happened to exist. An explicitly set GOPI_BIN is still honoured (the
// operator's pinned binary wins, exactly as before).
//
// A build that cannot be produced is FATAL, not skipped, and names BOTH the `go`
// toolchain lookup and the compiler output: a silent skip here would report a
// green regression guard that never ran (Rule 15). The build output is kept OUT
// of the error whenever it is empty, so an ordinary missing-toolchain failure
// reads as its own cause and never as "error: \n".
//
// The resolved path is stamped into GOPI_BIN so a later testutil.RunGopi call
// cannot silently re-resolve to another tree's binary.
func requireE2EGopiBin(t *testing.T) string {
	t.Helper()
	bin := os.Getenv(testutil.BinaryPinEnv)
	if bin != "" {
		if _, err := os.Stat(bin); err != nil {
			t.Fatalf("%s=%s is set but not usable: %v", testutil.BinaryPinEnv, bin, err)
		}
	} else {
		bin = buildE2EGopiBinary(t)
	}
	abs, err := filepath.Abs(bin)
	if err != nil {
		t.Fatalf("resolve %s: %v", bin, err)
	}
	t.Setenv(testutil.BinaryPinEnv, abs)
	return abs
}

// buildE2EGopiBinary builds the binary from THIS tree once per test binary and
// returns its path, failing the test when the build cannot be produced.
func buildE2EGopiBinary(t *testing.T) string {
	t.Helper()
	e2eGopiBinOnce.Do(func() {
		// The toolchain is looked up FIRST so a missing `go` is reported as
		// itself — never as a confusing exit status from exec.Command.
		goBin, err := exec.LookPath("go")
		if err != nil {
			e2eGopiBinErr = fmt.Errorf("no `go` toolchain on PATH to build the E2E binary from THIS tree: %w", err)
			return
		}
		dir := filepath.Join(testutil.SuiteTempRoot(), e2eGopiBinDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			e2eGopiBinErr = fmt.Errorf("create the suite scratch bin dir %s: %w", dir, err)
			return
		}
		bin := filepath.Join(dir, e2eGopiBinFileName)
		cmd := exec.Command(goBin, "build", "-o", bin, ".")
		// CGO_ENABLED=0 for the same reason the Makefile builds with it: the
		// artifact is a pure-Go static binary, and a CGO-disabled build is what
		// production ships (reuse the existing target, not a new one).
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			if trimmed := trimBuildOutput(string(out)); trimmed != "" {
				e2eGopiBinErr = fmt.Errorf("go build -o %s . in %s: %w\n%s", bin, dir, err, trimmed)
				return
			}
			e2eGopiBinErr = fmt.Errorf("go build -o %s . in %s: %w (no compiler output)", bin, dir, err)
			return
		}
		// A `go build` of a NON-main package writes a non-executable archive;
		// stamping that path would make every RunGopi below fail with "permission
		// denied" — a loud but misleading failure deep inside the E2E. Refuse it
		// here, where the cause is obvious (Rule 15).
		fi, err := os.Stat(bin)
		if err != nil {
			e2eGopiBinErr = fmt.Errorf("built %s is missing after a successful build: %w", bin, err)
			return
		}
		if fi.Mode()&0o111 == 0 {
			e2eGopiBinErr = fmt.Errorf("built %s is not executable (mode %v) — is this test still in package main?", bin, fi.Mode())
			return
		}
		e2eGopiBinPath = bin
	})
	if e2eGopiBinErr != nil {
		t.Fatalf("the CLI E2E needs a gopi binary built from THIS tree, and the build failed: %v "+
			"(set GOPI_BIN to a prebuilt binary to bypass the build; a skip here would report a green "+
			"regression guard that never ran)", e2eGopiBinErr)
	}
	return e2eGopiBinPath
}

// trimBuildOutput caps a build log for a failure message, marking the cut.
func trimBuildOutput(s string) string {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) > e2eGopiBuildOutputLimit {
		return trimmed[:e2eGopiBuildOutputLimit] + " …(truncated)"
	}
	return trimmed
}

// TestE2EGopiBinaryIsBuiltInTheSuiteScratchRoot is the regression guard for the
// defect the resolver above was fixed for.
//
// The defect: the resolver fell back to `filepath.Join("..", "..", "bin",
// "gopi")`. That path exists only in the repo where somebody ran `make build` —
// bin/ is gitignored, so a fresh linked worktree (Rule 6a, the standard
// development shape) has none, and all three callers FAILED by construction
// there. On a shared host the same resolver flipped between pass and fail with
// whichever tree's bin/gopi happened to exist, which is worse than a plain
// failure: the E2E then executes a binary that is NOT this branch's.
//
// The guard's premise is therefore "no ambient build product", and it is
// asserted on the RESOLVED PATH rather than on the caller's behaviour: the
// binary must lie inside testutil.SuiteTempRoot(), i.e. in a directory this
// suite created for the build. A revert to the tree-relative fallback fails here
// even in the MAIN repo (where `../../bin/gopi` does exist and would otherwise
// resolve happily) — the arm that a fresh-worktree-only run cannot provide. It
// also pins the shape the E2E needs: an executable file (a `go build` of a
// non-main package leaves a non-executable archive, which would fail much later
// and much less clearly).
func TestE2EGopiBinaryIsBuiltInTheSuiteScratchRoot(t *testing.T) {
	if ambient := os.Getenv(testutil.BinaryPinEnv); ambient != "" {
		// The resolver honours an operator-pinned GOPI_BIN by design, so the
		// scratch-root claim does not apply to it. Skipped with its reason
		// VISIBLE (never silently): unset GOPI_BIN to exercise the guard.
		t.Skipf("GOPI_BIN=%s is set in the ambient environment — the resolver honours it, so the "+
			"scratch-root guard does not apply to this run", ambient)
	}
	bin := requireE2EGopiBin(t)
	root, err := filepath.Abs(testutil.SuiteTempRoot())
	if err != nil {
		t.Fatalf("resolve the suite scratch root %s: %v", testutil.SuiteTempRoot(), err)
	}
	binAbs, err := filepath.Abs(bin)
	if err != nil {
		t.Fatalf("resolve the resolved binary %s: %v", bin, err)
	}
	if binAbs != root && !strings.HasPrefix(binAbs, root+string(filepath.Separator)) {
		t.Fatalf("the E2E binary resolved to %s, OUTSIDE the suite scratch root %s: the resolver "+
			"is depending on an ambient build product again. bin/ is gitignored, so a tree-relative "+
			"fallback fails in every fresh linked worktree (and silently executes ANOTHER tree's binary "+
			"where one exists) — the resolver must build what it needs into the scratch root.",
			binAbs, root)
	}
	fi, err := os.Stat(binAbs)
	if err != nil {
		t.Fatalf("the resolved binary %s does not exist: the resolver claimed a path it did not produce", binAbs)
	}
	// The resolution IS the evidence, so print it: a run log must show the build
	// landing in the suite scratch root, not merely a green bit.
	t.Logf("resolved the E2E binary at %s (mode %v, %d bytes) inside the suite scratch root %s",
		binAbs, fi.Mode(), fi.Size(), root)
	if fi.Mode()&0o111 == 0 {
		t.Fatalf("the resolved binary %s is not executable (mode %v): every testutil.RunGopi call below "+
			"would fail with a permission error unrelated to the branch under test", binAbs, fi.Mode())
	}
}

// sandboxProfileDirPerm is the mode this suite's sandbox profile roots and
// ledgers are created with — the same 0o755 every other fixture in this package
// uses for a test directory.
const sandboxProfileDirPerm = 0o755

// sandboxProfileLedger pins a hermetic sandbox whose LEDGER IS A FILE OF ITS OWN
// PROFILE, and returns the home and the ledger path.
//
// THE DEFECT IT CLOSES (measured 2026-11-04, task 01a1a69f). deliverTaskNotify
// suppresses the events of a ledger that is not its profile's own —
// taskNotifyOriginLedger compares memoryDBPath() with defaultMemoryDBPath(),
// i.e. "<GOPI_HOME>/.gopi/memory.db". A fixture that set GOPI_HOME=<dir> and
// GOPI_MEMORY_DB=<dir>/memory.db therefore made every event it fired FOREIGN:
// recorded, reported on stderr, never broadcast. Six notify/stale tests in this
// package then failed asserting an alarm their own code had fired, with the
// symptom "open <tmp>/notify.log: no such file or directory" — the log was never
// written because delivery was withheld.
//
// The fix is not a second directory. It is THIS directory, spelled through the
// same joins the production resolvers use (config.ProfileDirIn /
// config.ProfilePathIn, Rule 14), so the creator and the reader cannot drift.
// A fixture that wants its events broadcast must isolate the PROFILE; isolating
// only the ledger is exactly the foreign shape the gate refuses.
func sandboxProfileLedger(t *testing.T) (home, ledger string) {
	t.Helper()
	home = t.TempDir()
	ledger = config.ProfilePathIn(home, config.MemoryDBFileName)
	if err := os.MkdirAll(config.ProfileDirIn(home), sandboxProfileDirPerm); err != nil {
		t.Fatalf("create the sandbox profile root %s: %v", config.ProfileDirIn(home), err)
	}
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv(config.MemoryDBEnv, ledger)
	// The legacy spelling is the SAME override under an older name: left ambient
	// it smuggles a different database back in through the fallback chain
	// (ledgerpin.go records why "" and not Unsetenv).
	t.Setenv(config.MemoryDBEnvLegacy, "")
	return home, ledger
}

// staleCLIEnv builds a hermetic env over the GIVEN sandbox home: the same
// GOPI_HOME + GOPI_MEMORY_DB the in-process seed wrote, notify silenced except a
// temp log when wanted. One home for seed and child — a split (seed in t.Setenv,
// child on another path) would have the child scan an EMPTY ledger and the
// assertions fail confusingly. The child env must carry PATH (a notify cmd like
// `cat >> file` is a shell-less exec without PATH, which cannot find cat) plus
// HOME for the config layer.
//
// The ledger is spelled INSIDE the home's profile (config.ProfilePathIn), not
// beside it: the child resolves its notify target from the PROFILE ROOT, so a
// ledger that is not that profile's own has every event it fires suppressed —
// see sandboxProfileLedger for the measurement.
func staleCLIEnv(t *testing.T, home, notifyLog string) []string {
	t.Helper()
	env := []string{
		"GOPI_HOME=" + home,
		"GOPI_MEMORY_DB=" + config.ProfilePathIn(home, config.MemoryDBFileName),
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GOPI_COMPUTE_NODES=",
		"GOPI_TASK_NOTIFY_ASYNC=0",
		"GOPI_TASK_NOTIFY_WEBHOOK=",
	}
	if notifyLog != "" {
		env = append(env,
			"GOPI_TASK_NOTIFY_CMD=cat >> "+notifyLog,
			"GOPI_TASK_NOTIFY_EVENTS=stale,claimed,in_progress,completed,failed")
	} else {
		env = append(env, "GOPI_TASK_NOTIFY_CMD=", "GOPI_TASK_NOTIFY_EVENTS=stale")
	}
	// The child must not inherit a stray GOPI_SETTINGS pointing at the real
	// /home/user/.gopi/settings.json (which carries compute_nodes / notify / model).
	env = append(env, "GOPI_SETTINGS="+filepath.Join(home, "settings.json"))
	return env
}

// seedStaleRowCLI submits + backdates a stale row in the sandbox ledger.
// liveToolcall=true keeps the heartbeat RECENT, which is the "live row" shape: it
// is what proves a stale verdict keys on the heartbeat and not on the claim.
func seedStaleRowCLI(t *testing.T, taskID, claimedBy string, old time.Time, liveToolcall bool) {
	t.Helper()
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "stale e2e " + taskID,
		Payload: "e2e fixture",
		TaskID:  taskID,
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed submit %s: %v", taskID, err)
	}
	db, err := openRawTasksDB(memoryDBPathForTest(t))
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer db.Close()
	if liveToolcall {
		recent := time.Now().Add(-1 * time.Minute)
		if _, err := db.Exec(`UPDATE tasks SET status = 'in_progress', claimed_by = ?,
			created_at = ?, updated_at = ?, last_toolcall_at = ? WHERE task_id = ?`,
			claimedBy, old.Unix(), old.Unix(), recent.Unix(), taskID); err != nil {
			t.Fatalf("backdate: %v", err)
		}
		return
	}
	if _, err := db.Exec(`UPDATE tasks SET status = 'in_progress', claimed_by = ?,
		created_at = ?, updated_at = ? WHERE task_id = ?`,
		claimedBy, old.Unix(), old.Unix(), taskID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
}

// taskUpdatedAtRaw reads one row's updated_at straight from the sandbox ledger
// (the read path's bump observable).
func taskUpdatedAtRaw(t *testing.T, taskID string) float64 {
	t.Helper()
	db, err := openRawTasksDB(memoryDBPathForTest(t))
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer db.Close()
	var updated float64
	if err := db.QueryRow(`SELECT updated_at FROM tasks WHERE task_id = ?`, taskID).Scan(&updated); err != nil {
		t.Fatalf("read updated_at %s: %v", taskID, err)
	}
	return updated
}

// memoryDBPathForTest mirrors the ledger path the tools layer resolves
// (GOPI_MEMORY_DB is set by the test env helper before seeding).
func memoryDBPathForTest(t *testing.T) string {
	t.Helper()
	if p := os.Getenv(config.MemoryDBEnv); p != "" {
		return p
	}
	// The fallback must land where the TOOLS LAYER would land, not on a fresh
	// directory of its own: memoryDBPath resolves "<gopi data root>/memory.db",
	// so a bare t.TempDir() here read a database no writer had ever opened and a
	// test comparing rows saw an empty ledger instead of the fixture's rows.
	gd, err := config.GopiDir()
	if err != nil {
		t.Fatalf("resolving the gopi data root for the ledger fallback: %v", err)
	}
	return filepath.Join(gd, config.MemoryDBFileName)
}

// TestSandboxProfileLedgerIsBroadcastable pins the PROPERTY the fixtures rely
// on, at the helper itself — not at its nine call sites.
//
// THE DEFECT IT GUARDS (measured 2026-11-04). deliverTaskNotify suppresses the
// events of a ledger that is not its profile's own: taskNotifyOriginLedger
// compares memoryDBPath() with "<GOPI_HOME>/.gopi/memory.db". A fixture that
// pinned GOPI_HOME=<dir> with GOPI_MEMORY_DB=<dir>/memory.db made every event it
// fired FOREIGN — recorded and reported on stderr, never broadcast — so the six
// notify/stale tests plus the worktree-orphan E2E failed on the ABSENCE of the
// log they had just asked the code to write ("open <tmp>/notify.log: no such
// file or directory").
//
// WHY A GUARD AT THE HELPER AND NOT ONLY AT THE ASSERTIONS. The nine call sites
// are the SYMPTOM; the helper is the cause. Re-spelling it back to the sibling
// shape turns all nine red at once — which is exactly what the red proof for
// the fix did — but nothing in this package says so in one place, and the next
// fixture author who "simplifies" the two pins apart gets no warning until
// eight unrelated tests fall over. This test names the relation.
//
// It fires a REAL event through the production producer (NotifySyntheticEvent)
// and requires it to reach the configured command, so it cannot pass on a
// spelling that merely LOOKS right: the gate is the authority on what a
// broadcastable profile is, and the gate runs.
func TestSandboxProfileLedgerIsBroadcastable(t *testing.T) {
	home, ledger := sandboxProfileLedger(t)

	// (1) THE RELATION, stated the way taskNotifyDelivers reads it: the ledger
	// this process would open must BE the profile's own file.
	if got, want := filepath.Clean(tools.MemoryDBPath()), filepath.Clean(ledger); got != want {
		t.Fatalf("the ledger this process resolves is %q but the sandbox created %q — the fixture is not "+
			"driving the ledger it thinks it is", got, want)
	}
	gd, err := config.GopiDir()
	if err != nil {
		t.Fatalf("resolving the sandbox profile root: %v", err)
	}
	if got, want := filepath.Clean(ledger), filepath.Clean(config.ProfilePathIn(filepath.Dir(gd), config.MemoryDBFileName)); got != want {
		t.Fatalf("the sandbox ledger is %q but its own profile's ledger is %q — the two pins are not the same "+
			"sandbox, so taskNotifyDelivers reads every event this fixture fires as coming from a FOREIGN "+
			"ledger and SUPPRESSES it (its events belong to that ledger's own sink). A notify test then fails "+
			"on the ABSENCE of the log it just asked the code to write", got, want)
	}

	// (2) THE BEHAVIOUR, through the production producer: a real synthetic alarm
	// must land in the configured command's log. A helper that satisfied (1) by
	// coincidence but left a stale gate input in the environment would fail here.
	notifyLog := filepath.Join(home, "notify.log")
	t.Setenv(config.NotifyCmdEnv, "cat >> "+notifyLog)
	t.Setenv(config.NotifyEventsEnv, tools.TaskHeartbeatNotifyEvent)
	t.Setenv(config.NotifyAsyncEnv, "")
	t.Setenv(config.NotifyWebhookEnv, "")

	tools.NotifySyntheticEvent(tools.TaskHeartbeatNotifyEvent, "sess-guard-profile-ledger", "test-model",
		"the sandbox profile's own ledger must be broadcastable")

	body, err := os.ReadFile(notifyLog)
	if err != nil {
		t.Fatalf("the event this fixture fired never reached the operator channel (Rule 15): %v — "+
			"NOT delivered means the ledger is FOREIGN to the profile, so the fix is in sandboxProfileLedger "+
			"(spell the ledger through config.ProfilePathIn), never in an assertion", err)
	}
	if !strings.Contains(string(body), tools.TaskHeartbeatNotifyEvent) {
		t.Fatalf("the delivered payload does not carry the %q event: %s", tools.TaskHeartbeatNotifyEvent, body)
	}
}

// taskListRenderSandbox builds the hermetic ledger + notify fixtures the
// `gopi task list` render tests share: a sandbox GOPI_HOME/GOPI_MEMORY_DB (so
// the operator's live ledger and sink are NEVER touched — memory #2081), and a
// notify cmd appending to a temp log.
//
// RENAMED 2026-11-05 (was taskListSweepSandbox, Rule 43): the stale sweep this
// helper was written beside is deleted, and the three sweep tests with it — but
// two SURVIVING render tests (the ledger-hint read-out and the zero-tree-walk
// render) still needed the sandbox, so the fixture is MOVED here rather than
// deleted with the mechanism that happened to host it. The name now says what
// the caller gets, not which mechanism once lived next door. It seeds no row:
// the zombie seeding belonged to the sweep tests and went with them.
func taskListRenderSandbox(t *testing.T) (home, notifyLog string) {
	t.Helper()
	if _, err := os.Hostname(); err != nil {
		t.Skipf("no hostname: %v", err)
	}
	home, _ = sandboxProfileLedger(t)
	t.Setenv("GOPI_SETTINGS", filepath.Join(home, "settings.json"))
	t.Setenv("GOPI_TASK_NOTIFY_ASYNC", "0")
	t.Setenv("GOPI_TASK_NOTIFY_WEBHOOK", "") // a dev shell may export a real webhook
	notifyLog = filepath.Join(t.TempDir(), "notify.log")
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat >> "+notifyLog)
	return home, notifyLog
}

// runTaskListCLI drives the REAL `gopi task list` verb — parseTasksArgs
// (parseTasksArgs) followed by the same renderer the dispatch switch calls —
// and returns the rendered ledger plus everything the verb printed to stdout.
// The property under test is about the VERB a user types, so the verb's own
// parse+render path is what must be exercised; a hand-rolled call to the
// renderer would prove nothing about the flag surface. The capture rides the
// suite's existing captureStdout helper.
//
// MOVED here 2026-11-05 (Rule 43) from the deleted stale-sweep test file, which
// was its only host and not its owner: the `gopi task list` render tests still
// invoke the verb through it.
func runTaskListCLI(t *testing.T, extra []string) (string, error) {
	t.Helper()
	opts, err := parseTasksArgs(taskverbs.List, extra)
	if err != nil {
		return "", err
	}
	var result string
	var fnErr error
	printed := captureStdout(t, func() { result, fnErr = runTaskList(opts) })
	return printed + result, fnErr
}
