package tools

// Branch-delete guard (2026-09-17 incident, task 01a0af50) — the REF half.
//
// The incident: a LIVE session's worktree directory was destroyed mid-run, and
// the branch ref that carried its only committed deliverable became
// unreachable: `git branch --contains <tip>` and `git tag --contains <tip>`
// were both EMPTY, and `git worktree add <path> <branch>` answered
// `fatal: invalid reference`. The commit survived only because a human made a
// ref by hand. A deleted ref is the one thing a cleanup arm cannot restore, so
// this file answers ONE question before any FORCE delete:
//
//	would deleting this branch leave its tip reachable from NO other ref?
//
// `git branch -d` already answers "is this merged" — git's own lock — and every
// arm keeps using it. What `-d` does NOT know is whether this host is about to
// forget the tip entirely, which is exactly the state an arm reaches when it
// holds standing to override `-d` (a push that landed, a merge that just
// committed, a branch already contained in the integration branch). Hence:
// FORCE deletes ask this predicate first, and a tip that no other ref reaches is
// REFUSED and NAMED rather than reaped.
//
// Reachability, not equality: a tip another branch has on its ancestry — a
// merge commit, a topic branch that picked it up — is not orphaned by this
// deletion, so the guard stays out of the way of ordinary reaping (see the
// negative half of the regression test: a merged branch is still deleted).
//
// THE DELIVERY POINT IS PART OF THE REF NAMESPACE (2026-09-18, the
// finish-cleanup chain's own shape). A caller that reached this guard through a
// DELIVERY — the chain pushed the branch to a target in step ② and is now
// force-deleting the local ref — holds evidence the local scan cannot see: the
// ref it just created at that target holds the very tip in question. Asking the
// local repo alone therefore answers "would this commit survive in no ref?"
// WRONG for exactly the delivered case, and the chain refuses its own healthy
// close-out (the two fixture tests red at master). So the predicate can be
// asked WITH a delivery point (`branchDeleteOrphanRefusalAt`), and the question
// becomes the honest one:
//
//	would deleting this branch leave its tip reachable from NO ref — the local
//	repo's refs OR the delivery point's?
//
// The delivery point is asked over the SAME channel the delivery used
// (`git ls-remote --heads`, the push's own addressing — a bare path for a local
// remote, an ssh alias for a peer), and asking it writes nothing into the repo
// under judgement. It is FAIL-CLOSED: unreadable, absent or MISMATCHED (the
// branch moved after the push, so the tip at risk is not the delivered one)
// falls through to the local scan and the refusal it would have reached anyway.
// The local scan is never weakened — it still runs, and still refuses an
// orphan.

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// branchDeleteGitTimeout bounds ONE git process of the orphan guard.
const branchDeleteGitTimeout = 30 * time.Second

// branchDeleteShortHashLen is how many hex digits a refusal sentence quotes.
const branchDeleteShortHashLen = 12

// branchDeleteRefsHeadsPrefix is the namespace every local branch lives in — the
// form both the exclusion below and `rev-parse --verify` need. It is the package
// constant repo_sweep.go already defines for the same purpose (Rule 14: one
// spelling of `refs/heads/`, not a per-file derivation — deriving it from the
// porcelain "branch " prefix produced a pattern that matched NO ref at all, so
// the branch under test counted as its own container and the guard could never
// refuse; measured: the oracle test now passes only with the real namespace).
const branchDeleteRefsHeadsPrefix = refsHeadsPrefix

// branchDeleteNoOtherRefMarker is the phrase a refusal carries — one string for
// an operator (or a test) to match without knowing the wording around it (the
// finishCleanupInUseMarker convention).
const branchDeleteNoOtherRefMarker = "its tip is reachable from no other ref"

// branchDeleteDeliveryPointMarker heads the sentence a refusal adds when a
// DELIVERY POINT was asked and did not hold the tip — the evidence beside the
// local scan, so an operator reading the refusal knows which refs were
// consulted (Rule 15: a checked step is reported, never silent).
const branchDeleteDeliveryPointMarker = "the delivery point"

