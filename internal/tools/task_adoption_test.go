package tools

// Adoption chain tests: real git repos in t.TempDir (Rule 4 最小样本 — the
// chain IS git, so the tests drive git), ledger isolated via GOPI_MEMORY_DB.
// Covers the epic's three acceptance runs: clean adopt, conflict dispatch,
// re-tick idempotency.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// adoptionTestRepo builds a bare-master + worktree repo with one commit and
// returns its path (the "server" repo the sweep runs against).
func adoptionTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	git("init", "-b", "master")
	git("config", "user.email", "test@gopi")
	git("config", "user.name", "test")
	git("commit", "--allow-empty", "-m", "root")
	return dir
}

// adoptionSeedTask submits a completed task whose deliverable names the
// branch — the ① gate's positive case.
func adoptionSeedTask(t *testing.T, branch string) {
	t.Helper()
	out, err := TaskSubmit(TaskSubmitOptions{
		Summary: "work on " + branch,
		Payload: "already executed elsewhere; branch pushed back",
		Tags:    "adoption-test",
		Source:  "auto",
	})
	if err != nil {
		t.Fatalf("seed submit: %v", err)
	}
	id := adoptionTaskID(t, out)
	if _, err := TaskUpdate(id, "in_progress"); err != nil {
		t.Fatalf("seed progress: %v", err)
	}
	if _, err := TaskUpdate(id, "completed"); err != nil {
		t.Fatalf("seed complete: %v", err)
	}
	// Backfill deliverable.branch like the executor does (M4.9).
	if err := adoptionTestStampBranch(t, id, branch); err != nil {
		t.Fatalf("stamp deliverable: %v", err)
	}
}

func adoptionTestStampBranch(t *testing.T, id, branch string) error {
	t.Helper()
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`UPDATE tasks SET deliverable=? WHERE task_id=?`,
		`{"repo":"/tmp/irrelevant","branch":"`+branch+`"}`, id)
	return err
}

func adoptionTaskID(t *testing.T, submitOut string) string {
	t.Helper()
	for _, f := range strings.Fields(submitOut) {
		if strings.HasPrefix(f, "t-") || len(f) == 36 {
			return strings.Trim(f, "()")
		}
	}
	t.Fatalf("no task id in submit output: %q", submitOut)
	return ""
}

