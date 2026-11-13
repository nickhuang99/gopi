package tools

// dpuoFixture — the INCIDENT'S OWN SHAPE (row 01a183fa), built once and shared by
// the two regression arms and by the red proof that runs this fixture against the
// PRE-FIX tree. That is why this file references ONLY symbols that existed before
// the repair: a fixture that used the repair's own vocabulary could not be
// compiled into the tree the red proof measures (memory #14229's rule for red
// proofs on a clone-channel ground).

import (
	"os"
	"path/filepath"
	"testing"
)

// dpuoFixture builds the incident's shape and returns the recorded repo, the
// ground (which does NOT hold the run's tip), the delivery point's directory, and
// the tip itself.
func dpuoFixture(t *testing.T) (repo, ground, bare, tip string) {
	t.Helper()
	root := t.TempDir()
	bare = filepath.Join(root, "server")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, bare, "init", "-q", "--bare", "-b", masterBranchName)

	seed := filepath.Join(root, "seed")
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, seed, "init", "-q", "-b", masterBranchName)
	rebuildTestGit(t, seed, "config", "user.email", "dpuo@test")
	rebuildTestGit(t, seed, "config", "user.name", "dpuo")
	if err := os.WriteFile(filepath.Join(seed, "f.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, seed, "add", "f.txt")
	rebuildTestGit(t, seed, "commit", "-q", "-m", "base")
	rebuildTestGit(t, seed, "push", "-q", bare, masterBranchName)

	// THE GROUND AND THE RECORDED REPO ARE CLONED *BEFORE* THE PUSH, so neither
	// ever saw the run's commit — the incident's measured condition, produced
	// without touching an object store by hand.
	ground = filepath.Join(root, "ground")
	rebuildTestGit(t, root, "clone", "-q", bare, ground)
	repo = filepath.Join(root, "recorded")
	rebuildTestGit(t, root, "clone", "-q", bare, repo)

	// THE RUN'S WORK IS MADE AND DELIVERED FROM SOMEWHERE ELSE, which is what the
	// clone channel does (it pushes HEAD:refs/heads/master from its own ground
	// state). Only the delivery point learns about the commit.
	maker := filepath.Join(root, "maker")
	rebuildTestGit(t, root, "clone", "-q", bare, maker)
	rebuildTestGit(t, maker, "config", "user.email", "dpuo@test")
	rebuildTestGit(t, maker, "config", "user.name", "dpuo")
	if err := os.WriteFile(filepath.Join(maker, "work.txt"), []byte("the run's own work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, maker, "add", "work.txt")
	rebuildTestGit(t, maker, "commit", "-q", "-m", "the work this run produced")
	tip = rebuildTestGit(t, maker, "rev-parse", "HEAD")
	rebuildTestGit(t, maker, "push", "-q", bare, "HEAD:refs/heads/"+masterBranchName)

	// THE PRECONDITION — and it is the incident row's OWN shape, which is why it is
	// the directory that goes rather than the object that is doctored. The arm
	// names the tip from the row's landed record (the sentence the incident printed
	// said so: "the tip eaaf6b43ab0e (the clone channel's own landed record of its
	// push)"), so the run's own ground has to be GONE for that witness to be the
	// one used; and the recorded repo is a clone taken before the push, so it never
	// fetched the commit. Nobody on this host can read it.
	if _, err := branchDeleteGit(t.Context(), repo, "cat-file", "-e", tip+gitPeerCommitSuffix); err == nil {
		t.Fatalf("fixture is wrong: the recorded repo can already read the tip %s, so nothing is unreadable here",
			branchDeleteShortHash(tip))
	}
	// And the delivery point really holds it: this is the self-contradiction the
	// incident sentence carried — `origin/master = <the very tip>`.
	if got := rebuildTestGit(t, bare, "rev-parse", refsHeadsPrefix+masterBranchName); got != tip {
		t.Fatalf("fixture is wrong: the delivery point's %s is %s, want the run's tip %s",
			masterBranchName, branchDeleteShortHash(got), branchDeleteShortHash(tip))
	}
	if err := os.RemoveAll(ground); err != nil {
		t.Fatalf("the fixture must be able to reclaim the ground: %v", err)
	}
	return repo, ground, bare, tip
}
