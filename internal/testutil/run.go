package testutil

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/worktreeroots"
)

// Subprocess timeouts (Rule 1: named constants, no magic numbers).
const (
	// subprocessTimeout bounds one whole `gopi` run. The loop's own
	// GOPI_TIMEOUT_SEC default (300s) is far longer than any mock-backed run
	// needs; this is the test-side safety net against a hang.
	subprocessTimeout = 180 * time.Second
	// procPollInterval is how often WaitFor polls the process output.
	procPollInterval = 25 * time.Millisecond
	// procStartTimeout bounds waiting for a server to print its address.
	procStartTimeout = 30 * time.Second
	// SuiteBinEnv names the binary EVERY e2e case in a suite drives. The
	// suite entry point (PinSuiteForTestMain) builds it once from the tree
	// under test and pins it under this name; a runner that wants to hand the
	// suite its own build (scripts/test-budget.sh) sets it before the run. See
	// pinSuiteTestedBin for why the tree's bin/gopi is NOT a valid source.
	//
	// It is EXPORTED (Rule 14: one definition, referenced by every consumer)
	// because the pin NAMES are part of the contract this package owns: a
	// fixture that reads a pin channel, and gopibin_resolver_gate_test.go,
	// which must RECOGNISE the sanctioned spelling to tell it apart from a
	// hand-rolled resolver, both speak these names — and a second, re-typed
	// name is a name that drifts away from the one GopiBin reads (Rule 13).
	SuiteBinEnv = "GOPI_TEST_BIN"
	// BinaryPinEnv is a single CASE's deliberate override — a test that built a
	// binary itself stamps the absolute path here (see cmd/agent's
	// sweepZombiesRequireBin / reportSeamBinary). It outranks the suite pin.
	BinaryPinEnv = "GOPI_BIN"
	// BinDirName is the directory, inside a repo root, the declared build
	// writes, and BinFileName the artifact's name. Spelled here, once, so a
	// fixture that must CREATE a repo's own bin/gopi (the build contract's
	// subject) does so through the same definitions the resolver reads.
	BinDirName  = "bin"
	BinFileName = "gopi"
	// suiteBinDirPrefix names the per-run build directory under the suite's
	// pinned scratch root (never a fixed path: a predictable name in a writable
	// directory is a plant-a-symlink hazard, and the scratch root's own GC owns
	// the lifecycle).
	suiteBinDirPrefix = "suite-tested-bin-"
)

// GopiBin returns the path to the gopi binary EVERY e2e case in this suite
// drives. Resolution order, and why it is ordered this way:
//
//  1. $GOPI_BIN — a single CASE's own build (sweepZombiesRequireBin,
//     reportSeamBinary stamp the absolute path of the binary they just built).
//     An explicitly named binary that does not exist stays FATAL, never a
//     skip (Rule 15: a silent skip of an e2e case is a green that proves
//     nothing).
//  2. $GOPI_TEST_BIN — the suite's own pin: PinSuiteForTestMain builds the
//     binary once from THE TREE UNDER TEST under the scratch root, and a
//     runner that hands the suite its own build (scripts/test-budget.sh) sets
//     it before the run. This is the resolution EVERY e2e case must get.
//  3. <module root>/bin/gopi — the one-click entry (scripts/e2e_smoke.sh)
//     builds exactly this from this tree, which is what makes it correct
//     THERE, and only there.
//
// (3) is deliberately LAST and never a silent first choice: a worktree's
// bin/gopi is a STALE file whose revision an arbitrary other process in this
// shared checkout decides. Measured 2026-10-03 (task 01a1023c): the SAME
// revision of this test went GREEN standalone and inside one two-package
// budget run, and RED inside the next one, because the tested-binary path
// walked up from the test's own CWD to a run-garden worktree whose bin/gopi
// predated commit 1d901244 — so the test observed an OLD production binary's
// ground-stamp REFUSAL and reported it as a flake of the tree under test. A
// suite that silently drives that file can never be trusted; the pin's job is
// to make the tested revision a property of THIS run.
func GopiBin(t *testing.T) string {
	t.Helper()
	r := resolveGopiBin(t)
	if r.fatal != "" {
		t.Fatal(r.fatal)
	}
	if r.path == "" {
		t.Skip(r.skip)
	}
	return r.path
}

