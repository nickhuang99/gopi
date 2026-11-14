package tools

// task_ground_delivery_reading.go — THE DELIVERY AXIS of `gopi task show`: does
// the branch this row RECORDS exist anywhere but its own ground, and does the
// peer hold it?
//
// THE DEFECT THIS ANSWERS (epic 01a17e7b; the A half landed in f64c703 and
// 161f48a). Two defects were stacked on one row. (A) the advisory ground line
// asked for the row's branch ref in the RECORDED repo, so a clone-channel run —
// whose branch lives in its CLONE's own ref store — read `(NOT FOUND now — a
// stale reading)`; f64c703 fixed that by asking the branch by the ground's
// SHAPE and by carrying the list of directories asked
// (task_ground_branch_probe.go). (B) is the one this file reads: THE DELIVERY
// LEG RUNS ONCE, FROM THE RUN'S OWN END HOOK. A run interrupted by a signal
// never reaches that hook, so its commits, its green tests and its report are
// finished while nothing was ever pushed — and the ledger rendered such a row
// exactly like a run that did NOTHING, because no surface asked the two
// questions that tell them apart:
//
//	1. is the row's tip reachable anywhere other than its own ground?
//	2. does the PEER advertise it?
//
// Measured on this host (2026-10-28, row 01a17df2-…, killed by a signal): its
// work was in `refs/heads/task/<id>` of a CLONE ground, the recorded repo had no
// such ref, and no peer ever received it. Then the clone AND the run's session
// directory were collected by the sweep, and the tip
// (3af81a93271e80356c99b95263a125a808a27e67) can no longer be read in ANY repo
// on this machine:
//
//	$ git -C /home/user/work/gopi cat-file -t 3af81a93…
//	fatal: git cat-file: could not get object info
//
// That is what "B was never fixed" costs: the loss is not that a branch is
// missing — it is that nothing ever SAID so, and by the time anyone looked the
// evidence was gone too. This line makes the state a grep-able fact WHILE the
// ground still exists, which is the only window in which a delivery can be
// re-run (`gopi task retry-delivery`, the path that already exists).
//
// WHAT IT IS NOT:
//   - NOT a writer. It states directories and runs read-only git; `task show`
//     never changes the deliverable, the row, an event or a ref.
//   - NOT a second spelling of any reader (Rule 14): the branch existence and
//     the asked-places evidence come from taskGroundBranchRefIn, the peer's
//     answer from deliveryRetryPeerTip, the tip from the same read
//     deliveryRetryRowTip performs (HEAD in a clone ground, the ref elsewhere),
//     and the channel from finishCleanupRowTargetResolution.
//   - NOT a retry and NOT a change of delivery TIMING. When it runs, where it
//     runs and what it pushes are untouched — the line only makes the state
//     readable (Rule 28: the delivery life cycle is the system layer's).
//   - NEVER a claim it could not observe: an axis it could not read is
//     UNCHECKED and says WHAT could not be read (Rule 15). "I could not ask" is
//     never rendered as "the peer has nothing", and "nothing is here" is never
//     rendered as "the work never existed".
//
// COST. Only terminal rows that record a branch pay for it: up to two
// `rev-parse --verify` probes (the branch axis), one tip read, and — only when
// a peer actually resolves — one read-only `ls-remote`. Every call is bounded
// by the package timeout that already governs that lane.

import (
	"context"
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/devworkspace"
)

// TaskShowDeliveryLabel is the line's label, named once (Rule 1b) so the
// renderer, the CLI test and a future consumer face share one spelling. It is
// deliberately distinct from TaskShowGroundLabel: that line says WHERE the run
// worked, this one says whether what it produced ever LEFT.
const TaskShowDeliveryLabel = "delivery (read now):"

