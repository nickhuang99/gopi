package tools

// rebuild_vet_mergeset.go — rule 2 of devdoc/stale-merge-audit-20260916.md §5:
// the post-merge gate re-run, measured over the MERGED TREE.
//
// WHY A SECOND MEASUREMENT, when rebuild_vet_gate.go's gate already re-runs
// `go vet ./...` after the round. Two gaps, each measured on this repo:
//
//  1. THE TREE, NOT THE MERGED RESULT. rebuild_vet_gate.go measures the
//     merger's own CHECKOUT (its header says so: "SCOPE — the tree the round
//     closes on, never a prepared merge result … no worktree"). That is a
//     measurement of whatever the working directory happens to hold — and a
//     merge commit is not necessarily what a later pass finds there. §5.2 asks
//     for the gates to run on the MERGE COMMIT: "after the merge commit exists".
//     Here the revision under test is materialised in a DETACHED worktree of
//     the merge commit itself (`git worktree add --detach <scratch>/<rev>
//     <mergeRev>`), so the verdict is a fact about the commit and not about a
//     directory. The mechanism is the merge-TIME build gate's, reused: one
//     scratch tree, one checkout, the same cache resolvers, the same reap.
//
//  2. THE GATE SET, NOT ONE COMMAND. §5.2 names the repo's own gates as
//     `go vet ./...` PLUS `go test ./internal/standards/ ./internal/stylegate/
//     ./internal/testutil/`. The first half landed 2026-09-16 (4fdb6684); this
//     file adds the second half — the cheap deterministic gates whose redness
//     `go build` structurally cannot see (a standards/stylegate/testutil
//     assertion is a `_test.go` fact, and `go build` does not compile test
//     trees at all).
//
// SMOKE + BRANCH-SCOPED (Rule 27 / internal/smokegate), and it must stay that
// way: the whole-tree suite is NOT run here. `go vet ./...` compiles the whole
// module's test trees (seconds, no test bodies execute — measured 21.3s warm on
// this host, the sibling gate's own number), and the gate PACKAGES are run only
// when the merge's own diff touched one of them (`git diff --name-only
// <mergeRev>^ <mergeRev>`), which is the branch-scoped rule the smoke gate
// states. A landing that touched none of them pays one vet and nothing else.
//
// NEVER A ROLLBACK (Rule 12), and never silent (Rule 15). A red gate here does
// NOT unwind the merge and does not touch the round's build result. It is
// reported twice, both through channels the close loop already owns:
//
//   - a build-failed-style NOTIFY (VetFailedEvent — the same event the sibling
//     gate and the rebuild failure use) whose summary names the gate AND the
//     package and the position;
//   - a bug ROW (TaskSubmit, tag `bug`) so the defect is a queue item a later
//     run picks up, deduped by summary so repeated landings of one defect do
//     not flood the ledger.
//
// ABSTAIN IS THE DEFAULT (the merger's DETECT-never-block contract): no merge
// revision, no go.mod, no toolchain, no cache, no scratch tree, a timeout, an
// unresolvable module set — each abstains with its reason stated. Nothing about
// the branch is reported red because this host could not measure it.
//
// ZERO new dependency (Rule 10.7): toolkit + stdlib only, and the sharing is
// deliberate rather than duplicated (Rule 14) — the toolchain name, the loop
// argument, the module marker, the three status spellings, the env builder, the
// cache resolvers, the wait delay, the unresolved-modules predicate and the
// budget reader are all the sibling gate's, imported by reference.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	// mergesetGateName is how this step identifies itself in every message. It
	// is deliberately not vetGateName: the two are different measurements (the
	// merged commit's tree vs the checkout), and a reader must be able to tell
	// which one spoke. Rule 1b: compared and reported, so named once.
	mergesetGateName = "mergeset"

	// mergesetGateTestVerb / mergesetGateCountFlag / mergesetGateCountOne build
	// the per-package test leg. `-count=1` because the shared build cache holds
	// test RESULTS as well as compiled packages: without it a run could answer
	// "(cached)" from a previous identical run, which is a recorded historical
	// fact rather than a measurement of this tree.
	mergesetGateTestVerb   = "test"
	mergesetGateCountFlag  = "-count"
	mergesetGateCountOne   = "1"
	mergesetGatePackageSep = "/"

	// mergesetGateFirstParent is how the merge's own diff base is read: the
	// FIRST PARENT of the merge commit is the tip the landing started from, so
	// `<mergeRev>^..<mergeRev>` is exactly what the landing brought in. Read
	// from the commit rather than from a caller-supplied handle, so the step
	// works on every landing face (a manual `gopi task merge` has no handle).
	mergesetGateFirstParent = "^"

	// mergesetGateBaseDirSuffix namespaces the BASE checkout so the two trees
	// this step materialises can never collide in the scratch root (the suffix
	// is a path component, never a revision spelling).
	mergesetGateBaseDirSuffix = "base-"

	// mergesetGatePreexistingReasonFmt states an attribution decision in the
	// gate's own words: the base revision was red here too, so this landing did
	// not introduce it.
	mergesetGatePreexistingReasonFmt = "the %s gate was already RED on the base revision (%s) — this red PRE-EXISTED the landing, so it is not reported as this landing's defect"

	// mergesetGateDiffVerb / mergesetGateNameOnlyFlag are the git spelling of
	// the touched-file read.
	mergesetGateDiffVerb     = "diff"
	mergesetGateNameOnlyFlag = "--name-only"

	// mergesetGateDisabledEnvName names the operator escape hatch (Rule 2: the
	// constant is declared here, the ONE reader is mergesetGateDisabled). An
	// unforeseen false report must be one env var away from gone.
	mergesetGateDisabledEnvName = "GOPI_POSTMERGE_MERGESET_GATE"

	// mergesetGateDisabledValue turns the step off.
	mergesetGateDisabledValue = "0"

	// mergesetGateGoSourceSuffix is the file shape the touched-package map reads
	// (a `_test.go` counts: a test-tree change is exactly what this step exists
	// to catch).
	mergesetGateGoSourceSuffix = ".go"

	// mergesetGateFindingMax bounds the finding lines carried into a bug row's
	// payload (a module-wide red can print thousands; the row names the first
	// positions and the total count is stated beside them).
	mergesetGateFindingMax = 5

	// mergesetGateReasonNoRevision / …NoScratch / …NoDiffBase are the abstention
	// reasons of the resolution steps, each a statement about the MEASUREMENT.
	mergesetGateReasonNoRevision = "no merge revision was supplied for the merged-result measurement"
	mergesetGateReasonNoScratch  = "no usable scratch tree for a merged-result checkout"
	mergesetGateReasonNoDiffBase = "the merge commit's first parent could not be resolved, so the touched-package scope is unknown"

	// mergesetGateTestGateLabel names the second half of the step in the
	// report: the repo's cheap deterministic gates.
	mergesetGateTestGateLabel = "gates"

	// mergesetGateLegStartFmt is the line a leg of this step logs when it
	// STARTS — the same fix, the same reason, and the same shape as the build
	// gate's leg start line (mergeBuildGateLegStartFmt): this step's two legs
	// are the longest in the whole landing path (measured 2026-10-16: the vet
	// leg 267.9s, the gate-packages leg 243.6s, concurrently), and before this
	// line the step produced NOTHING on either stream for 260.8s. A reader
	// cannot distinguish a slow leg from a wedged one in a silent window; the
	// START line with the budget turns the silence into "still working, and
	// for at most this long".
	mergesetGateLegStartFmt = "leg START `%s` (%s, budget %ds)\n"

	// mergesetGateLegStartMarker is the LITERAL prefix this step's start line
	// carries (see mergesetGateLegStartFmt); it exists for the same reason the
	// build gate's does — one definition the test greps for (Rule 1b/13).
	mergesetGateLegStartMarker = "MERGESET gate leg START "

	// The HEAD read the landing face uses to name the revision under test
	// (Rule 1b: the verb and the flags are spellings this file compares).
	mergerHeadRevVerb         = "rev-parse"
	mergesetGateHeadFlag      = "HEAD"
	mergesetGateHeadShortFlag = "--short"
)