// ── the MIRROR holder: a holder that is not a durable one ───────────────────
//
// INCIDENT (epic 01a12af7, case ④). A close-out's orphan guard asks the right
// question — "would this tip survive in some other ref?" — but it enumerated
// EVERY ref, and one family of refs answers YES without keeping anything:
//
//	refs/remotes/gitPushcheck/master   the pushback lane's per-pass mirror,
//	                                   written by a fetch and DELETED by the
//	                                   same pass's own defer
//	                                   (gitPushbackPruneMirrors).
//
// Measured on this host: a branch was deleted while its ONLY remaining holder
// was refs/remotes/gitPushcheck/master — an internal scratch ref with no
// promise of a next pass. The commit was one `git fetch` away from being
// unreachable, and every surface told the operator that its tip was "reachable
// from another ref", which was true in the same sense that a file in a
// directory about to be emptied is "on disk".
//
// WHAT THIS ARM IS. It began as a READER change at the report seam only (epic
// 01a12af7): when the holders a refusal would name are ALL non-durable, the
// sentence stopped claiming the tip was safe and printed the recovery command
// instead. It changes no decision — the refusal fired on exactly the same ref
// set as before — and the caller's arm was untouched.
//
// THE GUARD ITSELF WAS THEN FOUND TO HAVE THE SAME HOLE (epic 01a12f48,
// 2026-10-12), and the report-only boundary was not enough. Measured: an
// archived branch's tip survived with the object present and `git for-each-ref
// --contains` EMPTY — the commit reachable from no ref at all, one `git gc` from
// gone — while the arm that removed the ref had asked this very question and been
// told "another ref holds it". The ref it was told about was this mirror. A
// holder that its own writer deletes is not evidence the commit survives, so the
// guard now makes its decision on DURABLE holders only: a tip whose every
// container is scratch takes the SAME refusal as a tip with no container (with
// the holder named and the recovery command in the sentence). That is the
// original incident's own answer, applied to the ref set it was always about —
// not a new policy, and not a widened refusal: a tag, a branch or another
// remote's tracking ref still silences the guard exactly as it did.
//
// DURABLE, for this arm's purpose, means a ref a WRITER is responsible for
// keeping: a branch, a tag, another remote's tracking ref (the peer's own copy
// of the work). The pushback lane's namespace is the ONE family this file
// declares non-durable, and it names it through the constant that owns the
// spelling (gitPushbackCheckRootRef — Rule 14), never a second literal.
var branchDeleteNonDurableRefPrefixes = []string{gitPushbackCheckRootRef}

// branchDeleteNonDurableRef reports whether ref is a MIRROR the pushback lane
// writes and prunes within one pass — a holder that proves nothing about
// durability (see the block above).
func branchDeleteNonDurableRef(ref string) bool {
	for _, p := range branchDeleteNonDurableRefPrefixes {
		if strings.HasPrefix(ref, p) {
			return true
		}
	}
	return false
}

// branchDeleteDurableHolders keeps only the refs a writer is responsible for
// keeping. An EMPTY result is the fact this arm exists to report: the tip's
// only company is scratch that the next fetch may drop.
func branchDeleteDurableHolders(refs []string) []string {
	var out []string
	for _, ref := range refs {
		if !branchDeleteNonDurableRef(ref) {
			out = append(out, ref)
		}
	}
	return out
}

// branchDeleteMirrorOnlyMarker is the phrase that heads the mirror-only note —
// one string for an operator (or a test) to match without knowing the words
// around it (the branchDeleteNoOtherRefMarker convention).
const branchDeleteMirrorOnlyMarker = "its tip is held only by a scratch mirror"

