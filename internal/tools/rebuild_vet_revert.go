package tools

// rebuild_vet_revert.go — the post-merge VET gate's REVERT half: the comparative
// judgement that decides whether a red `go vet ./...` is the MERGE's doing, and
// the unwind that takes back only a branch-introduced breakage.
//
// WHY THE GATE WAS REPORT-ONLY, AND WHY THAT WAS RIGHT FOR ITS FIRST VERSION
// (the determination this file acts on; the full evidence trail is
// devdoc/postmerge-vet-gate-revert.md). The first version of the gate
// deliberately held NO revert handle. Its own words in rebuild_vet_gate.go were
// "This gate is the post-hoc measurement whose answer is 'what did we just land,
// and does it still hold together'", and Rule 12 is the rule it took that shape
// from: the close loop's rollback half is a response to a LANDING BUILD failure,
// so a gate that is not a landing build must not roll back. That reasoning holds
// while the measurement is report-only — and it was measured to be insufficient
// on 2026-09-16: a merge landed a tree whose `internal/tools` TEST package no
// longer compiled (`undefined: syncTaskMigrateFrom`), `go build ./...` stayed
// GREEN (so the round declared itself a success), the gate went RED and reported
// through `build-failed`, and master stayed broken until a human read the alarm
// and ran the revert by hand.
//
// A RED BUILD AND A RED VET ARE NOT THE SAME DECISION (the Rule-12 question the
// epic requires answering, and the reason this change does not contradict it):
//
//	| | red BUILD (Rule 12's subject) | red VET (this file's subject) |
//	|---|---|---|
//	| what is broken | the artifact master's OWN contract says it produces | a tree master keeps but does not itself consume |
//	| who is blocked | every consumer of the deployed binary, immediately | the next `go test` in that repo, not the deployed binary |
//	| is master broken? | NO — master still IS what the round made it; the ARTIFACT did not follow | YES — master does not hold together as a source tree |
//	| remedy | none (the merge is good; the build is a separate failure) | take the merge back; the branch's work is preserved |
//	| Rule 12 verdict | a notify, never a rollback | a rollback is AVAILABLE and is the remedy — for a BRANCH-INTRODUCED red only |
//
// The discriminating question is not "how bad is it" but "is the thing master
// now holds DEFECTIVE ON ITS OWN TERMS". A failed `go build` is a statement
// about the artifact, and Rule 12 is explicit that git history stays fine. A red
// `go vet` is a statement about MASTER'S OWN CONTENT — master's test tree is in
// master — and master's contract with every other branch in the queue is that
// `go test ./...` works there. So the remedy exists, and this file applies it
// under the one condition that keeps the remedy honest.
//
// THE CONDITION, AND IT IS THE WHOLE DESIGN: ONLY A BRANCH-INTRODUCED RED IS
// REVERTED. A repo whose master was ALREADY vet-red before the merge pays
// nothing for it — reverting there would unwind honest work for a failure it did
// not cause (and would red every branch in the queue in turn), which is the
// mis-sized-gate class this repo has already paid for twice: the 2026-09-13
// post-merge verify gate judged ABSOLUTE redness and reverted six honest
// branches in an hour. The measurement is the same `go vet ./...` in two trees,
// and the decision is the DIFFERENCE between them.
//
// WHERE THE COMPARISON RUNS: a temporary detached `git worktree` of the RECORDED
// PRE-MERGE TIP (`FailedMergeRevert.PreTip`), under the merge-build scratch class
// (internal/scratch — never /tmp, Rule 22), using the merge-time build gate's own
// resolvers for the cache, the module cache, the environment, the bound and the
// worktree reap. Nothing is invented: the second measurement is the same command
// in the tree the round STARTED from, and the unwind is the close loop's existing
// `RestoreMaster` (branch refs re-created, `reset --hard` to the recorded tip,
// the round's minted version tag dropped).
//
// WHO CALLS IT: `RebuildResult.Vet` is classified here, and cmd/agent's
// serveRebuild decides — so `RebuildAfterMerge` keeps its report-only contract
// (its caller holds the revert handle, it never does). The DECISION and the GIT
// live here, in the tools package that already owns both.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// The comparative verdict carried on VetGateResult
// ---------------------------------------------------------------------------

