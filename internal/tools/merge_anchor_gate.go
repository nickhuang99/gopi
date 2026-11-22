package tools

// merge_anchor_gate.go — the MERGE-TIME PUBLISHED-ANCHOR gate (⓪e): the union
// of two revisions is asked the ONE question internal/gendoc exists to answer —
// does the document of record still publish anchors the tree resolves? — and a
// union whose document lags its anchor sources is REFUSED before `git merge`
// runs.
//
// THE INCIDENT IT CLOSES. A branch commit (3592f4498) moved
// `cmd/agent/cli_log.go`'s routing predicate 602 → 611, regenerated COMMANDS.md
// against a PRE-FINAL state of that same commit and committed a block naming
// `:607`. The landing that carried it (merge c52dad2d2, 00:57:04) is RED at its
// own revision — measured in a checkout of that merge's own tree:
//
//	git show c52dad2d2:COMMANDS.md | grep cli_log.go      →  cli_log.go:607
//	git show c52dad2d2:cmd/agent/cli_log.go | sed -n 611p →  func isLogSubcommand(
//
// The measurement that could have answered it EXISTED and ran on every landing
// — `RebuildAfterMergeMergeset` → mergeset_gendoc.go, whose union contained
// cli_log.go and whose edge answered internal/standards — but its contract is
// REPORT-ONLY (Rule 12: `res.OK` is never touched by a gate), so the answer
// arrived as a log line after the document was already on the integration
// branch. The arm that used to REFUSE at the delivery point
// (task_finish_gendoc.go: "a non-zero exit REFUSES the delivery") was deleted 73
// minutes earlier by babef3935, when the client's trial merge was retired. The
// drift left through a hole both halves had measured and neither had acted on.
//
// WHAT THIS LANE REPAIRS — and what it deliberately does not. Not the delivery
// point that lost its arm (its server-side justification is gone), and not the
// branch's own pre-push check (a SIBLING row owns that half: 84627a646,
// `finishCleanupBranchAnchorCheck`). It repairs the seam where the drift is a
// statement about a tree NOBODY has checked out — the two revisions' ANSWER
// TOGETHER. A branch can be internally consistent (document and source in step
// on its own tip) while its union with master is not, because master
// regenerated the same document separately. Measured on this repo 2026-10-12
// (the master before this ground's, three commits 03f2d1dfe 07:40, 93e402d36
// 13:28, 721268925 16:18): each moved `cmd/agent/cli_doccheck.go`'s predicate on
// its OWN branch and regenerated COMMANDS.md on its OWN branch, each branch was
// green alone, and the master they were merged into carried
// `cli_doccheck.go:161` in the document and `:194` in the source.
//
// WHY HERE AND NOT A NEW HOOK (Rules 3/35 — no new tool, no new script, no
// resident dependency). The union tree is ALREADY materialised on this path by
// the ⓪c lane's own builder (`mergeUnionGateTree`: `read-tree -m` in an
// ALTERNATE index + `write-tree`, then `git archive | tar -x` into scratch — no
// worktree, no checkout, the repo's own index and working tree never touched),
// and the edge (internal/gendoc) is a leaf package this repo already consumes.
// This is one more member of the gate sequence that already runs before every
// landing, reading the SAME materialised tree through the SAME archive pipe.
// Nothing ticks, and nothing new has to be remembered.
//
// THE COMPARISON IS MEASURED, NEVER ASSUMED — the discipline ⓪b/⓪c follow, and
// here it is what keeps the lane from freezing a queue for someone else's red
// tree. The UNION is measured first; only when it is red does the gate measure
// the TARGET's own tree and the BRANCH's own tree, because a target that does
// not PASS the same measurement has established nothing about this candidate.
// That is not hypothetical: measured 2026-10-13, the integration ref this lane
// was developed against is ALREADY red — `TestCommandsDocGeneratedBlockMatchesSource`
// names `cli_scratch_gc.go:161` vs `:614` and the gate's own red proof reports
// "precondition: the committed document's block is already stale". Without the
// premise legs the FIRST branch offered after such a red state would be refused
// for it.
//
// ABSTAIN IS THE DEFAULT (the merger's DETECT-never-block contract): the
// operator escape hatch, no edge in the tree, no anchor source in the union's
// change, no compile contract, an unusable scratch tree, an unmerged union, a
// gate package this union cannot run, a closure that does not COMPILE, a
// premise leg that did not PASS, or a failure the probe cannot attribute to a
// failing test. Exactly ONE shape refuses: the edge's document tests fail on
// the union, PASS on the target and PASS on the branch alone.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nickhuang99/gopi/internal/cliindex"
	"github.com/nickhuang99/gopi/internal/gendoc"
	"github.com/nickhuang99/gopi/internal/scratch"
)

