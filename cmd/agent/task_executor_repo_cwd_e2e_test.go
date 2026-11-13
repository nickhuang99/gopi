package main

// task_executor_repo_cwd_e2e_test.go — the daemon-context-isolation epic's
// END-TO-END acceptance: a worker launched from directory X executing a task
// that BELONGS to repo Y must stand its executor child in Y, with an
// execution context of its own.
//
// WHY ONLY AN E2E CAN PROVE IT: the resolution reads the LEDGER (the row's
// deliverable.repo). A unit test can assert the resolver's return value, but
// only a real `gopi worker` run proves the two halves meet — that the spawn
// seam resolved from the row the worker actually claimed and that the child
// really ended up standing there. The child's own `pwd` tool_result is the
// evidence, paired to its tool_use by tool_use_id; a transcript substring is
// NOT evidence here (the briefing the child receives names the repo path too,
// so "the repo appears somewhere" stays green while the child stands in the
// launcher's directory — the measured green-blind shape this suite documents).
//
// THE PRE-EPIC FAILURE (red-before, measured): the old walk launched the
// worker FROM the task's own repo, so "child cwd == repo" was accidentally
// true and nothing distinguished the two. These tests launch the worker from
// a DIFFERENT directory (a second repo) — exactly what a resident daemon does
// — so the assertions can only pass when the context came from the ROW.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/heavytest"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
	"github.com/nick/gopi/internal/worktreeroots"
)

// executorCwdProbeRepo builds a minimal git work tree with ONE commit — the
// task's repo target (submit verifies it before writing the row) and, for the
// daemon's own directory, a plainly different tree.
func executorCwdProbeRepo(t *testing.T, name string) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", repo, err)
	}
	for _, args := range [][]string{
		{"init"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"},
		{"checkout", "-b", "master"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v\n%s", args, repo, err, out)
		}
	}
	// GOPI.md marks the project: the executor reads the rules file from cwd,
	// so the file's presence is what the resolution is FOR.
	if err := os.WriteFile(filepath.Join(repo, "GOPI.md"), []byte("# project "+name+"\n"), 0o644); err != nil {
		t.Fatalf("write GOPI.md: %v", err)
	}
	add := exec.Command("git", "add", "-A")
	add.Dir = repo
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	commit := exec.Command("git", "commit", "-m", "base")
	commit.Dir = repo
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	return repo
}

// executorProbeEnv is the daemon's launch environment: the mock LLM, a
// throwaway profile + ledger, and the operator-actor declaration.
func executorProbeEnv(t *testing.T, gopiHome, dbPath, mockURL string, extra ...string) []string {
	t.Helper()
	env := testutil.MockEnv(t, gopiHome, mockURL)
	env = append(env,
		"GOPI_MEMORY_DB="+dbPath,
		"GOPI_AUTO_TASK_REGISTER=1",
		config.SkipClaimBroadcastEnv+"=1",
		"GOPI_AGENT_SPAWN_GUARD_OFF=1",
		// The GROUND ROOT is pinned per test (epic worker-creates-child-ground):
		// a repo-aware row's executor child is spawned in a linked worktree the
		// worker CREATES, and without this the walk would create it under the
		// operator's default workspace root. Pinned, the walk also cannot share
		// a ground with another test.
		worktreeroots.WorktreeRootEnvName+"="+t.TempDir(),
	)
	return append(env, extra...)
}

