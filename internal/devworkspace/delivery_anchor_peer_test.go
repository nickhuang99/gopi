package devworkspace

// delivery_anchor_peer_test.go — THE READING THAT WAS A SNAPSHOT, and the one
// fact that makes it correct.
//
// THE DEFECT (row 01a18317, measured 2026-10-28 on a live clone ground). The
// empty refusal's "behind" count was taken against deliveryBaseTip, which
// resolves `refs/remotes/origin/<base>` — a ref only a FETCH advances. No fetch
// runs anywhere on this leg before the empty gate (the first network step is the
// pull, and the pull is exactly what an empty ground never reaches), so on a
// workspace clone the tip is a snapshot of CLONE TIME. Measured on that row's
// ground at 23:12: origin/master read 977e8ca (the tip the clone was cut from at
// 23:05:47) while the peer's master carried c22e1c3 and 3 commits the ground had
// never seen, landed 23:11:56. The count answered 0, so the refusal still said
// "so there is nothing to pull and nothing to push".
//
// WHY THE EXISTING RED-PROOF IS GREEN ON BROKEN CODE (Rule 31's exact shape):
// internal/devworkspace/delivery_anchor_test.go's "behind" arm HANDS THE LEG A
// FETCH — `gitIn(t, dir, "fetch", "-q", "origin")` right after the base moves —
// so the stale remote-tracking ref is refreshed by the FIXTURE and the count the
// production code takes against it happens to be right. That fetch is the fixture
// doing the fix's job. It also makes the arm fatal on real ground:
// `git pull --rebase origin master` would hit the dirty worktree those commits
// leave behind, so the only way it could ever have run end-to-end was with the
// fetch that hid the defect.
//
// SO NOTHING HERE FETCHES. The base advances on the origin AFTER the ground was
// cut, the ground's own remote-tracking ref is MEASURED to still be the old tip,
// and deliveryEmptyGate is entered directly — the state every real run's end hook
// stands in. Two arms, and they are opposite facts:
//
//	① THE PEER ANSWERS: the count appears (a number, not a claim), the recovery
//	  command appears, and "nothing to pull" does NOT. Red before the fix.
//	② THE PEER DOES NOT ANSWER: the count is not taken from anything local and
//	  no claim is invented — the sentence falls back to the claim-free clause.

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestDeliveryEmptyRefusalReadsTheBehindCountFromThePEER is the red-proof for row
// 01a18317: the refusal's "behind" reading must come from the PEER's answer, not
// from a remote-tracking ref the channel never fetches.
func TestDeliveryEmptyRefusalReadsTheBehindCountFromThePEER(t *testing.T) {
	ctx := context.Background()

	t.Run("the base moves after the ground was cut, and nothing fetches for it", func(t *testing.T) {
		origin := newOrigin(t)
		cfg := cloneCfg(t, origin)

		// THE GROUND IS CUT HERE — the clone is taken now, and its
		// refs/remotes/origin/<base> is pinned at this tip for the rest of the
		// test (no fetch runs, by design).
		if _, err := Open(ctx, cfg); err != nil {
			t.Fatalf("Open: %v", err)
		}
		dir := Dir(cfg.Root, cfg.TaskID)

		// THE PRECONDITION THIS WHOLE TEST RESTS ON, MEASURED FIRST: the tip the
		// ground's local ref holds is the one the clone was cut from.
		staleTip, _, berr := deliveryBaseTip(ctx, dir, DefaultBranch, cfg.GroundBranch)
		if berr != nil {
			t.Fatalf("base tip: %v", berr)
		}

		// THE BASE MOVES AFTERWARDS — the shape of every run whose task sat in a
		// queue while the integration branch took other commits.
		const landedCommits = 2
		for i := 0; i < landedCommits; i++ {
			commitIn(t, origin, "landed-after-the-cut-"+strconv.Itoa(i)+".txt",
				"a commit the ground has never seen")
		}

		// THE FIXTURE'S SHAPE, measured rather than assumed: the ground's own
		// remote-tracking ref did NOT move. If some step refreshed it, this test
		// would be measuring the old behaviour and prove nothing.
		if again, _, err := deliveryBaseTip(ctx, dir, DefaultBranch, cfg.GroundBranch); err != nil || again != staleTip {
			t.Fatalf("fixture: the ground's remote-tracking tip moved from %s to %q (err %v) — "+
				"something fetched, and this test then proves nothing", staleTip, again, err)
		}

		// THE PRODUCTION CALL, entered directly: exactly what the end hook asks.
		blk, err := deliveryEmptyGate(ctx, cfg, dir, DefaultBranch)
		if err != nil {
			t.Fatalf("deliveryEmptyGate: %v", err)
		}
		// THE KIND MOVED, and this is the fix's own subject (row 01a19483): with
		// the shape decided from the SAME peer reading as the count, this fixture
		// — HEAD at the cut tip, the peer two commits further — IS the inherited
		// shape, not an empty ground. Before the fix it came back Empty (the shape
		// was taken from the stale remote-tracking ref) while the sentence still
		// carried the peer's count: the two-worlds symptom this arm was written
		// for. What the arm KEEPS asserting is the reading: the count is the
		// PEER's, and no local ref was trusted for it.
		if blk == nil || blk.Kind != BlockerKindInheritedDelivery {
			t.Fatalf("a ground the PEER has moved past has INHERITED its delivery: want %q, got %+v",
				BlockerKindInheritedDelivery, blk)
		}
		report := blk.Report()

		if want := fmt.Sprintf("%d commit(s) this ground does not have", landedCommits); !strings.Contains(report, want) {
			t.Errorf("the refusal must carry the PEER's count %q (the peer is %d commits ahead):\n%s",
				want, landedCommits, report)
		}
		if strings.Contains(report, deliveryEmptyNothingToMoveText) {
			t.Fatalf("a ground the PEER is %d commit(s) ahead of must not be told there is nothing "+
				"to pull — that sentence came from a remote-tracking ref nobody fetched:\n%s",
				landedCommits, report)
		}
		// AND NO RECOVERY IS OFFERED FOR AN INHERITED DELIVERY: the base already
		// CONTAINS this ground's HEAD, so a pull cannot make it deliverable.
		if strings.Contains(report, "recovers them") {
			t.Fatalf("an inherited delivery must not offer a pull as a recovery:\n%s", report)
		}
		t.Logf("the peer-measured refusal: %s", report)
	})

	t.Run("an unreachable peer abstains instead of asserting", func(t *testing.T) {
		origin := newOrigin(t)
		cfg := cloneCfg(t, origin)
		if _, err := Open(ctx, cfg); err != nil {
			t.Fatalf("Open: %v", err)
		}
		dir := Dir(cfg.Root, cfg.TaskID)

		// THE PEER IS GONE: the address answers nothing, so no count was taken.
		cfg.DevRepo = filepath.Join(cfg.Root, "no-such-peer-here")

		blk, err := deliveryEmptyGate(ctx, cfg, dir, DefaultBranch)
		if err != nil {
			// An unresolvable BASE would be a real error; an unanswered PEER is
			// not — the gate must keep its claim-free clause instead.
			t.Fatalf("an unanswered peer is an ABSTENTION, not an error: %v", err)
		}
		if blk == nil || blk.Kind != BlockerKindEmpty {
			t.Fatalf("want the empty blocker, got %+v", blk)
		}
		report := blk.Report()
		if strings.Contains(report, "commit(s) this ground does not have") {
			t.Fatalf("nothing was measured against the peer, so no count may be asserted:\n%s", report)
		}
		if !strings.Contains(report, deliveryEmptyNothingToMoveText) {
			t.Fatalf("an unmeasured peer must fall back to the claim-free clause:\n%s", report)
		}
		t.Logf("the abstention: %s", report)
	})
}

