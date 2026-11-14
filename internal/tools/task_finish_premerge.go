package tools

// Client close-out: THE BRANCH'S OWN CHECKS BEFORE THE PUSH — the guard that
// survives the removal of the trial-merge ("pre-merge") gate.
//
// WHAT WAS REMOVED, AND WHY (measured, 2026-10-12). The close-out chain used to
// FETCH the peer's master, MERGE it into the branch locally (the "trial merge"),
// rebuild the merged tree, run a merged-state test run, run a published-anchor
// gate, and push only if every one of those passed. Three finished branches were
// refused by it in ONE batch: 01a1301b was stopped by an anchor gate whose
// COMMANDS.md anchor was not even inside its diff, and 01a13019 / 01a1300b ran
// 834-838 s and were still refused with no FAIL line anywhere in their output.
// The same three branches were pushed by hand in about a minute each and merged
// cleanly. The client was paying minutes of CPU and taking a SECOND, gratuitous
// decision about a state that belongs to the SERVER: merging a branch into
// master, and judging the composed tree, is the merger's own act — performed on
// the integration branch, with the real master in front of it, once.
//
// WHAT SURVIVES, and it is the whole of this file:
//
//	① the PRECONDITIONS — a branch to deliver, and a directory checked out ON it
//	   (never the main checkout, Rule 29; never a merge ground, because nothing
//	   merges). No branch = a recorded no-op. No ground, a dirty ground, or a
//	   ground on another branch = a recorded ABSTENTION.
//	② the BRANCH'S OWN TEST RUN, in its own worktree, at its own revision.
//	   Packages come from the branch's own diff against the integration branch
//	   (`smoke.TouchedPackages(dir, <integration ref>, HEAD)`), and the command
//	   is the declared SCOPE target (`smokegate.ScopedForPackages` — `make
//	   test-budget PKGS='…'`, never the whole-tree run, Rule 27). This is the
//	   check a build cannot make: `go build` compiles no `_test.go`, so a red
//	   the branch itself introduces is invisible to it.
//	③ the PUSH, in the caller. Nothing in this file touches the remote.
//
// NO MERGE RUNS HERE. No `git merge`, no `git fetch`, no merged-state checkout,
// no rebuild of a composed tree, no anchor gate. The integration branch's
// objects are read (to compute the diff the selection is taken against) and
// never merged into anything. A branch that is BEHIND the integration branch,
// and a branch that would CONFLICT with it, are both still delivered: the skew
// and the conflict are the merger's, with the author's commits on hand.
//
// A REFUSAL here is one fact and only one: the branch's OWN tests are red at the
// revision being pushed — which is a fact about this run's own deliverable, the
// kind of finding its author can act on. Everything that cannot be measured
// ABSTAINS and is REPORTED in the step's detail (Rule 15): the switch off, no
// declared scope target, a diff base that does not resolve, no touched Go
// package, a dirty ground that makes a run unrepresentative of the pushed
// revision, a budget trip. An abstention never blocks a delivery.
//
// THE `finishCleanupPreMerge*` PREFIX ON THE SPELLING CONSTANTS BELOW is a
// NAMING VESTIGE, kept deliberately: those few git spellings ("rev-parse",
// "--verify", "--quiet", "HEAD", "--abbrev-ref", "merge-base", "--is-ancestor")
// have their ONE definition here and are referenced from five sibling files
// (Rule 14 — a shared value with exactly one definition, never mirrored).
// Moving them would churn five files and buy nothing; renaming them would be a
// second name for the same string. What is NOT vestigial is the vocabulary that
// DESCRIBES a step: the step name, the markers and the events below say what
// this lane actually does now.
import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"strings"

	"github.com/nickhuang99/gopi/internal/boolvocab"
	"github.com/nickhuang99/gopi/internal/smoke"
	"github.com/nickhuang99/gopi/internal/smokegate"
)

// --- tunables (Rule 1: every literal the lane uses lives here) ---

const (
	// finishCleanupBranchCheckEnv is the kill switch (default ON, the
	// finishCleanupEnv convention: "0"/"off"/"no"/"false"/"disabled" turns the
	// branch's test run off). Read in exactly one place —
	// finishCleanupBranchCheckOn.
	finishCleanupBranchCheckEnv = "GOPI_FINISH_CLEANUP_BRANCH_CHECKS"

	// finishCleanupBranchCheckStep is the lane's report step name: an operator
	// (or a test) matches the step on it, so it is spelled once. It replaced
	// the removed trial-merge lane's `pre-merge` step name deliberately — a
	// report must not keep a name whose act no longer happens.
	finishCleanupBranchCheckStep = "branch-checks"

	// finishCleanupBranchCheckDefaultTimeoutSec bounds the branch's test run
	// when the repo's entry declares no timeout for it. It IS the merge-time
	// build gate's bound (mergeBuildGateDefaultTimeoutSec), which in turn
	// mirrors the config package's own default: one value, several readers, so
	// no lane can disagree about how long a bounded run may take (Rule 14).
	finishCleanupBranchCheckDefaultTimeoutSec = mergeBuildGateDefaultTimeoutSec

	// finishCleanupBranchCheckEventKeep bounds this lane's own audit trail for
	// one row: the last N events survive a reiterated refusal, so a delivery
	// that keeps failing cannot grow the ledger without end.
	finishCleanupBranchCheckEventKeep = 3

	// mergeCheckBuildName is how a check list names the repo's declared build
	// in an abstention sentence (Rule 1b: the name is compared nowhere and
	// printed in one place).
	mergeCheckBuildName = "build"

	// mergeCheckAbstainSeparator joins several abstention reasons into one
	// reported sentence.
	mergeCheckAbstainSeparator = "; "

	// mergeCheckAbstainPrefix introduces the clauses naming the checks that
	// could NOT be measured (Rule 15: an unmeasured check is a reported
	// abstention, never a silent pass). Spelled once, used by both the verified
	// and the refused sentence.
	mergeCheckAbstainPrefix = " — not measured: "
)

