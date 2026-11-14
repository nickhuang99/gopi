package tools

// Regression suite for the WIP SNAPSHOT leg (epic: an interrupted run's
// uncommitted work survives it — task_wip_snapshot.go,
// task_successor_wip_pointer.go).
//
// WHAT IS PINNED, one test per arm of the contract:
//
//	① a DIRTY ground becomes a restorable deposit: the tracked half applies from
//	   tracked.patch, the untracked half is byte-identical from untracked/, and
//	   nothing was committed, stashed or cleaned to produce it (Rule 30);
//	② a CLEAN ground writes NOTHING — no `wip/` directory at all (the "zero write
//	   when there is nothing to save" half, which a `!= ""` assertion would miss);
//	③ the `failed` writeback runs the leg (taskUpdateStampedCore), and the note
//	   it files names the deposit — this is the ONLY leg left: the periodic
//	   lifecycle leg was deleted 2026-10-30 with the pass that carried it (see
//	   task_wip_snapshot.go's header), so the arm that drove it is gone rather
//	   than kept green against a leg production never ran;
//	④ a successor mint whose source row left a deposit puts the PATH in the new
//	   row's briefing, and one whose source row left none SAYS so (Rule 15);
//	⑤ the deposit never lands in a git-tracked path: the ground's porcelain is
//	   unchanged by the snapshot, and the deposit lives under the session dir.
//
// The ground is a REAL `git init` tree (never a mock): the measurement this leg
// depends on is git's own, and a fixture that does not run git would test the
// fixture. Directories come from the device scratch root (Rule 22/32).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/devworkspace"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// wipScratchDir is one scratch directory for this suite, off the `/` partition
// (Rule 22) and NOT inside the source tree (Rule 32).
func wipScratchDir(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(testutil.TestScratchDir(t, "wipsnap"), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("scratch %s: %v", dir, err)
	}
	return dir
}

// wipGit runs one git verb in dir and fails the test on a non-zero exit — the
// fixture builder's own instrument (a broken fixture must not look like a
// broken subject).
func wipGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Identity is supplied explicitly so the fixture does not depend on the
	// operator's git config being readable in a test process — and the caller's
	// repository routing is REMOVED (testutil.GitEnv, not os.Environ()): `cmd.Dir`
	// does not override GIT_DIR, so an inherited GIT_DIR would hand this fixture's
	// `git init` the operator's real repository (Rule 13/15's git-env class).
	cmd.Env = testutil.GitEnv(
		"GIT_AUTHOR_NAME=wip", "GIT_AUTHOR_EMAIL=wip@example.invalid",
		"GIT_COMMITTER_NAME=wip", "GIT_COMMITTER_EMAIL=wip@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// wipCloneGround builds a CLONE-shaped ground for taskID under a scratch
// workspace root and returns (repo, ground). This is the shape the epic's own
// run stands in (Rule 6c): `.git` is a DIRECTORY, the directory is named by the
// row's id, and it stands on the row's branch — the three facts
// reclaimRunCloneGroundIdentity reads.
func wipCloneGround(t *testing.T, taskID string) (repo, ground string) {
	t.Helper()
	repo = wipScratchDir(t, "repo")
	wipGit(t, repo, "init", "-q", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatalf("seed tracked file: %v", err)
	}
	wipGit(t, repo, "add", "tracked.txt")
	wipGit(t, repo, "commit", "-q", "-m", "seed")

	root := wipScratchDir(t, "wsroot")
	ground = devworkspace.Dir(root, taskID)
	wipGit(t, repo, "clone", "-q", repo, ground)
	wipGit(t, ground, "checkout", "-q", "-b", "task/"+taskID)
	// The workspace root must resolve to THIS scratch root, or the clone arm of
	// the attribution ladder never fires (config's own env channel, Rule 2).
	t.Setenv(config.WorkspaceRootEnvName, root)
	return repo, ground
}

// wipSeedRunRow creates the row the leg will measure, with the deliverable blob
// a real run carries (repo + branch + worktree).
func wipSeedRunRow(t *testing.T, taskID, repo, branch, worktree string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "seed row for the wip snapshot leg", TaskID: taskID,
		Payload: "acceptance: an interrupted run's work survives", Tags: "bug", Source: "llm"}); err != nil {
		t.Fatalf("submit %s: %v", taskID, err)
	}
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer closeDB()
	// A run has STARTED before it can fail: `failed` is not reachable from
	// `pending` (taskAllowedTransitions), and a row that never ran has no ground
	// to measure — so the fixture drives the same edge a real run does.
	if _, err := TaskUpdate(taskID, TaskStatusInProgress); err != nil {
		t.Fatalf("progress %s: %v", taskID, err)
	}
	blob := taskDeliverableRebuild(taskDeliverable{Repo: repo, Branch: branch, Worktree: worktree})
	if _, err := db.ExecContext(context.Background(),
		`UPDATE tasks SET deliverable = ? WHERE task_id = ?`, blob, taskID); err != nil {
		t.Fatalf("stamp deliverable on %s: %v", taskID, err)
	}
}