// The three verdicts the line may state, each an operator DISPOSITION (Rule 1:
// a value acted on is a named constant, never a literal at the render site).
const (
	// TaskShowDeliveryDelivered — the peer advertises the branch at exactly the
	// tip this row's own ground/repo holds. The strongest positive the reading
	// can make, and the only one it will make.
	TaskShowDeliveryDelivered = "DELIVERED"
	// TaskShowDeliveryUndelivered — the reading completed and the peer does not
	// hold this row's tip (either it advertises nothing for the branch, or a
	// different commit). A debt an operator can act on.
	TaskShowDeliveryUndelivered = "UNDELIVERED"
	// TaskShowDeliveryMergedLocally — the peer advertises no ref for the branch,
	// BUT the commit is already contained in the recorded repo's INTEGRATION
	// branch. Measured on this host the same hour the line was written (rows
	// 01a17e03-12f0 and 01a17dfd-09ef): both tips are 161f48a, which IS master's
	// tip, so "the work did not land" would have been a false alarm — the ref
	// never reached the peer, the WORK already had. A verdict word is a claim,
	// and this is the one the observation supports.
	TaskShowDeliveryMergedLocally = "MERGED-LOCALLY"
	// TaskShowDeliveryUnchecked — an axis the verdict needs could not be read.
	// It is NOT a soft "no" (Rule 15): the why names what was not readable, so
	// a reader can tell an unreadable channel from an empty one.
	TaskShowDeliveryUnchecked = "UNCHECKED"
)

