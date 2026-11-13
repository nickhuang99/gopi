package tools

// integration_claim_target.go — WHICH ref answers "is <commit> on the
// integration branch?" (task 01a12d30, filed by row 01a12d1b).
//
// THE MEASURED DEFECT. `master_claim.go` owns the one criterion this repo uses
// to decide "X is already on master" — `git merge-base --is-ancestor <c>
// <integration branch>` — and it asked that question of the LOCAL branch ref,
// spelled as the bare name `master` (IntegrationBranchName). On this host that
// ref is NOT the integration branch's current state: the delivery
// gate and the ground baseline both read the peer's master through a FORCED
// MIRROR fetch (`SyncPeerFetch`, git_peer_fetch.go —
// `+refs/heads/master:refs/remotes/gitPushcheck/master`), which is the ref the
// delivery lane itself decides on (`gitPushbackIsMerged`). Nothing on any
// automatic path advances the local checkout, and `git rev-parse master` here
// is 62 commits behind that mirror (measured 2026-10-12; evidence in this row's
// session directory).
//
// So the SAME commit got two verdicts from the same repo:
//
//	$ git merge-base --is-ancestor f82777a9f refs/heads/master                 → exit 1
//	$ git merge-base --is-ancestor f82777a9f refs/remotes/gitPushcheck/master  → exit 0
//
// The exit-1 reading ("no ref contains it") became a bug row's sole premise and
// cost a whole run. The ref that is stale is the one nobody refreshes; the ref
// the machinery decides on is the one the delivery lane writes. A verdict that
// reads the stale one is a FALSE NEGATIVE of "already landed" — the direction
// that makes a run re-implement landed work (devdoc/commit-path-ancestry-gate.md
// records the incident class; devdoc/premerge-merged-state-test-run.md:156
// records the DECISION to keep the local/tracking refs unreachable-by-mechanism,
// which is why this file refreshes nothing).
//
// WHAT THIS FILE DOES, and what it deliberately does NOT.
//
// It resolves a TARGET REF, never a fetch. The question "which of the two refs
// is the better answer?" is decided by an object-graph measurement taken inside
// THIS repository, between the two refs themselves:
//
//	mirror wins  ⟺  the mirror ref resolves to a commit
//	                AND the local integration branch is an ANCESTOR-or-equal of it
//	                AND the mirror carries at least one commit the local branch does not.
//
// That is "the mirror knows everything this checkout knows, and more", which is
// exactly the property that makes it the fresher answer to an ancestry question
// — and it needs no peer name, no network and no configuration. A repo whose
// mirror is absent, equal, behind, or DIVERGED (the peer's master was rewound
// and this checkout has commits the mirror lacks) falls back to the local
// integration branch, i.e. to the pre-fix answer byte for byte. Both counts are
// object-store reads of refs that already exist: no fetch, no timer, no hook,
// no new ref, no state file (Rule 35 — the fix adds a read and a better
// question, never a mechanism).
//
// THE DECLINED ALTERNATIVE, recorded so it is not re-attempted blind: making
// the local ref fresh (a pull on read, a timer, a hook). devdoc/premerge-
// merged-state-test-run.md:156 declines it in writing — the mirror already IS
// the fresh answer and a second channel for one question is Rule 14's defect —
// and the cost is recurring (a network round-trip on every read, Rule 35). This
// file is the third option: leave every ref where it is and make the READER
// name the right one.
//
// THE GAP IT LEAVES, honestly: a reader who asks git directly (`git branch
// --contains`, `git merge-base --is-ancestor <c> master` by hand) still gets the
// stale answer, because no mechanism advances that ref — for that reader the
// discipline is `gopi task archive --verify-commit <rev>` (cli_archive.go, which
// prints the ref it measured against) or naming the mirror explicitly.
//
// A SECOND DECLINED DESIGN, for the same reason: activating on the peers'
// tracking refs (`refs/remotes/<peer>/master`) instead of on the mirror itself.
// Measured on this host 2026-10-12: that ref is EQUAL to the local branch
// (`1508e3821`), so the activator would not fire in the very state the defect
// was found in — the client pass that writes it had not run for hours. A guard
// that only works when a second, unrelated mechanism has already run is not a
// guard.

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// integrationClaimLocalRef is the fallback target: the local integration
	// branch, composed from the ONE integration-branch constant (Rule 1/13 —
	// never a second "master" literal).
	integrationClaimLocalRef = refsHeadsPrefix + IntegrationBranchName

	// integrationClaimMinLookahead is the smallest lead that makes the mirror
	// the better answer: ONE commit ahead is already a state in which the two
	// refs can disagree, which is the whole failure. Named here rather than
	// typed (Rule 1).
	integrationClaimMinLookahead = 1

	// integrationClaimRelativeRefspecSeparator is git's range spelling
	// (`<a>..<b>`), used by both counts.
	integrationClaimRelativeRefspecSeparator = ".."

	// integrationClaimRefFormatFlag is `--format=` as this file passes it, with
	// integrationClaimRefFormat — the ONE for-each-ref format this file reads
	// (the masterClaimRefFormat sibling).
	integrationClaimRefFormatFlag = "--format="
	integrationClaimRefFormat     = masterClaimRefFormat

	// The git spellings the measurements are built from, named here rather than
	// typed at their call sites (Rule 1): resolve the mirror, count one ref
	// against the other.
	integrationClaimRevParseVerb  = finishCleanupPreMergeRevParseVerb
	integrationClaimRefVerifyFlag = finishCleanupPreMergeVerifyFlag
	integrationClaimRefQuietFlag  = finishCleanupPreMergeQuietFlag
	integrationClaimPeelSuffix    = masterClaimPeelSuffix
	integrationClaimRevListVerb   = "rev-list"
	integrationClaimCountFlag     = "--count"
)

