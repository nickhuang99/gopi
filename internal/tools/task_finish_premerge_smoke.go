package tools

// The close-out's BRANCH-SCOPED TEST RUN — the check that learns something a
// build cannot.
//
// THE DEFECT THIS CLOSES (measured, task 01a12ca0, 2026-10-12). The gate that
// used to sit here measured `go build` alone on a trial-merged tree. `go build`
// compiles no `_test.go`, so a defect that exists only in the COMPOSITION of two
// branches — a writer green on its branch, a reader whose filter green on its
// branch, red together — was invisible in every place it was checked. The
// measurement that followed: the shared master carried two RED chat fixtures for
// ≈ 2 d 9.5 h, and no gate on the merge path noticed.
//
// WHAT IT IS NOW (2026-10-12, second revision — the trial merge was removed).
// The check still exists, and it is measured where it is both CHEAP and
// DECISIVE: on the BRANCH'S OWN REVISION, in the branch's own worktree, before
// the push.
//
//	command  — the ONE declared source (`smokegate.ScopedForPackages`, rendering
//	           the repo's scoped target `make test-budget PKGS='…'`, which routes
//	           through the declared per-package budget script and the heavy-skip
//	           ledger). Never re-inlined here (Rule 14).
//	packages — the primitive that already exists for exactly this question:
//	           `smoke.TouchedPackages(<the branch's ground>, <integration ref>,
//	           HEAD)` answers "which Go packages can THIS BRANCH have
//	           regressed", from the branch's own diff. The base is a LOCAL ref,
//	           so nothing is fetched and nothing is merged: this lane reads the
//	           integration branch's objects, it never writes to the branch.
//
// WHY THE BASE IS THE INTEGRATION REF, NOT A PEER MIRROR: the question is "did
// this branch introduce a red", and the integration branch is the ref that
// defines what the branch changed. It is resolved by the same resolver the rest
// of the close-out uses (Rule 14), and it is the same three-dot range the merge
// machinery uses.
//
// WHY IT NO LONGER RUNS ON A MERGED TREE: the composed tree's verdict is the
// SERVER's to make, on the integration branch, once (see the header of
// task_finish_premerge.go for the measurement that removed the trial merge). The
// branch-scoped run is the half the author can act on before the ref leaves, so
// it is the half that stays.
//
// EVERY un-measurable condition ABSTAINS and is REPORTED (Rule 15): the check
// disabled by its kill switch, a tree that carries no scoped entry point, a diff
// base that does not resolve, a diff that touches no Go package, a budget trip,
// an unresolvable module set, a dirty ground that makes the run unrepresentative
// of the pushed revision. A REFUSAL is reserved for what the branch's own
// revision did — a failing test — and it names the check and the failing test's
// own message.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/gendoc"
	"github.com/nickhuang99/gopi/internal/smoke"
	"github.com/nickhuang99/gopi/internal/smokegate"
)

// --- tunables (Rule 1: every literal this check uses lives here) ---

