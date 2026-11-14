package tools

// task_ground_shape_clause_test.go — the announcement's SHAPE SENTENCE must be
// rendered from a MEASUREMENT of the ground, never from the channel asking.
//
// THE MEASURED DEFECT THIS PINS (2026-10-27, row 01a156d8-c7bf-739a-9308-f8f7e74513a4-r1,
// which is this epic's own run): the run's ground was a CLONE —
//
//	$ test -d .git && echo DIRECTORY            → DIRECTORY
//	$ git rev-parse --git-common-dir            → .git
//	$ git remote -v                             → origin peerbox:/home/user/work/gopi
//
// — and its OWN briefing said, in one and the same message, both
//
//	it is a linked worktree of your task's repo, checked out on branch master.
//	This ground is a CLONE standing on branch master.
//
// Neither sentence was a lie about the code that printed it; the shape sentence
// was simply a typed literal rendered unconditionally, while the publishing
// clause was rendered `if cloneRun != nil`. The delivery consequence differs by
// shape (a clone's commit is OWED by the channel; a linked worktree's is already
// in the shared repo), so a wrong shape sentence tells a run its undelivered
// commit is safe — the exact failure the epic exists to remove.
//
// All three arms are built on REAL git (t.TempDir repositories, no fake): a
// fixture that never had a `.git` directory would pin a statement about a shape
// the code had not measured.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/devworkspace"
)

// groundShapeFixture builds the TWO real shapes in one throwaway tree: a
// repository, a LINKED worktree of it, and a CLONE of it. It returns
// (cloneDir, linkedDir).
func groundShapeFixture(t *testing.T) (cloneDir, linkedDir string) {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "main")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	groundTestGitT(t, repo, "init", "-q", "-b", devworkspace.DefaultBranch)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	groundTestGitT(t, repo, "add", "README.md")
	groundTestGitT(t, repo, "commit", "-q", "-m", "initial")

	linkedDir = filepath.Join(root, "wt-linked")
	groundTestGitT(t, repo, "worktree", "add", "-q", linkedDir, "-b", "task/fixture-linked")

	cloneDir = filepath.Join(root, "clone-ground")
	groundTestGitT(t, root, "clone", "-q", repo, cloneDir)
	return cloneDir, linkedDir
}

// TestGroundShapeSentenceIsRenderedFromTheMeasuredShape is the headline: the two
// shapes make OPPOSITE delivery claims, and each claim is attached to the shape
// that makes it true.
func TestGroundShapeSentenceIsRenderedFromTheMeasuredShape(t *testing.T) {
	cloneDir, linkedDir := groundShapeFixture(t)

	// The fixtures ARE the shapes they are named for — asserted through the one
	// discriminator, so a fixture that silently became the other shape fails here
	// rather than passing the arm below for the wrong reason.
	if got := devworkspace.GroundModelOf(cloneDir); got != devworkspace.GroundModelClone {
		t.Fatalf("the clone fixture must MEASURE as a clone; got %q", got)
	}
	if got := devworkspace.GroundModelOf(linkedDir); got != devworkspace.GroundModelLinkedWorktree {
		t.Fatalf("the linked fixture must MEASURE as a linked worktree; got %q", got)
	}

	t.Run("clone ground: the commit is NOT already on the server", func(t *testing.T) {
		got := GroundShapeSentence(cloneDir, "master")
		if !strings.Contains(got, "it is a CLONE") {
			t.Errorf("a clone ground must be announced as a CLONE:\n%s", got)
		}
		// THE DEFECT, as an assertion: the false sentence must not appear.
		if strings.Contains(got, "NO push is needed") {
			t.Errorf("a CLONE ground must NOT be told \"NO push is needed\" — its commit lives only in this directory until the channel publishes it:\n%s", got)
		}
		if strings.Contains(got, "it is a LINKED WORKTREE") {
			t.Errorf("a CLONE ground must not be described as a linked worktree (the measured 2026-10-27 sentence):\n%s", got)
		}
		// The affirmative half: the reader is told who owes the publish.
		if !strings.Contains(got, "DELIVERY LOOP") {
			t.Errorf("a clone ground's sentence must name the channel's DELIVERY LOOP as the publisher:\n%s", got)
		}
		if !strings.Contains(got, "NOT already on the server") {
			t.Errorf("a clone ground's sentence must say its commit is NOT already on the server:\n%s", got)
		}
	})

	t.Run("linked worktree ground: the commit IS already shared", func(t *testing.T) {
		got := GroundShapeSentence(linkedDir, "task/fixture-linked")
		if !strings.Contains(got, "LINKED WORKTREE") {
			t.Errorf("a linked worktree ground must be announced as a LINKED WORKTREE:\n%s", got)
		}
		if !strings.Contains(got, "NO push is needed") {
			t.Errorf("a linked worktree ground MUST still be told no push is needed — its commit is already on its branch in the shared repo:\n%s", got)
		}
		if strings.Contains(got, "it is a CLONE") {
			t.Errorf("a linked worktree ground must not be described as a clone:\n%s", got)
		}
	})

	t.Run("unmeasurable ground: neither claim is made", func(t *testing.T) {
		got := GroundShapeSentence(filepath.Join(t.TempDir(), "nothing-here"), "master")
		if !strings.Contains(got, "SHAPE COULD NOT BE MEASURED") {
			t.Errorf("a directory with no git entry has NO shape, and the sentence must say so rather than pick one:\n%s", got)
		}
		for _, forbidden := range []string{"NO push is needed", "it is a CLONE", "LINKED WORKTREE"} {
			if strings.Contains(got, forbidden) {
				t.Errorf("an unmeasurable ground must make NEITHER delivery claim (%q appeared):\n%s", forbidden, got)
			}
		}
		if !strings.Contains(got, string(devworkspace.GroundModelMissing)) {
			t.Errorf("the sentence must name the measured reason from the constant's own spelling (%q):\n%s",
				devworkspace.GroundModelMissing, got)
		}
	})
}