// The WHY spellings of this file, named once (Rule 1b). Every sentence that a
// test pins is addressed through its constant, never re-typed.
const (
	// taskShowDeliveryDeliveredFmt — %s×3: target, branch, tip. It states the
	// comparison it made rather than the word "delivered" alone: a peer that
	// advertises the NAME at a different commit is not a delivery, and the two
	// readings must not print the same sentence.
	taskShowDeliveryDeliveredFmt = "the peer %s advertises %s at this row's own tip %s — the run's work left this machine"
	// taskShowDeliveryCloneOnlyFmt — %s×5: branch, tip, ground, repo, peer.
	// The measured shape: the work exists in ONE directory in the world. The
	// "the recorded repo does not hold it" clause is only TRUE for a clone
	// (its refs exist nowhere else); a linked worktree's refs ARE the recorded
	// repo's, so that shape gets taskShowDeliveryGroundOnlyFmt below instead of
	// a sentence that would describe a difference which does not exist.
	taskShowDeliveryCloneOnlyFmt = "this row's branch %s is at %s ONLY in its own ground %s — the recorded repo %s does not hold it, and neither does the peer %s"
	// taskShowDeliveryGroundOnlyFmt — %s×4: branch, tip, ground, peer. The same
	// verdict for the shape where the ground and the repo are ONE ref store.
	taskShowDeliveryGroundOnlyFmt = "this row's branch %s is at %s only in %s, and the peer %s does not advertise it"
	// taskShowDeliveryPeerOthersFmt — %s×4: branch, peer tip, this row's tip,
	// where that tip lives.
	taskShowDeliveryPeerOthersFmt = "the peer advertises %s at %s, which is NOT this row's tip %s (%s) — the ref the peer holds is some other commit, so this row's work did not land"
	// taskShowDeliveryNothingFmt — %s×3: branch, asked places, and the row's own
	// landed record (a witness that is CONSULTED and names nothing, or the
	// statement that the ledger could not even be read). The absence of a
	// READING, stated as such: it never claims the work never existed.
	taskShowDeliveryNothingFmt = "no ref in any place this ground can hold %s carries it (asked %s), its own landed record names no tip either (%s), and the peer does not advertise it either — no tip can be named for this row, so nothing can be delivered or compared (this is the absence of a reading, not evidence that the work never existed)"
	// taskShowDeliveryNoChannelFmt — %s: the repo whose peers were asked.
	taskShowDeliveryNoChannelFmt = "no peer resolves for %s on this host, so \"did the work leave\" cannot be asked; the local reading is: %s"
	// taskShowDeliveryNoRepoFmt — the row records no repo at all.
	taskShowDeliveryNoRepoFmt = "this row records no repo, so there is no delivery channel to ask — the local reading is: %s"
	// taskShowDeliveryPeerUnknownTipFmt — %s×3: target, branch, peer tip plus
	// the local reason.
	taskShowDeliveryPeerUnknownTipFmt = "the peer %s advertises %s at %s, but no local tip can be named for this row (%s) — the peer holds the NAME and this reading cannot say it holds this row's WORK"
	// taskShowDeliveryMergedLocallyFmt — %s×4: branch, tip, integration ref,
	// peer. It states the LOCAL evidence only; it does NOT say the peer holds
	// the work, because nothing here asked the peer an ancestry question (that
	// needs a fetch, and this reading writes nothing).
	taskShowDeliveryMergedLocallyFmt = "the peer %s advertises no ref for %s, but this row's tip %s is already contained in %s of the recorded repo — the branch was never delivered, the WORK already is (a local ancestry reading; the peer's own integration branch was not asked, because that needs a fetch and this line writes nothing)"
	// taskShowDeliveryRecordedLandingFmt — %s×2: the tip the record names, the
	// integration ref that CONTAINS it. It is the clone channel's delivered
	// shape: the channel publishes the INTEGRATION branch and reclaims the
	// run's own address, so `task/<id>` is absent BY DESIGN on every row it
	// delivered — and the tip is then nameable only from the channel's own
	// landed record on the row.
	//
	// WHY THE RECORD IS NOT ALLOWED TO DECIDE ON ITS OWN (the sentence says so
	// out loud, because a reader will otherwise take "the record says it was
	// pushed" for arrival): the record names a COMMIT; whether that commit is
	// on the integration branch is measured HERE, by `git merge-base
	// --is-ancestor`, exactly as it is for a tip read out of a tree. The same
	// discipline the delivery-proof arm states for its own third witness
	// (`!tipFromLandedRecord`): a witness names IDENTITY, arrival is measured.
	taskShowDeliveryRecordedLandingFmt = "the clone channel's own landed record on this row names its tip %s (read in no directory — the ground is gone), and that tip is MEASURED to be contained in %s of the recorded repo — the channel published the integration branch and reclaimed the run's own ref, so the address is gone by design while the WORK landed (the record names the commit; the containment is what proves arrival; the peer's own integration branch was not asked, because that needs a fetch and this line writes nothing)"
	// taskShowDeliveryRecordUnconfirmedFmt — %s×2: the tip the record names,
	// the integration refs that were asked. THE REVERSE ARM, and the reason
	// this repair cannot turn "cannot decide" into "everything passed": a
	// record that claims a push is NOT evidence the push landed, so a tip no
	// integration ref contains stays UNCHECKED with the claim and the refs
	// asked named.
	taskShowDeliveryRecordUnconfirmedFmt = "the clone channel's own landed record on this row names its tip %s, but none of the recorded repo's integration refs contains it (asked %s) — a record states that a push was attempted, it is NOT evidence of arrival, and no ref this ground can read holds that commit, so the delivery cannot be confirmed (this is the absence of a reading, not evidence that the work never existed)"
	// taskShowDeliveryRecordUnreadableFmt — %s×2: the tip, the recorded repo.
	// The same non-verdict as the sentence above, for the sharper reason: the
	// repo cannot even resolve the sha, so no containment question could be
	// asked there at all.
	taskShowDeliveryRecordUnreadableFmt = "the clone channel's own landed record on this row names its tip %s, but the recorded repo %s cannot even resolve that commit, so no containment could be measured (this is the absence of a reading, not evidence that the work never existed)"
	// taskShowDeliveryRecordTipLocalFmt — %s: the tip. The local half when the
	// tip came from the record: it must not claim a directory, because none
	// answered.
	taskShowDeliveryRecordTipLocalFmt = "the row's branch is at %s per the clone channel's own landed record on this row (read in no directory — the ground is gone)"
	// deliveryProofLandedWitnessText — what the THIRD witness answered when the
	// reading had no tip at all. It names the witness by what it IS (the
	// channel's own record on the row) so a reader can tell "this shape was
	// asked and had nothing" from "nobody asked it" — and it does not claim
	// that the record is ABSENT, because the reader cannot tell an empty ledger
	// from an unreadable one (deliveryProofLandedTip logs which, and answers ""
	// for both — Rule 15: the sentence must be true for BOTH).
	deliveryProofLandedWitnessText = "the clone channel's landed record on this row names no tip, or the ledger holding it could not be read — the reader reports which, and this line will not guess"
	// taskShowDeliveryLocalTipFmt — %s×2: tip, the directory holding it.
	taskShowDeliveryLocalTipFmt = "the row's branch is at %s in %s"
	// taskShowDeliveryNoLocalTip is the local half when no tip is readable at
	// all — the sentence an UNCHECKED reading quotes instead of an empty slot.
	taskShowDeliveryNoLocalTip = "the row's branch could not be read in any directory this ground can hold it in"
)

