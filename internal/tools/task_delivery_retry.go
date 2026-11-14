package tools

// DELIVERY RETRY — the ledger-keyed repair path for a row whose delivery was
// BLOCKED (the alignment / pre-merge gate refused it), and the ONE place that
// answers "does the peer hold this row's branch?" before acting on it.
//
// WHY THIS FILE EXISTS (measured 2026-10-12, epics 01a12dad / 01a12dd3). The
// close-out chain asks its gates and refuses when one says no — correctly: a
// branch that does not merge, or does not build once merged, must not be
// pushed. What did not exist is a path that comes BACK to that row: each
// `--apply` replays the same blocked attempt, and when the blocker (a dirty
// main checkout, a peer master that conflicts) clears, nothing was waiting to
// notice. So the branch is on no peer ref and no pass advances it, while the
// ledger reads `completed`. Memory #1817's safety net is the shape this file
// implements: a ledger-keyed retry that
//
//	① asks the PEER first (read-only `ls-remote` — the delivery's own channel,
//	   never a push exit code, never a local ref), and
//	② is idempotent BY THE PEER'S ANSWER: a branch the peer already holds is
//	   skipped (nothing to do), and one the peer's master already CONTAINS is
//	   skipped too — pushing that would RESURRECT a ref the server's adoption
//	   chain deleted on purpose.
//	③ delivers through the ONE chain (align → published-anchor gate →
//	   pre-merge, i.e. the same `taskFinishCleanupChain` `--apply` runs; this
//	   file adds no second gate and no second push), and
//	④ verifies from the PEER afterwards, so "delivered" is the peer's own
//	   answer rather than this process's opinion.
//
// WHEN IT IS BLOCKED the outcome is NOT settled: the row's event trail gets a
// `delivery-blocked` record (Rule 15 — silence is the defect), the caller's
// line carries the `delivery owed: ` wording the plan face reads, and the next
// pass asks again. No daemon, no timer, no script (Rule 35): the pass that
// already ticks is the carrier.
//
// CAUTION (58b5618b, task 01a18799 slice 3/4): this paragraph used to say the
// resident lifecycle tick "already fires the finish-cleanup sweep". That sweep
// is retired, and with it the pass that used to call DeliveryRetryLines — the
// function is now called from NOWHERE in cmd/ or internal/agent, which is
// MEASURED and recorded, not assumed: internal/standards' cwd-release gate
// carries the reading and retires the arm as a trigger
// (TestGateCwdReleaseDeliveryRetryRetirement, cwdReleaseDeliveryRetryProducer's
// doc — `git grep -n 'DeliveryRetryTaskID\|DeliveryRetryLines\|DeliveryRetryJSON'
// -- cmd/agent internal/agent` prints nothing). Do not read the sentence above
// as "a resident pass retries this": the retry is reachable only through the
// library faces (DeliveryRetryOwedRowIDs / DeliveryRetryJSON /
// DeliveryRetryLines), and nothing in the tree schedules them today.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/devworkspace"
)

// The row's delivery state, as this path reports it. Each
// value is a DISPOSITION an operator acts on, so they are named constants
// (Rule 1/13) rather than literals at the call sites.
const (
	// deliveryRetryStatusDelivered — the peer now advertises the branch: this
	// attempt performed the delivery.
	deliveryRetryStatusDelivered = "delivered"
	// deliveryRetryStatusAlreadyDelivered — the peer already held the ref
	// before this attempt: nothing was done, and nothing was owed.
	deliveryRetryStatusAlreadyDelivered = "already-delivered"
	// deliveryRetryStatusAlreadyMerged — the peer's master already CONTAINS
	// the branch: pushing it would resurrect a ref the server removed after
	// adopting the work.
	deliveryRetryStatusAlreadyMerged = "already-merged"
	// deliveryRetryStatusInherited — the row's ground stands on a tip the
	// integration branch ALREADY CONTAINS (`merge-base --is-ancestor HEAD
	// origin/<base>` says so), so there is nothing to publish and nothing to
	// pull. Its own status, NOT the default `blocked`: a repair that reported
	// this `blocked` would hold a delivered row open forever, which is exactly
	// the defect (measured: 49 rows matching 交付没有落地).
	deliveryRetryStatusInherited = "inherited"
	// deliveryRetryStatusNoLocalBranch — no local ref: there is nothing to
	// hand over (an earlier pass pushed it and dropped the ref). Recorded as a
	// successful no-op so it can never read as `src refspec … does not match
	// any`, which reads as a FAILED delivery on a row that was delivered fine.
	deliveryRetryStatusNoLocalBranch = "no-local-branch"
	// deliveryRetryStatusBlocked — the gates refused and the peer does not
	// have the branch: the row still OWES its delivery.
	deliveryRetryStatusBlocked = "blocked"
	// deliveryRetryStatusNoChannel — no peer resolved on this host: there is
	// no delivery channel to retry through (single-node deployment, or an
	// alias git may not be handed).
	deliveryRetryStatusNoChannel = "no-channel"
	// deliveryRetryStatusUnreadable — the row or its deliverable could not be
	// read: reported, never guessed at.
	deliveryRetryStatusUnreadable = "unreadable"
)

// The event names this path writes to a row's own trail (an operator greps for
// them, and the owed-scan below keys on them).
const (
	// DeliveryRetryBlockedEvent — the gates refused and the peer is without
	// the branch. Its presence as the row's NEWEST event is what the owed-scan
	// below selects. CORRECTED 2026-10-30 (task 01a18ef7): this used to say the
	// resident lifecycle pass scans for it "so the retry rides the tick that
	// already runs" — no pass runs it any more. EXPORTED
	// because two faces print it (the verb's "nothing to do" line and the
	// pass's log), and a second spelling of an event name would silently scan
	// for nothing (Rule 14).
	DeliveryRetryBlockedEvent = "delivery-blocked"
	// deliveryRetryBlockedEvent is the in-package alias the scan and the
	// writer read, so no call site repeats the literal (Rule 13).
	deliveryRetryBlockedEvent = DeliveryRetryBlockedEvent
	// deliveryRetryDeliveredEvent — the peer holds the branch after this
	// attempt (or already held it, which is stated in the detail).
	deliveryRetryDeliveredEvent = "delivery-verified"
	// deliveryRetryNoOpEvent — nothing to deliver (no local ref, already
	// merged into peer master). Recorded so a second pass is visibly a no-op
	// rather than silence.
	deliveryRetryNoOpEvent = "delivery-noop"
)

// deliveryRetryMaxRows bounds one pass's work: the retry fetches, merges and
// rebuilds per row, so a runaway ledger must not turn one tick into a fork
// bomb (the same contract as the sweep's own caps).
const deliveryRetryMaxRows = 10

// deliveryRetryOwedMarker is the wording a blocked row carries — the SAME
// marker the close-out chain's own reason composer prefixes
// (finishCleanupDeliveryOwedSkipMarker), so the plan face, the settled
// classifier and this report cannot disagree about what "owed" means.
const deliveryRetryOwedMarker = finishCleanupDeliveryOwedSkipMarker

