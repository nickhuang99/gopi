package main

// worker_foreground_console_e2e_test.go — the epic's ACCEPTANCE, on the real
// binary: a foreground worker prints the task it executes, a detached daemon
// does not, and BOTH still file the same text in the row's session directory.
//
// WHY AN E2E IS THE ONLY HONEST PROOF: the routing decision reads the SPAWNER's
// own identity (the daemon marker env of the process that claims and spawns), so
// a unit test can assert the predicate but cannot show that a real `gopi worker`
// process ends up printing a real executor child's real model answer on the
// stream its launcher holds. The observable here is the worker process's OWN
// stdout pipe — the "console" the operator sees — captured separately from
// stderr, exactly as the two differ for a reader.
//
// THE PRE-FIX FAILURE (measured before the change): the foreground worker's
// stdout carried only its round lines ("worker: loop start", "claimed: …") and
// NOTHING of the child's answer — the answer existed solely in
// <session-dir>/output.log. The daemon half is asserted as the ABSENCE of the
// child's text in the daemon's own stdio (logs/worker.log), which is where a
// console copy would have landed for it.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

const (
	// consoleE2EWait bounds the poll for a round's artifacts (the run log
	// appearing with the child's text); the daemon's own round cadence is the
	// slow side. A test deadline, never a production timer.
	consoleE2EWait = 90 * time.Second
	// consoleE2EPoll is that poll's cadence.
	consoleE2EPoll = 200 * time.Millisecond
	// consoleE2EChildText is the distinctive text the mock model answers with —
	// distinctive so that "it reached the console" cannot be satisfied by the
	// round's own lines or by the briefing (which names the task summary too).
	consoleE2EChildText = "console-probe-7f3a: the executor child's own answer"
)

// consoleE2EWorkerStreams runs `gopi worker <args…>` from a scratch directory
// with stdout and stderr captured SEPARATELY (the console face is stdout, and a
// merged capture could not tell it from a diagnostic).
//
// The working directory is a temp dir on purpose: the claimant's cwd is the
// fallback executor ground, and a test process standing in the repo's own
// worktree would hand that worktree to the child as its project.
func consoleE2EWorkerStreams(t *testing.T, bin string, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.Dir = t.TempDir()
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s %v: %v", bin, args, err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			var ee *exec.ExitError
			if asExitError(err, &ee) {
				return out.String(), errb.String(), ee.ExitCode()
			}
			t.Fatalf("wait %s %v: %v", bin, args, err)
		}
	case <-time.After(consoleE2EWait):
		_ = cmd.Process.Kill() //stylegate:ignore kills the child process the test spawned and is tearing down
		<-done
		t.Fatalf("gopi %v did not finish within %s\n--- stdout ---\n%s\n--- stderr ---\n%s",
			args, consoleE2EWait, out.String(), errb.String())
	}
	return out.String(), errb.String(), 0
}

// consoleE2ESubmit registers the probe row in the sandbox profile, targeting its
// own scratch repo (so the child's cwd resolution never falls back to the
// launcher's directory).
func consoleE2ESubmit(t *testing.T, env []string, repo, taskID, summary string) {
	t.Helper()
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit", summary,
		"--payload", "answer with the probe text", "--task-id", taskID, "--repo", repo},
		"", env, t.TempDir()); code != 0 {
		t.Fatalf("submit %s: code=%d stderr=%s", taskID, code, stderr)
	}
}

