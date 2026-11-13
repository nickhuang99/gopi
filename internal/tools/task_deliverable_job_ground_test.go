package tools

// The BACKGROUND-JOB half of the task↔branch/worktree linkage, and the
// in-place branch reader it needs.
//
// THE MEASURED GAP (epic "task should record its branch and worktree", live
// ledger 2026-09-18): the creation-time ground seam is wired to the BASH tool
// only (internal/agent loop.go), so a run whose worktree/branch is created by
// a DETACHED job — `job(action=start, command="… git worktree add <wt> -b
// <branch> …")`, or the `git checkout -b` variant — recorded NO ground while
// the row stayed repo-only, and the terminal writeback was blind to it too
// (taskWorktreeAddBranchFromSession scanned bash lines only). Measured: 47
// job-start ground-creating commands in the 400 most recent sessions, and row
// 01a0aa0f-f6e2 left a completed wrapper with deliverable {"repo":…} only.
//
// What these tests pin:
//
//	(a) taskGroundCommandBlock — bash AND job-start carry a readable command;
//	    a job's output/kill action does not (it names no command).
//	(b) TaskDeliverableWorktreeJobForRow — a job command that creates a linked
//	    worktree stamps worktree+branch onto the row, through the SAME writer
//	    and ownership guards as the bash seam.
//	(c) taskBranchCreatedFromCommand — the in-place creation reader, with its
//	    directory gate (a creation in ANOTHER repo's worktree is not this
//	    row's branch — the hijack the worktree axis already refuses).
//	(d) taskGroundFromSession / taskWritebackBranch — a child session whose
//	    ground was created by a job and never resolvable from its launch dir
//	    still yields the branch at writeback time.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/toolnames"
)