// IntegrationClaimTargetRef returns the ref "is <commit> on the integration
// branch?" must be asked of, in repo — the delivery lane's mirror when it is
// strictly ahead of the local integration branch, else the local branch (the
// pre-fix answer).
func IntegrationClaimTargetRef(repo string) string {
	ref, _ := integrationClaimTargetRefAndLookahead(repo)
	return ref
}

// integrationClaimTargetRefAndLookahead is the ONE resolution: the target ref
// and, when the mirror won, the lead that made it win (0 otherwise). One
// function so the two facts are measured together and cannot disagree — a
// caller that must SAY why the mirror was believed cannot re-run the count and
// get a different answer (Rules 8/13: measure once, read twice).
//
// It never fails: an unreadable repo, an absent or diverged mirror, or a git
// that answered nothing at all all fall back to the local ref — the
// conservative direction, and the one that keeps a caller's verdict identical
// to the shipped one (Rule 15: a measurement that could not be taken is not
// evidence for a change).
func integrationClaimTargetRefAndLookahead(repo string) (string, int) {
	ref, lead, _ := integrationClaimTargetMeasured(repo)
	return ref, lead
}

// integrationClaimTargetMeasured is the SAME resolution, one layer richer: it
// also answers WHY the mirror did not win, so a caller that must TELL a human
// which ref answered (the close loop's rebuild measurement,
// rebuild_integration_ref.go) reads the reason from the measurement itself
// rather than re-deriving it — a second probe could disagree with the first
// (Rule 8/13: measure once, read twice).
//
// note is "" when the mirror won. Every other return carries a non-empty,
// human-readable reason, and the three spellings live here as constants (Rule
// 1b) so the rebuild's log line and this resolver cannot drift.
func integrationClaimTargetMeasured(repo string) (ref string, lead int, note string) {
	if strings.TrimSpace(repo) == "" {
		return integrationClaimLocalRef, 0, integrationClaimNoteUnreadable + ": empty repo path"
	}
	if _, err := repoSweepGit(repo, integrationClaimRevParseVerb, integrationClaimRefVerifyFlag,
		integrationClaimRefQuietFlag, gitPushbackCheckRef+integrationClaimPeelSuffix); err != nil {
		return integrationClaimLocalRef, 0, integrationClaimNoteNoMirror // no mirror: nothing to prefer
	}
	lead, err := integrationClaimCountBetween(repo, integrationClaimLocalRef, gitPushbackCheckRef)
	if err != nil || lead < integrationClaimMinLookahead {
		return integrationClaimLocalRef, 0, integrationClaimNoteNoMirror
	}
	behind, err := integrationClaimCountBetween(repo, gitPushbackCheckRef, integrationClaimLocalRef)
	if err != nil || behind > 0 {
		// The two refs DIVERGED (the peer's master was rewound and this checkout
		// has work the mirror lacks): neither is a superset of the other, so
		// neither is the better answer. Abstain to the shipped behaviour.
		return integrationClaimLocalRef, 0, integrationClaimNoteMirrorDiverged
	}
	return gitPushbackCheckRef, lead, ""
}

const (
	// integrationClaimNoteNoMirror is the reason recorded when the mirror did NOT
	// offer a fresher tip: absent from this checkout, unreadable, or resolving to
	// something that is not strictly ahead of the local integration branch. All
	// three are the same DECISION (the local ref answers), so they share one
	// spelling rather than three reasons nothing acts on.
	integrationClaimNoteNoMirror = gitPushbackCheckRef + " did not offer a fresher tip than " +
		integrationClaimLocalRef
	// integrationClaimNoteMirrorDiverged is the one case worth separating: the
	// mirror is present and ahead, but neither ref is a superset of the other,
	// so preferring it would be a guess.
	integrationClaimNoteMirrorDiverged = gitPushbackCheckRef + " diverged from " +
		integrationClaimLocalRef + " (neither is a superset of the other)"
	// integrationClaimNoteUnreadable is the empty-input arm's reason.
	integrationClaimNoteUnreadable = "the repo could not be measured"
)

// integrationClaimCountBetween returns how many commits reachable from to are
// not reachable from from — `git rev-list --count <from>..<to>` (the count this
// epic's own diagnosis uses; the same verb scripts/archive-tag-census.sh and
// devdoc/worktree-base-align.md read).
func integrationClaimCountBetween(repo, from, to string) (int, error) {
	out, err := repoSweepGit(repo, integrationClaimRevListVerb, integrationClaimCountFlag,
		from+integrationClaimRelativeRefspecSeparator+to)
	if err != nil {
		return 0, fmt.Errorf("rev-list --count %s..%s: %w", from, to, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("rev-list --count %s..%s returned %q: %w", from, to, strings.TrimSpace(out), err)
	}
	return n, nil
}
