package devworkspace

// delivery_anchor_peer_shape_test.go — ONE PEER READING, TWO CONSUMERS (row
// 01a19483, the bug the report-only round 01a19462 filed by Rule 41).
//
// THE DEFECT. `deliveryEmptyGate` asks two questions about the same peer and used
// to answer them from two different worlds:
//
//	· "is the base ahead of me?" — deliveryBaseTip → `refs/remotes/origin/<base>`,
//	  which only a FETCH advances, and nothing on this leg fetches before the gate;
//	· "how many commits am I missing?" — deliveryCommitsMissing, which asks the
//	  PEER (`ls-remote`) and, when the tip object is absent, runs the ONE fetch
//	  that lands only in FETCH_HEAD.
//
// So on a ground the base had moved past, the SHAPE discriminator compared HEAD
// against a CLONE-TIME tip: `head == baseTip` held, the strict-ancestor shape was
// unreachable, BlockerKindInheritedDelivery could never fire, and the immutable
// fact was re-measured once per delivery round while the sentence contradicted
// itself — "carries no commit to deliver" AND "you are N behind, a pull recovers
// them" in one breath.
//
// THE ARMS BELOW ARE THE SHAPE, NOT THE COUNT, and the difference is what made
// the defect invisible to every existing test: each of those fetches the base
// forward for the ground first (delivery_anchor_test.go's "behind" arm does it
// explicitly, delivery_anchor_delivered_test.go's inherited arm does it to
// refresh `origin/<base>`), so the stale ref was refreshed BY THE FIXTURE. That
// is the fixture doing the production code's job, and it is why the third shape
// stayed unreached while the suite was green (Rule 31).
//
// NOTHING HERE FETCHES. The base moves on the ORIGIN after the ground was cut;
// the ground's own remote-tracking ref is MEASURED to still be the cut tip; and
// deliveryEmptyGate is entered directly — the state every real end hook stands in.

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

// peerBaseTip is the PEER's own answer for the integration base, read here (by
// the test, for its preconditions only) so an arm can state where the base
// really is. It is `ls-remote`, which writes nothing: a test that fetched would
// refresh the very ref whose staleness it is asserting.
func peerBaseTip(t *testing.T, dir, base string) string {
	t.Helper()
	url := strings.TrimSpace(gitIn(t, dir, gitRemoteVerb, gitGetURLVerb, originRemoteName))
	tip := deliveryPeerBaseTip(context.Background(), dir, url, base)
	if tip == "" {
		t.Fatalf("fixture: the peer at %s did not answer for %s", url, base)
	}
	return tip
}