const (
	// mergeCheckSmokeName is how the check list names the branch's own test run
	// ("build" / "smoke"): printed in the abstention sentence and matched by the
	// tests that pin the refusal's wording.
	mergeCheckSmokeName = "smoke"

	// finishCleanupBranchCheckTimeoutSec bounds the check's subprocess. It is
	// DERIVED from the entry point's own budget rather than invented: the
	// declared smoke entry point (scripts/test-budget.sh) applies one
	// per-package budget and reports a trip as a TIMEOUT with its own exit code,
	// so the lane's bound only has to sit above it and catch a WEDGED process the
	// script's own accounting cannot. Below the script's budget this number
	// would kill legitimate runs; that is why it is not a round guess.
	finishCleanupBranchCheckTimeoutSec = 30 * 60

	// mergeCheckSmokeUnmeasurableMark is the abstention sentence for a run whose
	// PREREQUISITE is absent: the entry point builds the tested CLI binary from
	// the tree under test and says so in its own words ("not evidence about this
	// tree") when it could not. A toolchain precondition is the LANE's
	// environment, never the delivery's content.
	mergeCheckSmokeUnmeasurableMark = "the declared entry point cannot build its tested binary from this tree"

	// finishCleanupBranchCheckNoEntryMark is the abstention sentence for a tree
	// that does not carry the declared scope target (the caller appends the file
	// and target it looked for): the lane never invents a command for a tree
	// that has none (Rule 12's contract, read at the other end), and never
	// substitutes the whole-tree one (`make test`), which would be the run
	// Rule 27 forbids wearing a plausible name.
	finishCleanupBranchCheckNoEntryMark = "this tree carries no declared scoped test entry point"

	// finishCleanupBranchCheckNoPackagesMark is the abstention sentence for the
	// ONE shape that still has nothing to run: a tree that carries NONE of the
	// gate roster's packages AND whose diff selects no Go package either. The
	// membership the caller asks is the UNION's result, never the diff's
	// selection alone — a diff that selects nothing is not an abstention any
	// more, because the roster alone is a runnable command (bug row 01a1ad80:
	// asked of the diff, this sentence was the early exit that skipped every
	// gate on a markdown-only landing).
	finishCleanupBranchCheckNoPackagesMark = "the branch's own diff names no Go package"

	// --- what a MISSING GROUND withheld (the unrun-selection note) ---
	//
	// THE SHAPE THIS CLOSES (epic 01a13303). The lane's abstention for a
	// missing ground said only "the branch's own test run was NOT made" — the
	// same sentence for "there was nothing to run" and for "there were
	// packages, and nobody ever ran them". Measured three times in this repo
	// (drifted commits 3217781bc / 22c38dbd5 were already red at their own
	// revision, and the one machine that runs internal/standards never ran
	// them). Every gate downstream abstains BY DESIGN (mergeset is
	// report-only, the merger's ⓪e checks blobs, Rule 27's smoke is
	// branch-scoped) — so this lane's sentence is the only place the fact can
	// appear, and it has to name WHAT was withheld rather than only THAT
	// something was.
	//
	// It costs no directory and no new mechanism: the selection is the same
	// primitive the run itself uses (smoke.TouchedPackages — two git reads
	// against the branch's own ref), so the note is a fact about the OBJECT
	// STORE, available exactly when the ground is not (Rule 35: no new hook,
	// no new file, one more reader of an existing primitive).
	finishCleanupBranchCheckUnrunHead = " — what the missing ground withheld, computed from the git objects alone: the packages this branch's diff selects are "

	// finishCleanupBranchCheckUnrunEmpty is the same note when the diff selects
	// nothing: the withheld work was empty, and saying so is the difference
	// between "nothing needed running" and "nobody ran it" (Rule 15).
	finishCleanupBranchCheckUnrunEmpty = " — what the missing ground withheld, computed from the git objects alone: this branch's diff selects NO Go package, so its own test run had nothing to run"

	// finishCleanupBranchCheckUnrunUnknown is the note when the selection
	// itself could not be computed here (no integration ref, an unreadable
	// ref): the abstention then says the packages are UNKNOWN rather than
	// silent — never an empty list, which would read as "nothing to run".
	finishCleanupBranchCheckUnrunUnknown = " — what the missing ground withheld could not be computed from the git objects alone: "
)

