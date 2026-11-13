// Tests for the GIT HALF of a deliverable answer
// (task_deliverable_git.go) — what `gopi task show --branch/--worktree`
// answers when the LEDGER has no row.
//
// THE DEFECT THESE PIN (measured 2026-09-23). The operator asked about the
// branch he was STANDING IN (`git branch --show-current` printed
// fix/memory-escape-decode, checked out in a live worktree holding uncommitted
// work) and `gopi task show --branch` answered "no ledger row names branch …",
// exit 1. Both halves were true — the ledger genuinely had no row for it (a
// hand-made worktree never goes through the dispatch path) and git genuinely
// had the branch — but only one half was stated, and the stated half read like
// "this branch does not exist". The feature's primary question ("I am in a
// worktree; which row is this?") therefore collapsed into a refusal for exactly
// the work the branch→task linkage epic exists to make visible.
//
// WHAT IS PINNED HERE:
//
//  1. a branch the LEDGER does not know but GIT has is reported as such, with
//     the tip and the checkout, and the ledger half is stated SEPARATELY
//     (TestGitProvenanceAnswersForABranchNoRowRecords);
//  2. a branch that is dirty in a live worktree says so — the fact an operator
//     must have before touching anything
//     (TestGitProvenanceReportsUncommittedWork);
//  3. a branch NOBODY has — ledger or git — still says so, and does not invent
//     a presence (TestGitProvenanceDoesNotInventAMissingBranch);
//  4. the worktree selector answers for a directory the ledger does not know
//     (TestGitProvenanceAnswersForAnUnrecordedWorktree);
//  5. the reading is READ-ONLY — no ledger write across a provenance read
//     (TestGitProvenanceIsReadOnly);
//  6. the CHANNEL is labelled on the block's own first line, so a git reading
//     can never be mistaken for a ledger record (devdoc/branch-task-linkage.md
//     §7's "the answer must say which channel answered").

package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitProvenanceGit runs one git command with a deterministic identity and no
// ambient configuration — the repo's standing fixture shape (pushContractGit),
// because every question here is about git's own semantics.
func gitProvenanceGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gitProvenanceRepo builds a real repository with one commit on master and a
// linked worktree carrying its own branch — the exact shape the operator was
// standing in when the defect was reported.
//
// The linked worktree is created through `git worktree add`, so the branch is
// checked out somewhere git can see: the reading under test must resolve the
// checkout from git's own porcelain list, not from the ledger.
func gitProvenanceRepo(t *testing.T) (repo, worktree, branch string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed — the reading shells out to the git binary by design")
	}
	base := t.TempDir()
	repo = filepath.Join(base, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	gitProvenanceGit(t, repo, "init", "-q", "-b", "master")
	gitProvenanceGit(t, repo, "config", "user.email", "t@t")
	gitProvenanceGit(t, repo, "config", "user.name", "t")
	gitProvenanceGit(t, repo, "commit", "-q", "--allow-empty", "-m", "seed")

	worktree = filepath.Join(base, "wt")
	branch = "fix/provenance-unrecorded"
	gitProvenanceGit(t, repo, "worktree", "add", "-q", worktree, "-b", branch)
	return repo, worktree, branch
}

// gitProvenanceNoRowsResolution is the resolution the caller holds when the
// ledger was READ and no row recorded the selector — the shape
// ResolveTaskIDByDeliverable now returns alongside ErrDeliverableNoOwner.
func gitProvenanceNoRowsResolution(rowsWithBranch int) DeliverableResolution {
	return DeliverableResolution{Readable: true, RowsWithBranch: rowsWithBranch}
}

