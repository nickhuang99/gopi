package tools

// The BRANCH axis of the claim/creation-time ground stamp (epic 01a0b182).
//
// THE GAP THESE TESTS PIN. Every constructor of the (branch, worktree) pair on
// the claim/creation path funnels through taskWorktreeOwningDir, which resolves
// a *LINKED worktree* of the row's repo and — by construction — can never match
// the repo's MAIN checkout (its first `git worktree list --porcelain` record is
// the main checkout and is excluded). A run that creates its branch IN PLACE —
// the ordinary `cd <repo> && git checkout -b feat/x`, no `git worktree add`,
// which is what a run working in the main checkout does — therefore records NO
// ground for the whole in-flight window, and the branch reaches the ledger only
// at COMPLETION (taskBackfillDeliverableBranch, guarded `t.Status ==
// completed`). That is the producer half of the merger's `unowned:`/gap class:
// branchAttributionRows loads no row for the branch, the classifier returns
// branchAttribUnowned, and the merger render prints the branch as a GAP while
// its owner is demonstrably still working on it.
//
// The tests below are RED before the change by construction: the resolver they
// exercise does not exist yet, so the row keeps its submit-time repo-only blob.
// The report records the mutation red-proof for each half.

import (
	"context"
	"encoding/json"
	"github.com/nickhuang99/gopi/internal/scratch"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// inPlaceBranchFixture is a real repo checked out on its MAIN checkout, whose
// HEAD has been moved to a fresh feature branch by the ordinary in-place
// `git checkout -b` — the shape a run working in the main checkout leaves
// behind, and the ONE shape taskWorktreeOwningDir cannot see.
type inPlaceBranchFixture struct {
	t      *testing.T
	repo   string
	branch string
}

func newInPlaceBranchFixture(t *testing.T, branch string) *inPlaceBranchFixture {
	t.Helper()
	root := t.TempDir()
	f := &inPlaceBranchFixture{t: t, repo: filepath.Join(root, "repo"), branch: branch}
	deliverableWTGit(t, root, "init", f.repo)
	deliverableWTGit(t, f.repo, "config", "user.email", "t@t")
	deliverableWTGit(t, f.repo, "config", "user.name", "t")
	deliverableWTGit(t, f.repo, "checkout", "-b", masterBranchName)
	if err := os.WriteFile(filepath.Join(f.repo, "a.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	deliverableWTGit(t, f.repo, "add", "a.txt")
	deliverableWTGit(t, f.repo, "commit", "-m", "base")
	// THE IN-PLACE CREATION: no worktree, HEAD merely moves.
	deliverableWTGit(t, f.repo, "checkout", "-b", branch)
	return f
}

// inPlaceRowBlob seeds a repo-only row (the submit shape) and returns its raw
// deliverable blob after running fn.
func inPlaceRowBlob(t *testing.T, taskID, repo string, fn func()) string {
	t.Helper()
	deliverableWTSeedWrapperRow(t, os.Getenv(memoryDBEnv), taskID, repo)
	fn()
	return deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), taskID)
}

// TestInPlaceBranchIsStampedAtClaimTime is the epic's core acceptance for the
// branch axis: a row claimed for a run whose ground is the repo's MAIN
// checkout on a NON-exempt branch must record that branch on its OWN row while
// the row is still in flight — no linked worktree anywhere.
//
// The worktree axis stays EMPTY on purpose: there is no linked worktree, and a
// stamp that invented one would be a lie about the ground (Rule 15).
func TestInPlaceBranchIsStampedAtClaimTime(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	f := newInPlaceBranchFixture(t, "feat/inplace-claim")

	blob := inPlaceRowBlob(t, "t-inplace-claim", f.repo, func() {
		_, _, written := taskWorktreeStampClaimer("t-inplace-claim", f.repo, f.repo)
		if !written {
			t.Errorf("claim-time stamp must record the in-place branch (repo %s, branch %s): written=false", f.repo, f.branch)
		}
	})

	var d struct {
		Repo     string `json:"repo"`
		Branch   string `json:"branch"`
		Worktree string `json:"worktree"`
	}
	if err := json.Unmarshal([]byte(blob), &d); err != nil {
		t.Fatalf("blob is not valid JSON (%s): %v", blob, err)
	}
	if d.Branch != f.branch {
		t.Fatalf("in-flight row must name the in-place branch: branch=%q, want %q (blob %s)", d.Branch, f.branch, blob)
	}
	if d.Repo != f.repo {
		t.Fatalf("repo must be preserved: %q, want %q", d.Repo, f.repo)
	}
	if d.Worktree != "" {
		t.Fatalf("no linked worktree exists, so the worktree axis must stay empty (got %q in %s)", d.Worktree, blob)
	}
}

// TestInPlaceBranchIsStampedAtCreationTime is the SAME axis through the
// executor's bash-round seam: the round that ran `git checkout -b` (or
// `git switch -c`) from the main checkout records the branch immediately,
// not at completion.
//
// The round's workdir is DELIBERATELY an unrelated directory — the session dir
// the agent actually stands in (measured live: the agent's own bash calls run
// from the session dir and reach the repository through a `cd` INSIDE the
// command, e.g. `cd <repo> && git commit …`). Before this change the candidate
// scan read only the round's workdir and git's `-C`, so such a round produced
// NO candidate at all and the row stayed repo-only — the exact live shape.
func TestInPlaceBranchIsStampedAtCreationTime(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	f := newInPlaceBranchFixture(t, "feat/inplace-seam")
	sessionDir := t.TempDir()

	blob := inPlaceRowBlob(t, "t-inplace-seam", f.repo, func() {
		TaskDeliverableWorktreeBashForRow("t-inplace-seam", sessionDir, "cd "+f.repo+" && git switch -c "+f.branch)
	})

	var d struct {
		Repo     string `json:"repo"`
		Branch   string `json:"branch"`
		Worktree string `json:"worktree"`
	}
	if err := json.Unmarshal([]byte(blob), &d); err != nil {
		t.Fatalf("blob is not valid JSON (%s): %v", blob, err)
	}
	if d.Branch != f.branch {
		t.Fatalf("the creation seam must stamp the in-place branch: branch=%q, want %q (blob %s)", d.Branch, f.branch, blob)
	}
	if d.Worktree != "" {
		t.Fatalf("the worktree axis must stay empty for an in-place branch (got %q in %s)", d.Worktree, blob)
	}
	if n := strings.Count(blob, `"`+DeliverableBranchKey+`"`); n != 1 {
		t.Fatalf("exactly ONE branch key is allowed (one spelling, one writer), got %d in %s", n, blob)
	}
}

// TestInPlaceBranchNotStampedOnExemptBranch: the main checkout on master is
// NOT a deliverable — the claim-time stamp must keep leaving the blob alone.
// The exemption is the ONE predicate every other path shares (taskBranchExempt),
// so an integration branch can never be recorded as a task's deliverable.
func TestInPlaceBranchNotStampedOnExemptBranch(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	deliverableWTGit(t, root, "init", repo)
	deliverableWTGit(t, repo, "config", "user.email", "t@t")
	deliverableWTGit(t, repo, "config", "user.name", "t")
	deliverableWTGit(t, repo, "checkout", "-b", masterBranchName)

	before := inPlaceRowBlob(t, "t-inplace-exempt", repo, func() {})
	if _, branch, written := taskWorktreeStampClaimer("t-inplace-exempt", repo, repo); written || branch != "" {
		t.Fatalf("master is exempt: the stamp must not record it (branch=%q written=%v)", branch, written)
	}
	if after := deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), "t-inplace-exempt"); after != before {
		t.Fatalf("an exempt ground must not touch the blob:\n before=%s\n after =%s", before, after)
	}
}

