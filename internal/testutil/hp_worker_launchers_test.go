package testutil

// THE HP-LAUNCHER GATE (epic hp-inline-launchers, 2026-09-12).
//
// THE DEFECT THIS CLOSES: HP (the second worker host) bypassed the
// single-launcher contract. It had NO installed launcher, and its two
// unsupervised starters started workers THEMSELVES —
//
//     setsid nohup ./bin/gopi worker >> "$LOG" 2>&1 < /dev/null &
//     echo "started pid $!" >> "$LOG"
//
// — with no exit-status check. A launch REFUSED by the agent-session guard
// (exit 3) was therefore logged exactly like a success, and nothing was left
// pointing at the operator-owned launcher. Those two scripts live OUTSIDE the
// repo (~/bin on HP), so the repo copies under scripts/peer-worker/ are their source of
// truth and the object this gate audits: an HP-side edit that re-inlines a
// worker start is caught by `go test`, not by reading a remote file.
//
// THE RULES (per script):
//  1. DELEGATION ONLY — no inline start shape (the SAME regexes the GLM watcher
//     gate uses, so the two cannot drift), no per-worker loop, and the count
//     must reach the launcher as a single call.
//  2. CHECKED STATUS — the launcher's status is captured, a non-zero status
//     prints a FAILED line naming the refusal code and the fact that NO worker
//     was started, and the script exits with that status.
//  3. THE OPERATOR POINTER — the canonical worker-ownership pointer line, which
//     the repo gate (TestWorkerOwnershipPointerPresent) also requires of any
//     file naming the launcher.
//  4. BEHAVIOUR, NOT SHAPE — the loudness rule is driven end to end with a stub
//     launcher in a sandbox, so a script that merely CONTAINS the right words
//     while returning 0 still fails.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/procname"
	"golang.org/x/sys/unix"
)

const (
	// hpScriptsDir is the repo-side home of the HP launchers (Rule 6b: the
	// deliverable is shipped FROM here to ~/bin on the peer).
	hpScriptsDir = scriptsDirName + "/peer-worker"
	// hpAutostartName / hpNightsafeName are the two starters the epic rewrites.
	// They are the names the peer installs, so the repo copy must match exactly.
	hpAutostartName = "peer-launcher-autostart.sh"
	hpNightsafeName = "peer-launcher-nightsafe.sh"
	hpAutostartRel  = hpScriptsDir + "/" + hpAutostartName
	hpNightsafeRel  = hpScriptsDir + "/" + hpNightsafeName
	// hpLauncherVar is the shell variable holding the peer-side launcher path:
	// the delegation target is named ONCE per script (Rule 13), never typed at
	// the call site.
	hpLauncherVar = "LAUNCHER"
	// hpCountFlag is the launcher's counting seam — the liveness source of
	// truth these scripts read instead of running their own `pgrep`.
	hpCountFlag = "--count"
	// hpDryRunFlag is the launcher seam a --dry-run starter prints.
	hpDryRunFlag = "--dry-run"
	// hpRefusalKnob is the env knob carrying the agent-session guard's refusal
	// status (Rule 1a: never an inline 3 at the message or the comparison).
	hpRefusalKnob = "GOPI_WORKER_REFUSAL_EXIT_CODE"
	// hpLauncherKnob / hpRepoKnob / hpCountKnob are the delegation knobs each
	// script reads, with the defaults its env-default declaration states.
	hpLauncherKnob = "GOPI_WORKER_LAUNCHER"
	hpRepoKnob     = "GOPI_WORKER_REPO"
	hpCountKnob    = "GOPI_WORKER_N"
	// hpNoWorkerStatement is the sentence a refused launch must print: the
	// epic's acceptance asks for an explicit statement that NOTHING started.
	hpNoWorkerStatement = "no worker was started"
	// hpFailureMarker is the token the log line and stderr carry.
	hpFailureMarker = "FAILED"
	// hpLogKnobs are the per-script log-path knobs (they differ, so they are
	// listed rather than derived).
	hpAutostartLogKnob = "GOPI_WORKER_AUTOSTART_LOG"
	hpNightsafeLogKnob = "GOPI_WORKER_NIGHTSAFE_LOG"
	// hpNightStartKnob / hpNightEndKnob bound the night window; the behaviour
	// test opens the window with them instead of waiting for a wall clock.
	hpNightStartKnob = "GOPI_NIGHT_START_HOUR"
	hpNightEndKnob   = "GOPI_NIGHT_END_HOUR"
	// hpLegacyAuditRel is the THIRD inline worker-start path the sweep found on
	// the peer (~/.gopi/peer-worker-night-legacy-audit.sh), kept repo-side for the same
	// reason as the two starters. It is deliberately NOT in hpScripts: it is a
	// bounded per-round drainer (a hand-run campaign tool, scheduled by
	// nothing), not a launcher — so the delegation rule does not apply to it,
	// while the CHECKED-STATUS rule does (it used to loop over a worker start
	// with no status check, the same silent-success hole).
	hpLegacyAuditRel = hpScriptsDir + "/peer-worker-night-legacy-audit.sh"
	// hpWorkerBinKnob is the drainer's binary knob — a knob only so the
	// loudness path is driveable with a stub; the default is the real binary.
	hpWorkerBinKnob = "GOPI_WORKER_BIN"
	// hpEnvDefaultRe matches a `VAR="${GOPI_KNOB:-default}"` declaration.
	hpEnvDefaultRe = `(?m)^([A-Z_]+)="\$\{(GOPI_[A-Z_]+):-([^}]*)\}"`
)

// hpScripts are the two starters, paired with the log knob each one owns.
var hpScripts = []struct {
	rel     string
	logKnob string
}{
	{hpAutostartRel, hpAutostartLogKnob},
	{hpNightsafeRel, hpNightsafeLogKnob},
}

// hpKnownKnobs is the closed set of env knobs these scripts may read. A new
// knob is a new contract (Rule 1b/14): it must be added here with its default
// and its reason, or the gate fails rather than letting a drift through.
var hpKnownKnobs = map[string]string{
	hpLauncherKnob:     "$HOME/bin/" + workerUpScriptName,
	hpRepoKnob:         "$HOME/work/GoPi",
	hpCountKnob:        "1",
	hpRefusalKnob:      glmRefusalExitText, // the guard's refusal status, pinned once
	hpNightStartKnob:   "23",
	hpNightEndKnob:     "9",
	hpAutostartLogKnob: "$(gopi_dir)/logs/worker-autostart.log",
	hpNightsafeLogKnob: "$(gopi_dir)/logs/worker-night-cron.log",
	hpWorkerBinKnob:    "/usr/local/bin/gopi",
}

