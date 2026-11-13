package devworkspace

// delivery_anchor_delivered_test.go — THE TWO ZEROES, which the empty gate used
// to answer with one sentence.
//
// THE DEFECT (row 01a182df, measured 2026-10-28). `deliveryEmptyGate` decides
// "is there anything to publish?" from ONE reading:
// `git rev-list --count HEAD --not origin/<base>`. The clone channel's own push
// MOVES `origin/<base>` to this run's tip, so after a SUCCESSFUL delivery the
// very same reading is 0 by definition — and the next attempt in that ground read
// it as "this run produced nothing", wrote `BlockerKindEmpty` and sent the reader
// looking for work that had already landed. Two opposite facts, one number, one
// sentence (Rule 15).
//
// THE FIX these arms pin: the gate takes a SECOND reading, and it is a fact about
// THIS RUN rather than about the number — the tip this run's own delivery pushed
// (Config.DeliveredTip), and whether the integration base can reach it NOW
// (`git merge-base --is-ancestor`). Reached ⇒ a DIFFERENT kind
// (BlockerKindAlreadyDelivered) and DIFFERENT words; not reached, or not
// measured, or nothing supplied ⇒ the empty kind, unchanged.
//
//	① a ground this run produced nothing in          → BlockerKindEmpty
//	② a ground whose own push already landed         → BlockerKindAlreadyDelivered,
//	                                                   and the two reports are not
//	                                                   the same text
//	③ a tip supplied that the base does NOT reach    → BlockerKindEmpty (the
//	                                                   second reading must be a
//	                                                   MEASUREMENT, never a latch)
//	④ a tip this store cannot read                   → BlockerKindEmpty (an
//	                                                   unanswered question is an
//	                                                   abstention, never a claim)

import (
	"context"
	"strings"
	"testing"
)

// emptyGateFor opens one clone workspace on a fresh origin and returns the
// config, the directory and its HEAD — the three things every arm below needs.
// It is a helper rather than a copy of four lines so the arms differ ONLY in the
// fact each one is about.
func emptyGateFor(t *testing.T) (Config, string, string) {
	t.Helper()
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	head := strings.TrimSpace(gitIn(t, dir, "rev-parse", gitHeadRef))
	return cfg, dir, head
}