// vetClassificationReason is WHY a red gate was NOT attributed to the merge.
// It is a separate type from VetGateResult.Reason on purpose: Reason explains an
// ABSTENTION about the measurement ("I could not run the gate"), this explains a
// FAILURE that was measured and NOT attributed to the round ("I ran it, it is
// red, and the red is not this merge's"). Merging the two would let a reader
// paraphrase a kept merge as an unmeasured one — the exact rounding that turns a
// refusal into a rubber stamp.
type vetClassificationReason string

const (
	// vetClassIntroduced — the pre-merge tip VETS and the merged tree does not.
	// The only reason that may unwind a landing.
	vetClassIntroduced vetClassificationReason = "introduced"
	// vetClassPreExisting — the pre-merge tip was ALREADY red: the round
	// changed nothing about this class of failure and must not be blamed for it.
	vetClassPreExisting vetClassificationReason = "pre-existing"
	// vetClassUnknown — the comparison could not be made (no recorded pre-merge
	// tip, no scratch tree, an unreadable revision, a budget the baseline needed
	// and did not get). Fails SAFE: a merge is never unwound on evidence the
	// merger did not obtain (and, per the 2026-09-13 lesson, an unjudged result
	// is NOT a failure).
	vetClassUnknown vetClassificationReason = "unknown"
)

// vetClassification is the comparative judgement of ONE red gate: which reason,
// in the toolchain's own words, and — for the introduced case — the position the
// breakage was first seen at, which is what a rework brief needs to quote.
type vetClassification struct {
	// Reason is the classification (see the constants above).
	Reason vetClassificationReason
	// Detail is the human sentence behind the Reason: the baseline's own
	// diagnostic for a pre-existing red, the blocker for an unknown one. It
	// rides the notify payload and the log line, so an operator never has to
	// re-run the comparison to learn WHY.
	Detail string
	// Reference is the position the failure was first seen at
	// (`file:line[:col]`) for an introduced red; "" otherwise.
	Reference string
}

// VetGateRevertReason is the CLASSIFICATION as the wire spells it — the same
// three words every branch of this design is gated on (Rule 1b: compared and
// rendered values are constants). It is empty for a gate that is not red, so
// callers can branch on it directly.
func (v *VetGateResult) VetGateRevertReason() string {
	if v == nil {
		return ""
	}
	return string(v.Classification)
}

// ReportOnlyBanner is the phrase every keep-rendering carries. It is exported
// because a TEST asserts it: the guarantee that a red gate never silently
// becomes a rollback has to be checkable, and a string a test shares with the
// production rendering cannot drift away from it.
const ReportOnlyBanner = "(merge kept, this is a REPORT)"

// vetClassNoBaselineReason explains an uncomparison — an abstention with a
// reason, never a reversal on evidence nobody holds.
const vetClassNoBaselineReason = "no recorded pre-merge tip to compare against — the failure cannot be attributed to this merge"