// --- bounds of the runner's capture (Rule 1) ---

const (
	// mergeBuildGateErrorHeadLines / mergeBuildGateErrorTailLines split the
	// quoted budget between the two ends of a failed command's output, and
	// mergeBuildGateErrorElision marks where lines were dropped between them, so
	// a reader never takes the concatenation for contiguous output.
	mergeBuildGateErrorHeadLines = mergeBuildGateErrorLines / 2
	mergeBuildGateErrorTailLines = mergeBuildGateErrorLines - mergeBuildGateErrorHeadLines
	mergeBuildGateErrorElision   = "…"

	// mergeCheckTestResultPrefix is what the testing package prints in front of
	// a test's outcome (`--- FAIL: TestX`): the marker that makes a line part of
	// the failing package's own block rather than a neighbour's `ok` line.
	mergeCheckTestResultPrefix = "--- "

	// mergeCheckTestFailVerb is the outcome word the block opens with
	// (`--- FAIL:`), compared whole so `--- FAILFLAKY:` cannot select it.
	mergeCheckTestFailVerb = "FAIL:"

	// mergeCheckBlockIndentChars are the characters a quoted block's lines may
	// be indented with. A block is read out of a REPORT as well as out of raw
	// `go test` output, and the report indents both its marker line
	// (`failure_marker_indent` in scripts/test-budget.sh) and the test's own body
	// under it — so the marker is recognised by its TRIMMED prefix, and the
	// block's end is the first line back at (or left of) the marker's own
	// indentation. Measured 2026-10-31 (task 01a18f6e): the detector required the
	// marker at column 0, matched nothing in the report, and the lane fell back
	// to a both-ends quote whose windows held the run's header and an unrelated
	// `[setup failed]` package — the failing test's name was never printed.
	mergeCheckBlockIndentChars = " \t"

	// mergeCheckSummaryFieldCount is how many tab-separated fields the
	// toolchain's per-package FAIL summary carries (`FAIL`, the package, the
	// time) — what distinguishes it from the bare `FAIL` marker above it.
	mergeCheckSummaryFieldCount = 3

	// mergeCheckTestVerb is the toolchain verb whose output is shaped as one
	// block per package (`go test`), compared as a WHOLE field so a module or
	// path that merely contains the word ("gotestsum", "./testdata") cannot
	// select the test arm.
	mergeCheckTestVerb = "test"

	// mergeCheckFailureSummaryPrefix is the toolchain's own summary marker for a
	// failed test binary (`FAIL\t<package>\t<time>`, and the bare `FAIL` line
	// above it).
	mergeCheckFailureSummaryPrefix = "FAIL"

	// mergeCheckOutputCapMax bounds the bytes the runner RETAINS for one check.
	// The retention is what a tail extraction reads, so a command that prints
	// without end must still leave the lane the END of its output, however long
	// the middle was (the captured amount is capped, the command itself is not:
	// a check runs to completion or to its own timeout).
	mergeCheckOutputCapMax = 2 << 20
)

// --- git spellings (Rule 1b: compared/dispatched values live once) ---

const (
	finishCleanupPreMergeRevParseVerb   = "rev-parse"
	finishCleanupPreMergeVerifyFlag     = "--verify"
	finishCleanupPreMergeQuietFlag      = "--quiet"
	finishCleanupPreMergeHeadArg        = "HEAD"
	finishCleanupPreMergeAbbrevRefFlag  = "--abbrev-ref"
	finishCleanupPreMergeMergeBaseVerb  = mergeBuildGateMergeBaseVerb
	finishCleanupPreMergeAncestorFlag   = mergeBuildGateAncestorFlag
	finishCleanupPreMergeWorktreeVerb   = finishCleanupWorktreeVerb
	finishCleanupPreMergeWorktreeAdd    = finishCleanupWorktreeAddVerb
	finishCleanupPreMergeWorktreeRmVerb = finishCleanupWorktreeRemoveVerb
	finishCleanupPreMergeDetachFlag     = finishCleanupWorktreeDetachFlag
)

// --- verdicts (Rule 1b: each is matched by name, so each is spelled once) ---

const (
	finishCleanupBranchCheckDisabledMarker = "the branch's own test run is disabled (" + finishCleanupBranchCheckEnv + ")"
	finishCleanupBranchCheckNoBranchMarker = "no branch resolved — nothing to check before pushing"
	finishCleanupBranchCheckNoBaseMarker   = "no integration branch resolves here, so the packages this branch touches cannot be selected — its own test run was NOT made"
	finishCleanupBranchCheckDirtyMark      = "the ground holds uncommitted work, so a test run in it is not evidence about the pushed revision (rev "
	finishCleanupBranchCheckOffBranchMark  = "no directory is checked out on the delivered branch (the main checkout is never a test ground) — the branch's own test run was NOT made"
	finishCleanupBranchCheckPassMarker     = "the branch's own test run passed at the revision being pushed (packages from its diff against the integration branch)"
	finishCleanupBranchCheckFailMarker     = "the branch's own test run FAILED at the revision being pushed — the branch was NOT pushed"

	// finishCleanupDeliveryBlockedMarker is appended to every refusal that
	// WITHHOLDS the worktree/branch removal for a reason that is not the
	// delivery lane's own verdict (ground ownership, live-run identity,
	// incompleteness, the deterministic checklist). It says what the reader has
	// to act on: the delivery stands, the directory is STILL THERE.
	finishCleanupDeliveryBlockedMarker = " — the worktree was NOT released and the branch keeps its check-out; this refusal is recorded on the row's trail and must be resolved before the ground can be collected"

	// finishCleanupRemovalAuditStanding is the standing a finish-cleanup
	// removal derives from, as the removal-audit record states it: what the
	// chain established about the delivered branch before it released the
	// ground. It names the branch's OWN checks and the deterministic
	// checklist — NOT a merge, because nothing on this path merges any more
	// (the trial merge is gone; see finishCleanupBranchCheckVerify).
	finishCleanupRemovalAuditStanding = "delivery chain: the branch was checked and classified on its own revision; "
)

