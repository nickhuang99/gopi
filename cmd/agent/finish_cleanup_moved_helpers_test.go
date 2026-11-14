// Code generated from 58b5618^ (the revision before the finish-cleanup
// retirement, slice 3/4) by a one-off extraction — see the task report.
//
// WHY THESE LIVE HERE (Rule 43, the reference-distribution criterion): every
// declaration below used to sit inside the retired finish-cleanup mechanism, but
// it is NOT part of that mechanism. LIVE tests in this package still call each one
// BY NAME — that is the reference distribution that makes it a shared tool, and a
// shared tool moves out BEFORE its mechanism is deleted; it does not get deleted
// with it. They are carried here verbatim, from the parent revision.
//
// A symbol that no longer has a live reader does NOT belong here: this file is a
// restore of what the live tier still names, never a home for dead code.

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// resumeE2EJSONQuote renders a string as a JSON literal for the fixtures above
// (the blob the deliverable column must carry).
func resumeE2EJSONQuote(s string) string {
	b, _ := json.Marshal(s) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	return string(b)
}

// sessioneventTypeStartForTest is the session_start event name as the READER
// defines it (one source; never a re-typed literal — Rule 1b).
const sessioneventTypeStartForTest = sessionevent.EventTypeSessionStart

// workerResumeE2E builds the world the mode is for: a repo, a linked worktree
// on a feature branch carrying committed deliverable work, a ledger row naming
// both, and — the part that makes a resume POSSIBLE — a run directory for that
// row holding a run log whose session_start records the raw log, plus that raw
// log with one → REQUEST checkpoint.
//
// The run directory is written by the test rather than produced by a first run
// because the checkpoint IS the resumed session's input, and a live first run
// would need the very worktree this test is about.
func workerResumeE2E(t *testing.T) (taskID, repo, wt, branch, rawLog, gopiHome, dbPath string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	gitIn := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
		}
	}
	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	wt = filepath.Join(root, "wt")
	branch = "feat/worker-resume-e2e"
	gitIn(root, "init", repo)
	gitIn(repo, "config", "user.email", "t@t")
	gitIn(repo, "config", "user.name", "t")
	gitIn(repo, "checkout", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(repo, "add", "a.txt")
	gitIn(repo, "commit", "-m", "init")
	gitIn(repo, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, "deliverable.txt"), []byte("the work"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(repo, "add", "deliverable.txt")
	gitIn(repo, "commit", "-m", "deliverable work")
	gitIn(repo, "checkout", "master")
	gitIn(repo, "worktree", "add", wt, branch)

	taskID = "t-worker-resume-e2e"
	gopiHome = t.TempDir()
	dbPath = filepath.Join(t.TempDir(), "memory.db")

	// The row's run directory (<GOPI_HOME>/.gopi/sessions/<task_id>/), its run
	// log, and the raw
	// log the run log points at — the address `gopi session resume <task-id>` resolves
	// from, deliberately NOT passed on the command line by this mode.
	runDir := filepath.Join(gopiHome, config.ProfileDirName, "sessions", taskID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rawLog = filepath.Join(runDir, "raw.log")
	checkpoint := `{"model":"deepseek-v4-flash","max_tokens":16384,"messages":[` +
		`{"role":"user","content":[{"type":"text","text":"[EPIC GOAL]\nfinish the deliverable"}]},` +
		`{"role":"assistant","content":[{"type":"text","text":"working"}]}]}`
	if err := os.WriteFile(rawLog, []byte("→ REQUEST\n"+checkpoint+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The run log: ONE session_start whose rawLogPath names the checkpoint. The
	// JSON is built from the shared event type rather than a literal, so the
	// fixture cannot drift from what the reader looks for.
	runLog := filepath.Join(runDir, "run.jsonl")
	start := `{"type":"` + sessioneventTypeStartForTest + `","sessionId":"sess-WR-e2e","rawLogPath":` +
		resumeE2EJSONQuote(rawLog) + `,"model":"deepseek-v4-flash","timestamp":"2026-09-24T00:00:00Z"}`
	if err := os.WriteFile(runLog, []byte(start+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The row itself: a live-looking row whose deliverable names the ground.
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "worker resume e2e",
		Payload: "finish the deliverable",
		TaskID:  taskID,
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	blob := `{"repo":` + resumeE2EJSONQuote(repo) + `,"branch":` + resumeE2EJSONQuote(branch) + `,"worktree":` + resumeE2EJSONQuote(wt) + `}`
	db, err := openRawTasksDB(dbPath)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	if _, err := db.Exec(`UPDATE tasks SET status = 'in_progress', deliverable = ? WHERE task_id = ?`, blob, taskID); err != nil {
		t.Fatalf("shape row: %v", err)
	}
	db.Close() //stylegate:ignore the fixture's own handle, closed immediately
	return taskID, repo, wt, branch, rawLog, gopiHome, dbPath
}
