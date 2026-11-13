package tools

// THE DELIVERY AXIS OF THE RESUME GATE.
//
// THE DEFECT THIS FILE EXISTS FOR (measured 2026-10-30, this host). The resume
// gate judged ONE axis — the row's ledger status — so a `completed` row read
// "this work is FINISHED" no matter what its branch held. Fifteen commits were
// sitting in three such rows, each with a clean worktree, a live ground and a
// tip that is an ancestor of NOTHING:
//
//	$ gopi task status 01a18799-3304-7c30-b50f-8feb76908c8b ; echo $?
//	status:      completed
//	resumable:   no
//	5
//	$ git -C ~/workspace/01a18799-3304-7c30-b50f-8feb76908c8b rev-parse HEAD
//	d7d3c920adef797edcbaf42958c5e5878cfa42d4
//	$ git -C ~/workspace/01a18799-3304-7c30-b50f-8feb76908c8b merge-base --is-ancestor d7d3c92 master ; echo $?
//	1                    ← likewise origin/master, and likewise the branch's own peer ref
//
// The same measurement held for 01a18ea5 (tip 0c469ca2) and 01a18f4a (tip
// a9bf8eaf), whose branches the recorded repo does not hold at all. The status
// was true and the CONCLUSION drawn from it was false: "finished" is a claim
// about the WORK, and a status column cannot make that claim alone. The second
// axis is the one that can — whether that work reached the integration branch —
// and the machinery to read it already exists in this package (the same tip
// resolution and ancestry comparison `task show`'s "delivery (read now):" line
// and the delivery-proof arm already use). This file adds no new git logic; it
// asks the existing readings the one question the gate never asked.
//
// THE THREE ANSWERS AND WHAT EACH DOES (this is the whole design):
//
//	LANDED    → carry on refusing. The work left this machine; a resume would
//	            duplicate delivered work. This is exactly today's behaviour, and
//	            it is now EARNED rather than assumed from a status column.
//	UNLANDED  → ADMIT. The row's own commit is contained in no integration ref
//	            the recorded repo holds: there IS unfinished work here, whatever
//	            the status says. This is the population the epic exists for.
//	UNKNOWN   → carry on REFUSING, and say so with the unreadable axis NAMED.
//
// WHY UNKNOWN REFUSES. The tempting reading — "I could not tell, so let it
// through in case" — is WRONG here, and the wrongness is measured rather than
// aesthetic: the rows it would admit are the ones with NO deliverable record at
// all (a row with no branch, a row whose ground and refs were reclaimed), and
// that is the `completed`-row population whose admission was itself the defect
// an earlier fix closed — `gopi worker --resume` rerunning a finished row as if
// it were an interrupted one, duplicating work item by item. So the rule is:
// an UNLANDED verdict is EVIDENCE of work still owed, and an UNKNOWN verdict is
// the ABSENCE of evidence — and the two get opposite verdicts because the gate
// admits on evidence, never on its lack.
//
// WHY UNKNOWN IS STILL LOUD, WITHOUT WRITING: the answer rides the row's own
// status view (`delivery: UNKNOWN — …`, printed by `gopi task status` beside
// `resumable:`), and the refusal sentence quotes the same clause, so every face
// that could act on the row states which axis it could not read. The rejected
// alternative was a trail record on the ledger: `taskProcessStatus` is reached
// from the INSPECTION verb, and a read verb that writes is a writer in
// disguise — it would put a write-lock acquisition behind every `task status`
// of a `completed` row, and it would make "who touched this ledger" unanswerable
// for a call nobody thinks of as a mutation. Rule 15 asks for a LOUD unknown,
// not for a persisted one; the loudness is the sentence, and it is on every
// surface that takes the decision.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// The axis' verdict words (Rule 1: a value a surface branches on is a named
// constant, never a literal at the render site).
const (
	// TaskResumeDeliveryLanded — the row's tip IS an ancestor of one of the
	// recorded repo's integration refs. The only positive this axis makes.
	TaskResumeDeliveryLanded = "LANDED"
	// TaskResumeDeliveryUnlanded — the tip was read and is contained in NO
	// integration ref the repo holds. A debt, and the reason a `completed` row
	// may be resumed.
	TaskResumeDeliveryUnlanded = "UNLANDED"
	// TaskResumeDeliveryUnknown — an axis the verdict needs could not be read.
	// NOT a soft "no": the why names what was unreadable.
	TaskResumeDeliveryUnknown = "UNKNOWN"
)

