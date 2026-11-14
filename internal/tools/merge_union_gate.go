package tools

// merge_union_gate.go — the MERGE-TIME UNION COMPILE gate (⓪c): compiles the
// MERGE RESULT, not just the branch.
//
// THE INCIDENT IT CLOSES (2026-09-16, master e791c9c7): `cmd/agent`'s TEST
// BINARY did not build on master. Two branches had each added a helper called
// `funcBody` to the same package:
//
//	db1fcef6  cmd/agent/cli_tasks_rebuild_pass_test.go    func funcBody(t *testing.T, src, decl string) string
//	362278e2  cmd/agent/merger_resume_arm_wiring_test.go  func funcBody(t *testing.T, src, name string) string
//
// Neither branch's own suite compiled the other's file, so each was green
// ALONE. Their union — master after both landed — was a redeclaration:
//
//	cmd/agent/merger_resume_arm_wiring_test.go:104:6: funcBody redeclared in this block
//
// Every test in `cmd/agent` was unrunnable and nothing in the merge path
// noticed. The reason is structural, not an oversight: the ⓪b BUILD gate runs
// the repo's DECLARED build — for GoPi `CGO_ENABLED=0 go build -o bin/gopi
// ./cmd/agent/` — and `go build` NEVER COMPILES `_test.go` FILES. A test-only
// collision is therefore invisible to it by construction: green branch + green
// master + red union. Measured for this exact shape (a two-file duplicate
// declaration in one package): `go build ./...` exits 0 while
// `go test -run '^$' -count=1 ./...` and `go vet ./...` both report
// "funcBody redeclared in this block".
//
// WHY A SEPARATE LANE INSTEAD OF EXTENDING ⓪b: the build gate's contract is
// Rule 12 — a repo DECLARES its build, and GoPi declares the operator's own
// `go build`. Teaching it to compile tests would either change what that
// declaration means or make it run a command no repo declared (a guess). Both
// gates measure a different question, and only this one asks "what happens when
// these two branches are TOGETHER": build-input equality does not imply output
// equality, and the merge result is a THIRD revision that neither the branch's
// nor master's own build ever visited.
//
// MECHANISM — the union is materialised from GIT OBJECTS; no working tree, no
// index and no branch of the repo is ever touched (the same master-safety
// argument as the contract and build gates):
//  1. `git merge-tree --write-tree <branch> <master>` — git's OWN three-way
//     merge, run entirely in the object store, which prints the merge result's
//     TREE and nothing else about it;
//  2. a TEXTUAL conflict is the same call's NON-ZERO EXIT with the conflicted
//     paths in its output (git reports the merge as failed and lists them in
//     `git merge-tree`'s unmerged stage lines, `<mode> <oid> <stage>\t<path>`):
//     MERGE_RESULT_UNAVAILABLE — it is the resolver lane's case, exactly as
//     before this gate existed. This gate owns the COMPLEMENTARY half (texts
//     that merge cleanly still have to compile) and says so rather than
//     guessing;
//  3. `git archive <tree> | tar -x` into a scratch directory — the exact bytes
//     of the merge result. No worktree is created, so nothing is registered in
//     the repo and there is no registration to reap (the ⓪b gate's repair
//     machinery exists because it uses worktrees; this gate does not);
//  4. the compile-only pass runs there with the gate's own Go caches
//     (`go test -run '^$' -count=1 ./...` builds every test binary and runs
//     NOTHING — no ledger, no fixture, no daemon; then `go vet ./...`).
//
// WHY `merge-tree --write-tree` AND NOT `read-tree -m <base> <branch> <master>`
// (the builder this gate was written with, REPLACED 2026-10-13): `read-tree -m`
// is an INDEX operation, and a path whose two sides differ is recorded in the
// index in its UNMERGED state (stages 1/2/3) even when the two sides' hunks do
// not overlap and the merge is textually CLEAN — `write-tree` then refuses the
// whole tree with `fatal: git-write-tree: error building trees`. Measured on a
// reproduction (`f.txt` changed at its first and last line by the two sides,
// nothing else anywhere): `read-tree -m` → `f.txt: unmerged (…)` ×3 +
// `fatal: git-write-tree: error building trees`, while
// `git merge-tree --write-tree <branch> <master>` → exit 0 and a tree id. The
// gate read those unmerged stages as "textual conflict, the resolver lane owns
// it" and ABSTAINED — on the shape two branches editing the SAME FILE FROM
// DIFFERENT HUGE sides produce, which is ordinary rather than exotic: the
// strongest check this lane owns was therefore absent exactly where a mergeset
// produces it most often. `merge-tree --write-tree` performs the merge itself,
// so "clean" and "conflicted" are ITS verdict (exit status), never inferred
// from a stage's presence.
//
// THE COMPARISON IS MEASURED, NEVER ASSUMED. The union comes FIRST (the common
// case is a green union and the gate costs one pass). Only when the union FAILS
// does the gate measure the branch ALONE — the identical test-compile, on the
// branch's own tree — because the refusal's meaning is "the COMBINATION broke
// it": a branch that also fails alone is not union-introduced (its own break
// belongs to the ⓪b lane and the rework route), and the branch-alone
// measurement turns that from a guess into a measurement at the price of one
// compile, paid only on the path that is about to refuse.
//
// ABSTAIN IS THE DEFAULT (the merger's DETECT-never-block contract, and the
// lesson the build gate learned the hard way): no compile contract for the repo,
// no Go toolchain or no tar on this host, a git too old to know
// `merge-tree --write-tree`, an unreadable revision, a conflicted union, a
// compile failure the BRANCH reproduces alone, a failure the environment caused
// (module resolution — mergeBuildGateUnresolvedModules), a timeout, or an
// unusable scratch tree/cache. Exactly ONE shape refuses: branch compiles alone,
// union does not.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/testcompile"
)