// readHpScript reads a repo-side HP starter.
func readHpScript(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRootForProcnameGate(t), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("%s must exist — it is the source of truth for the peer's ~/bin copy: %v", rel, err)
	}
	if len(raw) == 0 {
		t.Fatalf("%s is empty", rel)
	}
	return string(raw)
}

// TestHpWorkerLaunchersDelegateToTheSingleLauncher is the SHAPE gate: the only
// way either script may cause a worker to exist is one call to the single
// launcher carrying the count.
func TestHpWorkerLaunchersDelegateToTheSingleLauncher(t *testing.T) {
	for _, tc := range hpScripts {
		t.Run(tc.rel, func(t *testing.T) {
			body := readHpScript(t, tc.rel)
			code := stripShellComment(body)

			for _, re := range inlineStartPatterns {
				if m := re.FindString(code); m != "" {
					t.Errorf("%s starts workers ITSELF (%q) — that is the second, unsupervised launcher this epic removes: "+
						"the ONE start path is the single launcher %s (log path, argv shape, process accounting and the "+
						"operator's single point of control all live there)", tc.rel, strings.TrimSpace(m), workerUpScriptRel)
				}
			}
			if strings.Contains(code, "setsid") || strings.Contains(code, "nohup") {
				t.Errorf("%s still detaches a process itself (setsid/nohup in executable code) — detaching is the "+
					"launcher's job; a starter that detaches is a second launcher whatever it spawns", tc.rel)
			}
			if strings.Contains(code, "for i in") || strings.Contains(code, "seq 1") {
				t.Errorf("%s loops over a per-worker index — the launcher takes the COUNT and owns the spawn loop "+
					"(one call, N workers); a loop here is a second launch path", tc.rel)
			}
			// The delegation target is named through the variable, and the file
			// name is this system's launcher contract, not a per-script choice.
			if !strings.Contains(code, hpLauncherVar) || !strings.Contains(code, workerUpScriptName) {
				t.Errorf("%s does not name the single launcher (%q) via %s — the starter must DELEGATE to %s",
					tc.rel, workerUpScriptName, hpLauncherVar, workerUpScriptRel)
			}
			if !strings.Contains(code, `-x "$`+hpLauncherVar+`"`) {
				t.Errorf("%s does not check that the launcher exists and is executable — a missing launcher must be a "+
					"FAILED launch, never a fallback to an inline start", tc.rel)
			}
			if !strings.Contains(code, `"$`+hpLauncherVar+`" "$N"`) {
				t.Errorf("%s does not invoke the launcher with the worker count (want \"$%s\" \"$N\") — the count must "+
					"reach the launcher, not be expanded here", tc.rel, hpLauncherVar)
			}
			if !strings.Contains(code, hpCountFlag) {
				t.Errorf("%s does not use the launcher's %s seam for the liveness check — a second accounting "+
					"implementation (pgrep) is how the numbers drifted apart in the first place", tc.rel, hpCountFlag)
			}
			if !strings.Contains(code, hpDryRunFlag) {
				t.Errorf("%s has no %s seam — the epic's acceptance proves the delegation with a dry run that starts "+
					"nothing", tc.rel, hpDryRunFlag)
			}
		})
	}
}

// TestHpWorkerLaunchersReportRefusalLoudly is the STATIC half of the
// silent-failure gate: the words and the status flow an operator needs.
func TestHpWorkerLaunchersReportRefusalLoudly(t *testing.T) {
	for _, tc := range hpScripts {
		t.Run(tc.rel, func(t *testing.T) {
			code := stripShellComment(readHpScript(t, tc.rel))

			if !strings.Contains(code, "status=$?") {
				t.Errorf("%s does not capture the launcher's exit status (want `status=$?` immediately after the "+
					"call) — an unchecked status is exactly the defect this epic closes", tc.rel)
			}
			if !strings.Contains(code, `"$status" -ne 0`) {
				t.Errorf("%s never compares the launcher status against zero — a refused launch must take a "+
					"different path from an accepted one", tc.rel)
			}
			if !strings.Contains(code, hpFailureMarker) || !strings.Contains(code, hpNoWorkerStatement) {
				t.Errorf("%s does not print a %s line stating that %q — the acceptance requires the refusal to be "+
					"loud AND explicit about nothing having started", tc.rel, hpFailureMarker, hpNoWorkerStatement)
			}
			if !strings.Contains(code, `exit "$status"`) {
				t.Errorf("%s does not propagate the launcher's status (want `exit \"$status\"`) — a cron caller "+
					"cannot see a refusal whose exit code is swallowed", tc.rel)
			}
			if !strings.Contains(code, `REFUSAL_EXIT_CODE`) {
				t.Errorf("%s does not name the refusal code through its knob — the FAILED message must spell out the "+
					"agent-session guard's status, not leave the operator to guess", tc.rel)
			}
		})
	}
}

