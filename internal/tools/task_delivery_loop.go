// task_delivery_loop.go — ONE deliverable branch, handed over in a BOUNDED
// LOOP instead of a single push.
//
// THE DEFECT THIS CLOSES (measured 2026-10-24, row 01a16fe2 — its own run's
// reflog had two entries, `clone` and `commit`, and NO THIRD ONE: the delivery
// never attempted a push at all). The push-back lane and the close-out chain
// both made ONE git call and gave up when the peer answered no. The peer's
// answer has four different causes, and only one of them is "somebody pushed
// first":
//
//	the peer's master moved past our base  → our base is simply stale
//	the peer owns refs/heads/<name> elsewhere → a NAME COLLISION (a human call)
//	the branch cannot be merged at all        → a CONTENT conflict (a decision)
//	the peer is unreachable / auth failed     → not about the work
//
// The first is the ordinary case on a two-host setup and it terminates by
// itself: fetch the peer's master, ALIGN the branch onto it (a real merge
// commit, built with merge-tree + commit-tree + update-ref so no working tree
// is needed and no existing commit is rewritten), then push again. The
// second and third need somebody to LOOK at the conflict, and the fourth needs
// nobody to attempt anything. A single call that reports all four as "push
// failed" is what this file replaces: each round re-FETCHES (never trusts the
// previous round's view of the peer), classifies what it saw, and — when the
// cause is one a merge can resolve — runs the alignment merge on the branch and
// goes round again.
//
// WHAT THIS IS NOT. Not a daemon, not a tick, not a scheduled retry (Rule 35):
// the whole thing is a `for` inside the one call that was already delivering.
// Not a second pull implementation (Rules 13/14): the fetch leg is
// gitPushbackFetchMaster — the lane's ONE entry point for "read the peer's
// master" — and the push leg is gitPushbackPush. Not an infinite retry: the
// round count is pushbackDeliveryRounds (Rule 1), spent by construction, and
// every exit carries the last round's measurement.
//
// THE RESOLUTION HOOK IS THE EXISTING SESSION CHANNEL. When a round finds a
// conflict its own deterministic merge cannot settle, the loop calls
// pushbackResolveConflict — a caller-supplied function, declared in this
// interface and NOT re-decided here. Production supplies a hook that hands the
// facts to the run's own agent session; a caller that has no session (the
// periodic flush, a test) supplies none, and the loop then stops with the
// conflict quoted rather than inventing a resolution. That is the honest answer
// to "who resolves this": the same worker/session that produced the work, or a
// human. This file never answers it silently.

package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const (
	// pushbackDeliveryRounds is THE BRAKE: how many deliver→measure→align
	// rounds one branch gets. Named once and read by the logic AND by every
	// line that reports a round count, so the "attempt 2/3" an operator reads
	// is the number that actually bounds the loop (Rule 1b).
	//
	// WHY 3, and not more: round 1 is the ordinary delivery, round 2 is the
	// stale-base retry (the peer moved between our cut and our push — the one
	// cause that genuinely needs a second look), round 3 covers a peer that
	// moved TWICE during a hand-over. A conflict of content is not a clock and
	// does not get better with more rounds of the same merge; it is handed to
	// the resolver hook on the FIRST round it appears.
	pushbackDeliveryRounds = 3

	// pushbackDeliveryDelivered / ...Refused / ...Broken are the three SHAPES
	// a delivery outcome can take, so no caller parses prose (the same
	// three-shape discipline devworkspace.AttemptDelivery uses).
	//
	//	pushbackDeliveryDelivered — the branch reached the peer.
	//	pushbackDeliveryRefused   — it did not, and the reason is the last
	//	                            round's OWN measurement (a conflict, a
	//	                            collision, a dead peer).
	//	pushbackDeliveryBroken    — the loop could not run at all (no git, no
	//	                            branch): not about the peer.
	pushbackDeliveryDelivered = "delivered"
	pushbackDeliveryRefused   = "refused"
	pushbackDeliveryBroken    = "broken"
)