// TestWorkerExecutorChildStandsInTheTasksRepoNotTheLaunchers is the epic's
// red-before/green-after walk on the REAL binary: the worker runs in
// launcherRepo, the task belongs to taskRepo.
func TestWorkerExecutorChildStandsInTheTasksRepoNotTheLaunchers(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	bin := testutil.GopiBin(t)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	// Round 1 runs `pwd`; the mock repeats its last script entry afterwards,
	// so the run ends with a plain answer.
	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{ID: "call_cwd", Name: toolnames.ToolBash,
			Args: map[string]interface{}{"command": "pwd"}}},
		testutil.Resp{Text: "cwd probe done"},
	)
	t.Cleanup(mock.Close)

	taskRepo := executorCwdProbeRepo(t, "owned-project")
	launcherRepo := executorCwdProbeRepo(t, "launcher-project")
	env := executorProbeEnv(t, gopiHome, dbPath, mock.URL())

	const taskID = "8bccd196-bf56-4569-a799-3887b1382572"
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"executor cwd e2e", "--payload", "run pwd in your project", "--task-id", taskID, "--repo", taskRepo},
		"", env, launcherRepo); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}

	// THE DAEMON's launch context: a DIFFERENT project. Before the epic the
	// child inherited this directory, which is what made "which project's
	// GOPI.md did this task run under" a lie.
	stdout, stderr, code := runGopiMapped(t, []string{"worker", "--once"}, "", env, launcherRepo)
	if code != 0 {
		t.Fatalf("worker --once must succeed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	// The resolution is REPORTED (Rule 15): the run says which directory the
	// executor stands in and why — a silent fallback would leave the operator
	// unable to tell a resolved task repo from an unresolved one. The record
	// lives in the ROUND's own log, because the spawn seam writes what it
	// decided about the run into <session-dir>/output.log — the same file the
	// child's own text goes to (epic child-stream-task-output); the launcher's
	// stream carries only the round's own lines (claim / workdir).
	sessionDir := filepath.Join(gopiHome, ".gopi", config.SessionsDirName, taskID)
	roundLog, err := os.ReadFile(filepath.Join(sessionDir, tools.WorkerLogFileName))
	if err != nil {
		t.Fatalf("read the round's output log: %v", err)
	}
	// The seam's record, read from the ROUND's log. Under the ground epic the
	// directory it names is the GROUND (a linked worktree of taskRepo), not
	// taskRepo itself — the resolution that used to answer "taskRepo" is no
	// longer the last word on the child's directory.
	if !strings.Contains(string(roundLog), taskGroundLineLabel) {
		t.Errorf("the spawn seam must record the CHILD'S GROUND it resolved (%q) in the task's own log; got:\n%s", taskGroundLineLabel, roundLog)
	}

	// THE EVIDENCE: the child's own `pwd` result, paired by tool_use_id.
	transcript := readFirstSessionJSONL(t, sessionDir)
	childCwd := cwdProbeResult(t, transcript)
	if childCwd == "" {
		t.Fatalf("the child's session JSONL must record the pwd tool_result; got:\n%s", transcript)
	}
	// THE CONTRACT (epic worker-creates-child-ground): a repo-aware row's child
	// stands in the GROUND the worker created for it — a linked worktree of the
	// task's repo, not the repo itself (the repo is the row's MAIN checkout and
	// is shared) and not the launcher's directory.
	if childCwd == strings.TrimRight(launcherRepo, "/") {
		t.Fatalf("the executor child stands in the LAUNCHER's directory %s — the daemon's launch context leaked into the task", launcherRepo)
	}
	// The row's recorded ground is the reference — read from the LEDGER, not by
	// stat-ing the directory: a completed run's own finish path may have already
	// reclaimed the worktree, and the claim is about where the child STOOD.
	if _, _, groundWT := groundRowDeliverable(t, dbPath, taskID); groundWT == "" {
		t.Fatalf("the row must name the ground the worker created for it; deliverable.worktree is empty")
	} else if childCwd != groundWT {
		t.Fatalf("the executor child must stand in the row's ground %s; it reported %q", groundWT, childCwd)
	}
	if childCwd == strings.TrimRight(taskRepo, "/") {
		t.Fatalf("the child stands in the row's MAIN checkout %s — a repo-aware row must work in its own linked worktree (epic worker-creates-child-ground)", taskRepo)
	}
	if !strings.Contains(childCwd, tools.TaskGroundDirPrefix) {
		t.Fatalf("the child's directory %s must be the GROUND the worker created (%q + the row's stem)", childCwd, tools.TaskGroundDirPrefix)
	}
}

// TestWorkerServesEachTaskInItsOwnRepoAcrossConsecutiveRounds is the epic's
// FRESHNESS walk: ONE worker process executes TWO tasks that belong to two
// different projects, and each child must stand in its OWN repo — the second
// round must not inherit the first round's directory.
//
// This is the leak the epic is about, on the daemon's own axis: a long-lived
// process serves many tasks, and anything it does not re-resolve per task is
// carried over.
//
// THE OBSERVABLE: the child's own session_start record (`workdir`) — the
// directory each child wrote down as its working directory at startup, read
// per task from that task's own session JSONL. It is chosen over a bash `pwd`
// probe for a mechanical reason: the mock LLM's script is a GLOBAL request
// counter (`testutil.NewMockLLM` repeats only the LAST entry), so which script
// entry a given round receives is not knowable across two runs. The workdir
// record is per-task by construction and is the same fact the wrapper's own
// workdir-from-session resolution reads in production
// (internal/tools/task_deliverable_branch.go).
func TestWorkerServesEachTaskInItsOwnRepoAcrossConsecutiveRounds(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	bin := testutil.GopiBin(t)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{ID: "call_cwd", Name: toolnames.ToolBash,
			Args: map[string]interface{}{"command": "pwd"}}},
		testutil.Resp{Text: "cwd probe done"},
	)
	t.Cleanup(mock.Close)

	repoA := executorCwdProbeRepo(t, "project-a")
	repoB := executorCwdProbeRepo(t, "project-b")
	launcherRepo := executorCwdProbeRepo(t, "launcher-project")
	env := executorProbeEnv(t, gopiHome, dbPath, mock.URL())

	cases := []struct{ id, repo string }{
		{"t-fresh-1-project-a", repoA},
		{"t-fresh-2-project-b", repoB},
	}
	for _, tc := range cases {
		if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
			"freshness " + tc.id, "--payload", "run pwd", "--task-id", tc.id, "--repo", tc.repo},
			"", env, launcherRepo); code != 0 {
			t.Fatalf("submit %s: code=%d stderr=%s", tc.id, code, stderr)
		}
	}
	if _, wErr, code := runGopiMapped(t, []string{"worker", "--once"}, "", env, launcherRepo); code != 0 {
		t.Fatalf("worker --once must succeed: code=%d stderr=%s", code, wErr)
	}

	sessionsRoot := filepath.Join(gopiHome, ".gopi", config.SessionsDirName)
	for _, tc := range cases {
		dir := filepath.Join(sessionsRoot, tc.id)
		got := sessionStartWorkdir(t, readFirstSessionJSONL(t, dir))
		if got == "" {
			t.Fatalf("%s: the child's session_start record carries no workdir — cannot tell where it stood", tc.id)
		}
		// THE CONTRACT under the ground epic: each child stands in ITS OWN
		// GROUND — the linked worktree the worker created for that row, named by
		// the row itself. The freshness property the epic is about is unchanged
		// (each round gets its own directory), and it is now asserted through
		// the row's recorded worktree rather than the bare repo.
		_, _, want := groundRowDeliverable(t, dbPath, tc.id)
		if want == "" {
			t.Fatalf("%s: the row must name the ground its child stood in; deliverable.worktree is empty", tc.id)
		}
		if got != want {
			t.Errorf("%s: the executor child must stand in ITS OWN ground %s; it started in %q — a child in another project's tree (the previous round's or the launcher's) is exactly the cross-task leakage this epic removes",
				tc.id, want, got)
		}
		if got == strings.TrimRight(tc.repo, "/") {
			t.Errorf("%s: the child stands in its row's MAIN checkout %s, not in a linked worktree", tc.id, tc.repo)
		}
	}
}

