package tools

// deploy_drift.go — the measured DISTANCE between the artifact a checkout's bin/
// carries and the branch that checkout would build from.
//
// THE MEASURED CASE (host nick-HP-Laptop, 2026-10-14, epic 01a137cb). A dispatch
// seam's guard was correct in the source, wired at the ONE spawn seam, and green
// for 74 commits — and absent from the artifact the machine actually ran.
// /usr/local/bin/gopi is a SYMLINK to /home/nick/work/GoPi/bin/gopi, so the
// deployed binary IS that checkout's bin/gopi (one file, not two:
// `sha256sum $(which gopi) <repo>/bin/gopi` printed one digest). `strings` on it
// found the guard's SIBLING literal once and its own literal zero times — the
// deployed artifact predates the wiring commit.
//
// The distance was not one layer deep, and that is why this file measures REFS
// rather than only the binary: the artifact's own attested revision
// (c3a3f4eae74c, via the report `gopi version` prints) IS HP's local
// refs/heads/master tip — so a rebuild would find it ALREADY CURRENT and skip
// (RebuildAfterMerge's `versionMatches(currentRev, tip)` short-circuit, measured:
// the two strings are equal). HP's local master is itself 97 commits behind the
// mirror its grounds are cut from. So "rebuild it" is the wrong remedy here, and
// a checker that only said "your binary is old" would have sent the operator
// down it.
//
// WHAT THIS FILE IS: three read-only measurements, composed from machinery that
// already exists — binaryVersion (rebuild.go's own artifact probe, which reads
// the report and recovers the revision), versionMatches (rebuild.go's own
// follow-the-tip comparison), and the repo's refs. Nothing here rebuilds,
// fetches, writes or schedules anything: Rule 35's default is a one-shot the
// operator invokes, and the machinery that rebuilds AFTER a landing already ticks
// (mergerPassCloseLoop → serveRebuildAfterMerge → RebuildAfterMerge).
//
// WHY IT IS NOT A CRON LINE OR A DAEMON: the failure this measures is that the
// machine ran an artifact 74 commits old — a fact about a moment, not about a
// schedule. A resident watcher would be a new persistent dependency (Rule 35),
// and the mechanism that fixes it already exists; what was missing is an
// operator-invocable way to SEE the distance. This is that, and it is a flag on
// `gopi version` — the verb whose whole subject is "what is this artifact".

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/devworkspace"
)

// DeployDriftRefs are the refs a deployed artifact may be measured against, in
// the order they are reported: the branch this checkout would BUILD from, then
// the integration mirror its grounds are cut from, then the push-back lane's
// scratch mirror.
//
// THE THIRD ENTRY IS MEASURED BUT DOES NOT VOTE (see deployDriftRefIsScratch and
// DeployDriftRef.Scratch). It is kept in the list because the number is real
// evidence about that lane — and it is kept out of the summary verdict because
// NOTHING AN OPERATOR DOES MOVES IT: `refs/remotes/gitPushcheck/master` is
// written by a push-back pass and pruned by that same pass
// (gitPushbackPruneMirrors), so on a host that never runs one it freezes and
// falls further behind on every landing (measured 2026-10-28/29 on host
// nick-sager: 3 days old, 405 commits and monotonically growing, then 426).
// A ref that no operator action can bring the artifact to follow cannot make
// the artifact stale; saying it did was the contradiction this file now
// removes (its own line read "not stale behind it" beside a printed `STALE:`).
var DeployDriftRefs = []string{
	"refs/heads/" + IntegrationBranchName,
	refsRemotesPrefix + deployDriftOriginRemote + "/" + IntegrationBranchName,
	gitPushbackCheckRootRef + IntegrationBranchName,
}