// The lane's verdict codes — the three answers a caller acts on.
const (
	finishCleanupPreMergeStatusVerified = "verified"
	finishCleanupPreMergeStatusRefused  = "refused"
	finishCleanupPreMergeStatusSkipped  = "skipped"
)

// The audit-event names (an operator greps for them, a test matches them).
//
// THREE names for the three verdicts. The positive arm exists because its
// absence was once a defect (epic 01a12fa5): with no name for "it ran and
// passed", the trail could not tell a lane that PASSED from one that ABSTAINED,
// and a reader took the refusal name for a verdict.
const (
	finishCleanupPreMergeVerifiedEvent   = "branch-checks-verified"
	finishCleanupPreMergeRefusedEvent    = "branch-checks-refused"
	finishCleanupPreMergeUnverifiedEvent = "branch-checks-unverified"
)

// finishCleanupPreMergeResult is the lane's outcome for one delivery attempt.
//
// Deliberately not a bare error: the chain must tell "I proved this delivery
// safe" from "I could not judge" (ABSTAIN — the branch still goes, with the
// reason recorded) and from "I proved it unsafe" (REFUSE — no push). Collapsing
// the three loses the only information an operator needs.
type finishCleanupPreMergeResult struct {
	Status string
	Detail string
}

// OK reports whether the delivery may proceed.
func (r finishCleanupPreMergeResult) OK() bool {
	return r.Status == finishCleanupPreMergeStatusVerified || r.Status == finishCleanupPreMergeStatusSkipped
}

// finishCleanupBranchCheckOn reports whether the branch's own test run is
// enabled (default ON; the explicit OFF values disable — the finishCleanupEnv
// convention, read from the ONE vocabulary definition in internal/boolvocab).
// The ONE reader of finishCleanupBranchCheckEnv (Rule 2).
func finishCleanupBranchCheckOn() bool {
	return !boolvocab.IsFalse(os.Getenv(finishCleanupBranchCheckEnv))
}

// finishCleanupBranchCheckVerify runs the branch's own check before the push.
//
// repoDir is the git database (the main repo dir); ground is a directory the
// caller believes is checked out on `branch` — "" is allowed, and the branch's
// own linked worktree is then resolved here. branch is the DELIVERABLE's branch
// (never "whatever is checked out").
//
// It DOES NOT MUTATE the tree: no merge, no checkout, no reset. The only git
// calls it makes are reads (rev-parse, merge-base, diff, ls-tree) plus the
// bounded test run in the ground it was given.
//
// Outcomes:
//
//	verified — the branch's own test run passed at the revision being pushed,
//	           or nothing could be measured (ABSTAINED, and the detail says
//	           exactly what could not be measured).
//	refused  — the run FAILED. The branch is not pushed; it keeps its commits,
//	           its name and its ground, and the reason rides the row's trail.
//	skipped  — a precondition is absent (no branch, no ground, a dirty ground,
//	           the switch off). The delivery proceeds and the abstention is
//	           recorded.
func finishCleanupBranchCheckVerify(ctx context.Context, repoDir, ground, branch string) finishCleanupPreMergeResult {
	branch = strings.TrimSpace(branch)
	if branch == "" || finishCleanupBranchExempt(repoDir, branch) {
		return finishCleanupPreMergeResult{Status: finishCleanupPreMergeStatusSkipped, Detail: finishCleanupBranchCheckNoBranchMarker}
	}
	if !finishCleanupBranchCheckOn() {
		return finishCleanupPreMergeResult{Status: finishCleanupPreMergeStatusSkipped, Detail: finishCleanupBranchCheckDisabledMarker}
	}
	if strings.TrimSpace(ground) == "" || !finishCleanupBranchCheckGroundOnBranch(ctx, ground, branch) {
		// The caller named no ground, or named one sitting on some OTHER branch
		// (never entered, Rule 29). RESOLVE the branch's own linked worktree; a
		// branch with none gets the recorded abstention below, never a silent
		// skip.
		if resolved := finishCleanupBranchWorktree(ctx, repoDir, branch); resolved != "" {
			ground = resolved
		}
	}
	if ground == "" || !finishCleanupBranchCheckGroundOnBranch(ctx, ground, branch) {
		return finishCleanupBranchCheckOffBranchResult(ctx, repoDir, branch)
	}
	// THE DIFF BASE. The selection is taken against the integration branch —
	// the only ref whose contents define "what this branch changed". No
	// integration ref resolves ⇒ no selection ⇒ recorded abstention (the lane
	// never guesses a base, and never falls back to a whole-tree run).
	base := finishCleanupIntegrationRef(ctx, repoDir)
	if base == "" {
		return finishCleanupPreMergeResult{Status: finishCleanupPreMergeStatusSkipped, Detail: finishCleanupBranchCheckNoBaseMarker}
	}
	// A DIRTY GROUND MAKES THE RUN UNREPRESENTATIVE. What is being pushed is the
	// branch's COMMIT; a run in a worktree carrying uncommitted edits measures
	// those as well, so the verdict would be about a tree nobody is delivering.
	// The delivery already proceeds on a dirty ground (the removal's refusal,
	// never the delivery's), so this is an abstention, said out loud.
	if tip, ok := finishCleanupPreMergeRev(ctx, ground, finishCleanupPreMergeHeadArg); ok && finishCleanupCheckGroundHoldsWork(ctx, ground) {
		return finishCleanupPreMergeResult{Status: finishCleanupPreMergeStatusSkipped,
			Detail: finishCleanupBranchCheckDirtyMark + tip + ")"}
	}
	rev, ok := finishCleanupPreMergeRev(ctx, ground, finishCleanupPreMergeHeadArg)
	if !ok {
		return finishCleanupBranchCheckOffBranchResult(ctx, repoDir, branch)
	}
	return finishCleanupBranchCheckAt(ctx, ground, base, branch, rev)
}

