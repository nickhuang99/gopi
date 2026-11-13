package tools

// mergeset_stylegate.go — THE WHOLE-REPO-RATCHET HALF of the merged-result
// measurement (rebuild_vet_mergeset.go).
//
// THE DEFECT this file closes, in the words of whoever next reads the branch.
// internal/stylegate is a WHOLE-REPO invariant: its gate scans every package in
// the module (gate_test.go: TestStyleGate walks up to go.mod and calls Scan over
// the tree) and fails when any rule exceeds a ceiling of 0 — a new
// `time.Sleep` in a test, a `t.Skip` with no reason, a discarded error, an
// unchecked Close. Its scope is the whole tree, so the branch that introduces a
// hit can be ANY branch; nothing about the hit points at internal/stylegate.
//
// THIS STEP ALREADY RAN IT, AND THE SCOPE MADE THAT A NO-OP FOR THE ONE CASE
// THAT MATTERS. mergesetGatePackageDirs is a FIXED list of three gate
// directories, and the gate-package leg joins the run set only when the
// LANDING'S OWN DIFF touched one of them. A branch that adds
// `time.Sleep(50 * time.Millisecond)` to cmd/agent/x_test.go touches
// cmd/agent — not internal/stylegate — so the leg never fired, the landing was
// green on every measurement the close loop owned, and master went red for
// whoever ran the suite next. Measured at this ground's base 7ecfa12: the
// ratchet suite is GREEN (the 30 drifted sites were annotated by task
// 01a143d2's branch, merged), so the failure mode left is exactly the silent
// arrival of a NEW hit — which is what an unscoped branch brings.
//
// WHY THE SCOPE IS "ANY .go FILE" AND NOT A PACKAGE LIST. The stylegate leg is
// the ONE leg in this step whose invariant is not keyed to a directory: the
// suite measures the whole module, and a hit in any package moves it. So the
// honest trigger is the landing touching Go SOURCE at all — which is every
// landing that can possibly change the answer, and (measured on this repo) the
// ordinary landing. A landing that touches no .go file CANNOT move a Go AST
// scan, and that arm abstains with its reason stated (Rule 15: never a silent
// skip).
//
// WHY NOTHING ELSE IS NEEDED. No second run set member, no widening of the
// fixed directory list: when the landing's diff already touched a gate
// directory, the gate-package leg asks for ./internal/stylegate itself, and
// this leg stands down (mergesetStylegateOwed) rather than running the same
// command twice in one measurement.
//
// WHY HERE AND NOT A NEW HOOK (Rule 3/35 — no new tool, no new script, no new
// resident dependency). RebuildAfterMergeMergeset already runs on every landing
// (RebuildAfterMerge's close loop, `gopi task merge`'s tail, the merger pass),
// already materialises the merged revision in a detached worktree, already
// measures the FIRST PARENT for attribution, and already owns the reporting
// channels (notify + deduped bug row). The ratchet is ONE MORE MEMBER in a run
// set this step already builds — nothing ticks, nothing new has to be
// remembered, and nothing is installed on the host.
//
// THE ATTRIBUTION ARM IS THE EXISTING ONE. The base revision is already
// checked out and already measured for every run in the set, so a red the base
// ALSO had is downgraded to an abstention by mergesetGateAttribute — the same
// reader, the same reason text, no second policy. That is what keeps a
// legitimately-red base from firing on every landing that happens to touch Go.
//
// NEVER A REPAIR (Rule 21-style: no ceiling is raised, no baseline is
// re-pointed). The reason table (internal/stylegate/reasons) and the ceiling
// table (gate_test.go) are the RATCHET's own data; editing either from a
// landing gate would let a run excuse its own hit. This leg measures and
// reports, exactly like the sibling legs, with res.OK untouched.

import (
	"fmt"
	"strings"
)

// mergesetStylegateGateName is how this leg identifies itself in every message.
// It is deliberately NOT mergesetGateTestGateLabel: the two are different
// measurements (a directory-scoped run set vs a whole-repo invariant), and a
// reader of the merger log must be able to tell which one spoke (Rule 1b).
const (
	mergesetStylegateGateName = "stylegate-ratchet"

	// mergesetStylegateNoGoNote is the abstention this leg reports when the
	// landing's diff brought no Go source: a scan over a tree whose Go source
	// the landing did not change cannot be moved by it, so the leg stands down
	// and SAYS SO (Rule 15 — a skip that cannot be told from a broken leg is a
	// hole, not a saving).
	mergesetStylegateNoGoNote = "the landing brought no Go source (%s), so the whole-repo style ratchet cannot have moved — not run"

	// mergesetStylegateAlreadyRunNote is the stand-down when the gate-package
	// leg on this same measurement already asked the toolchain for
	// ./internal/stylegate (it does so whenever the landing itself touched that
	// directory). Running the same command twice in one close-out would double
	// the leg's wall time to learn the same answer (Rule 27: the merge path
	// stays cheap), so the duplicate is skipped by NAME.
	mergesetStylegateAlreadyRunNote = "the gate-package leg already runs ./internal/stylegate on this landing (%s touched), so the ratchet leg stands down rather than running the same command twice"
)

