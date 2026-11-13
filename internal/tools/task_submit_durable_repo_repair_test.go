package tools

// task_submit_durable_repo_repair_test.go — THE DEAD-WORKSPACE REPAIR
// (epic 01a15629).
//
// The acceptance has three faces and each is one test below:
//
//	① a workspace path that is GONE is repaired to the repository its own row
//	   recorded, and the fact is returned as one line (never silent, Rule 15);
//	② a repo that is NOT under the workspace root is handed back byte-identical
//	   with no note, no stat and no git process — a normal submit is unchanged
//	   (measured at the PROCESS boundary, not inferred from the source);
//	③ a path that is ALIVE is identity: a live repository, including a live
//	   workspace clone, is returned untouched (idempotence — the repair can run
//	   on every seam without ever moving a value twice).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/devworkspace"
	"github.com/nick/gopi/internal/testutil"
)

// repairFixture builds a source repository and a workspace root, points the ONE
// workspace-root reader at it, and returns both paths.
func repairFixture(t *testing.T) (source, root string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not installed: the repair asks git whether a path is a work tree")
	}
	base := t.TempDir()
	source = filepath.Join(base, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	repairGit(t, source, "init", "-q", "-b", "master")
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repairGit(t, source, "add", "a.txt")
	repairGit(t, source, "commit", "-q", "-m", "init")
	root = filepath.Join(base, "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// The env channel WINS over settings, so an inherited value would silently
	// redirect the fixture's root (and make the boundary assertions lie).
	t.Setenv(config.WorkspaceRootEnvName, root)
	return source, root
}

func repairGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = testutil.GitEnv(
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// repairSeedRow seeds a row through the PRODUCTION submit (so the schema is the
// ledger's own, never a hand-rolled INSERT), then stamps the deliverable the
// executor's backfill writes: repo + worktree.
func repairSeedRow(t *testing.T, taskID, repo, worktree string) {
	t.Helper()
	t.Setenv(config.SkipClaimBroadcastEnv, "1")
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "seed", Payload: "seed", TaskID: taskID, Source: "human",
	}); err != nil {
		t.Fatalf("seed row %s: %v", taskID, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskRepairLedgerTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open the ledger: %v", err)
	}
	defer db.Close() //stylegate:ignore process-lifetime handle closed by the fixture
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = 'in_progress', deliverable = ? WHERE task_id = ?`,
		taskDeliverableRebuild(taskDeliverable{Repo: repo, Worktree: worktree}), taskID); err != nil {
		t.Fatalf("stamp row %s: %v", taskID, err)
	}
}

// TestRepairDurableRepoRecoversADeadWorkspaceFromItsRow is ①: the parent
// workspace is GONE, and the repair still answers with the live repository its
// own ledger row recorded — printing why.
func TestRepairDurableRepoRecoversADeadWorkspaceFromItsRow(t *testing.T) {
	source, root := repairFixture(t)
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))

	// The parent's workspace, as the executor stamped it: repo = the repository,
	// worktree = the per-run checkout — and then the checkout is RECLAIMED.
	workspace := devworkspace.Dir(root, "01a15629-0000-7000-8000-0000000pare")
	repairSeedRow(t, "01a15629-0000-7000-8000-0000000pare", source, workspace)
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatalf("fixture: the dead workspace must not exist (%v)", err)
	}

	got, note := RepairDurableRepoForSubmit(workspace)
	if SameDir(got, workspace) {
		t.Fatalf("the repair must not hand back the DEAD workspace %q — that is the shape the clone channel refused", got)
	}
	if !SameDir(got, source) {
		t.Fatalf("repaired repo = %q, want the repository row 01a15629-…pare recorded (%q)", got, source)
	}
	for _, want := range []string{workspace, source, "01a15629-0000-7000-8000-0000000pare", TaskRepairDeadWorkspaceSourceLabel} {
		if !strings.Contains(note, want) {
			t.Fatalf("the repair must be reported with %q (Rule 15: a repair nobody can read is a silent one); got: %q", want, note)
		}
	}
	// IDEMPOTENT: the answer is a live repository, so a second pass is identity
	// with no note — the seam can run on every submit.
	if again, note2 := RepairDurableRepoForSubmit(got); !SameDir(again, got) || note2 != "" {
		t.Fatalf("a second pass must be identity and silent; got (%q, %q)", again, note2)
	}
}

// TestRepairDurableRepoLeavesANormalRepoByteIdentical is ②: a repo outside the
// workspace root — every ordinary project on this host — is handed back
// verbatim, with no note and NO GIT PROCESS. The count is measured at the
// process boundary with a shim, so it cannot be satisfied by reading the source.
func TestRepairDurableRepoLeavesANormalRepoByteIdentical(t *testing.T) {
	source, _ := repairFixture(t)
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	calls := repairInstallGitCallRecorder(t)

	got, note := RepairDurableRepoForSubmit(source)
	if got != source {
		t.Fatalf("a repo outside the workspace root must come back byte-identical; got %q want %q", got, source)
	}
	if note != "" {
		t.Fatalf("nothing may be reported for a normal repo; got: %q", note)
	}
	if n := calls(); n != 0 {
		t.Fatalf("a repo outside the workspace root must spawn no git process; the repair ran git %d time(s)", n)
	}
}

// TestRepairDurableRepoLeavesALiveWorkspaceAlone is ③: a workspace that is
// still on disk and still a git work tree is IDENTITY — the repair may never
// move a value that is already usable.
func TestRepairDurableRepoLeavesALiveWorkspaceAlone(t *testing.T) {
	source, root := repairFixture(t)
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))

	live := devworkspace.Dir(root, "01a15629-0000-7000-8000-0000000live")
	repairGit(t, root, "clone", "-q", source, live)
	repairSeedRow(t, "01a15629-0000-7000-8000-0000000live", source, live)

	got, note := RepairDurableRepoForSubmit(live)
	if got != live || note != "" {
		t.Fatalf("a live workspace must be identity and silent; got (%q, %q)", got, note)
	}
}

// TestRepairDurableRepoReportsAnUnprovableRepair is the honest MISS: the path is
// dead and NO row records it, so nothing can be recovered — the input comes back
// with a note that says exactly that. The row keeps the path it was given; the
// clone channel then refuses it naming the REAL reason.
func TestRepairDurableRepoReportsAnUnprovableRepair(t *testing.T) {
	_, root := repairFixture(t)
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))

	orphan := filepath.Join(root, "01a15629-0000-7000-8000-0000000orph")
	got, note := RepairDurableRepoForSubmit(orphan)
	if got != orphan {
		t.Fatalf("an unprovable repair must not invent a repository; got %q want %q", got, orphan)
	}
	for _, want := range []string{orphan, root, DeliverableWorktreeKey} {
		if !strings.Contains(note, want) {
			t.Fatalf("the miss must name %q so the operator knows what was looked for; got: %q", want, note)
		}
	}
}

// repairInstallGitCallRecorder puts a counting shim in front of git for the rest
// of the test and returns a reader for the count (the same technique the clone
// channel's own test uses — "spawned no git process" is a measurement).
func repairInstallGitCallRecorder(t *testing.T) func() int {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not installed: the recorder wraps a real git")
	}
	dir := t.TempDir()
	countFile := filepath.Join(dir, "calls")
	shim := filepath.Join(dir, "git")
	script := "#!/bin/sh\necho call >> " + countFile + "\nexec " + realGit + " \"$@\"\n"
	if err := os.WriteFile(shim, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() int {
		body, err := os.ReadFile(countFile)
		if err != nil {
			return 0
		}
		return len(strings.Fields(string(body)))
	}
}
