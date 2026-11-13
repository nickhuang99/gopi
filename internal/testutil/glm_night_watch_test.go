package testutil

// THE SINGLE-LAUNCHER GATE for scripts/glm-night-watch.sh — the GLM-window
// watcher that runs on the zbook and asks the HP peer for workers.
//
// WHY IT LIVES IN testutil: this package already owns the repo-level SCRIPT
// gates (scripts_gate_test.go: the /tmp ban, `bash -n`, the unbounded-loop ban;
// procname_gate_test.go: the launcher's worker COUNT), so this is the same kind
// of check in the same place, next to the launcher contract it pins.
//
// THE DEFECT THIS CLOSES. The pre-fix watcher was a SECOND, unsupervised
// launcher: it ssh'd a hand-written
//
//     setsid nohup ./bin/gopi worker > ~/.gopi/logs/worker-night-$i.log 2>&1 &
//
// once per worker, in a loop, and never looked at the exit status. That gave it
// its own log-dir contract (a per-worker timestamp-named file no task id can
// resolve — retired 2026-09-12), its own process accounting, and no single
// point of control — and it made a launch REFUSED by the agent-session guard
// (the refusal exit code below) log EXACTLY like a successful one. The operator
// read `started worker 1 pid 1234` while nothing had started.
//
// A version-SENSITIVE gate (the point of tests 2 and 3): each of them reads the
// watcher's body at the REVISION THE TEST RUNS AT, so `git stash` +
// `git show HEAD:scripts/glm-night-watch.sh` into place — or dropping in the
// pre-fix installed copy — turns this file RED. That is the acceptance
// criterion, and it is what makes these tests a regression guard rather than a
// description of the current file.
//
// THE FOUR TESTS
//
//  1. TestGlmNightWatchDelegatesToTheSingleLauncher — the SHAPE of the fix: the
//     one remote act is the single launcher, invoked with the worker COUNT; no
//     inline start anywhere; the launcher's file name equals the Go constant
//     for scripts/gopi-worker-up.sh (the single launcher contract, spelled once
//     there and once on the shell side).
//  2. TestGlmNightWatchEnvKnobsAreDefined — Rule 1b/13/14: every env knob the
//     script reads is DECLARED with this file's constant name as its env name,
//     and the knobs this gate governs are asserted equal to their Go values (a
//     rename on either side reddens the build).
//  3. TestGlmNightWatchRefusedLaunchIsLoudAndNonZero — the silent-failure half,
//     driven END TO END through the script's own seams with a stub transport:
//     a refusal (exit 3) is logged with a FAILED line that NAMES the refusal
//     and makes the script's exit non-zero; a zero-status launch is NOT logged
//     as a failure; no inline start happens in either run.
//  4. TestGlmNightWatchOperatorPathStartsWorkersUnattended — the "do not break
//     the operator" half, with the REAL launcher in a sandbox: a launch from a
//     plain shell with an empty environment (i.e. exactly what an ssh session
//     carries) still starts N workers. In-process orchestration only, so this
//     gate can never sit unscheduled in the worker queue behind the very
//     workers it replaces.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/heavytest"
	"github.com/nick/gopi/internal/procname"
)

const (
	// glmWatchScriptName is the watcher under test — the file name the script
	// itself derives from $0.
	glmWatchScriptName = "glm-night-watch.sh"
	// glmWatchScriptRel is the repo-root-relative path of the watcher (the
	// source of truth; the installed copy under <gopi-dir> is written by its
	// --install seam and must stay byte-identical to this one).
	glmWatchScriptRel = scriptsDirName + "/" + glmWatchScriptName
	// glmWatchDryRunFlag is the verification seam: it prints the delegation and
	// exits 0 without launching anything and without waiting for the window.
	glmWatchDryRunFlag = "--dry-run"
	// glmWatchInstallFlag is the operator seam that writes the installed copy
	// under <gopi-dir>.
	glmWatchInstallFlag = "--install"

	// --- env knobs (the script's defaults ARE the shell-side definition site;
	//     the Go side owns the name, so both halves are spelled once) ---
	glmWatchPeerEnv        = "GOPI_GLM_PEER"
	glmWatchLauncherEnv    = "GOPI_GLM_LAUNCHER"
	glmWatchSSHEnv         = "GOPI_GLM_SSH"
	glmWatchRefusalEnv     = "GOPI_GLM_REFUSAL_EXIT_CODE"
	glmWatchTargetEnv      = "GOPI_GLM_TARGET_HHMM"
	glmWatchWaitStepEnv    = "GOPI_GLM_WAIT_STEP_SEC"
	glmWatchWindowMaxEnv   = "GOPI_GLM_WINDOW_MAX_SEC"
	glmWatchLauncherValue  = "bin/" + workerUpScriptName
	glmWatchPeerValue      = "hp"
	glmWatchSSHValue       = "ssh"
	glmWatchRefusalExit    = "3"
	glmWatchTargetValue    = "2300"
	glmWatchWaitStepValue  = "60"
	glmWatchWindowMaxValue = "86400"

	// glmWatchWorkerNEnv is the one knob the watcher SHARES with the launcher:
	// how many workers the launcher should start. Sharing it is the point —
	// there is no second count in the system.
	glmWatchWorkerNEnv = "GOPI_WORKER_N"
	// glmWatchWorkerNValue is the script-side default worker count.
	glmWatchWorkerNValue = "2"

	// stubSSHExitEnv carries the stub transport's exit status in tests (0 =
	// the launcher accepted, anything else = a failed launch).
	stubSSHExitEnv = "STUB_SSH_EXIT"
	// glmWatchProbeMarkEnv points the sandbox probe at the file it records the
	// worker PIDs into, so the assertion reads a record rather than a race.
	glmWatchProbeMarkEnv = "GLM_PROBE_MARK"
	// testEpochMkdir is the PATH-shimmed epoch command the sandbox probe uses:
	// a cluster `date` that reports a target ten minutes away, so the script's
	// delegation branch is observable without a wall-clock negotiation.
	testEpochMkdir = "epoch"

	// --- sandbox + probe settings ---
	// probeWindowMaxSec collapses the window wait: with a target a few minutes
	// away and a bound of one second, the wait is abandoned and the launcher is
	// called NOW. It is the SCRIPT's own knob (not a patched code path), so the
	// probe exercises the shipped delegation logic.
	probeWindowMaxSec = "1"
	// probeWindowOffsetMin is how far ahead the shimmed `date` puts the window.
	probeWindowOffsetMin = 10
	// probeConnectTimeout mirrors the -o ConnectTimeout=N the watcher passes
	// ssh; the Go transport applies the same bound, so a hung probe fails the
	// test instead of parking it.
	probeConnectTimeout = 15
	// probeWaitSLess is how long the probe is willing to wait for a worker's
	// output artifact to appear before it gives up (the happy path returns on
	// the first poll; nothing sleeps the full budget).
	probeWaitSLess = 10
	// probePollInterval / probePollBudget are the record-file poll: 100ms ×
	// probeWaitSLess × 10 — expressed from the constant above so the budget and
	// the stated wait can never disagree.
	probePollInterval = 100 * time.Millisecond
	probePollBudget   = probeWaitSLess * 10
	// workerStartupLogName is the launcher's ONE stable file for pre-claim
	// worker output (the loop banner), as internal/procname defines it — the
	// leaf constant package shell, Go and tests all read.
	workerStartupLogName = procname.WorkerStartupLogName

	// --- the reap budget (Rule 1a: named, never inline) ---
	//
	// probeReapPollInterval / probeReapPollBudget bound the reap of the
	// detached workers this probe starts: 100ms × 50 = 5s for SIGTERM to land
	// and another 5s after SIGKILL. A worker that ignores SIGTERM because it is
	// mid-round still cannot outlive the budget.
	probeReapPollInterval = 100 * time.Millisecond
	probeReapPollBudget   = 50

	// reapProcRoot is the procfs root the reap reads. procCommName is the
	// process-NAME entry; the OTHER two facts it needs (procCmdlineName,
	// procStatName) come from this package's single source of truth in
	// procident.go (Rule 14) — a second declaration of them here collides with
	// it at package scope (the merge caught exactly that).
	reapProcRoot = "/proc"
	procCommName = "comm"
)