const (
	// mergeAnchorGateEnvName names the operator escape hatch (Rule 2: the
	// default below is the constant, and this is the ONE reader).
	mergeAnchorGateEnvName = "GOPI_MERGE_ANCHOR_GATE"

	// mergeAnchorGateDisabledValue turns the gate off — an unforeseen
	// false-positive must always be one env var away from gone.
	mergeAnchorGateDisabledValue = "0"

	// mergeAnchorGateTimeoutEnvName overrides the per-command budget: a slow
	// compile group must never hold the merge queue hostage (Rule 2: ONE reader).
	mergeAnchorGateTimeoutEnvName = "GOPI_MERGE_ANCHOR_GATE_TIMEOUT_SEC"

	// MergeAnchorGateName is how the merger log and the refusal report name this
	// lane (Rule 1b: the spelling lives once, beside the gate). It is DISTINCT
	// from the merged-result consumer's own name (gendocUnionGateName,
	// mergeset_gendoc.go): both lanes ask the same edge, and an operator reading
	// a refusal must be able to tell WHICH measurement refused.
	MergeAnchorGateName = "PUBLISHED-ANCHOR gate"

	// mergeAnchorGateScratchPattern names the gate's per-call scratch inside the
	// merge-build runtime class (Rule 22: off the `/` partition, and the GC
	// bounds a killed merger's leftovers).
	mergeAnchorGateScratchPattern = "merge-anchor-gate-"

	// mergeAnchorGateIndexEnvName is the environment variable every git child
	// this gate starts DROPS from its environment, so a nested merger
	// invocation can never make this gate read an index that is not its own.
	// The gate builds no index of its own (the union is git's own
	// `merge-tree --write-tree`), so the name exists for that ONE reader.
	mergeAnchorGateIndexEnvName = "GIT_INDEX_FILE"

	// mergeAnchorGateUnionDirName is the directory the union tree is extracted
	// into. Distinct from the ⓪c gate's own names ("union", "branch", "target")
	// on purpose: a shared directory would let one lane read the other's tree.
	mergeAnchorGateUnionDirName = "anchor-union"

	// mergeAnchorGatePremiseDirPrefix names a premise leg's own checkout
	// (master's tree, then the branch's tree), so the two legs can never read
	// each other's materialisation.
	mergeAnchorGatePremiseDirPrefix = "anchor-premise-"

	// mergeAnchorGateGoBinaryName is the tool the measurement needs; a missing
	// one ABSTAINS (a host that cannot measure is not evidence about a branch).
	mergeAnchorGateGoBinaryName = "go"

	// mergeAnchorGateTestVerb / mergeAnchorGateTestFlags render the measurement:
	// `-count=1` defeats the test RESULT cache (a cached PASS is not a
	// measurement of this tree) and `-json` makes the per-test outcomes
	// machine-readable, which is how a failure is attributed (or refused).
	mergeAnchorGateTestVerb  = "test"
	mergeAnchorGateTestFlags = "-count=1 -json"

	// mergeAnchorGateTestSuffix is the tree suffix a RUNNABLE gate package must
	// carry: a package with no test file has nothing to measure.
	mergeAnchorGateTestSuffix = "_test.go"

	// mergeAnchorGateBuildFailedText is the toolchain's own compile-failure
	// marker in the `-json` stream ("FAIL <pkg> [build failed]").
	mergeAnchorGateBuildFailedText = "[build failed]"

	// The two `-json` spellings this gate dispatches on (Rule 1b).
	mergeAnchorGateJSONTestFailAction    = "fail"
	mergeAnchorGateJSONPackageFailAction = "fail"

	// mergeAnchorGateFindingLimit caps how many findings one refusal quotes
	// verbatim — the rework brief must stay a brief, and the count is reported
	// either way.
	mergeAnchorGateFindingLimit = 6

	// mergeAnchorGateFindingMoreFmt echoes the tally when the cap bites.
	mergeAnchorGateFindingMoreFmt = " … (%d more)"

	// mergeAnchorGateFindingSep joins findings on ONE line: the merger's log and
	// the row's detail field are single-line consumers, so a refusal never
	// smuggles a newline into either.
	mergeAnchorGateFindingSep = " | "

	// mergeAnchorGatePackagePrefix is the Go keyword a package clause opens with.
	mergeAnchorGatePackagePrefix = "package "

	// mergeAnchorGateCompileText is the sentence a closure that does not COMPILE
	// is reported under — always an ABSTENTION, never a refusal: a union tree
	// that will not build is the UNION COMPILE lane's case (and on a fused union
	// two branches can make a path hold files from both sides, measured
	// 2026-09-30 on master e791c9c7), while this lane measures DOCUMENTS.
	mergeAnchorGateCompileText = "the union's document tests do not compile (the UNION COMPILE lane owns a broken union tree; this lane measures documents, not compilation)"

	// mergeAnchorGateBranchFailText is the ABSTAIN line for a branch whose own
	// document tests already fail: the union is red, but so is the branch alone,
	// so the drift is not union-introduced (it is the branch's own pre-push
	// leg's case — the sibling arm — and this lane must not refuse for it).
	mergeAnchorGateBranchFailText = "the branch's OWN document tests fail too, so the union failure is not union-introduced"

	// mergeAnchorGateUnattributedText is the ABSTAIN line for a FAIL that named
	// no failing test: a verdict without its evidence is not a verdict (Rule 15).
	mergeAnchorGateUnattributedText = "the union's document tests failed without naming a failing test"

	// mergeAnchorGateMergeTreeVerb / mergeAnchorGateMergeTreeWriteFlag are
	// git's own union-of-two-revisions command and the flag that makes it write
	// the resulting tree (`git merge-tree --write-tree`) — how this lane reads
	// the CONTENT of a merge result without touching an index, worktree or ref
	// (Rule 1b: the spelling lives once, beside its reader).
	mergeAnchorGateMergeTreeVerb      = "merge-tree"
	mergeAnchorGateMergeTreeWriteFlag = "--write-tree"

	// mergeAnchorGatePathSeparator splits one `git ls-tree -r` line into its
	// metadata half and its path; mergeAnchorGateLsTreeFieldCount is how many
	// whitespace-separated fields that metadata half carries
	// (`<mode> <type> <blob>`) and mergeAnchorGateLsTreeBlobField is the index
	// of the blob id within them. The three are this gate's ONE reading of that
	// line shape (Rule 1b), and they live beside the reader that needs them.
	mergeAnchorGatePathSeparator    = "\t"
	mergeAnchorGateLsTreeFieldCount = 3
	mergeAnchorGateLsTreeBlobField  = 2

	// mergeAnchorGateRefuseDetailFmt is the refusal's own sentence (Rule 1b).
	mergeAnchorGateRefuseDetailFmt = "the MERGE RESULT of %s into %s fails the published-anchor document tests %s passes: %s (master unchanged)"
	// mergeAnchorGateErrorClassDetailFmt is the refusal of the edge's ERROR
	// class — a defect no package in the union could catch at all, so the
	// sentence names the document's own measure instead of a test run (there is
	// none to name). It quotes the edge itself, never a re-parse of a test's
	// output (Rule 14).
	mergeAnchorGateErrorClassDetailFmt = "the MERGE RESULT of %s into %s publishes a generated document no package this " +
		"tree can run would catch a drift in: %s (master unchanged)"
)

// mergeAnchorGateAnchorPattern reads the `<path>:<identity>` references a failing
// document test quotes — the anchor's own spelling, compiled once (Rule 1b/13:
// one definition, no bare literal at the use site), so the edge's answer and this
// gate's attribution cannot disagree about what an anchor looks like.
//
// THE IDENTITY IS INTERPOLATED, NOT RE-TYPED (Rules 1b/13/14). The published
// block was re-keyed on SYMBOL identity on 2026-10-12 (`f571564a0`:
// `cli_notify.go:isNotifySubcommand` instead of `cli_log.go:611`, because a line
// number stales on any insertion above the declaration) and the doc tests quote
// that shape back when they fail. A `(\d+)`-only reading here extracted NOTHING
// from every real finding: the extraction was empty, so the blame arm had no
// path to compare, so the lane ABSTAINED instead of refusing the branch that
// introduced the drift. `internal/cliindex` already publishes the ONE spelling of
// both halves — the separator and the identity alternation — so this pattern is
// built from them rather than typing a second copy that can drift from the
// renderer while both stay green on their own (the same defect at gendoc's
// AnchorPattern, fixed in 6507aec).
var mergeAnchorGateAnchorPattern = regexp.MustCompile(
	`\b([A-Za-z0-9_][A-Za-z0-9_./-]*\.go)` + cliindex.AnchorSep + `(` + cliindex.AnchorIdentityPatternSrc + `)`)