// gopiBinResolution is the ONE precedence decision behind both faces below.
// Splitting the DECISION (resolveGopiBin) from the caller's policy (skip,
// fatal, or "keep going") is what lets a census probe resolve through the same
// rule without adopting GopiBin's skip policy: it needs to distinguish "there
// is nothing to drive" from "the test would have passed anyway", and reports
// that direction UNVERIFIED rather than green or skipped — a silent skip of an
// e2e case is a green that proves nothing (Rule 15).
type gopiBinResolution struct {
	// path is the resolved binary, empty when nothing resolved.
	path string
	// fatal names why a pin that WAS given cannot be used (Rule 15: a named
	// binary that does not exist is never quietly substituted).
	fatal string
	// skip is the reason rendered when nothing resolved at all.
	skip string
}

// resolveGopiBin applies the precedence ONCE, for both faces:
//
//  1. $BinaryPinEnv — a single CASE's own build.
//  2. $SuiteBinEnv — the suite's pin (built from the tree under test by
//     PinSuiteForTestMain, or handed to the run by scripts/test-budget.sh).
//  3. the binary this process already pinned in memory.
//  4. a build from the tree under test, pinned for the rest of the suite.
//  5. <module root>/bin/gopi — correct only for the one-click entry
//     (scripts/e2e_smoke.sh), which is the one thing that builds it.
func resolveGopiBin(t *testing.T) gopiBinResolution {
	t.Helper()
	if p := os.Getenv(BinaryPinEnv); p != "" {
		if _, err := os.Stat(p); err != nil {
			return gopiBinResolution{fatal: fmt.Sprintf("%s=%s is set but not usable: %v", BinaryPinEnv, p, err)}
		}
		return gopiBinResolution{path: p}
	}
	if p := os.Getenv(SuiteBinEnv); p != "" {
		if _, err := os.Stat(p); err != nil {
			return gopiBinResolution{fatal: fmt.Sprintf("%s=%s is set but not usable: %v (the suite pin must point at the binary it built)",
				SuiteBinEnv, p, err)}
		}
		return gopiBinResolution{path: p}
	}
	if p := suitePinnedTestedBin(); p != "" {
		return gopiBinResolution{path: p}
	}
	if p := pinSuiteTestedBin(); p != "" {
		return gopiBinResolution{path: p}
	}
	return gopiBinResolution{skip: skipReasonForTreeBin()}
}

// GopiBinOptional resolves the tested binary WITHOUT owning the caller's
// policy, through the same ONE precedence as GopiBin. It returns ok=false with
// the reason when nothing resolved (the caller reports its own direction as
// UNVERIFIED and continues), and FAILS the case when a pin was given but is
// unusable — the same no-silent-substitution rule as GopiBin.
func GopiBinOptional(t *testing.T) (path string, ok bool, why string) {
	t.Helper()
	r := resolveGopiBin(t)
	if r.fatal != "" {
		t.Fatal(r.fatal)
	}
	return r.path, r.path != "", r.skip
}

// skipReasonForTreeBin renders why the last resort failed.
func skipReasonForTreeBin() string {
	root := moduleRoot()
	if root == "" {
		return "gopi binary not found and no test binary pinned — run scripts/e2e_smoke.sh first (it builds bin/gopi)"
	}
	p := filepath.Join(root, BinDirName, BinFileName)
	if _, err := os.Stat(p); err != nil {
		return fmt.Sprintf("gopi binary not found at %s — run scripts/e2e_smoke.sh first", p)
	}
	return ""
}

var (
	suiteTestedBinOnce sync.Once
	suiteTestedBinPath string
)

// suitePinnedTestedBin reports the binary this process already pinned, if any.
func suitePinnedTestedBin() string { return suiteTestedBinPath }