// The repo's cheap deterministic gate packages (§5.2). Each is a plain
// directory name; the import-path form the toolchain takes is derived by
// prefixing "./" (mergesetGatePackageRefs). The names live here as constants
// (Rule 1b/13) because they are compared against a diff's file paths and
// rendered into the toolchain's argv.
const (
	mergesetGateStandardsDir = "internal/standards"
	mergesetGateStylegateDir = "internal/stylegate"
	mergesetGateTestutilDir  = "internal/testutil"
)

// mergesetGatePackageDirs is the membership test for a touched path: a file
// under one of these directories puts that gate package in scope. Order is
// stable so the reported package list is reproducible.
var mergesetGatePackageDirs = [...]string{
	mergesetGateStandardsDir,
	mergesetGateStylegateDir,
	mergesetGateTestutilDir,
}

// mergesetGateResult is one gate's verdict inside the merged tree.
type mergesetGateResult struct {
	// Gate is vetGateName or mergesetGateTestGateLabel — the reported failure
	// says which half of the step spoke.
	Gate string
	// Status is PASS | FAIL | ABSTAIN (the build gate's three spellings).
	Status string
	// Reason is the abstention reason ("" unless ABSTAIN).
	Reason string
	// Cmd is the command that ran (the evidence a reader can re-run).
	Cmd string
	// Packages are the gate packages the test leg was scoped to ("" for the vet
	// half, which is module-wide).
	Packages []string
	// Findings are the first positions the toolchain named (capped).
	Findings []vetGateFinding
	// ErrorCount is how many diagnostics were seen in total.
	ErrorCount int
	// BaseWasGreen records that this gate's red was CONFIRMED against a green
	// base revision — i.e. the measurement attributes it to the landing. False
	// means either the base was red too (the red is downgraded to an abstention
	// and never reported) or the base could not be measured (the fail stands, but
	// attribution is unproven). It exists so a reader can tell a confirmed
	// regression from an unverified one.
	BaseWasGreen bool
	// BaseRev names the revision a verdict was measured ON, for the legs whose
	// subject IS a revision other than the landing (the base-documents leg,
	// mergeset_gendoc_base.go). Empty for every leg measured on the landing
	// itself — so a reader can tell "the drift this landing composed" from "the
	// drift this landing found" from the verdict alone, without reading which
	// gate spoke.
	BaseRev string
	// OutputTail is the toolchain's own text when no position could be parsed.
	OutputTail string
	// Elapsed is this gate's wall time.
	Elapsed time.Duration
}

// Failed reports whether this gate measured a red tree (the only condition that
// reports anything).
func (g mergesetGateResult) Failed() bool {
	return g.Status == mergeBuildGateStatusFail
}

// Ref is the first position this gate's failure was seen at, with the
// toolchain's own text as the fallback — never a bare "something failed"
// (Rule 15b).
func (g mergesetGateResult) Ref() string {
	if len(g.Findings) > 0 {
		return g.Findings[0].String()
	}
	if g.OutputTail != "" {
		return g.OutputTail
	}
	return vetGateNoDiagnosticReason
}

// Summary renders one gate's verdict as one line, gate + package + position
// first — the sentence the notify payload and the bug row both carry.
func (g mergesetGateResult) Summary(repo, rev string) string {
	scope := ""
	if len(g.Packages) > 0 {
		scope = " packages=" + strings.Join(g.Packages, ",")
	}
	switch g.Status {
	case mergeBuildGateStatusFail:
		attrib := ""
		if g.BaseWasGreen {
			attrib = mergesetGateAttributedSuffix
		}
		if g.BaseRev != "" {
			attrib = fmt.Sprintf(mergesetGateBaseRevSuffixFmt, g.BaseRev)
		}
		return fmt.Sprintf(mergesetGateFailSummaryFmt, g.Gate, repo, rev, scope, g.Ref(), g.ErrorCount) + attrib
	case mergeBuildGateStatusAbstain:
		return fmt.Sprintf(mergesetGateAbstainSummaryFmt, g.Gate, repo, g.Reason)
	default:
		return fmt.Sprintf(mergesetGatePassSummaryFmt, g.Gate, repo, rev, scope)
	}
}

