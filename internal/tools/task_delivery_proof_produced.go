package tools

// task_delivery_proof_produced.go — THE SECOND WAY TO ANSWER "DID THIS RUN
// PRODUCE THIS TIP?", for the case where the FIRST witness cannot be asked at
// all because the ground is gone.
//
// THE DEFECT THIS REPAIRS (filed as row 01a18253-f7c4-7cf7-8cb3-868647a25401,
// measured 2026-10-28 on the live ledger). `tipIsTheCutPoint`
// (task_delivery_proof_cutpoint.go) answers "did this run produce any commit of
// its own?" — and it can only answer it from the GROUND's object store, because
// it compares the tip against the branch reflog's OLDEST entry and both objects
// have to be readable in one place. When the ground is already reclaimed
// (`devworkspace.GroundModelMissing`) it returns false — correctly, because an
// unread cut point must never manufacture a "produced nothing" verdict — and the
// caller then falls through to the containment arms.
//
// That fall-through is sound for a run that produced something and WRONG for a
// run that produced nothing, because of a tautology: the cut-point commit is
// already on the integration branch (it was cut FROM it), so
//
//	git merge-base --is-ancestor <tip> origin/master   ⇒ 0
//
// is satisfied by the tip being an ancestor of ITSELF. The row is then judged
// `delivered` — and `delivered` is the signal that RELEASES the ground and lets
// the writeback land. A run of two minutes that never made a commit is
// indistinguishable, on every surface, from one that delivered.
//
// WHICH PIECE OF EVIDENCE ACTUALLY SURVIVES THE GROUND — measured, not assumed.
// Four candidates were taken to the live ledger before any code was written:
//
//	(a) the row's landed record (`clone-delivery-landed`, deliveryProofLandedTip)
//	    — USABLE, and already read. It NAMES the tip, so it is this arm's best
//	    source for WHICH commit to judge; it cannot answer whether the run
//	    PRODUCED that commit (a channel that pushed the inherited cut point
//	    records exactly the same sentence — measured on 01a17dc1 below).
//	(b) the removal-audit branch-delete record's `sha` — NOT USABLE HERE. The
//	    field is optional and no arm ever wrote one for these branches: 4043
//	    branch-delete records on this host's sink, 477 carrying a `sha`, none for
//	    the branches of the rows in question (measured 2026-10-27, quoted in
//	    deliveryProofTipOf's own header).
//	(c) the commit's author/committer timestamps — USABLE, and it is what this
//	    file measures. Both are recorded INSIDE the commit object, so they
//	    survive the ground's deletion and any clone that still holds the object.
//	(d) the commit's parent — USABLE but NOT SUFFICIENT alone: "the tip has a
//	    parent" is true of every inherited commit and of every commit a run
//	    makes after its first. It is what makes (c) meaningful (a commit is
//	    produced by SOMEONE at SOME time; the question is whether that time
//	    falls inside THIS row's execution), not a verdict by itself.
//
// So the verdict rests on (c) with (a) naming the subject, and the measurement
// is a DISPROOF: the tip was produced by this run only if it can have been
// produced during the run at all. Both stamps earlier than the row's execution
// start ⇒ the commit existed before this run began ⇒ it cannot be this run's
// work. Anything else (one stamp inside the window, no clock on the row, an
// unreadable commit) is NOT a disproof and the arm says nothing.
//
// MEASURED ON THE LIVE LEDGER (2026-10-28, `--db /home/user/.gopi/memory.db`,
// 275 rows carrying `delivery-proof-unverified`, 60 of them judged `delivered`):
// this witness DISPROVES exactly ONE row and leaves fifty-nine alone —
//
//	DISPROVED 01a17dc1-4fe2-7c63-9b1d-f1add338eda8 tip=6c752f685
//	          author=10-27T23:30:31 committer=10-27T23:38:28 start=10-27T23:39:11(claimed_at)
//	kept       … 59 rows …
//	SUMMARY   disproof=1 kept=59 nowitness=0
//
// and that one row IS the false positive: its ground was cut at 6c752f6, its
// recorded tip is 6c752f685 (the SAME commit), the run lasted 2m24s
// (`in_progress 23:39:11 → completed 23:41:43`), it walked away leaving two
// untracked `.bak` files in its session directory, and the channel's landed
// record nevertheless says "delivered … at 6c752f685" because the push of an
// unmoved HEAD succeeds trivially.
//
// THE OTHER DIRECTION IS THE CONSTANT DANGER, and it is why every reading here is
// a value with a stated absence (Rule 15) rather than a bare bool: a witness that
// answered "not produced" on a missing clock would turn every legacy row into
// `unknown`. The census confirms the size of that risk — the population the
// previous repair (d370a6e) recovered had grown from 42 rows to 60 by the time
// this arm was measured, and after it is in place 59 keep `delivered` and ONE
// (`01a17dc1`) moves to `unknown`. A row with no readable execution clock is NOT
// a disproof, and TestDeliveryProof_ProducedWitnessIsThreeValued pins that arm.

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// deliveryProofClockSkewSeconds is the slack granted to a clock comparison.
//
// It exists because the two sides come from DIFFERENT clocks: the row's start
// stamp is written by the gopi process on this host, while a commit's stamps are
// written by whichever git process made the commit — possibly on another host
// (the clone channel clones from a peer) whose clock is a little ahead or
// behind. Without slack, a run whose own first commit carries a stamp a few
// seconds "before" the claim would be called a disproof, which is a FALSE
// refusal produced by clock skew rather than by evidence.
//
// THE VALUE IS BRACKETED FROM BOTH SIDES BY THE LIVE LEDGER, and the bracket is
// narrow (measured 2026-10-28 against the 275-row census, 60 rows judged
// `delivered`). Per row the two gaps are (run start − author stamp) and (run
// start − committer stamp) in seconds; a POSITIVE gap means the stamp is earlier
// than the run began:
//
//	the ONE row this witness DISPROVES (01a17dc1)
//	    author gap +520s, committer gap  +43s
//	the FIFTY-NINE rows it must NOT touch
//	    largest author gap     +64728s   ← an adopted commit keeps its author date
//	    largest COMMITTER gap     −152s   ← every one committed AFTER it began
//
// So any value in (0, 43) separates the class, and this constant must stay BELOW
// 43 or the false positive slips through. It is 10 for a stated reason: enough
// to absorb a few seconds of genuine cross-host skew between the git that wrote
// the stamp and the gopi process that wrote the row's clock, without approaching
// the 43s the disproof row relies on. The headroom on each side is written down
// so a later reader can move this with evidence rather than by feel — 33s before
// the false positive is missed, 162s before a delivered row is wrongly disowned.
//
// The ASYMMETRY the numbers show is itself the reason BOTH stamps must be early:
// a preserved AUTHOR date is ordinary (adoption, cherry-pick, rebase) and 59 rows
// exhibit it; a preserved COMMITTER date is not, because the committer stamp is
// written by the process that just made the commit.
const deliveryProofClockSkewSeconds = 10