// pushbackDeliveryCause names WHY a round did not deliver. It is a
// classification of a MEASUREMENT (see pushbackRound): every value is decided
// by a git answer this file read, never by a failure's text.
type pushbackDeliveryCause string

const (
	// pushbackCauseNone: the round delivered; nothing to explain.
	pushbackCauseNone pushbackDeliveryCause = ""
	// pushbackCauseUnreachable: the peer could not be read at all (fetch or
	// ls-remote failed). Not about the work — no round of the same loop helps.
	pushbackCauseUnreachable pushbackDeliveryCause = "unreachable"
	// pushbackCauseUnrelated: the branch shares no commit with the peer's
	// trunk. No merge can land it; the remedy is `gopi task archive`, not a
	// retry (the same finding pushbackUnrelatedHistoryReasonFmt renders).
	pushbackCauseUnrelated pushbackDeliveryCause = "unrelated-history"
	// pushbackCauseCollision: the peer owns refs/heads/<name> at a divergent
	// tip. A human decision — never forced, never forked here.
	pushbackCauseCollision pushbackDeliveryCause = "name-collision"
	// pushbackCauseContentConflict: merging the peer's master into the branch
	// conflicts on a path git NAMES. The resolver hook's job; when no hook is
	// declared, the loop stops with the paths quoted.
	pushbackCauseContentConflict pushbackDeliveryCause = "content-conflict"
	// pushbackCauseStaleBase: our branch does not contain the peer's master and
	// merging it is clean. The ordinary case — one round of fetch+merge closes
	// it, and the push then goes out on the aligned tip.
	pushbackCauseStaleBase pushbackDeliveryCause = "stale-base"
	// pushbackCauseRefused: everything above was measured fine and git STILL
	// refused the push (a server-side hook, a permission). git's own sentence
	// is the reason.
	pushbackCauseRefused pushbackDeliveryCause = "push-refused"
)

// String names the cause for a report line (a reader greps these).
func (c pushbackDeliveryCause) String() string { return string(c) }

// pushbackConflictResolver is the RESOLUTION HOOK: the one place a delivery
// that needs a decision hands its facts to somebody who can make it.
//
// It receives the peer's master tip as a ref name, verbatim git output for the
// conflicted merge, and the branch/local directory it happened in — and it
// answers whether the conflict was resolved in the working tree. A true answer
// commits nothing itself (the loop re-measures the tree before it trusts it),
// and a false answer stops the loop with the facts already quoted.
//
// It is a variable-shaped hook rather than a new subsystem on purpose (Rule 3:
// no new mechanism where an existing channel exists). The production wiring —
// the worker's own agent session — is a caller decision; this file only
// defines the seam and calls it.
type pushbackConflictResolver func(ctx context.Context, repoDir, branch, peerTip, gitOutput string) (resolved bool, how string)

// pushbackDeliveryAttempt is ONE round's record. Rounds are kept (not just the
// last) because the report a human reads is the HISTORY: "attempt 1: stale
// base, merged; attempt 2: delivered" says more than "delivered".
type pushbackDeliveryAttempt struct {
	// Round is 1-based and read against pushbackDeliveryRounds in the same
	// sentence the log prints.
	Round int
	// Cause is what this round MEASURED (pushbackCauseNone when it delivered).
	Cause pushbackDeliveryCause
	// Detail is the round's own words: git's verbatim output for a conflict,
	// the peer's tip, the refusal sentence. NEVER a paraphrase (Rule 15).
	Detail string
	// Aligned reports that this round merged the peer's master into the branch
	// (so the NEXT round pushes a branch that contains it).
	Aligned bool
	// Delivered reports that this round's push landed.
	Delivered bool
}

// pushbackDelivery is the whole loop's outcome for ONE branch.
type pushbackDelivery struct {
	Branch    string
	Shape     string // pushbackDeliveryDelivered | Refused | Broken
	Attempts  []pushbackDeliveryAttempt
	Delivered bool
	// Reason is the caller-facing sentence when the loop did not deliver:
	// the LAST round's measurement, verbatim, plus which round it was. It is
	// the value a ledger reason carries — never a generic "push failed".
	Reason string
}