// DeployDriftVerdicts are the answers a measured ref can give. They are named
// because the caller PRINTS them and a test ASSERTS them: an inline sentence at
// the print site would let the report and the test drift apart, and the whole
// point of this file is that the words an operator reads are the measurement.
const (
	// DeployDriftVerdictFollows — the artifact is the ref's tip.
	DeployDriftVerdictFollows = "follows the ref"
	// DeployDriftVerdictBehind — the artifact predates the ref and is an
	// ancestor of it: the ordinary "deployed N commits ago" case.
	DeployDriftVerdictBehind = "behind"
	// DeployDriftVerdictAhead — the artifact carries commits the ref does not.
	// Distinct from Behind on purpose: an artifact AHEAD of the ref is not
	// stale, it is from another line, and the remedy differs (it does not
	// follow the ref by rebasing).
	DeployDriftVerdictAhead = "ahead"
	// DeployDriftVerdictDiverged — both distances are non-zero: the artifact
	// and the ref share a merge base and then part.
	DeployDriftVerdictDiverged = "diverged"
)

// Verdict names which of the four answers this ref gave. It is deliberately a
// METHOD over the measured fields — the print site and the tests read the SAME
// decision, so no caller can re-derive "behind == 0 means fine" on its own
// (which is exactly the false-clean that shipped: see Ahead's doc).
//
// It answers "WHAT DOES THIS REF SAY ABOUT THE ARTIFACT", never "is the artifact
// stale": an AHEAD answer and a DIVERGED one are measurements too, and only
// CountsAsStale() decides which measurements an operator can act on. Keeping the
// two questions in two methods is what makes the printed line and the printed
// summary incapable of contradicting each other.
func (r DeployDriftRef) Verdict() string {
	switch {
	case r.Reason != "":
		return r.Reason
	case r.Follows:
		return DeployDriftVerdictFollows
	case r.Ahead == 0 && r.Behind == 0:
		// Neither distance is non-zero and the revision is not the ref's tip.
		// This is the shape of an artifact whose revision is unreadable as an
		// object in this repo, so the distances were measured but mean nothing
		// — stated, never reported as "follows".
		return deployDriftDistanceUnreadableReason
	case r.Ahead > 0 && r.Behind > 0:
		return DeployDriftVerdictDiverged
	case r.Ahead > 0:
		return DeployDriftVerdictAhead
	default:
		return DeployDriftVerdictBehind
	}
}

// CountsAsStale reports whether THIS ref is a reason to call the artifact stale
// — the ONE criterion, read by Stale() and by nothing re-derived at a print
// site.
//
// THE CRITERION IS "THE ARTIFACT IS BEHIND THIS REF", stated as the three facts
// that hold together:
//
//   - the measurement was TAKEN (Reason == "", so an absent or unreadable ref
//     is never evidence against the artifact — the two silences differ, Rule 15);
//   - the artifact does not follow it (Follows is RebuildAfterMerge's own
//     comparison);
//   - the ref CARRIES COMMITS THE ARTIFACT LACKS (Behind > 0).
//
// WHY `Behind > 0` AND NOT `!Follows` (this is the whole defect, decision A): a
// ref can answer AHEAD, which means the ARTIFACT carries commits the ref does
// not. That artifact has the ref's whole history and then some — it is not
// behind anything, so it is not stale, and the only action it could suggest is
// the one thing an operator must not do to it: rebuild DOWN to a ref it already
// contains. The old criterion was `!Follows`, which folded AHEAD in and made the
// summary contradict its own line ("this artifact is AHEAD of it, not stale
// behind it") as well as the DeployDriftVerdictAhead doc above. Both halves of
// the report now read THIS method, so they cannot disagree again.
//
// DIVERGED still votes: both distances are non-zero, so the ref does carry
// commits the artifact lacks and the artifact genuinely IS behind it — the
// `Behind > 0` half is what keeps that case (and every ordinary `behind N`
// case) reporting STALE, unchanged.
//
// A SCRATCH ref never votes, whatever its distance: see DeployDriftRefs' doc.
func (r DeployDriftRef) CountsAsStale() bool {
	if r.Scratch || r.Reason != "" || r.Follows {
		return false
	}
	return r.Behind > 0
}

