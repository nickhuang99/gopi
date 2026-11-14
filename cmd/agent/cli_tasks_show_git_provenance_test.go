// The `gopi task show --branch/--worktree` face when the LEDGER HAS NO ROW —
// the reported defect, pinned at the verb the operator actually types.
//
// MEASURED 2026-09-23 (the operator's own words, task 01a0cc9b): he ran
//
//	$ gopi task show --branch fix/memory-escape-decode
//	gopi task show error: task: no ledger row names branch "fix/memory-escape-decode" …
//	exit 1
//
// on the branch he was STANDING IN — `git branch --show-current` printed it,
// the linked worktree held uncommitted work, and the ledger genuinely had no
// row for it (0 of 1019 branch-carrying rows; a worktree made by hand never
// goes through the dispatch path). The refusal was the whole answer, so the
// verb's primary question ("I am in a worktree; which row is this?") came back
// as something that reads like "this branch does not exist".
//
// WHAT THIS FILE PINS (the acceptance the operator can re-run by hand):
//
//  1. the ledger fact and the git fact are BOTH stated, each LABELLED, and the
//     git half names the tip, the checkout and its uncommitted work
//     (TestTaskShowBranchNoRowAnswersWithLedgerAndGitFacts);
//  2. the refusal is not weakened: the error still comes back (the exit code
//     stays non-zero — a selector that resolved no row is not a success), and
//     an empty result is impossible, so the operator never gets a silent
//     stdout
//     (the same test, plus TestTaskShowUnknownSelectorStillFailsNonZero);
//  3. the ROW-EXISTS path is untouched: a selector that resolves renders the
//     digest and NOT the provenance block, so the ordinary answer does not grow
//     a second voice (TestTaskShowResolvedSelectorKeepsThePlainDigest).

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// showBranchGitProvenanceRepo prepares a real repository with one commit, its
// worktree sandbox settings, and a linked worktree on its own branch — the
// ground the reported invocation was standing in.
//
// GOPI_SETTINGS is pinned at a sandbox file that DECLARES the repository (the
// Rule-12 repos table), so the reading resolves the repo from configuration
// rather than from wherever `go test` happened to be started.
func showBranchGitProvenanceRepo(t *testing.T, branch string) (repo, worktree string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed — the show face shells out to git by design")
	}
	base := t.TempDir()
	repo = filepath.Join(base, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	run := func(dir string, args ...string) string {
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
	run(repo, "init", "-q", "-b", "master")
	run(repo, "config", "user.email", "t@t")
	run(repo, "config", "user.name", "t")
	run(repo, "commit", "-q", "--allow-empty", "-m", "seed")
	worktree = filepath.Join(base, "wt")
	run(repo, "worktree", "add", "-q", worktree, "-b", branch)

	settings := filepath.Join(base, "settings.json")
	body := `{"repos":{"` + repo + `":{"build":""}}}`
	if err := os.WriteFile(settings, []byte(body), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv("GOPI_SETTINGS", settings)
	return repo, worktree
}

// TestTaskShowBranchNoRowAnswersWithLedgerAndGitFacts is the reported
// invocation, end to end through the verb's own entry point.
func TestTaskShowBranchNoRowAnswersWithLedgerAndGitFacts(t *testing.T) {
	branch := "fix/memory-escape-decode"
	repo, worktree := showBranchGitProvenanceRepo(t, branch)
	// The operator's own shape: uncommitted work in that worktree. It is
	// UNTRACKED, exactly like the reported one (memory_escape.go was untracked).
	wip := filepath.Join(worktree, "memory_escape.go")
	if err := os.WriteFile(wip, []byte("package x\n"), 0o644); err != nil {
		t.Fatalf("write wip: %v", err)
	}
	// A ledger the read succeeds against — the shape the operator had: rows
	// exist, none names his branch.
	dbPath := testutil.NewTestDBPath(t)
	showSelectorSeed(t, dbPath, "t-other-row", tools.TaskStatusCompleted, repo, "feat/something-else", "")
	tip := strings.TrimSpace(mustGitOut(t, repo, "rev-parse", "--short", branch))

	out, err := runTaskShow(&tasksOptions{selBranch: branch, db: dbPath})
	if err == nil {
		t.Fatal("a selector no row records must still fail (the exit code stays non-zero)")
	}
	if !errors.Is(err, tools.ErrDeliverableNoOwner) {
		t.Errorf("the failure must be the resolver's no-owner sentinel: %v", err)
	}
	if !strings.Contains(err.Error(), branch) {
		t.Errorf("the refusal must name the branch it could not resolve: %v", err)
	}
	if out == "" {
		t.Fatal("an empty stdout would hide the whole answer — the provenance block must be returned")
	}

	for _, want := range []string{
		tools.TaskGitProvenanceLabel, // the channel is labelled
		branch,                       // the branch is named
		"EXISTS",                     // the git verdict
		tip,                          // its tip
		worktree,                     // where it is checked out
		"memory_escape.go",           // the uncommitted work
		"the ledger was read",        // the ledger half, separately stated
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the answer must contain %q:\n%s", want, out)
		}
	}
	// The ROW half must not appear: nothing was resolved.
	if strings.Contains(out, "task_id:") {
		t.Errorf("no row was resolved, so no digest may be rendered:\n%s", out)
	}
}

// TestTaskShowUnknownSelectorStillFailsNonZero: the negative half — a branch
// neither the ledger NOR git has must still be refused, and the answer must not
// claim a presence. This is what keeps the change from becoming "always say
// yes".
func TestTaskShowUnknownSelectorStillFailsNonZero(t *testing.T) {
	_, _ = showBranchGitProvenanceRepo(t, "fix/only-this-one-exists")
	dbPath := testutil.NewTestDBPath(t)

	out, err := runTaskShow(&tasksOptions{selBranch: "fix/definitely-not-a-branch-anywhere", db: dbPath})
	if err == nil {
		t.Fatal("an unknown branch must still fail the show")
	}
	if !strings.Contains(out, "does NOT exist") {
		t.Errorf("the answer must state the absence:\n%s", out)
	}
	if strings.Contains(out, "EXISTS") {
		t.Errorf("the answer must not claim a branch that does not exist:\n%s", out)
	}
}

// TestTaskShowResolvedSelectorKeepsThePlainDigest: the row-exists path is
// untouched — the digest, and no provenance block.
func TestTaskShowResolvedSelectorKeepsThePlainDigest(t *testing.T) {
	dbPath := testutil.NewTestDBPath(t)
	branch := "feat/provenance-resolved"
	showSelectorSeed(t, dbPath, "t-resolved", tools.TaskStatusInProgress, "/repo/here", branch, "/wt/here")

	out, err := runTaskShow(&tasksOptions{selBranch: branch, db: dbPath})
	if err != nil {
		t.Fatalf("show by a recorded branch: %v", err)
	}
	if !strings.Contains(out, "t-resolved") {
		t.Errorf("the digest must name the resolved row:\n%s", out)
	}
	if strings.Contains(out, tools.TaskGitProvenanceLabel) {
		t.Errorf("a resolved selector must not render the no-row provenance block:\n%s", out)
	}
}

// mustGitOut is the fixture's read-only git helper (a failure is a fixture
// bug, so it reports rather than skips).
func mustGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v in %s: %v", args, dir, err)
	}
	return string(out)
}