// classifyVetFailure decides whether a red gate belongs to the merge. It is only
// ever called for a FAILED verdict: a PASS and an ABSTAIN are classified with no
// baseline (there is nothing to attribute), which is also the honest answer when
// the caller has no pre-merge tip to compare against (a round that landed
// nothing, an operator-initiated merge, a capture that failed).
//
// The measurement is deliberately the SAME command in both trees
// (vetGateSpec.Argv, the gate's own resolved spec): a comparison between two
// different commands would be a comparison of two things.
func classifyVetFailure(vet *VetGateResult, revert *FailedMergeRevert) vetClassification {
	if vet == nil || !vet.Failed() {
		return vetClassification{}
	}
	if revert == nil || revert.Repo == "" || revert.PreTip == "" {
		return vetClassification{
			Reason: vetClassUnknown,
			Detail: vetClassNoBaselineReason,
		}
	}
	if vet.Repo != "" && revert.Repo != vet.Repo {
		// A baseline from ANOTHER repo says nothing about this one, and saying so
		// is the point: a mis-wired handle must be visible, not assumed
		// equivalent (they are different trees entirely).
		return vetClassification{
			Reason: vetClassUnknown,
			Detail: fmt.Sprintf("the recorded pre-merge tip belongs to %s, not to the vetted repo %s", revert.Repo, vet.Repo),
		}
	}
	baseline, reason := vetGateBaselineAt(revert.Repo, revert.PreTip, vet)
	if baseline.Status == mergeBuildGateStatusAbstain {
		return vetClassification{Reason: vetClassUnknown, Detail: reason}
	}
	if !baseline.Failed() {
		// The pre-merge tip VETTED and the merged tree does not: the round
		// introduced the breakage, and it is the only case this design unwinds.
		return vetClassification{
			Reason:    vetClassIntroduced,
			Detail:    "the pre-merge tip vets and the merged tree does not (" + vet.Repo + " at " + revert.PreTip + ")",
			Reference: firstRefOf(vet),
		}
	}
	// The tree the round STARTED from was already red — and the finding it is red
	// WITH is quoted verbatim, so a reader can compare the two outputs instead of
	// trusting the word "pre-existing".
	return vetClassification{
		Reason: vetClassPreExisting,
		Detail: fmt.Sprintf("the pre-merge tip %s was ALREADY vet-red before this merge (%s vs %s)",
			revert.PreTip, firstRefOf(&baseline), firstRefOf(vet)),
	}
}

// ---------------------------------------------------------------------------
// The baseline measurement (the same gate, in the tree the round started from)
// ---------------------------------------------------------------------------

// vetGateBaselineAt is the seam's insertion point: the substitution short-
// circuits BEFORE the worktree is materialised, so the pre-existing arm costs no
// checkout and no second vet run — and cannot be confused with a real reading. It
// is a named function (rather than a branch inside the measurement) so the
// gate's own spec-level tests keep exercising the REAL measurement
// (vetGateMeasureBaselineAt) with nothing substituted.
func vetGateBaselineAt(repo, rev string, merged *VetGateResult) (VetGateResult, string) {
	if ok, detail := vetBaselineSubstituted(); ok {
		res := VetGateResult{Status: mergeBuildGateStatusFail, Gate: vetGateName, Repo: repo, Reason: detail}
		res.ErrorCount = 1
		res.Findings = []vetGateFinding{{File: vetBaselineOverrideMarkerFile, Line: "1", Message: detail}}
		return res, ""
	}
	return vetGateMeasureBaselineAt(repo, rev, merged)
}