// glmWatchKnobs maps a knob's shell-side constant (spelled here, asserted
// against the script's own `FOO="${ENV:-default}"` line) to its env name and
// the value the script must default to. A knob added to the script without a
// row here is reported — a new env knob is a new contract, not an oversight to
// ignore.
type glmWatchKnob struct {
	shellVar string
	value    string
	what     string
}

var glmWatchKnobs = map[string]glmWatchKnob{
	glmWatchPeerEnv:      {"PEER", glmWatchPeerValue, "the peer the launcher runs on"},
	glmWatchLauncherEnv:  {"LAUNCHER_NAME", glmWatchLauncherValue, "the peer-side launcher path (must be the single launcher)"},
	glmWatchSSHEnv:       {"SSH_BIN", glmWatchSSHValue, "the transport (the test seam)"},
	glmWatchRefusalEnv:   {"REFUSAL_EXIT_CODE", glmWatchRefusalExit, "the agent-session guard's refusal exit status, named once"},
	glmWatchTargetEnv:    {"TARGET_HHMM", glmWatchTargetValue, "the default window start (HHMM)"},
	glmWatchWaitStepEnv:  {"WAIT_STEP_SEC", glmWatchWaitStepValue, "the wait heartbeat cadence in seconds"},
	glmWatchWindowMaxEnv: {"WINDOW_MAX_SEC", glmWatchWindowMaxValue, "the hard bound on the window wait in seconds"},
	glmWatchWorkerNEnv:   {"N", glmWatchWorkerNValue, "the worker count handed to the launcher"},
}