// taskGroundDeliveryReading is ONE row's delivery answer, with the evidence of
// how each axis was obtained. An axis that could not be read leaves its own
// field empty and its reason in why — never a zeroed claim.
type taskGroundDeliveryReading struct {
	branch   string
	repo     string
	worktree string
	// ref is the branch axis' answer WITH the places it asked ("" sites when
	// the row names no directory to ask).
	ref taskGroundBranchRef
	// tip is the commit the row's branch carries in the directory that holds
	// it (the clone ground's HEAD, else the ref in the recorded repo).
	tip string
	// recorded says the tip came from the clone channel's OWN landed record on
	// the row (deliveryProofLandedTip) rather than from a directory, because no
	// directory holds the ref any more. It names IDENTITY only — arrival is
	// still measured against the integration refs — so it is a WITNESS, never a
	// verdict, and verdict() below never turns it into one on its own.
	recorded bool
	// integrationAsked is the integration refs the containment question could
	// actually ask (taskGroundIntegrationRefsPresent) — the evidence an
	// UNCHECKED reading quotes instead of an empty slot.
	integrationAsked []string
	// tipReadableInRepo says the recorded repo can resolve this row's tip at
	// all, which is the precondition for measuring containment THERE.
	tipReadableInRepo bool
	// mainDir is the directory the peer channel and the integration refs are
	// resolved in (finishCleanupMainRepoDir of the recorded repo).
	mainDir string
	// target is the resolved peer, "" when none resolved (no channel).
	target string
	// peerTip is what the peer advertises for the branch, "" when it advertises
	// nothing (or when it was never asked — peerAsked tells the two apart).
	peerTip string
	// integration is the recorded repo's integration ref name when the row's
	// tip is contained in it ("" otherwise) — the local no-debt evidence.
	integration string
	status      string
	why         string
}

// taskShowDeliveryReading renders the delivery line for one row ("" when there
// is no delivery question to answer).
//
// ONLY TERMINAL ROWS PAY: an in-flight row has not finished, so "not delivered
// yet" is the ordinary state and printing it would be the noise this epic
// removes. `failed` is the population that matters most — it is the shape an
// interrupted run leaves (measured row 01a17df2 was `failed` with its work
// complete) — and `completed` is read too, because a completed row that never
// reached the peer is exactly the defect 01a163df recorded.
func taskShowDeliveryReading(t *Task) string {
	if t == nil || !taskTerminalStatus(t.Status) {
		return ""
	}
	recRepo, recBranch, recWT := taskRowDeliverable(t.TaskID)
	branch := strings.TrimSpace(recBranch)
	if branch == "" {
		return "" // no branch recorded: no delivery promise to read
	}
	ctx, cancel := context.WithTimeout(context.Background(), branchDeleteGitTimeout)
	defer cancel()
	d := taskGroundDeliveryReading{branch: branch, repo: recRepo, worktree: recWT}
	d.ref = taskGroundBranchRefIn(recRepo, recWT, branch)
	d.tip = taskGroundDeliveryTip(ctx, d.ref)
	if d.tip == "" {
		// THE CLONE CHANNEL'S DELIVERED SHAPE (measured 2026-11-05, row
		// 01a1a891-c437). The channel publishes the INTEGRATION branch
		// (`HEAD:refs/heads/<integration>`) and its close-out RECLAIMS the run's
		// own `task/<id>` ref, so the row's ADDRESS is absent BY DESIGN on every
		// row it delivered — and this reading asked only the address, so it had
		// no tip and fell to the "nothing is anywhere" sentence for work that
		// was sitting on origin/master.
		//
		// The witness that DOES survive is the row's own landed record, written
		// by the channel at the moment it pushed (deliveryProofLandedTip — the
		// same reader the delivery-proof arm and the completion witness use, so
		// the three faces cannot disagree about what a row's record says). It
		// names a COMMIT and nothing more: the containment question below is
		// still measured by git, which is what stops a record from certifying
		// its own arrival.
		d.tip, d.recorded = taskGroundDeliveryRecordedTip(t.TaskID, branch)
	}

	mainDir := finishCleanupMainRepoDir(recRepo)
	if strings.TrimSpace(mainDir) == "" {
		d.status, d.why = TaskShowDeliveryUnchecked, fmt.Sprintf(taskShowDeliveryNoRepoFmt, d.localText())
		return TaskShowDeliveryLabel + " " + d.status + " — " + d.why
	}
	d.mainDir = mainDir
	// The containment question is asked in ONE place, and its PRECONDITION is
	// recorded separately: a repo that cannot resolve the sha could not have
	// answered, which is not the same reading as "it answered no" (Rule 15).
	d.integrationAsked = taskGroundIntegrationRefsPresent(ctx, mainDir)
	d.tipReadableInRepo = deliveryProofCommitReadable(ctx, mainDir, d.tip)
	if d.tipReadableInRepo {
		d.integration = taskGroundDeliveryMergedInto(ctx, mainDir, d.tip)
	}
	d.target = finishCleanupRowTargetResolution(mainDir).SSHTarget
	if strings.TrimSpace(d.target) == "" {
		d.status, d.why = TaskShowDeliveryUnchecked, fmt.Sprintf(taskShowDeliveryNoChannelFmt, recRepo, d.localText())
		return TaskShowDeliveryLabel + " " + d.status + " — " + d.why
	}

	// THE PEER IS ASKED through the delivery's own channel (read-only
	// `ls-remote` over the resolved target — the same call the retry path makes
	// before it acts, so this line and that path cannot disagree about what the
	// peer holds).
	d.peerTip = deliveryRetryPeerTip(ctx, mainDir, d.target, branch)
	d.status, d.why = d.verdict()
	return TaskShowDeliveryLabel + " " + d.status + " — " + d.why
}