// mergeAnchorGateVerdict is the gate's decision for one branch.
type mergeAnchorGateVerdict struct {
	// Refused is true when the branch must not be merged: the document tests
	// the edge named fail on the union and pass on the target and the branch.
	Refused bool
	// Detail is the refusal reason ("" unless Refused): the document's own
	// findings, quoted from the failing tests' output.
	Detail string
	// Findings are the machine-readable half of the refusal: the failing tests'
	// own output lines.
	Findings []string
	// Mandate is the operator sentence the EDGE itself renders for this change
	// (gendoc.GeneratedDocMandate): WHAT the document is, WHICH generator
	// rewrites it and WHICH package blocks on it. It is BUILT from the
	// discovered edge, never typed by a caller — a hand-written reminder is the
	// drift it is meant to catch (Rule 14).
	Mandate string
}

// mergeAnchorGateSuite is ONE tree's document-test verdict.
type mergeAnchorGateSuite struct {
	// status is the shared build-gate vocabulary: Pass / Fail / Abstain.
	status string
	// output is the runner's own `-json` stream (the evidence a refusal quotes).
	output string
	// reason carries an abstention's sentence ("" unless status is Abstain).
	reason string
}

// mergeAnchorGate asks the published-anchor question about the MERGE RESULT of
// branch into master: do the document tests the edge names still pass in the
// tree that would land?
//
// EVERY ARM LEAVES A DURABLE RECORD (epic 01a144bfb, acceptance item 1). This
// lane has one refusing arm and ~26 abstaining ones, and until 2026-10-16 the
// only trace of ANY of them was a stderr line (`taskNotifyLog`), which no file
// keeps: measured 2026-10-16 on nick-HP-Laptop, `~/.gopi/merger/` holds no log
// at all, so after four published-anchor drifts landed on master in one day the
// question "which arm let this through, and why" had no instrument — the answer
// existed only in a stream that had already scrolled away.
//
// The record rides machinery that ALREADY RUNS (Rule 35: no new tool, script,
// hook, tick or file): the trail below captures the very sentences this lane
// already emits, and `commit` appends them to the owning row's `task_events`
// trail — the append-only audit table the ledger already has and the sibling
// `ref-gone` arm already writes to. The write is deferred so it happens on
// EVERY exit, including the panicking one, without a second copy of the arm
// vocabulary at 26 return sites.
func mergeAnchorGate(repo, master, branch string) (verdict mergeAnchorGateVerdict) {
	trail := &mergeAnchorGateTrail{}
	defer func() { trail.commit(repo, master, branch, verdict) }()
	if mergeAnchorGateDisabled() {
		return mergeAnchorGateVerdict{}
	}
	// A branch the target already CONTAINS adds nothing: the union is master
	// itself, whose own document state the corpus gates read on every merge
	// anyway. One ancestry read replaces the whole measurement.
	if mergeBuildGateBranchAlreadyInTarget(repo, branch, master) {
		trail.log("ABSTAINS for %s (%s already contains %s — the union is %s itself, so there is no union-introduced drift to find) — merging unchanged",
			branch, master, branch, master)
		return mergeAnchorGateVerdict{}
	}
	workRoot, cleanup, okScratch := mergeAnchorGateScratch()
	if !okScratch {
		trail.log("ABSTAINS for %s (no usable scratch tree for the union checkout) — merging unchanged", branch)
		return mergeAnchorGateVerdict{}
	}
	defer cleanup()
	// The union tree comes from GIT'S OWN merge (`merge-tree --write-tree`), and
	// asking it that way is a MEASURED decision, not a convenience: the ⓪c lane
	// materialises its union with raw `read-tree -m <base> <ours> <theirs>` over
	// an empty alternate index, and for two sides changing DIFFERENT HUNKS of one
	// file that leaves the index UNMERGED — measured on this gate's own fixture
	// pair: `read-tree -m` reports `f.txt: unmerged` and `write-tree` fails, so
	// the sibling lane ABSTAINS on the very shape this lane exists for (and is
	// right to: it cannot compile a tree that does not exist). `merge-tree` is the
	// union the merge itself would produce — exit 0 clean, exit 1 carrying
	// conflicts — and it touches no index, worktree or ref of the repo.
	unionTree, conflicts, abortReason, okTree := mergeAnchorGateUnionTree(repo, master, branch)
	if !okTree {
		trail.log("ABSTAINS for %s (%s) — merging unchanged", branch, abortReason)
		return mergeAnchorGateVerdict{}
	}
	if conflicts != "" {
		trail.log("ABSTAINS for %s (%s: %s) — merging unchanged", branch, mergeUnionGateUnresolvedTreeText, conflicts)
		return mergeAnchorGateVerdict{}
	}
	// The union's OWN change: the paths the union tree holds that the target's
	// tree does not. Read from OBJECTS (`ls-tree -r` in the gate's own alternate
	// index), never from a worktree — the master-safety argument every arm of
	// this sequence rests on.
	added, err := mergeAnchorGateAddedPaths(repo, master, unionTree, workRoot)
	if err != nil {
		trail.log("ABSTAINS for %s (the union's change could not be measured: %v) — merging unchanged", branch, err)
		return mergeAnchorGateVerdict{}
	}
	if len(added) == 0 {
		trail.log("ABSTAINS for %s (the union adds no path to %s — nothing for the published-anchor edge to map) — merging unchanged", branch, master)
		return mergeAnchorGateVerdict{}
	}
	unionDir := filepath.Join(workRoot, mergeAnchorGateUnionDirName)
	if err := mergeUnionGateExtract(repo, unionTree, unionDir); err != nil {
		trail.log("ABSTAINS for %s (materialising the union tree failed: %v) — merging unchanged", branch, err)
		return mergeAnchorGateVerdict{}
	}

	// (1) ASK THE EDGE, on the UNION's tree and the UNION's change — the same one
	// read every consumer of this edge makes (Rule 14: no second opinion in the
	// caller).
	ans, err := gendoc.GeneratedDocAnswerOpts(unionDir, added, gendocErrorClassSkipped())
	if err != nil {
		trail.log("ABSTAINS for %s (the published-anchor edge could not be read on the union: %v) — merging unchanged", branch, err)
		return mergeAnchorGateVerdict{}
	}
	if !ans.TakesPart() {
		trail.log("ABSTAINS for %s (the union publishes no generated document: %s) — merging unchanged",
			branch, mergeAnchorGateAbstainReason(ans))
		return mergeAnchorGateVerdict{}
	}
	if derr := gendoc.EmptyGateDerivedErrors(ans.Errors); len(derr) > 0 {
		// THE ERROR CLASS IS A REFUSAL HERE, and it is the one class this lane
		// must not leave to a log line: it names a defect NO package in the
		// union could ever catch, which is the shape the lane exists for. The
		// refusal is attributed the same way every other one on this path is —
		// to a file THIS BRANCH's own change touched — so a hole somebody else
		// left in the target's document cannot refuse an unrelated landing.
		//
		// It is decided BEFORE the union's tests are run because it does not
		// depend on them (the hole is a property of the TREE, measured from the
		// answer already in hand), and a refusal that waited for a run whose
		// gate packages cannot exist would never arrive.
		//
		// The SUB-CLASS is selected, never the whole Errors channel
		// (gendoc.EmptyGateDerivedErrors): the classes share a severity and
		// nothing else, and refusing on the channel itself is the regression
		// that twice turned this path into a refusal of the DECLARATION
		// invariant before the gate it owed had run.
		touched, err := mergeAnchorGateChangedPaths(repo, master, branch, workRoot)
		if err != nil {
			trail.log("ABSTAINS for %s (the branch's own change could not be measured: %v) — merging unchanged", branch, err)
			return mergeAnchorGateVerdict{}
		}
		// ATTRIBUTION: the class refuses when the union's own change moved a
		// published anchor source of a document the class names
		// (Answer.Touched — the edge's own measurement, never a re-parse: this
		// finding quotes the document and its generator and carries NO
		// `file.go:NNN` ref, so the sibling predicate below, which reads anchor
		// refs out of a finding, cannot attribute it and would log "names no file
		// this branch changed" while the union moved exactly that anchor source;
		// measured 2026-10-14 on the fixture tree the devdoc records). The
		// branch's own touched set is kept as the second arm so a refusal is
		// never lost to a change this lane cannot see.
		if !ans.Touched && !mergeAnchorGateBlamesBranch(derr, touched) {
			trail.log("the union's published-anchor ERROR class names no file this branch changed for %s: %s",
				branch, strings.Join(derr, mergeAnchorGateFindingSep))
		} else {
			return mergeAnchorGateVerdict{
				Refused: true,
				Detail: fmt.Sprintf(mergeAnchorGateErrorClassDetailFmt, branch, master,
					strings.Join(derr, mergeAnchorGateFindingSep)) + gendocErrorClassSkipNote(),
				Findings: derr,
			}
		}
	}
	if len(ans.Findings) > 0 {
		// A broken edge is a defect IN THE TREE, not in this measurement's
		// environment: a document stating a generated region whose regeneration
		// instruction runs nothing. It is NOT this lane's refusal — the union is
		// a THIRD revision, and a document no package reads is the delivering
		// branch's own affair (the sibling pre-push arm refuses on it, and ⓪b's
		// compile leg has already spoken if the union does not build) — it is
		// stated, and the measurement continues.
		trail.log("the union's published-anchor edge is BROKEN for %s: %s",
			branch, strings.Join(ans.Findings, mergeAnchorGateFindingSep))
	}
	if len(ans.Required) == 0 {
		trail.log("ABSTAINS for %s (the union's change touches no published anchor source: %d path(s) added, %d edge(s)) — merging unchanged",
			branch, len(added), ans.Docs)
		return mergeAnchorGateVerdict{}
	}
	runnable, skips := mergeAnchorGateRunnable(unionDir, ans.Required)
	if len(runnable) == 0 {
		trail.log("ABSTAINS for %s (the edge named %d gate package(s), none runnable in this union: %s) — merging unchanged",
			branch, len(ans.Required), strings.Join(skips, mergeAnchorGateFindingSep))
		return mergeAnchorGateVerdict{}
	}
	if len(skips) > 0 {
		trail.log("the edge named %d gate package(s) THIS UNION CANNOT RUN for %s: %s",
			len(skips), branch, strings.Join(skips, mergeAnchorGateFindingSep))
	}
	timeoutSec, err := mergeAnchorGateTimeout()
	if err != nil {
		trail.log("ABSTAINS for %s (budget unreadable: %v) — merging unchanged", branch, err)
		return mergeAnchorGateVerdict{}
	}
	if !mergeBuildGateIsGoRepo(unionDir) {
		trail.log("ABSTAINS for %s (%s: the union is not a Go module, so there is no declared way to run its tests) — merging unchanged",
			branch, mergeBuildGateStatusNoContract)
		return mergeAnchorGateVerdict{}
	}
	cacheDir, okCache := mergeBuildGateGoCache(repo)
	if !okCache {
		trail.log("ABSTAINS for %s (no usable shared Go build cache — the reason is logged above) — merging unchanged", branch)
		return mergeAnchorGateVerdict{}
	}
	modCacheDir, okModCache := mergeBuildGateSharedModCache()
	if !okModCache {
		trail.log("ABSTAINS for %s (no populated shared module cache — the reason is logged above) — merging unchanged", branch)
		return mergeAnchorGateVerdict{}
	}

	// (2) MEASURE THE UNION — the gate packages the edge named, run in the
	// materialised merge result (the branch-and-package-scoped test run of Rule
	// 27, never a whole-tree suite).
	unionRes := mergeAnchorGateSuiteRun(unionDir, runnable, timeoutSec, cacheDir, modCacheDir)
	switch unionRes.status {
	case mergeBuildGateStatusAbstain:
		trail.log("ABSTAINS for %s (%s) — merging unchanged", branch, unionRes.reason)
		return mergeAnchorGateVerdict{}
	case mergeBuildGateStatusPass:
		trail.log("PASS for %s (the published-anchor document tests of the merge result of %s into %s pass)",
			branch, branch, master)
		return mergeAnchorGateVerdict{}
	}

	findings := mergeAnchorGateFailingTests(unionRes.output)
	if len(findings) == 0 {
		// The suite said FAIL and named no failing test: a measurement the gate
		// cannot attribute to a document. ABSTAIN rather than refuse on an
		// unattributed failure (Rule 15: a verdict without its evidence is not a
		// verdict).
		trail.log("ABSTAINS for %s (%s) — merging unchanged", branch, mergeAnchorGateUnattributedText)
		return mergeAnchorGateVerdict{}
	}

	// (3) THE PREMISE AND THE BLAME — paid only on the path that is about to
	// refuse, and they are what make the refusal a statement about THIS branch
	// rather than about a tree somebody else left red.
	//
	// THE PREMISE: the branch must PASS the same measurement ALONE. A branch
	// whose own document tests already fail is red on its own account — measured
	// on its own tip, unaffected by anything the union composed — and refusing it
	// here would report the same defect twice while telling the author the wrong
	// thing about where it came from. Its own pre-push check is where that is
	// decided (the sibling arm).
	//
	// THE TARGET is deliberately NOT required to pass. A red target does not
	// excuse a stale union — it is exactly how one arises: the target moved an
	// anchor source without regenerating, the branch regenerated its own document
	// correctly, and the UNION's document is the branch's (the only side that
	// touched it) while the union's source carries BOTH moves. Demanding a green
	// target would abstain on that shape, which is the shape this lane exists
	// for. What replaces it is BLAME, and it is the stronger test: the union's
	// failure must name an anchor in a file THIS BRANCH's own change touched. A
	// failure in a file the branch never went near came from the other side, and
	// this lane abstains and says so (never a refusal for someone else's drift —
	// the ⓪b gate's 2026-09-13 lesson, and the measured red integration ref this
	// lane was written against).
	premise, err := mergeAnchorGateTreeRun(repo, branch, branch, runnable, workRoot, timeoutSec, cacheDir, modCacheDir)
	if err != nil {
		trail.log("ABSTAINS for %s (the branch's own document tests could not be measured: %v) — merging unchanged", branch, err)
		return mergeAnchorGateVerdict{}
	}
	switch premise.status {
	case mergeBuildGateStatusAbstain:
		trail.log("ABSTAINS for %s (the branch's own document tests could not be measured: %s) — merging unchanged", branch, premise.reason)
		return mergeAnchorGateVerdict{}
	case mergeBuildGateStatusFail:
		trail.log("ABSTAINS for %s (%s) — merging unchanged", branch, mergeAnchorGateBranchFailText)
		return mergeAnchorGateVerdict{}
	}
	touched, err := mergeAnchorGateChangedPaths(repo, master, branch, workRoot)
	if err != nil {
		trail.log("ABSTAINS for %s (the branch's own change could not be measured: %v) — merging unchanged", branch, err)
		return mergeAnchorGateVerdict{}
	}
	if !mergeAnchorGateBlamesBranch(findings, touched) {
		trail.log("ABSTAINS for %s (the union's failing anchor names no file this branch changed: %s) — merging unchanged",
			branch, strings.Join(findings, mergeAnchorGateFindingSep))
		return mergeAnchorGateVerdict{}
	}
	mandate, err := gendoc.GeneratedDocMandate(unionDir, added)
	if err != nil {
		// The refusal stands on its OWN evidence (the failing tests' output);
		// the mandate is the brief's context, so a mandate that could not be
		// rendered is STATED and does not become a second verdict (Rule 15).
		trail.log("the refusal's mandate could not be rendered for %s: %v", branch, err)
	}
	return mergeAnchorGateVerdict{
		Refused:  true,
		Detail:   mergeAnchorGateDetail(branch, master, runnable, findings),
		Findings: findings,
		Mandate:  mandate,
	}
}