// inlineStartPatterns are the shapes of a SECOND launcher: any of them means
// the watcher is starting workers itself instead of delegating. They are
// spelled as regexes so the check is about the COMMAND, not about one edit's
// whitespace.
var inlineStartPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(^|[^[:alnum:]_./-])` + `\.?/bin/` + regexp.QuoteMeta(procname.NameGopi) + `[[:space:]]+` + regexp.QuoteMeta(procname.VerbWorker) + `([^[:alnum:]_]|$)`),
	regexp.MustCompile(`(^|[^[:alnum:]_])` + regexp.QuoteMeta(procname.NameGopi) + `[[:space:]]+` + regexp.QuoteMeta(procname.VerbWorker) + `([^[:alnum:]_]|$)`),
	regexp.MustCompile(`(^|[^[:alnum:]_])` + procname.NameGopi + regexp.QuoteMeta(scriptsDirName) + `-` + regexp.QuoteMeta(procname.VerbWorker) + `-`),
}

// envDefaultRe is (re)stated here for the knob gate: the procname gate has the
// same regexp for the launcher; keeping this file's copy independent means a
// change to one gate cannot silently relax the other.
var glmEnvDefaultRe = regexp.MustCompile(`(?m)^([A-Z_]+)="\$\{(GOPI_[A-Z_]+):-([^}]*)\}"`)

// TestGlmNightWatchDelegatesToTheSingleLauncher is the SHAPE gate: the watcher's
// only remote act is the single launcher, called once with the worker count.
func TestGlmNightWatchDelegatesToTheSingleLauncher(t *testing.T) {
	body := readGlmWatchScript(t)
	code := stripShellComment(body)

	if !strings.Contains(code, "LAUNCHER_NAME") || !strings.Contains(code, glmWatchLauncherValue) {
		t.Errorf("%s does not name the single launcher (%q) — the watcher must DELEGATE to %s (%s), never start workers itself",
			glmWatchScriptRel, glmWatchLauncherValue, workerUpScriptRel, workerUpScriptName)
	}
	for _, re := range inlineStartPatterns {
		if m := re.FindString(code); m != "" {
			t.Errorf("%s starts workers ITSELF (%q) — that is the second, unsupervised launcher this gate exists to prevent: "+
				"the one remote act must be the single launcher %s (log path, argv shape, process accounting and the operator's "+
				"single point of control all live there)", glmWatchScriptRel, strings.TrimSpace(m), workerUpScriptRel)
		}
	}

	// The delegation is a single call carrying the COUNT. A loop around it is
	// the "loop of one-worker starts" the epic names.
	if strings.Contains(code, "for i in") || strings.Contains(code, "seq 1") {
		t.Errorf("%s loops over a per-worker index — the launcher takes the COUNT and owns the per-worker spawn loop "+
			"(one call, N workers), so a loop here is a second launch path", glmWatchScriptRel)
	}
	if !strings.Contains(code, `"$LAUNCHER_NAME $N"`) {
		t.Errorf("%s does not invoke the launcher with the worker count (want the single remote command \"$LAUNCHER_NAME $N\") — "+
			"the count must reach the launcher, not be expanded here", glmWatchScriptRel)
	}

	// The install seam this epic requires (the installed copy lives OUTSIDE the
	// repo, so the repo holds the source of truth and the seam that writes it).
	if !strings.Contains(code, "--install)") {
		t.Errorf("%s has no --install seam — the installed copy at <gopi-dir>/%s is outside the repo, so without "+
			"this seam the two copies drift apart silently", glmWatchScriptRel, glmWatchScriptName)
	}
}

// TestGlmNightWatchEnvKnobsAreDefined is the Rule 1b/13/14 gate: every knob the
// script READS is declared with this file's constant as its env name, and the
// knobs this gate governs default to exactly their Go-side value.
func TestGlmNightWatchEnvKnobsAreDefined(t *testing.T) {
	body := readGlmWatchScript(t)

	defined := map[string]string{} // env name -> value
	for _, m := range glmEnvDefaultRe.FindAllStringSubmatch(body, -1) {
		shellVar, envName, value := m[1], m[2], m[3]
		knob, known := glmWatchKnobs[envName]
		if !known {
			t.Errorf("%s declares %s (as shell variable %s) from an env knob this gate does not know — a new knob is a "+
				"new contract: add it to glmWatchKnobs with its shell variable name, its value and what it governs",
				glmWatchScriptRel, envName, shellVar)
			continue
		}
		if shellVar != knob.shellVar {
			t.Errorf("%s reads %s into %q, but the shell-side definition site is %q — one name per knob on each side, "+
				"or a rename leaves half the script reading nothing", glmWatchScriptRel, envName, shellVar, knob.shellVar)
		}
		if value != knob.value {
			t.Errorf("%s defaults %s to %q, this gate says %q (%s) — the two must agree, so a rename is a red build "+
				"rather than a silent drift", glmWatchScriptRel, envName, value, knob.value, knob.what)
		}
		defined[envName] = value
	}
	for envName, knob := range glmWatchKnobs {
		if _, ok := defined[envName]; !ok {
			t.Errorf("%s no longer declares %s (%s) — the knob either vanished (and %s was typed inline) or the "+
				"declaration was rewritten; both are the drift this gate catches", glmWatchScriptRel, envName, knob.what, knob.value)
		}
	}

	// The refusal status is the one VALUE this gate shares with a Go constant:
	// the agent-session guard's exit code. Pinning it here means the loud
	// message cannot silently name a different number than the guard returns.
	if got := defined[glmWatchRefusalEnv]; got != glmRefusalExitText {
		t.Errorf("%s declares the refusal exit code as %q; the agent-session guard's refusal status is %q — the message "+
			"that tells an operator WHY a launch failed must name the code the guard actually returns",
			glmWatchScriptRel, got, glmRefusalExitText)
	}
}

// glmRefusalExitText is the string form of the guard's refusal status. The
// guard's code itself is not imported (this package must not depend on the
// guard's implementation — the epic forbids touching it, and a test that reads
// it would couple the two), so the number is pinned ONCE here and compared to
// both the shell default and the sandboxed launch's exit status.
const glmRefusalExitText = "3"

// TestGlmNightWatchRefusedLaunchIsLoudAndNonZero is the silent-failure gate,
// driven end to end: the stub transport stands in for ssh, so the whole script
// runs (window bound collapsed by its own knob) with no peer, no worker and no
// task.
func TestGlmNightWatchRefusedLaunchIsLoudAndNonZero(t *testing.T) {
	root := repoRootForProcnameGate(t)
	script := filepath.Join(root, filepath.FromSlash(glmWatchScriptRel))
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("%s is missing: %v", glmWatchScriptRel, err)
	}

	t.Run("refused", func(t *testing.T) {
		env, logPath := glmWatchSandbox(t, glmRefusalExitText)
		res := runGlmWatch(t, script, env)
		if res.exit != 3 {
			t.Errorf("a REFUSED launch exits %d, want non-zero (3, the guard's refusal status) — a caller (cron, an "+
				"operator's shell) must be able to see that no worker was started\nstdout+log:\n%s", res.exit, res.combined)
		}
		if !strings.Contains(res.combined, "FAILED") {
			t.Errorf("a refused launch produced no FAILED line — the refusal is exactly the silent failure this gate "+
				"closes (the pre-fix watcher logged `started worker N pid …` for a launch that never happened)\n%s", res.combined)
		}
		if !strings.Contains(res.combined, glmRefusalExitText) {
			t.Errorf("the refusal line does not name the exit status (%s) — an operator reading the log must be able to "+
				"tell a refusal from a broken peer\n%s", glmRefusalExitText, res.combined)
		}
		// The log is the artifact a human reads after the fact: it must carry
		// the same line the command printed.
		if logged, err := os.ReadFile(logPath); err == nil {
			if !strings.Contains(string(logged), "FAILED") {
				t.Errorf("the log at %s carries no FAILED line — the failure was reported to a stdout nobody keeps", logPath)
			}
		} else {
			t.Errorf("read the watcher log %s: %v", logPath, err)
		}
		if strings.Contains(res.combined, inlineStartMarker()) {
			t.Errorf("the refused run still contains an inline worker start (%q) — a failed delegation must NEVER fall "+
				"back to starting workers outside the launcher\n%s", inlineStartMarker(), res.combined)
		}
	})

	t.Run("accepted", func(t *testing.T) {
		env, _ := glmWatchSandbox(t, "0")
		res := runGlmWatch(t, script, env)
		if res.exit != 0 {
			t.Errorf("an ACCEPTED launch exits %d, want 0\n%s", res.exit, res.combined)
		}
		if strings.Contains(res.combined, "FAILED") {
			t.Errorf("an accepted launch (stub exit 0) was reported as FAILED — the loud path must not cry wolf, or the "+
				"operator stops reading it\n%s", res.combined)
		}
		if !strings.Contains(res.combined, glmWatchLauncherValue) {
			t.Errorf("the accepted run's output does not name the launcher it delegated to (%q)\n%s", glmWatchLauncherValue, res.combined)
		}
	})

	t.Run("dry-run", func(t *testing.T) {
		env, _ := glmWatchSandbox(t, "0")
		res := runGlmWatch(t, script, env, glmWatchDryRunFlag)
		if res.exit != 0 {
			t.Fatalf("%s %s exits %d, want 0\n%s", glmWatchScriptRel, glmWatchDryRunFlag, res.exit, res.combined)
		}
		if !strings.Contains(res.combined, glmWatchLauncherValue) {
			t.Errorf("%s %s does not show the delegation line (the remote command naming %q) — the dry run is the "+
				"verification seam, and it must show WHAT would run\n%s", glmWatchScriptRel, glmWatchDryRunFlag, glmWatchLauncherValue, res.combined)
		}
		// Launching nothing is the contract: the stub records every invocation,
		// and a recorded one means the dry run launched.
		if recorded, err := os.ReadFile(env.stubRecord); err == nil && len(strings.TrimSpace(string(recorded))) > 0 {
			t.Errorf("%s %s LAUNCHED something (the transport recorded %q) — a dry run must launch nothing",
				glmWatchScriptRel, glmWatchDryRunFlag, strings.TrimSpace(string(recorded)))
		}
	})
}

// TestGlmNightWatchOperatorPathStartsWorkersUnattended is the "do not break the
// operator" gate, with the REAL launcher: a launch from a plain shell carrying
// an EMPTY environment — the closest thing to an ssh session this host can
// produce without a peer — must still start N workers.
//
// It is orchestrated in-process (no `gopi task` submission) on purpose: this
// gate must never sit unscheduled in the worker queue behind the very workers
// it replaces, or the regression guard disappears exactly when it is needed.
func TestGlmNightWatchOperatorPathStartsWorkersUnattended(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionHostCondition)
	root := repoRootForProcnameGate(t)
	launcher := filepath.Join(root, filepath.FromSlash(workerUpScriptRel))
	if _, err := os.Stat(launcher); err != nil {
		t.Fatalf("%s is missing: %v", workerUpScriptRel, err)
	}
	home := t.TempDir()
	shimDir := filepath.Join(home, "shim")
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		t.Fatalf("create the shim dir: %v", err)
	}
	writeEpochShim(t, filepath.Join(shimDir, testEpochMkdir))

	// The launcher the probe delegates to refuses to start anything without an
	// executable at its binary path, so that artifact is resolved BEFORE the
	// launch: from an operator pin, the checkout's own bin/, or a build of that
	// checkout. It is resolved ONCE for the whole test binary (see
	// opPathWorkerBin) — the acceptance gate builds with `go build ./...`, which
	// writes no binary anywhere, so a fresh worktree carries none.
	opPathWorkerBin(t, root)

	// The probe's launch is CONTAINED: the sandbox gets its own scratch HOME
	// (source scripts/lib/tmp-ban.sh — the /tmp ban reaches this probe too) and
	// its own output root, so the workers it starts are the only ones whose
	// artifacts land there. This test may never observe, let alone touch, a
	// worker started by anybody else.
	gopiDir := filepath.Join(home, ".gopi")
	if err := os.MkdirAll(gopiDir, 0o755); err != nil {
		t.Fatalf("create the sandbox gopi dir: %v", err)
	}
	scratchHome := filepath.Join(home, "scratch-home")
	scratchRoot := filepath.Join(scratchHome, "workspace", "gopi-test-scratch")
	if err := os.MkdirAll(scratchRoot, 0o755); err != nil {
		t.Fatalf("create the sandbox scratch root: %v", err)
	}
	// The scratch tree must be OFF the root filesystem, or the launcher's own
	// helper refuses (the /tmp ban is mechanical). t.TempDir() lands inside the
	// suite's pinned scratch root, which is on /dev/shm — verify it, and skip
	// rather than silently probing a path the launcher will refuse.
	helper := filepath.Join(root, scriptsDirName, filepath.FromSlash(scratchHelperShell))
	check := exec.Command("bash", "-c", ". \"$1\" && gopi_assert_off_root_fs \"$2\"", "bash", helper, scratchRoot)
	if out, err := check.CombinedOutput(); err != nil {
		t.Skipf("the sandbox scratch root %s is on / — the launcher refuses a scratch tree on the root filesystem, so a run here would test the refusal, not the launch\n%s", scratchRoot, out)
	}
	t.Logf("sandbox scratch root %s is off / (suite temp root %s)", scratchRoot, os.Getenv("TMPDIR"))

	// The probe stands in for the peer session: the scratch HOME plus ONLY what
	// the operator path needs (PATH, HOME, the launcher's own knobs). No
	// inherited agent variable reaches it, so a launch here is a launch from a
	// plain shell — which is the claim under test.
	probe := exec.Command("bash", "-c", operatorProbeBody, "probe", launcher, home)
	probe.Env = []string{
		"PATH=" + shimDir + ":" + launcherBinDir(launcher) + ":" + os.Getenv("PATH"),
		"HOME=" + scratchHome,
		// COLLAPSED window: the shimmed `date` puts the target minutes ahead, so
		// the wait is abandoned and the REAL delegation logic runs immediately.
		glmWatchWindowMaxEnv + "=" + probeWindowMaxSec,
		glmWatchLauncherEnv + "=" + launcher,
		// The launcher writes each worker's output under <gopi-dir>/sessions, so
		// pinning GOPI_HOME to the sandbox is what makes the containment real:
		// every artifact this test sees belongs to a worker IT started.
		"GOPI_HOME=" + home,
		// The launcher starts <repo>/bin/<name>; the sandbox HOME has no
		// checkout, so the repo under test is named explicitly. This is the
		// launcher's OWN knob (Rule 1b), not a patched code path.
		"GOPI_WORKER_REPO=" + root,
		glmWatchWorkerNEnv + "=" + fmt.Sprintf("%d", probeWorkerCount),
	}
	var outBuf strings.Builder
	probe.Stdout = &outBuf
	probe.Stderr = &outBuf
	if err := probe.Run(); err != nil {
		if body, rerr := os.ReadFile(filepath.Join(home, "probe.out")); rerr == nil {
			outBuf.WriteString("\n--- watcher output ---\n")
			outBuf.Write(body)
		}
		t.Fatalf("the operator-path probe failed: %v\n%s", err, outBuf.String())
	}

	// THE TRUTH SOURCE: the launcher's own startup log. A worker prints its
	// loop-start banner before it claims anything, so the COUNT of that banner
	// is the number of workers that really started — evidence that survives a
	// worker which finds no task to claim (the sandbox's queue is empty, so no
	// output.log is ever opened: an artifact-based check would report zero for
	// a perfectly good launch).
	//
	// It is the LAUNCHER's file (one stable path under the sandbox gopi dir),
	// read directly — no process table is scanned and no PID is touched.
	//
	// This is the hard lesson of the probe's first version: a process-table
	// scan matching an ARGV PATTERN finds every worker on the machine, and
	// "stop the ones you found by pattern" is the 04:12 incident class. A test
	// that cannot NAME the processes it owns must not search for them at all.
	startupLog := filepath.Join(gopiDir, "logs", workerStartupLogName)
	if err := waitForWorkerLoopStarts(startupLog, probeWorkerCount); err != nil {
		t.Logf("SANDBOX TREE (%s):\n%s", home, listTree(home))
		t.Fatalf("the operator path did not start %d worker(s): %v\n"+
			"a launch from a shell WITHOUT any agent signature must still start N workers (this is the half of the "+
			"contract that must NOT regress while the watcher stops being a second launcher)\nprobe output:\n%s",
			probeWorkerCount, err, outBuf.String())
	}

	// THE REAP — and why the earlier version of this comment was wrong to say
	// "nothing is killed by this test".
	//
	// The workers were started detached (setsid + nohup, the launcher's whole
	// point), so nothing downstream of the launcher owns them: the probe shell
	// exited, the launcher exited, and these processes would keep polling the
	// SANDBOX's queue forever — a sandbox whose state dir is deleted the moment
	// the test returns. They are then invisible to their owner and visible to
	// every OTHER profile's process-table scan, which is the defect of the epic
	// this file now closes: `gopi worker --status` showed three workers when the
	// operator had started one (measured 2026-09-13: two leaked fixtures, argv
	// `--worker-log-root /dev/shm/gopi-test-scratch/…`, sat "idle" beside the
	// operator's non-empty pending queue).
	//
	// They are reaped by IDENTITY, never by pattern: their argv carries the
	// sandbox root this test alone names, and their comm is the gopi name. A
	// process-table pattern match ("all gopi workers") is the 04:12 incident
	// class — it finds the machine's real fleet. The check after the reap is the
	// assertion: this test may not leave a process behind that other profiles
	// will report as theirs.
	reaped := reapProbeWorkers(t, gopiDir, opPathWorkerBin(t, root))
	if reaped != probeWorkerCount {
		t.Errorf("reaped %d of the %d worker(s) this probe started (profile %s) — a leaked detached worker "+
			"outlives the sandbox it belongs to and is then reported as ANOTHER profile's worker",
			reaped, probeWorkerCount, gopiDir)
	}
	t.Logf("operator path: %d worker(s) announced their loop in %s and were reaped", countWorkerLoopStarts(startupLog), startupLog)
}

// sandboxWorkerLogRoot is the profile's sessions tree — where a task's output
// log lands now that the destination is derived from the row
// (cmd/agent/cli_tasks_exec_output.go). Derived from the sandbox the probe env
// builds (config.SessionsDirName, never a re-typed "sessions"), so a lookup
// under any other profile can never match it.
func sandboxWorkerLogRoot(gopiDir string) string {
	return filepath.Join(gopiDir, config.SessionsDirName)
}

// reapProbeWorkers stops every detached gopi worker whose argv names logRoot as
// its --worker-log-root, and returns how many it stopped. It is the ONLY kill in
// this package, and it is bounded and identity-based:
//
//   - IDENTIFIED by the PROFILE the process belongs to — the gopi dir its own
//     environment resolves to (config.GopiDirFromEnviron over
//     /proc/<pid>/environ), which is this sandbox's alone. It used to be read
//     from argv (`--worker-log-root <sandbox>`, the launcher's old spelling);
//     the flag is retired (epic child-stream-task-output) and the environment
//     is the better identity anyway: it is what the process IS, not what the
//     operator typed. Never a name pattern and never "all gopi workers"
//     (Rule 24's sibling trap: a pattern matches the operator's fleet);
//   - DETACHED first (pid == session, the post-setsid signature) so a foreground
//     `gopi worker --status`-shaped process is never a candidate;
//   - killed with SIGTERM, then polled, then SIGKILL — a worker mid-round gets
//     its chance to finish the claim boundary, and a stuck one still cannot
//     outlive the budget.
func reapProbeWorkers(t *testing.T, gopiDir, bin string) int {
	t.Helper()
	// The binary check is not decoration: the identity is the PROFILE a worker's
	// environment names, and a worker of that profile is still only a candidate
	// if it runs the gopi build this test launched.
	if bin == "" {
		t.Fatalf("no binary to identify the probe's workers by (profile %s)", gopiDir)
	}
	ids := probeWorkerIdentities(gopiDir)
	for _, id := range ids {
		if err := probeReapDelivery(id, gopiDir, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			t.Errorf("SIGTERM pid %d (starttime %d): %v", id.Pid, id.StartTime, err)
		}
	}
	survivors := ids
	for i := 0; i < probeReapPollBudget && len(survivors) > 0; i++ {
		time.Sleep(probeReapPollInterval) //stylegate:allow-sleep bounded poll for the reaped workers to exit; budget probeReapPollBudget
		survivors = probeWorkerIdentities(gopiDir)
	}
	for _, id := range survivors {
		if err := probeReapDelivery(id, gopiDir, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			t.Errorf("SIGKILL pid %d (starttime %d): %v", id.Pid, id.StartTime, err)
		}
	}
	if len(survivors) > 0 {
		// SIGKILL is not waitable by a non-parent, so the poll is the proof: a pid
		// still wearing the sandbox argv after the budget is a process this test
		// failed to stop, and saying so beats a silent leak.
		for i := 0; i < probeReapPollBudget && len(probeWorkerIdentities(gopiDir)) > 0; i++ {
			time.Sleep(probeReapPollInterval) //stylegate:allow-sleep post-SIGKILL poll; budget probeReapPollBudget
		}
	}
	if left := probeWorkerIdentities(gopiDir); len(left) > 0 {
		t.Errorf("worker(s) %v survived the reap (SIGTERM + SIGKILL) — they poll the deleted sandbox forever and "+
			"every other profile's worker scan will count them", left)
	}
	return len(ids)
}

// probeReapDelivery is the ONLY place this file delivers a signal, and it
// addresses a PROCESS, never a number (Rule 42).
//
// THE DEFECT IT CLOSES (measured 2026-10-27). The reap below used to collect
// bare `[]int` out of a `comm == "gopi"` scan and hand each one to
// `syscall.Kill(pid, sig)`. That is a statement about a NUMBER: the kernel
// recycles pids, and between the scan and the signal the number can name a
// different process entirely — the suite's own slot in the pid space, a
// terminal, an operator console. The delivery would land, silently, on
// whatever now owned the number.
//
// `testutil.KillIfIdentical` is the tree's ONE guarded delivery (25 call sites,
// counted in CODE with `grep -rn "KillIfIdentical(" --include=*.go . | grep -v
// "func KillIfIdentical"`; a grep that also counts comment mentions reports
// more, so quote it with the filter) and it answers the incarnation half
// exactly: it re-reads pid+starttime immediately before the signal and REFUSES
// a reused pid by name; it also refuses a systemd user instance, whose SIGINT
// stops the operator's whole desktop session. It does NOT answer ownership —
// measured on this host 2026-10-27: KillIfIdentical(<a live gopi chat this
// prober never started>, 0) returns nil. So this seam asks BOTH questions, and
// it takes the IDENTITY the scan captured rather than the pid, so a caller
// cannot accidentally re-open the unguarded shape.
func probeReapDelivery(id ProcIdentity, gopiDir string, sig syscall.Signal) error {
	// THE OWNERSHIP HALF, RE-ASSERTED AT THE DELIVERY SEAM (Rule 42). The
	// incarnation test alone is NOT ownership: measured on this host 2026-10-27,
	// `KillIfIdentical(<a live `gopi chat` the prober did not start>, 0)` returns
	// nil — it agrees about any live process whose starttime matches its own
	// reading. What makes a candidate OURS is the sandbox profile its OWN
	// environment names, and the scan established that a moment ago; between the
	// scan and this line the process could have exited and its number been
	// reused, so both questions are asked HERE, at the seam, in the order the
	// guards must run:
	//
	//	① is it still the INCARNATION the scan captured? (has the number been
	//	   recycled, or was the forged starttime never real? — refusing here costs
	//	   no /proc environment read)
	//	② is it still running in the PROFILE this reap was asked to clean?
	//
	// Only then does anything get delivered. A candidate that fails either is
	// REFUSED by name — never silently skipped — because "I chose a process and
	// then it was not mine" is a fact the operator reading the leak report needs.
	//
	// A GONE CANDIDATE IS THE GOAL, NOT A FAILURE (and NOT a refusal either).
	// The pre-fix code got this for free: kill(2) on a dead number answers ESRCH,
	// which the caller ignores. KillIfIdentical answers `ErrProcessGone` instead,
	// which does NOT wrap ESRCH — so a candidate that exited between the poll and
	// this line would read as a delivery failure and turn a clean reap red. The
	// exit is what the reap ASKED for, so it is reported as success; the
	// distinction between "gone" and "not mine" is kept, because only the second
	// one is a finding.
	if now, err := ReadProcIdentity(id.Pid); err != nil {
		if errors.Is(err, ErrProcessGone) {
			return nil
		}
		return fmt.Errorf("read pid %d before signalling it: %w", id.Pid, err)
	} else if !now.SameIncarnation(id) {
		return fmt.Errorf("refuse to signal pid %d: it is no longer the incarnation the scan captured (now %s, was starttime %d) — a recycled number must never be signalled (Rule 42)",
			id.Pid, now, id.StartTime)
	}
	if !workerBelongsToProfile(strconv.Itoa(id.Pid), gopiDir) {
		return fmt.Errorf("refuse to signal pid %d: it no longer resolves to profile %s, so it is not this reap's to stop (Rule 42: never signal what you do not own)",
			id.Pid, gopiDir)
	}
	return KillIfIdentical(id, sig)
}