// DeliveryRetryReport is one row's answer: what the peer held BEFORE anything
// ran, what this attempt did, what the peer holds AFTER (asked, never inferred).
type DeliveryRetryReport struct {
	TaskID  string
	Branch  string
	Repo    string
	Target  string
	Status  string
	Detail  string
	PeerTip string
	// RowTip is the tip THIS ROW owes the peer — read from the tree its work
	// lives in (a clone ground's HEAD; a linked worktree's branch in the main
	// checkout). "" when no such tip can be read, which is a different answer
	// from "the peer holds something".
	RowTip string
	// Blocked is the reason the row still owes its delivery ("" unless the
	// gates refused and the peer does not hold the branch).
	Blocked string
	// Clone is the CLONE CHANNEL's own result when the row's ground was a clone
	// (nil for every other shape). It is the attempt's own record: a
	// clone-ground row's delivery is its ground's END-hook publish (Rule 6c),
	// and the clone channel reports that.
	Clone *devworkspace.Result
}

// Owed reports whether this row still owes its delivery after the attempt.
func (r *DeliveryRetryReport) Owed() bool {
	return r != nil && r.Status == deliveryRetryStatusBlocked
}

// DeliveryRetryLine renders the one line an operator (and the pass log) reads.
func DeliveryRetryLine(r *DeliveryRetryReport) string {
	if r == nil {
		return "<nil>"
	}
	head := r.TaskID + " " + r.Status
	if r.Branch != "" {
		head += " branch=" + r.Branch
	}
	if r.Target != "" {
		head += " peer=" + r.Target
	}
	if r.Detail != "" {
		head += " — " + r.Detail
	}
	return head
}

// DeliveryRetryTaskID is the exported ONE-row entry point: given a terminal
// row's id it asks the peer, delivers through the existing gates when delivery
// is owed, and answers what the peer holds. Callers: the CLI verb
// (`gopi task retry-delivery`, itself retired 2026-10-30).
func DeliveryRetryTaskID(taskID string) (*DeliveryRetryReport, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("delivery retry: empty task id")
	}
	ctx, cancel := context.WithTimeout(context.Background(), finishCleanupChainTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, fmt.Errorf("delivery retry: open ledger: %w", err)
	}
	defer db.Close()
	row, err := deliveryRetryReadRow(ctx, db, taskID)
	if err != nil {
		return nil, err
	}
	return deliveryRetryOne(ctx, db, row), nil
}

// deliveryRetryRow is one ledger row as this path reads it: the deliverable
// (repo + branch + worktree) and the status.
type deliveryRetryRow struct {
	TaskID   string
	Status   string
	Repo     string
	Branch   string
	Worktree string
}

// deliveryRetryRow reads the row's deliverable out of the ledger. A missing
// row is an ERROR (a caller named it), never an empty attempt.
func deliveryRetryReadRow(ctx context.Context, db *sql.DB, taskID string) (deliveryRetryRow, error) {
	var status, blob string
	err := db.QueryRowContext(ctx,
		`SELECT COALESCE(status,''), COALESCE(deliverable,'') FROM tasks WHERE task_id = ?`, taskID).Scan(&status, &blob)
	if err == sql.ErrNoRows {
		return deliveryRetryRow{}, fmt.Errorf("delivery retry: no task with task_id=%s", taskID)
	}
	if err != nil {
		return deliveryRetryRow{}, fmt.Errorf("delivery retry: read task %s: %w", taskID, err)
	}
	d := finishCleanupParseDeliverable(blob)
	return deliveryRetryRow{
		TaskID:   taskID,
		Status:   status,
		Repo:     strings.TrimSpace(d.Repo),
		Branch:   strings.TrimSpace(d.Branch),
		Worktree: strings.TrimSpace(d.Worktree),
	}, nil
}