// pinSuiteTestedBin establishes the ONE binary this suite drives, at most once
// per test binary (sync.Once), and publishes it so every case — and every CHILD
// process the suite spawns — resolves the same build.
//
// The failure it closes (measured 2026-10-03, task 01a1023c): resolution used
// to fall through to <module root>/bin/gopi, where module root was found by
// walking up from the test's CWD. In a shared checkout that reachable file is
// a run-garden worktree's bin/gopi, whose revision another process owns: the
// SAME revision of the test went green standalone and inside one two-package
// budget run, then RED inside the next one, because that file predated commit
// 1d901244. The test then reported an OLD binary's production refusal as a
// flake of the tree under test. Building from the tree under test makes the
// tested revision a property of THIS run.
func pinSuiteTestedBin() string {
	suiteTestedBinOnce.Do(func() {
		suiteTestedBinPath = resolveSuiteTestedBin()
	})
	return suiteTestedBinPath
}

func resolveSuiteTestedBin() string {
	// a) The RUNNER's build, built once for the whole run and handed to every
	// package in the run — the property that makes a multi-package budget run
	// verify ONE revision (scripts/test-budget.sh sets it).
	if p := os.Getenv(SuiteBinEnv); p != "" {
		_, err := os.Stat(p)
		if err == nil {
			return p
		}
		fmt.Fprintf(os.Stderr, "testutil: %s=%s is not usable: %v — building from the tree under test instead\n",
			SuiteBinEnv, p, err)
	}
	p := buildSuiteTestedBinFromTree()
	if p == "" {
		return ""
	}
	// Publish for CHILD processes: a suite that re-executes gopi must drive the
	// same revision it was verified against, not a second build of its own.
	if err := os.Setenv(SuiteBinEnv, p); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: %s not exported (%v) — child processes may resolve their own binary\n",
			SuiteBinEnv, err)
	}
	return p
}

// buildSuiteTestedBinFromTree builds ./cmd/agent/ from the tree under test into
// a per-run directory under the suite's pinned scratch root (Rule 34: never a
// predictable name in a shared writable directory, and never the tree's own
// bin/ — a build artifact inside the worktree is exactly the file whose
// revision is not this run's).
//
// An EMPTY return means "no build": the caller then resolves exactly as it did
// before, and the failure is REPORTED rather than silently answered with a
// binary of unknown revision (Rule 15: a silent substitution is the defect this
// function exists to remove).
func buildSuiteTestedBinFromTree() string {
	root := moduleRoot()
	if root == "" {
		return ""
	}
	if !SuiteTempRootPinned() {
		fmt.Fprintf(os.Stderr, "testutil: scratch root is not pinned, so the tested binary cannot be built "+
			"under it (Rule 22) — e2e cases will resolve %s, whose revision this run does not control\n", filepath.Join(root, "bin", "gopi"))
		return ""
	}
	outDir, err := os.MkdirTemp(SuiteTempRoot(), suiteBinDirPrefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testutil: cannot create the tested-binary directory under %s: %v\n", SuiteTempRoot(), err)
		return ""
	}
	out := filepath.Join(outDir, BinFileName)
	args := []string{"build", "-o", out}
	// Stamp the artifact with the revision it is built from. The toolchain cannot
	// do it here: `vcs.revision` is written only for a checkout whose `.git` is a
	// DIRECTORY, and the tree under test is frequently a linked worktree (`.git`
	// is a FILE) — so without this the binary carries no identity, and the
	// freshness guard in cmd/agent can only compare mtimes, which cannot tell a
	// byte-identical rewrite from a real edit. The operand's spelling is
	// buildidentity's constants (Rule 14: one spelling, shared by the recipes and
	// the reader), never a literal retyped at a call site.
	args = append(args, LdflagsFlag(root)...)
	args = append(args, "."+string(filepath.Separator)+"cmd"+string(filepath.Separator)+"agent"+string(filepath.Separator))
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: building the tested binary from %s failed: %v\n%s\n", root, err, stderr.String())
		return ""
	}
	fmt.Fprintf(os.Stderr, "testutil: tested binary pinned to %s\n", out)
	return out
}