// TestGroundAnnouncementClauseCarriesTheMeasuredShapeSentence pins the WIRING:
// the announcement the spawn seam binds into a briefing must carry the sentence
// the shape produced, not a template of its own.
func TestGroundAnnouncementClauseCarriesTheMeasuredShapeSentence(t *testing.T) {
	cloneDir, linkedDir := groundShapeFixture(t)

	cloneClause := TaskGroundAnnouncementClause(cloneDir, "master")
	if !strings.Contains(cloneClause, TaskGroundAnnouncementLabel) {
		t.Errorf("the clause must carry its label %q:\n%s", TaskGroundAnnouncementLabel, cloneClause)
	}
	if !strings.Contains(cloneClause, GroundShapeSentence(cloneDir, "master")) {
		t.Errorf("the announcement must carry the MEASURED shape sentence for a clone ground:\n%s", cloneClause)
	}
	if strings.Contains(cloneClause, "NO push is needed") {
		t.Errorf("the announcement for a CLONE ground must not carry the linked-worktree claim:\n%s", cloneClause)
	}

	linkedClause := TaskGroundAnnouncementClause(linkedDir, "task/fixture-linked")
	if !strings.Contains(linkedClause, "NO push is needed") {
		t.Errorf("the announcement for a LINKED worktree ground must carry the no-push claim:\n%s", linkedClause)
	}

	// The label is shape-neutral: it is the first thing the model reads, and it
	// must not tell a clone ground it is in a worktree.
	if strings.Contains(TaskGroundAnnouncementLabel, "worktree") {
		t.Errorf("the announcement label %q must stay shape-neutral — a clone ground is not a worktree", TaskGroundAnnouncementLabel)
	}
}

// TestGroundShapeDiscriminatorHasOneAnswer pins that the discriminator the
// ANNOUNCEMENT uses and the one the DELIVERY PROOF uses are the same answer:
// `deliveryProofIsCloneGround` (which decides whether a row's push is owed) must
// agree with `devworkspace.GroundModelOf` on both shapes. Two readers that can
// disagree are how a sentence about one shape gets applied to the other.
func TestGroundShapeDiscriminatorHasOneAnswer(t *testing.T) {
	cloneDir, linkedDir := groundShapeFixture(t)

	if !deliveryProofIsCloneGround(cloneDir) {
		t.Errorf("deliveryProofIsCloneGround(%q) must be true for a measured clone", cloneDir)
	}
	if deliveryProofIsCloneGround(linkedDir) {
		t.Errorf("deliveryProofIsCloneGround(%q) must be false for a measured linked worktree", linkedDir)
	}
	for _, dir := range []string{"", "   ", filepath.Join(t.TempDir(), "missing")} {
		if deliveryProofIsCloneGround(dir) {
			t.Errorf("deliveryProofIsCloneGround(%q) must be false — an absent ground is not a clone", dir)
		}
		if got := devworkspace.GroundModelOf(dir); got == devworkspace.GroundModelClone {
			t.Errorf("GroundModelOf(%q) must not report a clone for an absent ground; got %q", dir, got)
		}
	}
}