// probeWorkerIdentities lists the detached gopi workers that belong to the
// profile gopiDir names — the sandbox identity this test alone creates, read
// from each process's OWN environment (its GOPI_HOME / HOME, resolved by the
// same config.GopiDirFromEnviron the production scan uses) rather than from
// argv: the retired --worker-log-root was the only argv-carried identity this
// reap ever had, and the profile is the honest one.
//
// IT RETURNS IDENTITIES, NOT PIDS (Rule 42). The caller signals these, and a
// pid alone is a number the kernel recycles; pid+starttime+cmdline is the
// process. The identity is captured HERE, at the moment the candidate was
// measured, which is the strongest reading available — a re-read at delivery
// time cannot tell "the process I chose" from "a process that now wears its
// number".
func probeWorkerIdentities(gopiDir string) []ProcIdentity {
	pids := probeWorkerPIDs(gopiDir)
	out := make([]ProcIdentity, 0, len(pids))
	for _, pid := range pids {
		id, err := ReadProcIdentity(pid)
		if err != nil {
			// A candidate that cannot be READ is a fact about a stranger, not a
			// licence to signal: dropped here, never defaulted (Rule 15b).
			continue
		}
		out = append(out, id)
	}
	return out
}

// probeWorkerPIDs is the SCAN half: the pids that pass every identity test.
// It is deliberately separate from probeWorkerIdentities because a caller that
// wants a COUNT (the leak assertion) must not have to signal anything, and a
// caller that signals must not be able to get here without an identity
// (Rule 42).
func probeWorkerPIDs(gopiDir string) []int {
	entries, err := os.ReadDir(reapProcRoot)
	if err != nil {
		return nil
	}
	var pids []int
	for _, e := range entries {
		pid, aerr := strconv.Atoi(e.Name())
		if aerr != nil || pid <= 0 || pid == os.Getpid() {
			continue
		}
		comm, cerr := os.ReadFile(filepath.Join(reapProcRoot, e.Name(), procCommName))
		if cerr != nil || strings.TrimSpace(string(comm)) != procname.NameGopi {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(reapProcRoot, e.Name(), procCmdlineName))
		if rerr != nil {
			continue
		}
		fields := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
		if len(fields) < 2 || fields[1] != procname.VerbWorker {
			continue
		}
		if !workerBelongsToProfile(e.Name(), gopiDir) {
			continue
		}
		if !detachedSession(pid) {
			continue
		}
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	return pids
}

// workerBelongsToProfile reports whether the process in /proc/<pid> resolves
// to gopiDir — the identity, and the whole reason this reap cannot touch a
// worker somebody else started. An unreadable environ (a foreign uid, or a
// process that exited between the readdir and the read) is NOT the sandbox's,
// never a default that would sweep it in.
func workerBelongsToProfile(pidName, gopiDir string) bool {
	raw, err := os.ReadFile(filepath.Join(reapProcRoot, pidName, procEnvironName))
	if err != nil {
		return false
	}
	environ := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	return config.GopiDirFromEnviron(environ) == gopiDir
}

// detachedSession reports whether pid leads its own process session (pid ==
// session id, field 6 of /proc/<pid>/stat) — the post-setsid signature of the
// launcher's workers. A foreground invocation is never a reap candidate.
func detachedSession(pid int) bool {
	raw, err := os.ReadFile(filepath.Join(reapProcRoot, strconv.Itoa(pid), procStatName))
	if err != nil {
		return false
	}
	stat := string(raw)
	// comm may contain spaces AND parentheses, so the LAST ')' closes it; the
	// fields after it are state + ppid + pgrp + session.
	close := strings.LastIndex(stat, ")")
	if close < 0 {
		return false
	}
	fields := strings.Fields(stat[close+1:])
	const sessionIdx = 3
	if len(fields) <= sessionIdx {
		return false
	}
	session, serr := strconv.Atoi(fields[sessionIdx])
	if serr != nil {
		return false
	}
	return session == pid
}

// ---------------------------------------------------------------------------
// sandbox plumbing
// ---------------------------------------------------------------------------

// glmWatchSandbox builds a sandboxed run of the watcher: a private HOME (so the
// script's log and --install target never touch the real profile), a stub
// transport in PATH that records every invocation and exits with wantExit, and
// the script's OWN knobs collapsing the window wait.
type glmWatchSandboxEnv struct {
	env        []string
	stubRecord string
	home       string
}

func glmWatchSandbox(t *testing.T, wantExit string) (glmWatchSandboxEnv, string) {
	t.Helper()
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("create the stub transport dir: %v", err)
	}
	record := filepath.Join(home, "transport-invocations")
	shimDir := filepath.Join(home, "shim")
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		t.Fatalf("create the shim dir: %v", err)
	}
	writeEpochShim(t, filepath.Join(shimDir, testEpochMkdir))
	writeStubSSH(t, filepath.Join(binDir, glmWatchSSHValue))

	logPath := filepath.Join(home, ".gopi", "logs", "glm-night-watch.log")
	env := []string{
		"PATH=" + shimDir + ":" + binDir + ":" + os.Getenv("PATH"),
		"HOME=" + home,
		glmWatchSSHEnv + "=" + glmWatchSSHValue,
		stubSSHExitEnv + "=" + wantExit,
		// The record path reaches the stub through the environment so the stub
		// itself stays a fixed program (no argument encoding to get wrong).
		"STUB_SSH_RECORD=" + record,
		// The script's own window bound: the target is minutes away, the bound
		// is a second, so the wait is abandoned and the delegation branch runs
		// in this test's lifetime without patching any script code.
		glmWatchWindowMaxEnv + "=" + probeWindowMaxSec,
		glmWatchLauncherEnv + "=" + glmWatchLauncherValue,
		glmWatchWorkerNEnv + "=" + glmWatchWorkerNValue,
	}
	return glmWatchSandboxEnv{env: env, stubRecord: record, home: home}, logPath
}

