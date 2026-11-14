package tools

// worktree_sweep_undelivered.go — the UNDELIVERED-COMMITS rung of every arm that
// frees a run's ground (epic: the sweep's missing criterion).
//
// THE DEFECT THIS CLOSES, stated as it was measured. A removal arm judged a
// ground by three facts — how old it is, whether its tree is clean, whether a
// live process stands in it — and never asked the one question that decides
// whether removing the DIRECTORY destroys WORK: do the commits this checkout
// holds exist anywhere else?
//
// WHY BOTH GROUND SHAPES ARE COVERED, and what the removal destroys in each —
// the half a first draft of this file got wrong twice, in opposite directions,
// and both corrections came from running the thing (2026-10-22):
//
//	· LINKED WORKTREE (`.git` is a FILE). `git worktree remove` deletes the
//	  directory; the branch and its objects survive in the main repo, so a naive
//	  reading says "nothing is lost". The measurement says otherwise: the
//	  operator's symptom is `[closeout-refused]: the recorded worktree directory
//	  is not there` — the row's recorded deliverable is the DIRECTORY, the
//	  delivery is a single shot per row taken FROM that directory, and nothing
//	  re-runs it (task_finish_cleanup.go, its ④ comment, measured on HP
//	  2026-09-26: `settled=7 refused=3`, every one that sentence). A surviving
//	  ref that nothing will ever push is not delivery.
//	· CLONE GROUND (`.git` is a DIRECTORY — internal/devworkspace). The
//	  directory IS a repository, and `devworkspace.Reclaim` removes it with
//	  `os.RemoveAll`, taking objects and any ref that existed only there.
//
// Measured on this host, 2026-10-22, so the file states evidence rather than a
// story: (a) the anchor axis's `git worktree remove` REFUSES a clone ground
// ("is not a working tree", exit 128), so that axis cannot be the remover of a
// clone; (b) every ground standing in /home/user/workspace on that date was a
// clone whose HEAD was already in master (ahead=0) — the loss this rung exists
// to prevent had not yet happened there, which is why the rung's evidence comes
// from the mechanism and from the sandbox fixture below, not from a corpse;
// (c) the removal-audit sink records 735 distinct /home/user/workspace targets
// already removed, 472 of them `gopi-<stem>` LINKED grounds removed by
// `task-self-reclaim` / `task-self-own-sweep` — the shape whose delivery path
// dies with the directory.
//
// THE PREDICATE. One question, asked of git directly (never of a clock or a
// ledger status, both of which are proxies the defect proves wrong):
//
//	how many commits does the registration's branch hold that NO delivery point
//	has?  (`git rev-list --count <branch> --not <every point that resolves>`)
//
// A delivery point is where the branch's work is allowed to live besides this
// directory, and there are exactly two kinds:
//
//	① THE REMOTE'S COPY OF THE SAME BRANCH (`refs/remotes/<remote>/<branch>`) —
//	   the clone channel's own contract (`gitPushcheck`): the worker pushes the
//	   branch, and THAT push is the delivery (Rule 6c).
//	② THE INTEGRATION BRANCH, local or remote tracking — the merger's delivery,
//	   the shape that makes a finished branch's ground truly spent.
//
// The point that is deliberately NOT on the list is `refs/heads/<branch>` — the
// branch ITSELF. It always resolves (the ground is standing on it), so counting
// it would make every ground "delivered" by construction: the measurement has to
// name a place the work exists OUTSIDE the thing being removed.
//
// A count above zero KEEPS the ground, in its own sentence, naming the number
// and every ref the question was asked about — an operator who reads "held" must
// be able to see what holds it and how to release it (push the branch, or merge
// it), which is what the reason says.
//
// UNMEASURABLE IS NOT DELIVERED. A probe that cannot resolve the branch, cannot
// run git, or finds NO delivery point at all answers Measured=false, and the
// caller KEEPS: the rung's whole purpose is that "I could not tell whether this
// is the only home of that work" is not a licence to delete it (Rule 15a, the
// same direction every other unmeasurable rung on these axes takes).

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// The probe's git vocabulary, each spelled once (Rules 1b/13/14).
const (
	// worktreeUndeliveredRevListVerb is git's `rev-list` — the ONE command this
	// file runs; its `--count` form answers the rung's question directly instead
	// of making this file walk a commit list.
	worktreeUndeliveredRevListVerb = "rev-list"
	// worktreeUndeliveredCountFlag makes rev-list print a NUMBER, not commits.
	worktreeUndeliveredCountFlag = "--count"
	// worktreeUndeliveredNotFlag excludes the delivery points from the count:
	// "commits reachable from <branch> and from none of these".
	worktreeUndeliveredNotFlag = "--not"
	// worktreeUndeliveredMergeBaseVerb is git's `merge-base` — used ONLY to
	// detect the unrelated-history shape the lineage rung owns, never to measure
	// delivery (that is rev-list's answer above).
	worktreeUndeliveredMergeBaseVerb = "merge-base"
)