// Rounds spends the report's own count (a test asserts it against the named
// limit rather than a literal).
func (d pushbackDelivery) Rounds() int { return len(d.Attempts) }

// LastAttempt is the final round's record (nil for a loop that never ran): the
// round whose measurement IS the delivery's reason. A caller reads its Aligned
// flag to tell "aligned but not pushed" from "never got past the first gate".
func (d pushbackDelivery) LastAttempt() *pushbackDeliveryAttempt {
	if len(d.Attempts) == 0 {
		return nil
	}
	last := d.Attempts[len(d.Attempts)-1]
	return &last
}

// String renders the round history for a log line: "1: stale-base (…); 2:
// delivered". An empty detail is omitted rather than printed blank.
func (d pushbackDelivery) String() string {
	parts := make([]string, 0, len(d.Attempts))
	for _, a := range d.Attempts {
		cause := a.Cause.String()
		if a.Delivered {
			cause = pushbackDeliveryDelivered
		}
		if a.Detail == "" {
			parts = append(parts, fmt.Sprintf("%d: %s", a.Round, cause))
			continue
		}
		parts = append(parts, fmt.Sprintf("%d: %s (%s)", a.Round, cause, a.Detail))
	}
	return strings.Join(parts, "; ")
}

// pushbackDeliveryExhaustedReasonFmt is the sentence the loop owes when it
// spent every round and still did not deliver. Args: the LAST ROUND'S OWN
// MEASUREMENT (git's verbatim output / the classifier's finding — never a
// summary of it), the round that produced it, the named limit, and that
// round's cause.
//
// The measurement comes FIRST because the sentence is appended to the lane's
// existing refusal lines (`<branch>: the peer rejected the push …`), and a
// caller — the flush, the close-out chain, a reader grepping for a branch
// name — must not have to parse past a prefix to find the branch or git's
// words. The round accounting is the TAIL: a fact ABOUT the attempt, not the
// failure itself (Rule 15: the reason names the cause, the count is context).
const pushbackDeliveryExhaustedReasonFmt = "%s (delivery loop: round %d/%d measured %s)"

// pushbackDeliveryReason renders one non-delivery reason through that format,
// so every exit of the loop accounts for its round the same way (Rule 14: one
// composition, not eight).
func pushbackDeliveryReason(primary string, round int, cause pushbackDeliveryCause) string {
	return fmt.Sprintf(pushbackDeliveryExhaustedReasonFmt, primary, round, pushbackDeliveryRounds, cause)
}