// The three renderings (Rule 1b: the report's own spellings), each naming the
// gate so a reader of the notify stream knows which measurement spoke.
const (
	mergesetGateFailSummaryFmt    = "post-merge gate RED on the MERGED result: gate=%s repo=%s rev=%s%s at %s (%d finding(s))"
	mergesetGateAbstainSummaryFmt = "post-merge gate ABSTAINED on the merged result: gate=%s repo=%s (%s) — the round stands"
	mergesetGatePassSummaryFmt    = "post-merge gate green on the MERGED result: gate=%s repo=%s rev=%s%s"

	// mergesetGateAttributedSuffix rides a FAIL whose attribution was PROVEN
	// against a green base — a reader can tell a confirmed regression from one
	// whose base could not be measured.
	mergesetGateAttributedSuffix = " [attributed to this landing: the base revision was green]"

	// mergesetGateBaseRevSuffixFmt rides a FAIL measured ON a revision other
	// than the landing (the base-documents leg): the drift PRE-EXISTED, and the
	// verdict names the revision it was found at so the reader can go and look.
	mergesetGateBaseRevSuffixFmt = " [measured on the BASE revision %s: this drift PRE-EXISTED the landing]"

	// mergesetGatePartialPreexistingFmt rides a FAIL that survived the base
	// comparison with findings the base did NOT have: the red as a whole
	// pre-existed, but THESE findings did not — so the row is filed, and its
	// reason names both the base's red it is not reporting and how many of the
	// landing's findings were suppressed as pre-existing.
	mergesetGatePartialPreexistingFmt = "the %s gate was already RED on the base revision (%s), but %d finding(s) reported here are NOT part of that pre-existing red — the FAIL is scoped to those"
)

// RebuildAfterMergeMergeset is the close loop's merged-result step: after the
// declared build has run and a merge commit exists, measure the repo's cheap
// gates OVER THAT COMMIT and report every red one.
//
// It returns the verdicts (nil when the step is disabled or has nothing to
// measure) so a caller can render them; the reporting side effects — the notify
// and the bug row — happen inside, so no caller can forget them.
//
// mergeRev is the merge commit (or any revision) under test; "" abstains. It
// never returns an error: every outcome is a verdict (the merger's
// DETECT-never-block contract), and no caller may be tempted to treat "the
// toolchain answered red" as a harness failure.
func RebuildAfterMergeMergeset(repo, mergeRev string) []mergesetGateResult {
	if strings.TrimSpace(repo) == "" || strings.TrimSpace(mergeRev) == "" {
		// Reported, never silent (Rule 15b): a step that measured nothing must
		// say so, and it is the ONE line the two empty-argument calls share.
		mergesetGateLog("ABSTAINS for %q at %q: %s", repo, mergeRev, mergesetGateReasonNoRevision)
		return nil
	}
	if mergesetGateDisabled() {
		return nil
	}
	spec, reason := mergesetGateResolveSpec(repo)
	if reason != "" {
		// The measurement is not possible at all: reported ONCE (not per gate),
		// so an unmeasurable host is one log line rather than noise.
		mergesetGateLog("ABSTAINS for %s: %s", repo, reason)
		return nil
	}
	results := mergesetGateMeasure(repo, mergeRev, spec)
	for _, res := range results {
		mergesetGateReport(repo, mergeRev, res)
	}
	return results
}

// MergeHeadRev resolves the revision the merged-result measurement runs over:
// the landing face's current tip (the merge commit the round just created).
// "" means the resolution failed — the caller ABSTAINS rather than measuring
// whatever a stale directory holds (the step's whole point is that the revision
// is a fact about the commit, not about a directory).
func MergeHeadRev(repo string) string {
	if strings.TrimSpace(repo) == "" {
		return ""
	}
	out, err := repoSweepGit(repo, mergerHeadRevVerb, mergesetGateHeadShortFlag, mergesetGateHeadFlag)
	if err != nil {
		mergesetGateLog("could not resolve HEAD in %s: %s", repo, mergeBuildGateTail(err.Error()))
		return ""
	}
	return strings.TrimSpace(out)
}

// mergesetGateSpec is the resolved measurement contract for the merged tree:
// the shared caches, the budget, and the scratch root the checkout is made in.
type mergesetGateSpec struct {
	GoCacheDir  string
	ModCacheDir string
	TimeoutSec  int
}

// mergesetGateResolveSpec resolves the contract, or returns the abstention
// reason. Every probe is the sibling gate's (Rule 14): one reader per question in
// this package, never a second spelling of "is this a Go module" or "where is
// the cache".
func mergesetGateResolveSpec(repo string) (mergesetGateSpec, string) {
	if _, err := os.Stat(filepath.Join(repo, vetGateModFile)); err != nil {
		return mergesetGateSpec{}, vetGateNoModuleReason
	}
	if _, err := exec.LookPath(vetGateToolchainBinary); err != nil {
		return mergesetGateSpec{}, fmt.Sprintf("no %s toolchain on this host: %v", vetGateToolchainBinary, err)
	}
	cacheDir, ok := mergeBuildGateGoCache(repo)
	if !ok {
		return mergesetGateSpec{}, "no usable shared Go build cache — the reason is logged above"
	}
	modCacheDir, ok := mergeBuildGateSharedModCache()
	if !ok {
		return mergesetGateSpec{}, "no populated shared module cache — the reason is logged above"
	}
	// ONE budget reader for the whole close loop (Rule 2): the merged-result
	// step measures the same kind of work as the checkout step (a bounded
	// toolchain run over this module), so it resolves the SAME number rather
	// than inventing a second knob an operator would have to find.
	timeout, err := vetGateTimeout()
	if err != nil {
		return mergesetGateSpec{}, err.Error()
	}
	return mergesetGateSpec{GoCacheDir: cacheDir, ModCacheDir: modCacheDir, TimeoutSec: timeout}, ""
}

