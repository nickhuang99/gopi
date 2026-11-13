package main

// Deliverable.worktree resume-chdir test (bugfix epic 2026-09-03): when a
// --task row carries deliverable.worktree (backfilled at creation by the
// executor seam), `gopi session resume --task T` chdirs into that worktree — the
// fresh session works where the executor's commits live, not the main repo.
// Fail-soft paths: no task, no worktree on the row, a vanished worktree
// (recycled) — all stay in the current dir.

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/devworkspace"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// seedDeliverableBlob writes the deliverable JSON {repo,branch,worktree}
// onto a task row directly (the shape the executor creation-time backfill
// produces). cmd/agent tests open the ledger file with the same driver.
func seedDeliverableBlob(t *testing.T, taskID, repo, branch, worktree string) {
	t.Helper()
	dbPath := os.Getenv("GOPI_MEMORY_DB")
	if dbPath == "" {
		t.Fatalf("GOPI_MEMORY_DB not set")
	}
	db, err := sql.Open("sqlite", tools.LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	blob := `{"repo":"` + repo + `","branch":"` + branch + `","worktree":"` + worktree + `"}`
	now := float64(time.Now().Unix())
	if _, err := db.Exec(`UPDATE tasks SET deliverable = ?, updated_at = ? WHERE task_id = ?`, blob, now, taskID); err != nil {
		t.Fatalf("seed deliverable blob: %v", err)
	}
}

// seedResumeWTFixture builds a main checkout + linked worktree and stamps
// the worktree+branch into a task row's deliverable (the creation-time
// backfill shape the seam produces).
func seedResumeWTFixture(t *testing.T) (taskID, clientDir, wtDir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	clientDir = filepath.Join(root, "client")
	wtDir = filepath.Join(root, "wt")
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		// testutil.GitEnv STRIPS the inherited repository routing (GIT_DIR and
		// friends) before pinning the fixture identity — `cmd.Dir` alone does
		// not override GIT_DIR, so a suite started from a tree that exports it
		// would drive these commits into the WRONG repository (the standards
		// gate Rule 13/15 exists for exactly that).
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	git(root, "init", clientDir)
	git(clientDir, "config", "user.email", "t@t")
	git(clientDir, "config", "user.name", "t")
	git(clientDir, "checkout", "-b", "master")
	os.WriteFile(filepath.Join(clientDir, "base.txt"), []byte("base\n"), 0o644)
	git(clientDir, "add", "base.txt")
	git(clientDir, "commit", "-m", "base")
	git(clientDir, "worktree", "add", "-b", "feat/resume-wt", wtDir, "master")
	git(clientDir, "checkout", "master")

	taskID = "t-resume-wt-chdir"
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	if _, err := tools.TaskNew("resume worktree closure", taskID, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	// Stamp the deliverable {repo,branch,worktree} — what the executor seam
	// backfills at creation.
	seedDeliverableBlob(t, taskID, clientDir, "feat/resume-wt", wtDir)
	return taskID, clientDir, wtDir
}

// TestResumeChdirToWorktree: a row carrying deliverable.worktree chdirs the
// process into that worktree.
func TestResumeChdirToWorktree(t *testing.T) {
	taskID, _, wtDir := seedResumeWTFixture(t)
	startWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Chdir(startWd)

	// A LINKED-worktree row is the POSITIVE CONTROL for this change: the
	// worktree channel's behaviour must not move (one judge, two models — the
	// clone path changed, this one did not).
	got, gerr := resumeChdirToWorktree(taskID)
	if gerr != nil {
		t.Fatalf("a LINKED-worktree row must be entered, got %v", gerr)
	}
	if got != wtDir {
		t.Fatalf("resumeChdirToWorktree = %q, want %q", got, wtDir)
	}
	wd, _ := os.Getwd() //stylegate:ignore cwd label recorded with a session; empty degrades to the documented fallback
	if wd != wtDir {
		t.Fatalf("cwd = %q, want worktree %q", wd, wtDir)
	}
}

// TestResumeChdirToWorktreeNoTask: no --task → stays in place.
func TestResumeChdirToWorktreeNoTask(t *testing.T) {
	startWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Chdir(startWd)
	if got, err := resumeChdirToWorktree(""); got != "" || err != nil {
		t.Fatalf("no-task resume must not chdir and must not refuse, got %q / %v", got, err)
	}
	//stylegate:ignore cwd label recorded with a session; empty degrades to the documented fallback
	if wd, _ := os.Getwd(); wd != startWd {
		t.Fatalf("cwd changed without a task: %q", wd)
	}
}

// TestResumeChdirToWorktreeMissingWorktree: the row's recorded ground is GONE.
//
// This test used to assert the OPPOSITE — that the resume stayed in the caller's
// cwd — and that assertion was the bug: `gopi session resume <row>` launched from
// the main checkout degraded silently into the shared tree, where the session's
// commits have no owner, no branch and no rollback point (Rule 37). It now
// asserts the refusal, and that the refusal carries the four facts an operator
// needs to repair the row (recorded path / model verdict / detail / repair).
func TestResumeChdirToWorktreeMissingWorktree(t *testing.T) {
	startWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Chdir(startWd)

	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	t.Setenv(config.ResumeAllowMissingGroundEnv, "")
	taskID := "t-resume-wt-gone"
	if _, err := tools.TaskNew("recycled worktree resume", taskID, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	// Deliverable.worktree points at a path that does not exist.
	gone := filepath.Join(t.TempDir(), "no-such-wt")
	seedDeliverableBlob(t, taskID, "/tmp/never", "feat/gone", gone)

	got, err := resumeChdirToWorktree(taskID)
	if err == nil {
		t.Fatalf("a row whose recorded ground is GONE must be refused, got dir %q", got)
	}
	if got != "" {
		t.Fatalf("a refused resume must not report a directory, got %q", got)
	}
	//stylegate:ignore cwd label recorded with a session; empty degrades to the documented fallback
	if wd, _ := os.Getwd(); wd != startWd {
		t.Fatalf("a refused resume must not move the process: %q", wd)
	}
	refusal, ok := tools.TaskResumeGroundRefusalOf(err)
	if !ok {
		t.Fatalf("the refusal must be the TYPED one (so the exit path renders its fields): %v", err)
	}
	if refusal.Kind != tools.TaskResumeGroundRefusalGone {
		t.Fatalf("kind = %q, want %q — the row RECORDS a ground, so this is not the no-ground case",
			refusal.Kind, tools.TaskResumeGroundRefusalGone)
	}
	if !samePathForTest(refusal.Path, gone) {
		t.Errorf("the refusal must carry the RECORDED path %q, got %q", gone, refusal.Path)
	}
	if refusal.Model != string(devworkspace.GroundModelMissing) {
		t.Errorf("the refusal must carry the judge's model %q, got %q",
			devworkspace.GroundModelMissing, refusal.Model)
	}
	if !strings.Contains(refusal.Detail, "has no .git entry") {
		t.Errorf("the refusal must carry the judge's own detail, got %q", refusal.Detail)
	}
	// The RENDERED exit text is what the operator reads: row, recorded path,
	// model, and the repair command.
	text := resumeGroundRefusalText(resumeProgName, err)
	for _, want := range []string{taskID, gone, string(devworkspace.GroundModelMissing),
		"repair:", "gopi task show " + taskID} {
		if !strings.Contains(text, want) {
			t.Errorf("the refusal text must carry %q:\n%s", want, text)
		}
	}
	// THE ESCAPE HATCH, and only when it is set: the same row with the switch on
	// degrades to the caller's cwd AND says so loudly.
	t.Setenv(config.ResumeAllowMissingGroundEnv, "1")
	got, err = resumeChdirToWorktree(taskID)
	if err != nil || got != "" {
		t.Fatalf("with %s=1 the refusal must become the stated degradation, got dir %q / %v",
			config.ResumeAllowMissingGroundEnv, got, err)
	}
	if wd, _ := os.Getwd(); wd != startWd { //stylegate:ignore cwd label recorded with a session; empty degrades to the documented fallback
		t.Fatalf("the degraded resume runs in the CALLER's cwd; it moved to %q", wd)
	}
}

// TestResumeChdirToWorktreeCloneWorkspace is THE regression this fix exists
// for. The clone channel's workspace is a standalone clone (`.git` a
// DIRECTORY, at <workspace_root>/<task-id>, registered in nobody's worktree
// list). The worktree-era reading (deliverable.worktree + os.Stat + a
// registration read) knew only the LINKED model, so this directory read as "the
// ground is gone" — or, before that, as nothing at all — and the resume fell
// through into the caller's cwd. It must be ENTERED, with no fallback.
func TestResumeChdirToWorktreeCloneWorkspace(t *testing.T) {
	taskID, repoDir, cloneDir := seedResumeCloneFixture(t)
	startWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Chdir(startWd)
	t.Setenv(config.ResumeAllowMissingGroundEnv, "")

	// The landing notice must name WHICH model the ground is: the clone channel's
	// workspace was the shape this face could not describe (it printed
	// "deliverable.worktree … (branch …)" and said nothing about whether the
	// directory was a clone or a guest checkout).
	oldStderr := os.Stderr
	pr, pw, perr := os.Pipe()
	if perr != nil {
		t.Fatalf("pipe: %v", perr)
	}
	os.Stderr = pw
	got, gerr := resumeChdirToWorktree(taskID)
	os.Stderr = oldStderr
	pw.Close() //stylegate:ignore test captures a diagnostic notice through an os.Pipe; the read below is the assertion
	notice := make([]byte, 4096)
	n, _ := pr.Read(notice) //stylegate:ignore reads from the fixture pipe; the bytes are the assertion
	if gerr != nil {
		t.Fatalf("a CLONE workspace must be entered — it is a ground registered nowhere: %v", gerr)
	}
	if !strings.Contains(string(notice[:n]), "model "+string(devworkspace.GroundModelClone)) {
		t.Errorf("the landing notice must name the ground model (clone):\n%s", string(notice[:n]))
	}
	if !samePathForTest(got, cloneDir) {
		t.Fatalf("resumeChdirToWorktree = %q, want the clone workspace %q", got, cloneDir)
	}
	wd, _ := os.Getwd() //stylegate:ignore cwd label recorded with a session; empty degrades to the documented fallback
	if !samePathForTest(wd, cloneDir) {
		t.Fatalf("cwd = %q, want the clone workspace %q (staying in %q is the defect)", wd, cloneDir, startWd)
	}
	if samePathForTest(repoDir, wd) {
		t.Fatalf("the resume must NOT run in the row's repo %q", repoDir)
	}
	// The workdir override is latched to the ground (a descendant must not be
	// handed the launch cwd), the second half of "entered".
	if !samePathForTest(taskWorkdirOverride, cloneDir) {
		t.Fatalf("taskWorkdirOverride = %q, want %q", taskWorkdirOverride, cloneDir)
	}
}

// samePathForTest compares two directory spellings by their RESOLVED identity:
// git reports resolved paths, and a temp root can be reached through a symlink.
func samePathForTest(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, ea := filepath.EvalSymlinks(a)
	rb, eb := filepath.EvalSymlinks(b)
	return ea == nil && eb == nil && ra == rb
}

// seedResumeCloneFixture builds a repository plus a STANDALONE CLONE of it (the
// clone channel's workspace shape) and stamps {repo,branch,worktree} onto a row.
func seedResumeCloneFixture(t *testing.T) (taskID, repoDir, cloneDir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	repoDir = filepath.Join(root, "repo")
	cloneDir = filepath.Join(root, "01a15bc4-clone-fixture")
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	git(root, "init", repoDir)
	git(repoDir, "config", "user.email", "t@t")
	git(repoDir, "config", "user.name", "t")
	git(repoDir, "checkout", "-b", "master")
	if err := os.WriteFile(filepath.Join(repoDir, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write base: %v", err)
	}
	git(repoDir, "add", "base.txt")
	git(repoDir, "commit", "-m", "base")
	git(root, "clone", "-q", repoDir, cloneDir)
	git(cloneDir, "checkout", "-b", "feat/resume-clone")

	// The fixture must BE the clone model, or the test proves nothing.
	if info, err := os.Stat(filepath.Join(cloneDir, ".git")); err != nil || !info.IsDir() {
		t.Fatalf("the fixture's .git must be a DIRECTORY (the clone model): err=%v", err)
	}

	taskID = "t-resume-clone-chdir"
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	if _, err := tools.TaskNew("clone workspace resume", taskID, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	seedDeliverableBlob(t, taskID, repoDir, "feat/resume-clone", cloneDir)
	return taskID, repoDir, cloneDir
}

// TestResumeDegradedNoticeNamesTheCallersCwd guards the LIVE sentence the escape
// hatch prints: the detail, the CALLER's cwd, and the bare fact that the run is
// degraded.
//
// WHY THE CWD IS LOAD-BEARING, not decoration: measured 2026-10-07 (row
// 01a112e3), `gopi worker --resume --branch <name>` was launched from the MAIN
// checkout, so the un-named fallback was the shared tree, where a commit has no
// owner, no branch and no rollback point (Rule 37). Dropping the cwd from the
// sentence is the regression this catches: the message stays plausible and the
// operator loses the one fact that tells them where the resume will write.
//
// This test used to call resumeFallbackNotice — a SECOND spelling of the same
// warning that the shipped path stopped printing when the refusal landed (dead
// code kept alive by its own test, Rule 31). It now drives the function the
// operator actually meets.
func TestResumeDegradedNoticeNamesTheCallersCwd(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	gone := "/gone/worktree"
	refusal := tools.TaskResumeGroundGoneRefusal("t-degraded-notice", gone, "feat/gone", "/repo",
		string(devworkspace.GroundModelMissing), "no ground: /gone/worktree has no .git entry")
	got := resumeDegradedNotice(refusal)
	if !strings.Contains(got, gone) {
		t.Errorf("the notice must name the ground it could not enter: %q", got)
	}
	if !strings.Contains(got, wd) {
		t.Errorf("the notice must name the CALLER's cwd %q it degrades into: %q", wd, got)
	}
	if !strings.Contains(got, "CALLER") {
		t.Errorf("the notice must say the directory is the caller's, not one the resume chose: %q", got)
	}
	if !strings.Contains(got, "DEGRADED") || !strings.Contains(got, config.ResumeAllowMissingGroundEnv) {
		t.Errorf("the notice must be unmistakable and name the switch that bought it: %q", got)
	}
}

// TestResumeChdirToWorktreeNoBlob: a row with no deliverable.worktree (the
// pre-fix repo-only submit shape) stays in place.
func TestResumeChdirToWorktreeNoBlob(t *testing.T) {
	startWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Chdir(startWd)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	taskID := "t-resume-wt-noblob"
	if _, err := tools.TaskNew("repo-only row", taskID, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	if got, err := resumeChdirToWorktree(taskID); got != "" || err != nil {
		t.Fatalf("a row recording NO ground must stay put without refusing (nothing is gone), got %q / %v", got, err)
	}
	//stylegate:ignore cwd label recorded with a session; empty degrades to the documented fallback
	if wd, _ := os.Getwd(); wd != startWd {
		t.Fatalf("cwd changed for a repo-only row: %q", wd)
	}
}

// TestResumeChdirNoticeMentionsBranch: the stderr notice names the branch
// (observability — the user sees WHERE the resume landed).
func TestResumeChdirNoticeMentionsBranch(t *testing.T) {
	taskID, _, wtDir := seedResumeWTFixture(t)
	startWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Chdir(startWd)

	// Capture stderr.
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	got, chdirErr := resumeChdirToWorktree(taskID)
	os.Stderr = old
	if chdirErr != nil {
		t.Fatalf("a linked-worktree row must be entered: %v", chdirErr)
	}
	w.Close() //stylegate:ignore test captures a diagnostic notice through an os.Pipe; the read below is the assertion, and a pipe-writer close error cannot lose the notice already buffered
	buf := make([]byte, 4096)
	n, _ := r.Read(buf) //stylegate:ignore reads from the fixture pipe; the bytes are the assertion
	notice := string(buf[:n])
	if got != wtDir {
		t.Fatalf("resumeChdirToWorktree = %q, want %q", got, wtDir)
	}
	if !strings.Contains(notice, "feat/resume-wt") || !strings.Contains(notice, wtDir) {
		t.Fatalf("notice must name the branch and worktree: %s", notice)
	}
}

// legacyResumeChdirToWorktree is the PRE-FIX body of resumeChdirToWorktree,
// preserved verbatim (minus its comment block) as the RED PROOF of this change.
//
// It is not dead code: the test below RUNS it on the same fixtures the fixed
// function is run on. `git show HEAD:cmd/agent/cli_resume.go` cannot serve that
// purpose — the moment this change is committed, HEAD carries the FIX — so the
// pre-fix semantics have to live somewhere that survives the commit, and the
// only honest place is beside the assertions that measure it.
//
// WHAT IT PROVES: on a CLONE-channel workspace it returns "" (the directory is
// real, and the worktree-era reading cannot see it as ground), and on a row
// whose recorded ground is GONE it ALSO returns "" with a one-line warning —
// i.e. both shapes degrade into the caller's cwd, which is the defect.
func legacyResumeChdirToWorktree(taskID string) string {
	if taskID == "" {
		return ""
	}
	wtx, _ := tools.TaskDeliverableWorktreeFor(taskID)
	if wtx == "" {
		return ""
	}
	info, err := os.Stat(wtx)
	if err != nil || !info.IsDir() {
		return ""
	}
	if err := os.Chdir(wtx); err != nil {
		return ""
	}
	taskWorkdirOverride = wtx
	return wtx
}

// TestResumeChdirToWorktreePreFixSemanticsAreTheDefect pins the RED side of this
// change: what the PRE-FIX body does on the very rows the fixed one refuses.
//
// The pre-fix body's defect is NOT "it cannot enter a clone workspace" — an
// existing directory is chdir'd into whatever it is. It has TWO holes, and both
// are measured here:
//
//	① a recorded ground that is GONE   → returns "" with one warning line and
//	   the resume runs in the caller's cwd (the silent degradation: launched
//	   from the main checkout, that is a session whose commits land in the
//	   shared tree with no owner, no branch and no rollback point — Rule 37)
//	② a recorded path that EXISTS but is not a ground at all → chdir'd into
//	   blindly: the worktree-era reading asks `os.Stat`, never "is this a
//	   ground, and which model?" — the question the clone channel made
//	   unavoidable and devworkspace.ResolveGroundState answers.
func TestResumeChdirToWorktreePreFixSemanticsAreTheDefect(t *testing.T) {
	startWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	// ① GONE ground: pre-fix returns "", the fixed body refuses.
	t.Chdir(startWd)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	t.Setenv(config.ResumeAllowMissingGroundEnv, "")
	goneRow := "t-resume-prefix-gone"
	if _, err := tools.TaskNew("pre-fix gone ground", goneRow, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	gone := filepath.Join(t.TempDir(), "no-such-wt")
	seedDeliverableBlob(t, goneRow, "/tmp/never", "feat/gone", gone)

	if got := legacyResumeChdirToWorktree(goneRow); got != "" {
		t.Fatalf("PRE-FIX body entered %q for a GONE ground — the red proof is void", got)
	}
	if wd, _ := os.Getwd(); !samePathForTest(wd, startWd) { //stylegate:ignore cwd label recorded with a session
		t.Fatalf("PRE-FIX body moved the process to %q for a gone ground", wd)
	}
	if got, ferr := resumeChdirToWorktree(goneRow); ferr == nil || got != "" {
		t.Fatalf("the FIXED body must REFUSE a gone ground, got dir %q / %v", got, ferr)
	}

	// ② a recorded path that EXISTS and is not a work tree: pre-fix enters it,
	// the fixed body refuses (model: missing).
	t.Chdir(startWd)
	notAGround := t.TempDir()
	wrongRow := "t-resume-prefix-ungrounded"
	if _, err := tools.TaskNew("pre-fix ungrounded path", wrongRow, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	seedDeliverableBlob(t, wrongRow, "/tmp/never", "feat/wrong", notAGround)
	if got := legacyResumeChdirToWorktree(wrongRow); !samePathForTest(got, notAGround) {
		t.Fatalf("PRE-FIX body did not enter the existing-but-ungrounded path %q (got %q) — "+
			"the red proof is void", notAGround, got)
	}
	t.Chdir(startWd)
	if got, ferr := resumeChdirToWorktree(wrongRow); ferr == nil || got != "" {
		t.Fatalf("the FIXED body must REFUSE a path that is not a ground, got dir %q / %v", got, ferr)
	}
}