// branchDeleteMirrorOnlyNote is the sentence a mirror-only refusal appends: it
// names the internal ref, says when that ref goes away, and gives the ONE
// command that makes the tip durable before it is too late.
//
// The command is `git branch <new-name> <ref>`, and it is deliberately a BRANCH
// rather than a tag: `git branch` REUSES the objects the repo already has (no
// copy, no new file, one ref write), while a tag writes a ref the tag namespace
// then owns forever. The recovery is a rename of the holding ref, not a
// snapshot — the operator's next move is usually to deliver the work, and a
// branch is what a delivery reads.
func branchDeleteMirrorOnlyNote(branch string, holders []string) string {
	if len(holders) == 0 {
		return ""
	}
	return "\n  " + branchDeleteMirrorOnlyMarker + ": the only other ref holding this tip is " +
		strings.Join(holders, " ") + " — the pushback lane's own per-pass mirror, which that lane DELETES when the pass ends. " +
		branchDeleteRecoveryHint(branch)
}

// branchDeleteRecoveryHint is the recovery sentence, spelled once (Rule 1b):
// both this arm and the close-out chain's own report (finishCleanupDeleteDeliveredBranch)
// render it, so an operator reads the same instruction wherever they meet it.
func branchDeleteRecoveryHint(ref string) string {
	return "Keep it now if you may still want it: `git branch " + ref + "-recovered " + ref + "`"
}

// branchDeleteLsRemoteVerb / branchDeleteLsRemoteHeadsFlag /
// branchDeleteLsRemoteFieldSep are the delivery-point probe's three git
// spellings, named once (Rule 1b). `ls-remote` is deliberate: it is the read
// side of the very channel the delivery wrote through (push → ls-remote), it
// needs no fetch — nothing is written into the repo under judgement — and
// `--heads` + an exact `refs/heads/<branch>` argument keeps the answer to ONE
// line, so no ref-pattern matching can widen it.
const (
	branchDeleteLsRemoteVerb      = "ls-remote"
	branchDeleteLsRemoteHeadsFlag = "--heads"
	branchDeleteLsRemoteFieldSep  = "\t"
)

// branchDeleteOrphanRefusal returns the NAMED refusal ("" = the delete is safe)
// when deleting branch in repo would leave its tip reachable from no other ref.
//
// This is the NO-DELIVERY-POINT form: the ref namespace is the repo itself.
// A caller that DELIVERED the ref (a push this sweep performed) asks
// branchDeleteOrphanRefusalAt with the target it pushed to — the local scan
// alone cannot see a ref the delivery created elsewhere, and refusing on that
// blindness is a false refusal (see the file comment).
func branchDeleteOrphanRefusal(repo, branch string) string {
	return branchDeleteOrphanRefusalAt(repo, branch, "")
}