// mergesetGateRun is one gate's execution inside the prepared checkout.
type mergesetGateRun struct {
	Gate     string
	Argv     []string
	Packages []string
}

// mergesetGateMeasure materialises mergeRev in a detached scratch worktree and
// runs the gate set in it, removing the checkout before it returns.
//
// The worktree — not the merger's own directory — is what makes this a
// measurement of the COMMIT (see the file header). It is the build gate's own
// scratch class, checkout naming and reap, so a crashed run leaves the residue
// the sweeper already knows how to take.
func mergesetGateMeasure(repo, mergeRev string, spec mergesetGateSpec) []mergesetGateResult {
	workRoot, cleanup, ok := mergeBuildGateScratch(repo)
	if !ok {
		mergesetGateLog("ABSTAINS for %s at %s: %s", repo, mergeRev, mergesetGateReasonNoScratch)
		return nil
	}
	defer cleanup()
	defer mergeBuildGateReapRegistrations(repo, workRoot)

	dir := filepath.Join(workRoot, mergeBuildGateWorktreeDirName(mergeRev))
	// FRESH PATH (worktree_fresh_path.go): the name is DERIVED from the rev, so
	// two measurements of the same rev collide on one path — and on a REUSED
	// workRoot (a crashed run's leftovers under the same scratch class) the
	// directory can survive. git would reuse it silently when it is empty and
	// the second measurement would then read a tree this run did not create.
	if err := worktreeFreshPath(dir); err != nil {
		mergesetGateLog("ABSTAINS for %s at %s (%v)", repo, mergeRev, err)
		return nil
	}
	if _, err := repoSweepGit(repo, mergeBuildGateWorktreeVerb, mergeBuildGateWorktreeAddVerb,
		mergeBuildGateWorktreeDetachFlag, dir, mergeRev); err != nil {
		mergesetGateLog("ABSTAINS for %s at %s (worktree add --detach failed: %s)",
			repo, mergeRev, mergeBuildGateTail(err.Error()))
		return nil
	}

	runs := []mergesetGateRun{{
		Gate: vetGateName,
		Argv: []string{vetGateToolchainBinary, vetGateToolchainVerb, vetGateLoopArgument},
	}}
	// The scoped test leg, decided from the MERGE'S OWN DIFF so the scope is a
	// fact about the landing rather than a caller's claim. An unresolvable diff
	// base is NOT silently widened to the whole tree (that is the prescription
	// Rule 27 retired) — it abstains with its reason.
	switch pkgs, reason := mergesetGateTouchedGatePackages(repo, mergeRev); {
	case reason != "":
		mergesetGateLog("ABSTAINS for %s at %s (the gates leg): %s", repo, mergeRev, reason)
	case len(pkgs) > 0:
		runs = append(runs, mergesetGateRun{
			Gate:     mergesetGateTestGateLabel,
			Argv:     append(mergesetGateTestArgv(), pkgs...),
			Packages: pkgs,
		})
	}
	// THE UNION LEG (mergeset_gendoc.go): the package-scope map above reads the
	// LANDING'S diff and knows the repo's fixed gate directories; a published
	// document of record can be blocked on by ANY package the tree names, and a
	// landing composes two branches whose own diffs were each measured (and
	// passed) SEPARATELY. So the same question is asked of the union, through
	// the SAME edge discovery the delivery point already consumes, and whatever
	// packages it names join the run set.
	//
	// It is asked HERE, inside the merged-result step, and not by a hook of its
	// own (Rule 3/35): this measurement already runs on every landing and its
	// revision already IS the union, so the union half is one more member in a
	// membership test rather than a new dependency.
	if unionRuns, note := mergesetGendocRuns(context.Background(), repo, mergeRev, dir); note != "" {
		mergesetGateLog("ABSTAINS for %s at %s (the published-anchors leg): %s", repo, mergeRev, note)
	} else {
		runs = append(runs, unionRuns...)
	}
	// THE DEVDOC-ANCHOR LEG USED TO STAND HERE and is gone with its corpus. It
	// re-ran the three anchor cells' gates (anchor-NUMBER, prose-anchor,
	// prose-path-line) over the union, because those gates lived in a package
	// that was in neither of this step's other sources. All three gates, their
	// baselines and internal/docclass itself were deleted with the document
	// pipeline (removal slice 3/5), so the leg has no run set left to add: the
	// union is still measured by the published-anchors and whole-repo legs
	// below, and nothing here silently claims to have measured a corpus that no
	// longer exists.
	//
	// THE WHOLE-REPO-RATCHET LEG (mergeset_stylegate.go): internal/stylegate's
	// ceiling-0 invariant is the ONE member of this step's run set whose scope is
	// not a directory — the suite scans the whole module, so a hit introduced
	// anywhere moves it. The gate-package map above knows only the fixed gate
	// directories, so a landing that adds a `time.Sleep` to a test in
	// cmd/agent (or any other package) never asked it; the ratchet then arrived
	// silently and master went red for the next person to run the suite. The
	// trigger here is therefore the landing touching Go SOURCE at all.
	if sgRuns, note := mergesetStylegateRuns(repo, mergeRev, dir); note != "" {
		mergesetGateLog("ABSTAINS for %s at %s (the whole-repo-ratchet leg): %s", repo, mergeRev, note)
	} else {
		runs = append(runs, sgRuns...)
	}
	// THE CMD/AGENT-RATCHET LEG (mergeset_cmdagent.go): the population this step
	// had NO source for. The gate-package map knows three directories, and the
	// whole-repo leg measures ONE invariant (internal/stylegate); the tests whose
	// SUBJECT is a file another landing owns — the devdoc corpus's class banners,
	// the man pages against the binary's own -h output, the whole-module
	// retired-verb scan — live in cmd/agent and were asked by nothing here, so
	// their red arrived on master unmeasured. The trigger is the landing touching
	// Go source or one of the two document trees those projections read, and the
	// run is a NAMED roster, never the package's whole suite (Rule 27).
	if caRuns, note := mergesetCmdAgentRuns(repo, mergeRev, dir); note != "" {
		mergesetGateLog("ABSTAINS for %s at %s (the cmd/agent-ratchet leg): %s", repo, mergeRev, note)
	} else {
		runs = append(runs, caRuns...)
	}

	out := make([]mergesetGateResult, 0, len(runs))
	// The BASE tree is measured too, when the merge's first parent resolves: a
	// red the base revision ALREADY had is not this landing's defect, and
	// reporting it as one would fire on every landing forever — an alarm nobody
	// can clear, which is the same as no alarm. Measured 2026-09-18: master's
	// own `internal/testutil` was red (`TestScriptsAreBoundedAndOffTmp`), and
	// without attribution every subsequent merge would have filed a bug row
	// naming a defect it did not introduce.
	//
	// ONLY a red base can absorb the reading — a green base leaves every finding
	// attributed to the landing, and an unmeasurable base leaves it attributed
	// too (the conservative direction: never silently excuse a finding).
	var base []mergesetGateResult
	var baseDocDir, baseDocRev string
	// baseDocDir / baseDocRev are the SAME base revision the attribution arm
	// uses, resolved ONCE and shared: the base-documents leg measures this
	// checkout's document corpus, so resolving the revision twice would be two
	// answers to one question (Rule 2). Empty means the leg abstains — never a
	// silent skip.
	if baseRev, ok := mergesetGateBaseRev(repo, mergeRev); ok {
		baseDocRev = baseRev
		baseDir := filepath.Join(workRoot, mergeBuildGateWorktreeDirName(mergesetGateBaseDirSuffix+baseRev))
		// FRESH PATH (worktree_fresh_path.go): a reused base checkout would
		// make every finding's attribution read another run's tree. An
		// unfresh base REFUSES THE BASE ARM ONLY (the conservative direction
		// this block already documents): every finding is then attributed to
		// the landing, never silently excused.
		if ferr := worktreeFreshPath(baseDir); ferr != nil {
			mergesetGateLog("base revision %s could not be checked out (%v) — every finding is attributed to the landing",
				baseRev, ferr)
		} else if _, err := repoSweepGit(repo, mergeBuildGateWorktreeVerb, mergeBuildGateWorktreeAddVerb,
			mergeBuildGateWorktreeDetachFlag, baseDir, baseRev); err == nil {
			baseDocDir = baseDir
			base = make([]mergesetGateResult, 0, len(runs))
			for _, run := range runs {
				base = append(base, mergesetGateRunOne(baseDir, run, spec))
			}
		} else {
			mergesetGateLog("base revision %s could not be checked out (%s) — every finding is attributed to the landing",
				baseRev, mergeBuildGateTail(err.Error()))
		}
	}

	// THE BASE-DOCUMENTS LEG (mergeset_gendoc_base.go): the pre-existing half of
	// the published-anchor edge. It rides the checkout the attribution arm above
	// already materialised — the SAME revision, the SAME worktree, one more
	// member in the run set over it (Rule 3/35: no new process, no new tick, no
	// new script). Its verdict is NEVER attributed: a drift measured on the base
	// revision is by definition not this landing's, and `baseDocRuns` is kept
	// OUT of `runs` so the base arm cannot weight its own reading.
	var baseDocRuns []mergesetGateResult
	if baseDocDir != "" {
		if baseRuns, note := mergesetGendocBaseRuns(repo, mergeRev, baseDocDir); note != "" {
			mergesetGateLog("ABSTAINS for %s at %s (the base-documents leg): %s", repo, mergeRev, note)
		} else {
			for _, run := range baseRuns {
				baseDocRuns = append(baseDocRuns, mergesetGateRunOne(baseDocDir, run, spec))
			}
		}
	} else {
		mergesetGateLog("ABSTAINS for %s at %s (the base-documents leg): %s", repo, mergeRev, mergesetGateReasonNoDiffBase)
	}

	for _, run := range runs {
		res := mergesetGateRunOne(dir, run, spec)
		mergesetGateAttribute(&res, base)
		out = append(out, res)
	}
	// The base leg's verdicts are REPORTED, never attributed — and each carries
	// its own revision in the summary, so a reader cannot mistake a drift the
	// landing FOUND for one it composed.
	for i := range baseDocRuns {
		baseDocRuns[i].BaseRev = baseDocRev
	}
	return append(out, baseDocRuns...)
}