// worktreeUndeliveredProbe is the rung's answer for one registration. It is a
// struct rather than a bare int because "zero" and "could not ask" are different
// facts and the caller must keep the second (Rule 15a).
type worktreeUndeliveredProbe struct {
	// Ahead is how many commits the branch holds that no delivery point has.
	Ahead int
	// Measured reports whether the question was ANSWERED. False means the caller
	// must keep (see the file doc: unmeasurable is never delivered).
	Measured bool
	// Points is every delivery ref the question was asked about, as printed in
	// the refusal so an operator can reproduce the measurement by hand.
	Points []string
	// Commits is the object id of every commit the count stands for, so the
	// equivalence legs (worktree_sweep_undelivered_equivalent.go) can ask about
	// each one instead of re-deriving the set with a second rev-list.
	Commits []string
	// Equivalent records, one line each, the commits this probe's count was
	// REDUCED by because the fresh integration tip provably carries their work
	// under another id (a rebase or a re-land). It is evidence, never a claim: a
	// reader can re-run each line's git command by hand.
	Equivalent []string
	// Why is the failure sentence when Measured is false (Rule 15: a refusal
	// that does not say why is noise).
	Why string
}

// worktreeUndeliveredDeliveryPoints is the ref set the count is taken AGAINST,
// in the order the refusal prints them. `refs/heads/<branch>` is absent by
// design — see the file doc.
func worktreeUndeliveredDeliveryPoints(branch string) []string {
	candidates := []string{
		refsRemotesPrefix + deliveryProofOriginRemote + "/" + branch,
		refsRemotesPrefix + deliveryProofOriginRemote + "/" + IntegrationBranchName,
		refsHeadsPrefix + IntegrationBranchName,
	}
	// THE BRANCH ITSELF IS NEVER A DELIVERY POINT (measured, 2026-10-22).
	//
	// The file doc already says `refs/heads/<branch>` is absent by design — but
	// the list above spells the INTEGRATION branch's local ref, and a CLONE
	// GROUND's `deliverable.branch` IS the integration branch (every worker row
	// on this host writes `"branch":"master"`; measured on the live ledger and
	// on all four standing clone grounds). When branch == master the third
	// candidate IS the branch under measurement, so the count is taken against
	// itself and git answers 0 by construction:
	//
	//	$ git -C <clone ground> rev-list --count master \
	//	    --not refs/remotes/origin/master refs/remotes/origin/master refs/heads/master
	//	0
	//	$ git -C <clone ground> rev-list --count master --not refs/remotes/origin/master
	//	1                      # the commit that only this ground holds
	//
	// A zero from a self-referential measurement is not "delivered", it is "the
	// question was never asked" — and the caller acts on it by DELETING the
	// directory (`devworkspace.Reclaim` → `os.RemoveAll`), which is exactly the
	// loss this rung exists to prevent. So the exclusion is applied to the BUILT
	// list rather than assumed from the branch's spelling: any candidate equal
	// to `refs/heads/<branch>` is dropped, whatever the branch is called.
	// THE SAME `branch == master` COLLAPSE HAS A SECOND CONSEQUENCE, measured on
	// this host 2026-10-27: candidate ① (`--branch`) and candidate ② (the
	// integration branch) both spell `refs/remotes/origin/master` for a CLONE
	// ground, because such a ground's `deliverable.branch` IS master. Left as
	// it was, the keep sentence named ONE delivery point TWICE in all 76 of the
	// grounds the rung kept on that date:
	//
	//	(refs/remotes/origin/master, refs/remotes/origin/master)
	//
	// That is a Rule 15 defect and not a cosmetic one: the sentence is the thing
	// an operator acts on ("push the branch or merge it into master"), and "two
	// refs hold nothing" reads as two separate facts when there is one ref. So
	// the dedup is applied by VALUE over the BUILT list, exactly like the
	// self-exclusion below — never by position, so it holds for every spelling.
	self := refsHeadsPrefix + branch
	out := make([]string, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	for _, p := range candidates {
		if p == self || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// worktreeUndeliveredCommits asks the rung's question for one branch, on one
// repository. `repo` is where the refs are read (a linked worktree shares its
// main checkout's object store and refs, so any of the two answers the same
// question — `repo` is used because it is always the sweep's own repo argument).
//
// Every `git` failure is a MEASUREMENT failure, never a zero: the only path that
// answers Ahead==0 is a rev-list that RAN and printed 0.
func worktreeUndeliveredCommits(repo, branch string) worktreeUndeliveredProbe {
	repo = strings.TrimSpace(repo)
	branch = strings.TrimSpace(branch)
	if repo == "" || branch == "" {
		return worktreeUndeliveredProbe{
			Why: fmt.Sprintf("the registration names no branch to measure (repo %q, branch %q)", repo, branch),
		}
	}
	p := worktreeUndeliveredProbe{}
	// Resolve the branch FIRST: a registration whose branch no longer exists has
	// nothing to deliver, and it is also not a failure — it is an answer, so it
	// is Measured with Ahead 0 (the dead-registration rung handles the rest).
	ctx, cancel := context.WithTimeout(context.Background(), worktreeSweepGitTimeout)
	defer cancel()
	if _, err := worktreeUndeliveredGit(ctx, repo, "rev-parse", "--verify", "--quiet", refsHeadsPrefix+branch); err != nil {
		p.Measured = true
		return p
	}
	args := []string{worktreeUndeliveredRevListVerb, worktreeUndeliveredCountFlag, branch}
	for _, point := range worktreeUndeliveredDeliveryPoints(branch) {
		if _, err := worktreeUndeliveredGit(ctx, repo, "rev-parse", "--verify", "--quiet", point); err != nil {
			// A delivery point the repo does not have answers nothing: it is
			// skipped, not invented (a fixture with no remote must still be
			// measurable, or every local checkout would be unmeasurable).
			continue
		}
		p.Points = append(p.Points, point)
	}
	if len(p.Points) == 0 {
		p.Why = "no delivery point resolves in " + repo + " (tried " +
			strings.Join(worktreeUndeliveredDeliveryPoints(branch), ", ") + ")"
		return p
	}
	args = append(args, worktreeUndeliveredNotFlag)
	args = append(args, p.Points...)
	out, err := worktreeUndeliveredGit(ctx, repo, args...)
	if err != nil {
		p.Why = err.Error()
		return p
	}
	n, cerr := strconv.Atoi(strings.TrimSpace(out))
	if cerr != nil {
		p.Why = "git " + worktreeUndeliveredRevListVerb + " answered " + strconv.Quote(strings.TrimSpace(out)) +
			", which is not a count"
		return p
	}
	p.Ahead = n
	p.Measured = true
	// The commit ids behind the count, read with the SAME rev-list question so
	// the set and the number cannot disagree. A failure here is not a measurement
	// failure — the count already answered — so it leaves Commits empty and the
	// equivalence legs simply do not run (see worktreeUndeliveredSubtractEquivalent).
	listArgs := []string{worktreeUndeliveredRevListVerb, branch, worktreeUndeliveredNotFlag}
	listArgs = append(listArgs, p.Points...)
	if list, lerr := worktreeUndeliveredGit(ctx, repo, listArgs...); lerr == nil {
		for _, line := range strings.Split(list, "\n") {
			if sha := strings.TrimSpace(line); sha != "" {
				p.Commits = append(p.Commits, sha)
			}
		}
	}
	return p
}

// worktreeUndeliveredGit runs one git command in dir and returns its trimmed
// stdout. stderr is carried into the error so a caller that refuses can quote
// git's own words instead of paraphrasing them (Rule 15).
func worktreeUndeliveredGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, gitBinaryName, args...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", &worktreeUndeliveredGitError{args: args, err: err, detail: msg}
		}
		return "", &worktreeUndeliveredGitError{args: args, err: err}
	}
	return strings.TrimSpace(string(out)), nil
}