// finishCleanupBranchCheckOffBranchResult is THE missing-ground verdict, built
// in ONE place (Rule 14): both arms that reach it — a ground the caller could
// not produce, and a ground whose HEAD does not resolve — return the SAME
// sentence, and the sentence now carries what the withheld run would have
// selected.
func finishCleanupBranchCheckOffBranchResult(ctx context.Context, repoDir, branch string) finishCleanupPreMergeResult {
	return finishCleanupPreMergeResult{Status: finishCleanupPreMergeStatusSkipped,
		Detail: finishCleanupBranchCheckOffBranchMark + finishCleanupBranchCheckWithheldNote(ctx, repoDir, branch)}
}

// finishCleanupBranchCheckAt is the measurement itself: build the check for the
// branch's own revision and run it in the ground.
//
// The run happens IN THE GROUND — the directory this run owns and is delivering
// from — because the question is about the branch's own revision and no other
// tree is needed to answer it (Rule 29: one ground per run; a detached copy of
// the tree would be a second copy of the same files).
func finishCleanupBranchCheckAt(ctx context.Context, dir, base, branch, rev string) finishCleanupPreMergeResult {
	check, why, ok := finishCleanupBranchCheckTest(dir, base, branch)
	if !ok {
		return finishCleanupPreMergeResult{Status: finishCleanupPreMergeStatusSkipped, Detail: why}
	}
	// The PUBLISHED-ANCHOR ARM joins the SAME check list (Rule 14: one list, one
	// runner, one ground). Its three outcomes are kept apart on purpose, because
	// their remedies differ (Rule 15): a BROKEN edge is a defect in the tree and
	// REFUSES; a runnable gate is one more check; an ABSTENTION is a measurement
	// this lane could not take and is REPORTED beside the verdict rather than
	// silently dropped.
	checks := []mergeCheck{check}
	// The STYLEGATE ARM joins the same list, and it is deliberately NOT part of
	// the selection above: the ratchet's ceiling is a WHOLE-TREE reading
	// (ignored-error counts every package) while this lane's selection is
	// branch-scoped, so a `testsuite/` or `cmd/agent/` commit that discards one
	// more error pushes the tree-wide reading over the ceiling without ever
	// selecting `internal/stylegate`. Measured on this repo 2026-10-30: three such
	// sites in one night from three unrelated rows, none found by its own author.
	// It is UNCONDITIONAL for the same reason — a diff may not decide whether a
	// whole-tree reading is taken — and a tree without the package abstains rather
	// than inventing a pattern (Rule 15).
	//
	// THE CENSUS ARM THAT USED TO SIT HERE IS GONE (Rule 36 / commit 78bdf05a).
	// It appended a check that ran the `config-anchor-census --check` leg, in the
	// package that hosted the `TestMergeConfigAnchorGate` arms. That generator and
	// its table were deleted with the documentation-citation family on
	// 2026-11-05, so the arm was addressing a test name that no longer exists: a
	// reader of this lane would have seen a check run and pass while measuring
	// nothing. The rule it served (a census figure names its generator and the
	// commit it was measured at) still stands as Rule 38 — what is retired is the
	// machinery that produced that figure for CONFIG.md's anchors.
	styleGateNote := ""
	if styleGate, ok := finishCleanupBranchStylegateCheck(dir); ok {
		// ONE MEASUREMENT PER DELIVERY (Rule 26 — the overlap is stated, not
		// run twice). The scoped smoke check now carries the must-run gate
		// roster (smoke.WithGatePackages), so on a normal tree `check.Cmd`
		// ALREADY addresses this package. The arm stays as the answer for the
		// one shape the roster cannot cover — a scoped check that was not built
		// at all (see the empty-Cmd arm below) — and it is skipped (not silently:
		// its absence is by construction the same reading) when the roster has
		// already put it in the command.
		if !mergeCheckCmdCarriesPackage(check.Cmd, smoke.PackagePattern(styleGatePackageDir)) {
			checks = append(checks, styleGate)
		}
	} else {
		styleGateNote = styleGateAbstention()
	}
	anchor, findings, anchorWhy, anchorOK := finishCleanupBranchAnchorCheck(dir, base, branch)
	switch {
	case len(findings) > 0:
		return finishCleanupPreMergeResult{Status: finishCleanupPreMergeStatusRefused,
			Detail: finishCleanupBranchCheckFailMarker + " (" + gendocUnionGateName + ", rev " + rev + "): " +
				anchorWhy + mergeCheckAbstainSeparator + strings.Join(findings, mergeCheckAbstainSeparator)}
	case anchorOK:
		checks = append(checks, anchor)
	}
	res := finishCleanupChecksIn(dir, checks)
	// The stylegate arm's abstention rides along with the anchor arm's: both are
	// measurements this lane could NOT take, and neither changes the verdict the
	// run earned (Rule 15 — an unmeasured arm is reported, never silently dropped
	// and never turned into a pass or a refusal of its own).
	note := anchorWhy
	if styleGateNote != "" {
		if note == "" {
			note = styleGateNote
		} else {
			note = note + mergeCheckAbstainSeparator + styleGateNote
		}
	}
	switch res.Status {
	case finishCleanupPreMergeStatusRefused:
		return finishCleanupPreMergeResult{Status: finishCleanupPreMergeStatusRefused,
			Detail: finishCleanupBranchCheckFailMarker + " (" + res.Check + ", rev " + rev + "): " + res.Detail}
	case finishCleanupPreMergeStatusVerified:
		if res.Detail != "" {
			// The runner reports an unmeasurable check as verified-WITH-A-REASON:
			// nothing was measured, and the reason is the finding (Rule 15).
			return finishCleanupPreMergeResult{Status: finishCleanupPreMergeStatusSkipped, Detail: res.Detail}
		}
		return finishCleanupPreMergeResult{Status: finishCleanupPreMergeStatusVerified,
			Detail: finishCleanupBranchCheckAnchorNote(finishCleanupBranchCheckPassMarker+" (rev "+rev+")", note)}
	}
	return finishCleanupPreMergeResult{Status: finishCleanupPreMergeStatusSkipped,
		Detail: finishCleanupBranchCheckAnchorNote(res.Detail, note)}
}