// deliveryRetryOne is the path itself, in the order the shape requires:
//
//	① the row must be terminal and must name a branch + repo (else the row is
//	   not a delivery candidate and nothing is attempted);
//	② a peer must resolve (no channel = the honest "cannot retry here");
//	③ THIS ROW's own tip is read from the tree its work lives in — the value
//	   every qualification below is asked about;
//	④ THE PEER IS ASKED (ls-remote): a ref carrying THIS row's tip is nothing
//	   to do; the resurrect guard asks whether the peer's master already
//	   CONTAINS this row's tip;
//	⑤ no local tip and no local ref — a recorded no-op, never a failed push;
//	⑥ only then does the ONE chain run (align → anchor gate → pre-merge →
//	   push), and the peer is asked AGAIN for the verdict — compared against
//	   this row's own tip, so a peer that merely advertises the NAME is not
//	   mistaken for a delivery.
func deliveryRetryOne(ctx context.Context, db *sql.DB, row deliveryRetryRow) *DeliveryRetryReport {
	rep := &DeliveryRetryReport{TaskID: row.TaskID, Branch: row.Branch, Repo: row.Repo}

	// ① the row itself. AN OPEN ROW THAT OWES A DELIVERY is a candidate too,
	// and it is the shape the close-out seam produces: the proof arm REFUSES
	// the terminal write and records the debt on the still-open row
	// (task_delivery_owed_refusal.go). Before this arm the verb answered "not a
	// delivery candidate" for exactly the rows that need it, because it read
	// the status first and stopped there.
	if !taskTerminalStatus(row.Status) {
		if why := deliveryRetryOpenOwedReason(ctx, db, row.TaskID); why != "" {
			rep.Status = deliveryRetryStatusBlocked
			rep.Detail = why
			// No record is written here: the debt WAS the finding (the row's
			// newest trail entry is the record), and re-appending it on every
			// tick would drip identical lines down an append-only trail.
			return rep
		}
	}
	if !taskTerminalStatus(row.Status) || row.Repo == "" || row.Branch == "" {
		rep.Status = deliveryRetryStatusUnreadable
		rep.Detail = "the row is not a delivery candidate (status=" + row.Status + ", repo/branch must both be recorded)"
		deliveryRetryRecord(ctx, db, rep)
		return rep
	}

	// ② the delivery channel — resolved against THE ROW's repo, not this
	// process's cwd: the ls-remote, the fetch and the push below all run in that
	// repo, and a git remote is a per-repository setting (see
	// finishCleanupRowTargetResolution — the ONE resolver).
	mainDir := finishCleanupMainRepoDir(row.Repo)
	target := finishCleanupRowTargetResolution(mainDir).SSHTarget
	rep.Target = target
	if target == "" {
		rep.Status = deliveryRetryStatusNoChannel
		rep.Detail = noServerTargetSkipReason("no peer resolved on this host, so there is no channel to retry the delivery through")
		deliveryRetryRecord(ctx, db, rep)
		return rep
	}

	// ③ THE ROW'S OWN TIP — what this row actually owes the peer, asked of the
	// tree its work lives in (a clone ground holds its own branch; a linked
	// worktree's branch lives in the main checkout). Every qualification below
	// is asked about THIS tip, never about the peer's own advertised one.
	rowTip := deliveryRetryRowTip(ctx, row, mainDir)
	rep.RowTip = rowTip

	// ④ the peer's own answer, read through the delivery's channel
	peerTip := deliveryRetryPeerTip(ctx, mainDir, target, row.Branch)
	if peerTip != "" {
		rep.PeerTip = peerTip
		// A ref the peer advertises is a NO-OP only when it carries THIS row's
		// tip. The measured defect this arm repairs (bug 01a17343, 2026-10-26):
		// the guard used to ask "does the peer's master contain the tip the peer
		// just advertised?" — a question that is TRUE BY DEFINITION whenever the
		// row's branch IS an integration branch (every CLONE-GROUND row records
		// deliverable.branch=master), so 78 measured failed rows answered
		// `already-merged` and performed no delivery at all, while their ground
		// HEAD was contained in NEITHER the peer's master nor the delivery
		// point's.
		if rowTip != "" && rowTip == peerTip {
			rep.Status = deliveryRetryStatusAlreadyDelivered
			rep.Detail = "the peer already advertises " + row.Branch + " at this row's own tip " + peerTip + " — nothing to do"
			// THE RECORD (bug 01a19d5f): this exit has just MEASURED that the
			// peer holds this row's own commit, which is the one fact the
			// delivery proof reads (`clone-delivery-landed`). Without it the row
			// reads as never delivered and the proof falls back to "carries no
			// commit to deliver".
			recordLanded(rep, row.Worktree, peerTip, deliveryRetryLandedArmPeerAdvertises, "", "")
			deliveryRetryRecord(ctx, db, rep)
			return rep
		}
		// The peer holds a DIFFERENT tip on that name. That is not a delivery,
		// and it is not a reason to stop either: the resurrect guard asks the
		// one question it was written for — has THIS row's work been adopted
		// into the peer's master? — about THIS row's tip.
		if deliveryRetryPeerMasterContains(ctx, mainDir, target, rowTip) {
			rep.Status = deliveryRetryStatusAlreadyMerged
			rep.Detail = "the peer's master already contains this row's own tip " + rowTip + " — left alone (pushing it would resurrect a ref the server's adoption chain removed on purpose)"
			deliveryRetryRecord(ctx, db, rep)
			return rep
		}
		if rowTip == "" {
			// NOTHING LOCAL TO COMPARE WITH — the branch was recycled after an
			// earlier delivery, so the peer's own ref is the only record there
			// is. Here the guard's original question is the right one: the ref
			// WAS the delivery, and "adopted into master" is a no-op either way.
			if deliveryRetryPeerMasterContains(ctx, mainDir, target, peerTip) {
				rep.Status = deliveryRetryStatusAlreadyMerged
				rep.Detail = "the peer's master already contains " + peerTip + " — left alone (pushing it would resurrect a ref the server's adoption chain removed on purpose)"
				deliveryRetryRecord(ctx, db, rep)
				return rep
			}
			rep.Status = deliveryRetryStatusAlreadyDelivered
			rep.Detail = "the peer already advertises " + row.Branch + " at " + peerTip + " — nothing to do"
			// THE RECORD (bug 01a19d5f), and this SECOND already-delivered exit
			// needs it just as much as the one above. Its condition is
			// `rowTip == ""` — the ordinary state of a row whose branch was
			// RECYCLED after an earlier delivery (the comment above), or whose
			// ground could not be read — and in that state the peer's advertised
			// ref is the ONLY record of what was delivered. Leaving it out made
			// this face's fix PARTIAL in a way a standalone stopwatch hid: the
			// same test was green alone and red inside the suite, because which
			// of the two exits a fixture reaches depends on whether the ground's
			// HEAD can be read at that instant.
			recordLanded(rep, row.Worktree, peerTip, deliveryRetryLandedArmPeerAdvertises, "", "")
			deliveryRetryRecord(ctx, db, rep)
			return rep
		}
	}

	// ⑤ no local ref: there is nothing to hand over. Named explicitly so this
	// can never surface as git's "src refspec … does not match any", which an
	// operator reads as a FAILED delivery on a row that was delivered fine.
	if rowTip == "" && !finishCleanupBranchExists(ctx, mainDir, row.Branch) {
		rep.Status = deliveryRetryStatusNoLocalBranch
		rep.Detail = finishCleanupBranchAbsentDeliveryNote
		deliveryRetryRecord(ctx, db, rep)
		return rep
	}

	// ⑥ DELIVERY — WHICH DELIVERY DEPENDS ON THE GROUND'S SHAPE, and the shape
	// is a FACT (`.git` is a DIRECTORY = a clone of its own; a FILE = a
	// registration in the main checkout), never a branch name.
	//
	//	A CLONE GROUND delivers through the CLONE CHANNEL
	//	(devworkspace.AttemptDelivery: clean → pull --rebase → the DECLARED
	//	build → push origin HEAD:<branch>), because that is the channel that
	//	created the ground and the only one that owns its push. Handing such a
	//	row to the close-out chain is the measured defect this arm repairs (bug
	//	01a174c3, 2026-10-26): every clone ground records
	//	`deliverable.branch = master` (the channel pushes HEAD:master), and the
	//	close-out chain SKIPS master/main as the INTEGRATION branch
	//	(finishCleanupExemptBranchesResolved) — correct for a chain whose job is
	//	to clean up a merged feature branch, and a permanent block for a row
	//	whose whole deliverable is the push. 99 of 103 failed rows on this
	//	host measured `delivery owed: branch master is exempt`.
	//
	//	Anything else (a linked worktree, no worktree) goes through the ONE
	//	close-out chain exactly as before.
	if deliveryProofIsCloneGround(row.Worktree) {
		return deliveryRetryCloneDelivery(ctx, db, row, rep, mainDir, target)
	}

	// THE CLOSE-OUT CHAIN ASK IS RETIRED (2026-10-30, task 01a18799, slice 3/4).
	// The retry used to hand the row to the worktree+branch close-out chain and
	// report that chain's verdict as the attempt's detail. The chain is gone
	// (Rule 6c: a clone ground publishes itself at its own END hook), so what
	// this pass reports below is the peer's answer, read after nothing was
	// pushed — a true report of "this row still owes".
	//
	// HONEST GAP, named rather than hidden (Rule 26): DeliveryRetryTaskID has no
	// production caller today (the `retry-delivery` verb was retired in slice
	// 2/4), so this file is a library with tests, not a live leg. It stays in
	// this slice because removing it is a decision about a DIFFERENT mechanism —
	// the delivery-owed event family that task_delivery_owed_*.go still writes
	// and scans — and two deletions in one slice is how coverage gets lost.

	// ⑦ the verdict is the PEER's, asked after the attempt — and it is about
	// THIS ROW's tip, not merely about the peer happening to advertise the
	// name. A peer tip that still differs from the row's own is not a
	// delivery: answering "delivered" there would report a push that never
	// happened (the same measured confusion step ④ above repairs).
	if tip := deliveryRetryPeerTip(ctx, mainDir, target, row.Branch); tip != "" {
		rep.PeerTip = tip
		if rowTip == "" {
			rowTip = deliveryRetryRowTip(ctx, row, mainDir)
			rep.RowTip = rowTip
		}
		if rowTip == "" || tip == rowTip {
			rep.Status = deliveryRetryStatusDelivered
			rep.Detail = "the peer now advertises " + row.Branch + " at " + tip
			// NO `recordLanded` HERE, and that is a MEASUREMENT rather than an
			// oversight (bug 01a19d5f enumerated every success exit; this one was
			// examined and is not a fourth face). Step ④ above RETURNS on every
			// branch once the peer advertises anything at all, so this exit is
			// reached only when the peer's first answer was EMPTY — and the
			// non-clone arm pushes nothing (the close-out chain is retired, see
			// above), so between the two reads nothing here can move the peer's
			// ref. It therefore fires only if the peer changes on its own, which
			// no fixture can produce and which means this exit cannot be VERIFIED
			// by a test — and a write no test can drive is the same vacuum this
			// bug was filed about, one shape over (Rule 31). If this arm ever
			// becomes reachable by its own action, it must record like the other
			// three: `recordLanded(rep, row.Worktree, tip,
			// deliveryRetryLandedArmPeerAdvertises, "", "")`.
			deliveryRetryRecord(ctx, db, rep)
			return rep
		}
	}
	// Not delivered: the row still owes it. The reason is the gates' own — the
	// tree still carries the work and the peer still does not have it — stated
	// through the row's recorded deliverable rather than through a chain report
	// that no longer exists (never silence: Rule 15).
	why := deliveryRetryOwedReason(row)
	rep.Blocked = why
	rep.Status = deliveryRetryStatusBlocked
	rep.Detail = why
	deliveryRetryRecord(ctx, db, rep)
	return rep
}

