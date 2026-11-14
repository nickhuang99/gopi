package tools

// task_resume_no_commit.go — THE RESUMED RUN THAT PRODUCED NO COMMIT DID NOT
// FINISH THE WORK, so its row must not read `completed`.
//
// THE DEFECT (epic resume-zero-commit, measured 2026-11-01). `gopi worker
// --resume <failed-row>` re-enters a row that already failed, and when the
// resumed run makes NO commit of its own the clone channel's delivery leg
// measures `count == 0` and — correctly, for an ORDINARY run — concludes
// "nothing to deliver" / "already on the integration branch". The close-out
// then writes the row `completed` on that measurement. For a RESUME that
// conclusion is the wrong claim: zero commits means the reason the row failed
// was never touched at all. "I was asked to produce nothing and produced
// nothing" and "I was asked to fix something and did not touch it" wear the same
// `count == 0` and are opposite facts about the work (Rule 15).
//
// WHY THIS IS NOT A SECOND CRITERION, AND WHY IT IS NARROW:
//
//	· the FACT it tests already exists — the delivery leg's own classification
//	  that this run produced no commit of its own (the empty-ground blocker and
//	  its inherited-delivery sibling). No git call, no reflog reader, no second
//	  opinion about the ground (Rules 8/13/14).
//	· the SUBJECT it tests is `tools.TaskRunResumeFrom()` — the ONE reader of the
//	  door's resume declaration (Rule 21). Empty means "no resume was declared",
//	  and every ordinary run therefore keeps its pre-existing behaviour byte for
//	  byte: a TOO_BIG reframe row, a chat ground and a counter-proof run are
//	  still `completed` on the channel's measurement, which is what rows
//	  01a18244/01a1907d fixed and what this must not undo.
//	· it does not touch the DELIVERY at all. Nothing is pushed, nothing is
//	  reclaimed differently, no round is spent. It changes ONE claim — the status
//	  a zero-commit resume closes with — and the sentence that explains it.
//
// A DIRTY GROUND IS NOT THIS CASE and never reaches here: uncommitted work makes
// the attempt return a DIRTY blocker, which is a refused delivery (a different
// exit entirely). But the guard is stated anyway, because a future reader will
// ask: a pending change is work a delivery round can still turn into a commit,
// so it can never be read as "nothing happened".

import (
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/devworkspace"
)

// ResumeNoCommitToken is the ONE spelling of the conclusion, asserted by the
// tests on both faces (Rule 1b/14): an operator scanning `task list` reads the
// same words the row's reason carries.
const ResumeNoCommitToken = "this resume produced NO COMMIT"

// resumeNoCommitReasonFmt composes the row's WHY. It states, in the order a
// reader needs them: what was measured (no commit of its own), what that means
// for THIS row (the failure the resume was dispatched to repair is untouched),
// and the channel's own measurement verbatim, so the two facts are never
// confused and neither is paraphrased (Rule 14).
const resumeNoCommitReasonFmt = ResumeNoCommitToken + " — this resumed run made no commit of its own, so %s " +
	"was NOT TOUCHED by this resume: whatever the row failed on is exactly as it was left. This is not " +
	"`completed`, because a resume that changed nothing has not repaired anything. Re-dispatch it with the " +
	"failure re-read, or close it `failed` with why. The delivery leg measured: %s"

// ResumeNoCommitVerdict answers the ONE question this file exists for, and it is
// asked at the close-out of every run that took the clone ground:
//
//	resumedFrom != "" (a resume was DECLARED) AND producedNoCommit (the delivery
//	leg measured that this run made no commit of its own)
//	    → (true, the composed reason): the status must NOT be `completed`.
//	anything else → (false, ""): the caller keeps its existing decision.
//
// `producedNoCommit` is the caller's classification of the delivery outcome, and
// it must be the measured one — `NothingToDeliver` (the ground carries no commit
// past the integration point) or `InheritedDelivery` (the content this ground was
// handed is already on the base). `AlreadyDelivered` is deliberately NOT one of
// them: that arm means this run's own push landed, i.e. it DID produce a commit.
//
// `taskID` is used only to name the row in the composed sentence; a blank id
// still yields the verdict (the caller's arm already said it wrote no row).
func ResumeNoCommitVerdict(resumedFrom, taskID, channelReason string, producedNoCommit bool) (bool, string) {
	if strings.TrimSpace(resumedFrom) == "" || !producedNoCommit {
		return false, ""
	}
	measured := devworkspace.CloneChannelBody(channelReason)
	if measured == "" {
		measured = devworkspace.CloneChannelBody(taskNothingToDeliverNoMeasurement)
	}
	subject := "this row"
	if id := strings.TrimSpace(taskID); id != "" {
		subject = "row " + id
	}
	reason := fmt.Sprintf(resumeNoCommitReasonFmt, subject, measured)
	// The channel's own composition site is used, never hand-pasted (Rule 14):
	// "how many times does `clone channel: ` appear" is decided there, so this
	// file cannot drift from the other faces that speak the channel's lines.
	return true, devworkspace.CloneChannelReason(reason)
}
