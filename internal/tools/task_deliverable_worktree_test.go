package tools

// Deliverable.worktree creation-time backfill tests (bugfix epic
// 2026-09-03, nick's finding — the M4.8 information-closure loop's
// missing worktree half). Each acceptance case maps to one test:
//
//	(a) taskWorktreeOwningDir: cwd inside a linked worktree of the repo →
//	    (worktree, branch); main checkout / unrelated dir / exempt branch
//	    → ("","")
//	(b) taskBackfillDeliverableWorktree: stamps {branch,worktree} onto a
//	    repo-only row (CAS merge, any status, idempotent re-stamp)
//	(c) TaskDeliverableWorktreeBash: the executor seam — after a bash round
//	    working in the worktree, the wrapper row carries worktree+branch
//	(d) MergedCleanupSweep live-worktree protection: a LIVE row carrying
//	    deliverable.worktree protects that worktree even when its branch
//	    attribution is missing/mismatched (the fresh-worktree-at-master-tip
//	    window the branch-only guard ⑤ could not see)
//
// Fixtures are real-git (worktree/porcelain semantics are git-truth).

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newDeliverableWTFixture: a main checkout (master, one commit) with a
// linked worktree on a fresh feature branch (branch still AT master tip —
// the exact shape a freshly-created executor worktree has before its first
// commit).
type deliverableWTFixture struct {
	t          *testing.T
	root       string
	clientDir  string // main checkout (master)
	wtDir      string // linked worktree on the fresh feature branch
	featBranch string
}

func deliverableWTGit(t *testing.T, dir string, args ...string) string {
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

func newDeliverableWTFixture(t *testing.T) *deliverableWTFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	f := &deliverableWTFixture{
		t:          t,
		root:       root,
		clientDir:  filepath.Join(root, "client"),
		wtDir:      filepath.Join(root, "workspace", "gopi-task-wt"),
		featBranch: "feat/deliverable-wt-work",
	}
	git := func(dir string, args ...string) string {
		return deliverableWTGit(t, dir, args...)
	}
	git(root, "init", f.clientDir)
	git(f.clientDir, "config", "user.email", "t@t")
	git(f.clientDir, "config", "user.name", "t")
	git(f.clientDir, "checkout", "-b", "master")
	os.WriteFile(filepath.Join(f.clientDir, "base.txt"), []byte("base\n"), 0o644)
	git(f.clientDir, "add", "base.txt")
	git(f.clientDir, "commit", "-m", "base")
	git(f.clientDir, "worktree", "add", "-b", f.featBranch, f.wtDir, "master")
	git(f.clientDir, "checkout", "master")
	return f
}