// mergesetGateBaseRev resolves the revision the landing started from — the
// merge commit's FIRST PARENT. !ok means it cannot be resolved, and the caller
// then attributes every finding to the landing.
func mergesetGateBaseRev(repo, mergeRev string) (string, bool) {
	out, err := repoSweepGit(repo, mergerHeadRevVerb, mergeRev+mergesetGateFirstParent)
	if err != nil {
		return "", false
	}
	rev := strings.TrimSpace(out)
	return rev, rev != ""
}

// mergesetGateAttribute decides whether a gate's red belongs to THE LANDING, by
// comparing it against the same gate measured on the base revision:
//
//   - base RED too → the red PRE-EXISTED. It is not this landing's defect, so it
//     is not reported as one: the verdict is downgraded to an ABSTAIN carrying
//     the attribution, which the log states and nothing else acts on.
//   - base GREEN → the landing introduced it. The FAIL stands, with the
//     comparison recorded as the evidence (which is what makes a report
//     actionable rather than an assertion).
//   - base not measured at all → the FAIL stands untouched (the conservative
//     direction).
//
// The comparison is deliberately on the gate's own verdict, not on a finding
// diff: `internal/testutil`'s shared-source gates assert on the WHOLE tree, so a
// landing elsewhere shifts their findings without introducing anything — a
// finding-level diff would report those shifts as new defects, exactly the
// false-alarm mode the 2026-09-16 standards rekey incident produced.
func mergesetGateAttribute(res *mergesetGateResult, base []mergesetGateResult) {
	if res == nil || !res.Failed() {
		return
	}
	for _, b := range base {
		if b.Gate != res.Gate {
			continue
		}
		if !b.Failed() {
			res.BaseWasGreen = true
			return
		}
		// The base was red too. WHICH PART of the red pre-existed is a second
		// question, and answering it with the gate's verdict alone is what lost
		// the 2026-10-13 detection: the docclass leg's findings are PER-ROW (one
		// manifest row ↔ one anchor pair), so a landing that ADDS a drifted row
		// to a gate the base already had red disappeared with the base's red.
		//
		// The split is attempted FIRST, and it is deliberately the narrow one:
		// it needs BOTH sides to carry findings AND every base finding to appear
		// in the landing's by MESSAGE (the anchor's own identity — the row line
		// in the citing document moves with the manifest, the message does not).
		// A gate whose findings shift between the two trees (the whole-tree
		// shared-source gates the paragraph above names) fails that subset test
		// and keeps the conservative whole-verdict downgrade, so this cannot
		// manufacture new-defect noise from a shift.
		if fresh, suppressed, ok := mergesetGateFreshFindings(res.Findings, b.Findings); ok {
			if len(fresh) == 0 {
				res.Status = mergeBuildGateStatusAbstain
				res.Reason = fmt.Sprintf(mergesetGatePreexistingReasonFmt, res.Gate, b.Ref())
				res.OutputTail = ""
				res.Findings = nil
				return
			}
			// The landing introduced findings the base did not have: the FAIL
			// stands, scoped to exactly those, and the reason names the
			// pre-existing ones it is NOT reporting.
			res.Findings = fresh
			res.ErrorCount = len(fresh)
			res.Reason = fmt.Sprintf(mergesetGatePartialPreexistingFmt, res.Gate, b.Ref(), len(suppressed))
			res.OutputTail = ""
			return
		}
		// Pre-existing, and not separable: downgraded, reason said in full.
		was := b.Ref()
		res.Status = mergeBuildGateStatusAbstain
		res.Reason = fmt.Sprintf(mergesetGatePreexistingReasonFmt, res.Gate, was)
		res.OutputTail = ""
		return
	}
}