// jobGroundSeedSession writes a child session JSONL into a fresh gopi home:
// the session_start line (workdir = the repo's main checkout — the executor
// shape whose HEAD is exempt, which is exactly why the launch dir resolves
// nothing), then the given raw JSONL lines verbatim.
func jobGroundSeedSession(t *testing.T, workdir string, lines ...string) string {
	t.Helper()
	sessHome := t.TempDir()
	t.Setenv("HOME", sessHome)
	// GOPI_HOME (pinned by testutil.PinSuiteGopiProfile) wins over HOME for
	// config.GopiDir — name it explicitly, as the sibling branch tests do.
	t.Setenv(config.GopiDirEnv, sessHome)
	sessDir := filepath.Join(sessHome, ".gopi", "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessID := "child-job-ground-1"
	body := `{"type":"session_start","sessionId":"child-job-ground-1","model":"glm-exec","timestamp":"x","workdir":"` + workdir + `"}` + "\n"
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(filepath.Join(sessDir, sessID+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return sessID
}

// jobGroundToolUseLine renders one assistant tool_use line the way the session
// logger writes it (the scans read these bytes, so the fixture must be the
// real serialized shape, not a hand-built struct).
func jobGroundToolUseLine(name, action, command string) string {
	in := map[string]string{"command": command}
	if action != "" {
		in["action"] = action
	}
	blob, err := json.Marshal(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []map[string]any{{
				"type":  "tool_use",
				"name":  name,
				"input": in,
			}},
		},
	})
	if err != nil {
		panic(err)
	}
	return string(blob)
}

// TestJobGroundCommandBlockReadsBashAndJobStart pins (a): the ONE reader that
// decides which tool calls carry readable ground — bash always, a job call
// only for its start action (output/kill name no command, and a job result
// echo is not a creation).
func TestJobGroundCommandBlockReadsBashAndJobStart(t *testing.T) {
	cases := []struct {
		name, action, command, want string
		ok                          bool
		why                         string
	}{
		{toolnames.ToolBash, "", "git worktree add /w -b feat/x", "git worktree add /w -b feat/x", true, "the bash round (unchanged)"},
		{toolnames.ToolJob, toolnames.ToolJobActionStart, "git worktree add /w -b feat/x", "git worktree add /w -b feat/x", true, "the detached creation (the gap this closes)"},
		{toolnames.ToolJob, toolnames.ToolJobActionOutput, "job-1", "", false, "reading a job's output creates nothing"},
		{toolnames.ToolJob, toolnames.ToolJobActionKill, "job-1", "", false, "killing a job creates nothing"},
		{toolnames.ToolBash, "", "", "", false, "an empty command carries no ground"},
		{toolnames.ToolJob, toolnames.ToolJobActionStart, "", "", false, "an empty job command carries no ground"},
		{toolnames.ToolRead, "", "git worktree add /w -b feat/x", "", false, "only the two command tools are ground-bearing"},
	}
	for _, c := range cases {
		got, ok := taskGroundCommandBlock(c.name, c.action, c.command)
		if got != c.want || ok != c.ok {
			t.Errorf("taskGroundCommandBlock(%q, %q, %q) = (%q, %v), want (%q, %v) (%s)",
				c.name, c.action, c.command, got, ok, c.want, c.ok, c.why)
		}
	}
}

// TestJobGroundSeamStampsWorktree pins (b) end to end: a job(action=start)
// command that creates a linked worktree of the row's repo stamps
// deliverable.worktree + deliverable.branch — the same two axes the bash seam
// stamps, written by the same ONE writer.
//
// RED BEFORE THE FIX: the job command never reached any seam (the only caller
// was the bash round), so the row stayed repo-only.
func TestJobGroundSeamStampsWorktree(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	taskID := "t-wt-job-seam"
	deliverableWTSeedWrapperRow(t, db, taskID, f.clientDir)

	// The detached creation: a SECOND worktree, created by a job command.
	wt2 := filepath.Join(f.root, "wt-job")
	deliverableWTGit(t, f.clientDir, "worktree", "add", "-b", "feat/job-ground", wt2, "master")
	TaskDeliverableWorktreeJobForRow(taskID, "cd "+f.clientDir+" && git worktree add -b feat/job-ground "+wt2+" master")

	var d struct {
		Branch   string `json:"branch"`
		Worktree string `json:"worktree"`
	}
	if err := json.Unmarshal([]byte(deliverableWTRowBlob(t, db, taskID)), &d); err != nil {
		t.Fatalf("decode blob: %v", err)
	}
	if d.Branch != "feat/job-ground" || d.Worktree != wt2 {
		t.Fatalf("the job seam must record the ground a background job created: branch=%q worktree=%q (want feat/job-ground / %s)", d.Branch, d.Worktree, wt2)
	}
}

// TestJobGroundSeamStampsInPlaceBranch pins the BRANCH-ONLY shape of the same
// seam: a job that creates the branch in the repo's MAIN checkout (no worktree
// at all — `git -C <repo> checkout -b feat/x`) records the branch, and the
// worktree axis stays empty. This is the shape the row 01a0aa0f family left
// with NO information whatsoever.
func TestJobGroundSeamStampsInPlaceBranch(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	taskID := "t-branch-job-seam"
	deliverableWTSeedWrapperRow(t, db, taskID, f.clientDir)

	// The in-place creation, run by the job: HEAD of the main checkout moves
	// onto a fresh branch.
	deliverableWTGit(t, f.clientDir, "checkout", "-b", "feat/job-inplace")
	TaskDeliverableWorktreeJobForRow(taskID, "git -C "+f.clientDir+" checkout -b feat/job-inplace")

	var d struct {
		Branch   string `json:"branch"`
		Worktree string `json:"worktree"`
	}
	if err := json.Unmarshal([]byte(deliverableWTRowBlob(t, db, taskID)), &d); err != nil {
		t.Fatalf("decode blob: %v", err)
	}
	if d.Branch != "feat/job-inplace" {
		t.Fatalf("the job seam must record the in-place branch: branch=%q (want feat/job-inplace)", d.Branch)
	}
	if d.Worktree != "" {
		t.Fatalf("an in-place branch has no worktree — the axis must stay empty, got %q", d.Worktree)
	}
}

// TestBranchCreatedFromCommandReadsOnlyOwnGround pins (c): the in-place
// creation reader returns the branch a command creates ON repo's ground, and
// refuses one created elsewhere. The foreign case is the deliverable hijack
// the worktree axis already refuses (2026-09-12) in its branch-only spelling:
// a routine cross-task inspection must not re-point this row at another task's
// branch.
func TestBranchCreatedFromCommandReadsOnlyOwnGround(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	other := filepath.Join(t.TempDir(), "other-task-wt")
	cases := []struct {
		cmd, want, why string
	}{
		{"git checkout -b feat/x", "feat/x", "unnamed dir = the caller's workdir, which is repo"},
		{"git -C " + repo + " checkout -b feat/y", "feat/y", "git's own -C form"},
		{"cd " + repo + " && git switch -c feat/z", "feat/z", "the measured live shape"},
		{"cd " + repo + "/internal/tools && git checkout -b feat/sub", "feat/sub", "a subdirectory of the repo is still its ground"},
		{"cd " + other + " && git checkout -b feat/foreign", "", "another task's worktree is not this row's ground"},
		{"git -C " + other + " switch -c feat/foreign", "", "the -C spelling of the same hijack"},
		{"git checkout -b master", "", "an exempt name is never a deliverable"},
		{"git checkout -- file.txt", "", "a file restore is not a creation"},
		{"git switch feat/x", "", "switching to an EXISTING branch creates nothing"},
		{"git commit -m 'x'", "", "a commit names no branch"},
		{"echo checkout -b feat/nope", "", "prose is not a command"},
	}
	for _, c := range cases {
		if got := taskBranchCreatedFromCommand(c.cmd, repo, repo); got != c.want {
			t.Errorf("taskBranchCreatedFromCommand(%q) = %q, want %q (%s)", c.cmd, got, c.want, c.why)
		}
	}
}

// TestJobGroundWritebackResolvesBranchFromJobCommand pins (d): the TERMINAL
// resolution. The child launched from the repo's main checkout (HEAD exempt —
// the launch-dir path resolves nothing), it created its branch in place inside
// a JOB command, and the shared session scan returns that branch — which
// taskWritebackBranch then stamps on the completed row.
//
// RED BEFORE THE FIX: taskWorktreeAddBranchFromSession scanned bash lines
// only, and it looked for `worktree add` alone — a job-created in-place branch
// was invisible, so the completed row carried no branch for the merger to
// attribute (its commits read as `unowned`).
func TestJobGroundWritebackResolvesBranchFromJobCommand(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)

	// The child's delivery: a branch created in place (no linked worktree).
	const branch = "feat/job-writeback"
	deliverableWTGit(t, f.clientDir, "checkout", "-b", branch)

	sessID := jobGroundSeedSession(t, f.clientDir,
		jobGroundToolUseLine(toolnames.ToolJob, toolnames.ToolJobActionStart, "git -C "+f.clientDir+" checkout -b "+branch))
	t.Setenv(TaskChildSessionEnv, sessID)

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "job-created ground", Repo: f.clientDir})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := adoptionTaskID(t, out)
	if _, err := TaskUpdate(taskID, TaskStatusInProgress); err != nil {
		t.Fatalf("TaskStart seam: %v", err)
	}
	if got := taskWritebackBranch(); got != branch {
		t.Fatalf("the writeback resolver must read the branch a job created: got %q, want %q", got, branch)
	}
	if _, err := TaskRunWriteback(taskID, TaskStatusCompleted, "done — run: elapsed=1s"); err != nil {
		t.Fatalf("TaskRunWriteback: %v", err)
	}
	blob := deliverableWTRowBlob(t, db, taskID)
	got, err := taskDeliverableBranch(blob)
	if err != nil {
		t.Fatalf("deliverable parse: %q: %v", blob, err)
	}
	if got != branch {
		t.Fatalf("completed row deliverable.branch = %q, want %q (blob %s)", got, branch, blob)
	}
}