// taskGroundDeliveryRecordedTip names the row's tip from the CLONE CHANNEL's own
// landed record, and reports that it did so.
//
// THE READER IS THE SIBLING ARM'S (deliveryProofLandedTip), never a second query
// (Rule 14): the delivery-proof arm and the completion witness read the same
// record, and a third spelling here could disagree with them about what the row
// says it pushed. What this function adds is a NAMED BOOLEAN for the caller —
// `recorded` — because the two witnesses have different standing. A tip read out
// of a directory is a fact about a ref; a tip read out of a record is the run's
// own CLAIM, so the caller must render it as one and must never let it decide
// arrival by itself.
func taskGroundDeliveryRecordedTip(taskID, branch string) (tip string, recorded bool) {
	tip = deliveryProofLandedTip(taskID, branch)
	return tip, tip != ""
}

// taskGroundDeliveryTip reads the commit the row's branch carries, from the
// directory the branch axis FOUND it in — a clone ground's HEAD (its own ref
// store, checked out on the row's branch) or `refs/heads/<branch>` in the
// recorded repo. "" when the branch was found nowhere or the read failed.
func taskGroundDeliveryTip(ctx context.Context, ref taskGroundBranchRef) string {
	if !ref.exists || strings.TrimSpace(ref.where) == "" || strings.TrimSpace(ref.branch) == "" {
		return ""
	}
	if ref.whereLabel() == string(devworkspace.GroundModelClone) {
		return deliveryProofGitRead(ctx, ref.where, finishCleanupPreMergeRevParseVerb, taskGitHeadToken)
	}
	return deliveryProofGitRead(ctx, ref.where, finishCleanupPreMergeRevParseVerb,
		finishCleanupPreMergeVerifyFlag, refsHeadsPrefix+ref.branch)
}

