package tools

// The close-out's STYLEGATE ARM — the style ratchet (internal/stylegate), run on
// the branch's own revision WITHOUT the branch having to touch that package.
//
// THE DEFECT THIS CLOSES (measured on this repo, 2026-10-30, three instances in
// one night from three unrelated rows). The ratchet's ceiling is a WHOLE-TREE
// reading: `ignored-error` counts every discarded error in every package. The
// branch's own check, and the merge/verify smoke path, are BRANCH-SCOPED —
// `smoke.TouchedPackages` selects only the packages the diff touched. So a row
// that adds ONE unannotated `_ = p.cmd.Wait()` to `testsuite/` or `cmd/agent/`
// pushes the tree-wide reading over the ceiling while the round that introduced
// it runs `./testsuite/...` or `./cmd/agent/...` and sees nothing. The red is
// deferred to whoever next happens to run `./internal/stylegate` — measured
// instances: `testsuite/chat_pty_drain_test.go:109`, `cmd/agent/
// ground_kill_identity_test.go:156`, `cmd/agent/closeout_reap_split_test.go:100`.
// None of the three was found by its own author.
//
// WHAT THIS IS NOT (Rule 35 — zero new persistent dependency). No new script, no
// Makefile target, no hook, no resident, no cron line. It is ONE MORE MEMBER of
// the check list this lane ALREADY builds and runs (`checks` in
// finishCleanupBranchCheckAt), on the SAME ground, through the SAME runner
// (`finishCleanupChecksIn`) and the SAME declared entry point (`make test-budget
// PKGS='…'`, rendered by smokegate.ScopedForPackages). Nothing ticks, and nothing
// new has to be remembered.
//
// WHY IT NAMES THE GATE'S PACKAGE RATHER THAN A DIFFERENT SUITE. The check must
// run in the round that introduces the drift, whatever that round touched, so it
// cannot be an ADDITION to the touched-package selection (that is the selection
// that missed it). It is a SECOND check, addressed at the ONE package that
// carries the ratchet, appended to the same list — the same shape, and for the
// same reason, as the census arm beside it (task_finish_premerge_census.go).
//
// THE RATCHET IS TWO-WAY, and that is the gate's business, not this arm's: an
// annotation added without lowering the ceiling fails just as an unannotated site
// raising the reading does (`TestEveryCeilingEqualsItsMeasurement`). Both
// directions are caught by running the package, which is all this arm does.
//
// AN ABSENT PACKAGE ABSTAINS WITH ITS OWN SENTENCE (Rule 15). A tree that carries
// no `internal/stylegate/` — a fixture, a nested module, another project — is not
// this repo's shape, and the lane never invents a package pattern for someone
// else's tree. It says so instead of silently running nothing.
//
// ITS SIBLING IS NOT A DUPLICATE (Rule 26 — state the overlap). The server side
// already measures this invariant AFTER a landing: `mergeset_stylegate.go`
// (commit 3a3d79d) adds the ratchet to the merged-result gate set whenever the
// landing touches any .go file, on the merged revision, and REPORTS a red through
// notify plus a deduped bug row. That leg cannot do this arm's job and this arm
// cannot do that one's: the mergeset leg runs on the SERVER, on a revision that
// has ALREADY landed, and it never refuses anything — by the time it speaks, the
// unannotated site is on master and the cleanup belongs to whoever reads the
// notice. This arm runs on the AUTHOR'S ground, on the revision being pushed, and
// a red here is a REFUSAL: the branch keeps its commits and never lands red. The
// two are the two halves of one invariant (catch it before landing; prove it
// still holds after), and neither can be derived from the other.

import (
	"os"
	"path/filepath"

	"github.com/nickhuang99/gopi/internal/smoke"
)

const (
	// styleGatePackageDir is the ONE package that carries the style ratchet's
	// ceilings (`gateCeilings` in gate_test.go) and the arms that compare them
	// with a whole-tree measurement. It is repo-relative and slash-separated
	// because it is rendered into a go-test package pattern (Rule 1b: the string
	// carries meaning, so it is named here rather than typed at the call site).
	//
	// IT IS AN ALIAS OF THE MUST-RUN ROSTER'S ENTRY, not a second spelling
	// (Rules 13/14): `smoke.GatePackageDirs` is the ONE definition of which gate
	// packages a delivery cannot skip, and this arm addresses one MEMBER of it.
	// Two literals for the same directory is exactly the drift that lets a rename
	// leave one of them behind.
	styleGatePackageDir = smoke.GatePackageStylegate

	// styleGateCheckName is how the check list names this arm. It is the name an
	// operator reads on a refusal and the name the tests below match on, so it is
	// spelled once.
	styleGateCheckName = "stylegate-ratchet"

	// styleGateAbsentMark is the abstention sentence for a tree with no ratchet
	// package: a measurement, never a silent pass (Rule 15).
	styleGateAbsentMark = "this tree carries no stylegate ratchet gate package"
)

// finishCleanupBranchStylegateCheck is the stylegate arm of the branch's own
// pre-push check: the package holding the style ratchet, appended to the check
// list finishCleanupBranchCheckTest already builds.
//
// The returned bool is false when this arm must NOT become a check at all (the
// tree does not carry the package): the caller reports its own abstention, and
// this arm contributes nothing rather than an empty command.
//
// It deliberately does NOT take the branch or the base: the question is not "what
// did this branch change" (that selection is what let the drift through) but "is
// the whole-tree style reading still within its ceiling", which is a question
// about the tree ON THE GROUND and is answerable without a diff. That is also why
// the arm is UNCONDITIONAL in the caller's list: no diff may decide whether a
// whole-tree reading is taken.
func finishCleanupBranchStylegateCheck(dir string) (mergeCheck, bool) {
	if !finishCleanupBranchStyleGatePackagePresent(dir) {
		return mergeCheck{}, false
	}
	return mergeCheck{
		// The pattern is composed by the ONE composer (smoke.PackagePattern): the
		// root's pattern is deliberately non-recursive, and a second spelling of
		// `./` + `/...` here is how that rule gets broken (Rule 14).
		Cmd:        mergeCheckSmokeCommand([]string{smoke.PackagePattern(styleGatePackageDir)}),
		Name:       styleGateCheckName,
		TimeoutSec: finishCleanupBranchCheckTimeoutSec,
	}, true
}

// finishCleanupBranchStyleGatePackagePresent reports whether the tree under `dir`
// carries the ratchet package. It is a question ABOUT THE TREE, asked the same way
// the sibling smoke arm asks its prerequisite (mergeCheckSmokeUnmeasurable): a
// package a tree does not have is never a failing test, and an invented pattern
// would make the run address nothing. (The CENSUS sibling this comment used to
// name was retired in commit 78bdf05a with the generator it drove.)
func finishCleanupBranchStyleGatePackagePresent(dir string) bool {
	if dir == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(styleGatePackageDir)))
	return err == nil && info.IsDir()
}

// styleGateAbstention is the sentence a caller reports when this arm could not
// become a check: the tree carries no ratchet package, so the question is
// unanswerable HERE rather than answered "clean" (Rule 15).
func styleGateAbstention() string { return styleGateAbsentMark }