// deliverBranchToPeer runs the bounded delivery loop for ONE branch against
// ONE target, and reports what happened. See the file header for the contract;
// the four properties a caller may rely on are:
//
//  1. every round re-fetches the peer's master (gitPushbackFetchMaster) —
//     no round trusts an earlier round's view;
//  2. the round count is pushbackDeliveryRounds, spent by construction;
//  3. a cause that no round can change (unreachable, unrelated history, a name
//     collision, a content conflict with no resolver) STOPS the loop
//     immediately with that cause quoted — a loop that keeps fetching at a dead
//     peer / an orphan branch is the thing this brake exists to prevent;
//  4. the returned Reason is the last round's own measurement, never a
//     summary.
//
// resolve may be nil: the loop then has no decision-maker and stops at the
// first thing that needs one, quoting it.
func deliverBranchToPeer(ctx context.Context, repoDir, target, branch string, resolve pushbackConflictResolver) pushbackDelivery {
	out := pushbackDelivery{Branch: branch, Shape: pushbackDeliveryBroken}
	branchOut := strings.TrimSpace(branch)
	if branchOut == "" {
		out.Reason = "delivery loop: no branch to deliver"
		return out
	}
	if strings.TrimSpace(target) == "" {
		out.Reason = "delivery loop: no peer target to deliver " + branchOut + " to"
		return out
	}

	// THE ANCESTRY QUESTION IS ASKED ONCE, and only once: whether a branch is
	// cut from the peer's history is a property of the two histories, and no
	// number of rounds changes it — asking it per round would only be a slower
	// way to reach the same refusal. It is asked AFTER the first fetch (the
	// merge-base needs the peer's tip object present).
	var ancestryAsked bool

	// nextRef is the ref this round tries to align onto, decided by the
	// PREVIOUS round's measurement ("" = nothing left to align). It is the
	// whole state the loop carries between rounds, and it is a REF rather than
	// a verdict so the next round re-reads it after its own fetch: the peer may
	// have moved again, and a ref that moved is a tip this loop has not seen.
	nextRef := ""

	for round := 1; round <= pushbackDeliveryRounds; round++ {
		if err := ctx.Err(); err != nil {
			out.Attempts = append(out.Attempts, pushbackDeliveryAttempt{
				Round: round, Cause: pushbackCauseUnreachable, Detail: err.Error()})
			out.Reason = pushbackDeliveryReason(err.Error(), round, pushbackCauseUnreachable)
			return out
		}

		// ROUND N: fetch FIRST. The fetch is the lane's own entry point
		// (Rules 13/14) — one answer about where the peer's master is, shared
		// with the periodic flush and the close-out gate, so the three cannot
		// disagree about the peer's tip. No round trusts an earlier round's
		// view: the loop's whole point is that the second look is a FRESH one.
		if err := gitPushbackFetchMaster(ctx, repoDir, target); err != nil {
			detail := strings.TrimSpace(err.Error())
			out.Attempts = append(out.Attempts, pushbackDeliveryAttempt{
				Round: round, Cause: pushbackCauseUnreachable, Detail: detail})
			out.Reason = pushbackDeliveryReason(detail, round, pushbackCauseUnreachable)
			return out
		}
		peerTip := shortSha(gitPushbackLocalSha(ctx, repoDir, gitPushbackCheckRef))

		if !ancestryAsked {
			ancestryAsked = true
			if !gitPushbackSharesHistoryWithMaster(ctx, repoDir, branchOut) {
				// Reported EXACTLY as the flush reports it — one sentence, one
				// spelling (pushbackUnrelatedHistoryReasonFmt), so a branch
				// refused by the loop and by the flush reads identically.
				detail := fmt.Sprintf(pushbackUnrelatedHistoryReasonFmt, branchOut,
					masterBranchName, masterBranchName, branchOut, masterBranchName)
				out.Attempts = append(out.Attempts, pushbackDeliveryAttempt{
					Round: round, Cause: pushbackCauseUnrelated, Detail: detail})
				out.Shape = pushbackDeliveryRefused
				out.Reason = pushbackDeliveryReason(detail, round, pushbackCauseUnrelated)
				return out
			}
		}

		// THE ALIGNMENT DECIDED BY THE PREVIOUS ROUND now runs, in the fresh
		// state this round's fetch produced. It runs BEFORE the push because
		// the previous round already proved a push of the unaligned tip is
		// refused — and because the alignment is the ONLY thing that changes
		// between rounds.
		aligned := false
		if nextRef != "" {
			ok, detail, cause := gitPushbackDeliveryAlign(ctx, repoDir, branchOut, nextRef, peerTip, resolve)
			if !ok {
				out.Attempts = append(out.Attempts, pushbackDeliveryAttempt{
					Round: round, Cause: cause, Detail: detail})
				out.Shape = pushbackDeliveryRefused
				out.Reason = pushbackDeliveryReason(detail, round, cause)
				return out
			}
			aligned = true
			nextRef = ""
			out.Attempts = append(out.Attempts, pushbackDeliveryAttempt{
				Round: round, Cause: pushbackCauseStaleBase, Detail: detail, Aligned: true})
		}

		// THE PUSH. Round 1 always tries it: the ordinary hand-over needs no
		// alignment at all, and an alignment performed for a delivery that was
		// about to succeed would move a tip nobody asked it to move. A round
		// that just aligned also pushes — that is what makes the alignment
		// VERIFIED rather than merely claimed.
		gitMsg, ok := gitPushbackPush(ctx, repoDir, target, branchOut)
		if ok {
			if !aligned {
				out.Attempts = append(out.Attempts, pushbackDeliveryAttempt{
					Round: round, Cause: pushbackCauseNone, Delivered: true})
			}
			out.Delivered = true
			out.Shape = pushbackDeliveryDelivered
			return out
		}
		refusal := gitPushbackRefusal(ctx, repoDir, target, branchOut, gitMsg)

		// THE REFUSAL IS MEASURED, never assumed. Two questions decide whether
		// another round can help, and each answers with a REF to align onto:
		//
		//	does the peer own refs/heads/<branch> at a divergent tip?
		//	    → that ref is what this branch must contain; the loop merges IT.
		//	does this branch contain the peer's master?
		//	    → no: the master mirror is what it must contain.
		//
		// Neither: the refusal is not about alignment (a hook, a permission, a
		// blocked pushurl) — retrying it would spend rounds on a constant, so
		// the loop stops with git's own sentence (Rule 15).
		ref, cause, why := gitPushbackDeliveryNextAlignment(ctx, repoDir, target, branchOut)
		if ref == "" {
			detail := refusal
			if why != "" {
				detail = refusal + finishCleanupReasonSeparator + why
			}
			out.Attempts = append(out.Attempts, pushbackDeliveryAttempt{
				Round: round, Cause: cause, Detail: detail})
			out.Shape = pushbackDeliveryRefused
			out.Reason = pushbackDeliveryReason(detail, round, cause)
			return out
		}
		nextRef = ref
		out.Attempts = append(out.Attempts, pushbackDeliveryAttempt{
			Round: round, Cause: cause, Detail: refusal})
	}

	// Every round was spent: the last measurement is the reason.
	last := pushbackDeliveryAttempt{Round: pushbackDeliveryRounds, Cause: pushbackCauseRefused,
		Detail: "the peer refused every push and the last round's alignment did not settle it"}
	if n := len(out.Attempts); n > 0 {
		last = out.Attempts[n-1]
	}
	out.Shape = pushbackDeliveryRefused
	out.Reason = pushbackDeliveryReason(last.Detail, pushbackDeliveryRounds, last.Cause)
	return out
}