// verdict is the comparison itself, stated once so the positive and the two
// negative shapes cannot drift apart: DELIVERED requires the peer's tip to
// EQUAL this row's own tip. Anything less is a debt or an unreadable axis.
func (d taskGroundDeliveryReading) verdict() (status, why string) {
	switch {
	case d.tip != "" && d.peerTip == d.tip:
		return TaskShowDeliveryDelivered, fmt.Sprintf(taskShowDeliveryDeliveredFmt, d.target, d.branch, d.tip)
	case d.recorded && d.integration != "":
		// THE CLONE CHANNEL'S DELIVERED SHAPE, and the arm this epic exists for.
		// The address is gone by design (the channel reclaims `task/<id>`), the
		// record names the commit, and the commit is MEASURED to be contained in
		// the recorded repo's integration ref — so the work landed and this is a
		// local ancestry reading of exactly the kind the sibling arms make.
		return TaskShowDeliveryMergedLocally, fmt.Sprintf(taskShowDeliveryRecordedLandingFmt,
			d.tip, d.integration)
	case d.tip != "" && d.peerTip != "" && d.integration != "":
		return TaskShowDeliveryMergedLocally, fmt.Sprintf(taskShowDeliveryMergedLocallyFmt,
			d.target, d.branch, d.tip, d.integration)
	case d.tip != "" && d.peerTip != "":
		return TaskShowDeliveryUndelivered, fmt.Sprintf(taskShowDeliveryPeerOthersFmt,
			d.branch, d.peerTip, d.tip, d.whereText())
	case d.tip != "" && d.integration != "":
		// A tip the recorded repo's integration branch already CONTAINS is not a
		// delivery DEBT, whatever the peer advertises for the branch name — and
		// this arm must sit BEFORE the plain "tip exists, peer has nothing" one,
		// which is exactly where this file's first draft got it wrong: driven
		// against this host's real ledger it reported two live rows
		// (01a17e03-12f0, 01a17dfd-09ef) as UNDELIVERED while their tips WERE
		// the integration tip. The measurement caught it the moment the line ran
		// on real data, which is the reason Rule 31 exists.
		return TaskShowDeliveryMergedLocally, fmt.Sprintf(taskShowDeliveryMergedLocallyFmt,
			d.target, d.branch, d.tip, d.integration)
	case d.recorded:
		// THE REVERSE ARM, and the reason this repair cannot become a blanket
		// pass (Rule 15 + the sibling arm's `!tipFromLandedRecord`): a record
		// that claims a push is NOT evidence the push landed. The tip it names
		// was sought in the recorded repo's integration refs and found in NONE
		// of them — so the delivery cannot be confirmed, and the line says what
		// the record claimed and which refs were asked. It must NOT fall through
		// to the "only in its own ground" sentence below: no directory was read,
		// so claiming one held the work would be a statement this reading never
		// observed.
		if !d.tipReadableInRepo {
			return TaskShowDeliveryUnchecked, fmt.Sprintf(taskShowDeliveryRecordUnreadableFmt, d.tip, d.repo)
		}
		return TaskShowDeliveryUnchecked, fmt.Sprintf(taskShowDeliveryRecordUnconfirmedFmt,
			d.tip, d.integrationAskedText())
	case d.tip != "":
		// The measured interrupted-run shape: the tip lives in the ground and
		// the peer has no such ref, and the integration branch does not contain
		// it either — the work exists in ONE directory in the world.
		// `taskShowDeliveryCloneOnlyFmt` names the clone only when the CLONE is
		// what answered (a linked worktree's refs are the shared repo's, so
		// "only in its own ground" would be false there — the shape decides it,
		// not this sentence).
		if d.groundIsClone() {
			return TaskShowDeliveryUndelivered, fmt.Sprintf(taskShowDeliveryCloneOnlyFmt,
				d.branch, d.tip, d.ref.where, d.repo, d.target)
		}
		return TaskShowDeliveryUndelivered, fmt.Sprintf(taskShowDeliveryGroundOnlyFmt,
			d.branch, d.tip, d.ref.where, d.target)
	case d.peerTip != "":
		return TaskShowDeliveryUnchecked, fmt.Sprintf(taskShowDeliveryPeerUnknownTipFmt,
			d.target, d.branch, d.peerTip, taskShowDeliveryNoLocalTip)
	default:
		// NOTHING NAMED ANYWHERE — and that is the UNCHECKED direction, not a
		// negative verdict (Rule 15). With no tip there is no question that
		// "did it leave" could be answered, so the line states the absence of a
		// READING and the places it asked — including the row's own landed
		// record, which was consulted too and named nothing — it never renders
		// this as "the row has no work", which no observation here supports.
		return TaskShowDeliveryUnchecked, fmt.Sprintf(taskShowDeliveryNothingFmt, d.branch,
			d.ref.siteText(), deliveryProofLandedWitnessText)
	}
}