// mergesetGateFreshFindings splits a red landing's findings against the SAME
// gate's findings on the base revision, and reports whether the split is
// ADMISSIBLE at all.
//
// It returns ok=false — "do not split, fall back to the whole-verdict
// comparison" — unless both sides carry findings AND every base finding appears
// in the landing's. The subset test is what makes the split safe: a gate whose
// findings are re-derived between two trees (a count, a whole-tree assertion, a
// line that moved with the code) will not have its base findings verbatim in
// the landing, so it is NOT split and the conservative downgrade applies
// unchanged. Identity is by MESSAGE, not by the full position: the position's
// citing-document line is the MANIFEST's own line number and moves when the
// manifest moves (measured 2026-10-13: the same anchor drift reported at
// :1223 on the base and :1224 on the landing), while the message carries the
// anchor's own identity — which file, which recorded number, which text.
func mergesetGateFreshFindings(landing, base []vetGateFinding) (fresh, suppressed []vetGateFinding, ok bool) {
	if len(landing) == 0 || len(base) == 0 {
		return nil, nil, false
	}
	baseMessages := make(map[string]bool, len(base))
	for _, b := range base {
		baseMessages[b.Message] = true
	}
	for _, b := range base {
		found := false
		for _, l := range landing {
			if l.Message == b.Message {
				found = true
				break
			}
		}
		if !found {
			// The base's red is not a subset of the landing's: the two
			// verdicts are not comparable finding-by-finding.
			return nil, nil, false
		}
	}
	for _, l := range landing {
		if baseMessages[l.Message] {
			suppressed = append(suppressed, l)
			continue
		}
		fresh = append(fresh, l)
	}
	return fresh, suppressed, true
}

// mergesetGateTestArgv is the prefix of the scoped gate-package run: the
// toolchain, the test verb and the cache-defeating `-count=1`.
func mergesetGateTestArgv() []string {
	return []string{vetGateToolchainBinary, mergesetGateTestVerb, mergesetGateCountFlag, mergesetGateCountOne}
}

// mergesetGateRunOne runs one gate's argv in dir and shapes the outcome as a
// verdict. The execution rules (the env builder, the process group, the wait
// delay, the timeout-is-not-a-red-tree rule, the unresolved-module abstention)
// are the sibling gate's, so the two halves of the close loop cannot disagree
// about what a failure IS.
func mergesetGateRunOne(dir string, run mergesetGateRun, spec mergesetGateSpec) mergesetGateResult {
	res := mergesetGateResult{Gate: run.Gate, Packages: run.Packages,
		Cmd: strings.Join(run.Argv, " "), Status: mergeBuildGateStatusAbstain}
	mergesetGateLog(mergesetGateLegStartFmt, res.Cmd, run.Gate, spec.TimeoutSec)
	out := mergesetGateExecute(dir, run.Argv, spec)
	res.Elapsed = out.Elapsed
	if out.Abstain != "" {
		res.Reason = out.Abstain
		return res
	}
	combined := vetGateOutput(out.Stdout, out.Stderr)
	res.Findings, res.ErrorCount = vetGateParseFindings(out.Stdout, out.Stderr)
	if strings.Contains(combined, vetGateNoPackagesMarker) {
		res.Reason = vetGateNoPackagesReason
		return res
	}
	if out.Err == nil && res.ErrorCount == 0 {
		// Silence is the pass for a toolchain gate: `go test` prints nothing to
		// fail with and `go vet` prints nothing at all on a clean tree
		// (measured). A zero exit with no diagnostic IS the measurement.
		res.Status = mergeBuildGateStatusPass
		return res
	}
	res.Status = mergeBuildGateStatusFail
	if len(res.Findings) == 0 {
		// A failure shape the parser does not know (a test assertion panic, a
		// signal, an unfamiliar diagnostic spelling) is still REPORTED, by name
		// with the output tail as evidence — never as a bare "it failed".
		res.OutputTail = outputTail(combined)
	}
	return res
}