// deliveryProofProduced answers "could THIS RUN have produced THIS TIP?" from
// evidence that survives the ground's deletion, and returns the reading it used
// so a refusal can quote it.
//
// Three-valued, on purpose (Rule 15):
//
//	verdict == deliveryProofProducedYes  — a stamp falls inside the run's window,
//	                                       so the tip may be this run's work.
//	verdict == deliveryProofProducedNo   — BOTH stamps predate the run's start by
//	                                       more than the skew, so the commit
//	                                       existed before the run began.
//	verdict == deliveryProofProducedUnreadable — no clock on the row, or the
//	                                       commit's stamps could not be read. NOT
//	                                       a disproof; the caller must fall
//	                                       through exactly as it did before.
//
// `dir` is the directory whose object store holds `tip` — the ground when it is
// alive, else the row's recorded repo. An unreadable commit there is
// `unreadable`, never `no`.
func deliveryProofProduced(ctx context.Context, taskID, dir, tip string) (verdict, evidence string) {
	tip = strings.TrimSpace(tip)
	if tip == "" {
		return deliveryProofProducedUnreadable, "no tip was named, so there is no commit to ask about"
	}
	start, clockSource, ok := deliveryProofRowStart(taskID)
	if !ok {
		return deliveryProofProducedUnreadable, fmt.Sprintf(
			"row %s carries no readable execution clock (created_at / claimed_at / in_progress_at all empty), "+
				"so when the tip was made cannot be compared with when the run ran", taskID)
	}
	short := branchDeleteShortHash(tip)
	if strings.TrimSpace(dir) == "" {
		return deliveryProofProducedUnreadable, fmt.Sprintf(
			"no directory that can read %s was recorded, so its commit stamps were never read", short)
	}
	read := deliveryProofGitRead(ctx, dir, "log", "-1", "--format=%at %ct", tip)
	if read == "" {
		return deliveryProofProducedUnreadable, fmt.Sprintf(
			"%s is not readable in %s, so its commit stamps were never read", short, dir)
	}
	fields := strings.Fields(read)
	if len(fields) != 2 {
		return deliveryProofProducedUnreadable, fmt.Sprintf(
			"the commit stamps of %s read as %q, which is not the two-number shape this arm measures", short, read)
	}
	var author, committer int64
	if _, err := fmt.Sscan(fields[0], &author); err != nil || author <= 0 {
		return deliveryProofProducedUnreadable, fmt.Sprintf(
			"the author stamp of %s read as %q, which is not a POSIX time", short, fields[0])
	}
	if _, err := fmt.Sscan(fields[1], &committer); err != nil || committer <= 0 {
		return deliveryProofProducedUnreadable, fmt.Sprintf(
			"the committer stamp of %s read as %q, which is not a POSIX time", short, fields[1])
	}
	limit := int64(start) - deliveryProofClockSkewSeconds
	ev := fmt.Sprintf("%s: author %s, committer %s; the run's %s %s",
		short, deliveryProofClockStamp(author), deliveryProofClockStamp(committer),
		clockSource, deliveryProofClockStamp(int64(start)))
	if author < limit && committer < limit {
		return deliveryProofProducedNo, ev
	}
	return deliveryProofProducedYes, ev
}