// TestHpWorkerLauncherKnobsAreDeclared is the Rule 1b/13/14 gate: every knob
// these scripts read is declared with its env name, and the knobs the epic
// pins (the launcher path, the count, the refusal code) default to exactly the
// value the rest of the system uses.
func TestHpWorkerLauncherKnobsAreDeclared(t *testing.T) {
	re := regexp.MustCompile(hpEnvDefaultRe)
	for _, tc := range hpScripts {
		t.Run(tc.rel, func(t *testing.T) {
			body := readHpScript(t, tc.rel)
			seen := map[string]string{}
			for _, m := range re.FindAllStringSubmatch(body, -1) {
				envName, value := m[2], m[3]
				want, known := hpKnownKnobs[envName]
				if !known {
					t.Errorf("%s declares %s from an env knob this gate does not know — a new knob is a new contract: "+
						"add it to hpKnownKnobs with its default", tc.rel, envName)
					continue
				}
				if value != want {
					t.Errorf("%s defaults %s to %q; the shared value is %q — the two must agree, so a rename is a red "+
						"build rather than a silent drift", tc.rel, envName, value, want)
				}
				seen[envName] = value
			}
			for _, must := range []string{hpLauncherKnob, hpRepoKnob, hpCountKnob, hpRefusalKnob, tc.logKnob} {
				if _, ok := seen[must]; !ok {
					t.Errorf("%s no longer declares %s — the value was either dropped or typed inline (Rule 1b)",
						tc.rel, must)
				}
			}
			if seen[hpRefusalKnob] != glmRefusalExitText {
				t.Errorf("%s declares the refusal code as %q; the agent-session guard's status is %q — the FAILED "+
					"message must name the code the guard actually returns", tc.rel, seen[hpRefusalKnob], glmRefusalExitText)
			}
			if !strings.Contains(seen[hpLauncherKnob], workerUpScriptName) {
				t.Errorf("%s defaults the launcher to %q, which is not this system's single launcher (%s)",
					tc.rel, seen[hpLauncherKnob], workerUpScriptName)
			}
		})
	}
}

// TestHpWorkerLaunchersCarryTheWorkerOwnershipPointer keeps the two files
// inside the repo-wide policy gate: a file that names the launcher must carry
// the canonical operator-only pointer (devdoc/worker-ownership.md), so the
// scripts cannot quietly become agent-executable instructions.
func TestHpWorkerLaunchersCarryTheWorkerOwnershipPointer(t *testing.T) {
	for _, tc := range hpScripts {
		t.Run(tc.rel, func(t *testing.T) {
			line, ok := workerOwnershipPointerLine(readHpScript(t, tc.rel))
			if !ok {
				t.Fatalf("%s does not carry the canonical pointer line — the annotation must be the ONE line "+
					"combining %q with %s", tc.rel, workerOwnershipMarker, workerOwnershipCanonicalRel)
			}
			if !strings.HasPrefix(line, "#") {
				t.Errorf("%s points at the canonical block on a non-comment line (%q) — the pointer is documentation, "+
					"not code", tc.rel, strings.TrimSpace(line))
			}
		})
	}
}

// --- behaviour: the loudness rule, driven end to end ----------------------

// hpBuildEnv renders a subprocess environment for a starter: the ambient
// environment with every GOPI_* variable dropped (so a knob set for this suite
// cannot leak into the probe) plus the knobs the probe supplies.
func hpBuildEnv(extra ...string) []string {
	out := make([]string, 0, len(os.Environ())+len(extra))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GOPI_") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, extra...)
}

// hpRunStarter runs a starter with a sandbox home and returns its combined
// output, its exit status and the log the script wrote.
func hpRunStarter(t *testing.T, rel, sandbox string, args []string, env ...string) (string, int, string) {
	t.Helper()
	return hpRunStarterStdin(t, rel, sandbox, args, nil, env...)
}

// hpRunStarterStdin is hpRunStarter with an explicit stdin: a WATCHED run is
// one with a terminal on fd 0, so the retirement gate's second arm is driven by
// handing it the slave of a pseudo-terminal (see openHpPTY). A nil stdin means
// /dev/null — a scheduler's shape.
func hpRunStarterStdin(t *testing.T, rel, sandbox string, args []string, stdin *os.File, env ...string) (string, int, string) {
	t.Helper()
	script := filepath.Join(repoRootForProcnameGate(t), filepath.FromSlash(rel))
	return hpRunCommandStdin(t, script, sandbox, args, stdin, env...)
}

// hpRunCommandStdin runs any command with a controlled stdin and environment:
// hpRunStarterStdin runs a starter through it, and the scheduler-wrapper probe
// runs a WRAPPER that then invokes the starter (which is the only way the
// ancestry arm can be reached — a direct invocation's parent is the test
// process).
func hpRunCommandStdin(t *testing.T, path, sandbox string, args []string, stdin *os.File, env ...string) (string, int, string) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{path}, args...)...)
	cmd.Env = hpBuildEnv(env...)
	if stdin != nil {
		cmd.Stdin = stdin
	} else {
		// An explicit NUL stdin: the probe must never inherit the suite's own
		// terminal, which would make the refusal arm unreachable.
		devNull, err := os.Open(os.DevNull)
		if err != nil {
			t.Fatalf("open %s: %v", os.DevNull, err)
		}
		defer devNull.Close() //stylegate:ignore the harness's own fixture teardown
		cmd.Stdin = devNull
	}
	out, err := cmd.CombinedOutput()
	status := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run %s: %v", path, err)
		}
		status = ee.ExitCode()
	}
	logBody, _ := os.ReadFile(filepath.Join(sandbox, "log")) //stylegate:ignore launcher log captured for the caller's failure message; an absent file yields an empty body
	return string(out), status, string(logBody)
}

// --- the status-78 contract: the refusal is driven with a REAL pty ---------

// hpRetiredExitText is the status a retirement refusal returns (EX_CONFIG in
// sysexits(3)): deliberately different from the guard's own refusal code, so
// "this script is retired" and "the launch was refused" cannot be confused.
const hpRetiredExitText = "78"

// hpRetiredMarker is the token the refusal line carries; an operator greps it.
const hpRetiredMarker = "REFUSED"

// hpRulingTexts are the two facts a refusal message must name: the operator's
// ruling, and the measured consequence (the mask).
var hpRulingTexts = []string{"cronjob", "1,214", "MASKED"}

// openHpPTY opens a pty pair: the MASTER is held by the test, the SLAVE is the
// stdin of the run under test — which is what "an operator is watching" means
// to the kernel, and therefore to the gate ([ -t 0 ]). Hosts without /dev/ptmx
// skip, because the descriptor IS the subject.
func openHpPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no /dev/ptmx on this host (%v): the retirement gate's terminal arm cannot be exercised without a real pty", err)
	}
	var n int
	if _, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN); err != nil {
		master.Close() //stylegate:ignore the harness's own fixture teardown
		t.Skipf("TIOCGPTN unavailable (%v): cannot resolve the pty slave", err)
	}
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		master.Close() //stylegate:ignore the harness's own fixture teardown
		t.Skipf("TIOCSPTLCK unavailable (%v): cannot open the pty slave", err)
	}
	n, err = unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		master.Close() //stylegate:ignore the harness's own fixture teardown
		t.Skipf("TIOCGPTN unavailable after unlock (%v)", err)
	}
	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR, 0)
	if err != nil {
		master.Close() //stylegate:ignore the harness's own fixture teardown
		t.Skipf("cannot open the pty slave (%v)", err)
	}
	t.Cleanup(func() {
		slave.Close()  //stylegate:ignore closes the pty slave the fixture opened
		master.Close() //stylegate:ignore closes the pty master the fixture opened
	})
	return master, slave
}