// mergesetGateExecute runs argv in dir under the spec's bounds, with the shared
// caches and the process-group teardown. A non-zero exit is the measurement (a
// red tree); a timeout or a start failure is an ABSTAIN.
func mergesetGateExecute(dir string, argv []string, spec mergesetGateSpec) vetGateOutcome {
	var ctx context.Context
	var cancel context.CancelFunc
	if spec.TimeoutSec > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(spec.TimeoutSec)*time.Second)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = mergeBuildGateEnv(spec.GoCacheDir, spec.ModCacheDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = mergeBuildGateWaitDelay
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := vetGateOutcome{Stdout: stdout.String(), Stderr: stderr.String(), Err: err, Elapsed: time.Since(start)}

	if ctx.Err() != nil && cmd.Process != nil {
		// The group id is RE-VERIFIED before delivery: it is this child's pid, and the
		// kernel may have recycled that number to an unrelated group (see killGroupGuard.go).
		_, _ = signalProcessGroup(cmd.Process.Pid, cmd.Process.Pid, syscall.SIGKILL) //stylegate:ignore signal to a process group we are already abandoning; ESRCH (the group already gone) is the expected error
	}
	if ctx.Err() == context.DeadlineExceeded {
		out.TimedOut = true
		out.Abstain = fmt.Sprintf("the gate exceeded its %ds budget (a timeout is not a red tree)", spec.TimeoutSec)
		return out
	}
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			out.Abstain = fmt.Sprintf("the gate could not be started: %v", err)
			return out
		}
		if mergeBuildGateUnresolvedModules(vetGateOutput(out.Stdout, out.Stderr)) {
			out.Abstain = vetGateUnresolvedReason
			return out
		}
		// The mergeset legs LINK test binaries, so a full filesystem kills them
		// with no compiler diagnostic — the same class the build and union
		// gates already abstain on, through the same predicate and the same
		// dual-partition reason (Rule 14: one definition).
		if marker, yes := mergeBuildGateDiskExhausted(vetGateOutput(out.Stdout, out.Stderr)); yes {
			out.Abstain = mergeBuildGateDiskExhaustedErr(marker, dir)
			return out
		}
	}
	return out
}

// mergesetGateTouchedGatePackages maps the merge's own diff to the gate packages
// it touched, returned as toolchain package refs ("./internal/standards").
// reason is non-empty when the scope cannot be resolved at all — an abstention,
// never a silent widening to the whole tree.
func mergesetGateTouchedGatePackages(repo, mergeRev string) (refs []string, reason string) {
	base := mergeRev + mergesetGateFirstParent
	out, err := repoSweepGit(repo, mergesetGateDiffVerb, mergesetGateNameOnlyFlag, base, mergeRev)
	if err != nil {
		return nil, fmt.Sprintf("%s (%s)", mergesetGateReasonNoDiffBase, mergeBuildGateTail(err.Error()))
	}
	touched := make(map[string]bool, len(mergesetGatePackageDirs))
	for _, line := range strings.Split(out, "\n") {
		p := strings.TrimSpace(line)
		if !strings.HasSuffix(p, mergesetGateGoSourceSuffix) {
			continue
		}
		for _, gateDir := range mergesetGatePackageDirs {
			if strings.HasPrefix(p, gateDir+mergesetGatePackageSep) {
				touched[gateDir] = true
			}
		}
	}
	// Declaration order, so the reported package list is reproducible.
	for _, gateDir := range mergesetGatePackageDirs {
		if touched[gateDir] {
			refs = append(refs, "."+mergesetGatePackageSep+gateDir)
		}
	}
	return refs, ""
}

// mergesetGateReport is the ONE place a verdict becomes visible: a red gate is
// notified AND filed as a bug row, everything else is logged. Nothing is
// swallowed (Rule 15b) — an abstention says why, a pass says it passed.
func mergesetGateReport(repo, rev string, res mergesetGateResult) {
	if res.Failed() {
		summary := res.Summary(repo, rev)
		mergesetGateLog("RED: %s", summary)
		// The close loop's OWN channel: the same synthetic event the rebuild
		// failure and the sibling gate use, so a subscribed operator hears this
		// without a reconfiguration (VetFailedEvent == BuildFailedEvent).
		NotifySyntheticEvent(VetFailedEvent, repo, "", summary)
		mergesetGateFileBugRow(repo, rev, res)
		return
	}
	mergesetGateLog("verdict: %s", res.Summary(repo, rev))
}