// mergesetStylegateRuns answers the whole-repo-ratchet question for one landing
// and returns the run to append to the caller's run set, plus ONE log-ready
// note. The contract is the sibling legs' (mergesetGendocRuns,
// mergesetDocclassRuns): a non-empty note means this leg added NOTHING, and the
// note distinguishes "nothing was obliged" from "the scope could not be read"
// in its own words.
//
// repo is the repository the landing happened in; mergeRev is the merged
// revision; dir is the landing's own detached checkout, which only has to be
// non-empty (the leg runs the module-root suite, it reads no file out of it).
func mergesetStylegateRuns(repo, mergeRev, dir string) ([]mergesetGateRun, string) {
	if dir == "" {
		return nil, mergesetGateReasonNoScratch
	}
	goFiles, reason := mergesetStylegateGoFiles(repo, mergeRev)
	if reason != "" {
		return nil, reason
	}
	if len(goFiles) == 0 {
		return nil, fmt.Sprintf(mergesetStylegateNoGoNote, mergesetStylegateScopeLabel(repo, mergeRev))
	}
	// The gate-package leg's own decision is asked, not re-derived: if it already
	// names ./internal/stylegate, this run would be the same command a second
	// time. The predicate is the sibling's membership test, so the two legs
	// cannot disagree about what "already asked" means (Rule 14).
	if mergesetStylegateAlreadyAsked(repo, mergeRev) {
		return nil, fmt.Sprintf(mergesetStylegateAlreadyRunNote, mergesetGateStylegateDir)
	}
	ref := "." + mergesetGatePackageSep + mergesetGateStylegateDir
	mergesetGateLog(mergesetStylegateFireLogFmt, len(goFiles), strings.Join(mergesetStylegateSample(goFiles), ", "))
	return []mergesetGateRun{{
		Gate:     mergesetStylegateGateName,
		Argv:     append(mergesetGateTestArgv(), ref),
		Packages: []string{ref},
	}}, ""
}

// mergesetStylegateGoFiles returns the Go files the LANDING brought in — the
// change set a whole-repo AST scan can be moved by.
//
// It reads the same diff the sibling legs read (`<mergeRev>^ <mergeRev>`), so
// all of this step's scope decisions are answers to ONE question (Rule 2/14). A
// diff that cannot be read is an ABSTENTION, never a silent widening: a leg
// that cannot see what the landing changed must say so rather than run (or not
// run) on a guess.
func mergesetStylegateGoFiles(repo, mergeRev string) (files []string, reason string) {
	out, err := repoSweepGit(repo, mergesetGateDiffVerb, mergesetGateNameOnlyFlag,
		mergeRev+mergesetGateFirstParent, mergeRev)
	if err != nil {
		return nil, fmt.Sprintf("%s (%s)", mergesetGateReasonNoDiffBase, mergeBuildGateTail(err.Error()))
	}
	for _, line := range strings.Split(out, "\n") {
		p := strings.TrimSpace(line)
		if p == "" || !strings.HasSuffix(p, mergesetGateGoSourceSuffix) {
			continue
		}
		files = append(files, p)
	}
	return files, ""
}

// mergesetStylegateAlreadyAsked reports whether the gate-package leg will name
// ./internal/stylegate on this landing. It delegates to the sibling leg's own
// mapping (mergesetGateTouchedGatePackages) rather than repeating its prefix
// test, so a change to that leg's scope cannot leave this one measuring a
// duplicate.
func mergesetStylegateAlreadyAsked(repo, mergeRev string) bool {
	refs, reason := mergesetGateTouchedGatePackages(repo, mergeRev)
	if reason != "" {
		// The sibling leg abstains in this case too, so nothing asked for the
		// package: this leg is the only one that can.
		return false
	}
	want := "." + mergesetGatePackageSep + mergesetGateStylegateDir
	for _, r := range refs {
		if r == want {
			return true
		}
	}
	return false
}

// mergesetStylegateScopeLabel names the diff the scope was read from, so the
// abstention above says WHICH revision pair it looked at (Rule 15b: a report
// without its evidence is not a report).
func mergesetStylegateScopeLabel(repo, mergeRev string) string {
	return repo + " " + mergeRev + mergesetGateFirstParent + ".." + mergeRev
}

// mergesetStylegateSample caps the file list carried into the log line: a
// landing can bring hundreds of Go files, and the line exists to say the leg
// FIRED, not to enumerate.
func mergesetStylegateSample(files []string) []string {
	if len(files) <= mergesetStylegateSampleMax {
		return files
	}
	return files[:mergesetStylegateSampleMax]
}

// mergesetStylegateSampleMax bounds the sample above (Rule 1b).
const mergesetStylegateSampleMax = 3

// mergesetStylegateFireLogFmt is the line a FIRING leg logs: the scope in
// numbers plus a sample, so a reader of the close-out can see what obliged the
// run without re-reading the diff.
const mergesetStylegateFireLogFmt = "the landing brought %d Go file(s) (%s), so the whole-repo style ratchet runs over the merged revision"