// TestGitProvenanceAnswersForABranchNoRowRecords is the reported defect: the
// ledger has no row, git HAS the branch — the answer must say both, separately.
func TestGitProvenanceAnswersForABranchNoRowRecords(t *testing.T) {
	repo, worktree, branch := gitProvenanceRepo(t)
	tip := gitProvenanceGit(t, repo, "rev-parse", "--short", branch)
	// The repo is reachable because the LEDGER names it for some OTHER branch —
	// the ordinary shape: rows carry the repo, this branch just has no row.
	res := gitProvenanceNoRowsResolution(1019)
	res.Owners = []TaskForBranchOwner{{TaskID: "t-other", Branch: "feat/other", Repo: repo}}

	p := TaskGitProvenanceFor(context.Background(), DeliverableSelector{Branch: branch}, res)
	if !p.RefKnown || !p.RefExists {
		t.Fatalf("git has %s at %s — the reading must report it: %+v", branch, tip, p)
	}
	if p.Repo != repo {
		t.Errorf("repo = %q, want the candidate that carries the branch %q", p.Repo, repo)
	}
	if p.Tip != tip {
		t.Errorf("tip = %q, want %q", p.Tip, tip)
	}
	if p.Checkout != worktree {
		t.Errorf("checkout = %q, want the linked worktree %q", p.Checkout, worktree)
	}

	text := p.Text(DeliverableSelector{Branch: branch}, res)
	// The verdict, the channel label, and the ledger half, each present.
	if !strings.Contains(text, "EXISTS") || !strings.Contains(text, branch) {
		t.Errorf("the answer must state the branch EXISTS and name it:\n%s", text)
	}
	if !strings.HasPrefix(text, TaskGitProvenanceLabel) {
		t.Errorf("the block must open with its channel label %q:\n%s", TaskGitProvenanceLabel, text)
	}
	if !strings.Contains(text, "ledger: the ledger was read: 1019 row(s) carry a branch and none names branch") {
		t.Errorf("the ledger half must be stated separately from the git half:\n%s", text)
	}
}