// mergeAnchorGateDetail renders the refusal's sentence: which packages were
// measured, and the document's own findings — the failing tests' output, capped
// so a wide failure stays a brief.
func mergeAnchorGateDetail(branch, master string, packages, findings []string) string {
	shown := findings
	more := ""
	if len(shown) > mergeAnchorGateFindingLimit {
		more = fmt.Sprintf(mergeAnchorGateFindingMoreFmt, len(shown)-mergeAnchorGateFindingLimit)
		shown = shown[:mergeAnchorGateFindingLimit]
	}
	return fmt.Sprintf(mergeAnchorGateRefuseDetailFmt, branch, master,
		strings.Join(packages, mergeAnchorGateFindingSep),
		strings.Join(shown, mergeAnchorGateFindingSep)+more)
}

// mergeAnchorGateAbstainReason renders a participation abstention with the
// counts the walk produced, so "the scan looked and found nothing" is never
// something a reader has to infer from silence (Rule 15).
func mergeAnchorGateAbstainReason(ans gendoc.Answer) string {
	reason := ans.Reason
	if reason == "" {
		reason = gendoc.ParticipationUnknown
	}
	return fmt.Sprintf("%s (%d candidate(s), %d edge(s))", reason, ans.Candidates, ans.Docs)
}

// --- the measurement ---------------------------------------------------------