// hpWriteStub writes an executable stub and returns its path.
func hpWriteStub(t *testing.T, path, body string) string {
	t.Helper()
	writeExecutable(t, path, body)
	return path
}

// TestHpWorkerLaunchersRefusedLaunchIsLoudNonZeroAndSilentOnSuccess is the
// end-to-end half of the gate: a stub launcher stands in for the real one, so
// the whole script runs with no peer, no worker and no task.
func TestHpWorkerLaunchersRefusedLaunchIsLoudNonZeroAndSilentOnSuccess(t *testing.T) {
	for _, tc := range hpScripts {
		t.Run(tc.rel, func(t *testing.T) {
			sandbox := t.TempDir()
			logPath := filepath.Join(sandbox, "log")
			// The retirement gate's terminal arm: a WATCHED run is one with a
			// terminal on stdin, so every probe below hands the script the
			// slave side of a real pty (hpRunStarter's default is /dev/null,
			// which is a scheduler's shape and refuses).
			_, watched := openHpPTY(t)

			// A repo whose bin/gopi reports pending work: the pending gate must
			// let the delegation through without a real queue.
			repo := filepath.Join(sandbox, "repo")
			if err := os.MkdirAll(filepath.Join(repo, "bin"), 0o755); err != nil {
				t.Fatalf("mkdir repo bin: %v", err)
			}
			hpWriteStub(t, filepath.Join(repo, "bin", procname.NameGopi), "#!/bin/bash\necho '[pending] stub task'\n")

			// The stub launcher: --count reports a state, the launch call takes
			// the state from STUB_LAUNCH_EXIT and records every invocation.
			calls := filepath.Join(sandbox, "calls")
			stub := hpWriteStub(t, filepath.Join(sandbox, workerUpScriptName), `#!/bin/bash
printf '%s\n' "$*" >> "`+calls+`"
if [ "${1:-}" = "`+hpCountFlag+`" ]; then
  echo "live gopi workers: ${STUB_WORKERS:-0} (pids: none)"
  echo "other gopi processes: 0 (not workers — daemons, the merger, one-shot run children)"
  exit 0
fi
echo "stub launcher: launching $1 worker(s)"
exit "${STUB_LAUNCH_EXIT:-0}"
`)
			base := []string{
				"HOME=" + sandbox,
				"GOPI_HOME=" + sandbox,
				hpRepoKnob + "=" + repo,
				tc.logKnob + "=" + logPath,
				hpNightStartKnob + "=0",
				hpNightEndKnob + "=24", // window always open for the nightsafe probe
			}

			// (a) a MISSING launcher is a failed launch: loud, 127, nothing started.
			out, status, logBody := hpRunStarterStdin(t, tc.rel, sandbox, nil, watched, append(base, hpLauncherKnob+"="+filepath.Join(sandbox, "absent.sh"))...)
			if status == 0 {
				t.Errorf("(a) missing launcher: exit status 0 — a missing launcher must fail loudly, never fall back "+
					"to an inline start\noutput:\n%s", out)
			}
			for _, want := range []string{hpFailureMarker, hpNoWorkerStatement} {
				if !strings.Contains(out, want) {
					t.Errorf("(a) missing launcher: output does not contain %q\n%s", want, out)
				}
			}
			if !strings.Contains(logBody, hpFailureMarker) {
				t.Errorf("(a) missing launcher: the log does not record the failure\n%s", logBody)
			}

			// Each probe reads a FRESH log: the log is an append-only record, so
			// a FAILED line from probe (a) must not be mistaken for one written
			// by the probe under test.
			if err := os.WriteFile(logPath, nil, 0o644); err != nil {
				t.Fatalf("reset the log: %v", err)
			}

			// (b) a REFUSING launcher (the guard's exit 3) is named as such.
			out, status, logBody = hpRunStarterStdin(t, tc.rel, sandbox, nil, watched, append(base,
				hpLauncherKnob+"="+stub, "STUB_LAUNCH_EXIT="+glmRefusalExitText)...)
			if status != 3 {
				t.Errorf("(b) refusing launcher: exit status %d, want 3 (the launcher's refusal) — a cron caller "+
					"must see the refusal\noutput:\n%s", status, out)
			}
			// The FAILED line must name the code MACHINE-readably ($GOPI_WORKER_REFUSAL_EXIT_CODE=3),
			// so a grep for the knob finds the refusal regardless of prose.
			for _, want := range []string{hpFailureMarker, "$" + hpRefusalKnob + "=" + glmRefusalExitText, hpNoWorkerStatement} {
				if !strings.Contains(out, want) {
					t.Errorf("(b) refusing launcher: output does not contain %q — the refusal must be loud and named\n%s", want, out)
				}
			}
			if !strings.Contains(logBody, hpFailureMarker) {
				t.Errorf("(b) refusing launcher: the log does not record the failure\n%s", logBody)
			}

			// (c) an ACCEPTING launcher: exit 0, no failure line, exactly ONE
			// launch call after the count call.
			if err := os.Remove(calls); err != nil && !os.IsNotExist(err) {
				t.Fatalf("reset calls: %v", err)
			}
			if err := os.WriteFile(logPath, nil, 0o644); err != nil {
				t.Fatalf("reset the log: %v", err)
			}
			out, status, logBody = hpRunStarterStdin(t, tc.rel, sandbox, nil, watched, append(base, hpLauncherKnob+"="+stub)...)
			if status != 0 {
				t.Errorf("(c) accepting launcher: exit status %d, want 0\noutput:\n%s", status, out)
			}
			if strings.Contains(out, hpFailureMarker) || strings.Contains(logBody, hpFailureMarker) {
				t.Errorf("(c) accepting launcher: a FAILED line was emitted on the success path\nout:\n%s\nlog:\n%s", out, logBody)
			}
			rawCalls, err := os.ReadFile(calls)
			if err != nil {
				t.Fatalf("(c) the stub recorded no invocation: %v", err)
			}
			invocations := strings.Fields(strings.TrimSpace(string(rawCalls)))
			if len(invocations) != 2 {
				t.Errorf("(c) the stub was invoked %d times (%q), want 2: one %s liveness call and ONE launch call "+
					"carrying the count", len(invocations), strings.TrimSpace(string(rawCalls)), hpCountFlag)
			}
			if !strings.Contains(string(rawCalls), hpCountFlag) {
				t.Errorf("(c) the liveness number did not come from the launcher's %s seam: %q", hpCountFlag, string(rawCalls))
			}

			// (d) a LIVE worker: the script asks for nothing (no launch call).
			if err := os.Remove(calls); err != nil && !os.IsNotExist(err) {
				t.Fatalf("reset calls: %v", err)
			}
			out, status, _ = hpRunStarterStdin(t, tc.rel, sandbox, nil, watched, append(base, hpLauncherKnob+"="+stub, "STUB_WORKERS=1")...)
			if status != 0 {
				t.Errorf("(d) worker alive: exit status %d, want 0\noutput:\n%s", status, out)
			}
			rawCalls, err = os.ReadFile(calls)
			if err != nil {
				t.Fatalf("(d) the count call was not made: %v", err)
			}
			if got := strings.TrimSpace(string(rawCalls)); got != hpCountFlag {
				t.Errorf("(d) worker alive: the stub saw %q, want only %s — a live worker must stop the delegation "+
					"before it asks for another", got, hpCountFlag)
			}
		})
	}
}