// deliveryRetryCloneDelivery delivers a CLONE-GROUND row through the channel
// that created its ground (devworkspace.AttemptDelivery), and reports the
// PEER's own answer afterwards.
//
// WHY NOT THE CLOSE-OUT CHAIN (bug 01a174c3, measured 2026-10-26). A clone
// ground is a clone of its own and it records `deliverable.branch = master`,
// because the channel's push is `push origin HEAD:master`. The close-out chain
// treats master/main as the INTEGRATION branch and SKIPS it
// (finishCleanupExemptBranchesResolved) — a correct exemption for a chain whose
// job is to recycle a MERGED FEATURE branch, and a permanent block for a row
// whose entire deliverable IS that push. Measured on this host before the fix:
//
//	$ gopi task retry-delivery --task 01a16deb-73df-7a96-851d-97297f619246
//	… blocked branch=master peer=peer1:/home/user/work/gopi — delivery owed: branch master is exempt
//
// while the ground's own HEAD was contained in neither the peer's master nor
// any other peer ref. 99 of 103 failed rows on this host were that shape.
//
// WHAT IS DELIBERATELY NOT DONE HERE: the exemption itself is untouched (it
// lives in the close-out chain and it is right there — this file never relaxes
// it, it ROUTES around it for the ground shape the chain was never about). No
// second gate is added either: the channel's own steps (clean → pull --rebase →
// declared build → push) ARE the gate, and the build command is the DECLARED one
// (Rule 12) or the attempt refuses by name.
//
// THE VERDICT IS THE PEER'S, asked AFTER the attempt and compared against THIS
// ROW's own tip — a peer that merely advertises the name is not a delivery
// (the same distinction step ④ repairs).
func deliveryRetryCloneDelivery(ctx context.Context, db *sql.DB, row deliveryRetryRow, rep *DeliveryRetryReport, mainDir, target string) *DeliveryRetryReport {
	ground := row.Worktree
	cfg, cfgErr := deliveryRetryCloneConfig(ctx, ground, row)
	if cfgErr != nil {
		// The DECLARATION is unreadable/unusable (dev_repo, workspace root, the
		// repos build table): a row the channel cannot be pointed at, reported
		// in the channel's own wording rather than as a chain skip.
		rep.Blocked = deliveryRetryChannelReason(cfgErr.Error())
		rep.Status = deliveryRetryStatusBlocked
		rep.Detail = rep.Blocked
		deliveryRetryRecord(ctx, db, rep)
		return rep
	}
	// THE TIP THIS ROW OWES, READ BEFORE THE CHANNEL RUNS. The channel's own
	// first step is `pull --rebase origin <branch>`, which MOVES this ground —
	// so a tip read afterwards is the peer's tip, and every comparison with it
	// is a value against itself. That is exactly how a measured 2026-10-26 run
	// reported `delivered` while the peer's ref never moved: the ground was
	// fast-forwarded onto the peer, the push became an empty no-op that git
	// still exits 0 on, and the post-attempt comparison could not tell.
	rowTipBefore := deliveryProofGitRead(ctx, ground, finishCleanupPreMergeRevParseVerb, taskGitHeadToken)
	peerTipBefore := deliveryRetryPeerTip(ctx, mainDir, target, row.Branch)
	res, blk, err := deliveryRetryAttemptDelivery(ctx, cfg, ground)
	rep.Clone = &res
	switch {
	case blk != nil && blk.Kind == devworkspace.BlockerKindEmpty:
		// "THERE IS NOTHING HERE TO DELIVER" IS NOT A FAILED DELIVERY
		// (bug 01a1827e, measured 2026-10-28). The empty gate answers
		// `rev-list --count HEAD --not origin/<base>` == 0 for THIS directory,
		// which is what a ground that has ALREADY delivered looks like once the
		// channel's own `pull --rebase` (or an earlier round) fast-forwarded it
		// onto the peer: the row's work is on the peer, so the count is 0 — the
		// number describes "already delivered", and it used to be reported as
		// `blocked` forever because `case blk != nil` claimed it first and the
		// `already-merged` exit below was unreachable for this kind.
		//
		// The distinction is made where the channel cannot make it: the peer's
		// own answer about THIS ROW'S work, read from rowTipBefore — the tip
		// taken BEFORE the pull could move this ground.
		//
		// NOT A RELAXATION: every other kind (push refused, build failed, dirty
		// worktree, pull conflict) still falls through to `case blk != nil`, and
		// an empty ground whose work the peer does NOT contain stays blocked
		// with the channel's own empty sentence.
		//
		// WHICH SHAPE THIS ARM OWNS, and why `inherited` is NOT it (task: the
		// delivery retry's forward arm, 2026-10-30). The two statuses answer two
		// different measurements and they can both be true of one ground, so the
		// split is by WHICH ARM THE GATE'S KIND REACHED, not by re-testing the
		// peer inside each arm:
		//
		//   · THIS arm (`Kind == empty`) is reached only when the gate could NOT
		//     rank HEAD against the integration tip — HEAD *IS* that tip (a fresh
		//     ground cut from the base, which produced nothing of its own) or the
		//     peer refused to be measured. The ground stands ON the base, so the
		//     work at that tip is the base's own content; if the peer's master
		//     contains it, the row's work is on the peer and its branch ref is
		//     gone — that is `already-merged`, the "do not resurrect a ref the
		//     adoption chain removed" disposition.
		//   · The `inherited` arm below is reached when the gate DID rank them and
		//     found HEAD a STRICT ANCESTOR of the tip — content this run was
		//     handed, already on the integration branch, with the base moved on.
		//     That arm owns the disposition for this shape (see its comment).
		//
		// Keeping the containment question HERE — and not in the inherited arm —
		// is what keeps both statuses reachable and mutually exclusive: an
		// inherited ground never enters this arm, so the peer check below cannot
		// silently relabel it.
		if rowTipBefore != "" && deliveryRetryPeerMasterContains(ctx, mainDir, target, rowTipBefore) {
			rep.RowTip = rowTipBefore
			rep.PeerTip = deliveryRetryPeerTip(ctx, mainDir, target, row.Branch)
			// THE SENTENCE NAMES WHAT WAS MEASURED. The peer's ADVERTISED ref for
			// `row.Branch` is often EMPTY here — a clone-ground row records
			// `deliverable.branch = task/<id>`, a name the server's adoption chain
			// removed on purpose, and `ls-remote` answers nothing for it. So the
			// tip is named only when the peer really advertises one; otherwise the
			// sentence says MASTER, which is the ref the containment was measured
			// against (`gitPushbackCheckRef`). The first draft of this message
			// pasted the empty tip into the text ("the peer's  already contains"),
			// which is the defect that only a REAL run shows (Rule 31).
			held := "the peer's master"
			if rep.PeerTip != "" {
				held = "the peer's " + rep.PeerTip
			}
			rep.Status = deliveryRetryStatusAlreadyMerged
			rep.Detail = "this ground carries no commit to deliver (" + blk.Step + " answered 0) and " + held +
				" already contains this row's work " + rowTipBefore +
				" — left alone (the delivery landed; re-pushing would resurrect a ref the server's adoption chain removed on purpose)"
			deliveryRetryRecord(ctx, db, rep)
			return rep
		}
		rep.Blocked = deliveryRetryChannelReason(blk.Report())
	case blk != nil && blk.Kind == devworkspace.BlockerKindAlreadyDelivered:
		// THE OTHER MEANING OF THE SAME ZERO (row 01a182df), on the REPAIR face:
		// this row's own earlier push already moved `origin/<base>` onto its tip,
		// so the count is 0 because the delivery SUCCEEDED. It is neither
		// `delivered` (this attempt pushed nothing) nor `blocked` (nothing was
		// refused): the row's work is on the peer, which is the `already-delivered`
		// state the consumer already knows how to read as a success terminal.
		// A repair that reported it `blocked` would hold a delivered row open
		// forever — the same defect class, one face over.
		rep.Status = deliveryRetryStatusAlreadyDelivered
		rep.Detail = blk.Report()
		// THE RECORD (bug 01a19d5f), and WHICH TIP it names. The gate reached this
		// arm because the integration base already REACHES the tip an earlier
		// push of this row landed (cfg.DeliveredTip) — so the commit to record is
		// the ground's own HEAD as it stood BEFORE the channel ran, which is the
		// commit the peer holds. Only when that reading failed does the gate's own
		// input stand in; an empty tip is then SAID OUT LOUD by recordLanded
		// (Rule 15), never written as a record without a commit.
		landedTip := rowTipBefore
		if strings.TrimSpace(landedTip) == "" {
			landedTip = cfg.DeliveredTip
		}
		base := cfg.BaseBranch
		if strings.TrimSpace(base) == "" {
			base = devworkspace.DefaultBranch
		}
		recordLanded(rep, ground, landedTip, deliveryRetryLandedArmGate, base, "")
		deliveryRetryRecord(ctx, db, rep)
		return rep
	case blk != nil && blk.Kind == devworkspace.BlockerKindInheritedDelivery:
		// THE THIRD MEANING OF THE SAME ZERO (epic: inherited delivery), on the
		// REPAIR face: the integration branch already CONTAINS this ground's HEAD,
		// so the count is 0 because the content is there — not because the ground
		// is empty and not because this attempt pushed it. Nothing was refused, so
		// this must NOT take the default `blocked` branch below: that is the branch
		// that keeps a delivered row "owed" forever (measured: 49 rows matching
		// 交付没有落地). It is a no-op with its own status and its own sentence.
		rep.Status = deliveryRetryStatusInherited
		rep.Detail = blk.Report()
		deliveryRetryRecord(ctx, db, rep)
		return rep
	case blk != nil:
		// A step refused, in the tool's own words: the workspace is KEPT with
		// its commits (the channel never stashes or cleans the work).
		rep.Blocked = deliveryRetryChannelReason(blk.Report())
	case err != nil:
		// Not about the work at all (no declared build, a dead remote, a git
		// that cannot read the branch).
		rep.Blocked = deliveryRetryChannelReason(err.Error())
	case res.Pushed:
		// The push landed. The verdict below is still the PEER's, not this
		// process's opinion.
	default:
		rep.Blocked = deliveryRetryChannelReason(
			"the attempt reported neither a push nor a reason (detail: " + res.Detail + ")")
	}
	if rep.Blocked != "" {
		rep.Status = deliveryRetryStatusBlocked
		rep.Detail = rep.Blocked
		deliveryRetryRecord(ctx, db, rep)
		return rep
	}
	// The peer's answer, AFTER the push — and the verdict is about THIS ROW'S
	// WORK, never about the peer merely having some tip on that name.
	//
	// TWO FACTS ARE REQUIRED, and each was measured missing on its own:
	//
	//	① the peer's ref MOVED (peerTipAfter != peerTipBefore). A `push` of a
	//	   branch the peer is already on is an empty no-op that git still exits
	//	   0 on, so `res.Pushed` alone cannot carry this.
	//	② what the peer now has CONTAINS this row's work (rowTipBefore, read
	//	   before the channel's pull could move it). A peer tip unrelated to
	//	   the row is not a delivery.
	rep.RowTip = rowTipBefore
	tip := deliveryRetryPeerTip(ctx, mainDir, target, row.Branch)
	if tip == "" {
		// Fail-closed: a push this run believes landed, on a peer it cannot
		// read, is not a delivery it may claim.
		rep.Blocked = deliveryRetryChannelReason(
			"the push was accepted but the peer advertises no " + row.Branch + " afterwards (the peer could not be read, or the ref did not land)")
		rep.Status = deliveryRetryStatusBlocked
		rep.Detail = rep.Blocked
		deliveryRetryRecord(ctx, db, rep)
		return rep
	}
	rep.PeerTip = tip
	// THE PEER'S REF MUST HAVE MOVED. An unchanged tip means this attempt
	// delivered nothing, whatever the channel's own result says — and if the
	// peer already carries this row's work, the honest word for that is
	// `already-merged` (the server's adoption chain took it; re-pushing would
	// resurrect a ref it removed on purpose), not `delivered`.
	if tip == peerTipBefore {
		if rowTipBefore != "" && deliveryRetryPeerMasterContains(ctx, mainDir, target, rowTipBefore) {
			rep.Status = deliveryRetryStatusAlreadyMerged
			rep.Detail = "the peer's " + row.Branch + " did not move (" + tip + ") and already contains this row's work " +
				rowTipBefore + " — left alone (pushing it would resurrect a ref the server's adoption chain removed on purpose)"
			deliveryRetryRecord(ctx, db, rep)
			return rep
		}
		rep.Blocked = deliveryRetryChannelReason(
			"the channel reported a push but the peer's " + row.Branch + " is unchanged at " + tip +
				" — this row's work " + rowTipBefore + " is NOT on the peer")
		rep.Status = deliveryRetryStatusBlocked
		rep.Detail = rep.Blocked
		deliveryRetryRecord(ctx, db, rep)
		return rep
	}
	rep.Status = deliveryRetryStatusDelivered
	rep.Detail = cloneChannelFailurePrefix + "pushed " + row.Branch + " from the row's ground — the peer advanced from " +
		peerTipBefore + " to " + tip
	recordLanded(rep, ground, tip, deliveryRetryLandedArmCloneChannel, peerTipBefore, tip)
	deliveryRetryRecord(ctx, db, rep)
	return rep
}