// deliverableWTSeedWrapperRow inserts a repo-only deliverable wrapper row
// (the submit shape: {"repo":...} only — no branch/worktree yet).
func deliverableWTSeedWrapperRow(t *testing.T, db string, taskID, repo string) {
	t.Helper()
	ctx := context.Background()
	conn, err := openTasksDB(ctx, db)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer conn.Close()
	deliverable := `{"repo":"` + repo + `"}`
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, deliverable, source, created_at, updated_at)
		 VALUES (?, ?, 'in_progress', ?, 'auto', ?, ?)`,
		taskID, "wrapper work in progress", deliverable, nowStamp(), nowStamp()); err != nil {
		t.Fatalf("seed wrapper row: %v", err)
	}
}

// deliverableWTRowBlob returns the task row's raw deliverable blob.
func deliverableWTRowBlob(t *testing.T, db, taskID string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := openTasksDB(ctx, db)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer conn.Close()
	task, err := taskByID(ctx, conn, taskID)
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	if task == nil {
		t.Fatalf("task %s not found", taskID)
	}
	return task.Deliverable
}

// TestTaskWorktreeOwningDirResolvesLinkedWorktree is epic case (a): a cwd
// inside the linked worktree resolves to (worktree, branch); the main
// checkout cwd and an unrelated dir resolve to nothing.
func TestTaskWorktreeOwningDirResolvesLinkedWorktree(t *testing.T) {
	f := newDeliverableWTFixture(t)

	wt, branch := taskWorktreeOwningDir(f.clientDir, f.wtDir)
	if wt != f.wtDir {
		t.Fatalf("worktree dir mismatch: want %s got %s", f.wtDir, wt)
	}
	if branch != f.featBranch {
		t.Fatalf("branch mismatch: want %s got %s", f.featBranch, branch)
	}

	// Nested cwd inside the worktree also resolves.
	sub := filepath.Join(f.wtDir, "sub", "dir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	wt, branch = taskWorktreeOwningDir(f.clientDir, sub)
	if wt != f.wtDir || branch != f.featBranch {
		t.Fatalf("nested cwd must resolve: wt=%s branch=%s", wt, branch)
	}

	// Main checkout cwd → nothing (never attribute the main checkout).
	if wt, branch := taskWorktreeOwningDir(f.clientDir, f.clientDir); wt != "" || branch != "" {
		t.Fatalf("main checkout must not resolve: wt=%s branch=%s", wt, branch)
	}

	// Unrelated dir → nothing.
	other := filepath.Join(f.root, "elsewhere")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if wt, branch := taskWorktreeOwningDir(f.clientDir, other); wt != "" || branch != "" {
		t.Fatalf("unrelated dir must not resolve: wt=%s branch=%s", wt, branch)
	}
}

// TestTaskWorktreeOwningDirSkipsExemptBranches: a worktree parked on an
// exempt branch (master/main/archive) is never attributed — nothing to
// backfill.
func TestTaskWorktreeOwningDirSkipsExemptBranches(t *testing.T) {
	f := newDeliverableWTFixture(t)
	// An exempt-branch linked worktree (archive/* — the finish-cleanup
	// exemption set) must never be attributed: nothing to backfill.
	exemptBranch := "archive/test-20260903"
	exemptWT := filepath.Join(f.root, "wt-archive")
	deliverableWTGit(t, f.clientDir, "worktree", "add", "-b", exemptBranch, exemptWT, "master")
	defer deliverableWTGit(t, f.clientDir, "worktree", "remove", exemptWT, "--force")

	if wt, branch := taskWorktreeOwningDir(f.clientDir, exemptWT); wt != "" || branch != "" {
		t.Fatalf("exempt-branch worktree must not resolve: wt=%s branch=%s", wt, branch)
	}
	// The feature worktree still resolves (sanity — the fixture's own wt).
	if wt, branch := taskWorktreeOwningDir(f.clientDir, f.wtDir); wt != f.wtDir || branch != f.featBranch {
		t.Fatalf("feature worktree must still resolve: wt=%s branch=%s", wt, branch)
	}
}

// TestTaskBackfillDeliverableWorktreeStampsCreationFields is epic case (b):
// the CAS merge stamps {branch,worktree} onto a repo-only row — exactly
// what the creation-time backfill must do — while leaving a later-arrived
// field intact, and is idempotent on re-stamp.
func TestTaskBackfillDeliverableWorktreeStampsCreationFields(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	taskID := "t-wt-backfill-1"
	deliverableWTSeedWrapperRow(t, db, taskID, f.clientDir)

	if err := taskBackfillDeliverableWorktree(taskID, f.wtDir, f.featBranch); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	var d struct {
		Repo     string `json:"repo"`
		Branch   string `json:"branch"`
		Worktree string `json:"worktree"`
	}
	if err := json.Unmarshal([]byte(deliverableWTRowBlob(t, db, taskID)), &d); err != nil {
		t.Fatalf("decode blob: %v", err)
	}
	if d.Repo != f.clientDir || d.Branch != f.featBranch || d.Worktree != f.wtDir {
		t.Fatalf("creation stamp missing: repo=%q branch=%q worktree=%q", d.Repo, d.Branch, d.Worktree)
	}

	// Idempotent: re-stamping the same values must not error or change the
	// blob.
	before := deliverableWTRowBlob(t, db, taskID)
	if err := taskBackfillDeliverableWorktree(taskID, f.wtDir, f.featBranch); err != nil {
		t.Fatalf("re-backfill: %v", err)
	}
	if after := deliverableWTRowBlob(t, db, taskID); after != before {
		t.Fatalf("idempotent re-stamp changed the blob:\n%s\n%s", before, after)
	}
}

// TestTaskBackfillDeliverableWorktreeKeepsRepoOnlyUntouched: a row whose
// deliverable is repo-only must stay repo-only when no worktree is given
// (the no-op path), and a repo-less/empty deliverable must never be
// clobbered into a worktree-only blob.
func TestTaskBackfillDeliverableWorktreeKeepsRepoOnlyUntouched(t *testing.T) {
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	taskID := "t-wt-backfill-2"
	deliverableWTSeedWrapperRow(t, db, taskID, "/tmp/never-exists")

	if err := taskBackfillDeliverableWorktree(taskID, "", ""); err != nil {
		t.Fatalf("no-op backfill: %v", err)
	}
	blob := deliverableWTRowBlob(t, db, taskID)
	if !strings.Contains(blob, `"repo":"/tmp/never-exists"`) || strings.Contains(blob, "worktree") {
		t.Fatalf("empty stamp must not touch the blob: %s", blob)
	}
}

// TestTaskDeliverableWorktreeBashBackfillsWrapperRow is epic case (c): the
// executor seam — after a bash round whose workdir is the linked worktree,
// the wrapper row (repo-only at submit) carries worktree+branch.
func TestTaskDeliverableWorktreeBashBackfillsWrapperRow(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	taskID := "t-wt-bash-seam"
	deliverableWTSeedWrapperRow(t, db, taskID, f.clientDir)
	t.Setenv(TaskWrapperEnv, taskID)
	defer t.Setenv(TaskWrapperEnv, "")

	// Simulate the executor's bash round: workdir = the linked worktree
	// (the seam's TaskDeliverableWorktreeBash(workdir, command)).
	TaskDeliverableWorktreeBash(f.wtDir, "git status")

	var d struct {
		Branch   string `json:"branch"`
		Worktree string `json:"worktree"`
	}
	if err := json.Unmarshal([]byte(deliverableWTRowBlob(t, db, taskID)), &d); err != nil {
		t.Fatalf("decode blob: %v", err)
	}
	if d.Branch != f.featBranch || d.Worktree != f.wtDir {
		t.Fatalf("executor seam must backfill worktree+branch: branch=%q worktree=%q", d.Branch, d.Worktree)
	}
}

// TestTaskBashWorkdirCandidatesExpandsTilde: the Rule-6 creation command is
// written the way a human types it — `git worktree add ~/workspace/gopi-x
// -b feat/x` — so the path token is a literal "~" the SHELL would have
// expanded. Statting it unexpanded resolves against the process cwd, fails,
// and the worktree is never stamped (row 01a08913-d4eb: the child created
// and committed in ~/workspace/gopi-hippocampus-reframe while its row kept
// a repo-only deliverable). The candidate list must carry the EXPANDED dir.
func TestTaskBashWorkdirCandidatesExpandsTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wt := filepath.Join(home, "workspace", "gopi-tilde-wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cands := taskBashWorkdirCandidates("", "git worktree add ~/workspace/gopi-tilde-wt -b feat/tilde master")
	for _, c := range cands {
		if c == wt {
			return
		}
	}
	t.Fatalf("the ~-spelled worktree path must resolve to %s, got %v", wt, cands)
}