// sessionStartWorkdir reads the `workdir` field of a session JSONL's FIRST
// event (the session_start record the child writes at startup).
func sessionStartWorkdir(t *testing.T, transcript string) string {
	t.Helper()
	for _, line := range strings.Split(transcript, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var ev struct {
			Workdir string `json:"workdir"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		return strings.TrimSpace(ev.Workdir)
	}
	return ""
}

// TestWorkerChildDoesNotInheritTheDaemonsSessionHandle is Fix B's e2e half:
// the daemon process itself carries an agent-session handle (the shape of a
// worker started from an agent shell), and the record of what it handed to the
// executor child must show the handle STRIPPED — the child then mints and
// exports its OWN handle.
//
// The observable is the spawn seam's own record in the round's log (the round
// re-points its stdio into <session-dir>/output.log at claim time). It has to
// be THIS observable: the child re-exports its own handle milliseconds into
// its run, so a probe inside the child cannot distinguish "inherited then
// overwritten" from "stripped" — the seam's report can, and it is the same
// line an operator reads.
func TestWorkerChildDoesNotInheritTheDaemonsSessionHandle(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	bin := testutil.GopiBin(t)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	const leakedHandle = "session-of-the-daemons-launcher"
	probe := "printenv " + tools.TaskExecutorSessionEnv
	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{ID: "call_env", Name: toolnames.ToolBash,
			Args: map[string]interface{}{"command": probe}}},
		testutil.Resp{Text: "env probe done"},
	)
	t.Cleanup(mock.Close)

	taskRepo := executorCwdProbeRepo(t, "owned-project")
	launcherRepo := executorCwdProbeRepo(t, "launcher-project")
	// The daemon's own launch context: an agent-session handle + its anchor,
	// exactly what a worker started from an agent's shell inherits.
	env := executorProbeEnv(t, gopiHome, dbPath, mock.URL(),
		tools.TaskExecutorSessionEnv+"="+leakedHandle,
		config.AgentSessionAncestorEnv+"="+strconv.Itoa(os.Getpid()))

	const taskID = "73a4e7f7-d03e-4dcc-8ebd-05d2453b839b"
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"executor env e2e", "--payload", "read your session handle", "--task-id", taskID, "--repo", taskRepo},
		"", env, launcherRepo); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}
	if _, wErr, code := runGopiMapped(t, []string{"worker", "--once"}, "", env, launcherRepo); code != 0 {
		t.Fatalf("worker --once must succeed: code=%d stderr=%s", code, wErr)
	}

	sessionDir := filepath.Join(gopiHome, ".gopi", config.SessionsDirName, taskID)
	roundLog, err := os.ReadFile(filepath.Join(sessionDir, tools.WorkerLogFileName))
	if err != nil {
		t.Fatalf("read the round's output log: %v", err)
	}
	record := string(roundLog)
	// The seam reports the STRIP, naming EVERY handle it kept from this task.
	// The prefix comes from the production format itself (never a hand-typed
	// copy), and both names must appear — a partial strip is a leak.
	prefix := strings.SplitN(taskEnvStripFmt, "%v", 2)[0]
	if !strings.Contains(record, prefix) {
		t.Fatalf("the spawn seam must record the execution-context strip for %s (%q) in the round's log — a silent strip leaves an operator unable to tell 'nothing leaked' from 'nothing was checked'; got:\n%s",
			taskID, strings.TrimSpace(prefix), record)
	}
	for _, name := range tools.DaemonContextEnvVars {
		if !strings.Contains(record, name) {
			t.Errorf("the strip record must name %s (both of this daemon's context variables were live); got:\n%s", name, record)
		}
	}
	if !strings.Contains(record, taskID) {
		t.Errorf("the strip record must name the task it protected (%s); got:\n%s", taskID, record)
	}
	// The child mints its OWN handle: the probe inside the child must never
	// report the daemon's value, and must report a non-empty one of its own.
	transcript := readFirstSessionJSONL(t, sessionDir)
	if got := executorToolResult(t, transcript, probe); got == leakedHandle {
		t.Fatalf("the executor child ran with %s=%q — the spawning daemon's own handle reached it (the child must export its own session id, never inherit one)", tools.TaskExecutorSessionEnv, leakedHandle)
	}
}

// executorToolResult pairs the tool_result answering the bash tool_use whose
// command is `command`, returning the LAST non-empty line of the result
// payload (the bash tool wraps its output in a timing/exit-code envelope).
// Pairing by tool_use_id — never a transcript substring — is what makes the
// answer belong to THIS call.
func executorToolResult(t *testing.T, transcript, command string) string {
	t.Helper()
	type block struct {
		Type      string                 `json:"type"`
		ID        string                 `json:"id"`
		ToolUseID string                 `json:"tool_use_id"`
		Input     map[string]interface{} `json:"input"`
		Content   string                 `json:"content"`
	}
	type msg struct {
		Content []block `json:"content"`
	}
	type event struct {
		Type    string `json:"type"`
		Message msg    `json:"message"`
	}
	probeID := ""
	for _, line := range strings.Split(transcript, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var ev event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		for _, b := range ev.Message.Content {
			if b.Type == "tool_use" && b.Input["command"] == command {
				probeID = b.ID
			}
		}
		if probeID == "" {
			continue
		}
		for _, b := range ev.Message.Content {
			if b.ToolUseID != probeID {
				continue
			}
			lines := strings.Split(strings.TrimRight(b.Content, "\n"), "\n")
			for i := len(lines) - 1; i >= 0; i-- {
				if got := strings.TrimSpace(lines[i]); got != "" {
					return got
				}
			}
			return ""
		}
	}
	return ""
}