// THE CLONE-DELIVERY-LANDED RECORD, ON EACH OF THIS FILE'S SUCCESS EXITS.
//
// WHY THIS FILE HAD TO LEARN TO WRITE IT (bug row 01a19d5f; the same defect
// 7303f30e repaired on two other faces). "The peer holds this row's work" is a
// statement about ONE COMMIT, and the row's own trail is the only place a later
// reader can find it: the delivery proof's third arm reads `clone-delivery-landed`
// records and nothing else (tools.TaskLandedDeliveryTip). Every exit below used to
// change the in-memory report and stop there, so a row this path had just
// verified as delivered still carried NO record of it — the proof answered "",
// the empty gate lost its second reading, and the row fell back to "carries no
// commit to deliver", which points a reader at a commit that is in fact on the
// peer.
//
// ONE WRITER, ONE GRAMMAR (Rule 14): TaskDeliveryRecordLanded, never a local copy
// of its format.
//
// THE DETAIL NAMES THE ARM AND WHAT IT MEASURED. The writer's default detail is
// "pushed to origin", and this file's exits mostly did NOT push: one found the
// peer already holding the tip, one read the peer advancing after the channel
// ran, one read the gate's own "this run's delivery already landed". A record
// that said "pushed" on any of them would be a claim about a push this face never
// made — exactly the confusion the episode was filed on.
const (
	// deliveryRetryLandedArmPeerAdvertises — this file's PEER step found the
	// row's branch on the peer at this row's own tip: nothing was pushed.
	deliveryRetryLandedArmPeerAdvertises = "peer-advertises"
	// deliveryRetryLandedArmCloneChannel — the clone channel ran, the push was
	// accepted and the peer's ref ADVANCED from the tip it held before.
	deliveryRetryLandedArmCloneChannel = "clone-channel"
	// deliveryRetryLandedArmGate — the channel's own gate answered
	// `BlockerKindAlreadyDelivered`: it measured this run's push already reachable
	// from the integration base, so nothing was pushed by THIS attempt either.
	deliveryRetryLandedArmGate = "already-delivered-gate"
)

