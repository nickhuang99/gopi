package tools

// task_reclaim_stranded_commits.go — the LEDGER RECORD of what a permanently
// refused CLONE ground still holds, so "this commit has no destination" is a
// READABLE FACT rather than an inference from a sweep line that was printed
// while the directory happened to exist.
//
// THE DEFECT, measured on 2026-11-01 against row
// 01a16043-2b43-7590-a050-3418abd608ef (epic: failed row, stranded commit):
//
//	status           failed  (zombie disposal: heartbeat stalled, holder pid dead)
//	verdict          reclaim-unreachable — "…refusing to remove a directory this
//	                 row does not own (row status and directory left untouched;
//	                 this pass will not report this row again)"
//	its tip          f2215b227ac3b9f279e169f6a128f7b56c16c8b4
//	peer master      2dd1140c235036d4d0d9b841b12e2d0d9b7e3067  (tip NOT contained)
//	git cherry       + f2215b227…            (patch-unique: the WORK is not on master)
//	PROJECT.md on master still carries Rule 36 (grep -c → 2)
//
// So the ledger said `failed`, the commit was real and unreachable from every
// other repository on this host, and the surfaces an operator reads did not
// name it. The population measurement (this host, 2026-11-01) is what makes the
// diagnosis precise rather than a guess:
//
//	undelivered grounds                82   (all 82 are CLONES; 0 linked)
//	carry `delivery-proof-owed`        65   ← the tip IS on the trail
//	carry ANY delivery/reclaim record  74
//	carry no delivery record at all     8   ← the row 01a16043 shape
//	`task show` renders `delivery-proof-owed`   0
//
// THE SECOND LINE IS THE CORRECTION THAT MATTERS. An earlier version of this
// file claimed "the ledger's last word about the stranded work was silence".
// That is FALSE and it was measured false: `delivery-proof-owed`
// (task_delivery_proof.go) already writes the tip, the count and the delivery
// points — 804 characters on row 01a16043, naming `f2215b227ac3` and both
// `refs/remotes/origin/master` points. What is missing is the RENDER: that
// event is NOT a member of verdictTrailEventNames, so `task show` — the surface
// a reviewer actually reads — prints nothing about it, and the row's `verdict:`
// line shows whatever verdict event came later. The record existed and could
// not be read.
//
// So this file closes the half that is genuinely open, and it does NOT restate
// the misleading "nothing recorded" claim:
//
//	· the 8 rows with NO delivery record at all get one, written by the arm that
//	  refused them, on the trail, naming the ground, the branch, the count, the
//	  delivery points and the ids;
//	· every record this arm writes IS rendered (the event is a member of
//	  verdictTrailEventNames), so a reader learns it from `task show`;
//	· the `reclaim-unreachable` marker next door — which closes with "this pass
//	  will not report this row again" — stops being the ONLY thing the arm ever
//	  says about a clone ground it will never revisit.
//
// THE RENDER GAP ITSELF IS NOT FIXED HERE, and saying so is part of the
// delivery: adding `deliveryProofOwedEvent` to verdictTrailEventNames changes
// the verdict line for every row on the ledger that carries it, which is a
// separate decision with its own blast radius (measured: only 2 of the 65 would
// have it surface, because a newer verdict event outranks it — so the change
// would buy 2 rows and shift the rendering of 63). Filed as its own finding.
//
// WHY THIS IS NOT A SECOND MEASUREMENT (Rules 13/14). The commit set comes from
// worktreeUndeliveredCommits — the probe the undelivered-commits rung already
// calls — read from the directory the clone shape requires (the ground's own ref
// store), and reduced by the SAME equivalence legs that rung uses. This file adds
// no git vocabulary, no criterion and no fetch: it asks the existing question and
// writes the answer where a reader can find it.
//
// WHY IT ONLY FIRES FOR A CLONE GROUND. `git worktree remove` deletes a linked
// worktree's DIRECTORY while its branch and objects stay in the shared repo, so
// that shape cannot take work with it (worktree_sweep_undelivered.go states the
// same fact); a clone ground IS its only repository, and its removal is the one
// act that destroys the commits. Recording "stranded commits" for a shape that
// cannot strand them would make the event name false.
//
// UNMEASURABLE IS NOT "NOTHING IS THERE" (Rule 15a). A probe that could not
// answer writes a record SAYING what could not be read and keeps the ground;
// only a MEASURED zero is silent — because a measured zero means this rung has
// nothing to disclose, and a line per pass about a row holding no work would be
// the noise the aggregate note already ended.

import (
	"context"
	"strings"

	"github.com/nick/gopi/internal/devworkspace"
)