// wipDirty makes the ground hold BOTH halves of "uncommitted work": one modified
// tracked file and one new untracked file.
func wipDirty(t *testing.T, ground string) (trackedBody, untrackedBody string) {
	t.Helper()
	trackedBody = "one\nCHANGED IN THE RUN\n"
	if err := os.WriteFile(filepath.Join(ground, "tracked.txt"), []byte(trackedBody), 0o644); err != nil {
		t.Fatalf("dirty the tracked file: %v", err)
	}
	untrackedBody = "brand new work the run had not committed\n"
	if err := os.WriteFile(filepath.Join(ground, "untracked-new.txt"), []byte(untrackedBody), 0o644); err != nil {
		t.Fatalf("add the untracked file: %v", err)
	}
	return trackedBody, untrackedBody
}

// TestWIPSnapshotDirtyGroundIsRestorable is arm ① + ⑤: a dirty ground produces a
// deposit from which BOTH halves can be recovered, and producing it leaves the
// ground exactly as dirty as it was (nothing committed, cleaned or stashed).
func TestWIPSnapshotDirtyGroundIsRestorable(t *testing.T) {
	newVerifyTestDB(t)
	const id = "t-wipdirty"
	repo, ground := wipCloneGround(t, id)
	wipSeedRunRow(t, id, repo, "task/"+id, ground)
	trackedBody, untrackedBody := wipDirty(t, ground)

	before := reclaimMustPorcelain(t, ground)

	snap, err := SnapshotUncommittedWork(id, taskWIPTriggerFailed)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Worktree != ground {
		t.Fatalf("measured worktree = %q, want the row's own ground %q (the attribution ladder must not guess)", snap.Worktree, ground)
	}
	if snap.Clean || snap.Path == "" {
		t.Fatalf("a dirty ground must produce a deposit, got clean=%v path=%q note=%s", snap.Clean, snap.Path, snap.Note)
	}

	// ⑤ the ground is UNTOUCHED: same porcelain body, byte for byte.
	after := reclaimMustPorcelain(t, ground)
	if after != before {
		t.Fatalf("the snapshot changed the ground's status — it must only READ\nbefore:\n%s\nafter:\n%s", before, after)
	}
	// ⑤ the deposit is under the run's OWN session dir, never in the tree.
	sessionDir, derr := taskSessionDir(id)
	if derr != nil {
		t.Fatalf("session dir: %v", derr)
	}
	if !strings.HasPrefix(snap.Path, sessionDir+string(filepath.Separator)) {
		t.Fatalf("deposit %q is not under the run's session dir %q (Rule 37)", snap.Path, sessionDir)
	}
	if strings.HasPrefix(snap.Path, ground) {
		t.Fatalf("deposit %q is INSIDE the measured worktree — it would become git-visible dirt", snap.Path)
	}

	// ① the tracked half is restorable from the patch alone: apply it to a clean
	// checkout of the same base and compare the content.
	clean := wipScratchDir(t, "restore")
	wipGit(t, repo, "clone", "-q", repo, clean)
	patch := string(mustReadFile(t, filepath.Join(snap.Path, taskWIPTrackedPatchFile)))
	if !strings.Contains(patch, "CHANGED IN THE RUN") {
		t.Fatalf("tracked.patch does not carry the tracked change:\n%s", patch)
	}
	wipGit(t, clean, "apply", "--binary", filepath.Join(snap.Path, taskWIPTrackedPatchFile))
	if got := string(mustReadFile(t, filepath.Join(clean, "tracked.txt"))); got != trackedBody {
		t.Fatalf("restored tracked.txt = %q, want %q", got, trackedBody)
	}

	// ① the untracked half is a byte-identical copy at its own relative path.
	copied := filepath.Join(snap.Path, taskWIPUntrackedDirName, "untracked-new.txt")
	if got := string(mustReadFile(t, copied)); got != untrackedBody {
		t.Fatalf("copied untracked file = %q, want %q", got, untrackedBody)
	}
	if snap.UntrackedFiles != 1 {
		t.Fatalf("untracked files captured = %d, want 1", snap.UntrackedFiles)
	}

	// The hand-over note exists and names the restore recipe (it is the artifact
	// the successor reads FIRST — a missing one makes the pointer unusable).
	handoff := string(mustReadFile(t, filepath.Join(snap.Path, taskWIPHandoffFile)))
	if !strings.Contains(handoff, taskWIPTrackedPatchFile) || !strings.Contains(handoff, "git apply --binary --3way") {
		t.Fatalf("hand-over note does not carry a pasteable restore recipe:\n%s", handoff)
	}
}