const (
	// mergeUnionGateEnvName names the operator escape hatch (Rule 2: the
	// default below is the constant, and this is the ONE reader).
	mergeUnionGateEnvName = "GOPI_MERGE_UNION_GATE"

	// mergeUnionGateDisabledValue turns the gate off — an unforeseen
	// false-positive must always be one env var away from gone.
	mergeUnionGateDisabledValue = "0"

	// mergeUnionGateTimeoutEnvName overrides the per-command budget: a slow
	// compile must never hold the merge queue hostage (Rule 2: ONE reader).
	mergeUnionGateTimeoutEnvName = "GOPI_MERGE_UNION_GATE_TIMEOUT_SEC"

	// mergeUnionGateScratchPattern names the gate's per-call scratch inside the
	// runtime scratch class (the GC recognises the class and bounds a crashed
	// merger's leftovers).
	mergeUnionGateScratchPattern = "merge-union-gate-"

	// mergeUnionGateUnionDirName / mergeUnionGateBranchDirName /
	// mergeUnionGateTargetDirName are the three checkouts inside the per-call
	// scratch: the merge result, and (only when the union failed) the branch
	// alone and the target alone. Distinct names by construction — a shared one
	// would let the premise measurement read the other's tree.
	mergeUnionGateUnionDirName  = "union"
	mergeUnionGateBranchDirName = "branch"
	mergeUnionGateTargetDirName = "target"

	// mergeUnionGateGoBinaryName / mergeUnionGateTarBinaryName /
	// mergeUnionGateGitBinaryName are the three tools the measurement needs;
	// a missing one ABSTAINS (a host that cannot compile is not evidence about
	// a branch).
	mergeUnionGateGoBinaryName  = testcompile.GoBinary
	mergeUnionGateTarBinaryName = "tar"
	mergeUnionGateGitBinaryName = "git"

	// mergeUnionGateTarExtractFlags extract a tar stream from stdin (`-f -`).
	mergeUnionGateTarExtractFlagFile    = "-f"
	mergeUnionGateTarExtractFlagStdin   = "-"
	mergeUnionGateTarExtractFlagExtract = "-x"

	// The git plumbing verbs and flags (Rule 1b: dispatched-on values are
	// constants). merge-tree's `--write-tree` is the flag that makes it perform
	// a REAL three-way merge and print the resulting tree, instead of the
	// legacy trivial-merge report; the archive/rev-parse verbs resolve and
	// materialise a revision the same gate reads alone.
	mergeUnionGateMergeTreeVerb      = "merge-tree"
	mergeUnionGateMergeTreeWriteFlag = "--write-tree"
	mergeUnionGateArchiveVerb        = "archive"
	mergeUnionGateRevParseVerb       = "rev-parse"
	mergeUnionGateTreeSuffix         = "^{tree}"

	// mergeUnionGateGitUnknownOptionText / mergeUnionGateGitUnknownRevText are
	// what git prints when it does not know the `--write-tree` flag — the
	// measured shapes of "this git is older than the merge-tree --write-tree
	// contract", and BOTH are needed because two generations refuse it in
	// different words: git 2.34.1 answers `fatal: unknown rev --write-tree`
	// (the older parser routes the flag to revision resolution; measured on
	// peer1 2026-10-13), while a newer parser's own option table answers
	// `unknown option`. They are checked ONLY on the union call (the sole call
	// that passes a flag younger than `git merge-base`), so an unreadable
	// revision cannot be mistaken for an old toolchain by a later call's text.
	// The `--write-tree` flag landed in git 2.38; the host the builder was
	// written on ran 2.43.0.
	mergeUnionGateGitUnknownOptionText = "unknown option"
	mergeUnionGateGitUnknownRevText    = "unknown rev"

	// mergeUnionGateGoTestCompileCmd renders the compile-ONLY test pass. The
	// filter matches no test, so every test binary is BUILT and nothing is run:
	// no ledger is opened, no daemon started, no fixture executed. This is the
	// command that compiles `_test.go` files — exactly what `go build` skips and
	// what the 2026-09-16 collision needed.
	//
	// ITS SPELLING IS NOT THIS PACKAGE'S. It is internal/testcompile's ONE
	// definition (Rules 13/14), because the clone channel's delivery loop runs
	// the SAME command out of internal/devworkspace — and that package cannot
	// import this one (`internal/tools` imports `internal/devworkspace` in 46
	// files; devworkspace imports tools in none). Before it was hoisted, this
	// gate and the build gate held two `Sprintf` formats and two literals of one
	// command, which is exactly the drift the leaf package removes. The
	// "format" this constant renders into still exists because this gate's leg
	// is composed with its own toolchain-word constant; the WORD and the FILTER
	// are the leaf package's.
	mergeUnionGateGoTestCompileCmdFmt = testcompile.CommandFormat
	mergeUnionGateTestRunNone         = testcompile.RunNothingFilter

	// A VET LEG WAS DELIBERATELY REJECTED. `go vet ./...` was the incident's own
	// repro command and it does catch a duplicate declaration — but so does the
	// test compile above, and a vet leg buys a class of failures that are NOT
	// merge hazards while costing the comparison its symmetry: in Go 1.24
	// `unsafe.Pointer`→`uintptr` conversions in test files are reported (a field
	// offset is inherently unsafe), so a vet leg would refuse branches whose
	// union is byte-identical in the failing package to master. The refusal must
	// stay as narrow as the defect it closes.

	// mergeUnionGateEnvAbsenceErr is the ABSTAIN reason when this host cannot
	// perform the measurement at all.
	mergeUnionGateEnvAbsenceErr = "this host cannot perform the union measurement (the toolchain is unavailable, or its commands cannot be built)"

	// mergeUnionGateUnresolvedModulesErr is the ABSTAIN reason for a union
	// compile that could not RESOLVE its module set: the tree was never
	// compiled, so the failure is not a statement about it.
	mergeUnionGateUnresolvedModulesErr = "the union compile could not resolve its modules in the gate's caches (no reachable module proxy) — the merge result was never compiled"

	// mergeUnionGateUnresolvedTreeText stands in for the diagnostics when the
	// union has textual conflicts — the resolver lane's case, never a refusal
	// here.
	mergeUnionGateUnresolvedTreeText = "(the union tree could not be written — it has textual conflict stages, which the resolver lane owns)"

	// mergeUnionGateOldGitText is the ABSTAIN reason when this host's git does
	// not implement `merge-tree --write-tree`. There is deliberately NO fallback
	// to the replaced `read-tree -m` builder: that primitive cannot tell a clean
	// two-sided hunk split from a conflict (it leaves stages for both), so
	// falling back would re-install the very false abstention this gate was
	// fixed for. An older git abstains with a reason instead.
	mergeUnionGateOldGitText = "this host's git does not implement `merge-tree --write-tree` (older than git 2.38), so the union tree can be materialised by no primitive that distinguishes a clean two-sided hunk split from a conflict"

	// mergeUnionGateConflictPathLimit caps the conflict path list repeated in
	// the log (a wide conflict must not turn one log line into a file listing).
	mergeUnionGateConflictPathLimit = 8

	// mergeUnionGateGitTimeout bounds each git plumbing call inside the gate.
	mergeUnionGateGitTimeout = 30 * time.Second
)

