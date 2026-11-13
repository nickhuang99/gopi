package devworkspace

// delivery_anchor_reflog_delivered_test.go — THE FOURTH MEANING OF THE SAME
// ZERO, and the one no caller can supply.
//
// THE DEFECT (epic: deliveryEmptyGate judges a SUCCESSFUL push as an empty
// ground). `deliveryEmptyGate` already separates three shapes of `ahead == 0`
// (empty / already-delivered-when-a-tip-was-SUPPLIED / inherited), and the
// caller-supplied arm (Config.DeliveredTip) is the one that owns "this run's own
// push moved the base onto this tip". It is OPTIONAL, by the file's own rule
// ("no tip supplied → an ABSTENTION, never a claim"), so when a caller supplies
// none — the delivery loop's ordinary shape — the single shot this gate holds
// cannot separate:
//
//	① a ground cut from the base that produced nothing   (ahead == 0, head == peer tip)
//	② this clone's OWN push having landed                (ahead == 0, head == peer tip)
//
// Both read `head == reading.tip`, so both fell into BlockerKindEmpty, whose
// sentence says "carries no commit to deliver … nothing to pull and nothing to
// push" about work that IS on the integration branch — and `empty` does not break
// the delivery loop, so every round re-measured an immutable fact.
//
// THE SECOND READING, inside shape ①, is a signature GIT writes and nothing else
// can: the ground's reflog for the remote-tracking base carries `update by push`
// as the newest entry (measured on this tree: a push moves that ref by a reflog
// update whose subject is exactly "update by push", while a fetch writes
// "fetch -q origin: fast-forward"). A push also REFUSES to move a known
// remote-tracking ref to a commit it does not have, so the tip in that entry is
// the ground's own existing commit — a backdated or forged entry cannot buy
// anything it does not already hold. Read at the level the ref moved: tip in the
// entry AND that tip == this HEAD.
//
// THE ARMS, each a DIFFERENT fact:
//
//	① the push landed, no tip supplied      → BlockerKindAlreadyDelivered
//	② the same tip arrived by a FETCH       → BlockerKindEmpty (a fetch is not a
//	                                          push; the claim-free clause stands)
//	③ the push landed, no reflog to read    → BlockerKindEmpty (an unanswered
//	                                          question is an ABSTENTION — Rule 15)

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reflogPathFor spells the on-disk reflog of one remote-tracking ref: the file
// `git reflog show` reads. Named here rather than inline so arm ③ removes the
// exact object the measurement depends on (a clone's `.git` is a DIRECTORY —
// the shape this ground has).
func reflogPathFor(t *testing.T, dir, branch string) string {
	t.Helper()
	return filepath.Join(dir, ".git", "logs", "refs", "remotes", originRemoteName, branch)
}

