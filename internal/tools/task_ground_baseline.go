package tools

// task_ground_baseline.go — THE GROUND'S BASE COMMIT, AND WHAT HAPPENS WHEN THE
// PEER CANNOT BE REACHED (epic worktree-base-align, 2026-09-26).
//
// THE PROBLEM, measured rather than argued. A task's ground used to be cut from
// whatever commit the repo's checkout happened to be on — in practice its LOCAL
// integration branch, which on a client host lags the peer's by however long it
// has been since the last sync pass (the client arm pulls with --ff-only, and
// that pull is REFUSED exactly when the two masters have diverged, so the lag
// grows precisely while the host is busy). Every run therefore started N commits
// behind the peer and paid for that lag twice: once when its own merge met a
// peer change it never saw, and once again when the close-out's pre-merge gate
// had to reconcile the two. Cutting the ground from the PEER'S OWN TIP removes
// the lag before the run has written a single line — the cheapest possible place
// to fix it.
//
// THE THREE ANSWERS THIS FILE CAN GIVE, and why there are three rather than two:
//
//	① the peer's tip      — the base the ground is cut from (the goal);
//	② no peer, no fetch   — a host that declares no peer (single-node, or the
//	                        server role) or an undeclared/invalid peer alias:
//	                        nothing to align to, the local baseline is the
//	                        only baseline there is, and NO NOTE is printed
//	                        (there is no one to be out of step with — a note
//	                        here would be noise an operator learns to ignore);
//	③ the peer is declared but UNREACHABLE — the interesting one. The run is
//	   NOT refused: a task that cannot start because a peer is down would make
//	   the peer a single point of failure for local work. The ground is cut
//	   from the local baseline, the run proceeds, and the degradation is stated
//	   IN THE RESOLUTION ITSELF (Rule 15) — never silently, because a stale
//	   baseline is invisible until it becomes a merge conflict, which is the
//	   exact failure this epic exists to remove.
//
// Rule 35 (no new knob): there is NO env var, no settings key and no CLI verb
// here. The decision is taken from what the host ALREADY declares — its node
// role (config.ResolveNodeRole, via IsServerNode) and its peer list
// (config.ResolveSyncPeers, read through the ONE target renderer
// finishCleanupServerTargetForRepo). On a host that declares neither, this file
// does nothing at all.
//
// THE OPERATOR-FACING STATEMENT OF THIS PROPERTY — what a fresh ground's base
// is, why it is the PEER's tip and not the local integration branch, and what
// `git diff --stat <local master>...HEAD` therefore means in a fresh ground —
// is devdoc/base-align-head-start.md. It exists because a closing run read that
// diff, saw foreign files, and could not tell a head start from a defect; the
// doc is cited HERE so the two never drift apart (server-side work, 2026-10-12).

import (
	"context"
	"fmt"
	"strings"
)

// The degradation note (Rule 1b/13/14: ONE spelling, referenced by the producer
// and by the test that asserts it — a test that re-types the sentence would
// drift from the text an operator actually reads).
//
// It says three things on purpose: what happened, what the consequence is, and
// that the run is still going. A note that only said "fetch failed" would leave
// the reader to work out whether their run is now mis-based; a note that only
// said "baseline may be stale" would hide that the peer was the reason.
const (
	taskGroundBaselineStaleNoteFmt = "baseline may be stale / NOT aligned with the peer %s — the peer fetch failed (%s); the worktree was still created from the LOCAL %s. Read the peer's own tip before trusting a conflict here as a real disagreement"
	// taskGroundBaselineAlignedFmt is the ALIGNED counterpart: the same channel
	// (the resolution's Detail) states the base positively when the fetch
	// worked, so an operator can tell "aligned" from "degraded" without
	// inferring one from the absence of the other.
	taskGroundBaselineAlignedFmt = "baseline cut from the peer %s tip %s (not from the local %s)"
	// taskGroundBaselineErrorSeparator / …Tail keep the note ONE line: git's
	// stderr is multi-line and this text lands in a log line.
	taskGroundBaselineSeparator = "; "
)

// taskGroundPeerBaseline decides the commit a NEW ground starts at, and the
// note that goes with it.
//
// It returns (ref, note): ref is what taskGroundCreateFrom is given as its base
// — the peer's integration-branch tip sha (never the local branch's name), or ""
// for the local HEAD baseline. note is the operator-facing sentence, "" when
// there is nothing to say.
//
// The SERVER ROLE never fetches (Rule 28's asymmetry, and the peer of a server
// is its own client — a ground cut from a client's master would be a category
// error). IsServerNode is the ONE predicate for that question in this package;
// it is consulted rather than re-derived from the role string, so the ground
// cannot disagree with the close-out chain about which arm this host is on.
func taskGroundPeerBaseline(repo string) (string, string) {
	local := IntegrationBranchName
	if IsServerNode() {
		// The server IS the integration point: its own master is the freshest
		// baseline in the topology, and fetching "itself" is the one thing the
		// constraints forbid outright. Nothing to align to, nothing to report.
		return "", ""
	}
	peer := finishCleanupServerTargetForRepo(repo).Rendered
	if peer == "" {
		// No declared peer, or a declared one whose alias git/ssh may not be
		// handed (the option-injection guard): the local baseline is the only
		// baseline this host has, and no note is owed — there is no peer this
		// run could be out of step WITH. The single-node case looks exactly
		// like this, which is why it must stay silent.
		return "", ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitPeerFetchTimeout)
	defer cancel()
	sha, err := SyncPeerFetch(ctx, repo, peer)
	if err != nil {
		return "", fmt.Sprintf(taskGroundBaselineStaleNoteFmt, peer,
			mergeBuildGateTail(strings.ReplaceAll(err.Error(), "\n", taskGroundBaselineSeparator)), local)
	}
	return sha, fmt.Sprintf(taskGroundBaselineAlignedFmt, peer, shortSha(sha), local)
}