// mergeUnionGateConflictStageShape matches one of `git merge-tree`'s unmerged
// stage records for a conflicted path: `<mode> <oid> <stage>\t<path>` (the same
// shape `git ls-files -u` prints). It is what makes the conflict reader
// PROOF-BEARING rather than text-sniffing: only a record of this shape can
// contribute a path, so a failure message that happens to mention a conflict
// yields no paths and stays an abstention with git's own reason.
var mergeUnionGateConflictStageShape = regexp.MustCompile(`^[0-7]{6} [0-9a-f]+ [123]\t`)

// mergeUnionGateRefuseDetailFmt renders the refusal: the union's own output head
// (the compiler's text — the payload's evidence), the branch whose own test
// build PASSED (measured, not assumed), and the target it was combined with.
const mergeUnionGateRefuseDetailFmt = "the MERGE RESULT of %s into %s does not compile, although the branch's own test build does — %s (master unchanged)"

// mergeUnionGateBranchFailText is the ABSTAIN log line for a branch whose own
// test build fails: the union is red, but so is the branch alone, so the failure
// is not union-introduced (the ⓪b lane and the rework route own it).
const mergeUnionGateBranchFailText = "the branch's OWN test build fails too, so the union failure is not union-introduced"

// mergeUnionGateVerdict is the union gate's decision for one branch.
type mergeUnionGateVerdict struct {
	// Refused is true when the branch must not be merged: its own test build
	// passes and the merge result's does not.
	Refused bool
	// Detail is the refusal reason ("" unless Refused).
	Detail string
	// BuildCmd is the command whose failure refused the branch, so the reworker
	// verifies with the SAME one (the build gate's refusal contract).
	BuildCmd string
	// BranchErr is the union compile failure's diagnostics (the compiler's own
	// text).
	BranchErr string
}

// mergeUnionGateLeg is one compile-only command the union pass runs.
type mergeUnionGateLeg struct {
	// Cmd is the command line (run through sh -c).
	Cmd string
	// Command is the bare command word (for the toolchain log lines).
	Command string
}