const (
	// reclaimStrandedCommitsEvent is the trail event under which a permanently
	// refused CLONE ground's undelivered commits are recorded. It is a member of
	// verdictTrailEventNames (tasks.go), so `task show` renders it: a row whose
	// ground is its work's only home must not require running a sweep to learn
	// that (Rule 15 — a refusal that only reaches stdout is invisible to the next
	// reader).
	reclaimStrandedCommitsEvent = "reclaim-stranded-commits"

	// reclaimStrandedCommitsMarker is the token the record opens with — what an
	// operator greps to find every row on this ledger whose ground is holding
	// work, and what a regression test asserts (Rule 1b: one spelling).
	reclaimStrandedCommitsMarker = "ground holds undelivered commits"

	// reclaimStrandedNoneMarker is the MEASURED-ZERO record's token: the probe
	// ran and there is nothing here to disclose. It is written rather than
	// omitted so a later reader can tell "measured, nothing stranded" from "the
	// arm never asked" — the two states must never share a rendering.
	reclaimStrandedNoneMarker = "ground holds no undelivered commit"

	// reclaimStrandedUnmeasurableMarker is the third, NAMED state: the question
	// could not be answered and the ground is kept on that basis. Never collapsed
	// into either verdict (Rule 15a).
	// NOTE THE WORDING: it must NOT contain reclaimStrandedCommitsMarker, and that
	// is a Rule 1b defect the first draft shipped — "could not measure whether the
	// ground holds undelivered commits" carries "ground holds undelivered commits"
	// inside it, so `grep` for the stranding token matched every unmeasurable
	// record too. Measured: the fixture for this file failed on its own grep, which
	// is the collision announcing itself. Three tokens, none a substring of
	// another, so a grep for one answers exactly one state.
	reclaimStrandedUnmeasurableMarker = "undelivered-commit measurement unavailable"

	// reclaimStrandedBranchMissingMarker is the FOURTH named state: the row names
	// a branch the ground does not have. It is not stranding and it is not a
	// clean zero — it is a registration disagreeing with its directory, which is
	// its own finding. Collapsing it into either verdict would hide a broken
	// deliverable block behind a reassuring sentence (Rule 15a).
	reclaimStrandedBranchMissingMarker = "the row's branch does not exist in the ground"

	// reclaimStrandedReadTail says how to read the work WITHOUT the directory
	// being able to deliver it: the ground is kept, so the objects are on disk
	// and every commit below is readable by ordinary git. Naming the command in
	// the record is what makes the disposition actionable rather than a
	// tombstone (Rule 15: a refusal an operator cannot act on is noise).
	reclaimStrandedReadTail = " — the ground is KEPT (a clone ground is never this arm's to remove); " +
		"read the work with `git -C <ground> show <sha>`. Nothing will push these for you: " +
		"this row is terminal and a run's delivery leg runs once, from its own close-out."

	// reclaimStrandedCommitListCap bounds the ids embedded in the record. A trail
	// row is read by a human; the count and the refs are the measurement, and a
	// ground with more pending commits than this is a shape whose reader needs
	// `git rev-list` rather than a longer sentence. The overflow is SAID, never
	// silently dropped (Rule 15).
	reclaimStrandedCommitListCap = 10
)

// The two git spellings reclaimStrandedBranchExists needs, named here rather
// than typed inline (Rule 1). `--verify --quiet` is the ONE shape that answers
// "does this ref exist" without writing anything and without printing git's
// noise into a caller's stream.
const (
	reclaimStrandedRevParseVerb    = "rev-parse"
	reclaimStrandedVerifyQuietFlag = "--verify"
)