// finishCleanupBranchCheckWithheldNote reports WHAT the branch's own test run
// would have selected, computed with NO GROUND AT ALL — from the branch's own
// ref in the object store, through the same primitive the run itself uses
// (smoke.TouchedPackages: two reads, `git diff base...branch` and
// `git ls-tree branch`). It is the second half of the missing-ground
// abstention: the lane could not RUN the check, but it can always say which
// packages were left unrun, and a sentence that names them cannot be mistaken
// for "there was nothing to run".
//
// WHY THIS IS NOT A NEW MECHANISM (Rule 35): no hook, no file, no new tool —
// one more reader of an existing selection primitive, called on the path that
// already had to decide the question and simply declined to answer it.
//
// EVERY ARM IS NAMED (Rule 15): an empty selection says the withheld work was
// EMPTY; a selection that could not be computed at all (no repository, no
// branch, no integration ref, an unreadable ref) says UNKNOWN and carries the
// reason — never an empty list, which would read as the first arm.
func finishCleanupBranchCheckWithheldNote(ctx context.Context, repoDir, branch string) string {
	if strings.TrimSpace(repoDir) == "" || strings.TrimSpace(branch) == "" {
		return finishCleanupBranchCheckUnrunUnknown + finishCleanupBranchCheckNoBranchMarker
	}
	base := finishCleanupIntegrationRef(ctx, repoDir)
	if base == "" {
		return finishCleanupBranchCheckUnrunUnknown + finishCleanupBranchCheckNoBaseMarker
	}
	sel, err := smoke.TouchedPackages(repoDir, base, branch)
	if err != nil {
		return finishCleanupBranchCheckUnrunUnknown + err.Error()
	}
	// The WITHHELD SELECTION is the SAME union the run itself would have used
	// (smoke.WithGatePackagesIn) — a sentence naming only the diff's packages
	// would tell an operator the gates were covered when they were not, which is
	// the defect this change closes, restated in prose. It is asked of the
	// OBJECT STORE (repoDir), the only tree this arm has: a missing ground has no
	// checkout to inspect, and asking anyway keeps this sentence and the run's
	// own list built by the same function.
	packages := smoke.WithGatePackagesIn(repoDir, sel.Packages)
	if len(packages) == 0 {
		return finishCleanupBranchCheckUnrunEmpty
	}
	return finishCleanupBranchCheckUnrunHead + strings.Join(packages, mergeCheckUnrunPackageSeparator)
}

// mergeCheckUnrunPackageSeparator joins the withheld package patterns in the
// abstention sentence. It is the list separator an operator reads, spelled once
// (Rule 1b) — the sentence is compared by name in the tests below.
const mergeCheckUnrunPackageSeparator = ", "

// mergeCheckSmokeCommand renders the check's command line: the branch-SCOPED
// smoke form of the declared entry point (smokegate.ScopedForPackages — the
// target and the assignment are spelled in that package, never here), carrying
// the selected package patterns as ONE quoted argument.
//
// WHY NOT smokegate.Command: `make test` runs the WHOLE MODULE (its recipe is
// `scripts/test-budget.sh ./...`), so using it here would be the run Rule 27
// forbids on the delivery path. The scoped target is the declared bounded form
// of the same command.
//
// The quoting is not cosmetic: `sh -c` re-splits an unquoted line, so the
// package list would be read by make as further TARGETS and the run would
// silently cover a subset — the failure smokegate.ScopedCommand's own
// single-quoted slot exists to prevent.
func mergeCheckSmokeCommand(packages []string) string {
	return smokegate.ScopedForPackages(packages, mergeCheckShellQuote)
}

// mergeCheckSmokeUnmeasurable reports whether the declared entry point can
// actually measure `packages` in `dir` — its own PREREQUISITE, asked before the
// run so a preflight failure is never read as a test failure.
//
// The prerequisite is the entry point's own: scripts/test-budget.sh builds the
// tested CLI binary from `./cmd/agent/` of the tree under test and states that a
// run which could not build it is "not evidence about this tree". So the lane
// asks the same question the script asks — does this tree carry that package —
// and abstains rather than blaming the delivery for the lane's environment.
//
// It is deliberately a QUESTION ABOUT THE TREE, not about the script's wording:
// a tree that HAS the package may still fail to build it for a real reason, and
// that failure is the delivery's (the run reports it).
func mergeCheckSmokeUnmeasurable(dir string, packages []string) bool {
	if len(packages) == 0 {
		return true
	}
	info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(finishCleanupCLIPackageDir)))
	return err != nil || !info.IsDir()
}

// mergeCheckShellQuote renders ONE argv token so `sh -c` passes it through
// verbatim: single-quoted, with any embedded single quote escaped the POSIX way.
func mergeCheckShellQuote(token string) string {
	return "'" + strings.ReplaceAll(token, "'", `'\''`) + "'"
}