// mergeUnionGateSpec is the measurable command set for a repo. It is a LIST
// because the incident needed two commands (build-then-vet) and a repo may
// legitimately need one; WHICH command failed is the reworker's most useful
// fact.
type mergeUnionGateSpec struct {
	// Legs are the commands the union pass runs, in order.
	Legs []mergeUnionGateLeg
	// PremiseLeg is the leg whose PASS is the comparison's premise — measured on
	// the branch's own tree when the union fails. It is the TEST-COMPILE leg by
	// construction: "the branch builds alone" must be the same measurement as
	// the one that failed, or the comparison would be between two different
	// questions.
	PremiseLeg mergeUnionGateLeg
	// TimeoutSec bounds EACH command (<=0 = no timeout).
	TimeoutSec int
}

// mergeUnionGate asks the comparative union question for one branch: does the
// MERGE RESULT of branch into master compile? It refuses only when the union
// fails and the branch's own test build passes.
func mergeUnionGate(repo, master, branch string) mergeUnionGateVerdict {
	if mergeUnionGateDisabled() {
		return mergeUnionGateVerdict{}
	}
	// A branch the target already CONTAINS adds nothing: the union is master
	// itself, so there is no merge-result defect to find (the same containment
	// read the ⓪b gate makes, and the ordinary shape of a branch created from
	// master and never committed to). One ancestry read replaces a whole compile
	// AND keeps the verdict wording truthful.
	if mergeBuildGateBranchAlreadyInTarget(repo, branch, master) {
		mergeUnionGateLog("ABSTAINS for %s (%s already contains %s — the branch adds nothing, so there is no merge result to compile) — merging unchanged",
			branch, master, branch)
		return mergeUnionGateVerdict{}
	}
	spec, ok, err := mergeUnionGateResolveSpec(repo)
	if err != nil {
		mergeUnionGateLog("ABSTAINS for %s (compile contract unreadable: %v) — merging unchanged", branch, err)
		return mergeUnionGateVerdict{}
	}
	if !ok {
		mergeUnionGateLog("ABSTAINS for %s (%s: no compile-only command set for this repo) — merging unchanged",
			branch, mergeBuildGateStatusNoContract)
		return mergeUnionGateVerdict{}
	}
	if err := mergeUnionGateToolchainAvailable(); err != nil {
		mergeUnionGateLog("ABSTAINS for %s (%s: %v) — merging unchanged", branch, mergeUnionGateEnvAbsenceErr, err)
		return mergeUnionGateVerdict{}
	}
	cacheDir, okCache := mergeBuildGateGoCache(repo)
	if !okCache {
		mergeUnionGateLog("ABSTAINS for %s (no usable shared Go build cache — the reason is logged above) — merging unchanged", branch)
		return mergeUnionGateVerdict{}
	}
	modCacheDir, okModCache := mergeBuildGateSharedModCache()
	if !okModCache {
		// A Go module whose modules cannot be resolved would fetch over the
		// network — the 2026-09-15 false-refusal family. ABSTAIN, naming the
		// reason the sibling gate already logged.
		mergeUnionGateLog("ABSTAINS for %s (no populated shared module cache — the reason is logged above) — merging unchanged", branch)
		return mergeUnionGateVerdict{}
	}
	workRoot, cleanup, okScratch := mergeUnionGateScratch(repo)
	if !okScratch {
		mergeUnionGateLog("ABSTAINS for %s (no usable scratch tree for the union checkout) — merging unchanged", branch)
		return mergeUnionGateVerdict{}
	}
	defer cleanup()

	tree, conflicts, abortReason, okTree := mergeUnionGateTree(repo, master, branch)
	if !okTree {
		mergeUnionGateLog("ABSTAINS for %s (%s) — merging unchanged", branch, abortReason)
		return mergeUnionGateVerdict{}
	}
	if conflicts != "" {
		mergeUnionGateLog("ABSTAINS for %s (%s: %s) — merging unchanged", branch, mergeUnionGateUnresolvedTreeText, conflicts)
		return mergeUnionGateVerdict{}
	}
	unionDir := filepath.Join(workRoot, mergeUnionGateUnionDirName)
	if err := mergeUnionGateExtract(repo, tree, unionDir); err != nil {
		mergeUnionGateLog("ABSTAINS for %s (materialising the union tree failed: %v) — merging unchanged", branch, err)
		return mergeUnionGateVerdict{}
	}

	// (3) the compile-only pass over the MERGE RESULT. Both legs run even after
	// a failure, because which one failed is real information (a duplicate
	// declaration and an undefined symbol are reported by different passes).
	var firstFail *mergeUnionGateLegResult
	for i := range spec.Legs {
		leg := spec.Legs[i]
		// ONE line at the START of each leg (see mergeBuildGateLegStartFmt for
		// the measured 260.8s silent window this closes): a leg that is slow and
		// a leg that is wedged read identically when only the RESULT line is
		// logged.
		mergeUnionGateLog("leg START `%s` on the union of %s (budget %ds)", leg.Cmd, branch, spec.TimeoutSec)
		res := mergeUnionGateRun(unionDir, leg, spec.TimeoutSec, cacheDir, modCacheDir)
		switch res.Status {
		case mergeBuildGateStatusAbstain:
			mergeUnionGateLog("ABSTAINS for %s (%s could not be measured: %s) — merging unchanged", branch, leg.Cmd, res.Err)
			return mergeUnionGateVerdict{}
		case mergeBuildGateStatusFail:
			if firstFail == nil {
				firstFail = &mergeUnionGateLegResult{leg: leg, res: res}
			}
		}
	}
	if firstFail == nil {
		mergeUnionGateLog("PASS for %s (the merge result of %s into %s compiles)", branch, branch, master)
		return mergeUnionGateVerdict{}
	}

	// (4) the union FAILED — measure the PREMISE before believing it was the
	// combination. The refusal means "the COMBINATION broke it", so BOTH sides
	// are measured with the SAME command:
	//   - the branch's own tree: a branch that fails alone is branch-introduced,
	//     not union-introduced (ABSTAIN — its break belongs to the ⓪b lane and
	//     the rework route);
	//   - master's own tree: a branch-and-union failure says nothing when the
	//     TEST BUILD is already red there (measured 2026-09-16: master's own
	//     `go test -run '^$' -count=1 ./...` exits 0 today, but the state is
	//     REACHABLE — one test-only break landing on master turns every
	//     candidate into "branch and union red"). Enforcing a red target would
	//     freeze the whole queue for a failure no branch caused — the exact rule
	//     the ⓪b gate learned on 2026-09-13 and this gate must not relearn.
	// Both measurements are paid only on the path that is about to refuse.
	alone := mergeUnionGateTreeAlone(repo, branch, branch, spec.PremiseLeg, spec.TimeoutSec, workRoot, mergeUnionGateBranchDirName, cacheDir, modCacheDir)
	switch alone.Status {
	case mergeBuildGateStatusAbstain:
		mergeUnionGateLog("ABSTAINS for %s (the branch-alone test build could not be measured: %s) — merging unchanged", branch, alone.Err)
		return mergeUnionGateVerdict{}
	case mergeBuildGateStatusFail:
		mergeUnionGateLog("ABSTAINS for %s (%s) — merging unchanged", branch, mergeUnionGateBranchFailText)
		return mergeUnionGateVerdict{}
	}
	target := mergeUnionGateTreeAlone(repo, master, master, spec.PremiseLeg, spec.TimeoutSec, workRoot, mergeUnionGateTargetDirName, cacheDir, modCacheDir)
	if target.Status != mergeBuildGateStatusPass {
		mergeUnionGateLog("ABSTAINS for %s (branch fails the union, but target %s did not PASS its own test build either — %s) — not branch-introduced, merging unchanged",
			branch, master, mergeUnionGateOutcomeReason(target))
		return mergeUnionGateVerdict{}
	}

	errText := mergeBuildGateErrorText(firstFail.res.Output)
	if errText == "" {
		errText = mergeBuildGateAbstainBuildText
	}
	return mergeUnionGateVerdict{
		Refused:   true,
		Detail:    fmt.Sprintf(mergeUnionGateRefuseDetailFmt, branch, master, errText),
		BuildCmd:  firstFail.leg.Cmd,
		BranchErr: errText,
	}
}