// vetGateMeasureBaselineAt runs the gate's OWN measurement over rev, in a
// temporary detached worktree under the merge-build scratch class, and returns the
// baseline verdict (or an ABSTAIN explaining why the comparison could not be
// made). It never returns an error: every failure to measure is a verdict.
//
// The tree is a WORKTREE of a committed revision, not the merger's own checkout:
// the pre-merge tip exists only in git, the merger's checkout holds the MERGED
// tree, and checking the old revision out in place would destroy the very state
// the comparison is about.
func vetGateMeasureBaselineAt(repo, rev string, merged *VetGateResult) (VetGateResult, string) {
	abstained := func(why string) (VetGateResult, string) {
		return VetGateResult{Status: mergeBuildGateStatusAbstain, Gate: vetGateName, Repo: repo, Reason: why}, why
	}
	spec, reason := vetGateResolveSpec(repo)
	if reason != "" {
		return abstained(reason)
	}
	if merged != nil && merged.TimeoutSec > 0 {
		// The baseline is measured under the budget the gate ACTUALLY ran with
		// (the resolved one, not a second reading of the environment): two
		// measurements compared against each other must have had the same terms,
		// and a baseline given a different budget could crash into a timeout and
		// be misread as the measurement it never finished.
		spec.TimeoutSec = merged.TimeoutSec
	}
	if strings.TrimSpace(rev) == "" {
		return abstained(vetClassNoBaselineReason)
	}
	workRoot, cleanup, ok := mergeBuildGateScratch(repo)
	if !ok {
		return abstained("no usable scratch tree for a baseline checkout")
	}
	defer cleanup()
	// The gate's own reap, so a baseline checkout cannot outlive the comparison
	// (the registration residue this codebase has measured before).
	defer mergeBuildGateReapRegistrations(repo, workRoot)
	dir := filepath.Join(workRoot, mergeBuildGateWorktreeDirName(rev))
	// FRESH PATH (worktree_fresh_path.go): same derived-name collision as the
	// build gate's checkout, and the same consequence — a reused empty
	// directory would make the BASELINE measurement read a tree this run did
	// not create. Abstain rather than compare against an unknown tree.
	if err := worktreeFreshPath(dir); err != nil {
		return abstained(fmt.Sprintf("baseline worktree path %s is not fresh: %s", dir, err))
	}
	if _, err := repoSweepGit(repo, mergeBuildGateWorktreeVerb, mergeBuildGateWorktreeAddVerb,
		mergeBuildGateWorktreeDetachFlag, dir, rev); err != nil {
		return abstained(fmt.Sprintf("worktree add --detach %s failed: %s", rev, mergeBuildGateTail(err.Error())))
	}
	return vetGateRunIn(dir, spec, repo), ""
}

// vetGateRunIn executes the gate spec in dir and shapes the outcome as a
// VetGateResult — the ONE execution-to-verdict path, shared by the merged-tree
// measurement (runPostMergeVetGate) and the baseline one, so a tree can never be
// judged by two subtly different readings of the same command.
func vetGateRunIn(dir string, spec vetGateSpec, resultRepo string) VetGateResult {
	res := VetGateResult{Status: mergeBuildGateStatusAbstain, Gate: vetGateName, Repo: resultRepo, TimeoutSec: spec.TimeoutSec}
	out := vetGateExecute(dir, spec)
	res.Elapsed = out.Elapsed
	if out.Abstain != "" {
		res.Reason = out.Abstain
		return res
	}
	combined := vetGateOutput(out.Stdout, out.Stderr)
	res.Findings, res.ErrorCount = vetGateParseFindings(out.Stdout, out.Stderr)
	res.PackageCount = vetGateCountPackages(combined)
	// An EMPTY module is not a red tree: the toolchain says so in its own words,
	// and neither a pass nor a failure would be a measurement.
	if strings.Contains(combined, vetGateNoPackagesMarker) {
		res.Reason = vetGateNoPackagesReason
		return res
	}
	// SILENCE IS THE PASS. `go vet` prints nothing at all on a clean tree
	// (measured: exit 0, both streams empty), so a zero exit with no diagnostic IS
	// the measurement "the tree vets" — the tools that answer on success (a
	// build's stamp, a test's ok line) are not this one.
	if out.Err == nil && res.ErrorCount == 0 {
		res.Status = mergeBuildGateStatusPass
		return res
	}
	// Everything else is the measurement: the toolchain ran and answered that the
	// tree does not vet.
	res.Status = mergeBuildGateStatusFail
	if len(res.Findings) == 0 {
		// A failure shape the parser does not know (a panic, a signal, a
		// diagnostic spelling this file has never seen) is still REPORTED — by
		// name, with the output tail as its evidence. Reporting nothing because
		// the parse came up empty is exactly the silent failure Rule 15b forbids.
		res.OutputTail = outputTail(combined)
	}
	return res
}