// TestWIPSnapshotCleanGroundWritesNothing is arm ②: a clean ground creates no
// directory at all. Asserting "the wip dir is absent" (not "it is empty") is the
// point — an empty directory is still a write, and it would make every clean run
// leave a trace in the profile.
func TestWIPSnapshotCleanGroundWritesNothing(t *testing.T) {
	newVerifyTestDB(t)
	const id = "t-wipclean"
	repo, ground := wipCloneGround(t, id)
	wipSeedRunRow(t, id, repo, "task/"+id, ground)

	if body := reclaimMustPorcelain(t, ground); strings.TrimSpace(body) != "" {
		t.Fatalf("fixture is not clean:\n%s", body)
	}
	snap, err := SnapshotUncommittedWork(id, taskWIPTriggerFailed)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !snap.Clean || snap.Path != "" {
		t.Fatalf("a clean ground must write nothing, got clean=%v path=%q", snap.Clean, snap.Path)
	}
	sessionDir, derr := taskSessionDir(id)
	if derr != nil {
		t.Fatalf("session dir: %v", derr)
	}
	if _, statErr := os.Stat(filepath.Join(sessionDir, taskWIPDirName)); !os.IsNotExist(statErr) {
		t.Fatalf("a clean ground left %s behind (stat err = %v); the leg must write NOTHING", filepath.Join(sessionDir, taskWIPDirName), statErr)
	}
}

// TestFailedWritebackSnapshotsFirst is arm ③: the `failed` writeback itself runs
// the leg, so a writeback that is the last thing a dying process does still
// leaves the deposit on disk — and the row's trail says where.
func TestFailedWritebackSnapshotsFirst(t *testing.T) {
	newVerifyTestDB(t)
	const id = "t-wipfail"
	repo, ground := wipCloneGround(t, id)
	wipSeedRunRow(t, id, repo, "task/"+id, ground)
	wipDirty(t, ground)

	if _, err := TaskUpdateWithResult(id, TaskStatusFailed, "the run died on the ground"); err != nil {
		t.Fatalf("failed writeback: %v", err)
	}
	// The row IS failed (the snapshot never blocks the transition).
	if row := taskRowByID(t, id); row.Status != TaskStatusFailed {
		t.Fatalf("row status = %q, want failed", row.Status)
	}
	sessionDir, derr := taskSessionDir(id)
	if derr != nil {
		t.Fatalf("session dir: %v", derr)
	}
	deposit := filepath.Join(sessionDir, taskWIPDirName)
	if fi, statErr := os.Stat(deposit); statErr != nil || !fi.IsDir() {
		t.Fatalf("the failed writeback left no deposit at %s (stat err = %v)", deposit, statErr)
	}
	// The trail names it (Rule 15: a preservation nobody can find is not one).
	if detail := taskEventDetailFor(t, id, taskWIPSnapshotEvent); !strings.Contains(detail, deposit) {
		t.Fatalf("the row's trail does not name the deposit %q, tail:\n%s", deposit, detail)
	}
}