// mergeUnionGateOutcomeReason renders an ABSTAIN outcome's reason for a log
// line, falling back to its status when the reason is empty (an outcome always
// carries at least one of the two).
func mergeUnionGateOutcomeReason(out mergeBuildGateOutcome) string {
	if out.Err != "" {
		return mergeBuildGateTail(out.Err)
	}
	return out.Status
}

// mergeUnionGateTreeAlone measures ONE revision's own test build in its own
// scratch checkout, with the spec's premise leg. Extracting the tip through
// `git archive` keeps the repo untouched (no checkout, no worktree) — every
// revision here is consulted as objects.
func mergeUnionGateTreeAlone(repo, rev, label string, leg mergeUnionGateLeg, timeoutSec int, workRoot, dirName, goCache, modCache string) mergeBuildGateOutcome {
	// The tree is the WHOLE answer here, so it is trimmed before it becomes an
	// argument: `git rev-parse` ends its line, and an untrimmed value reaches
	// `git archive` as "<oid>\n" — which git answers with `not a valid object
	// name` (measured: it turned this leg into an abstention for every union
	// that failed to compile).
	tree, err := mergeUnionGateGit(repo, mergeUnionGateRevParseVerb, rev+mergeUnionGateTreeSuffix)
	tree = strings.TrimSpace(tree)
	if err != nil || tree == "" {
		return mergeBuildGateOutcome{Status: mergeBuildGateStatusAbstain,
			Err: fmt.Sprintf("resolving %s's tree failed: %s", label, mergeBuildGateTail(errTextOr(err, "no tree hash")))}
	}
	dir := filepath.Join(workRoot, dirName)
	if err := mergeUnionGateExtract(repo, tree, dir); err != nil {
		return mergeBuildGateOutcome{Status: mergeBuildGateStatusAbstain,
			Err: fmt.Sprintf("materialising %s's tree failed: %v", label, err)}
	}
	return mergeUnionGateRun(dir, leg, timeoutSec, goCache, modCache)
}

// errTextOr renders err's text, or fallback when err is nil.
func errTextOr(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	return err.Error()
}

// mergeUnionGateLegResult pairs a leg with its failed measurement.
type mergeUnionGateLegResult struct {
	leg mergeUnionGateLeg
	res mergeBuildGateOutcome
}

