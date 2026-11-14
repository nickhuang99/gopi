package tools

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// A KILLED run must not leave its build-cache sandbox behind.
//
// MEASURED 2026-10-12 (this host, task 01a13032-4c50): <home>/workspace/
// gopi-build-cache held 686 `gopi-tools-suite-build-cache-<pid>` directories
// (1.9 GiB) and not one of them named a live process. Every one is the sandbox
// of a run that DIED rather than exited: the clean path —
// toolsRemoveBuildCacheSandbox, called after m.Run — removes the sandbox 2.7 s
// after the arm creates it, and a controlled clean run of this suite leaves
// nothing. What produces the residue is the wall clock a run does not control:
// scripts/test-budget.sh passes one budget as `go test -timeout`, and Go's own
// timeout kills the test binary from inside (panic: test timed out, then exit)
// while a bare `go test ./...` gives every package a 10m clock that expires the
// same way. A terminating signal skips the defer stack AND the post-m.Run
// removal, which is exactly why the reaper has to be a signal handler
// (toolsReapBuildCacheSandboxOnSignal).
//
// The test drives the real shape: a CHILD of this same test binary (so it runs
// the real TestMain and the real sandbox arm), killed with SIGTERM — the signal
// the budget timeout delivers — and the sandbox the child reported is asserted
// gone afterwards. Reverting the handler makes it red with the directory still
// on disk.
const (
	// buildCacheSandboxSignalChildEnv marks the child half of this test. It is
	// read by the child to park itself until the parent kills it.
	buildCacheSandboxSignalChildEnv = "GOPI_TOOLS_BUILDCACHE_SIGNAL_CHILD"
	// buildCacheSandboxSignalPathEnv names the file the CHILD writes its own
	// sandbox path into. The parent reads the path from there rather than
	// recomputing it: which candidate arm wins is the host's decision, and a
	// second spelling of that decision here is exactly the drift the candidate
	// list exists to avoid.
	buildCacheSandboxSignalPathEnv = "GOPI_TOOLS_BUILDCACHE_SIGNAL_PATH"
	// buildCacheSandboxSignalWait bounds the parent's wait for the child to
	// create its sandbox (compile + TestMain on a loaded host).
	buildCacheSandboxSignalWait = 90 * time.Second
	// buildCacheSandboxSignalReapWait bounds the wait for the reaper after the
	// signal — generous, because the handler only has to run one RemoveAll.
	buildCacheSandboxSignalReapWait = 30 * time.Second
)

// buildCacheSandboxSignalYieldSpace hands the scheduler back while a bounded
// wait re-reads its observable.
//
// Both waits below poll a fact a DIFFERENT process produces (the child's report
// file; the sandbox directory's disappearance after the child's handler ran), so
// the correct wait is "re-read the observable until it changes, or the deadline",
// and no interval between the reads is load-bearing: the deadline is what bounds
// the wait, and the yield only stops this goroutine from starving the child it is
// waiting for. A fixed time.Sleep here would instead encode an assumption about
// how fast that child is — the class of assumption that makes a test hang on a
// loaded host and pass on a quiet one (stylegate's `test-sleep` rule, ceiling 0).
func buildCacheSandboxSignalYieldSpace() {
	runtime.Gosched()
}

// TestSuiteBuildCacheSandboxNameIsClaimedByTheSweep pins the two halves of one
// contract that live in two packages (Rule 14): the name THIS suite mints for its
// per-run GOCACHE sandbox must be a name the sweep's `build-cache/suite-sandbox`
// row claims. A prefix spelled in both places could drift, and a drifted
// predicate reports an empty tree while the shells pile up — the failure mode
// this assertion exists to make impossible.
func TestSuiteBuildCacheSandboxNameIsClaimedByTheSweep(t *testing.T) {
	if got, want := worktreeroots.SharedBuildCacheSuiteSandboxPrefix, toolsBuildCacheSandboxName+"-"; got != want {
		t.Fatalf("the shared prefix %q and the writer's name %q disagree — the sweep's predicate is built from "+
			"the shared one, so a drift here means the row claims nothing the writer creates", got, want)
	}
	pinned := os.Getenv(scratch.GoCacheRootEnvName)
	if pinned == "" {
		t.Skipf("%s is unset — this run installed no sandbox, so there is no minted name to check",
			scratch.GoCacheRootEnvName)
	}
	name := filepath.Base(pinned)
	if !strings.HasPrefix(name, toolsBuildCacheSandboxName) {
		t.Skipf("the engaged candidate is not the windowed arm (%s does not carry %s) — this host resolved a "+
			"different placement, which its own predicate owns", name, toolsBuildCacheSandboxName)
	}
	if !scratch.BuildCacheSuiteSandboxNameIsOurs(name) {
		t.Fatalf("the sweep does not claim the name this suite minted (%s) — the shells an unreaped run leaves "+
			"would stay unclaimed, which is the residue class the row exists for", name)
	}
	if got, want := filepath.Base(filepath.Dir(pinned)), worktreeroots.SharedBuildCacheDirName; got != want {
		t.Fatalf("the pinned sandbox %s sits under %q — the sweep's row is rooted at "+
			"<worktree root>/%s, so a sandbox elsewhere is a path no row addresses",
			pinned, got, worktreeroots.SharedBuildCacheDirName)
	}
}