// TestDeliveryEmptyGateTellsTheTwoZeroesApart is the red-proof for row
// 01a182df: the same `count == 0` must not be worded and classified the same way
// whether the run produced nothing or already delivered.
func TestDeliveryEmptyGateTellsTheTwoZeroesApart(t *testing.T) {
	ctx := context.Background()

	t.Run("a ground this run produced nothing in is still the EMPTY kind", func(t *testing.T) {
		cfg, dir, _ := emptyGateFor(t)

		// THE PRECONDITION, MEASURED: nothing ahead of the base.
		baseTip, integration, err := deliveryBaseTip(ctx, dir, DefaultBranch, "")
		if err != nil {
			t.Fatalf("base tip: %v", err)
		}
		if n, cerr := deliveryOutstanding(ctx, dir, gitHeadRef, baseTip); cerr != nil || n != 0 {
			t.Fatalf("fixture precondition: commits ahead against %s = %d (err %v), want 0", integration, n, cerr)
		}

		blk, err := deliveryEmptyGate(ctx, cfg, dir, DefaultBranch)
		if err != nil {
			t.Fatalf("deliveryEmptyGate: %v", err)
		}
		if blk == nil || blk.Kind != BlockerKindEmpty {
			t.Fatalf("a ground that produced nothing must stay %q, got %+v", BlockerKindEmpty, blk)
		}
		if strings.Contains(blk.Report(), deliveryAlreadyDeliveredMarker) {
			t.Fatalf("nothing was ever pushed, so the already-delivered marker must not appear:\n%s", blk.Report())
		}
	})

	t.Run("this run's own push already landed is its OWN kind", func(t *testing.T) {
		cfg, dir, _ := emptyGateFor(t)

		// THE WORK, AND THE PUSH THE CHANNEL MAKES, both real: the destination is
		// the integration branch (publishGround spells the same refspec), so the
		// fixture measures the shape production measures.
		commitIn(t, dir, "delivered.txt", "the work this run delivered")
		gitIn(t, dir, "push", "-q", originRemoteName, gitHeadRef+":"+refsHeadsPrefix+DefaultBranch)
		deliveredTip := strings.TrimSpace(gitIn(t, dir, "rev-parse", gitHeadRef))

		// THE PRECONDITION OF THE DEFECT ITSELF, MEASURED: the push moved the
		// base onto our tip, so the one-dimensional reading is now 0 — the number
		// cannot tell this state from an empty ground.
		baseTip, integration, err := deliveryBaseTip(ctx, dir, DefaultBranch, "")
		if err != nil {
			t.Fatalf("base tip: %v", err)
		}
		if n, cerr := deliveryOutstanding(ctx, dir, gitHeadRef, baseTip); cerr != nil || n != 0 {
			t.Fatalf("fixture precondition: after the push the count against %s is %d (err %v), want 0 — "+
				"this arm proves nothing unless the number collides", integration, n, cerr)
		}

		cfg.DeliveredTip = deliveredTip
		blk, err := deliveryEmptyGate(ctx, cfg, dir, DefaultBranch)
		if err != nil {
			t.Fatalf("deliveryEmptyGate: %v", err)
		}
		if blk == nil {
			t.Fatalf("a delivered ground must still answer with a blocker; nil means 'go ahead and deliver'")
		}
		if blk.Kind == BlockerKindEmpty {
			t.Fatalf("a run whose own push landed must NOT be told it produced nothing:\n%s", blk.Report())
		}
		if blk.Kind != BlockerKindAlreadyDelivered {
			t.Fatalf("blocker kind = %q, want %q", blk.Kind, BlockerKindAlreadyDelivered)
		}
		report := blk.Report()

		// THE FACTS IT MUST CARRY: the tip this run pushed, and the base that now
		// reaches it. A sentence that names neither is an opinion, not a reading.
		for _, want := range []string{deliveredTip, deliveryAlreadyDeliveredMarker, DefaultBranch} {
			if !strings.Contains(report, want) {
				t.Errorf("the already-delivered report must name %q:\n%s", want, report)
			}
		}

		// AND THE TWO REASONS MUST NOT SHARE A SENTENCE (Rule 15): the empty
		// blocker's own words are the counter-example, measured here rather than
		// remembered.
		emptyBlk := &Blocker{Kind: BlockerKindEmpty, Words: "a ground with nothing in it"}
		if strings.Contains(emptyBlk.Report(), deliveryAlreadyDeliveredMarker) {
			t.Fatalf("the empty refusal must not borrow the already-delivered vocabulary:\n%s", emptyBlk.Report())
		}
		if strings.Contains(report, deliveryEmptyNothingToMoveText) {
			t.Fatalf("a delivered ground is not 'nothing to pull and nothing to push' — the work WAS pushed:\n%s", report)
		}
		t.Logf("the already-delivered report: %s", report)
	})

	t.Run("a tip the base does NOT reach is still the EMPTY kind", func(t *testing.T) {
		cfg, dir, _ := emptyGateFor(t)

		// A COMMIT THIS GROUND MADE AND NEVER PUSHED, kept as an object while HEAD
		// goes back to the base — so the count is 0 (the ground is level with the
		// base) and the supplied tip is provably NOT something the base landed.
		commitIn(t, dir, "unpushed.txt", "work that never left this ground")
		unpushed := strings.TrimSpace(gitIn(t, dir, "rev-parse", gitHeadRef))
		gitIn(t, dir, "reset", "-q", "--hard", deliveryOriginPrefix+DefaultBranch)

		baseTip, _, err := deliveryBaseTip(ctx, dir, DefaultBranch, "")
		if err != nil {
			t.Fatalf("base tip: %v", err)
		}
		if n, cerr := deliveryOutstanding(ctx, dir, gitHeadRef, baseTip); cerr != nil || n != 0 {
			t.Fatalf("fixture precondition: commits ahead = %d (err %v), want 0", n, cerr)
		}
		if reached, ok := deliveryReachedFromBase(ctx, dir, unpushed, baseTip); !ok || reached {
			t.Fatalf("fixture precondition: the unpushed tip %s must be measurable and NOT reachable (reached=%v ok=%v)",
				unpushed, reached, ok)
		}

		cfg.DeliveredTip = unpushed
		blk, err := deliveryEmptyGate(ctx, cfg, dir, DefaultBranch)
		if err != nil {
			t.Fatalf("deliveryEmptyGate: %v", err)
		}
		if blk == nil || blk.Kind != BlockerKindEmpty {
			t.Fatalf("a tip the base does not reach is NOT a delivery: want %q, got %+v", BlockerKindEmpty, blk)
		}
		if strings.Contains(blk.Report(), deliveryAlreadyDeliveredMarker) {
			t.Fatalf("the second reading must be a measurement, never a latch on the field:\n%s", blk.Report())
		}
	})

	t.Run("a tip this store cannot read abstains into the EMPTY kind", func(t *testing.T) {
		cfg, dir, _ := emptyGateFor(t)
		// AN ANSWERABLE QUESTION ONLY IF GIT CAN LOOK: this sha is not in the
		// object store, so merge-base cannot answer. An unanswered question is an
		// abstention (Rule 15) — the claim-free empty kind, never a guess.
		cfg.DeliveredTip = "0123456789abcdef0123456789abcdef01234567"

		blk, err := deliveryEmptyGate(ctx, cfg, dir, DefaultBranch)
		if err != nil {
			t.Fatalf("an unreadable tip is an ABSTENTION, not an error: %v", err)
		}
		if blk == nil || blk.Kind != BlockerKindEmpty {
			t.Fatalf("want the empty kind on an unanswered reading, got %+v", blk)
		}
	})
}