// mergesetGateFileBugRow files a red merged-result gate as a `bug` ROW, so the
// defect is a queue item a later run picks up rather than a line in a log
// nobody re-reads (§5.2's "or file a bug row").
//
// DEDUP: the summary is the DEFECT's identity (repo + gate + position), not the
// landing's. A red that survives several landings is ONE row — including the
// position means the identity is stable across the revisions that share it,
// which is what keeps a red-master window from flooding the ledger with one row
// per landing. The revision, the command and the findings ride the PAYLOAD, so
// the row still points a reader at the latest evidence.
//
// A failure to file is REPORTED, never swallowed (Rule 15b): the merge is
// already landed and the notify has already fired, so the row is the durable
// half — an operator has to know when it did not arrive.
func mergesetGateFileBugRow(repo, rev string, res mergesetGateResult) {
	summary := mergesetGateBugSummary(repo, res)
	live, err := reworkSummaryLive(summary)
	if err != nil {
		mergesetGateLog("bug row NOT filed for %s (dedup probe failed: %v) — the notify above is the only record", repo, err)
		return
	}
	if live {
		mergesetGateLog("bug row already open for this defect — not filing a second one (%s)", summary)
		return
	}
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: summary,
		Payload: fmt.Sprintf(mergesetGateBugPayloadFmt,
			repo, rev, res.Gate, res.Cmd, strings.Join(res.Packages, " "), res.Ref(),
			res.ErrorCount, mergesetGateFindingsText(res)),
		Tags:   bugTag,
		Source: taskMergeSourceAuto,
		Repo:   repo,
	}); err != nil {
		mergesetGateLog("bug row NOT filed for %s (%v) — the notify above is the only record", repo, err)
		return
	}
	// The locus is not decoration (Rule 15): this line is the ONLY record a
	// reader of a merger log has, and a plain "filed" cannot be told apart from
	// "filed into a ledger you cannot see" — the ordinary case under the test
	// ledger pin (Rule 32).
	mergesetGateLog("filed bug row: %s %s", summary, bugRowLedgerLocus())
}

// mergesetGateBugSummary is the bug row's identity AND its headline, in ONE
// function so the dedup probe and the submission can never disagree (Rule 2).
//
// THE BOUND IS NOT COSMETIC (measured 2026-09-18, this step's own first test
// run): a summary longer than taskSummaryMaxRunes is TRUNCATED by the store
// (sanitizeTaskSummary → textguard), so a dedup probe built from the unbounded
// text searched for a string that was never stored — the probe never matched and
// every measurement of the same defect filed ANOTHER row. The identity is
// therefore cut HERE, once, by mergesetGateSummaryIdentity — to exactly what the
// store keeps, TRAILING WHITESPACE INCLUDED (the store's own TrimSpace; measured
// 2026-10-16, without it the value differed by one space whenever the cut landed
// between two words); the full text still reaches a reader through the payload
// and the log line (this is the row's HEADLINE, not its evidence).
//
// The identity is DEFECT-scoped: repo + gate + position, never a revision — so
// the same red measured on several landings is one row, and a stable prefix of
// the same three parts is still the same key.
func mergesetGateBugSummary(repo string, res mergesetGateResult) string {
	full := fmt.Sprintf(mergesetGateBugSummaryFmt, res.Gate) + mergesetGateSummarySep +
		repo + " " + res.Ref()
	return mergesetGateSummaryIdentity(full, taskSummaryMaxRunes)
}

// mergesetGateSummarySep is the separator the headline uses (Rule 1b).
const mergesetGateSummarySep = " — "

// mergesetGateSummaryIdentity reproduces the STORE's summary value for a
// headline this lane builds: cut to at most max RUNES — the unit the ledger's
// own ceiling counts (taskSummaryMaxRunes), and a boundary that cannot split a
// multi-byte character (a repo path may carry one) — and then TrimSpace, the
// same fold the store applies on the way in (sanitizeTaskSummary →
// textguard.Sanitize, and insertTaskRow's own strings.TrimSpace).
//
// THE TRIM IS NOT COSMETIC, and the ORDER is part of it. The cut lands wherever
// the max-th rune falls, which for a headline ending in an interpolated path can
// be an inter-word SPACE: measured (200-rune ceiling, repo
// strings.Repeat("a", 146) + " " + a long tail) the cut left a TRAILING SPACE
// that the store had already dropped — 200 runes probed against 199 stored — so
// a reader that trusted the old comment's "exactly what the store keeps" and
// compared byte-for-byte saw an INTERMITTENT mismatch that depended on the repo
// path's length. Trimming BEFORE the cut would spend the ceiling on whitespace
// and still not match the store, which is why the cut comes first.
func mergesetGateSummaryIdentity(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		s = string(r[:max])
	}
	return strings.TrimSpace(s)
}

// mergesetGateFindingsText renders the capped findings for the bug row's payload
// (one position per line; a red with none retained says so, so the payload is
// never an empty section).
func mergesetGateFindingsText(res mergesetGateResult) string {
	if len(res.Findings) == 0 {
		return res.Ref()
	}
	var b strings.Builder
	for i, f := range res.Findings {
		if i >= mergesetGateFindingMax {
			break
		}
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(f.String())
	}
	return b.String()
}

// The bug row's own spellings (Rule 1b). The payload states the Rule 12
// constraint where the fixer will read it — the merge is NOT unwound, and the
// fix is a fresh branch (Rule 23), never an edit on the landed commit.
const (
	mergesetGateBugSummaryFmt = "bug: post-merge gate RED on the merged result: %s"
	mergesetGateBugPayloadFmt = `The post-merge gate re-run measured the MERGED tree of %s at %s and found it RED.

Gate:     %s
Command:  %s
Packages: %s
Position: %s
Findings: %d reported (first ones below)

%s

This row is the durable record of a red that the close loop REPORTS and does
NOT unwind (GOPI.md Rule 12: a merge is never rolled back). The measurement ran
over a detached worktree of the merge commit, so the repository the merger
works in was not modified.

What to do: fix the tree on a FRESH branch from master (Rule 23 — never continue
on the landed commit), and re-run the same command to confirm. The gate re-runs
by itself on the next landing, so a second row for the same position is not
expected (this one is deduped by its summary).
`
)

// mergesetGateDisabled reports whether the operator escape hatch is set. Rule 2:
// this is the ONE reader of mergesetGateDisabledEnvName; unset (or any other
// value) means ENABLED.
func mergesetGateDisabled() bool {
	return strings.TrimSpace(os.Getenv(mergesetGateDisabledEnvName)) == mergesetGateDisabledValue
}

// mergesetGateLog renders one line with the step's own greppable prefix, so an
// operator reading the merger log sees WHICH measurement produced it.
func mergesetGateLog(format string, args ...any) {
	taskNotifyLog("merger: MERGESET gate "+format, args...)
}