// finishCleanupCLIPackageDir is the package the declared entry point builds its
// tested binary from — its PREREQUISITE, read off the tree by
// mergeCheckSmokeUnmeasurable before any verdict is formed about a delivery.
const finishCleanupCLIPackageDir = "cmd/agent"

// finishCleanupBranchCheckTest builds the branch-scoped test check for the
// ground `dir`, against the diff base `base` (the integration branch).
//
// It returns (check, abstention, ok): ok=false means the check could not be
// BUILT for this tree, and `abstention` is the sentence the caller must report
// (never a silent pass). The four questions this function can answer without
// running anything: is the check switched off, does the tree carry the declared
// scoped entry point, does the diff select any package at all, can the entry
// point's prerequisite be met here.
//
// The selection is taken against THE GROUND ITSELF — the directory the branch is
// checked out in and the one the check runs in — because the question is "what
// did THIS BRANCH change" and `branch` names the revision being pushed.
func finishCleanupBranchCheckTest(dir, base, branch string) (mergeCheck, string, bool) {
	if !finishCleanupBranchCheckOn() {
		return mergeCheck{}, finishCleanupBranchCheckDisabledMarker, false
	}
	// The declared scope target, read off the tree itself (Rule 12: a repo's
	// build contract is declared, not discovered): a tree that does not declare
	// it is an abstention — the lane never invents a command for someone else's
	// project.
	declared, err := smokegate.DeclaresScopedTarget(dir)
	if err != nil || !declared {
		// Two different abstentions, one sentence each: a tree with no Makefile
		// (the read failed) and a Makefile with no such target are both "nothing
		// to run here", and the sentence carries the file it looked for.
		return mergeCheck{}, finishCleanupBranchCheckNoEntryMark + " (" + smokegate.ScopedTarget + " in " +
			smokegate.EntryPointFile + ")", false
	}
	sel, err := smoke.TouchedPackages(dir, base, branch)
	if err != nil {
		// The selection is a git query: a failure to run it is a measurement
		// failure, and the branch is never blamed for it (Rule 15).
		return mergeCheck{}, finishCleanupBranchCheckNoPackagesMark + ": " + err.Error(), false
	}
	// THE DIFF'S PACKAGES UNION THE MUST-RUN GATE ROSTER (this epic's fix). The
	// command below is `TouchedPackages` UNION `smoke.GatePackageDirs`, built by
	// the ONE pure function that owns the union, so a branch whose diff touches
	// no gate package still runs the gates — which is the whole point: the gates
	// are the INVARIANT, not a consequence of the diff, and their absence from
	// this command is what let two real reds live on master across a merge wave.
	//
	// An EMPTY diff selection is no longer an abstention with nothing to do: the
	// roster alone is a runnable command (a docs-only landing has moved a gate's
	// fixture before — the man-source red in mergeset_cmdagent.go's header). What
	// remains an abstention is a tree that does not CARRY the roster's packages,
	// which is the next question and is asked below.
	//
	// THE UNION IS TAKEN FIRST, AND THE ABSTENTION IS ASKED OF ITS RESULT. The
	// order is the whole fix: asked of the DIFF's selection (the shape this
	// function had), the abstention fires on the one branch the roster exists
	// for — a docs-only landing selects no Go package, so the lane returned
	// before the union was ever computed and every gate in the roster went unrun.
	// Measured on this repo: `07b85fd2` landed the gap-doc gate and the three
	// docs-only commits after it (`115340f6` / `8a867570` / `521e580b`, 0 Go
	// files each) each took this path, so the gate they had to satisfy ran
	// nowhere. The sentence below therefore now describes the tree, not the
	// diff: it is the abstention for a tree that carries NONE of the roster's
	// packages (and whose diff selected nothing either) — the only remaining
	// shape with nothing to run (Rule 15: the abstention names its reason).
	packages := smoke.WithGatePackagesIn(dir, sel.Packages)
	if len(packages) == 0 {
		return mergeCheck{}, finishCleanupBranchCheckNoPackagesMark, false
	}
	check := mergeCheck{
		Name:       mergeCheckSmokeName,
		Cmd:        mergeCheckSmokeCommand(packages),
		TimeoutSec: finishCleanupBranchCheckTimeoutSec,
	}
	// The check is armed with its preflight abstention BEFORE the run: the
	// entry point's own prerequisite is only meaningful when the runnable list is
	// non-empty — which is `packages`, the same list the command carries, never
	// the diff's raw selection computed beside it.
	if mergeCheckSmokeUnmeasurable(dir, packages) {
		check.preflightWhy = mergeCheckSmokeUnmeasurableMark
	}
	return check, "", true
}

