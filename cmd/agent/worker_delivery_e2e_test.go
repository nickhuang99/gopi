package main

// worker_delivery_e2e_test.go — the CLOSE-OUT's delivery leg on a CONTINUATION,
// on the real binary (bug 01a193f8, measured 2026-10-31 on rows 01a190bb @05:57
// and 01a190b4 @06:02).
//
// THE DEFECT THIS FILE PINS. `gopi worker --resume <row>` runs
// `gopi session resume` in a child, and that child is a CONTINUATION: it mints no
// row, so loop.go's start hook — which registered a row AND adopted the clone
// ground — was skipped whole. Three layers chained from that one gap:
//
//	cloneGroundDir stayed ""  →  CloneRunGround() answered ("", "") — the
//	                             WORKTREE channel's answer
//	                          →  the close-out's delivery leg took its
//	                             COMPATIBILITY arm: no attempt, no `push
//	                             accepted`, no `delivery did not land`
//	                          →  the end hook wrote `completed` on a claim that had
//	                             never tried to publish; the completion witness
//	                             recorded `witnessless`, which by contract changes
//	                             no status.
//
// The row read `completed`, the commits sat in the clone, and the source repo's
// `git ls-remote` had never heard of them. The control group — same host, same
// day, a QUEUE-launched `gopi worker` with no --resume — pushed normally, which
// is what located the defect in the resume leg rather than in the environment.
//
// WHAT THIS FILE MEASURES, end to end on the shipped binary:
//
//	BEFORE: the source repo's master tip, and that the clone stands ahead of it.
//	RUN:    `gopi worker --resume <row>` over a row whose recorded ground IS that
//	        clone, with a scripted executor that makes a real commit there.
//	AFTER:  the source repo's master MOVED to the run's commit, and the operator's
//	        stderr names the verdict (`delivery delivered`).
//
// The two facts are asserted SEPARATELY and neither implies the other: the commit
// arriving is the work landing, the line on stderr is the run saying which way it
// went. A run that pushed silently and a run that reported a delivery it did not
// make are both failures (Rule 15).

