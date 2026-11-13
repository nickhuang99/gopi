package standardgate

// The PUBLICATION fixture — an object this repository HOLDS and no ref reaches.
//
// WHY A REAL OBJECT IS REQUIRED, and why a 40-zero hash cannot stand in for it.
// The census checks now make TWO independent measurements about a cited revision:
// existence (`git cat-file -e`) and publication (`git for-each-ref --contains`).
// A hash that names nothing fails the FIRST probe, so an arm built on one cannot
// tell a working publication probe from a missing one — the red it produces is
// the rebase shape's red, which the generator already had before this fix. The
// defect being repaired lives exactly in the gap between the two: an object the
// store still holds (so `cat-file` says yes, and `--print --at REV` genuinely
// reproduces the rows) that NO REF reaches (so no clone owes it anything and
// `git prune` collects it). Only a real, unreferenced commit exercises that gap.
//
// WHAT THIS FIXTURE IS, precisely: `git commit-tree` of the EMPTY tree with no
// parent, run in the workspace under test. It creates one commit object (plus the
// well-known empty tree, which most repositories already hold) and NO ref — not a
// branch, not a tag, not a remote-tracking ref. It cannot be pushed, cannot be
// merged, cannot be checked out, and nothing in the repository can reach it.
//
// WHAT HAPPENS TO IT AFTERWARDS: it is garbage, and `git prune` NAMES it — that is
// the fixture's own falsification (`gitPublicationProbeNamesIt` below asserts the
// object appears in `git prune --dry-run`, i.e. that git itself agrees nothing
// reaches it). Any ordinary `git gc` in the clone collects it and leaves no trace;
// a fixture that leaked into a ref would make the arm it serves vacuous, so the
// helper refuses to return a revision that a ref DOES reach.
//
// It is NOT a second worktree, NOT a branch and NOT a file in the tree (Rules 6a,
// 29, 30): the arm's working tree is untouched, and the repository is left exactly
// as it was found apart from one collectable object.

import (
	"os/exec"
	"strings"
	"testing"
)

// publicationProbeRev creates an object this repository holds and no ref reaches,
// and proves both halves by MEASUREMENT before returning it (Rule 5: a fixture
// whose shape is assumed is a fixture that can pass an arm for the wrong reason).
func publicationProbeRev(t *testing.T, root string) string {
	t.Helper()
	// The empty tree is git's own well-known object; asking for its id rather than
	// spelling forty hex digits keeps this file free of an object id (Rule 1).
	emptyTree := strings.TrimSpace(publicationGitOut(t, root, "hash-object", "-t", "tree", "/dev/null"))
	rev := strings.TrimSpace(publicationGitOutEnv(t, root, "commit-tree", emptyTree, "-m",
		"publication probe: an object every ref was rewritten past (standards package fixture)"))

	if _, err := exec.Command(gitBinary, "-C", root, "cat-file", "-e", rev).Output(); err != nil {
		t.Fatalf("the fixture %s is NOT an object in %s (%v) — the arm needs an object the store HOLDS",
			rev, root, err)
	}
	if refs := publicationRefs(t, root, rev); refs != "" {
		t.Fatalf("the fixture %s is reachable from %q — a PUBLISHED commit cannot stand in for an "+
			"unreferenced one, and an arm built on it would pass without the fix", rev, refs)
	}
	if !publicationGitPruneNamesIt(t, root, rev) {
		t.Logf("note: `git prune --dry-run` did not name %s (git may be keeping it for another reason); "+
			"the ref probe above is the arm's subject, so the run continues", rev)
	}
	return rev
}

// publicationRefs returns the refs whose history contains rev, as one string —
// empty when NO ref reaches it. It is the same question the generators ask, spelled
// independently here so the fixture and the check cannot share a single bug.
func publicationRefs(t *testing.T, root, rev string) string {
	t.Helper()
	return strings.TrimSpace(publicationGitOut(t, root, "for-each-ref", "--contains", rev,
		"--format=%(refname)"))
}

// publicationGitPruneNamesIt reports whether git itself considers rev collectable,
// i.e. that `git prune --dry-run` lists it. It is the fixture's falsification: an
// object no REF reaches but that git still protects (a reflog entry, an index
// cache) is not the shape this arm claims to build.
func publicationGitPruneNamesIt(t *testing.T, root, rev string) bool {
	t.Helper()
	out, err := exec.Command(gitBinary, "-C", root, "prune", "--dry-run").Output()
	if err != nil {
		t.Logf("`git prune --dry-run` failed (%v) — the reachability question is answered by the ref "+
			"probe alone", err)
		return false
	}
	return strings.Contains(string(out), rev)
}

// publicationGitOut runs git in root and fails the test on a non-zero status — an
// arm that silently read "" would assert against nothing.
func publicationGitOut(t *testing.T, root string, argv ...string) string {
	t.Helper()
	out, err := exec.Command(gitBinary, append([]string{"-C", root}, argv...)...).Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(argv, " "), err)
	}
	return string(out)
}

// publicationGitOutEnv is publicationGitOut for the object-WRITING commands
// (`commit-tree` needs an author and a committer, and the harness may not have one
// configured): the identity is supplied explicitly so the fixture never depends on
// the operator's git config, and never writes to it.
func publicationGitOutEnv(t *testing.T, root string, argv ...string) string {
	t.Helper()
	cmd := exec.Command(gitBinary, append([]string{"-C", root}, argv...)...)
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME=census fixture",
		"GIT_AUTHOR_EMAIL=census-fixture@example.invalid",
		"GIT_COMMITTER_NAME=census fixture",
		"GIT_COMMITTER_EMAIL=census-fixture@example.invalid",
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s (with a supplied identity): %v", strings.Join(argv, " "), err)
	}
	return string(out)
}