// TestJobGroundSessionScanReadsBothHalves pins the ONE-read contract of the
// shared session scan: one pass returns BOTH the worktree-add branch (bash or
// job) and the in-place created branch, latest wins in each axis.
func TestJobGroundSessionScanReadsBothHalves(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	deliverableWTGit(t, repo, "init", "-b", "master")
	deliverableWTGit(t, repo, "config", "user.email", "t@t")
	deliverableWTGit(t, repo, "config", "user.name", "t")
	os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644)
	deliverableWTGit(t, repo, "add", "base.txt")
	deliverableWTGit(t, repo, "commit", "-m", "base")

	wt := filepath.Join(t.TempDir(), "wt")
	deliverableWTGit(t, repo, "worktree", "add", "-b", "feat/wt-half", wt, "master")
	deliverableWTGit(t, repo, "checkout", "-b", "feat/inplace-half")

	sessID := jobGroundSeedSession(t, repo,
		jobGroundToolUseLine(toolnames.ToolJob, toolnames.ToolJobActionStart, "git -C "+repo+" worktree add -b feat/wt-half "+wt+" master"),
		jobGroundToolUseLine(toolnames.ToolJob, toolnames.ToolJobActionOutput, "job-1"),
		jobGroundToolUseLine(toolnames.ToolJob, toolnames.ToolJobActionStart, "git -C "+repo+" checkout -b feat/inplace-half"),
	)
	add, created := taskGroundFromSession(sessID, repo)
	if add != "feat/wt-half" {
		t.Errorf("worktree-add reading = %q, want feat/wt-half", add)
	}
	if created != "feat/inplace-half" {
		t.Errorf("in-place creation reading = %q, want feat/inplace-half", created)
	}
	// A repo-less call (no ground reference) must NOT guess the in-place
	// reading — the directory gate cannot answer "was it created HERE?".
	if _, createdNoRepo := taskGroundFromSession(sessID, ""); createdNoRepo != "" {
		t.Errorf("without a repo the created-branch reading must stay empty, got %q", createdNoRepo)
	}
}

// TestJobGroundSeamWithoutRowIsNoop guards the ledger half: a job command with
// no row to pair (a plain chat session) writes nothing — the seam never
// invents a row, exactly like the bash caller.
func TestJobGroundSeamWithoutRowIsNoop(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	t.Setenv(TaskWrapperEnv, "")
	TaskDeliverableWorktreeJobForRow("", "git worktree add -b feat/none "+f.wtDir+" master")
	ctx := context.Background()
	conn, err := openTasksDB(ctx, db)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer conn.Close()
	var n int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("a row-less job seam must write nothing, found %d rows", n)
	}
}
