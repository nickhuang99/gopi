package testutil

// worker_up_liveness_test.go — THE LAUNCHER'S OUTCOME ANNOUNCEMENT
// (epic worker-spawn-doors, GAP 2 / Decision 2; decision record
// devdoc/worker-display-ownership.md).
//
// THE MEASURED DEFECT THIS PINS (2026-09-14, this host, real binary, contained
// sandbox HOME/GOPI_HOME):
//
//	$ scripts/gopi-worker-up.sh 1            # from an AGENT's shell
//	requested worker 1: pid 1234567 …        # <- pre-fix: "started worker 1"
//	live gopi workers: 0 (pids: none)
//	… exit status 0
//
// The child was REFUSED by the agent-spawn guard (exit 3) — no worker existed
// — and the launcher still printed its start line and exited 0. A launcher
// that starts NOTHING printed what a launcher that starts N workers prints:
// the operator reads "started" and believes a fleet is up (Rule 15, no silent
// failure at the launcher's own seam).
//
// WHAT IS PINNED HERE, all over a SYNTHETIC procfs fixture so nothing on this
// host is started, counted or killed (WORKER-OWNERSHIP: OPERATOR-ONLY, Rule 24 —
// the test never touches a live fleet):
//
//  1. TestWorkerUpLauncherVerdictDistinguishesRefusalFromSuccess — the verdict
//     is derived from the argv-shape COUNT, so an unchanged fixture means "no
//     live worker" and the launcher must say so and exit NON-ZERO.
//  2. TestWorkerUpLauncherVerdictSeesTheWorkersItStarted — the same predicate
//     must report a produced worker (the fixture gains one between the two
//     count reads), so the verdict is not merely "always complain".
//  3. TestWorkerUpLauncherNeverClaimsStartedBeforeTheCount — Rule 15 applied
//     to the WORD ITSELF: the per-worker line must not assert a start it
//     cannot have verified yet.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/procname"
)

const (
	// workerUpLiveLinePrefix is the operator-facing count line the verdict
	// loop reads (its number is whatever the fixture holds).
	workerUpLiveLinePrefix = "live " + procname.NameGopi + " workers:"
	// workerUpVerdictOK is the substring of the confirmed-launch verdict.
	workerUpVerdictOK = "launch verdict:"
	// workerUpVerdictUnconfirmed marks the refused/absent launch.
	workerUpVerdictUnconfirmed = "UNCONFIRMED"
	// workerUpStartedClaim is the line shape the pre-fix launcher printed for a
	// child it had not verified. Its absence is asserted (tests 3).
	workerUpStartedClaim = "started worker"
	// workerUpRequestedClaim is the honest form of the same line.
	workerUpRequestedClaim = "requested worker"
)

// launcherArmedFixture is the synthetic-procfs harness: a fixture directory
// plus the env an armed launcher needs to run its post-launch verdict without
// touching this host's fleet. `arm` is flipped ON only after the launch
// requests are issued, which is exactly the real sequence (the launcher starts
// its children, then counts).
type launcherArmedFixture struct {
	t     *testing.T
	root  string // the synthetic procfs root the script scans
	arm   string // path of the ARM marker file (absent ⇒ no worker yet)
	repo  string // a fake repo holding bin/<name> (so the launch path runs)
	env   []string
	start int // how many children each launch run "requests"
}

// newLauncherArmedFixture builds the harness. The script is driven through its
// own seams (GOPI_PROC_ROOT, GOPI_PROC_NAME, the verb names) so no /proc
// process and no real worker is involved.
func newLauncherArmedFixture(t *testing.T, repoRoot string) *launcherArmedFixture {
	t.Helper()
	root := t.TempDir()
	f := &launcherArmedFixture{t: t, root: root, start: 1}

	// A fake repo whose bin/<PROC_NAME> exists and is executable: the script
	// requires it before the launch loop, and this stub NEVER becomes a worker
	// (it is not named like one and is not a worker verb) — it is the "child
	// that refuses" of the measured defect, in miniature.
	repo := t.TempDir()
	f.repo = repo
	binDir := filepath.Join(repo, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("create fake repo bin: %v", err)
	}
	// The child writes nothing and exits 3 — the guard's refusal status. It is
	// deliberately NOT a worker: the count must stay 0 whatever it does.
	stub := "#!/usr/bin/env bash\nexit " + fmt.Sprint(guardRefusalExit) + "\n"
	if err := os.WriteFile(filepath.Join(binDir, procname.NameGopi), []byte(stub), 0o755); err != nil {
		t.Fatalf("install the refusing child stub: %v", err)
	}

	f.arm = filepath.Join(root, "armed")
	writeEmptyProcRoot(t, root)
	f.env = append(os.Environ(),
		procname.EnvName+"="+procname.NameGopi,
		procname.EnvVerbWorker+"="+procname.VerbWorker,
		procname.EnvVerbTask+"="+procname.VerbTask,
		procname.EnvProcRoot+"="+root,
		"GOPI_WORKER_REPO="+repo,
		"GOPI_WORKER_N=1",
		"GOPI_WORKER_OUTPUT_ROOT="+filepath.Join(root, "output"),
	)
	return f
}

// guardRefusalExit is the agent-spawn guard's refusal status — the number the
// measured refusal exits with. It is a named constant because the test's stub
// child and the assertion both speak it.
const guardRefusalExit = 3

// writeEmptyProcRoot creates a procfs root with no processes at all (but at
// least one pid-shaped directory, so the counter's "broken procfs root" guard
// does not fire — that guard is about an EMPTY SCAN, a different condition).
func writeEmptyProcRoot(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the proc root's baseline entry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comm"), []byte("systemd\n"), 0o644); err != nil {
		t.Fatalf("write baseline comm: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte("/sbin/systemd\x00"), 0o644); err != nil {
		t.Fatalf("write baseline cmdline: %v", err)
	}
}

