package tools

// task_deliverable_tip.go — THE DELIVERABLE-TIP REACHABILITY CRITERION AT
// CLOSE-OUT: a row may claim `completed` only with its own deliverable still
// reachable from SOME ref.
//
// INCIDENT (epic 01a13bbb, measured 2026-10-15). A real ledger row
// (01a13ae1-2477-7490-b79b-24b8b8b2b705) reads `completed`, its recorded
// `deliverable.branch` is `task/01a13ae124777490b79b24b8b8b2b705`, and that
// branch's tip is reachable from NO ref on either host:
//
//	$ git cat-file -t 6446222744…                → commit     (the object is ALIVE)
//	$ git for-each-ref --contains 6446222744…    → (empty)    (no ref holds it)
//	$ git rev-parse --verify --quiet refs/heads/task/01a13ae1…  → rc=1 (the ref is gone)
//	$ ssh zpeer 'git for-each-ref --contains 6446222744…'       → (empty)
//	$ git merge-base --is-ancestor 6446222744… master           → rc=1
//
// The removal-audit sink has FOUR records naming that branch, and the two
// branch-delete ones (`finish-cleanup-branch` @18:24:43 and
// `finish-cleanup-branch-force` @18:24:50) carry sha=6446222744… with
// delivery_point=zpeer:~/work/GoPi — i.e. the close-out chain force-deleted the
// ref on the strength of a push whose landing the delivery point does not
// actually hold. The row was then written back `completed` HOURS LATER with
// nothing left holding its work: the loss is SILENT, because every surface that
// renders a terminal row renders this one exactly like a delivered one.
//
// WHAT THIS ARM ADDS. One criterion, at the ONE choke point every harness
// close-out passes through (taskUpdateStampedCore's delivery state gate, the
// same seam the run-residue arm uses), for a row that records a
// `deliverable.branch`:
//
//	is that branch's tip still reachable from some ref — the local repo's refs,
//	or the delivery point the removal-audit sink recorded for it?
//
// Reachability, not existence: a tip on another ref's ancestry (a merge commit
// on the integration branch, a topic branch that picked it up) is reachable, so
// the overwhelming majority of legitimate close-outs — the branch is still
// there, or its work landed and the ref was consumed — are SILENT. The measured
// false-positive class is the whole reason the answer is ancestry and not
// `for-each-ref --contains <branch>`: the census reader in
// task_ref_reachability.go found 1184 of 1214 unresolved rows accounted for by
// reachable history, and a gate that called those defects would be noise.
//
// IT REUSES THE BRANCH-DELETE GUARD'S READS (Rule 35 — no second mechanism):
// branchDeleteTipOf resolves a ref, branchDeleteListRefs enumerates the ref
// namespace once, branchDeleteContainingRefs applies git's own ancestry test,
// branchDeleteDurableHolders drops the pushback lane's per-pass mirrors, and
// branchDeleteDeliveryPointHoldsTip asks the delivery over the channel the
// delivery itself wrote through (`ls-remote`). The tip's FALLBACK when the ref
// is already gone is the sha the removal-audit sink recorded at the delete —
// the ONE fact a recovery wants, and the reason the audit sink exists.
//
// WHAT IT DOES NOT DO. It deletes nothing, restores nothing, pushes nothing and
// never moves a ref: an unreachable tip is RECORDED on the row's own trail (an
// attributed task_event naming the tip) and printed on stderr; the operator's
// switch (deliverableTipUnreachableEnv) may additionally REFUSE the `completed`
// writeback, leaving the row OPEN — the same record-then-refuse ladder, and the
// same knob shape, as the run-residue arm. An unreadable repository, or a tip
// nobody can name, is reported as UNCHECKED and never rendered as "no ref
// holds it" (Rule 15).

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/removalaudit"
)