// mergeUnionGateResolveSpec resolves the compile-only command set for repo. A Go
// module gets the two commands that compile every package's TEST files; a repo
// that is not a Go module has no KNOWN way to compile its tests (the declared
// build is all the toolchain knowledge we have) and ABSTAINS — never a guess
// (Rule 12's declared-not-discovered rule).
func mergeUnionGateResolveSpec(repo string) (mergeUnionGateSpec, bool, error) {
	if !mergeBuildGateIsGoRepo(repo) {
		return mergeUnionGateSpec{}, false, nil
	}
	timeout, err := mergeUnionGateTimeout()
	if err != nil {
		return mergeUnionGateSpec{}, false, err
	}
	testCompile := mergeUnionGateLeg{
		Cmd:     fmt.Sprintf(mergeUnionGateGoTestCompileCmdFmt, mergeUnionGateGoBinaryName, mergeUnionGateTestRunNone),
		Command: mergeUnionGateGoBinaryName,
	}
	return mergeUnionGateSpec{
		Legs:       []mergeUnionGateLeg{testCompile},
		PremiseLeg: testCompile,
		TimeoutSec: timeout,
	}, true, nil
}

// mergeUnionGateTimeout resolves the per-command budget: the operator's override
// when it is a positive integer, else the default. Rule 2 — this is the ONE
// reader of mergeUnionGateTimeoutEnvName, and an unparsable value is an ERROR
// (the gate abstains with the reason), never a silent fallback to the default.
func mergeUnionGateTimeout() (int, error) {
	raw := strings.TrimSpace(os.Getenv(mergeUnionGateTimeoutEnvName))
	if raw == "" {
		return mergeUnionGateDefaultTimeoutSec, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s=%q is not a positive number of seconds", mergeUnionGateTimeoutEnvName, raw)
	}
	return n, nil
}

// mergeUnionGateDefaultTimeoutSec bounds EACH compile-only command when the
// operator declares no budget. It mirrors the build gate's own default so the
// two gates never disagree about how long a measurement may take (the test
// compile reuses the same build cache, so it is the same order of work).
const mergeUnionGateDefaultTimeoutSec = mergeBuildGateDefaultTimeoutSec

// mergeUnionGateToolchainAvailable reports whether this host can perform the
// measurement at all: the git plumbing, the tar extractor and the Go toolchain
// must all exist. An absent tool is an ABSTENTION, never a refusal — a branch is
// not broken because the merger's host cannot compile it.
func mergeUnionGateToolchainAvailable() error {
	for _, tool := range []string{mergeUnionGateGitBinaryName, mergeUnionGateTarBinaryName, mergeUnionGateGoBinaryName} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s: %w", tool, err)
		}
	}
	return nil
}

// mergeUnionGateScratch prepares the gate's per-call scratch (the three
// checkouts live here) and its cleanup.
func mergeUnionGateScratch(repo string) (workRoot string, cleanup func(), ok bool) {
	classDir := scratch.RuntimeScratchDir(scratch.RuntimeClassMergeBuild)
	if classDir == "" {
		return "", func() {}, false
	}
	workRoot, err := scratch.MkdirTempIn(classDir, mergeUnionGateScratchPattern)
	if err != nil {
		taskNotifyLog("merger: UNION gate scratch unusable: %v", err)
		return "", func() {}, false
	}
	return workRoot, mergeUnionGateScratchCleanup(repo, workRoot), true
}

// mergeUnionGateScratchCleanup removes the per-call scratch. This gate creates
// NO worktree (the union lives in an archive-extracted directory), so unlike the
// build gate's cleanup there is no git registration of its own to reap. The reap
// still runs — a registration under this root can only come from a crashed ⓪b
// call in the SAME class directory, and reaping it here is free — and the
// removal failure is REPORTED (Rule 15b).
func mergeUnionGateScratchCleanup(repo, workRoot string) func() {
	return func() {
		if err := os.RemoveAll(workRoot); err != nil {
			taskNotifyLog("merger: UNION gate scratch %s not removed: %v", workRoot, err)
		}
		mergeBuildGateReapRegistrations(repo, workRoot)
	}
}