// mergeAnchorGateSuiteRun runs the edge's gate packages in dir and returns the
// runner's verdict. Every environmental failure is an ABSTAIN carrying its own
// sentence (never a refusal): a runner that cannot start, a timeout, a module
// set that cannot resolve, a full disk, a package that does not COMPILE.
func mergeAnchorGateSuiteRun(dir string, packages []string, timeoutSec int, cacheDir, modCacheDir string) mergeAnchorGateSuite {
	ctx, cancel := mergeAnchorGateContext(timeoutSec)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", mergeAnchorGateCommand(packages))
	cmd.Dir = dir
	// The environment is the ⓪b gate's ONE builder (GOCACHE = the SHARED,
	// disk-backed, bounded cache; GOMODCACHE = the operator's shared read-only
	// one) — so a merge probe never fills the operator's caches and the three
	// merge-time gates cannot disagree about where a build reads from (Rule 14).
	cmd.Env = mergeBuildGateEnv(cacheDir, modCacheDir)
	// Process-group isolation + bounded pipes: the same two guards the build
	// gate measured (a hung command is killed with its children at the deadline,
	// and a grandchild holding the pipes cannot make Wait outlive the budget).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = mergeBuildGateWaitDelay
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil && cmd.Process != nil {
		// The group id is RE-VERIFIED before delivery: it is this child's pid, and the
		// kernel may have recycled that number to an unrelated group (see killGroupGuard.go).
		_, _ = signalProcessGroup(cmd.Process.Pid, cmd.Process.Pid, syscall.SIGKILL) //stylegate:ignore signal to a process group we are already abandoning; ESRCH (the group already gone) is the expected error
	}
	if ctx.Err() == context.DeadlineExceeded {
		return mergeAnchorGateSuite{status: mergeBuildGateStatusAbstain,
			reason: fmt.Sprintf("%s exceeded its %ds budget (a timeout is not a document verdict)", mergeAnchorGateCommand(packages), timeoutSec)}
	}
	if err == nil {
		return mergeAnchorGateSuite{status: mergeBuildGateStatusPass, output: string(out)}
	}
	if _, ok := err.(*exec.ExitError); !ok {
		// The command could not be started at all: a measurement failure, never
		// a document verdict (the two sibling gates' own rule).
		return mergeAnchorGateSuite{status: mergeBuildGateStatusAbstain,
			reason: fmt.Sprintf("%s could not be started: %v", mergeAnchorGateCommand(packages), err)}
	}
	return mergeAnchorGateJudge(dir, packages, string(out))
}

