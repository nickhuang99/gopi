package devworkspace

// THE CLONE CHANNEL'S TEST FENCE, proved on real git.
//
// The measured residue this pins (2026-10-26, peerbox's own repository):
//
//	$ git log --format='%H|%an <%ae>|%s' feat/exists
//	3043963598210ca6d6edb4075feb389a77de7c5e|t <t@t>|work
//	89ed62db66a125cde90939468c825d4f649aa789|t <t@t>|init
//	$ git merge-base master feat/exists   → no merge base (orphan root)
//	$ git reflog show feat/exists
//	30439635 feat/exists@{0}: push
//
// `t <t@t>` is THIS package's fixture identity (see gitIn), and `push` is this
// package's verb. A fixture's throwaway root reached a real node.
//
// The two arms below are the boundary's contract in both directions: a delivery
// whose recipient is OUTSIDE test space is refused before it runs (and nothing
// moves), and one whose recipient is INSIDE test space still delivers (the
// ordinary case, which is why the fence cannot simply refuse every test).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/pushfence"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// outsideOriginMovesTheRootsAway makes the fixture origin OUTSIDE test space
// without writing outside the suite's temp tree: the origin is created under one
// temp root, then the roots IN FORCE are repointed at a DIFFERENT temp root. The
// origin is now a path no root covers — exactly the shape of a real node's
// checkout as this process measures it.
func outsideOriginMovesTheRootsAway(t *testing.T, origin string) {
	t.Helper()
	elsewhere := t.TempDir()
	t.Setenv(scratch.TmpDirEnvName, elsewhere)
	t.Setenv(scratch.GopiTmpDirEnvName, elsewhere)
	if pushfence.InTestSpace(origin) {
		t.Fatalf("fixture precondition failed: %q still measures as test space", origin)
	}
}

// TestAttemptDeliveryRefusesATestPushToARealNode is THE regression arm. Without
// the fence this call PUSHES the fixture's branch into the origin — which is
// what happened on 2026-10-26.
func TestAttemptDeliveryRefusesATestPushToARealNode(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	// Open must run BEFORE the roots move: the clone itself is not the fenced
	// step, and moving the roots first would make the fixture look broken.
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("deliverable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "work.txt")
	gitIn(t, dir, "commit", "-q", "-m", "deliverable work")

	before := gitIn(t, origin, "rev-parse", DefaultBranch)
	outsideOriginMovesTheRootsAway(t, origin)

	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("a fence refusal is a BLOCKER, not an error: %v", err)
	}
	if blk == nil {
		t.Fatalf("the delivery must be REFUSED, got result %+v", res)
	}
	if blk.Kind != BlockerKindPush {
		t.Fatalf("blocker kind = %q, want %q", blk.Kind, BlockerKindPush)
	}
	if !strings.Contains(blk.Words, pushfence.Marker) {
		t.Fatalf("the refusal must carry the fence's marker %q, got %q", pushfence.Marker, blk.Words)
	}
	if !strings.Contains(blk.Words, origin) {
		t.Fatalf("the refusal must name the target it judged, got %q", blk.Words)
	}
	// THE PROOF THAT NOTHING MOVED: origin's master is still the commit it was,
	// and the deliverable never arrived.
	if after := gitIn(t, origin, "rev-parse", DefaultBranch); after != before {
		t.Fatalf("origin moved from %s to %s — the fence ran too late", before, after)
	}
	if log := gitIn(t, origin, "log", "--oneline", DefaultBranch); strings.Contains(log, "deliverable work") {
		t.Fatalf("the deliverable reached origin despite the fence:\n%s", log)
	}
}

// TestAttemptDeliveryStillDeliversInsideTestSpace is the anti-over-refusal arm:
// the ordinary fixture shape (origin under the roots in force) must deliver.
// Without it the fence could be "fixed" by refusing everything.
func TestAttemptDeliveryStillDeliversInsideTestSpace(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("deliverable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "work.txt")
	gitIn(t, dir, "commit", "-q", "-m", "deliverable work")

	if !pushfence.InTestSpace(origin) {
		t.Fatalf("fixture precondition failed: %q must measure as test space", origin)
	}
	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("AttemptDelivery: %v", err)
	}
	if blk != nil {
		t.Fatalf("a delivery inside test space must not be refused, got %q: %s", blk.Kind, blk.Report())
	}
	if !res.Pushed {
		t.Fatalf("the delivery must land, got %+v", res)
	}
	if log := gitIn(t, origin, "log", "--oneline", DefaultBranch); !strings.Contains(log, "deliverable work") {
		t.Fatalf("origin is missing the deliverable:\n%s", log)
	}
}

// TestPushFenceRefusalNamesAnUnreadableRecipient: with no origin URL there is
// nothing to judge, so the fence stays out of the way and the push itself
// reports the git-level failure (a fence that withheld a real delivery because
// it could not take a reading would be its own class of silent failure).
func TestPushFenceRefusalNamesAnUnreadableRecipient(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if why := pushFenceRefusal(ctx, dir); why != "" {
		t.Fatalf("pushFenceRefusal with no origin = %q, want \"\"", why)
	}
}