// gitPushbackDeliveryNextAlignment answers "is there anything left to align
// this branch onto, and what is it" after a REFUSED push. It returns the ref to
// merge ("" when nothing is left), the cause that names the situation, and git's
// own words when the answer required a measurement that failed.
//
// The two questions are asked in the order that costs least and decides most:
// the peer's own REF of this name first (it is the refusal's usual subject — a
// re-delivered branch whose peer copy moved), then the master mirror (the
// ordinary stale base).
func gitPushbackDeliveryNextAlignment(ctx context.Context, repoDir, target, branch string) (string, pushbackDeliveryCause, string) {
	// ① THE PEER'S OWN REF. `gitPushbackClassifyPeerRef` mirrors it and answers
	// an ancestry verdict; a DIVERGENT pair (neither tip contains the other) is
	// what a refused re-delivery looks like, and the peer's tip is the ref this
	// branch must then contain.
	advertised, measured := gitPushbackPeerRefTip(ctx, repoDir, target, branch)
	if measured {
		if verdict, _ := gitPushbackClassifyPeerRef(ctx, repoDir, target, branch, advertised); verdict == gitPushbackPeerRefCollision {
			return fmt.Sprintf(gitPushbackPeerRefMirrorFmt, branch), pushbackCauseCollision,
				"the peer's " + fmt.Sprintf(gitPushbackPeerRefHeadsFmt, branch) +
					" has diverged from ours; the loop merges THAT ref into the branch (no force, no fork)"
		}
	}

	// ② THE STALE BASE. A branch that does not contain the peer's master cannot
	// be landed by the peer's `merge --ff-only`, and merging the mirror is the
	// one operation that fixes it.
	if gitPushbackFastForwardsOntoMaster(ctx, repoDir, branch) {
		return "", pushbackCauseRefused, ""
	}
	return masterBranchName, pushbackCauseStaleBase, ""
}

