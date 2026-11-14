package tools

// The archive-reason record's `repo:` field names the REPOSITORY the ref was
// written into — never the caller's checkout (epic 01a14a8c, measured
// 2026-10-15).
//
// THE DEFECT this file is the red proof for: `gopi task archive` is normally run
// from a LINKED WORKTREE (/home/user/workspace/gopi-<task>), while the ref it
// writes (refs/tags/archive/<branch> + refs/gopi/archive-reason/<branch>) lives
// in the SHARED ref store (<main>/.git/refs/...). The record used to be rendered
// from the caller's path, so it named a transient worktree — a directory the
// finish-cleanup chain reaps at hand-over — instead of the repository that holds
// the decision. Read back later, the record answered "where does this decision
// live?" with a path that no longer existed, and two archives of one branch,
// run from two different worktrees, disagreed in a field that describes the
// REPOSITORY rather than the shell.
//
// Real git in temp repos (the repo_sweep_test.go fixture style): the subject is
// git orchestration, so a mocked git would test the mock — and the whole bug is
// about which of two REAL directories git answers with.

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// archRepoField returns the `repo:` line of a reason record, trimmed — the field
// a reviewer reads to find the repository that holds the ref. It fails the test
// when the record carries no such line: the absence of the field is not a value.
func archRepoField(t *testing.T, record string) string {
	t.Helper()
	for _, line := range strings.Split(record, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if rest, ok := strings.CutPrefix(line, archiveReasonRepoFieldPrefix); ok {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatalf("reason record carries no %q line; got:\n%s", archiveReasonRepoFieldPrefix, record)
	return ""
}

// archTopLevel is a checkout's OWN answer for its work-tree root — the value the
// test compares against, so a symlinked temp root cannot make a correct answer
// look wrong.
func archTopLevel(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(archGit(t, dir, "rev-parse", "--show-toplevel"))
}

// TestArchiveReasonRepoFieldNamesTheSharedRepoFromALinkedWorktree is the
// REGRESSION for the field: the SAME branch archived from (a) the main checkout
// and (b) a linked worktree of it records ONE repo value, and that value is the
// main checkout — the directory whose .git actually holds the ref.
//
// On the pre-fix behaviour the worktree lane records the worktree path, so the
// two readings differ and the first assertion fails with both paths named.
func TestArchiveReasonRepoFieldNamesTheSharedRepoFromALinkedWorktree(t *testing.T) {
	archLedgerFile(t)
	repo := archTestRepo(t, "feat/a", "feat/b")
	mainCheckout := archTopLevel(t, repo)

	// A linked worktree of the SAME repository — the shape every gopi task
	// ground has, and the shape `gopi task archive` is normally invoked from.
	// It is created BESIDE the repo, never inside it: a worktree inside the
	// repo would be picked up by the repo's own status.
	wtParent := t.TempDir()
	wtPath := filepath.Join(wtParent, "gopi-linked")
	if out, err := exec.Command("git", "-C", repo, "worktree", "add", "-q",
		wtPath, "-b", "feat/in-worktree").CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v: %s", err, out)
	}

	// (a) The main checkout: the record names it (unchanged behaviour).
	if _, err := ArchiveTagReasoned(repo, "feat/a", ArchiveReasonSuperseded, "from the main checkout"); err != nil {
		t.Fatalf("ArchiveTagReasoned(main checkout): %v", err)
	}
	fromMain := archRepoField(t, archReasonRecord(t, repo, "feat/a"))
	if fromMain != mainCheckout {
		t.Fatalf("repo: recorded from the main checkout = %q, want %q", fromMain, mainCheckout)
	}

	// (b) The LINKED WORKTREE: the record must STILL name the main checkout —
	// that is where the ref went. Pre-fix this answer was wtPath.
	if _, err := ArchiveTagReasoned(wtPath, "feat/b", ArchiveReasonSuperseded, "from a linked worktree"); err != nil {
		t.Fatalf("ArchiveTagReasoned(linked worktree): %v", err)
	}
	fromWorktree := archRepoField(t, archReasonRecord(t, repo, "feat/b"))
	if fromWorktree == wtPath {
		t.Fatalf("repo: names the CALLER's worktree %q — it must name the repository whose .git holds the ref (%q)",
			wtPath, mainCheckout)
	}
	if fromWorktree != mainCheckout {
		t.Fatalf("repo: from the linked worktree = %q, want the shared repo %q (caller was %q)",
			fromWorktree, mainCheckout, wtPath)
	}

	// (c) The identity property the field exists for: one repository, one value,
	// whatever checkout the operator happened to stand in.
	if fromMain != fromWorktree {
		t.Fatalf("repo: differs by caller: %q (main checkout) vs %q (linked worktree) — "+
			"the field describes the REPOSITORY, not the shell", fromMain, fromWorktree)
	}

	// (d) The ref the record describes really does live in that repository's
	// store: read it back THROUGH the main checkout, where a linked worktree's
	// refs are shared. A record that names a repo it is not in is the defect.
	if got := strings.TrimSpace(archGit(t, mainCheckout, "rev-parse", "--verify",
		archiveReasonRefPrefix+"feat/b")); got == "" {
		t.Fatalf("%sfeat/b does not resolve in %q, the repository the record names",
			archiveReasonRefPrefix, mainCheckout)
	}
}