// glmWatchRun is one sandboxed run of the watcher.
type glmWatchRun struct {
	exit     int
	combined string
}

// runGlmWatch runs the script with the sandbox environment and returns its exit
// status plus everything it printed (stdout+stderr; the log file is asserted
// separately where it matters).
func runGlmWatch(t *testing.T, script string, env glmWatchSandboxEnv, args ...string) glmWatchRun {
	t.Helper()
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = env.env
	cmd.Dir = t.TempDir() // never the repo: a run must write nothing into it
	res := glmWatchRun{}
	out, err := cmd.CombinedOutput()
	res.combined = string(out)
	if err == nil {
		res.exit = 0
		return res
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("run %s: %v\n%s", script, err, out)
	}
	res.exit = ee.ExitCode()
	return res
}

// writeStubSSH writes the stub transport: it records the peer and the remote
// command (the delegation, verbatim) and exits with the status the test wants —
// the refusal path is driven by the STATUS, exactly like the real launcher's.
func writeStubSSH(t *testing.T, path string) {
	t.Helper()
	body := "#!/usr/bin/env bash\n" +
		"set -u\n" +
		"# The stub stands in for ssh: record the delegation, then exit with the\n" +
		"# status the test asked for (0 = the launcher accepted, non-zero = a\n" +
		"# failed/refused launch). It starts nothing, ever.\n" +
		"printf '%s\\n' \"$*\" >> \"$" + "STUB_SSH_RECORD\"\n" +
		"exit \"${" + stubSSHExitEnv + ":-0}\"\n"
	writeExecutable(t, path, body)
}