// moduleRoot walks up from the current directory to find go.mod.
func moduleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// MockEnv builds the subprocess environment for a mock-backed run. It strips
// every ANTHROPIC_*/DEEPSEEK_*/GOPI_* variable from the parent environment —
// a leaked real key or base URL must never reach the network from a test
// (zero real-API guarantee) — then injects the mock endpoint and a temp
// GOPI_HOME so sessions/raw logs/memory.db all land in the sandbox.
//
// AGENT-SPAWN GUARD: the env also declares the OPERATOR-ACTOR shape the
// sandbox really is. This test process is the actor driving the real binary
// (the operator's role), but when the suite runs inside a gopi agent session
// the parent environment carries that session's signature — the GOPI_* strip
// above already removes gopi's own half, and HermesSessionIDEnv is stripped
// for the same reason (it is hermes', not ours). HumanTerminalEnv then states
// the actor explicitly, so a guarded verb (`gopi worker`, `gopi task run`)
// starts without an override — no coupling to an inherited signature, which a
// human's own terminal would not have. See devdoc/pa-never-runs-tasks-20260912.md.
func MockEnv(t *testing.T, gopiHome, mockURL string) []string {
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(key, "ANTHROPIC_") ||
			strings.HasPrefix(key, "DEEPSEEK_") ||
			strings.HasPrefix(key, "GOPI_") ||
			key == config.HermesSessionIDEnv {
			continue
		}
		env = append(env, kv)
	}
	// Both alias pairs are set so client resolution works regardless of which
	// env var a future refactor prefers (ResolveClaudeAPIKey/BaseURL read
	// ANTHROPIC_* first, DEEPSEEK_* second — claude_client.go).
	env = append(env,
		"ANTHROPIC_AUTH_TOKEN=test-key",
		"ANTHROPIC_BASE_URL="+mockURL,
		"DEEPSEEK_API_KEY=test-key",
		"DEEPSEEK_BASE_URL="+mockURL,
		"GOPI_HOME="+gopiHome,
		// The GROUND ROOT is pinned per test, ALWAYS (epic
		// worker-creates-child-ground): a repo-aware row's executor child is
		// spawned in a linked worktree the worker CREATES, and MockEnv strips
		// every GOPI_* var off the ambient environment — so without this the
		// fixture's ground would be created under the OPERATOR's real default
		// workspace root (~/workspace), not the test's own temp repo. That is
		// both a hermeticity break (the test writes into a real user directory)
		// and a wrong reading: the row's recorded deliverable.worktree then lies
		// outside the fixture's repo and any assertion about the child's ground
		// fails for a reason that has nothing to do with the code under test.
		// t.TempDir() is per-test and auto-removed, so two tests can never share
		// one ground. Declared here rather than at each call site because
		// MockEnv is the ONE script that decides a mocked run's environment
		// (Rule 14) — 59 call sites cannot each remember it.
		worktreeroots.WorktreeRootEnvName+"="+t.TempDir(),
		// The operator-actor declaration, ALWAYS set (never inherited): see the
		// function comment — a guarded verb must not depend on what the test
		// harness happened to be spawned from.
		config.HumanTerminalEnv+"=1",
	)
	return env
}