// mergeUnionGateTree materialises the MERGE RESULT's tree from git objects with
// `git merge-tree --write-tree <branch> <master>`: git performs the three-way
// merge itself and prints the result's tree id. It never touches the repo's own
// index or working tree.
//
// The argument order is the merge's own grammar (`git merge <branch>` run from
// master: the branch is merged INTO master), and for two cleanly-merging trees
// the result is the same either way — which is what makes this a faithful
// stand-in for landing the branch on master.
//
// Returns (tree, conflictPaths, abortReason, ok). ok=false is an abstention with
// abortReason set; ok=true with a non-empty conflictPaths is
// MERGE_RESULT_UNAVAILABLE (a textual conflict — the resolver lane owns it).
//
// A CONFLICT IS THE COMMAND'S OWN NON-ZERO EXIT, not a stage count: both
// readings are possible on `read-tree -m` (the builder this replaced), where a
// clean two-sided hunk split leaves exactly the same stages as a real conflict.
// merge-tree decides it, and its output lists the conflicted paths on the same
// stream it prints the tree on.
func mergeUnionGateTree(repo, master, branch string) (tree, conflicts, abortReason string, ok bool) {
	out, err := mergeUnionGateGit(repo, mergeUnionGateMergeTreeVerb, mergeUnionGateMergeTreeWriteFlag, branch, master)
	if err != nil {
		if mergeUnionGateGitTooOldForWriteTree(err, out) {
			return "", "", mergeUnionGateOldGitText, false
		}
		if paths := mergeUnionGateConflictPaths(out); paths != "" {
			return "", paths, "", true
		}
		// Not a conflict and not an old toolchain: a refused argument, a
		// missing revision, an unreadable object store. The gate could not
		// perform the measurement — ABSTAIN with git's own reason.
		return "", "", fmt.Sprintf("%s %s failed: %s", mergeUnionGateMergeTreeVerb, mergeUnionGateMergeTreeWriteFlag, mergeBuildGateTail(err.Error())), false
	}
	tree = mergeUnionGateTreeLine(out)
	if tree == "" {
		return "", "", fmt.Sprintf("%s %s produced no tree hash", mergeUnionGateMergeTreeVerb, mergeUnionGateMergeTreeWriteFlag), false
	}
	return tree, "", "", true
}

// mergeUnionGateGitTooOldForWriteTree reports whether a failed merge-tree call
// failed because THIS GIT does not know `--write-tree` (the flag is git 2.38+):
// git's own refusal to read the flag — `unknown option` on a newer parser,
// `unknown rev` on git 2.34.1 (measured on peer1 2026-10-13). It is read on
// that call alone, so a
// revision that does not resolve (a different message) can never be reported as
// an old toolchain.
func mergeUnionGateGitTooOldForWriteTree(err error, out string) bool {
	if err == nil {
		return false
	}
	return strings.Contains(out, mergeUnionGateGitUnknownOptionText) ||
		strings.Contains(out, mergeUnionGateGitUnknownRevText)
}

// mergeUnionGateTreeLine returns the tree id from a successful `merge-tree
// --write-tree` run: the FIRST line of its stdout is the tree, and every later
// line belongs to the conflict/informational report — which is not this gate's
// business on a clean merge.
func mergeUnionGateTreeLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return line
	}
	return ""
}

// mergeUnionGateConflictPaths renders the paths a failed `merge-tree --write-tree`
// named, as a bounded, deduped list — the evidence that the union is a TEXTUAL
// conflict and therefore the resolver lane's business. The lines it reads are
// the unmerged stage records git prints for a conflicted merge
// (`<mode> <oid> <stage>\t<path>`); an output carrying no such record yields "",
// which is what keeps this reader from inventing a conflict out of a failure
// that was not one.
func mergeUnionGateConflictPaths(out string) string {
	seen := map[string]bool{}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if !mergeUnionGateConflictStageShape.MatchString(line) {
			continue
		}
		_, path, found := strings.Cut(line, "\t")
		if !found {
			continue
		}
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		if len(paths) >= mergeUnionGateConflictPathLimit {
			paths = append(paths, "…")
			break
		}
		paths = append(paths, path)
	}
	return strings.Join(paths, ", ")
}

