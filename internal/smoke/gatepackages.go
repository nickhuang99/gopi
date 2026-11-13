package smoke

// gatepackages.go — THE MUST-RUN GATE PACKAGE ROSTER, and the ONE function that
// unions it into any branch-scoped selection.
//
// THE DEFECT THIS CLOSES (this epic's own measurement). The delivery loop's test
// leg is finishCleanupBranchCheckTest (internal/tools), and its command scopes
// `smoke.TouchedPackages` — the packages the branch's diff touched — VERBATIM.
// A diff that does not touch a gate package therefore runs no gate at all, and
// the gates themselves are the expensive ones (`internal/standards` 123–327 s,
// `internal/tools` 420 s), so no foreground `go test` window ever reaches them
// and nothing else in the tree runs them either (this repo carries no CI
// configuration — `git ls-files | grep -cE '^\.github/workflows/|^\.gitlab-ci|
// ^Jenkinsfile|^\.circleci/'` measured 0 on the cut of this change).
//
// MEASURED CONSEQUENCE, on this repo: two real reds (Rule 13's worktree-root
// fixture literal and the assertion-totality UNCLASSIFIED pair) lived on master
// across a whole merge wave, because every landing in that wave touched no gate
// package and every gate leg therefore read "nothing to run here".
//
// WHY THESE TWO PACKAGES AND NOT MORE (Rule 27 — this must not become the
// whole-tree run). testdata/heavy-tests.tsv already states the criterion in its
// own words: the heavy inventory was derived as the tests needing >=5 s MINUS
// "the gate packages (internal/standards, internal/stylegate), which must stay
// on the smoke path so a merge cannot land with its gates unrun". So the roster
// is not chosen here — it is the set that table EXCLUDES from the heavy
// exemption, and this constant is where that exclusion finally has a name a
// caller can union in.
//
// NOTHING TICKS (Rule 35 — zero new persistent dependency). No cron line, no CI
// file, no Makefile target, no hook, no resident: one exported roster plus one
// pure function, unioned into a selection the delivery lane ALREADY computes and
// run through the entry point it ALREADY invokes. The mechanism that must
// remember nothing is the one that cannot be forgotten.
//
// ITS SIBLING IS NOT A DUPLICATE (Rule 26 — state the overlap).
// internal/tools/task_finish_premerge_stylegate.go already appends an
// UNCONDITIONAL check for internal/stylegate, for exactly this reason. That arm
// and this roster answer the same question for ONE package; the roster is the
// general form (both gate packages, in the branch's own scoped command). The
// lane keeps the arm only where the roster does not already cover it, so the
// ratchet is not measured twice in one delivery.

// The gate packages, spelled once (Rule 1b/13: a directory name that decides
// what runs carries meaning, so it is a constant and its consumers reference
// it — internal/tools' styleGatePackageDir is THIS constant, not a literal).
import (
	"os"
	"path/filepath"
	"sort"
)

const (
	// GatePackageStandards is the standards gate package: the Rule 16–21 text
	// gates and their data report (`TestStandardGates`,
	// `TestStandardGateDataReport` and every rule's red-proof probe).
	GatePackageStandards = "internal/standards"

	// GatePackageStylegate is the style ratchet: a WHOLE-TREE reading
	// (`ignored-error` counts every package), which is why a branch-scoped
	// selection can never be trusted to reach it.
	GatePackageStylegate = "internal/stylegate"
)

// GatePackageDirs is the must-run roster — module-relative, slash-separated,
// exactly as the gate packages sit in the tree. It is a list rather than two
// separate references so a caller UNIONING it cannot forget one of them
// (Rule 13: the membership lives in one place and the function below reads it).
//
// Its provenance is testdata/heavy-tests.tsv (see this file's header), and the
// non-emptiness is ASSERTED by a test rather than assumed: an empty roster would
// union nothing and every caller would keep reading green while running no gate
// (Rule 15).
var GatePackageDirs = []string{
	GatePackageStandards,
	GatePackageStylegate,
}