// The criterion's one event name and its ONE reader of the operator switch
// (Rules 1b/2). The event deliberately does NOT share the run-residue family's
// `completed-unclean` stem: that stem is a PREFIX MATCH for the residue arm's
// own idempotence guard (reclaimRunResidueFiled), so borrowing it would make
// this record suppress an unrelated one.
const (
	// deliverableTipProducedNothingEvent is the trail record for the OTHER
	// conclusion. A different name on purpose: `deliverableTipFiled` asks whether
	// the UNREACHABLE record exists to stay idempotent, and a produced-nothing
	// record that borrowed that stem would make a later real loss look already
	// filed (the same prefix-matching trap the residue family documents).
	deliverableTipProducedNothingEvent = "deliverable-produced-nothing"
	// deliverableTipUnreachableEvent is the attributed trail record.
	deliverableTipUnreachableEvent = "deliverable-tip-unreachable"
	// deliverableTipUnreachableRefusedEvent is the BINDING verdict's record, so
	// "recorded" and "recorded AND the writeback was stopped" stay different
	// facts about a row (the dirty-close-out family's own distinction).
	deliverableTipUnreachableRefusedEvent = deliverableTipUnreachableEvent + "-refused"
	// deliverableTipUnreachableEnv turns the record into a REFUSAL. Default OFF:
	// the standing contract is the record.
	deliverableTipUnreachableEnv = "GOPI_TASK_DELIVERABLE_TIP_ENFORCE"
	// deliverableTipUnreachableMarker is the phrase the sentence carries, so an
	// operator (or a test) matches it without knowing the words around it.
	deliverableTipUnreachableMarker = "its deliverable's tip is reachable from no ref"
)

// The verdicts. `unreachable` is the defect class; `reachable` and `settled`
// are the two shapes a healthy close-out wears, `no-branch` is the control case
// (a row that records no branch is not judged at all), and `unchecked` is the
// honest non-answer (Rule 15: a reader that could not look must say so).
const (
	deliverableTipVerdictNoBranch    = "no-branch"
	deliverableTipVerdictReachable   = "reachable"
	deliverableTipVerdictSettled     = "settled"
	deliverableTipVerdictUnreachable = "unreachable"
	deliverableTipVerdictUnchecked   = "unchecked"
	// deliverableTipVerdictProducedNothing is the SIXTH verdict, and it is not a
	// weaker `unreachable` — it is the opposite fact: the row's branch resolves to
	// no ref because the run never made a commit of its own, so there is nothing
	// that went missing. Its own value so no caller can fold the two together
	// (Rule 15: one fact, one name — the cost of the fold is measured on row
	// 01a18d61-509f, whose result told a later reader to hunt for a commit the
	// run never made).
	deliverableTipVerdictProducedNothing = "produced-nothing"
)

// deliverableTipOutcome is ONE row's reading, carrying the measurements behind
// the verdict so a reviewer can re-check it without re-running the arm.
type deliverableTipOutcome struct {
	Verdict string
	TaskID  string
	Repo    string
	Branch  string
	// Ground is the row's recorded deliverable.worktree — the directory the
	// produced-nothing witnesses have to read (a branch's reflog, a commit's
	// stamps). "" once the channel has reclaimed it, which is why the second
	// witness needs no directory at all.
	//
	// It is ALSO the first directory the delivery DESTINATION is read in: a
	// clone ground's own `refs/remotes/origin/<base>` is the tracking ref the
	// channel's push advances, and the measured incident's row recorded its
	// ground while the arm was judging it.
	Ground string
	// Tip is the commit the branch pointed at — resolved from the live ref when
	// it still exists, else from the removal-audit record of its deletion ("" =
	// nobody kept it, which the note says out loud).
	Tip           string
	DeliveryPoint string
	// Holders are the refs that still contain the tip (after the non-durable
	// mirrors are dropped) — non-empty on the `reachable` verdict.
	Holders []string
	// AuditPath is the removal-audit sink this run actually read. It is carried
	// (and rendered in the note) because "the sink holds no record for this
	// branch" and "we read a different file than the writer wrote" are different
	// findings, and only the path tells them apart (Rule 15).
	AuditPath string
	// AuditRead is false when the sink could not be read at all.
	AuditRead bool
	// Destination names the delivery destination that CARRIED the tip, when one
	// did — "<ref> (<dir>)". It is the difference between "some local ref holds
	// it" and "the point this row delivers TO holds it", which is the finding a
	// reviewer re-checks first.
	Destination string
	Note        string
}