// TestTaskDeliverableWorktreeBashTildeWorktreeAddMoment is the end-to-end
// form of the same bug: the executor's literal creation round (tilde path,
// run from the main repo cwd) must stamp deliverable.worktree. HOME is
// pointed at the fixture root so `~/workspace/<name>` IS the fixture's
// linked worktree — the exact live shape.
func TestTaskDeliverableWorktreeBashTildeWorktreeAddMoment(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	// The fixture's worktree lives at <root>/workspace/gopi-task-wt; making
	// <root> the home turns it into ~/workspace/gopi-task-wt.
	t.Setenv("HOME", f.root)
	taskID := "t-wt-tilde-moment"
	deliverableWTSeedWrapperRow(t, db, taskID, f.clientDir)
	t.Setenv(TaskWrapperEnv, taskID)
	defer t.Setenv(TaskWrapperEnv, "")

	// The literal creation command, ~-spelled, with no workdir param.
	TaskDeliverableWorktreeBash("", "cd ~/work/repo && git worktree add ~/workspace/gopi-task-wt -b "+f.featBranch+" master")

	var d struct {
		Branch   string `json:"branch"`
		Worktree string `json:"worktree"`
	}
	if err := json.Unmarshal([]byte(deliverableWTRowBlob(t, db, taskID)), &d); err != nil {
		t.Fatalf("decode blob: %v", err)
	}
	if d.Worktree != f.wtDir || d.Branch != f.featBranch {
		t.Fatalf("the ~-spelled creation moment must backfill worktree+branch: worktree=%q branch=%q", d.Worktree, d.Branch)
	}
	// Ownership closure: the stamped worktree is the SAME directory git
	// resolves for the branch (creation-time record == cleanup-time target).
	if got := finishCleanupBranchWorktree(context.Background(), f.clientDir, f.featBranch); got != d.Worktree {
		t.Errorf("recorded worktree %q must equal git's linked worktree %q", d.Worktree, got)
	}
}