// TestDeliveryPeerOperandKeepsATransportAddressAsItIs pins the ONE distinction
// that decides whether the behind reading can be taken at all: a LOCAL PATH is
// turned into a file:// address (so the read never runs a receiving repository's
// upload-pack hooks), while an address some transport already serves — an ssh
// spelling such as `peerbox:/home/user/work/gopi`, this clone's own origin — is
// passed through UNTOUCHED. Prefixing the second kind with file:// would ask a
// transport that does not exist and the answer would look like an unreachable
// peer: exactly the false abstention this test exists to prevent.
func TestDeliveryPeerOperandKeepsATransportAddressAsItIs(t *testing.T) {
	sshAddress := "peerbox:/home/user/work/gopi"
	if operand, ref := deliveryPeerOperand(sshAddress, DefaultBranch); operand != sshAddress {
		t.Fatalf("an ssh address must be passed through as it is, got %q for %q", operand, sshAddress)
	} else if ref != "refs/heads/"+DefaultBranch {
		t.Fatalf("ref = %q, want %q", ref, "refs/heads/"+DefaultBranch)
	}

	localPath := t.TempDir()
	operand, _ := deliveryPeerOperand(localPath, DefaultBranch)
	if !strings.HasPrefix(operand, deliveryFileURLScheme) {
		t.Fatalf("a local path must become a %s address, got %q for %q",
			deliveryFileURLScheme, operand, localPath)
	}

	if operand, _ := deliveryPeerOperand("", DefaultBranch); operand != "" {
		t.Fatalf("no configured peer must spell no address, got %q", operand)
	}
}