// worktreeUndeliveredGitError keeps git's argv and its own diagnostic together,
// so an unmeasurable refusal names the exact command that failed.
type worktreeUndeliveredGitError struct {
	args   []string
	err    error
	detail string
}

func (e *worktreeUndeliveredGitError) Error() string {
	msg := "git " + strings.Join(e.args, " ") + ": " + e.err.Error()
	if e.detail != "" {
		msg += ": " + e.detail
	}
	return msg
}

func (e *worktreeUndeliveredGitError) Unwrap() error { return e.err }

// worktreeUndeliveredRefusal renders the rung's keep sentence for a probe that
// answered Ahead>0: the count, the branch, the refs the question was asked
// against, and the two actions that make the ground collectable. ONE spelling
// (Rule 13), referenced by both removal axes and by the tests.
func worktreeUndeliveredRefusal(branch string, p worktreeUndeliveredProbe) string {
	return "the ground's branch " + branch + " holds " + plural(p.Ahead) +
		" that no delivery point has (" + strings.Join(p.Points, ", ") + ")" +
		" — this directory is that work's only home: push the branch or merge it into " +
		IntegrationBranchName + ", then the ground is collectable"
}

// worktreeUndeliveredUnmeasurable renders the keep sentence for a probe that
// could not answer. The two are different facts and never share a sentence: one
// says the work IS here, the other says nobody finished looking (Rule 15).
func worktreeUndeliveredUnmeasurable(branch string, p worktreeUndeliveredProbe) string {
	why := p.Why
	if why == "" {
		why = "the probe answered nothing"
	}
	return "could not measure whether branch " + branch +
		" carries commits no delivery point has (" + why + ") — keeping the ground"
}