import (
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

const (
	// workerResumeDeliveryTaskID is the fixture row: a task id (not a session
	// UUID), which is what makes the resume target resolve through the ROW.
	workerResumeDeliveryTaskID = "t-worker-resume-delivery-e2e"
	// workerResumeDeliveryRunName is the fixture run's id — the log's base name.
	// The run DIRECTORY is named by the row, the log by the run.
	workerResumeDeliveryRunName = "bbbbbbbb-1111-2222-3333-444444444444"
	// workerResumeDeliverySubject is the commit the scripted executor makes in
	// the clone; it is what the source repo's master must carry afterwards.
	workerResumeDeliverySubject = "worker resume delivery e2e"
)

// workerResumeDeliveryFixture builds the two-repo shape the channel works in:
// `repo` is the SOURCE (a non-bare local checkout standing in for the remote,
// with receive.denyCurrentBranch=ignore so a push into it is accepted) and
// `ground` is a CLONE of it — a directory whose `.git` is a DIRECTORY, which is
// the discriminator the adaptation reads.
//
// The clone stands on `task/<row>` with ONE commit the source does not have: that
// is the work a continuation must publish.
func workerResumeDeliveryFixture(t *testing.T) (repo, ground, branch string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	repo = filepath.Join(root, "source-repo")
	ground = filepath.Join(root, "ground")
	branch = "task/" + strings.ReplaceAll(workerResumeDeliveryTaskID, "-", "")

	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	rawPromptGit(t, repo, "init", "-q", "-b", masterBranchName)
	rawPromptGit(t, repo, "config", "receive.denyCurrentBranch", "ignore")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rawPromptGit(t, repo, "add", "a.txt")
	rawPromptGit(t, repo, "commit", "-q", "-m", "init")

	// THE GROUND IS A CLONE — `git clone` is the only way to make one, so the
	// fixture measures the real shape rather than hand-building a `.git` dir.
	rawPromptGit(t, root, "clone", "-q", repo, ground)
	rawPromptGit(t, ground, "checkout", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(ground, "work.txt"), []byte("the continuation's work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rawPromptGit(t, ground, "add", "work.txt")
	rawPromptGit(t, ground, "commit", "-q", "-m", "work only the clone has")
	return repo, ground, branch
}

// seedWorkerResumeDeliveryRow creates the row the continuation resumes: a
// TERMINAL-shape residue (the first run died) whose deliverable names the clone,
// so `resumeChdirToWorktree` — the ONE reader of deliverable.worktree — puts the
// child in the ground whose delivery this file measures.
func seedWorkerResumeDeliveryRow(t *testing.T, dbPath, repo, ground, branch, gopiHome string) {
	t.Helper()
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "worker resume delivery e2e",
		Payload: "make the change and let the close-out publish it",
		TaskID:  workerResumeDeliveryTaskID,
		Source:  "human",
		Tags:    "e2e",
		Repo:    repo,
	}); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	db, err := openRawTasksDB(dbPath)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close() //stylegate:ignore the fixture's own handle, closed when this seed returns
	blob, merr := json.Marshal(map[string]string{
		tools.DeliverableWorktreeKey: ground,
		"branch":                     branch,
		"repo":                       repo,
	})
	if merr != nil {
		t.Fatalf("marshal deliverable: %v", merr)
	}
	if _, err := db.Exec(
		`UPDATE tasks SET deliverable = ?, status = 'failed', ended_at = ?, reason = ? WHERE task_id = ?`,
		string(blob), float64(resumeDeadRunEndedAt), resumeDeadRunReason, workerResumeDeliveryTaskID,
	); err != nil {
		t.Fatalf("shape row: %v", err)
	}
	writeWorkerResumeDeliveryRunLog(t, gopiHome)
}

// writeWorkerResumeDeliveryRunLog mints the row's run directory and the raw log
// the resume target resolution reads — the row's OWN run, never a substituted
// session (taskproc's measured defect, 2026-10-28).
func writeWorkerResumeDeliveryRunLog(t *testing.T, gopiHome string) {
	t.Helper()
	runDir := filepath.Join(gopiHome, config.ProfileDirName, config.SessionsDirName, workerResumeDeliveryTaskID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rawPath := filepath.Join(runDir, workerResumeDeliveryRunName+"-raw.log")
	if err := os.WriteFile(rawPath, []byte("→ REQUEST\n"+checkpointBody()+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rawJSON, err := json.Marshal(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"type":"session_start","sessionId":"22222222-3333-4444-5555-666666666666","model":"deepseek-v4-flash","rawLogPath":` + string(rawJSON) + `,"timestamp":"2026-10-31T05:50:00Z"}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"make the change and let the close-out publish it"}]}}`,
		`{"type":"session_end","elapsed_sec":9.5,"total_iterations":2,"total_tokens":1200,"exit_code":0}`,
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(runDir, workerResumeDeliveryRunName+sessionlayout.JSONLSuffix), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestWorkerResumePublishesItsCommits is the acceptance walk: the row's commits
// must REACH the source repo, and the run must SAY SO.
//
// RED BEFORE THE FIX, measured on the shipped binary: master does not move, and
// the stderr carries neither `push accepted` nor `delivery did not land` — the
// leg never ran. To re-take the red, delete the adoptContinuationCloneGround call
// in internal/agent/loop.go's resume arm, rebuild bin/gopi, and run this test.
func TestWorkerResumePublishesItsCommits(t *testing.T) {
	bin := testutil.GopiBin(t)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)

	repo, ground, branch := workerResumeDeliveryFixture(t)
	before := rawPromptGit(t, repo, "log", "--oneline", "-1")
	t.Logf("BEFORE: source master = %q; ground %s on %s", before, ground, branch)

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	root := filepath.Join(t.TempDir(), "workspace")
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	// Rule 12: the build is DECLARED, never discovered. `true` is a real command
	// with a real exit status, so the leg pushes nothing it did not run.
	body := `{"sync_peers":[],"dev_repo":"` + repo + `","workspace_root":"` + root +
		`","repos":{"` + repo + `":{"build":"true"}}}`
	if err := os.WriteFile(settingsPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	// THE SCRIPTED EXECUTOR: one real commit in the directory it stands in —
	// which IS the ground, because the resume chdir'd there.
	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{ID: "call_commit", Name: toolnames.ToolBash,
			Args: map[string]interface{}{"command": "printf 'published by a continuation\\n' >> a.txt && " +
				"git add -A && git -c user.email=t@t -c user.name=t commit -q -m '" + workerResumeDeliverySubject + "'"}}},
		testutil.Resp{Text: "committed in the ground the continuation stands in"},
	)
	t.Cleanup(mock.Close)

	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env,
		"GOPI_BINARY="+bin,
		"GOPI_MEMORY_DB="+dbPath,
		config.SettingsEnv+"="+settingsPath,
		config.SkipClaimBroadcastEnv+"=1",
		"GOPI_SKIP_TERMINAL_PUSH=1",
		"GOPI_CRASH_DUMP_SWEEP=off",
		// The channel's knobs are ENV-first; this run must not inherit the
		// host's own dev_repo (which would publish into the tree under test).
		config.DevRepoEnvName+"="+repo,
		config.WorkspaceRootEnvName+"="+root,
	)
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	seedWorkerResumeDeliveryRow(t, dbPath, repo, ground, branch, gopiHome)

	// cwd = a directory OUTSIDE every repo: `gopi worker --resume` is the face
	// that does NOT stand in the ground itself (the child does).
	launchDir := t.TempDir()
	stdout, stderr, code := runGopiMapped(t,
		[]string{"worker", "--resume", workerResumeDeliveryTaskID}, "", env, launchDir)
	t.Logf("run exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)

	// ① THE WORK LANDED. This is the fact the bug destroyed: the row read
	//    `completed` while this line still said the pre-run tip.
	after := rawPromptGit(t, repo, "log", "--oneline", "-1")
	t.Logf("AFTER: source master = %q", after)
	if after == before {
		t.Fatalf("the source repo's master did not move — the continuation's commits stayed in the clone exactly as rows 01a190bb / 01a190b4 reported\nbefore=%q\nafter=%q\nstderr:\n%s",
			before, after, stderr)
	}
	if !strings.Contains(after, workerResumeDeliverySubject) {
		t.Fatalf("the new tip must BE the run's commit, got %q\nstderr:\n%s", after, stderr)
	}

	// ② THE RUN SAID WHICH WAY IT WENT. A silent push is the other half of the
	//    defect (Rule 15): the absence of either verdict is what made the
	//    original two samples unreadable from their logs.
	if !strings.Contains(stderr, "clone channel: delivery delivered") &&
		!strings.Contains(stderr, "clone channel: delivery BLOCKED") &&
		!strings.Contains(stderr, "clone channel: delivery BROKEN") {
		t.Fatalf("the run must report its delivery verdict on stderr — neither `push accepted` nor a non-delivery reason appeared, which is the exact shape of the defect:\n%s", stderr)
	}
	if !strings.Contains(stderr, "clone channel: delivery delivered") {
		t.Fatalf("the push LANDED (master moved) but the leg did not say `delivered`:\n%s", stderr)
	}
}