// gitPushbackDeliveryAlign brings the branch up to `onto` (the peer's master
// mirror, or the peer's own divergent ref of this branch) and reports whether
// it succeeded, in the three-valued shape the loop classifies on:
// (true, detail, cause) — the branch now CONTAINS that ref; (false, detail,
// cause) — it does not, and CAUSE says why in the words the report will carry.
//
// It is the ONE place the resolver hook is called, so "who resolves a content
// conflict" is answered exactly once per round.
func gitPushbackDeliveryAlign(ctx context.Context, repoDir, branch, onto, peerTip string,
	resolve pushbackConflictResolver) (bool, string, pushbackDeliveryCause) {
	clean, mergedTree, mergeOut := gitPushbackDeliveryTrialMerge(ctx, repoDir, branch, onto)
	if !clean {
		if resolve == nil {
			// A conflict with nobody to answer it. Stopping is the honest end:
			// the paths git named are on the line, and spending rounds on a
			// merge that will conflict identically is the live-lock this brake
			// exists to prevent.
			return false, mergeOut, pushbackCauseContentConflict
		}
		// THE HOOK: the existing session channel decides. It receives git's own
		// words and the directory it happened in, and answers whether a
		// resolution now exists.
		resolved, how := resolve(ctx, repoDir, branch, peerTip, mergeOut)
		detail := mergeOut
		if strings.TrimSpace(how) != "" {
			detail = mergeOut + finishCleanupReasonSeparator + "resolver: " + how
		}
		if !resolved {
			return false, detail, pushbackCauseContentConflict
		}
		// Re-MEASURE, never trust the answer: the hook says a resolution
		// exists, so the merge must now be clean. A hook that claimed more than
		// it did surfaces here as itself — git's refusal, quoted.
		clean, mergedTree, mergeOut = gitPushbackDeliveryTrialMerge(ctx, repoDir, branch, onto)
		if !clean {
			return false, detail + finishCleanupReasonSeparator + mergeOut, pushbackCauseContentConflict
		}
		line, err := gitPushbackAlignBranch(ctx, repoDir, branch, onto, peerTip, mergedTree)
		if err != nil {
			return false, detail + finishCleanupReasonSeparator + line, pushbackCauseContentConflict
		}
		return true, detail + finishCleanupReasonSeparator + line, pushbackCauseContentConflict
	}
	line, err := gitPushbackAlignBranch(ctx, repoDir, branch, onto, peerTip, mergedTree)
	if err != nil {
		return false, line, pushbackCauseContentConflict
	}
	return true, line, pushbackCauseStaleBase
}

// gitPushbackDeliveryTrialMerge asks git whether merging `theirs` into
// `ours` would be CLEAN — without touching any working tree — and returns the
// MERGED TREE when it is.
//
// `git merge-tree --write-tree <ours> <theirs>` writes the merge to the object
// store and exits 0 when it is clean, printing the resulting tree id on its
// first line; a conflict makes it exit 1 and print the conflicted PATH NAMES.
// That is the measurement this loop classifies on: a clean answer is
// alignment (the loop performs it with the tree it just got), a conflicted one
// is a decision (the resolver hook, or the loop stops).
//
// An invocation that could not run at all (an old git without
// `--write-tree`, a wedged repo) is reported as NOT clean with git's own
// sentence: the loop then treats it as a conflict and hands it to the
// resolver, which is the honest treatment of a question this lane could not
// ask (Rule 15 — an unmeasured premise is never evidence of a clean merge).
func gitPushbackDeliveryTrialMerge(ctx context.Context, repoDir, ours, theirs string) (clean bool, tree string, output string) {
	// THE OUTPUT IS READ OFF THE PROCESS, not off the runner's summary. The
	// shared runner (gitPeerRun) returns STDERR on failure — the right shape for
	// a fetch or a push, wrong for this one: merge-tree prints the CONFLICTED
	// PATH NAMES on STDOUT (measured: stdout carries them, stderr carries
	// usage/fatal). Reading the runner's string dropped exactly the fact this
	// function exists to report — the reason then said "content-conflict" with
	// no path on it, which is the swallowed-cause defect Rule 15 bans.
	cctx, cancel := context.WithTimeout(ctx, gitPushbackFetchTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", "-C", repoDir, "merge-tree", "--write-tree", ours, theirs)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	gitWords := strings.TrimSpace(strings.Join([]string{
		strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()),
	}, "\n"))
	if err == nil {
		return true, pushbackFirstLine(stdout.String()), ""
	}
	// git's documented exit 1 is a CONFLICT (stdout lists the paths); every
	// other status is an invocation failure. Both are reported VERBATIM and both
	// stop the merge — the difference is what the words say, and those words are
	// git's, so a reader tells them apart without this file inventing a label.
	words := gitWords
	if words == "" {
		words = strings.TrimSpace(err.Error())
	}
	return false, "", fmt.Sprintf("%s %s %s: %s", gitDeliveryMergeTreeVerb, ours, theirs, words)
}