// firstRef is the ONE rendering of a verdict's position: its first finding, else
// the toolchain's own text (bounded) so a failure shape the parser does not know
// is still described with evidence, never as a bare "it failed" (Rule 15b).
func firstRef(r VetGateResult) string {
	if len(r.Findings) > 0 {
		return r.Findings[0].String()
	}
	if r.OutputTail != "" {
		return r.OutputTail
	}
	return vetGateNoDiagnosticReason
}

// firstRefOf is firstRef for the verdict the classifier holds by pointer (the
// merged one), so no copy is made just to describe it.
func firstRefOf(r *VetGateResult) string {
	if r == nil {
		return vetGateNoDiagnosticReason
	}
	return firstRef(*r)
}

// ---------------------------------------------------------------------------
// The revert decision — the ONE place that decides, with the git beside it
// ---------------------------------------------------------------------------

// VetGateRevertDecision is the classifier's answer for one close-loop round: the
// classification and the classification-driven REPORT (a log line plus a notify).
// The caller does not re-derive it.
type VetGateRevertDecision struct {
	// Revert is true ONLY for a red gate the merge introduced — the single
	// condition under which this design unwinds a landing.
	Revert bool
	// Class is the full classification (Reason + Detail + Reference).
	Class vetClassification
}

// ClassifyVetGateRevert is the entry point the close loop calls: it classifies a
// red gate against the recorded pre-merge tip and REPORTS the outcome, returning
// whether the caller must unwind.
//
// The deviation from the first version's Rule 12 shape, stated where a reader
// will meet it: on a red gate the report is still made — that half never changes
// — but an INTRODUCED red now also returns Revert=true, because a tree that does
// not hold together is master's own defect and the close loop owns the repair
// (see the header's build-vs-vet table). A pre-existing red and an uncomparable
// red keep the original behaviour exactly: report, keep the merge, blame nobody.
func ClassifyVetGateRevert(vet *VetGateResult, revert *FailedMergeRevert) VetGateRevertDecision {
	class := classifyVetFailure(vet, revert)
	if vet != nil {
		vet.Classification = class.Reason
		vet.ClassificationDetail = class.Detail
		vet.ClassificationRef = class.Reference
	}
	if class.Reason == "" {
		// Not a red gate: nothing to classify, nothing to revert. Nothing is
		// logged here either — the gate's own PASS/ABSTAIN line already reported
		// it, and a second line per round is the noise that trains an operator to
		// stop reading the log.
		return VetGateRevertDecision{Class: class}
	}
	if class.Reason == vetClassIntroduced {
		vetGateLog("INTRODUCED BY THIS MERGE: %s", vetIntroducedSummary(vet, class))
		NotifySyntheticEvent(VetIntroducedEvent, vet.Repo, "", vetIntroducedSummary(vet, class))
		return VetGateRevertDecision{Revert: true, Class: class}
	}
	vetGateLog("NOT attributed to this merge (%s): %s", class.Reason, vetNotAttributedSummary(vet, class))
	NotifySyntheticEvent(RebuildVetDispositionEvent, vet.Repo, "", vetNotAttributedSummary(vet, class))
	return VetGateRevertDecision{Class: class}
}

// RebuildVetRevertedWord is how the classifier's own sentence ends for an
// introduced red ("merge reverted, its branch preserved"). It lives HERE, beside
// the classification that produces it, so the word and the decision cannot drift.
const RebuildVetRevertedWord = "merge reverted, its branch preserved"

// WHY THE DISPOSITION IS DELIVERED AS ITS OWN SENTENCE (measured 2026-09-17):
// the gate's own failure line is emitted the moment the gate runs — before the
// classification can exist, because attributing the red needs a SECOND
// measurement of the pre-merge tip. So the two are separate deliveries: the gate
// announces the MEASUREMENT (build-failed: gate, repo, position, counts), and the
// classifier announces the DISPOSITION (kept with the report banner, or reverted).
// Keeping them in one sentence is what made a REVERTED round tell a subscribed
// operator "merge kept, this is a REPORT" — each sentence must be true at the
// moment it is sent, and a disposition is not known at measurement time.
const RebuildVetDispositionEvent = VetFailedEvent

