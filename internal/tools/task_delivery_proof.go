package tools

// task_delivery_proof.go — THE DELIVERY-PROOF CRITERION AT CLOSE-OUT: a
// success terminal status is a CLAIM THE WRITE PATH MUST PROVE.
//
// INCIDENT (row 01a163df-a105-71b8-a297-680c5d35d093, measured 2026-10-22).
// The row reads `status: completed`, its deliverable is
//
//	{"branch":"master","repo":"/home/user/work/gopi",
//	 "worktree":"/home/user/workspace/01a163df-a105-71b8-a297-680c5d35d093"}
//
// and the ground it names is a CLONE-channel workspace whose three commits
// never reached the delivery point:
//
//	$ git -C <ground> log --oneline -1        → f26ab6f
//	$ git -C <ground> rev-parse origin/master → 882c645
//	$ git -C <ground> rev-list --left-right --count origin/master...HEAD
//	  0	3
//
// The writeback was performed by a surviving CHILD process (its wrapper pid
// was already gone) at the row's exact `ended_at`. Every surface that renders
// a terminal row rendered this one exactly like a delivered one: the status
// word asserted work that exists nowhere but in a directory nobody will merge.
//
// WHY THE EXISTING ARMS DO NOT COVER IT.
//
//   - `reclaimRunTrackedDirtRefusal` / `reclaimRunResidueRefusal` measure the
//     run's WORKTREE for uncommitted dirt. Here the work WAS committed — to
//     the ground. Committed and undelivered is exactly the state they pass.
//   - `deliverableTipRefusal` asks whether a tip is reachable from SOME ref.
//     The ground's own `refs/heads/master` holds f26ab6f, so the answer is
//     yes, and it is a true answer to a different question.
//
// THE CRITERION. For a row that CLAIMS a delivery (`completed`/`verified`) and
// records a deliverable branch, ask: is the TIP THE RUN PRODUCED contained in
// the DESTINATION THE ROW DECLARED?
//
//	tip         the ground's HEAD (the commit the run actually made), falling
//	            back to the tip the removal-audit sink recorded when the
//	            ground is already gone.
//	destination origin/<branch> — the clone channel's delivery point, read
//	            from the ground's own tracking ref (a clone channel refreshes
//	            it on every round's `pull --rebase`, and a successful `push`
//	            updates it in place), falling back to the recorded repo's
//	            origin/<branch>. The row's `repo` alone is deliberately not the
//	            primary: a client host's refs/heads/master is a MANUAL ref that
//	            goes stale by design (devdoc/integration-claim-ref-staleness.md,
//	            memory #9146), so a judgement anchored on it would refuse
//	            legitimate closes.
//
// THREE-VALUED, and Unknown is NEVER collapsed (memory #3342/#3339): a row
// whose ground, tip or destination cannot be read is RECORDED, never refused.
// A guard that refuses what it cannot see wedges the ledger.
//
// TWO ARMS PER (tip, destination) PAIR, because this repo LANDS WORK BY
// CHERRY-PICK/REBASE ("adoption: merge <branch>"): ancestry
// (`merge-base --is-ancestor`) and PATCH EQUIVALENCE (`git cherry`, no line
// marked `+`). Refusing on independence alone would refuse exactly the closes
// the merger performed correctly.
//
// AND A THIRD ARM, FOR THE REWRITE THE SECOND CANNOT SEE (measured 2026-10-27,
// row 01a17a67-f272-7cdf-adc1-2485d7a73a33). The clone channel delivers with
// `pull --rebase` then `push`, so a run's commits are REPLAYED onto a
// destination that has MOVED. Two different things then happen to them:
//
//	(a) A PURE REBASE keeps every patch identical — git recomputes a hunk into
//	    the same lines and `git patch-id --stable` answers the same id, which
//	    `git cherry` ALSO reports as present. Measured on the row's own pair:
//	      $ git -C <ground> show 50b57f8 | git patch-id --stable
//	      f9fbd4d125307209c7ed86ff924cba530eb8090f 50b57f896edd...
//	      $ git -C <master> show 5e3ce2e18 | git patch-id --stable
//	      f9fbd4d125307209c7ed86ff924cba530eb8090f 5e3ce2e18df16...   <- identical
//	      $ git -C <ground> cherry ecce621cd HEAD
//	      - 50b57f896ecdd560ed4ed882157dfce8bf25d6bf          <- PRESENT
//	    The `git cherry` arm already answers this one, and it is the arm that
//	    keeps the criterion independent of commit identity.
//
//	(b) A REBASE THAT RE-DERIVED THE CONTENT changes the patch itself — a
//	    regenerated table measured against a moved base is the shape — and then
//	    NO patch comparison can ever match. Measured on the row's second commit:
//	      $ git -C <ground> show 34f8039 | git patch-id --stable
//	      cb843035253d6a3f453a6d12a5a1f72c76d7c7d2 34f803948994...
//	      $ git -C <master> show ecce621cd | git patch-id --stable
//	      4f1ab5bf8c7ccb089452d1922b8aa56c5994f586 ecce621cdfa1...   <- DIFFERENT
//	      $ git -C <ground> cherry ecce621cd HEAD
//	      + 34f80394899406c2e3e00bf35f055b35cd48c092          <- ABSENT
//
// For (b) the honest witness is not the content but the DELIVERY: the channel
// pushed, and it recorded that it did. So this arm accepts ONE further piece of
// evidence, and only in this precise shape:
//
//	the row carries the channel's own landed record for THIS branch, naming the
//	very tip this run produced (TaskDeliveryRecordLanded).
//
// It cannot rescue an undelivered row — a run whose push never landed has no
// such record, and the arm reads `false` — and it deliberately does not accept a
// record naming some OTHER tip, because "this row was delivered once" is not
// "THIS commit was delivered".
//
// AND NO SEPARATE PATCH-ID ARM WAS ADDED, because one already runs: `git cherry`
// IS a patch-id comparison (it computes `git patch-id --stable` for both sides
// and matches ids). Measured before writing one, on a pair with NO common
// ancestor at all — the case a commit-based comparison could not answer:
//
//	$ git -C <ground> cherry refs/remotes/origin/master <tip>
//	- 7c8e0f0a79d3eec9c3bdbb0e65573ead843b16b5     ← classified, ancestors or not
//
// A second arm computing the same ids would be a re-implementation of the arm
// above (Rule 35's "prefer what already runs"), and it would have bought nothing
// for the row at hand: its second commit's patch id CHANGED across the rebase
// (cb84303… → 4f1ab5b…), so no id comparison could ever have matched it.
//
// WHAT IT DOES NOT DO: it deletes nothing, restores nothing, pushes nothing
// and moves no ref. It is a READ at the one write choke point.

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/devworkspace"
	"github.com/nickhuang99/gopi/internal/removalaudit"
)

// The criterion's names (Rule 1b/13: one spelling each, referenced everywhere).
const (
	// deliveryProofOwedEvent is the attributed trail record for a REFUSED
	// delivery claim: the row is still open and its trail says why.
	deliveryProofOwedEvent = "delivery-proof-owed"
	// DeliveryProofUnverifiedEvent is the trail record for a write this arm
	// could not judge. It is the loud half of Rule 15: the row closed
	// unproven, and that fact is on its trail rather than in nobody's memory.
	DeliveryProofUnverifiedEvent = "delivery-proof-unverified"
	// deliveryProofAllowEnv is the operator's escape, in the same shape as
	// GOPI_TASK_DIRTY_CLOSEOUT_ALLOW: the refusal becomes a loud RECORD, so
	// "proven" and "let through by decision" stay distinguishable on the row.
	// It is an ALIAS of config.TaskDeliveryProofAllowEnv (Rule 14: the catalog
	// row says `gopi --help-env` lists this escape, and a help row that names a
	// name this reader does not read would be a documented knob nothing
	// consumes — the literal is declared once, in internal/config, and every
	// consumer references it).
	deliveryProofAllowEnv = config.TaskDeliveryProofAllowEnv
	// deliveryProofOriginRemote is the delivery point's remote name. The
	// clone channel's push and pull both address it, so its tracking refs are
	// the only destination the channel itself maintains.
	deliveryProofOriginRemote = "origin"
	// deliveryProofCherryAbsentMark / ...PresentMark are the two line prefixes
	// `git cherry` emits: `+` = the patch is ABSENT upstream, `-` = present.
	deliveryProofCherryAbsentMark  = "+"
	deliveryProofCherryPresentMark = "-"
	// deliveryProofMarker is the phrase a refusal carries, so an operator (or
	// a test) matches it without knowing the words around it.
	deliveryProofMarker = "its deliverable is not at the destination it declared"
	// deliveryProofFullSHALen is the length of a full object name — the shape
	// `git ls-remote`'s first column must have before this arm measures
	// against it (a short or decorated answer is not an object name).
	deliveryProofFullSHALen = 40
	// DeliveryLandedRecordPrefix / …From / …At / …Detail introduce ONE landed
	// record's detail: "<prefix><branch><from><ground><at><tip><detail><rest>".
	// They are the record's GRAMMAR, exported in the same block as the event
	// name so the writer (the clone channel's delivery leg) and the reader
	// (this arm) can never drift (Rules 1b/14).
	DeliveryLandedRecordPrefix = devworkspace.CloneChannelPrefix + "delivered "
	DeliveryLandedRecordFrom   = " from "
	DeliveryLandedRecordAt     = " at "
	DeliveryLandedRecordDetail = " — "
	// deliveryProofRefreshTimeout bounds the ONE refresh fetch this arm runs
	// before judging (deliveryProofRefreshDestination). It is the same budget the
	// close-out chain already gives its git reads (reclaimGitTimeout), referenced
	// rather than re-typed (Rule 1/2): a second number for "how long may git take
	// here" is a knob nobody would keep in step.
	deliveryProofRefreshTimeout = reclaimGitTimeout
	// deliveryProofRefreshRefspec is the standard tracking-ref refresh: every
	// branch of the delivery point into refs/remotes/<remote>/*. A narrower
	// refspec (the row's own branch) is REFUSED BY GIT when that branch does not
	// exist upstream — and for a clone-channel row it usually does not (the
	// channel pushes the INTEGRATION branch while the row records `task/<id>`),
	// so a narrow refspec would fail loudest exactly on the rows that need the
	// refresh. A wildcard asks for nothing that is not already a tracking ref's
	// business.
	deliveryProofRefreshRefspec = "+refs/heads/*:" + refsRemotesPrefix + deliveryProofOriginRemote + "/*"
	// deliveryProofLandedRecordScanLimit bounds how many of the row's landed
	// records this reader looks at: a row that was delivered more than this many
	// times is not a question this arm answers, and the newest records are the
	// ones that matter (the query orders by id DESC).
	deliveryProofLandedRecordScanLimit = 32
)