// deployDriftRefIsScratch reports whether ref lives in the push-back lane's
// private SCRATCH namespace — a ref the lane writes and the lane prunes, and one
// no operator-facing build can ever move the artifact toward.
//
// It is a namespace PREFIX test against gitPushbackCheckRootRef (Rule 14: the
// namespace exists once), so a peer mirror inside that namespace
// (gitPushbackPeerRefMirrorFmt, …/<ns>/peer/<branch>) is covered by the same
// answer without a second literal.
func deployDriftRefIsScratch(ref string) bool {
	return strings.HasPrefix(strings.TrimSpace(ref), gitPushbackCheckRootRef)
}

// deployDriftOriginRemote is the configured remote name whose tracking ref is
// the checkout's own view of its upstream. It is declared here because this
// file measures it by NAME (a ref string), and a second spelling of "origin" in
// a report about which refs were read would let the printed list and the
// measured list disagree (Rules 1/13/14).
//
// WHY THIS REF IS MEASURED AT ALL (measured 2026-10-28, host nick-sager): the
// report used to measure only the local branch and the peer mirror. The
// deployment gap it exists to surface was 67 commits on refs/heads/master AND
// 27 on refs/remotes/origin/master — the second number was simply absent from
// the report, so an operator reading it saw one distance and had to run
// `rev-list --count` by hand for the other. The epic that added this row names
// origin/master explicitly: a report whose subject is "how far behind is the
// deployed artifact" must measure every ref a reader would compare it against.
const deployDriftOriginRemote = "origin"

// DeployDrift is one artifact's measured distance from each ref this ground can
// resolve. Every field is a measurement or a STATED reason it is missing —
// never a guess (Rule 15).
type DeployDrift struct {
	// Repo is the checkout the artifact was resolved under.
	Repo string
	// Artifact is the binary's path ("" = this checkout carries none, which is
	// itself the answer for a repo that was never built here).
	Artifact string
	// Revision is the revision the artifact ATTESTED, recovered from the report
	// it prints. "" means the artifact answered with no revision — a dirty build
	// or a shape this probe cannot read — and that is stated, never read as
	// "current".
	Revision string
	// Refs is one entry per ref measured, in DeployDriftRefs order; a ref this
	// ground cannot resolve contributes an entry with a stated Reason rather
	// than being dropped (an absent ref and an unreadable one are different
	// answers).
	Refs []DeployDriftRef
}

// DeployDriftRef is the artifact's distance from ONE ref.
type DeployDriftRef struct {
	// Ref is the ref as spelled in DeployDriftRefs.
	Ref string
	// Tip is that ref's current tip (abbreviated); "" when unreadable.
	Tip string
	// Behind is how many commits the ref has that the artifact's revision does
	// not. On its own it does NOT mean "the artifact follows the ref": a
	// DIVERGED artifact (one that shares a merge base but has commits the ref
	// never had) is also `Behind == 0`. Verdict is what says which case this is
	// — see the deployDrift verdict constants.
	Behind int
	// Ahead is how many commits the artifact's revision has that the ref does
	// not. It is the half that turns the false-clean into a stated answer
	// (measured 2026-10-28: the deployed 84beb30 was 0 commits behind the peer
	// mirror and 284 commits AHEAD of it, and the report said "0 behind").
	Ahead int
	// Follows is the verdict, and it is RebuildAfterMerge's OWN comparison —
	// never a second definition of "follows the tip" (Rules 8/13).
	Follows bool
	// Scratch marks a ref that lives in a lane-private SCRATCH namespace
	// (deployDriftRefIsScratch). Its distance is measured and printed, and it
	// does NOT vote on Stale(): nothing an operator does moves such a ref toward
	// the artifact, so it can never be the reason the artifact is called stale
	// (see DeployDriftRefs' doc for the measured case).
	Scratch bool
	// Reason states why this ref produced no answer; "" = it produced one.
	Reason string
}