// branchDeleteOrphanRefusalAt is the general form: deliveryPoint names a remote
// target the caller DELIVERED branch to ("" = the caller delivered nowhere, and
// only the local repo's refs are consulted). The delivery point is asked
// through the channel the delivery itself used (ls-remote), and counts as
// holding the tip only when it lists the DELIVERED tip — a target holding a
// different (e.g. since-moved) tip is no evidence for the tip at risk, so the
// local refusal stands.
//
// The scan enumerates every ref of the repo ONCE and asks git's own ancestry
// test for each branch other than the one being deleted — `git branch
// --contains` semantics, but with the deleted branch EXCLUDED, which the plain
// verb cannot express (it would count the ref under test as its own container).
// Tags, notes and remote-tracking refs are included for the same reason the
// incident's operator checked them: `git tag --contains <tip>` was empty too,
// and a tag is a perfectly good thing to keep a commit alive.
//
// FAIL-CLOSED: a repo whose refs cannot be read refuses the delete (an
// unreadable ref namespace is not evidence of a spare ref), and a tip that
// cannot be resolved (a branch that already vanished) is reported as such
// rather than read as "nothing to lose". The delivery point obeys the same
// posture: unreadable or mismatched = no evidence, never a pass.
func branchDeleteOrphanRefusalAt(repo, branch, deliveryPoint string) string {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "" // nothing named: nothing to orphan (callers skip blanks already)
	}
	ctx, cancel := context.WithTimeout(context.Background(), branchDeleteGitTimeout)
	defer cancel()
	tip := branchDeleteRevParse(ctx, repo, branchDeleteRefsHeadsPrefix+branch)
	if tip == "" {
		return "" // no such ref: the delete cannot orphan a tip that is not there
	}
	// The delivery point's verdict is settled FIRST (it is the caller's own
	// act, not a repo observation): only the exact tips it holds count.
	delivered := branchDeleteDeliveryPointHoldsTip(ctx, repo, deliveryPoint, branch, tip)
	refs, err := branchDeleteListRefs(ctx, repo)
	if err != nil {
		return "the repo's refs could not be read (" + err.Error() + ") — refusing to delete " + branch +
			" on an unreadable ref namespace" + branchDeleteDeliveryPointNote(deliveryPoint, branch, delivered)
	}
	containers := branchDeleteContainingRefs(ctx, repo, tip, branch, refs)
	// ONLY A DURABLE CONTAINER COUNTS (epic 01a12f48). The question this guard
	// asks is "would this commit survive in a ref?", and a ref that its own
	// writer DELETES when the pass ends answers it falsely — measured: a branch
	// was removed while its only remaining container was the pushback lane's
	// per-pass mirror (refs/remotes/gitPushcheck/master), so the tip's only
	// company was scratch the next pass drops and the commit was one `git gc`
	// from gone. That is the SAME state the no-container refusal exists for, so
	// it takes the SAME refusal — with the holder named, and the recovery
	// command in the sentence (the mirror half of this file).
	durable := branchDeleteDurableHolders(containers)
	if len(durable) == 0 && !delivered {
		refusal := branchDeleteNoOtherRefMarker + " (" + branchDeleteShortHash(tip) + ") — the commit would survive in no ref; " +
			"archive it (`gopi task archive " + branch + " --reason <code>` — the ONE writer, which also records WHY) " +
			"or delete it by hand" +
			branchDeleteDeliveryPointNote(deliveryPoint, branch, delivered)
		if len(containers) > 0 {
			// A scratch-only holder is a DIFFERENT fact from no holder at all,
			// and the operator has to be able to tell them apart: this one names
			// WHICH ref holds it, when that ref goes away, and the ONE command
			// that makes the tip durable now.
			return refusal + branchDeleteMirrorOnlyNote(branch, containers)
		}
		return refusal
	}
	return ""
}

// branchDeleteScratchOnlyHolders reports the refs that still contain `tip`
// after a delete, WHEN every one of them is a non-durable mirror — the measured
// fact behind the epic's case ④.
//
// IT IS A READER, DELIBERATELY SEPARATE FROM THE GUARD ABOVE. The guard decides
// whether a delete may happen, and it must keep deciding on exactly the ref set
// it always did (a refusal built on "are the holders durable?" would change the
// DECISION, and this epic changes only the REPORT). So the honesty lives here,
// at the report seam: the caller asks this AFTER the ref is gone, with the tip
// it captured BEFORE the delete, and amends a success line that would otherwise
// read "branch -d OK" while the commit's only company is scratch.
//
// Returns nil in every other case — no holders (the orphan guard's own case,
// already reported by the guard), a durable holder (the tip really is kept), an
// unreadable tip (nothing was captured before the delete) or an unreadable ref
// namespace ("unknown" is not a claim, Rule 15).
//
// The branch argument is excluded by name, exactly as branchDeleteContainingRefs
// excludes it: the caller may ask BEFORE the delete in a dry run, and the ref
// under judgement is never its own container.
func branchDeleteScratchOnlyHolders(ctx context.Context, repo, tip, branch string) []string {
	tip = strings.TrimSpace(tip)
	if tip == "" {
		return nil
	}
	refs, err := branchDeleteListRefs(ctx, repo)
	if err != nil {
		return nil
	}
	holders := branchDeleteContainingRefs(ctx, repo, tip, branch, refs)
	if len(holders) == 0 {
		return nil
	}
	if len(branchDeleteDurableHolders(holders)) > 0 {
		return nil // a real ref keeps it: the ordinary, unremarkable shape
	}
	return holders
}

