package main

// The `gopi task show` face's advisory ground line (epic 01a0bb4d follow-up,
// gap B) — asserted at the CLI ENTRY, not only in the tools package, because
// the epic's acceptance is what the OPERATOR sees when they type the verb.
//
// The resolution itself, its guards and its why-suffixes are unit-tested in
// internal/tools (task_ground_advisory_test.go); what this file pins is the
// WIRING: runTaskShow — the function the show verb calls — carries the line,
// and a row that recorded nothing still gets it.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// showVerbRowID names the fixture row both show-verb tests address. It is a
// named constant (Rule 1b) because the id is used as a PATH ELEMENT — the row's
// own run directory — and as the ledger address, so a second spelling would
// silently split the fixture in two.
const showVerbRowID = "t-show-verb-advisory"

// TestTaskShowVerbRendersAdvisoryGroundForAnUnrecordedRow drives the CLI's own
// show entry on a row whose RECORDED deliverable is repo-only while its session
// names the worktree and branch it created. The recorded block must stay
// repo-only and the advisory line must carry the resolved ground — the epic's
// headline acceptance, observed at the operator's face.
func TestTaskShowVerbRendersAdvisoryGroundForAnUnrecordedRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)

	// A real repo + linked worktree fixture (git truth — the resolution reads
	// `git worktree list` and verifies the branch with git).
	repo := filepath.Join(t.TempDir(), "repo")
	wt := filepath.Join(t.TempDir(), "workspace", "gopi-show-ground")
	branch := "feat/show-verb-advisory"
	showVerbGit(t, filepath.Dir(repo), "init", repo)
	showVerbGit(t, repo, "config", "user.email", "t@t")
	showVerbGit(t, repo, "config", "user.name", "t")
	showVerbGit(t, repo, "checkout", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	showVerbGit(t, repo, "add", "a.txt")
	showVerbGit(t, repo, "commit", "-m", "base")
	showVerbGit(t, repo, "worktree", "add", "-b", branch, wt, "master")

	// The run's session: session_start (main checkout) + the worktree-add.
	//
	// THE RECORD LIVES IN THE ROW'S OWN RUN DIRECTORY
	// (`sessions/<task-id>/<session>.jsonl`), not flat beside it. That is where
	// a run's own log is written, and it is the ONLY address the session stamp
	// accepts: `taskSessionStampRefusal` refuses an id whose record sits at
	// another home, including the flat root a chat session occupies
	// (internal/tools/task_session_stamp.go). A fixture that wrote the flat
	// spelling could only reach the row's session cell by bypassing the
	// production seam — which is exactly what this test must not do.
	gd, err := config.GopiDir()
	if err != nil {
		t.Fatalf("gopi dir: %v", err)
	}
	sessDir := filepath.Join(gd, config.SessionsDirName, showVerbRowID)
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessID := "sess-show-verb-advisory"
	body := `{"type":"session_start","sessionId":"` + sessID + `","model":"m","timestamp":"x","workdir":"` + repo + `"}` + "\n" +
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"` + toolnames.ToolBash +
		`","input":{"command":"cd ` + repo + ` \u0026\u0026 git worktree add ` + wt + ` -b ` + branch + `"}}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(sessDir, sessID+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	// The row: repo-only deliverable (nothing was recorded by the run itself).
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary:   "show verb advisory fixture",
		TaskID:    showVerbRowID,
		Repo:      repo,
		Source:    "human",
		SessionID: sessID,
	}); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	// THE RUN SESSION IS STAMPED BY THE PRODUCTION SEAM (2026-10-29, e71b998):
	// TaskSubmit's SessionID is the row's SUBMITTER (its own column), and the
	// run-session cell is left EMPTY for the run that will fill it — so a
	// fixture that stops at TaskSubmit seeds a row with no session at all, and
	// the advisory line has nothing to resolve from (the pre-fix reading of
	// this test). TaskBackfillSession is the writer that fills it, refusing any
	// id whose record is not in the row's own run directory.
	if err := tools.TaskBackfillSession(showVerbRowID, sessID); err != nil {
		t.Fatalf("stamp the row's run session: %v", err)
	}

	out, err := runTaskShow(&tasksOptions{taskID: showVerbRowID})
	if err != nil {
		t.Fatalf("runTaskShow: %v", err)
	}
	if !strings.Contains(out, tools.TaskShowGroundLabel) {
		t.Fatalf("the show VERB must render %q:\n%s", tools.TaskShowGroundLabel, out)
	}
	if !strings.Contains(out, branch) {
		t.Errorf("the advisory line must carry the resolved branch %q:\n%s", branch, out)
	}
	// The RECORDED deliverable block stays as recorded: no worktree line.
	if strings.Contains(out, tools.TaskShowDeliverableWorktreeLabel) {
		t.Errorf("the advisory line must not write the recorded deliverable:\n%s", out)
	}
	// …and the ledger's own blob is byte-identical after the show: the CLI
	// gate, not only the tools-level one.
	if blob := showVerbRowBlob(t, showVerbRowID); strings.Contains(blob, "worktree") {
		t.Errorf("the show VERB changed the recorded deliverable: %s", blob)
	}
}

// TestTaskShowVerbNeverFailsOnABrokenAdvisory: the verb's error path is for
// USAGE and LEDGER failures only. A row whose advisory resolution cannot read
// anything still returns its digest (Rule 15: the absence is rendered on the
// line, never as a failure of the show).
func TestTaskShowVerbNeverFailsOnABrokenAdvisory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)
	// A real repo (submit verifies it — a non-repo aborts the submit) whose
	// session does NOT exist: the advisory has nothing to read and must
	// degrade to its why-line, never fail the verb.
	repo := filepath.Join(t.TempDir(), "repo")
	showVerbGit(t, filepath.Dir(repo), "init", repo)
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary:   "no session, no ground",
		TaskID:    "t-show-verb-broken",
		Repo:      repo,
		Source:    "human",
		SessionID: "ghost-session-that-does-not-exist",
	}); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	out, err := runTaskShow(&tasksOptions{taskID: "t-show-verb-broken"})
	if err != nil {
		t.Fatalf("a broken advisory resolution must never fail the verb: %v", err)
	}
	if !strings.Contains(out, "t-show-verb-broken") {
		t.Fatalf("the row's digest must still render:\n%s", out)
	}
}

// showVerbGit runs git for the fixture, failing the test on error.
func showVerbGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
	}
}

// showVerbRowBlob reads a row's raw deliverable blob from the SANDBOXED ledger
// (the same DB the binary under test writes to), without touching the engine.
func showVerbRowBlob(t *testing.T, taskID string) string {
	t.Helper()
	db := ledgerDB(t)
	var blob string
	if err := db.QueryRowContext(context.Background(),
		`SELECT deliverable FROM tasks WHERE task_id = ?`, taskID).Scan(&blob); err != nil {
		t.Fatalf("read deliverable for %s: %v", taskID, err)
	}
	return blob
}