// mergeCheckCmdCarriesPackage reports whether a rendered check command already
// addresses the given go-test package pattern — the reading that keeps ONE
// measurement from being taken twice in one delivery.
//
// It is a SUBSTRING test on the rendered command because that is what the
// command IS: a single-quoted `PKGS='<patterns joined by spaces>'` argument
// (smokegate.ScopedForPackages), so a pattern appears verbatim or not at all.
// The package is spelled by the caller through the ONE composer
// (smoke.PackagePattern), never re-derived here.
func mergeCheckCmdCarriesPackage(cmd, pattern string) bool {
	return cmd != "" && pattern != "" && strings.Contains(cmd, pattern)
}

// finishCleanupBranchCheckAnchorNote appends the published-anchor arm's own
// abstention sentence to the lane's verdict sentence — WITHOUT changing the
// verdict. The two facts are different (did the branch's tests pass? was the
// anchor question answerable here?) and the status stays the one the run earned,
// so an operator reading the ledger sees both instead of one silence (Rule 15).
// An empty note leaves the sentence exactly as the sibling lane wrote it.
func finishCleanupBranchCheckAnchorNote(detail, note string) string {
	if note == "" {
		return detail
	}
	if detail == "" {
		return note
	}
	return detail + mergeCheckAbstainSeparator + note
}

// finishCleanupCheckGroundHoldsWork reports whether the ground carries worktree
// changes right now. It is the run's representativeness precondition: a probe
// that cannot answer reads as TRUE (the conservative side — an unprovable clean
// tree is never reported as clean).
//
// The reading goes through finishCleanupProbeClean, never a bare `!= ""`
// comparison, because a CLEAN probe does not come back empty: finishCleanupGit
// substitutes its own sentinel for "git printed nothing", so a clean ground
// arrives as the marker and a `!= ""` test calls it DIRT. That mistake is what
// made this lane abstain on every clean ground (measured 2026-10-12, task
// 01a130a1: TestBranchCheckRunQuotesTheFailingPackage and its sibling saw
// status="skipped" — "the ground holds uncommitted work" — over worktrees whose
// own `git status --porcelain` printed nothing). The sentinel's one definition
// stays in the constant; the one READING of it stays in finishCleanupProbeClean
// (Rules 13/14), so this predicate cannot drift from the dirty gate again.
func finishCleanupCheckGroundHoldsWork(ctx context.Context, ground string) bool {
	// PARSED porcelain: stdout only (git's stderr is diagnostics, never dirt).
	out, err := finishCleanupGitParsedStdout(ctx, ground, finishCleanupGitTimeout,
		finishCleanupStatusVerb, finishCleanupStatusPorcelainFlag)
	if err != nil {
		return true
	}
	clean, _ := finishCleanupProbeClean(out)
	return !clean
}

// finishCleanupBranchCheckGroundOnBranch reports whether ground's checked-out
// branch IS the delivered branch. It is the lane's last guard before it runs
// anything: a test run in a directory sitting on some OTHER branch would judge
// that other branch's revision and report it as this delivery's. A detached
// HEAD (the finishCleanup sentinel) is not a branch, so it answers false.
func finishCleanupBranchCheckGroundOnBranch(ctx context.Context, ground, branch string) bool {
	out, err := finishCleanupGitParsedStdout(ctx, ground, finishCleanupGitTimeout, finishCleanupPreMergeRevParseVerb, finishCleanupPreMergeAbbrevRefFlag, finishCleanupPreMergeHeadArg)
	if err != nil {
		return false
	}
	head := strings.TrimSpace(out)
	return head != "" && head != finishCleanupDetachedHead && head == branch
}

// finishCleanupPreMergeRev resolves a revision to its object id (ok=false when
// it does not resolve — the caller abstains rather than judging against
// nothing).
func finishCleanupPreMergeRev(ctx context.Context, repoDir, rev string) (string, bool) {
	out, err := finishCleanupGitParsedStdout(ctx, repoDir, finishCleanupGitTimeout, finishCleanupPreMergeRevParseVerb,
		finishCleanupPreMergeVerifyFlag, finishCleanupPreMergeQuietFlag, rev)
	if err != nil {
		return "", false
	}
	out = strings.TrimSpace(out)
	return out, out != ""
}

// mergeCheck is one command a tree is measured with.
type mergeCheck struct {
	// Name identifies the check in an abstention sentence (e.g. "smoke").
	Name string
	// Cmd is the command line, run through `sh -c` in the directory given.
	Cmd string
	// TimeoutSec bounds this check; 0 = no bound.
	TimeoutSec int
	// preflightWhy is the abstention sentence for a check whose PREREQUISITE
	// cannot be met in this tree (the caller measured it before adding the check
	// to the list).
	preflightWhy string
}

// mergeCheckOutcome is the result of running a check list.
//
//	Status verified — the check passed.
//	Status refused  — the check ran and FAILED (Detail is its own diagnostic
//	                  block, Check names it).
//	Status skipped  — nothing could be measured (no cache, a timeout, an
//	                  unresolvable module set, no check armed): Detail is the
//	                  abstention reason, never a silent pass.
type mergeCheckOutcome struct {
	Status string
	Detail string
	Check  string
}

