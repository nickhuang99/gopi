package tools

// THE TEST FENCE AT THE PUSH ITSELF (internal/pushfence, asked from
// gitPushbackPush).
//
// Why an arm HERE and not only in the plan: gitPushbackPlan asks the fence once
// per pass and skips the whole flush, but the plan is NOT the only caller of
// gitPushbackPush — the delivery LOOP (task_delivery_loop.go) and
// memory_sync_task.go's harvest reach the push directly. A fence that lived only
// in the plan left those legs uncovered, which is exactly the shape of the
// measured residue: a boundary that could push while every OTHER boundary was
// fenced.
//
// The measured residue (2026-10-26, peer1's own repository):
//
//	$ git log --format='%H|%an <%ae>|%s' feat/exists
//	3043963598210ca6d6edb4075feb389a77de7c5e|t <t@t>|work
//	89ed62db66a125cde90939468c825d4f649aa789|t <t@t>|init
//	$ git merge-base master feat/exists   → no merge base (orphan root)
//	$ git reflog show feat/exists
//	30439635 feat/exists@{0}: push
//
// `t <t@t>` is this package's fixture identity (see newPBWorld).

import (
	"context"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/pushfence"
	"github.com/nickhuang99/gopi/internal/scratch"
)

const (
	// pushFenceTestBranch is the deliverable branch these two arms push. It is
	// featur-shaped like the residue, so a reader can line the fixture up with
	// refs/heads/feat/exists without translating names.
	pushFenceTestBranch = "feat/pushfence-arm"

	// pushFenceTestFile is the one file the deliverable commit carries.
	pushFenceTestFile = "work.txt"
)

// pushFenceCommit pushes one commit of the named branch from the world's client.
func pushFenceCommit(t *testing.T, w *pbWorld, branch string) {
	t.Helper()
	w.git(w.client, "checkout", "-q", "-b", branch)
	pbWrite(t, w.client, pushFenceTestFile, "deliverable\n")
	w.git(w.client, "add", pushFenceTestFile)
	w.git(w.client, "commit", "-q", "-m", "deliverable work")
}

// TestGitPushbackPushAsksTheTestFence: with a recipient OUTSIDE every root in
// force, the push is refused BEFORE git runs — and the refusal is the fence's
// own sentence, which is how this arm distinguishes "the fence stopped it" from
// "the push failed for some other reason".
func TestGitPushbackPushAsksTheTestFence(t *testing.T) {
	w := newPBWorld(t)
	branch := pushFenceTestBranch
	pushFenceCommit(t, w, branch)

	// The world's server lives under the suite's temp root. Repointing the roots
	// at a DIFFERENT temp directory makes the server measure as outside test
	// space WITHOUT writing anything outside the suite's own tree.
	elsewhere := t.TempDir()
	t.Setenv(scratch.TmpDirEnvName, elsewhere)
	t.Setenv(scratch.GopiTmpDirEnvName, elsewhere)
	if pushfence.InTestSpace(w.server) {
		t.Fatalf("fixture precondition failed: %q still measures as test space", w.server)
	}

	msg, ok := gitPushbackPush(context.Background(), w.client, w.server, branch)
	if ok {
		t.Fatal("gitPushbackPush reported success into a target outside test space")
	}
	if !strings.Contains(msg, pushfence.Marker) {
		t.Fatalf("the refusal must be the fence's own sentence (marker %q), got %q", pushfence.Marker, msg)
	}
	if !strings.Contains(msg, w.server) {
		t.Fatalf("the refusal must name the target it judged, got %q", msg)
	}
	// NOTHING MOVED: the server never received the branch.
	if w.serverRefExists(branch) {
		t.Fatalf("the server carries %q — the fence ran too late", branch)
	}
}

// TestGitPushbackPushStillDeliversInsideTestSpace is the anti-over-refusal arm:
// the ordinary fixture shape must keep delivering through the SAME call.
func TestGitPushbackPushStillDeliversInsideTestSpace(t *testing.T) {
	w := newPBWorld(t)
	branch := pushFenceTestBranch
	pushFenceCommit(t, w, branch)
	if !pushfence.InTestSpace(w.server) {
		t.Fatalf("fixture precondition failed: %q must measure as test space", w.server)
	}
	if msg, ok := gitPushbackPush(context.Background(), w.client, w.server, branch); !ok {
		t.Fatalf("the push inside test space must land, got %q", msg)
	}
	if !w.serverRefExists(branch) {
		t.Fatalf("the server does not carry %q after a delivered push", branch)
	}
}