// writeEpochShim writes the PATH shim for the epoch command the watcher uses to
// resolve the window: it reports a target minutes ahead, so the wait's bound
// (the script's own knob) expires immediately and the delegation branch runs.
func writeEpochShim(t *testing.T, path string) {
	t.Helper()
	body := "#!/usr/bin/env bash\n" +
		"# Shimmed epoch: the window is minutes away, so the watcher's own bound\n" +
		"# abandons the wait and delegates now. Only the two spellings the watcher\n" +
		"# uses are answered; -d '@<epoch>' (the human-readable form) falls through.\n" +
		"set -u\n" +
		"case \"$1\" in\n" +
		"  +%s) date -d \"+" + fmt.Sprintf("%d", probeWindowOffsetMin) + " minutes\" +%s ;;\n" +
		"  *) /usr/bin/env date \"$@\" ;;\n" +
		"esac\n"
	writeExecutable(t, path, body)
}

// probeWorkerCount is how many workers the operator-path probe asks for.
const probeWorkerCount = 2

// operatorProbeBody is the sandbox probe: it runs the REAL watcher in the
// sandboxed HOME (a plain shell, an empty environment — the operator's own
// path) and lets the launcher start the workers, then waits for the launch to
// be recorded and exits. It inspects NO process table and kills NOTHING.
//
// The first version of this probe scanned `ps` for the worker argv shape and
// terminated every match. That is the 04:12 incident class — a pattern match
// reaching work the test did not start (it killed five live workers). The
// containment rule this file follows now: a test may act only on processes it
// can NAME (PIDs it spawned itself); when it cannot name them, it must not
// search for them at all. The evidence here is the workers' own output
// artifacts under the sandboxed output root, never the host's process table.
const operatorProbeBody = `
set -u
watcher=$1
home=$2
bash "$watcher" >"$home/probe.out" 2>&1 &
self=$!
wait "$self" 2>/dev/null
`