// mergeAnchorGateJudge maps a non-zero run to ABSTAIN or FAIL, sharing the
// sibling gates' predicates so the three lanes cannot disagree about what "the
// environment refused" means (Rule 14).
func mergeAnchorGateJudge(dir string, packages []string, output string) mergeAnchorGateSuite {
	if mergeBuildGateUnresolvedModules(output) {
		return mergeAnchorGateSuite{status: mergeBuildGateStatusAbstain, output: output,
			reason: mergeUnionGateUnresolvedModulesErr}
	}
	if marker, yes := mergeBuildGateDiskExhausted(output); yes {
		return mergeAnchorGateSuite{status: mergeBuildGateStatusAbstain, output: output,
			reason: mergeBuildGateDiskExhaustedErr(marker, dir)}
	}
	if failed := mergeAnchorGateBuildFailurePackage(output); failed != "" {
		return mergeAnchorGateSuite{status: mergeBuildGateStatusAbstain, output: output,
			reason: mergeAnchorGateCompileText + " — " + failed}
	}
	return mergeAnchorGateSuite{status: mergeBuildGateStatusFail, output: output}
}

// mergeAnchorGateCommand renders the measurement's command line: the edge's
// packages, through the declared test runner, branch- and package-scoped
// (Rule 27: a merge-time lane never runs a whole-tree suite).
func mergeAnchorGateCommand(packages []string) string {
	patterns := make([]string, 0, len(packages))
	for _, p := range packages {
		patterns = append(patterns, "./"+p+mergeAnchorGatePackageGlob)
	}
	return fmt.Sprintf("%s %s %s %s", mergeAnchorGateGoBinaryName, mergeAnchorGateTestVerb,
		mergeAnchorGateTestFlags, strings.Join(patterns, " "))
}

// mergeAnchorGatePackageGlob widens one package directory to its subtree — the
// same scope every other lane in this repo runs a package with.
const mergeAnchorGatePackageGlob = "/..."

// mergeAnchorGateContext builds the measurement's deadline context: the
// operator's budget when it is set, else the sibling gate's own default, so the
// merge-time gates never disagree about how long a measurement may take.
func mergeAnchorGateContext(timeoutSec int) (context.Context, context.CancelFunc) {
	if timeoutSec > 0 {
		return context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	}
	return context.WithCancel(context.Background())
}

// mergeAnchorGateTreeRun measures ONE revision's own document tests: the tree is
// extracted through `git archive` (no checkout, no worktree, no branch of the
// repo touched) and the SAME suite runs there with the SAME caches — so the
// premise is the identical measurement, never a different question.
func mergeAnchorGateTreeRun(repo, rev, label string, packages []string, workRoot string,
	timeoutSec int, cacheDir, modCacheDir string) (mergeAnchorGateSuite, error) {
	tree, err := mergeUnionGateGit(repo,
		mergeUnionGateRevParseVerb, rev+mergeUnionGateTreeSuffix)
	// The tree is the WHOLE answer here, so it is trimmed before it becomes an
	// argument — the sibling leg's own measurement (mergeUnionGateTreeAlone):
	// `git rev-parse` ends its line, and an untrimmed value reaches `git archive`
	// as "<oid>\n", which git answers with `not a valid object name`. Measured
	// 2026-10-16 on a git-2.38 host, with the union arm gone through: this leg
	// abstained on EVERY branch, so the lane could never reach its refusal (task
	// 01a144eb-0d51-7d2d-ab3b-51500a7132e2).
	tree = strings.TrimSpace(tree)
	if err != nil || tree == "" {
		return mergeAnchorGateSuite{}, fmt.Errorf("resolving %s's tree failed: %s", label, mergeBuildGateTail(errTextOr(err, "no tree hash")))
	}
	dir := filepath.Join(workRoot, mergeAnchorGatePremiseDirPrefix+mergeAnchorGateDirToken(label))
	if err := mergeUnionGateExtract(repo, tree, dir); err != nil {
		return mergeAnchorGateSuite{}, fmt.Errorf("materialising %s's tree failed: %w", label, err)
	}
	return mergeAnchorGateSuiteRun(dir, packages, timeoutSec, cacheDir, modCacheDir), nil
}

// mergeAnchorGateDirToken renders a revision name as one path element (a ref
// carries `/` and, for an expression, `:`) so two legs can never collide.
func mergeAnchorGateDirToken(label string) string {
	return strings.NewReplacer("/", "-", ":", "-", "^", "-", "~", "-").Replace(label)
}

// --- attribution -------------------------------------------------------------

// mergeAnchorGateBlamesBranch reports whether any finding names a `<path>:<line>`
// anchor in a file THIS BRANCH's own change touched. It is the whole attribution
// test, and it is read off the document's OWN evidence (the failing test's
// message publishes the file it disagrees about) rather than inferred from what
// the edge said — a refusal must be traceable to the branch it refuses.
func mergeAnchorGateBlamesBranch(findings []string, touched map[string]bool) bool {
	for _, f := range findings {
		for _, ref := range mergeAnchorGateAnchorRef(f) {
			if touched[ref] {
				return true
			}
		}
	}
	return false
}

// mergeAnchorGateAnchorRef extracts the `path.go:<identity>` references a finding
// quotes, in repo-relative form — BOTH identity shapes (a symbol name and the
// legacy line number), because the extraction is what the blame arm compares
// against the branch's changed paths. An extraction that reads only line numbers
// comes back empty on every symbol-addressed finding, and an empty extraction
// blames nobody, which turns a refusal into an abstention (see the pattern's own
// comment). A finding that quotes no reference blames nobody (and the caller
// abstains rather than refusing on prose).
func mergeAnchorGateAnchorRef(finding string) []string {
	var out []string
	for _, m := range mergeAnchorGateAnchorPattern.FindAllStringSubmatch(finding, -1) {
		out = append(out, strings.TrimPrefix(m[1], "./"))
	}
	return out
}