// VetIntroducedEvent is the event the classifier's INTRODUCED disposition rides.
// It is deliberately the SAME event family the reversion path uses: both mean "a
// landing was taken back", and an operator watching reversions should not have to
// also watch a second channel. The measurement delivery and this one are separated
// by their SENTENCE, not by their event name (see the split above): the measurement
// line never claims a disposition, so the two can never contradict each other.
const VetIntroducedEvent = RevertedEvent

// The two report renderings (Rule 1b: the report's own spellings).
const (
	// vetIntroducedSummaryFmt states the attribution AND the disposition: a
	// reversion is coming and the branch's work survives it.
	vetIntroducedSummaryFmt = "post-merge gate FAILED and the failure is INTRODUCED by this merge: gate=%s repo=%s %s (%d finding(s), %d package(s)) — " + RebuildVetRevertedWord + " (reverting the landing; the branch's work is preserved)"
	// vetNotAttributedSummaryFmt states the reason, the baseline in the
	// toolchain's own words, and the report-only banner — the payload says the
	// disposition instead of leaving it to be inferred.
	vetNotAttributedSummaryFmt = "post-merge gate FAILED but NOT attributed to this merge (%s): gate=%s repo=%s %s %s"
)

// vetIntroducedSummary is the ONE rendering of an introduced red (the log line
// and the notify payload carry the same sentence).
func vetIntroducedSummary(vet *VetGateResult, class vetClassification) string {
	ref := class.Reference
	if ref == "" {
		ref = firstRefOf(vet)
	}
	return fmt.Sprintf(vetIntroducedSummaryFmt, vet.Gate, vet.Repo, ref, vet.ErrorCount, vet.PackageCount)
}

// vetNotAttributedSummary is the ONE rendering of a red gate that stays put.
func vetNotAttributedSummary(vet *VetGateResult, class vetClassification) string {
	ref := firstRefOf(vet)
	if class.Reason == vetClassPreExisting {
		// For a pre-existing red the BASELINE's own finding is the useful
		// reference: it is the failure master already had, which is precisely
		// what is not this merge's doing.
		ref = class.Detail
	}
	return fmt.Sprintf(vetNotAttributedSummaryFmt, class.Reason, vet.Gate, vet.Repo, ref, ReportOnlyBanner)
}

// RevertIntroducedVetFailure unwinds a landing whose red gate this file
// attributed to the merge — the close loop's existing mechanism, invoked from the
// ONE place that learned the reason.
//
// It is a thin, named wrapper ON PURPOSE. The caller could call RestoreMaster
// itself, but then the "this path may only run for an introduced red" rule would
// live at a call site in another package, next to an if-statement — and the next
// edit would move the condition and leave the unwind. Here the rule and the git
// are in one file, and cmd/agent decides only WHEN to ask.
//
// What is NOT done here, deliberately: the close loop's ORDER. Restoring master
// invalidates the decision to build the merged tree (there is nothing left to
// build), so the CALLER checks this before kicking off its build — not after. A
// revert issued after a build would leave the binary stamped at a merge master no
// longer has, which is the stale-artifact failure inverted.
func RevertIntroducedVetFailure(revert *FailedMergeRevert, vet *VetGateResult) ([]string, error) {
	if revert == nil || revert.Repo == "" {
		return nil, fmt.Errorf("vet gate revert: no revert handle to unwind")
	}
	if vet == nil || vet.Classification != vetClassIntroduced {
		return nil, fmt.Errorf("vet gate revert: refusing to unwind %s — the failure was not attributed to this merge (%q)",
			revert.Repo, classificationWordOf(vet))
	}
	restored, err := revert.RestoreMaster()
	if err != nil {
		// Rule 15b: a failed unwind is the loudest thing this design can say —
		// master may be left red, so it is reported and NEVER read as a success
		// by the caller (the error propagates).
		vetGateLog("REVERT FAILED for %s (master may be left vet-red at the merge): %v", revert.Repo, err)
		return restored, err
	}
	// The MINTED version tag is dropped by RestoreMaster's own step ③ from the
	// handle the caller passed in — never re-resolved here: the round's name is a
	// fact about the round, and a second lookup would be a second answer.
	vetGateLog("REVERTED %s to the pre-merge tip %s — the gate failure (%s) is introduced by the landing, and its version tag is dropped with it (%d finding(s))",
		revert.Repo, revert.PreTip, vet.ClassificationRef, vet.ErrorCount)
	RevertNotify(revert.Repo, revert.PreTip, "", 0, "post-merge vet gate: "+vet.ClassificationRef+" — introduced by this merge")
	return restored, nil
}