// TestSuccessorBriefingNamesTheSnapshotPath is arm ④, both directions: the
// successor's PAYLOAD carries the deposit's path when one exists, and the mint's
// line STATES the absence when one does not.
func TestSuccessorBriefingNamesTheSnapshotPath(t *testing.T) {
	newVerifyTestDB(t)
	const judged, bare = "t-wipre", "t-wipbare"

	// ① a judged row that left a deposit.
	repo, ground := wipCloneGround(t, judged)
	wipSeedRunRow(t, judged, repo, "task/"+judged, ground)
	wipDirty(t, ground)
	if _, err := TaskUpdateWithResult(judged, TaskStatusFailed, "the run died on the ground"); err != nil {
		t.Fatalf("fail %s: %v", judged, err)
	}
	sessionDir, derr := taskSessionDir(judged)
	if derr != nil {
		t.Fatalf("session dir: %v", derr)
	}
	deposit := filepath.Join(sessionDir, taskWIPDirName)

	out, err := TaskCloneWithOptions(context.Background(), TaskCloneOptions{
		TaskID: judged, Reason: "resume the interrupted work"})
	if err != nil {
		t.Fatalf("clone %s: %v", judged, err)
	}
	if !strings.Contains(out, TaskSuccessorWIPPathMarker+deposit) {
		t.Fatalf("the mint's line does not name the snapshot %q:\n%s", deposit, out)
	}
	successor := taskRowByID(t, cloneMintedSuccessorOf(t, judged))
	if !strings.Contains(successor.Payload, TaskSuccessorWIPMarker) ||
		!strings.Contains(successor.Payload, deposit) {
		t.Fatalf("successor briefing does not carry %q + %q — the relayer would never find the work:\n%s",
			TaskSuccessorWIPMarker, deposit, successor.Payload)
	}
	if !strings.Contains(successor.Payload, taskWIPTrackedPatchFile) {
		t.Fatalf("successor briefing names the deposit but not the patch inside it:\n%s", successor.Payload)
	}

	// ② a judged row with NO deposit: the line says so, the payload says nothing.
	wipSeedRunRow(t, bare, repo, "task/"+bare, "")
	if _, err := TaskUpdateWithResult(bare, TaskStatusFailed, "the run never got far enough"); err != nil {
		t.Fatalf("fail %s: %v", bare, err)
	}
	out, err = TaskCloneWithOptions(context.Background(), TaskCloneOptions{
		TaskID: bare, Reason: "resume the interrupted work"})
	if err != nil {
		t.Fatalf("clone %s: %v", bare, err)
	}
	if !strings.Contains(out, TaskSuccessorNoWIPMarker) {
		t.Fatalf("the mint's line must STATE the absence (%q), got:\n%s", TaskSuccessorNoWIPMarker, out)
	}
	if strings.Contains(taskRowByID(t, cloneMintedSuccessorOf(t, bare)).Payload, TaskSuccessorWIPMarker) {
		t.Fatalf("a successor with no deposit must not carry the snapshot block")
	}
}

// reclaimMustPorcelain is this suite's clean measurement of a ground: the SAME
// reader the leg itself uses (reclaimGitStatus), so a fixture and its subject can
// never disagree about what "dirty" means.
func reclaimMustPorcelain(t *testing.T, dir string) string {
	t.Helper()
	body, err := reclaimGitStatus(dir)
	if err != nil {
		t.Fatalf("git status --porcelain in %s: %v", dir, err)
	}
	return strings.TrimSpace(body)
}