// mergeAnchorGateChangedPaths is the branch's OWN change: every path whose
// content differs between the branch's tree and the target's tree (blob ids, so
// a file both sides carry in different revisions counts). Read from OBJECTS.
func mergeAnchorGateChangedPaths(repo, target, branch, workRoot string) (map[string]bool, error) {
	targetBlobs, err := mergeAnchorGateTreeBlobs(repo, workRoot, target)
	if err != nil {
		return nil, err
	}
	branchTree, err := mergeUnionGateGit(repo,
		mergeUnionGateRevParseVerb, branch+mergeUnionGateTreeSuffix)
	// Trimmed for the same measured reason as the premise leg above: untrimmed,
	// `git ls-tree` answers `not a valid object name`, which would turn the
	// BLAME arm — the arm that decides whether a refusal is about THIS branch —
	// into an abstention.
	branchTree = strings.TrimSpace(branchTree)
	if err != nil || branchTree == "" {
		return nil, fmt.Errorf("resolving %s's tree failed: %s", branch, mergeBuildGateTail(errTextOr(err, "no tree hash")))
	}
	branchBlobs, err := mergeAnchorGateTreeBlobs(repo, workRoot, branchTree)
	if err != nil {
		return nil, err
	}
	touched := map[string]bool{}
	for path, blob := range branchBlobs {
		if targetBlobs[path] != blob {
			touched[path] = true
		}
	}
	return touched, nil
}

// --- reading the runner's own stream ----------------------------------------

// mergeAnchorGateEvent is the subset of `go test -json` this gate reads: the
// toolchain's own field names, used as-is.
type mergeAnchorGateEvent struct {
	Action  string `json:"Action"`
	Test    string `json:"Test"`
	Output  string `json:"Output"`
	Package string `json:"Package"`
}

// mergeAnchorGateEvents parses the runner's stream, skipping any line that is
// not one of its events — a `-json` stream also carries plain lines (the
// toolchain's own `[build failed]` summary is one), and a reader that assumed
// every line were JSON would drop the very evidence a compile failure needs.
func mergeAnchorGateEvents(stream string) []mergeAnchorGateEvent {
	var out []mergeAnchorGateEvent
	for _, line := range strings.Split(stream, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var e mergeAnchorGateEvent
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		if e.Action == "" {
			continue
		}
		out = append(out, e)
	}
	return out
}

// mergeAnchorGateFailingTests extracts the failing tests' own output — the
// document's findings, which is what a refusal quotes and what a reworker
// verifies against.
func mergeAnchorGateFailingTests(stream string) []string {
	events := mergeAnchorGateEvents(stream)
	failing := map[string]bool{}
	for _, e := range events {
		if e.Test != "" && e.Action == mergeAnchorGateJSONTestFailAction {
			failing[e.Package+"\x00"+e.Test] = true
		}
	}
	var out []string
	seen := map[string]bool{}
	named := map[string]bool{}
	for _, e := range events {
		if e.Test == "" || !failing[e.Package+"\x00"+e.Test] {
			continue
		}
		for _, line := range strings.Split(e.Output, "\n") {
			line = strings.TrimRight(line, " \t")
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "=== RUN") || strings.HasPrefix(trimmed, "--- FAIL") {
				continue
			}
			if seen[line] {
				continue
			}
			seen[line] = true
			// The FIRST line a failing test speaks carries the test's identity:
			// the brief's reader has to know WHICH assertion to re-run, and a
			// finding that is only a message sends them looking.
			if key := e.Package + "." + e.Test; !named[key] {
				named[key] = true
				line = key + mergeAnchorGateFindingSep + line
			}
			out = append(out, line)
		}
	}
	return out
}

// mergeAnchorGateBuildFailurePackage reports the package whose tests did not
// COMPILE, in the toolchain's two spellings: its own `[build failed]` marker, or
// a package-level failure whose package reported no test result at all. Both are
// read here in one place, and "" means the failure was a TEST failure.
func mergeAnchorGateBuildFailurePackage(stream string) string {
	events := mergeAnchorGateEvents(stream)
	ranTests := map[string]bool{}
	for _, e := range events {
		if e.Test != "" {
			ranTests[e.Package] = true
		}
	}
	for _, e := range events {
		if e.Action == mergeAnchorGateJSONPackageFailAction && e.Package != "" && !ranTests[e.Package] {
			return e.Package
		}
	}
	if strings.Contains(stream, mergeAnchorGateBuildFailedText) {
		return mergeAnchorGateBuildFailedText
	}
	return ""
}

// --- selecting what to run ---------------------------------------------------

// mergeAnchorGateRunnable keeps only the gate packages this union can actually
// RUN — a directory that exists in the materialised tree and carries at least
// one test file — and states every one it dropped with its reason. A named
// package the tree cannot run is an answer about a differently-shaped tree,
// never a silent skip (Rule 15).
func mergeAnchorGateRunnable(unionDir string, packages []string) (runnable, skips []string) {
	for _, p := range packages {
		dir := filepath.Join(unionDir, filepath.FromSlash(p))
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			skips = append(skips, p+" ("+mergeAnchorGateNotADirNote+")")
			continue
		}
		hasTest, err := mergeAnchorGateHasTestFile(dir)
		if err != nil {
			skips = append(skips, p+" (unreadable: "+err.Error()+")")
			continue
		}
		if !hasTest {
			skips = append(skips, p+" ("+mergeAnchorGateNoTestNote+")")
			continue
		}
		runnable = append(runnable, p)
	}
	return runnable, skips
}

// mergeAnchorGateHasTestFile reports whether dir carries a runnable test file.
func mergeAnchorGateHasTestFile(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), mergeAnchorGateTestSuffix) {
			return true, nil
		}
	}
	return false, nil
}

// The two reasons a named gate package is dropped from the run set.
const (
	mergeAnchorGateNotADirNote = "not a directory in the materialised union"
	mergeAnchorGateNoTestNote  = "carries no test file, so there is nothing to run"
)

// --- plumbing ----------------------------------------------------------------

// mergeAnchorGateUnionTree materialises the union of master and branch through
// GIT'S OWN merge (`git merge-tree --write-tree`): the union `git merge` would
// produce, with no index, worktree or ref of the repo touched, and — unlike raw
// `read-tree -m` — able to resolve two sides that changed different hunks of one
// file. Exit 0 = a clean union tree on stdout; exit 1 with the tree on the first
// line = the union carries TEXTUAL CONFLICTS (the resolver lane's business, so
// this gate abstains and states the conflicted paths); any other failure ABSTAINS
// with git's own words.
func mergeAnchorGateUnionTree(repo, master, branch string) (tree, conflicts, reason string, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), mergeUnionGateGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, mergeUnionGateGitBinaryName, "-C", repo,
		mergeAnchorGateMergeTreeVerb, mergeAnchorGateMergeTreeWriteFlag, master, branch)
	cmd.Env = mergeAnchorGateGitEnv()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return "", "", mergeAnchorGateMergeTreeTimeoutText, false
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return "", "", fmt.Sprintf("%s produced no tree: %s",
			mergeAnchorGateMergeTreeVerb, mergeBuildGateTail(strings.TrimSpace(stderr.String()))), false
	}
	tree = strings.TrimSpace(lines[0])
	if err == nil {
		return tree, "", "", true
	}
	if _, isExit := err.(*exec.ExitError); !isExit {
		return "", "", fmt.Sprintf("%s could not be started: %v", mergeAnchorGateMergeTreeVerb, err), false
	}
	// A non-zero exit that still printed a tree is the conflict shape: git lists
	// what it could not resolve after the tree hash.
	return tree, mergeAnchorGateMergeTreeConflicts(lines[1:]), "", true
}