// finishCleanupChecksIn runs `checks` in the directory the run's own ground
// names, in order. The caches are resolved ONCE, before the first check, and an
// unmeasurable environment abstains before anything runs — the lane never falls
// back to the operator's own caches.
func finishCleanupChecksIn(dir string, checks []mergeCheck) mergeCheckOutcome {
	goCache, okCache := mergeBuildGateGoCache(dir)
	if !okCache {
		return mergeCheckOutcome{Status: finishCleanupPreMergeStatusSkipped,
			Detail: "no usable build cache for this tree (the lane never falls back to the operator's caches)"}
	}
	modCache, okMod := mergeBuildGateSharedModCache()
	if !okMod {
		return mergeCheckOutcome{Status: finishCleanupPreMergeStatusSkipped,
			Detail: "no populated shared module cache available to run the branch's own check"}
	}
	abstained := make([]string, 0, len(checks))
	for _, check := range checks {
		if check.Cmd == "" {
			continue
		}
		exit, output, timedOut := mergeCheckOutput(dir,
			mergeBuildGateSpec{Cmd: check.Cmd, TimeoutSec: check.TimeoutSec}, goCache, modCache)
		switch {
		case timedOut:
			abstained = append(abstained, fmt.Sprintf("%s: timed out after %ds", check.Cmd, check.TimeoutSec))
		case exit != 0:
			if check.preflightWhy != "" && !mergeCheckCompiled(output) {
				// The check's own PREREQUISITE was measurably absent, and the
				// run never reached the package: a toolchain precondition is
				// this lane's environment, never the delivery's content.
				abstained = append(abstained, check.preflightWhy)
				continue
			}
			return mergeCheckOutcome{Status: finishCleanupPreMergeStatusRefused, Check: check.Name,
				Detail: fmt.Sprintf("%s: %s", check.Cmd, mergeCheckFailureText(check.Cmd, output))}
		}
	}
	return mergeCheckOutcome{Status: finishCleanupPreMergeStatusVerified,
		Detail: strings.Join(abstained, mergeCheckAbstainSeparator)}
}

// mergeCheckCompiled reports whether a failed run actually reached package
// compilation — a "does not compile" failure carries the toolchain's own
// diagnostic, whereas a test failure exits non-zero with none. It is what keeps
// a real test failure from being softened into an abstention by a preflight
// sentence.
func mergeCheckCompiled(out string) bool {
	for _, needle := range mergeCheckCompileNeedles {
		if strings.Contains(out, needle) {
			return false
		}
	}
	return true
}

// mergeCheckCompileNeedles are the toolchain's own "this did not compile"
// markers (Rule 1: the spellings live here, not at the comparison site).
var mergeCheckCompileNeedles = []string{
	"[build failed]",
	"build constraints exclude all Go files",
	"cannot find package",
	"is not in std",
	"undefined:",
	"# ",
}

// mergeCheckOutput runs one check and returns what its command printed, with a
// bounded amount of the MIDDLE dropped when that is more than the runner is
// willing to retain (mergeCheckOutputCapMax).
//
// WHY THE CAP IS NOT THE TOOL'S HEAD CAP: a check's diagnostics are read from
// the END of its output as well as from its head (mergeCheckFailureText), so a
// capture that kept only the first N bytes would destroy exactly the window the
// refusal needs — the failure block of the last package `go test` reported.
// The drop lands in the middle, it is announced with the number of bytes it
// swallowed, and it is bounded in SPACE only: the check itself still runs to
// completion or to its own timeout.
func mergeCheckOutput(dir string, spec mergeBuildGateSpec, goCache, sharedModCache string) (int, string, bool) {
	var w mergeCheckCappedOutput
	exit, _, timedOut := mergeBuildGateRunCapped(dir, spec, goCache, sharedModCache, &w)
	return exit, w.text(), timedOut
}

// mergeCheckOutputCapPart is the size of EACH retained window: the head gets the
// first half of the budget, the tail the last half, so both ends of a long run
// survive at the same size.
const mergeCheckOutputCapPart = mergeCheckOutputCapMax / 2

// mergeCheckOutputDroppedFmt is the line that stands in for the dropped middle
// (Rule 38's spirit applied to a log: a reader must be able to tell a capped
// capture from a command that printed nothing).
const mergeCheckOutputDroppedFmt = "… [%d bytes of this check's output were not retained by the lane] …"

// mergeCheckCappedOutput is a bounded sink for a check's stream. It keeps the
// FIRST mergeCheckOutputCapPart bytes and the LAST mergeCheckOutputCapPart
// bytes, so `go test` output — whose failing block can be printed after the
// passing packages' lines — is quoted from a capture that still holds it,
// whatever the run's total size.
//
// Redirecting BOTH the child's stdout and stderr to ONE writer is safe here
// because of os/exec's own guarantee: when Stdout and Stderr are the same
// comparable writer, exec serialises the copies itself, so at most one
// goroutine is inside Write at a time. This type assumes exactly that, and any
// caller wiring it up must keep passing the SAME value to both.
type mergeCheckCappedOutput struct {
	head  []byte
	tail  []byte
	total int
}

// Write never fails and never short-writes: it is a capture, not a pipe, and the
// check's exit status must not depend on the lane's memory (a Write that
// reported a short count would make the toolchain see a broken stdout).
func (w *mergeCheckCappedOutput) Write(p []byte) (int, error) {
	n := len(p)
	w.total += n
	if len(w.head) < mergeCheckOutputCapPart {
		room := mergeCheckOutputCapPart - len(w.head)
		take := room
		if take > len(p) {
			take = len(p)
		}
		w.head = append(w.head, p[:take]...)
		p = p[take:]
	}
	if len(p) > 0 {
		w.tail = append(w.tail, p...)
		if len(w.tail) > mergeCheckOutputCapPart {
			w.tail = w.tail[len(w.tail)-mergeCheckOutputCapPart:]
		}
	}
	return n, nil
}

// text renders the capture: the whole output when nothing was dropped, else
// head + the announcement + tail.
func (w *mergeCheckCappedOutput) text() string {
	dropped := w.total - len(w.head) - len(w.tail)
	if dropped <= 0 {
		return string(w.head) + string(w.tail)
	}
	return string(w.head) + "\n" + fmt.Sprintf(mergeCheckOutputDroppedFmt, dropped) + "\n" + string(w.tail)
}