// TestKilledSuiteRunReapsItsBuildCacheSandbox is the regression guard on the
// sandbox's OTHER exit path (see the file header).
func TestKilledSuiteRunReapsItsBuildCacheSandbox(t *testing.T) {
	if os.Getenv(buildCacheSandboxSignalChildEnv) != "" {
		buildCacheSandboxSignalChildPark(t)
		return
	}

	pinned := os.Getenv(scratch.GoCacheRootEnvName)
	if pinned == "" || !strings.Contains(pinned, toolsBuildCacheSandboxName) {
		t.Skipf("%s=%q is not this suite's sandbox — the arm did not engage on this host, so there is no "+
			"sandbox whose fate after a signal could be asserted", scratch.GoCacheRootEnvName, pinned)
	}

	report := filepath.Join(t.TempDir(), "sandbox-path")
	cmd := exec.Command(os.Args[0], "-test.run", "^"+t.Name()+"$", "-test.v")
	cmd.Env = append(os.Environ(), buildCacheSandboxSignalChildEnv+"=1", buildCacheSandboxSignalPathEnv+"="+report)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	// Its own session, so the cleanup kill below cannot miss a grandchild and so
	// the SIGTERM is aimed at exactly the one process under test.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the child suite run: %v", err)
	}
	childPid := cmd.Process.Pid
	// Rule 15b: the child is PARKED (select{}) — a failure before the kill below
	// must not leave it running, and the kill is best-effort by construction
	// (the child may already be gone, which is the normal path).
	defer func() {
		_ = syscall.Kill(-childPid, syscall.SIGKILL) //stylegate:ignore fixture cleanup on the failure path; best-effort
		_, _ = cmd.Process.Wait()
	}()

	sandbox := buildCacheSandboxSignalAwaitReport(t, report, &out, childPid)
	if sandbox == "" {
		return
	}
	t.Logf("child %d reported its sandbox %s", childPid, sandbox)
	if _, err := os.Stat(sandbox); err != nil {
		t.Fatalf("the child reported a sandbox the parent cannot see (%s): %v — the report is the wrong "+
			"reading, so this test would prove nothing about the reaper", sandbox, err)
	}

	// SIGTERM is the signal the budget path delivers (scripts/test-budget.sh's
	// `go test -timeout`, and Go's own per-package default).
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signalling SIGTERM to the child %d: %v", childPid, err)
	}
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case <-waitErr:
	case <-time.After(buildCacheSandboxSignalWait):
		t.Fatalf("the child %d did not exit within %s of SIGTERM — signal.Notify DISABLES a signal's default "+
			"action, so a handler that observes without re-raising leaves the run alive forever "+
			"(toolsReapBuildCacheSandboxOnSignal's second half). Child output:\n%s",
			childPid, buildCacheSandboxSignalWait, out.String())
	}
	// The child must die BY THE SIGNAL, not merely exit: a handler that
	// swallowed SIGTERM would report success while turning "the budget killed a
	// long run" into "the budget killed nothing".
	var ws syscall.WaitStatus
	if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
		ws = status
	}
	if !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
		t.Fatalf("the child %d ended as %v, not by SIGTERM — the reaper must clean up and then let the "+
			"signal end the run with the status its caller sent. Child output:\n%s",
			childPid, cmd.ProcessState, out.String())
	}

	deadline := time.Now().Add(buildCacheSandboxSignalReapWait)
	for {
		_, err := os.Stat(sandbox)
		if errors.Is(err, fs.ErrNotExist) {
			return
		}
		if err != nil {
			t.Fatalf("stat %s: %v", sandbox, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("the sandbox %s survived the run that created it being SIGTERMed — this is the "+
				"residue class measured 2026-10-12 (686 such directories, 1.9 GiB, every one from a run "+
				"that died instead of exiting). A terminating signal skips both the defer stack and the "+
				"post-m.Run removal, so the sandbox needs a signal handler "+
				"(toolsReapBuildCacheSandboxOnSignal). Child output:\n%s", sandbox, out.String())
		}
		buildCacheSandboxSignalYieldSpace()
	}
}

// buildCacheSandboxSignalChildPark is the child's half: report this process's
// own pinned sandbox to the parent, then park until the parent's signal. It must
// never run in the parent (the env marker above is the only way in).
func buildCacheSandboxSignalChildPark(t *testing.T) {
	report := os.Getenv(buildCacheSandboxSignalPathEnv)
	if report == "" {
		t.Fatalf("%s is set but %s is not — the child cannot report its sandbox",
			buildCacheSandboxSignalChildEnv, buildCacheSandboxSignalPathEnv)
	}
	// The value is this run's OWN pinned sandbox: TestMain installed it before
	// m.Run, so it already exists on disk at this point.
	if err := os.WriteFile(report, []byte(os.Getenv(scratch.GoCacheRootEnvName)), 0o600); err != nil {
		t.Fatalf("reporting the sandbox path to %s: %v", report, err)
	}
	// Park. Unreachable in a healthy run — the parent SIGTERMs this process.
	select {}
}

// buildCacheSandboxSignalAwaitReport waits for the child to publish its sandbox
// path and returns it ("" after reporting a skip, when the arm did not engage in
// the child either).
func buildCacheSandboxSignalAwaitReport(t *testing.T, report string, out *bytes.Buffer, childPid int) string {
	t.Helper()
	deadline := time.Now().Add(buildCacheSandboxSignalWait)
	for {
		if raw, err := os.ReadFile(report); err == nil {
			sandbox := strings.TrimSpace(string(raw))
			if sandbox == "" {
				t.Skipf("the child %d reported no sandbox (%s unset in it) — the same arm did not engage "+
					"there, so a signal has nothing to reap. Child output:\n%s",
					childPid, scratch.GoCacheRootEnvName, out.String())
			}
			return sandbox
		}
		if time.Now().After(deadline) {
			t.Fatalf("the child %d never reported its sandbox within %s. Child output:\n%s",
				childPid, buildCacheSandboxSignalWait, out.String())
		}
		buildCacheSandboxSignalYieldSpace()
	}
}