// RunGopi runs the real binary to completion: args are the CLI argv after the
// binary, stdin is piped to the process, env/dir sandbox the run. Returns the
// captured stdout, stderr, and the process exit code.
//
// THE WAIT IS JUDGED BY PROGRESS, NOT BY ELAPSED TIME (see runprogress.go for the
// measured incident this replaces). Two situations used to collapse into one
// verdict: a child that is STUCK and a child that is SLOW because the host is
// busy. The old inner deadline bounded the wait with wall clock alone, so a busy
// host condemned a run that was still moving — measured 2026-10-31: this fixture
// passed in 9.271s alone and FAILED at 180.04s inside a whole-package run whose
// loadavg was at its peak, with the child in state=S (an ORDINARY wait, which is
// what the kernel reports for both situations).
//
// The three exits are distinct and ALL of them still fail loudly when the run
// does not finish:
//   - the child exits — the caller gets its code (the normal path);
//   - nothing scheduled in the child's TREE for runNoProgressStall —
//     RunWaitStalled: a HANG, reported as such and decided in that window
//     instead of costing the whole budget;
//   - still moving at subprocessTimeout — RunWaitCeiling: the run never STOPS. A
//     different defect, named separately so the two cannot borrow each other's
//     explanation.
func RunGopi(t *testing.T, args []string, stdin string, env []string, dir string) (stdout, stderr string, code int) {
	t.Helper()
	bin := GopiBin(t)

	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb

	if err := cmd.Start(); err != nil {
		t.Fatalf("gopi %v failed to start: %v", args, err)
	}
	pid := cmd.Process.Pid
	exited := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(exited)
	}()
	// lastSnap is written by the progress reader, which waitRunProgress calls
	// from THIS goroutine at every step — so the failure message can quote the
	// reading the verdict was taken on, with no lock and no race.
	var lastSnap RunProgressSnapshot
	progress := func() RunProgressSnapshot {
		lastSnap = measureRunProgress(pid)
		return lastSnap
	}
	outcome, elapsed := waitRunProgress(exited, progress, runNoProgressStall, subprocessTimeout, runProgressStep, nil)
	if outcome != RunWaitLeft {
		// The child is OURS (this fixture started it), so signalling its pid is
		// never a group, never a pattern (Rule 42).
		_ = cmd.Process.Kill() //stylegate:ignore the child this fixture started; ESRCH means it exited on its own and the verdict stands
		select {
		case <-exited:
		case <-time.After(runKillGrace):
			t.Errorf("gopi %v was killed after the wait ended as %v but was still running %v later — the fixture could not reap the child it started",
				args, outcome, runKillGrace)
		}
	}
	switch outcome {
	case RunWaitStalled:
		t.Fatalf("gopi %v made NO PROGRESS for %v (bound runNoProgressStall=%v): nothing in its process tree was scheduled in that whole window — %s, elapsed %v. This is a HANG verdict, not a slow host: the criterion is whether the child moves, so a run merely queued behind other CPU work keeps advancing its own ticks and reaches the ceiling instead.\n--- stdout ---\n%s\n--- stderr ---\n%s",
			args, runNoProgressStall, runNoProgressStall, runTreeState(lastSnap), elapsed.Round(time.Millisecond), out.String(), errb.String())
	case RunWaitCeiling:
		t.Fatalf("gopi %v was STILL MAKING PROGRESS %v after start and hit the ceiling (bound subprocessTimeout=%v): %s, last movement %v before the verdict. This is NOT a hang — the tree kept being scheduled, so the run is alive: what failed is that it does not terminate at all.\n--- stdout ---\n%s\n--- stderr ---\n%s",
			args, elapsed.Round(time.Millisecond), subprocessTimeout, runTreeState(lastSnap), runNoProgressStall, out.String(), errb.String())
	}

	exit := 0
	if waitErr != nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			t.Fatalf("gopi %v: waiting for the child: %v", args, waitErr)
		}
	}
	return out.String(), errb.String(), exit
}

// Proc is a running gopi subprocess with streaming-captured output (needed
// for server modes like `gopi web` that never exit on their own).
type Proc struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdout *captureBuffer
	stderr *captureBuffer
	done   chan error // closed with the Wait result
	// waited records that Wait() already consumed the done value. The done
	// channel is 1-buffered, so once consumed Running() would otherwise
	// report true forever (the select default case fires on an empty
	// channel) and a deferred Stop() would block on <-done forever — the
	// exact deadlock a Wait-then-Stop caller hits. waited makes Running()
	// report false and Stop() a no-op after Wait().
	muWait sync.Mutex
	waited bool
	// mock is the pgid-scoped handle that owns this process's group; its
	// t.Cleanup registration is the safety net Stop() is not. The handle also
	// owns the single Wait the goroutine below reports through — a second
	// cmd.Wait on one *exec.Cmd would be an error and a race.
	mock *MockProcess
}

// StartGopiBin launches an EXPLICIT binary in the background — the same
// process discipline as StartGopi (streaming capture + the pgid-scoped reaper
// registered as a t.Cleanup safety net), for callers that must drive a
// specific build rather than the canonical $GOPI_BIN / bin/gopi resolution.
func StartGopiBin(t *testing.T, bin string, args []string, env []string) *Proc {
	t.Helper()
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("gopi binary %s not found: %v", bin, err)
	}
	return startProc(t, bin, args, "", env, "")
}

// StartGopi launches the binary in the background. stdin may be nil (the
// process gets a closed pipe). The caller MUST Stop() the process.
func StartGopi(t *testing.T, args []string, stdin string, env []string, dir string) *Proc {
	t.Helper()
	return startProc(t, GopiBin(t), args, stdin, env, dir)
}