// The WHY spellings, named once (Rule 1b) so the renderer and the tests address
// the same sentences instead of retyping them.
const (
	// taskResumeDeliveryNoRecordFmt — the row records no <field>, so there is
	// no delivery question to answer at all.
	taskResumeDeliveryNoRecordFmt = "this row records no deliverable %s, so there is no branch to compare with the integration branch"
	// taskResumeDeliveryNoTipFmt — the branch, and the places asked.
	taskResumeDeliveryNoTipFmt = "the row's branch %s could not be read in any place this ground can hold it (asked %s), so no tip can be named"
	// taskResumeDeliveryNoRepoDirFmt — the recorded repo resolves to no
	// directory this host can ask (a reclaimed ground, a foreign checkout).
	taskResumeDeliveryNoRepoDirFmt = "the recorded repo %s is not a directory this host can ask, so the integration branch cannot be read"
	// taskResumeDeliveryLandedFmt — tip, integration ref, repo.
	taskResumeDeliveryLandedFmt = "the row's tip %s is already on %s of %s — the work LEFT this machine, so there is nothing for a resume to continue"
	// taskResumeDeliveryUnlandedFmt — tip, the refs asked, repo.
	taskResumeDeliveryUnlandedFmt = "the row's tip %s is contained in none of the integration refs this repo holds (%s in %s) — the work never landed, so it is NOT finished however the status column reads"
	// taskResumeDeliveryNoIntegrationFmt — the repo whose integration refs were
	// all unresolvable.
	taskResumeDeliveryNoIntegrationFmt = "none of the integration refs exist in %s, so there is no branch to compare the row's tip against"
)

// taskResumeDeliveryVerdict is the second axis' answer: a word, the evidence
// behind it, and the tip it judged ("" when none could be named).
type taskResumeDeliveryVerdict struct {
	State string
	Why   string
	Tip   string
}

// taskResumeDeliveryOverrideCaveatFmt is the sentence an ADMITTED `completed`
// row carries when the admission came from the DELIVERY axis rather than from
// the status: it names the status that would have refused and quotes the
// measurement that overrode it. A `resumable: yes` on a `completed` row is a
// surprising answer, and a surprising answer without its evidence is
// indistinguishable from a bug (Rule 15).
const taskResumeDeliveryOverrideCaveatFmt = "%s is a claim about the ROW; the WORK was measured separately and it never landed — %s. " +
	"Resuming continues this row's own unfinished delivery; if you believe the row really is finished, check the branch and the integration ref by hand before you resume"

// taskResumeDeliveryOverrideCaveat renders that sentence for one row.
func taskResumeDeliveryOverrideCaveat(status string, d taskResumeDeliveryVerdict) string {
	return fmt.Sprintf(taskResumeDeliveryOverrideCaveatFmt, status, d.Why)
}

// taskResumeDeliveryStatusMayBeOverridden reports whether the delivery axis may
// override this status at all.
//
// ONLY THE TWO STATUSES THAT CLAIM THIS ROW'S OWN DELIVERY ARE IN IT:
// `completed` and `verified` both say "the work of THIS row is done", which is
// precisely the claim the delivery axis can test. `superseded` is DELIBERATELY
// OUT — it says the row was retired IN FAVOUR OF A SUCCESSOR, so the work lives
// in another row's delivery, and overriding it here would run the same work
// twice under two ids. A status this vocabulary does not know is out for the
// obvious reason: nothing here can say what its "finished" means.
func taskResumeDeliveryStatusMayBeOverridden(status string) bool {
	return status == TaskStatusCompleted || status == TaskStatusVerified
}

// carriesUnfinishedWork reports whether this verdict is EVIDENCE that the row's
// work never reached the integration branch — the one condition under which a
// status that reads "finished" does not close the resume gate.
//
// LANDED and UNKNOWN both answer false, for OPPOSITE reasons stated in this
// file's header: one proves the work left, the other proves nothing.
func (v taskResumeDeliveryVerdict) carriesUnfinishedWork() bool {
	return v.State == TaskResumeDeliveryUnlanded
}

// admitsByDelivery is the WHOLE admission rule in one place: admitted iff the
// status claims this row's own delivery AND the tip is contained in no
// integration ref. Both halves matter — the first keeps `superseded` out, the
// second keeps UNKNOWN out — and stating them together is what stops a caller
// from implementing half of it (this function's first draft did: it asked the
// verdict alone, which would have admitted a superseded row whose reissue had
// simply not landed yet).
func taskResumeDeliveryAdmits(status string, d taskResumeDeliveryVerdict) bool {
	return taskResumeDeliveryStatusMayBeOverridden(status) && d.carriesUnfinishedWork()
}