// DeployDriftOf measures repo's deployed artifact against every ref in
// DeployDriftRefs. It performs no write of any kind and never returns an error:
// a measurement that could not be taken is a STATED field, because the caller
// (an operator reading the line) has no other way to tell an unmade measurement
// from a clean one (Rule 15).
func DeployDriftOf(repo string) DeployDrift {
	d := DeployDrift{Repo: strings.TrimSpace(repo)}
	if d.Repo == "" {
		d.Artifact = ""
		d.Revision = ""
		return d
	}
	return DeployDriftAt(d.Repo, artifactPath(d.Repo))
}

// DeployDriftAt measures the artifact at artifactPath against every ref repo
// can resolve. It is the seam DeployDriftOf and RunningDeployDrift share, so the
// distance arithmetic has ONE implementation whatever artifact is the subject
// (Rule 13/14: the thing being measured is a parameter, never a second copy of
// the loop).
//
// artifact may lie OUTSIDE repo — that is the case this seam exists for: the
// deployed /usr/local/bin/gopi is a symlink into /home/nick/work/gopi/bin, and a
// run started anywhere else has to be able to measure THAT file rather than the
// bin/ of whatever directory it happens to stand in.
func DeployDriftAt(repo, artifact string) DeployDrift {
	d := DeployDrift{Repo: strings.TrimSpace(repo), Artifact: strings.TrimSpace(artifact)}
	if d.Repo == "" || d.Artifact == "" {
		return d
	}
	_, rev, dirty := binaryVersionAt(d.Repo, d.Artifact)
	d.Revision = rev
	for _, ref := range DeployDriftRefs {
		entry := DeployDriftRef{Ref: ref, Scratch: deployDriftRefIsScratch(ref)}
		tip, err := repoSweepGit(d.Repo, "rev-parse", ref)
		if err != nil || strings.TrimSpace(tip) == "" {
			entry.Reason = deployDriftRefUnreadableReason
			d.Refs = append(d.Refs, entry)
			continue
		}
		entry.Tip = strings.TrimSpace(tip)
		// An artifact that attested NO revision cannot follow anything, and the
		// dirty case is the measured reason it often attests none (a rebuild
		// from a dirty tree yields a report with no revision: rebuild.go's
		// binaryVersion drops it deliberately). Stated, never a silent zero.
		switch {
		case dirty:
			entry.Reason = deployDriftArtifactDirtyReason
		case d.Revision == "":
			entry.Reason = deployDriftNoRevisionReason
		default:
			entry.Follows = versionMatches(d.Revision, entry.Tip)
			ahead, behind, derr := deployDriftCommitsApart(d.Repo, d.Revision, ref)
			if derr != nil {
				entry.Reason = deployDriftDistanceUnreadableReason
			} else {
				entry.Ahead, entry.Behind = ahead, behind
			}
		}
		d.Refs = append(d.Refs, entry)
	}
	return d
}