// THE THREE VALUES (row 01a183fa, measured 2026-10-29). They are the criterion's
// whole vocabulary, and each names a DIFFERENT epistemic state:
//
//	deliveryProofConfirmed   the work IS at a destination this arm measured.
//	deliveryProofAbsent      the work is PROVABLY not at a destination this arm
//	                         MEASURED — every arm ran and every one answered no,
//	                         on evidence that was actually read.
//	deliveryProofUnverified  the question could not be answered (Rule 15: a
//	                         reader that could not look says so).
//
// THE DEFECT THIS VOCABULARY REPAIRS, in the row's own words. The arm used to
// answer `undelivered` after a content arm that never RAN ("The content
// comparison could not be completed against … (its objects are not readable
// here), so this verdict rests on the ancestry and patch arms alone"), in a
// sentence whose own parenthetical named the destination as the SAME SHA as the
// tip being judged — and wrote the deterministic sentence "its deliverable is not
// at the destination it declared". An UNMEASURED question was reported as a
// measured NO, so a row whose work had landed was refused, left `in_progress`,
// its ground unreleased and its worker reported failed.
//
// The old names are kept as ALIASES, not as a second vocabulary: the two states
// they distinguished by NAME are now distinguished by the arm's own `Measured`
// set (deliveryProofOutcome), which is the fact the sentence must be built from.
const (
	deliveryProofConfirmed  = "confirmed"
	deliveryProofAbsent     = "absent"
	deliveryProofUnverified = "unverified"
	// deliveryProofDelivered / deliveryProofUndelivered are the pre-rename
	// spellings, still referenced by tests and by readers outside this file.
	deliveryProofDelivered   = deliveryProofConfirmed
	deliveryProofUndelivered = deliveryProofAbsent
	// deliveryProofUnknown is every non-answer: the arm's default, and what a
	// row whose destination could not be read at all is left with.
	deliveryProofUnknown = deliveryProofUnverified
)

// deliveryProofOutcome is ONE row's reading, carrying every measurement behind
// the verdict so a reviewer can re-check it without re-running the arm.
type deliveryProofOutcome struct {
	State string
	// TaskID / Repo / Branch / Ground are the row's declared facts.
	TaskID string
	Repo   string
	Branch string
	Ground string
	// Tip is the commit the RUN produced (ground HEAD, else the audit sink's
	// recorded tip) — "" means nobody kept it, which the note says out loud.
	Tip string
	// Destination is the ref (in DestinationAt) the claim was measured
	// against, and Destinations lists every one that resolved.
	Destination    string
	DestinationAt  string
	Destinations   []string
	DestinationsAt []string
	Note           string
	// Measured holds the destinations (their `subject()` spelling) whose
	// containment question was ACTUALLY answered — every arm that ran returned a
	// verdict, no arm was skipped for an unreadable object or a cut-off walk.
	//
	// IT IS THE HALF THAT SEPARATES `absent` FROM `unverified`, and it exists
	// because no arm's own return value carries that distinction across the
	// three-leg seam: `deliveryContentAncestryOrPatch` answers `answered=false`
	// when neither ancestry nor `git cherry` could run, and the content leg
	// answers `judged=false` for an unreadable object — both are non-answers,
	// and the caller is the only place that sees them together with the others.
	Measured []string
	// Refresh records what the ONE pre-judgement fetch did: "refreshed <dir>" or
	// the reason it could not run. Rule 15: a stale view that could not be
	// refreshed is SAID, never silently judged as if it were current.
	Refresh string
}

// deliveryProofSaw is the per-destination reading the judge loop records: every
// non-answer the arms produced, and whether any containment question was
// answered at all. It is the ONE place the three arms' answers are collected,
// so the verdict is derived from facts rather than inferred from a loop shape.
type deliveryProofSaw struct {
	// Containment recorded at least one answered question (ancestry, patch or
	// content said yes or no).
	Measured bool
	// ContentRan records at least one content comparison that answered — the
	// arm whose silence the old sentence mis-stated.
	ContentRan bool
	// Gaps names each arm that could NOT run, in the arm's own words.
	Gaps []string
	// TipReadable records that SOME directory could resolve the tip's own
	// commit. When nothing can, no patch or content comparison is possible at
	// all — which is a fact about reach, and the reason the verdict below has a
	// second way to be a MEASURED absence (the destination's own history simply
	// does not carry the object).
	TipReadable bool
	// LivePoint records that at least one destination was resolved from the
	// DELIVERY POINT ITSELF (`git ls-remote`, so its sha is what the point holds
	// NOW) and that sha was readable where the measurement ran. Only that shape
	// lets an unreadable tip be a MEASURED absence: a cached ref that does not
	// carry the tip speaks for some directory's last fetch, never for the point
	// (a stale cache and a real gap are indistinguishable, and the incident is
	// exactly a refusal built on the stale one).
	LivePoint bool
}

// deliveryProofRef is one resolved destination: the ref to measure, the
// directory the measurement runs in, and — for the LIVE witness — the sha the
// delivery point reported and the point's own URL. A clone ground's tracking
// ref and the recorded repo's are different worlds, and the note must say WHICH
// was consulted; a delivery point is a third world, and saying "origin/master in
// /home/user/workspace/<task>" would name a cache while claiming the point.
type deliveryProofRef struct {
	Ref string
	At  string
	// Point is the sha the DELIVERY POINT answered for Ref ("" for a ref read
	// out of a local cache — there the ref name IS the whole measurement).
	Point string
	// URL is the delivery point as the ground names it ("" for a local cache).
	URL string
}

// measureRef is what the containment measurement actually uses: a live
// destination is measured against the sha its point reported, a cached one
// against the ref name in the directory that holds the cache.
func (r deliveryProofRef) measureRef() string {
	if r.Point != "" {
		return r.Point
	}
	return r.Ref
}

// deliveryProofSubject names the destination for the operator: the point and
// the sha when git was asked, the ref and its directory when a cache was read.
func (r deliveryProofRef) subject() string {
	if r.Point != "" {
		return r.Ref + " (" + r.URL + " = " + branchDeleteShortHash(r.Point) + ")"
	}
	return r.Ref + " (" + r.At + ")"
}

// deliveryProofIsCloneGround reports whether dir is a CLONE channel ground —
// a clone's `GroundGitEntryName` entry is a DIRECTORY, a linked worktree's is a
// FILE. The discriminator is ONE definition (`devworkspace.GroundModelOf`,
// Rules 13/14): the publish-target guard, the branch-delete guard and this arm
// all ask it rather than re-statting the entry, because a second spelling of
// "which shape is this?" is exactly how a sentence about one shape got applied
// to the other (measured 2026-10-27: a clone ground whose own briefing called it
// a linked worktree).
func deliveryProofIsCloneGround(dir string) bool {
	return devworkspace.GroundModelOf(dir) == devworkspace.GroundModelClone
}

