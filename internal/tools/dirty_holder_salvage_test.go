package tools

// Tests for the dirty dead-holder salvage arm (dirty_holder_salvage.go).
//
// The arm exists because the reclaim's guard ② refuses a DIRTY directory by
// design (uncommitted work is work) — and, before this change, that refusal
// was rendered and then forgotten: no row, no notification, no durable copy.
// The tests below pin the four properties the fix must hold: a row IS filed,
// the work IS captured (both halves — a tracked diff and the untracked files a
// diff can never show), the worktree is NOT touched, and ONE row per branch.

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
)

// salvageFixture is deadHolderReclaimFixture plus the isolation the salvage
// needs (a temp GOPI_HOME for the attachment container, a temp scratch home so
// a capture never lands in the operator's tree) and a DIRTY worktree.
func salvageFixture(t *testing.T) (repo, worktree, branch string) {
	t.Helper()
	repo, worktree, branch = deadHolderReclaimFixture(t)
	t.Setenv(config.GopiDirEnv, t.TempDir())
	t.Setenv(scratch.HomeEnvName, t.TempDir())
	// Tracked change + untracked file: the two halves a capture must cover.
	if err := os.WriteFile(filepath.Join(worktree, "a.txt"), []byte("base\nedited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "new_test.go"), []byte("package p // new file, never in a diff"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo, worktree, branch
}

// salvageSeedRow files the dead-holder row the arm keys on (a TERMINAL row
// whose claim pid is provably gone), and returns its id.
func salvageSeedRow(t *testing.T, repo, branch, worktree string) string {
	t.Helper()
	const id = "t-salvage"
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: id, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, id, taskStatusCompleted, repo, branch, worktree, hostPid(t, deadPID(t)))
	return id
}

// salvageRowFor finds the salvage row's id + deliverable by the deterministic
// summary, or "" when none was filed.
func salvageRowFor(t *testing.T, branch string) (string, string) {
	t.Helper()
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	var id, blob, status string
	err := db.QueryRowContext(ctx,
		`SELECT task_id, deliverable, status FROM tasks WHERE summary = ?`,
		dirtyHolderSalvageSummaryPrefix+branch).Scan(&id, &blob, &status)
	if err != nil {
		return "", ""
	}
	return id, blob + "|" + status
}

// salvageAttachmentByBase finds an attachment copy by basename under the
// task's session dir (the container TaskCopyAttachments writes into).
func salvageAttachmentByBase(t *testing.T, taskID, base string) string {
	t.Helper()
	gd, err := config.GopiDir()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(gd, taskSessionsRootName, taskID, taskAttachmentsDirName)
	var found string
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err // the walk must not hide an unreadable container (Rule 15)
		}
		if !d.IsDir() && d.Name() == base {
			found = p
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk attachments under %s: %v", root, err)
	}
	return found
}

// TestSalvageDirtyDeadHolder_FilesARowWithBothHalvesCaptured is the headline
// acceptance: the dirty state produces ONE row whose attachments carry the
// tracked diff AND the untracked file, with the untracked content verifiable
// byte-for-byte out of the archive.
func TestSalvageDirtyDeadHolder_FilesARowWithBothHalvesCaptured(t *testing.T) {
	repo, wt, branch := salvageFixture(t)
	salvageSeedRow(t, repo, branch, wt)

	held := ReclaimHeldBranches(repo)
	if why, ok := held[branch]; !ok || !strings.Contains(why, reclaimSkipDirtyReason) {
		t.Fatalf("precondition: the arm must refuse this branch as DIRTY, got held=%v", held)
	}
	lines := SalvageDirtyDeadHolderWorktrees(repo, held)
	if len(lines) != 1 || !strings.Contains(lines[0], branch) {
		t.Fatalf("one salvage line naming the branch, got %v", lines)
	}

	id, meta := salvageRowFor(t, branch)
	if id == "" {
		t.Fatal("no salvage row was filed for the dirty dead-holder worktree")
	}
	if !strings.Contains(meta, branch) || !strings.Contains(meta, taskStatusPending) {
		t.Errorf("the row must be claimable (pending) and carry the branch for dedup, got %s", meta)
	}

	// The tracked half: a real diff of the edited file.
	patchPath := salvageAttachmentByBase(t, id, dirtyHolderTrackedPatchName)
	if patchPath == "" {
		t.Fatal("the tracked diff was not attached")
	}
	patch, err := os.ReadFile(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(patch), "a.txt") || !strings.Contains(string(patch), "+base\n+edited") {
		t.Errorf("the attached patch must carry the tracked change, got:\n%s", patch)
	}

	// The untracked half: a file no diff would ever show.
	arcPath := salvageAttachmentByBase(t, id, dirtyHolderUntrackedArchiveName)
	if arcPath == "" {
		t.Fatal("the untracked archive was not attached")
	}
	got := readTarEntry(t, arcPath, "new_test.go")
	if !strings.Contains(got, "never in a diff") {
		t.Errorf("the untracked file's content must survive the capture, got %q", got)
	}
}