// consoleE2ERunLog waits for the row's run log to carry the child's answer and
// returns its path and body — the FILE face both spawners must keep.
//
// The path is resolved by the PRODUCTION resolver (tools.TaskLogGlob, the same
// one `task show` globs), pointed at the SANDBOX home this test handed the
// worker: GOPI_HOME is re-pinned in THIS process for the duration of the call,
// because the resolver reads the calling process's profile — the suite's own
// pin, not the child's. Resolving through the production glob rather than
// re-spelling "<dir>/output.log" is what makes "the file the seam opened" and
// "the file this test reads" agreement about a real path instead of a mirrored
// literal.
func consoleE2ERunLog(t *testing.T, gopiHome, taskID string) (string, string) {
	t.Helper()
	t.Setenv(config.GopiDirEnv, gopiHome)
	glob, err := tools.TaskLogGlob(taskID)
	if err != nil {
		t.Fatalf("resolve the run log glob for %s: %v", taskID, err)
	}
	deadline := time.Now().Add(consoleE2EWait)
	for {
		if paths, gerr := filepath.Glob(glob); gerr == nil {
			for _, path := range paths {
				body, rerr := os.ReadFile(path)
				if rerr == nil && strings.Contains(string(body), consoleE2EChildText) {
					return path, string(body)
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the run's output log for %s never carried the child's answer (%q) within %s — the FILE face is not optional for either spawner\n  glob: %s\n  tree: %s",
				taskID, consoleE2EChildText, consoleE2EWait, glob, consoleE2ETree(t, filepath.Dir(glob)))
		}
		time.Sleep(consoleE2EPoll) //stylegate:allow-sleep poll for the round's run log (bounded by consoleE2EWait)
	}
}

// consoleE2ETree renders the session directory's contents for a failure message
// (a missing log is diagnosed by what IS there, never by the absence alone).
func consoleE2ETree(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "session dir unreadable: " + err.Error()
	}
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "  %s\n", e.Name())
	}
	if b.Len() == 0 {
		return "(session dir is empty)"
	}
	return b.String()
}