// mergeAnchorGateMergeTreeConflicts renders git's own conflict messages as one
// bounded line — the evidence a reader needs to see WHY the union was not
// measurable, and that it was the resolver lane's shape rather than silence.
func mergeAnchorGateMergeTreeConflicts(messages []string) string {
	var out []string
	for _, line := range messages {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
		if len(out) >= mergeAnchorGateFindingLimit {
			break
		}
	}
	if len(out) == 0 {
		return mergeAnchorGateConflictsUnnamedText
	}
	return strings.Join(out, mergeAnchorGateFindingSep)
}

// mergeAnchorGateConflictsUnnamedText is what a conflict-shaped exit reports when
// git printed no message: the verdict stands and its evidence is stated as
// unavailable (Rule 15).
const mergeAnchorGateConflictsUnnamedText = "the union carries conflicts and git named no path"

// mergeAnchorGateMergeTreeTimeoutText is the abstention a hung union merge
// reports.
const mergeAnchorGateMergeTreeTimeoutText = "the union merge exceeded its budget"

// mergeAnchorGateGitEnv is the environment every git child of this gate runs
// with: the caller's own GIT_INDEX_FILE is dropped FIRST (a duplicate entry would
// leave which value wins to getenv's first-match rule), so a nested merger
// invocation can never make this gate read an index that is not its own.
func mergeAnchorGateGitEnv() []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, mergeAnchorGateIndexEnvName+"=") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// mergeAnchorGateAddedPaths is the union's OWN change: every path whose CONTENT
// the union composes differently from the target's tree. Read from OBJECTS
// (`ls-tree -r` through the tree hash), never from a worktree — the
// master-safety argument the whole gate sequence rests on.
func mergeAnchorGateAddedPaths(repo, target, unionTree, workRoot string) ([]string, error) {
	before, err := mergeAnchorGateTreeBlobs(repo, workRoot, target)
	if err != nil {
		return nil, err
	}
	after, err := mergeAnchorGateTreeBlobs(repo, workRoot, unionTree)
	if err != nil {
		return nil, err
	}
	var added []string
	for path, blob := range after {
		if before[path] != blob {
			added = append(added, path)
		}
	}
	sort.Strings(added)
	return added, nil
}

// mergeAnchorGateTreeBlobs maps one tree's tracked paths to their blob ids — a
// reading of `git ls-tree -r` in the gate's own alternate index, so the repo's
// index is never consulted.
//
// THE COMPARISON IS BY CONTENT, NEVER BY MEMBERSHIP, and that is the whole point
// (measured while building this gate's own fixture): both sides carry the anchor
// source, each in its own revision, and only the BLOB ids say the union's copy
// is a third revision. A membership test ("a path the target does not have")
// would report an empty change on exactly the incident this lane exists for and
// abstain in silence.
func mergeAnchorGateTreeBlobs(repo, workRoot, tree string) (map[string]string, error) {
	out, err := mergeUnionGateGit(repo, manifestUnionGateLsTreeVerb, manifestUnionGateRecursiveFlag,
		manifestUnionGateFullTreeFlag, tree)
	if err != nil {
		return nil, err
	}
	blobs := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		meta, path, found := strings.Cut(strings.TrimRight(line, "\r"), mergeAnchorGatePathSeparator)
		if !found || path == "" {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) < mergeAnchorGateLsTreeFieldCount {
			continue
		}
		blobs[path] = fields[mergeAnchorGateLsTreeBlobField]
	}
	return blobs, nil
}

// mergeAnchorGateScratch prepares this gate's per-call scratch and its cleanup.
// It shares the ⓪c gate's runtime class (the same kind of work, the same GC
// bound) and creates NO worktree, so there is no git registration of its own to
// reap — the sibling's reap still runs for a crashed ⓪c call in the same class.
func mergeAnchorGateScratch() (workRoot string, cleanup func(), ok bool) {
	classDir := scratch.RuntimeScratchDir(scratch.RuntimeClassMergeBuild)
	if classDir == "" {
		return "", func() {}, false
	}
	workRoot, err := scratch.MkdirTempIn(classDir, mergeAnchorGateScratchPattern)
	if err != nil {
		taskNotifyLog("merger: ANCHOR gate scratch unusable: %v", err)
		return "", func() {}, false
	}
	return workRoot, func() {
		if err := os.RemoveAll(workRoot); err != nil {
			taskNotifyLog("merger: ANCHOR gate scratch %s not removed: %v", workRoot, err)
		}
	}, true
}

// mergeAnchorGateTimeout resolves the per-command budget: the operator's
// override when it is a positive integer, else the sibling gate's own default.
// Rule 2 — this is the ONE reader of mergeAnchorGateTimeoutEnvName, and an
// unparsable value is an ERROR (the gate abstains with the reason), never a
// silent fallback to the default.
func mergeAnchorGateTimeout() (int, error) {
	raw := strings.TrimSpace(os.Getenv(mergeAnchorGateTimeoutEnvName))
	if raw == "" {
		return mergeUnionGateDefaultTimeoutSec, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s=%q is not a positive number of seconds", mergeAnchorGateTimeoutEnvName, raw)
	}
	return n, nil
}

// mergeAnchorGateTrailFrom is DELETED with the thread-local idea it served: the
// trail is an ordinary local (mergeAnchorGate holds it and defers commit), and
// every line is emitted through `trail.log`, so no second vocabulary exists and
// no call site can log into a trail that is not the current one.
// mergeAnchorGateDisabled reports whether the operator escape hatch is set.
// Rule 2: this is the ONE reader of mergeAnchorGateEnvName; unset (or any other
// value) means ENABLED.
func mergeAnchorGateDisabled() bool {
	return strings.TrimSpace(os.Getenv(mergeAnchorGateEnvName)) == mergeAnchorGateDisabledValue
}