// TestSalvageDirtyDeadHolder_LeavesTheWorktreeUntouched pins the promise the
// operator's line makes: the salvage DISPATCHES, it never commits, cleans or
// removes. An arm that "helpfully" committed would destroy the human's choice.
func TestSalvageDirtyDeadHolder_LeavesTheWorktreeUntouched(t *testing.T) {
	repo, wt, branch := salvageFixture(t)
	salvageSeedRow(t, repo, branch, wt)

	before := gitStatusPorcelain(t, wt)
	headBefore := gitHead(t, wt)
	if lines := SalvageDirtyDeadHolderWorktrees(repo, ReclaimHeldBranches(repo)); len(lines) != 1 {
		t.Fatalf("expected one salvage line, got %v", lines)
	}
	if after := gitStatusPorcelain(t, wt); after != before {
		t.Errorf("the worktree must be byte-identical after the capture:\nbefore: %s\nafter:  %s", before, after)
	}
	if after := gitHead(t, wt); after != headBefore {
		t.Errorf("the salvage must not commit: HEAD moved %s → %s", headBefore, after)
	}
	if _, err := os.Stat(filepath.Join(wt, "new_test.go")); err != nil {
		t.Errorf("the untracked file must still exist in place: %v", err)
	}
	// And the branch itself is untouched — the arm never rewrites it.
	if got := strings.TrimSpace(strings.SplitN(gitHead(t, wt), "\n", 2)[0]); got != headBefore {
		t.Errorf("branch tip changed: %s → %s", headBefore, got)
	}
}

// TestSalvageDirtyDeadHolder_DedupsToOneRow: every merger tick sees the same
// dirty state, so the arm must file ONE row per branch however often it runs.
func TestSalvageDirtyDeadHolder_DedupsToOneRow(t *testing.T) {
	repo, wt, branch := salvageFixture(t)
	salvageSeedRow(t, repo, branch, wt)

	held := ReclaimHeldBranches(repo)
	if lines := SalvageDirtyDeadHolderWorktrees(repo, held); len(lines) != 1 {
		t.Fatalf("first pass must file one row, got %v", lines)
	}
	if lines := SalvageDirtyDeadHolderWorktrees(repo, held); len(lines) != 0 {
		t.Errorf("second pass must be a no-op (dedup), got %v", lines)
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE summary = ?`,
		dirtyHolderSalvageSummaryPrefix+branch).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("exactly ONE salvage row per branch, got %d", n)
	}
}

// TestSalvageDirtyDeadHolder_IgnoresOtherRefusalReasons: only the arm's own
// DIRTY refusal is salvageable. A branch held for any other reason (a live
// process inside, a re-attached branch, a foreign path) is a different fact
// and must not be turned into a "salvage" row behind a dirty label.
func TestSalvageDirtyDeadHolder_IgnoresOtherRefusalReasons(t *testing.T) {
	repo, wt, branch := salvageFixture(t)
	salvageSeedRow(t, repo, branch, wt)

	held := map[string]string{branch: "no live process check failed: path is a linked worktree of ANOTHER repo"}
	if lines := SalvageDirtyDeadHolderWorktrees(repo, held); len(lines) != 0 {
		t.Errorf("a non-dirty refusal must not be salvaged, got %v", lines)
	}
	if id, _ := salvageRowFor(t, branch); id != "" {
		t.Errorf("no row may be filed for a non-dirty refusal (got %s)", id)
	}
}

// TestSalvageKillSwitch_ReportsWithoutFiling: the switch is report-only — an
// operator who turned it off still sees that the state exists (Rule 15: no
// silent skip), and no row is minted.
func TestSalvageKillSwitch_ReportsWithoutFiling(t *testing.T) {
	repo, wt, branch := salvageFixture(t)
	salvageSeedRow(t, repo, branch, wt)
	t.Setenv(DirtyHolderSalvageEnv, "0")

	lines := SalvageDirtyDeadHolderWorktrees(repo, ReclaimHeldBranches(repo))
	if len(lines) != 1 || !strings.Contains(lines[0], dirtyHolderSalvageDisabledMarker) {
		t.Fatalf("the kill switch must report the skipped dispatch, got %v", lines)
	}
	if id, _ := salvageRowFor(t, branch); id != "" {
		t.Errorf("the kill switch must file nothing (got %s)", id)
	}
}

// TestMergerSweepCarriesTheSalvageLines is the wiring half: the sweep result
// the merger's summary renders must carry the arm's lines — a row filed
// silently (invisible in the tick output) is the defect this closes.
func TestMergerSweepCarriesTheSalvageLines(t *testing.T) {
	repo, wt, branch := salvageFixture(t)
	salvageSeedRow(t, repo, branch, wt)

	res := MergerRepoSweep(repo, 0)
	if len(res.DirtySalvage) != 1 || !strings.Contains(res.DirtySalvage[0], branch) {
		t.Fatalf("the sweep must carry the salvage line, got %v (held=%v)", res.DirtySalvage, res.Held)
	}
	if res.Protected != 1 {
		t.Errorf("precondition: the branch stays worktree-protected (protected=%d)", res.Protected)
	}
}

// --- helpers ---

func gitStatusPorcelain(t *testing.T, dir string) string {
	t.Helper()
	out := gitOutput(t, dir, "status", "--porcelain")
	return out
}

func gitHead(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := finishCleanupGitRaw(context.Background(), dir, args...)
	if err != nil {
		t.Fatalf("git %s in %s: %v", strings.Join(args, " "), dir, err)
	}
	return out
}

// readTarEntry returns the content of name inside a .tar.gz archive.
func readTarEntry(t *testing.T, archive, name string) string {
	t.Helper()
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			t.Fatalf("%s not found in %s", name, archive)
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name != name {
			continue
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
}