// branchDeleteTipOf resolves a branch's tip for a caller that must capture it
// BEFORE the ref goes away ("" when the ref does not resolve — the ordinary
// second-pass shape, and never an error: there is nothing to remember about a
// branch that is already gone).
func branchDeleteTipOf(ctx context.Context, repo, branch string) string {
	return branchDeleteRevParse(ctx, repo, branchDeleteRefsHeadsPrefix+branch)
}

// branchDeleteDeliveryPointHoldsTip reports whether the delivery point lists
// refs/heads/<branch> at exactly tip — the evidence that the delivery kept the
// commit, obtained with ONE read-only `git ls-remote` over the delivery's own
// addressing. Every failure mode (no point named, unreachable point, absent
// ref, a different tip, unparseable output) answers false, which is the
// fail-closed direction: the caller then decides on the local scan alone.
// Nothing is written, fetched or cloned into repo.
//
// workDir is where the git process runs, NOT the target: a delivery point is a
// REMOTE ADDRESS (an ssh alias, `zbook:~/work/GoPi`) and is passed as `git
// ls-remote`'s repository ARGUMENT — it is never a working directory, and a
// relative path among its spellings therefore resolves against the same
// directory the delivery's own push ran in (the judged repo).
func branchDeleteDeliveryPointHoldsTip(ctx context.Context, workDir, deliveryPoint, branch, tip string) bool {
	if strings.TrimSpace(deliveryPoint) == "" {
		return false
	}
	out, err := branchDeleteGit(ctx, workDir, branchDeleteLsRemoteVerb,
		branchDeleteLsRemoteHeadsFlag, deliveryPoint, branchDeleteRefsHeadsPrefix+branch)
	if err != nil {
		return false
	}
	return branchDeleteLsRemoteTip(out) == tip
}

// branchDeleteLsRemoteTip reads the hash out of one `ls-remote` line (the
// format is "<hash>\t<refname>", one line because the caller passed the exact
// ref). "" when the output carries no such line.
func branchDeleteLsRemoteTip(out string) string {
	for _, line := range strings.Split(out, "\n") {
		hash, _, found := strings.Cut(strings.TrimSpace(line), branchDeleteLsRemoteFieldSep)
		if found {
			return strings.TrimSpace(hash)
		}
	}
	return ""
}

// branchDeleteDeliveryPointNote renders the evidence sentence a refusal adds
// when a delivery point was asked: which point, the delivered ref, and the
// verdict ("did not hold ..." / "not readable, so it could not be asked"). An
// empty note means no point was named — there is nothing to report.
func branchDeleteDeliveryPointNote(deliveryPoint, branch string, holds bool) string {
	if strings.TrimSpace(deliveryPoint) == "" {
		return ""
	}
	if holds {
		return "" // unreachable by construction: a held tip is not refused
	}
	return " — " + branchDeleteDeliveryPointMarker + " " + deliveryPoint + " did not hold " +
		branchDeleteRefsHeadsPrefix + branch + " at that tip (unreadable or a different tip)"
}

// branchDeleteShortHash shortens a resolved hash for a refusal sentence: 12 hex
// digits is what the incident's reports quoted, unambiguous at this repo's size
// and still readable.
func branchDeleteShortHash(hash string) string {
	if len(hash) <= branchDeleteShortHashLen {
		return hash
	}
	return hash[:branchDeleteShortHashLen]
}