// adoptionPushBranch commits file on a branch in a scratch clone and pushes
// it into the server repo's refs/remotes via a local remote (mimics the
// client's push-back channel: bare-ish remote → remote-tracking refs).
func adoptionPushBranch(t *testing.T, repo, branch, file, content string, conflict bool) {
	t.Helper()
	scratch := t.TempDir()
	git := func(dir string, args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	git(scratch, "clone", "-q", repo, ".")
	git(scratch, "config", "user.email", "client@gopi")
	git(scratch, "config", "user.name", "client")
	git(scratch, "checkout", "-q", "-b", branch)
	if conflict {
		// Conflict recipe: the branch rewrites line 1 of README, which
		// master also rewrote after the clone.
		os.WriteFile(filepath.Join(scratch, file), []byte(content), 0o644)
		git(scratch, "add", file)
		git(scratch, "commit", "-qm", "client edit "+file)
		os.WriteFile(filepath.Join(repo, file), []byte("server edit\n"), 0o644)
		git(repo, "add", file)
		git(repo, "commit", "-qm", "server edit "+file)
	} else {
		os.WriteFile(filepath.Join(scratch, file), []byte(content), 0o644)
		git(scratch, "add", file)
		git(scratch, "commit", "-qm", "add "+file)
	}
	git(scratch, "push", "-q", "origin", branch)
}

// adoptionCountsOf reads the task rows the sweep created (dispatch face).
func adoptionCountConflictTasks(t *testing.T, branch string) int {
	t.Helper()
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE `+taskTagTokenSQLTest(taskTagMergeConflict)+` AND payload LIKE ?`,
		"%"+fmt.Sprintf(taskMergePayloadBranchPrefix, branch)+"%").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestAdoptionCleanBranchAdopts(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo := adoptionTestRepo(t)
	adoptionSeedTask(t, "feat/clean")
	adoptionPushBranch(t, repo, "feat/clean", "clean.txt", "clean work\n", false)

	counts, err := AdoptionSweep(context.Background(), AdoptionOptions{Repo: repo, Submit: true})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(counts.Adopted) != 1 || counts.Adopted[0] != "feat/clean" {
		t.Fatalf("want feat/clean adopted, got %+v (failed=%v)", counts, counts.Failed)
	}
	// The merge commit exists on master and the remote ref is gone.
	out, err := adoptionGit(repo, "log", "--oneline", "-2")
	if err != nil || !strings.Contains(out, "adoption: merge feat/clean") {
		t.Fatalf("adoption commit missing: %q err=%v", out, err)
	}
	//stylegate:ignore git plumbing in a fixture; the refs read afterwards are the assertion
	if refs, _ := adoptionGit(repo, "for-each-ref", "refs/remotes"); strings.Contains(refs, "feat/clean") {
		t.Fatalf("remote ref not pruned: %q", refs)
	}
	// Second pass is a no-op (branch gone → nothing scanned).
	counts2, err := AdoptionSweep(context.Background(), AdoptionOptions{Repo: repo, Submit: true})
	if err != nil || counts2.Scanned != 0 {
		t.Fatalf("second pass: %+v err=%v", counts2, err)
	}
}

// adoptionConflictPayload fetches the payload of the dispatched conflict
// row for branch (the briefing content check).
func adoptionConflictPayload(t *testing.T, branch string) string {
	t.Helper()
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var payload string
	if err := db.QueryRow(`SELECT payload FROM tasks WHERE `+taskTagTokenSQLTest(taskTagMergeConflict)+` AND payload LIKE ?`,
		"%"+fmt.Sprintf(taskMergePayloadBranchPrefix, branch)+"%").Scan(&payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	return payload
}

func TestAdoptionConflictDispatchesSelfContainedTask(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo := adoptionTestRepo(t)
	// Seed README on master first so both sides can edit it.
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitout, err := adoptionGit(repo, "add", "README")
	_ = gitout
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adoptionGit(repo, "commit", "-m", "readme base"); err != nil {
		t.Fatal(err)
	}
	adoptionSeedTask(t, "feat/conflict")
	adoptionPushBranch(t, repo, "feat/conflict", "README", "client edit\n", true)

	counts, err := AdoptionSweep(context.Background(), AdoptionOptions{Repo: repo, Submit: true})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(counts.Dispatch) != 1 || counts.Dispatch[0] != "feat/conflict" {
		t.Fatalf("want feat/conflict dispatched, got %+v failed=%v", counts, counts.Failed)
	}
	// Scene saved: worktree clean again (abort ran).
	status, err := adoptionGit(repo, "status", "--porcelain")
	if err != nil || strings.TrimSpace(status) != "" {
		t.Fatalf("worktree not restored after abort: %q err=%v", status, err)
	}
	// The dispatched briefing is self-contained: branch, intent, diff
	// summary, acceptance (TaskSearch only shows the summary line — the
	// payload check reads the row directly).
	payload := adoptionConflictPayload(t, "feat/conflict")
	for _, want := range []string{"branch feat/conflict (tip", "work on feat/conflict", "README", "Acceptance:", "Conflict"} {
		if !strings.Contains(payload, want) {
			t.Fatalf("briefing missing %q in: %s", want, payload)
		}
	}
}

func TestAdoptionRepeatedTickDoesNotRedispatch(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo := adoptionTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := adoptionGit(repo, "add", "README"); err != nil {
		t.Fatal(err)
	}
	if _, err := adoptionGit(repo, "commit", "-m", "readme base"); err != nil {
		t.Fatal(err)
	}
	adoptionSeedTask(t, "feat/dup")
	adoptionPushBranch(t, repo, "feat/dup", "README", "client edit\n", true)

	for i := 0; i < 3; i++ {
		if _, err := AdoptionSweep(context.Background(), AdoptionOptions{Repo: repo, Submit: true}); err != nil {
			t.Fatalf("sweep %d: %v", i, err)
		}
	}
	if n := adoptionCountConflictTasks(t, "feat/dup"); n != 1 {
		t.Fatalf("epic ⑤ violated: %d conflict rows for one branch (want 1)", n)
	}
}

func TestAdoptionSkipsBranchWithoutCompletedTask(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo := adoptionTestRepo(t)
	// No ledger row at all → ① gate fails → skipped, untouched.
	adoptionPushBranch(t, repo, "feat/orphan", "orphan.txt", "x\n", false)
	counts, err := AdoptionSweep(context.Background(), AdoptionOptions{Repo: repo, Submit: true})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if counts.Skipped != 1 || len(counts.Adopted) != 0 || len(counts.Dispatch) != 0 {
		t.Fatalf("orphan branch must skip: %+v", counts)
	}
}

func TestAdoptionHunkExtraction(t *testing.T) {
	repo := adoptionTestRepo(t)
	// Simulate a mid-merge index state via a conflicted file on disk.
	conflicted := "<<<<<<< HEAD\nserver\n=======\nclient\n>>>>>>> feat/x\n"
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte(conflicted), 0o644); err != nil {
		t.Fatal(err)
	}
	// No index conflict → no conflicted files → briefing says so.
	files, err := adoptionConflictFiles(repo)
	if err != nil {
		t.Fatalf("conflict files: %v", err)
	}
	_ = files
	var b strings.Builder
	if err := adoptionMergeConflictBriefing(repo, adoptionBranch{Name: "feat/x", Tip: "abc1234"}, &b); err != nil {
		t.Fatalf("briefing: %v", err)
	}
	if !strings.Contains(b.String(), "adoptionBranch") && b.String() == "" {
		t.Fatal("empty briefing")
	}
}

func TestDeliverableBranchDecode(t *testing.T) {
	branch, err := taskDeliverableBranch(`{"repo":"/x","branch":"feat/a"}`)
	if err != nil || branch != "feat/a" {
		t.Fatalf("got %q err=%v", branch, err)
	}
	branch, err = taskDeliverableBranch(`{"repo":"/x"}`)
	if err != nil || branch != "" {
		t.Fatalf("branch-less row must decode to empty: %q err=%v", branch, err)
	}
	if _, err := taskDeliverableBranch("not-json"); err == nil {
		t.Fatal("garbage deliverable must error")
	}
}