// reclaimStrandedCommitsRecord writes the ground's undelivered-commit set onto
// the row's append-only trail. Called ONLY from reclaimUnreachableRecord — the
// arm's single writer of the permanent-refusal marker — so it fires once per
// row (reclaimUnreachableRecorded gates it) and never for a refusal whose
// grounds can clear.
//
// It writes no status transition, deletes nothing and moves no ref: it is a
// note, exactly like the marker it accompanies (AppendTaskTrailEvent's contract).
func reclaimStrandedCommitsRecord(r reclaimDeadHolderRow) {
	ground := strings.TrimSpace(r.Worktree)
	if ground == "" {
		return
	}
	// A LINKED WORKTREE IS NEVER STRANDED (see the file doc): its removal drops
	// a registration and leaves the branch in the shared repo. Asked through the
	// ONE shape discriminator, never a second implementation of "is this a
	// clone?" (Rules 13/14).
	if devworkspace.GroundModelOf(ground) != devworkspace.GroundModelClone {
		return
	}
	branch := strings.TrimSpace(r.Branch)
	if branch == "" {
		AppendTaskTrailEvent(r.TaskID, reclaimStrandedCommitsEvent,
			reclaimStrandedUnmeasurableMarker+": the row records no branch, so there is nothing to "+
				"measure the ground "+ground+" against")
		return
	}
	// THE BRANCH MUST EXIST IN THE GROUND BEFORE ITS COUNT MEANS ANYTHING.
	// Asked here, through the probe's own git funnel, because the probe answers
	// `Measured=true, Ahead=0` for a branch it cannot resolve — deliberately:
	// for the SWEEP that is a dead registration for a later rung to handle, not
	// a measurement failure. For THIS record the same answer would be a lie of
	// another kind: "the ground holds nothing" is a claim about a branch, and a
	// branch that is not there supports no claim at all. Measured on this file's
	// own fixture: a row naming `no-such-branch` produced "ground holds no
	// undelivered commit: branch no-such-branch of … holds nothing no delivery
	// point has ()" — a measured-zero sentence carrying an EMPTY delivery-point
	// list, i.e. a question never asked, wearing the answer's clothes (Rule 15a).
	if !reclaimStrandedBranchExists(ground, branch) {
		AppendTaskTrailEvent(r.TaskID, reclaimStrandedCommitsEvent,
			reclaimStrandedBranchMissingMarker+": the row names branch "+branch+" but the ground "+
				ground+" has no such ref — the registration and the directory disagree, so whether "+
				"any work is stranded cannot be read from this ground")
		return
	}
	p := worktreeUndeliveredCommits(ground, branch)
	// A DELIVERY-POINT LIST THAT RESOLVES TO NOTHING IS NOT A ZERO EITHER: the
	// probe returns Measured=true with Points empty only when it could not ask
	// (its own comment says a skipped point "answers nothing"), so the count that
	// follows such a list is a self-referential 0, never a measurement.
	if !p.Measured || len(p.Points) == 0 {
		why := strings.TrimSpace(p.Why)
		if why == "" {
			why = "no delivery point resolves, so no count was taken"
		}
		AppendTaskTrailEvent(r.TaskID, reclaimStrandedCommitsEvent,
			reclaimStrandedUnmeasurableMarker+": "+why+" — keeping the ground "+ground)
		return
	}
	// THE FRESH-TIP EQUIVALENCE LEGS, asked exactly as the sweep's rung asks them:
	// this repository's `origin/master` snapshot is stale in 82 of 82 clone
	// grounds on this host (measured 2026-11-01), so a count taken against it
	// alone reports landed work as undelivered. `r.Repo` is the fresh source (the
	// main checkout the record names); where it IS the ground there is nothing
	// fresher to ask and the legs decline, which leaves the pre-fix reading.
	p = worktreeUndeliveredSubtractEquivalent(ground, strings.TrimSpace(r.Repo), branch, p)
	if p.Ahead == 0 {
		AppendTaskTrailEvent(r.TaskID, reclaimStrandedCommitsEvent,
			reclaimStrandedNoneMarker+": branch "+branch+" of "+ground+
				" holds nothing no delivery point has ("+strings.Join(p.Points, ", ")+"); "+
				"this arm's refusal is about OWNERSHIP and strands no work")
		return
	}
	body := reclaimStrandedCommitsMarker + ": branch " + branch + " of " + ground + " holds " +
		plural(p.Ahead) + " that no delivery point has (" + strings.Join(p.Points, ", ") + ") — " +
		reclaimStrandedCommitList(p.Commits) + reclaimStrandedReadTail
	if len(p.Equivalent) > 0 {
		// The commits this count was REDUCED by are evidence, not decoration: a
		// reader must be able to see that the equivalence legs already ran and
		// what they matched, or a later "why is this only 3?" has no answer.
		body += " Already-landed under another id (counted as delivered): " +
			strings.Join(p.Equivalent, "; ") + "."
	}
	AppendTaskTrailEvent(r.TaskID, reclaimStrandedCommitsEvent, body)
}

// reclaimStrandedCommitList renders the commit ids for the record, bounded by
// reclaimStrandedCommitListCap. An empty list under a positive count is stated
// as such: the count answered and the id read did not, and a reader must not
// read an empty id list as "no commits" (Rule 15).
func reclaimStrandedCommitList(commits []string) string {
	if len(commits) == 0 {
		return "the count answered but its commit ids could not be read (ask `git rev-list` in the ground)"
	}
	shown := commits
	overflow := 0
	if len(shown) > reclaimStrandedCommitListCap {
		overflow = len(shown) - reclaimStrandedCommitListCap
		shown = shown[:reclaimStrandedCommitListCap]
	}
	out := strings.Join(shown, ", ")
	if overflow > 0 {
		out += " (and " + plural(overflow) + " more — `git rev-list " + "<branch>" + " --not " +
			strings.Join(worktreeUndeliveredDeliveryPoints("<branch>"), " ") + "` in the ground lists them all)"
	}
	return out
}

// reclaimStrandedBranchExists reports whether the ground's own ref store carries
// `refs/heads/<branch>`. It uses the probe's existing git funnel
// (worktreeUndeliveredGit) and the existing ref-prefix constants, so there is no
// second vocabulary for "where are the refs" (Rules 13/14), and it is a READ: no
// fetch, no ref written, nothing fetched from a peer.
func reclaimStrandedBranchExists(ground, branch string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), worktreeSweepGitTimeout)
	defer cancel()
	_, err := worktreeUndeliveredGit(ctx, ground,
		reclaimStrandedRevParseVerb, reclaimStrandedVerifyQuietFlag, refsHeadsPrefix+branch)
	return err == nil
}