// mergeUnionGateGit runs one git plumbing command in repo, and returns the
// combined output (stdout + stderr): `merge-tree` writes its verdict to stdout
// while git's own diagnostics go to stderr, and BOTH carry reasons this gate
// must read (the conflicted path list, and an old toolchain's refusal). A
// non-zero exit is returned as an error, never logged away (Rule 15b).
func mergeUnionGateGit(repo string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mergeUnionGateGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, mergeUnionGateGitBinaryName, append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// mergeUnionGateExtract materialises tree into dir: `git archive <tree>` piped
// into `tar -x`. It is the sibling of the build gate's materialisation step
// (mergeBuildGateBuild) and differs in exactly the way that matters: NO WORKTREE
// IS CREATED, so the repo gains no registration and the cleanup has nothing of
// this gate's to repair.
func mergeUnionGateExtract(repo, tree, dir string) error {
	if err := os.MkdirAll(dir, scratch.DirPerm); err != nil {
		return fmt.Errorf("mkdir checkout: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), mergeUnionGateGitTimeout)
	defer cancel()

	archive := exec.CommandContext(ctx, mergeUnionGateGitBinaryName, "-C", repo, mergeUnionGateArchiveVerb, tree)
	var archiveErr bytes.Buffer
	archive.Stderr = &archiveErr
	untar := exec.CommandContext(ctx, mergeUnionGateTarBinaryName,
		mergeUnionGateTarExtractFlagExtract, mergeUnionGateTarExtractFlagFile, mergeUnionGateTarExtractFlagStdin)
	untar.Dir = dir
	var untarErr bytes.Buffer
	untar.Stderr = &untarErr

	pipe, err := archive.StdoutPipe()
	if err != nil {
		return fmt.Errorf("git archive pipe: %w", err)
	}
	untar.Stdin = pipe
	if err := untar.Start(); err != nil {
		return fmt.Errorf("%s start: %w", mergeUnionGateTarBinaryName, err)
	}
	if err := archive.Start(); err != nil {
		// The extractor is already running with a pipe nobody will write to;
		// its Wait is the only release (Rule 15b: the error is REPORTED by the
		// caller, the dangling process is reaped here).
		_ = untar.Wait() //stylegate:ignore only releases the dangling extractor; the archive start error below is the reported one
		return fmt.Errorf("git archive start: %w", err)
	}
	archiveWait := archive.Wait()
	untarWait := untar.Wait()
	if archiveWait != nil {
		return fmt.Errorf("git archive %s: %w: %s", tree, archiveWait, mergeBuildGateTail(archiveErr.String()))
	}
	if untarWait != nil {
		return fmt.Errorf("%s extract: %w: %s", mergeUnionGateTarBinaryName, untarWait, mergeBuildGateTail(untarErr.String()))
	}
	return nil
}

// mergeUnionGateRun runs ONE compile-only command in a checkout, with the gate's
// own Go caches and the build gate's environment contract.
func mergeUnionGateRun(dir string, leg mergeUnionGateLeg, timeoutSec int, goCache, modCache string) mergeBuildGateOutcome {
	var ctx context.Context
	var cancel context.CancelFunc
	if timeoutSec > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", leg.Cmd)
	cmd.Dir = dir
	// The environment comes from the ⓪b gate's ONE builder — GOCACHE = the
	// SHARED, disk-backed, bounded cache; GOMODCACHE = the shared read-only one;
	// the caller's GIT_CONFIG_* normalised — so a merge probe can never fill the
	// operator's caches and the two gates can never disagree about where a build
	// reads from (Rule 14: one builder, never a second spelling).
	cmd.Env = mergeBuildGateEnv(goCache, modCache)
	// Process-group isolation + bounded pipes: the same two guards the build
	// gate measured (a hung command is killed with its children at the deadline,
	// and a GRANDCHILD holding the pipes cannot make Wait outlive the budget).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = mergeBuildGateWaitDelay
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil && cmd.Process != nil {
		// The group id is RE-VERIFIED before delivery: it is this child's pid, and the
		// kernel may have recycled that number to an unrelated group (see killGroupGuard.go).
		_, _ = signalProcessGroup(cmd.Process.Pid, cmd.Process.Pid, syscall.SIGKILL) //stylegate:ignore signal to a process group we are already abandoning; ESRCH (the group already gone) is the expected error
	}
	if ctx.Err() == context.DeadlineExceeded {
		return mergeBuildGateOutcome{Status: mergeBuildGateStatusAbstain,
			Err: fmt.Sprintf("%s exceeded its %ds budget (a timeout is not a compile error)", leg.Cmd, timeoutSec)}
	}
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return mergeUnionGateJudgeFailure(dir, string(out))
		}
		// The command could not be started at all: a measurement failure, never
		// a compile failure — ABSTAIN (the build gate's own rule).
		return mergeBuildGateOutcome{Status: mergeBuildGateStatusAbstain,
			Err: fmt.Sprintf("%s could not be started: %v", leg.Cmd, err)}
	}
	return mergeBuildGateOutcome{Status: mergeBuildGateStatusPass, Output: mergeBuildGateErrorText(string(out))}
}

// mergeUnionGateJudgeFailure maps a non-zero compile-only command run in dir to
// ABSTAIN or FAIL: a failure whose text is the environment's refusal to RESOLVE the module
// set, or the filesystem's refusal to WRITE the output, is a measurement the
// gate could not make — the same predicates and the same reasons as the build
// gate's (Rule 14: one predicate, never a second spelling) — and everything else
// is a real compile failure.
//
// The disk-exhaustion arm is the SAME false refusal the ⓪b gate carried until
// 2026-10-12: this gate's legs are `go` commands that LINK test binaries, so a
// full filesystem kills them at the link stage with no compiler diagnostic, and
// before the class existed the union gate refused (archived) a branch for it.
func mergeUnionGateJudgeFailure(dir, output string) mergeBuildGateOutcome {
	if mergeBuildGateUnresolvedModules(output) {
		return mergeBuildGateOutcome{Status: mergeBuildGateStatusAbstain, Output: output,
			Err: mergeUnionGateUnresolvedModulesErr}
	}
	if marker, yes := mergeBuildGateDiskExhausted(output); yes {
		return mergeBuildGateOutcome{Status: mergeBuildGateStatusAbstain, Output: output,
			Err: mergeBuildGateDiskExhaustedErr(marker, dir)}
	}
	return mergeBuildGateOutcome{Status: mergeBuildGateStatusFail, Output: output}
}

// mergeUnionGateLog renders one verdict line. The prefix is a constant so every
// line the gate emits is greppable as one family — an operator reading the
// merger log must be able to see WHICH measurement produced a refusal.
func mergeUnionGateLog(format string, args ...any) {
	taskNotifyLog("merger: UNION gate "+format, args...)
}

// mergeUnionGateDisabled reports whether the operator escape hatch is set.
// Rule 2: this is the ONE reader of mergeUnionGateEnvName; unset (or any other
// value) means ENABLED.
func mergeUnionGateDisabled() bool {
	return strings.TrimSpace(os.Getenv(mergeUnionGateEnvName)) == mergeUnionGateDisabledValue
}
