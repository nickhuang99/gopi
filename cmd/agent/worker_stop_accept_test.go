package main

// Graceful-stop acceptance for the producer's own worktree release.
//
// WHY THIS FILE EXISTS (signal-handling epic; sandbox rig /tmp/self-release-*):
// the producer-side release is a DEFER on the round's close-out. Go's default
// SIGTERM/SIGINT disposition kills the process outright, so a stop signal that
// lands mid-round used to unwind NOTHING — measured on the rig: `gopi worker
// --once` under a supervisor's `timeout` exited 143 with `worker: claimed <id>`
// as its last line, the row stayed in_progress and its worktree stayed in
// `git worktree list`, so scripts/safe-merge.sh gate [c] kept refusing the
// branch of a run that was already over.
//
// This test drives the REAL signal path, not a simulated flag:
//
//	① a real git repo + a linked worktree on a feat branch, the deliverable
//	   row claimed by THIS process (host:pid — the stamp the release matches);
//	② assertWorkerStopRelease arms installWorkerStopSignals, spawns a real
//	   `sleep` as the "in-flight run child" through registerPassChild (the
//	   production registration runTaskRunOnce performs), and delivers a REAL
//	   SIGTERM to this process;
//	③ the handler must FORWARD it to the child (the child dies ⇒ the wait
//	   ends, no 20s grace), and the round's close-out — the production
//	   release call — must then free the directory;
//	④ the branch tip must be byte-identical and a sibling worktree untouched.
//
// It FAILS on the unfixed parent for the intended reason: there the signal
// kills the process (the test binary), so the release never runs at all — with
// the handler absent this file cannot even report a green.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// stopReleaseChild is the pid of the real child the handler must forward to —
// shared with the assertion because the test's own liveness check is what
// proves the forward happened (rather than the 20s grace expiring).
var stopReleaseChildPID int

func TestWorkerStopReleasesOwnWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	// The release reads the ledger through GOPI_MEMORY_DB and the settings for
	// the peer topology; both are sandboxed so the test can never touch the
	// developer's own rows.
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"sync_peers":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsEnv, settings)
	t.Setenv(config.SyncPeersEnv, "")
	// The release writes its line to the WORKER's own stderr (epic
	// child-stream-task-output: the worker keeps the launcher's stream for the
	// whole round), so the pass only needs a profile to resolve the ledger
	// against — no log to attach to.
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	wt := filepath.Join(root, "wt")
	wt2 := filepath.Join(root, "wt2")
	const branch, branch2 = "feat/self-release-e2e", "feat/self-live-e2e"
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run(root, "init", repo)
	run(repo, "config", "user.email", "t@t")
	run(repo, "config", "user.name", "t")
	run(repo, "checkout", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(repo, "add", "a.txt")
	run(repo, "commit", "-m", "init")
	run(repo, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, "deliverable.txt"), []byte("the deliverable"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(repo, "add", "deliverable.txt")
	run(repo, "commit", "-m", "the deliverable")
	tip := run(repo, "rev-parse", branch)
	run(repo, "checkout", "-b", branch2)
	if err := os.WriteFile(filepath.Join(repo, "other.txt"), []byte("peer del"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(repo, "add", "other.txt")
	run(repo, "commit", "-m", "peer deliverable")
	tip2 := run(repo, "rev-parse", branch2)
	run(repo, "checkout", "master")
	run(repo, "worktree", "add", wt, branch)
	run(repo, "worktree", "add", wt2, branch2)

	// The row: claimed by THIS process, status in_progress — the honest state
	// at a round's close-out (the terminal write lands after the release).
	taskID := "01a0962e-stop-release"
	seedOwnClaimedRow(t, taskID, repo, branch, wt)

	// The stop: a real signal, delivered while a real child is in flight.
	stopReleaseChildPID = startStopReleaseChild(t, taskID)

	// The production close-out: the release runs in the round's defer chain.
	lines := tools.ReclaimOwnFinishedWorktrees()
	joined := strings.Join(lines, "\n")
	t.Logf("release lines: %s", joined)
	if !strings.Contains(joined, "own worktree released") {
		t.Fatalf("the stopped round's own release did not run: %q", joined)
	}

	// ①② the row's worktree is gone; the branch is the deliverable and stays.
	list := run(repo, "worktree", "list")
	if strings.Contains(list, wt+" ") || strings.Contains(list, wt+"\n") {
		t.Fatalf("the stopped round's own worktree is still registered:\n%s", list)
	}
	if got := run(repo, "rev-parse", branch); got != tip {
		t.Fatalf("branch %s moved: %s != %s", branch, got, tip)
	}
	// ③ the peer's ground is untouched: it is not this process's row.
	if !strings.Contains(list, wt2+" ") {
		t.Fatalf("the peer's worktree was freed:\n%s", list)
	}
	if got := run(repo, "rev-parse", branch2); got != tip2 {
		t.Fatalf("peer branch %s moved: %s != %s", branch2, got, tip2)
	}
}

// startStopReleaseChild spawns a real `sleep` as the in-flight run child,
// registers it exactly the way runTaskRunOnce does (registerPassChild, kind
// passChildKindRun), installs the production signal handler and delivers a REAL
// SIGTERM to this process. It returns the child's pid so the test can prove
// the handler FORWARDED the signal (a forwarded SIGTERM ends `sleep` at once;
// without the forward the handler still exits the wait only at its 20s grace,
// which the elapsed-time assertion below rules out).
func startStopReleaseChild(t *testing.T, taskID string) int {
	t.Helper()
	// THE TEST-ENV VERIFICATION (so the assertion can never pass by accident):
	// ① systemd and its `timeout`-style wrappers send the stop to the WHOLE
	// process group (verified: a child started by `timeout` survives only if
	// it escapes to its own group). So the child gets its own group — the
	// forwarding below is the ONLY thing that can end it.
	child := exec.Command("sleep", "60")
	// Setpgid is the SUBJECT (the forward is the only thing allowed to end it);
	// Pdeathsig adds the kernel's lifetime arm, which Setpgid does not provide.
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	child.Dir = t.TempDir()
	if err := child.Start(); err != nil {
		t.Skipf("sleep unavailable: %v", err)
	}
	pid := child.Process.Pid
	underTest := passChild{
		Kind:  passChildKindRun,
		Label: "task-run child " + taskID,
		PID:   pid,
		// Production registration (merger_verify_child.go) sets Setpgid and
		// records the group, so an escalation can signal the GROUP; the test
		// mirrors that, which is what makes the group-forward path live.
		Group: pid,
	}
	deregister := registerPassChild(underTest)
	// The WAITER: production deregisters when the child is reaped
	// (runTaskRunOnce's `defer deregisterChild()` right after cmd.Wait), so
	// the registry's emptiness IS "the child exited". The test holds the same
	// relation — without this goroutine the entry would sit in the registry
	// for the whole test and even a perfectly forwarded signal would look like
	// a 5s grace expiry.
	reaped := make(chan struct{})
	go func() {
		_, _ = child.Process.Wait() //stylegate:ignore reap of the worker under test; its exit is signalled by the reaped channel, not by this error
		deregister()
		close(reaped)
	}()
	t.Cleanup(func() {
		deregister()
		_ = child.Process.Kill()    //stylegate:ignore teardown of the worker under test; the stop path is what the test drives
		_, _ = child.Process.Wait() //stylegate:ignore reap after the kill; the pid is not the assertion
	})
	uninstall := installWorkerStopSignals()
	t.Cleanup(uninstall)

	// DISARM THE SUITE'S OWN SIGNAL ARM FIRST — it is a COMPETING subscriber of
	// the very signal this test delivers, and it is destructive (measured
	// 2026-10-23, this host; the failure this fixes).
	//
	// internal/testutil's pin arms a SIGTERM handler that judges the suite's
	// run root at signal time (armSuiteRunRootReapOnSignal). It cannot know
	// whether its process is about to die (the os/signal peer set is
	// unreadable), so its only safe action is a REVERSIBLE one: it renames the
	// WHOLE run root aside, then re-raises and waits suiteRunRootSurviveSettle
	// (500ms) to find out whether it survived. This test lives inside that
	// root — t.TempDir() is the pinned suite temp root — so when this test
	// delivers its OWN SIGTERM, the arm moves the test's ledger (and its git
	// repos) out from under it for the duration of the measurement:
	//
	//	=== RUN   TestWorkerStopReleasesOwnWorktree
	//	testutil: terminated — moved this run's scratch root
	//	  /dev/shm/suite-cmd-agent-run-<N> aside to ..._shelved (the owner
	//	  registration names this process) ...
	//	scratch: recreated suite run root /dev/shm/suite-cmd-agent-run-<N>
	//	  (written to after that run's pin had removed it)
	//	worker_stop_accept_test.go:126: the stopped round's own release did not
	//	  run: ""
	//	--- FAIL: TestWorkerStopReleasesOwnWorktree (0.12s)
	//
	// i.e. ReclaimOwnFinishedWorktrees opened GOPI_MEMORY_DB, the path no longer
	// existed, openTasksDB built a FRESH empty ledger there (0 rows — measured
	// with a debug probe: `total=0 mine=0 filtered=0`), so the release found
	// nothing to release. The assertion then reported the honest symptom.
	//
	// The disarm is the API's own documented contract (JoinSuiteRunRootSignalHandler:
	// "disarms the signal arm and WAITS for its goroutine to leave its park", and
	// FinishSuiteForTestMain calls it on the clean path): this test takes over the
	// process's SIGTERM disposition for its duration, exactly as the production
	// worker does. It also makes the observation HONEST — in production the only
	// subscriber of the stop signal is installWorkerStopSignals, and the arm is
	// test-infrastructure noise that changes the outcome being measured.
	testutil.JoinSuiteRunRootSignalHandler()
	// The grace is BOUNDED and shortened here on purpose: the whole point of
	// this test is "the handler FORWARDED the signal", and the only way the
	// assertion can see that is elapsed-time — a forward ends the wait in
	// roughly one poll interval, the timeout path burns the grace. Wall-clock
	// assertions on a shared machine are flaky unless the two outcomes are
	// far apart, so pin the grace instead of trusting the 5s default.
	t.Setenv(workerStopGraceEnv, "3")

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("deliver SIGTERM: %v", err)
	}
	// The handler is a goroutine: wait for it to bank the request (bounded).
	deadline := time.Now().Add(5 * time.Second)
	for !workerStopRequested() {
		if time.Now().After(deadline) {
			t.Fatal("the SIGTERM handler never recorded the stop request")
		}
		time.Sleep(10 * time.Millisecond) //stylegate:allow-sleep poll tick while the SIGTERM handler records the stop request; the wait is bounded by the loop's deadline
	}
	started := time.Now()
	awaitWorkerStopChild(false) // the BARE-SIGNAL shape: no durable request standing
	elapsed := time.Since(started)
	select {
	case <-reaped:
	case <-time.After(time.Second):
		t.Fatalf("the in-flight child (pid %d) did not exit on its own", pid)
	}
	if elapsed > workerStopGrace()/2 {
		t.Fatalf("the stop waited %s — the signal was not forwarded to the in-flight child (pid %d)", elapsed, pid)
	}
	if syscall.Kill(pid, 0) == nil {
		t.Fatalf("the in-flight child (pid %d) survived the forwarded stop", pid)
	}
	return pid
}

// seedOwnClaimedRow writes the claimed-by-this-process row the release matches
// (host:pid, in_progress, deliverable naming repo/branch/worktree) — the shape
// a worker round's own writeback leaves at close-out.
func seedOwnClaimedRow(t *testing.T, taskID, repo, branch, wt string) {
	t.Helper()
	host, err := os.Hostname()
	if err != nil {
		t.Skipf("no hostname: %v", err)
	}
	stamp := host + ":" + strconv.Itoa(os.Getpid())
	db, err := tools.OpenTasksDBForSweep(t.Context())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	now := float64(time.Now().Unix())
	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO tasks (task_id, summary, status, deliverable, source, claimed_by, claimed_at, created_at, updated_at)
		 VALUES (?,?,?,?,'auto',?,?,?,?)`,
		taskID, "stop-release acceptance", tools.TaskStatusInProgress,
		`{"repo":"`+repo+`","branch":"`+branch+`","worktree":"`+wt+`"}`,
		stamp, now, now, now); err != nil {
		t.Fatalf("seed row: %v", err)
	}
}