// TaskDeliverableTipReachable judges the row's recorded deliverable branch and
// returns what it found. It is exported so the evidence can be re-taken from a
// test or a session (the criterion's own readers), never to be called as a
// policy by something other than the close-out seam.
//
// A row with no recorded branch is NOT judged (deliverableTipVerdictNoBranch):
// the criterion is a statement about a claim that names a ref, and a row that
// names none makes no such claim — the control case the epic's acceptance
// requires to pass untouched.
func TaskDeliverableTipReachable(taskID string) (deliverableTipOutcome, error) {
	out := deliverableTipOutcome{TaskID: taskID, Verdict: deliverableTipVerdictNoBranch}
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return out, fmt.Errorf("deliverable tip: task_id must not be empty")
	}
	repo, branch, ground := taskRowDeliverable(taskID)
	repo = strings.TrimSpace(repo)
	branch = strings.TrimSpace(branch)
	out.Repo, out.Branch, out.Ground = repo, branch, strings.TrimSpace(ground)
	if branch == "" {
		return out, nil // the control case: nothing named, nothing judged
	}
	if repo == "" {
		out.Verdict = deliverableTipVerdictUnchecked
		out.Note = deliverableTipUncheckedNote(taskID, branch, "the row records no repository to ask")
		return out, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()

	// ① THE LIVE REF. A branch that still resolves is the ordinary mid-flight
	// close-out (the ref is removed by finish-cleanup's step ④, AFTER the
	// writeback), and it is reachable by definition — its own ref holds it.
	tip := branchDeleteTipOf(ctx, repo, branch)
	if tip != "" {
		out.Tip = tip
		out.Verdict = deliverableTipVerdictReachable
		return out, nil
	}
	// ①b THE GROUND'S HEAD — the OTHER witness that needs no record (measured on
	// the epic's own false positive). A CLONE ground that is still standing
	// carries the run's tip at HEAD whether or not the row's ADDRESS ref was
	// ever created: `devworkspace` cuts the ground, the run commits there, and
	// the channel publishes `HEAD:refs/heads/<integration>`. A run whose address
	// ref never existed therefore had NO subject to look for, and the arm fell
	// through to "no removal-audit record kept the tip" — a statement about the
	// sink, rendered as a statement about the work.
	//
	// It is the same witness the sibling arm reads first (deliveryProofTipOf),
	// and it is read only when the address ref did not resolve, so the ordinary
	// mid-flight close-out above is untouched.
	if out.Ground != "" {
		out.Tip = deliveryProofGitRead(ctx, out.Ground, "rev-parse", "HEAD")
	}
	// ② THE AUDIT SINK — the tip the delete recorded, keyed by the branch. This
	// is the ONE artefact that knows what the ref pointed at, and reading it is
	// the whole reason the sink is written. A sink that cannot be read is NOT
	// "no record exists": the tip stays unknown and the verdict says unchecked
	// rather than inventing an orphan.
	auditReadOK := false
	if path, _ := removalaudit.SinkPath(); path != "" {
		out.AuditPath = path
		if recs, err := removalaudit.BranchDeleteRecords(path); err == nil {
			auditReadOK = true
			out.AuditRead = true
			for _, rec := range recs[branch] {
				if out.Tip == "" && strings.TrimSpace(rec.Sha) != "" {
					out.Tip = strings.TrimSpace(rec.Sha)
				}
				if out.DeliveryPoint == "" && strings.TrimSpace(rec.DeliveryPoint) != "" {
					out.DeliveryPoint = strings.TrimSpace(rec.DeliveryPoint)
				}
			}
		}
		// ②b THE GROUND-DELETION RECORD, the same sink's OTHER kind (measured
		// 2026-10-29). This reading is what `gopi task show` prints as
		// "delivery (read now)", and it had exactly the gap the delivery proof
		// had: a CLONE ground is reclaimed with os.RemoveAll, not branch-deleted,
		// so nothing in `kind=branch-delete` ever named its tip. Two rows
		// (01a17de7-fe23 / 01a17de9-e772) rendered UNCHECKED for that reason
		// alone while their commits were ancestors of origin/master. The record
		// now carries the tip; this asks it — through the SAME function the
		// proof uses, so the two readings cannot drift.
		if out.Tip == "" {
			out.Tip = deliveryProofGroundRemovalTip(branch, taskID)
		}
	}
	// ②c THE ROW'S OWN LANDED RECORD — the clone channel's statement that it
	// pushed THIS branch at THIS tip (TaskDeliveryRecordLanded). It is the last
	// witness and the only one that survives EVERY form of cleanup: a reclaimed
	// clone ground leaves no branch-delete record (the ground is removed whole,
	// `os.RemoveAll`) and the channel's own row may carry no sha yet, so the
	// tip the arm needs exists ONLY here. Measured on the incident: the e2e log
	// holds `delivery delivered — pushed master to origin` while the arm said
	// "no removal-audit record kept the tip", i.e. it had no subject to look
	// for. The reader is the sibling arm's (deliveryProofLandedTip), so the two
	// arms cannot disagree about which commit a row delivered.
	//
	// IT DOES NOT BY ITSELF MAKE THE ROW REACHABLE: `fromLandedRecord` is not a
	// containment answer, and every containment arm still has to find the tip in
	// a ref. "The channel said it pushed X" is a statement about what was SENT.
	if out.Tip == "" {
		out.Tip = deliveryProofLandedTip(taskID, branch)
	}
	// ③ THE LOCAL REF NAMESPACE — ONE enumeration per directory that can answer,
	// then git's own ancestry test per ref (the guard's reads, reused). An
	// unreadable namespace is reported, never read as "no ref holds it"
	// (Rule 15) — that is the one direction that would make this arm dangerous.
	//
	// THE GROUND IS ASKED TOO (epic 01a19433). It is the directory the run's work
	// was actually made in, and the only one whose refs the channel's own push
	// touches — so on the clone channel it, not the recorded repo, is where a
	// delivered tip is reachable. Reading it does not widen the verdict: a ref is
	// a ref, and the tip must still be contained (never "the directory exists").
	// ONE directory is the ordinary case (the recorded repo, or a ground that IS
	// the repo). A directory whose refs cannot be read is SKIPPED rather than
	// turned into an abstention while another one can still answer, and the arm
	// reports `unchecked` only when NO namespace could be read at all — the
	// pre-existing contract, now measured per directory.
	dirs := deliverableTipHolderDirs(out, repo)
	readableDirs := 0
	var listErr error
	for _, dir := range dirs {
		refs, err := branchDeleteListRefs(ctx, dir)
		if err != nil {
			listErr = err
			continue
		}
		readableDirs++
		if out.Tip == "" {
			continue
		}
		holders := branchDeleteDurableHolders(branchDeleteContainingRefs(ctx, dir, out.Tip, branch, refs))
		if len(holders) > 0 {
			out.Holders = holders
			// A holder that IS a delivery destination is named as one: "some ref
			// holds it" and "the ref this row delivers TO holds it" are the same
			// measurement with different weights, and the reviewer re-checks the
			// second first.
			out.Destination = deliverableTipDestinationAmong(holders, dir)
			out.Verdict = deliverableTipVerdictReachable
			return out, nil
		}
	}
	if readableDirs == 0 {
		out.Verdict = deliverableTipVerdictUnchecked
		out.Note = deliverableTipUncheckedNote(taskID, branch,
			"the repository's refs could not be read ("+listErr.Error()+")")
		return out, nil
	}
	if out.Tip != "" {
		// ④ THE DELIVERY POINT — the ref namespace of the machine the close-out
		// chain pushed to. Asked over `ls-remote`, the read side of the very
		// channel the push wrote through; unreadable or holding a different tip
		// is no evidence, which is the fail-closed direction.
		if out.DeliveryPoint != "" {
			if branchDeleteDeliveryPointHoldsTip(ctx, repo, out.DeliveryPoint, branch, out.Tip) {
				out.Verdict = deliverableTipVerdictReachable
				return out, nil
			}
		}
		// ④b THE DELIVERY DESTINATION — the ref the row's work is DELIVERED TO,
		// which on the clone channel is NOT the ref the row records.
		//
		// THE FALSE POSITIVE THIS ARM EXISTS FOR (epic 01a19433, measured twice).
		// The clone channel publishes `HEAD:refs/heads/<integration>`
		// (devworkspace.publishGround), while `deliverable.branch` is the run's
		// ADDRESS (`task/<id>`) — so every ref-shaped read above that asks for
		// `refs/remotes/origin/<branch>` asks about a ref the channel NEVER
		// pushes, while the work sits on `refs/remotes/origin/master`. Measured
		// inside a real `gopi worker --resume` e2e whose own log carries BOTH
		// `clone channel: delivery delivered — pushed master to origin after 1
		// attempt(s)` and `AFTER: source master = "858b4f5 …"`, and yet the row
		// was reported with this arm's sentence:
		//
		//	branch task/tworkerresumedeliverye2e resolves to no ref, no delivery
		//	point was recorded for it, and no reachable commit names this task id
		//
		// The sibling delivery-proof arm answers UNCHECKED for the same fact
		// (deliveryProofIntegrationDestination, added for rows 01a18799 /
		// 01a18ea5) — an arm that reports "somebody's tip is at risk" while
		// another arm reports "I could not see it" is the two answers disagreeing
		// about one fact, which is the shape Rule 15 forbids.
		//
		// WHAT KEEPS IT FROM BEING "DELETE THE CHECK" (the arm's ONLY job is to
		// catch a run whose work exists in NO ref):
		//
		//	· the destination is a REF, and the tip must be contained in it — by
		//	  ancestry or by patch equivalence (deliveryContentAncestryOrPatch,
		//	  the sibling's own leg, reused rather than re-spelled). A destination
		//	  that merely EXISTS proves nothing, so a run whose commit never
		//	  reached master still falls through to `unreachable`.
		//	· it is asked in the directories that can answer for that ref — the
		//	  row's GROUND first (the clone channel's own tracking ref, refreshed
		//	  by the very push that delivered), then the recorded repo. The same
		//	  order the sibling uses, and for the same measured reason.
		//	· nothing is fetched, pushed or moved: every read is rev-parse /
		//	  merge-base / cherry over refs that already resolve.
		if dest, ok := deliverableTipDestinationCarries(ctx, out, branch); ok {
			out.Destination = dest
			out.Verdict = deliverableTipVerdictReachable
			return out, nil
		}
	}
	// ⑤ REACHABLE HISTORY — the merger's own subject names the row when the work
	// landed (`Merge … (re-lands 01a12ff2)`), so a ref that was consumed after a
	// successful landing is accounted for. ONE `git log --all` (the census
	// reader's own index), and an unreadable object store is reported rather
	// than read as "nothing names this row".
	named, _, herr := taskRefNamedTaskIDs(ctx, repo)
	if herr != nil {
		out.Verdict = deliverableTipVerdictUnchecked
		out.Note = deliverableTipUncheckedNote(taskID, branch,
			"the reachable history could not be read ("+herr.Error()+")")
		return out, nil
	}
	if taskRefHistoryNames(named, taskID) {
		out.Verdict = deliverableTipVerdictSettled
		return out, nil
	}
	if !auditReadOK && out.Tip == "" {
		out.Verdict = deliverableTipVerdictUnchecked
		out.Note = deliverableTipUncheckedNote(taskID, branch,
			"the removal-audit sink could not be read, so the tip the branch pointed at is unknown")
		return out, nil
	}
	// ⑥ THE PRODUCED-NOTHING GATE — asked AFTER every reachability arm has said
	// no, and BEFORE the defect verdict may be written (bug row
	// 01a1907d-family, measured 2026-10-31 on 01a18d61-509f: a TOO_BIG reframe
	// row closed `completed` and this arm wrote "the work is unreachable" into a
	// row whose ground had NEVER HELD A COMMIT — HEAD == origin/master == the
	// commit the branch was cut at, `rev-list --count HEAD --not origin/master`
	// = 0, and the branch's reflog carried clone+checkout and nothing else).
	//
	// "NO REF HOLDS THIS TIP" and "THIS RUN PRODUCED NO TIP OF ITS OWN" are two
	// different facts, and only the FIRST is a loss: there is no work to be
	// unreachable when no work was ever made. The epic's own words are the
	// criterion — a produced-nothing conclusion is a CONCLUSION, and the sentence
	// a reader then needs is what the run DID deliver (a decomposition, a verdict,
	// a report), never an instruction to hunt for a missing commit.
	//
	// BOTH existing witnesses are asked, in the order of how much state each
	// needs (Rules 8/13: reuse, never a second mechanism):
	//
	//	① tipIsTheCutPoint — the tip IS the commit the branch's own reflog names
	//	   as its creation position. Needs the ground; available on a live one.
	//	② deliveryProofProduced — the tip's own author/committer stamps predate
	//	   the row's execution clock, so it was made before this run began,
	//	   whoever made it. Needs NO ground, which is the case that matters once
	//	   the channel has reclaimed the workspace (the incident's own shape).
	//
	// An unreadable answer from both is NOT the produced-nothing verdict: it
	// falls through to the pre-fix behaviour (Rule 15 — an abstention is never a
	// claim in either direction).
	if producedNothing, why := deliverableTipProducedNothing(taskID, out); producedNothing {
		out.Verdict = deliverableTipVerdictProducedNothing
		out.Note = deliverableTipProducedNothingNote(out, why)
		return out, nil
	}
	out.Verdict = deliverableTipVerdictUnreachable
	out.Note = deliverableTipUnreachableNote(out)
	return out, nil
}