// taskResumeDeliveryAxis reads, for ONE row, whether the commit its branch
// carries is an ancestor of the recorded repo's integration branch.
//
// It takes the *Task the caller already read, because the status view builds one
// of these per row and a second ledger read per row would be a second answer to
// the same question (Rules 13/14).
func taskResumeDeliveryAxis(t *Task) taskResumeDeliveryVerdict {
	if t == nil {
		return taskResumeDeliveryVerdict{State: TaskResumeDeliveryUnknown,
			Why: fmt.Sprintf(taskResumeDeliveryNoRecordFmt, "blob")}
	}
	var spec TaskDeliverableSpec
	if err := json.Unmarshal([]byte(t.Deliverable), &spec); err != nil {
		return taskResumeDeliveryVerdict{State: TaskResumeDeliveryUnknown,
			Why: fmt.Sprintf(taskResumeDeliveryNoRecordFmt, "blob (it is not readable as one)")}
	}
	repo := strings.TrimSpace(spec.Repo)
	branch := strings.TrimSpace(spec.Branch)
	worktree := strings.TrimSpace(spec.Worktree)
	switch {
	case repo == "":
		return taskResumeDeliveryVerdict{State: TaskResumeDeliveryUnknown,
			Why: fmt.Sprintf(taskResumeDeliveryNoRecordFmt, "repo")}
	case branch == "":
		return taskResumeDeliveryVerdict{State: TaskResumeDeliveryUnknown,
			Why: fmt.Sprintf(taskResumeDeliveryNoRecordFmt, "branch")}
	}
	ctx, cancel := context.WithTimeout(context.Background(), branchDeleteGitTimeout)
	defer cancel()
	// THE TIP: the same resolution `task show` uses (taskGroundDeliveryTip over
	// the same site order), so the two faces cannot name different commits for
	// one row.
	ref := taskGroundBranchRefIn(repo, worktree, branch)
	tip := taskGroundDeliveryTip(ctx, ref)
	if tip == "" {
		return taskResumeDeliveryVerdict{State: TaskResumeDeliveryUnknown,
			Why: fmt.Sprintf(taskResumeDeliveryNoTipFmt, branch, ref.siteText())}
	}
	mainDir := finishCleanupMainRepoDir(repo)
	if strings.TrimSpace(mainDir) == "" {
		return taskResumeDeliveryVerdict{State: TaskResumeDeliveryUnknown, Tip: tip,
			Why: fmt.Sprintf(taskResumeDeliveryNoRepoDirFmt, repo)}
	}
	// THE COMPARISON: every integration ref this repo holds, asked through the
	// ONE enumeration the delivery reading shares
	// (taskGroundIntegrationRefsPresent) and in the ONE order the close-out
	// chain uses — so the resume gate, `task show`'s delivery line and the
	// close-out can never disagree about which branch is "the integration
	// branch", nor about what an empty list MEANS.
	present := taskGroundIntegrationRefsPresent(ctx, mainDir)
	for _, name := range present {
		// `--is-ancestor` prints nothing: the ANSWER is the exit status, so the
		// parsed-stdout reader's error IS the verdict (Rule 15 — the empty stdout
		// is not a value here, and the return is not discarded).
		if _, err := finishCleanupGitParsedStdout(ctx, mainDir, finishCleanupGitTimeout,
			finishCleanupPreMergeMergeBaseVerb, finishCleanupPreMergeAncestorFlag, tip, name); err == nil {
			return taskResumeDeliveryVerdict{State: TaskResumeDeliveryLanded, Tip: tip,
				Why: fmt.Sprintf(taskResumeDeliveryLandedFmt, tip, name, mainDir)}
		}
	}
	if len(present) == 0 {
		// No integration ref exists: measuring nothing is not measuring "not
		// landed" (Rule 15 — a scan that found nothing cannot pass as a clean
		// one). UNKNOWN, with the directory that was asked.
		return taskResumeDeliveryVerdict{State: TaskResumeDeliveryUnknown, Tip: tip,
			Why: fmt.Sprintf(taskResumeDeliveryNoIntegrationFmt, mainDir)}
	}
	return taskResumeDeliveryVerdict{State: TaskResumeDeliveryUnlanded, Tip: tip,
		Why: fmt.Sprintf(taskResumeDeliveryUnlandedFmt, tip, strings.Join(present, ", "), mainDir)}
}

// TaskResumeDeliveryAxisFor is the row-id form of the axis — the one the wrapper
// face uses, which holds an id and not a *Task. It reads the row through the one
// read-only opener, and an unreadable row is UNKNOWN (never a silent "landed",
// which would refuse a resume on the strength of a failed read).
func TaskResumeDeliveryAxisFor(taskID string) taskResumeDeliveryVerdict {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return taskResumeDeliveryVerdict{State: TaskResumeDeliveryUnknown,
			Why: fmt.Sprintf(taskResumeDeliveryNoRecordFmt, "row id")}
	}
	ctx := context.Background()
	db, err := openTasksDBReadOnly(ctx, memoryDBPath())
	if err != nil {
		return taskResumeDeliveryVerdict{State: TaskResumeDeliveryUnknown,
			Why: fmt.Sprintf(taskResumeDeliveryNoRecordFmt, "row (the ledger could not be opened: "+err.Error()+")")}
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil || t == nil {
		return taskResumeDeliveryVerdict{State: TaskResumeDeliveryUnknown,
			Why: fmt.Sprintf(taskResumeDeliveryNoRecordFmt, "row")}
	}
	return taskResumeDeliveryAxis(t)
}