// countWorkerLoopStarts counts the loop-start banners the launcher's startup
// log carries — one per worker that really started.
func countWorkerLoopStarts(startupLog string) int {
	raw, err := os.ReadFile(startupLog)
	if err != nil {
		return 0
	}
	return strings.Count(string(raw), procname.WorkerLoopStartLine)
}

// waitForWorkerLoopStarts polls the launcher's startup log until want workers
// have announced their loop, or the poll budget runs out. The poll exists
// because a worker needs a moment to exec and print; a single read would report
// zero for a perfectly good launch.
func waitForWorkerLoopStarts(startupLog string, want int) error {
	got := 0
	for i := 0; i < probePollBudget; i++ {
		if got = countWorkerLoopStarts(startupLog); got >= want {
			return nil
		}
		time.Sleep(probePollInterval) //stylegate:allow-sleep poll interval while the launcher announces its loop in the log; the loop is bounded by probePollBudget
	}
	return fmt.Errorf("after %v only %d worker(s) had announced their loop in %s (want %d)",
		probePollInterval*probePollBudget, got, startupLog, want)
}

// launcherBinDir is the directory holding the gopi binary the launcher starts —
// the last path element of the launcher's configured path, so a sandbox never
// hardcodes the checkout it happens to run from.
func launcherBinDir(launcher string) string {
	return filepath.Dir(filepath.Dir(launcher))
}

// The operator-path probe is the ONE case in this file that needs a REAL
// executable: the launcher refuses to start anything when <repo>/bin/<name> is
// missing or not executable (`$SELF: $bin is missing or not executable (build
// with: make build)`, exit 1). Everywhere else this file reads a script's body
// or drives the stub-transport seam, so no binary is involved at all.
const (
	// binDirName is the launcher's own bin directory (<repo>/bin), spelled once
	// (Rule 1b/13) — the path its bin check and every test message read.
	binDirName = "bin"
	// opPathWorkerBinEnv pins the binary the probe's launcher starts. It is the
	// escape hatch for a caller that already knows which build to exercise.
	opPathWorkerBinEnv = "GOPI_TEST_WORKER_BIN"
	// opPathBuildArg / opPathBuildPkg / opPathCgoEnv are the canonical build
	// contract, the same shape settings.json declares for this repo
	// (CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/) — never discovered.
	opPathBuildArg = "build"
	opPathBuildPkg = "./cmd/agent/"
	opPathCgoEnv   = "CGO_ENABLED"
	// opPathBuildTimeout bounds the ONE build (Rule 1a: no inline deadline).
	opPathBuildTimeout = 300 * time.Second
)

// opPathBinOnce / opPathBinPath / opPathBinErr / opPathBuiltBin memoise the
// binary the probe runs, for the whole test binary — mirroring cmd/agent's
// gopiTestBinPath. The cache matters twice over: `go test ./...` may run this
// package's gates concurrently, and a build per gate would be the suite paying
// for the same artifact repeatedly.
//
//nolint:gochecknoglobals // process-level build cache (see cmd/agent/gopibin_test.go)
var (
	opPathBinOnce  sync.Once
	opPathBinPath  string
	opPathBinErr   error
	opPathBuiltBin string
)