// DeployDriftRepoOf resolves the repository a deployed artifact was BUILT FROM,
// from the artifact's own path: a gopi artifact is written to
// <repo>/bin/gopi by the declared build (Rule 12), so <repo> is two levels up.
// ok=false means the path does not have that shape, and the caller must state
// its fallback rather than treat it as a repo.
//
// THE `<repo>` LEVEL MUST ACTUALLY HOLD A REPOSITORY. Layout alone is not
// enough and the difference is measured, not theoretical: `/usr/local/bin/gopi`
// has a `bin` level like any build output, so a shape-only rule answered
// `/usr/local` — a directory that holds no repository — and the report would
// then have measured refs nobody could resolve while naming a repo that does not
// exist (measured 2026-10-28, this function's own test caught it). So the parent
// is required to carry a `.git` ENTRY, in either shape the project recognizes:
// a DIRECTORY (an ordinary clone) or a FILE (a linked worktree's gitdir
// pointer). `git rev-parse` is deliberately NOT run here — a gopi artifact may
// legitimately be a copy placed outside any checkout, and the honest answer for
// that is "no repo resolved", which the caller states.
func DeployDriftRepoOf(artifact string) (repo string, ok bool) {
	artifact = strings.TrimSpace(artifact)
	if artifact == "" {
		return "", false
	}
	binDir := filepath.Dir(artifact)
	if filepath.Base(binDir) != rebuildBinDirName {
		return "", false
	}
	parent := filepath.Dir(binDir)
	if parent == "" || parent == string(filepath.Separator) {
		return "", false
	}
	if _, err := os.Lstat(filepath.Join(parent, devworkspace.GroundGitEntryName)); err != nil {
		return "", false
	}
	return parent, true
}

// Stale reports whether the artifact is genuinely BEHIND at least one ref this
// ground could measure — the single verdict the operator acts on.
//
// THE CRITERION IS NOT "does not follow": it is DeployDriftRef.CountsAsStale(),
// which requires the ref to CARRY COMMITS THE ARTIFACT LACKS. An artifact AHEAD
// of a ref has that ref's whole history and then some, so it is not stale
// behind it — and the pre-`!Follows` criterion contradicted this report's own
// per-ref line and the DeployDriftVerdictAhead doc (measured 2026-10-29: master
// and origin/master both `follows`, only the frozen scratch mirror `ahead`,
// and the report still printed `STALE:`). A scratch-namespace ref never votes at
// all — see DeployDriftRefs' doc.
func (d DeployDrift) Stale() bool {
	for _, r := range d.Refs {
		if r.CountsAsStale() {
			return true
		}
	}
	return false
}

// VotingRefs returns the entries that are allowed to decide the summary
// verdict — every measured ref EXCEPT the scratch ones. It exists so the print
// site can say WHICH refs the summary spoke about instead of leaving a reader to
// infer it from a list that mixes both kinds (Rule 15: an unstated exclusion
// reads as an oversight, and this report is the one place an operator looks to
// decide whether to rebuild).
func (d DeployDrift) VotingRefs() []DeployDriftRef {
	out := make([]DeployDriftRef, 0, len(d.Refs))
	for _, r := range d.Refs {
		if !r.Scratch {
			out = append(out, r)
		}
	}
	return out
}

// RunningDeployDrift measures the artifact THIS PROCESS IS RUNNING against the
// refs of the repo it was started from — the question an operator actually asks
// ("is the gopi on this machine old?"), answered about the file that is
// answering it.
//
// WHY THIS IS NOT DeployDriftOf(cwd): measured 2026-10-28 on host nick-sager.
// /usr/local/bin/gopi is a SYMLINK to /home/nick/work/gopi/bin/gopi. Started
// from a task ground, `gopi version --drift` reported
// `drift-artifact: /home/nick/workspace/<task-id>/bin/gopi` and
// `refs/heads/master: follows the ref — the artifact was built from it` — a
// TRUE statement about that ground's own fresh build and a completely
// misleading answer about the deployed binary, which was 67 commits behind. A
// report whose subject is the DEPLOYMENT must measure the deployed artifact;
// the cwd shape stays available as DeployDriftOf for "how far is the build in
// THIS checkout".
//
// self is the path of the running executable (cmd/agent passes
// os.Executable()). It is a parameter rather than an os.Executable() call here
// because this package must stay testable against a fixture artifact — and
// because a probe that silently substituted a different file would be the exact
// defect this function exists to remove.
func RunningDeployDrift(repo, self string) DeployDrift {
	d := DeployDrift{Repo: strings.TrimSpace(repo), Artifact: strings.TrimSpace(self)}
	if d.Repo == "" || d.Artifact == "" {
		return d
	}
	return DeployDriftAt(d.Repo, d.Artifact)
}