// TestDeliveryEmptyGateDistinguishesInheritedDeliveryFromAnEmptyGround is the
// red-proof for the THIRD meaning of the same `count == 0` (epic: inherited
// delivery). Both arms BELOW reach the gate with `rev-list --count HEAD --not
// origin/master` answering 0, and the pre-fix gate told them apart from nothing:
// both came back BlockerKindEmpty with a "nothing to pull and nothing to push"
// clause that was FALSE for one of them and left the delivery loop re-measuring
// an immutable fact once per round (52 sessions measured carrying the phrase).
//
// The discriminator is the ONE thing the two shapes differ in, and it is
// measured here, never assumed:
//
//	① HEAD *IS* the base tip — a ground cut from the base that produced nothing.
//	   It stays EMPTY, and the fix must not touch it (an empty ground may still
//	   owe a closing report, so it keeps its own exit).
//	② HEAD is a STRICT ancestor of the base tip — the base landed this ground's
//	   content and moved on. Nothing to push, and nothing the run can do about
//	   it: that is the INHERITED delivery.
func TestDeliveryEmptyGateDistinguishesInheritedDeliveryFromAnEmptyGround(t *testing.T) {
	ctx := context.Background()

	t.Run("a ground standing ON the base tip stays EMPTY", func(t *testing.T) {
		cfg, dir, head := emptyGateFor(t)
		baseTip, integration, err := deliveryBaseTip(ctx, dir, DefaultBranch, "")
		if err != nil {
			t.Fatalf("base tip: %v", err)
		}
		// THE FIXTURE'S OWN SHAPE, measured: HEAD IS the base tip.
		if head != baseTip {
			t.Fatalf("fixture: a fresh ground must stand ON %s (%s != %s)", integration, head, baseTip)
		}
		blk, err := deliveryEmptyGate(ctx, cfg, dir, DefaultBranch)
		if err != nil {
			t.Fatalf("deliveryEmptyGate: %v", err)
		}
		if blk == nil || blk.Kind != BlockerKindEmpty {
			t.Fatalf("a ground that produced NOTHING must stay %q, got %+v", BlockerKindEmpty, blk)
		}
		if strings.Contains(blk.Report(), deliveryInheritedDeliveryMarker) {
			t.Fatalf("nothing was inherited — HEAD IS the base tip:\n%s", blk.Report())
		}
	})

	t.Run("a ground the base has moved PAST inherited its delivery", func(t *testing.T) {
		// ITS OWN ORIGIN, because this arm must move the BASE while leaving the
		// ground's HEAD where it is — the opposite of what emptyGateFor's caller
		// can do with the directory alone.
		origin := newOrigin(t)
		cfg := cloneCfg(t, origin)
		if _, oerr := Open(ctx, cfg); oerr != nil {
			t.Fatalf("Open: %v", oerr)
		}
		dir := Dir(cfg.Root, cfg.TaskID)
		head := strings.TrimSpace(gitIn(t, dir, "rev-parse", gitHeadRef))
		// THE BASE MOVES ON (someone else's work lands on the integration
		// branch). The ground keeps its own HEAD, which is now a STRICT ancestor
		// of the base tip — the shape of every row whose work was adopted and
		// whose queue position outlived the adoption.
		// The commit lands in the ORIGIN (someone else's merge), and the ground
		// FETCHES it — the shape production reaches: the channel's own
		// `pull --rebase` (or an earlier round) advanced the remote-tracking ref
		// past this ground's HEAD without moving HEAD itself.
		commitIn(t, origin, "landed-by-someone-else.txt", "work that landed on the base, not here")
		gitIn(t, dir, "fetch", "-q", originRemoteName)
		baseTip, integration, err := deliveryBaseTip(ctx, dir, DefaultBranch, "")
		if err != nil {
			t.Fatalf("base tip: %v", err)
		}
		headBefore := head

		// THE PINNED PRECONDITIONS, measured, because the arm proves nothing
		// unless the two numbers really do collide:
		if n, cerr := deliveryOutstanding(ctx, dir, gitHeadRef, baseTip); cerr != nil || n != 0 {
			t.Fatalf("fixture precondition: commits ahead against %s = %d (err %v), want 0", integration, n, cerr)
		}
		reached, ok := deliveryReachedFromBase(ctx, dir, gitHeadRef, baseTip)
		if !ok || !reached {
			t.Fatalf("fixture precondition: %s must contain HEAD (reached=%v ok=%v)", integration, reached, ok)
		}
		if headBefore == baseTip {
			t.Fatalf("fixture: HEAD must NOT be the base tip for this arm to mean anything (both %s)", baseTip)
		}

		blk, err := deliveryEmptyGate(ctx, cfg, dir, DefaultBranch)
		if err != nil {
			t.Fatalf("deliveryEmptyGate: %v", err)
		}
		if blk == nil {
			t.Fatal("an inherited ground must still answer with a blocker; nil means 'go ahead and deliver'")
		}
		if blk.Kind != BlockerKindInheritedDelivery {
			t.Fatalf("blocker kind = %q, want %q — reporting this as %q is the defect the kind removes:\n%s",
				blk.Kind, BlockerKindInheritedDelivery, BlockerKindEmpty, blk.Report())
		}
		report := blk.Report()
		// THE SENTENCE NAMES THE FACT AND THE TIP, and it does NOT wear the
		// empty ground's claim-free clause as its own conclusion.
		if !strings.Contains(report, deliveryInheritedDeliveryMarker) {
			t.Errorf("the sentence must state the fact it measured (%q):\n%s", deliveryInheritedDeliveryMarker, report)
		}
		if !strings.Contains(report, deliveryShortSha(headBefore)) {
			t.Errorf("the sentence must name the tip the base contains (%s):\n%s",
				deliveryShortSha(headBefore), report)
		}
		if !strings.Contains(report, "NOT a failure") {
			t.Errorf("a reader must be told this is not a failure:\n%s", report)
		}
		if strings.Contains(report, "delivery did not land") {
			t.Errorf("nothing was refused — the failure's prefix must not appear:\n%s", report)
		}
		t.Logf("the inherited answer: %s", report)
	})
}