// TestTaskDeliverableWorktreeBashGitCWorktreeCandidate: the executor
// commonly runs git from the MAIN repo cwd with an explicit
// `git -C <worktree> ...` — the seam must catch the -C target even when
// the workdir is the main checkout.
func TestTaskDeliverableWorktreeBashGitCWorktreeCandidate(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	taskID := "t-wt-gitc-seam"
	deliverableWTSeedWrapperRow(t, db, taskID, f.clientDir)
	t.Setenv(TaskWrapperEnv, taskID)
	defer t.Setenv(TaskWrapperEnv, "")

	// workdir = the MAIN checkout; the command MODIFIES the worktree via -C.
	// The subcommand is a mutation on purpose (2026-09-15): a read-only probe
	// must NOT bind a row (see
	// TestTaskDeliverableWorktreeBashReadOnlyProbeDoesNotBind) — a `-C <wt>
	// status` is the routine inspection of someone else's tree, and treating it
	// as "this run works there" is the hijack that protected orphaned worktrees
	// from the sweeper.
	TaskDeliverableWorktreeBash(f.clientDir, "git -C "+f.wtDir+" commit --allow-empty -m probe")

	var d struct {
		Branch   string `json:"branch"`
		Worktree string `json:"worktree"`
	}
	if err := json.Unmarshal([]byte(deliverableWTRowBlob(t, db, taskID)), &d); err != nil {
		t.Fatalf("decode blob: %v", err)
	}
	if d.Branch != f.featBranch || d.Worktree != f.wtDir {
		t.Fatalf("-C candidate must backfill: branch=%q worktree=%q", d.Branch, d.Worktree)
	}
}

// TestTaskDeliverableWorktreeBashReadOnlyProbeDoesNotBind is the regression for
// the MEASURED live leak (2026-09-15, this epic's own row): a round that only
// LOOKS at another task's worktree must not bind this row to it.
//
// The instance, taken from the ledger rather than imagined: row
// 01a0a36a-86be-763c-b38c-f2444661439f was minted by its `task run` wrapper with
// a repo-only deliverable. One inspection round in that session — the loop
// inspecting THIS epic's attachment, `for wt in <orphan-A> <orphan-B>; do git -C
// $wt status; done` — re-stamped the row's deliverable.worktree with the orphan.
// From then on the worktree sweeper's ownership arm read `deliverable.worktree`
// and printed "owned by live task 01a0a36a (in_progress)": the orphan that the
// task existed to clean up had become protected BY that task, and it stayed
// registered (the attachment's complaint reproducing itself inside the fix run).
//
// The property pinned here: a read-only subcommand at a -C target is not
// evidence of where the run works; a mutation at the same target is.
func TestTaskDeliverableWorktreeBashReadOnlyProbeDoesNotBind(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	taskID := "t-wt-probe-no-bind"
	deliverableWTSeedWrapperRow(t, db, taskID, f.clientDir)
	t.Setenv(TaskWrapperEnv, taskID)
	defer t.Setenv(TaskWrapperEnv, "")

	// The inspection shapes actually run against another task's worktree: the
	// per-command probes AND the `cd`-into-it loop form. None may bind.
	for _, command := range []string{
		"git -C " + f.wtDir + " status",
		"git -C " + f.wtDir + " log --oneline -1",
		"git -C " + f.wtDir + " worktree list",
		"git -C " + f.wtDir + " diff --stat",
		"cd " + f.wtDir + " && git status --porcelain",
		"for wt in " + f.wtDir + "; do git -C $wt status; git -C $wt log -1; done",
	} {
		TaskDeliverableWorktreeBash(f.clientDir, command)
	}

	blob := deliverableWTRowBlob(t, db, taskID)
	var d struct {
		Branch   string `json:"branch"`
		Worktree string `json:"worktree"`
	}
	if err := json.Unmarshal([]byte(blob), &d); err != nil {
		t.Fatalf("decode blob: %v", err)
	}
	if d.Worktree != "" {
		t.Errorf("a read-only probe BOUND the row to %q — the row then becomes that worktree's protected owner and the sweeper can never collect it (blob %s)", d.Worktree, blob)
	}

	// The other half of the same property: the creation moment and a mutation
	// still bind (the seam must not have been silenced altogether).
	TaskDeliverableWorktreeBash(f.clientDir, "git -C "+f.wtDir+" commit --allow-empty -m work")
	if err := json.Unmarshal([]byte(deliverableWTRowBlob(t, db, taskID)), &d); err != nil {
		t.Fatalf("decode blob: %v", err)
	}
	if d.Worktree != f.wtDir || d.Branch != f.featBranch {
		t.Errorf("a mutation in the worktree must still bind the row: worktree=%q branch=%q want %q/%q", d.Worktree, d.Branch, f.wtDir, f.featBranch)
	}
}