// deliveryRetryLandedPeerDetailFmt / …CloneChannelFmt / …GateFmt are the three
// detail sentences, one per arm (Rule 1b: the arm is an identifier, the sentence
// is composed where the measurement is, and a test asserts the ARM token so the
// three exits can never be mistaken for one another).
const (
	deliveryRetryLandedPeerDetailFmt = "the peer already advertised %s at %s " +
		"(recorded by the delivery repair's %s arm, which pushed nothing itself)"
	deliveryRetryLandedCloneChannelFmt = "the clone channel pushed %s from the row's ground and the peer's ref " +
		"advanced from %s to %s (recorded by the delivery repair's %s arm after the channel's own push)"
	deliveryRetryLandedGateFmt = "the peer's %s reaches this row's tip %s, so the delivery already landed " +
		"(recorded by the delivery repair's %s arm, which pushed nothing itself)"
	// deliveryRetryLandedNoTipFmt is the Rule 15 sentence for "the delivery was
	// verified but its record cannot name the commit": the tip could not be read,
	// so the row carries no tip for the proof to judge. It names the ARM and the
	// directory — the two things a later reader needs to decide whether the
	// missing record is a defect or a discard.
	deliveryRetryLandedNoTipFmt = "delivery retry: the delivery is verified but its `%s` record could NOT be written " +
		"(arm=%s, ground=%s): the tip could not be read, so this row carries no tip for the delivery proof to judge"
)

// recordLanded writes this path's clone-delivery-landed record for ONE success
// exit, through the ONE writer.
//
// THE BRANCH IS THE ROW'S OWN ADDRESS, resolved here rather than at three call
// sites: the row's recorded branch when it has one, and the ledger address
// (TaskGroundBranchName) when it does not — the reader admits both
// (deliveryProofRowAddresses), so the record must be filed under a name the
// reader will actually count.
func recordLanded(rep *DeliveryRetryReport, ground, tip, arm, from, to string) {
	if rep == nil {
		return
	}
	branch := strings.TrimSpace(rep.Branch)
	if branch == "" {
		branch = TaskGroundBranchName(rep.TaskID)
	}
	ground = strings.TrimSpace(ground)
	if strings.TrimSpace(tip) == "" {
		// RULE 15: the delivery IS verified, and the record cannot name it. Said
		// out loud (this package's log face is stderr, which the invoking console
		// shows) rather than returning silently — a missing record must be
		// distinguishable from an exit that never tried.
		taskNotifyLog(deliveryRetryLandedNoTipFmt, DeliveryLandedEventName, arm, ground)
		return
	}
	detail := ""
	switch arm {
	case deliveryRetryLandedArmPeerAdvertises:
		detail = fmt.Sprintf(deliveryRetryLandedPeerDetailFmt, branch, tip, arm)
	case deliveryRetryLandedArmCloneChannel:
		detail = fmt.Sprintf(deliveryRetryLandedCloneChannelFmt, branch, from, to, arm)
	case deliveryRetryLandedArmGate:
		detail = fmt.Sprintf(deliveryRetryLandedGateFmt, from, tip, arm)
	default:
		detail = fmt.Sprintf(deliveryRetryLandedPeerDetailFmt, branch, tip, arm)
	}
	TaskDeliveryRecordLanded(rep.TaskID, branch, ground, tip, detail)
}

// cloneChannelFailurePrefix opens every clone-channel reason this path records.
// It is the channel's own prefix (devworkspace's lines and the run-time
// reporter use it too), so an operator reading a row's reason can tell a
// CHANNEL refusal from a close-out-chain skip without knowing either file.
//
// IT IS APPLIED BY deliveryRetryChannelReason AND NOWHERE ELSE: the bodies this
// file collects do not share one convention about the prefix — `blk.Report()`
// and most of the channel's errors already open with it, `ctx.Err()` and this
// file's own sentences do not — so a call site that pastes the constant itself
// either doubles it or gets it wrong for its group. Measured 2026-10-30: every
// `blk.Report()` arm read `delivery owed: clone channel: clone channel: …`.
//
// THE VALUE IS devworkspace's (moved 2026-10-31, Rule 43/Rule 1/13): the same
// words open this package's blocked rows, cmd/agent's delivery-loop reasons and
// internal/agent's end-hook lines, so the spelling has exactly ONE owner —
// devworkspace.CloneChannelPrefix — and this name is an alias, never a second
// literal. What stays here is the DELIVERY-RETRY face's own composition
// (deliveryRetryChannelReason): its owed marker, its empty-body sentence, and
// its own test pins.
const cloneChannelFailurePrefix = devworkspace.CloneChannelPrefix

// deliveryRetryChannelReasonOfNothing is what a blocked row says when the body
// it was handed is empty: the prefix over nothing is not a reason (Rule 15).
const deliveryRetryChannelReasonOfNothing = "the channel reported no reason (an empty refusal — the report is the defect)"

// deliveryRetryChannelReason composes a blocked row's reason: the owed marker,
// the channel's prefix ONCE, and the body that actually explained the failure.
//
// The prefix is idempotent here on purpose (see the test beside this file): the
// body a blocked arm holds may already carry the channel's wording —
// `blk.Report()` opens with it for every Kind, and `AttemptDelivery` composes
// most of its errors that way — or it may be bare (`ctx.Err()`, a settings
// refusal, a sentence this file writes). One strip, one re-add, and the reader
// sees what happened instead of a stutter.
func deliveryRetryChannelReason(body string) string {
	why := strings.TrimSpace(body)
	why = strings.TrimSpace(strings.TrimPrefix(why, cloneChannelFailurePrefix))
	if why == "" {
		why = deliveryRetryChannelReasonOfNothing
	}
	return deliveryRetryOwedMarker + cloneChannelFailurePrefix + why
}

// deliveryRetryAttemptDelivery is the ONE call this path makes to the channel,
// named so the routing is greppable from the router (a second call site would
// be a second policy).
func deliveryRetryAttemptDelivery(ctx context.Context, cfg devworkspace.Config, dir string) (devworkspace.Result, *devworkspace.Blocker, error) {
	return devworkspace.AttemptDelivery(ctx, cfg, dir)
}