// armWorkers makes the fixture hold exactly n worker-shaped processes — the
// "this launch produced workers" event, injected between the launcher's launch
// requests and its count.
func (f *launcherArmedFixture) armWorkers(n int) {
	f.t.Helper()
	for i := 0; i < n; i++ {
		pid := fmt.Sprintf("9%03d", i)
		dir := filepath.Join(f.root, pid)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			f.t.Fatalf("create armed worker %s: %v", pid, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "comm"), []byte(procname.NameGopi+"\n"), 0o644); err != nil {
			f.t.Fatalf("write armed worker comm: %v", err)
		}
		cmd := procname.NameGopi + "\x00" + procname.VerbWorker + "\x00"
		if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(cmd), 0o644); err != nil {
			f.t.Fatalf("write armed worker cmdline: %v", err)
		}
	}
}

// run executes the launcher over the fixture and returns its exit code plus
// the combined output. A non-zero exit is DATA here (the refusal verdict), not
// a harness failure — so exec's error is inspected, never fatal.
func (f *launcherArmedFixture) run(script string, args ...string) (int, string) {
	f.t.Helper()
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = f.env
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !asExitError(err, &ee) {
			f.t.Fatalf("run %s: %v\n%s", filepath.Base(script), err, out)
		}
		code = ee.ExitCode()
	}
	return code, string(out)
}

// asExitError is errors.As spelled without importing errors twice in the
// helpers above (the fixture only ever needs this one shape).
func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

// TestWorkerUpLauncherVerdictDistinguishesRefusalFromSuccess is the minimal
// repro of the measured silent failure. The fixture does NOT arm a worker, so
// the launch produced none: the launcher must (a) still print the count line,
// (b) print an UNCONFIRMED verdict that names the refusal cause, and (c) exit
// NON-ZERO. Pre-fix it exited 0 after claiming a start.
func TestWorkerUpLauncherVerdictDistinguishesRefusalFromSuccess(t *testing.T) {
	script := filepath.Join(repoRootForProcnameGate(t), filepath.FromSlash(workerUpScriptRel))
	f := newLauncherArmedFixture(t, repoRootForProcnameGate(t))

	code, out := f.run(script)
	if !strings.Contains(out, workerUpLiveLinePrefix) {
		t.Fatalf("the launcher printed no count line, so the verdict cannot be derived:\n%s", out)
	}
	if !strings.Contains(out, workerUpVerdictUnconfirmed) {
		t.Errorf("a launch that produced NO worker printed no UNCONFIRMED verdict — this is exactly the silent "+
			"failure the verdict exists to close (a refused child, guard exit %d, used to exit 0):\n%s", guardRefusalExit, out)
	}
	if code == 0 {
		t.Errorf("exit status = 0 with ZERO live %s workers — a launch whose children were all refused must not "+
			"report success (measured defect: guard refusal exit %d, launcher exit 0)\n%s", procname.NameGopi, guardRefusalExit, out)
	}
	if strings.Contains(out, workerUpStartedClaim) {
		t.Errorf("the launcher still claims %q for a child it cannot have verified (Rule 15):\n%s", workerUpStartedClaim, out)
	}
}

// TestWorkerUpLauncherVerdictSeesTheWorkersItStarted is the other direction,
// so the verdict is not a constant complaint: with the SAME fixture armed (a
// worker-shaped process exists), the launcher must confirm the launch and exit
// 0. The arm happens before the run, which is the "this launch produced a
// worker" world; the refused world is the test above.
func TestWorkerUpLauncherVerdictSeesTheWorkersItStarted(t *testing.T) {
	script := filepath.Join(repoRootForProcnameGate(t), filepath.FromSlash(workerUpScriptRel))
	f := newLauncherArmedFixture(t, repoRootForProcnameGate(t))
	f.armWorkers(2)

	code, out := f.run(script)
	if !strings.Contains(out, workerUpVerdictOK) {
		t.Errorf("a launch with live workers printed no confirmed-launch verdict:\n%s", out)
	}
	if strings.Contains(out, workerUpVerdictUnconfirmed) {
		t.Errorf("a launch with live workers was reported UNCONFIRMED — the verdict must follow the argv-shape "+
			"count, not a fixed answer:\n%s", out)
	}
	if code != 0 {
		t.Errorf("exit status = %d with live %s workers, want 0\n%s", code, procname.NameGopi, out)
	}
}

// TestWorkerUpLauncherNeverClaimsStartedBeforeTheCount pins the WORD: the
// per-worker line is printed inside the launch loop, before any count has run,
// so it may only describe a REQUEST. A "started worker" claim there is a claim
// the launcher cannot support — the exact wording the measured defect printed
// for a refused child.
func TestWorkerUpLauncherNeverClaimsStartedBeforeTheCount(t *testing.T) {
	script := filepath.Join(repoRootForProcnameGate(t), filepath.FromSlash(workerUpScriptRel))
	f := newLauncherArmedFixture(t, repoRootForProcnameGate(t))

	_, out := f.run(script)
	if strings.Contains(out, workerUpStartedClaim) {
		t.Errorf("the launcher prints %q before it has counted anything — the honest line is %q, because a child "+
			"may refuse (guard exit %d) after the request:\n%s", workerUpStartedClaim, workerUpRequestedClaim, guardRefusalExit, out)
	}
	if !strings.Contains(out, workerUpRequestedClaim) {
		t.Errorf("the launcher prints no %q line — the operator-facing launch line disappeared entirely:\n%s", workerUpRequestedClaim, out)
	}
}