// TestHpNightLegacyAuditDrainerChecksEveryRoundStatus gates the third sweep
// hit. The drainer legitimately keeps its inline `worker --once` round — it is
// NOT a launcher-shaped spawn (no setsid/nohup, no second log contract, no
// process accounting), it is a bounded per-round drainer for one campaign — so
// the delegation rule does not apply. The silent-success rule does: every round
// must capture the status, a non-zero round must stop the loop loudly with the
// refusal code named, and the loop must not continue past it.
func TestHpNightLegacyAuditDrainerChecksEveryRoundStatus(t *testing.T) {
	code := stripShellComment(readHpScript(t, hpLegacyAuditRel))

	if strings.Contains(code, "setsid") || strings.Contains(code, "nohup") {
		t.Errorf("%s detaches a process (setsid/nohup) — the drainer runs the round in FOREGROUND and waits for it; "+
			"detaching is the single launcher's job", hpLegacyAuditRel)
	}
	// The round's status is captured immediately after the call and compared.
	roundIdx := strings.Index(code, "worker --once")
	if roundIdx < 0 {
		t.Fatalf("%s no longer runs a `worker --once` round — if the campaign tool became a launcher call, this gate "+
			"and the devdoc's disposition both need updating (it must not silently drop the checked-status rule)",
			hpLegacyAuditRel)
	}
	after := code[roundIdx:]
	if !strings.Contains(after, "status=$?") {
		t.Errorf("%s does not capture the round's exit status (`status=$?` right after the round) — an unchecked "+
			"round is the silent-success hole this disposition closes", hpLegacyAuditRel)
	}
	if !strings.Contains(after, `"$status" -ne 0`) {
		t.Errorf("%s never compares the round status against zero — a refused round must stop the loop", hpLegacyAuditRel)
	}
	if !strings.Contains(after, "exit \"$status\"") {
		t.Errorf("%s does not propagate the refused round's status — an unattended caller cannot see the refusal",
			hpLegacyAuditRel)
	}
	for _, want := range []string{hpFailureMarker, "no further round was started", "REFUSAL_EXIT_CODE"} {
		if !strings.Contains(code, want) {
			t.Errorf("%s does not carry %q — a failed round must be loud, name the refusal code and state that the "+
				"run stopped", hpLegacyAuditRel, want)
		}
	}
	// The binary is named through its knob (Rule 13/1b), not typed inline: the
	// knob is also what makes the loudness path testable with a stub.
	if !strings.Contains(code, `"$`+hpWorkerBinKnob+`" worker --once`) {
		t.Errorf("%s does not drive the round through $%s — the binary must be named once, and the knob is what makes "+
			"the refusal path reachable in a test", hpLegacyAuditRel, hpWorkerBinKnob)
	}
	if _, ok := hpKnownKnobs[hpWorkerBinKnob]; !ok {
		t.Errorf("%s reads %s but the gate's knob table does not declare it", hpLegacyAuditRel, hpWorkerBinKnob)
	}
}

// TestHpNightLegacyAuditDrainerStopsLoudlyOnRefusal drives the drainer end to
// end with a stub binary that refuses (the guard's exit 3): the loop must stop
// after ONE round, say FAILED with the refusal code, and exit 3.
func TestHpNightLegacyAuditDrainerStopsLoudlyOnRefusal(t *testing.T) {
	sandbox := t.TempDir()
	// A stub HOME so the drainer's ~/.gopi writes land in the sandbox: the
	// script reads $HOME at least for settings.json / memory.db / its logs.
	if err := os.MkdirAll(filepath.Join(sandbox, ".gopi"), 0o755); err != nil {
		t.Fatalf("mkdir .gopi: %v", err)
	}
	stub := hpWriteStub(t, filepath.Join(sandbox, "gopi-refuse"),
		"#!/bin/bash\necho \"agent-session guard: refusing to start a worker\" >&2\nexit "+glmRefusalExitText+"\n")

	script := filepath.Join(repoRootForProcnameGate(t), filepath.FromSlash(hpLegacyAuditRel))
	cmd := exec.Command("bash", script)
	cmd.Env = hpBuildEnv("HOME="+sandbox, hpWorkerBinKnob+"="+stub)
	out, err := cmd.CombinedOutput()
	status := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run %s: %v", hpLegacyAuditRel, err)
		}
		status = ee.ExitCode()
	}

	if status != 3 {
		t.Errorf("a refusing round: exit status %d, want 3 (the guard's refusal) — the drainer must propagate it\n%s",
			status, out)
	}
	if got := strings.Count(string(out), "起 worker"); got != 1 {
		t.Errorf("the drainer started %d rounds, want 1 — the loop MUST stop at the first refused round "+
			"(silent continuation is the defect)\n%s", got, out)
	}
	for _, want := range []string{hpFailureMarker, "$" + hpRefusalKnob + "=" + glmRefusalExitText, "no further round was started"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the refusal line does not contain %q\n%s", want, out)
		}
	}
}