// worktreeUndeliveredSharesNoAncestor reports whether `branch` and the
// integration branch share no common ancestor — the shape whose verdict belongs
// to the LINEAGE rung, not to this one (see the caller's doc). A repository that
// cannot answer reads FALSE: an unanswerable lineage question is not a lineage
// verdict, and both callers must keep answering rather than fall silent on a
// repository they cannot judge.
func worktreeUndeliveredSharesNoAncestor(readFrom, branch string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), worktreeSweepGitTimeout)
	defer cancel()
	if _, err := worktreeUndeliveredGit(ctx, readFrom, "rev-parse", "--verify", "--quiet",
		refsHeadsPrefix+branch); err != nil {
		return false
	}
	if _, err := worktreeUndeliveredGit(ctx, readFrom, worktreeUndeliveredMergeBaseVerb,
		IntegrationBranchName, branch); err != nil {
		return true
	}
	return false
}

// worktreeSweepUndeliveredRefusal is the rung's CALLER-SHAPED face: given the
// directory about to be removed, its repo and its branch, it returns "" when the
// removal may proceed, or the keep sentence when it may not. Every removal arm
// calls THIS (Rules 13/14: one implementation, never a second reading of the
// probe).
//
// IT ANSWERS "" FOR EVERY LINKED WORKTREE, and that is a fact rather than a
// concession: `git worktree remove` deletes the directory, while the branch and
// its objects live in the main repo the registration shares — so a linked ground
// cannot take work with it, and refusing there would stop the release pass
// without protecting anything. The ground whose directory IS its only repository
// is the CLONE channel's (deliveryProofIsCloneGround — the discriminator the
// branch-delete and publish-target guards already use, Rule 35).
func worktreeSweepUndeliveredRefusal(ground, repo, branch string) string {
	// A DIRECTORY THAT IS ALREADY GONE HAS NOTHING FOR THIS RUNG TO PROTECT, and
	// the sentence it would otherwise print is simply FALSE. Measured on this
	// host (2026-10-23) by running the rung for real: the anchor sink still
	// names /home/user/workspace/gopi-closeout-sweep, whose own removal record
	// is `{"ts":"2026-10-22T21:13:48","kind":"worktree-removal","arm":
	// "task-self-own-sweep","target":"/home/user/workspace/gopi-closeout-sweep"}`
	// — the directory had been gone for a day, while this rung reported "this
	// directory is that work's only home: push the branch or merge it into
	// master, then the ground is collectable".
	//
	// Two costs, both Rule 15's: the operator is told something untrue about
	// their own filesystem, and the keep is PERMANENT — every other rung on the
	// anchor axis relaxes with TIME (the age floor) or with a live process
	// (the holder scan), while a commit count is a fixed fact about a ref, so a
	// registration whose directory no longer exists would be parked forever.
	// "Removing this directory destroys work" is moot when there is no
	// directory: no removal can destroy what was already removed, and a loss
	// that already happened is not undone by refusing later.
	//
	// The right surface for "this anchor's directory is gone" is the
	// dead-registration rung, which already exists and already owns that shape.
	if _, err := os.Stat(strings.TrimSpace(ground)); err != nil {
		return ""
	}
	// WHERE THE REFS ARE READ — the one thing the two ground shapes disagree
	// about, and getting it wrong answers a silent zero (measured 2026-10-22 on
	// the sandbox fixture: `git rev-parse --verify refs/heads/task/undeliv-demo`
	// exits 1 in the main repo and 0 in the clone ground carrying it).
	//
	//	· CLONE GROUND — a clone of its own: the run's branch lives in ITS refs
	//	  and was never fetched back into the repo it came from, so the question
	//	  is asked IN THE GROUND.
	//	· LINKED WORKTREE — shares the main repo's refs and object store by
	//	  construction, so `repo` sees the very same refs and either answers
	//	  identically.
	readFrom := repo
	if deliveryProofIsCloneGround(ground) {
		readFrom = ground
	}
	// AN UNRELATED-HISTORY BRANCH IS THE LINEAGE RUNG'S CASE, NOT THIS ONE. A
	// branch that shares NO common ancestor with the integration branch makes
	// `git rev-list --count` report every commit it has — correct arithmetic and
	// the wrong owner: `reclaim_ground_checklist.go`'s rung ② already refuses
	// that shape with its own named sentence, and the existing arm-level test
	// (TestRunGroundSweepKeepsAGroundWhoseBranchSharesNoAncestor) pins THAT
	// refusal at the apply step. Answering here would preempt it at the plan
	// step and turn a pinned contract into an unpinned one. So: no common
	// ancestor → this rung says nothing, and the lineage check speaks.
	// The hand-off is only to a rung that CAN speak: an integration branch that
	// does not resolve here is not a lineage verdict, it is a missing ref, and
	// this rung must keep answering (unmeasurable → keep) rather than fall silent
	// on a repository it cannot judge. `git merge-base --is-ancestor`'s sibling —
	// resolution first, verdict second.
	ctx, cancel := context.WithTimeout(context.Background(), worktreeSweepGitTimeout)
	defer cancel()
	if _, err := worktreeUndeliveredGit(ctx, readFrom, "rev-parse", "--verify", "--quiet",
		refsHeadsPrefix+IntegrationBranchName); err == nil {
		if worktreeUndeliveredSharesNoAncestor(readFrom, branch) {
			return ""
		}
	}
	p := worktreeUndeliveredCommits(readFrom, branch)
	if !p.Measured {
		return worktreeUndeliveredUnmeasurable(branch, p)
	}
	// THE DATA-SOURCE HALF (worktree_sweep_undelivered_equivalent.go). The count
	// above is taken against the delivery points THIS repository has, and a clone
	// ground's `refs/remotes/origin/master` is a snapshot nothing ever fetches
	// (measured on this host 2026-10-27: 206 of 208 clone grounds carry a stale
	// one, the measured case 12h54m behind). So a commit that IS on the
	// integration branch reads as undelivered. The rung's predicate stays as it
	// is — the count is still the answer — and the count is reduced only by
	// commits whose work the FRESH integration tip provably carries under another
	// id (rebase rewrites the hash, a re-land rewrites the subject).
	//
	// `repo` is the fresh source: for a clone ground it is the MAIN CHECKOUT the
	// record names, i.e. the repo the merger and the operator keep current, and
	// reading a ref from it is one local git call (no fetch anywhere: see the
	// equivalence file's header and Rule 35). Where `repo` IS the ground (the
	// adopt arm passes the directory twice) there is no fresher source to ask, so
	// the legs decline and behaviour is exactly the pre-fix one.
	p = worktreeUndeliveredSubtractEquivalent(readFrom, repo, branch, p)
	if p.Ahead > 0 {
		return worktreeUndeliveredRefusal(branch, p)
	}
	return ""
}