// deliveryRetryCloneConfig resolves the declaration this delivery needs: the
// channel's settings (dev_repo, workspace root) and — Rule 12, never discovered
// — the BUILD the `repos` table declares for this repository.
//
// The declaration hangs on the ground's own origin first (the workspace says
// where it was cloned from), then the source repo's local path, then the row's
// own deliverable — the same precedence the run-time leg uses
// (cloneDeliveryConfig in cmd/agent), with the ground's answer first because a
// repair runs with the ground in hand.
func deliveryRetryCloneConfig(ctx context.Context, ground string, row deliveryRetryRow) (devworkspace.Config, error) {
	devRepo, _, err := config.DevRepoConfigured()
	if err != nil {
		return devworkspace.Config{}, fmt.Errorf("%s unreadable: %w", config.DevRepoSettingsKey, err)
	}
	root, rootSource, err := config.WorkspaceRootConfigured()
	if err != nil {
		return devworkspace.Config{}, fmt.Errorf("%s unusable: %w", config.WorkspaceRootSettingsKey, err)
	}
	cfg := devworkspace.Config{
		TaskID:     row.TaskID,
		DevRepo:    devRepo,
		Root:       root,
		RootSource: rootSource,
		Branch:     row.Branch,
		// THE ANCHOR: the directory the ROW records as its deliverable. A repair
		// runs in `ground`, and deliveryAnchorGate refuses when the two disagree
		// — so a repair never republishes from a directory that carries none of
		// this row's commits (defect 01a17dee; the same gate the run-time leg
		// uses, one implementation).
		Worktree: row.Worktree,
		// THE SECOND FACT THE EMPTY GATE NEEDS (row 01a182df): the tip this row's
		// own delivery already recorded pushing. A repair leg runs in the very
		// ground whose `origin/<base>` the earlier push moved onto that tip, so
		// without it `rev-list --count` answers 0 and the gate would report an
		// already-delivered row as an empty one — the same misclassification
		// this field exists to remove, on the second of the two faces that call
		// AttemptDelivery. "" when nothing was recorded, which is the ordinary
		// first-attempt answer.
		DeliveredTip: TaskLandedDeliveryTip(row.TaskID, row.Branch),
		// THE SUCCESS-PATH NARRATION REACHES ITS WRITER (row 01a182df). This
		// config carried no Log at all, so `attempt N/M: push`, `push accepted`
		// and the gate's own readings were composed and immediately dropped
		// (`Result.printf` returns silently on a nil writer). The repair leg's
		// operator therefore saw only the final one-line report and could not
		// tell an accepted push from an attempt that never ran — which is what
		// made a delivered row look like an empty one. os.Stderr is the writer
		// the invoking console already shows (`gopi task retry-delivery`), and
		// `--json` reads the machine shape from the report, not from this.
		Log: os.Stderr,
	}
	// A REPAIR DELIVERS TO THE DESTINATION THE ROW ALREADY DECLARED — it never
	// mints a new one. So GroundBranch is set only when the row's recorded
	// branch IS this row's own ledger address (TaskGroundBranchName), which is
	// the shape a post-fix clone claim records. Two rows are thereby told apart
	// by measurement, not by hope:
	//
	//   * a post-fix row (branch = task/<id>) → GroundBranch set, so
	//     AttemptDelivery republishes it under a lease. Without this the repair
	//     push would be a plain push, and a row whose ground was rebased since
	//     its last delivery is NOT a fast-forward — the repair would be refused
	//     for a reason that has nothing to do with the work.
	//   * a legacy row (branch = master) → GroundBranch stays empty, so the push
	//     is ordinary and a non-fast-forward is real information: that branch is
	//     shared with every other clone row, and moving it from a repair is
	//     exactly what the channel must not do.
	if own := TaskGroundBranchName(row.TaskID); own != "" && own == strings.TrimSpace(row.Branch) {
		cfg.GroundBranch = own
		cfg.BaseBranch = devworkspace.DefaultBranch
	}
	var looked []string
	if origin := deliveryProofGitRead(ctx, ground,
		"remote", "get-url", deliveryProofOriginRemote); origin != "" {
		if display, derr := devworkspace.SourceRepoDisplayPath(origin); derr == nil && display != "" {
			looked = append(looked, display)
		}
	}
	if display, derr := devworkspace.SourceRepoDisplayPath(devRepo); derr == nil && display != "" {
		looked = append(looked, display)
	}
	if rowRepo := strings.TrimSpace(row.Repo); rowRepo != "" {
		looked = append(looked, rowRepo)
	}
	for _, repo := range looked {
		rc, ok, err := config.ResolveRepoBuild(repo)
		if err != nil {
			return devworkspace.Config{}, fmt.Errorf("%s: %w", config.ReposSettingsKey, err)
		}
		if ok {
			cfg.Build = rc.Build
			cfg.BuildTimeout = time.Duration(rc.BuildTimeoutSec) * time.Second
			return cfg, nil
		}
	}
	// NO DECLARATION IS NOT AN ERROR HERE: cfg.Build stays empty on purpose so
	// AttemptDelivery refuses with its own sentence (Rule 12 — the command is
	// declared, never discovered, never defaulted).
	return cfg, nil
}

// deliveryRetryPeerTip asks the peer for the branch, with ONE read-only
// `ls-remote --heads` over the delivery's own addressing. "" means the peer
// does not advertise it (or cannot be read — fail-closed: a peer this run
// could not ask is not evidence of a delivery).
func deliveryRetryPeerTip(ctx context.Context, workDir, target, branch string) string {
	if strings.TrimSpace(target) == "" || strings.TrimSpace(branch) == "" {
		return ""
	}
	dir := workDir
	if dir == "" {
		dir = "."
	}
	// PARSED remote tip (`ls-remote` lines are split for the object id): stdout
	// only — stderr here carries progress and transport diagnostics.
	out, err := finishCleanupGitParsedStdout(ctx, dir, finishCleanupGitTimeout, branchDeleteLsRemoteVerb,
		branchDeleteLsRemoteHeadsFlag, target, branchDeleteRefsHeadsPrefix+branch)
	if err != nil {
		return ""
	}
	return branchDeleteLsRemoteTip(out)
}

// deliveryRetryPeerMasterContains reports whether the peer's master already
// contains tip. It fetches the peer's master into the reserved mirror ref the
// pre-merge gate itself uses (ONE spelling of that ref, Rule 14) and asks git
// an ancestry question — the same predicate `git branch -d` would make, asked
// against the PEER rather than the stale local master.
//
// A fetch that fails, or an ancestry question that cannot be answered, reads
// FALSE: the caller then treats the ref as live rather than resurrecting or
// deleting on a guess.
// deliveryRetryRowTip reads the tip THIS ROW owes the peer — the question the
// resurrect guard must actually be asked about.
//
// WHY IT IS NOT "WHAT THE PEER ADVERTISES" (bug 01a17343, measured 2026-10-26):
// a CLONE-GROUND row records `deliverable.branch = master` (the clone channel
// pushes `HEAD:master`, and its ground is a plain clone standing on its
// integration branch), so the peer's ref for that name IS the peer's own
// integration branch. Asking "does the peer's master contain the tip the peer
// advertises for this branch?" therefore compares a value with itself: it is
// true by construction, every such row answered `already-merged`, and 78
// measured failed rows performed NO delivery while their ground HEAD was
// contained in neither the peer's master nor the delivery point's.
//
// WHERE THE TIP LIVES depends on the ground's shape, and the shape is a FACT
// (`.git` is a DIRECTORY = a clone of its own; a FILE = a registration in the
// main checkout) — never inferred from the path spelling:
//
//	· CLONE GROUND (the row named a worktree that is its own repository) — the
//	  row's branch is checked out THERE, so its HEAD is the tip. A named
//	  worktree is asked FIRST and its answer wins: it is the tree the run
//	  actually worked in.
//	· LINKED WORKTREE / no worktree — the branch lives in the main checkout,
//	  so it is read from `refs/heads/<branch>` there.
//
// "" means no such ref could be read (a recycled branch, a directory that is
// gone) — a different answer from "the peer holds something", and the caller
// keeps the no-local-branch arm for it.
func deliveryRetryRowTip(ctx context.Context, row deliveryRetryRow, mainDir string) string {
	if wt := strings.TrimSpace(row.Worktree); wt != "" && deliveryProofIsCloneGround(wt) {
		return deliveryProofGitRead(ctx, wt, finishCleanupPreMergeRevParseVerb, taskGitHeadToken)
	}
	if strings.TrimSpace(row.Branch) == "" {
		return ""
	}
	dir := mainDir
	if strings.TrimSpace(dir) == "" {
		dir = row.Repo
	}
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	return deliveryProofGitRead(ctx, dir, finishCleanupPreMergeRevParseVerb,
		finishCleanupPreMergeVerifyFlag, refsHeadsPrefix+row.Branch)
}