// branchDeleteRevParse resolves one ref to its full hash ("" when absent or
// unreadable — both mean "no tip of this name to orphan").
func branchDeleteRevParse(ctx context.Context, repo, ref string) string {
	out, err := branchDeleteGit(ctx, repo, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// branchDeleteListRefs enumerates every ref name in repo (heads and tags alike),
// in one git call.
func branchDeleteListRefs(ctx context.Context, repo string) ([]string, error) {
	out, err := branchDeleteGit(ctx, repo, "for-each-ref", "--format=%(refname)")
	if err != nil {
		return nil, err
	}
	var refs []string
	for _, line := range strings.Split(out, "\n") {
		if l := strings.TrimSpace(line); l != "" {
			refs = append(refs, l)
		}
	}
	return refs, nil
}

// branchDeleteContainingRefs returns every ref (other than refs/heads/<branch>)
// whose tip has tip on its ancestry — the "would this commit survive?" answer.
func branchDeleteContainingRefs(ctx context.Context, repo, tip, branch string, refs []string) []string {
	excluded := branchDeleteRefsHeadsPrefix + branch
	var out []string
	for _, ref := range refs {
		if ref == excluded {
			continue
		}
		if _, err := branchDeleteGit(ctx, repo, "merge-base", "--is-ancestor", tip, ref); err == nil {
			out = append(out, ref)
		}
	}
	return out
}

// ── the `-d` ARMS' second lock, made explicit ───────────────────────────────
//
// WHY THIS EXISTS (MEASURED 2026-10-12, epic 01a12f68). Every ref-removing arm
// that does not ask branchDeleteOrphanRefusal relies on git's own lock instead:
// `git branch -d` refuses a branch that is not merged. The gap this arm closes
// was stated as a HYPOTHESIS by the report that fixed the guard (verified by
// driving the real git in a fixture repo — raw output in this run's session dir):
//
//	① `git branch -d` is NOT master-anchored. It consults the branch's UPSTREAM
//	   first, and a branch whose upstream is one of the pushback lane's scratch
//	   mirrors is deleted — with a warning, exit 0 — even though its tip is an
//	   ancestor of NOTHING in the integration branch:
//
//	     $ git branch -d topic
//	     warning: deleting branch 'topic' that has been merged to
//	              'refs/remotes/gitPushcheck/topic', but not yet merged to HEAD.
//	     Deleted branch topic (was 32a2803).        RC=0
//	     $ git merge-base --is-ancestor 32a2803 master   → rc=1 (NOT an ancestor)
//
//	② That lock is NOT established by any lock this repo's arms rely on: an
//	   ordinary `fetch` into the mirror namespace leaves NO branch.<name>.remote
//	   config and no `%(upstream)`, so a mirror-as-upstream can only come from an
//	   explicit `git branch --set-upstream-to` / `push -u` that THIS REPO NEVER
//	   ISSUES (measured: the only branch with an upstream in the real repo is
//	   master tracking refs/remotes/zbook/master — a DURABLE ref, the shape git's
//	   rule was written for). So the mirror-upstream hazard is real but unobserved.
//
//	③ The lock the arms actually DO hold is a READ of the integration branch
//	   (`merge-base --is-ancestor <branch> master`) taken elsewhere and earlier —
//	   in the worst case by a DIFFERENT process on a DIFFERENT tip (the merger has
//	   no lock against the branch's own run still committing).
//
// So this helper does not create a new policy: it asks, at the delete site, the
// question the arms' comments claim is already answered — "is this tip in the
// integration branch, or does a DURABLE ref hold it?" — and returns a refusal
// when neither lock fires. The refusal is rendered as a KEPT branch (never an
// error), which is exactly what task_finish_cleanup.go's own entrance contract
// already does: it deletes through finishCleanupDeleteDeliveredBranch, which
// asks branchDeleteOrphanRefusalAt and escalates to `-D` only when the guard
// allows. One behaviour, and now one spelling.
//
// It is the SAME question the guard above answers, so it reuses its parts: the
// tip is resolved once, the refs are listed once, and the durable/scrap
// classification is branchDeleteDurableHolders (the family that declares the
// pushback lane's namespace scratch lives there, Rule 14).
// branchDeleteMergeBaseVerb / branchDeleteIsAncestorFlag are the two git
// spellings of lock ① (Rule 1b: named once, shared by the guard and this arm).
const (
	branchDeleteMergeBaseVerb  = "merge-base"
	branchDeleteIsAncestorFlag = "--is-ancestor"
)

// branchDeleteUpstreamFormat is the one reader that answers "does git's UPSTREAM
// lock bind for this branch at all" — asked for the refusal's EVIDENCE sentence,
// never to decide (the decision is lock ① + durability below).
const branchDeleteUpstreamFormat = "--format=%(upstream:short)"

// branchDeleteScratchTipMarker heads the refusal sentence. One string to match.
const branchDeleteScratchTipMarker = "not merged into the integration branch and its tip is held only by scratch"

// branchDeleteNonDurableTipRefusal returns the NAMED refusal ("" = the delete is
// safe) when deleting branch in repo would rest on NEITHER lock: the tip is not
// in the integration branch AND no durable ref holds it. Callers ask it BEFORE
// the delete and keep the branch when it answers — never delete first.
func branchDeleteNonDurableTipRefusal(repo, branch, integration string) string {
	branch = strings.TrimSpace(branch)
	integration = strings.TrimSpace(integration)
	if branch == "" || integration == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), branchDeleteGitTimeout)
	defer cancel()
	tip := branchDeleteRevParse(ctx, repo, branchDeleteRefsHeadsPrefix+branch)
	if tip == "" {
		return "" // no such ref: nothing to orphan
	}
	// Lock ① — git's own: the tip is IN the integration branch. Asked against
	// the tip resolved NOW, which is the whole point: an ancestor read taken
	// earlier (by this arm's caller, or by another process) proves nothing about
	// the ref standing here now.
	if _, err := branchDeleteGit(ctx, repo, branchDeleteMergeBaseVerb, branchDeleteIsAncestorFlag, tip, integration); err == nil {
		return ""
	}
	refs, err := branchDeleteListRefs(ctx, repo)
	if err != nil {
		return branchDeleteScratchTipMarker + " for " + branch + " (" + branchDeleteShortHash(tip) + "), and the refs could not be read (" + err.Error() + ")"
	}
	if len(branchDeleteDurableHolders(branchDeleteContainingRefs(ctx, repo, tip, branch, refs))) > 0 {
		return "" // a real ref keeps the commit: the ordinary shape
	}
	return branchDeleteScratchTipMarker + ": " + branch + " (" + branchDeleteShortHash(tip) + ") is not an ancestor of " +
		integration + ", no durable ref (branch, tag, another remote's tracking ref) contains it, and the merged-only lock of a plain branch delete is not a lock against that" +
		branchDeleteUpstreamEvidence(ctx, repo, branch) + " — kept as it is; " + branchDeleteRecoveryHint(branch)
}