// classificationWordOf renders a verdict's classification for a message, using
// the unknown word when nothing was set (a verdict that never reached the
// classifier): the message must state a reason, never an empty parenthesis.
func classificationWordOf(vet *VetGateResult) string {
	if vet == nil || vet.Classification == "" {
		return string(vetClassUnknown)
	}
	return string(vet.Classification)
}

// ---------------------------------------------------------------------------
// The seam that makes the DIRECTION provable on the real merger path
// ---------------------------------------------------------------------------

// vetBaselineOverrideEnvName names the test/diagnostic seam that substitutes the
// pre-merge baseline measurement: a value of "red" makes the baseline report red
// the way the merged tree does, i.e. it CONSTRUCTS the pre-existing state a real
// repo must otherwise be in for that arm to be reachable.
//
// WHY THE SEAM IS NOT A HOLE: its only effect is to make the comparative
// measurement MORE conservative — a substituted "red" can move the verdict toward
// KEEPING a merge and can never move it toward REVERTING one, because Revert
// requires the baseline to be measured green. A production process has no reason
// to set it and no way to be wrong because of it; what it buys is that both arms
// of this design can be driven through the REAL close loop (cmd/agent's
// serveRebuild) with a real git revert, instead of only through a unit call. Rule
// 2: the constant is declared here, the ONE reader is vetBaselineOverride.
const vetBaselineOverrideEnvName = "GOPI_VET_GATE_BASELINE_OVERRIDE"

// vetBaselineOverrideRed is the only accepted value: red means "report the
// baseline as red regardless of the tree" (the pre-existing arm). Anything else —
// including "" (the production case) — is IGNORED, never an error: a nonsense
// value must not be able to stop the close loop.
const vetBaselineOverrideRed = "red"

// vetBaselineOverrideRedDetail is the Detail recorded when the seam stands in for
// the measurement. It says so OUT LOUD: a reader of the payload must never
// believe a substituted baseline was measured.
const vetBaselineOverrideRedDetail = "the baseline was SUBSTITUTED as red by " + vetBaselineOverrideEnvName + "=" + vetBaselineOverrideRed + " (a test seam — no tree was measured)"

// vetBaselineOverrideMarkerFile is the position the substituted baseline
// reports: there is no real file to point at, and pointing at nothing would render
// the finding as ":1".
const vetBaselineOverrideMarkerFile = vetBaselineOverrideEnvName + "=" + vetBaselineOverrideRed

// vetBaselineOverride reads the seam (Rule 2: the ONE reader).
func vetBaselineOverride() string {
	return strings.ToLower(strings.TrimSpace(os.Getenv(vetBaselineOverrideEnvName)))
}

// vetBaselineSubstituted reports whether the seam applies to a baseline
// measurement, with the Detail to use instead of a measured one.
func vetBaselineSubstituted() (bool, string) {
	if vetBaselineOverride() != vetBaselineOverrideRed {
		return false, ""
	}
	return true, vetBaselineOverrideRedDetail
}