// GatePackagePatterns renders the roster as go-test package patterns, through
// the ONE composer (PackagePattern) — a second spelling of `./` + `/...` here is
// how the root package's non-recursive rule gets broken (Rule 14).
func GatePackagePatterns() []string {
	out := make([]string, 0, len(GatePackageDirs))
	for _, dir := range GatePackageDirs {
		out = append(out, PackagePattern(dir))
	}
	return out
}

// WithGatePackagesIn returns `packages` UNION the roster patterns the tree at
// `dir` ACTUALLY CARRIES — the form every delivery-path caller wants.
//
// WHY PRESENCE IS ASKED (and why the answer is a NARROWER union, never an
// abstention). The roster is unconditional for GOPI's OWN tree; for any other
// tree the same sentence is false. A lane that refused to build a command at all
// on a tree without `internal/standards` would stop running the diff's OWN
// packages — turning "the gates are absent here" into "nothing is checked here",
// which is strictly worse than what the roster replaced. So the union narrows to
// what exists, and the caller keeps its own behaviour unchanged for a tree that
// carries none of them (its empty-selection answer stays its own).
//
// Nothing here is silent for a reader who asks: GatePackagesMissing reports the
// members this tree does not carry, so the question "were the gates covered?"
// has a direct answer instead of an inference from a command string.
func WithGatePackagesIn(dir string, packages []string) []string {
	present := make(map[string]bool, len(GatePackageDirs))
	for _, rel := range GatePackageDirs {
		if dir != "" && dirHasPackage(dir, rel) {
			present[PackagePattern(rel)] = true
		}
	}
	out := make([]string, 0, len(packages)+len(present))
	for _, p := range WithGatePackages(packages) {
		// WithGatePackages already added every roster pattern; keep only the
		// members this tree carries, so a tree without them reads exactly as it
		// did before the roster existed.
		if rosterPattern(p) && !present[p] {
			continue
		}
		out = append(out, p)
	}
	return out
}

// GatePackagesMissing returns the roster directories the tree at `dir` does NOT
// carry, in roster order. A caller that needs to SAY the gates could not run
// here reads this; an empty result means the whole roster is present.
func GatePackagesMissing(dir string) []string {
	var missing []string
	for _, rel := range GatePackageDirs {
		if dir == "" || !dirHasPackage(dir, rel) {
			missing = append(missing, rel)
		}
	}
	return missing
}

// dirHasPackage reports whether `dir` holds the module-relative directory `rel`.
func dirHasPackage(dir, rel string) bool {
	info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
	return err == nil && info.IsDir()
}

// rosterPattern reports whether `p` is one of the roster's OWN patterns (as
// opposed to a package the caller's diff selected, which is never dropped).
func rosterPattern(p string) bool {
	for _, rel := range GatePackageDirs {
		if p == PackagePattern(rel) {
			return true
		}
	}
	return false
}

// WithGatePackages returns `packages` UNION the gate roster's patterns, SORTED
// and DEDUPED. It is PURE (no filesystem, no git, no environment): every
// judgement is visible here and unit-testable without a repository.
//
// It never narrows and never widens to the whole module: the result is the
// caller's own selection plus at most len(GatePackageDirs) patterns, so a
// branch that touched nothing still addresses exactly the gates (which is the
// point — the gates are the invariant, not the diff) and a branch that touched
// the gates keeps them once.
//
// It also never RETURNS an empty list for an empty input: an empty selection
// means "the diff touched no package", and the answer to that is the roster, not
// silence. That is the shape the delivery lane needs — a docs-only branch must
// still run the gates, because a document landing has moved a gate's fixture
// before (the man-source red in mergeset_cmdagent.go's header).
func WithGatePackages(packages []string) []string {
	seen := make(map[string]bool, len(packages)+len(GatePackageDirs))
	out := make([]string, 0, len(packages)+len(GatePackageDirs))
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, p := range packages {
		add(p)
	}
	for _, p := range GatePackagePatterns() {
		add(p)
	}
	sort.Strings(out)
	return out
}