// TestHpWorkerLaunchersDryRunStartsNothing is the acceptance seam: `--dry-run`
// prints the delegation it would perform and exits 0, without a launcher, a
// count call or a worker.
func TestHpWorkerLaunchersDryRunStartsNothing(t *testing.T) {
	for _, tc := range hpScripts {
		t.Run(tc.rel, func(t *testing.T) {
			sandbox := t.TempDir()
			// --dry-run reads what WOULD happen; it is still running the script,
			// so it needs a watched context like any other run (the retirement
			// gate grants no exemption to it).
			_, watched := openHpPTY(t)
			out, status, logBody := hpRunStarterStdin(t, tc.rel, sandbox, []string{hpDryRunFlag}, watched,
				"HOME="+sandbox,
				"GOPI_HOME="+sandbox,
				// NOTE: no launcher override — the dry run must work with the
				// DEFAULT launcher path, which is exactly the acceptance claim
				// (/home/user/bin/gopi-worker-up.sh present, dry run real) and must not
				// require that file to exist yet.
				tc.logKnob+"="+filepath.Join(sandbox, "log"),
				hpNightStartKnob+"=0", hpNightEndKnob+"=24")
			if status != 0 {
				t.Errorf("--dry-run exited %d, want 0 (it launches nothing, so a missing launcher is not a failure "+
					"of the dry run)\noutput:\n%s", status, out)
			}
			if !strings.Contains(out, workerUpScriptName) {
				t.Errorf("--dry-run output does not name the delegation target (%s) — the operator must see WHAT "+
					"would run\noutput:\n%s", workerUpScriptName, out)
			}
			if strings.Contains(logBody, hpFailureMarker) {
				t.Errorf("--dry-run wrote a failure to the log:\n%s", logBody)
			}
		})
	}
}

// --- the RETIREMENT contract (2026-09-26) ---------------------------------
//
// The two starters above used to be a SAFETY NET: a schedule (cron `*/10` on
// HP) asked the single launcher for a worker whenever none was alive. The
// operator retired it — "这个 cronjob 必须终止，我们不能依赖这种系统来做,
// 它掩盖了问题" — because the net MASKED a worker-death loop: the log recorded
// 1,214 delegations, and the zero counts between them were honest (the pids are
// gone). A cron line that replaces a dying worker every ten minutes converts
// "workers keep dying" into "there is always a worker", which is why the
// underlying failure was never diagnosed.
//
// So the scripts now have to REFUSE an automatic caller (Rule 15: loudly, with
// a non-zero status and a message that names the ruling and the reason) while
// an OPERATOR may still run one by hand in a terminal. These tests pin BOTH
// halves, and they pin them BEHAVIOURALLY — a script that merely contains the
// right words still fails.

// hpSandbox is the fixture a retirement probe drives a starter with: a private
// home, a repo whose bin/gopi reports pending work, and a stub launcher that
// records every invocation. Nothing here touches the operator's crontab, a
// worker, or the real launcher.
type hpSandbox struct {
	dir      string
	logPath  string
	repo     string
	stub     string
	calls    string
	launcher string
	base     []string
}