// The three values of deliveryProofProduced. Named constants, not bare strings
// (Rule 1b), because a caller comparing them by literal is how two spellings of
// one verdict appear.
const (
	deliveryProofProducedYes        = "yes"
	deliveryProofProducedNo         = "no"
	deliveryProofProducedUnreadable = "unreadable"
)

// deliveryProofRowStart reads the moment this row's execution began, from the
// THREE stamps the ledger keeps, in the order of how precisely each one names
// it: the current in_progress stretch's start, else the claim stamp, else the
// row's creation.
//
// ALL THREE ARE GROUND-INDEPENDENT — they live in the ledger, not in a
// directory — which is the whole reason this witness can be asked at
// close-out after the ground has been reclaimed. The fallback order is not a
// convenience: on this host's legacy rows `in_progress_at` is NULL (the column
// arrived later) and `claimed_at` is the only stamp that marks when the run
// actually started work (measured on the census rows: `in_progress=(none)` for
// every one of them, `claimed=` populated).
//
// ok=false means the row carries NO clock at all — an honest absence, and the
// caller treats it as "not a disproof" rather than as "produced nothing".
func deliveryProofRowStart(taskID string) (start float64, source string, ok bool) {
	if strings.TrimSpace(taskID) == "" {
		return 0, "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	db, err := openTasksDBReadOnly(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("delivery proof: the clock of %s could not be read: %v", taskID, err)
		return 0, "", false
	}
	defer db.Close()
	row, err := taskByID(ctx, db, taskID)
	if err != nil || row == nil {
		taskNotifyLog("delivery proof: the clock of %s could not be read: %v", taskID, err)
		return 0, "", false
	}
	if row.HasInProgressAt && row.InProgressAt > 0 {
		return row.InProgressAt, "in_progress_at", true
	}
	if row.HasClaim && row.ClaimedAt > 0 {
		return row.ClaimedAt, "claimed_at", true
	}
	if row.CreatedAt > 0 {
		return row.CreatedAt, "created_at", true
	}
	return 0, "", false
}

// deliveryProofClockStamp renders one POSIX second for a refusal sentence. It is
// local time because every stamp it is compared against is written by a process
// on this host in this zone, and a reader checking the sentence by hand will be
// reading `git log --date=iso` output.
func deliveryProofClockStamp(sec int64) string {
	if sec <= 0 {
		return "(none)"
	}
	return time.Unix(sec, 0).Format(time.RFC3339)
}