// deliveryProofGitRead runs one read-only git command in dir ("" + no error
// when the command fails: every caller treats "cannot read" as a value, never
// as a reason to panic or to guess).
func deliveryProofGitRead(ctx context.Context, dir string, args ...string) string {
	out, err := branchDeleteGit(ctx, dir, args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// deliveryProofTipOf returns the tip the RUN produced, from THREE witnesses in
// the order of how directly each one knows it:
//
//  1. the ground's HEAD, when the ground is still on disk (a clone ground and a
//     linked worktree both answer `rev-parse HEAD`);
//  2. the tip the removal-audit sink recorded for the branch when its ref was
//     deleted;
//  3. THE ROW'S OWN LANDED RECORD — the clone channel's statement, written by
//     TaskDeliveryRecordLanded, that it pushed THIS branch at THIS tip.
//
// WHY (3) HAD TO BE ADDED, AND WHY IT IS NOT THE THIRD ARM WEARING A HAT
// (measured 2026-10-27/28, four rows: 01a17de7 / 01a17e58 / 01a17e03-ef4a /
// 01a17e03-cb63). All four read `delivery-proof-unverified` with this very
// sentence — "the tip this run produced cannot be named: its ground is gone and
// no removal-audit record kept its sha" — while their commits were ancestors of
// origin/master (6afc5ae5, 8f12b871, 7c8079dc, 5f4c675f). Measured on this host:
//
//	$ ls -d /home/user/workspace/01a17de7-fe23-72c1-b4ad-70141607c6ea
//	ls: cannot access '...': No such file or directory        ← witness (1) gone
//	$ grep '"kind":"branch-delete".*"branch":"task/01a17de7fe2372c1b4ad70141607c6ea"' \
//	      ~/.gopi/logs/removal-audit.log | wc -l
//	0                                                          ← witness (2) silent
//	（4043 branch-delete records on that sink, 477 of them carrying a `sha` —
//	 the field is optional, and no arm ever wrote one for these branches.）
//
// So the arm answered "cannot be named" for a commit that was ON THE
// INTEGRATION BRANCH, and the only artefact that knew the sha — the row's own
// landed record, whose text names it verbatim — was read EXCLUSIVELY from
// inside the tip-known branch of this function's caller. That is the defect:
// the ability to JUDGE BY SHA was made conditional on the DIRECTORY still
// existing, and the third arm was unreachable in exactly the case it was built
// for.
//
// `fromLandedRecord` is returned so the caller can keep the two questions
// apart. A tip NAMED by that record may not then be judged PRESENT by the same
// record: "the channel said it pushed X" is a statement about what was sent, not
// about what the destination holds, and letting one fact answer both would make
// this arm unconditionally green — the failure mode this repair must not
// introduce.
func deliveryProofTipOf(ctx context.Context, taskID, repo, ground, branch string) (tip, source string, fromLandedRecord bool) {
	if strings.TrimSpace(ground) != "" {
		if t := deliveryProofGitRead(ctx, ground, "rev-parse", "HEAD"); t != "" {
			return t, "the ground's HEAD", false
		}
	}
	if path, _ := removalaudit.SinkPath(); path != "" {
		if recs, err := removalaudit.BranchDeleteRecords(path); err == nil {
			for _, rec := range recs[branch] {
				if t := strings.TrimSpace(rec.Sha); t != "" {
					return t, "the removal-audit record of its deletion", false
				}
			}
		}
	}
	// 2b. THE GROUND-DELETION RECORD — the same sink, the OTHER kind (measured
	// 2026-10-29). A CLONE ground is reclaimed, not branch-deleted: the close-out
	// destroys a whole repository with `os.RemoveAll`, and until
	// `WorktreeRemoveAt` landed that record carried no sha at all (17704
	// worktree-removal records on this host's sink, ZERO with one). So witness 2
	// answered nothing for exactly the rows this arm exists for, and a commit
	// sitting on the integration branch was judged `delivery-proof-unverified`
	// because its DIRECTORY was gone. The record is now written with the tip, and
	// the reader asks it by the branch the ground stood on — the row's declared
	// address, and its own derived `task/<id>` as the secondary spelling.
	if tip := deliveryProofGroundRemovalTip(branch, taskID); tip != "" {
		return tip, "the removal-audit record of its ground's deletion", false
	}
	if t := deliveryProofLandedTip(taskID, branch); t != "" {
		return t, "the clone channel's own landed record of its push", true
	}
	_ = repo
	return "", "", false
}

// deliveryProofGroundRemovalTip reads the tip out of the GROUND-DELETION record
// for this row's branch, and answers "" when the sink holds none.
//
// WHY IT IS A SECOND READER AND NOT A WIDER FIRST ONE (Rule 14 applied to
// MEANING, not to code). `kind=branch-delete` and `kind=worktree-removal` are
// two different destructions decided on different evidence; a reader that
// lumped them together would answer a branch question with a directory's
// record. The two are read by ONE walker (removalaudit.RecordsByBranch) and
// asked SEPARATELY here, in the order of how directly each knows the tip: a
// deleted ref's recorded sha is a statement about that ref, a deleted ground's
// recorded sha is a statement about the directory's HEAD.
//
// NEWEST RECORD WINS, because a row can have its ground removed more than once
// (a retry on a re-cut ground) and the run ended on the LAST one. A record with
// no sha is skipped rather than answering "" for the branch: its own absence is
// the pre-fix shape and says nothing about a later record.
func deliveryProofGroundRemovalTip(branch, taskID string) string {
	path, _ := removalaudit.SinkPath()
	if path == "" {
		return ""
	}
	recs, err := removalaudit.WorktreeRemovalRecords(path)
	if err != nil {
		// Rule 15: an unreadable sink is REPORTED, never read as "no record".
		taskNotifyLog("delivery proof: the removal-audit sink %s could not be read for the ground's tip: %v", path, err)
		return ""
	}
	for _, name := range []string{strings.TrimSpace(branch), TaskGroundBranchName(taskID)} {
		if name == "" {
			continue
		}
		for i := len(recs[name]) - 1; i >= 0; i-- {
			if t := strings.TrimSpace(recs[name][i].Sha); deliveryProofIsHexSHA(t) {
				return t
			}
		}
	}
	return ""
}

// deliveryProofLandedTip returns the commit the CLONE CHANNEL recorded pushing
// for this row, read out of the row's own trail (the record
// TaskDeliveryRecordLanded writes through the one grammar
// DeliveryLandedRecordPrefix/…From/…At/…Detail).
//
// The OLDEST ambiguity is settled the same way deliveryProofLandedRecorded
// settles it: the branch a record may name and still be evidence about THIS row
// is one of the row's OWN addresses (deliveryProofRowAddresses), and a record
// the reader cannot parse is not evidence. The order is newest-first because a
// retried delivery re-pushes after `pull --rebase` (the rebase rewrites the
// row's own commits), so more than one record can exist for a row and the LAST
// push is the one the row ended on.
//
// "" means no readable record exists — the honest empty answer, never a guess.
func deliveryProofLandedTip(taskID, branch string) string {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(branch) == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	db, err := openTasksDBReadOnly(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("delivery proof: the landed records of %s could not be read: %v", taskID, err)
		return ""
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT detail FROM task_events WHERE task_id = ? AND event = ? ORDER BY id DESC LIMIT ?`,
		taskID, DeliveryLandedEventName, deliveryProofLandedRecordScanLimit)
	if err != nil {
		taskNotifyLog("delivery proof: the landed records of %s could not be queried: %v", taskID, err)
		return ""
	}
	defer rows.Close()
	want := deliveryProofRowAddresses(taskID, branch)
	for rows.Next() {
		var detail string
		if err := rows.Scan(&detail); err != nil {
			taskNotifyLog("delivery proof: a landed record of %s could not be scanned: %v", taskID, err)
			return ""
		}
		gotBranch, gotTip, ok := deliveryProofParseLandedRecord(detail)
		if !ok || !want[gotBranch] {
			continue
		}
		return gotTip
	}
	if err := rows.Err(); err != nil {
		taskNotifyLog("delivery proof: the landed records of %s ended badly: %v", taskID, err)
	}
	return ""
}

// deliveryProofIntegrationDestination is the destination a GROUND-GONE row is
// measured against, and it exists for ONE measured shape.
//
// The clone channel publishes `HEAD:refs/heads/<integration>` (publishGround),
// while the row's `deliverable.branch` is the run's own ADDRESS (`task/<id>`).
// So for every row delivered by the post-2026-10-28 channel the declared branch
// resolves NOWHERE on the delivery point — measured on the four rows this
// arm's repair was filed for:
//
//	$ git rev-parse --verify --quiet refs/remotes/origin/task/01a17de7fe2372c1b4ad70141607c6ea
//	(no output)                      ← and the same for the other three
//	$ git merge-base --is-ancestor 6afc5ae542de33c81f30458e3fd9733f6ae0085d origin/master
//	$ echo $?
//	0                                ← the commit IS on the integration branch
//
// Asking only for origin/<declared-branch> therefore answers "there is no
// declared destination" for work that is sitting on the integration branch,
// which is the same category of wrong answer as "cannot be named".
//
// The delivery point is asked FIRST and ITS answer is what is measured against
// (the discipline deliveryProofLiveDestination already applies: a local cache is
// a fact about some directory's last fetch). The cached ref in the recorded repo
// is the fallback, so an offline close-out behaves as before.
//
// It is never a licence to go green: the returned value is only a DESTINATION.
// The tip must still be contained in it by ancestry or patch equivalence, so a
// run whose commits never reached the integration branch is still refused.
func deliveryProofIntegrationDestination(ctx context.Context, repo string) (deliveryProofRef, bool) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return deliveryProofRef{}, false
	}
	ref := refsRemotesPrefix + deliveryProofOriginRemote + "/" + IntegrationBranchName
	lsRef := refsHeadsPrefix + IntegrationBranchName
	if url := deliveryProofGitRead(ctx, repo, "remote", "get-url", deliveryProofOriginRemote); url != "" {
		if out := deliveryProofGitRead(ctx, repo, branchDeleteLsRemoteVerb, deliveryProofOriginRemote, lsRef); out != "" {
			for _, line := range strings.Split(out, "\n") {
				fields := strings.Fields(line)
				if len(fields) == 2 && fields[1] == lsRef && deliveryProofIsHexSHA(fields[0]) {
					return deliveryProofRef{Ref: ref, At: repo, Point: fields[0], URL: url}, true
				}
			}
		}
	}
	if _, err := branchDeleteGit(ctx, repo, "rev-parse", "--verify", "--quiet", ref); err != nil {
		return deliveryProofRef{}, false
	}
	return deliveryProofRef{Ref: ref, At: repo}, true
}

// deliveryProofCommitReadable answers whether dir's object store can resolve
// commit — the precondition for measuring containment THERE. A destination
// whose sha this directory cannot read is an absence of evidence, and this arm
// says so rather than reporting that absence as a verdict.
func deliveryProofCommitReadable(ctx context.Context, dir, commit string) bool {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(commit) == "" {
		return false
	}
	_, err := branchDeleteGit(ctx, dir, "cat-file", "-e", commit+gitPeerCommitSuffix)
	return err == nil
}

// deliveryProofDestinations resolves every destination ref this arm may
// measure, in the order it trusts them.
//
// THE FIRST WITNESS IS THE DELIVERY POINT ITSELF, and for a CLONE ground it has
// to be — the two refs this arm used to read are both LOCAL CACHES of it:
//
//	refs/remotes/origin/<branch> IN THE GROUND moves only when THIS ground
//	  pushes or fetches. A ground abandoned at its cut point keeps the cut
//	  point forever, and the delivery loop's own fetch runs on the next attempt
//	  of a loop that a refused close-out never reaches — so the "witness" is
//	  frozen exactly when the arm needs it.
//	refs/remotes/origin/<branch> IN THE RECORDED REPO is a different process's
//	  cache, refreshed by whoever last ran git there — no relationship at all to
//	  what the delivery point holds now.
//
// So neither is a fact about the destination; both are facts about some
// directory's last fetch. Measured (2026-10-26, fixture in
// task_delivery_proof_live_witness_test.go): a deliverable sitting at the
// delivery point, delivered by a path this ground never followed — a merger's
// adoption, or another node's push — is reported `undelivered` by both caches,
// and the row is left OPEN at close-out with the reclaim skipped. That is not a
// conservative answer, it is a wrong one: the work IS at its destination.
//
// `git ls-remote` asks the point. It writes nothing (no fetch, no ref update),
// which keeps this arm's promise that it never moves anything; an unreachable
// point answers nothing and the caches below still decide, so an offline
// close-out behaves exactly as before.
//
// A ref that does not resolve is not a destination — an absent ref is an
// absence of evidence, and the caller renders that as Unknown.
func deliveryProofDestinations(ctx context.Context, repo, ground, branch string) []deliveryProofRef {
	if strings.TrimSpace(branch) == "" {
		return nil
	}
	ref := refsRemotesPrefix + deliveryProofOriginRemote + "/" + branch
	var out []deliveryProofRef
	if deliveryProofIsCloneGround(ground) {
		if live, ok := deliveryProofLiveDestination(ctx, ground, branch); ok {
			out = append(out, live)
		}
	}
	seen := map[string]bool{}
	for _, at := range []string{ground, repo} {
		at = strings.TrimSpace(at)
		if at == "" || seen[at] {
			continue
		}
		seen[at] = true
		if _, err := branchDeleteGit(ctx, at, "rev-parse", "--verify", "--quiet", ref); err != nil {
			continue
		}
		out = append(out, deliveryProofRef{Ref: ref, At: at})
	}
	return out
}

// deliveryProofLiveDestination asks the DELIVERY POINT what its branch holds,
// through the ground's own `origin` URL (`git ls-remote` — a read of the remote
// ref namespace, no fetch and no local ref written).
//
// The returned deliveryProofRef keeps TWO facts apart that the ref-shaped
// destinations conflate: `At` is the GROUND, because that is the directory the
// containment measurement runs in and the only place the delivered commit's
// objects can be answered from; `Point` is the sha the delivery point reported,
// which is what the measurement is against. `URL` is the point as the note must
// name it — a reader has to be able to tell "the destination said so" from "a
// local cache said so", and that is the whole defect.
//
// ok=false means the point could NOT be asked (no origin, unreachable, branch
// absent there) — an absence of evidence, never a verdict.
func deliveryProofLiveDestination(ctx context.Context, ground, branch string) (deliveryProofRef, bool) {
	if strings.TrimSpace(ground) == "" || strings.TrimSpace(branch) == "" {
		return deliveryProofRef{}, false
	}
	url := deliveryProofGitRead(ctx, ground, "remote", "get-url", deliveryProofOriginRemote)
	if url == "" {
		return deliveryProofRef{}, false
	}
	lsRef := refsHeadsPrefix + branch
	out := deliveryProofGitRead(ctx, ground, branchDeleteLsRemoteVerb, deliveryProofOriginRemote, lsRef)
	if out == "" {
		return deliveryProofRef{}, false
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] != lsRef || !deliveryProofIsHexSHA(fields[0]) {
			continue
		}
		// THE OBJECTS HAVE TO BE HERE to measure against that sha, and this arm
		// does not fetch (a fetch would move the very cache whose staleness is
		// the defect). A ground that does not carry the point's tip answers
		// nothing and leaves the caches to decide.
		if _, err := branchDeleteGit(ctx, ground, "cat-file", "-e", fields[0]); err != nil {
			return deliveryProofRef{}, false
		}
		return deliveryProofRef{Ref: lsRef, At: ground, Point: fields[0], URL: url}, true
	}
	return deliveryProofRef{}, false
}

// deliveryProofIsHexSHA reports whether s has the shape of a full object name
// (ls-remote's first column). A short or decorated answer is not a sha this arm
// may measure against.
func deliveryProofIsHexSHA(s string) bool {
	if len(s) != deliveryProofFullSHALen {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

// deliveryProofTipContained reports whether tip's work is at ref, using BOTH
// arms: ancestry, and — because this repo lands work by cherry-pick/rebase, so
// the commits differ while the patches are identical — patch equivalence
// (`git cherry <ref> <tip>` listing no `+` line).
//
// IT IS THE SEAM'S FIRST TWO LEGS, not a second implementation of them
// (delivery_content_landed.go, deliveryContentAncestryOrPatch): the criterion
// lives in ONE place because four faces of this machinery each built their own
// weaker version and each was wrong in its own way. The seam is the caller's
// route; this function survives as the (test-pinned) two-armed spelling the
// close-out chain's own notes quote.
func deliveryProofTipContained(ctx context.Context, at, ref, tip string) (bool, string) {
	landed, why, _ := deliveryContentAncestryOrPatch(ctx, at, ref, tip)
	return landed, why
}

// CloneDeliveryTip reads the commit a clone ground's HEAD names — the tip the
// channel just pushed. "" means git could not answer, and the caller must SAY so
// rather than record a delivery it cannot name.
func CloneDeliveryTip(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	return deliveryProofGitRead(ctx, dir, "rev-parse", "HEAD")
}

// TaskDeliveryRecordLanded is the CLONE CHANNEL's write side of the third arm:
// it records, on the row, that a push of `branch` landed carrying `tip`.
//
// WHY THE TIP IS IN THE RECORD. "This row was delivered once" is not "the commit
// I am judging was delivered". A run that pushes, then commits more (the same
// ledger row, the same branch), has delivered its first commits and NOT its
// last; a record that named only the branch would let the second state pass as
// the first. The tip makes the record a statement about ONE commit.
//
// The grammar is fixed by the exported constants beside it
// (DeliveryLandedRecordPrefix/…From/…At/…Detail) so this writer and the reader
// in deliveryProofLandedRecorded cannot drift.
func TaskDeliveryRecordLanded(taskID, branch, ground, tip, detail string) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" || strings.TrimSpace(branch) == "" || strings.TrimSpace(tip) == "" {
		taskNotifyLog("clone-delivery-landed record skipped: task=%q branch=%q tip=%q", taskID, branch, tip)
		return
	}
	if strings.TrimSpace(detail) == "" {
		detail = "pushed to origin"
	}
	AppendTaskTrailEvent(taskID, DeliveryLandedEventName, fmt.Sprintf("%s%s%s%s%s%s%s%s",
		DeliveryLandedRecordPrefix, branch, DeliveryLandedRecordFrom, ground,
		DeliveryLandedRecordAt, tip, DeliveryLandedRecordDetail, detail))
}

// deliveryProofLandedRecorded answers whether the row carries the channel's OWN
// record of a delivery of THIS tip on THIS branch, and returns that record's
// detail as the evidence sentence.
//
// It is the third arm's read half and it is deliberately narrow: the event must
// be the channel's landed event, the branch must be one of THIS ROW'S OWN
// ADDRESSES (see deliveryProofRowAddresses), and the tip must be the very commit
// being judged. A record naming another tip answers false — it is evidence about
// a DIFFERENT commit.
func deliveryProofLandedRecorded(taskID, branch, tip string) (bool, string) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(branch) == "" || strings.TrimSpace(tip) == "" {
		return false, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	db, err := openTasksDBReadOnly(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("delivery proof: the landed record of %s could not be read: %v", taskID, err)
		return false, ""
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT detail FROM task_events WHERE task_id = ? AND event = ? ORDER BY id DESC LIMIT ?`,
		taskID, DeliveryLandedEventName, deliveryProofLandedRecordScanLimit)
	if err != nil {
		taskNotifyLog("delivery proof: the landed records of %s could not be queried: %v", taskID, err)
		return false, ""
	}
	defer rows.Close()
	wantTip := strings.ToLower(strings.TrimSpace(tip))
	want := deliveryProofRowAddresses(taskID, branch)
	for rows.Next() {
		var detail string
		if err := rows.Scan(&detail); err != nil {
			taskNotifyLog("delivery proof: a landed record of %s could not be scanned: %v", taskID, err)
			return false, ""
		}
		gotBranch, gotTip, ok := deliveryProofParseLandedRecord(detail)
		if !ok {
			continue
		}
		if want[gotBranch] && strings.ToLower(gotTip) == wantTip {
			return true, detail
		}
	}
	if err := rows.Err(); err != nil {
		taskNotifyLog("delivery proof: the landed records of %s ended badly: %v", taskID, err)
	}
	return false, ""
}

// deliveryProofRowAddresses is the SET of names that count as THIS row's own
// address in a landed record — the branches a record may name and still be
// evidence about this row.
//
// WHY A SET AND NOT ONE NAME (measured 2026-10-27, this very row). The writer
// and the reader each name the address, but they derive it at DIFFERENT times,
// so they disagree across the 90f6a96 wiring boundary:
//
//   - the WRITER records what the clone workspace STOOD ON when it pushed —
//     after the wiring that is `task/<id>` (GroundBranch), before it was the
//     integration branch (a clone checked out the source's branch);
//   - the READER is handed `deliverable.branch` — what the CLAIM stamped, which
//     for a row claimed BEFORE the wiring is `master` and stays `master` (the
//     stamp is written once; nothing rewrites it).
//
// So a row claimed on the old shape is delivered by a post-fix channel that
// records `task/<id>`, and a reader that compared only `deliverable.branch`
// would answer "no record of this branch" for exactly the rows this arm exists
// to rescue — the arm unreachable, silently, in the OPPOSITE direction from the
// `landedRecordBranchOf` defect (`cmd/agent/devworkspace_channel.go`).
//
// Both names are the ROW'S OWN in a sense that cannot be forged: `branch` comes
// from the row, and `TaskGroundBranchName(taskID)` is DERIVED from the task id,
// so no other row can ever answer to it. Everything else still fails: a record
// naming `some/other/branch` is evidence about a branch this row never claimed.
func deliveryProofRowAddresses(taskID, branch string) map[string]bool {
	want := make(map[string]bool, 2)
	if b := strings.TrimSpace(branch); b != "" {
		want[b] = true
	}
	if own := TaskGroundBranchName(taskID); own != "" {
		want[own] = true
	}
	return want
}

// deliveryProofParseLandedRecord pulls (branch, tip) out of ONE landed record.
// ok=false for any detail that is not in the documented grammar — a record this
// reader cannot parse is not evidence, and it says nothing rather than guessing.
func deliveryProofParseLandedRecord(detail string) (branch, tip string, ok bool) {
	rest, found := strings.CutPrefix(detail, DeliveryLandedRecordPrefix)
	if !found {
		return "", "", false
	}
	branch, rest, found = strings.Cut(rest, DeliveryLandedRecordFrom)
	if !found {
		return "", "", false
	}
	_, rest, found = strings.Cut(rest, DeliveryLandedRecordAt)
	if !found {
		return "", "", false
	}
	tip, _, _ = strings.Cut(rest, DeliveryLandedRecordDetail)
	tip = strings.TrimSpace(tip)
	if strings.TrimSpace(branch) == "" || !deliveryProofIsHexSHA(tip) {
		return "", "", false
	}
	return strings.TrimSpace(branch), tip, true
}

// TaskDeliveryProof reads one row and judges whether its deliverable actually
// landed at the destination it declared. Exported so the evidence can be
// re-taken from a test or a session; the close-out seam is its only policy
// caller.
func TaskDeliveryProof(taskID string) (deliveryProofOutcome, error) {
	out := deliveryProofOutcome{TaskID: taskID, State: deliveryProofUnknown}
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return out, fmt.Errorf("delivery proof: task_id must not be empty")
	}
	repo, branch, ground := taskRowDeliverable(taskID)
	out.Repo, out.Branch, out.Ground = repo, branch, ground
	if strings.TrimSpace(branch) == "" {
		// Nothing named, nothing claimed: a row that declares no branch makes
		// no delivery claim this arm can measure (the control case).
		out.Note = fmt.Sprintf("row %s records no deliverable branch — no delivery claim to prove", taskID)
		return out, nil
	}
	if strings.TrimSpace(repo) == "" && strings.TrimSpace(ground) == "" {
		out.Note = deliveryProofUnknownNote(out, "the row records neither a repository nor a ground to ask")
		return out, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()

	tip, tipSource, tipFromLandedRecord := deliveryProofTipOf(ctx, taskID, repo, ground, branch)
	out.Tip = tip
	if tip == "" {
		out.Note = deliveryProofUnknownNote(out,
			"the tip this run produced cannot be named: its ground is gone, no removal-audit record kept its sha, "+
				"and the clone channel left no landed record on the row naming the commit it pushed")
		return out, nil
	}
	// THE ONE REFRESH, BEFORE ANY JUDGEMENT (row 01a183fa). The whole arm reads
	// refs that are LOCAL CACHES of the delivery point, and a cache that has not
	// been refreshed answers a question about some directory's last fetch rather
	// than about the destination. Measured on the incident row: the delivery point
	// reported `origin/master = eaaf6b43` — the very tip — while the ground's
	// object store could not read it, so every containment arm silently could not
	// be asked and the arm answered "it did not land".
	//
	// A fetch is the one thing that turns that shape into a measurable one: it
	// brings the destination's objects into the directory the measurement runs in.
	// It moves TRACKING refs only (`+refs/heads/*:refs/remotes/origin/*`), never a
	// branch, never the delivery point — this arm's promise that it deletes,
	// restores and pushes nothing is unchanged, and a fetch cannot deliver work
	// that was not delivered (it copies the destination's OWN state here, so it
	// can only ever make the measurement aim better, never manufacture a "yes":
	// the tip still has to be reachable from what the destination holds).
	//
	// It runs ONCE, bounded, and a failure DEGRADES to `unverified` with its
	// reason recorded — never to a verdict (Rule 15).
	var refreshDir string
	var refreshed bool
	out.Refresh, refreshDir, refreshed = deliveryProofRefreshDestination(ctx, ground, repo)
	dests := deliveryProofDestinations(ctx, repo, ground, branch)
	// THE GROUND-GONE WITNESS (measured 2026-10-27/28, four rows). When the
	// ground directory is gone, the row's declared branch is the run's ADDRESS
	// (`task/<id>`) and the channel pushed the INTEGRATION branch —
	// publishGround's ONE refspec — so origin/<declared> resolves nowhere and
	// the arm would answer "no declared destination to measure against" for a
	// commit sitting on master. The integration branch is asked as a further
	// destination, LAST in the order, and only when the ground itself cannot be
	// asked: a live ground's own tracking ref stays the first witness.
	//
	// AND IT IS ASKED WHETHER OR NOT THE GROUND SURVIVES (measured 2026-10-31,
	// rows 01a18799 / 01a18ea5). Gating this destination on the ground's
	// DELETION made the arm structurally blind for a DISPATCHED row: such a row
	// records `task/<id>` as its declared branch, the channel publishes
	// `HEAD:refs/heads/master`, and the run's ground is still standing when the
	// end hook measures — so `refs/remotes/origin/task/<id>` resolves nowhere,
	// the destination list came back EMPTY, and the arm answered
	// `delivery-proof-unverified` ("no declared destination to measure
	// against") for BOTH rows, which deliveryProofRefusal then LET THROUGH.
	// The row's own trail is the proof (verbatim, task_events of
	// 01a18ea5-8925-7c3e-b99e-130eae4c0f91):
	//
	//	delivery-proof-unverified | delivery proof for 01a18ea5-… (branch
	//	  task/01a18ea589257c3eb99e130eae4c0f91) NOT judged: no
	//	  refs/remotes/origin/task/01a18ea589257c3eb99e130eae4c0f91 resolves in
	//	  the ground or the recorded repo, so there is no declared destination
	//	  to measure against
	//	completed                 | — elapsed=3209.4s iter=172 tokens=19214676
	//
	// A destination that only exists in the reclaim case is a destination the
	// ordinary run never reaches, which is the opposite of what a guard needs.
	// The cost of asking always is BOUNDED and LOCAL: deliveryProofIntegration-
	// Destination reads `git ls-remote origin refs/heads/master` (a read of the
	// ref namespace, no fetch) with the fallback of a local rev-parse, in the
	// row's recorded repo — the same repo the refresh arm above already asks
	// over the network. It moves no ref, and it can only ever ADD a destination
	// a containment arm may answer against; a run whose work did not land still
	// answers no (the two regression arms in
	// task_delivery_proof_ground_gone_test.go hold both directions).
	if integ, ok := deliveryProofIntegrationDestination(ctx, repo); ok {
		dests = append(dests, integ)
	}
	// THE PRODUCED-NOTHING GATE, asked BEFORE any containment arm. A ground
	// whose HEAD is the very commit its cut-time snapshot names produced no
	// commit of its own, and every arm below would then answer "contained" for
	// a trivial reason — the tip is an ancestor of ITSELF. Measured (this
	// suite's TestDeliveryProof_AGroundOnItsCutPointProducedNothing; its red
	// proof disables this very condition and watches the same row come back
	// `delivered`, with the note "7b868a540c06 is an ancestor of 7b868a540c06".
	// An empty subject set is not "everything landed", and `delivered` is the
	// signal that RELEASES the ground).
	//
	// The CUT POINT comes from the branch's own reflog (tipIsTheCutPoint), never
	// from `refs/remotes/origin/master`: the channel's delivery push ADVANCES
	// that ref to the pushed tip, so reading it answered "produced nothing" for
	// runs that had just delivered (row 01a1810a; the measurement is on the
	// function).
	if tipIsTheCutPoint(ctx, ground, branch, tip) {
		out.State = deliveryProofUnknown
		out.Note = deliveryProofUnknownNote(out, fmt.Sprintf(
			"the tip %s is the very commit this run's ground was cut from, so the run produced NO commit of "+
				"its own — there is no delivery to prove", branchDeleteShortHash(tip)))
		return out, nil
	}
	// AND THE SAME QUESTION FROM EVIDENCE THAT SURVIVES THE GROUND'S DELETION.
	// The gate above can only be asked while the ground is readable, so a
	// reclaimed ground never gets the question at all — and every containment
	// arm below then answers "contained" for the cut-point commit, which is an
	// ancestor of the integration branch by construction. The commit's OWN
	// author/committer stamps, compared with the row's OWN execution clock, are
	// the witness that needs no directory (task_delivery_proof_produced.go
	// carries the four candidates measured, and why this one survived).
	//
	// It can only DISPROVE: a tip whose stamps both predate the run by more than
	// the skew was made before this run began, whoever made it. Everything else —
	// one stamp inside the window, no clock on the row, an unreadable commit —
	// falls through exactly as before, which is what keeps the rows the previous
	// repair recovered (d370a6e) from being pushed back to `unknown`.
	if verdict, evidence := deliveryProofProduced(ctx, taskID, deliveryProofTipDir(repo, ground), tip); verdict == deliveryProofProducedNo {
		out.State = deliveryProofUnknown
		out.Note = deliveryProofUnknownNote(out, fmt.Sprintf(
			"the tip %s existed BEFORE this run began, so the run produced NO commit of its own — there is no "+
				"delivery to prove (%s)", branchDeleteShortHash(tip), evidence))
		return out, nil
	}
	for _, d := range dests {
		out.Destinations = append(out.Destinations, d.Ref)
		out.DestinationsAt = append(out.DestinationsAt, d.At)
	}
	if len(dests) == 0 {
		out.Note = deliveryProofUnknownNote(out, fmt.Sprintf(
			"no %s%s/%s resolves in the ground or the recorded repo, so there is no declared destination to measure against",
			refsRemotesPrefix, deliveryProofOriginRemote, branch))
		return out, nil
	}
	var tried []string
	var contentWhy []string
	// SAW is the record of what was MEASURED, and it is the fact the verdict is
	// built from — an arm that could not run leaves a GAP here, never a "no".
	var saw deliveryProofSaw
	// MEASURED WHERE THE OBJECTS ARE. A successful refresh put the destination's
	// objects in `refreshDir`; measuring there instead of in the cached directory
	// is what turns "its objects are not readable here" into an answer. The
	// destination's REF is unchanged — only the directory the comparison runs in —
	// so this cannot widen the criterion: the tip still has to be reachable from
	// what the destination holds.
	if deliveryProofTipReadableAnywhere(ctx, []string{ground, repo, refreshDir}, tip) {
		saw.TipReadable = true
	}
	for _, d := range dests {
		at := d.At
		if refreshed {
			at = refreshDir
		}
		if d.Point != "" && deliveryProofCommitReadable(ctx, at, d.Point) {
			saw.LivePoint = true
		}
		ok, why, answered := deliveryContentAncestryOrPatch(ctx, at, d.measureRef(), tip)
		if answered {
			saw.Measured = true
			out.Measured = append(out.Measured, d.subject())
		} else {
			saw.Gaps = append(saw.Gaps, fmt.Sprintf(
				"the ancestry and patch arms could not be asked against %s (its objects are not readable from %s, or git could not answer)",
				d.subject(), at))
		}
		if ok {
			out.State = deliveryProofDelivered
			out.Destination, out.DestinationAt = d.Ref, d.At
			out.Note = fmt.Sprintf("row %s: %s (%s) is delivered — %s in %s",
				taskID, branch, tipSource, why, d.subject())
			return out, nil
		}
		// THE THIRD ARM, per destination: patch equivalence answers "the same
		// patch arrived", and a rebase that re-derived the content changes the
		// patch itself (see task_delivery_proof_content.go). This arm asks the
		// only question that survives that: are the LINES this run added there?
		// It runs in the ground (which holds the run's commits) against the
		// destination tree read from whichever directory carries it — never a
		// fetch, never a push.
		for _, pair := range deliveryProofContentPairs(ground, repo, refreshDir, refreshed) {
			landed, why, judged := deliveryProofContentLanded(ctx, pair[0], pair[1], d.measureRef(), d.Ref, tip)
			if !judged {
				continue
			}
			saw.ContentRan = true
			saw.Measured = true
			if landed {
				out.State = deliveryProofDelivered
				out.Destination, out.DestinationAt = d.Ref, d.At
				out.Note = fmt.Sprintf("row %s: %s (%s) is delivered — %s in %s",
					taskID, branch, tipSource, why, d.subject())
				return out, nil
			}
			contentWhy = append(contentWhy, why)
		}
		tried = append(tried, d.subject())
	}
	// THE CONTENT ARM'S SILENCE IS SAID ONCE, AND IT IS NOT A VERDICT. The gap
	// is recorded whether or not the verdict ends up `absent`: a sentence about
	// "the content" must never be written by a run that did not measure it (the
	// defect this repair exists for).
	if !saw.ContentRan {
		saw.Gaps = append(saw.Gaps, fmt.Sprintf(
			"the content comparison never ran against %s (the directory that holds the destination's objects could not "+
				"be read against this run's commits, or the walk was out of bounds)", strings.Join(tried, ", ")))
	}
	// THE THIRD ARM, LAST: the clone channel's OWN record that a push of this
	// branch landed carrying THIS tip. It answers the one case no content
	// comparison can (a rebase that re-derived the content — see the file
	// header), and it is deliberately narrow: the record must name one of the
	// row's OWN branches AND the very commit being judged. A record naming
	// another tip is evidence about a different commit and answers nothing here.
	//
	// AND IT IS NOT AVAILABLE WHEN IT IS THE TIP'S ONLY WITNESS. The same record
	// that NAMED this commit cannot then testify that the commit ARRIVED:
	// "the channel said it pushed X" is a statement about what was sent, and a
	// destination that does not hold X answers nothing about it. Letting one fact
	// answer both questions would make this arm unconditionally green for every
	// row whose ground is gone — precisely the failure mode this repair must not
	// introduce (the second regression arm exists to catch it).
	if !tipFromLandedRecord {
		if ok, record := deliveryProofLandedRecorded(taskID, branch, tip); ok {
			out.State = deliveryProofDelivered
			out.Note = fmt.Sprintf("row %s: %s (%s) is delivered — the clone channel recorded the landed push of this very commit: %s",
				taskID, branch, branchDeleteShortHash(tip), record)
			return out, nil
		}
	}
	// THE VERDICT, from what was MEASURED. `absent` is a claim of a completed
	// measurement — every containment question that could be asked was asked and
	// every one answered no — and `unverified` is the honest non-answer, which
	// KEEPS THE ROW OPEN WITHOUT REFUSING IT (deliveryProofRefusal records it and
	// lets the write through: refusing what cannot be seen is how the ledger was
	// wedged in the first place).
	//
	// The old code collapsed the two into `undelivered`. Measured on row
	// 01a183fa: the destination's own sha equalled the tip, the objects were NOT
	// readable, the content arm never ran — and the row was refused with a
	// deterministic "it did not land".
	if !saw.Measured {
		// THE SECOND WAY TO A MEASURED ABSENCE — a MEASUREMENT, not an inference,
		// and narrow on purpose. When the delivery POINT ITSELF named its sha (so
		// that sha is what the point holds NOW), that sha is readable here (so the
		// object store in front of us really contains the point's history), and the
		// tip is readable NOWHERE — then the point's history does not contain the
		// run's commit: a push deposits a commit's objects, so a commit the point
		// has never held was never delivered.
		//
		// EVERY clause is load bearing. Without LivePoint the shape is
		// indistinguishable from the incident: a stale CACHED ref that lacks the
		// tip says nothing about the point, and the tip being unreadable may mean
		// only that this host never fetched it. The reverse shape — the object
		// exists but no arm could compare — is not this case either, and stays
		// `unverified` below.
		if !saw.TipReadable && saw.LivePoint {
			out.State = deliveryProofAbsent
			out.Note = deliveryProofObjectAbsentNote(out, tip, tipSource, strings.Join(tried, ", "))
			return out, nil
		}
		out.State = deliveryProofUnverified
		out.Note = deliveryProofUnverifiedNote(out, tip, tipSource, strings.Join(tried, ", "), saw.Gaps,
			"NO containment arm could be asked at all")
		return out, nil
	}
	out.State = deliveryProofAbsent
	out.Note = deliveryProofAbsentNote(out, tip, tipSource, strings.Join(tried, ", "), contentWhy, saw)
	return out, nil
}

// deliveryProofUnverifiedNote is the sentence for a question that was never
// answered: it names the tip, the destinations that WERE read, and every arm
// that could not run. It contains no claim about the destination's contents —
// that is the whole point (row 01a183fa).
func deliveryProofUnverifiedNote(out deliveryProofOutcome, tip, tipSource, tried string, gaps []string, why string) string {
	gap := ""
	if len(gaps) > 0 {
		gap = " What could not be measured: " + strings.Join(deliveryProofDedup(gaps), "; ") + "."
	}
	return fmt.Sprintf(
		"run %s claims `%s` (branch %s, repo %s, ground %s): the delivery of the tip %s (%s) against %s is UNVERIFIED, "+
			"NOT disproved — %s.%s This is a statement about this arm's reach, not about the destination's contents: "+
			"the work may be there and unmeasurable from here. Refresh: %s. The write is NOT refused and the question is "+
			"recorded on the row's trail as %s (01a183fa).",
		out.TaskID, taskStatusCompleted, out.Branch, out.Repo, out.Ground,
		branchDeleteShortHash(tip), tipSource, tried, why, gap, out.Refresh, DeliveryProofUnverifiedEvent)
}

// deliveryProofAbsentNote is the sentence for a MEASURED absence: WHO claims
// delivery, WHICH commit the run made and where it came from, WHICH destination
// refs were read (and in which directories), WHAT the content comparison found —
// the closest destination commit and the per-file difference classes, so a reader
// can tell "the work is missing" from "only the measured base changed" (rows
// 01a17a67 / 01a17a5f were refused as the latter and closed as `dead`) — and what
// the two legal exits are.
//
// EVERY CLAUSE MUST HOLD. The subject list of the "in NEITHER … NOR …" clause is
// built from the arms that ACTUALLY RAN (`saw`), never from a fixed phrase: the
// pre-repair sentence said "NOR the content" while its own next sentence admitted
// the content comparison had not completed, and that single overreach refused a
// delivered row (01a183fa). A run that asked two of the three questions says so,
// and says which leg it could not take.
func deliveryProofAbsentNote(out deliveryProofOutcome, tip, tipSource, tried string, contentWhy []string,
	saw deliveryProofSaw) string {
	asked := []string{"the ancestry", "the patches"}
	if saw.ContentRan {
		asked = append(asked, "the content")
	}
	var content string
	if len(contentWhy) > 0 {
		content = " The content comparison (blank lines, comment-only lines and `" + deliveryProofMetadataMarker +
			"` metadata normalised away) found the run's added lines missing: " + strings.Join(deliveryProofDedup(contentWhy), " / ") + "."
	}
	return fmt.Sprintf(
		"run %s claims `%s` (branch %s, repo %s, ground %s) but %s: the tip %s (%s) is in %s of %s, "+
			"so the work exists only in the run's own ground.%s Deliver it (the channel's push must land it at the delivery point) "+
			"and write back again, or leave the row OPEN — a success terminal on an undelivered row is indistinguishable from a "+
			"delivered one on every surface that renders it (01a163df). Nothing was deleted, moved, restored or pushed by this arm",
		out.TaskID, taskStatusCompleted, out.Branch, out.Repo, out.Ground, deliveryProofMarker,
		branchDeleteShortHash(tip), tipSource, neitherNor(asked), tried, content)
}

// deliveryProofDedup drops repeated sentences, preserving order: the same
// commit is measured once per destination, and a refusal that repeats itself
// reads as noise rather than as evidence.
func deliveryProofDedup(lines []string) []string {
	seen := make(map[string]bool, len(lines))
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	return out
}

// deliveryProofUnknownNote is the honest non-answer: it NAMES what could not be
// read, so it is never mistaken for a verdict.
func deliveryProofUnknownNote(out deliveryProofOutcome, why string) string {
	return fmt.Sprintf("delivery proof for %s (branch %s) NOT judged: %s — the question was never answered, "+
		"which is not the same as an answer of \"it did not land\"", out.TaskID, out.Branch, why)
}

// deliveryProofAllowance is the ONE reading of the operator's escape: WHICH
// source asked for it and whether it is in force at all. It exists so the flag
// and the environment variable cannot diverge (the epic's hard constraint: no
// "flag works, env does not" fork) — the two inputs are resolved by exactly one
// function, and the caller branches on its verdict, never on either input.
type deliveryProofAllowance struct {
	// InForce is true when EITHER source asked for the release.
	InForce bool
	// ByFlag / ByEnv say WHERE the release came from, so the printed notice and
	// the trail record name the actual decision rather than a guess.
	ByFlag bool
	ByEnv  bool
}

// deliveryProofAllowanceFor is THE judgement point (Rule 14: one decision, one
// place). `flag` is `gopi task update --force`; the environment variable is the
// older face of the same escape. Both feed ONE boolean; neither can take effect
// on a path the other does not.
func deliveryProofAllowanceFor(flag bool) deliveryProofAllowance {
	env := config.EnvBool(deliveryProofAllowEnv)
	return deliveryProofAllowance{InForce: flag || env, ByFlag: flag, ByEnv: env}
}

// source names where the allowance came from, for the trail record and the
// printed notice — never guessed, and never reporting a source that did not
// ask (a `--force` run with the env ALSO set says both, because both are true).
func (a deliveryProofAllowance) source() string {
	switch {
	case a.ByFlag && a.ByEnv:
		return cliverbs.TaskUpdateForceFlag + " and " + deliveryProofAllowEnv
	case a.ByFlag:
		return cliverbs.TaskUpdateForceFlag
	case a.ByEnv:
		return deliveryProofAllowEnv
	}
	return ""
}

// deliveryProofAllowedNote is the SENTENCE a released write leaves behind: it
// keeps the arm's own measurement AND states that the write went through
// unproven, by whose decision, and where that decision is recorded. Rule 15's
// loud half — a waived refusal is never silent, and it can never be mistaken for
// a proof.
func deliveryProofAllowedNote(note string, a deliveryProofAllowance) string {
	return fmt.Sprintf("delivery proof WAIVED by %s: this write proceeds WITHOUT the proof that the deliverable reached "+
		"its declared destination, and the waiver is recorded on this row's trail as %s. %s",
		a.source(), DeliveryProofUnverifiedEvent, note)
}

// deliveryProofRefusal is the close-out seam's face: it refuses the success
// terminal when the deliverable is provably NOT at its declared destination,
// records the verdict on the row's trail either way (Rule 15 — the still-open
// row explains itself, and a permitted or unjudgeable write is never silent),
// and returns the note for the caller's error.
//
// `force` is `gopi task update --force`, the CLI face of the operator's escape;
// it is passed to deliveryProofAllowanceFor, the ONE place that decides, so the
// flag and GOPI_TASK_DELIVERY_PROOF_ALLOW behave identically by construction.
func deliveryProofRefusal(taskID string, force bool) (string, bool) {
	out, err := TaskDeliveryProof(taskID)
	if err != nil {
		taskNotifyLog("delivery proof for %s: %v", taskID, err)
		return "", false
	}
	switch out.State {
	case deliveryProofAbsent:
		if allow := deliveryProofAllowanceFor(force); allow.InForce {
			// The operator's decision: the write proceeds, and the row carries
			// the fact that it was allowed WITHOUT proof. The record name is
			// `delivery-proof-unverified` — the SAME name an unjudgeable write
			// carries, chosen deliberately: on this trail that name means "no
			// proof was produced for this row", and its presence is what
			// distinguishes a waived close from a PROVEN one (which writes
			// nothing at all). The detail says which face was used.
			deliveryProofEvent(taskID, DeliveryProofUnverifiedEvent, deliveryProofAllowedNote(out.Note, allow))
			taskNotifyLog("delivery proof: WAIVED %s — %s", taskID, allow.source())
			fmt.Fprintf(os.Stderr, "[task] delivery proof: WAIVED %s by %s — the write proceeds unverified; "+
				"recorded on the row's trail as %s\n",
				taskID, allow.source(), DeliveryProofUnverifiedEvent)
			return "", false
		}
		deliveryProofEvent(taskID, deliveryProofOwedEvent, out.Note)
		// THE ROW GOES ON THE DELIVERY QUEUE, not just on stderr. The trail
		// record above is this arm's own name, which NOTHING scans for; the
		// retry path scans for `delivery-blocked` and this writes exactly that
		// name (task_delivery_owed_refusal.go), so the refused row is claimable
		// by `gopi task retry-delivery` the moment the write is refused rather
		// than only in the refusal's own output.
		taskDeliveryRefusedQueue(taskID, out.Note)
		taskNotifyLog("delivery proof: REFUSED %s — %s", taskID, out.Note)
		fmt.Fprintln(os.Stderr, "[task] "+out.Note)
		return out.Note, true
	case deliveryProofUnverified:
		// THE ROW STAYS OPEN ONLY IF SOMETHING ELSE SAYS SO; this arm does not
		// judge it dead. It writes the loud record and returns "allowed" — an
		// unanswerable question is never a refusal (that refusal is exactly what
		// wedged row 01a183fa while its work sat on the integration branch).
		deliveryProofEvent(taskID, DeliveryProofUnverifiedEvent, out.Note)
		taskNotifyLog("%s", out.Note)
		fmt.Fprintln(os.Stderr, "[task] "+out.Note)
	}
	return "", false
}

// deliveryProofEvent writes one attributed trail entry, bounded by the same
// per-row cap the residue and tip arms use (Rule 13: one spelling of the cap),
// so a re-landed writeback cannot drip a second identical record.
func deliveryProofEvent(taskID, event, detail string) {
	if strings.TrimSpace(detail) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("delivery proof event %s: %v", taskID, err)
		return
	}
	defer db.Close()
	if taskEventCountForDetailOn(ctx, db, taskID, event, detail) >= reclaimRunResidueRefusalLimit {
		return
	}
	taskEventWrite(ctx, db, taskID, event, detail)
}

// neitherNor renders the subject list of the absence sentence — "NEITHER a NOR b
// NOR c" — from the arms that ACTUALLY RAN. The list is DATA (measured, in
// deliveryProofSaw) and a hard-coded phrase is exactly what the repair removed:
// the pre-repair sentence claimed a content comparison it had just admitted it
// could not complete (Rule 1: a sentence assembled in two places drifts).
func neitherNor(items []string) string {
	if len(items) == 0 {
		return "NONE"
	}
	if len(items) == 1 {
		return "NEITHER " + items[0] + " NOR anything else this arm can compare"
	}
	// "NEITHER a NOR b NOR c": the first word carries the pairing and every
	// following subject repeats NOR, so no subject is introduced twice.
	return "NEITHER " + strings.Join(items, " NOR ")
}

// deliveryProofRefreshOff is the RED PROOF's switch for the pre-judgement
// refresh, in the shape the other arms use (deliveryProofContentArmOff,
// deliveryContentPatchIDOff): a package variable, never a parameter, so a
// production caller cannot choose a staler view, while a test can remove the
// refresh and watch the same fixture's verdict change.
var deliveryProofRefreshOff bool

// deliveryProofRefreshDestination runs the ONE bounded fetch this arm performs
// before judging, in the directory whose view the judgement depends on: the
// GROUND first (it holds the run's commits, and a refreshed ground makes the
// destination's objects readable where the containment arms run), then the
// recorded repo (the witness that survives the ground's reclamation).
//
// It answers a sentence, never a verdict: "refreshed <dir>" when a fetch ran, or
// the reason it could not. The caller records it on the outcome so an operator
// reading a refusal knows whether the view was current (Rule 15 — a stale view
// that could not be refreshed must not be silently judged as if it were).
//
// It is deliberately NOT a retry mechanism (Rule 35): nothing loops, nothing
// re-arms, and the fetch is part of the measurement's own precondition — "look
// at the destination as it is now" — not a way to make a failing check pass. A
// fetch that changes nothing leaves the verdict exactly where it was.
func deliveryProofRefreshDestination(ctx context.Context, ground, repo string) (note, dir string, fetched bool) {
	if deliveryProofRefreshOff {
		return "the refresh is DISABLED (test switch deliveryProofRefreshOff): the judgement reads the cached view as it is",
			"", false
	}
	var reasons []string
	for _, candidate := range []string{strings.TrimSpace(ground), strings.TrimSpace(repo)} {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err != nil {
			reasons = append(reasons, fmt.Sprintf("%s is not on disk", candidate))
			continue
		}
		if deliveryProofGitRead(ctx, candidate, "remote", "get-url", deliveryProofOriginRemote) == "" {
			reasons = append(reasons, fmt.Sprintf("%s has no %s remote to refresh from", candidate, deliveryProofOriginRemote))
			continue
		}
		fctx, cancel := context.WithTimeout(ctx, deliveryProofRefreshTimeout)
		_, err := branchDeleteGit(fctx, candidate, "fetch", "--quiet", "--prune", deliveryProofOriginRemote, deliveryProofRefreshRefspec)
		cancel()
		if err != nil {
			// A failed refresh is REPORTED and the arm carries on with the view it
			// has; whatever it concludes from that view is judged on its own merits
			// (and an unmeasurable question lands on `unverified`, not on a refusal).
			reasons = append(reasons, fmt.Sprintf("the refresh of %s failed: %v", candidate, err))
			continue
		}
		return fmt.Sprintf("refreshed %s from %s before judging", candidate, deliveryProofOriginRemote), candidate, true
	}
	if len(reasons) == 0 {
		return "no directory to refresh: the row records neither a ground nor a repo", "", false
	}
	return "the destination view could NOT be refreshed — " + strings.Join(reasons, "; "), "", false
}

// deliveryProofContentPairs names the (srcDir, destDir) pairs the content arm
// may run in, with the REFRESHED directory first in BOTH roles when the
// pre-judgement fetch succeeded: that directory is the only one whose view of
// the destination is current, and whose object store therefore holds both the
// run's commits and the destination's tip. The rest is
// deliveryProofContentDirs' own rule (ground first, then the recorded repo,
// duplicates dropped) — this function adds ONE directory to that list and
// delegates, rather than re-implementing the pairing.
func deliveryProofContentPairs(ground, repo, refreshDir string, refreshed bool) [][2]string {
	if !refreshed || strings.TrimSpace(refreshDir) == "" {
		return deliveryProofContentDirs(ground, repo)
	}
	return deliveryProofContentDirs(refreshDir, "")
}

// deliveryProofTipReadableAnywhere reports whether ANY of these directories can
// resolve the tip's commit — the precondition for every patch and content
// comparison (they all read the run's own object).
func deliveryProofTipReadableAnywhere(ctx context.Context, dirs []string, tip string) bool {
	for _, dir := range dirs {
		if deliveryProofCommitReadable(ctx, dir, tip) {
			return true
		}
	}
	return false
}

// deliveryProofObjectAbsentNote is the sentence for the absence measured by
// LOOKING: the destination's history is readable here and does not carry this
// commit, and the commit itself is readable nowhere.
func deliveryProofObjectAbsentNote(out deliveryProofOutcome, tip, tipSource, tried string) string {
	return fmt.Sprintf(
		"run %s claims `%s` (branch %s, repo %s, ground %s) but %s: the tip %s (%s) is in NEITHER the ancestry "+
			"NOR the patches NOR the content of %s — the destination's own history is readable here and does not carry "+
			"that commit object at all, and the commit is readable nowhere else on this host, so no patch or content "+
			"comparison could have matched it either. A push deposits a commit's objects at its destination; a commit "+
			"the delivery point has never held was never delivered. Refresh: %s. Deliver it (the channel's push must "+
			"land it at the delivery point) and write back again, or leave the row OPEN (01a163df). Nothing was "+
			"deleted, moved, restored or pushed by this arm",
		out.TaskID, taskStatusCompleted, out.Branch, out.Repo, out.Ground, deliveryProofMarker,
		branchDeleteShortHash(tip), tipSource, tried, out.Refresh)
}

// TaskLandedDeliveryTip answers the tip THIS ROW's own clone-channel delivery
// recorded pushing, or "" when no such record exists — the reader the channel's
// empty gate needs to tell "this ground produced nothing" from "this ground's
// work is already on the integration branch" (row 01a182df).
//
// It is a thin EXPORT over the one parser (Rule 14: one implementation, so the
// writer TaskDeliveryRecordLanded and this reader cannot drift): the record is
// durable in the row's trail, which matters because the two facts usually meet
// in DIFFERENT processes — the round that pushed and the round that re-opened
// the ground and asked again.
func TaskLandedDeliveryTip(taskID, branch string) string {
	if strings.TrimSpace(branch) == "" {
		// The caller that has not resolved an address yet still has ONE the
		// reader accepts: the row's own `task/<id>` (deliveryProofRowAddresses
		// admits every name the row can legitimately be delivered under).
		branch = TaskGroundBranchName(taskID)
	}
	return deliveryProofLandedTip(taskID, branch)
}