// TestInPlaceBranchOwnershipGuardRefusesAForeignRepo: the in-place resolution
// must be anchored on the row's OWN repo. A row claiming its ground in a
// DIFFERENT repository (the foreign-cwd trap TaskRepoForLaunch documents) must
// not adopt that repository's branch.
func TestInPlaceBranchOwnershipGuardRefusesAForeignRepo(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	mine := newInPlaceBranchFixture(t, "feat/inplace-mine")
	theirs := newInPlaceBranchFixture(t, "feat/inplace-theirs")

	before := inPlaceRowBlob(t, "t-inplace-foreign", mine.repo, func() {})
	if _, branch, written := taskWorktreeStampClaimer("t-inplace-foreign", mine.repo, theirs.repo); written || branch != "" {
		t.Fatalf("a foreign repo's ground must never be adopted (branch=%q written=%v)", branch, written)
	}
	if after := deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), "t-inplace-foreign"); after != before {
		t.Fatalf("the refusal must leave the blob untouched:\n before=%s\n after =%s", before, after)
	}
}

// TestInPlaceBranchOwnershipScan runs the ownership scan directly for the
// in-place axis: a branch ALREADY named by another row is not this row's to
// take, and the scan must see the BRANCH (not only the worktree key) so a
// branch-only row is protected from a second owner.
func TestInPlaceBranchOwnershipScan(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	f := newInPlaceBranchFixture(t, "feat/inplace-owned")

	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "owner", TaskID: "t-inplace-owner", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, deliverable = ? WHERE task_id = ?`,
		taskStatusInProgress, `{"repo":"`+f.repo+`","branch":"`+f.branch+`"}`, "t-inplace-owner"); err != nil {
		t.Fatalf("seed owner: %v", err)
	}

	owner, err := taskBranchRowOwner("t-inplace-claimer", f.repo, f.branch)
	if err != nil {
		t.Fatalf("branch ownership scan: %v", err)
	}
	if owner != "t-inplace-owner" {
		t.Fatalf("branch ownership scan = %q, want t-inplace-owner — a branch another row already names must not be taken by a second row", owner)
	}
	// The row that OWNS the branch is not reported as a foreign owner of itself.
	if self, err := taskBranchRowOwner("t-inplace-owner", f.repo, f.branch); err != nil || self != "" {
		t.Fatalf("self-ownership must not be reported: owner=%q err=%v", self, err)
	}
	// A DIFFERENT repo's identically-named branch is a different ground.
	if other, err := taskBranchRowOwner("t-inplace-claimer", filepath.Join(t.TempDir(), "elsewhere"), f.branch); err != nil || other != "" {
		t.Fatalf("a branch of another repo must not be read as this one's: owner=%q err=%v", other, err)
	}
}

// TestInPlaceBranchIsAttributedNotUnowned is the EPIC's end-to-end half, and it
// runs the PRODUCTION functions — the real loader, the real classifier and the
// real render (no second mapper):
//
//	claim + in-place branch  →  the row names the branch (in flight)
//	                         →  branchAttributionRows loads a row for it
//	                         →  BranchAttribution names the owner (NOT unowned)
//	                         →  mergeCandidateLines/renderMergeCandidates print
//	                            `task <id>`, never the no-row GAP note.
//
// The GAP note (candidateNoRowNote) is the exact sentence the epic is about —
// the merger telling an operator that nobody owns a branch whose owner is
// demonstrably still working.
func TestInPlaceBranchIsAttributedNotUnowned(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	f := newInPlaceBranchFixture(t, "feat/inplace-attrib")
	const taskID = "t-inplace-attrib"

	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "attribution", TaskID: taskID, Repo: f.repo}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	// The REAL claim path (the wrapper's own transition), then the claim-time
	// ground stamp the wrapper runs before spawning the child.
	if err := TaskClaimExplicit(taskID, "attrib:1"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := TaskStart(taskID, "attrib:1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := claimGroundRowStatus(t, taskID); got != TaskStatusInProgress {
		t.Fatalf("row status = %q, want %q — the attribution must be exercised IN FLIGHT", got, TaskStatusInProgress)
	}
	if _, _, written := taskWorktreeStampClaimer(taskID, f.repo, f.repo); !written {
		t.Fatalf("the claim-time stamp must record the in-place branch %s", f.branch)
	}

	// The classifier, on the ledger the production loader reads.
	d := BranchAttribution(f.repo, f.branch, branchAttribRows(t), scratch.HolderScan{}, true)
	if d.Verdict == branchAttribUnowned {
		t.Fatalf("an in-flight row's in-place branch reads %s — this IS the gap class the epic closes (reason %q, evidence %v)",
			branchAttribUnowned, d.Reason, d.Evidence)
	}
	if d.Task != taskID {
		t.Fatalf("attributed task = %q, want %s (the merger must be able to name WHO is working on the branch)", d.Task, taskID)
	}

	// The render the epic reuses. The branch must appear, attributed, with the
	// owner named — never as the no-row gap.
	lines, errMsg := BranchAttributionLines(f.repo)
	if errMsg != "" {
		t.Fatalf("branch attribution unavailable: %s", errMsg)
	}
	mcs := mergeCandidateLines(lines)
	found := false
	for _, mc := range mcs {
		if mc.Branch != f.branch {
			continue
		}
		found = true
		if mc.Task != taskID {
			t.Fatalf("render attributed %s to %q, want %s (note: %s)", f.branch, mc.Task, taskID, mc.Note)
		}
		if strings.Contains(mc.Note, candidateNoRowNote) {
			t.Fatalf("the branch is still rendered as the no-row GAP: %s", mc.Note)
		}
	}
	if !found {
		t.Fatalf("the freshly claimed branch %s is missing from the attribution render entirely", f.branch)
	}
	// renderMergeCandidates is the merged-repo face's OWN composer; it must show
	// the task clause for this branch (one mapper, reused).
	rendered := renderMergeCandidates(lines, BranchInventory{Total: len(lines)})
	if !strings.Contains(rendered, taskID) {
		t.Fatalf("the merge-candidate render does not name the owning row %s:\n%s", taskID, rendered)
	}
	if strings.Contains(rendered, candidateNoRowNote) {
		t.Fatalf("the merge-candidate render still prints the no-row GAP note:\n%s", rendered)
	}
}

// TestBranchCreationsFromCommand pins the Rule 15b trigger (the reader's
// precision is what keeps the new signal from being noise): only a CREATING
// spelling counts, and a branch-named-then-chained command counts.
func TestBranchCreationsFromCommand(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
		why  string
	}{
		{"git checkout -b feat/x", true, "the plain in-place creation"},
		{"cd /repo && git checkout -b feat/x", true, "the measured live shape"},
		{`cd /repo && git commit -m "x"`, false, "a commit names no branch"},
		{"/usr/bin/git switch -c feat/x", true, "an absolute git path still reads"},
		{"git -C /repo checkout -b feat/x", true, "git's own -C form"},
		{"git -c core.hooksPath=/dev/null switch -C feat/x", true, "the force spelling behind a global value flag"},
		{"git checkout -b feat/x && git commit -m y", true, "chained after the creation"},
		{"git checkout -- a.txt", false, "a file restore is not a creation"},
		{"git switch feat/x", false, "switching to an EXISTING branch creates nothing"},
		{"git worktree add /w -b feat/x", false, "a worktree add is the worktree axis's own seam"},
		{"echo checkout -b", false, "prose is not a command"},
		{"git status", false, "an ordinary round"},
	}
	for _, c := range cases {
		if got := taskBranchCreationsFromCommand(c.cmd); got != c.want {
			t.Errorf("taskBranchCreationsFromCommand(%q) = %v, want %v (%s)", c.cmd, got, c.want, c.why)
		}
	}
}

// TestInPlaceBranchMixedVersionReadKeepsWorking is the schema-compatibility
// acceptance: a row written by an OLDER binary — repo + branch, no worktree
// key at all — keeps reading correctly through every reader this change
// touches. No schema break: the blob stays v1-shaped and the branch axis is
// what the attribution classifies on.
func TestInPlaceBranchMixedVersionReadKeepsWorking(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	repo := filepath.Join(t.TempDir(), "old-repo")
	const branch = "feat/old-row-shape"

	// The OLD shape, byte-for-byte: repo + branch, no worktree key.
	oldBlob := `{"repo":"` + repo + `","branch":"` + branch + `"}`
	deliverableWTSeedWrapperRow(t, os.Getenv(memoryDBEnv), "t-old-shape", repo)
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET deliverable = ? WHERE task_id = ?`, oldBlob, "t-old-shape"); err != nil {
		t.Fatalf("seed old-shape blob: %v", err)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	if got := deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), "t-old-shape"); got != oldBlob {
		t.Fatalf("the old row must read back byte-identical before any write: got %s", got)
	}
	if r, b, w := taskRowDeliverable("t-old-shape"); r != repo || b != branch || w != "" {
		t.Fatalf("readers must still resolve the old shape: repo=%q branch=%q worktree=%q", r, b, w)
	}
	// The new writer may re-render such a row (the blob builder writes its keys
	// in a fixed order, which is not necessarily the order an OLD binary wrote
	// them) but the row's MEANING must be unchanged and the worktree axis must
	// stay absent: a reader of either version sees the same three fields. That
	// is the compatibility contract — the schema is the field NAMES, never the
	// byte order.
	if err := taskBackfillDeliverableWorktree("t-old-shape", "", branch); err != nil {
		t.Fatalf("branch-only re-stamp: %v", err)
	}
	after := deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), "t-old-shape")
	if r, b, w := taskRowDeliverable("t-old-shape"); r != repo || b != branch || w != "" {
		t.Fatalf("a redundant branch stamp must not change the row's meaning:\n before=%s\n after =%s (repo=%q branch=%q worktree=%q)", oldBlob, after, r, b, w)
	}
	if strings.Contains(after, `"`+DeliverableWorktreeKey+`"`) {
		t.Fatalf("a branch-only stamp must never invent a worktree key: %s", after)
	}
	if n := strings.Count(after, `"`+DeliverableBranchKey+`"`); n != 1 {
		t.Fatalf("exactly ONE branch key is allowed, got %d in %s", n, after)
	}
}