// TestWorkerForegroundPrintsTheTaskOnItsConsole is the epic's acceptance walk:
// `gopi worker --once` in the FRONT must print the executor child's answer on
// the stream its launcher holds, and the same answer must still land in the
// row's own session directory.
func TestWorkerForegroundPrintsTheTaskOnItsConsole(t *testing.T) {
	bin := testutil.GopiBin(t)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: consoleE2EChildText})
	t.Cleanup(mock.Close)
	taskRepo := executorCwdProbeRepo(t, "console-e2e-project")
	env := executorProbeEnv(t, gopiHome, dbPath, mock.URL())

	const taskID = "6a7ee7b3-886a-4fc7-a0fc-b689a5223a0a"
	consoleE2ESubmit(t, env, taskRepo, taskID, "foreground console acceptance")

	stdout, stderr, code := consoleE2EWorkerStreams(t, bin, env, CliVerbWorker, workerFlagOnce)
	if code != 0 {
		t.Fatalf("foreground `gopi worker --once` must succeed: code=%d\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	// The round really ran the task: without this the console assertion would
	// pass for a worker that never claimed anything (the green-blind shape).
	if !strings.Contains(stdout+stderr, taskID) {
		t.Fatalf("the worker never claimed %s — nothing was executed\n--- stdout ---\n%s\n--- stderr ---\n%s", taskID, stdout, stderr)
	}
	if !strings.Contains(stdout, consoleE2EChildText) {
		t.Fatalf("the executor child's answer must reach the FOREGROUND worker's console (stdout) — this is the behaviour the epic restores; a reader of `gopi worker` saw only the round's own lines.\n--- stdout ---\n%s\n--- stderr ---\n%s", stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "also on this console") {
		t.Errorf("the seam must ANNOUNCE that the child's stream is also on this console (Rule 15: the destination is never silent; the line is printed on the spawning stream, i.e. this worker's stderr) — got:\n--- stdout ---\n%s\n--- stderr ---\n%s", stdout, stderr)
	}
	logPath, body := consoleE2ERunLog(t, gopiHome, taskID)
	if !strings.Contains(body, consoleE2EChildText) {
		t.Errorf("the row's run log (%s) must keep the child's answer — the console face is additive, never a replacement; got:\n%s", logPath, body)
	}
}

// TestDaemonWorkerKeepsTheTaskTextOutOfItsOwnStdio is the other half: a
// DETACHED worker's stdio is <gopi-dir>/logs/worker.log (the spawn seam
// redirects it there), so the child's text must NOT be copied into it — the
// session directory is that spawner's record. Both assertions come from the
// same round, so the contrast is measured, not assumed.
func TestDaemonWorkerKeepsTheTaskTextOutOfItsOwnStdio(t *testing.T) {
	bin := wdBin(t)
	gopiHome := t.TempDir()
	settings := filepath.Join(gopiHome, "settings.json")
	if err := os.WriteFile(settings, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	dbPath := filepath.Join(gopiHome, "memory.db")
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: consoleE2EChildText})
	t.Cleanup(mock.Close)
	taskRepo := executorCwdProbeRepo(t, "console-e2e-daemon-project")

	// ITS OWN workspace_root (measured 2026-10-28). Without it the row's ground
	// lands under the DEFAULT root (~/workspace) — shared with the operator's
	// real runs and with every earlier run of this suite — where a leftover
	// directory of the same name turned the whole run into
	// "cannot resolve the ground … the ground path is taken by a directory that
	// is not a registered linked worktree". That message is the ground guard
	// doing its job; the FIXTURE was the thing reaching outside its sandbox.
	// A daemon fixture must not depend on the state of a directory it does not
	// own, so it pins the root inside its own t.TempDir().
	workspaceRoot := t.TempDir()
	env := wdEnv(t, gopiHome, settings,
		"GOPI_MEMORY_DB="+dbPath,
		"ANTHROPIC_AUTH_TOKEN=test-key",
		"ANTHROPIC_BASE_URL="+mock.URL(),
		"DEEPSEEK_API_KEY=test-key",
		"DEEPSEEK_BASE_URL="+mock.URL(),
		config.HumanTerminalEnv+"=1",
		"GOPI_AUTO_TASK_REGISTER=1",
		"GOPI_ALLOW_AGENT_SPAWN=1",
		worktreeroots.WorktreeRootEnvName+"="+workspaceRoot,
	)
	const taskID = "1220bd89-575c-4fb4-81c0-8b1eedddfafa"
	submitEnv := append(append([]string{}, env...), tools.TaskExecutorSessionEnv+"=")
	consoleE2ESubmit(t, submitEnv, taskRepo, taskID, "daemon console acceptance")

	code, out := wdRun(t, bin, env, CliVerbWorker, workerDaemonFlagLong)
	if code != 0 {
		t.Fatalf("`gopi worker --daemon` must exit 0, got %d:\n%s", code, out)
	}
	workerPID, workerLog := wdParseStart(t, out)
	t.Cleanup(func() { wdReap(t, gopiHome) })
	if !wdProcAlive(workerPID) {
		t.Fatalf("the worker daemon (pid %d) is not alive", workerPID)
	}

	logPath, body := consoleE2ERunLog(t, gopiHome, taskID)
	if !strings.Contains(body, consoleE2EChildText) {
		t.Errorf("the row's run log (%s) must hold the child's answer; got:\n%s", logPath, body)
	}
	// The routing verdict is in the RUN's record (execRunFacts = the run log +
	// the spawner's stderr, which for a daemon IS worker.log): the operator can
	// tell a suppressed console copy from a copy that failed.
	if !strings.Contains(body, "no console copy") {
		t.Errorf("the run log must carry the daemon's routing verdict (Rule 15); got:\n%s", body)
	}
	// And the daemon's own stdio — the file the console copy would have used —
	// must not carry it: the session directory is the daemon's record.
	daemonStdio, err := os.ReadFile(workerLog)
	if err != nil {
		t.Fatalf("read the daemon's own stdio log %s: %v", workerLog, err)
	}
	if strings.Contains(string(daemonStdio), consoleE2EChildText) {
		t.Errorf("the child's text reached the DETACHED worker's own stdio (%s) — a daemon keeps the task's account in the row's session directory, not in a per-component file no task id reaches", workerLog)
	}
	if !strings.Contains(string(daemonStdio), "no console copy") {
		t.Errorf("the daemon's own log must record the routing verdict beside the round it describes; got:\n%s", daemonStdio)
	}
}