// --- the published-anchor arm: the check the branch's own change OBLIGES ---

// The names this arm reports under. They are spelled once here (Rule 1b): an
// operator reads them in an abstention sentence and the tests below match on
// them. The check's own NAME is deliberately absent from this list — it is the
// merged-result half's constant (gendocUnionGateName), referenced rather than
// mirrored (Rule 14: one defect does not get two spellings, and this arm and
// that leg report under the SAME name).
const (
	// mergeCheckAnchorNoEdgeMark states that this tree publishes no generated
	// document, so the edge obliged nothing. It is an ABSTENTION and says which
	// reading produced it, never a silent pass (Rule 15).
	mergeCheckAnchorNoEdgeMark = "no published-anchor edge in this tree"

	// mergeCheckAnchorNoPathsMark states that the branch's own change touches no
	// file the tree can see.
	mergeCheckAnchorNoPathsMark = "the branch's own change touches no path"

	// mergeCheckAnchorBrokenMark introduces the findings of a BROKEN edge — a
	// document whose generator is not in the tree, or an edge no package reads.
	// It is NOT an abstention: an instruction a reader would follow into a
	// command that runs nothing is a defect, and this lane refuses on it.
	mergeCheckAnchorBrokenMark = "the published-anchor edge is BROKEN"

	// mergeCheckAnchorErrorClassMark introduces the refusal of the edge's
	// ERROR class — the defects NO package in the tree can catch (a root document
	// whose generator sits under testdata/ derives no gate package at all, so a
	// line moved under one of its anchors is caught by nobody). It is its own
	// mark because the two facts have different remedies: a broken NON-ERROR edge
	// is "un-break the edge and the gate runs", the ERROR class is "there is no
	// gate to run — somebody has to write one, or retire the class in writing".
	mergeCheckAnchorErrorClassMark = "the published-anchor edge reports an ERROR-class defect no package can catch"

	// mergeCheckAnchorErrorClassRetiredMark is the SAME MEASUREMENT under a
	// RETIRED class, and it is a mark of its own for one reason: a caller reads a
	// mark as a refusal. The fallback below is reached only BECAUSE the skip
	// emptied Errors (its own comment says it reports "the shape the ERROR arm
	// above reports"), so returning the class mark here would refuse on a class
	// this run has retired — the escape hatch with no strength, measured
	// 2026-10-14 on the empty-gate fixture.
	//
	// It deliberately does NOT contain mergeCheckAnchorErrorClassMark: a substring
	// match is exactly how a caller classifies a refusal, so an abstention that
	// carried the mark in prose would be read as one (Rule 15).
	mergeCheckAnchorErrorClassRetiredMark = "the published-anchor edge's ERROR class is retired for this run, " +
		"so this change's anchor movement is reported rather than refused"
)