// mergeCheckFailureText extracts the diagnostics a FAILED check's refusal must
// quote, from the check's whole captured output.
//
// WHY THE COMMAND DECIDES: there is no single end that carries a failure, and
// the two tool shapes differ in OPPOSITE directions.
//
//	A COMPILER (`go build`, `go vet`) prints its diagnostics in
//	package-visit order and says nothing at the end, so the HEAD is where the
//	reason is — which is why mergeBuildGateErrorText keeps it.
//
//	A TEST RUN (`go test`) prints one block per package AS EACH PACKAGE
//	FINISHES, and the failing one is a `--- FAIL: TestX` block followed by its
//	assertion lines and its own `FAIL\t<pkg>` summary. MEASURED (two probes, an
//	8-package module whose LAST-listed package passes): the order of those
//	blocks is the toolchain's scheduling, so it is NOT a reliable end. What IS
//	reliable is the block itself: `--- FAIL` opens it and the package's own
//	`FAIL\t<pkg>` line closes it. The extraction therefore reads THAT block out
//	of the middle when the output carries one, and falls back to both ends of
//	the output when it does not (a non-Go command, or a test run that failed
//	without printing a test result).
//
// The bound is the SAME budget the head extraction uses
// (mergeBuildGateErrorLines / mergeBuildGateErrorMaxBytes): only WHICH lines
// are quoted changed, never how many.
func mergeCheckFailureText(cmd, out string) string {
	if !mergeCheckIsTestCommand(cmd) {
		// The compiler shape, unchanged: `go build` / `go vet` state the reason
		// at the head, which is exactly what mergeBuildGateErrorText keeps.
		return mergeBuildGateErrorText(out)
	}
	if block := mergeCheckTestFailureBlock(out); block != "" {
		return block
	}
	// A test command that failed WITHOUT printing a test result (a package that
	// does not compile under `go test`) has no block to read out: both ends of
	// its output are kept, since the head may hold the diagnostic and the tail
	// the toolchain's summary.
	return mergeCheckBothEndsText(out)
}

// mergeCheckIsTestCommand reports whether the check's command line runs a Go
// test run — the ONE command shape whose failure block is not at its head.
//
// TWO shapes select it, and both are read from a declared source, never guessed:
// the bare `go test` verb, and the repo's declared SCOPED entry point
// (`smokegate.ScopedTarget`), whose recipe runs the project's own test suite
// bounded to the packages named. Fields are compared whole, so a module or flag
// that merely CONTAINS the verb ("gotestsum", "./testdata") cannot select the
// test arm; the scoped target is matched as a PREFIX, because its own line
// carries the package assignment beside it.
//
// The scoped arm is load-bearing for the branch-check lane, whose command IS
// that form: without it the lane's refusal would quote the entry point's own
// first line instead of the failing package — the incident shape (a refusal that
// named a passing package and no cause) this function exists to prevent.
// Measured 2026-10-12 (task 01a130a1): TestBranchCheckRunQuotesTheFailingPackage
// failed on exactly this, with the refusal's quote ending at
// `scripts/test-budget.sh …`.
func mergeCheckIsTestCommand(cmd string) bool {
	for _, field := range strings.Fields(cmd) {
		if field == mergeCheckTestVerb {
			return true
		}
	}
	return strings.HasPrefix(strings.TrimSpace(cmd), smokegate.ScopedTarget)
}

// mergeCheckTestFailureBlock extracts the failing package's own block from a
// test run's output: from the first `--- FAIL` line through the `FAIL\t<pkg>`
// summary line that closes it (bounded by the shared budget).
//
// BOTH ends matter and both are kept: the `--- FAIL` line names the test and
// every assertion line under it is the cause, while the closing summary names
// the PACKAGE — what a reader needs in order to go and look. A block that is
// longer than the budget is cut at the budget from its head, so what survives
// is the test's name and its first diagnostics.
//
// Returns "" when the output carries no `--- FAIL` line (the caller then quotes
// both ends of the whole output instead, rather than inventing a block).
func mergeCheckTestFailureBlock(out string) string {
	lines := strings.SplitAfter(out, "\n")
	start, end := -1, -1
	// markerIndent is the COLUMN the marker was found at: the block's lines that
	// are back at (or left of) it are no longer the test's own, which is what
	// ends the block when no `FAIL\t<pkg>` summary follows it.
	markerIndent := 0
	offset := 0
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r\n")
		trimmed := strings.TrimLeft(line, mergeCheckBlockIndentChars)
		indent := len(line) - len(trimmed)
		next := offset + len(raw)
		switch {
		case start < 0 && strings.HasPrefix(trimmed, mergeCheckTestResultPrefix+mergeCheckTestFailVerb):
			start, markerIndent = offset, indent
		case start >= 0 && strings.HasPrefix(trimmed, mergeCheckFailureSummaryPrefix):
			// The package's own summary (`FAIL\t<pkg>\t<time>`): the block's
			// LAST line, kept because it names the package. Only a summary that
			// carries the package and its timing counts — the bare `FAIL`
			// marker one line above it ends no block.
			if !mergeCheckIsPackageSummary(trimmed) {
				offset = next
				continue
			}
			end = offset + len(strings.TrimRight(raw, "\n"))
		case start >= 0 && trimmed != "" && indent <= markerIndent:
			// THE REPORT'S OWN PROSE closes the block (measured 2026-10-31, task
			// 01a18f6e). A quoted block ends where the text that quoted it
			// resumes, and that text sits at the marker's column or left of it.
			// Without this arm the tail ran to the END OF THE OUTPUT — through
			// the report's next section, its unrelated `[setup failed]` package
			// and its stamp line — so the budget was spent on lines that are not
			// the failure and the failing test's own name was pushed out of the
			// quote. Raw `go test` output has no such line (every line is at
			// column 0 beside the marker), so this arm changes nothing there.
			end = offset
		}
		if start >= 0 && end >= 0 {
			break
		}
		offset = next
	}
	if start < 0 {
		return ""
	}
	if end < 0 {
		end = len(out)
	}
	if end <= start {
		return ""
	}
	return mergeBuildGateBoundedLines(out[start:end], mergeBuildGateErrorLines, mergeBuildGateErrorMaxBytes)
}