func deliveryRetryPeerMasterContains(ctx context.Context, workDir, target, tip string) bool {
	if strings.TrimSpace(tip) == "" {
		return false
	}
	dir := workDir
	if dir == "" {
		dir = "."
	}
	if err := gitPushbackFetchMaster(ctx, dir, target); err != nil {
		return false
	}
	_, err := finishCleanupGit(ctx, dir, finishCleanupPreMergeMergeBaseVerb,
		finishCleanupPreMergeAncestorFlag, tip, gitPushbackCheckRef)
	return err == nil
}

// finishCleanupDeliveryOwedReason renders the blocked reason from the chain's
// own report, in the `delivery owed: ` vocabulary the plan face and the settled
// classifier already read. It states WICH gate stopped the chain (the failing
// step and its detail) so the operator's next move is named, not guessed.
func finishCleanupDeliveryOwedReason(chain *FinishCleanupReport) string {
	why := ""
	if chain != nil {
		if chain.Skipped {
			why = chain.SkipReason
		}
		if strings.TrimSpace(why) == "" {
			for _, s := range chain.Steps {
				if s.Step != finishCleanupPushVerb && !s.OK {
					why = s.Step + ": " + s.Detail
				}
			}
		}
	}
	if strings.TrimSpace(why) == "" {
		why = "no push step reached the peer (the gates did not let the delivery through)"
	}
	return deliveryRetryOwedMarker + why
}

// deliveryRetryRecord writes the row's own trail entry. The event NAME is the
// state, so an operator can find the rows that still owe a delivery by asking
// the ledger rather than re-deriving the state per row (Rule 15: the refusal is
// recorded where a human reads it).
func deliveryRetryRecord(ctx context.Context, db *sql.DB, rep *DeliveryRetryReport) {
	if db == nil || rep == nil {
		return
	}
	event := deliveryRetryNoOpEvent
	switch rep.Status {
	case deliveryRetryStatusBlocked:
		event = deliveryRetryBlockedEvent
	case deliveryRetryStatusDelivered, deliveryRetryStatusAlreadyDelivered, deliveryRetryStatusAlreadyMerged,
		deliveryRetryStatusInherited:
		event = deliveryRetryDeliveredEvent
	}
	taskEventWrite(ctx, db, rep.TaskID, event, rep.Detail)
}

// deliveryRetryOwedIDs returns the ids whose NEWEST trail event says their
// delivery is blocked — the ledger-keyed set. It is a READ and it never mutates
// anything; a row that has since been delivered carries a newer event and drops
// out by itself. NO PRODUCTION CALLER as of 2026-10-30 (its two consumers — the
// lifecycle pass and the retired `retry-delivery` verb — are gone); kept for the
// tests and filed as its own removal row rather than deleted inside this task.
func deliveryRetryOwedIDs(ctx context.Context, db *sql.DB, limit int) ([]string, error) {
	if db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = deliveryRetryMaxRows
	}
	rows, err := db.QueryContext(ctx, `
		SELECT e.task_id FROM task_events e
		WHERE e.event = ?
		  AND e.created_at = (SELECT MAX(e2.created_at) FROM task_events e2 WHERE e2.task_id = e.task_id)
		ORDER BY e.created_at ASC LIMIT ?`, deliveryRetryBlockedEvent, limit)
	if err != nil {
		return nil, fmt.Errorf("delivery retry scan: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("delivery retry scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("delivery retry scan: %w", err)
	}
	return ids, nil
}

// DeliveryRetryOwedRows is the exported read: the rows whose newest trail event
// is `delivery-blocked`. No production caller today (see deliveryRetryOwedIDs).
func DeliveryRetryOwedRows(ctx context.Context, db *sql.DB, limit int) ([]string, error) {
	return deliveryRetryOwedIDs(ctx, db, limit)
}

// DeliveryRetryLines runs the path over ids and returns one rendered line per
// row (Rule 14's one render, kept for the tests; no production caller today).
func DeliveryRetryLines(ids []string) []string {
	reps := deliveryRetryRunRows(ids)
	out := make([]string, 0, len(reps))
	for _, r := range reps {
		out = append(out, DeliveryRetryLine(r))
	}
	return out
}

// DeliveryRetryJSON renders the same run as JSON — the scriptable face. It
// re-runs NOTHING: it renders the reports from ONE run, so the printed answer
// and the JSON answer can never describe two different attempts.
func DeliveryRetryJSON(ids []string) (string, error) {
	reps := deliveryRetryRunRows(ids)
	b, err := json.MarshalIndent(reps, "", "  ")
	if err != nil {
		return "", fmt.Errorf("delivery retry: render json: %w", err)
	}
	return string(b), nil
}

// DeliveryRetryOwedRowIDs is the verb's inventory: the rows the ledger itself
// says owe a delivery. It opens the ledger through the same resolver every
// other ledger face uses (so --db and the production profile agree).
func DeliveryRetryOwedRowIDs() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), finishCleanupDBTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, fmt.Errorf("delivery retry: open ledger: %w", err)
	}
	defer db.Close()
	ids, err := deliveryRetryOwedIDs(ctx, db, deliveryRetryMaxRows)
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// deliveryRetryRunRows runs the path over a set of ids, fail-soft per row: one
// unreadable row must not stop the others (Rule 15 — the failure is reported
// on its own line, not swallowed and not promoted to a whole-pass failure).
func deliveryRetryRunRows(ids []string) []*DeliveryRetryReport {
	out := make([]*DeliveryRetryReport, 0, len(ids))
	for _, id := range ids {
		rep, err := DeliveryRetryTaskID(id)
		if err != nil {
			out = append(out, &DeliveryRetryReport{TaskID: id, Status: deliveryRetryStatusUnreadable, Detail: err.Error()})
			continue
		}
		out = append(out, rep)
	}
	return out
}

// deliveryRetryOwedReason renders WHY a row still owes its delivery, from the
// row's own recorded deliverable — the replacement for the retired chain's
// finished-cleanup refusal text (the close-out mechanism is gone; slice 3/4 of
// task 01a18799). It names the two facts an operator can act on and nothing
// else: what this row records, and that the peer does not hold it yet.
func deliveryRetryOwedReason(row deliveryRetryRow) string {
	what := "branch " + row.Branch
	if row.Branch == "" {
		what = "no recorded branch"
	}
	where := row.Worktree
	if where == "" {
		where = "no recorded worktree"
	}
	return "delivery owed: " + what + " in " + where + " — the peer does not advertise this row's tip after the attempt"
}