// integrationAskedText is the refs the containment question could ask, as the
// UNCHECKED reading quotes them — and the explicit statement when the recorded
// repo holds none, because "asked nothing" and "asked and got no" are different
// readings (Rule 15).
func (d taskGroundDeliveryReading) integrationAskedText() string {
	if len(d.integrationAsked) == 0 {
		return "none — the recorded repo " + d.repo + " holds no integration ref, so containment could not be measured"
	}
	return strings.Join(d.integrationAsked, ", ")
}

// taskGroundDeliveryMergedInto answers, purely LOCALLY, whether the recorded
// repo's integration branch already contains tip — the ONE fact that turns "this
// branch never reached the peer" into "the work already had, under another
// name". It runs `git merge-base --is-ancestor` in the recorded repo (no fetch,
// no write, nothing into any tree) and answers "" whenever the question cannot
// be answered (no integration ref, a tip the repo does not carry, a wedged
// repo) — an unreadable ancestry is not evidence of a merge.
func taskGroundDeliveryMergedInto(ctx context.Context, repo, tip string) string {
	if strings.TrimSpace(repo) == "" || strings.TrimSpace(tip) == "" {
		return ""
	}
	for _, name := range taskGroundIntegrationRefsPresent(ctx, repo) {
		// `--is-ancestor` prints nothing: the ANSWER is the exit status, so the
		// parsed-stdout reader's error is the verdict (Rule 15 — no discarded
		// return; the empty stdout is not a value here).
		if _, err := finishCleanupGitParsedStdout(ctx, repo, finishCleanupGitTimeout,
			finishCleanupPreMergeMergeBaseVerb, finishCleanupPreMergeAncestorFlag, tip, name); err == nil {
			return name
		}
	}
	return ""
}

// taskGroundIntegrationRefsPresent lists, in the ONE order the close-out chain
// uses (finishCleanupIntegrationRefs), the integration refs this repo actually
// holds.
//
// WHY IT EXISTS AS ITS OWN FUNCTION (Rule 14): the ancestry readers need the
// same list, and an EMPTY list is NOT the same answer as "none of them
// contains the tip" — an empty list means nothing could be compared at all,
// which is an UNCHECKED reading rather than a negative verdict (Rule 15). Two
// call sites ask it: taskGroundDeliveryMergedInto (`task show`'s delivery
// line) and taskResumeDeliveryAxis (the resume gate's second axis). Sharing
// the enumeration is what stops them from disagreeing about which branch is
// "the integration branch".
func taskGroundIntegrationRefsPresent(ctx context.Context, repo string) []string {
	if strings.TrimSpace(repo) == "" {
		return nil
	}
	present := make([]string, 0, len(finishCleanupIntegrationRefs))
	for _, name := range finishCleanupIntegrationRefs {
		if _, ok := finishCleanupPreMergeRev(ctx, repo, name); ok {
			present = append(present, name)
		}
	}
	return present
}

// groundIsClone reports whether the row's own ground is a CLONE — its refs
// exist nowhere else, which is what makes "only in its own ground" a true
// sentence.
func (d taskGroundDeliveryReading) groundIsClone() bool {
	return d.ref.exists && d.ref.whereLabel() == string(devworkspace.GroundModelClone)
}

// whereText states where this row's tip was read, for the "peer holds a
// different commit" sentence (a reader must be able to see WHICH tree answered).
func (d taskGroundDeliveryReading) whereText() string {
	return fmt.Sprintf(taskShowDeliveryLocalTipFmt, d.tip, d.ref.where)
}

// localText is the local half quoted by an UNCHECKED reading: the tip and the
// directory it was found in, the record that named it when no directory could,
// or the explicit statement that none was readable.
func (d taskGroundDeliveryReading) localText() string {
	if d.tip != "" && d.recorded && strings.TrimSpace(d.ref.where) == "" {
		return fmt.Sprintf(taskShowDeliveryRecordTipLocalFmt, d.tip)
	}
	if d.tip == "" || strings.TrimSpace(d.ref.where) == "" {
		if len(d.ref.sites) > 0 {
			return taskShowDeliveryNoLocalTip + " (asked " + d.ref.siteText() + ")"
		}
		return taskShowDeliveryNoLocalTip
	}
	return d.whereText()
}