func newHpSandbox(t *testing.T, logKnob string) *hpSandbox {
	t.Helper()
	sandbox := t.TempDir()
	logPath := filepath.Join(sandbox, "log")

	// A repo whose bin/gopi reports pending work: the pending gate must be able
	// to let a delegation through without a real queue.
	repo := filepath.Join(sandbox, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir repo bin: %v", err)
	}
	hpWriteStub(t, filepath.Join(repo, "bin", procname.NameGopi), "#!/bin/bash\necho '[pending] stub task'\n")

	// The stub launcher: --count reports a liveness number, a launch call takes
	// its status from STUB_LAUNCH_EXIT, and EVERY invocation is recorded — so
	// "no launcher was called" is an observation, not a claim.
	calls := filepath.Join(sandbox, "calls")
	stub := hpWriteStub(t, filepath.Join(sandbox, workerUpScriptName), `#!/bin/bash
printf '%s\n' "$*" >> "`+calls+`"
if [ "${1:-}" = "`+hpCountFlag+`" ]; then
  echo "live gopi workers: ${STUB_WORKERS:-0} (pids: none)"
  echo "other gopi processes: 0 (not workers — daemons, the merger, one-shot run children)"
  exit 0
fi
echo "stub launcher: launching $1 worker(s)"
exit "${STUB_LAUNCH_EXIT:-0}"
`)
	// The DEFAULT launcher path (no override knob) points INSIDE the sandbox:
	// a refusal must never be able to reach the operator's real /home/user/bin copy,
	// whether or not this sandbox's stub is passed explicitly.
	if err := os.MkdirAll(filepath.Join(sandbox, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir sandbox bin: %v", err)
	}
	defaultLauncher := filepath.Join(sandbox, "bin", workerUpScriptName)
	writeExecutable(t, defaultLauncher, `#!/bin/bash
printf 'DEFAULT-LAUNCHER-CALLED %s\n' "$*" >> "`+calls+`"
exit 0
`)
	// The default path the refusal probe checks: the sandbox's OWN bin dir, at
	// the launcher's real name — so "the launcher was not called" is an
	// observation about a file that WOULD have been reached. (The probes pass
	// $GOPI_WORKER_LAUNCHER explicitly; this copy is what the DEFAULT resolves
	// to when the check's HOME is the sandbox.)
	return &hpSandbox{
		dir:      sandbox,
		logPath:  logPath,
		repo:     repo,
		stub:     stub,
		calls:    calls,
		launcher: defaultLauncher,
		base: []string{
			"HOME=" + sandbox,
			"GOPI_HOME=" + sandbox,
			hpLauncherKnob + "=" + stub,
			hpRepoKnob + "=" + repo,
			logKnob + "=" + logPath,
			hpNightStartKnob + "=0",
			hpNightEndKnob + "=24", // window always open for the nightsafe probe
		},
	}
}

// callsSeen returns the recorded launcher invocations (empty when the stub was
// never reached).
func (s *hpSandbox) callsSeen(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(s.calls)
	if err != nil {
		return ""
	}
	return string(raw)
}

// TestHpWorkerLaunchersRefuseAnAutomaticCaller is the behaviour gate for the
// retirement: the SAME invocation, once with a scheduler's shape and once with
// an operator's terminal, must take two different paths.
func TestHpWorkerLaunchersRefuseAnAutomaticCaller(t *testing.T) {
	for _, tc := range hpScripts {
		t.Run(tc.rel, func(t *testing.T) {
			// (1) NO TERMINAL — the shape cron runs a script in (verified live:
			// cron hands the child no terminal). Must refuse, loudly, naming the
			// ruling and the reason, and must not reach the launcher.
			sb := newHpSandbox(t, tc.logKnob)
			out, status, logBody := hpRunStarter(t, tc.rel, sb.dir, nil, sb.base...)
			if status != 78 {
				t.Errorf("automatic context: exit status %d, want %s (the retirement status) — an automatic "+
					"caller must be REFUSED, and the status must be distinguishable from the launcher's own "+
					"refusal\noutput:\n%s", status, hpRetiredExitText, out)
			}
			for _, want := range append([]string{hpRetiredMarker, hpNoWorkerStatement}, hpRulingTexts...) {
				if !strings.Contains(out, want) {
					t.Errorf("automatic context: the refusal does not carry %q — the ruling and the reason must be "+
						"pasted where an operator reads them\noutput:\n%s", want, out)
				}
			}
			if got := sb.callsSeen(t); got != "" {
				t.Errorf("automatic context: the launcher was CALLED (%q) — the refusal must happen before any "+
					"delegation; that delegation IS the automated net the operator retired", got)
			}
			if !strings.Contains(logBody, hpRetiredMarker) {
				t.Errorf("automatic context: the refusal left no record in the log\nlog:\n%s", logBody)
			}

			// (2) WATCHED — an operator's terminal on stdin (a real pty): the
			// same file must still do its job. This half is what keeps the
			// refusal from being a blanket "this script is dead".
			_, watched := openHpPTY(t)
			if err := os.Remove(sb.calls); err != nil && !os.IsNotExist(err) {
				t.Fatalf("reset calls: %v", err)
			}
			out, status, _ = hpRunStarterStdin(t, tc.rel, sb.dir, nil, watched, sb.base...)
			if status != 0 {
				t.Errorf("watched run: exit status %d, want 0 — an operator in a terminal may still run this by "+
					"hand; the gate must not have swallowed the whole script\noutput:\n%s", status, out)
			}
			if strings.Contains(out, hpRetiredMarker) {
				t.Errorf("watched run: a REFUSED line was emitted although a terminal was attached — the gate "+
					"must tell the two callers apart\noutput:\n%s", out)
			}
			if got := sb.callsSeen(t); !strings.Contains(got, hpCountFlag) {
				t.Errorf("watched run: the launcher was never asked for liveness (%q) — a watched run keeps its "+
					"old job (LOOK BEFORE ASKING, then delegate at most once)", got)
			}

			// (3) A SCHEDULER-STYLE WRAPPER — an operator's shell script that
			// CRON runs (cron's `SHELL=/bin/sh` and a wrapper script are exactly
			// how a caller would try to look watched). The wrapper holds the pty,
			// so the terminal arm sees one; only the scheduler-ancestry arm can
			// catch it, which is why the wrapper renames its OWN process to
			// `cron` (the kernel's comm: /proc/self/comm is writable, so the
			// ancestor is real for every reader, exactly as cron's child is).
			_, wrapped := openHpPTY(t)
			wrapper := filepath.Join(sb.dir, "scheduler-wrapper.sh")
			writeExecutable(t, wrapper, `#!/bin/bash
# An automatic caller wearing an operator's terminal: the comm below is what a
# cron child's parent has, and /proc/self/comm is writable by its owner. The
# script is run as a CHILD (no exec): exec would replace this process image
# with bash's and reset the name we just set.
printf 'cron\n' > /proc/self/comm
bash "$TARGET" "$@"
`)
			if err := os.Remove(sb.calls); err != nil && !os.IsNotExist(err) {
				t.Fatalf("reset calls: %v", err)
			}
			out, status, _ = hpRunCommandStdin(t, wrapper, sb.dir, nil, wrapped, append(sb.base,
				"TARGET="+filepath.Join(repoRootForProcnameGate(t), filepath.FromSlash(tc.rel)))...)
			if status != 78 {
				t.Errorf("scheduler wrapper with a pty: exit status %d, want %s — the ancestry arm must catch a "+
					"scheduled caller even when a terminal is present, otherwise the gate is trivially bypassed "+
					"by running the script from a wrapper\noutput:\n%s", status, hpRetiredExitText, out)
			}
			if got := sb.callsSeen(t); got != "" {
				t.Errorf("scheduler wrapper with a pty: the launcher was CALLED (%q) — the wrapper path is the "+
					"bypass this arm exists to close", got)
			}
		})
	}
}

// TestHpWorkerLaunchersRefusalSurvivesASchedulerEnvironment pins the other half
// of "not forgeable from a crontab line": cron's own environment (`SHELL=/bin/sh`
// and a bare PATH) must not change the verdict, and neither may a GIT_* or any
// other variable the environment can carry. The gate reads the PROCESS TABLE
// and fd 0, so nothing in the environment can open it.
func TestHpWorkerLaunchersRefusalSurvivesASchedulerEnvironment(t *testing.T) {
	for _, tc := range hpScripts {
		t.Run(tc.rel, func(t *testing.T) {
			sb := newHpSandbox(t, tc.logKnob)
			out, status, _ := hpRunStarter(t, tc.rel, sb.dir, nil, append(sb.base,
				"SHELL=/bin/sh",
				"PATH=/usr/bin:/bin",
				"USER=nick",
				"LOGNAME=nick",
				"GOPI_ALLOW_AUTOSTART=1", // a would-be escape hatch: it must mean NOTHING
				"GOPI_WORKER_FORCE=1",
			)...)
			if status != 78 {
				t.Errorf("cron-shaped environment: exit status %d, want %s — no environment variable may open the "+
					"gate (an escape hatch is the ruling reversed); inertness of GOPI_ALLOW_AUTOSTART is the point "+
					"\noutput:\n%s", status, hpRetiredExitText, out)
			}
			if got := sb.callsSeen(t); got != "" {
				t.Errorf("cron-shaped environment: the launcher was CALLED (%q)", got)
			}
		})
	}
}

// TestHpWorkerLaunchersOfferNoEscapeHatch guards the SHAPE of the refusal: a
// knob that switches the gate off would be the ruling reversed, so the gate's
// arms are reached from the process table and fd 0 only, and no environment
// variable is consulted on the refusal path.
func TestHpWorkerLaunchersOfferNoEscapeHatch(t *testing.T) {
	for _, tc := range hpScripts {
		t.Run(tc.rel, func(t *testing.T) {
			body := readHpScript(t, tc.rel)
			code := stripShellComment(body)
			// A message NAMING a hatch is the point ("there is no --force, no
			// GOPI_ALLOW_AUTOSTART"): a message cannot honour one. So the ban is
			// checked line by line, skipping the lines that only PRINT.
			for i, line := range strings.Split(code, "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "echo ") || strings.HasPrefix(trimmed, "log ") || strings.HasPrefix(trimmed, "printf ") {
					continue
				}
				for _, banned := range []string{"GOPI_ALLOW", "ALLOW_AUTOSTART", "--force"} {
					if strings.Contains(trimmed, banned) {
						t.Errorf("%s line %d honours %q — an escape hatch (a flag or an env knob that switches "+
							"the retirement gate off) is the operator's ruling reversed; a refusal message may "+
							"NAME it, the code may not read it", tc.rel, i+1, banned)
					}
				}
			}
			// The arms themselves decide from the kernel's process table and fd
			// 0, never from the environment (a crontab line can set any
			// variable it likes). Checked over the gate region only — the file's
			// own knobs (launcher path, log path) are declared outside it.
			gateStart := strings.Index(code, "refuse_retired() {")
			lastArm := strings.LastIndex(code, "refuse_retired \"")
			if gateStart < 0 || lastArm < gateStart {
				t.Fatalf("%s does not carry the retirement gate (%q … %q) — the refusal must be a path the "+
					"script takes", tc.rel, "refuse_retired() {", "refuse_retired \"")
			}
			gate := code[gateStart:lastArm]
			if strings.Contains(gate, "${GOPI_") {
				t.Errorf("%s reads an environment knob inside the gate — the arm that decides must be the kernel's "+
					"process table and fd 0, because a crontab line can set any variable it likes", tc.rel)
			}
			// The arms themselves: the scheduler is read from the kernel's
			// process table, the watched/unwatched half from fd 0.
			if !strings.Contains(code, "[ -t 0 ]") {
				t.Errorf("%s does not probe a terminal on stdin ([ -t 0 ]) — the watched/unwatched distinction is "+
					"the gate's second arm, and it must be a real fd probe, not a variable", tc.rel)
			}
			if !strings.Contains(code, "SCHEDULER_COMM_MARKERS") {
				t.Errorf("%s does not carry the scheduler comm markers — the ancestry arm must name what it looks "+
					"for in ONE place (Rule 1b/13)", tc.rel)
			}
			if !strings.Contains(code, "$RETIRED_EXIT_CODE") {
				t.Errorf("%s does not exit with its named retirement status (want \"$RETIRED_EXIT_CODE\") — a "+
					"magic status is the silent-failure class Rule 15 removes", tc.rel)
			}
		})
	}
}