// TestDeliveryEmptyGateTakesItsShapeAndItsCountFromOnePeerReading is the
// red-proof for row 01a19483. Three arms, three DIFFERENT outcomes, and the
// first two are the ones the single-reading gate could not tell apart.
func TestDeliveryEmptyGateTakesItsShapeAndItsCountFromOnePeerReading(t *testing.T) {
	ctx := context.Background()

	t.Run("a ground standing where the peer stands is EMPTY and claim-free", func(t *testing.T) {
		cfg, dir, head := emptyGateFor(t)

		// THE SHAPE, measured by the TEST from the PEER: HEAD IS the peer's tip.
		// This is the arm the fix must not touch — no reading can turn a ground
		// that produced nothing into an inherited delivery.
		peerTip := peerBaseTip(t, dir, DefaultBranch)
		if head != peerTip {
			t.Fatalf("fixture: a fresh ground must stand ON the peer's tip (HEAD %s, peer %s)", head, peerTip)
		}

		blk, err := deliveryEmptyGate(ctx, cfg, dir, DefaultBranch)
		if err != nil {
			t.Fatalf("deliveryEmptyGate: %v", err)
		}
		if blk == nil || blk.Kind != BlockerKindEmpty {
			t.Fatalf("a ground that produced NOTHING must stay %q, got %+v", BlockerKindEmpty, blk)
		}
		report := blk.Report()
		if strings.Contains(report, deliveryInheritedDeliveryMarker) {
			t.Fatalf("nothing was inherited — HEAD IS the peer's tip:\n%s", report)
		}
		if !strings.Contains(report, deliveryEmptyNothingToMoveText) {
			t.Fatalf("an empty ground keeps its claim-free clause:\n%s", report)
		}
		if strings.Contains(report, "this ground does not have") {
			t.Fatalf("no count may be asserted against a peer that is exactly level:\n%s", report)
		}
	})

	t.Run("a ground the peer has moved past has INHERITED its delivery", func(t *testing.T) {
		origin := newOrigin(t)
		cfg := cloneCfg(t, origin)
		if _, err := Open(ctx, cfg); err != nil {
			t.Fatalf("Open: %v", err)
		}
		dir := Dir(cfg.Root, cfg.TaskID)
		head := strings.TrimSpace(gitIn(t, dir, "rev-parse", gitHeadRef))

		// THE REMOTE-TRACKING TIP AT CUT TIME — the value the pre-fix gate took
		// its shape from, and the value this test then proves it never re-read.
		cutTip, _, berr := deliveryBaseTip(ctx, dir, DefaultBranch, cfg.GroundBranch)
		if berr != nil {
			t.Fatalf("base tip: %v", berr)
		}

		// THE BASE MOVES ON, in the ORIGIN only (someone else's merges land while
		// this row sits in its queue). Two commits, so the count is a number this
		// arm can name rather than ">0".
		const landedAfterTheCut = 2
		for i := 0; i < landedAfterTheCut; i++ {
			commitIn(t, origin, "landed-by-someone-else-"+strconv.Itoa(i)+".txt",
				"work that landed on the base, not here")
		}

		// THE FIXTURE'S OWN SHAPE, MEASURED — and it is the whole point: nothing
		// in this arming fetched, so the ground's remote-tracking ref still reads
		// the cut tip while the PEER has moved. If a step ever refreshed it, this
		// fixture would be doing the fix's job and the arm would prove nothing.
		if again, _, err := deliveryBaseTip(ctx, dir, DefaultBranch, cfg.GroundBranch); err != nil || again != cutTip {
			t.Fatalf("fixture: the ground's remote-tracking tip moved from %s to %q (err %v) — "+
				"something fetched, and this arm then proves nothing", cutTip, again, err)
		}
		peerTip := peerBaseTip(t, dir, DefaultBranch)
		if peerTip == cutTip {
			t.Fatalf("fixture: the peer did not move (%s == %s), so there is no inherited shape to measure",
				peerTip, cutTip)
		}
		// THE SHAPE THE OLD DISCRIMINATOR COULD NOT SEE: against the stale ref
		// HEAD *IS* the base tip (so shape ① "genuinely empty" was chosen), while
		// against the PEER HEAD is a STRICT ANCESTOR (shape ② "inherited").
		if head != cutTip {
			t.Fatalf("fixture: HEAD (%s) must be the stale tip (%s) for this arm to reproduce the defect", head, cutTip)
		}
		if head == peerTip {
			t.Fatalf("fixture: HEAD (%s) must NOT be the peer's tip (%s)", head, peerTip)
		}

		blk, err := deliveryEmptyGate(ctx, cfg, dir, DefaultBranch)
		if err != nil {
			t.Fatalf("deliveryEmptyGate: %v", err)
		}
		if blk == nil {
			t.Fatal("an inherited ground must still answer with a blocker; nil means 'go ahead and deliver'")
		}
		if blk.Kind != BlockerKindInheritedDelivery {
			t.Fatalf("blocker kind = %q, want %q — classifying this as %q is the defect that kept the loop "+
				"re-measuring an immutable fact:\n%s", blk.Kind, BlockerKindInheritedDelivery, BlockerKindEmpty,
				blk.Report())
		}
		report := blk.Report()

		// ① THE FACT IT MEASURED, and the tip whose content the base contains.
		for _, want := range []string{deliveryInheritedDeliveryMarker, deliveryShortSha(head), "NOT a failure"} {
			if !strings.Contains(report, want) {
				t.Errorf("the inherited sentence must carry %q:\n%s", want, report)
			}
		}

		// ② ONE PEER READING, TWO CONSUMERS — the same-world proof. The count in
		// the sentence is the count taken against the PEER's tip, which is the
		// very tip the shape was decided from. A gate keeping two readings would
		// have to name two tips; this asserts one, and no inherited sentence may
		// carry a recovery claim about a base that already CONTAINS its HEAD.
		wantCount := strconv.Itoa(landedAfterTheCut) + " commit(s) this ground does not have"
		if !strings.Contains(report, wantCount) {
			t.Errorf("the sentence must quote the count taken against the PEER's tip %s (%q):\n%s",
				deliveryShortSha(peerTip), wantCount, report)
		}
		for _, forbidden := range []string{
			gitPullVerb + " " + gitPullRebaseFlag + " " + originRemoteName + " " + DefaultBranch,
			"recovers them",
			deliveryEmptyNothingToMoveText,
		} {
			if strings.Contains(report, forbidden) {
				t.Errorf("an INHERITED delivery must not be told %q — the base already contains its HEAD, "+
					"so no pull recovers anything here:\n%s", forbidden, report)
			}
		}

		// ③ AND THE READING LEFT NO LOCAL TRACE, which is the second half of
		// "one world": if the gate had refreshed `origin/<base>` on its way to an
		// answer, the shape it took would have been a LOCAL tip after all (and the
		// next reader of that ref would see a peer state this leg invented). The
		// count's fetch lands in FETCH_HEAD only — measured here, after the fact.
		if still, _, err := deliveryBaseTip(ctx, dir, DefaultBranch, cfg.GroundBranch); err != nil || still != cutTip {
			t.Fatalf("the gate moved the ground's remote-tracking tip %s → %q (err %v): the measurement "+
				"must not create the freshness it reads", cutTip, still, err)
		}
		t.Logf("the inherited sentence: %s", report)
	})

	t.Run("a ground carrying its own work is not blocked at all", func(t *testing.T) {
		// THE CONTROL, and it is the one that proves the shared reading did not
		// turn the gate into a latch: a ground with a commit of its own has
		// ahead > 0 and the gate must step out of the way (nil, nil) BEFORE any
		// peer reading matters.
		cfg, dir, _ := emptyGateFor(t)
		commitIn(t, dir, "my-work.txt", "the work this run produced")
		blk, err := deliveryEmptyGate(ctx, cfg, dir, DefaultBranch)
		if err != nil {
			t.Fatalf("deliveryEmptyGate: %v", err)
		}
		if blk != nil {
			t.Fatalf("a ground with work of its own must not be blocked, got %q: %s", blk.Kind, blk.Report())
		}
	})
}