// startProc is the shared implementation behind StartGopi/StartGopiBin.
func startProc(t *testing.T, bin string, args []string, stdin string, env []string, dir string) *Proc {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.Dir = dir
	if stdin == "" {
		cmd.Stdin = strings.NewReader("")
	} else {
		cmd.Stdin = strings.NewReader(stdin)
	}
	p := &Proc{
		t:      t,
		cmd:    cmd,
		stdout: newCaptureBuffer(),
		stderr: newCaptureBuffer(),
		done:   make(chan error, 1),
	}
	cmd.Stdout = p.stdout
	cmd.Stderr = p.stderr
	// Spawned through the pgid-scoped reaper: the gopi process leads its own
	// group, so Stop() can take down everything it forked (a `gopi web`
	// server, a wrapper's child) — and the group is reaped on EVERY exit
	// path, including a panic or a t.Fatal that never reaches a deferred
	// Stop(). Stop() is therefore for a caller that wants the process dead
	// mid-test, not the only thing standing between a daemon and the suite.
	p.mock = StartMockProcessCmd(t, cmd)
	go func() { p.done <- p.mock.Wait() }()
	return p
}

// Stdout / Stderr return the output captured so far (thread-safe).
func (p *Proc) Stdout() string { return p.stdout.String() }
func (p *Proc) Stderr() string { return p.stderr.String() }

// Running reports whether the process is still alive (or was never
// Wait()ed — after Wait() the done value is consumed and the process is
// gone, so Running reports false).
func (p *Proc) Running() bool {
	p.muWait.Lock()
	defer p.muWait.Unlock()
	if p.waited {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// Wait blocks until the process exits and returns its exit code.
func (p *Proc) Wait() int {
	err := <-p.done
	p.muWait.Lock()
	p.waited = true
	p.muWait.Unlock()
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	p.t.Fatalf("gopi %v: %v", p.cmd.Args[1:], err)
	return -1
}

// Signal sends a signal to the running process (e.g. syscall.SIGTERM for
// the graceful-closeout tests). No-op once the process has exited.
func (p *Proc) Signal(sig os.Signal) {
	p.t.Helper()
	if !p.Running() {
		return
	}
	if err := p.cmd.Process.Signal(sig); err != nil {
		p.t.Errorf("gopi %v: signal %v: %v", p.cmd.Args[1:], sig, err)
	}
}

// Stop kills the process and waits for it to exit. Safe to call multiple
// times; idempotent after the first kill. After Wait() it is a no-op (the
// process already exited and its done value was consumed).
func (p *Proc) Stop() {
	p.t.Helper()
	p.muWait.Lock()
	waited := p.waited
	p.muWait.Unlock()
	if waited {
		return
	}
	if p.Running() {
		p.mock.Reap()
		<-p.done
	}
}

// WaitFor polls cond until it returns true or procStartTimeout elapses.
// Fails the test with the process's captured output on timeout.
func (p *Proc) WaitFor(what string, cond func() bool) {
	p.t.Helper()
	deadline := time.Now().Add(procStartTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		if !p.Running() {
			break
		}
		time.Sleep(procPollInterval)
	}
	p.t.Fatalf("gopi %v: timed out waiting for %s\n--- stdout ---\n%s\n--- stderr ---\n%s",
		p.cmd.Args[1:], what, p.Stdout(), p.Stderr())
}

// WebBaseURL waits for the `gopi web` listening banner on stderr and returns
// the first "open: http://host:port" URL. Works with --addr=127.0.0.1:0
// (ephemeral port — the banner prints the actually-bound port).
func (p *Proc) WebBaseURL() string {
	p.t.Helper()
	var base string
	p.WaitFor("the listening banner", func() bool {
		for _, line := range strings.Split(p.Stderr(), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "open: http://") {
				base = strings.TrimPrefix(line, "open: ")
				return true
			}
		}
		return false
	})
	return base
}

// captureBuffer is a goroutine-safe writer accumulating into a string.
type captureBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func newCaptureBuffer() *captureBuffer { return &captureBuffer{} }

func (c *captureBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *captureBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

var _ io.Writer = (*captureBuffer)(nil)