// TestHpWorkerLaunchersRecordTheRulingAndTheMeasurement keeps the WHY in the
// file, where the next operator reads it (brief §3): the ruling verbatim, the
// measured delegation count, and the consequence (the mask) — in the HEADER, so
// it is read before the code is.
func TestHpWorkerLaunchersRecordTheRulingAndTheMeasurement(t *testing.T) {
	for _, tc := range hpScripts {
		t.Run(tc.rel, func(t *testing.T) {
			body := readHpScript(t, tc.rel)
			header := body
			if idx := strings.Index(body, "\nset -uo pipefail"); idx >= 0 {
				header = body[:idx]
			}
			for _, want := range []string{hpRetiredOn, hpRulingQuote, hpDelegationMeasurement, hpMaskStatement} {
				if !strings.Contains(header, want) {
					t.Errorf("%s header does not record %q — the ruling, the measurement and the consequence must "+
						"be read by the next person BEFORE they run or re-install it", tc.rel, want)
				}
			}
		})
	}
}

// TestHpWorkerLaunchersRefuseAnAutomaticContextStatically is the SHAPE half of
// the retirement gate (the behaviour half is TestHpWorkerLaunchersRefuseAnAutomaticCaller):
// a script that stops consulting the context at all would otherwise pass by
// simply never refusing — the test above would catch that, and this one names
// the missing seam directly.
func TestHpWorkerLaunchersRefuseAnAutomaticContextStatically(t *testing.T) {
	for _, tc := range hpScripts {
		t.Run(tc.rel, func(t *testing.T) {
			code := stripShellComment(readHpScript(t, tc.rel))
			for _, want := range []string{"refuse_retired", "scheduler_ancestor", "watched_terminal", hpRetiredMarker} {
				if !strings.Contains(code, want) {
					t.Errorf("%s does not carry the retirement seam %q in executable code — the refusal must be a "+
						"path the script takes, not prose about a path", tc.rel, want)
				}
			}
		})
	}
}

// The retirement's own vocabulary, pinned across code and test (Rule 1b/14: a
// shared value has ONE definition in each layer, and these two must agree).
const (
	// hpRetiredOn is the date the operator retired the scheduled starter.
	hpRetiredOn = "2026-09-26"
	// hpRulingQuote is a substring of the ruling every header must carry
	// verbatim: the ruling is a quotation, and a paraphrase would let the next
	// reader think it was somebody's preference.
	hpRulingQuote = "cronjob 必须终止"
	// hpDelegationMeasurement is the measured count that produced the ruling.
	hpDelegationMeasurement = "1,214"
	// hpMaskStatement is the consequence the header must state: the net MASKED
	// the failure instead of exposing it.
	hpMaskStatement = "MASKED"
)