// deployDriftCommitsApart reads BOTH distances between the ref and the
// artifact's revision, through the SAME git runner the rest of this package
// uses (one timeout, one environment): `ahead` is what the artifact has that
// the ref does not, `behind` is what the ref has that the artifact does not.
//
// BOTH, because one of them is not enough to decide the verdict, and reporting
// only `behind` is the measured false-clean this function replaces. Measured
// 2026-10-28 on the deployed artifact (revision 84beb30):
//
//	git rev-list --count 84beb30..refs/remotes/gitPushcheck/master  → 0    ("0 behind")
//	git rev-list --count refs/remotes/gitPushcheck/master..84beb30  → 284  (the artifact is AHEAD)
//	git merge-base --is-ancestor 84beb30 refs/remotes/gitPushcheck/master → rc=1 (NOT an ancestor)
//
// The artifact had been built from a DIFFERENT line than the mirror (the two
// share a merge base and then part), so `0 behind` was TRUE and completely
// misleading: a reader saw a clean verdict for an artifact 284 commits off the
// ref. `ahead` is the number that makes that case state itself (Rules 15, 41).
//
// An unreadable distance is an error the caller STATES rather than a convenient
// zero — a zero would read as "the artifact follows this ref", which is the
// exact false-clean this file exists to prevent.
func deployDriftCommitsApart(repo, rev, ref string) (ahead, behind int, err error) {
	if ahead, err = deployDriftRevCount(repo, ref, rev); err != nil {
		return 0, 0, err
	}
	if behind, err = deployDriftRevCount(repo, rev, ref); err != nil {
		return 0, 0, err
	}
	return ahead, behind, nil
}

// deployDriftRevCount is `git rev-list --count <from>..<to>` parsed to an int.
// It refuses an answer that is not a plain non-negative integer rather than
// returning a zero the caller would print as a measurement.
func deployDriftRevCount(repo, from, to string) (int, error) {
	out, err := repoSweepGit(repo, "rev-list", "--count", from+baseClaimRangeSep+to)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(out)
	if s == "" {
		return 0, errDeployDriftEmptyCount
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errDeployDriftNotANumber
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

// errDeployDriftEmptyCount / errDeployDriftNotANumber are the two shapes of an
// answer this parser refuses. Both are stated (the caller turns them into
// deployDriftDistanceUnreadableReason) instead of being read as zero.
var (
	errDeployDriftEmptyCount = errors.New("git rev-list --count answered nothing")
	errDeployDriftNotANumber = errors.New("git rev-list --count answered something that is not a count")
)

// artifactPath is where a GoPi checkout's build contract writes (Rule 12's
// declared build), spelled from the SAME constants the rebuild verifier uses so
// the two faces cannot disagree about which file is "the deployed artifact".
func artifactPath(repo string) string {
	return filepath.Join(repo, rebuildBinDirName, rebuildBinName)
}

// deployDriftRefUnreadableReason / ...ArtifactDirtyReason / ...NoRevisionReason /
// ...DistanceUnreadableReason are the stated reasons a measurement is absent.
// Named, because a bare sentence at the site is the shape Rule 1 forbids and
// because the operator's next action depends on WHICH of these it was (an
// unreadable ref is a fetch; a dirty artifact is a clean-tree rebuild; no
// revision is a build recipe; an unreadable distance is neither).
const (
	deployDriftRefUnreadableReason      = "this ground cannot resolve the ref, so no distance was measured"
	deployDriftArtifactDirtyReason      = "the artifact was built from a dirty tree, so it attests no revision to compare"
	deployDriftNoRevisionReason         = "the artifact attests no revision, so there is nothing to compare with the ref"
	deployDriftDistanceUnreadableReason = "the commit distance behind the ref could not be read"
)