// TestGitProvenanceReportsUncommittedWork: the fact the operator standing in
// that worktree needs most — the checkout holds work that exists nowhere else.
func TestGitProvenanceReportsUncommittedWork(t *testing.T) {
	repo, worktree, branch := gitProvenanceRepo(t)
	if err := os.WriteFile(filepath.Join(worktree, "work-in-progress.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatalf("write worktree file: %v", err)
	}
	res := gitProvenanceNoRowsResolution(5)
	res.Owners = []TaskForBranchOwner{{TaskID: "t-other", Branch: "feat/other", Repo: repo}}

	p := TaskGitProvenanceFor(context.Background(), DeliverableSelector{Branch: branch}, res)
	if !strings.Contains(p.Dirty, "work-in-progress.go") {
		t.Errorf("dirty = %q, want it to name the uncommitted file", p.Dirty)
	}
	// The dirty half is the whole sentence, not the word "uncommitted" — the
	// CLEAN sentence legitimately contains that word ("has no uncommitted
	// work"), so a probe matching it would pass for the wrong reason.
	if !strings.Contains(p.Text(DeliverableSelector{Branch: branch}, res), "HAS uncommitted work") {
		t.Errorf("the answer must say the checkout holds uncommitted work:\n%s",
			p.Text(DeliverableSelector{Branch: branch}, res))
	}

	// The clean half is the CONTROL: the same repository, the same branch, no
	// untracked file — the dirt sentence must not appear (otherwise the line is
	// noise every operator learns to ignore).
	if err := os.Remove(filepath.Join(worktree, "work-in-progress.go")); err != nil {
		t.Fatalf("remove worktree file: %v", err)
	}
	clean := TaskGitProvenanceFor(context.Background(), DeliverableSelector{Branch: branch}, res)
	if clean.Dirty != "" {
		t.Errorf("dirty = %q after the file was removed, want clean", clean.Dirty)
	}
	if text := clean.Text(DeliverableSelector{Branch: branch}, res); strings.Contains(text, "HAS uncommitted work") {
		t.Errorf("a clean checkout must not be reported as dirty:\n%s", text)
	}
}

// TestGitProvenanceDoesNotInventAMissingBranch: the negative half — a branch
// NEITHER the ledger NOR git has must still be reported as absent. Without this
// the change would "fix" the defect by claiming every branch exists.
func TestGitProvenanceDoesNotInventAMissingBranch(t *testing.T) {
	repo, _, _ := gitProvenanceRepo(t)
	res := gitProvenanceNoRowsResolution(7)
	res.Owners = []TaskForBranchOwner{{TaskID: "t-other", Branch: "feat/other", Repo: repo}}

	missing := "fix/never-existed"
	p := TaskGitProvenanceFor(context.Background(), DeliverableSelector{Branch: missing}, res)
	if !p.RefKnown || p.RefExists {
		t.Fatalf("git has no %s — the reading must report absence, not presence: %+v", missing, p)
	}
	text := p.Text(DeliverableSelector{Branch: missing}, res)
	if !strings.Contains(text, "does NOT exist") {
		t.Errorf("the absent answer must say so by name:\n%s", text)
	}
	if strings.Contains(text, "EXISTS") {
		t.Errorf("an absent branch must not also be reported as present:\n%s", text)
	}
}

// TestGitProvenanceAnswersForAnUnrecordedWorktree: the worktree selector's own
// half — a directory the ledger does not know is still readable git ground.
func TestGitProvenanceAnswersForAnUnrecordedWorktree(t *testing.T) {
	_, worktree, branch := gitProvenanceRepo(t)
	res := gitProvenanceNoRowsResolution(3)

	p := TaskGitProvenanceFor(context.Background(), DeliverableSelector{Worktree: worktree}, res)
	if !p.RepoKnown || !p.RefKnown || !p.RefExists {
		t.Fatalf("the worktree exists and is on %s — the reading must report it: %+v", branch, p)
	}
	text := p.Text(DeliverableSelector{Worktree: worktree}, res)
	if !strings.Contains(text, "is a git work tree") {
		t.Errorf("the worktree answer must state the directory is a work tree:\n%s", text)
	}
	if !strings.Contains(text, branch) {
		t.Errorf("the worktree answer must name the branch checked out there (%s):\n%s", branch, text)
	}

	// A directory that is not a repository at all answers "not a work tree",
	// never a blank block.
	plain := t.TempDir()
	bare := TaskGitProvenanceFor(context.Background(), DeliverableSelector{Worktree: plain}, res)
	if text := bare.Text(DeliverableSelector{Worktree: plain}, res); !strings.Contains(text, "not a readable git work tree") {
		t.Errorf("a non-repository directory must be reported as such:\n%s", text)
	}
}

// TestGitProvenanceIsReadOnly: the reading writes nothing — an untracked file
// that git reports as dirt must still be there, unchanged, afterwards (the
// family's read-only contract: a lookup never mutates what it inspects).
func TestGitProvenanceIsReadOnly(t *testing.T) {
	repo, worktree, branch := gitProvenanceRepo(t)
	wip := filepath.Join(worktree, "keep-me.go")
	if err := os.WriteFile(wip, []byte("package x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	before := gitProvenanceGit(t, repo, "status", "--porcelain")
	res := gitProvenanceNoRowsResolution(1)
	res.Owners = []TaskForBranchOwner{{TaskID: "t-other", Branch: "feat/other", Repo: repo}}

	_ = TaskGitProvenanceFor(context.Background(), DeliverableSelector{Branch: branch}, res)
	_ = TaskGitProvenanceFor(context.Background(), DeliverableSelector{Worktree: worktree}, res)

	if _, err := os.Stat(wip); err != nil {
		t.Errorf("the reading removed the operator's uncommitted file: %v", err)
	}
	if after := gitProvenanceGit(t, repo, "status", "--porcelain"); after != before {
		t.Errorf("the working tree changed across a read-only provenance lookup:\nbefore %q\nafter  %q", before, after)
	}
}

// TestGitProvenanceLedgerUnreadableVerdict: an unreadable ledger is never
// rendered as "no row names it" — the two answers are different operator
// problems (a missing dispatch vs a broken read), so they must not collapse.
func TestGitProvenanceLedgerUnreadableVerdict(t *testing.T) {
	_, _, branch := gitProvenanceRepo(t)
	res := DeliverableResolution{Readable: false} // the ledger could not be read

	text := TaskGitProvenanceFor(context.Background(), DeliverableSelector{Branch: branch}, res).
		Text(DeliverableSelector{Branch: branch}, res)
	if !strings.Contains(text, "UNKNOWN") {
		t.Errorf("an unreadable ledger must be reported as UNKNOWN, not as absence:\n%s", text)
	}
	if strings.Contains(text, "the ledger was read") {
		t.Errorf("an unreadable ledger must not be reported as a successful read:\n%s", text)
	}
}