// opPathWorkerBin resolves the executable the probe's launcher will run, so the
// operator-path case never depends on a by-hand `make build` in the worktree it
// happens to run from: the acceptance gate builds with `go build ./...`, which
// writes NO binary into bin/, so a fresh worktree carries none — and the probe
// would then fail on the launcher's (correct) refusal instead of on the
// contract it checks.
//
// Order: an operator-pinned $GOPI_TEST_WORKER_BIN, then an already-built
// <repo>/bin/<name>, then a build of THIS checkout. The second step is the
// property the case is really about: the launcher is driven with its DEFAULT
// binary path, so pointing it at some outside binary would test a wiring the
// operator never uses.
//
// A checkout whose build cannot be produced is SKIPPED, never failed — the same
// contract run.go's GopiBin keeps for a toolchain-less sandbox.
func opPathWorkerBin(t *testing.T, root string) string {
	t.Helper()
	opPathBinOnce.Do(func() { opPathBinPath, opPathBinErr = resolveOpPathWorkerBin(root) })
	if opPathBinErr != nil {
		// The skip reason is a single literal (the stylegate's test-skip rule
		// reads one, never a concatenation) carrying the resolved build error in
		// its %v: that output explains the skip better than any fixed sentence,
		// and the operator knob it names is the remedy.
		t.Skipf("no gopi binary for the operator-path probe: %v — run `make build` (or set $%s=<path>) to exercise this case",
			opPathBinErr, opPathWorkerBinEnv)
	}
	if _, err := os.Stat(opPathBinPath); err == nil {
		return opPathBinPath
	}
	// A sibling gate (or a suite cleaning up its own build) can consume the file
	// between the cache and this call; the built artifact is kept, so the
	// launcher's path is restored from it. Only when THAT is gone too is the
	// case unrunnable.
	if opPathBuiltBin != "" {
		if err := os.Rename(opPathBuiltBin, opPathBinPath); err == nil {
			return opPathBinPath
		}
	}
	t.Skipf("the operator-path probe's binary %s disappeared mid-run (a concurrent suite?) — nothing to launch",
		opPathBinPath)
	return ""
}

// resolveOpPathWorkerBin produces the binary path ONCE (the sync.Once above).
func resolveOpPathWorkerBin(root string) (string, error) {
	def := filepath.Join(root, binDirName, procname.NameGopi)
	if pinned := os.Getenv(opPathWorkerBinEnv); pinned != "" {
		if _, err := os.Stat(pinned); err != nil {
			return def, fmt.Errorf("%s=%s is not usable: %w", opPathWorkerBinEnv, pinned, err)
		}
		return pinned, nil
	}
	if fi, err := os.Stat(def); err == nil && fi.Mode()&0o111 != 0 {
		return def, nil
	}
	return buildWorkerBin(root, def)
}

// buildWorkerBin builds the binary the launcher starts, then puts it at the
// launcher's default path. The build lands in a TEMP FILE inside that same
// directory (never a target of its own: a test process that is killed mid-build
// must not leave something that looks like a runnable gopi), and the rename is
// atomic, so a concurrent gate can never observe a half-written binary.
//
// When the path is already taken — an operator's own build, or a concurrent
// gate — the separate copy is returned instead of racing the rename: the
// launcher can carry any repo, and the probe names the one under test through
// its own GOPI_WORKER_REPO knob (a knob, never a patched code path).
func buildWorkerBin(root, def string) (string, error) {
	dir := filepath.Dir(def)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return def, fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, procname.NameGopi+buildTmpSuffix)
	if err != nil {
		return def, fmt.Errorf("create the build target in %s: %w", dir, err)
	}
	built := tmp.Name()
	if err := tmp.Close(); err != nil {
		return def, fmt.Errorf("close the build target %s: %w", built, err)
	}
	// Remove the (empty) placeholder first: `go build -o` refuses to overwrite
	// a file it did not create.
	if err := os.Remove(built); err != nil {
		return def, fmt.Errorf("clear the build target %s: %w", built, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), opPathBuildTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", opPathBuildArg, "-o", built, opPathBuildPkg)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), opPathCgoEnv+"=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		// The failed artifact is left in place: it is the only record of why the
		// build did not produce a binary.
		return def, fmt.Errorf("go %s -o %s %s: %w\n%s", opPathBuildArg, built, opPathBuildPkg, err, out)
	}
	opPathBuiltBin = built
	if err := os.Rename(built, def); err != nil {
		// def exists (or is not replaceable): the separate copy is the target.
		return built, nil
	}
	return def, nil
}

// buildTmpSuffix names the build's temporary file (Rule 1b: the pattern is
// spelled once). It starts with the dot `go build` ignores from a package
// directory, so the file can never be mistaken for a source package.
const buildTmpSuffix = "*.building"

// inlineStartMarker is the substring whose PRESENCE in a run's output means an
// inline worker start happened (the second-launcher shape this epic removes).
func inlineStartMarker() string { return "./bin/" + procname.NameGopi + " " + procname.VerbWorker }

// TestWorkerStartupLogNameIsTheLaunchersFile pins the ONE file this gate reads
// against the launcher's own spelling of it: the launcher creates
// <gopi-dir>/logs/worker-startup.log as the single pre-claim record, and a gate
// that read a differently-spelled path would report zero for every good launch.
func TestWorkerStartupLogNameIsTheLaunchersFile(t *testing.T) {
	body := readLauncherScript(t)
	if !strings.Contains(body, workerStartupLogName) {
		t.Fatalf("%s no longer names %q — this gate counts worker loop-start banners in that file (the launcher's "+
			"one stable pre-claim record); if the launcher renamed it, this gate must follow, or it silently "+
			"reports zero workers forever", workerUpScriptRel, workerStartupLogName)
	}
	if !strings.Contains(body, startupLogShellVar) {
		t.Fatalf("%s no longer declares %s — the startup log path is the evidence a launch test reads",
			workerUpScriptRel, startupLogShellVar)
	}
}

// startupLogShellVar is the launcher's shell variable holding the startup-log
// path (Rule 1b: named here rather than typed into the assertion).
const startupLogShellVar = "STARTUP_LOG"

// readLauncherScript reads the single launcher's body.
func readLauncherScript(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRootForProcnameGate(t), filepath.FromSlash(workerUpScriptRel)))
	if err != nil {
		t.Fatalf("read %s: %v", workerUpScriptRel, err)
	}
	return string(raw)
}

// listTree renders a directory tree (two levels) for a failure message, so a
// missing artifact is diagnosable from the test output alone.
func listTree(root string) string {
	var b strings.Builder
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error { //stylegate:ignore diagnostic tree render for a failure message; a walk error only shortens the message
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil || strings.Count(rel, string(os.PathSeparator)) > 2 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		fmt.Fprintf(&b, "  %s\n", rel)
		return nil
	})
	if b.Len() == 0 {
		return "  (empty)"
	}
	return b.String()
}

// readIfPresent reads a file for a diagnostic message, returning a note when it
// does not exist.
func readIfPresent(path string) string {
	body, err := os.ReadFile(path)
	if err != nil {
		return "  (absent: " + path + ")"
	}
	return string(body)
}

// readGlmWatchScript reads the watcher's body at the revision the test runs at.
func readGlmWatchScript(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRootForProcnameGate(t), filepath.FromSlash(glmWatchScriptRel)))
	if err != nil {
		t.Fatalf("read %s: %v", glmWatchScriptRel, err)
	}
	return string(raw)
}

// writeExecutable writes a script and marks it executable.
func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