// finishCleanupBranchAnchorCheck is THE PUBLISHED-ANCHOR ARM of the branch's own
// pre-push check: the packages the branch's change OBLIGES by virtue of the
// documents it publishes anchors into, appended to the same check list the
// smoke selection feeds.
//
// THE DEFECT IT CLOSES (measured 2026-10-13, task 01a13132). A branch commit
// moved cmd/agent/cli_log.go's routing predicate 602 → 611 and committed a
// COMMANDS.md generated block still naming :607 — the document was regenerated
// against a PRE-FINAL state of that very commit. The landing that carried it
// (merge c52dad2d2) is RED: `go test -run TestCommandsDoc ./internal/standards/`
// fails three ways at that revision (the generated-block comparison, the
// anchor-resolution arm, and the red proof's own precondition), measured
// in a checkout of the landing's tree. (Both of those tests were deleted with
// internal/docclass in 5a35f61 — the package survives, those arms do not — so
// the measurement is history; what the branch-red gate reads TODAY is the smoke
// selection below.) The landing's union of paths DID include
// cmd/agent/cli_log.go and the edge DID name internal/standards — so the
// merged-result step had the right answer in hand and still did not catch it:
// it is REPORT-ONLY (Rule 12 — `res.OK` is never touched by a gate), and the
// arm that used to REFUSE here (internal/tools/task_finish_gendoc.go: "a
// non-zero exit REFUSES the delivery") was deleted 73 minutes before that
// landing, by babef3935, when the client's trial merge was retired.
//
// WHAT THIS IS NOT. It never runs the generator (`go run …commands-census.go .`
// REWRITES the document — a repair is the author's act, never a gate's), never
// hand-edits a generated block, and never widens the scope: the packages are
// the edge's own answer for THIS branch's diff, measured the same three-dot way
// the sibling smoke selection is (smoke.TouchedPackages), so another branch's
// commit is never attributed here.
//
// WHY IT IS NOT A NEW DEPENDENCY (Rule 35): there is no new hook, no new
// package, no new command. The edge (internal/gendoc) is a leaf package that
// already exists and is already consumed by the merged-result step; this is one
// more member of the SAME check list the lane already builds and runs, on the
// same ground, through the same runner.
//
// EVERY UN-MEASURABLE CONDITION ABSTAINS WITH ITS OWN SENTENCE (Rule 15) — a
// base that does not resolve, a change with no path, a tree that publishes no
// generated document. A REFUSAL is reserved for what the branch's own revision
// did, and a broken edge is in that class because it is a defect in the tree
// rather than in this measurement's environment.
//
// The returned bool is false when this arm must NOT become a check at all
// (because the arm abstained without a runnable command): the finding is
// returned instead, for the caller to report.
func finishCleanupBranchAnchorCheck(dir, base, branch string) (mergeCheck, []string, string, bool) {
	changed, why := finishCleanupBranchAnchorChangedPaths(dir, base, branch)
	if why != "" {
		return mergeCheck{}, nil, why, false
	}
	if len(changed) == 0 {
		return mergeCheck{}, nil, mergeCheckAnchorNoPathsMark, false
	}
	ans, err := gendoc.GeneratedDocAnswerOpts(dir, changed, gendocErrorClassSkipped())
	if err != nil {
		return mergeCheck{}, nil, mergeCheckAnchorNoEdgeMark + ": " + err.Error(), false
	}
	if !ans.TakesPart() {
		reason := ans.Reason
		if reason == "" {
			reason = gendoc.ParticipationUnknown
		}
		return mergeCheck{}, nil, fmt.Sprintf("%s (%d candidate(s), %d edge(s)): %s",
			mergeCheckAnchorNoEdgeMark, ans.Candidates, ans.Docs, reason), false
	}
	if derr := gendoc.EmptyGateDerivedErrors(ans.Errors); len(derr) > 0 {
		// THE ERROR CLASS REFUSES FIRST, and it is a refusal of a DIFFERENT
		// remedy from the sibling below, so it says its own sentence: an edge
		// defect NO package in this tree can catch is a hole in the guard — the
		// branch's own pre-push check has just measured the tree it is about to
		// deliver, and a class that refused here would otherwise reach master
		// with nothing in its way.
		//
		// The SUB-CLASS is selected, never the whole channel
		// (gendoc.EmptyGateDerivedErrors): another ERROR class riding the same
		// channel would otherwise refuse this lane EARLIER and on a defect whose
		// remedy is different — measured twice, 2026-10-13 and 2026-10-14, each
		// time as the anchor lane refusing on the DECLARATION invariant before
		// the gate the document obliged had run.
		return mergeCheck{}, derr, mergeCheckAnchorErrorClassMark + gendocErrorClassSkipNote(), false
	}
	if len(ans.Findings) > 0 {
		return mergeCheck{}, ans.Findings, mergeCheckAnchorBrokenMark, false
	}
	if len(ans.Required) == 0 {
		// The change obliges nothing: the edge answered and the answer was
		// "no published anchor source was touched" — a measurement, stated with
		// the count it was taken over.
		//
		// UNLESS THE CHANGE DID MOVE AN ANCHOR SOURCE, which is the shape the
		// ERROR arm above reports: the document's anchors were touched and the
		// edge names nothing to run, because that document has no gate package.
		// Reading the count as "touches no published anchor source" would state
		// the opposite of what was measured — and state it as a PASS
		// (Rule 15; measured on the empty-gate fixture, 2026-10-14).
		//
		// AND THE SKIP RETIRES *THIS* ARM TOO, which is the whole point of the
		// branch below. This arm is reachable ONLY once the class arm above has
		// stood down: a tree with a live ERROR class refuses there, so a run that
		// reaches here with Touched set is one whose skip just emptied Errors.
		// Measured 2026-10-14 (task 01a13ae1): with GOPI_GENDOC_ERROR_CLASS_SKIP
		// set, the class arm stood down and THIS arm returned
		// mergeCheckAnchorErrorClassMark — the caller still refused, so the
		// documented escape had no strength on the committing surface.
		//
		// The decision is read from the edge's OWN answer (ans.Skip — the field
		// GeneratedDocAnswerOpts sets when it emptied Errors), never re-read from
		// the environment (Rule 2: the env var has exactly ONE reader, and this
		// arm is not it). A retired class is REPORTED, never silenced: the
		// sentence carries its own mark and says the class is retired, so an
		// operator reads the measurement and the decision together (Rule 15).
		if ans.Touched {
			if ans.Skip {
				return mergeCheck{}, nil, fmt.Sprintf("%s (%d path(s) measured)%s",
					mergeCheckAnchorErrorClassRetiredMark, len(changed), gendocErrorClassSkipNote()), false
			}
			return mergeCheck{}, nil, fmt.Sprintf(
				"%s: this change moves a published anchor source and the edge names no package to run for it "+
					"(%d path(s) measured)", mergeCheckAnchorErrorClassMark, len(changed)), false
		}
		return mergeCheck{}, nil, fmt.Sprintf(
			"%s touches no published anchor source (%d path(s) measured)", gendocUnionGateName, len(changed)), false
	}
	refs := make([]string, 0, len(ans.Required))
	for _, p := range ans.Required {
		// The rendering is the merged-result consumer's own (Rule 14: one
		// spelling of "./<dir>"), reused rather than re-typed.
		refs = append(refs, "."+mergesetGatePackageSep+p)
	}
	return mergeCheck{
		Name:       gendocUnionGateName,
		Cmd:        mergeCheckSmokeCommand(refs),
		TimeoutSec: finishCleanupBranchCheckTimeoutSec,
	}, nil, "", true
}

// finishCleanupBranchAnchorChangedPaths is the change this arm measures: the
// paths THIS branch touched, read the same three-dot way and through the same
// selection the sibling smoke arm uses — so the two halves of the branch's own
// check cannot disagree about what the branch changed, and another branch's
// commit is never attributed here (Rule 14).
//
// A read that did not answer returns its OWN sentence rather than an empty
// path list: an unmeasured change is an abstention, never "nothing to do"
// (Rule 15).
func finishCleanupBranchAnchorChangedPaths(dir, base, branch string) (paths []string, why string) {
	// The GO files the branch touched, through the sibling smoke arm's own
	// primitive: the edge maps a change onto the `file.go:NNN` anchor sources a
	// document publishes, and every one of those is a Go file — so the
	// selection the smoke arm already makes is exactly the change this arm must
	// ask about, and no second diff read is needed.
	touched, err := smoke.ChangedGoFiles(dir, base, branch)
	if err != nil {
		return nil, fmt.Sprintf("%s: the branch's change could not be measured: %s", gendocUnionGateName, err.Error())
	}
	return touched, ""
}