// deliverableTipHolderDirs is the ordered, de-duplicated list of directories
// whose ref namespaces this arm may read: the GROUND the run worked in first
// (the only directory the channel's own push writes refs in), then the repo the
// row records. Empty entries are dropped, so a row with no recorded worktree
// reads exactly one directory — the pre-fix shape.
func deliverableTipHolderDirs(out deliverableTipOutcome, repo string) []string {
	var dirs []string
	seen := map[string]bool{}
	for _, dir := range []string{strings.TrimSpace(out.Ground), strings.TrimSpace(repo)} {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	return dirs
}

// deliverableTipDestinationRefs are the refs this row's work is DELIVERED TO,
// in the order they are trusted, as ref spellings (the directory is chosen by
// the caller).
//
//	· refs/remotes/origin/<base> — the clone channel's destination as a LOCAL
//	  tracking ref, in the ground (moved by the very push that delivered) and in
//	  the recorded repo;
//	· refs/heads/<base> — a bare/plain repo's or a local delivery point's own
//	  branch (the fixtures' shape, and what `ls-remote` answers for a path).
//
// The base is IntegrationBranchName — `master` — because that is what the
// channel's ONE refspec publishes (`devworkspace.publishGround`) and what
// devworkspace.DefaultBranch is. It is deliberately NOT derived per row: a row
// does not record its base branch, and inventing one per row would be a second
// definition of "where work is delivered" (Rules 1/14).
func deliverableTipDestinationRefs() []string {
	return []string{
		refsRemotesPrefix + deliveryProofOriginRemote + "/" + IntegrationBranchName,
		refsHeadsPrefix + IntegrationBranchName,
	}
}

// deliverableTipDestinationAmong answers "<ref> (<dir>)" when one of the given
// holder refs is a delivery destination, and "" otherwise.
func deliverableTipDestinationAmong(holders []string, dir string) string {
	want := map[string]bool{}
	for _, ref := range deliverableTipDestinationRefs() {
		want[ref] = true
	}
	for _, ref := range holders {
		if want[ref] {
			return ref + " (" + dir + ")"
		}
	}
	return ""
}

// deliverableTipDestinationCarries asks whether a DELIVERY DESTINATION holds the
// row's tip, and answers with the destination's name when one does.
//
// A destination that exists but does not CONTAIN the tip is not a pass: the
// containment test is the sibling delivery-proof arm's own leg
// (deliveryContentAncestryOrPatch — ancestry, then patch equivalence, because
// this repo lands work by cherry-pick/rebase), so a run whose commit never
// reached the integration branch keeps falling through to the defect verdict.
// An unanswered containment question is treated exactly like a no (fail-closed),
// and the directories are de-duplicated so one repo is not read twice.
func deliverableTipDestinationCarries(ctx context.Context, out deliverableTipOutcome, branch string) (string, bool) {
	if strings.TrimSpace(out.Tip) == "" {
		return "", false // nothing to look for (the note already says so)
	}
	seen := map[string]bool{}
	for _, dir := range []string{out.Ground, out.Repo} {
		dir = strings.TrimSpace(dir)
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		for _, ref := range deliverableTipDestinationRefs() {
			if ref == refsHeadsPrefix+branch {
				continue // the row's OWN ref is arm ①'s question, already answered
			}
			if _, err := branchDeleteGit(ctx, dir, "rev-parse", "--verify", "--quiet", ref); err != nil {
				continue // the ref does not resolve here — an absence of evidence
			}
			if landed, _, answered := deliveryContentAncestryOrPatch(ctx, dir, ref, out.Tip); answered && landed {
				return ref + " (" + dir + ")", true
			}
		}
	}
	return "", false
}

// deliverableTipProducedNothing asks the two witnesses above and reports whether
// the row's tip was already produced before this run began (with the evidence of
// whichever answered).
func deliverableTipProducedNothing(taskID string, out deliverableTipOutcome) (bool, string) {
	if strings.TrimSpace(out.Tip) == "" {
		// Nobody kept a sha: the question has no subject, and "no sha" is what
		// the unreachable sentence already says out loud.
		return false, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	if out.Ground != "" && tipIsTheCutPoint(ctx, out.Ground, out.Branch, out.Tip) {
		return true, fmt.Sprintf("branch %s's own reflog names %s as the commit it was cut at, and HEAD is that commit (%s)",
			out.Branch, branchDeleteShortHash(out.Tip), out.Ground)
	}
	dir := deliveryProofTipDir(out.Repo, out.Ground)
	if verdict, evidence := deliveryProofProduced(ctx, taskID, dir, out.Tip); verdict == deliveryProofProducedNo {
		return true, evidence
	}
	return false, ""
}

// deliverableTipProducedNothingNote is the SECOND correct sentence for this
// criterion, and it is the one a produced-nothing row must wear. It states the
// measurement, denies the phantom loss BY NAME (so an operator who greps for the
// old sentence on this row knows it is absent on purpose), and says where the
// run's real deliverable is — the row's own `reason:`/report, not a ref.
func deliverableTipProducedNothingNote(out deliverableTipOutcome, why string) string {
	return fmt.Sprintf("run %s claims `%s` for %s, and its deliverable branch %s resolves to no ref — but branch %s was still AT ITS CUT POINT when this was measured, so the run produced NO commit of its own: there is no delivery to prove and NOTHING to look for. Nothing is lost, moved or restored by this arm, and the run's actual deliverable (a decomposition, a verdict, a report) is in the row's own reason/report, not in a ref. Evidence: %s",
		out.TaskID, taskStatusCompleted, out.Repo, out.Branch, out.Branch, why)
}

// deliverableTipUnreachableNote is the defect sentence: WHO claims delivery,
// WHICH ref it named, WHICH commit is at risk (or that nobody kept its sha), and
// WHERE the refs were consulted — the four facts a recovery needs.
func deliverableTipUnreachableNote(out deliverableTipOutcome) string {
	tip := "no removal-audit record kept the tip the branch pointed at"
	if out.Tip != "" {
		tip = "the tip is " + out.Tip
	} else if out.AuditPath != "" {
		// Naming the file consulted is the difference between "the record is not
		// there" and "this run read a different sink than the writer wrote" —
		// the second is a real state (a sandboxed profile, a pinned override)
		// and it must not render as the first.
		tip += " (the sink this run read was " + out.AuditPath + ", and it names no deletion of this branch)"
	} else {
		tip += " — it can only be identified by hand"
	}
	point := "no delivery point was recorded for it"
	if out.DeliveryPoint != "" {
		point = "the recorded delivery point " + out.DeliveryPoint + " does not hold it"
	}
	return fmt.Sprintf("run %s claims `%s` for %s, but %s: branch %s resolves to no ref, %s, and no reachable commit names this task id (%s); the work is unreachable and nothing was deleted, moved or restored by this arm",
		out.TaskID, taskStatusCompleted, out.Repo, deliverableTipUnreachableMarker, out.Branch, point, tip)
}

// deliverableTipUncheckedNote is the honest non-answer. It NAMES what could not
// be read, so it is never mistaken for "no ref holds the tip".
func deliverableTipUncheckedNote(taskID, branch, why string) string {
	return fmt.Sprintf("deliverable tip for %s (branch %s) NOT judged: %s — the question was never answered, which is not the same as an answer of \"no ref holds it\"",
		taskID, branch, why)
}

// ── the close-out seam ─────────────────────────────────────────────────────

// deliverableTipAtWriteback is the RECORD half, and it sits beside
// reclaimRunResidueAtWriteback in the same ONE state gate: it never changes the
// outcome being written (the caller's terminal write is authoritative and has
// already committed when this runs), and it does nothing at all for the control
// case — a row that records no branch, or one whose tip is still reachable.
func deliverableTipAtWriteback(taskID, status string) {
	if !taskStatusClaimsDelivery(status) {
		return
	}
	out, err := TaskDeliverableTipReachable(taskID)
	if err != nil {
		taskNotifyLog("deliverable tip check for %s: %v", taskID, err)
		return
	}
	switch out.Verdict {
	case deliverableTipVerdictUnreachable:
		if deliverableTipFiled(taskID) {
			return // idempotent: the closing row already carries the record
		}
		deliverableTipRecord(taskID, out)
	case deliverableTipVerdictProducedNothing:
		// A CONCLUSION, not a defect (bug row 01a200xx, measured 2026-10-31):
		// the ground never produced a commit, so nothing is unreachable and no
		// record of a loss may be filed. It is still SAID on stderr and on the
		// trail — under its own event name, so an operator reading the row's
		// history sees which of the two facts was measured (Rule 15) — and it
		// never wears the unreachable marker, which is what a later reader greps.
		deliverableTipProducedNothingRecord(taskID, out)
	case deliverableTipVerdictUnchecked:
		// Reported, never swallowed (Rule 15) — and never on the record, so the
		// trail distinguishes "measured and found unreachable" from "could not
		// look". A row with no branch never reaches here (not judged at all).
		taskNotifyLog("%s", out.Note)
		fmt.Fprintln(os.Stderr, "[task] "+out.Note)
	}
}

// deliverableTipRefusal is the BINDING half (the operator's switch): the row's
// status is left UNTOUCHED and the row stays OPEN, with the same attributed
// note already filed, so the queue's open list carries the handle and the run
// can push the tip somewhere durable and write back again. A false return means
// "not refused": nothing was found, or the switch is off.
func deliverableTipRefusal(taskID string) (string, bool) {
	if !deliverableTipEnforced() {
		return "", false
	}
	out, err := TaskDeliverableTipReachable(taskID)
	if err != nil {
		taskNotifyLog("deliverable tip refusal check for %s: %v", taskID, err)
		return "", false
	}
	if out.Verdict != deliverableTipVerdictUnreachable {
		return "", false
	}
	if !deliverableTipFiled(taskID) {
		deliverableTipRecord(taskID, out) // the trail carries the fact either way
	}
	deliverableTipEventOnce(taskID, deliverableTipUnreachableRefusedEvent, out.Note)
	return out.Note, true
}

// deliverableTipEnforced reports the operator's choice (Rule 2: the single
// reader of deliverableTipUnreachableEnv).
func deliverableTipEnforced() bool { return config.EnvBool(deliverableTipUnreachableEnv) }

// deliverableTipFiled reports whether this row's trail already carries the
// record — the same idempotence guard shape the residue arm uses, so a
// re-landed writeback does not drip a second record.
func deliverableTipFiled(taskID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return false // best-effort: an unreadable trail must not block a close-out
	}
	defer db.Close()
	var one int
	err = db.QueryRowContext(ctx,
		`SELECT 1 FROM task_events WHERE task_id = ? AND event LIKE ? LIMIT 1`,
		taskID, deliverableTipUnreachableEvent+"%").Scan(&one)
	return err == nil
}

// deliverableTipRecord is the ACT: the attributed event and the operator's
// stderr line are written together, so the trail and the terminal cannot
// disagree (Rule 14) — the same contract reclaimRunResidueRecord draws.
func deliverableTipRecord(taskID string, out deliverableTipOutcome) string {
	if out.Note == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("deliverable tip record %s: %v", taskID, err)
		fmt.Fprintln(os.Stderr, "[task] "+out.Note)
		return out.Note
	}
	defer db.Close()
	taskEventWrite(ctx, db, taskID, deliverableTipUnreachableEvent, out.Note)
	taskNotifyLog("%s", out.Note)
	fmt.Fprintln(os.Stderr, "[task] "+out.Note)
	return out.Note
}

// deliverableTipProducedNothingRecord writes the CONCLUSION to the row's trail
// and the operator's stderr — the same one-act-two-places contract
// deliverableTipRecord draws, under its own event name and WITHOUT the
// unreachable marker, so nothing that greps for a loss matches it.
func deliverableTipProducedNothingRecord(taskID string, out deliverableTipOutcome) {
	if out.Note == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("produced-nothing record %s: %v", taskID, err)
		fmt.Fprintln(os.Stderr, "[task] "+out.Note)
		return
	}
	defer db.Close()
	taskEventWrite(ctx, db, taskID, deliverableTipProducedNothingEvent, out.Note)
	taskNotifyLog("%s", out.Note)
	fmt.Fprintln(os.Stderr, "[task] "+out.Note)
}

// deliverableTipEventOnce files the binding verdict once per row, bounded by the
// residue arm's own per-row limit (Rule 13: one spelling of the cap).
func deliverableTipEventOnce(taskID, event, detail string) {
	if taskEventCountForDetail(taskID, event, detail) >= reclaimRunResidueRefusalLimit {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("deliverable tip event %s: %v", taskID, err)
		return
	}
	defer db.Close()
	taskEventWrite(ctx, db, taskID, event, detail)
}