// gitDeliveryMergeTreeVerb is git's own spelling of the merge-tree verb, so the
// reason sentence quotes the command that produced it (Rule 1b: a CLI verb is a
// constant, never typed inline at the call site).
const gitDeliveryMergeTreeVerb = "merge-tree --write-tree"

// pushbackFirstLine is the merge-tree contract's tree id: the first line of
func pushbackFirstLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return ""
}

// gitPushbackAlignBranch makes the branch CONTAIN the peer's master — the
// same operation the flush's refusal line tells a human to perform
// (`merge master into it`), performed by the loop instead.
//
// NO WORKING TREE IS TOUCHED, and that is a requirement, not an optimisation.
// Both callers run this in a directory whose checkout is NOT the branch being
// delivered: the periodic flush works in the main checkout (HEAD on master)
// and the close-out chain pushes a REF from the main repo (measured: the push
// must not run in the worktree, which step ③ may already have removed). A
// `git merge` there would either refuse or merge into the WRONG branch, so the
// alignment is built the way git builds a merge internally:
//
//	the merged TREE   — merge-tree --write-tree (already computed by
//	                    gitPushbackDeliveryTrialMerge; passed in, so the tree
//	                    the loop decided on is the tree it commits)
//	the merge COMMIT  — commit-tree <tree> -p <branch> -p <master>, i.e. the
//	                    branch's tip and the peer's tip as the two parents
//	the REF           — update-ref refs/heads/<branch> <commit>: a pure
//	                    EXTENSION of the branch (never a rewrite: no existing
//	                    commit loses its identity, and a branch that has already
//	                    crossed the wire keeps every commit it had)
//
// It returns git's own words on failure. Failure leaves the ref untouched —
// update-ref is the last step, so a failed commit-tree cannot half-apply an
// alignment.
func gitPushbackAlignBranch(ctx context.Context, repoDir, branch, masterRef, peerTip, mergedTree string) (string, error) {
	tree := strings.TrimSpace(mergedTree)
	if tree == "" {
		return "", fmt.Errorf("%s: no merged tree to commit (the trial merge produced none) — the branch was left untouched", branch)
	}
	msg := "merge the peer's " + masterRef
	if peerTip != "" {
		msg += " (peer tip " + peerTip + ")"
	}
	msg += " into " + branch + " — delivery loop alignment"

	out, err := gitPushbackGitRun(ctx, repoDir, "commit-tree", tree,
		"-p", branch, "-p", masterRef, "-m", msg)
	if err != nil {
		return strings.TrimSpace(out + "\n" + err.Error()), err
	}
	commit := pushbackFirstLine(out)
	if commit == "" {
		return "", fmt.Errorf("%s: git commit-tree printed no commit id — the branch was left untouched", branch)
	}
	if out, err := gitPushbackGitRun(ctx, repoDir, "update-ref",
		refsHeadsPrefix+branch, commit, branch); err != nil {
		return strings.TrimSpace(out + "\n" + err.Error()), err
	}
	return "aligned " + branch + " onto " + masterRef + " (" + shortSha(commit) + ")", nil
}