// TestWorkerResumeWithNothingToPublishConcludes is the CONTROL arm: a
// continuation whose ground holds nothing past the integration point must end on
// the channel's MEASURED CONCLUSION (`nothing to deliver`), not on a failed
// delivery and not on an empty report.
//
// It is the arm that keeps the fix from over-reaching: arming the leg must not
// turn every zero-commit resume into a non-delivery (bug 01a182a8's shape).
func TestWorkerResumeWithNothingToPublishConcludes(t *testing.T) {
	bin := testutil.GopiBin(t)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)

	repo, ground, branch := workerResumeDeliveryFixture(t)
	// WIND THE GROUND BACK to the integration point: the branch exists, the
	// clone is a clone, and there is no work past master to publish.
	rawPromptGit(t, ground, "reset", "-q", "--hard", "origin/master")

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	root := filepath.Join(t.TempDir(), "workspace")
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	body := `{"sync_peers":[],"dev_repo":"` + repo + `","workspace_root":"` + root +
		`","repos":{"` + repo + `":{"build":"true"}}}`
	if err := os.WriteFile(settingsPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	// The executor makes NO commit: it answers, and the close-out asks the
	// channel about an empty ground.
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "nothing to change here"})
	t.Cleanup(mock.Close)

	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env,
		"GOPI_BINARY="+bin,
		"GOPI_MEMORY_DB="+dbPath,
		config.SettingsEnv+"="+settingsPath,
		config.SkipClaimBroadcastEnv+"=1",
		"GOPI_SKIP_TERMINAL_PUSH=1",
		"GOPI_CRASH_DUMP_SWEEP=off",
		config.DevRepoEnvName+"="+repo,
		config.WorkspaceRootEnvName+"="+root,
	)
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	seedWorkerResumeDeliveryRow(t, dbPath, repo, ground, branch, gopiHome)

	before := rawPromptGit(t, repo, "log", "--oneline", "-1")
	stdout, stderr, code := runGopiMapped(t,
		[]string{"worker", "--resume", workerResumeDeliveryTaskID}, "", env, t.TempDir())
	t.Logf("run exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)

	if after := rawPromptGit(t, repo, "log", "--oneline", "-1"); after != before {
		t.Fatalf("an EMPTY ground must publish nothing, but master moved %q → %q\nstderr:\n%s", before, after, stderr)
	}
	if !strings.Contains(stderr, "nothing to deliver") {
		t.Fatalf("an empty ground is a CONCLUSION (`nothing to deliver`), not silence and not a non-delivery:\n%s", stderr)
	}
	// ③ THE STATUS THE CLOSE-OUT WROTE (epic resume-zero-commit). The leg's
	//    conclusion is unchanged — that is what ② pins — but the row must NOT
	//    wear `completed`: this resume was dispatched against a FAILED row and
	//    produced no commit, so nothing it was sent to repair was touched, and
	//    `completed` would claim the opposite (rows 01a18377 → 01a19d78's whole
	//    premise). `failed` is also the status that keeps the recovery route open
	//    (it is resumable), so the operator loses nothing.
	//
	//    RED BEFORE THE FIX: with the criterion removed this reads `completed`
	//    while stderr says `nothing to deliver` — the exact pair of sentences the
	//    epic was filed against.
	status := workerResumeDeliveryRowStatus(t, dbPath)
	t.Logf("row status after the continuation: %q", status)
	if status != tools.TaskStatusFailed {
		t.Fatalf("a resume that produced NO commit must not close `completed`: status=%q\nstderr:\n%s", status, stderr)
	}
	reason := workerResumeDeliveryRowReason(t, dbPath)
	t.Logf("row reason: %s", reason)
	if !strings.Contains(reason, tools.ResumeNoCommitToken) {
		t.Fatalf("the row must carry the ONE spelling %q so a reader of `task show` learns WHY it is not completed:\n%s",
			tools.ResumeNoCommitToken, reason)
	}
}

// workerResumeDeliveryRowReason reads the row's reason column through the same
// handle — the arm that asserts WHAT the close-out said, not only its status.
func workerResumeDeliveryRowReason(t *testing.T, dbPath string) string {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close() //stylegate:ignore the fixture's own handle, closed when this reader returns
	var reason string
	if err := db.QueryRow(`SELECT COALESCE(reason, '') FROM tasks WHERE task_id = ?`, workerResumeDeliveryTaskID).Scan(&reason); err != nil {
		t.Fatalf("read row reason: %v", err)
	}
	return reason
}

// workerResumeDeliveryRowStatus reads the row's status through the fixture's own
// handle — used by the arms that must state what the close-out wrote.
func workerResumeDeliveryRowStatus(t *testing.T, dbPath string) string {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close() //stylegate:ignore the fixture's own handle, closed when this reader returns
	var status string
	if err := db.QueryRow(`SELECT status FROM tasks WHERE task_id = ?`, workerResumeDeliveryTaskID).Scan(&status); err != nil {
		t.Fatalf("read row: %v", err)
	}
	return status
}