// branchDeleteUpstreamEvidence renders the `%(upstream)` reading for the refusal
// sentence: an EMPTY answer is itself the evidence (git's upstream lock does not
// bind this branch), so it is printed as a named fact rather than dropped —
// "no upstream" and "could not read" are different findings (Rule 15).
func branchDeleteUpstreamEvidence(ctx context.Context, repo, branch string) string {
	out, err := branchDeleteGit(ctx, repo, branchDeleteForEachRefVerb, branchDeleteUpstreamFormat, branchDeleteRefsHeadsPrefix+branch)
	if err != nil {
		return " (its upstream could not be read: " + err.Error() + ")"
	}
	if u := strings.TrimSpace(out); u != "" {
		return " (git's upstream lock here resolves to " + u + ", not to the integration branch)"
	}
	return " (this branch has no upstream at all, so that lock never bound it)"
}

// BranchDeleteNonDurableTipRefusal is branchDeleteNonDurableTipRefusal for
// callers outside this package (Rule 14: a value/behaviour shared across
// packages has ONE definition, exported). cmd/agent's `gopi merge` run is the
// caller — its three delete sites sat on git's `-d` lock alone (epic 01a12f68).
func BranchDeleteNonDurableTipRefusal(repo, branch, integration string) string {
	return branchDeleteNonDurableTipRefusal(repo, branch, integration)
}

// branchDeleteForEachRefVerb is git's ref-enumerating verb, named once.
const branchDeleteForEachRefVerb = "for-each-ref"

// branchDeleteGit runs one git command in repo and returns its stdout.
func branchDeleteGit(ctx context.Context, repo string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
