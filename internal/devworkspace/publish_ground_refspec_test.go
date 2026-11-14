package devworkspace

// publish_ground_refspec_test.go — THE DESTINATION THE DRIVER DOCUMENTS, MEASURED.
//
// THE DEFECT THIS GUARDS (removed 2026-10-28, the doc/code-did-not-match class):
// `publishGround` was documented as pushing "TWO destinations in ONE push" — the
// integration branch AND the row's own `task/<id>` — and carried a whole
// paragraph arguing why the second refspec needed `+` (force) semantics. Since
// `9ab17237e` the implementation has built ONE refspec, and the `ground`
// parameter it still accepted was dead: nothing it could be set to changed a
// byte of the command. A comment that disagrees with the command is not a stale
// comment, it is a false statement a reader has no instrument to refute: the
// ledger records `deliverable.branch = task/<id>`, and a reader of that comment
// concluded the ref exists on the server, which it never does.
//
// WHAT THIS PINS, and it is the CHEAP HALF OF THE PAIR. `publishGround` builds
// its argv INSIDE the function, so the argv is not readable from a test. The
// question is therefore answered twice, once from each side:
//
//  1. HERE — the COMMAND’S EFFECT: a delivery on a real (fixture) origin leaves
//     master advanced and creates NO `task/<id>` ref there. This is what the
//     comment claims, measured.
//  2. devworkspace_source_refspec_test.go — the SOURCE: `publishGround` takes no
//     `ground` parameter and its body names exactly ONE refspec, so a second
//     destination cannot be reintroduced without the gate going red.
//
// Either half alone is defeatable. The effect test alone passes for a command
// that pushes a second ref equal to an existing one; the source test alone
// passes for a function that never runs. Together they say: the driver pushes
// one destination, and that destination is what the prose above it names.
//
// The address is deliberately NOT pushed, and the reason is measured rather than
// aesthetic: after a `pull --rebase` the row's own commits are REWRITTEN, so a
// round-2 push of `HEAD:refs/heads/task/<id>` is a non-fast-forward refusal, and
// forcing it (the `+` the removed paragraph argued for) leaves one `task/*` ref
// per task on the server forever — peer1 carried 15 of them, every one already
// merged and none ever deleted (9ab17237e).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// publishGroundTaskID is a row id in the ledger's own spelling (Rule 1: the ref
// the test asserts must NOT exist is derived from it, never typed twice).
const publishGroundTaskID = "01a141a7-0000-7000-8000-00000000f1e1"

// TestPublishGroundPushesExactlyOneDestination is the effect half: the delivery
// moves the integration branch and creates nothing else on the server.
func TestPublishGroundPushesExactlyOneDestination(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	cfg.TaskID = publishGroundTaskID
	cfg.GroundBranch = "task/" + publishGroundTaskID
	cfg.BaseBranch = DefaultBranch

	res, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := os.WriteFile(filepath.Join(res.Dir, "work.txt"), []byte("deliverable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, res.Dir, "add", "work.txt")
	gitIn(t, res.Dir, "commit", "-q", "-m", "the row's work")
	localTip := strings.TrimSpace(gitIn(t, res.Dir, "rev-parse", gitHeadRef))

	dres, blk, err := AttemptDelivery(context.Background(), cfg, res.Dir)
	if err != nil {
		t.Fatalf("AttemptDelivery: %v", err)
	}
	if blk != nil {
		t.Fatalf("delivery blocked (%s): %s", blk.Kind, blk.Words)
	}
	if !dres.Pushed {
		t.Fatalf("delivery did not push: %+v", dres)
	}

	// ① THE ONE DESTINATION: the integration branch carries the run's commit.
	if remoteTip := strings.TrimSpace(gitIn(t, origin, "rev-parse", refsHeadsPrefix+DefaultBranch)); remoteTip != localTip {
		t.Fatalf("origin's %s = %s, want the run's commit %s — the delivery's one documented destination did not move",
			DefaultBranch, remoteTip, localTip)
	}
	// ② THE DESTINATION THE COMMENT MUST NOT CLAIM: asking the server for the
	// row's own address, by the question a reader of that comment would ask.
	out, err := exec.Command("git", "-C", origin, "rev-parse", "--verify", "--quiet", refsHeadsPrefix+cfg.GroundBranch).Output()
	if err == nil {
		t.Fatalf("origin carries %s at %q. publishGround pushes ONE destination (%s); a second one is a ref no merge "+
			"path consumes and it piles up one dead branch per task forever (9ab17237e). If a second destination is "+
			"ever genuinely wanted, the prose above publishGround and this test must change in the same commit.",
			cfg.GroundBranch, strings.TrimSpace(string(out)), DefaultBranch)
	}
}