// mergeCheckIsPackageSummary reports whether a `FAIL`-prefixed line is the
// toolchain's per-package summary (`FAIL\t<pkg>\t<time>`) rather than the bare
// `FAIL` marker the testing package prints above it: two tab-separated fields
// after the marker.
func mergeCheckIsPackageSummary(line string) bool {
	return len(strings.Split(line, "\t")) >= mergeCheckSummaryFieldCount
}

// mergeBuildGateBoundedLines joins the non-blank lines of text, keeping the
// HEAD (up to maxLines lines and maxBytes bytes).
//
// The byte bound never splits the line it lands in: a cap that cut mid-line
// would publish a fragment as if it were the tool's own text.
func mergeBuildGateBoundedLines(text string, maxLines, maxBytes int) string {
	nonBlank := make([]string, 0, mergeBuildGateErrorLines)
	size := 0
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(nonBlank) >= maxLines || (size > 0 && size+len(line) > maxBytes) {
			break
		}
		nonBlank = append(nonBlank, line)
		size += len(line) + 1
	}
	return strings.Join(nonBlank, "\n")
}

// mergeCheckBothEndsText quotes BOTH ends of a failed run's output: the head
// (where a compile diagnostic lives) and the tail (where the toolchain's own
// summary — the failing package and its time — is printed), separated by an
// elision marker when lines were dropped between them.
func mergeCheckBothEndsText(out string) string {
	lines := make([]string, 0, mergeBuildGateErrorLines)
	for _, raw := range strings.Split(out, "\n") {
		if strings.TrimSpace(strings.TrimRight(raw, "\r")) == "" {
			continue
		}
		lines = append(lines, strings.TrimRight(raw, "\r"))
	}
	if len(lines) <= mergeBuildGateErrorLines {
		return strings.Join(lines, "\n")
	}
	head := strings.Join(lines[:mergeBuildGateErrorHeadLines], "\n")
	tail := strings.Join(lines[len(lines)-mergeBuildGateErrorTailLines:], "\n")
	return head + "\n" + mergeBuildGateErrorElision + "\n" + tail
}

// finishCleanupBranchCheckReason records a lane outcome where an operator will
// find it without re-running the close-out: an append-only task_events row on
// the delivery's own row (the ledger's existing auditing channel).
//
// Fail-soft and never blocking: an unreachable ledger costs the record, never
// the close-out (the failure is reported to stderr — Rule 15b). The record is
// written for the VERIFIED verdict too: a lane that ran and passed must be
// distinguishable from one that never ran (Rule 15 — the fact is recorded,
// never inferred from the absence of a record).
func finishCleanupBranchCheckReason(taskID string, res finishCleanupPreMergeResult) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" || strings.TrimSpace(res.Detail) == "" {
		return
	}
	event := finishCleanupBranchCheckEventName(res.Status)
	ctx, cancel := context.WithTimeout(context.Background(), finishCleanupDBTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("finish-cleanup: branch-check record for %s not written (ledger unavailable): %v", taskID, err)
		return
	}
	defer db.Close()
	taskEventWrite(ctx, db, taskID, event, res.Detail)
	finishCleanupBranchCheckPruneTrail(ctx, db, taskID)
}

// finishCleanupBranchCheckEventName maps a verdict onto the trail's event name —
// the ONE definition of that mapping (Rule 13), so the writers and the tests
// cannot drift apart:
//
//	verified → branch-checks-verified   (the lane RAN and passed);
//	skipped  → branch-checks-unverified (it ABSTAINED — the delivery still goes);
//	refused  → branch-checks-refused    (it was proven NOT deliverable).
//
// An unrecognised verdict takes the refusal arm: a name nobody can classify must
// never read back as a pass (Rule 15 — fail toward saying less, never toward
// claiming more).
func finishCleanupBranchCheckEventName(status string) string {
	switch status {
	case finishCleanupPreMergeStatusVerified:
		return finishCleanupPreMergeVerifiedEvent
	case finishCleanupPreMergeStatusSkipped:
		return finishCleanupPreMergeUnverifiedEvent
	}
	return finishCleanupPreMergeRefusedEvent
}

// finishCleanupPreMergeRefusedReason records a close-out step that WITHHELD the
// delivery for a reason that is not this lane's own verdict (the ownership,
// completion and checklist refusals keep the release lane's refusal name so ONE
// grep still finds every withheld delivery). It exists so those call sites state
// a verdict instead of re-typing a result literal at each one.
func finishCleanupPreMergeRefusedReason(taskID, detail string) {
	finishCleanupBranchCheckReason(taskID, finishCleanupPreMergeResult{
		Status: finishCleanupPreMergeStatusRefused,
		Detail: detail,
	})
}

// finishCleanupBranchCheckPruneTrail keeps the newest
// finishCleanupBranchCheckEventKeep events for one row (the trail is bounded,
// the audit survives).
func finishCleanupBranchCheckPruneTrail(ctx context.Context, db *sql.DB, taskID string) {
	_, err := db.ExecContext(ctx,
		`DELETE FROM task_events WHERE task_id = ? AND event IN (?, ?, ?) AND id NOT IN (`+
			`SELECT id FROM task_events WHERE task_id = ? AND event IN (?, ?, ?) ORDER BY id DESC LIMIT ?)`,
		taskID, finishCleanupPreMergeVerifiedEvent, finishCleanupPreMergeRefusedEvent, finishCleanupPreMergeUnverifiedEvent,
		taskID, finishCleanupPreMergeVerifiedEvent, finishCleanupPreMergeRefusedEvent, finishCleanupPreMergeUnverifiedEvent,
		finishCleanupBranchCheckEventKeep)
	if err != nil {
		taskNotifyLog("finish-cleanup: branch-check trail prune for %s failed: %v", taskID, err)
	}
}