// TestTaskDeliverableWorktreeBashPlainSessionNoop: no wrapper env → the
// seam is a no-op (a plain chat session never touches the ledger).
func TestTaskDeliverableWorktreeBashPlainSessionNoop(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	// No TaskWrapperEnv set (plain session).
	TaskDeliverableWorktreeBash(f.wtDir, "git status")
	// Nothing to assert against the ledger — the call must simply not
	// panic and not write. Probe the wrapper-less guard directly.
	if wt, _ := TaskDeliverableWorktreeFor("no-such-task"); wt != "" {
		t.Fatalf("no wrapper row must resolve nothing")
	}
	_ = f
}

// TestMergedCleanupLiveWorktreeProtection is epic case (d): a LIVE row
// whose deliverable carries the WORKTREE but a MISMATCHED/absent branch —
// the fresh-worktree window — must protect that worktree from the sweep.
// Before the fix the branch-only guard ⑤ missed it and the frozen pair was
// removed; now the live worktree set protects it.
func TestMergedCleanupLiveWorktreeProtection(t *testing.T) {
	// Reuse the merged fixture: the worktree branch is merged into master
	// and still checked out in a linked worktree (the frozen shape the
	// sweep exists to clear) — but a LIVE row names the worktree.
	f := newMergedCleanupFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	ctx := context.Background()
	conn, err := openTasksDB(ctx, db)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	// Live row with deliverable.worktree = the worktree and NO branch
	// (branch attribution missing — the exact creation-window shape).
	deliverable := `{"repo":"` + f.clientDir + `","worktree":"` + f.wtDir + `"}`
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, deliverable, source, created_at, updated_at)
		 VALUES (?, ?, 'in_progress', ?, 'auto', ?, ?)`,
		"t-live-wt-only", "active work in progress", deliverable, nowStamp(), nowStamp()); err != nil {
		t.Fatalf("seed live worktree row: %v", err)
	}
	conn.Close() //stylegate:ignore transport teardown on a connection already reported as failed

	rep := mergedCleanupRun(t, f)
	if len(rep.Removed) != 0 {
		t.Fatalf("live deliverable.worktree must protect the pair: removed=%+v", rep.Removed)
	}
	if len(rep.Skipped) != 1 || !strings.Contains(rep.Skipped[0].Detail, "live task") {
		t.Fatalf("want one live-task skip, got skipped=%+v", rep.Skipped)
	}
	if _, err := os.Stat(f.wtDir); err != nil {
		t.Errorf("worktree must survive a live-worktree skip: %v", err)
	}
	if err := exec.Command("git", "-C", f.clientDir, "rev-parse", "--verify", "--quiet", f.featBranch).Run(); err != nil {
		t.Errorf("branch must survive a live-worktree skip")
	}
}

// TestTaskDeliverableWorktreeBashWorktreeAddMoment: the CREATION moment
// itself — `git worktree add <path> -b feat/x` run from the MAIN repo cwd
// (no -C, no workdir) — the seam must catch the new worktree path as a bare
// argument and backfill it immediately.
func TestTaskDeliverableWorktreeBashWorktreeAddMoment(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	taskID := "t-wt-add-moment"
	deliverableWTSeedWrapperRow(t, db, taskID, f.clientDir)
	t.Setenv(TaskWrapperEnv, taskID)
	defer t.Setenv(TaskWrapperEnv, "")

	// A SECOND worktree created by a raw `git worktree add` command from the
	// main repo (the executor's literal creation round).
	wt2 := filepath.Join(f.root, "wt2")
	deliverableWTGit(t, f.clientDir, "worktree", "add", "-b", "feat/add-moment", wt2, "master")

	// The seam fires on the command that created it (cwd = main repo, no
	// workdir param — the worktree path comes from the command line).
	TaskDeliverableWorktreeBash("", "git worktree add "+wt2+" -b feat/add-moment master")

	var d struct {
		Branch   string `json:"branch"`
		Worktree string `json:"worktree"`
	}
	if err := json.Unmarshal([]byte(deliverableWTRowBlob(t, db, taskID)), &d); err != nil {
		t.Fatalf("decode blob: %v", err)
	}
	if d.Branch != "feat/add-moment" || d.Worktree != wt2 {
		t.Fatalf("creation-moment seam must backfill the NEW worktree: branch=%q worktree=%q", d.Branch, d.Worktree)
	}
}

// TestTaskDeliverableWorktreeAccessors: the resume-facing accessors read
// back exactly what the creation-time backfill stamped.
func TestTaskDeliverableWorktreeAccessors(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	taskID := "t-wt-accessors"
	deliverableWTSeedWrapperRow(t, db, taskID, f.clientDir)

	if wt, branch := TaskDeliverableWorktreeFor(taskID); wt != "" || branch != "" {
		t.Fatalf("repo-only row must resolve no worktree yet: wt=%s branch=%s", wt, branch)
	}
	if err := taskBackfillDeliverableWorktree(taskID, f.wtDir, f.featBranch); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	wt, branch := TaskDeliverableWorktreeFor(taskID)
	if wt != f.wtDir || branch != f.featBranch {
		t.Fatalf("accessor mismatch: wt=%s branch=%s", wt, branch)
	}
	if repo := TaskDeliverableRepoFor(taskID); repo != f.clientDir {
		t.Fatalf("repo accessor mismatch: %s", repo)
	}
}

// TestTaskBackfillDeliverableWorktreeNeverRebindsToAnotherWorktree is the
// 2026-09-12 HIJACK regression (epic: worktree recycle). The backfill is a
// DETECTION seam: it may close a gap (repo-only row → repo+worktree), never
// re-point a row that already names a directory. The live incident: row
// 01a09250's deliverable read gopi-wtrecycle2, then a read-only probe of
// another task's worktree (`git -C /home/nick/workspace/gopi-cov-agent status`)
// rebound it to gopi-cov-agent — the row then BLOCKED the other task's
// recycle as a foreign owner (and was itself blocked by it), which is the
// mutual-lock shape the whole epic is about.
//
// RED before the fix: the second stamp overwrote worktree+branch.
func TestTaskBackfillDeliverableWorktreeNeverRebindsToAnotherWorktree(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	taskID := "t-wt-no-rebind"
	deliverableWTSeedWrapperRow(t, db, taskID, f.clientDir)

	if err := taskBackfillDeliverableWorktree(taskID, f.wtDir, f.featBranch); err != nil {
		t.Fatalf("first stamps: %v", err)
	}
	bound := deliverableWTRowBlob(t, db, taskID)

	other := filepath.Join(t.TempDir(), "gopi-someone-else")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := taskBackfillDeliverableWorktree(taskID, other, "feat/someone-else"); err != nil {
		t.Fatalf("second stamp must be a no-op, not an error: %v", err)
	}
	if after := deliverableWTRowBlob(t, db, taskID); after != bound {
		t.Fatalf("a bound row must NEVER be rebound by a detection probe:\nbefore %s\nafter  %s", bound, after)
	}
}

// TestTaskBackfillDeliverableWorktreeFillsABranchGapWhileBound: the guard is
// on the WORKTREE only — a row already bound to a directory may still learn
// its branch (the branch half was the reported backfill gap), so the
// no-rebind rule cannot be satisfied by disabling the seam.
func TestTaskBackfillDeliverableWorktreeFillsABranchGapWhileBound(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	taskID := "t-wt-branch-gap"
	deliverableWTSeedWrapperRow(t, db, taskID, f.clientDir)

	if err := taskBackfillDeliverableWorktree(taskID, f.wtDir, ""); err != nil {
		t.Fatalf("stamp worktree without branch: %v", err)
	}
	if blob := deliverableWTRowBlob(t, db, taskID); strings.Contains(blob, `"branch"`) {
		t.Fatalf("precondition: no branch expected yet: %s", blob)
	}
	if err := taskBackfillDeliverableWorktree(taskID, f.wtDir, f.featBranch); err != nil {
		t.Fatalf("fill the branch gap: %v", err)
	}
	if blob := deliverableWTRowBlob(t, db, taskID); !strings.Contains(blob, `"branch":"`+f.featBranch+`"`) {
		t.Fatalf("the same worktree must still be able to learn its branch: %s", blob)
	}
}

// ---------------------------------------------------------------------------
// Row-ownership of the creation-time backfill (epic raw-prompt-worktree-
// release, 2026-09-14).
//
// THE DEFECT: the seam resolved its row from GOPI_TASK_WRAPPER_ID alone, so the
// BARE-PROMPT face — which has no wrapper row and no wrapper env, and mints its
// row through the harness's own registration hook — was invisible to it. The
// executor could create a Rule-6 worktree, commit in it, and the row that DID
// exist for that run stayed empty; every reclaim arm is deliverable-keyed, so
// the directory was never released and the merger read the branch as
// worktree-protected ACTIVE BOND WORK. Measured e2e in
// cmd/agent/rawprompt_worktree_release_e2e_test.go.
//
// The two tests below are the minimal repros: the row the CALLER names must win
// over the ambient env (both directions — a caller with no row stamps nothing,
// and a caller naming a row is NOT silently redirected to the env's row).
// ---------------------------------------------------------------------------

// TestTaskDeliverableWorktreeBashForRowNeedsNoWrapperEnv pins the bare-prompt
// shape: no GOPI_TASK_WRAPPER_ID anywhere, the row passed explicitly — the
// backfill must still land.
func TestTaskDeliverableWorktreeBashForRowNeedsNoWrapperEnv(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	taskID := "t-wt-rawprompt-row"
	deliverableWTSeedWrapperRow(t, db, taskID, f.clientDir)
	// The bare-prompt face: the wrapper env is explicitly ABSENT.
	t.Setenv(TaskWrapperEnv, "")

	TaskDeliverableWorktreeBashForRow(taskID, f.wtDir, "git status")

	var d struct {
		Branch   string `json:"branch"`
		Worktree string `json:"worktree"`
	}
	if err := json.Unmarshal([]byte(deliverableWTRowBlob(t, db, taskID)), &d); err != nil {
		t.Fatalf("decode blob: %v", err)
	}
	if d.Worktree != f.wtDir || d.Branch != f.featBranch {
		t.Fatalf("a caller-named row must be stamped with no wrapper env present: worktree=%q branch=%q", d.Worktree, d.Branch)
	}
}

// TestTaskDeliverableWorktreeBashForRowIgnoresForeignEnvRow pins the other
// direction: the row the caller NAMES is the one stamped, never the row in the
// ambient env. A bare-prompt run whose process happens to inherit a wrapper env
// (the daemon-context leak class) must not write its worktree onto a FOREIGN
// row — that is the deliverable-hijack shape the 2026-09-12 fix closed for
// directories, here closed for ROW IDENTITY.
func TestTaskDeliverableWorktreeBashForRowIgnoresForeignEnvRow(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	own, foreign := "t-wt-own-row", "t-wt-foreign-row"
	deliverableWTSeedWrapperRow(t, db, own, f.clientDir)
	deliverableWTSeedWrapperRow(t, db, foreign, f.clientDir)
	t.Setenv(TaskWrapperEnv, foreign)
	defer t.Setenv(TaskWrapperEnv, "")

	TaskDeliverableWorktreeBashForRow(own, f.wtDir, "git status")

	if blob := deliverableWTRowBlob(t, db, own); !strings.Contains(blob, `"worktree":"`+f.wtDir+`"`) {
		t.Fatalf("the caller-named row must be the one stamped: %s", blob)
	}
	if blob := deliverableWTRowBlob(t, db, foreign); strings.Contains(blob, `"worktree"`) {
		t.Fatalf("the ambient env's row must NOT be touched when the caller names its own row: %s", blob)
	}
}

// TestTaskDeliverableWorktreeBashForRowWithoutRowIsNoop pins the guard: a
// caller that names no row and has no wrapper env stamps nothing (a plain
// chat/session with no ledger row must not guess one).
func TestTaskDeliverableWorktreeBashForRowWithoutRowIsNoop(t *testing.T) {
	f := newDeliverableWTFixture(t)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	t.Setenv(TaskWrapperEnv, "")

	TaskDeliverableWorktreeBashForRow("", f.wtDir, "git status") // must not panic / write

	ctx := context.Background()
	conn, err := openTasksDB(ctx, db)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer conn.Close()
	var n int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks`).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if n != 0 {
		t.Fatalf("a row-less caller must write nothing, found %d row(s)", n)
	}
}