// TestDeliveryEmptyGateOwnPushIsNotAnEmptyGround is the red-proof for the epic:
// a ground whose OWN push already landed must be reported as a DELIVERY, using no
// caller-supplied tip at all, while the same tip arriving any other way keeps the
// claim-free empty sentence.
func TestDeliveryEmptyGateOwnPushIsNotAnEmptyGround(t *testing.T) {
	ctx := context.Background()

	t.Run("this clone's own push landed ⇒ ALREADY DELIVERED, with no tip supplied", func(t *testing.T) {
		cfg, dir, _ := emptyGateFor(t)

		// THE WORK, AND THE PUSH THE CHANNEL MAKES — both real, and to the
		// integration ref, so the remote-tracking ref moves exactly as production
		// moves it (this is the same refspec shape the channel publishes with, and
		// the push is what writes the reflog entry the gate will read).
		commitIn(t, dir, "delivered.txt", "the work this run delivered")
		head := strings.TrimSpace(gitIn(t, dir, "rev-parse", gitHeadRef))
		gitIn(t, dir, "push", "-q", originRemoteName, gitHeadRef+":"+refsHeadsPrefix+DefaultBranch)

		// THE PRECONDITION OF THE DEFECT ITSELF, MEASURED: the count is 0, so the
		// one-dimensional reading cannot tell this state from an empty ground.
		baseTip, integration, err := deliveryBaseTip(ctx, dir, DefaultBranch, "")
		if err != nil {
			t.Fatalf("base tip: %v", err)
		}
		if n, cerr := deliveryOutstanding(ctx, dir, gitHeadRef, baseTip); cerr != nil || n != 0 {
			t.Fatalf("fixture precondition: commits ahead against %s = %d (err %v), want 0 — "+
				"this arm proves nothing unless the number collides", integration, n, cerr)
		}
		// AND THE OTHER HALF OF SHAPE ①: the peer's live tip IS this HEAD, which is
		// why no caller-supplied tip could have been needed.
		reading := deliveryPeerReadingTip(ctx, dir, deliveryOriginURL(ctx, dir, cfg.DevRepo), DefaultBranch)
		if reading.tip != head {
			t.Fatalf("fixture precondition: the peer's %s must stand on this HEAD (%s), it answers %q",
				integration, head, reading.tip)
		}
		// AND THE SIGNATURE IS REALLY THERE, read independently of the gate.
		reflog := strings.TrimSpace(gitIn(t, dir, "reflog", "show", deliveryReflogNewestFlag,
			deliveryReflogFormatFlag+deliveryReflogFormat, deliveryFullRef(DefaultBranch)))
		if !strings.HasSuffix(reflog, deliveryReflogFieldSep+gitUpdateByPushSubject) {
			t.Fatalf("fixture precondition: the remote-tracking reflog's newest entry must be git's push subject, got %q", reflog)
		}
		// NO TIP IS SUPPLIED — the whole point of the arm.
		cfg.DeliveredTip = ""

		blk, err := deliveryEmptyGate(ctx, cfg, dir, DefaultBranch)
		if err != nil {
			t.Fatalf("deliveryEmptyGate: %v", err)
		}
		if blk == nil {
			t.Fatal("a delivered ground must still answer with a blocker; nil means 'go ahead and deliver'")
		}
		if blk.Kind != BlockerKindAlreadyDelivered {
			t.Fatalf("a ground whose own push landed must not be told it produced nothing: kind = %q, want %q\n%s",
				blk.Kind, BlockerKindAlreadyDelivered, blk.Report())
		}
		report := blk.Report()
		for _, want := range []string{deliveryAlreadyDeliveredMarker, head, integration} {
			if !strings.Contains(report, want) {
				t.Errorf("the already-delivered report must name %q:\n%s", want, report)
			}
		}
		if strings.Contains(report, deliveryEmptyNothingToMoveText) {
			t.Errorf("the work WAS pushed — the empty ground's claim-free clause must not appear:\n%s", report)
		}
		t.Logf("Ran: git commit; git push origin %s:%s; deliveryEmptyGate(cfg with NO DeliveredTip)\n"+
			"Result: kind=%q — %s", gitHeadRef, refsHeadsPrefix+DefaultBranch, blk.Kind, report)
	})

	t.Run("the same tip arriving by a FETCH is still the EMPTY kind", func(t *testing.T) {
		cfg, dir, _ := emptyGateFor(t)
		// THE BASE MOVES BY SOMEONE ELSE'S WORK, and this ground TAKES it — the
		// fetch writes its own reflog subject. HEAD is then reset onto that very
		// tip, which is shape ① by every number the old gate could read
		// (ahead == 0, head == peer tip). A fetch is not a delivery.
		commitIn(t, cfg.DevRepo, "landed-elsewhere.txt", "work that landed on the base, not here")
		gitIn(t, dir, "fetch", "-q", originRemoteName)
		gitIn(t, dir, "reset", "-q", "--hard", deliveryOriginPrefix+DefaultBranch)
		head := strings.TrimSpace(gitIn(t, dir, "rev-parse", gitHeadRef))

		reading := deliveryPeerReadingTip(ctx, dir, deliveryOriginURL(ctx, dir, cfg.DevRepo), DefaultBranch)
		if reading.tip != head {
			t.Fatalf("fixture precondition: the peer's tip must equal this HEAD (%s), it answers %q", head, reading.tip)
		}
		reflog := strings.TrimSpace(gitIn(t, dir, "reflog", "show", deliveryReflogNewestFlag,
			deliveryReflogFormatFlag+deliveryReflogFormat, deliveryFullRef(DefaultBranch)))
		if !strings.HasSuffix(reflog, deliveryReflogFieldSep+gitFetchFastForwardSubject) {
			t.Fatalf("fixture precondition: a FETCH must have written git's own fetch subject (%q), got %q",
				gitFetchFastForwardSubject, reflog)
		}
		cfg.DeliveredTip = ""

		blk, err := deliveryEmptyGate(ctx, cfg, dir, DefaultBranch)
		if err != nil {
			t.Fatalf("deliveryEmptyGate: %v", err)
		}
		if blk == nil || blk.Kind != BlockerKindEmpty {
			t.Fatalf("a fetch is not a delivery: want %q, got %+v", BlockerKindEmpty, blk)
		}
		if strings.Contains(blk.Report(), deliveryAlreadyDeliveredMarker) {
			t.Fatalf("no push ever moved this ref — the delivery claim must not appear:\n%s", blk.Report())
		}
		t.Logf("Ran: git fetch origin (reflog newest = %q); deliveryEmptyGate(cfg with NO DeliveredTip)\n"+
			"Result: kind=%q", reflog, blk.Kind)
	})

	t.Run("no reflog to read abstains into the EMPTY kind", func(t *testing.T) {
		cfg, dir, _ := emptyGateFor(t)
		commitIn(t, dir, "own2.txt", "work this run pushed")
		head := strings.TrimSpace(gitIn(t, dir, "rev-parse", gitHeadRef))
		gitIn(t, dir, "push", "-q", originRemoteName, gitHeadRef+":"+refsHeadsPrefix+DefaultBranch)
		reading := deliveryPeerReadingTip(ctx, dir, deliveryOriginURL(ctx, dir, cfg.DevRepo), DefaultBranch)
		if reading.tip != head {
			t.Fatalf("fixture precondition: the peer's tip must equal this HEAD (%s), it answers %q", head, reading.tip)
		}
		// THE EVIDENCE REMOVED, THE PUSH LEFT STANDING: the question "did a push
		// move this ref?" can no longer be answered, and an unanswered question is
		// an ABSTENTION, never a claim (Rule 15).
		if rerr := os.Remove(reflogPathFor(t, dir, DefaultBranch)); rerr != nil {
			t.Fatalf("remove the remote-tracking reflog: %v", rerr)
		}
		cfg.DeliveredTip = ""

		blk, err := deliveryEmptyGate(ctx, cfg, dir, DefaultBranch)
		if err != nil {
			t.Fatalf("an unreadable reflog is an ABSTENTION, not an error: %v", err)
		}
		if blk == nil || blk.Kind != BlockerKindEmpty {
			t.Fatalf("want the empty kind when the reflog cannot be read, got %+v", blk)
		}
		if strings.Contains(blk.Report(), deliveryAlreadyDeliveredMarker) {
			t.Fatalf("an unreadable reflog is not evidence of a delivery:\n%s", blk.Report())
		}
		t.Logf("Ran: git push origin %s:%s; rm %s; deliveryEmptyGate(cfg with NO DeliveredTip)\n"+
			"Result: kind=%q (abstention, claim-free sentence)", gitHeadRef, refsHeadsPrefix+DefaultBranch,
			reflogPathFor(t, dir, DefaultBranch), blk.Kind)
	})
}
